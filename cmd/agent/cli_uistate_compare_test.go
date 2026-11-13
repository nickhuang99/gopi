package main

// cli_uistate_compare_test.go — `gopi ui_state --compare`: the one-call check of
// the authoritative transcript against the PAINTED grid.
//
// The fixture is the PRODUCT's own sink and the PRODUCT's own discovery, driven
// through the verb: a dump written by uiclient.NewScreenDumper into a fixture
// profile, a transcript written as a session log, and the verb's answer asserted
// as the caller sees it (stdout, stderr and the exit code). Nothing here
// re-implements a reader — a test that parsed the artifact itself would agree
// with the code by construction, which is exactly what a contract assertion must
// not do.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/uiclient"
)

// compareLine is the CJK+latin line the grid is painted with and the transcript
// carries. It is longer than a narrow grid would hold on purpose (see
// chat_dump_geometry_pty_test.go's twin case).
const compareLine = "中文与 latin 混排 —— dump 必须逐字符还原。"

// compareOtherLine is a line only ONE side will have, per case.
const compareOtherLine = "a line the model never produced in this session"

// compareSessionID names the fixture session the transcript is read from.
const compareSessionID = "0f0f0f0f-bbbb-4ccc-8ddd-eeeeeeeeeeee"

// compareFixture writes a dump (painted with the given line, at the given
// geometry provenance) and a session log whose assistant text is the transcript,
// both inside the fixture profile the verb discovers. It returns the dump path.
func compareFixture(t *testing.T, painted string, geometry uiclient.GeometrySource, transcriptLines ...string) string {
	t.Helper()
	gopiDir, _ := uiStateFixtureProfile(t)
	sessions := filepath.Join(gopiDir, "sessions", compareSessionID)
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir the fixture session dir: %v", err)
	}
	writeCompareSessionLog(t, filepath.Join(sessions, compareSessionFileName), transcriptLines)

	// The dump: the product's sink at a REAL geometry (117x26 — the width the
	// regression uses), painted through the same Write path production tees.
	dir := filepath.Join(sessions, compareDumpDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir the fixture dump dir: %v", err)
	}
	path := filepath.Join(dir, uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path:     path,
		Cols:     dumpGeomPtyCols,
		Rows:     dumpGeomPtyRows,
		Geometry: geometry,
		Identity: uiclient.DumpIdentity{
			Surface:          uiclient.SurfaceChat,
			PID:              os.Getpid(),
			Target:           chatDumpTarget(false),
			ColsRows:         [2]int{dumpGeomPtyCols, dumpGeomPtyRows},
			TerminalColsRows: [2]int{dumpGeomPtyCols, dumpGeomPtyRows},
			Started:          liveStartStamp(t),
		},
	})
	if err != nil {
		t.Fatalf("arm the fixture sink: %v", err)
	}
	// Paint, wait past the sink's throttle, paint again: the SECOND write is the
	// one the file carries (the sink is change-detecting).
	if _, err := d.Write([]byte("\x1b[2J\x1b[H" + painted + "\r\n")); err != nil {
		t.Fatalf("paint the fixture dump: %v", err)
	}
	//stylegate:allow-sleep lets the fixture's dump sink observe the repaint before the next write; the sink is change-detecting, so a slow host only retries later
	time.Sleep(uiStateFixtureRefreshWait)
	if _, err := d.Write([]byte("\x1b[H" + painted + "\x1b[3;1Hstatus: idle\r\n")); err != nil {
		t.Fatalf("repaint the fixture dump: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("finalize the fixture dump: %v", err)
	}
	return path
}

// The fixture's file names, spelled locally: they are the ARTIFACT contract the
// verb discovers by, and a test that read them from the implementation could not
// notice the contract moving.
const (
	compareSessionFileName = "session.jsonl"
	compareDumpDirName     = "dump"
)

