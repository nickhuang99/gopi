package main

// cli_scratch_gc_trees_explain_test.go — the trees arm's `--explain` face,
// driven through the REAL verb against a SANDBOX.
//
// THE DEFECT THESE TESTS PIN (measured by the filing run, 2026-10-14):
// `gopi tool scratch-gc trees --explain` printed NOTHING for every entry it
// protected. The arm ignored the flag entirely and computed only the candidates a
// sweep would REMOVE, so "this entry is protected, and here is why" and "this
// entry was never looked at" rendered as the same thing — empty output — which is
// the one question `--explain` exists to answer.
//
// THE FIXTURE IS THE ONE THE BRIEFING MEASURED: a sessions-tree CONTAINER holding
// an aged per-run raw log (a candidate) and a session record (NOT the tree's
// shape), which is what pins the container. `--explain` must now name that keep,
// and the per-tree SUMMARY line must carry the counts so an empty plan can never
// read as proof of protection.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

// heldSessionsContainer plants one container in the sandbox's sessions tree with
// an AGED per-run raw log (the tree's own shape, a real candidate), a session
// record (not the tree's shape — the entry that PINS the container), and, unless
// hold is "", a live process standing INSIDE the container.
//
// It answers with the container's path and the paths of the two children, and it
// RE-STATS the ages after the holder is started: starting a process whose cwd is
// inside a directory does not by itself change an mtime, but the fixture's own
// writes do (measured, `reAge` in the package's tests), so the ages are set last
// and asserted to be the reason the entry is kept — never the clock.
func heldSessionsContainer(t *testing.T, sessDir, name string, hold bool) (container, rawLog, record string) {
	t.Helper()
	// A DIRECT child of the sessions root, and a name of its own: the seeded
	// fixture already owns `sessDir/<its own container id>`, and reusing that
	// path would hand this fixture the seed's `session.jsonl` — which pins the
	// container for a reason the test did not plant (measured: that collision
	// made BOTH the keep and the flip cases pass for the wrong reason).
	container = filepath.Join(sessDir, name)
	if err := os.MkdirAll(container, scratch.DirPerm); err != nil {
		t.Fatal(err)
	}
	rawLog = filepath.Join(container, name+scratch.SessionRawLogFileSuffix)
	record = filepath.Join(container, "session.jsonl")
	for _, p := range []string{rawLog, record} {
		if err := os.WriteFile(p, []byte("fixture\n"), scratch.FilePerm); err != nil {
			t.Fatal(err)
		}
	}
	// The log is aged past the tree's own window; the RECORD is aged too, so
	// nothing about this container can be explained by freshness.
	for _, p := range []string{rawLog, record} {
		backdate(t, p, scratch.SessionRawLogStaleAge+2*time.Hour)
	}
	backdate(t, container, scratch.SessionRawLogDirStaleAge+2*time.Hour)

	if hold {
		holder := exec.Command("sleep", "120")
		// cwd IS the subject here (the holder is what the tree veto reads), so Dir
		// stays; Pdeathsig keeps it from outliving a killed test binary.
		holder.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
		holder.Dir = container
		if err := holder.Start(); err != nil {
			t.Fatalf("start the holder: %v", err)
		}
		t.Cleanup(func() {
			_ = holder.Process.Kill() //stylegate:ignore the holder exists only to hold the directory; its exit status is not asserted
			_ = holder.Wait()         //stylegate:ignore reaping the test's own child
		})
		// The AGES are set LAST so that only the holder rule can explain the
		// keep: a fixture that passed because it looked young would pin nothing.
		for _, p := range []string{rawLog, record} {
			backdate(t, p, scratch.SessionRawLogStaleAge+2*time.Hour)
		}
		backdate(t, container, scratch.SessionRawLogDirStaleAge+2*time.Hour)
	}
	return container, rawLog, record
}

// verdictLine returns the ONE `scratch-verdict` line for path, or "" — the same
// label the default arm's explain face prints, so an operator's filter for
// `^scratch-verdict ` finds every arm's judgement.
func verdictLine(out, path string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, scratchGCVerdictLabel+" "+path+" ") {
			return line
		}
	}
	return ""
}

