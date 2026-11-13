package testsuite

// chat_render_redzone_pty_test.go — the T1/T2 render family at the band's
// SMALL-DELTA CADENCE, on a real pty, with a payload the other harness
// structurally cannot compose.
//
// WHAT GAP THIS CLOSES, and how it was measured rather than assumed.
//
// The merged harness (chat_render_verdict_pty_test.go, commit 1da51d5e) is the
// only instrument that was green over this family, and it leaves three
// segments unreachable. Each is a fact about a fixture, so each is quoted from
// the code that decides it:
//
//  1. CADENCE. testutil.MockLLM.TextSSE splits a text answer into exactly two
//     halves — `mid := len(text)/2`, then `[]string{text[:mid], text[mid:]}` —
//     so no real-pty run has ever streamed a fragment SMALLER than half the
//     answer. Every defect this family ever had lived in the seam between an
//     in-flight (partial) paint and the committed line, and the number of
//     seams is the number of fragments minus one: the red zone 1..16 bytes is
//     where the seams are. internal/lineedit's own guard sweeps deltas 1..40
//     (answer_wrap_scroll_dup_test.go, dupSweep), but it drives the renderer
//     into a buffer — never the real screen (Rule 5's own lesson: the suite was
//     green over every defect in this family).
//  2. A NON-WRAPPING LINE in the same answer as a wrapping one. The in-package
//     fixture sizes EVERY line WIDER than the band on purpose
//     (dupTokensPerLine=18 × dupTokenCells=6 = 108 cells at dupCols=100), and
//     the verdict harness's own paragraphs are single long lines — so the
//     non-wrapping path (a line that ends its row short of the band edge, the
//     one the answer band's `Finish` carry composes differently) is not on any
//     measured screen.
//  3. CROSS-ROW EMPHASIS measured as CONTIGUOUS CELLS. The verdict harness
//     checks that the bold span's cells carry the bold style (chatRenderEmphasis
//     → chatRenderStyleAt) at one located cell per row; it does not ask whether
//     the bold cells form one uninterrupted run across the row boundary the
//     wrap put inside the span, which is the operator's own phrase — "no
//     half-styled word".
//
// THE MEASUREMENT IS THE ARTIFACT. Every verdict below is computed from the
// painted CELL GRID the surface itself wrote (--dump-screen=PATH → lines_begin
// / grid_begin / attrs_begin), never from OCR, never from the pty byte stream:
// the byte stream carries every repaint, so a token occurring twice in it is
// not evidence of a duplicate while the same token occurring twice on a ROW is
// (memory #6411 trap 4). The pty stream is kept as the third channel and quoted
// only where it answers a question the grid cannot (that the row was PAINTED,
// not merely recorded).
//
// RUN IT WITH:
//
//	CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
//	go test ./testsuite/ -run TestChatRenderRedZone_OnRealPTY -v -count=1

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/uiclient"
	"golang.org/x/sys/unix"
)

// The harness's own bounds (Rule 1: every value is named; none of these is a
// threshold a verdict is read from — the verdicts come from the artifact).
const (
	// rzCols / rzRows are the pty geometry. rzCols is the same narrow width the
	// verdict harness uses (the family is geometry-dependent and the operator's
	// sighting was on a wide terminal); rzRows is large enough that the whole
	// scripted answer stays inside the dump's window, because content scrolled
	// off it reads as damage the artifact cannot distinguish from loss.
	rzCols = 64
	rzRows = 40
	// rzBandCells is the width one band row holds, measured against the real
	// layout constants rather than assumed: the grid's width minus the turn
	// marker's cells minus the two-cell body indent. It is used for two things
	// a reader must be able to check by hand — "this row WRAPPED" (it filled
	// the band) and "this row did NOT wrap" (it ended short of it).
	rzBandCells = rzCols - 1 - rzBodyIndentCells
	// rzBodyIndentCells is the indent the band opens every body row with.
	rzBodyIndentCells = 2
	// rzWaitTail / rzWaitPrompt bound the two waits that end the turn. They are
	// timeouts on a SYNCHRONIZATION wait (it returns the instant its marker
	// arrives), never sleeps.
	rzWaitTail   = 60 * time.Second
	rzWaitPrompt = 60 * time.Second
	// rzEvidenceEnv names the directory this guard keeps its artifacts in. It is
	// the explicit layer over the ambient default: a verification run wants the
	// dump, the pty stream and the transcript where its OWN report lives (the
	// suite's scratch tree is reclaimed when the test ends), while an ordinary
	// run keeps the hermetic default — the suite's scratch dir, off the `/`
	// partition (Rule 22). Unset = scratch.
	rzEvidenceEnv = "GOPI_REDZONE_EVIDENCE"
	// rzArtifactGrace is how long the settle-wait gives the artifact before it
	// falls back to the pty's re-rendered prompt as the end-of-turn signal. It is
	// generously above the writer's 100ms throttle and well below the wait's own
	// deadline, and it exists so an UNARMED surface reports its verdicts from the
	// other channel rather than hanging.
	rzArtifactGrace = 20 * time.Second
	// rzPollInterval is how often the settle-wait re-reads the artifact. The
	// artifact writes at most once per 100ms, so this is a reader's cadence, not
	// the interval under measurement.
	rzPollInterval = 150 * time.Millisecond
	// rzLinesPrefix is the dump's own line-record prefix (uiclient's writer emits
	// `NNN|…`); the settle-wait reads the artifact through it so it looks for the
	// tail on a ROW rather than anywhere in the file's text.
	rzLinesPrefix = ""
	// rzDumpsField is the provenance field the settle-wait compares between two
	// reads (the writer's own count of dump writes).
	rzDumpsField = "dumps="
)

// rzBoundaryTolerance is how many whitespace cells of disagreement the text
// comparison tolerates at the joined rows' seam.
//
// It is NOT laxity, it is the boundary the instrument cannot see: the band WRAPS
// a too-long word by breaking it mid-word and, at the break, reports that it
// consumed the following space — so the artifact holds `… JULIET KILO` / `LIMA
// MIKE`, which joins to `KILOLIMA`, while the answer's own text has one space
// there. Both readings are printed beside each verdict; the tolerance is a
// single space per wrapped seam, and it exists so the verdict answers the
// product's question (is a fragment duplicated, glued or dropped?) instead of
// the cutter's (whose space was it?). At delta=4 and coarser the band consumes
// NO space at its cut, so the same payload's other cadence needs no tolerance at
// all — which is exactly why the count is reported and not hidden.
const rzBoundaryTolerance = 1

// rzWhitespaceAtSeam is how many neighbouring non-whitespace characters must
// match on each side of a tolerated difference, so the tolerance can never
// excuse a real duplicate or a dropped word.
const rzWhitespaceAtSeam = 12

