// declared_trees_verdict_test.go — the trees arm's REPORTING TWIN's contract.
//
// The defect these tests pin (measured 2026-10-14): `gopi tool scratch-gc trees`
// computed only the entries a sweep WOULD REMOVE, so every KEEP was silence —
// "protected, and here is why" and "never a candidate in the first place" printed
// the identical thing (nothing), which is exactly the question `--explain` exists
// to answer.
//
// FOUR REASONS IN ONE FIXTURE, which is the point: a single fixture holding a
// held entry, a young entry, a namespace miss and an aged candidate proves the
// reasons are DISTINGUISHED rather than one constant rendered four times — a
// report that answers the same sentence for every keep would pass a
// one-fixture-per-case test and fail this one.
package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDeclaredTreeVerdicts_NamesEveryKeepReason is the ONE-fixture, four-reason
// case: an aged unheld candidate (collectable), a YOUNG one (kept by the clock),
// a HELD one (kept by the owner's own record — the .holder channel, which the
// permission-free ladder reads without the process table), and a namespace MISS
// (not this tree's shape: counted, never a verdict line).
//
// It also pins the ROOT's own line: a declared tree's container is kept by
// design, and a report that omits it makes the container look escaped rather
// than protected.
func TestDeclaredTreeVerdicts_NamesEveryKeepReason(t *testing.T) {
	home := t.TempDir()
	goTmp := filepath.Join(home, "workspace", "gopi-go-tmp")
	t.Setenv("GOPI_HOME", home)
	t.Setenv(GoTmpDirEnvName, goTmp)
	if err := os.MkdirAll(goTmp, DirPerm); err != nil {
		t.Fatal(err)
	}
	tree, ok := treeByName(TreeNameGoTmp)
	if !ok {
		t.Fatalf("the %s tree must be declared", TreeNameGoTmp)
	}

	// AGED means "past the TREE'S OWN window" (GoTmpStaleAge) — the sweep no
	// longer accepts a caller's override, so a 2h entry under a 24h row is a KEEP
	// by construction and would make this fixture assert nothing (task 01a17b8b-581a).
	agedAge := GoTmpStaleAge + time.Hour
	aged := seedGoBuildDir(t, goTmp, "go-build-aged", agedAge, 4096)
	young := seedGoBuildDir(t, goTmp, "go-build-young", 0, 4096)
	held := seedGoBuildDir(t, goTmp, "go-build-held", agedAge, 4096)
	RegisterHolder(held)
	// Re-age AFTER the registration: writing the .holder record inside the entry
	// refreshes the entry's own mtime, so without this line the held case would
	// be kept by the CLOCK and this test would pass while pinning nothing.
	reAge(t, held, agedAge)
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(held, HolderFileName)) //stylegate:ignore t.Cleanup removal of the registration this test wrote; the assertions it supports already ran
	})
	// A namespace MISS: an operator's own directory in the SAME tree. Never a
	// candidate, whatever its age — and, by this arm's decision, never a verdict
	// line either: reporting the neighbours would bury the answer.
	foreign := filepath.Join(goTmp, "operator-notes")
	if err := os.MkdirAll(foreign, DirPerm); err != nil {
		t.Fatal(err)
	}

	verdicts, summary := DeclaredTreeVerdicts(tree)
	byPath := map[string]EntryVerdict{}
	for _, v := range verdicts {
		if strings.TrimSpace(v.Reason) == "" {
			t.Errorf("%s: a verdict with no reason is the Rule 15 defect this face exists to close", v.Path)
		}
		byPath[v.Path] = v
	}

	// ① the ROOT: kept by design, in its own line.
	if v, ok := byPath[goTmp]; !ok {
		t.Errorf("the tree's own ROOT has no verdict line — a container that is not reported looks escaped, not protected")
	} else if v.Collectable || !strings.Contains(v.Reason, EntryKeepDeclaredRoot) {
		t.Errorf("the root's verdict must carry %q, got %+v", EntryKeepDeclaredRoot, v)
	}

	// ② the aged candidate: collectable, with the shared prefix.
	if v, ok := byPath[aged]; !ok {
		t.Errorf("the aged candidate %s has no verdict", aged)
	} else if !v.Collectable || !strings.HasPrefix(v.Reason, EntryCollectablePrefix) {
		t.Errorf("the aged unheld entry must be reported collectable, got %+v", v)
	}
	// …and the predicate agrees, because it is the same decision point.
	scan := ScanHolders()
	if !declaredEntryCollectable(tree, aged, time.Now(), tree.window(), heldTreeScan{index: heldSet(scan, goTmp), scan: scan}) {
		t.Errorf("the bool face disagrees with the reason face about the aged entry")
	}

	// ③ the young entry: kept, and the reason names the CLOCK (not the holder).
	if v, ok := byPath[young]; !ok {
		t.Errorf("the young entry %s has no verdict — this is the keep the old arm rendered as silence", young)
	} else if v.Collectable || !strings.Contains(v.Reason, EntryKeepFresh) {
		t.Errorf("the young entry's verdict must name %q, got %+v", EntryKeepFresh, v)
	}

	// ④ the held entry: kept, and the reason is the OWNER's own sentence.
	if v, ok := byPath[held]; !ok {
		t.Errorf("the held entry %s has no verdict", held)
	} else if v.Collectable {
		t.Errorf("the held entry must be reported KEPT, got %+v", v)
	} else if !strings.Contains(v.Reason, EntryKeepLiveHolder) || !strings.Contains(v.Reason, OwnerReasonLive) {
		t.Errorf("the held entry's reason must name the holder veto AND the owner evidence, got %q", v.Reason)
	} else if strings.Contains(v.Reason, EntryKeepFresh) {
		t.Errorf("the held entry's reason names the clock — the two keeps must not render the same: %q", v.Reason)
	}

	// ⑤ the namespace miss: NOT a verdict, but COUNTED — a reader must be able to
	// see the walk covered it.
	if _, ok := byPath[foreign]; ok {
		t.Errorf("a namespace miss was reported as a candidate verdict — it is never a candidate")
	}
	if summary.NotOurs != 1 {
		t.Errorf("not_ours=%d, want 1 (the operator's own directory) — an unjudged entry must still be counted", summary.NotOurs)
	}
	if summary.Examined != 3 {
		t.Errorf("examined=%d, want 3 (the three in-namespace entries)", summary.Examined)
	}
	if summary.Collectable != 1 || summary.KeptTotal() != 2 {
		t.Errorf("collectable=%d kept=%d, want 1 and 2", summary.Collectable, summary.KeptTotal())
	}
	if summary.Truncated {
		t.Errorf("the walk reported itself truncated on a fixture of four entries")
	}
	// The distribution says WHICH rule protected each entry — a kept total alone
	// cannot tell the clock from a live owner.
	if n := summary.KeptReasons[reasonOf(t, byPath[young])]; n != 1 {
		t.Errorf("the kept-reason distribution does not count the young entry: %v", summary.KeptReasons)
	}
	if n := summary.KeptReasons[reasonOf(t, byPath[held])]; n != 1 {
		t.Errorf("the kept-reason distribution does not count the held entry: %v", summary.KeptReasons)
	}
}

