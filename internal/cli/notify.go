package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"clubmate/internal/notify"
	"clubmate/internal/rpc"
)

var (
	errNoWorkspace    = errors.New("no workspace")
	errEmptyMessage   = errors.New("message is empty")
	quietNotifyErrors = []error{errCoreNotRunning, errNoWorkspace, errEmptyMessage, rpc.ErrRateLimited, context.DeadlineExceeded}
)

const (
	sessionEnv      = "CLUBMATE_SESSION_ID"
	agentSessionEnv = "CLUBMATE_AGENT_SESSION"
	stdinArg        = "-"
	notifyTimeout   = 5 * time.Second
	maxStdin        = 64 << 10
)

type notifyRequest struct {
	ProjectID string `json:"projectId"`
	Session   string `json:"session,omitempty"`
	Title     string `json:"title,omitempty"`
	Message   string `json:"message"`
	Kind      string `json:"kind"`
}

func (a *app) notifyCmd() *cobra.Command {
	var (
		req   notifyRequest
		quiet bool
	)
	cmd := &cobra.Command{
		Use:   "notify [--title T] [--kind K] [--workspace W] [--session S] [--quiet] <message | ->",
		Short: "Show a macOS notification from Clubmate (- reads the message from stdin)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := a.notify(cmd, req, args)
			if err != nil {
				if quiet && slices.ContainsFunc(quietNotifyErrors, func(target error) bool { return errors.Is(err, target) }) {
					return nil
				}
				return err
			}
			if quiet {
				return nil
			}
			return a.render(res, func(w io.Writer) { fmt.Fprintln(w, res.ID) })
		},
	}
	cmd.Flags().StringVar(&req.Title, "title", "", "title, up to 120 characters")
	cmd.Flags().StringVar(&req.Kind, "kind", notify.DefaultKind, "one of "+strings.Join(notify.Kinds, ", "))
	cmd.Flags().StringVar(&req.ProjectID, "workspace", "", "workspace id (default $"+projectEnv+", else the only open workspace)")
	cmd.Flags().StringVar(&req.Session, "session", "", "session to reveal on click (default $"+agentSessionEnv+", else $"+sessionEnv+")")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "print nothing and exit 0 when Clubmate isn't running, no workspace resolves, the message is empty or the rate limit hits (for hooks)")
	return cmd
}

func (a *app) notify(cmd *cobra.Command, req notifyRequest, args []string) (notifyResult, error) {
	if !notify.ValidKind(req.Kind) {
		return notifyResult{}, fmt.Errorf("unknown kind %q (want one of %s)", req.Kind, strings.Join(notify.Kinds, ", "))
	}
	message, err := notifyMessage(cmd.InOrStdin(), args)
	if err != nil {
		return notifyResult{}, err
	}
	if req.Message = notify.Clip(message, notify.MaxMessage); req.Message == "" {
		return notifyResult{}, errEmptyMessage
	}
	req.Title = notify.Clip(req.Title, notify.MaxTitle)
	ctx, cancel := context.WithTimeout(cmd.Context(), notifyTimeout)
	defer cancel()
	if req.ProjectID, err = a.notifyWorkspace(ctx, req.ProjectID); err != nil {
		return notifyResult{}, err
	}
	if req.Session == "" && req.ProjectID == os.Getenv(projectEnv) {
		req.Session = cmp.Or(os.Getenv(agentSessionEnv), os.Getenv(sessionEnv))
	}
	var res notifyResult
	return res, a.call(ctx, "notify.post", req, &res)
}

type notifyResult struct {
	ID string `json:"id"`
}

func notifyMessage(stdin io.Reader, args []string) (string, error) {
	if len(args) != 1 || args[0] != stdinArg {
		return strings.Join(args, " "), nil
	}
	data, err := io.ReadAll(io.LimitReader(stdin, maxStdin))
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return string(data), nil
}

func (a *app) notifyWorkspace(ctx context.Context, flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if env := os.Getenv(projectEnv); env != "" {
		return env, nil
	}
	var open []string
	if err := a.call(ctx, "ui.presence", struct{}{}, &open); err != nil {
		return "", err
	}
	if len(open) != 1 {
		return "", fmt.Errorf("%w: pass --workspace (%d workspaces are open; see `clubmate workspace list`)", errNoWorkspace, len(open))
	}
	return open[0], nil
}
