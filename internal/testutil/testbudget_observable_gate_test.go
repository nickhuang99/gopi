package testutil

// THE OBSERVABLE-SURFACE GATE: the entry point must show it is alive WHILE it
// works, not after.
//
// WHAT IS GUARDED (measured 2026-10-14, task 01a13875). scripts/test-budget.sh
// printed its header only AFTER the tested-binary build, then ran
// `go test … -json >"$raw_json" 2>&1` in the foreground: the whole human summary
// is rendered from the stream only AFTER `go test` RETURNS. A caller therefore
// saw NO output for the entire suite — both streams stayed 0 bytes for 7+
// minutes, indistinguishable from a hang, and the foreground `bash` tool killed
// the run on its own 2-minute idle-output net. The run's STREAM was never the
// problem (it is written from the first byte); the OBSERVABLE surface was.
//
// THE FIX these gates pin: the header prints BEFORE the tested-binary build, and
// while `go test` runs the script emits a bounded heartbeat on ITS OWN stdout —
// never into the stream, whose bytes stay a pure `go test -json` stream, and
// never changing the run's exit code (`wait` still collects the child's status).
//
// WHY THE ARMS RUN THE REAL SCRIPT. Any warning against a silent surface is
// reproducible by BEING silent: a static grep for an `echo` would pass on a
// script that prints its header on the last line. So the dynamic arm RUNS the
// shipped entry point against a stub `go` that behaves like a real suite — it
// emits structured events, then keeps working — and reads the run's stdout
// THROUGH A PIPE while the child is alive. The stub TOUCHES A MARKER when it
// finishes, and the assertion is that the header and a heartbeat were both read
// while that marker did NOT exist: "arrived before go test finished", measured
// rather than inferred. The pure predicate is then driven with the PRE-FIX shape
// (header only at the end) as the RED PROOF, so the gate's own verdict is shown
// to be falsifiable instead of asserted.
//
// No device load is generated (Rule 27): the stub sleeps a bounded two seconds
// and writes a few bytes. The pause is what makes "before it finished"
// observable at all, and it is a `sleep`, not pressure.

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	// budgetObservableHeaderMarker is the run's first identity line — the one a
	// caller watching the run must see promptly.
	budgetObservableHeaderMarker = "=== gopi test budget run ==="
	// budgetObservableHeartbeatMarker is the SENTENCE every heartbeat line
	// carries. It is deliberately the phrase and not the bare word: the header
	// also NAMES the heartbeat (telling the caller its cadence), so a substring
	// test on "heartbeat" is satisfied by the header alone and the gate would
	// pass on a run that never emits one — measured while writing this gate, on
	// a mutant with the heartbeat loop removed.
	budgetObservableHeartbeatMarker = "go test still running"
	// budgetObservableIntervalEnv is the ONE override the script reads for the
	// heartbeat interval; the arm sets it to the floor so the gate costs seconds.
	budgetObservableIntervalEnv = "GOPI_TEST_BUDGET_HEARTBEAT_SEC"
	// budgetObservableInterval is the interval the arm asks for (the script's
	// own floor is 1 positive whole second).
	budgetObservableInterval = "1"
	// budgetObservableStubSeconds is how long the stub suite keeps working. It
	// must exceed one interval so a heartbeat is due before it finishes.
	budgetObservableStubSeconds = "3"
	// budgetObservableReadDeadline bounds how long the arm waits for a line
	// before declaring the surface silent. It is a CEILING, not a sleep.
	budgetObservableReadDeadline = 40 * time.Second
	// budgetObservableStubPkg is the package the arm requests. It only has to
	// resolve so the run's own scope check stays silent.
	budgetObservableStubPkg = "./internal/smokegate/"
	// budgetObservableMarkerName is the file the stub touches when it FINISHES —
	// the instrument that turns "arrived early" into a measurement.
	budgetObservableMarkerName = "stub-finished.marker"
	// budgetObservableStreamSuffix is the script's own stream suffix, so the arm
	// finds THE stream this run produced rather than guessing a name shape.
	budgetObservableStreamSuffix = ".json"
	// budgetObservableStreamEvents is how many structured events the stub emits:
	// the run-via-pass events of one test, plus the package-level verdict.
	budgetObservableStreamEvents = 3
	// budgetObservableStageHeader / …Heartbeat name the two stages the arm walks
	// while it watches the run's output, so "the header arrived" and "a heartbeat
	// arrived" are separate, assertable facts.
	budgetObservableStageHeader    = "header"
	budgetObservableStageHeartbeat = "heartbeat"
	// budgetObservableEventRun / …EventPass are real `go test -json` events: the
	// stream must stay a pure stream, so the stub emits exactly this shape.
	budgetObservableEventRun  = `{"Action":"run","Package":"MODULEPKG","Test":"TestStubKeepsWorking"}`
	budgetObservableEventPass = `{"Action":"pass","Package":"MODULEPKG","Test":"TestStubKeepsWorking","Elapsed":3.0}`
	budgetObservableEventDone = `{"Action":"pass","Package":"MODULEPKG","Elapsed":3.0}`
)

