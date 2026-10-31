package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
)

type uiEventOptions struct {
	projectID string
	slot      string
	node      string
	action    string
	value     string
}

func (a *app) uiCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "ui", Short: "Inspect and drive plugin UI"}
	cmd.AddCommand(a.uiSectionsCmd(), a.uiDumpCmd(), a.uiEventCmd(), a.uiSnapshotCmd(), a.uiPresentCmd())
	return cmd
}

func (a *app) uiSectionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sections",
		Short: "List UI sections contributed by plugins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var list []internalsdui.Section
			if err := a.call(cmd.Context(), "ui.sections", struct{}{}, &list); err != nil {
				return err
			}
			return a.render(list, func(w io.Writer) {
				table(w, func(tw *tabwriter.Writer) {
					fmt.Fprintln(tw, "ID\tTITLE\tICON\tORDER\tPLUGIN")
					for _, s := range list {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", s.ID, s.Title, s.Icon, s.Order, s.Plugin)
					}
				})
			})
		},
	}
}

func (a *app) uiDumpCmd() *cobra.Command {
	var projectID string
	cmd := &cobra.Command{
		Use:   "dump",
		Short: "Print the UI trees rendered for a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var views []internalsdui.SlotView
			if err := a.call(cmd.Context(), "ui.get", map[string]string{"projectId": projectID}, &views); err != nil {
				return err
			}
			return a.render(views, func(w io.Writer) {
				for _, v := range views {
					fmt.Fprintf(w, "%s rev=%d owner=%s\n", v.Slot, v.Rev, v.Owner)
					writeOutline(w, v.Tree, 1)
				}
			})
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "project id")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func (a *app) uiEventCmd() *cobra.Command {
	var opts uiEventOptions
	cmd := &cobra.Command{
		Use:   "event",
		Short: "Send a UI event to the plugin owning a slot",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{"projectId": opts.projectID, "slot": opts.slot, "nodeId": opts.node, "action": opts.action}
			if err := setJSONParam(params, "value", opts.value); err != nil {
				return err
			}
			return a.callAndPrint(cmd.Context(), "ui.event", params)
		},
	}
	cmd.Flags().StringVar(&opts.projectID, "project", "", "project id")
	cmd.Flags().StringVar(&opts.slot, "slot", "", "slot, e.g. terminals.main")
	cmd.Flags().StringVar(&opts.node, "node", "", "node id")
	cmd.Flags().StringVar(&opts.action, "action", "", "action name")
	cmd.Flags().StringVar(&opts.value, "value", "", "event value as JSON")
	for _, name := range []string{"project", "slot", "node", "action"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

func (a *app) callAndPrint(ctx context.Context, method string, params any) error {
	var res json.RawMessage
	if err := a.call(ctx, method, params, &res); err != nil {
		return err
	}
	return a.render(res, func(w io.Writer) { fmt.Fprintln(w, string(res)) })
}

func setJSONParam(params map[string]any, key, value string) error {
	if value == "" {
		return nil
	}
	if !json.Valid([]byte(value)) {
		return fmt.Errorf("invalid %s JSON: %s", key, value)
	}
	params[key] = json.RawMessage(value)
	return nil
}

func writeOutline(w io.Writer, n sdui.Node, depth int) {
	fmt.Fprintln(w, strings.Repeat("  ", depth)+nodeLine(n))
	for _, child := range n.Children {
		writeOutline(w, child, depth+1)
	}
}

func nodeLine(n sdui.Node) string {
	var b strings.Builder
	b.WriteString(n.Type)
	if n.ID != "" {
		b.WriteString("#" + n.ID)
	}
	for _, key := range slices.Sorted(maps.Keys(n.Props)) {
		fmt.Fprintf(&b, " %s=%s", key, propValue(n.Props[key]))
	}
	return b.String()
}

func propValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

type snapshotResult struct {
	Windows []struct {
		ID     int    `json:"id"`
		Kind   string `json:"kind"`
		Title  string `json:"title"`
		PNG    string `json:"png,omitempty"`
		Layout string `json:"layout"`
	} `json:"windows"`
	Summary string `json:"summary"`
}

func (a *app) uiSnapshotCmd() *cobra.Command {
	var (
		projectID string
		out       string
		noPNG     bool
		size      string
	)
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Draw a test-mode app's windows to png plus a flagged layout dump",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(out)
			if err != nil {
				return err
			}
			params := map[string]any{"out": dir, "png": !noPNG}
			if projectID != "" {
				params["projectId"] = projectID
			}
			if size != "" {
				w, h, err := parseSize(size)
				if err != nil {
					return err
				}
				params["width"], params["height"] = w, h
			}
			var res snapshotResult
			if err := a.call(cmd.Context(), "ui.snapshot", params, &res); err != nil {
				return err
			}
			return a.render(res, func(w io.Writer) {
				for _, win := range res.Windows {
					fmt.Fprintf(w, "%s %q\n", win.Kind, win.Title)
					if win.PNG != "" {
						fmt.Fprintf(w, "  png    %s\n", win.PNG)
					}
					fmt.Fprintf(w, "  layout %s\n", win.Layout)
				}
				fmt.Fprintln(w, res.Summary)
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "directory for the png and layout files")
	cmd.Flags().StringVar(&projectID, "project", "", "project id")
	cmd.Flags().BoolVar(&noPNG, "no-png", false, "write only the layout dump")
	cmd.Flags().StringVar(&size, "size", "", "resize the main window first, e.g. 640x800")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func (a *app) uiPresentCmd() *cobra.Command {
	var (
		projectID string
		hide      bool
	)
	cmd := &cobra.Command{
		Use:   "present <target>",
		Short: "Open or close a popover or sheet in a test-mode app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{"target": args[0], "shown": !hide}
			if projectID != "" {
				params["projectId"] = projectID
			}
			return a.callAndPrint(cmd.Context(), "ui.present", params)
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "project id")
	cmd.Flags().BoolVar(&hide, "hide", false, "close it instead")
	return cmd
}

func parseSize(s string) (float64, float64, error) {
	w, h, ok := strings.Cut(s, "x")
	width, errW := strconv.ParseFloat(w, 64)
	height, errH := strconv.ParseFloat(h, 64)
	if !ok || errW != nil || errH != nil || width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("invalid size %q, want WxH like 640x800", s)
	}
	return width, height, nil
}
