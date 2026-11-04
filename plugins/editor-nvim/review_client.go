package main

import (
	"errors"
	"time"

	"github.com/neovim/go-client/nvim"
)

const reviewOpenTimeout = 15 * time.Second

var errNoReviewTab = errors.New("diffview returned no review tab")

type reviewer interface {
	reviewOpen(o reviewOpenArgs) (int, error)
	reviewInfo(tab int) (reviewInfo, error)
	reviewEntries(tab int) ([]reviewEntry, error)
	reviewComments(tab int) ([]reviewComment, error)
	reviewNext(tab int, dir int) error
	reviewExcludes(tab int, globs []string) error
	reviewLayout(tab int, layout string) error
	reviewClose(tab int) error
	reviewFocus(tab int) error
	reviewFilters(tab int) (reviewFilters, error)
	reviewMeta(tab int, m reviewMeta) error
	reviewCommentsChanged() error
}

type reviewFilter struct {
	Shown bool `msgpack:"shown"`
	Count int  `msgpack:"count"`
}

type reviewFilters struct {
	Code      reviewFilter `msgpack:"code"`
	Tests     reviewFilter `msgpack:"tests"`
	Generated reviewFilter `msgpack:"generated"`
	Reviewed  reviewFilter `msgpack:"reviewed"`
}

type reviewMeta struct {
	Subtitle  string `msgpack:"subtitle"`
	Recipient string `msgpack:"recipient"`
	Sent      int    `msgpack:"sent"`
}

type reviewOpenArgs struct {
	Cwd, Base, Head string
	Left, Right     string
	Excludes        []string
}

type reviewLabels struct {
	Left  string `msgpack:"left" json:"left"`
	Right string `msgpack:"right" json:"right"`
}

type reviewInfo struct {
	Base      string       `msgpack:"base"`
	Head      string       `msgpack:"head"`
	MergeBase string       `msgpack:"merge_base"`
	Editable  bool         `msgpack:"editable"`
	Labels    reviewLabels `msgpack:"labels"`
}

type reviewEntry struct {
	Path      string `msgpack:"path"`
	Status    string `msgpack:"status"`
	Additions int    `msgpack:"additions"`
	Deletions int    `msgpack:"deletions"`
	Reviewed  bool   `msgpack:"reviewed"`
	Stale     bool   `msgpack:"stale"`
	Comments  int    `msgpack:"comments"`
	Category  string `msgpack:"category"`
	Kind      string `msgpack:"kind"`
	OldPath   string `msgpack:"old_path"`
}

type reviewComment struct {
	ID        int64  `msgpack:"id" json:"id"`
	File      string `msgpack:"file" json:"file"`
	Side      string `msgpack:"side" json:"side"`
	LineStart int    `msgpack:"line_start" json:"lineStart"`
	LineEnd   int    `msgpack:"line_end" json:"lineEnd"`
	Type      string `msgpack:"type" json:"type"`
	Text      string `msgpack:"text" json:"text"`
	Status    string `msgpack:"status" json:"status"`
	LineText  string `msgpack:"line_text" json:"lineText"`
}

type luaResult[T any] struct {
	OK    bool   `msgpack:"ok"`
	Value T      `msgpack:"value"`
	Err   string `msgpack:"err"`
}

func reviewCall[T any](c *client, timeout time.Duration, name string, args ...any) (T, error) {
	var res luaResult[T]
	err := c.callWithin(timeout, func(v *nvim.Nvim) error { return v.ExecLua(reviewCallLua, &res, name, args) })
	if err != nil {
		return res.Value, err
	}
	if res.Err != "" {
		return res.Value, errors.New(res.Err)
	}
	return res.Value, nil
}

func reviewDo(c *client, name string, args ...any) error {
	_, err := reviewCall[any](c, rpcTimeout, name, args...)
	return err
}

func (c *client) reviewOpen(o reviewOpenArgs) (int, error) {
	opts := map[string]any{"cwd": o.Cwd}
	if o.Base != "" {
		opts["base"] = o.Base
	}
	if o.Head != "" {
		opts["head"] = o.Head
	}
	labels := map[string]string{}
	if o.Left != "" {
		labels["left"] = o.Left
	}
	if o.Right != "" {
		labels["right"] = o.Right
	}
	if len(labels) > 0 {
		opts["labels"] = labels
	}
	if len(o.Excludes) > 0 {
		opts["excludes"] = o.Excludes
	}
	tab, err := reviewCall[int](c, reviewOpenTimeout, "open", opts)
	if err == nil && tab == 0 {
		err = errNoReviewTab
	}
	return tab, err
}

func (c *client) reviewInfo(tab int) (reviewInfo, error) {
	return reviewCall[reviewInfo](c, rpcTimeout, "info", tab)
}

func (c *client) reviewEntries(tab int) ([]reviewEntry, error) {
	return reviewCall[[]reviewEntry](c, rpcTimeout, "entries", tab)
}

func (c *client) reviewComments(tab int) ([]reviewComment, error) {
	return reviewCall[[]reviewComment](c, rpcTimeout, "comments", tab)
}

func (c *client) reviewNext(tab int, dir int) error {
	return reviewDo(c, "next_unreviewed", dir, tab)
}

func (c *client) reviewExcludes(tab int, globs []string) error {
	if globs == nil {
		globs = []string{}
	}
	return reviewDo(c, "set_excludes", globs, tab)
}

func (c *client) reviewLayout(tab int, layout string) error {
	return reviewDo(c, "set_layout", layout, tab)
}

func (c *client) reviewClose(tab int) error {
	return reviewDo(c, "close", tab)
}

func (c *client) reviewFocus(tab int) error {
	var res luaResult[any]
	if err := c.exec(reviewFocusLua, &res, tab); err != nil {
		return err
	}
	if res.Err != "" {
		return errors.New(res.Err)
	}
	return nil
}

func (c *client) reviewFilters(tab int) (reviewFilters, error) {
	return reviewCall[reviewFilters](c, rpcTimeout, "filters", tab)
}

func (c *client) reviewMeta(tab int, m reviewMeta) error {
	return reviewDo(c, "set_meta", tab, m)
}

func (c *client) reviewCommentsChanged() error {
	return c.exec(commentsChangedLua, nil)
}
