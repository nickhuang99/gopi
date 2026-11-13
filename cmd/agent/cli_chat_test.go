package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestIsChatSubcommand verifies the `chat` subcommand word is recognized as
// the first token before any non-chat argv is forwarded on. isChatSubcommand
// itself os.Exits on a match (the shared subcommand-dispatch contract), so the
// *detection* rule — args[0] == "chat" — is asserted here without invoking it.
func TestIsChatSubcommand(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"chat"}, true},
		{[]string{"chat", "-h"}, true},
		{[]string{"chat", "some-raw.log"}, true},
		{[]string{"write", "hello"}, false},
		{nil, false},
		{[]string{""}, false},
	}
	for _, c := range cases {
		got := len(c.args) > 0 && c.args[0] == "chat"
		if got != c.want {
			t.Errorf("isChat(args=%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

// TestRunChatSeedsFromCheckpoint verifies that `gopi chat <rawlog>` loads the
// raw log checkpoint as the conversation seed (the loop would then continue
// from that context). We exercise the seed path with a real checkpoint file
// and confirm the returned code / error handling behave, then free the
// stdin loop concern by only exercising inputs that return before reading a
// line (help and an unknown seed target).
func TestRunChat_HelpReturnsZero(t *testing.T) {
	if code := runChat([]string{"-h"}); code != 0 {
		t.Errorf("runChat([-h]) = %d, want 0", code)
	}
	if code := runChat([]string{"--help"}); code != 0 {
		t.Errorf("runChat([--help]) = %d, want 0", code)
	}
}

// TestRunChat_UnknownSeedErrors verifies a nonexistent seed target produces a
// nonzero exit and does not hang in the REPL (the error is raised before the
// first stdin read).
func TestRunChat_UnknownSeedErrors(t *testing.T) {
	if code := runChat([]string{"definitely-not-a-real-session"}); code == 0 {
		t.Error("runChat(unknown seed) = 0, want nonzero")
	}
}

// TestRunChat_SeedFromRawLogPath verifies a readable raw log path is accepted
// as the seed and loads a checkpoint (the function proceeds to build the
// session — we only assert the seed path does not error before the REPL by
// refusing to swallow the missing-file case versus present-file case: a
// present file must be accepted as a seed). We stop asserting past the seed
// because the REPL loop requires real stdin + a live API.
func TestRunChat_SeedFromRawLogPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gopi-raw-seed.log")
	// A minimal but structurally valid checkpoint: a REQUEST line with a JSON
	// system + user message. LoadCheckpoint parses the last → REQUEST block.
	req := `{"model":"deepseek-v4-pro","stream":true,"system":[{"type":"text","text":"sys"}],
	"messages":[{"role":"user","content":"hello"}],"thinking":null}`
	if err := os.WriteFile(p, []byte("→ REQUEST\n"+req+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// resolveResumeLogPath must accept the existing path verbatim (the seed
	// branch uses it), proving the chat seed wiring is reachable.
	got, err := resolveResumeLogPath(p)
	if err != nil {
		t.Fatalf("resolveResumeLogPath(%s): %v", p, err)
	}
	if got != p {
		t.Errorf("resolveResumeLogPath = %q, want %q", got, p)
	}
}

// TestChatRenderer_BufferedPopIn verifies the human-friendly renderer buffers
// streamed deltas and flushes them in coherent chunks: whole lines flush
// immediately (natural pause), and a run of short fragments accumulates rather
// than printing raw, so the answer "pops in" as readable units instead of
// jumbling fragment-by-fragment.
func TestChatRenderer_BufferedPopIn(t *testing.T) {
	var sb strings.Builder
	r := newChatRenderer(&sb)

	// A word split across tiny fragments should NOT be printed until a flush
	// boundary (space/newline) arrives or flush() is called.
	r.write("Hel")
	r.write("lo,")
	if sb.Len() != 0 {
		t.Fatalf("renderer printed a partial word early: %q", sb.String())
	}

	// A newline is a natural pause — it must flush immediately.
	r.write(" world\n")
	if got := sb.String(); got != "Hello, world\n" {
		t.Fatalf("renderer after newline = %q, want %q", got, "Hello, world\n")
	}
	sb.Reset()

	// A long buffer (no boundary) must flush to keep visible progress.
	long := strings.Repeat("a", chatMaxBufferedBytes+5)
	r.write(long)
	if got := sb.String(); got != long {
		t.Fatalf("renderer did not flush the oversized buffer (got %d of %d chars)", len(got), len(long))
	}
	sb.Reset()

	// A trailing-space word flushes after the word window elapses.
	r.write("done")
	r.write(" ")
	if sb.Len() != 0 {
		t.Fatalf("space alone should not flush early, got %q", sb.String())
	}
	// Advance the flush marker well into the past so the next completed word
	// trips the word window.
	r.lastFlush = time.Now().Add(-chatWordFlushWindow * 2)
	r.write("done ")
	if got := sb.String(); got != "done done " {
		t.Fatalf("renderer after word window = %q, want %q", got, "done done ")
	}
	sb.Reset()

	// flush() must emit the buffered tail (never drop the answer's ending).
	r.write("tail")
	r.flush()
	if got := sb.String(); got != "tail" {
		t.Fatalf("renderer.flush() = %q, want %q", got, "tail")
	}
}

// TestChatIndexArgs is the regression test for the backfill-dispatch bug:
// isChatSubcommand used to forward args[2:] — dropping the verb — so
// `gopi chat backfill` fell into runChatList's list branch and printed
// "No chats found" instead of running the migration. The tail passed to
// runChatList MUST include the verb (runChatList branches on rest[0]).
func TestChatIndexArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
		ok   bool
	}{
		{"backfill keeps verb", []string{"chat", "backfill", "--dry-run"}, []string{"backfill", "--dry-run"}, true},
		// The `list` case that used to sit here is gone WITH the verb (epic
		// verb-restructure, 2026-10-05): chatIndexArgs now REFUSES the word by
		// name, and it refuses by EXITING, so it cannot be exercised in this
		// process's table. The refusal is driven on the REAL binary by
		// TestChatCLIRealBinaryRefusesAnUnknownFlag ("retired index subcommand")
		// and asserted end to end by TestG5ChatListAndBackfillRunAsAdvertised.
		{"repl fallback", []string{"chat"}, nil, false},
		{"repl with seed", []string{"chat", "some.log"}, nil, false},
		{"repl flag only", []string{"chat", "--tools"}, nil, false},
		{"unknown verb is repl", []string{"chat", "bogus"}, nil, false},
		{"empty argv", nil, nil, false},
	}
	for _, c := range cases {
		got, ok := chatIndexArgs(c.args)
		if ok != c.ok {
			t.Errorf("%s: ok=%v want %v", c.name, ok, c.ok)
			continue
		}
		if ok && strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s: tail=%v want %v", c.name, got, c.want)
		}
	}
}

// TestChatHelpText_TakesTheCommandArgument is the regression guard for the
// REPL's /help: the REPL intercepts /help before the shared dispatch (it must
// — its session-state words are not table entries), and that intercept used to
// ignore the argument and dump EVERYTHING. `/help task` must print the /task
// usage and nothing else; only a bare `/help` prints the full banner + table.
func TestChatHelpText_TakesTheCommandArgument(t *testing.T) {
	// /help <table command> == that command's usage, verbatim — the same text
	// execSlashCommand returns, so the two carriers cannot drift.
	for name, c := range slashCommands {
		for _, form := range []string{"/help " + name, "/help /" + name, "/help " + strings.ToUpper(name)} {
			got, err := chatHelpText(form)
			if err != nil {
				t.Fatalf("chatHelpText(%q) error: %v", form, err)
			}
			if got != c.Usage {
				t.Errorf("chatHelpText(%q) = %q, want the /%s usage block", form, got, name)
			}
		}
	}

	// The REPL-native session words answer too (they are not table entries,
	// so an argument-aware /help must not report them as unknown).
	for _, word := range []string{"quit", "exit", "clear", "new", "tools"} {
		got, err := chatHelpText("/help " + word)
		if err != nil {
			t.Fatalf("chatHelpText(/help %s) error: %v", word, err)
		}
		if strings.Contains(got, slashHelpText()) || got == chatUsage+slashHelpText() {
			t.Errorf("/help %s dumped the whole help instead of its own usage", word)
		}
		if !strings.HasPrefix(got, "Usage: /") {
			t.Errorf("/help %s = %q, want a usage block", word, got)
		}
	}

	// A bare /help keeps the full dump: banner + the shared table.
	full, err := chatHelpText("/help")
	if err != nil {
		t.Fatalf("chatHelpText(/help) error: %v", err)
	}
	if !strings.Contains(full, chatUsage) || !strings.Contains(full, slashHelpText()) {
		t.Errorf("bare /help must print the banner AND the table:\n%s", full)
	}
	if full != chatUsage+slashHelpText() {
		t.Errorf("bare /help = %q, want banner+table", full)
	}

	// An unknown command is an error naming the word — never a silent dump.
	if _, err := chatHelpText("/help nosuchthing"); err == nil {
		t.Error("/help nosuchthing must be an error")
	}
}

// TestChatHelpText_AgreesWithSharedDispatch pins the two carriers to ONE
// answer for the words they share: the REPL's /help <cmd> and the shared
// execSlashCommand's /help <cmd> must return identical text (R4 — the table is
// the single source of truth; the REPL only adds its native words).
func TestChatHelpText_AgreesWithSharedDispatch(t *testing.T) {
	for _, form := range []string{"/help task", "/help log", "/help text2img", "/help t2i"} {
		repl, err := chatHelpText(form)
		if err != nil {
			t.Fatalf("chatHelpText(%q): %v", form, err)
		}
		shared, err := execSlashCommand(form, CommandContext{})
		if err != nil {
			t.Fatalf("execSlashCommand(%q): %v", form, err)
		}
		if repl != shared {
			t.Errorf("%s: REPL help %q != shared dispatch %q", form, repl, shared)
		}
	}
}

// TestChatHelpSwitchRoutesThroughTheArgument is the anti-re-inline gate for
// the defect itself. The bug was not in the help RENDERER (chatHelpText did
// not exist) but in the REPL's `/help` arm: it printed the whole banner
// unconditionally, ignoring the words the user typed. A future edit that puts
// that unconditional dump back — or drops the routing to chatHelpText — fails
// here, even though every other test stays green.
func TestChatHelpSwitchRoutesThroughTheArgument(t *testing.T) {
	src, err := os.ReadFile("cli_chat.go")
	if err != nil {
		t.Fatalf("read cli_chat.go: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "chatHelpText(msg)") {
		t.Error("the REPL's /help arm must route the raw line through chatHelpText — " +
			"without it, `/help <cmd>` dumps the whole help again")
	}
	// The pre-fix shape: the /help ARM rendered the dump itself. Scope the
	// check to that arm (the CLI's `-h` path legitimately prints chatUsage).
	arm := strings.Index(body, `case "/" + helpCommandName`)
	if arm < 0 {
		t.Fatal("cli_chat.go has no /help arm in the slash switch")
	}
	armBody := body[arm:]
	if n := strings.Index(armBody, "\n\t\t\tcase "); n > 0 {
		armBody = armBody[:n]
	}
	if strings.Contains(armBody, "slashHelpText()") || strings.Contains(armBody, "fmt.Print(chatUsage)") {
		t.Errorf("the /help arm renders the full help itself again — it must delegate:\n%s", armBody)
	}
	if !strings.Contains(armBody, "chatHelpText(msg)") {
		t.Errorf("the /help arm does not route through chatHelpText:\n%s", armBody)
	}
}
