package main

// worker_terminal_scope_hint_test.go — the CLI half of the terminal-scope hint
// (task 01a163f3, measured 2026-10-22 22:04:55: a foreground `gopi worker`
// inside a `vte-spawn-*.scope` was HUNG UP when its terminal tab was torn down,
// `signal:SIGHUP`, `sourced:false`).
//
// THREE CASES, and they are the three the epic names:
//
//	(a) a terminal (vte-spawn) scope        → the banner line IS rendered
//	(b) a `gopi-daemon-worker-*.scope`      → NO line (the daemon already escaped)
//	(c) an empty / unreadable measurement   → NO line, and no error surfaced
//
// WHAT A UNIT TEST CANNOT SEE is the WIRING — that the line is reached on the
// FOREGROUND face and only there (after the --daemon branch returned, after the
// -h scan). That half is a source pin at the bottom, the same instrument the
// (since withdrawn, 2026-10-23) used for its call site.

import (
	"errors"
	"strings"
	"testing"
)

// The two measured shapes this suite presents to the decision function.
const (
	// hintTerminalScope is this host's own /proc/self/cgroup (a terminal tab).
	hintTerminalScope = "/user.slice/user-1000.slice/user@1000.service/app.slice/" +
		"app-org.gnome.Terminal.slice/vte-spawn-668117f9-0083-4d3d-9197-a71f438c5647.scope"
	// hintDaemonScope is where `gopi worker --daemon` really runs: a transient
	// scope named from cgroupscope.EscapeTargetPrefix, created by the daemon seam.
	hintDaemonScope = "/user.slice/user-1000.slice/user@1000.service/app.slice/" +
		"gopi-daemon-worker-nicksager.scope"
)

// TestWorkerTerminalScopeHint_ATerminalScopeIsNamed pins case (a): the line is
// rendered, it NAMES the measured scope (so an operator can check it with
// `systemd-cgls`), and it names the remedy — the `--daemon` invocation built from
// the same constant the flag parser reads.
func TestWorkerTerminalScopeHint_ATerminalScopeIsNamed(t *testing.T) {
	line := workerTerminalScopeHintLine(hintTerminalScope)
	if line == "" {
		t.Fatal("case (a): a terminal (vte-spawn) scope produced NO hint line — the measured defect " +
			"(SIGHUP on tab teardown, 2026-10-22 22:04:55) is once again silent")
	}
	if !strings.Contains(line, hintTerminalScope) {
		t.Errorf("case (a): the line does not name the measured scope %q, so the operator cannot re-check "+
			"the claim; got: %s", hintTerminalScope, line)
	}
	if !strings.Contains(line, workerScopeHintRemedy) {
		t.Errorf("case (a): the line does not name the remedy %q; got: %s", workerScopeHintRemedy, line)
	}
	if !strings.Contains(line, daemonFlagLong) {
		t.Errorf("case (a): the remedy no longer carries %q — the advice drifted from the flag", daemonFlagLong)
	}
	if strings.Contains(line, "\n\n") {
		t.Errorf("case (a): the hint must be exactly ONE line; got: %q", line)
	}
	if n := len(strings.Split(strings.TrimRight(line, "\n"), "\n")); n != 1 {
		t.Errorf("case (a): the hint is %d lines, want 1", n)
	}
	// The consequence must be stated, not just the remedy: an operator who reads
	// only the remedy learns a habit, not the reason.
	if !strings.Contains(line, "SIGHUP") {
		t.Errorf("case (a): the line does not name the consequence (SIGHUP); got: %s", line)
	}
}

// TestWorkerTerminalScopeHint_DaemonScopeIsSilent pins case (b): the scope the
// `--daemon` face acquires is NOT a terminal scope, so the foreground banner has
// nothing to say about it. This is the case that makes the banner usable — a
// line that fired there would train the operator to ignore it.
func TestWorkerTerminalScopeHint_DaemonScopeIsSilent(t *testing.T) {
	if line := workerTerminalScopeHintLine(hintDaemonScope); line != "" {
		t.Errorf("case (b): a gopi-daemon-worker-*.scope produced the terminal hint; the daemon already "+
			"left the terminal's lifetime and this line is a false alarm. got: %s", line)
	}
}

