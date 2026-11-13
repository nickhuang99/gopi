package tools

// task_briefing_run_alternatives_partial.go — THE PARTIAL-MATCH SEAM: an
// acceptance command whose `-run` pattern reaches SOME test, but not all of the
// gate tests it was written to cover.
//
// WHY THIS IS A SEPARATE ARM FROM ITS SIBLING (the epic's own measured case).
// task_briefing_run_alternatives.go stamps a pattern whose alternative reaches
// NO test function — the `[no tests to run]` shape, which `go test` at least
// prints a marker for. The case that made this epic is one rung quieter and one
// rung MORE dangerous:
//
//	$ go test ./internal/standards/ -run 'Rule13|Rule16|NamedLimits|ErrorWrap' -count=1 -v
//	=== RUN   TestGate1ErrorWrapRedProof
//	--- PASS: TestGate1ErrorWrapRedProof (0.00s)
//	ok  	github.com/nick/gopi/internal/standards	0.736s
//
// `-run` matches TEST FUNCTION NAMES, not rule numbers. Of the 222 top-level
// test functions in that package, that pattern reached exactly ONE
// (`ErrorWrap` matched a name); the three rule-numbered alternatives reached
// nothing. The run really executed a test, really passed, and really exit=0 — so
// nothing printed `no tests to run`, the sibling's zero-reach arm is silent by
// construction, and `scripts/test-budget.sh`'s exit 5 (NO TEST EXECUTED) cannot
// fire either: the filter matched something. The Rule 13 red was never touched
// while the console read green.
//
// WHAT THIS ARM MEASURES. For every `go test <pkg> -run '<A|B|C>'` a briefing
// writes as an acceptance command, it splits the pattern into Go's own
// alternatives and counts, per alternative, how many top-level test functions in
// the named package it reaches — the SAME census the sibling already takes
// (runAltPatternsIn / runAltPackageDir / runAltTestNames, called directly rather
// than re-parsed from rendered text). The finding is the PARTIAL shape: at least
// one alternative reaches nothing AND at least one reaches something.
//
// THE TWO HALVES OF THE DEFECT ARE KEPT APART ON PURPOSE (Rule 15). This arm
// never fires on the all-dead pattern (that is the sibling's, which says
// `no tests to run`), and the sibling never fires on the partial one. A single
// predicate that reported both under one label would tell a reader "nothing ran"
// about a run that DID run — which is the same conflation, mirrored.
//
// WHAT IT MAY NOT CONCLUDE. It never says the acceptance criterion was met or
// unmet; it says ONE alternative of the selector reaches no test in this ground,
// so that half of the selector cannot run and the green it reads carries only
// the other half. A pattern whose alternatives ALL reach a test stamps nothing —
// the ordinary case, and the reason this arm cannot degrade the briefings it
// touches.
//
// NO NEW MECHANISM (Rule 35): no subprocess, no script, no resident anything —
// one clause on a seam that already exists (the dispatch seam, beside its six
// siblings), reading this ground's own test files with the standard library.
// ADDITIVE by the same contract: best-effort, never errors, never blocks a run,
// silent on a selector that is wholly live, and idempotent (it recognises its own
// stamp).

import (
	"fmt"
	"regexp"
	"strings"
)

// TaskBriefingRunAltPartialLabel heads the partial-match clause, spelled once
// and exported for the same reason as its siblings (Rules 1/13/14): the
// producer, the dispatch seam's record and the regression tests read ONE string.
const TaskBriefingRunAltPartialLabel = "[Briefing acceptance — a `go test -run` pattern in this briefing is PARTIALLY live: some of its alternatives reach NO test function, so the green it prints covers only the rest]"

const (
	// runAltPartialStampMarker is how this arm recognises its own earlier stamp
	// (idempotence: the dispatch seam renders a briefing once, but a briefing
	// that already carries this clause must not be stamped twice).
	runAltPartialStampMarker = "is PARTIALLY live"

	// runAltPartialAlreadyStampedText is the record for the idempotent arm — a
	// statement that nothing was judged, which is never a pass (Rule 15).
	runAltPartialAlreadyStampedText = "the briefing already carries a partial-match stamp (an earlier pass appended it); no pattern was judged and nothing was appended"
	// TaskBriefingRunAltPartialAlreadyStampedExcerpt is quoted in the record when
	// this clause meets its own stamp a second time.
	TaskBriefingRunAltPartialAlreadyStampedExcerpt = "the briefing already carries a partial-match stamp"

	// TaskBriefingRunAltPartialVerifyOnly is the instruction the clause carries:
	// the same word its sibling uses, because the repair is the same act —
	// replace the dead half with a name that exists, or drop it.
	TaskBriefingRunAltPartialVerifyOnly = "REPLACE THE DEAD ALTERNATIVES WITH THE TESTS THAT EXIST, OR DROP THEM FROM THE SELECTOR"
)

