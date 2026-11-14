package testsuite

// turnfold_recordfile_wire_test.go — the fold's FIRST data source measured on
// the WIRE, through the real binary (PROJECT.md Rule 31).
//
// The unit arms in internal/agent/turnfold_test.go prove that turnFoldNote reads
// `<session-dir>/self-record.md`. What they cannot prove is that a REAL run's
// fold note is built from that file: the wire is where the substitute note ends
// up, and this walk therefore drives `gopi chat --once --tools` against a
// scripted mock, then READS the session's own raw.log and its own
// self-record.md and compares the two, line for line.
//
// The discriminator is the note's `recorded:` line carrying the FILE's own
// `t<turn> `-stamped text, copied verbatim, and carrying NO source mark: the
// file is the only source there is, so `recorded: (…)` cannot appear.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// turnFoldWireFileNote is the note the mock's answer records. CJK on purpose: a
// note is capped in RUNES, so a byte-based budget would cut it three times
// shorter than an English one.
const turnFoldWireFileNote = "折叠 note 的 recorded 槽位必须优先读 self-record.md，并按 turn 取行原文拷贝"

// TestTurnFold_RecordFileIsTheNotesSourceOnTheWire is the wire-level acceptance
// arm: the session's self-record.md holds the note, and the fold note on the last
// request carries it VERBATIM, with the source named as the file.
func TestTurnFold_RecordFileIsTheNotesSourceOnTheWire(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	probe := "turnfold-recfile-probe.txt"
	if err := os.WriteFile(filepath.Join(work, probe), []byte("probe content\n"), 0o644); err != nil {
		t.Fatalf("fixture write: %v", err)
	}

	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ // turn 1, round 1: a tool
			ID:   "call_fold_recfile",
			Name: "read",
			Args: map[string]interface{}{"path": probe},
		}},
		testutil.Resp{ // turn 1, round 2: the model's own record block
			Text: "read it and the answer is in the file\n[IMPORTANT]\n" + turnFoldWireFileNote + "\n[/IMPORTANT]",
			// Rune-safe fragmentation at a non-zero width: the legacy two-half
			// split cuts on a BYTE index and would slice the CJK answer mid-rune.
			TextDeltaBytes: 16,
		},
		testutil.Resp{Text: "second turn answered"}, // turn 2
		testutil.Resp{Text: "third turn answered"},  // turn 3
	)
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"},
		"/tools\nfirst message please\nsecond message please\nthird message please\n/quit\n",
		turnFoldSmallWindowEnv(t, home, llm.URL()), work)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}

	total := llm.RequestCount()
	if total < 3 {
		t.Fatalf("the mock served %d request(s); want ≥3 so that a completed turn exists", total)
	}

	// THE FILE: every `- <note>` line the run recorded, read back from disk.
	recordPath := turnFoldWireFindRecordFile(t, home)
	fileBody, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read %s: %v", recordPath, err)
	}
	var fileNotes []string
	for _, line := range strings.Split(string(fileBody), "\n") {
		// Through the record layer's OWN parser: a line is `r<round> <note>`
		// from the round-stamp epic on and `- <note>` in every line written
		// before it (the file is append-only evidence, never rewritten).
		if _, note, ok := agent.SelfRecordNoteFromLine(line); ok && strings.TrimSpace(note) != "" {
			fileNotes = append(fileNotes, strings.TrimSpace(note))
		}
	}
	if len(fileNotes) == 0 {
		t.Fatalf("%s holds no note line:\n%s", recordPath, fileBody)
	}

	// THE WIRE: the fold note that replaced turn 1.
	noteRow := uiNoteRow(t, llm, total)
	if noteRow == "" {
		t.Fatalf("no turn note on the last request's user rows: %q", turnFoldWireUserTexts(t, llm, total))
	}
	if !strings.Contains(noteRow, "recorded: ") {
		t.Fatalf("the note on the wire carries no recorded-knowledge slot:\n%s", noteRow)
	}
	// AND THE SLOT CARRIES NO SOURCE MARK (operator, 2026-11-10): the record
	// file is the ONLY source a fold reads, so `(self-record.md)` was scaffolding
	// — the slot is the verbatim copy and nothing else.
	if strings.Contains(noteRow, "recorded: (") {
		t.Fatalf("the recorded slot still carries a source mark:\n%s", noteRow)
	}
	// LINE FOR LINE: every note in the file is on the wire, verbatim.
	for _, want := range fileNotes {
		if !strings.Contains(noteRow, want) {
			t.Fatalf("the file's note is not on the wire verbatim: %q\n--- note row on request %d ---\n%s", want, total, noteRow)
		}
	}
	t.Logf("self-record.md (%s):\n%s", recordPath, fileBody)
	t.Logf("fold note on request %d:\n%s", total, noteRow)
}

// turnFoldWireFindRecordFile locates the run's record file under a throwaway
// home. It walks, because the session directory is named by the run's own id (a
// fact of the run, not something the test may guess) — the walk is over the
// TEST's temp home, never the operator's profile.
func turnFoldWireFindRecordFile(t *testing.T, home string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == "self-record.md" {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", home, err)
	}
	if found == "" {
		t.Fatalf("the run wrote no self-record.md under %s", home)
	}
	return found
}
