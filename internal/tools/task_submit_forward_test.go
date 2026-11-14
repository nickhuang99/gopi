package tools

// task_submit_forward_test.go — the REPRO for the remote-submit ledger
// address defect (epic remote-submit-central-ledger).
//
// The defect: a `task_submit` issued on a remote leg wrote its row into the
// PROCESS-LOCAL ledger (memoryDBPath() → ~/.gopi/memory.db) even though that
// node declares itself a client whose ledger is a MIRROR of the central one.
// The write succeeded, the operator was told "Task queued: …", and the row
// was a black hole: never claimed, notified or verified.
//
// The scene is the real two-node topology simulated in-process (the
// established pattern: the ssh leg is replaced by the peer's REAL handler,
// never a mock of the merge): a MIRROR ledger and the AUTHORITATIVE ledger
// are two files, GOPI_MEMORY_DB points at the mirror, and the node declares
// role=client + sync_to=<peer>. The forward seam drives the peer's RECEIPT
// path against the authoritative ledger. (It used to run SyncClaimRemote,
// the `--claim-remote` handler; that peer handler was deleted with the
// cross-node claim lock, so the seam now stubs the receipt itself — the
// assertions below are about the LEDGER the row lands in, which is the
// property this file exists to pin.)
//
// RED before the fix: the row lands in the MIRROR ledger and the central
// ledger stays empty (asserted here as "the central ledger MUST carry the
// row"). GREEN after: the central ledger carries it, the mirror carries the
// echo, and a refused forward leaves NEITHER ledger with a row.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agencyscan"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tasksource"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// taskForwardPeer is the authoritative alias the test node declares; the
// value is opaque (no ssh is ever dialled — the seam replaces the transport).
const taskForwardPeer = "peer1"

