// Branch → task lookup: the ONE answer to "which ledger row owns this branch?"
//
// WHY THIS EXISTS. The operator and the merger both need to link a BRANCH back
// to the ledger row that produced it, and today neither can do it from the
// shell: the linkage lives in the row's deliverable blob
// ({"repo":…,"branch":…,"worktree":…}), which is only readable per-row
// (`gopi task show <id>`) or swept as a whole (`gopi task finish-cleanup` /
// the merger's attribution report). The operator's workaround was a
// task/xxx.md SIDECAR committed next to the code — a second, hand-maintained
// index of the same fact (Rule 14: one definition, never a mirror).
//
// WHY THE ANSWER IS PLURAL. The same branch name is commonly named by MORE
// than one row over its life: the in-flight run that is walking the work, and
// the COMPLETED row that already finished with it (the completion-time
// backfill, taskBackfillDeliverableBranch). Measured on the live ledger
// (2026-09-16): only the LIVE-status rows stamped at claim/carry time carry
// BOTH branch and worktree — the ordinary row is paired at completion time.
// A lookup that answered with one row would therefore have to guess which one
// the asker meant, and would return "no owner" for exactly the branch whose
// owner is the most interesting one (the run still in flight, whose status
// stamp is the row's). This file answers with EVERY owner and lets the caller
// read the status.
//
// WHAT IT DELIBERATELY DOES NOT DO. It is READ-ONLY: no claim, no status
// move, no ledger write, no notify event, no git command. It never picks a
// "winner" among the owners, and it never re-derives the deliverable blob's
// shape — the parse is the SAME parser the cleanup chain and the attribution
// classifier use (taskDeliverableBranchOrEmpty / taskDeliverableWorktree,
// finishCleanupParseDeliverable), so a blob this file cannot read is a blob
// they cannot read either (Rule 14).

package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nick/gopi/internal/config"
)

// THE RESOLUTION POLICY — WHICH OWNER ROW IS *THE* OWNER (epic
// merge-candidate-live-owner-fence, 2026-09-22).
//
// TaskForBranch answers with EVERY row that names a branch, and that plurality
// is deliberate (see this file's header): the in-flight run and the completed
// row that already finished with it commonly name the SAME branch. But a
// decision-time fence needs ONE answer — "may this branch be merged?" — and so
// does `gopi merge --status`'s owner column, which used to render whichever row
// its own loader happened to keep. Two loaders answering one question from
// their own tie-break rules is exactly the drift Rule 13/14 forbid: measured
// 2026-09-22, `gopi merge --status` printed `fix/jieba-token-absorbs-search-kw
// (completed)` while that branch's live owner was an `in_progress` row with an
// alive worker — the archive face's loader prefers the DECIDED row, which is
// right for BURIAL bookkeeping and wrong for "who owns this branch right now".
//
// TaskForBranchResolvedRow below is the ONE answer, and it is a FIXED policy
// rather than a choice left to each caller:
//
//	LIVE first — a row whose status still owes work (TaskForBranchOwner.Live:
//	pending/claimed/in_progress) OUTRANKS every other row, because the question
//	a merge fence asks is "is somebody still working here?". The archive face's
//	preference for the decided row is the opposite question ("has this branch
//	been buried under a decision?") and stays where it is. Note that LIVE is
//	NOT the complement of Terminal: `cancelled` and `verified` are neither, and
//	reading `!Terminal` as live would park a cancelled branch forever.
//
// It returns a bool rather than a sentinel TaskForBranchOwner so a row that
// carries none of its fields cannot be mistaken for "no owner" (Rule 15).
func TaskForBranchResolvedRow(lookup TaskForBranchLookup) (TaskForBranchOwner, bool) {
	for _, o := range lookup.Owners {
		if o.Live {
			return o, true
		}
	}
	if len(lookup.Owners) == 0 {
		return TaskForBranchOwner{}, false
	}
	// No live owner: the newest row of any other kind is the provenance record
	// this branch ended with (the loader orders its owners newest-first within
	// each group).
	return lookup.Owners[0], true
}

// taskForBranchRowScanMax is the runaway bound on the evidence read — the same
// shape (and the same reason) as branchAttributionRowScanMax: the read REFUSES
// rather than answering from a truncated universe (memory #2632: an age-ASC
// LIMIT dropped the live rows, so a partial scan answered wrongly and silently).
// It is deliberately far above any real ledger (measured 2026-09-16: 1455 rows
// total, 513 carrying a branch), so it stops a runaway, never a real ledger.
const taskForBranchRowScanMax = 50000

// taskForBranchRowsTotalUnknown is LedgerRowsTotal when the ledger's own row
// count could not be read. It is a SENTINEL and not 0 for Rule 15's reason: the
// renderer must be able to say "this ledger holds nothing" (a fact it measured)
// and must never say it from a count that failed.
const taskForBranchRowsTotalUnknown = -1

// taskGroundApplyReconcile folds one reconciliation pass into a lookup: the
// Rule-15b reading, the SECOND, LABELLED git channel when a live ground was
// measured, and the reconstructed owner when the evidence named one.
//
// It is ONE function because the branch lookup and its worktree sibling answer
// the same way and must not drift apart in what they do with the answer
// (Rule 14): two copies of this fold would be free to label the channel
// differently, or to drop it on one path only.
func taskGroundApplyReconcile(out *TaskForBranchLookup, q taskGroundReconcileQuery, ans taskGroundReconcileAnswer) {
	out.ReconcileNote = ans.Looked
	if ans.GroundKnown {
		// THE SECOND CHANNEL, measured: git registers a ground for this
		// selector that NO row of the ledger this answer read owns. It
		// travels as its own field so the renderer can LABEL it as a second
		// reading rather than appending it under an already-formed ledger
		// verdict (the contradiction the operator reported).
		out.Ground = &TaskGroundRegistration{
			Kind:     taskGroundReconcileKindWord(q),
			Repo:     ans.Repo,
			Worktree: ans.Worktree,
			Branch:   ans.Branch,
			Tip:      ans.Tip,
		}
	}
	if !ans.Found {
		return
	}
	out.Owners = []TaskForBranchOwner{ans.Owner}
	out.Reconciled = true
	// A reconstructed owner IS an owner: leaving the unowned-ground channel
	// standing beside an owner block would be the same contradiction the other
	// way round.
	out.Ground = nil
}

