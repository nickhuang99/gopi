package agent

import (
	"fmt"
	"github.com/nick/gopi/internal/tasksource"
	"io"
	"os"
	"strings"

	"github.com/nick/gopi/internal/boolvocab"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/runcmd"
	"github.com/nick/gopi/internal/runend"
	"github.com/nick/gopi/internal/tools"
	"github.com/nick/gopi/internal/worktreerelease"
)

// runEndReasonRecorded reads the truncation the run recorded on a row (the
// wrapper's close-out fallback — same fact, read from the ledger so the two
// writers agree instead of racing). Typed alias of tools.RunEndReasonFor so the
// close-out path reads in the agent's own vocabulary.
func runEndReasonRecorded(runID string) runend.Reason {
	return tools.RunEndReasonFor(runID)
}

// agentProgName names the EXECUTOR face in the operator-facing diagnostics the
// agent package emits on its own behalf (the cwd release's failure line, for
// one). Spelled once (Rule 1) so a log grep for the face matches every line.
// It lives in THIS file rather than loop.go for a measured reason: adding lines
// to loop.go shifts every line number after them, and the corpus cites that file
// by line (870 `loop.go:NNN` references across 166 documents), so an insertion
// makes a pile of unrelated docs stale. The release call in loop.go is
// unavoidable; a new constant there is not. That cost is churn, not a gate —
// the check that once asserted those citations
// (the devdoc line-anchor gate, which failed when an anchor drifted onto a
// blank line) was deleted with internal/docclass in 5a35f61, and nothing reads a
// `loop.go:NNN` reference now. The census that replaced it over CONFIG.md's rows
// (`config-anchor-census.go --check`, its TestMergeConfigAnchorGate* arms in
// internal/tools) went with the documentation-citation family in 78bdf05a
// (2026-11-05): no executable anchor check runs on this tree.
const agentProgName = "gopi executor"

// Task-ledger auto-registration (启动即登记/结束补统计). Every gopi run is a
// task: the start hook inserts an in_progress row into the tasks ledger with
// an auto-generated t-<unix> task_id (task-id-style consolidation: the task
// row and the session are DIFFERENT entities — task_id is the ledger key,
// session_id the JSONL link), the end hook flips it to completed/failed by
// exit code and records the run stats as a task_events result digest (the
// same numbers the session_end event carries; bug056: the summary column is
// never overwritten — the [auto] briefing stays verbatim, stats ride the
// audit trail). The row's task_id is retained on the Agent (autoTaskRowID)
// so the end hook, terminal push, finish-cleanup and heartbeat all close
// exactly the row the start hook created — no task_id==session_id coupling.
//
// task-single-row-exec (2026-09-10): auto registration is reserved for runs
// with NO ledger row — bare prompt/chat/web sessions. A `task run` CHILD
// (GOPI_TASK_WRAPPER_ID exported pre-spawn) already HAS its human wrapper
// row (the single execution row carrying session_id / run provenance / claim
// / writeback), so the start hook skips auto registration entirely and
// latches autoTaskRowID to the wrapper row's task_id — killing the duplicate
// human+auto two-row structure (双份维护: closeout×2 / display×2 / sync×2 /
// stale×2).
//
// Design constraints (devdoc/task-autoregister-impl.md):
//
//   - Harness-side only: NO new LLM tool (Rule 3) — the ledger stays an
//     audit layer; runtime task state remains the LLM's [TASK_LIST] (the
//     Stateless philosophy stands).
//
//   - Best-effort: any DB failure is logged and swallowed — never blocks
//     the run (the ledger records the run; it must not be able to kill it).
//
//   - Default ON; GOPI_AUTO_TASK_REGISTER=0 (or off/no/false/disabled)
//     turns it off (Rule 2: the env var is read in exactly one place).
//
//   - Ownership stamp (stale-zombie epic 2026-09-05): the start hook stamps
//     claimed_by=hostname:pid on the row it inserts (TaskStampSelfClaim) —
//     the row IS this run, so the runner owns it. Before this, auto rows
//     carried NO claim stamp; a killed runner left the row forever
//     "suspected" in the staleness evidence chain (zombieVerdict upgrades a
//     row to CONFIRMED only on a dead CLAIMED pid) = the 8h+ stale
//     in_progress rows. The stamp routes auto rows through the trusted
//     pid-death layer: dead runner → CONFIRMED, so the stale report can say
//     WHY the row is stranded instead of "cannot tell"; live runner → pid
//     veto (a slow live round is never falsely reported).
//
//     NOTE (2026-10-26): a CONFIRMED verdict no longer leads to DISPOSAL —
//     that mechanism is gone (devdoc/zombie-sweep-removed.md). The stamp's
//     value is the evidence it makes possible, not a cleanup it triggers.
const (
	// autoTaskRegisterEnv is the kill switch (default ON). The SPELLING has one
	// home — config.TaskAutoRegisterEnv (Rule 14) — because internal/tools names
	// it for its spawners and internal/testutil drops it from a suite's process
	// environment, and neither package can import this one (agent imports tools).
	// This alias is the SEMANTICS side: "what value means OFF" is
	// autoTaskRegisterEnabledWith below.
	autoTaskRegisterEnv = config.TaskAutoRegisterEnv

	// AutoTaskSummaryPrefix marks harness-registered rows, distinguishable
	// from `gopi task new` manual rows (and searchable via `gopi task search auto`).
	//
	// EXPORTED (epic task-ledger-auto-row-disposal, 2026-10-11) because it names
	// WHICH WRITER minted a row, and that question is asked outside this package:
	// internal/tools' extinction pass selects the harness's own rows by this
	// marker, and cmd/agent's fixtures must mint a row of the shape the harness
	// actually mints. That reader cannot import this package (agent imports
	// tools, so tools cannot import agent — the cycle would be immediate), so it
	// carries a MIRROR held to this value by
	// TestAutoRunLitterSummaryPrefixMatchesTheAgentRegistrationHook, which reads
	// the constant out of this file's SOURCE — a drift fails the build instead of
	// silently emptying the sweep's queue. A consumer in cmd/agent needs no
	// mirror at all: it references this constant directly.
	AutoTaskSummaryPrefix = "[auto] "

	// autoTaskSummaryMaxChars caps the captured briefing text: the summary
	// must stay a readable single-line ledger row (the full text lives in
	// the session JSONL / raw log anyway).
	autoTaskSummaryMaxChars = 200

	// autoTaskStatsFormat is the end-of-run stats digest recorded as the
	// task_events result detail (bug056: fields mirror the session_end
	// event (gopilog/types.go) — they used to be appended to the summary,
	// now they ride the audit trail, summary stays immutable).
	autoTaskStatsFormat = " — elapsed=%.1fs iter=%d tokens=%d"

	// autoTaskResumeHintFormat is appended to FAILED ledger events: the
	// failed→resume pointer (standard recovery path). %s is the ROW's task id
	// (the reservation is unconditional), which is a target `gopi session resume`
	// accepts directly — the session JSONL + raw log survive the failure, so
	// resume replays the exact context.
	autoTaskResumeHintFormat = " | FAILED — recoverable: `gopi session resume %s` replays session context; check the session raw log for the fatal request/response"

	// autoTaskNotDeliveredFormat is the operator line for a run whose own push
	// did not land: the row is refused the SUCCESS terminal it would otherwise
	// have worn, and lands `failed` instead. Four fields: the row id, the status
	// it landed, the claim it did NOT get, and the delivery leg's own sentence.
	//
	// WHY IT SAYS MORE THAN "LEFT OPEN" (bug 01a19639, measured 2026-11-01). The
	// wording used to be "… LEFT OPEN — the delivery did not land", which was
	// true about the CLAIM and false about the ROW: the arm also wrote
	// `in_progress`, and nothing downstream ever re-drove it (the lifecycle pass
	// and the `retry-delivery` verb were both retired 2026-10-30; the scan that
	// would have selected these rows carries its own "NO PRODUCTION CALLER"
	// note). A reader told "open" had no way to learn that the open state was
	// permanent. The words now name the status that was actually written and the
	// claim that was actually withheld, and the sentence still opens with
	// `LEFT OPEN` so an operator's existing grep and this run's own e2e keep
	// meaning what they meant — the claim IS still unwritten.
	autoTaskNotDeliveredFormat = "task ledger: %s LEFT OPEN — the delivery did not land (no `%s` claim is written), so the row lands `%s`; recover with `gopi session resume %s` or reissue it: %s"

	// autoTaskTerminalHintFormat is the FAILED hint for a failure the taxonomy
	// marks NOT recoverable (account-level: the provider account itself is
	// unusable). Offering "recoverable: gopi session resume" there was the other half
	// of the 23:00 GLM-1113 misreport — the resumer was told to replay into the
	// same dead account. The pointer stays (the JSONL/raw log still explain
	// WHAT failed) but the verdict flips; the actionable remedy rides the
	// error digest appended right after (ErrorRecord.Remedy). %s is again the
	// row's task id, which is the resume target.
	autoTaskTerminalHintFormat = " | FAILED — NOT recoverable by resume: `gopi session resume %s` would replay into the same failure — fix the cause first, then resume for context"

	// NOTE (unclassified failures): there is deliberately NO third hint. An
	// error nothing classified (CategoryUnknown) keeps
	// autoTaskResumeHintFormat — a pointer that carries no verdict — while
	// the digest beside it now reads `recoverable=unknown` instead of the
	// false `recoverable=false` this change removed (ErrorRecord.SummaryDigest,
	// Rule 15a). A third wording would be a ledger-contract change for every
	// failure, which is not what the classifier fix is for.
)