// budgetObservableFaults is the gate's PURE predicate over an observed line
// stream: given the lines a caller read and whether the suite had ALREADY
// finished when the last of them arrived, return one finding per way the
// observable contract is unmet. Pure so the red proof can drive the SAME
// decider with the pre-fix shape instead of a copy of its logic.
func budgetObservableFaults(lines []string, finishedBeforeLastLine bool) []string {
	var findings []string
	headerAt, heartbeatAt := -1, -1
	for i, line := range lines {
		if headerAt < 0 && strings.Contains(line, budgetObservableHeaderMarker) {
			headerAt = i
		}
		if heartbeatAt < 0 && strings.Contains(line, budgetObservableHeartbeatMarker) {
			heartbeatAt = i
		}
	}
	if headerAt < 0 {
		findings = append(findings, "the run printed no header ("+budgetObservableHeaderMarker+")")
	} else if finishedBeforeLastLine {
		// The pre-fix shape, exactly: everything arrives at the end.
		findings = append(findings, "the header arrived only AFTER the suite finished — a caller watching the run saw nothing while it worked")
	}
	if heartbeatAt < 0 {
		findings = append(findings, "the run printed no heartbeat line while the suite was running ("+budgetObservableHeartbeatMarker+")")
	}
	if headerAt >= 0 && heartbeatAt >= 0 && heartbeatAt < headerAt {
		findings = append(findings, "a heartbeat preceded the header — the run's identity must be readable before its progress")
	}
	return findings
}

// TestBudgetRunIsObservableWhileGoTestRuns is the acceptance gate: the REAL
// entry point, driven against a stub suite that keeps working, must print its
// header and at least one heartbeat while that suite is still running.
func TestBudgetRunIsObservableWhileItWorks(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	module := repoModulePath(t, root)
	scratchHome := t.TempDir()
	marker := filepath.Join(t.TempDir(), budgetObservableMarkerName)
	stubDir := t.TempDir()

	stub := "#!/usr/bin/env bash\n" +
		"# test-budget gate fixture: stands in for `go`. Generated in a test\n" +
		"# TempDir, never committed.\n" +
		"case \"$1\" in\n" +
		"  list) exit 1 ;;\n" +
		"  build)\n" +
		"    out=\"\"\n" +
		"    while [ $# -gt 0 ]; do case \"$1\" in -o) out=\"$2\";; esac; shift; done\n" +
		"    [ -n \"$out\" ] && : >\"$out\"\n" +
		"    exit 0 ;;\n" +
		"esac\n" +
		"printf '%s\\n' '" + budgetObservableEventRun + "'\n" +
		"sleep " + budgetObservableStubSeconds + "\n" +
		"printf '%s\\n' '" + budgetObservableEventPass + "'\n" +
		"printf '%s\\n' '" + budgetObservableEventDone + "'\n" +
		": >'" + marker + "'\n" +
		"exit 0\n"
	stub = strings.ReplaceAll(stub, "MODULEPKG", module+"/internal/smokegate")
	if err := os.WriteFile(filepath.Join(stubDir, budgetStubBinName), []byte(stub), 0o755); err != nil {
		t.Fatalf("write the stub go: %v", err)
	}

	cmd := exec.Command(budgetShellBin, filepath.Join(root, filepath.FromSlash(budgetScriptPath)), budgetObservableStubPkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		ScratchHomeEnvName+"="+scratchHome,
		budgetObservableIntervalEnv+"="+budgetObservableInterval,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", budgetScriptPath, err)
	}

	// Read the run's stdout THROUGH THE PIPE, while the child is alive: this is
	// the measurement a caller's idle-output net performs, and the reason the
	// gate reads a pipe instead of waiting for CombinedOutput to return.
	//
	// ORDERING, and it is not cosmetic: the scanner goroutine is the ONLY
	// reader, it closes the channel when the child's stdout reaches EOF, and
	// `cmd.Wait()` runs AFTER that. Calling Wait first closes the pipe under the
	// scanner and yields `read |0: file already closed` — a failure of the
	// INSTRUMENT, which would read exactly like a failure of the surface
	// (measured while writing this gate).
	var lines []string
	lineCh := make(chan string, 1)
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		close(lineCh)
		readErr <- scanner.Err()
	}()

	// The heartbeat must be OBSERVED while the suite is still working: the stub
	// touches its marker as its LAST act, so the marker's absence at the moment
	// the heartbeat arrives is the proof that this was a live reading rather
	// than a summary rendered afterwards.
	deadline := time.After(budgetObservableReadDeadline)
	sawHeartbeat := false
	// finishedEarly starts TRUE (pessimistic): it is cleared only by the
	// positive observation that the stub's marker was ABSENT when a heartbeat
	// line arrived, i.e. that the suite was still running. A run whose heartbeat
	// is only rendered after `go test` returns therefore cannot clear it.
	finishedEarly := true
	stage := budgetObservableStageHeader
	for line := range lineCh {
		lines = append(lines, line)
		if stage == budgetObservableStageHeader && strings.Contains(line, budgetObservableHeaderMarker) {
			stage = budgetObservableStageHeartbeat
		}
		if !sawHeartbeat && strings.Contains(line, budgetObservableHeartbeatMarker) {
			sawHeartbeat = true
			if _, statErr := os.Stat(marker); statErr != nil {
				finishedEarly = false
			}
		}
		select {
		case <-deadline:
			cmd.Process.Kill() //stylegate:ignore kills the fixture child this test started, so a silent surface cannot hang the gate
			t.Fatalf("the run emitted no complete output within %s — its surface is silent while it works\nobserved:\n%s",
				budgetObservableReadDeadline, strings.Join(lines, "\n"))
		default:
		}
	}
	if err := <-readErr; err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("reading the run's stdout failed: %v", err)
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		t.Fatalf("the observable-surface change altered the run's verdict: %v\noutput:\n%s", waitErr, strings.Join(lines, "\n"))
	}
	if !sawHeartbeat {
		t.Fatalf("the run printed no %s line at all — its surface is still silent while it works\nobserved:\n%s",
			budgetObservableHeartbeatMarker, strings.Join(lines, "\n"))
	}
	if stage == budgetObservableStageHeader {
		t.Errorf("the header line never arrived — its marker is missing from the run's output\nobserved:\n%s",
			strings.Join(lines, "\n"))
	}
	if finishedEarly {
		t.Errorf("the heartbeat arrived only after the suite had finished — the run is still silent while it works\n"+
			"observed:\n%s", strings.Join(lines, "\n"))
	}

	if findings := budgetObservableFaults(lines, false); len(findings) != 0 {
		t.Errorf("the run's observable surface fails its own contract (%v)\noutput:\n%s", findings, strings.Join(lines, "\n"))
	}

	// The stream must remain a PURE `go test -json` stream: every line an event,
	// and no heartbeat text smuggled into it. This is the property the fix was
	// forbidden to trade away (the stream is the run's own evidence).
	streams, err := filepath.Glob(filepath.Join(scratchHome, "queue", budgetConcurrentBudgetSubdir, budgetConcurrentStreamPrefix+"*"+budgetObservableStreamSuffix))
	if err != nil {
		t.Fatalf("glob streams: %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("expected exactly one stream from this run, found %d: %v", len(streams), streams)
	}
	body, err := os.ReadFile(streams[0])
	if err != nil {
		t.Fatalf("read stream %s: %v", streams[0], err)
	}
	if got := strings.Count(string(body), budgetObservableHeartbeatMarker); got != 0 {
		t.Errorf("the stream carries %d heartbeat line(s) — the heartbeat must go to the caller's stdout, never into the run's evidence", got)
	}
	events := 0
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") {
			t.Fatalf("the stream holds a non-event line %q — it is no longer a pure `go test -json` stream", line)
		}
		events++
	}
	if events != budgetObservableStreamEvents {
		t.Errorf("the stream carries %d event(s), want %d — the stub's own events must land in the stream unchanged",
			events, budgetObservableStreamEvents)
	}
}

