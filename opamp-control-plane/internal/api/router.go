package api

import (
	"net/http"
)

// NewRouter creates a new HTTP router with all API routes.
func NewRouter(h *Handlers) *http.ServeMux {
	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("GET /health", h.GetHealth)
	mux.HandleFunc("GET /ready", h.GetReady)
	mux.HandleFunc("GET /api/v1/health", h.GetHealth)

	// Agent endpoints
	mux.HandleFunc("GET /api/v1/agents", h.ListAgents)
	mux.HandleFunc("GET /api/v1/agents/{id}", h.GetAgent)
	mux.HandleFunc("GET /api/v1/agents/{id}/config", h.GetAgentConfig)
	mux.HandleFunc("DELETE /api/v1/agents/{id}", h.DeleteAgent)

	// Config endpoints
	mux.HandleFunc("GET /api/v1/selectors", h.GetSelectors)

	// Sync endpoint
	mux.HandleFunc("POST /api/v1/sync", h.TriggerSync)

	return mux
}

// WithLogging wraps a handler with request logging.
func WithLogging(handler http.Handler, logger interface{ Info(string, ...any) }) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
		)
		handler.ServeHTTP(w, r)
	})
}

// WithCORS adds CORS headers to responses.
func WithCORS(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		handler.ServeHTTP(w, r)
	})
}
