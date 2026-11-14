package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// ── IsServerNode: the four decision paths, tested DIRECTLY ──────────────────
//
// WHY THIS FILE EXISTS (devdoc/assert-the-bug-audit-20260917.md §4): the
// 2026-09-16 stranded-branch incident ran through the CLIENT gate in
// task_finish_cleanup.go, and the gate is IsServerNode() — which, before this
// file, had ZERO direct assertions in the whole suite. The name was reachable
// (it appears in four test files), but only in COMMENTS, which is exactly what
// scripts/func_coverage.py counts: the metric calls IsServerNode "covered"
// while nothing asserted which branch of it decides. A node-identity predicate
// that silently flips turns a client into a node that recycles worktrees
// without pushing, or a server into one that pushes to itself — the failure
// modes this file pins.
//
// The four paths (memory_sync_delta.go:786-809):
//
//	① role-declared        config.ResolveNodeRole() non-empty → role == server (:789-790)
//	② legacy hostname      no role: own hostname among peers → client (:800-806)
//	③ fail-safe default    no role, hostname NOT among peers, or os.Hostname (:797-798, :809)
//	                       failed → CLIENT (the N1 fix: a client that thinks it
//	                       is a server merges the central repo — overreach is
//	                       cheap in the other direction)
//	④ unreadable settings  ResolveSyncPeers errors → CLIENT, never a panic (:801-803)
//
// Every test pins ONE path and names why the other three cannot be the decider
// (the negative half matters as much: a role test that passes because the
// hostname happens to be absent proves nothing).

// isServerNodeSettingsEnv is the settings-file override this file uses to make
// the peer resolution fail. internal/config reads the raw env name without
// exporting a constant (config.ResolveSettingsPath), so the spelling lives
// here once rather than at each t.Setenv call.
const isServerNodeSettingsEnv = "GOPI_SETTINGS"

// selfHostnameOrTest is the hostname the legacy hostname arm compares against.
// A host without one falls back to a name the peer-planting tests use, which
// keeps the case meaningful instead of vacuously failing.
func selfHostnameOrTest(t *testing.T) string {
	t.Helper()
	self, err := os.Hostname()
	if err != nil || strings.TrimSpace(self) == "" {
		return "gopi-test-client"
	}
	return self
}

// isServerNodeGit runs one git command in dir (the matrix cell's probe).
func isServerNodeGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// isServerNodeCase resets the process-wide memoised identity around a case
// (ResolveNodeRole caches once per process — the covLifeSetRole idiom).
func isServerNodeCase(t *testing.T) {
	t.Helper()
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
}

// TestIsServerNodeRoleDeclaredServer: ① with role=server the answer is TRUE
// regardless of the peer list — the declared topology is the primary source.
func TestIsServerNodeRoleDeclaredServer(t *testing.T) {
	isServerNodeCase(t)
	t.Setenv(config.RoleEnv, string(config.NodeRoleServer))
	// A peer list that CONTAINS this hostname: under ② that alone would make
	// this node a client. The role must win, or the arm order is wrong.
	t.Setenv(config.SyncPeersEnv, selfHostnameOrTest(t))
	if !IsServerNode() {
		t.Fatalf("role=%s must make this node the server even with its own name in sync_peers", config.NodeRoleServer)
	}
}

// TestIsServerNodeRoleDeclaredClient: ① with role=client the answer is FALSE,
// including when the peer list would ALSO say client (they agree) and when it
// would say nothing at all (the role alone decides).
func TestIsServerNodeRoleDeclaredClient(t *testing.T) {
	t.Run("own name among peers", func(t *testing.T) {
		isServerNodeCase(t)
		t.Setenv(config.RoleEnv, string(config.NodeRoleClient))
		t.Setenv(config.SyncPeersEnv, selfHostnameOrTest(t))
		if IsServerNode() {
			t.Fatal("role=client must never be the server")
		}
	})
	t.Run("no peers declared", func(t *testing.T) {
		isServerNodeCase(t)
		t.Setenv(config.RoleEnv, string(config.NodeRoleClient))
		t.Setenv(config.SyncPeersEnv, "")
		// The arm ① must still answer: an empty peer list is not an excuse to
		// fall through to the legacy hostname arm.
		if IsServerNode() {
			t.Fatal("role=client with no peers must still be a client")
		}
	})
}

