package main

// chat_replay_witness.go — THE TURN-CLOSE WITNESS A RUN WITHOUT `--film` HAS.
//
// THE MEASURED DEFECT THIS FILE EXISTS FOR (2026-11-04). `gopi chat --replay
// SESSION --from-turn N` shortened NOTHING on an ordinary run:
//
//	$ time script -qec "gopi chat --replay d6120d04…"               → 13.789s
//	$ time script -qec "gopi chat --replay d6120d04… --from-turn 2"  → 13.798s
//	$ time script -qec "gopi chat --replay d6120d04… --from-turn 3"  → 13.807s
//
// and that was not the screen accumulating (the prefix's rows MUST still be
// painted — memory #17609/#17611). The prefix's one permitted shortcut,
// `prefixAdvanceIsWitnessed`, gated on `r.beats != nil`, and a beat driver is
// installed IFF `--film` was given: so on the ordinary path the flag's whole
// effect was ONE NOTICE LINE. The shortage was in the WITNESS, not in the design.
//
// SO THE WITNESS EXISTS WITHOUT BEATS. What `--film` gets from `finishTurn`'s
// `turn end` beat, an ordinary run gets from the REPL itself: the seam that
// commits the answer tail, settles the dump and closes the turn is the same
// moment, and this counter is bumped there. It is a COUNT, never a duration: the
// prefix advances because the turn CLOSED, not because a timer expired — which
// is the property the shortcut was always allowed to keep (memory #17611:
// 去掉的必须是计时器，不是判据).

import (
	"sync"
	"time"
)

// chatTurnWitness counts the turns this run's REPL has CLOSED — answer tail
// committed, screen final. The replay driver reads it to decide whether a prefix
// turn is behind it.
//
// NIL-SAFE BY CONTRACT: a session built without one reports 0 closed turns,
// which reads as "no witness" and leaves the plain settle wait in place. That is
// also what keeps the DEFAULT path — no `--from-turn` — untouched: nothing reads
// this counter unless fromTurn > 1.
type chatTurnWitness struct {
	mu     sync.Mutex
	closed int
}

// mark records that ONE more turn has closed. Called from the REPL's own
// turn-end seam, the one place that knows the turn's screen is final.
func (w *chatTurnWitness) mark() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.closed++
	w.mu.Unlock()
}

// count reports how many turns this run has closed.
func (w *chatTurnWitness) count() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// awaitTurnWitness waits, BOUNDED, for the witness to report turn `ordinal`
// closed. It is a CONDITION wait, never a sleep for a duration: what it returns
// true for is a fact the surface stated, not a timer that expired.
//
// THE BOUND IS THE PLAIN WAIT'S OWN (chatReplaySettleQuiescence): a witness that
// does not arrive within it has bought nothing, and the caller then takes the
// ordinary path it would have taken anyway — so a run whose surface never
// reports closure is no slower than before this file existed.
func (w *chatTurnWitness) awaitTurnWitness(ordinal int, timeout time.Duration) bool {
	if w == nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		if w.count() >= ordinal {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(chatReplayPollMS * time.Millisecond) //stylegate:allow-sleep the sleep is the observation cadence of the run's own turn-close counter, not a fixed wait for an external event
	}
}
