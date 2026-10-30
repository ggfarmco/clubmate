package cli

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/spf13/cobra"

	"clubmate/internal/buildinfo"
	"clubmate/internal/mcp"
)

const mcpPoll = 5 * time.Second

func (a *app) mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the command registry to agents over MCP (stdio)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := &mcp.Server{
				Dial:    a.mcpDial,
				Version: buildinfo.String(),
				Project: os.Getenv(projectEnv),
				Log:     cmd.ErrOrStderr(),
				Poll:    mcpPoll,
				Caller:  os.Getenv(agentSessionEnv),
			}
			return srv.Serve(cmd.Context(), cmd.InOrStdin(), a.out)
		},
	}
}

func (a *app) mcpDial(ctx context.Context) (mcp.Core, error) {
	c, err := a.dial(ctx)
	if errors.Is(err, errCoreNotRunning) || errors.Is(err, fs.ErrNotExist) {
		return nil, mcp.ErrNoCore
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}
