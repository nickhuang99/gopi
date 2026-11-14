// address_test.go — the regression gate for the axis defect this file's
// subject repairs (Rule 5: every fix ships a minimal repro).
//
// THE DEFECT, measured on a REAL session (01a196e8-5e96-7c4c-b5ac-4da55c0104bc,
// 2026-11-01): `gopi session log <id> --turns` reported 2 turns, and the export
// written beside it was `context-turn336.md` — the session's 336th HTTP
// `→ REQUEST` record, under the word every other surface reserves for a TURN.
//
// THE FIXTURE SEPARATES THE TWO AXES THE SMALLEST WAY THAT CAN STILL DISAGREE
// (Rule 10.4): TWO operator rows answered by FOUR request rows. A fixture with
// one turn, or with as many rounds as turns, is blind — both addresses print the
// same number and these assertions cannot fail.
package ctxdump

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// turnAxisFixture is the count of turns and rounds the fixture below records,
// named so the assertions read "want the fixture's own numbers" instead of
// restating them (Rule 1).
const (
	fixtureTurns  = 2
	fixtureRounds = 4
)

// writeAxisFixture writes a session record with fixtureTurns turns and
// fixtureRounds request rows, plus the raw HTTP log carrying those rounds.
//
// The turn boundaries are REAL, not simulated: each operator row is a plain
// `user` row with no harness prefix, which is exactly what
// sessionevent.IsTurnOpeningRow answers "a turn opens here" for. That is the
// point — this test proves the export reads the SAME predicate every other
// surface reads, so a change to the predicate moves both sides together.
func writeAxisFixture(t *testing.T) (sessionPath, rawLogPath string) {
	t.Helper()
	dir := t.TempDir()
	sessionPath = filepath.Join(dir, "fixture.jsonl")

	lines := []string{
		`{"type":"session_start","sessionId":"axis-fixture","model":"fixture","timestamp":"2026-11-01T00:00:00Z"}`,
	}
	// Each turn is ONE operator row answered by TWO rounds. An `assistant` row
	// carries the content the reader folds into an iteration, and each `request`
	// row flushes the content before it, stamping that content with the round's
	// canonical ordinal — so the fixture produces real iterations, real
	// RequestOrdinals and a turn span that covers an INTERVAL of rounds rather
	// than a single one.
	round := 0
	for turn := 1; turn <= fixtureTurns; turn++ {
		lines = append(lines, fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"ask %d"}}`, turn))
		for i := 0; i < fixtureRounds/fixtureTurns; i++ {
			round++
			lines = append(lines, fmt.Sprintf(
				`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer %d"}]}}`,
				round))
			lines = append(lines, fmt.Sprintf(
				`{"type":"request","iteration":%d,"prompt_tokens":10,"cache_hit":1,"cache_miss":9,"completion_tokens":2,"tagged_usage":true,"usage_base":"delta"}`,
				round))
		}
	}
	if err := os.WriteFile(sessionPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write session fixture: %v", err)
	}

	rawLogPath = filepath.Join(dir, "fixture.raw.log")
	f, err := os.Create(rawLogPath)
	if err != nil {
		t.Fatalf("create raw log fixture: %v", err)
	}
	defer f.Close()
	for i := 1; i <= fixtureRounds; i++ {
		req := deepseek.ClaudeRequest{
			Model: "fixture",
			Messages: []deepseek.ClaudeMessage{{
				Role:    "user",
				Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: fmt.Sprintf("round-%d", i)}},
			}},
		}
		body, merr := json.Marshal(req)
		if merr != nil {
			t.Fatalf("marshal round %d: %v", i, merr)
		}
		if _, werr := f.Write(rawlogfmt.Encode(rawlogfmt.TagRequest, body)); werr != nil {
			t.Fatalf("write round %d: %v", i, werr)
		}
	}
	return sessionPath, rawLogPath
}

