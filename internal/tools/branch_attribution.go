package tools

// Branch attribution — the merger's ONE answer to "who, if anyone, is working
// on this branch right now?".
//
// WHY THIS EXISTS. The merger decides per unmerged branch and it wants exactly
// two simple dispositions: an OWNED branch is left alone (its owner's run is
// still walking the work), an UNOWNED one is archived and resubmitted. What it
// actually has today is git occupancy, which names a DIRECTORY and never a
// person: `git worktree list` says a branch is checked out somewhere, and
// nothing in git records which gopi task put it there. The ledger is the only
// place that can know — and it does so unevenly. Measured on the live ledger
// (2026-09-16, ~/.gopi/memory.db): 513 of 1455 rows carry a deliverable.branch,
// 417 a worktree, but only the five LIVE-status rows that were stamped at
// submit/carry-time carry BOTH; the completion-time backfill
// (taskBackfillDeliverableBranch, task_deliverable_branch.go:347, called from
// internal/tools/tasks.go:5405) is the only writer that pairs branch with
// worktree for an ordinary row, and it runs when the run is already over.
//
// THE MEASURED DEFECT this file closes (row 01a0a996, live during the change):
// status=in_progress, claimed_by=peerbox:3680671 (that pid is GONE — the wrapper
// was killed), deliverable.branch=fix/live-runner-not-dead-holder (the ref no
// longer exists — it was merged), deliverable.worktree=/home/user/workspace/
// gopi-live-veto (the directory no longer exists). Two independent sweep arms
// look at that row and both answer wrongly:
//
//   - the age sweep's ownership rung uses worktreeSweepRowHoldsGround, which
//     requires a LIVE status AND !HolderProvenDead — and HolderProvenDead is
//     true here (the claim pid is a dead local pid), so a row that is in fact
//     still in_progress reads UNOWNED (internal/tools/worktree_sweep.go:923);
//   - the resume arm cannot even LOAD the row when its deliverable names only a
//     branch: deadHolderResumeRows queries `deliverable LIKE '%worktree%'`
//     (task_holder_resume.go:435), its own sibling worktreeSweepTaskRows having
//     already fixed exactly this class of needle-truncation miss.
//
// WHAT THIS FILE DELIBERATELY DOES NOT DO. It does not weaken the occupancy
// veto in EITHER arm: a live-status row that is merely a RECORD (a ledger row
// outlives the process that wrote it) must never protect a directory forever —
// that is the parked-forever bug memory #2145 describes. The ground-holding
// rule stays where it is; this file adds the missing axis — the row that holds
// no ground any more but still names the branch, which is OWED a decision
// rather than silently dropped.
//
// CONSERVATISM IS THE CONTRACT (memory #2632/#2633: a fail-open liveness probe
// deleted a live session's worktree). An unreadable probe is NEVER a death
// proof, an absent registration is NEVER evidence of absent work, and every
// verdict carries the evidence that produced it so an operator can disagree.
// There is no destructive action anywhere in this file: it reports.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// branchAttribution is the classifier's answer about ONE branch — a CLOSED set
// of six values, because the merger's next move must be derivable from it and
// an open string would make every consumer re-invent the choice.
type branchAttribution string

const (
	// branchAttribLiveHolder: a row whose recorded runner is STILL RUNNING
	// stands on this branch. The strongest evidence there is; every other arm
	// must leave the branch alone.
	branchAttribLiveHolder branchAttribution = "live-holder"
	// branchAttribOwnerEnded: a row names this branch, that row's run is
	// provably over (claim pid gone / runner gone / life declared ended), and
	// its recorded directory still holds the branch. This is the RESUME
	// population: the work is unfinished and nobody is on it any more.
	branchAttribOwnerEnded branchAttribution = "owner-ended"
	// branchAttribOwnerRecordOnly: a row names this branch, the row's ground
	// is not provably held, but NOTHING holds the branch (no registration, or
	// the recorded directory is gone) — an ownership record with no ground
	// left. Distinct from owner-ended on purpose: there is no interrupted
	// checkout to walk back into, so the disposal is a ledger question, not a
	// resume.
	branchAttribOwnerRecordOnly branchAttribution = "owner-record-only"
	// branchAttribUnowned: the ledger was read successfully and no row names
	// this branch at all. The ONLY verdict that licenses disposal.
	branchAttribUnowned branchAttribution = "unowned"
	// branchAttribUngoverned: nothing on this host can say — the ledger could
	// not be read, or an occupying process's probe was unreadable. Never a
	// licence to act (Rule 15: unknown is never dead).
	branchAttribUngoverned branchAttribution = "ungoverned"
	// branchAttribIntegration: master/main or the archive-* ledger of decided
	// losers. Not a task's branch at all.
	branchAttribIntegration branchAttribution = "integration"
)

// branchAttributionEvidence is one evidence sentence plus its confidence rank.
// The rank exists so the strongest evidence WINS without a second ordering
// table: exactly one implementation of "live beats ended" (Rule 14).
type branchAttributionEvidence struct {
	// Rank is the confidence tier: higher wins. See the tier constants below.
	Rank int
	Task string // the ledger row the evidence came from ("" = none)
	Rule string // the closed-set rule name the verdict carries
	Why  string // the operator-facing sentence
	// Resumable marks the death-proof tier of a row that was STILL in a live
	// holder status — an interrupted run there is work to continue. It is false
	// for a terminal row: that work is over, and offering a resume for it would
	// be a promise the resume arm itself refuses (it filters on
	// taskLiveHolderStatus).
	Resumable bool
}

