package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"clubmate/internal/sessionhost"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "clubmate-session:", err)
		os.Exit(1)
	}
}

func run() error {
	var cfg sessionhost.Config
	if err := json.NewDecoder(os.Stdin).Decode(&cfg); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	return sessionhost.Run(ctx, cfg)
}
