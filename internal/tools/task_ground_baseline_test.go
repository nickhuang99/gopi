package tools

// task_ground_baseline_test.go — the ground's BASE COMMIT: aligned to the
// peer's tip when the peer is there, and EXPLICITLY degraded when it is not
// (epic worktree-base-align, 2026-09-26).
//
// Both tests drive the REAL production surface (TaskRunGroundFor — the same
// entry the spawn seam calls, cmd/agent/cli_tasks.go) against a REAL two-repo
// world, and both read PHYSICAL truth: the commit the new worktree's HEAD
// resolves to, and whether the main checkout moved. A test that only asserted
// "a directory appeared" would pass for the defect this epic removes.
import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/worktreeroots"
)

// The three fixture rows' ids, and they MUST be full uuids.
//
// The ground's NAME is derived from the row's id — the resolution refuses an id
// that is not a full uuid, by name, BEFORE anything is created
// (taskGroundNameRefusal; epic worktree-naming-uuid, commit 19caf9de9). A row
// carrying a readable slug therefore has no ground at all, which is what these
// fixtures used to be saddled with: the refusal fired and the three tests below
// asserted nothing about peer-fetch semantics. Distinct values, because the
// stem IS the row's own id: two rows sharing an id would share one ground.
const (
	groundBaselinePeerRow   = "01a11500-0000-7000-8000-000000000031"
	groundBaselineStaleRow  = "01a11500-0000-7000-8000-000000000032"
	groundBaselineServerRow = "01a11500-0000-7000-8000-000000000033"
	groundBaselineDeltaRow  = "01a11500-0000-7000-8000-000000000034"
)

// groundBaselineFixture builds the world both tests need:
//
//	server  a bare repo (the PEER) whose integration branch the test moves,
//	client  a clone of it (the task's deliverable.repo),
//	row     a submitted task row naming the client as its repo.
//
// The fetch target is rewritten to a LOCAL PATH through git's own `insteadOf`,
// which is what keeps the test network-free AND keeps production code
// unmodified: the production target renderer still composes `<alias>:<path>`
// (finishCleanupServerTarget), so the seeded command really is the one a
// client host with a declared peer runs — an alias no ssh_config on this
// machine would resolve would make the "aligned" case untestable offline.
func groundBaselineFixture(t *testing.T, taskID, peerAlias string) (repo, server string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	server = filepath.Join(root, "peer-repo")
	repo = filepath.Join(root, "client")

	peerFetchGit(t, root, "init", "--bare", "-b", IntegrationBranchName, server)
	peerFetchGit(t, root, "clone", server, repo)
	peerFetchGit(t, repo, "config", "user.email", "t@t")
	peerFetchGit(t, repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	peerFetchGit(t, repo, "add", "base.txt")
	peerFetchGit(t, repo, "commit", "-m", "base")
	peerFetchGit(t, repo, "push", "origin", IntegrationBranchName)

	// The task's own outcome directory + ledger, both sandboxed: a ground test
	// must never touch the operator's profile (Rule 32).
	t.Setenv(worktreeroots.WorktreeRootEnvName, t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))

	// The host's declared topology: a CLIENT with exactly one peer. Written
	// through the ONE settings reader (config.ResolveSettingsPath), so what the
	// production code resolves is this file and not the operator's.
	settings := filepath.Join(root, "settings.json")
	body, err := json.Marshal(map[string]any{
		"role":       string(config.NodeRoleClient),
		"sync_peers": []string{peerAlias},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settings)
	t.Setenv(config.RoleEnv, string(config.NodeRoleClient))
	t.Setenv("GOPI_SYNC_PEERS", peerAlias)
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)

	// NOTE: the peer alias's URL rewrite (git's `insteadOf`) is installed by
	// each TEST, never here. Two rewriting rules that match one URL are
	// resolved by git's own preference order, so a fixture that installed an
	// "aligned" rule up front would silently defeat a test that wants the
	// alias UNREACHABLE (measured on the first run of this file: the aligned
	// rule won, and the "unreachable peer" test's peer was reachable).

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "ground baseline fixture " + taskID, Payload: "do the work", Repo: repo, TaskID: taskID,
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	return repo, server
}

