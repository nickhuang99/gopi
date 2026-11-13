package tools

// task_briefing_path_set.go — the FOURTH measurement this dispatch seam takes:
// a briefing that PINS A PATH SET together with its count ("12 行: paths/a.md,
// paths/b.md …") and then writes an acceptance criterion against that set.
//
// THE MEASURED GAP (row 01a1447c-253e-74a2-8329-1c37447c9e59's briefing,
// 2026-10-16 — the row this epic was filed from). Its briefing pinned twelve
// paths as the stored-cell drift population of devdoc/DOC-INDEX.tsv and made
// "exactly these twelve rows change" an ACCEPTANCE criterion. Measured on that
// row's own base:
//
//   - ten of the twelve were ALREADY compliant — a sibling commit (cc9c1fee8,
//     authored 18:16, merged 19:34) had re-derived every one of them, while the
//     set the briefing pinned was a snapshot the PREVIOUS row took at 19:11;
//   - the tree's real drift population that day was a DIFFERENT set of twelve
//     (of which two were CLASS MOVES, which no cell re-derivation can clear).
//
// So the criterion was unreachable as written, and the dispatched run paid to
// discover that. THREE SHIPPED SCANNERS ARE BLIND TO THIS SHAPE, measured on the
// row's own briefing text (probe in this run's session artifacts): the figure
// clause returned 0 (the text counts in CHINESE — "12 行", "12 条" — and names no
// English unit beside a digit), the gate-assertion scanner returned 0 (no RED
// verdict word), and the unlanded scanner returned 0 (nothing asserts the branch
// lacks a revision). The BASE clause DID stamp — the text declares `base 1c5b086`
// — but its instruction is generic ("run THAT surface again") and knows nothing
// about a SET: it cannot say that the pinned population is itself the moving part.
//
// WHAT THIS CLAUSE MEASURES, AND WHAT IT DELIBERATELY DOES NOT.
//
// It does NOT run the surface the set came from. Re-deriving a path set means
// running the classifier over the corpus — measured at ~40 s for a handful of
// paths and minutes for the whole tree — and this seam runs on EVERY dispatch,
// before the child's first token. A dispatch path that spends a classifier pass
// on every briefing would be a cost paid by every run to serve a rare shape, so
// the clause states the instruction and the command shape instead, and says in
// its own words that no measurement was taken here (Rule 15: an unmeasured arm
// is STATED, never silently passed).
//
// What it DOES measure is cheap and exact, and it is the half the child cannot
// get wrong: WHICH of the pinned paths this ground can even speak about. A path
// that resolves outside the run's own ground is named as such, because a pinned
// path from another checkout is not a population of this tree.
//
// It says RE-MEASURE and never completion: a set that has moved means the
// briefing's population was read at another revision, NOT that the work is done
// — the very confusion this clause exists to prevent (the base clause's
// doctrine, one level down).
//
// ADDITIVE by the same contract as its four siblings: best-effort, never errors,
// never blocks a claim, silent on the ordinary briefing that pins no path set,
// and every set examined leaves a line in the record — the positive control (a
// set whose paths all resolve inside the ground) included.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/taskreport"
)

// TaskBriefingPathSetLabel heads the pinned-set clause, spelled once and exported
// for the same reason as its four siblings (Rules 1/13/14): the producer, the
// spawn seam's record and the regression tests read ONE string.
const TaskBriefingPathSetLabel = "[Briefing freshness — a PINNED PATH SET this briefing names was NOT re-measured at dispatch]"

// TaskBriefingPathSetVerifyOnly is the instruction the clause carries. It is a
// third, DIFFERENT spelling from its siblings' on purpose, and the difference is
// the whole point: the base clause sends the child back to a revision, the figure
// clause back to a command's output, and this one back to a SET — the child must
// compare two sets before it repairs anything, because the set it was handed is
// the part that may already be gone.
const TaskBriefingPathSetVerifyOnly = "RE-MEASURE THE SET BEFORE WORKING OFF IT"

const (
	// taskBriefingPathSetRecordFmt is the record line for a pinned set this seam
	// examined: the count the briefing states, the paths it could resolve inside
	// this ground, and the paths it could not. One spelling.
	taskBriefingPathSetRecordFmt = "briefing path set → the briefing pins %d %s naming %d path(s) (%d resolve inside this ground, %d do not) — NOT re-measured at dispatch, so the clause carries the instruction and the command shape (%s)\n"
	// taskBriefingPathSetUnresolvedRecordFmt is the record line for a set whose
	// paths ALL resolve outside this ground: there is nothing about this tree in
	// it. It is a stated arm, never a silent skip (Rule 15).
	taskBriefingPathSetUnresolvedRecordFmt = "briefing path set → UNMEASURED (%s) — the pinned set was NOT examined, which is not a pass: %s\n"
	// taskBriefingPathSetAlreadyStampedReason is the stated reason the seam writes
	// when it measures a briefing that already carries this clause: nothing was
	// appended and the earlier instruction stands (Rule 15 — and the reason this
	// seam, like its siblings, is idempotent).
	taskBriefingPathSetAlreadyStampedReason = "the briefing already carries a path-set stamp (an earlier pass appended it); no set was examined and nothing was appended"
)