// TestDeclaredTreeVerdicts_NamesTheContainerKeep is the CONTAINER arm's twin: the
// briefing's measured fixture — a sessions-tree container holding ONE aged
// candidate and ONE session record (not the tree's shape). The container is kept
// because a non-collectable entry remains in it, and BEFORE this face that keep
// was zero output: `--explain` printed nothing about a container the plan had
// silently left alone.
func TestDeclaredTreeVerdicts_NamesTheContainerKeep(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	sessRoot := filepath.Join(home, ".gopi", "sessions")
	if err := os.MkdirAll(sessRoot, DirPerm); err != nil {
		t.Fatal(err)
	}
	container := filepath.Join(sessRoot, sessionIDFixture)
	if err := os.MkdirAll(container, DirPerm); err != nil {
		t.Fatal(err)
	}
	aged := filepath.Join(container, sessionIDFixture+SessionRawLogFileSuffix)
	mustWrite(t, aged, "raw http traffic\n")
	reAge(t, aged, SessionRawLogStaleAge+time.Hour)
	// The session record: the entry that PINS the container — not the tree's
	// `.raw.log` shape, so it is kept whatever its age.
	record := filepath.Join(container, "session.jsonl")
	mustWrite(t, record, "{}\n")

	tree, ok := treeByName(TreeNameSessionRaw)
	if !ok {
		t.Fatalf("the %s tree must be declared", TreeNameSessionRaw)
	}
	verdicts, summary := DeclaredTreeVerdicts(tree)
	byPath := map[string]EntryVerdict{}
	for _, v := range verdicts {
		byPath[v.Path] = v
	}
	if v, ok := byPath[aged]; !ok || !v.Collectable {
		t.Errorf("the aged per-run raw log must be reported collectable, got %+v", v)
	}
	if v, ok := byPath[record]; !ok {
		t.Errorf("the session record has no verdict — the keep that pins the container is exactly what used to be silence")
	} else if v.Collectable || !strings.Contains(v.Reason, EntryKeepNotOurs) {
		t.Errorf("the session record must be reported kept as not-this-tree's-shape, got %+v", v)
	}
	if v, ok := byPath[container]; !ok {
		t.Errorf("the CONTAINER has no verdict — its keep is the reading the briefing could not get anywhere")
	} else if v.Collectable || !strings.Contains(v.Reason, EntryKeepContainerNotDrained) {
		t.Errorf("the container's verdict must name %q, got %+v", EntryKeepContainerNotDrained, v)
	}
	if summary.KeptTotal() == 0 {
		t.Errorf("the summary counted no keep at all: %+v", summary)
	}
}

