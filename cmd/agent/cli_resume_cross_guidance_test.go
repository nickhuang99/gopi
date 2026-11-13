// cli_resume_cross_guidance_test.go — the CROSS-GUIDANCE between the two live
// resume faces (`gopi session resume` and `gopi worker --resume`), asserted on
// the REAL BINARY's own help output.
//
// WHY THIS FILE EXISTS. Two surfaces resume work and they differ in the one way
// that costs an operator something: the session layer starts a run and closes
// NOTHING out (a successful resume leaves the row's worktree registered for
// good), while the worker layer takes that ground over and runs the close-out
// chain that RELEASES it. Choosing wrong leaks a worktree and nothing says so —
// a silent failure, which PROJECT.md Rule 15 forbids. The cheapest correct fix is
// not to remove a verb (the session face is the ONLY route for `gopi chat` and
// a bare `gopi "<prompt>"`, which have no row and therefore no ground) but to
// make each page name the other, and to give the choice where it is actually
// made: on the row, in `gopi task show`.
//
// WHAT IS PINNED HERE, and why each half is a separate case:
//
//	(a) `gopi session resume -h` names the worker command AND the consequence
//	    of staying here (nothing is released);
//	(b) `gopi worker -h` names the session command in its --resume paragraph —
//	    the mirror sentence, so neither page is the only teacher;
//	(c) `gopi task show <id>` prints the applicable face BESIDE the row's
//	    ground, because the row is where an operator starts (case (c) lives in
//	    internal/tools, at the renderer, plus the CLI-face case in
//	    cli_tasks_show_advisory_test.go).
//
// THE RED PROOF (Rule 31 + the project's red-proof convention): the guidance is
// asserted by the COMMAND STRING it must carry, not by a keyword. Deleting the
// added paragraph from either page turns the matching case red — observed, not
// argued; see the task report's Evidence section for the two red runs.
//
// Rule 31: every case drives the real binary over the argv an operator types.
// Rule 17: environment is set with t.Setenv. The binary comes from
// gopiTestBinPath (this package's ONE per-run build).
package main

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/cliverbs"
)

// resumeGuidanceTimeoutSec bounds one `-h` probe. A routing regression that
// dropped the flag into the agent loop would hang, and the timeout is what
// turns that into a failure (Rule 1: this is the named constant).
const resumeGuidanceTimeoutSec = 30

// resumeHelpPage runs `gopi <argv...>` in the sandbox profile and returns the
// combined output. It is the ONE probe helper of this file, so both pages are
// read through the same channel (Rule 14).
func resumeHelpPage(t *testing.T, argv ...string) string {
	t.Helper()
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	cmd := exec.Command(bin, argv...)
	cmd.Env = cliContractEnv(home)
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-timeoutAfter(resumeGuidanceTimeoutSec * time.Second):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		t.Fatalf("`%s %s` did not return — the route fell through to the agent loop (the timeout is the detector)",
			bin, strings.Join(argv, " "))
	}
	if err != nil {
		t.Fatalf("`%s %s`: %v\n%s", bin, strings.Join(argv, " "), err, out)
	}
	return string(out)
}