// The evidence tiers, strongest first. They are NAMED because the pair
// (rank, reclaimer-verdict) is the whole conservatism argument: a wrong
// promotion to a "dead" tier is what deletes somebody's work.
const (
	// tierProcessLive: a recorded runner incarnation is alive.
	tierProcessLive = 100
	// tierClaimLive: a row's claim stamp names a pid that is alive on this host.
	tierClaimLive = 90
	// tierCheckoutLive: a live-status row whose claim pid is provably gone, but
	// a process is standing in the branch's own checkout. The weaker statement
	// is deliberate: the checkout holder is NOT proven to be this row's runner
	// (a sibling worker in the same tree is the measured false-positive risk),
	// so it outranks "ended" — "somebody is there" must never be read as "the
	// ground is free" — but it is ranked BELOW the row's own recorded runner.
	tierCheckoutLive = 70
	// tierLedgerEnded: the ledger itself says the run is over (a terminal
	// status, or ended_at past the grace window).
	tierLedgerEnded = 80
	// tierProcessGone: a POSITIVE death proof from /proc — the recorded runner
	// incarnation is absent/zombie/recycled, or the claim pid resolves on this
	// host and is gone.
	tierProcessGone = 60
	// tierRecordOnly: a row names the branch but holds no provable ground.
	tierRecordOnly = 20
	// tierUnknown: the evidence could not be read. NEVER a death proof.
	tierUnknown = 0
)

// branchAttributionDecision is the full answer: the verdict, the strongest
// evidence behind it, and every sentence that was weighed (so a dry run or a
// status line can print WHY, not just what).
type branchAttributionDecision struct {
	Repo    string
	Branch  string
	Verdict branchAttribution
	// Task names the owning row when one exists ("" otherwise).
	Task string
	// Worktree is the directory the ledger records for the branch, when a row
	// does.
	Worktree string
	// Held records whether some process holds the branch's checkout.
	Held bool
	// HeldNames names the processes the holder scan found inside the checkout
	// (scratch.HolderNames — "pid N (cwd /w)"). It is EVIDENCE for Held, never a
	// second measurement: a render prints it beside Held and never derives Held
	// from it (memory #2632).
	HeldNames []string
	// RunnerKnown/RunnerLive are the ROW axis of liveness, and they are the
	// reason a `gopi merger --status` reader can trust this decision at all
	// (defect measured 2026-09-18, task 01a0b1d9).
	//
	// A task's runner is recorded on its own ledger row as a three-factor
	// identity (pid + kernel start ticks + cmdline — tasksRowForSweep.Ident,
	// the same triple `gopi task show` prints). The CHECKOUT scan (Held) is not
	// a substitute for it and never was: a `gopi worker <task-id>` process runs
	// with its SESSION directory as its working directory, so a checkout can
	// read "nobody inside" while the run is demonstrably alive — which is
	// exactly the sentence the operator was given and could not believe.
	//
	// RunnerKnown is false for a row that records no identity (or for a branch
	// no row names at all — then the checkout is the ONLY thing anything knows,
	// and a render must say so rather than imply the run is over).
	RunnerKnown bool
	RunnerLive  bool
	// Age is how long the branch's checkout has existed, from the git worktree
	// registration (the ADMIN directory's birth time); AgeKnown is false when
	// no checkout exists or the host could not report one. It is the signal the
	// operator asked for in as many words — "just report how long the
	// worktree/branch lives, which can arouse user attention" — and it is a
	// measurement of git's bookkeeping that claims nothing about any run.
	Age      time.Duration
	AgeKnown bool
	// Checkout records whether a checkout of this branch EXISTS on disk. It is
	// what separates the two populations a caller must not conflate: a branch
	// git still refuses the merger (a checkout holds it) versus a row that
	// merely remembers a directory that is already gone. The compact merger
	// line keeps only the first — the second is a ledger-hygiene item, printed
	// in full by the standalone face.
	Checkout bool
	// Reason is the strongest evidence's sentence — the ONE line the merger
	// prints. It always names the evidence, never a bare disposition.
	Reason string
	// Evidence is every sentence weighed, strongest first. Kept because the
	// interesting failures here are disagreements, and an operator cannot
	// adjudicate one from the winner alone.
	Evidence []string
	// Reclaimer says which EXISTING arm is the remedy for the branches whose
	// verdict is not final — the arm whose own verdict decides, so this field
	// is a pointer and never a promise (memory #2324: "reclaimable" MUST NOT be
	// derived from "dead holder").
	Reclaimer branchAttributionArm
}

// branchAttributionArm names an existing arm the merger already runs, so a
// non-final verdict never invents a remedy.
type branchAttributionArm string

const (
	// branchArmNone: final verdict — the merger can decide on it directly.
	branchArmNone branchAttributionArm = ""
	// branchArmResume: the dead-holder resume leg
	// (ResumeDeadHolderRuns), wired into the merger pass.
	branchArmResume branchAttributionArm = "resume"
	// branchArmReclaim: the dead-holder worktree reclaim
	// (ReclaimDeadHolderWorktrees), which frees the DIRECTORY and leaves the
	// branch to the next merge sweep.
	branchArmReclaim branchAttributionArm = "reclaim"
	// branchArmReaper: the dead-registration repair of the age sweep
	// (WorktreeSweepRun), which collects a registration whose directory is
	// gone (reap / reap-branch). It is the remedy for a record-only row whose
	// directory is gone but whose registration survives.
	branchArmReaper branchAttributionArm = "registration-repair"
)

