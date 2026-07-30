package webhook

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

// maxWebhookBody caps how much of a request body is read. Large enough for
// any realistic push payload's `ref` to be parsed; requests beyond it are
// answered 413 rather than silently truncated.
const maxWebhookBody = 10 << 20

var (
	cleanupInterval    = time.Minute
	httpListenAndServe = func(srv *http.Server) error { return srv.ListenAndServe() }
	deployTimeout      = 30 * time.Minute
)

func isValidAppName(name string) bool {
	return state.AppNameRegex.MatchString(name)
}

// rateLimiter provides a simple per-IP token bucket rate limiter.
type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    int
	window   time.Duration
	stopChan chan struct{}
}

type visitor struct {
	count int
	// windowStart is when the current counting window opened. It is fixed for
	// the life of the window and is what allow() compares against.
	windowStart time.Time
	// lastSeen is only used by cleanup() to evict idle entries, so it may be
	// refreshed on every request without affecting the limit.
	lastSeen time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitor),
		limit:    limit,
		window:   window,
		stopChan: make(chan struct{}),
	}
	// cleanupInterval is read here, in the caller's goroutine, and not inside
	// the goroutine below. Reading it there made the load race with any test
	// that swaps the tunable: the spawned goroutine is not ordered against
	// anything the caller does afterwards, so the load could land arbitrarily
	// late. Capturing it at construction time puts the read in the same
	// happens-before chain as the constructor call.
	interval := cleanupInterval
	// Cleanup stale entries every minute
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rl.cleanup()
			case <-rl.stopChan:
				return
			}
		}
	}()
	return rl
}

func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for ip, v := range rl.visitors {
		if time.Since(v.lastSeen) > rl.window {
			delete(rl.visitors, ip)
		}
	}
}

func (rl *rateLimiter) stop() {
	close(rl.stopChan)
}

// allow reports whether a request from ip may proceed, using a fixed window
// of rl.window that holds at most rl.limit requests.
//
// The previous implementation compared against lastSeen, which it also
// refreshed on every request — so the window only ever expired after a full
// period of complete silence. A client sending one request per second was
// therefore allowed exactly `limit` requests and then blocked permanently,
// because the window it was waiting on could never elapse. For a webhook
// endpoint that means a moderately active repository gets locked out for good
// and push-to-deploy silently stops working.
func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	v, ok := rl.visitors[ip]
	if !ok || now.Sub(v.windowStart) > rl.window {
		rl.visitors[ip] = &visitor{count: 1, windowStart: now, lastSeen: now}
		return true
	}
	v.count++
	v.lastSeen = now
	return v.count <= rl.limit
}

type Server struct {
	Port   int
	Secret string
	deploy func(ctx context.Context, appName string) error
	// deployMu guards both maps below.
	deployMu    sync.Mutex
	deployLocks map[string]bool
	// deployPending records that a push arrived while a deploy for that app
	// was already running, so exactly one follow-up run is performed after it
	// finishes instead of losing the newer commit.
	deployPending map[string]bool
	deployWg      sync.WaitGroup
	limiter       *rateLimiter
}

func NewServer(port int, secret string) *Server {
	return &Server{
		Port:          port,
		Secret:        secret,
		deployLocks:   make(map[string]bool),
		deployPending: make(map[string]bool),
		limiter:       newRateLimiter(60, time.Minute), // 60 req/min per IP
	}
}

