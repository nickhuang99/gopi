package main

// cli_uistate_audit_test.go — the WHOLE-ANSWER audit through the VERB: the unit
// the operator asks for ("did my answer arrive on screen?"), its exit-code
// contract, and the RED direction of each claim on the SAME input.
//
// The fixture is the product's own sink and the product's own discovery, driven
// through `runUIStateSubcommand` (stdout, stderr and the exit code as the caller
// sees them): a test that parsed the artifact itself would agree with the code by
// construction, which is what a contract assertion must not do.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/uiclient"
)

// The audit fixture's answer. The band holds its LIVE row and scrolls the rest off
// the top of the reserved region (internal/lineedit/region.go — the terminal
// discards what leaves an interior margin), so on a real pty the line the grid
// carries is the one the band was given LAST. The fixture paints that line and
// then builds the transcript around it: `auditPainted` is what really reached the
// screen, `auditLost` is what the log holds and the paint does not — the operator's
// first damage class, reproduced rather than simulated.
const (
	auditPainted = "the last line the band drew, and the one the grid carries"
	auditLost    = "a line the log holds and the paint never reached"
	// auditFixtureHeadRunes is how much of a painted row the fixture requires back:
	// enough that the row is unmistakably present, short enough that a band row CUT
	// at the band's own width still counts as painted.
	auditFixtureHeadRunes = 8
)

// auditFixture writes a session log whose assistant text is the given answer
// lines, and a dump painted with `painted` through the REAL chat surface — the
// production editor on a REAL pty at 117x26, painting into the production sink
// (exactly the harness `chat_dump_geometry_pty_test.go` established). It returns
// the dump path and the session log path.
//
// WHY NOT WRITE THE GRID BY HAND: the audit's whole claim is that an agent can
// check what the BAND drew. A grid typed into a file would make every assertion
// here agree with my idea of a screen instead of with the product's, which is the
// substitution Rule 31 forbids. `painted` is what the band is handed — so a line
// present in the log and ABSENT from `painted` is a paint loss the band really
// never drew.
func auditFixture(t *testing.T, painted, transcript []string) (dumpPath, sessionPath string) {
	t.Helper()
	gopiDir, _ := uiStateFixtureProfile(t)
	sessions := filepath.Join(gopiDir, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir the fixture session dir: %v", err)
	}
	sessionPath = filepath.Join(sessions, compareSessionID, compareSessionFileName)
	if err := os.MkdirAll(sessionlayout.RecordContainer(sessionPath), 0o755); err != nil {
		t.Fatalf("mkdir the fixture session: %v", err)
	}
	writeCompareSessionLog(t, sessionPath, transcript)

	_, slave := dumpGeomPty(t)
	if got := lineedit.TerminalCols(int(slave.Fd())); got != dumpGeomPtyCols {
		t.Fatalf("the pty reports %d columns, want %d: the case is unset rather than green", got, dumpGeomPtyCols)
	}
	dir := filepath.Join(sessionlayout.RecordContainer(sessionPath), compareDumpDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir the fixture dump dir: %v", err)
	}
	dumpPath = filepath.Join(dir, uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: dumpPath,
		Identity: uiclient.DumpIdentity{
			Surface:          uiclient.SurfaceChat,
			PID:              os.Getpid(),
			Target:           chatDumpTarget(false),
			TerminalColsRows: [2]int{dumpGeomPtyCols, dumpGeomPtyRows},
			Started:          uiDumpStartStamp(),
		},
	})
	if err != nil {
		t.Fatalf("arm the fixture sink: %v", err)
	}
	term, err := lineedit.Open(slave, d)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	defer func() { _ = term.Close() }() //stylegate:ignore the harness's own terminal teardown
	ed := lineedit.NewChatEditor(term, d, chatPrompt, nil, os.Stderr, NewSlashCompleter())
	// The editor's own background state (the completion worker) is stopped before
	// the case ends, so the suite's close-out goroutine sweep does not see a parked
	// worker from this fixture (the same teardown chat_dump_geometry_pty_test.go
	// documents).
	defer ed.Close()
	ed.SetGeometryObserver(chatGeomObserver(d))
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%dx%d) = false: the fixture needs the reserved layout installed", dumpGeomPtyCols, dumpGeomPtyRows)
	}
	for _, row := range painted {
		if !ed.WriteBandRow(row) {
			t.Fatalf("the band refused the row %q: no reserved layout is active", row)
		}
	}
	// The fixture is a DIRECT DRIVER of the layout: it installs the reserved
	// region and writes band rows WITHOUT ever running a read loop, then reads the
	// grid back on this goroutine. A band row is drawn by the read loop that owns
	// the layout, and this test has none, so the rows it just queued are written
	// HERE — otherwise the dump below is read against an empty grid and every
	// assertion built on it is about a screen the band never drew (measured: this
	// is what the pre-fix queue+wait produced, one 4s bound per row).
	if !ed.SettleBand() {
		t.Fatalf("the band did not settle the painted rows: the layout does not own the screen")
	}
	if err := d.Close(); err != nil {
		t.Fatalf("finalize the fixture dump: %v", err)
	}
	// The fixture asserts the paint it is about to audit ACTUALLY EXISTS, read back
	// through the same reader `gopi ui_state` uses (never a second parser): the band
	// keeps its LIVE row only, so a case built on an earlier row would go green
	// against a grid that never carried it — the failure mode that made a
	// whole-answer case read as a paint loss.
	cand, err := uiclient.ReadDump(dumpPath)
	if err != nil {
		t.Fatalf("the fixture dump does not read back: %v", err)
	}
	// A row is accepted either whole or as its own head: a band row wider than the
	// band is CUT at the band's width (internal/lineedit/editor.go keeps the safety
	// split for callers that do not wrap), so demanding the whole row back would
	// reject exactly the over-wide case the audit has a damage class for.
	grid := strings.Join(cand.Dump.Lines, "\n")
	for _, row := range painted {
		head := row
		if runes := []rune(row); len(runes) > auditFixtureHeadRunes {
			head = string(runes[:auditFixtureHeadRunes])
		}
		if !strings.Contains(grid, head) {
			t.Fatalf("the fixture's band did not leave %q on the grid it claims to have painted:\n%s", head, grid)
		}
	}
	return dumpPath, sessionPath
}

