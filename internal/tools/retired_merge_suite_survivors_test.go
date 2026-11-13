package tools

// Retired-merge-suite survivors.
//
// These declarations were BORN inside merge test files. When that suite was
// retired (task 01a17fdc) tests that are not merge tests still called them, so
// each was carried out here VERBATIM rather than rewritten at its call site
// (PROJECT.md Rule 43: a symbol referenced outside the mechanism it was born in
// is a general tool — move it first, then delete the mechanism).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// containsString reports whether names holds want.
func containsString(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func itoaForTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// --- fixture helpers (share rebuild_test.go's git runner) ---

// rebuildTestGitE runs git returning (output, err) without failing the test
// (for probes that expect a non-zero answer like rev-parse --verify).
func rebuildTestGitE(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what
// was written (taskNotifyLog writes stderr unconditionally — the same seam the
// off-policy worktree test uses).
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stderr
	os.Stderr = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}
	os.Stderr = prev
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close read end: %v", err)
	}
	return string(out)
}

// deadPID (the corpse factory used below) already exists in this package —
// claim_holder_freshness_test.go defines it for the same reason (a hand-picked
// constant could belong to a live process). Reused, not re-implemented.

// lockFixture creates a lock file held by THIS process, with the given stamp,
// and returns (path, inode).
func lockFixture(t *testing.T, stamp string) (string, uint64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "serve.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() }) //stylegate:ignore test cleanup: the descriptor must stay open for the whole test body
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Skipf("cannot take flock in this environment: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }) //stylegate:ignore t.Cleanup unlock of the file this test exclusively locked; the descriptor closes with the test
	if _, err := f.WriteAt([]byte(stamp), 0); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, inodeOf(st)
}

// fakeLockTable installs a /proc/locks fixture naming pid for ino — the
// inherited-lock shape, where the named pid is the dead creator.
func fakeLockTable(t *testing.T, pid int, ino uint64) {
	t.Helper()
	saved := procLockTableRead
	procLockTableRead = func() ([]byte, error) {
		return []byte(fmt.Sprintf("1: FLOCK  ADVISORY  WRITE %d 08:04:%d 0 EOF\n", pid, ino)), nil
	}
	t.Cleanup(func() { procLockTableRead = saved })
}

const (
	// mbgSettingsFileName is the settings file the fixtures write.
	mbgSettingsFileName = "settings.json"
	// mbgTestBuildCmd is the declared build command: a script the fixtures
	// control, so the gate's decision is driven by real git + a real
	// subprocess without compiling a Go program on every run.
	mbgTestBuildCmd = "sh ./" + mbgBuildScript
	// mbgBuildScript / mbgBrokenFileName are the fixture's build script and
	// the broken revision's file (the incident shape: a source file
	// referencing package-level symbols that do not exist).
	mbgBuildScript    = "build.sh"
	mbgBrokenFileName = "broken_revision.go"
	// mbgTestBuildTimeoutSec bounds the fixture builds (a hung fixture must
	// fail loudly, not wedge the suite).
	mbgTestBuildTimeoutSec = 60
	// mbgSlowBuildCmd / mbgSlowBuildTimeoutSec drive the timeout arm: a build
	// that outlives its declared budget must ABSTAIN (a timeout is not a
	// compile error).
	mbgSlowBuildCmd        = "sleep 30"
	mbgSlowBuildTimeoutSec = 1
	// mbgTimeoutSlackSec is the wall-time headroom the timeout assertion
	// allows on top of the gate's own bounds (scratch worktree add/remove,
	// process teardown): the fixture's build sleeps 30s, so any leak of the
	// grandchild past the deadline is unmistakable against this ceiling.
	mbgTimeoutSlackSec = 10
	// mbgBrokenBranch / mbgCleanBranch are the two candidates the fixtures
	// queue: the incident's shape, and a branch that must still land.
	mbgBrokenBranch = "fix/merger-rebuild-selfrestart"
	mbgCleanBranch  = "feat/clean-build"
	// mbgFilePerm is the standard readable file mode for the fixtures.
	mbgFilePerm = 0o644
)

// mbgTestDB points GOPI_MEMORY_DB at a fresh ledger and returns its path (the
// refusal lane writes a task row, so the fixtures need one).
func mbgTestDB(t *testing.T) string {
	t.Helper()
	db := filepath.Join(t.TempDir(), "mem.db")
	t.Setenv("GOPI_MEMORY_DB", db)
	return db
}

// mbgWriteSettings points GOPI_SETTINGS at a settings file declaring repo's
// build contract (Rule 12: the build command is DECLARED, never discovered).
func mbgWriteSettings(t *testing.T, repo, buildCmd string, timeoutSec int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), mbgSettingsFileName)
	body, err := json.Marshal(map[string]any{
		"repos": map[string]any{
			repo: map[string]any{"build": buildCmd, "build_timeout_sec": timeoutSec},
		},
	})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := os.WriteFile(path, body, mbgFilePerm); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", path)
}

