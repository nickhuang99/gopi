package main

// daemon_cgroup_e2e_test.go — the epic daemon-cgroup-detach ACCEPTANCE proof,
// run against the REAL binary in a private GOPI_HOME.
//
// WHY IT EXISTS (incidents 2026-09-12 04:12 and 2026-09-13 06:40): the shared
// self-daemonization seam detached with fork/exec-self + setsid, which makes the
// child a session leader but leaves it a MEMBER of the login session's cgroup.
// When that session's systemd scope was torn down every member received SIGTERM,
// so a "detached" daemon died with the ssh session that started it.
//
// THE PROPERTY IS A CGROUP PROPERTY, AND /proc/<pid>/cgroup IS THE ONLY JUDGE.
// The classic mistake is to assert the MECHANISM (setsid succeeded, a wrapper
// ran, the argv looks right) and call it independence. It is not: setsid leaves
// the cgroup untouched, and on this host a cgroup.procs write has been observed
// to return SUCCESS while /proc/self/cgroup still named the old cgroup. Every
// case below therefore reads the daemon's LIVE /proc/<daemon pid>/cgroup — the
// same file the incident was diagnosed from.
//
// THE HOST-DEPENDENT CASE IS A PROVENANCE PROOF, NOT A SKIP-IF-ABSENT PROOF.
// Whether the escape succeeds depends on the host's systemd delegation, so the
// cases run in both worlds and the assertion is:
//
//   - escape available → the daemon's cgroup is NOT a session scope (the epic's
//     acceptance criterion A/B), and the parent announced the mechanism,
//   - escape unavailable → the daemon IS still in the session scope, AND the
//     start said so on stderr and in its log. That is the epic's acceptance
//     criterion D: an explicit, documented, NON-SILENT degradation.
//
// A case that merely skipped on an undelegated host would leave the regression
// this epic exists to prevent free to return, announced by nobody.
//
// HERMETICITY (Rule 6): a private GOPI_HOME with an empty repos table, no sync
// peers, an idle budget the case controls, and the resident scratch sweep
// throttled far into the future. Every daemon is stopped by its EXPLICIT pid
// (never a pattern — the operator has live merger/worker daemons on this host)
// and every kill carries provenance (signalrec's intent record, the house
// rule from child_stop.go).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/cgroupscope"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/signalrec"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// dcPollInterval is the E2E poll cadence (Rule 1: one named literal).
	dcPollInterval = 100 * time.Millisecond
	// dcLogWait bounds how long the daemon's log may take to carry its scope
	// line (the child writes it before the handshake byte, so this is generous).
	dcLogWait = 20 * time.Second
	// dcIdleSec / dcIdleExits keep the detached worker's idle budget tiny so a
	// case can safely stop it by pid without racing its self-exit.
	dcIdleSec   = "30"
	dcIdleExits = "3"
	// dcScopeDisabledThrottleMin disables the resident scratch sweep for the
	// probe daemon (Rule 6: an E2E never sweeps real state). It is the sweep's
	// OWN knob (scratch.GCIntervalEnvName), never a test-only bypass.
	dcScopeDisabledThrottleMin = "100000"
	// dcScopeOKMarker / dcScopeDegradedMarker are the two lines the child's
	// daemonScopeChildEscape writes into the daemon's own log. They are the
	// child's MEASUREMENT (its own /proc/self/cgroup), as opposed to the
	// parent's prediction.
	dcScopeOKMarker       = daemonScopeLogPrefix + " ok"
	dcScopeDegradedMarker = daemonScopeLogPrefix + " DEGRADED"
)

// dcSettings writes the sandbox settings (an EMPTY repos table, so no pass ever
// touches a real repository).
func dcSettings(t *testing.T, home string) string {
	t.Helper()
	gopiDir := filepath.Join(home, ".gopi")
	if err := os.MkdirAll(gopiDir, 0o755); err != nil {
		t.Fatalf("mkdir gopi dir: %v", err)
	}
	path := filepath.Join(gopiDir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"repos":{}}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return path
}

