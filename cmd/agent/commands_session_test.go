// commands_session_test.go — the two SESSION-DESCRIPTIVE words the shared table
// now owns (/info, /context), asserted through the REAL dispatch each carrier
// uses (execSlashCommand with a CommandContext) rather than by calling the Run
// closures directly. That distinction is the point: the defect this epic fixes is
// a word one carrier answered and the other refused, so a test that bypassed the
// dispatch would pass while both carriers stayed broken (Rule 31).
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// slashSessionFixture is a CommandSession with EVERY fact present, so a row that
// silently goes missing fails by name rather than by a substring that happens to
// appear elsewhere in the block.
func slashSessionFixture() CommandSession {
	return CommandSession{
		SessionID:   "11111111-2222-3333-4444-555555555555",
		SessionDir:  "/tmp/sessions/11111111-2222-3333-4444-555555555555",
		SessionFile: "/tmp/sessions/11111111-2222-3333-4444-555555555555/session.jsonl",
		Model:       "fixture-model",
		Dir:         "/tmp/ground",
		Repo:        "/tmp/repo",
		InRepo:      true,
		Node:        "fixture-node",
		ToolsOn:     true,
	}
}

// TestSlashInfoAnswersFromTheCarrierFacts is the portability half: /info through
// the SHARED dispatch prints the same block the REPL's own arm prints, composed
// from the facts the carrier supplied.
func TestSlashInfoAnswersFromTheCarrierFacts(t *testing.T) {
	cs := slashSessionFixture()
	out, err := execSlashCommand("/info", CommandContext{Session: cs})
	if err != nil {
		t.Fatalf("/info through the shared dispatch errored: %v", err)
	}
	// The block is the ONE renderer's output, byte for byte — not "a text that
	// mentions the same strings".
	if want := chatInfoBlock(slashSessionFields(cs)); out != want {
		t.Fatalf("/info is not chatInfoBlock's output:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
	// And the block actually carries the facts, so the identity above cannot
	// pass on two equally empty strings.
	for _, want := range []string{cs.SessionID, cs.SessionFile, cs.Model, cs.Dir, cs.Repo, cs.Node} {
		if !strings.Contains(out, want) {
			t.Errorf("/info block is missing %q:\n%s", want, out)
		}
	}
}

// TestSlashInfoRefusesACarrierWithNoSessionLog is Rule 15 applied to the seam: a
// carrier that supplied no facts is REFUSED with the reason, never handed a block
// of "(unknown)" rows that reads as "the command ran and found nothing".
func TestSlashInfoRefusesACarrierWithNoSessionLog(t *testing.T) {
	out, err := execSlashCommand("/info", CommandContext{})
	if err == nil {
		t.Fatalf("/info with no carrier facts must refuse, got output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "no session log") {
		t.Errorf("the refusal does not name the cause: %v", err)
	}
	if out != "" {
		t.Errorf("a refused command printed a block anyway:\n%s", out)
	}
}

// TestSlashContextExportsViaTheSharedEntry drives /context through the shared
// dispatch against a REAL raw logger — the same writer a live run uses — and
// asserts the artifact landed and the answer is the ONE line. This is the test
// the epic's "each new command gets a test" deliverable names for /context.
func TestSlashContextExportsViaTheSharedEntry(t *testing.T) {
	// TWO turns of ONE round each: the session's last TURN is 2, and so is its
	// last ROUND — a fixture where the two numbers happen to agree still proves
	// the name comes from the axis (address_test.go separates them).
	rl, sl := contextAxisFixture(t, 2, 1)
	defer func() { _ = rl.Close() }() //stylegate:ignore the raw logger is closed on the way out of this fixture; its records were already synced and read
	defer func() { _ = sl.Close() }() //stylegate:ignore the session logger is closed on the way out of this fixture; its records were already synced and read

	cs := slashSessionFixture()
	cs.RawLogPath = rl.Path()
	cs.SessionFile = sl.Path()
	cs.SessionDir = t.TempDir()

	out, err := execSlashCommand("/context", CommandContext{Session: cs})
	if err != nil {
		t.Fatalf("/context through the shared dispatch errored: %v", err)
	}
	if strings.Count(strings.TrimRight(out, "\n"), "\n") != 0 {
		t.Errorf("/context printed more than ONE line:\n%s", out)
	}
	if !strings.Contains(out, "context-turn2.md") {
		t.Errorf("/context does not name the artifact for the LAST TURN:\n%s", out)
	}
	if !strings.Contains(out, cs.SessionDir) {
		t.Errorf("/context's line does not name where it wrote:\n%s", out)
	}
}

// TestSlashContextRefusesANoLogCarrier is that command's honest degradation on the
// shared path: a carrier with no raw log has nothing to export, and says so
// instead of writing an empty file.
func TestSlashContextRefusesANoLogCarrier(t *testing.T) {
	_, err := execSlashCommand("/context", CommandContext{Session: CommandSession{SessionDir: t.TempDir()}})
	if err == nil {
		t.Fatal("/context with no raw log must refuse")
	}
	if !strings.Contains(err.Error(), "raw HTTP log") {
		t.Errorf("the refusal does not name the cause: %v", err)
	}
}

// TestSlashContextKeepsItsArgumentGrammar: the shared entry reads the REPL's OWN
// parser, so a spelling that is refused in the console is refused in the browser
// too (one grammar, two carriers).
func TestSlashContextKeepsItsArgumentGrammar(t *testing.T) {
	cs := slashSessionFixture()
	cs.SessionDir = t.TempDir()
	for _, bad := range []string{"/context 0", "/context --outt=/tmp/x", "/context 1 2"} {
		if _, err := execSlashCommand(bad, CommandContext{Session: cs}); err == nil {
			t.Errorf("%s was accepted — the shared entry invented a grammar of its own", bad)
		}
	}
	// The help word stays answered from the ONE usage block (the table's own
	// hard-help standard, applied to a command whose grammar is hand-parsed).
	out, err := execSlashCommand("/context help", CommandContext{Session: cs})
	if err != nil {
		t.Fatalf("/context help errored: %v", err)
	}
	if out != chatContextUsage {
		t.Errorf("/context help is not chatContextUsage:\n%s", out)
	}
}

// TestPortableSessionWordsAppearInHelp is deliverable ③: the two new words are in
// the table, so BOTH carriers' /help lists them automatically — asserted from the
// rendered page, never from the map (a word in the map that the renderer skips is
// exactly the drift this asserts against).
func TestPortableSessionWordsAppearInHelp(t *testing.T) {
	page := slashHelpText()
	for _, word := range []string{chatInfoCommandName, chatContextCommandName} {
		if !strings.Contains(page, "/"+word) {
			t.Errorf("/help does not list /%s:\n%s", word, page)
		}
		// The page's row IS the usage block's first line (one definition), so the
		// command is discoverable with the sentence that describes it.
		first := strings.SplitN(strings.TrimSpace(slashCommands[word].Usage), "\n", 2)[0]
		if !strings.Contains(page, first) {
			t.Errorf("/help's row for /%s is not its Usage summary %q:\n%s", word, first, page)
		}
	}
	// The retired pair stays out: the portability census must not reopen the
	// 2026-11-07 decision on the way past.
	for _, gone := range []string{"/code-search", "/memory"} {
		if strings.Contains(page, gone) {
			t.Errorf("/help re-advertises the retired %s:\n%s", gone, page)
		}
	}
}

// TestHelpListsEachPortableWordExactlyOnce is the duplicate-row guard, and it
// exists because the MEASURED run printed /info and /context TWICE: chatUsage
// listed them in the console's own "Slash commands" block AND slashHelpText()
// printed them from the table. Moving a word into the table without removing it
// from the hand-maintained block is a silent second owner — /help still reads as
// complete while the page carries the same command on two rows.
//
// The count is over the RENDERED PAGE, and it counts a row by the row's shape
// ("/word" followed by whitespace), so a sentence that happens to mention the
// word is not miscounted as a row.
func TestHelpListsEachPortableWordExactlyOnce(t *testing.T) {
	page := chatUsage + slashHelpText()
	for _, word := range []string{chatInfoCommandName, chatContextCommandName} {
		rows := 0
		for _, line := range strings.Split(page, "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == "/"+word {
				rows++
			}
		}
		if rows != 1 {
			t.Errorf("/help carries a row for /%s %d times, want exactly 1:\n%s", word, rows, page)
		}
	}
}

// TestPortableSessionWordsAreNotReplOnlyWords pins the OTHER half of the move:
// a word the shared table owns must not also be listed as REPL-native, or the
// completion menu would carry it twice and the /help answer would come from two
// sources.
func TestPortableSessionWordsAreNotReplOnlyWords(t *testing.T) {
	for _, word := range []string{chatInfoCommandName, chatContextCommandName} {
		if _, ok := slashCommands[word]; !ok {
			t.Errorf("/%s is missing from the shared table", word)
		}
		for _, native := range chatSessionWords {
			if native == word {
				t.Errorf("/%s is BOTH a table entry and a REPL-native word — two owners for one word", word)
			}
		}
	}
}

// contextAxisFixture writes ONE session through the SAME writers a live run
// uses — the raw HTTP log AND the session record — so an export test measures
// both axes a real session carries instead of a hand-built file.
//
// THE TURN BOUNDARIES ARE REAL: each turn opens with a `user` row written by
// LogUser (an operator row, the thing IsTurnOpeningRow answers "yes" to), so a
// change to that predicate moves the fixture and the production reader together
// — which is the property the axis fix depends on.
func contextAxisFixture(t *testing.T, turns, roundsPerTurn int) (rawLog *agent.RawLogger, sessionLog *agent.SessionLogger) {
	t.Helper()
	t.Setenv(rawLogDirEnv, t.TempDir())
	rl, err := agent.NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger: %v", err)
	}
	if rl == nil {
		t.Skipf("this run has the raw log disabled (%s=OFF)", chatContextTestRawLogEnv)
	}
	sl, serr := agent.NewSessionLogger("fixture", rl.Path())
	if serr != nil {
		t.Fatalf("NewSessionLogger: %v", serr)
	}
	write := rl.LogFunc()
	round := 0
	for turn := 1; turn <= turns; turn++ {
		if uerr := sl.LogUser(fmt.Sprintf("p%d", turn), fmt.Sprintf("ask %d", turn)); uerr != nil {
			t.Fatalf("LogUser(turn %d): %v", turn, uerr)
		}
		for i := 0; i < roundsPerTurn; i++ {
			round++
			if aerr := sl.LogAssistantText(fmt.Sprintf("answer %d", round)); aerr != nil {
				t.Fatalf("LogAssistantText(round %d): %v", round, aerr)
			}
			body, merr := json.Marshal(deepseek.ClaudeRequest{
				Model: "fixture",
				Messages: []deepseek.ClaudeMessage{{
					Role:    "user",
					Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: fmt.Sprintf("round-%d-body", round)}},
				}},
			})
			if merr != nil {
				t.Fatalf("marshal round %d: %v", round, merr)
			}
			write(rawlogfmt.TagRequest, "", body)
			if rerr := sl.LogRequest(round, 10, 1, 9, 2); rerr != nil {
				t.Fatalf("LogRequest(round %d): %v", round, rerr)
			}
		}
	}
	if serr := rl.Sync(); serr != nil {
		t.Fatalf("RawLogger.Sync: %v", serr)
	}
	if serr := sl.Sync(); serr != nil {
		t.Fatalf("SessionLogger.Sync: %v", serr)
	}
	return rl, sl
}