// branchAttributionRows loads the rows that can name a branch: EVERY row
// carrying a non-empty deliverable, with no needle on the blob's spelling.
//
// The needle is deliberately absent. A `LIKE '%worktree%'` filter is the
// measured truncation bug of this family twice over (memory #2632: an age-ASC
// LIMIT dropped the live rows; deadHolderResumeRows' worktree needle drops
// every branch-only row). worktreeSweepTaskRows already took this lesson: it
// scans every row with a deliverable and lets the parsers decide, because a
// needle cannot truncate an axis out of the evidence silently (Rule 15). This
// loader is the same rule stated once more for the branch axis; the runaway
// bound REFUSES the pass rather than answering from a partial universe.
func branchAttributionRows(ctx context.Context) ([]tasksRowForSweep, error) {
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, COALESCE(deliverable,''), COALESCE(ended_at,0),
		        COALESCE(claimed_by,''), COALESCE(`+tasksProcPidColumnName+`,0),
		        COALESCE(`+tasksProcStartColumnName+`,0), COALESCE(`+tasksProcCmdColumnName+`,'') FROM tasks
		 WHERE COALESCE(deliverable,'') <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tasksRowForSweep
	for rows.Next() {
		var id, status, deliverable, claimedBy, procCmd string
		var endedAt float64
		var procPid, procStart int64
		if err := rows.Scan(&id, &status, &deliverable, &endedAt, &claimedBy, &procPid, &procStart, &procCmd); err != nil {
			return nil, err
		}
		if taskDeliverableBranchOrEmpty(deliverable) == "" {
			continue // no branch claim on this row: nothing to attribute
		}
		ident := procIdentity{Pid: int(procPid), StartTicks: procStart, Cmd: procCmd}
		out = append(out, tasksRowForSweep{
			TaskID:      id,
			Status:      status,
			Worktree:    taskDeliverableWorktree(deliverable),
			Branch:      taskDeliverableBranchOrEmpty(deliverable),
			Repo:        finishCleanupParseDeliverable(deliverable).Repo,
			Deliverable: deliverable,
			EndedAt:     endedAt,
			ClaimedBy:   claimedBy,
			Ident:       ident,
			// The holder verdict is NOT re-derived: it is the ONE shared
			// classifier the finish-cleanup arms consult (Rule 14 — one
			// definition of "this row's runner is gone").
			HolderProvenDead: finishCleanupDeadLocalHolder(status, claimedBy, ident),
		})
		if len(out) >= branchAttributionRowScanMax {
			return nil, fmt.Errorf("branch attribution: more than %d rows carry a branch — refusing to answer from a truncated universe", branchAttributionRowScanMax)
		}
	}
	return out, rows.Err()
}

// branchAttributionRowScanMax is the runaway bound on the evidence read. It is
// deliberately far above any real ledger (measured 2026-09-16: 513 rows carry a
// branch) — it exists to stop a runaway, not to page a large ledger, which is
// exactly what the incident memory #2632 forbids.
const branchAttributionRowScanMax = 50000

// branchAttributionBranchBudget bounds ONE resolve call's branch loop (the
// public face may be handed a preview listing up to 200 candidates per repo).
const branchAttributionBranchBudget = 500

// branchAttributionLedgerUnreadable is the ONE spelling of "the branch axis
// could not be answered because the task ledger would not open" (Rule 1b: the
// arm that ERRORS and the renders that report it compare the same constant).
const branchAttributionLedgerUnreadable = "task ledger unreadable"

// ledgerChannelPhrase renders the resolution's channel for an operator reading
// an unreadable-ledger message: a named channel stays its own name, and the
// one channel that is NOT a name (the built-in default) is spelled out with the
// directory it hangs from. Named constants only (Rule 1b) — the alternative, a
// bare switch over literals, is how this message drifts from the resolution.
func ledgerChannelPhrase(source string) string {
	switch source {
	case config.MemoryDBEnv, config.MemoryDBEnvLegacy:
		return source + " channel"
	case config.LedgerSettingsKey:
		return config.LedgerSettingsKey + " settings key"
	default:
		return "default location ($HOME/" + config.ProfileDirName + ")"
	}
}

// branchAttributionLedgerCause explains WHY the ledger would not open, in the
// terms an operator acts on: WHERE it was looked for, WHICH channel named that
// path, and WHAT was found there. It exists because the error this replaces was
// useless in the other direction too — `%!w(<nil>)` named no path, so the
// operator could not even tell WHICH ledger was meant (Rule 15: an error must
// let its reader act).
//
// It never MASKS the underlying opener error: the caller appends the wrapped
// cause, so the rendered line carries both the location verdict and the
// driver's own words.
func branchAttributionLedgerCause(path string) string {
	// The channel comes from the ONE resolution (Rule 2/14) rather than a second
	// walk of the env: this message and `gopi config show` describe the same
	// ledger, and a hand-rolled ranking here would name a channel the loader
	// would not have used (the settings key is one of them now).
	_, source := config.ResolveLedger(config.LedgerNoFlag)
	cause := "no ledger at " + path + " (" + ledgerChannelPhrase(source) + ")"
	switch st, err := os.Stat(path); {
	case err != nil:
		cause += " — nothing to read: " + err.Error()
	case st.IsDir():
		cause += " — the path is a DIRECTORY, not a ledger file"
	default:
		cause += fmt.Sprintf(" — the file is there (%d bytes) but could not be opened", st.Size())
	}
	return cause
}

