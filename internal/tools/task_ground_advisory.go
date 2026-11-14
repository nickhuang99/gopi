package tools

// task_ground_advisory.go — the READ-ONLY advisory ground line of
// `gopi task show` (epic 01a0bb4d follow-up, gap B).
//
// WHAT THE ROW RECORDS vs WHAT IS TRUE NOW. `task show` renders the deliverable
// block field-wise out of the row's OWN blob (deliverable.repo / branch /
// worktree). That blob is the ADOPTABLE field: the merger, the sweeper and the
// reclaim arms read it, and a bound worktree is an OWNERSHIP claim on a
// directory. It is therefore written only by the seams that have EVIDENCE (the
// claim stamp, the creation seam, the terminal writeback) and, by contract, it
// is never re-pointed once it names a worktree (taskBackfillDeliverableWorktreeAt
// — the 2026-09-12 foreign-owner hijack).
//
// A row that recorded NOTHING — the measured 2-of-131 in-flight shape, whose
// ground exists and is readable but whose claim/creation seam never fired —
// therefore shows no ground at all, and an operator reading `task show` cannot
// tell "this run has no ground" from "this run HAS a ground we never recorded".
//
// THE LINE: a clearly-labelled, READ-ONLY advisory that resolves the ground
// from the row's OWN session through the EXISTING readers (the shared session
// reading in task_ground_session_reading.go — taskGroundSessionReadingFor: the
// session JSONL scan the terminal writeback uses, taskWorkdirFromSession, the
// cost-aware taskFindSessionJSONLForRow lookup, and taskBranchExists as the
// VERIFICATION). It is advisory by construction:
//
//   - it NEVER writes — no UPDATE, no stamp, no event; the recorded deliverable
//     is exactly what it was before the show (the regression test asserts the
//     raw blob AND the row's status are identical across a show);
//   - it never fails the show — every failure mode below is rendered as a WHY
//     on the line (Rule 15), never as an error return and never as silence;
//   - it is rendered for EVERY row that has anything to resolve (in-flight and
//     terminal alike), because a terminal row's recorded blob is exactly as
//     frozen as an in-flight one — and it stays SILENT only when the row
//     records nothing AND has no session to resolve from (there is no reading
//     and no question to answer; a line there would be an invention).

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/devworkspace"
)

// ── the AUTHORITY clause: what the run's briefing says about its tree ────────
//
// SAME READING, SECOND CONSUMER (epic 01a0db25). The advisory below answers
// "where is this run's ground". The clause here answers the question the same
// measured defect raised from the other side: the row's PAYLOAD can name the
// repo's MAIN checkout as its only absolute path — measured on live row
// 01a0dabe-3fa1-7cc8-b601-66f1456b8ffd, whose payload says "Repo:
// /home/user/work/GoPi (branch master at 4c802614). Re-verify every line number
// before relying on it." — and a run given one absolute path writes to it.
//
// The briefing is the model's only instruction, so the correction can only live
// in the briefing: name the ground as the ONE tree the run writes, and say in
// the same sentence what to do with any OTHER absolute path the payload hands
// over. That is a TEXT fix; this clause is its render, and the main-checkout
// deposit arm (task_main_checkout_deposit.go) is the machine check that catches
// the case where the text was read anyway.

// TaskBriefingAuthorityClause renders the ONE abbreviation of "my tree is the
// ground" a briefing carries (Rule 1b: the sentence has one definition, so a
// regression test can address it by name). It returns "" for a row with no
// ground — a repo-less row has no second tree to be confused with, and a clause
// naming no directory is worse than no clause (Rule 15).
//
// It is DELIBERATELY not part of the ground announcement: that clause announces
// WHERE the ground is (and forbids a second worktree); this one says which tree
// AUTHORITY over the run's writes, which is what a payload naming the shared
// checkout takes away.
func TaskBriefingAuthorityClause(ground, mainCheckout string) string {
	g := strings.TrimSpace(ground)
	if g == "" {
		return ""
	}
	m := strings.TrimSpace(mainCheckout)
	if m == "" {
		return fmt.Sprintf(taskBriefingAuthorityNoMainFmt, g)
	}
	return fmt.Sprintf(taskBriefingAuthorityFmt, g, m)
}

