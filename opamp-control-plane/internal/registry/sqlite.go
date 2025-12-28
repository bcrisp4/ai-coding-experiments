package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bcrisp4/opamp-control-plane/pkg/models"
	_ "modernc.org/sqlite"
)

// SQLiteRegistry implements Registry using SQLite.
type SQLiteRegistry struct {
	db            *sql.DB
	logger        *slog.Logger
	handlers      map[int]EventHandler
	nextHandlerID int
	mu            sync.RWMutex
}

// NewSQLiteRegistry creates a new SQLite-backed registry.
func NewSQLiteRegistry(dbPath string, logger *slog.Logger) (*SQLiteRegistry, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrent access
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	r := &SQLiteRegistry{
		db:       db,
		logger:   logger,
		handlers: make(map[int]EventHandler),
	}

	if err := r.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return r, nil
}

func (r *SQLiteRegistry) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS agents (
		instance_uid TEXT PRIMARY KEY,
		agent_description TEXT,
		labels TEXT,
		status TEXT NOT NULL DEFAULT 'unknown',
		last_seen DATETIME,
		effective_config_name TEXT,
		effective_config_hash TEXT,
		config_status TEXT NOT NULL DEFAULT 'unknown',
		remote_config_status TEXT,
		capabilities INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status);
	CREATE INDEX IF NOT EXISTS idx_agents_last_seen ON agents(last_seen);
	CREATE INDEX IF NOT EXISTS idx_agents_config_status ON agents(config_status);
	`

	_, err := r.db.Exec(schema)
	return err
}

// RegisterAgent registers a new agent or updates an existing one.
func (r *SQLiteRegistry) RegisterAgent(ctx context.Context, agent *models.Agent) error {
	descJSON, err := json.Marshal(agent.AgentDescription)
	if err != nil {
		return fmt.Errorf("failed to marshal agent description: %w", err)
	}

	labelsJSON, err := json.Marshal(agent.Labels)
	if err != nil {
		return fmt.Errorf("failed to marshal labels: %w", err)
	}

	var remoteConfigStatusJSON []byte
	if agent.RemoteConfigStatus != nil {
		remoteConfigStatusJSON, err = json.Marshal(agent.RemoteConfigStatus)
		if err != nil {
			return fmt.Errorf("failed to marshal remote config status: %w", err)
		}
	}

	now := time.Now()
	query := `
	INSERT INTO agents (
		instance_uid, agent_description, labels, status, last_seen,
		effective_config_name, effective_config_hash, config_status,
		remote_config_status, capabilities, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(instance_uid) DO UPDATE SET
		agent_description = excluded.agent_description,
		labels = excluded.labels,
		status = excluded.status,
		last_seen = excluded.last_seen,
		capabilities = excluded.capabilities,
		updated_at = excluded.updated_at
	`

	_, err = r.db.ExecContext(ctx, query,
		agent.InstanceUID,
		string(descJSON),
		string(labelsJSON),
		agent.Status,
		now,
		agent.EffectiveConfigName,
		agent.EffectiveConfigHash,
		agent.ConfigStatus,
		string(remoteConfigStatusJSON),
		agent.Capabilities,
		now,
		now,
	)

	if err != nil {
		return fmt.Errorf("failed to register agent: %w", err)
	}

	r.emit(Event{
		Type:      EventAgentConnected,
		Agent:     agent,
		Timestamp: now,
	})

	return nil
}

// UpdateAgent updates an existing agent.
func (r *SQLiteRegistry) UpdateAgent(ctx context.Context, agent *models.Agent) error {
	descJSON, err := json.Marshal(agent.AgentDescription)
	if err != nil {
		return fmt.Errorf("failed to marshal agent description: %w", err)
	}

	labelsJSON, err := json.Marshal(agent.Labels)
	if err != nil {
		return fmt.Errorf("failed to marshal labels: %w", err)
	}

	var remoteConfigStatusJSON []byte
	if agent.RemoteConfigStatus != nil {
		remoteConfigStatusJSON, err = json.Marshal(agent.RemoteConfigStatus)
		if err != nil {
			return fmt.Errorf("failed to marshal remote config status: %w", err)
		}
	}

	now := time.Now()
	query := `
	UPDATE agents SET
		agent_description = ?,
		labels = ?,
		status = ?,
		last_seen = ?,
		effective_config_name = ?,
		effective_config_hash = ?,
		config_status = ?,
		remote_config_status = ?,
		capabilities = ?,
		updated_at = ?
	WHERE instance_uid = ?
	`

	result, err := r.db.ExecContext(ctx, query,
		string(descJSON),
		string(labelsJSON),
		agent.Status,
		agent.LastSeen,
		agent.EffectiveConfigName,
		agent.EffectiveConfigHash,
		agent.ConfigStatus,
		string(remoteConfigStatusJSON),
		agent.Capabilities,
		now,
		agent.InstanceUID,
	)

	if err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("agent not found: %s", agent.InstanceUID)
	}

	r.emit(Event{
		Type:      EventAgentUpdated,
		Agent:     agent,
		Timestamp: now,
	})

	return nil
}

// GetAgent retrieves an agent by instance UID.
func (r *SQLiteRegistry) GetAgent(ctx context.Context, instanceUID string) (*models.Agent, error) {
	query := `
	SELECT instance_uid, agent_description, labels, status, last_seen,
		effective_config_name, effective_config_hash, config_status,
		remote_config_status, capabilities, created_at, updated_at
	FROM agents WHERE instance_uid = ?
	`

	var agent models.Agent
	var descJSON, labelsJSON, remoteConfigStatusJSON sql.NullString
	var lastSeen sql.NullTime
	var effectiveConfigName, effectiveConfigHash sql.NullString

	err := r.db.QueryRowContext(ctx, query, instanceUID).Scan(
		&agent.InstanceUID,
		&descJSON,
		&labelsJSON,
		&agent.Status,
		&lastSeen,
		&effectiveConfigName,
		&effectiveConfigHash,
		&agent.ConfigStatus,
		&remoteConfigStatusJSON,
		&agent.Capabilities,
		&agent.CreatedAt,
		&agent.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	if descJSON.Valid {
		if err := json.Unmarshal([]byte(descJSON.String), &agent.AgentDescription); err != nil {
			r.logger.Warn("failed to unmarshal agent description", "error", err)
		}
	}

	if labelsJSON.Valid {
		if err := json.Unmarshal([]byte(labelsJSON.String), &agent.Labels); err != nil {
			r.logger.Warn("failed to unmarshal labels", "error", err)
		}
	}

	if remoteConfigStatusJSON.Valid && remoteConfigStatusJSON.String != "" {
		agent.RemoteConfigStatus = &models.RemoteConfigStatus{}
		if err := json.Unmarshal([]byte(remoteConfigStatusJSON.String), agent.RemoteConfigStatus); err != nil {
			r.logger.Warn("failed to unmarshal remote config status", "error", err)
		}
	}

	if lastSeen.Valid {
		agent.LastSeen = lastSeen.Time
	}
	if effectiveConfigName.Valid {
		agent.EffectiveConfigName = effectiveConfigName.String
	}
	if effectiveConfigHash.Valid {
		agent.EffectiveConfigHash = effectiveConfigHash.String
	}

	return &agent, nil
}

// ListAgents retrieves agents matching the filter criteria.
func (r *SQLiteRegistry) ListAgents(ctx context.Context, filter models.AgentFilter) ([]*models.Agent, error) {
	query := `
	SELECT instance_uid, agent_description, labels, status, last_seen,
		effective_config_name, effective_config_hash, config_status,
		remote_config_status, capabilities, created_at, updated_at
	FROM agents WHERE 1=1
	`
	args := []any{}

	if filter.Status != nil {
		query += " AND status = ?"
		args = append(args, *filter.Status)
	}

	if filter.ConfigStatus != nil {
		query += " AND config_status = ?"
		args = append(args, *filter.ConfigStatus)
	}

	query += " ORDER BY last_seen DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}
	defer rows.Close()

	var agents []*models.Agent
	for rows.Next() {
		var agent models.Agent
		var descJSON, labelsJSON, remoteConfigStatusJSON sql.NullString
		var lastSeen sql.NullTime
		var effectiveConfigName, effectiveConfigHash sql.NullString

		err := rows.Scan(
			&agent.InstanceUID,
			&descJSON,
			&labelsJSON,
			&agent.Status,
			&lastSeen,
			&effectiveConfigName,
			&effectiveConfigHash,
			&agent.ConfigStatus,
			&remoteConfigStatusJSON,
			&agent.Capabilities,
			&agent.CreatedAt,
			&agent.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}

		if descJSON.Valid {
			if err := json.Unmarshal([]byte(descJSON.String), &agent.AgentDescription); err != nil {
				r.logger.Warn("failed to unmarshal agent description in list",
					"instance_uid", agent.InstanceUID, "error", err)
			}
		}
		if labelsJSON.Valid {
			if err := json.Unmarshal([]byte(labelsJSON.String), &agent.Labels); err != nil {
				r.logger.Warn("failed to unmarshal labels in list",
					"instance_uid", agent.InstanceUID, "error", err)
			}
		}
		if remoteConfigStatusJSON.Valid && remoteConfigStatusJSON.String != "" {
			agent.RemoteConfigStatus = &models.RemoteConfigStatus{}
			if err := json.Unmarshal([]byte(remoteConfigStatusJSON.String), agent.RemoteConfigStatus); err != nil {
				r.logger.Warn("failed to unmarshal remote config status in list",
					"instance_uid", agent.InstanceUID, "error", err)
			}
		}
		if lastSeen.Valid {
			agent.LastSeen = lastSeen.Time
		}
		if effectiveConfigName.Valid {
			agent.EffectiveConfigName = effectiveConfigName.String
		}
		if effectiveConfigHash.Valid {
			agent.EffectiveConfigHash = effectiveConfigHash.String
		}

		// Apply label filter in memory (could be optimized with JSON functions)
		if len(filter.Labels) > 0 {
			match := true
			for k, v := range filter.Labels {
				if agent.Labels[k] != v {
					match = false
					break
				}
			}
			if !match {
				continue
			}
		}

		agents = append(agents, &agent)
	}

	return agents, rows.Err()
}

// DeleteAgent removes an agent from the registry.
func (r *SQLiteRegistry) DeleteAgent(ctx context.Context, instanceUID string) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM agents WHERE instance_uid = ?", instanceUID)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("agent not found: %s", instanceUID)
	}

	return nil
}

// UpdateAgentStatus updates the connection status of an agent.
func (r *SQLiteRegistry) UpdateAgentStatus(ctx context.Context, instanceUID string, status models.AgentStatus) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		"UPDATE agents SET status = ?, last_seen = ?, updated_at = ? WHERE instance_uid = ?",
		status, now, now, instanceUID,
	)
	if err != nil {
		return fmt.Errorf("failed to update agent status: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("agent not found: %s", instanceUID)
	}

	agent, _ := r.GetAgent(ctx, instanceUID)
	if agent != nil {
		eventType := EventAgentUpdated
		if status == models.AgentStatusDisconnected {
			eventType = EventAgentDisconnected
		} else if status == models.AgentStatusConnected {
			eventType = EventAgentConnected
		}
		r.emit(Event{
			Type:      eventType,
			Agent:     agent,
			Timestamp: now,
		})
	}

	return nil
}

// UpdateAgentConfigStatus updates the config status of an agent.
func (r *SQLiteRegistry) UpdateAgentConfigStatus(ctx context.Context, instanceUID string, configHash string, status models.ConfigStatus) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		"UPDATE agents SET effective_config_hash = ?, config_status = ?, updated_at = ? WHERE instance_uid = ?",
		configHash, status, now, instanceUID,
	)
	if err != nil {
		return fmt.Errorf("failed to update agent config status: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("agent not found: %s", instanceUID)
	}

	agent, _ := r.GetAgent(ctx, instanceUID)
	if agent != nil {
		eventType := EventConfigApplied
		if status == models.ConfigStatusFailed {
			eventType = EventConfigFailed
		}
		r.emit(Event{
			Type:      eventType,
			Agent:     agent,
			Timestamp: now,
		})
	}

	return nil
}

// RecordHeartbeat updates the last_seen timestamp for an agent.
func (r *SQLiteRegistry) RecordHeartbeat(ctx context.Context, instanceUID string) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		"UPDATE agents SET last_seen = ?, updated_at = ? WHERE instance_uid = ?",
		now, now, instanceUID,
	)
	return err
}

// GetStaleAgents returns agents that haven't sent a heartbeat within the threshold.
func (r *SQLiteRegistry) GetStaleAgents(ctx context.Context, threshold time.Duration) ([]*models.Agent, error) {
	cutoff := time.Now().Add(-threshold)

	filter := models.AgentFilter{}
	agents, err := r.ListAgents(ctx, filter)
	if err != nil {
		return nil, err
	}

	var stale []*models.Agent
	for _, agent := range agents {
		if agent.LastSeen.Before(cutoff) && agent.Status == models.AgentStatusConnected {
			stale = append(stale, agent)
		}
	}

	return stale, nil
}

// Subscribe registers an event handler and returns an unsubscribe function.
func (r *SQLiteRegistry) Subscribe(handler EventHandler) func() {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := r.nextHandlerID
	r.nextHandlerID++
	r.handlers[id] = handler

	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.handlers, id)
	}
}

func (r *SQLiteRegistry) emit(event Event) {
	r.mu.RLock()
	handlers := make([]EventHandler, 0, len(r.handlers))
	for _, h := range r.handlers {
		handlers = append(handlers, h)
	}
	r.mu.RUnlock()

	for _, h := range handlers {
		go h(event)
	}
}

// Close closes the database connection.
func (r *SQLiteRegistry) Close() error {
	return r.db.Close()
}