// BranchAttribution answers, for ONE branch of repo, who (if anyone) is working
// on it — conservatively. It is read-only: no merge, no delete, no lock, no
// ledger write.
//
// The verdict is FINAL only for live-holder, unowned and integration; the two
// owner-* verdicts name the existing arm whose own verdict decides the remedy
// (Reclaimer), so a caller never renders a promise it cannot keep.
func BranchAttribution(repo, branch string, rows []tasksRowForSweep, holders scratch.HolderScan, ledgerRead bool) branchAttributionDecision {
	d := branchAttributionDecision{Repo: repo, Branch: branch, Verdict: branchAttribUngoverned}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		d.Reason = "no branch name given — nothing to attribute"
		return d
	}
	// ① the integration/exempt namespaces are not a task's work at all: the
	//    same two exemptions every merge and cleanup path shares (Rule 13 —
	//    referenced, never re-spelled).
	if branch == masterBranchName || branch == mainBranchName || taskMergeExempt(branch) {
		d.Verdict = branchAttribIntegration
		d.Reason = "integration/exempt namespace (" + branch + ") — not a task's branch"
		return d
	}
	if repoSweepArchiveExempt(repo, branch) {
		d.Verdict = branchAttribIntegration
		d.Reason = "archive-tagged decided loser (refs/tags/" + branchArchiveTagName(branch) + ") — never merged or deleted"
		return d
	}
	// ② an unreadable ledger is not evidence: nothing may be concluded from it
	//    (Rule 15's conservative direction — the same rule that makes an
	//    unreadable /proc probe "unreadable", never "dead").
	if !ledgerRead {
		d.Reason = "the task ledger could not be read — no attribution is possible, and nothing may be concluded from its absence"
		return d
	}

	// ③ the branch's own GROUND, measured once (git registration + the
	//    directory + the holder scan). It is the axis that decides whether an
	//    ended owner left a checkout behind, and it is read BEFORE the rows are
	//    ranked because the checkout holder is itself evidence about a row.
	path, occupied, held, occWhy := branchAttributionCheckout(repo, branch, holders)
	d.Checkout = occupied
	// d.Held is the MEASURED half of the pair the operator-facing renders need:
	// "a checkout holds this branch" (Checkout — git refuses it to the merger)
	// versus "a live process is standing in that checkout RIGHT NOW" (Held —
	// the /proc holder scan). The field existed and no render carried it, so
	// every render could report only the LEDGER's opinion of a task row's
	// runner — and that opinion was printed as a fact about the checkout, which
	// is the misreading this epic reported. Both halves now travel together
	// (Rule 15: never let a render assert what the measurement did not say).
	d.Held = held
	// The ground's EVIDENCE and AGE travel with the decision for the same reason
	// Held does: the render that states a measurement must be able to show what
	// was measured (who is inside) and how long it has been standing, without
	// opening a probe of its own (Rule 13/14).
	d.HeldNames, d.Age, d.AgeKnown = branchAttributionCheckoutDetail(repo, path, held)
	if occupied {
		d.Worktree = path
		d.Evidence = append(d.Evidence, "checkout: "+path+" — "+occWhy)
	} else if occWhy != "" {
		d.Evidence = append(d.Evidence, "checkout: none — "+occWhy)
	}

	best := branchAttributionEvidence{Rank: -1, Rule: string(branchAttribUnowned)}
	matched := 0
	for i := range rows {
		row := rows[i]
		if row.Branch != branch {
			continue
		}
		matched++
		ev := branchAttributionJudgeRow(row, path, occupied, held)
		d.Evidence = append(d.Evidence, fmt.Sprintf("%s: %s", ev.Rule, ev.Why))
		if ev.Rank > best.Rank {
			best = ev
		}
	}
	if matched == 0 {
		// The ledger WAS read and says nothing about this branch.
		d.Verdict = branchAttribUnowned
		d.Reason = "no ledger row names this branch (the ledger was read) — nobody can be shown to own it"
		return d
	}
	d.Task, d.Reason = best.Task, best.Why
	// The ROW axis, measured here (ONE place, Rule 14) so every render of this
	// decision prints the same liveness answer beside the checkout's.
	d.RunnerKnown, d.RunnerLive = branchAttributionRunnerLiveness(rows, best.Task)
	if !occupied {
		// No checkout holds the branch: the row's own recorded directory (if
		// any) is the only path evidence, and it is reported as such — never
		// presented as ground somebody stands on.
		d.Worktree = branchAttributionRowWorktree(rows, best.Task)
	}
	switch best.Rank {
	case tierProcessLive, tierClaimLive, tierCheckoutLive:
		d.Verdict = branchAttribLiveHolder
		return d
	case tierLedgerEnded, tierProcessGone:
		// The row's run is over. Whether its GROUND is still occupied decides
		// which existing arm is the remedy — and the arm's own verdict is what
		// frees anything, never this sentence (memory #2324).
		if !occupied {
			// No checkout to walk back into: the registration (if any) is the
			// only thing left, and the age sweep's repair arm collects exactly
			// that.
			d.Verdict = branchAttribOwnerRecordOnly
			d.Reclaimer = branchArmReaper
			d.Reason += " — and no checkout holds the branch, so there is no interrupted run left to resume"
			return d
		}
		if best.Resumable {
			// A live-status row whose runner is provably gone, with its
			// checkout still standing: the resume arm's population.
			d.Verdict = branchAttribOwnerEnded
			d.Reclaimer = branchArmResume
			return d
		}
		// The row's work is over (terminal status). Its checkout is not
		// somebody's interrupted run — it is a spent one, and what is left to
		// release is the DIRECTORY (the reclaim arm's job), after which the
		// branch reaches the merge sweep like any other deliverable.
		d.Verdict = branchAttribOwnerEnded
		d.Reclaimer = branchArmReclaim
		d.Reason += " — its work is over; the reclaim arm releases the directory and the branch then merges like any other deliverable"
		return d
	default:
		d.Verdict = branchAttribOwnerRecordOnly
		d.Reclaimer = branchArmReaper
		return d
	}
}