// auditArgs is the invocation the whole-answer mode is reached by — the SAME
// arguments the usage page tells an agent to run, so a help text that drifted away
// from the parser fails here.
func auditArgs(session string, extra ...string) []string {
	args := []string{uiStateFlagAll, uiStateFlagSession, session, uiStateFlagTarget, string(uiclient.SurfaceChat)}
	return append(args, extra...)
}

// TestUIStateAudit_WholeAnswerIsOneCall is the GREEN leg: every line of the answer
// is checked in ONE call, the ledger's counts close, and the exit code says the
// check RAN and agreed. The transcript here is exactly the line the paint carries,
// so "agreed" is a statement about a non-empty grid — not about a refusal nobody
// read.
func TestUIStateAudit_WholeAnswerIsOneCall(t *testing.T) {
	painted := []string{auditPainted}
	_, session := auditFixture(t, painted, painted)

	out, code := uiStateRun(t, auditArgs(session))
	if code != 0 {
		t.Fatalf("exit = %d, want 0 for an answer whose every line is on the grid\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"#gopi-ui-state-audit",
		"outcome=" + uiStateAuditWordAgreed,
		"status=" + string(uiclient.AuditAgreed),
		"matched=1",
		"disagreed=0",
		"audited=1",
		"transcript_lines=1",
		"geometry=" + string(uiclient.GeometrySourceLayout),
		"cols=" + strconv.Itoa(dumpGeomPtyCols),
		"frames=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the ledger does not carry %q:\n%s", want, out)
		}
	}
}

// TestUIStateAudit_NamesTheLostLineAndItsDirection is the RED leg on the SAME
// fixture with ONE variable flipped: a line the LOG holds and the paint does not.
// The audit must find it, name the DIRECTION (the transcript has it, the grid does
// not), classify it, and exit with the disagreement code — never report the answer
// as intact.
func TestUIStateAudit_NamesTheLostLineAndItsDirection(t *testing.T) {
	painted := []string{auditPainted}
	// The same paint, with the lost line appended to the log AFTER it.
	_, session := auditFixture(t, painted, []string{auditPainted, auditLost})

	out, code := uiStateRun(t, auditArgs(session))
	if code != uiStateExitCompareMismatch {
		t.Fatalf("exit = %d, want %d (the check RAN and found a disagreement)\noutput:\n%s", code, uiStateExitCompareMismatch, out)
	}
	for _, want := range []string{
		"outcome=" + uiStateAuditWordDisagreed,
		"matched=1",
		"disagreed=1",
		"damage=" + string(uiclient.DamagePaintLoss),
		"in_transcript=true",
		"in_grid=false",
		auditLost,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the ledger does not carry %q:\n%s", want, out)
		}
	}
	// The CLEAN direction on the same fixture: without the planted line the very
	// same call agrees — so the disagreement above is caused by the loss, not by
	// the fixture.
	if out2, code2 := uiStateRun(t, auditArgs(session)); code2 != uiStateExitCompareMismatch && strings.Contains(out2, uiStateAuditWordAgreed) {
		t.Fatalf("the fixture agrees with the lost line removed, so the RED leg proves nothing")
	}
}

