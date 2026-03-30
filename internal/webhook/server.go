package webhook

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

var validAppNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)

func isValidAppName(name string) bool {
	return validAppNameRe.MatchString(name)
}

// rateLimiter provides a simple per-IP token bucket rate limiter.
type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    int
	window   time.Duration
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
	}
	// Cleanup stale entries every minute
	go func() {
		for {
			time.Sleep(time.Minute)
			rl.mu.Lock()
			for ip, v := range rl.visitors {
				if time.Since(v.lastSeen) > rl.window {
					delete(rl.visitors, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
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
	deploy      func(appName string) error
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

func (s *Server) SetDeployHandler(handler func(appName string) error) {
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
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
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
	ip := strings.SplitN(r.RemoteAddr, ":", 2)[0]
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
			s.deployLocks[appName] = true
			s.deployMu.Unlock()

			log.Printf("Webhook triggered deploy for %s", appName)

			// Safety: release lock after 30 min even if deploy hangs
			timer := time.AfterFunc(30*time.Minute, func() {
				s.deployMu.Lock()
				delete(s.deployLocks, appName)
				s.deployMu.Unlock()
				log.Printf("Deploy for %s timed out after 30 minutes, releasing lock", appName)
			})

			if err := s.deploy(appName); err != nil {
				log.Printf("Deploy failed for %s: %v", appName, err)
			}
			timer.Stop()

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