// branchArchiveTagName renders the archive tag a branch would carry as a
// decided loser — the SAME namespace repoSweepArchiveExempt probes (Rule 14:
// one spelling, so a message can never name a ref the predicate does not read).
func branchArchiveTagName(branch string) string { return archiveBranchPrefix + branch }

// branchAttributionCheckout measures the branch's GROUND once: is it checked
// out in a linked worktree of repo, does that directory still exist, and does a
// live process hold it?
//
// The registration alone is NOT occupancy — git keeps listing a worktree whose
// directory is gone (until it is pruned), the distinction ReclaimHeldBranches
// and DeadHolderBranches both draw. Every "no" returns its own sentence so the
// decision can print WHY it concluded there is no ground (Rule 15), and no
// unreadable answer is ever rendered as a positive one.
func branchAttributionCheckout(repo, branch string, holders scratch.HolderScan) (path string, occupied, held bool, why string) {
	byBranch, _, err := mergedCleanupWorktrees(repo)
	if err != nil {
		return "", false, false, "the worktree listing could not be read (" + err.Error() + ") — no checkout is claimed either way"
	}
	p, ok := byBranch[branch]
	if !ok {
		return "", false, false, "git does not report this branch checked out in a linked worktree"
	}
	if _, statErr := os.Stat(p); statErr != nil {
		return p, false, false, "the registration at " + p + " has no directory (its checkout is gone; only git bookkeeping remains)"
	}
	return p, true, scratch.HolderVeto(p, holders) != "", "a checkout exists on disk"
}

// branchAttributionJudgeRow turns ONE row that names the branch into the
// strongest evidence it carries, given the branch's measured ground.
//
// THE ORDER IS THE WHOLE DESIGN, and two of its rungs were corrected against
// MEASURED false positives on the live ledger (2026-09-16):
//
//  1. A TERMINAL row's live claim pid is HISTORY, not life. Four completed /
//     failed rows on this host carry a claim stamp naming a still-running pid —
//     the long-lived `gopi worker` process that wrote the stamp and outlived the
//     row (memory #2161). Ranking a live claim pid above the ledger's own
//     verdict made finished rows read as "somebody is working here". The
//     ledger's declaration (a terminal status) therefore outranks pid evidence
//     for a row that is not in a live holder status.
//  2. A process standing in the checkout is real evidence but a WEAK identity:
//     it is not proven to be THIS row's runner (that is what the row's own
//     recorded triple is for), so it ranks below the triple and only counts for
//     a row that is still in a live status.
func branchAttributionJudgeRow(row tasksRowForSweep, checkout string, occupied, held bool) branchAttributionEvidence {
	live := taskLiveHolderStatus(row.Status)
	// A row that records a DIFFERENT directory than the branch's own checkout
	// is not this checkout's owner (the collision worktreeSweepRowNamesOtherDir
	// guards); its evidence still counts, but never as the ground's holder.
	ownCheckout := !occupied || row.Worktree == "" || sameWorkingPath(row.Worktree, checkout)
	if live {
		// (a) THE STRONGEST EVIDENCE: the row's own recorded runner incarnation
		//     is still alive. /proc carries pid + start-ticks + cmd, and a
		//     matching incarnation is unique, so this cannot be a recycled pid.
		//     Reuses the ONE implementation (finishCleanupProcessHolderLive)
		//     rather than opening a second /proc reader — a second liveness
		//     implementation is how a probe drifts back to the fail-open cwd
		//     scan of memory #2632.
		if ownCheckout && row.Ident.Pid > 0 && row.Ident.StartTicks > 0 && finishCleanupProcessHolderLive(row.Ident) {
			return branchAttributionEvidence{
				Rank: tierProcessLive, Task: row.TaskID, Rule: "runner-live",
				Why: fmt.Sprintf("task %s's recorded runner (pid %d, start ticks %d) is still running", row.TaskID, row.Ident.Pid, row.Ident.StartTicks),
			}
		}
		// (b) the claim stamp names a pid THIS host can see and that pid is
		//     alive. Only meaningful while the row is still in a live status;
		//     for a terminal row the stamp is history (rung 1 above).
		if ownCheckout {
			if _, pid, known := claimHolder(row.ClaimedBy); known && pidAlive(pid) {
				return branchAttributionEvidence{
					Rank: tierClaimLive, Task: row.TaskID, Rule: "claim-live",
					Why: fmt.Sprintf("task %s is claimed by a live local process (%s)", row.TaskID, row.ClaimedBy),
				}
			}
		}
	}
	// (c) the LEDGER itself declares the run over: a terminal status, or a live
	//     status whose ended_at is past the grace window — the same declaration
	//     the age sweep reads (Rule 14: one definition of "life ended").
	if !live || worktreeSweepRowLifeEnded(row, worktreeSweepOptions{Now: branchAttributionNow()}) {
		return branchAttributionEvidence{
			Rank: tierLedgerEnded, Task: row.TaskID, Rule: "ledger-ended",
			Why: fmt.Sprintf("the ledger declares task %s's run over (%s%s)", row.TaskID, row.Status, branchAttributionEndedDetail(row)),
		}
	}
	// (d) a POSITIVE death proof for the row's runner, or for its claim pid.
	//     Both are conservative by construction: finishCleanupDeadLocalHolder
	//     answers false for a foreign/blank stamp and for a row with no
	//     identity, and a foreign stamp is never called dead.
	if row.HolderProvenDead {
		return branchAttributionEvidence{
			Rank: tierProcessGone, Task: row.TaskID, Rule: "runner-gone", Resumable: true,
			Why: fmt.Sprintf("task %s is still %s but its recorded runner and claim pid are provably gone — an interrupted run nobody is standing on", row.TaskID, row.Status),
		}
	}
	// (e) the row's own runner is unprovable, but SOMEBODY is standing in the
	//     branch's checkout. Not provably this row's runner — hence below the
	//     triple — but "a process is there" must never read as "the ground is
	//     free" (the cardinal sin of memory #2632).
	if live && occupied && held && row.Worktree == "" {
		return branchAttributionEvidence{
			Rank: tierCheckoutLive, Task: row.TaskID, Rule: "checkout-held",
			Why: fmt.Sprintf("task %s is %s and a live process holds its branch's checkout %s (the row records no directory of its own, so the holder cannot be tied to this row — and it must not be read as absence)",
				row.TaskID, row.Status, checkout),
		}
	}
	// (f) nothing proves the run is over and nothing proves it is still going:
	//     the row is a RECORD, and a record must never hold a branch forever
	//     (memory #2145) — but it is also not a death proof, so it is ranked
	//     last and the caller routes it to the arms that can prove more.
	return branchAttributionEvidence{
		Rank: tierRecordOnly, Task: row.TaskID, Rule: "record-only",
		Why: fmt.Sprintf("task %s is %s and no probe could prove its run live or over (claim stamp %q) — an ownership record with no provable ground",
			row.TaskID, row.Status, row.ClaimedBy),
	}
}

