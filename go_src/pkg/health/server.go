package health

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	server       *http.Server
	mu           sync.RWMutex
	ready        bool
	checks       map[string]Check
	startTime    time.Time
	reloadFunc   func() error
	authToken    string // optional bearer token for protected endpoints
	configPath   string // path to config.json for model switching
	getModelList func() []string // returns list of available model names
	getCurModel  func() string // returns current active model name
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

// SetModelConfig sets config path and callbacks for model switching.
func (s *Server) SetModelConfig(configPath string, getModelList func() []string, getCurModel func() string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configPath = configPath
	s.getModelList = getModelList
	s.getCurModel = getCurModel
}

func (s *Server) modelsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	if r.Method == http.MethodOptions { w.WriteHeader(204); return }
	w.Header().Set("Content-Type", "application/json")
	s.mu.RLock()
	getList := s.getModelList
	getCur := s.getCurModel
	s.mu.RUnlock()

	if getList == nil {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"models": []string{}, "current": ""})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"models":  getList(),
		"current": getCur(),
	})
}

func (s *Server) modelSwitchHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	if r.Method == http.MethodOptions { w.WriteHeader(204); return }

	modelName := r.URL.Query().Get("model")
	if modelName == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "model parameter required"})
		return
	}

	s.mu.RLock()
	configPath := s.configPath
	s.mu.RUnlock()

	if configPath == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "model switching not configured"})
		return
	}

	// Read config, change model_name, save
	data, err := os.ReadFile(configPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("read config: %v", err)})
		return
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("parse config: %v", err)})
		return
	}

	// Update model_name in agents.defaults
	if agents, ok := cfg["agents"].(map[string]any); ok {
		if defaults, ok := agents["defaults"].(map[string]any); ok {
			defaults["model_name"] = modelName
		}
	}

	newData, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("marshal config: %v", err)})
		return
	}

	if err := os.WriteFile(configPath, newData, 0600); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("write config: %v", err)})
		return
	}

	// Trigger reload
	s.mu.RLock()
	reloadFunc := s.reloadFunc
	s.mu.RUnlock()

	if reloadFunc != nil {
		_ = reloadFunc()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "switched",
		"model":  modelName,
	})
}

