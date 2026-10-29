package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"clubmate/internal/rpc"
	"clubmate/internal/schema"
)

const (
	projectEnv = "CLUBMATE_PROJECT_ID"
	sessionEnv = "CLUBMATE_SESSION_ID"
)

func (a *app) commandCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "command", Short: "List, describe and run commands"}
	cmd.AddCommand(a.commandListCmd(), a.commandDescribeCmd(), a.commandRunCmd())
	return cmd
}

func (a *app) commandListCmd() *cobra.Command {
	var agent bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List core primitives and plugin commands",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var list []rpc.Command
			if err := a.call(cmd.Context(), "command.list", struct{}{}, &list); err != nil {
				return err
			}
			if agent {
				list = slices.DeleteFunc(list, func(c rpc.Command) bool { return !c.Agent })
			}
			return a.render(list, func(w io.Writer) {
				table(w, func(tw *tabwriter.Writer) {
					fmt.Fprintln(tw, "ID\tTITLE\tPLUGIN\tFLAGS")
					for _, c := range list {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.ID, c.Title, c.Plugin, commandFlags(c))
					}
				})
			})
		},
	}
	cmd.Flags().BoolVar(&agent, "agent", false, "only commands agents can run")
	return cmd
}

func (a *app) commandDescribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "describe <id|capability>",
		Short: "Show a command's description and argument schema",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.resolveCommand(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.render(c, func(w io.Writer) { describe(w, c) })
		},
	}
}

func (a *app) commandRunCmd() *cobra.Command {
	var (
		projectID, args string
		agent           bool
	)
	cmd := &cobra.Command{
		Use:   "run <id|capability> [key=value ...]",
		Short: "Run a command",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			params := commandParams(positional[0], projectID)
			if err := setJSONParam(params, "args", args); err != nil {
				return err
			}
			if pairs := positional[1:]; len(pairs) > 0 {
				if args != "" {
					return fmt.Errorf("use either --args or key=value arguments, not both")
				}
				c, err := a.resolveCommand(cmd.Context(), positional[0])
				if err != nil {
					return err
				}
				values, err := coerce(c, pairs)
				if err != nil {
					return err
				}
				params["args"] = values
			}
			if agent {
				params["agent"] = true
			}
			return a.callAndPrint(cmd.Context(), "command.run", params)
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "workspace id (default $"+projectEnv+")")
	cmd.Flags().StringVar(&args, "args", "", "command arguments as JSON")
	cmd.Flags().BoolVar(&agent, "agent", false, "run as an agent call: agent commands only, destructive ones approved in the app")
	return cmd
}

func commandParams(id, projectID string) map[string]any {
	if projectID == "" {
		projectID = os.Getenv(projectEnv)
	}
	params := map[string]any{"id": id, "projectId": projectID}
	if from := os.Getenv(sessionEnv); from != "" {
		params["fromSession"] = from
	}
	return params
}

func (a *app) resolveCommand(ctx context.Context, id string) (rpc.Command, error) {
	var list []rpc.Command
	if err := a.call(ctx, "command.list", struct{}{}, &list); err != nil {
		return rpc.Command{}, err
	}
	var caps struct {
		Capabilities map[string]string `json:"capabilities"`
	}
	if err := a.call(ctx, "command.capabilities", struct{}{}, &caps); err != nil {
		return rpc.Command{}, err
	}
	find := func(id string) (rpc.Command, bool) {
		i := slices.IndexFunc(list, func(c rpc.Command) bool { return c.ID == id })
		if i < 0 {
			return rpc.Command{}, false
		}
		return list[i], true
	}
	if c, ok := find(id); ok {
		return c, nil
	}
	if c, ok := find(caps.Capabilities[id]); ok {
		return c, nil
	}
	return rpc.Command{}, fmt.Errorf("unknown command: %q", id)
}

func coerce(c rpc.Command, pairs []string) (map[string]any, error) {
	out := map[string]any{}
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("argument %q is not key=value", pair)
		}
		var prop schema.Prop
		if c.Args != nil {
			prop, ok = c.Args.Properties[key]
		}
		if !ok {
			return nil, fmt.Errorf("unknown arg %q for %s", key, c.ID)
		}
		v, err := coerceValue(prop, value)
		if err != nil {
			return nil, fmt.Errorf("arg %q: %w", key, err)
		}
		out[key] = v
	}
	return out, nil
}

func coerceValue(p schema.Prop, value string) (any, error) {
	switch p.Type {
	case "integer":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("want integer, got %q", value)
		}
		return n, nil
	case "number":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("want number, got %q", value)
		}
		return f, nil
	case "boolean":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("want boolean, got %q", value)
		}
		return b, nil
	case "array":
		items := []any{}
		if value == "" {
			return items, nil
		}
		for part := range strings.SplitSeq(value, ",") {
			item, err := coerceValue(*p.Items, part)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	}
	return value, nil
}

func commandFlags(c rpc.Command) string {
	var out []string
	for name, on := range map[string]bool{"agent": c.Agent, "destructive": c.Destructive, "hidden": c.Hidden} {
		if on {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return strings.Join(out, ",")
}

func describe(w io.Writer, c rpc.Command) {
	fmt.Fprintf(w, "%s  %s  (plugin %s)\n", c.ID, c.Title, c.Plugin)
	if c.Description != "" {
		fmt.Fprintf(w, "  %s\n", c.Description)
	}
	if f := commandFlags(c); f != "" {
		fmt.Fprintf(w, "  flags: %s\n", f)
	}
	if len(c.Provides) > 0 {
		fmt.Fprintf(w, "  provides: %s\n", strings.Join(c.Provides, ", "))
	}
	describeSchema(w, "args", c.Args)
	describeSchema(w, "result", c.Result)
}

func describeSchema(w io.Writer, label string, o *schema.Object) {
	if o == nil || len(o.Properties) == 0 {
		return
	}
	fmt.Fprintf(w, "  %s:\n", label)
	for _, name := range slices.Sorted(maps.Keys(o.Properties)) {
		p := o.Properties[name]
		typ := p.Type
		if p.Items != nil {
			typ += " of " + p.Items.Type
		}
		var notes []string
		if slices.Contains(o.Required, name) {
			notes = append(notes, "required")
		}
		if len(p.Enum) > 0 {
			enum, _ := json.Marshal(p.Enum)
			notes = append(notes, "one of "+string(enum))
		}
		if p.Default != nil {
			def, _ := json.Marshal(p.Default)
			notes = append(notes, "default "+string(def))
		}
		line := fmt.Sprintf("    %s (%s)", name, typ)
		if len(notes) > 0 {
			line += " [" + strings.Join(notes, ", ") + "]"
		}
		if p.Description != "" {
			line += ": " + p.Description
		}
		fmt.Fprintln(w, line)
	}
}
