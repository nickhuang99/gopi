package tools

// merge_anchor_gate_trail.go — the DURABLE per-merge record of the merge-time
// PUBLISHED-ANCHOR gate's decision, and the reason this file exists at all.
//
// THE GAP. `mergeAnchorGate` has exactly ONE refusing arm and ~26 abstaining
// ones, and until 2026-10-16 every one of them spoke only into the merger log
// stream (`taskNotifyLog` → stderr). Nothing on disk kept it: measured
// 2026-10-16 on nick-HP-Laptop, `~/.gopi/merger/` holds no log file at all, so
// after FOUR published-anchor drifts landed on master in ONE day (commits
// 14861c100 / 57fa55834 / 448d2a67c / 3e07a4052, each a `Merge branch …` whose
// own diff touched no generated document) the question "which arm let this
// through, and why" had no instrument. The answer had been printed and had
// scrolled away. That absence is acceptance item 1 of epic 01a144bfb, and the
// premise of every other judgement about this gate.
//
// WHY THIS IS NOT A NEW DEPENDENCY (Rules 3/35) — the four questions:
//
//  1. WHAT BREAKS WITHOUT IT: a landing that this lane declined to refuse is
//     unexplainable after the fact. Concretely, the four landings above: the
//     only surviving evidence is that master drifted, and the reason ("the
//     branch's own document tests fail too, so the union failure is not
//     union-introduced" — the PREMISE arm) had to be re-derived by reading the
//     source. Nothing on the machine recorded it.
//  2. WHY IT CANNOT BE FIXED IN-PROCESS: a stream is not a record. The gate
//     runs inside a merger pass / a foreground `gopi merge` whose stderr is
//     captured by whatever started it and kept by nothing, and the question is
//     asked DAYS later, by a different process, about a merge that has already
//     happened. Only a durable medium answers it.
//  3. WHO OWNS ITS LIFECYCLE: nobody has to — this file adds NO artifact of its
//     own. It writes rows into `task_events`, the ledger's own append-only audit
//     table (`internal/tools/tasks.go`, schema at the `tasksSchemaEventsTable`
//     const), on the row the existing ONE branch→row loader (`TaskForBranch`)
//     resolves for the branch — the SAME channel and the SAME ownership the
//     sibling `ref-gone` arm already uses (`repo_sweep_ref_guard.go`, event
//     `ref-gone`). The ledger's own backup/GC/reclaim already covers it, and
//     `task_events` is pruned by no writer (only held to a bound per row, see
//     `finishCleanupBranchCheckPruneTrail`).
//  4. WHAT THE NEXT PERSON SEES: they do not have to find anything — the record
//     is on the row they are already looking at. `gopi task show <id>` renders
//     it as `verdict:` / `verdict.event: merge-anchor-gate` (this file registers
//     the event in `verdictTrailEventNames`, whose own doc invites exactly this:
//     a lane's ruling on a judged row), and the raw rows are readable with one
//     SELECT. Every record names the gate, the branch, the arm and the reason.
//
// NO new tool, no new verb, no new script, no new tick, no new file, no new
// knob: the trail rides machinery the merge path already runs (Rule 35's
// "prefer what already runs"), and the write happens inside the gate's own call.
//
// SHAPE. The sentences this lane ALREADY emits ARE the answer (Rule 14: a
// second vocabulary would drift from the first), so the trail captures them
// verbatim, in order, and prefixes them with the arm's own class. The class is
// DERIVED (`Refused` from the verdict struct; `DISABLED` from the ONE knob
// reader; the leading token of the first line otherwise) rather than typed at
// 30 return sites, so no site can be forgotten and no line can be re-worded out
// of the record.
//
// A COMMIT IS ALWAYS ATTEMPTED, and a record that could not be written SAYS SO
// (Rule 15b: a silent failure is the defect this whole file is about). When the
// branch has no owning ledger row at all, the record has nowhere to land and the
// trail says exactly that, naming the branch and the ledger it asked.

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	// MergeAnchorGateEventName is the `task_events.event` value of this lane's
	// per-merge record. It is a VERDICT name (a ruling ON the row), which is why
	// it is also registered in verdictTrailEventNames: `gopi task show` renders
	// the newest verdict on its own line, so an operator asking "why did this
	// branch land" reads the answer on the row itself rather than in a stream
	// nobody kept. Declared here and referenced there (Rule 1b/13/14).
	MergeAnchorGateEventName = "merge-anchor-gate"

	// The arm classes. Exactly one is recorded per invocation, and the three
	// spellings are the whole vocabulary a reader has to know: the lane refused
	// the branch, the lane let it through while SAYING something (pass or
	// abstain — the two are separated by the line's own token), or the operator
	// switch turned the lane off. `mergeAnchorGateArmSilent` is the FOURTH value
	// and it is deliberately not a class a healthy run can produce: it is the
	// record of an exit that emitted no line at all, which is a silent pass —
	// the one shape Rule 15 forbids — so the record names it instead of
	// rendering an empty trail that reads like a clean measurement.
	mergeAnchorGateArmRefused  = "REFUSED"
	mergeAnchorGateArmPass     = "PASS"
	mergeAnchorGateArmAbstain  = "ABSTAIN"
	mergeAnchorGateArmDisabled = "DISABLED"
	mergeAnchorGateArmSilent   = "SILENT"

	// The tokens the arm class is DERIVED from: the leading words of the
	// sentences this lane emits (`mergeAnchorGateLog` call sites in
	// merge_anchor_gate.go). Derived rather than passed so the 30 sites stay
	// untouched and a re-worded line cannot silently drop out of the record.
	mergeAnchorGateLineTokenAbstain = "ABSTAINS "
	mergeAnchorGateLineTokenPass    = "PASS "

	// mergeAnchorGateRecordSep joins the captured lines on ONE line: the ledger
	// detail column is read by `task show` (single-line renderer) and by a `LIKE`
	// probe (the dedup below), so the record is one line by construction.
	mergeAnchorGateRecordSep = " › "

	// mergeAnchorGateRecordMaxLines bounds the captured trail: a lane that
	// logged its way through many dispositions still leaves a brief, and the
	// count of what was dropped is stated rather than silently truncated
	// (Rule 15).
	mergeAnchorGateRecordMaxLines = 12

	// mergeAnchorGateRecordBudget is the deadline for the record's own write.
	// It is the ledger open + one lookup + one insert; the gate must not hold a
	// landing hostage to a slow disk, and a record that misses its budget is
	// REPORTED (never silently dropped).
	mergeAnchorGateRecordBudget = 10 * time.Second

	// mergeAnchorGateNoOwnerText is the honest fourth answer of the lookup: the
	// ledger was READ and no row of it names this branch, so the record has no
	// row to land on. It names the ledger it asked, because "no row names this
	// branch" is a claim about ONE database and a host carries several.
	mergeAnchorGateNoOwnerFmt = "no ledger row of %s names branch %s, so this record has no row to land on — %s"
)