// newSlashContextRawLog writes n request records through the SAME logger a live
// run uses (so this fixture would catch a change to the log's framing), and
// returns it ready to read.
func newSlashContextRawLog(t *testing.T, n int) *agent.RawLogger {
	t.Helper()
	t.Setenv(rawLogDirEnv, t.TempDir())
	rl, err := agent.NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger: %v", err)
	}
	if rl == nil {
		t.Skipf("this run has the raw log disabled (%s=OFF)", chatContextTestRawLogEnv)
	}
	write := rl.LogFunc()
	for i := 1; i <= n; i++ {
		body, merr := json.Marshal(deepseek.ClaudeRequest{
			Model: "fixture",
			Messages: []deepseek.ClaudeMessage{{
				Role:    "user",
				Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: fmt.Sprintf("round-%d-body", i)}},
			}},
		})
		if merr != nil {
			t.Fatalf("marshal round %d: %v", i, merr)
		}
		write(rawlogfmt.TagRequest, "", body)
	}
	if serr := rl.Sync(); serr != nil {
		t.Fatalf("RawLogger.Sync: %v", serr)
	}
	return rl
}

// TestSlashInfoMatchesTheReplBlock is the property that makes the move safe: the
// browser's /info and the console's /info are ONE block, so an operator reading
// the two side by side cannot be shown two different sessions.
//
// It is asserted through the console's own assembler (replCommandContext over a
// live session logger) against the console arm's renderer, which is what the
// table's Run composes too — a divergence would have to be introduced in one of
// the two doors and would fail here.
func TestSlashInfoMatchesTheReplBlock(t *testing.T) {
	logger, err := agent.NewSessionLogger("", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer func() { _ = logger.Close() }() //stylegate:ignore the session logger is closed on the way out of the identity fixture; its records were already read

	sess := &chatSession{toolsEnabled: true}
	ag := &agent.Agent{Model: "fixture-model", SessionLog: logger}

	shared, err := execSlashCommand("/info", replCommandContext(ag, sess))
	if err != nil {
		t.Fatalf("/info through the shared dispatch (REPL context) errored: %v", err)
	}
	console := chatInfoBlock(chatInfoFieldsFor(ag, sess))
	if shared != console {
		t.Fatalf("the shared /info and the console's block are two renderings:\n--- shared ---\n%s\n--- console ---\n%s", shared, console)
	}
	if !strings.Contains(shared, logger.SessionID()) {
		t.Errorf("the shared /info lost the live session UUID:\n%s", shared)
	}
}
