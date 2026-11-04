package automation

import (
	"context"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	HelpID   = "help"
	HelpIcon = "info.circle"
	ActAsk   = "ask"
	AskID    = HelpID + "/" + ActAsk
)

func HelpItem(label string) sdui.TreeItem {
	return sdui.TreeItem{ID: HelpID, Label: label, Icon: HelpIcon, Muted: true}
}

func HelpHeader(title, meta, ask string) sdui.Node {
	button := sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(AskID, ask, "sparkles"), ActAsk), sdui.VariantPrimary), "cmd+n")
	return sdui.Header(title, sdui.HeaderProps{Icon: HelpIcon, Meta: meta}, button)
}

func Facts(facts [][2]string) sdui.Node {
	rows := make([]sdui.Node, 0, len(facts))
	for _, f := range facts {
		rows = append(rows, Property(f[0], f[1], ""))
	}
	return Properties(rows...)
}

func HelpSelection[P Entry[P]](ctx context.Context, h *pluginsdk.Host, workspace string, items []P) (P, string, error) {
	var stored string
	if _, err := h.StorageGet(ctx, SelectedKey(workspace), &stored); err != nil {
		var none P
		return none, "", err
	}
	if stored == HelpID || len(items) == 0 {
		var none P
		return none, HelpID, nil
	}
	return Selection(ctx, h, workspace, items)
}
