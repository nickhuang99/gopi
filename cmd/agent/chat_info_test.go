package main

// chat_info_test.go — the `/info` contract, asserted on the RENDERED BLOCK rather
// than through a terminal.
//
// WHAT A UNIT CASE CAN AND CANNOT PROVE (Rule 31's unit half). The claim
// "`/info` prints THIS session's UUID" is a claim about a running REPL reaching
// its arm with the live logger and the live session — that is asserted end to end
// on the real binary (the report's `## Verification` carries the run). What is
// asserted HERE is everything the E2E case must be able to TRUST: the block's
// shape (the UUID verbatim, one fact per line), its COPY CLEANLINESS (no gutter
// glyph, no reserved column, no indent on a runnable line), the vocabulary
// agreement (/help, /help info, the menu, the refusal all learn the word from ONE
// place), and the anti-re-inline guard that the REPL arm routes through the
// renderer instead of printing its own copy of the block.

import (
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/tools"
)

// The literal the block must NEVER carry, and the prefix every copyable line must
// carry. Both are READ from the constants that own them (Rule 14) rather than
// retyped: gutterProbeGlyph is the band's own turn marker (lineedit.TurnMarker),
// the glyph whose measured arrival in a paste is why internal/lineedit/answer.go
// draws no gutter on the copying layer, and chatInfoVerbPrefix is the binary's own
// name, which cli_verbs.go's verb words are suffixed to ("gopi chat", "gopi session log").
var (
	gutterProbeGlyph   = strings.TrimSpace(lineedit.TurnMarker)
	chatInfoVerbPrefix = runcmd.BinaryName + " "
)

// TestChatInfoProbesAreNotEmpty keeps the two probes above from making their own
// assertions vacuous: a blank gutter probe would pass every "no gutter" check,
// and a blank verb prefix would pass every "a pasteable command" check.
func TestChatInfoProbesAreNotEmpty(t *testing.T) {
	if gutterProbeGlyph == "" {
		t.Fatal("the gutter probe is empty — the copy-clean assertion above is vacuous")
	}
	if strings.TrimSpace(chatInfoVerbPrefix) == "" {
		t.Fatal("the verb prefix is blank — the copyable-line assertion is vacuous")
	}
}

// infoFixture returns a filled fields struct with a REAL-looking UUID, so the
// assertions below read the way the operator's own block reads.
func infoFixture() chatInfoFields {
	return chatInfoFields{
		SessionID:   "4c9a1f3e-8b2d-4a10-9e6f-2c5d7a0b1e33",
		SessionDir:  "/home/user/.gopi/sessions/4c9a1f3e-8b2d-4a10-9e6f-2c5d7a0b1e33",
		SessionFile: "/home/user/.gopi/sessions/4c9a1f3e-8b2d-4a10-9e6f-2c5d7a0b1e33/session.jsonl",
		Model:       "deepseek-v4",
		Dir:         "/home/user/workspace/gopi-task",
		Repo:        "/home/user/workspace/gopi-task",
		InRepo:      true,
		Node:        "peerbox (server→hp)",
		ToolsOn:     true,
	}
}

