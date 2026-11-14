package main

// chat_prefetch.go — the EXECUTOR half of typing-time memory prefetch: the
// goroutine that runs a debounced, read-only retrieval while the operator
// types, the record it leaves behind, and the ONE place its result is adopted
// into the turn's model input.
//
// The split of responsibility is deliberate:
//
//	internal/memoryprefetch  PURE decisions — keywords, the complexity gate, the
//	                         debounce/differential state machine, the budget
//	                         trim, the RAG section, the injected form (stdlib
//	                         only, no DB, no goroutine, testable without a
//	                         fixture).
//	this file                the GOROUTINE, the read-only ledger handle, the
//	                         bounded timeout, the record, and the adoption at
//	                         the outbound-message boundary.
//
// WHAT IT MAY NOT DO, and why each is a rule and not a preference:
//
//   - it never writes to the ledger. It opens the operator's memory DB through
//     tools.LedgerReadOnlyDSNIn (mode=ro + query_only(true)), NOT through the
//     production search opener, which MIGRATES a cold default DB — i.e. writes
//     — and would make a keystroke a writer. The row set is read with the same
//     query shape MemorySearchHits uses, so the prefetch cannot drift from what
//     the model would have got by asking itself.
//   - it never blocks the editor. Every observation it takes is a PUBLISHED
//     SNAPSHOT (Editor.Value().Text() — lineedit publishes on every mutation,
//     internal/lineedit/valueowner.go states the invariant), and every query it
//     issues runs on its own connection with its own timeout: a busy ledger
//     costs the prefetch its result, never the operator a keystroke.
//   - it never blocks the CLOSE. The result a lookup produced is taken from the
//     state machine without waiting for the lookup to finish, so a turn sealed
//     mid-query is sealed immediately (measured give-up on a blocked ledger
//     does not hold up the seal).
//   - it is never started when the switch is off — not "started and idled", but
//     never started, and the prompt is then byte-identical to the pre-knob tree
//     (cmd/agent/chat_prefetch_test.go pins both halves).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/memoryprefetch"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/tools"
	_ "modernc.org/sqlite" // driver name "sqlite", the name tools.LedgerReadOnlyDSNIn's DSN is built for
)

// The executor's own numbers, named (Rule 1).
const (
	// prefetchGoroutineTimeoutMs is the hard ceiling on ONE prefetch run's
	// ledger access, in milliseconds. It is deliberately far below the ledger's
	// own 15s busy timeout: that timeout exists so a writer QUEUES rather than
	// failing, which is the right contract for a foreground writer and the
	// wrong one for a lookup whose value expires in a second. A prefetch that
	// cannot read within this window GIVES UP and records the outcome.
	prefetchGoroutineTimeoutMs = 1500

	// prefetchLedgerBusyTimeoutMs is the busy timeout the prefetch's OWN
	// read-only DSN carries — deliberately NOT production's 15s. The handle must
	// refuse (or wait) far less than the goroutine's own deadline, so the
	// goroutine's bound is what ends a lookup against a wedged ledger.
	prefetchLedgerBusyTimeoutMs = 250

	// prefetchPollIntervalMs is how often the debounce is re-evaluated. The
	// executor does not watch keystrokes (that would need a new seam in the
	// editor); it samples the PUBLISHED snapshot on its own clock, which is
	// what makes "the operator paused" observable without touching the read
	// loop.
	prefetchPollIntervalMs = 100

	// prefetchHitLimit is how many rows the query asks for: a few more than the
	// section keeps, so the trim has something to trim with.
	prefetchHitLimit = 6

	// prefetchLadderBudgetMs bounds the SHARED recall ladder's walk inside one
	// lookup. It is a partition of prefetchGoroutineTimeoutMs, not an addition
	// to it: the ladder may spend this much, the fallback below it gets the
	// rest, and the lookup's own deadline still ends everything.
	//
	// WHY THE WALK NEEDS ITS OWN BOUND (measured, operator's ledger, 251MB,
	// 2026-10-07). The ladder is a series of COUNT probes over subsets, so a
	// query whose AND is 0 costs a walk, not a query: MEASURED full walks of
	// `254 321 426 cancelled e2e help merger 条积压` 861ms,
	// `peerbox 现在 上任务积压太多了 上任 任务 务积 积压 压太` 1723ms,
	// `还没 没修 修复 复的 的问 问题` 3203ms. Handing the ladder the whole
	// 1500ms would therefore turn the prefetch's outcome into "gave up" on
	// exactly the queries this change exists to fix. With the walk bounded
	// here, a slow query still returns its ladder pick when the walk FINDS one
	// in time (measured: the six real recorded queries pick at 0/39/45/200/245/
	// 465ms) and otherwise falls back to OR, which is measured at 1-175ms.
	//
	// The value is the measured knee: 400ms already loses the `peerbox …` walk,
	// 900 leaves ~600ms for the fallback — more than 3x the worst OR measured
	// on this ledger.
	prefetchLadderBudgetMs = 900

	// prefetchCandidatesLadder / prefetchCandidatesOR are the record's
	// `candidates` vocabulary: WHICH recall path produced the candidate set the
	// section was rendered from. They exist so the claim "the prefetch now runs
	// the ladder" is checkable per lookup rather than inferred from the code —
	// and so the OR path's survival is visible instead of assumed.
	prefetchCandidatesLadder = "ladder"
	prefetchCandidatesOR     = "or"

	// prefetchOutcome* are the record's outcome vocabulary. Named (Rule 1)
	// because the record is an OBSERVABLE contract: a reader (and the test)
	// matches on these strings, so a re-typed literal in either place would let
	// the two drift.
	prefetchOutcomeHits       = "hits"
	prefetchOutcomeEmpty      = "empty"
	prefetchOutcomeSuperseded = "superseded"
	// prefetchOutcomeBelowFloor is what the record says when the query DID
	// return rows but fewer than internal/memoryprefetch's injection floor
	// (MinInjectHits, framework-level, default 2), so the section was never
	// rendered and nothing was injected. It is its own word rather than a reuse
	// of prefetchOutcomeEmpty for the reason `empty` and `error` are separate:
	// the two answer DIFFERENT questions, and a reader acting on the wrong one
	// reaches the opposite conclusion — `empty` says the ledger had nothing on
	// the question; this says it had something and the floor declined it. The
	// lookup's own `hits` is recorded beside it (1, in the case the floor was
	// built for), so the count that was thrown away is observable rather than
	// inferred (Rule 15). The FIRST version of this change reported both
	// refusals as `empty`, and the A/B caught it: an arm whose hits the RENDERER
	// had trimmed away read as "the floor refused", which is a different cause.
	prefetchOutcomeBelowFloor = "below-floor"
	// prefetchOutcomeAdopted labels the ADOPTION event — the second half of the
	// story, on its own line: the fetch's outcome says what the lookup found,
	// this says that a prompt actually absorbed it. Two facts, two lines, so a
	// reader never has to infer one from the other.
	prefetchOutcomeAdopted = "adopted"
	prefetchOutcomeGaveUp  = "gave-up"
	prefetchOutcomeError   = "error"
	// prefetchOutcomeRefused is what the record says when the lookup was NEVER
	// MADE — the query did not run, so the row must not claim a search that
	// produced nothing (Rule 15: "no rows" and "never asked" are different
	// facts, and the record is the only place the difference can be read). Its
	// one producer is a REPLAY (internal/replaymode): the recording is the
	// context a replay works from, so re-querying the operator's live ledger
	// would answer a question the run has no business asking.
	prefetchOutcomeRefused = "refused"
	// prefetchOutcomeShared labels the event that records the TYPING path's
	// section being handed to a turn boundary that would otherwise have run the
	// SAME query a second time. It is an outcome of its own rather than a
	// variant of `adopted` because nothing was looked up: the lookup count is
	// exactly what a reader audits, and a shared turn must not appear in it.
	prefetchOutcomeShared = "shared"

	// replayPrefetchSkippedWhat is the ONE spelling of what a replay does NOT
	// build (chatPrefetchFor's construction-time skip). It is a constant because
	// the same words are the announcement's object and, through it, the test's
	// assertion — a re-typed literal in either place would let the sentence and
	// the code drift (Rule 1/13).
	replayPrefetchSkippedWhat = "build the typing-time and turn-boundary memory prefetch for this run"

	// prefetchNoSessionDirMarker is the ONE spelling of the record's own
	// "cannot be recorded" report, shared by openPrefetchRecord (which speaks it)
	// and the regression that requires it to be ABSENT under a replay (which
	// matches it). Rule 13/14: the sentence and its matcher are one value.
	prefetchNoSessionDirMarker = "no session directory — no prefetch record will be written"

	// prefetchDriverName is the database/sql driver name every ledger opener in
	// this tree passes as a string literal (internal/tools/ledger_dsn.go's
	// closing note explains why the LITERAL is load-bearing instrumentation for
	// a gate there). This file is a non-production opener the contract gate does
	// not scan, so it names the same string through this constant instead of a
	// bare literal.
	prefetchDriverName = "sqlite"

	// prefetchRecordAdoptedYes / No are the `adopted` field's vocabulary. The
	// record must be able to say NO — an observable that can only report success
	// is not an observable.
	prefetchRecordAdoptedYes = "yes"
	prefetchRecordAdoptedNo  = "no"

	// prefetchBusyRetryMax and prefetchBusyRetryDelayMs bound the retries ONE
	// lookup may spend on a ledger that is momentarily locked by a writer. They
	// do NOT enlarge the budget: a retry is only taken when the goroutine's own
	// deadline still has room for it, and the total is still capped by
	// prefetchGoroutineTimeoutMs. Measured reason for them existing at all:
	// with five workers and a merger writing this ledger, `database is locked
	// (5) (SQLITE_BUSY)` arrived 280ms into a lookup whose DSN busy timeout is
	// prefetchLedgerBusyTimeoutMs — a read that WAL says should not block on a
	// writer, and that succeeds on the next attempt.
	prefetchBusyRetryMax     = 2
	prefetchBusyRetryDelayMs = 40

	// prefetchHandleFresh / prefetchHandleSession are the record's `handle`
	// vocabulary: which of the two connection lifetimes a lookup used.
	// prefetchHandleSession is the executor's own long-lived read-only handle
	// (opened once, warmed, reused); prefetchHandleFresh is the one-shot shape a
	// caller with no executor opens (a test, a probe), whose dial and pragma
	// round-trip land inside that call's own budget.
	prefetchHandleFresh   = "fresh"
	prefetchHandleSession = "session"

	// prefetchWarmFallbackReason names the DECISION the executor takes when its
	// session handle cannot be warmed: it falls back to a dial per lookup rather
	// than refusing to retrieve anything (see the seatbelt comment in
	// (*chatPrefetch).handle for the reasoning and for the two disclosures that
	// make this a Rule 15-compliant fallback rather than a silent failure).
	// Named (Rule 1b) because it is the sentence an operator reads on stderr,
	// compared by a test, and it must not drift between the two.
	prefetchWarmFallbackReason = "deliberate fallback: the warm handle is an optimisation, not the feature"

	// prefetchSegNoOpenMs is how the record renders an open segment that never
	// happened: a reused handle pays no dial, and printing 0 would read as "the
	// dial was free" instead of "there was no dial". The same value is what a
	// segment that ran out of budget before it started reports.
	prefetchSegNoOpenMs = 0
	// prefetchSegUnitMs was retired from this block by the fix for the segment
	// identity (see prefetchLookupDetailed): the segments' unit is expressed by the
	// code — one clock, three spans of it, the third its remainder — and what the
	// reader checks is the SUM, not a rounding step to be tolerated.
)

