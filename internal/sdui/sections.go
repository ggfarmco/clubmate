package sdui

import (
	"cmp"
	"slices"
	"strings"
)

type SectionDef struct {
	ID     string
	Title  string
	Icon   string
	Order  int
	Plugin string
}

type Section struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Icon   string `json:"icon"`
	Order  int    `json:"order"`
	Plugin string `json:"plugin"`
}

type ViewDef struct {
	Slot   string
	Plugin string
}

type Sections struct {
	list   []Section
	owners map[string]string
	views  map[string]string
}

func NewSections(defs []SectionDef, views ...ViewDef) *Sections {
	s := &Sections{list: []Section{}, owners: map[string]string{}, views: map[string]string{}}
	for _, v := range views {
		s.views[v.Slot] = v.Plugin
	}
	for _, d := range defs {
		if _, dup := s.owners[d.ID]; dup {
			continue
		}
		s.owners[d.ID] = d.Plugin
		s.list = append(s.list, Section(d))
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
	section, ok := SlotSection(slot)
	if !ok {
		return "", false
	}
	return s.Owner(section)
}

func SlotSection(slot string) (string, bool) {
	for _, suffix := range []string{".sidebar", ".main", ".status"} {
		if section, ok := strings.CutSuffix(slot, suffix); ok && section != "" {
			return section, true
		}
	}
	return "", false
}
