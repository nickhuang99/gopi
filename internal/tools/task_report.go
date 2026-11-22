package tools

// task_report.go — the BOUNDED TASK REPORT seam (harness side).
//
// The epic: every task leaves a bounded, human-readable report in its OWN
// session directory, written at a DETERMINISTIC time by the harness, and
// `gopi task show <id>` surfaces it — so an operator can tell what a task
// did without reading a 10k-line jsonl.
//
// THREE faces, all keyed on the task id (the worker-output-log precedent:
// "given the task id, reach every artifact" must be one join, never a scan):
//
//	1. HAND-OFF  — ReportPath resolves <gopi-dir>/sessions/<task_id>/task-report.md;
//	               the wrapper exports it as taskreport.PathEnv on the child's
//	               environment BEFORE spawn (cmd.Env, not os.Setenv: a
//	               post-spawn setenv is too late — the child reads it at
//	               auto-registration, milliseconds in). The path is a
//	               HARNESS fact (relocatable by GOPI_HOME, migrated from the
//	               legacy root, pruned later), so it is never model-derived.
//	2. TRANSPORT — the executor writes the narrative to that path as its
//	               wrap-up artifact (the prompt contract names it). The file
//	               is the durable carrier: it survives the child's death, a
//	               killed writeback, and a pruned ledger.
//	3. SEAL      — the harness runs SealTaskReport at the run's terminal
//	               writeback: the report is BOUNDED to the cap, a model that
//	               wrote nothing gets an honest SKELETON (the facts the
//	               harness owns + an explicit "not recorded" marker — never a
//	               fabricated narrative), and the path + size + any missing
//	               required section is stamped into the run's result digest
//	               (the append-only task_events detail), so the ledger stays
//	               the index even after the directory is pruned.
//
// The seal NEVER clobbers model-authored content: an existing non-empty file
// is bounded in place and left otherwise untouched. The seam is
// model-INDEPENDENT in the sense that matters — a report file with the
// required sections always exists at the end of a run, whatever the model did.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/taskreport"
)

// TaskShowReportLabel is the render label of the report section in
// `gopi task show` (exported for the same reason TaskShowOutputLogLabel is:
// the CLI test asserts the operator-visible line without mirroring it).
const TaskShowReportLabel = "task_report:    "

// TaskShowReportWarnLabel is the warning line's label: the report exists but
// does not carry every required section — the operator sees the gap in the
// same output as the report, instead of trusting a partial one.
const TaskShowReportWarnLabel = "task_report_warn: "

// TaskShowReportClaimLabel is the label of the report's gate-claim finding in
// `task show`: a sentence asserting a gate result with nothing observed behind
// it. Exported for the same reason as the labels above — the CLI test asserts
// the operator-visible line, and the renderer and the test must share ONE
// definition of it (Rule 14).
const TaskShowReportClaimLabel = "task_report_claim: "

// TaskShowReportRanLabel is the label of the Rule 31 ran-check finding in
// `task show` — the read face of the seal's record, so the operator sees why a
// "completed" row's report never says its surface ran (Rule 31's checked face,
// runcheck.go). Exported for the same reason as the labels above.
const TaskShowReportRanLabel = "task_report_ran: "

// taskReportFileMode is the report file's permission mode (owner write, world
// readable — the operator reads it from another shell).
const taskReportFileMode = 0o644

// taskReportDirMode is the permission bits of a created session dir (mirrors
// the session logger's MkdirAll mode).
const taskReportDirMode = 0o755

// taskReportGitTimeout caps the single `git rev-parse` that resolves the
// reported commit (a wedged repo must never stall a run's writeback).
const taskReportGitTimeout = 5 * time.Second

// TaskReportPath resolves the ABSOLUTE path of a task's report:
// <gopi-dir>/sessions/<task_id>/task-report.md. It goes through
// taskSessionDir, so the report can never disagree with the attachment area,
// the worker output log, or the session JSONL about where a task's artifacts
// live (one root, one key).
func TaskReportPath(taskID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task report: task_id must not be empty")
	}
	sd, err := taskSessionDir(taskID)
	if err != nil {
		return "", fmt.Errorf("task report path for %s: %w", taskID, err)
	}
	return filepath.Join(sd, taskreport.FileName), nil
}