// writeCompareSessionLog writes a minimal session.jsonl carrying the transcript
// as ONE assistant text block — the shape internal/gopilog parses into
// Iteration.TextBlocks, i.e. the same authority `gopi session log <id> --response --text`
// prints.
func writeCompareSessionLog(t *testing.T, path string, lines []string) {
	t.Helper()
	start := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	var b strings.Builder
	b.WriteString(`{"type":"session_start","timestamp":"` + start.Format(time.RFC3339) + `","sessionId":"` + compareSessionID + `","model":"fixture","workdir":"/tmp"}` + "\n")
	b.WriteString(`{"type":"user","timestamp":"` + start.Format(time.RFC3339) + `","message":{"role":"user","content":"fixture question"}}` + "\n")
	// The text is JSON-encoded (never concatenated raw): a CJK line with an em
	// dash must survive the round trip byte-exact, and a hand-built literal is
	// how a fixture quietly stops testing the thing it names.
	text, err := json.Marshal(strings.Join(lines, "\n"))
	if err != nil {
		t.Fatalf("encode the fixture transcript: %v", err)
	}
	b.WriteString(`{"type":"assistant","timestamp":"` + start.Format(time.RFC3339) + `","message":{"role":"assistant","content":[{"type":"text","text":` + string(text) + `}]}}` + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write the fixture session log %s: %v", path, err)
	}
}

// TestUIStateCompare_MatchesThePaintedGridAgainstTheTranscript is the happy path,
// end to end through the verb: a line that is on the grid AND in the transcript
// matches, exit 0.
func TestUIStateCompare_MatchesThePaintedGridAgainstTheTranscript(t *testing.T) {
	path := compareFixture(t, compareLine, uiclient.GeometrySourceLayout, compareLine)
	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, filepath.Join(filepath.Dir(path), "..", compareSessionFileName),
	})
	if code != 0 {
		t.Fatalf("exit = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, string(uiclient.CompareMatch)) {
		t.Fatalf("the answer does not carry %q:\n%s", uiclient.CompareMatch, out)
	}
	if !strings.Contains(out, string(uiclient.GeometrySourceLayout)) {
		t.Fatalf("the answer does not report the grid's geometry provenance:\n%s", out)
	}
	if !strings.Contains(out, "#gopi-ui-state-compare") {
		t.Fatalf("the answer is not the compare record:\n%s", out)
	}
}

// TestUIStateCompare_LocalizesWhichSideLostTheLine is the diagnosis the flag
// exists for: a line only the transcript has is a PAINT/RE-WRAP fact (exit 3),
// and a line only the grid has is a STALE-FRAME fact. Both are reported with the
// longest verbatim prefix on the grid, so a reader can tell a truncation from a
// total absence.
func TestUIStateCompare_LocalizesWhichSideLostTheLine(t *testing.T) {
	path := compareFixture(t, compareLine, uiclient.GeometrySourceLayout, compareLine, compareOtherLine)
	session := filepath.Join(filepath.Dir(path), "..", compareSessionFileName)
	// The dump's line is on BOTH sides; the other transcript line is only in the
	// log — the paint never showed it.
	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, compareOtherLine,
		uiStateFlagSession, session,
	})
	if code != uiStateExitCompareMismatch {
		t.Fatalf("exit = %d, want %d (a line the transcript has and the grid does not)\noutput:\n%s",
			code, uiStateExitCompareMismatch, out)
	}
	if !strings.Contains(out, string(uiclient.CompareMismatch)) {
		t.Fatalf("the answer does not report %q:\n%s", uiclient.CompareMismatch, out)
	}
	if !strings.Contains(out, "in_transcript=true") {
		t.Fatalf("the answer does not say the transcript had it (the direction is the diagnosis):\n%s", out)
	}

	// The other direction: a line the GRID has and the transcript does not.
	out2, code2 := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, compareTranscriptWithout(t, session, compareLine),
	})
	if code2 == 0 {
		t.Fatalf("exit = 0 for a grid-only line, want a mismatch\noutput:\n%s", out2)
	}
	if !strings.Contains(out2, "in_transcript=false") {
		t.Fatalf("the answer does not say the transcript LACKED it:\n%s", out2)
	}

	// A third shape that must NOT be reported as either of the two above: a line
	// NEITHER side has. Blaming a layer here would send a reader hunting a paint
	// defect for a line that is simply not this session's text.
	out3, code3 := uiStateRun(t, []string{
		uiStateFlagCompare, "neither side carries this line",
		uiStateFlagSession, session,
	})
	if code3 == 0 {
		t.Fatalf("exit = 0 for a line present on neither side\noutput:\n%s", out3)
	}
	if !strings.Contains(out3, "in_transcript=false") || !strings.Contains(out3, "in_grid=false") {
		t.Fatalf("the answer mislabels the both-sides-absent case:\n%s", out3)
	}
	if strings.Contains(out3, "the paint lost it") {
		t.Fatalf("a line neither side has was blamed on the paint:\n%s", out3)
	}
}

