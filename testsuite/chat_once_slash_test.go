package testsuite

// chat_once_slash_test.go — the SCRIPTED face of `gopi chat --once`
// (cmd/agent/chat_once_seq.go): a non-TTY caller drives the REPL's own operator
// controls by writing the lines a human would type.
//
// Every case drives the REAL binary through testutil.RunGopi against a local
// mock provider, so what is asserted is what a driver's shell sees. The two
// claims that matter are BOTH on the wire and on the caller's own streams:
//
//   - a `/`-line is DISPATCHED BY THE REPL, never sent to the model — asserted
//     as the mock having served ZERO requests (a claim about the run's traffic,
//     not about its text: an answer's presence can never prove what was NOT
//     sent);
//   - a command that cannot be honoured fails LOUDLY with a specific message and
//     a non-zero status (PROJECT.md Rule 15), instead of leaving a script a silent
//     0.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// chatOnceSlashMockAnswer is the answer the mock would give IF a slash line ever
// reached it. Its absence from stdout is how a "never sent to the model" claim is
// double-checked against the text as well as against the traffic counter.
const chatOnceSlashMockAnswer = "THE-MODEL-ANSWERED-SO-THIS-WAS-SENT-TO-IT"

// chatOnceWireUserTexts returns the text of every `user` message on the n-th
// recorded request, in order.
//
// WHY NOT A SUBSTRING CHECK ON THE WHOLE BODY: the request body carries a SYSTEM
// prompt, and that prompt documents this surface — so a body-wide search for
// "/tools" matches the system prompt and reports a leak that never happened
// (measured: the mutation test below failed on the system prompt's own text). A
// claim about what was SENT must be made about the message list, not about the
// bytes.
func chatOnceWireUserTexts(t *testing.T, llm *testutil.MockLLM, n int) []string {
	t.Helper()
	body := llm.RequestBody(n)
	if body == "" {
		t.Fatalf("no recorded request %d to read the wire from", n)
	}
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("the recorded request %d is not the JSON this test expects: %v", n, err)
	}
	var texts []string
	for _, m := range req.Messages {
		if m.Role != "user" {
			continue
		}
		// The content is either a bare string (a plain message) or an array of
		// blocks (a message carrying tool results); both shapes carry the user's
		// text, so both are read.
		var s string
		if err := json.Unmarshal(m.Content, &s); err == nil {
			texts = append(texts, s)
			continue
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			continue
		}
		var joined []string
		for _, b := range blocks {
			if b.Text != "" {
				joined = append(joined, b.Text)
			}
		}
		texts = append(texts, strings.Join(joined, "\n"))
	}
	return texts
}

// TestChatOnceSlash_ArgumentIsDispatchedNotSentToModel is the argv shape: a
// command line on the command line is executed by the REPL's own dispatch, and
// the model is never called for it.
func TestChatOnceSlash_ArgumentIsDispatchedNotSentToModel(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceSlashMockAnswer})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once", "/help"}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi chat --once /help exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	// (a) the command's OWN output — the shared table, exactly as the interactive
	// REPL prints it (one dispatch, no second table).
	if !strings.Contains(stdout, "Slash commands (executed locally — never sent to the model):") {
		t.Errorf("stdout carries no /help table:\n%s", stdout)
	}
	// (b) THE claim: no model request was issued for a command line.
	if n := llm.RequestCount(); n != 0 {
		t.Errorf("the model was called %d time(s) for a slash command, want 0 — the line was SENT instead of dispatched", n)
	}
	if strings.Contains(stdout, chatOnceSlashMockAnswer) {
		t.Errorf("the model's answer is on stdout — the slash line reached the model:\n%s", stdout)
	}
}