// TaskReportDestination is the EXPORT resolution of the report contract: the
// same path TaskReportPath resolves, but with its PARENT DIRECTORY made a
// PRECONDITION rather than an assumption.
//
// WHY THE PRECONDITION EXISTS (measured 2026-10-28, run 01a1828d): the spawn
// seam exported an absolute destination into the child's environment and the
// child's contract — stated in the prompt and obeyed by every run that writes
// a report — is `cat > "$GOPI_TASK_REPORT_PATH"`. Nothing verified that the
// directory holding that path existed, so a destination naming a container
// that was absent at spawn time reached the executor as a path `cat >` cannot
// open: the run's only report went nowhere, and the harness's close-out seal
// wrote a skeleton at ITS own resolution instead. Two witnesses of one
// contract disagreed about where the work goes, and the run could not tell.
//
// THE CRITERION, and it is the whole function: an exported path must be
// WRITABLE, else it must not enter the environment. So this resolves, creates
// the parent (MkdirAll, idempotent — the container is the row's own and a
// re-submit reuses it), and PROVES writability with a probe file before
// answering. Any failure is returned, and the seam that exports it prints the
// refusal and exports NOTHING (Rule 15: a destination that cannot be written
// is refused out loud, never handed over as a best effort).
//
// Rule 14: the path itself still comes from TaskReportPath — this function
// adds the precondition, never a second resolution. The READ faces keep using
// TaskReportPath, because a `task show` that created directories would be a
// display command mutating the filesystem.
func TaskReportDestination(taskID string) (string, error) {
	path, err := TaskReportPath(taskID)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(dir, taskReportDirMode); err != nil {
		return "", fmt.Errorf("task report destination for %s: create %s: %w", taskID, dir, err)
	}
	if err := taskReportProbeWritable(dir); err != nil {
		return "", fmt.Errorf("task report destination for %s: %w", taskID, err)
	}
	return path, nil
}

// taskReportProbeFile is the probe's file name inside the destination
// directory. It is created and removed inside the same call, so a directory
// that "exists" but cannot be written (a read-only mount, another user's
// container, a full filesystem) is caught HERE, where the refusal can be
// printed, instead of at the executor's first write.
const taskReportProbeFile = ".report-destination-probe"

// taskReportProbeWritable proves dir accepts a write, and fails naming the
// directory. The probe is removed whatever the outcome: an exported
// destination must not be left carrying this seam's litter, so a probe that
// could not be cleaned is reported too — "it worked but left a file behind"
// is the litter this precondition exists to stop.
func taskReportProbeWritable(dir string) error {
	probe := filepath.Join(dir, taskReportProbeFile)
	f, openErr := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, taskReportFileMode)
	if openErr == nil {
		openErr = f.Close()
	}
	removeErr := os.Remove(probe)
	if openErr != nil {
		// A failed create may still have left a partial file; the caller's
		// refusal names the directory, which is what the operator must fix.
		return fmt.Errorf("destination directory %s is not writable: %w", dir, openErr)
	}
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return fmt.Errorf("destination directory %s accepted the probe but would not release it: %w", dir, removeErr)
	}
	return nil
}

// SealTaskReport closes out a task's report at the run's terminal writeback
// and returns the digest line that records it ("" when there is nothing to
// record — no task id, or a path that cannot be resolved). Always best-effort:
// a seal failure is returned as an error for the caller to LOG and never to
// fail the run (the terminal writeback is the authoritative fact).
//
// The caller states NO outcome here, so the skeleton reports the row as the
// LEDGER currently reads it. Every close-out that IS closing out an outcome
// uses SealTaskReportOutcome instead (see its doc — the ordering defect this
// signature used to hide).
//
// MERGE NOTE (rework run 01a12e51, 2026-10-13): this signature and its
// SealTaskReportOutcome sibling arrive from the archived tip 352ba4726, which
// master (c136ccc27) had moved PAST — master's own work in this file is the
// FILTERED-PACKAGE CLAIM RECORD (taskReportFilteredClaimRecord, in
// SealTaskReportOutcome below, and the show-side FilteredClaimsIn block in
// TaskReportShowBlock). BOTH sides are kept in full and neither is a
// re-expression of the other: the branch named the outcome parameter, master
// added a second record on the same seal. No line of either side was dropped.
func SealTaskReport(taskID string) (string, error) {
	return SealTaskReportOutcome(taskID, "")
}