// compareTranscriptWithout rewrites the fixture session's transcript to lines
// that exclude want, returning the same path — the "the grid carries text this
// session never produced" shape (a stale frame, or another surface's screen).
func compareTranscriptWithout(t *testing.T, sessionPath, want string) string {
	t.Helper()
	raw, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read the fixture session log: %v", err)
	}
	if strings.Contains(string(raw), want) {
		replaced := strings.ReplaceAll(string(raw), want, "some other text")
		if err := os.WriteFile(sessionPath, []byte(replaced), 0o644); err != nil {
			t.Fatalf("rewrite the fixture session log: %v", err)
		}
	}
	return sessionPath
}

// TestUIStateCompare_RefusesAFallbackGeometryAndSaysWhy is the guard that keeps
// the check honest: at a geometry nothing measured, the comparison REFUSES rather
// than answering containment at the wrong width — the false signal this change
// exists to remove. The refusal is on stderr (it is a refusal, not a screen) and
// the reason is the fallback notice verbatim.
func TestUIStateCompare_RefusesAFallbackGeometryAndSaysWhy(t *testing.T) {
	path := compareFixture(t, compareLine, uiclient.GeometrySourceFallback, compareLine)
	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, filepath.Join(filepath.Dir(path), "..", compareSessionFileName),
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a refused compare\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, string(uiclient.CompareRefused)) {
		t.Fatalf("the answer does not report %q:\n%s", uiclient.CompareRefused, out)
	}
	if !strings.Contains(out, "FALLBACK") {
		t.Fatalf("the refusal does not name the fallback:\n%s", out)
	}
}

