package tools

// image_peer_test.go — the peer-image probe (image_peer.go), driven by INJECTED
// revisions and a FABRICATED procfs root.
//
// WHY NO TEST HERE READS A LIVE UNRELATED PROCESS: the question this file's
// subject answers is about a process nobody controls, so a test that "found a
// worker with --status" would be asserting on whatever happened to be running
// that hour — a flaky test whose green says nothing and whose red blames the
// environment. The two arms are reached the two honest ways instead:
//
//   • arm ① (the kernel's " (deleted)" marker) — a fabricated procfs root whose
//     <root>/<pid>/exe link points at an unlinked path;
//   • arm ② (the two stamps disagree) — peerImageVerdict, a PURE function of
//     the strings, so the exact incident (84beb309 running, ee873ec6 on disk)
//     is replayed as a value instead of reconstructed from a machine.
//
// The negative halves matter as much as the positive ones: a path string that
// merely CONTAINS the marker must not read as replaced, and identical stamps
// must not read as stale — otherwise the alarm fires on healthy machines and is
// deleted by the first person it wakes up.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/procname"
)

const (
	// peerTestRunningRev / peerTestDiskRev are the two revisions of the measured
	// incident (row 01a18301): the worker started on 84beb309 and the artifact
	// had been rebuilt to ee873ec6 by the time it printed a delivery verdict.
	peerTestRunningRev = "84beb3091e0e0d3e5b6b4e3d1cb2a6a8b3c6d5e4"
	peerTestDiskRev    = "ee873ec616e5d2b0a1c7f8e9d4c3b2a1908f7e6d"

	// peerTestProgram is the artifact path both images live at — the path
	// /proc/<pid>/exe resolves to, spelled once so the assertions below read as
	// the incident rather than as arbitrary strings.
	peerTestProgram = "/home/nick/work/gopi/bin/gopi"

	// peerTestPID is the worker pid from the incident.
	peerTestPID = 1038465

	// peerTestDeletedMarker is the KERNEL's marker, spelled in the fixture so it
	// reads as what it imitates. The production spelling is procDeletedSuffix
	// and is what the code reads; " (deleted)" is not our value (Rule 13 governs
	// values we define, and this one is the kernel's).
	peerTestDeletedMarker = " (deleted)"
)

// TestPeerImageVerdictNamesBothRevisionsOnTheMeasuredIncident is the RED-shaped
// assertion for the epic: the state that produced a false "delivery did not
// land" must be NAMED, with the running revision, the disk revision, and the
// path — in that identity, not merely "something is wrong".
func TestPeerImageVerdictNamesBothRevisionsOnTheMeasuredIncident(t *testing.T) {
	stale, reason := peerImageVerdict(peerTestPID, peerTestProgram+peerTestDeletedMarker,
		peerTestRunningRev, peerTestDiskRev, true)
	if !stale {
		t.Fatalf("the measured incident read as FRESH — a worker replaced under it would go on printing delivery verdicts nobody can trust:\nreason=%q", reason)
	}
	for _, want := range []string{
		strconv.Itoa(peerTestPID),
		peerTestProgram,
		strings.TrimSuffix(peerTestProgram+peerTestDeletedMarker, peerTestDeletedMarker),
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("the verdict does not name %q:\n%s", want, reason)
		}
	}
	// The marker itself is the proof rather than a diagnosis, so it is quoted.
	if !strings.Contains(reason, peerTestDeletedMarker) {
		t.Errorf("the verdict does not quote the kernel's own marker %q:\n%s", peerTestDeletedMarker, reason)
	}
}

// TestPeerImageVerdictStampArmFiresOnDisagreement pins arm ② — the in-place
// rebuild arm — on the strings alone.
func TestPeerImageVerdictStampArmFiresOnDisagreement(t *testing.T) {
	stale, reason := peerImageVerdict(peerTestPID, peerTestProgram, peerTestRunningRev, peerTestDiskRev, true)
	if !stale {
		t.Fatal("two disagreeing stamps read as fresh — arm ② would be dead code")
	}
	if !strings.Contains(reason, peerTestRunningRev) || !strings.Contains(reason, peerTestDiskRev) {
		t.Fatalf("the verdict must name BOTH revisions, got:\n%s", reason)
	}
}

// TestPeerImageVerdictNegativeHalves is the anti-alarm half: the three states
// that are NOT staleness must stay silent, or the warning becomes noise.
func TestPeerImageVerdictNegativeHalves(t *testing.T) {
	cases := []struct {
		name        string
		target      string
		imageRev    string
		diskRev     string
		diskStamped bool
	}{
		{"identical stamps", peerTestProgram, peerTestRunningRev, peerTestRunningRev, true},
		{"unreadable artifact leaves arm 2 unmeasurable", peerTestProgram, peerTestRunningRev, "", false},
		{"no running stamp", peerTestProgram, "", peerTestDiskRev, true},
		// A path that merely CONTAINS the marker is not a replaced image: the
		// kernel's marker is a SUFFIX on the link target, and a path named
		// "gopi (deleted)-build" is a different file, not an unlinked one.
		{"marker inside a name is not the marker", "/tmp/gopi" + peerTestDeletedMarker + "-build", peerTestRunningRev, peerTestRunningRev, true},
	}
	for _, tc := range cases {
		stale, reason := peerImageVerdict(peerTestPID, tc.target, tc.imageRev, tc.diskRev, tc.diskStamped)
		if stale {
			t.Errorf("%s: read as stale — the alarm fires on a healthy machine:\n%s", tc.name, reason)
		}
	}
}

