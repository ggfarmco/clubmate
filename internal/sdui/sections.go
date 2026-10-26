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

type Sections struct {
	list   []Section
	owners map[string]string
}

func NewSections(defs []SectionDef) *Sections {
	s := &Sections{list: []Section{}, owners: map[string]string{}}
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

func SlotSection(slot string) (string, bool) {
	for _, suffix := range []string{".sidebar", ".main"} {
		if section, ok := strings.CutSuffix(slot, suffix); ok && section != "" {
			return section, true
		}
	}
	return "", false
}
