package main

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/memoryprefetch"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
	_ "modernc.org/sqlite"
)

// The OFF/ON tests. They drive the REAL executor (chatPrefetchFor,
// chatPrefetchAdopt, prefetchLookup) — a test that re-implements the adoption
// would assert nothing about the binary (Rule 31).

// TestPrefetchOffStartsNoGoroutineAndLeavesThePromptByteIdentical is the epic's
// OFF contract, both halves: with the switch off nothing is started (the
// constructor returns nil, and nil's lifecycle methods are no-ops) and the
// outbound message is BYTE-identical to the one the boundary produced.
//
// The goroutine half is measured on a SIGNATURE OF THIS CODE (prefetchLoopFrame)
// rather than on the process's whole goroutine count, and it therefore needs no
// retry: another test in this binary can move runtime.NumGoroutine freely — that
// is exactly what made the old form flaky — but it cannot make a goroutine
// running (*chatPrefetch).loop appear or disappear.
func TestPrefetchOffStartsNoGoroutineAndLeavesThePromptByteIdentical(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, "")
	prefetchSettings(t, `{"memory_prefetch": false}`)

	in := chatOutbound{Message: "what does the memory prefetch epic do?", Display: "what does the memory prefetch epic do?"}

	// The OFF path's goroutine claim, measured on the quantity this test OWNS:
	// the goroutines running the executor's own loop (prefetchLoopFrame). There
	// is no settle window to wait out and no retry — with the switch OFF
	// chatPrefetchFor returns nil, so Start/Stop are no-ops and the loop
	// goroutine cannot exist; a signature that names it is deterministic where
	// the process-wide delta this replaced was not (see
	// prefetchGoroutineSettleGap for the measured failure of that form).
	p := chatPrefetchFor(t.TempDir(), func() string { return in.Message }, os.Stderr)
	if p != nil {
		t.Fatalf("chatPrefetchFor returned an executor with the switch OFF")
	}
	p.Start(context.Background()) // nil receiver: must not start anything
	p.Stop()
	if grew := prefetchGoroutineSettleGap(prefetchLoopFrame, 0, prefetchOffGoroutineSettleWait); grew > 0 {
		t.Errorf("the OFF path left %d goroutine(s) running the prefetch loop — OFF must start none", grew)
	}

	out, adopted := chatPrefetchAdopt(in, nil, in.Message)
	if adopted {
		t.Errorf("the OFF path adopted something")
	}
	if out.Message != in.Message || out.Display != in.Display {
		t.Errorf("the OFF path changed the message:\n got %q\nwant %q", out.Message, in.Message)
	}
}

// TestPrefetchOnStartsExactlyOneLoopGoroutineAndStopReclaimsIt is the ON half of
// the goroutine contract, and the reason the OFF half's marker is trustworthy:
// it proves prefetchLoopFrame RESOLVES to a live goroutine. A marker that
// matches nothing would satisfy "the OFF path left no loop goroutine" whether or
// not the feature exists — a passing green with nothing behind it.
//
// It is also the poll's second real client: Stop cancels the loop and waits for
// it, but the goroutine is still unwinding when Stop returns (it closes p.done
// in its own defer), so "reclaimed" is a bounded poll on the signature rather
// than an instantaneous read.
func TestPrefetchOnStartsExactlyOneLoopGoroutineAndStopReclaimsIt(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)
	prefetchSettings(t, `{"memory_prefetch": true}`)
	// A ledger path that does not exist: the line source returns "" so the loop
	// never claims a query, and this pin keeps even that door away from the
	// operator's own profile (the absent-ledger path costs no DB access).
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "absent.db"))

	p := chatPrefetchFor(t.TempDir(), func() string { return "" }, os.Stderr)
	if p == nil {
		t.Fatal("chatPrefetchFor returned nil with the switch ON")
	}
	if got := prefetchGoroutinesWithFrame(prefetchLoopFrame); got != 0 {
		p.Stop()
		t.Fatalf("the loop goroutine exists before Start (frame %q matched %d) — the signature cannot distinguish started from stopped", prefetchLoopFrame, got)
	}
	p.Start(context.Background())
	if got := prefetchAwaitFrameCount(prefetchLoopFrame, 1, prefetchOffGoroutineSettleWait); got != 1 {
		p.Stop()
		t.Fatalf("Start must run exactly ONE prefetch loop goroutine, but frame %q matched %d — a marker that resolves to nothing would make the OFF path's claim of absence vacuous", prefetchLoopFrame, got)
	}
	p.Stop()
	if got := prefetchGoroutineSettleGap(prefetchLoopFrame, 0, prefetchOffGoroutineSettleWait); got != 0 {
		t.Errorf("Stop returned with %d loop goroutine(s) still on the stack — the executor did not reclaim its own goroutine", got)
	}
}

