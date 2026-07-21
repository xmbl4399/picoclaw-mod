package health

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"maps"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sipeed/picoclaw/pkg/routing"
)

type Server struct {
	server     *http.Server
	mu         sync.RWMutex
	ready      bool
	checks     map[string]Check
	startTime  time.Time
	reloadFunc func() error
	authToken  string // optional bearer token for protected endpoints

	activeCharacter        atomic.Value // holds string; empty = no character active
	characterChangeCallback func(string) // called when SetActiveCharacter is called
}

type Check struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type StatusResponse struct {
	Status string           `json:"status"`
	Uptime string           `json:"uptime"`
	PID    int              `json:"pid,omitempty"`
	Checks map[string]Check `json:"checks,omitempty"`
}

func NewServer(host string, port int, token string) *Server {
	mux := http.NewServeMux()
	s := &Server{
		ready:     false,
		checks:    make(map[string]Check),
		startTime: time.Now(),
		authToken: token,
	}

	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/ready", s.readyHandler)
	mux.HandleFunc("/reload", s.reloadHandler)
	mux.HandleFunc("/character/active", s.characterActiveHandler)
	mux.HandleFunc("/character/clear", s.characterClearHandler)
	mux.HandleFunc("/", serveWebUI)

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	return s
}

func (s *Server) Start() error {
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
	return s.server.ListenAndServe()
}

func (s *Server) StartContext(ctx context.Context) error {
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return s.server.Shutdown(context.Background())
	}
}

func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.ready = false
	s.mu.Unlock()
	return s.server.Shutdown(ctx)
}

func (s *Server) SetReady(ready bool) {
	s.mu.Lock()
	s.ready = ready
	s.mu.Unlock()
}

func (s *Server) RegisterCheck(name string, checkFn func() (bool, string)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status, msg := checkFn()
	s.checks[name] = Check{
		Name:      name,
		Status:    statusString(status),
		Message:   msg,
		Timestamp: time.Now(),
	}
}

// SetReloadFunc sets the callback function for config reload.
func (s *Server) SetReloadFunc(fn func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadFunc = fn
}

func (s *Server) reloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed, use POST"})
		return
	}

	// Token check
	s.mu.RLock()
	requiredToken := s.authToken
	s.mu.RUnlock()

	if requiredToken != "" {
		given := extractBearerToken(r.Header.Get("Authorization"))
		if given == "" || subtle.ConstantTimeCompare([]byte(given), []byte(requiredToken)) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
	}

	s.mu.Lock()
	reloadFunc := s.reloadFunc
	s.mu.Unlock()

	if reloadFunc == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "reload not configured"})
		return
	}

	if err := reloadFunc(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reload triggered"})
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	uptime := time.Since(s.startTime)
	resp := StatusResponse{
		Status: "ok",
		Uptime: uptime.String(),
		PID:    os.Getpid(),
	}

	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s.mu.RLock()
	ready := s.ready
	checks := make(map[string]Check)
	maps.Copy(checks, s.checks)
	s.mu.RUnlock()

	if !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(StatusResponse{
			Status: "not ready",
			Checks: checks,
		})
		return
	}

	for _, check := range checks {
		if check.Status == "fail" {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(StatusResponse{
				Status: "not ready",
				Checks: checks,
			})
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	uptime := time.Since(s.startTime)
	_ = json.NewEncoder(w).Encode(StatusResponse{
		Status: "ready",
		Uptime: uptime.String(),
		Checks: checks,
	})
}

// HandlerMux is the interface for registering HTTP handlers, used by
// RegisterOnMux so that callers can pass any mux implementation
// (e.g. *http.ServeMux or a custom dynamic mux).
type HandlerMux interface {
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// RegisterOnMux registers /health, /ready, /reload, /character/active
// and the root web UI (/) handlers onto the given mux.
// This allows the health endpoints to be served by a shared HTTP server.
func (s *Server) RegisterOnMux(mux HandlerMux) {
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/ready", s.readyHandler)
	mux.HandleFunc("/reload", s.reloadHandler)
	mux.HandleFunc("/character/active", s.characterActiveHandler)
	mux.HandleFunc("/character/clear", s.characterClearHandler)
	// Register the web chat UI at the root path.
	// Go 1.22+ ServeMux prioritizes longer, more specific patterns over
	// shorter prefix patterns, so /, /pico/, /character/active etc.
	// will correctly dispatch to their respective handlers.
	mux.HandleFunc("/", serveWebUI)
}

// SetActiveCharacter stores the currently active character ID (safe for concurrent use).
// An empty value means no character is active (default agent persona).
// When a non-empty character ID is set, the characterChangeCallback (if non-nil) is invoked.
func (s *Server) SetActiveCharacter(characterID string) {
	id := strings.TrimSpace(characterID)
	s.activeCharacter.Store(id)
	if s.characterChangeCallback != nil {
		s.characterChangeCallback(id)
	}
}

// SetCharacterChangeCallback sets a callback that fires when a character becomes active.
func (s *Server) SetCharacterChangeCallback(fn func(characterID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.characterChangeCallback = fn
}

// ActiveCharacter returns the currently active character ID, or "" if none.
func (s *Server) ActiveCharacter() string {
	val := s.activeCharacter.Load()
	if val == nil {
		return ""
	}
	return val.(string)
}

// CharacterGetter returns a function that returns the current active character ID.
// This is a convenience for passing to channels.
func (s *Server) CharacterGetter() func() string {
	return s.ActiveCharacter
}

// characterClearHandler handles POST /character/clear?name=...&id=...
// Used by the WebUI on character switch to set the active character.
// The prompt body is accepted but ignored — the backend only needs the ID.
func (s *Server) characterClearHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		id = r.URL.Query().Get("name")
	}
	if id == "" {
		http.Error(w, "missing name or id parameter", http.StatusBadRequest)
		return
	}
	s.SetActiveCharacter(id)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "character_id": id})
}

// characterActiveHandler serves GET /character/active returning the active character ID
// or records the active character via POST (JSON: {"character_id": "..."}).
// POST requires Bearer token authentication when authToken is configured.
func (s *Server) characterActiveHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"character_id": s.ActiveCharacter(),
		})

	case http.MethodPost:
		// Require authentication
		s.mu.RLock()
		requiredToken := s.authToken
		s.mu.RUnlock()
		if requiredToken != "" {
			given := extractBearerToken(r.Header.Get("Authorization"))
			if given == "" || subtle.ConstantTimeCompare([]byte(given), []byte(requiredToken)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		var body struct {
			CharacterID string `json:"character_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Normalize the character ID
		id := routing.NormalizeAgentID(body.CharacterID)
		s.SetActiveCharacter(id)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"character_id": id,
		})

	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed, use GET or POST"})
	}
}

func statusString(ok bool) string {
	if ok {
		return "ok"
	}
	return "fail"
}

// extractBearerToken returns the token from an "Authorization: Bearer <t>" header,
// or the empty string if the header is missing or malformed.
func extractBearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) < len(prefix) {
		return ""
	}
	if header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}