// chatPrefetch is the run's prefetch EXECUTOR: one per chat session, created
// only when the switch is on (chatPrefetchFor). Its zero value is inert —
// every method is a no-op-safe read of state it does not have — so a nil-able
// pointer is not needed at the call sites.
type chatPrefetch struct {
	cfg  memoryprefetch.Config
	line func() string // the published line snapshot, nil on a face with no editor

	mu      sync.Mutex
	session memoryprefetch.Session
	// turnState is the TURN-BOUNDARY machine (chat_prefetch_turn.go), owned by
	// the REPL goroutine — deliberately a second State rather than the typing
	// one: the two answer different questions over different accumulations (the
	// typing machine's `Satisfied` is about a line that is still changing; a
	// turn's is about prompts already sent with a section), and sharing one would
	// make a turn's retrieval suppress the operator's next pause for terms it
	// happens to share. Neither is behind p.mu, because neither is shared between
	// goroutines: the loop goroutine owns `session`, the REPL owns `turnState`.
	turnState memoryprefetch.State
	record    *os.File // the run's record, nil when the session dir is unknown
	// handleRef is the session's warmed read-only ledger handle: ONE dial for
	// the whole session, reused by every lookup. nil until the first lookup asks
	// for it (and nil forever if the dial failed), which is what makes the
	// executor's cost profile visible in the record's `handle` field.
	handleRef *prefetchHandle
	// notesMu guards the SECOND leg's own handle and its one-shot latch. It is a
	// separate mutex from mu because the two legs run on the same goroutine but
	// the note handle is built lazily from whichever lookup asks first — and a
	// second dial is exactly what the latch exists to prevent.
	notesMu sync.Mutex
	// notesSrc is the note leg's session-scoped source (chat_prefetch_notes.go):
	// the index's read-only handle and the id of THIS run, whose own notes are
	// excluded (a note written moments ago by the run that is asking is text
	// already in its context — an echo, not a retrieval).
	notesSrc *prefetchNotesSource
	// notesSessionID is THIS run's own session id — the id whose notes are
	// EXCLUDED from the note leg (see memoryprefetch.NoteHits: a hit recorded by
	// the reader's own run is an echo of text already in its context, not a
	// retrieval). Empty means no exclusion, which is the honest answer for a
	// caller that has no session id.
	notesSessionID string
	// stderr is the channel the constructor already reports an unusable record
	// on. Stop() needs it too: the record's own close is the ONE failure the
	// record file cannot carry (it is the thing that just failed).
	stderr *os.File

	started bool
	done    chan struct{}
	core    context.Context
	cancel  context.CancelFunc
}

