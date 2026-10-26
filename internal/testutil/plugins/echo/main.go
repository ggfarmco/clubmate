package main

import (
	"context"
	"os"
	"os/exec"

	"clubmate/pkg/pluginsdk"
)

type readyPayload struct {
	Projects int `json:"projects"`
}

type forkedPayload struct {
	PID int `json:"pid"`
}

func main() {
	if os.Getenv("ECHO_CRASH") == "1" {
		os.Exit(3)
	}
	onInit := initialize
	if os.Getenv("ECHO_FORK_SLEEP") == "1" {
		onInit = forkSleepAndExit
	}
	pluginsdk.Main(pluginsdk.Plugin{Init: onInit})
}

func initialize(ctx context.Context, h *pluginsdk.Host) error {
	projects, err := h.ListProjects(ctx)
	if err != nil {
		return err
	}
	_, err = h.Publish(ctx, "echo.ready", readyPayload{Projects: len(projects)})
	return err
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
