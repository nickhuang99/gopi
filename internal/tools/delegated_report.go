package tools

// delegated_report.go — the SEAL's half of the result-delegation check: when a
// report hands a gate's conclusion to a path instead of stating it, the
// SENTENCE is recorded and each path it names is MEASURED at that moment
// (does it exist? how big is it?).
//
// THE MEASURED CASE (row 01a12d6b, 2026-10-12). A run's done-gate chain is
// build → tests → write the report → task(action=commit/review), so "the report
// is written and the row is complete" and "the acceptance command has no verdict
// yet" are the SAME state. That run's `## Verification` carried:
//
//	- **Tests**: `make test-budget PKGS='./internal/testutil'` → see the stream at
//	  `artifacts/make-testbudget-testutil.txt`.
//
// The acceptance command afterwards exited 2 with two red tests, and the report
// was already written — the delegation is the only place its conclusion lives,
// and nothing at close-out looked at it. The build → tests → report → commit
// order means the LAST measurement of a run can always be the one the report
// never saw; this record is what makes that state VISIBLE instead of invisible.
//
// THE ACTION IS (A) RECORD — never a rewrite, never a refusal — the discipline
// gateclaim.go, runcheck.go and master_claim_report.go established, and here the
// reason is measured twice over: the predicate is a reading of prose, and this
// project's own corpus (369 reports under ~/.gopi/sessions on 2026-10-12) holds
// only TWO delegating sentences — the motivating one and one sibling
// ("→ 见 `artifacts/26-budget.txt`（最终读数）"). A refusal keyed on so narrow a
// shape would still be a refusal keyed on prose; a RECORD costs a reader one
// line and destroys nothing (Rule 15: no silent failure, no silent rewrite).
//
// THE FILESYSTEM HALF LIVES HERE, not in the leaf: internal/taskreport owns the
// reading of prose and the STATUS VOCABULARY (DelegatedPathStatusAbsent /
// DelegatedPathStatusPresent / DelegatedPathStatusUnreadable); this file owns
// the meas-ure-ment, so the seal's digest and `task show`'s render can never
// describe the same path differently.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/taskreport"
)

// TaskShowReportDelegatedLabel is the label of the result-delegation finding in
// `task show`: the sentences that hand a gate's conclusion to a path, with each
// path's status read at the same moment the seal recorded it. Exported for the
// same reason as the sibling labels — the CLI test asserts the operator-visible
// line, and the renderer and the test share ONE definition of it (Rule 14).
const TaskShowReportDelegatedLabel = "task_report_delegated: "

// directoryPathPrefix is the spelling a delegated path can carry that names a
// DIRECTORY (or the top of the tree) rather than a file: like every other
// path-shape constant here, the spelling is taken from the one definition
// (filepath.Separator) instead of a bare literal (Rules 1b/13).
const directoryPathPrefix = string(filepath.Separator)

// delegatedAbsolutePathPrefix is the leading marker of a path that does not
// resolve against the session directory (POSIX absolute).
const delegatedAbsolutePathPrefix = "/"

// taskReportDelegatedRecord adjudicates the sealed body's result delegations
// and returns the digest fragment naming the sentences that hand a conclusion to
// a path, with each path's status measured right here ("" when the report
// delegates nothing). The stderr warning is emitted HERE, so the operator reads
// the finding at the moment the artifact of record is closed out.
//
// reportPath is the report's own absolute path — the base a delegated `artifacts/…`
// path is relative to, since a report's sibling directory is where its evidence
// lives (taskreport.TaskReportPath's contract).
func taskReportDelegatedRecord(taskID, reportPath, body string) string {
	verdict := taskreport.DelegatedClaimsIn(body)
	if len(verdict.Claims) == 0 {
		return ""
	}
	status := delegatedPathStatuses(verdict, filepath.Dir(reportPath))
	record := taskreport.DelegatedRecord(verdict, status)
	for _, line := range strings.Split(strings.TrimRight(taskreport.DelegatedWarning(taskID, verdict, status), "\n"), "\n") {
		fmt.Fprintf(os.Stderr, "%s%s\n", taskReportClaimWarnPrefix, line)
	}
	return record
}

// delegatedPathStatuses measures every path the verdict names, from the run's
// session directory, ONCE per path (a path named by two sentences is one
// measurement, not two).
func delegatedPathStatuses(v taskreport.DelegatedClaims, baseDir string) map[string]string {
	status := map[string]string{}
	failed := map[string]error{}
	for _, c := range v.Claims {
		for _, p := range c.Paths {
			if _, done := status[p]; done {
				continue
			}
			if err, done := failed[p]; done {
				// Reported already: the FIRST failure is what put this path in
				// the status map, and the status rendering carries that error —
				// this second copy is retained only to short-circuit the retry.
				_ = err //stylegate:ignore the status map already carries this same error to the caller; this copy only marks the path as attempted
				continue
			}
			abs, err := delegatedPathResolve(p, baseDir)
			if err != nil {
				status[p] = taskreport.DelegatedPathStatusUnreadable(err)
				failed[p] = err
				continue
			}
			info, err := os.Stat(abs)
			switch {
			case err == nil && info.IsDir():
				// A DIRECTORY is not a conclusion: the delegation names a place,
				// and the record says so rather than claiming a result is there.
				status[p] = taskreport.DelegatedPathStatusUnreadable(
					fmt.Errorf("%s is a directory, not an artifact", abs))
			case err == nil:
				status[p] = taskreport.DelegatedPathStatusPresent(info.Size())
			case os.IsNotExist(err):
				status[p] = taskreport.DelegatedPathStatusAbsent
			default:
				status[p] = taskreport.DelegatedPathStatusUnreadable(err)
			}
		}
	}
	return status
}

// taskReportReadTimeStatusNote is appended to the delegation label on the READ
// face (`task show`), where the status beside each path was measured at the
// moment the operator is reading — never at seal time. Saying which measurement
// a status is keeps the digest's record and the read face's render from being
// read as the same fact (Rule 38: a figure names when it was taken).
const taskReportReadTimeStatusNote = " (paths stat'd NOW, at read time)"

// delegatedPathStatusNow measures ONE delegated path for the read face and
// returns the status line (the same vocabulary the seal's digest uses, from the
// leaf — never a second spelling).
func delegatedPathStatusNow(token, baseDir string) string {
	st := delegatedPathStatuses(taskreport.DelegatedClaims{
		Claims: []taskreport.DelegatedClaim{{Paths: []string{token}}},
	}, baseDir)
	return st[token]
}

// delegatedPathResolve turns a delegated path token into an absolute path: a
// POSIX-absolute token stands as it is, everything else is relative to the
// run's session directory (where the report and its `artifacts/` siblings
// live). A `~/` token is NOT resolved — guessing a home directory for a path a
// report wrote is the kind of invention this record must not contain — so the
// caller records it as unmeasurable instead of measuring the wrong file.
func delegatedPathResolve(token, baseDir string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("empty path token")
	}
	if strings.HasPrefix(token, delegatedAbsolutePathPrefix) {
		return token, nil
	}
	if strings.HasPrefix(token, directoryPathPrefix) {
		return token, nil
	}
	if strings.HasPrefix(token, taskreport.DelegatedPathHomePrefix) {
		return "", fmt.Errorf("%s is spelled against a home directory the report does not own", token)
	}
	if baseDir == "" {
		return "", fmt.Errorf("the report's own directory could not be resolved")
	}
	return filepath.Join(baseDir, token), nil
}
