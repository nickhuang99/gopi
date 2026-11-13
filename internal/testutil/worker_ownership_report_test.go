package testutil

// REGRESSION GUARD for the gate's failure message (Rule 5: every fix ships
// a minimal repro test).
//
// THE DEFECT (2026-09-16, observed while fixing task 01a0a58c): the message
// capped its file listing at workerOwnershipMaxReportedHits and printed a bare
// "… (truncated)". It listed ONE file while the real uncovered set was SIX, so
// the briefing written from that report fixed one sixth of the blast radius —
// the truncation was silent. The count is the half that must never be lost.
//
// THE CONTRACT THIS TEST PINS: with more uncovered files than the cap, the
// message names the TOTAL and how many were withheld; and the env hook prints
// every file. It reads workerOwnershipUncoveredReport directly — the smallest
// sample that exercises the exact rendering the gate emits (no tree fixture,
// no scan: the report IS the subject).

import (
	"fmt"
	"strings"
	"testing"
)

// TestWorkerOwnershipReportDisclosesTheTotalWhenTruncated is the minimal repro
// of the under-report: 26 uncovered files, a 20-file listing — the total must
// appear in the message.
func TestWorkerOwnershipReportDisclosesTheTotalWhenTruncated(t *testing.T) {
	const total = workerOwnershipMaxReportedHits + 6 // one over the cap is enough; 6 mirrors the incident's hidden files

	missing := make([]string, 0, total)
	for i := 0; i < total; i++ {
		missing = append(missing, fmt.Sprintf("devdoc/uncovered-%02d.md", i))
	}

	got := workerOwnershipUncoveredReport(missing)
	if !strings.Contains(got, fmt.Sprintf("%d uncovered file(s)", total)) {
		t.Errorf("the report does not state the TOTAL (%d) — a reader can only see the listing:\n%s", total, got)
	}
	wantWithheld := fmt.Sprintf("listing first %d, %d withheld", workerOwnershipMaxReportedHits, total-workerOwnershipMaxReportedHits)
	if !strings.Contains(got, wantWithheld) {
		t.Errorf("the report does not disclose what was withheld (want %q):\n%s", wantWithheld, got)
	}
	if !strings.Contains(got, workerOwnershipFullListEnvName) {
		t.Errorf("the report does not name the full-list escape hatch %s:\n%s", workerOwnershipFullListEnvName, got)
	}
	if n := strings.Count(got, "devdoc/uncovered-"); n != workerOwnershipMaxReportedHits {
		t.Errorf("the report listed %d files, want the readability cap %d:\n%s", n, workerOwnershipMaxReportedHits, got)
	}
}

// TestWorkerOwnershipReportFullListEscapeHatchPrintsEveryFile pins the second
// half: the count alone is not actionable when 40 files are uncovered, so the
// knob must actually print them all — and it must go through config.EnvBool, so
// a disabled spelling ("0"/"off") stays disabled.
func TestWorkerOwnershipReportFullListEscapeHatchPrintsEveryFile(t *testing.T) {
	const total = workerOwnershipMaxReportedHits + 6
	missing := make([]string, 0, total)
	for i := 0; i < total; i++ {
		missing = append(missing, fmt.Sprintf("devdoc/uncovered-%02d.md", i))
	}

	for _, off := range []string{"", "0", "off", "false"} {
		t.Setenv(workerOwnershipFullListEnvName, off)
		got := workerOwnershipUncoveredReport(missing)
		if n := strings.Count(got, "devdoc/uncovered-"); n != workerOwnershipMaxReportedHits {
			t.Errorf("%s=%q listed %d files, want the cap %d (a disabled value must not enable the full list):\n%s",
				workerOwnershipFullListEnvName, off, n, workerOwnershipMaxReportedHits, got)
		}
	}

	t.Setenv(workerOwnershipFullListEnvName, "1")
	got := workerOwnershipUncoveredReport(missing)
	if n := strings.Count(got, "devdoc/uncovered-"); n != total {
		t.Errorf("%s=1 listed %d files, want every one of %d:\n%s", workerOwnershipFullListEnvName, n, total, got)
	}
	if !strings.Contains(got, fmt.Sprintf("listing all %d", total)) {
		t.Errorf("the full-list report must still state the total (%d):\n%s", total, got)
	}
}

// TestWorkerOwnershipReportAtTheCapWithholdsNothing is the boundary: a set that
// exactly fills the cap is complete, so the message must not claim a
// truncation it did not perform (a false "withheld" would be its own lie).
func TestWorkerOwnershipReportAtTheCapWithholdsNothing(t *testing.T) {
	missing := make([]string, 0, workerOwnershipMaxReportedHits)
	for i := 0; i < workerOwnershipMaxReportedHits; i++ {
		missing = append(missing, fmt.Sprintf("devdoc/uncovered-%02d.md", i))
	}
	got := workerOwnershipUncoveredReport(missing)
	if strings.Contains(got, "withheld") {
		t.Errorf("a complete listing claims withheld files:\n%s", got)
	}
	if n := strings.Count(got, "devdoc/uncovered-"); n != workerOwnershipMaxReportedHits {
		t.Errorf("listed %d files, want %d:\n%s", n, workerOwnershipMaxReportedHits, got)
	}
}
