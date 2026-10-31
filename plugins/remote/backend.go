package main

import (
	"context"
	"encoding/json"

	"clubmate/pkg/pluginsdk"
)

const busPageSize = 1000

type backend interface {
	Commands(ctx context.Context) ([]commandInfo, error)
	Run(ctx context.Context, id, workspace string, args json.RawMessage) (json.RawMessage, error)
	Workspaces(ctx context.Context) ([]pluginsdk.Project, error)
	Events(ctx context.Context, after int64) ([]pluginsdk.Event, error)
	Head(ctx context.Context) (int64, error)
	Version(ctx context.Context) (string, error)
}

type kvStore interface {
	StorageGet(ctx context.Context, key string, out any) (bool, error)
	StorageSet(ctx context.Context, key string, v any) error
}

type commandInfo struct {
	ID      string `json:"id"`
	Remote  bool   `json:"remote"`
	Running bool   `json:"running"`
}

type hostBackend struct {
	host *pluginsdk.Host
}

type busReadParams struct {
	Pattern string `json:"pattern"`
	After   int64  `json:"after"`
	Limit   int    `json:"limit"`
}

type seqResult struct {
	Seq int64 `json:"seq"`
}

type pingResult struct {
	Version string `json:"version"`
}

func (b hostBackend) Commands(ctx context.Context) ([]commandInfo, error) {
	var out []commandInfo
	err := b.host.Call(ctx, "command.list", struct{}{}, &out)
	return out, err
}

func (b hostBackend) Run(ctx context.Context, id, workspace string, args json.RawMessage) (json.RawMessage, error) {
	var out json.RawMessage
	err := b.host.RunCommand(ctx, id, workspace, args, &out)
	return out, err
}

func (b hostBackend) Workspaces(ctx context.Context) ([]pluginsdk.Project, error) {
	return b.host.ListProjects(ctx)
}

func (b hostBackend) Events(ctx context.Context, after int64) ([]pluginsdk.Event, error) {
	var out []pluginsdk.Event
	err := b.host.Call(ctx, "bus.read", busReadParams{Pattern: "*", After: after, Limit: busPageSize}, &out)
	return out, err
}

func (b hostBackend) Head(ctx context.Context) (int64, error) {
	var out seqResult
	err := b.host.Call(ctx, "bus.head", struct{}{}, &out)
	return out.Seq, err
}

func (b hostBackend) Version(ctx context.Context) (string, error) {
	var out pingResult
	err := b.host.Call(ctx, "core.ping", struct{}{}, &out)
	return out.Version, err
}