// mbgCommitBuildScript commits the fixture build script on the CURRENT branch.
func mbgCommitBuildScript(t *testing.T, repo string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, mbgBuildScript), []byte(mbgBuildScriptBody), mbgFilePerm); err != nil {
		t.Fatalf("write %s: %v", mbgBuildScript, err)
	}
	run(t, repo, "add", mbgBuildScript)
	run(t, repo, "commit", "-q", "-m", "fixture: declared build contract")
}

// mbgMakeBrokenBranch creates the incident's branch: the declared build script
// is inherited from master, and the broken revision adds a file referencing
// never-defined symbols, plus the diagnostics its build prints.
func mbgMakeBrokenBranch(t *testing.T, repo, branch string) {
	t.Helper()
	run(t, repo, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, mbgBrokenFileName), []byte(mbgBrokenRevisionBody()), mbgFilePerm); err != nil {
		t.Fatalf("write broken revision: %v", err)
	}
	diag := strings.Join(mbgIncidentDiagnostics, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(repo, mbgBrokenFileName+".diagnostics"), []byte(diag), mbgFilePerm); err != nil {
		t.Fatalf("write diagnostics: %v", err)
	}
	run(t, repo, "add", mbgBrokenFileName, mbgBrokenFileName+".diagnostics")
	run(t, repo, "commit", "-q", "-m", "fixture: the incident's non-compiling revision")
	run(t, repo, "checkout", "-q", "master")
}

// mbgCountReworkRows counts the ledger's rows for one query (the dedup
// assertion: ONE row per branch however it was refused).
func mbgCountRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count query: %v", err)
	}
	return n
}

// mbgReworkRow reads the branch-rework row's payload for branch (the row the
// refusal lane mints; payload + deliverable are what a reworker claims).
func mbgReworkRow(t *testing.T, branch string) (payload, deliverable, status string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	row := db.QueryRowContext(ctx,
		`SELECT payload, deliverable, status FROM tasks WHERE summary = ?`,
		resolveRowReworkSummaryPrefix+branch)
	if err := row.Scan(&payload, &deliverable, &status); err != nil {
		t.Fatalf("no branch-rework row for %s: %v", branch, err)
	}
	return payload, deliverable, status
}

// mbgBuildScriptBody is the fixture's declared build command: it fails with the
// incident's measured diagnostics when the broken revision's file is present,
// and succeeds on a clean revision (master).
var mbgBuildScriptBody = "#!/bin/sh\n" +
	"if [ -f " + mbgBrokenFileName + " ]; then\n" +
	"\techo '# the incident (82b05c4f, merged by 82b3b6f3): ten never-defined symbols' 1>&2\n" +
	"\tcat " + mbgBrokenFileName + ".diagnostics 1>&2\n" +
	"\texit 1\n" +
	"fi\n" +
	"exit 0\n"

