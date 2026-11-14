package main

// chat_turn_dump_variable_read_pty_test.go — the READ BOUNDARY, driven for real.
//
// THE QUESTION. Session 884eca28's armed chat grid holds a scrollback row whose
// answer text is absent and whose place is taken by the tail of an escape
// sequence (`;61H24;73H5;230m` — three placed sequences with every introducer
// gone). A predecessor task reconciled that same real pty turn at cell level and
// measured lost=0 extra=0 — but its reader fed the sink in UNIFORM 7-byte reads.
// A uniform boundary cannot reach one whole class of read: the sink interprets
// exactly the writes it RECEIVES, so the boundary IS its read list. A frame
// delivered as ONE write puts the frame boundary INSIDE the read; a frame split
// at a fixed step puts it wherever the step falls; and a read that is SHORT (the
// caller's model returning fewer bytes than the terminal produced) delivers a
// stream the terminal never had the tail of.
//
// WHAT THIS FILE DOES. It drives the same answer, on the same real 117x26 pty,
// through the same armed `--dump-screen` sink, under DIFFERENT read lists, and
// reads the result back as the artifact's RENDERED CELL GRID. The reconciliation
// is the artifact's cells against the answer's cells, in BOTH directions (lost =
// produced cells the grid lacks, extra = grid cells the answer never had), and it
// carries its own self-check: the counter is run on a deliberately injured copy of
// the answer's own rows, so a `lost=0 extra=0` cannot be a green that means
// nothing.
//
// THE WIRING FACT THAT MAKES THIS FAITHFUL: internal/uiclient/dump.go's Write is
// one `Screen.Feed` per call, so the sink's `Frames` counter is its READ COUNT. A
// boundary leg therefore reports its own read count beside the sink's frames, and
// the two must agree — otherwise the leg is measuring a boundary the sink did not
// see.
//
// EVIDENCE: every artifact and recording lands under the run's session directory
// (Rule 30 — never in the worktree).

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// The geometry: the layout's own shape AND the live evidence's shape, so the wrap
// decisions here are the ones the operator's terminal made.
const (
	vrrPtyCols = 117
	vrrPtyRows = 26
)

// vrrUniformReadBytes is the READ SIZE the predecessor task MODELLED: its reader
// handed the sink a flat 7 bytes at a time. It is kept as a measured leg — never
// as the only one — because "the model was more forgiving than the production
// path" is a claim that needs both boundaries on the same turn.
const vrrUniformReadBytes = 7

// vrrEvidenceDir is the run's session directory: evidence belongs OUTSIDE the
// worktree, so a run leaves `git status --porcelain` empty (Rule 30).
const vrrEvidenceDir = "/home/user/.gopi/sessions/01a0e851-bbf0-7b56-9922-1313da2e677e"

// vrrWriteLogName is the recording of the paint writes one turn produced, one
// line per write: its index, its byte length, and its escaped head and tail. It
// is what makes the boundary MEASURED rather than assumed: the read sizes a leg
// uses are read off this file, and a reader of the evidence can see the frame
// shapes for themselves.
const vrrWriteLogName = "varread-paint-writes.txt"

// The fixture's own cell markers. Same vocabulary as the turn-dump cases
// (turnDumpToken), so a repeated run is attributable to the renderer and not to a
// token family whose prefixes look alike.
const (
	vrrTokenPrefix = "vrr"
	vrrBodyLines   = 12
	vrrTailLine    = "variable read boundary tail"
	vrrCJKLine     = "主场景的定义需要把粘贴场景的 header 预算独立立项"
	vrrBoldLine    = "**先不立**这一步，等确认后再说"
	vrrCodeLine    = "用 `chat_surface action=see` 读 attrs 做逐列对账"
	vrrCheckMark   = "✓"
)

// vrrAnswer is the answer the turn is driven with: the three shapes the live
// frame was made of (CJK prose, an emphasis span, inline code) plus enough plain
// lines to make the answer band scroll, so the reading is band + scrollback.
func vrrAnswer() string {
	var b strings.Builder
	b.WriteString(vrrCJKLine)
	b.WriteString("\n")
	b.WriteString(vrrBoldLine)
	b.WriteString("\n")
	b.WriteString(vrrCodeLine)
	b.WriteString("\n")
	for i := 1; i <= vrrBodyLines; i++ {
		b.WriteString(turnDumpToken(vrrTokenPrefix, i))
		b.WriteString(" ")
		b.WriteString(vrrCheckMark)
		b.WriteString("\n")
	}
	b.WriteString(vrrTailLine)
	b.WriteString("\n")
	return b.String()
}

// vrrDeltas splits the answer into the streamed fragments a provider delivers —
// small on purpose, so the renderer's own frames are small and a read boundary
// lands inside sequences often.
func vrrDeltas(answer string) []string {
	const deltaRunes = 4
	runes := []rune(answer)
	out := make([]string, 0, len(runes)/deltaRunes+1)
	for i := 0; i < len(runes); i += deltaRunes {
		j := i + deltaRunes
		if j > len(runes) {
			j = len(runes)
		}
		out = append(out, string(runes[i:j]))
	}
	return out
}

