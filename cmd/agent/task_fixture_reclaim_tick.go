// task_fixture_reclaim_tick.go — the FIXTURE-ROW RECLAIM arm of the upkeep tick.
//
// WHERE IT HANGS, AND WHY THAT IS NOT A NEW DEPENDENT (Rule 35). The arm is
// called from scratchGCTick, i.e. it rides a tick this host ALREADY fires: the
// resident cadence is scratchGCDefaultIntervalMin (60 minutes, off unless
// configured by GOPI_TEST_SCRATCH_GC_MIN / the settings `scratch_gc` switch),
// and the tick's own one-shot face is `gopi tool scratch-gc tick` — which is
// also the face the host-level watchdog unit drives (scripts/systemd/
// gopi-scratch-gc-watchdog.timer, OnUnitActiveSec=30min). No timer, no cron
// line, no config key, no state file and no second cadence is created here; the
// throttle is the tick's.
//
// WHY THIS TICK AND NOT ANOTHER. It is the ONE surviving periodic pass on this
// host whose subject is already "reclaim what nobody will come back for" — the
// task-lifecycle pass that used to carry ledger duties was deleted whole
// (2026-10-30, task_lifecycle_tick.go carries the per-leg measurement), the
// worker's round is dispatch-only by operator ruling, and the merger's pass has
// no production caller. A fixture row outliving its creator is exactly that
// class of residue, and it is bounded by the same sweep the operator already
// trusts to bound everything else.
//
// FAIL-OPEN, like every sibling arm: an unreadable ledger, a row this process
// cannot judge, a losing CAS — each is REPORTED and the tick continues. A
// ledger-hygiene arm must never be able to fail the host task.

package main

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/tools"
)

// fixtureReclaimLogFmt is the ONE spelling of the arm's per-row line (Rule 1b):
// the collected and the kept rows read through the same shape, so an operator
// scanning a log can tell them apart by the verdict word alone.
const fixtureReclaimLogFmt = "[%s] [fixture-reclaim] %s %s creator=%s — %s\n"

// fixtureReclaimRoleLabel is the arm's own label (distinct from the scratch
// sweep's, so one grep of a transcript separates the two duties).
const fixtureReclaimRoleLabel = "fixture-reclaim"

// fixtureReclaimTick runs ONE fixture-reclaim pass and renders its verdicts.
// It returns what it COLLECTED (the count a caller's log reports); the kept
// rows are printed, never silently dropped (Rule 15: a row that was judged and
// left in place must say why, or the arm is indistinguishable from one that
// never ran).
func fixtureReclaimTick(role string) int {
	res, err := tools.FixtureReclaimSweep("")
	if err != nil {
		fmt.Printf(fixtureReclaimLogFmt, role, fixtureReclaimRoleLabel,
			"FAILED", "—", fmt.Sprintf("the pass could not read the ledger (%v) — no row was touched", err))
		return 0
	}
	for _, rec := range res.Cancelled {
		fmt.Printf(fixtureReclaimLogFmt, role, fixtureReclaimRoleLabel, rec.Verdict, rec.Creator, rec.Reason)
	}
	for _, rec := range res.Kept {
		fmt.Printf(fixtureReclaimLogFmt, role, fixtureReclaimRoleLabel, rec.Verdict, orDashToken(rec.Creator), rec.Reason)
	}
	return len(res.Cancelled)
}

// orDashToken renders an empty creator as a word rather than a gap, so a kept
// row that recorded NO creator is legible in the log (the placeholder spelling
// every renderer in this package shares).
func orDashToken(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none recorded)"
	}
	return s
}
