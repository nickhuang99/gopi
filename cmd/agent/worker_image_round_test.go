package main

// worker_image_round_test.go — the WORKER claim loop's per-round image check
// (image_selfcheck.go's workerImageRoundCheck) and the `--status` image column
// (cli_worker_status.go's workerImageRowNote).
//
// WHAT IT PINS, and why each half would have caught the measured incident (row
// 01a18301, 2026-10-28):
//
//   - THE PROLOGUE IS NOT ENOUGH. The worker started at 18:50:43 with a CLEAN
//     prologue and the artifact was replaced hours later, so a prologue-only
//     check passed and then said nothing for the rest of the process's life.
//     The round check is therefore driven TWICE over one check value: a stale
//     verdict prints once, an IDENTICAL one stays silent, and a NEW stale state
//     prints again — asserted on the exact sentences, not on a boolean.
//   - THE SENTENCES NAME BOTH SIDES. Running revision vs artifact revision, and
//     the kernel's " (deleted)" marker, because the operator's first question is
//     "stale relative to WHAT?" and the second is "is this even the same file?".
//   - THE LOOP DOES NOT EXIT. The worker face's ruling (image_selfcheck.go): it
//     also carries housekeeping a stale image performs correctly, so the check
//     REPORTS. The test asserts the warning's own words say so, which is the
//     half a reader of a captured log actually needs.
//   - A `--status` ROW CARRIES THE IMAGE. The human row and the JSON field both
//     render which image the pid runs, so "the dispatcher is an old binary" is
//     visible at dispatch time instead of from `go version -m /proc/<pid>/exe`.
//
// WHAT IT DOES NOT DO (honest): it never reads a live unrelated process and
// never replaces the running test binary's own file. The stale states arrive
// through the two seams the production code already has — an injected probe
// function and an injected procfs root (procname.EnvProcRoot) — which is the
// only way to reach these states without manufacturing the very incident.

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// workerImageTestRunningRev / workerImageTestDiskRev are the two revisions of
	// the measured incident: the worker started on 84beb309 while the artifact on
	// disk had been rebuilt to ee873ec6.
	workerImageTestRunningRev = "84beb3091e0e0d3e5b6b4e3d1cb2a6a8b3c6d5e4"
	workerImageTestDiskRev    = "ee873ec616e5d2b0a1c7f8e9d4c3b2a1908f7e6d"

	// workerImageTestProgram is the artifact path the incident's /proc/<pid>/exe
	// resolved to.
	workerImageTestProgram = "/home/user/work/gopi/bin/gopi"

	// workerImageTestPID is the worker pid from the incident.
	workerImageTestPID = 1038465

	// workerImageTestPid2 is a second pid, used to prove two different pids are
	// rendered as two different facts (a per-row read, never one scan-level
	// verdict reused for every row).
	workerImageTestPid2 = 1038466

	// workerImageTestDeletedMarker is the KERNEL's marker. The production
	// spelling is procstat.DeletedSuffix / procDeletedSuffix; this is the
	// fixture's copy of a kernel constant, not a second definition of ours.
	workerImageTestDeletedMarker = " (deleted)"
)

// workerImageStaleCheck builds the stale verdict the incident produced, with
// both revisions filled in — the value the injected probe returns.
func workerImageStaleCheck(runningRev, diskRev string) tools.SelfImageCheck {
	return tools.SelfImageCheck{
		Probed: true, SelfPath: workerImageTestProgram + workerImageTestDeletedMarker,
		Replaced: true, DiskPath: workerImageTestProgram,
		SelfRevision: runningRev, DiskRevision: diskRev, DiskStamped: true,
		Stale:  true,
		Reason: "the file at " + workerImageTestProgram + " was replaced while this process kept running",
	}
}

