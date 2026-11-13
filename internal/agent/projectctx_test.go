package agent

// projectctx_test.go — the regression proof for the project-context walk
// (epic project-context-walkup, Rule 5: every fix ships a minimal repro).
//
// THE DEFECT (measured 2026-09-21): the loader read `GOPI.md` from the process
// cwd ONLY, so a run started from a subdirectory of the repo lost the whole
// project context silently.
//
// THE REPRO below (**red on the pre-change code**) is the first test in this
// file: the file exists ONLY at a top directory, the resolution starts one
// level down, and it must come back with the top file's path. Nothing is
// mocked — real directories, a real file, real bytes.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// projectCtxTestBody is the marker body a resolved file must carry, so a test
// asserting "the right file" can prove it by CONTENT and not only by path.
const projectCtxTestBody = "project rules for the walk-up test\n"

// writeProjectCtx writes the marker file into dir and returns its path.
func writeProjectCtx(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, projectContextFileName)
	if err := os.WriteFile(p, []byte(projectCtxTestBody), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// TestProjectContextResolvesFromNestedDir is THE regression test: the file is
// only at the top, resolution starts two levels down, and the walk must find
// it.
func TestProjectContextResolvesFromNestedDir(t *testing.T) {
	top := t.TempDir()
	want := writeProjectCtx(t, top)
	nested := filepath.Join(top, "internal", "tools")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", nested, err)
	}

	got := ResolveProjectContextFile(nested)
	if got.Path != want {
		t.Fatalf("resolution from %s = %q, want %q (the file is one or two levels up — the pre-change loader read the cwd only and returned nothing)",
			nested, got.Path, want)
	}
	// The searched list must START at the directory the caller named: a walk
	// that reported a different origin would make the operator-facing line
	// useless.
	if len(got.Searched) == 0 || got.Searched[0] != nested {
		t.Fatalf("searched list %v does not start at the requested directory %s", got.Searched, nested)
	}
}

// TestProjectContextStopsAtTheOutermostRepoTop pins the repo boundary: a
// repository nested inside another repository is a fixture/vendored checkout,
// and the OUTER one is the project — so the walk may not continue past the
// outer top (an ancestor file must not answer), and it must not stop at the
// inner one either.
func TestProjectContextStopsAtTheOutermostRepoTop(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "vendored")
	for _, d := range []string{outer, inner} {
		if err := os.MkdirAll(filepath.Join(d, gitDirEntryName), 0o755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}
	}
	start := filepath.Join(inner, "pkg")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", start, err)
	}

	got := ResolveProjectContextFile(start)
	if got.Path != "" {
		t.Fatalf("resolved %q, want no file (the walk must reach the outermost repo top and stop there)", got.Path)
	}
	wantSearched := []string{start, inner, outer}
	if len(got.Searched) != len(wantSearched) {
		t.Fatalf("searched %v, want exactly %v (nothing past the outermost repo top)", got.Searched, wantSearched)
	}
	for i := range wantSearched {
		if got.Searched[i] != wantSearched[i] {
			t.Fatalf("searched %v, want %v", got.Searched, wantSearched)
		}
	}
}

// TestProjectContextStopsAtAWorktreeTop pins the worktree boundary: a linked
// worktree is its own project (its own tracked file), so the walk stops at its
// top and never climbs into whatever checkout stands beside it.
func TestProjectContextStopsAtAWorktreeTop(t *testing.T) {
	parent := t.TempDir()
	main := filepath.Join(parent, "main")
	wt := filepath.Join(parent, "wt")
	for _, d := range []string{main, wt} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if err := os.Mkdir(filepath.Join(main, gitDirEntryName), 0o755); err != nil {
		t.Fatalf("mkdir main/.git: %v", err)
	}
	// A LINKED worktree's .git entry is a FILE (the gitdir pointer).
	if err := os.WriteFile(filepath.Join(wt, gitDirEntryName), []byte("gitdir: "+main+"/.git/worktrees/wt\n"), 0o644); err != nil {
		t.Fatalf("write wt/.git: %v", err)
	}
	start := filepath.Join(wt, "internal")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", start, err)
	}

	got := ResolveProjectContextFile(start)
	if got.Path != "" {
		t.Fatalf("resolved %q, want no file", got.Path)
	}
	if len(got.Searched) != 2 || got.Searched[0] != start || got.Searched[1] != wt {
		t.Fatalf("searched %v, want exactly [%s %s] — the walk must stop at the worktree top, not climb into %s",
			got.Searched, start, wt, main)
	}
}