// TestPeerImageProbeReplacedArmOverFabricatedProcfs reaches arm ① through the
// production path, with a procfs root built to answer: <root>/<pid>/exe → an
// unlinked target. No live process is touched.
func TestPeerImageProbeReplacedArmOverFabricatedProcfs(t *testing.T) {
	root := t.TempDir()
	pidDir := filepath.Join(root, strconv.Itoa(peerTestPID))
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pidDir, err)
	}
	gone := filepath.Join(t.TempDir(), "replaced-artifact")
	if err := os.WriteFile(gone, []byte("stale bytes\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", gone, err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatalf("unlink %s: %v", gone, err)
	}
	if err := os.Symlink(gone+peerTestDeletedMarker, filepath.Join(pidDir, procname.ExeName)); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	check := PeerImageProbeAt(root, peerTestPID)
	if !check.Probed {
		t.Fatalf("the probe did not run: %s", check.Unmeasurable)
	}
	if !check.Replaced || !check.Stale {
		t.Fatalf("an unlinked image read as fresh: replaced=%v stale=%v (%q)", check.Replaced, check.Stale, check.ExePath)
	}
	if check.DiskPath != gone {
		t.Fatalf("DiskPath = %q, want the marker stripped: %q", check.DiskPath, gone)
	}
	note := check.RowNote()
	for _, want := range []string{peerImageReplacedWord, peerTestDeletedMarker, selfImageNoRevision} {
		if !strings.Contains(note, want) {
			t.Errorf("the row clause does not carry %q:\n%s", want, note)
		}
	}
}

// TestPeerImageProbeUnmeasurableIsStatedNotSilent is the Rule 15 half: a probe
// that could not run must SAY so, and must never render as "current".
func TestPeerImageProbeUnmeasurableIsStatedNotSilent(t *testing.T) {
	for _, root := range []string{"", t.TempDir()} {
		check := PeerImageProbeAt(root, peerTestPID)
		if check.Probed {
			t.Fatalf("root %q: a probe with no link read as PROBED", root)
		}
		if check.Stale {
			t.Fatalf("root %q: an unmeasurable probe read as stale", root)
		}
		if check.StateWord() != PeerImageStateUnmeasurable {
			t.Errorf("root %q: state = %q, want %q", root, check.StateWord(), PeerImageStateUnmeasurable)
		}
		if note := check.RowNote(); !strings.Contains(note, peerImageUnmeasurablePrefix) {
			t.Errorf("root %q: the row clause is not the stated-unmeasurable form: %q", root, note)
		}
	}
}

// TestPeerImageNoteNamesBothSidesAndTheRemedy pins the sentence the worker loop
// prints at a round boundary: the two revisions, the two paths, the restart, and
// the fact that the loop deliberately keeps ticking (so a reader cannot mistake
// it for the merge loop's refusal).
func TestPeerImageNoteNamesBothSidesAndTheRemedy(t *testing.T) {
	stale, reason := peerImageVerdict(peerTestPID, peerTestProgram+peerTestDeletedMarker, peerTestRunningRev, peerTestDiskRev, true)
	check := PeerImageCheck{
		Probed: true, ExePath: peerTestProgram + peerTestDeletedMarker, Replaced: true,
		DiskPath: peerTestProgram, ImageRevision: peerTestRunningRev,
		DiskRevision: peerTestDiskRev, DiskStamped: true, Stale: stale, Reason: reason,
	}
	note := check.PeerImageNote("gopi worker", peerTestPID)
	for _, want := range []string{
		"STALE image",
		peerTestRunningRev,
		peerTestDiskRev,
		peerTestDeletedMarker,
		"until it is restarted",
		"keeps ticking on purpose",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("the worker warning does not carry %q:\n%s", want, note)
		}
	}
}

