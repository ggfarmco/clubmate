package analyzer_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"clubmate/tools/testlint/analyzer"
)

func TestFixedSleep(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), analyzer.Analyzer, "clubmate/r1")
}

func TestUnscaledTestifyWait(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), analyzer.Analyzer, "r2")
}

func TestReadOnce(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), analyzer.Analyzer, "clubmate/r3")
}

func TestSharedTempPath(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), analyzer.Analyzer, "r4")
}
