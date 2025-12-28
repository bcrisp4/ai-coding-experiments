// Package opamp provides the OpAMP server implementation.
package opamp

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/open-telemetry/opamp-go/server"
	"github.com/open-telemetry/opamp-go/server/types"

	"github.com/bcrisp4/opamp-control-plane/internal/registry"
	"github.com/bcrisp4/opamp-control-plane/pkg/models"
)

// ConfigProvider is called to get the effective config for an agent.
type ConfigProvider func(agent *models.Agent) (*models.EffectiveConfig, error)

// Server wraps the OpAMP server and handles agent lifecycle.
type Server struct {
	opampServer  server.OpAMPServer
	registry     registry.Registry
	configProvider ConfigProvider
	logger       *slog.Logger

	// connections tracks active WebSocket connections by instance UID
	connections sync.Map
}

// ServerConfig contains configuration for the OpAMP server.
type ServerConfig struct {
	ListenEndpoint string
	Registry       registry.Registry
	ConfigProvider ConfigProvider
	Logger         *slog.Logger
}

// NewServer creates a new OpAMP server.
func NewServer(cfg ServerConfig) (*Server, error) {
	s := &Server{
		registry:       cfg.Registry,
		configProvider: cfg.ConfigProvider,
		logger:         cfg.Logger,
	}

	opampServer := server.New(s.logger)
	s.opampServer = opampServer

	return s, nil
}

// Handler returns the HTTP handler for the OpAMP WebSocket endpoint.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := s.opampServer.Attach(types.ConnectionCallbacksStruct{
			OnConnectedFunc: s.onConnected,
			OnMessageFunc:   s.onMessage,
			OnConnectionCloseFunc: s.onConnectionClose,
		})
		if err != nil {
			s.logger.Error("failed to attach connection", "error", err)
			http.Error(w, "failed to attach connection", http.StatusInternalServerError)
			return
		}
		conn.Connection(w, r)
	})
}

// Start starts the OpAMP server on the given address.
func (s *Server) Start(ctx context.Context, addr string) error {
	settings := server.StartSettings{
		Settings: server.Settings{
			Callbacks: types.CallbacksStruct{
				OnConnectingFunc: func(request *http.Request) types.ConnectionResponse {
					return types.ConnectionResponse{Accept: true}
				},
			},
		},
		ListenEndpoint: addr,
	}

	return s.opampServer.Start(settings)
}

// Stop stops the OpAMP server.
func (s *Server) Stop(ctx context.Context) error {
	s.opampServer.Stop(ctx)
	return nil
}

// onConnected is called when a new agent connects.
func (s *Server) onConnected(ctx context.Context, conn types.Connection, msg *protobufs.AgentToServer) {
	instanceUID := string(msg.InstanceUid)
	s.logger.Info("agent connected", "instance_uid", instanceUID)

	// Store the connection
	s.connections.Store(instanceUID, conn)

	// Build agent from the message
	agent := s.agentFromMessage(msg)
	agent.Status = models.AgentStatusConnected

	// Register or update the agent
	if err := s.registry.RegisterAgent(ctx, agent); err != nil {
		s.logger.Error("failed to register agent", "instance_uid", instanceUID, "error", err)
	}

	// Send current config to the agent
	s.sendConfigToAgent(ctx, conn, agent)
}

