package testsuite

// chat_once_text_test.go — the `--once` face of the input criterion (epic
// 01a104cb), driven through the REAL binary against a local mock provider.
//
// The three claims, and why each needs the real binary rather than a unit call:
//
//  1. a "/"-opening line that is TEXT (a pasted path, a lone slash) leaves the
//     console and reaches the model — asserted as RequestCount() > 0 AND as the
//     text on the WIRE, because "it was not refused" is not "it was sent";
//  2. the literal-send escape ("//word") sends the command-SHAPED line to the
//     model with one slash intact — the channel a user needs precisely because
//     the line LOOKS like a command;
//  3. a real command still dispatches locally and an unknown command is still
//     REFUSED with a non-zero status — the no-regression limb, which is what
//     stops the escape from swallowing typos.
//
// The transcript shape matters and is deliberate: `--once` reads a scripted
// session only when its FIRST line is a command or an escape (chat_once_seq.go),
// so a test of the TEXT case drives the one-prompt face (the caller's own
// spelling stays whatever it was) while a test of the ESCAPE case drives the
// REPL's line loop. Both are the caller's real spellings.

import (
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// chatOnceTextMockAnswer is what the mock answers IF a line reaches it — a
// distinctive string, so "the model was asked" can be read off stdout as well as
// off the request counter.
const chatOnceTextMockAnswer = "THE-MOCK-MODEL-ANSWERED"

// runChatOnceScript drives `gopi chat --once` with `stdin` through the real
// binary and returns what the caller sees.
func runChatOnceScript(t *testing.T, stdin string) (stdout, stderr string, code int, llm *testutil.MockLLM) {
	t.Helper()
	home := t.TempDir()
	work := t.TempDir()

	llm = testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceTextMockAnswer})
	t.Cleanup(llm.Close)

	stdout, stderr, code = testutil.RunGopi(t, []string{"chat", "--once"}, stdin,
		testutil.MockEnv(t, home, llm.URL()), work)
	return stdout, stderr, code, llm
}

// chatOnceTextSentToModel reports whether the model was asked for `want` — read
// from the request's USER MESSAGES, never from the body as a whole: the body
// carries gopi's system prompt, which documents this surface's own slash words,
// so a substring search would report a leak that never happened (measured trap,
// memory #5868).
func chatOnceTextSentToModel(t *testing.T, llm *testutil.MockLLM, want string) bool {
	t.Helper()
	for _, text := range chatOnceWireUserTexts(t, llm, 1) {
		if strings.Contains(text, want) {
			return true
		}
	}
	return false
}

