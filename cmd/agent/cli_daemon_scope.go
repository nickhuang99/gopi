package main

// cli_daemon_scope.go — the CGROUP ESCAPE of the shared daemonization seam.
//
// WHY (epic daemon-cgroup-detach, incidents 2026-09-12 04:12 and 2026-09-13
// 06:40): fork/exec-self + setsid makes a detached daemon a session leader with
// no controlling terminal, but it does NOT change its cgroup. The daemon kept
// living in /user.slice/user-1000.slice/session-N.scope, and when that session
// scope was torn down systemd SIGTERMed EVERY member — the daemon and its
// children with it. setsid is a real but PARTIAL protection (Linux clears the
// SIGHUP disposition at setsid, so a session teardown's hangup never reaches
// the daemon); cgroup membership is the part setsid never touched.
//
// WHAT THIS FILE ADDS, in one place for BOTH faces (the epic's "one mechanism,
// not copied per face"): the child is started inside a NEW scope unit, so
// /proc/<child pid>/cgroup no longer names the session scope.
//
// TWO MECHANISMS, ONE ORDER, MEASURED — NOT ASSUMED:
//
//  ① systemd-run --scope --user (the declarative path). systemd creates the
//     unit, so the daemon appears in `systemctl --user list-units` and dies
//     with the user manager instead of with the login session. Measured cost on
//     this host: ~36ms (devdoc/cgroup-limiting-not-cleanup-20260911.md). It
//     requires an ANSWERING user manager, which needs the user bus.
//  ② the in-process cgroupfs escape (internal/cgroupscope) when ① has no
//     manager to talk to. Zero dependency, zero bus: mkdir under the user
//     manager's delegated subtree + one write into cgroup.procs.
//
// Both are probed BEFORE anything is detached, and the choice is REPORTED in
// the parent's start note, so an operator reading the log knows which one ran
// and WHY (Rule 15: never a silent downgrade).
//
// WHEN NEITHER WORKS: the escape falls back to `setsid` ONLY (the historical
// behaviour) and the parent says so on stderr AND in the daemon's log, naming
// the reason. That is the documented degradation: the daemon is still detached
// from the terminal, still answers the START PROOF handshake, but it is still a
// member of the session scope and will die with it. It is announced, never
// silent, and `--require-scope` turns it into a hard, visible start failure for
// an operator who needs the guarantee rather than the convenience.
//
// WHAT IS DELIBERATELY NOT DONE HERE: no pattern-based process kill (the
// operator has live merger/worker daemons on this host); no change to the
// worker's drain-and-self-die contract, the merger's single-owner flock, or the
// --daemon/--once conflict check (all of them live in the faces, untouched).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/cgroupscope"
)

// --- named constants (PROJECT.md Rule 1: one place to find and change) ---

