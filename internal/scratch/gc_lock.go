// gc_lock.go — the ONE lock decision for the scratch sweep.
//
// THE DEFECT THIS FIXES (measured 2026-10-12, task 01a1300b): the lock could be
// released only by MTIME (GCLockStaleAge, 10 min). A GC that CRASHED left
// `.gc.lock` behind with its own pid written inside; every later pass read
// EEXIST, reported locked=false, SweepHome returned `(0, nil)`, and the CLI
// printed a successful `removed=0` with an empty stderr. "Nothing to collect"
// and "the sweep never ran" looked IDENTICAL — Rule 15's silent failure — while
// the holder pid in the lock file was decidable RIGHT THEN (/proc, same host,
// same user).
//
// The decision is therefore ONE three-valued question about the recorded owner
// ("lock holder pid 4242"):
//
//	LIVE       the process directory is there and the process is not a zombie
//	           → the lock is real: DEFER, and say so with the pid (a correct
//	             judgement is not an error — it just must not look like a sweep
//	             that found nothing).
//	GONE       no /proc/<pid> at all, or a terminated-but-unreaped zombie (which
//	           answers kill(pid,0) while doing no work) → the lock is ABANDONED:
//	           break it and retry once, so the sweep runs in THIS pass.
//	UNKNOWN    nothing decidable: no pid recorded in the lock, an unparsable pid,
//	           or procfs that would not answer → DEFER, marked as unmeasured.
//	           Unknown is never a licence to break (same asymmetry as the holder
//	           rule: a leaked directory is disk usage, a double sweep over a live
//	           one is a race).
//
// The MTIME valve is UNCHANGED and still applies to every arm (a lock older than
// GCLockStaleAge is broken whatever its content says) — it is the recovery path
// for a RECYCLED pid, where the number now names a stranger that never held this
// lock, and it is the pre-existing contract the stale-age test pins. The pid
// check only ever makes the sweep run EARLIER, never later.
//
// A verdict is EVIDENCE, not just a boolean: the caller carries GCLockEvidence
// to its output line, so `removed=0` stops being the whole story. The type is
// deliberately separate from EntryVerdict (which describes a CANDIDATE) — a lock
// decides whether there is a sweep at all.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

// GCLockOutcome is the word a lock decision is reported under. Four values, one
// per way the question can come out — a caller that renders them apart makes
// "deferred" and "never ran" distinguishable from "ran and removed nothing"
// (Rule 15).
type GCLockOutcome string

const (
	// GCLockOutcomeAcquired: no lock was held, or an abandoned one was broken;
	// this process owns it and the sweep runs.
	GCLockOutcomeAcquired GCLockOutcome = "acquired"
	// GCLockOutcomeBrokeAbandoned: a lock WAS held, its owner is provably gone
	// (dead pid or the mtime valve); it was removed and the lock retaken.
	GCLockOutcomeBrokeAbandoned GCLockOutcome = "broke-abandoned"
	// GCLockOutcomeDeferredLive: a LIVE process holds the lock. The sweep is
	// deferred — a correct outcome, and it must be readable as such.
	GCLockOutcomeDeferredLive GCLockOutcome = "deferred-live-owner"
	// GCLockOutcomeDeferredUnknown: the lock exists but its owner could not be
	// decided (no pid recorded, unparsable pid, or procfs that did not answer),
	// and the mtime valve had not expired either. Deferred, marked as unmeasured.
	GCLockOutcomeDeferredUnknown GCLockOutcome = "deferred-unmeasurable"
)

// GCLockOwnerLiveness is the three-valued answer about the pid recorded in the
// lock file. It is the whole point of this file: "alive" and "cannot tell" must
// not share a value, because only one of them may authorise breaking a lock.
type GCLockOwnerLiveness string

const (
	// GCLockOwnerLive: the process is there and working.
	GCLockOwnerLive GCLockOwnerLiveness = "live"
	// GCLockOwnerGone: provably not there — no /proc entry, or a zombie.
	GCLockOwnerGone GCLockOwnerLiveness = "gone"
	// GCLockOwnerUnknown: the kernel did not answer (no pid recorded anywhere,
	// an unparsable pid, an unreadable stat). NEVER rendered as "gone".
	GCLockOwnerUnknown GCLockOwnerLiveness = "unknown"
)

