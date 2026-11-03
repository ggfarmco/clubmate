package main

import (
	"fmt"
	"path"
	"strings"

	"clubmate/pkg/sdui"
)

const (
	reviewKeys   = "r reviewed · ]f next to review · ,rc comment · ]a next comment"
	reviewLegend = "○ to review · ✓ reviewed · ● changed since · n comments"
	noReview     = "No review open"
	staleIcon    = "circle.inset.filled"
)

var kindBadges = map[string]sdui.HeaderBadge{
	"new":     {Text: "new", Tone: "success"},
	"deleted": {Text: "deleted", Tone: "danger"},
	"renamed": {Text: "renamed", Tone: "neutral"},
}

func reviewView(rs []*review, sel string) sdui.Node {
	var cur *review
	for _, r := range rs {
		if r.ID == sel {
			cur = r
		}
	}
	if cur == nil && len(rs) > 0 {
		cur = rs[len(rs)-1]
	}
	if cur == nil {
		return sdui.Empty(noReview)
	}
	children := []sdui.Node{reviewHeader(cur, rs)}
	if tip, ok := reviewTip(cur); ok {
		children = append(children, tip)
	}
	items := reviewTree(cur)
	if len(cur.Comments) > 0 {
		items = append(items, commentItems(cur))
	}
	children = append(children, sdui.Tree("review/tree", items, selectedFile(cur)))
	if hidden := hiddenReviewed(cur); hidden > 0 {
		children = append(children, sdui.HStack(
			sdui.MutedText(fmt.Sprintf("%d reviewed %s hidden ·", hidden, plural(hidden, "file", "files"))),
			sdui.WithAction(sdui.WithVariant(sdui.Button("review/showReviewed", "show", ""), sdui.VariantGhost), "showReviewed"),
		))
	}
	children = append(children, reviewFooter(cur), reviewStrip(cur))
	if cur.Sheet != nil {
		children = append(children, sendSheetView(cur))
	}
	n := sdui.VStack(children...)
	n.ID = "review"
	return n
}

func selectedFile(r *review) string {
	if r.Current == "" {
		return ""
	}
	return "file:" + r.Current
}

func reviewHeader(r *review, rs []*review) sdui.Node {
	done, total := r.progress()
	open := len(r.openComments())
	var title sdui.Node
	if len(rs) > 1 {
		options := make([]sdui.Option, 0, len(rs))
		for _, o := range rs {
			d, t := o.progress()
			options = append(options, sdui.Option{Value: o.ID, Label: fmt.Sprintf("%s · %d/%d · %d", sides(o), d, t, len(o.openComments()))})
		}
		title = sdui.HStack(textStyled("Review", "title"), sdui.WithAction(sdui.Select("review/switch", options, r.ID), "switch"))
	} else {
		title = sdui.Header("Review", sdui.HeaderProps{Meta: subtitle(r)})
	}
	allDone := total > 0 && done == total
	tone := ""
	if allDone {
		tone = "success"
	}
	progress := sdui.HStack(sdui.Progress("review/progress", done, total, tone), sdui.Text(fmt.Sprintf("%d / %d reviewed", done, total)))
	added, removed := 0, 0
	for _, e := range r.shownEntries() {
		added += e.Additions
		removed += e.Deletions
	}
	counts := sdui.HStack(
		sdui.WithTone(sdui.Text(fmt.Sprintf("+%d", added)), "success"),
		sdui.WithTone(sdui.Text(fmt.Sprintf("−%d", removed)), "danger"),
		sdui.MutedText("·"),
		commentCount(open),
	)
	children := []sdui.Node{title, progress, counts}
	if allDone {
		children = append(children, sdui.WithTone(sdui.Text(doneLine(r, total, open)), "success"))
	}
	children = append(children, reviewFilterRow(r))
	n := sdui.VStack(children...)
	n.ID = "review/header"
	return n
}

func commentCount(open int) sdui.Node {
	if open == 0 {
		return sdui.MutedText("no comments")
	}
	return sdui.WithTone(sdui.Text(fmt.Sprintf("%d open %s", open, plural(open, "comment", "comments"))), "warning")
}

func doneLine(r *review, total, open int) string {
	if r.Sent > 0 {
		return fmt.Sprintf("%d/%d · %d %s sent", total, total, r.Sent, plural(r.Sent, "comment", "comments"))
	}
	all := fmt.Sprintf("All %d %s reviewed", total, plural(total, "file", "files"))
	if open == 0 {
		return all
	}
	return fmt.Sprintf("%s · %d %s for %s", all, open, plural(open, "comment", "comments"), recipientName(r))
}