// TestChatInfoBlockCarriesTheSessionUUIDVerbatim is the epic's HARD requirement:
// the FULL session UUID is in the output, byte for byte — the handle `gopi session log
// <id>` and `gopi chat --resume <id>` take. A truncated or elided id is the
// defect this command exists to prevent.
func TestChatInfoBlockCarriesTheSessionUUIDVerbatim(t *testing.T) {
	f := infoFixture()
	block := chatInfoBlock(f)
	if !strings.Contains(block, f.SessionID) {
		t.Fatalf("the block does not carry the session UUID verbatim:\n%s", block)
	}
	// On its OWN line, and on the labelled row — a UUID that shares a line with
	// another fact cannot be selected out of the block.
	wantRow := chatInfoLabelSession + chatInfoLabelSep + f.SessionID
	if !strings.Contains(block, wantRow+"\n") {
		t.Errorf("the UUID is not alone on its row (want %q):\n%s", wantRow, block)
	}
	// The other answers the epic names, each on its own row.
	for _, want := range []string{
		chatInfoLabelSessionDir + chatInfoLabelSep + f.SessionDir,
		chatInfoLabelSessionFile + chatInfoLabelSep + f.SessionFile,
		chatInfoLabelModel + chatInfoLabelSep + f.Model,
		chatInfoLabelDir + chatInfoLabelSep + f.Dir,
		chatInfoLabelNode + chatInfoLabelSep + f.Node,
	} {
		if !strings.Contains(block, want+"\n") {
			t.Errorf("the block is missing the row %q:\n%s", want, block)
		}
	}
	// Every row states a VALUE: an empty one is the failure mode Rule 15 names,
	// and it is what a reader would otherwise fill in with a guess. (The tools
	// row is asserted against the banner's own spelling, so /info and the startup
	// panel cannot describe one session two ways.)
	for _, line := range linesOf(block) {
		if strings.HasSuffix(line, chatInfoLabelSep) {
			t.Errorf("a row has an EMPTY value (silence where a fact belongs): %q\n%s", line, block)
		}
	}
	if !strings.Contains(block, chatInfoLabelTools+chatInfoLabelSep+chatInfoToolsValue(true)+"\n") {
		t.Errorf("the tools row does not state the live mode (%q):\n%s",
			chatInfoToolsValue(true), block)
	}
}

// TestChatInfoBlockIsCopyClean is the acceptance criterion that costs a paste,
// turned into an assertion: the lines a reader copies ARE the commands. Nothing
// decorative may appear on them — no turn gutter (the band's `▌ ` marker, whose
// absence internal/lineedit/answer.go gives the copying layer), no reserved
// column, no bullet, no indent, no trailing padding.
func TestChatInfoBlockIsCopyClean(t *testing.T) {
	block := chatInfoBlock(infoFixture())
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")

	// 1. No gutter glyph anywhere in the block — the operator's measured failure
	//    was a paste whose first line died with the word `▌`.
	for i, line := range lines {
		if strings.Contains(line, gutterProbeGlyph) {
			t.Errorf("line %d carries the band's gutter glyph %q — a paste of it does not run:\n%q",
				i+1, gutterProbeGlyph, line)
		}
	}
	// 2. No line is padded or indented: what a selection takes is what is there.
	for i, line := range lines {
		if line != strings.TrimRight(line, " ") {
			t.Errorf("line %d has trailing padding (%q) — a selection would carry it", i+1, line)
		}
		if line != "" && line != strings.TrimLeft(line, " \t") {
			t.Errorf("line %d is indented (%q) — a copied command must start at column 0", i+1, line)
		}
	}
	// 3. Every line of the COPY SECTION is runnable AS IT STANDS: it starts with
	//    the gopi verb word (the CLI's own constant), with no label in front.
	cmds := chatInfoCommands(infoFixture())
	if len(cmds) == 0 {
		t.Fatalf("the block prints no command to copy:\n%s", block)
	}
	for _, line := range cmds {
		if !strings.HasPrefix(line, chatInfoVerbPrefix) {
			t.Errorf("the copyable line %q does not start with the gopi verb — a reader cannot paste a label", line)
		}
		if !strings.Contains(block, line+"\n") {
			t.Errorf("the copyable line %q is not in the rendered block:\n%s", line, block)
		}
	}
	// 4. The copy section is SEPARATED from the facts so a selection cannot drag
	//    a label along: the header line, then the commands.
	if !strings.Contains(block, chatInfoReplayHeader+"\n"+cmds[0]+"\n") {
		t.Errorf("the copy section is not headed by %q followed immediately by a command:\n%s",
			chatInfoReplayHeader, block)
	}
}