// dcEnv is the daemon's environment: a private profile, an empty ledger, no
// compute peers, no claim broadcast, the scratch sweep off, and an idle budget
// the case controls. The agent signature is stripped (stripAgentSignature), so
// the probe runs with the OPERATOR's shell shape — the only one in which
// "the daemon starts" is a meaningful assertion.
func dcEnv(t *testing.T, home, settings string, extra ...string) []string {
	t.Helper()
	return stripAgentSignature(append(os.Environ(),
		append([]string{
			"GOPI_HOME=" + home,
			"GOPI_SETTINGS=" + settings,
			workerIdleIntervalEnv + "=" + dcIdleSec,
			workerIdleExitsEnv + "=" + dcIdleExits,
			scratch.GCIntervalEnvName + "=" + dcScopeDisabledThrottleMin,
		}, extra...)...))
}

// dcCgroupOf reads /proc/<pid>/cgroup through the SHARED reader (no second
// parsing of the file: the production classifier and this assertion must agree
// by construction, not by luck).
func dcCgroupOf(t *testing.T, pid int) string {
	t.Helper()
	rel, err := cgroupscope.RelOfPID(pid)
	if err != nil {
		t.Fatalf("read cgroup of pid %d: %v", pid, err)
	}
	return rel
}

// dcWaitDaemonLog waits for the daemon's log to carry a marker, returning what
// it saw when the budget expires (a bare boolean would hide the diagnostic).
func dcWaitDaemonLog(t *testing.T, logPath, marker string) bool {
	t.Helper()
	deadline := time.Now().Add(dcLogWait)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(b), marker) {
			return true
		}
		time.Sleep(dcPollInterval) //stylegate:allow-sleep bounded wait for the daemon's own scope line
	}
	return false
}

// dcStopPID stops a daemon by EXPLICIT pid with provenance: the intent record
// is written BEFORE the signal (child_stop.go's house rule — a SIGKILL leaves
// the victim no chance to record anything, so the killer must).
func dcStopPID(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	if syscall.Kill(pid, 0) != nil {
		return
	}
	if err := signalrec.Intent(daemonScopeUnitPrefix+"test", "daemon cgroup e2e cleanup", pid, 0); err != nil {
		// A missing PROVENANCE RECORD must not fail the test (signalrec's own
		// contract: the refusal is about the record only), but it must be
		// visible — silence here is what turned a stale scratch home into an
		// unexplained log line in the acceptance run.
		t.Logf("could not record the cleanup intent for pid %d: %v", pid, err)
	}
	// The pid arrived by PARSING the daemon's own success line (`wdParseStart`),
	// so it is a number out of a log, not a handle: this is exactly the shape
	// Rule 42 forbids delivering to blind — read the incarnation first.
	sigID, sigErr := testutil.ReadProcIdentity(pid)
	if sigErr != nil {
		return // already gone: there is no incarnation left to address
	}
	//stylegate:ignore best-effort stop of our own probe daemon; the guard refuses a recycled pid
	_ = testutil.KillIfIdentical(sigID, syscall.SIGTERM)
	deadline := time.Now().Add(dcLogWait)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(dcPollInterval) //stylegate:allow-sleep bounded wait for our probe daemon to exit
	}
	t.Logf("probe daemon pid %d still alive after SIGTERM", pid)
}

// dcEscapeChosenByTheSeam reports whether a detach on THIS host really leaves
// the login session scope — asked of the SEAM, not of one mechanism.
//
// WHY NOT cgroupscope.Available ALONE (the defect this replaces, measured
// 2026-09-17): Available answers for the IN-PROCESS cgroupfs rung only, while
// the seam's ladder is "① systemd-run --scope --user, ② the in-process escape,
// ③ an announced setsid-only degradation" — and on this host rung ① is the one
// that WINS (the user bus answers). Fanning the two rungs out into one boolean
// made every case below read the host backwards: it took the "escape
// unavailable" arm, asserted the daemon was still session-scoped, and failed
// against a daemon that had genuinely escaped into its own systemd scope.
//
// The parenthetical that used to sit on the previous sentence — "Available
// returns ErrNoEscape because user-1000.slice is root:root 0755" — described
// the NAIVE predicate and is not true of the current one (epic 01a12c31: from
// this position Available now returns nil, measured). The conclusion is
// unaffected, and for a better reason than before: Available still answers for
// rung ② only, so rung ① winning is still invisible to it.
//
// It stays a source of truth rather than a literal: the value comes from
// daemonScopeDecide, the SAME function production calls before a detach
// (cli_daemon_scope.go), so a test can never disagree with the binary.
func dcEscapeChosenByTheSeam() bool {
	return daemonScopeDecide(nil).scopeEscaped()
}

