package tools

// task_row_repo_products.go — the arm that measures the repository the ROW
// declares, and reports a product that landed OUTSIDE the ground.
//
// THE DEFECT THIS CLOSES (measured 2026-11-22, row 01a20059-d6c4). That row's
// ground was a clone of the host's dev_repo (origin=peer1:/home/user/work/gopi)
// while the row's own `deliverable.repo` named /home/user/work/sudoku. The run
// wrote NINE product files into sudoku, and every close-out arm measured the
// GROUND instead:
//
//	the delivery leg      → "carries no commit to deliver" (a true reading of a
//	                        directory the products never entered)
//	the delivery proof    → could not find origin/master in the row's repo, so
//	                        verdict=delivery-proof-unverified
//	the Rule 30 dirt arm  → measured the ground, which was CLEAN, so it passed
//
// Three arms, three measurements, none of them about the directory the row said
// its work lived in — and the row closed `completed` with the products outside
// every ref the system can see. The gap is not "the work was uncommitted"
// (a later commit put it in; measured the same day: ac5eb28 carries all nine,
// and that repository has NO remote at all — a separate finding, the operator's
// call). The gap is that NOTHING MEASURED THE ROW'S OWN REPOSITORY.
//
// WHAT THIS ARM IS: a RECORD, never a refusal. It runs at the same close-out
// gate the Rule 30 arms run from, takes ONE reading (`git -C <row repo> status
// --porcelain`), attributes the entries it can prove belong to this run (the
// window proof the main-checkout arm already uses — reclaimEntryTouchedAfter +
// reclaimRunWindowStart), and files ONE attributed trail entry plus one stderr
// line. It touches nothing: no delete, no move, no `git add`, no commit, no
// stash, no chmod.
//
// WHY IT MUST NOT REFUSE, stated because "just refuse it" is the obvious next
// idea: a row may legitimately declare a repository it does not commit in (an
// investigation whose product is a report, a row whose work lives in the ground
// and whose repo is only a reference), and the Rule 30 gate's binding refusal is
// load-bearing for a DIFFERENT shape (uncommitted work in the run's OWN ground).
// Adding a second refusal here would refuse those legitimate rows and would
// WEAKEN the first gate's meaning by teaching operators to waive it. Rule 15's
// loud half is satisfied by a record the operator can read.
//
// WHEN IT IS SILENT, and each silence is a shape it must not speak for:
//
//	· the row declares no repository           — there is nothing to measure;
//	· the declared path is not a live git
//	  work tree                                — an unmeasurable repository is
//	                                             not evidence (the diagnostic
//	                                             says which of the two failed);
//	· the row's ground IS that same repository — the products are IN the ground,
//	  (same git common dir, or the ground is where Rule 30's and the delivery
//	  a clone of it)                            arms already look; a second
//	                                             reporter there would be noise;
//	· nothing is attributable to this run      — an undated row, or a repository
//	                                             whose dirt predates the run.
//
// THE HONEST RESIDUAL, reproduced here rather than left to be discovered: the
// attribution is a TIME bound, not a lineage claim. A sibling process writing
// into the same repository during this row's window is indistinguishable from
// this row by mtime alone — the note therefore states the window it used, and an
// operator who knows better can falsify the record from the sentence. And a
// product that was COMMITTED into the row's repository leaves no dirt at all, so
// this arm cannot see that shape; it reports the one it measures and says so.
//
// RULE 35: no new dependency. One git read of a directory the ledger already
// names, one existing trail writer, one existing window reader.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TaskRowRepoProductsEvent is this arm's trail record.
//
// It is NOT a member of the `completed-unclean%` family on purpose: that family
// is the RESIDUE family (dirt IN the run's own ground) and shares one idempotence
// slot through `event LIKE '<stem>%'`, so joining it would let this record
// suppress — or be suppressed by — the Rule 30 arms it sits beside. It IS a
// member of verdictTrailEventNames (tasks.go), because it is a JUDGMENT ON the
// row: the close-out measured where the row's work actually went and is telling
// the row.
const TaskRowRepoProductsEvent = "products-in-row-repo"

// TaskRowRepoProductsMarker is the greppable token the note carries, so an
// operator scanning `task list`/`task show` output finds the row without parsing
// a sentence (the same discipline NoDeliverableMarker follows).
const TaskRowRepoProductsMarker = "[products-in-row-repo]"

// The two ground relations this arm can measure, each its own sentence because
// the two facts invite different reactions: an unidentified ground is a row
// whose record is incomplete, a different ground is the measured gap itself.
const (
	taskRowRepoGroundUnknownBasis = "the row records no ground, so the ground↔declared-repo comparison could not be made"
	taskRowRepoGroundDiffersBasis = "the row's recorded ground is a DIFFERENT repository from the one the row declares"
	taskRowRepoGroundSameBasis    = "the row's recorded ground IS this repository"
)