// TestChatInfoPrintsNoShortAlias is the epic's BAN, asserted rather than trusted:
// the command answers to its FULL word and to nothing shorter. A one-letter or
// shortest-unique-prefix spelling would be a second word every surface (switch,
// menu, refusal, docs) would have to carry — and a prefix scheme is the invented
// dispatch mechanism the epic forbids.
func TestChatInfoPrintsNoShortAlias(t *testing.T) {
	full := chatInfoCommandName
	if len(full) <= 1 {
		t.Fatal("the word is one character: the no-short-form property below is vacuous")
	}
	// EVERY PROPER PREFIX of the word is refused by the whole discovery surface —
	// the help answer, the native usage table, the shared registry AND the shared
	// alias table (whose entries are the accepted alternate spellings; a prefix
	// that landed there would be a short form wearing an alias's clothes).
	//
	// WHY THIS IS A PROPERTY AND NOT A LIST: the gate must keep testing the same
	// thing if the word is ever renamed, and a fixed list of "i", "in" would stop
	// covering the new spelling in silence (Rule 10.4 — verify the smallest sample
	// the claim admits).
	for i := 1; i < len(full); i++ {
		short := full[:i]
		if _, err := chatHelpText(completionSlashPrefix + helpCommandName + " " + short); err == nil {
			t.Errorf("/help %s answered for the SHORT spelling %q — the word has no short form",
				short, short)
		}
		if _, ok := chatNativeUsage(short); ok {
			t.Errorf("the native usage table answers the short spelling %q", short)
		}
		if _, ok := lookupCommand(short); ok {
			t.Errorf("the shared table resolves the short spelling %q", short)
		}
		if canon, ok := chatInfoAliasTarget(short); ok {
			t.Errorf("the shared alias table makes %q a spelling of %q — a short form by another name",
				short, canon)
		}
	}
	// The FULL word, by contrast, IS an accepted spelling and has no twin: the
	// command is reached in the one way the epic allows.
	if _, ok := chatInfoAliasTarget(full); ok {
		t.Errorf("%s is listed as an alias — /info is its own command, not a redirect", full)
	}
	if !containsWord(chatReplCommandNames(), full) {
		t.Errorf("the REPL's accepted vocabulary omits the full word %q", full)
	}
	// And the switch accepts exactly the ONE spelling: the decision the epic makes
	// instead of an abbreviation scheme (a prefix resolver is the invented
	// dispatch mechanism the epic forbids).
	src, err := os.ReadFile("cli_chat.go")
	if err != nil {
		t.Fatalf("read cli_chat.go: %v", err)
	}
	armCase := `case "/" + chatInfoCommandName:`
	if n := strings.Count(string(src), armCase); n != 1 {
		t.Errorf("cli_chat.go carries the /%s arm %d times — the REPL accepts it in exactly ONE spelling",
			chatInfoCommandName, n)
	}
	if !strings.Contains(string(src), `case "/" + chatInfoCommandName:`) {
		t.Error("cli_chat.go does not accept the full word in its switch")
	}
}

// TestChatInfoIsDiscoveredByBothHelpFormes is the discoverability contract: the
// /help page lists the word, and /help info answers for it with the SAME block
// the REPL's own vocabulary resolver returns. A command an operator cannot find
// is a command that does not exist.
func TestChatInfoIsDiscoveredByBothHelpFormes(t *testing.T) {
	page, err := chatHelpText(completionSlashPrefix + helpCommandName)
	if err != nil {
		t.Fatalf("chatHelpText(/help): %v", err)
	}
	found := false
	for _, w := range slashWordsInHelpText(page) {
		if w == chatInfoCommandName {
			found = true
		}
	}
	if !found {
		t.Errorf("the /help page does not list /%s:\n%s", chatInfoCommandName, page)
	}
	usage, err := chatHelpText(completionSlashPrefix + helpCommandName + " " + chatInfoCommandName)
	if err != nil {
		t.Fatalf("/help %s: %v", chatInfoCommandName, err)
	}
	if usage != chatInfoUsage {
		t.Errorf("/help %s answered with a block that is not the command's own usage:\n%s",
			chatInfoCommandName, usage)
	}
	// The REPL's own resolver (what the switch's vocabulary answers with) must
	// agree with the help page — one text, two readers.
	if repl, ok := chatReplSlashVocabulary(chatInfoCommandName); !ok || repl != usage {
		t.Errorf("the REPL vocabulary and the help page disagree about /%s:\n%q\n%q",
			chatInfoCommandName, repl, usage)
	}
	// The word is in the REPL's accepted vocabulary, i.e. the refusal names it
	// and the completion menu offers it. THE OWNER MOVED ON 2026-11-08 (epic
	// 01a1b818-e02f): /info used to be REPL-native because the shared table could
	// not see the facts it prints, and it is now a TABLE word (it reaches the
	// browser too). What this asserts is the invariant that survived the move —
	// the menu and the refusal must not omit a word the switch answers — so it
	// asks for a VERDICT ("somebody owns it"), not for one particular owner. A
	// word owned by NEITHER still fails here, which is the regression this guards.
	if _, inTable := slashCommands[chatInfoCommandName]; !inTable && !containsWord(chatSessionWords, chatInfoCommandName) {
		t.Errorf("neither the shared table nor chatSessionWords carries %q — the menu and the refusal would omit a command the switch answers",
			chatInfoCommandName)
	}
	if !containsWord(chatReplCommandNames(), chatInfoCommandName) {
		t.Errorf("the REPL's accepted vocabulary omits %q", chatInfoCommandName)
	}
	if !strings.Contains(chatUnknownCommandError("frobnicate").Error(),
		completionSlashPrefix+chatInfoCommandName) {
		t.Errorf("the unknown-command refusal does not list /%s:\n%s",
			chatInfoCommandName, chatUnknownCommandError("frobnicate").Error())
	}
}

