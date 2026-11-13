package main

// daemon_userbus_probe.go — WHY the daemon ladder's rung ① (systemd-run --user
// --scope) failed, as a MEASURED host state instead of one blended sentence.
//
// THE DEFECT THIS FILE ANSWERS (epic: user-bus diagnosis, 2026-09-15). When
// BOTH escape rungs fail, the seam reports
//
//	setsid-only (Failed to connect to bus: No such file or directory; in-process escape: …)
//
// and that ONE sentence covers THREE host states with three DIFFERENT repairs.
// The operator therefore cannot tell "this host has no user manager at all"
// from "the manager is alive and its bus socket path was removed from the
// filesystem while the kernel still holds the socket" — the state measured on
// this host (2026-09-15: /run/user/1000/bus and /run/user/1000/systemd/private
// both absent from the VFS, both still BOUND in /proc/net/unix, connect()
// returning ENOENT, user@1000.service `active` on the system bus). The
// distinction is not cosmetic: the unlinked state is repairable at the manager
// without a reboot, while a genuinely absent manager is a different problem
// entirely — and a diagnostic that names neither sends the reader to the wrong
// place (the same failure mode devdoc/no-privilege-daemon.md records).
//
// THE FOUR STATES (three distinguishable faults + one healthy, plus unknown):
//
//  1. no-user-manager  — user@<uid>.service is not active. The bus clients fail
//     because there is nobody to answer; systemd-run has no manager to create a
//     scope in.
//  2. bus-path-unlinked — the manager IS alive, the socket path is ABSENT in the
//     VFS (stat → ENOENT), the kernel still has a LISTENing AF_UNIX socket bound
//     at that path (/proc/net/unix row + inode), and connect() fails ENOENT
//     because pathname resolution never finds the node. The manager is fine; the
//     PATH is gone. This is this host's state, and it is the silent rung-①
//     disabler.
//  3. bus-node-stale   — a node EXISTS at the path but no socket accepts on it
//     (a previous manager's leftover, or something else holding the name):
//     connect() is refused, not ENOENT. Different repair from ②.
//  4. bus-ready        — the path exists and accepts connections (the state in
//     which systemd-run --user has a manager to talk to).
//
// MEASURED, NEVER PREDICTED (Rule 15). Every field of the verdict is an
// observation: the liveness answer is the one `systemctl is-active user@<uid>
// .service` gives on the SYSTEM bus (the only bus that answers on this host —
// the user manager's own bus is the thing under diagnosis, so asking it would
// be circular), the path verdict is stat(2), the kernel verdict is
// /proc/net/unix, and the client verdict is a REAL connect() with a bounded
// timeout — nothing here infers a mechanism from a config listing, and nothing
// claims a cause (the epic's rule: measured, never predicted, and the diagnosis
// never asserts WHO unlinked the socket).
//
// WHAT IS DELIBERATELY NOT DONE HERE:
//
//   - no privilege is prescribed, and the repair that needs one is reported as
//     an OPERATOR action, never as gopi's own step (Rule 25; the string sweep in
//     daemon_scope_no_privilege_test.go gates the production literals of this
//     package).
//   - no host repair is attempted: gopi does not restart the user manager, does
//     not create the runtime directory and does not touch the sockets it only
//     observes. This file reads procfs, stats two paths, dials two sockets and
//     asks systemd one question.
//   - no dependency: stdlib only (net, os, os/exec, bufio, syscall).

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// --- the reason CODES: the discriminator's wire values (Rule 1: one place) ---
//
// They are short, greppable and stable on purpose: an operator (and a test)
// reads the reason off the diagnosis' first line, and "three distinct reasons"
// means three distinct VALUES of this field, not three spellings of one.
const (
	// userBusReasonNoManager is state ①: no user manager for this uid.
	userBusReasonNoManager = "no-user-manager"
	// userBusReasonUnlinked is state ②: manager alive, socket path unlinked
	// from the VFS while the kernel still has the socket bound.
	userBusReasonUnlinked = "bus-path-unlinked"
	// userBusReasonStale is state ③: a node exists at the path but refuses.
	userBusReasonStale = "bus-node-stale"
	// userBusReasonHealthy is state ④: the bus answers.
	userBusReasonHealthy = "bus-ready"
	// userBusReasonUnknown is the honest floor: the probe could not classify
	// the state. It is a distinct value so a reader never mistakes "we do not
	// know" for one of the named faults.
	userBusReasonUnknown = "unknown"
)

// --- the operator-facing VERDICTS, one per reason (Rule 1/14: no bare prose
// typed at a print site). Each is ONE line: the degraded start is the normal
// outcome on this host, so the block is read on every restart. ---
const (
	userBusVerdictNoManager = "the user manager for this uid is not running, so no manager answers a scope request"
	userBusVerdictUnlinked  = "the user manager is alive, but this socket path is gone from the filesystem while the kernel still has the socket bound"
	userBusVerdictStale     = "a socket node exists at this path but nothing accepts connections on it (the node is stale)"
	userBusVerdictHealthy   = "the user bus answers at this path"
	userBusVerdictUnknown   = "the user manager / user bus state could not be classified"
)

