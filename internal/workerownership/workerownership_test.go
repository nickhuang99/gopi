package workerownership

// PRODUCTION-SIDE pin for the slash-pair rule (Rule 1b/13: the separator the
// matcher keys on is an exported constant, referenced — never a literal at the
// matching site — and the helper the predicate uses is exercised directly).
//
// The repro that proves the BUG (slash_pair_repro_test.go) deliberately compiles
// against the pre-fix source, so it cannot name the post-fix symbols. This file
// carries the other half: it ties the local value the repro uses to the
// production constant, so the two can never drift, and it exercises
// abbreviatedWithSlash at its boundaries.

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/procname"
)

// TestLaunchShapeSeparatorIsTheCorpusSeparator pins the exported constant and
// the boundary behaviour of the left-hand test, so a matcher edited to key on
// another character (or to test the wrong side of the shape) fails here.
func TestLaunchShapeSeparatorIsTheCorpusSeparator(t *testing.T) {
	if string(LaunchShapeSeparator) != slashPairSeparator {
		t.Fatalf("LaunchShapeSeparator = %q, but the repro's local separator is %q — the rule and its proof have drifted",
			LaunchShapeSeparator, slashPairSeparator)
	}
	root := procname.NameGopi + " " + procname.VerbTask + " " + TaskRunSubverb
	if !abbreviatedWithSlash(root+string(LaunchShapeSeparator)+procname.VerbWorker, len(root)) {
		t.Errorf("abbreviatedWithSlash does not see the separator right after the shape — the left-hand test is blind")
	}
	if abbreviatedWithSlash(root+" <id>", len(root)) {
		t.Errorf("abbreviatedWithSlash reports an abbreviation where the shape is written out")
	}
	if abbreviatedWithSlash(root, len(root)) {
		t.Errorf("abbreviatedWithSlash reports an abbreviation at end of body (no separator follows)")
	}
}
