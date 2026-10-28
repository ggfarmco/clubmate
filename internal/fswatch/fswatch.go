package fswatch

import (
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsevents"
)

const (
	latency      = 200 * time.Millisecond
	overflowOver = 1000
	gitDir       = "/.git/"
	lossy        = fsevents.MustScanSubDirs | fsevents.KernelDropped | fsevents.UserDropped
)

func Keep(path string) bool {
	_, rest, found := strings.Cut(path, gitDir)
	return !found || keepGit(rest)
}

func keepGit(rest string) bool {
	if rest == "HEAD" || rest == "index" || rest == "packed-refs" ||
		strings.HasPrefix(rest, "refs/heads/") || strings.HasPrefix(rest, "refs/remotes/") {
		return true
	}
	linked, ok := strings.CutPrefix(rest, "worktrees/")
	name, file, hasFile := strings.Cut(linked, "/")
	return ok && hasFile && name != "" && (file == "HEAD" || file == "index")
}

func keeper(gitRoots []string) func(string) bool {
	return func(path string) bool {
		for _, root := range gitRoots {
			if rest, ok := strings.CutPrefix(path, root+"/"); ok {
				return keepGit(rest)
			}
		}
		return Keep(path)
	}
}

func Watch(roots, gitRoots []string, fn func(paths []string, overflow bool)) (func(), error) {
	es := &fsevents.EventStream{
		Paths:   roots,
		Latency: latency,
		Flags:   fsevents.FileEvents,
		Events:  make(chan []fsevents.Event),
	}
	if err := es.Start(); err != nil {
		return nil, err
	}
	quit := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		keep := keeper(gitRoots)
		pending := newPending(keep)
		var flush <-chan time.Time
		for {
			select {
			case batch := <-es.Events:
				pending.add(batch)
				if flush == nil && !pending.empty() {
					flush = time.After(latency)
				}
			case <-flush:
				flush = nil
				pending.emit(fn)
			case <-quit:
				stopped := make(chan struct{})
				go func() {
					defer close(stopped)
					es.Stop()
				}()
				for {
					select {
					case <-es.Events:
					case <-stopped:
						return
					}
				}
			}
		}
	}()
	return sync.OnceFunc(func() {
		close(quit)
		<-finished
	}), nil
}

type pending struct {
	keep     func(string) bool
	seen     map[string]struct{}
	paths    []string
	overflow bool
}

func newPending(keep func(string) bool) *pending {
	return &pending{keep: keep, seen: map[string]struct{}{}}
}

func (p *pending) empty() bool {
	return !p.overflow && len(p.paths) == 0
}

func (p *pending) add(batch []fsevents.Event) {
	for _, ev := range batch {
		if p.overflow {
			return
		}
		if ev.Flags&lossy != 0 {
			p.setOverflow()
			return
		}
		if _, ok := p.seen[ev.Path]; ok || !p.keep(ev.Path) {
			continue
		}
		p.seen[ev.Path] = struct{}{}
		p.paths = append(p.paths, ev.Path)
		if len(p.paths) > overflowOver {
			p.setOverflow()
		}
	}
}

func (p *pending) setOverflow() {
	p.overflow = true
	p.paths = nil
	p.seen = nil
}

func (p *pending) emit(fn func(paths []string, overflow bool)) {
	if p.overflow {
		fn(nil, true)
	} else if len(p.paths) > 0 {
		fn(p.paths, false)
	}
	*p = *newPending(p.keep)
}
