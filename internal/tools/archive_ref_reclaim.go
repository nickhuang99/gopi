package tools

// The DECIDED-EXCLUSION REF RECLAIMER (epic archive-ref-reclaim, 2026-10-01).
//
// INCIDENT: an archive tag is a DECISION ("this branch will never be merged"),
// and for every branch it names the decision was recorded and the BRANCH REF
// WAS LEFT BEHIND. Measured on this host before the fix: `refs/heads/feat/
// capture-self-window` and `refs/tags/archive/feat/capture-self-window` both at
// e1701ac8 — the tag says "decided", the head says "waiting for a decision",
// and the two are the same commit. Nothing in the repo ever removed the head:
//
//	① repoSweepUnprotectedBranches drops an archive-tagged head from the
//	   candidate set (repoSweepArchiveExempt) — it SKIPS, it does not delete;
//	② finishCleanupBranchExempt lets the close-out chain pass such a branch
//	   over — again a skip;
//	③ ArchiveTagReasoned writes the tag (and its reason record) and RETURNS.
//
// Three skip arms and no delete arm: the head is exempt from every path that
// could remove it, so it is exempt from removal itself. An exemption without a
// reclaim arm is not a decision, it is a leak, and it is a leak that hides —
// the branch is invisible in the merge queue (that is what the tag buys) while
// remaining in `git branch` forever.
//
// WHAT THIS ARM IS, AND WHAT IT DELIBERATELY IS NOT. It is the ONE place that
// turns an existing archive tag into the removal of the head that tag decided
// against. It is NOT a second archive lane (it never mints a tag), not a prune
// of the archive namespace (the tag is the durable half and is NEVER touched),
// and not a general branch reaper (it acts only where the decision ALREADY
// exists as a ref).
//
// THE FIVE GUARDS, each of which is a REFUSAL when it does not hold:
//
//	① the tag must EXIST. The decision is the input; a branch with no archive
//	   tag is somebody's work in flight and no arm here may touch it.
//	② the tag and the head must name the SAME COMMIT. A tag at an older tip
//	   means the branch MOVED AFTER the decision — new commits on a decided
//	   branch are the one shape where deleting the head would destroy work, and
//	   the honest answer is to keep the ref and report the divergence
//	   (Rule 15: no silent failure, and no silent destruction either).
//	③ the branch must not be held by any REGISTERED worktree. A checkout is a
//	   live peer's ground and this arm never acts inside somebody else's tree
//	   (PROJECT.md Rule 29). The check reads git's own registry, so an occupancy
//	   the sweep can see is an occupancy this arm obeys.
//	④ master/main are never candidates — the integration head, by NAME.
//	   (A tag written over it is a mistake this arm refuses to act on.)
//
//	   `task/*` USED to be refused by name too, and that was a defect: the
//	   refusal sentence ("is … a task-run ground") was asserted BEFORE anything
//	   was checked, so a `task/<id>` branch whose ledger row is long gone, whose
//	   worktree was reclaimed and whose content is already on master was held
//	   out of reclaim FOREVER — by a sentence that was, in its own words, false
//	   about it. The reason a task ground must not be removed is a FACT about
//	   the ground, not a property of its spelling, so the arm now ASKS:
//	   `task/*` is refused only when ③ a registered worktree holds it, or ⑥ a
//	   ledger row still tracks it and that row is NOT terminal — and the refusal
//	   NAMES the fact (the worktree path, or the task id with its status) so the
//	   operator reads which one vetoed it.
//	⑤ the decision must still be the tip's decision: the guard ② check is
//	   re-asked at the moment of the write, so a commit landing between the
//	   scan and the delete cannot slip through.
//
// RECORDS THE WHY BEFORE IT REMOVES (epic archive-reclaim-reason, 2026-10-17).
// A tag is a decision; the companion ref refs/gopi/archive-reason/<branch> is
// the WHY, and this arm used to remove the head without ever asking for it — so
// every reclaim left a reason-less archive ref behind and `gopi task archive
// --check` reported the arm's own output as debt (measured: 11 refs removed, 10
// with no record; 239→229 only after a human wrote them by hand). It now records
// through the project's ONE archive writer (ArchiveTagReasoned) between guard ②
// and the write, so a reclaim never deepens that debt; an existing record is
// never rewritten (the other lanes' reasons are the only copy of why a branch
// lost) and a record that cannot be written KEEPS the ref (Rule 15).
//
// THE DELETE IS `git update-ref -d`, NOT `git branch -d`, AND THAT IS
// DELIBERATE. `branch -d` refuses a branch not merged into its upstream — the
// refusal is a MERGE question, and an archived branch is by construction one
// the merge path will never take. Asking git's merged-only check here would
// make the arm succeed exactly on the branches that need no reclaim arm (the
// ones already in master) and refuse every branch the tag exists for. The
// safety comes from guards ①–⑤ instead: the tag is the authorisation, the
// same-commit check is the proof the authorisation still describes the tip, and
// the worktree guard keeps live ground out of it. `update-ref` is also the
// form that CANNOT move a checked-out branch out from under its checkout —
// combined with ③, no live tree is ever left on a ref that no longer exists.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nickhuang99/gopi/internal/removalaudit"
)