// TestChatOnceSlash_StdinSequenceIsDispatched is the piped shape, and the one the
// epic is about: SEVERAL lines, so the caller can issue a command AND end the
// session the way a human does (/quit). Before this, every line after the first
// was folded into one message and shipped to the model.
func TestChatOnceSlash_StdinSequenceIsDispatched(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceSlashMockAnswer})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"}, "/help\n/quit\n",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("the scripted session exit = %d, want 0 (a clean /quit is the caller's own end-of-session)\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Slash commands (executed locally") {
		t.Errorf("stdout carries no /help table:\n%s", stdout)
	}
	if n := llm.RequestCount(); n != 0 {
		t.Errorf("the model was called %d time(s) for a command-only script, want 0", n)
	}
	// /quit really ended the session through the REPL's own path (its goodbye is
	// the same one an interactive user gets).
	if !strings.Contains(stderr, "bye.") {
		t.Errorf("stderr carries no goodbye — the REPL's own /quit path did not run:\n%s", stderr)
	}
	// The session was finalized (the REPL's closeout), so a scripted run leaves
	// the same durable trace an interactive one does.
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertCounts(t, map[string]int{"session_start": 1, "session_end": 1})
}

// TestChatOnceSlash_CommandThenMessageInOneScript is the capability a driver
// needs and could not have: a command line AND a message in one invocation, each
// reaching the arm it names — the command dispatched locally, the message sent to
// the model as the turn's user message.
func TestChatOnceSlash_CommandThenMessageInOneScript(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceSlashMockAnswer})
	defer llm.Close()

	const scriptedMessage = "summarise the situation please"
	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"},
		"/tools\n"+scriptedMessage+"\n/quit\n", testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	// Exactly ONE model request: ONE message line, and no request for either
	// command line.
	if n := llm.RequestCount(); n != 1 {
		t.Fatalf("the model was called %d time(s), want exactly 1 (the one message line — the two command lines must be dispatched locally)", n)
	}
	body := llm.RequestBody(1)
	if !strings.Contains(body, scriptedMessage) {
		t.Errorf("the model request does not carry the scripted message: %.400s", body)
	}
	// THE claim, made about what was SENT: no user message on the wire is a
	// command line.
	for _, text := range chatOnceWireUserTexts(t, llm, 1) {
		if strings.Contains(text, "/tools") {
			t.Errorf("a COMMAND line is on the wire — it was sent to the model instead of being dispatched: %q", text)
		}
	}
	if !strings.Contains(stdout, chatOnceSlashMockAnswer) {
		t.Errorf("stdout = %q, want the turn's answer", stdout)
	}
	// The command's OWN effect is visible: /tools announces the toggle through the
	// REPL's notice, on the stream the REPL has always used for it.
	if !strings.Contains(stderr, "tools ENABLED") {
		t.Errorf("stderr does not carry /tools' own toggle notice:\n%s", stderr)
	}
}

// TestChatOnceSlash_RefusedCommandIsLoudAndNonZero is the failure contract
// (Rule 15): a line the surface cannot honour must be a specific message AND a
// non-zero status — never a silent 0, and never a fallback that ships the line to
// the model.
func TestChatOnceSlash_RefusedCommandIsLoudAndNonZero(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceSlashMockAnswer})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once", "/definitely-not-a-command"}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code == 0 {
		t.Fatalf("an unhonourable command line exit = 0, want non-zero\n--- stdout ---\n%s\n--- stderr ---\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "definitely-not-a-command") {
		t.Errorf("stderr does not NAME the offending command:\n%s", stderr)
	}
	if !strings.Contains(stderr, "not honoured") {
		t.Errorf("stderr does not say the scripted line was not honoured:\n%s", stderr)
	}
	// The refusal must not become a model turn.
	if n := llm.RequestCount(); n != 0 {
		t.Errorf("the model was called %d time(s) for a refused command, want 0 — the fallback the epic forbids", n)
	}
	// And the fabricated "turn failed" of the old face is gone: nothing was asked
	// of the model, so nothing failed a turn.
	if strings.Contains(stderr, "turn failed") || strings.Contains(stderr, "without producing any text") {
		t.Errorf("stderr reports a MODEL TURN failure for a run with no turn:\n%s", stderr)
	}
}