// TestPrefetchAdoptAppendsToMessageNeverToDisplay pins WHERE the section may
// land: the model's input, never the operator's own line on screen.
// describe renders an outbound turn for a FAILURE MESSAGE, both halves: after
// the adoption contract changed there are two places a retrieval can sit (the
// message the no-tools path sends, and the Prefetch seed the agent turns into
// the model's own search pair), and a diagnostic that printed only one of them
// would report "the section did not ride" while it sat in the other.
func (o chatOutbound) describe() string {
	if o.Prefetch.Section == "" {
		return o.Message
	}
	return o.Message + "\n[prefetch " + o.Prefetch.Query + "]\n" + o.Prefetch.Section
}

func TestPrefetchAdoptSeedsTheTurnNeverDisplay(t *testing.T) {
	line := "prefetch the memory ledger read-only"
	dir := t.TempDir()
	// seedPrefetchLedger builds the fixture through the scaffold (Rule 32's
	// writer path, which pins GOPI_MEMORY_DB for the test) and this pin is the
	// belt to that scaffold's braces, in the same t.Setenv form (Rule 17).
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)

	p := chatPrefetchFor(dir, func() string { return line }, os.Stderr)
	if p == nil {
		t.Fatal("chatPrefetchFor returned nil with the switch ON")
	}
	defer p.Stop()

	query, ok := p.session.Claim(line, time.Now(), p.cfg)
	if !ok {
		t.Fatal("the state machine refused a line the gate should accept")
	}
	// Drive the REAL typing lookup, which is what records the LOOKUP event (what
	// was asked, what came back, how long it took) and stores the section for
	// adoption. It replaced a hand-rolled Claim+prefetchLookup+Use sequence whose
	// record assertion below passed VACUOUSLY: it looked for the `hits` KEY, and
	// the adoption line used to carry a fabricated `"hits":0`, so the string was
	// always present and no lookup ever needed to have run (measured 2026-10-07 —
	// the assertion went red only when the fabrication was removed).
	p.fetch(query)

	in := chatOutbound{Message: line, Display: line}
	out, adopted := chatPrefetchAdopt(in, p, line)
	if !adopted {
		t.Fatal("chatPrefetchAdopt did not adopt a section that is in hand")
	}
	// The retrieval rides the turn's SEED, and the message stays the operator's
	// own line: the section reaches the model as its own memory(action=search)
	// pair (internal/agent/chat_prefetch_seed.go), never as text glued onto the
	// request — a prefix on the request is exactly the shape that carries no
	// tool_use, so the model could not vote on the hits it received.
	if out.Message != line {
		t.Errorf("the message is no longer the operator's own line: %q", out.Message)
	}
	if !strings.Contains(out.Prefetch.Section, "memory") {
		t.Errorf("the section did not ride the turn as a seed:\n%s", out.describe())
	}
	if out.Prefetch.Query == "" {
		t.Error("the seed carries no query, so the model's seeded call has no argument to read back")
	}
	if out.Display != in.Display {
		t.Errorf("the DISPLAY was modified (it must stay the operator's own line): %q", out.Display)
	}

	// The record: the run's observable must exist in the session directory and
	// say what happened (query / hits / ms / outcome / adopted).
	rec := readPrefetchRecord(t, dir)
	if !strings.Contains(rec, prefetchRecordAdoptedYes) || !strings.Contains(rec, prefetchOutcomeHits) {
		t.Errorf("the record does not report the adoption:\n%s", rec)
	}
	if !p.recordHasQuery() {
		t.Errorf("the record carries no query line — the observable must name what was asked")
	}
}

