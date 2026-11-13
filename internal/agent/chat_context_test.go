package agent

// chat_context_test.go — the chat surface is told its OWN context.
//
// The epic this file closes: the plain chat path sent a three-sentence persona
// that FORBADE mentioning tools or internals, the --tools path sent the task
// assembly (which believes it is a task runner), and NEITHER was told which
// host, node role or repo the process was running in. Two things must therefore
// hold, and each is asserted against a real surface rather than a copy of the
// text:
//
//  1. the block is PRESENT in the prompt each chat path actually sends
//     (chatSystemPromptServed / chatOpsSystemPrompt — the composition points the
//     request builders call, not a helper they were told about);
//  2. the runtime values IN it are the process's REAL ones — the hostname
//     os.Hostname returns, the node identity config.ResolveNodeRole resolves,
//     and the repo root git answers for the process's cwd.
//
// Rule 17: every env flip is t.Setenv (test-scoped), and the node-identity memo
// is reset through the seam internal/config exports for exactly this.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/config"
)

// chatContextTestRepo creates a real git repository and makes it the process
// cwd, returning its resolved top directory. A FIXTURE repo is used rather than
// the repo under test because that is the only way the assertion can DETECT a
// wrong answer: in the repo under test, a hard-coded path and the truth are the
// same string.
func chatContextTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Skipf("git init unavailable in this environment: %v: %s", err, out)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}
	t.Chdir(dir)
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel in the fixture repo: %v", err)
	}
	top := strings.TrimSpace(string(out))
	if top == "" {
		t.Fatal("the fixture repo resolved no top directory")
	}
	_ = resolved
	return top
}

// TestChatPromptCarriesChatContextBlock is the presence half, on the plain
// (no-tools) path: the prompt the request is built from names the surface, the
// place, and every verb of the self-observation loop.
func TestChatPromptCarriesChatContextBlock(t *testing.T) {
	repo := chatContextTestRepo(t)
	got := chatSystemPromptServed(nil)

	if !strings.Contains(got, chatContextHead) {
		t.Fatalf("the chat prompt carries no chat-context block (%q missing):\n%s", chatContextHead, got)
	}
	// WHERE — the three runtime fields, each with its label.
	for _, want := range []string{"Where: host ", "node ", "repo " + repo} {
		if !strings.Contains(got, want) {
			t.Errorf("chat prompt missing %q — the block must state where this process runs:\n%s", want, got)
		}
	}
	// WHAT — this is a console, not a task run.
	for _, want := range []string{"console", "ANSWER BODY is the artifact"} {
		if !strings.Contains(got, want) {
			t.Errorf("chat prompt missing %q — the block must state what this surface is", want)
		}
	}
	// HOW TO SEE MYSELF HERE — the loop, spelled through the leaf constants.
	arm := "`gopi " + cliverbs.Chat + " " + cliverbs.DumpScreen + "`"
	read := "`gopi " + cliverbs.UIState + " " + cliverbs.TargetFlag + " " + cliverbs.Chat + "`"
	compare := cliverbs.UIState + " " + cliverbs.Compare + " \"<line>\" " + cliverbs.SessionFlag
	for _, want := range []string{arm, read, compare, cliverbs.Log, cliverbs.Raw, cliverbs.Stats} {
		if !strings.Contains(got, want) {
			t.Errorf("chat prompt missing %q — the block must teach the self-observation loop", want)
		}
	}
	// (d) the operator's rule for this surface, and the honest no-tools limb.
	if !strings.Contains(got, "NO pixel snapshot") {
		t.Errorf("chat prompt missing the operator's no-pixel-snapshot rule for this surface")
	}
	if !strings.Contains(got, "carries no tools") {
		t.Errorf("chat prompt must state plainly that this path cannot run the check " +
			"(an instruction the agent cannot act on is worse than none)")
	}
	// The persona body is still there — immediately after the operator's
	// reply-language directive when one is configured, FIRST when none is. The
	// directive is prepended by the same helper the task path uses
	// (systemPromptWithOutputLanguage), so asserting "the prompt opens with the
	// persona body" would be false on every machine that configured a language:
	// the honest assertion is that the persona is what the directive introduces.
	if !strings.HasPrefix(got, systemPromptWithOutputLanguage(chatSystemPrompt)) {
		t.Fatalf("the chat prompt must open with the persona body (behind the operator's directive, if configured):\n%s", head(got))
	}
}

