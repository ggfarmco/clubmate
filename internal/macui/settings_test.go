package macui_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingsFontMenus(t *testing.T) {
	for _, tc := range []struct{ name, tab string }{
		{"interface font", "general"},
		{"agents chat font", "agents"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := start(t, "macui-settings-"+tc.tab, "", "-NSTreatUnknownArgumentsAsOpen", "NO", "--settings-tab", tc.tab)
			in.cm("ui", "present", "settings")
			var picker string
			in.eventually("the font picker shows", func() bool {
				e := in.snapshot().kind("settings").find(func(e element) bool { return strings.HasPrefix(e.ID, "font.") })
				if e != nil {
					picker = e.ID
				}
				return e != nil
			})

			menu := in.pointer("click", picker).Menu
			require.NotEmpty(t, menu)
			assert.Equal(t, 1, checked(menu), "one family has the ✓")
			assert.NotContains(t, titles(menu), "Font", "no stray picker header")
			more := moreFonts(menu)
			require.NotNil(t, more)
			assert.Equal(t, "More fonts", more.Title)
			assert.NotContains(t, titles(more.Children), "More fonts")
			family := more.Children[0].Title

			in.pointer("menu", picker, "More fonts > "+family)
			in.eventually("the submenu names the picked family", func() bool {
				more := moreFonts(in.pointer("click", picker).Menu)
				return more != nil && more.Title == "More fonts · "+family
			})
			reopened := in.pointer("click", picker).Menu
			assert.Equal(t, 0, checked(reopened), "no top-level ✓ once a More fonts family is picked")
			assert.Equal(t, 1, checked(moreFonts(reopened).Children))
		})
	}
}

func titles(items []menuItem) []string {
	out := make([]string, 0, len(items))
	for _, i := range items {
		out = append(out, i.Title)
	}
	return out
}

func checked(items []menuItem) int {
	n := 0
	for _, i := range items {
		if i.Checked {
			n++
		}
	}
	return n
}

func moreFonts(items []menuItem) *menuItem {
	for i := range items {
		if strings.HasPrefix(items[i].Title, "More fonts") {
			return &items[i]
		}
	}
	return nil
}