// AutoTaskRegisterOff is the env PAIR that turns auto-registration off, for
// process SPAWNERS outside this package (the merger's resolve-conflict child
// in cmd/agent): set an env entry of exactly this string. Exported as one
// "NAME=VALUE" string so a spawner cannot split the pair and pair the OFF
// name with the ON value — the 2026-09-10 defect (GOPI_AUTO_TASK_REGISTER=1
// behind an "Off" name) minted a duplicate ledger row per resolve child and
// made one merger look like several merger processes. Guarded by
// TestAutoTaskRegisterOff_IsOff (this package, which drives the ONLY reader
// autoTaskRegisterEnabledWith) and by the spawner-side gate
// TestClosingRoundEnvRegistersNoRowAndKeepsTheReportOnTheParentRow
// (cmd/agent/devworkspace_channel_report_round_test.go), which asserts the
// assembled round child env carries exactly one OFF pair, judges it with
// AutoTaskRegisterOffValue, and proves the env name agrees with
// tools.AutoRegisterOffEnv — so the two packages cannot drift.
//
// NOTE (2026-10-29): the comment used to name a different spawner-side gate,
// `TestMergeResolveEnv_DisablesAutoRegister` in `cmd/agent/merge_resolve_env_test.go`,
// as the guard on this pair. No such test and no such file exists in this
// repository at ANY revision (`git grep "func TestMergeResolveEnv_DisablesAutoRegister"`
// over `git rev-list --all` matches nothing; the path was never tracked). A
// gate named in a comment is a claim a reader cannot check, so the sentence now
// names the gate that really runs.
const AutoTaskRegisterOff = autoTaskRegisterEnv + "=" + autoTaskRegisterOffValue

// autoTaskRegisterOffValue is the OFF value autoTaskRegisterEnabled honors.
const autoTaskRegisterOffValue = "0"

// autoTaskRegisterEnabled reports whether auto-registration is on (default
// ON; only an explicit OFF value in autoTaskRegisterEnv disables it).
// Mirrors boolEnvTrue's truthiness convention, inverted for a default-on flag.
func autoTaskRegisterEnabled() bool {
	return autoTaskRegisterEnabledWith(os.Getenv)
}

// autoTaskRegisterEnabledWith is autoTaskRegisterEnabled for a caller-supplied
// env lookup — the value contract (which spellings mean OFF) checked without
// mutating the process environment (TestAutoTaskRegisterOff_IsOff).
func autoTaskRegisterEnabledWith(getenv func(string) string) bool {
	return !boolvocab.IsFalse(getenv(autoTaskRegisterEnv))
}

// AutoTaskRegisterOffValue reports whether v is an OFF spelling for the
// auto-register env — the value-level predicate, exported for spawner-side
// tests that inspect an ALREADY-ASSEMBLED child env (they read
// env[tools.AutoRegisterOffEnv] and must judge the value alone, without the
// name). It is the same vocabulary autoTaskRegisterEnabledWith honors, so the
// two can never drift.
func AutoTaskRegisterOffValue(v string) bool {
	return !autoTaskRegisterEnabledWith(func(k string) string {
		if k == autoTaskRegisterEnv {
			return v
		}
		return ""
	})
}

// AutoTaskSummaryLine is the ONE spelling of the row summary a harness-registered
// run writes: the [auto] marker plus the briefing folded to one bounded line.
//
// EXPORTED (epic auto-row-summary-decisive-line, row 01a18124) for the same
// reason AutoTaskSummaryPrefix is: the ledger's summary contract is read by a
// surface OUTSIDE this package — cmd/agent's delivery-retry briefing has to KNOW
// the budget its first line must fit into, and a test there has to assert the
// real assembled row (`[auto] ` + the fold) rather than re-implement the fold
// and drift from it. One implementation, two readers (Rule 14).
func AutoTaskSummaryLine(briefing string) string {
	return AutoTaskSummaryPrefix + normalizeTaskSummary(briefing)
}

// AutoTaskSummaryBudget is the rune budget a folded summary may occupy — the
// SAME value normalizeTaskSummary enforces, exported so a caller that BUILDS a
// briefing (cmd/agent's delivery retry round) can size its first line to fit
// instead of hoping it does. One definition (this constant); the alias exists so
// the number is never typed a second time (Rule 1/13/14).
const AutoTaskSummaryBudget = autoTaskSummaryMaxChars

// normalizeTaskSummary folds the briefing text into a single-line bounded
// ledger summary: whitespace collapsed, hard-capped at autoTaskSummaryMaxChars.
// bug056: this IS the immutable summary — nothing downstream may rewrite it
// (the writeback result digest moved to task_events).
func normalizeTaskSummary(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > autoTaskSummaryMaxChars {
		s = string(r[:autoTaskSummaryMaxChars]) + "…"
	}
	return s
}

// logAutoTaskf reports auto-registration outcomes: via the agent logger when
// one exists, straight to stderr otherwise (tests build Agent{} literals with
// a nil Logger).
func (a *Agent) logAutoTaskf(format string, args ...any) {
	if a.Logger != nil {
		a.Logger.Printf(format, args...)
		return
	}
	fmt.Fprintf(os.Stderr, "[gopi] "+format+"\n", args...)
}

// releaseOwnWorktreesAtCloseout runs the PRODUCER release pass at the end of a
// run that has NO wrapper tail — the bare-prompt / chat face.
//
// WHY IT HAS TO EXIST (bug row 01a18eb0, measured at b5e8c1d): the pass had two
// callers, `gopi task run`'s taskReclaimOwnDirectories and `gopi worker`'s
// workerRoundOwnWorktrees, and both are faces that own a WRAPPER row. A bare
// prompt mints its own row (maybeRegisterSessionTask) and then has no tail at
// all, so the linked worktree its executor created stayed registered and on
// disk while the row's deliverable was recorded correctly. The recording legs
// were never the defect; the release leg was simply absent from this face.
//
// WHAT IT MAY FREE is the pass's own decision, not this caller's: rows stamped
// with THIS pid's claim stamp (reclaimSelfRows) plus the launch-directory
// candidates the pass arms itself (reclaim_own_ground.go). A wrapper child's
// row carries the WRAPPER's stamp, so this adds nothing to a dispatched run.
//
// BEST-EFFORT BY CONTRACT, like every other close-out duty: a pass that cannot
// run is REPORTED (Rule 15) and never changes the row's outcome or the exit
// code. Bounded by the pass's own budget (tools.ReclaimOwnWorktreesAtCloseoutWithin).
func (a *Agent) releaseOwnWorktreesAtCloseout() {
	for _, line := range tools.ReclaimOwnWorktreesAtCloseoutWithin() {
		a.logAutoTaskf("own-worktree-release: %s", line)
	}
}

// taskLogWriter is the io.Writer the agent's own close-out diagnostics go to —
// the SAME destination logAutoTaskf uses (the agent logger when one exists,
// stderr otherwise), so a release failure lands in the log an operator already
// reads for this session rather than in a second stream (Rule 2: one writer).
func (a *Agent) taskLogWriter() io.Writer {
	if a != nil && a.Logger != nil {
		return a.Logger.Writer()
	}
	return os.Stderr
}