// taskBriefingAuthorityFmt is the clause's ONLY spelling when the shared
// checkout is known: the ground, the shared tree, and the instruction that
// resolves the collision (an absolute path in the payload is a REFERENCE; a
// write goes to the ground, always under its own relative path).
const taskBriefingAuthorityFmt = TaskBriefingAuthorityLabel + "\n" +
	"Your worktree is %s. That is the ONLY tree this run writes: every file you create, edit, test or commit — source, tests, evidence artifacts and reports alike — goes there, under a path relative to it.\n" +
	"Any OTHER absolute path in this briefing is a REFERENCE for reading, never a place to write — in particular %s is the repo's MAIN checkout, shared with the operator and with other running tasks; a write there is invisible to this run's branch and cannot be merged.\n" +
	"If this briefing's text (or Rule 6a) tells you to create a worktree or to write into the repo, THIS clause wins: the worktree named above already IS your ground — never create a second one, and do not rename its branch (the branch name IS the ledger address: the task id, so the row can be read back off it with `gopi task show --branch <branch>`)."

// TaskBriefingAuthorityLabel is the authority clause's leading marker, spelled
// once and EXPORTED (Rules 1b/13/14) for the same reason as its siblings: the
// writer below, the reframe swap's preservation list (internal/agent/
// reframe_ground_clauses.go) and the regression tests all address ONE string.
// Before this constant existed the label was typed by hand in two packages, and
// the half that mattered is that a rename here would have left the other half
// silently matching nothing — a clause dropped without a word.
const TaskBriefingAuthorityLabel = "[Your tree — the ONLY one you write]"

// taskBriefingAuthorityNoMainFmt is the clause when the shared checkout could
// not be resolved: the authority statement stands (it does not depend on
// naming the other tree), and the clause says nothing about a directory it
// cannot name.
const taskBriefingAuthorityNoMainFmt = TaskBriefingAuthorityLabel + "\n" +
	"Your worktree is %s. That is the ONLY tree this run writes: every file you create, edit, test or commit goes there, under a path relative to it. Any OTHER absolute path in this briefing is a REFERENCE for reading, never a place to write."

