package replayplay

// turnstep_test.go — ONE STEP IS ONE TURN (operator's ruling, 2026-10-22).
//
// The fixture is the smallest sample that can tell the two shapes apart: ONE
// operator row answered with THREE request/response exchanges, each carrying one
// tool call. Under the shape this replaces the playback had THREE steps and its
// status line read "3/3" — three things to step through for one thing the person
// said. Under this one there is ONE step, labelled
// "turn 1/1  (3 request(s), 3 tool call(s))".
//
// The ROUND sequence is untouched and stays addressable (--rounds, --round N,
// `gopi session log <id> N`): what changed is which sequence the player steps
// through.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// TestOneStepIsOneTurnThreeRounds is the core assertion.
func TestOneStepIsOneTurnThreeRounds(t *testing.T) {
	dir := t.TempDir()
	writeTimeline(t, dir, 3)
	mat, err := LoadSession(dir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	// The ROUND sequence is unchanged: three exchanges, three rounds.
	if len(mat.Rounds) != 3 || mat.RoundCount != 3 {
		t.Fatalf("rounds = %d / count = %d, want 3 and 3 — the per-exchange view must keep its own numbering",
			len(mat.Rounds), mat.RoundCount)
	}
	// The STEP sequence is the TURN: one operator row, one step.
	if len(mat.Turns) != 1 {
		t.Fatalf("turns = %d, want 1 — one operator row is ONE turn (every round it took is inside it)", len(mat.Turns))
	}
	if len(mat.Turns) != 1 {
		t.Fatalf("len(Turns) = %d, want 1", len(mat.Turns))
	}
	step := mat.Turns[0]
	if step.Kind != RoundTurn {
		t.Fatalf("the step's kind is %q, want %q", step.Kind, RoundTurn)
	}
	if !strings.HasPrefix(step.Label, sessionevent.CounterUnitTurnOne+" 1/1") {
		t.Fatalf("the step's label %q does not open with the turn and its position", step.Label)
	}
	// The label's DETAIL is the rounds this turn took — measured, not assumed.
	if !strings.Contains(step.Label, "3 request(s)") {
		t.Fatalf("the step's label %q does not carry the three requests it spans", step.Label)
	}
	if !strings.Contains(step.Label, "3 tool call(s)") {
		t.Fatalf("the step's label %q does not carry the three tool calls it spans", step.Label)
	}
	// AND THE CONTENT IS THE WHOLE TURN: every exchange's tool call is inside
	// the ONE step — that is the property the step unit exists for. (The old
	// three-step shape put one call in each.)
	if len(step.Calls) < 3 {
		t.Fatalf("the step holds %d tool call(s), want at least the 3 the turn ran — a step that drops the turn's calls is not a turn", len(step.Calls))
	}
}

// TestStepLabelCountsAreTheTurnsOwn is the cross-surface agreement: the number in
// the step's "request(s)" detail equals the round count the material carries, and
// the "k/N" denominator equals the material's turn count. Two surfaces, one fact.
func TestStepLabelCountsAreTheTurnsOwn(t *testing.T) {
	dir := t.TempDir()
	writeTimeline(t, dir, 2)
	mat, err := LoadSession(dir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(mat.Turns) != mat.TurnCount {
		t.Fatalf("turns = %d but TurnCount = %d — the turn sequence and the turn count must be one number", len(mat.Turns), mat.TurnCount)
	}
	want := "turn 1/" + itoaTest(len(mat.Turns))
	if !strings.HasPrefix(mat.Turns[0].Label, want) {
		t.Fatalf("the label %q does not open with %q: the denominator must be the material's own turn count", mat.Turns[0].Label, want)
	}
	if !strings.Contains(mat.Turns[0].Label, itoaTest(mat.RoundCount)+" request(s)") {
		t.Fatalf("the label %q does not carry the material's %d round(s)", mat.Turns[0].Label, mat.RoundCount)
	}
}

// TestRoundAddressSpaceIsUnchanged: `--round N` still resolves against the ROUNDS,
// so a caller who passes round 2 keeps meaning exchange 2 — the change to the
// step unit must not silently renumber an address.
func TestRoundAddressSpaceIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeTimeline(t, dir, 3)
	mat, err := LoadSession(dir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	for n := 1; n <= 3; n++ {
		idx, rerr := RoundIndexFor(mat, itoaTest(n))
		if rerr != nil {
			t.Fatalf("RoundIndexFor(%d): %v", n, rerr)
		}
		if idx != n {
			t.Fatalf("--round %d resolved to index %d — the round address space moved", n, idx)
		}
	}
	if len(mat.Rounds) == len(mat.Turns) {
		t.Fatal("the fixture's rounds and steps are the same length, so this test cannot see a swap of the two sequences")
	}
}

// itoaTest is the file's own integer spelling (kept local so the test imports no
// formatting package for one cast).
func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestStatusLineNamesTheTurnAsItsUnit: the status line's position carries the
// unit's WORD, not a bare "3/8" whose unit a reader had to infer — and the word
// comes from sessionevent, where the session surfaces' vocabulary lives, so the
// painted chrome and the printed tables cannot drift apart (Rule 14).
func TestStatusLineNamesTheTurnAsItsUnit(t *testing.T) {
	out := Render(RenderOptions{
		Round:      Round{Index: 2, Kind: RoundTurn, Label: "turn 2/25  (3 request(s))"},
		RoundIndex: 2, RoundTotal: 25, Cols: 100, Rows: 4,
		SessionID: "56507166-ef62-4d2d-bafe-af504fca9311",
		Palette:   DefaultPalette(), Truecolor: true,
	})
	want := sessionevent.CounterUnitTurnOne + " 2/25"
	if !strings.Contains(out, want) {
		t.Fatalf("the status line does not carry %q — the position must name its unit:\n%q", want, out)
	}
	if strings.Contains(out, " "+string(FrameRound)+" ") {
		t.Fatalf("the status line repeats the step's kind beside a position that already says it:\n%q", out)
	}
	// The chrome is ONE row: it must not wrap, or it pushes a row of content off
	// a fixed canvas.
	for _, row := range renderedRows(out) {
		if _, cells := stripCells(row); cells > 100 {
			t.Fatalf("a status row is %d cells wide on a 100-column canvas: %q", cells, row)
		}
	}
}

// TestTurnIsThePlayableUnitAndRoundsAreNot pins the ONE unit the playback walks
// after `steps` stopped existing: Material.PlayableUnits answers with the TURN
// count, TurnAt addresses the TURN sequence, and neither can be answered from
// the round sequence — the two are equal in a one-exchange-per-ask session and
// different the moment a turn takes several, which is the case this fixture
// builds on purpose.
func TestTurnIsThePlayableUnitAndRoundsAreNot(t *testing.T) {
	dir := t.TempDir()
	// ONE operator row answered with THREE exchanges: one turn, three rounds.
	body := `{"type":"user","message":{"role":"user","content":"one ask"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"first"}]}}` + "\n" +
		`{"type":"request","iteration":0}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"second"}]}}` + "\n" +
		`{"type":"request","iteration":1}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"third"}]}}` + "\n" +
		`{"type":"request","iteration":2}` + "\n"
	path := filepath.Join(dir, SessionJSONLName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	mat, err := LoadJSONLAt(path, 40, 10)
	if err != nil {
		t.Fatalf("LoadJSONLAt: %v", err)
	}
	if len(mat.Turns) != 1 {
		t.Fatalf("Turns = %d, want 1 (one operator row is ONE turn)", len(mat.Turns))
	}
	if mat.RoundCount != 3 {
		t.Fatalf("RoundCount = %d, want 3 (three `request` rows)", mat.RoundCount)
	}
	// The playback walks the TURNS, so its length is the turn count — NOT the
	// round count. This is the assertion the old StepCount could not make, because
	// it answered with whichever sequence happened to be non-empty first.
	if got := mat.PlayableUnits(); got != len(mat.Turns) {
		t.Fatalf("PlayableUnits = %d, want the TURN count %d (the round count is %d and is NOT the playback's unit)",
			got, len(mat.Turns), mat.RoundCount)
	}
	if got := mat.PlayableUnits(); got == len(mat.Rounds) {
		t.Fatalf("PlayableUnits = %d equals the round count — the fixture must be able to tell the two apart", got)
	}
	if _, ok := mat.TurnAt(2); ok {
		t.Fatalf("TurnAt(2) answered for a material with 1 turn")
	}
	turn, ok := mat.TurnAt(1)
	if !ok {
		t.Fatalf("TurnAt(1) named no turn")
	}
	// EVERY exchange of the ask is inside that one turn.
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(strings.Join(turnText(turn), "\n"), want) {
			t.Fatalf("turn 1 does not carry %q — a turn holds every exchange the harness ran to answer the ask:\n%v", want, turnText(turn))
		}
	}
}

// turnText renders a turn's prose for a content assertion.
func turnText(r Round) []string {
	out := append([]string(nil), r.Text...)
	for _, c := range r.Calls {
		out = append(out, c.Name, c.Result)
	}
	return out
}
