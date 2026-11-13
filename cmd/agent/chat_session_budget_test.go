package main

// chat_session_budget_test.go — the chat surface's session-budget DEFAULT
// (cmd/agent/chat_session_budget.go).
//
// THE DEFECT THESE TESTS PIN (task 01a115c4-1ef0-7551-9960-9a5c34f3ed84): the
// tool-enabled chat path sends the TASK system prompt, whose rule 5 taught a
// fabricated 60-minute planning ceiling — measured on 2026-10-07 to have made a
// chat impose that deadline on itself and propose ending early. The repair is a
// DEFAULT (unset ⇒ "0" = unlimited), never an override, so the properties below
// are the whole contract.
//
// TWO LAYERS, because they catch different mistakes:
//
//   - the SEAM (this process): the publication's own behaviour — unset ⇒
//     unlimited, an explicit value untouched, a failure reported (Rule 15);
//   - the SURFACE (the real binary, `gopi chat --once --tools`): the sentence
//     the model actually reads, read back off the mock's captured request body,
//     because the defect was a sentence and only the wire can prove it is gone
//     (Rule 31: the surface, not a copy of it).

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
)

// The two sentences these tests assert on. They are the PROMPT's own words
// (internal/agent renders them from the budget source), quoted as constants so
// a rewording fails in ONE place and so an unrelated sentence mentioning a
// budget cannot satisfy the assertion.
const (
	chatBudgetFabricatedCeiling = "BUDGET: session budget 60min"
	chatBudgetUnlimitedRule     = "BUDGET: unlimited"
	// chatOnceReplyText is the mock provider's answer — the case is about the
	// REQUEST the run sends, so the reply only has to be a well-formed turn.
	chatOnceReplyText = "OK"
)

// TestChatBudgetPublishesUnlimitedWhenUnset is the headline case: a chat run
// that inherits NO budget export publishes unlimited, and the notice names the
// variable, the value and the reason (Rule 15 — a silent default would leave
// the operator's "why is my chat unlimited?" unanswerable).
//
// The variable is genuinely ABSENT for the call, not merely empty: the
// publication's input is the environment's own "unset", which is the whole
// defect (a budget nobody had decided on).
func TestChatBudgetPublishesUnlimitedWhenUnset(t *testing.T) {
	// Rule 17: a PER-CALL flip uses os.Setenv behind testutil.SnapshotEnv
	// (t.Setenv's scope is the whole test, which would hide the "was it absent
	// before?" half this assertion is about).
	defer testutil.SnapshotEnv(t, chatSessionBudgetEnv)()
	if err := os.Unsetenv(chatSessionBudgetEnv); err != nil { // env-leak-ok: testutil.SnapshotEnv restores this on test return, ABSENCE included — the case's SUBJECT is the variable being absent, which t.Setenv cannot express (it only ever sets a present value)
		t.Fatalf("unset %s: %v", chatSessionBudgetEnv, err)
	}

	stderr := captureStderrStream(t, func() { publishChatSessionBudgetDefault(termToolStylesFor(lineedit.ColourOff)) })

	if got := os.Getenv(chatSessionBudgetEnv); got != chatSessionBudgetUnlimited {
		t.Fatalf("%s = %q after the chat default, want %q (unlimited)",
			chatSessionBudgetEnv, got, chatSessionBudgetUnlimited)
	}
	for _, want := range []string{chatSessionBudgetEnv, chatSessionBudgetUnlimited, "unlimited"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the default was published without saying so: stderr %q does not name %q", stderr, want)
		}
	}
}

// TestChatBudgetHonoursAnExplicitValue is the constraint the operator stated in
// as many words: a default, never an enforcement. An explicitly exported budget
// is a decision about the operator's OWN surface, so the chat honours it
// verbatim — including values that are NOT unlimited — and does not touch it.
//
// "0" is in the list on purpose: a caller that already declared unlimited must
// not be re-declared, and an implementation keyed on the VALUE rather than on
// PRESENCE would print its default notice over a decision somebody had made.
func TestChatBudgetHonoursAnExplicitValue(t *testing.T) {
	for _, value := range []string{"20", "0", "1"} {
		t.Run("explicit="+value, func(t *testing.T) {
			defer testutil.SnapshotEnv(t, chatSessionBudgetEnv)()
			if err := os.Setenv(chatSessionBudgetEnv, value); err != nil { // env-leak-ok: testutil.SnapshotEnv restores this on test return, absence included — the flip is PER-CALL (call N's value must be gone before call N+1), so it is torn down by the defer this file pairs with every set //stylegate:ignore SnapshotEnv IS the explicit restore: the pair is torn down when THIS closure returns, which is the per-call scope the case is about, not at test cleanup
				t.Fatalf("set %s: %v", chatSessionBudgetEnv, err)
			}

			stderr := captureStderrStream(t, func() { publishChatSessionBudgetDefault(termToolStylesFor(lineedit.ColourOff)) })

			if got := os.Getenv(chatSessionBudgetEnv); got != value {
				t.Fatalf("%s = %q after a chat run, want the operator's %q untouched",
					chatSessionBudgetEnv, got, value)
			}
			if strings.Contains(stderr, "unset") {
				t.Errorf("a run with an explicit budget announced a default: stderr %q", stderr)
			}
		})
	}
}

