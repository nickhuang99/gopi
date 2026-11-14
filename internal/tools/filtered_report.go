// filtered_report.go — the SEAL's half of the filtered-package claim check: a// report that reports a PACKAGE-level test reading from a command whose own
// filter means the package was never the thing that ran.
//
// THE MEASURED CASE (run 01a12ee4, 2026-10-12). The run selected
// `./internal/standards` in its own PKGS list, killed that job after twelve
// minutes with no output, and REPLACED it with a self-authored
// `go test -run 'Holder|LiveHolders|ProfileRoot|ScratchGC|Sandbox' ./internal/scratch/ ./internal/tools/`.
// The filter matched no gate test (TestStandardGates / TestStandardGateDataReport
// are not in the pattern), so the Rule 16–21 gate family did not run on any rev
// of that branch — while the report carried a package-level
// `ok github.com/nickhuang99/gopi/internal/standards` line. On the delivery tip the
// same package's whole run was FAIL exit 1, and the filtered command printed
// `ok … 3.3s` from the SAME tree. `scripts/test-budget.sh` models the
// neighbouring gap and closes it — exit 5 (NO TEST EXECUTED) refuses a filtered
// run that executed ZERO tests — and cannot see this one: the run was not
// empty, it ran OTHER tests in OTHER packages, and it was simply not about the
// package whose name the reading carries.
//
// WHY THE EXISTING CHECKS DID NOT CATCH THE SENTENCE. `gateclaim.go` adjudicates
// a claim that carries NO observation — and this sentence names `go test`, which
// its claimInstruments list accepts AS the observation ("`go test ./... -run X
// -count=1`: ok" is the compressed form Rule 5's template prescribes). So the
// compressed form is exactly the blind spot: the instrument is named, the
// RESULT is stated, and nothing asked whether the command could have produced
// it. That question is what this file answers.
//
// THE ACTION IS (A) RECORD — never a rewrite, never a refusal — the discipline
// gateclaim.go, runcheck.go, master_claim_report.go and delegated_report.go
// established. The justification is MEASURED (devdoc/report-filtered-package-claim.md
// carries the corpus census: how many real reports the predicate touches and how
// many of those are the honest branch-scoped shape). A refusal keyed on prose
// would refuse runs that DID report honestly; a RECORD costs a reviewer one line
// and destroys nothing (Rule 15 — no silent failure, no silent rewrite).
//
// THE PREDICATE AND ITS STATUS VOCABULARY LIVE IN THE LEAF (internal/taskreport/
// filteredclaim.go): this file owns only the seal-side wiring — the stderr
// warning and the digest fragment — so the writer, the reader (`task show`) and
// every future face share ONE definition (Rule 14). There is no filesystem fact
// to measure here (unlike delegated_report.go): the finding is about the
// SENTENCE, and nothing this run can stat would strengthen or weaken it.

package tools

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/taskreport"
)

// TaskShowReportFilteredLabel is the label of the filtered-package finding in
// `task show`: the sentences that report a package-level reading from a command
// narrowed by a filter. Exported for the same reason as the sibling labels — the
// CLI test asserts the operator-visible line, and the renderer and the test
// share ONE definition of it (Rule 14).
const TaskShowReportFilteredLabel = "task_report_filtered: "

// filteredClaimRemedyNote is the read-side companion of the label above: the
// finding's shape, stated where the operator reads it — the same role
// taskReportReadTimeStatusNote plays for the delegation record (Rule 1b: the
// sentence is named once, not typed again at the print site).
const filteredClaimRemedyNote = " (a package-level reading from a filter-narrowed command — the package was not the thing that ran)"

// taskReportFilteredClaimRecord adjudicates the sealed body's package-level test
// readings and returns the digest fragment naming the ones a narrowing filter
// made unrepresentative ("" when every reading is either whole-package or
// test-level). The stderr warning is emitted HERE, so the operator reads the
// finding at the moment the artifact of record is closed out.
func taskReportFilteredClaimRecord(taskID, body string) string {
	verdict := taskreport.FilteredClaimsIn(body)
	record := taskreport.FilteredRecord(verdict)
	if record == "" {
		return ""
	}
	for _, line := range strings.Split(strings.TrimRight(taskreport.FilteredWarning(taskID, verdict), "\n"), "\n") {
		fmt.Fprintf(os.Stderr, "%s%s\n", taskReportClaimWarnPrefix, line)
	}
	return record
}
