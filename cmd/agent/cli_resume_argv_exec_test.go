package main

// THE SPAWNED CHILD AND THE RECORDED COMMAND ARE ONE SHAPE (epic resume-surface
// convergence, step 2 of 3).
//
// Step 1 gave the resume a SECOND address — the no-verb `gopi --session
// <target> [--task <id>]` face — while `internal/runcmd.ResumeArgv` kept
// rendering the older `gopi session resume …` spelling. Two consumers read that
// renderer and both would have gone on spawning (and recording) the shape the
// epic is converging away from:
//
//   - cmd/agent's worker-resume child (`workerResumeChild`), and
//   - internal/tools' dead-holder arm (`spawnDeadHolderResume`),
//
// plus the run_cmd stamp in cmd/agent/cli_resume.go, which built its own argv.
//
// WHY THESE ASSERTIONS ARE EXECUTED RATHER THAN RENDERED (Rule 31): a renderer
// that emits a command the receiving FACE does not accept is exactly the defect
// this epic keeps producing — `gopi resume …` (refused by name),
// `gopi session resume …` (an address step 3 retires), and a `--session` argv
// whose face refused `--task` (measured, before this step: exit 1, `unknown
// option "--task"`, i.e. every spawned recovery child would have run
// SESSION-ONLY and left the crashed row unclosed). So the argv this test
// asserts on is handed to the REAL binary and the binary's own words are read
// back.
//
// The assertions are deliberately cheap and API-free (an unresolvable target and
// a nonexistent row stop before any model call): what is being pinned is the
// ARGV CONTRACT, not a happy path.

import (
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/runcmd"

	_ "modernc.org/sqlite"
)

// TestResumeArgvIsExecutedByTheFaceThatAcceptsIt runs the rendered argv and
// requires the RECEIVING face's own sentence back — proof that the child the
// worker spawns lands on the no-verb resume face instead of an unknown-option
// refusal (or, worse, the bare-prompt agent path).
// makeSessionRoot creates the profile's session root for a probe HOME, so the
// resolver reaches its OWN "neither … nor …" sentence instead of stopping at
// open(2) one layer earlier (measured 2026-10-18; memory #12570 records the same
// obligation for this face's own contract test).
// TestResumeArgvIsExecutedByTheFaceThatAcceptsIt runs the rendered argv and
// requires the RECEIVING face's own sentence back — proof that the child the
// worker spawns lands on the no-verb resume face instead of an unknown-option
// refusal (or, worse, the bare-prompt agent path).
// makeSessionRoot creates the profile's session root for a probe HOME, so the
// resolver reaches its OWN "neither … nor …" sentence instead of stopping at
// open(2) one layer earlier (measured 2026-10-18; memory #12570 records the same
// obligation for this face's own contract test).
func makeSessionRoot(t *testing.T, home string) {
	t.Helper()
	if err := os.MkdirAll(sessionFlagSessionsDir(t, home), 0o755); err != nil {
		t.Fatalf("fixture: create the session root under %s: %v", home, err)
	}
}

func TestResumeArgvIsExecutedByTheFaceThatAcceptsIt(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	// The session ROOT must exist before the probe, or the resolver stops one
	// layer earlier and answers with an open(2) error instead of its own
	// "neither a raw log path nor an existing session id" sentence (measured
	// 2026-10-18; the same fixture obligation memory #12570 records for this
	// face's own contract test).
	makeSessionRoot(t, home)

	// A target that resolves to nothing: the run stops at resolution, before a
	// checkpoint, a model call or a session artifact.
	const unknownTarget = "01a096c9-6f7d-7e43-8e46-9ecca58e9cd0"
	argv := runcmd.ResumeArgv(unknownTarget, "")
	// Execute the RENDERED words (argv[0] reduced to its basename by Command),
	// not the slice: the recorded form is what a reader pastes back.
	rendered := strings.Fields(runcmd.Command(argv))[1:]
	out, err := runCLIContractProbeRaw(t, bin, home, rendered...)
	if err == nil {
		t.Fatalf("%s exited 0 on an unresolvable target — it did not reach the resume face\n%s",
			runcmd.Command(argv), out)
	}
	// The face names ITSELF by the argv the caller typed (rule: a surface's
	// diagnostic carries the surface's own spelling).
	if !strings.Contains(out, "gopi "+runcmd.FlagSession+":") {
		t.Fatalf("%s: the refusal does not come from the --session face — got:\n%s", runcmd.Command(argv), out)
	}
	if strings.Contains(out, "unknown option") || strings.Contains(out, "unknown flag") {
		t.Fatalf("%s: the face REFUSED the argv this renderer emits — got:\n%s", runcmd.Command(argv), out)
	}
	if !strings.Contains(out, "is neither a raw log path nor an existing session id") {
		t.Fatalf("%s: the child did not reach the resume resolution — got:\n%s", runcmd.Command(argv), out)
	}
}