func (s *Server) SetDeployHandler(handler func(ctx context.Context, appName string) error) {
	s.deploy = handler
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/_qd/webhook/", s.handleWebhook)
	mux.HandleFunc("/_qd/health", s.handleHealth)

	addr := fmt.Sprintf(":%d", s.Port)
	log.Printf("Webhook server listening on %s", addr)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
		// ReadHeaderTimeout is the one that actually bounds a slowloris attack:
		// without it a client can hold a connection open indefinitely by
		// dribbling out header bytes, because ReadTimeout only starts counting
		// once the request line has been read. This listener is reachable from
		// the internet, so the cost of omitting it is real.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Graceful shutdown on signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// shutdownStarted is closed before Shutdown is called, shutdownDone after it
	// returns. Both are needed to make the deployWg.Wait() below sound:
	// ListenAndServe returns ErrServerClosed the moment Shutdown is CALLED, not
	// when in-flight handlers finish, so without joining Shutdown a request that
	// had passed signature verification but not yet reached deployWg.Add(1)
	// could spawn its deploy goroutine after Wait had already returned — and
	// process exit would kill that deploy mid-build (besides being the
	// documented Add-races-Wait misuse of sync.WaitGroup).
	shutdownStarted := make(chan struct{})
	shutdownDone := make(chan struct{})

	go func() {
		<-stop
		log.Println("Shutting down webhook server...")

		// Stop the rate limiter cleanup goroutine
		s.limiter.stop()

		close(shutdownStarted)
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
		defer cancel()
		srv.Shutdown(ctx)
		close(shutdownDone)
	}()

	if err := httpListenAndServe(srv); err != http.ErrServerClosed {
		return err
	}

	// Join the shutdown only if it is ours to join. ErrServerClosed can also
	// come from a Close/Shutdown issued elsewhere — or from a test double that
	// never triggers the signal path — and blocking on shutdownDone in that
	// case would hang here forever. shutdownStarted is closed before Shutdown
	// is called, so if our goroutine is the source this select always sees it.
	select {
	case <-shutdownStarted:
		<-shutdownDone
	default:
	}

	// Wait for in-flight deploys to finish
	log.Println("Waiting for in-flight deploys to complete...")
	s.deployWg.Wait()
	log.Println("All deploys completed. Server stopped.")

	return nil
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Rate limiting
	ip := clientIP(r.RemoteAddr)
	if !s.limiter.allow(ip) {
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}

	// Extract app name from path: /_qd/webhook/{app-name}
	path := strings.TrimPrefix(r.URL.Path, "/_qd/webhook/")
	appName := strings.TrimSpace(path)
	if appName == "" || !isValidAppName(appName) {
		http.Error(w, "App name required", http.StatusBadRequest)
		return
	}

	// Read body, capped to prevent memory exhaustion. MaxBytesReader rather
	// than LimitReader: LimitReader TRUNCATES silently at the cap, so a
	// legitimate oversized payload (GitHub delivers up to 25 MB for large
	// multi-commit pushes) had its HMAC computed over the truncated bytes and
	// was answered "401 Invalid signature" — sending the operator hunting for
	// a secret mismatch that does not exist. Answer 413 honestly instead.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "Payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}

	// Verify signature/token based on webhook provider
	if s.Secret != "" {
		verified := false

		if sig := r.Header.Get("X-Hub-Signature-256"); sig != "" {
			verified = VerifyGitHubSignature(body, sig, s.Secret)
		} else if r.Header.Get("X-Gitlab-Token") != "" {
			verified = VerifyGitLabToken(r, s.Secret)
		} else if sig := r.Header.Get("X-Gitea-Signature"); sig != "" {
			verified = VerifyGiteaSignature(body, sig, s.Secret)
		}

		if !verified {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}
	// authenticated records whether this request proved knowledge of the
	// shared secret. It gates the deploy trigger below: without a configured
	// secret every verification branch above is skipped, which used to mean
	// ANY unauthenticated POST to this endpoint could make the server pull and
	// run arbitrary code from the repository. Read-only outcomes (ignored
	// events, unknown branches) stay unauthenticated so misconfiguration is
	// still diagnosable.
	authenticated := s.Secret != ""

	// Parse event from provider-specific header
	event := r.Header.Get("X-GitHub-Event")
	if event == "" {
		event = r.Header.Get("X-Gitlab-Event")
	}
	if event == "" {
		event = r.Header.Get("X-Gitea-Event")
	}
	ref := extractRefFromPayload(string(body))
	// Non-branch refs (tags, notes) leave branch empty, which the check further
	// down treats as "no branch information" rather than as a mismatch.
	branch := ""
	if head, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
		branch = head
	}

	// Check event type (normalize various provider event strings)
	lowerEvent := strings.ToLower(event)
	lowerEvent = strings.ReplaceAll(lowerEvent, " hook", "")
	if lowerEvent != "push" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Event ignored (not push)")
		return
	}

	// A push event is a request to execute code. Refuse it outright when no
	// secret is configured rather than deploying on an unverified trigger.
	if !authenticated {
		log.Printf("Rejected unauthenticated push for %s: no webhook secret configured", appName)
		http.Error(w, "Webhook secret not configured on this server; refusing to deploy", http.StatusUnauthorized)
		return
	}

	// A ref that is present but is not a branch must not deploy. GitHub
	// delivers TAG pushes as `X-GitHub-Event: push` with `ref:
	// refs/tags/v1.2.3`, and its webhook config cannot filter them out — so
	// without this check every tag push redeployed the app even though the
	// operator configured "branch: main". Only a wholly absent ref (a
	// provider that does not send one) still falls through as "no branch
	// information".
	if ref != "" && branch == "" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Ref ignored (not a branch)")
		return
	}

	// Load app config and check branch
	app, err := state.GetApp(appName)
	if err != nil {
		http.Error(w, fmt.Sprintf("App not found: %s", appName), http.StatusNotFound)
		return
	}

	if branch != "" && branch != app.Branch {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Branch ignored (expected %s, got %s)", app.Branch, branch)
		return
	}

	// Honour the per-app opt-out. This was never checked, so an app deployed
	// with "Enable push-to-deploy webhook? n" still auto-deployed on every
	// push that reached this endpoint with the global secret — while `list`
	// displayed "Webhook: false", making the flag look authoritative when it
	// was purely cosmetic.
	if !app.WebhookEnabled {
		log.Printf("Rejected push for %s: push-to-deploy is disabled for this app", appName)
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, "Push-to-deploy is disabled for %s", appName)
		return
	}

	// Serialize per app, and decide the HTTP answer from that BEFORE replying.
	// The lock check used to live inside the spawned goroutine while the
	// response was written unconditionally, so a push arriving mid-deploy was
	// dropped and still answered "200 Deploy triggered" — the provider's
	// delivery log showed success for a commit that was never deployed, and the
	// running version silently lagged until the next push.
	//
	// A push that arrives while a deploy is running is now recorded as pending
	// and re-run once that deploy finishes, so the newest commit always gets
	// deployed rather than being lost.
	if s.deploy == nil {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Deploy triggered for %s", appName)
		return
	}

	s.deployMu.Lock()
	if s.deployLocks[appName] {
		s.deployPending[appName] = true
		s.deployMu.Unlock()
		log.Printf("Deploy already in progress for %s; queued a follow-up run", appName)
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, "Deploy in progress for %s; queued a follow-up run for this push", appName)
		return
	}
	s.deployLocks[appName] = true
	s.deployMu.Unlock()

	s.deployWg.Add(1)
	go func() {
		defer s.deployWg.Done()

		// Loop so a push that arrived mid-deploy (recorded as pending above)
		// is served by one follow-up run rather than being dropped. The lock
		// is held for the whole loop, so a burst of pushes collapses into at
		// most one extra deploy — which is what the operator wants: the extra
		// run deploys the branch tip, i.e. every queued commit at once.
		for {
			log.Printf("Webhook triggered deploy for %s", appName)
			s.runDeploy(appName)

			s.deployMu.Lock()
			if s.deployPending[appName] {
				delete(s.deployPending, appName)
				s.deployMu.Unlock()
				log.Printf("Running queued follow-up deploy for %s", appName)
				continue
			}
			delete(s.deployLocks, appName)
			s.deployMu.Unlock()
			return
		}
	}()

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Deploy triggered for %s", appName)
}

