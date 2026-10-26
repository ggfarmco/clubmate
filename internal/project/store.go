package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"clubmate/internal/id"
)

var (
	ErrNotFound      = errors.New("project not found")
	ErrInvalidRepo   = errors.New("repo must be an absolute path to an existing directory")
	ErrDuplicateRepo = errors.New("repo already in project")
	ErrEmptyName     = errors.New("project name is empty")
)

const schema = `
CREATE TABLE IF NOT EXISTS projects(
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS repos(
	project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
	position INTEGER,
	path TEXT,
	PRIMARY KEY(project_id, path)
);`

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Repos     []string  `json:"repos"`
	CreatedAt time.Time `json:"createdAt"`
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: q.Encode()}).String())
	if err != nil {
		return nil, fmt.Errorf("open project db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate project db: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Create(ctx context.Context, name string, repos []string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, ErrEmptyName
	}
	seen := map[string]bool{}
	for _, repo := range repos {
		if err := validateRepo(repo); err != nil {
			return Project{}, err
		}
		if seen[repo] {
			return Project{}, fmt.Errorf("%w: %s", ErrDuplicateRepo, repo)
		}
		seen[repo] = true
	}
	p := Project{ID: id.New("prj"), Name: name, CreatedAt: time.Now()}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(id, name, created_at) VALUES(?, ?, ?)`, p.ID, p.Name, p.CreatedAt.UnixNano()); err != nil {
		return Project{}, fmt.Errorf("insert project: %w", err)
	}
	for i, repo := range repos {
		if _, err := tx.ExecContext(ctx, `INSERT INTO repos(project_id, position, path) VALUES(?, ?, ?)`, p.ID, i, repo); err != nil {
			return Project{}, fmt.Errorf("insert repo: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit: %w", err)
	}
	p.Repos = repos
	return p, nil
}

func (s *Store) List(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at FROM projects ORDER BY created_at, rowid`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	for i := range out {
		if out[i].Repos, err = s.repos(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) Get(ctx context.Context, id string) (Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx, `SELECT id, name, created_at FROM projects WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	if p.Repos, err = s.repos(ctx, id); err != nil {
		return Project{}, err
	}
	return p, nil
}

func (s *Store) AddRepo(ctx context.Context, id, path string) (Project, error) {
	if err := validateRepo(path); err != nil {
		return Project{}, err
	}
	if _, err := s.Get(ctx, id); err != nil {
		return Project{}, err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO repos(project_id, position, path)
		 VALUES(?, (SELECT COALESCE(MAX(position), -1) + 1 FROM repos WHERE project_id = ?), ?)`,
		id, id, path)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Project{}, fmt.Errorf("%w: %s", ErrDuplicateRepo, path)
		}
		return Project{}, fmt.Errorf("insert repo: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) repos(ctx context.Context, id string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM repos WHERE project_id = ? ORDER BY position`, id)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("scan repo: %w", err)
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProject(r scanner) (Project, error) {
	var p Project
	var created int64
	if err := r.Scan(&p.ID, &p.Name, &created); err != nil {
		return Project{}, err
	}
	p.CreatedAt = time.Unix(0, created)
	return p, nil
}

func validateRepo(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %s", ErrInvalidRepo, path)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %s", ErrInvalidRepo, path)
	}
	return nil
}
