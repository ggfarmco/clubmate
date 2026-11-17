package graphs

import (
	"context"
	"encoding/json"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/automation"
)

const pruneTick = time.Hour

type runRef struct {
	id        string
	workspace string
}

func (a *app) pruneLoop(ctx context.Context, after time.Duration) {
	if after <= 0 {
		return
	}
	a.prune(ctx, after)
	t := time.NewTicker(min(pruneTick, after/2))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.prune(ctx, after)
		}
	}
}

func (a *app) prune(ctx context.Context, after time.Duration) {
	cutoff := a.now().Add(-after)
	a.mu.Lock()
	var old []runRef
	for _, r := range a.runs {
		if r.endedBefore(cutoff) {
			old = append(old, runRef{id: r.ID, workspace: r.Workspace})
		}
	}
	a.mu.Unlock()
	for _, r := range old {
		args, err := json.Marshal(automation.IDArgs{ID: r.id})
		if err != nil {
			logError(err)
			continue
		}
		if _, err := a.stopRun(ctx, pluginsdk.CommandRun{ID: "agents.graph-delete", WorkspaceID: r.workspace, Args: args}, true); err != nil {
			logError(err)
		}
	}
	a.flush(ctx)
}

func (r *Run) endedBefore(cutoff time.Time) bool {
	if r.Status != statusStopped {
		return false
	}
	ended := r.StartedAt
	if r.StoppedAt != nil {
		ended = *r.StoppedAt
	}
	return ended.Before(cutoff)
}