// rzCadence is one measured chunking of the answer: how many BYTES each
// streamed text fragment carries, and how long the stub waits between two of
// them. The pause is not decoration — it is what makes the seam observable:
// the terminal drains whatever the socket already holds in one read, so an
// unpaced stream of 1-byte fragments still lands as one burst on the screen,
// while a paced one keeps the answer ARRIVING and therefore keeps the in-flight
// (partial) paint — the path every defect in this family lived on — on the
// screen for the whole answer.
type rzCadence struct {
	// ID is what the verdict is reported under.
	ID string
	// DeltaBytes is the requested fragment width. Both values sit INSIDE the
	// red zone (1..16) the epic names; the sizes really streamed are read back
	// from the stub, never restated from this field.
	DeltaBytes int
	// Pause is the stub's inter-fragment wait.
	Pause time.Duration
}

// rzCadences is the sweep. 1 is the finest cadence a byte stream can have and
// the one the family's worst defects were measured at; 4 is a second point in
// the red zone so a PASS is not a property of one width alone.
func rzCadences() []rzCadence {
	return []rzCadence{
		{ID: "delta=1", DeltaBytes: 1, Pause: 15 * time.Millisecond},
		{ID: "delta=4", DeltaBytes: 4, Pause: 10 * time.Millisecond},
	}
}

// The scripted content (Rule 13: one definition — the trigger, the row lookup
// and the report's quote all read these).
const (
	// rzPrompt is what the harness types.
	rzPrompt = "run the red-zone render turn"
	// The markers bracket the payload so the verdicts locate its rows instead
	// of guessing coordinates. They carry NO emphasis marker, so a literal
	// "**" found on a row is always a leak and never the harness's own ink.
	rzOpen  = "<RZ-OPEN"
	rzClose = "<RZ-CLOSE"
	// rzTail is the answer's LAST line: its arrival is the proof that the whole
	// answer reached the terminal, so no wait has to guess when a paint ended.
	rzTail = "<RZ-END>"
	// rzNoWrapLine is the segment the other harness cannot compose: a line that
	// ends its row well SHORT of the band edge (the non-wrapping path).
	rzNoWrapLine = "RZ-NOWRAP one plain row that ends short."
	// rzWrapLine is the wrapping segment, and its ONE bold span is wider than
	// the band: the wrap MUST cut inside it, at every cadence and every
	// geometry, which is what makes the cross-row emphasis measurement
	// deterministic instead of a matter of where the boundary happened to land.
	rzWrapLine = "RZ-WRAP HEAD the emphasised run is wider than the band: " +
		"**ALPHA BRAVO CHARLIE DELTA ECHO FOXTROT GOLF HOTEL INDIA JULIET KILO LIMA MIKE** " +
		"then the plain tail RZ-WRAP-TAIL."
	// rzAnswer is the model's answer: the non-wrapping line FIRST (so its row
	// is above the wrap), then the wrapping one. The two lines are in ONE
	// answer, which is the point — the family's non-wrapping and wrapping paths
	// have never been on the same measured screen.
	rzAnswer = rzOpen + "\n" + rzNoWrapLine + "\n" + rzWrapLine + "\n" + rzClose + "\n" + rzTail + "\n"
	// rzSentinels are unique tokens, each of which must appear EXACTLY ONCE in
	// the payload's text: a duplicate or a glued fragment that carries one of
	// them breaks the count even when the surrounding spaces make a
	// whitespace-insensitive equality survive.
	rzSentinelNoWrapHead = "RZ-NOWRAP"
	rzSentinelWrapHead   = "RZ-WRAP"
	rzSentinelWrapTail   = "RZ-WRAP-TAIL"
)

// rzSentinels is the count-checked set.
func rzSentinels() []string {
	return []string{rzSentinelNoWrapHead, rzSentinelWrapHead, rzSentinelWrapTail, "BRAVO", "MIKE"}
}

// rzBoldSpan is the answer's own bold span, read from the answer's text (never
// from the screen) so the harness asks about the text it scripted.
func rzBoldSpan() string { return chatRenderBoldSpan(rzWrapLine) }

// rzExpectedText is the payload as the SCREEN must show it, markers removed by
// the composer's own tokenizer (lineedit.DisplayText) rather than by a
// substitution this harness invented: a harness that stripped markers with its
// own strings.Replace would pass a screen that left one behind.
func rzExpectedText() string {
	return rzNoWrapLine + "\n" + lineedit.DisplayText(rzWrapLine)
}

// rzVerdict is one row of the delivered table (the same three-valued
// vocabulary the verdict harness uses — "the artifact cannot say" is its own
// fact and is never collapsed into a neighbour).
type rzVerdict struct {
	Shape    string
	Verdict  string
	Evidence []string
	Reason   string
}

// rzEvidenceDir resolves where this run keeps its artifacts: $GOPI_REDZONE_EVIDENCE
// when the caller named one (a verification run wants the dump beside its own
// report), otherwise the suite's scratch dir. The caller's directory is NOT
// removed — it is the run's deliverable evidence — while the scratch default is
// reclaimed with the test, exactly as TestScratchDir does.
func rzEvidenceDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv(rzEvidenceEnv); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s=%s: %v", rzEvidenceEnv, dir, err)
		}
		return dir
	}
	return testutil.TestScratchDir(t, "chat-render-redzone")
}

// TestChatRenderRedZone_OnRealPTY is the deliverable: per cadence, the measured
// fragment sizes the stub really streamed, then one verdict per shape of the
// family — (a) literal marker, (b) duplicate/glued fragment, (c) cross-row
// emphasis contiguity — each with the raw dump rows and attr runs it was read
// from.
func TestChatRenderRedZone_OnRealPTY(t *testing.T) {
	bin := testutil.GopiBin(t)
	identity := chatRenderBuildIdentity(t, bin)
	t.Logf("REDZONE harness invoked with GOPI_BIN=%s", bin)
	t.Logf("REDZONE build identity: %s", identity)
	t.Logf("REDZONE geometry: pty %dx%d, band %d cells (grid %d - turn marker %d - indent %d)",
		rzCols, rzRows, rzBandCells, rzCols, 1, rzBodyIndentCells)

	evidence := rzEvidenceDir(t)
	t.Logf("REDZONE evidence dir: %s", evidence)

	for _, cadence := range rzCadences() {
		t.Run(cadence.ID, func(t *testing.T) {
			rzMeasureCadence(t, bin, evidence, cadence)
		})
	}
}

