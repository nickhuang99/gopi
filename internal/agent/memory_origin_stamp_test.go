package agent

// The store stamp's SHAPE (epic 自动记忆 item 2/5). This is the regression
// guard for a defect that shipped once and was caught by the suite, not by
// review: the stamp was built field-by-field, so a run with no session log
// still got a round — a ROUND WITH NO SESSION, which the store correctly
// refuses. Every store in such a run then failed, and the harness's completion
// checkpoint answered the failure by forcing another round, so the run burned
// an extra iteration and the memory never landed.
//
// The rule the fix encodes, and what these pin: the stamp is SESSION-FIRST. No
// session ⇒ no round either, because a round is a cursor into a session and
// half a pointer is a claim nobody can follow.

import (
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// TestMemoryStoreOriginIsSessionFirst is the red-then-green guard: a run with no
// session log claims NOTHING — not a round, not a host — so the store takes the
// row instead of refusing it.
func TestMemoryStoreOriginIsSessionFirst(t *testing.T) {
	// The run is ON a real round (4) — that is exactly the trap: the round is
	// known and it is still not stampable without a session.
	a := &Agent{hippocampusRound: 4}
	origin := a.memoryStoreOrigin()

	if !origin.None() {
		t.Fatalf("a session-less run stamped %+v — a round with no session is the shape the store refuses", origin)
	}
	if !origin.Complete() {
		t.Fatalf("a session-less run's stamp is not storable (%+v): the store would refuse every memory write and the run would burn a round on the retry", origin)
	}
	if origin.SessionID != "" {
		t.Errorf("SessionID = %q with no session log, want \"\"", origin.SessionID)
	}
	if origin.Round != sessionevent.RoundIndexUnknown {
		t.Errorf("Round = %d with no session log, want the absence value %d", origin.Round, sessionevent.RoundIndexUnknown)
	}
}

// TestMemoryStoreOriginStampsBothWhenASessionExists: with a session log the
// stamp carries BOTH halves — the pointer is the whole point of the feature, so
// the session-first rule must not degrade into "session only".
func TestMemoryStoreOriginStampsBothWhenASessionExists(t *testing.T) {
	sl, err := NewSessionLoggerForRun("test-model", filepath.Join(t.TempDir(), "raw.log"), "stamp-probe-session", nil)
	if err != nil {
		t.Fatalf("session logger: %v", err)
	}
	defer sl.Close()

	a := &Agent{SessionLog: sl, hippocampusRound: 2}
	origin := a.memoryStoreOrigin()

	if origin.SessionID != "stamp-probe-session" {
		t.Errorf("SessionID = %q, want the run's own session id", origin.SessionID)
	}
	if origin.Round != 2 {
		t.Errorf("Round = %d, want the run's own round 2", origin.Round)
	}
	if !origin.Complete() {
		t.Errorf("a full pointer must be storable, got %+v", origin)
	}
	if origin.None() {
		t.Error("a full pointer reports as absent")
	}
}

// TestMemoryStoreOriginRoundWithNoRecordedRound: a session whose round was never
// stamped is an honest row — the session is claimed, the round stays absent —
// and the store must still take it.
func TestMemoryStoreOriginSessionWithoutARecordedRound(t *testing.T) {
	sl, err := NewSessionLoggerForRun("test-model", filepath.Join(t.TempDir(), "raw.log"), "stamp-probe-no-round", nil)
	if err != nil {
		t.Fatalf("session logger: %v", err)
	}
	defer sl.Close()

	a := &Agent{SessionLog: sl} // hippocampusRound left at its zero value
	origin := a.memoryStoreOrigin()

	if origin.SessionID != "stamp-probe-no-round" {
		t.Errorf("SessionID = %q, want the session that IS known", origin.SessionID)
	}
	// THE ROUND NOW COMES FROM THE SESSION, NOT FROM A PER-AGENT CURSOR that the
	// chat path silently leaves at zero (defect C, measured 2026-10-20): a
	// session that has not logged a request yet is at round ONE, which is the
	// same numbering the session log itself uses. (The note index no longer
	// shares this resolver: its number is a TURN, not a round —
	// memory_origin_stamp.go's own comment states why the two are separate.) The assertion below pins BOTH halves of that: never the
	// absence value once a session is known, and never a number the session has
	// no evidence for.
	if origin.Round != sessionevent.RoundIndexBase {
		t.Errorf("Round = %d with a known session and no request logged, want %d (the first round) — never the absence value %d for a run that HAS a session",
			origin.Round, sessionevent.RoundIndexBase, sessionevent.RoundIndexUnknown)
	}
	if !origin.Complete() {
		t.Errorf("a session without a round is an honest row, not a refusal: %+v", origin)
	}
}

// TestMemoryStoreOriginRoundCountsFromOne: the loop's round IS the session's own
// 1-based round — the same numbering the store records and the CLI takes — so no
// conversion happens at the stamp site. Round 1 must survive as 1, not become 0
// (which would be the absence value).
func TestMemoryStoreOriginRoundOneSurvives(t *testing.T) {
	sl, err := NewSessionLoggerForRun("test-model", filepath.Join(t.TempDir(), "raw.log"), "stamp-probe-round1", nil)
	if err != nil {
		t.Fatalf("session logger: %v", err)
	}
	defer sl.Close()

	a := &Agent{SessionLog: sl, hippocampusRound: 1}
	if got := a.memoryStoreOrigin().Round; got != 1 {
		t.Fatalf("round 1 stamped as %d — the first round must not collapse into the absence value %d",
			got, sessionevent.RoundIndexUnknown)
	}
}

// TestMemoryStoreOriginNeverSendsAHalfPointerToTheStore is the end-to-end half:
// whatever the stamp produces is fed to the store's own completeness rule, so a
// future edit that changes the stamp and the rule back into disagreement fails
// here rather than in a run that silently loses its memory.
func TestMemoryStoreOriginNeverSendsAHalfPointerToTheStore(t *testing.T) {
	rounds := []int{0, 1, 2, 7}
	for _, round := range rounds {
		sessionless := &Agent{hippocampusRound: round}
		if o := sessionless.memoryStoreOrigin(); !o.Complete() {
			t.Errorf("round %d with no session log produced a REFUSED stamp %+v", round, o)
		}
		// And the refusal rule still fires on the hand-built shape, so this
		// guard cannot pass by the rule being weakened to accept anything.
		if (tools.MemoryOrigin{Round: round}).Complete() && round != 0 {
			t.Errorf("the store's own rule accepted a bare round (%d) — the refusal was weakened", round)
		}
	}
}

// TestStoreRoundNumberComesFromTheSessionWhenTheChatCursorIsUnstamped is the
// regression for defect C (measured 2026-10-20): the chat surface stamps
// hippocampusRound only through stampHippocampusRound, which returns EARLY when
// a.Hippocampus is nil — and the host is nil whenever GOPI_HIPPOCAMPUS is off.
// The cursor therefore stays 0 for the whole conversation and every captured row
// was stamped round_index=0, the ABSENCE value, for runs that had dozens of
// rounds (the operator's ledger: session 2c394553-… holds 10 rows all at round 0
// while its own session log carries 52 `request` events).
//
// The case drives the REAL session logger and its REAL request counter, with the
// hippocampus host absent exactly as it is on this host (no host ⇒ no cursor).
func TestStoreRoundNumberComesFromTheSessionWhenTheChatCursorIsUnstamped(t *testing.T) {
	sl, err := NewSessionLoggerForRun("test-model", filepath.Join(t.TempDir(), "raw.log"), "round-c-probe", nil)
	if err != nil {
		t.Fatalf("session logger: %v", err)
	}
	defer sl.Close()

	a := &Agent{SessionLog: sl} // Hippocampus nil: the chat surface's ordinary shape
	if a.hippocampusRound != sessionevent.RoundIndexUnknown {
		t.Fatalf("the fixture needs the cursor UNSTAMPED, got %d", a.hippocampusRound)
	}
	if got := a.storeRoundNumber(); got != sessionevent.RoundIndexBase {
		t.Fatalf("with no request logged the round is %d, want %d (the first round)", got, sessionevent.RoundIndexBase)
	}

	// Two logged requests ⇒ the round in flight is the THIRD, and the stamp must
	// say so instead of 0.
	for i := 0; i < 2; i++ {
		if err := sl.LogRequest(i, 1, 0, 1, 1); err != nil {
			t.Fatalf("log request %d: %v", i, err)
		}
	}
	if got := a.storeRoundNumber(); got != 3 {
		t.Fatalf("after 2 logged requests the round is %d, want 3 — a chat row must name the round that produced it, not the absence value %d",
			got, sessionevent.RoundIndexUnknown)
	}

	// A STAMPED cursor still wins: the task loop derives the same number at the
	// same instant, and where it is set it is the authoritative reading.
	a.hippocampusRound = 7
	if got := a.storeRoundNumber(); got != 7 {
		t.Fatalf("a stamped cursor (7) was overridden by the session count (%d)", got)
	}
}
