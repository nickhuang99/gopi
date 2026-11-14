package main

// chat_dump_geometry_pty_test.go — the REGRESSION that pins the dump's grid to
// the geometry the LAYOUT drew into, on a REAL pty of a KNOWN width.
//
// THE DEFECT THIS PINS (measured 2026-09-26, session 8c836970): the dump's grid
// was built by probing os.Stderr's TIOCGWINSZ while the editor measures os.Stdin
// through lineedit.Terminal.Size. When that probe failed — on this host it does,
// for the chat REPL's stderr — internal/uiclient silently substituted its 80x24
// default, and the artifact then re-interpreted a frame the layout had painted at
// 117 columns as if it were 80 wide. The observable was:
//
//	#gopi-chat-dump ... terminal=0x0 model=0x0 grid=80x24 ... frames=12
//
// frames=12 proves the layout painted; grid=80x24 says the observation was built
// at a width the paint never used. Correct wraps re-read as truncation =
// FABRICATED CORRUPTION, and an agent reading the artifact cannot distinguish it
// from real corruption. That is the whole reason this file exists.
//
// WHAT IS ASSERTED (three facts, one harness):
//
//  1. the grid's width EQUALS the pty's — the region was installed at 117 columns
//     and the sink's grid is 117 wide, not the 80x24 default;
//  2. the artifact SAYS SO: `geometry=layout` in the record line (and in the JSON
//     twin), so a reader can tell an authoritative grid from a fallback one;
//  3. a CJK+latin sentence handed to the band comes back BYTE-EXACT in the grid —
//     which is only true at the right width, because at 80 columns the row would
//     have been cut and the tail lost.
//
// …plus the FALLBACK case, forced without a pty: the artifact must SAY
// `geometry=fallback` and the record must explain itself (Rule 15 — a fallback
// that is not recorded is a silent failure, and this is the red proof that it is
// not).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// The pty the regression uses. Both numbers are named because the case is ABOUT
// the numbers: the width must be one the 80-column default would visibly
// mis-read, and the height must host the reserved layout.
const (
	dumpGeomPtyCols = 117
	dumpGeomPtyRows = 26
)

// dumpGeomSentence is the CJK+latin line the band is given and the grid must
// return byte-exact. It is longer than the fallback grid's 80 columns on
// purpose: at the wrong width it CANNOT round-trip, so a green assertion here is
// evidence the width is the layout's rather than a coincidence of a short string.
const dumpGeomSentence = "中文与 latin 混排的句子 —— dump 必须逐字符还原，不能重排。"

// dumpGeomPty is an open pty pair sized to dumpGeomPtyCols × dumpGeomPtyRows,
// allocated through the package's VERIFIED fixture (chat_pty_fixture_test.go):
// the four openpty steps are not atomic, so the slave fd it hands back is one
// the kernel has been asked to confirm is a terminal, with a bounded, named
// number of retries — and a failure that cannot be retried away names the
// observed condition rather than skipping quietly (Rule 15). Construction
// failure SKIPS (a host without /dev/ptmx cannot host this case, and a
// substitute stream would assert nothing about a real terminal).
func dumpGeomPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	pair := chatOpenPtyPair(t, dumpGeomPtyCols, dumpGeomPtyRows)
	return pair.Master, pair.Slave
}

