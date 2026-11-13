package tools

import (
	"context"

	"github.com/nick/gopi/internal/agencyscan"
)

// agencyscan wiring (epic agency-proposals).
//
// agencyscan is a LEAF package: its mining rules are pure functions over the
// ledger (internal/agencyscan/agencyscan.go). It must not import internal/tools
// — so the ONE thing it cannot own, the write, is injected HERE, where the
// ledger's SQL and TaskSubmit already live. This is the whole coupling: one
// seam, no shared state, no new table, no new store.
func init() {
	agencyscan.SetSubmitFunc(func(req agencyscan.SubmitRequest) (string, error) {
		return TaskSubmit(TaskSubmitOptions{
			Summary: req.Summary,
			Payload: req.Payload,
			Tags:    req.Tags,
			Source:  req.Source,
		})
	})
}

// QuarantineTagSet is the "never auto-claim" tag set the queue honours, in the
// comma form TaskNextFiltered takes. One definition (Rule 14): the worker's
// exclusion list, the CLI's improvements face and any future claimer must never
// disagree about what is quarantined. A proposal row carries it, which is what
// keeps this pass from becoming an auto-dispatch machine.
const QuarantineTagSet = agencyscan.TagQuiet

// TaskImprovementReport runs the agency miner in REPORT-ONLY mode and renders
// the candidates for an operator or a test harness. It NEVER writes: the write
// path is gated behind the GOPI_AGENCY switch and reached only by the task-end
// hook.
//
// This is also the epic's historical-replay harness: running it over the real
// ledger prints exactly what the pass would have proposed, which is how the
// real-problem-vs-noise verdict in devdoc/agency-proposals.md was reached.
func TaskImprovementReport(minFamilySize int) (agencyscan.ScanResult, error) {
	return agencyscan.Report(context.Background(), agencyscan.Options{MinFamilySize: minFamilySize})
}