// TestTurnAddressIsTheSessionsOwnTurnNotTheRequestOrdinal is the defect itself:
// the session's LAST turn is turn 2 of 2, and the round it renders is 4. Before
// this axis existed the export called it "turn 4" — one refusal away from
// `--turn 4`, which does not exist.
func TestTurnAddressIsTheSessionsOwnTurnNotTheRequestOrdinal(t *testing.T) {
	sessionPath, rawLogPath := writeAxisFixture(t)

	sess, err := gopilog.ParseFile(sessionPath)
	if err != nil {
		t.Fatalf("parse the fixture session: %v", err)
	}
	if got := gopilog.SessionTurns(sess); got != fixtureTurns {
		t.Fatalf("the fixture records %d turn(s), want %d — the assertions below would be measuring the wrong fixture", got, fixtureTurns)
	}

	last, err := TurnAddressFor(sessionPath, 0)
	if err != nil {
		t.Fatalf("TurnAddressFor(last): %v", err)
	}
	if last.Turn != fixtureTurns {
		t.Errorf("the last export is turn %d, want %d (the session's own turn count)", last.Turn, fixtureTurns)
	}
	if last.Round != fixtureRounds {
		t.Errorf("the last export renders round %d, want %d (the round whose body it holds)", last.Round, fixtureRounds)
	}
	if last.FirstRound == last.LastRound {
		t.Errorf("the turn states the round interval %d–%d, which is ONE round — the fixture holds two per turn, so the interval is not being read off the turn's span", last.FirstRound, last.LastRound)
	}

	first, err := TurnAddressFor(sessionPath, 1)
	if err != nil {
		t.Fatalf("TurnAddressFor(1): %v", err)
	}
	if first.Turn != 1 || first.FirstRound != 1 || first.LastRound != fixtureRounds/fixtureTurns {
		t.Errorf("turn 1 = %+v, want turn 1 covering rounds 1–%d", first, fixtureRounds/fixtureTurns)
	}

	// PAST THE END IS REFUSED BY NAME, with the count that would have worked.
	// Addressing a turn by a ROUND number is the mistake this epic exists to
	// stop, so the refusal must name the turn count, not the round count.
	_, err = TurnAddressFor(sessionPath, fixtureRounds)
	if err == nil {
		t.Fatalf("TurnAddressFor(%d) succeeded on a %d-turn session — a round number was accepted as a turn", fixtureRounds, fixtureTurns)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d turn(s)", fixtureTurns)) {
		t.Errorf("the out-of-range refusal does not name the session's turn count: %v", err)
	}

	// And the request body the turn-scoped export holds really is that round.
	req, err := RequestAtTurn(rawLogPath, last)
	if err != nil {
		t.Fatalf("RequestAtTurn: %v", err)
	}
	if got := req.Messages[0].Content[0].Text; got != fmt.Sprintf("round-%d", fixtureRounds) {
		t.Errorf("the turn-scoped export holds %q, want the request of round %d", got, fixtureRounds)
	}
}