// branchAttributionEndedDetail renders the second clause of a "ledger-ended"
// sentence: the ended_at stamp when that is what ended the row's life (a live
// status beside it), empty for a terminal status. ONE spelling for the
// distinction, so an operator can tell "the run finished" from "the row was
// stamped over without a status move".
func branchAttributionEndedDetail(row tasksRowForSweep) string {
	if row.terminal() || row.EndedAt <= 0 {
		return ""
	}
	return fmt.Sprintf(", ended_at %s", time.Unix(int64(row.EndedAt), 0).UTC().Format(time.RFC3339))
}

// branchAttributionRowWorktree returns the directory a named row records.
func branchAttributionRowWorktree(rows []tasksRowForSweep, taskID string) string {
	for _, row := range rows {
		if row.TaskID == taskID {
			return row.Worktree
		}
	}
	return ""
}

// branchAttributionCheckoutDetail adds the EVIDENCE and AGE of a checkout's
// ground to the held/occupied pair branchAttributionOccupancy decides:
//
//   - heldNames names the processes the holder scan found (scratch.HolderNames),
//     so a render can show what it saw instead of asking the operator to trust
//     an assertion. It is empty for the negative answer and when the probe could
//     not run.
//   - ageKnown/age: how long the checkout's git registration has existed. This
//     is the operator's own requested signal ("just report how long the
//     worktree/branch lives, which can arouse user attention") and it MEASURES
//     git's bookkeeping — never a run: a checkout whose registration is a day
//     old deserves a glance whether or not a process is inside it.
func branchAttributionCheckoutDetail(repo, path string, held bool) (heldNames []string, age time.Duration, ageKnown bool) {
	if path == "" {
		return nil, 0, false
	}
	if held {
		heldNames = scratch.HolderNames(path, 0)
	}
	age, ageKnown = branchAttributionCheckoutAge(repo, path)
	return heldNames, age, ageKnown
}

// branchAttributionCheckoutAge answers how long a checkout of repo has existed,
// by measuring the WRITE TIME of git's own registration for it
// (<repo>/.git/worktrees/<id>/gitdir): git writes that file when `git worktree
// add` creates the checkout and does not rewrite it afterwards, so it is the
// registration's birth as this host can report it. It is the same file the
// worktree sweep's repair arm already reads (Rule 13 — a second registration
// reader is how two answers drift apart).
//
// It is HONEST ABOUT FAILURE: an unreadable or unresolvable registration returns
// false, and the render then says NOTHING about age rather than printing a zero
// (Rule 15, and the operator's own instruction: no info beats wrong info).
func branchAttributionCheckoutAge(repo, path string) (time.Duration, bool) {
	if repo == "" || path == "" {
		return 0, false
	}
	admin, err := worktreeSweepAdminDir(repo, path)
	if err != nil || admin == "" {
		return 0, false
	}
	info, err := os.Stat(filepath.Join(admin, worktreeSweepGitdirFileName))
	if err != nil {
		return 0, false
	}
	age := branchAttributionNow().Sub(info.ModTime())
	if age < 0 {
		age = 0 // a clock that moved backwards is not an age; never print one
	}
	return age, true
}