// groundBaselinePointPeerAt rewrites the declared peer alias to `dest` through
// git's own URL rewrite (url.<dest>.insteadOf), which is what makes these tests
// network-free: the host's peer target is an ssh URL (config.ResolveSyncPeers →
// finishCleanupServerTarget composes `<alias>:<path>`), and a fetch of it would
// need a peer machine and credentials.
//
// The rewritten prefix is READ FROM PRODUCTION
// (finishCleanupServerTargetForRepo, the face task_ground_baseline.go itself
// resolves with), never re-typed here: the path half of that target is
// production's business, and a test that spelled it independently would stop
// rewriting anything the moment production changed it — silently testing the
// unreachable branch while believing it tested the aligned one (measured on
// this file's first run: a value written from an older spelling no longer
// matched, and git fell through to ssh).
//
// A second rewrite rule for the same URL is resolved by git's own preference
// order, so callers install exactly ONE — the fixture installs none.
func groundBaselinePointPeerAt(t *testing.T, repo, dest string) {
	t.Helper()
	target := finishCleanupServerTargetForRepo(repo).Rendered
	if strings.TrimSpace(target) == "" {
		t.Fatalf("no peer target is composed from the fixture's settings — the test would prove nothing")
	}
	peerFetchGit(t, repo, "config", "url."+dest+".insteadOf", target)
}

// groundBaselineLocalMaster reads the client repo's own integration branch,
// resolved THROUGH THE ROW'S REPO — the baseline a pre-epic ground would use.
func groundBaselineLocalMaster(t *testing.T, repo string) string {
	t.Helper()
	return peerFetchGit(t, repo, "rev-parse", IntegrationBranchName)
}