// TestTheArtifactNameAndHeaderStateTheSameAddress is the epic's acceptance in
// one assertion: on BOTH axes, what the file is CALLED and what it SAYS are the
// same address — and each says which axis it is in.
func TestTheArtifactNameAndHeaderStateTheSameAddress(t *testing.T) {
	sessionPath, rawLogPath := writeAxisFixture(t)
	out := t.TempDir()

	// --- the TURN axis ---
	turnAddr, err := TurnAddressFor(sessionPath, 2)
	if err != nil {
		t.Fatalf("TurnAddressFor(2): %v", err)
	}
	req, err := RequestAtTurn(rawLogPath, turnAddr)
	if err != nil {
		t.Fatalf("RequestAtTurn: %v", err)
	}
	sum, err := Write(out, req, turnAddr)
	if err != nil {
		t.Fatalf("Write(turn axis): %v", err)
	}
	if want := fmt.Sprintf(FileNameFormat, 2); filepath.Base(sum.Path) != want {
		t.Errorf("the turn-scoped file is named %q, want %q", filepath.Base(sum.Path), want)
	}
	page := readPage(t, sum.Path)
	if !strings.Contains(page, fmt.Sprintf(PageTitleTurnFormat, 2)) {
		t.Errorf("the page header does not state turn 2:\n%s", sectionAround(page, "# Model request"))
	}
	// THE TURN-SCOPED FURNITURE SPEAKS ONLY OF THE TURN (operator ruling, 2026-11-10):
	// no round interval row, no "rendered round" row, and the axial note is gone,
	// because a round number is a model-layer detail on a page an operator reads in
	// turns. The measure is the FURNITURE — everything above `## System prompt` —
	// never the whole file: the model's own system text is verbatim and does
	// mention rounds, and that bytes rule wins.
	if n := furnitureRoundHits(t, page); n != 0 {
		t.Errorf("the turn-scoped page's own furniture carries %d round mention(s), want 0:\n%s",
			n, furniture(page))
	}
	for _, want := range []string{
		"| turn | 2 |",
		"round-4", // the bytes really are that round's body
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not carry %q — the name would be a claim the bytes do not support:\n%s",
				want, sectionAround(page, "## Request"))
		}
	}

	// --- the ROUND axis ---
	roundAddr := RoundAddress(3)
	req3, err := RequestAtTurn(rawLogPath, roundAddr)
	if err != nil {
		t.Fatalf("RequestAtTurn(round 3): %v", err)
	}
	rsum, err := Write(out, req3, roundAddr)
	if err != nil {
		t.Fatalf("Write(round axis): %v", err)
	}
	if want := fmt.Sprintf(RoundFileNameFormat, 3); filepath.Base(rsum.Path) != want {
		t.Errorf("the round-scoped file is named %q, want %q", filepath.Base(rsum.Path), want)
	}
	rpage := readPage(t, rsum.Path)
	if !strings.Contains(rpage, fmt.Sprintf(PageTitleRoundFormat, 3)) {
		t.Errorf("the round-scoped page does not say it is a ROUND:\n%s", sectionAround(rpage, "# Model request"))
	}
	if !strings.Contains(rpage, "round-3") {
		t.Errorf("the round-scoped page does not hold round 3's body:\n%s", sectionAround(rpage, "## Request"))
	}
	// A round-scoped page must not CLAIM a turn: the word may appear only to say
	// it is not one (its title carries "NOT a turn"), never as an address row.
	if strings.Contains(rpage, "| turn | 3 |") {
		t.Errorf("the round-scoped page states a TURN row — a round is not a turn:\n%s", sectionAround(rpage, "## Request"))
	}

	// THE SCREEN LINE STATES THE SAME ADDRESS (one line, per the epic's rule).
	for _, line := range []string{sum.Line(), rsum.Line()} {
		if n := strings.Count(strings.TrimRight(line, "\n"), "\n"); n != 0 {
			t.Errorf("the console line is %d lines long, want 1: %q", n+1, line)
		}
	}
	if !strings.Contains(sum.Line(), fmt.Sprintf("TURN %d", 2)) {
		t.Errorf("the turn-scoped console line does not state the turn: %q", sum.Line())
	}
	if !strings.Contains(rsum.Line(), fmt.Sprintf("ROUND %d", 3)) {
		t.Errorf("the round-scoped console line does not state the round: %q", rsum.Line())
	}
}

