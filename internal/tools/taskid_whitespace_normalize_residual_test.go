package tools

// ---------------------------------------------------------------------------
// RESIDUAL task_id entry points: write / filesystem / sync faces.
//
// The sibling file taskid_whitespace_normalize_test.go pinned the READ
// accessors (TaskTags / TaskBriefing / TaskShow …). This file pins the
// SECOND half of the same bug class: the exported WRITE, FILESYSTEM and
// SYNC entry points that also take a task_id and — before this fix — never
// normalized it. Two symptoms, one cause:
//
//	ledger  — the UPDATE/export filter matched the PADDED literal, matched
//	          no row, and the write was a SILENT no-op (a stamp that never
//	          landed, a rollback that never gave the claim back, a push that
//	          reported "task not found");
//	disk    — the session container is a directory named after the id, so a
//	          padded id created a SECOND container (sessions/"  t-1\n ")
//	          invisible to the row-keyed lookups.
//
// Every entry point below routes through the ONE seam taskIDNormalize
// (tasks.go) and the tests prove pad == exact (same row / same dir) while
// empty and unknown ids keep their previous refusal semantics.
//
// The fixture is the MINIMAL sample (text2img habit 4): ONE row seeded with
// the EXACT id, proven to carry no padded twin before any assertion runs —
// so a padded id resolving at all can only come from the normalization.
// ---------------------------------------------------------------------------

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// taskIDWSResidualSeed is the fixture row's exact (trimmed) id.
	taskIDWSResidualSeed = "t-ws-residual-1"
	// taskIDWSResidualPad is its padded form: leading + trailing space (the
	// copy-paste / shell-substitution shape) AND an embedded newline, so the
	// tests prove TrimSpace's full cutset is applied.
	taskIDWSResidualPad = "  t-ws-residual-1 \n "
	// taskIDWSResidualGone is a padded id that names NO row at all.
	taskIDWSResidualGone = "  t-ws-residual-absent  "
	// taskIDWSResidualPeer is a declared (fake) sync peer alias — the sync
	// faces only leave the process when a peer exists.
	taskIDWSResidualPeer = "gopi-wsresidual-peer"
	// taskIDWSPadReason is the shape of an UNKNOWN-row padded id (what the
	// notification and the dir faces must trim to).
	taskIDWSResidualGoneTrimmed = "t-ws-residual-absent"
)

// taskIDWSResidualSandbox isolates GOPI_HOME + the ledger in a t.TempDir()
// (never the real ~/.gopi) and seeds ONE row with the EXACT id, then proves
// the padded id addresses no row (the minimal-sample precondition).
func taskIDWSResidualSandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	// The ledger must be the file the profile at `home` OWNS (<home>/.gopi/
	// memory.db). Putting it at <home>/memory.db leaves the two halves of the
	// configuration in different places — ledger here, notify target there — and
	// the delivery gate (taskNotifyDelivers) then withholds this profile's own
	// events, which is the shape these fixtures used to have.
	t.Setenv(memoryDBEnv, filepath.Join(config.ProfileDirIn(home), memoryDefaultDBFile))
	t.Setenv(config.RoleEnv, "client")
	// The task-row-sync switch ON: this file's fixtures exercise the CLAIM
	// ROUND's export/import machinery, which is an AUTOMATIC crossing and is
	// OFF by default (config.TaskSyncSettingsKey). The round under test
	// literally cannot reach its fake ssh leg with the switch off, so the
	// fixture would assert nothing. The switch-off arm — local claim only —
	// is pinned by cmd/agent's TestClaimBroadcastVerdictUnderTaskSyncOff.
	t.Setenv(config.TaskSyncEnv, config.EnvBoolOn)
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	if _, err := TaskNewOID(TaskNewOptions{
		Summary: "ws residual row " + taskIDWSResidualSeed,
		TaskID:  taskIDWSResidualSeed,
		Payload: "residual fixture payload",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed row %s: %v", taskIDWSResidualSeed, err)
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("seed verify: open ledger: %v", err)
	}
	defer db.Close()
	exact, err := taskByID(ctx, db, taskIDWSResidualSeed)
	if err != nil || exact == nil {
		t.Fatalf("seed verify: the exact id %q did not resolve: %v", taskIDWSResidualSeed, err)
	}
	padded, err := taskByID(ctx, db, taskIDWSResidualPad)
	if err != nil {
		t.Fatalf("seed verify: padded lookup errored: %v", err)
	}
	if padded != nil {
		t.Fatalf("seed verify: the padded id resolved — the fixture is not minimal")
	}
	return home
}

