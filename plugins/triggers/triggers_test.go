package main_test

import (
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const wait = 15 * time.Second

type trigger struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	History []struct {
		Topic  string `json:"topic"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"history"`
}

type fixture struct {
	h       *testutil.PluginHarness
	project string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	h := testutil.NewPluginHarness(t,
		testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"),
	)
	f := &fixture{h: h, project: h.CreateProject(t, "demo").ID}
	testutil.Eventually(t, func() bool {
		return f.run(t, "triggers.list", map[string]any{}, nil) == nil && f.run(t, "cron.list", map[string]any{}, nil) == nil
	}, wait)
	return f
}

func (f *fixture) run(t *testing.T, command string, args, out any) error {
	t.Helper()
	return f.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": command, "projectId": f.project, "args": args}, out)
}

func TestCronEmitFiresTrigger(t *testing.T) {
	f := newFixture(t)
	var added trigger
	require.NoError(t, f.run(t, "triggers.add", map[string]any{
		"name":    "relay",
		"on":      "event.ping",
		"match":   `{"n":"7"}`,
		"command": "notify.post",
		"args":    `{"message":"n={{payload.n}}"}`,
	}, &added))
	var job struct {
		ID string `json:"id"`
	}
	require.NoError(t, f.run(t, "cron.add", map[string]any{
		"name":    "tick",
		"command": "triggers.emit",
		"args":    `{"name":"ping","payload":"{\"n\":\"7\"}"}`,
		"every":   "1h",
	}, &job))
	require.NoError(t, f.run(t, "cron.run", map[string]any{"id": job.ID}, nil))

	var shown trigger
	testutil.Eventually(t, func() bool {
		require.NoError(t, f.run(t, "triggers.show", map[string]any{"id": added.ID}, &shown))
		return len(shown.History) > 0
	}, wait)
	assert.Equal(t, "event.ping", shown.History[0].Topic)
	assert.Equal(t, "ok", shown.History[0].Status, shown.History[0].Reason)
	var events []struct {
		Payload struct {
			Message string `json:"message"`
		} `json:"payload"`
	}
	require.NoError(t, f.h.Client(t).Call(t.Context(), "bus.read", map[string]any{"pattern": "notify.posted"}, &events))
	require.Len(t, events, 1)
	assert.Equal(t, "n=7", events[0].Payload.Message)

	f.h.WaitSlot(t, f.project, "triggers.sidebar", func(n sdui.Node) bool { return firstIcon(n) == "bolt" })
	f.h.Event(t, f.project, "triggers.sidebar", "triggers", "pause", added.ID)
	f.h.WaitSlot(t, f.project, "triggers.sidebar", func(n sdui.Node) bool { return firstIcon(n) == "pause.circle" })
}

func TestAddRejectsLoopsAndCatchAll(t *testing.T) {
	f := newFixture(t)
	base := map[string]any{"name": "x", "on": "event.ping", "command": "notify.post", "args": `{"message":"hi"}`}
	tests := []struct {
		name  string
		field string
		value string
		want  string
	}{
		{"own command", "command", "triggers.emit", `command "triggers.emit" cannot be run by a trigger`},
		{"cron command", "command", "cron.add", `command "cron.add" cannot be run by a trigger`},
		{"catch-all", "on", "*", `"*" is not allowed`},
		{"bad match", "match", `{"a..b":1}`, `match: bad field path "a..b"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := maps.Clone(base)
			args[tt.field] = tt.value
			assert.ErrorContains(t, f.run(t, "triggers.add", args, nil), tt.want)
		})
	}
}

func firstIcon(n sdui.Node) string {
	items, _ := n.Props["items"].([]any)
	if len(items) == 0 {
		return ""
	}
	item, _ := items[0].(map[string]any)
	icon, _ := item["icon"].(string)
	return icon
}
