// repo_sweep_report_only_test.go — the red-proof pair for the epic
// sweep-report-not-act (briefing briefings/brief-sweep-report-not-act.md).
//
// The epic's acceptance is an OBSERVATION, not an intention: the reported
// incident (a live session's branch and worktree deleted mid-session by the
// per-repo sweep) must be reconstructed in a fixture, and the tick must be shown
// to REPORT the branch and keep it — with the live owner NAMED in the alarm —
// instead of pruning it.
package tools

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// --- ① the incident, reconstructed ------------------------------------------

// TestRepoSweepWouldPruneReportsLiveBranchAndMutatesNothing reconstructs the
// reported incident and asserts the tick's new behaviour over it.
//
// THE SHAPE (it is the trap, so it is reproduced exactly): a branch whose tip is
// ALREADY AN ANCESTOR of the integration branch — because its work was merged
// and the owner kept working on the same branch — is still
//   - present in refs/heads/,
//   - checked out in a LIVE linked worktree,
//   - named by a LIVE ledger row.
//
// The old arm ① asked git one question (`merge-base --is-ancestor <branch>
// master`) and, on a yes, ran `branch -d` — a predicate that cannot tell a spent
// ref from a live one, because nothing in it asks WHO is using the branch. The
// test measures the verdict is still reached (the predicate is unchanged) and
// that the DISPOSITION changed: reported, not pruned.
//
// RED PROOF: this test fails on the pre-epic tree — there, `res.Reported` does
// not exist, `res.WouldPruneLive` is always 0, the ref is GONE after the tick
// and no `sweep-would-prune-live-branch` event is delivered. It is the
// observation the epic was dispatched for.
func TestRepoSweepWouldPruneReportsLiveBranchAndMutatesNothing(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := rsTestRepo(t)
	// The branch: master + one commit, merged back into master (--no-ff), so its
	// tip IS an ancestor of master while the branch keeps existing.
	rsReportGitOut(t, repo, "checkout", "-b", rsLiveBranch)
	writeFileT(t, filepath.Join(repo, "work.txt"), "delivered work\n")
	rsReportGitOut(t, repo, "add", "work.txt")
	rsReportGitOut(t, repo, "commit", "-m", "the branch's deliverable")
	tip := rsReportGitOut(t, repo, "rev-parse", rsLiveBranch)
	rsReportGitOut(t, repo, "checkout", "master")
	rsReportGitOut(t, repo, "merge", "--no-ff", "-m", "merge "+rsLiveBranch, rsLiveBranch)
	// The predicate the old arm ran, measured directly: it IS true. This is the
	// whole incident — a true verdict about a branch that is still in use.
	if _, err := repoSweepGit(repo, notFFMergeBaseVerb, notFFIsAncestorFlag, rsLiveBranch, masterBranchName); err != nil {
		t.Fatalf("fixture: the branch tip must be an ancestor of master: %v", err)
	}
	// A LIVE ledger row names the branch — the owner evidence the old predicate
	// never consulted.
	seedLiveRowForBranch(t, rsLiveBranch)

	// The alarm channel: the same notify lane the stale/build-failed/worktree-
	// reaped events use. Subscribed to the sweep's own event only.
	eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+eventsFile)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", repoSweepWouldPruneNoticeEvent)

	res := MergerRepoSweep(repo, 0)
	if res.Err != "" {
		t.Fatalf("sweep: %s", res.Err)
	}

	// (a) the verdict is REPORTED, with the branch, its tip, its verdict and its
	// owner evidence — the four facts an operator needs to act.
	if res.Reported != 1 || len(res.WouldPrune) != 1 {
		t.Fatalf("the live branch must be REPORTED once, got reported=%d entries=%+v", res.Reported, res.WouldPrune)
	}
	entry := res.WouldPrune[0]
	if entry.Branch != rsLiveBranch || entry.Tip != tip {
		t.Errorf("the report must name the branch and its tip: got %+v want branch=%s tip=%s", entry, rsLiveBranch, tip)
	}
	if !strings.Contains(entry.Owners, repoSweepOwnerEvidenceLabel) {
		t.Errorf("the report must carry owner evidence: %q", entry.Owners)
	}
	if entry.LiveRows == nil {
		t.Errorf("a LIVE ledger row names this branch — the report must say so: %q", entry.Owners)
	}

	// (b) the alarm fired, through the notify channel, naming the branch, the tip
	// and the live row.
	data, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatalf("the would-prune alarm was not delivered: %v", err)
	}
	ev := string(data)
	for _, want := range []string{`"event":"` + repoSweepWouldPruneNoticeEvent + `"`, rsLiveBranch, tip, liveRowTaskID} {
		if !strings.Contains(ev, want) {
			t.Errorf("the alarm must carry %q: %s", want, ev)
		}
	}

	// (c) NOTHING moved: the ref, its tip, and master are exactly where they
	// were. This is the epic's own acceptance sentence — the sweep reports, it
	// never acts.
	if got := rsReportGitOut(t, repo, "rev-parse", rsLiveBranch); got != tip {
		t.Errorf("the reported branch moved: tip=%s want %s", got, tip)
	}
	if !rsBranchExists(repo, rsLiveBranch) {
		t.Fatal("the reported branch was DELETED — the sweep must never mutate a ref")
	}
}

