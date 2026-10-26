package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const mainSlot = "echo.main"

type forkedPayload struct {
	PID int `json:"pid"`
}

type projectCreated struct {
	Project pluginsdk.Project `json:"project"`
}

func main() {
	if os.Getenv("ECHO_CRASH") == "1" {
		os.Exit(3)
	}
	p := pluginsdk.Plugin{Init: initialize, BusEvent: busEvent, UIEvent: uiEvent, Command: command}
	if os.Getenv("ECHO_FORK_SLEEP") == "1" {
		p.Init = forkSleepAndExit
	}
	pluginsdk.Main(p)
}

func initialize(ctx context.Context, h *pluginsdk.Host) error {
	ready, err := h.Publish(ctx, "echo.ready", nil)
	if err != nil {
		return err
	}
	projects, err := h.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, p := range projects {
		if _, err := h.Render(ctx, p.ID, mainSlot, sdui.Text("hello")); err != nil {
			return err
		}
	}
	_, err = h.Subscribe(ctx, "project.*", ready.Seq)
	return err
}

func busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	if ev.Topic != "project.created" {
		return
	}
	var created projectCreated
	if err := json.Unmarshal(ev.Payload, &created); err != nil {
		fmt.Fprintln(os.Stderr, "echo: bad project.created:", err)
		return
	}
	if _, err := h.Render(ctx, created.Project.ID, mainSlot, sdui.Text("hello")); err != nil {
		fmt.Fprintln(os.Stderr, "echo: render:", err)
	}
}

func uiEvent(_ context.Context, _ *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	if ev.Action != "say" {
		return nil, fmt.Errorf("unknown action %q", ev.Action)
	}
	return map[string]json.RawMessage{"said": ev.Value}, nil
}

func command(_ context.Context, _ *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	if cmd.ID != "echo.say" {
		return nil, fmt.Errorf("unknown command %q", cmd.ID)
	}
	return cmd.Args, nil
}

func forkSleepAndExit(ctx context.Context, h *pluginsdk.Host) error {
	cmd := exec.Command("sleep", "1000")
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := h.Publish(ctx, "echo.forked", forkedPayload{PID: cmd.Process.Pid}); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