// TestScratchGCCLITrees_ExplainNamesTheHeldContainerKeep is the acceptance's
// central case: the arm is run for REAL with `--explain --dry-run`, and the
// container the briefing measured gets a verdict line that names WHY it is kept —
// where the pre-fix binary printed nothing for it.
func TestScratchGCCLITrees_ExplainNamesTheHeldContainerKeep(t *testing.T) {
	f := seedDeclaredTreesFixture(t)
	// TWO containers, because the two keeps must not be conflated: a HELD one
	// (nothing below it is judged, so its children get no verdict at all) and an
	// UNHELD one pinning on its own session record (whose children ARE judged).
	heldContainer, _, _ := heldSessionsContainer(t, f.sessDir, "held-container", true)
	pinnedContainer, pinnedLog, pinnedRecord := heldSessionsContainer(t, f.sessDir, "pinned-container", false)

	out, stderr, code := runGopiMapped(t,
		[]string{CliVerbScratchGC, scratchGCTreesFlag, scratchGCDryRunFlag, scratchGCExplainFlag,
			scratch.DeclaredTreeFlagSpelling, scratch.TreeNameSessionRaw},
		"", declaredTreesEnv(f.home, f.goTmp), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}

	// ① the HELD container's keep, named. This is the line the pre-fix binary did
	// not print at all.
	if line := verdictLine(out, heldContainer); line == "" {
		t.Errorf("no %s line for the held container %s — the keep printed as silence:\n%s", scratchGCVerdictLabel, heldContainer, out)
	} else if !strings.Contains(line, scratchGCKeepVerdict) {
		t.Errorf("the held container must be reported KEPT, got:\n%s", line)
	} else if !strings.Contains(line, scratch.EntryKeepContainerHeld) {
		t.Errorf("the container's verdict must name the holder veto %q, got:\n%s", scratch.EntryKeepContainerHeld, line)
	}

	// ② the PINNED container and its session record — the entry that pins it, and
	// the keep that before the fix produced no output either.
	if line := verdictLine(out, pinnedRecord); line == "" {
		t.Errorf("no %s line for the session record %s:\n%s", scratchGCVerdictLabel, pinnedRecord, out)
	} else if !strings.Contains(line, scratch.EntryKeepNotOurs) {
		t.Errorf("the session record must be reported kept as not-this-tree's-shape, got:\n%s", line)
	}
	if line := verdictLine(out, pinnedContainer); line == "" {
		t.Errorf("no %s line for the pinned container %s:\n%s", scratchGCVerdictLabel, pinnedContainer, out)
	} else if !strings.Contains(line, scratch.EntryKeepContainerNotDrained) {
		t.Errorf("the pinned container must name %q, got:\n%s", scratch.EntryKeepContainerNotDrained, line)
	}

	// ③ the aged candidate is still reported collectable — the arm did not stop
	// reporting the plan, it gained a second half.
	if line := verdictLine(out, pinnedLog); line == "" {
		t.Errorf("no %s line for the aged candidate %s:\n%s", scratchGCVerdictLabel, pinnedLog, out)
	} else if !strings.Contains(line, scratchGCCollectableVerdict) {
		t.Errorf("the aged candidate must still be reported collectable, got:\n%s", line)
	}

	// ④ the ROOT's own line: the container's container survives by design, and a
	// report that omits it makes it look escaped rather than protected.
	if line := verdictLine(out, f.sessDir); line == "" {
		t.Errorf("no %s line for the declared tree ROOT %s:\n%s", scratchGCVerdictLabel, f.sessDir, out)
	} else if !strings.Contains(line, scratch.EntryKeepDeclaredRoot) {
		t.Errorf("the root's verdict must carry %q, got:\n%s", scratch.EntryKeepDeclaredRoot, line)
	}

	// ⑤ the SUMMARY line, always: an empty plan must never be able to read as
	// proof of protection.
	summary := summaryLine(out, scratch.TreeNameSessionRaw)
	if summary == "" {
		t.Fatalf("the trees arm printed no summary line for %s:\n%s", scratch.TreeNameSessionRaw, out)
	}
	for _, want := range []string{"examined=", "kept=", "collectable=", "not_ours=", "truncated="} {
		if !strings.Contains(summary, want) {
			t.Errorf("the summary line must carry %q, got:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "examined=0") || strings.Contains(summary, "kept=0") {
		t.Errorf("the summary counted nothing on a fixture with judgeable entries:\n%s", summary)
	}
	// ⑥ the reason DISTRIBUTION: which rule did the protecting.
	if !strings.Contains(out, scratchGCKeepReasonsLabel) {
		t.Errorf("the verdict face must print the kept-by-reason distribution:\n%s", out)
	}

	// ⑦ the vetting face deleted nothing.
	for _, kept := range []string{heldContainer, pinnedContainer, pinnedLog, pinnedRecord, f.sessDir} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("a --dry-run --explain pass removed %s: %v", kept, err)
		}
	}
}