// ArchiveReclaimDeleted / ArchiveReclaimKept / ArchiveReclaimSkipped are the
// three verdicts this arm returns per branch (Rule 1b: the render, the tests
// and any future caller spell them from here, never inline).
const (
	// ArchiveReclaimDeleted — the head was removed; the archive tag stays.
	ArchiveReclaimDeleted = "deleted"
	// ArchiveReclaimKept — the tag and the head DISAGREE on the commit, so the
	// head was kept and the divergence reported (guard ②).
	ArchiveReclaimKept = "kept-divergent"
	// ArchiveReclaimSkipped — a guard other than ② refused: a worktree holds
	// the branch, or the name is structurally protected (④/③).
	ArchiveReclaimSkipped = "skipped"
)

// ArchiveReclaimRow is ONE branch's outcome from the reclaim arm — what was
// asked, what was decided, and the git reading that decided it. The tip fields
// are carried so a reviewer can verify the same-commit guard without a second
// scan: a row that says "deleted" carries the sha both refs named.
type ArchiveReclaimRow struct {
	Branch  string `json:"branch"`
	Tag     string `json:"tag"`
	Verdict string `json:"verdict"`
	// Reason is the operator-facing sentence for Kept/Skipped ("" for a
	// deletion — the outcome is its own sentence). It always names WHICH guard
	// fired, never a wordless skip (Rule 15).
	Reason string `json:"reason,omitempty"`
	// Kind names WHICH refusal this row is (ArchiveReclaimProtected*), so a
	// caller can react to the FACT that vetoed the branch without parsing the
	// sentence — and so a test can assert on the fact rather than on prose.
	// Empty for a deletion and for the divergence arm (which is its own kind).
	Kind    string `json:"kind,omitempty"`
	TagTip  string `json:"tag_tip,omitempty"`
	HeadTip string `json:"head_tip,omitempty"`
}

// ArchiveReclaimResult is the arm's whole report: the rows plus the git
// failure that stopped the scan (Rule 15 — a scan that could not run must
// never render as "nothing to reclaim").
type ArchiveReclaimResult struct {
	Repo    string              `json:"repo"`
	Rows    []ArchiveReclaimRow `json:"rows"`
	Deleted int                 `json:"deleted"`
	Kept    int                 `json:"kept"`
	Skipped int                 `json:"skipped"`
	// Err is the scan failure, if any: it is REPORTED and the arm does nothing
	// further, because a partial view of the ref space is not a decision basis.
	Err string `json:"err,omitempty"`
}

// The kinds of refusal an archive-reclaim row can carry. They are named
// constants because they are COMPARED (by the arm, and by a test asserting on
// WHICH fact vetoed a branch) — Rule 1b: the spellings live here, never inline.
const (
	// ArchiveReclaimProtectedIntegrationBranch — master/main: the branch the
	// whole system integrates INTO, refused by NAME alone.
	ArchiveReclaimProtectedIntegrationBranch = "integration-branch"
	// ArchiveReclaimProtectedLiveRow — the ledger still tracks the branch and
	// the row it picked is NOT terminal, so the work may still be in flight.
	ArchiveReclaimProtectedLiveRow = "live-task-row"
	// ArchiveReclaimProtectedWorktree — a registered worktree holds the branch.
	ArchiveReclaimProtectedWorktree = "held-by-worktree"
)

// archiveReclaimProtected reports whether branch is out of this arm's reach BY
// NAME — the integration branch (master/main), and nothing else.
//
// The task-branch namespace (`task/<id>`, TaskGroundBranchPrefix) deliberately
// did NOT survive in this predicate: it is not protected by its NAME but by the
// two FACTS the scan checks in the loop below (a held worktree, a non-terminal
// owning row). A `task/*` branch nobody holds and nobody tracks is residue, and
// refusing it by prefix kept it there forever behind a reason that was simply
// untrue of it (see this file's header, guard ④).
func archiveReclaimProtected(branch string) bool {
	switch branch {
	case masterBranchName, mainBranchName:
		return true
	}
	return false
}

// archiveReclaimIsTaskGround reports whether branch is in the task-branch
// namespace — the shape whose protection is a QUESTION about facts rather than a
// name test. It reads the ONE shared spelling (TaskGroundBranchPrefix).
func archiveReclaimIsTaskGround(branch string) bool {
	return strings.HasPrefix(branch, TaskGroundBranchPrefix)
}

// ArchiveReclaimOwnerRow is ONE ledger row that a branch's reclaim decision had
// to consult: the row's id, its status, and the task id normalised out of it
// (the same normalisation the archive gate applies to a branch-derived id). The
// status is passed back out because it is what the refusal sentence and the test
// report.
type ArchiveReclaimOwnerRow struct {
	TaskID string
	Status string
}

