package agent

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Hippocampus host — the scheduler side of the active-memory organ
// (epic: hippocampus, child 3/4; spec devdoc/hippocampus.md §4/§6).
//
// Division of labour (ontology §2.2): the ORGAN retrieves and reports raw
// objective signals (confidence, memory weight, a briefing text). The
// SCHEDULER (this file) owns everything about the host loop — when the organ
// is asked, which delivery level a brief earns, how often, and what lands in
// the context. The organ never decides a level; the host never invents a hit.
//
// The four levels, all four of them host-side:
//
//	Lv-0 archive       — a session-JSONL event, NO context mutation
//	Lv-1 append        — one user message at the next round boundary (memo)
//	Lv-2 meta event    — one user message at the round boundary, framed as a
//	                     re-evaluation demand (same physical channel as Lv-1 —
//	                     devdoc §4 "不要写两套注入机制"; deliberately NOT a new
//	                     promptSection: the system prompt is a static,
//	                     cache-stable list, §3)
//	Lv-3 soft interrupt— scheduler-adjudicated: the brief is injected with the
//	                     highest priority at the round boundary. It NEVER calls
//	                     Agent.Interrupt() and never cancels the run ctx
//	                     (devdoc §1.4: that is the user's hard stop, and it
//	                     throws away the round in flight). Over quota or under
//	                     the confidence floor it degrades to Lv-1.
//
// The organ is reachable only through the narrow hippoRecallFn below: its
// production implementation is internal/hippocampus's engine, bound to *Agent
// at the end of this file (the single wire-level contract, devdoc §6). Tests
// inject a stub, so the whole host runs network-free and deterministic, and
// the agent package needs no import of the organ package.

// hippoRecallFn is the scheduler's call into the organ. It mirrors
// internal/hippocampus.Engine.Run (same shape: ctx + request → result + err)
// so the production binding is a one-liner; keeping it a func type means the
// agent package never imports the hippocampus package.
type hippoRecallFn func(ctx context.Context, req hippocampusRecallRequest) (*hippocampusBrief, error)

// hippocampusRecallRequest is one ask. The organ never probes the agent's task
// state itself — the host hands the context in (ontology §2.2).
type hippocampusRecallRequest struct {
	// TaskContext is the host-owned material the retrieval fans out from.
	// Resolved by hippocampusTaskContext, never set by callers.
	TaskContext string
	// SessionID / TaskID are the provenance stamps the organ writes into any
	// record it distills.
	SessionID string
	TaskID    string
	// Round is the 1-based round the recall was launched for (0 = run start).
	Round int
}

// hippocampusBrief is the ONLY thing the organ may emit: raw objective
// signals plus a briefing text. It carries NO delivery level (the scheduler
// computes it) and no answer-like text (职责漂移防线).
type hippocampusBrief struct {
	// Confidence is the organ's self-assessed relevance (0..1).
	Confidence float64
	// MemoryWeight is the intrinsic weight of the memory the brief came from
	// (0..1). Both signals are recorded, never inferred by the host.
	MemoryWeight float64
	// Summary is the briefing body: what was retrieved, stated as material
	// for the model, not as an answer to the task.
	Summary string
	// Memories is the number of memory records the brief rests on — the
	// budget controller's tie-breaker (a weightless brief with hits is still
	// worth archiving).
	Memories int
	// Keys is the memory STATE the brief reports: one stable key per action
	// the cycle executed and per memory lead it rests on. It is the raw
	// material of the convergence gate — two briefs with the same keys
	// describe the same memory state, so delivering the second one tells the
	// model nothing it has not already been told (the noise the real-session
	// measurement exposed: 8 identical memos in 8 rounds). The organ reports
	// the keys; the host owns the gate.
	Keys []string
	// DurationMS is how long the retrieval took (Lv-0 audit only).
	DurationMS int
}

// hippocampusLevel is a delivery channel. Its numeric order IS the escalation
// order, so the level ceiling (GOPI_HIPPOCAMPUS_MAX_LV) is a plain compare.
type hippocampusLevel int

const (
	// hippocampusLv0Archive writes the brief to the session JSONL only.
	hippocampusLv0Archive hippocampusLevel = iota
	// hippocampusLv1Append adds a post-round reminder message.
	hippocampusLv1Append
	// hippocampusLv2MetaEvent adds an in-context meta event (re-evaluate).
	hippocampusLv2MetaEvent
	// hippocampusLv3SoftInterrupt is the scheduler-adjudicated soft interrupt.
	hippocampusLv3SoftInterrupt
)

// clampHippocampusLevel folds an arbitrary int onto the legal level range, so
// an out-of-range GOPI_HIPPOCAMPUS_MAX_LV can neither disable archiving nor
// unlock a level the scheduler does not implement.
func clampHippocampusLevel(l hippocampusLevel) hippocampusLevel {
	if l < hippocampusLv0Archive {
		return hippocampusLv0Archive
	}
	if l > hippocampusLv3SoftInterrupt {
		return hippocampusLv3SoftInterrupt
	}
	return l
}

