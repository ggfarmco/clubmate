package main

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"clubmate/pkg/pluginsdk"
)

const (
	quickfixMax   = 10000
	quickfixText  = 512
	quickfixFiles = 1000
	quickfixPath  = 4096
	quickfixBytes = 2 << 20
)

var (
	errQuickfixCount = fmt.Errorf("quickfix: items must hold 1 to %d entries", quickfixMax)
	errQuickfixTitle = fmt.Errorf("quickfix: title must be at most %d chars without line breaks or NUL", quickfixText)
	errQuickfixPath  = fmt.Errorf("quickfix: path must be absolute, at most %d bytes, without line breaks or NUL", quickfixPath)
	errQuickfixLine  = fmt.Errorf("quickfix: line must be 1 to %d", math.MaxInt32)
	errQuickfixCol   = fmt.Errorf("quickfix: col must be 0 (none) or 1 to %d", math.MaxInt32)
	errQuickfixText  = fmt.Errorf("quickfix: text must be at most %d chars without NUL", quickfixText)
	errQuickfixSize  = fmt.Errorf("quickfix: items hold more than %d MiB of paths and text; send fewer", quickfixBytes>>20)
)

type quickfixArgs struct {
	Title string         `json:"title"`
	Items []quickfixItem `json:"items"`
}

type quickfixItem struct {
	Path string `json:"path" msgpack:"filename"`
	Line int    `json:"line" msgpack:"lnum"`
	Col  int    `json:"col" msgpack:"col"`
	Text string `json:"text" msgpack:"text"`
}

func (a *app) quickfix(ctx context.Context, h *pluginsdk.Host, projectID string, args quickfixArgs) error {
	if err := validQuickfix(args); err != nil {
		return err
	}
	e, c, err := a.attach(ctx, h, projectID, args.Items[0].Path)
	if c == nil || err != nil {
		return err
	}
	title, items := capFiles(args)
	if err := c.quickfix(title, items); err != nil {
		return err
	}
	focusMain(ctx, h, e.id)
	return nil
}

func validQuickfix(args quickfixArgs) error {
	if len(args.Items) == 0 || len(args.Items) > quickfixMax {
		return errQuickfixCount
	}
	if utf8.RuneCountInString(args.Title) > quickfixText || strings.ContainsAny(args.Title, "\n\r\x00") {
		return errQuickfixTitle
	}
	size := 0
	for i, it := range args.Items {
		size += len(it.Path) + len(it.Text)
		if size > quickfixBytes {
			return errQuickfixSize
		}
		var err error
		switch {
		case !filepath.IsAbs(it.Path) || len(it.Path) > quickfixPath || strings.ContainsAny(it.Path, "\n\r\x00"):
			err = errQuickfixPath
		case it.Line < 1 || it.Line > math.MaxInt32:
			err = errQuickfixLine
		case it.Col < 0 || it.Col > math.MaxInt32:
			err = errQuickfixCol
		case utf8.RuneCountInString(it.Text) > quickfixText || strings.ContainsRune(it.Text, 0):
			err = errQuickfixText
		}
		if err != nil {
			return fmt.Errorf("%w: items[%d]", err, i)
		}
	}
	return nil
}

func capFiles(args quickfixArgs) (string, []quickfixItem) {
	files := map[string]bool{}
	items := make([]quickfixItem, 0, len(args.Items))
	for _, it := range args.Items {
		if !files[it.Path] {
			if len(files) == quickfixFiles {
				continue
			}
			files[it.Path] = true
		}
		items = append(items, it)
	}
	if len(items) < len(args.Items) {
		return fmt.Sprintf("%s (first %d files)", args.Title, quickfixFiles), items
	}
	return args.Title, items
}