// maybeRegisterSessionTask is the START hook: called once per non-resume run
// right after the epic user message is logged (runEngine). It inserts the
// in_progress ledger row (task_id = auto t-<unix>; the session_id COLUMN
// records the session that executed the run) and retains the row's task_id on
// the Agent (autoTaskRowID) so the end hook closes exactly that row. Latched by
// a.autoTaskRegistered so the end hook knows there is a row to close; a failed
// insert latches false and the end hook becomes a no-op (never a
// half-registered session).
//
// RUN IDENTITY (epic run-identity-unify, 2026-09-20): the run's DIRECTORY is
// named by the row's own id (sessions/<task_id>/), which is what makes a row
// resolvable without hunting a session id. The session_id column is the ledger's
// own field — the anti-self-reference key and the record of which session
// submitted/executed the row — and is NOT how a run is addressed or found.
//
// task-single-row-exec (2026-09-10): an execution that already HAS a ledger
// row — a `task run` CHILD, identified by the wrapper's pre-spawn
// GOPI_TASK_WRAPPER_ID env — must NOT register a second auto row. The human
// wrapper row IS the run (it carries session_id via the bug054 stamp,
// run provenance via TaskStampRun, claim via TaskStart, and the terminal
// writeback via the wrapper side); an auto mirror row would be a pure
// redundant duplicate (双份维护: closeout×2 / display×2 / sync×2 / stale×2).
// So the wrapper-child gate skips registration entirely and latches
// autoTaskRowID to the WRAPPER row's task_id — the heartbeat then touches
// the single human row, keeping it alive for the whole run. Auto rows are
// reserved for executions with NO ledger row: bare prompt/chat/web sessions
// (they get the audit carrier a row provides).
func (a *Agent) maybeRegisterSessionTask(briefing string) {
	// THE ARM THAT ATTACHES TO A PRE-EXISTING ROW IS TAKEN FIRST, WHILE THE
	// START HOOK'S OWN LATCH IS STILL INTACT (truncated-run end-hook epic,
	// 2026-09-25). The latch is cleared on the arm that mints a NEW row only.
	//
	// Measured shape of the defect (probe + stack, then removed): the hook
	// cleared a.autoTaskRegistered on ENTRY, so a call arriving with the row
	// ALREADY latched (autoTaskRowID set) returned through the adopted-row
	// gate below having DISARMED it. The END hook's own early return —
	// `if !a.autoTaskRegistered || a.SessionLog == nil` — then fired and the
	// row was NEVER closed: a run the wallclock cap cut short sat at
	// `in_progress` forever (the two internal/agent reds:
	// TestAgent_{WallclockCap,StuckLoop}_TruncatedRunIsNotCompleted). The
	// status rule was already correct; the latch is what was lost.
	//
	// Reachability, stated honestly: no production caller reaches this hook
	// twice — `runEngine` is its only production call site, and the chat faces
	// (`Agent.Chat`/`ChatOps`) never reach it at all. The second call that
	// reproduced the red comes from the TESTS' own start-hook call, which sets
	// up the row the assertions read before driving `a.Run`. The ordering is
	// this way regardless: for a WRAPPER CHILD the gate is a no-op on the latch
	// (autoTaskRegistered is false by design — the wrapper's own writeback owns
	// that row), so taking the attaching arm first costs the minting arm
	// nothing and makes the hook correct-by-construction rather than correct by
	// the caller's discipline.
	if a.latchWrapperRow() {
		return
	}
	// Clear the latch only on the arm that is about to mint a NEW row: nothing
	// is registered above this line, so clearing earlier disarms the end hook
	// for a row this call does not create.
	a.autoTaskRegistered = false
	if !autoTaskRegisterEnabled() || a.SessionLog == nil {
		return
	}
	// Wrapper-child gate: this process executes an EXISTING human wrapper
	// row (`gopi task run` exported GOPI_TASK_WRAPPER_ID pre-spawn — Rule 2:
	// one writer cli_tasks.go runTaskRunOnce, one reader here). The human
	// row is the single source of execution truth; do not mint an auto
	// mirror. autoTaskRowID = wrapper id so the liveness heartbeat (loop.go
	// setTaskHeartbeatSession → taskheartbeat.go touch, WHERE task_id = ?
	// AND status='in_progress') lands on the human row during the run.
	if a.latchWrapperRow() {
		return
	}
	if !autoTaskRegisterEnabled() || a.SessionLog == nil {
		return
	}
	summary := AutoTaskSummaryLine(briefing)
	if strings.TrimSpace(summary) == strings.TrimSpace(AutoTaskSummaryPrefix) {
		return // empty briefing — nothing meaningful to register
	}
	sessionID := a.SessionLog.SessionID()
	// The model is captured at task-creation time (root-cause fix for the
	// task-show-model epic): the ledger row itself carries which model ran
	// the task — list/show render it from the DB, no JSONL lookup needed.
	// taskID is LEFT EMPTY so the ledger mints t-<unix> — the row's uniform
	// ledger key, and (since run-identity-unify) the name of the run DIRECTORY
	// this run writes its log into (sessions/<task_id>/). One id, one role: the
	// run's address. The session id goes in the session_id COLUMN beside it and
	// is a different thing — the ledger's record of which session
	// submitted/executed the row (the anti-self-reference key), never the run's
	// address.
	//
	// task-create-unified (2026-09-05): the register goes through the
	// EXTENDED create face (TaskNewOID + TaskNewOptions) so the row is born
	// with the full submit-parity schema in ONE INSERT — source is stamped
	// explicitly ('auto'), and payload/tags/deliverable/reason ride the
	// same row when a caller supplies them. Payload stays EMPTY for bare
	// auto rows on purpose: the briefing lives in the session JSONL and the
	// show renderer pulls it from there (committed contract — a payload
	// mirror would double-store every prompt for no display gain).
	taskID, err := tools.TaskNewOID(tools.TaskNewOptions{
		Summary:   summary,
		SessionID: sessionID,
		Model:     a.Model,
		Source:    tasksource.Server,
		// Deliverable closure at registration (epic raw-prompt-worktree-release,
		// 2026-09-14): the row a bare-prompt/chat run registers must be born
		// with its repo, exactly as a `task run` wrapper row is born with the
		// submit-time repo. An EMPTY blob is not "no information" — it is a
		// switch that turns the whole worktree lifecycle off for this run:
		// the executor's creation-time backfill refuses a repo-less blob, so
		// the Rule-6 worktree the run creates is recorded nowhere; the
		// reclaim arms select rows by deliverable, so nothing ever releases
		// the directory; and the merger reads the branch as worktree-protected
		// ACTIVE BOND WORK and never merges it. Measured e2e (cmd/agent/
		// rawprompt_worktree_release_e2e_test.go): the run left its worktree
		// registered and its auto row's deliverable blank.
		//
		// The value comes from tools (one default: the process's own git work
		// tree, the same resolution the submit engine uses), never from a
		// second spelling here. "" when this process stands in no work tree —
		// the row then honestly carries no deliverable, and nothing downstream
		// may guess one.
		// ... AND, since epic raw-prompt-clone-channel (2026-10-23), the
		// repo is the one the CHANNEL will actually work in: when the clone
		// channel is ON (settings `dev_repo`), a bare-prompt row names that
		// declared source repository — the same value `task submit`/`worker`
		// resolve — so the row and the ground agree. When the channel is OFF,
		// a.autoTaskDeliverableJSON (internal/agent/cloneground.go) returns
		// tools.TaskAutoDeliverableJSON() UNCHANGED — that arm is the
		// compatibility contract, byte for byte.
		Deliverable: a.autoTaskDeliverableJSON(),
		// Run provenance (task-show-run-cmd epic): this row IS the run, so
		// the launch command is THIS process's own argv — recorded verbatim
		// for exactly the reason a `task run` wrapper records its own: an
		// operator reading `task show` on the row must be able to paste the
		// command and reproduce the run. Before this, every bare auto row
		// showed NO run_cmd at all (the `[auto] worker`-class row in
		// devdoc/rootcause-useless-auto-task-worker.md), leaving the row's
		// "how was this launched" question unanswerable. The argv is
		// rendered by the shared runcmd renderer (basename + shell quoting),
		// never hand-written: a pseudo-command here would be worse than
		// nothing (nick's report on the in_progress worker rows).
		RunMethod: runcmd.MethodAuto,
		RunCmd:    runcmd.Command(os.Args),
	})
	if err != nil {
		a.logAutoTaskf("task ledger: register failed (continuing): %v", err)
		return
	}
	a.autoTaskRowID = taskID
	a.autoTaskRegistered = true
	// Ownership stamp (stale-zombie epic, 2026-09-05): the row is inserted
	// directly in_progress with NO claim — absent this stamp a killed
	// runner leaves the row forever "suspected" in the staleness evidence
	// chain (zombieVerdict upgrades to CONFIRMED only on a dead CLAIMED
	// pid). Stamp the runner's own hostname:pid at birth so the trusted
	// pid-death layer applies: dead runner → CONFIRMED (the report can name
	// the dead holder); live runner → pid veto. The row IS this run, so the
	// stamp is birth provenance, not a lock. It triggers no cleanup:
	// disposal was removed 2026-10-26 (devdoc/zombie-sweep-removed.md).
	// Best-effort — a stamp
	// failure must never block registration (log and continue).
	if err := tools.TaskStampSelfClaim(taskID); err != nil {
		a.logAutoTaskf("task ledger: self-claim %s failed (continuing): %v", taskID, err)
	}
	// THE CLOSEOUT OBLIGATION ARMS THE BROKEN-PIPE GUARD (bug row
	// 01a1950b, measured 2026-11-01). The row just minted is a PROMISE that
	// this process will reach its end hook and write a terminal status. The
	// promise is only keepable while the process survives a launcher walking
	// away, and one death mode makes that impossible: a write to fd 1/2 after
	// the reader closed the pipe raises SIGPIPE, whose DEFAULT disposition
	// kills the process outright — no session_end, no end hook, and a row left
	// `in_progress` with `ended_at` empty forever.
	//
	// MEASURED, on the real binary, from this tree:
	//
	//	$ GOPI_MEMORY_DB=<private copy> ./bin/gopi chats 2>&1 | head -12
	//	… 12 banner lines …
	//	pipefail exit=141                      ← 128+SIGPIPE(13)
	//	$ ./bin/gopi task list --status in_progress
	//	#01a1954e-…  [in_progress]  [auto] chats  …  [died 1.93s since 12:12:07]
	//
	// and the same row 90 s later still read `status: in_progress`,
	// `ended_at: (not ended)`, `live: gone` — nothing collects it, BY DESIGN:
	// automatic disposal was removed in the operator's 2026-10-26 ruling
	// (`devdoc/zombie-sweep-removed.md`), so a run that dies without its
	// close-out is a ledger row nobody will ever close. The producer is
	// therefore the only place this can be repaired.
	//
	// WHY IT WAS MISSING HERE. main.go already ignores SIGPIPE for a
	// `task run` CHILD (`GOPI_TASK_WRAPPER_ID`), and the comment there says
	// plainly that "top-level `gopi | head` semantics are untouched". That was
	// correct while a top-level run owned nothing; since run-identity-unify it
	// MINTS ITS OWN ROW (this function), so it carries exactly the closeout the
	// wrapper child's guard was written to protect. The guard keyed on the
	// wrong fact: not "am I someone's child" but "do I owe a closeout".
	//
	// The window between the successful INSERT above and this call is a few
	// instructions with no output write, which is why arming here (rather than
	// before the insert) cannot itself lose the row.
	armBrokenPipeSurvivalForRowOwner()
	a.logAutoTaskf("task ledger: registered %s (in_progress, auto, session %s)", taskID, sessionID)
}

