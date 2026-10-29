package session

import "time"

func WithSnapshotTimeout(d time.Duration) Option {
	return func(m *Manager) { m.snapshotTimeout = d }
}