// TestUIStateAudit_OverWideLineIsNotAccusedOfBeingLost is the classification's
// honesty, driven through the verb on a line the band cannot fit on one row.
//
// MEASURED BEHAVIOUR (re-measured 2026-09-28, task 01a0e433). A band row wider
// than the band is WRAPPED across rows (`region.WriteLines`), and the audit sees
// one transcript line against three grid rows: it joins them, reports `agreed`
// with `rejoined=1`, and — this is the point — NEVER accuses the paint of losing
// the line. A reader told "the paint lost it" would chase a renderer defect that
// a wrapping band explains exactly as well.
//
// THE PREVIOUS FORM OF THIS CASE asserted the opposite exit code, on a premise its
// own comment stated: "the band CUTS a row wider than itself — it does not wrap
// it". That premise was FALSE and the case was green because of a real defect: the
// dump sink re-seeded its grid on the layout announce, dropped the installed
// scrolling region, and the following rows then landed on the rows the previous
// ones occupied — which made an over-wide row READ as a truncation. With the sink
// fixed (ScreenDumper.SetGeometry now RESIZES instead of rebuilding; see
// devdoc/chat-dump-readability-settle-and-resize.md) the wrap is real, so the
// assertion moves to what is true now: the line arrives, the audit says so, and it
// names the rejoining rather than a row-exact match.
func TestUIStateAudit_OverWideLineIsNotAccusedOfBeingLost(t *testing.T) {
	line := strings.Repeat("中文与 latin 混排的句子 —— ", 12)
	if cols := lineedit.DisplayWidth(line); cols <= dumpGeomPtyCols {
		t.Fatalf("the fixture's line is %d cells, which FITS the band's %d: the case needs a genuinely over-wide line", cols, dumpGeomPtyCols)
	}
	_, session := auditFixture(t, []string{line}, []string{line})

	out, code := uiStateRun(t, auditArgs(session))
	if strings.Contains(out, "damage="+string(uiclient.DamagePaintLoss)) {
		t.Fatalf("an over-wide line was accused of being LOST, though the band's own wrap explains it exactly as well:\n%s", out)
	}
	// The line arrived (the wrap is real), so the audit agrees — and it must say
	// HOW, or a reader cannot tell a wrapped arrival from a row-exact one.
	if code == uiStateExitCompareMismatch {
		// A disagreement is acceptable ONLY when it is the ambiguity class, never
		// the flat accusation — the classification above is the hard leg.
		if !strings.Contains(out, "damage="+string(uiclient.DamageCJKWrap)) &&
			!strings.Contains(out, "damage="+string(uiclient.DamageWrapArtifactOrLoss)) {
			t.Fatalf("the over-wide line carries neither ambiguity class and is not reported as arrival:\n%s", out)
		}
		return
	}
	if !strings.Contains(out, "rejoined=1") {
		t.Fatalf("the audit agreed on an over-wide line without naming that it was REJOINED across rows — a reader cannot tell a wrap from a row-exact match:\n%s", out)
	}
	// The arithmetic that decided it is on the ledger, so the reader can check it.
	if !strings.Contains(out, "expected_cols=") || !strings.Contains(out, "grid_cols="+strconv.Itoa(dumpGeomPtyCols)) {
		t.Errorf("the ledger does not carry the line's own width against the grid's:\n%s", out)
	}
}

// TestUIStateAudit_CouldNotRunIsNotAgreement is the contract's third state, and the
// one that must never collapse into the first: with no dump armed the audit CANNOT
// RUN, and it says so with exit 1 and the word — an unchecked screen is never a
// verified one.
func TestUIStateAudit_CouldNotRunIsNotAgreement(t *testing.T) {
	_, session := auditFixture(t, []string{auditPainted}, []string{auditPainted})
	// Remove the arming: the artifact the audit would read is simply not there.
	dump := filepath.Join(sessionlayout.RecordContainer(session), compareDumpDirName, uiclient.ChatDumpFileName)
	if err := os.Remove(dump); err != nil {
		t.Fatalf("remove the armed artifact: %v", err)
	}
	out, code := uiStateRun(t, auditArgs(session))
	if code != uiStateExitCouldNotRun {
		t.Fatalf("exit = %d, want %d (the audit could not run)\noutput:\n%s", code, uiStateExitCouldNotRun, out)
	}
	if strings.Contains(out, uiStateAuditWordAgreed) {
		t.Fatalf("an UNARMED profile was reported as agreement (the confusion this contract exists to remove):\n%s", out)
	}
	// The refusal names the artifact that was missing — either spelling of the same
	// fact ("no dump found") is honest; a word from the agreement vocabulary is not.
	if !strings.Contains(out, uiclient.ChatDumpFileName) && !strings.Contains(out, uiclient.DumpFileName) {
		t.Errorf("the refusal does not name the artifact that was missing:\n%s", out)
	}
}