// taskRowRepoProductsNoteFmt is the arm's ONE sentence (Rule 14: the trail detail
// and the stderr line must say the same thing). Fields, in order: marker, row,
// entry count, the row's declared repository, the entries, the basis sentence,
// the ground as recorded, the window start.
const taskRowRepoProductsNoteFmt = "%s run %s has product(s) OUTSIDE its own ground: %d entr(ies) in the repository the row " +
	"DECLARES (%s) were written during this run — entries: %s (%s; ground recorded: %s; attributed by the row's window " +
	"starting %s). Nothing was deleted, moved, committed or stashed — this arm only measures and records, it does not " +
	"refuse the writeback; the residue arms measure the GROUND, so a product that never reached it has no other reporter"

// TaskRowRepoProductsRecord measures the row's declared repository and, when it
// can attribute a product there, files the record on the row's trail. It returns
// the filed sentence ("" when there is nothing to say) and NEVER refuses
// anything: every failure mode is reported and then swallowed (Rule 15 — a run's
// exit must not depend on our ability to read another repository).
func TaskRowRepoProductsRecord(taskID string) string {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return ""
	}
	repo, _, worktree := taskRowDeliverable(taskID)
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return ""
	}
	if liveDir(repo) == "" {
		taskNotifyLog("row-repo products %s: the declared repository %s is not a live directory — nothing was measured",
			taskID, repo)
		return ""
	}
	if _, err := taskGitRepoIdentityKey(repo); err != nil {
		taskNotifyLog("row-repo products %s: the declared path %s is not a readable git work tree (%v) — nothing was measured",
			taskID, repo, err)
		return ""
	}
	basis := taskRowRepoProductsGroundBasis(repo, worktree)
	if basis == taskRowRepoGroundSameBasis {
		return ""
	}
	porcelain, err := repoSweepGit(repo, taskRowRepoStatusVerb, taskRowRepoPorcelainFlag)
	if err != nil {
		taskNotifyLog("row-repo products %s: `git status --porcelain` in %s failed: %v", taskID, repo, err)
		return ""
	}
	window := reclaimRunWindowStart(taskID)
	if window == reclaimRunNoWindow {
		taskNotifyLog("row-repo products %s: the row carries no clock, so no entry in %s can be attributed to it — nothing was recorded",
			taskID, repo)
		return ""
	}
	entries := taskRowRepoProductsEntries(repo, porcelain, window)
	if len(entries) == 0 {
		return ""
	}
	note := taskRowRepoProductsNote(taskID, repo, worktree, entries, basis, window)
	if taskRowRepoProductsFiled(taskID) {
		// Idempotent: a re-landed writeback of the same run does not drip a
		// second identical record into the row's trail (the family's own guard
		// shape, on this arm's own event).
		return note
	}
	ctx, cancel := contextWithReclaimGitTimeout()
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		// The trail is the carrier; without it the note still goes to stderr
		// (Rule 15: a degradation is never swallowed).
		taskNotifyLog("row-repo products record %s: %v", taskID, err)
		fmt.Fprintln(os.Stderr, "[task] "+note)
		return note
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, TaskRowRepoProductsEvent, note)
	taskNotifyLog("%s", note)
	return note
}

// taskRowRepoProductsGroundBasis answers WHICH relation the row's recorded
// ground has to the repository the row declares — and the answer is measured,
// never assumed (Rule 15: a comparison that could not be made says so, and the
// caller renders that fact instead of reporting a difference it did not see).
//
// TWO proofs of "the same repository", because this host produces both shapes:
//
//	① the same git common dir — two checkouts (a main checkout and its linked
//	   worktree) that git itself calls one repository;
//	② the ground is a CLONE of the row's repository — its `origin` names the
//	   row's path. That is the ordinary gopi shape (a clone ground of the row's
//	   repo), and the products of such a run are in the ground, where Rule 30 and
//	   the delivery arms already measure them; a second reporter would be noise.
func taskRowRepoProductsGroundBasis(repo, ground string) string {
	ground = strings.TrimSpace(ground)
	if ground == "" || liveDir(ground) == "" {
		return taskRowRepoGroundUnknownBasis
	}
	repoKey, repoErr := taskGitRepoIdentityKey(repo)
	groundKey, groundErr := taskGitRepoIdentityKey(ground)
	if repoErr == nil && groundErr == nil && repoKey == groundKey {
		return taskRowRepoGroundSameBasis
	}
	if url, err := repoSweepGit(ground, taskRowRepoRemoteVerb, taskRowRepoGetURLVerb, deliveryProofOriginRemote); err == nil {
		if taskRowRepoURLNamesPath(url, repo) {
			return taskRowRepoGroundSameBasis
		}
	}
	return taskRowRepoGroundDiffersBasis
}

// taskRowRepoURLNamesPath reports whether a remote address names the given local
// directory: the path part of an `https://`, `ssh://` or scp-like
// (`user@host:/path`) address is compared with the directory through SameDir, so
// two spellings of one path never read as two repositories (a stat identity
// comparison, never a string guess — a path that does not exist cannot be the
// repository).
func taskRowRepoURLNamesPath(url, dir string) bool {
	p := taskRowRepoURLPath(url)
	if p == "" || dir == "" {
		return false
	}
	return SameDir(p, dir)
}