const (
	// daemonScopeUnitPrefix names the transient systemd scope unit a detached
	// daemon runs in: "gopi-daemon-<component>-<profile>", where <profile> is
	// the tag daemon_scope_profile.go derives from THIS profile's data root.
	//
	// THE NAME IS A FUNCTION OF THE PROFILE, NOT OF THE FACE AND NOT OF THE
	// LAUNCHER (2026-09-16, epic daemon-scope-per-profile). Two earlier
	// spellings were measured wrong, and each one cost a suite:
	//
	//  1. ONE NAME PER FACE ("gopi-daemon-<component>"), on the theory that
	//     systemd is "idempotent about an existing unit name" and a second
	//     start would reuse the running unit. IT IS NOT IDEMPOTENT: a transient
	//     scope name is a HANDLE, and while the unit is ALIVE systemd refuses a
	//     second one (measured on this host, 2026-09-16:
	//     `systemd-run --user --scope --unit=<a running unit> /bin/true` →
	//     "Failed to start transient scope unit: Unit <taken>.scope already
	//     exists.", and it keeps saying so on every attempt until the holder
	//     exits). So every LATER start of the same face died: a resident
	//     `gopi merger --daemon` (the normal state THEN — retired 2026-09-19) made every
	//     TEST-started merger pass the escape probe and then die at the start
	//     with EOF — each one burning the handshake budget, so the cmd/agent
	//     suite timed out at its own 10m panic instead of reporting a failure
	//     (measured: TWO concurrent full-suite runs, 600.192s and 600.226s,
	//     both `panic: test timed out` under TestUnboundDaemonSurvivesItsSpawner).
	//
	//  2. THE LAUNCHER'S PID as the suffix ("gopi-daemon-<component>-<pid>",
	//     the 2026-09-15 stopgap). It removed the collision against a resident
	//     daemon, but the uniqueness was in the WRONG DIMENSION: a profile is
	//     not a pid. `go test` runs EVERY case of one package inside ONE
	//     process, so every daemon that package starts asked for a name
	//     carrying the SAME pid — two cases that overlap in time, or a case
	//     that leaves a daemon behind (a leaked fixture), collided with each
	//     other instead of with the operator's daemon. And the operator cannot
	//     NAME the unit: it changes on every start.
	//
	// WHAT SYSTEMD ACTUALLY DOES (all measured on this host, 2026-09-16, one
	// controlled run each; the full table is in
	// devdoc/daemon-scope-per-profile.md):
	//
	//   - the name is taken EXACTLY while the unit is alive/loaded. A transient
	//     scope whose main process has just exited releases its name within
	//     ~10-20ms (a 3s scope: the name answered "already exists" until the
	//     process exited, then became recreatable on the first attempt;
	//     `systemd-run --user --scope --quiet /bin/true` — the shape of the
	//     probe below — costs 7.6ms);
	//   - stopping a unit frees its name, and a unit whose process exits leaves
	//     NOTHING loaded (LoadState=not-found, no --collect needed). Measured
	//     again on the migrated test suite's own units: `systemctl --user stop
	//     <unit>` → LoadState=not-found on the FIRST read, and the SAME name was
	//     re-creatable immediately after (exit 0), so the release is bounded by
	//     the stop call itself rather than by a timer;
	//   - `systemd-run --scope` stays in the foreground for the child's WHOLE
	//     LIFETIME and exits with the CHILD's status: it is the unit's
	//     "control" process, not the holder. Measured: a launched scope reports
	//     ControlPID/systemd-run's own pid as the unit's main pid (an EMPTY
	//     MainPID once that launcher is gone), the holder is the child (found in
	//     the unit's cgroup), and `systemd-run --scope -- /bin/true` exits 0
	//     while `-- /bin/false` exits 1. That is ALSO why the scope creation
	//     must not be held under a caller's command deadline: a 60s holder under
	//     a 10s context is killed by the context, and the creation is recorded
	//     as `signal: killed` — a start-shaped failure that is really a test
	//     harness bug (see daemon_scope_unit_test.go).
	//
	// So an existing name is a real, bounded condition — and the launcher now
	// DECIDES about it explicitly instead of discovering it in the child's log
	// (daemonScopeUnitExists / daemonScopeReuseNote): a start that finds THIS
	// profile's own live daemon in the unit reuses it, and a start that finds a
	// name held by anything else fails NAMING the unit (Rule 15).
	//
	// WHAT IS DELIBERATELY UNCHANGED: the singleton contract. The profile's
	// single owner is enforced by the FLOCK (the control fd), never by the unit
	// name — a second start of the same profile is refused by the lock, naming
	// the holder pid, long before the escape. The unit name carries the profile
	// so an operator can CORRELATE the two: `systemctl --user list-units` shows
	// one unit per profile, and the running daemon's unit is printed by the
	// start note (daemonScopeUnitEnv / daemonStartedNote).
	// THE VALUE COMES FROM THE PACKAGE THAT CLASSIFIES IT (cgroupscope
	// .EscapeTargetPrefix): the escape target's own name is what tells a cgroup
	// reader that a process which chose this mechanism has LEFT the login
	// session, so the producer and the classifier must not carry two spellings
	// that can drift (Rules 13/14).
	daemonScopeUnitPrefix = cgroupscope.EscapeTargetPrefix
	// daemonScopeUnitPidSep joins the component to THIS profile's tag, and —
	// only on the unresolvable-profile path — the unknown mark to the
	// launcher's pid: "gopi-daemon-merger-<profile tag>".
	daemonScopeUnitPidSep = daemonUnitPartSep
	// systemdRunPath is the systemd-run binary. Absolute on purpose: the escape
	// must not depend on the caller's PATH (a daemon started from a stripped
	// environment still has the mechanism it was told about).
	systemdRunPath = "/usr/bin/systemd-run"
	// systemdRunFlagScope / ...FlagUser / ...FlagUnit / ...FlagQuiet are the
	// EXACT systemd-run invocation of mechanism ① (one place to read it).
	systemdRunFlagScope = "--scope"
	systemdRunFlagUser  = "--user"
	systemdRunFlagUnit  = "--unit"
	systemdRunFlagQuiet = "--quiet"
	// systemdRunProbeBudget bounds the probe invocation (`systemd-run --user
	// --scope --quiet /bin/true`). A user manager that cannot be reached fails
	// in milliseconds; a wedged one must not hold the CALLER's prompt, which is
	// the whole point of the flag.
	systemdRunProbeBudget = 5 * time.Second
	// systemdRunStartBudget bounds the real `systemd-run --scope` START. It is
	// separate from daemonHandshakeWait on purpose: the handshake must still be
	// able to report the child's own failure line, which needs a live child.
	systemdRunStartBudget = 10 * time.Second
	// scopeFreeProcessName is the program the probe runs: a binary that exists
	// on every host and exits 0 immediately.
	scopeFreeProcessName = "/bin/true"
	// daemonScopeEnv values: WHICH mechanism got the child out of the session
	// scope. They are carried in the child's environment so the child can put
	// the fact in its own log line, and so an operator can grep a live daemon's
	// /proc/<pid>/environ for the answer.
	daemonScopeModeEnv   = "GOPI_DAEMON_SCOPE_MODE"
	daemonScopeModeSysd  = "systemd-scope"
	daemonScopeModeCgfs  = "cgroupfs-scope"
	daemonScopeModeNone  = "setsid-only"
	daemonScopeUnitEnv   = "GOPI_DAEMON_SCOPE_UNIT"
	daemonScopeDetailEnv = "GOPI_DAEMON_SCOPE_DETAIL"
	// daemonScopeProfileEnv carries THIS profile's canonical identity into the
	// child. It exists so the daemon can be ASKED which profile it serves: the
	// existing-unit decision (daemonScopeSameProfileDaemon) reads exactly this
	// variable out of the holder's /proc/<pid>/environ, and the comparison is
	// verbatim string equality — a path reconstructed from GOPI_HOME could not
	// name the DEFAULT profile at all (whose processes have no GOPI_HOME). One
	// spelling, written by every wrap and read by the decision (Rule 13).
	daemonScopeProfileEnv = "GOPI_DAEMON_PROFILE"
	// daemonScopeLogPrefix prefixes the child's scope line in the daemon's own
	// log. It is greppable and constant so a test asserts on it rather than on
	// prose.
	daemonScopeLogPrefix = "daemon: scope"
	// daemonScopeDegradedNote is the parent's line when the started daemon's
	// MEASURED cgroup is still a login session scope. It is a PLAIN EXPLANATION,
	// not a warning and not an error (epic "merger restart giving some
	// warning/error message", 2026-09-13): the daemon DID start, and the only
	// thing the operator needs is what this means and where the daemon sits. The
	// sentence "could not leave the login session scope" is kept verbatim —
	// cmd/agent/daemon_cgroup_e2e_test.go asserts on it — while the
	// error-shaped phrasing (WARNING … , WILL die …, a mechanism list) is gone:
	// on this host that degradation is the NORMAL, unavoidable outcome, and
	// printing it as a failure is what sent the operator looking for a bug.
	daemonScopeDegradedNote = "%s: note: could not leave the login session scope — this daemon runs in %s and will end with that scope.\n"
	// daemonScopeDegradedUnknownNote is the same note when the child's cgroup
	// could not be read at all. The MEASUREMENT is missing, so the pre-start
	// probe's verdict is named AS a verdict — it is never passed off as a
	// reading (the seam's rule: the measurement decides, the prediction never).
	daemonScopeDegradedUnknownNote = "%s: note: could not leave the login session scope — the daemon's cgroup could not be read (%v); this is the pre-start probe: %s\n"
	// daemonScopeRequiredNote is the hard failure under --require-scope.
	daemonScopeRequiredNote = "%s: refusing to start: %s requested but the cgroup escape is unavailable: %v\n"
	// daemonScopeNoPrivilegeNote answers the reader's most natural next
	// question about a degraded start — "is this a privilege problem?" — with
	// the measured answer, and NOTHING else. It is printed BESIDE the note
	// (daemonScopeMeasureLines), never inside it: the note stays the one-line
	// measurement the previous epic pinned (it is read on every restart).
	//
	// WHY THIS TEXT EXISTS (operator report, 2026-09-14): reading the degraded
	// line, the operator asked for passwordless sudo for worker and merger — a
	// remedy that is BOTH unnecessary and ineffective, and one that would hand
	// the daemon's whole process tree to root for no gain (dead end, measured):
	//
	//   - a daemon started under `sudo` was measured in the cgroup of the
	//     UNPRIVILEGED shell that launched it
	//     (/user.slice/user-1000.slice/session-18855.scope) — a process
	//     inherits its cgroup from its LAUNCHER, so changing the uid changes
	//     nothing about the membership;
	//   - root has no user manager of its own to migrate into:
	//     /sys/fs/cgroup/user.slice/user-0.slice/user@0.service does not exist;
	//   - and no unprivileged migration can work here either, because the
	//     kernel gates a cgroup.procs write on the subtree the source and the
	//     target share (/sys/fs/cgroup/user.slice/user-1000.slice is
	//     root:root 0755 — `mkdir` in the delegated subtree succeeds, the
	//     migration does not).
	//
	// So privilege is not the missing ingredient, and the honest thing to say
	// is what the daemon asks for: NOTHING. A daemon runs unprivileged; if the
	// escape fails, that is a property of the host, and it is announced (Rule
	// 15: never a silent downgrade) WITHOUT inventing a remedy that does not
	// work. The sentence names the privilege only to deny it — the shape the
	// regression gate (daemon_scope_no_privilege_test.go) enforces.
	daemonScopeNoPrivilegeNote = "%s: that is not a privilege problem: no privilege is needed to run this daemon, " +
		"and none obtains the escape (measured) — the start stays unprivileged.\n"
	// daemonScopeChosenNote reports which mechanism the child will use.
	daemonScopeChosenNote = "%s: cgroup escape: %s\n"
	// daemonScopeNoneDetailFmt joins the two mechanisms' reasons into ONE line.
	// Both are named on purpose (the fix for a missing user bus is not the fix
	// for an undelegated cgroup subtree), but a multi-line systemd-run failure
	// would turn a one-line note into a wall of text on the operator's prompt —
	// so each reason contributes its FIRST line only (scopeNoneDetail).
	daemonScopeNoneDetailFmt = "%s; in-process escape: %s"
)

