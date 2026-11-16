package plugin

import (
	"testing"
	"time"
)

var CallConn = callConn

func SetRouteTimeout(t *testing.T, d time.Duration) {
	old := routeTimeout
	routeTimeout = d
	t.Cleanup(func() { routeTimeout = old })
}
