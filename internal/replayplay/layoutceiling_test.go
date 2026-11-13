package replayplay

// layoutceiling_test.go — THE DEBUG LAY-OUT ANSWERS *BOTH* TRUNCATIONS.
//
// THE DEFECT THIS FILE PINS (measured 2026-11-01, task 01a19638): `gopi session
// replay --full` expanded every tool call's recorded input and STILL printed
// `(N row(s) of this turn's context are not laid out; its tool rows are)` on the
// body — because the flag reached `Dims.FullToolInput` (the SPELLING of a row)
// and never `roundRowsMax` (HOW MANY rows exist at all). An operator asking for
// the debug view therefore could not obtain the turn's whole picture at ANY flag
// combination: the ceiling is shared by every round of a turn, so on a large
// turn each round was allotted a handful of rows and most of its context was
// reported as not laid out.
//
// The two flags bound two different things, and both must be reachable from the
// one flag the verb has. The tests below fix the ORDER of the three states —
// default (bounded), input-only (the retired behaviour of `--full`), and the
// debug lay-out (no ceiling, full inputs) — so a future change cannot quietly
// restore the half-honoured flag by collapsing two of them.

import (
	"fmt"
	"strings"
	"testing"
)

// layoutFixture is a TURN whose FIRST exchange carries more context rows than
// the whole ceiling admits, followed by a later exchange with a tool call. It is
// the shape the ceiling was measured on: a flood in one round and real content
// in another, so the two questions ("was anything cut" and "are the calls still
// there") are answered separately.
func layoutFixture() Round {
	turn := Round{Index: 1, Kind: RoundTurn, RoundTotal: 2, Prompt: "ask"}
	flood := Round{Index: 1, Kind: RoundModel, Prompt: "ask"}
	// Enough ticks that the turn cannot fit the ceiling however it is divided:
	// a bounded lay-out MUST report omitted rows, or this fixture would prove
	// nothing about the ceiling (it would fit).
	for i := 0; i < roundRowsMax; i++ {
		flood.HarnessPrompts = append(flood.HarnessPrompts, fmt.Sprintf("tick %d", i))
	}
	flood.Calls = []ToolCallView{{Name: "bash", InputRaw: `{"command":"echo flooded"}`, Result: "flooded result"}}
	later := Round{Index: 2, Kind: RoundModel}
	later.Calls = []ToolCallView{{Name: "read", InputRaw: `{"path":"/tmp/x"}`, Result: "later result"}}
	turn.Segments = []Round{flood, later}
	return turn
}

// layoutFloodMarker is the fixture's own tick text: counting it is how these
// tests say "this many of the flood's rows exist" without trusting a length.
const layoutFloodMarker = "tick "

// layoutTickRows counts the fixture's flood rows in a body.
func layoutTickRows(rows []FrameRow) int {
	n := 0
	for _, r := range rows {
		if strings.Contains(r.Cells, layoutFloodMarker) {
			n++
		}
	}
	return n
}