// daemonScopeRequireFlag makes the cgroup escape MANDATORY: a detach that
// cannot leave the session scope fails loudly instead of degrading. Both faces'
// parsers accept it (daemonScopeFlags below) and the shared seam consumes it
// from the parent argv, so it can never reach the child.
const daemonScopeRequireFlag = "--require-scope"

// daemonScopeFlags are the seam's own flags: the words every face's scanner
// must accept, and the words the re-exec builders must NOT forward to the child
// (the child runs with the escape already applied). One list, so a new seam
// flag cannot be forgotten in a face's scanner.
var daemonScopeFlags = []string{daemonScopeRequireFlag}

// daemonSeamFlag reports whether a word belongs to the daemon SEAM rather than
// to a face's loop flags. ONE predicate: the worker's scanner and the merger's
// dispatcher both ask it, so a seam flag can never be accepted in one face and
// refused in the other.
func daemonSeamFlag(word string) bool {
	for _, f := range daemonScopeFlags {
		if word == f {
			return true
		}
	}
	return false
}

// daemonScopeDecision is what the pre-detach probe concluded: which escape the
// child will use, and the reason when none is available.
type daemonScopeDecision struct {
	// mode is one of daemonScopeModeSysd / daemonScopeModeCgfs /
	// daemonScopeModeNone.
	mode string
	// detail explains the decision for the start note and the log (the failing
	// command's output, the in-process probe's error, …). Never empty for a
	// degraded decision.
	detail string
	// required is true when --require-scope was passed: an unavailable escape
	// is then a hard start failure.
	required bool
	// userBus is the measured host state behind a FAILED rung ① (epic user-bus
	// diagnosis, 2026-09-15). It is the zero value when rung ① worked, when it
	// failed for a reason that is not a bus failure, or when the diagnosis could
	// not be taken — and the zero value prints nothing (the seam must never
	// invent a state it did not measure, Rule 15).
	userBus userBusDiagnosis
}

// scopeEscaped reports whether the decision actually leaves the session scope.
func (d daemonScopeDecision) scopeEscaped() bool {
	return d.mode == daemonScopeModeSysd || d.mode == daemonScopeModeCgfs
}

// daemonScopeRequested reports whether argv asked for a mandatory escape. ONE
// predicate for both faces (the same discipline as daemonFlagRequested).
func daemonScopeRequested(args []string) bool {
	for _, a := range args {
		if a == daemonScopeRequireFlag {
			return true
		}
	}
	return false
}