// liveRowTaskID is the task id the incident's live owner row carries.
const liveRowTaskID = "live-owner-01a0af77"

// rsLiveBranch is the branch the incident reconstruction works on. Its name is
// the victim's: a recovery branch that had already delivered, whose owner kept
// using it.
const rsLiveBranch = "fix/recovery-branch"

// --- ② the gate: no mutating git verb may reach the sweep path ---------------

// TestRepoSweepReportPathHasNoMutatingGitVerb is the anti-regression gate for
// this epic: the files that make up the per-repo sweep's decision path are read
// as SOURCE and refused if they contain a ref-mutating git verb. This is the
// mechanical half of the rule — a future agent that re-inlines
// `git branch -d` into the sweep's report arm fails the build rather than
// re-creating the incident (Rule 13's exhaustive-test pattern: the literal may
// appear nowhere on this path).
//
// Scope is deliberately the sweep's DECISION path, not the whole package: the
// disposal path (task_merged_cleanup.go's non-report-only branch) legitimately
// still spells these verbs for the fixtures that exercise it, and a whole-tree
// scan would have to baseline them — a weakened gate. The report path is where
// the incident happened and where the ban is absolute.
func TestRepoSweepReportPathHasNoMutatingGitVerb(t *testing.T) {
	// The WHOLE sweep path, not just the report file: a future arm could
	// reintroduce a mutation in a sibling file and this guard would have been
	// green about it (measured 2026-09-19: the gate named only
	// repo_sweep_report.go, so repo_sweep.go was unguarded).
	files := []string{"repo_sweep_report.go", "repo_sweep.go", "repo_sweep_ref_guard.go"}
	verbs := []string{
		`"branch", "-d"`,
		`"branch", "-D"`,
		`"branch", "-m"`,
		`"tag", "-d"`,
		`"worktree", "remove"`,
		`"worktree", "prune"`,
		`"update-ref", "-d"`,
	}
	var checked int
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("the gate must read %s: %v", f, err)
		}
		checked++
		if hit := sweepMutatingVerbHit(string(src), verbs); hit != "" {
			t.Errorf("%s carries the ref-mutating git verb %s — the sweep reports, it never acts", f, hit)
		}
	}
	// Read-floor: a broken file list must not make this gate pass vacuously.
	if checked == 0 {
		t.Fatal("the gate read no file — the sweep's report path is not being checked")
	}
	// RED PROOF, in-place: the very predicate above must fire on the shape it
	// bans, or it is not a gate. (Without this the test could be green because
	// the matcher is blind, which is the failure mode a source gate dies of.)
	if hit := sweepMutatingVerbHit(`if _, derr := repoSweepGit(repo, "branch", "-d", branch); derr != nil {`, verbs); hit == "" {
		t.Fatal("RED PROOF: the matcher must fire on the retired arm's own text")
	}
	if hit := sweepMutatingVerbHit(`repoSweepGit(repo, "merge", "--ff-only", branch)`, verbs); hit != "" {
		t.Errorf("the matcher must NOT fire on a permitted verb (found %s)", hit)
	}
}

// sweepMutatingVerbHit returns the first banned verb found in src, or "".
func sweepMutatingVerbHit(src string, verbs []string) string {
	for _, v := range verbs {
		if strings.Contains(src, v) {
			return v
		}
	}
	return ""
}

// --- fixtures ---------------------------------------------------------------

