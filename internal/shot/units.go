// units.go — the RECLAMATION face of the capture unit registry: `gopi tool shot`
// starts a transient systemd user unit per capture, registers it, and this file
// is where the registry is read and the orphaned units are stopped.
//
// It exists so the surface has exactly ONE caller-visible entry point
// (ReclaimOrphanedUnits) while the POLICY stays in internal/scratch
// (ReclaimShotUnits: which verdict keeps a unit, and the conservative
// "unmeasurable ⇒ keep") and the TEARDOWN stays here (stopUserUnitErr: the one
// systemctl argv).
//
// WHICH EVENT POINTS FIRE IT (epic shot-reclaim-trigger, Rule 35): the
// reclamation must be REACHABLE ON AN IDLE HOST, so it rides events that
// necessarily happen on any host and adds no daemon, timer, cron line, script,
// config key or schedule:
//
//   - `gopi tool shot` start-up (RenewNotice — a capture is the leak's own source, so
//     every new capture is a free opportunity to sweep the previous ones);
//   - `gopi worker` start-up (runTaskWorker → RenewNotice, before the loop);
//   - `gopi tool scratch-gc` (cmd/agent/cli_scratch_gc.go) — the host-watchdog face.
//     (CORRECTED 2026-10-30, task 01a18ef7: this list also named "the resident
//     worker/merger tick (cmd/agent/scratch_gc_tick.go)"; no resident component
//     reaches that tick any more — worker round 187d05c, merger tick deleted
//     2026-09-19 — so the verb's `--tick` arm is the only door.)
//
// The two start-up triggers are the point of this file's second entry point:
// the previous design assumed "the tick already runs", but a worker
// self-exits after a drained queue (~90s), so a leak that happens on a quiet
// host had NOTHING that would ever reach the sweep (measured 2026-09-27: six
// ACTIVE gopi-shot-* units, oldest ~1h50m, zero chrome reclaimed).
package shot

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// The roles this package hands its callers, so a log line says WHICH event fired
// the sweep without each call site inventing a word (Rules 1b/14). They are the
// roles the observable evidence quotes, so the tests and an acceptance grep read
// the same spellings the code prints.
const (
	// RenewRoleShotStartup labels the sweep fired from `gopi tool shot`'s start-up.
	RenewRoleShotStartup = "shot-startup"
	// RenewRoleWorkerStartup labels the sweep fired from `gopi worker`'s
	// start-up (the loop is entered AFTER this line, so the sweep can never
	// delay or obstruct a claim).
	RenewRoleWorkerStartup = "worker-startup"
)

// ReclaimOrphanedUnits stops every registered capture unit whose creating
// process is provably gone, and removes those registrations.
//
// The liveness question is asked through internal/procname's single identity
// ladder (scratch.ShotUnitCreatorVerdict) — the same one the task ledger asks
// about its own runners — so there is no second probe to drift. A creator that
// is LIVE or UNMEASURABLE keeps its unit; only a measured gone/recycled creator
// authorises the stop, and a stop that fails keeps the registration for the next
// sweep.
//
// procRoot is procname.DefaultProcRoot in production; it is a parameter so a
// test can drive the exact production ladder over a synthetic tree.
func ReclaimOrphanedUnits(procRoot string) (scratch.ShotUnitOutcome, error) {
	dir := scratch.ShotUnitDir()
	return scratch.ReclaimShotUnits(procRoot, dir, stopUserUnitErr)
}

// PlanOrphanedUnits reports what ReclaimOrphanedUnits would do, stopping
// nothing. It is the dry-run face the `gopi tool scratch-gc` report uses, so an
// operator can see a host's orphaned captures before anything is stopped.
func PlanOrphanedUnits(procRoot string) ([]scratch.ShotUnitPlan, error) {
	return scratch.PlanShotUnits(procRoot, scratch.ShotUnitDir())
}

// DefaultProcRoot is the procfs root a production caller reads, re-exported from
// internal/procname (the ONE definition) so cmd/agent's call sites never spell
// the path themselves (Rules 13/14).
const DefaultProcRoot = procname.DefaultProcRoot