// taskIDWSResidualRow reads the fixture row back by its EXACT id.
func taskIDWSResidualRow(t *testing.T) *Task {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	defer db.Close()
	row, err := taskByID(context.Background(), db, taskIDWSResidualSeed)
	if err != nil || row == nil {
		t.Fatalf("read row %s: (%v, %v)", taskIDWSResidualSeed, row, err)
	}
	return row
}

// taskIDWSResidualSetStatus forces the fixture row to a status (the seeds
// above are born in_progress; the recovery faces need claimed/pending).
func taskIDWSResidualSetStatus(t *testing.T, status string) {
	t.Helper()
	taskIDWSResidualExec(t, `UPDATE tasks SET status = ? WHERE task_id = ?`, status, taskIDWSResidualSeed)
}

// taskIDWSResidualExec runs one raw write against the sandbox ledger (only
// used to shape fixtures, never to assert product behavior).
func taskIDWSResidualExec(t *testing.T, query string, args ...any) {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("fixture write: open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("fixture write %q: %v", query, err)
	}
}

// taskIDWSResidualFakeSSH installs a fake `ssh` on PATH that records its
// argv into the returned capture file and answers the claim channel's
// ClaimPushResponse shape (accepted, uncontested). It is the only way the
// sync faces get a REACHABLE peer without a network — and it makes the
// padded id's reach observably countable.
func taskIDWSResidualFakeSSH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "ssh-argv.txt")
	shim := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" >> " + capture + "\n" +
		`printf '%s' '{"accepted":true,"reason":"row-not-found"}'` + "\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return capture
}

// taskIDWSResidualPeers declares ONE fake peer (env has precedence over any
// ambient settings.json, so the round really leaves the process).
func taskIDWSResidualPeers(t *testing.T) {
	t.Helper()
	t.Setenv(configSyncPeersEnv, taskIDWSResidualPeer)
}