// SealTaskReportOutcome is SealTaskReport with the TERMINAL OUTCOME the
// caller is in the middle of writing stated explicitly, and it is the door
// every real close-out takes.
//
// WHY THE OUTCOME IS A PARAMETER (measured 2026-10-12, row 01a12de6-c052):
// the seal runs FIRST, inside writebackOutcome, BEFORE TaskRunWriteback lands
// the terminal status. So taskRowFacts could only ever read the row the
// writeback had not yet written — always `in_progress` — and the harness's own
// fallback report stated a status its own ledger would contradict one call
// later. 14 of 14 skeleton reports on this host carried exactly that
// (`status: in_progress`, `branch: (unknown)`, `commit: (unknown)`), including
// rows whose deliveries were real and merged. The seal KNOWS the outcome — it
// is the `status` argument of the very call that invoked it — so it must state
// that, not a snapshot of the row it is about to overwrite.
//
// Order matters (Rule 15 — no silent hole): the file is read FIRST, so a
// model-authored report is bounded and preserved; only an absent or
// whitespace-only file is replaced by the skeleton.
func SealTaskReportOutcome(taskID, outcome string) (string, error) {
	path, err := TaskReportPath(taskID)
	if err != nil {
		return "", err
	}
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(filepath.Dir(path), taskReportDirMode); err != nil {
		return "", fmt.Errorf("task report: create session dir for %s: %w", taskID, err)
	}
	body, readErr := os.ReadFile(path)
	authored := readErr == nil && strings.TrimSpace(string(body)) != ""
	if !authored {
		// No model narrative (child crashed, model skipped the artifact, or
		// the model wrote an empty file). The harness states what it owns and
		// marks the rest unrecorded — an explicit hole, not an invented story.
		// The skeleton carries the evidence section (SkeletonSections) and its
		// placeholder states the two accepted shapes.
		body = []byte(taskreport.Skeleton(taskID, taskReportFacts(taskID, outcome)))
	}
	bounded, truncated := taskreport.Bound(string(body))
	missing := taskreport.MissingSections(bounded)
	if string(body) != bounded || !authored {
		if err := os.WriteFile(path, []byte(bounded), taskReportFileMode); err != nil {
			return "", fmt.Errorf("task report: write %s: %w", path, err)
		}
	}
	line := taskreport.DigestLine(path, len(bounded), missing)
	if truncated {
		line += " [truncated]"
	}
	// GATE-CLAIM RECORD (gateclaim.go): the report is NEVER rewritten — no door
	// may silently edit a run's own sentences — but a `## Verification` claim
	// that asserts a gate result nobody observed is RECORDED here, on the
	// append-only digest the ledger keeps, and said out loud on stderr. The
	// motivating run claimed an empty worktree while its own close-out
	// machinery listed five dirty entries; a reviewer can act on a recorded
	// sentence, not on a trusted one (Rule 15).
	if record := taskReportGateClaimRecord(taskID, bounded); record != "" {
		line += " — " + record
	}
	// RULE 31 RECORD (runcheck.go): "a command you did not run is not delivered"
	// — the report must carry a `Ran:` line with the exact invocation. The
	// measured case (2026-09-21): `gopi task sync --push-only --task-id T` was
	// reported done, and one real invocation found it broken twice over; its
	// report carried Build:/Tests: only, so nothing in it could ask the
	// question. Same discipline as the gate-claim record directly above: the
	// report is NEVER rewritten and the seal is NEVER refused (see
	// devdoc/rule31-ran-check.md for the measured justification — a refusal
	// keyed on this predicate would refuse 71% of this repo's real reports),
	// but the gap is RECORDED on the append-only digest and said on stderr.
	if record := taskReportRanCheckRecord(taskID, bounded); record != "" {
		line += " — " + record
	}
	// MASTER-CLAIM RECORD (master_claim_report.go): a report that says the
	// integration branch ALREADY has something is checked against the object
	// graph — the commit it names must be an ANCESTOR of master
	// (`git merge-base --is-ancestor`, whose exit status is the verdict). The
	// measured case (session 01a0f3de) read the tip of an ARCHIVED branch as
	// "master already implements it" and closed exit=0, while the same run saw
	// the branch ref was gone: an archive tag keeps the commit reachable, so
	// reading it SUCCEEDED and a successful command reads like evidence. Same
	// action as the two records above — RECORD (digest + stderr), never rewrite
	// the report, never refuse the seal.
	if record := taskReportMasterClaimRecord(taskID, bounded); record != "" {
		line += " — " + record
	}
	// RESULT-DELEGATION RECORD (delegated.go): a report's done-gate chain is
	// build → tests → write the report → task(action=commit/review), so "the
	// report is written and the row is complete" and "the acceptance command has
	// no verdict yet" are the SAME state — and the report can hand the whole
	// conclusion to a path instead of stating it. The measured case (row
	// 01a12d6b, 2026-10-12): `## Verification` carried "Tests: `make test-budget
	// PKGS='./internal/testutil'` → see the stream at
	// `artifacts/make-testbudget-testutil.txt`", the acceptance stream afterwards
	// exited 2 with two red tests, and nothing at close-out looked at the
	// sentence at all. Same action as the three records above — RECORD (digest +
	// stderr), never rewrite the report, never refuse the seal — and the same
	// measurement discipline: each delegated path's EXISTENCE at this moment is
	// stamped beside it (an absent path is a dangling delegation; a present one
	// is recoverable, and the record must not imply more).
	if record := taskReportDelegatedRecord(taskID, path, bounded); record != "" {
		line += " — " + record
	}
	// FILTERED-PACKAGE CLAIM RECORD (filtered_report.go): a package-level test
	// reading is reported from a command whose own filter means the package was
	// never the thing that ran. The measured case (run 01a12ee4, 2026-10-12):
	// the run selected `./internal/standards` in its PKGS, killed that run, and
	// replaced it with a self-authored `-run 'Holder|LiveHolders|…'` whose
	// pattern matches no gate test; the report carried
	// `ok github.com/nickhuang99/gopi/internal/standards` while the package's whole run
	// on the same rev was FAIL exit 1. `scripts/test-budget.sh` models the
	// neighbouring gap (exit 5 refuses a filtered run that executed ZERO tests)
	// and CANNOT see this one: the run was not empty, it was simply not about
	// the package whose name the reading carries. Same action as the four
	// records above — RECORD (digest + stderr), never rewrite the report, never
	// refuse the seal — and the record carries the REMEDY (re-run the package
	// itself), because a finding that says only "wrong" leaves the author
	// guessing the fix.
	if record := taskReportFilteredClaimRecord(taskID, bounded); record != "" {
		line += " — " + record
	}
	return line, nil
}

