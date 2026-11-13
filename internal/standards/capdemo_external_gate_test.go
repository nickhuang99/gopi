package standardgate

// capdemo_external_gate_test.go — the build-side half of the publish
// checkpoint for docs/archive/demo/external/.
//
// WHY THIS TEST EXISTS ON TOP OF THE SCRIPT. `scripts/capdemo-external-check.sh`
// is the one command a reader (or a publisher) runs; this test is what makes it
// run WITHOUT being remembered. The bundle's own rule is "every claim is an
// anchor", and it had already regressed silently once (two re-shot stills left
// stale digests and byte counts in the captions, two stills cited by
// devdoc/capability-annotation-handoff.md were renamed out of existence, and the
// edition still asserted that `gopi -h` advertises no `annotate` row after the
// verb landed). Nothing failed a build for any of that, which is the defect this
// file closes.
//
// It is deliberately a THIN wrapper: it does not re-implement one line of the
// audit, so the script's verdict and this gate's verdict cannot disagree. What
// it adds is an owner — the suite — and therefore a lifecycle: the checkpoint is
// exercised by `go test ./internal/standards/` instead of waiting for a human to
// remember the command.
//
// It creates no scratch directory (Rule 22 applies to nothing here), runs no
// gopi capability (it only reads `bin/gopi -h` when that binary is present, and
// the script reports a MISSING binary as a note rather than a stale anchor), and
// generates no load (Rule 27).
//
// THE PRECONDITION, stated out loud: this gate audits a bundle that a checkout
// does NOT carry. `docs/archive/demo/external/` is run output of tracked
// generators — stills rendered by gopi's own verbs — so it is untracked by
// design (GOPI.md Rule 39 + devdoc/demo-artifacts-untracked.md; the untrack
// removed 1295 tracked paths / 147 MB from `docs/archive/demo`, and this bundle
// is in that class), which means "the directory is not here" is the NORMAL state
// of a clone, not a lost deliverable. So when the bundle is absent this test
// reports its precondition — `docs/archive/demo/external is not part of this
// checkout and never was, so the publish checkpoint has no subject here` — and
// passes, because nothing is missing. The absence is never silent and never
// guessed: it is PROVEN from this checkout's own history (`git log -- <path>`
// is empty ⇒ the bundle was never in it), and an absent bundle that its own
// history DOES carry is still a hard failure (a deleted bundle, not an absent
// one). Reading a directory's absence as a bundle's absence is the rule this
// file follows — Rule 15: "nothing was found" and "I never looked" differ, and
// where the bundle IS present the gate still fails on any stale anchor.
//
// Rule 15: a green result must be a real measurement, so the test asserts the
// script actually REPORTED on the bundle (its summary line) rather than merely
// exiting 0 — a vacuous pass (deleted bundle, a script that prints nothing) is a
// failure, not a pass.

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	// capdemoCheckScriptRel is the ONE checkpoint command (Rule 14: the path is
	// named here and in the bundle's prose, never re-typed into the audit).
	capdemoCheckScriptRel = "scripts/capdemo-external-check.sh"
	// capdemoCheckOKMarker is the script's own success line. Its presence is the
	// anti-vacuous-pass evidence: an exit 0 with no report is not a green.
	capdemoCheckOKMarker = "every anchor in docs/archive/demo/external resolves"
	// capdemoCheckBundleRel is the directory whose absence would make the whole
	// check meaningless.
	capdemoCheckBundleRel = "docs/archive/demo/external"
	// capdemoAbsentMarker is what the gate prints when this checkout carries no
	// bundle to audit. It is a VALUE (a reader and the regression arm match on it,
	// Rule 1), never a re-typed literal.
	capdemoAbsentMarker = "capdemo-external: no subject in this checkout —"
	// capdemoHistoryTimeout bounds the one subprocess the absence proof runs
	// (Rule 2: the deadline lives here, once).
	capdemoHistoryTimeout = 30 * time.Second
	// capdemoCheckoutHistoryArgs reads this checkout's history for ONE path. An
	// empty output means the path was never committed here; a non-empty one means
	// it WAS, so its absence now is a deletion. One argv element per flag — a
	// single joined string is not a git command (measured).
	capdemoCheckoutHistoryArgs = "log --oneline -1 --"
)

// capdemoHistoryArgv is capdemoCheckoutHistoryArgs as the ARGV it must be — one
// element per token, so the two never drift apart.
var capdemoHistoryArgv = strings.Fields(capdemoCheckoutHistoryArgs)

// capdemoFataler is the one reporter surface the precondition needs — *testing.T
// in production, a recorder in the regression arms, so the arms exercise this
// function itself instead of a copy of it.
type capdemoFataler interface {
	Helper()
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
}

