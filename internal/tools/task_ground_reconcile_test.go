// Tests for the READ-SIDE reconciliation of a HISTORICAL owner-less ground
// (task_ground_reconcile.go) and for the git half reading the repository the
// ROW records (epic 01a0e074, gaps 1 and 2).
//
// THE SHAPE UNDER TEST is the one the epic measured on the live ledger: a run
// minted its OWN branch/worktree (`git worktree add <dir> -b <branch>`) while
// its row recorded a DIFFERENT ground, so no row names the minted one and
// every deliverable→row lookup answers "no owner" for a ground that is live.
//
// WHAT IS PINNED HERE:
//
//  1. the lookup FINDS the owning row from the evidence (the git registration,
//     the row's life window, the minting command in the row's own session) and
//     PRINTS that provenance — TestGroundReconcileNamesTheOwnerFromEvidence;
//  2. the answer says it was RECONSTRUCTED, so a reader never mistakes it for a
//     recorded axis — and the same holds for the worktree spelling
//     (TestGroundReconcileAlsoAnswersTheWorktreeSelector);
//  3. a ground whose row cannot be shown to have minted it stays owner-less,
//     with the miss line naming WHAT was looked for — the NEGATIVE CONTROL that
//     keeps this from becoming "always name a row"
//     (TestGroundReconcileDoesNotInventAnOwner, and the life-window control);
//  4. the git half reads the repository the ROW records, not the process's
//     working directory, and says which one it read
//     (TestDeliverableGitHalfReadsTheRepoTheRowRecords), falling back to the
//     declared repos table with that fallback STATED when the row records none
//     (TestDeliverableGitHalfStatesTheFallback).

package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// reconcileGit runs one git command with a deterministic identity and no
// ambient configuration (the same fixture shape gitProvenanceGit uses).
func reconcileGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// reconcileGround builds a real repository whose linked worktree is a ground
// git registers — the shape the reconciliation measures ①.
func reconcileGround(t *testing.T, worktree, branch string) (repo string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed — the reconciliation reads git's own registration by design")
	}
	repo = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	reconcileGit(t, repo, "init", "-q", "-b", "master")
	reconcileGit(t, repo, "config", "user.email", "t@t")
	reconcileGit(t, repo, "config", "user.name", "t")
	reconcileGit(t, repo, "commit", "-q", "--allow-empty", "-m", "seed")
	reconcileGit(t, repo, "worktree", "add", "-q", worktree, "-b", branch)
	// The repository is DECLARED, exactly as a real host declares it (Rule 12's
	// repos table): the branch spelling of the reconciliation asks the declared
	// repositories, never a path it invented.
	settings := filepath.Join(filepath.Dir(repo), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"repos":{"`+repo+`":{"build":""}}}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", settings)
	return repo
}

// reconcileSandbox pins the profile the session reader resolves sessions under
// and returns the sessions directory.
func reconcileSandbox(t *testing.T) string {
	t.Helper()
	sb := testutil.TestDeviceScratchDir(t, "reconcile")
	t.Setenv(config.GopiDirEnv, sb)
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("gopi dir: %v", err)
	}
	dir := filepath.Join(gd, config.SessionsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	return dir
}

