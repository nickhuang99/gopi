package agent

// selfrecord_noteindex_test.go — the WIRING half of the turn-level note index:
// the ONE call that records a note (recordSelfNotes) must also index it.
//
// WHY THIS ARM EXISTS AT ALL. The tools-layer test (internal/tools/
// chat_notes_test.go) proves the index works when it is written; it says nothing
// about whether the AGENT ever writes it. Delete the indexSelfNotes call from
// recordSelfNotes and every test in that file still passes while the feature is
// dead — the failure this file is here to catch.
//
// The three facts it pins, all from a REAL recordSelfNotes run through a real
// Agent with a real SessionLogger under an isolated GOPI_HOME and a pinned
// ledger path:
//
//	the row EXISTS        the note is searchable afterwards
//	the provenance is RIGHT  session_id is this run's, turn_index is the TURN
//	                         it recorded in (the number the file line carries),
//	                         source_host is this machine and source_path is the
//	                         record file's FULL path
//	the FILE is UNCHANGED    self-record.md still holds exactly the note, one
//	                         line, as before — the index is a second layer, not a
//	                         replacement (第一期's semantics are the constraint)

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// TestRecordSelfNotesAlsoIndexesThem is the wiring contract.
func TestRecordSelfNotesAlsoIndexesThem(t *testing.T) {
	// The ledger the tools layer resolves: the fixture pins GOPI_MEMORY_DB at a
	// fresh per-test file (Rule 32), and chatDBPath falls back to exactly that
	// path (GOPI_CHAT_DB unset).
	testutil.NewTestDBPath(t)
	// The run's own profile: GopiDir resolves <GOPI_HOME>/.gopi, so the session
	// directory the note file lands in is inside this test's tree.
	t.Setenv(config.GopiDirEnv, t.TempDir())
	// The machine identity the row must carry is this process's own — no
	// simulated node in the ambient environment.
	t.Setenv("GOPI_SYNC_NODE", "")

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	ag := &Agent{SessionLog: sl, Logger: log.New(io.Discard, "", 0)}
	// THE TURN HAS TO BE OPENED, because a turn is what the stamp numbers now:
	// one COMPLETE INPUT — the person's line — is written through the production
	// writer, which advances the session's own turn count under the reader's
	// predicate (sessionevent.IsTurnOpeningRow). Without this row the run has no
	// turn at all and the absence stamp `t? ` is the honest answer (which the
	// parser arm below pins separately).
	if err := sl.LogUser("selfrecord-noteindex-prompt", "the operator's line that opens turn one"); err != nil {
		t.Fatalf("open the turn: %v", err)
	}

	const needle = "note-index-wiring-needle"
	content := "answer text\n\n" + selfRecordOpenMarker + "\n" +
		"wired the round-level index and recorded the " + needle + " finding\n" +
		selfRecordCloseMarker + "\n"
	notes := ag.recordSelfNotes(content, "")
	if len(notes) != 1 {
		t.Fatalf("recordSelfNotes returned %d note(s), want 1", len(notes))
	}

	// THE FILE IS UNCHANGED: still exactly one line, still the note, still
	// where it always was.
	path, err := selfRecordPathFor(sl.SessionID(), sl.TaskID())
	if err != nil {
		t.Fatalf("record path: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the record file must exist after a recorded round: %v", err)
	}
	if got := strings.Count(strings.TrimSpace(string(raw)), "\n"); got != 0 {
		t.Fatalf("the record file carries %d extra lines, want the note alone:\n%s", got, raw)
	}
	if !strings.Contains(string(raw), needle) {
		t.Fatalf("the record file lost its note:\n%s", raw)
	}

	// THE ROW EXISTS, with the right provenance.
	out, err := tools.ChatNoteSearch(needle, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, needle) {
		t.Fatalf("the note the agent recorded is not searchable — the index write is not wired:\n%s", out)
	}
	if !strings.Contains(out, sl.SessionID()) {
		t.Fatalf("the row must carry this run's session id (%s):\n%s", sl.SessionID(), out)
	}
	if !strings.Contains(out, "turn 1") {
		t.Fatalf("the row must carry the TURN the note was recorded in:\n%s", out)
	}
	if !strings.Contains(out, tools.OriginHostName()) {
		t.Fatalf("the row must carry this machine's identity (%s):\n%s", tools.OriginHostName(), out)
	}
	if !strings.Contains(out, filepath.Base(path)) || !strings.Contains(out, filepath.Dir(path)) {
		t.Fatalf("the row must carry the record file's FULL path (%s):\n%s", path, out)
	}
}

// countSelfRecordNoteLines counts the note lines in a record file through the
// record layer's OWN parser — one definition of "what a line is", so a test
// cannot pass by looking for a spelling the writer no longer uses.
func countSelfRecordNoteLines(body string) int {
	n := 0
	for _, line := range strings.Split(body, "\n") {
		if _, note, ok := SelfRecordNoteFromLine(line); ok && strings.TrimSpace(note) != "" {
			n++
		}
	}
	return n
}