// archiveReclaimOwnerLookup answers "which ledger rows track this branch?" —
// ALL of them, because the question the arm asks ("is any row's reading
// non-terminal?") is not answerable from the single most-cautious pick: a
// branch with a `cancelled` row AND an `in_progress` row is the exact shape
// that parked a live deliverable before (see archiveBranchOwners).
//
// The failure is RETURNED, never folded into an empty answer: a ledger that
// could not be read is not "nothing tracks this branch", and reading it that way
// would delete a live run's ground (Rule 15).
type archiveReclaimOwnerLookup func(branch string) ([]ArchiveReclaimOwnerRow, error)

// archiveReclaimOwnersFromLedger is the PRODUCTION owner lookup: the ledger
// seam, actually used. It reads the same rows the archive gate reads
// (archiveBranchOwners) but keeps EVERY row naming the branch rather than the
// one most-cautious pick — the pick answers "how shall this branch be
// described", which is a different question from "is anything still building on
// it".
//
// The double read is deliberate. archiveBranchOwners resolves the operator's
// ledger itself (its own env/path chain), while the scan needs the seam to be
// injectable so a test can pin it without touching that ledger; and
// archiveReclaimMatchesBranch keeps the two readings on ONE branch-matching
// rule, so a second reader cannot disagree with the first about which rows name
// a branch (Rule 13).
func archiveReclaimOwnersFromLedger(branch string) ([]ArchiveReclaimOwnerRow, error) {
	owners, err := archiveBranchOwners()
	if err != nil {
		return nil, err
	}
	var out []ArchiveReclaimOwnerRow
	for b, o := range owners {
		if !archiveReclaimMatchesBranch(b, branch) {
			continue
		}
		out = append(out, ArchiveReclaimOwnerRow{TaskID: strings.TrimSpace(o.taskID), Status: o.status})
	}
	if len(out) == 0 {
		// The map has no exact key for this branch — which is NOT yet "no row
		// tracks it": the exact-key map is built on trimmed equality, while this
		// arm ALSO recognises a `task/<id>` ground whose ledger row spells the id
		// differently (archiveReclaimMatchesBranch's normalisation half), so ask
		// the enumeration directly with the same matcher. Both readers enumerate
		// the SAME axes now (task_owner_axes.go) — this is the arm's tolerance
		// being applied, not a second membership set.
		all, aerr := archiveReclaimAllOwners()
		if aerr != nil {
			return nil, aerr
		}
		for _, o := range all {
			if archiveReclaimMatchesBranch(o.branch, branch) {
				out = append(out, ArchiveReclaimOwnerRow{TaskID: strings.TrimSpace(o.taskID), Status: o.status})
			}
		}
	}
	return out, nil
}

// archiveReclaimMatchesBranch is the ONE branch-matching rule this arm uses
// when it asks the ledger about a name: equal after trimming (the branch is what
// git holds), or equal after the task-id normalisation the archive path already
// applies to a branch-derived id (taskIDNormalize), so a `task/<id>` ground
// whose ledger row records the id in another spelling is still recognised.
//
// The equality half is the package's ONE spelling of "this axis names that
// branch" (taskOwnerBranchMatchesAxis) — the same function TaskForBranch's
// matcher calls, so the membership the lookup uses and the membership this arm
// uses cannot drift (task_owner_axes.go). The normalisation half is this ARM's
// own question ("may this name refer to the same task?") and is not shared: a
// lookup answers about an exact branch name, while a reclaim decision has to
// recognise the ground of a row whose id was spelled differently.
func archiveReclaimMatchesBranch(recorded, branch string) bool {
	recorded, branch = strings.TrimSpace(recorded), strings.TrimSpace(branch)
	if recorded == "" || branch == "" {
		return false
	}
	if taskOwnerBranchMatchesAxis(recorded, branch) {
		return true
	}
	if !archiveReclaimIsTaskGround(branch) {
		return false
	}
	return taskIDNormalize(recorded) == taskIDNormalize(branch)
}

// archiveReclaimAllOwners reads every (task_id, status, branch) the ledger holds
// for a deliverable — the OWNER AXIS ENUMERATION (task_owner_axes.go).
//
// It used to be a second SQL reader of its own (`LIKE '%"branch":"%'` plus
// deliverableBranchOf), and that second reader was the divergence epic 01a13214
// closed: it read the PRIMARY axis only, so a branch a run CREATED as a ground —
// a fact the ledger records on purpose — did not make its row a candidate here
// while TaskForBranch counted it (the measured `base-peermaster-01a12b54`:
// `merge --status` named row 01a12b54-e636…, `archive --check` said `(none)`).
//
// The SQL and the parse now live in ONE place (taskDeliverableScanRows and
// taskDeliverableParse), driven by the SAME matcher the branch lookup uses, so
// the two faces share a membership set by construction rather than by
// convention. The `LIKE` needle is gone with the second query: it was a
// narrowing hint even here, and a needle on a JSON blob also matches a branch
// name occurring in the row's REPO or WORKTREE string.
func archiveReclaimAllOwners() ([]archiveReclaimOwnerRecord, error) {
	records, err := taskOwnerAxesAll()
	if err != nil {
		return nil, err
	}
	out := make([]archiveReclaimOwnerRecord, 0, len(records))
	for _, rec := range records {
		out = append(out, archiveReclaimOwnerRecord{branch: rec.branch, taskID: rec.taskID, status: rec.status})
	}
	return out, nil
}