// finishSessionTask is the END hook: called right after LogSessionEnd in the
// runEngine defer (and by tests / the panic-recover path as a thin wrapper).
// Flips the start hook's row to completed/failed (exit code) and appends the
// run stats (elapsed/iterations/tokens — the session_end numbers already
// computed there). FAILED rows carry a resume pointer in the summary —
// failed→resume is the standard recovery path: the run directory
// (sessions/<task_id>/, named by the row's own id) holds the session JSONL and
// raw log, so `gopi session resume <task-id>` replays the exact context.
// Panic-recover rounds pass a collected *ErrorRecord via
// finishSessionTaskRecorded (SummaryDigest → notification pipe shows WHY).
// No-op when the start hook did not register. Best-effort.
//
// The end reason is EMPTY here on purpose: this entry point is the one the
// panic-recover round and the tests use, and a panic is not a truncation —
// its exit code is already non-zero.
func (a *Agent) finishSessionTask(exitCode, iterations, tokens int, elapsedSec float64) {
	var failure *ErrorRecord
	if exitCode != 0 && panicCollectorPeek() != nil {
		failure = a.collectErrorRecord(nil, iterations, tokens, elapsedSec)
	}
	a.finishSessionTaskRecorded(exitCode, iterations, tokens, elapsedSec, failure, runend.Finished)
}