// TestPrefetchGivesUpOnABlockedLedger is the ON-path's bounding requirement:
// with a writer holding an EXCLUSIVE lock, the prefetch GIVES UP within its own
// bound and says so. It builds no load (Rule 27 — a held lock is a state, not a
// stressor) and it does not wait for the ledger's own 15s busy timeout.
func TestPrefetchGivesUpOnABlockedLedger(t *testing.T) {
	path := openPrefetchLockRigLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)

	// A writer holding the write lock: rollback journal + BEGIN EXCLUSIVE is the
	// shape the seam measurement (devdoc/reports/memory-prefetch-seams.md §5)
	// used to prove the read-only DSN returns SQLITE_BUSY rather than blocking
	// forever. The rig's ledger is CREATED in that journal mode rather than
	// flipped into it — a flip needs exclusive access and races the fixture's own
	// pooled seed (measured: chat_prefetch_dsn_rig_test.go).
	assertLockRigLedgerIsInRollbackJournal(t, path)
	holder, err := sql.Open(prefetchDriverName, tools.LedgerRollbackJournalDSN(path))
	if err != nil {
		t.Fatalf("open the lock holder: %v", err)
	}
	defer holder.Close() //stylegate:ignore fixture handle closed at the end of the test
	holder.SetMaxOpenConns(1)
	if _, err := holder.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("BEGIN EXCLUSIVE: %v", err)
	}
	defer func() {
		if _, err := holder.Exec("ROLLBACK"); err != nil {
			t.Logf("rollback the fixture lock: %v", err)
		}
	}()

	started := time.Now()
	section, outcome, _, err := prefetchLookup(context.Background(), "prefetch memory ledger", memoryprefetch.DefaultConfig())
	elapsed := time.Since(started)
	if outcome != prefetchOutcomeGaveUp {
		t.Errorf("outcome = %q, want %q (err=%v)", outcome, prefetchOutcomeGaveUp, err)
	}
	if section != "" {
		t.Errorf("a section was produced from a locked ledger: %q", section)
	}
	if err == nil {
		t.Errorf("the give-up carries no reason — Rule 15: a silent give-up is indistinguishable from an empty result")
	}
	// The bound: the goroutine's own deadline (1500ms) plus scheduling slack,
	// and deliberately FAR below the ledger's own 15s busy timeout.
	if elapsed > 4*time.Second {
		t.Errorf("the prefetch took %v against a locked ledger — it is supposed to give up within its own bound", elapsed)
	}
}

// TestPrefetchOffWritesNoRecord is the OFF half of the observable: a run with
// the knob off must leave no prefetch record at all (a file that appears
// whether or not the feature ran is not an observation about the feature).
func TestPrefetchOffWritesNoRecord(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, "")
	// The default is ON (2026-10-04), so "the OFF path" now has to be SAID: an
	// empty settings file would arm the knob and this test would measure the ON
	// path while claiming the OFF one. The explicit false is the channel a user
	// turns it off with — `{"memory_prefetch": false}` — and that is the state
	// this test is about.
	prefetchSettings(t, `{"memory_prefetch": false}`)

	dir := t.TempDir()
	if p := chatPrefetchFor(dir, func() string { return "x" }, os.Stderr); p != nil {
		t.Fatal("the switch is OFF but an executor was built")
	}
	if _, err := os.Stat(filepath.Join(dir, config.MemoryPrefetchRecordName)); err == nil {
		t.Errorf("the OFF path created a record file")
	}
}

// seedPrefetchLedger builds a minimal distilled-qa ledger (base table + FTS +
// the two rows) with the WRITER scaffold's path (Rule 32), and returns its path.
//
// It is the DSN-agnostic half of the fixture: every rig that measures an ORDINARY
// reader's behaviour wants this one, on production's contract. A rig whose
// subject is the REFUSAL a held lock produces asks for its own journal mode
// through seedPrefetchLedgerInJournal instead — see chat_prefetch_dsn_rig_test.go
// for the measurement that makes the mode a property of the FILE rather than a
// post-hoc flip.
func seedPrefetchLedger(t *testing.T) string {
	t.Helper()
	return seedPrefetchLedgerThrough(t, func(path string) (*sql.DB, error) {
		return sql.Open(prefetchDriverName, tools.LedgerDSN(path))
	})
}

