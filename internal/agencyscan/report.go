package agencyscan

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// reportSizeTail bounds the family-size histogram's printed tail: every
// bucket above it is folded into one ">=N+1" entry.
const reportSizeTail = 8

// Report renders one REPORT-ONLY pass as operator-facing text. It is the
// historical-replay face (epic agency-proposals): the same miner, the same
// thresholds, no write — so "what would this have proposed?" is answerable
// over the real ledger without touching it. It is also the human half of the
// idempotence proof: run it twice and the candidate list is byte-identical.
func Report(ctx context.Context, opts Options) (ScanResult, error) {
	opts.Submit = false
	return Scan(ctx, opts)
}

// RenderReport formats a scan result for the terminal (one block per
// candidate with its evidence).
func RenderReport(res ScanResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "agency scan: %d row(s) examined, %d candidate pattern(s)\n",
		res.Scanned, len(res.Candidates))
	if len(res.FamilySizes) > 0 {
		var sizes []int
		for k := range res.FamilySizes {
			sizes = append(sizes, k)
		}
		sort.Ints(sizes)
		var parts []string
		for _, k := range sizes {
			v := res.FamilySizes[k]
			// Below the gate: collapse the singleton tail into one bucket —
			// it is the "no pattern here" mass, not a candidate surface.
			if k > reportSizeTail {
				parts = append(parts, fmt.Sprintf(">=%d:%d", reportSizeTail+1, v))
				break
			}
			parts = append(parts, fmt.Sprintf("%d:%d", k, v))
		}
		fmt.Fprintf(&b, "task-family sizes (size:families): %s\n", strings.Join(parts, " "))
	}
	if len(res.Candidates) == 0 {
		b.WriteString("no pattern met the thresholds — nothing would be proposed.\n")
		return b.String()
	}
	for i, c := range res.Candidates {
		summary, _, ok := renderSubmission(c)
		fmt.Fprintf(&b, "\n[%d] %s  (%s)\n", i+1, c.Kind, c.RecurrenceKey)
		if ok {
			fmt.Fprintf(&b, "    would enqueue: %s\n", summary)
		} else {
			b.WriteString("    would enqueue: (REFUSED — headline over its budget)\n")
		}
		fmt.Fprintf(&b, "    count=%d failed=%d\n", c.Count, c.FailedCount)
		for j, e := range c.Family {
			if j >= maxEvidenceRows {
				fmt.Fprintf(&b, "    … %d more row(s)\n", len(c.Family)-maxEvidenceRows)
				break
			}
			fmt.Fprintf(&b, "      %s\n", oneLine(e))
		}
	}
	fmt.Fprintf(&b, "\nReport-only: nothing was written. Enable writes with %s=1.\n", "GOPI_AGENCY")
	return b.String()
}
