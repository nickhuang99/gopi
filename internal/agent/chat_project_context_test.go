package agent

// chat_project_context_test.go — THE regression guard for the chat-path project
// context (GOPI.md) injection.
//
// WHAT IT PINS. Both chat prompt paths — the plain one (Agent.Chat, no tools on
// the wire) and the tool-enabled one (Agent.ChatOps) — must send the project's
// loaded GOPI.md as the SECOND system block, exactly as a task run does
// (Context.ProjectContext → BuildClaudeRequest's projectContext parameter →
// Request.System[1]). Removing the injection (going back to the `""` argument at
// either request builder) turns this file red, which is its whole purpose.
//
// WHY IT ASSERTS ON THE REQUEST BODY and not on a helper's return value: the
// defect being closed is not "a string exists somewhere" but "the block never
// reached the wire" — the loader ran, the string sat on Context.ProjectContext,
// and BuildClaudeRequest dropped it because the caller passed "". Only the
// emitted request can tell those apart.
//
// THE COST OF THIS TEST IS ZERO API SPEND: the httptest server answers the SSE
// stream itself, so the assertions read the bytes the real request builder
// produced.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/deepseek"
)

// chatProjectContextMarker is the string the fixture tree's GOPI.md carries. It
// appears nowhere else, so "the request carries the marker" cannot be satisfied
// by any other part of the assembled prompt.
const chatProjectContextMarker = "PROJECT-RULES-MARKER-7788"

// chatProjectContextBody is the fixture file's whole content. The byte-for-byte
// comparison in TestChatProjectContextComesFromTheLoader needs a body with a
// trailing newline and more than one line (the shape a real GOPI.md has), not a
// bare token.
const chatProjectContextBody = "# Fixture project rules\n\n- " + chatProjectContextMarker + ": always wear a hat.\n"

// chatProjectContextFileNameSpelled is the fixture file's name, spelled here
// because the test writes the file itself (the production constant
// projectContextFileName is the reader's; a fixture that referenced it would
// not prove the reader reads the file it documents).
const chatProjectContextFileNameSpelled = "GOPI.md"

// systemBlocksForChat collects the system blocks of the request(s) a chat turn
// sends, guarded because the handler runs on the server's own goroutine.
type systemBlocksForChat struct {
	mu     sync.Mutex
	bodies []string
	blocks [][]string
}

func (c *systemBlocksForChat) record(t *testing.T, r *http.Request) {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}
	var req deepseek.ClaudeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return
	}
	blocks := make([]string, 0, len(req.System))
	for _, b := range req.System {
		blocks = append(blocks, b.Text)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, string(body))
	c.blocks = append(c.blocks, blocks)
}

// lastBlocks returns the system blocks of the LAST request seen.
func (c *systemBlocksForChat) lastBlocks() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.blocks) == 0 {
		return nil
	}
	return c.blocks[len(c.blocks)-1]
}

// chatProjectContextFixtureServer answers every request with a one-line SSE
// stream and records the request's system blocks.
func chatProjectContextFixtureServer(t *testing.T, rec *systemBlocksForChat) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"ok"}, "end_turn")
	}))
}

// chatProjectContextFixtureTree writes a GOPI.md carrying the marker into its
// own temp dir and returns that dir.
func chatProjectContextFixtureTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, chatProjectContextFileNameSpelled)
	if err := os.WriteFile(path, []byte(chatProjectContextBody), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", chatProjectContextFileNameSpelled, err)
	}
	return dir
}

// TestChatPlainPathCarriesProjectContext is the PLAIN-path regression guard:
// Agent.Chat must put the loaded project context on the wire as the second
// system block. Delete the injection and this fails with "the chat request
// carried no project context".
func TestChatPlainPathCarriesProjectContext(t *testing.T) {
	rec := &systemBlocksForChat{}
	server := chatProjectContextFixtureServer(t, rec)
	defer server.Close()

	ag := newTestAgent(t, server)
	ag.Ctx.ProjectContext = chatProjectContextMarker

	if _, err := ag.Chat("say hi", nil); err != nil {
		t.Fatalf("Chat() error: %v", err)
	}

	blocks := rec.lastBlocks()
	if len(blocks) != 2 {
		t.Fatalf("plain chat request carried %d system block(s), want 2 (the chat prompt + the project context): %q", len(blocks), blocks)
	}
	if !strings.Contains(blocks[1], chatProjectContextMarker) {
		t.Errorf("plain chat request carried no project context — system block 2 = %q, want it to contain %q", blocks[1], chatProjectContextMarker)
	}
	if strings.Contains(blocks[0], chatProjectContextMarker) {
		t.Errorf("project context rode inside the chat prompt block instead of its own system block: %q", blocks[0])
	}
}