// chatPrefetchFor builds the session's executor, or returns nil when the knob
// is OFF — or when this process is RE-RUNNING A RECORDING (internal/replaymode),
// in which case the skip is announced once and nothing is allocated (see the
// short-circuit's own comment: a replay draws a session that already happened,
// so it has no business arming a sampler against the operator's live ledger).
//
// THE OFF PATH IS THE WHOLE CONTRACT: it starts no goroutine, opens no
// file, and returns nil — so the adoption point has nothing to append and the
// prompt is byte-identical. It is a separate function (rather than a branch
// inside a constructor that always allocates) so that "off starts nothing" is
// a property of the code path, not of a discipline.
func chatPrefetchFor(sessionDir string, line func() string, stderr *os.File) *chatPrefetch {
	if !config.MemoryPrefetchEnabled() {
		return nil
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	// A REPLAY BUILDS NO PREFETCH AT ALL (internal/replaymode; the ONE
	// construction point). The per-lookup guards below — fetch and
	// startTurnLookup — were the first answer to this defect and they are
	// correct in OUTCOME but wrong in PLACE: they let the machinery be built and
	// ARMED, so a replay printed one refusal per typing event and per recorded
	// turn, plus the record's own "no session directory" complaint, on a display
	// whose whole job is to draw a session that already happened (measured
	// 2026-10-24: `gopi chat --replay` stderr full of `[gopi replay] refused to …
	// memory prefetch …`).
	//
	// The question is asked HERE, before anything is allocated, opened or
	// started: a nil return means no goroutine, no record file and no sampling
	// face — which is also why the announcement is emitted HERE rather than left
	// to the per-event guards: those now never fire, and a run that skipped
	// silently would make "the prefetch was never built" and "the prefetch was
	// built and found nothing to do" the same observable (Rule 15).
	//
	// The guards below stay exactly as they are, as DEFENCE IN DEPTH: a future
	// caller that reaches the executor by another route still cannot query the
	// operator's ledger from a replay.
	if replaymode.Active() {
		// RECORDED, NOT DRAWN: a replay's screen is the recording, so the refusal
		// goes to the run's evidence file (Rule 15) and the band is left alone.
		announceReplayMemoryRefusal(replayPrefetchSkippedWhat)
		return nil
	}
	p := &chatPrefetch{
		cfg:    memoryprefetch.DefaultConfig(),
		line:   line,
		stderr: stderr,
		// The note leg's exclusion id comes from the SESSION DIRECTORY, which is
		// the run's own session id (<sessions root>/<session-uuid> — the same id
		// the index stamps into every row it records). One derivation, so the
		// leg excludes the reader's own echoes by construction rather than by a
		// second lookup of the same fact.
		notesSessionID: prefetchNotesSessionIDFromDir(sessionDir),
	}
	p.record = openPrefetchRecord(sessionDir, stderr)
	return p
}

// openPrefetchRecord opens (appending to) the run's prefetch record in the
// session directory. A session directory that cannot be resolved or written is
// REPORTED and the record is disabled — never silently missing (Rule 15), and
// never routed elsewhere: the session directory is the ONE place a run's
// evidence belongs (Rule 35 — no new artifact outside it).
func openPrefetchRecord(sessionDir string, stderr *os.File) *os.File {
	if strings.TrimSpace(sessionDir) == "" {
		fmt.Fprintf(stderr, "[gopi] memory prefetch: on, but this session has %s\n", prefetchNoSessionDirMarker)
		return nil
	}
	path := filepath.Join(sessionDir, config.MemoryPrefetchRecordName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintf(stderr, "[gopi] memory prefetch: on, but the record %s cannot be opened (%v) — prefetch runs will not be recorded\n", path, err)
		return nil
	}
	return f
}

// Start begins the loop, exactly once. It is idempotent because the REPL may
// call it from more than one wiring point; a second call must not create a
// second goroutine (that is the shape "no goroutine when off / ONE when on"
// has to survive).
//
// The cycle ctx is the RUN's context, so closing the chat stops the loop with
// everything else — no shutdown hook, no second lifecycle.
func (p *chatPrefetch) Start(ctx context.Context) {
	if p == nil || p.started {
		return
	}
	p.started = true
	p.core, p.cancel = context.WithCancel(ctx)
	p.done = make(chan struct{})
	go p.loop()
}

// Stop ends the loop and closes the record. Safe on nil and safe to call twice.
func (p *chatPrefetch) Stop() {
	if p == nil {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		<-p.done
	}
	// The close error is REPORTED, never discarded (Rule 15). A record that
	// could not be flushed is the difference between "the feature had nothing
	// to say" and "the observable is missing its tail" — and it is the one
	// failure the record FILE cannot carry, because the file is the channel
	// that just failed. So it goes to the same stderr the constructor already
	// reports an unusable record on.
	//
	// Reported, not returned and not panicked: this runs from a defer on the
	// chat command's path, where there is no receiver left for an error, and
	// Stop's contract is "safe on nil and safe to call twice" — a panic here
	// would take down a run whose only problem is an unflushed log line.
	p.mu.Lock()
	rec := p.record
	p.record = nil
	h := p.handleRef
	p.handleRef = nil
	p.mu.Unlock()
	if h != nil {
		h.close()
	}
	// The SECOND leg's handle is released through the same close-out: it is a
	// different database file and therefore a different handle, and a session
	// that ends without closing it would leak one connection per chat.
	p.notesMu.Lock()
	notes := p.notesSrc
	p.notesSrc = nil
	p.notesMu.Unlock()
	if notes != nil {
		notes.close()
	}
	if rec == nil {
		return
	}
	if err := rec.Close(); err != nil {
		p.reportRecordCloseFailure(err)
	}
}

// reportRecordCloseFailure names a prefetch RECORD that could not be closed.
// It is nil-safe on stderr (the zero value of chatPrefetch is inert) and it
// never touches the record itself — the record is what failed.
func (p *chatPrefetch) reportRecordCloseFailure(err error) {
	if p == nil || p.stderr == nil {
		return
	}
	fmt.Fprintf(p.stderr, "[gopi] memory prefetch: the record did not close cleanly (%v) — the run's prefetch lines after the last successful write may be missing\n", err)
}

// loop samples the published line on its own clock and, when the operator has
// paused on a line worth fetching, runs ONE lookup. It exits on the run's
// context; every error it meets is recorded, never swallowed (Rule 15).
func (p *chatPrefetch) loop() {
	defer close(p.done)
	tick := time.NewTicker(time.Duration(prefetchPollIntervalMs) * time.Millisecond)
	defer tick.Stop()
	last := ""
	var lastChange time.Time
	for {
		select {
		case <-p.core.Done():
			return
		case <-tick.C:
		}
		line := ""
		if p.line != nil {
			line = p.line()
		}
		now := time.Now()
		if line != last {
			last = line
			lastChange = now
			continue // the operator is still typing; nothing to do yet
		}
		if last == "" {
			continue
		}
		if !p.session.Observe(now, lastChange, p.cfg) {
			continue
		}
		query, ok := p.session.Claim(line, now, p.cfg)
		if !ok {
			continue
		}
		p.fetch(query)
	}
}

// notesSource returns the SECOND leg's session-scoped source (chat_prefetch_notes.go),
// or nil when this executor has none. It is built lazily ONCE — the note index is a
// different database file from the memory ledger, so it has a handle of its own, and
// the leg's one-dial latch lives on the source rather than on the executor.
func (p *chatPrefetch) notesSource() *prefetchNotesSource {
	if p == nil {
		return nil
	}
	p.notesMu.Lock()
	defer p.notesMu.Unlock()
	if p.notesSrc == nil {
		p.notesSrc = newPrefetchNotesSource(p.notesSessionID)
	}
	return p.notesSrc
}

// fetch runs ONE read-only lookup and stores what it found. The result is
// discarded by Use when the line moved on while the query was in flight — a
// stale section must never be adopted.
func (p *chatPrefetch) fetch(query string) {
	// A REPLAY ASKS NOTHING OF THE LIVE LEDGER (internal/replaymode; the epic's
	// defect B). The retrieval here is a QUERY against the operator's own
	// memory.db, and a replay's context is supposed to come from the RECORDING,
	// not from a fresh search of the store as it happens to look today — the
	// two give different answers, so a replay that queried would draw a picture
	// of a run nobody had. The guard sits HERE, at the entry point of the
	// lookup, rather than inside prefetchQueryRows: below this line there are
	// four more statements (the stat, the handle dial, the ladder match and the
	// two candidate queries), and stopping at the SQL statement alone would
	// still have opened the database.
	//
	// The row is WRITTEN, and it says `refused` — deliberately not `empty`. A
	// refused lookup and a lookup that found nothing are different facts, and
	// the record is the one place a reader can tell them apart (Rule 15). The
	// hits/ms pointers stay nil for the same reason: nothing was measured, so
	// nothing is reported as measured (Rule 15a).
	if replaymode.Active() {
		announceReplayMemoryRefusal("query the operator's ledger for a memory prefetch (" + query + ")")
		p.recordLine(prefetchRecord{
			Query:   query,
			Outcome: prefetchOutcomeRefused,
			Adopted: boolLabel(false),
		})
		return
	}
	// The lookup's segments come from the call itself, so a give-up can be
	// attributed to the part that spent the budget (stat / open / query) instead
	// of being reported as one anonymous 1500ms (Rule 15: a diagnosis, not a
	// timestamp). Its `ms` comes from the same call, for the same reason: a second
	// clock wrapped around the first would report a window (this function's own
	// entry, the handle lookup included) that the segments cannot account for —
	// measured at up to 2ms of unaccounted residual under load, which is exactly
	// what made the segment sum look like a rounding artefact instead of the
	// identity it is.
	res := prefetchLookupDetailed(p.coreOrBackground(), query, p.cfg, p.handle(), p.notesSource())
	outcome, hits, err := res.Outcome, res.Hits, res.Err
	section := res.Section
	adopted := false
	if len(section) > 0 {
		// A result the differential no longer recognises as the line's own is
		// REFUSED here (the operator typed on while the query ran) and recorded
		// as superseded — never silently stored for a question nobody asked.
		adopted = p.session.Use(query, section)
		if !adopted {
			outcome = prefetchOutcomeSuperseded
		}
	}
	p.recordLine(prefetchRecord{
		Query:         query,
		Hits:          prefetchHitsLabel(hits),
		Ms:            prefetchMsLabel(res.ElapsedMs),
		Outcome:       outcome,
		Adopted:       boolLabel(adopted),
		Err:           prefetchErrText(err),
		Segment:       res.Segment,
		Segments:      &res.Seg,
		Candidates:    res.Candidates,
		Notes:         prefetchNotesLabel(res.Notes),
		MemoryOutcome: res.MemoryOutcome,
	})
}

// coreOrBackground returns the run's context, or a background one when no run
// is in flight. The zero value of chatPrefetch is documented as INERT, and an
// inert executor must still be usable by a caller that drives it directly (a
// test, a diagnostic): `context.WithTimeout` panics on a nil parent, so the
// fallback is what makes that promise true rather than aspirational.
func (p *chatPrefetch) coreOrBackground() context.Context {
	if p != nil && p.core != nil {
		return p.core
	}
	return context.Background()
}

// handle returns the executor's warmed read-only handle, dialling it ONCE on
// first use. It runs in the prefetch goroutine (never on the operator's typing
// path); a dial that fails is not retried here — the lookup that asked for the
// handle simply falls back to its own fresh one, whose failure it will record
// with its own segment attribution.
//
// The mutex covers the construction only: the handle itself is safe for
// concurrent use, and a second caller racing the first must not dial a second
// connection (the ONE-HANDLE-PER-SESSION property is what makes the dial
// cost amortise).
func (p *chatPrefetch) handle() *prefetchHandle {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handleRef != nil {
		return p.handleRef
	}
	path := config.LedgerPath()
	if strings.TrimSpace(path) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(p.coreOrBackground(), time.Duration(prefetchGoroutineTimeoutMs)*time.Millisecond)
	defer cancel()
	h, err := prefetchOpenHandle(ctx, path)
	if err != nil {
		// DELIBERATE FALLBACK — a recorded DECISION, not an oversight.
		//
		// WHY NOT REFUSE. The warm handle is an OPTIMISATION; the prefetch is
		// the feature. Refusing here would delete retrieval for the WHOLE
		// session because one dial failed once — and a dial is measured to fail
		// on conditions that say nothing about whether the ledger is usable.
		// The instance this rule was decided on: this task's own stream carries
		// "warm read-only ledger: unable to open database file: out of memory
		// (14)) — each lookup will dial its own" in the same second the same
		// file's write also failed, i.e. a scratch root under pressure, not a
		// broken ledger. The feature still works in that state: each lookup
		// opens its own handle and pays the dial inside its own budget.
		//
		// SO RULE 15 IS SATISFIED BY DISCLOSURE, NOT BY A REFUSAL — refusing
		// here would be a failure that HIDES a working path. Two things carry
		// the disclosure, and both are required:
		//
		//   1. the failure is NAMED on stderr with its CAUSE and the LEDGER
		//      PATH, under the reason below — never swallowed;
		//   2. the record carries the CONSEQUENCE: every lookup of this session
		//      reports `handle: "fresh"` (prefetchHandleFresh), the field a
		//      reader needs to know the dial was NOT amortised and to interpret
		//      the lookup's own ms. The trace of this fallback in a session's
		//      record is therefore `grep '"handle":"fresh"'`.
		if p.stderr != nil {
			fmt.Fprintf(p.stderr, "[gopi] memory prefetch: %s — could not warm the read-only ledger handle for %s (%v); each lookup will dial its own (the record will say handle=%q)\n",
				prefetchWarmFallbackReason, path, err, prefetchHandleFresh)
		}
		return nil
	}
	p.handleRef = h
	return h
}

// take returns the section to append for the line being sealed, and marks it
// adopted in the record. It NEVER waits for an in-flight lookup: a turn sealed
// mid-query is sealed without the section (the give-up is the honest
// behaviour — the alternative would put the operator's Enter behind a busy
// ledger).
func (p *chatPrefetch) take(line string) (chatPrefetchSection, bool) {
	if p == nil {
		return chatPrefetchSection{}, false
	}
	p.mu.Lock()
	section, ok := p.session.Take(line, p.cfg)
	query := p.session.PendingQuery()
	p.mu.Unlock()
	if !ok {
		return chatPrefetchSection{}, false
	}
	p.recordLine(prefetchRecord{
		Query:        query,
		Outcome:      prefetchOutcomeAdopted,
		Adopted:      prefetchRecordAdoptedYes,
		SectionBytes: len(section),
	})
	return chatPrefetchSection{Query: query, Section: section}, true
}

// turnQuery returns the retrieval key this turn would fetch, or "" when the
// turn's input yields nothing worth retrieving. It is the pure core's own answer
// (memoryprefetch.TurnQuery) with the executor's config applied — the executor
// does not rebuild the key, because a second join of the same terms is a second
// statement of the delta rule (Rule 13/14).
//
// WHY THE SHARING CHECK NEEDS IT: the section handed over by takeShared may only
// be shared when it answers THIS turn's question, and the turn's question is
// TurnStep's input — the same text, the same gate, the same delta. Any other
// comparison (the line, the message, a prefix of either) would be about
// something adjacent to the question rather than about the question.
func (p *chatPrefetch) turnQuery(in memoryprefetch.TurnInput) string {
	if p == nil {
		return ""
	}
	query, _, err := memoryprefetch.TurnQuery(&p.turnState, in, p.cfg)
	if err != nil {
		// The input was unusable for a reason the pure core names. It is reported
		// rather than swallowed (Rule 15) and the sharing is skipped: the turn
		// then follows its ordinary path, which records the same failure.
		p.reportTurnPayloadFailure(prefetchErrText(err))
		return ""
	}
	return query
}

// takeShared hands the TYPING path's pending section to a TURN BOUNDARY whose
// own query is the SAME string — the ONE-retrieval-per-turn contract
// (task 01a11601).
//
// THE DEFECT IT CLOSES, measured on a real run. A line the operator TYPED is
// handed to both machines: the typing sampler, on its own clock, builds a key
// from the published buffer and fetches it; at the seal the turn boundary builds
// a key from the same words and fetches it AGAIN. The run's own record shows it
// as two `outcome: hits` lines with one identical query — one question, two
// ledger round-trips, two sections rendered for one prompt. A full line with a
// pause is the WORST case (both keys complete before the Enter), and it is the
// ordinary case for an operator who reads what they typed before submitting.
//
// WHY THE COMPARISON IS ON THE QUERY, and not on the source or the line. The
// turn boundary is not a duplicate of the typing path — it exists for the four
// faces the sampler cannot see, and their keys are DIFFERENT BY CONSTRUCTION: a
// notify turn's words are in the notice, a paste turn's are in the spilled file
// behind a reference (`lineedit.StripPasteRefs`; the reference's own words are a
// path and a size), a `/queue`d prompt is typed into an editor whose buffer the
// sampler never saw, and a model-typed line has no operator at the keyboard. So
// "this turn was prefetched at typing time" is TRUE exactly when the two keys
// are equal, which is exactly when the second lookup costs a question nobody
// asked — including the query the typing machine actually FETCHED, which is its
// own clipped/short form (`Session.Query`) and need not match a freshly built
// key.
//
// WHY IT LOOKS AT THE TYPING MACHINE'S OWN MEMORY. `PendingQuery` is the query
// whose section the Session is holding; comparing it here is the ONLY honest
// test, because a section is shared only when it answers this turn's question.
// A key that MATCHES but whose section the typing path has already handed to an
// earlier turn returns ok=false from Take (the Session clears it on take), so a
// later turn gets its own retrieval rather than a section that is already spent.
//
// IT IS CONSERVATIVE IN ONE DIRECTION ON PURPOSE, and the direction is stated
// rather than hidden: the comparison is on the whole query string, so a turn
// whose key merely OVERLAPS the typed one still spends its own lookup. An
// overlap-based skip would suppress a genuinely different question — the failure
// this feature was built to prevent — and a missed sharing costs one lookup,
// which is the cheap side of that trade.
//
// WHAT IT RECORDS. `outcome: shared` (never `hits`): no query ran here, and the
// record's lookup count is exactly what a reader audits — a shared turn must not
// appear in it. The adoption half is recorded the same way any adoption is
// (adopted=yes + section_bytes), so "did a prompt absorb this section?" has one
// answer regardless of which machine fetched it.
func (p *chatPrefetch) takeShared(line, query, source string) (chatPrefetchSection, bool) {
	if p == nil || query == "" {
		return chatPrefetchSection{}, false
	}
	p.mu.Lock()
	if p.session.PendingQuery() != query {
		p.mu.Unlock()
		return chatPrefetchSection{}, false
	}
	section, ok := p.session.Take(line, p.cfg)
	p.mu.Unlock()
	if !ok {
		return chatPrefetchSection{}, false
	}
	p.recordLine(prefetchRecord{
		Query:        query,
		Outcome:      prefetchOutcomeShared,
		Adopted:      prefetchRecordAdoptedYes,
		SectionBytes: len(section),
		Source:       source,
	})
	return chatPrefetchSection{Query: query, Section: section}, true
}

// prefetchRecord is the per-run observable: what was asked, what came back,
// how long it took, how it ended, and whether it reached a prompt. It is one
// JSON line per event, in the session directory.
//
// IT CARRIES TWO KINDS OF EVENT, AND THEY MEASURE DIFFERENT THINGS (Rule 15a —
// the reason the fields below are pointers). A LOOKUP line answers "what was
// asked, what came back, how long it took"; an ADOPTION line (outcome=adopted)
// answers "a prompt absorbed a section, and it was this big" and ran no query of
// its own. A field that belongs to one kind is therefore ABSENT from the other,
// never zero: an int cannot carry both "not measured" and "measured zero", and
// the corpus proved the collision was live rather than theoretical — a census
// asking the record's own documented question ("how many lookups found
// nothing?") counted 47 hits=0 lines, 35 of which were adoption events that had
// found something. Absent is decided by the KEY, which is what a reader of the
// JSON sees; a consumer that ignores keys still reads a zero, so nothing that
// worked before breaks.
type prefetchRecord struct {
	// Query is the retrieval key this event ASKED. It is optional in the wire
	// sense because an event with no question of its own (a supersession, an
	// entry whose input could not be assembled) must not render as an event that
	// asked the EMPTY STRING — different facts, and only one of them is worth a
	// reader's attention (measured: 12 such lines in the corpus, every one of
	// them reading `"query":""`).
	Query string `json:"query,omitempty"`
	// Hits and Ms are the LOOKUP's measurement, in POINTERS because "not
	// measured" and "measured zero" are different answers (Rule 15a) and an int
	// cannot spell both. A lookup sets them — a lookup that found nothing
	// legitimately reports hits=0, and a fast one legitimately reports ms=0 —
	// while an adoption event leaves them nil, which is what makes the
	// fabrication this record used to carry impossible to write down rather than
	// merely discouraged.
	Hits    *int   `json:"hits,omitempty"`
	Ms      *int64 `json:"ms,omitempty"`
	Outcome string `json:"outcome"`
	Adopted string `json:"adopted"`
	Err     string `json:"err,omitempty"`
	// Source names the FACE the retrieved turn's input came from
	// (internal/inputsource: human / notify / llm) — the same provenance the
	// message boundary stamps on a reference. It is set by the TURN-BOUNDARY
	// path only: the typing path has no turn face to name, and leaving the field
	// absent there is what keeps the typing record's bytes unchanged by this
	// addition (Rule 15a — an optional field stays absent when it is unknown).
	Source string `json:"source,omitempty"`
	// SectionBytes is the size of the block a prompt absorbed (the adoption
	// event), so the record answers "how much did this cost the context?" — the
	// question a token budget is actually made of — without re-reading a log.
	SectionBytes int `json:"section_bytes,omitempty"`
	// Ms is the LOOKUP's own elapsed reading (prefetchResult.ElapsedMs), never a
	// clock the caller wrapped around it — the segments are spans of that same
	// clock, so `ms` and `seg_*_ms` are one measurement reported four ways.
	//
	// Segments is WHERE the budget went, and it is the field that makes a
	// give-up diagnosable: `ms` alone says a lookup failed to finish inside
	// prefetchGoroutineTimeoutMs, while `seg_open_ms` vs `seg_query_ms` says
	// whether the dial, the lock or the search spent it. Present on every lookup
	// (not only failures) so a fast run's shape is comparable with a slow one.
	Segments *prefetchSegments `json:"segments,omitempty"`
	// Segment names the single part a give-up is attributed to (stat / open /
	// query). Empty on success.
	Segment string `json:"segment,omitempty"`
	// Candidates names the recall path that produced this lookup's candidate set
	// (ladder / or) — the observable behind "prefetch now runs the ladder", and
	// the reason the OR fallback's rate is a number rather than a claim.
	// Optional in the JSON sense only: it is empty when no query ran at all
	// (superseded / empty), where there is no path to name.
	Candidates string `json:"candidates,omitempty"`

	// Notes is the SECOND retrieval leg's measurement — the round-level note
	// index (chat_prefetch_notes.go). It is a pointer because "no note leg ran"
	// (an adoption event, a caller with no executor) and "the note leg ran and
	// found nothing" are different answers, and only one of them is worth a
	// reader's attention (Rule 15a, the same reason Hits/Ms are pointers).
	//
	// WHY IT IS ITS OWN OBJECT RATHER THAN A FEW MORE SCALARS. The leg has a
	// verdict of its own (an index that is ABSENT is not an index that FAILED),
	// a tier of its own (which of fts/trigram/like answered), a count, a clock and
	// an error — five facts that belong together and that a reader compares with
	// the memory leg's five. Keeping them under one key is what makes the pair
	// readable side by side instead of a flat list of near-identical names.
	Notes *prefetchNotesRecord `json:"notes,omitempty"`

	// MemoryOutcome is the MEMORY leg's own verdict, present only when a second
	// leg was consulted (the pair `memory_outcome` + `notes.outcome` is then
	// always complete). With one leg the `outcome` field already IS the memory
	// leg's verdict, and emitting a second copy would be the same fact recorded
	// twice (Rule 14) — which is why this field is omitted on the memory-only
	// shapes and their record bytes are unchanged.
	MemoryOutcome string `json:"memory_outcome,omitempty"`
}

// prefetchNotesRecord is the note leg's half of a lookup's record line. Every
// field is always written when the leg ran, including the failure ones, so a
// reader never has to infer "did the second leg even ask?" from the absence of a
// key (Rule 15a: the leg's presence is stated).
type prefetchNotesRecord struct {
	// Outcome is the leg's own verdict: hits / empty / absent / failed /
	// gave-up / unavailable (the session-scoped latch) / not-consulted (no
	// source was handed in). Named constants, never literals (Rule 1).
	Outcome string `json:"outcome"`
	// Tier names which tier of the shared ladder answered (fts / trigram /
	// like), empty when none did.
	Tier string `json:"tier,omitempty"`
	// HitsCount is how many note hits the leg OFFERED the section (after the
	// echo exclusion and the empty-row refusal, before the floor and the trim).
	HitsCount int `json:"hits"`
	// Ms is the leg's own clock, measured outside the memory leg's window so the
	// two numbers cannot hide inside one another (prefetchLookupDetailed).
	Ms int64 `json:"ms"`
	// Err names why the leg could not answer. Present whenever Outcome is failed
	// or gave-up — a leg that raised an error must never read as a leg that found
	// nothing (Rule 15).
	Err string `json:"err,omitempty"`
}

// recordHasQuery reports whether the run's record has recorded at least one
// QUERY. It exists for the tests (and is honest about that in its name): the
// record's whole value is naming what was asked, so "there is a record" is not
// the claim — "the record names the question" is.
func (p *chatPrefetch) recordHasQuery() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.record == nil {
		return false
	}
	b, err := os.ReadFile(p.record.Name())
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "\"query\":")
}

