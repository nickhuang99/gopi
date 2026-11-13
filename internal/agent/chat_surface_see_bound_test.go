package agent

// chat_surface_see_bound_test.go — the BACKSTOP on `action=see`, pinned on the
// real dispatcher.
//
// WHY THIS EXISTS SEPARATELY from the lineedit suite's bounded-rendering tests:
// those prove the RENDERING is bounded, and they would all still pass if the
// tool return were assembled some other way. This file proves the TOOL cannot
// put an unbounded answer into a model's context whatever the surface hands it —
// the surface is not this package's object (a foreign run's artifact, a section
// added to the rendering later, a pathological grid), so the last line of
// defence has to live here and has to be tested here.
//
// The incident (2026-11-06): one `action=see` tool_result carried 2,783,126
// characters, 2,778,520 of them scrollback. Nothing between the dumping sink and
// the request body could say "that is too much" — this is the place that can.

import (
	"fmt"
	"strings"
	"testing"
)

// bigGrid builds a grid rendering of n characters per row, as a surface would
// hand one over: the shape is irrelevant to the backstop, only the size is.
func bigGrid(rows, cols int) string {
	var b strings.Builder
	for r := 0; r < rows; r++ {
		fmt.Fprintf(&b, "%03d|%s\n", r, strings.Repeat("x", cols))
	}
	return b.String()
}

// TestChatSurfaceSeeBoundsItsReturn is the red-then-green core: a surface that
// hands over a rendering far larger than the budget must NOT be returned whole,
// the answer must carry a notice naming the cut and the total, and the cut must
// land on a line boundary so no row is presented half-complete.
func TestChatSurfaceSeeBoundsItsReturn(t *testing.T) {
	// Comfortably above the budget, in whole rows, so the cut has boundaries.
	const cols = 200
	rows := ChatSurfaceSeeCharsMax/cols + 100
	grid := bigGrid(rows, cols)
	if len(grid) <= ChatSurfaceSeeCharsMax {
		t.Fatalf("the fixture is not above the budget: %d <= %d", len(grid), ChatSurfaceSeeCharsMax)
	}

	a := &Agent{}
	a.SetChatSurface(stubChatSurface{obs: ChatScreenObservation{
		Armed: true, Found: true, Path: "/x/chat-screen.dump", Grid: grid,
	}})
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionSee})

	if len(got) > ChatSurfaceSeeCharsMax+len(chatSurfaceSeeTruncatedNotice(len(grid), len(grid), "/x/chat-screen.dump")) {
		t.Fatalf("see returned %d characters for a %d-character rendering — the backstop did not fire", len(got), len(grid))
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("the answer was cut without saying so — a bare cut is indistinguishable from a screen that ends there")
	}
	if !strings.Contains(got, "/x/chat-screen.dump") {
		t.Errorf("the truncation notice does not name the artifact to read the rest from:\n%s", got[len(got)-400:])
	}
	// The cut is at a LINE boundary: every line the answer carries is either a
	// whole rendering row or the notice. A half row would be a screen row the
	// model reads as complete.
	body := got[:strings.Index(got, "\n[chat_surface ")]
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.Contains(line, ChatSurfaceActionSee) {
			continue // header / facts lines
		}
		if !strings.HasSuffix(line, strings.Repeat("x", len(line)-len(strings.TrimLeft(line, "0123456789|")))) {
			t.Fatalf("a returned line is not a whole rendering row: %q", line)
		}
	}
}

// TestChatSurfaceSeeLeavesAnOrdinaryAnswerUntouched is the other half of every
// bound: a normal screen must arrive byte-identical, or the fix silently taxes
// the case it was never meant to touch.
func TestChatSurfaceSeeLeavesAnOrdinaryAnswerUntouched(t *testing.T) {
	grid := bigGrid(24, 80)
	a := &Agent{}
	a.SetChatSurface(stubChatSurface{obs: ChatScreenObservation{
		Armed: true, Found: true, Path: "/x/chat-screen.dump", Grid: grid,
	}})
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionSee})
	if strings.Contains(got, "truncated") {
		t.Fatalf("a %d-character screen was reported as truncated — the bound fired on an ordinary answer", len(grid))
	}
	if !strings.HasSuffix(got, grid) {
		t.Fatalf("the ordinary answer does not end with the grid verbatim:\n%s", got)
	}
}

// TestChatSurfaceSeeBoundCutsOnRuneBoundaryWhenNoLineExists pins the fallback:
// one pathologically long line has no line boundary to cut on, so the cut must
// fall back to a RUNE boundary — a byte offset inside a multi-byte character
// would put a broken rune into the model's prompt, which is worse than a shorter
// answer.
func TestChatSurfaceSeeBoundCutsOnRuneBoundaryWhenNoLineExists(t *testing.T) {
	// CJK: 3 bytes per rune, so a byte cut at the budget lands mid-rune unless
	// the fallback walks back.
	one := strings.Repeat("知", ChatSurfaceSeeCharsMax/3+1000)
	got := chatSurfaceSeeBound(one, "/x/dump")
	if !strings.Contains(got, "truncated") {
		t.Fatalf("a %d-byte single line was not bounded", len(one))
	}
	body := got[:strings.Index(got, "\n[chat_surface ")]
	if strings.ContainsRune(body, '\uFFFD') {
		t.Fatalf("the cut produced a replacement character — it landed inside a rune")
	}
	for _, r := range body {
		if r != '知' {
			t.Fatalf("the body carries %q, which is not part of the original text: the cut split a rune", r)
		}
	}
}

// TestChatSurfaceSeeBoundIsIdenticalBelowTheBudget keeps the backstop from being
// a second renderer: at or under the budget it must return its input untouched,
// byte for byte.
func TestChatSurfaceSeeBoundIsIdenticalBelowTheBudget(t *testing.T) {
	for _, s := range []string{"", "a", strings.Repeat("y", ChatSurfaceSeeCharsMax)} {
		if got := chatSurfaceSeeBound(s, "/x/dump"); got != s {
			t.Fatalf("chatSurfaceSeeBound changed a %d-character body that already fits", len(s))
		}
	}
}
