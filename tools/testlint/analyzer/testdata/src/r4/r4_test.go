package r4

import (
	"os"
	"path/filepath"
)

func helper(name string) {
	_ = filepath.Join(os.TempDir(), "fixed") // want "shared fixed temp path"
	_ = os.TempDir() + "/fixed"              // want "shared fixed temp path"
	_ = filepath.Join(os.TempDir(), name)
}