// --- the ACTIONABLE next step, one per reason. This is the half the blended
// sentence never had: the repair differs per state, and the one that needs an
// operator (restarting the manager) is named as THEIRS — never as a step gopi
// takes, and never as a privilege gopi asks for (Rule 25). The substring
// "not a privilege problem" is kept verbatim: it is the correction fragment the
// no-privilege gate requires to stay in the operator-facing text. ---
const (
	userBusNextNoManager = "start a login session for uid %d (or enable lingering for it with `loginctl enable-linger %d`) so a user manager exists, then retry"
	userBusNextUnlinked  = "this is not a privilege problem: the manager's runtime directory must be recreated (operator action on this host: restart user-runtime-dir@%d.service, then user@%d.service) — and keep tmpdir/scratch sweeps out of the runtime directory, because emptying it unlinks these sockets while the manager keeps running"
	userBusNextStale     = "something else is holding this path, or the manager must recreate the socket (operator action: restart user@%d.service)"
	userBusNextHealthy   = ""
	// The unknown state's next step names the SAME commands the probe ran, with
	// absolute paths (the diagnosis must work from a stripped environment), so a
	// reader can reproduce the measurement without trusting this process.
	userBusNextUnknown = "ask the host directly: `%s %s %s`, `stat '%s'`, `grep -F '%s' %s`"
)

// --- naming facts (Rule 13: constants, never a bare literal at a use site) ---

const (
	// userBusManagerUnitFmt is the user manager unit name ("user@1000.service").
	userBusManagerUnitFmt = "user@%d.service"
	// userBusRuntimeDirFallbackFmt is the runtime directory systemd uses when
	// XDG_RUNTIME_DIR is not exported — the same location the user bus lives in.
	userBusRuntimeDirFallbackFmt = "/run/user/%d"
	// userBusRuntimeDirEnv is the base-directory variable the bus path is
	// derived from. It is READ (not assumed): a session whose runtime dir is
	// somewhere else must be diagnosed at ITS paths, or the probe would stat a
	// path the session never used (Rule 15).
	userBusRuntimeDirEnv = "XDG_RUNTIME_DIR"
	// userBusPrivateDirName / userBusPrivateSocketName name the manager's own
	// socket ("<runtime>/systemd/private") — the one `systemd-run --user`
	// dials. BOTH paths are probed: systemd-run needs the private socket while
	// ordinary session clients (dbus-send, session bus tools) need the bus
	// socket, and they can be in different states.
	userBusPrivateDirName    = "systemd"
	userBusPrivateSocketName = "private"
	// userBusSocketName is the session bus socket's file name ("<runtime>/bus").
	userBusSocketName = "bus"
	// userBusPathKindPrivate / ...Bus label which socket a path observation is
	// about (the evidence line names it).
	userBusPathKindPrivate = "private"
	userBusPathKindBus     = "bus"

	// procNetUnixPath is the kernel's table of AF_UNIX sockets: the evidence
	// that a socket is BOUND even when its pathname is gone.
	procNetUnixPath = "/proc/net/unix"
	// procNetUnixPathFieldIdx0 is the index of the Path column in a
	// /proc/net/unix row. Zero-based and named, so the parser cannot drift from
	// the kernel's format by one field (the column layout is fixed by the kernel
	// and documented in proc(5)).
	//
	// READ THE COLUMNS OFF THE RAW ROW, NEVER OFF proc(5). The man page's
	// header line ("Num RefCount Protocol Flags Type St Inode Path") is
	// ZERO-BASED and so omits `Num`; the FIELD in position 0 is not an index at
	// all — it is the row's address, rendered as a 16-hex-digit LABEL ending in
	// ':' ("0000000000000000:"), so it does not stand in for a missing column
	// and every column that follows sits one position to the RIGHT of its
	// header name. Measured (kernel 6.8.0-138-generic), one row, verbatim:
	//
	//	0000000000000000: 00000002 00000000 00010000 0001 01 17620230 /run/user/1000/bus
	//	[0] addr label  [1] RefCount [2] Protocol [3] Flags [4] Type [5] St
	//	[6] Inode       [7] Path
	//
	// so [5]=state, [6]=inode, [7]=path IS the correct mapping for this row
	// shape, and it is the label at [0] that makes an eight-field row eight
	// fields. A parser that dropped the label would read [4]=St and [5]=Inode
	// and silently see no LISTEN. Verify with `cat /proc/net/unix` rather than
	// with the header.
	procNetUnixPathFieldIdx0 = 7
	// procNetUnixStateFieldIdx0 is the index of the St column, whose
	// unixStateListen value marks an accepting socket.
	procNetUnixStateFieldIdx0 = 5
	// procNetUnixInodeFieldIdx0 is the index of the inode column — the number
	// an operator can match against `ls -l /proc/<pid>/fd` to find the holder.
	procNetUnixInodeFieldIdx0 = 6
	// unixStateListen is the St column value for a LISTENing AF_UNIX socket.
	unixStateListen = "01"

	// systemctlPath is the absolute systemd client (no PATH dependency: the
	// diagnosis must work from a stripped daemon environment).
	systemctlPath = "/usr/bin/systemctl"
	// systemctlIsActiveArg is the systemd verb that answers liveness with an
	// exit status and one word.
	systemctlIsActiveArg = "is-active"
	// systemctlShowArg is the systemd verb that answers one property of one unit
	// (`--property=<name> --value` prints just the value, and exits 0 whether or
	// not the unit exists).
	systemctlShowArg = "show"
	// systemctlPropertyFlag names the property to report. It is NOT optional:
	// without it systemctl treats the property WORD as a unit name and dumps
	// every property of the real unit — measured (the existing-unit check then
	// read a NON-EMPTY answer for a name systemd does not know, and refused
	// every start).
	systemctlPropertyFlag = "--property"
	// systemctlValueFlag asks for the property's VALUE alone — without it the
	// output is "Property=value", which a caller must then parse.
	systemctlValueFlag = "--value"
	// systemctlPropertyLoadState is the property that answers "does this unit
	// exist (loaded or not)?" in ONE WORD. Measured on this host: a name systemd
	// does not know answers "not-found"; a live transient scope answers
	// "loaded". The alternative (`list-unit-files`) prints a HEADER, so a
	// non-empty-output test reads a missing unit as taken — measured, and the
	// reason this comparison is a word and not a line count.
	systemctlPropertyLoadState = "LoadState"
	// systemctlPropertyMainPID is the property naming a unit's main process. IT
	// IS NOT READ ANY MORE, and the reason is measured rather than stylistic:
	// systemd 249 reports NO pid for a transient SCOPE — `--property=MainPID`
	// and `--property=PIDs` both print an EMPTY value for a live scope on this
	// host (2026-09-16), so the holder is found through the unit's CGROUP
	// instead (daemonUnitHolderPids). The constant is kept because the
	// DIAGNOSIS still prints it (`gopi worker --status`), where an empty value
	// is honest information about the scope mechanism.
	systemctlPropertyMainPID = "MainPID"
	// systemctlPropertyControlGroup is the property that DOES name a scope's
	// processes: the unit's cgroup directory, a path relative to the cgroup
	// mount point. Measured on this host (2026-09-16): a live scope answers
	// "/user.slice/user-1000.slice/user@1000.service/app.slice/<unit>.scope",
	// and one `cat <that>/cgroup.procs` returns the holder's pid — the
	// MainPID-free way to identify who holds a scope name.
	systemctlPropertyControlGroup = "ControlGroup"
	// daemonPidDecimalBytes is the width of one decimal pid in a cgroup.procs
	// listing, used only to size that read (a pid is at most 7 digits on Linux,
	// plus its newline). It is a capacity hint, never a parse bound: the list is
	// parsed by field.
	daemonPidDecimalBytes = 8
	// systemctlQueryBudget bounds EACH systemctl question on the start path. A
	// wedged user bus must not hold the caller's prompt, and two questions plus
	// the probe still fit inside the handshake's own budget.
	systemctlQueryBudget = 5 * time.Second
	// daemonUnitFileSuffix is appended when a unit NAME is handed to systemctl
	// (the tools take a unit FILE name).
	daemonUnitFileSuffix = ".scope"
	// daemonLoadStateNotFound is systemd's own word for a unit it does not know
	// — the ONLY value that means "the name is free": a unit in any other state
	// holds its name, including while it is tearing down.
	daemonLoadStateNotFound = "not-found"
	// procEnvPathFmt reads a live process's environment (NUL-separated). It is
	// the identity evidence the existing-unit decision compares, and the ONLY
	// place this path shape is spelled.
	procEnvPathFmt = "/proc/%d/environ"
	// procCommPathFmt reads a live process's command name — EVIDENCE in a
	// message (daemonProcessComm), never part of a decision.
	procCommPathFmt = "/proc/%d/comm"
	// systemctlActiveWord is that verb's positive answer.
	systemctlActiveWord = "active"
	// systemctlInactiveExit is the exit status of `systemctl is-active` for a
	// unit systemd KNOWS and which is not running — the "no manager answers"
	// answer.
	systemctlInactiveExit = 3
	// systemctlUnknownExit is the same verb's exit status for a unit systemd
	// does not know at all. It is a different fact from "known and inactive"
	// and the SAME operator consequence (no manager answers here), which is why
	// both are accepted — while any other status is a failure of the QUESTION
	// and must not be read as an answer (userBusManagerActive).
	systemctlUnknownExit = 4
	// userBusLivenessBudget bounds the system-bus question. A diagnosis runs on
	// a start path: a wedged bus must not hold the caller's prompt.
	userBusLivenessBudget = 5 * time.Second
	// userBusConnectBudget bounds EACH connect() attempt (2 paths ⇒ ≤ 2×). It
	// is what turns "connect hangs" into a reported state instead of a hung
	// start.
	userBusConnectBudget = 2 * time.Second

	// userBusUnixNetwork is the socket family every user-bus path is on.
	userBusUnixNetwork = "unix"
	// userBusFailureMarkerWord is the token every user-bus client failure this
	// epic is about carries in systemd's own words ("Failed to connect to bus:
	// No such file or directory"). The diagnosis is asked for ONLY when rung ①
	// died of that word, so a systemd-run failure that has nothing to do with
	// the bus never gets a user-bus verdict printed beside it.
	userBusFailureMarkerWord = "bus"
	// userBusUnclassifiedFailure is the honest word for a connect/stat failure
	// whose errno the probe could not extract — never a fallback number, and
	// never a claim about the mechanism.
	userBusUnclassifiedFailure = "unclassified failure"
)

