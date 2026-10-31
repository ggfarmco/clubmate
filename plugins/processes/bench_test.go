package main

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const benchProcesses = 300

func BenchmarkRecord(b *testing.B) {
	sessions, nodes, hist, now := benchFixture()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		hist.record(sessions, nodes, now.Add(time.Duration(i)*time.Second))
	}
}

func BenchmarkMainView(b *testing.B) {
	for _, column := range []string{colCPU, colHistory, colCommand} {
		b.Run(column, func(b *testing.B) {
			m := benchModel(column)
			b.ReportAllocs()
			for b.Loop() {
				mainView(m)
			}
		})
	}
}

func BenchmarkMainViewJSON(b *testing.B) {
	m := benchModel(colCPU)
	b.ReportAllocs()
	for b.Loop() {
		view, _ := mainView(m)
		if _, err := json.Marshal(view); err != nil {
			b.Fatal(err)
		}
	}
}

func benchModel(column string) viewModel {
	_, nodes, hist, now := benchFixture()
	return viewModel{
		nodes:     nodes,
		selected:  nodes[len(nodes)/2].Process.PID,
		sort:      sdui.Sort{Column: column, Desc: true},
		collapsed: map[int]bool{},
		scope:     "All",
		totalKey:  projectKey("p"),
		hist:      hist,
		memKiB:    1 << 25,
		now:       now,
	}
}

func benchFixture() ([]group, []pluginsdk.ProcessNode, *history, time.Time) {
	now := time.Now()
	sessions := make([]group, 0, 10)
	for i := range cap(sessions) {
		sessions = append(sessions, group{ID: "s" + strconv.Itoa(i), ProjectID: "p", Title: "session " + strconv.Itoa(i)})
	}
	nodes := make([]pluginsdk.ProcessNode, benchProcesses)
	for i := range nodes {
		pid := 1000 + i
		ppid := 1000 + (i-1)/3
		if i%30 == 0 {
			ppid = 1
		}
		nodes[i] = pluginsdk.ProcessNode{
			Process: pluginsdk.ProcessInfo{
				PID:       pid,
				PPID:      ppid,
				Comm:      "proc" + strconv.Itoa(i),
				Args:      []string{"/usr/local/bin/proc" + strconv.Itoa(i), "--flag", "value", "--path=/Users/me/project/src/" + strconv.Itoa(i)},
				StartTime: now.Add(-time.Duration(i) * time.Minute),
				RSSKiB:    int64(1024 * (i%200 + 1)),
				CPU:       float64(i % 37),
			},
			SessionID: sessions[i%len(sessions)].ID,
			Orphan:    i%50 == 0,
		}
	}
	hist := newHistory()
	start := now.Add(-historyLen * time.Second)
	for i := range historyLen {
		hist.record(sessions, nodes, start.Add(time.Duration(i)*time.Second))
	}
	return sessions, nodes, hist, now
}
