// sandbox_reclaim_audit.go — the ONE place a sandbox reclaim becomes a
// removal-audit record.
//
// WHY THE WRITER LIVES HERE AND NOT IN internal/scratch: the sweep is what knows
// which directories it deleted, but internal/scratch cannot emit the record —
// internal/removalaudit imports internal/config, which imports internal/scratch,
// so an emit from the sweep would close an import cycle. The two CALLERS (the
// `gopi tool scratch-gc sandboxes` face and the resident tick) therefore write
// it, through this shared helper, so the two channels cannot drift in what they
// record — only in the ARM, which is the whole point of having two arms.
//
// WHAT IT ANSWERS, and why each field is there: "who deleted this sandbox, and
// on what grounds" asked AFTER the directory is gone. Path + bytes are what was
// destroyed; reason is the plan's criterion VERBATIM (never re-worded, so the
// record and the plan cannot disagree about why); evidence is the ownership
// facts the plan compared against the process table; the arm names the CHANNEL.
//
// Rule 15: this is best-effort and must never fail the deletion it explains.
// removalaudit's emit contract already swallows its own I/O errors (a sink it
// cannot write is reported on stderr by that package, never returned), so a
// failure to record can never turn a completed reclaim into a failed one — the
// directory is already gone by the time this is called.
package main

import (
	"fmt"
	"time"

	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// sandboxReclaimAgeSecond is the age precision the evidence string renders at.
// Sub-second precision would make two readers' greps of the same record differ
// on a rounding that carries no information (Rule 1: a named value, not a bare
// literal inside the format string).
const sandboxReclaimAgeSecond = time.Second

// emitSandboxReclaimAudit writes ONE audit record per directory this sweep
// DELETED. arm names the channel that drove the sweep.
//
// It is called only after a sweep that actually ran — a dry run never reaches
// here, because a dry run deletes nothing and a record for a surviving directory
// would claim a reclaim that did not happen.
func emitSandboxReclaimAudit(arm removalaudit.Arm, root string, res scratch.SandboxSweepResult) {
	for _, r := range res.Reclaimed {
		removalaudit.SandboxRemove(arm, root, r.Path, r.Bytes, r.Reason,
			sandboxReclaimEvidence(r))
	}
}

// sandboxReclaimEvidence renders the ownership facts the plan judged the
// directory on, in the same vocabulary the plan's own reason sentence uses
// (outcome word, pid, kernel start ticks) so `--explain` and the sink can be
// reconciled line for line.
//
// A directory with NO owner marker says so by name rather than by an empty
// field: "no marker" and "marker present, owner dead" are different findings,
// and collapsing them would make the record unable to explain why a sandbox was
// collected early.
func sandboxReclaimEvidence(r scratch.SandboxReclaimed) string {
	age := (r.Age / sandboxReclaimAgeSecond) * sandboxReclaimAgeSecond
	if !r.HasOwner {
		return fmt.Sprintf("age=%s owner=(none declared — no marker record)", age)
	}
	return fmt.Sprintf("age=%s owner=pid:%d/start:%d outcome=%s cmd=%q",
		age, r.OwnerPID, r.OwnerStart, r.OwnerOutcome, r.OwnerCmd)
}