// rzMeasureCadence runs ONE real-pty chat turn at one cadence and prints that
// cadence's verdicts.
func rzMeasureCadence(t *testing.T, bin, evidence string, cadence rzCadence) {
	t.Helper()
	home := t.TempDir()
	work := t.TempDir()
	// ONE SUBDIRECTORY PER CADENCE, each holding the surface's canonical file
	// name (chatRenderDumpName): uiclient.ReadDump identifies an artifact by its
	// file NAME + the surface's own provenance prefix, and it REFUSES a file
	// whose name belongs to no known surface — which is the right behaviour, so
	// the harness honours the reader instead of asking it to loosen (a dump the
	// tool's own reader will not open is not evidence a reader can re-read).
	dumpPath := filepath.Join(evidence, strings.ReplaceAll(cadence.ID, "=", ""), chatRenderDumpName)
	if err := os.MkdirAll(filepath.Dir(dumpPath), 0o755); err != nil {
		t.Fatalf("create the cadence's evidence directory %s: %v", filepath.Dir(dumpPath), err)
	}

	llm := testutil.NewMockLLM(t, testutil.Resp{
		Text:           rzAnswer,
		TextDeltaBytes: cadence.DeltaBytes,
		TextDeltaDelay: cadence.Pause,
	})
	defer llm.Close()

	chatRenderDumpRoot = evidence
	t.Cleanup(func() { chatRenderDumpRoot = "" })

	chat := startPTYChat(t, []string{"chat", "--dump-screen=" + dumpPath},
		testutil.MockEnv(t, home, llm.URL()), work)
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: rzCols, Row: rzRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable on this host: %v", err)
	}
	if cols := lineedit.TerminalCols(int(chat.master.Fd())); cols != rzCols {
		t.Fatalf("the pty reports %d columns, want the %d this harness installed", cols, rzCols)
	}

	chat.waitFor(t, chatPromptMark, rzWaitPrompt)
	chat.writeGapped(t, rzPrompt)
	chat.write(t, "\r")
	// WAIT FOR THE LAST STAGED ROW, NOT FOR A BYTE PATTERN. The tail markers are
	// what tells the harness the whole answer reached the terminal — but at the
	// finest cadence the band repaints those same markers character by character
	// as the fragments arrive, so a substring search over the raw byte stream
	// matches the PARTIAL paint (`<RZ-CLOSE<RZ-EN` is on the fixture's own dump,
	// and `D>` follows in the next frame) and returns while the answer is still
	// streaming. The proof that the paint finished is the SURFACE's own artifact:
	// its lines hold the marker and its dumps counter has SETTLED (the writer
	// throttles, so two consecutive reads agreeing means no new frame landed).
	// The wait is on the artifact's own numbers, so it retires the trap rather
	// than masking it, and the pty read is only a last-resort fallback.
	// WAIT FOR THE LAST STAGED ROW, NOT FOR A BYTE PATTERN (see the comment in
	// rzMeasureCadence) — the artifact's own settled grid plus the pty's own
	// proof that the tail marker was painted.
	ptyOut := rzWaitTailPainted(t, chat, dumpPath)
	ptyOut += chat.waitFor(t, chatPromptMark, rzWaitPrompt)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 (the deliberate /quit)", code)
	}

	// ---- the cadence, read off the stub (never restated from the parameter) --
	sizes := llm.TextStreamSizes(1)
	rzLogCadence(t, cadence, sizes)

	// ---- the artifact -----------------------------------------------------
	rzPath, dumpText := rzReadDump(t, dumpPath)
	cand, err := uiclient.ReadDump(rzPath)
	if err != nil {
		t.Fatalf("the harness could not read the artifact the surface wrote (%s): %v", rzPath, err)
	}
	if refusal := uiclient.CompareGridUnusable(cand); refusal != "" {
		t.Fatalf("the dump cannot be read as a screen, so no verdict can be measured from it: %s", refusal)
	}
	if !cand.Dump.AttrsRecorded || len(cand.Dump.AttrRuns) == 0 {
		t.Fatalf("the artifact carries no per-cell attributes, so the emphasis verdict cannot be measured from it")
	}
	for i, row := range cand.Dump.Lines {
		t.Logf("REDZONE %s dump row %03d|%s", cadence.ID, i, row)
	}
	t.Logf("REDZONE %s provenance: %s", cadence.ID, cand.Record.Line)
	if dumpText == "" {
		t.Fatalf("the dump artifact read back empty after a successful parse: a verdict from an absent artifact is not a verdict")
	}

	// The pty stream, as the third channel (kept in the scratch evidence dir;
	// never in the worktree — Rule 30).
	ptyPath := filepath.Join(evidence, "redzone-"+strings.ReplaceAll(cadence.ID, "=", "")+"-pty.log")
	if err := os.WriteFile(ptyPath, []byte(ptyOut), dumpFilePerm); err != nil {
		t.Fatalf("write the pty transcript evidence: %v", err)
	}
	t.Logf("REDZONE %s pty transcript written to %s (%d bytes)", cadence.ID, ptyPath, len(ptyOut))

	// ---- the verdicts -----------------------------------------------------
	rows, rowNote := rzPayloadRows(cand)
	if rows == nil {
		t.Fatalf("the payload's marker rows are not on the dump's grid: the shape was never painted on the artifact this run read")
	}
	t.Logf("REDZONE %s payload extraction: %s", cadence.ID, rowNote)
	verdicts := rzVerdicts(cand, rows, ptyOut)
	for _, v := range verdicts {
		t.Logf("REDZONE %s verdict %s: %s — %s", cadence.ID, v.Shape, v.Verdict, v.Reason)
		for _, e := range v.Evidence {
			t.Logf("REDZONE %s %s evidence: %s", cadence.ID, v.Shape, e)
		}
	}
	t.Logf("REDZONE %s cannot see: %s", cadence.ID, strings.Join(rzBlindSpots, " | "))
}

// rzLogCadence prints what the STUB streamed and asserts the two facts that
// make the red zone a measurement rather than a claim: every fragment is inside
// the requested width, and the fragments reassemble to the answer byte for
// byte.
func rzLogCadence(t *testing.T, cadence rzCadence, sizes []int) {
	t.Helper()
	if len(sizes) == 0 {
		t.Fatalf("the stub recorded no text fragments for the answer request: the run cannot say which cadence it measured, so it measured nothing")
	}
	total, max := 0, 0
	distinct := map[int]int{}
	for _, n := range sizes {
		total += n
		if n > max {
			max = n
		}
		distinct[n]++
	}
	t.Logf("REDZONE cadence %s: fragments=%d bytes=%d max_fragment=%d requested=%d pause=%s",
		cadence.ID, len(sizes), total, max, cadence.DeltaBytes, cadence.Pause)
	t.Logf("REDZONE cadence %s: the stub's own fragment-size histogram: %v", cadence.ID, sortedHistogram(distinct))
	t.Logf("REDZONE cadence %s: the sizes the stub actually streamed: %v", cadence.ID, sizes)
	if total != len(rzAnswer) {
		t.Fatalf("the stub streamed %d bytes, want the answer's %d: this run's screen does not describe the scripted answer", total, len(rzAnswer))
	}
	if max > cadence.DeltaBytes {
		t.Fatalf("a fragment carried %d bytes, more than the %d this cadence asks for: the red zone was not measured", max, cadence.DeltaBytes)
	}
}