// daemonScopeDecide probes the two mechanisms in order and returns the
// decision. It runs in the PARENT, before anything is detached, because its
// whole purpose is to be able to FAIL the start — a probe after the fork could
// only report into a log nobody has been told to read yet.
//
// The probe is deliberately a REAL run, not a config inspection: "the user bus
// socket exists" and "systemd-run works" are different facts on this host
// (measured), and only the second one matters.
func daemonScopeDecide(args []string) daemonScopeDecision {
	d := daemonScopeDecision{required: daemonScopeRequested(args)}
	if cgroupscope.Off() {
		d.mode = daemonScopeModeNone
		d.detail = cgroupscope.EnvOff + " is set"
		return d
	}
	sysdErr := probeSystemdRun()
	if sysdErr == nil {
		d.mode = daemonScopeModeSysd
		d.detail = systemdRunPath
		return d
	}
	cgErr := cgroupscope.Available(os.Getuid())
	if cgErr == nil {
		d.mode = daemonScopeModeCgfs
		d.detail = fmt.Sprintf("%s (uid %d)", cgroupscope.Root, os.Getuid())
		return d
	}
	// Neither mechanism works. Both reasons are reported: the operator's fix is
	// different for a missing user bus than for an undelegated cgroup subtree,
	// and a message that named only one of them would send them to the wrong
	// place.
	//
	// The bus reason is ALSO diagnosed (epic user-bus diagnosis, 2026-09-15):
	// the blended sentence's first half — "Failed to connect to bus: No such
	// file or directory" — is a symptom three DIFFERENT host states produce, and
	// the measured state (no manager / manager alive with the socket path
	// unlinked / a stale node) is what tells the operator which repair applies.
	// The diagnosis is taken HERE, once, at the decision, and carried in the
	// decision so every print site renders the same measured state.
	d.mode = daemonScopeModeNone
	d.detail = scopeNoneDetail(sysdErr, cgErr)
	d.userBus = daemonUserBusDiagnosis(sysdErr)
	return d
}

// scopeNoneDetail renders the degraded decision's reason as ONE line: each
// mechanism's first line, joined (daemonScopeNoneDetailFmt).
//
// WHY the first line and not the whole error (epic "merger restart giving some
// warning/error message", 2026-09-13): the degraded case is the NORMAL outcome
// on this host, so its reason is read every single time the operator restarts
// a daemon. A systemd-run failure can carry a multi-line message, and pasting it
// into the note turns a one-line note into a wall the operator learns to skip.
// The first line is the actionable one ("Failed to connect to bus: No such file
// or directory"); the full text stays available in the daemon's own log through
// daemonScopeDetailEnv.
func scopeNoneDetail(sysdErr, cgErr error) string {
	return fmt.Sprintf(daemonScopeNoneDetailFmt, firstLine(sysdErr.Error()), firstLine(cgErr.Error()))
}

// firstLine returns s's first non-empty line, trimmed. A single-line s is
// returned unchanged, so the helper is a no-op for every reason that already is
// one line.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return strings.TrimSpace(s)
}