// Text renders the level's audit token (session log + live debugging).
func (l hippocampusLevel) Text() string {
	switch l {
	case hippocampusLv1Append:
		return hippocampusLvTextAppend
	case hippocampusLv2MetaEvent:
		return hippocampusLvTextMetaEvent
	case hippocampusLv3SoftInterrupt:
		return hippocampusLvTextSoftInterrupt
	default:
		return hippocampusLvTextArchive
	}
}

// Framing constants. Both frames are logged BOTH ways (session JSONL + the
// live request) and the frame is the only difference between Lv-1 and Lv-2 —
// which is what makes "Lv-2 output arrived as a message, not a prompt
// section" checkable (Rule 11: the wire is auditable).
const (
	// hippocampusAppendFrame wraps a post-round memo (Lv-1).
	hippocampusAppendFrame = "【海马体备忘】"
	// hippocampusMetaEventFrame wraps an in-context meta event (Lv-2).
	hippocampusMetaEventFrame = "【海马体元事件】"
)

// Hippocampus message bodies. Each is spelled out rather than templated so a
// delivery test can assert one literal that exists exactly once in the
// package (Rule 13).
const (
	hippocampusAppendInstruction = "这是一条检索备忘：仅供参考，不必改变当前计划；若与本轮工作无关，直接忽略。"
	// hippocampusMetaEventInstruction mirrors the memory tool's own routing
	// directive ("CALL IT FIRST, BEFORE investigating") — it asks for a
	// re-evaluation of the plan, not for an answer.
	hippocampusMetaEventInstruction = "这是上下文内的元事件：在继续之前，先重新评估后续步骤（是否要改用其它路径，或先处理此线索）。"
	// hippocampusInterruptInstruction is the Lv-3 frame: the brief becomes the
	// primary context of the next round — the graceful approximation of
	// "terminate the current flow and open a new round with the brief first".
	hippocampusInterruptInstruction = "这是经调度器裁决的软中断简报：把它当作下一回合的首要上下文，先据此重新评估当前方向，再继续执行。"
)

// Audit tokens — the values written to the session log. One definition each:
// the literal gate test asserts no other production file of this package
// carries them (Rule 13).
const (
	hippocampusLvTextArchive       = "lv-0-archive"
	hippocampusLvTextAppend        = "lv-1-append"
	hippocampusLvTextMetaEvent     = "lv-2-meta-event"
	hippocampusLvTextSoftInterrupt = "lv-3-soft-interrupt"
	// hippocampusBriefEvent is the Lv-0 session-event type (N7). The two
	// existing JSONL consumers ignore it STRUCTURALLY: trajectory.go switches
	// on ev.Type with no matching branch (and no default), and
	// memory_brew.go's brewRenderEvent switches on message.role after a nil
	// Message check — this event carries no "message" key at all, so it is
	// skipped before the role switch is reached.
	hippocampusBriefEvent = "hippocampus_brief"
	// hippocampusLogKeyLevel / hippocampusLogKeySummary and the signal keys
	// below are the Lv-0 record's field names.
	hippocampusLogKeyLevel        = "level"
	hippocampusLogKeySummary      = "summary"
	hippocampusLogKeyConfidence   = "confidence"
	hippocampusLogKeyMemoryWeight = "memory_weight"
	hippocampusLogKeyMemories     = "memories"
	hippocampusLogKeyDurationMS   = "duration_ms"
	hippocampusLogKeyRound        = "round"
)

// hippocampusBriefRecord is one Lv-0 archive row (written by
// (*SessionLogger).LogHippocampusBrief). It mirrors the audit fields of the
// session event one-for-one.
type hippocampusBriefRecord struct {
	// Level is the scheduler's decision, rendered by (hippocampusLevel).Text.
	Level string
	// Summary is the briefing body (or the failure marker).
	Summary string
	// Confidence / MemoryWeight are the organ's raw signals.
	Confidence   float64
	MemoryWeight float64
	// Memories / DurationMS / Round complete the audit record.
	Memories   int
	DurationMS int
	Round      int
}

// hippocampusRecallFailureSummary is the archive body of a failed recall. It
// is an audit marker, never delivered as content: the organ degrades, it
// never lies.
const hippocampusRecallFailureSummary = "retrieval failed (see the organ's own log); archived for audit, not injected"

// ---------------------------------------------------------------------------
// Budget controller (devdoc §4 Lv-3 risk row + §7). It only adjudicates —
// Observe accounts after the fact.
// ---------------------------------------------------------------------------

// hippocampusBudget is the compute/attention budget of ONE run. It is touched
// only from the run's own goroutine (launch/adjudicate), never from the
// recall goroutine, so it needs no lock.
type hippocampusBudget struct {
	maxRecallPerRun  int
	lv3MinInterval   time.Duration
	lv3MaxPerSession int

	recalls int

	// lv3Armed is false until the first Lv-3 grant, so the quota window is
	// measured from the ACTUAL first interruption.
	lv3Armed bool
	lv3Last  time.Time
	lv3Count int
}

// newHippocampusBudget builds the controller from the resolved config.
func newHippocampusBudget(cfg hippocampusConfig) *hippocampusBudget {
	return &hippocampusBudget{
		maxRecallPerRun:  cfg.MaxRecallPerRun,
		lv3MinInterval:   cfg.Lv3MinInterval,
		lv3MaxPerSession: cfg.Lv3MaxPerSession,
	}
}