// onMessage is called when a message is received from an agent.
func (s *Server) onMessage(ctx context.Context, conn types.Connection, msg *protobufs.AgentToServer) *protobufs.ServerToAgent {
	instanceUID := string(msg.InstanceUid)

	// Record heartbeat
	if err := s.registry.RecordHeartbeat(ctx, instanceUID); err != nil {
		s.logger.Debug("failed to record heartbeat", "instance_uid", instanceUID, "error", err)
	}

	// Handle status report
	if msg.StatusReport != nil {
		s.handleStatusReport(ctx, instanceUID, msg.StatusReport)
	}

	// Handle remote config status
	if msg.RemoteConfigStatus != nil {
		s.handleRemoteConfigStatus(ctx, instanceUID, msg.RemoteConfigStatus)
	}

	// Build response
	response := &protobufs.ServerToAgent{
		InstanceUid: msg.InstanceUid,
	}

	// Check if agent needs config update
	agent, err := s.registry.GetAgent(ctx, instanceUID)
	if err != nil {
		s.logger.Error("failed to get agent", "instance_uid", instanceUID, "error", err)
		return response
	}

	if agent != nil && s.configProvider != nil {
		effectiveConfig, err := s.configProvider(agent)
		if err != nil {
			s.logger.Error("failed to get effective config", "instance_uid", instanceUID, "error", err)
			return response
		}

		if effectiveConfig != nil && effectiveConfig.Hash != agent.EffectiveConfigHash {
			response.RemoteConfig = &protobufs.AgentRemoteConfig{
				Config: &protobufs.AgentConfigMap{
					ConfigMap: map[string]*protobufs.AgentConfigFile{
						"collector.yaml": {
							Body:        effectiveConfig.Content,
							ContentType: "text/yaml",
						},
					},
				},
				ConfigHash: []byte(effectiveConfig.Hash),
			}

			// Update the expected config hash
			s.registry.UpdateAgentConfigStatus(ctx, instanceUID, effectiveConfig.Hash, models.ConfigStatusPending)
		}
	}

	return response
}

// onConnectionClose is called when an agent disconnects.
func (s *Server) onConnectionClose(conn types.Connection) {
	// Find the instance UID for this connection
	var instanceUID string
	s.connections.Range(func(key, value any) bool {
		if value == conn {
			instanceUID = key.(string)
			return false
		}
		return true
	})

	if instanceUID != "" {
		s.logger.Info("agent disconnected", "instance_uid", instanceUID)
		s.connections.Delete(instanceUID)

		ctx := context.Background()
		if err := s.registry.UpdateAgentStatus(ctx, instanceUID, models.AgentStatusDisconnected); err != nil {
			s.logger.Error("failed to update agent status", "instance_uid", instanceUID, "error", err)
		}
	}
}

// handleStatusReport processes an agent's status report.
func (s *Server) handleStatusReport(ctx context.Context, instanceUID string, status *protobufs.StatusReport) {
	agent, err := s.registry.GetAgent(ctx, instanceUID)
	if err != nil || agent == nil {
		return
	}

	// Update agent description if provided
	if status.AgentDescription != nil {
		agent.AgentDescription = models.AgentDescription{
			IdentifyingAttributes:    protoToStringMap(status.AgentDescription.IdentifyingAttributes),
			NonIdentifyingAttributes: protoToStringMap(status.AgentDescription.NonIdentifyingAttributes),
		}

		// Extract labels from identifying attributes
		if agent.Labels == nil {
			agent.Labels = make(map[string]string)
		}
		for k, v := range agent.AgentDescription.IdentifyingAttributes {
			agent.Labels[k] = v
		}
	}

	if err := s.registry.UpdateAgent(ctx, agent); err != nil {
		s.logger.Error("failed to update agent from status report", "instance_uid", instanceUID, "error", err)
	}
}

// handleRemoteConfigStatus processes the config status from an agent.
func (s *Server) handleRemoteConfigStatus(ctx context.Context, instanceUID string, status *protobufs.RemoteConfigStatus) {
	configHash := string(status.LastRemoteConfigHash)

	var configStatus models.ConfigStatus
	switch status.Status {
	case protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED:
		configStatus = models.ConfigStatusApplied
		s.logger.Info("agent applied config", "instance_uid", instanceUID, "hash", configHash)
	case protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLYING:
		configStatus = models.ConfigStatusPending
	case protobufs.RemoteConfigStatuses_RemoteConfigStatuses_FAILED:
		configStatus = models.ConfigStatusFailed
		s.logger.Warn("agent failed to apply config",
			"instance_uid", instanceUID,
			"hash", configHash,
			"error", status.ErrorMessage,
		)
	default:
		configStatus = models.ConfigStatusUnknown
	}

	if err := s.registry.UpdateAgentConfigStatus(ctx, instanceUID, configHash, configStatus); err != nil {
		s.logger.Error("failed to update config status", "instance_uid", instanceUID, "error", err)
	}
}

