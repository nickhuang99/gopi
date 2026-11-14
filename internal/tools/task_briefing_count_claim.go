package tools

// task_briefing_count_claim.go — the THIRD measurement the dispatch seam takes:
// a briefing's quoted POPULATION FIGURE ("the table carries 168 rows"), read
// against THIS GROUND's own tree before the briefing reaches the child.
//
// THE MEASURED GAP (epic 01a137cb, 2026-10-14, from row 01a13781's incident).
// That row's briefing reported a MEASUREMENT — "internal/docclass/testdata/
// anchor-number-baseline.tsv carries **168 rows** while the tree produces 167",
// reddening three internal/docclass tests — and named the revision its
// measurement was taken at (base bbf8d9760). The seam re-measured the DECLARATION
// and stamped 978 characters: the base was 45 commits behind the integration tip.
// So the deployment gap (below) is not the whole story — this class of briefing
// IS covered at the base-declaration end.
//
// WHAT IS NOT COVERED, and this file is the measured answer to it: the seam's base
// clause fires on the DISTANCE between the declared base and the tip, and its own
// positive control deliberately stamps NOTHING when that distance is zero
// (`if ahead == 0 { … continue }` — a guard that stamped every freshly-cut
// briefing would be noise). So a briefing whose base declaration is CURRENT — or
// which declares no base at all — while its quoted figure has gone stale reaches
// the child with no re-measure instruction, and the child pays to re-derive a
// number that is already green.
//
// The figure itself has no shape predicate anywhere in internal/taskreport (that
// package's scanners read revisions beside a base word, or assertion phrases:
// "never landed", "not an ancestor"). One was added for this file —
// taskreport.CountClaimsIn — and its bounds were TUNED BY MEASUREMENT over the
// operator's own ledger (400 most recent payloads, 2026-10-14: 8 firings → 5 → 3
// → 1 as the bounds landed, the surviving one a true figure). The tuning is in
// that file's comments; the verdict it produced is here, in what this seam may
// CONCLUDE.
//
// WHAT THIS MEASUREMENT IS, AND WHAT IT IS NOT — the load-bearing distinction:
//
//   - It reads a LINE COUNT. That is the one population figure this process can
//     re-derive without running the surface the briefing ran, and it is exact for
//     the measured shape (the incident's table: 168 rows recorded vs 167
//     produced; the wrapper count behind it is the line count).
//   - It therefore does NOT re-measure a figrure whose unit is not a line
//     ("252 anchor rows scanned"). Calling a re-derived LINE COUNT that answer
//     would be a false claim of exactly the kind this whole seam exists to
//     prevent, so the clause states what it counted, names the surface the child
//     must run, and never asserts the briefing's defect is gone.
//   - It says RE-MEASURE, never VERIFY ONLY, and never completion: a figure that
//     differs says the number was taken in ANOTHER tree at ANOTHER revision.
//     (The base clause's own doctrine, unchanged, one level down.)
//
// It is ADDITIVE by the same contract as the two clauses above it in this file's
// neighbours: best-effort, never errors, never blocks a run, silent on the
// ordinary briefing that quotes no population figure, and every claim it
// examined leaves a line in the record — INCLUDING the positive control (a figure
// that agrees) and the arms that measured nothing (Rule 15).

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/taskreport"
)

// TaskBriefingCountLabel heads the figure clause, spelled once and exported for
// the same reason as its two siblings (Rules 1/13/14): the producer, the spawn
// seam's record and the regression tests read ONE string.
const TaskBriefingCountLabel = "[Briefing freshness — a population FIGURE this briefing quotes was RE-MEASURED at dispatch]"

// TaskBriefingCountVerifyOnly is the instruction the figure clause carries. It
// names the only honest consequence of a figure that no longer matches: the
// number was measured elsewhere, so the surface that produced it must be run
// again HERE. It is deliberately a DIFFERENT spelling from the base clause's
// instruction: that one sends the child back to a git revision, this one to a
// command's output.
const TaskBriefingCountVerifyOnly = "RE-MEASURE THE FIGURE BEFORE REPAIRING"

const (
	// taskBriefingCountMovedRecordFmt / ...AgreesRecordFmt / ...UnresolvedRecordFmt
	// are the record lines for every figure examined: the STAMPED one, the
	// positive control (a figure that agrees with this ground, which by design
	// stamps nothing), and the arms that measured nothing. One spelling each.
	taskBriefingCountMovedRecordFmt      = "briefing figure → %s carries %d line(s) in THIS ground while the briefing states %d — STAMPED into the child's briefing (%s)\n"
	taskBriefingCountAgreesRecordFmt     = "briefing figure → %s carries %d line(s) in THIS ground, which agrees with the %d the briefing states — the figure stands and nothing is stamped\n"
	taskBriefingCountUnresolvedRecordFmt = "briefing figure → UNMEASURED (%s) — the figure the briefing quotes was NOT re-measured, which is not a pass: %s\n"
)