// allowRecall reports whether one more retrieval fits this run's budget.
func (b *hippocampusBudget) allowRecall() bool { return b.recalls < b.maxRecallPerRun }

// observeRecall records one launched recall.
func (b *hippocampusBudget) observeRecall() { b.recalls++ }

// allowLv3 reports whether a soft interrupt may fire now: the per-session cap
// and the minimum spacing must both hold.
func (b *hippocampusBudget) allowLv3(now time.Time) bool {
	if b.lv3Count >= b.lv3MaxPerSession {
		return false
	}
	if b.lv3Armed && now.Sub(b.lv3Last) < b.lv3MinInterval {
		return false
	}
	return true
}

// observeLv3 records one granted soft interrupt.
func (b *hippocampusBudget) observeLv3(now time.Time) {
	b.lv3Armed = true
	b.lv3Last = now
	b.lv3Count++
}

// hippoInjection is one wire-level message the loop adds + logs at the round
// boundary.
type hippoInjection struct {
	Level hippocampusLevel
	Text  string // already framed
}

// hippoRecallSlot is the channel between the recall goroutine and the run
// goroutine. The goroutine writes the brief EXACTLY ONCE and then closes done;
// the run goroutine reads the brief only after <-done returns, so the close
// provides the happens-before edge and the brief field is race-free without a
// lock. EVERYTHING ELSE about the in-flight recall (the slot itself, its
// cancel, the queue) lives in the scheduler and is guarded by hippoScheduler.mu
// — the goroutine touches those through the locked helpers below, never
// directly, because it can outlive the round that launched it.
type hippoRecallSlot struct {
	req  hippocampusRecallRequest
	done chan struct{}
}

// hippoScheduler is the run-scoped host: budget, delivery queue, in-flight
// recall, Lv-0 archive.
type hippoScheduler struct {
	cfg    hippocampusConfig
	recall hippoRecallFn
	budget *hippocampusBudget
	now    func() time.Time

	// base is the process-wide cancel root the run contexts derive from
	// (Agent.interruptRoot). Recalls launched from call sites that have no
	// run ctx (the Chat/ChatOps dispatch path) use it, so Agent.Interrupt()
	// ends them exactly like the run's own work.
	base context.Context
	// cancel aborts the in-flight recall. It derives from the RUN ctx, so
	// Agent.Interrupt()/ctx-cancel ends an in-flight hippocampal cycle through
	// the same channel as any other run work (devdoc §1.4).
	cancel context.CancelFunc
	// inflight is the recall currently running; nil when none.
	inflight *hippoRecallSlot
	// pending holds delivered-but-not-yet-injected briefs.
	pending []*hippocampusBrief

	// delivered is the round (or chat turn) the queue was last drained for.
	// It is part of the drain-once-per-round contract: the drain site runs at
	// the request-construction point, which IS the round boundary. -1 = the
	// nothing-delivered-yet sentinel (round 0 is the run start, so 0 cannot
	// serve as "never").
	delivered int

	// lastInjectedRound is the last round a memo was actually delivered into —
	// the reference point of the minimum-spacing gate.
	lastInjectedRound int
	// seenKeys is the run's set of memory-state keys already delivered (the
	// convergence gate) and sawBrief latches the first brief of the run, so
	// the FIRST brief is never suppressed by its own key.
	seenKeys map[string]bool
	sawBrief bool

	// failures counts failed recalls (a body-less brief) — they archive at
	// Lv-0, so they are tracked for the run's audit line, never delivered.
	failures int
	// maxLv is the effective ceiling. Call sites that have no round boundary
	// to deliver into (Chat/ChatOps) clamp it to Lv-1 — "不具备" becomes an
	// explicit rule instead of a runtime surprise (devdoc §4.1).
	maxLv hippocampusLevel

	// started latches EndOfRun's work (its archive must happen exactly once,
	// even when both the normal defer and the panic recover run).
	started bool
	ended   bool

	// turnToolRounded / turnStored are the CHAT path's per-TURN memory-store
	// checkpoint state (chat_ops.go's maybeChatOpsStoreCheckpoint): whether the
	// turn at hand really did tool work, and whether the checkpoint already
	// fired for it. They live HERE, on the gate's own object, rather than on
	// Agent, for two reasons: the state exists at all only when the gate is on
	// (a nil host is the gate-off guarantee), and an Agent field would move
	// every line of loop.go's Agent struct — a large diff in the one file the
	// devdoc corpus cites by line number (measured at the time: a 13-line
	// insert at :1852 turned 16 anchors in 10 devdocs onto blank lines). That
	// measurement was made by the devdoc line-anchor gate, which was
	// deleted with internal/docclass in 5a35f61 — nothing resolves a loop.go
	// line anchor now; churn, not a red gate. AND NOTHING REPLACES IT: the config-anchor census that was the executable anchor check (`internal/standards/testdata/config-anchor-census.go --check`, driven by the TestMergeConfigAnchorGate* arms in internal/tools) was deleted with the whole documentation-citation family in 78bdf05a (2026-11-05, operator ruling), so no executable anchor check runs over CONFIG.md's rows either.
	//
	// Lifetime: written and read from the run goroutine only (the ChatOps
	// loop) — never by the recall goroutine — so they are NOT under mu, like
	// budget/maxLv above.
	turnToolRounded bool
	turnStored      bool

	// sessionRound is the CHAT path's session-cumulative round counter. A
	// persistent chat shares ONE session log across many turns
	// (PersistChatSession), and a per-turn counter would restart at 1 on every
	// turn — leaving an archive trail whose round numbers repeat, so a reader
	// could not order it, which is the whole reason the stamp exists
	// (measured on the real path: per-turn numbering produced rounds
	// [1,2,3,1,1,2] for a three-turn session). Monotonic across the session is
	// the property that makes the trail readable, so the counter advances per
	// round and is never reset by a turn boundary.
	sessionRound int

	// mu guards EVERY field the recall goroutine can touch — inflight, cancel,
	// pending, delivered, recall — plus the started/ended latches: the recall goroutine
	// hands its result back while the run goroutine may already be draining,
	// and it may still be running when EndOfRun fires (a recall that outlives
	// its round is archived by the next drain or by EndOfRun). No call site
	// touches those fields directly — the locked accessors (setInflight/
	// takeInflight/stopInflight/enqueue/drainPending/deliver/beginEnd) are the
	// only door; the unlocked reads in AtRoundBoundary/EndOfRun, the unlocked
	// writes in launchRecall and the bare h.recall read inside the recall
	// goroutine were the data races fixed 2026-09-10.
	// budget/maxLv/seenKeys/sawBrief/lastInjectedRound are NOT under mu — only
	// ever touched from the run's own goroutine (noteState/adjudicate).
	mu sync.Mutex
}