// TestChatDump_GridFollowsTheLayoutGeometryOnARealPty is the regression: a real
// pty of KNOWN width, a reserved layout installed at that width, and a dump whose
// grid width must equal it — with a CJK+latin row byte-exact.
func TestChatDump_GridFollowsTheLayoutGeometryOnARealPty(t *testing.T) {
	// The master stays open for the whole case (closing it first would SIGHUP
	// the slave under the editor); its cleanup is registered by dumpGeomPty.
	_, slave := dumpGeomPty(t)
	// The pty's own answer, read through the SAME primitive the editor uses: if
	// this disagrees with the constants above, the case is unset rather than
	// green against a number the kernel never held.
	if got := lineedit.TerminalCols(int(slave.Fd())); got != dumpGeomPtyCols {
		t.Fatalf("the pty reports %d columns, want %d — the kernel did not take the size", got, dumpGeomPtyCols)
	}

	path := filepath.Join(t.TempDir(), uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: path,
		Cols: 0,
		Rows: 0,
		Identity: uiclient.DumpIdentity{
			Surface:          uiclient.SurfaceChat,
			PID:              os.Getpid(),
			Target:           chatDumpTarget(false),
			TerminalColsRows: [2]int{0, 0},
			Started:          uiDumpStartStamp(),
		},
	})
	if err != nil {
		t.Fatalf("arm the dump sink: %v", err)
	}
	// The sink is armed at the DEFAULT geometry (0,0 → 80x24 fallback), which is
	// exactly the pre-fix state: only the layout's own install may move it.
	if _, cols, rows := d.Geometry(); cols != 0 && cols != dumpGeomDefaultCols {
		t.Fatalf("the sink opened at %dx%d; the case needs it to start at the default so the layout install is the only thing that can move it", cols, rows)
	}

	// The REAL editor, reading the pty slave (the geometry source production
	// uses) and painting into the sink (the tee production installs).
	term, err := lineedit.Open(slave, d)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	defer func() { _ = term.Close() }() //stylegate:ignore the harness's own terminal teardown
	ed := lineedit.NewChatEditor(term, d, chatPrompt, nil, os.Stderr, NewSlashCompleter())
	ed.SetGeometryObserver(chatGeomObserver(d))
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: the case needs the reserved layout installed", dumpGeomPtyCols, dumpGeomPtyRows)
	}
	// 1. the LAYOUT's own geometry is the pty's.
	cols, rows, ok := ed.LayoutGeometry()
	if !ok {
		t.Fatal("the layout reports no geometry after EnableRegion installed it")
	}
	if cols != dumpGeomPtyCols || rows != dumpGeomPtyRows {
		t.Fatalf("the layout's geometry is %dx%d, want the pty's %dx%d", cols, rows, dumpGeomPtyCols, dumpGeomPtyRows)
	}

	// The band gets the CJK+latin line, the way the flow seam delivers one.
	if !ed.WriteBandRow(dumpGeomSentence) {
		t.Fatal("the band refused the row: no reserved layout is active on the editor")
	}
	// DIRECT DRIVER: no read loop runs in this case (nothing calls Run), so the row
	// just queued is written HERE — the dump below is read as the screen the band
	// drew, and a queued-but-undrawn row would make every geometry assertion here
	// about a grid nothing was painted onto.
	if !ed.SettleBand() {
		t.Fatal("the band did not settle the row: the layout does not own the screen")
	}

	// 2. the GRID's geometry is the layout's (the defect's own number).
	geom, gcols, grows := d.Geometry()
	if gcols != dumpGeomPtyCols {
		t.Fatalf("the dump grid is %d columns wide, want the pty's %d — the sink re-interpreted the layout's paint stream at a geometry the layout never drew into (before the fix: %d, the model default)",
			gcols, dumpGeomPtyCols, dumpGeomDefaultCols)
	}
	if grows != dumpGeomPtyRows {
		t.Fatalf("the dump grid is %d rows tall, want the pty's %d", grows, dumpGeomPtyRows)
	}
	if geom != uiclient.GeometrySourceLayout {
		t.Fatalf("the sink's geometry provenance is %q, want %q — the grid's width came from somewhere other than the installed layout",
			geom, uiclient.GeometrySourceLayout)
	}

	// 3. the artifact SAYS it: close, then read the file back through the SAME
	// reader `gopi ui_state` uses (never a second parser in a test).
	if err := d.Close(); err != nil {
		t.Fatalf("finalize the dump: %v", err)
	}
	cand, err := uiclient.ReadDump(path)
	if err != nil {
		t.Fatalf("read back the dump %s: %v", path, err)
	}
	if got := cand.Record.Fields[dumpRecordGeometryFieldName]; got != string(uiclient.GeometrySourceLayout) {
		t.Fatalf("the record's geometry= field is %q, want %q", got, uiclient.GeometrySourceLayout)
	}
	if cand.Dump.Cols != dumpGeomPtyCols {
		t.Fatalf("the artifact's own grid header says %d columns, want %d", cand.Dump.Cols, dumpGeomPtyCols)
	}

	// 3b. …and the row round-trips byte-exact. The comparison runs through the
	// shipped one-call compare (uiclient.CompareTranscriptLine): the transcript
	// side is the sentence itself, so a match means "the grid carries exactly
	// these runes" — the same answer `gopi ui_state --compare` gives.
	verdict := uiclient.CompareTranscriptLine(dumpGeomSentence, cand, []string{dumpGeomSentence})
	if verdict.Status != uiclient.CompareMatch {
		t.Fatalf("the CJK+latin line did not round-trip: status=%s in_grid=%t row=%d prefix=%d reason=%s\nrow bytes: %q",
			verdict.Status, verdict.InGrid, verdict.GridRow, verdict.GridPrefixRunes, verdict.Reason,
			gridRowForTest(cand, dumpGeomSentence))
	}
	if verdict.Geometry != uiclient.GeometrySourceLayout {
		t.Fatalf("the verdict reports geometry=%q, want %q", verdict.Geometry, uiclient.GeometrySourceLayout)
	}

	// The terminal is released and the editor's own background state (the
	// completion worker) stopped, so the suite's goroutine sweep does not see a
	// parked worker from this case.
	ed.Close()
}

