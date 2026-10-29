package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	maxMatches     = 10_000
	maxPerFile     = 1000
	pollBatch      = 500
	pollBytes      = 448 << 10
	pollWait       = time.Second
	snippetRunes   = 512
	snippetLead    = 64
	previewBytes   = snippetRunes * utf8.UTFMax
	maxContext     = 100
	defaultContext = 8
)

var errNotRegular = errors.New("not a regular file")

type grepOptions struct {
	Query string `json:"query"`
	Regex bool   `json:"regex"`
	Case  bool   `json:"case"`
	Word  bool   `json:"word"`
	Glob  string `json:"glob"`
}

type grepMatch struct {
	Path    string   `json:"path"`
	AbsPath string   `json:"absPath"`
	Line    int      `json:"line"`
	Col     int      `json:"col"`
	Text    string   `json:"text"`
	Ranges  [][2]int `json:"ranges"`
}

type pollResult struct {
	ID        string      `json:"id"`
	Matches   []grepMatch `json:"matches"`
	Next      int         `json:"next"`
	Done      bool        `json:"done"`
	Truncated bool        `json:"truncated,omitempty"`
	Indexing  bool        `json:"indexing,omitempty"`
	Error     string      `json:"error,omitempty"`
}

type previewLine struct {
	N    int    `json:"n"`
	Text string `json:"text"`
}

type serialEmit func([]grepMatch) bool

type engine interface {
	run(ctx context.Context, opts grepOptions, emit serialEmit) error
}

type search struct {
	id        string
	workspace string
	indexing  bool
	cancel    context.CancelFunc
	mu        sync.Mutex
	matches   []grepMatch
	done      bool
	cancelled bool
	truncated bool
	freed     bool
	total     int
	err       string
	wake      chan struct{}
}

type searches struct {
	seq         atomic.Int64
	mu          sync.Mutex
	byID        map[string]*search
	byWorkspace map[string]*search
}

func newSearches() *searches {
	return &searches{byID: map[string]*search{}, byWorkspace: map[string]*search{}}
}

func (ss *searches) start(workspace string, prepare func(context.Context, func()) (engine, error), opts grepOptions) string {
	ctx, cancel := context.WithCancel(context.Background())
	s := &search{id: "grep-" + strconv.FormatInt(ss.seq.Add(1), 10), workspace: workspace, cancel: cancel, wake: make(chan struct{})}
	ss.mu.Lock()
	old := ss.byWorkspace[workspace]
	if old != nil {
		delete(ss.byID, old.id)
	}
	ss.byWorkspace[workspace], ss.byID[s.id] = s, s
	ss.mu.Unlock()
	if old != nil {
		old.stop()
	}
	go func() {
		eng, err := prepare(ctx, s.markIndexing)
		s.markReady()
		if err == nil {
			err = eng.run(ctx, opts, s.emit)
		}
		s.finish(ctx, err)
	}()
	return s.id
}

func (ss *searches) poll(ctx context.Context, workspace, id string, after int) pollResult {
	s := ss.lookup(workspace, id)
	if s == nil {
		return pollResult{ID: id, Matches: []grepMatch{}, Next: after, Done: true}
	}
	timer := time.NewTimer(pollWait)
	defer timer.Stop()
	for {
		s.mu.Lock()
		after = min(max(after, 0), s.total)
		if s.cancelled || s.done || s.total > after {
			res := s.page(after)
			s.mu.Unlock()
			return res
		}
		wake := s.wake
		s.mu.Unlock()
		select {
		case <-wake:
		case <-timer.C:
		case <-ctx.Done():
		}
		s.mu.Lock()
		res := s.page(min(max(after, 0), s.total))
		s.mu.Unlock()
		return res
	}
}

func (ss *searches) cancel(workspace, id string) {
	ss.mu.Lock()
	s := ss.byID[id]
	if s != nil && s.workspace == workspace {
		delete(ss.byID, id)
		delete(ss.byWorkspace, workspace)
	} else {
		s = nil
	}
	ss.mu.Unlock()
	if s != nil {
		s.stop()
	}
}

func (ss *searches) lookup(workspace, id string) *search {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if s := ss.byID[id]; s != nil && s.workspace == workspace {
		return s
	}
	return nil
}

func (ss *searches) drop(workspace string) {
	ss.mu.Lock()
	s := ss.byWorkspace[workspace]
	if s != nil {
		delete(ss.byWorkspace, workspace)
		delete(ss.byID, s.id)
	}
	ss.mu.Unlock()
	if s != nil {
		s.stop()
	}
}

func collect(ctx context.Context, eng engine, opts grepOptions, limit int) ([]grepMatch, bool, error) {
	inner, cancel := context.WithCancel(ctx)
	defer cancel()
	out := []grepMatch{}
	truncated := false
	size := 0
	err := eng.run(inner, opts, func(batch []grepMatch) bool {
		for _, m := range batch {
			raw, err := json.Marshal(m)
			if err != nil || len(out) >= limit || size+len(raw) > pollBytes {
				truncated = true
				cancel()
				return false
			}
			size += len(raw)
			out = append(out, m)
		}
		return true
	})
	if truncated || ctx.Err() != nil || errors.Is(err, errTruncated) {
		return out, true, nil
	}
	return out, false, err
}