// deliver records the drain bookkeeping. maxLv/budget/seenKeys/sawBrief/
// lastInjectedRound are NOT under mu (only the run goroutine touches them, by
// contract), but delivered IS read on the drain path while a recall goroutine
// may still be handing a brief back — so it lives with the other shared fields.
func (h *hippoScheduler) deliver(round int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.delivered = round
}

// recallFn returns the bound organ under mu. The recall goroutine reads the
// binding too (SetHippocampusRecall may rebind it after a run has started), so
// the field is only ever reached through this accessor.
func (h *hippoScheduler) recallFn() hippoRecallFn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.recall
}

// setRecall rebinds the organ (locked — see recallFn: the recall goroutine
// reads the binding from its own goroutine).
func (h *hippoScheduler) setRecall(fn hippoRecallFn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recall = fn
}

// setInflight installs one recall as the in-flight one, with its cancel.
// Locked: the recall goroutine clears the SAME fields when it finishes
// (enqueue), so a bare assignment here would be an unsynchronized write racing
// that clear — the exact DATA RACE the -race repro in this package carries
// (read of h.inflight in AtRoundBoundary vs write in enqueue).
func (h *hippoScheduler) setInflight(slot *hippoRecallSlot, cancel context.CancelFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.inflight, h.cancel = slot, cancel
}

// takeInflight detaches the in-flight slot so the caller can wait on it
// outside the lock (waiting under mu would serialize the organ's hand-off).
// The returned cancel is the slot's own, or nil when nothing was in flight.
func (h *hippoScheduler) takeInflight() (*hippoRecallSlot, context.CancelFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	slot, cancel := h.inflight, h.cancel
	h.inflight, h.cancel = nil, nil
	return slot, cancel
}

// stopInflight aborts the in-flight recall (if any) and drops the slot. Used
// when the run is ending: the ctx cancel is what makes an organ still working
// on a doomed cycle unwind promptly.
func (h *hippoScheduler) stopInflight() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancel != nil {
		h.cancel()
	}
	h.inflight, h.cancel = nil, nil
}

// ---------------------------------------------------------------------------
// Delivery gates (noise control — measured, not guessed)
// ---------------------------------------------------------------------------
//
// A real session (the child-4/4 adoption run) showed the naive rule delivering
// ONE MEMO PER ROUND: the same five leads, the same "best confidence" 1.00,
// eight rounds running — the model reads an identical briefing every round and
// each read costs context, tokens and attention. Two gates fix it, both
// host-side and both keyed on what the organ reports (the organ itself never
// decides a delivery):

// hippoNoRound is the "no round delivered yet" sentinel for the scheduler's
// round bookkeeping (round 0 IS the run start, so 0 cannot mean "never").
const hippoNoRound = -1

// converged reports whether this brief repeats the memory state the model has
// already been shown — in which case it is archived (Lv-0) and never
// re-delivered. A brief WITHOUT keys never converges (there is nothing to
// compare, so the model stays the judge). The run's FIRST brief settles the
// state (the caller latches that); later briefs must EXTEND it to earn a
// delivery, which is what stops a repeated briefing loop.
func converged(seen map[string]bool, b *hippocampusBrief) bool {
	if b == nil || len(b.Keys) == 0 {
		return false
	}
	for _, k := range b.Keys {
		if !seen[k] {
			return false
		}
	}
	return true
}

// noteState records the brief's keys as delivered. kept = the keys the model
// has now been told about (changed or not) — the run's state the next brief is
// compared against.
func (h *hippoScheduler) noteState(b *hippocampusBrief) {
	if h == nil || b == nil {
		return
	}
	if h.seenKeys == nil {
		h.seenKeys = make(map[string]bool, len(b.Keys))
	}
	for _, k := range b.Keys {
		h.seenKeys[k] = true
	}
	h.sawBrief = true
}

