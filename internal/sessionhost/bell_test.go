package sessionhost

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBellScannerIgnoresStringTerminators(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []string
		want   bool
	}{
		{"bare", []string{"done\a"}, true},
		{"osc title", []string{"\x1b]0;~/src\a$ "}, false},
		{"osc split", []string{"\x1b]0;~/s", "rc\a$ "}, false},
		{"st then bell", []string{"\x1b]2;x\x1b\\", "\a"}, true},
		{"dcs aborted by csi", []string{"\x1bPq\x1b[0m\a"}, true},
		{"bell in csi", []string{"\x1b[1\am"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bellScanner
			rang := false
			for _, c := range tc.chunks {
				rang = b.feed([]byte(c)) || rang
			}
			assert.Equal(t, tc.want, rang)
		})
	}
}
