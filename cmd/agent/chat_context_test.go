// chat_context_test.go — the /context argument parser, its ONE-usage rule, and
// the export arm driven end to end through the real raw logger.
//
// WHY THIS FILE EXISTS SEPARATELY FROM chat_screen_test.go: the two commands
// share a parser SHAPE on purpose (a bare turn number, one at most, unknown
// flags refused by name, a help word), and the epic required that shape be
// followed rather than a second style invented. A future edit that "improves"
// one parser and not the other is exactly the drift these tests catch.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/ctxdump"
)

// chatContextTestRawLogEnv names the switch that disables the raw log. It is
// spelled here rather than imported because the constant lives in
// internal/agent (rawLogEnableEnv), which this package may not reach — and a
// test that guessed would silently stop skipping on a host whose raw log is off.
const chatContextTestRawLogEnv = "GOPI_RAW_LOG"

// TestContextSlashRefusesEveryShapeItDoesNotTake pins Rule 15 on the parser: a
// caller who typed a flag or a digit this command does not accept believes it
// was understood, so every shape is refused BY NAME.
func TestContextSlashRefusesEveryShapeItDoesNotTake(t *testing.T) {
	cases := []struct {
		label string
		args  []string
		want  string
	}{
		{"a non-numeric turn", []string{"58x"}, "58x"},
		{"a negative turn", []string{"-1"}, "-1"},
		{"turn zero", []string{"0"}, "0"},
		{"an unknown flag", []string{"--outt=x"}, "--outt=x"},
		{"--out with no value", []string{"--out"}, chatContextOutFlag},
		{"an empty --out value", []string{"--out="}, chatContextOutFlag},
	}
	for _, c := range cases {
		_, err := parseChatContextArgs(c.args)
		if err == nil {
			t.Errorf("%s (%v) was ACCEPTED — a caller who typed it believes it was understood", c.label, c.args)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: the refusal %q does not name %q — the caller cannot see what to fix", c.label, err, c.want)
		}
	}
	// TWO turn numbers is a contradiction, not a "take the first".
	_, err := parseChatContextArgs([]string{"58", "59"})
	if err == nil {
		t.Errorf("two turn numbers were accepted — /context takes ONE turn")
	} else if !strings.Contains(err.Error(), "58") || !strings.Contains(err.Error(), "59") {
		t.Errorf("the two-turn refusal %q does not name both numbers", err)
	}
}

// TestContextSlashAcceptsTheDocumentedShapes pins the shapes the usage page
// promises, so the page and the parser cannot drift apart.
func TestContextSlashAcceptsTheDocumentedShapes(t *testing.T) {
	// The bare form = the LAST request (0 means "newest" inside the renderer).
	bare, err := parseChatContextArgs(nil)
	if err != nil || bare.Turn != 0 || bare.Path != "" {
		t.Errorf("the bare form did not parse to the last request: %+v (%v)", bare, err)
	}
	// A turn number, alone and with the two --out spellings.
	got, err := parseChatContextArgs([]string{"58"})
	if err != nil || got.Turn != 58 {
		t.Errorf("/context 58 did not parse to turn 58: %+v (%v)", got, err)
	}
	got, err = parseChatContextArgs([]string{"58", "--out", "/x/y.md"})
	if err != nil || got.Turn != 58 || got.Path != "/x/y.md" {
		t.Errorf("/context 58 --out PATH did not parse: %+v (%v)", got, err)
	}
	got, err = parseChatContextArgs([]string{"58", "--out=/x/y.md"})
	if err != nil || got.Path != "/x/y.md" {
		t.Errorf("/context 58 --out=PATH did not parse: %+v (%v)", got, err)
	}
	// Every help spelling asks for the usage block.
	for _, word := range []string{chatContextHelpWord, chatContextHelpFlagShort, chatContextHelpFlagLong} {
		got, err := parseChatContextArgs([]string{word})
		if err != nil || !got.Help {
			t.Errorf("/context %s did not ask for the usage block: %+v (%v)", word, got, err)
		}
	}
}