// TestChatOnceText_PastedPathReachesTheModel is the epic's headline case on this
// face: the input that used to be refused as an unknown command is now the
// model's message. It is also the RED proof's target — before the classifier,
// this run exited 1 with `unknown command "/tmp/cn-result.gif"` and made ZERO
// requests.
func TestChatOnceText_PastedPathReachesTheModel(t *testing.T) {
	const path = "/tmp/CN-result.gif"
	stdout, stderr, code, llm := runChatOnceScript(t, path+"\n/quit\n")

	if code != 0 {
		t.Fatalf("exit = %d, want 0 (a path is a message, not a refusal)\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if n := llm.RequestCount(); n == 0 {
		t.Fatalf("the model was never asked — the path was swallowed by the console\n--- stdout ---\n%s\n--- stderr ---\n%s", stdout, stderr)
	}
	if !chatOnceTextSentToModel(t, llm, path) {
		t.Errorf("the recorded request's user messages do not carry %q — the line did not arrive verbatim", path)
	}
	if strings.Contains(stderr, "unknown command") {
		t.Errorf("a pasted path was still refused as a command:\n%s", stderr)
	}
}

// TestChatOnceText_LoneSlashIsAMessage pins the smallest case of the criterion:
// a lone "/" is not a command and not a refusal — it is text.
func TestChatOnceText_LoneSlashIsAMessage(t *testing.T) {
	stdout, stderr, code, llm := runChatOnceScript(t, "/\n")

	if code != 0 {
		t.Fatalf("exit = %d, want 0 for a lone slash\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if n := llm.RequestCount(); n == 0 {
		t.Fatalf("a lone \"/\" was not sent to the model:\n--- stdout ---\n%s\n--- stderr ---\n%s", stdout, stderr)
	}
	if strings.Contains(stderr, "unknown command") {
		t.Errorf("a lone slash was refused as a command:\n%s", stderr)
	}
}

// TestChatOnceText_EscapeSendsTheCommandShapedLineLiterally is the escape
// channel on this face: "//quit" does NOT end the session, and the model is
// asked for "/quit" — one slash, verbatim. The transcript opens with the escape
// line, so the caller is driving a scripted session (the line loop), which is
// exactly how a user reaches for the channel after being refused.
func TestChatOnceText_EscapeSendsTheCommandShapedLineLiterally(t *testing.T) {
	stdout, stderr, code, llm := runChatOnceScript(t, "//quit\n/quit\n")

	if code != 0 {
		t.Fatalf("exit = %d, want 0 (the escape is a message, the trailing /quit ends the session)\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if n := llm.RequestCount(); n == 0 {
		t.Fatalf("the escaped line never reached the model\n--- stdout ---\n%s\n--- stderr ---\n%s", stdout, stderr)
	}
	if !chatOnceTextSentToModel(t, llm, "/quit") {
		t.Errorf("the model was not asked for the literal %q — the escape lost its slash", "/quit")
	}
	if strings.Contains(stderr, "unknown command") {
		t.Errorf("the escape was refused as a command:\n%s", stderr)
	}
	// The escape is NOT the command: the session ended because of the SECOND
	// line, not the first (the REPL's own goodbye proves the loop ran the
	// trailing /quit).
	if !strings.Contains(stderr, "bye.") {
		t.Errorf("the trailing /quit did not end the session:\n%s", stderr)
	}
}

// TestChatOnceText_UnknownCommandIsStillRefused: the no-regression limb that
// makes the escape safe to add — a command-SHAPED word the carrier lacks is
// still refused, still names the escape, and still never reaches the model.
func TestChatOnceText_UnknownCommandIsStillRefused(t *testing.T) {
	stdout, stderr, code, llm := runChatOnceScript(t, "/nonexistent\n/quit\n")

	if code == 0 {
		t.Errorf("a refused command left exit = 0 (Rule 15: a silent 0 for a line that did not run)\n--- stdout ---\n%s\n--- stderr ---\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "unknown command") || !strings.Contains(stderr, "/nonexistent") {
		t.Errorf("stderr carries no refusal naming the word:\n%s", stderr)
	}
	if !strings.Contains(stderr, "available:") {
		t.Errorf("the refusal lost its command table:\n%s", stderr)
	}
	if !strings.Contains(stderr, "//nonexistent") {
		t.Errorf("the refusal does not teach the literal-send channel:\n%s", stderr)
	}
	if n := llm.RequestCount(); n != 0 {
		t.Errorf("an unknown command made %d model request(s), want 0 — a typo became a question", n)
	}
}

// TestChatOnceText_RealCommandsStillDispatch is the other half of the
// no-regression limb: the four words the epic names are still local commands,
// and the model is never asked one of them.
func TestChatOnceText_RealCommandsStillDispatch(t *testing.T) {
	cases := []struct {
		script string
		want   string
	}{
		{"/help\n/quit\n", "Slash commands (executed locally"},
		{"/task list\n/quit\n", "task"},
		{"/tools\n/quit\n", "tools"},
		{"/quit\n", "bye."},
	}
	for _, tc := range cases {
		t.Run(strings.TrimSpace(strings.Split(tc.script, "\n")[0]), func(t *testing.T) {
			stdout, stderr, code, llm := runChatOnceScript(t, tc.script)
			if code != 0 {
				t.Fatalf("exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
			}
			if n := llm.RequestCount(); n != 0 {
				t.Errorf("a real command made %d model request(s), want 0 — it was SENT instead of dispatched", n)
			}
			if !strings.Contains(stdout+stderr, tc.want) {
				t.Errorf("the command's own output does not carry %q:\n--- stdout ---\n%s\n--- stderr ---\n%s", tc.want, stdout, stderr)
			}
		})
	}
}

// TestChatOnceText_OnePromptContractIsUnchanged pins the contract the epic
// forbids changing: input that does NOT open with a command (or an escape) is
// still ONE message, multi-line folded — a scripted line only exists when the
// first line is a command.
func TestChatOnceText_OnePromptContractIsUnchanged(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceTextMockAnswer})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"}, "first line\nsecond line\n",
		testutil.MockEnv(t, home, llm.URL()), work)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if n := llm.RequestCount(); n != 1 {
		t.Fatalf("a plain two-line prompt made %d request(s), want 1 (the one-prompt contract)", n)
	}
	texts := chatOnceWireUserTexts(t, llm, 1)
	if !strings.Contains(strings.Join(texts, "\n"), "first line") || !strings.Contains(strings.Join(texts, "\n"), "second line") {
		t.Errorf("the folded message lost a line: %v", texts)
	}
}

// TestChatOnceText_NoLitterInTheWorkDir: the scripted face writes nothing into
// the caller's directory (Rule 30's spirit — a run's own residue is its own).
func TestChatOnceText_NoLitterInTheWorkDir(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceTextMockAnswer})
	defer llm.Close()

	if _, stderr, code := testutil.RunGopi(t, []string{"chat", "--once"}, "/tmp/CN-result.gif\n/quit\n",
		testutil.MockEnv(t, home, llm.URL()), work); code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, stderr)
	}
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the run left %d entries in the working directory: %v", len(entries), names)
	}
}
