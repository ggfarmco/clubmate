package proc_test

import (
	"testing"

	"clubmate/internal/proc"
)

func BenchmarkSnapshot(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := proc.Snapshot(); err != nil {
			b.Fatal(err)
		}
	}
}
