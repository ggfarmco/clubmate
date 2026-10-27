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
	if !found {
		return true
	}
	if rest == "HEAD" || rest == "index" || strings.HasPrefix(rest, "refs/heads/") {
		return true
	}
	name, file, ok := strings.Cut(strings.TrimPrefix(rest, "worktrees/"), "/")
	return strings.HasPrefix(rest, "worktrees/") && ok && name != "" && (file == "HEAD" || file == "index")
}

func Watch(roots []string, fn func(paths []string, overflow bool)) (func(), error) {
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
		pending := newPending()
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
	seen     map[string]struct{}
	paths    []string
	overflow bool
}

func newPending() *pending {
	return &pending{seen: map[string]struct{}{}}
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
		if _, ok := p.seen[ev.Path]; ok || !Keep(ev.Path) {
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
	*p = *newPending()
}
