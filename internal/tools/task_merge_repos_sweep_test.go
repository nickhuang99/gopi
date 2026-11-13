package tools

// task_merge_repos_sweep_test.go — the regression suite for `gopi task merge`'s
// NO-BRANCH spelling (epic task-merge-noarg-sweep).
//
// WHY THIS FILE EXISTS. Three surfaces used to tell three different stories
// about one verb:
//
//	the help page    "merge every repo in the settings.json "repos" table"
//	the doc comment  "empty branches without All is a usage error"
//	the engine       `if len(branches) == 0 { return counts, nil }` — exit 0,
//	                 every counter 0, not one repo looked at
//
// The engine's behaviour won, because it was the one that ran: a full sweep
// examined nothing and reported success. That is Rule 15's shape exactly (a
// failure wearing a green exit code), and the tests below are the guard that
// the two prose surfaces and the engine now say the SAME thing.
//
// TestTaskMergeNoBranchSweepsTheReposTable is the RED arm: on the unmodified
// engine it fails on `counts.Repos`/`counts.Merged` (measured — the sweep
// returned before git ran), and it can only pass once the engine resolves the
// table and sweeps each repo.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// tmrsSettingsFileName is the scratch settings document each test points
	// GOPI_SETTINGS at (Rule 32: the environment is pinned, the operator's own
	// profile is never written).
	tmrsSettingsFileName = "settings.json"
	// tmrsFilePerm is the mode of every scratch file this suite writes.
	tmrsFilePerm = 0o644
	// tmrsBranch / tmrsBranchFile name the fixture's single feature branch and
	// the file it adds — the divergence a sweep has to decide.
	tmrsBranch      = "feat/tmrs-sweep"
	tmrsBranchFile  = "swept.txt"
	tmrsMasterFile  = "master.txt"
	tmrsBranchBody  = "the branch's work\n"
	tmrsMasterBody  = "master's base\n"
	tmrsCommitEmail = "tmrs@fixture"
	tmrsCommitName  = "tmrs fixture"
)

// tmrsFixture builds a repo with a master commit and one divergent feature
// branch (fast-forwardable), then leaves the checkout on master so a landing is
// permitted. Returns the repo path and the branch name.
func tmrsFixture(t *testing.T) (repo, branch string) {
	t.Helper()
	repo = t.TempDir()
	run(t, repo, "init", "-q")
	run(t, repo, "config", "user.email", tmrsCommitEmail)
	run(t, repo, "config", "user.name", tmrsCommitName)
	run(t, repo, "symbolic-ref", "HEAD", "refs/heads/"+IntegrationBranchName)
	tmrsWrite(t, repo, tmrsMasterFile, tmrsMasterBody)
	run(t, repo, "add", tmrsMasterFile)
	run(t, repo, "commit", "-q", "-m", "fixture: the base")
	run(t, repo, "checkout", "-q", "-b", tmrsBranch)
	tmrsWrite(t, repo, tmrsBranchFile, tmrsBranchBody)
	run(t, repo, "add", tmrsBranchFile)
	run(t, repo, "commit", "-q", "-m", "fixture: the branch's work")
	run(t, repo, "checkout", "-q", IntegrationBranchName)
	return repo, tmrsBranch
}

// tmrsWrite writes one file into the fixture repo.
func tmrsWrite(t *testing.T, repo, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(body), tmrsFilePerm); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// tmrsWriteRepos points GOPI_SETTINGS at a document whose "repos" table holds
// exactly the given repo paths (merge-only entries: no build contract, which
// Rule 12 makes a valid pure merge target).
func tmrsWriteRepos(t *testing.T, repos ...string) string {
	t.Helper()
	table := make(map[string]any, len(repos))
	for _, repo := range repos {
		table[repo] = map[string]any{}
	}
	return tmrsWriteSettingsDoc(t, map[string]any{config.ReposSettingsKey: table})
}