// TestWorkerImageRoundCheckSpeaksOnceAndNamesBothRevisions is the epic's core
// assertion: over a sequence of rounds, the check prints on the transition into
// a stale state, stays silent while it is unchanged, and speaks again only when
// the state actually changes.
func TestWorkerImageRoundCheckSpeaksOnceAndNamesBothRevisions(t *testing.T) {
	var out bytes.Buffer
	check := &workerImageRoundCheck{
		probe: func() tools.SelfImageCheck {
			return workerImageStaleCheck(workerImageTestRunningRev, workerImageTestDiskRev)
		},
		out: &out,
	}

	check.Report(workerProgTopLevel)
	first := out.String()
	if first == "" {
		t.Fatal("a STALE image produced no line at the round boundary — this is the exact silence the epic is about")
	}
	for _, want := range []string{
		"STALE image",
		workerImageTestRunningRev,
		workerImageTestDiskRev,
		workerImageTestDeletedMarker,
		"keeps ticking on purpose",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("the warning does not carry %q:\n%s", want, first)
		}
	}

	// Round 2..3, same state: silence. A heartbeat per round is what a real alarm
	// gets lost in.
	check.Report(workerProgTopLevel)
	check.Report(workerProgTopLevel)
	if got := out.String(); got != first {
		t.Fatalf("an UNCHANGED stale state spoke again (a per-round heartbeat):\nfirst=%s\nnow=%s", first, got)
	}

	// Round 4, a NEW state (the artifact rebuilt again): one more line, exactly.
	check.probe = func() tools.SelfImageCheck {
		return workerImageStaleCheck(workerImageTestRunningRev, "0f0e0d0c0b0a09080706050403020100ffeeddcc")
	}
	check.Report(workerProgTopLevel)
	if strings.Count(out.String(), "STALE image") != 2 {
		t.Fatalf("a changed stale state did not add exactly one line:\n%s", out.String())
	}

	// Round 5, the image is current again (a restart already happened): silence.
	check.probe = func() tools.SelfImageCheck {
		return tools.SelfImageCheck{Probed: true, SelfRevision: workerImageTestRunningRev, DiskRevision: workerImageTestRunningRev}
	}
	check.Report(workerProgTopLevel)
	if strings.Count(out.String(), "STALE image") != 2 {
		t.Fatalf("a CURRENT image spoke:\n%s", out.String())
	}
}

// TestWorkerImageRoundCheckIsSilentOnACleanImage is the anti-noise half: a
// healthy process must log nothing at all, so the presence of the warning is
// itself the signal.
func TestWorkerImageRoundCheckIsSilentOnACleanImage(t *testing.T) {
	var out bytes.Buffer
	check := &workerImageRoundCheck{
		probe: func() tools.SelfImageCheck {
			return tools.SelfImageCheck{Probed: true, SelfRevision: workerImageTestRunningRev, DiskRevision: workerImageTestRunningRev}
		},
		out: &out,
	}
	for i := 0; i < 5; i++ {
		check.Report(workerProgTopLevel)
	}
	if out.Len() != 0 {
		t.Fatalf("a current image produced %d byte(s) of output:\n%s", out.Len(), out.String())
	}
}

// TestWorkerStatusRowCarriesTheRunningImage proves the `--status` half over a
// FABRICATED procfs root: the row for a pid whose image was replaced must name
// the replaced state, and the row for a pid whose image is current must say so —
// per-row, because two workers can be two different binaries.
func TestWorkerStatusRowCarriesTheRunningImage(t *testing.T) {
	root := workerImageFakeProcRoot(t, map[int]string{
		workerImageTestPID:  workerImageTestProgram + workerImageTestDeletedMarker,
		workerImageTestPid2: workerImageTestProgram,
	})
	t.Setenv(procname.EnvProcRoot, root)

	staleRow := workerImageRowNote(workerImageTestPID)
	for _, want := range []string{tools.PeerImageStateReplaced, workerImageTestDeletedMarker, workerImageTestProgram} {
		if !strings.Contains(staleRow, want) {
			t.Errorf("the replaced worker's row does not carry %q: %q", want, staleRow)
		}
	}
	currentRow := workerImageRowNote(workerImageTestPid2)
	if !strings.Contains(currentRow, tools.PeerImageStateCurrent) {
		t.Errorf("a current image's row does not say so: %q", currentRow)
	}
	if currentRow == staleRow {
		t.Fatalf("two different images produced the same row: %q", currentRow)
	}

	// THE ROW ITSELF, not only the clause: the pid, the state word and the task
	// field keep their places, so an existing reader's grep still works.
	w := workerStatusWorker{PID: workerImageTestPID, Record: workerStateRecord{PID: workerImageTestPID}}
	row := w.Report()
	if !strings.Contains(row, strconv.Itoa(workerImageTestPID)) || !strings.Contains(row, tools.PeerImageStateReplaced) {
		t.Fatalf("the rendered worker row loses the pid or the image: %q", row)
	}
}

// TestWorkerStatusJSONCarriesTheImage is the machine half: the JSON document's
// worker entry names the image state and both revisions, and it does so for the
// foreground shape too (the shape an operator most often runs).
func TestWorkerStatusJSONCarriesTheImage(t *testing.T) {
	root := workerImageFakeProcRoot(t, map[int]string{
		workerImageTestPID: workerImageTestProgram + workerImageTestDeletedMarker,
	})
	t.Setenv(procname.EnvProcRoot, root)

	scan := workerStatusScan{
		Workers:    []workerStatusWorker{{PID: workerImageTestPID, Record: workerStateRecord{PID: workerImageTestPID}}},
		Foreground: []int{workerImageTestPID},
	}
	list := workerStatusJSONWorkers(scan)
	if len(list) != 2 {
		t.Fatalf("expected one record row and one foreground row, got %d", len(list))
	}
	for _, w := range list {
		if w.Image != tools.PeerImageStateReplaced {
			t.Errorf("row pid %d: image = %q, want %q", w.PID, w.Image, tools.PeerImageStateReplaced)
		}
	}
}