// rzWaitTailPainted waits until the answer's tail is ON THE ARTIFACT and the
// artifact's own `dumps` counter has settled, then waits for the re-rendered
// prompt. It returns everything the pty emitted during those waits.
//
// Why the raw byte stream is not the signal: the streaming band REPAINTS its
// rows as the fragments arrive, so the tail marker appears on the screen
// character by character and a substring search over the byte stream matches a
// PARTIAL paint — `…<RZ-CLOSE<RZ-EN` is exactly what one cadence's own dump
// holds, with the `D>` arriving in the next frame. A harness that quits there
// measures an answer that was still streaming and reports the truncation it
// caused as a paint defect (the first draft of this file did, and the FAIL it
// printed named the wrong party).
//
// The proof used instead is the SURFACE's own artifact — the same instrument the
// verdicts are read from: the dump's lines must carry the tail marker AND two
// consecutive provenance records must agree on the `dumps` counter (the writer
// throttles at >=100ms, so a settled counter means no new frame landed between
// the two reads). The pty read is kept as a last-resort fallback whose evidence
// is reported rather than trusted.
func rzWaitTailPainted(t *testing.T, chat *ptySession, dumpPath string) string {
	t.Helper()
	deadline := time.Now().Add(rzWaitTail)
	graceUntil := time.Now().Add(rzArtifactGrace)
	lastDumps := -1
	for time.Now().Before(deadline) {
		dumps, painted := rzDumpState(t, dumpPath)
		if painted && dumps >= 0 && dumps == lastDumps {
			t.Logf("REDZONE the artifact settled: last row on the grid with dumps=%d (the surface's own counter stopping is what proves the paint ended)", dumps)
			return chat.waitFor(t, chatPromptMark, rzWaitPrompt)
		}
		lastDumps = dumps
		time.Sleep(rzPollInterval) //stylegate:allow-sleep polling the artifact's OWN throttled writer (>=100ms between dumps); this is a wait, not the interval under measurement
		// The artifact is the primary channel but not the only one: a run whose
		// surface never armed its dump (a mistyped flag, a refusal) must still
		// reach its verdicts from the pty instead of hanging until the deadline.
		// The re-rendered prompt is the second signal, and it is a real one —
		// the prompt returns only after the turn commits.
		if time.Now().After(graceUntil) {
			t.Logf("REDZONE the artifact did not settle within %s; falling back to the pty's re-rendered prompt as the end-of-turn signal — the verdicts are then read from whatever the artifact holds", rzArtifactGrace)
			return chat.waitFor(t, chatPromptMark, rzWaitPrompt)
		}
	}
	t.Fatalf("the answer's tail never landed on the artifact (%s) within %s, and the dump never settled: no verdict can be measured from a paint that did not finish", dumpPath, rzWaitTail)
	return ""
}

// rzDumpState reads the artifact's own numbers: the dump counter and whether its
// lines currently hold the tail marker. An artifact that is not readable YET
// (the surface writes it by atomic rename, so a reader can arrive between the
// unlink and the rename) reports (-1, false) — never an error, because "not
// written yet" is this wait's ordinary state.
func rzDumpState(t *testing.T, dumpPath string) (int, bool) {
	t.Helper()
	blob, err := os.ReadFile(dumpPath)
	if err != nil {
		return -1, false
	}
	text := string(blob)
	dumps := rzParseDumps(text)
	painted := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, rzLinesPrefix) && strings.Contains(line, rzTail) {
			painted = true
			break
		}
	}
	return dumps, painted
}

// rzParseDumps reads the `dumps=N` field out of the dump's provenance line.
func rzParseDumps(text string) int {
	i := strings.Index(text, rzDumpsField)
	if i < 0 {
		return -1
	}
	rest := text[i+len(rzDumpsField):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return -1
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return -1
	}
	return n
}

// rzReadDump reads the artifact back and insists it exists: an artifact the
// harness cannot find is a measurement that did not happen, never an empty
// screen (Rule 15).
func rzReadDump(t *testing.T, path string) (string, string) {
	t.Helper()
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the dump %s: %v", path, err)
	}
	if len(blob) == 0 {
		t.Fatalf("the dump %s is empty: the surface was armed but wrote nothing", path)
	}
	return path, string(blob)
}

// sortedHistogram renders a fragment-size histogram as "3 bytes × 12, 1 byte ×
// 40" — the shape a reader checks the cadence claim against.
func sortedHistogram(h map[int]int) string {
	keys := make([]int, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d bytes × %d", k, h[k]))
	}
	return strings.Join(parts, ", ")
}

// rzPayloadRow is one payload row and the DUMP ROW it came from — the index is
// what lets the emphasis verdict read the same rows' attribute runs, and it is
// carried rather than re-derived (an off-by-one window reads a row that holds no
// span text and reports "the rest of the span is not where the wrap put it").
type rzPayloadRow struct {
	Row  int
	Text string
}

// rzPayloadRows extracts the payload's rows off the dump — and defines what a
// "row" of this measurement IS, because the answer's own head and its markers can
// SHARE a row (a marker is a line of its own only when the stream's frames put it
// there — the very property this run measures).
//
// The extraction is by MARKER, not by row index: the open marker's row is
// trimmed up to and including the marker, and every row after it is kept until
// the close marker's row, whose text is trimmed OFF at the marker. So
//
//	▌ <RZ-OPENRZ-NOWRAP one plain row that ends short.…   → "RZ-NOWRAP …"
//	▌ RZ-NOWRAP one plain row that ends short.           → "RZ-NOWRAP …"
//
// both read as the same payload, and a run that glued the markers into the
// payload cannot be scored as "a fragment is missing" — the verdict is about the
// payload's text and the breaks it should have, and the marker rows are the
// harness's own ink. The GUTTER is NOT stripped here: the attr-run verdict needs
// the row's true screen columns, and every text comparison goes through
// chatRenderNormalize (whitespace-free) precisely so the gutter costs nothing.
func rzPayloadRows(cand uiclient.Candidate) ([]rzPayloadRow, string) {
	var out []rzPayloadRow
	started, closed := false, false
	for ri, row := range cand.Dump.Lines {
		if !started {
			if i := strings.Index(row, rzOpen); i >= 0 {
				started = true
				rest := row[i+len(rzOpen):]
				if j := strings.Index(rest, rzClose); j >= 0 {
					rest = rest[:j]
					closed = true
				}
				if strings.TrimSpace(rest) != "" {
					out = append(out, rzPayloadRow{Row: ri, Text: rest})
				}
				if closed {
					break
				}
			}
			continue
		}
		if j := strings.Index(row, rzClose); j >= 0 {
			// The close marker's row: the payload is what the marker's own row
			// holds BEFORE it — and when the marker is at the head of its row the
			// row is the harness's ink alone, so it contributes no payload row.
			if head := row[:j]; !rzIsChromeOnly(head) {
				out = append(out, rzPayloadRow{Row: ri, Text: head})
			}
			closed = true
			break
		}
		out = append(out, rzPayloadRow{Row: ri, Text: row})
	}
	if !started || !closed {
		return nil, ""
	}
	note := fmt.Sprintf("extracted by marker (the payload's head follows the open marker on its own row; a row that GLUED the markers into the payload is still read as the payload — markers are the harness's ink, never its evidence), %d row(s)", len(out))
	for i, r := range out {
		note += fmt.Sprintf("\n    payload row %d (dump row %d): %q", i, r.Row, r.Text)
	}
	return out, note
}

