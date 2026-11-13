package replayplay

// humanround_material_test.go — THE OPERATOR'S OWN SNIPPET, as a replay step.
//
// THE REPORT (operator, 2026-10-11; task 01a12b40): the playback showed
//
//	HARNESS:  [system] [t+222s] — +1s since your previous request.
//	TOOL: read(path=…)
//	RESULT:   [t+223s] [tool_use id: …]
//	 gopi session replay 5af45c3e round 3/123  PAUSED  1x  (top of this round)
//
// and the operator's reading is that this is ONE round — one harness clock tick
// with the tool call it announced — not one round per `request` row. The fixture
// below is that snippet in miniature: ONE tick, TWO tool calls, each exchange
// closed by its own `request` row.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/gopilog"
)

// TestOneOperatorRowWithManyCallsIsOneRound is the regression guard for the
// reported defect: N tool calls answering ONE operator row are ONE step of the
// playback, while the exchange count beside it still reports N (the two are
// different quantities and the playback prints BOTH under their own names —
// Rule 15).
//
// The fixture is the operator's own reported snippet in miniature: their row,
// the harness tick that follows it, then N `request` rows. Under the restored
// definition (2026-10-12) each of those rows is ONE ROUND, so the playback steps
// N times — and their single row is ONE TURN, the neighbour. The old reading
// (one step per operator row) is what this test pinned in reverse; the operator
// revoked it because it made `rounds=` disagree with every other surface.
func TestOneOperatorRowWithManyCallsIsNTurns(t *testing.T) {
	const calls = 3
	var b strings.Builder
	b.WriteString(`{"type":"user","message":{"role":"user","content":"read the file"}}` + "\n")
	b.WriteString(`{"type":"user","message":{"role":"user","content":"[system] [t+222s] — +1s since your previous request."}}` + "\n")
	for i := 0; i < calls; i++ {
		b.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t` + string(rune('1'+i)) + `","name":"read","input":{"path":"/tmp/x"}}]}}` + "\n")
		b.WriteString(`{"type":"request","iteration":` + string(rune('0'+i)) + `}` + "\n")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, SessionJSONLName)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}
	mat, err := LoadJSONL(path)
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if len(mat.Rounds) != calls || mat.RoundCount != calls {
		t.Fatalf("rounds = %d / count = %d, want %d and %d: %d `request` rows are %d ROUNDS (one HTTP request/response each) whatever the operator typed",
			len(mat.Rounds), mat.RoundCount, calls, calls, calls, calls)
	}
	if mat.TurnCount != 1 {
		t.Fatalf("turns = %d, want 1: the operator's single row is ONE turn, the neighbour — never the round count", mat.TurnCount)
	}
	if mat.Rounds[0].Label != "round 1/3" {
		t.Fatalf("the label is %q, want \"round 1/3\": N is the ROUND count (the same number `gopi session log <id> --rounds` tabulates and the status line's k/N uses)", mat.Rounds[0].Label)
	}
	if got := gopilog.SessionRounds(mustParse(t, path)); got != calls {
		t.Fatalf("the session reader reports %d rounds, want %d — the two surfaces must read the same reader", got, calls)
	}
	// Each exchange of the SAME span shares the round's ordinal: that is what
	// makes `--toolcall N` and the playback agree about "which round".
	sess := mustParse(t, path)
	for i := range sess.Iterations {
		if got := gopilog.TurnOrdinalIn(sess, i); got != 1 {
			t.Fatalf("exchange %d is in human round %d, want 1", i, got)
		}
	}
}

func mustParse(t *testing.T, path string) *gopilog.Session {
	t.Helper()
	s, err := gopilog.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	return s
}

// TestTwoTicksAreTwoRounds is the other side of the same coin: the count follows
// the TICKS, so two ticks make two rounds even when the second is followed by no
// exchange at all — the mark is the round, not the exchange.
func TestTwoTicksAreTwoRounds(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"type":"user","message":{"role":"user","content":"[system] [t+1s] — first request of this session."}}` + "\n")
	b.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"first"}]}}` + "\n")
	b.WriteString(`{"type":"request","iteration":0}` + "\n")
	b.WriteString(`{"type":"user","message":{"role":"user","content":"[system] [t+30s] — +29s since your previous request."}}` + "\n")
	b.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"second"}]}}` + "\n")
	b.WriteString(`{"type":"request","iteration":1}` + "\n")
	dir := t.TempDir()
	path := filepath.Join(dir, SessionJSONLName)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	mat, err := LoadJSONL(path)
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if len(mat.Rounds) != 2 || mat.RoundCount != 2 || mat.TurnCount != 2 {
		t.Fatalf("rounds = %d / count = %d / exchanges = %d, want 2/2/2", len(mat.Rounds), mat.RoundCount, mat.TurnCount)
	}
	if mat.Rounds[1].Label != "round 2/2" {
		t.Fatalf("round 2's label is %q, want \"round 2/2\"", mat.Rounds[1].Label)
	}
}