// GCLockOwnerNone is how a verdict with no recorded holder pid is rendered — a
// named word, so an operator never has to read `owner-pid=0` as "pid zero".
const GCLockOwnerNone = "none"

// gclockBreakRetries is how many times the create is retried after an abandoned
// lock is broken. ONE is deliberate: a second failure means somebody re-created
// the lock in the gap, which is a live competitor, not a stale file.
const gclockBreakRetries = 1

// GCLockVerdict is one lock decision, carried as EVIDENCE to the caller's output
// line: whether this process now owns the lock, which pid the previous holder
// recorded, whether an abandoned lock was broken to get here, and the sentence
// that decided it.
type GCLockVerdict struct {
	// Locked reports whether this caller now owns the lock (and must release it).
	Locked bool
	// BrokeAbandoned reports that a lock WAS held by an abandoned owner and was
	// removed by this call — the fact an operator needs to see, because it means
	// every previous pass was silently deferred.
	BrokeAbandoned bool
	// OwnerPid is the pid recorded by the lock that was found (0 when none was
	// readable).
	OwnerPid int
	// HasOwnerPid reports whether OwnerPid is a real reading (Rule 15a: a zero
	// that means "not measured" must not be readable as a value).
	HasOwnerPid bool
	// Outcome is the word this decision is reported under.
	Outcome GCLockOutcome
	// Reason is the deciding sentence — why deferred, why broken, who holds it.
	Reason string
}

// GCLockFields renders a lock verdict as the logfmt FIELDS a count line appends
// (` lock=<outcome> owner=pid:<n>`), so a sweep's own line states which lock
// decision produced it. ONE renderer (Rule 1b), because three call sites print
// it: the CLI, the resident tick and the sandbox arm.
func GCLockFields(v GCLockVerdict) string {
	owner := GCLockOwnerNone
	if v.HasOwnerPid {
		owner = fmt.Sprintf("pid:%d", v.OwnerPid)
	}
	return fmt.Sprintf(" lock=%s owner=%s", v.Outcome, owner)
}

// GCLockEvidence renders a lock verdict as ONE sentence for a log line: the
// decision word, the holder it was measured against, and the reason that decided
// it. The same renderer feeds every arm, so the three cannot drift (Rule 1b).
func GCLockEvidence(v GCLockVerdict) string {
	owner := GCLockOwnerNone
	if v.HasOwnerPid {
		owner = fmt.Sprintf("pid:%d", v.OwnerPid)
	}
	return fmt.Sprintf("lock=%s owner=%s reason=%q", v.Outcome, owner, v.Reason)
}

// GCLockNeedsReporting reports whether a verdict is worth its OWN line: anything
// other than a plain acquire. A pass that broke an abandoned lock, deferred to a
// live owner, or could not measure the owner is exactly the shape that used to
// be invisible (Rule 15), so those are the outcomes a caller narrates.
func GCLockNeedsReporting(v GCLockVerdict) bool {
	return v.Outcome != GCLockOutcomeAcquired
}

// GCLockOwnerLivenessFor answers the three-valued question about one pid, from
// procfs. It is the ONLY place the lock's liveness is decided, so the break arm,
// the defer arm and every test rest on one answer.
func GCLockOwnerLivenessFor(pid int) (GCLockOwnerLiveness, string) {
	if pid <= 0 {
		return GCLockOwnerUnknown, "no pid recorded"
	}
	dir := filepath.Join(ProcRootDir, strconv.Itoa(pid))
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return GCLockOwnerGone, fmt.Sprintf("pid %d has no %s entry: the holder is gone", pid, dir)
		}
		return GCLockOwnerUnknown, fmt.Sprintf("pid %d could not be probed (%s): %v", pid, dir, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ProcStatName))
	if err != nil {
		// The directory is there but the kernel would not answer (no procfs on
		// this host, or a restricted read). An unmeasured owner is NOT a dead one.
		return GCLockOwnerUnknown, fmt.Sprintf("pid %d is present but its %s is unreadable: %v", pid, ProcStatName, err)
	}
	state, ok := procname.StatField(string(raw), procname.StatFieldState)
	if !ok {
		return GCLockOwnerUnknown, fmt.Sprintf("pid %d: %s carries no parsable state field", pid, ProcStatName)
	}
	if state == procname.ZombieState {
		// Terminated but unreaped: it answers kill(pid,0) while doing no work,
		// so for a lock it is GONE — the same ruling internal/procname states
		// for a zombie runner. It cannot be sweeping anything.
		return GCLockOwnerGone, fmt.Sprintf("pid %d is a zombie: its holder exited without releasing the lock", pid)
	}
	return GCLockOwnerLive, fmt.Sprintf("pid %d is alive", pid)
}