// TestChatContextRuntimeValuesAreTheProcesssOwn is the truth half: the values
// rendered are re-derived here from the process itself, independently of the
// rendering code. If the block hard-coded a host or a repo, this test would pass
// only on the machine it was written on — which is the defect it exists to
// catch.
func TestChatContextRuntimeValuesAreTheProcesssOwn(t *testing.T) {
	repo := chatContextTestRepo(t)
	t.Setenv(config.RoleEnv, string(config.NodeRoleServer))
	config.ResetNodeIdentityForTest()

	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skipf("os.Hostname unavailable (%v) — the display label degrades to %q by design", err, chatUnknownHost)
	}
	role, syncTo := config.ResolveNodeRole()
	if role != config.NodeRoleServer {
		t.Fatalf("precondition: the resolver returned role %q, want %q", role, config.NodeRoleServer)
	}
	wantNode := string(role) + "→" + syncTo
	if syncTo == "" {
		wantNode = string(role)
	}

	got := chatContextRules()
	if !strings.Contains(got, "host "+host) {
		t.Errorf("the block's host is not this process's os.Hostname (%q):\n%s", host, got)
	}
	if !strings.Contains(got, "node "+wantNode) {
		t.Errorf("the block's node is not what config.ResolveNodeRole reports (%q):\n%s", wantNode, got)
	}
	if !strings.Contains(got, "repo "+repo) {
		t.Errorf("the block's repo is not what git resolves for this cwd (%q):\n%s", repo, got)
	}

	// An UNDECLARED node is rendered explicitly, never dropped: a missing field
	// reads as an omission, and the block's whole point is that it never guesses.
	t.Setenv(config.RoleEnv, "")
	t.Setenv(config.SyncToEnv, "")
	t.Setenv("GOPI_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))
	config.ResetNodeIdentityForTest()
	if got := chatContextRules(); !strings.Contains(got, "node "+chatNodeUndeclared) {
		t.Errorf("an undeclared node must render as %q, not vanish:\n%s", chatNodeUndeclared, got)
	}
}

// TestChatPromptDropsBlockWhenSectionIsEmptied is the red proof: it removes the
// block's content the way a regression would (an empty section) and shows the
// prompt loses it — so the presence battery above is a real assertion about a
// removable thing, not a tautology about a constant.
func TestChatPromptDropsBlockWhenSectionIsEmptied(t *testing.T) {
	repo := chatContextTestRepo(t)
	_ = repo
	if !strings.Contains(chatSystemPromptServed(nil), chatContextHead) {
		t.Fatal("precondition: the block must be present before it is removed")
	}
	orig := chatContextSection
	chatContextSection = PromptSection{Name: chatContextSectionName, Order: chatContextSectionOrder}
	t.Cleanup(func() { chatContextSection = orig })

	got := chatSystemPromptServed(nil)
	if strings.Contains(got, chatContextHead) {
		t.Fatalf("an emptied chat-context section still appears in the prompt:\n%s", got)
	}
	// The comparison target carries the record contract, and that is NOT residue
	// of the emptied section: the contract is appended by the assembly's own
	// tail (maybeAppendChatSelfRecordRule) whether or not the block rendered, so
	// the residue this test is about is the SEPARATOR an emptied block could
	// leave behind — asserted directly below, where a stray join shows up as a
	// blank line between two sections.
	want := maybeAppendChatSelfRecordRule(systemPromptWithOutputLanguage(chatSystemPrompt))
	if got != want {
		t.Fatalf("an emptied section must leave no residue (no stray separator):\n got=%q\nwant=%q", got, want)
	}
	if strings.Contains(strings.TrimSuffix(got, chatSelfRecordRule), promptSectionSeparator+promptSectionSeparator) {
		t.Fatalf("an emptied section left a stray separator behind:\n%q", got)
	}
}