// finishSessionTaskRecorded is the END hook core: flips the start hook's row
// to completed/failed (exit code) and records the run stats as the
// task_events result digest (elapsed/iterations/tokens — the session_end
// numbers already computed there). bug056: the stats NO LONGER append to
// the summary — the [auto] summary stays the briefing text verbatim; the
// digest lives in the append-only audit trail (show renders it as
// `result:`). FAILED rows carry a resume pointer in the digest —
// failed→resume is the standard recovery path: the run directory
// (sessions/<task_id>/, named by the row's own id) holds the session JSONL and
// raw log, so `gopi session resume <task-id>` replays the exact context.
// Panic-recover rounds pass a collected *ErrorRecord via
// finishSessionTaskRecorded (SummaryDigest → notification pipe shows WHY).
// No-op when the start hook did not register. Best-effort.
//
// truncation is the run's END REASON (runend.Reason): the loop's engine-forced
// exits (wallclock cap / stuck net / iteration budget) reach the ledger HERE.
// The status rule is `exitCode != 0 || endReason.Truncated()` — an engine
// boundary is NOT a completion, so a truncated run must never read `completed`
// (the defect this parameter exists to close: the decision used to be the exit
// code alone, and every engine-forced stop exits ZERO).
//
// ONE END HOOK, ONE FUNCTION (restored 2026-09-24, the shape this file had
// before 572100d5). That commit split the close-out body into a SECOND function
// named `finishSessionTaskRecordedWithReason` and left this one a one-line
// forwarder with no production caller — so the end hook ran under a name only
// the loop and the tests knew, while `finishSessionTaskRecorded` (every
// doc-reference's, every comment's and the structural guard's anchor) no longer
// carried the close-out. The end reason is a PARAMETER of the one hook, never a
// reason to fork it: whatever this function defers — the cwd release below is
// the load-bearing one — must cover the exit paths of the hook that actually
// runs, and there is now exactly one such function to name.
func (a *Agent) finishSessionTaskRecorded(exitCode, iterations, tokens int, elapsedSec float64, failure *ErrorRecord, truncation runend.Reason) {
	// WHERE THE CHILD'S RUN-ROW CLOSE-OUT USED TO SIT, and why it is gone
	// (task 01a16f7c). It reconciled the WRAPPER's row on its own authority: it
	// asked "is the pid my wrapper published still alive?" — and the answer was
	// known before the question (the wrapper is the process that WAITs for us,
	// so a wrapper that is gone is one that no longer exists to be asked, and a
	// live one is refused by the seam's own gate). What it structurally could
	// not do was WRITE: the seam refused every write while the claim pid was
	// alive, and on the healthy path the row was already terminal. Every
	// healthy run paid a /proc read and a ledger read for a decision that was a
	// no-op by construction.
	//
	// WHICH ROW IT CLOSED IS THE WRAPPER'S JOB. A `task run` / `worker` wrapper
	// that survives reaches its own post-Wait writebackOutcome; a wrapper
	// KILLED in that window is what the ledger's stale/zombie machinery already
	// dispositions (claim holder dead ⇒ row reclaimable), and the worker-side
	// run close-out reap still recognises the processes a dead run leaked — the
	// lineage env it reads is unchanged.
	//
	// THE ONE HALF THAT SURVIVES, because it never was a liveness verdict: the
	// TRUNCATION TESTIMONY. A wrapper CHILD has no auto row, so the row the
	// wrapper will write is the only place an engine-forced stop can be
	// recorded — and every engine-forced stop exits ZERO, which is exactly what
	// the wrapper's post-Wait writeback reads back (tools.RunEndReasonFor) to
	// refuse `completed`. That note closes no row and asks about no pid; it is
	// the child's statement of WHY the run stopped, kept so a cap-truncated
	// `task run` cannot close `completed` one door after the fix that made the
	// exit code honest.
	a.recordRunTruncationOnWrapperRow(truncation)
	// CWD RELEASE ON EVERY RETURN (worktree-residue epic, 2026-09-15): the
	// release duty is a property of the END HOOK, not of the happy path. The
	// hook has early returns below (no auto row registered / no session log,
	// and a failed ledger close) that used to skip the release entirely — a
	// child that reached the end hook yet bailed there stayed IN the worktree
	// it had been running in, vetoing that worktree's removal for the rest of
	// its life. Deferring the release past the gate gives every exit path the
	// same coverage; the action is idempotent, so the common path costs one
	// no-op. ONE implementation (PROJECT.md Rules 13/14) and best-effort by
	// contract — a failed chdir is reported (Rule 15) and never changes the
	// row's outcome or the exit code.
	defer worktreerelease.ReleaseReporting(a.taskLogWriter(), agentProgName)
	// THE WORKTREE RELEASE LEG (bug row 01a18eb0, measured at b5e8c1d). The
	// release pass lives in cmd/agent's close-out tails — taskReclaimOwnDirectories
	// for the `task run` wrapper and workerRoundOwnWorktrees for a worker round —
	// and BOTH are faces with a WRAPPER ROW. The bare-prompt / chat face has none
	// (maybeRegisterSessionTask mints its own row), so it never reached either
	// tail: the executor created its Rule-6 linked worktree, the creation seam
	// recorded it on the row, and then nobody asked the release pass at all.
	//
	// THE MEASUREMENT (cmd/agent/rawprompt_worktree_release_e2e_test.go, red at
	// this revision and green with this defer): after the run exits,
	// `git worktree list` still names the directory, the directory is still on
	// disk, and the BRANCH and the row's deliverable were already correct — i.e.
	// the recording legs worked and only the RELEASE leg was missing. That is why
	// this file's own end hook is the right place: the hook is the one thing every
	// face runs, and the pass it now calls is the ONE entry point (Rules 13/14 —
	// tools.ReclaimOwnWorktreesAtCloseout, never a second spelling of the two
	// halves, and never tools.ReclaimOwnFinishedWorktrees alone: the second half
	// runs under the same close-out mark).
	//
	// ORDER: registered AFTER the cwd-release defer above and BEFORE the
	// clone-delivery defer below, so the LIFO unwind runs deliver → release the
	// ground → hand the cwd back. Removing a directory this process stands in is
	// only sound in that order.
	//
	// SCOPE, stated so a later reader does not mistake it for a sweep: the pass is
	// the PRODUCER half — it selects rows stamped with THIS pid's claim
	// (reclaimSelfRows) and the directories its own close-out armed. A wrapper
	// child's row is claimed by the WRAPPER's pid, so this adds nothing to a
	// dispatched run's ground, and a stranger's checkout is not in the set.
	defer a.releaseOwnWorktreesAtCloseout()
	// THE CLONE GROUND'S DELIVERY LEG (epic raw-prompt-clone-channel-delivery-leg,
	// reordered by epic raw-prompt-clone-channel-delivery-order). The leg's
	// PRIMARY call site is in the body, immediately before the status write (see
	// `DELIVER FIRST, CLAIM SECOND` below) — that order is what stops a landed
	// delivery from being refused by the delivery proof, and what keeps a blocked
	// delivery from being claimed `completed` at all. This DEFER is the fallback
	// for the early returns below (no registered row / no session log / a failed
	// ledger close): a duty that only covers the happy path is a duty that
	// vanishes exactly when something went wrong, which is the reason
	// worktreerelease is deferred too. ONE attempt per run is guaranteed by
	// cloneDeliveryDone, not by the number of call sites.
	defer func() { a.finishCloneDelivery(a.lastClosedRowStatus) }()
	if !a.autoTaskRegistered || a.SessionLog == nil {
		return
	}
	taskID := a.autoTaskRowID
	a.autoTaskRegistered = false // one row per session — never updated twice
	status := tools.TaskStatusCompleted
	if exitCode != 0 {
		status = tools.TaskStatusFailed
	}
	// Engine-truncation rule (runend): a run the ENGINE cut short — wallclock
	// cap, stuck net, iteration budget — was still working when it stopped, so
	// it must NOT close as `completed`. The ledger's status set is unchanged
	// (sync/verify/the claim gate share it): the run is recorded `failed`,
	// which is the existing vocabulary for "this row is not a delivered
	// result", and the WHY rides the reason column + the task_events digest
	// below. Note this shares the failed arm's resume pointer on purpose — a
	// truncated run IS the canonical resume candidate.
	if truncation.Truncated() {
		status = tools.TaskStatusFailed
	}
	// Honest-exit guard (reframe verdict=IMPOSSIBLE): the reframe dispatch
	// already marked this row failed with the model's reason — the end hook
	// must not re-close it as completed and erase the verdict from the ledger
	// (Rule 15: the operator-facing decision must survive the close).
	if a.reframeHonestExit && status == tools.TaskStatusCompleted {
		status = tools.TaskStatusFailed
	}
	// bug056: the digest is the OLD summary-tail body, now a task_events
	// detail — the summary column itself is never touched again.
	digest := fmt.Sprintf(autoTaskStatsFormat, elapsedSec, iterations, tokens)
	// Truncation marker: the digest is what `gopi task show` renders as its
	// `result:` line AND the trail every other reader walks, so the WHY of an
	// engine-forced stop belongs here (runend.Detail — one spelling, shared
	// with the renderer and the wrapper's writeback).
	if d := truncation.Detail(); d != "" {
		digest += " | " + d
	}
	if exitCode != 0 && failure == nil {
		failure = a.collectErrorRecord(nil, iterations, tokens, elapsedSec)
	}
	if status == tools.TaskStatusFailed {
		// The resume verdict follows the taxonomy: an ACCOUNT-level failure
		// (CategoryAccount — dead credential / out of balance) must not offer
		// "recoverable: gopi session resume", which would replay into the same dead
		// account. Every other category keeps the original wording verbatim.
		//
		// An UNCLASSIFIED failure also keeps the original wording, and that is
		// deliberate rather than left over: this change removed the false
		// CLAIM on the other side (the digest used to print
		// `recoverable=false` for an error nothing had classified, which
		// contradicted this very pointer — the measured `category=unknown
		// recoverable=false` rows). The pointer itself is generic advice that
		// carries no verdict, and the digest beside it now says
		// `recoverable=unknown`, so the reader sees ONE honest statement
		// instead of two that disagree. Rewording the pointer is a ledger
		// contract change for every failure — not this row's charter.
		hint := autoTaskResumeHintFormat
		if failure != nil && failure.Category == string(deepseek.CategoryAccount) {
			hint = autoTaskTerminalHintFormat
		}
		digest += fmt.Sprintf(hint, taskID)
	}
	if failure != nil {
		digest += " — " + failure.SummaryDigest()
	}
	// DELIVER FIRST, CLAIM SECOND — THE ORDER IS THE FIX (epic
	// raw-prompt-clone-channel-delivery-order, measured 2026-10-24).
	//
	// The delivery proof at the write choke point (tools.deliveryProofRefusal)
	// measures the tip the run produced against refs/remotes/origin/<branch>,
	// and that ref moves only when the PUSH lands. So a leg running after this
	// write measured a destination the push had not moved yet and refused the
	// claim — on the shipped binary both sentences appeared on one stderr
	// (`delivery proof: REFUSED …` above `delivery delivered — pushed master to
	// origin`), leaving a run whose work HAD landed parked at in_progress
	// forever. Running the leg here, before the write, gives each duty a true
	// state to measure: delivered → the tracking ref already holds the tip, so
	// the proof allows the claim; blocked/broken → the ref did not move, and the
	// claim is never written at all (below), which is the same non-lying
	// outcome without a proof arm having to catch it.
	//
	// The compatibility arm is untouched: with no clone ground the leg returns
	// before reading config, git or the log, so the worktree channel and every
	// non-clone face take exactly the path they took before (the defer below
	// stays as the fallback for this hook's early returns).
	delivery := a.finishCloneDelivery("")
	// An EMPTY ground is a CONCLUSION, not a failed delivery (bug 01a182a8): the
	// attempt came back saying the workspace carries no commit past the
	// integration branch, which is exactly what a run asked to produce NO change
	// looks like. It is neither delivered nor undelivered, so the status the hook
	// decided stands — but the row carries the leg's own sentence instead of the
	// truncation wording, which is why the Reason is still written below.
	undelivered := delivery.Attempted && !delivery.Delivered && !delivery.NothingToDeliver && !delivery.AlreadyDelivered && !delivery.InheritedDelivery
	// A SUCCESS TERMINAL IS A DELIVERY CLAIM, and a run whose own push did not
	// land must not wear it. It becomes `failed` — the ledger's existing word
	// for "this row is not a delivered result" — with the leg's own sentence in
	// the reason column.
	//
	// WHY NOT "LEAVE THE ROW OPEN" (bug 01a19639, measured 2026-11-01). This arm
	// used to write `in_progress` and return, on the premise that something
	// downstream would re-drive the delivery. MEASURED: nothing does. The two
	// carriers named in the contract are both gone — the lifecycle pass was
	// retired 2026-10-30 (cmd/agent/task_lifecycle_tick.go records the six legs
	// it composed and why each lost its caller) and the `retry-delivery` verb
	// with it (taskverbs.RemovedVerbs); `deliveryRetryOwedIDs`, the scan that
	// would select these rows, carries its own note: "NO PRODUCTION CALLER as of
	// 2026-10-30 … kept for the tests". So the row was parked at `in_progress`
	// FOREVER, and the framework's own verdict said so on every read:
	//
	//	$ gopi task status 01a19613-9019-71bc-80bf-0f450ec97ce5
	//	status:      in_progress
	//	verdict:     dead
	//	evidence:    pid 1453624 is gone — the run recorded a clean end (exit 0,
	//	             reason complete); the WORK may have finished and the row was
	//	             never closed
	//	resumable:   no
	//
	// A row that reads `dead` + `resumable: no` + `in_progress` is the one
	// combination from which NOBODY can act: not the dispatcher (it looks
	// running), not the operator (`--resume` refuses a finished-work status),
	// not the reissue census (it lists `failed` rows). It took a hand-typed
	// `gopi task update <id> --status failed` 65 minutes later to clear it.
	//
	// `failed` is the status the rest of the system already agrees with: it is
	// NOT a delivery claim, so nothing in the writeback's proof chain disputes
	// it (taskStatusClaimsDelivery excludes it); it IS resumable
	// (taskRunResumeStatusAdmits), so the operator keeps the recovery route the
	// open row was meant to preserve; and it puts the row in the reissue census
	// the operator already reads. The non-delivery reason still rides the reason
	// column, so the WHY the open row carried is not lost.
	if undelivered && status == tools.TaskStatusCompleted {
		status = tools.TaskStatusFailed
		digest += fmt.Sprintf(autoTaskResumeHintFormat, taskID)
		a.logAutoTaskf(autoTaskNotDeliveredFormat, taskID, tools.TaskStatusCompleted, status, taskID, delivery.Reason)
	}
	// A RESUMED RUN THAT PRODUCED NO COMMIT DID NOT FINISH THE WORK (epic
	// resume-zero-commit; row 01a19d78's own symptom). Every arm above reads
	// `count == 0` as a CONCLUSION about the row — "nothing to deliver", "the
	// content this ground was handed is already on the base" — and that reading
	// is right for an ORDINARY run, which was asked to produce a change and whose
	// production of none is a fact to report. It is the WRONG reading for a
	// RESUME: that run was dispatched to REPAIR a failure, so zero commits means
	// the failure was never touched at all, and writing `completed` says the
	// opposite of what the measurement said (Rule 15).
	//
	// MEASURED, this row's premise: a resume of failed row 01a18377 ran 12
	// minutes, pushed nothing, and closed `completed` with the channel's own
	// sentence on it — the defect it existed to fix still live. The state read
	// `completed`; the work read `untouched`. Both were true at once, which is
	// why the status had to move.
	//
	// NARROW ON PURPOSE, and the narrowness is the whole design: the SUBJECT is
	// tools.TaskRunResumeFrom() — the door's declaration, whose absence means "an
	// ordinary first run", so rows 01a18244/01a1907d's `completed` arms (a run
	// asked to produce nothing; a TOO_BIG reframe; a counter-proof) keep their
	// behaviour byte for byte. The FACT is the delivery leg's own classification,
	// already measured above — no git call, no second opinion (Rules 8/13/14).
	// `AlreadyDelivered` is excluded because that arm means this run's OWN push
	// landed, i.e. it did produce a commit.
	resumeNoCommit, resumeReason := tools.ResumeNoCommitVerdict(
		tools.TaskRunResumeFrom(), taskID, delivery.Reason,
		delivery.NothingToDeliver || delivery.InheritedDelivery)
	if resumeNoCommit && status == tools.TaskStatusCompleted {
		status = tools.TaskStatusFailed
		digest += fmt.Sprintf(autoTaskResumeHintFormat, taskID)
		a.logAutoTaskf(autoTaskNotDeliveredFormat, taskID, tools.TaskStatusCompleted, status, taskID, resumeReason)
	}
	// The reason the delivery arms below write: the channel's own sentence
	// unless this run is the resumed-no-commit case, which is owed the sentence
	// that says WHY `completed` was refused. One variable, so the two arms stay
	// one write shape (Rule 14).
	deliveryRowReason := delivery.Reason
	if resumeNoCommit {
		deliveryRowReason = resumeReason
	}
	// The witness below is reached on this arm now, and that is half the fix: a
	// `return` before `recordTaskRowClosed` left the session JSONL with NO
	// terminal-write record at all (measured: 0 `task_row_closed` events in
	// sessions/659b645b-…/session.jsonl), so "the hook decided to leave it open"
	// and "the hook never ran" were indistinguishable from the transcript —
	// exactly the ambiguity the witness exists to remove (truncated_run_test.go
	// / recordTaskRowClosed's own docstring).
	// The reason column (the ledger's own WHY slot, rendered by `task show` as
	// `reason:`) carries the truncation for a truncated row; every other case
	// passes "" so the status default applies, exactly as before.
	out, err := tools.TaskUpdateReason(taskID, status, truncation.ReasonText(), digest)
	// The session JSONL's TERMINAL-WRITE WITNESS: recorded on success AND on
	// failure (Rule 15 — a failed write is a fact, not a silence). It is the
	// run's own proof that the end hook reached the ledger, which a silent
	// early return cannot forge: before it, the truncated-run defect showed a
	// row parked at `in_progress` while the session log said nothing at all
	// about whether the hook had run — the status rule was already correct.
	a.recordTaskRowClosed(taskID, status, truncation, err)
	// The status the delivery leg's reason write must carry (see the field's
	// note in loop.go): recorded even when the close FAILED, because the leg's
	// own report then says the reason could not land — which is the truth.
	a.lastClosedRowStatus = status
	if err != nil {
		a.logAutoTaskf("task ledger: close failed (continuing): %v", err)
		return
	}
	a.logAutoTaskf("task ledger: %s %s (auto)", status, taskID)
	_ = out
	// The row is terminal (or a failure), and the delivery leg still owes it a
	// sentence: `failed` is not a delivery CLAIM, so the proof never refuses it,
	// but an operator reading the row must still be told that the push did not
	// land (this is the same reason write the deferred leg used to perform —
	// Rule 15: a non-landing delivery is never silent on the row). It is a
	// same-status write, so nothing transitions and no proof arm re-runs.
	if undelivered {
		if _, rerr := tools.TaskUpdateReason(taskID, status, delivery.Reason, ""); rerr != nil {
			a.logAutoTaskf("task ledger: the non-delivery reason could not reach %s (continuing): %v", taskID, rerr)
		}
	}
	// THE THIRD OUTCOME GETS ITS SENTENCE TOO (bug 01a182a8). "Nothing to
	// deliver" is neither a delivery nor a non-delivery, so it must not be left
	// SILENT on the row: the operator reading `status: completed` is owed the
	// reason it closed — the empty ground, named — and it is written through the
	// same one door, as a same-status write (no transition, no proof re-run).
	if delivery.NothingToDeliver {
		if _, rerr := tools.TaskUpdateReason(taskID, status, deliveryRowReason, ""); rerr != nil {
			a.logAutoTaskf("task ledger: the nothing-to-deliver reason could not reach %s (continuing): %v", taskID, rerr)
		} else {
			a.logAutoTaskf("task ledger: %s closed `%s` with nothing to deliver (no delivery was owed): %s",
				taskID, status, deliveryRowReason)
		}
	}
	// THE FIFTH OUTCOME'S SENTENCE (row 01a182df): the delivery ALREADY LANDED,
	// so the run is a completion and the row is owed the reason it closed — the
	// opposite fact from the arm above, through the same one door (same-status
	// write: no transition, no proof re-run). Left silent, an operator reading
	// `status: completed` cannot tell a delivered ground from an empty one, and
	// that ambiguity is the defect this row was filed against.
	if delivery.AlreadyDelivered {
		if _, rerr := tools.TaskUpdateReason(taskID, status, delivery.Reason, ""); rerr != nil {
			a.logAutoTaskf("task ledger: the already-delivered reason could not reach %s (continuing): %v", taskID, rerr)
		} else {
			a.logAutoTaskf("task ledger: %s closed `%s` with its delivery already landed: %s",
				taskID, status, delivery.Reason)
		}
	}
	// THE SIXTH OUTCOME'S SENTENCE (epic: inherited delivery): the content this
	// ground was handed is ALREADY on the integration branch, so the run is a
	// completion and the row is owed the reason it closed — through the same one
	// door (a same-status write: no transition, no proof re-run). Left silent, a
	// reader of `status: completed` could not tell this from an EMPTY ground, and
	// that ambiguity is the defect the row was filed against.
	if delivery.InheritedDelivery {
		if _, rerr := tools.TaskUpdateReason(taskID, status, deliveryRowReason, ""); rerr != nil {
			a.logAutoTaskf("task ledger: the inherited-delivery reason could not reach %s (continuing): %v", taskID, rerr)
		} else {
			a.logAutoTaskf("task ledger: %s closed `%s` with the content it was handed already on the integration branch: %s",
				taskID, status, deliveryRowReason)
		}
	}
	// M4.19 ② terminal must-succeed push: the row's terminal state
	// (completed/failed) MUST reach every sync_peers peer — the start point
	// already guaranteed a single runner, so this push has no interference
	// and "no reason to fail" (nick). Retries with backoff; exhaustion
	// alarms through the GOPI_TASK_NOTIFY pipe (terminal-push-failed
	// synthetic event — abnormal, never silent). GOPI_SKIP_TERMINAL_PUSH=1
	// opts out (sims / callers managing sync themselves). config.TaskSyncEnabled()
	// is the coarser opt-out: with automatic task-row exchange OFF (the
	// switch's default) the verdict stays on the ledger that ran the row,
	// which is the same nothing-to-push-to outcome the no-peer arm reports.
	if !config.TaskSyncEnabled() {
		a.logAutoTaskf("terminal push: skipped (task sync OFF — %s)", config.TaskSyncSettingsKey)
	} else if a.terminalPushSkipped() {
		a.logAutoTaskf("terminal push: skipped (GOPI_SKIP_TERMINAL_PUSH=1)")
	} else if err := tools.PushTerminalTaskSync(taskID); err != nil {
		a.logAutoTaskf("terminal push: FAILED (alarm dispatched): %v", err)
		tools.NotifyTerminalPushFailed(taskID, err)
	} else {
		a.logAutoTaskf("terminal push: ok")
	}
	// THE END HOOK NO LONGER RUNS A CLOSE-OUT CHAIN (2026-10-30, task 01a18799,
	// slice 3/4). It used to call tools.TaskFinishCleanupSessionID here — the
	// worktree+branch hand-off chain (reclaim a dead holder's ground → align →
	// gate → push → remove the directory → delete the ref) — behind a
	// one-report-per-run deferral protocol (internal/closeouthandoff) that named
	// whoever else was reporting it. That whole mechanism is RETIRED: a
	// dispatched run's delivery is its clone ground's own END-hook publish
	// (Rule 6c), where there is no branch to hand over, no peer to ask and no
	// directory to reclaim. What the hook still owns is the CWD release below,
	// which was always the producer half of that chain's own guard.
	// CWD RELEASE — performed by the DEFERRED call at the top of this
	// function, so it covers every return path (worktree-residue epic). It is
	// placed there rather than here because the early returns below would
	// otherwise skip it; the action is idempotent, so the double cover costs
	// one no-op. The seam it was written for is right here: immediately after
	// the chain that may have REMOVED a worktree.
	//
	// Agency-proposal pass (epic agency-proposals, form A): the LAST thing the
	// end hook does is give the level-3 miner a chance to read the ledger and
	// propose an improvement. Off unless GOPI_AGENCY is truthy; a no-op
	// otherwise (see agencyhook.go). It runs here because this is the
	// process's only "a session just ended" edge that costs NO main-loop
	// attention — the prompt is already flushed and no token is spent.
	a.maybeRunAgencyPass()
}