// TestChatOpsPathCarriesProjectContext is the same guard for the tool-enabled
// path — the two are separate request builders, so one can regress alone.
func TestChatOpsPathCarriesProjectContext(t *testing.T) {
	rec := &systemBlocksForChat{}
	server := chatProjectContextFixtureServer(t, rec)
	defer server.Close()

	ag := chatOpsAgent(t, server)
	ag.Ctx.ProjectContext = chatProjectContextMarker

	if _, err := ag.ChatOps("say hi", nil); err != nil {
		t.Fatalf("ChatOps() error: %v", err)
	}

	blocks := rec.lastBlocks()
	if len(blocks) != 2 {
		t.Fatalf("tool-enabled chat request carried %d system block(s), want 2 (the tool-routing prompt + the project context): %q", len(blocks), blocks)
	}
	if !strings.Contains(blocks[1], chatProjectContextMarker) {
		t.Errorf("tool-enabled chat request carried no project context — system block 2 = %q, want it to contain %q", blocks[1], chatProjectContextMarker)
	}
	if !strings.Contains(blocks[0], chatContextHead) {
		t.Errorf("tool-enabled chat request lost its chat-surface block: %q", blocks[0])
	}
}

// TestChatPathsWithoutProjectContextAreUnchanged pins the OPTIONAL half: with
// nothing loaded (no GOPI.md anywhere above the cwd — the ordinary case on a
// machine that is not this project) both paths send exactly ONE system block,
// byte-identical to the prompt they composed before the injection existed.
func TestChatPathsWithoutProjectContextAreUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, ag *Agent) error
		base func(ag *Agent) string
	}{
		{
			name: "plain",
			run:  func(t *testing.T, ag *Agent) error { _, err := ag.Chat("say hi", nil); return err },
			base: func(ag *Agent) string { return chatSystemPromptServed(ag) },
		},
		{
			name: "tools",
			run:  func(t *testing.T, ag *Agent) error { _, err := ag.ChatOps("say hi", nil); return err },
			base: func(ag *Agent) string { return chatOpsSystemPrompt(ag) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &systemBlocksForChat{}
			server := chatProjectContextFixtureServer(t, rec)
			defer server.Close()

			ag := newTestAgent(t, server)
			if tc.name == "tools" {
				ag.SystemPrompt = assembleSystemPrompt()
			}
			ag.Ctx.ProjectContext = ""

			if err := tc.run(t, ag); err != nil {
				t.Fatalf("chat turn error: %v", err)
			}
			blocks := rec.lastBlocks()
			if len(blocks) != 1 {
				t.Fatalf("a chat with no project context sent %d system block(s), want exactly 1: %q", len(blocks), blocks)
			}
			if want := tc.base(ag); blocks[0] != want {
				t.Errorf("a chat with no project context changed its system prompt:\n got=%q\nwant=%q", blocks[0], want)
			}
		})
	}
}

// TestChatProjectContextComesFromTheLoader closes the loop with the REAL
// loader: the bytes LoadProjectContext returns for a project tree are the bytes
// the chat request carries. It is the join that a hand-set marker cannot prove —
// the loader is shared with the task path (Rule 35: one reader, no second one).
func TestChatProjectContextComesFromTheLoader(t *testing.T) {
	dir := chatProjectContextFixtureTree(t)
	t.Setenv(config.GopiDirEnv, t.TempDir()) // a profile with no rules file: this arm is about the REPOSITORY bytes
	loaded := LoadProjectContext(dir)
	// The two-level loader wraps the repository body in its heading (the merge's
	// fixed shape, epic system-level-GOPI.md); the CONTRACT this test guards is
	// that the fixture's bytes arrive verbatim INSIDE that block, and that the
	// block is the only level present.
	if want := repositoryHeading(chatProjectContextFileNameSpelled) + "\n\n" + strings.TrimSpace(chatProjectContextBody); loaded != want {
		t.Fatalf("LoadProjectContext(%s) = %q, want the fixture file's bytes under the repository heading (%q)", dir, loaded, want)
	}

	rec := &systemBlocksForChat{}
	server := chatProjectContextFixtureServer(t, rec)
	defer server.Close()

	ag := newTestAgent(t, server)
	ag.Ctx.ProjectContext = loaded

	if _, err := ag.Chat("say hi", nil); err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	blocks := rec.lastBlocks()
	if len(blocks) != 2 || blocks[1] != loaded {
		t.Fatalf("the chat request did not carry the loader's bytes as their own system block: blocks=%q loaded=%q", blocks, loaded)
	}
}

// TestChatProjectContextCostIsMeasured reports the injection's price, so the
// number in the task report is reproducible rather than remembered: the project
// rule file's bytes, and the token estimate the harness itself uses for it
// (EstimateTokens — the same chars-per-token rule the trim decision is made on).
// It reads this repository's own GOPI.md; a checkout without one skips (absence
// is not a failure — the file is OPTIONAL).
func TestChatProjectContextCostIsMeasured(t *testing.T) {
	path := filepath.Join("..", "..", chatProjectContextFileNameSpelled)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no %s at the repo root (%v) — nothing to measure", chatProjectContextFileNameSpelled, err)
	}
	block := deepseek.Message{Role: "system", Content: string(data)}
	t.Logf("project context cost: %d bytes (%d runes), ~%d tokens as its own system block",
		len(data), utf8.RuneCountInString(string(data)), EstimateTokens(block))
}