// archiveReclaimOwnerRecord is one enumerated ledger row (the fallback reader's
// shape — unexported: the exported ArchiveReclaimOwnerRow is what crosses the
// seam).
type archiveReclaimOwnerRecord struct {
	branch string
	taskID string
	status string
}

// archiveReclaimRowLiveness reads ONE ledger row's status as the answer to "may
// the work still be in flight, so must this branch be kept?" — TRUE means KEEP.
//
// It is emphatically NOT the terminal set of the worktree sweep
// (worktree_sweep.go's tasksRowForSweep.terminal), and the difference is the
// whole safety direction. That predicate answers "is this ROW finished", where
// the only non-finished statuses are the ones a claim path can still reach
// (pending/claimed/in_progress). This one answers "may a live process still be
// building on this branch", where the honest answer for a status this package
// does not classify is KEEP (Rule 15's default-deny: an unconsidered status is
// not a permission to delete a ref) — `reopened` is exactly such a status, and
// it is live here for that reason.
//
// A row that names NO status at all ("" — a ledger row written before the
// column, or by a caller that left it empty) can claim no terminality either,
// so it keeps the branch as well. The read is the same one the archive gate
// makes (archiveOwnerRank's LIVE rank), so the gate and this arm agree about
// which rows are live: the arm asks about EVERY row naming the branch, and the
// gate acts on the most-cautious one — a live row makes both say no.
func archiveReclaimRowLiveness(status string) bool {
	switch strings.TrimSpace(status) {
	case TaskStatusCompleted, TaskStatusVerified, TaskStatusFailed,
		TaskStatusCancelled, TaskStatusSuperseded:
		return false
	}
	return true
}

// archiveReclaimLiveRow picks the row whose refusal sentence to print when an
// owning row keeps the branch: the FIRST live row, in the order the lookup
// returned (the query's own order, so repeated runs answer identically).
func archiveReclaimLiveRow(rows []ArchiveReclaimOwnerRow) (ArchiveReclaimOwnerRow, bool) {
	for _, r := range rows {
		if archiveReclaimRowLiveness(r.Status) {
			return r, true
		}
	}
	return ArchiveReclaimOwnerRow{}, false
}

// archiveReclaimOwnerUnknown is how a refusal names a live row that carried no
// task id: the ABSENCE is rendered, never an empty field a reader would take
// for a formatting bug (and never a task id this arm invented).
const archiveReclaimOwnerUnknown = "(task id not recorded on the row)"

// archiveReclaimDivergence is the guard-② refusal sentence. It is built from
// the two SHAs, so the report says exactly which commits disagreed — an
// operator reading it can decide whether the branch moved (rework) or the tag
// points somewhere unexpected.
func archiveReclaimDivergence(branch, tagTip, headTip string) string {
	return fmt.Sprintf(
		"archive tag and branch ref name DIFFERENT commits (%s %s vs %s %s) — the branch moved after the decision "+
			"or the tag was written elsewhere; keeping the ref (Rule 15: report, never delete work)",
		archiveBranchPrefix+branch, tagTip, refsHeadsPrefix+branch, headTip)
}

// archiveReclaimHeldStatement is the guard-③ refusal: the sentence AND the kind
// it is evidence of. The sentence names the tree so the operator can see WHOSE
// ground vetoed it (Rule 29) — never a wordless skip (Rule 15).
func archiveReclaimHeldStatement(branch, worktree string) (kind, reason string) {
	return ArchiveReclaimProtectedWorktree,
		fmt.Sprintf("branch %s is checked out at %s — a live checkout is never touched (GOPI.md Rule 29)",
			branch, worktree)
}

// archiveReclaimProtectedStatement is the guard-④ refusal: the integration
// branch, refused by NAME. The sentence says only what was actually measured —
// master/main are refused because of WHICH BRANCH THEY ARE, and nothing here
// claims anything about task grounds (that claim is the sixth guard's, and it is
// made from facts).
func archiveReclaimProtectedStatement(branch string) (kind, reason string) {
	return ArchiveReclaimProtectedIntegrationBranch, ArchiveProtectedRefusalText(branch)
}

// ArchiveProtectedRefusalText is the ONE sentence for guard ④'s veto, exported
// so BOTH faces of it read the same words (Rule 13): the reclaim sweep prints it
// as a skipped row's reason, and the human `gopi task archive <branch>` gate
// prints it as its refusal BEFORE it writes anything.
//
// It says what was MEASURED — a branch NAME — and claims nothing about grounds,
// rows or worktrees (those are other guards' facts, and each names its own).
// "never a reclaim candidate" is the reclaim lane's own phrasing: a reclaim is
// what that guard governs, and the sweep is where it is printed. The ARCHIVE
// face states the same veto in its own words (archiveProtectedRefusalSentence),
// derived from this sentence rather than restating it.
func ArchiveProtectedRefusalText(branch string) string {
	return fmt.Sprintf("branch %s is the integration branch — never a reclaim candidate", branch)
}

