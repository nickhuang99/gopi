package tools

// Regression suite: `gopi task clone` — THE CLEAN-ROOM COPY.
//
// The verb exists for exactly ONE reason (see task_clone.go's header): the
// operator has decided the source row's workspace is no longer trustworthy and
// wants the same work in a fresh room. Three properties are therefore
// load-bearing, and each gets its own assertion here:
//
//	① THE STATUS GATE. A clone is refused while the source may be RUNNING
//	   (a second run beside the first is the double-execution the runner exists
//	   to prevent), refused while the source has never run (nothing to distrust),
//	   and refused on an abandoned row unless the operator says so. Every refusal
//	   is asserted to have written NOTHING — a refusal that leaves a half-born
//	   row behind is worse than no refusal.
//	② THE SOURCE ROW IS UNTOUCHED. Cloning reads; it never moves validates or
//	   rewrites the original, whose verdict is a historical record.
//	③ THE GROUND DIFFERS. The successor's ground is derived from its OWN id, so
//	   the room really is a different directory — the one claim the verb makes.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/fullid"
	"github.com/nickhuang99/gopi/internal/tasktags"
)

// cloneSeedTerminal submits a row and walks it to `status` through the ledger's
// legal ladder, then stamps a deliverable carrying the ground its run owned.
func cloneSeedTerminal(t *testing.T, id, status, deliverable string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "seed row for the clean-room clone", TaskID: id,
		Payload: "acceptance: the cloned row runs in a clean room", Tags: "bug", Source: "llm"}); err != nil {
		t.Fatalf("submit %s: %v", id, err)
	}
	if status == TaskStatusPending {
		return
	}
	ladder := map[string][]string{
		TaskStatusInProgress: {TaskStatusInProgress},
		TaskStatusCancelled:  {TaskStatusCancelled},
		TaskStatusFailed:     {TaskStatusInProgress, TaskStatusFailed},
		TaskStatusCompleted:  {TaskStatusInProgress, TaskStatusCompleted},
		TaskStatusVerified:   {TaskStatusInProgress, TaskStatusCompleted, TaskStatusVerified},
	}[status]
	if ladder == nil {
		t.Fatalf("cloneSeedTerminal: %q has no legal ladder", status)
	}
	for _, step := range ladder {
		if _, err := TaskUpdate(id, step); err != nil {
			t.Fatalf("%s → %s: %v", id, step, err)
		}
	}
	if deliverable != "" {
		db, closeDB, err := taskSyncDB()
		if err != nil {
			t.Fatalf("open ledger: %v", err)
		}
		defer closeDB()
		if _, err := db.ExecContext(context.Background(),
			`UPDATE tasks SET deliverable = ? WHERE task_id = ?`, deliverable, id); err != nil {
			t.Fatalf("stamp deliverable on %s: %v", id, err)
		}
	}
}