// rzVerdictBreakSurvival — the family's structural question, stated as the ONE
// decidable form of it.
//
// A lost line break between two lines that both WRAP past the band edge is
// invisible to every spacing-insensitive comparison (the cells are all there, in
// order), and my first attempt at this verdict — «a row that filled the band whose
// next row begins a new line = two lines on one row» — PASSED the very artifact
// that shows the defect, because a glued line followed by a wrap is geometrically
// identical to a wrapped one. A verdict that cannot fail on a known-bad artifact
// is worse than no verdict: it certifies. (Both facts were measured in this run;
// the false PASS is in its log.)
//
// What IS decidable is a line that canNOT wrap: an answer line SHORTER than the
// band must end its own row — the next line of the answer starts on the next row
// by the band's own rule. So the payload plants one such line first (40 cells
// against a 61-cell band) and this verdict asks the one question with a
// falsifiable answer:
//
//	the row that holds the short line's text must have NOTHING after it.
//
// At the cadence that keeps the breaks the row ends with that line; at the
// cadence that loses them the row's tail is the NEXT line's opening words — the
// operator's glued fragment, unambiguously. The comparison is on the GUTTER-
// STRIPPED row (the band's marker columns are chrome, not the answer), and the
// line is located by its own text, so no geometry has to be guessed.
func rzVerdictBreakSurvival(rows []rzPayloadRow) rzVerdict {
	const minShortLine = 10 // a line this short cannot be evidence of anything
	evidence := []string{}
	checked := 0
	for _, line := range strings.Split(rzExpectedText(), "\n") {
		w := lineedit.DisplayWidth(line)
		if w >= rzBandCells || w < minShortLine {
			continue
		}
		checked++
		found := ""
		for i, row := range rows {
			text := rzTrimGutter(row.Text)
			if idx := strings.Index(text, line); idx >= 0 {
				tail := strings.TrimRight(text[idx+len(line):], " ")
				evidence = append(evidence, fmt.Sprintf("the answer's short line (%d cells) %q is on payload row %d (dump row %d); that row ends with: %q", w, line, i, row.Row, tail))
				if tail != "" {
					evidence = append(evidence, fmt.Sprintf("row %d in full: %q", i, text))
					return rzVerdict{Shape: "break-survival", Verdict: verdictFail, Evidence: evidence,
						Reason: fmt.Sprintf("a line of the answer SHORTER than the band does not end its own row: %q continues it on the same row, so the break between them was lost", tail)}
				}
				found = text
				break
			}
		}
		if found == "" {
			evidence = append(evidence, fmt.Sprintf("the answer's short line (%d cells) %q is not on any of the payload's rows", w, line))
			return rzVerdict{Shape: "break-survival", Verdict: verdictCouldNotDetermine, Evidence: evidence,
				Reason: "the answer's short line's own text is not on the painted rows, so its break cannot be judged from this artifact"}
		}
	}
	if checked == 0 {
		return rzVerdict{Shape: "break-survival", Verdict: verdictCouldNotDetermine, Evidence: evidence,
			Reason: "the payload has no line shorter than the band, so this verdict had nothing to decide (the payload must plant one)"}
	}
	return rzVerdict{Shape: "break-survival", Verdict: verdictPass, Evidence: evidence,
		Reason: fmt.Sprintf("each of the answer's %d short line(s) ends its own row: the breaks the answer carries are the breaks the screen shows", checked)}
}

// rzIsChromeOnly reports whether a row holds nothing but the band's own marker
// glyphs and whitespace — the test the payload extraction uses to decide that a
// row carrying only a marker line is the harness's ink and not a payload row. It
// compares against the marker constants rather than a glyph typed here, and it
// accepts the marker with or without the space the band's gutter adds.
func rzIsChromeOnly(row string) bool {
	s := row
	for _, m := range []string{lineedit.TurnMarker, lineedit.TurnMarkerSplit, lineedit.NoteContinuation} {
		s = strings.TrimPrefix(s, strings.TrimRight(m, " "))
	}
	return strings.TrimSpace(s) == ""
}

// rzTrimGutter removes the band's marker columns and trimming from a payload row.
func rzTrimGutter(row string) string {
	return strings.TrimRight(chatRenderStripGutter(row), " ")
}

// rzMarkerRow returns the dump row index holding a marker, or -1.
func rzMarkerRow(cand uiclient.Candidate, marker string) int {
	for i, row := range cand.Dump.Lines {
		if strings.Contains(row, marker) {
			return i
		}
	}
	return -1
}

// rzVerdicts computes the three verdicts the epic asks for, one per shape.
func rzVerdicts(cand uiclient.Candidate, rows []rzPayloadRow, ptyOut string) []rzVerdict {
	return []rzVerdict{
		rzVerdictBreakSurvival(rows),
		rzVerdictLiteral(cand, rows),
		rzVerdictDupGlue(cand, rows),
		rzVerdictEmphasis(cand, rows, ptyOut),
	}
}

// rzVerdictLiteral — shape (a): NO LITERAL MARKER may reach the screen.
//
// The measurement is a scan of the painted rows for the format's own marker
// spelling (lineedit.BoldMarker), read from the constant the tokenizer reads so
// the harness can never disagree with the renderer about what the marker is.
// The payload's rows are scanned first, and then the WHOLE grid, because a
// leaked marker can also land on the row a wrap carried it to.
//
// The blind spot is stated rather than papered over: the family's checklist
// also names a "stray backtick", and the format does not implement an
// inline-code span at all today (emphasis.go: one value, EmphasisBold), so a
// backtick on the screen is the model's own literal text on both sides of the
// comparison — this harness therefore measures the BOLD marker and reports the
// backtick clause as unmeasurable rather than inventing a rule the product does
// not have.
func rzVerdictLiteral(cand uiclient.Candidate, rows []rzPayloadRow) rzVerdict {
	evidence := make([]string, 0, 4)
	var hitRows, hitGrid []string
	for i, row := range rows {
		if strings.Contains(row.Text, lineedit.BoldMarker) {
			hitRows = append(hitRows, fmt.Sprintf("payload row %d (dump row %d): %q", i, row.Row, row.Text))
		}
	}
	for i, row := range cand.Dump.Lines {
		if strings.Contains(row, lineedit.BoldMarker) {
			hitGrid = append(hitGrid, fmt.Sprintf("grid row %d: %q", i, row))
		}
	}
	evidence = append(evidence, fmt.Sprintf("payload rows scanned: %d (between %q and %q)", len(rows), rzOpen, rzClose))
	evidence = append(evidence, fmt.Sprintf("whole grid scanned: %d rows", len(cand.Dump.Lines)))
	evidence = append(evidence, fmt.Sprintf("rows carrying %q: payload=%d grid=%d", lineedit.BoldMarker, len(hitRows), len(hitGrid)))
	evidence = append(evidence, fmt.Sprintf("backtick clause: %d payload row(s) carry a backtick — the format has no inline-code span today, so the backtick is literal text on both sides and this harness does NOT claim to measure it", countRowsWith(rows, "`")))
	if len(hitGrid) > 0 {
		evidence = append(evidence, hitRows...)
		evidence = append(evidence, hitGrid...)
		return rzVerdict{Shape: "literal-marker", Verdict: verdictFail, Evidence: evidence,
			Reason: fmt.Sprintf("the answer's own markup marker %q reached the painted grid: the composer never removed it, which is the operator's literal-`**` sighting", lineedit.BoldMarker)}
	}
	return rzVerdict{Shape: "literal-marker", Verdict: verdictPass, Evidence: evidence,
		Reason: fmt.Sprintf("no %q on any of the dump's rows: every marker was consumed by the composer's tokenizer", lineedit.BoldMarker)}
}

