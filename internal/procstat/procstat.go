// Package procstat answers ONE question about a reported process: is the
// process that was alive when the record was written still alive now?
//
// It is a leaf package: no imports of project packages, no business logic —
// only the constant that names the liveness probe's result and the probe
// itself, so every consumer (session backfill, crash classification, future
// liveness checks) shares ONE definition instead of mirroring a string or a
// bare literal per package (Rule 14).
package procstat

// Verdict is the answer of a liveness probe. It is deliberately three-valued:
// "gone" and "unknown" are different facts, and treating them alike is what
// makes a crash report over-claim (Rule 15: a session whose classification
// cannot be determined must NOT be reported as a crash).
type Verdict string

const (
	// VerdictAlive means the probe proved the process still exists. A session
	// whose process is alive has NOT crashed — it is simply still being
	// written to. This is the verdict that turns a live session's missing
	// session_end marker from "crash" into "still running".
	VerdictAlive Verdict = "alive"
	// VerdictGone means the probe proved the process no longer exists. Only
	// this verdict may take part in a crash classification.
	VerdictGone Verdict = "gone"
	// VerdictUnknown means the probe could not decide (no PID recorded, a
	// PID the OS refused to answer for, a platform without a probe). Unknown
	// is never upgraded to "crash": the caller must stay silent instead.
	VerdictUnknown Verdict = "unknown"
)

// DeletedSuffix is what the kernel appends to a /proc/<pid>/{cwd,exe} link whose
// target has been unlinked ("/path (deleted)"). It is spelled HERE, once, for
// every reader that resolves a procfs link into a path (Rule 14: a value shared
// across packages gets one definition in a leaf package, never a mirror).
//
// Why it matters: an unstripped suffix makes the path unusable as a path — a
// directory used as an ownership proof, a session dir, a cwd comparison — and
// the failure is silent (the path simply does not exist), which is exactly the
// shape Rule 15 warns about.
const DeletedSuffix = " (deleted)"

// Alive reports whether the process identified by pid still exists.
//
// Return-value contract (the whole point of the three-valued Verdict):
//   - pid <= 0 means "no PID was recorded" → VerdictUnknown, never Gone.
//     An absent PID is missing evidence, not evidence of death.
//   - A probe that cannot answer (permission, unsupported platform) →
//     VerdictUnknown.
//   - Only a definite "no such process" answer → VerdictGone.
//
// A process that has TERMINATED but has not yet been reaped (a zombie, /proc
// state Z, or the transient dead state X) answers signal 0 while being unable
// to run again, so it is VerdictGone: reporting it alive is what makes a caller
// treat a dead process as a live one — the exact over-claim this three-valued
// verdict exists to prevent. The state clause is best-effort: when /proc cannot
// be read the signal-0 answer stands, so the probe only ever downgrades
// alive→gone, never the reverse.
func Alive(pid int) Verdict {
	if pid <= 0 {
		return VerdictUnknown
	}
	return alive(pid)
}
