package automation

import (
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Allowed(tt.cmd, tt.act, tt.parents))
		})
	}
}
