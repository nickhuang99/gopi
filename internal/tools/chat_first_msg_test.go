package tools

// chat_first_msg_test.go — THE HEAD COLUMN IS THE OPERATOR'S, AND ONLY THEIRS.
//
// The defect these cases pin (measured 2026-10-28 on the operator's profile):
// `gopi session list --mode chat` titled three of his conversations
// "[system] 16 notices are pending: …" / "[system] An external process sent this
// session a message: …" — i.e. the head column had been written by the LAST input
// the session carried, and that input was a `gopi notify` delivery. See
// chat_first_msg.go for the reasoning; these tests are the red proof.
//
// THE CENTRAL CASE IS THE INJECTION ARRIVING FIRST (the epic's acceptance): a
// session whose very first turn is a delivery must still be recognised as a
// conversation — it gets the explicit placeholder, never the framework's
// sentence, and the moment the person types, the head becomes their line.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/inputsource"
)

// chatNoticeFirstMsg is the shape a notify delivery's text actually has: the
// framework's own sentence, built from sessionevent's markers so a change to the
// marker cannot leave this fixture asserting yesterday's string.
const chatNoticeFirstMsg = "[system] An external process sent this session a message: task event: event=completed"

// chatNoticeBatchFirstMsg is the batch form a job-notice turn opens with.
const chatNoticeBatchFirstMsg = "[system] 16 notices are pending: external message ab17, external message 342e"

// TestChatEnsureFirstMsgIgnoresInjectedText is the primary regression case: a
// row whose first turn carried framework text is born with the PLACEHOLDER, and
// its stored value is never the injected sentence — under either recorded face,
// because the restatement of a delivery is written from the human face too.
func TestChatEnsureFirstMsgIgnoresInjectedText(t *testing.T) {
	chatTestEnv(t)
	cases := []struct {
		name    string
		text    string
		source  string
		session string
	}{
		{"notify delivery", chatNoticeFirstMsg, inputsource.Notify, "chat-fm-0001"},
		{"notify batch", chatNoticeBatchFirstMsg, inputsource.Notify, "chat-fm-0002"},
		{"delivery restatement under the human face", chatNoticeFirstMsg, inputsource.Human, "chat-fm-0003"},
		{"model-typed line", "keep going please", inputsource.LLM, "chat-fm-0004"},
		{"empty text", "", inputsource.Human, "chat-fm-0005"},
	}
	for _, tc := range cases {
		if _, err := ChatEnsureRow(ChatSession{
			SessionID: tc.session, Mode: ChatModeCLI,
			FirstMsg: tc.text, FirstMsgSource: tc.source,
		}); err != nil {
			t.Fatalf("%s: ensure: %v", tc.name, err)
		}
		d, err := ChatShow(tc.session)
		if err != nil {
			t.Fatalf("%s: detail: %v", tc.name, err)
		}
		if d.FirstMsg != ChatFirstMsgPlaceholder {
			t.Errorf("%s: first_msg = %q, want the placeholder %q (the injected text belongs on the log, not in the index head)",
				tc.name, d.FirstMsg, ChatFirstMsgPlaceholder)
		}
	}
}

// TestChatEnsureFirstMsgStoresOperatorLine: the operator's own line IS stored, in
// both faces the person can produce (typed at the prompt, or parked with /queue),
// and it is folded to one bounded line as before.
func TestChatEnsureFirstMsgStoresOperatorLine(t *testing.T) {
	chatTestEnv(t)
	for _, tc := range []struct{ session, text, source string }{
		{"chat-fm-0010", "  fix   the\nlogin bug ", inputsource.Human},
		{"chat-fm-0011", "run the tests after the merge", inputsource.Queue},
	} {
		if _, err := ChatEnsureRow(ChatSession{
			SessionID: tc.session, Mode: ChatModeCLI,
			FirstMsg: tc.text, FirstMsgSource: tc.source,
		}); err != nil {
			t.Fatalf("ensure %s: %v", tc.session, err)
		}
		d, err := ChatShow(tc.session)
		if err != nil {
			t.Fatalf("detail: %v", err)
		}
		want := strings.Join(strings.Fields(tc.text), " ")
		if d.FirstMsg != want {
			t.Errorf("%s (source=%s): first_msg = %q, want %q", tc.session, tc.source, d.FirstMsg, want)
		}
	}
}

