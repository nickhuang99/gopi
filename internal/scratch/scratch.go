// Package scratch is the single definition site for the GoPi scratch tree
// (Rule 14: a value shared across packages lives in a small leaf constant
// package, exported, with exactly one definition).
//
// WHY /tmp IS BANNED (the incident this package encodes): on this host /tmp
// lives on the 147G `/` partition, which the 2026-09-11 exhaustion took from
// 19G free to 1.5G free — an unbounded `while :; do dd ... conv=fsync; done`
// stress loop wrote there while test suites abandoned 13,044 scratch
// directories. Everything throwaway (suite scratch, dump files, load output)
// must therefore land under <home>/workspace/gopi-test-scratch, which the age
// GC bounds (internal/testutil/scratchroot.go — GCStaleScratchRoot).
//
// This package holds ONLY names + the one path join: it imports nothing from
// the project, so any package (including internal/testutil, the suite
// entry-point layer) can depend on it without a cycle.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// WorkspaceDirName is the shared workspace directory under the home — the
	// ONLY filesystem tree bulk output may land in. It is an ALIAS of
	// worktreeroots.WorkspaceDirName (Rule 14): the same directory is both the
	// default worktree root and the parent of the scratch home, so a rename
	// must be one line, not a grep-and-hope.
	WorkspaceDirName = worktreeroots.WorkspaceDirName
	// RootDirName is the fixed scratch home under the workspace: one
	// well-known directory means one GC target and one thing to delete by
	// hand in an emergency.
	RootDirName = "gopi-test-scratch"
	// QueueDirName holds suite-less, large, throwaway artifacts (load files,
	// dumps, DB copies) under the same GC as the suite scratch.
	QueueDirName = "queue"
	// TmpDirName names the ONE shared temp workspace under the scratch home.
	// It is a STABLE, shared child on purpose (not a per-process queue entry):
	// a child may outlive the gopi that published the variable (a detached
	// worker, a nohup handoff), and collecting its temp root from under it
	// would be data loss.
	//
	// It USED to be the built-in DEFAULT temp root as well; the default is now
	// DefaultTmpDirLeaf under the workspace (scratch.DefaultTmpDir), and this
	// name survives as the SUITE fixture root (internal/testutil pins TMPDIR to
	// a child of it — one directory inside the one tree the GC bounds).
	TmpDirName = "tmp"
	// DefaultTmpDirLeaf is the LEAF name of the built-in default temp root that
	// scratch.DefaultTmpDir publishes when nothing is configured — an ordinary
	// directory under the workspace (Rule 22's "prefer the explicit layer": the
	// workspace is the operator's declared working space, and the default must
	// be the obvious user-facing one).
	//
	// Why "_tmp" and not "tmp" under the workspace: the workspace also holds the
	// worktrees (its DEFAULT root is the workspace with a per-branch child,
	// worktreeroots), and a name that cannot collide with a branch or a repo
	// child keeps the two roles distinguishable by their spelling alone. The
	// leading underscore is the workspace's ONE reserved namespace.
	DefaultTmpDirLeaf = "_tmp"
	// DumpsDirName holds crash/diagnostic dumps: durable enough to inspect
	// after an incident, still inside the one tree the GC and the disk
	// accounting know about (and never on `/`).
	DumpsDirName = "dumps"

	// HomeEnvName publishes the scratch home to child processes — the ONLY
	// variable that, when set, decides where the scratch tree is (Rule 2).
	HomeEnvName = "GOPI_TEST_SCRATCH_HOME"
	// WorkspaceHomeEnvName overrides the home whose workspace holds the
	// scratch tree.
	WorkspaceHomeEnvName = "GOPI_TEST_HOME"

	// ReadmeFileName makes the tree self-describing; the GC never collects it.
	ReadmeFileName = "README.md"

	// BatchPoolDirPrefix names the pooled rsync batch-scratch directory. The
	// pool stops being per-run (and starts surviving a crashed rsync) after
	// this age, so the GC can bound it.
	BatchPoolDirPrefix = "gopi-merge-batch-"
	// BatchPoolStaleAge is how long a pooled batch directory may sit unused
	// before it counts as garbage.
	BatchPoolStaleAge = 24 * time.Hour

	// HomeDirEnvName is the ONE name of the variable that OVERRIDES the scratch
	// HOME itself (HomeEnvName), as opposed to returning to its default under
	// the home. It governs only the values this package computes from HomeDir;
	// the directory membership rules have exactly one definition each
	// (RootDirName for the home, the base-residue family constants for the
	// memory base) and are NOT overridable — a knob that could switch them off
	// would be a knob that could switch the sweep off. It is a NAME with a
	// reader (HomeDir), never a value with none (Rule 21).
	HomeDirEnvName = "GOPI_TEST_SCRATCH_HOME"
)

