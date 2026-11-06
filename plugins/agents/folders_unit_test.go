package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionBlocksFolder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status string
		queue  []queuedPrompt
		setup  *setup
		want   bool
	}{
		{name: "running", status: statusRunning, want: true},
		{name: "waiting", status: statusWaiting, want: true},
		{name: "queued", status: statusDone, queue: []queuedPrompt{{ID: "q1"}}, want: true},
		{name: "worktree on the way", status: statusIdle, setup: newSetup(worktreeNewArgs{}), want: true},
		{name: "done", status: statusDone},
		{name: "idle", status: statusIdle},
		{name: "closed", status: statusClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newSession(record{Status: tt.status}, nil, true, nil)
			s.queue, s.setup = tt.queue, tt.setup

			assert.Equal(t, tt.want, s.blocksFolder())
		})
	}
}

func TestWorkspacesWithClonesResumeOnce(t *testing.T) {
	t.Parallel()
	cloning := connectRepo{URL: "u", State: repoCloning, Op: "op1", Folder: "/a"}
	recs := []record{
		{Workspace: "w", ConnectRequests: []connectRequest{{Repos: []connectRepo{cloning, {Path: "/p", State: repoPending}}}}},
		{Workspace: "w", ConnectRequests: []connectRequest{{Repos: []connectRepo{cloning}}}},
		{Workspace: "idle", ConnectRequests: []connectRequest{{Repos: []connectRepo{{Path: "/p", State: repoPending}}}}},
	}

	assert.Equal(t, []string{"w"}, workspacesCloning(recs))
}

func TestOpenTaskKeepsTheSessionFromClosing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		state string
		want  bool
	}{
		{landWaits, true},
		{landPROpen, true},
		{landFailed, true},
		{landMerged, false},
		{landOnBranch, false},
		{landLeft, false},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			t.Parallel()
			task := taskRecord{Repos: []taskRepo{{State: tt.state}}}

			assert.Equal(t, tt.want, task.open())
		})
	}
}
