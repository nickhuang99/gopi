package shot

// displayalloc_test.go — the three questions of a private display, proved
// WITHOUT a live machine: the derivation of the candidate list from the sockets
// actually present, the classification of an attempt read back from its own log,
// and the settled-grab criterion at its own seam.
//
// These are the three units the shell pipeline got wrong for months (measured
// 2026-10-02): a fixed number window, a guess printed as a conclusion, and a
// single grab that photographed the previous beat. Each one is pinned here so a
// future edit that reintroduces it fails a test rather than a film.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDisplayCandidatesComeFromTheSocketDirectory pins the DERIVATION: the
// candidate list is what is NOT occupied under the socket directory this machine
// actually holds — never a fixed window, and the occupancy record is the socket
// file itself.
func TestDisplayCandidatesComeFromTheSocketDirectory(t *testing.T) {
	dir := t.TempDir()
	// This host's own measured occupancy (2026-10-02): :0 :1 :62 :85 :97 :240 —
	// a fixed twelve-number window starting at :62 cannot describe it, which is
	// exactly the defect the derivation removes.
	used := []int{0, 1, 62, 85, 97, 240}
	for _, n := range used {
		if err := os.WriteFile(DisplaySocketPath(dir, n), nil, 0o600); err != nil {
			t.Fatalf("fixture socket for :%d: %v", n, err)
		}
	}
	// A non-registration entry must not be read as one: the sweep is over names
	// that are X followed by digits.
	if err := os.WriteFile(filepath.Join(dir, "Xbanana"), nil, 0o600); err != nil {
		t.Fatalf("fixture stray entry: %v", err)
	}
	got, err := DisplayUsedNumbers(dir)
	if err != nil {
		t.Fatalf("DisplayUsedNumbers: %v", err)
	}
	want := []int{0, 1, 62, 85, 97, 240}
	if len(got) != len(want) {
		t.Fatalf("used numbers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("used numbers = %v, want %v (sorted ascending)", got, want)
		}
	}
	taken := map[int]bool{}
	for _, n := range got {
		taken[n] = true
	}
	cand := DisplayCandidateNumbers(dir, got)
	if len(cand) == 0 {
		t.Fatal("no candidate numbers at all: the search must reach past a machine's occupancy")
	}
	// THE PREFERRED NUMBER FIRST WHEN IT IS FREE, and every occupied number absent.
	// (The list above includes :62 — this host really holds a server there — so the
	// expected head is computed from it rather than assumed: "preferred" is a
	// PREFERENCE, and an occupied preference must not be offered.)
	wantHead := displayPreferredNumber
	for taken[wantHead] {
		wantHead++
	}
	if cand[0] != wantHead {
		t.Fatalf("first candidate = :%d, want the smallest free number at or above the preferred :%d", cand[0], wantHead)
	}
	if taken[cand[0]] {
		t.Fatalf("first candidate :%d is already registered on this machine", cand[0])
	}
	for _, n := range cand {
		if taken[n] {
			t.Fatalf("candidate :%d is already registered on this machine — the list must skip occupancy", n)
		}
	}
	// The wrap BELOW the preferred number is what makes the search describe any
	// machine shape: :61 and below are reachable, not only the ascending arm.
	if !containsInt(cand, displayPreferredNumber-1) {
		t.Fatalf("candidate list %v does not reach below :%d — the search must wrap", cand[:4], displayPreferredNumber)
	}
	// And an unreadable directory is an ERROR, never "nothing is used": an empty
	// answer would make every candidate look free (Rule 15b).
	if _, err := DisplayUsedNumbers(filepath.Join(dir, "does-not-exist")); err == nil {
		t.Fatal("a missing socket directory was reported as an empty occupancy — an absent answer must not read as a clean machine")
	}
}

