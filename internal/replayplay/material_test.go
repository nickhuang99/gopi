package replayplay

// material_test.go — WHAT A ROUND IS MADE OF, and the cell arithmetic under it.
//
// The loaders are tested against SYNTHETIC dump/timeline files written into the
// test's own directory (Rule 22's scratch discipline: t.TempDir, never a path in
// the repo), so the case states its own geometry and its own wide runes rather
// than depending on one operator session's dump staying on disk.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// writeDump writes a chat-screen dump twin carrying grid/attr runs and returns
// its path.
func writeDump(t *testing.T, cols, rows int, grid []string, runs [][]lineedit.DumpAttrRun, scrollback []string) string {
	t.Helper()
	body := map[string]any{
		"identity": map[string]any{"surface": "chat"},
		"screen": map[string]any{
			"version":        1,
			"cols":           cols,
			"rows":           rows,
			"attrs_recorded": len(runs) > 0,
			"grid":           grid,
			"attr_runs":      runs,
			"scrollback":     scrollback,
			"lines":          grid,
			"blind_spots":    []string{},
			"cursor":         map[string]int{"row": 0, "col": 0},
		},
	}
	blob, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal the dump: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, DumpJSONName)
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatalf("write the dump: %v", err)
	}
	return dir
}

// TestFitCells_WideRuneContinuationCostsNoColumn is the REGRESSION GUARD for the
// defect this package was measured to have: the dump stores one grid slot per
// column, a wide rune in the first and a continuation marker in the second, and
// a renderer that emitted the marker as well produced a row ONE COLUMN WIDER PER
// WIDE RUNE. Measured on the operator's own dump (1b1f7dc1…): a 117-column row
// carrying 4 wide runes rendered 121 columns wide.
func TestFitCells_WideRuneContinuationCostsNoColumn(t *testing.T) {
	// Two wide runes, each followed by its continuation slot.
	const cells = 10
	row := "ab常 驻 cd  "
	got := fitCells(row, cells)
	if w := lineedit.DisplayWidth(got); w != cells {
		t.Fatalf("fitCells(%q, %d) renders %d columns, want %d: a wide rune's continuation slot was counted twice",
			row, cells, w, cells)
	}
	// The continuation SLOT is dropped, not emitted: the wide rune already
	// covers both columns, so the row reads "ab常驻cd" — the same glyphs the
	// screen showed, one slot shorter than the grid stored them.
	if !strings.HasPrefix(got, "ab常驻cd") {
		t.Fatalf("the glyphs the screen showed were not preserved: %q", got)
	}
}

// TestFitCells_TruncationNeverSplitsAWideRune pins the other half: a cut in the
// middle of a wide rune must drop the whole rune, never emit half of it (a
// terminal handed half a wide rune paints a filler cell and shifts the row).
func TestFitCells_TruncationNeverSplitsAWideRune(t *testing.T) {
	got := fitCells("ab常 驻 ", 4) // "ab"=2, "常"=2 → the second wide rune does not fit
	if w := lineedit.DisplayWidth(got); w != 4 {
		t.Fatalf("truncated row is %d columns, want 4: %q", w, got)
	}
	if strings.Contains(got, "驻") {
		t.Fatalf("the truncation kept a rune that does not fit: %q", got)
	}
}

