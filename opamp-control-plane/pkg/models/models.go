// Package models contains shared types used across the control plane.
package models

import (
	"time"
)

// AgentStatus represents the connection status of an agent.
type AgentStatus string

const (
	AgentStatusConnected    AgentStatus = "connected"
	AgentStatusDisconnected AgentStatus = "disconnected"
	AgentStatusUnknown      AgentStatus = "unknown"
)

// ConfigStatus represents the status of config application on an agent.
type ConfigStatus string

const (
	ConfigStatusPending ConfigStatus = "pending"
	ConfigStatusApplied ConfigStatus = "applied"
	ConfigStatusFailed  ConfigStatus = "failed"
	ConfigStatusUnknown ConfigStatus = "unknown"
)

// Agent represents a managed telemetry agent.
type Agent struct {
	InstanceUID string `json:"instance_uid"`

	// AgentDescription contains identifying and non-identifying attributes.
	AgentDescription AgentDescription `json:"agent_description"`

	// Labels are used for config selector matching.
	Labels map[string]string `json:"labels"`

	// Status is the current connection status.
	Status AgentStatus `json:"status"`

	// LastSeen is the timestamp of the last heartbeat or message.
	LastSeen time.Time `json:"last_seen"`

	// EffectiveConfigName is the name of the currently assigned config.
	EffectiveConfigName string `json:"effective_config_name,omitempty"`

	// EffectiveConfigHash is the hash of the currently applied config.
	EffectiveConfigHash string `json:"effective_config_hash,omitempty"`

	// ConfigStatus indicates whether the config was successfully applied.
	ConfigStatus ConfigStatus `json:"config_status"`

	// RemoteConfigStatus contains detailed status from the agent.
	RemoteConfigStatus *RemoteConfigStatus `json:"remote_config_status,omitempty"`

	// Capabilities reported by the agent.
	Capabilities uint64 `json:"capabilities,omitempty"`

	// CreatedAt is when the agent was first registered.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the agent was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// AgentDescription contains agent metadata.
type AgentDescription struct {
	IdentifyingAttributes    map[string]string `json:"identifying_attributes,omitempty"`
	NonIdentifyingAttributes map[string]string `json:"non_identifying_attributes,omitempty"`
}

// RemoteConfigStatus contains detailed config application status.
type RemoteConfigStatus struct {
	LastRemoteConfigHash string `json:"last_remote_config_hash,omitempty"`
	Status               string `json:"status,omitempty"`
	ErrorMessage         string `json:"error_message,omitempty"`
}

// AgentFilter contains filter criteria for listing agents.
type AgentFilter struct {
	Status       *AgentStatus      `json:"status,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	ConfigStatus *ConfigStatus     `json:"config_status,omitempty"`
	Limit        int               `json:"limit,omitempty"`
	Offset       int               `json:"offset,omitempty"`
}

// ConfigSelector defines a label-based config assignment rule.
type ConfigSelector struct {
	Name     string        `yaml:"name" json:"name"`
	Match    SelectorMatch `yaml:"match" json:"match"`
	Config   string        `yaml:"config" json:"config"`
	Overlay  string        `yaml:"overlay,omitempty" json:"overlay,omitempty"`
	Priority int           `yaml:"priority,omitempty" json:"priority,omitempty"`
}

// SelectorMatch defines the matching criteria for a selector.
type SelectorMatch struct {
	Labels map[string]string `yaml:"labels" json:"labels"`
}

// SelectorsFile represents the _selectors.yaml file structure.
type SelectorsFile struct {
	Selectors []ConfigSelector `yaml:"selectors" json:"selectors"`
}

// EffectiveConfig represents the resolved configuration for an agent.
type EffectiveConfig struct {
	Name         string `json:"name"`
	Hash         string `json:"hash"`
	Content      []byte `json:"content"`
	SelectorName string `json:"selector_name"`
}

// SyncResult contains the result of a Git sync operation.
type SyncResult struct {
	Status         string `json:"status"`
	Commit         string `json:"commit"`
	ConfigsUpdated int    `json:"configs_updated"`
	AgentsNotified int    `json:"agents_notified"`
	Error          string `json:"error,omitempty"`
}

// HealthStatus represents the health of the control plane.
type HealthStatus struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Checks    map[string]string `json:"checks"`
}
