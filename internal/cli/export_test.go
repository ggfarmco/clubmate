package cli

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

var SplitLine = splitLine

func NewRootWithLauncher(stdout, stderr io.Writer, launch func(ctx context.Context, projectID string) error) *cobra.Command {
	return newRoot(stdout, stderr, launch)
}
