package shot

// filmvanished_test.go — "the window is not there RIGHT NOW" is a TRANSIENT.
//
// THE DEFECT THIS PINS (measured 2026-10-04, task 01a103f4). Since the film's
// default terminal became xfce4-terminal (01a10370) the filmed window can leave
// the server and come back while a film is running. The server then answers
// GetGeometry on the stale id with an X11 error — "code 9" (BadDrawable) in the
// field — and the grab used to return it as a FATAL error, so ONE such instant
// ended the whole film: no more frames, no GIF, no retry, and the only sentence
// about it was the error text. The same file already treats "the surface has not
// settled yet" as a transient it carries on through, which is what makes the old
// behaviour an inconsistency rather than a policy.
//
// WHAT IS DRIVEN HERE. The criterion is the RECOVERY, at the grab's own seam: the
// capture function answers with the server's typed refusal for the stale id, the
// window LIST changes to the fresh id the terminal mapped (the injection is at
// ListWindowsProvider, the same seam the CLI drives), and the grab must (a) carry
// on, (b) shoot the NEW id, (c) say so where the caller can see it, and (d) count
// the recovery. The negative arm is the bound: a window that never comes back is
// an ERROR that delivered NOTHING — never a frame marked settled.
//
// No display, no X socket, no terminal: the criterion is the deliverable here and
// it must be provable without a screen (PROJECT.md Rule 31).

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// filmGrabTestTerminal builds the minimum a settled grab needs from a terminal:
// the window it resolved, the pid whose windows are its own, and the rule it was
// selected by. It is a literal of the STRUCT rather than a started terminal — the
// grab's criterion is about the window, and starting a real emulator to prove it
// would test the emulator.
func filmGrabTestTerminal(window uint32) *FilmTerminal {
	return &FilmTerminal{
		Display: ":99",
		PID:     1717,
		Window:  window,
		spec:    FilmTerminalSpec{Name: FilmTerminalNameXfce4, WindowRule: FilmWindowRulePIDTitleRect},
	}
}

// filmGrabServeWindowList makes ListWindows answer with exactly the windows given,
// so a test can take one window away and put another in its place — the same seam
// the CLI reads through, not a mock of the selection rule.
func filmGrabServeWindowList(t *testing.T, windows func() []XWindowInfo) {
	t.Helper()
	restore := ListWindowsProvider
	ListWindowsProvider = func(display string) (XWindowList, error) {
		return XWindowList{Windows: windows()}, nil
	}
	t.Cleanup(func() { ListWindowsProvider = restore })
}

// TestSettledGrabRecoversWhenTheWindowVanishes is the red-then-green proof of the
// fix: the typed server refusal on the window a frame started shooting, the
// terminal mapping a NEW window under the same pid, and the frame continuing.
//
// It FAILS against the pre-fix behaviour (which returned the X11 error as fatal
// on the first refusal) and passes with the retry, which is the whole claim.
func TestSettledGrabRecoversWhenTheWindowVanishes(t *testing.T) {
	restoreCapture := filmWindowCapture
	t.Cleanup(func() { filmWindowCapture = restoreCapture })
	restoreLog := filmTerminalLog
	t.Cleanup(func() { filmTerminalLog = restoreLog })

	const staleWindow = uint32(0x200003)
	const freshWindow = uint32(0x300007)

	// The terminal's window list: it holds the STALE window while the frame is
	// being shot, and the FRESH one from the moment the rebuild happened — exactly
	// what the two windows of one xfce4-terminal pid look like on a live display.
	var rebuilt bool
	filmGrabServeWindowList(t, func() []XWindowInfo {
		if rebuilt {
			return []XWindowInfo{{ID: fmt.Sprintf("0x%x", freshWindow), PID: 1717, Name: "gopi film film", Measured: true, W: 1186, H: 521}}
		}
		return []XWindowInfo{{ID: fmt.Sprintf("0x%x", staleWindow), PID: 1717, Name: "gopi film film", Measured: true, W: 1186, H: 521}}
	})

	var notices bytes.Buffer
	filmTerminalLog = &notices

	shot := []uint32{}
	filmWindowCapture = func(display string, window uint32) ([]byte, x11Geometry, error) {
		shot = append(shot, window)
		switch len(shot) {
		case 1:
			// The stale window is gone at this instant: the server's own refusal,
			// typed, with the code it sends (9 = BadDrawable).
			rebuilt = true
			return nil, x11Geometry{}, NewX11Error("GetGeometry", X11ErrorBadDrawable, 12)
		case 2:
			return []byte("first-paint-from-the-new-window"), x11Geometry{}, nil
		default:
			return []byte("settled"), x11Geometry{}, nil
		}
	}

	term := filmGrabTestTerminal(staleWindow)
	g, err := SettledGrab(":99", term, filmGrabDefaultTries, time.Millisecond)
	if err != nil {
		t.Fatalf("a window that vanished and came back mid-frame ended the grab: %v — this is the defect (one transient must not kill the frame)", err)
	}
	if !g.Settled {
		t.Fatal("the frame was not settled after the window came back")
	}
	if string(g.PNG) != "settled" {
		t.Fatalf("delivered %q, want the frame taken after the re-resolution", g.PNG)
	}
	if g.Vanished != 1 {
		t.Fatalf("vanished = %d, want 1 — the recovery was not counted, so a churning window would look like a clean run", g.Vanished)
	}
	if term.Window != freshWindow {
		t.Fatalf("the terminal still reports window 0x%x, want the RE-RESOLVED 0x%x — the id the driver photographs must come from the selection rule, not from a local copy", term.Window, freshWindow)
	}
	if shot[0] != staleWindow {
		t.Fatalf("the first grab was taken from 0x%x, want the window the frame started on (0x%x)", shot[0], staleWindow)
	}
	for i, w := range shot[1:] {
		if w != freshWindow {
			t.Fatalf("grab %d was still taken from the vanished window 0x%x — the re-resolution was not adopted", i+2, w)
		}
	}
	if !strings.Contains(notices.String(), "re-resolved it to") {
		t.Fatalf("the re-resolution was SILENT — a recovery nobody can see is a recovery nobody can check (Rule 15); notices:\n%s", notices.String())
	}
	if !strings.Contains(notices.String(), fmt.Sprintf("0x%x", freshWindow)) {
		t.Fatalf("the notice does not name the window it re-resolved to:\n%s", notices.String())
	}
}