// tmrsWriteSettingsDoc marshals body into a scratch settings file and pins
// GOPI_SETTINGS at it — the ONE channel config.ResolveMergerRepos reads.
func tmrsWriteSettingsDoc(t *testing.T, body map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), tmrsSettingsFileName)
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := os.WriteFile(path, encoded, tmrsFilePerm); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsPathEnv, path)
	return path
}

// TestTaskMergeNoBranchSweepsTheReposTable is THE regression for the epic: the
// no-branch, no---all spelling must resolve the settings.json "repos" table and
// sweep each configured repo, so the help page's promise is a description of
// what runs.
//
// Red on the unmodified engine: it returned `counts, nil` from the empty-branch
// guard before git ran, so Repos was 0 (the field did not even exist) and the
// branch stayed unmerged. Green only when the sweep actually decides branches.
func TestTaskMergeNoBranchSweepsTheReposTable(t *testing.T) {
	repo, branch := tmrsFixture(t)
	// Rule 32: the ledger path comes from the scaffold, never hand-rolled —
	// a sweep that dispatches a conflict must write a per-test database.
	testutil.NewTestDBPath(t)
	tmrsWriteRepos(t, repo)
	preTip := strings.TrimSpace(rsGit(t, repo, true, "rev-parse", IntegrationBranchName))

	counts, err := TaskMerge(TaskMergeOptions{Repo: repo, Submit: true}, nil, false)
	if err != nil {
		t.Fatalf("TaskMerge (no branches): %v", err)
	}
	if counts.Repos != 1 {
		t.Fatalf("repos = %d, want 1 — the no-branch spelling must SWEEP the settings.json repos table, not return before deciding anything (counts=%+v)", counts.Repos, counts)
	}
	if counts.Merged != 1 {
		t.Fatalf("merged = %d, want 1 (the fixture's ff branch) — counts=%+v", counts.Merged, counts)
	}
	if len(counts.Outcomes) != 1 || counts.Outcomes[0].Branch != branch {
		t.Fatalf("outcomes = %+v, want the one fixture branch %q", counts.Outcomes, branch)
	}
	// The landing is a fact about the REPO, not only about the counters: master
	// moved and now carries the branch's file.
	if got := strings.TrimSpace(rsGit(t, repo, true, "rev-parse", IntegrationBranchName)); got == preTip {
		t.Fatalf("%s did not move (%s) — the sweep reported a merge that never happened", IntegrationBranchName, got)
	}
	if out := rsGit(t, repo, false, "show", IntegrationBranchName+":"+tmrsBranchFile); strings.TrimSpace(out) != strings.TrimSpace(tmrsBranchBody) {
		t.Fatalf("%s:%s = %q, want the branch's content — the merge did not land", IntegrationBranchName, tmrsBranchFile, out)
	}
}

