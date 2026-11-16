package acp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForkSessionRequestWire(t *testing.T) {
	raw, err := json.Marshal(ForkSessionRequest{SessionID: "s1", Cwd: "/w"})
	require.NoError(t, err)
	require.Contains(t, string(raw), `"sessionId":"s1"`)
	require.Contains(t, string(raw), `"cwd":"/w"`)
}
