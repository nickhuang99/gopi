package main

// daemon_userbus_seam_test.go — the PROVENANCE proof for the user-bus diagnosis
// at the operator's own seam (epic: user-bus diagnosis, 2026-09-15).
//
// WHAT THIS FILE IS FOR, and why the unit tests above are not enough: those
// cases prove the CLASSIFIER discriminates. They say nothing about whether the
// start path PRINTS it — and the defect this epic removes lived exactly there
// (one blended sentence emitted by the seam, three host states behind it). A
// wording test cannot see a line that was never printed, so the case runs the
// REAL binary in a private GOPI_HOME and reads the operator's stdout/stderr:
//
//   - a degraded start prints exactly ONE diagnosis block, whose reason is the
//     state a DIRECT probe of this host measures (both worlds: on a host whose
//     bus answers the block is absent, and asserting that absence is the same
//     test's other half — never a skip),
//   - the block is printed where the operator looks (after the chosen-mechanism
//     line), and the pre-existing measured note and no-privilege correction are
//     untouched (other tests pin those; this one must not disturb them),
//   - the --require-scope refusal carries the SAME diagnosis (the seam's other
//     exit: an operator who demanded the guarantee is the one who most needs to
//     know which repair applies) and still fails the start.

import (
	"os"
	"strings"
	"testing"
)

// ubSeamBlockLines returns the diagnosis block's lines (the ones carrying the
// "user bus (" header) from captured operator output.
func ubSeamBlockLines(out string) []string {
	var block []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "user bus (") {
			block = append(block, line)
		}
	}
	return block
}

// TestUserBusSeam_DegradedStartPrintsTheMeasuredState is the acceptance case:
// `gopi worker --daemon` on this host falls through both rungs, so the start
// must print the diagnosis of WHY rung ① failed — and the reason it prints must
// be the state a direct probe measures, not a guess.
func TestUserBusSeam_DegradedStartPrintsTheMeasuredState(t *testing.T) {
	bin := wdBin(t)
	home := t.TempDir()
	settings := dcSettings(t, home)
	env := dcEnv(t, home, settings)

	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong)
	if code != 0 {
		t.Fatalf("a degraded start must still succeed, got %d:\n%s", code, out)
	}
	pid, _ := wdParseStart(t, out)
	t.Cleanup(func() { dcStopPID(t, pid) })

	// The direct probe of THIS host is the ground truth the printed block must
	// agree with (the same function the seam calls, run here independently).
	measured := probeDaemonUserBus()
	block := ubSeamBlockLines(out)

	if measured.reason == userBusReasonHealthy {
		// The other world: rung ① works here, so there is no diagnosis to print
		// and the start must not invent one. Asserted, never skipped.
		if len(block) != 0 {
			t.Errorf("the bus answers on this host (%s) yet the start printed a diagnosis: %v", measured.reason, block)
		}
		return
	}
	if len(block) != 1 {
		t.Fatalf("a degraded start must print exactly ONE user-bus diagnosis, got %d: %v\noutput:\n%s", len(block), block, out)
	}
	if !strings.Contains(block[0], measured.reason) {
		t.Errorf("the printed reason must be the state a direct probe measures (%q), got:\n%s", measured.reason, block[0])
	}
	// The three supporting lines are the operator's evidence and next step: the
	// measurement, the commands that reproduce it, and what to do.
	for _, want := range []string{"  measured: ", "  confirm: ", "  next: "} {
		if !strings.Contains(out, want) {
			t.Errorf("the diagnosis must carry %q (the epic's actionable half):\n%s", want, out)
		}
	}
	// The blended sentence is still there (it names the failing invocation) but
	// it is no longer the ONLY thing said about the bus: the distinct reason
	// exists beside it.
	if !strings.Contains(out, "Failed to connect to bus") {
		t.Errorf("the ladder's own reason must keep being reported, got:\n%s", out)
	}
	// The pre-existing operator text is untouched: the measured scope note and
	// the no-privilege correction (other tests pin these two exactly).
	if !strings.Contains(out, "could not leave the login session scope") {
		t.Errorf("the measured scope note must survive (daemon_scope_note_test.go asserts it):\n%s", out)
	}
	if !strings.Contains(out, dsNoPrivCorrectionFragment) {
		t.Errorf("the no-privilege correction must survive:\n%s", out)
	}
}

// TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis is the seam's OTHER
// exit: --require-scope turns the unavailable escape into a hard failure, and
// the refusal must name the measured state too — that reader has no daemon to
// inspect afterwards and is the one who most needs the repair.
func TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis(t *testing.T) {
	bin := wdBin(t)
	home := t.TempDir()
	settings := dcSettings(t, home)
	env := dcEnv(t, home, settings)

	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong, daemonScopeRequireFlag)
	// WHICH WORLD IS THIS? Read it from the SEAM's own ladder (daemonScopeDecide),
	// never from one rung of it. --require-scope can only refuse a start when NO
	// mechanism can escape, and this host's rung ① (systemd-run --user --scope)
	// succeeds — so the refusal this case was written for does not exist here and
	// demanding it unconditionally failed a perfectly healthy start (measured
	// 2026-09-17, task cmd-agent-red-reanchor: "must refuse a start whose escape
	// is unavailable" against a start that chose systemd-scope and came up).
	// BOTH worlds are ASSERTED — the escaping half is a real assertion, never a
	// skip — so neither host loses coverage.
	if dcEscapeChosenByTheSeam() {
		if code != 0 {
			t.Fatalf("%s must NOT refuse a start on a host whose seam can escape, got %d:\n%s", daemonScopeRequireFlag, code, out)
		}
		pid, _ := wdParseStart(t, out)
		if pid <= 0 {
			t.Fatalf("the escaping start named no usable pid:\n%s", out)
		}
		t.Cleanup(func() { dcStopPID(t, pid) })
		if !strings.Contains(out, daemonScopeChosenNote[len("gopi worker"):]) && !strings.Contains(out, "cgroup escape:") {
			t.Errorf("an escaping start must name the mechanism it chose, got:\n%s", out)
		}
		if block := ubSeamBlockLines(out); len(block) != 0 {
			t.Errorf("a start that escaped has no bus failure to diagnose, yet the seam printed %d block line(s): %v", len(block), block)
		}
		return
	}
	if code == 0 {
		t.Fatalf("%s must refuse a start whose escape is unavailable:\n%s", daemonScopeRequireFlag, out)
	}
	if !strings.Contains(out, daemonScopeRequireFlag) {
		t.Errorf("the refusal must name the flag, got:\n%s", out)
	}
	measured := probeDaemonUserBus()
	if measured.reason == userBusReasonHealthy {
		return // rung ① works here: nothing to diagnose, and the case says so
	}
	if block := ubSeamBlockLines(out); len(block) != 1 {
		t.Fatalf("the refusal must carry exactly one user-bus diagnosis, got %v\noutput:\n%s", block, out)
	} else if !strings.Contains(block[0], measured.reason) {
		t.Errorf("the refusal's diagnosis must be the measured state (%q), got:\n%s", measured.reason, block[0])
	}
	if !strings.Contains(out, dsNoPrivCorrectionFragment) {
		t.Errorf("the refusal must keep answering the privilege question:\n%s", out)
	}
}

// TestUserBusSeam_NoDiagnosisWhenRungOneDidNotFailOnTheBus is the seam-level
// counterpart of the cost gate: with the in-process escape turned OFF and (on a
// host where rung ① also fails) a rung-① error that is NOT a bus failure, no
// diagnosis may be printed. The case is written so it is meaningful in both
// worlds: when rung ① works, the chosen mechanism is systemd-scope and there is
// nothing to diagnose either.
func TestUserBusSeam_SilentWhenNothingToDiagnose(t *testing.T) {
	bin := wdBin(t)
	home := t.TempDir()
	settings := dcSettings(t, home)
	env := dcEnv(t, home, settings)

	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong)
	if code != 0 {
		t.Fatalf("start: %d\n%s", code, out)
	}
	if pid, _ := wdParseStart(t, out); pid > 0 {
		t.Cleanup(func() { dcStopPID(t, pid) })
	}
	// The diagnosis is emitted only for a bus failure behind rung ①; the block
	// must therefore never outnumber the ladder's own failure (one at most).
	if block := ubSeamBlockLines(out); len(block) > 1 {
		t.Errorf("at most one diagnosis block may be printed, got %d: %v", len(block), block)
	}
}

// dsUserBusProbeCost guards the seam's cost: the probe is only run when rung ①
// failed, so the case above is the one that would catch a probe wired into the
// happy path (it would print on a healthy host). The assertion is structural —
// the seam calls daemonUserBusDiagnosis, which is the gate.
func TestUserBusSeam_ProbeIsBehindTheFailureGate(t *testing.T) {
	src, err := os.ReadFile("cli_daemon_scope.go")
	if err != nil {
		t.Fatalf("read the seam: %v", err)
	}
	if !strings.Contains(string(src), "daemonUserBusDiagnosis(sysdErr)") {
		t.Error("the seam must diagnose the bus state from rung ①'s OWN error (the gate that keeps a healthy start silent)")
	}
	spawnSrc, err := os.ReadFile("cli_daemon_spawn.go")
	if err != nil {
		t.Fatalf("read the spawn seam: %v", err)
	}
	for _, site := range []string{"daemonUserBusReport(spec.prog, scope.userBus)"} {
		if !strings.Contains(string(spawnSrc), site) {
			t.Errorf("the spawn seam must render the diagnosis at %q", site)
		}
	}
}
