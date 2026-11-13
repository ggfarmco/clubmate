package main

import (
	"context"
	"slices"

	"clubmate/pkg/sdui"
)

const (
	reviewBaseField = "reviewBase"
	actDismiss      = "dismiss"
	reviewStart     = "Session start · before turn 1"
)

type reviewValue struct {
	Turn int     `json:"turn"`
	Base *string `json:"reviewBase"`
}

func (a *app) reviewAgainst(ctx context.Context, workspaceID string, s *session, st *sessionUI, base *string) error {
	a.uiMu.Lock()
	if base != nil {
		st.reviewBase = *base
	}
	branch := st.reviewBase
	a.uiMu.Unlock()
	if _, bases := s.hasChanges(); !slices.Contains(bases, branch) {
		branch = ""
	}
	if branch == "" {
		return a.review(ctx, workspaceID, s, 0)
	}
	rec := s.snapshot()
	return a.host.RunCommand(ctx, diffviewCmd, workspaceID, diffviewArgs{Cwd: rec.Worktree, Base: branch, Session: rec.ID}, nil)
}

func reviewButton(sid string, bases []string, current string) sdui.Node {
	button := sdui.WithAction(sdui.Button(sid+"/review", "Review changes", "doc.text.magnifyingglass"), actReview)
	if len(bases) == 0 {
		return button
	}
	if !slices.Contains(bases, current) {
		current = ""
	}
	options := []sdui.Option{{Value: "", Label: reviewStart}}
	for _, b := range bases {
		options = append(options, sdui.Option{Value: b, Label: b})
	}
	title := sdui.Node{Type: "text", Props: map[string]any{"text": "Review changes", "style": "title"}}
	form := sdui.WithAction(sdui.Form(sid+"/review/form",
		sdui.HStack(sdui.WithTone(sdui.Icon("doc.text.magnifyingglass"), "accent"), title),
		sdui.HStack(sdui.MutedText("Compare with"), sdui.Select(reviewBaseField, options, current)),
		sdui.MutedText("A branch is compared from its merge-base with this worktree."),
		sdui.HStack(sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/review/cancel", "Cancel", ""), actDismiss), sdui.VariantSecondary), sdui.WithVariant(sdui.Button(sid+"/review/open", "Open", ""), sdui.VariantPrimary)),
	), actReview)
	return sdui.WithPopover(button, form)
}