// cloneRowCount is the assertion instrument the three refusals share: "the
// ledger gained no row" is measured, never inferred from an error message.
func cloneRowCount(t *testing.T) int {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer closeDB()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

// cloneRowSnapshot reads the source row's user-visible columns so a before/after
// comparison can prove the clone did not touch it (property ②).
func cloneRowSnapshot(t *testing.T, id string) string {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer closeDB()
	var summary, status, payload, tags, deliverable, parent string
	if err := db.QueryRowContext(context.Background(),
		`SELECT summary, status, COALESCE(payload,''), COALESCE(tags,''),
		        COALESCE(deliverable,''), COALESCE(parent_task_id,'')
		   FROM tasks WHERE task_id = ?`, id).
		Scan(&summary, &status, &payload, &tags, &deliverable, &parent); err != nil {
		t.Fatalf("snapshot %s: %v", id, err)
	}
	return summary + "\x00" + status + "\x00" + payload + "\x00" + tags + "\x00" + deliverable + "\x00" + parent
}

// cloneMintedSuccessorOf reads the row a clone minted back OFF THE LEDGER.
//
// IT MUST NOT BE ASSUMED FROM THE SOURCE ID. Since the operator's 2026-11-05
// ruling a clone mints a FRESH uuid v7, so there is no `<src>-c1` to guess —
// and a helper that guessed one would be asserting the very defect the ruling
// removed. The parent edge is what identifies it; the id is whatever the ledger
// minted.
func cloneMintedSuccessorOf(t *testing.T, parentID string) string {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer closeDB()
	var succ string
	if err := db.QueryRowContext(context.Background(),
		`SELECT task_id FROM tasks WHERE parent_task_id = ? ORDER BY id DESC LIMIT 1`, parentID).Scan(&succ); err != nil {
		t.Fatalf("read the successor of %s back off the ledger: %v", parentID, err)
	}
	if succ == "" || succ == parentID {
		t.Fatalf("the ledger holds no successor of %s", parentID)
	}
	return succ
}

// TestTaskCloneMintsACleanRoomRow is the happy path: a failed row yields a NEW
// row with its OWN uuid, pending, parented on the source, carrying its briefing
// and tags, with a ground that is NOT the source's directory.
func TestTaskCloneMintsACleanRoomRow(t *testing.T) {
	newVerifyTestDB(t)
	root := t.TempDir()
	t.Setenv("GOPI_WORKSPACE_ROOT", root)
	srcGround := filepath.Join(root, "gopi-src-task")
	cloneSeedTerminal(t, "t-cl", TaskStatusFailed, taskDeliverableRebuild(taskDeliverable{
		Repo: root, Branch: "task/t-cl", Worktree: srcGround,
	}))
	before := cloneRowSnapshot(t, "t-cl")

	out, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{TaskID: "t-cl"})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	for _, want := range []string{"parent=t-cl", TaskCloneMarker} {
		if !strings.Contains(out, want) {
			t.Errorf("the clone line must carry %q:\n%s", want, out)
		}
	}
	newID := cloneMintedSuccessorOf(t, "t-cl")
	if !strings.Contains(out, newID) {
		t.Errorf("the clone line must NAME the row the ledger minted (%q):\n%s", newID, out)
	}
	// ⓪ THE IDENTITY AXIS (operator ruling 2026-11-05, memory #27273): the new
	// row's id is a canonical uuid v7 — a FRESH identity, not `<src>-c<N>`.
	// This is the red proof: the pre-ruling mint answered `t-cl-c1`, which
	// fullid.IsUUID refuses, and every downstream reader that treats the id as
	// an identity (parent_task_id, deliverable, notify routing, --resume's
	// selector) lost the row.
	if !fullid.IsUUID(newID) {
		t.Fatalf("the clone minted %q, which is NOT a canonical uuid — a suffix destroys the identity every downstream reader resolves", newID)
	}
	t.Logf("clone minted %s (fullid.IsUUID=%v, version=%c) for source %s", newID, fullid.IsUUID(newID), newID[14], "t-cl")
	if strings.Contains(newID, "t-cl") {
		t.Fatalf("the clone's id %q carries the SOURCE id — a minted uuid must not encode its parent", newID)
	}
	if got := cloneRowSnapshot(t, "t-cl"); got != before {
		t.Errorf("the SOURCE row changed across the clone:\nbefore=%q\nafter =%q", before, got)
	}
	clone := mustTaskByID(t, newID)
	if clone.Status != TaskStatusPending {
		t.Errorf("clone status = %q, want %q", clone.Status, TaskStatusPending)
	}
	if clone.ParentTaskID != "t-cl" {
		t.Errorf("clone parent = %q, want t-cl (the lineage edge)", clone.ParentTaskID)
	}
	if !taskHasTagToken(clone.Tags, tasktags.ReworkLineageTag) {
		t.Errorf("clone tags = %q — without the lineage token Rule 33 parks it forever behind a "+
			"failed parent", clone.Tags)
	}
	// ⓪b THE LINEAGE AXIS — the provenance the id suffix used to spell is now
	// DATA on the row (operator ruling 2026-11-05): `clone` says why the row
	// exists, `rework-of:<source>` says what it copies. Without both, a reader
	// who used to learn the lineage from `<uuid>-c1` learns nothing.
	if !taskHasTagToken(clone.Tags, tasktags.CloneLineageTag) {
		t.Errorf("clone tags = %q — the row must SAY it is a clean-room copy (%q)", clone.Tags, tasktags.CloneLineageTag)
	}
	if want := tasktags.ReworkOfTagPrefix + "t-cl"; !taskHasTagToken(clone.Tags, want) {
		t.Errorf("clone tags = %q — the row must NAME the source it copies (%q)", clone.Tags, want)
	}
	if !strings.Contains(clone.Payload, "acceptance: the cloned row runs in a clean room") {
		t.Errorf("the clone's briefing must carry the source's text VERBATIM:\n%s", clone.Payload)
	}
	// ③ THE GROUND AXIS — the verb's whole promise.
	src := mustTaskByID(t, "t-cl")
	ground, _, err := taskCloneGround(src, clone.TaskID)
	if err != nil {
		t.Fatalf("derive the clone's ground: %v", err)
	}
	if ground == "" {
		t.Fatalf("the clone must resolve a ground under GOPI_WORKSPACE_ROOT=%s", root)
	}
	if filepath.Clean(ground) == filepath.Clean(srcGround) {
		t.Fatalf("the clone's ground IS the source's directory (%s) — that is the one outcome "+
			"this verb must never produce", ground)
	}
	// …and it is derived from the NEW row's OWN id, which is the whole reason a
	// fresh uuid matters: a ground named after `<src>-c1` would carry the parent's
	// id into a path, so two different rows would name paths that look related.
	if filepath.Base(ground) != clone.TaskID {
		t.Fatalf("the ground %q is not derived from the new row's id %q", ground, clone.TaskID)
	}
	if strings.Contains(filepath.Base(ground), "t-cl") {
		t.Fatalf("the ground name %q carries the SOURCE id — the new row's identity must stand alone", filepath.Base(ground))
	}
	if !strings.Contains(out, ground) {
		t.Errorf("the clone line must STATE the ground it promises:\nwant %q in:\n%s", ground, out)
	}
}