func (s *Server) configHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions { w.WriteHeader(204); return }

	s.mu.RLock()
	configPath := s.configPath
	s.mu.RUnlock()

	if configPath == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "config path not set"})
		return
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if r.Method == http.MethodGet {
		var cfg map[string]any
		json.Unmarshal(data, &cfg)
		result := map[string]any{}

		// All agent defaults (safe subset)
		if agents, ok := cfg["agents"].(map[string]any); ok {
			if defaults, ok := agents["defaults"].(map[string]any); ok {
				safe := map[string]any{}
				for _, k := range []string{
					"workspace","restrict_to_workspace","allow_read_outside_workspace",
					"max_tokens","max_tool_iterations","temperature","context_window",
					"max_media_size","steering_mode","summarize_message_threshold",
					"summarize_token_percent","max_llm_retries","llm_retry_backoff_secs",
					"split_on_marker","context_manager","max_parallel_turns",
					"model_fallbacks","image_model","character_prompt",
				} {
					if v, ok := defaults[k]; ok { safe[k] = v }
				}
				// Sub-objects
				for _, k := range []string{"subturn","tool_feedback","turn_profile","routing"} {
					if v, ok := defaults[k]; ok { safe[k] = v }
				}
				result["agents"] = map[string]any{"defaults": safe}
			}
		}

		// Tools config
		if tools, ok := cfg["tools"].(map[string]any); ok {
			safe := map[string]any{}
			for _, k := range []string{"allow_read_paths","allow_write_paths","exec","skills","cron"} {
				if v, ok := tools[k]; ok { safe[k] = v }
			}
			if web, ok := tools["web"].(map[string]any); ok {
				safe["web"] = map[string]any{"enabled": web["enabled"], "provider": web["provider"]}
			}
			result["tools"] = safe
		}

		// Current model config
		curModel := ""
		if a, ok := cfg["agents"].(map[string]any); ok {
			if d, ok := a["defaults"].(map[string]any); ok {
				curModel, _ = d["model_name"].(string)
			}
		}
		if models, ok := cfg["model_list"].([]any); ok {
			for _, m := range models {
				if mm, ok := m.(map[string]any); ok && mm["model_name"] == curModel {
					mc := map[string]any{}
					for _, k := range []string{"model","provider","api_base","rpm",
						"request_timeout","thinking_level","max_tokens_field",
						"tool_schema_transform","streaming","proxy","connect_mode"} {
						if v, ok := mm[k]; ok { mc[k] = v }
					}
					result["current_model"] = mc
					break
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
		return
	}

	// POST: partial update with expanded whitelist
	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Whitelisted fields for agents.defaults patch
	agentFields := map[string]bool{
		"workspace":true,"restrict_to_workspace":true,"allow_read_outside_workspace":true,
		"max_tokens":true,"max_tool_iterations":true,"temperature":true,"context_window":true,
		"max_media_size":true,"steering_mode":true,"summarize_message_threshold":true,
		"summarize_token_percent":true,"max_llm_retries":true,"llm_retry_backoff_secs":true,
		"split_on_marker":true,"context_manager":true,"max_parallel_turns":true,
		"model_fallbacks":true,"image_model":true,"character_prompt":true,
	}
	toolFields := map[string]bool{
		"allow_read_paths":true,"allow_write_paths":true,
	}
	modelFields := map[string]bool{
		"model":true,"provider":true,"api_base":true,"rpm":true,
		"request_timeout":true,"thinking_level":true,"max_tokens_field":true,
		"tool_schema_transform":true,"proxy":true,"connect_mode":true,
	}

	// Apply to agents.defaults
	if section, ok := cfg["agents"].(map[string]any); ok {
		if defaults, ok := section["defaults"].(map[string]any); ok {
			for k, v := range patch {
				if agentFields[k] { defaults[k] = v }
			}
		}
		// Patch current model config
		curModel := ""
		if d, ok := section["defaults"].(map[string]any); ok {
			curModel, _ = d["model_name"].(string)
		}
		if models, ok := cfg["model_list"].([]any); ok {
			for _, m := range models {
				if mm, ok := m.(map[string]any); ok && mm["model_name"] == curModel {
					for k, v := range patch {
						if modelFields[k] { mm[k] = v }
					}
					break
				}
			}
		}
	}
	// Apply to tools
	if tools, ok := cfg["tools"].(map[string]any); ok {
		for k, v := range patch {
			if toolFields[k] { tools[k] = v }
		}
		// Also patch nested exec settings
		if exec, ok := tools["exec"].(map[string]any); ok {
			for k, v := range patch {
				if k == "custom_allow_patterns" || k == "custom_deny_patterns" || k == "enable_deny_patterns" {
					exec[k] = v
				}
			}
		}
	}

	newData, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if err := os.WriteFile(configPath, newData, 0600); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	s.mu.RLock()
	reloadFunc := s.reloadFunc
	s.mu.RUnlock()
	if reloadFunc != nil { _ = reloadFunc() }

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (s *Server) characterClearHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	if r.Method == http.MethodOptions { w.WriteHeader(204); return }

	// GET: return character registry (workspace/characters/_registry.json)
	if r.Method == http.MethodGet {
		s.characterListHandler(w, r)
		return
	}

	// DELETE: remove a character from registry + file
	if r.Method == http.MethodDelete {
		s.characterDeleteHandler(w, r)
		return
	}

	s.mu.RLock()
	configPath := s.configPath
	s.mu.RUnlock()

	// POST: Read body - if it contains character prompt text, activate; if empty, clear
	body, _ := io.ReadAll(r.Body)
	isClear := len(body) == 0 || string(body) == "" || string(body) == "{}"

	// Update config.json
	data, err := os.ReadFile(configPath)
	workspace := "/root/.picoclaw/workspace"
	if err == nil {
		var cfg map[string]any
		json.Unmarshal(data, &cfg)
		if agents, ok := cfg["agents"].(map[string]any); ok {
			if defaults, ok := agents["defaults"].(map[string]any); ok {
				if isClear {
					defaults["character_prompt"] = ""
				} else {
					defaults["character_prompt"] = string(body)
				}
				if ws, ok := defaults["workspace"].(string); ok && ws != "" {
					workspace = ws
				}
			}
		}
		newData, _ := json.MarshalIndent(cfg, "", "    ")
		os.WriteFile(configPath, newData, 0600)
	}
	if isClear {
		// Clear character: write default empty CHARACTER.md, keep SOUL.md intact
		os.WriteFile(filepath.Join(workspace, "CHARACTER.md"), []byte("# Character\n\nNo active character. Default PicoClaw mode.\n"), 0644)
	} else {
		// Activate character: write persona to CHARACTER.md, do NOT touch SOUL.md
		os.WriteFile(filepath.Join(workspace, "CHARACTER.md"), []byte("# Character Role\n\n"+string(body)), 0644)
	}

	// Update characters.json registry for agent-accessible character switching
	charName := r.URL.Query().Get("name")
	promptPreview := string(body)
	if len(promptPreview) > 120 {
		promptPreview = promptPreview[:120]
	}
	updateCharacterRegistry(workspace, charName, string(body), isClear)

	s.mu.RLock()
	reloadFunc := s.reloadFunc
	s.mu.RUnlock()
	if reloadFunc != nil { _ = reloadFunc() }

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
}

// characterListHandler returns the _registry.json as JSON (GET /character/clear)
func (s *Server) characterListHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	configPath := s.configPath
	s.mu.RUnlock()

	workspace := "/root/.picoclaw/workspace"
	data, err := os.ReadFile(configPath)
	if err == nil {
		var cfg map[string]any
		if json.Unmarshal(data, &cfg) == nil {
			if agents, ok := cfg["agents"].(map[string]any); ok {
				if defaults, ok := agents["defaults"].(map[string]any); ok {
					if ws, ok := defaults["workspace"].(string); ok && ws != "" {
						workspace = ws
					}
				}
			}
		}
	}

	registryPath := filepath.Join(workspace, "characters", "_registry.json")
	registryData, err := os.ReadFile(registryPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"characters":  []any{},
			"active_char": "",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(registryData)
}

// characterDeleteHandler deletes a character from the registry + file (DELETE /character/clear?id=xxx)
func (s *Server) characterDeleteHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	configPath := s.configPath
	s.mu.RUnlock()

	charID := r.URL.Query().Get("id")
	if charID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing id"})
		return
	}

	workspace := "/root/.picoclaw/workspace"
	data, err := os.ReadFile(configPath)
	if err == nil {
		var cfg map[string]any
		if json.Unmarshal(data, &cfg) == nil {
			if agents, ok := cfg["agents"].(map[string]any); ok {
				if defaults, ok := agents["defaults"].(map[string]any); ok {
					if ws, ok := defaults["workspace"].(string); ok && ws != "" {
						workspace = ws
					}
				}
			}
		}
	}

	registryPath := filepath.Join(workspace, "characters", "_registry.json")
	type CharEntry struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Preview string `json:"preview"`
	}
	type Registry struct {
		Characters []CharEntry `json:"characters"`
		ActiveChar string      `json:"active_char"`
	}

	var reg Registry
	regData, err := os.ReadFile(registryPath)
	if err == nil {
		json.Unmarshal(regData, &reg)
	}

	// Remove from registry
	var filtered []CharEntry
	for _, c := range reg.Characters {
		if c.ID != charID {
			filtered = append(filtered, c)
		}
	}
	reg.Characters = filtered
	if reg.ActiveChar == charID {
		reg.ActiveChar = ""
		// Also clear the character config
		s.clearCharacterConfig(configPath, workspace)
	}

	// Save registry
	newData, _ := json.MarshalIndent(reg, "", "  ")
	os.WriteFile(registryPath, newData, 0644)

	// Remove character file
	charFile := filepath.Join(workspace, "characters", charID+".md")
	os.Remove(charFile)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// clearCharacterConfig clears the active character from config.json and CHARACTER.md
