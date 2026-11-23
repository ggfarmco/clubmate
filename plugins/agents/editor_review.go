package main

import (
	"context"
	"encoding/json"
	"slices"
)

const (
	editorStateMethod  = "editor.state"
	pluginChangedTopic = "plugin.changed"
	reviewDiffview     = "diffview"
	reviewHidden       = "hidden"
)

var editorPlugins = []string{"editor-nvim", "editor-vscode"}

type editorState struct {
	Review string `json:"review"`
}

func (a *app) loadEditorReview(ctx context.Context) bool {
	var st editorState
	if err := a.host.Call(ctx, editorStateMethod, struct{}{}, &st); err != nil {
		logError("editor state", err)
		return false
	}
	old := a.editorReview.Swap(&st.Review)
	return old == nil || *old != st.Review
}

func (a *app) editorReviewMode() string {
	if p := a.editorReview.Load(); p != nil {
		return *p
	}
	return reviewDiffview
}

func (a *app) pluginChanged(payload json.RawMessage) {
	var p struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(payload, &p) != nil || !slices.Contains(editorPlugins, p.ID) {
		return
	}
	go func() {
		ctx := context.Background()
		if a.loadEditorReview(ctx) {
			a.renderAll(ctx)
		}
	}()
}
