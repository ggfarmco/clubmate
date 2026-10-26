package kv

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const (
	maxKeyBytes   = 256
	maxValueBytes = 256 << 10
)

var (
	ErrInvalidKey    = errors.New("key must be 1-256 bytes without NUL")
	ErrInvalidValue  = errors.New("value must be valid JSON")
	ErrValueTooLarge = errors.New("value exceeds 256 KiB")
)

const schema = `
CREATE TABLE IF NOT EXISTS kv(
	plugin TEXT NOT NULL,
	key TEXT NOT NULL,
	value TEXT NOT NULL,
	PRIMARY KEY(plugin, key)
);`

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve kv db path: %w", err)
	}
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: abs, RawQuery: q.Encode()}).String())
	if err != nil {
		return nil, fmt.Errorf("open kv db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate kv db: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Get(ctx context.Context, plugin, key string) (json.RawMessage, bool, error) {
	if err := validateKey(key); err != nil {
		return nil, false, err
	}
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE plugin = ? AND key = ?`, plugin, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get kv: %w", err)
	}
	return json.RawMessage(value), true, nil
}

func (s *Store) Set(ctx context.Context, plugin, key string, value json.RawMessage) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if len(value) > maxValueBytes {
		return ErrValueTooLarge
	}
	if !json.Valid(value) {
		return ErrInvalidValue
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO kv(plugin, key, value) VALUES(?, ?, ?)
		ON CONFLICT(plugin, key) DO UPDATE SET value = excluded.value`, plugin, key, string(value))
	if err != nil {
		return fmt.Errorf("set kv: %w", err)
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, plugin, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM kv WHERE plugin = ? AND key = ?`, plugin, key); err != nil {
		return fmt.Errorf("delete kv: %w", err)
	}
	return nil
}

func (s *Store) List(ctx context.Context, plugin, prefix string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key FROM kv WHERE plugin = ? AND substr(key, 1, length(?)) = ? ORDER BY key`, plugin, prefix, prefix)
	if err != nil {
		return nil, fmt.Errorf("list kv: %w", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan kv key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list kv: %w", err)
	}
	return keys, nil
}

func validateKey(key string) error {
	if key == "" || len(key) > maxKeyBytes || strings.ContainsRune(key, 0) {
		return ErrInvalidKey
	}
	return nil
}