// TaskGroundRegistration is the GIT CHANNEL of a non-owner answer: the ground
// git currently registers for the selector that was asked about, which NO row
// of the ledger the answer read owns.
//
// It is a type of its own rather than more prose inside the reconciliation note
// (epic 01a0ed38). The operator's complaint was precisely that the two readings
// arrived as one block: the ledger said "no owner" in the flat absolute, and a
// trailing sentence then quietly reported that git DOES register the thing. The
// readings come from two instruments — the ledger that was opened, and
// `git worktree list` run right now — so they are carried, and rendered, as two
// LABELLED channels; a reader (or a script) can then take either one.
//
// Every field is read, never guessed: a field git could not supply is empty and
// is rendered as absence of that fact, not as a zero.
type TaskGroundRegistration struct {
	// Kind is "branch" or "worktree" — the selector the registration answers.
	Kind string `json:"kind"`
	// Repo is the repository whose `git worktree list` registers the ground.
	Repo string `json:"repo"`
	// Worktree is the registered directory (the ground itself).
	Worktree string `json:"worktree"`
	// Branch is the branch checked out there ("" for a detached registration,
	// which is a fact and not a missing value).
	Branch string `json:"branch,omitempty"`
	// Tip is that branch's commit in Repo ("" when it could not be read).
	Tip string `json:"tip,omitempty"`
}

// taskForBranchCountRows is the ledger's own row count — every row, whether or
// not it carries a deliverable. It is what tells a FRESH PROFILE (empty ledger:
// nothing was ever dispatched into it, so no row of it can name anything) from
// a populated ledger that simply does not name this branch. The two are
// different findings with different remedies, and rendering them alike is the
// blank assertion Rule 15b forbids.
//
// It runs on the SAME handle the read already opened — never a second opener
// (the read guard exists precisely because opening has a cost and a write
// effect), and it is the ONLY count in this file.
func taskForBranchCountRows(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		return 0, fmt.Errorf("task: count ledger rows: %w", err)
	}
	return n, nil
}

// TaskForBranchOwner is ONE ledger row that names a branch: the branch→task
// answer's unit. Every field is read from the row — nothing is derived that
// the row does not carry, so an operator can re-read the same fact with
// `gopi task show <id>` and get the same values (Rule 15: a verdict nobody can
// re-derive is not an explanation).
type TaskForBranchOwner struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	// SessionID is the row's session_id column — the session that submitted
	// (or, for a harness-registered run, owns) the row. Empty for a row that
	// carries none, which is a fact, not a missing field.
	SessionID string `json:"session_id"`
	// Worktree is deliverable.worktree — empty for a row that recorded only a
	// branch, which is the ordinary in-flight shape until the first
	// git-touching round backfills it.
	Worktree string `json:"worktree"`
	// Branch is deliverable.branch — always non-empty here (rows without one
	// are not candidates at all).
	Branch string `json:"branch"`
	// Repo is deliverable.repo (the repo the branch lives in), "" when the
	// blob does not name one.
	Repo string `json:"repo"`
	// Provenance names WHICH axis answered for this row when it was not the
	// row's own primary axes — the SECONDARY ground a run created
	// (deliverable.grounds). It is empty for the ordinary case (the row names
	// the branch itself), and non-empty exactly when an operator reading the
	// answer with `gopi task show <id>` would otherwise find the row's
	// worktree/branch do not match the question (Rule 15: the answer must be
	// re-derivable, not surprising).
	Provenance string `json:"provenance,omitempty"`
	// axes is EVERY branch axis this row carries — its primary branch (when it
	// has one) followed by the branch of each ground it created, de-duplicated
	// (taskDeliverableOwnerAxes, Rule 14).
	//
	// It is UNEXPORTED and outside the wire shape on purpose: it is not part of
	// the lookup's answer (a caller reads Owners, Provenance and the status
	// fields), it is the enumeration's own material. It exists because the
	// candidate-set question ("which rows name this branch, through which axis")
	// must be answerable from the ONE scan that already parsed the blob — the
	// alternative is the second reader whose divergence is the whole subject of
	// task_owner_axes.go.
	axes []string
	// Tags and Summary are the row's OWN text, read by the same scan that
	// already had the row in hand — the material the archive gate's SUCCESSOR
	// predicate needs (task_rework_successor.go: a rework row names the branch
	// it replaces in its SUMMARY, and the rework lane in its TAGS, never in the
	// deliverable).
	//
	// They are UNEXPORTED for the same reason `axes` is: this lookup's answer
	// is "who owns the branch", and neither field is part of it. They exist so
	// that question can be ANSWERED MORE PRECISELY from one scan instead of the
	// second reader (a fresh SQL query for tags by task id) that this package
	// keeps deleting.
	Tags    string
	Summary string
	// HolderProvenDead is the shared holder verdict for this row
	// (finishCleanupDeadLocalHolder — the SAME classifier the finish-cleanup
	// arms and the attribution sweep consult, Rule 14). It is carried rather
	// than re-derived so the renderer can say "the runner is gone" without a
	// second definition of gone.
	HolderProvenDead bool `json:"holder_proven_dead"`
	// ClaimedBy is the row's claim stamp (host:pid), the value behind
	// HolderProvenDead.
	ClaimedBy string `json:"claimed_by"`
	// Terminal records that the row's status ends its life (taskTerminalStatus
	// — completed/failed). It is what lets the renderer mark the completion-
	// stamped owner apart from the live one without re-testing the status
	// spelling at a second site.
	Terminal bool `json:"terminal"`
	// Live records that the row still OWES work through this branch
	// (isLiveTaskStatus — pending/claimed/in_progress, the ONE list, Rule 14).
	//
	// It is carried as its own field rather than derived from Terminal, because
	// the two are NOT complements: `cancelled` and `verified` are neither live
	// nor terminal-by-that-predicate (a cancelled row never had a delivery
	// promise; a verified row is the server's acceptance state). A caller that
	// read `!Terminal` as "live" would park a cancelled branch forever — the
	// opposite failure of the one the merge fence exists to fix.
	Live bool `json:"live"`
}