// recordLine appends one line to the record. It is mutex-guarded because the
// fetch goroutine and the REPL's seal both call it, and a torn JSON line would
// make the observable unreadable exactly when it matters.
func (p *chatPrefetch) recordLine(rec prefetchRecord) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.record == nil {
		return
	}
	b, err := json.Marshal(rec)
	if err != nil {
		// Rule 15: an unrecordable event says so, in the channel that still
		// works (the record file is already the fallback channel, so this goes
		// to it — as a line that NAMES the failure rather than a silent gap).
		fmt.Fprintf(p.record, "{\"error\":%q}\n", err.Error())
		return
	}
	if _, err := p.record.Write(append(b, '\n')); err != nil {
		// Deliberately discarded: the record is an OBSERVABLE, not the feature.
		// A write failure cannot be reported anywhere the operator is looking
		// (this is the last channel), and it must never take the prefetch —
		// still less the run — down with it.
		return
	}
}

// prefetchHandle is the executor's OWN read-only ledger handle: ONE dial, ONE
// pragma readback, reused by every lookup of the session.
//
// WHY IT EXISTS — measured, and it is the whole point of this file's history.
// The first version opened a fresh `sql.DB` per lookup and closed it at the end.
// `sql.Open` is LAZY, so the dial (open the 236MB file, read its header, find
// the WAL, acquire the wal-index read mark) and the DSN's pragma round-trip both
// happen INSIDE the lookup's budget, on the caller's first statement — and once
// the dialect is spoken, any contention on the wal-index (five resident workers
// plus a merger write this ledger continuously) is added on top of the query
// rather than amortised. A warm handle pays that cost once, in the executor's
// own goroutine, and every lookup after it is a query.
type prefetchHandle struct {
	db *sql.DB

	// path is the file this handle was dialled against, recorded as a FACT rather
	// than re-resolved by a caller. It exists so a second reader can ask "is the
	// store I want the one this handle already holds?" without resolving the same
	// path twice and hoping the two resolutions agree — and the answer matters
	// here: the note index (tools.ChatNoteIndexPath) and the memory ledger
	// (config.LedgerPath) are THE SAME FILE in the ordinary configuration, so the
	// second retrieval leg reuses this handle instead of dialling the same file
	// again (Rule 35: no second connection to a store already open).
	path string

	// openMs is what the dial + pragma readback cost when this handle was
	// built. It is reported on every record that uses the handle, so the reader
	// can tell a warmed lookup from a cold one without a second data source.
	openMs int64

	// The effective connection state, read back FROM the connection (never
	// assumed from the DSN): the DSN is a request, PRAGMA is the answer. A
	// reader of the record can then see the read-only contract hold rather than
	// take this file's word for it.
	journalMode string
	queryOnly   int
	busyTimeout int
}

