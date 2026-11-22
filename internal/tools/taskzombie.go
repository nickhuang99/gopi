package tools

// Zombie confirmation for stale in_progress rows (M4.8 ③ — three-layer
// evidence chain, zero priors):
//
//   a) heartbeat stop      — last_toolcall_at older than the threshold: the
//                             stale detector itself (taskheartbeat.go).
//   b) claimed pid liveness — claimed_by carries "hostname:pid" (the claim
//                             stamp); a signal-0 probe decides dead/alive on
//                             the local host. STRONG evidence: zero false
//                             positives (a live claimed pid suppresses the
//                             stale event entirely).
//   c) session JSONL mtime  — <gopi dir>/sessions/<session_id>.jsonl mtime
//                             age corroborates the death (reported in the
//                             payload for the verify agent; never gates —
//                             the file may not exist for external rows).
//
// a+b both hit = CONFIRMED zombie → the verify agent decides the action
// (reissue the work as a fresh row, or cancel + redispatch — per the payload
// evidence). a alone (or a + c) = SUSPECTED → the event still fires but the
// payload marks it unconfirmed. tools→gopilog is a forbidden import edge
// (chat.go:620 — gopilog imports tools), so the session path is resolved
// locally from config.GopiDir.

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// claimed_by/claimed_at are written by the CLAIM ladder (pending→claimed CAS,
// crash-recovery re-stamp), NOT by the process that executes the row, so an
// in_progress row's stamp can name a process that stopped running long before
// the row started (TaskStart used to discard its `by` argument outright). The
// pid layer below then reads that dead pid as a LIVE holder whenever the pid
// is reused or unreaped, and its veto suppresses the stale event entirely —
// the row is stuck in_progress with no evidence reaching any consumer.
//
// The axis that closes this: a claim stamp is only credible as "the process
// currently executing" if it was written at/after the row's in_progress
// transition (in_progress_at). A stamp OLDER than that transition belongs to
// a previous claim, so its pid says nothing about the current run — the
// verdict degrades to UNVERIFIED and the pid layer loses its veto (the row is
// reported, never disposed on that evidence; a LIVE process is still caught
// by every other layer: the heartbeat and the worktree liveness scan).
const (
	// claimStampFresh: written at/after the row started — the pid IS the
	// executor and its liveness is authoritative (the live veto holds).
	claimStampFresh = "fresh"

	// claimStampStale: written BEFORE the row started — a previous
	// claimant's stamp. Its pid is not the executor; treated as UNVERIFIED.
	claimStampStale = "stale"

	// claimStampUnknown: no referent to judge against — a row that entered
	// in_progress on a build without in_progress_at (legacy NULL), or a row
	// with no claim stamp at all. The pid layer keeps its pre-existing vote,
	// so nothing that worked before the column existed changes behaviour.
	claimStampUnknown = "unknown"

	// claimStampHolderGone: the stamp IS this row's own (it was written with
	// the transition, so claimStampFreshness calls it "fresh") but the process
	// it names is NOT ALIVE RIGHT NOW — the executor died and the row was never
	// closed. It is a FOURTH reading, not a reclassification: the age axis still
	// says "fresh" (the stamp belongs to this run), while the liveness axis says
	// "nothing of this run is running".
	//
	// WHY IT EXISTS (measured 2026-10-27, row 01a17aba): `gopi task status
	// --worktree <ground>` printed
	//
	//	holder:      the host:1698361 (claim stamp is fresh for this run)
	//
	// while /proc/1698361 did not exist, and the very next two lines said
	// `verdict: dead` / `live: pid 1698361 alive=no`. The suffix was rendered
	// from the AGE axis alone (claimStampFresh: written at/after the row
	// started), and a reader takes "fresh for this run" as "this run is going":
	// the executor's death was stated twice and denied once on the same screen.
	// A six-hour-old corpse reading "fresh" is what made the ground look
	// permanently IN USE.
	claimStampHolderGone = "holder-gone"

	// claimStampSkewSec is the tolerance below which a stamp counts as
	// written WITH the transition rather than before it: the CAS claim and
	// the start transition are two statements a few milliseconds apart, and
	// their timestamps are second-resolution floats, so requiring
	// stamp >= referent exactly would misclassify the normal claim→start
	// race as stale (it must stay fresh — that pair is the healthy shape).
	claimStampSkewSec = 5
)

