package tools

// task_main_checkout_deposit.go — the MAIN-CHECKOUT arm: the first machine
// check that measures the SHARED checkout a run was pointed at, and attributes
// to the row only what it can prove.
//
// THE DEFECT THIS CLOSES (measured 2026-09-26, epic 01a0db25). A row's payload
// carried the repo's MAIN checkout as its only absolute path —
//
//	"Repo: /home/nick/work/GoPi (branch master at 4c802614). …"
//
// — measured on live row 01a0dabe-3fa1-7cc8-b601-66f1456b8ffd, whose payload
// contains exactly ONE absolute path and it is the shared checkout. That text
// is SUBMITTER payload: no gopi template renders a "Repo: …" line (Rule 1b: no
// producer exists to fix), so the seam that already resolves the main checkout
// for its placement assertion (TaskGroundResolution.MainCheckout) is the one
// place that can notice it. The run honoured the path for the two things a run
// MUST write — evidence artifacts (`devdoc/evidence/tui-two-defects/`) and a new
// test file (`internal/uiclient/live_emphasis_attr_test.go`) — and NO arm
// noticed: the close-out residue arm measures only the row's OWN recorded
// worktree, and reclaimRunUnattributableGround EXCLUDES the main checkout by
// construction (correctly: see the misattribution warning on
// ReclaimRunResidueCheck). The shared tree is shared, so blaming it on whichever
// row happens to close out next would be evidence nobody has.
//
// WHAT MAKES ATTRIBUTION POSSIBLE HERE — the precondition is a MEASUREMENT, not
// a convention: this arm runs ONLY for a row whose own briefing text NAMES the
// shared checkout. That is the one fact that separates "this run was pointed at
// the shared tree" from "the operator (or a sibling run) has scratch in it":
// measured over the epic's own row, 01a0dabe named /home/nick/work/GoPi and is
// the run that deposited there. A row whose briefing names only its own
// worktree — every row the ground announcement reaches — keeps today's handling
// EXACTLY (no measurement, no record).
//
// ── THE SECOND DOOR: deliverable.repo IS the shared checkout (2026-09-26) ────
//
// THE GAP THE MEASURED SHAPE WALKED THROUGH (HP-Laptop, row
// 01a0dbe4-ac93-74f6-8afe-e740669c9532): that run's briefing was SILENT about
// the shared checkout — no sentence named it — and the row's deliverable.repo
// WAS the shared checkout itself. `TaskBriefingNamesDir` is therefore false BY
// CONSTRUCTION (the briefing names the repo's own branch work, not the
// operator's tree), and the arm above returned "nothing to say". The run had no
// legal destination for evidence and invented `<repo>/scratch/`, ~161 files of
// untracked residue in the operator's checkout, measured by nobody.
//
// So the same measurement is reached by a SECOND door, whose selectivity is the
// DELIVERABLE rather than the prose — and it is applied in this order, because
// each step is a case the arm must remain silent about:
//
//  1. the row's recorded ground (deliverable.worktree) IS the shared checkout:
//     silent. That is a different defect shape (TaskGroundMainCheckout exists
//     exactly to refuse it on the spawn path), and a record there would put
//     this row's own scratch on its own trail as if it were a deposit into
//     somebody else's tree.
//  2. the row's deliverable.repo names some OTHER checkout: silent. Silence is
//     the whole safety story — a shared tree is the operator's and every
//     sibling run's, so a row that was never pointed at it is never measured
//     against it.
//  3. the row's deliverable.repo IS the shared checkout: the shared tree is
//     measured, and the SAME window predicate as above attributes the untracked
//     entries that fall inside this row's run. The window proof carries this
//     door alone — there is no prose to quote — so the record QUOTES THE
//     DELIVERABLE instead (the repo the run was handed) and states the window
//     as the falsifiable basis it is. A sibling writing into the shared tree
//     during this row's window is indistinguishable by mtime alone; that is
//     stated in the record rather than hidden.
//
// WHAT DOES NOT CHANGE — the guards this restructure sits beside:
//
//   - a TRACKED change in the shared tree is NEVER attributed (the untracked
//     split is untouched, and the uncommitted-work guards are not referenced or
//     weakened by this arm at all);
//   - a clean, or tracked-only, shared checkout records NOTHING (there is no
//     untracked entry to attribute, and the arm returns before it could);
//   - the arm still cannot mutate: no delete, no move, no git write, no chmod;
//     the regression asserts the checkout's porcelain is BYTE-IDENTICAL across
//     the close-out for both doors.
//
// WHAT IT DOES WITH A MEASUREMENT — it RECORDS, and it never touches a byte:
//
//   - `git -C <main> status --porcelain` is read (one subprocess, bounded);
//   - the UNTRACKED entries are the candidates (a tracked change in the shared
//     tree is somebody's uncommitted work — the epic's own prohibition: never
//     weaken or add traffic to the uncommitted-work guards);
//   - an entry is attributed to the row on EITHER of two proofs, and each is
//     quoted in the note so the reader can audit the basis:
//     (a) the entry's absolute path appears in the row's briefing text
//     (TaskBriefingNamesDir — a TOKEN-boundary match, so a sibling
//     "...-GoPi-old" can never be read as this checkout), or
//     (b) the entry's newest mtime falls INSIDE the row's window (its
//     in_progress_at / claimed_at / created_at → now), which is the only proof
//     available when the deposit path is not echoed in the briefing — exactly
//     the measured case, whose payload names the checkout but not one file it
//     would write.
//   - nothing else is touched: no delete, no move, no git write, no chmod. The
//     arm cannot even be extended into a cleaner by accident — it holds no
//     mutating call.
//
// THE HONEST RESIDUAL (reproduced here rather than left to be discovered): the
// window proof is a TIME bound, not a lineage claim. A live sibling process
// writing into the shared checkout during this row's window is
// indistinguishable from this row by mtime alone — so the record states its
// basis ("the row's briefing named that shared checkout" + the window), and an
// operator who knows better can falsify it from the sentence. That is the same
// line the epic draws for every other residue arm: report the violation, never
// build a machine that acts on somebody's work.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// reclaimMainCheckoutDepositEvent is the arm's own event value, suffixed from
// the close-out residue family's SHARED stem (Rule 14: one definition, and the
// existing `event LIKE 'completed-unclean%'` idempotence guard therefore covers
// this shape without a second probe). Distinct from its siblings because an
// operator grepping "which rows were stopped" must not have to parse a
// sentence: this event is a RECORD, never a refusal.
const reclaimMainCheckoutDepositEvent = reclaimRunResidueUncleanEvent + "-main-checkout-deposit"

