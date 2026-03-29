package webhook

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

type Server struct {
	Port   int
	Secret string
	deploy func(appName string) error
}

func NewServer(port int, secret string) *Server {
	return &Server{
		Port:   port,
		Secret: secret,
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
	if appName == "" {
		http.Error(w, "App name required", http.StatusBadRequest)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}

	// Verify signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if s.Secret != "" && !VerifyGitHubSignature(body, signature, s.Secret) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// Parse event from header and branch from already-read body
	event := r.Header.Get("X-GitHub-Event")
	ref := extractRefFromPayload(string(body))
	branch := ""
	if strings.HasPrefix(ref, "refs/heads/") {
		branch = strings.TrimPrefix(ref, "refs/heads/")
	}

	// Check event type
	if event != "push" {
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

	// Trigger deploy
	if s.deploy != nil {
		go func() {
			log.Printf("Webhook triggered deploy for %s", appName)
			if err := s.deploy(appName); err != nil {
				log.Printf("Deploy failed for %s: %v", appName, err)
			}
		}()
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Deploy triggered for %s", appName)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}
