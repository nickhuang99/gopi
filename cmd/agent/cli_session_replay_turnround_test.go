package main

// cli_session_replay_turnround_test.go — THE TWO UNITS MUST NAME EACH OTHER.
//
// The defect this pins (measured on session 80c07a7d, 2026-10-26): the playback
// has two address spaces — TURN (the operator surface) and ROUND (the transport,
// internal/debug) — and the static listings printed both numberings side by side
// without either one saying what the other covered. An operator reading
// `11  turn 11/11  (10 request(s), 8 tool call(s))` asked for that turn as
// `--round 152` and got ONE round of the ten: 3,419 bytes of the turn's 12,436,
// with all eight tool calls missing. Nothing in the output was wrong; what was
// wrong is that `round 152/152` never said "one round of turn 11, which covers
// rounds 143-152".
//
// So this file pins three sentences:
//
//  1. `--turns` prints each turn's ROUND INTERVAL beside its number, and the row
//     number is the TURN (so N turns over M > N rounds cannot be read as M).
//  2. `--round N` opens with the unit's own boundary: this is ONE round, it
//     belongs to turn k/K, that turn covers rounds a-b, ask `--turn k` for the
//     whole thing.
//  3. Its honest other half: a round no turn spans says so, and does not invent
//     a turn number (Rule 15).
//
// The fixture is the smallest one that can tell the shapes apart: ONE operator row
// answered with THREE request/response exchanges, each carrying its own tool call
// — so the turn count and the round count differ (1 vs 3) and a listing that
// conflated them could not pass.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/replayplay"
	"github.com/nick/gopi/internal/sessionevent"
)

// replayTurnRowLabelRe matches a listing row's own label ("turn 11/11"), so the
// row count is measured by what the rows SAY rather than by a substring that the
// listing's preamble also happens to contain.
var replayTurnRowLabelRe = regexp.MustCompile(`turn \d+/\d+`)

// replayTurnRoundFixture writes a session whose ONE operator row is answered with
// three exchanges, and returns its profile root and id.
func replayTurnRoundFixture(t *testing.T) (profileRoot, sessionID string) {
	t.Helper()
	root := t.TempDir()
	sessionID = replayTurnRoundSessionID
	dir := filepath.Join(config.ProfilePathIn(root, config.SessionsDirName), sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create the session dir: %v", err)
	}
	path := filepath.Join(dir, replayplay.SessionJSONLName)
	if err := os.WriteFile(path, []byte(replayTurnRoundTimeline), 0o600); err != nil {
		t.Fatalf("write the timeline: %v", err)
	}
	return root, sessionID
}

const (
	replayTurnRoundSessionID = "test-turn-round-0001"
	// replayTestToolCallRowPrefix is the row prefix a tool call is printed under
	// in a material's rows. It is internal/lineedit's ONE tool glyph plus its
	// marker column's space — the SAME marker the live status line, the chat
	// transcript and `chat --replay` draw. The old spelling ("TOOL: ") made this
	// view print eight tool calls under a name nothing else in the system used,
	// so an operator comparing it against the transcript (or grepping for the
	// marker their own report used) scored it ZERO (measured 2026-10-26, session
	// 80c07a7d). The value comes from the glyph's own definition, never typed.
	replayTestToolCallRowPrefix = lineedit.ToolGlyph + " "
	// replayTurnRoundRounds is how many ROUNDS the fixture's one turn spans.
	replayTurnRoundRounds = 3
	// replayTurnRoundTurns is how many TURNS it holds — deliberately not the
	// round count, so a surface that mixed the two fails here.
	replayTurnRoundTurns = 1
)