// taskReportClaimWarnPrefix is the stderr prefix of the seal's gate-claim
// finding — the same "[task] " the sibling close-out record (the residue arm)
// uses, so an operator reading one line of a close-out knows which family it
// belongs to (Rule 1b: the prefix is named here, not typed twice below).
const taskReportClaimWarnPrefix = "[task] "

// taskReportGateClaimRecord adjudicates the sealed body's gate claims and
// returns the digest fragment naming the unobserved ones ("" when every claim
// either carries its observation, delegates to a framework-owned measurement,
// or is absent). The stderr warning is emitted HERE, so the operator sees the
// finding at the moment the artifact of record is closed out.
func taskReportGateClaimRecord(taskID, body string) string {
	verdict := taskreport.GateClaimVerdict(body)
	record := taskreport.DigestRecord(verdict)
	if record == "" {
		return ""
	}
	for _, line := range strings.Split(strings.TrimRight(taskreport.ClaimWarning(taskID, verdict), "\n"), "\n") {
		fmt.Fprintf(os.Stderr, "%s%s\n", taskReportClaimWarnPrefix, line)
	}
	return record
}

// taskReportRanCheckRecord adjudicates the sealed body's Rule 31 evidence (a
// `Ran:` line with an invocation, or a stated reason it could not be run) and
// returns the digest fragment naming the gap ("" when the report states one or
// the other). The stderr warning is emitted HERE, so the operator reads the
// finding at the moment the artifact of record is closed out.
func taskReportRanCheckRecord(taskID, body string) string {
	verdict := taskreport.RunCheckVerdict(body)
	record := taskreport.RunCheckRecord(verdict)
	if record == "" {
		return ""
	}
	for _, line := range strings.Split(strings.TrimRight(taskreport.RunCheckWarning(taskID, verdict), "\n"), "\n") {
		fmt.Fprintf(os.Stderr, "%s%s\n", taskReportClaimWarnPrefix, line)
	}
	return record
}

