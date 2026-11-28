package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

const (
	slotAcquireID = "agents.slot-acquire"
	slotsID       = "agents.slots"
	slotCallWait  = 40 * time.Second
	slotHookInMax = 1 << 20
	slotWait      = "wait"
	slotDenied    = "denied"
)

var slotRetry = 30 * time.Second

type slotHookInput struct {
	ToolName  string `json:"tool_name"`
	ToolUseID string `json:"tool_use_id"`
	Cwd       string `json:"cwd"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

type slotAnswer struct {
	State   string `json:"state"`
	Request string `json:"request"`
	Text    string `json:"text"`
}

type slotDeny struct {
	HookSpecificOutput slotDenyOutput `json:"hookSpecificOutput"`
}

type slotDenyOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

type slotCall func(ctx context.Context, args map[string]any, out *slotAnswer) error

func (a *app) slotCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "slot", Short: "Resource slots that limit heavy agent commands (tests, simulator boots)"}
	cmd.AddCommand(&cobra.Command{
		Use:    "hook",
		Short:  "PreToolUse hook: wait for the Bash call's resource slot (reads the hook input on stdin)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return slotHook(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), a.slotAcquire)
		},
	}, &cobra.Command{
		Use:   "status",
		Short: "Print who holds and who waits for each resource slot (agents.slots)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.slotStatus(cmd.Context())
		},
	})
	return cmd
}

func (a *app) slotAcquire(ctx context.Context, args map[string]any, out *slotAnswer) error {
	ctx, cancel := context.WithTimeout(ctx, slotCallWait)
	defer cancel()
	params := commandParams(slotAcquireID, "")
	params["args"] = args
	return a.call(ctx, "command.run", params, out)
}

func slotHook(ctx context.Context, stdin io.Reader, stdout io.Writer, call slotCall) error {
	raw, err := io.ReadAll(io.LimitReader(stdin, slotHookInMax))
	if err != nil {
		return nil
	}
	var in slotHookInput
	if json.Unmarshal(raw, &in) != nil || in.ToolName != "Bash" || strings.TrimSpace(in.ToolInput.Command) == "" || os.Getenv(agentSessionEnv) == "" {
		return nil
	}
	request := ""
	failing := time.Time{}
	for {
		var ans slotAnswer
		err := call(ctx, map[string]any{"call": in.ToolUseID, "command": in.ToolInput.Command, "cwd": in.Cwd, "request": request}, &ans)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			if failing.IsZero() {
				failing = time.Now()
			}
			if time.Since(failing) >= slotRetry {
				return nil
			}
			time.Sleep(min(time.Second, slotRetry/10))
			continue
		}
		failing = time.Time{}
		switch ans.State {
		case slotWait:
			request = ans.Request
		case slotDenied:
			return json.NewEncoder(stdout).Encode(slotDeny{HookSpecificOutput: slotDenyOutput{HookEventName: "PreToolUse", PermissionDecision: "deny", PermissionDecisionReason: ans.Text}})
		default:
			return nil
		}
	}
}

type slotsStatus struct {
	Enabled bool    `json:"enabled"`
	CPU     float64 `json:"cpu"`
	MemFree float64 `json:"memFreeGB"`
	Kinds   []struct {
		ID      string `json:"id"`
		Line    string `json:"line"`
		Holders []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Command string `json:"command"`
		} `json:"holders"`
		Waiters []struct {
			Place   int    `json:"place"`
			Title   string `json:"title"`
			Command string `json:"command"`
			Why     string `json:"why"`
		} `json:"waiters"`
	} `json:"kinds"`
}

func (a *app) slotStatus(ctx context.Context) error {
	var res slotsStatus
	if err := a.call(ctx, "command.run", commandParams(slotsID, ""), &res); err != nil {
		return err
	}
	return a.render(res, func(w io.Writer) {
		if !res.Enabled {
			fmt.Fprintln(w, "Resource limits are off.")
			return
		}
		fmt.Fprintf(w, "CPU %.0f %% · %.1f GB free\n", res.CPU, res.MemFree)
		for _, k := range res.Kinds {
			fmt.Fprintln(w, k.Line)
			table(w, func(t *tabwriter.Writer) {
				for _, h := range k.Holders {
					fmt.Fprintf(t, "  holds\t%s\t%s\t%s\n", h.Title, h.Command, h.ID)
				}
				for _, q := range k.Waiters {
					fmt.Fprintf(t, "  waits %d\t%s\t%s\t%s\n", q.Place, q.Title, q.Command, q.Why)
				}
			})
		}
	})
}
