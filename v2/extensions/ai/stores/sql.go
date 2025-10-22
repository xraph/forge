package stores

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/ai"
)

// SQLAgentStore provides SQL database storage for agents
// Works with any SQL database using a simple schema with JSONB column
type SQLAgentStore struct {
	db     *sql.DB
	table  string
	logger forge.Logger
}

// NewSQLAgentStore creates a new SQL agent store
func NewSQLAgentStore(db *sql.DB, table string, logger forge.Logger) *SQLAgentStore {
	return &SQLAgentStore{
		db:     db,
		table:  table,
		logger: logger,
	}
}

// Create creates a new agent
func (s *SQLAgentStore) Create(ctx context.Context, agent *ai.AgentDefinition) error {
	// Serialize agent to JSON (no fixed schema!)
	data, err := json.Marshal(agent)
	if err != nil {
		return fmt.Errorf("failed to marshal agent: %w", err)
	}

	now := time.Now()
	agent.CreatedAt = now
	agent.UpdatedAt = now

	query := fmt.Sprintf(`
		INSERT INTO %s (id, name, type, data, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, s.table)

	_, err = s.db.ExecContext(ctx, query,
		agent.ID,
		agent.Name,
		agent.Type,
		data,
		agent.CreatedAt,
		agent.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	if s.logger != nil {
		s.logger.Info("agent created in SQL store", forge.F("agent_id", agent.ID))
	}

	return nil
}

// Get retrieves an agent by ID
func (s *SQLAgentStore) Get(ctx context.Context, id string) (*ai.AgentDefinition, error) {
	query := fmt.Sprintf(`
		SELECT data FROM %s WHERE id = $1
	`, s.table)

	var data []byte
	err := s.db.QueryRowContext(ctx, query, id).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	var agent ai.AgentDefinition
	if err := json.Unmarshal(data, &agent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent: %w", err)
	}

	return &agent, nil
}

// Update updates an existing agent
func (s *SQLAgentStore) Update(ctx context.Context, agent *ai.AgentDefinition) error {
	agent.UpdatedAt = time.Now()

	data, err := json.Marshal(agent)
	if err != nil {
		return fmt.Errorf("failed to marshal agent: %w", err)
	}

	query := fmt.Sprintf(`
		UPDATE %s SET name = $1, type = $2, data = $3, updated_at = $4
		WHERE id = $5
	`, s.table)

	result, err := s.db.ExecContext(ctx, query,
		agent.Name,
		agent.Type,
		data,
		agent.UpdatedAt,
		agent.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("agent %s not found", agent.ID)
	}

	if s.logger != nil {
		s.logger.Info("agent updated in SQL store", forge.F("agent_id", agent.ID))
	}

	return nil
}

// Delete deletes an agent by ID
func (s *SQLAgentStore) Delete(ctx context.Context, id string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, s.table)

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("agent %s not found", id)
	}

	if s.logger != nil {
		s.logger.Info("agent deleted from SQL store", forge.F("agent_id", id))
	}

	return nil
}

// List lists agents with optional filters
func (s *SQLAgentStore) List(ctx context.Context, filter ai.AgentFilter) ([]*ai.AgentDefinition, error) {
	query := fmt.Sprintf(`SELECT data FROM %s WHERE 1=1`, s.table)
	args := []interface{}{}
	argIndex := 1

	// Apply filters
	if filter.Type != "" {
		query += fmt.Sprintf(" AND type = $%d", argIndex)
		args = append(args, filter.Type)
		argIndex++
	}

	if filter.CreatedBy != "" {
		query += fmt.Sprintf(" AND data->>'created_by' = $%d", argIndex)
		args = append(args, filter.CreatedBy)
		argIndex++
	}

	// Apply pagination
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIndex)
		args = append(args, filter.Limit)
		argIndex++
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIndex)
		args = append(args, filter.Offset)
		argIndex++
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}
	defer rows.Close()

	agents := make([]*ai.AgentDefinition, 0)

	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		var agent ai.AgentDefinition
		if err := json.Unmarshal(data, &agent); err != nil {
			return nil, fmt.Errorf("failed to unmarshal agent: %w", err)
		}

		agents = append(agents, &agent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return agents, nil
}

// GetExecutionHistory retrieves execution history for an agent
func (s *SQLAgentStore) GetExecutionHistory(ctx context.Context, agentID string, limit int) ([]*ai.AgentExecution, error) {
	// For now, return empty array
	// This would require a separate table for execution history
	return []*ai.AgentExecution{}, nil
}

// CreateTable creates the agents table (helper method for migrations)
func (s *SQLAgentStore) CreateTable(ctx context.Context) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			data JSONB NOT NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)
	`, s.table)

	_, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	if s.logger != nil {
		s.logger.Info("agents table created", forge.F("table", s.table))
	}

	return nil
}
