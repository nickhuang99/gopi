package testutil

// REAL-TREE REPRO for the slash-pair false positive (Rule 5).
//
// devdoc/tmpdir-param-not-banner.md:60 names TWO group verbs at once —
// "(`gopi task run/worker`, `gopi merger`)" — to say they share one exit path
// from main(). The gate's needle `gopi task run` is a substring of the pair
// `task run/worker`, so the document read as a launch instruction and BOTH
// TestWorkerOwnershipPointerPresent and
// TestWorkerOwnershipGateStaysGreenAcrossSuccessiveMergeRounds were red on
// every tip carrying it. The fix belongs in the matcher (the leaf predicate),
// because the bytes are a correct sentence about the subcommand chain — the
// only two alternatives were worse: dismissing the file hides a real document
// from the audit, and stamping the operator-only pointer on a TMPDIR document
// would state something untrue.
//
// THE TEST IS DISCRIMINATING ON PURPOSE. A "the file is no longer an offender"
// assertion alone could be satisfied by a predicate that stopped matching
// anything. So the second half REWRITES the same sentence with both verbs
// written out (`gopi task run and worker`) and requires it to be reported: the
// green comes from the abbreviation rule, not from a blind matcher.
//
// The verdict is asked of the gate's OWN decision (workerOwnershipUncovered)
// over the real tree, so what is asserted is exactly what the gate asserts.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/workerownership"
)

const (
	// wodSlashPairDocRel is the measured victim document: the TMPDIR report
	// whose §"EnvVars" row names the group verbs as a slash pair.
	wodSlashPairDocRel = workerownership.DirDevdoc + "/tmpdir-param-not-banner.md"
	// wodSlashPairSeparator is the character the corpus's abbreviations join
	// two verbs with. It is spelled locally so this regression COMPILES against
	// the pre-fix source (a build error would prove nothing about the bug);
	// TestWorkerOwnershipSlashPairIsNotAnOffender asserts at RUNTIME that it
	// equals the leaf package's exported constant — so the two cannot drift,
	// and a re-spelling of the separator reddens here (Rule 1b/13).
	wodSlashPairSeparator = "/"
)

// wodSlashPairShape is the abbreviation the document uses, as the needle set
// would have embedded it — `task run` immediately followed by the separator and
// the second verb. Built (not typed) from the leaf package's values and the
// needle set, so a rename on either side reddens this regression.
var (
	wodSlashPairShape = workerownership.TaskRunSubverb + wodSlashPairSeparator +
		workerownershipLauncherVerb()
	// wodSlashPairWrittenOut is the SAME sentence with both verbs spelled out,
	// which is a genuine launch instruction and must stay reported.
	wodSlashPairWrittenOut = workerownership.TaskRunSubverb + " and " + workerownershipLauncherVerb()
)

// workerownershipLauncherVerb is the worker verb, taken from the needle set
// rather than re-typed (Rule 13/14: the value has ONE source — the leaf
// package built the needle from internal/procname).
func workerownershipLauncherVerb() string {
	needle := workerownership.Needles[0] // "<name> <worker-verb>"
	parts := strings.SplitN(needle, " ", 2)
	return parts[len(parts)-1]
}

// TestWorkerOwnershipSlashPairDocIsNotAnOffender is the regression: the real
// TMPDIR document must not be an offender, must NOT carry a pointer (the fix is
// not an annotation), and the whole-tree gate decision must be empty — while
// the needle still fires on the written-out form of the very same sentence.
func TestWorkerOwnershipSlashPairDocIsNotAnOffender(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	path := filepath.Join(root, filepath.FromSlash(wodSlashPairDocRel))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (the measured victim — if it moved, re-point this regression): %v", wodSlashPairDocRel, err)
	}
	body := string(raw)

	// The separator this regression builds its shapes from must be the one the
	// matcher keys on (Rule 1b/13): checked at RUNTIME so the file still
	// compiles against pre-fix source, where the constant does not exist yet.
	if wodSlashPairSeparator != string(workerownership.LaunchShapeSeparator) {
		t.Fatalf("this regression joins verbs with %q, the matcher keys on %q — the rule and its proof have drifted",
			wodSlashPairSeparator, workerownership.LaunchShapeSeparator)
	}

	// PREMISE: the document really is the slash-pair shape, so a green below
	// cannot come from a file that never carried it.
	if !strings.Contains(body, wodSlashPairShape) {
		t.Fatalf("%s no longer contains the slash pair %q — this regression would measure nothing", wodSlashPairDocRel, wodSlashPairShape)
	}
	if workerownership.MentionsLaunchShape(body) {
		t.Errorf("%s mentions a launch shape — the slash-joined pair is an abbreviation naming two verbs, not a command: the matcher must ignore a needle at the LEFT of %q",
			wodSlashPairDocRel, workerownership.LaunchShapeSeparator)
	}
	// The fix must NOT be an annotation: a TMPDIR document is not an
	// operator-only launch document, so a pointer here would be an untrue
	// statement (and adding the file to the dismiss table is forbidden too).
	if _, ok := workerownership.AnnotationLine(body); ok {
		t.Errorf("%s carries the canonical pointer — the matcher was meant to be fixed, not the document annotated", wodSlashPairDocRel)
	}

	missing, err := workerOwnershipUncovered(root)
	if err != nil {
		t.Fatalf("gate decision: %v", err)
	}
	for _, rel := range missing {
		if rel == wodSlashPairDocRel {
			t.Errorf("%s is still reported by the gate's own decision (%v)", wodSlashPairDocRel, missing)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("the gate decision over the real tree is not clean: %v", missing)
	}

	// RED-PROOF (the discriminating half): the SAME sentence with both verbs
	// written out IS a launch instruction and must still be caught. Without
	// this, the assertions above could be green because the predicate went
	// blind — the classic vacuous fix.
	writtenOut := strings.Replace(body, wodSlashPairShape, wodSlashPairWrittenOut, 1)
	if writtenOut == body {
		t.Fatalf("the rewrite of %q to %q changed nothing — the red-proof measures nothing", wodSlashPairShape, wodSlashPairWrittenOut)
	}
	if !workerownership.MentionsLaunchShape(writtenOut) {
		t.Errorf("with both verbs written out (%q) the matcher no longer fires — the abbreviation rule must not weaken a real launch shape",
			wodSlashPairWrittenOut)
	}
}
