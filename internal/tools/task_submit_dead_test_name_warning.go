package tools

// task_submit_dead_test_name_warning.go — the WRITE FACE of the dead-selector
// defect: a briefing is filed naming a `go test -run <name>` (or a `Test…`
// identifier in prose) that reaches NO test function, and nobody says so at the
// moment the row is written.
//
// WHY THIS FACE AND NOT ANOTHER (the epic's own finding, measured). The defect
// has three moments, and only one of them was unguarded:
//
//	① the row is WRITTEN     — nothing measured the names.  ← this file
//	② the row is DISPATCHED  — tools.TaskBriefingRunAlternativesRefresh (the
//	   sixth clause) and tools.TaskBriefingProseTestNamesRefresh (the seventh)
//	   census the names against the ground's own test corpus and STAMP the dead
//	   ones into the child's briefing. Those two already exist and fire.
//	③ the command is RUN     — scripts/test-budget.sh already turns a filtered
//	   run that executed zero tests into exit 5 with its own banner
//	   (`no_tests_marker`, the empty-run verdict), so the declared entry point
//	   discriminates.
//
// So the birth point — the submit — was the one that told nobody. MEASURED
// consequence (PA, 2026-11-05, four instances in one night): a row was filed
// whose entire acceptance command was `go test ./cmd/agent/ -run
// 'TestResumeSeamProbe'`, a name nothing implements; `go test` answered
// `ok … 0.589s [no tests to run]` and exited 0, and the difference between that
// and a real pass is four milliseconds and a suffix at the END of the `ok` line.
//
// WHAT THIS MEASURES — the SAME two predicates the dispatch seam uses, called
// directly rather than re-derived from their rendered text (a caller that
// re-parses a stamp is the drift BriefingWithClause exists to prevent):
//
//	(a) every `-run` alternative on a line that also names `go test`, resolved
//	    through the package pattern that command names (runAltPatternsIn /
//	    runAltPackageDir / runAltTestNames);
//	(b) every `Test…` identifier written in PROSE (proseTestNamesIn), censused
//	    against this ground's whole test corpus (proseTestCorpus).
//
// Both are reached through the exported wrappers' internals because this file
// lives in the same package — no second implementation, no parallel scanner.
//
// WHAT IT MAY NOT DO. It NEVER refuses the row. A briefing may legitimately name
// a test the child is being asked to WRITE (that is the dispatch seam's own
// stated boundary, and it is why the clause says "or say in the payload that the
// test is yours to write"). A refusal here would block exactly the epic that
// commissions a test — the same reason TaskHermesHostWarning and
// TaskSubmitBaseStalenessWarning warn and never block (Rules 15/40).
//
// SILENCE, stated rather than implied: an unreadable corpus, an unresolvable
// package pattern, or a repo-less submit yields NO warning. "Cannot measure" is
// not evidence of a defect, and a submit confirmation that cried wolf on every
// unresolvable pattern would be trained away by its readers within a day
// (Rule 15: a measurement that could not be taken is not a finding).
//
// NO NEW MECHANISM (Rule 35): no subprocess, no script, no resident anything —
// one constant, one helper, one line at the seam that already renders four
// sibling warnings.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	// TaskSubmitDeadTestNameWarningFmt is the warning TEMPLATE appended to
	// TaskSubmit's confirmation. Its substitutions are the count, the repo, and
	// the rendered list of dead selectors. Callers use
	// TaskSubmitDeadTestNameWarningText, never Sprintf directly (Rules 1/13/14:
	// one spelling, one renderer).
	TaskSubmitDeadTestNameWarningFmt = "warning: the briefing filed here names %d `go test` selector(s) or `Test…` identifier(s) that reach NO test function in %q — %s. `go test -run <such a name>` prints `no tests to run` and exits 0, so an acceptance line built on one can never fail: this row would be executed against a check that measures nothing. Replace each name with the test that exists, or say in the payload that the test is yours to write. The row still queues — a dead selector is a diagnosability defect, never a reason to refuse a dispatch."

	// taskSubmitDeadListSeparator joins the dead selectors in that list. Named
	// because a bare ", " at a fmt site is the shape Rule 1 forbids.
	taskSubmitDeadListSeparator = ", "

	// taskSubmitDeadRunShape labels a dead `-run` ALTERNATIVE (the sixth
	// clause's population) and names the package whose corpus judged it.
	taskSubmitDeadRunShape = "%q (a `-run` alternative of `go test %s`)"

	// taskSubmitDeadProseShape labels a dead identifier written in PROSE (the
	// seventh clause's population).
	taskSubmitDeadProseShape = "%q (written in prose as acceptance evidence)"
)