// TestUIStateCompare_RequiresAnAuthorityAndBoundsItsInput pins the two refusals
// TestUIStateCompare_DerivesTheAuthorityFromTheDump pins the ONE-CALL first
// experience: `--compare "<line>"` with NO --session must RUN, because the dump
// knows which session wrote it (the transcript sits one level above the dump
// directory). Naming the authority by hand stays the override, not a
// prerequisite — the old behaviour refused and sent the caller back for a second
// call, which is precisely the ceremony the operator's "one call" is about.
func TestUIStateCompare_DerivesTheAuthorityFromTheDump(t *testing.T) {
	path := compareFixture(t, compareLine, uiclient.GeometrySourceLayout, compareLine)
	out, code := uiStateRun(t, []string{uiStateFlagCompare, compareLine})
	if code != 0 {
		t.Fatalf("exit = %d for a compare with no --session, want 0 (the dump's own session is the authority)\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, compareSessionID) {
		t.Fatalf("the derived answer does not name the session it checked against:\n%s", out)
	}
	// The derivation must READ THAT SESSION, not merely resolve a path: the
	// line is on both sides, and a wrong authority would report no-transcript.
	if !strings.Contains(out, string(uiclient.CompareMatch)) {
		t.Fatalf("the compare did not report a match against the derived transcript:\n%s", out)
	}
	_ = path

	// ...and when there is NOTHING to derive from, the refusal still names the
	// flag (Rule 15: the derivation falls back to a refusal, never to a
	// comparison against nothing).
	sessions := filepath.Join(filepath.Dir(filepath.Dir(path)), compareSessionFileName)
	if err := os.Remove(sessions); err != nil {
		t.Fatalf("remove the sibling transcript: %v", err)
	}
	out2, code2 := uiStateRun(t, []string{uiStateFlagCompare, compareLine})
	if code2 != 1 {
		t.Fatalf("exit = %d for a compare whose authority cannot be derived, want 1\noutput:\n%s", code2, out2)
	}
	if !strings.Contains(out2, uiStateFlagSession) {
		t.Fatalf("the refusal does not name the missing flag:\n%s", out2)
	}
}

// TestUIStateCompare_RefusesABlankLine keeps the input bound the derivation does
// not touch: a blank line is inside every row, so an "it matched" answer for it
// would be vacuous — refused, not skipped.
func TestUIStateCompare_RefusesABlankLine(t *testing.T) {
	compareFixture(t, compareLine, uiclient.GeometrySourceLayout, compareLine)
	out, code := uiStateRun(t, []string{uiStateFlagCompare, compareLine + "\n\n" + compareLine, uiStateFlagSession, compareSessionID})
	if code != 1 {
		t.Fatalf("exit = %d for a blank compare line, want 1\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "blank") {
		t.Fatalf("the refusal does not explain the blank line:\n%s", out)
	}
}

// TestUIStateCompare_JSONCarriesTheSameFacts is the twin contract: the --json
// rendering carries the per-line verdicts, the geometry and the derived match
// flag, so a script never has to scrape the text form.
func TestUIStateCompare_JSONCarriesTheSameFacts(t *testing.T) {
	path := compareFixture(t, compareLine, uiclient.GeometrySourceLayout, compareLine)
	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, filepath.Join(filepath.Dir(path), "..", compareSessionFileName),
		uiStateFlagJSON,
	})
	if code != 0 {
		t.Fatalf("exit = %d, want 0\noutput:\n%s", code, out)
	}
	var rec uiStateCompareRecord
	if err := json.Unmarshal([]byte(extractJSONObject(out)), &rec); err != nil {
		t.Fatalf("the JSON rendering is not the compare envelope: %v\noutput:\n%s", err, out)
	}
	if rec.Tool != CliVerbUIState {
		t.Errorf("tool = %q, want %q", rec.Tool, CliVerbUIState)
	}
	if !rec.Match {
		t.Errorf("match = false for a line present on both sides\noutput:\n%s", out)
	}
	if len(rec.Verdicts) != 1 || rec.Verdicts[0].Status != uiclient.CompareMatch {
		t.Fatalf("verdicts = %+v, want one %s", rec.Verdicts, uiclient.CompareMatch)
	}
	if rec.Verdicts[0].Geometry != uiclient.GeometrySourceLayout {
		t.Errorf("the verdict's geometry = %q, want %q", rec.Verdicts[0].Geometry, uiclient.GeometrySourceLayout)
	}
	if rec.TranscriptLines == 0 {
		t.Errorf("transcript_lines = 0: the authority was never read")
	}
}