// reclaimMainCheckoutDepositBasis is the sentence fragment naming WHY an entry
// is attributed to the row. It is the arm's contract in one line, and it is
// built from the two proofs' names so a change to either cannot leave the note
// claiming a basis the code no longer checks.
// Two bases, one per door, and each is a SENTENCE the reader can falsify: the
// first rests on the briefing naming the checkout, the second on the row's own
// deliverable naming it (the shape whose briefing is silent — measured on row
// 01a0dbe4-ac93-74f6-8afe-e740669c9532, where the briefing named no checkout at
// all and the deposit went unrecorded).
const (
	reclaimMainCheckoutDepositBasisBriefing = "the row's briefing named that shared checkout and the entry falls inside this run's window"
	reclaimMainCheckoutDepositBasisRepo     = "the row's own deliverable.repo IS that shared checkout, its run's own session recorded it as the launch directory, and the entry falls inside this run's window"
)

// reclaimUntrackedEntryPrefix is git porcelain v1's status field for an
// untracked path — read from its single definition (Rule 1: the arm parses
// git's own vocabulary, never a re-typed literal).
const reclaimUntrackedEntryPrefix = "?? "

// reclaimMainCheckoutScratchName is the residue ROOT the measured incident
// invented inside the shared checkout (`<repo>/scratch/`, row 01a0dbe4,
// 2026-09-26). One definition, referenced by the arm's own reader
// (reclaimMainCheckoutScratch) and by the regression that reproduces that
// shape — never re-typed at either site (Rules 1b/13).
const reclaimMainCheckoutScratchName = "scratch"