// mayInject applies the minimum-spacing rule: a second delivery must be at
// least hippocampusMinInjectIntervalRounds rounds after the last one. The
// rule is silent when the rounds are unknown (0 = a non-round caller, e.g. the
// chat path) — there the convergence gate is the only noise control.
func (h *hippoScheduler) mayInject(round int) bool {
	if round <= 0 || h.lastInjectedRound <= 0 {
		return true
	}
	return round-h.lastInjectedRound >= hippocampusMinInjectIntervalRounds
}

// beginEnd latches EndOfRun: exactly one caller does the end-of-run work,
// whether it arrives from the normal defer or the panic recover path.
func (h *hippoScheduler) beginEnd() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ended {
		return false
	}
	h.ended = true
	return true
}

// newHippocampusHost builds the enabled host. Returns nil when the gate is
// off — callers must treat nil as "no hippocampus at all" (no recall, no
// injection, no log event, byte-identical behaviour).
func newHippocampusHost(cfg hippocampusConfig, recall hippoRecallFn) *hippoScheduler {
	if !cfg.Enabled {
		return nil
	}
	return &hippoScheduler{
		cfg:       cfg,
		recall:    recall,
		budget:    newHippocampusBudget(cfg),
		now:       time.Now,
		base:      context.Background(),
		maxLv:     clampHippocampusLevel(cfg.MaxLv),
		delivered: hippoNoRound,
	}
}

// setBase points the scheduler at the process-wide interrupt root.
func (h *hippoScheduler) setBase(base context.Context) {
	if h == nil || base == nil {
		return
	}
	h.base = base
}

// baseContext is the cancel root to use when no run ctx is at hand.
func (h *hippoScheduler) baseContext() context.Context {
	if h == nil || h.base == nil {
		return context.Background()
	}
	return h.base
}

// clampMaxLv lowers the delivery ceiling for call sites that cannot honour a
// higher level (devdoc/hippocampus.md §4.1: a chat path has no task flow to
// soft-interrupt). It never RAISES the ceiling — the configured knob stays the
// upper bound.
func (h *hippoScheduler) clampMaxLv(l hippocampusLevel) {
	if h == nil || l >= h.maxLv {
		return
	}
	h.maxLv = clampHippocampusLevel(l)
}

// StartOfRun launches the run's first recall. The organ runs OFF the run's
// critical path: a slow or hung organ must not delay the first LLM round (its
// result is collected at the first round boundary by AtRoundBoundary).
func (h *hippoScheduler) StartOfRun(ctx context.Context, a *Agent) {
	if h == nil {
		return
	}
	if ctx == nil {
		ctx = h.baseContext()
	}
	h.started = true
	h.launchRecall(ctx, hippocampusRecallRequest{
		TaskContext: a.hippocampusTaskContext(),
		SessionID:   a.sessionIDForTasks(),
		TaskID:      a.autoTaskRowID,
		Round:       0,
	})
}

// AtRoundBoundary drains the finished in-flight recall (if any), computes each
// brief's delivery level and returns the messages the loop must add. The next
// recall is launched HERE, so retrieval overlaps the LLM round that follows
// the collection.
func (h *hippoScheduler) AtRoundBoundary(ctx context.Context, a *Agent) []hippoInjection {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = h.baseContext()
	}
	// The chat path has its own start condition, and it is NOT "the loop
	// remembers": gopi chat hands the SAME long-lived agent to every turn
	// (PersistChatSession) and never calls the task loop's start-of-run seam,
	// so without this the path's very first boundary finds an empty queue and
	// archives nothing at all — the round stamp below would then have nothing
	// to label (measured on the real path: a tool-enabled chat turn archived
	// ZERO hippocampus_brief events, because StartOfRun had never launched a
	// retrieval). Round 1 is the trigger, and the started latch keeps it to
	// one launch per agent — the task loop sets the same latch from
	// startOfRunFor, so the two can never both fire.
	if !h.started {
		a.startOfRunFor(ctx)
	}
	// Wait for the in-flight recall, bounded by its own timeout. The wait is
	// on the SLOT channel, not on a sleep, so a fast organ costs nothing.
	// The wait's wall-clock is the recall's measured duration — it lands in
	// every brief of this drain (previously the archived duration_ms was
	// always 0 because only the organ could have stamped it).
	// The slot is DETACHED under mu (takeInflight) and waited on outside it:
	// every access to the shared slot/cancel goes through the lock, because the
	// recall goroutine clears those same fields from its own goroutine.
	waitStart := h.now()
	slot, cancel := h.takeInflight()
	if slot != nil {
		timer := time.NewTimer(h.cfg.RecallTimeout)
		select {
		case <-slot.done:
		case <-timer.C:
		}
		timer.Stop()
		if cancel != nil {
			cancel()
		}
	}
	recallMS := int(h.now().Sub(waitStart).Milliseconds())

	briefs := h.drainPending()
	if len(briefs) == 0 {
		return nil
	}
	for _, b := range briefs {
		if b != nil && b.DurationMS == 0 {
			b.DurationMS = recallMS
		}
	}
	round := a.hippocampusRound
	h.deliver(round)

	// Recall again for the round about to be issued: the brief just consumed
	// is one round old, and a long task earns a fresh look. Budget-bounded.
	h.launchRecall(ctx, hippocampusRecallRequest{
		TaskContext: a.hippocampusTaskContext(),
		SessionID:   a.sessionIDForTasks(),
		TaskID:      a.autoTaskRowID,
		Round:       round,
	})

	out := make([]hippoInjection, 0, len(briefs))
	for _, b := range briefs {
		level := h.adjudicate(b, round)
		h.archive(a, b, level, round)
		if level == hippocampusLv0Archive {
			continue
		}
		out = append(out, hippoInjection{Level: level, Text: h.frame(b, level)})
	}
	return out
}