// TestChatInfoReplayCommandNamesTheVerbsOwnWords is the drift guard for the
// PRINTED recipe: the line the block tells a reader to paste must be spelled with
// the same constants the parser and the verb dispatch use — a flag typed into a
// string here would be a command that does not run.
func TestChatInfoReplayCommandNamesTheVerbsOwnWords(t *testing.T) {
	f := infoFixture()
	replay := chatInfoReplayCommand(f.SessionID)
	for _, want := range []string{CliVerbChat, chatFlagResume, f.SessionID} {
		if !strings.Contains(replay, want) {
			t.Errorf("the replay line %q does not carry %q", replay, want)
		}
	}
	// The session is named EXACTLY ONCE. `--resume <id>` and the positional seed
	// are the same thing asked two ways, and stating both is a usage error the
	// verb refuses by name ("one resume continues one session") — measured, exit
	// 2, on the first version of this line. So the recipe carries the switch
	// alone, and the count below is what keeps a second spelling from creeping
	// back into a line whose whole job is to be pasted.
	if n := strings.Count(replay, f.SessionID); n != 1 {
		t.Errorf("the replay line names the session %d times — one resume continues one session, "+
			"and stating it twice is refused by the verb: %q", n, replay)
	}
	// The words of the line come from the constants, so the assertion is about
	// the shape and not about a literal typed here.
	if !strings.HasPrefix(replay, runcmd.BinaryName+" "+CliVerbChat+" "+chatFlagResume+" ") {
		t.Errorf("the replay line is not %q: %q", runcmd.BinaryName+" "+CliVerbChat+" "+chatFlagResume+" <id>", replay)
	}
	// The log line uses the log verb's own constant, under the group word it
	// actually dispatches from since verb-retirement phase 1.
	logLine := runcmd.BinaryName + " " + CliVerbSession + " " + CliVerbLog + " " + f.SessionID
	if !containsLine(strings.Join(chatInfoCommands(f), "\n"), logLine) {
		t.Errorf("the copy section does not carry %q: %v", logLine, chatInfoCommands(f))
	}
}

// TestChatInfoSaysSoWhenThereIsNoSessionLog covers the honest-degradation half
// (Rule 15): a run whose logger never opened has NO UUID, and the block must say
// that instead of printing a command with a placeholder a reader would paste.
func TestChatInfoSaysSoWhenThereIsNoSessionLog(t *testing.T) {
	block := chatInfoBlock(chatInfoFields{})
	if !strings.Contains(block, chatInfoNoSession) {
		t.Errorf("a session-less block does not say it has no UUID:\n%s", block)
	}
	if !containsLine(strings.Join(chatInfoCommands(chatInfoFields{}), "\n"), chatInfoNoCommands) {
		t.Errorf("a session-less block still prints a command to copy: %v", chatInfoCommands(chatInfoFields{}))
	}
	if strings.Contains(block, CliVerbChat+"  ") || strings.Contains(block, runcmd.BinaryName+" "+CliVerbLog+" \n") {
		t.Errorf("a session-less block printed a command with an empty argument:\n%s", block)
	}
}