// dcStartDetached starts `gopi <verb> --daemon` in a sandbox, waits for the
// parent's success line, and returns the daemon pid plus the parent's combined
// output. The cleanup is registered so no case can leak a resident process.
func dcStartDetached(t *testing.T, bin, home, settings, verb string) (int, string) {
	t.Helper()
	env := dcEnv(t, home, settings)
	code, out := wdRun(t, bin, env, verb, daemonFlagLong)
	if code != 0 {
		t.Fatalf("`gopi %s --daemon` must exit 0, got %d:\n%s", verb, code, out)
	}
	// The success line is the FACE's own (the worker's says "worker daemon
	// started"), so the parser is the one that face's E2E already owns — never a
	// second, drifting parse. (The merger had a sibling line until epic
	// daemon-machinery-removal, 2026-09-17; it has no detach face now.)
	pid, _ := wdParseStart(t, out)
	if pid <= 0 {
		t.Fatalf("success line named no usable pid:\n%s", out)
	}
	t.Cleanup(func() { dcStopPID(t, pid) })
	return pid, out
}

// dcDaemonEnvValue reads one variable out of a live process's environment (the
// mechanism stamp is carried there on purpose: an operator can grep a running
// daemon's /proc/<pid>/environ for the answer).
func dcDaemonEnvValue(t *testing.T, pid int, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
	if err != nil {
		t.Fatalf("read environ of pid %d: %v", pid, err)
	}
	for _, kv := range strings.Split(string(b), "\x00") {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			return v
		}
	}
	return ""
}

// TestWorkerDaemon_LeavesTheLoginSessionScope is criterion A for the WORKER
// face: a detached worker started from a normal shell does not carry the
// session scope in /proc/<pid>/cgroup when the host can escape — and when it
// cannot, the start SAYS SO (criterion D) and the daemon is honestly still in
// the scope.
func TestWorkerDaemon_LeavesTheLoginSessionScope(t *testing.T) {
	bin := wdBin(t)
	home := t.TempDir()
	settings := dcSettings(t, home)

	pid, out := dcStartDetached(t, bin, home, settings, CliVerbWorker)
	rel := dcCgroupOf(t, pid)
	logPath := filepath.Join(home, ".gopi", daemonLogDirName, workerDaemonLogBaseName)

	escapable := dcEscapeChosenByTheSeam()
	if escapable {
		if cgroupscope.InSessionScope(rel) {
			t.Fatalf("worker daemon pid %d is still in the login session scope %q — setsid alone does not leave it. Parent said:\n%s", pid, rel, out)
		}
		if !dcWaitDaemonLog(t, logPath, dcScopeOKMarker) {
			b, _ := os.ReadFile(logPath) //stylegate:ignore diagnostic read-back AFTER the assertion already failed: the log is dumped to explain it, so its error cannot be reported
			t.Errorf("the daemon's log never carried %q (its own /proc/self/cgroup measurement):\n%s", dcScopeOKMarker, string(b))
		}
		return
	}
	// The escape is unavailable on this host: the requirement is not success
	// but SILENCE ABOUT NOTHING — the parent names the reason, the log repeats
	// it, and the daemon is honestly reported as still session-scoped.
	if !strings.Contains(out, "could not leave the login session scope") {
		t.Errorf("an unavailable escape must be announced by the parent, got:\n%s", out)
	}
	if !cgroupscope.InSessionScope(rel) {
		t.Errorf("with no escape available, /proc/%d/cgroup = %q: the cgroup must still be the session scope (a different answer means the test's model of the host is wrong)", pid, rel)
	}
	if !dcWaitDaemonLog(t, logPath, dcScopeDegradedMarker) {
		b, _ := os.ReadFile(logPath) //stylegate:ignore diagnostic read-back AFTER the assertion already failed: the log is dumped to explain it, so its error cannot be reported
		t.Errorf("the daemon's log never carried %q:\n%s", dcScopeDegradedMarker, string(b))
	}
}

// TestMergerDaemon_LeavesTheLoginSessionScope IS GONE (epic
// daemon-machinery-removal, 2026-09-17): there is no merger daemon to place in a
// cgroup. The MERGER half of the escape machinery went with the resident form —
// a one-tick process is started by cron, not detached by a caller, so it has no
// login session to escape from and no start proof to make. The WORKER face's
// cases below are untouched: `gopi worker --daemon` is still a resident service
// with the same detach contract.