// archiveReclaimLiveRowStatement is the guard-⑥ refusal: a ledger row still
// tracks `task/<id>` and its status is not terminal, so the branch may be a
// running deliverable's ground. The sentence NAMES the row and its status, so
// the operator can look the row up and decide — the pre-fix sentence named a
// fact about the branch's NAME instead, which is why it could be (and was)
// false.
func archiveReclaimLiveRowStatement(branch string, row ArchiveReclaimOwnerRow) (kind, reason string) {
	taskID := strings.TrimSpace(row.TaskID)
	if taskID == "" {
		taskID = archiveReclaimOwnerUnknown
	}
	status := strings.TrimSpace(row.Status)
	if status == "" {
		status = archiveReclaimOwnerUnknown
	}
	return ArchiveReclaimProtectedLiveRow,
		fmt.Sprintf("branch %s is tracked by task %s whose row is %q — a non-terminal row may still be building on it, "+
			"so the ground is kept (finish or cancel the row to release it)", branch, taskID, status)
}

// ArchiveReclaimDecidedRefs removes the local branch refs whose archive tag
// ALREADY exists and still names the ref's own commit. It is the deterministic
// delete arm the three skip arms were missing, and it is IDEMPOTENT: a second
// run finds no tagged heads and reports nothing.
//
// It never deletes a tag, never mints one, never touches a ref whose tag is
// absent, and never acts on master/main. A `task/<id>` branch is refused only
// when a FACT says so (a registered worktree holds it, or a non-terminal ledger
// row tracks it) and the refusal names that fact; master/main are still refused
// by name. It writes ONLY the deletions its own guards cleared, and every
// refusal is a row with a reason (Rule 15).
//
// The whole scan is `git for-each-ref` over refs/tags/archive/ plus
// `git worktree list` — two calls, then one `update-ref -d` per reclaimed
// branch, so the arm's cost is proportional to the residue it finds, not to
// the repo's size.
//
// QUERYING THE LEDGER LAZILY, AND FAILING LOUDLY. The scan asks the owner
// lookup ONLY about a task-ground name whose worktree guard cleared — no ledger
// access at all on the ordinary `feat/…` residue — and it asks through the
// injectable seam so a test can drive every ledger shape without touching the
// operator's own ledger (Rule 32). A lookup that cannot answer stops the arm
// with its error: reading it as "no row tracks this branch" is exactly the
// reading that would delete a live ground (Rule 15).
func ArchiveReclaimDecidedRefs(repo string) ArchiveReclaimResult {
	return ArchiveReclaimDecidedRefsWith(repo, archiveReclaimOwnersFromLedger)
}

// archiveReclaimOwnerLookupDefault names the production lookup so a caller (and
// the test that pins the default) never respells it inline.
func archiveReclaimOwnerLookupDefault() archiveReclaimOwnerLookup {
	return archiveReclaimOwnersFromLedger
}