// usable reports whether this handle can still run a query — that is, whether it
// is non-nil and still holds its connection.
//
// IT EXISTS BECAUSE THE HANDLE'S LIFETIME OUTLIVES THE POINTER. `close` (called
// by chatPrefetch.Stop) nulls the field, and a lookup that is already in flight
// keeps the POINTER it captured — so "the handle I was handed" and "a handle I
// can query" stopped being the same question, and a second reader that answered
// the first one (path equality) went on to dereference nothing. That is the
// measured defect this method was added for (a SIGSEGV in the note leg's search,
// see the devdoc report); the predicate is spelled ONCE so every caller of a
// handle asks the same question (Rule 14).
func (h *prefetchHandle) usable() bool {
	if h == nil {
		return false
	}
	return h.db != nil
}

// usableDB is the connection this handle may be queried through, or nil when it
// has none — the VALUE form of the same predicate, for the call sites that must
// hand the connection to something else (the note leg hands it to
// tools.ChatNoteSearchOn). A caller that needs the connection must go through
// here rather than read the field: one place decides what "usable" means.
func (h *prefetchHandle) usableDB() *sql.DB {
	if !h.usable() {
		return nil
	}
	return h.db
}

// prefetchHitsLabel / prefetchMsLabel wrap a LOOKUP's measurement for the
// record. They exist so the two call sites that DO measure (the typing lookup
// and the turn lookup) cannot accidentally record a bare zero where the absence
// of a measurement is meant, and so the pointer-taking is spelled once (Rule
// 14). The name is the value: each returns a addressable copy of the number it
// was handed.
func prefetchHitsLabel(hits int) *int { return &hits }

func prefetchMsLabel(ms int64) *int64 { return &ms }

// prefetchSegments is where ONE lookup's budget went. Its field names are the
// question the record exists to answer — "the 1500ms deadline was exceeded, so
// which segment ate it?" — and `query_ms + open_ms + stat_ms == ms` is the
// property that makes the answer checkable rather than assertable.
type prefetchSegments struct {
	StatMs  int64 `json:"seg_stat_ms"`
	OpenMs  int64 `json:"seg_open_ms"`
	QueryMs int64 `json:"seg_query_ms"`

	// Handle says which connection lifetime was used; the two together are the
	// measurement (a fresh handle's open segment is real work, a reused one's is
	// zero because there was nothing to do).
	Handle string `json:"handle"`

	// WaitCount / WaitMs are database/sql's own pool-wait numbers for the query
	// (`db.Stats()` deltas): how many times the lookup had to WAIT for the
	// single pooled connection, and for how long. They separate "SQLite was
	// slow" from "this process was serialising itself", which no wall clock can
	// tell apart.
	WaitCount int64 `json:"wait_count"`
	WaitMs    int64 `json:"wait_ms"`

	// HandleWarmMs is what the SESSION handle's dial + pragma readback cost when
	// it was built — the cost this lookup did NOT pay, because the executor had
	// already paid it. Kept separate from OpenMs precisely so the identity
	// `ms ≈ seg_stat_ms + seg_open_ms + seg_query_ms` still holds for both
	// shapes: a segment reports the work THIS lookup performed, and a cost paid
	// once per session is reported once, as itself.
	HandleWarmMs int64 `json:"handle_warm_ms,omitempty"`

	// The connection's read-back state, so the read-only contract is OBSERVED,
	// not assumed.
	JournalMode string `json:"journal_mode,omitempty"`
	QueryOnly   int    `json:"query_only,omitempty"`
	BusyTimeout int    `json:"busy_timeout_ms,omitempty"`

	// Attempts is how many times the query was issued: a value above 1 is a
	// ledger that was locked and then let go, which is a different fact from a
	// query that took that long.
	Attempts int `json:"attempts"`
}

// prefetchResult is what ONE lookup produced, in one value: the section (empty
// unless Outcome is hits), the outcome, the hit count, the error, and where the
// time went.
type prefetchResult struct {
	Section string
	Outcome string
	Hits    int

	// Candidates names WHICH recall path produced the candidate set this result
	// was rendered from (prefetchCandidatesLadder / prefetchCandidatesOR). It is
	// the per-lookup evidence for the change's central claim — the section was
	// built from the ladder's AND-subset, not from OR — and it is recorded on
	// every lookup, not only on the interesting ones (a field present only in
	// some outcomes cannot be counted).
	Candidates string

	// Segment is the one that consumed the budget when the outcome is gave-up:
	// prefetchSegStat / prefetchSegOpen / prefetchSegQuery. It is what turns
	// "context deadline exceeded" into a diagnosis.
	Segment string
	Err     error

	// ElapsedMs is what the LOOKUP's own single clock read when it finished —
	// the same clock its segments are spans of, so `ms`, `seg_stat_ms`,
	// `seg_open_ms` and `seg_query_ms` are one measurement reported four ways
	// instead of four measurements that have to be reconciled. The record takes
	// its `ms` from HERE and never from a caller's outer clock (Rule 14: one
	// source of truth for one fact, and the fact is "how long the lookup took").
	//
	// IT MEASURES THE MEMORY LEG, and that is deliberate: the second leg (the
	// note index, chat_prefetch_notes.go) is measured on its own clock and
	// reported in Notes.Ms, so this window and its three segments keep the
	// identity they are asserted on, and a reader can still attribute a
	// slow give-up to the part that spent the budget.
	ElapsedMs int64

	Seg prefetchSegments

	// MemoryHits are the memory leg's OWN rows, kept so the second leg's rows can
	// be merged with them and the pair re-rendered into the ONE section. It is
	// not recorded (the record carries COUNTS: MemoryHitsCount, Notes.HitsCount,
	// and the combined `hits`).
	MemoryHits []memoryprefetch.Hit

	// MemoryHitsCount is how many rows the MEMORY leg returned, before either the
	// note leg's merge or the injection floor — the number a reader needs to tell
	// "the ledger had nothing" from "the ledger's rows did not survive".
	MemoryHitsCount int

	// CombinedHits is how many rows BOTH legs offered the section, before the
	// floor and the rune trim judged them. It is the count the injection floor
	// reads, so recording it makes "why was nothing injected" answerable without
	// re-running the query.
	CombinedHits int

	// LegDegradations are the named losses the COMPOSITION added (today: the note
	// leg's, when the note index could not be read at all). They ride into the
	// section through InjectSection, so a leg's failure is visible in the block
	// the model reads and not only in the record.
	LegDegradations []memoryprefetch.Degradation

	// MemoryOutcome is the memory leg's own verdict, kept separately from
	// Outcome because the two answer different questions once a second leg
	// exists: Outcome says what the operator's block ENDED UP carrying, while
	// this says what the memory leg alone found. Reported only when a second leg
	// was consulted (the record's `memory_outcome`, which then always rides
	// beside `notes_outcome`).
	MemoryOutcome string

	// Notes is the second leg's result — the note index's outcome, tier, hit
	// count, elapsed time and any error, all of them recorded even when the leg
	// contributed nothing (Rule 15: a leg that could not be asked must not look
	// like a leg that found nothing).
	Notes prefetchNotesResult
}

// The segment NAMES a give-up can be attributed to.
const (
	prefetchSegStat  = "stat"
	prefetchSegOpen  = "open"
	prefetchSegQuery = "query"
)

// prefetchOpenHandle dials the read-only ledger ONCE and reads the connection's
// effective state back off it. It is called by the executor on first use (in the
// prefetch goroutine, never on the operator's typing path) and by prefetchLookup
// when it was handed no handle.
//
// The readback is not decoration: `mode=ro` and `query_only(true)` in a DSN are
// a REQUEST, and the record's job is to carry the ANSWER. `PRAGMA journal_mode`
// additionally tells the reader whether the WAL is actually available — the fact
// that decides whether a read-only lookup can be blocked by a writer at all.
func prefetchOpenHandle(ctx context.Context, path string) (*prefetchHandle, error) {
	started := time.Now()
	db, err := sql.Open(prefetchDriverName, tools.LedgerReadOnlyDSNIn(path, prefetchLedgerBusyTimeoutMs))
	if err != nil {
		return nil, fmt.Errorf("open read-only ledger: %w", err)
	}
	db.SetMaxOpenConns(1)
	h := &prefetchHandle{db: db, path: path}
	// The dial is lazy; this statement is what performs it, and it is the reason
	// the call takes a context at all.
	//
	// The failure is read through gopi's own diagnosis before it is wrapped
	// (chat_prefetch_ledger_open.go): the driver's sentence for SQLITE_CANTOPEN
	// — `unable to open database file: out of memory (14)` — reads as an
	// allocation failure and names neither the ledger nor the trigger, while
	// the actual defect is path-shaped. The driver's text stays in the chain
	// (%w), so nothing is hidden.
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&h.journalMode); err != nil {
		db.Close() //stylegate:ignore a handle that failed to warm is closed here and never reused
		return nil, fmt.Errorf("warm read-only ledger: %w", prefetchLedgerOpenCause(err, path))
	}
	if err := db.QueryRowContext(ctx, "PRAGMA query_only").Scan(&h.queryOnly); err != nil {
		db.Close() //stylegate:ignore same: the handle never reaches a caller
		return nil, fmt.Errorf("read query_only: %w", err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&h.busyTimeout); err != nil {
		db.Close() //stylegate:ignore same: the handle never reaches a caller
		return nil, fmt.Errorf("read busy_timeout: %w", err)
	}
	h.openMs = time.Since(started).Milliseconds()
	return h, nil
}

// close releases the handle's connection. Safe on nil.
func (h *prefetchHandle) close() {
	if h == nil || h.db == nil {
		return
	}
	// The close error is discarded deliberately: this runs at the end of the
	// executor's life, on a handle that is READ-ONLY, so a failed close cannot
	// strand a write. It is the one place in this file where a discard is right,
	// and it is stated rather than silent (Rule 15).
	_ = h.db.Close() //stylegate:ignore the prefetch handle is READ-ONLY and this is the executor's last act, so a failed close strands no write (stated in the comment above)
	h.db = nil
}

// prefetchLookup runs the ONE read-only retrieval: a direct query against the
// operator's memory ledger, opened with tools.LedgerReadOnlyDSNIn, under a
// context deadline.
//
// h may be nil, in which case a fresh handle is dialled for this call alone and
// closed before it returns (the shape a caller with no executor uses). The
// executor passes its own long-lived handle so the dial is paid once per
// session.
//
// WHY NOT tools.MemorySearchHits: that entry point resolves the path through
// internal/config itself (so a test cannot point it at a fixture without
// mutating the process environment) and opens through the search opener, which
// MIGRATES a cold default DB. The prefetch must be provably read-only and
// provably pointed at the ledger the caller named, so it opens the DSN the
// seams doc selected and runs the production search shape (FTS MATCH, BM25
// rank) against it.
func prefetchLookup(ctx context.Context, query string, cfg memoryprefetch.Config) (string, string, int, error) {
	r := prefetchLookupDetailed(ctx, query, cfg, nil, nil)
	return r.Section, r.Outcome, r.Hits, r.Err
}

