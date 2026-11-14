// screen_dcs_string_test.go — the DCS arm's regression guard.
//
// THE DEFECT (measured 2026-10-03, task 01a0ff97). `gopi chat`'s armed screen
// dump never carried the startup screen (the logo box, "Tips for getting
// started", the identity block): the grid held rows 0..22 blank while the
// operator's terminal had shown the screen in full, and the ONLY difference
// between the two runs was `--no-color` — which skips the capability probe.
//
// The probe sends `ESC P + q <hex> ESC \` (XTGETTCAP: `colors`, `RGB`) before
// the layout paints. The interpreter had a string-sequence arm for OSC
// (`ESC ]`) and NONE for DCS (`ESC P`), so the `P` was read as a two-byte
// escape (ESC + one byte), the payload painted into the grid as text, and the
// payload's own ST (`ESC \`) was read as the start of the next sequence —
// swallowing the introducer of the layout's `ESC[1;24r`. The scrolling region
// was therefore never installed, everything the surface committed into the
// answer band flowed downward instead of scrolling, and `scrollRows`' `top==0`
// scrollback arm (internal/lineedit/screen.go) was never reached — so the
// replaced rows landed in neither the grid nor the scrollback.
//
// These tests are written against the MINIMAL byte shapes, so they fail for the
// parser's reason and not for a reason a longer recording happened to carry.
package lineedit

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sgr"
	"github.com/nickhuang99/gopi/internal/version"
)

// dcsQuery is the exact shape the capability probe emits (sgr.GetCapabilityQuery):
// `ESC P + q <payload in hex pairs> ESC \`. Built from the shipped constants so
// the test cannot drift away from what the probe actually sends.
func dcsQuery(name string) string { return sgr.GetCapabilityQuery(name) }

// The failure the defect produced, in its two observable halves: the sequence's
// payload painted as cells, and the sequence after it lost its introducer.
func TestDCSQueryPaintsNoCellAndDoesNotEatTheNextSequence(t *testing.T) {
	// `ESC[1;24r` is the region the chat layout installs; it is the sequence the
	// swallowed `ESC` used to be taken from.
	const layoutRegion = "\x1b[1;24r"
	feed := dcsQuery("colors") + dcsQuery("RGB") + layoutRegion

	s := NewScreen(testCols, testRows)
	s.Feed([]byte(feed))

	for row, line := range s.Dump().Lines {
		if strings.Contains(line, "q636F6C6F7273") || strings.Contains(line, "q524742") {
			t.Fatalf("a DCS payload painted into the grid — row %03d reads %q\n(this is the text the XTGETTCAP query's own hex was rendered as)", row, line)
		}
		if strings.Contains(line, "[1;24r") {
			t.Fatalf("the sequence AFTER the DCS lost its ESC introducer and painted as text — row %03d reads %q", row, line)
		}
	}

	// The region is the whole point: with the introducer intact, the layout's
	// own DECSTBM installs rows 1..24 (0-based 0..23), which is what makes
	// scrollRows save the leaving top row into the scrollback.
	top, bottom := s.ScrollRegion()
	if top != 0 || bottom != testRegionBottom {
		t.Fatalf("the scrolling region is %d..%d, want 0..%d — the DCS arm did not leave the following DECSTBM intact", top, bottom, testRegionBottom)
	}
}

// The operator-visible consequence: the startup screen must survive as HISTORY
// rather than being overwritten row by row. This is the assertion the task's
// acceptance states — a dump whose scrollback carries the logo and the tips.
//
// The band is refilled after the startup screen, exactly as a real run refills
// it: a row written at the region's bottom and promoted by ONE LF. The rows the
// startup screen occupied leave the TOP of the region on the way, and with
// `top == 0` (the arm this fix restores the reach to) they are saved as history.
func TestDCSQueryKeepsTheCommittedStartupScreenInScrollback(t *testing.T) {
	// The version token in the banner is read from its ONE home, so a version
	// bump never leaves this fixture asserting yesterday's string (Rule 13: a
	// literal copied from another package's constant is a second source of truth).
	banner := "gopi " + version.Version
	const tips = "Tips for getting started"

	s := NewScreen(testCols, testRows)
	// The probe, then the layout's own region install — the DECSTBM whose ESC
	// the DCS's ST used to swallow, and whose absence is what destroyed the
	// startup screen instead of filing it.
	s.Feed([]byte(dcsQuery("colors") + dcsQuery("RGB") + "\x1b[1;24r"))

	// The startup screen, committed the way region.writeBandLine commits: address
	// the live row, write, promote with ONE LF inside the region.
	promote := func(row string) {
		s.Feed([]byte("\x1b[24;1H" + row + "\x1b[24;1H\n"))
	}
	promote(banner)
	promote(tips)
	// …then the turns that follow, which is what actually pushes both rows off
	// the top: the region holds rows 1..24, so a row needs that many promotions
	// after it before it becomes history.
	for i := 0; i < testRows; i++ {
		promote("answer line")
	}

	history := strings.Join(s.Scrollback(), "\n")
	if !strings.Contains(history, banner) {
		t.Fatalf("the startup banner did not reach the scrollback — history is:\n%s", history)
	}
	if !strings.Contains(history, tips) {
		t.Fatalf("the tips row did not reach the scrollback — history is:\n%s", history)
	}
}

// The OSC arm keeps its behaviour through the shared reader: the same framing,
// the same two terminators, and the same abort rule.
func TestOSCAndDCSShareOneStringReader(t *testing.T) {
	cases := []struct {
		name string
		feed string
		want string // what the grid's first row must read afterwards
	}{
		{"OSC terminated by ST", "\x1b]11;?\x1b\\\x1b[1;1Hok", "ok"},
		{"OSC terminated by BEL", "\x1b]11;?\x07\x1b[1;1Hok", "ok"},
		{"DCS terminated by ST", dcsQuery("colors") + "\x1b[1;1Hok", "ok"},
		{"DCS terminated by BEL", "\x1bP+q636F6C6F7273\x07\x1b[1;1Hok", "ok"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewScreen(testCols, testRows)
			s.Feed([]byte(c.feed))
			if got := strings.TrimRight(s.Dump().Lines[0], " "); got != c.want {
				t.Fatalf("grid row 0 reads %q, want %q", got, c.want)
			}
		})
	}
}

// A string sequence split across two reads is resumed, not re-parsed: the
// half-arrived payload must not be painted by the first read.
func TestSplitStringSequenceIsResumedNotPainted(t *testing.T) {
	full := dcsQuery("colors") + "\x1b[1;24r"
	cut := len(full) / 2

	s := NewScreen(testCols, testRows)
	s.Feed([]byte(full[:cut]))
	for row, line := range s.Dump().Lines {
		if strings.Contains(line, "q") {
			t.Fatalf("the half-arrived DCS painted cells — row %03d reads %q", row, line)
		}
	}
	s.Feed([]byte(full[cut:]))
	if top, bottom := s.ScrollRegion(); top != 0 || bottom != testRegionBottom {
		t.Fatalf("after the second half the region is %d..%d, want 0..%d", top, bottom, testRegionBottom)
	}
}

// The geometry these tests draw into. Named so the assertions cannot drift into
// a size nobody chose (Rule 1): 24 rows is the layout's own region bottom for a
// 26-row terminal — rows 1..24, the same the reserved chat layout installs.
const (
	testCols         = 117
	testRows         = 26
	testRegionBottom = 23
)