// taskForwardScene builds the two ledgers and points the process at the
// MIRROR one, with the node declaring itself a client of taskForwardPeer.
// It returns the mirror + authoritative DB paths and the env switch that
// makes the seam's in-process peer handler read the authoritative ledger.
func taskForwardScene(t *testing.T) (mirrorDB, authDB string) {
	t.Helper()
	dir := t.TempDir()
	mirrorDB = filepath.Join(dir, "mirror-memory.db")
	authDB = filepath.Join(dir, "central-memory.db")
	t.Logf("scene: mirror=%s central=%s", mirrorDB, authDB)
	for _, p := range []string{mirrorDB, authDB} {
		db, err := openTasksDB(context.Background(), p)
		if err != nil {
			t.Fatalf("create ledger %s: %v", p, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close ledger %s: %v", p, err)
		}
	}
	// The suite pins the ledger env at process start (internal/testutil
	// ledgerpin); the SCENE's flip is test-scoped (t.Setenv restores it), and
	// each per-CALL ledger read below saves/restores explicitly, because
	// t.Setenv's undo scope is the test.
	t.Setenv(config.MemoryDBEnv, mirrorDB)
	t.Setenv(config.RoleEnv, string(config.NodeRoleClient))
	t.Setenv(config.SyncToEnv, taskForwardPeer)
	// THE TASK-ROW-SYNC SWITCH: this scene deliberately describes a node that
	// EXCHANGES task rows (that is what "role=client + sync_to" meant before
	// the switch existed), so it declares the exchange ON. Without this line
	// the scene would describe the AUTHORS' current deployment instead — a
	// client node that syncs nothing, whose submits are LOCAL (asserted by
	// TestClientNodeSubmitsLocallyWithTaskSyncOff in task_sync_switch_test.go).
	// One line here, not a per-test edit: every case below is about HOW a
	// forward behaves, not about whether one happens.
	t.Setenv(config.TaskSyncEnv, config.EnvBoolOn)
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	return mirrorDB, authDB
}

// taskForwardSeamOn installs the in-process peer transport: the peer's REAL
// session-import handler (SyncRemoteServe) runs against the authoritative ledger while the call
// is in flight, then the initiator's mirror ledger is restored. The env flip
// is per-CALL (undone when the hook returns), so it is the reviewed
// SnapshotEnv + os.Setenv pair rather than t.Setenv (whose undo scope is the
// test).
func taskForwardSeamOn(t *testing.T, mirrorDB, authDB string) {
	t.Helper()
	oldFwd, oldConfirm := taskSubmitForwardSeam, taskSubmitConfirmSeam
	taskSubmitForwardSeam = func(peer string, body []byte) ([]byte, error) {
		// Snapshotted at hook entry and restored when the hook RETURNS —
		// absence included, so the suite's ledger pin cannot be polluted.
		defer testutil.SnapshotEnv(t, config.MemoryDBEnv)()
		if err := os.Setenv(config.MemoryDBEnv, authDB); err != nil { //stylegate:ignore the flip must undo when THIS HOOK returns, not at test cleanup // env-leak-ok: SnapshotEnv restores on hook return, absence included
			return nil, err
		}
		// The peer's REAL session handler: this is the verb that creates a
		// row the peer has never seen.
		return SyncRemoteServe(body)
	}
	taskSubmitConfirmSeam = func(peer, remote, taskID string) error {
		// Same in-process answer the real `gopi task show <id>` prints.
		row := taskRowIn(t, authDB, taskID)
		if row == nil {
			return errTaskForwardUnconfirmable
		}
		return nil
	}
	t.Cleanup(func() { taskSubmitForwardSeam, taskSubmitConfirmSeam = oldFwd, oldConfirm })
}

// taskRowIn reads one row straight out of a NAMED ledger file. The read
// flips the process env to that file per call (SnapshotEnv pair) because
// openTasksDB's schema resolution consults the process configuration, not
// only its argument — a read of ledger B while the process points at ledger
// A would otherwise heal/return the wrong file.
func taskRowIn(t *testing.T, dbPath, taskID string) *Task {
	t.Helper()
	defer testutil.SnapshotEnv(t, config.MemoryDBEnv)()
	if err := os.Setenv(config.MemoryDBEnv, dbPath); err != nil { //stylegate:ignore the flip must undo when THIS CALL returns, not at test cleanup // env-leak-ok: SnapshotEnv restores on return, absence included
		t.Fatalf("point the reader at %s: %v", dbPath, err)
	}
	db, err := openTasksDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer db.Close()
	row, err := taskByID(context.Background(), db, taskID)
	if err != nil {
		t.Fatalf("read %s from %s: %v", taskID, dbPath, err)
	}
	return row
}

// taskIDFromConfirmation extracts the task id from the submit confirmation
// line ("Task queued: <id> (status=…)").
func taskIDFromConfirmation(t *testing.T, out string) string {
	t.Helper()
	const prefix = "Task queued: "
	i := strings.Index(out, prefix)
	if i < 0 {
		t.Fatalf("confirmation does not name the queued task: %q", out)
	}
	rest := out[i+len(prefix):]
	id := strings.TrimSpace(strings.SplitN(rest, " ", 2)[0])
	if id == "" {
		t.Fatalf("empty task id in confirmation %q", out)
	}
	return id
}

// TestTaskSubmitRemoteLegWritesTheAuthoritativeLedger is the repro: on a
// mirror leg the row must exist in the AUTHORITATIVE ledger. Pre-fix this
// fails with "central ledger has no row <id>" — the row went to the mirror.
func TestTaskSubmitRemoteLegWritesTheAuthoritativeLedger(t *testing.T) {
	mirrorDB, authDB := taskForwardScene(t)
	taskForwardSeamOn(t, mirrorDB, authDB)

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "remote submit must land in the central ledger",
		Payload: "briefing text",
		Tags:    "bug",
		Source:  "human",
	})
	if err != nil {
		t.Fatalf("submit on a mirror leg: %v", err)
	}
	id := taskIDFromConfirmation(t, out)

	// ① the AUTHORITATIVE ledger carries the row — the whole point.
	central := taskRowIn(t, authDB, id)
	if central == nil {
		t.Fatalf("authoritative ledger %s has NO row %s — the submit wrote somewhere else (confirmation: %q)", authDB, id, out)
	}
	if central.Status != taskStatusPending {
		t.Errorf("central row status = %q, want %q", central.Status, taskStatusPending)
	}
	if central.Summary != "remote submit must land in the central ledger" {
		t.Errorf("central row summary = %q", central.Summary)
	}
	if !hasTag(central.Tags, "bug") || !hasTag(central.Tags, taskSubmitForwardKind) {
		t.Errorf("central row tags = %q, want the row's own tag + the forward provenance", central.Tags)
	}
	// ② the mirror keeps the echo, so `task show` answers on the remote too.
	echo := taskRowIn(t, mirrorDB, id)
	if echo == nil {
		t.Fatalf("mirror ledger %s has no echo row %s", mirrorDB, id)
	}
	if !hasTag(echo.Tags, taskSubmitForwardedByPrefix+taskForwardPeer) {
		t.Errorf("mirror echo tags = %q, want the %s provenance", echo.Tags, taskSubmitForwardedByPrefix+taskForwardPeer)
	}
	// ③ the submitter is told which ledger owns the row.
	if !strings.Contains(out, taskForwardPeer) {
		t.Errorf("confirmation must name the authoritative ledger, got %q", out)
	}
}