// TestContextHasONEUsageDefinition pins the single-definition rule (Rule 14):
// three doors answer /help context, and they must all answer from the one var —
// otherwise a rename leaves one of them documenting a spelling the switch no
// longer accepts.
func TestContextHasONEUsageDefinition(t *testing.T) {
	if usage, ok := chatNativeUsage(chatContextCommandName); !ok || usage != chatContextUsage {
		t.Errorf("/help context does not answer from chatContextUsage (ok=%v)", ok)
	}
	if usage, ok := chatReplSlashVocabulary(chatContextCommandName); !ok || usage != chatContextUsage {
		t.Errorf("the refusal vocabulary does not answer /context from chatContextUsage (ok=%v)", ok)
	}
	// THE OWNER MOVED ON 2026-11-08 (epic 01a1b818-e02f): /context is a TABLE word
	// now — the table can see the raw log and the session dir through
	// CommandContext.Session, so the browser can answer it too. What this asserts is
	// the invariant that survived the move: the REPL still KNOWS the word, so the
	// completion menu offers it and the refusal names it. A word nobody knows still
	// fails here, which is the regression this guards.
	if !chatReplWordKnown(chatContextCommandName) {
		t.Errorf("the REPL does not know %q — the completion menu and the refusal would omit a command the switch answers", chatContextCommandName)
	}
	// The page's promise about the destination is the CONSTANT the export writes
	// with, so it cannot advertise a name format the code does not produce.
	if !strings.Contains(chatContextUsage, ctxdump.FileNamePattern) {
		t.Errorf("the usage page does not name the artifact's real name pattern (%q)", ctxdump.FileNamePattern)
	}
	// And the ONE line it promises is the renderer's own sentence.
	if !strings.Contains(chatContextUsage, "context written to") {
		t.Errorf("the usage page does not show the one-line report the command prints")
	}
}

// TestSessionContextIsDispatchedAndDocumented pins the CLI half: the sub-verb is
// in the group's vocabulary (so tab completion and the unknown-subcommand
// refusal name it), the family page documents it, and the parser refuses a
// turn number of zero (a position cannot be zero).
func TestSessionContextIsDispatchedAndDocumented(t *testing.T) {
	if !containsVerb(sessionSubcommandNames, sessionSubcmdContext) {
		t.Fatalf("sessionSubcommandNames does not carry %q — `gopi session context` would be refused as unknown", sessionSubcmdContext)
	}
	if !strings.Contains(sessionUsage, "gopi session "+sessionSubcmdContext) {
		t.Errorf("the family page does not document `gopi session %s`", sessionSubcmdContext)
	}
	if !strings.Contains(sessionContextUsage, "gopi session "+sessionSubcmdContext) {
		t.Errorf("the sub-verb's own page does not open with its own invocation")
	}
	for _, bad := range []string{"0", "-3", "3x", "", "  "} {
		if n, ok := parseSessionContextTurn(bad); ok {
			t.Errorf("parseSessionContextTurn(%q) accepted it as turn %d — a turn is 1 or greater", bad, n)
		}
	}
	for in, want := range map[string]int{"1": 1, "58": 58, " 7 ": 7} {
		if n, ok := parseSessionContextTurn(in); !ok || n != want {
			t.Errorf("parseSessionContextTurn(%q) = %d,%v want %d,true", in, n, ok, want)
		}
	}
}

