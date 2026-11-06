package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"clubmate/internal/paths"
	"clubmate/pkg/contract"
	"clubmate/pkg/id"
)

var (
	ErrNotFound        = errors.New("workspace not found")
	ErrInvalidFolder   = errors.New("folder must be an absolute path to an existing directory")
	ErrDuplicateFolder = errors.New("folder already in workspace")
	ErrEmptyName       = errors.New("workspace name is empty")
	ErrNotFolder       = errors.New("path is not a folder of this workspace")
	ErrNotDirectory    = errors.New("path is not a directory")
)

const (
	homeName      = "~"
	workspaceCols = "id, name, created_at, home, last_opened_at"
	homeIndex     = `CREATE UNIQUE INDEX IF NOT EXISTS projects_home ON projects(home) WHERE home = 1`
)

const schema = `
CREATE TABLE IF NOT EXISTS projects(
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	home INTEGER NOT NULL DEFAULT 0,
	last_opened_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS repos(
	project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
	position INTEGER,
	path TEXT,
	PRIMARY KEY(project_id, path)
);
CREATE TABLE IF NOT EXISTS worktrees(
	project_id TEXT,
	repo TEXT,
	path TEXT NOT NULL,
	PRIMARY KEY(project_id, repo),
	FOREIGN KEY(project_id, repo) REFERENCES repos(project_id, path) ON DELETE CASCADE
);`

var addedColumns = []struct{ name, def string }{
	{name: "home", def: "INTEGER NOT NULL DEFAULT 0"},
	{name: "last_opened_at", def: "INTEGER NOT NULL DEFAULT 0"},
}

type (
	Workspace = contract.Workspace
	Folder    = contract.Folder
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace db path: %w", err)
	}
	if err := paths.PrivateDB(abs); err != nil {
		return nil, fmt.Errorf("prepare workspace db: %w", err)
	}
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: abs, RawQuery: q.Encode()}).String())
	if err != nil {
		return nil, fmt.Errorf("open workspace db: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate workspace db: %w", err)
	}
	return &Store{db: db}, nil
}

func CanonicalPath(path string) string {
	if !filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(resolved)
}

func CanonicalPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = CanonicalPath(p)
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Create(ctx context.Context, name string, repos []string) (Workspace, error) {
	return s.create(ctx, name, repos, false)
}

func (s *Store) Home(ctx context.Context, dir string) (Workspace, bool, error) {
	p, err := scanWorkspace(s.db.QueryRowContext(ctx, `SELECT `+workspaceCols+` FROM projects WHERE home = 1 ORDER BY created_at LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		p, err = s.create(ctx, homeName, []string{CanonicalPath(dir)}, true)
		return p, err == nil, err
	}
	if err != nil {
		return Workspace{}, false, fmt.Errorf("get home workspace: %w", err)
	}
	if err := s.loadFolders(ctx, &p); err != nil {
		return Workspace{}, false, err
	}
	return p, false, nil
}

func (s *Store) FindByRepos(ctx context.Context, repos []string) (Workspace, bool, error) {
	want := pathSet(repos)
	if len(want) == 0 {
		return Workspace{}, false, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.project_id, r.path FROM repos r JOIN projects p ON p.id = r.project_id
		 ORDER BY p.last_opened_at DESC, p.created_at DESC, p.rowid DESC`)
	if err != nil {
		return Workspace{}, false, fmt.Errorf("find workspace by repos: %w", err)
	}
	defer rows.Close()
	var order []string
	byWorkspace := map[string][]string{}
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return Workspace{}, false, fmt.Errorf("scan repo: %w", err)
		}
		if _, ok := byWorkspace[id]; !ok {
			order = append(order, id)
		}
		byWorkspace[id] = append(byWorkspace[id], path)
	}
	if err := rows.Err(); err != nil {
		return Workspace{}, false, fmt.Errorf("find workspace by repos: %w", err)
	}
	for _, id := range order {
		if !maps.Equal(pathSet(byWorkspace[id]), want) {
			continue
		}
		p, err := s.Get(ctx, id)
		if err != nil {
			return Workspace{}, false, err
		}
		return p, true, nil
	}
	return Workspace{}, false, nil
}

func (s *Store) Rename(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrEmptyName
	}
	return s.exec(ctx, "rename workspace", `UPDATE projects SET name = ? WHERE id = ?`, name, id)
}

func (s *Store) Touch(ctx context.Context, id string, at time.Time) error {
	return s.exec(ctx, "touch workspace", `UPDATE projects SET last_opened_at = ? WHERE id = ?`, at.UnixNano(), id)
}