// branchAttributionRunnerLiveness reads the ROW axis of liveness for the row an
// attribution names: does the row RECORD a runner identity (pid + kernel start
// ticks), and is that incarnation alive on this host right now?
//
// This is the half the checkout scan structurally cannot supply. The scan asks
// /proc who has a cwd / open fd / mapped binary inside a directory; a `gopi
// worker <task-id>` runs with its SESSION directory as its cwd (measured
// 2026-09-18), so its worktree reads "nobody inside" while the run is alive. The
// row's own recorded triple is the identity that closes that hole — and `gopi
// task show` already prints it, so the operator's expectation ("I believe
// current task show already reveals liveness of task running") is met by
// carrying it into this surface rather than inventing a second probe.
//
// known=false is a real answer — the row records no runner identity, so this axis
// is UNVERIFIED — and it must never be rendered as "not alive".
func branchAttributionRunnerLiveness(rows []tasksRowForSweep, taskID string) (known, live bool) {
	if taskID == "" {
		return false, false
	}
	for _, row := range rows {
		if row.TaskID != taskID {
			continue
		}
		if row.Ident.Pid <= 0 || row.Ident.StartTicks <= 0 {
			return false, false // no recorded identity: unverified, not dead
		}
		// ONE liveness implementation (finishCleanupProcessHolderLive), never a
		// second /proc reader: two readers drift, and the fail-open direction
		// deletes live worktrees (memory #2632).
		return true, finishCleanupProcessHolderLive(row.Ident)
	}
	return false, false
}

// branchAttributionOccupancy answers whether a checkout still OCCUPIES the
// branch, and whether a live process holds that checkout.
//
// The registration is what answers the first half: git keeps listing a worktree
// whose directory is gone (until it is pruned), and a registration is NOT an
// occupancy — the distinction ReclaimHeldBranches and DeadHolderBranches both
// make. An unreadable listing is reported as "not occupied", which is safe for
// the caller's use (the verdict then routes to the registration repair, which
// consults git itself); it is never the basis of a removal.
func branchAttributionOccupancy(repo, branch, recorded string, holders scratch.HolderScan) (occupied, held bool) {
	byBranch, _, err := mergedCleanupWorktrees(repo)
	if err != nil {
		return false, false // occupancy unreadable: never guess a checkout
	}
	path, ok := byBranch[branch]
	if !ok {
		return false, false // git does not say this branch is checked out
	}
	if recorded != "" && !sameWorkingPath(recorded, path) {
		// The registration git reports is a DIFFERENT directory from the one
		// the ledger recorded for this row: this row does not own that
		// checkout, and treating it as the row's ground would hand a foreign
		// tree to the row's arm.
		return false, false
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return false, false // a registration without a directory is not occupancy
	}
	return true, scratch.HolderVeto(path, holders) != ""
}

// BranchAttributionReport resolves EVERY branch the merger could act on in repo
// — the unprotected candidates it would merge, plus the worktree-protected ones
// a row attributes — and returns the decisions sorted by branch.
//
// It exists so the merger (and `gopi merger --status`) gets the WHOLE picture
// in ONE ledger read and ONE holder scan, rather than paying a query per
// branch: the evidence is collected once and judged per branch, exactly as the
// age sweep does it.
func BranchAttributionReport(repo string) ([]branchAttributionDecision, error) {
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer cancel()
	// THE LEDGER ERROR KEEPS ITS OWN VARIABLE (defect, 2026-09-17). It used to
	// be read into `err`, and the branch listing below REASSIGNS `err`:
	// `branches, err := branchAttributionBranches(repo)` overwrites the ledger
	// failure with nil the moment the listing succeeds. The `if !ledgerRead`
	// arm then wrapped a NIL error, so the operator read
	//
	//	branch attribution UNAVAILABLE: branch attribution: task ledger unreadable: %!w(<nil>)
	//
	// — the real cause was destroyed by the statement that reported it, and the
	// message claimed a failure it could no longer describe (Rule 15: a
	// swallowed error is a silent failure; a MIS-REPORTED one is worse, because
	// the operator cannot act on a cause that was overwritten).
	//
	// ledgerErr is never reused, so the nil path is UNREPRESENTABLE rather than
	// merely avoided: reaching the `!ledgerRead` arm means ledgerErr != nil, and
	// `%w` can never render `%!w(<nil>)` again.
	rows, ledgerErr := branchAttributionRows(ctx)
	ledgerRead := ledgerErr == nil
	if ledgerErr != nil {
		rows = nil
	}
	branches, err := branchAttributionBranches(repo)
	if err != nil {
		return nil, err
	}
	var out []branchAttributionDecision
	holders := scratch.ScanHolders()
	for _, b := range branches {
		out = append(out, BranchAttribution(repo, b, rows, holders, ledgerRead))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Branch < out[j].Branch })
	if !ledgerRead {
		// The ledger failure is reported through the verdicts (ungoverned), but
		// it must ALSO surface as an error: a caller that only renders decisions
		// would otherwise show six "ungoverned" lines without saying why.
		//
		// ledgerErr — never `err` — is what is wrapped here: it is the variable
		// that still HOLDS the ledger failure, and it is provably non-nil on
		// this arm. Which LEDGER was meant is named too, so the message is
		// diagnosable (missing file? directory? permission?) instead of merely
		// truthful (Rule 15).
		return out, fmt.Errorf("branch attribution: %s: %s: %w",
			branchAttributionLedgerUnreadable, branchAttributionLedgerCause(memoryDBPath()), ledgerErr)
	}
	return out, nil
}