// TestLoadDump_FramesAreScrollbackPagesThenTheGrid pins the dump's own frame
// sequence: the scrollback replayed a screenful at a time, then the painted
// screen — and that a dump alone carries NO rounds (a screen is a screenful of
// cells, not a model round: it has no prompt, no reasoning and no tool calls).
func TestLoadDump_FramesAreScrollbackPagesThenTheGrid(t *testing.T) {
	grid := []string{"screen row one", "screen row two"}
	scroll := []string{"s1", "s2", "s3", "s4", "s5"}
	dir := writeDump(t, 20, 2, grid, nil, scroll)
	mat, err := LoadDump(filepath.Join(dir, DumpJSONName))
	if err != nil {
		t.Fatalf("LoadDump: %v", err)
	}
	// 5 scrollback lines at 2 rows per page = 3 pages, then the grid.
	if len(mat.DumpFrames) != 4 {
		t.Fatalf("dump frame count is %d, want 4 (3 scrollback pages + the screen): %+v", len(mat.DumpFrames), mat.DumpFrames)
	}
	for i := 0; i < 3; i++ {
		if mat.DumpFrames[i].Kind != FrameScrollback {
			t.Fatalf("frame %d is %q, want %q", i, mat.DumpFrames[i].Kind, FrameScrollback)
		}
		if len(mat.DumpFrames[i].Rows) > 2 {
			t.Fatalf("scrollback frame %d carries %d rows for a 2-row screen", i, len(mat.DumpFrames[i].Rows))
		}
	}
	last := mat.DumpFrames[3]
	if last.Kind != FrameScreen {
		t.Fatalf("the last frame is %q, want %q (the painted grid comes last)", last.Kind, FrameScreen)
	}
	if len(last.Rows) != len(grid) {
		t.Fatalf("the screen frame carries %d rows, want %d", len(last.Rows), len(grid))
	}
	if mat.GeometrySource != GeometryFromDump {
		t.Fatalf("geometry source is %q, want %q (a dump states the size the screen WAS)", mat.GeometrySource, GeometryFromDump)
	}
	if len(mat.Rounds) != 0 || mat.RoundCount != 0 {
		t.Fatalf("a dump alone reports %d round(s) / count %d: a screen is not a round, and 0 is the honest answer",
			len(mat.Rounds), mat.RoundCount)
	}
	if mat.PlayableUnits() != mat.DumpFrameCount() {
		t.Fatalf("PlayableUnits = %d, want the dump's own %d frames (no turn and no round exists, so the frames ARE what the player walks)",
			mat.PlayableUnits(), mat.DumpFrameCount())
	}
}

// TestLoadSession_GeometryAndSourceForTheTwoShapes pins the two material shapes
// and the two MATERIAL KINDS inside one: a session with both artifacts has
// ROUNDS (from the log) and DUMP FRAMES (from the dump), and the two never share
// a sequence — the dump's geometry wins, because that is the size the screen was.
func TestLoadSession_GeometryAndSourceForTheTwoShapes(t *testing.T) {
	dir := writeDump(t, 42, 7, []string{"hello"}, nil, nil)
	tl := writeTimeline(t, dir, 1)
	both, err := LoadSession(dir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if both.Source != SourceBoth {
		t.Fatalf("source is %q, want %q", both.Source, SourceBoth)
	}
	if both.Cols != 42 || both.Rows != 7 {
		t.Fatalf("geometry is %dx%d, want 42x7 (the dump's own)", both.Cols, both.Rows)
	}
	if len(both.Rounds) != 1 || both.RoundCount != 1 {
		t.Fatalf("rounds = %d / count = %d, want the log's single round", len(both.Rounds), both.RoundCount)
	}
	if both.Rounds[0].Kind != RoundModel {
		t.Fatalf("the step is %q, want %q", both.Rounds[0].Kind, RoundModel)
	}
	if !strings.Contains(both.Rounds[0].Prompt, "the operator question") {
		t.Fatalf("the round did not carry the request it answered:\n%+v", both.Rounds[0])
	}
	if len(both.DumpFrames) == 0 || both.DumpFrames[len(both.DumpFrames)-1].Kind != FrameScreen {
		t.Fatalf("the dump's own screen is not in DumpFrames: %+v", both.DumpFrames)
	}
	// The TURNS are what the playback walks when the log recorded them: the
	// dump's screens are a second material, never extra turns.
	if len(both.Turns) != both.TurnCount {
		t.Fatalf("len(Turns) = %d, want the TURN count %d", len(both.Turns), both.TurnCount)
	}
	_ = tl

	// A session with only a timeline: no dump → the fallback geometry, announced.
	only := t.TempDir()
	writeTimeline(t, only, 1)
	mat, err := LoadSession(only)
	if err != nil {
		t.Fatalf("LoadSession (timeline only): %v", err)
	}
	if mat.Source != SourceSessionLog {
		t.Fatalf("source is %q, want %q", mat.Source, SourceSessionLog)
	}
	if mat.GeometrySource != GeometryFromFallback {
		t.Fatalf("geometry source is %q, want %q — a timeline states no screen size and the playback must say so",
			mat.GeometrySource, GeometryFromFallback)
	}
}

// TestLoadSession_RefusesASessionWithNeitherArtifact is the Rule-15 case: the
// refusal must name BOTH paths and what each would have to be. A verb that
// invented empty material would play a blank screen and call it a replay.
func TestLoadSession_RefusesASessionWithNeitherArtifact(t *testing.T) {
	_, err := LoadSession(t.TempDir())
	if err == nil {
		t.Fatal("a session with neither artifact loaded — the refusal is the contract")
	}
	for _, want := range []string{DumpJSONName, SessionJSONLName} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %s: %v", want, err)
		}
	}
}

