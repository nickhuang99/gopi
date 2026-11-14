package main

// Pass-level rebuild invariant tests (epic merger-always-rebuilds, 2026-09-15).
//
// THE INVARIANT: a merger pass ends by bringing every configured repo's binary
// up to its master tip, whichever code path did the landing — the per-landing
// call inside mergerSweepRepo only covers the sweeps that were remembered
// (measured gap: serveAdoptionSweep lands adopted branches and never rebuilds;
// `gopi task merge-execute` merges with no rebuild at all).
//
// WHAT THESE TESTS PIN (three layers, because each one alone can pass
// vacuously):
//
//	① the MECHANISM: mergerPassCloseLoop rebuilds a stale artifact and is
//	   idempotent on the second call (no per-pass rebuild storm);
//	② the SCOPE: no declared build contract = silent, no per-pass log noise;
//	③ the WIRING AND ITS ORDER: mergerPassOnce really calls it, and calls it
//	   AFTER the per-repo sweep loop — the loop is where the merger BLOCKS on
//	   its spawned resolve/verify children, so this is what makes the rebuild
//	   follow the child's success/failure return (the operator's requirement)
//	   rather than racing it. ③ is a source-level assertion on purpose: it is
//	   the red-proof that fails the moment the invariant call is deleted or
//	   moved back above the sweep.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// staleBinRevision is a revision the fixture's artifact claims but the repo
// does NOT stand at — the staleness the invariant must repair.
const staleBinRevision = "deadbeef"

// writeStaleBin writes repo/bin/gopi as a fake artifact that reports a FIXED
// revision (staleBinRevision) whatever the repo does, so "stale" is provable
// (the shared tipBinBuild fixture computes the tip at call time and can never
// be stale).
func writeStaleBin(t *testing.T, repo string) {
	t.Helper()
	bin := filepath.Join(repo, testutil.BinDirName, testutil.BinFileName)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo " + staleBinRevision + "; fi\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestMergerPassCloseLoopRebuildsStaleBin: the invariant's mechanism — a repo
// whose artifact does not follow its master tip is rebuilt, and a second pass
// finds it current (idempotence: the invariant may run every pass).
func TestMergerPassCloseLoopRebuildsStaleBin(t *testing.T) {
	repo := rebuildFixtureRepo(t, tipBinBuild("__unused__"))
	writeReposEntry(t, repo) // declare the REAL build (writes the tip-reporting bin)
	writeStaleBin(t, repo)

	out := capturePassCloseLoop(t, []string{repo})
	if !strings.Contains(out, "built in") {
		t.Fatalf("stale artifact must be rebuilt by the pass close loop, got %q", out)
	}
	// The rebuilt artifact now follows the tip — proven by the SECOND pass
	// taking the idempotent shortcut instead of building again.
	out2 := capturePassCloseLoop(t, []string{repo})
	if !strings.Contains(out2, "already current") {
		t.Fatalf("a second pass must be idempotent (already current), got %q", out2)
	}
}

// TestMergerPassCloseLoopSilentWithoutContract: a repo with no declared build
// (docs-only repo / pure merge target) produces NO output — the invariant must
// not turn into a per-pass log line for every repo that never builds.
func TestMergerPassCloseLoopSilentWithoutContract(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	// A settings file whose repos table declares a DIFFERENT repo (so this
	// one is simply absent) and one pure merge target with an empty build.
	settingsDir := t.TempDir()
	settingsPath := filepath.Join(settingsDir, "settings.json")
	body := `{"repos":{"/nonexistent/other":{"build":"true"},"/nonexistent/pure":{}}}`
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)
	t.Setenv("GOPI_HOME", t.TempDir())

	out := capturePassCloseLoop(t, []string{repo, "/nonexistent/pure"})
	if strings.TrimSpace(out) != "" {
		t.Fatalf("no declared build contract must be silent, got %q", out)
	}
}

