package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"clubmate/internal/plugin"
)

func (a *app) pluginCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "plugin", Short: "Inspect and restart plugins"}
	cmd.AddCommand(a.pluginListCmd(), a.pluginRestartCmd())
	return cmd
}

func (a *app) pluginListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List plugins and their state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var list []plugin.Status
			if err := a.call(cmd.Context(), "plugin.list", struct{}{}, &list); err != nil {
				return err
			}
			return a.render(list, func(w io.Writer) {
				table(w, func(tw *tabwriter.Writer) {
					fmt.Fprintln(tw, "ID\tSTATE\tPID\tRESTARTS\tERROR")
					for _, st := range list {
						fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\n", st.ID, st.State, st.PID, st.Restarts, st.LastError)
					}
				})
			})
		},
	}
}

func (a *app) pluginRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart <id>",
		Short: "Stop a plugin and start it again",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var res struct{}
			if err := a.call(cmd.Context(), "plugin.restart", map[string]string{"id": args[0]}, &res); err != nil {
				return err
			}
			return a.render(res, func(w io.Writer) { fmt.Fprintln(w, "restarting", args[0]) })
		},
	}
}
