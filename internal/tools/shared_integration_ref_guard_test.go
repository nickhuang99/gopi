package tools

// shared_integration_ref_guard_test.go — the acceptance set for the shared
// `refs/heads/*` namespace guard (PROJECT.md Rule 5: every fix ships a minimal
// repro test; Rule 31: a surface in no execution has not been tested).
//
// The fixture is the incident's own shape reduced to its smallest form: a
// repository with ONE commit, its MAIN checkout holding the integration branch,
// plus ONE linked worktree holding a different branch. That is the state in
// which `refs/heads/<the main checkout's branch>` stops being "my branch" and
// becomes a ref another checkout is standing on.
//
// TWO tests, and the split is the point:
//
//	TestSharedIntegrationRefGuard_ReproducesTheIncidentWithoutTheGuard
//	    the RED half, run through RAW git (exec.Command, no BashInDir): the
//	    measured command really does roll the main checkout's branch back and
//	    leave it tracked-dirty. The guard's predicate is NOT on that path, which
//	    is why the damage lands. This is the proof the guard is answering a
//	    real defect rather than a hypothetical one.
//	TestSharedIntegrationRefGuard_AcceptanceSet
//	    the GREEN half plus every no-weakening case, each driven through
//	    BashInDir — the REAL bash-tool entry point, so a case can only pass if
//	    the guard is wired into the tool it governs and not merely present in the
//	    package beside it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/branchform"
)

// Fixture spellings, named because several cases and assertions share them
// (Rule 1: a value used twice is a value with one home). They are FIXTURE DATA —
// the guard itself is branch-name agnostic, which the `task/*` and `refs/gopi/*`
// cases below are exactly what pins.
const (
	refGuardFixtureTrunkBranch = "main"
	refGuardFixtureOtherBranch = "other"
	refGuardFixtureNewBranch   = "task/01a12da8fixture"
	refGuardFixtureTagRef      = "refs/gopi/archive-reason/refguard-probe"
	// The verb-coverage rows (shared_integration_ref_guard_verbs_test.go) need a
	// SOURCE branch to force-move FROM and to fetch FROM, a free name to rename it
	// to, and the remote name the config-driven fetch row configures.
	refGuardFixtureSrcBranch        = "refguard-src"
	refGuardFixtureSrcRenamedBranch = "refguard-src-renamed"
	refGuardFixtureRenamedBranch    = "main-renamed"
	refGuardFixtureOriginRemote     = "origin"
	// The verb-coverage rows create a second worktree through GOPI.md's own
	// branch-creation step, which spells the force-create flag in its long form.
	refGuardWorktreeSuffix = "-b"
	// The ref a `symbolic-ref` no-weakening row writes: a free ref whose TARGET is
	// a branch a third checkout holds (the target is READ, never written).
	refGuardFixtureSymrefName = "refs/heads/refguard-symref"
	// refGuardFixtureOriginBody is the content the bare origin's tip carries. It is
	// deliberately DIFFERENT from both fixture bodies: the config-driven fetch proof
	// observes the park through `git status --porcelain`, and an origin whose tree
	// matched the trunk checkout's would move the ref while staging NOTHING —
	// measured, and the reason this constant exists rather than a shared body.
	refGuardFixtureOriginBody = "origin-tip\n"
	// refGuardRemoteTrackingDestFmt is git's DEFAULT fetch mapping destination
	// (`refs/remotes/<remote>/*`): tracking refs, no local branch — the shape an
	// ordinary `git fetch` writes, which the guard must not touch.
	refGuardRemoteTrackingDestFmt = "refs/remotes/%s/*"
	refGuardFixtureTrunkFile      = "trunk.txt"
	refGuardFixtureSeedBody       = "one\n"
	refGuardFixtureSecondBody     = "two\n"

	// The RED half's HOLDER-DETACH protocol (measured on git 2.43.0, this run's
	// probe in the session's artifacts/rgprobe4.out): `git branch -f`, `branch -M`
	// and a refspec `fetch` aimed at a branch ANOTHER worktree has checked out are
	// refused by git ITSELF while that holder is ATTACHED — exit 128, "cannot force
	// update the branch 'main' used by worktree at …" / "refusing to fetch into
	// branch 'refs/heads/main' checked out at …" — so a RED proof that runs the raw
	// form against an attached holder measures git's refusal instead of the shared
	// namespace write. The refusal keys on the HOLDER'S HEAD, so detaching the
	// holder's HEAD (`checkout --detach`, which touches no tree and no index) puts
	// the raw write back on the path it takes in the un-guarded world, and
	// re-attaching the holder to the (now moved) ref afterwards renders the
	// incident's staged park. Both spellings are named here (Rule 1/13); the
	// protocol itself lives in refGuardDetachHolder / refGuardReattachHolder.
	refGuardHolderDetachVerb   = "checkout"
	refGuardHolderDetachFlag   = "--detach"
	refGuardHolderReattachVerb = "symbolic-ref"
	// refGuardHeadRefName is the symbolic ref re-attached: git's own name for the
	// per-worktree HEAD (the same spelling the guard's sibling readers use).
	refGuardHeadRefName = "HEAD"
)