// TestContextSlashWritesTheArtifactAndPrintsOneLine is the epic's headline
// acceptance for the CHAT arm: the export exists, its name carries the TURN it
// HOLDS, and the console gets exactly one line.
//
// THE FIXTURE RUNS THROUGH THE REAL WRITERS — the raw logger AND the session
// logger — because a turn-scoped export reads BOTH: turn boundaries live in the
// session record, request bodies in the raw log. A fixture with only the raw log
// can express a ROUND and nothing else, which is exactly the confusion this
// epic removes.
//
// THE TWO AXES ARE SEPARATED (Rule 10.4): 3 turns, 2 rounds each. The last turn
// is 3 while the last round is 6, so a name that reads "turn 6" is caught here
// rather than on a real session.
func TestContextSlashWritesTheArtifactAndPrintsOneLine(t *testing.T) {
	const turns = 3
	rl, sl := contextAxisFixture(t, turns, 2)
	defer func() { _ = rl.Close() }() //stylegate:ignore the raw logger is closed on the way out of this fixture; its records were already synced and read
	defer func() { _ = sl.Close() }() //stylegate:ignore the session logger is closed on the way out of this fixture; its records were already synced and read

	sessDir := t.TempDir()
	sess := &chatSession{sessionDir: sessDir}
	ag := &agent.Agent{RawLog: rl, SessionLog: sl}
	var console bytes.Buffer
	in := &chatInput{errOut: &console}

	// The bare form: the LAST TURN, and the artifact is named for it —
	// context-turn3.md, never context-turn0.md and never context-turn6.md.
	chatContextHandleLine(in, ag, sess, "/context")
	notice := console.String()
	if !strings.Contains(notice, "context-turn3.md") {
		t.Fatalf("the notice does not name the artifact for turn %d: %q", turns, notice)
	}
	if strings.Contains(notice, "context-turn6.md") {
		t.Fatalf("the artifact is named for the ROUND ordinal (6), not the turn: %q", notice)
	}
	if got := strings.Count(strings.TrimRight(notice, "\n"), "\n"); got != 0 {
		t.Errorf("the console got %d lines, want 1: %q", got+1, notice)
	}
	if strings.Contains(notice, "round-6-body") {
		t.Errorf("the console carries the page's BODY — that is the one thing /context must not do")
	}
	path := filepath.Join(sessDir, fmt.Sprintf(ctxdump.FileNameFormat, turns))
	page, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("the artifact the notice named does not exist: %v", rerr)
	}
	if !strings.Contains(string(page), "round-6-body") {
		t.Errorf("the artifact named for turn %d does not hold that turn's last round (取错段比取不到更糟)", turns)
	}
	if !strings.Contains(notice, fmt.Sprintf("%d bytes", len(page))) {
		t.Errorf("the one line does not state the artifact's real size (%d bytes): %q", len(page), notice)
	}

	// An explicit turn selects what it says, and --out overrides the destination.
	console.Reset()
	dest := filepath.Join(t.TempDir(), "mine.md")
	chatContextHandleLine(in, ag, sess, "/context 1 --out "+dest)
	if _, serr := os.Stat(dest); serr != nil {
		t.Fatalf("--out did not write %q: %v", dest, serr)
	}
	first, ferr := os.ReadFile(dest)
	if ferr != nil {
		t.Fatalf("read back %q: %v", dest, ferr)
	}
	if !strings.Contains(string(first), "round-2-body") || strings.Contains(string(first), "round-6-body") {
		t.Errorf("--out wrote a different turn than the one named")
	}

	// --round is the OTHER axis, and it keeps its own word in the artifact's name.
	console.Reset()
	chatContextHandleLine(in, ag, sess, "/context --round 6")
	rnotice := console.String()
	if !strings.Contains(rnotice, fmt.Sprintf(ctxdump.RoundFileNameFormat, 6)) {
		t.Errorf("/context --round 6 did not name a ROUND-scoped artifact: %q", rnotice)
	}
	if strings.Contains(rnotice, fmt.Sprintf(ctxdump.FileNameFormat, 6)) {
		t.Errorf("/context --round 6 named the artifact as a TURN: %q", rnotice)
	}
	rpage, rerr := os.ReadFile(filepath.Join(sessDir, fmt.Sprintf(ctxdump.RoundFileNameFormat, 6)))
	if rerr != nil {
		t.Fatalf("the round-scoped artifact does not exist: %v", rerr)
	}
	if !strings.Contains(string(rpage), "round-6-body") {
		t.Errorf("the round-scoped artifact does not hold round 6")
	}

	// A turn the session does not carry is REFUSED, never answered with a neighbour.
	console.Reset()
	chatContextHandleLine(in, ag, sess, "/context 99")
	if !strings.Contains(console.String(), "99") || !strings.Contains(console.String(), "3 turn(s)") {
		t.Errorf("the out-of-range refusal does not name the turn and the session's turn count: %q", console.String())
	}
}

