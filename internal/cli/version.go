package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"clubmate/internal/buildinfo"
	"clubmate/internal/sessionhost"
)

type versionInfo struct {
	Version         string `json:"version"`
	Commit          string `json:"commit"`
	SessionProtocol int    `json:"sessionProtocol"`
}

func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the clubmate version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := versionInfo{Version: buildinfo.Version, Commit: buildinfo.Commit, SessionProtocol: sessionhost.Protocol}
			return a.render(info, func(w io.Writer) { fmt.Fprintln(w, buildinfo.String()) })
		},
	}
}