// --- the operator-facing LINE shapes (Rule 1: no format string at a print
// site). Four lines, each bounded: the block is printed on every degraded
// start. ---
const (
	// userBusDiagHeaderFmt names the state machine, the reason and the verdict
	// in one line, so the reason is greppable where an operator looks first.
	userBusDiagHeaderFmt = "%s: user bus (%s): %s\n"
	// userBusDiagMeasuredFmt carries the observation the reason was read from.
	userBusDiagMeasuredFmt = "%s:   measured: %s\n"
	// userBusDiagConfirmFmt carries the commands that reproduce the measurement.
	userBusDiagConfirmFmt = "%s:   confirm: %s\n"
	// userBusDiagNextFmt carries the actionable next step (omitted when the
	// state has none, i.e. when the bus is ready).
	userBusDiagNextFmt = "%s:   next: %s\n"
	// userBusBlockerNoteFmt is the ONE-line form used before the fork: it names
	// the reason rung ① died of, so the start note's blended detail is not the
	// only thing an operator sees.
	userBusBlockerNoteFmt = "%s: user bus is not answering (%s): %s\n"
	// userBusUnlinkedStateFmt words the unlinked state and CARRIES THE ROW
	// COUNT: the path is gone while the kernel still holds it, and more than
	// one /proc/net/unix row naming it is how a live client shows up in the
	// table (see userBusUnixBinding). Naming the count keeps the sentence true
	// for the plain case (one row) as well as the shared one.
	userBusUnlinkedStateFmt = "path absent, socket bound (unlinked, %d /proc/net/unix row(s))"
	// userBusRowCountFmt is the row-count clause of the evidence line.
	userBusRowCountFmt = "%d /proc/net/unix row(s)"
)

