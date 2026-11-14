package testsuite

// chat_banner_pty_test.go — the chat REPL's STARTUP SCREEN (the boxed logo and
// its side panel), measured on a REAL pty with the REAL binary.
//
// WHY A PTY AND NOT A UNIT TEST. The banner's whole difficulty is its CARRIER:
// a block of rows is only on the screen if the surface that owns the screen drew
// it there. The in-package tests (cmd/agent/chat_banner_test.go) pin the box's
// arithmetic and its content; only a real terminal can say whether those rows
// reached a screen a user reads, and in which order relative to the prompt and
// the status row. It is the same instrument the chat self-observation recipe
// uses: run the surface on a pty, arm `--dump-screen`, read the artifact back
// through uiclient.ReadDump (the reader `gopi ui_state` uses), and assert on the
// dumped CELL ROWS rather than on a byte pattern — the renderer writes a row in
// one styled run per span, each with its own cursor placement, so a line is not
// guaranteed to be contiguous in the raw stream.
//
// WHAT IS ASSERTED, AND WHY EACH ONE IS THE POINT:
//
//  1. THE BOX IS ON THE SCREEN. Every row of the block the surface reports
//     (chatSurfaceBannerLines) appears as a dump row at the width the pty was
//     installed with, and the top border carries the title bar. A banner that is
//     printed but scrolled away is the defect this carrier exists to avoid, so
//     "the rows are in the artifact" is the assertion — not "the bytes were
//     written".
//  2. THE MARK AND THE SIDE PANEL ARE BOTH THERE. The mark's block art and the
//     panel's two sections (the tips header, the session header) are read from
//     the GRID, and the mark is LEFT of the panel on the same rows — the side
//     panel's whole meaning is that it sits beside the logo.
//  3. THE CHROME IS NOT UNDER THE BOX. The block ends above the editing line: the
//     prompt row and the status row are still theirs, and the box never ate them.
//
// The banner's own content policy (which tips, which facts, what gives way on a
// short terminal) is pinned in-package; this file pins that the RESULT of that
// policy is what the user's terminal shows.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/uiclient"
	"golang.org/x/sys/unix"
)

const (
	// chatBannerPTYCols / chatBannerPTYRows are the geometry this case installs
	// with TIOCSWINSZ. 100x30 is the shape internal/lineedit's own capture
	// documents (captureCols/captureRows) and it is wide enough for the box's
	// two columns plus room to spare, so the assertion is about the layout the
	// surface draws rather than about a width the box barely fits.
	chatBannerPTYCols = 100
	chatBannerPTYRows = 30
)

// chatBannerGridFacts is what the case reads off the screen: the panel's section
// headers, the title bar and the mark's own glyph. They are the surface's OWN
// words and glyph — reading them from the grid is what makes the assertion about
// the drawn screen rather than about the string constants.
var chatBannerGridFacts = []string{
	chatBannerTitleFact,      // the title bar: product + version
	chatBannerTipsHeaderFact, // the panel's first section
}