// TestAHistoricalRoundNamedFileIsReportedNotMigrated pins the epic's fourth
// constraint: a pre-axis artifact is LEFT WHERE IT IS, and the caller is told so
// rather than the tree being rewritten to match a new vocabulary.
func TestAHistoricalRoundNamedFileIsReportedNotMigrated(t *testing.T) {
	dir := t.TempDir()
	// The name a pre-axis export wrote for ROUND 4 in a 2-turn session.
	legacy := filepath.Join(dir, fmt.Sprintf(LegacyRoundNameFormat, 4))
	if err := os.WriteFile(legacy, []byte("old artifact\n"), 0o600); err != nil {
		t.Fatalf("write the historical artifact: %v", err)
	}
	note := RoundAliasReport(dir, TurnAddress(2, 3, 4, 4))
	if !strings.Contains(note, legacy) || !strings.Contains(note, "ROUND 4") {
		t.Fatalf("the historical artifact is not named in the note: %q", note)
	}
	if !strings.Contains(note, "not migrated") {
		t.Errorf("the note does not say the historical file was left in place: %q", note)
	}
	// It was NOT rewritten: the file's own bytes are untouched.
	got, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatalf("the historical artifact is gone: %v", err)
	}
	if string(got) != "old artifact\n" {
		t.Errorf("the historical artifact was rewritten to %q — historical files are not migrated", string(got))
	}
	// A round-scoped export has no legacy collision to report (it names a
	// different round space), and a turn with no collision reports nothing.
	if note := RoundAliasReport(dir, RoundAddress(4)); note != "" {
		t.Errorf("a round-scoped export reported a turn-axis collision: %q", note)
	}
	if note := RoundAliasReport(dir, TurnAddress(2, 3, 4, 2)); note != "" {
		t.Errorf("a turn with no colliding artifact reported one: %q", note)
	}
	// THE NOTE REQUIRES THE FILE TO EXIST (operator ruling, 2026-11-10). A turn
	// whose round differs from its ordinal is the ordinary case on every session
	// (`turn 2` renders a request the old scheme would have named `context-turnN`
	// for some large N), so an arithmetic-only note fired on EVERY ordinary
	// export and put a ROUND number on stderr in the channel the ruling cleans.
	// An empty directory therefore reports NOTHING.
	empty := t.TempDir()
	if note := RoundAliasReport(empty, TurnAddress(2, 3, 4, 4)); note != "" {
		t.Errorf("a directory with no historical artifact reported one (%q) — the note must be evidence of a file, never of arithmetic", note)
	}
	// A DIRECTORY wearing the legacy name is not an artifact either.
	if err := os.Mkdir(filepath.Join(empty, fmt.Sprintf(LegacyRoundNameFormat, 4)), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if note := RoundAliasReport(empty, TurnAddress(2, 3, 4, 4)); note != "" {
		t.Errorf("a directory named like the historical artifact produced a note (%q)", note)
	}

	// An ordinary export produces NO note — the screen keeps its one line, and a
	// session with nothing to report costs no noise.
	plain := Summary{Path: filepath.Join(dir, "x.md"), Address: TurnAddress(2, 3, 4, 4)}
	if got := plain.Notes(); len(got) != 0 {
		t.Errorf("an ordinary export produced %d note(s): %v", len(got), got)
	}
	noted := Summary{Alias: RoundAliasReport(dir, TurnAddress(2, 3, 4, 4))}
	if got := noted.Notes(); len(got) != 1 || !strings.Contains(got[0], legacy) {
		t.Errorf("the historical note did not reach Summary.Notes(): %v", got)
	}
}

// TestAnUnknownAxisIsRefusedByName pins Rule 15 on the one input that would
// otherwise produce a file whose name describes no axis at all.
func TestAnUnknownAxisIsRefusedByName(t *testing.T) {
	if err := (Address{Axis: Axis("epoch"), Turn: 1, Round: 1}).Validate(); err == nil {
		t.Fatal("Validate accepted an unknown axis — the artifact's name would describe no address space")
	}
	if err := RoundAddress(0).Validate(); err == nil {
		t.Error("Validate accepted round 0")
	}
	if err := TurnAddress(0, 0, 0, 1).Validate(); err == nil {
		t.Error("Validate accepted turn 0 — the artifact would be named for a turn it cannot hold")
	}
}

