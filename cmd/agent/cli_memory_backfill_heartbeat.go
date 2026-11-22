package main

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/tools"
)

// memoryBackfillHeartbeatEvery is the MAXIMUM WALL-CLOCK GAP between two
// liveness lines of a token backfill, and it is deliberately a TIME, not a
// number of batches.
//
// WHY TIME AND NOT A BATCH STRIDE (measured 2026-11-21, task 01a1fc80). The
// printer used to emit a line every `tools.JiebaBackfillProgressEvery` batches,
// i.e. every 5000 rows. On the operator's 32743-row ledger that put the FIRST
// line 62 seconds after start and the whole run finished in 99 seconds: for the
// first minute of a 99-second command the console said nothing at all, and a
// user cannot tell "still working" from "already dead" from silence. A batch
// stride also couples the gap to the BATCH SPEED — make one batch slow and the
// next line recedes with it, which is exactly the wrong direction: the slower
// the work, the longer the silence.
//
// So the interval is wall clock, and the deadline it is chosen against is the
// supervisor's idle-output window: a run that prints at least once every few
// seconds is never killed for being quiet, whatever the batches are doing.
const memoryBackfillHeartbeatEvery = 5 * time.Second

// memoryBackfillHeartbeat renders the t=0 announcement and then a liveness line
// on a WALL CLOCK, independent of whether any batch has finished.
//
// WHY IT OWNS ITS OWN GOROUTINE. The progress callback of the tools layer fires
// once per completed batch, so a printer built on it can only ever speak when a
// batch ENDS. That leaves the two cases this type exists for completely silent:
// the per-batch work that takes longer than the interval (one slow page = no
// line, which is where a 5h39m silent process was observed), and the start-up
// phase before the first page returns. A ticker goroutine observes neither
// event — it has its own clock — so it speaks in both.
//
// WHAT IT MAY NOT SAY (Rule 15): no percentage, no ETA. The scan does not know
// its own throughput in advance (the first batch is also the segmenter's
// warm-up), and a fabricated estimate is worse than no line. It reports only
// what it measured: elapsed wall time, rows already read, batches already
// committed, and the id the cursor stands at.
type memoryBackfillHeartbeat struct {
	verb  string
	out   io.Writer
	every time.Duration
	now   func() time.Time

	mu      sync.Mutex
	started time.Time
	latest  tools.JiebaBackfillProgress
	seen    bool
	ticks   int

	halted chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
}

// newMemoryBackfillHeartbeat builds a heartbeat whose interval and clock are
// INJECTED. Production passes memoryBackfillHeartbeatEvery and time.Now; a test
// passes a compressed interval so it can assert on the RULE (a line per
// period, whatever the batches are doing) instead of waiting real minutes.
func newMemoryBackfillHeartbeat(verb string, out io.Writer, every time.Duration, now func() time.Time) *memoryBackfillHeartbeat {
	if now == nil {
		now = time.Now
	}
	return &memoryBackfillHeartbeat{
		verb:   verb,
		out:    out,
		every:  every,
		now:    now,
		halted: make(chan struct{}),
	}
}