const (
	// runAltPartialRecordFmt is the CENSUS line, one per alternative of a
	// PARTIALLY live pattern: the count per alternative, so a zero is visible
	// against the live siblings it travelled with (the honest minimum — a
	// verdict with no census is a number nobody can check).
	// NOTE: this line carries NO verdict word. Every judged alternative prints one
	// — live ones included — so a verdict in it would report a WHOLLY LIVE
	// selector as a finding (measured: the positive-control leg read "PARTIAL" on
	// a pattern where every alternative reaches a test). The verdict lives in the
	// clause, which is emitted for the partial shape alone.
	runAltPartialRecordFmt = "briefing -run census → pattern %q in %s: alternative %q reaches %d of %d top-level test function(s)\n"
	// runAltPartialHeadFmt introduces the clause. Its substitutions are the
	// label and how many patterns were partially live.
	runAltPartialHeadFmt = "%s\n" +
		"%d `-run` pattern(s) in this briefing are PARTIALLY live: `-run` matches TEST FUNCTION NAMES, not rule numbers, so an alternative naming a rule reaches nothing while its siblings run — and the command still prints `ok … PASS` and exits 0, so no `no tests to run` marker and no exit-5 emptiness verdict ever appears.\n"
	// runAltPartialClaimFmt is the per-pattern block: the pattern, the package,
	// every DEAD alternative with its zero count, the live ones, the instruction,
	// the denial of the completion reading, and the quoted briefing line.
	runAltPartialClaimFmt = "- `go test %s -run %q`: %d of %d alternative(s) reach NO test function in %s — DEAD: %s; LIVE: %s.\n" +
		"  %s: run `grep -n '^%s' %s/*_test.go` HERE to read the names that exist, then re-write the selector with names that do — or drop the dead alternatives.\n" +
		"  This clause is NOT evidence that the work is done, and the green it reads is NOT a reading of the package.\n" +
		"  briefing line: %q\n"
	// runAltPartialNoLiveFmt states the shape where NO alternative is live: it
	// cannot happen in this arm (that is the sibling's all-dead finding) and if
	// it ever did, saying so is better than printing an empty LIVE list.
	runAltPartialNoLiveFmt = "(none — every alternative is dead; see the dead-alternative clause)"
	// runAltPartialRepairFmt is the PASTEABLE repair: the same command reduced to
	// its SURVIVING alternatives in the author's order, so the reader is handed a
	// selector that cannot print a green over an unrun gate.
	runAltPartialRepairFmt = "  REPLACEMENT (paste this — every alternative in it reaches a test): go test %s -run '%s'\n"
	// runAltPartialUnresolvedRecordFmt is the stated arm: a pattern this clause
	// could not judge, never a pass.
	runAltPartialUnresolvedRecordFmt = "briefing -run census → UNMEASURED (partial-match arm: %s) — the pattern was NOT judged: %s\n"
)

