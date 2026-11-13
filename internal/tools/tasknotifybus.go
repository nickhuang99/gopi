package tools

// Task-notify transition BUS — the ONE shared funnel for operator-relevant
// status transitions (epic task-notify-terminal-gap, 2026-09-12).
//
// Before this file the notify hook was wired at exactly TWO call sites
// (taskUpdate's post-commit hook and TaskNextFiltered's CAS claim) while
// every other writer of tasks.status moved the ledger row silently, and the
// zombie "stale" alarm hand-rolled a second delivery path with its own copy
// of the config/events/pattern gates. The bus makes the NOTIFY PATH
// callable from any writer under one contract:
//
//   - taskUpdate / the CAS claim / the post-commit sync writers record the
//     transition into a bus and flush it at once — the classic live case,
//     behaviour unchanged.
//   - buffered callers may observe several transitions (a batch import) or
//     defer the flush past a transaction commit, so a rolled-back write
//     never announces a status the row does not have.
//   - an event whose payload carries fields the row alone cannot supply
//     (the zombie evidence chain on the "stale" alarm) rides the same path
//     through observeEvent's payload decorator instead of forking a second
//     delivery implementation with its own gate copy.
//
// Delivery failures are logged and NEVER fatal (the side-channel contract:
// the ledger transition is already committed when the bus flushes).

import (
	"context"
	"strings"
)

// taskNotifyPayloadDecorator enriches a payload at delivery time — the hook
// for event-specific fields (the zombie evidence chain) that a row-only
// payload cannot carry. Nil = the plain transition payload.
type taskNotifyPayloadDecorator func(*taskNotifyPayload)

// taskNotifyTransition is ONE buffered notification: the row state BEFORE
// the write (prev — the payload's previous_status source) plus the row
// state AFTER it (cur — the payload's field source and the status the event
// announces), optionally decorated.
type taskNotifyTransition struct {
	prev     *Task
	cur      *Task
	decorate taskNotifyPayloadDecorator
}

// taskNotifyRowMove is one row-addressed status move buffered by a BATCH
// writer (the sync import paths) that knows the task_id but not a cheap
// in-memory row image. The post-write state is re-read from the ledger at
// flush time — which is also the strongest guarantee: the payload describes
// what the COMMITTED row actually says.
type taskNotifyRowMove struct {
	taskID     string
	prevStatus string
	status     string
}

// taskNotifyBus buffers one logical write's notifications.
type taskNotifyBus struct {
	items []taskNotifyTransition
	moves []taskNotifyRowMove
}

// observeIDs buffers a row-addressed move (see taskNotifyRowMove). Nil-safe
// and idempotent on a non-move (same status / empty id) like observe.
func (b *taskNotifyBus) observeIDs(taskID, prevStatus, status string) *taskNotifyBus {
	if b == nil || taskIDNormalize(taskID) == "" {
		return b
	}
	if strings.TrimSpace(prevStatus) == strings.TrimSpace(status) {
		return b
	}
	b.moves = append(b.moves, taskNotifyRowMove{taskID: taskID, prevStatus: prevStatus, status: status})
	return b
}

// observe records a status transition. Defensive by construction: a nil side
// or an unchanged status is not a transition and buffers nothing (the
// pre-bus call sites re-read the row and let notifyTaskTransition drop
// same-status writes; the bus drops them one step earlier, same outcome).
// Nil-safe so a caller can chain `bus.observe(...)` on a nil bus.
func (b *taskNotifyBus) observe(prev, cur *Task) *taskNotifyBus {
	return b.observeEvent(prev, cur, nil)
}

// observeEvent records a transition plus an optional payload decorator — the
// single entry point (observe is the undecorated case).
func (b *taskNotifyBus) observeEvent(prev, cur *Task, decorate taskNotifyPayloadDecorator) *taskNotifyBus {
	if b == nil || prev == nil || cur == nil {
		return b
	}
	if strings.TrimSpace(prev.Status) == strings.TrimSpace(cur.Status) {
		return b
	}
	b.items = append(b.items, taskNotifyTransition{prev: prev, cur: cur, decorate: decorate})
	return b
}

