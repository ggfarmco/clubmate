package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

const (
	maxFileBytes  = 5 << 20
	binarySniff   = 8 << 10
	maxWorkers    = 8
	maxLineRanges = snippetRunes + 1
)

type builtinEngine struct {
	list *listing
}

type globFilter struct {
	include []globPattern
	exclude []globPattern
}

type globPattern struct {
	re       *regexp.Regexp
	anchored bool
}

func (e builtinEngine) run(ctx context.Context, opts grepOptions, emit serialEmit) error {
	if e.list == nil {
		return nil
	}
	re, err := compileQuery(opts)
	if err != nil {
		return err
	}
	globs, err := compileGlobs(opts.Glob)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	paths := e.list.corpus.paths
	next := make(chan int)
	results := make(chan []grepMatch)
	var wg sync.WaitGroup
	wg.Go(func() {
		defer close(next)
		for i := range paths {
			select {
			case next <- i:
			case <-ctx.Done():
				return
			}
		}
	})
	for range min(maxWorkers, runtime.GOMAXPROCS(0)) {
		wg.Go(func() {
			for i := range next {
				if !globs.match(e.list.rel(paths[i])) {
					continue
				}
				found := grepFile(paths[i], e.list.abs(paths[i]), re)
				if len(found) == 0 {
					continue
				}
				select {
				case results <- found:
				case <-ctx.Done():
					return
				}
			}
		})
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	for found := range results {
		if ctx.Err() == nil && !emit(found) {
			cancel()
		}
	}
	return nil
}

func compileQuery(opts grepOptions) (*regexp.Regexp, error) {
	pattern := opts.Query
	if !opts.Regex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if opts.Word {
		pattern = `\b(?:` + pattern + `)\b`
	}
	if !opts.Case {
		pattern = `(?i)` + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}
	return re, nil
}

func grepFile(display, abs string, re *regexp.Regexp) []grepMatch {
	data, ok := readText(abs)
	if !ok {
		return nil
	}
	var out []grepMatch
	for n := 1; len(data) > 0 && len(out) < maxPerFile; n++ {
		line := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			data = nil
		}
		found := re.FindAllIndex(line, maxLineRanges)
		if found == nil {
			continue
		}
		spans := make([][2]int, 0, len(found))
		for _, f := range found {
			spans = append(spans, [2]int{f[0], f[1]})
		}
		out = append(out, newMatch(display, abs, n, line, spans))
	}
	return out
}

func readText(path string) ([]byte, bool) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil || len(data) > maxFileBytes || bytes.IndexByte(data[:min(len(data), binarySniff)], 0) >= 0 {
		return nil, false
	}
	return data, true
}

func compileGlobs(spec string) (globFilter, error) {
	var g globFilter
	for _, raw := range splitGlobs(spec) {
		negated := strings.HasPrefix(raw, "!")
		raw = strings.TrimPrefix(raw, "!")
		anchored := strings.Contains(strings.TrimSuffix(raw, "/"), "/")
		re, err := regexp.Compile("^" + globRegexp(strings.Trim(raw, "/")) + "$")
		if err != nil {
			return globFilter{}, fmt.Errorf("invalid glob %q: %w", raw, err)
		}
		if negated {
			g.exclude = append(g.exclude, globPattern{re: re, anchored: anchored})
		} else {
			g.include = append(g.include, globPattern{re: re, anchored: anchored})
		}
	}
	return g, nil
}

func splitGlobs(spec string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i <= len(spec); i++ {
		switch {
		case i < len(spec) && spec[i] == '{':
			depth++
		case i < len(spec) && spec[i] == '}':
			depth = max(depth-1, 0)
		case i == len(spec) || spec[i] == ',' && depth == 0:
			if g := strings.TrimSpace(spec[start:i]); g != "" {
				out = append(out, g)
			}
			start = i + 1
		}
	}
	return out
}

func globRegexp(glob string) string {
	var b strings.Builder
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString(`(?:.*/)?`)
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(`.*`)
			i++
		case c == '*':
			b.WriteString(`[^/]*`)
		case c == '?':
			b.WriteString(`[^/]`)
		case c == '{':
			b.WriteString(`(?:`)
		case c == '}':
			b.WriteString(`)`)
		case c == ',':
			b.WriteString(`|`)
		case c == '[':
			end := strings.IndexByte(glob[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := glob[i+1 : i+1+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + class + "]")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

func (g globFilter) match(rel string) bool {
	for _, p := range g.exclude {
		if p.matches(rel) {
			return false
		}
	}
	if len(g.include) == 0 {
		return true
	}
	for _, p := range g.include {
		if p.matches(rel) {
			return true
		}
	}
	return false
}

func (p globPattern) matches(rel string) bool {
	if p.anchored {
		for i := range len(rel) {
			if rel[i] == '/' && p.re.MatchString(rel[:i]) {
				return true
			}
		}
		return p.re.MatchString(rel)
	}
	for part := range strings.SplitSeq(rel, "/") {
		if p.re.MatchString(part) {
			return true
		}
	}
	return false
}
