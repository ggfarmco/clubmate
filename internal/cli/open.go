package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const openCapability = "editor.open"

func (a *app) openCmd() *cobra.Command {
	var projectID string
	cmd := &cobra.Command{
		Use:   "open <file>[:line]",
		Short: "Open a file in the workspace's editor",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file, line := splitLine(args[0])
			path, err := filepath.Abs(file)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", file, err)
			}
			values := map[string]any{"path": path}
			if line > 0 {
				values["line"] = line
			}
			params := commandParams(openCapability, projectID)
			params["args"] = values
			return a.callAndPrint(cmd.Context(), "command.run", params)
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "workspace id (default $"+projectEnv+")")
	return cmd
}

func splitLine(arg string) (string, int) {
	i := strings.LastIndexByte(arg, ':')
	if i <= 0 {
		return arg, 0
	}
	line, err := strconv.Atoi(arg[i+1:])
	if err != nil || line <= 0 || strings.ContainsAny(arg[i+1:], "+-") {
		return arg, 0
	}
	return arg[:i], line
}