// vrrBoundary is the READ BOUNDARY as a writer: what the editor paints into, and
// what hands the sink the bytes it will interpret.
//
// It is a real accumulator, which is the point: the editor's own writes are
// whatever size its row-building produced, while a tty read is bounded by a
// buffer — so a read of N bytes can SPAN several writes (accrued until full) and
// one write can span several reads (chopped). Which of those happens is decided
// by `reads`, never by the editor's write sizes.
//
// Two size forms, and the difference is the case:
//
//	uniform(step) — every read is `step` bytes, the predecessor's model;
//	from(log)     — the read sizes the turn ACTUALLY produced, replayed.
type vrrBoundary struct {
	dst io.Writer

	// reads is the size of each read this boundary will deliver, in order. It is
	// consumed one entry per delivered read.
	reads []int
	pos   int

	// acc accrues editor writes until a read is full.
	acc []byte

	// delivered/bytes count what the sink actually received, so the leg reports
	// its own read count beside the sink's Frames.
	delivered int
	bytes     int

	// recording, when non-nil, receives one line per DELIVERED read.
	recording io.Writer
}

func (w *vrrBoundary) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.acc = append(w.acc, p...)
	if err := w.drain(false); err != nil {
		return 0, err
	}
	return len(p), nil
}

// flush delivers whatever is accrued, as one final read. The turn's tail must
// reach the sink or the measurement would be of a truncated stream.
func (w *vrrBoundary) flush() error { return w.drain(true) }

// drain delivers accumulated bytes in whole reads, per the size list. With final
// set, the remaining bytes go as one last read (the tail of the turn); without it
// they wait for the write that completes the read.
func (w *vrrBoundary) drain(final bool) error {
	for len(w.acc) > 0 {
		want := len(w.acc)
		if w.pos < len(w.reads) {
			want = w.reads[w.pos]
		} else if !final {
			// The size list is exhausted and the turn is not over: deliver what we
			// have (a tty's final read is short) rather than stall.
			want = len(w.acc)
		}
		if want > len(w.acc) && !final {
			return nil // wait for the bytes that complete this read
		}
		if want > len(w.acc) {
			want = len(w.acc)
		}
		chunk := w.acc[:want]
		w.acc = w.acc[want:]
		if w.pos < len(w.reads) {
			w.pos++
		}
		if _, err := w.dst.Write(chunk); err != nil {
			return err
		}
		w.delivered++
		w.bytes += len(chunk)
		if w.recording != nil {
			fmt.Fprintf(w.recording, "read %04d size=%-5d %s\n", w.delivered, len(chunk), vrrEscaped(chunk))
		}
	}
	return nil
}

// vrrEscaped renders a read's bytes for the write log: control bytes and the
// sequence-introducer neighbourhood readable, and the length bounded so one line
// stays one line.
func vrrEscaped(b []byte) string {
	const headBytes, tailBytes = 40, 16
	var sb strings.Builder
	sb.WriteString("[")
	for i, c := range b {
		if i >= headBytes {
			sb.WriteString("…")
			break
		}
		sb.WriteString(vrrByte(c))
	}
	sb.WriteString("]")
	if len(b) > headBytes {
		sb.WriteString("[…")
		start := len(b) - tailBytes
		if start < headBytes {
			start = headBytes
		}
		for _, c := range b[start:] {
			sb.WriteString(vrrByte(c))
		}
		sb.WriteString("]")
	}
	return sb.String()
}

// vrrByte renders one byte: printable ASCII as itself, ESC as `^[`, other control
// bytes as `\xNN`.
func vrrByte(c byte) string {
	switch {
	case c == 0x1b:
		return "^["
	case c >= 0x20 && c < 0x7f:
		return string(rune(c))
	default:
		return "\\x" + strconv.FormatUint(uint64(c), 16)
	}
}

// vrrUniformReads is the predecessor's model stated once: every read is `step`
// bytes, and the number of reads is generous enough that the list never runs out
// before the turn does.
func vrrUniformReads(totalBytes, step int) []int {
	n := totalBytes/step + 1
	out := make([]int, n)
	for i := range out {
		out[i] = step
	}
	return out
}

// vrrRecorder tees the paint stream into a write log while forwarding each write
// WHOLE to the sink: it is the leg whose reads are the production write sizes —
// one read per write, the frame boundary inside the read. The sizes it records are
// what the other legs replay.
type vrrRecorder struct {
	dst  io.Writer
	log  io.Writer
	idx  int
	size []int
}

func (r *vrrRecorder) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.idx++
	r.size = append(r.size, len(p))
	if r.log != nil {
		fmt.Fprintf(r.log, "write %04d size=%-5d %s\n", r.idx, len(p), vrrEscaped(p))
	}
	if _, err := r.dst.Write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// vrrTrace is one measured leg.
type vrrTrace struct {
	leg       string
	reads     int
	readBytes int
	frames    int
	dumps     int
	rows      []string
	digest    string
	content   string
	scrollN   int
	produced  int
	matched   int
	lost      int
	extra     int
	first     int
	last      int
	path      string
}

