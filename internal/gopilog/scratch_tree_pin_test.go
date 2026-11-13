package gopilog

import (
	"testing"

	"github.com/nick/gopi/internal/scratch"
)

// TestRawLogDirNameMatchesScratchTree pins this package's raw-directory leaf
// name to the ONE declaration of it (scratch.declared_trees.go), the same shape
// sessionlayout's TestSideFileNamesMatchWriters uses for the provenance side
// file.
//
// WHY IT MUST BE A TEST AND NOT AN IMPORT: the direction of the dependency
// forbids sharing the constant directly in one of the two files. gopilog imports
// config (config.GopiDir), and config imports scratch — so gopilog CAN import
// scratch and compare. The comparison is what matters: if either spelling moves,
// the sweep's declared "raw" tree and the writer's actual raw log directory stop
// being the same directory, and the sweep would then bound a tree nothing writes
// to while the real one grows unbounded (the exact defect this epic closes).
func TestRawLogDirNameMatchesScratchTree(t *testing.T) {
	sample := scratch.RawLogNamePrefix + "20260920-120000" + scratch.RawLogFileSuffix
	if !rawLogNameRe.MatchString(sample) {
		t.Fatalf("the raw-log reader does not accept a name built from the declared tree's own constants (%q) — the sweep would bound files the reader cannot see", sample)
	}
	if rawLogNameRe.MatchString("fmt-detail.log") {
		t.Fatalf("the raw-log reader accepts a name outside the declared namespace — the two must agree on what is ours")
	}
	if rawLogsDir != scratch.RawLogDirName() {
		t.Fatalf("raw log directory name drifted: gopilog=%q, declared tree=%q — the sweep would bound the wrong tree",
			rawLogsDir, scratch.RawLogDirName())
	}
}
