package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"clubmate/internal/paths"
	"clubmate/internal/rpc"
)

type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return "exit status " + strconv.Itoa(e.Code)
}

type app struct {
	out    io.Writer
	asJSON bool
}

func NewRoot(stdout, stderr io.Writer) *cobra.Command {
	a := &app{out: stdout}
	root := &cobra.Command{
		Use:           "clubmate",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().BoolVar(&a.asJSON, "json", false, "print raw result as JSON")
	root.AddCommand(a.projectCmd(), a.sessionCmd(), a.processCmd(), a.attachCmd())
	return root
}

func Execute() int {
	root := NewRoot(os.Stdout, os.Stderr)
	err := root.Execute()
	if err == nil {
		return 0
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}

func (a *app) call(ctx context.Context, method string, params, result any) error {
	dirs, err := paths.Resolve()
	if err != nil {
		return err
	}
	socket := dirs.CoreSocket()
	c, err := rpc.Dial(ctx, socket)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) {
			return fmt.Errorf("clubmate-core is not running (socket %s)", socket)
		}
		return err
	}
	defer c.Close()
	return c.Call(ctx, method, params, result)
}

func (a *app) render(v any, human func(w io.Writer)) error {
	if a.asJSON {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	human(a.out)
	return nil
}

func table(out io.Writer, rows func(w *tabwriter.Writer)) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	rows(w)
	w.Flush()
}
