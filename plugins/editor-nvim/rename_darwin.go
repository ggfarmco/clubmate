//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func renameExcl(from, to string) error {
	err := unix.RenamexNp(from, to, unix.RENAME_EXCL)
	switch {
	case errors.Is(err, unix.EEXIST):
		return fmt.Errorf("config-copy: %s %w", to, errDestExists)
	case err != nil:
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}