// recordTaskRowClosed writes the terminal-write witness into the session
// JSONL (sessionevent.EventTypeTaskRowClosed): which row the END hook just
// wrote, the status it wrote, the run's end reason, and — when the write
// FAILED — the error.
//
// It is the run's own evidence that the hook reached the ledger, which is what
// the truncated-run defect left unprovable: the row sat at `in_progress`
// forever while the hook returned early, and the transcript could not tell
// that from a write that landed elsewhere (the status rule was already
// correct). Best-effort by contract like every other line of the close-out —
// but never silent (Rule 15): a logger that cannot write this reports it.
func (a *Agent) recordTaskRowClosed(taskID, status string, truncation runend.Reason, writeErr error) {
	if a == nil || a.SessionLog == nil {
		// No session log (minimal test Agents / chat close-out faces):
		// nothing to witness into. The ledger write itself already happened.
		return
	}
	errText := ""
	if writeErr != nil {
		errText = writeErr.Error()
	}
	if err := a.SessionLog.LogTaskRowClosed(taskID, status, truncation.Token(), errText); err != nil {
		a.logAutoTaskf("task ledger: terminal-write witness for %s not logged (continuing): %v", taskID, err)
	}
}

// recordRunTruncationOnWrapperRow writes the child's TESTIMONY about an
// engine-forced stop onto the WRAPPER row, as the append-only runend note.
//
// It is all that remains of the child-side run close-out (task 01a16f7c), and
// it is a different question: the close-out asked "is my wrapper still alive?"
// — a liveness guess about the process that WAITs for us, whose answer cannot
// change the outcome — while this asks "did the ENGINE cut the run short?" and
// records the answer where the only surviving writer will read it.
//
// WHY IT MUST EXIST AT ALL: a `task run` / `worker` child registers NO auto row
// (single-row exec), so on a truncated run this note is the ONLY place the
// reason reaches the wrapper's row — and every engine-forced stop exits ZERO,
// so the wrapper's post-Wait writeback would otherwise stamp `completed`
// (tools.RunEndReasonFor is the reader that refuses it).
//
// WHAT IT DOES NOT DO: it writes no status, closes no row, and reads no
// process. A finished run records nothing (an ordinary run's trail is
// unchanged), and a session that is not a run child — blank TaskRunIDEnv — is
// a no-op.
func (a *Agent) recordRunTruncationOnWrapperRow(truncation runend.Reason) {
	runID := strings.TrimSpace(os.Getenv(tools.TaskRunIDEnv))
	if runID == "" || !truncation.Truncated() {
		return
	}
	// The passed-in reason is UNIONED with what the row already records
	// (runEndReasonRecorded): the auto-row arm and this note are two doors to
	// the same fact, and either may have been the one that ran.
	reason := truncation.Or(runEndReasonRecorded(runID))
	if !reason.Truncated() {
		return
	}
	tools.RecordRunEnd(runID, reason)
}

