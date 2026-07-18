package health

import (
	"embed"
	"net/http"
	"os"
)

//go:embed webui/index.html
var webUI embed.FS

// serveWebUI serves the web chat UI at the root path.
// Checks PICOCLAW_WEBUI_OVERRIDE env var for hot-reload path, falls back to embedded.
func serveWebUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Check env override for hot-reload without recompiling
	if overridePath := os.Getenv("PICOCLAW_WEBUI_OVERRIDE"); overridePath != "" {
		if data, err := os.ReadFile(overridePath); err == nil {
			w.Write(data)
			return
		}
	}

	data, err := webUI.ReadFile("webui/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}