// TestChatOnceSlash_OnDiskSideEffect is the acceptance case that a command really
// EXECUTED rather than being echoed: /text2img renders a PNG through the shared
// engine, so the artifact is read back from disk afterwards. Zero network, so the
// on-disk truth is the only thing that could make this pass.
func TestChatOnceSlash_OnDiskSideEffect(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceSlashMockAnswer})
	defer llm.Close()

	out := filepath.Join(work, "scripted-once.png")
	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once", "/text2img GOPI-ONCE --out " + out}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	// (a) the command did its work on disk.
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("the scripted /text2img produced no file at %s: %v\n--- stdout ---\n%s\n--- stderr ---\n%s", out, err, stdout, stderr)
	}
	if info.Size() == 0 {
		t.Fatalf("the scripted /text2img wrote an EMPTY file at %s", out)
	}
	// (b) it is really a PNG (the format's own magic), read back from disk.
	head := make([]byte, 8)
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open the artifact: %v", err)
	}
	defer f.Close()
	if _, err := f.Read(head); err != nil {
		t.Fatalf("read the artifact's header: %v", err)
	}
	if string(head[:4]) != "\x89PNG" {
		t.Errorf("the artifact at %s is not a PNG (header %q)", out, head)
	}
	// (c) the command's result is on the caller's stream, and the model saw
	// nothing at all.
	if !strings.Contains(stdout, out) {
		t.Errorf("stdout does not carry the command's own result (the output path):\n%s", stdout)
	}
	if n := llm.RequestCount(); n != 0 {
		t.Errorf("the model was called %d time(s) for a local rendering command, want 0", n)
	}
}

// TestChatOnceSlash_PlainPromptIsStillOneMessage is the regression guard on the
// OTHER face of --once: input that does NOT open with a command is one prompt,
// folded into ONE message exactly as before. The scripted face must not have
// captured ordinary one-shot prompts.
func TestChatOnceSlash_PlainPromptIsStillOneMessage(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceSlashMockAnswer})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"}, "first line\nsecond line\n",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stderr ---\n%s", code, stderr)
	}
	if n := llm.RequestCount(); n != 1 {
		t.Fatalf("the model was called %d time(s), want exactly 1 (one multi-line prompt is ONE message)", n)
	}
	if body := llm.RequestBody(1); !strings.Contains(body, "first line") || !strings.Contains(body, "second line") {
		t.Errorf("the folded prompt did not reach the model intact: %.400s", body)
	}
	if strings.TrimSpace(stdout) != "" && !strings.Contains(stdout, chatOnceSlashMockAnswer) {
		t.Errorf("stdout = %q, want the turn's answer alone", stdout)
	}
}