// TestDisplayFailureIsClassifiedFromTheServersOwnLog pins the CLASSIFICATION and
// its order: every cause is a pattern in the server's own words, an empty log is
// reported as empty (not as a cause), and "already active" wins over the lock-file
// phrasing it shares with the unwritable arm.
func TestDisplayFailureIsClassifiedFromTheServersOwnLog(t *testing.T) {
	cases := []struct {
		name string
		log  string
		want DisplayCause
	}{
		{"occupied", "Fatal server error:\nServer is already active for display 62\n\tIf this server is no longer running, remove /tmp/.X62-lock\n\tand start again.\n", DisplayCauseOccupied},
		{"not-installed", "bash: line 1: Xvfb: command not found\n", DisplayCauseNotInstalled},
		{"unwritable", "Fatal server error:\nCannot establish any listening sockets - Make sure an X server isn't already running\n", DisplayCauseUnwritable},
		{"permission", "Fatal server error:\nCould not create server lock file: /tmp/.X62-lock: Permission denied\n", DisplayCauseUnwritable},
		{"other", "Xvfb: unrecognized option '-bogus'\n", DisplayCauseOther},
		{"empty", "", DisplayCauseOther},
	}
	for _, c := range cases {
		if got := classifyDisplayLog(c.log); got != c.want {
			t.Errorf("%s: classifyDisplayLog = %q, want %q", c.name, got, c.want)
		}
	}
	// The ORDER is load-bearing: Xvfb's occupied message NAMES the lock file, so a
	// classifier that tested "lock file" first would report a taken number as an
	// unwritable directory — two different fixes.
	occupied := "Server is already active for display 62 — remove /tmp/.X62-lock"
	if got := classifyDisplayLog(occupied); got != DisplayCauseOccupied {
		t.Fatalf("the occupied log (which mentions the lock file) classified as %q — the arms must be ordered occupied first", got)
	}

	// The REPORT is built from the attempts, and the quote is the server's own
	// text: a reader must be able to see WHY each number failed.
	dir := t.TempDir()
	report := &DisplayUnavailableError{Total: 3, Tried: 2, SocketDir: dir, Counts: map[DisplayCause]int{}}
	occupiedLog := filepath.Join(dir, "xvfb-62.log")
	if err := os.WriteFile(occupiedLog, []byte(occupied+"\n"), 0o644); err != nil {
		t.Fatalf("fixture log: %v", err)
	}
	emptyLog := filepath.Join(dir, "xvfb-63.log")
	if err := os.WriteFile(emptyLog, nil, 0o644); err != nil {
		t.Fatalf("fixture log: %v", err)
	}
	a1 := readDisplayAttempt(62, occupiedLog)
	a2 := readDisplayAttempt(63, emptyLog)
	report.Counts[a1.Cause]++
	report.Counts[a2.Cause]++
	report.Attempts = []DisplayAttempt{a1, a2}
	msg := report.Error()
	for _, frag := range []string{":62", ":63", "NUMBER IS TAKEN", "already active", "the log is EMPTY", occupiedLog} {
		if !strings.Contains(msg, frag) {
			t.Errorf("the failure report does not contain %q:\n%s", frag, msg)
		}
	}
	if !a2.Empty {
		t.Error("an empty log was not reported as empty — an empty log and a classified one must not look the same")
	}
}

// TestCheckFilmScratchRefusesAnUnwritableDirectory pins question 1 as a REAL
// write: a directory that cannot hold a marker cannot hold the server's log.
func TestCheckFilmScratchRefusesAnUnwritableDirectory(t *testing.T) {
	root := t.TempDir()
	ok := filepath.Join(root, "writable")
	if err := CheckFilmScratch(ok, "--film"); err != nil {
		t.Fatalf("a fresh directory was refused: %v", err)
	}
	if entries, err := os.ReadDir(ok); err != nil || len(entries) != 0 {
		t.Fatalf("the write probe was left behind (entries=%d, err=%v) — the check must clean up after itself", len(entries), err)
	}
	// A FILE where a directory is wanted: the refusal names it rather than
	// producing twelve failed starts.
	filePath := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := CheckFilmScratch(filePath, "--film"); err == nil {
		t.Fatal("a regular file was accepted as a film directory")
	}
	if err := CheckFilmScratch("", "--film"); err == nil {
		t.Fatal("an empty directory name was accepted — the caller's label must reach the refusal")
	}
}