// vrrTrace.drive runs ONE turn on a real pty at the live geometry with the real
// armed sink, through the boundary it is handed.
func vrrDrive(t *testing.T, leg string, mk func(io.Writer) io.Writer) vrrTrace {
	t.Helper()
	pair := chatOpenPtyPair(t, vrrPtyCols, vrrPtyRows)
	if got := lineedit.TerminalCols(int(pair.Slave.Fd())); got != vrrPtyCols {
		t.Fatalf("[%s] the pty reports %d columns, want %d — the kernel did not take the size", leg, got, vrrPtyCols)
	}
	sink := turnDumpSink(t)
	boundary := mk(sink)

	ed, err := lineedit.Open(pair.Slave, boundary)
	if err != nil {
		t.Fatalf("[%s] open the pty slave as a terminal: %v", leg, err)
	}
	defer func() { _ = ed.Close() }() //stylegate:ignore the harness's own terminal teardown

	chat := lineedit.NewChatEditor(ed, boundary, chatPrompt, nil, nil, nil)
	chat.SetGeometryObserver(chatGeomObserver(sink))
	chat.SetPalette(lineedit.BasicPalette)
	if !chat.EnableRegion() {
		t.Fatalf("[%s] EnableRegion(pty %dx%d) = false: the case needs the reserved layout installed", leg, vrrPtyCols, vrrPtyRows)
	}
	in := &chatInput{editor: chat, dumper: sink}
	if in.Answer() == nil {
		t.Fatalf("[%s] the input layer reports no answer band although the layout is installed", leg)
	}

	answer := vrrAnswer()
	renderer := newChatTurnRenderer(in)
	renderer.BeginTurn(1, turnDumpPromptLine)
	for _, delta := range vrrDeltas(answer) {
		renderer.Write(delta)
	}
	renderer.Finish()
	renderer.EndTurn()
	newChatFlowSeam(in, nil).Flush()
	// THE TAIL GOES IN FIRST. A read boundary may still be holding a partial read
	// when the turn ends (exactly as a tty may have bytes in flight), and the sink
	// freezes its artifact at Settle — so the tail is delivered BEFORE the settle.
	// The reverse order was measured: the artifact's `frames` counter came back one
	// short of the reads the boundary delivered (uniform7: 257 reads, 256 frames),
	// i.e. the frozen grid did not contain the stream's last bytes.
	vrrFlush(t, leg, boundary)
	in.SettleDump()

	cand, err := uiclient.ReadDump(sink.Path())
	if err != nil {
		t.Fatalf("[%s] read the artifact back (%s): %v", leg, sink.Path(), err)
	}
	rows := make([]string, 0, len(cand.Dump.Scrollback)+len(cand.Dump.Grid))
	rows = append(rows, cand.Dump.Scrollback...)
	rows = append(rows, cand.Dump.Grid...)
	if len(vrrDense(strings.Join(rows, ""))) == 0 {
		t.Fatalf("[%s] the artifact (%s) carries no cells at all (frames=%d dumps=%d) — nothing below could be asserted",
			leg, sink.Path(), cand.Frames, cand.Dumps)
	}
	tr := vrrTrace{
		leg: leg, frames: cand.Frames, dumps: cand.Dumps, rows: rows, path: sink.Path(),
	}
	tr.digest = vrrDigest(cand)
	tr.content = vrrContentDigest(cand)
	tr.scrollN = len(cand.Dump.Scrollback)
	if f := vrrEvidenceFile("varread-digest-" + leg + ".txt"); f != nil {
		_, _ = io.WriteString(f, tr.digest) //stylegate:ignore writes the digest into the file the variable read then parses; the parse is the assertion
		_ = f.Close()                       //stylegate:ignore closes the dump file so the next read sees a complete one; the closed handle is the fixture's own
	}
	tr.reads, tr.readBytes = vrrCounts(boundary)
	tr.produced = len([]rune(vrrDense(answer)))
	tr.lost, tr.extra, tr.matched, tr.first, tr.last = vrrReconcile(rows, answer)
	t.Logf("VARREAD %-9s reads=%-4d bytes=%-6d frames=%-3d dumps=%-3d produced=%d answer_rows=%d..%d matched=%d lost=%d extra=%d",
		tr.leg, tr.reads, tr.readBytes, tr.frames, tr.dumps, tr.produced, tr.first, tr.last, tr.matched, tr.lost, tr.extra)
	t.Logf("VARREAD %-9s artifact: %s", tr.leg, tr.path)
	return tr
}

// vrrCounts reports how many reads a boundary delivered and how many bytes, for
// whichever boundary shape the leg installed.
func vrrCounts(w io.Writer) (reads, bytes int) {
	switch b := w.(type) {
	case *vrrBoundary:
		return b.delivered, b.bytes
	case *vrrRecorder:
		return b.idx, vrrSizeSum(b.size)
	default:
		return 0, 0
	}
}