// TestChatOnceSlash_RefusalVocabularyMatchesTheHelpPage is the epic's acceptance
// case on the REAL BINARY: `printf '/frobnicate\n/quit\n' | gopi chat --once`
// must print, in one line, the SAME slash vocabulary the carrier's own /help
// page prints — every word slash-prefixed, the registry words included — and
// still exit non-zero (Rule 15: a scripted line that did not run is never 0).
//
// The comparison is between the TWO SURFACES the operator sees, both produced by
// this same run: the refusal on stderr and the /help page on stdout. That is
// what makes drift impossible to hide — a table command added later, or a native
// word the refusal forgets, breaks the equality.
//
// THE EXPECTED SET COMES FROM THE /help SURFACE, never from a second list
// spelled here (Rule 13/14 — one value, one definition). This test used to carry
// its own hand-written copy of the accepted words, and it rotted exactly as that
// shape predicts: /queue joined the REPL's vocabulary (ff76758c) and the copy
// (written in 6bb0135c) kept saying what the surface no longer said, so the case
// went red against a CORRECT producer. A hand copy cannot be kept current by
// anything — it is a mirror of a value that already has an owner. The owner is
// chatReplCommandNames (cmd/agent/commands.go) and the page it renders; the
// in-process gates that pin the page against that vocabulary — including the
// words the shared table cannot see — are cmd/agent/chat_slash_refusal_vocabulary_test.go
// (TestChatReplVocabularyCoversBothSources, TestChatRefusalVocabularyEqualsItsHelpPage).
// What THIS case adds, and the reason it stays on the real binary, is that the
// two surfaces are compared as two RENDERINGS of one run: a /help page and a
// refusal produced by the same process, one on stdout and one on stderr.
func TestChatOnceSlash_RefusalVocabularyMatchesTheHelpPage(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceSlashMockAnswer})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"}, "/frobnicate\n/help\n/quit\n",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code == 0 {
		t.Fatalf("a refused scripted command left exit = 0\n--- stdout ---\n%s\n--- stderr ---\n%s", stdout, stderr)
	}
	if n := llm.RequestCount(); n != 0 {
		t.Errorf("the model was called %d time(s) for refused/help lines, want 0", n)
	}

	// (a) the refusal names the refused word and prints a slash vocabulary.
	refusalLine := chatOnceSlashRefusalLine(stderr)
	if refusalLine == "" {
		t.Fatalf("stderr carries no refusal for the unknown command:\n%s", stderr)
	}
	got := chatOnceSlashTokenSet(t, refusalLine)
	if len(got) == 0 {
		t.Fatalf("the refusal lists no words:\n%s", refusalLine)
	}

	// (b) the OTHER surface this same run printed: the /help page. This IS the
	// expected set — the page is what the refusal has to agree with, and reading
	// it from the run is what removes the second list from this file (Rule
	// 13/14). Both spellings the page teaches a word in — the word starting a row
	// and an alias spelled inside one ("(also /exit)") — are accepted inputs, so
	// both belong to the set.
	helpWords := chatOnceSlashHelpVocabulary(stdout)
	if len(helpWords) == 0 {
		// A real failure, never a skip: with nothing extracted the equality below
		// would hand the producer a free pass (an empty set equals no set).
		t.Errorf("the run's /help printed no slash-command rows — the vocabulary comparison would be vacuous:\n--- stdout ---\n%s", stdout)
	}
	if !sameWordSet(got, helpWords) {
		t.Errorf("the refusal's vocabulary is not the vocabulary this run's /help page prints:\n got: %v\nwant: %v\nrefusal:\n%s",
			sortedWords(got), sortedWords(helpWords), refusalLine)
	}
	// (c) and say WHICH side leaked, so a failure names the direction of the
	// drift rather than only the two sets.
	for w := range helpWords {
		if !got[w] {
			t.Errorf("/help prints %s but the refusal does not list it — the refusal is the narrower one:\n%s", w, refusalLine)
		}
	}
	for w := range got {
		if !helpWords[w] {
			t.Errorf("the refusal lists %s, which this run's /help page does not print — the refusal is the wider one:\n%s", w, refusalLine)
		}
	}
}

// chatOnceSlashRefusalLine returns the line carrying the unknown-command refusal
// (the LAST one: the REPL's own refusal, and then the one-shot face's report of
// the same error, both name the word).
func chatOnceSlashRefusalLine(stderr string) string {
	line := ""
	for _, l := range strings.Split(stderr, "\n") {
		if strings.Contains(l, "unknown command") && strings.Contains(l, "/frobnicate") {
			line = l
		}
	}
	return line
}

