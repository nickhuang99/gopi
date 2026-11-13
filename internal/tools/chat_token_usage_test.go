package tools

import (
	"errors"
	"strings"
	"testing"
)

// sessionRowLine returns the ONE rendered index line for a session id (the
// index renders one line per row), failing the test when the row is missing —
// so an assertion on a row's rendering cannot pass by reading nothing.
func sessionRowLine(t *testing.T, rendered, id string) string {
	t.Helper()
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, id+" ") {
			return line
		}
	}
	t.Fatalf("row %s is not in the render:\n%s", id, rendered)
	return ""
}

// TestChatSessionLogOnMachineIsTheSharedCriterion pins the predicate the
// not-found branch consults: it answers "does THIS machine hold a log for the
// id" through the same implementation the list filter and ChatShow use, so a
// caller holding no chat row cannot get a second, weaker answer (Rule 13/14).
func TestChatSessionLogOnMachineIsTheSharedCriterion(t *testing.T) {
	chatTestEnv(t)
	const (
		hereID = "chat-log-here-no-row-1111"
		awayID = "chat-log-away-no-row-2222"
	)
	writeChatSessionJSONL(t, hereID, "a log with no index row")

	onMachine, err := ChatSessionLogOnMachine(hereID)
	if err != nil {
		t.Fatalf("ChatSessionLogOnMachine(%s): %v", hereID, err)
	}
	if !onMachine {
		t.Errorf("ChatSessionLogOnMachine(%s) = false for a log written under this profile", hereID)
	}
	away, err := ChatSessionLogOnMachine(awayID)
	if err != nil {
		t.Fatalf("ChatSessionLogOnMachine(%s): %v", awayID, err)
	}
	if away {
		t.Errorf("ChatSessionLogOnMachine(%s) = true for an id with no log on this machine", awayID)
	}

	// And it AGREES with the row-shaped criterion: for an indexed row whose log
	// is elsewhere, the id-only question and ChatShow's FilePresent must give
	// the same verdict — three surfaces, one probe.
	const indexedAwayID = "chat-indexed-log-away-3333"
	if _, err := ChatEnsureRow(ChatSession{
		SessionID: indexedAwayID, Mode: ChatModeCLI, Model: "m", FirstMsg: "away",
		LogPath: "/nonexistent-locality/session.jsonl",
	}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	row, err := ChatShow(indexedAwayID)
	if err != nil {
		t.Fatalf("ChatShow(%s): %v", indexedAwayID, err)
	}
	byID, err := ChatSessionLogOnMachine(indexedAwayID)
	if err != nil {
		t.Fatalf("ChatSessionLogOnMachine(%s): %v", indexedAwayID, err)
	}
	if byID != row.FilePresent {
		t.Errorf("the id-only criterion says present=%v while ChatShow says %v for the same session — two answers for one fact",
			byID, row.FilePresent)
	}
}

// TestChatShowReportsALogOnThisMachineWithNoIndexRow is C's second shape, and
// the one the operator can produce locally: the log IS here, the index simply
// has no row for it. "no such session" would be false — and it is what the
// not-found branch said before this split (measured 2026-10-05: 3435 such logs
// in zbook's own profile).
func TestChatShowReportsALogOnThisMachineWithNoIndexRow(t *testing.T) {
	chatTestEnv(t)
	const id = "chat-log-here-no-row-9999"
	writeChatSessionJSONL(t, id, "on disk, in no index row")

	_, err := ChatShow(id)
	if err == nil {
		t.Fatalf("ChatShow(%s) succeeded for a session with no chats row", id)
	}
	var notIndexed *ChatSessionNotIndexedError
	if !errors.As(err, &notIndexed) {
		t.Fatalf("ChatShow(%s) = %v, want ChatSessionNotIndexedError — the log IS on this machine, so a not-found answer states something untrue (Rule 15)", id, err)
	}
	if !strings.Contains(err.Error(), ChatSessionNotIndexedNextStep) {
		t.Errorf("the answer does not carry the next step %q: %v", ChatSessionNotIndexedNextStep, err)
	}
	// The counter-case: an id with neither an index row nor a local log is
	// genuinely unknown, and keeps the not-found sentence.
	if _, err := ChatShow("chat-nowhere-at-all-0000"); err == nil || !strings.Contains(err.Error(), "no session with id") {
		t.Errorf("an id with neither a row nor a local log = %v, want the named not-found error", err)
	}
}

// TestChatTokensReportedIsTheOneCriterion pins the derivation both rendering
// faces read: a row's token columns are a MEASUREMENT only when a turn was
// recorded AND at least one count is non-zero. The zero-turn row and the
// all-zero row are the two shapes the "unknown" wording exists for (Rule 15),
// and the non-zero row is the counter-case that must keep printing its numbers.
func TestChatTokensReportedIsTheOneCriterion(t *testing.T) {
	cases := []struct {
		name                  string
		turns, in, out, cache int
		want                  bool
	}{
		{name: "measured", turns: 3, in: 300, out: 50, cache: 280, want: true},
		{name: "measured-without-cache", turns: 1, in: 10, out: 5, cache: 0, want: true},
		{name: "all-zero-after-turns-is-unknown", turns: 1, in: 0, out: 0, cache: 0, want: false},
		{name: "no-turns-is-unknown", turns: 0, in: 0, out: 0, cache: 0, want: false},
		{name: "counts-without-a-recorded-turn-are-unknown", turns: 0, in: 300, out: 50, cache: 280, want: false},
	}
	for _, c := range cases {
		if got := ChatTokensReported(c.turns, c.in, c.out, c.cache); got != c.want {
			t.Errorf("%s: ChatTokensReported(%d,%d,%d,%d) = %v, want %v",
				c.name, c.turns, c.in, c.out, c.cache, got, c.want)
		}
	}
}

// TestChatListRendersUnknownTokensNotZero is the B face's small-sample repro:
// one row whose upstream never reported usage (turns recorded, every count 0 —
// the shape the recorder leaves behind) must NOT render as a measured zero,
// while a row with real totals still prints them.
func TestChatListRendersUnknownTokensNotZero(t *testing.T) {
	chatTestEnv(t)
	const (
		unknownID = "chat-tok-unknown-11111111"
		realID    = "chat-tok-real-22222222"
	)
	for _, row := range []ChatSession{
		{SessionID: unknownID, Mode: ChatModeCLI, Model: "deepseek-flash", FirstMsg: "silent provider", Workdir: "/tmp/proj"},
		{SessionID: realID, Mode: ChatModeCLI, Model: "deepseek-flash", FirstMsg: "measured provider", Workdir: "/tmp/proj"},
	} {
		if _, err := ChatEnsureRow(row); err != nil {
			t.Fatalf("ensure %s: %v", row.SessionID, err)
		}
	}
	// The recorder is the only writer of these columns: a turn folded with the
	// tracker's untouched totals is exactly "the API reported nothing".
	if err := ChatRecordTurn(unknownID, 0, 0, 0); err != nil {
		t.Fatalf("record silent turn: %v", err)
	}
	if err := ChatRecordTurn(realID, 300, 50, 280); err != nil {
		t.Fatalf("record measured turn: %v", err)
	}

	out, err := ChatList(ChatListFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ChatList: %v", err)
	}
	silent := sessionRowLine(t, out, unknownID)
	if strings.Contains(silent, "in=0 out=0 cache=0") {
		t.Errorf("a row whose usage was never reported renders as a measured zero:\n%s", silent)
	}
	if !strings.Contains(silent, ChatTokenUsageNote) {
		t.Errorf("the unmeasured row does not carry %q:\n%s", ChatTokenUsageNote, silent)
	}
	measured := sessionRowLine(t, out, realID)
	if !strings.Contains(measured, "total tok in=300 out=50 cache=280") {
		t.Errorf("a row with real counts no longer prints them (labelled as the session total they are):\n%s", measured)
	}
}

// TestChatShowCarriesTheUnmeasuredTokenFact is the same assertion one layer in:
// the struct the summary page renders from must know the counts are unmeasured,
// so `session show` cannot print a zero it cannot vouch for.
func TestChatShowCarriesTheUnmeasuredTokenFact(t *testing.T) {
	chatTestEnv(t)
	const (
		unknownID = "chat-tok-show-unknown-1111"
		realID    = "chat-tok-show-real-2222"
	)
	for _, id := range []string{unknownID, realID} {
		if _, err := ChatEnsureRow(ChatSession{SessionID: id, Mode: ChatModeCLI, Model: "deepseek-flash", FirstMsg: "token fact"}); err != nil {
			t.Fatalf("ensure %s: %v", id, err)
		}
		// The log is written so the row is NOT in the absent-log case: this test
		// is about the token fact, and the two must be separately observable.
		writeChatSessionJSONL(t, id, "hello "+id)
	}
	if err := ChatRecordTurn(unknownID, 0, 0, 0); err != nil {
		t.Fatalf("record silent turn: %v", err)
	}
	if err := ChatRecordTurn(realID, 300, 50, 280); err != nil {
		t.Fatalf("record measured turn: %v", err)
	}

	silent, err := ChatShow(unknownID)
	if err != nil {
		t.Fatalf("ChatShow(%s): %v", unknownID, err)
	}
	if silent.TokensReported {
		t.Errorf("ChatShow(%s).TokensReported = true for a row whose usage was never reported (counts %d/%d/%d)",
			unknownID, silent.InputTokens, silent.OutputTokens, silent.CacheTokens)
	}
	measured, err := ChatShow(realID)
	if err != nil {
		t.Fatalf("ChatShow(%s): %v", realID, err)
	}
	if !measured.TokensReported {
		t.Errorf("ChatShow(%s).TokensReported = false for a row with real totals", realID)
	}
	if measured.InputTokens != 300 || measured.OutputTokens != 50 || measured.CacheTokens != 280 {
		t.Errorf("ChatShow(%s) totals = %d/%d/%d, want 300/50/280", realID,
			measured.InputTokens, measured.OutputTokens, measured.CacheTokens)
	}
}

// TestChatShowAndListAgreeAboutTheLogPresence is the C criterion's consistency
// evidence, driven through the two REAL faces: the list page's per-row fact and
// `session show`'s fact for the same row come from ONE implementation
// (chatSessionLogPresent), so a reader told "missing" in the list cannot be told
// "present" by the summary. The absent case is a path this machine does not
// hold; the present case is a log written under the profile the test pinned.
func TestChatShowAndListAgreeAboutTheLogPresence(t *testing.T) {
	chatTestEnv(t)
	const (
		hereID = "chat-presence-agree-here-1111"
		goneID = "chat-presence-agree-gone-2222"
		absent = "/nonexistent-locality/session.jsonl"
	)
	if _, err := ChatEnsureRow(ChatSession{SessionID: hereID, Mode: ChatModeCLI, Model: "m", FirstMsg: "here"}); err != nil {
		t.Fatalf("ensure here: %v", err)
	}
	if _, err := ChatEnsureRow(ChatSession{SessionID: goneID, Mode: ChatModeCLI, Model: "m", FirstMsg: "gone", LogPath: absent}); err != nil {
		t.Fatalf("ensure gone: %v", err)
	}
	writeChatSessionJSONL(t, hereID, "a log this machine holds")

	rows, err := ChatSessionRows(ChatListFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ChatSessionRows: %v", err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.SessionID] = true
		detail, err := ChatShow(r.SessionID)
		if err != nil {
			t.Fatalf("ChatShow(%s): %v", r.SessionID, err)
		}
		if detail.FilePresent != r.FilePresent {
			t.Errorf("row %s: list says file_present=%v, show says %v — the two faces disagree about ONE row",
				r.SessionID, r.FilePresent, detail.FilePresent)
		}
	}
	if !seen[hereID] || !seen[goneID] {
		t.Fatalf("the fixtures did not reach the list face (here=%v gone=%v) — the assertion would pass vacuously", seen[hereID], seen[goneID])
	}
}

// TestChatShowDistinguishesAbsentLogFromAbsentSession pins the two answers
// apart: a row that IS in the index but whose log is not on this machine is
// returned WITH the fact (the caller renders the next step), while an id in no
// row is a NAMED not-found error. Before this split, "the log is elsewhere" and
// "no such session" were the same sentence (Rule 15).
func TestChatShowDistinguishesAbsentLogFromAbsentSession(t *testing.T) {
	chatTestEnv(t)
	const goneID = "chat-absent-log-row-1111"
	if _, err := ChatEnsureRow(ChatSession{
		SessionID: goneID, Mode: ChatModeCLI, Model: "m", FirstMsg: "elsewhere",
		LogPath: "/nonexistent-locality/session.jsonl",
	}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	row, err := ChatShow(goneID)
	if err != nil {
		t.Fatalf("ChatShow(%s) = %v, want the indexed row with FilePresent=false — the session EXISTS, only its log is away", goneID, err)
	}
	if row.FilePresent {
		t.Errorf("ChatShow(%s).FilePresent = true for a log this machine does not hold", goneID)
	}
	if _, err := ChatShow("11111111-2222-3333-4444-555555555555"); err == nil {
		t.Error("ChatShow accepted an id in no chats row — an unknown session must stay a NAMED error")
	} else if !strings.Contains(err.Error(), "no session with id") {
		t.Errorf("the not-found answer no longer says so in its own words: %v", err)
	}
}
