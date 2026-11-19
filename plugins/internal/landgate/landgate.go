package landgate

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	CommonEnv      = "CLUBMATE_LAND_COMMON"
	ProtectEnv     = "CLUBMATE_LAND_PROTECT"
	ProtectGlobEnv = "CLUBMATE_LAND_PROTECT_GLOB"
	VerdictsEnv    = "CLUBMATE_LAND_VERDICTS"
	priorCountEnv  = "CLUBMATE_GIT_CONFIG_COUNT"
	configCountEnv = "GIT_CONFIG_COUNT"
	hooksPathKey   = "core.hooksPath"

	Approved = "approved"
	Changes  = "changes"

	ByReviewer = "reviewer"
	ByDocs     = "docs"
	ByUser     = "user"
	ByNoGate   = "nogate"
)

var (
	//go:embed hook.sh
	hookScript []byte

	hookNames = []string{
		"applypatch-msg", "pre-applypatch", "post-applypatch", "pre-commit", "pre-merge-commit", "prepare-commit-msg",
		"commit-msg", "post-commit", "pre-rebase", "post-checkout", "post-merge", "pre-push", "pre-receive", "update",
		"proc-receive", "post-receive", "post-update", "reference-transaction", "pre-auto-gc", "post-rewrite",
		"sendemail-validate", "post-index-change", "p4-changelist", "p4-prepare-changelist", "p4-post-changelist",
		"p4-pre-submit",
	}
)

type Verdict struct {
	Old      string    `json:"old"`
	Tree     string    `json:"tree"`
	Outcome  string    `json:"outcome"`
	By       string    `json:"by"`
	Reviewer string    `json:"reviewer,omitempty"`
	Commit   string    `json:"commit,omitempty"`
	Findings []string  `json:"findings,omitempty"`
	Deferred []string  `json:"deferred,omitempty"`
	Summary  string    `json:"summary,omitempty"`
	At       time.Time `json:"at"`
}

func Install(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range hookNames {
		path := filepath.Join(dir, name)
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, hookScript) {
			continue
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, hookScript, 0o755); err != nil {
			return err
		}
		if err := os.Chmod(tmp, 0o755); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
	}
	return nil
}

var Inherited = []string{priorCountEnv, CommonEnv, ProtectEnv, ProtectGlobEnv, VerdictsEnv}

func SessionEnv(env []string, hooksDir, common string, protect, glob []string, verdicts string) map[string]string {
	if common == "" || len(protect) == 0 {
		return map[string]string{}
	}
	count := 0
	for _, kv := range env {
		if name, value, _ := strings.Cut(kv, "="); name == configCountEnv {
			count, _ = strconv.Atoi(value)
		}
	}
	return map[string]string{
		priorCountEnv:                             strconv.Itoa(count),
		fmt.Sprintf("GIT_CONFIG_KEY_%d", count):   hooksPathKey,
		fmt.Sprintf("GIT_CONFIG_VALUE_%d", count): hooksDir,
		configCountEnv:                            strconv.Itoa(count + 1),
		CommonEnv:                                 common,
		ProtectEnv:                                strings.Join(protect, " "),
		ProtectGlobEnv:                            strings.Join(glob, " "),
		VerdictsEnv:                               verdicts,
	}
}

func Read(dir, old, tree string) (Verdict, bool) {
	var v Verdict
	data, err := os.ReadFile(filepath.Join(dir, key(old, tree)))
	if err != nil || json.Unmarshal(data, &v) != nil {
		return Verdict{}, false
	}
	return v, true
}

func Write(dir string, v Verdict) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, key(v.Old, v.Tree))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func key(old, tree string) string {
	return old + "-" + tree + ".json"
}

const anyDepth = "**"

func Match(pattern, name string) bool {
	return matchParts(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchParts(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	if pattern[0] == anyDepth {
		for i := range len(name) + 1 {
			if matchParts(pattern[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, _ := path.Match(pattern[0], name[0])
	return ok && matchParts(pattern[1:], name[1:])
}
