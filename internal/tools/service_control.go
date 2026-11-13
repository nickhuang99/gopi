// service_control.go — the SHARED, component-parameterized half of a resident
// gopi service's control surface (single-instance lock + per-instance state and
// stop-request files).
//
// WHY IT EXISTS (epic worker-tree-daemon, 2026-09-13): the merger grew this
// surface first (merger_graceful.go / merger_status.go, epic
// merger-graceful-restart), and `gopi worker --daemon` now needs the SAME
// thing — a per-pid state file, a durable stop request and a liveness probe
// that does not take the lock. Copying those helpers for the worker would be a
// second implementation of an identical mechanism, drifting one fix behind the
// other (PROJECT.md Rule 14). So the mechanism lives HERE, parameterized by the
// component's directory + file prefixes, and the merger's exported functions
// are thin delegates that fix those parameters to the merger's values.
//
// WHAT IS *NOT* SHARED: the component's own STATE SCHEMA. The merger's state
// carries pass/repo/merge counters and an interrupt descriptor; the worker's
// carries round counters. Only the file placement, the pid↔name encoding and
// the liveness predicate are common — a shared struct would be a superset every
// component half-fills.

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/procstat"
)

const (
	// ControlFileMode / ControlDirMode are the permission bits of a control
	// file (a per-instance state file or a stop request) and of the directory
	// holding it. Named once so every component's control files share one
	// mode (Rule 1: no magic numbers at the call sites).
	ControlFileMode = 0o644
	// ControlDirMode is the directory mode of a component's control directory.
	ControlDirMode = 0o755

	// StopRequestPrefix names a component's completion request file: the file
	// "<prefix><pid>" asks the process with that pid to finish its in-flight
	// work and exit. The spelling carries the signal's INTENT (terminate
	// gracefully) — a bare pid would not say why the file exists. It is
	// component-INDEPENDENT (one vocabulary) and scoped by the component's own
	// directory, so a worker request can never be read by a merger.
	StopRequestPrefix = "TERM_"

	// ForceRequestPrefix names a FORCE request file: "<prefix><pid>" asks the
	// process to interrupt in-flight work NOW. A DISTINCT file (not a content
	// flag inside StopRequestPrefix+pid) because the two requests can arrive in
	// either order and a force must never be downgraded to a graceful stop by a
	// request that was written first.
	ForceRequestPrefix = "FORCE_"
)

// ControlDir resolves <gopi data root>/<name>, CREATING it — the writer's
// half. One resolver per component name, so an operator has one place to look
// and a sandboxed service (GOPI_HOME) never writes into the real profile.
func ControlDir(name string) (string, error) {
	gd, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("resolve gopi dir: %w", err)
	}
	dir := filepath.Join(gd, name)
	if err := os.MkdirAll(dir, ControlDirMode); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return dir, nil
}

// ControlDirReadOnly resolves <gopi data root>/<name> WITHOUT creating it —
// the reader's half. A status probe must not have the side effect of creating
// directories (the same side-effect-freedom discipline the lock probe keeps).
func ControlDirReadOnly(name string) (string, error) {
	gd, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("resolve gopi dir: %w", err)
	}
	return filepath.Join(gd, name), nil
}

// ControlStatePath is the per-instance STATE file for pid under dir: the
// "this pid is running, and here is what it is doing" record.
func ControlStatePath(dir, prefix string, pid int) string {
	return filepath.Join(dir, prefix+strconv.Itoa(pid))
}

// ControlRequestPath is the per-instance REQUEST file for pid under dir: the
// durable "please stop" record a component reads at its own boundary.
func ControlRequestPath(dir, prefix string, pid int) string {
	return filepath.Join(dir, prefix+strconv.Itoa(pid))
}

