package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"clubmate/internal/schema"
	"clubmate/internal/sdui"
)

const (
	ProtocolVersion = 1
	ManifestFile    = "plugin.json"
)

var (
	ErrInvalidManifest = errors.New("invalid plugin manifest")

	idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)
)

type Manifest struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Protocol    int         `json:"protocol"`
	Command     []string    `json:"command"`
	Contributes Contributes `json:"contributes"`
	Dir         string      `json:"-"`
}

type Contributes struct {
	Sections []Section    `json:"sections"`
	Commands []CommandDef `json:"commands"`
	Views    []View       `json:"views"`
}

type View struct {
	Slot string `json:"slot"`
}

type Section struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Icon    string `json:"icon"`
	Order   int    `json:"order"`
	Scope   string `json:"scope"`
	AutoNew bool   `json:"autoNew"`
}

type CommandDef struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Hidden      bool           `json:"hidden"`
	Confirm     string         `json:"confirm"`
	Args        *schema.Object `json:"args"`
	Result      *schema.Object `json:"result"`
	Agent       bool           `json:"agent"`
	Destructive bool           `json:"destructive"`
	Provides    []string       `json:"provides"`
}

func Load(dir string) (Manifest, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return Manifest{}, fmt.Errorf("resolve %s: %w", dir, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, invalid(dir, "decode: %v", err)
	}
	m.Dir = dir
	if err := m.validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func SectionDefs(manifests []Manifest) []sdui.SectionDef {
	var defs []sdui.SectionDef
	for _, m := range manifests {
		for _, s := range m.Contributes.Sections {
			defs = append(defs, sdui.SectionDef{ID: s.ID, Title: s.Title, Icon: s.Icon, Order: s.Order, Scope: s.Scope, AutoNew: s.AutoNew, Plugin: m.ID})
		}
	}
	return defs
}

func ViewDefs(manifests []Manifest, logger *slog.Logger) []sdui.ViewDef {
	sections := map[string]bool{}
	for _, m := range manifests {
		for _, s := range m.Contributes.Sections {
			sections[s.ID] = true
		}
	}
	claimed := map[string]string{}
	var defs []sdui.ViewDef
	for _, m := range manifests {
		for _, v := range m.Contributes.Views {
			section, _ := sdui.SlotSection(v.Slot)
			if !sections[section] {
				logger.Warn("view ignored: section not registered", "plugin", m.ID, "slot", v.Slot)
				continue
			}
			if first, ok := claimed[v.Slot]; ok {
				logger.Warn("view ignored: slot already claimed", "plugin", m.ID, "slot", v.Slot, "owner", first)
				continue
			}
			claimed[v.Slot] = m.ID
			defs = append(defs, sdui.ViewDef{Slot: v.Slot, Plugin: m.ID})
		}
	}
	return defs
}

func (m Manifest) Executable() string {
	return filepath.Join(m.Dir, m.Command[0])
}

func (m Manifest) validate() error {
	if !idPattern.MatchString(m.ID) {
		return invalid(m.Dir, "id %q does not match %s", m.ID, idPattern)
	}
	if m.Protocol != ProtocolVersion {
		return invalid(m.Dir, "protocol %d, want %d", m.Protocol, ProtocolVersion)
	}
	if err := m.validateCommand(); err != nil {
		return err
	}
	sectionIDs := make([]string, 0, len(m.Contributes.Sections))
	for _, s := range m.Contributes.Sections {
		sectionIDs = append(sectionIDs, s.ID)
	}
	if err := m.validateIDs("section", sectionIDs); err != nil {
		return err
	}
	for _, s := range m.Contributes.Sections {
		if s.Scope != "" && s.Scope != sdui.ScopeProject && s.Scope != sdui.ScopeApp {
			return invalid(m.Dir, "section %q has invalid scope %q", s.ID, s.Scope)
		}
	}
	commandIDs := make([]string, 0, len(m.Contributes.Commands))
	for _, c := range m.Contributes.Commands {
		commandIDs = append(commandIDs, c.ID)
	}
	if err := m.validateIDs("command", commandIDs); err != nil {
		return err
	}
	for _, c := range m.Contributes.Commands {
		if err := schema.Check(c.Args); err != nil {
			return invalid(m.Dir, "command %q args: %v", c.ID, err)
		}
		if err := schema.Check(c.Result); err != nil {
			return invalid(m.Dir, "command %q result: %v", c.ID, err)
		}
	}
	for _, v := range m.Contributes.Views {
		if _, ok := sdui.SlotSection(v.Slot); !ok {
			return invalid(m.Dir, "invalid view slot %q", v.Slot)
		}
	}
	return nil
}

func (m Manifest) validateCommand() error {
	if len(m.Command) == 0 {
		return invalid(m.Dir, "command is empty")
	}
	if !filepath.IsLocal(m.Command[0]) {
		return invalid(m.Dir, "command %q must be a relative path inside the plugin dir", m.Command[0])
	}
	info, err := os.Stat(m.Executable())
	if err != nil {
		return invalid(m.Dir, "command %q: %v", m.Command[0], err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return invalid(m.Dir, "command %q is not an executable file", m.Command[0])
	}
	return nil
}

func (m Manifest) validateIDs(kind string, ids []string) error {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != m.ID && !strings.HasPrefix(id, m.ID+".") {
			return invalid(m.Dir, "%s id %q must be %q or start with %q", kind, id, m.ID, m.ID+".")
		}
		if seen[id] {
			return invalid(m.Dir, "duplicate %s id %q", kind, id)
		}
		seen[id] = true
	}
	return nil
}

func invalid(dir, format string, args ...any) error {
	return fmt.Errorf("%s: %w: %s", dir, ErrInvalidManifest, fmt.Sprintf(format, args...))
}