// TaskBriefingNamesDir reports whether briefing TEXT addresses a directory — the
// ONE predicate behind every "the row's own briefing pointed at this tree" test
// in the close-out arms, and the reason the comparison is not a bare
// strings.Contains.
//
// WHAT COUNTS AS "NAMES": the directory itself, OR anything INSIDE it (a
// briefing that says "write into <main>/devdoc/" has named the shared checkout
// just as plainly as one that spells the checkout alone). Both are decided on
// one spelling of a path (SamePath), so a symlinked or trailing-separator
// spelling of the same directory is the same name.
//
// WHY TOKEN-BOUNDARY, NOT SUBSTRING: a sibling checkout named "...-GoPi-old" (or
// a backup ".../GoPi.bak") CONTAINS the main checkout's spelling, and a
// substring test would read it as this row's pointer at the shared tree — and
// then attribute a deposit in a SHARED tree on the strength of a path the
// briefing never named. A boundary is what a filesystem path has instead of a
// syntax: the character AFTER the match must not extend the path (not a word
// byte, not `/`, not `.`/`-`/`_`), and the character BEFORE it must not either
// (so `.../my-GoPi` is a different directory, not a suffix match).
//
// THE DELIBERATE CONSERVATISM (stated rather than hidden): a path written
// sentence-final — ".../GoPi. Then start." — is NOT read as naming the checkout,
// because a full stop is also a legal path byte and treating it as prose would
// make ".../GoPi.old" a pointer at the checkout. The arm errs toward the false
// NEGATIVE on purpose: missing a record costs a reader one grep, while a false
// attribution puts a sibling run's or the operator's file on this row's trail
// (the epic's own prohibition on measuring the shared tree as a blame
// instrument).
func TaskBriefingNamesDir(text, dir string) bool {
	d := strings.TrimSpace(dir)
	if d == "" || text == "" {
		return false
	}
	want := SamePath(d)
	if want == "" {
		return false
	}
	for _, tok := range briefingPathTokens(text) {
		got := SamePath(tok)
		if got == "" {
			continue
		}
		if got == want || strings.HasPrefix(got, want+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// briefingPathSeparator is the ONE separator a briefing's absolute paths are
// found by (Rule 1b). A path in prose is delimited by characters, not by
// whitespace: the payload that measured this defect continues after the path
// with " (branch master at 4c802614)." — so scanning tokens that end at
// whitespace alone would capture a parenthesis or a full stop with the path and
// miss the match.
const briefingPathSeparator = "/"

// briefingPathTokenMax caps how long a candidate path token may be: a briefing
// is free text and a pathological run of non-space bytes must not become an
// unbounded scan (Rule 1: the bound is named, not implied).
const briefingPathTokenMax = 4096

// briefingPathTokens extracts the ABSOLUTE-PATH TOKENS of briefing text: a run
// of path characters following the path separator, preceded either by the start
// of the text or by a character that cannot extend a path. Only absolute tokens
// are returned — a relative spelling cannot name the shared checkout.
func briefingPathTokens(text string) []string {
	var out []string
	for i := 0; i < len(text); i++ {
		if text[i] != '/' {
			continue
		}
		if i > 0 && briefingPathByte(text[i-1]) {
			continue // the separator is INSIDE a longer token, not the start of one
		}
		j := i
		for j < len(text) && j-i < briefingPathTokenMax && briefingPathByte(text[j]) {
			j++
		}
		out = append(out, text[i:j])
		i = j
	}
	return out
}

// briefingPathByte reports whether b may appear inside a path token. The
// allowed set is deliberately NARROW: word bytes, the separator, and the three
// punctuation characters that occur in real checkout names (`-`, `.`, `_`). A
// space, a parenthesis, a quote, a comma or a newline ENDS the token — which is
// what keeps "…/GoPi." and "…/GoPi)" from being compared as paths.
func briefingPathByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '/', b == '-', b == '.', b == '_':
		return true
	}
	return false
}

// The advisory line's label, named once (Rule 1b) so the renderer and every
// reader of the line — the CLI test, the tools test, a future consumer face —
// share the spelling instead of re-typing it. It is deliberately DISTINCT from
// the deliverable block's own labels: the block above is what the row RECORDS,
// this line is what resolves NOW.
const TaskShowGroundLabel = "ground (resolved now):"

// Test-facing marker constants (the line's READ vocabulary): the absence
// marker and the two existence qualifiers of an axis. The axis keys themselves
// live with the shared reading (taskGroundKey*) — one source of truth for the
// sentence both faces print (Rules 13/14).
const (
	// TaskShowGroundAbsent is the rendered value of an axis that resolved to
	// nothing — the explicit absence a reader can grep for (never a blank).
	TaskShowGroundAbsent = taskGroundKeyAbsent
	// TaskShowGroundExistsMarker / TaskShowGroundStaleMarker qualify an axis
	// whose VALUE was verified: the advisory's whole point is that this ground
	// was resolved LIVE, so whether it is still there is part of the answer.
	TaskShowGroundExistsMarker = "(exists)"
	TaskShowGroundStaleMarker  = "(NOT FOUND now — a stale reading)"
)

// taskShowGroundAdvisory renders the advisory ground line for one row ("" when
// the row has neither a recorded ground nor a session — nothing to resolve and
// nothing to explain).
//
// READ-ONLY BY CONSTRUCTION: the reading is a session scan, one git resolution
// and one `git rev-parse --verify`. No caller of this function may be a writer —
// the delivered contract is that `task show` never changes the deliverable it
// is showing.
func taskShowGroundAdvisory(t *Task) string {
	if t == nil {
		return ""
	}
	recRepo, recBranch, recWT := taskRowDeliverable(t.TaskID)
	sessionID := strings.TrimSpace(t.SessionID)
	if recBranch == "" && recWT == "" && sessionID == "" {
		// Nothing but a repo is recorded and there is no session to resolve
		// from: there is no ground QUESTION to answer here (a run that never
		// started has no ground), and a line saying so on every seeded row
		// would be noise — the failure mode this epic exists to remove.
		return ""
	}
	if recBranch != "" || recWT != "" {
		// A RECORDED ground: the advisory's value is the LIVE verification —
		// is this still where the run works? Rendered from the row's own
		// record, through the same sentence and the same verifier.
		r := taskGroundSessionReading{repo: recRepo, worktree: recWT, branch: recBranch}
		var state devworkspace.GroundState
		var ref taskGroundBranchRef
		if recWT != "" {
			// THE ONE GROUND JUDGE (measured 2026-10-19, row 01a150b3): "does the
			// path exist" was this line's whole test, and a workspace whose
			// REGISTRATION was swept still read "verified live" while
			// `gopi worker --resume` read the same directory as gone. Liveness is
			// a question about the DIRECTORY's model (a clone is alive without a
			// registration; a linked worktree is alive only while listed), so it
			// is asked of devworkspace.ResolveGroundState — the same function the
			// resume refusal calls. One judge, two renderings.
			state = devworkspace.ResolveGroundState(recWT)
			r.worktreeOK = state.Alive
		}
		if recBranch != "" {
			// THE BRANCH AXIS IS A QUESTION ABOUT THE GROUND'S SHAPE, not about
			// the recorded repo (measured 2026-10-28, row 01a17df2): a
			// clone-channel run keeps its branch in the CLONE's own ref store
			// and the recorded repo never has it, so asking only the repo
			// reported a delivered run as "NOT FOUND — a stale reading". The
			// probe asks the run's own ground first, then the recorded repo, and
			// its evidence (every directory asked) rides into the why.
			ref = taskGroundBranchRefIn(recRepo, recWT, recBranch)
			r.branchOK = ref.exists
		}
		r.why = taskShowGroundRecordedWhy(r, state, ref)
		return TaskShowGroundLabel + " " + taskGroundText(r) + "\n" + taskShowGroundResumeAdvice(r)
	}
	// THE SHOW FACE asks the clone-ground candidate too: a clone-channel run
	// issues neither `git worktree add -b` nor `git checkout -b` (its clone
	// arrives on the row's branch), so the session reading alone renders
	// `branch=(none)` for a row whose ground is standing on disk holding a live
	// refs/heads/task/<id> — measured on 8 rows, 2026-10-28.
	r := taskGroundSessionReadingForShow(t.TaskID, sessionID, recRepo, taskGroundResolveList)
	return TaskShowGroundLabel + " " + taskGroundText(r) + "\n" + taskShowGroundResumeAdvice(r)
}

// taskShowGroundResumeAdvice is the SECOND half of the advisory: the choice it
// describes is made HERE, on the row, not on a help page — so the row names
// which resume face applies to IT (Rule 15: the wrong pick leaks a worktree and
// nothing says so).
//
// WHICH FACE APPLIES is decided by whether the row's ground is THERE — the
// exact difference between the two faces:
//
//	ground verified live → `gopi worker --resume <task-id>`: takes the ground
//	                       over, runs the close-out chain, RELEASES the worktree.
//	no ground            → `gopi session resume <id>`: holds nothing back
//	                       either — nothing to hold.
//
// A row that RECORDS a ground whose directory is gone is reported as the second
// case, because the first face would refuse it; the advice follows the live
// reading, never the row's own record.
func taskShowGroundResumeAdvice(r taskGroundSessionReading) string {
	if strings.TrimSpace(r.worktree) != "" && r.worktreeOK {
		return cliverbs.ResumeGroundLineLabel + " " + resumeAdviceQuote + cliverbs.ResumeTaskRowAddress +
			resumeAdviceQuote + " — takes this ground over, then releases it\n"
	}
	return cliverbs.ResumeGroundLineLabel + " " + resumeAdviceQuote + cliverbs.ResumeSessionAddress +
		resumeAdviceQuote + " (no live ground here, so nothing has to be released)\n"
}

// resumeAdviceQuote is the backtick the advice line wraps a command in. It is a
// constant because a raw string literal cannot contain one (the reason two
// pages assemble their cross-guidance the same way).
const resumeAdviceQuote = "`"

// taskShowGroundRecordedWhy states which part of a RECORDED ground is not
// confirmed live — the honest qualifier on a reading that came from the row
// rather than from the run's session (Rule 15).
//
// ref carries the branch axis' ANSWER WITH ITS EVIDENCE (the directories that
// were asked). That is not decoration: the measured defect (row 01a17df2) was a
// "no" produced by asking the one tree a clone-channel run's branch is never in,
// so a negative now has to say where it looked, and a positive has to say where
// it found it (task_ground_branch_probe.go).
func taskShowGroundRecordedWhy(r taskGroundSessionReading, state devworkspace.GroundState, ref taskGroundBranchRef) string {
	switch {
	case r.worktree != "" && !r.worktreeOK:
		// The resolver's OWN evidence, not a second opinion: which model the
		// directory is and what was checked is exactly what an operator needs
		// when the line they relied on stops saying "verified".
		return "recorded ground — NOT a live ground (model " + string(state.Model) + "): " + state.Detail
	case r.branch != "" && !r.branchOK:
		if len(ref.sites) == 0 {
			return fmt.Sprintf(taskGroundWhyBranchNowhereFmt, r.branch)
		}
		return fmt.Sprintf(taskGroundWhyBranchStaleFmt, r.branch, ref.siteText())
	case r.worktree != "":
		return "recorded ground — verified live (model " + string(state.Model) + "): " + state.Detail + ref.whereNote()
	default:
		return "recorded ground — verified live" + ref.whereNote()
	}
}