// writeTimeline writes a session log into dir with n model rounds and returns
// its path. Each round is a `request` event followed by an assistant row, which
// is the shape internal/gopilog groups on.
func writeTimeline(t *testing.T, dir string, n int) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(`{"type":"user","message":{"role":"user","content":"the operator question"}}` + "\n")
	// THE ROW ORDER IS THE WRITER'S, and getting it wrong is what made this
	// fixture disagree with every real log: the assistant's blocks and their
	// tool results are written FIRST, and the `request` row that CLOSES the
	// exchange follows them (internal/agent's LogAssistant* then LogRequest —
	// the row records the usage of the call that just finished). Measured on
	// tests/fixtures/log-stats-raw and on a live session: user, assistant,
	// request, tool_result, … A fixture that puts the request row first makes
	// the reader attribute every exchange's content to the NEXT one.
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer %d"},{"type":"tool_use","id":"t%d","name":"bash","input":{"command":"echo %d"}}]}}`+"\n", i+1, i+1, i+1)
		fmt.Fprintf(&b, `{"type":"request","iteration":%d}`+"\n", i)
	}
	b.WriteString(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"the tool answered"}]}}` + "\n")
	path := filepath.Join(dir, SessionJSONLName)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write the timeline: %v", err)
	}
	return path
}

// TestLoadJSONL_OneOperatorRowIsOneStepPerRound pins the RESTORED definition on a
// log whose only operator row is its first (operator's ruling, 2026-10-12):
// round ≡ iteration ≡ one HTTP request/response, i.e. one JSONL `request` row.
// The N `request` rows under that ONE operator row are therefore N ROUNDS — N
// steps, each the same round `gopi session log <id> --rounds` tabulates and the
// same one `gopi session replay <id> --rounds` prints as `rounds=`. The operator
// row is ONE TURN, the neighbouring quantity, reported under its own name.
func TestLoadJSONL_OneOperatorRowIsOneStepPerRound(t *testing.T) {
	dir := t.TempDir()
	writeTimeline(t, dir, 3)
	mat, err := LoadJSONL(filepath.Join(dir, SessionJSONLName))
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if len(mat.Rounds) != 3 || mat.RoundCount != 3 {
		t.Fatalf("rounds = %d / count = %d, want 3 and 3 (3 `request` rows = 3 rounds under ONE operator row)", len(mat.Rounds), mat.RoundCount)
	}
	if mat.TurnCount != 1 {
		t.Fatalf("turns = %d, want 1 (the operator's single row is ONE turn — the neighbour, under its own name)", mat.TurnCount)
	}
	for i, r := range mat.Rounds {
		if r.Index != i+1 {
			t.Fatalf("round %d carries Index %d: the material's own numbering must match the session's", i, r.Index)
		}
		if r.Kind != RoundModel {
			t.Fatalf("round %d is %q, want %q", i, r.Kind, RoundModel)
		}
	}
}

// TestRoundCarriesItsWholeContent is THE property this change exists for: one
// step shows the round's request, its reasoning IN FULL, its prose, and its tool
// calls with their results — everything the old per-JSONL-row frames split
// across several steps or dropped outright.
func TestRoundCarriesItsWholeContent(t *testing.T) {
	dir := t.TempDir()
	const wantThinking = "a long chain of thought that no single screenful would hold, and it must survive whole"
	body := `{"type":"user","message":{"role":"user","content":"what is the cell width?"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[` +
		`{"type":"thinking","thinking":"` + wantThinking + `"},` +
		`{"type":"text","text":"two for a wide rune"},` +
		`{"type":"tool_use","id":"t1","name":"bash","input":{"command":"echo hi"}}]}}` + "\n" +
		`{"type":"request","iteration":1}` + "\n" +
		`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"hi"}]}}` + "\n"
	path := filepath.Join(dir, SessionJSONLName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	mat, err := LoadJSONL(path)
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if len(mat.Rounds) != 1 {
		t.Fatalf("rounds = %d, want 1: %+v", len(mat.Rounds), mat.Rounds)
	}
	r := mat.Rounds[0]
	if strings.TrimSpace(r.Prompt) != "what is the cell width?" {
		t.Fatalf("the round's prompt is %q", r.Prompt)
	}
	if len(r.Thinking) != 1 || !strings.Contains(r.Thinking[0], wantThinking) {
		t.Fatalf("the round's REASONING is not carried in full: %#v", r.Thinking)
	}
	if len(r.Text) != 1 || !strings.Contains(r.Text[0], "two for a wide rune") {
		t.Fatalf("the round's prose is not carried: %#v", r.Text)
	}
	if len(r.Calls) != 1 {
		t.Fatalf("the round's tool calls are %#v, want 1", r.Calls)
	}
	if r.Calls[0].Name != "bash" || !strings.Contains(r.Calls[0].InputRaw, "echo hi") {
		t.Fatalf("the tool call lost its identity or its input: %+v", r.Calls[0])
	}
	if strings.TrimSpace(r.Calls[0].Result) != "hi" {
		t.Fatalf("the tool RESULT is %q, want %q — a round shows what the tool answered", r.Calls[0].Result, "hi")
	}
	// And the rows are tagged by role, so the renderer can colour them apart.
	joined := strings.Join(roundCellRows(r), "\n")
	for _, want := range []string{roundPromptLabel, roundThinkingLabel, roundTextLabel, roundToolLabel, roundResultLabel} {
		if !strings.Contains(joined, want) {
			t.Errorf("the round's rows do not carry %q:\n%s", want, joined)
		}
	}
	roles := map[RowRole]bool{}
	for _, row := range r.Rows {
		roles[row.Role] = true
	}
	for _, want := range []RowRole{RolePrompt, RoleThinking, RoleText, RoleToolCall, RoleToolResult} {
		if !roles[want] {
			t.Errorf("no row carries the role %q — the renderer would paint it as body text", want)
		}
	}
}

// TestHarnessRowIsNotTheRoundsPrompt is the defect this change FIXED, and it is
// worth a case of its own: a harness-injected row (the framework's per-tool-call
// clock tick) arrives as a `user` event, and "last user row wins" made it the
// round's prompt — so a round of a real run displayed
// "[system] [t+683s] — first request of this turn" as the thing the operator had
// asked for, while the operator's own words were unrecoverable from the round
// view. The two are now separate: the tick is a HARNESS row of the round it
// arrived in, and the round's prompt is still the operator's.
func TestHarnessRowIsNotTheRoundsPrompt(t *testing.T) {
	dir := t.TempDir()
	const tick = sessionevent.HarnessMessagePrefix + "[t+683s] (first request of this turn)"
	body := `{"type":"user","message":{"role":"user","content":"the operator's own words"}}` + "\n" +
		`{"type":"user","message":{"role":"user","content":"` + tick + `"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"working"}]}}` + "\n" +
		`{"type":"request","iteration":1}` + "\n"
	path := filepath.Join(dir, SessionJSONLName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	mat, err := LoadJSONL(path)
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if len(mat.Rounds) != 1 {
		t.Fatalf("rounds = %d, want 1: a harness tick must not create a round of its own", len(mat.Rounds))
	}
	r := mat.Rounds[0]
	if strings.Contains(r.Prompt, sessionevent.HarnessMessagePrefix) {
		t.Fatalf("the round's prompt is the harness tick (%q): the operator's request was overwritten", r.Prompt)
	}
	if !strings.Contains(r.Prompt, "the operator's own words") {
		t.Fatalf("the round's prompt is %q, want the operator's request", r.Prompt)
	}
	if len(r.HarnessPrompts) != 1 || !strings.Contains(r.HarnessPrompts[0], "[t+683s]") {
		t.Fatalf("the harness row was dropped instead of kept beside the prompt: %#v", r.HarnessPrompts)
	}
}

// TestHarnessRowIsRecognisedPastLeadingWhitespace is the measured shape: a real
// row of this host's own tree begins "\n\n[system] This run's briefing is
// large…", and a prefix test against the raw text misses it — leaving a
// framework message rendered as the operator's.
func TestHarnessRowIsRecognisedPastLeadingWhitespace(t *testing.T) {
	dir := t.TempDir()
	body := `{"type":"user","message":{"role":"user","content":"start"}}` + "\n" +
		`{"type":"user","message":{"role":"user","content":"\n\n` + sessionevent.HarnessMessagePrefix + `TASK-START CHECKPOINT: this task…"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}` + "\n" +
		`{"type":"request","iteration":1}` + "\n"
	path := filepath.Join(dir, SessionJSONLName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	mat, err := LoadJSONL(path)
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if len(mat.Rounds) != 1 || len(mat.Rounds[0].HarnessPrompts) != 1 {
		t.Fatalf("a leading-blank-line harness row was not recognised: %+v", mat.Rounds)
	}
	if strings.Contains(mat.Rounds[0].Prompt, "TASK-START") {
		t.Fatalf("the briefing's checkpoint row became the round's prompt: %q", mat.Rounds[0].Prompt)
	}
}

// TestRoundIndexFor_RefusesOutOfRangeByRoundsOwnResolver pins the selector: the
// grammar is internal/gopilog's (`all|last|N`, 0 and N negative from the end),
// and an out-of-range selector is REFUSED — never clamped to another round.
//
// The material has THREE human rounds, so the fixture puts three operator rows
// in it: the selector resolves against human rounds (the playback's own unit),
// and a fixture whose exchanges all belonged to one ask could not tell a
// correct resolver from a broken one.
func TestRoundIndexFor_RefusesOutOfRangeByRoundsOwnResolver(t *testing.T) {
	dir := t.TempDir()
	writeTimelineWithAsks(t, dir, 3)
	mat, err := LoadJSONL(filepath.Join(dir, SessionJSONLName))
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if mat.RoundCount != 3 {
		t.Fatalf("the fixture made %d human rounds, want 3", mat.RoundCount)
	}
	ok := map[string]int{"1": 1, "3": 3, "0": 3, "last": 3, "-1": 3, "-3": 1}
	for arg, want := range ok {
		got, err := RoundIndexFor(mat, arg)
		if err != nil {
			t.Errorf("RoundIndexFor(%q) refused a valid selector: %v", arg, err)
			continue
		}
		if got != want {
			t.Errorf("RoundIndexFor(%q) = %d, want %d", arg, got, want)
		}
	}
	for _, arg := range []string{"4", "-4", "9999", "all", "banana"} {
		if _, err := RoundIndexFor(mat, arg); err == nil {
			t.Errorf("RoundIndexFor(%q) accepted a selector that names no single round", arg)
		}
	}
}

// writeTimelineWithAsks writes `asks` OPERATOR rows, each followed by ONE
// exchange, in the writer's own row order — the fixture the selector and the
// label tests need, where the human round count is the number of asks.
func writeTimelineWithAsks(t *testing.T, dir string, asks int) {
	t.Helper()
	var b strings.Builder
	for i := 0; i < asks; i++ {
		fmt.Fprintf(&b, `{"type":"user","message":{"role":"user","content":"ask %d"}}`+"\n", i+1)
		fmt.Fprintf(&b, `{"type":"user","message":{"role":"user","content":"[system] [t+%ds] — +1s since your previous request."}}`+"\n", i+1)
		fmt.Fprintf(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer %d"},{"type":"tool_use","id":"t%d","name":"bash","input":{"command":"echo %d"}}]}}`+"\n", i+1, i+1, i+1)
		fmt.Fprintf(&b, `{"type":"request","iteration":%d}`+"\n", i)
		fmt.Fprintf(&b, `{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t%d","content":"ok"}]}}`+"\n", i+1)
	}
	path := filepath.Join(dir, SessionJSONLName)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write the timeline: %v", err)
	}
}

// roundCellRows is a round's rows as plain text, for an assertion message.
func roundCellRows(r Round) []string {
	out := make([]string, 0, len(r.Rows))
	for _, row := range r.Rows {
		out = append(out, row.Cells)
	}
	return out
}