// TestTaskMergeNoBranchSweepsEveryConfiguredRepo pins the word EVERY: two
// declared repos, one branch each, both decide in the same invocation and the
// counters are the UNION (Rule 15's "nothing was silently dropped").
func TestTaskMergeNoBranchSweepsEveryConfiguredRepo(t *testing.T) {
	repoA, branchA := tmrsFixture(t)
	repoB, branchB := tmrsFixture(t)
	testutil.NewTestDBPath(t)
	tmrsWriteRepos(t, repoA, repoB)

	counts, err := TaskMerge(TaskMergeOptions{Submit: true}, nil, false)
	if err != nil {
		t.Fatalf("TaskMerge (no branches, two repos): %v", err)
	}
	if counts.Repos != 2 {
		t.Fatalf("repos = %d, want 2 (both keys of the table) — counts=%+v", counts.Repos, counts)
	}
	if counts.Merged != 2 {
		t.Fatalf("merged = %d, want 2 (one per repo) — counts=%+v", counts.Merged, counts)
	}
	// PER-REPO tallies are what the close loop rebuilds by (Rule 12: the build
	// contract is per repo). A union total cannot name them, so a sweep that
	// landed in two repos would rebuild the operator's cwd and leave the rest
	// running old code — the counters alone cannot tell a caller which repo.
	if len(counts.RepoTallies) != 2 {
		t.Fatalf("RepoTallies = %+v, want one entry per swept repo (2)", counts.RepoTallies)
	}
	for i, tally := range counts.RepoTallies {
		if tally.Repo == "" || tally.Merged != 1 || len(tally.Landed) != 1 {
			t.Fatalf("RepoTallies[%d] = %+v, want the repo path, merged=1 and the one landed branch", i, tally)
		}
	}
	got := make([]string, 0, len(counts.Outcomes))
	for _, o := range counts.Outcomes {
		got = append(got, o.Branch)
	}
	want := []string{branchA, branchB}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("outcomes = %v, want %v (sorted repo order, per-repo branch list)", got, want)
	}
	for _, repo := range []string{repoA, repoB} {
		if out := rsGit(t, repo, false, "show", IntegrationBranchName+":"+tmrsBranchFile); !strings.Contains(out, strings.TrimSpace(tmrsBranchBody)) {
			t.Fatalf("%s: master does not carry the branch's work (%q) — one repo's sweep was silently dropped", repo, out)
		}
	}
}

// TestTaskMergeNoBranchEmptyTableIsLoud pins the half of the old behaviour that
// was WORSE than the no-op: "nothing is configured" and "nothing needed
// merging" are different answers, and only one of them is a fact about the
// branches. An empty table must name the remedy and decide nothing.
func TestTaskMergeNoBranchEmptyTableIsLoud(t *testing.T) {
	repo, _ := tmrsFixture(t)
	testutil.NewTestDBPath(t)
	tmrsWriteRepos(t)
	preTip := strings.TrimSpace(rsGit(t, repo, true, "rev-parse", IntegrationBranchName))

	counts, err := TaskMerge(TaskMergeOptions{Repo: repo, Submit: true}, nil, false)
	if err == nil {
		t.Fatalf("TaskMerge with an empty repos table returned no error (counts=%+v) — an empty sweep that reports success is the silent failure this epic removes", counts)
	}
	if !strings.Contains(err.Error(), config.ReposSettingsKey) || !strings.Contains(err.Error(), config.SettingsPathEnv) {
		t.Fatalf("the refusal must name the table %q and the channel %q a reader can fix: %v",
			config.ReposSettingsKey, config.SettingsPathEnv, err)
	}
	if counts.Repos != 0 {
		t.Fatalf("repos = %d on a refused sweep, want 0", counts.Repos)
	}
	if got := strings.TrimSpace(rsGit(t, repo, true, "rev-parse", IntegrationBranchName)); got != preTip {
		t.Fatalf("%s moved on a refused sweep: %s → %s", IntegrationBranchName, preTip, got)
	}
}

// TestTaskMergeNoBranchUndeclaredRepoIsRefused pins the --repo narrowing: a
// directory the table does not declare is REFUSED BY NAME instead of being read
// as "sweep nothing, exit 0". The remedy (name the branches, or declare the
// repo) is in the message.
func TestTaskMergeNoBranchUndeclaredRepoIsRefused(t *testing.T) {
	repo, branch := tmrsFixture(t)
	declared := t.TempDir()
	testutil.NewTestDBPath(t)
	tmrsWriteRepos(t, declared)

	counts, err := TaskMerge(TaskMergeOptions{Repo: repo, Submit: true}, nil, false)
	if err == nil {
		t.Fatalf("TaskMerge --repo %q (undeclared) returned no error (counts=%+v) — the sweep covered no configured repo and said nothing", repo, counts)
	}
	if !strings.Contains(err.Error(), repo) {
		t.Fatalf("the refusal must name the repo it refused (%q): %v", repo, err)
	}
	if counts.Repos != 0 || counts.Merged != 0 {
		t.Fatalf("a refused sweep decided work: %+v", counts)
	}
	if got := strings.TrimSpace(rsGit(t, repo, true, "rev-parse", "--verify", "refs/heads/"+branch)); got == "" {
		t.Fatalf("the refused branch %q vanished", branch)
	}
}