// TaskForBranchQuery is the parsed, testable input of one lookup: the branch
// asked about plus the optional repo narrowing. It exists so the resolution
// policy can be unit-tested with no ledger and no process (the same split the
// read-path suite uses for `task list`).
type TaskForBranchQuery struct {
	Branch string
	// Repo, when non-empty, narrows the answer to owners whose deliverable
	// names that repo — the same branch name legitimately exists in several
	// repos (two hosts, two deliverables), and a caller who knows its repo
	// must be able to say so rather than reading a foreign row as its own.
	Repo string
}

// TaskForBranchLookup is the whole answer: the owners, whether the ledger was
// readable at all, and how many rows carried a branch (the evidence size).
//
// LedgerReadable is carried OUT of the engine rather than being folded into an
// empty owner list: "this branch has no owner" and "the ledger could not be
// read" are different answers and must never render the same way (Rule 15 —
// the same direction branchAttribution takes when it refuses to conclude from
// an unreadable ledger).
type TaskForBranchLookup struct {
	Branch string `json:"branch"`
	// Worktree carries the worktree path asked about by
	// TaskForWorktree (the worktree sibling of this lookup). It is EMPTY for a
	// branch lookup — the two lookups share one answer shape, and this field is
	// what tells the renderer which selector the answer is about, so a worktree
	// answer is never labelled "owners of branch …" (Rule 15: an answer that
	// misnames its own question is not an answer).
	Worktree string               `json:"worktree,omitempty"`
	Repo     string               `json:"repo,omitempty"`
	Owners   []TaskForBranchOwner `json:"owners"`
	// LedgerPath is the ledger THIS answer was read from — the caller's --db
	// when it named one, the resolved profile ledger otherwise. It is carried
	// so the no-owner claim can be SCOPED to the ledger that actually answered:
	// "no ledger row names branch X" is a statement about ONE database, and a
	// host carries several (each sandbox profile owns its own, the operator's
	// profile owns another), so an unqualified denial reads as a claim about
	// the machine and contradicts the ground git holds right beside it (epic
	// 01a0ed38, the measured window). It is the ONE resolved path — the same
	// variable the reader opened (Rule 14).
	LedgerPath string `json:"ledger_path,omitempty"`
	// Host is the machine THIS answer was read on (os.Hostname at read time).
	// It is the second half of the scope: a ledger PATH names a database, and
	// two hosts commonly hold the SAME path literal (`/home/<user>/.gopi/
	// memory.db` on a two-host install), so the path alone does not say which
	// machine's rows were consulted. Carried as a FIELD, never folded into the
	// verdict sentence, so a caller can read it without parsing prose (epic
	// 01a147e5: a completed, correctly-cleaned run was escalated as ownerless
	// because "no ledger row names branch X" was read as a claim about the
	// MACHINE while every other host's ledger was never opened). Empty means
	// the OS reported no hostname — reported as unknown, never as this host
	// being nobody (Rule 15).
	Host string `json:"host,omitempty"`
	// LedgerRowsTotal is how many rows this ledger holds AT ALL — every row,
	// not only the ones carrying a deliverable. It is what separates "this
	// ledger is EMPTY (a fresh profile: nothing here has been dispatched, so no
	// row of it can name anything)" from "this ledger has rows and none of them
	// names this branch" — two different findings with two different remedies
	// (dispatch into THIS profile vs. read the profile that dispatched it), and
	// rendering them alike is the blank assertion Rule 15b forbids.
	//
	// It is `RowsTotalUnknown` (-1) when the count could not be read: an
	// unmeasurable total is reported as unknown, never as 0 (Rule 15).
	LedgerRowsTotal int  `json:"ledger_rows_total"`
	Readable        bool `json:"ledger_readable"`
	RowsWithBranch  int  `json:"rows_with_branch"`
	// Reconciled reports that the owner list was reconstructed from EVIDENCE
	// rather than read from a recorded axis (epic 01a0e074, historical
	// owner-less grounds): the ledger names no row for the selector, so the
	// answer came from the ground's git registration plus the session that
	// minted it (task_ground_reconcile.go). It is a separate flag, never a
	// silent shape inside Owners, because a reader must be able to tell a
	// RECORDED owner from a RECONSTRUCTED one at a glance (Rule 15).
	Reconciled bool `json:"reconciled,omitempty"`
	// ReconcileNote carries the Rule-15b sentence of a reconciliation that
	// found a LIVE ground but could name no row: WHAT was looked for (the
	// registration, the rows alive at its birth, the minting command in their
	// sessions) and WHERE. Empty when no reconciliation was warranted.
	ReconcileNote string `json:"reconcile_note,omitempty"`
	// Ground is the SECOND, LABELLED channel of a NON-owner answer: the ground
	// git registers for this selector that NO row of the ledger this answer
	// read owns. It is deliberately a FIELD rather than a sentence inside the
	// ledger denial (epic 01a0ed38): the two readings come from two different
	// instruments — the ledger that was opened, and `git worktree list` run
	// right now — and a reader must see them as two channels, never as one
	// verdict with a contradiction under it. A lookup whose Owners is non-empty
	// carries none by construction (nothing is unowned).
	Ground *TaskGroundRegistration `json:"ground,omitempty"`
	// Reconciled reports that the owner list was reconstructed from EVIDENCE
}