// TestChatFirstMsgIsFirstNotLatest: a LATER operator line cannot displace the
// first one. This is the other half of the defect — the old clause took any
// non-empty text, so the column tracked the latest turn rather than the first.
func TestChatFirstMsgIsFirstNotLatest(t *testing.T) {
	chatTestEnv(t)
	sid := "chat-fm-0020"
	if _, err := ChatEnsureRow(ChatSession{SessionID: sid, Mode: ChatModeCLI,
		FirstMsg: "the first thing I asked", FirstMsgSource: inputsource.Human}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// A later turn — operator's own, then an injection — must change nothing.
	if wrote, err := ChatOfferFirstMsg(sid, "a later question", inputsource.Human); err != nil || wrote {
		t.Errorf("offer of a later operator line: wrote=%v err=%v; want false, nil", wrote, err)
	}
	if wrote, err := ChatOfferFirstMsg(sid, chatNoticeFirstMsg, inputsource.Notify); err != nil || wrote {
		t.Errorf("offer of a notify line: wrote=%v err=%v; want false, nil", wrote, err)
	}
	d, err := ChatShow(sid)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if d.FirstMsg != "the first thing I asked" {
		t.Errorf("first_msg = %q; want the FIRST operator line kept", d.FirstMsg)
	}
}

// TestChatFirstMsgPlaceholderYieldsToTheOperator is the epic's acceptance in
// miniature: a session born on an injected turn carries the placeholder, and it
// acquires the operator's line as soon as one exists — while an injected line
// never writes, and never blots out the real head afterwards.
func TestChatFirstMsgPlaceholderYieldsToTheOperator(t *testing.T) {
	chatTestEnv(t)
	sid := "chat-fm-0030"
	if _, err := ChatEnsureRow(ChatSession{SessionID: sid, Mode: ChatModeCLI,
		FirstMsg: chatNoticeBatchFirstMsg, FirstMsgSource: inputsource.Notify}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if wrote, err := ChatOfferFirstMsg(sid, chatNoticeFirstMsg, inputsource.Notify); err != nil || wrote {
		t.Fatalf("an injected line must not claim the head: wrote=%v err=%v", wrote, err)
	}
	if wrote, err := ChatOfferFirstMsg(sid, "what did the merger do?", inputsource.Human); err != nil || !wrote {
		t.Fatalf("the operator's line must claim the head: wrote=%v err=%v", wrote, err)
	}
	// Idempotent: the offer runs every turn, so a repeat must be a no-op.
	if wrote, err := ChatOfferFirstMsg(sid, "what did the merger do?", inputsource.Human); err != nil || wrote {
		t.Errorf("repeat offer: wrote=%v err=%v; want false, nil", wrote, err)
	}
	d, err := ChatShow(sid)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if d.FirstMsg != "what did the merger do?" {
		t.Errorf("first_msg = %q; want the operator's line", d.FirstMsg)
	}
	// A missing row is REPORTED, never a silent success (Rule 15).
	if _, err := ChatOfferFirstMsg("chat-fm-nonexistent", "hello", inputsource.Human); err == nil {
		t.Error("offering a first message to a row that does not exist must be an error, not a silent no-op")
	}
}

// TestChatEnsureReEnsureKeepsOperatorLine: the re-ensure path (RecordChatTurn's
// missing-row recovery) passes no text at all — it must not blank, nor replace
// with the placeholder, a stored operator line.
func TestChatEnsureReEnsureKeepsOperatorLine(t *testing.T) {
	chatTestEnv(t)
	sid := "chat-fm-0040"
	if _, err := ChatEnsureRow(ChatSession{SessionID: sid, Mode: ChatModeCLI,
		FirstMsg: "hello there", FirstMsgSource: inputsource.Human}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := ChatEnsureRow(ChatSession{SessionID: sid, Mode: ChatModeCLI}); err != nil {
		t.Fatalf("re-ensure: %v", err)
	}
	d, err := ChatShow(sid)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if d.FirstMsg != "hello there" {
		t.Errorf("re-ensure changed the head: %q", d.FirstMsg)
	}
}

// writeChatSessionJSONLWithSources fabricates a log whose user rows carry their
// RECORDED faces — the field the repair re-derives the head from.
func writeChatSessionJSONLWithSources(t *testing.T, sessionID string, rows []struct{ text, source string }) string {
	t.Helper()
	dir, err := chatSessionsDir()
	if err != nil {
		t.Fatalf("sessions dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	lines := []string{
		`{"type":"session_start","timestamp":"2026-10-27T08:00:00Z","model":"deepseek-flash","sessionId":"` + sessionID + `","workdir":"/tmp/proj"}`,
	}
	for i, r := range rows {
		src := r.source
		if src == "" {
			lines = append(lines, `{"type":"user","timestamp":"2026-10-27T08:0`+string(rune('1'+i))+`:00Z","message":{"role":"user","content":`+jsonString(r.text)+`}}`)
			continue
		}
		lines = append(lines, `{"type":"user","timestamp":"2026-10-27T08:0`+string(rune('1'+i))+`:00Z","source":"`+src+`","message":{"role":"user","content":`+jsonString(r.text)+`}}`)
	}
	lines = append(lines, `{"type":"session_end","timestamp":"2026-10-27T08:09:00Z","elapsed_sec":540,"exit_code":0}`)
	path := filepath.Join(dir, sessionID+taskSessionJSONLExt)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session jsonl: %v", err)
	}
	return path
}

// writeChatSessionJSONLBare writes the SAME fixture into the BARE layout —
// `sessions/<id>/session.jsonl` — which is the layout this machine's profile
// actually uses (measured 2026-10-30: 544 bare dirs, 0 flat files).
//
// IT EXISTS BECAUSE ITS ABSENCE COST A REAL DEFECT: chatSessionFactsOf looked its
// log up through the flat-only chatFindSessionJSONL, so on a bare-layout profile
// it answered nil for every row and the first repair run reported 29 readable
// logs as "this machine does not hold the session log". A fixture that only ever
// writes the flat layout could never have caught it.
func writeChatSessionJSONLBare(t *testing.T, sessionID string, rows []struct{ text, source string }) string {
	t.Helper()
	dir, err := chatSessionsDir()
	if err != nil {
		t.Fatalf("sessions dir: %v", err)
	}
	bare := filepath.Join(dir, sessionID)
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir bare session dir: %v", err)
	}
	lines := []string{
		`{"type":"session_start","timestamp":"2026-10-27T08:00:00Z","model":"deepseek-flash","sessionId":"` + sessionID + `","workdir":"/tmp/proj"}`,
	}
	for i, r := range rows {
		line := `{"type":"user","timestamp":"2026-10-27T08:0` + string(rune('1'+i)) + `:00Z"`
		if r.source != "" {
			line += `,"source":"` + r.source + `"`
		}
		line += `,"message":{"role":"user","content":` + jsonString(r.text) + `}}`
		lines = append(lines, line)
	}
	lines = append(lines, `{"type":"session_end","timestamp":"2026-10-27T08:09:00Z","elapsed_sec":540,"exit_code":0}`)
	path := filepath.Join(bare, taskBareSessionRecordName)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write bare session jsonl: %v", err)
	}
	return path
}

// TestChatFactsReadsTheBareLayout pins the resolver, not just the parse: the log
// lives at `sessions/<id>/session.jsonl` and the facts must still be found.
func TestChatFactsReadsTheBareLayout(t *testing.T) {
	chatTestEnv(t)
	sid := "chat-fm-0055"
	logPath := writeChatSessionJSONLBare(t, sid, []struct{ text, source string }{
		{chatNoticeFirstMsg, inputsource.Notify},
		{"the bare layout's operator line", inputsource.Human},
	})
	facts := chatSessionFactsOf(sid)
	if facts == nil {
		t.Fatalf("chatSessionFactsOf(%s) = nil; want the facts read from %s — a flat-only lookup cannot see this layout", sid, logPath)
	}
	if facts.FirstMsg != "the bare layout's operator line" {
		t.Errorf("facts.FirstMsg = %q; want the operator line from the bare-layout log", facts.FirstMsg)
	}
	if facts.LogPath != logPath {
		t.Errorf("facts.LogPath = %q; want %q", facts.LogPath, logPath)
	}
}

// TestChatRepairFirstMsgsSecondRunIsANoOp pins the REPORT of a re-run: a row the
// first pass left holding the placeholder has nothing to change, and saying
// otherwise is a false claim about work done.
//
// MEASURED 2026-10-30 (the defect this pins): the second pass printed
//
//	repair bf1ae7ff-…: "(no operator line yet)" -> "(no operator line yet)"
//
// and counted it, so `repaired` over-reported on every run after the first.
func TestChatRepairFirstMsgsSecondRunIsANoOp(t *testing.T) {
	chatTestEnv(t)
	sid := "chat-fm-0070"
	writeChatSessionJSONLBare(t, sid, []struct{ text, source string }{
		{chatNoticeFirstMsg, inputsource.Notify}, // no operator line anywhere
	})
	if _, err := ChatEnsureRow(ChatSession{SessionID: sid, Mode: ChatModeCLI}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	first, err := ChatRepairFirstMsgs(false)
	if err != nil {
		t.Fatalf("first repair: %v", err)
	}
	_ = first // the row already holds the placeholder; the point is the SECOND run
	second, err := ChatRepairFirstMsgs(false)
	if err != nil {
		t.Fatalf("second repair: %v", err)
	}
	for _, r := range second {
		if r.SessionID != sid {
			continue
		}
		if r.After != "" {
			t.Errorf("second run claims to have written %q over %q; it must report nothing to change", r.After, r.Before)
		}
		if r.Reason != ChatFirstMsgRepairNothingToChange {
			t.Errorf("second run reason = %q; want %q", r.Reason, ChatFirstMsgRepairNothingToChange)
		}
	}
	// And a row the first pass repaired from an injected line to the placeholder
	// must also read as nothing-to-change on the second pass.
	brokenID := "chat-fm-0071"
	writeChatSessionJSONLBare(t, brokenID, []struct{ text, source string }{
		{chatNoticeBatchFirstMsg, inputsource.Notify},
	})
	if _, err := ChatEnsureRow(ChatSession{SessionID: brokenID, Mode: ChatModeCLI}); err != nil {
		t.Fatalf("ensure broken: %v", err)
	}
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		t.Fatalf("open chat db: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE chats SET first_msg = ? WHERE session_id = ?`,
		chatNoticeBatchFirstMsg, brokenID); err != nil {
		_ = db.Close() //stylegate:ignore the seed already failed; this close is teardown, its own error cannot be reported over the fatal below
		t.Fatalf("seed old-style head: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the seed handle: %v", err)
	}
	if _, err := ChatRepairFirstMsgs(false); err != nil {
		t.Fatalf("repair broken: %v", err)
	}
	third, err := ChatRepairFirstMsgs(false)
	if err != nil {
		t.Fatalf("third repair: %v", err)
	}
	for _, r := range third {
		if r.SessionID == brokenID && r.After != "" {
			t.Errorf("re-repair of %s wrote %q again; the repair must be recomputable", brokenID, r.After)
		}
	}
}

// jsonString encodes s as a JSON string literal (the fixture's own quoting, so a
// text carrying a quote cannot silently produce a torn line).
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// TestChatFactsFirstMsgSkipsInjectedRows: the log scan used by `chat backfill`
// picks the first OPERATOR row, not the first user row — the same predicate the
// live path uses, so the two cannot disagree.
func TestChatFactsFirstMsgSkipsInjectedRows(t *testing.T) {
	chatTestEnv(t)
	sid := "chat-fm-0050"
	logPath := writeChatSessionJSONLWithSources(t, sid, []struct{ text, source string }{
		{chatNoticeFirstMsg, inputsource.Notify},
		{chatNoticeFirstMsg, inputsource.Human}, // the boundary's restatement
		{"why did the worker die?", inputsource.Human},
		{"a later question", inputsource.Human},
	})
	facts := chatSessionFactsOf(sid)
	if facts == nil {
		t.Fatal("chatSessionFactsOf returned nil for a log it was handed")
	}
	if facts.FirstMsg != "why did the worker die?" {
		t.Errorf("facts.FirstMsg = %q; want the first OPERATOR row (not the delivery, not its restatement)", facts.FirstMsg)
	}
	if facts.FirstMsgSource != inputsource.Human {
		t.Errorf("facts.FirstMsgSource = %q; want %q", facts.FirstMsgSource, inputsource.Human)
	}
	if facts.LogPath != logPath {
		t.Errorf("facts.LogPath = %q; want %q", facts.LogPath, logPath)
	}
}

// TestChatRepairFirstMsgsReDerivesFromLog is the historical-row repair: a row
// titled by the old rule is re-derived from its own log, a row already holding
// the operator's line is kept untouched, and a row whose log this machine does
// not hold is REPORTED and left alone (never blanked).
func TestChatRepairFirstMsgsReDerivesFromLog(t *testing.T) {
	chatTestEnv(t)
	// A row as the old rule left it: the head is the framework's sentence.
	brokenID := "chat-fm-0060"
	writeChatSessionJSONLWithSources(t, brokenID, []struct{ text, source string }{
		{chatNoticeFirstMsg, inputsource.Notify},
		{"why did the worker die?", inputsource.Human},
	})
	if _, err := ChatEnsureRow(ChatSession{SessionID: brokenID, Mode: ChatModeCLI}); err != nil {
		t.Fatalf("ensure broken: %v", err)
	}
	// Force the OLD stored value on, exactly what the pre-fix writer produced.
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		t.Fatalf("open chat db: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE chats SET first_msg = ? WHERE session_id = ?`,
		chatNoticeFirstMsg, brokenID); err != nil {
		_ = db.Close() //stylegate:ignore the seed already failed; this close is teardown, its own error cannot be reported over the fatal below
		t.Fatalf("seed old-style head: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the seed handle: %v", err)
	}

	// A healthy row: the operator's own line is already stored.
	healthyID := "chat-fm-0061"
	if _, err := ChatEnsureRow(ChatSession{SessionID: healthyID, Mode: ChatModeCLI,
		FirstMsg: "already the person's words", FirstMsgSource: inputsource.Human}); err != nil {
		t.Fatalf("ensure healthy: %v", err)
	}
	// A row whose log is not on this machine.
	orphanID := "chat-fm-0062"
	if _, err := ChatEnsureRow(ChatSession{SessionID: orphanID, Mode: ChatModeCLI, LogPath: "/nonexistent/x.jsonl"}); err != nil {
		t.Fatalf("ensure orphan: %v", err)
	}

	// DRY RUN first: it reports the same answer and writes nothing.
	dry, err := ChatRepairFirstMsgs(true)
	if err != nil {
		t.Fatalf("dry-run repair: %v", err)
	}
	byID := map[string]ChatFirstMsgRepair{}
	for _, r := range dry {
		byID[r.SessionID] = r
	}
	if got := byID[brokenID].After; got != "why did the worker die?" {
		t.Errorf("dry-run would write %q; want the operator's first line", got)
	}
	dryRow, err := ChatShow(brokenID)
	if err != nil {
		t.Fatalf("show %s after the dry run: %v", brokenID, err)
	}
	if dryRow.FirstMsg != chatNoticeFirstMsg {
		t.Errorf("dry run WROTE: first_msg = %q; it must not", dryRow.FirstMsg)
	}

	repairs, err := ChatRepairFirstMsgs(false)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	byID = map[string]ChatFirstMsgRepair{}
	for _, r := range repairs {
		byID[r.SessionID] = r
	}
	if got := byID[brokenID].After; got != "why did the worker die?" {
		t.Errorf("repaired head = %q; want the operator's first line", got)
	}
	repairedRow, err := ChatShow(brokenID)
	if err != nil {
		t.Fatalf("show %s after the repair: %v", brokenID, err)
	}
	if repairedRow.FirstMsg != "why did the worker die?" {
		t.Errorf("stored first_msg = %q; want the re-derived operator line", repairedRow.FirstMsg)
	}
	if r := byID[healthyID]; r.Reason != ChatFirstMsgRepairKept {
		t.Errorf("healthy row: reason = %q; want %q (nothing to repair)", r.Reason, ChatFirstMsgRepairKept)
	}
	if !strings.HasPrefix(byID[orphanID].Reason, ChatFirstMsgRepairNoLogPrefix) {
		t.Errorf("orphan row: reason = %q; want it to start with %q", byID[orphanID].Reason, ChatFirstMsgRepairNoLogPrefix)
	}
	orphanRow, err := ChatShow(orphanID)
	if err != nil {
		t.Fatalf("show %s: %v", orphanID, err)
	}
	if orphanRow.FirstMsg != ChatFirstMsgPlaceholder {
		t.Errorf("orphan row was blanked: %q", orphanRow.FirstMsg)
	}

	// IDEMPOTENT: the second pass has nothing to fix for the repaired row.
	again, err := ChatRepairFirstMsgs(false)
	if err != nil {
		t.Fatalf("second repair: %v", err)
	}
	for _, r := range again {
		if r.SessionID == brokenID && r.After != "" {
			t.Errorf("second pass rewrote %s again (%q -> %q); the repair must be recomputable", brokenID, r.Before, r.After)
		}
	}
}