// chatOnceSlashTokenSet reads the slash words out of a refusal line the way the
// operator reads them, and FAILS on a word printed without its slash (a bare
// word is a spelling the surface does not accept — the defect itself).
func chatOnceSlashTokenSet(t *testing.T, line string) map[string]bool {
	t.Helper()
	_, rest, ok := strings.Cut(line, "available:")
	if !ok {
		t.Fatalf("the refusal carries no available-command list:\n%s", line)
	}
	if list, _, ok := strings.Cut(rest, ";"); ok {
		rest = list
	}
	out := map[string]bool{}
	for _, tok := range strings.Split(rest, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if !strings.HasPrefix(tok, "/") {
			t.Errorf("the refusal lists %q WITHOUT a leading slash — a bare word is not a spelling this surface accepts:\n%s",
				tok, line)
		}
		out[tok] = true
	}
	return out
}

// chatOnceSlashHelpVocabulary extracts the slash vocabulary of the /help page
// THIS RUN printed, read the way the operator reads the page: a word that STARTS
// a line is a row of the vocabulary ("  /queue  queue a prompt …"), and a word
// spelled inside a row's prose as "(also /alias)" is the second accepted spelling
// of that row ("(also /exit — one command, two spellings)").
//
// This IS the expected set of the acceptance case — the page the refusal has to
// agree with — which is why the file carries no second copy of the vocabulary
// (Rule 13/14): one value, one definition, and the definition is the surface the
// operator is shown. A word the page prints but the parser does not accept would
// make the equality wrongly FAIL, so the equality is the assertion that both
// surfaces stay right.
func chatOnceSlashHelpVocabulary(stdout string) map[string]bool {
	out := map[string]bool{}
	add := func(word string) {
		if word != "" {
			out["/"+word] = true
		}
	}
	for _, line := range strings.Split(stdout, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "/"); ok {
			add(chatOnceSlashLeadingWord(rest))
		}
		// "(also /exit)" / "(also /new)" — the row's second accepted spelling.
		// A parenthetical that names no slash word ("(also -h on any command)")
		// contributes nothing (Rule 15: no guessing at a word that is not there).
		_, rest, ok := strings.Cut(line, "(also")
		if !ok {
			continue
		}
		if _, after, ok := strings.Cut(rest, "/"); ok {
			add(chatOnceSlashLeadingWord(after))
		}
	}
	return out
}

// chatOnceSlashLeadingWord returns the word a string starts with (letters,
// digits, hyphen), stopping at the first separator — the "/word" token a row
// begins with.
func chatOnceSlashLeadingWord(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-' || (c >= '0' && c <= '9') {
			continue
		}
		return s[:i]
	}
	return s
}

// sameWordSet reports whether two token sets are equal — the equality the epic
// asks for (not a containment in either direction).
func sameWordSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for w := range a {
		if !b[w] {
			return false
		}
	}
	return true
}

// sortedWords renders a word set deterministically for a failure message.
func sortedWords(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for w := range set {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// TestChatOnceSlash_KnownCommandKeepsItsOwnFailureMessage is the counter-case to
// the vocabulary agreement: re-rendering a refusal over the carrier's full
// vocabulary must apply ONLY to a word the carrier does not accept. A KNOWN
// command's own failure is a different fact ("your arguments are wrong"), and
// reporting it as "unknown command /task" would name the wrong defect — Rule 15's
// message must identify the real failure.
func TestChatOnceSlash_KnownCommandKeepsItsOwnFailureMessage(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceSlashMockAnswer})
	defer llm.Close()

	// /task is a shared-table command; the status word is the engine's own
	// validation (a real refusal from the engine, not from the command table).
	_, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"},
		"/task update not-an-id --status bogus\n/quit\n",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code == 0 {
		t.Errorf("a scripted line the engine refused left exit = 0\n--- stderr ---\n%s", stderr)
	}
	if strings.Contains(stderr, "unknown command") {
		t.Errorf("a KNOWN command's argument failure was reported as an unknown command:\n%s", stderr)
	}
	if !strings.Contains(stderr, "invalid status") {
		t.Errorf("the engine's own failure message did not reach stderr:\n%s", stderr)
	}
	if n := llm.RequestCount(); n != 0 {
		t.Errorf("the model was called %d time(s) for a locally-refused command, want 0", n)
	}
}
