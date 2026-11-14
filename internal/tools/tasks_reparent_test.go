package tools

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TaskReparent (Rule 33's missing write path: the EDGE becomes movable).
//
// Before it, parent_task_id could be set ONLY at INSERT — TaskNew is its sole
// writer and the generic UPDATE set-list carries no parent column — so a wrong
// edge was permanent and the only remedy was `cancel and re-submit`, which
// mints a new id and breaks every reference to the old one.
//
// These tests drive the REAL entry points: TaskReparent for the write, and
// TaskList / TaskNextFiltered (the same surfaces TestParentGate pins) for the
// effect, so "the edge moved" means the GATE now reads the new edge — not that
// a column changed. Rule 32: the ledger comes from the scaffold
// (testutil.NewTestDBPath), never a hand-rolled path.

// reparentDB pins a fresh per-test ledger (Rule 32; the same seam TestParentGate
// uses — an operator-profile write is refused by the scaffold).
func reparentDB(t *testing.T) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
}

// reparentRow submits one row with an EXPLICIT id and an optional parent, so
// every assertion below can name the rows it is talking about. Source=auto
// keeps the row out of any human-facing default view.
func reparentRow(t *testing.T, id, summary, parent string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		TaskID:       id,
		Summary:      summary,
		Payload:      "briefing for " + summary,
		ParentTaskID: parent,
		Source:       "auto",
	}); err != nil {
		t.Fatalf("submit %s: %v", id, err)
	}
}

