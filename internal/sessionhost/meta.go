package sessionhost

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Config struct {
	ID         string   `json:"id"`
	ProjectID  string   `json:"projectId"`
	Title      string   `json:"title"`
	Owner      string   `json:"owner"`
	Command    []string `json:"command"`
	Cwd        string   `json:"cwd"`
	Env        []string `json:"env"`
	Socket     string   `json:"socket"`
	MetaPath   string   `json:"metaPath"`
	Scrollback int      `json:"scrollback"`
}

type Meta struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"projectId"`
	Title     string     `json:"title"`
	Owner     string     `json:"owner"`
	Command   []string   `json:"command"`
	Cwd       string     `json:"cwd"`
	HostPID   int        `json:"hostPid"`
	ChildPID  int        `json:"childPid"`
	PGID      int        `json:"pgid"`
	StartedAt time.Time  `json:"startedAt"`
	ExitCode  *int       `json:"exitCode"`
	ExitedAt  *time.Time `json:"exitedAt"`
}

func ReadMeta(path string) (Meta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, fmt.Errorf("read meta: %w", err)
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, fmt.Errorf("decode meta %s: %w", path, err)
	}
	return m, nil
}

func WriteMeta(path string, m Meta) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode meta: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write meta: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("publish meta: %w", err)
	}
	return nil
}