// TestTaskRunGroundCutsTheBaseFromThePeerTip is the FEATURE's acceptance: the
// new worktree's base commit IS the peer's integration-branch tip, and it is
// demonstrably NOT the local one.
//
// The fixture makes the two differ ON PURPOSE (the peer's master is advanced
// after the clone): a base taken from the local HEAD would then be a commit
// that does not even contain the peer's work, which is the lag this epic
// removes. The three readings are independent — the tip sha equality, the
// containment of the peer tip in the new branch, and the main checkout's
// master staying put (Rule 29).
func TestTaskRunGroundCutsTheBaseFromThePeerTip(t *testing.T) {
	const peerAlias = "peer"
	repo, server := groundBaselineFixture(t, groundBaselinePeerRow, peerAlias)
	groundBaselinePointPeerAt(t, repo, server)
	local := groundBaselineLocalMaster(t, repo)

	// Move the PEER ahead of this host's local integration branch.
	adv := filepath.Join(filepath.Dir(server), "advance")
	peerFetchGit(t, filepath.Dir(server), "clone", server, adv)
	peerFetchGit(t, adv, "config", "user.email", "t@t")
	peerFetchGit(t, adv, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(adv, "peer.txt"), []byte("peer work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	peerFetchGit(t, adv, "add", "peer.txt")
	peerFetchGit(t, adv, "commit", "-m", "peer work")
	peerFetchGit(t, adv, "push", "origin", IntegrationBranchName)
	peerTip := peerFetchGit(t, server, "rev-parse", IntegrationBranchName)
	if peerTip == local {
		t.Fatal("fixture: the peer tip must differ from the local master, or this test proves nothing")
	}

	got, err := TaskRunGroundFor(groundBaselinePeerRow)
	if err != nil {
		t.Fatalf("TaskRunGroundFor: %v", err)
	}
	if !got.Created || got.Dir == "" {
		t.Fatalf("the ground was not created: %+v", got)
	}
	// ① The base commit is exactly the peer's tip.
	if head := peerFetchGit(t, got.Dir, "rev-parse", "HEAD"); head != peerTip {
		t.Fatalf("the ground's HEAD is %s, want the PEER's tip %s (the local master is %s); Detail=%q", head, peerTip, local, got.Detail)
	}
	// ② …and it CONTAINS it (a rebase or a merge of something else would give a
	//    different commit that still passes ① only by accident of the fixture).
	if out := peerFetchGit(t, got.Dir, "merge-base", "--is-ancestor", peerTip, "HEAD"); out != "" {
		t.Errorf("merge-base --is-ancestor output: %q", out)
	}
	// ③ The mirror ref the tip came from is durable (a ref, not FETCH_HEAD):
	//    the close-out's peer resolution reads this ref.
	if mirror := peerFetchGit(t, repo, "rev-parse", gitPushbackCheckRef); mirror != peerTip {
		t.Errorf("%s = %s, want the peer tip %s", gitPushbackCheckRef, mirror, peerTip)
	}
	// ④ Rule 29: the fetch did NOT move the main checkout's own branch.
	if after := groundBaselineLocalMaster(t, repo); after != local {
		t.Fatalf("the client's own master moved %s → %s — a ground must never pull into the shared checkout", local, after)
	}
	if st := peerFetchGit(t, repo, "status", "--porcelain"); st != "" {
		t.Errorf("the main checkout is dirty after grounding: %q", st)
	}
	// ⑤ The aligned note is handed to the operator (the same channel that
	//    carries the degradation): an operator must be able to read the base
	//    off the resolution, not infer it.
	if !strings.Contains(got.Detail, peerAlias) || !strings.Contains(got.Detail, "baseline cut from the peer") {
		t.Errorf("the resolution must state the base it cut from; got %q", got.Detail)
	}
	if strings.Contains(got.Detail, taskGroundBaselineStaleNoteFmt[:20]) {
		t.Errorf("a successful alignment must not carry the stale-baseline note: %q", got.Detail)
	}
}

// TestTaskRunGroundDegradesExplicitlyWhenThePeerIsUnreachable is the FEATURE's
// other half, and the one Rule 15 governs: an unreachable peer must NOT stop
// the run and must NOT be papered over. The ground is still created, from the
// LOCAL baseline, and the reason is named in the resolution — with git's own
// stderr, so the reader learns WHY the peer was unreachable.
func TestTaskRunGroundDegradesExplicitlyWhenThePeerIsUnreachable(t *testing.T) {
	const peerAlias = "deadpeer"
	repo, _ := groundBaselineFixture(t, groundBaselineStaleRow, peerAlias)
	// Point the alias at a path that is not a repository: deterministic, offline
	// and locally diagnosable (an unresolvable HOSTNAME would depend on the
	// machine's DNS and ssh setup, and would test those instead).
	peerFetchGit(t, repo, "config", "url."+filepath.Join(t.TempDir(), "nowhere")+".insteadOf", peerAlias+":~/work/GoPi")
	local := groundBaselineLocalMaster(t, repo)

	got, err := TaskRunGroundFor(groundBaselineStaleRow)
	if err != nil {
		t.Fatalf("an unreachable peer must NOT refuse the run (a down peer cannot be a single point of failure for local work): %v", err)
	}
	if !got.Created || got.Dir == "" {
		t.Fatalf("the ground must still be created on the degraded path; got %+v", got)
	}
	// The run still starts — from the LOCAL baseline.
	if head := peerFetchGit(t, got.Dir, "rev-parse", "HEAD"); head != local {
		t.Fatalf("the degraded ground's HEAD is %s, want the local baseline %s", head, local)
	}
	// And the degradation is NAMED (Rule 15) — the note, not merely a
	// directory: this is the assertion that would have caught a silent
	// fallback to local master.
	if !strings.Contains(got.Detail, "baseline may be stale") {
		t.Errorf("the resolution must carry the stale-baseline note; got %q", got.Detail)
	}
	if !strings.Contains(got.Detail, peerAlias) {
		t.Errorf("the note must name the peer it could not reach; got %q", got.Detail)
	}
	if !strings.Contains(got.Detail, "still created from the LOCAL") {
		t.Errorf("the note must say the ground was still created; got %q", got.Detail)
	}
	// git's own words travel with it (Rule 15: reported BY NAME, including
	// stderr) — the reader can tell a missing repo from a refused ssh host.
	if !strings.Contains(got.Detail, "the peer fetch failed") {
		t.Errorf("the note must carry the reason git gave, not just the fact of a failure; got %q", got.Detail)
	}
	// The whole note is ONE log line (git's stderr is multi-line and this text
	// is interpolated into a run log), and it is bounded by the package's
	// existing tail helper — so assert the bound too, or a future multi-line
	// error would silently split the operator's line.
	if strings.Contains(got.Detail, "\n") {
		t.Errorf("the degradation note must stay on one line; got %q", got.Detail)
	}
	// The main checkout is untouched on this path too.
	if after := groundBaselineLocalMaster(t, repo); after != local {
		t.Fatalf("the client's own master moved %s → %s", local, after)
	}
}

// TestTaskRunGroundSkipsThePeerFetchOnTheServerRole: the SERVER does not fetch
// itself (Rule 28's asymmetry). Its own integration branch IS the freshest
// baseline in the topology, so it aligns to nothing and reports nothing — and
// in particular it must not attempt a fetch that would fail against a peer
// entry naming a node this host is not.
func TestTaskRunGroundSkipsThePeerFetchOnTheServerRole(t *testing.T) {
	const peerAlias = "deadpeer"
	repo, _ := groundBaselineFixture(t, groundBaselineServerRow, peerAlias)
	peerFetchGit(t, repo, "config", "url."+filepath.Join(t.TempDir(), "nowhere")+".insteadOf", peerAlias+":~/work/GoPi")
	local := groundBaselineLocalMaster(t, repo)

	// Flip this process to the server role through the ONE resolver.
	t.Setenv(config.RoleEnv, string(config.NodeRoleServer))
	config.ResetNodeIdentityForTest()

	got, err := TaskRunGroundFor(groundBaselineServerRow)
	if err != nil {
		t.Fatalf("the server role must ground normally: %v", err)
	}
	if head := peerFetchGit(t, got.Dir, "rev-parse", "HEAD"); head != local {
		t.Fatalf("the server's ground must start at its OWN integration branch; HEAD=%s want %s", head, local)
	}
	if strings.Contains(got.Detail, "baseline may be stale") {
		t.Errorf("the server declares no peer to be out of step with — the note must not appear; got %q", got.Detail)
	}
	if strings.Contains(got.Detail, "baseline cut from the peer") {
		t.Errorf("the server must not claim a peer baseline; got %q", got.Detail)
	}
}

// TestTaskRunGroundTurnsAPeerHeadsupIntoItsOwnDiff is the REGRESSION GUARD for
// the 2026-10-12 report "a worker ground whose base is NOT plain master"
// (Rule 41 evidence, task 01a12d18): the ground was cut from the PEER's tip
// (0b3c1de29), which was 29 commits AHEAD of the host's local master
// (1508e3821), so `git diff --stat master...HEAD` printed 12 files of which
// only 6 were the run's own work. The measurement in that report is
// gitPushcheck/master@{2026-10-12 05:52:52}=1508e3821,
// @{2026-10-12 06:16:45}=0b3c1de29, @{2026-10-12 06:45:32}=b8287d5c6 — the
// mirror ref moved twice inside 53 minutes, so the base was a SNAPSHOT of a
// moving tip, not a wrong choice of ref.
//
// What this pins: the foreign files a closer sees are a HEADS-UP the run did
// not write, not a defect in the ground — and the property that makes the
// three-dot reading correct is `merge-base(local master, HEAD) == HEAD`, i.e.
// the ground's base is an ANCESTOR of the host's own integration branch, which
// is exactly when `master...HEAD` measures the ground's own work. The guard
// fails LOUDLY (naming the peer tip that was skipped) if the base ever leaves
// that chain: a ground cut from a tip that is not ahead of the local branch —
// a forked or rewound peer — would make the same reading lie in the other
// direction (it would hide a conflict instead of only showing it early).
func TestTaskRunGroundTurnsAPeerHeadsupIntoItsOwnDiff(t *testing.T) {
	const peerAlias = "peer"
	repo, server := groundBaselineFixture(t, groundBaselineDeltaRow, peerAlias)
	groundBaselinePointPeerAt(t, repo, server)

	// The PEER advances, exactly as zbook's master did while the host slept.
	adv := filepath.Join(filepath.Dir(server), "advance")
	peerFetchGit(t, filepath.Dir(server), "clone", server, adv)
	peerFetchGit(t, adv, "config", "user.email", "t@t")
	peerFetchGit(t, adv, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(adv, "foreign.txt"), []byte("landed while the host slept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	peerFetchGit(t, adv, "add", "foreign.txt")
	peerFetchGit(t, adv, "commit", "-m", "a row that landed on the peer after this host last pulled")
	peerFetchGit(t, adv, "push", "origin", IntegrationBranchName)

	localBefore := groundBaselineLocalMaster(t, repo)
	peerTip := peerFetchGit(t, server, "rev-parse", IntegrationBranchName)
	if peerTip == localBefore {
		t.Fatal("fixture: the peer tip must differ from the local master, or this test proves nothing")
	}

	got, err := TaskRunGroundFor(groundBaselineDeltaRow)
	if err != nil {
		t.Fatalf("TaskRunGroundFor: %v", err)
	}
	if !got.Created || got.Dir == "" {
		t.Fatalf("the ground was not created: %+v", got)
	}

	// ① THE PROPERTY the local-master reading depends on: the ground's base is
	//    an ANCESTOR of this host's own integration branch. It is the peer tip,
	//    and the local master has not moved (Rule 29) — so the base is ahead
	//    of it, never beside it.
	base := peerFetchGit(t, got.Dir, "rev-parse", "HEAD")
	if base != peerTip {
		t.Fatalf("the ground's base is %s, want the peer tip %s", base, peerTip)
	}
	if groundBaselineLocalMaster(t, repo) != localBefore {
		t.Fatalf("the ground moved the client's own master; the reading below would then be meaningless")
	}
	if out, err := exec.Command("git", "-C", got.Dir, "merge-base", "--is-ancestor",
		localBefore, "HEAD").CombinedOutput(); err != nil {
		// The named failure: the base has left the local branch's chain, and the
		// foreign files a closer reads are NO LONGER a plain heads-up.
		t.Fatalf("the ground's base %s is NOT an ancestor of the local %s: `git diff %s...HEAD` would contain commits that CANNOT be merged, and the head start this epic exists for is gone: %v: %s",
			base, IntegrationBranchName, IntegrationBranchName, err, out)
	}
	// …and, with the property holding, the three-dot reading IS the ancestor's
	// own delta: the foreign file is the PEER's, and the ground's own delta
	// against its base names nothing foreign.
	if out := peerFetchGit(t, got.Dir, "merge-base", localBefore, "HEAD"); out != localBefore {
		t.Fatalf("merge-base(%s, HEAD) = %s, want the local master %s — the fork point IS the local master, which is what makes the heads-up readable", IntegrationBranchName, out, localBefore)
	}
	if stat := peerFetchGit(t, got.Dir, "diff", "--stat", "HEAD"); stat != "" {
		t.Errorf("a fresh ground's delta against its OWN base must be empty; got %q", stat)
	}

	// THE GUARD'S OWN RED PATH, demonstrated on a real commit rather than
	// asserted: a peer whose integration branch was REWOUND or FORKED (a tip
	// that does NOT contain the local branch) is a base for which the same
	// three-dot reading misreports — it would HIDE work that cannot merge
	// instead of only showing a head start. So the predicate asserted above
	// must reject that tip. Without this block the guard could be vacuous on a
	// machine where every peer tip happens to descend from the local branch.
	peerFetchGit(t, adv, "checkout", "--orphan", "forked")
	if err := os.WriteFile(filepath.Join(adv, "divergent.txt"), []byte("a peer line this host never had\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	peerFetchGit(t, adv, "add", "divergent.txt")
	peerFetchGit(t, adv, "commit", "-m", "a peer master that does NOT contain this host's branch")
	forked := peerFetchGit(t, adv, "rev-parse", "HEAD")
	peerFetchGit(t, adv, "push", "--force", "origin", "forked:refs/heads/"+IntegrationBranchName)
	if forkTip := peerFetchGit(t, server, "rev-parse", IntegrationBranchName); forkTip != forked {
		t.Fatalf("fixture: the peer's rewound tip is %s, want %s", forkTip, forked)
	}
	if err := exec.Command("git", "-C", got.Dir, "merge-base", "--is-ancestor",
		localBefore, forked).Run(); err == nil {
		t.Fatal("the guard's predicate does not distinguish a FORKED peer tip from the aligned one — every assertion above could pass vacuously on such a base")
	}
}