// TestSettledGrabDeliversOnlyAnAgreedFrame drives the CRITERION at its own seam,
// with no display and no terminal: the frame delivered is the first one that
// agrees with its predecessor, the grabs before it are counted as stale, and a
// window that never settles is delivered AND reported (never silently passed off
// as settled).
func TestSettledGrabDeliversOnlyAnAgreedFrame(t *testing.T) {
	restore := filmWindowCapture
	defer func() { filmWindowCapture = restore }()

	t.Run("second grab agrees", func(t *testing.T) {
		calls := 0
		filmWindowCapture = func(display string, window uint32) ([]byte, x11Geometry, error) {
			calls++
			if calls == 1 {
				return []byte("first-paint"), x11Geometry{}, nil
			}
			return []byte("settled"), x11Geometry{}, nil
		}
		g, err := SettledGrab(":99", filmGrabTestTerminal(7), filmGrabDefaultTries, time.Millisecond)
		if err != nil {
			t.Fatalf("SettledGrab: %v", err)
		}
		if !g.Settled {
			t.Fatal("two identical grabs did not read as settled")
		}
		if g.Stale != 1 {
			t.Fatalf("stale = %d, want 1 — the first grab photographed a screen that then MOVED", g.Stale)
		}
		if g.Tries != 3 {
			t.Fatalf("tries = %d, want 3 (the grab that moved, plus the two that agreed)", g.Tries)
		}
		if string(g.PNG) != "settled" {
			t.Fatalf("delivered %q, want the agreed frame", g.PNG)
		}
	})

	t.Run("never settles", func(t *testing.T) {
		calls := 0
		// A window that keeps changing: every grab differs from the one before.
		filmWindowCapture = func(display string, window uint32) ([]byte, x11Geometry, error) {
			calls++
			return []byte(fmt.Sprintf("paint-%d", calls)), x11Geometry{}, nil
		}
		g, err := SettledGrab(":99", filmGrabTestTerminal(7), 3, time.Millisecond)
		if err != nil {
			t.Fatalf("SettledGrab: %v", err)
		}
		if g.Settled {
			t.Fatal("a window that never settled was reported as settled")
		}
		if g.Tries != 3 {
			t.Fatalf("tries = %d, want the bound 3 — the retries are a bound, not a guarantee", g.Tries)
		}
		if g.Stale != 2 {
			t.Fatalf("stale = %d, want 2", g.Stale)
		}
		if !bytes.Equal(g.PNG, []byte("paint-3")) {
			t.Fatalf("delivered %q, want the LAST grab (the retries are a bound)", g.PNG)
		}
	})

	t.Run("capture failure is reported", func(t *testing.T) {
		filmWindowCapture = func(display string, window uint32) ([]byte, x11Geometry, error) {
			return nil, x11Geometry{}, errors.New("no such window")
		}
		if _, err := SettledGrab(":99", filmGrabTestTerminal(7), 2, time.Millisecond); err == nil {
			t.Fatal("a capture failure was swallowed")
		} else if !strings.Contains(err.Error(), "no such window") {
			t.Fatalf("the capture failure lost its cause: %v", err)
		}
	})

	t.Run("empty bytes are refused", func(t *testing.T) {
		filmWindowCapture = func(display string, window uint32) ([]byte, x11Geometry, error) {
			return nil, x11Geometry{}, nil
		}
		if _, err := SettledGrab(":99", filmGrabTestTerminal(7), 2, time.Millisecond); err == nil {
			t.Fatal("a zero-byte grab was accepted as a frame")
		}
	})
}

// TestFilmWindowIsSelectedByPID pins the identity rule: the filmed window is the
// one publishing the terminal's OWN pid. A neighboring window on the same display
// is never a candidate, and a window with no pid is not one either.
func TestFilmWindowIsSelectedByPID(t *testing.T) {
	list := XWindowList{Windows: []XWindowInfo{
		{ID: "0x1", PID: 0, Name: "no pid at all"},
		{ID: "0x2", PID: 4242, Name: "the operator's other terminal"},
		{ID: "0x3", PID: 1717, Name: "our terminal", Measured: true, W: 900, H: 400},
	}}
	w, ok := filmWindowForPID(list, 1717)
	if !ok {
		t.Fatal("the window publishing our pid was not selected")
	}
	if w.ID != "0x3" {
		t.Fatalf("selected window %s, want 0x3 — identity is the pid, never the position in the list", w.ID)
	}
	if _, ok := filmWindowForPID(list, 9999); ok {
		t.Fatal("a pid that publishes no window was matched anyway")
	}
	if _, ok := filmWindowForPID(list, 0); ok {
		t.Fatal("pid 0 matched a window — a search for 'no pid' must find nothing")
	}
}

func containsInt(xs []int, want int) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