// taskReportFacts collects the facts the HARNESS owns for a skeleton report
// (and for the show-side "harness facts" echo): the ledger row's identity and
// outcome, plus the branch/commit of the executed work. Every value is
// optional — an unresolvable one renders "(unknown)", never a fabricated
// value.
//
// outcome is the TERMINAL status the caller is writing right now ("" when the
// caller is not a close-out). It WINS over the row's stored status, because
// the seal runs BEFORE the writeback lands it (see SealTaskReportOutcome): a
// report that read the row here would state the status it is about to
// replace. The row is still read for the values only the ledger holds
// (session id, created_at), and its status is the honest fallback for a
// caller that has no outcome to declare.
func taskReportFacts(taskID, outcome string) []string {
	status, sessionID, createdAt := taskRowFacts(taskID)
	if strings.TrimSpace(outcome) != "" {
		status = outcome
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = taskReportSessionID()
	}
	branch := taskReportBranch(taskID)
	commit := taskReportHeadCommit(taskID, branch)
	return []string{
		taskreport.FormatFact("task_id", taskID),
		taskreport.FormatFact("status", status),
		taskreport.FormatFact("session_id", sessionID),
		taskreport.FormatFact("branch", branch),
		taskreport.FormatFact("commit", commit),
		taskreport.FormatFact("created_at", taskReportCreatedAt(createdAt)),
	}
}

// taskReportSessionID is the RUN's session id when the row does not carry one
// yet — the same two env seams the commit resolver reads (the CLI exports them
// for its child; TaskChildSessionEnv is the one the task run's child session
// id arrives on).
//
// WHY (measured 2026-10-12, the live close-out of row 01a12e32-f80c): the row
// carried no session_id at the moment the seal ran (the writeback that stamps
// it is the call the seal precedes — the same ordering defect as the status),
// so the skeleton printed `session_id: (unknown)` while gopi's own
// `task show` named the session's JSONL on the very next line. An id the run
// is holding must not be rendered as unknown.
func taskReportSessionID() string {
	if sid := strings.TrimSpace(os.Getenv(TaskChildSessionEnv)); sid != "" {
		return sid
	}
	return strings.TrimSpace(os.Getenv(TaskExecutorSessionEnv))
}

// taskReportBranch resolves the branch the executed work lives on, from two
// channels in ONE order:
//
//  1. the ROW's own record — deliverable.branch, backfilled at claim time and
//     again at completion. This is the ledger's answer and it survives a
//     moved/deleted worktree, a pruned session directory and a wrapper that
//     died before its close-out.
//  2. the CHILD SESSION's resolution (taskWritebackBranch) — the only channel
//     that works for a run whose row never got a branch stamped.
//
// WHY THE FALLBACK EXISTS (measured 2026-10-12, row 01a12de6-c052): the
// skeleton resolved branch/commit from the child-session ENV alone, so it
// printed `branch: (unknown)` on a row whose ledger deliverable named
// `task/01a12de6c05273469efcbbd885387489` — the harness's own report
// contradicting the harness's own ledger about a fact the ledger was holding.
func taskReportBranch(taskID string) string {
	if _, branch, _ := taskRowDeliverable(taskID); branch != "" {
		return branch
	}
	return taskWritebackBranch()
}

// taskRowFacts reads the row's status / session id / created_at (best-effort:
// a DB miss yields empty values, which render as "(unknown)").
func taskRowFacts(taskID string) (status, sessionID string, createdAt float64) {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", "", 0
	}
	defer db.Close() //stylegate:ignore process-lifetime handle closed at function exit; the OS reclaims the descriptor
	t, err := taskByID(ctx, db, taskID)
	if err != nil || t == nil {
		return "", "", 0
	}
	return t.Status, t.SessionID, t.CreatedAt
}

