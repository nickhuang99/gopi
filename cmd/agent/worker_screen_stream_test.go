package main

// worker_screen_stream_test.go — the worker's recording: the three outcomes (armed
// with frames, armed with NOTHING, declined), and the rule the epic states hardest:
// a run that produced no screen output says so and leaves NO zero-payload artifact
// behind to be mistaken for evidence.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/replayplay"
	"github.com/nick/gopi/internal/uiclient"
)

// TestWorkerScreenStreamArmedRecordsTheRoundsLines is the armed path end to end:
// open → the round's announce lines → close, and then the artifact is read back
// with the SAME reader the replay verb uses (uiclient.ReadSgrFrames), which is
// what makes "replayable" a measurement rather than a claim.
func TestWorkerScreenStreamArmedRecordsTheRoundsLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "armed"+uiclient.ScreenStreamSuffix)
	t.Setenv(cliverbs.ScreenStreamEnv, path)
	t.Setenv(cliverbs.LegacyScreenStreamEnv, "")

	tee := workerScreenStreamOpen("")
	if tee == nil {
		t.Fatalf("an explicit %s=%s armed nothing", cliverbs.ScreenStreamEnv, path)
	}
	workerRoundAnnounce("[worker] claimed by this worker: fixture\n")
	workerScreenStreamClose(tee)

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("the armed round left no artifact at %s: %v", path, err)
	}
	defer f.Close()
	meta, frames, err := uiclient.ReadSgrFrames(f)
	if err != nil {
		t.Fatalf("the artifact is not readable by uiclient.ReadSgrFrames: %v", err)
	}
	if len(frames) == 0 {
		t.Fatalf("frames = 0: the artifact records nothing, so it is not a recording")
	}
	if !meta.Trailer {
		t.Fatalf("the artifact has no close-out record: %+v", meta)
	}
	var joined bytes.Buffer
	for _, p := range frames {
		joined.Write(p)
	}
	if !strings.Contains(joined.String(), "claimed by this worker: fixture") {
		t.Fatalf("the round's own line is not in the artifact; it holds %q", joined.String())
	}
}

// TestWorkerScreenStreamEmptyRunSaysSoAndLeavesNoFile is the requirement the epic
// states verbatim: an armed run that emitted NOTHING prints 本 run 无屏幕输出 and
// leaves no zero-payload stream behind. The `ls` half is the assertion.
func TestWorkerScreenStreamEmptyRunSaysSoAndLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty"+uiclient.ScreenStreamSuffix)
	t.Setenv(cliverbs.ScreenStreamEnv, path)
	t.Setenv(cliverbs.LegacyScreenStreamEnv, "")

	tee := workerScreenStreamOpen("")
	if tee == nil {
		t.Fatalf("an explicit %s=%s armed nothing", cliverbs.ScreenStreamEnv, path)
	}
	// …and the round has nothing to say: no announce line at all.

	out := captureStdout(t, func() { workerScreenStreamClose(tee) })
	if !strings.Contains(out, workerScreenStreamNoOutput) {
		t.Fatalf("the empty round did not print %q; it printed %q", workerScreenStreamNoOutput, out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a zero-payload artifact SURVIVED at %s (stat err = %v): that file is exactly what "+
			"a reader would mistake for evidence of a screen", path, err)
	}
	// The report names the path it removed, so the absence is auditable.
	if !strings.Contains(out, path) {
		t.Fatalf("the empty report does not name the artifact it removed: %q", out)
	}
}

// TestWorkerScreenStreamDeclinedSaysSoAndWritesNothing is the 0 state on the
// worker side: the default is off, the off state announces itself, and no path is
// even resolved — so there is nothing on disk to find.
func TestWorkerScreenStreamDeclinedSaysSoAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(cliverbs.ScreenStreamEnv, "0")
	t.Setenv(cliverbs.LegacyScreenStreamEnv, "")

	out := captureStdout(t, func() {
		if tee := workerScreenStreamOpen(""); tee != nil {
			t.Fatalf("GOPI_SCREEN_STREAM=0 armed a recorder: %+v", tee)
		}
	})
	if !strings.Contains(out, config.ScreenStreamStateOff) {
		t.Fatalf("the declined round did not report its decision; it printed %q", out)
	}
	if !strings.Contains(out, workerScreenStreamNoOutput) {
		t.Fatalf("a declined round must still say %q: no artifact was asked for, and the run may "+
			"still have produced nothing; the sentence is about the RUN, not the switch",
			workerScreenStreamNoOutput)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the fixture dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("a declined round left %d entrie(s) on disk", len(entries))
	}
}