// gclockOwnerLiveness is the package-level seam over GCLockOwnerLivenessFor: the
// production binding is the procfs question, and a test can rebind it for one
// case to prove the DECISION depends on the answer rather than on the mtime
// alone (the red proof behind this file — see gc_lock_test.go's
// TestGCLock_DecisionRestsOnLivenessNotMtimeAlone). Nothing else mutates it, and
// the production path never reads anything but the real function.
var gclockOwnerLiveness = GCLockOwnerLivenessFor

// ParseGCLockOwnerPid reads the pid this package writes into the lock file
// ("%d\n"). A missing, empty or non-numeric content is REPORTED as unmeasured
// (hasPid=false), never guessed — an unreadable lock is exactly the case that
// must keep deferring.
func ParseGCLockOwnerPid(raw []byte) (pid int, hasPid bool) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return 0, false
	}
	n, err := strconv.Atoi(text)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// AcquireGCLockVerdict is the ONE lock decision: it tries to take the lock, and
// when one is already held it judges its recorded owner (live / gone / unknown)
// before deciding whether the sweep may run and whether an abandoned lock must
// be broken. The verdict it returns is what the caller reports.
func AcquireGCLockVerdict(home string) (release func(), v GCLockVerdict, err error) {
	lockPath := filepath.Join(home, GCLockFileName)
	release, v, err = createGCLock(lockPath)
	if err != nil || v.Locked {
		return release, v, err
	}
	// EEXIST: a lock exists. Judge the owner before believing it.
	breakIt, found := gclockDisposition(lockPath)
	if !breakIt {
		return func() {}, found, nil
	}
	if rmErr := os.Remove(lockPath); rmErr != nil && !os.IsNotExist(rmErr) {
		// Could not break it: report why rather than pretending to sweep.
		found.Outcome = GCLockOutcomeDeferredUnknown
		found.Reason = fmt.Sprintf("abandoned lock %s could not be removed: %v", lockPath, rmErr)
		return func() {}, found, nil
	}
	for attempt := 0; attempt <= gclockBreakRetries; attempt++ {
		release, v, err = createGCLock(lockPath)
		if err == nil && v.Locked {
			v.BrokeAbandoned = true
			v.OwnerPid, v.HasOwnerPid = found.OwnerPid, found.HasOwnerPid
			v.Outcome = GCLockOutcomeBrokeAbandoned
			v.Reason = fmt.Sprintf("%s — abandoned lock broken and the lock retaken", found.Reason)
			return release, v, nil
		}
		if err != nil && !os.IsExist(err) {
			return nil, v, fmt.Errorf("gc lock %s: %w", lockPath, err)
		}
	}
	// The gap was taken by somebody else: that is a live competitor now.
	found.Outcome = GCLockOutcomeDeferredLive
	found.Reason = fmt.Sprintf("%s — another process re-created %s immediately after it was broken", found.Reason, lockPath)
	return func() {}, found, nil
}

// createGCLock is the ONE create: O_EXCL, so the kernel — not a check-then-act
// sequence — decides who owns the lock. The OWNER PID is written into the file,
// which is what makes the abandoned-holder question decidable at all.
func createGCLock(lockPath string) (release func(), v GCLockVerdict, err error) {
	f, openErr := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, FilePerm)
	if openErr != nil {
		if os.IsExist(openErr) {
			// Not a verdict yet: the caller has to look at the holder.
			return func() {}, GCLockVerdict{}, nil
		}
		return nil, GCLockVerdict{}, fmt.Errorf("gc lock %s: %w", lockPath, openErr)
	}
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid()) //stylegate:ignore best-effort lock-file write; the accompanying Close and flock semantics carry the meaning
	_ = f.Close()                              //stylegate:ignore close of a probe/temp file whose content was already consumed
	return func() {
			if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "scratch: gc lock %s not removed: %v\n", lockPath, err)
			}
		}, GCLockVerdict{
			Locked:  true,
			Outcome: GCLockOutcomeAcquired,
			Reason:  fmt.Sprintf("created %s for pid %d", lockPath, os.Getpid()),
		}, nil
}