// taskBriefingCountReasons are the stated reasons a figure could not be
// re-measured. Named, because a bare sentence at a fmt site is the shape Rule 1
// forbids and because the record is the only place a reader learns WHY.
const (
	taskBriefingCountNoRepoReason     = "there is no repo to re-measure the figure in"
	taskBriefingCountOutsideReason    = "the path the figure names resolves outside this ground, so this run cannot re-measure it"
	taskBriefingCountUnreadableReason = "the file the figure names could not be counted here"
	taskBriefingCountTooLargeReason   = "the file the figure names is larger than this seam will read to count it"
	// taskBriefingCountEllipsis spells the abbreviation a briefing uses when it
	// truncates a path (".../anchor-number-baseline.tsv"): it names a file by
	// SUFFIX, and a suffix is not a path this ground can resolve — so it is
	// stated as unmeasured rather than guessed at by suffix search.
	taskBriefingCountEllipsis = "..."
	// taskBriefingCountAlreadyStampedReason is the stated reason the seam writes
	// when it measures a briefing that already carries this clause: nothing was
	// appended and the earlier measurement stands (Rule 15 — and the reason this
	// seam, like its siblings, is idempotent).
	taskBriefingCountAlreadyStampedReason = "the briefing already carries a figure stamp (an earlier pass appended it); no figure was re-measured and nothing was appended"
)

// taskBriefingCountHeadFmt introduces the clause. Its substitutions are the
// label, the number of figures re-measured, and — deliberately absent — nothing
// that could read as a verdict: the head states that the numbers now read
// differently, and each block below says what to do about it.
const taskBriefingCountHeadFmt = "%s\n" +
	"This briefing quotes %d population figure(s) about file(s) in this repo, and each was RE-MEASURED JUST NOW, in THIS RUN's own ground, before the briefing was handed to you — and this ground reads a DIFFERENT number for every one of them.\n"

// taskBriefingCountClaimFmt is the clause's per-figure block: the file, the
// figure the briefing states with the unit the author used, what this ground
// reads, the instruction, the explicit denial of the completion reading, and the
// quoted line. Its substitutions are the file (twice: the subject and the
// instruction's object), the briefing's figure, its unit, this ground's count,
// the measurement sentence, the instruction constant, and the briefing line.
const taskBriefingCountClaimFmt = "- %s: this briefing states %d %s; THIS GROUND reads %d line(s) %s.\n" +
	"  %s: the figure was measured in another tree at another revision. Run the surface that produced it HERE, quote its output, and use THIS ground's number — a repair driven by the briefing's figure would be sized for a tree that no longer exists.\n" +
	"  This clause is NOT evidence that the work is done.\n" +
	"  briefing line: %q\n"

const (
	// taskBriefingCountLineMaxBytes caps how large a file this seam will read to
	// count it. A "row count" figure is a claim about a table or a doc, and a
	// figure quoted from a multi-megabyte file is not something this seam can
	// settle cheaply on the dispatch path — so the bound is named and the arm is
	// STATED, never silently skipped (Rule 1 / Rule 15).
	taskBriefingCountLineMaxBytes = 4 << 20
	// taskBriefingCountMaxFiles caps how many files this clause will count. The
	// stamp is prepended to a child's prompt, and a briefing with a systemic
	// figure problem must not push it past what a reader reads.
	taskBriefingCountMaxFiles = 4
)