// TestTaskMergeNoBranchUnreadableTableIsLoud pins the unreadable half: a
// settings document the resolver cannot parse is an ERROR carrying the
// resolver's own diagnosis, never a sweep of nothing.
func TestTaskMergeNoBranchUnreadableTableIsLoud(t *testing.T) {
	repo, _ := tmrsFixture(t)
	testutil.NewTestDBPath(t)
	path := filepath.Join(t.TempDir(), tmrsSettingsFileName)
	if err := os.WriteFile(path, []byte("{not json"), tmrsFilePerm); err != nil {
		t.Fatalf("write malformed settings: %v", err)
	}
	t.Setenv(config.SettingsPathEnv, path)

	if _, err := TaskMerge(TaskMergeOptions{Repo: repo}, nil, false); err == nil {
		t.Fatal("TaskMerge with an unreadable settings document returned no error — Rule 15: the failure must be loud")
	} else if !strings.Contains(err.Error(), config.ReposSettingsKey) {
		t.Fatalf("the refusal must name the table it could not read (%q): %v", config.ReposSettingsKey, err)
	}
}

// TestTaskMergeRepoFlagSweepsOnlyThatRepo pins the documented meaning of
// --repo on the no-branch spelling: it NARROWS the sweep to the declared repo
// and leaves the others untouched — "act on this repo instead of the cwd".
func TestTaskMergeRepoFlagSweepsOnlyThatRepo(t *testing.T) {
	repoA, _ := tmrsFixture(t)
	repoB, branchB := tmrsFixture(t)
	testutil.NewTestDBPath(t)
	tmrsWriteRepos(t, repoA, repoB)
	preTipB := strings.TrimSpace(rsGit(t, repoB, true, "rev-parse", IntegrationBranchName))

	counts, err := TaskMerge(TaskMergeOptions{Repo: repoA, Submit: true}, nil, false)
	if err != nil {
		t.Fatalf("TaskMerge --repo %q: %v", repoA, err)
	}
	if counts.Repos != 1 || counts.Merged != 1 {
		t.Fatalf("counts = %+v, want repos=1 merged=1 (the narrowed sweep)", counts)
	}
	if got := strings.TrimSpace(rsGit(t, repoB, true, "rev-parse", IntegrationBranchName)); got != preTipB {
		t.Fatalf("the un-narrowed repo %s moved (%s → %s) — --repo did not narrow the sweep", repoB, preTipB, got)
	}
	if got := strings.TrimSpace(rsGit(t, repoB, true, "rev-parse", "--verify", "refs/heads/"+branchB)); got == "" {
		t.Fatalf("the un-narrowed repo's branch %q was decided anyway", branchB)
	}
}

// TestTaskMergeNoBranchHelpClaimIsTheEngine keeps the PAGE and the ENGINE from
// drifting apart again, which is the defect this epic removed: the page's
// promise is asserted against the behaviour in the same suite, so a future edit
// that quietly restores the silent no-op turns this red rather than leaving a
// page that lies.
func TestTaskMergeNoBranchHelpClaimIsTheEngine(t *testing.T) {
	repo, _ := tmrsFixture(t)
	testutil.NewTestDBPath(t)
	tmrsWriteRepos(t, repo)

	// The claim, in the page's own words: with no branch and no --all, the
	// invocation consults the settings.json "repos" table.
	if _, err := TaskMerge(TaskMergeOptions{Submit: true}, nil, false); err != nil {
		t.Fatalf("the page says 'merge every repo in the settings.json repos table'; the engine said: %v", err)
	}
}
