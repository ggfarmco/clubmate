package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

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
	if os.Getenv("ECHO_WEDGE") == "1" {
		p.Init = wedgeAfterInit
	}
	pluginsdk.Main(p)
}

func initialize(ctx context.Context, h *pluginsdk.Host) error {
	if os.Getenv("ECHO_CRASH_ID") == h.PluginID() {
		os.Exit(3)
	}
	ready, err := h.Publish(ctx, "echo.ready", nil)
	if err != nil {
		return err
	}
	projects, err := h.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, p := range projects {
		if !ownsSection(h) {
			break
		}
		if _, err := h.Render(ctx, p.ID, mainSlot(h), sdui.Text("hello")); err != nil {
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
	if !ownsSection(h) {
		return
	}
	if _, err := h.Render(ctx, created.Project.ID, mainSlot(h), sdui.Text("hello")); err != nil {
		fmt.Fprintln(os.Stderr, "echo: render:", err)
	}
}

func uiEvent(_ context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	if ev.Action != "say" {
		return nil, fmt.Errorf("unknown action %q", ev.Action)
	}
	return map[string]any{"said": ev.Value, "plugin": h.PluginID()}, nil
}

func command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	if cmd.ID != h.PluginID()+".say" {
		if _, err := h.Publish(ctx, "echo.command", map[string]any{"plugin": h.PluginID(), "id": cmd.ID, "args": cmd.Args}); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unknown command %q", cmd.ID)
	}
	return map[string]any{"plugin": h.PluginID(), "args": cmd.Args}, nil
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

func wedgeAfterInit(ctx context.Context, h *pluginsdk.Host) error {
	if err := initialize(ctx, h); err != nil {
		return err
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGSTOP)
	}()
	return nil
}

func mainSlot(h *pluginsdk.Host) string {
	return h.PluginID() + ".main"
}

func ownsSection(h *pluginsdk.Host) bool {
	raw, err := os.ReadFile("plugin.json")
	if err != nil {
		return false
	}
	var m struct {
		Contributes struct {
			Sections []struct {
				ID    string `json:"id"`
				Scope string `json:"scope"`
			} `json:"sections"`
		} `json:"contributes"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	for _, s := range m.Contributes.Sections {
		if s.ID == h.PluginID() && s.Scope != "app" {
			return true
		}
	}
	return false
}