// TestChatBudgetPublishedValueIsTheAgentsOwnSpelling pins the ONE join that
// makes the default real: cmd/agent and internal/agent must agree on BOTH the
// variable's name and unlimited's spelling, or the publication writes a value
// nobody reads. The name cannot be imported (cmd/agent is `package main` and
// the reader's constant is unexported), so this half is pinned by measurement
// rather than by the compiler — the same shape internal/config's
// ChatSurfaceIdentityEnv uses.
func TestChatBudgetPublishedValueIsTheAgentsOwnSpelling(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRootForChatBudgetTest(t), "internal", "agent", "loop.go"))
	if err != nil {
		t.Fatalf("read internal/agent/loop.go: %v", err)
	}
	text := string(src)

	// The NAME: the single reader's own constant (anchored, so a comment
	// mentioning the variable cannot satisfy the assertion).
	nameRe := regexp.MustCompile(`sessionBudgetEnv\s*=\s*"([A-Z0-9_]+)"`)
	m := nameRe.FindStringSubmatch(text)
	if m == nil {
		t.Fatal("internal/agent/loop.go no longer declares sessionBudgetEnv = \"…\" — the join cannot be checked")
	}
	if m[1] != chatSessionBudgetEnv {
		t.Errorf("the chat path publishes %q, internal/agent reads %q — the default would be written to a variable nobody reads",
			chatSessionBudgetEnv, m[1])
	}

	// The MEANING: the reader's own documented contract for the value published.
	if !strings.Contains(text, chatSessionBudgetEnv+"=0 means UNLIMITED") {
		t.Errorf("internal/agent no longer documents %s=0 as UNLIMITED — the published default's meaning is unverifiable",
			chatSessionBudgetEnv)
	}
	if !strings.Contains(text, "return 0 // 0 = unlimited") {
		t.Errorf("internal/agent's sessionBudget no longer returns 0 for unlimited — %q may no longer mean what this path publishes",
			chatSessionBudgetUnlimited)
	}

	// The CONTRAST the failure notice quotes must be the reader's own default.
	defRe := regexp.MustCompile(`sessionBudgetMinutes\s*=\s*(\d+)`)
	d := defRe.FindStringSubmatch(text)
	if d == nil {
		t.Fatal("internal/agent/loop.go no longer declares sessionBudgetMinutes = N")
	}
	if n, err := strconv.Atoi(d[1]); err != nil || n != chatSessionBudgetDefault {
		t.Errorf("the chat path's failure notice names a %d-minute default; internal/agent's is %s",
			chatSessionBudgetDefault, d[1])
	}
}