// probeSystemdRun reports whether `systemd-run --user --scope` can actually
// create a scope here. The probe is a REAL scope creation of /bin/true: it is
// cheap (~36ms measured), it cannot disturb anything, and it fails exactly when
// the real start would fail.
func probeSystemdRun() error {
	if _, err := os.Stat(systemdRunPath); err != nil {
		return fmt.Errorf("no %s", systemdRunPath)
	}
	ctx, cancel := context.WithTimeout(context.Background(), systemdRunProbeBudget)
	defer cancel()
	cmd := exec.CommandContext(ctx, systemdRunPath,
		systemdRunFlagUser, systemdRunFlagScope, systemdRunFlagQuiet, scopeFreeProcessName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

// scopeUnitName is the transient unit name for a component
// ("gopi-daemon-merger-<profile tag>"). It is PER-PROFILE — see
// daemonScopeUnitPrefix for the two earlier spellings this replaced and the
// measurements behind each — so the name is stable for a profile (an operator
// can name it, and a second start of the same profile is the FLOCK's refusal,
// not a systemd name clash).
func scopeUnitName(component string) string {
	return scopeUnitNameFor(component, daemonProfileUnitTagNow())
}

// scopeUnitNameFor is scopeUnitName with the PROFILE TAG as a PARAMETER — the
// pure half, so the uniqueness property is testable in one process (where the
// profile cannot vary). Consumers use scopeUnitName.
func scopeUnitNameFor(component, profileTag string) string {
	return daemonScopeUnitPrefix + component + daemonScopeUnitPidSep + profileTag
}

// scopeDirName is the cgroupfs scope directory name for a component — the SAME
// name as the unit, so the two mechanisms produce one visible node. It carries
// the same per-profile identity as scopeUnitName for the same reason: the
// in-process escape creates the directory ITSELF, and cgroupscope.Enter
// refuses (or silently joins) an EXISTING directory, which would put a second
// daemon of the same profile in the first one's cgroup instead of its own.
//
// It is literally scopeUnitName's value (no format string, no second suffix
// expression): the two names must never drift, and one definition is what makes
// that a fact rather than a convention (Rule 13/14).
func scopeDirName(component string) string {
	return scopeUnitName(component)
}

// --- the EXPLICIT decision about an already-taken unit name ---------------
//
// Measured behaviour (2026-09-16, see daemonScopeUnitPrefix): systemd refuses a
// transient scope whose name is taken, and the name is taken exactly while the
// unit is ALIVE — a scope whose process has exited releases it within ~10-20ms
// and leaves nothing loaded. An existing name is therefore a bounded, real
// condition, and it is DECIDED here, in the parent, before anything is
// detached:
//
//   - THIS profile's own daemon is the holder → the start is a no-op: the unit
//     IS the requested end state, and it is reported (daemonScopeReuseNote)
//     rather than hidden behind a retry;
//   - anything else holds it (a daemon of ANOTHER profile whose name collided,
//     a foreign scope, a unit that is still tearing down) → the start FAILS,
//     naming the unit and the reason (Rule 15). It is never a retry: a name
//     held by a LIVE foreign unit does not free itself, so a retry would only
//     convert a named refusal into a handshake timeout.
//
// The probe runs only when the child would actually be wrapped: mechanism ②
// and the degraded setsid-only path never ask systemd for a unit name.

// daemonUnitExistsNote is the parent's refusal when the unit name is already
// taken. It names the unit (so the operator can `systemctl --user status <unit>`
// it) and points at the profile resolution that produced the name (so the
// collision is diagnosable without re-reading the daemon's log).
const daemonUnitExistsNote = "%s: refusing to start: the transient scope unit %s is already taken " +
	"(for profile %s) — this is not a retryable error: a LIVE unit holds its name until it exits\n"

// daemonUnitQueryFailedNote is the parent's refusal when the existing-unit
// question itself could not be answered. It never degrades to "assume free":
// the start is refused with the question's own failure named (Rule 15).
const daemonUnitQueryFailedNote = "%s: refusing to start: could not check whether the scope unit %s is free: %w\n"

// daemonScopeReuseNote is the parent's report when THIS profile's own daemon
// already holds the unit: the requested end state holds, so no second daemon is
// started and nothing is detached.
const daemonScopeReuseNote = "%s: the %s daemon for this profile is already running in the scope unit %s " +
	"(pid %d) — reusing it, no second daemon started\n"

// daemonScopeReuseDetailFmt is the reuse detail, so the reuse travels the same
// path as the other start details (a test can assert it without capturing
// stdout, and the wording has one definition).
const daemonScopeReuseDetailFmt = "reused the live scope unit %s (pid %d)"

// daemonScopeUnitExists asks systemd whether the unit name is ALREADY loaded
// ("already exists" is what this question decides, never "is something
// running": a loaded unit still tearing down holds its name too, and that
// distinction is exactly what the caller must act on).
//
// The answer's UNAVAILABILITY is an error, distinct from "does not exist": a
// check that cannot be asked must never read as "free" (Rule 15) — the start is
// then REFUSED with the question's own failure named, rather than handed to
// systemd-run to fail with a message about a unit nobody correlated.
func daemonScopeUnitExists(unitName string) (bool, error) {
	state, err := daemonScopeUnitLoadState(unitName)
	if err != nil {
		return false, err
	}
	return state != daemonLoadStateNotFound, nil
}

// daemonScopeUnitLoadState asks systemd for ONE unit's load state — the word
// that answers "is this name taken?" exactly (daemonScopeUnitExists).
//
// EVERY failure is an error: an unanswerable question and an unknown unit are
// different facts, and reading the first as "the name is free" would hand the
// start to systemd-run to fail with a message nobody correlated (Rule 15).
//
// THE `--user` FLAG IS LOAD-BEARING (measured 2026-09-16, and the defect that
// made every earlier version of this decision INERT): the daemon's unit is a
// TRANSIENT USER scope, so only the USER manager knows it. Without `--user` the
// same argv asks the SYSTEM manager, which answers "not-found" for EVERY unit
// this code asks about — a live user scope included. Measured on this host with
// a live `gopi-daemon-worker-…scope` (systemd 249): `systemctl show
// --property=LoadState --value <unit>.scope` → "not-found", while `systemctl
// --user show --property=LoadState --value <unit>.scope` → "loaded" and
// `--user show --property=ControlGroup` → the unit's real cgroup. The omission
// was invisible because "not-found" reads as the perfectly ordinary "the name is
// free": the start proceeded, discovered the taken name only inside systemd-run,
// and the reuse half could never fire.
//
// The bus is therefore named through the SAME constant the mechanism uses
// (systemdRunFlagUser), never a second spelling of "--user".
func daemonScopeUnitLoadState(unitName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlQueryBudget)
	defer cancel()
	out, err := exec.CommandContext(ctx, systemctlPath, systemdRunFlagUser, systemctlShowArg,
		systemctlPropertyFlag, systemctlPropertyLoadState, systemctlValueFlag,
		unitName+daemonUnitFileSuffix).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ask the user manager about %s: %s", unitName, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// daemonScopeResolveExistingUnit is the EXPLICIT existing-unit decision, run by
// the parent AFTER the wrap and BEFORE the start. It returns either
//
//	(reused=false, "", nil)      — no unit holds the name: start normally;
//	(reused=true,  detail, nil)  — THIS profile's own live daemon holds it: the
//	                               requested end state holds, so this start is
//	                               a reported NO-OP (nothing is detached);
//	(false, "", err)             — refused, naming the unit and why (Rule 15).
//
// It asks NOTHING when the child will not be wrapped (mechanism ② and the
// degraded path never ask systemd for a name).
//
// THE REUSE CASE IS DEFENCE IN DEPTH, AND IT IS SAID SO RATHER THAN ASSUMED: a
// second start of a profile whose daemon is alive is refused EARLIER, by the
// FLOCK, and the unit is held by the same process that holds the lock — so a
// start that reaches this function with a free lock finds no live daemon of
// this profile holding its unit. The branch exists because "the lock and the
// unit are always held by the same process" is the kind of invariant that is
// true until a daemon dies between the lock probe and the unit probe, and the
// alternative to deciding explicitly is discovering it as a handshake timeout.
func daemonScopeResolveExistingUnit(spec daemonSpec, component string, d daemonScopeDecision) (bool, string, error) {
	if d.mode != daemonScopeModeSysd {
		return false, "", nil
	}
	unitName := scopeUnitName(component)
	exists, err := daemonScopeUnitExists(unitName)
	if err != nil {
		return false, "", fmt.Errorf(daemonUnitQueryFailedNote, spec.prog, unitName, err)
	}
	if !exists {
		return false, "", nil
	}
	if pid, ok := daemonScopeSameProfileDaemon(unitName); ok {
		return true, daemonScopeReuseReport(spec, unitName, pid), nil
	}
	return false, "", fmt.Errorf(daemonUnitExistsNote, spec.prog, unitName, daemonProfileUnitTagNow())
}

// daemonScopeReuseReport reports the reuse decision to the operator (stdout,
// beside the other start notes) and returns the detail for the child's
// environment.
func daemonScopeReuseReport(spec daemonSpec, unitName string, pid int) string {
	fmt.Fprintf(os.Stdout, daemonScopeReuseNote, spec.prog, spec.component, unitName, pid)
	return fmt.Sprintf(daemonScopeReuseDetailFmt, unitName, pid)
}

// daemonScopeSameProfileDaemon reports the pid of the daemon ALREADY RUNNING in
// unitName for THIS profile, or ok=false when the holder is not this profile's
// own daemon.
//
// WHO HOLDS A SCOPE IS ANSWERED BY THE CGROUP, NOT BY systemd (measured on this
// host, epic daemon-scope-per-profile, 2026-09-16): systemd 249 reports NO pid
// for a transient SCOPE — `systemctl --user show --property=MainPID --value
// <scope>` is EMPTY for a live scope, and `--property=PIDs` is empty too, while
// `--property=ControlGroup` names the unit and one `cat <cgroup>/cgroup.procs`
// returns the daemon's pid (measured: 18ms, empty stdout from MainPID on the
// very unit that same cgroup proved alive). A MainPID-based reuse therefore
// silently never fired — the reuse was DEAD CODE that could only ever answer
// "not ours" (and the two tests pinned to it skipped instead of failing, which
// is how it stayed invisible).
//
// The profile identity is still the daemon's OWN statement: every launcher
// stamps daemonScopeProfileEnv into the child's environment (daemonScopeWrap),
// so the holder's /proc/<pid>/environ carries the identity VERBATIM — exact
// string equality, no path reconstruction, and it works for the DEFAULT profile
// too (whose processes have no GOPI_HOME to read back at all). A holder whose
// environment cannot be read is NOT claimed as ours: an unreadable answer is not
// a "yes" (Rule 15), and claiming a foreign daemon as one's own would silently
// turn this start into a no-op.
func daemonScopeSameProfileDaemon(unitName string) (int, bool) {
	pids, err := daemonUnitHolderPids(unitName)
	if err != nil {
		return 0, false
	}
	identity, ok := daemonProfileIdentity()
	if !ok {
		return 0, false
	}
	for _, pid := range pids {
		holder, err := daemonProcessEnvVar(pid, daemonScopeProfileEnv)
		if err != nil || holder == "" {
			continue
		}
		if holder == identity {
			return pid, true
		}
	}
	return 0, false
}

// daemonProcessComm returns a live process's command name from procfs
// (`/proc/<pid>/comm`, trimmed), or "" when it cannot be read. It is EVIDENCE,
// never a decision: callers that must act on the holder's identity use the
// daemon's own environment stamp (daemonProcessEnvVar), and this value only
// answers "which process is this really" in a diagnosis or a test.
func daemonProcessComm(pid int) string {
	//stylegate:ignore procfs read of a pid systemd named, used as evidence in a message; unreadable returns ""
	blob, err := os.ReadFile(fmt.Sprintf(procCommPathFmt, pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(blob))
}

// daemonUnitHolderPids returns the pids systemd has placed INSIDE unitName's
// cgroup — the processes that HOLD the unit's name, which is what the
// existing-unit decision must identify. The kernel's own order is preserved
// (cgroup.procs lists the earliest-created process first), so the daemon
// systemd started for the unit is pids[0] for a healthy unit.
//
// An unanswerable question is an ERROR (Rule 15) — including a unit whose
// cgroup holds no process: "I could not find the holder" must not read as "the
// holder is not ours".
func daemonUnitHolderPids(unitName string) ([]int, error) {
	dir, err := daemonUnitScopeCgroup(unitName)
	if err != nil {
		return nil, err
	}
	//stylegate:ignore cgroup read of the unit systemd itself named; an unreadable member list is returned as an error, never as "no holder present"
	blob, err := os.ReadFile(filepath.Join(dir, cgroupscope.ProcsFileName))
	if err != nil {
		return nil, fmt.Errorf("read the cgroup of %s: %w", unitName, err)
	}
	pids := make([]int, 0, len(blob)/daemonPidDecimalBytes)
	for _, field := range strings.Fields(string(blob)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 {
			continue
		}
		pids = append(pids, pid)
	}
	if len(pids) == 0 {
		return nil, fmt.Errorf("the cgroup of %s has no member process", unitName)
	}
	return pids, nil
}

// daemonScopeUnitMainPIDText returns systemd's RAW answer for a unit's MainPID
// property, trimmed — including the EMPTY string this host's systemd reports for
// a transient SCOPE (the measurement the cgroup-based holder read exists
// because of). It is DIAGNOSTIC evidence, not a decision: a caller that needs
// the holder uses daemonUnitHolderPids.
func daemonScopeUnitMainPIDText(unitName string) string {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlQueryBudget)
	defer cancel()
	out, err := exec.CommandContext(ctx, systemctlPath, systemdRunFlagUser, systemctlShowArg,
		systemctlPropertyFlag, systemctlPropertyMainPID, systemctlValueFlag,
		unitName+daemonUnitFileSuffix).Output()
	if err != nil {
		return fmt.Sprintf("(unreadable: %v)", err)
	}
	return strings.TrimSpace(string(out))
}

// daemonUnitScopeCgroup returns the ABSOLUTE cgroup directory of unitName, as
// RELATIVE to the cgroup mount point, so the mount point is prepended from the
// cgroup package's ONE definition of it rather than spelled again here.
func daemonUnitScopeCgroup(unitName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlQueryBudget)
	defer cancel()
	out, err := exec.CommandContext(ctx, systemctlPath, systemdRunFlagUser, systemctlShowArg,
		systemctlPropertyFlag, systemctlPropertyControlGroup, systemctlValueFlag,
		unitName+daemonUnitFileSuffix).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ask the user manager for the cgroup of %s: %s", unitName, strings.TrimSpace(string(out)))
	}
	rel := strings.TrimSpace(string(out))
	if rel == "" {
		return "", fmt.Errorf("systemd reports no cgroup for %s", unitName)
	}
	return filepath.Join(cgroupscope.Root, rel), nil
}