// taskBriefingPathSetNoGroundReason is the stated reason a set could not be
// examined at all: there is no ground to resolve its paths against, so the seam
// cannot even say which paths belong to this tree.
const taskBriefingPathSetNoGroundReason = "there is no ground to resolve the pinned paths against"

// taskBriefingPathSetHeadFmt introduces the clause. Its substitution is the
// number of sets examined. Note what the head says and what it refuses to say:
// the seam did NOT measure the set — it says so, and then gives the child the
// instruction it would have carried had the measurement been affordable.
const taskBriefingPathSetHeadFmt = "%s\n" +
	"This briefing pins %d population(s) of PATHS — a count AND the paths it counts — and the count is written as an acceptance criterion. A pinned set is a SNAPSHOT OF A LIVE POPULATION: it was read at some revision, and the paths that are red TODAY can be a DIFFERENT set. THIS SEAM DID NOT RE-MEASURE IT FOR YOU (re-deriving a set costs a full classifier pass, which this dispatch path does not spend), so the arms below are stated, not measured.\n"

// taskBriefingPathSetClaimFmt is the clause's per-set block: the figure with the
// author's own unit, the paths, what a moved set means, the instruction, the
// command shape, the two things that must be re-read before repairing, the
// explicit denial of the completion reading, and the quoted line. Its
// substitutions are the count, the unit, the paths, the count again (the
// instruction's object), the instruction constant, and the briefing line.
const taskBriefingPathSetClaimFmt = "- the briefing states %d %s and names these path(s): %s\n" +
	"  A SET THAT HAS MOVED IS NOT A SET THAT IS FINE, and it is not a set that will still be there: some of these paths may ALREADY be compliant (a sibling landing between the snapshot and this dispatch is the measured cause), while the paths that are drifted NOW may be different ones. Two consequences the briefing's own acceptance criterion cannot survive unexamined: a per-path re-derivation is only valid for a path that is STILL drifted, and a path whose CLASS moved cannot be cleared by re-deriving its cells at all (the class cell is a decision, and flipping it alone reddens the banner gate).\n" +
	"  %s: run the surface that produced this set in its REPORT-ONLY mode HERE, quote its output, and DIFF its set against the %d path(s) above. Repair only what still reproduces; if the surface reports an empty set, report that instead of repairing it — and change the acceptance count to the number this ground measures, never to the number the briefing states.\n" +
	"  This clause is NOT evidence that the work is done.\n" +
	"  briefing line: %q\n"

// taskBriefingPathSetOutsideReason is the stated reason a pinned set could not be
// examined at all: EVERY path in it lies outside this ground, so the whole
// population belongs to another checkout and this tree cannot answer for any of
// its rows. It is deliberately not the figure clause's per-path reason
// (taskBriefingCountOutsideReason): that one refuses ONE token, this one refuses
// a whole SET, and the two statements are not the same claim.
const taskBriefingPathSetOutsideReason = "every path the set names resolves outside this ground, so this tree cannot answer for any of them"

// taskBriefingPathSetAlreadyStampedExcerpt fills the already-stamped record's
// sentence field: there is no set to quote — the stamp IS the subject.
const taskBriefingPathSetAlreadyStampedExcerpt = "(this briefing's own previously appended path-set stamp)"

// taskBriefingPathSetOutsideNoteFmt is the per-set note a set whose SOME paths
// resolve outside this ground carries. It is rendered inside the claim block,
// where the child reads it, because a pinned path from another checkout is not a
// population of THIS tree and the child must not treat the count as one.
const taskBriefingPathSetOutsideNoteFmt = "  NOTE: %d of these path(s) do not resolve inside this ground, so this tree cannot answer for them (the briefing's set came from another checkout, or names a path this ground does not carry).\n"

// taskBriefingPathSetElidedFmt states the paths this clause's own bound left out,
// in the child's own view of the set — an elision nobody can see is a set the
// child believes it was given whole (Rule 15).
const taskBriefingPathSetElidedFmt = "%s (+%d more, elided by this clause's own bound)"

// taskBriefingPathSetMaxPaths bounds how many paths this clause renders for one
// set. The stamp is prepended to a child's prompt, and a hundred-path list pushed
// into it would be the truncation hazard the seam's own devdoc measures — so the
// bound is named and the elision is STATED (Rule 1 / Rule 15).
const taskBriefingPathSetMaxPaths = 12