// seedPrefetchLedgerThrough is the ONE place the fixture's rows are written: it
// seeds them through whatever handle the caller's opener builds, so two rigs
// that differ only in their connection contract cannot drift apart in what they
// seed — and the opener stays a CALL to a tools DSN helper at the site that
// chooses it (the suite's contract gate reads that shape).
func seedPrefetchLedgerThrough(t *testing.T, open func(string) (*sql.DB, error)) string {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	db, err := open(path)
	if err != nil {
		t.Fatalf("open fixture ledger: %v", err)
	}
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		`CREATE TABLE distilled_qa (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			topic TEXT NOT NULL,
			question TEXT NOT NULL,
			answer TEXT NOT NULL,
			tier TEXT DEFAULT 'warm',
			confidence REAL DEFAULT 0.5,
			possibly_stale INTEGER DEFAULT 0)`,
		// contentless-external FTS5 over the base table, the shape internal/tools
		// builds (m1FTSVirtualTable): the prefetch query joins it by rowid.
		`CREATE VIRTUAL TABLE distilled_qa_fts USING fts5(topic, question, answer, content='distilled_qa', content_rowid='id')`,
		// TWO DISTILLED RECORDS PER SUBJECT, and the doubling is load-bearing
		// rather than padding: the injection floor (internal/memoryprefetch,
		// Config.MinInjectHits) refuses a retrieval of fewer than 2 hits
		// outright, so a fixture whose lookup lands on ONE row never reaches a
		// section at all, and the tests that read one would be measuring the
		// floor instead of the behaviour they name. The second record of each
		// pair answers the same question in its own words — the shape a ledger
		// really has after two lessons on one topic — so the pair is a SET, not
		// a duplicated row.
		`INSERT INTO distilled_qa (topic, question, answer, tier, confidence) VALUES
			('memory prefetch seam', 'where should a read-only prefetch attach?', 'read the published line snapshot and open the ledger read-only', 'hot', 0.9),
			('memory prefetch seam', 'where should a read-only prefetch attach?', 'the line snapshot is read first and the ledger is opened read-only', 'hot', 0.9),
			('read-only ledger DSN', 'how does a read-only open bound its wait?', 'busy_timeout bounds the wait and query_only refuses writes', 'warm', 0.7),
			('read-only ledger DSN', 'how does a read-only open bound its wait?', 'the wait is bounded by busy_timeout; query_only refuses writes', 'warm', 0.7),
			('prefetch adoption point', 'where does a prefetched block enter a model request?', 'the outbound boundary renders the section and the message stays untouched', 'warm', 0.7),
			('prefetch adoption point', 'where does a prefetched block enter a model request?', 'the section is rendered at the outbound boundary; the message stays untouched', 'warm', 0.7)`,
		`INSERT INTO distilled_qa_fts (rowid, topic, question, answer) SELECT id, topic, question, answer FROM distilled_qa`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed fixture (%s): %v", stmt, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture ledger: %v", err)
	}
	return path
}

// readPrefetchRecord reads the run's record file, failing the test when it is
// absent (an observable that was never written is the defect this asserts).
func readPrefetchRecord(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, config.MemoryPrefetchRecordName))
	if err != nil {
		t.Fatalf("read the prefetch record: %v", err)
	}
	return string(b)
}

// The goroutine claims are measured on their own numbers, named (Rule 1): how
// long the count is given to settle, how long the red-proof gives a genuinely
// live goroutine to be MISSED, and the dumps that back both.
const (
	// prefetchOffGoroutineSettleWait is the bound within which a count must
	// come back to its expected value — the replacement for a fixed sleep.
	prefetchOffGoroutineSettleWait = 2 * time.Second

	// prefetchOffGoroutineLeakProbe is the (shorter) bound the red-proof uses
	// when it KNOWS a goroutine is alive and requires the poll to MISS it.
	prefetchOffGoroutineLeakProbe = 200 * time.Millisecond

	// prefetchGoroutineAppearWait bounds how long a freshly spawned goroutine is
	// given to APPEAR in a stack dump. A goroutine that has not been scheduled
	// yet has no frames of its own to report, so an instantaneous "is it
	// visible" read loses a race with the scheduler — MEASURED (2026-10-02, this
	// file): with a spawn followed by ONE immediate runtime.Stack, the parked spy
	// was absent from 5 of 12 dumps. That is the same failure shape as the flake
	// this file repairs, one level down, so the presence check is a bounded poll
	// too and not a single read.
	prefetchGoroutineAppearWait = 2 * time.Second

	// prefetchGoroutineDumpBytes bounds one goroutine dump capture, so an
	// enormous goroutine population cannot turn the measurement into the
	// problem it is measuring. The frames under test sit at the TOP of their
	// goroutine's stack and this budget is hit by thousands of goroutines, not
	// by one deep stack.
	prefetchGoroutineDumpBytes = 1 << 20

	// prefetchLoopFrame identifies the executor's own loop goroutine inside a
	// stack dump (the goroutine `chatPrefetch.Start` starts), i.e. the corpus
	// the OFF path's "started none" claim is made against. It is a MARKER, so
	// TestPrefetchOnStartsExactlyOneLoopGoroutineAndStopReclaimsIt proves it
	// resolves to a live goroutine: a marker matching nothing would make "no
	// goroutine carries it" true whether or not the loop is running.
	prefetchLoopFrame = "cmd/agent.(*chatPrefetch).loop"

	// prefetchRunSpyFrame identifies the goroutine the red-proof parks and
	// releases. It names its own function (prefetchRunSpyGoroutine, spawned
	// nowhere else), so the signature the red-proof measures is a quantity that
	// test run owns outright.
	prefetchRunSpyFrame = "cmd/agent.prefetchRunSpyGoroutine"
)

