package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
)

func BenchmarkTickDelta(b *testing.B) {
	before, after := tickTrees(b)
	base, err := sdui.IndexTree(before)
	if err != nil {
		b.Fatal(err)
	}
	var delta []byte
	b.ReportAllocs()
	for b.Loop() {
		next, err := sdui.IndexTree(after)
		if err != nil {
			b.Fatal(err)
		}
		delta, _ = next.DeltaFrom(base, sdui.DeltaOptions{Appends: true, Keeps: true})
	}
	b.ReportMetric(float64(len(after)), "tree-B")
	b.ReportMetric(float64(len(delta)), "delta-B")
}

func BenchmarkRenderJSON(b *testing.B) {
	_, tree := tickTrees(b)
	store := internalsdui.NewStore()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := store.RenderJSON(context.Background(), "p", "processes.main", "processes", tree); err != nil {
			b.Fatal(err)
		}
	}
}

func tickTrees(b *testing.B) ([]byte, []byte) {
	sessions, nodes, _, now := benchFixture()
	for i := range nodes {
		if i%10 != 0 {
			nodes[i].Process.CPU = 0
		}
	}
	hist := newHistory()
	start := now.Add(-historyLen * time.Second)
	for i := range historyLen {
		hist.record(sessions, nodes, nil, nil, start.Add(time.Duration(i)*time.Second))
	}
	render := func(now time.Time) []byte {
		m := benchModel(colCPU)
		m.nodes, m.hist, m.now = nodes, hist, now
		view, _ := mainView(m)
		raw, err := json.Marshal(view)
		if err != nil {
			b.Fatal(err)
		}
		return raw
	}
	before := render(now)
	for i := range nodes {
		if i%10 == 0 {
			nodes[i].Process.CPU += 1
		}
	}
	hist.record(sessions, nodes, nil, nil, now.Add(time.Second))
	return before, render(now.Add(time.Second))
}