// procZombieState: the zombie probe's output value. The path it reads
// (procRootName/procStatName) is the ONE definition in
// task_finish_cleanup.go's live-CWD scan constants — same package, so the
// existing names are referenced, never re-declared (Rule 13/14).
const procZombieState = "Z" // the state of a terminated, not-yet-reaped process

// pidAlive: REMOVED — it was the SECOND implementation of the liveness probe
// and it contradicted its own doc comment. The comment said "EPERM = exists
// (not ours, still alive)" while the code was `if syscall.Kill(pid, 0) != nil
// { return false }`, which reports EPERM as DEAD (epic 01a17e87: measured
// uid 1000 → pidAlive(1) == false about init). The package's ONE probe is now
// PidLivenessOf (service_control.go → internal/procstat), and this package's
// callers ask `pidAlive` — kept below as a thin, NON-duplicating delegate so
// the 28 internal call sites and the "zombie is dead" contract they rely on
// stay put while the DECISION lives in exactly one place (Rules 2/13/14).
//
// The zombie clause it had is inside the shared probe too (procstat reads the
// /proc state letter and answers VerdictGone for Z and for the measured
// transient X), so the behaviour is preserved and the duplication is gone.

// pidAlive reports whether pid is a live process, conservatively: only a
// MEASURED death answers false (see PidAlive's contract). A terminated-but-
// unreaped process is DEAD here — the common shape is a SIGKILLed wrapper that
// is its own child's unreaped zombie, and reading it as a live holder is what
// let a dead runner veto every stale verdict AND every close-out (2026-09-12
// incident, precondition bug).
func pidAlive(pid int) bool {
	return PidAlive(pid)
}

// pidGone reports whether pid is PROVABLY gone — the predicate every
// revocation, removal and reclaim arm in this package must use. It is NOT the
// negation of pidAlive: an unanswerable probe (EPERM, an I/O error, pid <= 0)
// answers FALSE here, because "I could not prove it is dead" is a reason to KEEP
// a record, never a reason to release or delete one (Rule 15, epic 01a17e87).
func pidGone(pid int) bool {
	return PidGone(pid)
}

// pidIsZombie reports whether /proc/<pid>/stat's state field is 'Z' —
// terminated, waiting to be reaped. Best-effort by contract: a missing or
// unreadable stat file (non-Linux host, a race with the parent's wait) reports
// FALSE, which leaves the pre-existing signal-0 verdict in place. The probe
// can therefore only turn a live-LOOKING pid into dead, never the reverse
// (Rule 15: no silent upgrade of evidence).
func pidIsZombie(pid int) bool {
	b, err := os.ReadFile(filepath.Join(procRootName, strconv.Itoa(pid), procStatName))
	if err != nil {
		return false
	}
	// The format is "<pid> (<comm>) <state> …", and comm may itself contain
	// spaces and parentheses (a process named "foo (bar)"), so the state is
	// the field after the LAST ')' — never a positional split on spaces.
	i := bytes.LastIndexByte(b, ')')
	if i < 0 || i+2 >= len(b) {
		return false
	}
	return string(b[i+2:i+3]) == procZombieState
}

// claimStampFreshness classifies the row's claim stamp against its
// execution-start referent (in_progress_at) — the axis that answers "is this
// pid the process CURRENTLY executing the row?" (see the claimStamp* consts).
func claimStampFreshness(t *Task) string {
	if t == nil || !t.HasInProgressAt || !t.HasClaim {
		return claimStampUnknown
	}
	if t.ClaimedAt < t.InProgressAt-claimStampSkewSec {
		return claimStampStale
	}
	return claimStampFresh
}

