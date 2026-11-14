package main

import "clubmate/pkg/sdui"

const noReview = "No review open"

func reviewView(rs []*review) sdui.Node {
	for _, r := range rs {
		if r.Sheet != nil {
			return sendSheetView(r)
		}
	}
	return sdui.Empty(noReview)
}

func textStyled(text, style string) sdui.Node {
	return sdui.Node{Type: "text", Props: map[string]any{"text": text, "style": style}}
}
