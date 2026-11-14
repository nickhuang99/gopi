package tools

// memory_sync_claim_test.go: the claim-lock unit matrix (M4.21 two-phase
// protocol). The ssh legs are NOT exercised here (no peer host in unit
// scope) — what IS exercised is the lock contract: the Phase-2/3 remote CAS
// (acquire + advance, exactly one winner, owner refusal), the rollback leg
// (claimed→pending, claimed_by kept as the audit trail), the collision
// classification (proceed/retry/yield), the ALOHA backoff sequence, and the
// round-exhaustion yield.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// configSyncPeersEnv re-exports the env name for t.Setenv use in this file.
const configSyncPeersEnv = config.SyncPeersEnv

func claimJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func claimUnmarshal(b []byte, v interface{}) error { return json.Unmarshal(b, v) }

// claimTestDB points the ledger at a temp file for one test (the standard
// GOPI_MEMORY_DB isolation, memory_sync_delta_test.go's newDeltaTestDB
// pattern — openTasksDB inits the schema on first open).
func claimTestDB(t *testing.T) func() {
	t.Helper()
	// The LOCAL ledger is this test's OWN PROFILE's ledger, not a file dropped
	// beside the operator's profile: GOPI_HOME moves to the temp home so the two
	// halves of the configuration agree — otherwise the notify target still
	// resolves to the operator's profile while the ledger does not, which is the
	// foreign-ledger shape the delivery gate (taskNotifyDelivers) suppresses and
	// these claim-collision fixtures would silently lose their events.
	home := t.TempDir()
	path := filepath.Join(config.ProfileDirIn(home), config.MemoryDBFileName)
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(memoryDBEnv, path)
	// M4.20-B + the ledger-address gate (85859202): pin UNDECLARED — the
	// single-machine default. IsServerNode() is false for it (N1 fail-safe),
	// so a server-role sandbox still cannot RULE on the claim-remote merge
	// residue instead of mirroring the claimer's in_progress rung — the
	// property this seed depends on. Undeclared is also the only role that
	// may write LOCALLY: a CLIENT pin with no sync_to peer makes TaskSubmit
	// refuse loudly (task_submit_forward.go), so claimSeedTask could not
	// seed its row at all. The peerless-mirror refusal is asserted, not
	// seeded, in remote_submit_repro_test.go.
	t.Setenv(config.RoleEnv, "")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close test db: %v", err)
	}
	return func() {}
}

// claimSeedRepo is the repo TaskSubmit validates with `git rev-parse` —
// the test environment's own package dir IS a git worktree, so it always
// passes (the repo value is never used beyond the validation probe).
func claimSeedRepo() string { return "." }

// claimSeedTask submits one task through the real write path and returns
// its id ("Task queued: t-... (status=pending …)").
func claimSeedTask(t *testing.T, summary string) string {
	t.Helper()
	out, err := TaskSubmit(TaskSubmitOptions{Summary: summary, Payload: "p: " + summary, Source: "human", Repo: claimSeedRepo()})
	if err != nil {
		t.Fatalf("seed submit: %v", err)
	}
	const marker = "Task queued: "
	idx := strings.Index(out, marker)
	if idx < 0 {
		t.Fatalf("seed submit: no task_id in %q", out)
	}
	rest := out[idx+len(marker):]
	if end := strings.IndexAny(rest, " \t("); end > 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}

// claimLocalClaim walks a fresh row through the local Phase-1 CAS claim
// (pending→claimed) — the lock protocol's precondition.
func claimLocalClaim(t *testing.T, id, owner string) {
	t.Helper()
	res, err := TaskNextFiltered(owner, "", "")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if res.Empty || res.Task.TaskID != id {
		t.Fatalf("claim: got %+v, want %s", res, id)
	}
}