// ArchiveReclaimDecidedRefsWith is ArchiveReclaimDecidedRefs with the ledger
// seam supplied: `lookup` answers "which rows track this branch?". The
// production entry point passes the default (archiveReclaimOwnerLookupDefault),
// so the shipped behaviour is one call away from the tested one — the seam does
// not fork the arm, it parameterises WHO the ledger question is put to.
func ArchiveReclaimDecidedRefsWith(repo string, lookup archiveReclaimOwnerLookup) ArchiveReclaimResult {
	res := ArchiveReclaimResult{Repo: repo}
	if lookup == nil {
		// A nil seam is a programming error, not a state: reporting it beats
		// silently becoming "no row tracks anything" (Rule 15).
		res.Err = "archive reclaim: no ledger owner lookup supplied"
		return res
	}
	entries, err := archiveNamespaceEntries(repo)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	held, _, err := mergedCleanupWorktrees(repo)
	if err != nil {
		// An unreadable worktree registry is NOT "no worktree holds it": that
		// reading would delete a live tree's ground, so the arm stops and says
		// so (Rule 15 + Rule 29).
		res.Err = fmt.Sprintf("worktree registry unreadable (refusing to reclaim): %v", err)
		return res
	}
	for _, e := range entries {
		branch := e.branch
		if branch == "" {
			continue
		}
		row := ArchiveReclaimRow{Branch: branch, Tag: e.tag}
		present, perr := repoSweepRefPresent(repo, branch)
		switch {
		case perr != nil:
			// The probe could not answer: reported, never read as "already
			// reclaimed" (Rule 15 — an inconclusive probe is not a state).
			row.Verdict, row.Reason = ArchiveReclaimSkipped, perr.Error()
		case !present:
			// The tag outlives its branch already — this IS the state the arm
			// produces, so there is no row and no work: the report's own
			// "nothing owed" sentence carries it.
			continue
		case archiveReclaimProtected(branch):
			row.Kind, row.Reason = archiveReclaimProtectedStatement(branch)
			row.Verdict = ArchiveReclaimSkipped
		case held[branch] != "":
			row.Kind, row.Reason = archiveReclaimHeldStatement(branch, held[branch])
			row.Verdict = ArchiveReclaimSkipped
		case archiveReclaimIsTaskGround(branch):
			// A task ground's protection is a FACT, not its name: ask the ledger
			// who (if anyone) still tracks it, and refuse only when the answer
			// names a row that is not terminal — naming that row in the refusal.
			rows, lerr := lookup(branch)
			if lerr != nil {
				// The ledger could not be read: NOT "nothing tracks it". That
				// reading would delete a live run's ground, so the arm stops
				// (Rule 15 — an unreadable ledger is not an empty ledger).
				res.Err = fmt.Sprintf("ledger owner lookup for %s failed (refusing to reclaim): %v", branch, lerr)
				return res
			}
			if live, ok := archiveReclaimLiveRow(rows); ok {
				row.Kind, row.Reason = archiveReclaimLiveRowStatement(branch, live)
				row.Verdict = ArchiveReclaimSkipped
				break
			}
			// No worktree holds it and no live row tracks it: the unowned
			// residue the archive decision was made about, and the whole point
			// of this arm.
			row.Verdict, row.TagTip, row.HeadTip, row.Reason = archiveReclaimOne(repo, branch, e.tag)
		default:
			row.Verdict, row.TagTip, row.HeadTip, row.Reason = archiveReclaimOne(repo, branch, e.tag)
		}
		switch row.Verdict {
		case ArchiveReclaimDeleted:
			res.Deleted++
		case ArchiveReclaimKept:
			res.Kept++
		default:
			res.Skipped++
		}
		res.Rows = append(res.Rows, row)
	}
	sort.Slice(res.Rows, func(i, j int) bool { return res.Rows[i].Branch < res.Rows[j].Branch })
	return res
}

// archiveReclaimOne decides ONE branch and, when every guard clears, removes
// its head. Split from the scan so the guard sequence is readable in one place
// and so a test can drive a single branch without a fixture repo's worth of
// other refs.
//
// Guard ② is asked TWICE on purpose: once here at decision time (which it
// reports) and once inside archiveReclaimDelete immediately before the write,
// so a commit landing in between cannot be deleted by a decision made about the
// previous tip. The second check's refusal is reported by the same divergence
// sentence.
func archiveReclaimOne(repo, branch, tag string) (verdict, tagTip, headTip, reason string) {
	return archiveReclaimOneWith(repo, branch, tag, archiveReclaimReasonRecorder)
}

// archiveReclaimReasonRecorder is the PRODUCTION reason seam: the project's ONE
// archive writer (ArchiveTagReasoned → archiveTagReasonedRef), which never
// rewrites an existing tag and never overwrites a record that already carries a
// reason. It is the DEFAULT, not an alternative lane.
func archiveReclaimReasonRecorder(repo, branch, tagTip string) error {
	return archiveReclaimRecordReason(repo, branch, tagTip)
}

// archiveReclaimReasonRecord is the seam type: "ensure this decided branch has a
// WHY recorded, naming the reclaim and the tip it acted on". It exists so a test
// can drive the record-before-remove ORDER and its failure half (Rule 15) without
// having to make git itself unwritable — the seam parameterises WHO writes the
// record, it does not fork the arm (the same shape as the ledger lookup seam).
type archiveReclaimReasonRecord func(repo, branch, tagTip string) error