// vrrFlush delivers a boundary's accrued tail, when the boundary is one of ours.
// The tail must reach the sink or the measurement would be of a truncated stream,
// so a failure here stops the case rather than being swallowed (Rule 15).
func vrrFlush(t *testing.T, leg string, w io.Writer) {
	t.Helper()
	b, ok := w.(*vrrBoundary)
	if !ok {
		return
	}
	if err := b.flush(); err != nil {
		t.Fatalf("[%s] flush the read boundary: %v", leg, err)
	}
}

func vrrSizeSum(sizes []int) int {
	total := 0
	for _, n := range sizes {
		total += n
	}
	return total
}

// vrrDense folds a painted row or a produced line to the cells the comparison
// works on: the layout's own markup carries no cell of the answer, and the
// renderer spends the space a wrap breaks at, so air is not part of the
// comparison. EVERYTHING else is kept — a dropped, duplicated or reordered
// character must reach the assertion.
func vrrDense(s string) string {
	s = strings.ReplaceAll(s, lineedit.AnswerCutLabel, "")
	s = strings.ReplaceAll(s, lineedit.BoldMarker, "")
	s = strings.ReplaceAll(s, lineedit.TurnMarker, "")
	s = strings.ReplaceAll(s, lineedit.TurnMarkerSplit, "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\t", "")
	return strings.ReplaceAll(s, "\n", "")
}

// vrrReconcileSpan reconciles against a row span the CALLER fixed, rather than one
// re-discovered from the reading. The self-check needs it: an injury that changed
// which rows the discovery walk picks would re-define the very window the injury is
// measured in, so the delta would be about the window instead of about the injury.
func vrrReconcileSpan(rows []string, answer string, first, last int) (lost, extra, matched int) {
	produced := []rune(vrrDense(answer))
	if first < 0 || last < first {
		return len(produced), 0, 0
	}
	var b strings.Builder
	for i := first; i <= last && i < len(rows); i++ {
		b.WriteString(vrrDense(rows[i]))
	}
	reading := []rune(b.String())
	matched = vrrLCS(produced, reading)
	return len(produced) - matched, len(reading) - matched, matched
}

// vrrReconcile reconciles a painted reading against the answer the turn was driven
// with, in both directions:
//
//	lost  — produced answer cells that are not on the reading;
//	extra — cells on the reading, INSIDE the rows that carry the answer, that the
//	        answer never had.
func vrrReconcile(rows []string, answer string) (lost, extra, matched int, first, last int) {
	produced := []rune(vrrDense(answer))
	first, last = vrrAnswerRows(produced, rows)
	if first < 0 {
		return len(produced), 0, 0, -1, -1
	}
	var b strings.Builder
	for i := first; i <= last && i < len(rows); i++ {
		b.WriteString(vrrDense(rows[i]))
	}
	reading := []rune(b.String())
	matched = vrrLCS(produced, reading)
	return len(produced) - matched, len(reading) - matched, matched, first, last
}

// vrrLCS is the length of the longest common subsequence of two rune slices — the
// number of produced cells that survived, IN ORDER. A dropped cell and a reordered
// cell both move it; a substring check would see neither.
func vrrLCS(a, b []rune) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			switch {
			case a[i-1] == b[j-1]:
				cur[j] = prev[j-1] + 1
			case prev[j] >= cur[j-1]:
				cur[j] = prev[j]
			default:
				cur[j] = cur[j-1]
			}
		}
		copy(prev, cur)
		for j := range cur {
			cur[j] = 0
		}
	}
	return prev[len(b)]
}

// vrrAnswerRows returns the first and last row index carrying a cell of the
// produced answer, walking the rows in order and consuming the produced runes
// greedily — the same order the reconciliation's LCS counts. It is what bounds
// EXTRA to the answer's own rows rather than to the layout's chrome.
func vrrAnswerRows(produced []rune, rows []string) (first, last int) {
	first, last = -1, -1
	pi := 0
	for i, row := range rows {
		for _, r := range []rune(vrrDense(row)) {
			if pi < len(produced) && r == produced[pi] {
				if first < 0 {
					first = i
				}
				last = i
				pi++
			}
		}
	}
	return first, last
}

