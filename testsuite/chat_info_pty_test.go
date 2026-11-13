package testsuite

// chat_info_pty_test.go — `/info` ON A REAL pty: the session-identity block as the
// TERMINAL showed it, cross-checked against the disk, with the printed commands
// handed to a real shell.
//
// WHAT THE UNIT SUITE CANNOT SAY, and why this file exists. cmd/agent's
// chat_info_test.go asserts the block's BYTES (a pure renderer) and its cells on
// the reserved band (panelHarness). Neither can say that the REAL binary, on a
// REAL terminal, prints an identity that MATCHES the session it is writing: the
// UUID on the screen must be the directory the run is writing into and the one
// `gopi log --last` reports, and the lines the block invites the reader to copy
// must RUN when they are pasted. That is the epic's acceptance, and it is the half
// a renderer test is structurally unable to reach (PROJECT.md Rule 31: the changed
// surface is EXECUTED, not merely compiled).
//
// THE INSTRUMENT IS THE PTY STREAM, and that was a MEASURED choice, not a
// preference. The repo's other chat screen cases read the surface's own
// `--dump-screen` artifact, so this case was first built that way — and the
// artifact FAILED to carry the block: measured over 20 samples at 500ms each
// (`frames=315 dumps=30` frozen, byte-identical from before the `/info` line to
// 10s after it; only `you> /info` and the completion menu's own rows were in the
// grid, never a `session info:` row). The pty stream of the SAME run carried the
// whole block, cell for cell. So for a REPL-NATIVE line the artifact lagged the
// terminal; the terminal is what the operator read, and it is what this case
// asserts on. The artifact's lag is a separate defect, filed rather than papered
// over here (see the task report's `## Not Done`).
//
// WRAPPING IS NOT THE SUBJECT OF THE ROW ASSERTIONS. The band wraps long values
// (a session path under a test home is longer than the band), so a row's value is
// reassembled from its continuation rows before it is checked; the wrap-at-
// narrow-geometry reading belongs to the panel harness's own case.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/uiclient"
	"golang.org/x/sys/unix"
)

