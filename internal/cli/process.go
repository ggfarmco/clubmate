package cli

import (
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"clubmate/internal/core"
	"clubmate/internal/proc"
)

func (a *app) processCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "process", Short: "Inspect and kill tracked processes"}
	cmd.AddCommand(a.processTreeCmd(), a.processKillCmd())
	return cmd
}

func (a *app) processTreeCmd() *cobra.Command {
	var filter core.TreeFilter
	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Show tracked processes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var nodes []proc.Node
			if err := a.call(cmd.Context(), "process.tree", filter, &nodes); err != nil {
				return err
			}
			return a.render(nodes, func(w io.Writer) {
				table(w, func(tw *tabwriter.Writer) {
					fmt.Fprintln(tw, "SESSION\tPID\tPPID\tCOMM\tRSS(MiB)\tCPU%\tFLAGS")
					for _, n := range nodes {
						fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%.1f\t%.1f\t%s\n",
							n.SessionID, n.Process.PID, n.Process.PPID, n.Process.Comm,
							float64(n.Process.RSSKiB)/1024, n.Process.CPU, flags(n))
					}
				})
			})
		},
	}
	cmd.Flags().StringVar(&filter.ProjectID, "project", "", "filter by project id")
	cmd.Flags().StringVar(&filter.SessionID, "session", "", "filter by session id")
	return cmd
}

func (a *app) processKillCmd() *cobra.Command {
	var req core.KillRequest
	cmd := &cobra.Command{
		Use:   "kill <pid>... | --session <id> --orphans",
		Short: "Kill tracked processes",
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				pid, err := strconv.Atoi(arg)
				if err != nil {
					return fmt.Errorf("invalid pid %q", arg)
				}
				req.PIDs = append(req.PIDs, pid)
			}
			var res struct {
				Killed []int `json:"killed"`
			}
			if err := a.call(cmd.Context(), "process.kill", req, &res); err != nil {
				return err
			}
			return a.render(res, func(w io.Writer) {
				for _, pid := range res.Killed {
					fmt.Fprintln(w, pid)
				}
			})
		},
	}
	cmd.Flags().StringVar(&req.SessionID, "session", "", "session id")
	cmd.Flags().BoolVar(&req.OrphansOnly, "orphans", false, "kill only orphaned processes of the session")
	return cmd
}

func flags(n proc.Node) string {
	switch {
	case n.Orphan && n.Duplicate:
		return "orphan,dup"
	case n.Orphan:
		return "orphan"
	case n.Duplicate:
		return "dup"
	}
	return ""
}