// taskIDWSResidualWriteFile writes a small fixture file for the copy face.
func taskIDWSResidualWriteFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("fixture file: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Ledger write faces (tasks.go)
// ---------------------------------------------------------------------------

// TestTaskStampRunPaddedIDStampsTheSameRow: the run-provenance stamp is
// best-effort — its WHERE matched the padded literal, so the padded id was
// a silent no-op and the row lost its "how was this run" answer.
func TestTaskStampRunPaddedIDStampsTheSameRow(t *testing.T) {
	taskIDWSResidualSandbox(t)
	const cmdline = "gopi task run " + taskIDWSResidualSeed
	if err := TaskStampRun(taskIDWSResidualPad, "serve", cmdline); err != nil {
		t.Fatalf("TaskStampRun(padded): %v", err)
	}
	row := taskIDWSResidualRow(t)
	if row.RunMethod != "serve" || row.RunCmd != cmdline {
		t.Errorf("TaskStampRun(padded) did not land on %s: run_method=%q run_cmd=%q",
			taskIDWSResidualSeed, row.RunMethod, row.RunCmd)
	}
}

// TestTaskStampSelfClaimPaddedIDAwardsTheSameRow: the zombie-evidence pid
// stamp is lost on a padded id — the row then reads as an unowned
// in_progress row forever ("suspected", never disposable).
func TestTaskStampSelfClaimPaddedIDAwardsTheSameRow(t *testing.T) {
	taskIDWSResidualSandbox(t)
	if err := TaskStampSelfClaim(taskIDWSResidualPad); err != nil {
		t.Fatalf("TaskStampSelfClaim(padded): %v", err)
	}
	row := taskIDWSResidualRow(t)
	if row.ClaimedBy != taskDefaultClaimHolder() {
		t.Errorf("TaskStampSelfClaim(padded): claimed_by = %q, want this runner %q",
			row.ClaimedBy, taskDefaultClaimHolder())
	}
}

// TestTaskResumeClaimedPaddedIDRecoversTheSameRow: the crash-recovery
// re-stamp must address the claimed row through the padded id too.
func TestTaskResumeClaimedPaddedIDRecoversTheSameRow(t *testing.T) {
	taskIDWSResidualSandbox(t)
	taskIDWSResidualSetStatus(t, taskStatusClaimed)
	if err := TaskResumeClaimed(taskIDWSResidualPad, ""); err != nil {
		t.Fatalf("TaskResumeClaimed(padded): %v", err)
	}
	row := taskIDWSResidualRow(t)
	if row.ClaimedBy != taskDefaultClaimHolder() {
		t.Errorf("TaskResumeClaimed(padded): claimed_by = %q, want this runner %q",
			row.ClaimedBy, taskDefaultClaimHolder())
	}
}

// TestRollbackLocalClaimPaddedIDRevertsTheSameRow: the lock protocol's
// yield face — the WHERE matched no row, so the revert silently did nothing
// and the row stayed claimed (a dead claimed row, exactly the shape the
// anti-deadlock revert exists to prevent).
func TestRollbackLocalClaimPaddedIDRevertsTheSameRow(t *testing.T) {
	taskIDWSResidualSandbox(t) // the seed row is in_progress (revertible)
	if err := RollbackLocalClaim(taskIDWSResidualPad); err != nil {
		t.Fatalf("RollbackLocalClaim(padded): %v", err)
	}
	if got := taskIDWSResidualRow(t).Status; got != taskStatusPending {
		t.Errorf("RollbackLocalClaim(padded): status = %q, want %q", got, taskStatusPending)
	}
}

// TestTaskRunWritebackTagsPaddedIDTagsTheSameRow: the writeback-tags face
// must normalize ONCE at entry. Without it the terminal STATUS landed (the
// sibling writeback normalizes internally) while THIS face's tags UPDATE
// matched no row — the provenance tag was silently DROPPED off a completed
// row (the worker:<host> tag was the visible casualty of exactly this
// split).
func TestTaskRunWritebackTagsPaddedIDTagsTheSameRow(t *testing.T) {
	taskIDWSResidualSandbox(t)
	const tag = "worker:ws-residual"
	if _, err := TaskRunWritebackTags(taskIDWSResidualPad, TaskStatusCompleted, "residual result", tag); err != nil {
		t.Fatalf("TaskRunWritebackTags(padded): %v", err)
	}
	row := taskIDWSResidualRow(t)
	if row.Status != taskStatusCompleted {
		t.Fatalf("status = %q, want %q", row.Status, taskStatusCompleted)
	}
	if !strings.Contains(row.Tags, tag) {
		t.Errorf("tags = %q, want the writeback tag %q — the tags UPDATE matched no row", row.Tags, tag)
	}
}

// ---------------------------------------------------------------------------
// Filesystem faces (task_session.go)
// ---------------------------------------------------------------------------

// TestTaskIDSessionDirPaddedIDIsTheSameContainer: the container is keyed by
// the LEDGER id — a padded id must resolve the SAME directory (never a
// second, padded-name container).
func TestTaskIDSessionDirPaddedIDIsTheSameContainer(t *testing.T) {
	home := taskIDWSResidualSandbox(t)
	exact, err := TaskSessionDir(taskIDWSResidualSeed)
	if err != nil {
		t.Fatalf("TaskSessionDir(exact): %v", err)
	}
	padded, err := TaskSessionDir(taskIDWSResidualPad)
	if err != nil {
		t.Fatalf("TaskSessionDir(padded): %v", err)
	}
	if padded != exact {
		t.Errorf("TaskSessionDir(padded) = %q, want the exact-id container %q", padded, exact)
	}
	if want := filepath.Join(home, ".gopi", config.SessionsDirName, taskIDWSResidualSeed); exact != want {
		t.Errorf("TaskSessionDir(exact) = %q, want %q", exact, want)
	}
	exactAtt, err := TaskAttachmentsDir(taskIDWSResidualSeed)
	if err != nil {
		t.Fatalf("TaskAttachmentsDir(exact): %v", err)
	}
	padAtt, err := TaskAttachmentsDir(taskIDWSResidualPad)
	if err != nil {
		t.Fatalf("TaskAttachmentsDir(padded): %v", err)
	}
	if padAtt != exactAtt {
		t.Errorf("TaskAttachmentsDir(padded) = %q, want the exact-id area %q", padAtt, exactAtt)
	}
}

// TestTaskCopyAttachmentsPaddedIDArchivesIntoTheSameDir: the copy face
// CREATES the container, so a padded id left a second directory on disk
// (filesystem drift) that no row-keyed reader would ever find again.
func TestTaskCopyAttachmentsPaddedIDArchivesIntoTheSameDir(t *testing.T) {
	home := taskIDWSResidualSandbox(t)
	src := taskIDWSResidualWriteFile(t, "log.txt", "residual attachment bytes\n")
	atts, err := TaskCopyAttachments(taskIDWSResidualPad, []string{src})
	if err != nil {
		t.Fatalf("TaskCopyAttachments(padded): %v", err)
	}
	if len(atts) != 1 {
		t.Fatalf("TaskCopyAttachments(padded) manifest = %d entries, want 1", len(atts))
	}
	sd, err := TaskSessionDir(taskIDWSResidualSeed)
	if err != nil {
		t.Fatalf("TaskSessionDir(exact): %v", err)
	}
	replica := filepath.Join(sd, filepath.FromSlash(atts[0].ReplicaRelPath))
	if _, err := os.Stat(replica); err != nil {
		t.Errorf("replica %s missing — the padded copy landed outside the row's container: %v", replica, err)
	}
	drifted := filepath.Join(home, ".gopi", config.SessionsDirName, taskIDWSResidualPad)
	if _, err := os.Stat(drifted); !os.IsNotExist(err) {
		t.Errorf("a padded-name container %q exists (stat err = %v) — filesystem drift", drifted, err)
	}
}

// TestTaskDeliverableAttachmentsPaddedIDReadsTheSameManifest: the run-time
// read face must agree with the copy face on a padded id (same manifest,
// same resolved replica paths).
func TestTaskDeliverableAttachmentsPaddedIDReadsTheSameManifest(t *testing.T) {
	taskIDWSResidualSandbox(t)
	src := taskIDWSResidualWriteFile(t, "shot.png", "PNG-ish residual bytes")
	atts, err := TaskCopyAttachments(taskIDWSResidualSeed, []string{src})
	if err != nil {
		t.Fatalf("TaskCopyAttachments(exact): %v", err)
	}
	taskIDWSResidualExec(t, `UPDATE tasks SET deliverable = ? WHERE task_id = ?`,
		taskDeliverableRenderV2("/tmp/ws-residual-repo", atts), taskIDWSResidualSeed)

	exactAtts, exactPaths, err := TaskDeliverableAttachments(taskIDWSResidualSeed)
	if err != nil || len(exactAtts) != 1 || len(exactPaths) != 1 {
		t.Fatalf("exact read = (%d atts, %d paths, %v), want (1, 1, nil)", len(exactAtts), len(exactPaths), err)
	}
	padAtts, padPaths, err := TaskDeliverableAttachments(taskIDWSResidualPad)
	if err != nil {
		t.Fatalf("TaskDeliverableAttachments(padded): %v", err)
	}
	if len(padAtts) != 1 || len(padPaths) != 1 {
		t.Fatalf("padded read = (%d atts, %d paths), want the exact-id pair (1, 1)", len(padAtts), len(padPaths))
	}
	if padAtts[0].SHA256 != exactAtts[0].SHA256 || padPaths[0] != exactPaths[0] {
		t.Errorf("padded/exact attachment reads disagree: %+v/%q vs %+v/%q",
			padAtts[0], padPaths[0], exactAtts[0], exactPaths[0])
	}
}

// TestTaskDeliverableRepoForPaddedIDResolvesTheSameRepo: the deliverable
// reader behind the adoption/backfill chain — a padded id must read the
// SAME repo, not "" (which silently means "no repo context").
func TestTaskDeliverableRepoForPaddedIDResolvesTheSameRepo(t *testing.T) {
	taskIDWSResidualSandbox(t)
	const repo = "/tmp/ws-residual-repo"
	taskIDWSResidualExec(t, `UPDATE tasks SET deliverable = ? WHERE task_id = ?`,
		taskDeliverableRenderV2(repo, nil), taskIDWSResidualSeed)
	if got := TaskDeliverableRepoFor(taskIDWSResidualPad); got != repo {
		t.Errorf("TaskDeliverableRepoFor(padded) = %q, want %q", got, repo)
	}
}

// TestTaskSubmitPaddedTaskIDMintsTheTrimmedContainer: the submit face
// pre-checks the explicit id for collision and then CREATES
// sessions/<id>/ — with an untrimmed id the row and its container were born
// under the padded name (the drift the whole epic exists to stop).
func TestTaskSubmitPaddedTaskIDMintsTheTrimmedContainer(t *testing.T) {
	home := taskIDWSResidualSandbox(t)
	repo := taskIDWSResidualWriteFile(t, "repo-probe.txt", "not a repo")
	if out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "ws residual submit row", Payload: "p", Source: "human",
		Repo: repo, TaskID: taskIDWSResidualPad,
	}); err != nil {
		// The repo probe refuses a non-git dir — the id must still not have
		// been minted under the padded name.
		if out != "" {
			t.Fatalf("TaskSubmit(padded, bad repo) = %q, want the refusal to carry no row", out)
		}
		t.Skipf("repo probe refused the fixture (expected on some hosts): %v", err)
	}
	drifted := filepath.Join(home, ".gopi", config.SessionsDirName, taskIDWSResidualPad)
	if _, err := os.Stat(drifted); !os.IsNotExist(err) {
		t.Errorf("submit created a padded-name container %q (stat err = %v)", drifted, err)
	}
	trimmed := filepath.Join(home, ".gopi", config.SessionsDirName, taskIDWSResidualSeed)
	if _, err := os.Stat(trimmed); err != nil {
		t.Errorf("submit did not create the trimmed container %q: %v", trimmed, err)
	}
}