// TestContextSlashWithoutARawLogSaysWhy pins the honest degradation: a run whose
// raw log is off has nothing to export, and that is STATED rather than answered
// with an empty file.
func TestContextSlashWithoutARawLogSaysWhy(t *testing.T) {
	var console bytes.Buffer
	in := &chatInput{errOut: &console}
	chatContextHandleLine(in, &agent.Agent{}, &chatSession{sessionDir: t.TempDir()}, "/context")
	if !strings.Contains(console.String(), "raw HTTP log") {
		t.Errorf("a run with no raw log got %q — the reason is not stated", console.String())
	}
	if len(strings.Split(strings.TrimRight(console.String(), "\n"), "\n")) != 1 {
		t.Errorf("the degradation notice is more than one line: %q", console.String())
	}
}

// ---- the SECTION selectors (--system / --user) ----

// TestContextSlashParsesTheSectionSelectors pins the grammar half: each selector
// resolves through ctxdump.SectionFor (the ONE place the mapping lives), no
// selector is the full page, and the PAIR is refused by naming both words.
func TestContextSlashParsesTheSectionSelectors(t *testing.T) {
	bare, err := parseChatContextArgs(nil)
	if err != nil {
		t.Fatalf("the bare form: %v", err)
	}
	if !bare.Section.IsFull() {
		t.Errorf("the bare form resolved to section %q — with no selector /context must export the whole request", bare.Section)
	}
	for _, c := range []struct {
		args []string
		want ctxdump.Section
	}{
		{[]string{ctxdump.SystemSelector}, ctxdump.SectionSystem},
		{[]string{ctxdump.UserSelector}, ctxdump.SectionUser},
		{[]string{"2", ctxdump.SystemSelector}, ctxdump.SectionSystem},
		{[]string{ctxdump.UserSelector, chatContextOutFlag, "/tmp/u.md"}, ctxdump.SectionUser},
	} {
		got, perr := parseChatContextArgs(c.args)
		if perr != nil {
			t.Errorf("/context %v: %v", c.args, perr)
			continue
		}
		if got.Section != c.want {
			t.Errorf("/context %v resolved to section %q, want %q", c.args, got.Section, c.want)
		}
	}
	_, cerr := parseChatContextArgs([]string{ctxdump.SystemSelector, ctxdump.UserSelector})
	if cerr == nil {
		t.Fatalf("/context --system --user was ACCEPTED — one side would be dropped and the caller would never know")
	}
	for _, want := range []string{ctxdump.SystemSelector, ctxdump.UserSelector} {
		if !strings.Contains(cerr.Error(), want) {
			t.Errorf("the mutual-exclusion refusal does not name %q: %v", want, cerr)
		}
	}
}

