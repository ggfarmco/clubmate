package main_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/sdui"
)

func TestUsageKeepsLatestAndCostAndWarnsInHeader(t *testing.T) {
	usage := func(used int, cost any) map[string]any {
		u := map[string]any{"sessionUpdate": "usage_update", "used": used, "size": 200000}
		if cost != nil {
			u["cost"] = cost
		}
		return map[string]any{"update": u}
	}
	f := start(t, map[string]any{"prompt": []map[string]any{
		usage(20000, map[string]any{"amount": 0.5, "currency": "USD"}),
		usage(60000, nil),
		usage(168000, nil),
	}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "hi")
	f.waitStatus(t, workspace, session, "done")

	var got struct {
		Usage struct {
			Used int `json:"used"`
			Size int `json:"size"`
			Cost struct {
				Amount float64 `json:"amount"`
			} `json:"cost"`
		} `json:"usage"`
	}
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session}, &got))
	assert.Equal(t, 168000, got.Usage.Used)
	assert.Equal(t, 200000, got.Usage.Size)
	assert.InDelta(t, 0.5, got.Usage.Cost.Amount, 0.001)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		warned := slices.ContainsFunc(cardsOf(n, "badge"), func(x sdui.Node) bool { return x.Props["text"] == "84% context" && x.Props["tone"] == "warning" })
		return warned && !slices.ContainsFunc(cardsOf(n, "text"), func(x sdui.Node) bool { return strings.Contains(x.Props["text"].(string), "$0.50") })
	})
	assert.Len(t, updates(t, f.logLines(t, session), "usage_update"), 1)
}

func TestCompactionIsOneEntryWithItsSummary(t *testing.T) {
	compaction := func(fields map[string]any) map[string]any {
		fields["compactionId"] = "c1"
		return map[string]any{"update": fields}
	}
	f := start(t, map[string]any{"prompt": []map[string]any{
		compaction(map[string]any{"sessionUpdate": "compaction_update", "status": "in_progress"}),
		compaction(map[string]any{"sessionUpdate": "compaction_summary_chunk", "content": map[string]any{"type": "text", "text": "<summary>raw"}}),
		compaction(map[string]any{"sessionUpdate": "compaction_summary_chunk", "content": map[string]any{"type": "text", "text": "</summary>"}}),
		compaction(map[string]any{"sessionUpdate": "compaction_update", "status": "completed", "summary": []map[string]any{{"type": "text", "text": "The cart total moved to cartTotal."}}}),
	}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "hi")
	f.waitStatus(t, workspace, session, "done")

	var got struct {
		Entries []struct {
			Kind       string `json:"kind"`
			ID         string `json:"id"`
			Text       string `json:"text"`
			Compaction struct {
				Status string `json:"status"`
			} `json:"compaction"`
		} `json:"entries"`
	}
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session}, &got))
	var compactions []string
	for _, e := range got.Entries {
		if e.Kind == "compaction" {
			compactions = append(compactions, e.ID+" "+e.Compaction.Status+" "+e.Text)
		}
	}
	assert.Equal(t, []string{"c1 completed The cart total moved to cartTotal."}, compactions)
	card := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return slices.ContainsFunc(cardsOf(n, "toolCard"), func(x sdui.Node) bool { return x.ID == session+"/compaction/c1" })
	})
	i := slices.IndexFunc(cardsOf(card, "toolCard"), func(x sdui.Node) bool { return x.ID == session+"/compaction/c1" })
	assert.Equal(t, "Context compacted", cardsOf(card, "toolCard")[i].Props["title"])
	assert.Equal(t, true, cardsOf(card, "toolCard")[i].Props["standalone"])
	assert.Len(t, updates(t, f.logLines(t, session), "compaction_summary_chunk"), 1)
}

func TestBooleanConfigOptionIsAToggleSentTypedAndRestored(t *testing.T) {
	f := start(t, map[string]any{"configs": []map[string]any{
		{"id": "fast", "name": "Fast mode", "category": "model_config", "type": "boolean", "currentValue": false},
	}})
	workspace := f.workspace(t, newRepo(t))
	first := f.newSession(t, workspace)
	toggle := func(session, value string) sdui.Node {
		return f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
			composers := cardsOf(n, "composer")
			if len(composers) != 1 || !strings.HasPrefix(composers[0].ID, session+"/") {
				return false
			}
			list, _ := composers[0].Props["pickers"].([]any)
			return slices.ContainsFunc(list, func(p any) bool {
				m, _ := p.(map[string]any)
				return m["id"] == "fast" && m["toggle"] == true && m["value"] == value
			})
		})
	}
	composer := cardsOf(toggle(first, "false"), "composer")[0].ID

	f.h.Event(t, workspace, "agents.main", composer, "config", map[string]string{"id": "fast", "value": "true"})
	toggle(first, "true")
	second := f.newSession(t, workspace)
	toggle(second, "true")

	inits := f.calls(t, "initialize")
	require.NotEmpty(t, inits)
	var init struct {
		ClientCapabilities struct {
			Session map[string]json.RawMessage `json:"session"`
		} `json:"clientCapabilities"`
	}
	require.NoError(t, json.Unmarshal(inits[0].Params, &init))
	assert.JSONEq(t, `{}`, string(init.ClientCapabilities.Session["compaction"]))
	assert.JSONEq(t, `{"boolean":{}}`, string(init.ClientCapabilities.Session["configOptions"]))
	sets := f.calls(t, "session/set_config_option")
	require.Len(t, sets, 2)
	assert.JSONEq(t, `{"sessionId":"`+f.session(t, workspace, second).AgentSessionID+`","configId":"fast","type":"boolean","value":true}`, string(sets[1].Params))
	require.Error(t, f.run(t, workspace, "agents.config", map[string]any{"session": first, "id": "fast", "value": "on"}, nil))
}
