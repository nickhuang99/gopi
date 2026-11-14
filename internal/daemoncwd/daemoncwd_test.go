package daemoncwd

// daemoncwd_test.go — the package-level tests of the cwd decision.
//
// WHAT THESE TESTS ARE FOR, and what they are NOT: they pin the DECISION's
// branches against an injected measurement (a fake device table), because the
// interesting branches — "the scratch tree is on `/`", "the scratch tree is
// inside a declared worktree root" — cannot be produced with a real directory on
// a host where /tmp IS on `/`. They deliberately do NOT stand in for the
// epic's acceptance evidence: that is the /proc measurement of a REAL spawned
// daemon in cmd/agent/cli_daemon_cwd_test.go, which is what "the child's cwd is
// escaped" is accepted on.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// fakeDevice builds a deps whose device table is a path→id map, so "on /" and
// "off /" are both constructible in one test binary.
func fakeDevice(rootID uint64, devices map[string]uint64) func(string) (uint64, error) {
	return func(path string) (uint64, error) {
		// Longest-prefix match, so a nested map entry wins over its parent.
		best, bestLen := uint64(0), -1
		for dir, id := range devices {
			if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
				if len(dir) > bestLen {
					best, bestLen = id, len(dir)
				}
			}
		}
		if bestLen < 0 {
			return 0, os.ErrNotExist
		}
		return best, nil
	}
}

// testDeps builds a deps over a real scratch fixture with an injected device
// table. The proc root is a real directory with its own (off-/) device, so the
// degradation branch is exercised the same way production exercises it.
func testDeps(t *testing.T, scratchHome string, devices map[string]uint64, roots []worktreeroots.Root) deps {
	t.Helper()
	proc := t.TempDir()
	devices[proc] = 3
	return deps{
		procRoot:      proc,
		scratchHome:   scratchHome,
		deviceID:      fakeDevice(1, devices),
		rootDeviceID:  func() (uint64, error) { return 1, nil },
		parentOf:      func(int) (int, bool) { return 0, false },
		readCwd:       func(int) (string, bool) { return "", false },
		worktreeRoots: roots,
	}
}