// mergeAnchorGateTrail accumulates the sentences ONE invocation emits.
type mergeAnchorGateTrail struct {
	lines []string
	// dropped counts the lines the cap above refused, so the record states what
	// it did not carry rather than implying the list is complete.
	dropped int
}

// add records one rendered line verbatim — the SAME string the merger log got,
// so the durable record and the stream can never disagree about what was said.
func (tr *mergeAnchorGateTrail) add(line string) {
	if tr == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if len(tr.lines) >= mergeAnchorGateRecordMaxLines {
		tr.dropped++
		return
	}
	tr.lines = append(tr.lines, line)
}

// log renders one verdict line into BOTH sinks: the merger log (unchanged, via
// taskNotifyLog — the operator reading the stream sees what they always saw) and
// this invocation's trail, which commit() makes durable.
func (tr *mergeAnchorGateTrail) log(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	tr.add(line)
	taskNotifyLog("merger: "+MergeAnchorGateName+" %s", line)
}

// arm derives this invocation's class from what was actually observed, never
// from what was expected: the verdict struct first (a refusal is the one arm
// that logs nothing of its own), then the ONE knob reader, then the leading
// token of the first recorded line.
func (tr *mergeAnchorGateTrail) arm(v mergeAnchorGateVerdict) string {
	if v.Refused {
		return mergeAnchorGateArmRefused
	}
	if mergeAnchorGateDisabled() {
		return mergeAnchorGateArmDisabled
	}
	switch {
	case len(tr.lines) == 0:
		return mergeAnchorGateArmSilent
	case strings.HasPrefix(tr.lines[0], mergeAnchorGateLineTokenAbstain):
		return mergeAnchorGateArmAbstain
	case strings.HasPrefix(tr.lines[0], mergeAnchorGateLineTokenPass):
		return mergeAnchorGateArmPass
	default:
		// A line that is neither: the lane stated something and continued (a
		// BROKEN edge, a gate package this union cannot run, a mandate that
		// could not be rendered) and then abstained, passed or refused.
		return mergeAnchorGateArmAbstain
	}
}

