package tools

import (
	"testing"
	"time"
)

// The job-notice WAKE primitive's own guard (epic 01a0eeea, flagship 5). It
// pins the THREE properties the REPL's idle select depends on, and it is
// written against the primitive alone — no REPL, no agent — so a regression in
// the primitive fails HERE, by name, instead of showing up as an intermittent
// "the console never woke up" in a pty case.

// TestJobNoticeWake_LevelAndEdgeAgreeWithTheInbox pins the level/edge pair the
// reader trusts: the edge fires when a notice is appended, and the level says
// whether a take would return something. The two must never disagree — a turn
// minted from an edge with a false level is a turn with nothing in it.
func TestJobNoticeWake_LevelAndEdgeAgreeWithTheInbox(t *testing.T) {
	cleanupJobs(t)
	TakeJobNotices()
	drainJobNoticeWake()

	if JobNoticePending() {
		t.Fatal("the level is true on a drained inbox — the reader would mint an empty turn")
	}

	out, err := JobStart("echo wake-level")
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	jobID := extractJobID(t, out)

	// THE EDGE: the appending goroutine signals the wake. The wait is bounded
	// and it is on the production signal, not on a sleep.
	select {
	case <-JobNoticeWake():
	case <-time.After(jobNoticeWakeTestTimeout):
		t.Fatal("no wake after the job finished — a parked reader would sleep through the notice instead of opening a turn")
	}
	// THE LEVEL, consulted exactly as the reader consults it after a wake.
	if !JobNoticePending() {
		t.Fatal("the wake fired but the level is false — the reader would treat a real notice as a stale token and wait forever")
	}

	// THE PEEK: it reports what the turn is being opened FOR, and it must not
	// drain — draining here is what would make the turn's round boundary inject
	// nothing (the one take point is internal/agent/chat_ops.go).
	peeked := PeekJobNotices()
	if len(peeked) != 1 || peeked[0].JobID != jobID {
		t.Fatalf("PeekJobNotices = %+v, want exactly the notice for %s", peeked, jobID)
	}
	if !JobNoticePending() {
		t.Fatal("PeekJobNotices DRAINED the inbox — the round boundary's own take would then inject nothing (the take point must stay unique)")
	}
	if got := TakeJobNotices(); len(got) != 1 || got[0].JobID != jobID {
		t.Fatalf("TakeJobNotices after a peek = %+v, want the same notice the peek described", got)
	}
	if JobNoticePending() {
		t.Fatal("the level is still true after the take — the reader would mint a turn for a notice nobody has")
	}
}

// TestJobNoticeWake_ProducesWithNoReaderAndNeverBlocks pins the producer's
// contract: the reaper signals from a goroutine that must never park, and there
// may be NO reader at all (a chat session that never asks). A blocking send
// would deadlock the reaper; an unbounded buffer would grow per completed job.
// So a second, third and tenth job after an unconsumed token must all return —
// and ONE token must still be pending at the end, because the level (not the
// token) is what carries the meaning.
func TestJobNoticeWake_ProducesWithNoReaderAndNeverBlocks(t *testing.T) {
	cleanupJobs(t)
	TakeJobNotices()

	for i := 0; i < jobNoticeWakeTestProducers; i++ {
		if _, err := JobStart("echo wake-producer"); err != nil {
			t.Fatalf("JobStart %d: %v", i, err)
		}
	}
	// No reader ever selected. The producer loop above must have completed (it
	// did — this line is reachable), which is the whole assertion: a blocking
	// send of the second token would have parked a reaper goroutine here.
	select {
	case <-JobNoticeWake():
	case <-time.After(jobNoticeWakeTestTimeout):
		t.Fatal("no token pending after the producers — the wake was lost, so the reader would wait for a notice that is already in the inbox")
	}
}

// drainJobNoticeWake empties the wake channel so a case starts from a known
// edge state. The channel is package-global by design (one inbox, one edge), so
// a case that left a token behind would otherwise hand it to the next one.
func drainJobNoticeWake() {
	for {
		select {
		case <-jobNoticeWake:
		default:
			return
		}
	}
}

const (
	// jobNoticeWakeTestTimeout bounds every wait in this file. It is a TEST
	// deadline for a signal that must arrive on its own — never a production
	// timer, and never a load generator (Rule 27): the producer is one `echo`.
	jobNoticeWakeTestTimeout = 10 * time.Second
	// jobNoticeWakeTestProducers is how many jobs are started with no reader
	// attached, so the non-blocking send is exercised on a FULL buffer rather
	// than only on the first token.
	jobNoticeWakeTestProducers = 3
)