func (s *Store) create(ctx context.Context, name string, repos []string, home bool) (Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Workspace{}, ErrEmptyName
	}
	seen := map[string]bool{}
	for _, repo := range repos {
		if err := validateRepo(repo); err != nil {
			return Workspace{}, err
		}
		if seen[repo] {
			return Workspace{}, fmt.Errorf("%w: %s", ErrDuplicateFolder, repo)
		}
		seen[repo] = true
	}
	p := Workspace{ID: id.New("prj"), Name: name, CreatedAt: time.Now(), Home: home}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(id, name, created_at, home) VALUES(?, ?, ?, ?)`, p.ID, p.Name, p.CreatedAt.UnixNano(), p.Home); err != nil {
		return Workspace{}, fmt.Errorf("insert workspace: %w", err)
	}
	for i, repo := range repos {
		if _, err := tx.ExecContext(ctx, `INSERT INTO repos(project_id, position, path) VALUES(?, ?, ?)`, p.ID, i, repo); err != nil {
			return Workspace{}, fmt.Errorf("insert repo: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, fmt.Errorf("commit: %w", err)
	}
	p.Folders = make([]Folder, 0, len(repos))
	for _, repo := range repos {
		p.Folders = append(p.Folders, Folder{Path: repo})
	}
	return p, nil
}

func (s *Store) List(ctx context.Context) ([]Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+workspaceCols+` FROM projects ORDER BY last_opened_at DESC, created_at DESC, rowid DESC`)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		p, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	for i := range out {
		if err := s.loadFolders(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) Get(ctx context.Context, id string) (Workspace, error) {
	p, err := scanWorkspace(s.db.QueryRowContext(ctx, `SELECT `+workspaceCols+` FROM projects WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, err
	}
	if err := s.loadFolders(ctx, &p); err != nil {
		return Workspace{}, err
	}
	return p, nil
}

func (s *Store) AddRepo(ctx context.Context, id, path string) (Workspace, error) {
	if err := validateRepo(path); err != nil {
		return Workspace{}, err
	}
	if _, err := s.Get(ctx, id); err != nil {
		return Workspace{}, err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO repos(project_id, position, path)
		 VALUES(?, (SELECT COALESCE(MAX(position), -1) + 1 FROM repos WHERE project_id = ?), ?)`,
		id, id, path)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Workspace{}, fmt.Errorf("%w: %s", ErrDuplicateFolder, path)
		}
		return Workspace{}, fmt.Errorf("insert repo: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) SetWorktree(ctx context.Context, id, repo, path string) (Workspace, error) {
	p, err := s.Get(ctx, id)
	if err != nil {
		return Workspace{}, err
	}
	if !slices.ContainsFunc(p.Folders, func(f Folder) bool { return f.Path == repo }) {
		return Workspace{}, ErrNotFolder
	}
	if path == "" || filepath.Clean(path) == repo {
		_, err = s.db.ExecContext(ctx, `DELETE FROM worktrees WHERE project_id = ? AND repo = ?`, id, repo)
	} else {
		if !isDir(path) {
			return Workspace{}, ErrNotDirectory
		}
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO worktrees(project_id, repo, path) VALUES(?, ?, ?)
			 ON CONFLICT(project_id, repo) DO UPDATE SET path = excluded.path`,
			id, repo, filepath.Clean(path))
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("set worktree: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) Delete(ctx context.Context, id string) error {
	return s.exec(ctx, "delete workspace", `DELETE FROM projects WHERE id = ?`, id)
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT name FROM pragma_table_info('projects')`)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, col := range addedColumns {
		if existing[col.name] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE projects ADD COLUMN ` + col.name + ` ` + col.def); err != nil {
			return fmt.Errorf("add column %s: %w", col.name, err)
		}
	}
	if _, err := db.Exec(homeIndex); err != nil {
		return fmt.Errorf("create home index: %w", err)
	}
	if _, err := db.Exec(uiStateSchema); err != nil {
		return fmt.Errorf("create ui state table: %w", err)
	}
	return nil
}

func (s *Store) exec(ctx context.Context, op, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
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

func (s *Store) loadFolders(ctx context.Context, p *Workspace) error {
	repos, err := s.repos(ctx, p.ID)
	if err != nil {
		return err
	}
	selected, err := s.worktrees(ctx, p.ID)
	if err != nil {
		return err
	}
	p.Folders = make([]Folder, 0, len(repos))
	for _, repo := range repos {
		path := selected[repo]
		p.Folders = append(p.Folders, Folder{Path: repo, Worktree: path, Missing: path != "" && !isDir(path)})
	}
	return nil
}

func (s *Store) worktrees(ctx context.Context, id string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT repo, path FROM worktrees WHERE project_id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var repo, path string
		if err := rows.Scan(&repo, &path); err != nil {
			return nil, fmt.Errorf("scan worktree: %w", err)
		}
		out[repo] = path
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanWorkspace(r scanner) (Workspace, error) {
	var p Workspace
	var created, opened int64
	if err := r.Scan(&p.ID, &p.Name, &created, &p.Home, &opened); err != nil {
		return Workspace{}, err
	}
	p.CreatedAt = time.Unix(0, created)
	if opened != 0 {
		p.LastOpenedAt = new(time.Unix(0, opened))
	}
	return p, nil
}

func pathSet(paths []string) map[string]bool {
	out := make(map[string]bool, len(paths))
	for _, p := range CanonicalPaths(paths) {
		out[p] = true
	}
	return out
}

func validateRepo(path string) error {
	if !isDir(path) {
		return fmt.Errorf("%w: %s", ErrInvalidFolder, path)
	}
	return nil
}

func isDir(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