// TestResolveEscapesToProcessRoot: the happy path — the scratch tree is off `/`
// and outside every worktree root, so the process root is chosen and the
// inherited directory is reported as escaped.
func TestResolveEscapesToProcessRoot(t *testing.T) {
	home := t.TempDir()
	inherited := filepath.Join(t.TempDir(), "wt")
	d := testDeps(t, home, map[string]uint64{home: 2, "/": 1}, nil)

	got, escaped, err := resolveWith(d, inherited, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := filepath.Join(ResolvePrefix(home), runtimeDirName, EscapeDirLeaf)
	if got != want {
		t.Fatalf("process root = %q, want %q", got, want)
	}
	if escaped != "" {
		t.Errorf("an escape from a NEUTRAL directory has nothing to explain, got reason %q", escaped)
	}
	if _, statErr := os.Stat(got); statErr != nil {
		t.Errorf("the process root must be CREATED (a child cannot start in a missing directory): %v", statErr)
	}
}

// TestResolveReportsTheWorktreeItLeft: when the child descends from a linked
// worktree, the inherited directory is named in the reason — the log line an
// operator uses to see WHICH worktree was released from its veto.
func TestResolveReportsTheWorktreeItLeft(t *testing.T) {
	home := t.TempDir()
	base := t.TempDir()
	wt := filepath.Join(base, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	d := testDeps(t, home, map[string]uint64{home: 2, "/": 1}, nil)

	got, escaped, err := resolveWith(d, wt, wt)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if escaped != ResolvePrefix(wt) {
		t.Fatalf("the declared worktree must be reported as left behind, got %q", escaped)
	}
	if WithinWorktree(got, wt) {
		t.Fatalf("resolved %q is inside the worktree it must escape", got)
	}
}

// TestResolveRefusesTheScratchTreeInsideADeclaredRoot: the invariant is not
// traded away for a convenience path. When the operator's declared worktree root
// CONTAINS the scratch tree, the process root would be inside a root where a
// future task worktree can live — so it is refused and the answer degrades to
// procfs, which is announced (escapedFrom carries the inherited directory).
func TestResolveRefusesTheScratchTreeInsideADeclaredRoot(t *testing.T) {
	home := filepath.Join(t.TempDir(), "scratch")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	root := worktreeroots.Root{Path: filepath.Dir(home), Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceDefault}
	d := testDeps(t, home, map[string]uint64{home: 2, "/": 1}, []worktreeroots.Root{root})
	if got, _, err := resolveWith(d, "/elsewhere", ""); err != nil || got != d.procRoot {
		t.Fatalf("a scratch tree inside a declared worktree root must degrade to procfs, got %q (err %v)", got, err)
	}
}

// TestResolveRefusesAScratchTreeOnRootFS: Rule 22's measured criterion — a
// scratch tree that shares `/`'s device is NOT a usable process root, however it
// is spelled. The escape degrades to procfs rather than silently pinning the
// child to the boot partition's filesystem.
func TestResolveRefusesAScratchTreeOnRootFS(t *testing.T) {
	home := t.TempDir()
	d := testDeps(t, home, map[string]uint64{"/": 1}, nil) // every path resolves to /
	got, _, err := resolveWith(d, "/elsewhere", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != d.procRoot {
		t.Fatalf("a scratch tree on / must be refused; want the proc root %q, got %q", d.procRoot, got)
	}
}

// TestResolveKeepsTheInheritedDirWhenNothingIsProvable: with no scratch tree and
// an unmeasurable procfs there is NO provable root. The inherited directory is
// kept and the caller is told (escapedFrom == dir selects the fallback report) —
// the decision never invents a path (Rule 22) and never fails silently.
func TestResolveKeepsTheInheritedDirWhenNothingIsProvable(t *testing.T) {
	d := deps{
		procRoot:      filepath.Join(t.TempDir(), "no-such-proc"),
		scratchHome:   "",
		deviceID:      func(string) (uint64, error) { return 1, nil },
		rootDeviceID:  func() (uint64, error) { return 1, nil },
		parentOf:      func(int) (int, bool) { return 0, false },
		readCwd:       func(int) (string, bool) { return "", false },
		worktreeRoots: nil,
	}
	got, escaped, err := resolveWith(d, "/inherited/dir", "")
	if err == nil {
		t.Fatalf("no provable root must be reported as an error")
	}
	if got != "/inherited/dir" || escaped != got {
		t.Fatalf("want the inherited directory kept and reported, got dir=%q escaped=%q", got, escaped)
	}
}

// TestEnvOverrideWinsAndIsNotSecondGuessed: the operator's explicit choice is
// honoured — but the escape is still REPORTED when the override lands inside the
// worktree the child descends from, so a hand-set override cannot silently
// reintroduce the veto.
func TestEnvOverrideWinsAndIsNotSecondGuessed(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv(EnvName, dir)
	d := testDeps(t, home, map[string]uint64{home: 2, "/": 1}, nil)
	got, escaped, err := resolveWith(d, dir, dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != dir {
		t.Fatalf("the override must win: got %q want %q", got, dir)
	}
	if escaped == "" {
		t.Errorf("an override inside the escaped worktree must still be REPORTED (Rule 15: never silent)")
	}
}

// TestResolveWorktreeOfWalksToTheLinkedWorktreeAncestor is the MECHANISM's
// safeguard: no caller passes the worktree, and the launcher's cwd is a
// SUBDIRECTORY of it (an agent session's `internal/tools`). The ancestry walk
// must still find the linked worktree — otherwise a daemon spawned by an agent
// standing one level down keeps the veto, which is the live incident shape.
func TestResolveWorktreeOfWalksToTheLinkedWorktreeAncestor(t *testing.T) {
	base := t.TempDir()
	wt := filepath.Join(base, "wt")
	nested := filepath.Join(wt, "internal", "tools")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// The linked-worktree marker: a .git FILE.
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /somewhere/.git/worktrees/wt\n"), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	d := testDeps(t, filepath.Join(base, "scratch"), map[string]uint64{base: 2}, nil)
	if got := resolveWorktreeOf(d, nested, ""); got != ResolvePrefix(wt) {
		t.Fatalf("ancestry walk: want the linked worktree %q, got %q", ResolvePrefix(wt), got)
	}
	// A main checkout (.git DIRECTORY) is NOT a worktree to escape: tasks are
	// delivered in linked worktrees, and escaping the main repo would push every
	// daemon out of the operator's own project for no gain.
	main := filepath.Join(base, "main")
	if err := os.MkdirAll(filepath.Join(main, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if got := resolveWorktreeOf(d, main, ""); got != "" {
		t.Errorf("a main checkout must not be treated as a worktree to escape, got %q", got)
	}
}

// TestEscapeFromWorktreeContract pins the caller-facing CONTRACT of the
// executor-side predicate, whose violation is silent and expensive: root is
// meaningful ONLY with ok=true, and ok=false means "keep the directory you
// passed in" — spelled "".
//
// The regression this guards: cmd/agent's executor seam assigned the first
// return unconditionally, so every non-escaping child got an EMPTY cwd and the
// wrapper fell back to the session dir. Measured on five cmd/agent e2e walks
// (each of them green again only once the caller held its resolved directory as
// the default).
func TestEscapeFromWorktreeContract(t *testing.T) {
	base := t.TempDir()
	wt := filepath.Join(base, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /somewhere/.git/worktrees/wt\n"), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	// A NEUTRAL directory: not a linked worktree, so nothing to escape.
	neutral := t.TempDir()
	home := filepath.Join(base, "scratch")
	d := testDeps(t, home, map[string]uint64{home: 2, base: 2, "/": 1}, nil)

	for _, tc := range []struct {
		name, dir, ownGround string
	}{
		{"neutral directory", neutral, ""},
		{"the task's own worktree", wt, wt},
		{"inside the task's own worktree", filepath.Join(wt, "internal"), wt},
	} {
		root, escapedFrom, ok := escapeWith(d, tc.dir, tc.ownGround)
		if ok {
			t.Errorf("%s: must NOT escape (ok=false), got ok=true root=%q", tc.name, root)
		}
		if root != "" || escapedFrom != "" {
			t.Errorf("%s: ok=false must spell root/escapedFrom \"\" (\"keep what you passed in\") — a caller assigning root unconditionally blanks its child's cwd; got root=%q escapedFrom=%q",
				tc.name, root, escapedFrom)
		}
	}

	// The armed case: a FOREIGN linked worktree escapes and names it.
	foreign, from, ok := escapeWith(d, wt, filepath.Join(base, "someone-elses-repo"))
	if !ok {
		t.Fatalf("a foreign worktree holder must escape, got ok=false")
	}
	if from != ResolvePrefix(wt) {
		t.Errorf("the worktree left behind must be named, got %q want %q", from, ResolvePrefix(wt))
	}
	if WithinWorktree(foreign, wt) {
		t.Errorf("the escape root %q is still inside the worktree it must leave", foreign)
	}
}

// TestIsDaemonArgv0 pins the identity predicate: the daemon's argv[0] IS the
// marker, and an ordinary gopi process (whose argv[0] is a path or the binary's
// name) is not one.
func TestIsDaemonArgv0(t *testing.T) {
	cases := []struct {
		argv0 string
		want  bool
	}{
		{Argv0Prefix + "merger", true},
		{Argv0Prefix + "worker", true},
		{"/home/x/bin/gopi", false},
		{"gopi", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsDaemonArgv0(c.argv0); got != c.want {
			t.Errorf("IsDaemonArgv0(%q) = %v, want %v", c.argv0, got, c.want)
		}
	}
}

// TestProcessCwdAndParentOfReadThisProcess is the real-tree half: the two
// procfs readers must answer for a LIVE process (this test binary) — a broken
// path constant would silently disable the measurement the whole epic rests on.
func TestProcessCwdAndParentOfReadThisProcess(t *testing.T) {
	cwd, ok := ProcessCwd(os.Getpid())
	if !ok {
		t.Skip("no readable procfs — the measurement channels are unavailable here")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if ResolvePrefix(cwd) != ResolvePrefix(wd) {
		t.Errorf("ProcessCwd = %q, want this process's cwd %q", cwd, wd)
	}
	ppid, ok := ParentOf(os.Getpid())
	if !ok || ppid <= 0 {
		t.Errorf("ParentOf must read this process's parent, got %d ok=%v", ppid, ok)
	}
	if !DescendsFrom(os.Getpid(), os.Getpid()) {
		t.Errorf("a process descends from itself")
	}
}
