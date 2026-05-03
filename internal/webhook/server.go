package webhook

import (
	"context"
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
	count    int
	lastSeen time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitor),
		limit:    limit,
		window:   window,
		stopChan: make(chan struct{}),
	}
	// Cleanup stale entries every minute
	go func() {
		ticker := time.NewTicker(cleanupInterval)
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

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, ok := rl.visitors[ip]
	if !ok || time.Since(v.lastSeen) > rl.window {
		rl.visitors[ip] = &visitor{count: 1, lastSeen: time.Now()}
		return true
	}
	v.count++
	v.lastSeen = time.Now()
	return v.count <= rl.limit
}

type Server struct {
	Port        int
	Secret      string
	deploy      func(ctx context.Context, appName string) error
	deployMu    sync.Mutex
	deployLocks map[string]bool
	deployWg    sync.WaitGroup
	limiter     *rateLimiter
}

func NewServer(port int, secret string) *Server {
	return &Server{
		Port:        port,
		Secret:      secret,
		deployLocks: make(map[string]bool),
		limiter:     newRateLimiter(60, time.Minute), // 60 req/min per IP
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
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Graceful shutdown on signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-stop
		log.Println("Shutting down webhook server...")

		// Stop the rate limiter cleanup goroutine
		s.limiter.stop()

		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	if err := httpListenAndServe(srv); err != http.ErrServerClosed {
		return err
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

	// Read body (limit to 10 MB to prevent memory exhaustion)
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
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

	// Parse event from provider-specific header
	event := r.Header.Get("X-GitHub-Event")
	if event == "" {
		event = r.Header.Get("X-Gitlab-Event")
	}
	if event == "" {
		event = r.Header.Get("X-Gitea-Event")
	}
	ref := extractRefFromPayload(string(body))
	branch := ""
	if strings.HasPrefix(ref, "refs/heads/") {
		branch = strings.TrimPrefix(ref, "refs/heads/")
	}

	// Check event type (normalize various provider event strings)
	lowerEvent := strings.ToLower(event)
	lowerEvent = strings.ReplaceAll(lowerEvent, " hook", "")
	if lowerEvent != "push" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Event ignored (not push)")
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

	// Trigger deploy (serialized per app)
	if s.deploy != nil {
		s.deployWg.Add(1)
		go func() {
			defer s.deployWg.Done()

			s.deployMu.Lock()
			if s.deployLocks[appName] {
				s.deployMu.Unlock()
				log.Printf("Deploy already in progress for %s, skipping", appName)
				return
			}
			// Mark deploy in progress
			s.deployLocks[appName] = true
			s.deployMu.Unlock()

			log.Printf("Webhook triggered deploy for %s", appName)

			// ctx is passed to the deploy handler so it can short-circuit on
			// timeout or shutdown. Whether real cancellation actually
			// interrupts an in-flight deploy depends on the handler honoring
			// ctx — RunRedeployContext checks ctx.Err() at major boundaries
			// (between git pull, build, compose up, caddy reload, state save)
			// but the long-running subprocess steps themselves run to their
			// own internal timeouts. Future work can thread ctx into
			// docker.ComposeUp / docker.BuildImage / git.Pull for true
			// per-syscall cancellation.
			ctx, cancel := context.WithTimeout(context.Background(), deployTimeout)
			defer cancel()

			// Run deploy in a goroutine so we can handle timeout
			done := make(chan error, 1)
			go func() {
				done <- s.deploy(ctx, appName)
			}()

			// Wait for deploy to complete or timeout
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

			// Release the lock
			s.deployMu.Lock()
			delete(s.deployLocks, appName)
			s.deployMu.Unlock()
		}()
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Deploy triggered for %s", appName)
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