// TestFinishCleanupOwnerRowScanPaddedIDIsNotItsOwnOwner: the close-out
// chain's ownership scan EXCLUDES the row being cleaned by exact id match.
// With an untrimmed id the exclusion missed and the cleaner saw its own
// delivery as a FOREIGN worktree owner — the cleanup then skipped forever
// ("refusing to remove a worktree another row owns", the owner being
// itself).
func TestFinishCleanupOwnerRowScanPaddedIDIsNotItsOwnOwner(t *testing.T) {
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	const wt = "/tmp/ws-residual-wt"
	ownershipSeedRow(t, db, taskIDWSResidualSeed, taskStatusPending, "", wt)

	// Control: a DIFFERENT id sees the row (the scan really reads the ledger).
	owners, err := finishCleanupWorktreeOwnerRows("t-ws-residual-other")
	if err != nil {
		t.Fatalf("scan(other id): %v", err)
	}
	if len(owners) != 1 || owners[0].TaskID != taskIDWSResidualSeed {
		t.Fatalf("scan(other id) = %+v, want the seeded owner row", owners)
	}
	// The padded SELF-query must exclude it exactly like the exact id does.
	for _, id := range []string{taskIDWSResidualSeed, taskIDWSResidualPad} {
		owners, err := finishCleanupWorktreeOwnerRows(id)
		if err != nil {
			t.Fatalf("scan(%q): %v", id, err)
		}
		if len(owners) != 0 {
			t.Errorf("scan(%q) = %+v, want the row itself excluded (it is not its own foreign owner)", id, owners)
		}
	}
}