// prefetchGoroutinesWithFrame returns how many live goroutines carry `frame`
// in their stack — a goroutine-count SIGNATURE of the code being measured.
//
// It is the attributed successor to `runtime.NumGoroutine() - baseline`: the
// whole-process count answers "how many goroutines does this binary have", which
// is a quantity the TEST DOES NOT OWN (a sibling case's database/sql
// connectionOpener, a test framework worker, a runtime sysmon all move it), while
// a frame-keyed count answers "how many goroutines is the thing under
// measurement running", which nothing else in the binary can move.
func prefetchGoroutinesWithFrame(frame string) int {
	buf := make([]byte, prefetchGoroutineDumpBytes)
	n := runtime.Stack(buf, true)
	count := 0
	for _, block := range strings.Split(string(buf[:n]), "\n\n") {
		if strings.Contains(block, frame) {
			count++
		}
	}
	return count
}

// prefetchGoroutineSettleGap waits, within `within`, for the number of live
// goroutines carrying `frame` to fall back to `want`, and returns how many are
// still above it (0 once it settled).
//
// WHY THE FRAME AND NOT A COUNT DELTA (measured 2026-10-02, the root cause of
// this test's 8/12 flake): the previous form compared
// `runtime.NumGoroutine()` against a baseline read before the goroutine under
// test was spawned. That is a DIFFERENCE OF TWO WHOLE-PROCESS COUNTS, and every
// goroutine in the binary — not just the one being watched — appears in it with
// either sign. Both directions were observed in one run: `database/sql`'s
// `(*DB).connectionOpener` goroutines exiting between the baseline read and the
// poll (measured: baseline=5, the two openers gone, the spy still parked →
// 5-1=4, gap<=0) and late-arriving ones keeping a stale baseline alive. In both
// cases the poll answered "settled" while the goroutine it was watching was
// demonstrably on the stack — a red-proof that had stopped proving anything.
//
// A frame-keyed subset cannot be cancelled by anything but its own members: the
// spy goroutine is the only goroutine in this binary running
// prefetchRunSpyGoroutine, and the executor's loop is the only one running
// (*chatPrefetch).loop.
//
// It yields to the scheduler with runtime.Gosched between reads, not
// time.Sleep: a fixed sleep asserts "this duration was enough", which is a guess
// about a scheduler this test does not own, while a bounded poll asserts the
// property and names the fact when it does not hold.
func prefetchGoroutineSettleGap(frame string, want int, within time.Duration) int {
	deadline := time.Now().Add(within)
	for {
		gap := prefetchGoroutinesWithFrame(frame) - want
		if gap <= 0 {
			return 0
		}
		if !time.Now().Before(deadline) {
			return gap
		}
		runtime.Gosched()
	}
}

// prefetchAwaitFrameCount polls until the frame signature reaches `want` and
// returns the last count it read — the PRESENCE mirror of
// prefetchGoroutineSettleGap, which reports how far ABOVE `want` the count still
// is when its bound expires.
//
// WHY BOTH ARE NEEDED: "the OFF path left no loop goroutine" is satisfied by a
// marker that resolves to nothing at all, so every claim of ABSENCE has to be
// paired with a case that proves the marker RESOLVES (the ON-path sibling). This
// helper is what makes that proof possible without an instantaneous read: it
// waits, within a named bound, for the goroutine to appear.
func prefetchAwaitFrameCount(frame string, want int, within time.Duration) int {
	deadline := time.Now().Add(within)
	for {
		got := prefetchGoroutinesWithFrame(frame)
		if got >= want || !time.Now().Before(deadline) {
			return got
		}
		runtime.Gosched()
	}
}

