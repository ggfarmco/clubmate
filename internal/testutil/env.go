package testutil

import (
	"os"
	"slices"
	"strings"
)

const hermeticEnv = "TESTUTIL_HERMETIC"

var leakedGitVars = []string{
	"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS",
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX",
}

func init() {
	if os.Getenv(hermeticEnv) != "" {
		return
	}
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); leaked(k) {
			os.Unsetenv(k)
		}
	}
	os.Setenv(hermeticEnv, "1")
}

func leaked(k string) bool {
	switch {
	case strings.HasPrefix(k, "CLUBMATE_TEST_"):
		return false
	case strings.HasPrefix(k, "CLUBMATE_"), strings.HasPrefix(k, "GIT_CONFIG_KEY_"), strings.HasPrefix(k, "GIT_CONFIG_VALUE_"):
		return true
	}
	return slices.Contains(leakedGitVars, k)
}
