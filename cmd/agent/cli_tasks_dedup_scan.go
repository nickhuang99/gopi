package main

// The twin-ROW audit — `gopi task dedup-scan`.
//
// WHY IT LIVES IN A FILE OF ITS OWN. Its implementation shipped inside
// cmd/agent/cli_memsync.go, the node→node PATCH channel, because that is where
// the verb was FIRST homed (`gopi memory memsync --dedup-scan`) before the
// 2026-09-15 cli-help-dedup restructure re-homed it to the ledger group. The
// patch channel is deleted with the cross-node sync family; this verb is not
// part of that family and never was — it audits ROWS of THIS node's ledger
// (payload hash / canonical hash / trigram similarity) and reads no peer, opens
// no ssh leg and moves nothing. Deleting it with the file that happened to
// contain it would remove a local ledger read because a neighbouring mechanism
// went away, which is exactly the mistake Rule 43's recipe exists to prevent
// (`grep -c` the symbol across the tree, then read the hits — the judgement is
// the reference distribution, never the file name).
//
// The audit's engine, tools.TaskDedupScan, is untouched by this removal.

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/tools"
)

// taskDedupScanProg is the canonical spelling of the twin-task audit (the
// ledger's own group, which is where the verb dispatches from).
const taskDedupScanProg = "gopi task dedup-scan"

// taskDedupScanExitTwinsFound is the visible exit for scripted and
// serve-tick consumers: a twin pair is a FINDING, not an error, so the audit
// must be distinguishable from a clean run without parsing its text (Rule 1:
// an exit code with a meaning is a named value, not a bare 2).
const taskDedupScanExitTwinsFound = 2

// runTaskDedupScan runs the audit for the ledger at dbPath and prints the twin
// pairs. --live narrows the population to claimable rows.
func runTaskDedupScan(fs *memoryFlagSet, dbPath string) {
	stats, err := tools.TaskDedupScan(dbPath, fs.has("live"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", taskDedupScanProg, err)
		os.Exit(1)
	}
	fmt.Printf("%s: %d rows scanned, %d twin pair(s)\n", CliVerbTask, stats.Scanned, len(stats.Pairs))
	for _, p := range stats.Pairs {
		fmt.Printf("  TWIN %s vs %s (gate=%s)\n", p.TaskIDA, p.TaskIDB, p.Gate)
	}
	if len(stats.Pairs) > 0 {
		os.Exit(taskDedupScanExitTwinsFound) // twins found — visible exit for scripted/serve-tick consumers
	}
}