// turnBodyDroppedCount sums the context rows the body REPORTS as not laid out —
// read off the printed note, never off a private counter, so the assertion is
// about what the operator can see.
func turnBodyDroppedCount(t *testing.T, rows []FrameRow) int {
	t.Helper()
	total := 0
	for _, r := range rows {
		if r.Role != RoleError {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(r.Cells, turnBodySegmentDroppedFmt, &n); err == nil {
			total += n
		}
	}
	return total
}

// TestFullLayoutLiftsTheTurnCeiling is the acceptance arm the epic states: the
// DEFAULT body still reports omitted rows (the compact view is the contract
// existing readers rely on), and the DEBUG lay-out reports ZERO and says so by
// carrying no note at all — a "0 row(s) … not laid out" row would be a statement
// about nothing.
func TestFullLayoutLiftsTheTurnCeiling(t *testing.T) {
	turn := layoutFixture()

	bounded := TurnBodyRows(turn, FallbackCols)
	dropped := turnBodyDroppedCount(t, bounded)
	if dropped == 0 {
		t.Fatalf("the DEFAULT body reports no omitted row on a %d-row flood: the fixture no longer reaches the ceiling, so this test would pass on the broken code too", roundRowsMax)
	}
	// The ceiling is a real bound, not a label: it bounds the CONTEXT, and the
	// tool rows are reserved ON TOP of it (they are the rows this view exists
	// for), so the body's own length is not the ceiling. The measurable statement
	// is about the flood: the bounded body lays out FEWER ticks than exist.
	if kept := layoutTickRows(bounded); kept >= roundRowsMax {
		t.Fatalf("the DEFAULT body laid out all %d tick(s) of the flood: the ceiling was not applied", kept)
	}

	full := TurnBodyRowsAt(turn, Dims{Cols: FallbackCols, FullToolInput: true, FullLayout: true})
	if got := turnBodyDroppedCount(t, full); got != 0 {
		t.Fatalf("the DEBUG body reports %d row(s) of context as not laid out, want 0 — `--full` must lift the row ceiling, not only expand tool input:\n%s",
			got, joinRowCells(full))
	}
	for _, r := range full {
		if r.Role == RoleError && strings.Contains(r.Cells, "are not laid out") {
			t.Fatalf("the DEBUG body carries a truncation note %q: a lifted ceiling prints none", r.Cells)
		}
	}
	// NOTHING WAS LEFT OUT — measured on the flood itself, not on the length of
	// the body: every tick exists in the debug lay-out and none is missing.
	if got := layoutTickRows(full); got != roundRowsMax {
		t.Fatalf("the DEBUG body lays out %d of the flood's %d tick(s): the debug view must print the turn's WHOLE context", got, roundRowsMax)
	}
	if len(full) <= len(bounded) {
		t.Fatalf("the DEBUG body laid out %d row(s) and the bounded one %d: lifting the ceiling must add rows", len(full), len(bounded))
	}
	// BOTH truncations are answered by the one flag, and they stay SEPARATE
	// properties (they bound different things) — the input-only state is the
	// retired behaviour of `--full` and is still reachable, so a change that
	// collapses the two would leave the debug view unable to say which it did.
	inputOnly := TurnBodyRowsAt(turn, Dims{Cols: FallbackCols, FullToolInput: true})
	if got := turnBodyDroppedCount(t, inputOnly); got != dropped {
		t.Fatalf("input-only lay-out reports %d omitted row(s), the bounded one %d: Dims.FullToolInput must NOT move the ceiling", got, dropped)
	}
}

// TestFullLayoutIsADimsPropertyThatSurvivesARelayout pins the mechanism rather
// than one call site: the l ay-out lives on Dims, so a caller that RE-LAYS the
// material at another width (the static `--turn` view does exactly that, and so
// does a resize) keeps it. Before this, the debug lay-out was a parameter held
// beside the material, and the first re-lay dropped it — the shape that makes a
// flag honoured once and then silently forgotten.
func TestFullLayoutIsADimsPropertyThatSurvivesARelayout(t *testing.T) {
	mat := Material{
		SessionID: "sess", RoundCount: 1, TurnCount: 1,
		Dims:   Dims{Cols: 80, Rows: 24},
		Rounds: []Round{layoutFixture()},
		Turns:  []Round{layoutFixture()},
	}
	mat.Rounds[0].Rows = turnBodyRows(mat.Rounds[0], mat.Dims)
	mat.Turns[0].Rows = mat.Rounds[0].Rows

	if got := turnBodyDroppedCount(t, mat.Turns[0].Rows); got == 0 {
		t.Fatalf("the fixture's default material reports no omitted row — this test needs one")
	}

	debugged := ExpandForDebug(mat)
	if !debugged.Dims.FullLayout || !debugged.Dims.FullToolInput {
		t.Fatalf("ExpandForDebug left FullLayout=%v FullToolInput=%v, want both true", debugged.Dims.FullLayout, debugged.Dims.FullToolInput)
	}
	if got := turnBodyDroppedCount(t, debugged.Turns[0].Rows); got != 0 {
		t.Fatalf("the debug material still reports %d omitted row(s) after ExpandForDebug", got)
	}

	relaid := Relayout(debugged, 100, 40)
	if !relaid.Dims.FullLayout || !relaid.Dims.FullToolInput {
		t.Fatalf("a re-lay dropped the debug lay-out: FullLayout=%v FullToolInput=%v — `--full` would stop being honoured the moment a caller names a canvas",
			relaid.Dims.FullLayout, relaid.Dims.FullToolInput)
	}
	if got := turnBodyDroppedCount(t, relaid.Turns[0].Rows); got != 0 {
		t.Fatalf("the re-laid debug material reports %d omitted row(s), want 0", got)
	}

	// THE ORDER OF THE THREE STATES: an input-only material (what `--full` used
	// to produce) re-lays back to the ceiling, so the two are not the same flag.
	inputOnly := Relayout(mat, 100, 40)
	inputOnly = ExpandForDebug(inputOnly)
	inputOnly.Dims.FullLayout = false
	inputOnly = inputOnly.relayoutRows()
	if got := turnBodyDroppedCount(t, inputOnly.Turns[0].Rows); got == 0 {
		t.Fatalf("clearing FullLayout did not restore the ceiling: the flag would be decorative")
	}
	if !inputOnly.Dims.FullToolInput {
		t.Fatalf("clearing FullLayout also cleared FullToolInput: the two are separate properties")
	}
}

// TestRowCeilingIsTheCompatibilityDefault pins the published number: the ceiling
// a default lay-out uses is roundRowsMax, the lifted one is 0 ("no ceiling"), and
// a Dims that never asked for the debug view reports the ceiling — the byte-for-
// byte default this change must not move.
func TestRowCeilingIsTheCompatibilityDefault(t *testing.T) {
	if got := (Dims{}).rowCeiling(); got != roundRowsMax {
		t.Fatalf("a default Dims reports ceiling %d, want roundRowsMax %d — the compact view is the default", got, roundRowsMax)
	}
	if got := (Dims{FullLayout: true}).rowCeiling(); got != 0 {
		t.Fatalf("a debug Dims reports ceiling %d, want 0 (no ceiling)", got)
	}
	if got := (Dims{FullToolInput: true}).rowCeiling(); got != roundRowsMax {
		t.Fatalf("an input-only Dims reports ceiling %d, want roundRowsMax: expanding a row's SPELLING must not change how many rows exist", got)
	}
}
