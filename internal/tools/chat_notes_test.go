package tools

// chat_notes_test.go — the round-level note index (chat_notes.go), tested
// red-then-green against the REAL production surfaces.
//
// WHAT IT PINS, in order: (1) a note that was never written cannot be found and
// one that was can — the red/green pair that makes "it works" a measurement
// rather than a claim; (2) the CJK route — a Chinese fragment INSIDE a longer
// run is found, which the unicode61 index alone cannot do (a contiguous CJK run
// is ONE token there), so the trigram tier is what answers it; (3) the
// provenance — the stored row carries the MACHINE from tools.OriginHostName()
// and the FULL local path, never a bare path (two machines in this topology
// have home directories that are the same string, which is why both columns
// exist); (4) the absence rules — an empty note and a session-less note are
// REFUSED rather than indexed as a row nobody can walk back to.
//
// The database comes from the scaffold (Rule 32): testutil.NewTestDBPath pins
// GOPI_MEMORY_DB at a fresh per-test file, which is the path chatDBPath()
// resolves (GOPI_CHAT_DB unset ⇒ the ledger path), so the tools-layer opener in
// this file lands on the fixture and never on the operator's live ledger.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// chatNoteFixtureHostEnv is the environment override every test in this file
// clears, so the machine identity it asserts is the one this process really
// resolves (os.Hostname) rather than a node simulated by the ambient
// environment. The variable is spelled by its owner — the tools package's own
// originSyncNodeEnv — never re-typed (Rule 1b).
func clearChatNoteHostEnv(t *testing.T) {
	t.Helper()
	t.Setenv(originSyncNodeEnv, "")
}

// TestChatNoteSearchIsRedThenGreen is the core contract: nothing written means
// nothing found; the note that was written is found, by its own words.
func TestChatNoteSearchIsRedThenGreen(t *testing.T) {
	testutil.NewTestDBPath(t)
	clearChatNoteHostEnv(t)

	const missing = "zzqx-no-such-note-token"
	out, err := ChatNoteSearch(missing, 0)
	if err != nil {
		t.Fatalf("search on an empty index: %v", err)
	}
	if !strings.Contains(out, "No notes matching") {
		t.Fatalf("an unwritten note must not be found; got:\n%s", out)
	}

	const needle = "the trigger keeps the trigram index in lockstep"
	note := ChatNote{
		SessionID: "sess-red-green",
		Turn:      3,
		Note:      "wired the note index and " + needle,
		Path:      "/home/user/.gopi/sessions/sess-red-green/self-record.md",
	}
	if err := ChatNoteRecord(note); err != nil {
		t.Fatalf("record: %v", err)
	}

	out, err = ChatNoteSearch("trigram", 0)
	if err != nil {
		t.Fatalf("search after a write: %v", err)
	}
	if !strings.Contains(out, "via fts") {
		t.Fatalf("an ASCII word must be answered by the unicode61 tier; got:\n%s", out)
	}
	if !strings.Contains(out, "sess-red-green") || !strings.Contains(out, "turn 3") {
		t.Fatalf("the hit must carry the session and the turn it recorded; got:\n%s", out)
	}
	if !strings.Contains(out, note.Path) {
		t.Fatalf("the hit must carry the FULL local path (%q); got:\n%s", note.Path, out)
	}
	host := OriginHostName()
	if !strings.Contains(out, host) {
		t.Fatalf("the hit must carry the machine it was recorded on (%q); got:\n%s", host, out)
	}
}

// TestChatNoteSearchFindsChineseInsideALongerRun is the CJK route: unicode61
// stores 网络测试环境 as ONE token, so a fragment of it ("测试环境") cannot match
// there — the trigram tier is what recalls it. A 2-character fragment is below
// trigram's lower bound and the LIKE floor answers that.
func TestChatNoteSearchFindsChineseInsideALongerRun(t *testing.T) {
	testutil.NewTestDBPath(t)
	clearChatNoteHostEnv(t)

	if err := ChatNoteRecord(ChatNote{
		SessionID: "sess-cjk",
		Turn:      1,
		Note:      "把网络测试环境的结果记下来，trigram 负责中文子串",
		Path:      "/home/user/.gopi/sessions/sess-cjk/self-record.md",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	out, err := ChatNoteSearch("测试环境", 0)
	if err != nil {
		t.Fatalf("trigram search: %v", err)
	}
	if !strings.Contains(out, "via trigram") {
		t.Fatalf("a 4-character CJK fragment inside a longer run needs the trigram tier; got:\n%s", out)
	}
	if !strings.Contains(out, "sess-cjk") {
		t.Fatalf("hit missing: %s", out)
	}

	out, err = ChatNoteSearch("测试", 0)
	if err != nil {
		t.Fatalf("like search: %v", err)
	}
	if !strings.Contains(out, "via like") {
		t.Fatalf("a 2-character CJK fragment is below trigram's lower bound and must reach the LIKE floor; got:\n%s", out)
	}
}

// TestChatNoteRecordRefusesAnonymousAndEmptyRows pins the two refusals: a row
// with no session cannot be walked back to a run, and a row with no text holds
// nothing — both are refused loudly instead of becoming unreadable index
// entries (Rule 15: no silent half-provenance).
func TestChatNoteRecordRefusesAnonymousAndEmptyRows(t *testing.T) {
	testutil.NewTestDBPath(t)
	clearChatNoteHostEnv(t)

	if err := ChatNoteRecord(ChatNote{Turn: 1, Note: "no session here"}); err == nil {
		t.Fatal("a note with no session_id must be refused")
	}
	if err := ChatNoteRecord(ChatNote{SessionID: "sess-x", Turn: 1, Note: "   "}); err == nil {
		t.Fatal("an empty note must be refused")
	}
	out, err := ChatNoteSearch("session", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "No notes matching") {
		t.Fatalf("a refused note must leave no row behind; got:\n%s", out)
	}
}

// TestChatNoteRoundAbsenceIsStoredAsAbsence: a run that could not say which
// round it was on stores RoundIndexUnknown — never a fabricated round 1, and
// never a NULL that reads back as a real number.
func TestChatNoteRoundAbsenceIsStoredAsAbsence(t *testing.T) {
	testutil.NewTestDBPath(t)
	clearChatNoteHostEnv(t)

	if err := ChatNoteRecord(ChatNote{
		SessionID: "sess-noround",
		Note:      "a note recorded without a round number",
		Path:      "/home/user/.gopi/sessions/sess-noround/self-record.md",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	out, err := ChatNoteSearch("without a round number", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "round ?") {
		t.Fatalf("an absent round must render as absence, not as round 0 or round 1; got:\n%s", out)
	}
}

// TestChatNoteSearchLimitIsHonouredAndDisclosed: --limit caps the render, and a
// capped result SAYS it was capped (a silent truncation reads as "that is all
// there is").
func TestChatNoteSearchLimitIsHonouredAndDisclosed(t *testing.T) {
	testutil.NewTestDBPath(t)
	clearChatNoteHostEnv(t)

	for i := 1; i <= 3; i++ {
		if err := ChatNoteRecord(ChatNote{
			SessionID: "sess-limit",
			Turn:      i,
			Note:      fmt.Sprintf("limit-probe note number %d", i),
			Path:      "/home/user/.gopi/sessions/sess-limit/self-record.md",
		}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	out, err := ChatNoteSearch("limit-probe", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "2 note(s) matching") || !strings.Contains(out, "(capped at 2)") {
		t.Fatalf("a capped result must say so; got:\n%s", out)
	}
}
