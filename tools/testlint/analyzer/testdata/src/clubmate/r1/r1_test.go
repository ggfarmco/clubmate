package r1

import (
	"testing"
	"time"

	"clubmate/internal/testutil"
)

func TestSleeps(t *testing.T) {
	time.Sleep(time.Millisecond) // want "fixed sleep"
	go func() { time.Sleep(time.Millisecond) }()
	t.Run("sub", func(t *testing.T) {
		time.Sleep(time.Millisecond) // want "fixed sleep"
	})
}

func TestDeadlines(t *testing.T) {
	<-time.After(time.Millisecond) // want "unscaled deadline: wrap"
	<-time.After(testutil.Timeout(time.Millisecond))
}

type fixture struct{}

func (fixture) settle() {
	time.Sleep(time.Millisecond) // want "fixed sleep"
}
