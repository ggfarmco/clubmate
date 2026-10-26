package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"clubmate/internal/project"
)

func (a *app) projectCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Manage projects"}
	cmd.AddCommand(a.projectCreateCmd(), a.projectListCmd(), a.projectAddRepoCmd(), a.projectDeleteCmd())
	return cmd
}

func (a *app) projectCreateCmd() *cobra.Command {
	var repos []string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var p project.Project
			params := map[string]any{"name": args[0], "repos": repos}
			if err := a.call(cmd.Context(), "project.create", params, &p); err != nil {
				return err
			}
			return a.render(p, func(w io.Writer) { fmt.Fprintln(w, p.ID) })
		},
	}
	cmd.Flags().StringArrayVar(&repos, "repo", nil, "repository path (repeatable)")
	return cmd
}

func (a *app) projectListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var list []project.Project
			if err := a.call(cmd.Context(), "project.list", struct{}{}, &list); err != nil {
				return err
			}
			return a.render(list, func(w io.Writer) {
				table(w, func(tw *tabwriter.Writer) {
					fmt.Fprintln(tw, "ID\tNAME\tREPOS")
					for _, p := range list {
						fmt.Fprintf(tw, "%s\t%s\t%s\n", p.ID, p.Name, strings.Join(p.Repos, ","))
					}
				})
			})
		},
	}
}

func (a *app) projectAddRepoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-repo <id> <path>",
		Short: "Add a repository to a project",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var p project.Project
			if err := a.call(cmd.Context(), "project.addRepo", map[string]string{"id": args[0], "path": args[1]}, &p); err != nil {
				return err
			}
			return a.render(p, func(w io.Writer) { fmt.Fprintln(w, strings.Join(p.Repos, "\n")) })
		},
	}
}

func (a *app) projectDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a project and kill its processes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var res struct{}
			if err := a.call(cmd.Context(), "project.delete", map[string]string{"id": args[0]}, &res); err != nil {
				return err
			}
			return a.render(res, func(w io.Writer) { fmt.Fprintln(w, "deleted", args[0]) })
		},
	}
}
