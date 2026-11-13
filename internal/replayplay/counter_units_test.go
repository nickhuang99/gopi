package replayplay

// counter_units_test.go — the replay's units are DIFFERENT quantities, and the
// material carries each one under its own name so no caller can print one under
// another's.
//
// WHAT CHANGED AND WHY THIS FILE EXISTS AT ALL. This file used to guard a
// "Turns vs Frames" pair: the old playback advanced one JSONL ROW at a time (a
// frame), and a `--frames` listing printed that count beside a turn count built
// from a second, private reading of the log. Both numbers were this package's
// own invention. Now:
//
//   - a STEP is one session ROUND (internal/gopilog's Iteration), which is the
//     unit `gopi session log <id> --rounds` counts — taken from the SAME reader,
//     so the two surfaces cannot disagree;
//   - the material's DUMP FRAMES are a second material, not turns: a screenful
//     of cells has no prompt, no reasoning and no tool calls, which is exactly
//     what makes it a different thing from a round;
//   - and a dump-only material has ROUNDS = 0, which is the honest answer rather
//     than a fabricated count.
//
// The relation that matters is therefore StepCount() == len(Rounds) whenever any
// round exists, == len(DumpFrames) when none does, and RoundCount == 0 for a
// dump-only material.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/gopilog"
)

// TestPlayableUnitsIsTheTurnCountWhenTurnsExist: with a log present the playback
// walks the session's TURNS — one turn, however many rounds and rows it carries,
// and a turn taller than the screen stays ONE turn (reading the rest is a scroll,
// which is what the old row-per-frame dump could not say).
func TestPlayableUnitsIsTheTurnCountWhenTurnsExist(t *testing.T) {
	dir := t.TempDir()
	// A round whose text is far longer than one screenful: it is still ONE step.
	long := ""
	for i := 0; i < 400; i++ {
		long += "word "
	}
	body := `{"type":"user","message":{"role":"user","content":"` + long + `"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + long + `"}]}}` + "\n" +
		`{"type":"request","iteration":1}` + "\n"
	path := filepath.Join(dir, SessionJSONLName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	mat, err := LoadJSONLAt(path, 40, 10)
	if err != nil {
		t.Fatalf("LoadJSONLAt: %v", err)
	}
	if len(mat.Rounds) != 1 {
		t.Fatalf("a turn taller than the screen became %d round(s), want 1: a round is one HTTP request/response", len(mat.Rounds))
	}
	if len(mat.Turns) != 1 {
		t.Fatalf("len(Turns) = %d, want 1: a turn taller than the screen is still ONE turn (reading the rest is a scroll)", len(mat.Turns))
	}
	if got := mat.PlayableUnits(); got != 1 {
		t.Fatalf("PlayableUnits = %d, want 1 — the playback's unit is the TURN", got)
	}
	if len(mat.Rounds[0].Rows) <= 10 {
		t.Fatalf("the round laid out %d rows for a 400-word body: the rows past the screen must still be laid out (the player scrolls them)", len(mat.Rounds[0].Rows))
	}
}

// TestDumpFramesAreASecondMaterialNotTurns: the dump's screenfuls ride their own
// field and the playback's unit ignores them while turns exist.
func TestDumpFramesAreASecondMaterialNotSteps(t *testing.T) {
	dir := writeDump(t, 20, 2, []string{"a screen row"}, nil, []string{"s1", "s2", "s3"})
	writeTimeline(t, dir, 2)
	mat, err := LoadSession(dir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(mat.DumpFrames) == 0 {
		t.Fatal("the dump contributed no frames")
	}
	// THE STEP IS THE TURN (operator's ruling, 2026-10-22). This assertion used to
	// pin StepCount to len(mat.Rounds) — the shape in which a round was a step.
	// The fixture writes TWO `request` rows inside ONE operator row, so the two
	// candidate numbers no longer coincide and the test can finally tell them
	// apart: what must hold is that the dump's screenfuls are not turn units while the
	// session has material of its own, and that the material's unit is the TURN.
	if len(mat.Turns) != mat.TurnCount {
		t.Fatalf("len(Turns) = %d, want the TURN count %d — the playback's unit is one TURN (every round the harness ran to answer one operator row is inside it)",
			len(mat.Turns), mat.TurnCount)
	}
	if len(mat.Turns) == len(mat.DumpFrames) {
		t.Fatalf("PlayableUnits = %d equalled the dump frame count: the dump's screenfuls are not units while the session has turns",
			len(mat.Turns))
	}
	if mat.RoundCount != 2 {
		t.Fatalf("RoundCount = %d, want 2: the fixture writes TWO `request` rows, and one request row IS one round (round ≡ iteration ≡ one HTTP request/response)", mat.RoundCount)
	}
	if mat.TurnCount != 1 {
		t.Fatalf("TurnCount = %d, want 1: the fixture's ONE operator row is ONE turn — the neighbour, never the round count", mat.TurnCount)
	}
}

// TestRoundCountComesFromTheSessionReader is the anti-divergence check: the
// material's round count is internal/gopilog's own answer for the same file,
// computed HERE independently from the same reader rather than trusted as a copy.
//
// BOTH numbers are checked, because they are two different quantities and the
// defect of 2026-10-11 was one of them being printed under the other's name:
// RoundCount is the ROUND count — one JSONL `request` row = one HTTP
// request/response = one iteration, what the log's round listing tabulates and what
// `gopi session log <id> --rounds` tabulates (operator's ruling, 2026-10-12) —
// and TurnCount is the OPERATOR-row count, the neighbour. This fixture is
// written under ONE operator row, so the two differ on purpose — which is
// exactly why the check reads them from their own functions instead of
// inferring one from the other.
func TestRoundCountComesFromTheSessionReader(t *testing.T) {
	dir := t.TempDir()
	path := writeTimeline(t, dir, 5)
	mat, err := LoadJSONL(path)
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	sess, err := gopilog.ParseFile(path)
	if err != nil {
		t.Fatalf("parse the same log: %v", err)
	}
	n := gopilog.SessionRounds(sess)
	if mat.RoundCount != n {
		t.Fatalf("the material says %d rounds, the session reader's round count is %d — the two surfaces would print different numbers for the same session",
			mat.RoundCount, n)
	}
	if got := gopilog.SessionTurns(sess); mat.TurnCount != got {
		t.Fatalf("the material says %d turns, the session reader says %d — a reader must be able to tell which count is which", mat.TurnCount, got)
	}
	if mat.RoundCount != len(mat.Rounds) {
		t.Fatalf("RoundCount = %d but there are %d rounds", mat.RoundCount, len(mat.Rounds))
	}
}