// gridRowForTest finds the row that best matches want, for a failure message
// that shows what the grid actually held (an assertion that says only "not
// equal" costs the next reader the whole diagnosis).
func gridRowForTest(cand uiclient.Candidate, want string) string {
	best, bestN := "", 0
	for _, row := range cand.Dump.Lines {
		n := 0
		for n < len([]rune(row)) && n < len([]rune(want)) && []rune(row)[n] == []rune(want)[n] {
			n++
		}
		if n > bestN {
			best, bestN = row, n
		}
	}
	return best
}

// dumpGeomDefaultCols mirrors internal/uiclient's fallback width for the failure
// messages above. It is deliberately a LOCAL constant rather than an import of
// the unexported one: a test that read the implementation's default could not
// notice the default changing, and the number is part of this case's story (the
// pre-fix artifact said 80x24 at a 117-wide pty).
const dumpGeomDefaultCols = 80

// TestChatDump_ForcedGeometryFailureSaysSo is the second half of the contract,
// forced WITHOUT a pty (Rule 27's spirit: prove the fallback path directly, never
// by making the machine fail): a sink whose layout never installed anything must
// record `geometry=fallback` and, at run time, print the notice.
func TestChatDump_ForcedGeometryFailureSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), uiclient.ChatDumpFileName)
	notices := 0
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path:              path,
		Identity:          uiclient.DumpIdentity{Surface: uiclient.SurfaceChat, PID: os.Getpid(), Started: uiDumpStartStamp()},
		OnGeometryChanged: func() { notices++ },
	})
	if err != nil {
		t.Fatalf("arm the sink: %v", err)
	}
	// 0,0 is the FORCED failure: nothing measured a terminal, so the sink took
	// its default — and it must have recorded that as the fallback, not as a
	// measurement.
	if geom, cols, rows := d.Geometry(); geom != uiclient.GeometrySourceFallback {
		t.Fatalf("a sink built with no measured geometry reports provenance %q (%dx%d), want %q — a default that reads as a measurement is the silent fallback this change removes",
			geom, cols, rows, uiclient.GeometrySourceFallback)
	}
	// A layout that is NOT installed: the observer path reports ok=false. The
	// fact is unchanged (the sink was built at the fallback), so nothing fires —
	// a notice per call would be noise, and construction is deliberately quiet
	// because that state is normally transient (SetGeometry moves it a moment
	// later when the layout installs).
	before := notices
	chatGeomObserver(d)(0, 0, false)
	if notices != before {
		t.Fatalf("the fallback notice fired for an unchanged geometry (%d → %d): the hook is for changes, not for every call", before, notices)
	}
	// One frame is painted (the bare word below is enough: the sink counts
	// interpreted WRITES, not cells), so the refusal that follows is about the
	// GEOMETRY rather than about an unpainted grid — the two are different
	// facts and this case is about the fallback one.
	if _, err := d.Write([]byte("a painted frame\n")); err != nil {
		t.Fatalf("paint one frame into the sink: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("finalize the dump: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the dump: %v", err)
	}
	if !strings.Contains(string(raw), dumpRecordGeometryFieldName+"="+string(uiclient.GeometrySourceFallback)) {
		t.Fatalf("the artifact does not say geometry=%s; its record line is:\n%s",
			uiclient.GeometrySourceFallback, strings.SplitN(string(raw), "\n", 2)[0])
	}
	cand, err := uiclient.ReadDump(path)
	if err != nil {
		t.Fatalf("read back the dump: %v", err)
	}
	// The one-call compare REFUSES at a fallback geometry rather than answering
	// containment at a width nothing measured — the false signal this whole
	// change exists to remove.
	v := uiclient.CompareTranscriptLine("anything at all", cand, []string{"anything at all"})
	if v.Status != uiclient.CompareRefused {
		t.Fatalf("compare at a fallback geometry returned %s, want %s (reason: %s)", v.Status, uiclient.CompareRefused, v.Reason)
	}
	if v.Reason != uiclient.GeometryFallbackNotice {
		t.Fatalf("the refusal reason is %q, want the fallback notice verbatim", v.Reason)
	}
}

