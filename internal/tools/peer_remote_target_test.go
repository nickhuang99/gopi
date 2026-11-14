package tools

// The DELIVERY TARGET'S RESOLUTION (peer_remote_target.go) — the two arms, and
// the regression the epic was filed for: a repo whose git REMOTE NAME and whose
// declared ssh ALIAS are not the same string must still DELIVER.
//
// MEASURED 2026-10-12 on host-b: `sync_peers: ["peer1"]` while `git remote -v`
// listed only `origin`. The client close-out chain composed its target as
// `<alias>:<path>`, read the topology alias as if it were a git remote name, and
// every delivery leg died on git's own
// `fatal: 'peer1' does not appear to be a git repository` — a sentence that
// names neither the declared peer nor the remotes that DO exist here.
//
// The three properties pinned below, in the order they matter:
//
//	① the resolution ASKS THIS REPO (git remote) and answers with the remote
//	   NAME when the repo configures one — and, having established that the
//	   declared peer is a name this repo's git really knows, still hands the
//	   LEGS the explicit `<alias>:<path>` form, because a leg gives its target
//	   to git in a DIFFERENT repository (the row's worktree) where that name
//	   resolves to nothing (peerRemoteTarget.SSHTarget carries the measurement);
//	② when the repo has NO remote of that name the resolution is the same
//	   explicit `<alias>:<path>` form, which is what ssh can carry on its own —
//	   that is the arm the host-b shape takes, and it is a RESOLUTION, not a
//	   failure. Both arms therefore agree on the ONE string a leg carries (the
//	   wire form), and differ only in what they can SAY about the peer;
//	③ a delivery that FAILS says which arm it was on and lists this repo's
//	   actual remotes (Rule 15) — the operator's question after a red push is
//	   "did the target go through a remote, or straight to ssh?", and the
//	   answer now rides the refusal.
//
// The real legs are driven, never simulated: ① and ② run the close-out chain
// itself (taskFinishCleanupChain — the SAME dispatcher `--apply` uses, so the
// target under test is the one production renders), with the ssh side either
// git's own remote machinery (a local path) or the committed relay shim.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// prtAliasInRemoteName is the alias used by the remote arm: it is ALSO the
// name the fixture client configures as its git remote, so the resolution has
// something to find. WHAT it finds is the ALIAS CHOICE (the peer is a name this
// repo's git knows) and the named diagnosis — not a change of the wire form:
// the legs still carry `<alias>:<path>`, because they hand their target to git
// in the row's repo, not in this checkout. The delivery below therefore runs
// through the ssh route, and the sentinel is its evidence.
const prtAliasInRemoteName = "peer-remote"

// prtAliasNotARemote is the alias used by the mismatch / diagnosis arms: this
// repo configures NO remote of that name (it has `origin`), which is the host-b
// shape the epic was filed for.
const prtAliasNotARemote = "peer1"

// prtChainReport runs the close-out chain for the fixture with the DECLARED
// topology in force — never with an injected target, so what runs is the
// production resolution (that is the whole subject here).
func prtChainReport(t *testing.T, f *finishCleanupFixture) *FinishCleanupReport {
	t.Helper()
	t.Setenv(finishCleanupExemptEnv, "")
	rep, err := taskFinishCleanupChain(context.Background(),
		&FinishCleanupReport{TaskID: "test", Repo: f.clientDir},
		taskFinishDeliverable{Repo: f.clientDir, Branch: f.featBranch, Worktree: f.wtDir}, false)
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	return rep
}

// prtStep finds a named step's detail and whether the chain REACHED it — the
// step's own OK flag is the caller's business (a REFUSED step is a step that
// ran, and that distinction is the whole point of several assertions here).
func prtStep(rep *FinishCleanupReport, step string) (string, bool) {
	for _, s := range rep.Steps {
		if s.Step == step {
			return s.Detail, true
		}
	}
	return "", false
}