// reclaimRunNoWindow is the zero window-start: a row the ledger cannot date is
// NOT attributable by time, and is therefore left alone (fail closed — an
// unattributable entry is never blamed on a row).
const reclaimRunNoWindow = 0.0

// TaskMainCheckoutDepositOutcome is the arm's verdict for one row: the shared
// checkout it measured, that checkout's raw porcelain body, the entries it
// attributed to the row, the BASIS sentence the attribution rests on, and the
// sentence a caller records.
type TaskMainCheckoutDepositOutcome struct {
	// Path is the main checkout measured ("" when the arm did not run).
	Path string
	// Porcelain is the raw `git status --porcelain` body of that checkout
	// ("" when it was clean, or not measured).
	Porcelain string
	// Deposits are the untracked entries attributed to this row.
	Deposits []string
	// Basis is the door's own sentence: WHY these entries are this row's
	// ("" when there is nothing to say). Carried rather than re-derived, so
	// the note and the measurement cannot disagree.
	Basis string
	// Scratch is the deposit path that names the residue root a run invented
	// ("" when no attributed entry names one) — see reclaimMainCheckoutScratch.
	Scratch string
	// Note is the attributed record ("" when there is nothing to say).
	Note string
}

// TaskMainCheckoutDepositArm measures the repo's MAIN checkout for a row that
// reaches one of its two doors (reclaimMainCheckoutDoor), and reports the
// untracked entries attributable to that row under that door's basis.
//
// It returns no outcome (and no error) for the ordinary row — one that names no
// repo, whose briefing never mentions the shared checkout and whose recoverable
// deliverable is not that checkout, or whose ground IS it — because there is no
// measurement to make and a zero verdict is the truthful answer. A git failure on
// the measured checkout IS an error: the caller reports it (Rule 15), and a
// shared checkout that cannot be read never becomes a refusal.
func TaskMainCheckoutDepositArm(taskID string) (TaskMainCheckoutDepositOutcome, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return TaskMainCheckoutDepositOutcome{}, fmt.Errorf("main checkout arm: task_id must not be empty")
	}
	repo, _, _ := taskRowDeliverable(taskID)
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return TaskMainCheckoutDepositOutcome{}, nil // a repo-less row names no shared checkout
	}
	main := taskGroundMainCheckout(repo)
	if main == "" {
		return TaskMainCheckoutDepositOutcome{}, nil // cannot be told: never guess
	}
	summary, payload, err := TaskBriefing(taskID)
	if err != nil {
		return TaskMainCheckoutDepositOutcome{}, err
	}
	briefing := summary + "\n" + payload
	basis, briefed := reclaimMainCheckoutDoor(taskID, repo, main, briefing)
	if basis == "" {
		// Neither door is open: the row names its repo but its briefing does
		// NOT point the run at the shared checkout, and its deliverable.repo is
		// not that checkout either. This is the arm's whole selectivity —
		// without a basis there is no ground for attributing anything in a
		// SHARED tree to this row, and it measures nothing.
		return TaskMainCheckoutDepositOutcome{}, nil
	}
	porcelain, err := reclaimGitStatus(main)
	if err != nil {
		return TaskMainCheckoutDepositOutcome{Path: main}, err
	}
	out := TaskMainCheckoutDepositOutcome{Path: main, Porcelain: porcelain}
	untracked := reclaimUntrackedDirtyPaths(porcelain)
	if len(untracked) == 0 {
		return out, nil // clean, or tracked-only: not this arm's business
	}
	if briefed {
		// The briefing door keeps its path proof: an entry the briefing NAMES
		// is attributable on the stronger basis, whatever its mtime.
		out.Deposits = reclaimMainCheckoutDeposits(main, untracked, briefing, reclaimRunWindowStart(taskID))
	}
	if len(out.Deposits) == 0 {
		// The deliverable door. Its proof is LINEAGE, not time: the shared
		// checkout was this run's OWN recorded launch directory (the session's
		// session_start.workdir), so the run was standing in the tree it
		// deposited into and every entry inside its window belongs to it. That
		// is the fact a timestamp cannot supply — an operator's own file,
		// written during the window by somebody who never ran here, is NOT this
		// row's deposit, and a time-only arm would file exactly that.
		out.Deposits = reclaimWindowDeposits(main, untracked, reclaimRunWindowStart(taskID), reclaimRunLaunchedIn(taskID, main))
	}
	if len(out.Deposits) == 0 {
		// Dirt this row cannot be shown to own: NO RECORD — the record is
		// evidence, never a suspicion (Rule 15's mirror: a record that cannot
		// name its subject is not a record).
		//
		// BUT NOT SILENT TOO (Rule 15, measured need). A SEMANTICALLY different
		// shape lands on this same line: the checkout carries untracked entries,
		// the row reached a door, and yet nothing was attributed. That is the
		// exact shape `TestMainCheckoutDepositIsRecordedForTheRowThatNamedIt`
		// failed on (row t-maindep, the ONLY deposit-family row with a deposit
		// and no record and no diagnostic at all — its three siblings each filed
		// one), and at that moment the arm left NOTHING behind, so no reader
		// could tell "the entries are not this row's" from "the attribution
		// proof could not be made". Those call for opposite reactions, so the
		// measurement's own inputs are reported: the door taken, how many
		// entries were seen, the window's start, and the lineage verdict.
		//
		// It is a DIAGNOSTIC, not a record: nothing is written to the row's
		// trail, and a clean checkout never reaches here (the early return above
		// covers it), so an ordinary close-out stays quiet.
		if basis != "" && len(untracked) > 0 {
			taskNotifyLog("main checkout arm %s: %d untracked entry/entries in %s but none attributable (basis=%q, windowStart=%v, launchedIn=%v) — the entries are either outside this run's window or the run was never launched in that tree; nothing was recorded and nothing was touched",
				taskID, len(untracked), main, basis, reclaimRunWindowStart(taskID), reclaimRunLaunchedIn(taskID, main))
		}
		return out, nil
	}
	out.Basis = basis
	out.Scratch = reclaimMainCheckoutScratch(out.Deposits)
	out.Note = taskMainCheckoutDepositNote(taskID, main, out.Deposits, basis, out.Scratch)
	return out, nil
}