// TestIsServerNodeLegacyHostnameAmongPeers: ② with no declared role, this
// host's name appearing in sync_peers means CLIENT (the server's name is not
// in its own peer list).
func TestIsServerNodeLegacyHostnameAmongPeers(t *testing.T) {
	isServerNodeCase(t)
	t.Setenv(config.RoleEnv, "")
	t.Setenv(config.SyncPeersEnv, selfHostnameOrTest(t)+",another-node")
	if IsServerNode() {
		t.Fatal("hostname in its own sync_peers means client (the server is not its own peer)")
	}
}

// TestIsServerNodeHostnameNotAmongPeersIsFailSafeClient is the N1 half — the
// fail-safe that a real incident bought (2026-09-01: a host missing from the
// alias list claimed server rights and merged the central repo). A hostname
// NOT in the peer list is CLIENT, not server: this is the path that decides on
// every undeclared host, so it is asserted directly rather than inferred.
func TestIsServerNodeHostnameNotAmongPeersIsFailSafeClient(t *testing.T) {
	isServerNodeCase(t)
	t.Setenv(config.RoleEnv, "")
	t.Setenv(config.SyncPeersEnv, "some-other-node,another")
	if IsServerNode() {
		t.Fatal("an undeclared host whose name is absent from sync_peers must default to CLIENT (fail-safe)")
	}
}

// TestIsServerNodeUnknownRoleSpellingIsUndeclared: a typo in the role field
// ("serer") resolves to the empty role (never guessed) and falls through to
// the fail-safe default. This is the path a settings.json hand-edit takes.
func TestIsServerNodeUnknownRoleSpellingIsUndeclared(t *testing.T) {
	isServerNodeCase(t)
	t.Setenv(config.RoleEnv, "serer")
	t.Setenv(config.SyncPeersEnv, "")
	if role, _ := config.ResolveNodeRole(); role != "" {
		t.Fatalf("an unknown role spelling must resolve to the undeclared role, got %q", role)
	}
	if IsServerNode() {
		t.Fatal("an unknown role spelling must not be read as server (fail-safe client)")
	}
}