// THE CLOSE-OUT REPORT'S VOCABULARY IS GONE WITH THE CHAIN IT DESCRIBED
// (2026-10-30, task 01a18799, slice 3/4). What stood here was the
// one-report-per-run deferral (`clientFinishCleanupReporter`, its two coverer
// labels and `finishCleanupDeferralFmt`) and `finishCleanupRepSummary` — the
// renderer of a *tools.FinishCleanupReport*. A report type with no producer is
// a renderer for a fact nothing measures, so it went with the chain.
// internal/closeouthandoff still exists: its Declare/Take half is what
// `gopi session resume` uses to keep one run's close-out single-sourced.

// terminalPushSkipEnv turns the M4.19 ② must-succeed terminal push off
// (sims / callers managing sync themselves). Default is PUSH (the must-
// succeed contract); the claim broadcast keeps its own GOPI_SKIP_CLAIM_
// BROADCAST kill-switch.
const terminalPushSkipEnv = "GOPI_SKIP_TERMINAL_PUSH"

func (a *Agent) terminalPushSkipped() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(terminalPushSkipEnv)), "1")
}

func finishCleanupRepSummary(rep *tools.FinishCleanupReport) string {
	if rep.Skipped {
		// ONE renderer for the skip tail (Rule 14): the log line and the CLI
		// line must not disagree about which steps already ran — the tail
		// names them because a skip reason is ONE step's state, not the
		// chain's (measured 2026-10-12: a delivery that landed printed as a
		// skip). The plan arm is excluded by the shared function, not here.
		return tools.FinishCleanupSkipVerdict + rep.SkipReason + tools.FinishCleanupSkipStepTail(rep)
	}
	ok := make([]string, 0, len(rep.Steps))
	failed := make([]string, 0, len(rep.Steps))
	for _, s := range rep.Steps {
		if s.OK {
			ok = append(ok, s.Step)
		} else {
			failed = append(failed, s.Step+": "+s.Detail)
		}
	}
	line := fmt.Sprintf("%s branch=%s done=[%s]", rep.TaskID, rep.Branch, strings.Join(ok, ","))
	if len(failed) > 0 {
		line += " failed=[" + strings.Join(failed, "; ") + "]"
	}
	return line
}