// reclaimMainCheckoutDoor decides WHICH door — if any — lets this row's close-out
// measure the shared checkout, and returns the BASIS sentence that door rests on.
// `briefed` reports whether the briefing door is the one that opened, because
// only that door can additionally attribute an entry by the path the briefing
// named (the deliverable door has no prose to quote).
//
// Three silent cases, in order, and each is a shape the arm must not speak for:
//
//	ground == main   the row's recorded ground IS the shared checkout — that is
//	                 the spawn-path refusal's shape (TaskGroundMainCheckout),
//	                 not a deposit into somebody else's tree, and a record here
//	                 would file the row's own scratch as a foreign deposit.
//	no door          the briefing never named this checkout AND (for the
//	                 deliverable door) the run's own session does not record it
//	                 as the launch directory — there is no basis, and a silent
//	                 briefing plus an unknown lineage is not evidence that this
//	                 run deposited; see the file header's honest residual.
//
// The BRIEFING door is tested FIRST, and the order is load-bearing rather than
// cosmetic: its proof is the row's own prose naming the tree, which is the
// stronger basis and the one the pre-existing behaviour rests on — a row whose
// briefing names the checkout keeps the path proof even when its deliverable.repo
// is that same checkout (the fixture shape) and even when its run's session
// cannot be read. The deliverable door opens only when the briefing stayed
// silent, which is the shape this change adds.
//
// A row that names some OTHER checkout (repo != main) is reached only when the
// briefing was silent about this one; its deliverable does not point here, so
// nothing is measured.
func reclaimMainCheckoutDoor(taskID, repo, main, briefing string) (basis string, briefed bool) {
	_, _, worktree := taskRowDeliverable(taskID)
	if SameDir(strings.TrimSpace(worktree), main) {
		return "", false
	}
	if TaskBriefingNamesDir(briefing, main) {
		return reclaimMainCheckoutDepositBasisBriefing, true
	}
	if SameDir(strings.TrimSpace(repo), main) {
		return reclaimMainCheckoutDepositBasisRepo, false
	}
	return "", false
}