// userBusPathObservation is what ONE socket path was measured to be. Every
// field is an observation or its error — never an inference (Rule 15).
type userBusPathObservation struct {
	// path is the absolute socket path that was probed.
	path string
	// kind is userBusPathKindPrivate or userBusPathKindBus.
	kind string
	// statErr is the stat(2) verdict: nil when a node exists at path,
	// fs.ErrNotExist when the path is absent (the discriminator's first half),
	// anything else when the path could not be examined at all.
	statErr error
	// bound reports that /proc/net/unix still has a socket at this path.
	bound bool
	// listen reports that the bound socket's state is LISTEN.
	listen bool
	// inode is the bound socket's kernel inode (0 when none was found) — the
	// value an operator matches in /proc/<pid>/fd.
	inode uint64
	// bindingRows is how many /proc/net/unix rows carried this path (0 when
	// none did, ≥2 when a client's row shares the path with the listener's —
	// see userBusUnixBinding). Evidence, never a decision input.
	bindingRows int
	// bindingErr non-nil means the kernel's table could not be READ (not that
	// it held no row). The two are different facts: the first must reach the
	// classifier as unknown, the second as "nothing is bound here".
	bindingErr error
	// connectErr is what a REAL connect() to path returned (nil = accepted).
	connectErr error
	// connectErrno is that error's errno (0 when connectErr is nil, or when the
	// errno could not be extracted).
	connectErrno syscall.Errno
}

// userBusObservation is the whole measurement: the liveness answer plus one
// observation per probed path.
type userBusObservation struct {
	// uid is whose manager/bus was probed.
	uid int
	// unit is the manager unit name the liveness question was asked about.
	unit string
	// runtimeDir is the directory the probed paths were derived from.
	runtimeDir string
	// managerActive is the liveness answer. It is only meaningful when
	// livenessErr is nil.
	managerActive bool
	// livenessErr non-nil means the liveness question could not be asked or
	// answered (no systemctl, no system bus, a wedged bus): the classifier then
	// refuses to name a fault it did not measure.
	livenessErr error
	// paths are the socket path observations, in probe order.
	paths []userBusPathObservation
}

// userBusDiagnosis is the CLASSIFIER's verdict: one reason, one verdict
// sentence, the measurement it was read from, the commands that reproduce it
// and the next step. It is the value the seam renders and the tests compare —
// one struct, so "three distinct reasons" is a field comparison.
type userBusDiagnosis struct {
	// reason is one of the userBusReason* codes.
	reason string
	// verdict is the operator-facing sentence for reason.
	verdict string
	// measured is the evidence line the reason was read from.
	measured string
	// confirm is the command(s) an operator runs to reproduce the measurement.
	confirm string
	// next is the actionable next step ("" when the state needs none).
	next string
	// managerActive / pathAbsent / socketBound / connectErrno are the raw facts
	// the classification used, kept for tests and for a programmatic caller.
	managerActive bool
	pathAbsent    bool
	socketBound   bool
	connectErrno  syscall.Errno
	// path is the decisive socket path ("" when none was decisive).
	path string
}

// userBusManagerUnit is the manager unit name for a uid ("user@1000.service").
func userBusManagerUnit(uid int) string {
	return fmt.Sprintf(userBusManagerUnitFmt, uid)
}

// userBusRuntimeDir is the runtime directory whose sockets are probed: the
// session's OWN XDG_RUNTIME_DIR when it is exported, else systemd's default
// location for the uid. Reading the variable instead of assuming the default is
// what keeps the probe honest on a session that was given a different one.
func userBusRuntimeDir(uid int, envRuntimeDir string) string {
	if strings.TrimSpace(envRuntimeDir) != "" {
		return envRuntimeDir
	}
	return fmt.Sprintf(userBusRuntimeDirFallbackFmt, uid)
}

// userBusPathSpec is one socket path to probe, with the kind it is.
type userBusPathSpec struct {
	path string
	kind string
}

// userBusPathSpecs lists the two sockets of a runtime directory in probe order:
// the manager's private socket first (it is the one `systemd-run --user` dials,
// so it is the decisive one for rung ①), then the session bus socket.
func userBusPathSpecs(runtimeDir string) []userBusPathSpec {
	return []userBusPathSpec{
		{path: filepath.Join(runtimeDir, userBusPrivateDirName, userBusPrivateSocketName), kind: userBusPathKindPrivate},
		{path: filepath.Join(runtimeDir, userBusSocketName), kind: userBusPathKindBus},
	}
}

// probeDaemonUserBus measures the host and classifies it. This is the function
// the daemon seam calls; the two halves are separate so the classifier is
// testable on synthetic observations without a host in a given state.
func probeDaemonUserBus() userBusDiagnosis {
	return classifyUserBus(observeUserBus(os.Getuid(), os.Getenv(userBusRuntimeDirEnv)))
}

