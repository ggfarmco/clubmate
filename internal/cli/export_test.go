package cli

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func NewRootWithLauncher(stdout, stderr io.Writer, launch func(ctx context.Context, projectID string) error) *cobra.Command {
	return newRoot(stdout, stderr, launch)
}