// TestScratchGCCLITrees_HolderDeathFlipsTheKeepToWouldRemove is the acceptance's
// second probe, and it is the one that proves the verdict TRACKS THE PREDICATE
// rather than a constant: the SAME binary, the SAME command, the SAME fixture
// shape, and the only difference is whether a live process stands inside the
// container. With the holder dead, the container the report called
// `keep … a live process holds the container` becomes `would-remove`.
//
// THE FIXTURE IS DELIBERATELY MINIMAL — the container holds ONLY the aged
// candidate, with no session record and nothing else kept in it. That makes the
// HOLDER the sole reason the container survives, so its death is the one variable
// under test; a fixture with a record in it would stay kept for a second,
// unrelated reason and the probe would prove nothing.
func TestScratchGCCLITrees_HolderDeathFlipsTheKeepToWouldRemove(t *testing.T) {
	// Arm 1 — a live process inside the container.
	heldFixture := seedDeclaredTreesFixture(t)
	heldContainer, heldLog := flipContainer(t, heldFixture.sessDir, "flip-held", true)
	heldOut, heldErr, code := runGopiMapped(t,
		[]string{CliVerbScratchGC, scratchGCTreesFlag, scratchGCDryRunFlag, scratchGCExplainFlag,
			scratch.DeclaredTreeFlagSpelling, scratch.TreeNameSessionRaw},
		"", declaredTreesEnv(heldFixture.home, heldFixture.goTmp), "")
	if code != 0 {
		t.Fatalf("the held pass: exit=%d stderr=%s", code, heldErr)
	}
	if line := verdictLine(heldOut, heldContainer); line == "" {
		t.Fatalf("no verdict for the held container %s — the keep is still silence:\n%s", heldContainer, heldOut)
	} else if !strings.Contains(line, scratch.EntryKeepContainerHeld) {
		t.Fatalf("the held container must name the holder veto, got:\n%s", line)
	}
	if strings.Contains(heldOut, "would-remove "+heldContainer+" ") {
		t.Errorf("a container a live process stands in was reported in the PLAN:\n%s", heldOut)
	}
	// Nothing below a held container is judged — that is the walk's own rule, and
	// the report must not invent a verdict the sweep never formed.
	if line := verdictLine(heldOut, heldLog); line != "" {
		t.Errorf("an entry inside a held container got a verdict line:\n%s", line)
	}

	// Arm 2 — the SAME fixture shape, no live process. A second fixture (not the
	// first one with its holder killed) so the two runs are independent.
	freeFixture := seedDeclaredTreesFixture(t)
	freeContainer, freeLog := flipContainer(t, freeFixture.sessDir, "flip-free", false)
	freeOut, freeErr, freeCode := runGopiMapped(t,
		[]string{CliVerbScratchGC, scratchGCTreesFlag, scratchGCDryRunFlag, scratchGCExplainFlag,
			scratch.DeclaredTreeFlagSpelling, scratch.TreeNameSessionRaw},
		"", declaredTreesEnv(freeFixture.home, freeFixture.goTmp), "")
	if freeCode != 0 {
		t.Fatalf("the unheld pass: exit=%d stderr=%s", freeCode, freeErr)
	}
	// THE FLIP: the identical entry, now in the plan.
	if !strings.Contains(freeOut, "would-remove "+freeContainer+" ") {
		t.Errorf("the container did NOT flip to would-remove once the holder was gone — the verdict is a constant, not a measurement:\n%s", freeOut)
	}
	if !strings.Contains(freeOut, "would-remove "+freeLog+" ") {
		t.Errorf("the aged candidate inside the container was not reported:\n%s", freeOut)
	}
	if line := verdictLine(freeOut, freeContainer); line == "" {
		t.Errorf("the flipped container has no verdict line:\n%s", freeOut)
	} else if !strings.Contains(line, scratchGCCollectableVerdict) {
		t.Errorf("the flipped container must be reported collectable, got:\n%s", line)
	} else if strings.Contains(line, scratch.EntryKeepContainerHeld) {
		t.Errorf("the flipped container still claims a holder nobody observed:\n%s", line)
	}
}