// TestTaskSubmitRemoteLegRefusalWritesNoRow is the negative half (Rule 15b):
// when the forward cannot be delivered, the submit REFUSES loudly — naming
// the would-be local path — and writes NO row anywhere. A silent node-local
// write is the defect; a silent *empty* ledger would be the same class.
func TestTaskSubmitRemoteLegRefusalWritesNoRow(t *testing.T) {
	mirrorDB, authDB := taskForwardScene(t)
	_ = authDB // the authoritative ledger must stay EMPTY on a refusal
	old := taskSubmitForwardSeam
	taskSubmitForwardSeam = func(peer string, body []byte) ([]byte, error) {
		return nil, errTaskForwardUnreachable // a peer that never answers
	}
	t.Cleanup(func() { taskSubmitForwardSeam = old })

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "row that must not be written locally", Source: "human"})
	if err == nil {
		t.Fatalf("an undeliverable forward must REFUSE, got success: %q", out)
	}
	msg := err.Error()
	for _, want := range []string{"REFUSED", taskForwardPeer, mirrorDB, config.SyncToSettingsKey} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal must name %q, got: %s", want, msg)
		}
	}
	// The decisive assertion: no black-hole row in the local (mirror) ledger.
	if rows := taskRowsIn(t, mirrorDB); len(rows) != 0 {
		t.Fatalf("the refusal wrote %d row(s) into the local ledger %v — a silent node-local write is the defect", len(rows), rows)
	}
	if rows := taskRowsIn(t, authDB); len(rows) != 0 {
		t.Fatalf("the authoritative ledger should be untouched by a failed forward, got %v", rows)
	}
}

// TestTaskSubmitDeclaredServerWritesLocally is the address decision's other
// half: a declared SERVER (the authoritative node) inserts locally and never
// dials a peer — the forward arm must not fire there.
func TestTaskSubmitDeclaredServerWritesLocally(t *testing.T) {
	mirrorDB, _ := taskForwardScene(t)
	t.Setenv(config.RoleEnv, string(config.NodeRoleServer))
	config.ResetNodeIdentityForTest()
	calls := 0
	old := taskSubmitForwardSeam
	taskSubmitForwardSeam = func(peer string, body []byte) ([]byte, error) {
		calls++
		return nil, errTaskForwardUnreachable
	}
	t.Cleanup(func() { taskSubmitForwardSeam = old })

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "local row on the authoritative node", Source: "human"})
	if err != nil {
		t.Fatalf("a server-node submit must not need a peer: %v", err)
	}
	if calls != 0 {
		t.Fatalf("the server arm dialled a peer %d time(s) — the ledger address is wrong", calls)
	}
	id := taskIDFromConfirmation(t, out)
	if row := taskRowIn(t, mirrorDB, id); row == nil {
		t.Fatalf("local ledger %s has no row %s", mirrorDB, id)
	}
}

// TestClientNodeSubmitsLocallyWithTaskSyncOff is the switch's own half of the
// address decision: a client node whose operator has NOT enabled task-row
// exchange keeps its rows in its OWN ledger. This is the upgrade posture (the
// switch is new and defaults OFF), and it is what makes "role=client" mean
// IDENTITY rather than "my ledger is somebody else's mirror".
//
// The seam counts dials: zero is the assertion, because the arm under test is
// the one that used to fire for every submit on such a node.
func TestClientNodeSubmitsLocallyWithTaskSyncOff(t *testing.T) {
	mirrorDB, authDB := taskForwardScene(t)
	t.Setenv(config.TaskSyncEnv, "")
	config.ResetNodeIdentityForTest()
	calls := 0
	old := taskSubmitForwardSeam
	taskSubmitForwardSeam = func(peer string, body []byte) ([]byte, error) {
		calls++
		return nil, errTaskForwardUnreachable
	}
	t.Cleanup(func() { taskSubmitForwardSeam = old })

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "client node, task sync off", Source: "human"})
	if err != nil {
		t.Fatalf("a client node that exchanges no task rows must submit locally: %v", err)
	}
	if calls != 0 {
		t.Fatalf("the client forward arm dialled a peer %d time(s) with task sync OFF", calls)
	}
	id := taskIDFromConfirmation(t, out)
	if row := taskRowIn(t, mirrorDB, id); row == nil {
		t.Fatalf("the local ledger %s has no row %s — a success receipt with no row is the black hole this file exists to prevent", mirrorDB, id)
	}
	if rows := taskRowsIn(t, authDB); len(rows) != 0 {
		t.Fatalf("task sync OFF put %d row(s) into the peer's ledger: %v", len(rows), rows)
	}
}