// TestUIStateCompare_TextHeaderSeparatesNeverReadFromReadAndEmpty is the
// reporting-honesty contract of the compare's TEXT header — the shape a human and
// an agent actually read (the JSON envelope was never the misleading half).
//
// MEASURED DEFECT (task 01a0de1a, from the run that filed it): the header printed
// `session=%s transcript_lines=0 match=false` for BOTH "the authority was read and
// held no lines" and "the authority was NEVER read" — the two printed
// byte-identically, and the ONE field that told them apart (`session_file`) lived
// only in the JSON envelope. A reader of the text form therefore could not tell
// "the log is empty" from "nobody looked at it", and the honest reading of a
// never-read compare is exactly what this case pins.
//
// The two vocabulary strings below are spelled LITERALLY, not read from the
// implementation: they are the text contract an operator reads, and a test that
// took them from the code would agree with the code by construction.
func TestUIStateCompare_TextHeaderSeparatesNeverReadFromReadAndEmpty(t *testing.T) {
	const (
		headerPrefix  = "#gopi-ui-state-compare"
		sessionFileKw = "session_file"
		notReadWord   = "not-read"
	)

	// (1) NEVER READ. A grid that cannot be read as the screen's layout (a FALLBACK
	// geometry) is refused BEFORE the transcript is fetched, so the authority is
	// genuinely not read — this is the shape the defect printed as
	// `transcript_lines=0` with nothing else to go on.
	fallback := compareFixture(t, compareLine, uiclient.GeometrySourceFallback, compareLine)
	unreadOut, unreadCode := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, compareTranscriptPath(fallback),
	})
	if unreadCode == 0 {
		t.Fatalf("exit = 0 for a refused compare:\n%s", unreadOut)
	}
	unreadHeader := compareHeaderLine(t, unreadOut, headerPrefix)
	if got := compareHeaderField(t, unreadHeader, sessionFileKw); got != notReadWord {
		t.Fatalf("the never-read header does not mark the authority as unread: %s=%q, want %q\nheader:\n%s",
			sessionFileKw, got, notReadWord, unreadHeader)
	}

	// (2) READ AND EMPTY. A real transcript file that exists, resolves AND holds
	// zero assistant lines: the authority WAS read. Its header must say so with the
	// path — the same fact the JSON envelope's session_file carries.
	empty := compareFixture(t, compareLine, uiclient.GeometrySourceLayout)
	session := compareTranscriptPath(empty)
	emptyOut, _ := uiStateRun(t, []string{uiStateFlagCompare, compareLine, uiStateFlagSession, session})
	emptyHeader := compareHeaderLine(t, emptyOut, headerPrefix)
	if got := compareHeaderField(t, emptyHeader, sessionFileKw); got != session {
		t.Fatalf("the read-and-empty header does not name the file it read: %s=%q, want %q\nheader:\n%s",
			sessionFileKw, got, session, emptyHeader)
	}
	if !strings.Contains(emptyHeader, "transcript_lines=0") {
		t.Fatalf("the fixture no longer produces an empty transcript — the case proves nothing:\n%s", emptyHeader)
	}

	// (3) THE DEFECT ITSELF: the two headers must not be the same line. Before the
	// fix they were byte-identical apart from nothing at all — both read
	// `... session=<the same typed path> transcript_lines=0 match=false`.
	if unreadHeader == emptyHeader {
		t.Fatalf("a never-read compare and a read-and-empty compare print the SAME header:\n%s", unreadHeader)
	}

	// (4) …and the text form agrees with the machine form: the JSON envelope's own
	// session_file is the value the text header printed.
	jsonOut, jsonCode := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, session,
		uiStateFlagJSON,
	})
	var rec uiStateCompareRecord
	if err := json.Unmarshal([]byte(extractJSONObject(jsonOut)), &rec); err != nil {
		t.Fatalf("the JSON rendering is not the compare envelope: %v\noutput:\n%s", err, jsonOut)
	}
	if rec.SessionFile != compareHeaderField(t, emptyHeader, sessionFileKw) {
		t.Fatalf("the text header and the JSON envelope disagree about the authority: text=%q json=%q",
			compareHeaderField(t, emptyHeader, sessionFileKw), rec.SessionFile)
	}
	if jsonCode != 0 && rec.Match {
		t.Fatalf("exit = %d with match=true:\n%s", jsonCode, jsonOut)
	}
}

// compareTranscriptPath names the transcript that sits BESIDE a fixture dump
// (<session dir>/dump/<dump file> → <session dir>/session.jsonl). It is the
// ABSOLUTE, cleaned path (never one carrying ".."), because the verb prints the
// path the caller typed and a header assertion against a relative spelling would
// be asserting the test's own argv.
func compareTranscriptPath(dumpPath string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(dumpPath)), compareSessionFileName)
}