// workerImageFakeProcRoot builds a procfs root whose <root>/<pid>/exe links
// resolve to the given targets — the honest way to reach the replaced arm
// without replacing a real binary (and without reading an unrelated process).
func workerImageFakeProcRoot(t *testing.T, links map[int]string) string {
	t.Helper()
	root := t.TempDir()
	for pid, target := range links {
		dir := filepath.Join(root, strconv.Itoa(pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.Symlink(target, filepath.Join(dir, procname.ExeName)); err != nil {
			t.Fatalf("symlink %s: %v", target, err)
		}
	}
	return root
}

// TestWorkerRoundCallsTheImageCheck is the GUARD that makes the round check
// more than a function nobody calls — the exact shape this epic exists to end
// (a fix that cannot reach the running process, and nothing saying so). It
// scans the claim loop's OWN source for the call, so deleting the call from the
// round fails HERE instead of silently reverting the behaviour with every other
// test still green.
//
// It reads the source rather than executing the loop because running a round
// means claiming a real row (Rule 6), and it pins the call by its receiver so a
// reworded body does not satisfy it.
func TestWorkerRoundCallsTheImageCheck(t *testing.T) {
	src, err := os.ReadFile(workerRoundFile)
	if err != nil {
		t.Fatalf("read %s: %v — the scan has no subject, so it cannot pass", workerRoundFile, err)
	}
	text := string(src)
	if !strings.Contains(text, workerImageRoundCall) {
		t.Fatalf(`%s no longer calls %q at its round boundary.

The worker's prologue check is NOT sufficient (row 01a18301): the measured process
started with a CLEAN prologue and the artifact was replaced hours later, so the
check must run again every round. Without this call a stale worker keeps printing
delivery verdicts from code that predates the fixes on disk, and nothing says so.`,
			workerRoundFile, workerImageRoundCall)
	}
	// The DECLARATION must sit OUTSIDE the loop: a check constructed per round
	// would forget what it already reported and print the warning every round —
	// the heartbeat that gets a real alarm lost.
	decl := strings.Index(text, workerImageRoundDecl)
	loop := strings.Index(text, workerRoundLoopHead)
	if decl < 0 || loop < 0 || decl > loop {
		t.Fatalf("%s: the round check must be declared before the round loop (%q before %q), so its once-per-change memory survives rounds (decl at %d, loop at %d)",
			workerRoundFile, workerImageRoundDecl, workerRoundLoopHead, decl, loop)
	}
}

// TestWorkerImageRoundCheckDoesNotClaimAStrandedFixWhenRevisionsMatch is the
// EPIC's defect, asserted on the exact sentence the worker loop prints.
//
// MEASURED (the row this test comes from): `gopi worker --status` printed
// "image replaced under the process … running 83b145ee6…, artifact at that path
// 83b145ee6…" — the SAME revision on both sides — and the round note said "the
// fix already on disk does NOT reach this process until it is restarted". The
// three fixes it implied were stranded (20c15007c / 45eb68ece / 116de3444) are
// ancestors of BOTH revisions, so a restart gains nothing: the sentence asked
// the operator to restart a process to get code it already runs.
func TestWorkerImageRoundCheckDoesNotClaimAStrandedFixWhenRevisionsMatch(t *testing.T) {
	var out bytes.Buffer
	check := &workerImageRoundCheck{
		probe: func() tools.SelfImageCheck {
			return workerImageStaleCheck(workerImageTestRunningRev, workerImageTestRunningRev)
		},
		out: &out,
	}
	check.Report(workerProgTopLevel)
	line := out.String()
	if line == "" {
		t.Fatal("a rebuilt image produced no line at all — the kernel's marker is real evidence and must still be reported")
	}
	for _, want := range []string{
		"REBUILT under it",
		workerImageTestRunningRev,
		workerImageTestDeletedMarker,
		"keeps ticking on purpose",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the rebuilt-image warning does not carry %q:\n%s", want, line)
		}
	}
	for _, forbidden := range []string{"does NOT reach", "is running a STALE image"} {
		if strings.Contains(line, forbidden) {
			t.Errorf("the rebuilt-image warning still carries %q — that is the false claim the epic is about:\n%s", forbidden, line)
		}
	}

	// And the machine word a JSON consumer switches on is the distinguishable one.
	if got, want := workerImageStaleCheck(workerImageTestRunningRev, workerImageTestRunningRev).AsPeerImage().StateWord(), tools.PeerImageStateRebuiltSameRevision; got != want {
		t.Errorf("state = %q, want %q", got, want)
	}
}