// replayTurnRoundTimeline is ONE operator row and THREE exchanges, each a set of
// assistant rows closed by its `request` row (the writer's own order).
var replayTurnRoundTimeline = `{"type":"user","message":{"role":"user","content":"run all three"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"first answer"},{"type":"tool_use","id":"t1","name":"bash","input":{"command":"echo one"}}]}}
{"type":"request","iteration":0}
{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"result one"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"second answer"},{"type":"tool_use","id":"t2","name":"read","input":{"path":"/tmp/x"}}]}}
{"type":"request","iteration":1}
{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"result two"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"third answer"},{"type":"tool_use","id":"t3","name":"bash","input":{"command":"echo three"}}]}}
{"type":"request","iteration":2}
{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t3","content":"result three"}]}}
`

// TestSessionReplayTurnsRowCarriesItsRoundInterval is the listing half: each row
// is numbered by the TURN and prints the ROUNDS that turn covered, so the two
// numbers stand on one line and name each other.
//
// THE ROW NUMBER IS THE TURN, asserted by counting rows: three rounds under one
// turn must list ONE row whose number is 1 — a listing that printed the rounds
// would show three.
func TestSessionReplayTurnsRowCarriesItsRoundInterval(t *testing.T) {
	home, id := replayTurnRoundFixture(t)
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--turns")
	if err != nil {
		t.Fatalf("--turns failed: %v\n%s", err, out)
	}
	// PRESENT: the interval, spelled by the shared formatter rather than by a
	// literal in this file (Rule 14: one spelling, and the test reads it).
	want := gopilog.FormatTurnRoundRange(1, replayTurnRoundRounds)
	if !strings.Contains(out, want) {
		t.Fatalf("--turns does not print %q — a turn's row must name the ROUNDS it covered:\n%s", want, out)
	}
	// The row count IS the turn count (1), while the rounds are 3: the listing
	// answers in turns.
	if got := strings.Count(out, "turn 1/"); got != replayTurnRoundTurns {
		t.Fatalf("the listing prints %d row(s) beginning %q, want %d — a row is a TURN:\n%s",
			got, "turn 1/", replayTurnRoundTurns, out)
	}
	// The ROW count, read off the labels the rows carry ("turn k/N") rather than
	// off the bare word: the listing's own preamble says "the turn's DETAIL", and
	// a substring count over the whole output would score that sentence as a row.
	if got := len(replayTurnRowLabels(out)); got != replayTurnRoundTurns {
		t.Fatalf("the listing prints %d turn row(s) over %d rounds — the row number must be the turn:\n%s",
			got, replayTurnRoundRounds, out)
	}
}

// TestSessionReplayRoundOpensWithItsUnitBoundary is the addressing half: the
// FIRST line a reader of `--round N` sees says that this is one round, which turn
// it belongs to, the interval that turn covers, and the flag that prints the
// whole turn.
func TestSessionReplayRoundOpensWithItsUnitBoundary(t *testing.T) {
	home, id := replayTurnRoundFixture(t)
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--round", "2")
	if err != nil {
		t.Fatalf("--round 2 failed: %v\n%s", err, out)
	}
	first, _, _ := strings.Cut(out, "\n")
	want := fmt.Sprintf(sessionevent.CounterRoundIsOneSpotFmt,
		1, replayTurnRoundTurns, gopilog.FormatTurnRoundRange(1, replayTurnRoundRounds), sessionReplayTurnFlag, 1)
	if first != want {
		t.Fatalf("--round 2's first line is\n  %q\nwant\n  %q\nthe unit boundary must be the first thing a reader meets", first, want)
	}
	// The boundary NAMES the flag, so a reader can copy the fix rather than
	// remember it.
	if !strings.Contains(first, sessionReplayTurnFlag) {
		t.Fatalf("the boundary does not name %q:\n%s", sessionReplayTurnFlag, first)
	}
	// And the round's own header still follows — the view is the round's.
	if !strings.Contains(out, fmt.Sprintf("round 2/%d", replayTurnRoundRounds)) {
		t.Fatalf("--round 2 does not name the round it rendered:\n%s", out)
	}
	// A round's view carries THAT round only: the other rounds' prose stays out
	// (so the new first line is not a claim made by a view that changed meaning).
	for _, absent := range []string{"first answer", "third answer"} {
		if strings.Contains(out, absent) {
			t.Fatalf("--round 2 leaked another round's content (%q):\n%s", absent, out)
		}
	}
	if !strings.Contains(out, "second answer") {
		t.Fatalf("--round 2 does not carry its own prose:\n%s", out)
	}
}

