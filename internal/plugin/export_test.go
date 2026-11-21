package plugin

import (
	"syscall"
	"testing"
	"time"
)

var (
	CallConn  = callConn
	KillGroup = killGroup
)

func SetKillpg(t *testing.T, f func(int, syscall.Signal) error) {
	old := killpg
	killpg = f
	t.Cleanup(func() { killpg = old })
}

func SetRouteTimeout(t *testing.T, d time.Duration) {
	old := routeTimeout
	routeTimeout = d
	t.Cleanup(func() { routeTimeout = old })
}