// TestSettledGrabRefusesAFrameWhenTheWindowNeverComesBack is the other half of the
// same judgement: recovery is BOUNDED. A window that stays away is not a frame the
// run may deliver — the refusal names the window, the attempts and the cause, and
// says in as many words that nothing was delivered.
func TestSettledGrabRefusesAFrameWhenTheWindowNeverComesBack(t *testing.T) {
	restoreCapture := filmWindowCapture
	t.Cleanup(func() { filmWindowCapture = restoreCapture })
	restoreLog := filmTerminalLog
	t.Cleanup(func() { filmTerminalLog = restoreLog })

	const staleWindow = uint32(0x200009)
	var listCalls int
	filmGrabServeWindowList(t, func() []XWindowInfo {
		listCalls++
		// The window is gone and NOTHING replaces it: the terminal's pid publishes
		// no window at all any more.
		return []XWindowInfo{{ID: "0x1", PID: 4242, Name: "somebody else's window", Measured: true, W: 900, H: 400}}
	})

	var notices bytes.Buffer
	filmTerminalLog = &notices

	captureCalls := 0
	filmWindowCapture = func(display string, window uint32) ([]byte, x11Geometry, error) {
		captureCalls++
		return nil, x11Geometry{}, NewX11Error("GetGeometry", X11ErrorBadDrawable, 7)
	}

	term := filmGrabTestTerminal(staleWindow)
	g, err := SettledGrab(":99", term, filmGrabDefaultTries, time.Millisecond)
	if err == nil {
		t.Fatalf("a window that never came back produced a frame (%d bytes, settled=%v) instead of an error — a lost window must never be delivered as a photographed one", len(g.PNG), g.Settled)
	}
	if g.PNG != nil {
		t.Fatalf("the refused grab still carried %d byte(s) of pixels", len(g.PNG))
	}
	var vanished *WindowVanishedError
	if !errors.As(err, &vanished) {
		t.Fatalf("the refusal is not a *WindowVanishedError, so a caller cannot tell it from any other failure: %v", err)
	}
	if vanished.Window != staleWindow {
		t.Fatalf("the refusal names window 0x%x, want the last one resolved (0x%x)", vanished.Window, staleWindow)
	}
	if vanished.Steps != filmGrabVanishedRetries {
		t.Fatalf("the refusal reports %d step(s), want the bound %d", vanished.Steps, filmGrabVanishedRetries)
	}
	if !errors.Is(err, vanished.Cause) && !strings.Contains(err.Error(), "BadDrawable") {
		t.Fatalf("the refusal lost the server's own cause: %v", err)
	}
	if !strings.Contains(err.Error(), "no frame was delivered") {
		t.Fatalf("the refusal does not say that nothing was delivered: %v", err)
	}
	if captureCalls < 1 || captureCalls > 1+filmGrabVanishedRetries {
		t.Fatalf("the window was photographed %d time(s), want between 1 and %d — the retries are a BOUND, not an unbounded loop", captureCalls, 1+filmGrabVanishedRetries)
	}
	if listCalls < filmGrabVanishedRetries {
		t.Fatalf("the window list was read only %d time(s), want at least one look-up per re-resolution step (%d) — the re-resolution must go through the selection rule, not around it", listCalls, filmGrabVanishedRetries)
	}
	if term.Window != staleWindow {
		t.Fatalf("a failed re-resolution changed the terminal's window to 0x%x — only a SUCCESSFUL re-resolution may adopt an id", term.Window)
	}
}

