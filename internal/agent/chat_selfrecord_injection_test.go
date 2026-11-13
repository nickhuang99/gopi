package agent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chatSelfRecordFixtureServer answers one round with a body carrying the record
// block, so the CAPTURE half (recordSelfNotes → self-record.md) is exercised
// through the real chat path rather than by calling the extractor directly.
func chatSelfRecordFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("drain request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{
			"done.\n\n" + selfRecordOpenMarker + "\n",
			"round one recorded, all green\n",
			selfRecordCloseMarker + "\n",
		}, "end_turn")
	}))
}

// TestChatPlainPathServesSelfRecordContract is the WIRING half for the plain
// chat path: the record contract must be ON the prompt the plain chat actually
// serves. Delete the append in assembleChatSystemPrompt and this fails with "the
// plain chat prompt carries no record contract".
func TestChatPlainPathServesSelfRecordContract(t *testing.T) {
	settingsWithOutputLanguage(t, `{"model":"m"}`)
	got := chatSystemPromptServed(nil)
	if !strings.Contains(got, selfRecordOpenMarker) || !strings.Contains(got, selfRecordCloseMarker) {
		t.Fatalf("the plain chat prompt carries no record contract:\n%s", got)
	}
	if strings.Count(got, selfRecordOpenMarker) != 1 {
		t.Fatalf("the contract must appear exactly once on the chat prompt, got %d", strings.Count(got, selfRecordOpenMarker))
	}
}

// TestChatToolsPathServesSelfRecordContract is the second assembly point: the
// tool-enabled chat path composes its own prompt (chatOpsSystemPrompt) and must
// carry the contract too, or "the chat surface records its rounds" would be true
// of one chat and false of the other.
func TestChatToolsPathServesSelfRecordContract(t *testing.T) {
	settingsWithOutputLanguage(t, `{"model":"m"}`)
	got := chatOpsSystemPrompt(&Agent{SystemPrompt: "base"})
	if !strings.Contains(got, selfRecordOpenMarker) {
		t.Fatalf("the tool-enabled chat prompt carries no record contract:\n%s", got)
	}
	if !strings.Contains(got, "base") {
		t.Fatalf("the contract must be APPENDED — the prompt it was added to is gone")
	}
}

// TestChatContractLandsAtTheTail pins the placement that keeps the prompt cache
// prefix stable: the contract is the LAST bytes of the served prompt, so it can
// move no block that came before it.
func TestChatContractLandsAtTheTail(t *testing.T) {
	settingsWithOutputLanguage(t, `{"model":"m"}`)
	got := chatSystemPromptServed(nil)
	if !strings.HasSuffix(got, chatSelfRecordRule) {
		t.Fatalf("the record contract is not the tail of the chat prompt — a mid-prompt landing would move the bytes of every block after it:\n...%s", tail(got))
	}
}

// TestChatContractIsIdempotentAndHeadSafe pins the two properties the append
// must have: applying it twice adds nothing (byte-stable, so a re-render per
// turn cannot grow the prefix), and the operator's reply-language directive
// stays the HEAD with the persona body at the very next block.
func TestChatContractIsIdempotentAndHeadSafe(t *testing.T) {
	settingsWithOutputLanguage(t, `{"model":"m"}`)
	t.Setenv("GOPI_OUTPUT_LANGUAGE", "zh-CN")
	once := chatSystemPromptServed(nil)
	twice := maybeAppendChatSelfRecordRule(once)
	if once != twice {
		t.Fatalf("the append is not idempotent — a re-render would grow the prompt: %d bytes vs %d", len(once), len(twice))
	}
	if !strings.HasPrefix(once, outputLanguageDirectiveMarker) {
		t.Fatalf("the reply-language directive must stay the head, got %q", head(once))
	}
	first, rest, ok := strings.Cut(once, promptSectionSeparator)
	if !ok {
		t.Fatal("the served prompt carries no section separator at all")
	}
	if !strings.HasPrefix(first, outputLanguageDirectiveMarker) || !strings.HasPrefix(rest, freezePreKnobChatPrompt) {
		t.Fatalf("directive and persona body must stay adjacent heads:\n first=%q\n rest=%q", head(first), head(rest))
	}
}

// TestChatEmptyPromptStaysEmpty is the emptied-prompt limb: appending to nothing
// must produce nothing (no stray separator), the same contract the block
// assemblies already honour.
func TestChatEmptyPromptStaysEmpty(t *testing.T) {
	if got := maybeAppendChatSelfRecordRule(""); got != "" {
		t.Fatalf("an empty prompt must stay empty, got %q", got)
	}
}

// TestChatRoundRecordsIntoTheSessionFile is the CAPTURE half end to end: a chat
// round whose answer carries the block lands a note in the run's OWN
// side-channel file, i.e. the chat path reaches recordSelfNotes. Before this
// change the chat path never called it, so a chat round's block was recorded
// nowhere (measured: 0 compliant rounds in 7314).
func TestChatRoundRecordsIntoTheSessionFile(t *testing.T) {
	server := chatSelfRecordFixtureServer(t)
	defer server.Close()
	ag := singleSessionAgent(t, server)

	if _, err := ag.Chat("say something", nil); err != nil {
		t.Fatalf("Chat(): %v", err)
	}
	dir, err := sessionDirForID(ag.SessionLog.SessionID(), "")
	if err != nil {
		t.Fatalf("sessionDirForID: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, selfRecordFileName))
	if err != nil {
		t.Fatalf("the chat round recorded nothing into %s: %v", selfRecordFileName, err)
	}
	if !strings.Contains(string(body), "round one recorded, all green") {
		t.Fatalf("%s does not carry the round's note:\n%s", selfRecordFileName, body)
	}
}

// TestChatOpsRoundRecordsIntoTheSessionFile is the CAPTURE half for the
// TOOL-ENABLED path: ChatOps returns a different place than Chat (the
// no-tool round's accept branch), so the wiring has to be pinned on both or a
// tool-enabled chat could serve the contract and record nothing.
//
// The fixture's /v1/messages stub answers a plain text round (no tool_use), so
// the round lands on the accept branch this test is about.
func TestChatOpsRoundRecordsIntoTheSessionFile(t *testing.T) {
	server := chatSelfRecordFixtureServer(t)
	defer server.Close()

	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	sl, err := NewSessionLogger("deepseek-flash", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	ag := newTestAgent(t, server)
	ag.SessionLog = sl
	ag.SystemPrompt = assembleSystemPrompt()
	ag.Ctx = &Context{}
	if _, err := ag.ChatOps("say something", nil); err != nil {
		t.Fatalf("ChatOps(): %v", err)
	}
	// No Close here: ChatOps owns the chat session's lifetime (it closes the
	// logger at the turn's finishChat), and a second Close on the same JSONL is
	// a test bug wearing a production error's clothes.
	dir, err := sessionDirForID(sl.SessionID(), "")
	if err != nil {
		t.Fatalf("sessionDirForID: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, selfRecordFileName))
	if err != nil {
		t.Fatalf("the tool-enabled chat round recorded nothing into %s: %v", selfRecordFileName, err)
	}
	if !strings.Contains(string(body), "round one recorded, all green") {
		t.Fatalf("%s does not carry the round's note:\n%s", selfRecordFileName, body)
	}
}
