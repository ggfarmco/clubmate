package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"clubmate/internal/buildinfo"
)

func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the clubmate version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(a.out, buildinfo.String())
		},
	}
}