// TestTaskCloneRefusedWhileTheRowMayBeRunning: an in_progress source is refused
// BY NAME and the ledger gains no row.
func TestTaskCloneRefusedWhileTheRowMayBeRunning(t *testing.T) {
	newVerifyTestDB(t)
	cloneSeedTerminal(t, "t-crun", TaskStatusInProgress, "")
	before := cloneRowCount(t)

	_, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{TaskID: "t-crun"})
	if err == nil {
		t.Fatal("cloning an in_progress row must be refused — its ground has an owner")
	}
	if !strings.Contains(err.Error(), "may be RUNNING") {
		t.Errorf("the refusal must say WHY (the row may be running):\n%s", err.Error())
	}
	if after := cloneRowCount(t); after != before {
		t.Errorf("a refused clone wrote a row: %d → %d", before, after)
	}
}

// TestTaskCloneRefusedOnAnUnrunRow: `pending` means nothing ran, so nothing can
// be distrusted — the gate names the remedy (run it) instead of copying it.
func TestTaskCloneRefusedOnAnUnrunRow(t *testing.T) {
	newVerifyTestDB(t)
	cloneSeedTerminal(t, "t-cpend", TaskStatusPending, "")
	before := cloneRowCount(t)

	_, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{TaskID: "t-cpend"})
	if err == nil {
		t.Fatal("cloning a pending row must be refused — its ground is unused, not dirty")
	}
	if !strings.Contains(err.Error(), "has not run yet") {
		t.Errorf("the refusal must say the row never ran:\n%s", err.Error())
	}
	if after := cloneRowCount(t); after != before {
		t.Errorf("a refused clone wrote a row: %d → %d", before, after)
	}
}

// TestTaskCloneRefusedOnAnAbandonedRowUnlessConfirmed: `cancelled` is a decision
// somebody made; it takes an explicit reversal, and the confirmation then works.
func TestTaskCloneRefusedOnAnAbandonedRowUnlessConfirmed(t *testing.T) {
	newVerifyTestDB(t)
	cloneSeedTerminal(t, "t-ccan", TaskStatusCancelled, "")
	before := cloneRowCount(t)

	_, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{TaskID: "t-ccan"})
	if err == nil {
		t.Fatal("cloning a cancelled row must be refused without --confirm-cancelled")
	}
	if !strings.Contains(err.Error(), "--confirm-cancelled") {
		t.Errorf("the refusal must name the flag that reverses it:\n%s", err.Error())
	}
	if after := cloneRowCount(t); after != before {
		t.Errorf("a refused clone wrote a row: %d → %d", before, after)
	}
	if _, err := TaskCloneWithOptions(context.Background(),
		TaskCloneOptions{TaskID: "t-ccan", ConfirmCancelled: true}); err != nil {
		t.Fatalf("--confirm-cancelled must admit the abandoned row: %v", err)
	}
	if _, ok := taskByIDMust(t, cloneMintedSuccessorOf(t, "t-ccan")); !ok {
		t.Fatal("the confirmed clone must exist as a row of its own")
	}
}