// TestMergerPassCloseLoopWiredAfterSweep is the WIRING + ORDERING red-proof:
// mergerPassOnce must ARM the invariant as a DEFER, and no `return` in the pass
// body may precede that arm. Deleting the arm, replacing the defer with a
// statement that a `return` can step over, or inserting an early return above
// it, fails here.
//
// THE DEFER IS THE ASSERTION, not an implementation detail: the pass has two
// early exits (unreadable settings, empty repos table) and both of them sit
// BELOW the adoption sweep — a master-advancing step. Measured on the previous
// revision of this fix: `gopi merger --once` with an empty repos table returned
// between the adoption sweep and the rebuild, so a pass that adopted branches
// left the binary stale. A defer fires on every exit path, which is what makes
// the rebuild an invariant instead of a statement in the happy path.
func TestMergerPassCloseLoopWiredAfterSweep(t *testing.T) {
	const (
		srcFile      = "cli_tasks.go"
		passFuncName = "func mergerPassOnce("
		sweepLoop    = "mergerSweepRepo(repo)"
		arm          = "defer func() { mergerPassCloseLoop(repos) }()"
	)
	b, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("read %s: %v", srcFile, err)
	}
	src := string(b)
	body := braceFuncBody(t, src, passFuncName)

	sweepAt := strings.Index(body, sweepLoop)
	if sweepAt < 0 {
		t.Fatalf("%s no longer contains %q — this test's ordering anchor moved; update it deliberately", srcFile, sweepLoop)
	}
	armAt := strings.Index(body, arm)
	if armAt < 0 {
		t.Fatalf("mergerPassOnce does not arm %q — the pass-level rebuild invariant is UNWIRED (or is a statement an early `return` can step over): every landing path that does not rebuild itself (the adoption sweep, task merge-execute) leaves bin/gopi stale", arm)
	}
	// Every exit must come AFTER the arm, or that exit skips the rebuild. The
	// scan is by OFFSET into the body (not strings.Index, which would re-find
	// an identical earlier line and report a false position).
	for _, off := range returnsBefore(body, armAt) {
		t.Fatalf("mergerPassOnce has a `return` at body offset %d BEFORE %q (body: %q) — that exit path skips the rebuild entirely; arm the invariant first", off, arm, strings.TrimSpace(body[off:min(off+40, len(body))]))
	}
}

// returnsBefore returns the offsets of every `return` statement inside body
// that precedes the arm offset.
func returnsBefore(body string, armAt int) []int {
	var out []int
	for off := armAt; off > 0; {
		idx := strings.LastIndex(body[:off], "return")
		if idx < 0 {
			break
		}
		// Only a statement-initial keyword counts (skip identifiers such as a
		// returnValue field, and only lines that are exactly `return`-led).
		lineStart := strings.LastIndex(body[:idx], "\n") + 1
		if strings.HasPrefix(strings.TrimSpace(body[lineStart:idx+len("return")]), "return") {
			out = append(out, idx)
		}
		off = idx
	}
	return out
}

// TestMergeExecFaceRebuildsAfterLanding is the SECOND face's wiring red-proof:
// `gopi task merge-execute` merges a branch into master, so it must reach the
// same close loop — and must reach it only for a run that actually landed
// (MergeExecLanded), never for a guard skip or a conflict.
func TestMergeExecFaceRebuildsAfterLanding(t *testing.T) {
	b, err := os.ReadFile("cli_tasks.go")
	if err != nil {
		t.Fatalf("read cli_tasks.go: %v", err)
	}
	body := braceFuncBody(t, string(b), "func runTaskMergeExecute(")
	if !strings.Contains(body, "mergerCloseLoopAfterLanding(rep.Repo)") {
		t.Fatalf("runTaskMergeExecute does not reach mergerCloseLoopAfterLanding — the merge-execute face advances master with NO rebuild (the gap this epic closes)")
	}
	if !strings.Contains(body, "tools.MergeExecLanded(rep)") {
		t.Fatalf("runTaskMergeExecute rebuilds without gating on tools.MergeExecLanded — a failed/skipped row would trigger a rebuild (and mint a name) for a landing that never happened")
	}
}