// The words this case spells. The block's own labels live in cmd/agent
// (chat_info.go) and a `package main` constant is not importable — the same
// reason chatPromptMark is spelled in chat_pty_test.go (ONE reading of the same
// emit, quoted here because the harness must assert the text a reader saw).
const (
	// chatInfoPTYHeader is chat_info.go's chatInfoHeader.
	chatInfoPTYHeader = "session info:"
	// chatInfoPTYLabelSession / …Dir / …File are chat_info.go's row labels.
	chatInfoPTYLabelSession = "session"
	chatInfoPTYLabelDir     = "session dir"
	chatInfoPTYLabelFile    = "session file"
	// chatInfoPTYReplayHeader is chat_info.go's chatInfoReplayHeader: the copy
	// section's own title, and the marker the block is complete by.
	chatInfoPTYReplayHeader = "copy (these lines are runnable as they stand):"
	// chatInfoPTYReplayPrefix / chatInfoPTYLogPrefix are the two commands the
	// copy section prints, up to the session id. They are spelled here the way
	// the suite spells the prompt: those words come from package main's constants
	// (runcmd.BinaryName + CliVerbChat + chatFlagResume, and runcmd.BinaryName +
	// CliVerbSession + CliVerbLog), which no test can import. Deriving them from
	// the binary's own output would make the assertion circular — the READER's
	// reading of the line must be independent of the code that wrote it.
	//
	// THE LOG PREFIX CARRIES THE GROUP WORD, and that is a correction, not a
	// preference. It was spelled `gopi log ` — the regime before verb-retirement
	// phase 1 (2026-10-06) moved the session readers under the `session` group,
	// after which the product prints `gopi session log <id>` (chat_info.go's
	// chatInfoCommands: runcmd.BinaryName + CliVerbSession + CliVerbLog) and the
	// bare spelling is REFUSED at argv[0]. The stale prefix made the block
	// detector below unable to ever match its last line: measured on a stream
	// captured from a real pty run (session 01a16a17's probe3-fast.plain), the
	// block was COMPLETE on screen — header, session row, copy title, replay line
	// and `gopi session log <id>` — while chatInfoPTYHasBlock returned FALSE with
	// logLine=false and every other element true. So this case failed by TIMEOUT
	// for 20s on a block it had already received, and the failure message's
	// 1500-byte quote (chatInfoPTYQuote keeps the stream's PREFIX) stopped short
	// of a block that begins at byte 2820 of 3648 — which is why the symptom
	// looked like "the menu ate the Enter".
	chatInfoPTYReplayPrefix = "gopi chat --resume "
	chatInfoPTYLogPrefix    = "gopi session log "
	// chatInfoPTYUUIDShape is the session id's shape: the full canonical UUID,
	// all 36 cells of it. A truncated handle is the failure this pattern exists
	// to catch — a prefix of a UUID still LOOKS like an id.
	chatInfoPTYUUIDShape = `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	// chatInfoPTYLabelSep is chat_info.go's chatInfoLabelSep — the cells between
	// a row's label and its value.
	chatInfoPTYLabelSep = "  "
	// chatInfoPTYAsk / chatInfoPTYAnswer drive ONE real turn before `/info`, so
	// the session carries a `→ REQUEST` checkpoint and the replay line the block
	// prints is genuinely resumable rather than refused for want of a turn (the
	// task report's Evidence item 5 measured `--resume` refusing a session that
	// held only slash lines).
	chatInfoPTYAsk    = "answer with the token"
	chatInfoPTYAnswer = "TURN-OK"
	// chatInfoPTYSlash / chatInfoPTYWord are the input: the prefix the REPL's own
	// vocabulary uses and the word the operator types. They are spelled because
	// they are the INPUT, not the subject — that the product answers this word is
	// what the case measures.
	chatInfoPTYSlash = "/"
	chatInfoPTYWord  = "info"
	// chatInfoPTYQuitWord is cmd/agent's quitCommandName as typed input;
	// chatInfoPTYBye is the REPL's last line (the string chat_queue_pty_test.go
	// spells as chatGoodbyeSpelled — one reading of the same emit, per file).
	chatInfoPTYQuitWord = "quit"
	chatInfoPTYBye      = "bye."
	// chatInfoPTYQuoteMax bounds a captured stream quoted in a failure message (a
	// whole screen at once drowns the reading it is meant to support).
	chatInfoPTYQuoteMax = 1500
)

// gutterProbeGlyph is the band's turn marker (`▌ `, lineedit.TurnMarker) WITHOUT
// its trailing space: the glyph whose presence at the head of a copied row is the
// defect. It is read from the ONE definition (lineedit.TurnMarker) rather than
// retyped, so a re-glyph of the band cannot leave this case checking a character
// the surface no longer draws.
var gutterProbeGlyph = strings.TrimSpace(lineedit.TurnMarker)

// The pty's geometry. WIDE on purpose: this case is not about wrapping (the panel
// harness owns that reading), and a 120-column terminal keeps the copy section's
// command lines whole so a reader's copy can be asserted cell-for-cell.
const (
	chatInfoPTYCols = 120
	chatInfoPTYRows = 40
	// chatInfoPTYBlockWait bounds the wait for the block to reach the stream.
	chatInfoPTYBlockWait = 20 * time.Second
	// chatInfoPTYNotifyWait is a poll interval for the stream reader (it returns
	// the instant the block is complete).
	chatInfoPTYNotifyWait = 150 * time.Millisecond
)

// TestChatInfo_OnRealPTY_IdentityMatchesDiskAndTheCommandRuns is the epic's
// end-to-end acceptance on a real pty and the real binary:
//
//	(1) `/info` prints the FULL session UUID, and that UUID equals the session
//	    directory on disk AND the id the session RECORD carries AND the one
//	    `gopi log --last` reports — one value, four surfaces;
//	(2) the block's copy lines appear as BARE commands (no gutter glyph, no
//	    label, no indent), and each one RUNS when a shell is handed it as it
//	    stands;
//	(3) `gopi log <id>` — the pasted line — exits 0.
func TestChatInfo_OnRealPTY_IdentityMatchesDiskAndTheCommandRuns(t *testing.T) {
	bin := testutil.GopiBin(t)
	t.Logf("INFO-E2E build identity: %s", chatRenderBuildIdentity(t, bin))

	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatInfoPTYAnswer})
	defer llm.Close()

	// The evidence this run leaves (the pty's byte stream), in a scratch
	// directory OFF the `/` partition and outside the worktree, so the
	// deliverable stays committed files only (Rules 22, 30).
	evidence := testutil.TestScratchDir(t, "chat-info-e2e")

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)

	// A pty that never received a size is not the terminal this case is about
	// (the banner case makes the same check): install the geometry and verify the
	// child really sees it.
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: chatInfoPTYCols, Row: chatInfoPTYRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable on this host: %v", err)
	}
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	// ONE real turn first, so the session has a `→ REQUEST` checkpoint — the
	// replay line's own precondition.
	chat.writeGapped(t, chatInfoPTYAsk)
	chat.write(t, "\r")
	chat.waitFor(t, chatInfoPTYAnswer, ptyPromptTimeout)

	// The surface under test, driven as the operator drives it: the bare word,
	// typed, then Enter.
	chat.writeGapped(t, chatInfoPTYSlash+chatInfoPTYWord)
	chat.write(t, "\r")
	stream := chatInfoPTYAwaitBlock(t, chat)
	rows := chatInfoPTYBlockRows(stream)
	for i, r := range rows {
		t.Logf("INFO-E2E screen row %03d|%s", i, r)
	}

	// ---- (1) the identity, four surfaces -----------------------------------
	printed := chatInfoPTYRowValue(t, rows, chatInfoPTYLabelSession)
	if !regexp.MustCompile(chatInfoPTYUUIDShape).MatchString(printed) {
		t.Fatalf("the session row printed %q, which is not a FULL UUID (%s) — a truncated handle is the failure this case exists for:\n%s",
			printed, chatInfoPTYUUIDShape, strings.Join(rows, "\n"))
	}
	// Disk: the directory this run writes into, derived through the layout the
	// product resolves (config.ProfileDirName + config.SessionsDirName), never a
	// second spelling of ".gopi/sessions".
	sessionsRoot := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	entries, err := os.ReadDir(sessionsRoot)
	if err != nil {
		t.Fatalf("read the sessions root %s: %v", sessionsRoot, err)
	}
	if len(entries) != 1 {
		t.Fatalf("%s holds %d entries, want the one session this run created", sessionsRoot, len(entries))
	}
	if got := entries[0].Name(); got != printed {
		t.Errorf("the printed UUID %q is not the session directory %q — /info named a session that is not this run's",
			printed, got)
	}
	// The RECORD: the session's own JSONL, read through the suite's loader (which
	// also pins "exactly one session").
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	if sess.ID != printed {
		t.Errorf("the session record's id %q != the printed UUID %q", sess.ID, printed)
	}
	// The path rows. The band wraps a long path, so each value is reassembled
	// from its continuation rows: the claim is about the WHOLE value — this
	// session's directory and JSONL — not about the part one row happened to fit.
	dirValue := chatInfoPTYRowValue(t, rows, chatInfoPTYLabelDir)
	if !strings.HasPrefix(dirValue, sessionsRoot) || !strings.Contains(dirValue, printed) {
		t.Errorf("the session-dir row reads %q, want the path under %q carrying session %q", dirValue, sessionsRoot, printed)
	}
	fileValue := chatInfoPTYRowValue(t, rows, chatInfoPTYLabelFile)
	if !strings.HasPrefix(fileValue, sessionsRoot) || !strings.Contains(fileValue, printed) {
		t.Errorf("the session-file row reads %q, want the JSONL inside %q for session %q", fileValue, sessionsRoot, printed)
	}
	// The reader: `gopi session log --last` on the same profile — the FAMILY
	// address, because the flat `log` is RETIRED and the binary refuses it at
	// argv[0] (the refusal was the assertion, not the reader).
	env := chatInfoPTYPasteEnv(t, home, llm.URL(), bin)
	stdout, stderr, code := testutil.RunGopi(t, []string{cliverbs.Session, cliverbs.Log, "--last"}, "", env, work)
	if code != 0 {
		t.Fatalf("gopi log --last exited %d: %s\n%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, printed) {
		t.Errorf("`gopi log --last` does not report session %q — the reader and /info disagree:\n%s", printed, stdout)
	}

	// ---- (2) the copy section, as the screen showed it ---------------------
	replayLine := chatInfoPTYCopyLine(t, rows, chatInfoPTYReplayPrefix, printed)
	logLine := chatInfoPTYCopyLine(t, rows, chatInfoPTYLogPrefix, printed)
	t.Logf("INFO-E2E copy lines lifted from the screen: %q and %q", replayLine, logLine)

	// The end of the session, so the child exits cleanly and the record closes.
	chat.writeGapped(t, chatInfoPTYSlash+chatInfoPTYQuitWord)
	chat.write(t, "\r")
	ptyOut := chat.waitFor(t, chatInfoPTYBye, ptyPromptTimeout)
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 (the deliberate /quit)", code)
	}
	if err := os.WriteFile(filepath.Join(evidence, chatRenderPTYLogName), []byte(ptyOut), dumpFilePerm); err != nil {
		t.Fatalf("keep the pty stream as evidence: %v", err)
	}

	// ---- (3) the pasted lines, handed to a shell AS THEY STAND -------------
	for _, pasted := range []string{logLine, replayLine} {
		out, errOut, rc := chatInfoPTYPaste(t, pasted, env, work)
		t.Logf("INFO-E2E pasted %q → rc=%d\n%s", pasted, rc, chatInfoPTYQuote(out+"\n"+errOut))
		if rc != 0 {
			t.Errorf("the printed line %q exited %d when pasted — a command the block invites the reader to copy must run:\nstdout:\n%s\nstderr:\n%s",
				pasted, rc, out, errOut)
		}
	}
	t.Logf("INFO-E2E evidence kept under %s (%s)", evidence, chatRenderPTYLogName)
}

// chatInfoPTYAwaitBlock reads the child's stream until the block is complete on
// screen (its header, an id-shaped session row and the copy section's title), and
// returns the ANSI-stripped stream. It is a synchronization wait — the reader
// consumes bytes and returns the moment the block is complete — never a sleep; a
// stream that never carries it fails with the rows it did carry.
func chatInfoPTYAwaitBlock(t *testing.T, chat *ptySession) string {
	t.Helper()
	var got strings.Builder
	deadline := time.After(chatInfoPTYBlockWait)
	for {
		select {
		case b, ok := <-chat.rx:
			if !ok {
				t.Fatalf("the pty closed while waiting for the /info block; stream so far:\n%s",
					chatInfoPTYQuote(chatRenderStripANSI(got.String())))
			}
			got.WriteByte(b)
			plain := chatRenderStripANSI(got.String())
			if chatInfoPTYHasBlock(chatInfoPTYBlockRows(plain)) {
				return plain
			}
		case <-deadline:
			t.Fatalf("the screen never carried the /info block within %s; stream so far:\n%s",
				chatInfoPTYBlockWait, chatInfoPTYQuote(chatRenderStripANSI(got.String())))
		}
	}
}

// chatInfoPTYBlockRows cuts a screen stream into the rows a terminal laid out:
// a carriage return and a line feed both end a row (the band positions the cursor
// with a CR and the answer rows end with a LF), and every row is stripped of
// trailing blanks. Rows that are not part of the block are kept — the copy-clean
// assertion is stronger when the row it checks is read among the rows the screen
// really had.
func chatInfoPTYBlockRows(plain string) []string {
	// The block is the tail: everything from its header on. Cutting there keeps
	// the assertion about the block's own rows rather than about a banner a
	// previous frame drew.
	if i := strings.LastIndex(plain, chatInfoPTYHeader); i >= 0 {
		plain = plain[i:]
	}
	plain = strings.NewReplacer("\r", "\n").Replace(plain)
	rows := make([]string, 0, strings.Count(plain, "\n")+1)
	for _, r := range strings.Split(plain, "\n") {
		rows = append(rows, strings.TrimRight(r, " "))
	}
	return rows
}

// chatInfoPTYHasBlock reports whether the rows carry the whole block: its header,
// an id-shaped session row, the copy section's title, AND both printed commands
// complete with their id. The copy commands are the LAST bytes the block writes,
// so waiting on them is waiting for the block to be complete rather than for its
// first byte — the earlier reading waited on the title and read the screen one
// frame early (measured: header + facts + title, commands not yet drawn).
func chatInfoPTYHasBlock(rows []string) bool {
	_, hasSession := chatInfoPTYValueOK(rows, chatInfoPTYLabelSession)
	hasHeader := false
	hasCopyTitle := false
	idShape := regexp.MustCompile(chatInfoPTYUUIDShape)
	replay, logLine := false, false
	for _, r := range rows {
		trimmed := strings.TrimLeft(r, " ")
		if trimmed == chatInfoPTYHeader {
			hasHeader = true
		}
		if strings.HasPrefix(trimmed, chatInfoPTYReplayHeader) {
			hasCopyTitle = true
		}
		// CONTAINS, not HasPrefix: the wait is about the block being COMPLETE,
		// and a command drawn behind a gutter is still the last thing the block
		// wrote. Whether the row is BARE is the copy-clean assertion's own
		// question — a wait that silently absorbed it would report a document
		// defect as a timeout and hide which one it was.
		if id, ok := chatInfoPTYCopyID(trimmed, chatInfoPTYReplayPrefix); ok && idShape.MatchString(id) {
			replay = true
		}
		if id, ok := chatInfoPTYCopyID(trimmed, chatInfoPTYLogPrefix); ok && idShape.MatchString(id) {
			logLine = true
		}
	}
	return hasHeader && hasSession && hasCopyTitle && replay && logLine
}

// chatInfoPTYCopyID returns the id a row carries for one of the printed commands,
// whether the row is bare or decorated — the READER's reading of the row (find the
// command, take what follows it), which is what the wait uses.
func chatInfoPTYCopyID(row, prefix string) (string, bool) {
	i := strings.Index(row, prefix)
	if i < 0 {
		return "", false
	}
	return strings.TrimSpace(row[i+len(prefix):]), true
}

// chatInfoPTYLabelPrefixes is every row label of the block, in the order they are
// printed. It is what tells a continuation row (a wrapped value's next line) from
// the start of the next fact.
var chatInfoPTYLabelPrefixes = []string{
	chatInfoPTYLabelSession,
	chatInfoPTYLabelDir,
	chatInfoPTYLabelFile,
	"model",
	"cwd",
	"repo",
	"node",
	"tools",
}

// chatInfoPTYValueOK finds the row a label owns and REASSEMBLES its value from the
// rows it wrapped onto. The label is matched WITH its separator
// (chatInfoPTYLabelSep) so "session" never matches "session dir", and it is
// matched from the row's left edge rather than by substring: a value that merely
// CONTAINS a label is a different row. A continuation row is one that starts no
// label; the reassembly stops there.
func chatInfoPTYValueOK(rows []string, label string) (string, bool) {
	prefix := label + chatInfoPTYLabelSep
	for i, r := range rows {
		trimmed := strings.TrimLeft(r, " ")
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		for _, next := range rows[i+1:] {
			cont := strings.TrimRight(next, " ")
			// A row carrying the band's cursor escape, a prompt or the status row
			// is not this value's continuation; a row that starts a label is the
			// next fact.
			if strings.TrimSpace(cont) == "" || strings.HasPrefix(cont, chatPromptMark) ||
				strings.HasPrefix(cont, " ") || chatInfoPTYStartsLabel(cont) {
				break
			}
			value += cont
		}
		return value, true
	}
	return "", false
}

// chatInfoPTYStartsLabel reports whether a row begins one of the block's facts.
func chatInfoPTYStartsLabel(row string) bool {
	for _, label := range chatInfoPTYLabelPrefixes {
		if strings.HasPrefix(row, label+chatInfoPTYLabelSep) {
			return true
		}
	}
	return false
}

// chatInfoPTYRowValue is chatInfoPTYValueOK, failing loudly when the row is
// absent — an assertion measured against "" would pass vacuously.
func chatInfoPTYRowValue(t *testing.T, rows []string, label string) string {
	t.Helper()
	value, ok := chatInfoPTYValueOK(rows, label)
	if !ok {
		t.Fatalf("the screen carries no %q row — the block was read without the fact this case asserts:\n%s",
			label, strings.Join(rows, "\n"))
	}
	return value
}

// chatInfoPTYCopyLine reads ONE copy line off the screen and insists it is a BARE
// command: the row's whole content IS the command, so nothing decorative (the
// turn gutter, a label, an indent, a wrap of the id) stands between the reader's
// selection and the shell. This is copy-cleanliness asserted at its PRODUCT level
// — not "the renderer emits no glyph" but "the cells on the screen are the
// command".
func chatInfoPTYCopyLine(t *testing.T, rows []string, prefix, id string) string {
	t.Helper()
	want := prefix + id
	for _, r := range rows {
		if r != want {
			continue
		}
		// A row whose cells ARE the command must still not OPEN with the band's
		// marker: the gutter is the row's leftmost cells, so a command preceded by
		// it is a selection that does not run. Checked on the row AS DRAWN, before
		// any trimming, so the assertion cannot be satisfied by the trim.
		if strings.HasPrefix(r, gutterProbeGlyph) {
			t.Errorf("the row carrying %q opens with the turn gutter %q — a paste of it does not run:\n%q",
				want, gutterProbeGlyph, r)
		}
		return want
	}
	// Not found as a whole row: report WHY, distinguishing "the row carries it
	// with a gutter or a label" (the defect this asserts against) from "the line
	// is not there at all". A row that merely CONTAINS the command (a gutter or a
	// wrap in front of it) is the first case.
	for _, r := range rows {
		if strings.Contains(r, want) {
			t.Fatalf("the command %q is on the screen but the row is not bare — a reader selecting it would paste the decoration too:\n%q",
				want, r)
		}
	}
	t.Fatalf("no screen row IS the command %q. Rows:\n%s", want, strings.Join(rows, "\n"))
	return ""
}

// chatInfoPTYPasteEnv is the environment a pasted line runs in: the mocked
// profile (testutil.MockEnv pins GOPI_HOME and the ground root) with the BINARY'S
// OWN DIRECTORY prepended to PATH, so the command word `gopi` in the copied line
// resolves to the build under test rather than to whatever is installed. Without
// that, the paste would prove something about another binary.
func chatInfoPTYPasteEnv(t *testing.T, home, mockURL, bin string) []string {
	t.Helper()
	env := testutil.MockEnv(t, home, mockURL)
	return append(env, "PATH="+filepath.Dir(bin)+":"+os.Getenv("PATH"))
}

// chatInfoPTYPaste runs one printed line through a SHELL, exactly as a reader who
// selected it would (bash -c, the line unmodified), with no stdin of its own — the
// interactive verb sees EOF and ends, which is what a pasted line in a scripted
// shell does. It returns the exit code the shell reported.
func chatInfoPTYPaste(t *testing.T, line string, env []string, dir string) (string, string, int) {
	t.Helper()
	cmd := exec.Command("bash", "-c", line)
	cmd.Env = env
	cmd.Dir = dir
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err == nil {
		return out.String(), errOut.String(), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return out.String(), errOut.String(), ee.ExitCode()
	}
	t.Fatalf("run the pasted line %q: %v", line, err)
	return "", "", -1
}

// chatInfoPTYQuote trims a captured stream to a printable quote.
func chatInfoPTYQuote(s string) string {
	if len(s) <= chatInfoPTYQuoteMax {
		return s
	}
	return s[:chatInfoPTYQuoteMax] + "…(truncated)"
}

// chatInfoArtifactWait bounds the wait for the ARTIFACT to carry the block. It is
// wider than the sink's own window (internal/uiclient's dumpMinInterval) by two
// orders of magnitude, so a loaded machine cannot fail the case for the wrong
// reason — the claim under test is "the window closes and installs the frame",
// never "it closes within a particular millisecond".
//
// It joins Dump.Lines, NOT Dump.Text(). Text() is the FILE's own rendering and
// every row of it carries the dump's "NNN|" row prefix — useful to a human
// reading the artifact, fatal to chatInfoPTYBlockRows, which reads a row's LABEL
// from its first cells. Lines is the readable rendering (identical row content,
// no prefix), so the two channels' readings stay comparable: the SAME helper
// parses both, and the assertion is about the block, not about the file format.
func chatInfoPTYGridText(cand uiclient.Candidate) string {
	return strings.Join(cand.Dump.Lines, "\n")
}
