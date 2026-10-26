package main

import (
	"context"
	"os"

	"clubmate/pkg/pluginsdk"
)

type readyPayload struct {
	Projects int `json:"projects"`
}

func main() {
	if os.Getenv("ECHO_CRASH") == "1" {
		os.Exit(3)
	}
	pluginsdk.Main(pluginsdk.Plugin{Init: initialize})
}

func initialize(ctx context.Context, h *pluginsdk.Host) error {
	projects, err := h.ListProjects(ctx)
	if err != nil {
		return err
	}
	_, err = h.Publish(ctx, "echo.ready", readyPayload{Projects: len(projects)})
	return err
}
