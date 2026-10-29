package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"clubmate/internal/diagnostics"
)

func (a *app) diagnosticsCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "diagnostics",
		Short: "Pack logs and core state into a zip for a bug report",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var res diagnostics.Result
			if err := a.call(cmd.Context(), "diagnostics.collect", struct{}{}, &res); err != nil {
				return err
			}
			if output == "" {
				output = filepath.Base(res.Path)
			}
			dst, err := filepath.Abs(output)
			if err != nil {
				return err
			}
			if err := copyFile(res.Path, dst); err != nil {
				return err
			}
			res.Path = dst
			return a.render(res, func(w io.Writer) { fmt.Fprintln(w, dst) })
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "zip file to write (default ./clubmate-diagnostics-<timestamp>.zip)")
	return cmd
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return out.Close()
}