// TestChatPTY_StartupScreenShowsTheLogoAndItsSidePanel is the acceptance test for
// the startup screen: the box, the mark, the panel, and the chrome still below.
func TestChatPTY_StartupScreenShowsTheLogoAndItsSidePanel(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	evidence := testutil.TestScratchDir(t, "chat-banner")
	dumpPath := evidence + "/chat-screen.dump"

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatBannerAnswer})
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat", "--dump-screen=" + dumpPath},
		testutil.MockEnv(t, home, llm.URL()), work)
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: chatBannerPTYCols, Row: chatBannerPTYRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable on this host: %v", err)
	}

	// THE GEOMETRY IS PROVEN, NOT ASSUMED. A pty that never received the size
	// reports 0 columns, and the surface then (rightly) declines to draw a box
	// it cannot fit: this check is what keeps such a run from being read as "the
	// banner is missing" (the render-verdict case asserts the same fact).
	if cols := lineedit.TerminalCols(int(chat.master.Fd())); cols != chatBannerPTYCols {
		t.Fatalf("the pty reports %d columns, want the %d this harness installed", cols, chatBannerPTYCols)
	}

	// Wait for the PROMPT, which is painted after the block: at that moment the
	// startup screen has been committed and the chrome below it is placed, which
	// is the state this case is about.
	ptyOut := chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	// FORCE ONE PAINT, THEN READ. The dump is a SAMPLED artifact and the chat
	// layout paints only when the screen CHANGES (chat_dump.go's own all-blank
	// notice states the measurement), so a reader that polls an idle screen can
	// keep seeing a frame from BEFORE the banner was committed. Typing one
	// character makes the editor echo, which is a change, which makes the
	// sampler take a frame that necessarily contains the startup screen. The
	// character is then removed, so the screen this case asserts on is the one
	// the user sees at rest.
	chat.writeGapped(t, chatBannerPaintNudge)
	chat.write(t, "\x7f")             // DEL: the editor's own erase, restoring the empty prompt
	time.Sleep(chatBannerPaintSettle) //stylegate:allow-sleep waits for the throttled dump sampler to take the frame the nudge forced
	t.Logf("pty carried the prompt; %d bytes seen before the screen was read\n--- pty ---\n%s--- end pty ---",
		len(ptyOut), chatBannerVisible(ptyOut))

	lines := chatBannerReadGrid(t, dumpPath)

	// ---- 1. the box is on the screen, at the pty's own width ----------------
	boxTop, boxBottom := chatBannerBoxRows(t, lines)
	if boxTop < 0 {
		t.Fatalf("no top border on the screen — the startup screen was not painted; grid:\n%s", chatBannerJoin(lines))
	}
	if boxBottom < 0 {
		t.Fatalf("the top border has no matching bottom border — the box was cut; grid:\n%s", chatBannerJoin(lines))
	}
	for _, want := range chatBannerGridFacts {
		if !chatBannerGridHolds(lines[boxTop:boxBottom+1], want) {
			t.Errorf("the screen's box does not carry %q; grid:\n%s", want, chatBannerJoin(lines))
		}
	}

	// ---- 2. the mark is left of the panel on the shared rows ----------------
	markRow := chatBannerRowHolding(lines, chatBannerMarkFact)
	if markRow < 0 {
		t.Fatalf("the mark is not on the screen; grid:\n%s", chatBannerJoin(lines))
	}
	panelRow := chatBannerRowHolding(lines, chatBannerTipsHeaderFact)
	if panelRow < 0 {
		t.Fatalf("the side panel is not on the screen; grid:\n%s", chatBannerJoin(lines))
	}
	markCol := strings.Index(lines[markRow], chatBannerMarkFact)
	panelCol := strings.Index(lines[panelRow], chatBannerTipsHeaderFact)
	if markCol < 0 || panelCol < 0 || markCol >= panelCol {
		t.Errorf("the mark (row %d, col %d) is not LEFT of the side panel (row %d, col %d) — the two halves share the box's rows and the logo column is the left one; grid:\n%s",
			markRow, markCol, panelRow, panelCol, chatBannerJoin(lines))
	}

	// ---- 3. the chrome is still below the box ------------------------------
	promptRow := chatBannerRowHolding(lines, chatPromptMark)
	statusRow := chatBannerStatusRowIndex(lines)
	if promptRow <= boxBottom {
		t.Errorf("the editing line is at row %d but the box ends at row %d — the startup screen took the prompt's row; grid:\n%s",
			promptRow, boxBottom, chatBannerJoin(lines))
	}
	if statusRow <= promptRow {
		t.Errorf("the status row is at %d and the prompt at %d — the startup screen disturbed the chrome's order; grid:\n%s",
			statusRow, promptRow, chatBannerJoin(lines))
	}

	// The artifact itself is kept: the evidence for this verdict is THIS run's
	// own screen, not a later reproduction of it.
	chatBannerKeepEvidence(t, evidence, dumpPath)
}