func (s *search) emit(batch []grepMatch) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelled {
		return false
	}
	for _, m := range batch {
		if s.total >= maxMatches {
			s.truncated = true
			s.signal()
			return false
		}
		s.matches = append(s.matches, m)
		s.total++
	}
	s.signal()
	return true
}

func (s *search) finish(ctx context.Context, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = true
	switch {
	case errors.Is(err, errTruncated):
		s.truncated = true
	case err != nil && ctx.Err() == nil:
		s.err = err.Error()
	}
	s.signal()
	s.cancel()
}

func (s *search) markIndexing() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexing = true
	s.signal()
}

func (s *search) markReady() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexing = false
}

func (s *search) stop() {
	s.mu.Lock()
	s.cancelled = true
	s.signal()
	s.mu.Unlock()
	s.cancel()
}

func (s *search) signal() {
	close(s.wake)
	s.wake = make(chan struct{})
}

func (s *search) page(after int) pollResult {
	res := pollResult{ID: s.id, Matches: []grepMatch{}, Next: after}
	if s.cancelled {
		res.Done = true
		return res
	}
	if !s.done {
		res.Indexing = s.indexing
	}
	if s.freed {
		res.Next, res.Done, res.Truncated, res.Error = s.total, true, s.truncated, s.err
		return res
	}
	size := 0
	for _, m := range s.matches[after:] {
		if len(res.Matches) == pollBatch {
			break
		}
		raw, err := json.Marshal(m)
		if err != nil || size+len(raw) > pollBytes && len(res.Matches) > 0 {
			break
		}
		size += len(raw)
		res.Matches = append(res.Matches, m)
	}
	res.Next = after + len(res.Matches)
	res.Done = s.done && res.Next == s.total
	res.Truncated = s.truncated
	if res.Done {
		res.Error = s.err
		s.matches, s.freed = nil, true
	}
	return res
}

func newMatch(display, abs string, line int, raw []byte, spans [][2]int) grepMatch {
	spans = mergeSpans(spans)
	col := 1
	if len(spans) > 0 {
		col = spans[0][0] + 1
	}
	text, ranges := snippet(raw, spans)
	return grepMatch{Path: display, AbsPath: abs, Line: line, Col: col, Text: text, Ranges: ranges}
}

func mergeSpans(spans [][2]int) [][2]int {
	if len(spans) < 2 {
		return spans
	}
	sorted := slices.Clone(spans)
	slices.SortFunc(sorted, func(a, b [2]int) int { return cmp.Compare(a[0], b[0]) })
	out := sorted[:1]
	for _, sp := range sorted[1:] {
		if last := &out[len(out)-1]; sp[0] <= last[1] {
			last[1] = max(last[1], sp[1])
			continue
		}
		out = append(out, sp)
	}
	return out
}

func snippet(raw []byte, spans [][2]int) (string, [][2]int) {
	for len(raw) > 0 && (raw[len(raw)-1] == '\n' || raw[len(raw)-1] == '\r') {
		raw = raw[:len(raw)-1]
	}
	from, to := 0, len(raw)
	if len(raw) > snippetRunes && utf8.RuneCount(raw) > snippetRunes {
		anchor := 0
		if len(spans) > 0 {
			anchor = min(spans[0][0], len(raw))
		}
		from = runesBack(raw, anchor, snippetLead)
		to = runesForward(raw, from, snippetRunes)
		if to == len(raw) {
			from = runesBack(raw, to, snippetRunes)
		}
	}
	ranges := make([][2]int, 0, len(spans))
	for _, sp := range spans {
		if sp[1] < from || sp[0] > to {
			continue
		}
		start, end := min(max(sp[0], from), to), min(max(sp[1], from), to)
		ranges = append(ranges, [2]int{utf8.RuneCount(raw[from:start]), utf8.RuneCount(raw[from:end])})
	}
	return string([]rune(string(raw[from:to]))), ranges
}

func runesBack(raw []byte, at, n int) int {
	for ; n > 0 && at > 0; n-- {
		_, size := utf8.DecodeLastRune(raw[:at])
		at -= size
	}
	return at
}

func runesForward(raw []byte, at, n int) int {
	for ; n > 0 && at < len(raw); n-- {
		_, size := utf8.DecodeRune(raw[at:])
		at += size
	}
	return at
}

func preview(path string, line, around int) ([]previewLine, error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	first, last := max(line-around, 1), line+around
	r := bufio.NewReader(io.LimitReader(f, maxFileBytes))
	out := []previewLine{}
	for n := 1; n <= last; n++ {
		raw, err := readLine(r)
		if err != nil && len(raw) == 0 {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, err
		}
		if n >= first {
			text, _ := snippet(raw, nil)
			out = append(out, previewLine{N: n, Text: text})
		}
	}
	return out, nil
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if room := previewBytes - len(line); room > 0 {
			line = append(line, chunk[:min(len(chunk), room)]...)
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}
