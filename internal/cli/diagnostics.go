package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"clubmate/internal/diagnostics"
	"clubmate/internal/paths"
)

const (
	diagnosticsTimeout = 60 * time.Second
	unreachableNote    = "core unreachable: no workspace/session/plugin/UI state"
)

func (a *app) diagnosticsCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "diagnostics",
		Short: "Pack logs and core state into a zip for a bug report",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := a.collectDiagnostics(cmd.Context(), output)
			if err != nil {
				return err
			}
			return a.render(res, func(w io.Writer) { fmt.Fprintln(w, res.Path) })
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "zip file to write (default ./clubmate-diagnostics-<timestamp>.zip)")
	return cmd
}

func (a *app) collectDiagnostics(ctx context.Context, output string) (diagnostics.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, diagnosticsTimeout)
	defer cancel()
	var res diagnostics.Result
	callErr := a.call(ctx, "diagnostics.collect", struct{}{}, &res)
	if callErr != nil {
		return collectOffline(output, callErr)
	}
	if output == "" {
		output = filepath.Base(res.Path)
	}
	dst, err := filepath.Abs(output)
	if err != nil {
		return diagnostics.Result{}, err
	}
	if err := copyFile(res.Path, dst); err != nil {
		return diagnostics.Result{}, err
	}
	res.Path = dst
	return res, nil
}

func collectOffline(output string, cause error) (diagnostics.Result, error) {
	now := time.Now()
	if output == "" {
		output = diagnostics.FileName(now)
	}
	dst, err := filepath.Abs(output)
	if err != nil {
		return diagnostics.Result{}, err
	}
	dirs, err := paths.Resolve()
	if err != nil {
		return diagnostics.Result{}, err
	}
	info := diagnostics.NewInfo(now)
	info.Core = diagnostics.Unreachable
	info.CoreError = cause.Error()
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return diagnostics.Result{}, err
	}
	logs, err := diagnostics.Logs(dirs)
	if err != nil {
		return diagnostics.Result{}, err
	}
	entries := append([]diagnostics.Entry{{Name: diagnostics.InfoName, Data: append(raw, '\n')}}, logs...)
	files, err := diagnostics.Save(dst, now, entries, unreachableNote)
	if err != nil {
		return diagnostics.Result{}, err
	}
	return diagnostics.Result{Path: dst, Files: files}, nil
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