// capdemoReport is where the no-subject verdict goes. A passing test's t.Logf is
// not printed by `go test` (measured), and the acceptance for this gate is that a
// reader of the run SEES the reason — so it is written to stdout.
var capdemoReport io.Writer = os.Stdout

// reportCapdemoPrecondition settles what an absent bundle MEANS, from the
// checkout's own history rather than from the directory listing: absent AND never
// committed here ⇒ nothing is missing, so the gate has no subject and says so on
// stdout; absent BUT committed here ⇒ the deliverable was deleted, which is the
// failure this test has always reported.
//
// The distinction is the Rule 15 one: "I looked and the bundle is not part of this
// checkout" is a measurement, "I skipped because a directory was not there" is not.
func reportCapdemoPrecondition(t capdemoFataler, root, bundleRel string) {
	t.Helper()
	out, err := runCapdemoHistoryCheck(root, bundleRel)
	if err != nil {
		t.Fatalf("%s is absent and this checkout's history could not be read to decide what that "+
			"means (%v: %s) — an absence that cannot be explained is not a pass",
			bundleRel, err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("%s is absent, but this checkout's own history carries it (%s) — the bundle was "+
			"DELETED, and that is not the same as a checkout that never had it; restore it "+
			"rather than letting an empty directory turn this gate green",
			bundleRel, strings.TrimSpace(string(out)))
	}
	fmt.Fprintf(capdemoReport, "%s %s is absent and no commit in this checkout ever carried it "+
		"(docs/ is not part of this snapshot; the bundle is untracked run output by design). "+
		"The publish checkpoint has nothing to audit here.\n", capdemoAbsentMarker, bundleRel)
}

// runCapdemoHistoryCheck runs the ONE history probe the absence proof is built on.
func runCapdemoHistoryCheck(root, bundleRel string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), capdemoHistoryTimeout)
	defer cancel()
	argv := append(append(make([]string, 0, len(capdemoHistoryArgv)+1), capdemoHistoryArgv...), bundleRel)
	cmd := exec.CommandContext(ctx, gitBinaryLiteral, argv...)
	cmd.Dir = root
	return cmd.CombinedOutput()
}

// TestCapdemoExternalBundleAnchorsResolve runs the publish checkpoint over the
// bundle, when the bundle is part of this checkout. What it decides is
// runCapdemoCheck's business; the arms below pin each verdict.
func TestCapdemoExternalBundleAnchorsResolve(t *testing.T) {
	runCapdemoCheck(t, repoRoot(t))
}

// runCapdemoCheck is the gate itself, taking the repo root and a reporter so the
// regression arms drive THIS function (not a copy of its logic) over throwaway
// checkouts. Three arms exist and the output names which one ran:
//
//	bundle PRESENT → the checkpoint must be green. Red means a claim in
//	  docs/archive/demo/external/ has gone stale — the script's output names each
//	  one (the still, the recorded value, the measured value), so the fix is
//	  mechanical.
//	bundle ABSENT FROM THIS CHECKOUT (never tracked here — see the file header) →
//	  the gate has no subject, prints that precondition on stdout, and passes.
//	  This is a verdict about the bundle, not a skip: the absence is PROVEN from
//	  the checkout's own history before it is accepted (Rule 15).
//	bundle ABSENT BUT ITS HISTORY CARRIES IT → a hard failure, as it always was:
//	  somebody deleted a deliverable, and emptying a directory must not turn a red
//	  gate green.
func runCapdemoCheck(t capdemoFataler, root string) {
	t.Helper()
	script := filepath.Join(root, capdemoCheckScriptRel)
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("%s is absent: the publish checkpoint for %s has no executable form — "+
			"restore it rather than deleting this test", capdemoCheckScriptRel, capdemoCheckBundleRel)
	}
	bundle := filepath.Join(root, capdemoCheckBundleRel)
	if _, err := os.Stat(bundle); err != nil {
		reportCapdemoPrecondition(t, root, capdemoCheckBundleRel)
		return
	}

	cmd := exec.Command("bash", script)
	cmd.Dir = root
	out, runErr := cmd.CombinedOutput()
	report := string(out)
	if runErr != nil {
		t.Errorf("%s has stale or missing anchors (the checkpoint exited non-zero) — each line "+
			"below names the claim and the measurement that contradicts it; fix the caption/README "+
			"or re-shoot the still:\n%s", capdemoCheckBundleRel, report)
		return
	}
	if !strings.Contains(report, capdemoCheckOKMarker) {
		t.Fatalf("the publish checkpoint exited 0 without reporting on %s — a checkpoint that "+
			"printed no verdict is not a green (the bundle or the script was gutted):\n%s",
			capdemoCheckBundleRel, report)
	}
}