// observeUserBus takes the measurement: one liveness question and one
// observation per socket path. It never fails as a whole — a measurement that
// could not be taken is carried as an error field, and the classifier decides
// what can be said from what WAS measured.
func observeUserBus(uid int, envRuntimeDir string) userBusObservation {
	obs := userBusObservation{
		uid:        uid,
		unit:       userBusManagerUnit(uid),
		runtimeDir: userBusRuntimeDir(uid, envRuntimeDir),
	}
	obs.managerActive, obs.livenessErr = userBusManagerActive(obs.unit)
	for _, spec := range userBusPathSpecs(obs.runtimeDir) {
		obs.paths = append(obs.paths, observeUserBusPath(spec.path, spec.kind))
	}
	return obs
}

// userBusManagerActive asks the SYSTEM bus whether the user manager unit is
// active, and returns the answer with the error of the question itself.
//
// WHY THE SYSTEM BUS AND NOT `systemctl --user` (the circularity rule): the
// user manager's own bus is the thing under diagnosis. Asking it would answer
// "no bus" whenever the bus is the fault, which is exactly the blended message
// this epic removes. /run/dbus/system_bus_socket is a different bus, owned by
// the system manager on this host, and it is what makes "there is no manager"
// distinguishable from "the manager is alive and its sockets are gone".
//
// THE EXIT STATUS IS THE ANSWER: `systemctl is-active` prints "active" and exits
// 0 for a live unit, prints "inactive"/"failed" and exits 3 for a known unit
// that is not running, and exits 4 for a unit systemd does not know at all.
// Both 3 and 4 mean "no manager answers here" — the difference between them is
// NOT turned into a fault, because the observation supports only the liveness
// fact.
func userBusManagerActive(unit string) (bool, error) {
	out, err := userBusRunBounded(systemctlPath, systemctlIsActiveArg, unit)
	word := strings.TrimSpace(out)
	if err == nil {
		if word != systemctlActiveWord {
			// A zero exit with another word ("reloading", "activating") is an
			// answer this probe does not classify: saying "no manager" about it
			// would be an unmeasured claim (Rule 15).
			return false, fmt.Errorf("ask the system bus about %s: unexpected answer %q", unit, word)
		}
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case systemctlInactiveExit, systemctlUnknownExit:
			// A status answer, not a failure of the question: systemd knows the
			// unit (or knows none by that name) and nothing is running. Both
			// mean "no manager answers here".
			return false, nil
		}
	}
	// The question itself could not be asked (no systemctl, no system bus, a
	// bus that refused): the caller must NOT read this as "no manager" — that
	// is the same fabricated-verdict family the degraded-note epic removed a
	// layer up (Rule 15).
	return false, fmt.Errorf("ask the system bus about %s: %w", unit, err)
}

// userBusRunBounded runs a binary with a bounded budget and returns its
// combined output. It is a leaf helper: the availability of the binary is
// checked first, so "not installed" and "failed" are different errors.
func userBusRunBounded(bin string, args ...string) (string, error) {
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("%s: %w", bin, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), userBusLivenessBudget)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// observeUserBusPath measures ONE socket path: stat(2) for the pathname, the
// kernel's /proc/net/unix table for the binding, and a real connect() for the
// client's verdict. The three are INDEPENDENT facts, which is precisely what
// makes the unlinked state visible: a path can be absent while the socket is
// bound, and it is the pair that names state ②.
func observeUserBusPath(path, kind string) userBusPathObservation {
	o := userBusPathObservation{path: path, kind: kind}
	if _, err := os.Stat(path); err != nil {
		o.statErr = err
	}
	binding, bindErr := userBusKernelBinding(path)
	if bindErr != nil {
		// The kernel's table could not be read: that is NOT "no socket is
		// bound" (see userBusKernelBinding), so it is carried as its own fact
		// and the classifier refuses to name a fault from it.
		o.bindingErr = bindErr
	} else if binding.found {
		o.bound, o.listen = binding.bound, binding.listen
		o.inode, o.bindingRows = binding.inode, binding.rows
	}
	o.connectErr, o.connectErrno = userBusConnect(path)
	return o
}

// userBusUnixBinding is what /proc/net/unix says about one path.
//
// A LOOKUP IS NOT A ROW SEARCH (measured 2026-09-15, the positive control's
// failure). ONE pathname can be carried by MORE THAN ONE row at the same
// instant: a single client `connect()` makes the kernel render a SECOND row for
// the same path — St=02 (unconnected), Inode=0, the connected socket object
// rather than the bound listener — and that row can come FIRST in the file. A
// scan that stops at the first row whose path column matches therefore describes
// the CLIENT, not the binding, and reports inode 0 and no LISTEN for a socket
// that is bound and listening at that very moment. Verbatim, both BEFORE the
// unlink, one client having connected (kernel 6.8.0-138-generic):
//
//	0000000000000000: 00000002 00000000 00000000 0001 02     0 /dev/shm/…/bus   <- first match
//	0000000000000000: 00000002 00000000 00010000 0001 01 17620230 /dev/shm/…/bus   <- the listener
//
// The two rows are not two bindings: the path column is what `bind(2)` created,
// and `unix_mkname` in the kernel copies the path into EVERY socket's address
// struct — a client's included, via connect(2). Only the LISTEN row is a
// binding, and its inode is the one an operator can match in /proc/<pid>/fd.
// So the whole table is scanned and the rows are FOLDED: the LISTEN row wins
// wherever it sits in the file, and `bound` means "the kernel holds a
// LISTENing socket at this path".
type userBusUnixBinding struct {
	// found is false when NO row carried this path at all — the honest
	// "nothing is bound here", which is a different fact from an unreadable
	// table (userBusKernelBinding's error return) and from a row that carried
	// no LISTEN (found, with bound false).
	found bool
	// bound is true when a LISTENing row was found for the path: the kernel
	// still holds the socket, which is the fact state ② is read from.
	bound bool
	// listen is true when that row's state is LISTEN (an accepting socket).
	listen bool
	// inode is the bound socket's kernel inode (0 when no row carried one).
	inode uint64
	// rows is how many /proc/net/unix rows carried this path: 0 when none did,
	// ≥2 when a client's row shares the path with the listener's. It is
	// EVIDENCE — carried into the measured line — and never an input to the
	// classification.
	rows int
}

