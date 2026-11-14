package tools

import (
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// TestWorktreeStubNamePrefixAgreesWithTheGroundPrefix pins the ONE value the
// sweep package cannot import back: internal/scratch owns the stub predicate and
// cannot import internal/tools (tools imports scratch), so the `gopi-` prefix
// exists twice by necessity — here as the producer's TaskGroundDirPrefix, there
// as WorktreeStubNamePrefix. This test is the tie Rule 14 requires for a value
// that cannot be hoisted, and it is placed in the OWNING package (the side that
// can import both).
func TestWorktreeStubNamePrefixAgreesWithTheGroundPrefix(t *testing.T) {
	if scratch.WorktreeStubNamePrefix != TaskGroundDirPrefix {
		t.Fatalf("the worktree-stub sweep looks for %q while task grounds are named %q — the sweep would find nothing, or the wrong things",
			scratch.WorktreeStubNamePrefix, TaskGroundDirPrefix)
	}
	// And the NAME the sweep's family regex accepts is the name this producer
	// mints, asserted on a real row id: a ground whose stem the predicate rejects
	// is a leak nothing can reclaim.
	ground := TaskGroundDirPrefix + taskGroundIDStem("01a0e85b-d697-7df7-90e4-a048b251610c")
	if !scratch.WorktreeStubNameMatches(filepath.Base(ground)) {
		t.Fatalf("the producer names a ground %q that the sweep's family does not recognise", ground)
	}
}
