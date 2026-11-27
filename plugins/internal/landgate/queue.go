package landgate

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

const queueMark = "queued\n"

func QueueDir(hooksDir, common string) string {
	sum := sha1.Sum([]byte(common))
	return filepath.Join(hooksDir, "queue", hex.EncodeToString(sum[:])[:16])
}

func WriteQueueMark(dir, branch string, on bool) error {
	path := filepath.Join(dir, "refs", "heads", filepath.FromSlash(branch))
	if !on {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(queueMark), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