// reparentState reads the row's parent edge, status and updated_at straight
// from the ledger — the facts a refused move must leave untouched.
func reparentState(t *testing.T, id string) (parent, status string, updatedAt float64, events int) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var parentNull *string
	if err := db.QueryRowContext(ctx,
		`SELECT parent_task_id, status, updated_at FROM tasks WHERE task_id = ?`, id).
		Scan(&parentNull, &status, &updatedAt); err != nil {
		t.Fatalf("read row %s: %v", id, err)
	}
	if parentNull != nil {
		parent = *parentNull
	}
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_events WHERE task_id = ?`, id).Scan(&events); err != nil {
		t.Fatalf("count events %s: %v", id, err)
	}
	return parent, status, updatedAt, events
}

// reparentTrail reads the newest trail entry for a row (the move's audit half).
func reparentTrail(t *testing.T, id string) (event, detail string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if err := db.QueryRowContext(ctx,
		`SELECT event, detail FROM task_events WHERE task_id = ? ORDER BY id DESC LIMIT 1`, id).
		Scan(&event, &detail); err != nil {
		t.Fatalf("read trail %s: %v", id, err)
	}
	return event, detail
}

// TestTaskReparentMovesTheEdgeAndTheGateFollows is the happy path plus the
// point of the whole feature: after the move, the row the GATE reads is the
// new parent. The chain is b←a and c←b (b depends on a, c depends on b); c is
// re-pointed onto a, which SHORTENS the chain — a legitimate move, not a ring.
func TestTaskReparentMovesTheEdgeAndTheGateFollows(t *testing.T) {
	reparentDB(t)
	reparentRow(t, "rp-a", "producer a", "")
	reparentRow(t, "rp-b", "producer b", "rp-a")
	reparentRow(t, "rp-c", "consumer c", "rp-b")

	// Parked to begin with: b is pending, so c parks and says why.
	out, err := TaskList(taskStatusPending)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "parent rp-b not released") {
		t.Fatalf("c must be parked by rp-b before the move:\n%s", out)
	}

	moved, err := TaskReparent("rp-c", "rp-a", "b is not the producer any more")
	if err != nil {
		t.Fatalf("reparent: %v", err)
	}
	if !strings.Contains(moved, "rp-b -> rp-a") {
		t.Errorf("the move must name both ends, got: %q", moved)
	}
	if parent, _, _, _ := reparentState(t, "rp-c"); parent != "rp-a" {
		t.Errorf("the edge must be rp-a afterwards, got %q", parent)
	}
	// The gate's own reading surface now names rp-a, never rp-b.
	after, err := TaskList(taskStatusPending)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if strings.Contains(after, "parent rp-b not released") {
		t.Errorf("the gate must no longer read the OLD edge:\n%s", after)
	}
	if !strings.Contains(after, "parent rp-a not released") {
		t.Errorf("the gate must read the NEW edge:\n%s", after)
	}
	// The audit half: an append-only entry carrying both ends and the reason.
	event, detail := reparentTrail(t, "rp-c")
	if event != ReparentEventName {
		t.Errorf("trail event = %q, want %q", event, ReparentEventName)
	}
	for _, want := range []string{"b is not the producer any more", "old parent_task_id: rp-b", "new parent_task_id: rp-a"} {
		if !strings.Contains(detail, want) {
			t.Errorf("trail detail must contain %q:\n%s", want, detail)
		}
	}
	// And the EDGE is the only thing that moved: no status transition was
	// consumed (the row is still pending, exactly as it was).
	if _, status, _, _ := reparentState(t, "rp-c"); status != taskStatusPending {
		t.Errorf("status must be untouched by a reparent, got %q", status)
	}
}

// TestTaskReparentClearsTheEdge: --clear-parent's engine arm. A cleared edge is
// a parentless row — every dispatcher treats it exactly like a row born
// without one, which is what makes this a REPAIR of a wrong edge rather than a
// bypass of the gate (the old edge is recorded in the trail, never erased).
func TestTaskReparentClearsTheEdge(t *testing.T) {
	reparentDB(t)
	reparentRow(t, "rp-p", "parent", "")
	reparentRow(t, "rp-k", "child", "rp-p")

	out, err := TaskReparent("rp-k", "", "the dependency was a mistake")
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if !strings.Contains(out, "rp-p -> (none)") {
		t.Errorf("the cleared arm must render both ends, got: %q", out)
	}
	if parent, _, _, _ := reparentState(t, "rp-k"); parent != "" {
		t.Errorf("the edge must be gone, got %q", parent)
	}
	if _, detail := reparentTrail(t, "rp-k"); !strings.Contains(detail, "new parent_task_id: (none)") {
		t.Errorf("the trail must record the removal by name:\n%s", detail)
	}
}

// TestTaskReparentRefusesCycles is the RED→GREEN proof of the cycle check. RED
// half: before the check existed, `TaskReparent(a, c)` on the chain c→b→a wrote
// an edge that makes the graph a ring, and every row on that ring parked
// forever (the gate reads a parent's status; a ring has no releaseable member).
// GREEN half: it is refused by name, WITH the path, and NOTHING is written.
//
// The second half of the test is the positive control that keeps the check
// honest: re-pointing c at a (an ANCESTOR of c, which shortens the chain) must
// still succeed. A check that refused every move "up" the tree would pass the
// first assertion and fail this one.
func TestTaskReparentRefusesCycles(t *testing.T) {
	reparentDB(t)
	reparentRow(t, "rp-a", "root", "")
	reparentRow(t, "rp-b", "mid", "rp-a")
	reparentRow(t, "rp-c", "leaf", "rp-b")

	beforeParent, beforeStatus, beforeUpdated, beforeEvents := reparentState(t, "rp-a")

	// Root → leaf closes the ring a ← c ← b ← a.
	_, err := TaskReparent("rp-a", "rp-c", "this would close a ring")
	if err == nil {
		t.Fatalf("reparenting the root onto its own descendant must be refused")
	}
	msg := err.Error()
	for _, want := range []string{"cycle", "rp-c -> rp-b -> rp-a"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal must contain %q, got: %s", want, msg)
		}
	}
	// ZERO WRITES: the edge, the status, the version stamp and the trail are
	// exactly as they were.
	afterParent, afterStatus, afterUpdated, afterEvents := reparentState(t, "rp-a")
	if afterParent != beforeParent || afterStatus != beforeStatus || afterUpdated != beforeUpdated || afterEvents != beforeEvents {
		t.Errorf("a refused cycle move must write NOTHING:\nbefore parent=%q status=%q updated=%v events=%d\nafter  parent=%q status=%q updated=%v events=%d",
			beforeParent, beforeStatus, beforeUpdated, beforeEvents, afterParent, afterStatus, afterUpdated, afterEvents)
	}
	// The DENIED row is what matters: the ring would have parked rp-a itself.
	if out, _ := TaskList(taskStatusPending); strings.Contains(out, "parent rp-c not released") { //stylegate:ignore lists pending rows to look for the parked child's marker; an error yields no marker and the assertion fails
		t.Errorf("the refused move must not have touched the row:\n%s", out)
	}

	// POSITIVE CONTROL: the same target set, an ancestor instead of a
	// descendant — allowed, so the check is a cycle check and not a blanket
	// "no upward moves" rule.
	if _, err := TaskReparent("rp-c", "rp-a", "shorten the chain"); err != nil {
		t.Fatalf("a non-cyclic move onto an ancestor must be allowed: %v", err)
	}
	if parent, _, _, _ := reparentState(t, "rp-c"); parent != "rp-a" {
		t.Errorf("positive control: edge = %q, want rp-a", parent)
	}
}

// TestTaskReparentSelfReferenceIsRefused: the one-node ring, named separately
// because the walk would report it as "the target already depends on this row"
// — true, and less legible than "you made the row its own parent".
func TestTaskReparentSelfReferenceIsRefused(t *testing.T) {
	reparentDB(t)
	reparentRow(t, "rp-self", "row", "")
	_, _, beforeUpdated, beforeEvents := reparentState(t, "rp-self")

	_, err := TaskReparent("rp-self", "rp-self", "oops")
	if err == nil || !strings.Contains(err.Error(), "its own parent") {
		t.Fatalf("a self-reference must be refused by name, got: %v", err)
	}
	_, _, afterUpdated, afterEvents := reparentState(t, "rp-self")
	if afterUpdated != beforeUpdated || afterEvents != beforeEvents {
		t.Errorf("a refused self-reference must write nothing (updated %v→%v, events %d→%d)",
			beforeUpdated, afterUpdated, beforeEvents, afterEvents)
	}
}

// TestTaskReparentGhostParentIsRefusedWithTheSubmitSentence: the edge's other
// refusal, spelled with the SAME sentence TaskSubmit uses (Rule 14) — an
// operator grepping for the submit-time refusal must find it through this door
// too, and a ghost edge is the same unnavigable link whichever door created it.
func TestTaskReparentGhostParentIsRefusedWithTheSubmitSentence(t *testing.T) {
	reparentDB(t)
	reparentRow(t, "rp-real", "row", "")
	_, _, beforeUpdated, beforeEvents := reparentState(t, "rp-real")

	_, err := TaskReparent("rp-real", "rp-doesnotexist", "typo")
	if err == nil {
		t.Fatalf("a ghost parent must be refused")
	}
	want := `parent_task_id "rp-doesnotexist" does not exist`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("the refusal must reuse the submit sentence %q, got: %s", want, err.Error())
	}
	// ...and the submit face agrees, measured on the SAME ledger: one
	// definition, two doors.
	if _, serr := TaskSubmit(TaskSubmitOptions{
		Summary: "ghost child", Payload: "x", ParentTaskID: "rp-doesnotexist", Source: "auto",
	}); serr == nil || !strings.Contains(serr.Error(), want) {
		t.Errorf("submit and update must refuse a ghost parent identically; submit said: %v", serr)
	}
	_, _, afterUpdated, afterEvents := reparentState(t, "rp-real")
	if afterUpdated != beforeUpdated || afterEvents != beforeEvents {
		t.Errorf("a refused ghost edge must write nothing")
	}
}

// TestTaskReparentRequiresReason: an append-only audit ledger records WHY the
// edge moved; an unexplained rewrite of a row's production dependency is what
// the append-only rule exists to prevent. Same shape as TaskSetPayload's.
func TestTaskReparentRequiresReason(t *testing.T) {
	reparentDB(t)
	reparentRow(t, "rp-a", "a", "")
	reparentRow(t, "rp-b", "b", "")
	_, _, beforeUpdated, beforeEvents := reparentState(t, "rp-b")

	if _, err := TaskReparent("rp-b", "rp-a", "   "); err == nil || !strings.Contains(err.Error(), "must carry --reason") {
		t.Fatalf("a reparent without --reason must be refused by name, got: %v", err)
	}
	// The CLEARING arm carries the same requirement — it is the same door.
	if _, err := TaskReparent("rp-b", "", ""); err == nil || !strings.Contains(err.Error(), "must carry --reason") {
		t.Fatalf("clearing without --reason must be refused by name, got: %v", err)
	}
	_, _, afterUpdated, afterEvents := reparentState(t, "rp-b")
	if afterUpdated != beforeUpdated || afterEvents != beforeEvents {
		t.Errorf("a refused reason-less move must write nothing")
	}
}

// TestTaskReparentMissingRowIsRefused: this ledger has NO delete path and this
// is not one either — a ghost SUBJECT is a typo, and it must leave no trace.
func TestTaskReparentMissingRowIsRefused(t *testing.T) {
	reparentDB(t)
	reparentRow(t, "rp-a", "a", "")
	if _, err := TaskReparent("rp-ghost", "rp-a", "why not"); err == nil ||
		!strings.Contains(err.Error(), `no task with task_id="rp-ghost"`) {
		t.Fatalf("a ghost subject must be refused by name, got: %v", err)
	}
	if _, err := TaskReparent("", "rp-a", "why not"); err == nil ||
		!strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("an empty row id must be refused by name, got: %v", err)
	}
}

// TestTaskReparentIsIdempotent: re-sending the edge a row already carries is
// not a move — no trail entry, no version bump, and the caller is told so
// rather than shown a diff-shaped success.
func TestTaskReparentIsIdempotent(t *testing.T) {
	reparentDB(t)
	reparentRow(t, "rp-a", "a", "")
	reparentRow(t, "rp-b", "b", "rp-a")
	parent, _, updated, events := reparentState(t, "rp-b")

	out, err := TaskReparent("rp-b", "rp-a", "again")
	if err != nil {
		t.Fatalf("idempotent move: %v", err)
	}
	if !strings.Contains(out, "unchanged") {
		t.Errorf("an unchanged edge must say so, got: %q", out)
	}
	p2, _, u2, e2 := reparentState(t, "rp-b")
	if p2 != parent || u2 != updated || e2 != events {
		t.Errorf("an unchanged edge must write nothing (parent %q→%q, updated %v→%v, events %d→%d)",
			parent, p2, updated, u2, events, e2)
	}
	// The cleared-twice arm is the same no-op — measured against rp-a's OWN
	// pre-state. `updated` above belongs to rp-b, and a row's timestamp is
	// stamped as float64(time.Now().Unix()) — a WHOLE second — so two rows
	// submitted in adjacent seconds make every CROSS-ROW timestamp comparison
	// flaky by construction: MEASURED 1 red in 200 bare runs, with no load
	// anywhere (the gap between this test's two adjacent submits reached
	// 34.6ms). The assertion compared one row's clock against another row's.
	// The Rule 41 row that filed this is tasks/01a12be5.
	_, _, updatedA, _ := reparentState(t, "rp-a")
	if _, err := TaskReparent("rp-a", "", "clear a row that has no parent"); err != nil {
		t.Fatalf("clearing an already-parentless row: %v", err)
	}
	if _, _, u3, e3 := reparentState(t, "rp-a"); u3 != updatedA || e3 != 0 {
		t.Errorf("clearing a parentless row must write nothing (updated %v, events %d)", u3, e3)
	}
}

// reparentSecondStraddle parks the caller until the wall clock has just crossed
// a whole-second boundary (plus a settle margin). TaskSubmit stamps
// created_at/updated_at as float64(time.Now().Unix()) — a WHOLE second — so a
// row inserted after this returns carries a stamp at least one second above a
// row inserted before it. That turns the boundary straddle from a 1-in-200
// scheduling accident into a property the fixture OWNS (Rule 27: no load is
// generated anywhere in this file — one bounded sleep).
func reparentSecondStraddle(t *testing.T) {
	t.Helper()
	const settle = 20 * time.Millisecond
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)) + settle) //stylegate:allow-sleep lines up with the second boundary of the wall clock (time.Now().Truncate) so the straddle the reparent case reproduces actually falls on a second boundary
}

// TestTaskReparentIdempotenceAcrossASecondBoundary is the regression guard for
// the Rule 41 row 01a12be5 finding: TestTaskReparentIsIdempotent flaked inside
// the branch-scoped package suite while passing in isolation, because its
// cleared-twice arm compared rp-a's updated_at against rp-b's `updated` — a
// DIFFERENT row's clock. Two rows submitted in adjacent seconds straddle the
// whole-second stamp under load and the comparison reddens; measured on the
// shipped assertion, 1 bare run in 200 (no load; max submit-pair gap 34.6ms).
//
// This test asserts the SAME physical property (clearing a row that already has
// no parent writes nothing) with the straddle PINNED IN PLACE, on the row's OWN
// pre-state — so it is deterministic in both directions: green on the fixed
// code, and red on EVERY run if the no-op path ever starts writing (the old
// assertion caught that only 1 run in 200).
func TestTaskReparentIdempotenceAcrossASecondBoundary(t *testing.T) {
	reparentDB(t)
	reparentRow(t, "rp-a", "a", "")
	reparentSecondStraddle(t)
	reparentRow(t, "rp-b", "b", "rp-a")

	_, _, ownUpdated, _ := reparentState(t, "rp-a")
	_, _, neighbourUpdated, _ := reparentState(t, "rp-b")
	if ownUpdated == neighbourUpdated {
		t.Fatalf("fixture: the two rows must straddle a whole second or this guard is vacuous (rp-a=%v rp-b=%v)",
			ownUpdated, neighbourUpdated)
	}

	if _, err := TaskReparent("rp-a", "", "clear a row that has no parent"); err != nil {
		t.Fatalf("clearing an already-parentless row: %v", err)
	}
	_, _, afterUpdated, afterEvents := reparentState(t, "rp-a")
	if afterUpdated != ownUpdated || afterEvents != 0 {
		t.Errorf("clearing a parentless row must write nothing (updated %v→%v, events %d; its neighbour rp-b is at %v)",
			ownUpdated, afterUpdated, afterEvents, neighbourUpdated)
	}
}

// TestTaskReparentKeepsTheFTSTriggerInStep: parent_task_id IS an indexed column
// (tasksFTSColumns), so the move is visible through `task search` with no
// second write — the trigger maintains the index, exactly as the payload
// revision's doc claims for its own column. This is the FTS consistency the
// brief asks to confirm, measured through the operator's own search surface.
func TestTaskReparentKeepsTheFTSTriggerInStep(t *testing.T) {
	reparentDB(t)
	reparentRow(t, "rp-newparent", "the new producer", "")
	reparentRow(t, "rp-row", "the consumer", "")

	// Before: the row carries no edge, so searching the (unrelated) parent id
	// must not reach it through the FTS tiers.
	before, err := TaskSearch("rp-newparent")
	if err != nil {
		t.Fatalf("search before: %v", err)
	}
	if strings.Contains(before, "the consumer") {
		t.Fatalf("the consumer row must not match the parent id before the move:\n%s", before)
	}

	if _, err := TaskReparent("rp-row", "rp-newparent", "link for the FTS check"); err != nil {
		t.Fatalf("reparent: %v", err)
	}
	after, err := TaskSearch("rp-newparent")
	if err != nil {
		t.Fatalf("search after: %v", err)
	}
	if !strings.Contains(after, "the consumer") {
		t.Errorf("the FTS index must follow the moved edge (the new parent id must match the child row):\n%s", after)
	}

	// ...and clearing it takes the row back OUT of that match — the same
	// trigger, the other direction.
	if _, err := TaskReparent("rp-row", "", "unlink again"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	cleared, err := TaskSearch("rp-newparent")
	if err != nil {
		t.Fatalf("search after clear: %v", err)
	}
	if strings.Contains(cleared, "the consumer") {
		t.Errorf("the FTS index must drop the cleared edge:\n%s", cleared)
	}
}

// TestTaskReparentCJKReasonSurvives: the reason rides argv (and an @file) as
// UTF-8; the trail entry must carry it byte-exact, and the move must not be a
// byte-counting casualty.
func TestTaskReparentCJKReasonSurvives(t *testing.T) {
	reparentDB(t)
	reparentRow(t, "rp-a", "生产者", "")
	reparentRow(t, "rp-b", "消费者", "")

	const reason = "原来挂错了：上游是生产者，不是这个行 —— 重新指向。"
	if _, err := TaskReparent("rp-b", "rp-a", reason); err != nil {
		t.Fatalf("reparent with a CJK reason: %v", err)
	}
	if _, detail := reparentTrail(t, "rp-b"); !strings.Contains(detail, reason) {
		t.Errorf("the CJK reason must survive byte-exact into the trail:\n%s", detail)
	}
}

// TestTaskReparentConcurrentMoversKeepTheLedgerConsistent is the -race half: N
// movers contend for ONE row's edge. Two facts are asserted, and both are
// claims about the LEDGER rather than about which goroutine won:
//
//  1. Every successful move left exactly one trail entry, so the audit trail
//     and the column agree — a move that landed without its trail row (or the
//     reverse) is the silent-divergence shape Rule 15 forbids.
//  2. The final edge is exactly one of the targets, and the ledger's trail
//     count equals the number of successful calls. No torn state, no lost
//     update, no error of any other kind.
//
// WHAT THIS DOES NOT CLAIM: that the pinned CAS's RowsAffected==0 arm fired.
// It cannot fire through this door — openTasksDB opens with _txlock=immediate,
// so the transaction takes the write lock at BEGIN and the second mover queues
// on busy_timeout instead of interleaving with the first. The CAS is the
// STATEMENT-level guarantee (it stays correct if the DSN changes or a foreign
// writer that does not take the immediate lock touches the row), and the
// serialization that makes it a backstop is what this test exercises. See
// devdoc/task-reparent.md, "the CAS: what is measured and what is argued".
func TestTaskReparentConcurrentMoversKeepTheLedgerConsistent(t *testing.T) {
	reparentDB(t)
	targets := []string{"rp-t1", "rp-t2", "rp-t3", "rp-t4"}
	for _, id := range targets {
		reparentRow(t, id, "target "+id, "")
	}
	reparentRow(t, "rp-shared", "the contended row", "")

	var wg sync.WaitGroup
	errs := make([]error, len(targets))
	start := make(chan struct{})
	for i, target := range targets {
		wg.Add(1)
		go func(i int, target string) {
			defer wg.Done()
			<-start // release them together: the contention is the point
			_, errs[i] = TaskReparent("rp-shared", target, "concurrent mover")
		}(i, target)
	}
	close(start)
	wg.Wait()

	successes := 0
	for i, err := range errs {
		if err == nil {
			successes++
			continue
		}
		// The one refusal this door can legitimately produce under contention
		// is the CAS arm — named, never a generic wrap.
		if !strings.Contains(err.Error(), "moved before this update landed") {
			t.Fatalf("target %s: unexpected failure: %v", targets[i], err)
		}
	}
	if successes == 0 {
		t.Fatalf("no mover succeeded — the contention test proves nothing")
	}
	parent, _, _, events := reparentState(t, "rp-shared")
	matched := false
	for _, target := range targets {
		if parent == target {
			matched = true
		}
	}
	if !matched {
		t.Errorf("the final edge must be one of the targets, got %q", parent)
	}
	if events != successes {
		t.Errorf("the trail and the column must agree: %d successful moves, %d trail entries", successes, events)
	}
}