// TestWorkerDaemon_StartProofAndPidSurviveTheScopeChange is the regression half
// of criterion C: adding the escape must not weaken the START PROOF contract —
// the caller still returns promptly, the success line still names a LIVE pid,
// the detached child has still genuinely left the caller's process tree (proved
// by its LIVE cgroup and by the reaping ancestor that now owns it, never by a
// PPID literal — Rule 24), and its stdio still lands in the face's log.
func TestWorkerDaemon_StartProofAndPidSurviveTheScopeChange(t *testing.T) {
	bin := wdBin(t)
	home := t.TempDir()
	settings := dcSettings(t, home)

	start := time.Now()
	pid, out := dcStartDetached(t, bin, home, settings, CliVerbWorker)
	elapsed := time.Since(start)
	// The scope probe is a real `systemd-run` invocation, so the caller's
	// budget is the handshake budget plus that probe — never an open-ended wait.
	if elapsed > wdPromptBudget {
		t.Errorf("--daemon held the invoking shell %s (budget %s) — the pre-detach scope probe must stay bounded", elapsed, wdPromptBudget)
	}
	if !wdProcAlive(pid) {
		t.Fatalf("worker daemon pid %d is not alive right after the launcher returned", pid)
	}
	// Still a REAL detach. WHICH fact proves it depends on the mechanism the
	// seam chose — and the criterion is the CGROUP, never PPID or SID (Rule 24).
	//
	// MEASURED 2026-10-12: this arm used to demand PPID == 1 unconditionally, and
	// it was RED here AND on pristine origin/master (control tree at 7a7f5c9b7,
	// the same message, PPID 3100) — the differential that attributes it to the
	// anchor rather than to any branch. On this host the seam takes rung ①
	// (`systemd-run --user --scope`), so the daemon is started by the transient
	// service manager, and a daemon that has genuinely LEFT the login session
	// scope has PPID = that manager (measured: 3100 = /lib/systemd/systemd
	// --user), never 1. The sibling case states the trap in full
	// (worker_daemon_flag_e2e_test.go, TestWorkerDaemon_LeavesTheLoginSessionScope:
	// "PPID and SID are NOT evidence ... BOTH fail against a daemon that detached
	// the STRONGEST way"). The mode is read from the daemon's OWN environment
	// stamp — the parent puts it there on purpose — never predicted here.
	//
	// Rung ① is not weaker for being asserted differently: it is proved by the
	// pair the sibling case already owns (the LIVE cgroup has left the session
	// scope AND names the scope unit the daemon was started under), which the
	// PPID form could not express at all.
	switch mode := wdDaemonScopeMode(t, pid); mode {
	case daemonScopeModeSysd:
		// Rung ① detaches by SCOPE — the transient manager owns the unit, so
		// reparenting to init is not what it does and not what proves it.
		rel := dcCgroupOf(t, pid)
		if cgroupscope.InSessionScope(rel) {
			t.Errorf("worker daemon pid %d was detached by %s but its cgroup is still the login session scope %q", pid, mode, rel)
		}
		unit := dcDaemonEnvValue(t, pid, daemonScopeUnitEnv)
		if unit == "" {
			t.Fatalf("worker daemon pid %d was detached by %s but carries no %s stamp", pid, mode, daemonScopeUnitEnv)
		}
		if !strings.Contains(rel, unit) {
			t.Errorf("worker daemon pid %d cgroup %q does not name the scope unit %q it was started under — the unit is the proof of the %s detach", pid, rel, unit, mode)
		}
	default:
		// The setsid-bearing paths (the in-process cgroupfs escape, and the
		// announced setsid-only degradation). Each is judged on ITS OWN
		// mechanism, and the cgroup is the judge for both.
		//
		// MEASURED 2026-10-12 on this host: this arm used to demand PPID == 1
		// ("reparented to init"), and it was RED here — reached for real through
		// the seam's own off-switch (GOPI_CGROUP_ESCAPE_OFF=1, which dcEnv
		// inherits), failing verbatim with "PPID=3100, want 1" against a daemon
		// that had genuinely detached. 3100 is /lib/systemd/systemd --user: the
		// kernel made it the child-subreaper of user.slice, so a daemon
		// abandoned by its session lands THERE and never on init. The literal
		// asserted one host's reaper instead of the mechanism — the same defect
		// the rung ① arm above already had removed, left behind in the arm the
		// host does not take day to day. NOT deleted: the property (this daemon
		// left the login session scope, and it is no longer the caller's child)
		// is asserted below, per rung, at full strength.
		if mode == daemonScopeModeCgfs {
			// Rung ② detaches IN PROCESS by writing its own cgroup: the daemon's
			// LIVE cgroup is the proof, and it must have left the session scope
			// (the same statement production makes — scopeEscaped is true for
			// cgroupfs-scope, cli_daemon_scope.go).
			if rel := dcCgroupOf(t, pid); cgroupscope.InSessionScope(rel) {
				t.Errorf("worker daemon pid %d was detached by %s but its cgroup is still the login session scope %q — the in-process escape did not take", pid, mode, rel)
			}
		}
		if mode == daemonScopeModeNone {
			// Rung ③ is an ANNOUNCED DEGRADATION, not a detach: the daemon is
			// honestly still in the login session scope, and the start said so
			// (criterion D — TestWorkerDaemon_RequireScopeOffSwitchIsADocumentedDegradation
			// pins the announcement itself). A daemon that had escaped anyway
			// would mean the stamp and the mechanism disagree.
			if rel := dcCgroupOf(t, pid); !cgroupscope.InSessionScope(rel) {
				t.Errorf("worker daemon pid %d is stamped %s (no escape ran) but its cgroup %q has left the login session scope — the stamp and the mechanism disagree", pid, mode, rel)
			}
		}
		// REPARENTING, where it is genuinely wanted, is asked as the FACT — which
		// process reaps a daemon that lands here — never as the literal 1. On
		// this host that is the user session's manager (measured: 3100, the same
		// value this arm used to fail on); on a host with no user session it is
		// init. ProcReaperOf MEASURES which one applies to this landing place
		// (internal/testutil/procident.go: it identifies a reaper by what it IS
		// — pid 1, or the systemd manager carrying --user on its own argv — and
		// walks ancestry to find the one above this test). The assertion still
		// catches exactly what the PPID form caught: the caller (this test) must
		// no longer be the parent of a daemon it detached from.
		want, why := testutil.ProcReaperOf()
		if want <= 0 {
			// An unmeasurable reaper is REPORTED, never treated as a pass: the
			// test's model of this host would then be wrong, and a silent
			// "could not tell" is the failure mode a detach proof exists to
			// prevent (Rule 15).
			t.Errorf("cannot name this host's reaping ancestor for worker daemon pid %d (wanted to assert PPID == the reaper): %s", pid, why)
			break
		}
		if ppid := wdProcessField(t, pid, wdProcessFieldPPID); ppid != want {
			t.Errorf("worker daemon pid %d has PPID=%d, want this host's reaping ancestor %d — the %s escape is a setsid detach and must not leave the daemon a child of the caller", pid, ppid, want, mode)
		}
	}
	// The stale-window check: the cgroup that is read must be the LIVE one, and
	// it must be readable twice with the same answer (a value that flips would
	// mean the assertion raced the migration).
	first := dcCgroupOf(t, pid)
	if second := dcCgroupOf(t, pid); first != second {
		t.Errorf("the daemon's cgroup changed between two reads (%q → %q): the verdict raced the migration", first, second)
	}
	// The start line names the mechanism that was chosen — an operator must be
	// able to read WHICH escape ran, not just that one was attempted.
	// The launcher must report the mechanism it chose — matched on a fragment
	// that is UNIQUE to that line (the log prefix daemonScopeLogPrefix is also
	// the prefix of the child's DEGRADED line, so a bare prefix match would
	// pass on the warning alone).
	if !strings.Contains(out, "cgroup escape: ") {
		t.Errorf("the launcher must report the cgroup escape it chose, got:\n%s", out)
	}
	mode := dcDaemonEnvValue(t, pid, daemonScopeModeEnv)
	switch mode {
	case daemonScopeModeSysd, daemonScopeModeCgfs, daemonScopeModeNone:
	default:
		t.Errorf("daemon pid %d carries %s=%q, which is not one of the three known mechanisms", pid, daemonScopeModeEnv, mode)
	}
	// stdio redirect is untouched: the loop banner is in the face's log.
	logPath := filepath.Join(home, ".gopi", daemonLogDirName, workerDaemonLogBaseName)
	if !wdWaitLogContains(t, logPath, "worker: loop start", dcLogWait) {
		t.Errorf("worker daemon log %s never showed the loop banner", logPath)
	}
}