// TestTaskPromptCarriesNoChatContext is the SCOPE half in the other direction: a
// task run must not be told it is a chat console. The two paths share the BLOCK
// and its runtime values; they do not share the assembly.
func TestTaskPromptCarriesNoChatContext(t *testing.T) {
	if p := assembleSystemPrompt(); strings.Contains(p, chatContextHead) {
		t.Fatalf("the TASK prompt carries the chat-surface block:\n%s", p)
	}
}

// TestChatOpsPromptCarriesChatContextBlock covers the --tools path: it sends
// the full tool-routing prompt PLUS the chat block, and it is the path where the
// block's self-check is actually runnable (bash + the CLI verbs are on the wire).
func TestChatOpsPromptCarriesChatContextBlock(t *testing.T) {
	chatContextTestRepo(t)
	base := assembleSystemPrompt()
	got := chatOpsSystemPrompt(&Agent{SystemPrompt: base})

	if !strings.HasPrefix(got, base+promptSectionSeparator) {
		t.Fatalf("the tool-enabled chat prompt must keep the tool-routing prompt first:\n%s", head(got))
	}
	if !strings.Contains(got, chatContextHead) {
		t.Fatalf("the tool-enabled chat prompt carries no chat-context block:\n%s", got)
	}
	if !strings.Contains(got, chatToolsLimb) {
		t.Errorf("the tool-enabled chat prompt must say the self-check IS runnable here (bash + the CLI verbs are on the wire), got:\n%s", got)
	}
	if strings.Contains(got, chatNoToolsLimb) {
		t.Errorf("the tool-enabled chat prompt carries the NO-TOOLS limb — a false instruction: bash is on its wire")
	}
	// A base-less Agent (a hand-built one) still gets a self-aware chat, and the
	// block is never emitted behind a dangling separator.
	if solo := chatOpsSystemPrompt(nil); !strings.HasPrefix(solo, chatContextHead) {
		t.Fatalf("an empty base must yield the block alone, got %q", head(solo))
	}
}

// TestChatContextFlagsComeFromLeafConstants is the Rule 1/13/14 guard on the
// invocation vocabulary: every flag the block renders is the leaf constant's own
// value, so a rename on the dispatch side cannot leave the prompt teaching a
// spelling the CLI rejects. The negative half — that the block composes them
// rather than typing them — is what the composed substrings below assert.
func TestChatContextFlagsComeFromLeafConstants(t *testing.T) {
	for _, c := range []struct{ name, value string }{
		{"DumpScreen", cliverbs.DumpScreen},
		{"TargetFlag", cliverbs.TargetFlag},
		{"Compare", cliverbs.Compare},
		{"SessionFlag", cliverbs.SessionFlag},
	} {
		if strings.TrimSpace(c.value) == "" {
			t.Fatalf("cliverbs.%s is empty — the prompt would render a bare flag", c.name)
		}
	}
	rules := chatContextRules()
	for _, want := range []string{
		cliverbs.Chat + " " + cliverbs.DumpScreen,
		cliverbs.UIState + " " + cliverbs.TargetFlag + " " + cliverbs.Chat,
		cliverbs.UIState + " " + cliverbs.Compare + " \"<line>\" " + cliverbs.SessionFlag,
	} {
		if !strings.Contains(rules, want) {
			t.Errorf("the block does not render the invocation %q built from the leaf constants:\n%s", want, rules)
		}
	}
}