// TestPeerRemoteTargetNamesTheArmAndThisReposRemotes pins the RESOLUTION as a
// unit: which string it answers with, and what it says about why. Two repos,
// two arms, both real (`git remote add` in a temp repo).
func TestPeerRemoteTargetNamesTheArmAndThisReposRemotes(t *testing.T) {
	if _, err := filepath.Abs("."); err != nil {
		t.Fatalf("abs: %v", err)
	}
	ctx := context.Background()

	// ① THE REMOTE ARM — the repo configures a remote whose name IS the alias.
	f := newFinishCleanupFixture(t)
	pushContractGit(t, f.clientDir, "remote", "add", prtAliasInRemoteName, f.serverDir)

	res := peerRemoteTargetFor(ctx, f.clientDir, prtAliasInRemoteName, peerRepoPath())
	if res.Remote != prtAliasInRemoteName || res.Rendered != prtAliasInRemoteName {
		t.Fatalf("a remote of that name must BE the repo-level answer, got remote=%q rendered=%q", res.Remote, res.Rendered)
	}
	if strings.Contains(res.Rendered, peerRemoteTailSep) {
		t.Errorf("the repo-level answer must be the bare remote name, not an alias:path shape: %q", res.Rendered)
	}
	// The WIRE form is the other field, and it is the ssh spelling on this arm
	// too: the legs do not resolve inside this repo (see SSHTarget's doc).
	if want := peerRemoteFallbackTarget(prtAliasInRemoteName, peerRepoPath()); res.SSHTarget != want {
		t.Errorf("the legs' target must stay the ssh form %q on this arm, got %q", want, res.SSHTarget)
	}
	for _, want := range []string{prtAliasInRemoteName, res.SSHTarget, "server"} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("the resolution sentence must name %q (which remote, which form it hands on, this repo's remotes): %q", want, res.Reason)
		}
	}

	// ② THE FALLBACK ARM — a real alias, but this repo has no remote of that
	// name. The answer is the explicit ssh form, and the sentence carries the
	// remotes that DO exist here plus the settings key to correct.
	ghost := peerRemoteTargetFor(ctx, f.clientDir, prtAliasNotARemote, peerRepoPath())
	wantTarget := prtAliasNotARemote + peerRemoteTailSep + peerRepoPath()
	if ghost.Remote != "" || ghost.Rendered != wantTarget {
		t.Fatalf("an alias this repo has no remote for must fall back to %q, got remote=%q rendered=%q",
			wantTarget, ghost.Remote, ghost.Rendered)
	}
	// `server` is the fixture client's own remote: the sentence must carry the
	// remotes THIS repo has, whatever their names happen to be.
	for _, want := range []string{prtAliasNotARemote, wantTarget, config.SyncPeersSettingsKey, "server"} {
		if !strings.Contains(ghost.Reason, want) {
			t.Errorf("the fallback sentence must name %q: %q", want, ghost.Reason)
		}
	}

	// ③ A REPO WITH NO REMOTE AT ALL — the measurement must be readable as a
	// measurement (Rule 15: "found nothing" is never a blank).
	bare := t.TempDir()
	pushContractGit(t, bare, "init", "--bare", "--quiet", filepath.Join(bare, "empty.git"))
	none := peerRemoteTargetFor(ctx, filepath.Join(bare, "empty.git"), prtAliasNotARemote, peerRepoPath())
	if !strings.Contains(none.Reason, pushbackRemoteNameNone) {
		t.Errorf("a repo with no remotes must print the named 'none' measurement, got %q", none.Reason)
	}

	// ④ NO ALIAS, NO TARGET — the one case that answers empty (the caller's
	// existing fail-soft arm names it), never a guess.
	if empty := peerRemoteTargetFor(ctx, f.clientDir, "", peerRepoPath()); empty.Rendered != "" || empty.Reason != "" {
		t.Errorf("an empty alias must resolve to nothing at all, got %+v", empty)
	}
}

