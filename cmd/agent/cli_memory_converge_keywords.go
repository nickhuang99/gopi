// doc-class: normative
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nickhuang99/gopi/internal/tools"
)

// memoryConvergeKeywordsVerb is the sub-verb word dispatched by
// `gopi memory <sub>` for the one-shot keywords-tail convergence. It is a
// constant because it is a CLI spelling compared in more than one place
// (Rule 1b/14).
const memoryConvergeKeywordsVerb = "converge-keywords"

// keywordsTailConvergeMaxHistBucket bounds the histogram rendering: the shape
// this migration is about has at most a handful of blocks per row, and a row
// that somehow carries hundreds is reported by its own count rather than by
// hundreds of lines. Named rather than inlined (Rule 1).
const keywordsTailConvergeMaxHistBucket = 10

// runMemoryConvergeKeywords implements `gopi memory converge-keywords`.
//
// WHY THIS VERB EXISTS. memoryStoreRow appends the "keywords: …" tail to the
// answer on every keyword update, and until commit 7139b28 it appended WITHOUT
// first stripping the tail already there. Each keyword update therefore left the
// previous tail behind, so a row edited N times carries N consecutive tail
// blocks. The writer is fixed; the rows it already wrote are not, and nothing in
// the read path can tell a stale tail from a current one — it is a DATA
// migration, which is exactly what this verb is.
//
// WHY IT IS A SUB-VERB AND NOT A SCRIPT (Rule 35). The alternative — a script
// beside the binary — would be a second implementation of "what a keywords tail
// is", free to drift from the writer's own keywordsTailRe / keywordsTailMarker,
// and a new persistent artifact whose lifecycle nobody owns. The capability
// belongs to gopi, so it lives in gopi's Go code, reachable also as
// `gopi tool memory converge-keywords` (the tool surface dispatches here), and
// it is invoked EXPLICITLY by whoever upgraded the binary: nothing schedules it,
// and there is no daemon, cron line, state file or lock.
//
// WHAT IT WRITES. Exactly one column — distilled_qa.answer — and only for rows
// carrying 2+ tail blocks, in ONE transaction. Prose, tier, confidence, the usage
// counters and the timestamps are read-only inputs, never outputs. It is
// idempotent: a second run changes nothing, so an interrupted run is repaired by
// running it again.
//
// The before/after histograms are printed from the SAME generator
// (tools.KeywordsTailHistogram) so the report cannot measure its own effect with
// a different instrument than the one that found the defect (Rule 38).
//
// -h is a real run of this surface (Rule 31: a changed surface is executed), and
// --dry-run reports the same scan/change split without writing.
func runMemoryConvergeKeywords(args []string) {
	fs := flag.NewFlagSet(memoryConvergeKeywordsVerb, flag.ExitOnError)
	dry := fs.Bool("dry-run", false, "report what would change; write nothing")
	dbFlag := fs.String("db", "", "ledger path (default: the resolved profile ledger)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	ctx := context.Background()

	path := memoryLedgerEffectivePath(*dbFlag)
	// THE LEDGER GUARD (cli_memory_ledger_guard.go): --dry-run is a read-only
	// mode of this verb, so an explicitly named --db that is not there is
	// refused by name instead of being answered "nothing to converge" about a
	// file this run would have created. A non-dry run is a WRITER and keeps the
	// ability to create, but says so.
	if *dry {
		if refusal := memoryLedgerReadOnlyRefusal(memoryDisplayName(memoryConvergeKeywordsVerb), *dbFlag); refusal != "" {
			fmt.Fprint(os.Stderr, refusal)
			os.Exit(1)
		}
	}
	// Computed before the open (the only moment the absence is observable),
	// printed after it succeeded — a "created" sentence must not outlive a run
	// that then failed.
	createdNotice := ""
	if !*dry {
		createdNotice = memoryLedgerWriterNotice(memoryDisplayName(memoryConvergeKeywordsVerb), *dbFlag, path)
	}
	db, err := tools.OpenMemoryLedger(path, *dry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memory %s: open %s: %v\n", memoryConvergeKeywordsVerb, path, err)
		os.Exit(1)
	}
	if createdNotice != "" {
		fmt.Fprintln(os.Stderr, createdNotice)
	}
	defer db.Close() //stylegate:ignore process-lifetime ledger handle closed on return

	rep, err := tools.KeywordsTailConverge(ctx, db, *dry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memory %s: %v\n", memoryConvergeKeywordsVerb, err)
		os.Exit(1)
	}
	fmt.Print(renderKeywordsTailConverge(rep, path))
}

// renderKeywordsTailConverge renders one run's measurement. Split out so it is
// unit-testable without a process (no os.Exit), like runMemoryProvenance.
func renderKeywordsTailConverge(rep tools.KeywordsTailConvergeReport, path string) string {
	mode := "APPLIED"
	if rep.DryRun {
		mode = "DRY-RUN (nothing written)"
	}
	out := fmt.Sprintf("memory %s: %s — db=%s\n", memoryConvergeKeywordsVerb, mode, path)
	out += "before " + renderKeywordsTailHistogram(rep.Before) + "\n"
	if rep.After != nil {
		out += "after  " + renderKeywordsTailHistogram(rep.After) + "\n"
	}
	out += fmt.Sprintf(
		"rows scanned=%d affected=%d changed=%d already-converged=%d\n",
		rep.Scanned, rep.Affected, rep.Changed, rep.Skipped)
	out += fmt.Sprintf(
		"kept block = LAST; rows where the FIRST block differs (choice changes text)=%d; kept block == search_kw=%d; interleaved-tail rows=%d\n",
		rep.FirstDiffers, rep.LastMatchesSearchKW, rep.InterleavedRows)
	return out
}

// renderKeywordsTailHistogram prints a bucket map in ascending key order with
// every bucket present, so two runs' lines are directly diffable and a bucket
// that falls to zero is visible rather than absent.
func renderKeywordsTailHistogram(hist map[int]int) string {
	if hist == nil {
		return "(no histogram)"
	}
	keys := make([]int, 0, len(hist))
	max := 0
	for k := range hist {
		keys = append(keys, k)
		if k > max {
			max = k
		}
	}
	sort.Ints(keys)
	if max > keywordsTailConvergeMaxHistBucket {
		max = keywordsTailConvergeMaxHistBucket
	}
	parts := make([]string, 0, max+1)
	for k := 0; k <= max; k++ {
		parts = append(parts, fmt.Sprintf("%d:%d", k, hist[k]))
	}
	render := "{" + strings.Join(parts, ", ") + "}"
	for _, k := range keys {
		if k > max {
			render += fmt.Sprintf(" [+ bucket %d:%d]", k, hist[k])
		}
	}
	return render
}