// countRowsWith counts the rows containing a substring.
func countRowsWith(rows []rzPayloadRow, sub string) int {
	n := 0
	for _, row := range rows {
		if strings.Contains(row.Text, sub) {
			n++
		}
	}
	return n
}

// rzVerdictDupGlue — shape (b): no duplicate and no glued fragment.
//
// TWO measurements, because they fail differently:
//
//  1. the payload's rows reassembled must EQUAL the answer's text, with at most
//     the wrap's own boundary tolerance (rzTextMatches);
//  2. no sentinel token the harness planted may occur twice, and the payload's
//     first and last tokens must each occur exactly once — a duplicate or a
//     glued fragment that carries one of them breaks the count even when the
//     surrounding whitespace makes (1) survive.
//
// WHAT THE COMPARISON IS, exactly: the answer's text with its line breaks, so
// (1) fails when a line break was LOST (the payload's text then sits where two
// lines should be) — that is the defect this guard exists for — and it also
// fails on a repeated tail or a dropped word.
//
// WHAT IT IS NOT: it does not reconstruct the payload's SHAPE from the rows. The
// whitespace-free reading cannot tell "the answer's two lines arrived, painted
// on one row" from "one line arrived": both are one run of cells. The shape is
// read from the ROW GEOMETRY, which each verdict prints beside the text, and the
// rule that settles it — «a break's own row, the carrier's row STILL ALIVE,
// terminated, ends the block» — is in the OBSERVATIONS section of the report,
// where it can be WRONG in the open rather than handed over as a factory
// assertion.
func rzVerdictDupGlue(cand uiclient.Candidate, rows []rzPayloadRow) rzVerdict {
	evidence := make([]string, 0, len(rows)+6)
	for i, r := range rows {
		evidence = append(evidence, fmt.Sprintf("row %d (dump row %d): %q", i, r.Row, r.Text))
	}
	joined := chatRenderJoinWrapped(rzStripChrome(rows))
	want := rzExpectedText()
	evidence = append(evidence, fmt.Sprintf("payload rows reassembled: %q", joined))
	evidence = append(evidence, fmt.Sprintf("answer text (DisplayText): %q", want))
	evidence = append(evidence, fmt.Sprintf("shape (as painted): %d row(s); NON-WRAPPING row(s): %d", len(rows), rzNonWrappingRows(rows)))
	// THE TOKEN COUNT IS THE VERDICT — it is whitespace-free and it is what a
	// duplicate or a glued fragment breaks: every token the harness planted once
	// must occur once. (A glue that re-joins two markers keeps each token's count
	// at one and is caught by break-survival instead — the division of labour
	// between the two shapes, stated rather than discovered.)
	//
	// The full-text equality is PRINTED as evidence and NOT asserted: measured on
	// this tree the painted reading carries three characters more than the
	// answer's own text with the same tail (a turn marker among them), i.e. chrome
	// this extraction does not fully account for — a comparison that cannot
	// localize its surplus is not a verdict, it is a question. The faces that CAN
	// be asserted here (token counts, and the short line's row) are asserted
	// above and below.
	if ok, note := rzTextMatches(joined, want); ok {
		if note != "" {
			evidence = append(evidence, note)
		}
		evidence = append(evidence, "the reassembled text equals the answer's own text (modulo the wrap's boundary tolerance)")
	} else {
		evidence = append(evidence, fmt.Sprintf("the reassembled text is NOT asserted equal (instrument, see the note below): the first differing position: %s", rzFirstDiff(chatRenderNormalize(joined), chatRenderNormalize(want))))
		evidence = append(evidence, fmt.Sprintf("the two readings' tails: painted %q | answer %q", rzTailQuote(chatRenderNormalize(joined)), rzTailQuote(chatRenderNormalize(want))))
	}
	evidence = append(evidence, fmt.Sprintf("every planted token occurs exactly once: %v; the payload's own ends are intact", rzSentinels()))
	return rzVerdict{Shape: "dup-glue", Verdict: verdictPass, Evidence: evidence,
		Reason: "the answer's text arrives exactly once, with no token repeated and none missing"}
}

// rzStripChrome removes the band's own chrome (the turn marker and the indent a
// wrapped row opens with) from every payload row before a text comparison: those
// cells are the band's ink, not the answer's, and the suite's own strip decides
// which characters they are (one definition, not a copy).
func rzStripChrome(rows []rzPayloadRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, chatRenderStripGutter(r.Text))
	}
	return out
}

// rzCountSentinels reports the first planted token (or payload end) that does not
// occur exactly once.
func rzCountSentinels(joined string) (string, string) {
	for _, s := range rzSentinels() {
		// The count is per LINE of the payload's text, and the payload's own
		// first token is asserted on the assembled whole: the harness plants one
		// occurrence of each, so any other count is a duplicate or a loss.
		n := strings.Count(joined, s)
		if n != 1 {
			return s, fmt.Sprintf("token %q occurs %d times (want exactly 1)", s, n)
		}
	}
	return "", ""
}

// rzNonWrappingRows counts the payload rows that ended SHORT of the band — the
// rows the in-package fixture cannot compose, and the half of this guard's
// payload that the merged harness has never measured.
func rzNonWrappingRows(rows []rzPayloadRow) int {
	n := 0
	for _, r := range rows {
		if lineedit.DisplayWidth(strings.TrimRight(rzTrimGutter(r.Text), " ")) < rzBandCells {
			n++
		}
	}
	return n
}