// prefetchLookupDetailed is prefetchLookup plus WHERE THE TIME WENT. Every
// segment below is measured because the record must be able to answer the one
// question a give-up raises — "the deadline expired; which part spent it?" —
// without a second instrument or a re-run.
//
// h is the executor's warmed handle, or nil for the one-shot shape. notes is the
// SECOND leg's source (chat_prefetch_notes.go), or nil for a caller that has no
// note index to consult — in which case the leg states `not-consulted` rather
// than reporting an empty search (Rule 15).
func prefetchLookupDetailed(ctx context.Context, query string, cfg memoryprefetch.Config, h *prefetchHandle, notes *prefetchNotesSource) prefetchResult {
	// ONE clock for the whole lookup, read HERE — before path resolution, before
	// the stat, before anything. Every segment is a SPAN OF this clock and the
	// query segment is its REMAINDER, which makes the identity below exact by
	// construction rather than by tolerance:
	//
	//	ms = seg_stat_ms + seg_open_ms + seg_query_ms
	//
	// WHY IT IS WORTH A CLOCK OF ITS OWN (measured, the defect this task was filed
	// on). The older shape took `ms` from the CALLER's clock (chatPrefetch.fetch
	// wrapped the call in its own time.Since), stat from a clock started after path
	// resolution, and open from a third started at the dial — three separately
	// floored spans that each lose part of a millisecond in the SAME direction, so
	// the reported `ms` could sit 1-2 above the sum of its own segments. Under load
	// that is exactly the "residual out of tolerance" a test sees, but it is NOT a
	// measurement artefact to be tolerated: a record whose parts do not account for
	// its whole is a record that cannot be trusted about WHERE a give-up's time
	// went, which is the single question these segments exist to answer. FIXED AT
	// THE ORIGIN (Rule 10#3) — one clock, one remainder — never by widening the
	// tolerance.
	lookupStart := time.Now()
	res := prefetchLookupPhased(ctx, query, cfg, h, notes, lookupStart)
	// Every segment is `.Milliseconds()` of a span of this ONE clock and the query
	// segment is the REMAINDER of it, so the three sum to `ms` EXACTLY — never a
	// tolerance. The remainder is never negative either: stat and open are readings
	// of an earlier point of this same monotonic clock, so their sum can only be
	// <= this final reading.
	//
	// THE SECOND LEG IS MEASURED OUTSIDE THIS WINDOW. prefetchLookupPhased runs the
	// note leg AFTER it has taken its own final reading, so the note leg's cost is
	// not inside `ms` and cannot break the identity the three segments are asserted
	// on. It is reported as its own number (Notes.Ms), which is what a reader
	// attributing a slow run needs: one measurement per leg, neither hiding inside
	// the other.
	res.ElapsedMs = time.Since(lookupStart).Milliseconds()
	res.Seg.QueryMs = res.ElapsedMs - res.Seg.StatMs - res.Seg.OpenMs
	if notes != nil {
		res.Notes = prefetchNotesSearch(ctx, query, cfg, notes, h)
		// The memory leg's own verdict, captured BEFORE composition overwrites
		// Outcome with the section's: with two legs, "what did the note index
		// contribute" and "what did the memory ledger answer" are two questions,
		// and only their pair tells a reader whether an injected block came from
		// the ledger, the notes, or both.
		res.MemoryOutcome = res.Outcome
	}
	return prefetchComposeSection(res, cfg)
}

// prefetchLookupPhased is the lookup's phases, measured on the ONE clock its
// caller opened and handed in. It never reports `ms` itself — the reading and
// the segment identity belong to prefetchLookupDetailed, in one place, so no
// early return can leave the record half-accounted.
//
// It returns after the MEMORY leg has produced its result (section, outcome,
// rows): the note leg runs in the caller, on its own clock, once this phase's
// measurement is closed.
func prefetchLookupPhased(ctx context.Context, query string, cfg memoryprefetch.Config, h *prefetchHandle, notes *prefetchNotesSource, lookupStart time.Time) prefetchResult {
	var res prefetchResult
	path := config.LedgerPath()
	if strings.TrimSpace(path) == "" {
		res.Outcome = prefetchOutcomeError
		res.Segment = prefetchSegStat
		res.Err = fmt.Errorf("no memory ledger path resolves")
		return res
	}
	if _, err := os.Stat(path); err != nil {
		// A ledger that is not there yet is not a failure of the prefetch:
		// there is simply nothing distilled on this host (the same fact
		// memory(action=search) reports as its empty hint).
		res.Seg.StatMs = time.Since(lookupStart).Milliseconds()
		res.Outcome = prefetchOutcomeEmpty
		return res
	}
	res.Seg.StatMs = time.Since(lookupStart).Milliseconds()

	// The budget is armed HERE, after the cheap work, and covers the ledger
	// access only. Arming it above the stat (as the first version did) would let
	// a slow filesystem present itself as a slow query, which is exactly the
	// confusion the segments exist to remove.
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(prefetchGoroutineTimeoutMs)*time.Millisecond)
	defer cancel()

	// ONE handle per lookup when the caller brought none, ONE handle per session
	// when the executor did. The dial and the pragma readback are real work on a
	// 236MB WAL ledger, and they belong to whichever of the two shapes owns them:
	// the fresh shape pays them inside this call (and reports them in open_ms),
	// the session shape paid them once, in the goroutine that owns the handle.
	db := (*sql.DB)(nil)
	if h.usable() {
		db = h.usableDB()
		res.Seg.Handle = prefetchHandleSession
		// No dial and no pragma readback happen in THIS lookup — the executor
		// paid both when it built the handle. OpenMs is therefore 0 and the
		// handle's own cost is reported as the separate, session-scoped fact it
		// is, which keeps the segment sum equal to the measured ms.
		res.Seg.OpenMs = prefetchSegNoOpenMs
		res.Seg.HandleWarmMs = h.openMs
		res.Seg.JournalMode, res.Seg.QueryOnly, res.Seg.BusyTimeout = h.journalMode, h.queryOnly, h.busyTimeout
		if runCtx.Err() != nil {
			// A caller that arrives with an already-expired budget never reaches
			// the query, and saying "open" here would blame a dial that did not
			// happen. Checked BEFORE any work, so the attribution is the truth.
			res.Outcome, res.Segment = prefetchOutcomeGaveUp, prefetchSegQuery
			res.Err = fmt.Errorf("prefetch gave up after %dms (%s): %w", prefetchGoroutineTimeoutMs, res.Segment, runCtx.Err())
			return res
		}
	} else {
		// The dial runs under ITS OWN bound, not the lookup's 1500ms: the dial
		// does not queue behind a writer (measured: 1-31ms under load 11), while
		// the QUERY legitimately waits up to the busy timeout. Giving the dial
		// the lookup's whole budget meant a WEDGED ledger surfaced as "the
		// 1500ms expired" instead of as the lock refusal it is — the misdiagnosis
		// this task was filed about.
		dialCtx, dialCancel := context.WithTimeout(ctx, time.Duration(prefetchLedgerBusyTimeoutMs)*time.Millisecond)
		var fresh *prefetchHandle
		dialAttempts, err := prefetchRetryOnBusy(dialCtx, func() error {
			var e error
			fresh, e = prefetchOpenHandle(dialCtx, path)
			return e
		})
		dialCancel()
		res.Seg.Attempts = dialAttempts
		res.Seg.OpenMs = time.Since(lookupStart).Milliseconds() - res.Seg.StatMs
		if err != nil {
			res.Seg.Handle = prefetchHandleFresh
			// A LOCKED dial is a locked ledger: the refusal is the diagnosis, and
			// it is checked BEFORE the budget, exactly as the query's own
			// classification below is (the two phases report the same fact).
			switch {
			case isBusyErr(err):
				// The ledger is locked. This is the SAME fact a locked query
				// reports, so it is the same outcome and the same diagnosis —
				// only the segment differs, because only the segment did.
				res.Outcome, res.Segment = prefetchOutcomeGaveUp, prefetchSegOpen
				res.Err = fmt.Errorf("%w (%s)", err, prefetchSegOpen)
			case runCtx.Err() != nil:
				res.Outcome, res.Segment = prefetchOutcomeGaveUp, prefetchSegOpen
				res.Err = fmt.Errorf("prefetch gave up opening the ledger after %dms: %w", prefetchGoroutineTimeoutMs, runCtx.Err())
			default:
				res.Outcome, res.Segment = prefetchOutcomeError, prefetchSegOpen
				res.Err = err
			}
			return res
		}
		defer fresh.close()
		db = fresh.db
		res.Seg.Handle = prefetchHandleFresh
		res.Seg.JournalMode, res.Seg.QueryOnly, res.Seg.BusyTimeout = fresh.journalMode, fresh.queryOnly, fresh.busyTimeout
	}

	hits, candidates, attempts, waitCount, waitMs, err := prefetchQueryRetry(runCtx, db, query, prefetchHitLimit)
	res.Seg.QueryMs = time.Since(lookupStart).Milliseconds() - res.Seg.StatMs - res.Seg.OpenMs
	res.Seg.Attempts, res.Seg.WaitCount, res.Seg.WaitMs = attempts, waitCount, waitMs
	res.Candidates = candidates

	if err != nil {
		res.Segment = prefetchSegQuery
		switch {
		case isBusyErr(err):
			// A LOCKED ledger is a give-up, and it is the shape a keystroke path
			// meets in practice: the DSN's own busy timeout
			// (prefetchLedgerBusyTimeoutMs, 250ms — deliberately not
			// production's 15s) refuses instead of queueing, so the operator's
			// typing never waits behind a writer. It is only reported after
			// prefetchBusyRetryMax retries, each of which is bounded by the SAME
			// goroutine deadline — a retry never enlarges the budget.
			//
			// WHY THIS ARM IS FIRST. Both facts here are true simultaneously
			// when a locked ledger outlives the budget: the refusal AND the
			// expired deadline. Which one the record reports is therefore a
			// choice, and reporting the deadline is a MISDIAGNOSIS — it says
			// "the search was slow" about a ledger that was simply held, which
			// is precisely the error this task was filed on (measured 2026-10-07,
			// load ~15: the attribution flipped with the scheduling, half the
			// rounds naming `context deadline exceeded` for a ledger a writer
			// held EXCLUSIVE). The refusal is the more specific fact and it is
			// the one a reader can act on, so it wins; the budget is still
			// respected, because the give-up happened when it did either way.
			res.Outcome = prefetchOutcomeGaveUp
			res.Err = err
		case runCtx.Err() != nil:
			// The bound expired: GIVE UP. This is the epic's requirement, and it
			// is a RESULT (recorded as such), not a crash. The segment names
			// which part spent it, so the next reader does not have to guess
			// between a slow query and a slow dial.
			res.Outcome = prefetchOutcomeGaveUp
			res.Err = fmt.Errorf("prefetch gave up after %dms (%s): %w", prefetchGoroutineTimeoutMs, res.Segment, runCtx.Err())
		case isEmptyLedger(err):
			res.Outcome, res.Segment, res.Err = prefetchOutcomeEmpty, "", nil
		default:
			res.Outcome, res.Err = prefetchOutcomeError, err
		}
		return res
	}
	if len(hits) == 0 {
		res.Outcome = prefetchOutcomeEmpty
		return res
	}
	// Rows came back from the memory leg. THE SECTION IS NOT BUILT HERE: the note
	// leg contributes rows to the SAME section (chat_prefetch_notes.go) and the
	// injection floor is judged on the COMBINED set, so the render happens once —
	// in prefetchComposeSection, with both legs' hits in hand. What this phase
	// reports is the memory leg's own verdict; the caller keeps it as
	// MemoryOutcome before composition overwrites Outcome.
	res.MemoryHits, res.MemoryHitsCount, res.Outcome = hits, len(hits), prefetchOutcomeHits
	return res
}

