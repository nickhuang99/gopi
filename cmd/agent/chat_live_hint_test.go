package main

import (
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// chat_live_hint_test.go — Defect A: the status row's live hint during a turn.
//
// The operator's report: after a message was submitted the row kept reading the
// IDLE value (`ready`) while the turn was in flight, so nothing on screen said
// "sent / thinking / responding" and the only evidence the turn had finished was
// the answer appearing out of nowhere.
//
// The assertions are the OPERATOR-VISIBLE ones (Rule 10.2 — the cells the read
// loop painted onto the reserved status row, replayed through the grid): the
// hint must CHANGE through the turn's real phases (sent → thinking → responding
// → ready), and each in-flight phase must be painted MORE THAN ONCE, because a
// state painted exactly once is the defect: the row then sits on that one value
// for the whole wait, and a wake that is lost leaves it on the previous one.
//
// The phases are driven by the SHIPPED functions the turn loop calls
// (chatTurnStatus.begin/setPhase/end), never by a status the test composes
// itself: what is under test is the wiring, not arithmetic the test could write
// by hand.

// livePhase is one state of a turn: the name the failure message uses, the phase
// value the wiring moves to, and the word the ROW must show for it.
type livePhase struct {
	name  string
	phase chatTurnPhase
	word  string
}

// livePhases is the state sequence a turn walks, in the order the operator sees
// it: the request is issued, the model reasons, the model answers, the prompt
// comes back. Named (Rule 1) because the test asserts the ORDER.
var livePhases = []livePhase{
	{"sent", chatTurnSent, statusLiveWordSent},
	{"thinking", chatTurnThinking, statusLiveWordThinking},
	{"responding", chatTurnResponding, statusLiveWordResponding},
}

// liveHintDrawsPerPhase is how many times one in-flight phase must reach the row
// before the next one starts: the state's own draw plus at least one keepalive
// frame behind it. Two is the smallest number that distinguishes "the row was
// refreshed while the turn ran" from "the row was painted once and then went
// silent" — which is the defect.
const liveHintDrawsPerPhase = 2

// iterCountForTest is the completed-turn count the phases publish under: the
// turn in flight is iterCount+1, and the label's iter field renders the
// completed count (see turnLabel), so a fixed value is what the row assertions
// need.
const iterCountForTest = 1

// liveTestTick is the keepalive period the test drives the source at. It is a
// test value, not a policy: the production period (statusLiveKeepalive) is
// what a run uses, and a test that slept it would spend seconds per phase.
const liveTestTick = 10 * time.Millisecond

// liveIdleQuietSteps is how many polls the idle window covers after the prompt
// has returned: several times the test period, so an animation that outlived its
// turn is seen rather than merely suspected.
const liveIdleQuietSteps = 20

// newLiveHintHarness builds the chat input layer with the TURN status source
// installed exactly as the REPL installs it (chatTurnStatus over the counters,
// published through the editor's own wake seam).
func newLiveHintHarness(t *testing.T) (*streamHarness, *chatStatusSource, *chatTurnStatus) {
	t.Helper()
	h := newStreamHarness(t)
	counters := newChatStatusSource("deepseek-flash", false)
	turn := newChatTurnStatus(counters, h.in.PublishStatus)
	turn.liveTick = liveTestTick
	h.in.SetStatus(turn.status)
	if !h.in.StatusNotifyEnabled() {
		t.Fatal("the harness's source supplied no wake seam: a publish could never reach the read loop, and every wait below would fail for the wrong reason")
	}
	return h, counters, turn
}

// TestChatLiveHintWalksTheTurnStates is the defect's regression guard: the row
// walks sent → thinking → responding → ready, each in-flight state being
// REPAINTED while it lasts.
func TestChatLiveHintWalksTheTurnStates(t *testing.T) {
	h, counters, turn := newLiveHintHarness(t)
	band := h.in.Answer()
	if band == nil {
		t.Fatal("no answer band on a terminal that hosts the layout")
	}

	// At the prompt the hint is the idle value.
	var sentRow string
	turn.publish(counters, 0, statusLiveIdleTurn, 0)
	h.waitHint(t, statusLiveIdle)

	for _, ph := range livePhases {
		// The phase begins on the real event the loop answers to, and the
		// renderer receives the delta the event carried.
		switch ph.phase {
		case chatTurnSent:
			turn.begin(iterCountForTest)
		case chatTurnThinking:
			turn.setPhase(ph.phase)
		case chatTurnResponding:
			turn.setPhase(ph.phase)
		}
		h.waitPhase(t, ph)
		h.waitDraws(t, band, liveHintDrawsPerPhase, ph)
		if !h.stateOnRow(ph.word) {
			t.Fatalf("after %s the row's live hint = %q, want it to end with %q\nrow: %q", ph.name, h.stateText(), ph.word, h.statusRow())
		}
		// The states are not carried by words the test could not tell apart: the
		// ROWS differ, which is the whole operator-visible claim.
		if ph.phase != chatTurnSent && h.statusRow() == sentRow {
			t.Fatalf("the %s row is identical to the sent row — the states are indistinguishable on screen\nrow: %q", ph.name, h.statusRow())
		}
		if ph.phase == chatTurnSent {
			sentRow = h.statusRow()
		}
	}

	// The prompt comes back: the row returns to the idle value — exactly the way
	// clearTool drops the tool row, so no stale "working" indicator outlives the
	// turn it described. The REPL's path is this ONE call (end the in-flight
	// state + apply the completed counters + repaint), so the assertion below is
	// about the path the loop actually takes.
	turn.publish(counters, iterCountForTest, statusLiveIdleTurn, 0)
	h.waitHint(t, statusLiveIdle)
	// And the animation stops with it: a keepalive still ticking after the turn
	// would repaint the row forever, which is a repaint storm rather than an
	// assurance.
	before := band.StatusWrites()
	for i := 0; i < liveIdleQuietSteps; i++ {
		h.replay()
		time.Sleep(liveTestTick) //stylegate:allow-sleep a bounded window in which a keepalive that outlived the turn would repaint
	}
	if after := band.StatusWrites(); after != before {
		t.Fatalf("the prompt returned and the row was repainted %d more time(s): the keepalive outlived the turn", after-before)
	}
}

// TestChatTurnPhasesWriteNothingFromTheCaller is the single-writer invariant at
// the REPL's own seam: with the editor's read loop STOPPED, the turn's own calls
// (the in-flight publish, the phase changes, the keepalive's frames) must write
// ZERO bytes. A caller that wrote here interleaves its cursor placement with the
// read loop's repaint — the two-writer defect the publish path exists to remove.
func TestChatTurnPhasesWriteNothingFromTheCaller(t *testing.T) {
	h, counters, turn := newLiveHintHarness(t)
	if !h.stopReadLoop() {
		t.Fatal("the editor's read loop did not return: the caller's writes cannot be attributed")
	}
	h.out.reset()

	turn.begin(iterCountForTest)
	for _, ph := range livePhases {
		turn.setPhase(ph.phase)
	}
	// The keepalive's frames are published too — let several ticks pass.
	time.Sleep(liveTestTick * liveIdleQuietSteps) //stylegate:allow-sleep a bounded window covering several keepalive ticks

	if got := h.out.count(); got != 0 {
		t.Fatalf("the turn's own status calls wrote %d chunk(s) with the read loop stopped — the caller is a second writer on the terminal fd (want 0)", got)
	}
	if !h.in.Answer().StatusPublishPending() {
		t.Fatal("the publish left no pending flag: the read loop would never draw the phase")
	}
	turn.publish(counters, 0, statusLiveIdleTurn, 0)
	turn.end()
}

// ── instruments ─────────────────────────────────────────────────────────────

// statusRow is the row the LAYOUT reserved for the status, replayed from the
// bytes the read loop wrote. It is read off the GRID rather than from a
// callback: the operator's evidence is the screen, so the assertion is the
// screen's.
func (h *streamHarness) statusRow() string {
	h.replay()
	return h.screen.Line(h.layout().StatusRow)
}

// stateText is the row's state as the operator reads it: the row's tail with its
// padding and the keepalive's animation cells removed. The live hint is the
// row's LAST field (every other field is a prefix; see chatStatusSource.status),
// so the tail IS the hint, and a substring test would accept a state word that
// merely occurs inside the tool column's digest.
func (h *streamHarness) stateText() string {
	// The column's PADDING is ASCII spaces and comes off first; the state's own
	// field gap (statusLiveInFlightGap) is a no-break space and is part of the
	// state, so it is NOT trimmed with them. The animation dots come off last —
	// they sit BEHIND the gap, so trimming the gap first would hide them.
	row := strings.TrimRight(h.statusRow(), " ")
	return strings.TrimRight(row, statusLiveAnimationChar)
}

// stateOnRow reports whether the row's live hint IS want. It is a SUFFIX test
// (not a substring one) for the reason above: the hint is the row's last field.
func (h *streamHarness) stateOnRow(want string) bool {
	return strings.HasSuffix(h.stateText(), want)
}

// waitHint waits until the reserved status row's live hint is want.
func (h *streamHarness) waitHint(t *testing.T, want string) {
	t.Helper()
	h.waitFor(t, "the status row to show the hint "+want, func() bool { return h.stateOnRow(want) })
}

// waitPhase waits until the row shows the phase's state word.
func (h *streamHarness) waitPhase(t *testing.T, ph livePhase) {
	t.Helper()
	h.waitFor(t, "the status row to show the "+ph.name+" state", func() bool { return h.stateOnRow(ph.word) })
}

// waitDraws waits until the answer renderer has drawn the status row at least n
// times since the phase began. The counter is the renderer's own write count for
// the row: a draw whose composed text did not change is deduplicated away and
// does not count, which is why the keepalive's frames must differ.
func (h *streamHarness) waitDraws(t *testing.T, band *lineedit.AnswerRenderer, n int, ph livePhase) {
	t.Helper()
	draws := band.StatusWrites()
	h.waitFor(t, "the status row to be repainted for the "+ph.name+" phase", func() bool {
		return band.StatusWrites() >= draws+n
	})
}

// TestStatusLiveWordsFitTheirColumn is the geometry tie between the constants
// (Rule 1): the reserved live column is a FIXED width, so a state word (plus its
// animation, plus the widest suffix a state can carry — a queued note) that
// outgrew it would silently push the row's other columns sideways. The check is
// the renderer's own idea of width, never len(): the gap is a multi-byte, one-CELL
// character.
func TestStatusLiveWordsFitTheirColumn(t *testing.T) {
	words := map[string]string{
		"sent":       statusLiveWordSent,
		"thinking":   statusLiveWordThinking,
		"responding": statusLiveWordResponding,
		"idle":       statusLiveIdle,
	}
	for name, word := range words {
		if w := displayCellsForTest(word); w > statusLiveCol {
			t.Errorf("the %s state word %q is %d cell(s) wide, over the reserved live column's %d — the row's later columns would move when the state changes",
				name, word, w, statusLiveCol)
		}
	}
	// The animation rides BEHIND the gap, so the widest thing the column must
	// hold is a state word plus one frame of the animation.
	for name, word := range words {
		widest := word + strings.Repeat(statusLiveAnimationChar, liveHintFrameCount-1)
		if w := displayCellsForTest(widest); w > statusLiveCol {
			t.Errorf("the %s state at its widest (word + %d animation cell(s)) is %d cells, over the reserved column's %d",
				name, liveHintFrameCount-1, w, statusLiveCol)
		}
	}
}
