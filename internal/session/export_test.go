package session

import "time"

func WithSnapshotTimeout(d time.Duration) Option {
	return func(m *Manager) { m.snapshotTimeout = d }
}

func WithSendTimeouts(send, drain time.Duration) Option {
	return func(m *Manager) { m.sendTimeout, m.drainTimeout = send, drain }
}