// TestTaskSubmitRemoteLegRefusesUnforwardableParent pins the LOUD REFUSAL for
// the one field the sync row cannot carry (Rule 15b): a parented submit would
// arrive on the authoritative ledger WITHOUT its tree edge, so it is refused
// naming the reason and both remedies — never forwarded with a silently
// dropped parent_task_id.
func TestTaskSubmitRemoteLegRefusesUnforwardableParent(t *testing.T) {
	mirrorDB, authDB := taskForwardScene(t)
	taskForwardSeamOn(t, mirrorDB, authDB)

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary:      "child of a split",
		ParentTaskID: "01a0b4bf-0000-7000-8000-000000000000",
		Source:       "human",
	})
	if err == nil {
		t.Fatalf("a parented remote submit must refuse (the parent column does not travel), got success: %q", out)
	}
	for _, want := range []string{"REFUSED", "parent_task_id", mirrorDB} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal must name %q, got: %s", want, err.Error())
		}
	}
	if rows := taskRowsIn(t, mirrorDB); len(rows) != 0 {
		t.Fatalf("the refusal wrote %d row(s) into the local ledger %v", len(rows), rows)
	}
	if rows := taskRowsIn(t, authDB); len(rows) != 0 {
		t.Fatalf("the refusal forwarded a row anyway: %v", rows)
	}
}

// TestTaskSubmitRemoteLegRefusesUnforwardablePriority pins the SECOND
// unforwardable field (same class, found by auditing SyncTaskRow's columns):
// the wire row carries no priority column, and the peer's import re-derives
// the tier from the tags alone — so an explicit --priority would be silently
// dropped on the very ledger that dispatches the row. It is refused instead.
//
// The CONTROL is in the same test: a submit whose explicit priority EQUALS
// the tag-derived tier must still forward (otherwise the guard would refuse
// every ordinary mirror-leg submit, since TaskSubmit defaults the field).
func TestTaskSubmitRemoteLegRefusesUnforwardablePriority(t *testing.T) {
	mirrorDB, authDB := taskForwardScene(t)
	taskForwardSeamOn(t, mirrorDB, authDB)

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary:  "urgent work",
		Tags:     taskTagDocs,     // tag-derived tier: taskPriorityDocs
		Priority: taskPriorityBug, // explicit tier: bug-fast — cannot travel
		Source:   "human",
	})
	if err == nil {
		t.Fatalf("an explicit priority the sync row cannot carry must refuse, got success: %q", out)
	}
	for _, want := range []string{"REFUSED", "priority", mirrorDB} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal must name %q, got: %s", want, err.Error())
		}
	}
	if rows := taskRowsIn(t, mirrorDB); len(rows) != 0 {
		t.Fatalf("the refusal wrote %d row(s) into the local ledger %v", len(rows), rows)
	}
	if rows := taskRowsIn(t, authDB); len(rows) != 0 {
		t.Fatalf("the refusal forwarded a row anyway: %v", rows)
	}

	// CONTROL: the same tags with the priority their derivation yields (the
	// value TaskSubmit itself would compute) forwards normally.
	out, err = TaskSubmit(TaskSubmitOptions{
		Summary: "docs work",
		Tags:    taskTagDocs,
		Source:  "human",
	})
	if err != nil {
		t.Fatalf("a forwardable submit must not be refused by the priority guard: %v", err)
	}
	id := taskIDFromConfirmation(t, out)
	if row := taskRowIn(t, authDB, id); row == nil {
		t.Fatalf("the authoritative ledger %s has no row %s", authDB, id)
	}
}

// taskRowsIn lists every task_id a ledger carries (fixture-free ground truth
// for the "no row at all" assertion).
func taskRowsIn(t *testing.T, dbPath string) []string {
	t.Helper()
	defer testutil.SnapshotEnv(t, config.MemoryDBEnv)()
	if err := os.Setenv(config.MemoryDBEnv, dbPath); err != nil { //stylegate:ignore the flip must undo when THIS CALL returns, not at test cleanup // env-leak-ok: SnapshotEnv restores on return, absence included
		t.Fatalf("point the reader at %s: %v", dbPath, err)
	}
	db, err := openTasksDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer db.Close()
	rows, err := db.QueryContext(context.Background(), `SELECT task_id FROM tasks ORDER BY id`)
	if err != nil {
		t.Fatalf("list %s: %v", dbPath, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan %s: %v", dbPath, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list %s: %v", dbPath, err)
	}
	return ids
}