func (s *Server) clearCharacterConfig(configPath, workspace string) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	var cfg map[string]any
	if json.Unmarshal(data, &cfg) != nil {
		return
	}
	agents, ok := cfg["agents"].(map[string]any)
	if !ok {
		return
	}
	defaults, ok := agents["defaults"].(map[string]any)
	if !ok {
		return
	}
	defaults["character_prompt"] = ""
	newData, _ := json.MarshalIndent(cfg, "", "    ")
	os.WriteFile(configPath, newData, 0600)
	os.WriteFile(filepath.Join(workspace, "CHARACTER.md"), []byte("# Character\n\nNo active character. Default PicoClaw mode.\n"), 0644)
}

func updateCharacterRegistry(workspace, charName, prompt string, isClear bool) {
	registryPath := filepath.Join(workspace, "characters", "_registry.json")
	os.MkdirAll(filepath.Join(workspace, "characters"), 0755)

	type CharEntry struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Preview string `json:"preview"`
	}
	type Registry struct {
		Characters []CharEntry `json:"characters"`
		ActiveChar string      `json:"active_char"`
	}

	var reg Registry
	data, err := os.ReadFile(registryPath)
	if err == nil {
		json.Unmarshal(data, &reg)
	}

	charID := sanitizeID(charName)
	if isClear {
		reg.ActiveChar = ""
	} else if charName != "" {
		// Save character prompt to dedicated file for agent read access
		charFile := filepath.Join(workspace, "characters", charID+".md")
		os.WriteFile(charFile, []byte(prompt), 0644)
		reg.ActiveChar = charID
		// Add or update entry
		found := false
		preview := charName
		for i, c := range reg.Characters {
			if c.ID == charID {
				reg.Characters[i].Preview = preview
				found = true
				break
			}
		}
		if !found {
			reg.Characters = append(reg.Characters, CharEntry{ID: charID, Name: charName, Preview: preview})
		}
	}

	regData, _ := json.MarshalIndent(reg, "", "  ")
	os.WriteFile(registryPath, regData, 0644)
}

func sanitizeID(name string) string {
	id := strings.ToLower(name)
	id = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, id)
	id = strings.Trim(id, "-")
	if id == "" { id = "char" }
	return id
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
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	if r.Method == http.MethodOptions { w.WriteHeader(204); return }
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

// RegisterOnMux registers /health, /ready and /reload handlers onto the given mux.
// This allows the health endpoints to be served by a shared HTTP server.
func (s *Server) RegisterOnMux(mux HandlerMux) {
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/ready", s.readyHandler)
	mux.HandleFunc("/reload", s.reloadHandler)
	mux.HandleFunc("/models", s.modelsHandler)
	mux.HandleFunc("/model/switch", s.modelSwitchHandler)
	mux.HandleFunc("/config", s.configHandler)
	mux.HandleFunc("/character/clear", s.characterClearHandler)
	mux.HandleFunc("/", serveWebUI)
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