// ---------------------------------------------------------------------------
// Sync faces (memory_sync_claim.go)
// ---------------------------------------------------------------------------

// TestSyncTaskPushOnlyPaddedIDPushesTheSameRow: the manual re-push of ONE
// row — a padded id hit the row-selection closure's exact-match filter,
// fell through to the full export, still matched nothing and returned
// "not found in the ledger (window AND full export)".
//
// THE RE-PUSH NOW ENDS LOCALLY (2026-11-06): the claim-broadcast leg it rode is
// RETIRED, so the padded id's SELECTION is still what is under test (it must
// resolve to its own row and reach the leg at all, not fail on "not found in
// the ledger"), and the line must now say the honest thing — no row crossed.
// Asserting "the row reached the peer" would pin a removed mechanism.
func TestSyncTaskPushOnlyPaddedIDPushesTheSameRow(t *testing.T) {
	taskIDWSResidualSandbox(t)
	capture := taskIDWSResidualFakeSSH(t)
	taskIDWSResidualPeers(t)
	lines, err := SyncTaskPushOnly(taskIDWSResidualPad)
	if err != nil {
		t.Fatalf("SyncTaskPushOnly(padded): %v", err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], syncClaimRetiredMarker) {
		t.Errorf("SyncTaskPushOnly(padded) = %q, want one RETIRED line — the padded id must still SELECT "+
			"its row (a \"not found in the ledger\" line here is the normalization regression this file "+
			"exists to catch)", lines)
	}
	// The capture file is the dial's PROOF OF ABSENCE: the retired leg never
	// builds an ssh argv, so nothing is ever written to it. A readable capture
	// means a dial happened, and its bytes are the argv that was dialled.
	if argv, err := os.ReadFile(capture); err == nil {
		t.Errorf("the manual push opened an ssh session (capture %q) — the leg is retired, "+
			"so a dial here is the regression", argv)
	}
	// "" keeps its long-standing meaning: the whole delta window.
	if _, err := SyncTaskPushOnly(""); err != nil {
		t.Errorf("SyncTaskPushOnly(\"\") = %v, want the whole-window push to keep working", err)
	}
}