// errTaskForwardUnreachable / errTaskForwardUnconfirmable stand in for the
// two ways a forward fails (the peer is down; the peer answers but does not
// carry the row). Their text never reaches the operator verbatim — the
// refusal wraps them — but both must be real error values, not nil.
var (
	errTaskForwardUnreachable   = errForwardStub{"ssh: connect to host port 22: Connection refused"}
	errTaskForwardUnconfirmable = errForwardStub{"gopi task show: no task with task_id"}
)

type errForwardStub struct{ msg string }

func (e errForwardStub) Error() string { return e.msg }

// --- provenance of a forwarded row (epic: the retired "auto" alias) --------

// taskShowIn renders `gopi task show` for a row read from a NAMED ledger
// (TaskShow itself reads memoryDBPath(), so the process env is flipped per
// call with the SnapshotEnv pair — the same discipline taskRowIn uses).
func taskShowIn(t *testing.T, dbPath, taskID string) string {
	t.Helper()
	defer testutil.SnapshotEnv(t, config.MemoryDBEnv)()
	if err := os.Setenv(config.MemoryDBEnv, dbPath); err != nil { //stylegate:ignore the flip must undo when THIS CALL returns, not at test cleanup // env-leak-ok: SnapshotEnv restores on return, absence included
		t.Fatalf("point the renderer at %s: %v", dbPath, err)
	}
	out, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("task show %s in %s: %v", taskID, dbPath, err)
	}
	return out
}

// TestForwardedRowCarriesTheSubmittersRoleNotATransport is the pin for the
// retired-alias defect. taskSubmitForwardSource used to be the string "auto",
// stamped on the wire row AND on the echo. "auto" is a RETIRED spelling whose
// one recorded meaning is tasksource.Merger (tasksource/source.go Normalize),
// so:
//
//   - the AUTHORITATIVE ledger — the ledger that claims, runs and adjudicates
//     the row — read every forwarded submit as a MERGE-PATH machine enqueue,
//     and `task show` there narrated it "harness/external tooling
//     (source=auto, no session recorded)";
//   - the LOCAL mirror copy read the same, so an operator on the client node
//     could not tell a forwarded submit from a merge conflict row.
//
// The decision (recorded on the constant): a forward moves a row's HOME, not
// its authorship. The wire row carries the SUBMITTER'S role (which TaskSubmit
// already resolved into ledger vocabulary), and the echo — the one copy that
// really is a product of the transport — carries tasksource.Forward.
//
// RED before: the authoritative row's source was "auto" (→ Merger here) and
// the echo's too. GREEN after: agent / forward, and the receipt names both.
func TestForwardedRowCarriesTheSubmittersRoleNotATransport(t *testing.T) {
	mirrorDB, authDB := taskForwardScene(t)
	taskForwardSeamOn(t, mirrorDB, authDB)

	// The caller's spelling is the OTHER retired alias on purpose: it is the
	// caller-side twin of the defect (a raw machine spelling reaching the
	// ledger), and the funnel must canonicalize it before the wire.
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "forwarded row provenance",
		Payload: "the briefing that makes it a real ticket",
		Tags:    "bug",
		Source:  "llm",
	})
	if err != nil {
		t.Fatalf("submit on a mirror leg: %v", err)
	}
	id := taskIDFromConfirmation(t, out)

	// ① the AUTHORITATIVE row: the submitter's ROLE, and never the alias.
	central := taskRowIn(t, authDB, id)
	if central == nil {
		t.Fatalf("authoritative ledger %s has no row %s", authDB, id)
	}
	gotCentral, known := tasksource.Normalize(central.Source)
	if !known {
		t.Fatalf("the forwarded row's source %q is not ledger vocabulary (want %s)", central.Source, tasksource.Policy())
	}
	if gotCentral == tasksource.Merger {
		t.Fatalf("the forwarded row reads as %q (raw %q) — a MERGE-PATH enqueue the merge path never made; a forward moves a row's HOME, never its authorship", gotCentral, central.Source)
	}
	if gotCentral != tasksource.Agent {
		t.Errorf("authoritative row source = %q (normalized %q), want %q (the submitter's role, canonicalized from the caller's %q)",
			central.Source, gotCentral, tasksource.Agent, "llm")
	}
	if central.Source != gotCentral {
		t.Errorf("the row stores the non-canonical spelling %q (normalizes to %q) — a row written today never carries an alias", central.Source, gotCentral)
	}

	// ② the LOCAL mirror copy: the one provenance the transport DOES author.
	echo := taskRowIn(t, mirrorDB, id)
	if echo == nil {
		t.Fatalf("mirror ledger %s has no echo row %s", mirrorDB, id)
	}
	gotEcho, known := tasksource.Normalize(echo.Source)
	if !known || gotEcho != tasksource.Forward {
		t.Errorf("mirror echo source = %q (normalized %q, known=%v), want %q", echo.Source, gotEcho, known, tasksource.Forward)
	}
	if gotEcho == tasksource.Merger {
		t.Errorf("the mirror echo reads as %q — the retired alias' mapping, not this node's forward", gotEcho)
	}

	// ③ the receipt names BOTH ledgers' provenance (one value would misdescribe
	// one of them), and the peer is still named.
	for _, want := range []string{"source=" + tasksource.Agent, "mirror_source=" + tasksource.Forward, taskForwardPeer} {
		if !strings.Contains(out, want) {
			t.Errorf("receipt must name %q, got: %s", want, out)
		}
	}

	// ④ `task show` RENDERS the value on both ledgers — the surface the
	// operator reads (Rule 31: the changed surface is run, not inferred).
	centralShow := taskShowIn(t, authDB, id)
	if strings.Contains(centralShow, "source=auto") || strings.Contains(centralShow, "merger") {
		t.Errorf("the authoritative row's submitted_by line still narrates the retired/machine role:\n%s", centralShow)
	}
	if !strings.Contains(centralShow, "LLM session") {
		t.Errorf("the authoritative row must render the submitter's role (an LLM session), got:\n%s", centralShow)
	}
	echoShow := taskShowIn(t, mirrorDB, id)
	if !strings.Contains(echoShow, tasksource.Forward) {
		t.Errorf("the mirror copy must render %q, got:\n%s", tasksource.Forward, echoShow)
	}
	if strings.Contains(echoShow, "merger") || strings.Contains(echoShow, "source=auto") {
		t.Errorf("the mirror copy still narrates a merge-path/retired role:\n%s", echoShow)
	}
}