// AdoptLedgerRow tells the agent that THIS run CONTINUES an existing ledger
// row, so the end hook must close that row instead of minting one of its own
// (epic resume-writes-terminal-status). It is the resume path's half of the
// start hook: `gopi session resume <task-id>` continues a row whose previous run died
// without writing a terminal status, and before this the close-out machinery
// had no row to latch onto — the resume's --task linkage was a bare ledger tag,
// so the row stayed exactly as the dead run left it (the operator's symptom: a
// finished resume whose row still read `failed` with `ended_at` frozen at the
// first run's death).
//
// It is deliberately NOT an environment variable and NOT the wrapper id:
// GOPI_TASK_WRAPPER_ID means "this process is a task run's CHILD" and other
// guards consume that meaning (the worktree/console/main dispatchers), so a
// resume that exported it would be lying about what it is. The resume is
// neither a child nor a fresh session: it is the SAME row being executed again,
// and it says so in process.
//
// Best-effort by contract (the ledger must never be able to refuse a recovery):
// an empty id is ignored, and a row that no longer exists or is already terminal
// is left untouched by the end hook's own guards — never forced.
//
// It reuses autoTaskRowID rather than adding a field — that variable IS "the
// row this run closes out" (the start hook sets it for a wrapper child, the end
// hook writes it), and a second one would be the same fact twice (Rule 14's
// 双份维护). Reusing it also keeps the change inside this file: the 870 devdoc
// citations into `internal/agent/loop.go` are line-numbered, so an unrelated
// fix that inserted there would re-key them all (a churn cost — the gate that
// once asserted them, the now-deleted doc-anchor gate, went with
// internal/docclass in 5a35f61 and nothing reads a `loop.go:NNN` anchor now).
//
// It sets BOTH halves of the start hook's latch, and it MUST: that hook is
// documented "called once per NON-RESUME run", so on the resume path nothing
// else ever sets autoTaskRegistered — an adoption that only named the row left
// the end hook returning early and the resume wrote NOTHING (measured: the row
// stayed exactly as the dead run left it). Setting it HERE, before the engine,
// is also what makes the liveness heartbeat
// (`setTaskHeartbeatSession → taskheartbeat.go touch, WHERE task_id = ?
// AND status='in_progress'`) keep the CONTINUED row alive for the whole run.
//
// The latch lives HERE and NOT in the end hook: the end hook CLEARS
// autoTaskRegistered by design ("one row per session — never updated twice"),
// and the end hook legitimately runs twice (TestAutoTaskRegisterOnAndEndHook),
// so an end-hook latch re-armed it on a spent AUTO row and the second end hook
// mutated a row it must not touch (measured regression). Nothing in the end
// hook changes.
func (a *Agent) AdoptLedgerRow(taskID string) {
	if a == nil {
		return
	}
	id := strings.TrimSpace(taskID)
	if id == "" {
		return
	}
	a.autoTaskRowID = id
	a.autoTaskRegistered = true
	a.logAutoTaskf("task ledger: adopted row %s for this run (a resume CONTINUES an existing row: the existing close-out writes its terminal status, no auto mirror)", id)
}

// AdoptedRowID reports the row this run is latched onto, or "" when it is
// latched onto none — the value AdoptLedgerRow sets and latchWrapperRow /
// the end hook read.
//
// It exists so the resume path's adoption GUARD is observable without a ledger
// and without running an agent: `gopi session resume <session-id>` must NOT adopt (the
// uuid names no row), and the only honest way to assert that is to read back
// the latch. A test asserting it by reaching into an unexported field through
// a second mechanism would be a second reader of the same fact (Rules 13/14);
// this is a read-only accessor over an EXISTING field, which is why it adds no
// state and shifts no line any devdoc cites (the line-anchor gate that once
// asserted the `loop.go:NNN` citations was deleted with internal/docclass in
// 5a35f61; what runs now is the config-anchor census over CONFIG.md rows).
func (a *Agent) AdoptedRowID() string {
	if a == nil {
		return ""
	}
	return a.autoTaskRowID
}

// latchWrapperRow resolves the row an ALREADY-EXISTING execution attaches to,
// in one place, for the two ways that happens: a `task run` CHILD (whose row
// arrives as GOPI_TASK_WRAPPER_ID, exported by the wrapper pre-spawn) and a
// `gopi session resume <task-id>` (whose row arrives in-process via AdoptLedgerRow —
// the resume spawns no child, so there is nothing that could inherit it).
//
// It reports whether this run attaches to an existing row; the caller then skips
// auto-registration entirely. Splitting these two gates into separate `return`
// sites in maybeRegisterSessionTask is what made the first draft of this fix
// fail silently: a resume run has NO SessionLog yet at the start hook, so a
// second gate placed below the session-log check never ran.
//
// The gate reads autoTaskRowID, which an ADOPTING resume already set before the
// engine started (AdoptLedgerRow) — for that run the start hook may never be
// called at all, and the latch it does not set was already set by the adopter.
// This is the ONLY latch point; see AdoptLedgerRow for why an end-hook latch
// was tried and reverted (it re-armed a spent auto row).
func (a *Agent) latchWrapperRow() bool {
	if wrapperID := strings.TrimSpace(os.Getenv(tools.TaskWrapperEnv)); wrapperID != "" {
		a.autoTaskRowID = wrapperID
		a.logAutoTaskf("task ledger: wrapper child (%s=%s) — no auto row (execution tracked on the human wrapper row)", tools.TaskWrapperEnv, wrapperID)
		return true
	}
	// Adopted-row gate: this run CONTINUES an existing row (AdoptLedgerRow, set
	// by `gopi session resume <task-id>` before the engine starts). The row is already
	// the run's single execution record — the resume must not mint an auto
	// mirror beside it (the same 双份维护 reasoning as the wrapper gate:
	// closeout×2 / display×2 / sync×2 / stale×2). Reporting true here is what
	// keeps the close-out machinery — the end hook's terminal status write, its
	// terminal push and its finish-cleanup chain — acting on the CONTINUED row
	// instead of opening a fresh one.
	if id := strings.TrimSpace(a.autoTaskRowID); id != "" {
		a.logAutoTaskf("task ledger: adopted row %s for this run (continues an existing row; no auto mirror)", id)
		return true
	}
	return false
}

// RegisterSessionTask is the exported wrapper around maybeRegisterSessionTask
// so the gopi chat CLI (package main) can register its chat conversation as a
// task-ledger row. It inserts an in_progress row with an auto t-<unix>
// task_id (session UUID in the session_id column — consolidation decouples
// the two), seeded with the given briefing text. No-op when auto-
// registration is off or the session is missing.
func (a *Agent) RegisterSessionTask(briefing string) {
	a.maybeRegisterSessionTask(briefing)
}

// FinishSessionTask is the exported wrapper around finishSessionTask so the
// gopi chat CLI can close the chat's task-ledger row at exit (flipping it to
// completed/failed by exit code and appending the run stats). No-op when no
// row was registered.
func (a *Agent) FinishSessionTask(exitCode, iterations, tokens int, elapsedSec float64) {
	a.finishSessionTask(exitCode, iterations, tokens, elapsedSec)
}