// sendConfigToAgent sends the effective config to an agent.
func (s *Server) sendConfigToAgent(ctx context.Context, conn types.Connection, agent *models.Agent) {
	if s.configProvider == nil {
		return
	}

	effectiveConfig, err := s.configProvider(agent)
	if err != nil {
		s.logger.Error("failed to get effective config", "instance_uid", agent.InstanceUID, "error", err)
		return
	}

	if effectiveConfig == nil {
		s.logger.Debug("no effective config for agent", "instance_uid", agent.InstanceUID)
		return
	}

	msg := &protobufs.ServerToAgent{
		InstanceUid: []byte(agent.InstanceUID),
		RemoteConfig: &protobufs.AgentRemoteConfig{
			Config: &protobufs.AgentConfigMap{
				ConfigMap: map[string]*protobufs.AgentConfigFile{
					"collector.yaml": {
						Body:        effectiveConfig.Content,
						ContentType: "text/yaml",
					},
				},
			},
			ConfigHash: []byte(effectiveConfig.Hash),
		},
	}

	if err := conn.Send(ctx, msg); err != nil {
		s.logger.Error("failed to send config to agent", "instance_uid", agent.InstanceUID, "error", err)
		return
	}

	s.registry.UpdateAgentConfigStatus(ctx, agent.InstanceUID, effectiveConfig.Hash, models.ConfigStatusPending)
	s.logger.Info("sent config to agent",
		"instance_uid", agent.InstanceUID,
		"config_name", effectiveConfig.Name,
		"hash", effectiveConfig.Hash,
	)
}

// PushConfigToAgent pushes a new config to a specific agent.
func (s *Server) PushConfigToAgent(ctx context.Context, instanceUID string) error {
	agent, err := s.registry.GetAgent(ctx, instanceUID)
	if err != nil {
		return err
	}
	if agent == nil {
		return nil
	}

	connVal, ok := s.connections.Load(instanceUID)
	if !ok {
		return nil // Agent not connected
	}

	conn := connVal.(types.Connection)
	s.sendConfigToAgent(ctx, conn, agent)
	return nil
}

// PushConfigToAll pushes configs to all connected agents.
func (s *Server) PushConfigToAll(ctx context.Context) error {
	s.connections.Range(func(key, value any) bool {
		instanceUID := key.(string)
		conn := value.(types.Connection)

		agent, err := s.registry.GetAgent(ctx, instanceUID)
		if err != nil {
			s.logger.Error("failed to get agent", "instance_uid", instanceUID, "error", err)
			return true
		}

		if agent != nil {
			s.sendConfigToAgent(ctx, conn, agent)
		}
		return true
	})

	return nil
}

// agentFromMessage creates an Agent model from an OpAMP message.
func (s *Server) agentFromMessage(msg *protobufs.AgentToServer) *models.Agent {
	agent := &models.Agent{
		InstanceUID: string(msg.InstanceUid),
		Labels:      make(map[string]string),
	}

	if msg.StatusReport != nil && msg.StatusReport.AgentDescription != nil {
		desc := msg.StatusReport.AgentDescription
		agent.AgentDescription = models.AgentDescription{
			IdentifyingAttributes:    protoToStringMap(desc.IdentifyingAttributes),
			NonIdentifyingAttributes: protoToStringMap(desc.NonIdentifyingAttributes),
		}

		// Use identifying attributes as labels
		for k, v := range agent.AgentDescription.IdentifyingAttributes {
			agent.Labels[k] = v
		}
	}

	if msg.Capabilities != 0 {
		agent.Capabilities = msg.Capabilities
	}

	return agent
}

// protoToStringMap converts protobuf key-value pairs to a string map.
func protoToStringMap(kvs []*protobufs.KeyValue) map[string]string {
	result := make(map[string]string)
	for _, kv := range kvs {
		if kv.Value != nil {
			if sv := kv.Value.GetStringValue(); sv != "" {
				result[kv.Key] = sv
			}
		}
	}
	return result
}
