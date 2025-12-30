// Package registry provides agent storage and management.
package registry

import (
	"context"
	"time"

	"github.com/bcrisp4/opamp-control-plane/pkg/models"
)

// Registry defines the interface for agent storage.
type Registry interface {
	// Agent CRUD
	RegisterAgent(ctx context.Context, agent *models.Agent) error
	UpdateAgent(ctx context.Context, agent *models.Agent) error
	GetAgent(ctx context.Context, instanceUID string) (*models.Agent, error)
	ListAgents(ctx context.Context, filter models.AgentFilter) ([]*models.Agent, error)
	DeleteAgent(ctx context.Context, instanceUID string) error

	// Status updates
	UpdateAgentStatus(ctx context.Context, instanceUID string, status models.AgentStatus) error
	UpdateAgentConfigStatus(ctx context.Context, instanceUID string, configHash string, status models.ConfigStatus) error

	// Heartbeat
	RecordHeartbeat(ctx context.Context, instanceUID string) error
	GetStaleAgents(ctx context.Context, threshold time.Duration) ([]*models.Agent, error)

	// Lifecycle
	Close() error
}

// EventType represents the type of registry event.
type EventType string

const (
	EventAgentConnected    EventType = "agent_connected"
	EventAgentDisconnected EventType = "agent_disconnected"
	EventAgentUpdated      EventType = "agent_updated"
	EventConfigApplied     EventType = "config_applied"
	EventConfigFailed      EventType = "config_failed"
)

// Event represents a registry event.
type Event struct {
	Type      EventType
	Agent     *models.Agent
	Timestamp time.Time
}

// EventHandler is a function that handles registry events.
type EventHandler func(Event)

// EventEmitter extends Registry with event capabilities.
type EventEmitter interface {
	Registry
	Subscribe(handler EventHandler) func()
}
