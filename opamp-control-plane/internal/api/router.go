package api

import (
	"net/http"
	"strings"
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

// CORSConfig configures CORS behavior.
type CORSConfig struct {
	// AllowedOrigins is a list of origins that are allowed.
	// Use "*" to allow all origins (not recommended for production).
	// If empty, CORS headers are not added.
	AllowedOrigins []string

	// AllowedMethods is a list of allowed HTTP methods.
	// Defaults to GET, POST, PUT, DELETE, OPTIONS if empty.
	AllowedMethods []string

	// AllowedHeaders is a list of allowed headers.
	// Defaults to Content-Type, Authorization if empty.
	AllowedHeaders []string
}

// WithCORS adds CORS headers to responses based on configuration.
func WithCORS(handler http.Handler, cfg CORSConfig) http.Handler {
	// If no origins configured, return handler without CORS
	if len(cfg.AllowedOrigins) == 0 {
		return handler
	}

	methods := cfg.AllowedMethods
	if len(methods) == 0 {
		methods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	}

	headers := cfg.AllowedHeaders
	if len(headers) == 0 {
		headers = []string{"Content-Type", "Authorization"}
	}

	methodsStr := strings.Join(methods, ", ")
	headersStr := strings.Join(headers, ", ")

	// Check if wildcard origin is allowed
	allowAll := false
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if allowAll {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" {
			// Check if origin is in allowed list
			for _, allowed := range cfg.AllowedOrigins {
				if origin == allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					break
				}
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", methodsStr)
		w.Header().Set("Access-Control-Allow-Headers", headersStr)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		handler.ServeHTTP(w, r)
	})
}