// runDeploy invokes the deploy handler once, bounded by deployTimeout.
//
// ctx is passed to the handler so it can short-circuit on timeout or shutdown.
// Whether real cancellation actually interrupts an in-flight deploy depends on
// the handler honoring ctx — RunRedeployContext checks ctx.Err() at major
// boundaries (between git pull, build, compose up, caddy reload, state save)
// but the long-running subprocess steps themselves run to their own internal
// timeouts. Future work can thread ctx into docker.ComposeUp /
// docker.BuildImage / git.Pull for true per-syscall cancellation.
func (s *Server) runDeploy(appName string) {
	ctx, cancel := context.WithTimeout(context.Background(), deployTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.deploy(ctx, appName)
	}()

	select {
	case err := <-done:
		if err != nil {
			log.Printf("Deploy failed for %s: %v", appName, err)
		}
	case <-ctx.Done():
		log.Printf("Deploy for %s timed out after %v, waiting for handler to honor ctx", appName, deployTimeout)
		<-done
		log.Printf("Timed-out deploy for %s completed", appName)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

// clientIP normalises an http.Request.RemoteAddr value into a bare host
// suitable for use as a rate-limiter key. The previous implementation used
// strings.LastIndex(":") which left the brackets on IPv6 addresses
// ("[::1]:1234" → "[::1]") and silently mis-split addresses without a port.
// net.SplitHostPort understands both v4 host:port and v6 [host]:port forms;
// when it fails (no port present, malformed) we fall back to the raw value
// so a misshaped RemoteAddr still gets rate-limited rather than being
// silently lumped into one bucket.
func clientIP(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	// SplitHostPort failed — could be a bare IPv6 without port, or a string
	// that already contains no port. Strip surrounding brackets if present
	// so "[::1]" and "::1" hash to the same bucket.
	if strings.HasPrefix(remoteAddr, "[") && strings.HasSuffix(remoteAddr, "]") {
		return remoteAddr[1 : len(remoteAddr)-1]
	}
	return remoteAddr
}