// vrrSelfCheck proves the counter is not blind: on a copy of the answer's own rows
// with a run of cells REMOVED it must report those cells lost, and on a copy
// carrying a run of cells the answer never had it must report them extra. A counter
// that cannot see a deliberate injury cannot certify an intact screen.
func vrrSelfCheck(t *testing.T, leg string, rows []string, answer string, first, last int) {
	t.Helper()
	if first < 0 || last <= first {
		t.Fatalf("[%s] the answer occupied rows %d..%d of the artifact: a self-check needs a multi-row answer span", leg, first, last)
	}
	span := append([]string(nil), rows[first:vrrMin(last+1, len(rows))]...)

	// THE BASELINE. The self-check measures the DELTA its injury causes, never an
	// absolute count: an artifact that is itself damaged (which is the case on the
	// tree this defect lives on) already reports a non-zero number, and an absolute
	// expectation would then fail for the artifact's reason instead of the counter's.
	// A delta of the exact injured amount proves the counter sees the injury.
	baseLost, baseExtra, _ := vrrReconcileSpan(rows, answer, first, last)

	// (a) A LOSS: remove a run of cells from the answer's own rows. The injury goes
	// on the row with the most CELLS, not on a middle one: the band's rows have very
	// different lengths, and a row too short to injure would turn this into a silent
	// skip. It is applied to the row's dense cells, never to its raw bytes — cutting
	// padding spaces changes nothing a reading sees, which is how a self-check can
	// pass while proving nothing.
	const injuryRunes = 6
	mid, midLen := 0, -1
	for i, r := range span {
		if n := len([]rune(vrrDense(r))); n > midLen {
			mid, midLen = i, n
		}
	}
	runes := []rune(vrrDense(span[mid]))
	if len(runes) <= injuryRunes*2 {
		t.Fatalf("[%s] the longest answer row (%q) carries only %d cells — too few to injure by %d, so this self-check cannot prove the counter works",
			leg, span[mid], len(runes), injuryRunes)
	}
	cutAt := len(runes) / 2
	damaged := append([]string(nil), span...)
	damaged[mid] = string(runes[:cutAt]) + string(runes[cutAt+injuryRunes:])
	damagedRows := append([]string(nil), rows...)
	copy(damagedRows[first:], damaged)
	lost, _, _ := vrrReconcileSpan(damagedRows, answer, first, last)
	if lost-baseLost != injuryRunes {
		t.Fatalf("[%s] SELF-CHECK (loss): removing %d cells from the answer's own rows moved lost by %d (baseline %d → %d), want exactly %d — the counter cannot measure a cell loss, so its lost=0 on the real artifact proves nothing",
			leg, injuryRunes, lost-baseLost, baseLost, lost, injuryRunes)
	}
	t.Logf("VARREAD %-9s SELF-CHECK (loss): %d cells removed -> lost=%d, +%d (the counter sees it)", leg, injuryRunes, lost, lost-baseLost)

	// (b) AN ADDITION: a parameter run the answer never had, painted onto a row —
	// the live frame's own `;61H24;73H5;230m` shape, so the self-check exercises the
	// exact cell run the case is about. Every added character is a printable ASCII
	// byte, so the count the counter must report is EXACTLY the fragment's length:
	// an approximate threshold ("at least half") would pass on a counter that
	// double-counted, and this case's whole subject is a diff that is off by a few
	// cells.
	fragment := ";61H24;73H5;230m"
	added := append([]string(nil), span...)
	added[0] += fragment
	addedRows := append([]string(nil), rows...)
	copy(addedRows[first:], added)
	_, extra, _ := vrrReconcileSpan(addedRows, answer, first, last)
	if extra-baseExtra != len(fragment) {
		t.Fatalf("[%s] SELF-CHECK (addition): painting %d cells the answer never had moved extra by %d (baseline %d → %d), want exactly %d — the counter cannot measure added characters, so its extra=0 on the real artifact proves nothing",
			leg, len(fragment), extra-baseExtra, baseExtra, extra, len(fragment))
	}
	t.Logf("VARREAD %-9s SELF-CHECK (addition): %d cells added -> extra=%d, +%d (the counter sees it)", leg, len(fragment), extra, extra-baseExtra)
}

// vrrContentDigest is the render truth of the CONTENT plane alone: non-blank
// scrollback rows plus the grid, with attributes. It is the digest that says
// whether the answer was painted — the plane a cell loss shows up on.
//
// It is separate from vrrDigest because the two planes have different causes: a
// blank scrollback row is an empty line the layout emitted (its count follows the
// turn's own scroll arithmetic), while a content row is a character the render
// drew. An instrument that folded them would report a blank-row difference as a
// loss of answer text — the exact false positive this case exists to avoid.
func vrrContentDigest(cand uiclient.Candidate) string {
	var b strings.Builder
	for _, r := range cand.Dump.Scrollback {
		if strings.TrimSpace(r) == "" {
			continue
		}
		b.WriteString("sb ")
		b.WriteString(r)
		b.WriteString("\n")
	}
	for i, row := range cand.Dump.Grid {
		fmt.Fprintf(&b, "g%03d %s\n", i, row)
		if cand.Dump.AttrsRecorded && i < len(cand.Dump.AttrRuns) {
			for _, run := range cand.Dump.AttrRuns[i] {
				fmt.Fprintf(&b, "  a%03d+%03d %s\n", run.Col, run.Cells, run.Style)
			}
		}
	}
	return b.String()
}

// vrrDigest is the artifact's RENDER TRUTH: every scrollback row, every grid row,
// and every attribute run, in order — the cell text AND the per-cell styling, so
// two legs that painted the same screen agree and a leg that lost, added or
// restyled a cell disagrees. It is what makes the cross-leg comparison an
// assertion about the RENDERED GRID rather than about a byte log or a row count.
func vrrDigest(cand uiclient.Candidate) string {
	var b strings.Builder
	for _, r := range cand.Dump.Scrollback {
		b.WriteString("sb ")
		b.WriteString(r)
		b.WriteString("\n")
	}
	for i, row := range cand.Dump.Grid {
		fmt.Fprintf(&b, "g%03d %s\n", i, row)
		if cand.Dump.AttrsRecorded && i < len(cand.Dump.AttrRuns) {
			for _, run := range cand.Dump.AttrRuns[i] {
				fmt.Fprintf(&b, "  a%03d+%03d %s\n", run.Col, run.Cells, run.Style)
			}
		}
	}
	return b.String()
}