// TestWorkerScreenStreamOffByDefault pins the SURFACE default this epic chose: a
// worker paints no screen of its own, so recording it is opt-in. The chat's
// default is the opposite (armed), and both come from internal/config's one table.
func TestWorkerScreenStreamOffByDefault(t *testing.T) {
	t.Setenv(cliverbs.ScreenStreamEnv, "")
	t.Setenv(cliverbs.LegacyScreenStreamEnv, "")
	if config.ScreenStreamDefaultWorker {
		t.Fatal("the worker's default is armed: a round's progress text is not a screen")
	}
	if !config.ScreenStreamDefaultChat {
		t.Fatal("the chat's default is declined: the operator's 2026-10-31 ruling arms it")
	}
	out := captureStdout(t, func() {
		if tee := workerScreenStreamOpen(""); tee != nil {
			t.Fatalf("the unset worker switch armed a recorder: %+v", tee)
		}
	})
	if !strings.Contains(out, config.ScreenStreamOriginDefault) {
		t.Fatalf("the default decision does not name its origin: %q", out)
	}
}

// TestWorkerScreenStreamLoopIsQuietWhenARoundReported is the loop boundary's ONE
// rule: two speakers for one run would make an operator count lines instead of
// reading one.
func TestWorkerScreenStreamLoopIsQuietWhenARoundReported(t *testing.T) {
	t.Setenv(cliverbs.ScreenStreamEnv, "1")
	t.Setenv(cliverbs.LegacyScreenStreamEnv, "")
	w := watchWorkerScreenStreamNoRound()
	workerScreenStreamOpened() // a round consulted the switch and reported for itself
	out := captureStdout(t, w.report)
	if strings.TrimSpace(out) != "" {
		t.Fatalf("the loop boundary spoke over a round's own report: %q", out)
	}
}

// TestWorkerScreenStreamLoopSaysSoWhenNoRoundRan is the drained/idle case: the
// switch was armed and nothing ran, so the sentence is printed and the report names
// that NO path was opened (nothing to remove, nothing to mistake).
func TestWorkerScreenStreamLoopSaysSoWhenNoRoundRan(t *testing.T) {
	t.Setenv(cliverbs.ScreenStreamEnv, "1")
	t.Setenv(cliverbs.LegacyScreenStreamEnv, "")
	w := watchWorkerScreenStreamNoRound()
	out := captureStdout(t, w.report)
	if !strings.Contains(out, workerScreenStreamNoOutput) {
		t.Fatalf("a run that never ran a round did not say %q; it printed %q", workerScreenStreamNoOutput, out)
	}
	if !strings.Contains(out, "no round ran") {
		t.Fatalf("the report does not say WHY there is no artifact: %q", out)
	}
}

// TestWorkerScreenStreamDeclaresAPaintableGridOffATty is the regression guard for a
// MEASURED defect: the first real worker run recorded `grid=0x0` (stdout was a
// pipe) and `gopi session replay --from stream` refused the artifact by name —
// "declares grid=0x0, which cannot be painted: a screen needs both dimensions".
// A recording that cannot be replayed is not a recording, so the header must carry
// the documented default and the run must SAY the size was not measured.
func TestWorkerScreenStreamDeclaresAPaintableGridOffATty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grid"+uiclient.ScreenStreamSuffix)
	t.Setenv(cliverbs.ScreenStreamEnv, path)
	t.Setenv(cliverbs.LegacyScreenStreamEnv, "")

	// The test process's stdout IS a pipe under `go test`, which is exactly the
	// worker's normal case — no pty is created, so the fallback rung is the one
	// under test.
	captureStdout(t, func() {
		if tee := workerScreenStreamOpen(""); tee == nil {
			t.Fatalf("an explicit %s=%s armed nothing", cliverbs.ScreenStreamEnv, path)
		} else {
			// ONE line, so this is the ARMED path and not the empty case (an empty
			// round removes its artifact, which is a different assertion above).
			workerRoundAnnounce("[worker] claimed by this worker: grid fixture\n")
			workerScreenStreamClose(tee)
		}
	})

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("no artifact at %s: %v", path, err)
	}
	defer f.Close()
	meta, _, err := uiclient.ReadSgrFrames(f)
	if err != nil {
		t.Fatalf("read the header: %v", err)
	}
	if meta.Grid[0] <= 0 || meta.Grid[1] <= 0 {
		t.Fatalf("the header declares grid=%dx%d: a replay refuses that by name (a screen needs "+
			"both dimensions), so the recording is an artifact nobody can paint", meta.Grid[0], meta.Grid[1])
	}
	if meta.Grid[0] != replayplay.FallbackCols || meta.Grid[1] != replayplay.FallbackRows {
		t.Fatalf("off a tty the grid must be the documented default %dx%d; got %dx%d",
			replayplay.FallbackCols, replayplay.FallbackRows, meta.Grid[0], meta.Grid[1])
	}
}