// TestIsServerNodeUnreadableSettingsIsClient: ④ an unreadable settings file
// (the peer list cannot be resolved) answers CLIENT rather than propagating an
// error — the predicate has no error return, so the only acceptable postures
// are "fail-safe" and "panic", and panic is not one of them.
func TestIsServerNodeUnreadableSettingsIsClient(t *testing.T) {
	isServerNodeCase(t)
	t.Setenv(config.RoleEnv, "")
	t.Setenv(config.SyncPeersEnv, "") // env empty → the settings file is consulted
	// The path must EXIST and be unreadable: an ABSENT settings file is a
	// legitimate empty config (FromSettingsFile returns defaults, no error),
	// so the only way to reach the error arm is a path that cannot be read as
	// a file — a directory. (Measured: this comment records why the obvious
	// "point it at a missing path" fixture does NOT reach arm ④.)
	unreadable := filepath.Join(t.TempDir(), "settings-as-a-directory")
	if err := os.Mkdir(unreadable, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(isServerNodeSettingsEnv, unreadable)
	// Document that the arm was really reached: the peers resolution fails.
	if _, err := config.ResolveSyncPeers(nil); err == nil {
		t.Fatal("fixture: an unreadable settings path must make the peers resolution fail")
	}
	if IsServerNode() {
		t.Fatal("an unresolvable peer list must answer CLIENT (fail-safe), never SERVER")
	}
}

// TestIsServerNodeRoleDeclaredIsStableAcrossCalls pins the memoisation
// contract the whole predicate rests on: ResolveNodeRole caches per process,
// so the answer cannot flip mid-run (a flipping gate would recycle a worktree
// on one pass and strand a branch on the next).
func TestIsServerNodeRoleDeclaredIsStableAcrossCalls(t *testing.T) {
	isServerNodeCase(t)
	t.Setenv(config.RoleEnv, string(config.NodeRoleServer))
	t.Setenv(config.SyncPeersEnv, "")
	first := IsServerNode()
	for i := 0; i < 5; i++ {
		if got := IsServerNode(); got != first {
			t.Fatalf("IsServerNode flipped mid-process: %v → %v on call %d", first, got, i+1)
		}
	}
}

// ── the ROLE × STATE matrix cell the incident exposed ───────────────────────
//
// The close-out chain's arm choice is ROLE (IsServerNode) × STATE (clean /
// dirty). The suite covers:
//
//	client + clean   TestFinishCleanupChainCleanWorktree                 (push+remove+-d)
//	client + dirty   TestFinishCleanupClientDirtyStillDelivers — THIS CELL,
//	                 covered since the client-push-rebase epic (2026-09-19):
//	                 the destructive steps stay refused (the directory
//	                 survives) and the idempotent PUSH still runs.
//	                 NARROWED 2026-10-07 (epic 01a11548): the cell now splits on
//	                 the KIND of dirt — UNTRACKED residue is rescued and the
//	                 ground reclaimed (this test), TRACKED uncommitted work keeps
//	                 the directory (see TestClientDeliverySurvives…TrackedWork).
//	server + clean   TestCovFinishCleanupLinkedWorktreeRemovedBranchKept
//	server + dirty   TestServerRecycleDirtyWorktreeSkipped
//
// The cell was MISSING until then: the implementation returned Skipped before
// the push, so the body below was unreachable behind a t.Skip that named its
// owner (task 01a0aab8) and the gap announced itself in every
// `go test ./internal/tools/` run instead of silently passing. That task's fix
// landed here, so the skip is gone and the cell is asserted directly. The
// recorded shape of the gap is kept in the comment rather than the code:
//
//	"MISSING ROLE×STATE CELL (client+dirty): the destructive steps stay refused
//	 but the idempotent push must still run — the chain returns Skipped BEFORE
//	 the push, so this test is RED against master" (devdoc/assert-the-bug-audit-20260917.md §4)
func TestFinishCleanupClientDirtyStillDelivers(t *testing.T) {
	f := newFinishCleanupFixture(t)
	if err := os.WriteFile(filepath.Join(f.wtDir, "wip.txt"), []byte("live bond writing"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := f.runChain()
	// The DIRT is rescued before anything is removed: the leftover file is
	// retrievable from the rescue ref, and the ground is reclaimed (the arm the
	// epic exists for — a finished run's scratch no longer parks the directory).
	if _, err := os.Stat(f.wtDir); err == nil {
		t.Fatalf("untracked residue must be RESCUED and the ground reclaimed: %s", finishCleanupReportLine(rep))
	}
	salvage := lastStepNamed(t, rep, finishCleanupSalvageStep)
	if !salvage.OK || !strings.Contains(salvage.Detail, finishCleanupSalvageRefPrefix) {
		t.Errorf("the rescue must be recorded and name its ref: %+v", salvage)
	}
	if got := preMergeGit(t, f.clientDir, "cat-file", "-p", finishCleanupSalvageRef("test", f.wtDir)+":wip.txt"); got != "live bond writing" {
		t.Errorf("the rescued file must be retrievable, got %q", got)
	}
	// The delivery half must NOT be refused: the push is idempotent and moves
	// only a PEER ref, so the branch reaches the server even when the
	// directory was dirty (this is the whole incident).
	if !serverHasRefRuns(t, f.serverDir, f.featBranch) {
		t.Errorf("the local branch must survive the recyclable ground (the branch step keeps it)")
	}
	if out, err := isServerNodeGit(f.serverDir, "rev-parse", "--verify", f.featBranch); err != nil {
		t.Errorf("the finished branch must be DELIVERED even from a dirty worktree (push is idempotent): %v %s\nreport: %+v",
			err, out, rep)
	}
	// The row reads SETTLED-BY-ACTION: the delivery is what was owed, the ground
	// was reclaimed once its residue became retrievable, and no refusal is left
	// standing — the classifier's "removed" arm, not the "refused" one.
	if !finishCleanupReportRemovedWorktree(rep) {
		t.Errorf("a delivered row whose ground was reclaimed must read as removed, got %q", rep.SkipReason)
	}
}
