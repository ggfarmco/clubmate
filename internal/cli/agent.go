package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const bundleExt = ".clubmate-session"

var errLineRange = errors.New("-L takes <start>[,<end>]")

func (a *app) agentCmd() *cobra.Command {
	var projectID string
	cmd := &cobra.Command{Use: "agent", Short: "Start, prompt, wait for and read agent sessions, as an agent call"}
	cmd.PersistentFlags().StringVar(&projectID, "project", "", "workspace id (default $"+projectEnv+")")
	run := func(cmd *cobra.Command, id string, args map[string]any) error {
		params := commandParams(id, projectID)
		params["args"] = args
		asAgent(params)
		return a.callAndPrint(cmd.Context(), "command.run", params)
	}
	cmd.AddCommand(a.agentSpawnCmd(run), a.agentTellCmd(run), a.agentWaitCmd(run), a.agentOutputCmd(run), a.agentSessionsCmd(run), a.agentCancelCmd(run), a.agentBlameCmd(run), a.agentTraceCmd(run), a.agentExportCmd(run), a.agentImportCmd(run))
	return cmd
}

type agentRun func(cmd *cobra.Command, id string, args map[string]any) error

func (a *app) agentSpawnCmd(run agentRun) *cobra.Command {
	var branch, title string
	cmd := &cobra.Command{
		Use:   "spawn [prompt]",
		Short: "Start a child session, in a new worktree with --branch (agents.new)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			args := map[string]any{}
			if len(positional) == 1 {
				args["prompt"] = positional[0]
			}
			if branch != "" {
				args["branch"] = branch
			}
			if title != "" {
				args["title"] = title
			}
			return run(cmd, "agents.new", args)
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "branch for a new worktree .worktrees/<branch>; default the caller's worktree")
	cmd.Flags().StringVar(&title, "title", "", "session title; default the prompt's first line")
	return cmd
}

func (a *app) agentTellCmd(run agentRun) *cobra.Command {
	return &cobra.Command{
		Use:   "tell <session> <text>",
		Short: "Send a session a prompt, steered into or queued after a running turn (agents.prompt)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, positional []string) error {
			return run(cmd, "agents.prompt", map[string]any{"session": positional[0], "text": positional[1]})
		},
	}
}

func (a *app) agentWaitCmd(run agentRun) *cobra.Command {
	var timeout int
	cmd := &cobra.Command{
		Use:   "wait <session>...",
		Short: "Wait until one of the sessions stops running (agents.wait)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			args := map[string]any{"sessions": positional}
			if timeout > 0 {
				args["timeout"] = timeout
			}
			return run(cmd, "agents.wait", args)
		},
	}
	cmd.Flags().IntVar(&timeout, "timeout", 0, "seconds to wait at most (default 120, max 300)")
	return cmd
}

func (a *app) agentOutputCmd(run agentRun) *cobra.Command {
	var fromTurn int
	cmd := &cobra.Command{
		Use:   "output <session>",
		Short: "Print a session's transcript (agents.transcript)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			args := map[string]any{"session": positional[0]}
			if fromTurn > 0 {
				args["fromTurn"] = fromTurn
			}
			return run(cmd, "agents.transcript", args)
		},
	}
	cmd.Flags().IntVar(&fromTurn, "from-turn", 0, "first turn to include (default all)")
	return cmd
}

func (a *app) agentSessionsCmd(run agentRun) *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List the workspace's agent sessions (agents.sessions)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, "agents.sessions", map[string]any{})
		},
	}
}

func (a *app) agentCancelCmd(run agentRun) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <session>",
		Short: "Stop a session's running turn (agents.cancel)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			return run(cmd, "agents.cancel", map[string]any{"session": positional[0]})
		},
	}
}

func (a *app) agentBlameCmd(run agentRun) *cobra.Command {
	var lines string
	cmd := &cobra.Command{
		Use:   "blame <file|commit>",
		Short: "Show which agent session, turn and prompt made a file's lines or a commit (agents.blame)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			args := map[string]any{"cwd": cwd}
			if _, err := os.Stat(positional[0]); err != nil {
				args["commit"] = positional[0]
				return run(cmd, "agents.blame", args)
			}
			args["path"] = filepath.Join(cwd, positional[0])
			if filepath.IsAbs(positional[0]) {
				args["path"] = positional[0]
			}
			if lines != "" {
				start, end, err := lineRange(lines)
				if err != nil {
					return err
				}
				args["startLine"], args["endLine"] = start, end
			}
			return run(cmd, "agents.blame", args)
		},
	}
	cmd.Flags().StringVarP(&lines, "lines", "L", "", "line range <start>[,<end>], 1-based")
	return cmd
}

func (a *app) agentTraceCmd(run agentRun) *cobra.Command {
	return &cobra.Command{
		Use:   "trace [commit|range]",
		Short: "Print Agent Trace records of a commit or a range a..b: who wrote each changed line (agents.trace)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			args := map[string]any{"cwd": cwd}
			if len(positional) == 1 {
				args["rev"] = positional[0]
			}
			return run(cmd, "agents.trace", args)
		},
	}
}

func (a *app) agentExportCmd(run agentRun) *cobra.Command {
	return &cobra.Command{
		Use:   "export <session> [dir]",
		Short: "Write a session bundle to continue it elsewhere; default dir ./<session>" + bundleExt + " (agents.export)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, positional []string) error {
			dir, err := filepath.Abs(positional[0] + bundleExt)
			if len(positional) == 2 {
				dir, err = filepath.Abs(positional[1])
			}
			if err != nil {
				return err
			}
			return run(cmd, "agents.export", map[string]any{"session": positional[0], "dir": dir})
		},
	}
}

func (a *app) agentImportCmd(run agentRun) *cobra.Command {
	var branch, note string
	cmd := &cobra.Command{
		Use:   "import <dir>",
		Short: "Continue an exported session here in a new session, in a new worktree with --branch (agents.import)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			dir, err := filepath.Abs(positional[0])
			if err != nil {
				return err
			}
			args := map[string]any{"dir": dir}
			if branch != "" {
				args["branch"] = branch
			}
			if note != "" {
				args["note"] = note
			}
			return run(cmd, "agents.import", args)
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "branch for a new worktree .worktrees/<branch>; default the caller's worktree")
	cmd.Flags().StringVar(&note, "note", "", "what the new session should do next")
	return cmd
}

func lineRange(s string) (int, int, error) {
	from, to, _ := strings.Cut(s, ",")
	start, err := strconv.Atoi(from)
	if err != nil || start < 1 {
		return 0, 0, errLineRange
	}
	if to == "" {
		return start, 0, nil
	}
	end, err := strconv.Atoi(to)
	if err != nil || end < start {
		return 0, 0, errLineRange
	}
	return start, end, nil
}

func asAgent(params map[string]any) {
	params["agent"] = true
	if caller := os.Getenv(agentSessionEnv); caller != "" {
		params["caller"] = caller
	}
}