// daemonProcessEnvVar reads ONE environment variable out of a live process's
// procfs entry. An unreadable entry is an ERROR, never an empty value.
func daemonProcessEnvVar(pid int, name string) (string, error) {
	//stylegate:ignore procfs read of a pid systemd named; unreadable is returned as an error, never as an empty value
	blob, err := os.ReadFile(fmt.Sprintf(procEnvPathFmt, pid))
	if err != nil {
		return "", err
	}
	for _, entry := range strings.Split(string(blob), "\x00") {
		if after, found := strings.CutPrefix(entry, name+"="); found {
			return after, nil
		}
	}
	return "", nil
}

// daemonScopeWrap rewrites the child command according to the decision:
//
//	systemd-scope  → argv becomes `systemd-run --user --scope --unit=<name>
//	                 <the child argv>`; systemd forks the child into a fresh
//	                 scope unit and systemd-run waits for it, so the parent's
//	                 handshake read still works exactly as before.
//	cgroupfs-scope → argv unchanged; the child migrates ITSELF before it
//	                 answers the handshake (daemonScopeEscapeSelf below).
//	setsid-only    → argv unchanged, and the child reports the degraded state.
//
// It also stamps the mode into the child's environment so the CHILD can log
// what actually happened to it (the parent's probe is a prediction; the child's
// /proc/self/cgroup is the measurement).
func daemonScopeWrap(cmd *exec.Cmd, component string, d daemonScopeDecision) {
	identity, _ := daemonProfileIdentity()
	cmd.Env = append(cmd.Env,
		daemonScopeModeEnv+"="+d.mode,
		daemonScopeUnitEnv+"="+scopeUnitName(component),
		daemonScopeDetailEnv+"="+d.detail,
		// The profile is stamped UNCONDITIONALLY (every mode, not just the
		// systemd one): it is what lets a later start recognise THIS profile's
		// own daemon as the holder of its unit (daemonScopeSameProfileDaemon),
		// and the identity must be the child's own statement, not something a
		// reader reconstructs from GOPI_HOME (the default profile has none).
		daemonScopeProfileEnv+"="+identity,
	)
	if d.mode != daemonScopeModeSysd {
		return
	}
	// WHAT systemd-run IS HANDED (2026-09-15 fix, the daemon would not start):
	// cmd.Args[0] is the STABLE component name ("gopi-worker"/"gopi-merger",
	// see daemonArgv0Prefix) — deliberately not the parent's argv[0], so `ps`
	// can name a detached daemon. But systemd-run resolves ITS first
	// non-flag argument as a COMMAND NAME on PATH, and no `gopi-worker`
	// exists there (the binary is `gopi`) — so the old form, which pasted
	// cmd.Args verbatim after the flags, failed every start with:
	//
	//     Failed to find executable gopi-worker: No such file or directory
	//
	// (measured: `systemd-run --user --scope --quiet ... gopi-worker worker`
	// reproduces it exactly).
	//
	// NO SHELL. A `sh -c 'exec -a <name> <path> …'` wrapper was tried and
	// rejected: it breaks the descriptors the daemon is handed — the
	// ExtraFiles numbering (control lock fd 3 / handshake fd 4, see
	// daemonControlChildFD) and the suite's daemon-lifetime binding fd
	// (internal/daemonlifetime requires the read end to reach the daemon) —
	// measured as the whole worker-daemon e2e set failing with "daemon
	// exited during startup without reporting: EOF" while the same command
	// succeeded by hand.
	//
	// So systemd-run execs the REAL absolute executable directly. The
	// stable face identity is carried by the UNIT NAME (scopeUnitName, which
	// is what `systemctl --user status` and the cgroup path expose) rather
	// than by argv[0] — an argv[0] that `ps` would only ever show alongside
	// its real path anyway.
	//
	// MERGE NOTE (fix/ledger-busy-dsn-contract-v2): this branch's variant of
	// the wrap assembled `sh -c 'exec -a <stable> <abspath> …'` through
	// daemonScopeQuote (one quoted command STRING). master replaced that with
	// the NO-SHELL form above, measured (the wrapper broke the daemon's
	// ExtraFiles fd numbering), so daemonScopeQuote and its consts are DROPPED
	// here along with the mechanism they served — the branch's own test now
	// asserts the appended child argv instead of the command string.
	argv := []string{
		systemdRunPath,
		systemdRunFlagUser,
		systemdRunFlagScope,
		systemdRunFlagUnit + daemonFlagValueSep + scopeUnitName(component),
		// --quiet keeps systemd-run's own "Running scope as unit …" line out of
		// the daemon's log: the log is the DAEMON's record, and the parent
		// already reports the mechanism it chose.
		systemdRunFlagQuiet,
		cmd.Path,
	}
	argv = append(argv, cmd.Args[1:]...)
	cmd.Path = systemdRunPath
	cmd.Args = argv
}

