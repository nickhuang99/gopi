package main

// The DRAIN half, end to end (epic 2026-09-16, branch fix/merge-rebuild-loop):
// a repo whose master advanced while its binary could not follow (the durable
// mark) is brought current by a close loop whose repo list does NOT name it —
// the shape of a fresh pass that only finds the mark.
//
// The MEASURED reason this matters (see internal/tools/rebuild_pending.go): a
// killed pass runs no defer, so a close loop that only exists at the pass END is
// exactly the step a killed pass never reaches. The 08:50-era daemon left three
// state files (2436009, 2456472, 2470012) with passes=0 — passes killed inside
// their own sweep — and bin/gopi stayed stale for ~4h.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

// writeFileRaw writes a fixture file the production primitives deliberately
// refuse to produce (see the unattributable-mark test).
func writeFileRaw(dir, name, body string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
}

// TestPassCloseLoopDrainsTheDurableMark: a stale artifact plus a pending mark is
// rebuilt by a close loop whose repo list does NOT mention the repo. The binary
// end state is what a mark is FOR: the artifact follows the tip the mark named,
// without any hand build and without the pass having to land anything itself.
func TestPassCloseLoopDrainsTheDurableMark(t *testing.T) {
	repo := rebuildFixtureRepo(t, tipBinBuild("__unused__"))
	writeReposEntry(t, repo) // declare the REAL build (writes the tip-reporting bin)
	writeStaleBin(t, repo)   // the binary as a successor finds it: behind the tip

	tools.MarkRebuildPending(repo, "gate landed a branch while the rebuild waited for the judge")

	// NO repo named: the drain is the only thing that can rebuild here — which
	// is precisely what a process killed before its close loop leaves behind.
	out := capturePassCloseLoop(t, nil)
	if !strings.Contains(out, "built in") {
		t.Fatalf("the close loop did not drain the durable mark: got %q — a killed landing's rebuild is lost", out)
	}
	// The obligation is discharged: the binary follows the tip the mark named.
	if got := tools.RebuildPendingRepos(); len(got) != 0 {
		t.Fatalf("the mark survived the drain (%v) — every later pass would rebuild this repo forever", got)
	}
	// IDEMPOTENCE: the next pass finds nothing to do (no rebuild), which is what
	// makes a per-pass drain affordable on a clean host.
	out2 := capturePassCloseLoop(t, nil)
	if strings.Contains(out2, "built in") {
		t.Fatalf("a second pass rebuilt an already-current repo: %q — the drain must be idempotent", out2)
	}
}

// TestPassCloseLoopDrainReportsAnUnattributableMark: a mark that names no repo
// must not be silently swallowed OR acted on — the operator is told, and the
// pass carries on. Nothing here may fail the pass: the close loop's job is to
// rebuild what it can.
func TestPassCloseLoopDrainReportsAnUnattributableMark(t *testing.T) {
	repo := rebuildFixtureRepo(t, tipBinBuild("__unused__"))
	writeReposEntry(t, repo)
	writeStaleBin(t, repo)
	// A mark whose body names no repo (a partial write, or a stray file an
	// operator copied in). Written directly: the primitive refuses to produce
	// one, which is exactly why the READ side must still handle it (Rule 15b).
	dir, err := tools.RebuildPendingDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileRaw(dir, tools.RebuildPendingPrefix+"0123456789abcdef", "\n"); err != nil {
		t.Fatal(err)
	}
	out := capturePassCloseLoop(t, nil)
	if !strings.Contains(out, "carries no repo") {
		t.Fatalf("an unattributable mark must be REPORTED, got %q (Rule 15: never a silent skip)", out)
	}
}

// TestPassOnceDrainsMarksAtPassStart is the ORDERING red-proof: the drain must be
// an act of the pass's START, so a pass killed mid-sweep (no defer runs) has
// already healed a stale artifact. Against the pre-fix source there is no drain
// at all, and the pass-end hook cannot help a killed pass.
func TestPassOnceDrainsMarksAtPassStart(t *testing.T) {
	src, err := os.ReadFile("cli_tasks.go")
	if err != nil {
		t.Fatal(err)
	}
	body := braceFuncBody(t, string(src), "func mergerPassOnce(")
	drain := passStartDrain(t, string(src), body)
	if drain < 0 {
		t.Fatalf("mergerPassOnce does not drain the durable marks at pass START — a pass killed mid-sweep runs no defer, so the stale artifact survives it (measured: three state files with passes=0, bin/gopi stale ~4h)")
	}
	// Anchors that make the ordering mean something: the drain must precede the
	// repo resolve and the sweep, i.e. it runs before any work can hang.
	resolve := strings.Index(body, "config.ResolveMergerRepos()")
	if resolve >= 0 && drain > resolve {
		t.Fatalf("the drain sits AFTER the repo resolve (offset %d vs %d) — it must be the pass's FIRST act, before anything that can hang", drain, resolve)
	}
	sweep := strings.Index(body, "mergerSweepRepo(repo)")
	if sweep >= 0 && drain > sweep {
		t.Fatalf("the drain sits after the sweep — a pass killed inside the sweep would leave the artifact stale, which is the measured defect")
	}
}
