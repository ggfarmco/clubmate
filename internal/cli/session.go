package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"clubmate/internal/session"
)

const fallbackShell = "/bin/zsh"

func (a *app) sessionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "session", Short: "Manage sessions"}
	cmd.AddCommand(a.sessionCreateCmd(), a.sessionListCmd(), a.sessionKillCmd(), a.sessionRemoveCmd(), a.sessionSendCmd(), a.sessionReadCmd())
	return cmd
}

func (a *app) sessionCreateCmd() *cobra.Command {
	var req session.CreateRequest
	cmd := &cobra.Command{
		Use:   "create --project <id> [--title T] [--cwd DIR] [-- cmd args...]",
		Short: "Create a session",
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Command = args
			if len(args) == 0 {
				req.Command = defaultCommand()
			}
			if term := os.Getenv("TERM"); term != "" {
				req.Env = []string{"TERM=" + term}
			}
			var s session.Session
			if err := a.call(cmd.Context(), "session.create", req, &s); err != nil {
				return err
			}
			return a.render(s, func(w io.Writer) { fmt.Fprintln(w, s.ID) })
		},
	}
	cmd.Flags().StringVar(&req.ProjectID, "project", "", "project id")
	cmd.Flags().StringVar(&req.Title, "title", "", "session title")
	cmd.Flags().StringVar(&req.Cwd, "cwd", "", "working directory")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func (a *app) sessionListCmd() *cobra.Command {
	var projectID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var list []session.Session
			if err := a.call(cmd.Context(), "session.list", map[string]string{"projectId": projectID}, &list); err != nil {
				return err
			}
			return a.render(list, func(w io.Writer) {
				table(w, func(tw *tabwriter.Writer) {
					fmt.Fprintln(tw, "ID\tPROJECT\tOWNER\tSTATE\tTITLE\tCOMMAND")
					for _, s := range list {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.ProjectID, s.Owner, s.State, s.Title, strings.Join(s.Command, " "))
					}
				})
			})
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "filter by project id")
	return cmd
}

func (a *app) sessionKillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kill <id>",
		Short: "Kill a session's process tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var res struct{}
			if err := a.call(cmd.Context(), "session.kill", map[string]string{"id": args[0]}, &res); err != nil {
				return err
			}
			return a.render(res, func(w io.Writer) { fmt.Fprintln(w, "killed", args[0]) })
		},
	}
}

func (a *app) sessionRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove an exited session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var res struct{}
			if err := a.call(cmd.Context(), "session.remove", map[string]string{"id": args[0]}, &res); err != nil {
				return err
			}
			return a.render(res, func(w io.Writer) { fmt.Fprintln(w, "removed", args[0]) })
		},
	}
}

func (a *app) sessionSendCmd() *cobra.Command {
	var enter bool
	cmd := &cobra.Command{
		Use:   "send <id> <text>",
		Short: "Type text into a session",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := commandParams("session.send", "")
			params["args"] = map[string]any{"id": args[0], "text": args[1], "enter": enter}
			var res json.RawMessage
			if err := a.call(cmd.Context(), "command.run", params, &res); err != nil {
				return err
			}
			return a.render(res, func(io.Writer) {})
		},
	}
	cmd.Flags().BoolVar(&enter, "enter", false, "press Enter after the text")
	return cmd
}

func (a *app) sessionReadCmd() *cobra.Command {
	var (
		bytes int
		raw   bool
	)
	cmd := &cobra.Command{
		Use:   "read <id>",
		Short: "Print the tail of a session's output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := commandParams("session.read", "")
			params["args"] = map[string]any{"id": args[0], "bytes": bytes, "raw": raw}
			var snap session.Snapshot
			if err := a.call(cmd.Context(), "command.run", params, &snap); err != nil {
				return err
			}
			return a.render(snap, func(w io.Writer) {
				if snap.AltScreen {
					fmt.Fprintln(cmd.ErrOrStderr(), "a full-screen app is running; no text to read")
					return
				}
				fmt.Fprint(w, snap.Text)
			})
		},
	}
	cmd.Flags().IntVar(&bytes, "bytes", session.ReadDefault, fmt.Sprintf("bytes to read from the end (max %d)", session.ReadMax))
	cmd.Flags().BoolVar(&raw, "raw", false, "keep ANSI escapes")
	return cmd
}

func defaultCommand() []string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = fallbackShell
	}
	return []string{shell, "-l"}
}