// branchAttributionBranches lists the branch names worth an answer: every local
// head plus every worktree-registered branch (a protected branch is exactly the
// case the merger cannot decide from git alone), minus the integration
// namespaces every path exempts.
func branchAttributionBranches(repo string) ([]string, error) {
	names, err := repoSweepGit(repo, "for-each-ref", "--format=%(refname:short)", refsHeadsPrefix)
	if err != nil {
		return nil, fmt.Errorf("branch list: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	add := func(b string) {
		b = strings.TrimSpace(b)
		if b == "" || seen[b] || b == masterBranchName || b == mainBranchName {
			return
		}
		if len(out) >= branchAttributionBranchBudget {
			return // budget reached: reported by the caller's own count
		}
		seen[b] = true
		out = append(out, b)
	}
	for _, b := range strings.Fields(names) {
		add(b)
	}
	if byBranch, _, werr := mergedCleanupWorktrees(repo); werr == nil {
		for b := range byBranch {
			add(b)
		}
	}
	sort.Strings(out)
	return out, nil
}

// branchAttributionListMax bounds how many attributed branches the merger's
// log/status line names. It is small on purpose: the line is an operator
// signal, not a dump, and a truncated list says how many it left out.
const branchAttributionListMax = 20

// BranchAttributionLines is the branch-axis picture, reduced to the four fields
// an operator line needs. It is the ONE renderer (Rule 14) both front doors
// compose: the merger's per-repo tick (RepoSweepResult.Attribution) and the
// read-only status preview (RepoSweepPreview.Attribution).
//
// The returned error is NOT swallowed into an empty list: a caller that shows
// nothing must be able to say the question could not be asked, not imply that
// no branch is attributed (Rule 15).
func BranchAttributionLines(repo string) ([]BranchAttributionLine, string) {
	decisions, err := BranchAttributionReport(repo)
	out := make([]BranchAttributionLine, 0, len(decisions))
	for _, d := range decisions {
		if d.Verdict == branchAttribIntegration {
			continue // master/main/archive namespaces are not anyone's branch
		}
		out = append(out, BranchAttributionLine{
			Branch:    d.Branch,
			Verdict:   string(d.Verdict),
			Task:      d.Task,
			Reason:    d.Reason,
			Reclaimer: d.Reclaimer,
			Checkout:  d.Checkout,
			Worktree:  d.Worktree,
			Held:      d.Held,
			// The evidence and the row axis travel with the line for the same
			// reason Held does (defect ③ was a field that existed on the
			// decision and was dropped here): a render that states a measurement
			// must be able to show it. See BranchAttributionLine's own docs.
			Inside:        d.HeldNames,
			ProbeMeasured: scratch.ProcReadable(),
			RunnerKnown:   d.RunnerKnown,
			RunnerLive:    d.RunnerLive,
			Age:           d.Age,
			AgeKnown:      d.AgeKnown,
		})
		if len(out) >= branchAttributionListMax {
			break
		}
	}
	if err != nil {
		return out, err.Error()
	}
	return out, ""
}

// BranchAttributionSummaryLines renders the standalone operator face's picture:
// one header line plus one line per attributed branch, and the owning task id on
// each — so a reader of `gopi task sweep-worktrees` can tell an unfinished
// session's branch from an abandoned one before deciding on the plan.
//
// It is deliberately NOT the merger's rendering (BranchAttributionLines, which
// feeds the single protected line): this face has no per-branch plan to attach
// to, so it prints the branches themselves. Both consume the SAME classifier
// and neither re-derives a verdict.
func BranchAttributionSummaryLines(repo string) []string {
	if strings.TrimSpace(repo) == "" {
		return nil // no repo to attribute against: say nothing, never guess
	}
	lines, err := BranchAttributionLines(repo)
	var out []string
	var owned int
	for _, l := range lines {
		if l.Verdict != string(branchAttribUnowned) && l.Verdict != string(branchAttribIntegration) {
			owned++
		}
	}
	if owned > 0 {
		out = append(out, fmt.Sprintf("branch-attribution: %d branch(es) attributed to a task row — name the owner before recycling anything (a branch with no owner is attributed to nobody and is the only kind safe to recycle outright)", owned))
		for _, l := range lines {
			if l.Verdict == string(branchAttribUnowned) || l.Verdict == string(branchAttribIntegration) {
				continue
			}
			ground := "no checkout holds it"
			if l.Checkout {
				ground = "a checkout holds it"
				if l.Held {
					ground += ", with a live process inside"
				} else {
					ground += ", with no process inside"
				}
			}
			out = append(out, fmt.Sprintf("  %s: %s (task %s, %s) — %s", l.Branch, verdictOperatorWords(l.Verdict), l.Task, ground, l.Reason))
		}
	}
	if err != "" {
		// The honest "not measured" line: an empty picture must not read as a
		// clean bill of health (Rule 15).
		out = append(out, "branch-attribution UNAVAILABLE: "+err)
	}
	return out
}

// branchAttributionNow is the clock seam the attribution shares with its
// consumers (a test pins the ended_at grace window without reaching into the
// sweep's options). It reads the SAME clock production uses, so a verdict
// cannot be computed against a different today than the arm that acts on it.
var branchAttributionNow = time.Now