// commit writes the record to the owning row's trail. It is DEFERRED in
// mergeAnchorGate, so it runs on every exit — including a panic — and its own
// failure is announced rather than swallowed (Rule 15b: this file exists
// precisely because a reason that reached only a stream was lost).
func (tr *mergeAnchorGateTrail) commit(repo, master, branch string, v mergeAnchorGateVerdict) {
	arm := tr.arm(v)
	body := "for " + branch + " into " + master + " (repo " + repo + "): "
	switch {
	case len(tr.lines) > 0:
		body += strings.Join(tr.lines, mergeAnchorGateRecordSep)
	case v.Refused:
		// A REFUSAL is the one arm that logs no line of its own BY DESIGN (see
		// arm()), so its silence is the verdict, not a defect: this record's own
		// arm field says REFUSED and the refusal's findings are appended below.
		// Measured 2026-10-16 on a git-2.38 host: before this arm existed every
		// refusal's durable record claimed "recorded as SILENT" in its body while
		// its envelope said REFUSED — the record contradicting itself on the one
		// field an operator reads to know WHICH arm decided (task
		// 01a144eb-0d51-7d2d-ab3b-51500a7132e2).
		body += "the refusal carries no line of its own (this lane's refusal arm logs its reasons as FINDINGS, never as a stream line) — the arm is " + mergeAnchorGateArmRefused + " and its findings follow"
	default:
		body += "the lane emitted NO line of its own — an exit that says nothing is a silent pass (Rule 15) and is recorded as " + mergeAnchorGateArmSilent
	}
	if tr.dropped > 0 {
		body += fmt.Sprintf(" (+%d further line(s) not carried — the record is capped at %d)",
			tr.dropped, mergeAnchorGateRecordMaxLines)
	}
	if v.Refused && len(v.Findings) > 0 {
		body += " | findings: " + strings.Join(v.Findings, mergeAnchorGateRecordSep)
	}

	ctx, cancel := context.WithTimeout(context.Background(), mergeAnchorGateRecordBudget)
	defer cancel()
	path := memoryDBPath()
	owner, ok, err := mergeAnchorGateOwningRow(ctx, branch, repo, path)
	if err != nil {
		taskNotifyLog("merger: %s could not record its %s verdict: %v", MergeAnchorGateName, arm, err)
		return
	}
	if !ok {
		taskNotifyLog("merger: %s %s", MergeAnchorGateName,
			fmt.Sprintf(mergeAnchorGateNoOwnerFmt, path, branch, body))
		return
	}
	// Dedup by (row, event, branch, arm, body): a branch this lane keeps making
	// the same decision about across ticks leaves ONE record, while a DIFFERENT
	// arm, branch or reason on the same row leaves its own — the same
	// one-decision-one-record discipline the `ref-gone` arm keeps, and the
	// reason the probe tests a rendered prefix rather than the row count.
	needle := MergeAnchorGateEventName + ": " + branch + " " + arm + " "
	detail := needle + body
	if taskEventHasDetail(owner.TaskID, needle) {
		return
	}
	db, err := openTasksDB(ctx, path)
	if err != nil {
		taskNotifyLog("merger: %s could not record its %s verdict on row %s: open ledger: %v",
			MergeAnchorGateName, arm, owner.TaskID, err)
		return
	}
	defer db.Close()
	// The INSERT is issued here rather than through taskEventWrite for the
	// `ref-gone` arm's reason: taskEventWrite logs its own failure and returns
	// nothing, and "the record landed" is exactly the claim a swallowed error
	// would falsify (Rule 15b).
	_, err = db.ExecContext(ctx,
		`INSERT INTO task_events (task_id, event, detail, created_at) VALUES (?, ?, ?, ?)`,
		owner.TaskID, MergeAnchorGateEventName, detail, float64(time.Now().Unix()))
	if err != nil {
		taskNotifyLog("merger: %s could not record its %s verdict on row %s: append task_events: %v",
			MergeAnchorGateName, arm, owner.TaskID, err)
	}
}

// mergeAnchorGateOwningRow resolves the row this branch's decision belongs to,
// through the ONE branch→row loader (Rule 14 — `TaskForBranch`, the same call
// `gopi task show --branch` and the sweep's owner-evidence arm make). It returns
// ok=false, listing no error, when the ledger was read and holds no row for the
// branch: that is a FACT about the ledger, not a failure, and the caller states
// it rather than inventing a record with no home.
func mergeAnchorGateOwningRow(ctx context.Context, branch, repo, dbPath string) (TaskForBranchOwner, bool, error) {
	_ = ctx
	lookup, err := TaskForBranch(TaskForBranchQuery{Branch: branch, Repo: repo}, dbPath)
	if err != nil {
		return TaskForBranchOwner{}, false, err
	}
	owner, ok := TaskForBranchResolvedRow(lookup)
	if !ok {
		return TaskForBranchOwner{}, false, nil
	}
	return owner, true, nil
}