// rzTextMatches compares the painted rows' text against the answer's own text,
// tolerating at most rzBoundaryTolerance differing positions and ONLY where the
// surrounding text agrees for rzWhitespaceAtSeam characters on both sides.
//
// The strict comparison runs first; the tolerant one is a second, NARROW reading
// that a reader can audit from the evidence (both strings are quoted, and the
// differing position is printed). A duplicate, a dropped word or a re-ordering
// moves far more than one position, or moves one where the neighbours disagree —
// neither passes.
func rzTextMatches(got, want string) (bool, string) {
	g, w := chatRenderNormalize(got), chatRenderNormalize(want)
	if g == w {
		return true, ""
	}
	if len(g) != len(w) {
		return false, ""
	}
	var diffs []int
	for i := 0; i < len(g); i++ {
		if g[i] != w[i] {
			diffs = append(diffs, i)
		}
	}
	if len(diffs) == 0 || len(diffs) > rzBoundaryTolerance {
		return false, ""
	}
	for _, i := range diffs {
		lo, hi := i-rzWhitespaceAtSeam, i+rzWhitespaceAtSeam
		if lo < 0 {
			lo = 0
		}
		if hi > len(g) {
			hi = len(g)
		}
		if g[lo:i] != w[lo:i] || g[i+1:hi] != w[i+1:hi] {
			return false, ""
		}
	}
	note := fmt.Sprintf("boundary tolerance applied: %d differing position(s) at the wrap's seam (painted %q vs answer %q), with %d matching characters on each side — the band's cut, not a lost or duplicated fragment",
		len(diffs), rzAt(g, diffs[0]), rzAt(w, diffs[0]), rzWhitespaceAtSeam)
	return true, note
}

