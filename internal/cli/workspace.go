package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"clubmate/internal/project"
)

const (
	appBundleID      = "com.clubmate.app"
	lastOpenedLayout = "2006-01-02 15:04"
)

var errRemoveNeedsYes = errors.New("removing a workspace stops all its processes; pass --yes to confirm")

func (a *app) workspaceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "workspace", Aliases: []string{"project"}, Short: "Manage workspaces"}
	cmd.AddCommand(
		a.workspaceListCmd(),
		a.workspaceOpenCmd(),
		a.workspaceHomeCmd(),
		a.workspaceCreateCmd(),
		a.workspaceRenameCmd(),
		a.workspaceAddFolderCmd(),
		a.workspaceRemoveCmd(),
	)
	return cmd
}

func (a *app) workspaceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List workspaces, most recently opened first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var list []project.Project
			if err := a.call(cmd.Context(), "project.list", struct{}{}, &list); err != nil {
				return err
			}
			return a.render(list, func(w io.Writer) {
				table(w, func(tw *tabwriter.Writer) {
					fmt.Fprintln(tw, "ID\tNAME\tLAST OPENED\tFOLDERS")
					for _, p := range list {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.ID, p.Name, lastOpened(p), strings.Join(p.Repos, ","))
					}
				})
			})
		},
	}
}

func (a *app) workspaceOpenCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "open <dir>... | --id <id>",
		Short: "Open folders as a workspace, or an existing workspace by id, in the app",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id != "" {
				if len(args) > 0 {
					return errors.New("pass either folders or --id, not both")
				}
				return a.openByID(cmd.Context(), id)
			}
			if len(args) == 0 {
				return errors.New("pass at least one folder or --id")
			}
			folders, err := absPaths(args)
			if err != nil {
				return err
			}
			var res struct {
				ID      string `json:"id"`
				Created bool   `json:"created"`
			}
			if err := a.call(cmd.Context(), "workspace.open", map[string]any{"folders": folders}, &res); err != nil {
				return err
			}
			return a.render(res, func(w io.Writer) { fmt.Fprintln(w, res.ID) })
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "workspace id")
	return cmd
}

func (a *app) workspaceHomeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "home",
		Short: "Open the home workspace rooted at ~ in the app",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var res struct {
				ID string `json:"id"`
			}
			if err := a.call(cmd.Context(), "workspace.home", struct{}{}, &res); err != nil {
				return err
			}
			return a.render(res, func(w io.Writer) { fmt.Fprintln(w, res.ID) })
		},
	}
}

func (a *app) workspaceCreateCmd() *cobra.Command {
	var repos []string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a named workspace",
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
	cmd.Flags().StringArrayVar(&repos, "repo", nil, "folder path (repeatable)")
	return cmd
}

func (a *app) workspaceRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <id> <name>",
		Short: "Rename a workspace",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var p project.Project
			if err := a.call(cmd.Context(), "project.rename", map[string]string{"id": args[0], "name": args[1]}, &p); err != nil {
				return err
			}
			return a.render(p, func(w io.Writer) { fmt.Fprintln(w, p.Name) })
		},
	}
}

func (a *app) workspaceAddFolderCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "add-folder <id> <dir>",
		Aliases: []string{"add-repo"},
		Short:   "Add a folder to a workspace",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(args[1])
			if err != nil {
				return fmt.Errorf("resolve %s: %w", args[1], err)
			}
			var p project.Project
			if err := a.call(cmd.Context(), "project.addRepo", map[string]string{"id": args[0], "path": dir}, &p); err != nil {
				return err
			}
			return a.render(p, func(w io.Writer) { fmt.Fprintln(w, strings.Join(p.Repos, "\n")) })
		},
	}
}

func (a *app) workspaceRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove <id>",
		Aliases: []string{"delete"},
		Short:   "Remove a workspace and stop its processes",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return errRemoveNeedsYes
			}
			var res struct{}
			if err := a.call(cmd.Context(), "project.delete", map[string]string{"id": args[0]}, &res); err != nil {
				return err
			}
			return a.render(res, func(w io.Writer) { fmt.Fprintln(w, "removed", args[0]) })
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm stopping the workspace's processes")
	return cmd
}

func (a *app) openByID(ctx context.Context, id string) error {
	var res struct {
		Clients int `json:"clients"`
	}
	if err := a.call(ctx, "project.open", map[string]string{"id": id}, &res); err != nil {
		return err
	}
	if res.Clients == 0 {
		if err := a.launch(ctx, id); err != nil {
			return err
		}
	}
	return a.render(res, func(w io.Writer) { fmt.Fprintln(w, "opened", id) })
}

func absPaths(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", p, err)
		}
		out = append(out, abs)
	}
	return out, nil
}

func lastOpened(p project.Project) string {
	if p.LastOpenedAt.IsZero() {
		return "-"
	}
	return p.LastOpenedAt.Format(lastOpenedLayout)
}

func launchApp(ctx context.Context, projectID string) error {
	out, err := exec.CommandContext(ctx, "open", "-b", appBundleID, "--args", "--open-project", projectID).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launch app: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