// reconcileSession writes a session JSONL in the layout a TASK RUN actually
// uses — sessions/<task_id>/<session_id>.jsonl, whose session_start carries
// both ids — with the ONE recorded command being the minting of the ground
// (④'s evidence). An empty command writes a record WITHOUT the minting (the
// negative control's fixture).
//
// The layout is deliberate: a row's session_id column is the SUBMITTING
// session, so a fixture that filed the run record under the session id would
// test a locator the live ledger mostly cannot use (215 of 271 GoPi rows file
// their run under the task id).
func reconcileSession(t *testing.T, dir, sessionID, taskID, workdir, branch, command string, at time.Time) {
	t.Helper()
	taskDir := filepath.Join(dir, taskID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("mkdir task session dir: %v", err)
	}
	var b strings.Builder
	start := map[string]any{
		"timestamp": at.Add(-time.Minute).Format(time.RFC3339Nano),
		"type":      "session_start", "sessionId": sessionID, "taskId": taskID,
		"workdir": workdir, "model": "test", "pid": os.Getpid(),
	}
	raw, err := json.Marshal(start)
	if err != nil {
		t.Fatalf("marshal session_start: %v", err)
	}
	b.Write(raw)
	b.WriteString("\n")
	if command != "" {
		ev := map[string]any{
			"timestamp": at.Format(time.RFC3339Nano),
			"type":      "assistant",
			"message": map[string]any{"content": []any{map[string]any{
				"type": "tool_use", "id": "toolu_1", "name": "bash",
				"input": map[string]any{"command": command},
			}}},
		}
		raw, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal tool_use: %v", err)
		}
		b.Write(raw)
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(taskDir, sessionID+".jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
}

// reconcileLedger creates a fresh ledger (pinned by the Rule-32 scaffold) and
// returns it with its path.
func reconcileLedger(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	t.Cleanup(func() {
		// The close error is CHECKED, not discarded (Rule 15): a ledger handle
		// that cannot be closed is a real failure, and at cleanup time t.Fatalf
		// is illegal (the test body has returned) — so it is reported through
		// t.Errorf, which records the failure on the running test and lets the
		// remaining cleanups proceed, and it still FAILS the package run rather
		// than reading as a healthy close.
		if err := db.Close(); err != nil {
			t.Errorf("close ledger %s: %v", path, err)
		}
	})
	return db, path
}

// reconcileSeedRow writes one row whose deliverable records the ground the run
// was GIVEN (never the one it minted) and whose life window is [created, ended]
// — ②③'s inputs.
func reconcileSeedRow(t *testing.T, db *sql.DB, taskID, sessionID, repo, branch, worktree string, created, ended time.Time) {
	t.Helper()
	blob, err := json.Marshal(map[string]string{"repo": repo, "branch": branch, "worktree": worktree})
	if err != nil {
		t.Fatalf("marshal deliverable: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO tasks (task_id, summary, status, session_id, deliverable, created_at, updated_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		taskID, "reconcile fixture "+taskID, taskStatusInProgress, sessionID, string(blob),
		float64(created.Unix()), float64(ended.Unix()), float64(ended.Unix())); err != nil {
		t.Fatalf("seed row %s: %v", taskID, err)
	}
}

// TestGroundReconcileNamesTheOwnerFromEvidence is GAP 1's acceptance: the
// reported shape, resolved for real — no row names the minted ground, and the
// lookup still names the row that minted it, with the provenance it used.
func TestGroundReconcileNamesTheOwnerFromEvidence(t *testing.T) {
	base := t.TempDir()
	mintedWT := filepath.Join(base, "gopi-minted")
	mintedBranch := "fix/minted-by-the-run"
	repo := reconcileGround(t, mintedWT, mintedBranch)
	sessions := reconcileSandbox(t)
	db, dbPath := reconcileLedger(t)

	now := time.Now()
	// The row was ALIVE when the ground was created (③): it started two hours
	// before and was still open.
	created, ended := now.Add(-2*time.Hour), now.Add(time.Hour)
	reconcileSeedRow(t, db, "01a0ddc4-c410-7460-9e0e-5ed2b3201bf0", "3592a5ef-300e-485b-a38a-6a2e7c08c1f2",
		repo, "task/01a0ddc4c41074609e0e5ed2b3201bf0",
		filepath.Join(base, "gopi-01a0ddc4c41074609e0e5ed2b3201bf0"), created, ended)
	reconcileSession(t, sessions, "3592a5ef-300e-485b-a38a-6a2e7c08c1f2",
		"01a0ddc4-c410-7460-9e0e-5ed2b3201bf0", mintedWT, mintedBranch,
		"cd "+repo+" && git worktree add "+mintedWT+" -b "+mintedBranch+" && echo ok", now)

	l, err := TaskForBranch(TaskForBranchQuery{Branch: mintedBranch}, dbPath)
	if err != nil {
		t.Fatalf("for-branch: %v", err)
	}
	if len(l.Owners) != 1 {
		t.Fatalf("the reconciliation must name exactly one owner, got %d (%s)", len(l.Owners), l.ReconcileNote)
	}
	if !l.Reconciled {
		t.Error("the answer must be marked RECONSTRUCTED — a reader must be able to tell it from a recorded axis")
	}
	owner := l.Owners[0]
	if owner.TaskID != "01a0ddc4-c410-7460-9e0e-5ed2b3201bf0" {
		t.Errorf("owner = %s, want the row that minted the ground", owner.TaskID)
	}
	for _, want := range []string{
		taskGroundReconcileProvenancePrefix,    // the channel marker
		"3592a5ef-300e-485b-a38a-6a2e7c08c1f2", // the session whose command is the evidence
		mintedBranch, mintedWT, repo,           // the ground and the repository
	} {
		if !strings.Contains(owner.Provenance, want) {
			t.Errorf("the provenance must carry %q:\n%s", want, owner.Provenance)
		}
	}
	if !strings.Contains(l.ReconcileNote, repo) || !strings.Contains(l.ReconcileNote, mintedBranch) {
		t.Errorf("the reading must be reported (what was looked for, and where):\n%s", l.ReconcileNote)
	}
	// The operator's own face: the verdict line says RECONSTRUCTED and prints
	// the owner — the same text the retired `for-branch` lookup returned.
	report := TaskForBranchReport(l)
	if !strings.Contains(report, "RECONSTRUCTED") || !strings.Contains(report, owner.TaskID) {
		t.Errorf("the report must state the reconstruction and name the row:\n%s", report)
	}
	if code := TaskForBranchExitCode(l); code != TaskForBranchExitOwned {
		t.Errorf("exit code = %d, want %d (the ground HAS an owner)", code, TaskForBranchExitOwned)
	}
}

// TestGroundReconcileAlsoAnswersTheWorktreeSelector: the same reconstruction
// through the worktree spelling (`pwd` is what the operator standing there
// holds).
func TestGroundReconcileAlsoAnswersTheWorktreeSelector(t *testing.T) {
	base := t.TempDir()
	mintedWT := filepath.Join(base, "gopi-minted-wt")
	mintedBranch := "fix/minted-worktree-spelling"
	repo := reconcileGround(t, mintedWT, mintedBranch)
	sessions := reconcileSandbox(t)
	db, dbPath := reconcileLedger(t)

	now := time.Now()
	reconcileSeedRow(t, db, "01a0de63-177c-7505-a246-7a286e23db84", "8b601c0a-277c-44a6-8803-f55dee4603c2",
		repo, "task/given", filepath.Join(base, "gopi-given"), now.Add(-time.Hour), now.Add(time.Hour))
	reconcileSession(t, sessions, "8b601c0a-277c-44a6-8803-f55dee4603c2",
		"01a0de63-177c-7505-a246-7a286e23db84", mintedWT, mintedBranch,
		"git worktree add "+mintedWT+" -b "+mintedBranch, now)

	l, err := TaskForWorktree(TaskForWorktreeQuery{Worktree: mintedWT}, dbPath)
	if err != nil {
		t.Fatalf("for-worktree: %v", err)
	}
	if len(l.Owners) != 1 || !l.Reconciled {
		t.Fatalf("the worktree spelling must reconcile to one owner (reconciled=%v, owners=%d, note=%s)",
			l.Reconciled, len(l.Owners), l.ReconcileNote)
	}
	if l.Owners[0].TaskID != "01a0de63-177c-7505-a246-7a286e23db84" {
		t.Errorf("owner = %s, want the row that minted the worktree", l.Owners[0].TaskID)
	}
}

// TestGroundReconcileDoesNotInventAnOwner is the NEGATIVE CONTROL: the SAME
// live ground, but the row's session records no `git worktree add` — so no row
// may be named, and the miss line must say what was looked for and where.
func TestGroundReconcileDoesNotInventAnOwner(t *testing.T) {
	base := t.TempDir()
	mintedWT := filepath.Join(base, "gopi-unclaimed")
	mintedBranch := "fix/unclaimed-ground"
	repo := reconcileGround(t, mintedWT, mintedBranch)
	sessions := reconcileSandbox(t)
	db, dbPath := reconcileLedger(t)

	now := time.Now()
	// Alive across the registration, working in the SAME repository — the only
	// missing piece is ④ (the minting command).
	reconcileSeedRow(t, db, "01a0dec6-3a8a-7a5d-986f-2d6c1a5d8dee", "47a6abf3-2e9c-42dc-99b4-19ae05f37c19",
		repo, "task/other", filepath.Join(base, "gopi-other"), now.Add(-time.Hour), now.Add(time.Hour))
	reconcileSession(t, sessions, "47a6abf3-2e9c-42dc-99b4-19ae05f37c19",
		"01a0dec6-3a8a-7a5d-986f-2d6c1a5d8dee", filepath.Join(base, "gopi-other"), "task/other",
		"cd "+repo+" && git status --porcelain", now)

	l, err := TaskForBranch(TaskForBranchQuery{Branch: mintedBranch}, dbPath)
	if err != nil {
		t.Fatalf("for-branch: %v", err)
	}
	if len(l.Owners) != 0 {
		t.Fatalf("no row recorded minting this ground, so none may be named (got %d)", len(l.Owners))
	}
	if l.Reconciled {
		t.Error("a lookup that named no row must not be marked as reconciled")
	}
	for _, want := range []string{repo, mintedBranch, "alive when the registration was created"} {
		if !strings.Contains(l.ReconcileNote, want) {
			t.Errorf("the miss line must name what was looked for (%q):\n%s", want, l.ReconcileNote)
		}
	}
	report := TaskForBranchReport(l)
	if !strings.Contains(report, taskForBranchNoOwnerMarker) || !strings.Contains(report, l.ReconcileNote) {
		t.Errorf("the report must keep the no-owner verdict AND print the reading:\n%s", report)
	}
}

// TestGroundReconcileHonoursTheLifeWindow: ③ is load-bearing — a row that had
// ALREADY ENDED when the ground was created cannot own it, even when its
// session records the very command.
func TestGroundReconcileHonoursTheLifeWindow(t *testing.T) {
	base := t.TempDir()
	mintedWT := filepath.Join(base, "gopi-late")
	mintedBranch := "fix/minted-after-the-row-ended"
	repo := reconcileGround(t, mintedWT, mintedBranch)
	sessions := reconcileSandbox(t)
	db, dbPath := reconcileLedger(t)

	now := time.Now()
	// The row ENDED an hour ago; the registration was created just now.
	reconcileSeedRow(t, db, "01a0def2-45cb-71ed-9d47-4f9eca55a350", "ee40a820-6512-4c95-b73a-15b95e4e5b72",
		repo, "task/ended", filepath.Join(base, "gopi-ended"), now.Add(-3*time.Hour), now.Add(-time.Hour))
	reconcileSession(t, sessions, "ee40a820-6512-4c95-b73a-15b95e4e5b72",
		"01a0def2-45cb-71ed-9d47-4f9eca55a350", mintedWT, mintedBranch,
		"git worktree add "+mintedWT+" -b "+mintedBranch, now.Add(-2*time.Hour))

	l, err := TaskForBranch(TaskForBranchQuery{Branch: mintedBranch}, dbPath)
	if err != nil {
		t.Fatalf("for-branch: %v", err)
	}
	if len(l.Owners) != 0 {
		t.Fatalf("a row that ended before the ground existed must not own it (got %s)", l.Owners[0].TaskID)
	}
}

// TestDeliverableGitHalfReadsTheRepoTheRowRecords is GAP 2's acceptance in the
// shape the operator reported: the POSITIONAL row id names a row whose
// deliverable records the repository the branch lives in, while the process's
// own directory (and the declared table) point elsewhere. The reading must
// answer about the ROW's repository and say so.
func TestDeliverableGitHalfReadsTheRepoTheRowRecords(t *testing.T) {
	base := t.TempDir()
	rowRepo := filepath.Join(base, "repo-row")
	otherRepo := filepath.Join(base, "repo-elsewhere")
	branch := "feat/row-recorded-repo"
	rowWT := filepath.Join(base, "wt-row")
	for _, r := range []string{rowRepo, otherRepo} {
		if err := os.MkdirAll(r, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", r, err)
		}
		reconcileGit(t, r, "init", "-q", "-b", "master")
		reconcileGit(t, r, "config", "user.email", "t@t")
		reconcileGit(t, r, "config", "user.name", "t")
		reconcileGit(t, r, "commit", "-q", "--allow-empty", "-m", "seed")
	}
	// Only the ROW's repository has the branch; the other one is the decoy the
	// declared table points at (the process's own directory in the real defect).
	reconcileGit(t, rowRepo, "worktree", "add", "-q", rowWT, "-b", branch)

	settings := filepath.Join(base, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"repos":{"`+otherRepo+`":{"build":""}}}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", settings)

	db, dbPath := reconcileLedger(t)
	now := time.Now()
	reconcileSeedRow(t, db, "t-row-repo", "sess-row-repo", rowRepo, "task/the-rows-own-ground", rowWT, now.Add(-time.Hour), now.Add(time.Hour))

	// The operator's invocation: the row id AND a branch no row records (the
	// minted one) — the row is what pins the repository.
	res, err := ResolveTaskIDByDeliverable(DeliverableSelector{Branch: branch, Row: "t-row-repo"}, dbPath)
	if err == nil {
		t.Fatalf("no row records %q, so the resolution must refuse (got task %s)", branch, res.TaskID)
	}
	if len(res.Repos) == 0 || res.Repos[0] != rowRepo {
		t.Errorf("the git half's FIRST candidate must be the row's recorded repo %q, got %v", rowRepo, res.Repos)
	}
	if note := strings.Join(res.RepoNotes, "\n"); !strings.Contains(note, rowRepo) || !strings.Contains(note, "repository the row records") {
		t.Errorf("the answer must state WHICH repository it read and why:\n%s", note)
	}

	p := TaskGitProvenanceFor(context.Background(), DeliverableSelector{Branch: branch, Row: "t-row-repo"}, res)
	if p.Repo != rowRepo {
		t.Errorf("the reading answered about %q, want the row's recorded repository %q", p.Repo, rowRepo)
	}
	if !p.RefExists {
		t.Errorf("the branch exists in the row's repository — the reading must find it: %+v", p)
	}
	if p.Checkout != rowWT {
		t.Errorf("checkout = %q, want the row's worktree %q", p.Checkout, rowWT)
	}
	text := p.Text(DeliverableSelector{Branch: branch, Row: "t-row-repo"}, res)
	if !strings.Contains(text, DeliverableRepoFromRowNoteFmt[:28]) {
		t.Errorf("the block must say the repository came from the row:\n%s", text)
	}
	// The CONTROL: the same selector asked with a row that records NOTHING must
	// fall back to the declared table — and the deciding evidence is that the
	// first candidate is no longer the row's repo (which the walk still
	// reaches, but not first).
	if _, err := db.Exec(`INSERT INTO tasks (task_id, summary, status, session_id, deliverable, created_at, updated_at, ended_at)
		VALUES ('t-no-repo','no repo','in_progress','sess-no-repo',?,?,?,?)`,
		`{"branch":"`+branch+`"}`, float64(now.Unix()), float64(now.Unix()), float64(now.Unix())); err != nil {
		t.Fatalf("seed repo-less row: %v", err)
	}
	res2, _ := ResolveTaskIDByDeliverable(DeliverableSelector{Branch: branch, Row: "t-no-repo"}, dbPath) //stylegate:ignore resolves the second row by deliverable in the fixture; the comparison below is the contract
	if len(res2.Repos) == 0 || res2.Repos[0] == rowRepo {
		t.Errorf("a row recording no repository must not lead the list with a foreign repo: %v", res2.Repos)
	}
	if note := strings.Join(res2.RepoNotes, "\n"); !strings.Contains(note, DeliverableRepoUnrecordedNote) {
		t.Errorf("the fallback must be SAID, not left implicit:\n%s", note)
	}
}

// TestDeliverableGitHalfStatesTheFallback: a row recording NO repository is the
// one case where the declared table and the working directory are read — and
// the answer must say the fallback was used.
func TestDeliverableGitHalfStatesTheFallback(t *testing.T) {
	base := t.TempDir()
	declared := filepath.Join(base, "repo-declared")
	branch := "feat/no-repo-recorded"
	if err := os.MkdirAll(declared, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	reconcileGit(t, declared, "init", "-q", "-b", "master")
	reconcileGit(t, declared, "config", "user.email", "t@t")
	reconcileGit(t, declared, "config", "user.name", "t")
	reconcileGit(t, declared, "commit", "-q", "--allow-empty", "-m", "seed")
	reconcileGit(t, declared, "worktree", "add", "-q", filepath.Join(base, "wt-declared"), "-b", branch)

	settings := filepath.Join(base, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"repos":{"`+declared+`":{"build":""}}}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", settings)

	db, dbPath := reconcileLedger(t)
	now := time.Now()
	// The row records a branch and a worktree, but NO repo.
	if _, err := db.Exec(
		`INSERT INTO tasks (task_id, summary, status, session_id, deliverable, created_at, updated_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"t-no-repo", "no repo recorded", taskStatusInProgress, "sess-no-repo",
		`{"branch":"`+branch+`","worktree":"/somewhere/wt"}`,
		float64(now.Add(-time.Hour).Unix()), float64(now.Unix()), float64(now.Unix())); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	res, err := ResolveTaskIDByDeliverable(DeliverableSelector{Branch: branch}, dbPath)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	note := strings.Join(res.RepoNotes, "\n")
	if !strings.Contains(note, DeliverableRepoUnrecordedNote) {
		t.Errorf("a row recording no repository must say the fallback was used:\n%s", note)
	}
	p := TaskGitProvenanceFor(context.Background(), DeliverableSelector{Branch: branch}, res)
	if p.Repo != declared || !p.RefExists {
		t.Errorf("the fallback must read the declared repository %q: %+v", declared, p)
	}
}

// --- the measured window: a LIVE ground git registers, and a ledger that ---
// --- names nothing at all (epic 01a0ed38) ------------------------------

// TestGroundReconcileScopesTheNoOwnerClaimAndLabelsTheGitChannel is the
// REGRESSION TEST for the operator-reported defect, and it reproduces the
// measured window exactly.
//
// THE MEASURED SHAPE. A task run mints its own worktree (`git worktree add … -b
// task/<id>`), so GIT registers a live ground. Its ledger row is still
// REPO-ONLY — `{"repo":…}` with no branch and no worktree, the in-flight shape
// the claim-time stamp has not reached yet, and for a row whose claim stamp is
// written onto a DIFFERENT profile's ledger, the row is not in the ledger being
// read at all. The (then-live) branch→row lookup on that branch then answered:
//
//	no owner: no ledger row names branch task/01a0ed38…
//	  (0 rows carry a branch; none names this one)
//	  reconciled against the git side: repository … registers the branch …
//
// — a flat absolute denial, then in the SAME block a sentence proving git holds
// the ground. The two halves contradict each other for the reader, which is what
// this test forbids.
//
// WHAT MUST HOLD NOW, and each assertion is one half of the fix:
//
//	① the ledger the claim is about is NAMED on the answer, so "no ledger row
//	   names it" is scoped rather than absolute;
//	② an EMPTY ledger says it is empty — never "0 rows carry a branch;
//	   none names this one", which reads as a populated ledger that looked
//	   and found nothing;
//	③ the git registration is a SECOND, LABELLED channel, printed under its
//	   own label naming the instrument and the finding — not a footnote
//	   hanging under the denial.
func TestGroundReconcileScopesTheNoOwnerClaimAndLabelsTheGitChannel(t *testing.T) {
	base := t.TempDir()
	mintedWT := filepath.Join(base, "gopi-01a0ed38fecc70e99c2b14bdef7887af")
	mintedBranch := "task/01a0ed38fecc70e99c2b14bdef7887af"
	// ① git registers a LIVE ground — the runner created it, nothing removed it.
	repo := reconcileGround(t, mintedWT, mintedBranch)
	reconcileSandbox(t)
	db, dbPath := reconcileLedger(t)

	// THE LEDGER HAS ROWS, AND EVERY ONE OF THEM IS REPO-ONLY — the measured
	// in-flight shape (a column of `{"repo":"/home/user/work/GoPi"}` rows carrying
	// no branch at all), so no row of it can name any selector.
	now := time.Now()
	for _, id := range []string{"01a0ed25-cc83-7766-82f3-d729367e24d6", "01a0ed34-b8a7-79c6-a7ca-aad436ec7e3e"} {
		reconcileSeedRepoOnlyRow(t, db, id, "sess-"+id, repo, now.Add(-time.Hour), now.Add(time.Hour))
	}

	l, err := TaskForBranch(TaskForBranchQuery{Branch: mintedBranch}, dbPath)
	if err != nil {
		t.Fatalf("for-branch: %v", err)
	}
	if len(l.Owners) != 0 {
		t.Fatalf("no row records this ground, so none may be named (got %d owners)", len(l.Owners))
	}
	// ① the ledger the claim is about travels with the answer.
	if l.LedgerPath != dbPath {
		t.Errorf("the lookup must carry the ledger it read (%q), got %q — an unscoped denial reads as a claim about the machine",
			dbPath, l.LedgerPath)
	}
	// ② an empty ledger is not this ledger, but its size must be MEASURED:
	// two rows here, and the answer must say so rather than at "0 rows".
	if l.LedgerRowsTotal != 2 {
		t.Errorf("LedgerRowsTotal = %d, want 2 (the ledger's own size, so an empty one is distinguishable)", l.LedgerRowsTotal)
	}
	// ③ the git channel is a FIELD, so a reader (and a script) can take it.
	if l.Ground == nil {
		t.Fatalf("git registers %q at %s but the answer carries no labelled git channel:\n%s", mintedBranch, mintedWT, l.ReconcileNote)
	}
	if l.Ground.Branch != mintedBranch || !sameWorkingPath(l.Ground.Worktree, mintedWT) || !sameWorkingPath(l.Ground.Repo, repo) {
		t.Errorf("the git channel must name the measured ground: %+v", *l.Ground)
	}

	report := TaskForBranchReport(l)
	// ② THE EMPTY-LEDGER LINE IS NOT PRINTED FOR A POPULATED LEDGER, and the
	// size line says how many rows DO carry a branch.
	if !strings.Contains(report, taskForBranchNoOwnerMarker) {
		t.Errorf("the answer must keep the named no-owner verdict:\n%s", report)
	}
	if !strings.Contains(report, dbPath) {
		t.Errorf("the answer must name the ledger the claim is about (%q):\n%s", dbPath, report)
	}
	if strings.Contains(report, taskForBranchLedgerEmptyFmt) || strings.Contains(report, "holds no row at all") {
		t.Errorf("a ledger holding rows must NOT be reported as empty:\n%s", report)
	}
	if !strings.Contains(report, fmt.Sprintf(taskForBranchNoBranchRowsFmt, l.LedgerRowsTotal)) {
		t.Errorf("this ledger holds rows and not one carries a branch — a distinct finding that must be stated:\n%s", report)
	}
	// ③ THE LABELLED SECOND CHANNEL: its own label, naming the instrument and
	// the finding — and it must appear as a channel, not as a trailing note.
	label := fmt.Sprintf(TaskGroundChannelLabel, deliverableSelectorBranchKind)
	if !strings.Contains(report, label) {
		t.Errorf("the git registration must render under its own label (%q):\n%s", label, report)
	}
	// The verdict line is the SCOPE line's subject, not the whole answer: the
	// unqualified "none names this one" wording must not survive beside a
	// labelled channel that proves git holds the ground. The old sentence is
	// gone by construction — assert it by name so a revert is caught.
	if strings.Contains(report, taskForBranchRowsCarryFmt) || strings.Contains(report, "rows carry a branch; none names this one") {
		t.Errorf("the unqualified count line must not be printed when no row carries a branch at all:\n%s", report)
	}
	// The reading itself is still reported (Rule 15b) — the miss sentence, with
	// what was looked for and where.
	if !strings.Contains(report, l.ReconcileNote) {
		t.Errorf("the reconciliation's own reading must stay in the answer:\n%s", report)
	}
	if code := TaskForBranchExitCode(l); code != TaskForBranchExitNoOwner {
		t.Errorf("exit = %d, want %d (the ledger was read and no row of it owns the branch)", code, TaskForBranchExitNoOwner)
	}
}

// TestGroundReconcileEmptyLedgerSaysItIsEmpty is ② in its own right: the branch
// the measured output named, asked of an EMPTY ledger, must say the ledger is
// empty rather than print the count line that reads as "I looked and found
// nothing" — the two have different remedies (dispatch into THIS profile, or
// read the profile that dispatched it).
func TestGroundReconcileEmptyLedgerSaysItIsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty-ledger.db")
	// A ledger file that EXISTS and is empty — the fresh-profile shape, which
	// is distinct from an absent one (both hold no rows; neither may be
	// reported as unreadable).
	reconcileSandbox(t)
	if _, err := TaskForBranch(TaskForBranchQuery{Branch: "x"}, path); err != nil {
		t.Fatalf("absent ledger: %v", err)
	}
	l, err := TaskForBranch(TaskForBranchQuery{Branch: "task/git-registered-but-nobody-owns-it"}, path)
	if err != nil {
		t.Fatalf("for-branch on an empty ledger: %v", err)
	}
	if !l.Readable || l.LedgerRowsTotal != 0 || l.RowsWithBranch != 0 {
		t.Fatalf("an absent ledger is a READABLE ledger holding nothing: readable=%v total=%d branches=%d",
			l.Readable, l.LedgerRowsTotal, l.RowsWithBranch)
	}
	if l.LedgerPath != path {
		t.Errorf("LedgerPath = %q, want %q", l.LedgerPath, path)
	}
	report := TaskForBranchReport(l)
	if !strings.Contains(report, taskForBranchLedgerEmptyFmt) {
		t.Errorf("an empty ledger must SAY it is empty:\n%s", report)
	}
	if strings.Contains(report, "rows carry a branch") {
		t.Errorf("an empty ledger must not be described by a branch-row count:\n%s", report)
	}
}

// reconcileSeedRepoOnlyRow writes one row whose deliverable records the REPO
// ALONE — the in-flight shape measured on the live ledger (the claim-time stamp
// reaches a row only when the run goes through the dispatch path, so an
// ordinary row carries `{"repo":…}` and nothing else until completion).
func reconcileSeedRepoOnlyRow(t *testing.T, db *sql.DB, taskID, sessionID, repo string, created, ended time.Time) {
	t.Helper()
	blob, err := json.Marshal(map[string]string{"repo": repo})
	if err != nil {
		t.Fatalf("marshal deliverable: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO tasks (task_id, summary, status, session_id, deliverable, created_at, updated_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		taskID, "repo-only fixture "+taskID, taskStatusInProgress, sessionID, string(blob),
		float64(created.Unix()), float64(ended.Unix()), float64(ended.Unix())); err != nil {
		t.Fatalf("seed repo-only row %s: %v", taskID, err)
	}
}