// TestChatInfoFieldsComeFromTheLiveSession is the READ half: /info must print
// THIS session's facts, so every field is taken from the surface that already
// owns it. The assertion is by IDENTITY of the source (the logger the REPL holds),
// which is what makes the E2E run's cross-check against the session DIRECTORY a
// check of one value rather than of two derivations.
func TestChatInfoFieldsComeFromTheLiveSession(t *testing.T) {
	logger, err := agent.NewSessionLogger("", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer func() { _ = logger.Close() }() //stylegate:ignore the session logger is closed on the way out of the surface-info fixture; its records were already read

	sess := &chatSession{toolsEnabled: true}
	sess.ground = tools.ChatGroundResolution{Dir: t.TempDir(), Repo: "/repo", InMainCheckout: true}
	ag := &agent.Agent{Model: "fixture-model", SessionLog: logger}

	f := chatInfoFieldsFor(ag, sess)
	if f.SessionID != logger.SessionID() || f.SessionID == "" {
		t.Errorf("the block's UUID is not the live logger's (%q vs %q)", f.SessionID, logger.SessionID())
	}
	if f.SessionFile != logger.Path() {
		t.Errorf("the block's JSONL path is not the logger's (%q vs %q)", f.SessionFile, logger.Path())
	}
	if f.SessionDir != sessionlayout.RecordContainer(logger.Path()) {
		t.Errorf("the block's session dir is not the JSONL's own directory (%q vs %q)",
			f.SessionDir, sessionlayout.RecordContainer(logger.Path()))
	}
	if f.Model != ag.Model {
		t.Errorf("the block's model is not the agent's (%q vs %q)", f.Model, ag.Model)
	}
	if !f.ToolsOn {
		t.Error("the block reports the start-up tools default instead of the live flag")
	}
	if f.Repo != sess.ground.Repo {
		t.Errorf("the block's repo is not the session's own ground (%q vs %q)", f.Repo, sess.ground.Repo)
	}
	// The live flag is read at RENDER time, not remembered: flipping it (what
	// /tools does) changes the block.
	sess.toolsEnabled = false
	if f2 := chatInfoFieldsFor(ag, sess); f2.ToolsOn {
		t.Error("the block still reports tools ON after the flag was flipped — it must read the live value")
	}
}

// TestChatInfoSwitchRoutesThroughTheRenderer is the anti-re-inline gate, in the
// shape the /help defect's own gate established: the arm must CALL the renderer.
// A future edit that prints the block from the switch (or answers /info with a
// second formatting) fails here while every other test stays green.
func TestChatInfoSwitchRoutesThroughTheRenderer(t *testing.T) {
	src, err := os.ReadFile("cli_chat.go")
	if err != nil {
		t.Fatalf("read cli_chat.go: %v", err)
	}
	body := string(src)
	arm := strings.Index(body, `case "/" + chatInfoCommandName:`)
	if arm < 0 {
		t.Fatal("cli_chat.go has no /info arm in the slash switch")
	}
	armBody := body[arm:]
	if n := strings.Index(armBody, "\n\t\t\tcase "); n > 0 {
		armBody = armBody[:n]
	}
	if !strings.Contains(armBody, "chatInfoHandleLine(input, ag, sess)") {
		t.Errorf("the /info arm must route through chatInfoHandleLine so the block has ONE renderer:\n%s", armBody)
	}
	// The block's own text must not be re-typed in the switch: the header and the
	// row labels are the renderer's business.
	for _, leak := range []string{chatInfoHeader, chatInfoReplayHeader, chatInfoLabelSessionFile} {
		if strings.Contains(armBody, leak) {
			t.Errorf("the /info arm re-inlines %q — the block belongs to chat_info.go alone:\n%s", leak, armBody)
		}
	}
	// And the shorthand spelling ("/i", "/in") must not appear as a switch word:
	// the epic forbids a short form.
	for _, short := range []string{`"/i":`, `case "/i"`, `case "/in"`} {
		if strings.Contains(body, short) {
			t.Errorf("cli_chat.go accepts the short form %q — /info has no alias", short)
		}
	}
}

// TestChatInfoBlockRendersOnTheBandWithNoGutter is the PAINTED-CELLS half of the
// copy-clean contract (Rule 10.2, 30 渲染真值): the flow-path test above asserts
// the bytes of one rendering, but the surface a reader actually SELECTS from is
// the reserved band — so the assertion belongs on the CELL GRID.
//
// The harness is the /help panel's own (panelHarness,
// chat_panel_rows_test.go): the same source, clock and read loop, so /info is
// measured on the layout /help was measured on. WHAT IS ASSERTED: every row of
// the block is on the BAND (never painted over the editing or status row), the
// UUID survives to the grid, and NO band row carrying the block's text opens with
// the turn's `▌ ` gutter — the glyph whose measured arrival in a paste is the
// reason internal/lineedit/answer.go draws no gutter on the copying layer.
func TestChatInfoBlockRendersOnTheBandWithNoGutter(t *testing.T) {
	h := panelHarness(t)
	h.in.PublishStatus()
	h.waitFor(t, "the published model on the status row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.layout().StatusRow), panelStatusModel)
	})
	h.typeKeys("/", "i", "n", "f", "o")
	editorTop := h.editorRow(t)
	h.waitFor(t, "the typed line on the editing row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(editorTop), "/info")
	})

	// A real session logger, so the painted block carries a REAL UUID.
	logger, err := agent.NewSessionLogger("", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer func() { _ = logger.Close() }() //stylegate:ignore same teardown on the second info case: the diagnostic output is what the test asserts, not the close
	sess := &chatSession{}
	ag := &agent.Agent{Model: "fixture-model", SessionLog: logger}

	// The command's OWN entry point (the REPL arm's body), not a hand-rolled
	// WriteLines call: what is measured is the surface the switch reaches.
	chatInfoHandleLine(h.in, ag, sess)
	h.waitFor(t, "the /info block on the band", func() bool {
		h.replay()
		_, _, band := panelBand(t, h)
		return strings.Contains(band, logger.SessionID())
	})

	block := chatInfoBlock(chatInfoFieldsFor(ag, sess))
	lines := linesOf(block)
	// A row of the block may WRAP (the band is 76 cells wide and a session path is
	// longer), so the panel assertion is made on each line's HEAD — the first
	// cells the band can carry. That is the panel suite's own convention: its
	// fixture lines are chosen to fit, and here the block's own values are the
	// content, so the head is what "the line reached the band" means.
	heads := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		head := line
		if len(head) > panelCols {
			head = head[:panelCols]
		}
		heads = append(heads, head)
	}
	// The chrome assertion is the /help panel's own: none of the block's text may
	// be painted over the editing or status row.
	assertPanelBandOnly(t, h, heads)

	_, _, band := panelBand(t, h)
	bandRows := strings.Split(band, "\n")
	for _, head := range heads {
		found := false
		for _, row := range bandRows {
			if !strings.Contains(row, head) {
				continue
			}
			found = true
			// The row the text landed on may NOT open with the gutter, in either
			// of its spellings: the turn marker (lineedit.TurnMarker) or its
			// split twin. A gutter a reader selects with the line is a paste
			// that does not run — the whole defect this check exists for.
			if strings.HasPrefix(row, lineedit.TurnMarker) || strings.HasPrefix(row, lineedit.TurnMarkerSplit) {
				t.Errorf("the band row carrying %q opens with the gutter %q — a copied command would not run:\n%q",
					head, lineedit.TurnMarker, row)
			}
		}
		if !found {
			t.Errorf("no band row carries the block line %q — it never reached the reserved region", head)
		}
	}
	// The BAND path is the one /help is measured on, so the block's rows and the
	// panel's rows are asserted the same way: the whole block, in order, with the
	// chrome intact.
	if !strings.Contains(band, logger.SessionID()) {
		t.Errorf("the session UUID did not survive to the painted grid:\n%s", band)
	}
	t.Logf("rendered band after /info (editing row %d, status row %d):\n%s",
		editorTop, h.layout().StatusRow, band)
}

// linesOf splits a rendered block into its lines.
func linesOf(block string) []string {
	return strings.Split(strings.TrimRight(block, "\n"), "\n")
}

// containsWord and containsLine are ALREADY defined once for this package
// (cmd/agent/cli_uistate_contradiction_test.go and
// worker_idle_drained_stop_e2e_test.go): the word-membership and line-membership
// predicates are reused rather than re-declared (Rule 14 — a second definition of
// a shared helper is a second reading of the same question).
