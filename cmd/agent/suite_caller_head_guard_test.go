package main

// suite_caller_head_guard_test.go — the PACKAGE-LEVEL regression for the defect
// this row was reopened for: running this package moves the CALLER'S checkout
// (the clone ground a worker drives it in) off its task branch.
//
// THE MEASURED DEFECT (2026-10-28, task 01a17dd8; the reproduction and its
// timeline are in this row's briefing): in a worker's own clone ground on branch
// `task/<row-id>`, `go test ./cmd/agent/ -run 'TestResume' -count=1` left the
// ground on `master` — the deliverable's own commits vanished from the working
// tree, the tree under test and the tested binary no longer agreed, and a
// whole-package `make test-budget PKGS='./cmd/agent'` reported ELEVEN REFUSED
// arms plus exit 1. That exit 1 was not a code verdict: it was the environment
// having been broken by the run itself.
//
// WHY A PER-CASE ASSERTION IS NOT ENOUGH, and why this file exists beside
// TestResumeCloseoutDoesNotMoveTheTreeUnderTest (cli_resume_session_target_test.go):
// that case guards the ONE resume it drives. The measured trigger was not that
// case — it was the GROUP (`-run TestResume`) and, worse, the whole PACKAGE, and
// the mechanism was a child gopi started by an unrelated case reaching
// `git checkout <integration>` inside the caller's checkout. A guard that names
// one case can be green while another case moves the tree; the question is a
// property of the RUN, so the reading is taken by TestMain around `m.Run()`.
//
// THE ASSERTION is the caller's OWN checkout, read twice — branch, HEAD, and
// `git status --porcelain` — and the reading must be identical before and after
// the package ran. It deliberately repairs NOTHING: moving a checkout back would
// hide which case moved it, and the checkout is not this process's to move
// (Rule 29).

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The spellings this guard reads the checkout with. Named once (Rule 1b) so the
// command and the message that quotes it cannot drift.
const (
	suiteCallerHeadRevParseArg      = "rev-parse"
	suiteCallerHeadAbbrevRefArg     = "--abbrev-ref"
	suiteCallerHeadHeadArg          = "HEAD"
	suiteCallerHeadShowToplevelArg  = "--show-toplevel"
	suiteCallerHeadStatusArg        = "status"
	suiteCallerHeadPorcelainArg     = "--porcelain"
	suiteCallerHeadShortLen         = 12
	suiteCallerHeadSwitchSentence   = "SUITE CALLER-HEAD GUARD: SWITCH %s → %s"
	suiteCallerHeadMovedSentence    = "SUITE CALLER-HEAD GUARD: HEAD moved %s → %s"
	suiteCallerHeadDirtSentence     = "SUITE CALLER-HEAD GUARD: the caller's checkout was clean before this package ran, and is now dirty:\n%s"
	suiteCallerHeadCause            = " — a case in this package moved the checkout this suite runs in, which is the defect this guard exists for: the tree under test and the tested binary no longer agree"
	suiteCallerHeadHeldSentence     = "SUITE CALLER-HEAD GUARD: the caller's checkout held still (%s on %s at %s)"
	suiteCallerHeadUnreadableFormat = "SUITE CALLER-HEAD GUARD: unreadable %s in %s: %v"
	// suiteCallerHeadFixtureTaskBranch is the branch the red-proof fixture stands
	// on before the mutation — the `task/<id>` address a clone ground carries
	// (Rule 6a), spelled as a fixture value rather than an unreadable id.
	suiteCallerHeadFixtureTaskBranch = "task/fixture-caller-head-guard"
)

// suiteCallerHeadSnapshot is the caller checkout's state at one instant.
//
// An EMPTY checkout means the guard has nothing to compare: the suite is not
// running inside a git work tree (a `git archive` extract has no checkout to
// move). It is never "the tree looked fine" — that is a populated snapshot with
// equal readings.
type suiteCallerHeadSnapshot struct {
	checkout  string
	branch    string
	head      string
	porcelain string
}

// suiteCallerHeadArmed records that TestMain took the "before" reading, and
// suiteCallerHeadBefore is that reading. Making the wiring observable is what
// lets TestSuiteCallerHeadGuardIsArmed fail when the arm is deleted: a guard
// nobody calls is a green that measures nothing.
var (
	suiteCallerHeadArmed  bool
	suiteCallerHeadBefore suiteCallerHeadSnapshot
)

