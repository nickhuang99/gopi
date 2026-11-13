package main

// worker_display_preflight_test.go — the ANNOUNCEMENT tests (epic
// worker-spawn-doors, GAP 1; decision record devdoc/worker-display-ownership.md).
//
// What is pinned here, and why each pin is worth a test:
//
//  1. the announcement ALWAYS prints a verdict, reachable or not. The defect
//     this closes is a SILENT failure (a worker that can never screen_capture
//     again and never says so), so "it printed nothing" must be a red test.
//  2. the display name it reports is the name it PROBED. The announcement's
//     whole value is that it describes the capability it will have at tool
//     time; an announcement of a different display than the one shot would use
//     is worse than none (Rule 2: one reader).
//  3. the unreachable text carries the two facts the operator needs — that the
//     worker can NEVER re-acquire a display, and the remedy (the durable local
//     display, spelled from its constant).
//
// The display names are NOT inline literals: they come from the constants in
// worker_display_preflight.go (Rule 1b/13 — see devdoc/worker-localhost-display.md).

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/shot"
)

// captureWorkerDisplay runs announceWorkerDisplay with stdout captured, and
// returns everything it printed.
func captureWorkerDisplay(t *testing.T) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = w
	announceWorkerDisplay()
	os.Stdout = saved
	_ = w.Close() //stylegate:ignore close of the write end of the captured-stdout pipe: the buffered announcement is drained below, and the descriptor belongs to this test

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read announcement: %v", err)
	}
	_ = r.Close() //stylegate:ignore close of the read end after the announcement was fully copied into buf
	return buf.String()
}

// TestAnnounceWorkerDisplay_NamesTheProbedDisplay: whatever the verdict, the
// line names the display the announcement RESOLVES — never an empty string
// standing in for "unknown", and never a display the prober did not touch.
// The expected name comes from the same reader the announcement uses
// (shot.ProbeDisplay), so the assertion holds on any host: with a display it
// names the reachable one, without a display the name is empty and the line
// must still not be blank (asserted by the unreachable test below).
func TestAnnounceWorkerDisplay_NamesTheProbedDisplay(t *testing.T) {
	probed, _ := shot.ProbeDisplay("") //stylegate:ignore a probe error is the host's answer (no display available), not a test failure — the assertion is that the line never goes blank
	name := workerDisplayProbeForTest()
	if name != "" {
		t.Setenv(shot.DisplayEnvName, name)
	} else {
		t.Setenv(shot.DisplayEnvName, "")
	}
	out := captureWorkerDisplay(t)
	if strings.TrimSpace(out) == "" {
		t.Fatal("announceWorkerDisplay printed nothing")
	}
	if name != "" && !strings.Contains(out, probed) {
		t.Errorf("announcement does not name the probed display %q:\n%s", probed, out)
	}
}

// TestAnnounceWorkerDisplay_UnreachableAlwaysAnnounces is the regression guard
// for Rule 15 (no silent failure) on the DISPLAY path: with no display at all,
// the worker must still say something, must say it is UNREACHABLE, must state
// that the condition is permanent for this process, and must name the remedy.
func TestAnnounceWorkerDisplay_UnreachableAlwaysAnnounces(t *testing.T) {
	t.Setenv(shot.DisplayEnvName, "")

	out := captureWorkerDisplay(t)
	if strings.TrimSpace(out) == "" {
		t.Fatal("announceWorkerDisplay printed NOTHING with no DISPLAY — that is the silent failure this preflight exists to prevent")
	}
	for _, want := range []string{
		workerDisplayLabel,
		"UNREACHABLE",
		"can NEVER re-acquire",
		workerLocalDisplay,          // the remedy: the durable local display
		workerForwardedDisplayShape, // the shape that goes stale
	} {
		if !strings.Contains(out, want) {
			t.Errorf("unreachable announcement is missing %q:\n%s", want, out)
		}
	}
	// The unreachable path must NOT claim the capability is fine.
	if strings.Contains(out, "reachable (screen_capture") {
		t.Errorf("unreachable announcement claims reachability:\n%s", out)
	}
}

// TestAnnounceWorkerDisplay_ReachableSaysSo: a reachable display announces the
// positive verdict (so the operator's fleet log distinguishes "armed" from
// "not armed" by reading one line, and never has to infer it from silence).
func TestAnnounceWorkerDisplay_ReachableSaysSo(t *testing.T) {
	name := workerDisplayProbeForTest()
	t.Setenv(shot.DisplayEnvName, name)
	if _, err := shot.ProbeDisplay(name); err != nil {
		t.Skipf("no reachable display on this host (%s: %v) — reachable-path assertion not applicable", name, err)
	}
	out := captureWorkerDisplay(t)
	if !strings.Contains(out, "reachable") {
		t.Errorf("reachable display did not announce itself as reachable:\n%s", out)
	}
	if strings.Contains(out, "UNREACHABLE") {
		t.Errorf("reachable display announced UNREACHABLE:\n%s", out)
	}
}

// workerDisplayProbeForTest returns the display the tests probe: the durable
// local one when it answers, else the ambient $DISPLAY, else "". It is a test
// helper, NOT a second reader of the environment in production code.
func workerDisplayProbeForTest() string {
	for _, candidate := range []string{workerLocalDisplay, os.Getenv(shot.DisplayEnvName)} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if _, err := shot.ProbeDisplay(strings.TrimSpace(candidate)); err == nil {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}
