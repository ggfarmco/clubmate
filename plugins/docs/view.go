package main

import (
	"clubmate/pkg/sdui"
)

const goneText = "File no longer exists"

type popupState struct {
	title  string
	path   string
	anchor string
	kind   string
	html   string
	rev    int
	gone   bool
	back   bool
}

func popupView(s popupState) sdui.Node {
	var actions []sdui.Node
	if s.back {
		back := sdui.WithVariant(sdui.WithAction(sdui.Button("back", "Back", "chevron.left"), "back"), sdui.VariantGhost)
		actions = append(actions, sdui.WithIconOnly(back))
	}
	header := sdui.Header(s.title, sdui.HeaderProps{}, actions...)
	if s.gone {
		return sdui.VStack(header, sdui.EmptyState(goneText, sdui.EmptyProps{Icon: "doc.questionmark", Hint: s.path}))
	}
	return sdui.VStack(header, sdui.Document("doc", sdui.DocumentProps{HTML: s.html, Path: s.path, Rev: s.rev, Size: "fill", Anchor: s.anchor, Kind: s.kind}))
}
