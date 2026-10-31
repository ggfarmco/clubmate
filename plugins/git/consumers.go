package main

import (
	"context"
	"errors"
	"slices"

	"clubmate/pkg/pluginsdk"
)

const (
	topicRemoving  = "git.worktree.removing"
	topicVacating  = "git.worktree.vacating"
	topicReleased  = "git.worktree.released"
	topicSwitching = "git.worktree.switching"
	topicSwitched  = "git.worktree.switched"
)

type consumers struct {
	h         *pluginsdk.Host
	projectID string
}

type worktreeEvent struct {
	Path  string `json:"path,omitempty"`
	Root  string `json:"root"`
	Force bool   `json:"force,omitempty"`
}

type handlerInfo struct {
	Plugin  string   `json:"plugin"`
	Handles []string `json:"handles"`
}

func (c consumers) switching(ctx context.Context, root string) error {
	return refusal(c.h.Broadcast(ctx, topicSwitching, c.projectID, worktreeEvent{Root: root}))
}

func (c consumers) switched(ctx context.Context, root string) error {
	return c.h.Broadcast(ctx, topicSwitched, c.projectID, worktreeEvent{Root: root})
}

func (c consumers) removing(ctx context.Context, path, root string, force bool) error {
	return refusal(c.h.Broadcast(ctx, topicRemoving, c.projectID, worktreeEvent{Path: path, Root: root, Force: force}))
}

func (c consumers) vacating(ctx context.Context, path, root string) error {
	return c.h.Broadcast(ctx, topicVacating, c.projectID, worktreeEvent{Path: path, Root: root})
}

func (c consumers) released(ctx context.Context, path, root string) error {
	return c.h.Broadcast(ctx, topicReleased, c.projectID, worktreeEvent{Path: path, Root: root})
}

func removingOwners(ctx context.Context, h *pluginsdk.Host) (map[string]bool, error) {
	var cmds []handlerInfo
	if err := h.Call(ctx, "command.list", struct{}{}, &cmds); err != nil {
		return nil, err
	}
	owners := map[string]bool{}
	for _, c := range cmds {
		if slices.Contains(c.Handles, topicRemoving) {
			owners[c.Plugin] = true
		}
	}
	return owners, nil
}

func refusal(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(rpcMessage(err))
}
