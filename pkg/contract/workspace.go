package contract

import (
	"encoding/json"
	"os"
	"time"
)

const WorkspaceEnv = "CLUBMATE_WORKSPACE_ID"

type Workspace struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Folders      []Folder   `json:"folders"`
	CreatedAt    time.Time  `json:"createdAt"`
	Home         bool       `json:"home"`
	LastOpenedAt *time.Time `json:"lastOpenedAt"`
}

type Folder struct {
	Path     string `json:"path"`
	Worktree string `json:"worktree,omitempty"`
	Missing  bool   `json:"missing,omitempty"`
}

func WorkspaceFromEnv() string {
	return os.Getenv(WorkspaceEnv)
}

func (w Workspace) MarshalJSON() ([]byte, error) {
	type plain Workspace
	if w.Folders == nil {
		w.Folders = []Folder{}
	}
	return json.Marshal(plain(w))
}

func (w Workspace) Paths() []string {
	out := make([]string, 0, len(w.Folders))
	for _, f := range w.Folders {
		out = append(out, f.Path)
	}
	return out
}

func (w Workspace) Roots() []string {
	out := make([]string, 0, len(w.Folders))
	for _, f := range w.Folders {
		out = append(out, f.Root())
	}
	return out
}

func (w Workspace) MissingWorktrees() []string {
	var out []string
	for _, f := range w.Folders {
		if f.Missing {
			out = append(out, f.Worktree)
		}
	}
	return out
}

func (f Folder) Root() string {
	if f.Worktree == "" || f.Missing {
		return f.Path
	}
	return f.Worktree
}