// userBusFoldUnixRows folds every table row that carried one path into what the
// kernel holds at it: `bound` is set by the LISTENing row wherever it sits in
// the file, and the inode reported is that row's. It is a PURE function of the
// rows so the fold's order-independence is a test over values — the positive
// control cannot construct the dangerous interleaving on demand (a client's row
// is placed by the kernel, not by the test).
//
// A row too short to carry a path column is skipped: it was not addressed by
// this lookup, and every decision here is read from a named column (Rule 1).
func userBusFoldUnixRows(rows [][]string) userBusUnixBinding {
	var b userBusUnixBinding
	for _, fields := range rows {
		if len(fields) <= procNetUnixPathFieldIdx0 {
			continue
		}
		b.found = true
		b.rows++
		inode, inoErr := strconv.ParseUint(fields[procNetUnixInodeFieldIdx0], 10, 64)
		if inoErr != nil {
			// An inode column the kernel printed in a shape this parser cannot
			// read is a loud zero, never a silent one: a misread column is the
			// defect family this fold exists for (Rule 15).
			inode = 0
		}
		if b.rows == 1 {
			b.inode = inode // a non-LISTENing row's inode is reported when no LISTENing row turns up
		}
		if fields[procNetUnixStateFieldIdx0] == unixStateListen {
			b.bound, b.listen, b.inode = true, true, inode
			continue
		}
		if !b.bound && b.inode != 0 {
			b.inode = inode
		}
	}
	return b
}

// userBusKernelBinding looks path up in /proc/net/unix and reports what the
// kernel still holds for it. A path whose socket was unlinked has NO row with
// the pathname — but the sockets this epic is about keep their row (measured
// 2026-09-15: inode 40136 for /run/user/1000/bus, LISTEN, connect → ENOENT), so
// the lookup is the second half of the discriminator.
//
// THE SCAN VISITS EVERY ROW (see userBusUnixBinding): the first row that
// matches a path is NOT necessarily the binding. A listener's row is settled by
// taking the first LISTENing row and letting it stand, and by remembering the
// first inode seen should NO row be LISTENing (a non-listening row can still
// carry one, and reporting it is honest; reporting 0 while the kernel holds a
// socket would not be).
//
// The file is read once per path: it is a small procfs file and a start path
// must not grow a cache with a lifetime of its own.
func userBusKernelBinding(path string) (userBusUnixBinding, error) {
	f, err := os.Open(procNetUnixPath)
	if err != nil {
		return userBusUnixBinding{}, fmt.Errorf("read %s: %w", procNetUnixPath, err)
	}
	defer f.Close() //stylegate:ignore read-only procfs handle; the scan's result is what matters and the fd dies with this process
	sc := bufio.NewScanner(f)
	var rows [][]string
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) <= procNetUnixPathFieldIdx0 {
			continue
		}
		if fields[procNetUnixPathFieldIdx0] != path {
			continue
		}
		rows = append(rows, fields)
	}
	// A table that could not be read through to the end is NOT a table that
	// holds no row: returning "no binding" here would turn an unreadable table
	// into the same answer as an absence, and the classifier reads an absence
	// as a fault (Rule 15 — the two must stay distinguishable).
	if err := sc.Err(); err != nil {
		return userBusUnixBinding{}, fmt.Errorf("scan %s: %w", procNetUnixPath, err)
	}
	binding := userBusFoldUnixRows(rows)
	return binding, nil
}

// userBusConnect dials path with a bounded timeout and returns the error plus
// its errno. The errno is the classification's third fact: ENOENT on an absent
// path is state ② (the node is not resolvable), ECONNREFUSED on a present node
// is state ③ (the node exists, nobody accepts).
func userBusConnect(path string) (error, syscall.Errno) {
	conn, err := net.DialTimeout(userBusUnixNetwork, path, userBusConnectBudget)
	if err == nil {
		_ = conn.Close() //stylegate:ignore the reply is irrelevant; closing the probe connection cannot change the verdict
		return nil, 0
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err, 0
	}
	return err, errno
}