// TestChatOncePromptTeachesNoFabricatedCeiling is the SURFACE half, and it is
// the one the operator asked for: the REAL binary, `gopi chat --once --tools`,
// against the mock provider — the exact invocation that rendered
// `BUDGET: session budget 60min` before this change. It reads the sentence back
// off the request body the mock captured, so the assertion is about what the
// model was sent and not about a copy of the prompt in this test.
//
// testutil.MockEnv strips every ambient GOPI_* variable, so "the operator
// exported nothing" is the real precondition here rather than a claim.
func TestChatOncePromptTeachesNoFabricatedCeiling(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceReplyText})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once", "--tools", "reply with OK"}, "",
		testutil.MockEnv(t, home, llm.URL()), work)
	if code != 0 {
		t.Fatalf("gopi chat --once --tools exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}

	body := llm.RequestBody(1)
	if body == "" {
		t.Fatal("the mock captured no request — no turn ran, so the prompt was never observed")
	}
	if strings.Contains(body, chatBudgetFabricatedCeiling) {
		t.Errorf("the chat turn still teaches the fabricated ceiling: request body carries %q", chatBudgetFabricatedCeiling)
	}
	if strings.Contains(body, "session budget 60min") {
		t.Errorf("the chat turn still renders a 60-minute budget anywhere in the request: %s", budgetRuleLine(body))
	}
	if !strings.Contains(body, chatBudgetUnlimitedRule) {
		t.Errorf("the chat turn does not state the unlimited budget — the tick line's ceiling statement is missing: %s",
			budgetRuleLine(body))
	}
	// The DEFAULT is announced on stderr, and the announcement is what makes the
	// run's own output self-explanatory (Rule 15).
	if !strings.Contains(stderr, chatSessionBudgetEnv) {
		t.Errorf("the run published the default without naming %s on stderr:\n%s", chatSessionBudgetEnv, stderr)
	}
}

// TestChatOnceHonoursAnExplicitBudget is the same surface with the operator's
// own export in front of it: the value reaches the prompt UNCHANGED, which is
// what "a default, never an enforcement" means end to end. It is the companion
// of the seam test above — that one proves nothing is overwritten in THIS
// process, this one proves the surviving value is what the model reads.
func TestChatOnceHonoursAnExplicitBudget(t *testing.T) {
	const explicitMinutes = 20
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatOnceReplyText})
	defer llm.Close()

	env := append(testutil.MockEnv(t, home, llm.URL()),
		chatSessionBudgetEnv+"="+strconv.Itoa(explicitMinutes))
	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once", "--tools", "reply with OK"}, "", env, work)
	if code != 0 {
		t.Fatalf("gopi chat --once --tools exit = %d, want 0\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}

	body := llm.RequestBody(1)
	if body == "" {
		t.Fatal("the mock captured no request — no turn ran")
	}
	want := "session budget " + strconv.Itoa(explicitMinutes) + "min"
	if !strings.Contains(body, want) {
		t.Errorf("an explicit %s=%d did not reach the chat prompt (want %q): %s",
			chatSessionBudgetEnv, explicitMinutes, want, budgetRuleLine(body))
	}
	if strings.Contains(body, chatBudgetUnlimitedRule) {
		t.Errorf("the chat default OVERWROTE the operator's explicit budget: %s", budgetRuleLine(body))
	}
}

// TestChatHelpChangesNoBudget pins the PLACEMENT of the call in the launch
// path. `gopi chat -h` is a USAGE QUERY, and the P0-1 rule is that such a run
// changes nothing in the environment — so the publication sits BELOW the argv
// walk's returns and below the `--film` dispatch (a driver, not a chat run).
// The test reads the source for the ordering, which is a property no runtime
// assertion of one command line can establish.
func TestChatHelpChangesNoBudget(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRootForChatBudgetTest(t), "cmd", "agent", "cli_chat.go"))
	if err != nil {
		t.Fatalf("read cli_chat.go: %v", err)
	}
	text := string(src)

	call := strings.Index(text, "publishChatSessionBudgetDefault(")
	if call < 0 {
		t.Fatal("cli_chat.go no longer calls publishChatSessionBudgetDefault( — the chat default is unreachable")
	}
	// The answer point writes the page the SCOPE selected (`chatHelpPage`), not
	// one fixed literal, so this guard searches the ANSWER SHAPE rather than the
	// page's value: a stdout write of whatever page the scope chose. Searching
	// `chatUsage` would have gone silently dead the moment the verb gained a
	// second page — the failure mode this test exists to prevent, one level up.
	lastUsageReturn := strings.LastIndex(text[:call], "os.Stdout.WriteString(chatHelpPage(")
	if lastUsageReturn < 0 {
		t.Fatal("no usage return found before the publication — this gate is checking nothing")
	}
	if lastUsageReturn > call {
		t.Error("the publication sits ABOVE a usage return: `gopi chat -h` would change the environment")
	}
	if film := strings.Index(text, "if filmSet {"); film >= 0 {
		if film > call {
			t.Error("the publication sits above the --film dispatch: a film run that serves no turn would publish a budget")
		} else if seg := text[film:call]; !strings.Contains(seg, "return ") {
			t.Error("the --film block no longer returns above the publication — a film run may now fall through to a chat turn")
		}
	}
}

// repoRootForChatBudgetTest locates the module root from this test file's own
// directory (cmd/agent → the parent), so the source-reading assertions need no
// build tag and no assumed working directory.
func repoRootForChatBudgetTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Dir(filepath.Dir(wd)) // cmd/agent → cmd → module root
	if _, err := os.Stat(filepath.Join(root, "internal", "agent", "loop.go")); err != nil {
		t.Fatalf("resolved %q as the module root but internal/agent/loop.go is not there: %v", root, err)
	}
	return root
}

// budgetRuleLine returns the request body's BUDGET line for a failure message,
// so a red test shows the reader the sentence that failed instead of a
// multi-kilobyte prompt or a JSON body.
func budgetRuleLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if idx := strings.Index(line, "BUDGET:"); idx >= 0 {
			end := idx + 160
			if end > len(line) {
				end = len(line)
			}
			return line[idx:end]
		}
	}
	return "(no BUDGET line in the rendered prompt)"
}