// TestMergeExecLanded pins the gate's semantics: only a COMPLETED run advanced
// the integration branch.
func TestMergeExecLanded(t *testing.T) {
	cases := []struct {
		name string
		rep  *tools.MergeExecResult
		want bool
	}{
		{name: "completed lands", rep: &tools.MergeExecResult{Status: "completed"}, want: true},
		{name: "failed does not", rep: &tools.MergeExecResult{Status: "failed"}, want: false},
		{name: "skipped does not", rep: &tools.MergeExecResult{Status: "skipped"}, want: false},
		{name: "nil does not", rep: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tools.MergeExecLanded(tc.rep); got != tc.want {
				t.Fatalf("MergeExecLanded(%+v) = %v, want %v", tc.rep, got, tc.want)
			}
		})
	}
}

// passSerialLegsCall / passSerialLegsDecl are the pass's NON-TICK legs: ONE
// definition reached by BOTH faces (mergerSerialFaceLegs, Rule 14) — the pass
// itself and the launching tick. The drain gate below reads them because on that
// shape the pass reaches the drain by CALL and the drain's own text lives in the
// definition, which is the same requirement stated once.
const (
	passSerialLegsCall = "mergerSerialFaceLegs(opts)"
	passSerialLegsDecl = "func mergerSerialFaceLegs("
	// passDrainCall is the drain itself — the only reader of the durable
	// per-repo rebuild marks.
	passDrainCall = "mergerPassCloseLoop(nil)"
)

// passStartDrain reports where the pass body reaches the durable-mark drain, or
// -1 when it does not. The drain's TEXT and the PASS reaching it may live in two
// places (the shared legs definition, the pass body) — what the ordering gates
// need is the offset at which the PASS gets to the drain, so a call into the
// shared definition reports the call site.
func passStartDrain(t *testing.T, src, passBody string) int {
	t.Helper()
	if i := strings.Index(passBody, passDrainCall); i >= 0 {
		return i
	}
	call := strings.Index(passBody, passSerialLegsCall)
	if call < 0 {
		return -1
	}
	if !strings.Contains(braceFuncBody(t, src, passSerialLegsDecl), passDrainCall) {
		return -1
	}
	return call
}

// funcBody returns the body of the top-level function whose declaration starts
// with decl (brace-matched, so a nested block cannot truncate it early).
func braceFuncBody(t *testing.T, src, decl string) string {
	t.Helper()
	start := strings.Index(src, decl)
	if start < 0 {
		t.Fatalf("declaration %q not found — was the function renamed? (update this test deliberately)", decl)
	}
	open := strings.Index(src[start:], "{")
	if open < 0 {
		t.Fatalf("declaration %q has no body", decl)
	}
	depth := 0
	for i := start + open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		}
	}
	t.Fatalf("declaration %q has an unbalanced body", decl)
	return ""
}

// capturePassCloseLoop runs mergerPassCloseLoop capturing stdout+stderr (the
// same capture shape the sibling serve-rebuild tests use).
func capturePassCloseLoop(t *testing.T, repos []string) string {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	mergerPassCloseLoop(repos)
	//stylegate:ignore write-end of the capture pipes; the bytes are read from the other end below
	_ = wOut.Close()
	//stylegate:ignore write-end of the capture pipes; the bytes are read from the other end below
	_ = wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	outB, _ := readAll(rOut) //stylegate:ignore read end of the capture pipe; a short read is what the assertion reports
	errB, _ := readAll(rErr) //stylegate:ignore read end of the capture pipe; a short read is what the assertion reports
	return string(outB) + string(errB)
}
