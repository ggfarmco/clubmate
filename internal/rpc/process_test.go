package rpc_test

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/core"
	"clubmate/internal/proc"
	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

func TestProcessRegister(t *testing.T) {
	srv, _ := testutil.NewRPCServer(t)
	plugin := connectAs(t, srv, rpc.Caller{PluginID: "agents", PID: os.Getpid()})
	var prj struct {
		ID string `json:"id"`
	}
	require.NoError(t, connectAs(t, srv, rpc.Caller{}).Call(t.Context(), "project.create", map[string]any{"name": "demo"}, &prj))

	child, exited := startSleep(t)
	other, _ := startSleep(t)

	foreign := connectAs(t, srv, rpc.Caller{PluginID: "other", PID: other.Process.Pid})
	err := foreign.Call(t.Context(), "process.register", map[string]any{"projectId": prj.ID, "pid": child.Process.Pid, "label": "x"}, nil)
	var rpcErr *jsonrpc2.Error
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, int64(jsonrpc2.CodeInvalidParams), rpcErr.Code)

	var root core.ProcessRoot
	require.NoError(t, plugin.Call(t.Context(), "process.register", map[string]any{"projectId": prj.ID, "pid": child.Process.Pid, "label": "Claude · repo"}, &root))
	assert.Equal(t, "agents", root.Owner)

	var nodes []proc.Node
	require.NoError(t, plugin.Call(t.Context(), "process.tree", map[string]any{"projectId": prj.ID, "sessionId": root.ID}, &nodes))
	require.Len(t, nodes, 1)
	assert.Equal(t, child.Process.Pid, nodes[0].Process.PID)
	assert.False(t, nodes[0].Orphan)

	var killed struct {
		Killed []int `json:"killed"`
	}
	require.NoError(t, plugin.Call(t.Context(), "process.kill", map[string]any{"sessionId": root.ID, "pids": []int{child.Process.Pid}}, &killed))
	assert.Equal(t, []int{child.Process.Pid}, killed.Killed)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "registered child survived kill")
	}
}

func startSleep(t *testing.T) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-exited })
	return cmd, exited
}