// prefetchComposeSection renders the ONE section both legs contribute to, and
// decides the lookup's outcome from the COMBINED result.
//
// WHY COMPOSITION IS ITS OWN FUNCTION. With two legs there are three questions a
// reader must be able to answer from one record line, and each needs a different
// fact:
//
//	did the memory ledger answer?      res.MemoryOutcome (kept before this call)
//	did the note index answer?         res.Notes.Outcome
//	did anything reach the model?      res.Outcome / res.Section
//
// Folding the two legs into one `outcome` word would answer only the third and
// lose the two that say WHY — which is the whole reason the legs are parallel
// rather than merged in the store.
//
// HOW THE HITS ARE ORDERED. The memory leg's hits come first (it is the primary
// leg and its ranking is the ledger's own), and the note leg's follow. The merge
// is the ONLY thing this function does to them — the RENDER then goes through
// memoryprefetch.InjectSection, the single place a section is built (Rule 14),
// under the same rune budget, the same MaxSegmentHits cap, the same injection
// floor and the same named degradations the memory-only path has always used.
//
// WHY THE FLOOR CAN NOW BE REACHED BY THE NOTES ALONE. The floor
// (memoryprefetch's MinInjectHits) counts the hits the lookup RETURNED, and both
// legs' hits count: one memory hit plus one note hit is the two the floor asks
// for — which is exactly the case this epic exists for (the memory ledger knows
// nothing; the note index holds the answer). A leg below the floor on its own no
// longer reads as "nothing found" when the other leg found something.
//
// THE `below-floor` OUTCOME SURVIVES, judged on the combined count, so a lookup
// whose rows came back but fell short of the floor still says so rather than
// reporting `empty` (the distinction the memory-only record already made).
//
// NOTHING HERE CAN DEGRADE A LEG THAT WORKED. The note leg's degradations (an
// echo suppressed, an unreadable note row) ride into the section only when
// something is rendered, and the note leg's FAILURE is recorded on its own field
// group — so a note index that could not be read leaves the memory section
// byte-for-byte what it would have been.
func prefetchComposeSection(res prefetchResult, cfg memoryprefetch.Config) prefetchResult {
	hits := make([]memoryprefetch.Hit, 0, len(res.MemoryHits)+len(res.Notes.Hits))
	hits = append(hits, res.MemoryHits...)
	hits = append(hits, res.Notes.Hits...)
	res.CombinedHits = len(hits)

	// THE LEGS' OWN NAMED LOSSES RIDE INTO THE SECTION — all of them, in one
	// list, so what the model reads and what the record carries are the same
	// facts. The note leg's (an echo suppressed, a note row with no text) come
	// from its own result; the composition adds one of its own when the note
	// index could not be read at all AND the memory leg had nothing, because
	// that is the case where the block's silence would otherwise be
	// indistinguishable from a clean "no results found" (Rule 15).
	legDegs := append([]memoryprefetch.Degradation{}, res.Notes.Degradations...)
	if len(hits) == 0 && res.Outcome != prefetchOutcomeError && res.Outcome != prefetchOutcomeGaveUp &&
		(res.Notes.Err != nil || res.Notes.Outcome == prefetchNotesOutcomeFailed || res.Notes.Outcome == prefetchNotesOutcomeGaveUp) {
		legDegs = append(legDegs, memoryprefetch.DegradationNoteLegFailed(res.Notes.Err))
	}
	res.LegDegradations = legDegs

	if len(hits) == 0 {
		// Neither leg offered a row. The outcome is the memory leg's own verdict,
		// which already distinguishes error / gave-up / empty — the note leg's
		// verdict rides beside it (and, when it was the leg that failed, is the
		// only place that fact can be read).
		res.Hits = res.MemoryHitsCount
		return res
	}

	section, ok := memoryprefetch.InjectSection("", hits, cfg, legDegs...)
	if !ok {
		if len(hits) < memoryprefetch.InjectFloor(cfg) {
			// THE INJECTION FLOOR refused (internal/memoryprefetch's
			// MinInjectHits, asked here through InjectFloor so the reason and the
			// decision read the SAME number, Rule 14): the lookup FOUND rows —
			// from one leg or both — but fewer than the floor, so nothing was
			// rendered and nothing will be injected, not even the header, which is
			// why the count is judged before the render.
			res.Outcome, res.Hits = prefetchOutcomeBelowFloor, len(hits)
			return res
		}
		// At or above the floor and still no section: every candidate was too
		// thin to render as a hit (the rune trim). That is the pre-existing
		// `empty` case — the indexes answered, and none of it was a section.
		res.Outcome, res.Hits = prefetchOutcomeEmpty, len(hits)
		return res
	}
	res.Section, res.Outcome, res.Hits = section, prefetchOutcomeHits, len(hits)
	return res
}

// prefetchNotesLabel builds the record's note-leg block from a result, or nil
// when the leg NEVER RAN.
//
// THE NIL CASE IS NOT A ZERO VALUE (Rule 15a). A one-shot lookup — one with no
// note source at all — produces the zero prefetchNotesResult, whose Outcome is
// the empty string, and a recorder that emitted it would write a leg that was
// never asked as a leg with an empty verdict: a reader could not tell "no second
// leg exists on this path" from "the second leg answered with nothing". So the
// absence of a verdict is what makes the block absent, and the block's own
// outcome vocabulary (not-consulted / absent / empty / hits / failed / gave-up)
// is always NON-empty for a leg that ran.
func prefetchNotesLabel(n prefetchNotesResult) *prefetchNotesRecord {
	if strings.TrimSpace(n.Outcome) == "" {
		return nil
	}
	return &prefetchNotesRecord{
		Outcome:   n.Outcome,
		Tier:      n.Tier,
		HitsCount: n.HitsCount,
		Ms:        n.ElapsedMs,
		Err:       prefetchErrText(n.Err),
	}
}

// prefetchQueryRetry issues the search query, retrying a LOCKED ledger up to
// prefetchBusyRetryMax times while the caller's deadline still has room. It
// returns the attempt count and database/sql's pool-wait delta for the attempts
// it made, so the record can separate "SQLite was slow" from "this process
// serialised itself on its single pooled connection".
func prefetchQueryRetry(ctx context.Context, db *sql.DB, query string, limit int) ([]memoryprefetch.Hit, string, int, int64, int64, error) {
	before := db.Stats()
	var hits []memoryprefetch.Hit
	path := ""
	// skipLadder is the retry loop's MEMORY of the first attempt's refusal: the
	// ladder walk costs its own sub-budget before the lock is ever met, and
	// re-paying it on every retry turns a 3-attempt refusal into a 1500ms expiry
	// (see prefetchQueryShared). It is a per-lookup variable, not state on the
	// handle, because the next lookup on this handle may well run on an unlocked
	// ledger and must get the walk's recall back.
	skipLadder := false
	attempts, err := prefetchRetryOnBusy(ctx, func() error {
		var e error
		hits, path, e = prefetchQueryShared(ctx, db, query, limit, skipLadder)
		if e != nil && isBusyErr(e) {
			skipLadder = true
		}
		return e
	})
	after := db.Stats()
	return hits, path, attempts, after.WaitCount - before.WaitCount, (after.WaitDuration - before.WaitDuration).Milliseconds(), err
}

// prefetchRetryOnBusy runs fn, retrying while it reports a LOCKED ledger, and
// returns how many attempts it took. Each wait is bounded by the CALLER's
// deadline, so a retry can never extend the budget it is bounded by — the
// goroutine's bound stays the thing that ends a lookup against a wedged ledger.
//
// It exists because the lock is TRANSIENT and the two phases that can meet it
// must be treated the same: a held lock at the dial and a held lock at the query
// are the same fact (SQLITE_BUSY), and retrying only the second would make a
// session whose warm-up happened to land in a lock window dial fresh handles for
// its whole life.
func prefetchRetryOnBusy(ctx context.Context, fn func() error) (int, error) {
	var err error
	for attempts := 1; ; attempts++ {
		err = fn()
		if err == nil || !isBusyErr(err) || attempts > prefetchBusyRetryMax {
			return attempts, err
		}
		select {
		case <-ctx.Done():
			return attempts, err
		case <-time.After(time.Duration(prefetchBusyRetryDelayMs) * time.Millisecond):
		}
	}
}

// prefetchSearchSQL is the search shape, in ONE place: the FTS5 TOP-N pattern —
// rank and limit INSIDE the virtual table, then join only the winners to the
// document table.
//
// WHY THE NESTED FORM AND NOT `JOIN ... ORDER BY bm25(...) LIMIT ?` (measured,
// peerbox 2026-10-06, 8076 rows / 236MB / the operator's own ledger, load ~18):
// the flat form is also a TOP-N to SQLite's planner, and it cost 75-88ms on the
// query shapes the live chat actually typed against 40-55ms for this one,
// because its ORDER BY carries a SECOND key (d.confidence DESC) that defeats the
// FTS table's own rank ordering, so every matching row is materialised and
// sorted instead of the winners being taken off the top. Both forms agree on the
// top rows whenever the limit is smaller than the candidate set.
//
// THE SECONDARY KEY IS PRESERVED, because this query is the retrieval the model
// itself would have got: `rank` (FTS5's bm25) stays primary, carried out of the
// subquery as `rk`, and d.confidence DESC is applied on top of it. Both were
// MEASURED against the flat form on the operator's own ledger: with only
// `ORDER BY d.confidence DESC` outside, 2 of 3 real query shapes came back in a
// different order (ids [1526 1524 1523 …] became [1524 1523 1526 …]), which is
// an ANSWER CHANGE and not an optimisation. With `ORDER BY f.rk, d.confidence
// DESC` all three shapes matched the flat form id for id.
const prefetchSearchSQL = `SELECT d.id, d.topic, d.question, d.answer, COALESCE(d.tier,''), COALESCE(d.confidence,0)
FROM (SELECT rowid AS rid, rank AS rk FROM distilled_qa_fts WHERE distilled_qa_fts MATCH ? ORDER BY rank LIMIT ?) f
JOIN distilled_qa d ON d.id = f.rid
ORDER BY f.rk, d.confidence DESC`