// TaskBriefingPathSetRefresh examines, at dispatch time, every path population a
// briefing pins with a count, and returns the clause to APPEND to the briefing the
// child receives plus the record lines for every set examined.
//
// It is the FOURTH sibling in this file's family and lives in the same seam for
// the same reason: this is the ONE spawn seam every executor face funnels
// through, and the judgement of what a pinned set MEANS is not re-implemented
// here — the SHAPE is internal/taskreport's (PathSetClaimsIn), and the ground
// comparison reuses the figure clause's ONE path resolver.
//
// Returns ("", nil) when the briefing pins no such set — the ordinary case, and
// the reason this cannot degrade every briefing it touches.
func TaskBriefingPathSetRefresh(repo, briefing string) (clause string, record []string) {
	if i := strings.Index(briefing, TaskBriefingPathSetLabel); i >= 0 {
		return "", []string{fmt.Sprintf(taskBriefingPathSetUnresolvedRecordFmt,
			taskBriefingPathSetAlreadyStampedReason, taskBriefingPathSetAlreadyStampedExcerpt)}
	}
	claims := taskreport.PathSetClaimsIn(briefing)
	if len(claims) == 0 {
		return "", nil
	}
	r := strings.TrimSpace(repo)
	var stamped []taskreport.PathSetClaim
	outside := map[int]int{}
	for _, c := range claims {
		excerpt := taskreport.PathSetClaimLineExcerpt(c.Line)
		if r == "" {
			record = append(record, fmt.Sprintf(taskBriefingPathSetUnresolvedRecordFmt,
				taskBriefingPathSetNoGroundReason, excerpt))
			continue
		}
		inside := 0
		for _, p := range c.Paths {
			if pathSetGroundCarries(r, p) {
				inside++
			}
		}
		if inside == 0 {
			// THE STATED ARM: a set whose every path lies outside this ground is
			// a population of another checkout. Nothing is stamped — there is
			// nothing about THIS tree in it — and the reason is recorded, so this
			// is distinguishable from a briefing that pins no set at all.
			record = append(record, fmt.Sprintf(taskBriefingPathSetUnresolvedRecordFmt,
				taskBriefingPathSetOutsideReason, excerpt))
			continue
		}
		outside[len(stamped)] = len(c.Paths) - inside
		record = append(record, fmt.Sprintf(taskBriefingPathSetRecordFmt,
			c.Count, c.Unit, len(c.Paths), inside, len(c.Paths)-inside, TaskBriefingPathSetVerifyOnly))
		stamped = append(stamped, c)
	}
	if len(stamped) == 0 {
		return "", record
	}
	var b strings.Builder
	fmt.Fprintf(&b, taskBriefingPathSetHeadFmt, TaskBriefingPathSetLabel, len(stamped))
	for i, s := range stamped {
		fmt.Fprintf(&b, taskBriefingPathSetClaimFmt,
			s.Count, s.Unit, TaskBriefingPathSetPathsText(s.Paths),
			TaskBriefingPathSetVerifyOnly, s.Count,
			taskreport.PathSetClaimLineExcerpt(s.Line))
		if n := outside[i]; n > 0 {
			fmt.Fprintf(&b, taskBriefingPathSetOutsideNoteFmt, n)
		}
	}
	return strings.TrimRight(b.String(), "\n"), record
}

// pathSetGroundCarries reports whether this ground can answer for one pinned
// path — the ONLY measurement this clause takes (its header says so). It is an
// EXISTENCE question, not a lexical one, and deliberately so: a pinned path that
// this tree does not carry is not a population of this tree, whatever it looks
// like — a set snapshotted in another checkout, or a path since renamed. The
// resolution reuses the figure clause's ONE resolver (task_briefing_count_claim.go,
// taskBriefingCountPath) so the two clauses cannot disagree about what "inside this
// ground" means (Rules 8/13/14).
func pathSetGroundCarries(repo, path string) bool {
	abs, reason := taskBriefingCountPath(repo, path)
	if reason != "" {
		return false
	}
	_, err := os.Stat(abs)
	return err == nil
}

// TaskBriefingPathSetPathsText renders the pinned paths for the child in the
// author's own spelling, bounded by taskBriefingPathSetMaxPaths and stating the
// elision when it truncates.
func TaskBriefingPathSetPathsText(paths []string) string {
	shown := paths
	elided := 0
	if len(shown) > taskBriefingPathSetMaxPaths {
		elided = len(shown) - taskBriefingPathSetMaxPaths
		shown = shown[:taskBriefingPathSetMaxPaths]
	}
	text := strings.Join(shown, ", ")
	if elided > 0 {
		text = fmt.Sprintf(taskBriefingPathSetElidedFmt, text, elided)
	}
	return text
}