// homeBase resolves the HOME the workspace and the scratch home hang under:
// the suite's override ($GOPI_TEST_HOME), else the user's home. It is the ONE
// resolution both WorkspaceUnderHome and HomeDir use (Rule 2: no second
// reader of the same knob), and os.TempDir() is deliberately never consulted.
func homeBase() string {
	if base := os.Getenv(WorkspaceHomeEnvName); base != "" {
		return base
	}
	// os.UserHomeDir honours $HOME first; a failure is not fatal for a path
	// function, so the fallback chain stays explicit.
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("HOME")
}

// WorkspaceUnderHome is the workspace directory under the user's home
// (<home>/workspace) — the ONLY filesystem tree bulk output may land in, the
// default worktree root, and (since the tmpdir epic of 2026-09-16) the parent
// of the built-in DEFAULT temp root. One function so the workspace is spelled
// exactly once (Rule 1b).
func WorkspaceUnderHome() string {
	return CanonicalPath(filepath.Join(homeBase(), WorkspaceDirName))
}

// HomeDir is the scratch home: $GOPI_TEST_SCRATCH_HOME when set (the value a
// parent suite publishes), else <GOPI_TEST_HOME|~>/workspace/
// gopi-test-scratch. os.TempDir() is deliberately never consulted.
func HomeDir() string {
	if home := os.Getenv(HomeDirEnvName); home != "" {
		return home
	}
	return CanonicalPath(filepath.Join(homeBase(), WorkspaceDirName, RootDirName))
}

// CanonicalPath resolves symlinks in an EXISTING path and returns the input
// unchanged otherwise (a not-yet-created tree has nothing to resolve).
//
// Why this matters: a scratch home may be reached through a SYMLINK — the
// stopped setup script (scripts/setup-scratch-home.sh) used to link
// <home>/workspace/gopi-test-scratch → /dev/shm/gopi-test-scratch on an
// unprivileged host, and the memory backing is RETIRED (operator ruling,
// 2026-09-18) while the LINKAGE it produced may still exist on an older host.
// getcwd(2)
// — and therefore os.Getwd() — always reports the RESOLVED path, so a scratch
// path spelled through the link makes every consumer that compares a chdir'd
// cwd against its own scratch path red (cmd/agent TestResumeChdirToWorktree).
// One canonical spelling, decided here, for both writers (this package) and
// the suite's assertions (internal/testutil re-uses it).
func CanonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		return resolved
	}
	return path
}

// freeCache is the ONE memo behind CachedCanonicalPath. A sync.Map because the
// callers are concurrent sweeps (a resident tick and a CLI verb can overlap on
// one host) and the keys are a handful of tree roots.
var freeCache sync.Map // map[string]string

// CachedCanonicalPath is CanonicalPath for a HOT path — one that a sweep asks
// about once per candidate.
//
// WHY IT EXISTS (measured 2026-09-20, this host): the declared-tree sweep asks
// "is this path a declared tree root?" per candidate, and filepath.EvalSymlinks
// walks EVERY component of the path with its own lstat — so on a 75k-entry tree
// the per-candidate resolution alone cost ~100 seconds (a dry run that should
// take one). The question only ever concerns a handful of TREE ROOTS, and a
// root's resolution cannot change inside one process run (a root that MOVES
// mid-sweep is a change of the tree's identity, not a cache miss worth paying
// for on every candidate).
//
// A path that could not be resolved is NOT cached: it may become resolvable
// (the tree is created later in the run), and caching a failure would make the
// memo answer a question about a different instant (Rule 15).
func CachedCanonicalPath(path string) string {
	if cached, ok := freeCache.Load(path); ok {
		return cached.(string)
	}
	resolved := CanonicalPath(path)
	if resolved == path {
		// Either already canonical or unresolvable: cache the canonical answer
		// only when the path exists and resolves, so a later-created tree is
		// still resolved correctly.
		if _, err := os.Stat(path); err != nil {
			return resolved
		}
	}
	freeCache.Store(path, resolved)
	return resolved
}

