package main

import (
	"fmt"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	listID       = "projects"
	openAction   = "open"
	deleteAction = "delete"
	createName   = "name"
	createRepos  = "repos"
	submitID     = "submit"
	detailSep    = "  "
)

func mainView(projects []pluginsdk.Project, selected, home, formID, deleting, failure string) sdui.Node {
	items := make([]sdui.Item, 0, len(projects))
	for _, p := range projects {
		items = append(items, sdui.Item{ID: p.ID, Label: p.Name, Detail: repoDetail(p.Repos, home)})
	}
	nodes := []sdui.Node{
		{Type: "text", Props: map[string]any{"text": "Projects", "style": "title"}},
		sdui.List(listID, items, selected),
	}
	if deleting != "" {
		nodes = slices.Insert(nodes, 1, sdui.MutedText("Deleting "+deleting+"…"))
	}
	if failure != "" {
		nodes = slices.Insert(nodes, 1, sdui.Badge("Delete failed: "+failure, "danger"))
	}
	if i := slices.IndexFunc(projects, func(p pluginsdk.Project) bool { return p.ID == selected }); i >= 0 {
		name := projects[i].Name
		nodes = append(nodes, sdui.HStack(
			sdui.Button(openAction, "Open", ""),
			sdui.WithConfirm(sdui.Button(deleteAction, "Delete…", ""), fmt.Sprintf("Delete project %s? Its terminals and editors are killed.", name)),
		))
	}
	form := sdui.WithAction(sdui.Form(formID,
		sdui.TextField(createName, "", "Name (defaults to the first folder)"),
		sdui.FolderField(createRepos, "Choose folders…", nil),
		sdui.Button(submitID, "Create", ""),
	), createAction)
	return sdui.VStack(append(nodes, form)...)
}

func repoDetail(repos []string, home string) string {
	shown := make([]string, 0, len(repos))
	for _, r := range repos {
		shown = append(shown, tilde(r, home))
	}
	return strings.Join(shown, detailSep)
}

func tilde(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+"/"); ok {
		return "~/" + rest
	}
	return path
}