// TestDeclaredTreeVerdicts_NamesTheUnfinishedScanKeep: a /proc pass that did not
// finish keeps everything, and the sentence must say THAT — not "a live process
// holds it", which would fabricate an observation nobody made (Rule 15).
func TestDeclaredTreeVerdicts_NamesTheUnfinishedScanKeep(t *testing.T) {
	home := t.TempDir()
	goTmp := filepath.Join(home, "workspace", "gopi-go-tmp")
	t.Setenv("GOPI_HOME", home)
	t.Setenv(GoTmpDirEnvName, goTmp)
	if err := os.MkdirAll(goTmp, DirPerm); err != nil {
		t.Fatal(err)
	}
	tree, ok := treeByName(TreeNameGoTmp)
	if !ok {
		t.Fatalf("the %s tree must be declared", TreeNameGoTmp)
	}
	aged := seedGoBuildDir(t, goTmp, "go-build-aged", 2*time.Hour, 4096)

	// The expired-deadline scan is the deterministic way to reach the truncated
	// branch (holder_scan_test.go's rule: the property is of the CODE PATH, never
	// of the host — no load is generated, Rule 27).
	truncated := ScanHoldersWithin(time.Nanosecond)
	if truncated.Complete() {
		t.Fatalf("sanity: a 1ns-bounded scan reported itself complete")
	}
	held := heldTreeScan{index: heldSet(truncated, goTmp), scan: truncated}

	if declaredEntryCollectable(tree, aged, time.Now(), time.Hour, held) {
		t.Fatalf("a truncated scan authorised a delete — the fail-closed half was weakened")
	}
	reason := declaredEntryKeepReason(tree, aged, time.Now(), time.Hour, held)
	if !strings.Contains(reason, EntryKeepScanIncomplete) {
		t.Errorf("the truncated-scan keep must name %q, got %q", EntryKeepScanIncomplete, reason)
	}
	if strings.Contains(reason, EntryKeepLiveHolder) {
		t.Errorf("the truncated-scan keep claims a holder nobody observed: %q", reason)
	}
}

