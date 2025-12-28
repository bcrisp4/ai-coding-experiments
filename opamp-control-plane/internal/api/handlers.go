// Package api provides the REST API handlers.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/bcrisp4/opamp-control-plane/internal/config"
	"github.com/bcrisp4/opamp-control-plane/internal/gitsync"
	"github.com/bcrisp4/opamp-control-plane/internal/registry"
	"github.com/bcrisp4/opamp-control-plane/pkg/models"
)

// instanceUIDPattern validates instance UID format.
// Allows alphanumeric characters, hyphens, underscores, and periods.
// Must be 1-256 characters long.
var instanceUIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,256}$`)

// Handlers contains the API handler dependencies.
type Handlers struct {
	Registry       registry.Registry
	ConfigResolver *config.Resolver
	GitSyncer      *gitsync.Syncer
	Logger         *slog.Logger
	StartTime      time.Time
}

// ListAgentsResponse is the response for listing agents.
type ListAgentsResponse struct {
	Agents []*models.Agent `json:"agents"`
	Count  int             `json:"count"`
}

// ListAgents handles GET /api/v1/agents
func (h *Handlers) ListAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filter := models.AgentFilter{}

	// Parse query parameters
	if status := r.URL.Query().Get("status"); status != "" {
		s := models.AgentStatus(status)
		filter.Status = &s
	}
	if configStatus := r.URL.Query().Get("config_status"); configStatus != "" {
		cs := models.ConfigStatus(configStatus)
		filter.ConfigStatus = &cs
	}

	agents, err := h.Registry.ListAgents(ctx, filter)
	if err != nil {
		h.Logger.Error("failed to list agents", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}

	writeJSON(w, http.StatusOK, ListAgentsResponse{
		Agents: agents,
		Count:  len(agents),
	})
}

// GetAgent handles GET /api/v1/agents/{id}
func (h *Handlers) GetAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceUID := r.PathValue("id")

	if !validateInstanceUID(instanceUID) {
		writeError(w, http.StatusBadRequest, "invalid agent id format")
		return
	}

	agent, err := h.Registry.GetAgent(ctx, instanceUID)
	if err != nil {
		h.Logger.Error("failed to get agent", "instance_uid", instanceUID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get agent")
		return
	}

	if agent == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	writeJSON(w, http.StatusOK, agent)
}

// GetAgentConfig handles GET /api/v1/agents/{id}/config
func (h *Handlers) GetAgentConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceUID := r.PathValue("id")

	if !validateInstanceUID(instanceUID) {
		writeError(w, http.StatusBadRequest, "invalid agent id format")
		return
	}

	agent, err := h.Registry.GetAgent(ctx, instanceUID)
	if err != nil {
		h.Logger.Error("failed to get agent", "instance_uid", instanceUID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get agent")
		return
	}

	if agent == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	effectiveConfig, err := h.ConfigResolver.Resolve(agent)
	if err != nil {
		h.Logger.Error("failed to resolve config", "instance_uid", instanceUID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to resolve config")
		return
	}

	if effectiveConfig == nil {
		writeError(w, http.StatusNotFound, "no config available")
		return
	}

	// Return raw YAML if Accept header requests it
	if r.Header.Get("Accept") == "application/x-yaml" || r.Header.Get("Accept") == "text/yaml" {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write(effectiveConfig.Content)
		return
	}

	writeJSON(w, http.StatusOK, effectiveConfig)
}

// TriggerSync handles POST /api/v1/sync
func (h *Handlers) TriggerSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	commit, err := h.GitSyncer.Sync(ctx)
	if err != nil {
		h.Logger.Error("sync failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, models.SyncResult{
			Status: "failed",
			Error:  err.Error(),
		})
		return
	}

	// Reload configs
	if err := h.ConfigResolver.LoadConfigs(); err != nil {
		h.Logger.Error("failed to reload configs", "error", err)
		writeJSON(w, http.StatusInternalServerError, models.SyncResult{
			Status: "failed",
			Commit: commit,
			Error:  err.Error(),
		})
		return
	}

	// Count agents that would be updated
	agents, err := h.Registry.ListAgents(ctx, models.AgentFilter{})
	if err != nil {
		h.Logger.Warn("failed to count agents for sync response", "error", err)
	}

	writeJSON(w, http.StatusOK, models.SyncResult{
		Status:         "synced",
		Commit:         commit,
		AgentsNotified: len(agents),
	})
}

// GetHealth handles GET /health and GET /api/v1/health
func (h *Handlers) GetHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	checks := make(map[string]string)

	// Check database
	if _, err := h.Registry.ListAgents(ctx, models.AgentFilter{Limit: 1}); err != nil {
		checks["database"] = "unhealthy: " + err.Error()
	} else {
		checks["database"] = "healthy"
	}

	// Check git sync
	if h.GitSyncer != nil {
		if commit := h.GitSyncer.GetLastCommit(); commit != "" {
			checks["git_sync"] = "healthy (commit: " + commit[:8] + ")"
		} else {
			checks["git_sync"] = "pending"
		}
	}

	// Determine overall status
	status := "healthy"
	for _, v := range checks {
		if v != "healthy" && !isHealthyStatus(v) {
			status = "unhealthy"
			break
		}
	}

	health := models.HealthStatus{
		Status:    status,
		Timestamp: time.Now(),
		Checks:    checks,
	}

	statusCode := http.StatusOK
	if status != "healthy" {
		statusCode = http.StatusServiceUnavailable
	}

	writeJSON(w, statusCode, health)
}

// GetReady handles GET /ready
func (h *Handlers) GetReady(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check if we can access the database
	if _, err := h.Registry.ListAgents(ctx, models.AgentFilter{Limit: 1}); err != nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

// GetSelectors handles GET /api/v1/selectors
func (h *Handlers) GetSelectors(w http.ResponseWriter, r *http.Request) {
	selectors := h.ConfigResolver.GetSelectors()
	writeJSON(w, http.StatusOK, map[string]any{
		"selectors": selectors,
		"count":     len(selectors),
	})
}

// DeleteAgent handles DELETE /api/v1/agents/{id}
func (h *Handlers) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceUID := r.PathValue("id")

	if !validateInstanceUID(instanceUID) {
		writeError(w, http.StatusBadRequest, "invalid agent id format")
		return
	}

	if err := h.Registry.DeleteAgent(ctx, instanceUID); err != nil {
		h.Logger.Error("failed to delete agent", "instance_uid", instanceUID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete agent")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func isHealthyStatus(s string) bool {
	return len(s) > 7 && s[:7] == "healthy"
}

// validateInstanceUID validates the instance UID format.
func validateInstanceUID(id string) bool {
	if id == "" {
		return false
	}
	return instanceUIDPattern.MatchString(id)
}

// ConfigPusher is used to push configs to agents.
type ConfigPusher interface {
	PushConfigToAgent(ctx context.Context, instanceUID string) error
	PushConfigToAll(ctx context.Context) error
}
