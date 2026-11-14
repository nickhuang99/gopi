package agent

// memory_replay_guard_test.go — THE AUTO-MEMORY WRITE GATE, measured against a
// REAL ledger file (epic `gopi chat --replay` must be really read-only; the
// operator's defect A).
//
// WHY THIS IS NOT A UNIT CASE (Rule 31). The claim is not "replaymode.Active()
// is true when armed" — that is one bool and it was already tested. The claim is
// "the capture paths do not put a row into the operator's distilled_qa". So each
// case drives the REAL capture entry point (captureSelfNotes /
// captureResponseText) against a REAL sqlite ledger on disk and counts the rows
// before and after; the CONTROL arm runs the same call while this process is NOT
// replaying and requires it to WRITE, because an unchanged count proves nothing
// about a path that never wrote in the first place.
//
// The ledger is the scaffold's (testutil.NewTestDBPath pins GOPI_MEMORY_DB at a
// fresh per-test file) — never a hand-rolled path, never the operator's profile.

import (
	"database/sql"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// replayStoreAgent builds the real agent shape the capture paths need: a session
// log under an isolated GOPI_HOME and a per-test ledger, with the round stamped
// so the provenance pointer is complete (an unfilled pointer would refuse the
// store for a reason that has nothing to do with the replay flag).
func replayStoreAgent(t *testing.T) *Agent {
	t.Helper()
	testutil.NewTestDBPath(t)
	t.Setenv(config.GopiDirEnv, t.TempDir())
	sl, err := NewSessionLoggerForRun("capture-probe-model", filepath.Join(t.TempDir(), "raw.log"), "replay-gate-session", nil)
	if err != nil {
		t.Fatalf("session logger: %v", err)
	}
	t.Cleanup(func() { _ = sl.Close() }) //stylegate:ignore cleanup close of the capture fixture's logger; the row count is the assertion
	return &Agent{SessionLog: sl, hippocampusRound: 1, Logger: log.New(io.Discard, "", 0)}
}

// replayDistilledCount is the distilled_qa row count, read through the REAL
// ledger the store wrote to.
func replayDistilledCount(t *testing.T) int {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(testDBPathFromEnv(t)))
	if err != nil {
		t.Fatalf("open the test ledger: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() }) //stylegate:ignore cleanup close of the test ledger reader
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM distilled_qa").Scan(&n); err != nil {
		// A ledger with no distilled_qa table has no distilled rows — the count is
		// 0 rather than an error, because the question this case asks is "how many
		// rows", and a table the first store creates has none (Rule 15: the answer
		// is reported as the value it is, not as a failure to read it).
		if strings.Contains(err.Error(), "no such table") {
			return 0
		}
		t.Fatalf("count distilled_qa: %v", err)
	}
	return n
}

// TestReplayRefusesTheAutoMemoryStore is the regression for defect A: with the
// replay flag armed, neither capture path may add a row, and with it disarmed
// BOTH must (the control).
//
// It also pins the SECOND half of the gate's contract: a refused capture returns
// "" — the tier it would have reported — so no caller can mistake it for a
// captured note.
func TestReplayRefusesTheAutoMemoryStore(t *testing.T) {
	a := replayStoreAgent(t)
	note := selfNoteProse(selfNoteConclusionRunes+40, "实测这条结论有证据支撑")
	body := "| a | b |\n|---|---|\n| 1 | 2 |\n\n" + strings.Repeat("证据 ", 200)

	// CONTROL: not replaying ⇒ the note path really writes.
	baseline := replayDistilledCount(t)
	if tier := a.captureSelfNote(note); tier == "" {
		t.Fatalf("the control captured nothing while this process is NOT replaying: the note gate refused it, so the armed reading below would measure nothing")
	}
	afterNote := replayDistilledCount(t)
	if afterNote <= baseline {
		t.Fatalf("the control stored no row (count %d → %d): the case below would be asserting against a path that never writes", baseline, afterNote)
	}

	replaymode.Arm("/a/recording/that/does/not/exist.jsonl")
	defer replaymode.Disarm()
	if !replaymode.Active() {
		t.Fatalf("replaymode.Active() is false after Arm")
	}

	// ARMED: both capture paths refuse, and the table does not move.
	if tier := a.captureSelfNote(note + " (second copy)"); tier != "" {
		t.Fatalf("captureSelfNote reported the tier %q while a replay was armed: the note reached the store", tier)
	}
	if tier := a.captureResponseText(body); tier != "" {
		t.Fatalf("captureResponseText reported the tier %q while a replay was armed: the response reached the store", tier)
	}
	if got := replayDistilledCount(t); got != afterNote {
		t.Fatalf("distilled_qa gained %d row(s) while a replay was armed (%d → %d): a replay writes nothing into the operator's ledger", got-afterNote, afterNote, got)
	}
}

// TestReplayRefusesTheMemoryToolStore pins the THIRD store site: the same gate
// at the model-initiated shape. It asserts the agent-level predicate rather than
// the whole dispatcher, because the dispatcher's answer is what the gate feeds —
// and the answer must be a SENTENCE naming the refusal, never an empty success
// that would read as a store that worked.
func TestReplayRefusesTheMemoryToolStore(t *testing.T) {
	a := replayStoreAgent(t)
	if a.memoryWriteReplayRefused("run the memory tool's store in the operator's ledger") {
		t.Fatalf("the control refused a memory-tool store while this process is NOT replaying")
	}
	replaymode.Arm("/a/recording.jsonl")
	defer replaymode.Disarm()
	if !a.memoryWriteReplayRefused("run the memory tool's store in the operator's ledger") {
		t.Fatalf("the memory-tool store was NOT refused while a replay was armed: a recorded `memory store` call would reach the operator's ledger")
	}
	// The announcement exists and names the refusal (Rule 15: never silent).
	if s := replaymode.Refuse("x"); s == "" || !strings.Contains(s, "refused") {
		t.Fatalf("replaymode.Refuse produced %q, which does not announce a refusal", s)
	}
}

// TestReplayRefusalGoesToTheEvidenceFileNotTheChatSurface is the regression for
// the operator's second half of the epic: the refusal must still be RECORDED
// (Rule 15) but must never be DRAWN.
//
// The measured defect: the sentence rode a.Logger, and a chat run points that
// logger at the layout's flow seam, so it appeared inside the answer band once
// per captured note. So the assertion is two-sided, and the CONTROL arm is the
// non-replay process, which must record nothing at all.
//
// WHY THE LOGGER IS A BUFFER HERE. That buffer IS the chat surface in this test:
// anything written to it would have been drawn in the answer band, so "the
// buffer stayed empty" is exactly the property the screen needs.
func TestReplayRefusalGoesToTheEvidenceFileNotTheChatSurface(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	// GOPI_HOME is a HOME, so the profile root is <home>/.gopi (config.GopiDir)
	// and the evidence rides that profile's logs directory.
	evidencePath := filepath.Join(home, config.ProfileDirName, replayMemoryRefusalLogDirName, replayMemoryRefusalLogName)

	var screen strings.Builder
	a := &Agent{Logger: log.New(&screen, "", 0)}

	// CONTROL: not replaying ⇒ no refusal, and therefore nothing recorded and
	// nothing drawn (a gate that recorded unconditionally would be a write on the
	// live path, which this epic forbids).
	if a.memoryWriteReplayRefused("store an auto-memory note") {
		t.Fatalf("the control refused a memory write while this process is NOT replaying")
	}
	if _, err := os.Stat(evidencePath); err == nil {
		t.Fatalf("%s exists after a NON-replay refusal check: a live run must not write replay evidence", evidencePath)
	}

	replaymode.Arm("/a/recording.jsonl")
	defer replaymode.Disarm()

	if !a.memoryWriteReplayRefused("store an auto-memory note in the operator's ledger (a note)") {
		t.Fatalf("the auto-memory store was NOT refused while a replay was armed")
	}
	if !a.memoryWriteReplayRefused("store a captured response in the operator's ledger (a body)") {
		t.Fatalf("the response capture was NOT refused while a replay was armed")
	}

	// THE SCREEN (here: the agent's logger) MUST STAY EMPTY. This is the defect.
	if got := screen.String(); strings.Contains(got, replaymode.RefuseMarker) || strings.TrimSpace(got) != "" {
		t.Fatalf("the refusal was written to the agent's logger, which a chat run draws in the answer band:\n%s", got)
	}

	// AND THE EVIDENCE MUST EXIST (Rule 15: silence on screen is never silence).
	raw, err := os.ReadFile(evidencePath)
	if err != nil {
		t.Fatalf("read the replay evidence file %s: %v — a refused write must stay READABLE, it just must not be shown", evidencePath, err)
	}
	text := string(raw)
	if n := strings.Count(text, replaymode.RefuseMarker); n != 2 {
		t.Fatalf("the evidence file carries %d refusal(s), want 2:\n%s", n, text)
	}
	if !strings.Contains(text, "auto-memory note") || !strings.Contains(text, "captured response") {
		t.Fatalf("the evidence does not name the refused writes:\n%s", text)
	}
}

// TestReplayRecordsNoSelfNotes is the regression for the class of write this epic
// found OUTSIDE the store gate: recordSelfNotes was reached on a replay and did
// BOTH of its halves — appended to the session's self-record.md and INSERTed into
// the chat_notes retrieval index — while the downstream auto-memory refusal
// printed one line per note (the visible symptom, not the defect). A replay is
// re-running a conversation that already happened, so it must reach neither.
//
// The instruments are the artifacts themselves: the record FILE must not appear,
// the index table must not gain a row, and the returned slice must be nil (which
// is what tells the capture path there is nothing to consider). The CONTROL arm
// runs the same call with the flag disarmed and requires BOTH writes to happen,
// because "nothing appeared" proves nothing about a path that never wrote.
func TestReplayRecordsNoSelfNotes(t *testing.T) {
	a := replayStoreAgent(t)
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	note := "[IMPORTANT]\n" + selfNoteProse(selfNoteConclusionRunes+40, "实测这条结论有证据支撑") + "\n[/IMPORTANT]"

	// CONTROL: not replaying ⇒ the note really lands (file + index).
	control := a.recordSelfNotes(note, "")
	if len(control) == 0 {
		t.Fatalf("the control recorded no note while this process is NOT replaying: the armed arm would be measuring a path that never writes")
	}
	dir, err := a.selfRecordDir()
	if err != nil {
		t.Fatalf("resolve the control's record directory: %v", err)
	}
	recordPath := filepath.Join(dir, selfRecordFileName)
	if _, serr := os.Stat(recordPath); serr != nil {
		t.Fatalf("the control wrote no %s: %v", recordPath, serr)
	}
	controlRows := replayChatNoteCount(t)

	replaymode.Arm("/a/recording.jsonl")
	defer replaymode.Disarm()

	if got := a.recordSelfNotes(note+"\n", ""); len(got) != 0 {
		t.Fatalf("recordSelfNotes returned %d note(s) while a replay was armed: the round's notes were treated as recorded", len(got))
	}
	// The FILE is the evidence layer and it is append-only, so an extra line is
	// the observable: the armed call must not have added one. Counted through the
	// record layer's OWN parser (selfRecordNoteFromLine), not by looking for one
	// bullet spelling: a line written from the round-stamp epic on opens with
	// `r<round> ` and one written before it opens with `- `, and both are notes.
	body, rerr := os.ReadFile(recordPath)
	if rerr != nil {
		t.Fatalf("read %s: %v", recordPath, rerr)
	}
	if n := countSelfRecordNoteLines(string(body)); n != len(control) {
		t.Fatalf("the record file holds %d note line(s) after an armed round, want the %d from the control: a replay appended to the evidence layer", n, len(control))
	}
	// The INDEX is the retrieval layer over the same facts.
	if got := replayChatNoteCount(t); got != controlRows {
		t.Fatalf("chat_notes gained %d row(s) while a replay was armed (%d → %d): a replay writes nothing into the operator's indexes", got-controlRows, controlRows, got)
	}
}

// replayChatNoteCount is the chat_notes row count, read through the REAL index
// the same opener resolves (tools.ChatNoteIndexPath), so the count is taken from
// the file the writer wrote.
func replayChatNoteCount(t *testing.T) int {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(tools.ChatNoteIndexPath()))
	if err != nil {
		t.Fatalf("open the note index: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() }) //stylegate:ignore cleanup close of the note-index reader
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM chat_notes").Scan(&n); err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return 0
		}
		t.Fatalf("count chat_notes: %v", err)
	}
	return n
}
