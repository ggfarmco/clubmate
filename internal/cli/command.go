package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"clubmate/internal/rpc"
)

func (a *app) commandCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "command", Short: "List and run plugin commands"}
	cmd.AddCommand(a.commandListCmd(), a.commandRunCmd())
	return cmd
}

func (a *app) commandListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List commands contributed by plugins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var list []rpc.Command
			if err := a.call(cmd.Context(), "command.list", struct{}{}, &list); err != nil {
				return err
			}
			return a.render(list, func(w io.Writer) {
				table(w, func(tw *tabwriter.Writer) {
					fmt.Fprintln(tw, "ID\tTITLE\tPLUGIN")
					for _, c := range list {
						fmt.Fprintf(tw, "%s\t%s\t%s\n", c.ID, c.Title, c.Plugin)
					}
				})
			})
		},
	}
}

func (a *app) commandRunCmd() *cobra.Command {
	var projectID, args string
	cmd := &cobra.Command{
		Use:   "run <id>",
		Short: "Run a plugin command",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			params := map[string]any{"id": positional[0], "projectId": projectID}
			if err := setJSONParam(params, "args", args); err != nil {
				return err
			}
			return a.callAndPrint(cmd.Context(), "command.run", params)
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "project id")
	cmd.Flags().StringVar(&args, "args", "", "command arguments as JSON")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