// TestContextSlashSectionWritesADifferentFile is the headline acceptance for the
// CHAT arm: `/context --system` writes the system-only artifact under the section
// name, holds the system half and NOT the messages, and leaves the full page
// alone (the full page is the evidence; a filtered view must never take its name).
func TestContextSlashSectionWritesADifferentFile(t *testing.T) {
	const turns = 3
	rl, sl := contextAxisFixture(t, turns, 2)
	defer func() { _ = rl.Close() }() //stylegate:ignore the raw logger is closed on the way out of this fixture; its records were already synced and read
	defer func() { _ = sl.Close() }() //stylegate:ignore the session logger is closed on the way out of this fixture; its records were already synced and read

	sessDir := t.TempDir()
	sess := &chatSession{sessionDir: sessDir}
	ag := &agent.Agent{RawLog: rl, SessionLog: sl}
	var console bytes.Buffer
	in := &chatInput{errOut: &console}

	// The FULL page first, so the section export below is measured against a file
	// that already exists.
	chatContextHandleLine(in, ag, sess, "/context")
	fullPath := filepath.Join(sessDir, fmt.Sprintf(ctxdump.FileNameFormat, turns))
	fullBefore, ferr := os.ReadFile(fullPath)
	if ferr != nil {
		t.Fatalf("the full export is not on disk: %v", ferr)
	}

	console.Reset()
	chatContextHandleLine(in, ag, sess, "/context --system")
	sysNotice := console.String()
	sysPath := filepath.Join(sessDir, fmt.Sprintf(ctxdump.SystemFileNameFormat, turns))
	if _, serr := os.Stat(sysPath); serr != nil {
		t.Fatalf("/context --system did not write %q: %v (console: %q)", sysPath, serr, sysNotice)
	}
	if !strings.Contains(sysNotice, filepath.Base(sysPath)) {
		t.Errorf("the one line does not name the section artifact %q: %q", filepath.Base(sysPath), sysNotice)
	}
	sysPage, serr := os.ReadFile(sysPath)
	if serr != nil {
		t.Fatalf("reading the section export %q: %v", sysPath, serr)
	}
	if !strings.Contains(string(sysPage), ctxdump.SystemHeading) || !strings.Contains(string(sysPage), ctxdump.ToolsHeading) {
		t.Errorf("the system export does not hold the system half: %q", string(sysPage))
	}
	if strings.Contains(string(sysPage), ctxdump.MessagesHeading) {
		t.Errorf("the system export holds the message section as well — that is not a filtered view")
	}

	console.Reset()
	chatContextHandleLine(in, ag, sess, "/context --user")
	userNotice := console.String()
	userPath := filepath.Join(sessDir, fmt.Sprintf(ctxdump.UserFileNameFormat, turns))
	if _, uerr := os.Stat(userPath); uerr != nil {
		t.Fatalf("/context --user did not write %q: %v (console: %q)", userPath, uerr, userNotice)
	}
	userPage, uerr := os.ReadFile(userPath)
	if uerr != nil {
		t.Fatalf("reading the section export %q: %v", userPath, uerr)
	}
	if !strings.Contains(string(userPage), ctxdump.MessagesHeading) || !strings.Contains(string(userPage), "round-6-body") {
		t.Errorf("the messages export does not hold the conversation: %q", string(userPage))
	}
	if strings.Contains(string(userPage), ctxdump.SystemHeading) || strings.Contains(string(userPage), ctxdump.ToolsHeading) {
		t.Errorf("the messages export holds the system half as well")
	}
	// The screen names which section the size belongs to (two exports of one turn
	// would otherwise print identical lines).
	if !strings.Contains(sysNotice, "section:") || !strings.Contains(userNotice, "section:") {
		t.Errorf("the one line does not name the section: system=%q user=%q", sysNotice, userNotice)
	}

	// THE EVIDENCE IS UNTOUCHED: the full page still holds the whole request.
	fullAfter, aerr := os.ReadFile(fullPath)
	if aerr != nil {
		t.Fatalf("re-reading the full page: %v", aerr)
	}
	if !bytes.Equal(fullBefore, fullAfter) {
		t.Errorf("the full page changed after the two section exports (%d → %d bytes) — the diffable evidence must not move",
			len(fullBefore), len(fullAfter))
	}

	// And the pair is refused on the console, with the reason, writing nothing.
	console.Reset()
	chatContextHandleLine(in, ag, sess, "/context --system --user")
	if refusal := console.String(); !strings.Contains(refusal, "pick one") {
		t.Errorf("the console accepted --system --user or refused it without a reason: %q", refusal)
	}
}
