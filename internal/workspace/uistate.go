package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const uiStateSchema = `
CREATE TABLE IF NOT EXISTS ui_state(
	workspace_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
	value TEXT NOT NULL
);`

func (s *Store) UIState(ctx context.Context, id string) (json.RawMessage, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM ui_state WHERE workspace_id = ?`, id).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get ui state: %w", err)
	}
	return json.RawMessage(value), true, nil
}

func (s *Store) SetUIState(ctx context.Context, id string, value json.RawMessage) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ui_state(workspace_id, value) VALUES(?, ?)
		 ON CONFLICT(workspace_id) DO UPDATE SET value = excluded.value`,
		id, string(value))
	if err != nil {
		return fmt.Errorf("set ui state: %w", err)
	}
	return nil
}

func (s *Store) ImportUIState(ctx context.Context, id string, value json.RawMessage) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ui_state(workspace_id, value) VALUES(?, ?) ON CONFLICT(workspace_id) DO NOTHING`,
		id, string(value))
	if err != nil {
		return fmt.Errorf("import ui state: %w", err)
	}
	return nil
}