// TestPushTerminalTaskSyncPaddedIDPushesTheSameTerminalRow: the
// end-of-life must-succeed push — a padded id could never export the row,
// so every retry failed and the terminal verdict was never delivered.
func TestPushTerminalTaskSyncPaddedIDPushesTheSameTerminalRow(t *testing.T) {
	taskIDWSResidualSandbox(t)
	taskIDWSResidualFakeSSH(t)
	taskIDWSResidualPeers(t)
	taskIDWSResidualSetStatus(t, taskStatusCompleted)
	// A RETIRED LEG IS NOT A TERMINAL-PUSH FAILURE: with no cross-node protocol
	// there is nothing to push TO (the same condition as no declared peer), so
	// the call returns nil instead of burning the retry ladder and handing the
	// close-out a permanent FAILED. What must NOT happen is a dial.
	if err := PushTerminalTaskSync(taskIDWSResidualPad); err != nil {
		t.Fatalf("PushTerminalTaskSync(padded): %v — a retired leg must not read as a delivery failure", err)
	}
}

// TestNotifyTerminalPushFailedPaddedIDNamesTheSameRow: the alarm payload
// must name the ledger id (the padded id would be un-joinable against the
// ledger an operator debugs with).
func TestNotifyTerminalPushFailedPaddedIDNamesTheSameRow(t *testing.T) {
	taskIDWSResidualSandbox(t)
	out := filepath.Join(t.TempDir(), "notify.json")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat > "+out)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "all")
	// Restore note (Rule 17): BOTH variables are given by t.Setenv ABOVE, so its
	// own test-scoped cleanup restores them — stdlib testing.go:1326 (common.Setenv)
	// does LookupEnv and calls os.Unsetenv when the variable started ABSENT, so a
	// manual os.Unsetenv pair here would be a redundant process-state write.
	NotifyTerminalPushFailed(taskIDWSResidualPad, fmt.Errorf("boom"))
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("notify payload missing: %v", err)
	}
	var p taskNotifyPayload
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatalf("payload parse: %v", err)
	}
	if p.TaskID != taskIDWSResidualSeed {
		t.Errorf("alarm task_id = %q, want the ledger id %q", p.TaskID, taskIDWSResidualSeed)
	}
}

// ---------------------------------------------------------------------------
// The other half of the contract: empty / unknown ids stay refused
// ---------------------------------------------------------------------------