// TestChatReportDump_SaysTheFallbackOnStderr is Rule 15's loud half at the seam
// the operator actually reads: the CLOSING report names the geometry the artifact
// was frozen at, and a fallback geometry is stated in words as well as in the
// record's own field.
func TestChatReportDump_SaysTheFallbackOnStderr(t *testing.T) {
	path := filepath.Join(t.TempDir(), uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path:     path,
		Identity: uiclient.DumpIdentity{Surface: uiclient.SurfaceChat, PID: os.Getpid(), Started: uiDumpStartStamp()},
	})
	if err != nil {
		t.Fatalf("arm the sink: %v", err)
	}
	if _, err := d.Write([]byte("a painted frame\n")); err != nil {
		t.Fatalf("paint: %v", err)
	}
	out := captureStderrStream(t, func() { chatReportDump(d) })
	if !strings.Contains(out, dumpRecordGeometryFieldName+"="+string(uiclient.GeometrySourceFallback)) {
		t.Fatalf("the closing report does not carry geometry=%s:\n%s", uiclient.GeometrySourceFallback, out)
	}
	if !strings.Contains(out, uiclient.GeometryFallbackNotice) {
		t.Fatalf("the run did not SAY the fallback in words (Rule 15):\n%s", out)
	}
	// The contrast: a layout-measured grid is reported WITHOUT the panic notice.
	d.SetGeometry(uiclient.GeometrySourceLayout, dumpGeomPtyCols, dumpGeomPtyRows)
	out2 := captureStderrStream(t, func() { chatReportDump(d) })
	if strings.Contains(out2, uiclient.GeometryFallbackNotice) {
		t.Fatalf("a layout-measured grid was reported with the fallback notice:\n%s", out2)
	}
	if !strings.Contains(out2, dumpRecordGeometryFieldName+"="+string(uiclient.GeometrySourceLayout)) {
		t.Fatalf("the report does not carry geometry=layout:\n%s", out2)
	}
}

// dumpRecordGeometryFieldName is the record field this file asserts on. It is
// spelled here as the WRITER's literal ("geometry") because that is the artifact
// contract a reader greps for — reading it from the implementation would make
// the test agree with the code by construction, which is exactly what a
// contract assertion must not do.
const dumpRecordGeometryFieldName = "geometry"