// TestWorkerDaemon_RequireScopeIsHardWhenUnavailable is criteria D and E's
// policy half: --require-scope turns "no escape available" into a refused
// start (non-zero, message naming the reason, no orphan) — and when the host
// CAN escape, the same flag must still start the daemon normally (the flag is
// a policy, never a switch that breaks the happy path).
func TestWorkerDaemon_RequireScopeIsHardWhenUnavailable(t *testing.T) {
	bin := wdBin(t)
	home := t.TempDir()
	settings := dcSettings(t, home)
	env := dcEnv(t, home, settings)
	escapable := dcEscapeChosenByTheSeam()

	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong, daemonScopeRequireFlag)
	if escapable {
		if code != 0 {
			t.Fatalf("--require-scope must not break a start on a host that CAN escape, got %d:\n%s", code, out)
		}
		pid, _ := wdParseStart(t, out)
		t.Cleanup(func() { dcStopPID(t, pid) })
		if rel := dcCgroupOf(t, pid); cgroupscope.InSessionScope(rel) {
			t.Errorf("--require-scope started a daemon that is still in the session scope %q", rel)
		}
		return
	}
	if code == 0 {
		t.Fatalf("--require-scope MUST refuse a start that cannot leave the session scope (a silent fake detach is the defect):\n%s", out)
	}
	if !strings.Contains(out, daemonScopeRequireFlag) {
		t.Errorf("the refusal must name the flag, got:\n%s", out)
	}
	if strings.Contains(out, "daemon started") {
		t.Errorf("a refused start must not also print a success line:\n%s", out)
	}
	if pids := wdSandboxPIDs(home); len(pids) != 0 {
		t.Errorf("a refused --require-scope start left %v behind", pids)
	}
}