// TestSessionReplayTurnPrintsAllRoundsOfTheTurn is the capability the boundary
// points at: `--turn 1` prints EVERY round of the turn — all three tool calls and
// all three results — which is exactly what a `--round N` address cannot do.
func TestSessionReplayTurnPrintsAllRoundsOfTheTurn(t *testing.T) {
	home, id := replayTurnRoundFixture(t)
	whole, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--turn", "1")
	if err != nil {
		t.Fatalf("--turn 1 failed: %v\n%s", err, whole)
	}
	for _, want := range []string{
		"first answer", "second answer", "third answer",
		"result one", "result two", "result three",
	} {
		if !strings.Contains(whole, want) {
			t.Fatalf("--turn 1 does not carry %q — a turn is ALL of its rounds:\n%s", want, whole)
		}
	}
	// The three tool calls, counted: a turn's view must print one per call.
	if got := strings.Count(whole, replayTestToolCallRowPrefix); got < replayTurnRoundRounds {
		t.Fatalf("--turn 1 prints %d tool-call row(s) for %d call(s) — the turn's calls must all be there:\n%s",
			got, replayTurnRoundRounds, whole)
	}
	// And it is STRICTLY BIGGER than any single round: the difference is the
	// point of the address.
	one, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--round", "1")
	if err != nil {
		t.Fatalf("--round 1 failed: %v\n%s", err, one)
	}
	if len(whole) <= len(one) {
		t.Fatalf("--turn 1 printed %d byte(s) and --round 1 printed %d: the whole turn cannot fit in one round",
			len(whole), len(one))
	}
}

// TestSessionReplayRoundWithoutATurnStatesThat is the ABSENCE arm: a round that no
// turn span covers must say so, never invent a turn number.
//
// It is asserted at the resolver that produces the sentence, because the CLI's
// loader always yields spans for a log that has rounds (a log with no operator row
// still gets one span) — so the branch is reachable through the type, not through
// this fixture, and a test that pretended otherwise would be pinning a path it
// cannot drive.
func TestSessionReplayRoundWithoutATurnStatesThat(t *testing.T) {
	mat := replayplay.Material{
		RoundCount: replayTurnRoundRounds,
		Turns: []replayplay.Round{
			// A turn whose span recorded no `request` row: no interval to state.
			{Index: 1, Kind: replayplay.RoundTurn, FirstRound: 0, LastRound: 0},
		},
	}
	if turn, _, _, _, ok := replayplay.RoundTurnOf(mat, 1); ok {
		t.Fatalf("RoundTurnOf claimed round 1 belongs to turn %d, but no span covers it — an absent interval must not be invented", turn)
	}
	if msg := sessionevent.CounterRoundNoTurn; !strings.Contains(msg, "not a turn") {
		t.Fatalf("the no-turn sentence %q does not deny being a turn", msg)
	}
	// PRESENT, in the same resolver: a span that DOES cover the round answers with
	// its number and interval, so the check above cannot pass by a resolver that
	// never answers at all.
	mat.Turns = []replayplay.Round{
		{Index: 1, Kind: replayplay.RoundTurn, FirstRound: 1, LastRound: replayTurnRoundRounds},
	}
	turn, first, last, total, ok := replayplay.RoundTurnOf(mat, 2)
	if !ok || turn != 1 || first != 1 || last != replayTurnRoundRounds || total != 1 {
		t.Fatalf("RoundTurnOf(round 2) = (turn %d, %d-%d of %d, ok=%v), want (1, 1-%d of 1, ok=true)",
			turn, first, last, total, ok, replayTurnRoundRounds)
	}
}