// flipContainer plants a sessions-tree container holding ONLY one aged per-run raw
// log — every child a candidate — and optionally a live process standing inside
// it. With the holder present the container and its content are kept; with the
// holder gone both are collectable, which is the keep→would-remove flip.
func flipContainer(t *testing.T, sessDir, name string, hold bool) (container, rawLog string) {
	t.Helper()
	// A name of its own (see heldSessionsContainer) and a DIRECT child of the
	// sessions root: the tree walks one level, so a nested container is never
	// judged as a container at all.
	container = filepath.Join(sessDir, name)
	if err := os.MkdirAll(container, scratch.DirPerm); err != nil {
		t.Fatal(err)
	}
	rawLog = filepath.Join(container, name+scratch.SessionRawLogFileSuffix)
	if err := os.WriteFile(rawLog, []byte("fixture\n"), scratch.FilePerm); err != nil {
		t.Fatal(err)
	}
	age := func() {
		backdate(t, rawLog, scratch.SessionRawLogStaleAge+2*time.Hour)
		// The container must clear the LONGER container window as well, or it
		// would be kept by the clock in BOTH arms and the probe would measure
		// nothing.
		backdate(t, container, scratch.SessionRawLogDirStaleAge+2*time.Hour)
	}
	age()
	if !hold {
		return container, rawLog
	}
	holder := exec.Command("sleep", "120")
	// Same shape as the branch above: cwd is the subject, Pdeathsig is the
	// lifetime arm the kernel enforces even when this binary is SIGKILLed.
	holder.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	holder.Dir = container
	if err := holder.Start(); err != nil {
		t.Fatalf("start the holder: %v", err)
	}
	t.Cleanup(func() {
		_ = holder.Process.Kill() //stylegate:ignore the holder exists only to hold the directory; its exit status is not asserted
		_ = holder.Wait()         //stylegate:ignore reaping the test's own child
	})
	age() // set LAST: nothing about this fixture may be explained by youth
	return container, rawLog
}

// summaryLine returns the ONE trees-arm summary line for a tree, or "".
func summaryLine(out, treeName string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "tree-summary tree="+treeName+" ") {
			return line
		}
	}
	return ""
}

// TestScratchGCCLITrees_SummaryPrintsWithoutTheExplainFlag pins the ADDITIVE
// half: the summary is unconditional, so a reader with an empty plan (no
// --explain, nothing collectable) can still see that the arm looked. It also
// pins that the DEFAULT face stays quiet — the verdict lines are the flag's
// business, and turning them on by default would bury the plan on a host whose
// trees hold hundreds of thousands of entries.
func TestScratchGCCLITrees_SummaryPrintsWithoutTheExplainFlag(t *testing.T) {
	f := seedDeclaredTreesFixture(t)
	container, _, _ := heldSessionsContainer(t, f.sessDir, "summary-container", false)

	out, stderr, code := runGopiMapped(t,
		[]string{CliVerbScratchGC, scratchGCTreesFlag, scratchGCDryRunFlag,
			scratch.DeclaredTreeFlagSpelling, scratch.TreeNameSessionRaw},
		"", declaredTreesEnv(f.home, f.goTmp), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if summaryLine(out, scratch.TreeNameSessionRaw) == "" {
		t.Errorf("the summary line must print WITHOUT --explain too:\n%s", out)
	}
	if line := verdictLine(out, container); line != "" {
		t.Errorf("a verdict line was printed without --explain — the flag must bound the noise:\n%s", line)
	}
}