// classifyUserBus turns the measurement into ONE reason. It is a pure function
// of the observation — the red-proof case (an unlinked socket must NEVER
// classify as "no user manager") is therefore a test over values, not a test
// that needs a host in the broken state.
//
// The order is the discriminator's order, and each step is a fact that was
// measured:
//
//	A. the liveness question could not be asked  → unknown (never a named fault).
//	B. the manager is not active                 → no-user-manager.
//	C. a path is ABSENT *and* its socket is bound → bus-path-unlinked (②).
//	D. a path is PRESENT and connect() was refused → bus-node-stale (③).
//	E. every probed path connected                → bus-ready.
//	F. anything else measured                     → unknown, with the evidence.
func classifyUserBus(obs userBusObservation) userBusDiagnosis {
	d := userBusDiagnosis{managerActive: obs.managerActive}
	if obs.livenessErr != nil {
		d.reason = userBusReasonUnknown
		d.verdict = userBusVerdictUnknown
		d.measured = fmt.Sprintf("the liveness question failed: %v; paths: %s", obs.livenessErr, userBusObservedPaths(obs))
		d.confirm = userBusConfirmCommands(obs, "")
		d.next = userBusUnknownNext(obs)
		return d
	}
	if !obs.managerActive {
		d.reason = userBusReasonNoManager
		d.verdict = userBusVerdictNoManager
		d.measured = fmt.Sprintf("%s is not %s (asked on the system bus); paths: %s", obs.unit, systemctlActiveWord, userBusObservedPaths(obs))
		d.confirm = fmt.Sprintf("%s %s %s", systemctlPath, systemctlIsActiveArg, obs.unit)
		d.next = fmt.Sprintf(userBusNextNoManager, obs.uid, obs.uid)
		return d
	}
	// A binding table that could not be read makes states ②③④
	// indistinguishable — the classifier would be guessing which one it is,
	// and it only ever names a fault it measured (Rule 15). This is checked
	// AFTER liveness (the two branches above are answers the liveness question
	// already gave) and BEFORE the ② loop, because that loop READS the binding
	// the unreadable table never delivered.
	for _, p := range obs.paths {
		if p.bindingErr == nil {
			continue
		}
		d.reason = userBusReasonUnknown
		d.verdict = userBusVerdictUnknown
		d.path = p.path
		d.measured = fmt.Sprintf("%s is %s; %s %s: the kernel's binding could not be measured: %v",
			obs.unit, systemctlActiveWord, p.kind, p.path, p.bindingErr)
		d.confirm = userBusConfirmCommands(obs, p.path)
		d.next = userBusUnknownNext(obs)
		return d
	}
	// ② before ③ on purpose: an absent-and-bound path is the state this epic
	// exists to name, and it must never fall through to the stale branch (which
	// describes a PRESENT node).
	for _, p := range obs.paths {
		if errors.Is(p.statErr, os.ErrNotExist) && p.bound {
			d.reason = userBusReasonUnlinked
			d.verdict = userBusVerdictUnlinked
			d.path, d.pathAbsent, d.socketBound = p.path, true, true
			d.managerActive, d.connectErrno = true, p.connectErrno
			d.measured = userBusPathEvidence(p, fmt.Sprintf("%s is %s", obs.unit, systemctlActiveWord))
			d.confirm = userBusConfirmCommands(obs, p.path)
			d.next = fmt.Sprintf(userBusNextUnlinked, obs.uid, obs.uid)
			return d
		}
	}
	for _, p := range obs.paths {
		if p.statErr == nil && errors.Is(p.connectErrno, syscall.ECONNREFUSED) {
			d.reason = userBusReasonStale
			d.verdict = userBusVerdictStale
			d.path, d.connectErrno = p.path, p.connectErrno
			d.measured = userBusPathEvidence(p, fmt.Sprintf("%s is %s", obs.unit, systemctlActiveWord))
			d.confirm = userBusConfirmCommands(obs, p.path)
			d.next = fmt.Sprintf(userBusNextStale, obs.uid)
			return d
		}
	}
	if userBusAllPathsReady(obs) {
		d.reason = userBusReasonHealthy
		d.verdict = userBusVerdictHealthy
		d.measured = fmt.Sprintf("%s is %s; %s", obs.unit, systemctlActiveWord, userBusObservedPaths(obs))
		d.confirm = userBusConfirmCommands(obs, "")
		return d
	}
	d.reason = userBusReasonUnknown
	d.verdict = userBusVerdictUnknown
	d.measured = fmt.Sprintf("%s is %s; paths: %s", obs.unit, systemctlActiveWord, userBusObservedPaths(obs))
	d.confirm = userBusConfirmCommands(obs, "")
	d.next = userBusUnknownNext(obs)
	return d
}

// userBusAllPathsReady reports whether EVERY probed path accepted a connection.
func userBusAllPathsReady(obs userBusObservation) bool {
	if len(obs.paths) == 0 {
		return false
	}
	for _, p := range obs.paths {
		if p.connectErr != nil {
			return false
		}
	}
	return true
}

// userBusObservedPaths renders every path observation on ONE line (the reason
// line and the evidence line must stay single lines: this block is read on
// every degraded start).
func userBusObservedPaths(obs userBusObservation) string {
	parts := make([]string, 0, len(obs.paths))
	for _, p := range obs.paths {
		parts = append(parts, p.kind+" "+userBusPathState(p))
	}
	return strings.Join(parts, ", ")
}

// userBusPathState names what was measured about one path, in the shortest
// honest words: absent/bound/unlinked, present-but-refusing, or answering.
func userBusPathState(p userBusPathObservation) string {
	switch {
	case errors.Is(p.statErr, os.ErrNotExist) && p.bound:
		return fmt.Sprintf(userBusUnlinkedStateFmt, p.bindingRows)
	case errors.Is(p.statErr, os.ErrNotExist):
		return "path absent, no socket bound"
	case p.statErr != nil:
		return "unreadable: " + p.statErr.Error()
	case p.connectErr == nil:
		return "accepting connections"
	default:
		return "present but refusing (" + userBusErrnoName(p.connectErrno) + ")"
	}
}

// userBusPathEvidence is the decisive path's evidence line: the path, what its
// stat/connect said, the kernel's binding (with the inode, so the holder can be
// found) and the liveness fact it is read together with.
func userBusPathEvidence(p userBusPathObservation, liveness string) string {
	binding := "no /proc/net/unix row"
	if p.bound {
		binding = fmt.Sprintf("%s row, inode %d, %s", userBusStateWord(p.listen), p.inode,
			fmt.Sprintf(userBusRowCountFmt, p.bindingRows))
	}
	connect := userBusErrnoName(p.connectErrno)
	if p.connectErr == nil {
		connect = "accepted"
	}
	// The path's state is rendered by the SAME helper the summary line uses
	// (userBusPathState), so the decisive line and the summary can never word
	// one measurement two ways — and the raw *fs.PathError is not pasted in,
	// because its own "stat <path>: " prefix would read as a second command.
	return fmt.Sprintf("%s %s is %s; connect %s; kernel %s; %s",
		p.kind, p.path, userBusPathState(p), connect, binding, liveness)
}