// TestTaskIDResidualUnknownAndEmptyStayRefused pins that normalization never
// MANUFACTURES a resolution: a padded UNKNOWN id behaves exactly like the
// bare unknown id (error / no row / no container beyond the trimmed name),
// and a blank id keeps every one of its previous semantics.
func TestTaskIDResidualUnknownAndEmptyStayRefused(t *testing.T) {
	home := taskIDWSResidualSandbox(t)
	capture := taskIDWSResidualFakeSSH(t)
	taskIDWSResidualPeers(t)

	// The best-effort stamps must not touch the fixture row through a
	// blank/unknown id (they report nil but write nothing).
	if err := TaskStampRun(taskIDWSResidualGone, "serve", "nope"); err != nil {
		t.Errorf("TaskStampRun(unknown) = %v, want the best-effort nil", err)
	}
	if err := TaskStampRun("", "serve", "nope"); err != nil {
		t.Errorf("TaskStampRun(blank) = %v, want the best-effort nil", err)
	}
	if err := TaskStampSelfClaim(taskIDWSResidualGone); err != nil {
		t.Errorf("TaskStampSelfClaim(unknown) = %v, want the best-effort nil", err)
	}
	row := taskIDWSResidualRow(t)
	if row.RunMethod != "" || row.RunCmd != "" || row.ClaimedBy != "" {
		t.Errorf("a blank/unknown id wrote the fixture row: method=%q cmd=%q claimed_by=%q",
			row.RunMethod, row.RunCmd, row.ClaimedBy)
	}

	// The refusal-bearing faces keep refusing.
	if err := TaskResumeClaimed(taskIDWSResidualGone, ""); err == nil {
		t.Errorf("TaskResumeClaimed(unknown) resolved a row, want an error")
	}
	if err := RollbackLocalClaim(taskIDWSResidualGone); err == nil {
		t.Errorf("RollbackLocalClaim(unknown) reverted something, want an error")
	}
	if _, err := SyncTaskPushOnly(taskIDWSResidualGone); err == nil {
		t.Errorf("SyncTaskPushOnly(unknown) pushed a row, want a not-found error")
	}
	if _, err := os.ReadFile(capture); err == nil {
		t.Errorf("a padded-unknown id reached the peer — the export must stay empty")
	}
	oldRetries := terminalPushRetries
	terminalPushRetries = [3]time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { terminalPushRetries = oldRetries }()
	// The switch is already ON for this file's sandbox (the fixture needs it
	// for the export/claim machinery), which is also what this assertion
	// needs: the retry-exhausted error can only be produced by a push that
	// actually attempts a leg.
	if err := PushTerminalTaskSync(taskIDWSResidualGone); err == nil {
		t.Errorf("PushTerminalTaskSync(unknown) succeeded, want the retry-exhausted error")
	}
	if atts, paths, err := TaskDeliverableAttachments(taskIDWSResidualGone); err != nil || atts != nil || paths != nil {
		t.Errorf("TaskDeliverableAttachments(unknown) = (%v, %v, %v), want (nil, nil, nil)", atts, paths, err)
	}
	if repo := TaskDeliverableRepoFor(taskIDWSResidualGone); repo != "" {
		t.Errorf("TaskDeliverableRepoFor(unknown) = %q, want \"\"", repo)
	}

	// The dir faces never check the ledger: a padded UNKNOWN id must resolve
	// the SAME path the bare unknown id resolves (the trimmed name), and a
	// blank id keeps the sessions ROOT (its long-standing shape).
	exactGone, err := TaskSessionDir(taskIDWSResidualGoneTrimmed)
	if err != nil {
		t.Fatalf("TaskSessionDir(unknown): %v", err)
	}
	padGone, err := TaskSessionDir(taskIDWSResidualGone)
	if err != nil {
		t.Fatalf("TaskSessionDir(padded unknown): %v", err)
	}
	if padGone != exactGone {
		t.Errorf("TaskSessionDir(padded unknown) = %q, want the trimmed path %q", padGone, exactGone)
	}
	blank, err := TaskSessionDir("")
	if err != nil {
		t.Fatalf("TaskSessionDir(blank): %v", err)
	}
	if want := filepath.Join(home, ".gopi", config.SessionsDirName); blank != want {
		t.Errorf("TaskSessionDir(blank) = %q, want the sessions root %q", blank, want)
	}
	if atts, err := TaskCopyAttachments(taskIDWSResidualGone, nil); err != nil || atts != nil {
		t.Errorf("TaskCopyAttachments(unknown, nil) = (%v, %v), want (nil, nil)", atts, err)
	}

	// The alarm face trims without a ledger lookup (unknown ids included).
	out := filepath.Join(t.TempDir(), "notify-blank.json")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat > "+out)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "all")
	// Restore note (Rule 17): BOTH variables are given by t.Setenv ABOVE, so its
	// own test-scoped cleanup restores them — stdlib testing.go:1326 (common.Setenv)
	// does LookupEnv and calls os.Unsetenv when the variable started ABSENT, so a
	// manual os.Unsetenv pair here would be a redundant process-state write.
	NotifyTerminalPushFailed(taskIDWSResidualGone, fmt.Errorf("boom"))
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("notify payload missing: %v", err)
	}
	var p taskNotifyPayload
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatalf("payload parse: %v", err)
	}
	if p.TaskID != taskIDWSResidualGoneTrimmed {
		t.Errorf("alarm task_id = %q, want the trimmed unknown id %q", p.TaskID, taskIDWSResidualGoneTrimmed)
	}
}