// daemonScopeChildEscape is the CHILD's half of the in-process escape: it
// migrates itself into its own cgroup and VERIFIES it, before the handshake
// byte is written (so "started" always means "started in the scope we
// promised"). A failure is reported into the daemon's log and does NOT fail the
// start — the daemon is alive either way, and the epic's rule is a DOCUMENTED
// degradation rather than a dead daemon nobody can debug.
//
// A child started by `systemd-run --scope` returns immediately: systemd already
// created and applied the unit, and re-migrating would move the daemon OUT of
// the unit a `systemctl --user stop` would target.
func daemonScopeChildEscape(prog, component string) {
	mode := os.Getenv(daemonScopeModeEnv)
	// The self-probe is the measurement, whatever the parent predicted: read
	// /proc/self/cgroup and say what it says.
	rel, relErr := cgroupscope.SelfRel()
	unit := os.Getenv(daemonScopeUnitEnv)
	if mode == daemonScopeModeCgfs {
		name := scopeDirName(component)
		sc, err := cgroupscope.Enter(os.Getuid(), name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s escape failed: %v\n", prog, daemonScopeLogPrefix, err)
			rel, relErr = cgroupscope.SelfRel()
		} else {
			fmt.Fprintf(os.Stderr, "%s: %s %s unit=%s cgroup=%s\n", prog, daemonScopeLogPrefix,
				daemonScopeModeCgfs, sc.Rel, sc.Dir)
			rel, relErr = sc.Rel, nil
		}
	}
	if relErr != nil {
		fmt.Fprintf(os.Stderr, "%s: %s could not read /proc/self/cgroup: %v\n", prog, daemonScopeLogPrefix, relErr)
		return
	}
	if cgroupscope.InSessionScope(rel) {
		fmt.Fprintf(os.Stderr,
			"%s: %s DEGRADED: still in a login session scope (%s) — this process dies with that scope\n",
			prog, daemonScopeLogPrefix, rel)
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %s ok unit=%s cgroup=%s\n", prog, daemonScopeLogPrefix, unit, rel)
}

// daemonScopeMeasureLine renders the post-start note from the child's MEASURED
// cgroup, or "" when there is nothing to say. It is a pure function so the
// wording is testable without capturing os.Stdout — and so the ONE place that
// decides what an operator reads is a function, not a print site.
//
// THE MEASUREMENT DECIDES, IN BOTH DIRECTIONS: a daemon that the probe expected
// to fail the escape but that is measured OUTSIDE a session scope gets no note
// at all (its own log line names the mechanism), and a note is emitted only
// when /proc/<pid>/cgroup really is a login session scope. The prediction is
// named ONLY when the measurement itself is unavailable — and then it is
// labelled as the probe it is.
func daemonScopeMeasureLine(spec daemonSpec, pid int, scope daemonScopeDecision) string {
	rel, err := cgroupscope.RelOfPID(pid)
	return daemonScopeNoteFor(spec.prog, rel, err, scope)
}

// daemonScopeNoteFor is daemonScopeMeasureLine's PURE half: it renders the note
// from an ALREADY MEASURED cgroup-relative path (or the measurement's error),
// so the wording an operator reads is testable on synthesized inputs instead of
// depending on this host's cgroup layout. The three outcomes are exactly:
//
//   - the process is outside any login session scope → "" (nothing to report:
//     a successful escape needs no note, its mechanism is named before start),
//   - the process IS in a login session scope → the plain note, naming the real
//     cgroup path it was measured in,
//   - the cgroup could not be read → the pre-start probe's verdict, LABELLED as
//     the prediction it is (never presented as a reading).
func daemonScopeNoteFor(prog, rel string, relErr error, scope daemonScopeDecision) string {
	if relErr != nil {
		if scope.scopeEscaped() {
			return ""
		}
		return fmt.Sprintf(daemonScopeDegradedUnknownNote, prog, relErr, scope.detail)
	}
	if !cgroupscope.InSessionScope(rel) {
		return ""
	}
	return fmt.Sprintf(daemonScopeDegradedNote, prog, rel)
}

// daemonScopeMeasureNote prints daemonScopeMeasureLine once the start verdict is
// positive: it names what the CHILD's cgroup actually is (read live from
// procfs), never what the pre-start probe predicted.
// It also prints the no-privilege correction beside the note
// (daemonScopeNoPrivilegeNote): the note says where the daemon really lives,
// the correction answers the question that reading it raises ("so I need
// sudo?") with the measured answer. Silence there is what let the operator
// arrive at a remedy that does not work and would cost them the whole process
// tree to root (Rule 15: a degradation the operator can act on must not be
// reported without the action — and here the action is "nothing, and do not
// take a privilege").
func daemonScopeMeasureNote(spec daemonSpec, pid int, scope daemonScopeDecision) {
	for _, line := range daemonScopeMeasureLines(spec, pid, scope) {
		fmt.Fprint(os.Stdout, line)
	}
}

// daemonScopeMeasureLines is the ONE place that decides what an operator reads
// about a started daemon's cgroup: the measured note (or nothing, when there is
// nothing to report) plus, for a degraded start, the no-privilege correction.
// It returns the lines instead of printing them so the wording is testable
// without capturing os.Stdout, and so no print site can grow a second opinion.
//
// The correction is emitted for the SAME cases as the note and no others: it is
// printed only when a note is (a degraded, MEASURED start), because a
// successful escape has no missing ingredient to explain away — answering
// "no privilege needed" for a daemon that is already sitting outside the
// session scope would invent a problem the measurement just disproved.
func daemonScopeMeasureLines(spec daemonSpec, pid int, scope daemonScopeDecision) []string {
	line := daemonScopeMeasureLine(spec, pid, scope)
	if line == "" {
		return nil
	}
	return []string{line, fmt.Sprintf(daemonScopeNoPrivilegeNote, spec.prog)}
}

// daemonScopeAbandon tells the child to stop after a FAILED START. It is the
// seam's explicit-pid cleanup (never a pattern, never a group: a cgroup-scoped
// child may not be the leader of any group we own), factored out of
// daemonSpawn so both the handshake failure and the --require-scope refusal
// share one path.
func daemonScopeAbandon(child *exec.Cmd, prog string) {
	if child.Process == nil {
		return
	}
	signalChildOnly(child.Process.Pid, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case <-done:
	case <-time.After(daemonExitGrace):
		fmt.Fprintf(os.Stderr, "%s: daemon pid %d did not stop within %s\n", prog, child.Process.Pid, daemonExitGrace)
	}
}

// daemonScopeModeLabel renders the decision for the start note: the mechanism
// plus its reason, or an explicit "setsid only".
func daemonScopeModeLabel(d daemonScopeDecision) string {
	if !d.scopeEscaped() {
		return daemonScopeModeNone + " (" + d.detail + ")"
	}
	return d.mode + " (" + d.detail + ")"
}

// scopeIntEnv is the environment VALUE form of a pid used by tests and by an
// operator inspecting /proc/<pid>/environ (one spelling).
func scopeIntEnv(name string, v int) string {
	return name + daemonFlagValueSep + strconv.Itoa(v)
}