// prefetchRunSpyGoroutine is the goroutine the red-proof parks. It is a NAMED
// function (not a closure) on purpose: its frame is what prefetchRunSpyFrame
// keys on, so the red-proof's measurement has an identity nothing else shares.
func prefetchRunSpyGoroutine(release chan struct{}, exited chan struct{}) {
	defer close(exited)
	<-release
}

// TestPrefetchGoroutineSettleGapDetectsALiveLeak is the red-proof for the poll
// that replaced the fixed sleep (Rule 5): a poll that can only ever answer
// "settled" would be a deleted assertion wearing a passing green, so this test
// requires it to MISS a goroutine that is genuinely alive — and to come back to
// zero by itself once that goroutine exits.
//
// It spawns the goroutine it measures through a NAMED function, so the quantity
// the poll compares is this test run's own signature rather than the process's
// whole goroutine count — which is what made the previous form flaky (see
// prefetchGoroutineSettleGap).
func TestPrefetchGoroutineSettleGapDetectsALiveLeak(t *testing.T) {
	release := make(chan struct{})
	exited := make(chan struct{})
	go prefetchRunSpyGoroutine(release, exited)

	// PRECONDITION, not the claim: if the spy is not visible in the signature
	// the poll reads, the two assertions below would be measuring nothing at
	// all — a truncated dump, a rotted frame spelling or a not-yet-scheduled
	// goroutine must fail here, loudly, rather than make "settled" true for the
	// wrong reason. It is a bounded POLL (prefetchGoroutineAppearWait) for the
	// same reason the assertions are: one instantaneous read loses to the
	// scheduler.
	if got := prefetchAwaitFrameCount(prefetchRunSpyFrame, 1, prefetchGoroutineAppearWait); got < 1 {
		close(release)
		<-exited
		t.Fatalf("the parked spy goroutine is not visible in the frame signature %q — the measurement below would assert nothing", prefetchRunSpyFrame)
	}

	if gap := prefetchGoroutineSettleGap(prefetchRunSpyFrame, 0, prefetchOffGoroutineLeakProbe); gap <= 0 {
		t.Errorf("the poll reported a settled count while a goroutine was alive — it cannot detect the leak it exists for")
	}

	close(release)
	<-exited
	if gap := prefetchGoroutineSettleGap(prefetchRunSpyFrame, 0, prefetchOffGoroutineSettleWait); gap != 0 {
		t.Errorf("the poll never settled after the goroutine exited: gap=%d, want 0", gap)
	}
}

// TestPrefetchStopReportsARecordThatCannotBeClosed is the red-proof for Stop's
// CHECKED close (Rule 5): the failure branch is driven with a REAL close
// error, not a hypothetical one, and the report is read back off the very
// channel Stop writes it to.
//
// Measuring first was the point: os.File.Close is NOT idempotent — a second
// close returns `file already closed` (os.ErrClosed), which is exactly the
// shape of a run whose record was already flushed by another path. That is how
// this test makes the branch fire on demand instead of mocking a writer.
func TestPrefetchStopReportsARecordThatCannotBeClosed(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)
	prefetchSettings(t, `{"memory_prefetch": true}`)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe the executor's stderr: %v", err)
	}
	defer r.Close()

	p := chatPrefetchFor(t.TempDir(), func() string { return "x" }, w)
	if p == nil {
		t.Fatal("chatPrefetchFor returned nil with the switch ON")
	}
	if p.record == nil {
		t.Fatal("the ON path opened no record, so there is no close to check")
	}
	// Drive the failure: the record is already closed when Stop gets to it.
	if err := p.record.Close(); err != nil {
		t.Fatalf("pre-close the record: %v", err)
	}
	p.Stop()
	if err := w.Close(); err != nil {
		t.Fatalf("close the reporting end: %v", err)
	}

	reported, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read the executor's stderr: %v", err)
	}
	if !strings.Contains(string(reported), "did not close cleanly") {
		t.Errorf("Stop swallowed a failing record close — reported=%q", string(reported))
	}
}