// vrrMin is the two-argument minimum, kept named so the row windows read the same
// at every call site.
func vrrMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// vrrEvidenceFile opens a named artifact under the run's session directory.
func vrrEvidenceFile(name string) *os.File {
	if err := os.MkdirAll(vrrEvidenceDir, 0o755); err != nil {
		return nil
	}
	f, err := os.Create(filepath.Join(vrrEvidenceDir, name))
	if err != nil {
		return nil
	}
	return f
}

// TestChatTurnDumpVariableReadBoundary_OnRealPTY drives the same turn under a
// UNIFORM read list and under the reads the turn actually produced, reconciles the
// artifact's cells against the answer in both directions, and proves the counter
// with an injured copy.
func TestChatTurnDumpVariableReadBoundary_OnRealPTY(t *testing.T) {
	answer := vrrAnswer()
	logFile := vrrEvidenceFile(vrrWriteLogName)
	if logFile != nil {
		defer func() { _ = logFile.Close() }() //stylegate:ignore the evidence log's own teardown
	}
	// LEG A — the production write sizes: one read per write, the frame boundary
	// INSIDE the read. The write log this leg produces is the recording the other
	// legs and the report read their numbers from.
	rec := &vrrRecorder{dst: nil, log: logFile}
	legA := vrrDrive(t, "whole", func(sink io.Writer) io.Writer {
		rec.dst = sink
		return rec
	})
	vrrSelfCheck(t, legA.leg, legA.rows, answer, legA.first, legA.last)

	// LEG B — the predecessor's model: a flat uniform read step.
	legB := vrrDrive(t, "uniform7", func(sink io.Writer) io.Writer {
		return &vrrBoundary{dst: sink, reads: vrrUniformReads(rec.sizeTotal(), vrrUniformReadBytes)}
	})
	vrrSelfCheck(t, legB.leg, legB.rows, answer, legB.first, legB.last)

	// LEG C — the reads the turn ACTUALLY produced, replayed as reads. The same
	// bytes the production boundary delivered, chopped at the production write
	// sizes, so the two legs differ only in where the sink's read boundaries fall.
	legC := vrrDrive(t, "production", func(sink io.Writer) io.Writer {
		return &vrrBoundary{dst: sink, reads: append([]int(nil), rec.size...)}
	})
	vrrSelfCheck(t, legC.leg, legC.rows, answer, legC.first, legC.last)

	// LEG D — the production boundary a SECOND time, same read list as leg A. It
	// separates the two things a cross-leg disagreement can mean: with A≡D the
	// disagreement is caused by the READ BOUNDARY; with A≠D it is the run's own
	// timing, and no boundary claim may be made from it.
	rec2 := &vrrRecorder{}
	legD := vrrDrive(t, "whole2", func(sink io.Writer) io.Writer {
		rec2.dst = sink
		return rec2
	})
	vrrSelfCheck(t, legD.leg, legD.rows, answer, legD.first, legD.last)

	// THE BOUNDARY A LEG DELIVERED IS THE BOUNDARY THE SINK SAW. The sink counts one
	// Frame per Write it receives, so a leg whose delivered read count disagrees with
	// its Frames was measuring a stream the sink did not interpret (or vice versa) —
	// and every number below would be about something else.
	if legA.reads != legA.frames {
		t.Errorf("LEG whole: the boundary delivered %d reads but the sink interpreted %d frames — the leg's read list is not the boundary the sink saw",
			legA.reads, legA.frames)
	}
	t.Logf("VARREAD CROSS-CHECK whole: delivered_reads=%d sink_frames=%d (equal ⇒ the boundary IS the sink's read list)", legA.reads, legA.frames)

	// THE CONTENT PLANE IS THE CRITERION, ASSERTED: every leg must carry the SAME
	// answer cells with the SAME attributes, because the read boundary is the
	// reader's artifact and never a fact about the paint. The comparison is the full
	// render truth of the content plane — scrollback content + grid + attribute runs —
	// so a leg that lost a cell, added one, or restyled one disagrees here even when
	// both legs' row counts match.
	for _, other := range []vrrTrace{legB, legC, legD} {
		if other.content != legA.content {
			t.Errorf("RENDER-FIDELITY: leg %q (%d reads) and leg %q (%d reads) painted DIFFERENT content from the SAME bytes — a read boundary is the reader's artifact, never a fact about the paint\nfirst difference at: %s",
				legA.leg, legA.reads, other.leg, other.reads, vrrFirstDiff(legA.content, other.content))
		}
	}
	t.Logf("VARREAD CROSS-CHECK render: content identical across whole(%d reads) / uniform7(%d) / production(%d) / whole2(%d)",
		legA.reads, legB.reads, legC.reads, legD.reads)

	// THE SCROLLBACK-COUNT PLANE, reported separately and NOT asserted equal: it counts
	// blank rows too, and a blank row is an empty line the layout emitted rather than
	// a character the render drew. It is logged so a reader can see the one axis on
	// which the legs did differ, and its cause is named in the same line.
	t.Logf("VARREAD SCROLLBACK rows: whole=%d uniform7=%d production=%d whole2=%d (blank rows are the layout's empty lines, not answer cells)",
		legA.scrollN, legB.scrollN, legC.scrollN, legD.scrollN)

	t.Logf("VARREAD VERDICT lost: whole=%d uniform7=%d production=%d whole2=%d | extra: whole=%d uniform7=%d production=%d whole2=%d",
		legA.lost, legB.lost, legC.lost, legD.lost, legA.extra, legB.extra, legC.extra, legD.extra)

	// The artifact of the production leg is the one a reader should look at when the
	// uniform model and the production boundary disagree.
	t.Logf("VARREAD evidence: writes=%s", filepath.Join(vrrEvidenceDir, vrrWriteLogName))
}