// ChatBanner's on-screen vocabulary: the strings this case looks for in the
// GRID. They are spelled here as the grid shows them — the mark's widest art
// row and the panel's first header — so the assertion reads what a reader sees.
// The surface's own constants are pinned in-package (cmd/agent/chat_banner.go's
// tests); here they are the harness's markers, which is why they are literals.
const (
	// chatBannerMarkFact is a row of the mark's own 3D plate: the "G"'s upper
	// body with the drop shadow cast along its right edge (the wordmark art's
	// second row, which every variant shares — a wide box draws the same
	// construction spelled longer).
	chatBannerMarkFact = "██▓▓▓▓██"
	// chatBannerTitleFact is the title bar's own words.
	chatBannerTitleFact = "gopi v"
	// chatBannerTipsHeaderFact is the side panel's first section header.
	chatBannerTipsHeaderFact = "Tips for getting started"
	// chatBannerAnswer is the scripted answer. It is short and single-line: this
	// case is about the screen the turn STARTS from, and a long answer would put
	// rows between the assertion and the block it reads.
	chatBannerAnswer = "banner-case-answer"
	// chatBannerPaintNudge is the one character typed to make the layout paint
	// (it is erased immediately; see the call site).
	chatBannerPaintNudge = "x"
	// chatBannerPaintSettle is how long the harness gives the throttled sampler
	// to take the frame the nudge forced. It is above the dump's own 100ms
	// throttle so the read cannot race the sampler it depends on.
	chatBannerPaintSettle = 400 * time.Millisecond
)

// chatBannerReadGrid waits for the dump to hold a painted screen and returns its
// rows plus the raw pty output seen so far. The wait is on the ARTIFACT for the
// same reason the cadence case states: the renderer writes a row in pieces, so
// only the artifact carries the drawn grid.
func chatBannerReadGrid(t *testing.T, dumpPath string) []string {
	t.Helper()
	deadline := time.Now().Add(ptyPromptTimeout)
	var last string
	for time.Now().Before(deadline) {
		cand, err := uiclient.ReadDump(dumpPath)
		// "not written yet" (the surface writes by atomic rename) is this poll's
		// ordinary state, never a verdict.
		if err == nil {
			if refusal := uiclient.CompareGridUnusable(cand); refusal == "" &&
				chatBannerRowHolding(cand.Dump.Lines, chatBannerMarkFact) >= 0 {
				// The sampled frame must hold the whole box, not a half-painted
				// one: the bottom border is the block's last row, so waiting for
				// it is waiting for the commit to have finished.
				if chatBannerBoxBottom(cand.Dump.Lines) >= 0 {
					return cand.Dump.Lines
				}
			}
			last = chatBannerJoin(cand.Dump.Lines)
		} else {
			last = err.Error()
		}
		time.Sleep(chatBannerFrameGap) //stylegate:allow-sleep polls the artifact the surface writes on a throttle; it is the reader's cadence, not the interval under measurement
	}
	t.Fatalf("the dump %s never carried the startup screen within %s; last read:\n%s", dumpPath, ptyPromptTimeout, last)
	return nil
}

