package shellenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"clubmate/internal/sessionhost"
)

const (
	marker         = "\n__CLUBMATE_ENV__\n"
	script         = `printf '\n__CLUBMATE_ENV__\n'; command env -0`
	clubmatePrefix = "CLUBMATE_"
	waitDelay      = 100 * time.Millisecond
)

var dropped = map[string]struct{}{
	"PWD": {}, "OLDPWD": {}, "SHLVL": {}, "_": {}, "TERM": {}, "TERMINFO": {},
	"TERM_PROGRAM": {}, "TERM_PROGRAM_VERSION": {}, "GHOSTTY_RESOURCES_DIR": {},
	"GHOSTTY_BIN_DIR": {}, "GHOSTTY_SHELL_FEATURES": {}, "__CFBundleIdentifier": {},
	"XPC_SERVICE_NAME": {}, "XPC_FLAGS": {},
}

func Resolve(ctx context.Context, shell string) ([]string, error) {
	cmd := exec.CommandContext(ctx, shell, "-l", "-i", "-c", script)
	cmd.Stdin = nil
	cmd.WaitDelay = waitDelay
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run %s: %w", shell, err)
	}
	_, payload, found := bytes.Cut(out, []byte(marker))
	if !found {
		return nil, fmt.Errorf("run %s: env marker not found", shell)
	}
	chunks := bytes.Split(payload, []byte{0})
	var env []string
	for _, entry := range chunks[:len(chunks)-1] {
		if len(entry) > 0 {
			env = append(env, string(entry))
		}
	}
	return env, nil
}

func Merge(base, resolved []string) []string {
	base = sessionhost.StripMarkers(slices.Clone(base))
	resolved = sessionhost.StripMarkers(slices.Clone(resolved))

	merged := make([]string, 0, len(base)+len(resolved))
	index := map[string]int{}
	set := func(entry string) {
		key, _, _ := strings.Cut(entry, "=")
		if _, drop := dropped[key]; drop {
			return
		}
		if i, ok := index[key]; ok {
			merged[i] = entry
			return
		}
		index[key] = len(merged)
		merged = append(merged, entry)
	}
	for _, entry := range base {
		set(entry)
	}
	for _, entry := range resolved {
		if strings.HasPrefix(entry, clubmatePrefix) {
			continue
		}
		set(entry)
	}
	return merged
}

func Apply(env []string) error {
	valid := slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
		key, _, ok := strings.Cut(entry, "=")
		return !ok || key == "" || strings.ContainsRune(entry, 0)
	})
	os.Clearenv()
	var errs []error
	for _, entry := range valid {
		key, value, _ := strings.Cut(entry, "=")
		if err := os.Setenv(key, value); err != nil {
			errs = append(errs, fmt.Errorf("setenv %s: %w", key, err))
		}
	}
	return errors.Join(errs...)
}