// TaskBriefingCountRefresh re-measures, at dispatch time, every population
// figure a briefing quotes about a file in the run's own ground, and returns the
// clause to APPEND to the briefing the child receives plus the record lines for
// every figure examined.
//
// It is the THIRD sibling in this file's family and lives in the same seam for
// the same reason: this is the ONE spawn seam every executor face funnels
// through, the measurement is taken against the run's OWN ground, and the
// judgement is not re-implemented here — the SHAPES are internal/taskreport's
// (CountClaimsIn), and the counting is this file's own one reader.
//
// Returns ("", nil) when the briefing quotes no such figure — the ordinary case,
// and the reason this cannot degrade every briefing it touches.
func TaskBriefingCountRefresh(repo, briefing string) (clause string, record []string) {
	if i := strings.Index(briefing, TaskBriefingCountLabel); i >= 0 {
		return "", []string{fmt.Sprintf(taskBriefingCountUnresolvedRecordFmt,
			taskBriefingCountAlreadyStampedReason, taskBriefingCountAlreadyStampedExcerpt)}
	}
	claims := taskreport.CountClaimsIn(briefing)
	if len(claims) == 0 {
		return "", nil
	}
	r := strings.TrimSpace(repo)
	seen := map[string]bool{}
	type moved struct {
		claim taskreport.CountClaim
		count int
	}
	var stamped []moved
	for _, c := range claims {
		excerpt := taskreport.CountClaimLineExcerpt(c.Line)
		if seen[c.File] {
			continue
		}
		seen[c.File] = true
		if len(stamped)+len(record) >= taskBriefingCountMaxFiles {
			break
		}
		path, reason := taskBriefingCountPath(r, c.File)
		if reason != "" {
			record = append(record, fmt.Sprintf(taskBriefingCountUnresolvedRecordFmt, reason, excerpt))
			continue
		}
		n, err := countFileLines(path)
		if err != nil {
			record = append(record, fmt.Sprintf(taskBriefingCountUnresolvedRecordFmt,
				fmt.Sprintf("%s: %v", taskBriefingCountUnreadableReason, err), excerpt))
			continue
		}
		if n == c.Count {
			// THE POSITIVE CONTROL, in the wild: this ground reads the number
			// the briefing states, so the figure is current and nothing is
			// stamped.
			record = append(record, fmt.Sprintf(taskBriefingCountAgreesRecordFmt, c.File, n, c.Count))
			continue
		}
		record = append(record, fmt.Sprintf(taskBriefingCountMovedRecordFmt, c.File, n, c.Count, c.File))
		stamped = append(stamped, moved{claim: c, count: n})
	}
	if len(stamped) == 0 {
		return "", record
	}
	var b strings.Builder
	fmt.Fprintf(&b, taskBriefingCountHeadFmt, TaskBriefingCountLabel, len(stamped))
	for _, s := range stamped {
		fmt.Fprintf(&b, taskBriefingCountClaimFmt,
			s.claim.File, s.claim.Count, s.claim.Unit, s.count,
			taskBriefingCountMeasurementSentence(s.count),
			TaskBriefingCountVerifyOnly,
			taskreport.CountClaimLineExcerpt(s.claim.Line))
	}
	return strings.TrimRight(b.String(), "\n"), record
}

// taskBriefingCountMeasurementSentence states what was counted, so the child is
// never left to infer it from a bare number — and so the clause cannot be read as
// having re-derived a figure this seam did not re-derive.
func taskBriefingCountMeasurementSentence(n int) string {
	return fmt.Sprintf("(counted with a newline count of the whole file, which is %d)", n)
}

// taskBriefingCountPath resolves the file token a figure named to an absolute
// path inside the ground. It returns ("", reason) when the token cannot name one,
// and the reason is always a stated one — the arm is never silent (Rule 15).
func taskBriefingCountPath(repo, file string) (string, string) {
	if repo == "" {
		return "", taskBriefingCountNoRepoReason
	}
	// A path abbreviated with an ellipsis names a file by SUFFIX (".../anchor-
	// number-baseline.tsv"). Only the leading form is unambiguous enough to
	// resolve (strip it, and the rest is a path); a middle ellipsis is left to the
	// child rather than guessed at by suffix search, which could match a
	// different file with the same name.
	tok := strings.TrimSpace(file)
	if strings.HasPrefix(tok, taskBriefingCountEllipsis+"/") {
		tok = strings.TrimPrefix(tok, taskBriefingCountEllipsis+"/")
	}
	if tok == "" || strings.Contains(tok, taskBriefingCountEllipsis) {
		return "", taskBriefingCountOutsideReason
	}
	root, err := filepath.Abs(repo)
	if err != nil {
		return "", taskBriefingCountNoRepoReason
	}
	abs := tok
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs = filepath.Clean(abs)
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", taskBriefingCountOutsideReason
	}
	return abs, ""
}

// countFileLines counts the newlines of the file at abs, refusing anything
// larger than taskBriefingCountLineMaxBytes — the ONE reader this seam counts
// with, so the number in the stamp and the number in the record cannot disagree.
//
// An EMPTY file is 0 lines, and a file whose last line carries no newline is
// counted for that line too: this is a line count of the whole file, and the
// figure it is compared against ("the table carries 168 rows") is the shape a
// reader confirms with `wc -l` plus one.
func countFileLines(abs string) (int, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, fmt.Errorf("%s is a directory, not a file", abs)
	}
	if info.Size() > taskBriefingCountLineMaxBytes {
		return 0, fmt.Errorf("%s: %s", taskBriefingCountTooLargeReason, abs)
	}
	f, err := os.Open(abs)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, scannerInitBytes), taskBriefingCountLineMaxBytes)
	n := 0
	for scanner.Scan() {
		n++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return n, nil
}

// taskBriefingCountAlreadyStampedExcerpt fills the already-stamped record's
// sentence field: there is no figure to quote — the stamp IS the subject.
const taskBriefingCountAlreadyStampedExcerpt = "(this briefing's own previously appended figure stamp)"