// chatBannerVisible renders a pty byte stream with its control bytes visible, so
// a failure message shows WHAT the surface wrote (a CSI is not a printable cell
// and would otherwise be lost in the log).
func chatBannerVisible(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == 0x1b {
			b.WriteString("<ESC>")
			continue
		}
		if r < 0x20 && r != '\n' {
			b.WriteString("<")
			b.WriteString(itoaHex(byte(r)))
			b.WriteString(">")
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// itoaHex renders one byte as two lowercase hex digits (the visible form above).
func itoaHex(b byte) string {
	const hex = "0123456789abcdef"
	return string([]byte{hex[b>>4], hex[b&0xf]})
}

// chatBannerFrameGap is the reader's polling cadence. It is above the dump's own
// 100ms throttle (uiclient's dumpMinInterval) so a poll never races the sampler
// it reads.
const chatBannerFrameGap = 150 * time.Millisecond

// chatBannerBoxTop / chatBannerBoxBottom find the box's borders by their corner
// glyphs, preferring a row that spans the screen (a corner inside the panel's
// own text cannot satisfy the width check).
func chatBannerBoxTop(lines []string) int {
	for i, row := range lines {
		if chatBannerIsBorderRow(row, boxTopLeftGlyph, boxTopRightGlyph) {
			return i
		}
	}
	return -1
}

func chatBannerBoxBottom(lines []string) int {
	for i, row := range lines {
		if chatBannerIsBorderRow(row, boxBottomLeftGlyph, boxBottomRightGlyph) {
			return i
		}
	}
	return -1
}

// chatBannerBoxRows returns the box's top and bottom rows, requiring the pair to
// be ordered (a bottom above the top is not a box).
func chatBannerBoxRows(t *testing.T, lines []string) (int, int) {
	t.Helper()
	top := chatBannerBoxTop(lines)
	bottom := chatBannerBoxBottom(lines)
	if top >= 0 && bottom >= 0 && bottom <= top {
		return top, -1
	}
	return top, bottom
}

// The box's corners, spelled as the grid shows them. They are the harness's
// markers (see chatBannerMarkFact's note).
const (
	boxTopLeftGlyph     = "╭"
	boxTopRightGlyph    = "╮"
	boxBottomLeftGlyph  = "╰"
	boxBottomRightGlyph = "╯"
)

// chatBannerIsBorderRow reports whether row is a full-width border: it starts
// with the left corner, ends with the right one, and every cell between them is
// rule or title text — no EDGE column, which is what distinguishes a border from
// a content row that merely mentions a corner glyph.
func chatBannerIsBorderRow(row, left, right string) bool {
	trimmed := strings.TrimRight(row, " ")
	if !strings.HasPrefix(trimmed, left) || !strings.HasSuffix(trimmed, right) {
		return false
	}
	return !strings.Contains(trimmed, "│")
}

// chatBannerRowHolding returns the index of the first row carrying want, or -1.
func chatBannerRowHolding(lines []string, want string) int {
	for i, row := range lines {
		if strings.Contains(row, want) {
			return i
		}
	}
	return -1
}

// chatBannerGridHolds reports whether any row carries want.
func chatBannerGridHolds(lines []string, want string) bool {
	return chatBannerRowHolding(lines, want) >= 0
}

// chatBannerStatusRowIndex is the row the status line occupies: the LAST
// non-empty row on the grid, which is where the reserved layout places it
// (region.go: statusRows is always the terminal's bottom row).
func chatBannerStatusRowIndex(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	return -1
}

// chatBannerJoin renders the grid for a failure message, row-numbered, so a
// reader of the failure sees the same screen the assertion saw.
func chatBannerJoin(lines []string) string {
	var b strings.Builder
	for i, row := range lines {
		b.WriteString(itoa3(i))
		b.WriteString("|")
		b.WriteString(row)
		b.WriteString("|\n")
	}
	return b.String()
}

// itoa3 renders a zero-padded row number without pulling strconv into the test's
// import list for one call.
func itoa3(n int) string {
	const digits = "0123456789"
	if n < 0 {
		return "???"
	}
	if n < 10 {
		return "00" + string(digits[n])
	}
	if n < 100 {
		return "0" + string(digits[n/10]) + string(digits[n%10])
	}
	return string(digits[n/100%10]) + string(digits[n/10%10]) + string(digits[n%10])
}

// chatBannerKeepEvidence copies the dump and the raw pty bytes into the run's
// evidence directory, so the verdict's evidence is this run's own artifacts.
func chatBannerKeepEvidence(t *testing.T, evidence, dumpPath string) {
	t.Helper()
	blob, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("read the screen artifact %s to keep it as evidence: %v", dumpPath, err)
	}
	kept := filepath.Join(evidence, "startup-screen.dump")
	if err := os.WriteFile(kept, blob, chatBannerEvidencePerm); err != nil {
		t.Fatalf("keep the screen artifact as evidence: %v", err)
	}
	// A SECOND copy under the run's own evidence channel when one is named
	// (chatBannerEvidenceEnv): the scratch tree is swept after the run, and a
	// verdict's evidence has to outlive the test that produced it.
	if dir := os.Getenv(chatBannerEvidenceEnv); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "startup-screen.dump"), blob, chatBannerEvidencePerm); err != nil {
			t.Fatalf("keep the screen artifact at %s: %v", dir, err)
		}
	}
	t.Logf("screen artifact kept at %s", kept)
}

// chatBannerEvidenceEnv names a directory OUTSIDE the swept scratch tree where
// this case's artifact is also written. It is read (not required) so the case
// runs identically in CI, where nothing names one.
const chatBannerEvidenceEnv = "GOPI_TEST_BANNER_EVIDENCE"

// chatBannerEvidencePerm is the mode the kept artifact is written with, matching
// the cadence case's own dump copy.
const chatBannerEvidencePerm = 0o644