// TestTaskCloneDryRunWritesNothing: the plan is reported in full and the ledger
// is untouched.
func TestTaskCloneDryRunWritesNothing(t *testing.T) {
	newVerifyTestDB(t)
	t.Setenv("GOPI_WORKSPACE_ROOT", t.TempDir())
	cloneSeedTerminal(t, "t-cdry", TaskStatusCompleted, taskDeliverableRebuild(taskDeliverable{
		Repo: t.TempDir(),
	}))
	before := cloneRowCount(t)

	out, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{TaskID: "t-cdry", DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	// The dry run names the id it WOULD mint — which is now a fresh uuid, so the
	// assertion is its SHAPE plus the fact that the ledger gained nothing, not a
	// suffix the run cannot predict.
	dryID := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "new task id:") {
			fields := strings.Fields(line)
			dryID = fields[len(fields)-3]
		}
	}
	if dryID == "" {
		t.Fatalf("the dry run must name the id it WOULD mint:\n%s", out)
	}
	if !fullid.IsUUID(dryID) {
		t.Errorf("the dry run would mint %q, which is not a canonical uuid:\n%s", dryID, out)
	}
	if !strings.Contains(out, TaskCloneDryRunMarker) {
		t.Errorf("the dry run must announce itself:\n%s", out)
	}
	if after := cloneRowCount(t); after != before {
		t.Fatalf("--dry-run wrote a row: %d → %d", before, after)
	}
}

// TestTaskCloneRefusedWhenALiveCloneAlreadyCarriesTheWork: a second clone would
// dispatch the same work twice, so the gate points at the LIVE one.
func TestTaskCloneRefusedWhenALiveCloneAlreadyCarriesTheWork(t *testing.T) {
	newVerifyTestDB(t)
	cloneSeedTerminal(t, "t-clive", TaskStatusFailed, "")
	if _, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{TaskID: "t-clive"}); err != nil {
		t.Fatalf("the first clone must succeed: %v", err)
	}
	before := cloneRowCount(t)

	_, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{TaskID: "t-clive"})
	if err == nil {
		t.Fatal("a second clone must be refused while the first one is still live")
	}
	if !strings.Contains(err.Error(), cloneMintedSuccessorOf(t, "t-clive")) {
		t.Errorf("the refusal must NAME the live clone:\n%s", err.Error())
	}
	if after := cloneRowCount(t); after != before {
		t.Errorf("the refused second clone wrote a row: %d → %d", before, after)
	}
}

// TestTaskCloneSuccessorIDShape pins the LEGACY spelling the shape test must
// keep recognising: an ordinary child that merely names the source as its parent
// is NOT a clone.
//
// THE SHAPE IS NO LONGER MINTED (operator ruling 2026-11-05): a clone now takes
// a fresh uuid, so these cases describe ROWS ALREADY ON DISK. That is exactly
// why the predicate stays — an id is the ledger's primary key, the pre-ruling
// rows are not renamed, and their live-successor gate must keep finding them or
// the same work is dispatched twice.
func TestTaskCloneSuccessorIDShape(t *testing.T) {
	cases := []struct {
		candidate string
		want      bool
	}{
		{"t-x-c1", true},
		{"t-x-c2", true},
		// A canonical uuid is NEVER a clone successor: the mint stopped
		// producing suffixes, so a uuid is a row in its own right.
		{"01a1a891-c437-7abd-a1ad-7a6efb484f42", false},
		{"t-x-c01", false}, // leading zero: not the ordinal spelling
		{"t-x-c", false},
		{"t-x-r1", false}, // a reopen successor, not a clone
		{"t-x-child", false},
		{"t-xy-c1", false},
	}
	for _, c := range cases {
		if got := taskCloneIsSuccessorID("t-x", c.candidate); got != c.want {
			t.Errorf("taskCloneIsSuccessorID(%q) = %v, want %v", c.candidate, got, c.want)
		}
	}
}