// TaskBriefingRunAltPartialRefresh judges every `go test <pkg> -run '<pattern>'`
// the briefing writes and returns the clause to APPEND plus the census record
// lines, for the patterns that are PARTIALLY live.
//
// Returns ("", nil) when the briefing names no `-run` pattern, or when every
// pattern it names is either wholly live or wholly dead — the two ordinary
// cases, and the reason this arm is silent on nearly every briefing.
func TaskBriefingRunAltPartialRefresh(repo, briefing string) (clause string, record []string) {
	if strings.Contains(briefing, runAltPartialStampMarker) {
		return "", []string{fmt.Sprintf(runAltPartialUnresolvedRecordFmt,
			runAltPartialAlreadyStampedText, TaskBriefingRunAltPartialAlreadyStampedExcerpt)}
	}
	patterns := runAltPatternsIn(briefing)
	if len(patterns) == 0 {
		return "", nil
	}
	r := strings.TrimSpace(repo)
	type partial struct {
		pkg       string
		pattern   string
		dead      []string
		live      []string
		total     int
		names     int
		line      string
		lineShown bool
	}
	var found []partial
	printedLine := map[string]bool{}
	for i, p := range patterns {
		if i >= runAltMaxPatterns {
			record = append(record, fmt.Sprintf(runAltPartialUnresolvedRecordFmt, runAltTooManyReason, p.line))
			continue
		}
		if r == "" {
			record = append(record, fmt.Sprintf(runAltPartialUnresolvedRecordFmt, runAltNoRepoReason, p.line))
			continue
		}
		if p.pkg == "" {
			record = append(record, fmt.Sprintf(runAltPartialUnresolvedRecordFmt, runAltNoPackageReason, p.line))
			continue
		}
		dir, reason := runAltPackageDir(r, p.pkg)
		if reason != "" {
			record = append(record, fmt.Sprintf(runAltPartialUnresolvedRecordFmt, reason, p.line))
			continue
		}
		names, reason := runAltTestNames(dir)
		if reason != "" {
			record = append(record, fmt.Sprintf(runAltPartialUnresolvedRecordFmt, reason, p.line))
			continue
		}
		alternatives := strings.Split(p.pattern, runAltAlternativeSeparator)
		if len(alternatives) > runAltMaxAlternatives {
			record = append(record, fmt.Sprintf(runAltPartialUnresolvedRecordFmt, runAltTooManyReason, p.line))
			continue
		}
		one := partial{pkg: p.pkg, pattern: p.pattern, names: len(names), line: p.line}
		judged := 0
		for _, alt := range alternatives {
			a := strings.TrimSpace(alt)
			if a == "" {
				continue
			}
			// A subtest selector is judged by its TOP-LEVEL half: the part before
			// Go's `/` names a test function this source scan can count, while the
			// subtest half is invisible without running the test (the sibling
			// states the same limit in its own record).
			top := a
			if idx := strings.Index(a, runAltSubtestSeparator); idx >= 0 {
				top = a[:idx]
			}
			re, err := regexp.Compile(top)
			if err != nil {
				record = append(record, fmt.Sprintf(runAltPartialUnresolvedRecordFmt,
					fmt.Sprintf("%s: %v", runAltBadRegexpReason, err), p.line))
				continue
			}
			judged++
			n := 0
			for _, name := range names {
				// Go matches `-run` UNANCHORED against the test name, so a fragment
				// matches here exactly as it does there.
				if re.MatchString(name) {
					n++
				}
			}
			record = append(record, fmt.Sprintf(runAltPartialRecordFmt, p.pattern, p.pkg, a, n, len(names)))
			if n == 0 {
				one.dead = append(one.dead, a)
				continue
			}
			one.live = append(one.live, a)
		}
		one.total = judged
		// THE PARTIAL SHAPE, and only it: at least one dead AND at least one live.
		// All-dead belongs to the sibling arm (it can rely on Go's own marker);
		// all-live is the ordinary honest selector.
		if len(one.dead) == 0 || len(one.live) == 0 {
			continue
		}
		one.lineShown = printedLine[p.line]
		printedLine[p.line] = true
		found = append(found, one)
	}
	if len(found) == 0 {
		return "", record
	}
	var b strings.Builder
	fmt.Fprintf(&b, runAltPartialHeadFmt, TaskBriefingRunAltPartialLabel, len(found))
	for _, d := range found {
		pkgDir := strings.TrimSuffix(d.pkg, "/")
		live := strings.Join(d.live, mergeCheckAbstainSeparator)
		if len(d.live) == 0 {
			live = runAltPartialNoLiveFmt
		}
		fmt.Fprintf(&b, runAltPartialClaimFmt,
			d.pkg, d.pattern, len(d.dead), d.total, d.pkg,
			strings.Join(d.dead, mergeCheckAbstainSeparator), live,
			TaskBriefingRunAltPartialVerifyOnly, runAltTestFuncPrefix, pkgDir,
			d.line)
		if d.lineShown {
			fmt.Fprint(&b, taskBriefingRunAltLineRepeatNote)
		}
		fmt.Fprintf(&b, runAltPartialRepairFmt, pkgDir, strings.Join(d.live, runAltAlternativeSeparator))
	}
	return strings.TrimRight(b.String(), "\n"), record
}