func recipientName(r *review) string {
	switch {
	case r.RecipientTitle != "":
		return r.RecipientTitle
	case r.OpenerTitle != "":
		return r.OpenerTitle
	}
	return "a new session"
}

func subtitle(r *review) string {
	if r.OpenerTitle != "" {
		return r.OpenerTitle
	}
	return sides(r)
}

func sides(r *review) string {
	return r.Info.Labels.Right + " ↔ " + r.Info.Labels.Left
}

func reviewFilterRow(r *review) sdui.Node {
	first := []sdui.Node{sdui.MutedText("Show"), filterBox("code", "code", r.Filters.Code)}
	if r.Counts.Tests > 0 {
		first = append(first, filterBox("tests", fmt.Sprintf("tests %d", r.Counts.Tests), r.Filters.Tests))
	}
	var second []sdui.Node
	if r.Counts.Generated > 0 {
		second = append(second, filterBox("generated", fmt.Sprintf("generated %d", r.Counts.Generated), r.Filters.Generated))
	}
	second = append(second, filterBox("reviewed", "reviewed", r.Filters.Reviewed))
	n := sdui.VStack(sdui.HStack(first...), sdui.HStack(second...))
	n.ID = "review/filters"
	return n
}

func filterBox(name, label string, on bool) sdui.Node {
	return sdui.WithAction(sdui.Checkbox("review/filters/"+name, label, on), "filter")
}

func reviewTip(r *review) (sdui.Node, bool) {
	done, _ := r.progress()
	if done > 0 || len(r.Comments) > 0 || r.Sent > 0 {
		return sdui.Node{}, false
	}
	text := "Read a file, press r to mark it reviewed; ]f opens the next one."
	if hiddenCategories(r) {
		text += " Tests and generated files are hidden."
	}
	n := sdui.Card("", "chevron.right", sdui.Text(text))
	n.ID = "review/tip"
	return n, true
}

func hiddenCategories(r *review) bool {
	return (r.Counts.Tests > 0 && !r.Filters.Tests) || (r.Counts.Generated > 0 && !r.Filters.Generated)
}

func hiddenReviewed(r *review) int {
	return len(r.shownEntries()) - len(r.visibleFiles())
}

type reviewDir struct {
	path  string
	files []reviewEntry
	all   []reviewEntry
}

func reviewTree(r *review) []sdui.TreeItem {
	visible := map[string]bool{}
	for _, e := range r.visibleFiles() {
		visible[e.Path] = true
	}
	var dirs []*reviewDir
	byPath := map[string]*reviewDir{}
	for _, e := range r.shownEntries() {
		dir := path.Dir(e.Path)
		d, ok := byPath[dir]
		if !ok {
			d = &reviewDir{path: dir}
			byPath[dir] = d
			dirs = append(dirs, d)
		}
		d.all = append(d.all, e)
		if visible[e.Path] {
			d.files = append(d.files, e)
		}
	}
	var items []sdui.TreeItem
	for _, d := range dirs {
		if d.path == "." {
			for _, e := range d.files {
				items = append(items, fileItem(r, e))
			}
			continue
		}
		items = append(items, dirItem(r, d))
	}
	return items
}

func dirItem(r *review, d *reviewDir) sdui.TreeItem {
	done, comments := 0, 0
	for _, e := range d.all {
		if reviewedNow(e) {
			done++
		}
		comments += e.Comments
	}
	children := make([]sdui.TreeItem, 0, len(d.files))
	for _, e := range d.files {
		children = append(children, fileItem(r, e))
	}
	item := sdui.TreeItem{
		ID: "dir:" + d.path, Label: d.path, Detail: fmt.Sprintf("%d/%d", done, len(d.all)), Truncate: "middle",
		HasChildren: true, Expanded: !r.Collapsed[d.path], Children: children,
	}
	if comments > 0 {
		item.Badges = []sdui.HeaderBadge{{Text: fmt.Sprint(comments), Tone: "warning"}}
	}
	return item
}