// announce is the t=0 line: it exists so the FIRST output of the command is
// emitted before any batch has run, and it states what the scan is about to
// traverse (the ledger's own row count and id extent) plus the batch size, so a
// reader can size the work even though nothing has been scanned yet.
//
// spanErr is reported INSIDE the line rather than aborting the run (Rule 15: an
// unmeasurable extent is said out loud, not swallowed). The scan itself is
// unaffected by whether the count could be taken, so the run continues and the
// reader is told why the numbers are absent.
func (h *memoryBackfillHeartbeat) announce(ledger string, span tools.JiebaBackfillExtent, spanErr error) {
	h.mu.Lock()
	h.started = h.now()
	h.mu.Unlock()
	if spanErr != nil {
		fmt.Fprintf(h.out, "memory %s: start — ledger %s, extent unreadable (%v); batch %d, cursor starts at id 0\n",
			h.verb, ledger, spanErr, tools.JiebaBackfillBatch)
		return
	}
	// "already-current rows are skipped" is the 01a1fae2 half: the cursor always
	// starts at id 0, but the scan is idempotent over the DATA, so an
	// interrupted run continues where the data left off — the announcement
	// therefore names the id window it traverses instead of pretending to
	// resume at a cursor the interrupted process never persisted (it persists
	// none, by design: Rule 35, no state file).
	fmt.Fprintf(h.out, "memory %s: start — %d row(s) in %s (ids %d..%d), batch %d, cursor starts at id 0; rows already current are skipped, so an interrupted run continues from the data\n",
		h.verb, span.Rows, ledger, span.MinID, span.MaxID, tools.JiebaBackfillBatch)
}

// observe is the tools-layer callback: it RECORDS the newest measurement and
// prints nothing. Printing here would restore the batch-speed coupling this
// type removes — a slow page would silence the console for as long as it runs.
func (h *memoryBackfillHeartbeat) observe(p tools.JiebaBackfillProgress) {
	h.mu.Lock()
	h.latest = p
	h.seen = true
	h.mu.Unlock()
}

// start launches the ticker goroutine. It is safe to call once; a second call
// would double every line, so it is not offered as a no-op.
func (h *memoryBackfillHeartbeat) start() {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		tick := time.NewTicker(h.every)
		defer tick.Stop()
		for {
			select {
			case <-h.halted:
				return
			case <-tick.C:
				h.line()
			}
		}
	}()
}

// line prints ONE liveness line from the newest recorded measurement. It never
// blocks the scan on anything but the mutex, and it composes its own sentence
// for the "no batch has finished yet" state instead of staying quiet.
func (h *memoryBackfillHeartbeat) line() {
	h.mu.Lock()
	h.ticks++
	n := h.ticks
	elapsed := h.now().Sub(h.started)
	p, seen := h.latest, h.seen
	h.mu.Unlock()

	if !seen {
		fmt.Fprintf(h.out, "memory %s: alive — no batch has completed yet, elapsed %s\n",
			h.verb, elapsed.Round(time.Second))
		return
	}
	fmt.Fprintf(h.out, "memory %s: alive — %d row(s) scanned, %d re-derived, %d batch(es), at id %d, elapsed %s (tick %d)\n",
		h.verb, p.Scanned, p.Changed, p.Batches, p.LastID, elapsed.Round(time.Second), n)
}

// halt stops the ticker and WAITS for an in-flight line to be written. Without
// the Wait a tick already selected from the channel could land after the final
// numbering, mixing a stale "alive" line into the summary that follows it.
func (h *memoryBackfillHeartbeat) halt() {
	h.once.Do(func() { close(h.halted) })
	h.wg.Wait()
}

// closeout prints the FINAL progress line after halt, so the last thing a
// reader sees before the summary is a complete measurement rather than a tick.
// An empty ledger never produced an observation, and that is stated as zeros
// rather than skipped (a run that scanned nothing must be distinguishable from
// a run that never started).
func (h *memoryBackfillHeartbeat) closeout() {
	h.halt()
	h.mu.Lock()
	p, seen := h.latest, h.seen
	elapsed := h.now().Sub(h.started)
	h.mu.Unlock()
	if !seen {
		fmt.Fprintf(h.out, "memory %s: progress — 0 row(s) scanned, 0 re-derived, 0 batch(es), at id 0, elapsed %s\n",
			h.verb, elapsed.Round(time.Second))
		return
	}
	fmt.Fprintf(h.out, "memory %s: progress — %d row(s) scanned, %d re-derived, %d batch(es), at id %d, elapsed %s\n",
		h.verb, p.Scanned, p.Changed, p.Batches, p.LastID, elapsed.Round(time.Second))
}