// reclaimWindowDeposits selects the untracked entries that fall inside the row's
// window — the shared time proof (the deliverable door adds its lineage
// precondition separately, which is why `launchedIn` gates the whole selection).
//
// An undated row (reclaimRunNoWindow) attributes nothing, and a run that did NOT
// launch in the measured tree attributes nothing either: fail closed, because an
// entry is only a deposit if the run's own window covers it AND the run was
// standing in that tree — the two facts a reader can check independently.
func reclaimWindowDeposits(main string, untracked []string, windowStart float64, launchedIn bool) []string {
	if !launchedIn || windowStart == reclaimRunNoWindow {
		return nil
	}
	var out []string
	for _, rel := range untracked {
		if reclaimEntryTouchedAfter(main, rel, windowStart) {
			out = append(out, rel)
		}
	}
	return out
}

// reclaimRunLaunchedIn reports whether this row's run RECORDED the shared
// checkout as its own launch directory — read from the run's session through the
// EXISTING readers (the row's session id, then the cost-aware session-log lookup
// taskFindSessionJSONLForRow, then the same scan the terminal writeback uses,
// taskGroundFromSession's reader).
//
// This is the deliverable door's LINEAGE proof, and it is deliberately a second
// measurement rather than a derivation: the row's deliverable saying "repo = the
// shared checkout" is a claim about the TARGET, while the session's own
// session_start.workdir is where the process actually stood. Only the
// conjunction supports "the run deposited here".
//
// FALSE on anything unreadable (no session id, no log, an unparseable start
// record): an unreadable lineage is not evidence, and the caller's fail-closed
// path then attributes nothing.
func reclaimRunLaunchedIn(taskID, main string) bool {
	sessionID := taskSessionIDForRow(taskID)
	if sessionID == "" {
		return false
	}
	wd := taskLaunchRepo(sessionID, taskWorkdirFromSession(sessionID))
	if wd == "" {
		return false
	}
	return SameDir(wd, main)
}

// reclaimMainCheckoutDeposits selects the entries attributable to the row: an
// entry whose absolute path the briefing NAMES, or — when the briefing names
// none (the measured shape) — one whose newest mtime falls inside the row's
// window. Both proofs are applied per entry, and the selection order is the
// entry order git printed (nothing is re-sorted).
func reclaimMainCheckoutDeposits(main string, untracked []string, briefing string, windowStart float64) []string {
	var out []string
	for _, rel := range untracked {
		abs := reclaimEntryAbsPath(main, rel)
		if TaskBriefingNamesDir(briefing, abs) {
			out = append(out, rel)
			continue
		}
		if windowStart == reclaimRunNoWindow {
			continue // undated row: the time proof cannot be made
		}
		if reclaimEntryTouchedAfter(main, rel, windowStart) {
			out = append(out, rel)
		}
	}
	return out
}

// reclaimEntryAbsPath resolves a porcelain entry to the absolute path the
// briefing would have to name. git collapses an untracked DIRECTORY to one
// entry carrying a trailing separator; the trailing slash is trimmed so the
// comparison is against the directory itself.
func reclaimEntryAbsPath(main, rel string) string {
	return filepath.Join(main, strings.TrimSuffix(strings.TrimSuffix(rel, "/"), string(filepath.Separator)))
}

