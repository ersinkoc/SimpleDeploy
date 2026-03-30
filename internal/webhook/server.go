package webhook

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

var validAppNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)

func isValidAppName(name string) bool {
	return validAppNameRe.MatchString(name)
}

type Server struct {
	Port        int
	Secret      string
	deploy      func(appName string) error
	deployMu    sync.Mutex
	deployLocks map[string]bool
}

func NewServer(port int, secret string) *Server {
	return &Server{
		Port:        port,
		Secret:      secret,
		deployLocks: make(map[string]bool),
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

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return server.ListenAndServe()
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract app name from path: /_qd/webhook/{app-name}
	path := strings.TrimPrefix(r.URL.Path, "/_qd/webhook/")
	appName := strings.TrimSpace(path)
	if appName == "" || !isValidAppName(appName) {
		http.Error(w, "App name required", http.StatusBadRequest)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}

	// Verify signature/token based on webhook provider
	if s.Secret != "" {
		verified := false

		if sig := r.Header.Get("X-Hub-Signature-256"); sig != "" {
			// GitHub or Gitea (both use HMAC-SHA256 with X-Hub-Signature-256)
			verified = VerifyGitHubSignature(body, sig, s.Secret)
		} else if r.Header.Get("X-Gitlab-Token") != "" {
			// GitLab uses a shared token in header
			verified = VerifyGitLabToken(r, s.Secret)
		} else if sig := r.Header.Get("X-Gitea-Signature"); sig != "" {
			// Gitea-specific header (same HMAC-SHA256 computation)
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
		go func() {
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
			done := make(chan struct{})
			timer := time.AfterFunc(30*time.Minute, func() {
				s.deployMu.Lock()
				delete(s.deployLocks, appName)
				s.deployMu.Unlock()
				log.Printf("Deploy for %s timed out after 30 minutes, releasing lock", appName)
			})

			if err := s.deploy(appName); err != nil {
				log.Printf("Deploy failed for %s: %v", appName, err)
			}
			close(done)
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
