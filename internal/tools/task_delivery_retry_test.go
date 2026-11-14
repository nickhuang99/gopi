package tools

// The ledger-keyed delivery retry (task_delivery_retry.go) driven end to end
// against a REAL two-node git world (Rule 31: the surface is executed, not
// asserted about).
//
// THE SHAPE OF THE WORLD. A bare "peer" plus a client repo standing in for the
// MAIN checkout, with the row's branch checked out in a linked worktree of it.
// The peer is reached through PRODUCTION ADDRESSING — an ssh alias composed by
// finishCleanupServerTarget into `<alias>:~/work/GoPi` — so the test places a
// receiver of its own at that name: an `ssh` that logs the argv it was handed
// (the evidence the production path resolved the peer at all) and re-targets
// every transfer to the sentinel bare repo in the test's temp space. This is
// the same instrument, for the same reason, as the sibling fixture-leak
// regression (testdata/ssh-sentinel-shim.sh), and the alternative was MEASURED
// failing: pointing GOPI_SYNC_PEERS at a plain path composes
// `…/peer.git:~/work/GoPi` and git answers "does not appear to be a git
// repository".
//
// THE THREE STATES THE EPIC NAMES, each driven through the SAME entry point:
//
//	① BLOCKED — the main checkout carries untracked residue, so the close-out
//	  chain's main-repo gate withholds the delivery. The retry must NOT report
//	  success: the peer still has no such ref, the status is `blocked`, and the
//	  row's own trail carries `delivery-blocked` (Rule 15 — a withheld delivery
//	  is visible, never silent).
//	② UNBLOCKED — the residue is removed and the SAME call is made again. The
//	  peer must then really advertise the branch — asked of the BARE REPO, never
//	  inferred from a report.
//	③ IDEMPOTENT — a row the peer already holds costs one read-only probe,
//	  moves nothing, and says so: no `src refspec … does not match any`, which
//	  memory #1817 measured an operator reading as a FAILED delivery on a row
//	  that was delivered fine.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// delRetryShimAlias is the peer alias the shim stands behind. It must pass
	// ValidateSSHPeerAlias (no colon, no whitespace, no leading `-`) — an alias
	// the production path would REFUSE would make this whole file vacuous.
	delRetryShimAlias = "delivery-retry-peer"
	// delRetryShimLogEnv / delRetryShimRepoEnv are the shim's inputs: where to
	// append the ssh argv, and the bare repo the transfer is routed to.
	delRetryShimLogEnv  = "GOPI_DELIVERY_RETRY_SHIM_LOG"
	delRetryShimRepoEnv = "GOPI_DELIVERY_RETRY_SHIM_REPO"
	// delRetryShimSSHbinary is the stand-in's own name on the shim PATH.
	delRetryShimSSHbinary = "ssh"
	// delRetryUploadPackVerb / delRetryReceivePackVerb are the two remote verbs
	// git asks ssh to run. The shim answers exactly these and REFUSES anything
	// else by name: a silent fall-through would exec the argv's own path, which
	// is precisely the reach into the operator's real repo being prevented.
	delRetryUploadPackVerb  = "git-upload-pack"
	delRetryReceivePackVerb = "git-receive-pack"
	// delRetryPeerDir names the sentinel bare repo (Rule 1b: the fixture's
	// shape is spelled once, so the three tests cannot drift).
	delRetryPeerDir = "peer"
	// delRetryRefspecErrorMarker is git's own wording for the shape the no-op
	// guard exists to prevent, spelled once.
	delRetryRefspecErrorMarker = "does not match any"
	// delRetryMainCheckoutBaseline is the content the fixture's README starts
	// with — restoring it is how a test clears the main-checkout dirt it made.
	delRetryMainCheckoutBaseline = "fixture\n"
)

// delRetryShim is an installed peer stand-in: the alias production resolves,
// the argv log, and the bare repo every transfer lands in.
type delRetryShim struct {
	alias string
	log   string
	repo  string
}