func fileItem(r *review, e reviewEntry) sdui.TreeItem {
	icon, tone := fileState(r, e)
	item := sdui.TreeItem{
		ID: "file:" + e.Path, Label: path.Base(e.Path), Icon: icon, Tone: tone, Truncate: "middle",
		Changes: &sdui.Changes{Added: e.Additions, Removed: e.Deletions},
		Menu:    []sdui.MenuEntry{{Action: "toggleReviewed", Label: reviewedLabel(e), Key: "r"}},
	}
	if b, ok := kindBadges[e.Kind]; ok {
		item.Badges = append(item.Badges, b)
	}
	if e.Comments > 0 {
		item.Badges = append(item.Badges, sdui.HeaderBadge{Text: fmt.Sprint(e.Comments), Tone: "warning"})
	}
	return item
}

func fileState(r *review, e reviewEntry) (string, string) {
	switch {
	case e.Stale:
		return staleIcon, "warning"
	case reviewedNow(e):
		return "checkmark", "success"
	case e.Path == r.Current:
		return "arrowtriangle.right.fill", "accent"
	}
	return "circle", ""
}

func reviewedLabel(e reviewEntry) string {
	if reviewedNow(e) {
		return "Mark not reviewed"
	}
	return "Mark reviewed"
}

func commentItems(r *review) sdui.TreeItem {
	var open, resolved []sdui.TreeItem
	for _, c := range r.Comments {
		item := sdui.TreeItem{ID: fmt.Sprintf("c:%d", c.ID), Label: fmt.Sprintf("%s:%d · %s", path.Base(c.File), c.LineStart, firstLine(c.Text)), Detail: c.Type}
		if c.Status == "open" {
			item.Icon = "circle"
			if c.Type == "issue" {
				item.Icon, item.Tone = staleIcon, "warning"
			}
			open = append(open, item)
			continue
		}
		item.Icon, item.Faint, item.Detail = "checkmark", true, "resolved"
		resolved = append(resolved, item)
	}
	label := fmt.Sprintf("Comments · %d open", len(open))
	return sdui.TreeItem{ID: commentsRootID, Label: label, Detail: ",rl", HasChildren: true, Expanded: !r.CommentsFolded, Children: append(open, resolved...)}
}

func reviewFooter(r *review) sdui.Node {
	done, total := r.progress()
	open := len(r.openComments())
	label := "Send review"
	if open > 0 {
		label = fmt.Sprintf("Send %d %s", open, plural(open, "comment", "comments"))
	}
	variant := sdui.VariantSecondary
	if total > 0 && done == total && open > 0 {
		variant = sdui.VariantPrimary
	}
	send := sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button("review/send", label, "bubble.left"), "send"), variant), "cmd+return")
	if open == 0 || r.Sheet != nil {
		send = sdui.WithDisabled(send)
	}
	copyButton := sdui.WithVariant(sdui.Button("review/copy", "Copy ,re", ""), sdui.VariantGhost)
	if r.Export == "" {
		copyButton = sdui.WithDisabled(copyButton)
	} else {
		copyButton.Props["copy"] = r.Export
	}
	n := sdui.VStack(sdui.MutedText(reviewKeys), sdui.HStack(send, copyButton), sdui.MutedText(reviewLegend))
	n.ID = "review/footer"
	return n
}

func reviewStrip(r *review) sdui.Node {
	done, total := r.progress()
	items := []sdui.Node{
		textStyled("Review", "title"),
		sdui.Progress("review/strip/progress", done, total, ""),
		sdui.Text(fmt.Sprintf("%d/%d", done, total)),
	}
	if open := len(r.openComments()); open > 0 {
		items = append(items, sdui.WithTone(sdui.Text(fmt.Sprintf("%d open", open)), "warning"))
	}
	items = append(items,
		sdui.WithIconOnly(sdui.WithVariant(sdui.WithAction(sdui.Button("review/strip/prev", "Previous file", "chevron.left"), "prev"), sdui.VariantGhost)),
		sdui.Text(stripPosition(r)),
		sdui.WithIconOnly(sdui.WithVariant(sdui.WithAction(sdui.Button("review/strip/next", "Next file", "chevron.right"), "next"), sdui.VariantGhost)),
	)
	n := sdui.HStack(items...)
	n.ID = "review/strip"
	return n
}

func stripPosition(r *review) string {
	files := r.visibleFiles()
	for i, e := range files {
		if e.Path == r.Current {
			return fmt.Sprintf("%s %d of %d", path.Base(e.Path), i+1, len(files))
		}
	}
	return fmt.Sprintf("%d %s", len(files), plural(len(files), "file", "files"))
}

func textStyled(text, style string) sdui.Node {
	return sdui.Node{Type: "text", Props: map[string]any{"text": text, "style": style}}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