// TestWorkerDaemon_RequireScopeOffSwitchIsADocumentedDegradation pins the
// explicit off-switch (GOPI_CGROUP_ESCAPE_OFF): disarming the escape must
// produce the SAME announced degradation as a host that cannot escape, never an
// error and never a silent success.
func TestWorkerDaemon_RequireScopeOffSwitchIsADocumentedDegradation(t *testing.T) {
	bin := wdBin(t)
	home := t.TempDir()
	settings := dcSettings(t, home)
	env := dcEnv(t, home, settings, cgroupscope.EnvOff+"=1")

	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong)
	if code != 0 {
		t.Fatalf("a disarmed escape must still leave a working (degraded) daemon, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "could not leave the login session scope") {
		t.Errorf("a disarmed escape must be announced, got:\n%s", out)
	}
	if !strings.Contains(out, cgroupscope.EnvOff) {
		t.Errorf("the announcement must name the switch that caused it (%s), got:\n%s", cgroupscope.EnvOff, out)
	}
	pid, _ := wdParseStart(t, out)
	t.Cleanup(func() { dcStopPID(t, pid) })
	if rel := dcCgroupOf(t, pid); !cgroupscope.InSessionScope(rel) {
		t.Errorf("with the escape disarmed the daemon must stay in its session scope, got %q", rel)
	}
	if mode := dcDaemonEnvValue(t, pid, daemonScopeModeEnv); mode != daemonScopeModeNone {
		t.Errorf("the daemon carries %s=%q, want %q", daemonScopeModeEnv, mode, daemonScopeModeNone)
	}
}

