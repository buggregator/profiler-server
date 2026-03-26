package profiler

import (
	"encoding/json"
	"io"
	"net/http"

	"go.uber.org/zap"
)

// handleHTTP processes incoming XHProf profile HTTP requests
func (p *Plugin) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit request body size
	body := http.MaxBytesReader(w, r.Body, p.cfg.MaxRequestSize)
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		p.log.Error("failed to read request body", zap.Error(err))
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Parse incoming profile
	var incoming IncomingProfile
	if err := json.Unmarshal(data, &incoming); err != nil {
		p.log.Error("failed to parse profile JSON", zap.Error(err))
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if len(incoming.Profile) == 0 {
		http.Error(w, "profile field is required", http.StatusUnprocessableEntity)
		return
	}
	if incoming.AppName == "" {
		http.Error(w, "app_name field is required", http.StatusUnprocessableEntity)
		return
	}
	if incoming.Hostname == "" {
		http.Error(w, "hostname field is required", http.StatusUnprocessableEntity)
		return
	}

	// Process the profile (heavy computation in Go)
	event := Process(&incoming)

	// Push to Jobs pipeline
	if err := p.pushToJobs(event); err != nil {
		p.log.Error("failed to push profile to jobs",
			zap.String("uuid", event.UUID),
			zap.Error(err),
		)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	p.log.Debug("profile processed and pushed",
		zap.String("uuid", event.UUID),
		zap.String("app_name", event.AppName),
		zap.Int("edges", event.TotalEdges),
	)

	w.WriteHeader(http.StatusOK)
}
