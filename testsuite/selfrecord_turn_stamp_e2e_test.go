package testsuite

// selfrecord_turn_stamp_e2e_test.go — the TURN stamp, measured END TO END
// (PROJECT.md Rule 31): one REAL `gopi chat --once` run, and then the two files
// that run wrote are compared to each other.
//
// WHAT IT PROVES, and why nothing smaller can.
//
// A unit arm can show that noteTurnNumber returns whatever a fake logger says.
// It cannot show that the number a note line carries is the number the READER
// (`gopi session log --turns`, `gopi session replay --turn N`) gives the same
// turn — because those two numbers are produced by two different halves of the
// system: the writer's count (SessionLogger.TurnsSoFar, advanced under
// sessionevent.IsTurnOpeningRow) and the reader's (gopilog.SessionTurnBounds,
// which applies the SAME predicate to the recorded rows). This walk runs the
// binary, then reads BOTH artifacts it left behind:
//
//	<home>/.gopi/sessions/<run>/self-record.md   the notes, stamped t<N>
//	<home>/.gopi/sessions/<run>/session.jsonl    the rows the reader numbers
//
// and requires the multi-set of stamps to equal the session's own turn axis.
// The two answers come from opposite ends of one run; if they agree, the stamp
// is the reader's number and not a second opinion about it.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/testutil"
)

// The notes each turn records, and the answers that carry them. Two operator
// lines = two turns; turn one takes TWO rounds (a tool call first), so it also
// pins the property the epic is about: one turn may produce several notes, and
// they must all carry the SAME number.
const (
	turnStampNoteOne = "turn-stamp-e2e first note: one turn, several rounds"
	turnStampNoteTwo = "turn-stamp-e2e second note"
)

// TestSelfRecordStampsTheTurnTheSessionLogNumbers is the acceptance walk.
func TestSelfRecordStampsTheTurnTheSessionLogNumbers(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	probe := "turn-stamp-probe.txt"
	if err := os.WriteFile(filepath.Join(work, probe), []byte("probe\n"), 0o644); err != nil {
		t.Fatalf("fixture write: %v", err)
	}

	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ // turn 1, round 1: a tool call
			ID:   "call_turn_stamp",
			Name: "read",
			Args: map[string]interface{}{"path": probe},
		}},
		testutil.Resp{ // turn 1, round 2: the note of turn one
			Text: "read it\n[IMPORTANT]\n" + turnStampNoteOne + "\n[/IMPORTANT]",
		},
		testutil.Resp{ // turn 2: the note of turn two
			Text: "second answer\n[IMPORTANT]\n" + turnStampNoteTwo + "\n[/IMPORTANT]",
		},
	)
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"},
		// The transcript opens with a COMMAND, which is what makes `--once` read it
		// as a scripted session (chat_once_seq.go) — two operator lines, therefore
		// two turns; the `/tools` line itself is a local command, not an input.
		"/tools\nfirst operator line\nsecond operator line\n/quit\n",
		testutil.MockEnv(t, home, llm.URL()), work)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}

	// THE FILE: every note line, read through the record layer's ONE parser.
	recordPath := turnStampFindOne(t, home, "self-record.md")
	body, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read %s: %v", recordPath, err)
	}
	type stamped struct {
		number int
		unit   string
		note   string
	}
	var fromFile []stamped
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		number, note, ok := agent.SelfRecordNoteFromLine(line)
		if !ok || strings.TrimSpace(note) == "" {
			continue
		}
		fromFile = append(fromFile, stamped{
			number: number,
			unit:   agent.SelfRecordStampUnitOf(line),
			note:   strings.TrimSpace(note),
		})
	}
	if len(fromFile) == 0 {
		t.Fatalf("%s holds no note line:\n%s", recordPath, body)
	}
	for _, got := range fromFile {
		if got.unit != agent.SelfRecordStampUnitTurnWord() {
			t.Fatalf("a line written by THIS run must be stamped on the TURN axis, this one is on %q:\n%s",
				got.unit, body)
		}
		if got.number < 1 {
			t.Fatalf("the stamp carries no number for %q — the run could not resolve its turn:\n%s", got.note, body)
		}
	}

	// THE SESSION LOG: the reader's own turn axis, from the same run.
	sessionPath := turnStampFindOne(t, home, "session.jsonl")
	sess, err := gopilog.ParseFile(sessionPath)
	if err != nil {
		t.Fatalf("parse %s: %v", sessionPath, err)
	}
	readerTurns := gopilog.SessionTurns(sess)
	if readerTurns != 2 {
		t.Fatalf("the reader numbers %d turn(s) for a scripted session of two operator lines — the fixture did not create the two turns this arm compares", readerTurns)
	}
	rows := gopilog.SessionTurnRows(sess)
	readerOrdinals := make([]int, 0, len(rows))
	for _, r := range rows {
		readerOrdinals = append(readerOrdinals, r.Turn)
	}

	// THE COMPARISON — the file's stamps against the reader's ordinals.
	var fileOrdinals []int
	for _, got := range fromFile {
		fileOrdinals = append(fileOrdinals, got.number)
	}
	sort.Ints(fileOrdinals)
	sort.Ints(readerOrdinals)
	fileSet := turnStampDedup(fileOrdinals)
	if len(fileSet) != len(readerOrdinals) {
		t.Fatalf("the file names %d distinct turn(s) %v while the session log numbers %d %v — the stamp is not the reader's number\n--- self-record.md ---\n%s",
			len(fileSet), fileSet, len(readerOrdinals), readerOrdinals, body)
	}
	for i := range readerOrdinals {
		if fileSet[i] != readerOrdinals[i] {
			t.Fatalf("turn %d of the session log has no note stamped t%d (file stamps %v, log ordinals %v)\n--- self-record.md ---\n%s",
				readerOrdinals[i], readerOrdinals[i], fileSet, readerOrdinals, body)
		}
	}

	// AND THE NOTES CARRY THE RIGHT ONE: the first turn's note is t1, the
	// second's t2 — a set comparison alone would pass with them swapped.
	wantByNote := map[string]int{turnStampNoteOne: 1, turnStampNoteTwo: 2}
	for _, got := range fromFile {
		want, known := wantByNote[got.note]
		if !known {
			t.Fatalf("the file holds a note this arm did not script: %q", got.note)
		}
		if got.number != want {
			t.Fatalf("the note %q is stamped t%d, want t%d — a note belongs to the turn it was written in\n--- self-record.md ---\n%s",
				got.note, got.number, want, body)
		}
	}
	t.Logf("self-record.md (%s):\n%s", recordPath, body)
	t.Logf("session log (%s): turns=%d ordinals=%v", sessionPath, readerTurns, readerOrdinals)
}

// turnStampFindOne locates a file the run wrote under a throwaway home. It walks,
// because the session directory is named by the run's own id — a fact of the run,
// not something a test may guess — and the walk is over the TEST's temp home,
// never the operator's profile.
func turnStampFindOne(t *testing.T, home, name string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == name {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", home, err)
	}
	if found == "" {
		t.Fatalf("the run wrote no %s under %s", name, home)
	}
	return found
}

// turnStampDedup collapses a sorted ordinal slice to its distinct values.
func turnStampDedup(sorted []int) []int {
	out := sorted[:0]
	last := 0
	for i, n := range sorted {
		if i == 0 || n != last {
			out = append(out, n)
			last = n
		}
	}
	return out
}