// ReportShotUnitReclaim prints the sweep's one-line summary plus the named
// reason for every unit the pass did NOT renew. A kept unit is a fact an
// operator needs to see — silence about it would read as "no orphan existed"
// (Rule 15), and it is exactly the evidence the epic's positive control asks
// for.
//
// ONE report shape for every face (the two start-up triggers, the CLI line and
// the resident tick's log), so a reader never has to learn two vocabularies.
//
// The three counts are SEPARATE because they are three different facts:
// `renewed` (the arm acted and the unit is gone), `denied` (the unit was
// reclaimable and the host refused the stop — it is still active) and `kept`
// (the unit was not reclaimable). Folding denials into keeps would make a failed
// reclamation indistinguishable from a policy keep, which is the Rule 15 defect
// this report exists to avoid.
func ReportShotUnitReclaim(role string, out scratch.ShotUnitOutcome) {
	if out.Scanned == 0 {
		return
	}
	fmt.Printf(scratch.ShotUnitReclaimLogFmt+"\n",
		role, out.Scanned, out.Renewed, out.Denied, out.Kept)
	for _, unit := range out.RenewedUnits {
		fmt.Printf(scratch.ShotUnitRenewedLogFmt+"\n", role, unit)
	}
	for _, denied := range out.DeniedRecords {
		fmt.Printf(scratch.ShotUnitDeniedLogFmt+"\n", role, denied.Unit, denied.Verdict, denied.Reason)
	}
	for _, kept := range out.KeptRecords {
		if IsStopDenied(kept) {
			continue // already named on its own denied line above
		}
		fmt.Printf(scratch.ShotUnitKeptLogFmt+"\n", role, kept.Unit, kept.Verdict, kept.Reason)
	}
	_ = os.Stdout.Sync() //stylegate:ignore flushes stdio at exit; nothing downstream reads the result
}

// IsStopDenied reports whether a kept record is kept because the STOP failed
// (rather than because the policy refused it). A denial is reported on its own
// line, so the keep line must not repeat it — the split is decided by the ONE
// marker internal/scratch appends, never by a second predicate re-derived here.
func IsStopDenied(plan scratch.ShotUnitPlan) bool {
	return strings.HasSuffix(plan.Reason, scratch.ShotUnitStopFailedReason)
}

// ReclaimOrphanedUnitsReporting is the call site the resident tick and the
// `gopi tool scratch-gc` face use. It is RenewNotice under the older name: ONE
// implementation, so a face cannot get a different sweep, a different report or
// a missing denial warning depending on which entry point it was wired to
// (Rules 13/14 — the rename would have been the second copy).
func ReclaimOrphanedUnitsReporting(role string) int {
	return RenewNotice(role)
}

// RenewNotice is the start-up trigger: the ONE call the two new event points
// (`gopi tool shot` start-up, `gopi worker` start-up) make, so the sweep cannot
// behave differently in one of them.
//
// WHY A NOTICE (its own exit status, and a warning on the denial path): an
// orphaned unit is reclaimed with `systemctl --user stop`, which only the USER
// MANAGER can perform. On a host where that is refused (a sandboxed test run, a
// session bus that will not answer), the sweep's own per-unit failure line goes
// to stderr and the counts have to be readable from there — so this function
// WARNS, by number, instead of leaving a caller to infer "denied == still
// running chrome" from a line an operator may not have seen.
//
// It is deliberately SILENT when there is nothing to do: a fresh host's every
// capture and every worker start must not print a zero-count line.
func RenewNotice(role string) int {
	// ONE probe of the registry's own usability, before the sweep: a whole
	// registry this process cannot act on is announced once, naming the path,
	// the reason and the channel (Rule 15). The probe decides nothing — the
	// per-unit verdicts below are unchanged and the sweep still names every unit
	// it keeps or is denied.
	if why := scratch.ShotUnitRegistryDenyReason(scratch.ShotUnitDir()); why != "" {
		fmt.Fprintf(os.Stderr, "[%s] [shot-unit-reclaim] orphaned capture units may go unreclaimed: %s\n", role, why)
	}
	out, err := ReclaimOrphanedUnits(DefaultProcRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] [shot-unit-reclaim] registry unavailable (caller unaffected): %v\n",
			role, err)
		return 0
	}
	ReportShotUnitReclaim(role, out)
	if out.Denied > 0 {
		// A denial is one host-refused `systemctl` per named unit: say what it
		// means, so the number is not read as a cosmetic counter.
		fmt.Fprintf(os.Stderr,
			"[%s] [shot-unit-reclaim] %d orphaned capture unit(s) could not be stopped by this host — "+
				"they are still ACTIVE and their registrations are kept for the next pass\n",
			role, out.Denied)
	}
	return out.Renewed
}