// archiveReclaimRecordReason ensures branch's reason record exists BEFORE the
// head is removed, and returns the writer's error when it could not be written.
//
// WHY THE ARM RECORDS AT ALL (epic archive-reclaim-reason, measured 2026-10-17):
// this arm removes the head of a branch whose archive TAG already names the
// decision, so the decision is durable — but a tag alone is a WHY-less decision.
// Every other archive lane (park, the merge-time gates, `gopi merge`'s
// unrelated-history verdict) reaches the reason namespace through
// ArchiveTagReasoned; this arm did not, so each reclaim it performed left a
// reason-less archive ref behind and the operator's `gopi task archive --check`
// reported the arm's own output as debt to be remediated by hand (11 refs
// removed, 10 without a record; the count fell 239→229 only after they were
// written by hand).
//
// THE WRITER IS THE SHARED ONE, AND IDEMPOTENCE IS ITS OWN RULE. The call is
// ArchiveTagReasoned — the ONE writer of both halves (tag + companion reason
// ref) — so this arm cannot mint a second reason namespace or a second record
// shape:
//
//	① a branch whose tag already carries a reason keeps that reason. The
//	   record is NEVER rewritten, and that is what preserves the other lanes'
//	   WHY: the park lane says not-ff-able-park, the compile gates say
//	   compile-failure, and a reclaim that restamped the record with its own
//	   code would delete the only copy of why the branch lost. A test pins the
//	   byte-identity of such a record across a reclaim.
//	② a LEGACY reason-less tag (the 21:04 batch, and every tag written before
//	   the reason lane existed) gets ArchiveReasonReclaimCarried, with the arm
//	   and the tag tip in the record's detail: — the WHY this arm can honestly
//	   claim is "the decision was already recorded, and the ref was reclaimed
//	   against that very commit", not a story about why the branch lost.
//	③ the TAG is not touched. ArchiveTagReasoned only WRITES a tag that does
//	   not exist, so an existing archive ref (and the commit it names) is
//	   byte-identical after this call — Rule 9's durable half.
//
// A RECORD THAT CANNOT BE WRITTEN STOPS THE REMOVAL (Rule 15). The caller keeps
// the head ref and reports the failure: deleting the ref first would leave
// exactly the reason-less state this arm now exists to prevent, with the
// decision's only remaining witness a tag nobody can interrogate.
func archiveReclaimRecordReason(repo, branch, tagTip string) error {
	if _, err := ArchiveTagReasoned(repo, branch, ArchiveReasonReclaimCarried,
		archiveReclaimReasonDetail(branch, tagTip)); err != nil {
		return fmt.Errorf("record the archive reason before removing the head: %w", err)
	}
	return nil
}

// archiveReclaimReasonDetail renders the record's detail: line — WHAT did this,
// and against WHICH commit. Both are facts read at record time: the arm's name
// (so a reader of the record knows which lane wrote it, and can tell a reclaim
// from a park or a compile gate) and the tag tip (so the record says which
// commit the decision was carried through against, which is the one thing that
// makes "the ref was reclaimed" checkable later).
func archiveReclaimReasonDetail(branch, tagTip string) string {
	return fmt.Sprintf(
		"%s carried this branch's existing archive decision through by removing %s%s (%s%s still names it)%s",
		removalaudit.ArmArchiveReclaimBranch, refsHeadsPrefix, branch, archiveTagRefPrefix, branch,
		archiveReclaimReasonTipSuffix(tagTip))
}

// archiveReclaimReasonTipSuffix renders the tip half of the detail line ("" when
// the tip is unknown — never the word "unknown" glued to a colon, which would
// read as a sha).
func archiveReclaimReasonTipSuffix(tip string) string {
	if strings.TrimSpace(tip) == "" {
		return " at an unrecorded tip"
	}
	return " at " + strings.TrimSpace(tip)
}

// archiveReclaimOneWith is archiveReclaimOne with the reason seam supplied: the
// record is written between guard ② and the removal, so the ORDER (record, then
// re-check, then remove) is one sequence a test can read in one place.
func archiveReclaimOneWith(repo, branch, tag string, record archiveReclaimReasonRecord) (verdict, tagTip, headTip, reason string) {
	headTip, err := repoSweepGit(repo, "rev-parse", "--verify", "--quiet", refsHeadsPrefix+branch)
	if err != nil {
		return ArchiveReclaimSkipped, "", "", "branch ref unreadable: " + err.Error()
	}
	headTip = strings.TrimSpace(headTip)
	tagTip, err = repoSweepGit(repo, "rev-parse", "--verify", "--quiet", archiveTagRefPrefix+branch)
	if err != nil {
		return ArchiveReclaimSkipped, "", "", "archive tag unreadable: " + err.Error()
	}
	// A LIGHTWEIGHT tag resolves straight to the commit; an ANNOTATED one
	// resolves to the tag object, whose peeled commit is what the branch is
	// compared against. Both shapes are read here so the guard does not
	// silently read an annotated tag as a divergence it is not.
	tagTip = strings.TrimSpace(tagTip)
	if peeled, perr := repoSweepGit(repo, "rev-parse", "--verify", "--quiet", archiveTagRefPrefix+branch+"^{commit}"); perr == nil {
		if p := strings.TrimSpace(peeled); p != "" {
			tagTip = p
		}
	}
	if tagTip != headTip {
		return ArchiveReclaimKept, tagTip, headTip, archiveReclaimDivergence(branch, tagTip, headTip)
	}
	// RECORD BEFORE REMOVE (epic archive-reclaim-reason). The WHY is written
	// while the ref it is about still exists; a record that cannot be written
	// keeps the ref (Rule 15) instead of leaving a reason-less archive behind.
	if record != nil {
		if rerr := record(repo, branch, tagTip); rerr != nil {
			return ArchiveReclaimSkipped, tagTip, headTip,
				"reason record not written, so the head ref was KEPT: " + rerr.Error()
		}
	}
	if err := archiveReclaimDelete(repo, branch, tagTip); err != nil {
		return ArchiveReclaimSkipped, tagTip, headTip, err.Error()
	}
	return ArchiveReclaimDeleted, tagTip, headTip, ""
}