// TestDeliveryTargetUsesTheReposOwnRemoteName is ACCEPTANCE ① of the epic: a
// repository whose git REMOTE NAME is the string the topology declares resolves
// that name to a peer this repo's git really knows, and DELIVERS the branch —
// read on the recipient, not from the push's own exit code.
//
// The wire form is the same on both arms, and that is not a downgrade of this
// case: the legs do not push INTO this checkout's remote configuration. They
// hand git a target while running in the ROW's repo (finishCleanupMainRepoDir /
// the ground's own repo), so a remote name configured HERE resolves to nothing
// there — measured, see peerRemoteTarget.SSHTarget. What this arm must deliver
// is therefore: the alias choice (the declared peer is one this repo's git
// knows), the named diagnosis saying so, and the branch actually landing. All
// three are asserted below against the sentinel.
func TestDeliveryTargetUsesTheReposOwnRemoteName(t *testing.T) {
	shim := installLeakSSHShim(t)
	allowTestPushTarget(t) // the sentinel is the recipient; nothing outside the suite's temp root is written

	f := newFinishCleanupFixture(t)
	// THE ARM'S CONDITION, in the smallest world that has it: this real repo
	// configures a git remote whose NAME is the alias the topology declares.
	// (The fixture's own `server` remote stays; the URL is git's business, the
	// NAME is what the resolution asks for.)
	pushContractGit(t, f.clientDir, "remote", "add", shim.alias, f.serverDir)

	t.Setenv(config.SyncPeersEnv, shim.alias)

	// The resolution is asked about the ROW's repo (the shape every delivery
	// caller must use): the process cwd is deliberately left elsewhere, because
	// a remote is a per-repository setting and a CLI close-out is invoked from a
	// different checkout than the one it delivers from.
	res := finishCleanupRowTargetResolution(f.clientDir)
	if res.Remote != shim.alias {
		t.Fatalf("fixture: %s must be found as a remote of the row's repo, got remote=%q reason=%q", shim.alias, res.Remote, res.Reason)
	}
	if want := peerRemoteFallbackTarget(shim.alias, peerRepoPath()); res.SSHTarget != want {
		t.Fatalf("a leg must carry the wire form %q on this arm too, got %q", want, res.SSHTarget)
	}
	for _, wantSub := range []string{shim.alias, res.SSHTarget} {
		if !strings.Contains(res.Reason, wantSub) {
			t.Errorf("the remote arm's sentence must name %q — the remote it found AND the form it hands on: %q", wantSub, res.Reason)
		}
	}
	t.Chdir(f.clientDir)
	if got := finishCleanupServerTarget(); got != res.SSHTarget {
		t.Fatalf("asked about this process's own checkout the answer must be the same here, got %q", got)
	}

	rep := prtChainReport(t, f)
	detail, ok := prtStep(rep, finishCleanupPushVerb)
	if !ok {
		t.Fatalf("the chain never reached its push step: %+v", rep)
	}
	// The delivery is read at the RECIPIENT: the sentinel holds the branch.
	if got := leakShimRefState(t, shim.repo, f.featBranch); got == "" {
		t.Fatalf("the remote arm must DELIVER: the sentinel holds no %s (push detail: %s)", f.featBranch, detail)
	}
	if invocations := shim.invocations(t); len(invocations) == 0 {
		t.Fatalf("no ssh ran at all — this arm's target is the ssh form, push detail: %s", detail)
	}
}