// claimStampHolderLive answers the question the AGE axis cannot: is the
// process this row's claim stamp names alive RIGHT NOW?
//
// It is three readings taken at the moment of the question, never a value kept
// from an earlier one — the shape killGroupStillTheChilds (kill_group_guard.go)
// established for the same reason, that a number read at T1 says nothing about
// what it names at T2:
//
//	① the pid names a process group at all (syscall.Getpgid > 0). ESRCH here is
//	   a POSITIVE death measurement: the kernel has no such process.
//	② the SAME reading repeated immediately answers the same group id. A number
//	   that is churning between groups is being recycled under us, so nothing
//	   about it can be trusted — and an untrustworthy reading may never assert
//	   death (Rule 15a), so it answers LIVE.
//	③ syscall.Kill(pid, 0) succeeds, minus the unreaped-zombie state — the SAME
//	   predicate pidAlive applies, so this function and the pid layer can never
//	   disagree about one pid (Rules 13/14).
//
// Every unmeasurable case answers TRUE: "fresh" is only withdrawn when this
// host measured the holder gone. Nothing here can make a live runner look dead
// — the only refusals are the kernel's own ESRCH and the Z state, which are
// exactly the two readings that mean "this process does no work".
func claimStampHolderLive(pid int) bool {
	if pid <= 0 {
		return true // nothing to measure — absence is not a death claim
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil || pgid <= 0 {
		return false // ① ESRCH: the kernel knows no process by this pid
	}
	again, err := syscall.Getpgid(pid)
	if err != nil || again != pgid {
		// ② the number moved between two readings: recycled (or a race) — the
		// reading cannot be attributed, and an unattributable reading may only
		// weaken a verdict, never manufacture a death.
		return true
	}
	// ③ the pid layer's OWN predicate (signal-0 plus the unreaped-zombie state),
	// so this reading and pidAlive can never disagree about one pid (Rules 13/14).
	return pidAlive(pid)
}

// claimStampVerdict is the reading the OPERATOR-FACING surfaces render
// (TaskProcessStatus.Freshness and the `holder:` line's suffix): the age axis
// of claimStampFreshness, FOLDED WITH the liveness of the pid it names.
//
// The fold is one-directional and it is the whole fix: a stamp the age axis
// calls fresh becomes claimStampHolderGone when this host measures the process
// it names as gone. Nothing else changes value — stale stays stale, unknown
// stays unknown — so every consumer that asks a question about WHO WROTE the
// stamp keeps reading claimStampFreshness, while the verb whose job is "is this
// row really running?" can no longer print "fresh for this run" over a corpse.
//
// The local-only guard is claimHolder's: a FOREIGN stamp's pid names a
// different process on that host, so it is unknowable here and stays as the age
// axis classified it (Rule 15a — never manufacture a death from an absent
// measurement).
func claimStampVerdict(t *Task) string {
	freshness := claimStampFreshness(t)
	if freshness != claimStampFresh {
		return freshness
	}
	_, pid, local := claimHolder(t.ClaimedBy)
	if !local || claimStampHolderLive(pid) {
		return freshness
	}
	return claimStampHolderGone
}

// claimHolder parses a claimed_by stamp ("hostname:pid"). ok=false for the
// empty / malformed / foreign-host cases (a foreign claim's pid is
// unknowable on this host — reported as unverified, never as dead).
// ClaimHolderPID is the EXPORTED face of claimHolder, for a caller OUTSIDE this
// package that must ask "which process does this row's claim stamp name, and is
// it on THIS host?" — the worker control face's `--branch` selector
// (cmd/agent/worker_control_selector.go). It is a one-line delegation on
// purpose: the parsing stays in ONE place (Rule 13/14), so a consumer can never
// arrive at a second, slightly different reading of the `host:pid` stamp — the
// same stamp every liveness judgement in the tree is built on.
//
// local is claimHolder's ok: FALSE means "do not act on this pid from this
// host", which is the case for both a foreign host (where the number names a
// different process) and a stamp that cannot be parsed.
func ClaimHolderPID(claimedBy string) (host string, pid int, local bool) {
	return claimHolder(claimedBy)
}

func claimHolder(claimedBy string) (host string, pid int, ok bool) {
	claimedBy = strings.TrimSpace(claimedBy)
	if claimedBy == "" {
		return "", 0, false
	}
	i := strings.LastIndex(claimedBy, claimHolderHostPidSep)
	if i <= 0 || i == len(claimedBy)-1 {
		return "", 0, false
	}
	n, err := strconv.Atoi(claimedBy[i+1:])
	if err != nil || n <= 0 {
		return "", 0, false
	}
	host = claimedBy[:i]
	if host != hostnameOrEmpty() {
		return host, n, false // foreign host: pid means nothing here
	}
	return host, n, true
}

// hostnameOrEmpty caches nothing — os.Hostname errors are rare; empty means
// "cannot compare" and the pid layer degrades to unverified.
func hostnameOrEmpty() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}
