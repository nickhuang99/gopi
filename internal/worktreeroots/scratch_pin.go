package worktreeroots

// scratch_pin.go — A PROCESS THAT IS A TEST NEVER RESOLVES THE OPERATOR'S REAL
// WORKTREE ROOT.
//
// THE DEFECT THIS CLOSES (measured 2026-10-01, epic test-worktree-root-leak):
// 174 orphan directories in the operator's REAL ~/workspace, each holding
// nothing but a `.git` POINTER whose gitdir had died with the /dev/shm suite run
// that created it. The producer was PRODUCTION code, run from a test:
//
//	internal/agent's reframe fixtures (reframeGroundFixture) call
//	tools.TaskRunGroundFor → taskGroundDirFor → config.ResolveRepoWorktreeRoot
//	→ worktreeroots.Resolve("") → DefaultRoot() = $HOME/workspace.
//
// t.TempDir() isolated the FIXTURE REPO (the fixture passes the repo path as an
// argument), but the GROUND is deliberately placed "under that repo's declared
// worktree_root" — the production placement rule — and a repo whose entry
// declares none gets the DEFAULT root, which is the operator's own workspace. So
// the suite minted real worktrees in a real user directory, and every
// `git worktree add` whose gitdir lived under the (deleted) test scratch left a
// pointer-nothing stub behind. Measured while this epic ran: each run of the two
// affected tests added three stubs, count 185 → 186 → 189 → 192.
//
// WHY THE FIX IS HERE — IN THE DEFINITION PACKAGE, AS A BRANCH IN THE RESOLVER —
// AND NOT AS A PIN AT EACH CALL SITE: Rule 14. This package IS the ONE
// definition of every root a gopi agent may create work in, and DefaultRoot is
// the ONE funnel every consumer goes through (config.ResolveRepoWorktreeRoot,
// the spawn seam's taskGroundDirFor, daemoncwd, the merger's build-cache gate).
// A pin installed at the funnel cannot be FORGOTTEN by the next caller — which
// is exactly how this defect survived: testutil.MockEnv pins the env for the
// SUBPROCESS path, while the IN-PROCESS producers were never wired, because
// "remember to call the pin" is not a mechanism.
//
// WHY THE HOOK IS INSTALLED BY THE TEST TREE AND NEVER BY PRODUCTION: this
// package is a LEAF that production links, and internal/testutil imports
// "testing" — a production import of it would drag the test framework into the
// shipped binary (the precedent documented in internal/scratch/holder.go and
// internal/scratch/tmproot.go). The direction is therefore INVERTED: this
// package holds a PROVIDER (nil in the shipped binary), and the test tree
// installs it from the suite entry point. `TestUnderTestRootProviderIsInstalled`
// fails when the provider is missing, so the branch cannot be silently unwired —
// the discipline Rule 21 applies to an env-name constant with no reader.
//
// PRECEDENCE IS PRESERVED (Rule 2): the provider is consulted ONLY by the
// DEFAULT branch. An explicit root — GOPI_WORKTREE_ROOT, or a repo's declared
// `worktree_root` — still wins, so a test that pins its own root keeps it and a
// sandbox that declares one keeps it too. And when no HOME can be resolved there
// is no operator root to protect, so the pre-existing abstention (ErrNoHome)
// stands unchanged — which is also what keeps `HOME=""` meaning "the resolver
// must abstain" for the callers that test exactly that.

import (
	"path/filepath"
	"strings"
	"sync"
)

const (
	// UnderTestRootLeaf is the directory a test process's worktree root lives
	// under. It is a NAME, never a path (Rule 22: the location is MEASURED —
	// the pinned scratch root's device decides whether the pin may engage).
	UnderTestRootLeaf = "gopi-worktree-root"
	// SourceUnderTest labels a root that came from the test pin: REPORTED, never
	// silent, so a reader of a refusal can tell a test root from a declared one
	// (the SourceDefault/SourceSettingsEnv precedent).
	SourceUnderTest = "test scratch pin"
)

var (
	// underTestRootFn is the provider the test tree installs: nil in the shipped
	// binary, which is the point (production carries no branch at all).
	underTestRootFnMu sync.Mutex
	underTestRootFn   func() string
	// underTestRootOnce/underTestRootDir memoise the provider's answer: the
	// resolver is called per row, and a per-call os.MkdirTemp would mint a NEW
	// root per call — each one a leak of its own.
	underTestRootOnce sync.Once
	underTestRootDir  string
)

// ProvideUnderTestRoot installs the test tree's root provider and drops the
// memoised answer, so the next resolution asks the new provider. A provider that
// returns "" means "no redirect" — the caller keeps the ordinary behaviour.
//
// It is called once per test binary from internal/testutil's suite entry point
// (PinSuiteForTestMain), so EVERY suite gets the protection without any
// individual test asking for it. A nil provider is ignored: a caller that cannot
// answer must not disable an installed one.
func ProvideUnderTestRoot(f func() string) {
	if f == nil {
		return
	}
	underTestRootFnMu.Lock()
	underTestRootFn = f
	underTestRootFnMu.Unlock()
	underTestRootOnce = sync.Once{}
	underTestRootDir = ""
}

// UnderTestRoot returns the root a TEST PROCESS must place worktrees under, or
// "" when this process is not a test (no provider installed) or when the
// provider could not name a legal one. DefaultRoot consults it — so an
// in-process producer cannot reach the operator's workspace even though nothing
// at its call site mentions tests.
func UnderTestRoot() string {
	underTestRootFnMu.Lock()
	fn := underTestRootFn
	underTestRootFnMu.Unlock()
	if fn == nil {
		return ""
	}
	underTestRootOnce.Do(func() {
		underTestRootDir = strings.TrimSpace(fn())
	})
	return underTestRootDir
}

// UnderTestRootPath builds the pinned root under base: <base>/<leaf>. Named once
// so the producer (internal/testutil) and every reader agree on the shape
// (Rules 13/14).
func UnderTestRootPath(base string) string {
	return filepath.Join(base, UnderTestRootLeaf)
}