// TestWorkerTerminalScopeHint_NoMeasurementIsSilent pins case (c): an EMPTY
// measurement and a FAILED one both print nothing, and the failure is swallowed
// on purpose — a hint must never be what fails a worker start.
func TestWorkerTerminalScopeHint_NoMeasurementIsSilent(t *testing.T) {
	if line := workerTerminalScopeHintLine(""); line != "" {
		t.Errorf("case (c): an empty cgroup path produced a hint; a guess is worse than silence. got: %s", line)
	}
	// The FAILED read, through the real printer.
	prev := workerSelfCgroup
	workerSelfCgroup = func() (string, error) {
		return "", errors.New("simulated: /proc/self/cgroup unreadable")
	}
	t.Cleanup(func() { workerSelfCgroup = prev })
	if got := captureStderr(t, reportWorkerTerminalScopeHint); got != "" {
		t.Errorf("case (c): an unreadable /proc/self/cgroup printed %q; the hint must stay silent when "+
			"the measurement could not be taken (Rule 24: no fresh read, no verdict)", got)
	}
}

// TestWorkerTerminalScopeHint_PrinterRendersTheLine pins the PRINTER (not just
// the pure renderer): on a terminal scope the line really reaches stderr, so the
// operator reads it on the stream the neighbouring notices use.
func TestWorkerTerminalScopeHint_PrinterRendersTheLine(t *testing.T) {
	prev := workerSelfCgroup
	t.Cleanup(func() { workerSelfCgroup = prev })
	workerSelfCgroup = func() (string, error) { return hintTerminalScope, nil }
	got := captureStderr(t, reportWorkerTerminalScopeHint)
	if !strings.Contains(got, workerScopeHintRemedy) {
		t.Errorf("the printer emitted %q, which does not carry the remedy %q", got, workerScopeHintRemedy)
	}
	// And the negative through the same printer.
	workerSelfCgroup = func() (string, error) { return hintDaemonScope, nil }
	if got := captureStderr(t, reportWorkerTerminalScopeHint); got != "" {
		t.Errorf("the printer emitted %q for a daemon scope; want nothing", got)
	}
}

// TestWorkerTerminalScopeHint_IsMeasuredByTheOnePackage pins the REUSE Rule 1b
// asks for: the marker knowledge lives in internal/cgroupscope, and this face
// asks it rather than carrying a second copy of "vte-spawn".
func TestWorkerTerminalScopeHint_IsMeasuredByTheOnePackage(t *testing.T) {
	src := stripCommentsForRetirement(cliTasksSource(t, "worker_start_notices.go"))
	if !strings.Contains(src, "cgroupscope.SelfRel") {
		t.Error("worker_start_notices.go no longer reads the measurement through cgroupscope.SelfRel — the " +
			"hint's evidence must be the package's ONE /proc read (Rule 24)")
	}
	if !strings.Contains(src, "cgroupscope.TerminalScope") {
		t.Error("worker_start_notices.go no longer asks cgroupscope.TerminalScope — a local copy of the " +
			"marker table would drift from the escape classifier (Rules 13/14)")
	}
	if strings.Contains(src, "vte-spawn") {
		t.Error("worker_start_notices.go spells a terminal marker inline; the markers belong to " +
			"internal/cgroupscope (Rule 1b: one spelling per value)")
	}
}

// TestWorkerTerminalScopeHint_SitsOnTheForegroundFaceOnly pins the WIRING: the
// call site is in main.go, it comes AFTER the --daemon branch (so the daemon
// face never prints it) and BEFORE runTaskWorker (so it precedes the loop's
// first round, which is when a terminal's teardown would first hurt).
func TestWorkerTerminalScopeHint_SitsOnTheForegroundFaceOnly(t *testing.T) {
	mainSrc := stripCommentsForRetirement(cliTasksSource(t, "main.go"))
	call := strings.Index(mainSrc, "reportWorkerTerminalScopeHint()")
	if call < 0 {
		t.Fatal("main.go never calls reportWorkerTerminalScopeHint — the banner is dead code and the " +
			"measured SIGHUP death is once again silent")
	}
	daemonBranch := strings.Index(mainSrc, "workerDaemonSpawn(args[1:])")
	if daemonBranch < 0 {
		t.Fatal("the --daemon branch is gone from main.go; this pin must be re-pointed")
	}
	if call < daemonBranch {
		t.Error("the hint is printed BEFORE the --daemon branch — `gopi worker --daemon` would carry a " +
			"foreground warning about a lifetime it has already left")
	}
	loop := strings.Index(mainSrc, "runTaskWorker(wa.once, wa.explicit, wa.runOptions())")
	if loop < 0 {
		t.Fatal("the foreground loop call is gone from main.go; this pin must be re-pointed")
	}
	if call > loop {
		t.Error("the hint is printed AFTER the foreground loop starts — it could never be read before the " +
			"first round, which is the moment the warning is about")
	}
}