// QueueDir is the suite-less large-artifact queue under the scratch home.
func QueueDir() string { return filepath.Join(HomeDir(), QueueDirName) }

// RuntimeDirName names the class-subdirectory layout for long-lived runtime
// scratch (job output, staged envelopes, an isolated git worktree) that must
// survive longer than one call but still never live on `/`.
const RuntimeDirName = "runtime"

// RuntimeClassJob / RuntimeClassMergeResolve / ... name the runtime scratch
// classes. They are constants (Rule 1b) because the directory name is written
// by one package and swept/reported by others.
const (
	RuntimeClassJob          = "job"
	RuntimeClassMergeResolve = "merge-resolve"
	RuntimeClassWebSearch    = "websearch"
	RuntimeClassShot         = "shot"
	// RuntimeClassText2Img names the /text2img staging scratch class.
	RuntimeClassText2Img = "text2img"
	// RuntimeClassMemsync is the staged-envelope file class.
	RuntimeClassMemsync = "memsync"
	// RuntimeClassMergeBuild is the merge-time BUILD gate's class: the
	// temporary detached worktrees + Go caches it builds a branch in
	// (internal/tools/merge_build_gate.go). It is a runtime class like any
	// other so a crashed merger's checkouts are GC-bounded and never land on
	// `/` (the 2026-09-11 exhaustion).
	RuntimeClassMergeBuild = "merge-build"
	// RuntimeClassUISession is the `gopi ui` surface's class: the per-session
	// directory its in-process engine's console output is redirected into
	// while the TUI owns the terminal (cmd/agent/cli_ui.go). A runtime class
	// like any other, so that log is age-bounded and never on `/` (Rule 22).
	RuntimeClassUISession = "ui-session"
)

// runtimeEntryPrefix marks a runtime scratch directory as sweepable: the
// queue entry name is "queue-runtime-<class>-<pid>", which the scratch GC
// (internal/testutil.isScratchEntry) recognises and age-collects — so a
// crashed process's leftovers are bounded by the same sweep as the test
// scratch, while a live process's files are protected by its open
// descriptors (the /proc/<pid>/fd holder check).
const runtimeEntryPrefix = QueueDirName + "-" + RuntimeDirName + "-"

// RuntimeScratchDir returns (and creates) this PROCESS's scratch directory for
// a runtime class: <scratch queue>/queue-runtime-<class>-<pid>. Returns "" and
// reports the reason when the tree cannot be created — the caller then falls
// back to the ambient temp root rather than failing (a runtime scratch path is
// never worth aborting the work for; it IS worth never defaulting to /tmp
// silently).
func RuntimeScratchDir(class string) string {
	if class == "" {
		class = defaultRuntimeClass
	}
	dir := filepath.Join(QueueDir(), fmt.Sprintf("%s%s-%d", runtimeEntryPrefix, class, os.Getpid()))
	if err := MkdirAllWithSuiteRunOwnerPerm(dir, DirPerm); err != nil {
		// Falling back to the ambient temp root is the ONE case this function
		// cannot avoid — so it must not be silent about WHERE that lands
		// (Rule 15: 23.5G of /tmp residue came from exactly this fallback
		// being invisible). ReportTempRootIfOnRootFS measures the effective
		// temp root and prints once per process when it is on `/`.
		fmt.Fprintf(os.Stderr, "scratch: runtime dir %s unavailable: %v (falling back to the ambient temp root)\n", dir, err)
		_ = ReportTempRootIfOnRootFS() //stylegate:ignore diagnostics-only: the fallback above already names the cause; this adds the measured device ids of the ambient root the caller is about to use and cannot change it
		return ""
	}
	// THE WRITER REGISTERS ITSELF (owner_record.go). This is the moment a per-run
	// scratch root comes into existence, so it is the moment its OWNER can be
	// recorded: pid + kernel start ticks + command line. With that record in
	// place a sweep proves the root's staleness exactly — the run that created it
	// is SIGKILLed, the record says who it was, and the next sweep collects the
	// residue the moment that process is gone instead of waiting out the whole
	// age window on a fresh mtime. A failure here is REPORTED and never fatal: the
	// registration is an extra guarantee, not the only bound (Rule 15b).
	if err := WriteOwnerRecord(dir, os.Getpid()); err != nil {
		fmt.Fprintf(os.Stderr, "scratch: owner registration in %s: %v\n", dir, err)
	}
	return dir
}