// flush delivers every buffered notification through notifyTaskTransition —
// the ONE shared notify path (config resolve, events allow-list, pattern
// gate, delivery, per-event decorator). Never returns an error and never
// panics on a delivery problem: the ledger write already stands.
func (b *taskNotifyBus) flush() {
	if b == nil {
		return
	}
	items := b.items
	b.items = nil
	moves := b.moves
	b.moves = nil
	for _, it := range items {
		notifyTaskTransitionDecorate(it.prev.Status, it.cur, it.decorate)
	}
	for _, mv := range moves {
		cur, err := taskNotifyReread(mv.taskID)
		if err != nil {
			taskNotifyLog("post-write re-read of %s: %v", mv.taskID, err)
			continue
		}
		if cur == nil {
			taskNotifyLog("post-write re-read of %s: row vanished — no notification", mv.taskID)
			continue
		}
		notifyTaskTransition(mv.prevStatus, cur)
	}
}

// taskNotifyFlushDeferred schedules a bus to flush once the caller's
// logical operation has finished — used by the transactional sync writers,
// whose status writes are only real after a commit. The returned function
// is idempotent (the bus empties itself on the first call).
func taskNotifyFlushDeferred(b *taskNotifyBus) func() {
	return func() { b.flush() }
}

// taskNotifyFlush delivers a bus immediately — the shape for a writer whose
// ledger write is already committed (taskUpdate, the CAS claim, the
// post-commit sync writers). The name is the SINGLE seam through which every
// non-transactional writer reaches the notify path, so a future
// deferred-flush protocol changes exactly one place.
func taskNotifyFlush(b *taskNotifyBus) {
	b.flush()
}

// taskNotifyPrevCopy snapshots a row with the status it held BEFORE a write
// (the authoritative payload previous_status). The rest of the row is
// carried as-is — a status move does not rewrite a row's summary/tags, so
// the copy is a faithful predecessor for the payload.
func taskNotifyPrevCopy(row *Task, prevStatus string) *Task {
	cp := *row
	cp.Status = prevStatus
	return &cp
}

// taskNotifyCopyWithStatus snapshots a row with the status it landed on —
// the payload source for a writer that knows the landed status statically
// (the sync writers set the column directly, so no re-read is warranted).
func taskNotifyCopyWithStatus(row *Task, landedStatus string) *Task {
	cp := *row
	cp.Status = landedStatus
	return &cp
}

// taskNotifyFlushStatus notifies ONE statically-known status move whose
// payload fields come from `row` (pre- or post-write — these writers do not
// rewrite summary/tags in the same move). A nil row or a same-status move
// announces nothing (the bus drops it). This is the ONE-liner face the
// direct-column writers (sync import / claim ladder / rollback) use so they
// cannot hand-roll a payload and drift from the shared shape.
func taskNotifyFlushStatus(row *Task, prevStatus, landedStatus string) {
	if row == nil {
		return
	}
	bus := taskNotifyBus{}
	bus.observe(taskNotifyPrevCopy(row, prevStatus), taskNotifyCopyWithStatus(row, landedStatus))
	taskNotifyFlush(&bus)
}

// taskNotifyReread loads one row's post-write state — the payload source for
// a buffered row-addressed move (and for any event whose landed status is
// only known after the write). Returns (nil, nil) when the row is gone.
func taskNotifyReread(taskID string) (*Task, error) {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return taskByID(ctx, db, taskID)
}

// taskNotifyFlushStatusMove notifies ONE committed status move addressed by
// task_id alone (the writer has no in-memory row image): the post-write row
// is re-read from the ledger at flush time, so the payload describes what
// the committed row actually says. This is the shape every non-taskUpdate
// writer that only knows a task_id uses.
func taskNotifyFlushStatusMove(taskID, prevStatus, landedStatus string) {
	bus := taskNotifyBus{}
	bus.observeIDs(taskID, prevStatus, landedStatus)
	taskNotifyFlush(&bus)
}