// gclockDisposition judges a lock that EXISTS. It returns whether the lock may
// be broken (the sweep may then run) plus the verdict that says WHY — the
// sentence the CLI prints so a deferred pass and an empty pass stop looking
// alike. The mtime valve is applied on every arm; the pid check only ever makes
// the break happen EARLIER.
func gclockDisposition(lockPath string) (breakIt bool, v GCLockVerdict) {
	fi, err := os.Stat(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Vanished under us: retake it, and say so rather than claiming a break.
			return true, GCLockVerdict{
				Outcome: GCLockOutcomeBrokeAbandoned,
				Reason:  fmt.Sprintf("lock %s disappeared before it could be judged", lockPath),
			}
		}
		return false, GCLockVerdict{
			Outcome: GCLockOutcomeDeferredUnknown,
			Reason:  fmt.Sprintf("lock %s could not be stated: %v", lockPath, err),
		}
	}
	raw, readErr := os.ReadFile(lockPath)
	pid, hasPid := ParseGCLockOwnerPid(raw)
	if readErr != nil {
		hasPid = false
	}
	stale := time.Since(fi.ModTime()) > GCLockStaleAge
	v = GCLockVerdict{OwnerPid: pid, HasOwnerPid: hasPid}

	if !hasPid {
		detail := "records no pid"
		if readErr != nil {
			detail = fmt.Sprintf("could not be read: %v", readErr)
		}
		if stale {
			return true, GCLockVerdict{
				Outcome: GCLockOutcomeBrokeAbandoned,
				Reason: fmt.Sprintf("lock %s %s and is older than %s — broken by the stale-age valve",
					lockPath, detail, GCLockStaleAge),
			}
		}
		return false, GCLockVerdict{
			Outcome: GCLockOutcomeDeferredUnknown,
			Reason: fmt.Sprintf("lock %s %s (owner unmeasurable) and is younger than %s — unknown is never broken",
				lockPath, detail, GCLockStaleAge),
		}
	}

	liveness, why := gclockOwnerLiveness(pid)
	switch liveness {
	case GCLockOwnerGone:
		return true, GCLockVerdict{
			Outcome:     GCLockOutcomeBrokeAbandoned,
			OwnerPid:    pid,
			HasOwnerPid: true,
			Reason:      fmt.Sprintf("lock %s records pid %d, which is dead (%s)", lockPath, pid, why),
		}
	case GCLockOwnerLive:
		if stale {
			// The pre-existing valve, and its reason: the lock file's mtime is
			// written ONCE and never refreshed, so an over-long sweep — or a pid
			// since recycled by a stranger — must not disable the sweep forever.
			return true, GCLockVerdict{
				Outcome:     GCLockOutcomeBrokeAbandoned,
				OwnerPid:    pid,
				HasOwnerPid: true,
				Reason: fmt.Sprintf("lock %s is older than %s (holder pid %d, %s) — broken by the stale-age valve",
					lockPath, GCLockStaleAge, pid, why),
			}
		}
		return false, GCLockVerdict{
			Outcome:     GCLockOutcomeDeferredLive,
			OwnerPid:    pid,
			HasOwnerPid: true,
			Reason: fmt.Sprintf("lock %s is held by live pid %d (%s) — this pass deferred, another GC is running",
				lockPath, pid, why),
		}
	default:
		if stale {
			return true, GCLockVerdict{
				Outcome: GCLockOutcomeBrokeAbandoned,
				Reason: fmt.Sprintf("lock %s records pid %d, which could not be measured (%s), and is older than %s — broken by the stale-age valve",
					lockPath, pid, why, GCLockStaleAge),
			}
		}
		return false, GCLockVerdict{
			Outcome:     GCLockOutcomeDeferredUnknown,
			OwnerPid:    pid,
			HasOwnerPid: true,
			Reason: fmt.Sprintf("lock %s records pid %d, which could not be measured (%s), and is younger than %s — unknown is never broken",
				lockPath, pid, why, GCLockStaleAge),
		}
	}
}
