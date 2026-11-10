package bus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	defaultRetainAge  = 30 * 24 * time.Hour
	defaultRetainRows = 10000
	cursorMaxAge      = 90 * 24 * time.Hour
	pruneEvery        = time.Hour
	pruneBatch        = 5000
)

type Option func(*Bus)

func WithRetention(age time.Duration, rows int) Option {
	return func(b *Bus) {
		b.retainAge, b.retainRows = age, rows
	}
}

func (b *Bus) Oldest(ctx context.Context) (int64, error) {
	var seq int64
	if err := b.db.QueryRowContext(ctx, `SELECT COALESCE(MIN(seq), 0) FROM events`).Scan(&seq); err != nil {
		return 0, fmt.Errorf("read oldest seq: %w", err)
	}
	return seq, nil
}

func (b *Bus) Prune(ctx context.Context, now time.Time) (int64, error) {
	last := b.Head()
	if last == 0 {
		return 0, nil
	}
	byAge, err := b.firstSince(ctx, now.Add(-b.retainAge), last)
	if err != nil {
		return 0, err
	}
	cut := min(byAge, last-int64(b.retainRows)+1)
	var lowest sql.NullInt64
	if err := b.db.QueryRowContext(ctx, `SELECT MIN(seq) FROM cursors`).Scan(&lowest); err != nil {
		return 0, fmt.Errorf("read cursors: %w", err)
	}
	if lowest.Valid {
		ceiling, err := b.firstSince(ctx, now.Add(-cursorMaxAge), last)
		if err != nil {
			return 0, err
		}
		cut = min(cut, max(lowest.Int64, ceiling))
	}
	cut = min(cut, last)
	var deleted int64
	for {
		res, err := b.db.ExecContext(ctx, `DELETE FROM events WHERE seq IN (SELECT seq FROM events WHERE seq < ? ORDER BY seq LIMIT ?)`, cut, pruneBatch)
		if err != nil {
			return deleted, fmt.Errorf("prune events: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return deleted, fmt.Errorf("prune events: %w", err)
		}
		deleted += n
		if n < pruneBatch {
			return deleted, nil
		}
	}
}

func (b *Bus) firstSince(ctx context.Context, t time.Time, last int64) (int64, error) {
	var seq int64
	err := b.db.QueryRowContext(ctx, `SELECT seq FROM events WHERE time >= ? ORDER BY seq LIMIT 1`, t.UnixNano()).Scan(&seq)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return last + 1, nil
	case err != nil:
		return 0, fmt.Errorf("find events since %s: %w", t.Format(time.RFC3339), err)
	}
	return seq, nil
}

func (b *Bus) pruneLoop(ctx context.Context) {
	defer b.wg.Done()
	t := time.NewTicker(pruneEvery)
	defer t.Stop()
	for {
		if n, err := b.Prune(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.Warn("prune bus", "err", err)
		} else if n > 0 {
			slog.Info("pruned bus", "events", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