// TestPrefetchStopIsSilentOnACleanClose is the other half of the same channel:
// the report must be a REPORT, not a per-run noise line — a clean close says
// nothing at all.
func TestPrefetchStopIsSilentOnACleanClose(t *testing.T) {
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)
	prefetchSettings(t, `{"memory_prefetch": true}`)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe the executor's stderr: %v", err)
	}
	defer r.Close()

	p := chatPrefetchFor(t.TempDir(), func() string { return "x" }, w)
	if p == nil {
		t.Fatal("chatPrefetchFor returned nil with the switch ON")
	}
	if p.record == nil {
		t.Fatal("the ON path opened no record to close")
	}
	p.Stop() // the record closes cleanly here
	if err := w.Close(); err != nil {
		t.Fatalf("close the reporting end: %v", err)
	}

	reported, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read the executor's stderr: %v", err)
	}
	if len(reported) != 0 {
		t.Errorf("a clean close reported something: %q", string(reported))
	}
}

// TestPrefetchWarmHandleFailureIsADisclosedFallbackNotASilentOne is the guard for
// the DECISION recorded at (*chatPrefetch).handle: when the session's read-only
// handle cannot be warmed, the executor falls back to a dial per lookup instead
// of refusing to retrieve anything — and that fallback is a Rule 15-compliant
// DISCLOSURE, not a silent failure.
//
// It pins the three halves the decision rests on, so that "the fallback is
// deliberate" cannot decay into "the failure is invisible":
//
//  1. the handle really is abandoned (no half-warmed handle is returned);
//  2. the stderr line NAMES the reason AND the ledger path — a reader can tell
//     which ledger failed without re-running anything;
//  3. the CONSEQUENCE lands in the record: the lookup reports handle="fresh",
//     which is the field a reader needs to know the dial was not amortised.
//
// The failure is produced by a STATE, not by load (Rule 27): the ledger path's
// parent directory does not exist, so the open cannot succeed. The real-world
// instance this decision was taken on is in this task's stream — the same warm
// open failing with `unable to open database file: out of memory (14)` while a
// scratch root was under pressure.
func TestPrefetchWarmHandleFailureIsADisclosedFallbackNotASilentOne(t *testing.T) {
	dir := t.TempDir()
	unopenable := filepath.Join(dir, "no-such-dir", config.MemoryDBFileName)
	t.Setenv("GOPI_MEMORY_DB", unopenable)
	t.Setenv(config.MemoryPrefetchEnv, config.EnvBoolOn)

	stderrPath := filepath.Join(dir, "stderr.txt")
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create the stderr capture: %v", err)
	}
	defer func() {
		if cerr := stderrFile.Close(); cerr != nil {
			t.Logf("close the stderr capture: %v", cerr)
		}
	}()

	p := chatPrefetchFor(dir, func() string { return "read-only ledger DSN" }, stderrFile)
	if p == nil {
		t.Fatal("chatPrefetchFor returned nil with the switch ON")
	}
	defer p.Stop()

	// (1) The session handle is ABANDONED rather than returned half-warmed.
	if h := p.handle(); h != nil {
		t.Fatalf("a handle was returned for an unopenable ledger path %s: %+v — the fallback "+
			"the decision names did not happen", unopenable, h)
	}

	// (2) Both facts a reader needs are on stderr: WHY, and WHICH ledger.
	reported, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("read the stderr capture: %v", err)
	}
	if !strings.Contains(string(reported), prefetchWarmFallbackReason) {
		t.Errorf("the warm-handle failure does not name the decision (%q) — a reader cannot tell "+
			"a deliberate fallback from a swallowed error; got %q", prefetchWarmFallbackReason, string(reported))
	}
	if !strings.Contains(string(reported), unopenable) {
		t.Errorf("the warm-handle failure does not name the ledger path %s — Rule 15's disclosure "+
			"is the path AND the reason; got %q", unopenable, string(reported))
	}

	// (3) The consequence is in the record's vocabulary. The fallback means every
	// lookup of the session dials its OWN handle, so the record must say
	// `handle: "fresh"` — the fact a reader needs to know the dial was not
	// amortised. Asserted against a SEEDED ledger, because the unopenable path
	// above never reaches a dial at all (it fails the stat segment first, which
	// is itself the honest attribution this file's segments exist for).
	seeded := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", seeded)
	res := prefetchLookupDetailed(context.Background(), "read-only ledger DSN", memoryprefetch.DefaultConfig(), nil, nil)
	if res.Seg.Handle != prefetchHandleFresh {
		t.Errorf("a handleless lookup reports handle=%q, want %q — the session-scale consequence of "+
			"the fallback would be invisible in the record", res.Seg.Handle, prefetchHandleFresh)
	}
}
