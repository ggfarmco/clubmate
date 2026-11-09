package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
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
	workspaceID string
	slot        string
	node        string
	action      string
	value       string
}

func (a *app) uiCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "ui", Short: "Inspect and drive plugin UI"}
	cmd.AddCommand(a.uiSectionsCmd(), a.uiDumpCmd(), a.uiEventCmd(), a.uiSnapshotCmd(), a.uiPresentCmd(), a.uiKeyCmd(), a.uiPointerCmd("click"), a.uiPointerCmd("hover"), a.uiPointerCmd("drag"), a.uiPointerCmd("menu"), a.uiPointerCmd("scroll"), a.uiPointerCmd("drop"))
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
	var workspaceID string
	cmd := &cobra.Command{
		Use:   "dump",
		Short: "Print the UI trees rendered for a workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var views []internalsdui.SlotView
			if err := a.call(cmd.Context(), "ui.get", map[string]string{"workspaceId": workspaceID}, &views); err != nil {
				return err
			}
			trees := make([]sdui.Node, 0, len(views))
			for _, v := range views {
				n, err := v.Node()
				if err != nil {
					return fmt.Errorf("decode %s tree: %w", v.Slot, err)
				}
				trees = append(trees, n)
			}
			return a.render(views, func(w io.Writer) {
				for i, v := range views {
					fmt.Fprintf(w, "%s rev=%d owner=%s\n", v.Slot, v.Rev, v.Owner)
					writeOutline(w, trees[i], 1)
				}
			})
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "workspace id")
	_ = cmd.MarkFlagRequired("workspace")
	return cmd
}

func (a *app) uiEventCmd() *cobra.Command {
	var opts uiEventOptions
	cmd := &cobra.Command{
		Use:   "event",
		Short: "Send a UI event to the plugin owning a slot",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			params := withCaller(map[string]any{"workspaceId": opts.workspaceID, "slot": opts.slot, "nodeId": opts.node, "action": opts.action})
			if err := setJSONParam(params, "value", opts.value); err != nil {
				return err
			}
			return a.callAndPrint(cmd.Context(), "ui.event", params)
		},
	}
	cmd.Flags().StringVar(&opts.workspaceID, "workspace", "", "workspace id")
	cmd.Flags().StringVar(&opts.slot, "slot", "", "slot, e.g. terminals.main")
	cmd.Flags().StringVar(&opts.node, "node", "", "node id")
	cmd.Flags().StringVar(&opts.action, "action", "", "action name")
	cmd.Flags().StringVar(&opts.value, "value", "", "event value as JSON")
	for _, name := range []string{"workspace", "slot", "node", "action"} {
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
		ID        int    `json:"id"`
		Kind      string `json:"kind"`
		Title     string `json:"title"`
		PNG       string `json:"png,omitempty"`
		Layout    string `json:"layout"`
		Key       bool   `json:"key,omitempty"`
		Focus     string `json:"focus,omitempty"`
		Responder string `json:"responder,omitempty"`
	} `json:"windows"`
	Summary string `json:"summary"`
}