// replayTurnRowLabels returns the "turn k/N" label of every listing ROW, in order.
// It is the count a listing's own numbering is measured by: a row is what carries
// a label, not what happens to contain the word.
func replayTurnRowLabels(out string) []string {
	var labels []string
	for _, line := range strings.Split(out, "\n") {
		if m := replayTurnRowLabelRe.FindStringSubmatch(line); m != nil {
			labels = append(labels, m[0])
		}
	}
	return labels
}

// replayQuotedGlyphSessionID / timeline: the operator-ruler fixture. The turn's
// PROSE is a pasted transcript and its tool RESULT is a printed file, and BOTH
// carry a line opening with the tool glyph. That is the measured shape (session
// 41e5dade, 2026-10-26): `--turn 50 | grep -c '^⚙ '` printed 9 over a header
// reading `calls=7`, and `--turn 62` printed 14 for the same `calls=7`.
const (
	replayQuotedGlyphSessionID = "test-quoted-glyph-0001"
)

var replayQuotedGlyphTimeline = `{"type":"user","message":{"role":"user","content":"count what I did"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"here is the transcript I pasted:\n⚙ bash(command=pasted-one)\nit answered"},{"type":"tool_use","id":"q1","name":"read","input":{"path":"/tmp/dump.txt"}}]}}
{"type":"request","iteration":0}
{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"q1","content":"file body:\n⚙ bash(command=from-the-file)\nend"}]}}
`

// TestSessionReplayTurnRulerMatchesItsHeader is the OPERATOR'S ruler, run at the
// real CLI: a line opening with the tool glyph at column 0 must be a real call
// row, so `grep -c '^⚙ '` and the header's `calls=` agree — even when the turn's
// own content quotes the glyph at the start of a line.
//
// The check is not "count the glyph somewhere": a ✱-leading content line WAS
// printed at column 0 before this fix, which is what made the ruler lie. So the
// count is asserted on the FIRST CELL of each line, exactly as an operator's grep
// does it, and the header's own number is read out of the output rather than
// assumed.
func TestSessionReplayTurnRulerMatchesItsHeader(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(config.ProfilePathIn(root, config.SessionsDirName), replayQuotedGlyphSessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create the session dir: %v", err)
	}
	path := filepath.Join(dir, replayplay.SessionJSONLName)
	if err := os.WriteFile(path, []byte(replayQuotedGlyphTimeline), 0o600); err != nil {
		t.Fatalf("write the timeline: %v", err)
	}
	out, err := runGopiCapture(t, root, CliVerbSession, sessionSubcmdReplay, replayQuotedGlyphSessionID, "--turn", "1")
	if err != nil {
		t.Fatalf("--turn 1 failed: %v\n%s", err, out)
	}
	header, _, _ := strings.Cut(out, "\n")
	declared, perr := strconv.Atoi(strings.TrimPrefix(header[strings.LastIndex(header, "calls="):], "calls="))
	if perr != nil {
		t.Fatalf("the header %q carries no readable calls= count: %v", header, perr)
	}
	scored := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, replayTestToolCallRowPrefix) {
			scored++
		}
	}
	if scored != declared {
		t.Fatalf("`grep -c '^⚙ '` scores %d line(s) over a header reading calls=%d — a quoted line, a printed file or a wrap continuation must never begin with the marker at column 0:\n%s",
			scored, declared, out)
	}
	// The quoted lines are still PRINTED (the fix moves content, it does not hide
	// it), inset by the content column.
	for _, want := range []string{"⚙ bash(command=pasted-one)", "⚙ bash(command=from-the-file)"} {
		if !strings.Contains(out, "  "+want) {
			t.Fatalf("the quoted line %q is not printed at the content column:\n%s", want, out)
		}
	}
}
