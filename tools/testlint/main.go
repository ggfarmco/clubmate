package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"clubmate/tools/testlint/analyzer"
)

func main() {
	singlechecker.Main(analyzer.Analyzer)
}