// TestSelfRecordLineCarriesTheTurnTheIndexStores is the epic's central pin: the
// FILE line (`t<turn> <note>`) and the INDEX row (chat_notes.turn_index) carry
// the SAME number, because recordSelfNotes evaluates noteTurnNumber ONCE and
// hands that one value to both writers.
//
// AND THE NUMBER IS THE TURN, NOT THE ROUND — the measured distinction this arm
// exists for. Two notes are recorded inside ONE turn (a turn is one complete
// input; the agent ran several rounds answering it), and both must be stamped
// with the SAME number; a second turn then gets the next one. Under the retired
// round stamp those two notes carried two different numbers, which is exactly
// why the operator asked for the change: a turn that ran twelve tool rounds
// produced twelve differently-numbered notes that no surface addresses.
//
// It is written against the real write path (recordSelfNotes → file + index)
// rather than against the two functions separately: a test that called
// appendSelfRecordFile with its own number would prove the formatter works and
// say nothing about the number the production path stamps.
func TestSelfRecordLineCarriesTheTurnTheIndexStores(t *testing.T) {
	testutil.NewTestDBPath(t)
	t.Setenv(config.GopiDirEnv, t.TempDir())
	t.Setenv("GOPI_SYNC_NODE", "")

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	ag := &Agent{SessionLog: sl, Logger: log.New(io.Discard, "", 0)}

	record := func(needle string) {
		t.Helper()
		content := "answer text\n\n" + selfRecordOpenMarker + "\n" + needle + "\n" + selfRecordCloseMarker + "\n"
		if got := ag.recordSelfNotes(content, ""); len(got) != 1 {
			t.Fatalf("recordSelfNotes returned %d note(s) for %q, want 1: %v", len(got), needle, got)
		}
	}
	// TURN ONE: the operator's line, then two recorded notes — with a `request`
	// row in between, i.e. two ROUNDS of one turn. Both notes belong to turn 1.
	if err := sl.LogUser("prompt-turn-one", "the operator's first line"); err != nil {
		t.Fatalf("open turn one: %v", err)
	}
	record("turn-one-first-needle")
	if err := sl.LogRequest(0, 0, 0, 0, 0); err != nil {
		t.Fatalf("log a request inside turn one: %v", err)
	}
	record("turn-one-second-needle")
	// TURN TWO: a new complete input — the same turn count moves by one, and only
	// because a turn OPENED (sessionevent.IsTurnOpeningRow, through the writer).
	if err := sl.LogUser("prompt-turn-two", "the operator's second line"); err != nil {
		t.Fatalf("open turn two: %v", err)
	}
	record("turn-two-needle")

	path, err := selfRecordPathFor(sl.SessionID(), sl.TaskID())
	if err != nil {
		t.Fatalf("record path: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	// THE FILE: one stamped line per note, and the stamp is the TURN.
	type fileLine struct {
		turn int
		note string
	}
	var lines []fileLine
	for _, line := range strings.Split(string(raw), "\n") {
		turn, note, ok := SelfRecordNoteFromLine(line)
		if !ok || strings.TrimSpace(note) == "" {
			continue
		}
		if unit := selfRecordStampUnitOf(line); unit != selfRecordStampUnitTurn {
			t.Fatalf("a newly written line must be on the TURN axis, this one says %q:\n%s", unit, line)
		}
		lines = append(lines, fileLine{turn: turn, note: strings.TrimSpace(note)})
	}
	if len(lines) != 3 {
		t.Fatalf("the record file holds %d note line(s), want 3:\n%s", len(lines), raw)
	}
	wantTurns := []int{1, 1, 2}
	for i, want := range wantTurns {
		if lines[i].turn != want {
			t.Fatalf("line %d (%q) is stamped t%d, want t%d — one evaluation of noteTurnNumber per note, and a TURN spans its rounds:\n%s",
				i, lines[i].note, lines[i].turn, want, raw)
		}
	}
	for _, want := range []string{
		selfRecordLinePrefix(1) + "turn-one-first-needle",
		selfRecordLinePrefix(1) + "turn-one-second-needle",
		selfRecordLinePrefix(2) + "turn-two-needle",
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("the file line is not spelled %q:\n%s", want, raw)
		}
	}

	// THE INDEX: the SAME number, note for note. Compared through the search the
	// operator uses (`gopi session notes` renders `turn N`), so this pin holds
	// the surface the acceptance criterion is written against.
	for _, want := range []fileLine{
		{turn: 1, note: "turn-one-first-needle"},
		{turn: 1, note: "turn-one-second-needle"},
		{turn: 2, note: "turn-two-needle"},
	} {
		out, err := tools.ChatNoteSearch(want.note, 0)
		if err != nil {
			t.Fatalf("search %q: %v", want.note, err)
		}
		if !strings.Contains(out, tools.ChatNoteTurnWord()+selfRecordSpace+itoaSelfRecord(want.turn)) {
			t.Fatalf("the index row for %q does not carry turn %d — the file line and the row disagree:\n%s",
				want.note, want.turn, out)
		}
	}
}

// selfRecordSpace is the separator the index render puts between the unit and
// the number ("turn 3") — spelled once here so the test above reads the same
// fact the renderer writes, rather than a literal with a stray double space.
const selfRecordSpace = " "

// TestSelfRecordEveryShapeIsReadByOneParser pins the APPEND-ONLY half: the file
// is evidence and is never rewritten, so it now holds THREE shapes and every
// reader goes through ONE parser. Nothing here may be skipped as prose.
//
//	`t7 …`  the shape written from the TURN epic on   → turn 7
//	`r9 …`  the RETIRED round stamp (2026-11-08 …)    → round 9, still a note
//	`- …`   every line written before any stamp       → no number, still a note
//
// The number and the UNIT are two questions about one parse
// (selfRecordStampUnitOf): a reader that prints a historical round as a turn is
// the silent substitution Rule 15 forbids, so both are pinned per shape.
func TestSelfRecordEveryShapeIsReadByOneParser(t *testing.T) {
	cases := []struct {
		line   string
		number int
		unit   string
		note   string
		isNote bool
	}{
		{"t7 the turn-stamped note", 7, selfRecordStampUnitTurn, "the turn-stamped note", true},
		{"t? a note whose turn could not be resolved", sessionevent.RoundIndexUnknown, selfRecordStampUnitTurn, "a note whose turn could not be resolved", true},
		{"r9 the retired round-stamped note", 9, selfRecordStampUnitRound, "the retired round-stamped note", true},
		{"r? a note whose round could not be resolved", sessionevent.RoundIndexUnknown, selfRecordStampUnitRound, "a note whose round could not be resolved", true},
		{"- a note written before any stamp existed", sessionevent.RoundIndexUnknown, "", "a note written before any stamp existed", true},
		{"- - a legacy note that begins with a dash", sessionevent.RoundIndexUnknown, "", "- a legacy note that begins with a dash", true},
		{"t0 is not a turn below the base", sessionevent.RoundIndexUnknown, "", "", false},
		{"r0 is not a round below the base", sessionevent.RoundIndexUnknown, "", "", false},
		{"t7a note with no separator is prose", sessionevent.RoundIndexUnknown, "", "", false},
		{"turn 7 is prose, not a stamp", sessionevent.RoundIndexUnknown, "", "", false},
		{"round 7 is prose, not a stamp", sessionevent.RoundIndexUnknown, "", "", false},
		{"", sessionevent.RoundIndexUnknown, "", "", false},
	}
	for _, c := range cases {
		number, note, ok := SelfRecordNoteFromLine(c.line)
		if ok != c.isNote {
			t.Fatalf("%q: parsed as a note = %v, want %v", c.line, ok, c.isNote)
		}
		if !ok {
			continue
		}
		if number != c.number {
			t.Fatalf("%q: number = %d, want %d", c.line, number, c.number)
		}
		if note != c.note {
			t.Fatalf("%q: note = %q, want %q", c.line, note, c.note)
		}
		if unit := selfRecordStampUnitOf(c.line); unit != c.unit {
			t.Fatalf("%q: unit = %q, want %q — a historical round must never be presented as a turn", c.line, unit, c.unit)
		}
	}
}

// TestSelfRecordParserReadsAFileOfEveryShape is the MIGRATION half, at file
// scale: a record file that has lived through both stamp families and the
// pre-stamp era is read end to end by the one parser, in order, losing nothing
// and inventing nothing.
//
// It is the acceptance the operator stated — "历史行保持可读（同一解析器）" — as an
// assertion rather than a hope, and it is the shape a real run file has on this
// host today.
func TestSelfRecordParserReadsAFileOfEveryShape(t *testing.T) {
	const body = "- the oldest note, before any stamp\n" +
		"r3 the round era's first note\n" +
		"r3 the round era's second note (same round)\n" +
		"r? a round that never resolved\n" +
		"t1 the turn era begins\n" +
		"t2 the next turn\n"
	type row struct {
		number int
		unit   string
		note   string
	}
	var got []row
	for _, line := range strings.Split(body, "\n") {
		number, note, ok := SelfRecordNoteFromLine(line)
		if !ok {
			continue
		}
		got = append(got, row{number: number, unit: selfRecordStampUnitOf(line), note: strings.TrimSpace(note)})
	}
	want := []row{
		{number: sessionevent.RoundIndexUnknown, unit: "", note: "the oldest note, before any stamp"},
		{number: 3, unit: selfRecordStampUnitRound, note: "the round era's first note"},
		{number: 3, unit: selfRecordStampUnitRound, note: "the round era's second note (same round)"},
		{number: sessionevent.RoundIndexUnknown, unit: selfRecordStampUnitRound, note: "a round that never resolved"},
		{number: 1, unit: selfRecordStampUnitTurn, note: "the turn era begins"},
		{number: 2, unit: selfRecordStampUnitTurn, note: "the next turn"},
	}
	if len(got) != len(want) {
		t.Fatalf("the parser read %d note line(s) out of %d, want %d — a shape was skipped as prose", len(got), len(want), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d parsed as %+v, want %+v", i, got[i], want[i])
		}
	}
}
