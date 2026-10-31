package sdui

import (
	"cmp"
	"slices"
	"strings"
)

const (
	ScopeProject = "project"
	ScopeApp     = "app"
)

type SectionDef struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Icon    string   `json:"icon"`
	Order   int      `json:"order"`
	Scope   string   `json:"scope"`
	AutoNew bool     `json:"autoNew"`
	Slots   []string `json:"slots,omitempty"`
	Host    string   `json:"host,omitempty"`
}

type Section struct {
	SectionDef
	Plugin string `json:"plugin"`
}

type ViewDef struct {
	Slot   string
	Plugin string
}

type Sections struct {
	list   []Section
	owners map[string]string
	scopes map[string]string
	views  map[string]string
	extra  map[string]string
}

func NewSections(defs []Section, views ...ViewDef) *Sections {
	s := &Sections{list: []Section{}, owners: map[string]string{}, scopes: map[string]string{}, views: map[string]string{}, extra: map[string]string{}}
	for _, v := range views {
		s.views[v.Slot] = v.Plugin
	}
	for _, d := range defs {
		if _, dup := s.owners[d.ID]; dup {
			continue
		}
		s.owners[d.ID] = d.Plugin
		d.Scope = cmp.Or(d.Scope, ScopeProject)
		s.scopes[d.ID] = d.Scope
		for _, name := range d.Slots {
			s.extra[d.ID+"."+name] = d.ID
		}
		s.list = append(s.list, d)
	}
	for i, sec := range s.list {
		if sec.Host != "" && (sec.Host == sec.ID || s.scopes[sec.Host] != sec.Scope) {
			s.list[i].Host = ""
		}
	}
	hosted := map[string]bool{}
	for _, sec := range s.list {
		hosted[sec.ID] = sec.Host != ""
	}
	for i, sec := range s.list {
		if hosted[sec.Host] {
			s.list[i].Host = ""
		}
	}
	slices.SortFunc(s.list, func(a, b Section) int {
		return cmp.Or(cmp.Compare(a.Order, b.Order), strings.Compare(a.ID, b.ID))
	})
	return s
}

func (s *Sections) List() []Section {
	return slices.Clone(s.list)
}

func (s *Sections) Owner(sectionID string) (string, bool) {
	plugin, ok := s.owners[sectionID]
	return plugin, ok
}

func (s *Sections) SlotOwner(slot string) (string, bool) {
	if plugin, ok := s.views[slot]; ok {
		return plugin, true
	}
	section, ok := s.SlotSection(slot)
	if !ok {
		return "", false
	}
	return s.Owner(section)
}

func (s *Sections) SlotScope(slot string) (string, bool) {
	section, ok := s.SlotSection(slot)
	if !ok {
		return "", false
	}
	scope, ok := s.scopes[section]
	return scope, ok
}

func (s *Sections) SlotSection(slot string) (string, bool) {
	if section, ok := s.extra[slot]; ok {
		return section, true
	}
	return SlotSection(slot)
}

func SlotSection(slot string) (string, bool) {
	for _, suffix := range []string{".sidebar", ".main", ".status"} {
		if section, ok := strings.CutSuffix(slot, suffix); ok && section != "" {
			return section, true
		}
	}
	return "", false
}