// TaskSubmitDeadTestNameWarningText renders the write-face warning. One helper
// owns the Sprintf so the template has a single consumer (Rule 13).
func TaskSubmitDeadTestNameWarningText(repo string, dead []string) string {
	return fmt.Sprintf(TaskSubmitDeadTestNameWarningFmt, len(dead), repo,
		strings.Join(dead, taskSubmitDeadListSeparator))
}

// TaskSubmitDeadTestNameWarning reports the warning for a submit whose summary
// and payload are about to be written against `repo`, or "" when there is
// nothing to say.
//
// It reads the SAME columns the sibling warnings read (the summary and the
// payload as they stand after the spill decision), so the write face measures
// what the reader will actually receive.
func TaskSubmitDeadTestNameWarning(repo, summary, payload string) string {
	dead := taskSubmitDeadSelectors(repo, summary+"\n"+payload)
	if len(dead) == 0 {
		return ""
	}
	return TaskSubmitDeadTestNameWarningText(repo, dead)
}

// taskSubmitDeadSelectors is the whole measurement: every selector in `text`
// that reaches no test function in `repo`'s own corpus, rendered as one line
// apiece. It returns nil for every arm it could not measure — see the file
// comment's SILENCE paragraph — so an unmeasurable briefing is quiet rather
// than accused (Rule 15: never a false finding).
func taskSubmitDeadSelectors(repo, text string) []string {
	r := strings.TrimSpace(repo)
	if r == "" || strings.TrimSpace(text) == "" {
		return nil
	}
	var dead []string
	seen := map[string]bool{}
	add := func(key, rendered string) {
		if seen[key] {
			return
		}
		seen[key] = true
		dead = append(dead, rendered)
	}
	// (a) THE `-run` POPULATION — the measured incident's own shape.
	for _, p := range runAltPatternsIn(text) {
		if p.pkg == "" {
			continue
		}
		dir, reason := runAltPackageDir(r, p.pkg)
		if reason != "" {
			continue
		}
		names, reason := runAltTestNames(dir)
		if reason != "" {
			continue
		}
		for _, alt := range strings.Split(p.pattern, runAltAlternativeSeparator) {
			a := strings.TrimSpace(alt)
			if a == "" {
				continue
			}
			// The subtest tail is not visible in a source scan (the sixth
			// clause states the same limit); only the top-level function is
			// judged here.
			top := a
			if i := strings.Index(a, runAltSubtestSeparator); i >= 0 {
				top = a[:i]
			}
			re, err := regexp.Compile(top)
			if err != nil {
				// An alternative that is not a Go regexp is a defect this
				// warning is not about; `go test` refuses it loudly by itself.
				continue
			}
			hits := 0
			for _, name := range names {
				// UNANCHORED, exactly as `go test -run` matches (the sixth
				// clause's own reading): a strict prefix of a live name
				// SELECTS that test, so it is live.
				if re.MatchString(name) {
					hits++
				}
			}
			if hits > 0 {
				continue
			}
			add("run\x00"+a+"\x00"+p.pkg,
				fmt.Sprintf(taskSubmitDeadRunShape, a, strings.TrimSuffix(p.pkg, "/")))
		}
	}
	// (b) THE PROSE POPULATION — the seventh clause's shape, one level over.
	// proseTestNamesIn already hands `-run`-line names to (a), so no name is
	// judged twice.
	named, _ := proseTestNamesIn(text)
	if len(named) > 0 {
		names, _, reason := proseTestCorpus(r)
		if reason == "" {
			for _, n := range named {
				re, err := regexp.Compile(n.name)
				if err != nil {
					continue
				}
				hits := 0
				for _, real := range names {
					if re.MatchString(real) {
						hits++
					}
				}
				if hits > 0 {
					continue
				}
				add("prose\x00"+n.name, fmt.Sprintf(taskSubmitDeadProseShape, n.name))
			}
		}
	}
	// SORTED, so the confirmation a re-submit produces is the same text (a
	// warning that reorders itself reads as a different finding).
	sort.Strings(dead)
	return dead
}
