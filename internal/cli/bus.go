package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"clubmate/internal/bus"
	"clubmate/internal/rpc"
)

const defaultReadLimit = 100

var errCoreDisconnected = errors.New("clubmate-core disconnected")

type busReadOptions struct {
	after  int64
	limit  int
	follow bool
}

type busEventNotification struct {
	Subscription string    `json:"subscription"`
	Event        bus.Event `json:"event"`
}

type busClosedNotification struct {
	Subscription string `json:"subscription"`
	Error        string `json:"error"`
}

func (a *app) busCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "bus", Short: "Publish and read bus events"}
	cmd.AddCommand(a.busPublishCmd(), a.busReadCmd())
	return cmd
}

func (a *app) busPublishCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "publish <topic> [json]",
		Short: "Publish an event",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{"topic": args[0]}
			if len(args) == 2 {
				if !json.Valid([]byte(args[1])) {
					return fmt.Errorf("invalid payload JSON: %s", args[1])
				}
				params["payload"] = json.RawMessage(args[1])
			}
			var e bus.Event
			if err := a.call(cmd.Context(), "bus.publish", params, &e); err != nil {
				return err
			}
			return a.render(e, func(w io.Writer) { fmt.Fprintln(w, e.Seq) })
		},
	}
}

func (a *app) busReadCmd() *cobra.Command {
	var opts busReadOptions
	cmd := &cobra.Command{
		Use:   "read <pattern>",
		Short: "Read events matching a pattern (topic, prefix.* or *)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.busRead(cmd.Context(), args[0], opts)
		},
	}
	cmd.Flags().Int64Var(&opts.after, "after", 0, "only events with seq greater than this")
	cmd.Flags().IntVar(&opts.limit, "limit", defaultReadLimit, "max events per read (with --follow, the page size)")
	cmd.Flags().BoolVar(&opts.follow, "follow", false, "keep printing new events until interrupted")
	return cmd
}

func (a *app) busRead(ctx context.Context, pattern string, opts busReadOptions) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	c, err := a.dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if !a.asJSON {
		fmt.Fprintln(a.out, "SEQ TIME TOPIC SOURCE PAYLOAD")
	}
	after, err := a.printBacklog(ctx, c, pattern, opts)
	if err != nil || !opts.follow {
		return err
	}
	return a.follow(ctx, c, pattern, after)
}

func (a *app) printBacklog(ctx context.Context, c *rpc.Client, pattern string, opts busReadOptions) (int64, error) {
	after := opts.after
	for {
		var events []bus.Event
		if err := c.Call(ctx, "bus.read", map[string]any{"pattern": pattern, "after": after, "limit": opts.limit}, &events); err != nil {
			return 0, err
		}
		for _, e := range events {
			if err := a.printEvent(e); err != nil {
				return 0, err
			}
			after = e.Seq
		}
		if !opts.follow || len(events) == 0 {
			return after, nil
		}
	}
}

func (a *app) follow(ctx context.Context, c *rpc.Client, pattern string, after int64) error {
	var sub struct {
		Subscription string `json:"subscription"`
	}
	if err := c.Call(ctx, "bus.subscribe", map[string]any{"pattern": pattern, "after": after}, &sub); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.Done():
			return errCoreDisconnected
		case n := <-c.Notifications():
			seq, err := a.handleBusNotification(n)
			if err != nil {
				return err
			}
			after = max(after, seq)
			if dropped := c.Dropped(); dropped > 0 {
				return fmt.Errorf("%d events dropped; re-run with --after %d", dropped, after)
			}
		}
	}
}

func (a *app) handleBusNotification(n rpc.Notification) (int64, error) {
	switch n.Method {
	case "bus.event":
		var ev busEventNotification
		if err := json.Unmarshal(n.Params, &ev); err != nil {
			return 0, fmt.Errorf("decode bus event: %w", err)
		}
		return ev.Event.Seq, a.printEvent(ev.Event)
	case "bus.closed":
		var closed busClosedNotification
		if err := json.Unmarshal(n.Params, &closed); err != nil {
			return 0, fmt.Errorf("decode bus closed: %w", err)
		}
		return 0, fmt.Errorf("subscription closed: %s", closed.Error)
	}
	return 0, nil
}

func (a *app) printEvent(e bus.Event) error {
	if a.asJSON {
		return json.NewEncoder(a.out).Encode(e)
	}
	_, err := fmt.Fprintf(a.out, "%d %s %s %s %s\n", e.Seq, e.Time.Local().Format(time.RFC3339), e.Topic, e.Source, e.Payload)
	return err
}