// TestBudgetObservablePredicateIsFalsifiable is the RED PROOF, executed rather
// than asserted: the SAME predicate the gate above runs is fed the PRE-FIX shape
// (the run's whole output rendered after the suite returned) and is required to
// report it. Without this, "the gate passes" and "the gate cannot fail" are
// indistinguishable.
func TestBudgetObservablePredicateIsFalsifiable(t *testing.T) {
	if findings := budgetObservableFaults([]string{
		budgetObservableHeaderMarker,
		budgetObservableHeartbeatMarker + " t+10s: go test still running (1 bytes)",
	}, false); len(findings) != 0 {
		t.Fatalf("the fixed shape is reported as broken (%v) — every red proof below would be vacuous", findings)
	}

	// Mutation 1: the pre-fix shape — the output exists, but ALL of it arrived
	// after `go test` returned. This is exactly the defect the fix removes.
	if findings := budgetObservableFaults([]string{
		budgetObservableHeaderMarker,
		"packages: 1 finished",
	}, true); len(findings) == 0 {
		t.Errorf("a run whose whole output arrived after the suite finished passes the predicate — " +
			"the gate cannot catch the silent surface it exists for")
	}

	// Mutation 2: no heartbeat at all (a header, then silence for the suite).
	if findings := budgetObservableFaults([]string{budgetObservableHeaderMarker}, false); len(findings) == 0 {
		t.Errorf("a run that prints a header and then nothing passes the predicate — silence during the suite IS the defect")
	}

	// Mutation 3: nothing at all.
	if findings := budgetObservableFaults(nil, false); len(findings) == 0 {
		t.Errorf("a run that prints NOTHING passes the predicate")
	}
}