// TestFinishCleanupTargetHoldsTheWireFormWhenTheAliasIsARemoteName is the
// MINIMAL REPRODUCTION of the regression this epic was filed for, and it is the
// smallest world in which it appears: a REAL repository that configures a git
// remote whose name IS the declared alias — the shape this checkout itself has
// (`git remote -v` → `peer1`), which is why the defect reached master.
//
// THE TWO SIDES, and which one moved (memory #6569's method: narrow to the
// smallest sample, then decide with evidence — never by weakening a claim):
//
//	the new production arm asserted the target becomes the bare remote NAME;
//	the two pre-existing tests assert the target is `<alias>:<path>`.
//
// They cannot both hold, so the question is not "who is stale" but "which
// contract is real". MEASURED, by executing git in a repository that does NOT
// configure the name (the state of every repo a leg actually pushes FROM):
//
//	git fetch peer1  →  fatal: 'peer1' … unknown transport
//	git push  peer1  →  fatal: 'peer1' does not appear to be a git repository
//
// A remote name is not a transport; it is a per-repo local alias, resolvable
// only where declared. A delivery target is handed to git in the ROW's repo. So
// the wire form is `<alias>:<path>` and the new arm's assertion was the side
// that moved — while its REAL contribution (asking this repo's remotes, and
// naming the arm in the diagnosis) is kept and asserted here.
//
// This test is written as its own arm rather than folded into the fixture
// tests because the sender must be a real repo with a real remote of that name:
// `git remote add` in a throwaway repository, replaced by `set-url`, exactly the
// minimal sample.
func TestFinishCleanupTargetHoldsTheWireFormWhenTheAliasIsARemoteName(t *testing.T) {
	shim := installLeakSSHShim(t)
	f := newFinishCleanupFixture(t)
	pushContractGit(t, f.clientDir, "remote", "add", shim.alias, f.serverDir) // the alias IS a remote of this real repo

	// The topology declares that same alias — that is the ONLY way the arm this
	// test is about can be reached (a resolution with no alias renders nothing,
	// which is a different, already-pinned case).
	t.Setenv(config.SyncPeersEnv, shim.alias)

	res := finishCleanupRowTargetResolution(f.clientDir)
	if res.Remote != shim.alias {
		t.Fatalf("the resolution must FIND the remote of that name, got remote=%q reason=%q", res.Remote, res.Reason)
	}
	if want := shim.alias + peerRemoteTailSep + peerRepoPath(); res.SSHTarget != want {
		t.Fatalf("the leg's target must stay the ssh form %q, got %q — a bare remote name is not a transport", want, res.SSHTarget)
	}
	if strings.Contains(res.SSHTarget, "\n") || strings.TrimSpace(res.SSHTarget) != res.SSHTarget {
		t.Fatalf("the target handed to git must be a clean single token, got %q", res.SSHTarget)
	}
	// The guard against the original defect is still in force on this arm: the
	// alias half of the composed target must pass the peer-alias validator, since
	// a remote NAME could equally have been option-shaped.
	if _, err := ValidateSSHGitTarget(res.SSHTarget); err != nil {
		t.Fatalf("the rendered target must survive the peer-alias guard: %v", err)
	}
}