// taskRowRepoURLPath extracts the local path an address names. It exists so the
// three address spellings this host uses are one comparison (Rule 14), and it
// answers "" for anything it cannot read as a path rather than guessing.
func taskRowRepoURLPath(url string) string {
	u := strings.TrimSpace(url)
	if u == "" {
		return ""
	}
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+len("://"):]
		j := strings.Index(rest, "/")
		if j < 0 {
			return "" // an address with no path component names no directory here
		}
		return strings.TrimSuffix(rest[j:], "/")
	}
	if j := strings.LastIndex(u, ":"); j >= 0 {
		return strings.TrimSuffix(u[j+1:], "/")
	}
	return strings.TrimSuffix(u, "/")
}

// taskRowRepoProductsEntries selects the entries attributable to this run: the
// dirt whose newest write falls inside the row's window. It reads EVERY porcelain
// entry, not only the untracked ones — the shape this arm exists for can equally
// be an edit to a tracked file in the row's repository.
func taskRowRepoProductsEntries(repo, porcelain string, windowStart float64) []string {
	if windowStart == reclaimRunNoWindow {
		return nil // an undated row: the time proof cannot be made
	}
	var out []string
	for _, rel := range taskRowRepoDirtyPaths(porcelain) {
		if reclaimEntryTouchedAfter(repo, rel, windowStart) {
			out = append(out, rel)
		}
	}
	return out
}

// taskRowRepoDirtyPaths extracts the PATH of every porcelain v1 entry, in git's
// own order: the two status columns and their separating space are skipped, a
// rename's destination is the path (its source is gone), and git's C-quoting is
// stripped so the entry can be joined onto the repository.
func taskRowRepoDirtyPaths(porcelain string) []string {
	var out []string
	for _, line := range strings.Split(porcelain, "\n") {
		if len(line) <= len(taskRowPorcelainPrefix) {
			continue
		}
		p := strings.TrimSpace(line[len(taskRowPorcelainPrefix):])
		if i := strings.Index(p, taskRowPorcelainRenameArrow); i >= 0 {
			p = strings.TrimSpace(p[i+len(taskRowPorcelainRenameArrow):])
		}
		p = strings.Trim(p, `"`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// The git spellings this arm reads with, named once (Rule 1b): the verb and its
// flag are a keyword the arm compares against nothing but which must not be
// typed at a call site that a rename could miss. The remote name is the delivery
// proof's OWN constant (deliveryProofOriginRemote) so the two arms can never
// disagree about which remote "the origin" is (Rule 14).
const (
	taskRowRepoStatusVerb    = "status"
	taskRowRepoPorcelainFlag = "--porcelain"
	taskRowRepoRemoteVerb    = "remote"
	taskRowRepoGetURLVerb    = "get-url"
)

// taskRowPorcelainPrefix is the width of the two status columns plus the space
// that separates them from the path (Rule 1: the number is named here, once).
const taskRowPorcelainPrefix = "XY "

// taskRowPorcelainRenameArrow is git's rename separator inside a porcelain entry.
const taskRowPorcelainRenameArrow = " -> "

// taskRowRepoProductsNote is the arm's ONE sentence (Rule 14): who, where, what,
// and the basis — plus the standing guarantee that nothing was touched.
func taskRowRepoProductsNote(taskID, repo, ground string, entries []string, basis string, windowStart float64) string {
	if strings.TrimSpace(ground) == "" {
		ground = taskRowRepoGroundNotRecorded
	}
	return fmt.Sprintf(taskRowRepoProductsNoteFmt, TaskRowRepoProductsMarker, taskID, len(entries), repo,
		strings.Join(entries, reclaimResidueFileSeparator), basis, ground,
		deliveryProofClockStamp(int64(windowStart)))
}

// taskRowRepoGroundNotRecorded is what the note says when the row records no
// ground at all — never an empty slot (a sentence with a hole in it reads as a
// measurement that was not made).
const taskRowRepoGroundNotRecorded = "(none recorded)"

// taskRowRepoProductsFiled reports whether this row already carries the record —
// the idempotence guard, so a re-landed writeback does not drip a second
// identical entry into the trail.
func taskRowRepoProductsFiled(taskID string) bool {
	ctx, cancel := contextWithReclaimGitTimeout()
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return false // best-effort: an unreadable trail must not change a sentence
	}
	defer db.Close()
	var one int
	err = db.QueryRowContext(ctx,
		`SELECT 1 FROM task_events WHERE task_id = ? AND event = ? LIMIT 1`,
		taskID, TaskRowRepoProductsEvent).Scan(&one)
	return err == nil
}

// taskRowRepoProductsRepoDir is the repository this arm reads, exported only for
// a caller that must not re-derive it (Rule 14). It normalises the recorded path
// the way the measurement does, so a reader and the measurement cannot disagree
// about WHICH directory was measured.
func taskRowRepoProductsRepoDir(taskID string) string {
	repo, _, _ := taskRowDeliverable(taskIDNormalize(taskID))
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return ""
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return filepath.Clean(repo)
	}
	return abs
}
