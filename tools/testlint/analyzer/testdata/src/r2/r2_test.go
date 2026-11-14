package r2

import (
	"time"

	"github.com/stretchr/testify/require"
)

func helper() {
	require.Eventually(nil, func() bool { return true }, time.Second, time.Millisecond) // want "unscaled deadline"
	require.Never(nil, func() bool { return false }, time.Second, time.Millisecond)     // want "unscaled deadline"
}