// ControlPidFromName recovers the pid a control file name addresses (0 when
// the name carries prefix + a non-positive integer). ONE decoder for both file
// kinds (Rule 2: the encoder ControlStatePath/ControlRequestPath and this
// decoder are the only places the encoding exists).
func ControlPidFromName(prefix, name string) int {
	if !strings.HasPrefix(name, prefix) {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimPrefix(name, prefix))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// PidLiveness is the THREE-VALUED answer of the shared liveness probe, aliased
// to procstat.Verdict so this package cannot drift from the ONE definition
// (Rules 2/14: procstat is the leaf package that owns the verdict vocabulary).
// Callers inside tools name it PidLiveness; the values are Alive/Gone/Unknown.
//
// WHY THREE VALUES AND NOT A BOOL (epic 01a17e87, measured): kill(pid, 0)
// answers in THREE ways, and two of them mean completely different things:
//
//	:nil                 the process EXISTS and is ours — alive
//	:EPERM               the process EXISTS and is not ours — ALIVE
//	:ESRCH               the kernel knows no such process — GONE
//	:anything else       the probe could not answer — UNKNOWN
//
// Folding EPERM into "dead" (what this package did before) turns "I am not
// allowed to look at it" into "it is not there". Measured on this host with
// uid 1000: syscall.Kill(1, 0) returns EPERM, so the old PidAlive(1) said
// FALSE about init — a live process reported dead. For a display that only
// omits somebody else's process; for a DELETION or OWNERSHIP decision it
// erases the record of a live process (the worker state residue sweep, the
// claim-holder liveness verdicts, the reclaim arms).
type PidLiveness = procstat.Verdict

// PidAlive reports whether pid is a live, RUNNING process — CONSERVATIVELY.
//
// It answers false ONLY for a measurement of death (PidGone); a probe that
// could not decide (unknown) answers TRUE. That direction is deliberate and it
// is the whole point of the three-valued probe: every caller of this predicate
// is asking "may I still treat this process as holding something?" — a
// WRONG "dead" releases or deletes a live process's record, while a wrong
// "alive" merely makes a caller wait, retry or report a stale holder. Unknown
// is therefore mapped onto the harmless side, and the decision is made once,
// here, instead of at 24 call sites that each guessed.
//
// A ZOMBIE is NOT alive: it has exited (its loop is done, its lock released)
// and only awaits a reap. Reporting it as running would make every stop/restart
// report a phantom busy service.
//
// DELETION call sites must NOT use this predicate — they use PidGone, because
// "unknown" is a reason to KEEP a record, never a reason to remove one.
//
// A NON-POSITIVE pid is false, not unknown-therefore-alive: `pid <= 0` names no
// process at all (it is the "no PID was recorded" sentinel), so there is
// nothing that could be running. That is the established contract this
// predicate has always had, pinned by TestPidAlive. It is NOT the `pid <= 1`
// special case the epic forbids: init IS signalable-shaped evidence (pid 1 is
// simply unsignalable by a non-root caller), while 0 and negatives are not pids.
func PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return PidLivenessOf(pid) != procstat.VerdictGone
}

// PidGone reports whether pid is PROVABLY gone — the predicate every DELETION,
// reclaim and reap arm must use before it removes a record.
//
// Only an ESRCH measurement (or a proven-terminal /proc state) answers true.
// EPERM — "it exists and we may not signal it" — is a measurement of EXISTENCE,
// so it answers FALSE, and a probe that could not decide answers FALSE as well
// (Rule 15: an unknown is never upgraded into a deletion).
//
// The distinction from PidAlive is not cosmetic: the worker state residue sweep
// removes the state file of every pid that is "not alive", so a predicate that
// folds EPERM into death deletes the record of a live worker owned by another
// user. Both predicates read ONE probe, so they can never disagree about what
// the kernel said — only about which side "unknown" is safe on.
func PidGone(pid int) bool {
	return PidLivenessOf(pid) == procstat.VerdictGone
}

// PidLivenessOf reports the three-valued liveness of pid through the ONE shared
// probe (internal/procstat). It is the single funnel for this package: the
// boolean views above are derived from it, so a change to the probe's semantics
// cannot reach some call sites and miss others (Rules 2/13/14).
func PidLivenessOf(pid int) PidLiveness {
	return procstat.Alive(pid)
}

// LiveStateFiles lists the state files under dir whose pid is ALIVE (pids
// ascending, deterministic for tests and operators). A state file whose pid is
// gone is skipped: the file is the record of a dead instance, not a running
// service (a writer cannot clean up after SIGKILL).
//
// The raw file bytes are returned beside the pid — the component parses its
// OWN schema (see the file header: the state struct is deliberately not
// shared).
func LiveStateFiles(dir, prefix string) []ControlInstance {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []ControlInstance
	for _, e := range entries {
		pid := ControlPidFromName(prefix, e.Name())
		if pid == 0 || !PidAlive(pid) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue // the process died between the liveness check and the read
		}
		out = append(out, ControlInstance{PID: pid, Path: path, Raw: strings.TrimSpace(string(raw))})
	}
	sortControlInstances(out)
	return out
}