// MkdirTemp creates a unique directory inside the runtime scratch class
// (falling back to the ambient temp root when the scratch tree is
// unavailable). Every caller that used os.MkdirTemp("", ...) uses this
// instead — the /tmp ban is mechanical, not a convention.
func MkdirTemp(class, pattern string) (string, error) {
	dir := RuntimeScratchDir(class)
	if dir == "" {
		return os.MkdirTemp("", pattern) // last resort: still reported above
	}
	return os.MkdirTemp(dir, pattern)
}

// MkdirTempIn creates a unique directory INSIDE a given scratch class directory
// (the class dir a caller already resolved, e.g. via RuntimeScratchDir) and
// REGISTERS this process as its owner — the writer-side obligation of
// owner_record.go, kept in ONE place so a producer cannot skip it by reaching for
// the raw os.MkdirTemp.
//
// A class directory may hold several run roots (the merge-build gate builds more
// than one branch per run); each is its own run-owned directory and carries its
// own record.
func MkdirTempIn(classDir, pattern string) (string, error) {
	dir, err := os.MkdirTemp(classDir, pattern)
	if err != nil {
		return "", err
	}
	if err := WriteOwnerRecord(dir, os.Getpid()); err != nil {
		fmt.Fprintf(os.Stderr, "scratch: owner registration in %s: %v\n", dir, err)
	}
	return dir, nil
}

// CreateTemp creates a unique file inside the runtime scratch class (same
// fallback contract as MkdirTemp).
func CreateTemp(class, pattern string) (*os.File, error) {
	dir := RuntimeScratchDir(class)
	if dir == "" {
		return os.CreateTemp("", pattern) // last resort: still reported above
	}
	return os.CreateTemp(dir, pattern)
}

// DumpDir returns a durable dump directory for a named dump CLASS (an empty
// class falls back to the generic "gopi-dumps"), creating it. Callers that
// previously used <os.TempDir()>/<name> land in the same tree as everything
// else throwaway — inspectable, GC-bounded, and never on `/`.
func DumpDir(class string) string {
	if class == "" {
		class = defaultDumpClass
	}
	dir := filepath.Join(HomeDir(), DumpsDirName, class)
	if err := MkdirAllWithSuiteRunOwnerPerm(dir, DirPerm); err != nil {
		// Best effort: the caller's own MkdirAll reports a real failure, and
		// a dump directory is a diagnostic aid, not a correctness gate.
		return dir
	}
	return dir
}

// IsInScratchTree reports whether path is inside the scratch home — the
// question a consumer asks before claiming "this data is bounded and off /".
func IsInScratchTree(path string) bool {
	home := filepath.Clean(HomeDir())
	p := filepath.Clean(path)
	return p == home || strings.HasPrefix(p, home+string(filepath.Separator))
}

// ParseBatchPoolDir extracts (baseTag, fingerprint, ok) from a pooled batch
// directory name "<prefix><baseTag>-<fingerprint>". ok is false for a name
// that does not carry the prefix — the caller must not guess.
func ParseBatchPoolDir(name string) (base, fingerprint string, ok bool) {
	if !strings.HasPrefix(name, BatchPoolDirPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(name, BatchPoolDirPrefix)
	// The fingerprint is the trailing hex-ish run after the LAST dash: a
	// base name may itself contain dashes.
	idx := strings.LastIndex(rest, "-")
	if idx <= 0 || idx == len(rest)-1 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}

// IsBatchPoolDirStale reports whether a pooled batch directory has not been
// used for BatchPoolStaleAge.
func IsBatchPoolDirStale(modTime, now time.Time) bool {
	return now.Sub(modTime) > BatchPoolStaleAge
}

const (
	// defaultDumpClass names an unclassified dump.
	defaultDumpClass = "gopi-dumps"
	// defaultRuntimeClass names a runtime scratch dir whose caller did not
	// name a class.
	defaultRuntimeClass = "misc"
)