// userBusStateWord words the kernel's socket state ("LISTEN" or "not LISTEN").
func userBusStateWord(listen bool) string {
	if listen {
		return "LISTEN"
	}
	return "not LISTEN"
}

// userBusErrnoName renders an errno for the operator without a bare number: the
// errno IS the discriminator (ENOENT names the unlinked state, ECONNREFUSED the
// stale node), so it must be readable.
func userBusErrnoName(errno syscall.Errno) string {
	switch errno {
	case 0:
		return "no error"
	case syscall.ENOENT:
		return "ENOENT (no such file or directory)"
	case syscall.ECONNREFUSED:
		return "ECONNREFUSED (connection refused)"
	case syscall.EACCES:
		return "EACCES (permission denied)"
	default:
		return userBusUnclassifiedFailure
	}
}

// userBusConfirmCommands is the operator's reproduction: the SAME commands this
// probe issues, so a reader can confirm the diagnosis on the host without
// trusting gopi's word for it. The decisive path is placed FIRST when there is
// one (it is the path the reason was read from).
func userBusConfirmCommands(obs userBusObservation, decisive string) string {
	ordered := make([]string, 0, len(obs.paths))
	for _, p := range obs.paths {
		if p.path == decisive {
			ordered = append(ordered, p.path)
		}
	}
	for _, p := range obs.paths {
		if p.path != decisive {
			ordered = append(ordered, p.path)
		}
	}
	cmds := []string{fmt.Sprintf("%s %s %s", systemctlPath, systemctlIsActiveArg, obs.unit)}
	for _, path := range ordered {
		cmds = append(cmds, fmt.Sprintf("stat '%s'", path))
		cmds = append(cmds, fmt.Sprintf("grep -F '%s' %s", path, procNetUnixPath))
	}
	return strings.Join(cmds, "; ")
}

// userBusUnknownNext is the unknown state's next step: the exact three questions
// the classifier could not answer, so the reader is not left with a shrug.
func userBusUnknownNext(obs userBusObservation) string {
	path := ""
	if len(obs.paths) > 0 {
		path = obs.paths[0].path
	}
	return fmt.Sprintf(userBusNextUnknown, systemctlPath, systemctlIsActiveArg, obs.unit, path, path, procNetUnixPath)
}

// daemonUserBusDiagnosisLines renders the operator-facing diagnosis for a start
// whose ladder fell through: the reason, the measurement, the confirming
// commands and the next step. It is the ONE place that decides these lines, so
// no print site can grow a second opinion (the same discipline as
// daemonScopeMeasureLines).
func daemonUserBusDiagnosisLines(prog string, d userBusDiagnosis) []string {
	lines := []string{
		fmt.Sprintf(userBusDiagHeaderFmt, prog, d.reason, d.verdict),
		fmt.Sprintf(userBusDiagMeasuredFmt, prog, d.measured),
		fmt.Sprintf(userBusDiagConfirmFmt, prog, d.confirm),
	}
	if d.next != "" {
		lines = append(lines, fmt.Sprintf(userBusDiagNextFmt, prog, d.next))
	}
	return lines
}

// daemonUserBusBlockerNote is the ONE-line form for the pre-fork note: it names
// the reason rung ① died of, so the chosen-mechanism line stops being the only
// place the blended sentence appears. It is empty for a ready bus (nothing to
// report) — the note exists for the degraded case only.
func daemonUserBusBlockerNote(prog string, d userBusDiagnosis) string {
	if d.reason == userBusReasonHealthy {
		return ""
	}
	return fmt.Sprintf(userBusBlockerNoteFmt, prog, d.reason, d.verdict)
}

// userBusFailureMentionsBus reports whether a rung-① failure is the one this
// diagnosis can classify. It is a word test on systemd's OWN output — the
// ladder is the only reason the user bus is probed at all, so a systemd-run
// failure about something else (a unit-name clash, a missing binary) must not
// collect a user-bus verdict it did not earn.
func userBusFailureMentionsBus(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), userBusFailureMarkerWord)
}

// daemonUserBusDiagnosis is the LADDER-level entry: it diagnoses why rung ①
// could not create a scope, and returns a zero diagnosis when the failure was
// not a user-bus failure at all (so the caller prints nothing).
//
// WHY IT TAKES THE LADDER'S OWN ERROR: the reason rung ① died is what decides
// whether a user-bus verdict is meaningful, and the probe must not be run (nor
// its cost paid) on a start whose rung ① failed for an unrelated reason.
func daemonUserBusDiagnosis(rung1Err error) userBusDiagnosis {
	if !userBusFailureMentionsBus(rung1Err) {
		return userBusDiagnosis{}
	}
	return probeDaemonUserBus()
}

// daemonUserBusDiagnosed reports whether a diagnosis carries a state worth
// printing (the zero value does not).
func daemonUserBusDiagnosed(d userBusDiagnosis) bool {
	return d.reason != ""
}

// daemonUserBusReport prints the diagnosis for a start whose ladder fell
// through, and is a no-op when the diagnosis was not taken. It exists so the
// seam has ONE call site, and so the lines come from
// daemonUserBusDiagnosisLines (no second opinion about the wording).
func daemonUserBusReport(prog string, d userBusDiagnosis) {
	if !daemonUserBusDiagnosed(d) {
		return
	}
	for _, line := range daemonUserBusDiagnosisLines(prog, d) {
		fmt.Fprint(os.Stderr, line)
	}
}
