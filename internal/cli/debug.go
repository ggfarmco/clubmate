package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
)

const debugCallSlack = 30 * time.Second

type pprofResult struct {
	Profile string `json:"profile"`
	Data    []byte `json:"data"`
}

func (a *app) debugCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "debug", Short: "Inspect the running core"}
	cmd.AddCommand(a.debugPprofCmd())
	return cmd
}

func (a *app) debugPprofCmd() *cobra.Command {
	var (
		output  string
		debug   int
		seconds int
		gc      bool
	)
	cmd := &cobra.Command{
		Use:   "pprof <heap|allocs|goroutine|block|mutex|threadcreate|cpu>",
		Short: "Write a pprof profile of the core",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(seconds)*time.Second+debugCallSlack)
			defer cancel()
			params := map[string]any{"profile": args[0], "debug": debug, "seconds": seconds, "gc": gc}
			var res pprofResult
			if err := a.call(ctx, "debug.pprof", params, &res); err != nil {
				return err
			}
			if output == "" && debug == 0 {
				output = args[0] + ".pprof"
			}
			if output == "" || output == "-" {
				_, err := a.out.Write(res.Data)
				return err
			}
			if err := os.WriteFile(output, res.Data, 0o600); err != nil {
				return err
			}
			return a.render(map[string]any{"path": output, "bytes": len(res.Data)}, func(w io.Writer) { fmt.Fprintln(w, output) })
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "file to write, - for stdout (default <profile>.pprof, stdout with --debug)")
	cmd.Flags().IntVar(&debug, "debug", 0, "text format level passed to pprof (1 or 2), 0 for the binary profile")
	cmd.Flags().IntVar(&seconds, "seconds", 0, "cpu profile duration (default 10)")
	cmd.Flags().BoolVar(&gc, "gc", false, "run a GC before a heap profile")
	return cmd
}