// TestDeliveryWireFormHoldsOnBothArms is the EPIC's central assertion, made at
// the LEG rather than at the resolver: `<alias>:<path>` is the string the
// close-out chain HANDS TO GIT on BOTH arms, and the difference between the
// arms is only what the resolution can SAY about the peer.
//
// Why this arm is separate from the resolver unit test (② in
// TestPeerRemoteTargetNamesTheArmAndThisReposRemotes): a field being right and
// a leg CARRYING it are two facts, and the regression this epic was filed for
// lived in the gap — the resolver's remote arm answered a bare remote name and
// the legs (finishCleanupServerTarget, the delivery retry channel, the
// push-target step) each had to read the right field to notice. Pinning the
// push-target step's own Target is what makes that gap unrepresentable: it is
// the string the leg resolved and handed on.
//
// The two arms, in the smallest worlds that have them, both driven through the
// real chain (`taskFinishCleanupChain` — the same dispatcher `--apply` uses):
//
//	remote arm    this real repo configures a git remote whose name IS the
//	              declared alias, so the resolution FINDS it (`Remote` non-empty,
//	              the diagnosis names it) — and the leg still carries
//	              `<alias>:<path>`.
//	fallback arm  the repo configures no remote of that name — the host-b shape —
//	              and the leg carries the SAME wire form.
//
// If the two could ever disagree, a delivery would be dialled differently
// depending on a per-repository config the receiving side cannot see, which is
// precisely the defect (peerRemoteTarget.SSHTarget carries the measurement:
// `git push peer1` → "does not appear to be a git repository").
//
// NOTE on the subtest names: they are deliberately shell-safe (no parentheses,
// no commas). t.TempDir() derives its directory name from the test name, and
// GIT_SSH_COMMAND is run through `sh -c` by git — a name carrying punctuation
// produces a sh syntax error in the shim, which would look like a product
// failure to the next reader.
func TestDeliveryWireFormHoldsOnBothArms(t *testing.T) {
	for _, arm := range []struct {
		name string
		// configureRemote says whether this arm's repo carries a remote named
		// like the declared alias — the ONLY difference between the two arms.
		configureRemote bool
	}{
		{name: "remote arm: the alias IS a remote of this repo", configureRemote: true},
		{name: "fallback arm: no remote of that name", configureRemote: false},
	} {
		t.Run(arm.name, func(t *testing.T) {
			shim := installLeakSSHShim(t)
			allowTestPushTarget(t) // the sentinel is the only recipient the shim can reach
			f := newFinishCleanupFixture(t)
			if arm.configureRemote {
				pushContractGit(t, f.clientDir, "remote", "add", shim.alias, f.serverDir)
			}
			t.Setenv(config.SyncPeersEnv, shim.alias)
			t.Chdir(f.clientDir)

			want := peerRemoteFallbackTarget(shim.alias, peerRepoPath())
			res := finishCleanupRowTargetResolution(f.clientDir)
			// The arms are told apart by what the resolution FOUND, never by the
			// wire form — stated as its own precondition so a fixture that
			// silently stopped exercising one arm fails here.
			if arm.configureRemote != (res.Remote == shim.alias) {
				t.Fatalf("fixture: configureRemote=%v but the resolution found remote=%q (reason: %s)",
					arm.configureRemote, res.Remote, res.Reason)
			}
			if res.SSHTarget != want {
				t.Fatalf("the resolution must hand the legs the wire form %q on this arm, got %q", want, res.SSHTarget)
			}

			rep := prtChainReport(t, f)
			// THE LEG: the push-target step's Target is the string this leg
			// resolved and handed on (the `push` step's own Target is the BRANCH
			// it moved, which is a different question).
			step := prtStepStruct(rep, finishCleanupPushTargetStep)
			if step == nil {
				t.Fatalf("the chain never reported its %s step: %+v", finishCleanupPushTargetStep, rep)
			}
			if step.Target != want {
				t.Fatalf("the push leg must carry the wire form %q on this arm, got %q (detail: %s)", want, step.Target, step.Detail)
			}
			if !strings.Contains(step.Target, peerRemoteTailSep) {
				t.Fatalf("the wire form must be <alias>%s<path>, got %q", peerRemoteTailSep, step.Target)
			}
			// And the resolution's own sentence must name the form the leg carries
			// (Rule 15: the diagnosis says which string is being handed on).
			if !strings.Contains(step.Detail, want) {
				t.Errorf("the target step's detail must name the form it hands on (%q): %s", want, step.Detail)
			}
			// And it DELIVERED through it: the sentinel holds the branch, dialled
			// as ssh with the declared alias in the argv.
			if got := leakShimRefState(t, shim.repo, f.featBranch); got == "" {
				t.Fatalf("this arm must DELIVER through the wire form: the sentinel holds no %s (push detail: %s)", f.featBranch, step.Detail)
			}
			invocations := shim.invocations(t)
			if len(invocations) == 0 {
				t.Fatalf("no ssh ran at all — the wire form is the ssh transport, target detail: %s", step.Detail)
			}
			if joined := strings.Join(invocations, "\n"); !strings.Contains(joined, shim.alias) {
				t.Errorf("the ssh argv must carry the declared alias %q, got:\n%s", shim.alias, joined)
			}
		})
	}
}

// prtStepStruct returns the whole named step (prtStep above answers only for
// its Detail; this arm also has to read the step's TARGET, which is the string
// the leg resolved).
func prtStepStruct(rep *FinishCleanupReport, step string) *FinishCleanupStep {
	for i := range rep.Steps {
		if rep.Steps[i].Step == step {
			return &rep.Steps[i]
		}
	}
	return nil
}