// taskReportCreatedAt renders a unix-seconds stamp for the facts list.
func taskReportCreatedAt(sec float64) string {
	if sec <= 0 {
		return ""
	}
	return time.Unix(int64(sec), 0).UTC().Format(time.RFC3339)
}

// taskReportHeadCommit resolves the executed work's commit tip: the branch
// ref in whichever ground records it, then `git -C <dir> rev-parse --short`.
// "" on any failure — a guess would be worse than an admission.
//
// The candidate ORDER is a widening net, each rung a real shape:
//
//  1. the CHILD SESSION's launch workdir — the branch seam's own resolution
//     (one authority for "where did this run work"); exact while the worktree
//     is alive.
//  2. the row's recorded deliverable.worktree — the ground survives a session
//     directory that was pruned or a child env that never reached this
//     process (measured: the skeleton printed `commit: (unknown)` for a run
//     whose branch tip was reachable the whole time).
//  3. the row's recorded deliverable.repo — the SHARED repo, where a linked
//     worktree's branch ref lives even after the worktree is removed. This is
//     the rung that answers for a delivered-and-cleaned-up run, and it is
//     read as a REF (refs/heads/<branch>), never as the repo's own HEAD —
//     the main checkout is usually on master and reading it would name the
//     wrong commit.
//
// A rung is tried only when a directory is known; branch == "" makes rung 3
// read the directory's own HEAD (the only reading available without a name).
func taskReportHeadCommit(taskID, branch string) string {
	for _, dir := range taskReportCommitDirs(taskID) {
		if out := taskReportRevParse(dir, branch); out != "" {
			return out
		}
	}
	return ""
}

// taskReportCommitDirs lists the directories a commit may be read from, in
// the priority order documented on taskReportHeadCommit. Duplicates and
// empties are dropped so no rung is probed twice.
func taskReportCommitDirs(taskID string) []string {
	var dirs []string
	add := func(d string) {
		d = strings.TrimSpace(d)
		if d == "" {
			return
		}
		for _, seen := range dirs {
			if seen == d {
				return
			}
		}
		dirs = append(dirs, d)
	}
	sid := strings.TrimSpace(os.Getenv(TaskChildSessionEnv))
	if sid == "" {
		sid = strings.TrimSpace(os.Getenv(TaskExecutorSessionEnv))
	}
	if sid != "" {
		add(taskWorkdirFromSession(sid))
	}
	_, _, worktree := taskRowDeliverable(taskID)
	add(worktree)
	add(TaskDeliverableRepoFor(taskID))
	return dirs
}

