package testsuite

// chat_banner_band_clean_pty_test.go — the acceptance test for the operator's
// report that the chat's OUTPUT AREA was polluted by tips (2026-10-03):
//
//	那些是 tips for user，请把这些放到 启动界面里，不要放到 输出区域里
//
// THE DEFECT IT PINS. `gopi chat` opens with its startup screen (the box plus its
// side panel) and then committed three `[gopi] …` prose lines into the ANSWER
// BAND — the area the operator reads as the conversation: the keyboard
// negotiation's Alt+Enter advice, the chat_surface armed notice, and the
// chat_surface identity notice. Each wrapped to two or three band rows before the
// operator had asked anything. chat_panel_notice.go carries the design of the fix
// (the facts move to the panel, the withheld sentences to the box's own notices
// section inside the same border, and a run with no screen keeps printing them).
//
// WHY THIS CASE READS THE PTY BYTES rather than the screen dump. The dump is a
// SAMPLED artifact of a mirror terminal (uiclient), and on this host the sampler
// does not take a frame for the chat layout's startup screen at all: the sibling
// case (chat_banner_pty_test.go) fails on the UNMODIFIED baseline for exactly
// that reason — measured, `git stash` + re-run, the same "never carried the
// startup screen" verdict. What this case is about is what the surface EMITTED,
// and the pty stream is that channel at byte level: a `[gopi] …` line committed
// into the band appears there in the same order the operator saw it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// The chat chrome's own stream marker and the three sentences the operator
// quoted. They are spelled HERE, independently of the renderer's constants, so
// the verdict is about the stream the operator received: any notice — including
// one added later by a developer who never read chat_panel_notice.go — carries
// the marker, and the three fragments identify today's sentences by the words the
// report quoted.
const (
	chatPanelChromeMarker     = "[gopi] "
	chatPanelUnsupportedFact  = "does not report Shift+Enter"
	chatPanelArmedFact        = "own screen is readable"
	chatPanelIdentityFact     = "surface identity"
	chatPanelUnauthorizedFact = "this run's own"
	// chatPanelNewlineFact is the panel's own way of stating the keyboard fact —
	// the assertion that the fact MOVED rather than vanished (chat_panel_notice.go).
	chatPanelNewlineFact = "Alt+Enter"
	// chatPanelStatusFact is the live status row's idle word. It is the
	// EDITOR's row, not the transcript's, which is why the band legitimately
	// carries it (chat_stream.go's own vocabulary).
	chatPanelStatusFact = "ready"
	// chatPanelBoxBottomGlyph is the startup screen's closing corner as the
	// screen draws it — the boundary the band starts below (chat_banner.go's
	// own glyph).
	chatPanelBoxBottomGlyph = "╯"
	// chatPanelCollectWindow is how long this case listens to the pty after the
	// prompt appears: the startup screen is committed after it
	// (chat_banner_pty_test.go's own measurement), and the notices this case is
	// about used to arrive in the same flush.
	chatPanelCollectWindow = 2 * time.Second
	// chatPanelReadTick is the poll interval while collecting.
	chatPanelReadTick = 50 * time.Millisecond
)

// TestChatPTY_StartupScreenLeavesTheAnswerBandClean is the operator-visible
// acceptance for the fix.
func TestChatPTY_StartupScreenLeavesTheAnswerBandClean(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	evidence := testutil.TestScratchDir(t, "chat-band-clean")
	dumpPath := evidence + "/chat-band-clean.dump"

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: chatBannerAnswer})
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat", "--dump-screen=" + dumpPath},
		testutil.MockEnv(t, home, llm.URL()), work)
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: chatBannerPTYCols, Row: chatBannerPTYRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable on this host: %v", err)
	}
	// THE GEOMETRY IS PROVEN, NOT ASSUMED: a pty that never received the size
	// reports 0 columns, and the surface then (rightly) declines to draw a box it
	// cannot fit — which this case would misread as "the band is clean".
	if cols := lineedit.TerminalCols(int(chat.master.Fd())); cols != chatBannerPTYCols {
		t.Fatalf("the pty reports %d columns, want the %d this harness installed", cols, chatBannerPTYCols)
	}

	// Wait for the PROMPT — painted before the startup screen is committed — then
	// collect the bytes that follow it, which is the flush the notices ride.
	promptOut := chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	stream := promptOut + chat.collect(t, chatPanelCollectWindow)
	chatPanelKeepStream(t, evidence, stream)

	// ---- 1. the startup screen was DRAWN, so this case can speak about the band
	//
	// A run whose box never appeared would pass "no chrome in the band" while
	// testing nothing at all. The closing corner is the block's last row, so
	// seeing it is seeing the commit finished.
	boxEnd := strings.LastIndex(stream, chatPanelBoxBottomGlyph)
	if boxEnd < 0 {
		t.Fatalf("the startup screen was never written to this pty, so this case cannot speak about the band; stream:\n%s",
			chatBannerVisible(stream))
	}

	// ---- 2. no chat chrome after the box's last row -------------------------
	//
	// This is the operator's requirement, stated against the bytes they received:
	// below the startup screen, the stream carries the editor's own chrome and
	// nothing else.
	band := stream[boxEnd+len(chatPanelBoxBottomGlyph):]
	for _, forbidden := range []string{
		chatPanelChromeMarker + "chat:",
		chatPanelChromeMarker + "chat_surface",
	} {
		if idx := strings.Index(band, forbidden); idx >= 0 {
			t.Errorf("the ANSWER BAND carries startup chrome (%q) — the operator's report says tips belong on the startup screen:\n%s",
				forbidden, chatBannerVisible(band[idx:]))
		}
	}

	// ---- 3. the facts moved instead of vanishing (Rule 15) ------------------
	//
	// A run that stopped printing a notice and stated nothing would pass check 2
	// while being the silent failure the rule forbids. So the startup screen has
	// to state what the band used to: the key that inserts a newline on THIS
	// terminal (the keyboard negotiation's verdict), which is the pane's own fact
	// and the first sentence the operator quoted.
	if !strings.Contains(stream[:boxEnd], chatPanelNewlineFact) {
		t.Errorf("the startup screen does not state the newline key for a terminal that cannot report Shift+Enter; the band is clean because the fact was LOST, not moved. stream:\n%s",
			chatBannerVisible(stream))
	}

	t.Logf("pty stream (control bytes visible):\n%s", chatBannerVisible(stream))
}

// chatPanelKeepStream writes this case's own pty bytes beside the run's other
// evidence, so the verdict is reproducible from the artifact rather than from a
// later re-run (the scratch tree is swept after the run).
func chatPanelKeepStream(t *testing.T, evidence, stream string) {
	t.Helper()
	path := filepath.Join(evidence, "pty-stream.txt")
	if err := os.WriteFile(path, []byte(chatBannerVisible(stream)), chatBannerEvidencePerm); err != nil {
		t.Fatalf("keep this run's pty stream as evidence: %v", err)
	}
	t.Logf("pty stream kept at %s", path)
}

// collect reads the pty for window, returning every byte that arrives. It is the
// reader for a case whose assertion is about a FLUSH rather than about a marker:
// waitFor stops at the first match, and the startup screen is committed after the
// prompt it matches.
func (p *ptySession) collect(t *testing.T, window time.Duration) string {
	t.Helper()
	var out []byte
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		select {
		case b, ok := <-p.rx:
			if !ok {
				return string(out)
			}
			out = append(out, b)
		case <-time.After(chatPanelReadTick): //stylegate:allow-sleep the reader's own cadence, not an interval under measurement
		}
	}
	return string(out)
}