// TestRollbackLocalClaimRevertsToPending: the M4.21 rollback semantics —
// claimed→pending, claimed_by KEPT (the audit trail), terminal rows never
// roll back, and the dispatcher CAN re-claim the row afterwards (the
// anti-deadlock property the old cancel semantics broke).
func TestRollbackLocalClaimRevertsToPending(t *testing.T) {
	claimTestDB(t)
	id := claimSeedTask(t, "claim rollback probe")
	claimLocalClaim(t, id, "tester")

	if err := RollbackLocalClaim(id); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	row := taskRowByID(t, id)
	if row.Status != taskStatusPending {
		t.Fatalf("want pending after rollback, got %s", row.Status)
	}
	if row.ClaimedBy == "" {
		t.Fatalf("claimed_by must be KEPT as the audit trail, got empty")
	}
	if !strings.Contains(row.Tags, claimRevertTag) {
		t.Fatalf("tags must carry %s, got %q", claimRevertTag, row.Tags)
	}

	// The anti-deadlock property: the reverted row is claimable again
	// (both sides get another chance — the nick veto of cancel).
	res, err := TaskNextFiltered("tester2", "", "")
	if err != nil {
		t.Fatalf("re-claim: %v", err)
	}
	if res.Empty || res.Task.TaskID != id {
		t.Fatalf("reverted row must be re-claimable (no dual-abandon deadlock), got %+v", res)
	}

	// Terminal rows never roll back (the writeback that raced wins).
	if _, err := TaskStart(id, "tester2"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := TaskUpdate(id, taskStatusCompleted); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := RollbackLocalClaim(id); err == nil {
		t.Fatalf("rollback on completed row must refuse")
	}
	if row := taskRowByID(t, id); row.Status != taskStatusCompleted {
		t.Fatalf("completed row clobbered: %s", row.Status)
	}
}

// TestRollbackLocalClaimRevertsInProgress: the anchor-guard incident
// (2026-09-02) — runTaskRunOnce calls TaskStart (claimed→in_progress)
// BEFORE the claim-lock protocol, so a yield leaves the row in_progress.
// RollbackLocalClaim must revert that shape too, or the row is a permanent
// zombie (dead pid, in_progress, no local transition out) that wedges the
// pending-count anchor guard forever. The revert is safe: the yield point
// precedes the child spawn, so no execution is racing.
func TestRollbackLocalClaimRevertsInProgress(t *testing.T) {
	claimTestDB(t)
	id := claimSeedTask(t, "in_progress rollback probe")
	claimLocalClaim(t, id, "tester")
	if _, err := TaskStart(id, "tester"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if row := taskRowByID(t, id); row.Status != taskStatusInProgress {
		t.Fatalf("precondition: row must be in_progress, got %s", row.Status)
	}
	if err := RollbackLocalClaim(id); err != nil {
		t.Fatalf("rollback in_progress: %v", err)
	}
	if row := taskRowByID(t, id); row.Status != taskStatusPending {
		t.Fatalf("want pending after in_progress rollback, got %s", row.Status)
	}
}

// TestExportClaimPatchCarriesPeerOriginRow: the claim-lock acquire patch
// must reach the row's ORIGIN even when the local copy is tagged
// from-<peer> (HP claiming a from-zpeer row). The M4.24 anti-reflow gate
// (c4745d9) excluded exactly those live rows from the export → the acquire
// patch came back empty ("not found in the ledger") → every round failed →
// yield into the stuck zombie (the anchor-guard incident). The claim
// channel bypasses the gate: the peer's --claim-remote handler owns the
// CAS/refusal decision, and an echo row no-ops idempotently.
func TestExportClaimPatchCarriesPeerOriginRow(t *testing.T) {
	claimTestDB(t)
	id := claimSeedTask(t, "peer-origin claim probe")
	// Simulate the row having been imported from peer "zpeer": tag it
	// from-zpeer and put it in the live in_progress rung (the claim
	// protocol's acquire-verb state after TaskStart).
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET tags = ?, status = ?, updated_at = ? WHERE task_id = ?`,
		"from-zpeer,"+claimRevertTag, taskStatusInProgress, float64(time.Now().Unix()), id); err != nil {
		t.Fatalf("tag+advance row: %v", err)
	}
	// Pre-fix this errored "not found in the ledger" because the export
	// gate dropped the from-zpeer live row before row selection.
	patch, err := exportClaimPatch(id, "hp-runner", "zpeer")
	if err != nil {
		t.Fatalf("exportClaimPatch must carry a from-peer live row, got: %v", err)
	}
	if len(patch.Rows) != 1 || patch.Rows[0].TaskID != id {
		t.Fatalf("want exactly the target row %s, got %+v", id, patch.Rows)
	}
	if patch.Rows[0].Status != taskStatusInProgress {
		t.Fatalf("row must ride verbatim in_progress, got %s", patch.Rows[0].Status)
	}
}

func TestClaimSSHCommandShape(t *testing.T) {
	// The argv shape is load-bearing: one remote element, BatchMode, the
	// claim verb. A wrong shape would land the verb as local argv (the
	// agent-loop hazard the session driver already documented).
	ctx, cancel := contextWithTimeout(time.Second)
	defer cancel()
	cmd := newSSHCommand(ctx, "somepeer", syncRemoteGopi+" task sync --claim-remote")
	// Args: [ssh, -o, BatchMode=yes, -o, ConnectTimeout=N, peer, remote] —
	// 7 elements total, the remote verb as the single last element.
	if len(cmd.Args) != 7 || cmd.Args[5] != "somepeer" {
		t.Fatalf("ssh argv shape drifted (want peer at [5]): %v", cmd.Args)
	}
	if !strings.HasSuffix(cmd.Args[len(cmd.Args)-1], "task sync --claim-remote") {
		t.Fatalf("remote verb must be the single last element, got %q", cmd.Args[len(cmd.Args)-1])
	}
	// BatchMode keeps a daemon from ever prompting.
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "BatchMode=yes") {
		t.Fatalf("claim ssh leg must be BatchMode: %v", cmd.Args)
	}
}

func TestSyncTaskPushOnlyRequiresPeers(t *testing.T) {
	claimTestDB(t)
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	t.Setenv("GOPI_SETTINGS", filepath.Join(sandbox, "settings.json"))
	t.Setenv(configSyncPeersEnv, "") // the no-peer arm: an EMPTY topology (t.Setenv restores presence AND value at test end)
	if _, err := SyncTaskPushOnly("t-anything"); err == nil || !strings.Contains(err.Error(), "no sync_peers") {
		t.Fatalf("want no-sync_peers refusal, got %v", err)
	}
}

// taskRowByID reads one row back through taskByID (temp-DB bound).
func taskRowByID(t *testing.T, id string) *Task {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer closeDB()
	row, err := taskByID(context.Background(), db, id)
	if err != nil {
		t.Fatalf("taskByID: %v", err)
	}
	if row == nil {
		t.Fatalf("row %s vanished", id)
	}
	return row
}

func TestClaimSSHExchangeUnreachableFailsSoft(t *testing.T) {
	// A nonexistent peer alias must fail the ssh leg FAST (ConnectTimeout)
	// and be classified unreachable — the retry classification is the
	// whole lock-round contract for network failures.
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no ssh binary in test env")
	}
	old := claimLockTimeout
	claimLockTimeout = 3 * time.Second
	defer func() { claimLockTimeout = old }()
	_, err := claimSSHExchange("gopi-claim-test-no-such-host", []byte(`{"version":2,"rows":[]}`))
	if err == nil {
		t.Fatalf("unreachable peer must error")
	}
	if strings.Contains(err.Error(), "REFUSED") {
		t.Fatalf("ssh failure must not look like a refusal")
	}
}

// ---- M4.19 ② terminal must-succeed push ----

func TestPushTerminalTaskSyncNoPeersIsSuccess(t *testing.T) {
	claimTestDB(t)
	// Seed one terminal row so exportClaimPatch could find it IF peers were
	// declared — the no-topology case is vacuous success (nothing to push).
	id := claimSeedTask(t, "m419 no-peer terminal push")
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	t.Setenv("GOPI_SETTINGS", filepath.Join(sandbox, "settings.json"))
	t.Setenv(configSyncPeersEnv, "") // the no-peer arm: an EMPTY topology (t.Setenv restores presence AND value at test end)
	if err := PushTerminalTaskSync(id); err != nil {
		t.Fatalf("no sync_peers declared must be vacuous success, got %v", err)
	}
}

// THE PEER IS NOW UNREACHABLE VIA THE TRANSPORT SEAM, not via a real ssh
// attempt, because the production transport is RETIRED (2026-11-06): the leg
// used to compose `ssh <peer> gopi task sync --claim-remote`, a verb deleted on
// 2026-11-05, so dialling turned a removal notice into a protocol error at the
// far end of a connection. What is still under test here is the RETRY SCHEDULE
// — three attempts, an error naming the count — and it is exercised with the
// one transport error that still exists: a transport that did not ANSWER (the
// hook returns a plain error). A RETIRED leg takes the opposite arm and is
// pinned separately, by TestPushTerminalTaskSyncRetiredLegEndsLocally.
func TestPushTerminalTaskSyncUnreachableExhaustsRetries(t *testing.T) {
	claimTestDB(t)
	id := claimSeedTask(t, "m419 unreachable terminal push")
	// The task-row-sync switch ON: this case is about the RETRY SCHEDULE
	// exhausting against an unreachable peer, and with the switch at its
	// default OFF the push returns before the first attempt ever runs — the
	// test would pass without exercising a single retry. The switch-off arm
	// (a terminal verdict that never leaves the ledger) is pinned separately
	// by TestPushTerminalTaskSyncUnderTaskSyncOffStaysLocal.
	t.Setenv(config.TaskSyncEnv, config.EnvBoolOn)
	// A peer alias that cannot resolve/connect — an import loop back into
	// THIS db (t.Setenv peers=self) would make the push SUCCEED (same
	// process sees the same ledger), the opposite of the case under test.
	t.Setenv(configSyncPeersEnv, "gopi-m419-no-such-peer")
	// Shorten the retry schedule (Rule 1: schedule lives in one var) so the
	// 3 attempts finish fast; the count matters, not the wall clock.
	old := terminalPushRetries
	terminalPushRetries = [3]time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { terminalPushRetries = old }()
	// A transport that did not ANSWER — a plain error, deliberately NOT the
	// retired sentinel, because the two must not be collapsed (see the
	// retired-leg arm below): this one is worth retrying, that one is not.
	attempts := 0
	SetClaimSSHExchangeHookForTest(func(string, []byte) ([]byte, error) {
		attempts++
		return nil, errors.New("dial tcp: connection refused")
	})
	t.Cleanup(func() { SetClaimSSHExchangeHookForTest(nil) })
	err := PushTerminalTaskSync(id)
	if err == nil {
		t.Fatalf("unreachable peer must exhaust retries into an error")
	}
	if attempts != 3 {
		t.Fatalf("the retry schedule ran %d attempt(s), want 3", attempts)
	}
	if !strings.Contains(err.Error(), "failed after 3 attempt") {
		t.Fatalf("error must report the retry count, got %v", err)
	}
}

// TestPushTerminalTaskSyncRetiredLegEndsLocally is the arm that separates "the
// peer said no / did not answer" from "there is no longer a protocol to ask
// over". The retired leg returns WITHOUT burning the retry schedule and WITHOUT
// handing the close-out a permanent FAILED — the same answer the no-peer arm
// gives, because it is the same condition: nothing to push TO. Retrying it
// would sleep the backoff merely to re-learn a fact (measured 6.02s).
func TestPushTerminalTaskSyncRetiredLegEndsLocally(t *testing.T) {
	claimTestDB(t)
	id := claimSeedTask(t, "retired terminal push")
	t.Setenv(config.TaskSyncEnv, config.EnvBoolOn)
	t.Setenv(configSyncPeersEnv, "gopi-m419-retired-leg-peer")
	attempts := 0
	SetClaimSSHExchangeHookForTest(func(peer string, _ []byte) ([]byte, error) {
		attempts++
		return nil, remoteLegRetired("claim broadcast", peer)
	})
	t.Cleanup(func() { SetClaimSSHExchangeHookForTest(nil) })
	if err := PushTerminalTaskSync(id); err != nil {
		t.Fatalf("a RETIRED leg must end the push locally (nothing to push TO), got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("the retired leg was attempted %d time(s) — it must not be RETRIED (the schedule is for a "+
			"peer that did not answer, not for a protocol that no longer exists)", attempts)
	}
}

// TestPushTerminalTaskSyncUnderTaskSyncOffStaysLocal is the OFF arm of the
// terminal push: with automatic task-row exchange off (its default), a terminal
// verdict must stay on the ledger that executed the row and return the SAME
// vacuous success the no-peer arm returns — never an error (the close-out
// caller logs "terminal push: ok" and moves on) and never an attempted leg.
//
// The observation is the SAME unresolvable peer the ON arm uses:
// TestPushTerminalTaskSyncUnreachableExhaustsRetries proves that this exact
// alias turns an attempted push into a retry-exhausted error, so a `nil` here
// is evidence that no leg was attempted — not merely an absent complaint.
func TestPushTerminalTaskSyncUnderTaskSyncOffStaysLocal(t *testing.T) {
	claimTestDB(t)
	id := claimSeedTask(t, "task-sync-off terminal push")
	t.Setenv(config.TaskSyncEnv, "")
	t.Setenv(configSyncPeersEnv, "gopi-m419-no-such-peer")
	if err := PushTerminalTaskSync(id); err != nil {
		t.Fatalf("with task sync off a terminal push must be a no-op success (there is nothing to push TO), got %v", err)
	}
}

func TestNotifyTerminalPushFailedFiresCmd(t *testing.T) {
	claimTestDB(t)
	out := filepath.Join(t.TempDir(), "notify-capture.json")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat > "+out)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "all")
	notifyTerminalPushFailed("t-m419-alarm", fmt.Errorf("boom after retries"))
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("cmd target never wrote the payload: %v", err)
	}
	var p taskNotifyPayload
	if err := claimUnmarshal(b, &p); err != nil {
		t.Fatalf("payload parse: %v", err)
	}
	if p.Event != taskNotifyEventTerminalPushFailed {
		t.Fatalf("event = %q, want %q", p.Event, taskNotifyEventTerminalPushFailed)
	}
	if p.TaskID != "t-m419-alarm" || !strings.Contains(p.Summary, "boom after retries") {
		t.Fatalf("payload missing task/error detail: %+v", p)
	}
}

func TestNotifyTerminalPushFailedUnsubscribedIsSilent(t *testing.T) {
	claimTestDB(t)
	out := filepath.Join(t.TempDir(), "notify-capture.json")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat > "+out)
	// default event filter (claimed,in_progress,completed,failed,superseded)
	// — the synthetic event must NOT pass it.
	notifyTerminalPushFailed("t-m419-quiet", fmt.Errorf("boom"))
	if _, err := os.Stat(out); err == nil {
		t.Fatalf("unsubscribed event must not deliver, but cmd target fired")
	}
}