// delRetryPeerShim installs the shim and points this process's peer resolution
// at it, returning the sentinel so a test can seed and read the peer's refs
// DIRECTLY.
func delRetryPeerShim(t *testing.T) *delRetryShim {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not installed")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("shim bin dir: %v", err)
	}
	repo := filepath.Join(root, delRetryPeerDir+".git")
	covLifeGit(t, root, covLifeGitBinary, "init", "-q", "--bare", repo)

	shimPath := filepath.Join(binDir, delRetryShimSSHbinary)
	if err := os.WriteFile(shimPath, []byte(delRetryShimBody), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	logPath := filepath.Join(root, "ssh.log")
	// BOTH routes are pinned — git's own ssh override (what every leg of the
	// chain uses) and PATH — so no leg can slip past to the real ssh.
	t.Setenv(gitSSHCommandEnv, shimPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(delRetryShimLogEnv, logPath)
	t.Setenv(delRetryShimRepoEnv, repo)
	t.Setenv(config.SyncPeersEnv, delRetryShimAlias)
	// The client arm is the one under test (the server arm recycles ground
	// without delivering); the process-wide identity cache is cleared, as the
	// sibling fixture does.
	covLifeSetRole(t, string(config.NodeRoleClient))
	return &delRetryShim{alias: delRetryShimAlias, log: logPath, repo: repo}
}

// delRetryShimBody is the shim in full: it logs the argv it was handed, then
// re-targets the transfer to the sentinel. It never execs the argv's own path.
const delRetryShimBody = `#!/bin/sh
set -u
log="${` + delRetryShimLogEnv + `:-}"
repo="${` + delRetryShimRepoEnv + `:-}"
last=""
for a in "$@"; do last="$a"; done
if [ -n "$log" ]; then printf 'argv=%s\n' "$*" >>"$log"; fi
case "$last" in
` + delRetryUploadPackVerb + `*) exec ` + delRetryUploadPackVerb + ` "$repo" ;;
` + delRetryReceivePackVerb + `*) exec ` + delRetryReceivePackVerb + ` "$repo" ;;
esac
echo "delivery-retry shim: refusing unrecognised remote command: $last" >&2
exit 127
`

// argv returns the ssh argv the shim was handed ("" when ssh never spawned —
// a different, reportable state).
func (s *delRetryShim) argv(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(s.log)
	if err != nil {
		return ""
	}
	return string(b)
}

// seedPeerMaster puts the client's integration branch on the peer, so the
// branch-check lane has a ground to run in (without one it ABSTAINS, which is
// a different test than the three here).
func (s *delRetryShim) seedPeerMaster(t *testing.T, client string) {
	t.Helper()
	branch := IntegrationBranchForRepo(client)
	covLifeGit(t, client, covLifeGitBinary, "push", "-q", s.repo, branch+":"+branch)
}

// holds asks the BARE REPO itself — never a report, never a local ref —
// whether the peer advertises the branch, and at which tip.
func (s *delRetryShim) holds(t *testing.T, branch string) string {
	t.Helper()
	out, err := covLifeGitErr(t, filepath.Dir(s.repo), covLifeGitBinary,
		"--git-dir="+s.repo, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// delRetryRepoWithGround builds the client repo, a linked worktree on the
// row's branch, and one commit on it. Returns (repo, worktree, branch).
func delRetryRepoWithGround(t *testing.T, branch string) (string, string, string) {
	t.Helper()
	repo, wt, branch := covLifeRepoWithWorktree(t, branch)
	covLifeWriteFile(t, filepath.Join(wt, "deliverable.txt"), "the row's work\n")
	covLifeGit(t, wt, covLifeGitBinary, "add", "deliverable.txt")
	covLifeGit(t, wt, covLifeGitBinary, "-c", "user.email=retry@example.invalid",
		"-c", "user.name=retry", "commit", "-q", "-m", "the deliverable")
	return repo, wt, branch
}

// delRetryTrailEvents returns the row's trail event names, oldest first.
func delRetryTrailEvents(t *testing.T, taskID string) []string {
	t.Helper()
	db, err := openTasksDB(t.Context(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(t.Context(),
		`SELECT event FROM task_events WHERE task_id = ? ORDER BY created_at ASC`, taskID)
	if err != nil {
		t.Fatalf("read trail: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			t.Fatalf("scan trail: %v", err)
		}
		out = append(out, e)
	}
	return out
}

// TestDeliveryRetryNonCloneArmReportsOwedAndPushesNothing is ① and ② rewritten
// after the close-out chain was retired (2026-10-30, task 01a18ea5).
//
// WHAT IT USED TO ASSERT, AND WHY THAT CANNOT STAND. The old
// TestDeliveryRetryBlockedThenDeliveredOnTheSamePath asserted a two-state path:
// first call refused by the main-repo gate, second call (blocker cleared)
// DELIVERED, the peer holding the branch's tip. That delivery was performed by
// the close-out chain, which 58b5618 deleted; `deliveryRetryOne` now states so
// in its own words ("THE CLOSE-OUT CHAIN ASK IS RETIRED … what this pass
// reports below is the peer's answer, read after nothing was pushed"). The
// second half of the old claim therefore describes a push no production code
// performs, and re-deriving it would mean pinning a delivery this file does not
// do — a test asserting a stranger's behaviour.
//
// WHAT IS TRUE NOW, AND IS WORTH PINNING: the non-clone arm reports the row as
// OWED with the row's own recorded deliverable as the reason, records it on the
// trail, and pushes NOTHING. That last half is the one that must never rot: a
// "retry" that silently stopped reporting would look identical to one that
// succeeded, and the ledger keyed set (DeliveryRetryOwedRowIDs) is what the
// pass that already ticks consumes.
func TestDeliveryRetryNonCloneArmReportsOwedAndPushesNothing(t *testing.T) {
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)
	repo, wt, branch := delRetryRepoWithGround(t, "task/retry-two-state")
	shim.seedPeerMaster(t, repo)
	covLifeSeedTaskRow(t, "t-retry-two-state",
		taskDeliverableRebuild(taskDeliverable{Repo: repo, Branch: branch, Worktree: wt}))

	rep, err := DeliveryRetryTaskID("t-retry-two-state")
	if err != nil {
		t.Fatalf("delivery retry: %v", err)
	}
	if !rep.Owed() {
		t.Fatalf("a non-clone row whose chain is retired must read as OWED, got status=%q detail=%q",
			rep.Status, rep.Detail)
	}
	if !strings.Contains(rep.Detail, deliveryRetryOwedMarker) {
		t.Errorf("the report must carry the owed wording %q: %q", deliveryRetryOwedMarker, rep.Detail)
	}
	// THE HONEST HALF: nothing was pushed, so the peer must hold nothing.
	if tip := shim.holds(t, branch); tip != "" {
		t.Fatalf("the peer received %s at %s from a pass that pushes nothing — the report lied about a delivery",
			branch, tip)
	}
	delRetryAssertNoRefspecError(t, rep)
	if events := delRetryTrailEvents(t, "t-retry-two-state"); !hasString(events, deliveryRetryBlockedEvent) {
		t.Errorf("the row's own trail must carry %q when a delivery is owed, got %v",
			deliveryRetryBlockedEvent, events)
	}
	// THE LEDGER IS THE INVENTORY: the owed row is discoverable by the pass that
	// already ticks, with no bookkeeping of its own.
	owed, err := DeliveryRetryOwedRowIDs()
	if err != nil {
		t.Fatalf("the owed-row scan must be readable: %v", err)
	}
	if !hasString(owed, "t-retry-two-state") {
		t.Errorf("the owed row is not in the ledger-keyed retry set: %v", owed)
	}
}

// TestDeliveryRetryChannelCarriesTheWireFormOnBothArms pins the SECOND leg of
// the epic's `TestDeliveryWireFormHoldsOnBothArms` contract at THIS channel:
// `deliveryRetryOne` reads the resolution's SSHTarget, so the retry's report —
// and the close-out chain it drives — must carry `<alias>:<path>` whether or not
// this repo happens to configure a git remote of that name.
//
// It is its own arm rather than an assertion inside ①/② above because those
// two drive the FALLBACK shape only (the fixture configures `origin`, never a
// remote named like the alias). The gap that matters is the remote arm: there
// the repo-level answer (`Rendered`) is the BARE remote name, and a channel
// reading the wrong field would hand git a name that resolves in this checkout
// and nowhere else — the exact defect this epic exists to abolish.
//
// The row is not required to reach `delivered` here (that is ②'s subject): what
// is asserted is the TARGET the channel resolved, its presence in the ssh argv,
// and — for the remote arm — that the resolution really did find the remote
// (otherwise the assertion would be vacuous, and a fixture that stopped
// exercising the arm would still pass).
func TestDeliveryRetryChannelCarriesTheWireFormOnBothArms(t *testing.T) {
	for _, arm := range []struct {
		name string
		// configureRemote says whether the CLIENT repo carries a git remote
		// named like the declared alias — the only difference between the arms.
		configureRemote bool
	}{
		{name: "remote arm: the alias IS a remote of this repo", configureRemote: true},
		{name: "fallback arm: no remote of that name", configureRemote: false},
	} {
		t.Run(arm.name, func(t *testing.T) {
			covLifeGitEnv(t)
			shim := delRetryPeerShim(t)
			repo, wt, branch := delRetryRepoWithGround(t, "task/retry-wire-form")
			shim.seedPeerMaster(t, repo)
			if arm.configureRemote {
				covLifeGit(t, repo, covLifeGitBinary, "remote", "add", shim.alias, wt)
			}
			covLifeSeedTaskRow(t, "t-retry-wire-form",
				taskDeliverableRebuild(taskDeliverable{Repo: repo, Branch: branch, Worktree: wt}))

			want := peerRemoteFallbackTarget(shim.alias, peerRepoPath())
			res := finishCleanupRowTargetResolution(repo)
			if arm.configureRemote != (res.Remote == shim.alias) {
				t.Fatalf("fixture: configureRemote=%v but the resolution found remote=%q (reason: %s)",
					arm.configureRemote, res.Remote, res.Reason)
			}
			if res.SSHTarget != want {
				t.Fatalf("the resolution must hand this channel the wire form %q, got %q", want, res.SSHTarget)
			}

			rep, err := DeliveryRetryTaskID("t-retry-wire-form")
			if err != nil {
				t.Fatalf("delivery retry: %v", err)
			}
			// THE LEG: the report's Target is the string this channel resolved
			// and handed to the chain's push.
			if rep.Target != want {
				t.Fatalf("the retry channel must carry the wire form %q on this arm, got %q (status=%q detail=%q)",
					want, rep.Target, rep.Status, rep.Detail)
			}
			if !strings.Contains(rep.Target, peerRemoteTailSep) {
				t.Fatalf("the wire form must be <alias>%s<path>, got %q", peerRemoteTailSep, rep.Target)
			}
			// WHAT IS NO LONGER ASSERTED HERE (2026-10-30, task 01a18ea5): that
			// the delivery "really travelled" the wire form — the peer
			// advertising the branch and the ssh argv naming the alias. Both
			// halves measured the close-out chain's push, which 58b5618 retired;
			// on this tree the retry resolves the target and pushes nothing, so
			// the assertions above (the string this channel hands on, on BOTH
			// arms — the epic's actual contract) are the whole subject. The
			// "nothing was pushed" fact is pinned positively by
			// TestDeliveryRetryNonCloneArmReportsOwedAndPushesNothing.
		})
	}
}

// TestDeliveryRetryIsIdempotentOnADeliveredRow — RETIRED (2026-10-30, task
// 01a18ea5), and its subject is covered from two live sides instead.
//
// It began by DELIVERING the row through the path under test ("no fixture
// hand-push"), which is exactly the step 58b5618 removed: the retry resolves a
// target and reports the peer's answer, it no longer pushes, so "a delivered row
// costs one read-only probe" has no first delivery to be the second call of.
// The two facts it protected survive where they are still true:
//
//   - the resurrect guard itself (a peer master that already CONTAINS the row's
//     tip is left alone) is asked on the CLONE arm and pinned by
//     TestDeliveryRetryOnACloneGroundRowAsksAboutTheRowsOwnTip, which is live
//     because the clone channel still delivers;
//   - "a pass that changed nothing reports no refspec error" is asserted by
//     delRetryAssertNoRefspecError in the owed-arm test above and by the no-local-
//     branch test below.
//
// Re-deriving the deleted half would mean asserting a delivery no production leg
// performs; the retirement is recorded here rather than by deleting the file in
// silence (Rule 41).

func TestDeliveryRetryWithNoLocalBranchRecordsANoOp(t *testing.T) {
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)
	repo := covLifeGitRepo(t)
	branch := "task/retry-ref-gone"
	covLifeGit(t, repo, covLifeGitBinary, "branch", branch)
	shim.seedPeerMaster(t, repo)
	covLifeSeedTaskRow(t, "t-retry-ref-gone",
		taskDeliverableRebuild(taskDeliverable{Repo: repo, Branch: branch}))
	// The ref is gone because an earlier pass delivered it and recycled the
	// branch — the state this guard exists for.
	covLifeGit(t, repo, covLifeGitBinary, "branch", "-D", branch)

	rep, err := DeliveryRetryTaskID("t-retry-ref-gone")
	if err != nil {
		t.Fatalf("delivery retry (no local ref): %v", err)
	}
	if rep.Status != deliveryRetryStatusNoLocalBranch {
		t.Errorf("status=%q, want %q (detail=%q)", rep.Status, deliveryRetryStatusNoLocalBranch, rep.Detail)
	}
	delRetryAssertNoRefspecError(t, rep)
}

// TestDeliveryRetryRefusesAnUnknownRow: a caller that names a row which is not
// in the ledger is told so — an error, never an empty successful attempt.
func TestDeliveryRetryRefusesAnUnknownRow(t *testing.T) {
	covLifeGitEnv(t)
	if _, err := DeliveryRetryTaskID("t-does-not-exist"); err == nil {
		t.Fatal("an unknown task id must be an error, not a silent no-op")
	}
	if _, err := DeliveryRetryTaskID("   "); err == nil {
		t.Fatal("an empty task id must be an error")
	}
}

// delRetryAssertNoRefspecError fails when a report carries git's own refspec
// error — the misreport this path exists to prevent (memory #1817: an operator
// reads it as a failed delivery on a row that was delivered fine).
func delRetryAssertNoRefspecError(t *testing.T, rep *DeliveryRetryReport) {
	t.Helper()
	if rep == nil {
		return
	}
	body := rep.Detail
	if strings.Contains(body, delRetryRefspecErrorMarker) {
		t.Errorf("the report carries git's refspec error (%q) — a delivered/no-op row must never read as a failed push: %q",
			delRetryRefspecErrorMarker, body)
	}
}

// covLifeGitErr runs one git command and returns its output WITHOUT failing the
// test — the shape a probe needs (a MISSING ref is an answer, not an error).
func covLifeGitErr(t *testing.T, dir, bin string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// hasString reports membership (used for the trail's event names and the
// ledger-keyed retry set).
func hasString(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}

// firstStatus / firstDetail render a possibly-nil report in a failure message
// without panicking on the nil the failure is describing.
func firstStatus(r *DeliveryRetryReport) string {
	if r == nil {
		return "<nil>"
	}
	return r.Status
}

func firstDetail(r *DeliveryRetryReport) string {
	if r == nil {
		return "<nil>"
	}
	return r.Detail
}

// TestDeliveryRetryOnACloneGroundRowAsksAboutTheRowsOwnTip is the regression
// guard for bug 01a17343 (measured 2026-10-26 on host host-a).
//
// THE DEFECT. Every CLONE-GROUND row records `deliverable.branch = master`
// (the clone channel pushes `HEAD:master` and its ground stands on the
// integration branch). The retry's resurrect guard used to ask the peer
// `ls-remote --heads <target> master`, take THAT tip, and then ask "does the
// peer's master contain it?" — a value compared with itself, true by
// construction. Every such row answered `already-merged`, recorded a no-op and
// performed NO delivery, while the row's own ground HEAD was contained in
// neither the peer's master nor the delivery point's. Measured on live ledger:
// 78 failed rows were all `already-merged`, and row
// 01a16deb-73df-7a96-851d-97297f619246 kept ground HEAD c76ccb13 with the
// delivery point at f110202d (ancestor? NO).
//
// THE QUESTION THIS TEST ASKS, and the one the guard must ask: on a clone-ground
// row whose OWN tip the peer does not have, the disposition must NOT be
// `already-merged`, and the row's own tip must be reported (RowTip) so the two
// tips can be compared at all.
func TestDeliveryRetryOnACloneGroundRowAsksAboutTheRowsOwnTip(t *testing.T) {
	covLifeGitEnv(t)
	shim := delRetryPeerShim(t)

	// A clone ground: its OWN repository (`.git` is a DIRECTORY), a plain clone
	// of the upstream, standing on the integration branch — exactly the shape
	// the clone channel creates at <workspace root>/<task-id>.
	upstream := covLifeGitRepo(t)
	clone := filepath.Join(t.TempDir(), "clone-ground")
	covLifeGit(t, upstream, covLifeGitBinary, "clone", "-q", upstream, clone)

	// The row's work: one commit on the clone's integration branch that the
	// peer has never seen.
	covLifeWriteFile(t, filepath.Join(clone, "deliverable.txt"), "the row's work\n")
	covLifeGit(t, clone, covLifeGitBinary, "add", "deliverable.txt")
	covLifeGit(t, clone, covLifeGitBinary, "-c", "user.email=retry@example.invalid",
		"-c", "user.name=retry", "commit", "-q", "-m", "the row's deliverable")
	rowTip := covLifeGit(t, clone, covLifeGitBinary, "rev-parse", "HEAD")

	// The peer holds its own master, at a DIFFERENT tip than this row's — the
	// state of every row the defect mis-classified.
	shim.seedPeerMaster(t, upstream)
	peerTipBefore := shim.holds(t, masterBranchName)
	if peerTipBefore == "" {
		t.Fatal("fixture: the peer must advertise the integration branch for this arm to mean anything")
	}
	if peerTipBefore == rowTip {
		t.Fatalf("fixture: the row's tip must differ from the peer's, both are %s", rowTip)
	}

	covLifeSeedTaskRow(t, "t-retry-clone-ground",
		taskDeliverableRebuild(taskDeliverable{Repo: clone, Branch: masterBranchName, Worktree: clone}))

	rep, err := DeliveryRetryTaskID("t-retry-clone-ground")
	if err != nil {
		t.Fatalf("delivery retry on a clone-ground row: %v", err)
	}
	if rep.Status == deliveryRetryStatusAlreadyMerged {
		t.Fatalf("a clone-ground row whose OWN tip the peer does not have was classified %q — "+
			"the resurrect guard compared the peer's tip with itself again "+
			"(rowTip=%q peerTip=%q detail=%q)",
			rep.Status, rowTip, rep.PeerTip, rep.Detail)
	}
	// THE TIP IS REPORTED, so the operator (and the guard) can compare.
	if rep.RowTip != rowTip {
		t.Errorf("the report must carry THIS ROW's own tip %q, got %q (status=%q detail=%q)",
			rowTip, rep.RowTip, rep.Status, rep.Detail)
	}
	// And the peer's own tip is NOT silently accepted as this row's delivery:
	// if the peer still advertises its own unrelated tip, the row is not
	// `already-delivered` either.
	if tip := shim.holds(t, masterBranchName); tip == peerTipBefore && rep.Status == deliveryRetryStatusAlreadyDelivered {
		t.Errorf("the peer's own unrelated tip %q was reported as THIS row's delivery (detail=%q)",
			tip, rep.Detail)
	}
}