// suiteCallerHeadGit runs one git command anchored with `-C dir` and returns
// stdout, turning a non-zero exit into an error that carries the command and
// git's own stderr (Rule 15: a bare exit status would read as "no answer").
func suiteCallerHeadGit(dir string, args ...string) (string, error) {
	cmd := exec.Command(gitBinaryName, append([]string{"-C", dir}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s in %s: %w (stderr: %s)",
			gitBinaryName, strings.Join(args, " "), dir, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// readSuiteCallerHeadSnapshot reads the checkout this suite is running in.
//
// An unreadable top-level (not inside a work tree) is the ONE case that yields
// an EMPTY snapshot; every other unreadable answer is reported and leaves its
// field empty, which the comparator refuses rather than mistakes for "unchanged".
func readSuiteCallerHeadSnapshot() suiteCallerHeadSnapshot {
	checkout, err := suiteCallerHeadGit(".", suiteCallerHeadRevParseArg, suiteCallerHeadShowToplevelArg)
	if err != nil || checkout == "" {
		return suiteCallerHeadSnapshot{} // not a checkout: nothing for this guard to say
	}
	return readSuiteCallerHeadSnapshotInDir(checkout, func(what string, err error) {
		fmt.Fprintf(os.Stderr, suiteCallerHeadUnreadableFormat+"\n", what, checkout, err)
	})
}

// readSuiteCallerHeadSnapshotInDir reads a NAMED checkout, reporting an
// unreadable property through report rather than swallowing it.
func readSuiteCallerHeadSnapshotInDir(dir string, report func(what string, err error)) suiteCallerHeadSnapshot {
	snap := suiteCallerHeadSnapshot{checkout: dir}
	if branch, err := suiteCallerHeadGit(dir, suiteCallerHeadRevParseArg, suiteCallerHeadAbbrevRefArg, suiteCallerHeadHeadArg); err != nil {
		report(suiteCallerHeadAbbrevRefArg, err)
	} else {
		snap.branch = branch
	}
	if head, err := suiteCallerHeadGit(dir, suiteCallerHeadRevParseArg, suiteCallerHeadHeadArg); err != nil {
		report(suiteCallerHeadHeadArg, err)
	} else {
		snap.head = head
	}
	if porcelain, err := suiteCallerHeadGit(dir, suiteCallerHeadStatusArg, suiteCallerHeadPorcelainArg); err != nil {
		report(suiteCallerHeadStatusArg, err)
	} else {
		snap.porcelain = porcelain
	}
	return snap
}

// armSuiteCallerHeadGuard takes the "before" reading. TestMain calls it
// immediately before `m.Run()`, so the reading covers the WHOLE package: every
// case, every child, every `-run` group filter.
func armSuiteCallerHeadGuard() {
	suiteCallerHeadArmed = true
	suiteCallerHeadBefore = readSuiteCallerHeadSnapshot()
}

// reportSuiteCallerHeadAtCloseOut compares the caller's checkout with the
// "before" reading and returns how many PROPERTIES moved: the branch, the commit
// HEAD names, and whether a checkout that started clean is now dirty.
//
// TestMain turns a non-zero count into the suite's exit code — the same shape the
// leaked-child sweep uses, and for the same reason: a property whose only trace is
// a log line is indistinguishable from a property that held.
func reportSuiteCallerHeadAtCloseOut(before suiteCallerHeadSnapshot) int {
	if before.checkout == "" {
		return 0 // not a checkout: nothing was armed, nothing to report
	}
	now := readSuiteCallerHeadSnapshotInDir(before.checkout, func(what string, err error) {
		fmt.Fprintf(os.Stderr, suiteCallerHeadUnreadableFormat+"\n", what, before.checkout, err)
	})
	moved := 0
	if now.branch != before.branch {
		fmt.Fprintf(os.Stderr, suiteCallerHeadSwitchSentence+suiteCallerHeadCause+"\n", before.branch, now.branch)
		moved++
	}
	if now.head != before.head {
		fmt.Fprintf(os.Stderr, suiteCallerHeadMovedSentence+suiteCallerHeadCause+"\n", before.head, now.head)
		moved++
	}
	if strings.TrimSpace(before.porcelain) == "" && strings.TrimSpace(now.porcelain) != "" {
		fmt.Fprintf(os.Stderr, suiteCallerHeadDirtSentence+suiteCallerHeadCause+"\n", now.porcelain)
		moved++
	}
	if moved == 0 {
		fmt.Fprintf(os.Stderr, suiteCallerHeadHeldSentence+"\n", before.checkout, before.branch, shortHead(before.head))
	}
	return moved
}

// shortHead abbreviates a revision for a message a human reads; the full 40 hex
// are noise, and the value printed is still the one that decided.
func shortHead(rev string) string {
	if len(rev) <= suiteCallerHeadShortLen {
		return rev
	}
	return rev[:suiteCallerHeadShortLen]
}

// TestSuiteCallerHeadGuardIsArmed proves the guard is WIRED, not merely present:
// TestMain must have taken the "before" reading for this package run, and that
// reading must be complete.
//
// Without this arm, deleting `armSuiteCallerHeadGuard()`/the close-out from
// TestMain would leave every other test green and the guard dead — the vacuum
// shape this repository has paid for twice (a named check that does not run
// reads exactly like a clean one).
func TestSuiteCallerHeadGuardIsArmed(t *testing.T) {
	if !suiteCallerHeadArmed {
		t.Fatal("the caller-HEAD guard was never armed: TestMain must call armSuiteCallerHeadGuard() before m.Run(), " +
			"or nothing in this package checks that the checkout it runs in is still on its branch afterwards")
	}
	snap := suiteCallerHeadBefore
	if snap.checkout == "" {
		t.Skip("this suite is not running inside a git checkout, so there is no caller HEAD to keep still")
	}
	if snap.branch == "" || snap.head == "" {
		t.Fatalf("the armed snapshot is incomplete: checkout=%q branch=%q head=%q — the comparator would compare "+
			"empty strings and call a moved checkout unchanged", snap.checkout, snap.branch, snap.head)
	}
	if got, err := suiteCallerHeadGit(snap.checkout, suiteCallerHeadRevParseArg, suiteCallerHeadShowToplevelArg); err != nil {
		t.Fatalf("re-reading the armed checkout %s: %v", snap.checkout, err)
	} else if got != snap.checkout {
		t.Fatalf("the guard armed on %q but the same reading now resolves %q — the comparison would be between two "+
			"different checkouts", snap.checkout, got)
	}
}

// TestSuiteCallerHeadGuardReportsAMovedHead is the guard's RED PROOF: the
// comparator must FIRE when a checkout moves, and must name the branch it moved
// from and to. It drives a throwaway fixture repo and never the caller's own
// checkout (Rule 29), so the assertion does not depend on the defect still being
// reproducible on this host.
//
// The mutation is the one the defect performs: `git checkout` another branch. A
// comparator that returned 0 here would be a green nobody could tell from a clean
// run — which is exactly the failure mode a package-level guard must not have.
func TestSuiteCallerHeadGuardReportsAMovedHead(t *testing.T) {
	ground := t.TempDir()
	testGit(t, ground, "init", "-q", "-b", masterBranchName)
	testGit(t, ground, "config", "user.email", resumeLatchFixtureGitEmail)
	testGit(t, ground, "config", "user.name", resumeLatchFixtureGitName)
	// The integration branch gets a COMMIT before anything else: an unborn branch
	// cannot be checked back out, and a fixture that could not perform the
	// mutation would fail for the fixture's reason instead of the guard's.
	if err := os.WriteFile(ground+"/"+resumeLatchFixtureRepoFile, []byte(resumeLatchFixtureRepoBody), 0o644); err != nil {
		t.Fatalf("write the fixture's file: %v", err)
	}
	testGit(t, ground, "add", resumeLatchFixtureRepoFile)
	testGit(t, ground, "-c", "commit.gpgsign=false", "commit", "-q", "-m", resumeLatchFixtureRepoSubject)
	testGit(t, ground, "checkout", "-q", "-b", suiteCallerHeadFixtureTaskBranch)

	report := func(what string, err error) { t.Fatalf("read %s in %s: %v", what, ground, err) }
	before := readSuiteCallerHeadSnapshotInDir(ground, report)
	if before.branch != suiteCallerHeadFixtureTaskBranch {
		t.Fatalf("fixture branch = %q, want %q", before.branch, suiteCallerHeadFixtureTaskBranch)
	}
	// THE MUTATION: exactly what the close-out leg did to a live run's ground.
	testGit(t, ground, "checkout", "-q", masterBranchName)

	if moved := reportSuiteCallerHeadAtCloseOut(before); moved == 0 {
		t.Fatalf("the guard reported NO movement for a checkout that went %q → %q — a comparator blind to this "+
			"is the vacuum that let eleven REFUSED arms read as a code verdict",
			suiteCallerHeadFixtureTaskBranch, masterBranchName)
	}
}

// TestSuiteCallerHeadGuardCallsAnUnmovedCheckoutUnchanged is the guard's GREEN
// CONTROL: a checkout that did not move must report ZERO movements. Without it, a
// comparator that always answers non-zero would look like a working red proof.
func TestSuiteCallerHeadGuardCallsAnUnmovedCheckoutUnchanged(t *testing.T) {
	ground := t.TempDir()
	testGit(t, ground, "init", "-q", "-b", masterBranchName)
	testGit(t, ground, "config", "user.email", resumeLatchFixtureGitEmail)
	testGit(t, ground, "config", "user.name", resumeLatchFixtureGitName)
	if err := os.WriteFile(ground+"/"+resumeLatchFixtureRepoFile, []byte(resumeLatchFixtureRepoBody), 0o644); err != nil {
		t.Fatalf("write the fixture's file: %v", err)
	}
	testGit(t, ground, "add", resumeLatchFixtureRepoFile)
	testGit(t, ground, "-c", "commit.gpgsign=false", "commit", "-q", "-m", resumeLatchFixtureRepoSubject)

	report := func(what string, err error) { t.Fatalf("read %s in %s: %v", what, ground, err) }
	before := readSuiteCallerHeadSnapshotInDir(ground, report)
	if moved := reportSuiteCallerHeadAtCloseOut(before); moved != 0 {
		t.Fatalf("an unmoved checkout reported %d movements — a guard that always fires cannot tell a broken run "+
			"from a clean one", moved)
	}
}