// TestWorkerDaemon_RequireScopeWithoutDaemonIsRefused is the seam-flag grammar
// guard: --require-scope belongs to --daemon, so the foreground face refuses it
// instead of silently dropping it (the same Rule 15 rule the worker's unknown
// argv words obey).
func TestWorkerDaemon_RequireScopeWithoutDaemonIsRefused(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the pin goes AFTER the binary is resolved (a relative
	// `go build -o bin .` runs in the package dir), and it keeps the CHILD from inheriting
	// this process's cwd — the live repository — whose own-worktree-release pass would
	// enumerate (and free) the developer's worktrees (see repo_context_sandbox_test.go).
	// (The invocation below is a connector/grammar refusal, and a refused invocation
	// resolves no repo set — but the gate judges SOURCE SHAPE and cannot see that, so
	// the site is pinned rather than argued: the pin costs a chdir and removes the
	// question.)
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := dcSettings(t, home)
	env := dcEnv(t, home, settings)

	code, out := wdRun(t, bin, env, CliVerbWorker, daemonScopeRequireFlag)
	if code == 0 {
		t.Fatalf("%s without %s must be refused, not silently dropped:\n%s", daemonScopeRequireFlag, daemonFlagLong, out)
	}
	if !strings.Contains(out, daemonScopeRequireFlag) || !strings.Contains(out, daemonFlagLong) {
		t.Errorf("the refusal must name both words, got:\n%s", out)
	}
}

// TestDaemonScopeDecide_ChosenMechanismMatchesTheHost is the DECISION's own
// truth check: daemonScopeDecide must never claim a mechanism that the host
// cannot deliver. When it reports systemd-scope, a real `systemd-run --user
// --scope` must work; when it reports cgroupfs-scope, the in-process escape
// must be Available.
func TestDaemonScopeDecide_ChosenMechanismMatchesTheHost(t *testing.T) {
	d := daemonScopeDecide(nil)
	switch d.mode {
	case daemonScopeModeSysd:
		if err := probeSystemdRun(); err != nil {
			t.Errorf("decide chose systemd-scope but the probe fails: %v", err)
		}
	case daemonScopeModeCgfs:
		if err := cgroupscope.Available(os.Getuid()); err != nil {
			t.Errorf("decide chose cgroupfs-scope but the escape is unavailable: %v", err)
		}
	case daemonScopeModeNone:
		if err := probeSystemdRun(); err == nil {
			t.Error("decide gave up on setsid-only although systemd-run works")
		}
		if err := cgroupscope.Available(os.Getuid()); err == nil && !cgroupscope.Off() {
			t.Error("decide gave up on setsid-only although the in-process escape is available")
		}
		if dcEscapeChosenByTheSeam() {
			t.Error("decide gave up on setsid-only although its OWN ladder escapes on this host (rung ①, systemd-run — the divergence this case exists to catch)")
		}
		if strings.TrimSpace(d.detail) == "" {
			t.Error("a degraded decision must carry the reason (Rule 15: never an unexplained downgrade)")
		}
	default:
		t.Fatalf("unknown scope mode %q", d.mode)
	}
}

// TestDaemonScopeWrap_OnlyWrapsTheSystemdPath pins the argv rewrite: mechanism
// ① replaces the command with systemd-run and stamps the unit name; the other
// two leave the command untouched — which is what keeps the worker's own entry
// point, its loop flags and its marker env byte-identical for the in-process
// escape (the argv shape the existing W1/W2/W6 cases assert on).
func TestDaemonScopeWrap_OnlyWrapsTheSystemdPath(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		wantWraps bool
	}{
		{daemonScopeModeSysd, true},
		{daemonScopeModeCgfs, false},
		{daemonScopeModeNone, false},
	} {
		orig := []string{daemonSpecArgv0(CliVerbWorker), CliVerbWorker, workerFlagOnce}
		cmd := &exec.Cmd{Path: "/opt/bin/gopi", Args: append([]string{}, orig...)}
		// The REAL executable, captured before the wrap: the wrap rewrites
		// cmd.Path itself (that is how systemd-run gets invoked), so reading it
		// afterwards would compare systemd-run against itself.
		realExe := cmd.Path
		d := daemonScopeDecision{mode: tc.mode, detail: "test"}
		daemonScopeWrap(cmd, CliVerbWorker, d)

		wrapped := cmd.Path == systemdRunPath
		if wrapped != tc.wantWraps {
			t.Errorf("mode %s: Path=%q (wrapped=%v), want wrapped=%v", tc.mode, cmd.Path, wrapped, tc.wantWraps)
		}
		if !tc.wantWraps {
			if len(cmd.Args) != len(orig) {
				t.Fatalf("mode %s rewrote argv %v, want it untouched", tc.mode, cmd.Args)
			}
			for i := range orig {
				if cmd.Args[i] != orig[i] {
					t.Errorf("mode %s: argv[%d]=%q, want %q", tc.mode, i, cmd.Args[i], orig[i])
				}
			}
		} else {
			// The wrapper must appear BEFORE the child argv (systemd-run takes
			// the command to run as its own trailing argv) and must name the
			// face's unit.
			if got := cmd.Args[0]; got != systemdRunPath {
				t.Errorf("wrapped argv[0]=%q, want %q", got, systemdRunPath)
			}
			joined := strings.Join(cmd.Args, " ")
			if !strings.Contains(joined, systemdRunFlagUser) || !strings.Contains(joined, systemdRunFlagScope) {
				t.Errorf("the wrapper must ask for a user scope, got %v", cmd.Args)
			}
			if !strings.Contains(joined, systemdRunFlagUnit+daemonFlagValueSep) {
				t.Errorf("systemd-run must name the unit the daemon escapes into, got %v", cmd.Args)
			}
			if want := systemdRunFlagUnit + daemonFlagValueSep + scopeUnitName(CliVerbWorker); !strings.Contains(joined, want) {
				t.Errorf("the wrapper must name the unit %q, got %v", want, cmd.Args)
			}
			// MERGE (fix/ledger-busy-dsn-contract-v2): the branch's variant
			// asserted the child words inside a `sh -c` command STRING (its
			// wrap built one) and read the wrapped argv through
			// daemonScopeQuote. master removed that shell entirely (cb536ada,
			// measured: the wrapper broke the daemon's ExtraFiles fd
			// numbering), so the wrap now APPENDS the child argv verbatim —
			// master's assertion is the one that matches production, and the
			// branch's quoting helper went with the mechanism it served.
			//
			// The tail is compared against the WRAPPED cmd.Path (the absolute
			// executable systemd-run is handed), not against orig[0] — orig[0]
			// is the stable argv[0] ("gopi-worker", see daemonArgv0Prefix),
			// which is deliberately NOT what the wrapper passes on: it is what
			// a detached daemon advertises in `ps`, and the whole reason
			// master's no-shell form exists is that systemd-run resolves a
			// COMMAND NAME on PATH and no `gopi-worker` exists there. Asserting
			// the tail against orig[0] asserted the pre-cb536ada shell
			// behaviour and had been failing on master ever since.
			joined = strings.Join(cmd.Args[len(cmd.Args)-len(orig):], " ")
			wantTail := append([]string{realExe}, orig[1:]...)
			if joined != strings.Join(wantTail, " ") {
				t.Errorf("the child argv must survive verbatim (after the real executable %q), got %v",
					realExe, cmd.Args)
			}
		}
		// The mechanism is stamped into the CHILD's environment in every mode:
		// the child measures its own /proc/self/cgroup from it, and an operator
		// can grep a live daemon's environ for the answer.
		found := false
		for _, kv := range cmd.Env {
			if kv == daemonScopeModeEnv+daemonFlagValueSep+tc.mode {
				found = true
			}
		}
		if !found {
			t.Errorf("mode %s: the child env carries no %s stamp (%v)", tc.mode, daemonScopeModeEnv, cmd.Env)
		}
	}
}