// prefetchQueryShared runs the retrieval and reports WHICH recall path produced
// the candidate set (prefetchCandidatesLadder / prefetchCandidatesOR).
//
// THE CANDIDATE SET IS THE LADDER'S AND-SUBSET, with OR kept as the fallback —
// the two halves the epic asked for, in one function so the precedence is
// readable in one place:
//
//  1. tools.LadderMatch walks the SHARED ladder (the same memsearchSubsetPick
//     memory_search runs) on the caller's OWN read-only handle, under its OWN
//     sub-budget (prefetchLadderBudgetMs). ok=true means the index itself
//     qualified that subset (the ladder's COUNT is the authority), so the
//     expression is run as the candidate query.
//  2. When the ladder finds nothing in budget — query outside its token bounds,
//     no subset qualified, or the walk ran out of time — the OR shape below
//     runs, so precision is never bought with recall.
//
// WHY OR SURVIVES AT ALL, measured rather than assumed: on the operator's
// ledger the ladder's top hit for `ctx 81k 69k hit` is the record the query
// names, while OR's total is 681 rows — i.e. OR is not useless, it is a WIDE
// net that ranks by bm25 over everything sharing one token. It is the right
// answer exactly when the ladder has none, so it stays.
func prefetchQueryShared(ctx context.Context, db *sql.DB, query string, limit int, skipLadder bool) ([]memoryprefetch.Hit, string, error) {
	if limit <= 0 || limit > prefetchHitLimit {
		limit = prefetchHitLimit
	}
	match := ""
	path := ""
	if skipLadder {
		// A PREVIOUS attempt already spent the ladder's own sub-budget on this
		// same locked ledger and came back with nothing. The walk is not going
		// to answer differently while the writer still holds the ledger, so it is
		// skipped and the fallback below meets the lock once — MEASURED (this
		// task, load ~15, a writer holding EXCLUSIVE): every attempt cost
		// prefetchLadderBudgetMs (252ms) for the walk plus the DSN's busy timeout
		// (~250ms) for the fallback, i.e. ~505ms per attempt and 3 attempts only
		// because the goroutine's deadline ran out first. Skipping the walk is
		// what makes a locked ledger report its REFUSAL within the budget instead
		// of reporting an expiry that is really the retries' own cost. match
		// stays empty, so the OR fallback below runs and meets the lock itself.
	}
	if !skipLadder {
		ladderCtx, ladderCancel := context.WithTimeout(ctx, time.Duration(prefetchLadderBudgetMs)*time.Millisecond)
		ladderMatch, ok := tools.LadderMatch(ladderCtx, db, query)
		ladderCancel()
		if ok {
			match, path = ladderMatch, prefetchCandidatesLadder
		}
	}
	if match == "" {
		match = ftsMatchQuery(query)
		if match == "" {
			return nil, "", nil
		}
		path = prefetchCandidatesOR
	}
	hits, err := prefetchQueryRows(ctx, db, match, limit)
	if err != nil {
		return nil, path, err
	}
	// A ladder pick whose rows did not survive the join (the FTS count and the
	// joined row set are two different questions) falls back rather than
	// reporting an empty candidate set: the OR shape is the net for exactly
	// this, and an empty result on the keystroke path is the outcome that costs
	// the operator a turn's context. Measured as unreachable on the operator's
	// ledger — recorded as a guard, not as a path that is expected to run.
	if len(hits) == 0 && path == prefetchCandidatesLadder {
		orMatch := ftsMatchQuery(query)
		if orMatch != "" && orMatch != match {
			orHits, orErr := prefetchQueryRows(ctx, db, orMatch, limit)
			if orErr == nil {
				return orHits, prefetchCandidatesOR, nil
			}
		}
	}
	return hits, path, nil
}

// prefetchQueryRows runs prefetchSearchSQL for one MATCH expression.
func prefetchQueryRows(ctx context.Context, db *sql.DB, match string, limit int) ([]memoryprefetch.Hit, error) {
	rows, err := db.QueryContext(ctx, prefetchSearchSQL, match, limit)
	if err != nil {
		return nil, fmt.Errorf("prefetch query: %w", err)
	}
	defer rows.Close() //stylegate:ignore read-only cursor closed at the end of its iteration
	var out []memoryprefetch.Hit
	for rows.Next() {
		var h memoryprefetch.Hit
		if err := rows.Scan(&h.ID, &h.Topic, &h.Question, &h.Answer, &h.Tier, &h.Confidence); err != nil {
			return nil, fmt.Errorf("prefetch scan: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("prefetch rows: %w", err)
	}
	return out, nil
}

// ftsMatchQuery turns an extracted keyword list into an FTS5 MATCH expression:
// every keyword quoted (so a keyword containing punctuation cannot become FTS
// syntax) and OR-joined (the ranking already prefers a row covering more of the
// query, so an AND here would only lose recall).
func ftsMatchQuery(query string) string {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, `""`)
		quoted = append(quoted, `"`+f+`"`)
	}
	return strings.Join(quoted, " OR ")
}

// isBusyErr reports whether err is SQLite declining because the ledger is held
// by another connection. It delegates to tools.LedgerBusyErr — the ONE
// classifier of that fact (Rule 14), shared with the recall ladder, which must
// abandon its walk on exactly the errors this path retries. A second copy of
// the match here is how the two would come to disagree about a lock.
func isBusyErr(err error) bool { return tools.LedgerBusyErr(err) }

// isEmptyLedger reports whether err is the "there is nothing to search yet"
// shape (a missing FTS table on a freshly created ledger). It is a fact about
// the LEDGER, not about the prefetch: the caller records it as an empty result.
func isEmptyLedger(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such table") || strings.Contains(msg, "distilled_qa_fts")
}

// prefetchErrText renders an error for the record. Named with this file's own
// prefix because the package already carries an errText (cli_tasks.go) with a
// different job, and a second meaning for one name is exactly the drift a
// package-wide identifier is supposed to prevent.
func prefetchErrText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// boolLabel renders the record's adopted field.
func boolLabel(b bool) string {
	if b {
		return prefetchRecordAdoptedYes
	}
	return prefetchRecordAdoptedNo
}

// ---------------------------------------------------------------------------
// The adoption, as ONE function.
// ---------------------------------------------------------------------------

// chatPrefetchAdopt returns the outbound message this turn will send, with the
// prefetched section appended when there is one for THIS line.
//
// IT IS THE WHOLE ADOPTION CONTRACT, in one place, for two reasons: the REPL
// calls it (so there is exactly one path from a prefetched section to a model
// request), and a test calls it with the switch off to assert BYTE IDENTITY —
// a claim that has to be made against the real function, never against a copy
// of its logic.
//
// Display is NEVER touched: it is the operator's own line, and a background
// retrieval must not edit what the operator is shown. A nil executor (the OFF
// case) adds nothing at all, so the turn is byte-identical.
//
// Message IS NOT TOUCHED EITHER, and that is what this change IS: the hits ride
// out.Prefetch and enter the request as the model's OWN memory(action=search)
// pair (internal/agent/chat_prefetch_seed.go). Appending them to Message — what
// this function used to do — put them where no existing mechanism could reach
// them: with no tool_use, neither the feedback discipline nor the whitelist
// context slim can see the hits, so one the model judged useless was re-sent on
// every later round for the rest of the session. The section is still
// memoryprefetch's rendering, untouched (Rule 13/14).
//
// The join itself is memoryprefetch.InjectSection — the pure core owns the
// injected form, so the same block is produced whether it is driven by the
// executor or by a test with no ledger at all.
func chatPrefetchAdopt(out chatOutbound, p *chatPrefetch, line string) (chatOutbound, bool) {
	if p == nil {
		return out, false
	}
	seed, ok := p.take(line)
	if !ok {
		return out, false
	}
	out.Prefetch = seed
	return out, true
}

// prefetchSectionSeparator joins the operator's own message to the injected
// section: a blank line, so the model reads the section as a distinct block and
// not as a continuation of the request (the same separator the pure core's own
// InjectSection uses between its parts).
const prefetchSectionSeparator = "\n\n"

// chatPrefetchTurnText returns the message the NO-TOOLS chat path sends: the
// operator's own line, with the turn's retrieval appended as a text block.
//
// WHY THIS STILL EXISTS, and why it is not the shape the tool-enabled path uses
// (Rule 15: a limit is stated, not hidden). `gopi chat` without `--tools`
// advertises NO tools, so a memory(action=search) pair would be a tool_use the
// request never declared — the wire would carry a call the model has no way to
// make, and there is no tool round in which it could vote on the hits. On that
// path the retrieval can still be READ; it can never be ADJUDICATED, and a text
// suffix is the honest form of "readable, not votable". The tool-enabled path
// takes the pair (internal/agent/chat_prefetch_seed.go) precisely because there
// the vote — and therefore the whitelist slim — is reachable.
//
// The OFF case appends nothing, so the message stays byte-identical.
func chatPrefetchTurnText(out chatOutbound) string {
	if out.Prefetch.Section == "" {
		return out.Message
	}
	return out.Message + prefetchSectionSeparator + out.Prefetch.Section
}

// ---------------------------------------------------------------------------
// The editor seam: ONE line, and it adds no new editor API.
// ---------------------------------------------------------------------------

// chatPrefetchLineSource returns the function the executor samples for the
// in-progress line, or nil when this input layer has no such thing.
//
// It reads the editor's PUBLISHED snapshot (lineedit.Value.Text through
// Editor.Value), which the editor republishes on every mutation and which is
// documented as safe for a foreign reader (internal/lineedit/valueowner.go).
// That is why the prefetch needs no seam inside the read loop: a goroutine that
// samples this function cannot race the writer, and it cannot steal a keystroke
// (it never touches the Source — the consuming interface the seams doc rejects
// for exactly that reason).
//
// The cooked path (piped stdin, --no-editor, a terminal that could not enter
// raw mode) has no editor and returns nil: the prefetch is then armed but has
// nothing to watch, and it records nothing. That is the honest degradation —
// there is no "typed-so-far" state to read on a cooked reader.
func chatPrefetchLineSource(in *chatInput) func() string {
	if in == nil || in.editor == nil {
		return nil
	}
	return func() string { return in.editor.Value().Text() }
}