// TestSettledGrabDoesNotRetryOtherFailures keeps the recovery NARROW: only "the
// window is not there" is transient. A display that cannot be reached, a capture
// that failed for any other reason, is still the fatal, immediate error it was —
// a retry loop around every failure would spend the bound on causes that cannot
// improve and would hide them behind it (Rule 15).
func TestSettledGrabDoesNotRetryOtherFailures(t *testing.T) {
	restoreCapture := filmWindowCapture
	t.Cleanup(func() { filmWindowCapture = restoreCapture })

	var listCalls int
	filmGrabServeWindowList(t, func() []XWindowInfo {
		listCalls++
		return nil
	})

	calls := 0
	filmWindowCapture = func(display string, window uint32) ([]byte, x11Geometry, error) {
		calls++
		return nil, x11Geometry{}, NewX11Error("GetImage", X11ErrorBadWindow+100, 3)
	}
	term := filmGrabTestTerminal(0x200010)
	if _, err := SettledGrab(":99", term, filmGrabDefaultTries, time.Millisecond); err == nil {
		t.Fatal("an unclassified X11 error was swallowed")
	}
	if calls != 1 {
		t.Fatalf("an unclassified error was retried %d time(s) — only a vanished window is transient", calls)
	}
	if listCalls != 0 {
		t.Fatalf("an unclassified error re-resolved the window %d time(s)", listCalls)
	}
}

// TestX11ErrorCarriesItsCodeAndIsClassified pins the TYPE itself: the code is the
// server's own word for the refusal, so it must survive the trip, and the
// vanished-window judgement must be ONE function over it (Rule 13/14) rather than
// a string match at each call site.
func TestX11ErrorCarriesItsCodeAndIsClassified(t *testing.T) {
	frame := NewX11Error("GetGeometry", X11ErrorBadDrawable, 41)
	if frame.Code != X11ErrorBadDrawable || frame.Sequence != 41 || frame.Request != "GetGeometry" {
		t.Fatalf("the error lost a field: %+v", frame)
	}
	if !strings.Contains(frame.Error(), "BadDrawable") || !strings.Contains(frame.Error(), "code 9") {
		t.Fatalf("the error text drops either the name or the code: %q", frame.Error())
	}
	// A WRAPPED error still classifies: the grab wraps what capture returned.
	wrapped := fmt.Errorf("grab window 0x1 on :99: %w", frame)
	if !IsX11Error(wrapped) {
		t.Fatal("a wrapped X11 error no longer reads as an X11 error")
	}
	if code, ok := X11ErrorCode(wrapped); !ok || code != X11ErrorBadDrawable {
		t.Fatalf("X11ErrorCode(wrapped) = (%d, %v), want (9, true)", code, ok)
	}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"BadDrawable is a vanished window", NewX11Error("GetGeometry", X11ErrorBadDrawable, 1), true},
		{"BadWindow is a vanished window", NewX11Error("GetGeometry", X11ErrorBadWindow, 1), true},
		{"an unclassified code is not", NewX11Error("GetImage", 99, 1), false},
		{"a non-X11 error is not", errors.New("connection refused"), false},
		{"a wrapped non-X11 error is not", fmt.Errorf("grab: %w", errors.New("io timeout")), false},
		{"no error at all is not", nil, false},
	}
	for _, c := range cases {
		if got := IsWindowVanishedError(c.err); got != c.want {
			t.Errorf("%s: IsWindowVanishedError = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestWindowVanishedReportNamesTheBound keeps the bound and the notice in ONE
// place: a report that re-typed "3" would keep saying three after the constant
// moved, and the number a reader is asked to trust is the one the code used.
func TestWindowVanishedReportNamesTheBound(t *testing.T) {
	if got := WindowVanishedSteps(); got != filmGrabVanishedRetries {
		t.Fatalf("WindowVanishedSteps() = %d, want the constant %d", got, filmGrabVanishedRetries)
	}
	if filmGrabVanishedRetries < 1 {
		t.Fatal("the re-resolution bound must admit at least one attempt, or the recovery is decoration")
	}
	if filmGrabVanishedStep <= 0 {
		t.Fatal("the re-resolution pause must be positive — a retry with no pause re-reads the same dead window")
	}
}
