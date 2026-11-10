package automation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/pluginsdk"
)

func TestAllowed(t *testing.T) {
	chain := map[string]string{"agt_c": "agt_b", "agt_b": "agt_a"}
	tests := []struct {
		name    string
		cmd     pluginsdk.CommandRun
		act     Action
		parents map[string]string
		want    bool
	}{
		{"ancestor of creator", pluginsdk.CommandRun{Agent: true, Caller: "agt_a"}, Action{Owner: "agt_x", CreatedBy: "agt_c"}, chain, true},
		{"unrelated", pluginsdk.CommandRun{Agent: true, Caller: "agt_x"}, Action{Owner: "agt_c"}, chain, false},
		{"cycle", pluginsdk.CommandRun{Agent: true, Caller: "agt_x"}, Action{Owner: "agt_a"}, map[string]string{"agt_a": "agt_b", "agt_b": "agt_a"}, false},
		{"caller-less vs owned", pluginsdk.CommandRun{Agent: true}, Action{Owner: "agt_a", CreatedBy: "agt_a"}, chain, false},
		{"user", pluginsdk.CommandRun{}, Action{Owner: "agt_a"}, nil, true},
		{"plugin-added", pluginsdk.CommandRun{Agent: true, Caller: "agt_x"}, Action{CreatedBy: "plugin:remote"}, nil, true},
		{"plugin-added, no caller", pluginsdk.CommandRun{Agent: true}, Action{CreatedBy: "plugin:remote"}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Allowed(tt.cmd, tt.act, tt.parents))
		})
	}
}

func TestExecuteNamesOriginOfNewSessions(t *testing.T) {
	var got []string
	x := Executor{Origin: OriginCronJob, RunCommand: func(_ context.Context, req CommandRequest) (json.RawMessage, error) {
		got = append(got, string(req.Args))
		return nil, nil
	}}
	x.Execute(t.Context(), Action{ID: "cj_1", Command: agentNew, CreatedBy: "agt_a", Args: json.RawMessage(`{"prompt":"tidy","jobBy":""}`)}, Run{})
	x.Execute(t.Context(), Action{ID: "cj_1", Command: agentNew, Args: json.RawMessage(`{"jobBy":"agt_a"}`)}, Run{})
	x.Execute(t.Context(), Action{ID: "cj_1", Command: PromptCommand, Args: json.RawMessage(`{"session":"agt_1"}`)}, Run{})
	assert.Equal(t, []string{`{"cronJob":"cj_1","jobBy":"agt_a","prompt":"tidy"}`, `{"cronJob":"cj_1","jobBy":""}`, `{"session":"agt_1"}`}, got)
}