// taskReportRevParse reads the tip of dir: refs/heads/<branch> when a branch
// name is known, dir's own HEAD otherwise. "" on any failure (not a work
// tree, wedged repo, deleted branch) — the caller moves to the next rung.
func taskReportRevParse(dir, branch string) string {
	ref := "HEAD"
	if b := strings.TrimSpace(branch); b != "" {
		ref = refsHeadsPrefix + b
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskReportGitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--short", ref).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// TaskReportShowBlock renders the `task show` report section: the bounded
// body when the file is present, the explicit PATH when the body is absent
// but the ledger recorded one (the pruned-directory case), and the honest
// degradation line for a row that never had a report (pre-seam history).
// empty = nothing to render (a row with no report and no recorded history —
// the caller then prints nothing rather than an empty header).
func TaskReportShowBlock(taskID string, createdAt float64, digest string) string {
	path, err := TaskReportPath(taskID)
	if err != nil {
		return ""
	}
	body, readErr := os.ReadFile(path)
	if readErr == nil && strings.TrimSpace(string(body)) != "" {
		var b strings.Builder
		fmt.Fprintf(&b, "%s%s\n", TaskShowReportLabel, path)
		if missing := taskreport.MissingSections(string(body)); len(missing) > 0 {
			fmt.Fprintf(&b, "%smissing required section(s): %s\n", TaskShowReportWarnLabel, strings.Join(missing, ", "))
		}
		// The gate-claim finding, on the surface the operator actually reads
		// (task show), not only in the ledger's digest: a claim that asserts a
		// gate result with nothing behind it is named here, next to the report
		// that makes it (gateclaim.go — the motivating case).
		if verdict := taskreport.GateClaimVerdict(string(body)); len(verdict.MissingSentences) > 0 {
			fmt.Fprintf(&b, "%s%s\n", TaskShowReportClaimLabel, taskreport.ClaimWarningLabel)
			for _, s := range verdict.MissingSentences {
				fmt.Fprintf(&b, "%s  %s\n", TaskShowReportClaimLabel, taskShowIndentAfterFirst(s))
			}
		}
		// The Rule 31 finding, on the same surface: the report says nothing about
		// a surface having been RUN and states no reason (runcheck.go). Rendered
		// BESIDE the authored text, never instead of it.
		if verdict := taskreport.RunCheckVerdict(string(body)); verdict.Missing() {
			fmt.Fprintf(&b, "%s%s\n", TaskShowReportRanLabel, taskreport.RanCheckNote)
		}
		// The result-delegation finding, on the same surface: the sentences
		// that hand a gate's conclusion to a path instead of stating it — the
		// state the done-gate chain (build → tests → report → commit) makes
		// reachable, where the report is already written and the acceptance
		// command has no verdict yet (delegated_report.go; row 01a12d6b).
		// Each path's status is measured NOW, at read time, and the label says
		// so — the SEAL-time measurement rides the row's digest.
		if verdict := taskreport.DelegatedClaimsIn(string(body)); len(verdict.Claims) > 0 {
			fmt.Fprintf(&b, "%s%s%s\n", TaskShowReportDelegatedLabel,
				taskreport.DelegatedWarningLabel, taskReportReadTimeStatusNote)
			for _, c := range verdict.Claims {
				fmt.Fprintf(&b, "%s  %s\n", TaskShowReportDelegatedLabel, taskShowIndentAfterFirst(c.Sentence))
				for _, p := range c.Paths {
					fmt.Fprintf(&b, "%s    %s — %s\n", TaskShowReportDelegatedLabel, p,
						delegatedPathStatusNow(p, filepath.Dir(path)))
				}
			}
		}
		// The filtered-package finding, on the same surface: the sentences that
		// report a PACKAGE-level test reading from a command whose own
		// `-run`/`-skip`/`-bench` filter removed tests from the selection, so
		// the package line they quote is not a reading of the package — the
		// compressed form gateclaim.go cannot see, because it names its own
		// instrument (filtered_report.go; run 01a12ee4). Rendered BESIDE the
		// authored text, never instead of it.
		if verdict := taskreport.FilteredClaimsIn(string(body)); len(verdict.Claims) > 0 {
			fmt.Fprintf(&b, "%s%s%s\n", TaskShowReportFilteredLabel,
				taskreport.FilteredClaimWarningLabel, filteredClaimRemedyNote)
			for _, c := range verdict.Claims {
				fmt.Fprintf(&b, "%s  %s\n", TaskShowReportFilteredLabel, taskShowIndentAfterFirst(c.Sentence))
				fmt.Fprintf(&b, "%s    filter: %s\n", TaskShowReportFilteredLabel,
					strings.Join(c.Filters, taskreport.FilteredFlagSeparator))
			}
		}
		shown, truncated := taskreport.TruncateBody(string(body))
		// The bounded body is AUTHORED text. It used to be appended at column 0,
		// which is how a report quoting a shell transcript contributed a
		// `status:` line `grep -c '^status:'` could not tell from the digest's
		// own field (the 2026-10-29 defect — see taskShowQuotedIndent). The
		// block's OWN lines above (path, warnings, findings) keep their labels
		// because this package writes them; the body is quoted.
		b.WriteString(taskShowIndentLines(shown))
		if !strings.HasSuffix(shown, "\n") {
			b.WriteString("\n")
		}
		if truncated {
			fmt.Fprintf(&b, "(body truncated for display — read %s for the full report)\n", path)
		}
		return b.String()
	}
	// The body is gone or was never written. The ledger may still hold the
	// path (recorded at seal time) — render it rather than pretending the
	// task never produced one.
	if recorded := taskreport.PathFromDigest(digest); recorded != "" {
		return fmt.Sprintf("%s%s (not found — recorded at run end; file pruned or removed)\n", TaskShowReportLabel, recorded)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		return fmt.Sprintf("%s%s (empty — the model wrote no report)\n", TaskShowReportLabel, path)
	}
	return fmt.Sprintf("%s%s\n", TaskShowReportLabel, taskreport.DegradationNote(createdAt))
}