// TestAgencyProposalSourceIsItsOwnVocabulary pins the second write-side use of
// the retired alias found by the audit: internal/agencyscan filed every
// improvement proposal with the bare "auto", whose Normalize mapping is
// merger — so an unattended mining pass announced itself to `task show` as a
// MERGE CONFLICT row. The miner's role now has its own value, and this test
// drives the funnel the miner really uses (internal/tools/agency.go's
// SetSubmitFunc seam) so the assertion is about the row, not the constant.
func TestAgencyProposalSourceIsItsOwnVocabulary(t *testing.T) {
	testutil.NewTestDBPath(t)
	if agencyscan.AutoTaggerSource == tasksource.Merger {
		t.Fatalf("the miner's source is %q — the retired alias read as the merge path's role", tasksource.Merger)
	}
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "proposal row provenance",
		Payload: "mined candidate",
		Tags:    agencyscan.TagImprovement + "," + agencyscan.TagAgencyProposal,
		Source:  agencyscan.AutoTaggerSource,
	})
	if err != nil {
		t.Fatalf("submit a proposal row: %v", err)
	}
	id := taskIDFromConfirmation(t, out)
	row := phase1TaskRow(t, id)
	if row.Source != tasksource.AgencyProposal {
		t.Errorf("proposal row source = %q, want %q", row.Source, tasksource.AgencyProposal)
	}
	if value, ok := tasksource.Normalize(row.Source); !ok || value != tasksource.AgencyProposal {
		t.Errorf("proposal row source %q is not accepted vocabulary as written (normalized %q, known=%v)", row.Source, value, ok)
	}
	show, err := TaskShow(id)
	if err != nil {
		t.Fatalf("task show %s: %v", id, err)
	}
	if !strings.Contains(show, tasksource.AgencyProposal) {
		t.Errorf("the proposal row must render %q, got:\n%s", tasksource.AgencyProposal, show)
	}
	if strings.Contains(show, "merger") {
		t.Errorf("the proposal row must not be narrated as a merge-path row:\n%s", show)
	}
}