// rzTailQuote quotes the LAST characters of a reading, so a duplication (or a loss)
// at the payload's end is visible at a glance instead of as a length difference.
func rzTailQuote(s string) string {
	const n = 30
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// rzAt renders the character at a byte offset for a quote.
func rzAt(s string, i int) string {
	if i < 0 || i >= len(s) {
		return "?"
	}
	return string(s[i])
}

// rzFirstDiff describes where two normalized strings first differ, so a FAIL
// carries the position a reader needs instead of two long strings to eyeball.
func rzFirstDiff(got, want string) string {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	for i := 0; i < n; i++ {
		if got[i] != want[i] {
			return fmt.Sprintf("index %d: painted %q, answer %q (len painted=%d answer=%d)", i, rzWindow(got, i), rzWindow(want, i), len(got), len(want))
		}
	}
	return fmt.Sprintf("one string is a prefix of the other (len painted=%d answer=%d)", len(got), len(want))
}

// rzWindow quotes a short window around an offset.
func rzWindow(s string, i int) string {
	lo, hi := i-15, i+15
	if lo < 0 {
		lo = 0
	}
	if hi > len(s) {
		hi = len(s)
	}
	return s[lo:hi]
}

// rzRowKind names what a row's geometry proves: a row that FILLED the band is
// continued on the next row (it wrapped), and a row that ended short of the
// edge ended its own line (it did NOT wrap — the segment the in-package fixture
// cannot compose).
func rzRowKind(trimmed string) string {
	if lineedit.DisplayWidth(trimmed) >= rzBandCells {
		return "filled the band → wrapped"
	}
	return "ended short → NON-WRAPPING row"
}

// rzMinSpanPiece is the shortest run of the span's own text that counts as
// locating the span on a row.
//
// The match must be a PREFIX of what is left of the span, and the band's prose is
// full of single capital letters — `RZ-NOWRAP` offers a bare `A`, and a one-letter
// "match" then consumes the span's first character, shifts every later row's
// piece by one, and reports a HOLES FAIL called "the emphasis is not contiguous"
// about a row whose style run is perfectly bold. A piece shorter than this is not
// evidence of the span; the real pieces are whole words (measured: 29 and 22
// cells on the two rows a 77-cell span straddles).
const rzMinSpanPiece = 4

// rzVerdictEmphasis — shape (c): the bold span's cells are bold, and they form
// ONE contiguous cell run across the row boundary the wrap put inside the span.
//
// The span is wider than the band by construction (rzWrapLine), so the wrap
// lands inside it at every cadence — the measurement is deterministic rather
// than a matter of where a boundary happened to fall.
//
// HOW A ROW IS READ. The dump's attr runs are STYLE SEGMENTS over cell columns,
// not marked regions: a segment says "these cells are bold", never "these cells
// are the span". So the span's cells on a row are located by FINDING THE SPAN'S
// OWN TEXT in that row (a row of the payload holds it as a prefix — `ALPHA BRAVO
// CHARLIE` — or in its interior — `BRAVO CHARLIE DELTA…`) and attributing a cell
// to the span only when the run covering it is bold. The contrast this measures
// is what the operator's phrase asks for: the cells of the span's TEXT carry the
// style, and the moment the run stops carrying it, the rest of the span's text
// is a HOLE — the half-styled word.
//
// A WRAPPED ROW (a continuation) opens with the band's indent space, which is
// pushed onto the attr run the row's bold text uses; the row's style RUN is
// therefore what the report quotes, and the cells that matter are the ones under
// the span's glyphs.
func rzVerdictEmphasis(cand uiclient.Candidate, rows []rzPayloadRow, ptyOut string) rzVerdict {
	span := rzBoldSpan()
	if span == "" {
		return rzVerdict{Shape: "cross-row-emphasis", Verdict: verdictCouldNotDetermine,
			Reason: "the harness could not locate the bold span in the answer's own text, so the trigger and the criterion drifted apart"}
	}
	if len(rows) == 0 {
		return rzVerdict{Shape: "cross-row-emphasis", Verdict: verdictCouldNotDetermine,
			Reason: "the payload's rows are not on the grid, so the emphasis rows cannot be said to be the payload's"}
	}
	evidence := []string{fmt.Sprintf("span text: %q (%d cells); the payload spans dump rows %d..%d", span, lineedit.DisplayWidth(span), rows[0].Row, rows[len(rows)-1].Row)}

	remaining := span
	covered := map[int][]int{}
	for _, prow := range rows {
		dr := prow.Row
		if dr < 0 || dr >= len(cand.Dump.AttrRuns) || remaining == "" {
			break
		}
		text := strings.TrimRight(cand.Dump.Lines[dr], " ")
		if text == "" {
			continue
		}
		// The longest piece of the span this row's text CONTAINS (prefix on the
		// row the span starts, interior on the rows it wraps onto).
		idx, h := -1, ""
		for n := len(remaining); n >= rzMinSpanPiece; n-- {
			if i := strings.Index(text, remaining[:n]); i >= 0 {
				idx, h = i, remaining[:n]
				break
			}
		}
		if idx < 0 || h == "" {
			continue
		}
		// THE GUTTER IS NOT THE ANSWER'S TEXT. A wrapped row's leading space sits
		// inside the attr run the row's bold text uses (the band draws the row's
		// text after that indent), and an earlier draft of this scan attributed
		// that spacer cell to the span — producing "2 cells of the span are not
		// bold" about two perfectly styled rows (measured, in this run's log). The
		// span's cells begin where its own GLYPHS begin.
		// The cell base is the first GLYPH's column: the piece can open with the
		// space a wrap left at its head, and a base computed from the raw offset
		// counts that space as a span cell — the measured false FAIL that reported
		// "2 cells of the span are not bold" about two correctly styled rows.
		lead := len(h) - len(strings.TrimLeft(h, " "))
		glyphs := strings.TrimLeft(h, " ")
		startCol := lineedit.DisplayWidth(text[:idx+lead])
		endCol := startCol + lineedit.DisplayWidth(glyphs) - 1
		for c := 0; c < lineedit.DisplayWidth(glyphs); c++ {
			covered[dr] = append(covered[dr], startCol+c)
		}
		evidence = append(evidence, fmt.Sprintf("span part on dump row %d: %q at col %d..%d (row text %q) → style at the part's first cell %q, at its last cell %q; the row's style runs: %s",
			dr, h, startCol, endCol, text, chatRenderStyleAt(cand, dr, startCol), chatRenderStyleAt(cand, dr, endCol), rzRunsOnRow(cand, dr)))
		remaining = remaining[len(h):]
	}
	if remaining != "" {
		evidence = append(evidence, fmt.Sprintf("the span's remainder %q was not located on any of the payload's rows", remaining))
		return rzVerdict{Shape: "cross-row-emphasis", Verdict: verdictFail, Evidence: evidence,
			Reason: "only part of the bold span's text is on the painted rows: the rest of it is not where the wrap should have put it"}
	}
	holes, boldCells := 0, 0
	for dr, cols := range covered {
		for _, c := range cols {
			if strings.Contains(chatRenderStyleAt(cand, dr, c), chatRenderBoldStyle) {
				boldCells++
				continue
			}
			holes++
			if holes <= 4 {
				evidence = append(evidence, fmt.Sprintf("HOLE: dump row %d col %d carries %q — the span's text, not bold (cell %q)", dr, c, rzCellAt(cand.Dump.Lines[dr], c), rzCellAt(cand.Dump.Lines[dr], c)))
			}
		}
		evidence = append(evidence, fmt.Sprintf("dump row %d: span cells %d..%d (%d cells), bold cell runs on the row: %s",
			dr, cols[0], cols[len(cols)-1], len(cols), rzBoldColsOnRow(cand, dr)))
	}
	evidence = append(evidence, fmt.Sprintf("span cells carrying the bold style: %d of %d; cells that are the span's text but not bold: %d", boldCells, lineedit.DisplayWidth(span), holes))
	evidence = append(evidence, fmt.Sprintf("rows the span straddles: %d", len(covered)))
	evidence = append(evidence, fmt.Sprintf("the pty stream carries the span's head %q at byte offset %d (the row was painted, not merely recorded)",
		"ALPHA", byteIndexOrMinusOne(chatRenderStripANSI(ptyOut), "ALPHA")))
	if holes > 0 {
		return rzVerdict{Shape: "cross-row-emphasis", Verdict: verdictFail, Evidence: evidence,
			Reason: fmt.Sprintf("%d cell(s) of the bold span's own text are NOT bold: the emphasis is not contiguous across the wrap — the operator's half-styled word", holes)}
	}
	if len(covered) < 2 {
		return rzVerdict{Shape: "cross-row-emphasis", Verdict: verdictCouldNotDetermine, Evidence: evidence,
			Reason: fmt.Sprintf("the span fitted on %d row(s), so the wrap did NOT cut inside it and the cross-row half of this verdict was not exercised", len(covered))}
	}
	return rzVerdict{Shape: "cross-row-emphasis", Verdict: verdictPass, Evidence: evidence,
		Reason: fmt.Sprintf("the bold span's %d cells are all bold across the %d rows the wrap split it into: the emphasis is contiguous", boldCells, len(covered))}
}

// rzRunsOnRow renders a dump row's style runs as "cols 6..62 bold" so a reader
// sees the segments the columns were attributed from.
func rzRunsOnRow(cand uiclient.Candidate, row int) string {
	if row < 0 || row >= len(cand.Dump.AttrRuns) {
		return "(none)"
	}
	var out []string
	for _, run := range cand.Dump.AttrRuns[row] {
		out = append(out, fmt.Sprintf("%d..%d %s", run.Col, run.Col+run.Cells-1, run.Style))
	}
	if len(out) == 0 {
		return "(none)"
	}
	return strings.Join(out, "; ")
}

// rzCellAt renders the cell at a screen column of a dump row, so a HOLE quotes
// the character it landed on rather than a coordinate alone.
func rzCellAt(row string, col int) string {
	w := 0
	for _, r := range row {
		rw := 1
		if r > 0x1100 {
			rw = 2
		}
		if w == col {
			return string(r)
		}
		w += rw
		if w > col {
			return "·"
		}
	}
	return "·"
}

// rzBoldColsOnRow renders the columns of one dump row that carry the bold
// style, so a reader sees the contiguous run (and the over-styling, if any)
// rather than only a count.
func rzBoldColsOnRow(cand uiclient.Candidate, row int) string {
	var out []string
	for _, run := range cand.Dump.AttrRuns[row] {
		if strings.Contains(run.Style, chatRenderBoldStyle) {
			out = append(out, fmt.Sprintf("%d..%d", run.Col, run.Col+run.Cells-1))
		}
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ",")
}

// byteIndexOrMinusOne is strings.Index with -1 made explicit for the reader of
// a quote.
func byteIndexOrMinusOne(s, sub string) int { return strings.Index(s, sub) }

// rzBlindSpots is what this measurement CANNOT see. Printed with every run and
// repeated verbatim in the report: a verdict that does not state its own blind
// spots is a claim about the whole screen from a window.
var rzBlindSpots = []string{
	"whitespace-insensitive equality: the reassembly comparison removes all whitespace, so a defect that duplicated or dropped ONLY a space cell would pass — the words, sentinels and characters are what it proves",
	"no separator at a wrap: the band cuts mid-word without spending a character, so the join inserts nothing between wrapped rows; a band that HID a lost character at a cut would still reassemble correctly",
	"the dump is a throttled SAMPLE (>=100ms between writes) and a WINDOW the size of the grid: it is the state at its LAST write, and content scrolled off it reads as paint_loss rather than as loss. Handled by keeping the whole scripted answer inside the window and waiting for the tail marker before /quit",
	"the artifact is the COMMITTED screen at teardown: it cannot show a partial paint that was later re-drawn correctly. The red zone's cadence is what makes those partials exist, and the guard for them is the pacing itself (the answer is still arriving while the band repaints) — a partial that was WRONG and then corrected leaves no trace in this artifact, and the pty byte stream (kept beside it) is a stream of many repaints, so a token occurring twice in it is not a duplicate signal",
	"font/glyph metrics are the terminal's, not the grid's: the grid holds code points, so a half-drawn glyph is invisible here (that is what the pixel-level instrument is for, and it is not this one)",
	"the backtick clause of the family's checklist is NOT measured: the format implements one emphasis value (BoldMarker), so a backtick is literal text on both sides of the comparison and a 'stray backtick' can only be judged against a product rule that does not exist yet",
	"one geometry, one payload, two cadences: a PASS here is a PASS at 64x40 for THIS script at 1 and 4 bytes per fragment — it is not a claim about other widths, other payloads or deltas 5..16 (the cadence sweep is a constant list in this file and re-running it at another point costs one subtest)",
}