// TestPeerImageVerdictSameRevisionRebuildIsNotAFixThatDidNotArrive pins the
// THIRD state, and it exists because the first two were printing the same
// sentence for it.
//
// THE MEASURED DEFECT. A `make build` of an unchanged (or dirty) tree rewrites
// the artifact at the same path, so the kernel marks every running process's
// image " (deleted)" — the marker is REAL. But when both stamps are the same
// revision, the code the process runs IS the code on disk, and the arm ①
// sentence ("the code it executes is NOT the code at that path") plus the note's
// "the fix already on disk does NOT reach this process until it is restarted"
// are both FALSE: the three merged fixes on disk are ancestors of the running
// revision, so a restart gains nothing. What must survive is the EVIDENCE (the
// marker, named), not the inference drawn from it.
func TestPeerImageVerdictSameRevisionRebuildIsNotAFixThatDidNotArrive(t *testing.T) {
	stale, reason := peerImageVerdict(peerTestPID, peerTestProgram+peerTestDeletedMarker,
		peerTestRunningRev, peerTestRunningRev, true)
	check := PeerImageCheck{
		Probed: true, ExePath: peerTestProgram + peerTestDeletedMarker, Replaced: true,
		DiskPath: peerTestProgram, ImageRevision: peerTestRunningRev,
		DiskRevision: peerTestRunningRev, DiskStamped: true, Stale: stale, Reason: reason,
	}

	// ① The kernel's marker is still REPORTED, with both sides named: dropping
	// it would hide a real fact about the process (constraint 2 of the epic).
	if !stale {
		t.Fatalf("a replaced image read as fresh — the kernel's marker is real even when the revision did not change:\nreason=%q", reason)
	}
	for _, want := range []string{
		strconv.Itoa(peerTestPID),
		peerTestProgram,
		peerTestDeletedMarker,
		peerTestRunningRev,
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("the verdict does not name %q:\n%s", want, reason)
		}
	}

	// ② ... but it no longer asserts that the disk code differs from the running
	// code. THAT is the false sentence the epic is about.
	if strings.Contains(reason, "is NOT the code at that path") {
		t.Errorf("the same-revision verdict still claims the running code is not the disk code:\n%s", reason)
	}
	if strings.Contains(reason, peerImageReplacedWord) {
		t.Errorf("the same-revision verdict still renders arm ①'s word %q:\n%s", peerImageReplacedWord, reason)
	}

	// ③ The machine word is DISTINGUISHABLE — a consumer must be able to tell
	// "rebuilt, same code" from "older code on disk" without reading prose.
	if got, want := check.StateWord(), PeerImageStateRebuiltSameRevision; got != want {
		t.Errorf("state = %q, want %q (the rebuild-same-revision state must be its own token)", got, want)
	}
	if check.StateWord() == PeerImageStateReplaced {
		t.Error("the same-revision rebuild published the plain replaced token — the two states are indistinguishable to a machine reader")
	}

	// ④ Both rendered forms carry the marker AND the same-revision clause, and
	// the note no longer claims a disk fix failed to arrive.
	row := check.RowNote()
	for _, want := range []string{peerTestDeletedMarker, peerTestRunningRev, peerImageSameRevisionClause} {
		if !strings.Contains(row, want) {
			t.Errorf("the row clause does not carry %q:\n%s", want, row)
		}
	}
	note := check.PeerImageNote("gopi worker", peerTestPID)
	for _, want := range []string{peerTestDeletedMarker, peerImageSameRevisionClause, "keeps ticking on purpose"} {
		if !strings.Contains(note, want) {
			t.Errorf("the worker note does not carry %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "does NOT reach") {
		t.Errorf("the worker note still claims the disk fix does not reach this process:\n%s", note)
	}
	// Stated, not only asserted: the two sentences a reader would actually see,
	// so this run's report can quote the AFTER state instead of describing it.
	t.Logf("same-revision ROW : %s", row)
	t.Logf("same-revision NOTE: %s", note)
}

// TestPeerImageVerdictReplacedDifferentRevisionStillAlarms is the half that must
// NOT be softened: when the two stamps really disagree, the running process is
// older than the artifact and the alarm is true, evidence and inference both.
func TestPeerImageVerdictReplacedDifferentRevisionStillAlarms(t *testing.T) {
	stale, reason := peerImageVerdict(peerTestPID, peerTestProgram+peerTestDeletedMarker,
		peerTestRunningRev, peerTestDiskRev, true)
	check := PeerImageCheck{
		Probed: true, ExePath: peerTestProgram + peerTestDeletedMarker, Replaced: true,
		DiskPath: peerTestProgram, ImageRevision: peerTestRunningRev,
		DiskRevision: peerTestDiskRev, DiskStamped: true, Stale: stale, Reason: reason,
	}
	if !stale {
		t.Fatal("replaced + disagreeing stamps read as fresh — the real alarm would be gone")
	}
	for _, want := range []string{peerTestRunningRev, peerTestDiskRev, peerTestDeletedMarker} {
		if !strings.Contains(reason, want) {
			t.Errorf("the alarm does not name %q:\n%s", want, reason)
		}
	}
	if !strings.Contains(reason, "is NOT the code at that path") {
		t.Errorf("the alarm lost the claim that IS true here (the running code is not the disk code):\n%s", reason)
	}
	if got, want := check.StateWord(), PeerImageStateReplaced; got != want {
		t.Errorf("state = %q, want %q", got, want)
	}
	if got, want := check.StaleWord(), peerImageReplacedWord; got != want {
		t.Errorf("word = %q, want %q", got, want)
	}
	// The remedy the note names is right for THIS state, so it stays.
	if note := check.PeerImageNote("gopi worker", peerTestPID); !strings.Contains(note, "does NOT reach") {
		t.Errorf("the different-revision note lost its (true) remedy clause:\n%s", note)
	}
}