// seedLiveRowForBranch inserts a LIVE (pending) ledger row whose deliverable
// names branch, so TaskForBranch reports an owner that is NOT terminal — the
// evidence that turns a would-be prune into the incident's shape.
func seedLiveRowForBranch(t *testing.T, branch string) {
	t.Helper()
	// Rule 32's scaffold for the WRITER, and then the one extra line this arm
	// cannot do without. A run resolves its ledger (GOPI_MEMORY_DB) and its
	// notify target (the PROFILE ROOT's settings.json) through two INDEPENDENT
	// channels, and deliverTaskNotify suppresses the events of a ledger that is
	// not its profile's own. So this fixture must move the PROFILE to the same
	// place as the ledger, or the alarm it is about to assert never leaves the
	// process — measured 2026-11-04 (task 01a1a647): the sweep reported
	// "would have pruned … the branch was REPORTED, not deleted" on stderr while
	// events.jsonl was never created, and the test failed on the ABSENCE of its
	// own alarm. The assertion was right; the fixture was the defect.
	db := testutil.NewTestDBPath(t)
	// NewTestDBPath's contract is <home>/.gopi/memory.db (it materialises the
	// profile root so the ledger is a file OF a profile), so the home is the
	// .gopi directory's parent — spelled through config.ProfileDirIn's own leaf
	// rather than a second "..".
	home := filepath.Dir(filepath.Dir(db))
	if want := config.ProfilePathIn(home, config.MemoryDBFileName); want != db {
		t.Fatalf("scaffold contract changed: NewTestDBPath returned %q, but the profile join under %q is %q — "+
			"this arm's whole point is that the ledger IS that profile's own", db, home, want)
	}
	t.Setenv(config.GopiDirEnv, home)
	ctx := context.Background()
	conn, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	deliverable := `{"branch":"` + branch + `"}`
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, 'pending', ?, 'auto', ?, ?)`,
		liveRowTaskID, "the live owner of the branch", deliverable, nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed live row: %v", err)
	}
}

// gitOut runs git in dir and returns its trimmed stdout.
func rsReportGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// writeFileT writes content to path or fails the test.
func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestMergedCleanupProductionCallersAllReportOnly is the mechanical half of the
// report's prose claim that "no production caller passes ReportOnly: false".
// Without it the claim is a sentence someone must remember to re-check: a new
// caller could pass the zero value (false = DESTRUCTIVE) and every gate here
// would stay green while the reaper quietly regained its mutating arm.
//
// The zero value is the dangerous direction, so the check is written against
// the VALUE, not against a `ReportOnly: false` spelling: a production call site
// that builds MergedCleanupOptions must set ReportOnly to a literal true in the
// same composite literal.
func TestMergedCleanupProductionCallersAllReportOnly(t *testing.T) {
	root := toolsRepoRoot(t)
	prodRoots := []string{"cmd", "internal"}
	var sites, checked int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "testdata" || base == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, prodRoots[0]+"/") && !strings.HasPrefix(rel, prodRoots[1]+"/") {
			return nil
		}
		body, berr := os.ReadFile(path)
		if berr != nil {
			return berr
		}
		src := string(body)
		// Every production construction of the options struct must opt in.
		for i := 0; ; {
			j := strings.Index(src[i:], "MergedCleanupOptions{")
			if j < 0 {
				break
			}
			start := i + j
			end := strings.Index(src[start:], "}")
			if end < 0 {
				break
			}
			lit := src[start : start+end]
			i = start + 1
			sites++
			if !strings.Contains(lit, "ReportOnly: true") {
				t.Errorf("%s: a production MergedCleanupOptions literal does not set ReportOnly: true — "+
					"the zero value (false) is the DESTRUCTIVE arm, so the sweep would reap again. "+
					"literal: %s", rel, lit)
				continue
			}
			checked++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree: %v", err)
	}
	// Read-floor: if the walk stopped finding call sites, the gate would be
	// green about a tree it is not reading (the merge-cleanup sites are known to
	// exist in cmd/agent/cli_tasks.go).
	if sites == 0 {
		t.Fatal("the walk found no production MergedCleanupOptions literal — the gate is reading a tree it does not understand")
	}
	if checked != sites {
		t.Errorf("%d of %d production MergedCleanupOptions sites opt into report-only — every one must", checked, sites)
	}
	t.Logf("production MergedCleanupOptions sites: %d, all report-only", sites)
}