// mustTaskByID / taskByIDMust read one row through the package's own reader (no
// hand-rolled SELECT), so the clone assertions read the ledger the product reads.
func mustTaskByID(t *testing.T, id string) *Task {
	t.Helper()
	row, ok := taskByIDMust(t, id)
	if !ok {
		t.Fatalf("task %s does not exist", id)
	}
	return row
}

func taskByIDMust(t *testing.T, id string) (*Task, bool) {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer closeDB()
	row, err := taskByID(context.Background(), db, id)
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return row, row != nil
}

// TestTaskCloneGateTableIsTheThreeAnswers pins the status gate itself, so a
// future edit to the predicate cannot silently widen the admitted set.
func TestTaskCloneGateTableIsTheThreeAnswers(t *testing.T) {
	admitted := map[string]bool{
		TaskStatusFailed:     true,
		TaskStatusCompleted:  true,
		TaskStatusVerified:   true,
		TaskStatusCancelled:  false, // needs the explicit confirmation
		TaskStatusPending:    false,
		TaskStatusInProgress: false,
		TaskStatusClaimed:    false,
	}
	for status, want := range admitted {
		ok, _, _ := taskCloneStatusAdmits(status, false)
		if ok != want {
			t.Errorf("taskCloneStatusAdmits(%q) = %v, want %v", status, ok, want)
		}
	}
	if ok, _, _ := taskCloneStatusAdmits(TaskStatusCancelled, true); !ok {
		t.Error("--confirm-cancelled must admit a cancelled row")
	}
}

// TestLegacySuffixedCloneRowsStayAddressable is the MIGRATION half of the
// 2026-11-05 ruling.
//
// "Stop minting `<uuid>-c<N>`" is not "forget the rows that already are". An id
// is the ledger's primary key, so the pre-ruling rows are NOT renamed — which
// means every reader that used to recognise their shape still has to:
//
//	① the shape predicate still answers TRUE for them (they are clones, and the
//	   live-successor gate must keep finding them);
//	② `task show` / the ledger's own reader still resolves the id verbatim;
//	③ the live-successor gate REFUSES a second clone of the same source while a
//	   legacy clone is still live — the double-dispatch the gate exists to stop
//	   does not care which spelling the live row uses.
func TestLegacySuffixedCloneRowsStayAddressable(t *testing.T) {
	newVerifyTestDB(t)
	t.Setenv("GOPI_WORKSPACE_ROOT", t.TempDir())
	const src = "01a1a891-0000-7000-8000-00000legacy"
	cloneSeedTerminal(t, src, TaskStatusFailed, "")

	// ① THE PREDICATE — a row in the pre-ruling shape is still read as a clone.
	if !taskCloneIsSuccessorID(src, src+"-c1") {
		t.Fatalf("the shape predicate stopped recognising %s-c1 — the pre-ruling rows on disk became unreadable", src)
	}
	// ② THE LEDGER READER — the id resolves verbatim, with no uuid-shape gate
	// in front of it (an id is a primary key, not a format).
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := insertTaskRow(ctx, db, taskCreateFields{
		TaskID: src + "-c1", Summary: "a clone minted before the ruling",
		Status: TaskStatusPending, ParentTaskID: src, Tags: "bug,rework",
		Payload: "the work", Source: "worker",
	}); err != nil {
		t.Fatalf("seed the legacy row: %v", err)
	}
	legacy, err := taskByID(ctx, db, src+"-c1")
	if err != nil {
		t.Fatalf("read the legacy row: %v", err)
	}
	if legacy == nil {
		t.Fatal("the legacy `<uuid>-c1` row is no longer addressable by its own id — the migration broke the primary key")
	}
	if legacy.ParentTaskID != src {
		t.Fatalf("the legacy row's parent edge = %q, want %q", legacy.ParentTaskID, src)
	}
	// ③ THE GATE — a live legacy clone still blocks a second clone.
	if got, _, err := taskCloneLiveSuccessorFind(ctx, db, src); err != nil {
		t.Fatalf("live-successor scan: %v", err)
	} else if got != src+"-c1" {
		t.Fatalf("the live-successor gate does not see the legacy clone (got %q) — a second clone would dispatch the same work twice", got)
	}
	if _, err := TaskCloneWithOptions(ctx, TaskCloneOptions{TaskID: src}); err == nil {
		t.Fatal("a second clone must be refused while the legacy clone is live")
	}
}