// refGuardGit runs a git command in dir for the fixture. A fixture that cannot
// be built is not a passing test, so failure is fatal here.
func refGuardGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(gitBinaryName, args...)
	cmd.Dir = dir
	cmd.Env = append(withoutGitDirEnv(os.Environ()),
		"GIT_AUTHOR_NAME=refguard", "GIT_AUTHOR_EMAIL=refguard@test",
		"GIT_COMMITTER_NAME=refguard", "GIT_COMMITTER_EMAIL=refguard@test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// refGuardRawGit runs a git command OUTSIDE the bash tool — no guard, no
// policy — and returns its combined output and error. It is the RED half's
// instrument: the path the measured incident's command actually came through.
func refGuardRawGit(dir string, args ...string) (string, error) {
	cmd := exec.Command(gitBinaryName, args...)
	cmd.Dir = dir
	cmd.Env = withoutGitDirEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// refGuardFixture is a two-checkout repository plus the shas the incident's
// command form needs (an OLDER commit to roll the shared ref back to).
type refGuardFixture struct {
	base string
	// trunk is the MAIN checkout, standing on refGuardFixtureTrunkBranch.
	trunk string
	// other is a LINKED worktree of the same repository, standing on
	// refGuardFixtureOtherBranch.
	other string
	// older is the first commit; head is the second. The incident rolled the
	// shared ref from a newer position back to an older one.
	older string
	head  string
}

// refGuardBuildFixture materialises the fixture under the test's own scratch
// directory (t.TempDir(), which is pinned off the `/` partition by the suite's
// own entry-point pin — Rule 22).
func refGuardBuildFixture(t *testing.T) refGuardFixture {
	t.Helper()
	base := t.TempDir()
	f := refGuardFixture{
		base:  base,
		trunk: filepath.Join(base, "trunk-checkout"),
		other: filepath.Join(base, "linked-worktree"),
	}
	if err := os.MkdirAll(f.trunk, 0o755); err != nil {
		t.Fatalf("mkdir trunk checkout: %v", err)
	}
	refGuardGit(t, f.trunk, "init", "-q", "-b", refGuardFixtureTrunkBranch)
	if err := os.WriteFile(filepath.Join(f.trunk, refGuardFixtureTrunkFile), []byte(refGuardFixtureSeedBody), 0o644); err != nil {
		t.Fatalf("write trunk file: %v", err)
	}
	refGuardGit(t, f.trunk, "add", refGuardFixtureTrunkFile)
	refGuardGit(t, f.trunk, "commit", "-q", "-m", "seed")
	f.older = refGuardGit(t, f.trunk, "rev-parse", "HEAD")

	// A second commit, so the trunk checkout's index holds a tree that differs
	// from the older ref — the exact condition under which a ref roll-back
	// renders as a STAGED diff (the incident's 60 paths).
	if err := os.WriteFile(filepath.Join(f.trunk, refGuardFixtureTrunkFile), []byte(refGuardFixtureSecondBody), 0o644); err != nil {
		t.Fatalf("rewrite trunk file: %v", err)
	}
	refGuardGit(t, f.trunk, "add", refGuardFixtureTrunkFile)
	refGuardGit(t, f.trunk, "commit", "-q", "-m", "second")
	f.head = refGuardGit(t, f.trunk, "rev-parse", "HEAD")

	// The linked worktree: same repository (shared refs/heads), different branch.
	refGuardGit(t, f.trunk, "worktree", "add", "-q", f.other, "-b", refGuardFixtureOtherBranch)

	// A SOURCE branch at the OLDER sha: what `branch -f <held> <src>` forces the
	// held ref ONTO and what the fetch rows mirror, so a row that really moves the
	// held ref has somewhere to move it from.
	refGuardGit(t, f.trunk, "branch", refGuardFixtureSrcBranch, f.older)
	return f
}

// refGuardSharedRef reads the MAIN checkout's integration-branch ref — the value
// the incident moved, and the value every assertion re-reads rather than
// trusting an error string (R2).
func refGuardSharedRef(t *testing.T, f refGuardFixture) string {
	t.Helper()
	return refGuardGit(t, f.trunk, "rev-parse", "refs/heads/"+refGuardFixtureTrunkBranch)
}

// refGuardPorcelain returns the trunk checkout's `git status --porcelain`.
func refGuardPorcelain(t *testing.T, f refGuardFixture) string {
	t.Helper()
	return refGuardGit(t, f.trunk, "status", "--porcelain")
}

// refGuardStagedCount returns the number of STAGED (index-vs-ref) paths in the
// trunk checkout — the quantity the incident measured at 60.
func refGuardStagedCount(t *testing.T, f refGuardFixture) int {
	t.Helper()
	out := refGuardGit(t, f.trunk, "diff", "--cached", "--name-only")
	if out == "" {
		return 0
	}
	return len(strings.Split(out, "\n"))
}

// refGuardDetachHolder takes the HOLDER checkout's HEAD off its branch WITHOUT
// touching its tree or its index (`checkout --detach`), so a later raw write to
// that branch is no longer refused by git's own holder check — the state the
// un-guarded world's command would meet if nobody held the branch at all. It is
// half of the RED half's protocol; refGuardReattachHolder closes it.
func refGuardDetachHolder(t *testing.T, dir string) {
	t.Helper()
	refGuardGit(t, dir, refGuardHolderDetachVerb, refGuardHolderDetachFlag)
}

// refGuardReattachHolder points a detached checkout's HEAD back AT the branch ref
// by name (`symbolic-ref HEAD refs/heads/<b>`), without touching the tree or the
// index. This is what renders a post-detach ref move as the incident's damage: the
// checkout's HEAD follows the ref to the older commit while its index still holds
// the pulled tree, so `git status` reports STAGED paths (the Rule 30 park). It
// asserts the same three preconditions the incident's shape has — HEAD is on the
// moved ref, nothing is UNSTAGED, nothing is UNTRACKED — so a protocol that
// accidentally dirtied the tree cannot be mistaken for the reproduced park.
func refGuardReattachHolder(t *testing.T, dir, branch string) {
	t.Helper()
	refGuardGit(t, dir, refGuardHolderReattachVerb, refGuardHeadRefName, refsHeadsPrefix+branch)
	if got := refGuardGit(t, dir, "symbolic-ref", "--quiet", refGuardHeadRefName); got != refsHeadsPrefix+branch {
		t.Fatalf("holder re-attach: HEAD is %q, want %q", got, refsHeadsPrefix+branch)
	}
	if dirty := refGuardGit(t, dir, "diff", "--name-only"); dirty != "" {
		t.Fatalf("holder re-attach staged the tree by a route other than the ref: UNSTAGED changes %q", dirty)
	}
	if untracked := refGuardGit(t, dir, "ls-files", "--others", "--exclude-standard"); untracked != "" {
		t.Fatalf("holder re-attach left UNTRACKED files %q — this is not the incident's shape", untracked)
	}
}

// refGuardIncidentCommand is the measured command form, verbatim: from a LINKED
// worktree, write the ref the MAIN checkout is standing on. The incident's own
// spelling is preserved (including the `-C` operand and the `2>/dev/null ||
// true` tail that kept the run from learning what it had done).
func refGuardIncidentCommand(f refGuardFixture, sha string) string {
	return gitBinaryName + " -C " + f.other + " " + refGuardUpdateRefVerb + " " +
		refsHeadsPrefix + refGuardFixtureTrunkBranch + " " + sha + " 2>/dev/null || true"
}

// TestSharedIntegrationRefGuard_ReproducesTheIncidentWithoutTheGuard is the RED
// half, and it must stay red-as-described: the command is run through RAW git,
// so the guard is provably ABSENT from the path that does the damage. It asserts
// the three measurements the incident's report carried —
//
//	the shared ref MOVED (the trunk checkout's refs/heads/<b> changed),
//	the trunk checkout is tracked-dirty, and
//	the dirt is STAGED (index-vs-ref), not unstaged and not untracked
//
// — which together are the mechanical reproduction of the Rule 30 merge-queue
// park. It then restores the fixture, so the damage is measured and undone
// rather than left for a later case to trip over.
func TestSharedIntegrationRefGuard_ReproducesTheIncidentWithoutTheGuard(t *testing.T) {
	f := refGuardBuildFixture(t)
	before := refGuardSharedRef(t, f)
	if before != f.head {
		t.Fatalf("fixture precondition: trunk ref is %s, want the second commit %s", before, f.head)
	}
	if got := refGuardPorcelain(t, f); got != "" {
		t.Fatalf("fixture precondition: a fresh fixture must be clean, got %q", got)
	}

	// The guard's predicate IS defined for this command — that is the link
	// between the reproduced damage and the thing being fixed. It is asserted
	// here so the RED half cannot silently drift away from the shape the GREEN
	// half refuses.
	if reason := guardSharedIntegrationRef(refGuardIncidentCommand(f, f.older), f.other); reason == "" {
		t.Fatalf("the guard's predicate does not cover the measured command form — the RED and GREEN halves have drifted apart")
	}

	// ── THE DAMAGE: raw git, no BashInDir, no guard ─────────────────────────
	cmd := gitBinaryName + " -C " + f.other
	args := []string{"-C", f.other, refGuardUpdateRefVerb, refsHeadsPrefix + refGuardFixtureTrunkBranch, f.older}
	if out, err := refGuardRawGit(f.other, args...); err != nil {
		t.Fatalf("the measured command form must SUCCEED when the guard is absent (%s), got: %v\n%s", cmd, err, out)
	}

	// 1. the SHARED ref moved — read from the trunk checkout, not from the caller.
	if after := refGuardSharedRef(t, f); after != f.older {
		t.Fatalf("RED proof: the trunk checkout's refs/heads/%s is %s, want the rolled-back %s — the shared namespace write did not reproduce",
			refGuardFixtureTrunkBranch, after, f.older)
	}
	// 2. the trunk checkout is tracked-dirty.
	porcelain := refGuardPorcelain(t, f)
	if porcelain == "" {
		t.Fatalf("RED proof: the trunk checkout stayed clean after the roll-back — the Rule 30 park did not reproduce")
	}
	// 3. the dirt is STAGED: index-vs-ref, with nothing unstaged and nothing
	//    untracked. This is the incident's signature (60 staged, 0 unstaged, 0
	//    untracked) and it is what distinguishes a ref roll-back from a human
	//    writing code.
	staged := refGuardStagedCount(t, f)
	if staged == 0 {
		t.Fatalf("RED proof: trunk porcelain is %q but nothing is STAGED — this is not the incident's shape", porcelain)
	}
	if dirty := refGuardGit(t, f.trunk, "diff", "--name-only"); dirty != "" {
		t.Fatalf("RED proof: the trunk checkout has UNSTAGED changes (%q) — the incident's shape is staged-only", dirty)
	}
	if untracked := refGuardGit(t, f.trunk, "ls-files", "--others", "--exclude-standard"); untracked != "" {
		t.Fatalf("RED proof: the trunk checkout has UNTRACKED files (%q) — the incident's shape has none", untracked)
	}
	t.Logf("RED reproduced: shared ref %s → %s, trunk staged paths = %d, porcelain = %q",
		f.head[:12], f.older[:12], staged, porcelain)

	// Restore the fixture: the RED proof measured the damage, it does not keep it.
	refGuardGit(t, f.trunk, "reset", "-q", "--hard", f.head)
	if got := refGuardSharedRef(t, f); got != f.head {
		t.Fatalf("fixture restore failed: ref is %s, want %s", got, f.head)
	}
	if got := refGuardPorcelain(t, f); got != "" {
		t.Fatalf("fixture restore failed: trunk is still dirty: %q", got)
	}
}

// refGuardAcceptanceCase is one row of the acceptance set. refuse == false means
// the command must be PERMITTED (BashInDir returns no error); refuse == true
// means it must be REFUSED, and the `want` strings must all appear in the
// refusal — a refusal that stops a session without naming the repo, the branch,
// the worktree that holds it and the non-destructive alternative is the silent
// failure Rule 15 forbids.
type refGuardAcceptanceCase struct {
	name   string
	dir    string
	cmd    string
	refuse bool
	want   []string
}

// TestSharedIntegrationRefGuard_AcceptanceSet is the acceptance set in ONE test,
// every case driven through BashInDir (Rule 31). The GREEN half re-reads the
// fixture's ref after the refusal, so "nothing was written" is PROVED rather
// than inferred from the error string.
func TestSharedIntegrationRefGuard_AcceptanceSet(t *testing.T) {
	if escapeDangerous {
		// An auditable skip, not a silent one (Rule 15/27): the guard shares the
		// operator's GOPI_ALLOW_DANGEROUS hatch by design, and with the hatch
		// open this acceptance set cannot observe its verdict.
		t.Skip("GOPI_ALLOW_DANGEROUS is set: the guard shares that hatch by design, so the refusal cannot be observed in this process")
	}
	f := refGuardBuildFixture(t)

	// A second repository with exactly ONE checkout: the ordinary local flow,
	// where no ref is shared with anybody.
	single := filepath.Join(f.base, "single-checkout")
	if err := os.MkdirAll(single, 0o755); err != nil {
		t.Fatalf("mkdir single checkout: %v", err)
	}
	refGuardGit(t, single, "init", "-q", "-b", refGuardFixtureTrunkBranch)
	if err := os.WriteFile(filepath.Join(single, refGuardFixtureTrunkFile), []byte(refGuardFixtureSeedBody), 0o644); err != nil {
		t.Fatalf("write single file: %v", err)
	}
	refGuardGit(t, single, "add", refGuardFixtureTrunkFile)
	refGuardGit(t, single, "commit", "-q", "-m", "seed")
	singleHead := refGuardGit(t, single, "rev-parse", "HEAD")

	cases := []refGuardAcceptanceCase{
		// ── REFUSED: the measured incident, verbatim ────────────────────────
		{
			name:   "refuse/measured-incident-form",
			dir:    f.other,
			cmd:    refGuardIncidentCommand(f, f.older),
			refuse: true,
			want: []string{
				f.trunk,                    // the repo whose shared branch it is
				refGuardFixtureTrunkBranch, // the branch
				f.other,                    // the worktree the command came from
				"ANOTHER worktree",         // that a different checkout holds it
				"Rule 30",                  // the consequence it would cause
				"refs/heads/" + refGuardFixtureTrunkBranch,
				"gopi tool sandbox run", // the non-destructive route for a fixture
			},
		},
		{
			name:   "refuse/no-error-suppression",
			dir:    f.other,
			cmd:    "git -C " + f.other + " update-ref refs/heads/" + refGuardFixtureTrunkBranch + " " + f.older,
			refuse: true,
			want:   []string{f.trunk, refGuardFixtureTrunkBranch, f.other},
		},
		{
			name:   "refuse/caller-cwd-is-the-worktree",
			dir:    f.other,
			cmd:    "git update-ref refs/heads/" + refGuardFixtureTrunkBranch + " " + f.older,
			refuse: true,
			want:   []string{f.trunk, refGuardFixtureTrunkBranch},
		},
		{
			name:   "refuse/deletion-of-a-held-branch",
			dir:    f.other,
			cmd:    "git -C " + f.other + " update-ref " + refGuardDeleteFlag + " refs/heads/" + refGuardFixtureTrunkBranch,
			refuse: true,
			want:   []string{f.trunk, refGuardFixtureTrunkBranch},
		},
		{
			name:   "refuse/symbolic-ref-repoint-of-a-held-branch",
			dir:    f.other,
			cmd:    "git -C " + f.other + " symbolic-ref refs/heads/" + refGuardFixtureTrunkBranch + " " + refsHeadsPrefix + refGuardFixtureOtherBranch,
			refuse: true,
			want:   []string{f.trunk, refGuardFixtureTrunkBranch},
		},
		{
			name:   "refuse/peeled-name-spelling",
			dir:    f.other,
			cmd:    "git -C " + f.other + " update-ref 'refs/heads/" + refGuardFixtureTrunkBranch + "^{commit}' " + f.older,
			refuse: true,
			want:   []string{f.trunk, refGuardFixtureTrunkBranch},
		},
		{
			name:   "refuse/chained-after-a-read-only-clause",
			dir:    f.other,
			cmd:    "git rev-parse --show-toplevel; echo '---run---'; git -C " + f.other + " update-ref refs/heads/" + refGuardFixtureTrunkBranch + " " + f.older,
			refuse: true,
			want:   []string{f.trunk, refGuardFixtureTrunkBranch},
		},

		// ── PERMITTED: the caller's OWN branch and own-checkout writes ──────
		{
			// The caller's own worktree holds `other`, so writing it is the
			// ordinary branch write.
			name:   "permit/own-branch-from-its-own-worktree",
			dir:    f.other,
			cmd:    "git -C " + f.other + " update-ref refs/heads/" + refGuardFixtureOtherBranch + " " + f.head,
			refuse: false,
		},
		{
			// The MAIN checkout writing its own integration branch: this is the
			// merger's, finish-cleanup's and the sync lane's own path, and it
			// must never be refused.
			name:   "permit/main-checkout-moves-its-own-branch",
			dir:    f.trunk,
			cmd:    "git -C " + f.trunk + " update-ref refs/heads/" + refGuardFixtureTrunkBranch + " " + f.head,
			refuse: false,
		},
		{
			name:   "permit/task-branch-nobody-holds",
			dir:    f.other,
			cmd:    "git -C " + f.other + " update-ref refs/heads/" + refGuardFixtureNewBranch + " " + f.head,
			refuse: false,
		},
		{
			name:   "permit/refs-gopi-namespace",
			dir:    f.other,
			cmd:    "git -C " + f.other + " update-ref " + refGuardFixtureTagRef + " " + f.head,
			refuse: false,
		},
		{
			name:   "permit/detached-head-symbolic-ref",
			dir:    f.other,
			cmd:    "git -C " + f.other + " symbolic-ref " + refGuardDetachedBranch + " " + refsHeadsPrefix + refGuardFixtureOtherBranch,
			refuse: false,
		},

		// ── PERMITTED: read-only probes and non-governed verbs ──────────────
		{
			name:   "permit/read-only-rev-parse",
			dir:    f.other,
			cmd:    "git -C " + f.other + " rev-parse refs/heads/" + refGuardFixtureTrunkBranch,
			refuse: false,
		},
		{
			name:   "permit/read-only-reflog",
			dir:    f.other,
			cmd:    "git -C " + f.trunk + " reflog show refs/heads/" + refGuardFixtureTrunkBranch,
			refuse: false,
		},
		{
			name:   "permit/git-log-and-status",
			dir:    f.other,
			cmd:    "git -C " + f.other + " log --oneline -1; git -C " + f.trunk + " status --porcelain",
			refuse: false,
		},
		{
			// A line with no git INVOCATION: the tokens are arguments to echo.
			name:   "permit/echo-of-the-refname-is-not-a-write",
			dir:    f.other,
			cmd:    "echo 'git " + refGuardUpdateRefVerb + " " + refsHeadsPrefix + refGuardFixtureTrunkBranch + " deadbeef'",
			refuse: false,
		},
		{
			name:   "permit/update-ref-outside-any-repo",
			dir:    f.base,
			cmd:    "true; git -C " + f.base + " rev-parse --show-toplevel 2>/dev/null || true",
			refuse: false,
		},

		// ── PERMITTED: a single-checkout repository has nothing to share ────
		{
			name:   "permit/single-checkout-repo-moves-its-own-ref",
			dir:    single,
			cmd:    "git -C " + single + " update-ref " + refsHeadsPrefix + refGuardFixtureTrunkBranch + " " + singleHead,
			refuse: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := BashInDir(c.cmd, c.dir)
			if !c.refuse {
				if err != nil {
					t.Fatalf("%q in %s must be PERMITTED, got refusal: %v (output %q)", c.cmd, c.dir, err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("%q in %s must be REFUSED (git ran it), got output: %q", c.cmd, c.dir, out)
			}
			refusal := err.Error()
			for _, want := range c.want {
				if !strings.Contains(refusal, want) {
					t.Errorf("%q: refusal must name %q, got:\n%s", c.cmd, want, refusal)
				}
			}
			// The refusal must also say that NOTHING ran — the difference
			// between a refusal and a failed command.
			if !strings.Contains(refusal, "dangerous command rejected:") {
				t.Errorf("%q: refusal is not on the dangerous-command channel:\n%s", c.cmd, refusal)
			}
		})
	}
}

// TestSharedIntegrationRefGuard_RefusalWritesNothing re-reads the fixture after
// the refusal: the measurement, not the error string, is the evidence that the
// shared ref was untouched (R2). It is a separate test so the assertion runs
// against a fixture whose ONLY operation was the refused command.
func TestSharedIntegrationRefGuard_RefusalWritesNothing(t *testing.T) {
	if escapeDangerous {
		t.Skip("GOPI_ALLOW_DANGEROUS is set: the guard shares that hatch by design, so the refusal cannot be observed in this process")
	}
	f := refGuardBuildFixture(t)
	before := refGuardSharedRef(t, f)

	out, err := BashInDir(refGuardIncidentCommand(f, f.older), f.other)
	if err == nil {
		t.Fatalf("the measured command form must be refused, got output: %q", out)
	}
	if after := refGuardSharedRef(t, f); after != before {
		t.Fatalf("the refusal let the shared ref move: %s → %s (want unchanged at %s)", before, after, before)
	}
	if got := refGuardPorcelain(t, f); got != "" {
		t.Fatalf("the refusal wrote into the trunk checkout: porcelain %q", got)
	}
	if n := refGuardStagedCount(t, f); n != 0 {
		t.Fatalf("the refusal left %s staged paths in the trunk checkout", strconv.Itoa(n))
	}
}

// refGuardFixtureWorktreeAddB is GOPI.md's OWN branch-creation step, aimed at a
// name no worktree holds — the false-positive surface the widening must not touch.
// Git itself refuses the same command aimed at a held branch, so the guard leaves
// the whole `-B` family to git (measured: `worktree add -B <held>` exits 128
// "'<b>' is already checked out at '<other worktree>'").
func refGuardFixtureWorktreeAddB(f refGuardFixture) string {
	return gitBinaryName + " -C " + f.trunk + " " + finishCleanupWorktreeVerb + " add -q " +
		filepath.Join(f.base, "wt-from-add-b") + " " + branchform.FlagCreateForce + " " + refGuardFixtureNewBranch + "2 " + f.older
}

// refGuardBuildSingleCheckout builds a ONE-checkout repository inside base: the
// ordinary local flow, where every verb in the guard's scope is an everyday command
// and no ref is shared with anybody. It is the fixture that keeps the widening
// honest — a guard that refused here would refuse gopi's own local work.
func refGuardBuildSingleCheckout(t *testing.T, base string) string {
	t.Helper()
	return refGuardBuildSingleCheckoutNamed(t, base, "single-checkout")
}

// refGuardBuildSingleCheckoutNamed is the same fixture under a caller-chosen name,
// for the cases that need a SECOND one-checkout repository in the same run.
func refGuardBuildSingleCheckoutNamed(t *testing.T, base, name string) string {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir single checkout: %v", err)
	}
	refGuardGit(t, dir, "init", "-q", "-b", refGuardFixtureTrunkBranch)
	if err := os.WriteFile(filepath.Join(dir, refGuardFixtureTrunkFile), []byte(refGuardFixtureSeedBody), 0o644); err != nil {
		t.Fatalf("write single checkout file: %v", err)
	}
	refGuardGit(t, dir, "add", refGuardFixtureTrunkFile)
	refGuardGit(t, dir, "commit", "-q", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, refGuardFixtureTrunkFile), []byte(refGuardFixtureSecondBody), 0o644); err != nil {
		t.Fatalf("write single checkout second body: %v", err)
	}
	refGuardGit(t, dir, "add", refGuardFixtureTrunkFile)
	refGuardGit(t, dir, "commit", "-q", "-m", "second")
	return dir
}

// refGuardRefValue reads a ref's value, or "" when it does not exist. It is the
// NON-FATAL form of the fixture reader: the verb-coverage RED proofs ask about refs
// whose ABSENCE is the evidence (a caller's branch renamed away), so a missing ref
// is an answer rather than a fixture failure.
func refGuardRefValue(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := refGuardRawGit(dir, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