// EndOfRun waits (bounded), drains and archives. Called from the run's
// end-of-run defer so the last brief is never lost — it lands in the session
// log even though no round is left to show it to the model (devdoc §4: 本 run
// 已无下一轮时降级为 Lv-0).
func (h *hippoScheduler) EndOfRun(ctx context.Context, a *Agent) {
	if h == nil || !h.beginEnd() {
		return
	}
	// Bounded wait: a hung organ costs the run hippocampusEndOfRunWait, never
	// a hang. The recall's ctx derives from the run ctx too, so when the run
	// ctx is already cancelled (interrupt/timeout) this returns immediately
	// and the goroutine unwinds on its own.
	// Detach under mu, wait outside it (same contract as AtRoundBoundary).
	slot, cancel := h.takeInflight()
	if slot != nil {
		timer := time.NewTimer(hippocampusEndOfRunWait)
		select {
		case <-slot.done:
		case <-timer.C:
		}
		timer.Stop()
		if cancel != nil {
			cancel()
		}
	}
	// Whatever the drain did not detach is aborted here (locked).
	h.stopInflight()
	for _, b := range h.drainPending() {
		h.archive(a, b, hippocampusLv0Archive, a.hippocampusRound)
	}
}

// launchRecall starts one recall in a goroutine bound to a cancelable child of
// ctx. The goroutine's only channel back is the slot; it never touches the
// agent's context or task state, so it cannot race the loop.
func (h *hippoScheduler) launchRecall(ctx context.Context, req hippocampusRecallRequest) {
	if h == nil || !h.budget.allowRecall() {
		return
	}
	// Capture the organ ONCE (locked): the goroutine below must never read the
	// shared binding field — SetHippocampusRecall can rebind it meanwhile.
	organ := h.recallFn()
	if organ == nil {
		return
	}
	h.budget.observeRecall()

	recallCtx, cancel := context.WithTimeout(ctx, h.cfg.RecallTimeout)
	slot := &hippoRecallSlot{req: req, done: make(chan struct{})}
	h.setInflight(slot, cancel)

	go func() {
		defer close(slot.done)
		brief, err := organ(recallCtx, slot.req)
		if err != nil || brief == nil {
			// No silent failure (Rule 15): a failed cycle is recorded. The
			// symptom is a lv-0 archive naming the failure — and the organ's
			// own log carries the error detail.
			brief = &hippocampusBrief{Summary: hippocampusRecallFailureSummary}
		}
		// (the goroutine closes over `organ`, never over h.recall)
		// The brief travels to the queue (locked) and NOWHERE else: a second
		// unsynchronized copy on the slot would be a race of its own (the run
		// goroutine reads the slot while this goroutine writes it).
		h.enqueue(cancel, brief)
	}()
}

// enqueue hands one finished brief to the delivery queue and clears the
// in-flight slot. Locked (mu): the hand-off races the run goroutine's drain,
// and a recall that finishes after EndOfRun must still be archived by the
// caller that closed the session.
func (h *hippoScheduler) enqueue(cancel context.CancelFunc, b *hippocampusBrief) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cancel()
	h.cancel = nil
	h.inflight = nil
	h.pending = append(h.pending, b)
	if len(h.pending) > hippocampusDeliveryBacklogMax {
		// Overflow policy: drop the OLDEST brief. The backlog is a delivery
		// queue, not a memory — an unbounded one would let a burst of recalls
		// hold unbounded injections (and unbounded memory) for a run.
		h.pending = h.pending[len(h.pending)-hippocampusDeliveryBacklogMax:]
	}
}

// drainPending returns the queued briefs and empties the queue.
func (h *hippoScheduler) drainPending() []*hippocampusBrief {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.pending
	h.pending = nil
	return out
}