// readPage reads a written artifact.
func readPage(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// TestTheRefusalNamesTheSurfaceThatPrintsThisAxis is the regression gate for a
// defect this file's own first cut INTRODUCED (found 2026-11-08 while writing
// the report, on session a60787b0-9081-451f-bd5a-9e396cfc048b).
//
// THE DEFECT: every refusal here cited `gopi session log <id> --turns` as the
// surface a reader could check the turn count against. That command tabulates
// the PERSON-ONLY SUBSET (gopilog.SessionHumanTurns); this export walks the
// session's WHOLE turn axis (gopilog.SessionTurns). On the real session those
// were 8 and 16 — so the refusal said "holds 16 turn(s)" and sent the reader to
// a command that answered "8". That is the epic's own defect (two numbers, one
// word, no statement of which is which) moved out one layer, and a refusal that
// misdirects is worse than one that says nothing.
//
// THE FIXTURE MUST SEPARATE THEM OR THE TEST IS BLIND (Rule 10.4): a fixture
// whose turns are all human records the same number on both axes, so both
// candidate witnesses pass every assertion and the test can never fail. The
// fixture below opens one turn from a `notify` delivery, which the reader counts
// as a turn (a run receives no operator input at all, so an unnoticed notify
// must still be one turn) but which is NOT a person face — so `turns` and
// `human turns` differ, and only the right witness satisfies the assertion.
func TestTheRefusalNamesTheSurfaceThatPrintsThisAxis(t *testing.T) {
	const (
		allTurns   = 2 // one human ask + one notify delivery
		humanTurns = 1 // only the first is a person face
	)
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "witness.jsonl")

	lines := []string{
		`{"type":"session_start","sessionId":"witness-fixture","model":"fixture","timestamp":"2026-11-01T00:00:00Z"}`,
		// turn 1 — the person's own ask.
		`{"type":"user","message":{"role":"user","content":"ask one"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer one"}]}}`,
		`{"type":"request","iteration":1,"prompt_tokens":10,"cache_hit":1,"cache_miss":9,"completion_tokens":2,"tagged_usage":true,"usage_base":"delta"}`,
		// turn 2 — a `notify` delivery. It opens a turn (sessionevent
		// .IsTurnOpeningRow's notify arm) but it is not a person face, so it is
		// counted by SessionTurns and skipped by SessionHumanTurns.
		`{"type":"user","source":"notify","message":{"role":"user","content":"a delivered message"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer two"}]}}`,
		`{"type":"request","iteration":2,"prompt_tokens":10,"cache_hit":1,"cache_miss":9,"completion_tokens":2,"tagged_usage":true,"usage_base":"delta"}`,
	}
	if err := os.WriteFile(sessionPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write witness fixture: %v", err)
	}

	sess, err := gopilog.ParseFile(sessionPath)
	if err != nil {
		t.Fatalf("parse the witness fixture: %v", err)
	}
	// THE FIXTURE'S OWN PREMISE, asserted before anything is judged by it.
	if got := gopilog.SessionTurns(sess); got != allTurns {
		t.Fatalf("the fixture records %d turn(s), want %d — the notify row did not open a turn, so this test would judge nothing", got, allTurns)
	}
	if got := gopilog.SessionHumanTurns(sess); got != humanTurns {
		t.Fatalf("the fixture records %d human turn(s), want %d — the two axes do not separate, so a witness pointing at either one would pass", got, humanTurns)
	}

	// The refusal, taking the address one past the end.
	_, err = TurnAddressFor(sessionPath, allTurns+1)
	if err == nil {
		t.Fatalf("TurnAddressFor(%d) succeeded on a %d-turn session", allTurns+1, allTurns)
	}
	msg := err.Error()

	// THE COUNT NAMED MUST BE THIS AXIS'S COUNT — the whole axis, not the subset.
	if !strings.Contains(msg, fmt.Sprintf("%d turn(s)", allTurns)) {
		t.Errorf("the refusal does not name the session's %d turn(s):\n%s", allTurns, msg)
	}
	// AND THE WITNESS IT NAMES MUST PRINT THAT NUMBER — `session show`, by name...
	if !strings.Contains(msg, "gopi session show") {
		t.Errorf("the refusal names no surface that prints this axis's count, so a reader cannot check it:\n%s", msg)
	}
	// ...and it must WARN AWAY from the subset, because the subset is the
	// command a reader reaches for first and it answers a different number.
	if !strings.Contains(msg, gopilog.HumanTurnUnitWords()) &&
		!strings.Contains(msg, "human-turn") &&
		!strings.Contains(msg, "human turn") {
		t.Errorf("the refusal does not say the other command prints the human-turn subset, so a reader who runs it concludes the export is broken:\n%s", msg)
	}
	// THE CONSTANT IS THE ONE EVERY REFUSAL USES (Rule 14): a second spelling
	// would put the wrong witness back in one code path and not the other.
	if TurnWitnessWords == "" {
		t.Fatal("TurnWitnessWords is empty — every turn-axis refusal would end in a bare dash")
	}
	if !strings.Contains(msg, TurnWitnessWords) {
		t.Errorf("this refusal does not end in TurnWitnessWords, so it was spelled by hand:\n%s", msg)
	}
}