// compareHeaderLine returns the compare record's header line — the one carrying
// the provenance fields — from the verb's own stdout.
func compareHeaderLine(t *testing.T, out, prefix string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	t.Fatalf("the output carries no compare header (%q):\n%s", prefix, out)
	return ""
}

// compareHeaderField returns the value of one `name=value` token on the header
// line, so a case asserts the FIELD rather than the whole line's spelling.
func compareHeaderField(t *testing.T, header, name string) string {
	t.Helper()
	prefix := name + uiStateFlagSep
	for _, tok := range strings.Fields(header) {
		if strings.HasPrefix(tok, prefix) {
			return strings.TrimPrefix(tok, prefix)
		}
	}
	t.Fatalf("the compare header carries no %s field:\n%s", name, header)
	return ""
}

// extractJSONObject returns the first '{' … last '}' span of out, so a rendering
// that prints a header line before the object still parses (the compare record is
// the object; anything else on stdout would be a defect this finds).
func extractJSONObject(out string) string {
	i := strings.Index(out, "{")
	j := strings.LastIndex(out, "}")
	if i < 0 || j <= i {
		return out
	}
	return out[i : j+1]
}

// TestUIStateCompare_SessionIDIsResolvedLikeLogsOwnLookup pins the id form of
// --session: a bare id must resolve through gopilog's own session lookup (the
// same one `gopi session log <id>` uses), not be treated as a path.
func TestUIStateCompare_SessionIDIsResolvedLikeLogsOwnLookup(t *testing.T) {
	compareFixture(t, compareLine, uiclient.GeometrySourceLayout, compareLine)
	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, compareLine,
		uiStateFlagSession, compareSessionID,
	})
	if code != 0 {
		t.Fatalf("exit = %d for --session <bare id>, want 0 — the id must resolve through the profile's session lookup\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, compareSessionID) {
		t.Fatalf("the answer does not name the resolved session file:\n%s", out)
	}
}

// compareGridRowGuard keeps the comparison surface honest: the grid rows the
// compare searches must be the dump's own Lines rendering — a CJK line is TWO
// cells per wide rune, so a reader that compared the cell-exact Grid rows would
// report a mismatch for a line that is on screen verbatim. This asserts the
// property directly on the artifact rather than trusting the comment.
func TestUIStateCompare_GridRowsAreTheContinuationFreeRendering(t *testing.T) {
	path := filepath.Join(t.TempDir(), uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path:     path,
		Cols:     dumpGeomPtyCols,
		Rows:     dumpGeomPtyRows,
		Geometry: uiclient.GeometrySourceLayout,
		Identity: uiclient.DumpIdentity{Surface: uiclient.SurfaceChat, PID: os.Getpid(), Started: liveStartStamp(t)},
	})
	if err != nil {
		t.Fatalf("arm the sink: %v", err)
	}
	if _, err := d.Write([]byte("\x1b[2J\x1b[H" + compareLine + "\r\n")); err != nil {
		t.Fatalf("paint: %v", err)
	}
	//stylegate:allow-sleep lets the fixture's dump sink observe the repaint before the next write; the sink is change-detecting, so a slow host only retries later
	time.Sleep(uiStateFixtureRefreshWait)
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	cand, err := uiclient.ReadDump(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	found := false
	for _, l := range cand.Dump.Lines {
		if l == compareLine {
			found = true
		}
	}
	if !found {
		t.Fatalf("the dump's Lines do not carry the CJK+latin line verbatim; lines:\n%s",
			strings.Join(cand.Dump.Lines, "\n"))
	}
	// …and the cell-exact Grid rows are NOT what the compare searches: the wide
	// rune's continuation column is a column, not a character.
	wideRuneCells := false
	for _, g := range cand.Dump.Grid {
		if g == compareLine {
			wideRuneCells = true
		}
	}
	if wideRuneCells {
		t.Fatalf("the cell-exact grid row equals the text: the fixture no longer exercises the wide-rune continuation (cols=%d)", cand.Dump.Cols)
	}
}