// adjudicate computes the delivery level of one brief — THE scheduler's call
// (the organ never sends a level). Rules, in order:
//
//	Lv-0 when the brief has no body, is below the append floor, or describes a
//	     memory state the model has already been shown (convergence gate).
//	Lv-3 only when the ceiling allows it, the confidence clears the floor AND
//	     the quota grants it; otherwise it DEGRADES to Lv-1 (never retries,
//	     never queues a second activation).
//	Lv-2 when the ceiling allows it and the confidence clears the meta-event
//	     floor.
//	Lv-1 is the floor for any brief with a body.
//
// Between two deliveries the minimum-spacing rule (hippocampusMinInject-
// IntervalRounds) applies to every level — a briefing loop that says the same
// thing every round is noise, however confident each single brief is.
func (h *hippoScheduler) adjudicate(b *hippocampusBrief, round int) hippocampusLevel {
	if b == nil || strings.TrimSpace(b.Summary) == "" {
		return hippocampusLv0Archive
	}
	// A failed recall archives its marker; it is never delivered as content.
	if b.Summary == hippocampusRecallFailureSummary {
		h.failures++
		return hippocampusLv0Archive
	}
	if b.Confidence < hippocampusAppendMinConfidence {
		return hippocampusLv0Archive
	}
	// Convergence gate (the measured-noise fix): what the model has already
	// been told is not delivered twice. The run's FIRST brief settles the
	// state; every later brief must change it to earn a delivery.
	if h.sawBrief && converged(h.seenKeys, b) {
		return hippocampusLv0Archive
	}
	// Spacing gate: even a genuinely new brief waits for the interval.
	if h.sawBrief && !h.mayInject(round) {
		return hippocampusLv0Archive
	}
	// Base level: the passive memo, clamped by the ceiling (a ceiling of Lv-0
	// means archive only — the level ceiling is never bypassed).
	lv := hippocampusLv0Archive
	if h.maxLv >= hippocampusLv1Append {
		lv = hippocampusLv1Append
	}
	if lv >= hippocampusLv1Append && b.Confidence >= hippocampusMetaEventMinConfidence &&
		h.maxLv >= hippocampusLv2MetaEvent {
		lv = hippocampusLv2MetaEvent
	}
	if b.Confidence >= hippocampusLv3MinConfidence && h.maxLv >= hippocampusLv3SoftInterrupt {
		if h.budget.allowLv3(h.now()) {
			h.budget.observeLv3(h.now())
			lv = hippocampusLv3SoftInterrupt
		}
		// Quota refused (the per-session cap or the spacing window): the brief
		// still lands, but ONE LEVEL DOWN — the rung it already earned above
		// (Lv-2 with an Lv-2 ceiling, else Lv-1). Deliberately NOT an error, NOT
		// a retry and NOT a queue: an over-quota request must never spawn
		// another activation (devdoc §7).
	}
	if lv > hippocampusLv0Archive {
		// The state the model has now been shown: the reference for the next
		// convergence comparison and for the spacing window.
		h.noteState(b)
		h.lastInjectedRound = round
	}
	return lv
}

// frame wraps a brief's body for the wire.
func (h *hippoScheduler) frame(b *hippocampusBrief, lv hippocampusLevel) string {
	var sb strings.Builder
	switch lv {
	case hippocampusLv3SoftInterrupt:
		sb.WriteString(hippocampusInterruptInstruction)
		sb.WriteString("\n")
	case hippocampusLv2MetaEvent:
		sb.WriteString(hippocampusMetaEventFrame)
		sb.WriteString(hippocampusMetaEventInstruction)
		sb.WriteString("\n")
	default:
		sb.WriteString(hippocampusAppendFrame)
		sb.WriteString(hippocampusAppendInstruction)
		sb.WriteString("\n")
	}
	sb.WriteString(b.Summary)
	return sb.String()
}

// archive writes the Lv-0 audit event. An injected brief is archived TOO (at
// its own level): the session JSONL is the audit trail of what the organ found
// and what the scheduler did with it — "线上有、日志无" is indistinguishable
// from never having happened (devdoc §4/§5).
func (h *hippoScheduler) archive(a *Agent, b *hippocampusBrief, lv hippocampusLevel, round int) {
	if a == nil || a.SessionLog == nil || b == nil {
		return
	}
	if err := a.SessionLog.LogHippocampusBrief(hippocampusBriefRecord{
		Level:        lv.Text(),
		Summary:      b.Summary,
		Confidence:   b.Confidence,
		MemoryWeight: b.MemoryWeight,
		Memories:     b.Memories,
		DurationMS:   b.DurationMS,
		Round:        round,
	}); err != nil && a.Logger != nil {
		// Recorded, never swallowed (Rule 15) — the audit of the audit.
		a.Logger.Printf("🧠 hippocampus brief archive failed: %v", err)
	}
	if a.Logger != nil {
		a.Logger.Printf("🧠 hippocampus brief archived (%s, confidence=%.2f, memories=%d)",
			lv.Text(), b.Confidence, b.Memories)
	}
}

// ---------------------------------------------------------------------------
// Agent-side hooks. Each is a NO-OP when the host is nil — that is the gate.
// ---------------------------------------------------------------------------