// reclaimUntrackedDirtyPaths extracts the untracked entries ("?? <path>") from
// a porcelain v1 body, in git's own order. git C-quotes a path holding unusual
// bytes; the quotes are stripped so the entry can be joined onto the checkout,
// never re-quoted.
func reclaimUntrackedDirtyPaths(porcelain string) []string {
	var out []string
	for _, line := range strings.Split(porcelain, "\n") {
		if !strings.HasPrefix(line, reclaimUntrackedEntryPrefix) {
			continue
		}
		p := strings.TrimSpace(strings.TrimPrefix(line, reclaimUntrackedEntryPrefix))
		p = strings.Trim(p, `"`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// reclaimEntryTouchedAfter reports whether the untracked entry at rel was
// written (or, for a directory, received anything) at or after windowStart.
// A directory entry is walked: git reports the DIRECTORY once, and the deposit
// that proves the row's hand is a FILE inside it (the measured deposit is
// exactly that — `devdoc/evidence/tui-two-defects/`, a directory of captures).
//
// Unreadable = false (never attributed): a path this arm cannot stat is not
// evidence about the row.
func reclaimEntryTouchedAfter(main, rel string, windowStart float64) bool {
	root := reclaimEntryAbsPath(main, rel)
	info, err := os.Stat(root)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return reclaimModTimeAfter(info, windowStart)
	}
	touched := reclaimModTimeAfter(info, windowStart)
	if touched {
		return true
	}
	// NO SHORT-CIRCUIT without a decision: the walk reports its own finding and
	// this reader returns it, so an unwalkable directory cannot silently read
	// as "not ours" when the directory itself already proved otherwise.
	found := false
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // an entry we cannot read is not evidence
		}
		fi, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if reclaimModTimeAfter(fi, windowStart) {
			found = true
			return filepath.SkipAll
		}
		return nil
	}) //stylegate:ignore inventory walk for a diagnostic message; an unreadable entry is omitted from the list and the deposit's own verdict is unaffected
	return found
}

// reclaimModTimeAfter is the window proof in one predicate (Rule 14): the
// file's newest write is at or after windowStart.
func reclaimModTimeAfter(info os.FileInfo, windowStart float64) bool {
	return info.ModTime().Unix() >= int64(windowStart)
}

// reclaimRunWindowStart resolves the moment this row's run began, in unix
// seconds: the in_progress transition when the ledger holds it, else the claim,
// else the submit. reclaimRunNoWindow (0) means the row cannot be dated at all,
// and the arm then makes no time-based attribution.
//
// One reader for the whole arm (Rule 2): the window is a fact about the ROW, and
// a second derivation of it would be a second opinion about whose work an entry
// is.
func reclaimRunWindowStart(taskID string) float64 {
	ctx, cancel := contextWithReclaimGitTimeout()
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("main checkout arm window (%s): %v", taskID, err)
		return reclaimRunNoWindow
	}
	defer db.Close()
	var inProgress, claimed, created sql.NullFloat64
	if err := db.QueryRowContext(ctx,
		`SELECT in_progress_at, claimed_at, created_at FROM tasks WHERE task_id = ?`, taskID).
		Scan(&inProgress, &claimed, &created); err != nil {
		taskNotifyLog("main checkout arm window (%s): %v", taskID, err)
		return reclaimRunNoWindow
	}
	for _, v := range []sql.NullFloat64{inProgress, claimed, created} {
		if v.Valid && v.Float64 > 0 {
			return v.Float64
		}
	}
	return reclaimRunNoWindow
}