// TestDeclaredEntryCollectable_IsTheReasonFace delegates: the sweep's bool and the
// report's sentence are ONE decision. This is the anti-drift pin — a future
// author who re-derives "why" in the report breaks it here.
func TestDeclaredEntryCollectable_IsTheReasonFace(t *testing.T) {
	home := t.TempDir()
	goTmp := filepath.Join(home, "workspace", "gopi-go-tmp")
	t.Setenv("GOPI_HOME", home)
	t.Setenv(GoTmpDirEnvName, goTmp)
	if err := os.MkdirAll(goTmp, DirPerm); err != nil {
		t.Fatal(err)
	}
	tree, ok := treeByName(TreeNameGoTmp)
	if !ok {
		t.Fatalf("the %s tree must be declared", TreeNameGoTmp)
	}
	// AGED means "past the TREE'S OWN window" (GoTmpStaleAge) — the sweep no
	// longer accepts a caller's override, so a 2h entry under a 24h row is a KEEP
	// by construction and would make this fixture assert nothing (task 01a17b8b-581a).
	agedAge := GoTmpStaleAge + time.Hour
	aged := seedGoBuildDir(t, goTmp, "go-build-aged", agedAge, 4096)
	young := seedGoBuildDir(t, goTmp, "go-build-young", 0, 4096)
	held := seedGoBuildDir(t, goTmp, "go-build-held", agedAge, 4096)
	RegisterHolder(held)
	// Re-age AFTER the registration: writing the .holder record inside the entry
	// refreshes the entry's own mtime, so without this line the held case would
	// be kept by the CLOCK and this test would pass while pinning nothing.
	reAge(t, held, agedAge)
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(held, HolderFileName)) //stylegate:ignore t.Cleanup removal of the registration this test wrote; the assertions it supports already ran
	})

	scan := ScanHolders()
	index := heldTreeScan{index: heldSet(scan, goTmp), scan: scan}
	now := time.Now()
	for _, tc := range []struct {
		name          string
		path          string
		wantCollect   bool
		wantReasonSub string
	}{
		{"aged", aged, true, ""}, // the empty reason IS the collectable signal
		{"young", young, false, EntryKeepFresh},
		{"held", held, false, EntryKeepLiveHolder},
		{"root", goTmp, false, EntryKeepDeclaredRoot},
	} {
		reason := declaredEntryKeepReason(tree, tc.path, now, time.Hour, index)
		if got := reason == ""; got != tc.wantCollect {
			t.Errorf("%s: collectable=%t from the reason %q, want %t", tc.name, got, reason, tc.wantCollect)
		}
		if tc.wantReasonSub == "" {
			if reason != "" {
				t.Errorf("%s: a collectable entry must answer with the EMPTY reason, got %q", tc.name, reason)
			}
		} else if !strings.Contains(reason, tc.wantReasonSub) {
			t.Errorf("%s: reason %q does not carry %q", tc.name, reason, tc.wantReasonSub)
		}
		if got := declaredEntryCollectable(tree, tc.path, now, time.Hour, index); got != tc.wantCollect {
			t.Errorf("%s: the bool face says %t, the reason face says %t — the two have DRIFTED", tc.name, got, tc.wantCollect)
		}
	}
}

// reasonOf reads a verdict's reason, failing the test when the verdict is absent
// (so a missing line is reported as the defect it is rather than as a panic).
// sessionIDFixture is one container's name in the sessions-tree fixtures — the
// shape the real writers mint (a session id), spelled once.
const sessionIDFixture = "0f1e2d3c-4b5a-6978-8765-43210fedcba9"

// mustWrite writes a fixture file, failing the test on error.
func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), FilePerm); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func reasonOf(t *testing.T, v EntryVerdict) string {
	t.Helper()
	if v.Reason == "" {
		t.Fatalf("a verdict with no reason was read through reasonOf")
	}
	return v.Reason
}
