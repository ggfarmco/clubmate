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

	"clubmate/internal/paths"
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
	if err := paths.PrivateDB(abs); err != nil {
		return nil, fmt.Errorf("prepare kv db: %w", err)
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

func (s *Store) DeletePrefix(ctx context.Context, plugin, prefix string) error {
	if err := validateKey(prefix); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM kv WHERE plugin = ? AND substr(key, 1, length(?)) = ?`, plugin, prefix, prefix); err != nil {
		return fmt.Errorf("delete kv prefix: %w", err)
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

func (s *Store) MovePlugin(ctx context.Context, from, to, prefix string, rewrite func(key string, v json.RawMessage) json.RawMessage) (int, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("move kv: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return 0, fmt.Errorf("move kv: %w", err)
	}
	n, err := moveRows(ctx, conn, from, to, prefix, rewrite)
	if err != nil {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		return 0, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		return 0, fmt.Errorf("move kv: %w", err)
	}
	return n, nil
}

func moveRows(ctx context.Context, conn *sql.Conn, from, to, prefix string, rewrite func(string, json.RawMessage) json.RawMessage) (int, error) {
	rows, err := conn.QueryContext(ctx, `SELECT key, value FROM kv WHERE plugin = ? ORDER BY key`, from)
	if err != nil {
		return 0, fmt.Errorf("move kv: %w", err)
	}
	type row struct{ key, value string }
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.key, &r.value); err != nil {
			rows.Close()
			return 0, fmt.Errorf("move kv: %w", err)
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("move kv: %w", err)
	}
	for _, r := range all {
		key := prefix + r.key
		if err := validateKey(key); err != nil {
			return 0, fmt.Errorf("move kv %q: %w", r.key, err)
		}
		value := rewrite(r.key, json.RawMessage(r.value))
		if !json.Valid(value) {
			return 0, fmt.Errorf("move kv %q: %w", r.key, ErrInvalidValue)
		}
		if _, err := conn.ExecContext(ctx, `INSERT OR IGNORE INTO kv(plugin, key, value) VALUES(?, ?, ?)`, to, key, string(value)); err != nil {
			return 0, fmt.Errorf("move kv: %w", err)
		}
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM kv WHERE plugin = ?`, from); err != nil {
		return 0, fmt.Errorf("move kv: %w", err)
	}
	return len(all), nil
}

func validateKey(key string) error {
	if key == "" || len(key) > maxKeyBytes || strings.ContainsRune(key, 0) {
		return ErrInvalidKey
	}
	return nil
}