// TaskForBranch resolves a branch to every ledger row that names it.
//
// It is a PURE READ of the ledger: the SQL is a single SELECT, the whole scan
// runs inside one read transaction, and no statement in this file writes.
// An unreadable ledger returns Readable=false (never an error that a caller
// could confuse with an empty answer, and never a partial owner list).
func TaskForBranch(q TaskForBranchQuery, dbPath string) (TaskForBranchLookup, error) {
	out := TaskForBranchLookup{Branch: strings.TrimSpace(q.Branch), Repo: strings.TrimSpace(q.Repo),
		LedgerRowsTotal: taskForBranchRowsTotalUnknown}
	if out.Branch == "" {
		return out, fmt.Errorf("task: branch must not be empty — there is nothing to look up")
	}
	ctx := context.Background()
	path := memoryDBPath()
	if strings.TrimSpace(dbPath) != "" {
		path = dbPath
	}
	// THE LEDGER THIS ANSWER IS ABOUT, carried out with the answer: every
	// sentence below is a claim about ONE database, and the reader has to be
	// able to tell which one answered (Rule 15 — an unscoped "no row names
	// branch X" reads as a claim about the machine, and the machine commonly
	// holds several ledgers).
	out.LedgerPath = path
	// THE MACHINE THAT LEDGER SITS ON. A ledger path cannot say WHICH host's
	// rows these are — a two-host install commonly carries the SAME literal
	// path on both machines — and that is the gap that made a completed,
	// correctly-cleaned run read as ownerless HERE (epic 01a147e5). Read once,
	// through the shared reader, so the verdict line and the "not consulted"
	// sentence can never name two different machines (Rule 14).
	out.Host = taskForBranchHostname()
	// THE READ GUARD, before any opener: this lookup is a PURE READ, so it must
	// not bring its subject into existence. An ABSENT ledger names no owner —
	// that is the empty answer, not a read failure — and creating the
	// operator's own ledger from a read path is the write this contract exists
	// to forbid (see ledger_read_guard.go for the measured cost of the
	// knowledge-base first open that used to happen here).
	if ledgerMissingForRead(path) {
		// Readable here, deliberately: this is not "I could not look" (that
		// stays reserved for a file that exists and cannot be read as a
		// ledger) — there is no ledger, so no row names the branch, and that
		// IS the answer. Reporting it as unreadable would park every branch on
		// a host whose ledger has not been built yet and freeze the queue.
		//
		// The size of that ledger is 0 BY MEASUREMENT (a file that does not
		// exist holds no rows), so the renderer can say the ledger is EMPTY
		// rather than "N rows carry a branch" (Rule 15b).
		out.Readable = true
		out.LedgerRowsTotal = 0
		return out, nil
	}
	db, err := openTasksDB(ctx, path)
	if err != nil {
		// An unopenable ledger is NOT "this branch has no owner": report the
		// read failure as a read failure (Rule 15).
		return out, fmt.Errorf("task: open ledger %s: %w", path, err)
	}
	defer db.Close()

	// The scan takes EVERY row carrying a non-empty deliverable and lets the
	// parsers decide — no `LIKE '%branch%'` needle. The needle is this
	// family's measured truncation bug twice over (memory #2632; the
	// attribution loader's own comment), and a needle on a JSON blob would
	// additionally match a branch name appearing in the REPO or WORKTREE
	// string.
	scan, err := taskDeliverableScanRows(ctx, db, func(branch, worktree, repo string, grounds []taskDeliverableGround) (bool, string) {
		// THE PRIMARY AXIS FIRST, then the SECONDARY grounds (epic 01a0dff1): a
		// ground the run CREATED and recorded on the plural axis names this row
		// exactly as the row's own branch does — that is the whole point of
		// recording it, and before the axis existed such a branch answered "no
		// owner" while its directory was live.
		//
		// The primary arm asks taskOwnerBranchMatchesAxis, the package's ONE
		// spelling of "does this row's axis NAME the branch asked about" — the
		// archive face reads the same function, so the two faces cannot form
		// different membership sets (task_owner_axes.go).
		if taskOwnerBranchMatchesAxis(branch, out.Branch) {
			return out.Repo == "" || strings.TrimSpace(repo) == out.Repo, ""
		}
		for _, g := range grounds {
			if !taskOwnerBranchMatchesAxis(g.Branch, out.Branch) {
				continue
			}
			if out.Repo != "" && !sameWorkingPath(strings.TrimSpace(g.Repo), out.Repo) {
				continue
			}
			// WHICH AXIS ANSWERED, said out loud: the row matched through a
			// ground it CREATED, so the sentence names that ground AND the
			// row's own — a reader checking with `gopi task show <id>` sees a
			// different primary pair and must be told why (Rule 15).
			return true, taskDeliverableGroundProvenance(worktree, branch, out.Branch)
		}
		return false, ""
	})
	if err != nil {
		return out, fmt.Errorf("task: scan ledger for branch %q: %w", out.Branch, err)
	}
	out.Readable = scan.Readable
	out.RowsWithBranch = scan.RowsWithBranch
	out.Owners = scan.Owners

	// HOW BIG THE LEDGER THIS ANSWERED FROM IS — its own count, over every row
	// (not just the ones carrying a deliverable). Without it "N rows carry a
	// branch, none names this one" cannot be told apart from "this ledger is a
	// fresh profile and holds nothing at all", and those are different answers
	// with different remedies (Rule 15b). A count that cannot be read is
	// reported as UNKNOWN, never as 0 — "the ledger is empty" is itself a claim.
	if n, countErr := taskForBranchCountRows(ctx, db); countErr != nil {
		out.LedgerRowsTotal = taskForBranchRowsTotalUnknown
	} else {
		out.LedgerRowsTotal = n
	}

	// NO RECORDED OWNER — MEASURE THE GROUND BEFORE CLAIMING ABSENCE (epic
	// 01a0e074). A row minted before the plural-grounds producer landed records
	// no ground at all, so a LIVE branch/worktree answers "no owner" while
	// git holds its registration and the run's own session records minting it.
	// The reconciliation (task_ground_reconcile.go) names that row from that
	// evidence, and its whole reading is REPORTED (ReconcileNote) whether or
	// not it names one, so "no owner" is never a blank assertion (Rule 15b).
	if len(out.Owners) == 0 && out.Readable {
		q := taskGroundReconcileQuery{Branch: out.Branch, Repo: out.Repo}
		ans := taskGroundReconcile(ctx, db, q)
		taskGroundApplyReconcile(&out, q, ans)
	}

	// Order: the IN-FLIGHT owner first, then terminal ones, newest task_id
	// first inside each group (the ONE ordering opinion — see
	// orderTaskForBranchOwners). A caller reading the JSON gets the same order.
	out.Owners = orderTaskForBranchOwners(out.Owners)
	return out, nil
}

