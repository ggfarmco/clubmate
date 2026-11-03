package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clubmate/plugins/agents/acp"
)

const (
	hitTitle  = "title"
	hitPrompt = "prompt"
	hitReply  = "reply"

	maxSearchResults = 50
	maxSessionHits   = 3
	snippetBefore    = 12
	snippetRunes     = 120
)

var (
	mdLink       = regexp.MustCompile(`!?\[([^\]\n]*)\]\([^)\n]*\)`)
	mdItalic     = regexp.MustCompile(`(^|[\s(])\*([^*\s][^*\n]*)\*`)
	mdHeading    = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	mdMarks      = strings.NewReplacer("**", "", "__", "", "`", "")
	promptMarker = []byte(`"kind":"` + kindPrompt + `"`)
	replyMarker  = []byte(`"` + acp.UpdateAgentMessageChunk + `"`)
)

type searchArgs struct {
	Query string `json:"query"`
}

type searchHit struct {
	Kind    string `json:"kind"`
	Turn    int    `json:"turn,omitempty"`
	Snippet string `json:"snippet"`
}

type searchResult struct {
	Session       string      `json:"session"`
	Title         string      `json:"title"`
	Status        string      `json:"status"`
	Worktree      string      `json:"worktree"`
	ParentSession string      `json:"parentSession,omitempty"`
	UpdatedAt     time.Time   `json:"updatedAt"`
	Hits          []searchHit `json:"hits"`
}

type textIndex struct {
	mu   sync.Mutex
	logs map[string]*indexedLog
}

type indexedLog struct {
	offset int64
	texts  []indexedText
	tail   strings.Builder
	grown  bool
}

type indexedText struct {
	kind  string
	turn  int
	id    string
	text  string
	lower string
}

func (a *app) search(workspaceID, query string) []searchResult {
	query = strings.TrimSpace(query)
	if query == "" {
		return []searchResult{}
	}
	needle := strings.ToLower(query)
	recs := a.list(workspaceID)
	out := []searchResult{}
	for _, rec := range recs {
		var hits []searchHit
		if snippet, ok := match(rec.Title, needle); ok {
			hits = append(hits, searchHit{Kind: hitTitle, Snippet: snippet})
		}
		for _, t := range a.texts.of(logPath(a.stateDir, rec.ID)) {
			if len(hits) == maxSessionHits {
				break
			}
			if snippet, ok := t.match(needle); ok {
				hits = append(hits, searchHit{Kind: t.kind, Turn: t.turn, Snippet: snippet})
			}
		}
		if len(hits) == 0 {
			continue
		}
		out = append(out, searchResult{Session: rec.ID, Title: rec.Title, Status: rec.Status, Worktree: rec.Worktree, ParentSession: rec.ParentSession, UpdatedAt: rec.UpdatedAt, Hits: hits})
	}
	slices.SortStableFunc(out, func(x, y searchResult) int {
		return cmp.Or(boolInt(y.Hits[0].Kind == hitTitle)-boolInt(x.Hits[0].Kind == hitTitle), y.UpdatedAt.Compare(x.UpdatedAt))
	})
	return out[:min(len(out), maxSearchResults)]
}

func (x *textIndex) warm(stateDir string, recs []record) {
	for _, rec := range recs {
		x.of(logPath(stateDir, rec.ID))
	}
}

func (x *textIndex) of(path string) []indexedText {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.logs == nil {
		x.logs = map[string]*indexedLog{}
	}
	l := x.logs[path]
	if l == nil {
		l = &indexedLog{}
		x.logs[path] = l
	}
	if err := l.extend(path); err != nil {
		delete(x.logs, path)
		return nil
	}
	return l.texts
}

func (l *indexedLog) extend(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() == l.offset {
		return nil
	}
	if info.Size() < l.offset {
		*l = indexedLog{}
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(l.offset, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			l.finish()
			return nil
		}
		l.offset += int64(len(line))
		l.add(line)
	}
}

func (l *indexedLog) add(line []byte) {
	prompt := bytes.Contains(line, promptMarker)
	if !prompt && !bytes.Contains(line, replyMarker) {
		return
	}
	var e envelope
	if json.Unmarshal(line, &e) != nil {
		return
	}
	switch {
	case e.Kind == kindPrompt:
		var p struct {
			Prompt []acp.ContentBlock `json:"prompt"`
		}
		if json.Unmarshal(e.Payload, &p) != nil {
			return
		}
		var parts []string
		for _, block := range p.Prompt {
			if block.Type == "text" && block.Text != "" {
				parts = append(parts, block.Text)
			}
		}
		l.append(hitPrompt, e.Turn, "", strings.Join(parts, "\n"))
	case e.Kind == kindUpdate:
		var u struct {
			SessionUpdate string           `json:"sessionUpdate"`
			MessageID     string           `json:"messageId"`
			Content       acp.ContentBlock `json:"content"`
		}
		if json.Unmarshal(e.Payload, &u) != nil || u.SessionUpdate != acp.UpdateAgentMessageChunk {
			return
		}
		if n := len(l.texts); n > 0 {
			last := &l.texts[n-1]
			if last.kind == hitReply && last.turn == e.Turn && last.id == u.MessageID {
				l.tail.WriteString(u.Content.Text)
				l.grown = true
				return
			}
		}
		l.append(hitReply, e.Turn, u.MessageID, u.Content.Text)
	}
}

func (l *indexedLog) append(kind string, turn int, id, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	l.finish()
	l.tail.Reset()
	if kind == hitReply {
		l.tail.WriteString(text)
	}
	plain := plainText(text)
	l.texts = append(l.texts, indexedText{kind: kind, turn: turn, id: id, text: plain, lower: strings.ToLower(plain)})
}

func (l *indexedLog) finish() {
	if !l.grown {
		return
	}
	last := &l.texts[len(l.texts)-1]
	last.text = plainText(l.tail.String())
	last.lower = strings.ToLower(last.text)
	l.grown = false
}

func plainText(md string) string {
	md = mdLink.ReplaceAllString(md, "$1")
	md = mdItalic.ReplaceAllString(md, "$1$2")
	md = mdHeading.ReplaceAllString(md, "")
	return mdMarks.Replace(md)
}

func (t indexedText) match(needle string) (string, bool) {
	if len(t.lower) != len(t.text) {
		return match(t.text, needle)
	}
	i := strings.Index(t.lower, needle)
	if i < 0 {
		return "", false
	}
	return snippet(t.text, i), true
}

func match(text, needle string) (string, bool) {
	lower := strings.ToLower(text)
	i := strings.Index(lower, needle)
	if i < 0 {
		return "", false
	}
	if len(lower) != len(text) {
		i = len(string([]rune(text)[:utf8.RuneCountInString(lower[:i])]))
	}
	return snippet(text, i), true
}

func snippet(text string, at int) string {
	start := at
	for n := 0; n < snippetBefore && start > 0; n++ {
		_, size := utf8.DecodeLastRuneInString(text[:start])
		start -= size
	}
	if nl := strings.LastIndexByte(text[start:at], '\n'); nl >= 0 {
		start += nl + 1
	} else if sp := strings.IndexByte(text[start:at], ' '); start > 0 && sp >= 0 {
		start += sp + 1
	}
	rest := []rune(text[start:])
	out := strings.Join(strings.Fields(string(rest[:min(len(rest), snippetRunes)])), " ")
	if start > 0 && text[start-1] != '\n' {
		out = "…" + out
	}
	if len(rest) > snippetRunes {
		out += "…"
	}
	return out
}