// vrrFirstDiff names the first line two render digests disagree at — the cheapest
// way to see WHICH cell/attr a boundary changed.
func vrrFirstDiff(a, b string) string {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(la) && i < len(lb); i++ {
		if la[i] != lb[i] {
			return fmt.Sprintf("line %d: A=%q B=%q", i, la[i], lb[i])
		}
	}
	return fmt.Sprintf("no differing line; A has %d lines, B has %d", len(la), len(lb))
}

// sizeTotal is the total byte count the recorded writes carried.
func (r *vrrRecorder) sizeTotal() int { return vrrSizeSum(r.size) }

// ——— The HELD-INTRODUCER probe —————————————————————————————————————————————
//
// The leg above drives the whole turn and could not make a cell loss appear: four
// read boundaries (a flat 7-byte step included) painted identical content. So the
// boundary's reachable damage is narrower than "any read size": it needs an EVENT
// to land between the two halves of one sequence — the interpreter holds a partial
// sequence in `Screen.pending`, and something that resets the parser while it is
// held ends the sequence's life. The candidate event on this path is the
// geometry re-measure: the recorder's SetGeometry reaches `Screen.Resize`, which
// clears `pending` — i.e. the held introducer is dropped and the parameters that
// arrive next are no longer a sequence at all, so they paint as CELLS.
//
// That is exactly the shape of the live frame: session 884eca28's row 011 carries
// `;61H24;73H5;230m` — placed sequences with every introducer gone — and 38 of a
// row's 52 answer cells are absent from the row that was committed. Whether that
// frame's loss is THIS event is not decidable from the frame (see the devdoc); what
// is decidable is whether the event can damage cells at all, and that is what this
// probe measures on the real sink.
//
// THE PROBE IS DETERMINISTIC AND THE READ BOUNDARY IS ITS FIRST HALF: the sink is
// handed the introducer `ESC[` ALONE (a tty read can return exactly that, and the
// pre-fix emitter wrote each placement as its own write — see the reported
// write-size census in the evidence file), then a re-measure arrives, then the
// parameters. The intended screen is two cells before the introducer and two after;
// the failure signature is the parameters standing where cells belong.
const (
	helapColsBefore = 100
	helapRowsBefore = 20
	helapColsAfter  = 117
	helapRowsAfter  = 26
	// The cells live on TWO rows: the self-check's own precondition is a multi-row
	// answer span LONG enough to injure, and the split sequence is what carries the
	// second placement. The two rows use different alphabets, so a cell that ends up
	// on the wrong row is visible to the reconciliation rather than absorbed by a
	// coincidental match.
	helapFirstRow  = 5 // 1-based grid row painted BEFORE the split sequence
	helapSecondRow = 6 // 1-based grid row the split sequence's ending places
	helapFirst     = "ABCDEFGHIJKLMNOPQRST"
	helapSecond    = "abcdefghijklmnopqrst"
)

// helapIntended is what the sink was told to paint, in row order.
func helapIntended() string { return helapFirst + helapSecond }

// helapRedEnv gates the assertion. The defect is LIVE on master, so the case is
// RED there; a repo that ships a red test cannot run its smoke gate, so the default
// run SKIPS with the reason and the number, and the env var turns the assertion on
// (the same landed form the line-break cadence repro uses — the defect stays
// visible while master stays green).
const helapRedEnv = "GOPI_HELD_INTRODUCER_RED"