// TestProjectContextOutsideVersionControlReachesTheRoot: with no repo marker
// anywhere above, the walk continues to the filesystem root (the documented
// simplicity stop) — it does not give up early.
func TestProjectContextOutsideVersionControlReachesTheRoot(t *testing.T) {
	top := t.TempDir()
	nested := filepath.Join(top, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", nested, err)
	}
	got := ResolveProjectContextFile(nested)
	if got.Path != "" {
		t.Fatalf("resolved %q, want no file", got.Path)
	}
	last := got.Searched[len(got.Searched)-1]
	if last != string(filepath.Separator) {
		t.Fatalf("walk ended at %q, want the filesystem root %q", last, string(filepath.Separator))
	}
}

// TestProjectContextAbsenceIsReportedAndNeverFatal is the complement half of
// the observability requirement: an absent file prints the directories
// searched, returns an empty context, and does NOT fail the run.
func TestProjectContextAbsenceIsReportedAndNeverFatal(t *testing.T) {
	// A deterministic absence: the walk starts inside a repo nested in a repo
	// (so it stops at a known, file-free boundary) regardless of what the
	// host's ancestors happen to hold.
	outer := t.TempDir()
	inner := filepath.Join(outer, "inner")
	for _, d := range []string{outer, inner} {
		if err := os.MkdirAll(filepath.Join(d, gitDirEntryName), 0o755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}
	}

	// The SYSTEM level is isolated too (epic system-level-GOPI.md): on a host
	// whose profile carries a rules file the merged context would not be empty,
	// and this arm is about the CHECKOUT walk finding nothing.
	t.Setenv(config.GopiDirEnv, t.TempDir())

	read := captureStderr(t)
	ctx := LoadProjectContext(inner)
	msg := read()

	if ctx != "" {
		t.Fatalf("LoadProjectContext returned %q, want empty (absence is optional, never fatal)", ctx)
	}
	if !strings.Contains(msg, "no repository rules file (tried "+coreNameList()+") — searched:") {
		t.Fatalf("stderr does not report the absence with the searched list:\n%s", msg)
	}
	for _, d := range []string{inner, outer} {
		if !strings.Contains(msg, d) {
			t.Fatalf("searched list does not name %s:\n%s", d, msg)
		}
	}
	if strings.Contains(msg, "warning") {
		t.Fatalf("a plain absence must not warn (optional stays optional):\n%s", msg)
	}
}

// TestProjectContextLoadIsReadOnceAndReportsBytes fixes the loaded line's
// shape and the one-read property: the message carries the byte count, the
// source directory, and it appears EXACTLY ONCE per session start (a second
// read would break the prompt-cache invariant the loader documents).
func TestProjectContextLoadIsReadOnceAndReportsBytes(t *testing.T) {
	top := t.TempDir()
	writeProjectCtx(t, top)
	nested := filepath.Join(top, "cmd", "agent")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", nested, err)
	}

	t.Setenv(config.GopiDirEnv, t.TempDir()) // only the REPOSITORY level loads in this arm
	read := captureStderr(t)
	ctx := LoadProjectContext(nested)
	msg := read()

	want := "[gopi] loaded " + projectContextFileName + " (" + strconv.Itoa(len(projectCtxTestBody)) + " bytes)"
	if !strings.Contains(msg, want) {
		t.Fatalf("stderr does not carry %q:\n%s", want, msg)
	}
	// The merged context carries the repository body under its heading
	// (epic system-level-GOPI.md): the BYTES are still the file's own, which is
	// what "read once, report the bytes" pins.
	if wantCtx := repositoryProjectContextHeading + "\n\n" + strings.TrimSpace(projectCtxTestBody); ctx != wantCtx {
		t.Fatalf("loaded context = %q, want %q", ctx, wantCtx)
	}
	if n := strings.Count(msg, "[gopi] loaded "+projectContextFileName); n != 1 {
		t.Fatalf("the loaded line appeared %d times, want exactly 1 (one read per session start):\n%s", n, msg)
	}
}

// TestProjectContextSearchedLineIsBounded: the rendered list is capped and a
// capped rendering SAYS what it dropped (Rule 15: never a silent truncation).
func TestProjectContextSearchedLineIsBounded(t *testing.T) {
	dirs := make([]string, maxSearchedDirsPrinted+3)
	for i := range dirs {
		dirs[i] = "/d" + strconv.Itoa(i)
	}
	line := searchDirsLine(dirs)
	if !strings.Contains(line, "(3 more)") {
		t.Fatalf("capped list %q does not state how many directories it dropped", line)
	}
	if strings.Count(line, "/d") != maxSearchedDirsPrinted {
		t.Fatalf("capped list %q shows %d directories, want %d", line, strings.Count(line, "/d"), maxSearchedDirsPrinted)
	}
	if got := searchDirsLine(nil); !strings.Contains(got, "no directory could be searched") {
		t.Fatalf("empty searched list renders %q, want an explicit explanation", got)
	}
}