// taskDeliverableScan is ONE read of the ledger's deliverable-carrying rows:
// every row the matcher accepted (as a TaskForBranchOwner), whether the ledger
// was readable at all, and how many rows carried a branch (the evidence size).
type taskDeliverableScan struct {
	Owners         []TaskForBranchOwner
	Readable       bool
	RowsWithBranch int
}

// taskDeliverableScanRows scans every row with a non-empty deliverable and
// keeps the ones matcher accepts.
//
// It is the ONE scan behind both deliverable→row lookups (branch and worktree):
// two loaders walking the same table with their own SQL and their own owner
// construction is exactly the drift Rules 13/14 forbid — the second one would
// answer the same question from a shape the first one never validated.
//
// The scan takes EVERY row carrying a non-empty deliverable and lets the
// parsers decide — no `LIKE '%needle%'`. The needle is this family's measured
// truncation bug twice over (memory #2632; the attribution loader's own
// comment), and a needle on a JSON blob would additionally match a branch name
// appearing in the REPO or WORKTREE string.
//
// Two rows per branch is a real shape (see this file's header): the matcher is
// what narrows, and it receives the row's own parsed fields (branch, worktree,
// repo) so a caller never re-parses the blob at a second site.
// The matcher receives the row's PRIMARY axes plus its SECONDARY grounds: the
// grounds are a real answer axis (the branch/worktree a run created), so the
// matcher must see them, and it sees them from the ONE parser (Rule 13/14) —
// never by re-reading the blob at a second site.
func taskDeliverableScanRows(ctx context.Context, db *sql.DB,
	matcher func(branch, worktree, repo string, grounds []taskDeliverableGround) (bool, string)) (taskDeliverableScan, error) {
	var out taskDeliverableScan
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, COALESCE(session_id,''), COALESCE(deliverable,''),
		        COALESCE(claimed_by,''), COALESCE(`+tasksProcPidColumnName+`,0),
		        COALESCE(`+tasksProcStartColumnName+`,0), COALESCE(`+tasksProcCmdColumnName+`,''),
		        COALESCE(tags,''), COALESCE(summary,'')
		   FROM tasks
		  WHERE COALESCE(deliverable,'') <> ''`)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	out.Readable = true
	for rows.Next() {
		var id, status, sessionID, deliverable, claimedBy, procCmd, tags, summary string
		var procPid, procStart int64
		if err := rows.Scan(&id, &status, &sessionID, &deliverable, &claimedBy, &procPid, &procStart, &procCmd, &tags, &summary); err != nil {
			return out, fmt.Errorf("task: read ledger row: %w", err)
		}
		branch := taskDeliverableBranchOrEmpty(deliverable)
		grounds := taskDeliverableGroundsOf(deliverable)
		if branch == "" && len(grounds) == 0 {
			continue // this row claims no ground at all: it is not a candidate
		}
		out.RowsWithBranch++
		if out.RowsWithBranch > taskForBranchRowScanMax {
			return out, fmt.Errorf("task: more than %d rows carry a branch — refusing to answer from a truncated universe",
				taskForBranchRowScanMax)
		}
		parsed := taskDeliverableParse(deliverable)
		worktree := parsed.Worktree
		repo := parsed.Repo
		// THE AXIS ENUMERATION, derived once per row from the values the scan
		// already parsed (task_owner_axes.go): the primary branch plus every
		// ground the run created. The matcher below is asked about ONE of them;
		// the accept-all matcher the enumeration drives keeps the whole set.
		axes := taskDeliverableOwnerAxes(branch, grounds)
		ok, provenance := matcher(branch, worktree, repo, grounds)
		if !ok {
			continue
		}
		out.Owners = append(out.Owners, TaskForBranchOwner{
			TaskID:           id,
			Status:           status,
			SessionID:        sessionID,
			Worktree:         worktree,
			Branch:           branch,
			Repo:             repo,
			Provenance:       provenance,
			ClaimedBy:        claimedBy,
			Terminal:         taskTerminalStatus(status),
			Live:             isLiveTaskStatus(status),
			HolderProvenDead: finishCleanupDeadLocalHolder(status, claimedBy, procIdentity{Pid: int(procPid), StartTicks: procStart, Cmd: procCmd}),
			axes:             axes,
			Tags:             tags,
			Summary:          summary,
		})
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// orderTaskForBranchOwners is the ONE ordering opinion of the deliverable→row
// family: the IN-FLIGHT owner first, then terminal ones, newest task_id first
// inside each group. The operator asking "who owns this branch/worktree" wants
// the run that is standing on it; the completed owner is the provenance record.
// It lived inline in TaskForBranch until the worktree lookup needed the same
// order (Rule 14: a second copy would be free to disagree with this one).
func orderTaskForBranchOwners(owners []TaskForBranchOwner) []TaskForBranchOwner {
	sort.SliceStable(owners, func(i, j int) bool {
		a, b := owners[i], owners[j]
		if a.Terminal != b.Terminal {
			return !a.Terminal
		}
		return a.TaskID > b.TaskID
	})
	if owners == nil {
		owners = []TaskForBranchOwner{}
	}
	return owners
}

// TaskForBranchJSON renders a lookup as one JSON object (scripts/CI).
func TaskForBranchJSON(l TaskForBranchLookup) (string, error) {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return "", fmt.Errorf("task: for-branch json: %w", err)
	}
	return string(b) + "\n", nil
}

// The no-owner answer is a DISTINCT exit code, not 0 and not the generic
// failure 1: "this branch has no owner" is a legitimate, script-consumable
// answer (the operator's "has this branch been adopted at all?" gate), while
// 1 stays the I/O-and-usage failure code. The value is 4 because 2 and 3 are
// taken by the surrounding conventions (`workerUnexpectedArgExitCode` = 2, the
// agent guard refusal / task-status "unverifiable" = 3), and a code that
// collided with a refusal would make a script unable to tell "no owner" from
// "I was not allowed to look" (Rule 15).
const (
	// TaskForBranchExitOwned: the branch has at least one owner.
	TaskForBranchExitOwned = 0
	// TaskForBranchExitNoOwner: the ledger was read and NO row names this
	// branch — the named, distinct non-zero answer.
	TaskForBranchExitNoOwner = 4
)

// The no-owner answer's sentences. Each is named once (Rule 1b) because every
// one of them is read by an operator and asserted by the tests, so a drift
// between what the verb promises and what it prints has to be a compile/assert
// failure rather than a wording change nobody notices.
const (
	// taskForBranchNoOwnerMarker is the NAMED prefix of the no-owner answer: the
	// word an operator (or a script matching on stdout) reads to tell "there is
	// no owner" from "the command printed nothing" (Rule 15). It is defined once
	// and asserted by the test, so the answer cannot quietly become a blank line.
	taskForBranchNoOwnerMarker = "no owner"

	// taskForBranchNoOwnerFmt is the verdict line itself. Its claim is scoped to
	// the LEDGER by the line that follows it, which names the database this
	// answer was read from — "no ledger row names X" is a statement about ONE
	// ledger, and the operator's host carries several.
	taskForBranchNoOwnerFmt = "%s: no ledger row names %s %s\n"
	// taskForBranchLedgerScopeFmt names the database the claim is about. It is
	// the ONE place the scope is stated, so the empty-ledger line, the count
	// line and the git channel below it are all read as facts about the same
	// database (Rule 15: an unscoped denial reads as a claim about the machine).
	taskForBranchLedgerScopeFmt = "  ledger: %s — this LEDGER only (a host carries one per profile; --db names another)\n"
	// taskForBranchHostScopeFmt names the MACHINE the ledger above sits on, so
	// the two halves of the scope travel together: WHICH database, and WHICH
	// host's copy of it. One line, because "ledger /home/x/.gopi/memory.db" is
	// the same path literal on both hosts of a two-host install — the path alone
	// does not answer "whose rows were consulted".
	taskForBranchHostScopeFmt = "  host:   %s — the machine that ledger was read on\n"
	// taskForBranchHostUnknownFmt is the honest limit of the line above: the OS
	// reported no hostname, so the machine is UNKNOWN — never blank, and never
	// silently "this one" (Rule 15: an unmeasured fact is not reported as a
	// measured one).
	taskForBranchHostUnknownFmt = "  host:   (this machine's hostname could not be read, so WHICH host's ledger answered is UNKNOWN)\n"
	// taskForBranchNotConsultedFmt states the BOUNDARY the denial stops at: a
	// ledger is per host AND per profile, and this answer opened exactly one of
	// them. It is the sentence whose absence cost a whole agent session (epic
	// 01a147e5). It names the switch an operator flips to make rows cross hosts
	// — the one knob that decides whether another host's ledger could have named
	// this branch — because the reader's next move depends on it, and it is
	// phrased as a fact about the switch, never as advice to change propagation.
	taskForBranchNotConsultedFmt = "  other hosts' ledgers were NOT consulted (row exchange is %s): a branch " +
		"dispatched on another host reads as ownerless HERE — `gopi task send --task-id <id>` lands one row " +
		"in a peer's ledger, and `ssh <peer> 'gopi task show <id>'` is the only proof it is there\n"
	// taskSyncOnValue / taskSyncOffValue are the two renderings of the switch's
	// VALUE inside the boundary sentence. They are named because the sentence is
	// asserted by a test and read by an operator: a bare "on"/"off" typed at the
	// render site would be a meaning-carrying literal (Rule 1b).
	taskSyncOnValue  = "on"
	taskSyncOffValue = "off"
	// taskForBranchLedgerEmptyFmt is the EMPTY-LEDGER case, said out loud: a
	// fresh profile holds no row at all, so "none names this one" is trivially
	// true and its remedy (dispatch into THIS profile, or read the profile that
	// dispatched it) is a different one from a populated ledger's (Rule 15b).
	taskForBranchLedgerEmptyFmt = "  (that ledger holds no row at all — a fresh profile: nothing has been dispatched into it, so no row of it can name anything)\n"
	// taskForBranchLedgerTotalUnknownFmt is the honest limit of the empty case:
	// the ledger's own size could not be counted, so it is NOT reported as empty
	// (Rule 15 — "the ledger is empty" is itself a claim).
	taskForBranchLedgerTotalUnknownFmt = "  (that ledger's own row count could not be read, so whether it holds rows at all is UNKNOWN)\n"
	// taskForBranchRowsCarryFmt / …RepoFmt are the populated-ledger half: rows
	// exist and carry branches, and none of them names the selector asked about.
	taskForBranchRowsCarryFmt     = "  (%d rows carry a branch; none names this one)\n"
	taskForBranchRowsCarryRepoFmt = "  (%d rows carry a branch; none names this %s in repo %s — drop --repo to look across repos)\n"
	// taskForBranchNoBranchRowsFmt is the third populated-ledger case: the ledger
	// has rows, but not one of them records a ground — so there is nothing for
	// the selector to match, and the operator's remedy is to make the run record
	// its ground, not to re-read the ledger.
	taskForBranchNoBranchRowsFmt = "  (that ledger holds %d row(s), and not one of them carries a branch — nothing here can name a selector at all)\n"

	// TaskGroundChannelLabel opens the SECOND, LABELLED channel of a non-owner
	// answer. It exists so the git registration is never a footnote under a
	// verdict already formed in the reader's mind (epic 01a0ed38): the label
	// states the instrument (git, read now, not the ledger) and the finding (a
	// ground NO row of THAT ledger owns) in one line, before the facts.
	TaskGroundChannelLabel = "git registers this %s — a ground that NO row of that ledger owns (read from git NOW, never recorded in the ledger):\n"
	// taskGroundChannelLineFmt is the channel's one fact line: repository,
	// ground, directory, tip.
	taskGroundChannelLineFmt = "  %s registers %s %q at %s (tip %s)\n"
	// taskGroundChannelNoTipFmt is the same line with the tip unreadable: the
	// tip is reported as absent, never as a zero-length sha (Rule 15).
	taskGroundChannelNoTipFmt = "  %s registers %s %q at %s (its tip could not be read)\n"
)

// taskSyncSwitchLabel renders the row-exchange switch as `<key>=<value>`, from
// the switch's OWN key constant and its ONE reader (Rule 2/14): the boundary
// sentence therefore names the same knob `gopi config` documents, and cannot
// claim a value the switch would not report. It is a FACT ABOUT THIS HOST's
// configuration, never advice to change it — propagation is the operator's
// switch (epic 01a147e5's explicit non-goal).
func taskSyncSwitchLabel() string {
	value := taskSyncOffValue
	if config.TaskSyncEnabled() {
		value = taskSyncOnValue
	}
	return config.TaskSyncSettingsKey + "=" + value
}

// taskForBranchHostname is the host THIS lookup ran on — the machine half of a
// no-owner answer's scope. It is ONE function because two renderers (the branch
// lookup and its worktree sibling) and the sentences that print the host must
// name the same machine (Rule 14); and because os.Hostname's failure has a
// meaning here that must not be improvised at two call sites: an EMPTY string
// says "this machine could not be named", which the renderer reports as UNKNOWN
// rather than as a blank (Rule 15 — an unmeasured fact is never reported as a
// measured one).
func taskForBranchHostname() string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return ""
	}
	return h
}

// TaskForBranchExitCode is the process exit code for a lookup: 0 when the
// branch has an owner, TaskForBranchExitNoOwner when it has none. It is a
// function of the ANSWER (not of the renderer or of os.Exit), so the contract
// "an unanswerable query must not look like an empty answer" is unit-testable
// without a subprocess.
func TaskForBranchExitCode(l TaskForBranchLookup) int {
	if len(l.Owners) == 0 {
		return TaskForBranchExitNoOwner
	}
	return TaskForBranchExitOwned
}

// TaskForBranchReport renders a lookup for an operator at a terminal. The
// no-owner case prints a NAMED answer on stdout and returns it as the same
// string the renderer would have produced — never an empty string, and never
// a success-looking blank line (Rule 15: an unanswerable query must not look
// like an empty answer).
//
// THE NO-OWNER ANSWER IS SCOPED, AND ITS SECOND CHANNEL IS LABELLED (epic
// 01a0ed38). The denial names the LEDGER it read, states whether that ledger is
// empty or simply does not name this selector, and — when git registers a
// ground no row of that ledger owns — reports git as a SEPARATE channel under
// its own label rather than appending it to an already-formed verdict. The
// measured defect this fixes: the unqualified "no owner: no ledger row names
// branch X" followed by a reconciliation sentence proving git holds the ground
// read as one block contradicting itself.
func TaskForBranchReport(l TaskForBranchLookup) string {
	var b strings.Builder
	what, subject := taskOwnerSubject(l)
	if len(l.Owners) == 0 {
		fmt.Fprintf(&b, taskForBranchNoOwnerFmt, taskForBranchNoOwnerMarker, what, subject)
		if l.LedgerPath != "" {
			// WHICH LEDGER THIS IS A CLAIM ABOUT. Stated before the size and the
			// git channel, so every line below is read as a fact about ONE
			// database (Rule 15).
			fmt.Fprintf(&b, taskForBranchLedgerScopeFmt, l.LedgerPath)
		}
		// WHICH MACHINE, and then the boundary: the ledger above is ONE host's
		// copy, and no other host's ledger was opened. Both sentences sit
		// directly under the denial, before the size line and before the git
		// channel — every fact below is a fact about THIS host's ledger, and a
		// reader must not have to infer that (epic 01a147e5).
		if strings.TrimSpace(l.Host) != "" {
			fmt.Fprintf(&b, taskForBranchHostScopeFmt, l.Host)
		} else {
			b.WriteString(taskForBranchHostUnknownFmt)
		}
		fmt.Fprintf(&b, taskForBranchNotConsultedFmt, taskSyncSwitchLabel())
		switch {
		case l.LedgerRowsTotal == 0:
			// A FRESH PROFILE: no row at all, so "none names this one" is
			// trivially true and its remedy is a different one.
			b.WriteString(taskForBranchLedgerEmptyFmt)
		case l.LedgerRowsTotal == taskForBranchRowsTotalUnknown:
			// The count failed: the ledger's own size is UNKNOWN, and "it is
			// empty" is a claim this answer did not measure (Rule 15).
			b.WriteString(taskForBranchLedgerTotalUnknownFmt)
		case l.RowsWithBranch == 0:
			// Rows exist, but none of them records a ground at all — a distinct
			// finding from "some rows carry branches and none is this one".
			fmt.Fprintf(&b, taskForBranchNoBranchRowsFmt, l.LedgerRowsTotal)
		case l.Repo != "":
			fmt.Fprintf(&b, taskForBranchRowsCarryRepoFmt, l.RowsWithBranch, what, l.Repo)
		default:
			fmt.Fprintf(&b, taskForBranchRowsCarryFmt, l.RowsWithBranch)
		}
		// THE SECOND CHANNEL, under its own label. It is rendered BEFORE the
		// reconciliation note because that note explains it (the note is the
		// reconciliation's reading of the same ground), and it is rendered at
		// all only when a LIVE registration was actually measured — never as a
		// hedge on a lookup that measured nothing.
		if g := l.Ground; g != nil {
			fmt.Fprintf(&b, TaskGroundChannelLabel, taskGroundChannelKind(g, what))
			b.WriteString(taskGroundRegistrationLine(g))
		}
		if l.ReconcileNote != "" {
			fmt.Fprintf(&b, "  %s\n", l.ReconcileNote)
		}
		return b.String()
	}
	if l.Reconciled {
		// SAY WHICH CHANNEL ANSWERED, before the owner block. A reconstructed
		// owner's worktree/branch fields describe the ROW's own ground, not the
		// one asked about, so a reader who did not see this line would read them
		// as a mismatch (Rule 15).
		fmt.Fprintf(&b, "%d owner(s) of %s %s — RECONSTRUCTED from evidence, the ledger records no row for it\n",
			len(l.Owners), what, subject)
	} else {
		fmt.Fprintf(&b, "%d owner(s) of %s %s\n", len(l.Owners), what, subject)
	}
	for _, o := range l.Owners {
		fmt.Fprintf(&b, "  task %s  status=%s%s\n", o.TaskID, o.Status, taskForBranchLiveSuffix(o))
		fmt.Fprintf(&b, "    session  %s\n", orDash(o.SessionID))
		fmt.Fprintf(&b, "    worktree %s\n", orDash(o.Worktree))
		fmt.Fprintf(&b, "    branch   %s\n", o.Branch)
		if o.Repo != "" {
			fmt.Fprintf(&b, "    repo     %s\n", o.Repo)
		}
		if o.Provenance != "" {
			fmt.Fprintf(&b, "    %s\n", o.Provenance)
		}
	}
	return b.String()
}

// taskGroundChannelKind names the selector the git channel is about. It prefers
// the selector's own KIND (a worktree channel renders "worktree", never
// "branch") and falls back to the renderer's word for the question — the same
// ONE decision taskOwnerSubject makes, so the label and the verdict above it can
// never describe two different questions (Rule 15).
func taskGroundChannelKind(g *TaskGroundRegistration, fallback string) string {
	if strings.TrimSpace(g.Kind) != "" {
		return g.Kind
	}
	return fallback
}

// taskGroundRegistrationLine renders the git channel's ONE fact line: which
// repository registers the ground, on which branch, at which directory, at
// which tip. The tip has its own sentence for the unreadable case, so "the tip
// is empty" is never rendered as a zero-length commit (Rule 15).
func taskGroundRegistrationLine(g *TaskGroundRegistration) string {
	value := taskGroundRegistrationValue(g)
	if strings.TrimSpace(g.Tip) == "" {
		return fmt.Sprintf(taskGroundChannelNoTipFmt, g.Repo, g.Kind, value, g.Worktree)
	}
	return fmt.Sprintf(taskGroundChannelLineFmt, g.Repo, g.Kind, value, g.Worktree, g.Tip)
}

// taskGroundRegistrationValue is the ground AS THE QUESTION ASKED IT: the
// branch for a branch selector, the directory for a worktree selector. Naming
// the branch in a worktree answer (or the path in a branch answer) would be a
// sentence about something the operator did not ask (Rule 15).
func taskGroundRegistrationValue(g *TaskGroundRegistration) string {
	if g.Kind == deliverableSelectorWorktreeKind {
		return g.Worktree
	}
	if strings.TrimSpace(g.Branch) != "" {
		return g.Branch
	}
	return g.Worktree
}

// taskOwnerSubject renders the question an answer is about as (what, subject):
// ("branch", "feat/x") or ("worktree", "/path/wt"). ONE place decides which
// selector a lookup answered, so the header line and the no-owner line can
// never disagree with the answer they describe — and so a worktree answer is
// never mislabelled as a branch answer (Rule 15). A lookup that names NEITHER
// (the zero value) reads as an empty branch, which is what the branch verb
// passes when it refused its query before the lookup.
func taskOwnerSubject(l TaskForBranchLookup) (what, subject string) {
	if l.Worktree != "" {
		what, subject = "worktree", l.Worktree
	} else {
		what, subject = "branch", l.Branch
	}
	if l.Repo != "" {
		subject += " (repo " + l.Repo + ")"
	}
	return what, subject
}

// taskForBranchLiveSuffix renders the holder verdict that matters to the
// reader: whether the owner's run is STILL GOING. It is derived from the
// shared classifier's boolean rather than from a second reading of the status
// string (Rule 14).
func taskForBranchLiveSuffix(o TaskForBranchOwner) string {
	if o.Terminal {
		return "  (ended)"
	}
	if o.HolderProvenDead {
		return "  (runner gone" + taskForBranchClaimSuffix(o.ClaimedBy) + ")"
	}
	return "  (in flight" + taskForBranchClaimSuffix(o.ClaimedBy) + ")"
}

// taskForBranchClaimSuffix renders the claim stamp of a NON-terminal owner:
// the evidence an operator re-probes (Rule 15 — a verdict without its evidence
// is a self-report).
func taskForBranchClaimSuffix(claimedBy string) string {
	if strings.TrimSpace(claimedBy) == "" {
		return ""
	}
	return ", claimed by " + claimedBy
}