// LiveStopRequestPIDs lists the pids under dir with a pending stop request
// whose process is still ALIVE — the actionable half of the request directory
// (a request for a dead pid is a no-op, and reporting it as pending would tell
// an operator to wait for a process that is already gone).
func LiveStopRequestPIDs(dir, prefix string) []int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	pids := make([]int, 0, len(entries))
	for _, e := range entries {
		pid := ControlPidFromName(prefix, e.Name())
		if pid == 0 || !PidAlive(pid) {
			continue
		}
		pids = append(pids, pid)
	}
	sortInts(pids)
	return pids
}

// ControlInstance is one live instance's raw state record: the pid the FILE
// NAME encodes plus the file's bytes. The component parses Raw into its own
// state type — the parse belongs to whoever owns the schema.
type ControlInstance struct {
	PID  int
	Path string
	Raw  string
}

// WriteControlText durably writes one component control file (state or
// request) via write-to-tmp + rename, so a reader never sees a half-written
// record. Best-effort by contract at the CALL SITE: a state file that cannot
// be written must never stop a service from working.
func WriteControlText(path string, body []byte) error {
	tmp := controlTmpPath(path)
	if err := os.WriteFile(tmp, body, ControlFileMode); err != nil {
		return fmt.Errorf("write control file %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("install control file %s: %w", path, err)
	}
	return nil
}

// controlTmpPath is the in-flight write path for ONE write of `path`: the
// caller's path plus a suffix unique to (this process, this write).
//
// WHY IT IS NOT A CONSTANT SUFFIX (measured 2026-10-15, host nick-sager). The
// predecessor spelled `path + controlTmpSuffix`, i.e. ONE tmp name shared by
// EVERY writer of that control file. A control file has several writers by
// design — a worker's state record is flushed from its loop goroutine and from
// the durable-request watcher goroutine, and the durable request file is
// written by the operator's `--stop` process — so two flushes that overlap on
// the same path both write that one tmp, and the SECOND rename(2) finds the
// name already consumed by the first:
//
//	gopi worker: install control file .../worker-state-<pid>: rename
//	  .../worker-state-<pid>.tmp .../worker-state-<pid>: no such file or directory
//
// Measured: 93 such lines in 3m49s on one profile, 51 in another, and a unit
// arm with 8 concurrent writers to one path lost 5-6 of them per run. The
// failure is not cosmetic: the state record is the ONLY answer to "what is this
// worker doing?" and to "would a stop have to wait?" — a lost flush means a
// `--status` or a `--stop` reads a record that is one boundary behind, or
// missing entirely.
//
// The uniqueness is (pid, per-process sequence), both of which the kernel
// guarantees no other writer can produce for the same instant: a pid is unique
// among live processes, and the counter is unique among that process's writes.
// The tmp stays a SIBLING of the target, which is what keeps the rename atomic
// (a tmp in another directory, or on another filesystem, could make rename(2)
// fail with EXDEV instead of installing).
//
// The NAME still carries controlTmpSuffix, so a reader that filters control
// files by prefix (LiveStateFiles, the status enumeration) continues to ignore
// an in-flight tmp exactly as before.
func controlTmpPath(path string) string {
	return fmt.Sprintf("%s%s%d.%d", path, controlTmpSuffix, os.Getpid(), controlTmpSeq.Add(1))
}

// controlTmpSeq is controlTmpPath's per-process write counter (see there).
var controlTmpSeq atomic.Uint64

// ClearControlText removes a control file, treating "already gone" as success
// (the exit path's cleanup: removing a request that was never written must not
// be an error).
func ClearControlText(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove control file %s: %w", path, err)
	}
	return nil
}

// controlTmpSuffix marks the in-flight write of a control file. A reader
// filters on the component's prefixes, so a tmp file is never mistaken for a
// record.
const controlTmpSuffix = ".tmp"