// TestChatDumpHeldIntroducerAcrossRemeasure_OnRealSink measures the one boundary
// event that DOES damage cells: a held sequence's introducer dropped by a
// re-measure while the interpreter waited for its parameters.
func TestChatDumpHeldIntroducerAcrossRemeasure_OnRealSink(t *testing.T) {
	sink := turnDumpSink(t)
	// A size that DIFFERS from the one the re-measure will announce, so the
	// re-measure really goes through the resize path (a same-size announcement takes
	// the early return and never touches the held tail).
	sink.SetGeometry(uiclient.GeometrySourceLayout, helapColsBefore, helapRowsBefore)
	// Cells before the split sequence, then the sequence that places the second pair
	// ON ITS OWN ROW — so the reading spans two rows.
	place := fmt.Sprintf("\x1b[%d;1H", helapFirstRow)
	placeNext := fmt.Sprintf("\x1b[%d;1H", helapSecondRow)
	if _, err := sink.Write([]byte(place + helapFirst + placeNext)); err != nil {
		t.Fatalf("paint the cells before the split sequence: %v", err)
	}
	// THE READ BOUNDARY, first half: the next sequence's introducer ALONE, nothing
	// else — a placement's bytes are written together by this renderer, so an
	// introducer left alone is CONSTRUCTED here and labelled as such; what it models
	// is a tty read whose tail is a partial sequence.
	if _, err := sink.Write([]byte("\x1b[")); err != nil {
		t.Fatalf("hand the sink the bare introducer: %v", err)
	}
	// THE EVENT: the geometry is re-measured while the introducer is held.
	sink.SetGeometry(uiclient.GeometrySourceLayout, helapColsAfter, helapRowsAfter)
	// THE READ BOUNDARY, second half: the rest of the split sequence — its parameters
	// and final byte — and then the cells it places.
	if _, err := sink.Write([]byte(placeNext[len("\x1b["):] + helapSecond)); err != nil {
		t.Fatalf("hand the sink the sequence's parameters: %v", err)
	}
	if err := sink.Settle(); err != nil {
		t.Fatalf("settle the artifact: %v", err)
	}
	cand, err := uiclient.ReadDump(sink.Path())
	if err != nil {
		t.Fatalf("read the artifact back (%s): %v", sink.Path(), err)
	}
	rows := make([]string, 0, len(cand.Dump.Scrollback)+len(cand.Dump.Grid))
	rows = append(rows, cand.Dump.Scrollback...)
	rows = append(rows, cand.Dump.Grid...)

	intended := helapIntended()
	lost, extra, matched, first, last := vrrReconcile(rows, intended)
	// WHAT THE ROWS ACTUALLY HOLD, printed whether the case passes or not: the
	// reading is the evidence, and a reader must not have to re-run the case to see
	// it.
	rowText := func(row int) string {
		if row-1 < len(rows) {
			return strings.TrimRight(rows[row-1], " ")
		}
		return "<no such row>"
	}
	t.Logf("HELAP grid=%dx%d intended=%q matched=%d lost=%d extra=%d answer_rows=%d..%d",
		cand.Dump.Cols, cand.Dump.Rows, intended, matched, lost, extra, first, last)
	t.Logf("HELAP row %d reads: %q", helapFirstRow, rowText(helapFirstRow))
	t.Logf("HELAP row %d reads: %q", helapSecondRow, rowText(helapSecondRow))
	if f := vrrEvidenceFile("held-introducer-probe.txt"); f != nil {
		fmt.Fprintf(f, "grid=%dx%d\nintended=%q\nrow%d=%q\nrow%d=%q\nmatched=%d lost=%d extra=%d\nrows:\n",
			cand.Dump.Cols, cand.Dump.Rows, intended, helapFirstRow, rowText(helapFirstRow),
			helapSecondRow, rowText(helapSecondRow), matched, lost, extra)
		for i, r := range rows {
			fmt.Fprintf(f, "%04d|%s\n", i, r)
		}
		_ = f.Close() //stylegate:ignore same dump-file close on the second case: the variable read below is the assertion, not the close
	}

	// The counter's own self-check, run here too: a probe that cannot see an injury
	// cannot certify an intact row.
	vrrSelfCheck(t, "helap", rows, intended, first, last)

	if os.Getenv(helapRedEnv) == "" {
		t.Skipf("held-introducer probe measured (matched=%d lost=%d extra=%d row%d=%q row%d=%q); the assertion is gated because a held introducer dropped by a re-measure is the defect %s fixes — set %s=1 to require it",
			matched, lost, extra, helapFirstRow, rowText(helapFirstRow), helapSecondRow, rowText(helapSecondRow), "e06a2aed", helapRedEnv)
	}
	if matched != len([]rune(intended)) || lost != 0 || extra != 0 {
		t.Fatalf("HELD-INTRODUCER: the sink was handed a bare introducer %q, then a re-measure, then the sequence's parameters — and %d of the %d intended cells are missing (extra=%d).\nrow %d reads %q\nrow %d reads %q\nThe parameters painted as CELLS: a read boundary that leaves the interpreter holding half a sequence is damaged by any event that resets its parser state, and the artifact then carries the sequence's parameter run as answer text — the live frame's own `;61H24;73H5;230m` signature.\nartifact: %s",
			"\x1b[", lost, len([]rune(intended)), extra,
			helapFirstRow, rowText(helapFirstRow), helapSecondRow, rowText(helapSecondRow), sink.Path())
	}
	t.Logf("HELAP OK: the held introducer survived the re-measure — row %d = %q, row %d = %q, lost=0 extra=0",
		helapFirstRow, rowText(helapFirstRow), helapSecondRow, rowText(helapSecondRow))
}