func (a *app) uiSnapshotCmd() *cobra.Command {
	var (
		workspaceID string
		out         string
		noPNG       bool
		size        string
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
			if workspaceID != "" {
				params["workspaceId"] = workspaceID
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
					if win.Key {
						fmt.Fprintf(w, "  key    focus %s (%s)\n", cmp.Or(win.Focus, "-"), cmp.Or(win.Responder, "none"))
					}
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
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "workspace id")
	cmd.Flags().BoolVar(&noPNG, "no-png", false, "write only the layout dump")
	cmd.Flags().StringVar(&size, "size", "", "resize the main window first, e.g. 640x800")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func (a *app) uiPresentCmd() *cobra.Command {
	var (
		workspaceID string
		hide        bool
	)
	cmd := &cobra.Command{
		Use:   "present <target>",
		Short: "Open or close a popover or sheet in a test-mode app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{"target": args[0], "shown": !hide}
			if workspaceID != "" {
				params["workspaceId"] = workspaceID
			}
			return a.callAndPrint(cmd.Context(), "ui.present", params)
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "workspace id")
	cmd.Flags().BoolVar(&hide, "hide", false, "close it instead")
	return cmd
}

func (a *app) uiKeyCmd() *cobra.Command {
	var workspaceID, focus string
	cmd := &cobra.Command{
		Use:   "key [combo]...",
		Short: "Send key combos such as opt+return or cmd+n to a test-mode app",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && focus == "" {
				return errors.New("give at least one combo or --focus")
			}
			params := withCaller(map[string]any{"keys": args})
			if focus != "" {
				params["focus"] = focus
			}
			if workspaceID != "" {
				params["workspaceId"] = workspaceID
			}
			return a.callAndPrint(cmd.Context(), "ui.key", params)
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "workspace id")
	cmd.Flags().StringVar(&focus, "focus", "", "focus this node (an id from the snapshot layout) first")
	return cmd
}

var pointerUsage = map[string][2]string{
	"click":  {"click <node>", "Click a node in a test-mode app (--at x,y from its top left, --count 2 for a double click)"},
	"hover":  {"hover <node>", "Move the pointer over a node in a test-mode app"},
	"drag":   {"drag <from node> <to node>", "Press on one node, move to another and release, in one window of a test-mode app"},
	"menu":   {"menu <node> [item]", "Open the menu a click (or --context: a right click) on a node shows and pick an item (path a > b); without item list it"},
	"scroll": {"scroll <node>", "Turn the scroll wheel over a node in a test-mode app (--dy lines, negative scrolls up; --keys cmd for ⌘+wheel)"},
	"drop":   {"drop <node> <file>", "Drop a file on a node in a test-mode app as if dragged from Finder"},
}

func (a *app) uiPointerCmd(action string) *cobra.Command {
	var (
		workspaceID, at, keys string
		count                 int
		dy                    float64
		context               bool
	)
	args := map[string]cobra.PositionalArgs{"drag": cobra.ExactArgs(2), "menu": cobra.RangeArgs(1, 2), "drop": cobra.ExactArgs(2)}[action]
	if args == nil {
		args = cobra.ExactArgs(1)
	}
	cmd := &cobra.Command{
		Use:   pointerUsage[action][0],
		Short: pointerUsage[action][1],
		Args:  args,
		RunE: func(cmd *cobra.Command, args []string) error {
			params := withCaller(map[string]any{"action": action, "node": args[0]})
			switch {
			case action == "drag":
				params["to"] = args[1]
			case action == "menu" && len(args) == 2:
				params["item"] = args[1]
			case action == "drop":
				file, err := filepath.Abs(args[1])
				if err != nil {
					return err
				}
				params["file"] = file
			}
			if at != "" {
				x, y, ok := strings.Cut(at, ",")
				px, errX := strconv.ParseFloat(x, 64)
				py, errY := strconv.ParseFloat(y, 64)
				if !ok || errX != nil || errY != nil {
					return fmt.Errorf("invalid --at %q, want x,y like 12,4", at)
				}
				params["at"] = []float64{px, py}
			}
			if count > 1 {
				params["count"] = count
			}
			if dy != 0 {
				params["dy"] = dy
			}
			if keys != "" {
				params["keys"] = keys
			}
			if context {
				params["context"] = true
			}
			if workspaceID != "" {
				params["workspaceId"] = workspaceID
			}
			return a.callAndPrint(cmd.Context(), "ui.pointer", params)
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "workspace id")
	cmd.Flags().StringVar(&at, "at", "", "point in the node, x,y in points from its top left (default: its centre)")
	cmd.Flags().StringVar(&keys, "keys", "", "modifiers held down, e.g. cmd or shift+opt")
	switch action {
	case "click":
		cmd.Flags().IntVar(&count, "count", 1, "clicks, 2 for a double click")
	case "menu":
		cmd.Flags().BoolVar(&context, "context", false, "open the node's context menu (right click)")
	case "scroll":
		cmd.Flags().Float64Var(&dy, "dy", 0, "wheel lines, positive scrolls down, negative up")
	}
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