// reclaimMainCheckoutScratch names the residue ROOT an attributed entry was
// invented under — the class the 2026-09-26 incident left (`<repo>/scratch/`),
// recovered from the entries themselves rather than asserted about the row.
//
// It returns the root's own constant (never a re-typed literal, Rule 1b) when an
// attributed entry's first path segment IS that root, and "" otherwise: a
// deposit into a tracked directory the repo already owns (`devdoc/evidence/…`)
// is residue too, just not THIS shape, and the note says only what it measured.
func reclaimMainCheckoutScratch(deposits []string) string {
	for _, rel := range deposits {
		head := strings.TrimPrefix(rel, "./")
		if head == reclaimMainCheckoutScratchName || strings.HasPrefix(head, reclaimMainCheckoutScratchName+string(filepath.Separator)) {
			return reclaimMainCheckoutScratchName
		}
	}
	return ""
}

// taskMainCheckoutDepositNote is the arm's ONE sentence: WHO (the row being
// closed out), WHERE (the shared checkout), WHAT (the entries, quoted back) and
// the BASIS the attribution rests on, plus the standing guarantee that nothing
// was touched. One definition, because the event detail and the stderr line
// must say the same thing (Rule 14). The basis ARRIVES from the door that
// opened (reclaimMainCheckoutDoor) rather than being re-decided here, so the
// sentence and the measurement cannot disagree about why the entry is on this
// row's trail, and the `scratch` clause is added only when a measured entry
// names it.
func taskMainCheckoutDepositNote(taskID, main string, deposits []string, basis, scratch string) string {
	note := fmt.Sprintf("run %s deposited residue in the repo's MAIN checkout %s — entries: %s (%s; nothing was deleted, moved or modified — this arm only measures, and the shared checkout is never cleaned by it)",
		taskID, main, strings.Join(deposits, reclaimResidueFileSeparator), basis)
	if scratch == "" {
		return note
	}
	return note + fmt.Sprintf(" [the run invented a `%s/` root inside the checkout — a run writes its evidence in its OWN session-artifacts directory instead; no reaper, no .gitignore entry and no sweep is added by this arm, so the deposit stays where the run left it for the operator to decide]", scratch)
}

// reclaimMainCheckoutDepositFiled reports whether this row already carries the
// main-checkout record — the idempotence guard, so a re-landed writeback of the
// same run does not drip a second identical record into the trail (the family's
// existing guard's shape, applied to this arm's own event).
func reclaimMainCheckoutDepositFiled(taskID string) bool {
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
		taskID, reclaimMainCheckoutDepositEvent).Scan(&one)
	return err == nil
}

// TaskMainCheckoutDepositRecord measures the shared checkout for this row and,
// when it attributed anything, files the record on the row's trail — returning
// the filed sentence ("" when there was nothing to record).
//
// It is the ONE door every close-out passes through (Rule 14: the measurement,
// the idempotence guard and the two carriers live here, not at the call site).
// A measurement failure is reported and never blocks the close-out: a run's
// writeback must not depend on our ability to read a SHARED tree.
func TaskMainCheckoutDepositRecord(taskID string) string {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return ""
	}
	out, err := TaskMainCheckoutDepositArm(taskID)
	if err != nil {
		taskNotifyLog("main checkout deposit check for %s: %v", taskID, err)
		return ""
	}
	if out.Note == "" {
		return ""
	}
	if reclaimMainCheckoutDepositFiled(taskID) {
		// Idempotent: a re-landed writeback of the same run does not drip a
		// second identical record into the row's trail (the family's existing
		// guard's shape, applied to this arm's own event).
		return out.Note
	}
	note := out.Note
	ctx, cancel := contextWithReclaimGitTimeout()
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		// The trail is the carrier; without it the note still goes to stderr
		// (Rule 15: a degradation is never swallowed).
		taskNotifyLog("main checkout deposit record %s: %v", taskID, err)
		fmt.Fprintln(os.Stderr, "[task] "+note)
		return note
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, reclaimMainCheckoutDepositEvent, note)
	taskNotifyLog("%s", note)
	return note
}

// contextWithReclaimGitTimeout is the arm's ONE bounded context builder, named
// so the two ledger reads and the record cannot drift apart about the budget
// (Rule 14; the budget itself is the family's existing limit, Rule 1).
func contextWithReclaimGitTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), reclaimGitTimeout)
}
