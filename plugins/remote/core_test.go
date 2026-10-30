package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

const alphaManifest = `{"id":"alpha","name":"alpha","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"alpha.say","title":"Say","remote":true}]}}`

func TestRemoteThroughCore(t *testing.T) {
	t.Setenv(listenEnv, "127.0.0.1:0")
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.BuildPluginDir(t, "clubmate/plugins/remote", "plugins/remote/plugin.json"), testutil.EchoPluginDirAs(t, alphaManifest))
	testutil.Eventually(t, func() bool {
		sts := h.Sup.Statuses()
		return len(sts) == 2 && sts[0].State == "running" && sts[1].State == "running"
	}, 10*time.Second)
	c := h.Client(t)
	var pr pairResult
	testutil.Eventually(t, func() bool {
		return c.Call(t.Context(), "command.run", map[string]any{"id": "remote.pair", "projectId": ""}, &pr) == nil
	}, 10*time.Second)
	link, err := url.Parse(pr.Link)
	require.NoError(t, err)
	base := "http://" + net.JoinHostPort(link.Query().Get("h"), link.Query().Get("p"))

	res := post(t, base, "/v1/pair", "", pairRequest{Code: pr.Code, Device: deviceInfo{ID: "phone", Name: "iPhone", Model: "iPhone"}})
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	var paired pairResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&paired))
	var hello helloResult
	testutil.Eventually(t, func() bool {
		rpcCall(t, base, paired.Token, "hello", nil, &hello)
		return len(hello.Commands) > 0
	}, 10*time.Second)
	var said map[string]any
	rpcCall(t, base, paired.Token, "command", map[string]any{"id": "alpha.say", "workspace": p.ID, "args": map[string]any{"text": "hi"}}, &said)
	_, stream := openEvents(t, base, paired.Token, "")
	require.Equal(t, ": connected", frame(t, stream))
	_, err = h.Bus.Publish(t.Context(), "agents.changed", "test", json.RawMessage(`{"workspace":"`+p.ID+`"}`))
	require.NoError(t, err)
	var devices []deviceView
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "remote.devices", "projectId": ""}, &devices))

	assert.Equal(t, []string{"alpha.say"}, hello.Commands)
	assert.Equal(t, "alpha", said["plugin"])
	assert.Contains(t, frame(t, stream), `"workspace":"`+p.ID+`"`)
	require.Len(t, devices, 1)
	assert.True(t, devices[0].Connected)
	assert.True(t, strings.HasPrefix(pr.Link, "clubmate://pair?v=1&c="+pr.Code+"&h=127.0.0.1&p="))
}

func rpcCall(t *testing.T, base, token, method string, params, out any) {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	res := post(t, base, "/v1/rpc", token, rpcRequest{Method: method, Params: raw})
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	var body reply
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	require.Nil(t, body.Error)
	require.NoError(t, json.Unmarshal(body.Result, out))
}