// TestDaemonSeamFlag_IsOwnedByTheWorkerFace pins the seam-flag predicate: the
// word is recognized by the shared predicate and it is NOT a loop flag of the
// face — so the re-exec builder cannot forward it to the child, which runs with
// the escape already applied.
//
// THE MERGER HALF IS GONE (epic daemon-machinery-removal, 2026-09-17): the
// merger has no daemon face and therefore no re-exec argv to keep the seam flag
// out of. `--daemon` on the merger is an unknown flag refused synchronously by
// the ordinary parser, which is pinned in status_action_conflict_e2e_test.go.
func TestDaemonSeamFlag_IsOwnedByTheWorkerFace(t *testing.T) {
	if !daemonSeamFlag(daemonScopeRequireFlag) {
		t.Errorf("%q must be a seam flag", daemonScopeRequireFlag)
	}
	if daemonSeamFlag("--daemon") || daemonSeamFlag("--once") {
		t.Error("ordinary face flags must not be classified as seam flags")
	}
	for _, f := range []string{daemonScopeRequireFlag, "--daemon"} {
		if _, bad := scanWorkerArgs([]string{f, workerFlagOnce}); bad != "" {
			t.Errorf("the worker scanner refused %q (bad=%q): the daemon vocabulary must cover the seam flag", f, bad)
		}
	}
	if got := workerDaemonLoopArgs([]string{daemonScopeRequireFlag, workerFlagOnce}); strings.Contains(strings.Join(got, " "), daemonScopeRequireFlag) {
		t.Errorf("the worker child argv must not carry the seam flag, got %v", got)
	}
}