// TestDeliveryTargetFallsBackToSshFormAndDeliversTheBranch is ACCEPTANCE ②, and
// it is the epic's headline case: a repository whose git remote is named
// DIFFERENTLY from the declared alias (`origin` here, `sync_peers[0]` = another
// name) must still hand its branch over.
//
// The ssh side is driven FOR REAL through the repository's committed relay shim
// (`testdata/ssh-sentinel-shim.sh`, installed as git's ssh by
// installLeakSSHShim): git really spawns ssh, the shim really runs
// `git-receive-pack` against the sentinel, and the ref's presence in the
// sentinel afterwards is the delivery's evidence — while the shim's argv log is
// the evidence that the alias the topology declared is the one that was dialled
// (Rule 31; and Rule 30's safety: the shim can only ever write the sentinel).
func TestDeliveryTargetFallsBackToSshFormAndDeliversTheBranch(t *testing.T) {
	shim := installLeakSSHShim(t)
	allowTestPushTarget(t) // the fallback's local side is the peer's ~/work/GoPi; the relay is where it lands

	t.Setenv(config.SyncPeersEnv, shim.alias)
	f := newFinishCleanupFixture(t)
	t.Chdir(f.clientDir)
	if remotes := pushContractGit(t, f.clientDir, "remote"); strings.Contains(remotes, shim.alias) {
		t.Fatalf("fixture: the client must NOT configure a remote named %q — this arm is the mismatch", shim.alias)
	}

	want := peerRemoteFallbackTarget(shim.alias, peerRepoPath())
	if got := finishCleanupRowTargetResolution(f.clientDir).Rendered; got != want {
		t.Fatalf("a remote-less alias must fall back to the explicit ssh form %q, got %q", want, got)
	}

	rep := prtChainReport(t, f)
	detail, ok := prtStep(rep, finishCleanupPushVerb)
	if !ok {
		t.Fatalf("the chain never reached its push step: %+v", rep)
	}
	if got := leakShimRefState(t, shim.repo, f.featBranch); got == "" {
		t.Fatalf("the fallback arm must DELIVER: the sentinel holds no %s (push detail: %s)", f.featBranch, detail)
	}
	invocations := shim.invocations(t)
	if len(invocations) == 0 {
		t.Fatalf("no ssh ran at all — the fallback arm must dial the declared alias, push detail: %s", detail)
	}
	if joined := strings.Join(invocations, "\n"); !strings.Contains(joined, shim.alias) {
		t.Errorf("the ssh argv must carry the declared alias %q, got:\n%s", shim.alias, joined)
	}
}

// TestDeliveryTargetFailureNamesTheArmAliasAndRemotes is ACCEPTANCE ③ (Rule 15):
// when the fallback arm cannot deliver, the refusal must name the arm it was on,
// the alias, the declared-topology key and this repo's ACTUAL remotes — the
// four facts an operator needs, and the four that the pre-fix report omitted.
//
// The transport is refused locally and inertly (GIT_SSH_COMMAND=/bin/false), so
// nothing is dialled and no host is contacted: the failure under test is the
// REPORT's, and the arm's own evidence (the resolution's sentence) is what is
// asserted.
func TestDeliveryTargetFailureNamesTheArmAliasAndRemotes(t *testing.T) {
	f := newFinishCleanupFixture(t)
	t.Setenv(gitSSHCommandEnv, "/bin/false")
	t.Setenv(config.SyncPeersEnv, prtAliasNotARemote)
	allowTestPushTarget(t)
	t.Chdir(f.clientDir)

	want := peerRemoteFallbackTarget(prtAliasNotARemote, peerRepoPath())
	if got := finishCleanupRowTargetResolution(f.clientDir).Rendered; got != want {
		t.Fatalf("fixture: want the fallback target %q, got %q", want, got)
	}
	rep := prtChainReport(t, f)
	detail, ok := prtStep(rep, finishCleanupPushVerb)
	if !ok {
		t.Fatalf("a failed delivery must still be REPORTED as a step: %+v", rep)
	}
	for _, wantSub := range []string{prtAliasNotARemote, config.SyncPeersSettingsKey, "server", want} {
		if !strings.Contains(detail, wantSub) {
			t.Errorf("the refusal must name %q — the four facts the pre-fix report omitted (Rule 15): %q", wantSub, detail)
		}
	}
	// And the peer's own ref was never touched: a refused delivery is not a
	// partial one.
	if got := leakShimRefState(t, f.serverDir, f.featBranch); got != "" {
		t.Errorf("a refused push must leave the peer's ref alone, found %s", got)
	}
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("a refused delivery must not consume the run's ground: %v", err)
	}
}
