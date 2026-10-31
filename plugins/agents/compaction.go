package main

import (
	"encoding/json"
	"strings"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

const (
	compactionRunning   = "in_progress"
	compactionCompleted = "completed"
	compactionFailed    = "failed"
	compactionCancelled = "cancelled"
)

var compactionTitles = map[string]string{
	compactionRunning:   "Compacting context…",
	compactionCompleted: "Context compacted",
	compactionFailed:    "Context compaction failed",
	compactionCancelled: "Context compaction cancelled",
}

type compaction struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (s *session) applyUsage(payload json.RawMessage) {
	var u acp.UsageUpdate
	if json.Unmarshal(payload, &u) != nil || u.Size <= 0 {
		return
	}
	if u.Cost == nil && s.usage != nil {
		u.Cost = s.usage.Cost
	}
	s.usage = &u
}

func (s *session) applyCompaction(kind string, turn int, payload json.RawMessage) {
	if kind == acp.UpdateCompactionChunk {
		var c acp.CompactionSummaryChunk
		if json.Unmarshal(payload, &c) == nil && c.CompactionID != "" && c.Content.Type == "text" {
			s.compactionEntry(c.CompactionID, turn).Text += c.Content.Text
		}
		return
	}
	var u acp.CompactionUpdate
	if json.Unmarshal(payload, &u) != nil || u.CompactionID == "" {
		return
	}
	en := s.compactionEntry(u.CompactionID, turn)
	next := *en.Compaction
	next.Status = u.Status
	if u.Error != "" {
		next.Error = u.Error
	}
	en.Compaction = &next
	var summary strings.Builder
	for _, b := range u.Summary {
		if b.Type == "text" {
			summary.WriteString(b.Text)
		}
	}
	if summary.Len() > 0 {
		en.Text = summary.String()
	}
}

func (s *session) compactionEntry(id string, turn int) *entry {
	en := s.upsert(entryCompaction, id, turn)
	if en.Compaction == nil {
		en.Compaction = &compaction{Status: compactionRunning}
	}
	return en
}

func compactionCard(id string, en *entry) sdui.Node {
	status := en.Compaction.Status
	title, ok := compactionTitles[status]
	if !ok {
		status, title = compactionCompleted, compactionTitles[compactionCompleted]
	}
	p := sdui.ToolCardProps{Kind: "think", Title: title, Status: status, Standalone: true}
	if text := strings.TrimSpace(strings.Join(nonEmpty(en.Compaction.Error, en.Text), "\n\n")); text != "" {
		p.Body = &sdui.ToolBody{Format: "text", Text: text}
	}
	return sdui.ToolCard(id, p)
}