// startOfRunFor is the gate-aware task-start hook: with the hippocampus
// enabled AND an organ bound it SUPERSEDES the passive task-start search
// enforcement (devdoc §4.2, 禁止双注入) and runs the organ instead. With the
// gate off — or with the gate on but no organ bound (the standalone branch
// before the merge-time binding) — it is today's injection, unchanged: an
// enabled-but-unbound host must never silently drop the run's entry point.
func (a *Agent) startOfRunFor(ctx context.Context) {
	if a.Hippocampus != nil && a.Hippocampus.recallFn() != nil {
		a.Hippocampus.StartOfRun(ctx, a)
	} else {
		a.maybeInjectTaskStartSearch()
	}
	// SELF-RECORDING (第一期): the model-facing contract, injected once before
	// the first round. It rides OUTSIDE the hippocampus gate — the two are
	// independent (the gate is about which ORGAN performs recall, this is about
	// the run recording itself), and a note file that only exists on the
	// gate-off path would make the feature untestable against the gate's own
	// shapes.
	//
	// IT IS SCOPED TO THE RUN SHAPES THAT ARE TASKS (RP-1 regression, 2026-10-01).
	// A PLAIN PROMPT (`gopi "<text>"`) is not a task this harness dispatched, so
	// it does not get the contract — a.taskChildRowID == "" IS the plain-prompt
	// run (loop.go RunPrompt), the same predicate that already decides the EPIC
	// framing. The message used to be appended unconditionally, and because the
	// wire COALESCES consecutive user text messages
	// (internal/deepseek/claude_client.go:560: "Merge consecutive user text
	// messages — the API requires alternating roles") the contract did not
	// arrive as a second message: it became text block #2 of the prompt's OWN
	// message. The prompt was then no longer the first user message "as typed"
	// — the promise `gopi -h` makes — so TestRunPathPlainPromptIsVerbatim read a
	// prompt buried in 883 characters. The defect was invisible to the frame
	// builder in loop.go, which was already correct on this path.
	a.maybeInjectSelfRecordRule(a.plainPrompt)
}

// roundBoundaryInjections drains the host and ADDS the messages — the single
// place the round-boundary channel reaches the context, right beside the
// TakeJobNotices loop (loop.go). Every injected message is logged exactly like
// a user message (the audit invariant maybeInjectTaskStartSearch established).
func (a *Agent) roundBoundaryInjections(ctx context.Context) []hippoInjection {
	if a.Hippocampus == nil {
		return nil
	}
	injections := a.Hippocampus.AtRoundBoundary(ctx, a)
	for _, inj := range injections {
		a.Ctx.AddUserMessage(inj.Text)
		if a.SessionLog != nil {
			a.SessionLog.LogUser(newUUIDv4(), inj.Text)
		}
	}
	return injections
}

// endOfRunHippocampus flushes the organ's tail before the session log closes.
func (a *Agent) endOfRunHippocampus(ctx context.Context) {
	if a.Hippocampus == nil {
		return
	}
	a.Hippocampus.EndOfRun(ctx, a)
}

// hippocampusTaskContext assembles the retrieval seed from the host's OWN
// material: the run's first user message (the epic framing) and the harness's
// task-list view. The organ is handed the material — it never gathers it
// (ontology §2.2).
func (a *Agent) hippocampusTaskContext() string {
	var sb strings.Builder
	if a.Ctx != nil && len(a.Ctx.Messages) > 0 {
		if first := a.Ctx.Messages[0].Content; first != "" {
			sb.WriteString(first)
		}
	}
	if a.TaskState != nil {
		if list := a.TaskState.format(); list != "" {
			sb.WriteString("\n")
			sb.WriteString(list)
		}
	}
	return truncateHippocampusContext(sb.String())
}

// truncateHippocampusContext bounds the seed handed to the organ.
func truncateHippocampusContext(s string) string {
	if len(s) <= hippocampusQueryMaxChars {
		return s
	}
	return s[:hippocampusQueryMaxChars]
}

// ---------------------------------------------------------------------------
// Production binding (the merge-time seam, devdoc/hippocampus.md §6)
// ---------------------------------------------------------------------------

// HippocampusRecallFunc / HippocampusRecallRequest / HippocampusBrief are the
// exported aliases the organ's adapter needs, so the binding can live in
// another package without exporting the host internals.
type (
	// HippocampusRecallFunc is the organ's recall entry point as the host
	// sees it (mirrors (*hippocampus.Engine).Run once its signature matches).
	HippocampusRecallFunc = hippoRecallFn
	// HippocampusRecallRequest is one retrieval ask.
	HippocampusRecallRequest = hippocampusRecallRequest
	// HippocampusBrief is what the organ reports back.
	HippocampusBrief = hippocampusBrief
)

// SetHippocampusRecall installs the production organ on an agent built by
// NewAgent. It returns false when the gate is off (no host ⇒ nothing to bind,
// and the call is a deliberate no-op rather than an error).
//
// The caller supplies the adapter — the import direction stays one-way
// (internal/hippocampus → internal/tools; internal/agent imports neither of
// the other two), so the binding lives where the organ's package is visible:
//
//	fn := func(ctx context.Context, req agent.HippocampusRecallRequest) (*agent.HippocampusBrief, error) {
//	    res, err := engine.Run(ctx, hippocampus.Request{TaskContext: req.TaskContext,
//	        SessionID: req.SessionID, TaskID: req.TaskID, Round: req.Round})
//	    if res == nil {
//	        return nil, err
//	    }
//	    return &agent.HippocampusBrief{
//	        Confidence:   res.Confidence,
//	        MemoryWeight: res.MemoryWeight,
//	        Summary:      res.Summary,
//	        Memories:     len(res.Hits),
//	    }, err
//	}
//	ag.SetHippocampusRecall(fn)
func (a *Agent) SetHippocampusRecall(fn HippocampusRecallFunc) bool {
	if a == nil || a.Hippocampus == nil {
		return false
	}
	a.Hippocampus.setRecall(fn)
	return true
}