// TestSessionResumeHelpNamesTheWorkerFace is case (a). The session page is the
// one an operator reaches when they have a task id in hand — exactly the wrong
// destination when that row has a worktree — so it must name the worker command
// AND say what staying here costs them.
func TestSessionResumeHelpNamesTheWorkerFace(t *testing.T) {
	out := resumeHelpPage(t, CliVerbSession, CliVerbResume, HelpFlag)

	// The command must be THE shared spelling (Rule 14): a page teaching a
	// command the CLI does not dispatch is worse than no page.
	if !strings.Contains(out, cliverbs.ResumeTaskRowAddress) {
		t.Errorf("`gopi %s %s -h` must name %q — the ground-holding resume — got:\n%s",
			CliVerbSession, CliVerbResume, cliverbs.ResumeTaskRowAddress, out)
	}
	// And the CONSEQUENCE, which is the reason the choice matters (Rule 15).
	if !strings.Contains(out, "RELEASES its worktree") {
		t.Errorf("the session page must state what the worker face does that it does not "+
			"(the worktree release) — Rule 15, an unstated consequence is the defect:\n%s", out)
	}
	// The routes that keep this verb alive at all: an operator must not read
	// the guidance as "this verb is retired".
	if !strings.Contains(out, "has no row") {
		t.Errorf("the session page must keep teaching the no-row routes it exists for:\n%s", out)
	}
	// Rule 15's other half: a session-id resume links no ledger row, and the
	// page must say so rather than let the operator assume it was recorded.
	if !strings.Contains(out, "names no ledger row") {
		t.Errorf("the session page must state that a session-id resume links no ledger row:\n%s", out)
	}
}

// TestWorkerResumeHelpNamesTheSessionFace is case (b) — the mirror sentence.
// The worker page is where a reader learns --resume is the ground-holding face;
// without the reverse pointer it reads as the ONLY resume, and a plain session
// (`gopi chat`, a bare prompt, a killed run with no row) has no ground for it.
func TestWorkerResumeHelpNamesTheSessionFace(t *testing.T) {
	out := resumeHelpPage(t, CliVerbWorker, HelpFlag)

	resumeIdx := strings.Index(out, workerFlagResume)
	if resumeIdx < 0 {
		t.Fatalf("`gopi %s -h` no longer documents %s:\n%s", CliVerbWorker, workerFlagResume, out)
	}
	if !strings.Contains(out, cliverbs.ResumeSessionAddress) {
		t.Errorf("the worker page must name %q for the sessions that have no ground:\n%s",
			cliverbs.ResumeSessionAddress, out)
	}
	// The pointer belongs WITH --resume: a line at the far end of the page is
	// not the answer to a reader who is looking at --resume.
	tail := out[resumeIdx:]
	if !strings.Contains(tail, cliverbs.ResumeSessionAddress) {
		t.Errorf("the session face must be named after %s (beside the paragraph it qualifies), "+
			"not only elsewhere on the page:\n%s", workerFlagResume, out)
	}
	if !strings.Contains(tail, "ground-HOLDING") {
		t.Errorf("the worker page must state WHICH half of the choice it is (the ground-holding one):\n%s", tail)
	}
}

// TestResumeGuidanceNamesTheSameCommandsEverywhere is the anti-drift case: the
// three surfaces teach ONE choice, so they must name the SAME two commands. The
// sentences deliberately differ (each page carries its own wording and
// indentation), but the COMMANDS are single-sourced in internal/cliverbs, and
// this case fails if a page starts spelling one of them by hand.
func TestResumeGuidanceNamesTheSameCommandsEverywhere(t *testing.T) {
	sessionPage := resumeHelpPage(t, CliVerbSession, CliVerbResume, HelpFlag)
	workerPage := resumeHelpPage(t, CliVerbWorker, HelpFlag)

	for _, c := range []struct {
		page string
		want string
	}{
		{cliverbs.ResumeTaskRowAddress, sessionPage},
		{cliverbs.ResumeSessionAddress, workerPage},
	} {
		if !strings.Contains(c.want, c.page) {
			t.Errorf("the rendered page must carry %q verbatim — the shared spelling is the "+
				"single source (Rules 13/14):\n%s", c.page, c.want)
		}
	}
	// The two addresses are DIFFERENT commands, and the whole point is that a
	// reader can tell them apart: if a future edit collapsed one into the
	// other, the guidance would read as symmetric advice about one verb.
	if cliverbs.ResumeTaskRowAddress == cliverbs.ResumeSessionAddress {
		t.Fatalf("the two resume faces must stay two distinct addresses, both are %q",
			cliverbs.ResumeTaskRowAddress)
	}
}