// TestResumeArgvCarriesTheRowLinkageToTheChild: the `--task` half is what makes
// a spawned recovery CLOSE OUT a row at all, so the face must ACCEPT it. Before
// this step it refused it by name (`unknown option "--task"`, exit 1) — which an
// argv assertion cannot see, because the renderer looked fine while every
// spawned child would have run SESSION-ONLY and left the crashed row unclosed.
//
// The evidence is the face's OWN two answers, and their DIFFERENCE is the point:
// `--bogus` is refused as an unknown option, `--task` is recognized and asked
// for its operand. (An unresolvable TARGET stops the run at resolution, before
// the row is ever consulted — so that sentence cannot be the evidence here; the
// end-to-end proof of the linkage is TestWorkerResumeStampsTheConvergedRunCmd,
// where the child stamps method=resume onto the row.)
// TestResumeArgvCarriesTheRowLinkageToTheChild: the `--task` half is what makes
// a spawned recovery CLOSE OUT a row at all, so the face must ACCEPT it. Before
// this step it refused it by name (`unknown option "--task"`, exit 1) — which an
// argv assertion cannot see, because the renderer looked fine while every
// spawned child would have run SESSION-ONLY and left the crashed row unclosed.
//
// The evidence is the face's OWN two answers, and their DIFFERENCE is the point:
// `--bogus` is refused as an unknown option, `--task` is recognized and asked
// for its operand. (An unresolvable TARGET stops the run at resolution, before
// the row is ever consulted — so that sentence cannot be the evidence here; the
// end-to-end proof of the linkage is TestWorkerResumeStampsTheConvergedRunCmd,
// where the child stamps method=resume onto the row.)
func TestResumeArgvCarriesTheRowLinkageToTheChild(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	makeSessionRoot(t, home)

	const unknownTarget = "01a096c9-6f7d-7e43-8e46-9ecca58e9cd0"

	// ① the flag WITH an operand is not refused by name.
	argv := runcmd.ResumeArgv(unknownTarget, "ffffffff-ffff-7fff-bfff-ffffffffffff")
	rendered := strings.Fields(runcmd.Command(argv))[1:]
	out, _ := runCLIContractProbeRaw(t, bin, home, rendered...) //stylegate:ignore the probe's exit status is the CONTRACT under test here; the assertion is on the rendered output, and a non-zero status is an accepted answer
	if strings.Contains(out, "unknown option") {
		t.Fatalf("%s: the face REFUSED the row linkage — a spawned child could not close the row it resumed:\n%s",
			runcmd.Command(argv), out)
	}
	// ② and it is CONSUMED (its operand is not read as the prompt): the tail
	// after the flag is what the face refuses, when anything is left over.
	bad, _ := runCLIContractProbeRaw(t, bin, home, runcmd.FlagSession, unknownTarget, runcmd.FlagTask) //stylegate:ignore the refusal being asserted is carried in the output, not in the exit status; a failure to spawn already lands in the buffer the assertion reads
	if !strings.Contains(bad, "requires a non-empty row id") {
		t.Fatalf("%s %s with no operand: the face did not ask for the row id — got:\n%s",
			runcmd.FlagSession, runcmd.FlagTask, bad)
	}
	// ③ the CONTRAST that makes ② evidence: an option the face does not carry is
	// still refused BY NAME (Rule 15), so the two arms cannot both be "accepted".
	bogus, _ := runCLIContractProbeRaw(t, bin, home, runcmd.FlagSession, unknownTarget, "--bogus") //stylegate:ignore the stray-option arm asserts on the refusal TEXT; the exit status is expected to be non-zero and is not the subject
	if !strings.Contains(bogus, "unknown option") {
		t.Fatalf("%s --bogus: a stray option was not refused by name — got:\n%s", runcmd.FlagSession, bogus)
	}
}

// TestWorkerResumeStampsTheConvergedRunCmd is the STAMP half of step 2, observed
// on a real row: after a `gopi worker --resume --branch` run, the ledger's
// run_cmd is the converged shape (and therefore replayable by the face that
// accepts it). The row is read back through the ledger, not through the run's
// own stdout — a stamp the run merely SAYS it wrote is not evidence.
