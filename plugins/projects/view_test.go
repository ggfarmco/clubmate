package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

func TestProjectsView(t *testing.T) {
	projects := []pluginsdk.Project{{ID: "prj_1", Name: "demo", Repos: []string{"/Users/me/a", "/opt/b", "/Users/mex"}}}
	view := mainView(projects, "prj_1", "/Users/me", "create-0", "", "")
	list := view.Children[1]
	assert.Equal(t, "~/a  /opt/b  /Users/mex", list.Props["items"].([]sdui.Item)[0].Detail)
	controls := view.Children[2]
	assert.Equal(t, "Delete project demo? Its terminals and editors are killed.", controls.Children[1].Props["confirm"])
	assert.Len(t, mainView(projects, "", "/Users/me", "create-0", "", "").Children, 3)
}

func TestProjectsViewDeleteStatus(t *testing.T) {
	projects := []pluginsdk.Project{{ID: "prj_1", Name: "demo"}}
	deleting := mainView(projects, "", "", "create-0", "demo", "")
	assert.Equal(t, "Deleting demo…", deleting.Children[1].Props["text"])
	failed := mainView(projects, "", "", "create-0", "", "boom")
	assert.Equal(t, "danger", failed.Children[1].Props["tone"])
	assert.Equal(t, "Delete failed: boom", failed.Children[1].Props["text"])
}