// mbgBrokenRevisionBody renders the broken revision: a source file referencing
// every symbol the incident's revision referenced (the shape the compiler
// rejects), with the incident's revision named in its header.
func mbgBrokenRevisionBody() string {
	var b strings.Builder
	b.WriteString("// Fixture for the 2026-09-13 incident: fix/merger-rebuild-selfrestart\n")
	b.WriteString("// (82b05c4f) referenced these symbols; none is defined anywhere, so the\n")
	b.WriteString("// revision does not compile — and the merge did not care.\n")
	b.WriteString("package main\n\nfunc selfRestartFixture() {\n")
	for _, sym := range mbgIncidentSymbols {
		b.WriteString("\t_ = " + sym + "\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// mbgIncidentDiagnostics are the six diagnostic lines MEASURED from the
// incident revision (file:line:col, verbatim). The fixture's build command
// prints exactly these on stderr, so the refusal path is exercised with real
// compiler text of the real shape.
var mbgIncidentDiagnostics = []string{
	"cmd/agent/merger_self_restart.go:373:47: undefined: mergerDaemonLogDirMode",
	"cmd/agent/merger_self_restart.go:376:76: undefined: mergerDaemonLogFileMode",
	"cmd/agent/merger_self_restart.go:393:3: undefined: mergerDaemonReadyEnv",
	"cmd/agent/merger_self_restart.go:393:41: undefined: mergerDaemonReadyChildFD",
	"cmd/agent/merger_self_restart.go:419:12: undefined: mergerDaemonAwaitReady",
	"cmd/agent/merger_self_restart.go:420:3: undefined: mergerDaemonAbandonChild",
}

// mbgIncidentSymbols are the never-defined symbols the incident's revision
// referenced (measured, see the file header). The broken fixture references
// every one of them: the gate's refusal must carry the compiler's own text,
// and this is the text a real incident produced.
var mbgIncidentSymbols = []string{
	"mergerDaemonLogDirMode",
	"mergerDaemonLogFileMode",
	"mergerDaemonReadyEnv",
	"mergerDaemonReadyChildFD",
	"mergerDaemonAwaitReady",
	"mergerDaemonAbandonChild",
}

const (
	// mbugFixtureModulePath / mbugFixtureGoMod / mbugFixturePkgDir are the
	// tiny Go module the fixtures compile (one package, no dependencies — the
	// compile-only pass must never need the network).
	mbugFixtureModulePath = "module mbugfixture\n\ngo 1.21\n"
	mbugFixtureGoMod      = "go.mod"
	mbugFixturePkgDir     = "p"
	mbugFixturePkgFile    = "p.go"
	mbugFixturePkgBody    = "package p\n\nfunc F() int { return 1 }\n"
	// mbugFixtureMasterFile / mbugFixtureBranchFile are the two files that
	// collide: the first is committed on MASTER (the sibling that already
	// landed), the second by the candidate branch.
	mbugFixtureMasterFile = "helper_master_test.go"
	mbugFixtureBranchFile = "helper_branch_test.go"
	// mbugFixtureConflictFile is the file the textual-conflict fixture edits on
	// both sides (same line).
	mbugFixtureConflictFile = "conflict.txt"
	// mbugFixtureConflictBase / Master / Branch are its three versions: the
	// branch and master each change the FIRST line from base, so the union has
	// a real conflict.
	mbugFixtureConflictBase    = "one\ntwo\n"
	mbugFixtureConflictMaster  = "master-side\ntwo\n"
	mbugFixtureConflictSibling = "branch-side\ntwo\n"
	// mbugFixtureSplitFile is the file the SPLIT-HUNK fixture has BOTH sides
	// edit at DIFFERENT ends (distinct lines in distinct hunks — a clean
	// textual merge), and the three bodies are base/master/candidate. The two
	// edited lines are named separately: the union is asserted to carry BOTH.
	mbugFixtureSplitFile       = "split.txt"
	mbugFixtureSplitBase       = "alpha\nbeta\ngamma\ndelta\n"
	mbugFixtureSplitMasterBody = "alpha\nbeta\ngamma\nmaster-tail\n"
	mbugFixtureSplitBranchBody = "branch-head\nbeta\ngamma\ndelta\n"
	mbugFixtureSplitBranchLine = "branch-head"
	mbugFixtureSplitMasterLine = "master-tail"
	// mbugHelperName is the colliding identifier: declared on master, and again
	// by the candidate branch. mbugHelperOtherName is the control's identifier
	// (a DIFFERENT name — the union compiles and the gate must stay quiet).
	mbugHelperName      = "unionFixtureHelper"
	mbugHelperOtherName = "unionFixtureOtherHelper"
	// mbugUndefinedSymbol is the never-defined symbol the failure fixtures
	// reference (the same shape the ⓪b fixture uses, scaled to Go).
	mbugUndefinedSymbol = "mbugNeverDefinedSymbol"
	// mbugFixtureBranches / mbugFixtureCleanBranch / mbugFixtureConflictBranch
	// are the candidate names the fixtures create.
	mbugFixtureBranch         = "feat/union-helper-b"
	mbugFixtureCleanBranch    = "feat/union-helper-control"
	mbugFixtureRedBranch      = "feat/union-helper-red"
	mbugFixtureConflictBranch = "feat/union-conflict"
	// mbugFixtureSplitBranch is the SPLIT-HUNK fixture's candidate: it edits a
	// different hunk of the SAME file master edits (a clean merge).
	mbugFixtureSplitBranch    = "feat/union-split-hunk"
	mbugFixtureAdvancedBranch = "feat/union-helper-advanced"
	// mbugFixtureSiblingBranch is the branch whose addition LANDED on master
	// first (the 2026-09-16 db1fcef6 side of the collision).
	mbugFixtureSiblingBranch = "feat/union-helper-sibling"
	// mbugFilePerm is the standard readable file mode for the fixtures.
	mbugFilePerm = 0o644
)

// mbugIncidentShape builds the 2026-09-16 collision as it actually happened:
// TWO branches cut from a COMMON BASE, each adding one helper with the same
// name to the same package's test files. The first is landed on master (the
// sibling), the candidate is still open — and it is cut from the BASE, not from
// the advanced master, which is what makes the branch green ALONE. That is the
// whole point of the shape: branch-alone green, master-alone green, union red.
func mbugIncidentShape(t *testing.T, repo, branch, branchHelper string) {
	t.Helper()
	mbugInitModule(t, repo)
	base := strings.TrimSpace(rsGit(t, repo, true, "rev-parse", "master"))
	// The sibling that landed first, developed in parallel from the same base.
	mbugMakeBranch(t, repo, mbugFixtureSiblingBranch, filepath.Join(mbugFixturePkgDir, mbugFixtureMasterFile),
		mbugHelperBody(mbugHelperName))
	run(t, repo, "merge", "-q", mbugFixtureSiblingBranch)
	// The candidate, cut from the BASE (never from the landed sibling): one
	// declaration on each side, one collision in the union.
	run(t, repo, "checkout", "-q", "-b", branch, base)
	mbugWriteFile(t, repo, filepath.Join(mbugFixturePkgDir, mbugFixtureBranchFile), mbugHelperBody(branchHelper))
	run(t, repo, "add", filepath.Join(mbugFixturePkgDir, mbugFixtureBranchFile))
	run(t, repo, "commit", "-q", "-m", branch+" work")
	run(t, repo, "checkout", "-q", "master")
}

// run executes git in repo, failing the test on error.
func run(t *testing.T, repo string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

// mbugInitModule writes the fixture Go module on master and commits it.
func mbugInitModule(t *testing.T, repo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repo, mbugFixturePkgDir), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", mbugFixturePkgDir, err)
	}
	mbugWriteFile(t, repo, mbugFixtureGoMod, mbugFixtureModulePath)
	mbugWriteFile(t, repo, filepath.Join(mbugFixturePkgDir, mbugFixturePkgFile), mbugFixturePkgBody)
	run(t, repo, "add", mbugFixtureGoMod, filepath.Join(mbugFixturePkgDir, mbugFixturePkgFile))
	run(t, repo, "commit", "-q", "-m", "fixture: the module compiles")
}

// mbugMakeBranch creates branch off the CURRENT master tip, writes rel there,
// commits, and returns to master.
func mbugMakeBranch(t *testing.T, repo, branch, rel, body string) {
	t.Helper()
	run(t, repo, "checkout", "-q", "-b", branch)
	mbugWriteFile(t, repo, rel, body)
	run(t, repo, "add", rel)
	run(t, repo, "commit", "-q", "-m", branch+" work")
	run(t, repo, "checkout", "-q", "master")
}

// mbugHelperBody renders a _test.go file declaring one package-level helper —
// the smallest revision that produces a redeclaration when two of them meet.
func mbugHelperBody(name string) string {
	return "package p\n\nfunc " + name + "() int { return 1 }\n"
}

// mbugWriteFile writes rel inside repo with the fixture's file mode.
func mbugWriteFile(t *testing.T, repo, rel, body string) {
	t.Helper()
	path := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
	}
	if err := os.WriteFile(path, []byte(body), mbugFilePerm); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// mhRepo creates a temp git repo (TaskSubmit verifies a repo before it writes,
// so the escalation's submit needs a real one).
func mhRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mhGit(t, dir, "init", "-q", "-b", "master")
	mhGit(t, dir, "config", "user.email", "t@t")
	mhGit(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mhGit(t, dir, "add", "base.txt")
	mhGit(t, dir, "commit", "-q", "-m", "base")
	return dir
}

// --- fixture helpers -------------------------------------------------------

// mhGit runs git in dir, failing the test on error.
func mhGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// mgWrite writes rel inside repo, creating parents.
func mgWrite(t *testing.T, repo, rel, body string) {
	t.Helper()
	path := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// mgGit runs git in repo and returns its trimmed stdout.
func mgGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	return rebuildTestGit(t, repo, args...)
}