// archiveReclaimDelete performs the one write this arm owns: the head ref.
// The tip the caller cleared is passed back in and RE-VERIFIED first, so the
// deletion is always about a commit an archive tag still names — the race
// window between the scan and this call is closed here rather than argued
// about.
//
// The tag ref is not read back into the deletion, and is never written: the
// durable half of the decision survives by construction.
//
// IT RECORDS BEFORE IT REMOVES (2026-10-12). Every other branch deletion in the
// tree ran `git branch -d/-D` and was enumerated by the structural census in
// internal/standards; this one runs `git update-ref -d` — deliberately (see this
// file's header) — and that spelling put it OUTSIDE the census's verb pair, so
// its deletions were the one class of local ref removal that left no
// `removal-audit.log` line. Measured on this host: 11 refs/heads (16 → 5) went
// at 09:45–09:46 CST 2026-10-12 with no `kind=branch-delete` record for any of
// them, and this function, driven for real against a fixture repo, removed its
// ref and wrote nothing. The record is emitted FIRST for the same reason the
// package's other arms emit first: a record written after the mutation explains
// nothing about a crash inside it.
func archiveReclaimDelete(repo, branch, wantTip string) error {
	cur, err := repoSweepGit(repo, "rev-parse", "--verify", "--quiet", refsHeadsPrefix+branch)
	if err != nil {
		return fmt.Errorf("re-check before delete: %w", err)
	}
	if strings.TrimSpace(cur) != wantTip {
		return fmt.Errorf("re-check before delete: %s%s moved to %s after the decision read %s — refusing",
			refsHeadsPrefix, branch, strings.TrimSpace(cur), wantTip)
	}
	removalaudit.BranchDeleteAt(removalaudit.ArmArchiveReclaimBranch, repo, branch, "",
		wantTip, "",
		"the archive tag "+archiveTagRefPrefix+branch+" still names this ref's own commit ("+wantTip+
			"), so the decision it records is this arm's authorisation; the tag is kept")
	if _, err := repoSweepGit(repo, removalaudit.RefDeleteVerb, removalaudit.RefDeleteFlag,
		refsHeadsPrefix+branch, wantTip); err != nil {
		return fmt.Errorf("delete %s%s: %w", refsHeadsPrefix, branch, err)
	}
	return nil
}

// ArchiveReclaimRowLine renders ONE row as the operator-facing line — the
// verdict, the tag that named the branch, and (for a refusal) the guard that
// fired. It is the ONE renderer for a row, shared by the sweep's own report and
// by the archive verb's automatic post-archive leg, so the two surfaces cannot
// disagree about what a row says (Rule 13). A deletion carries no Reason — the
// outcome IS its sentence — so the TAG is what names it there; rendering the
// reason alone would print an empty line for the one verdict that needs no
// explanation.
func ArchiveReclaimRowLine(r ArchiveReclaimRow) string {
	if r.Reason == "" {
		return fmt.Sprintf("%-16s %s", r.Verdict, r.Tag)
	}
	return fmt.Sprintf("%-16s %s  %s", r.Verdict, r.Tag, r.Reason)
}

// ArchiveReclaimReport renders the arm's result for an operator: one line per
// branch with its verdict and (for a refusal) the guard that fired, then the
// tally. A scan failure is rendered as the failure it is, never as a clean
// zero (Rule 15).
func ArchiveReclaimReport(res ArchiveReclaimResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "archive ref reclaim: %s\n", res.Repo)
	if res.Err != "" {
		fmt.Fprintf(&b, "  SCAN FAILED: %s\n", res.Err)
		return b.String()
	}
	if len(res.Rows) == 0 {
		b.WriteString("  clean — every archive tag's branch ref is already reclaimed\n")
		return b.String()
	}
	for _, r := range res.Rows {
		fmt.Fprintf(&b, "  %s\n", ArchiveReclaimRowLine(r))
	}
	fmt.Fprintf(&b, "  deleted=%d kept=%d skipped=%d\n", res.Deleted, res.Kept, res.Skipped)
	return b.String()
}

// DecidedRefReclaimed reports whether branch's head ref is in the state this
// arm produces: an archive tag exists and NO branch ref does. It is the ONE
// predicate for "the decision has been carried through", so a caller (a test,
// a status line, a future sweep) never re-derives it from two ref probes with
// its own spelling (Rule 13).
//
// Both probes are the project's existing ones — the archive-tag predicate every
// merge path shares, and repoSweepRefPresent (the ONE "does refs/heads/<name>
// resolve" probe, which distinguishes git's named absence from a broken repo).
// A probe that could not answer is NOT "reclaimed": the inconclusive case reads
// false and its error is deliberately not invented here (Rule 15 — the caller
// that needs the error asks repoSweepRefPresent directly).
func DecidedRefReclaimed(repo, branch string) bool {
	if branch == "" {
		return false
	}
	if !repoSweepArchiveExempt(repo, branch) {
		return false // no decision — nothing was ever owed
	}
	present, err := repoSweepRefPresent(repo, branch)
	if err != nil {
		return false // inconclusive ≠ reclaimed
	}
	return !present
}