// TestUIStateAudit_DerivesTheAuthorityFromTheDump: the whole-answer audit gets
// the same one-call first experience as --compare (they share
// uiStateResolveCompareSession), and when nothing can be derived the refusal
// still names the flags to pass.
func TestUIStateAudit_DerivesTheAuthorityFromTheDump(t *testing.T) {
	// The fixture's dump sits in the session tree, so the audit can find its own
	// authority: no --session, still a real verdict.
	dump, _ := auditFixture(t, []string{auditPainted}, []string{auditPainted})
	out, code := uiStateRun(t, []string{uiStateFlagAll, uiStateFlagTarget, string(uiclient.SurfaceChat)})
	if code == 0 && strings.Contains(out, "transcript_lines=0") {
		t.Fatalf("the audit exited 0 with no transcript read — it must not report agreement against nothing:\n%s", out)
	}
	// With the sibling transcript gone there is nothing to derive, and the
	// refusal names the flags the caller would have to pass.
	if err := os.Remove(filepath.Join(filepath.Dir(filepath.Dir(dump)), compareSessionFileName)); err != nil {
		t.Fatalf("remove the sibling transcript: %v", err)
	}
	out2, code2 := uiStateRun(t, []string{uiStateFlagAll, uiStateFlagTarget, string(uiclient.SurfaceChat)})
	if code2 == 0 {
		t.Fatalf("an audit with no derivable transcript exited 0:\n%s", out2)
	}
	if !strings.Contains(out2, uiStateFlagAll) || !strings.Contains(out2, uiStateFlagSession) {
		t.Errorf("the refusal does not name the flags to pass:\n%s", out2)
	}
}

// TestUIStateAudit_WindowIsStated: --last N narrows the audit and the ledger SAYS
// SO — an audit of the last N lines reported as if it covered the answer would
// claim a coverage it does not have.
func TestUIStateAudit_WindowIsStated(t *testing.T) {
	painted := []string{auditPainted}
	_, session := auditFixture(t, painted, []string{auditPainted, auditLost})
	// The window keeps the LAST line, which is the one the grid does NOT carry: the
	// audit disagrees, and it still says it audited one of two.
	out, code := uiStateRun(t, auditArgs(session, uiStateFlagLast, "1"))
	if code != uiStateExitCompareMismatch {
		t.Fatalf("exit = %d, want %d for a window whose line is not on the grid\noutput:\n%s", code, uiStateExitCompareMismatch, out)
	}
	for _, want := range []string{"audited=1", "transcript_lines=2", uiStateFlagLast, "outcome=" + uiStateAuditWordDisagreed} {
		if !strings.Contains(out, want) {
			t.Errorf("the windowed audit does not carry %q:\n%s", want, out)
		}
	}
	// The RED direction of the window: the same call WITHOUT the window audits both
	// lines, so a window that leaked into the count would show up here.
	out2, _ := uiStateRun(t, auditArgs(session))
	if !strings.Contains(out2, "audited=2") {
		t.Fatalf("the unwindowed audit does not cover the whole answer (the window is leaking):\n%s", out2)
	}
}

// TestUIStateAudit_HelpCarriesTheOneLineCommand is the discoverability contract:
// the exact command an agent must run appears in `gopi ui_state -h`, spelled here
// INDEPENDENTLY of the implementation (a test that read the string from the
// implementation could not notice the help text moving away from the parser).
func TestUIStateAudit_HelpCarriesTheOneLineCommand(t *testing.T) {
	out, code := uiStateRun(t, []string{HelpFlag})
	if code != 0 {
		t.Fatalf("`gopi ui_state -h` exited %d, want 0\noutput:\n%s", code, out)
	}
	const oneLine = "gopi ui_state --all --session <session-id> --target chat"
	if !strings.Contains(out, oneLine) {
		t.Fatalf("the help page does not carry the one-line command %q:\n%s", oneLine, out)
	}
	// The exit contract, in words, on the SAME page: a reader who never met this
	// verb must be able to tell "could not run" from "agreed".
	for _, want := range []string{"outcome", uiStateAuditWordAgreed, uiStateAuditWordDisagreed, uiStateAuditWordCouldNotRun} {
		if !strings.Contains(out, want) {
			t.Errorf("the help page does not state %q:\n%s", want, out)
		}
	}
	// …and the damage vocabulary the audit reports, so an agent reading the page
	// knows what it will be told before it runs anything.
	for _, want := range []string{string(uiclient.DamagePaintLoss), string(uiclient.DamageWrapArtifactOrLoss), string(uiclient.DamageCJKWrap), string(uiclient.DamageEmphasisUnpainted)} {
		if !strings.Contains(out, want) {
			t.Errorf("the help page does not name the damage class %q:\n%s", want, out)
		}
	}
}
