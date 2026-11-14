package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/signalrec"
	"github.com/nickhuang99/gopi/internal/tools"
)

// usage is the top-level gopi -h text, built from the env-var/settings/CLI
// catalog in internal/config so it can never drift from the knobs the code
// actually honours.
var usage = config.FormatUsage()

// The `gopi worker` argv vocabulary (Rule 1: the words this face accepts are
// named HERE, never typed inline; the scan below references them).
const (
	// workerFlagHelp / workerFlagHelpLong are the usage-query words. They are
	// kept as one pair so the -h scan can never recognize one spelling and
	// miss the other.
	workerFlagHelp     = "-h"
	workerFlagHelpLong = "--help"
	// workerFlagHelpWords joins the pair for the refusal message's "accepted:"
	// list — one place renders the two spellings.
	workerFlagHelpWords = workerFlagHelp + "/" + workerFlagHelpLong
	// workerFlagOnce is the drain-then-exit mode.
	workerFlagOnce = "--once"
	// workerDaemonFlag is the detach mode — the SAME spelling the merger face
	// accepts (cli_daemon_spawn.go owns it), referenced here so the scanner,
	// the refusal message and the child's re-exec argv cannot drift.
	workerDaemonFlag = workerDaemonFlagLong
	// workerFlagValueSep is the `--flag=VALUE` separator (both forms are
	// accepted; the scanner trims it, never a bare "=").
	workerFlagValueSep = "="
	// workerUnexpectedArgExitCode is the exit status of a REFUSED worker
	// launch: the POSIX usage class (2). It is deliberately distinct from the
	// agent-spawn guard's refusal (config.AgentGuardRefusalExitCode = 3) so a
	// caller can tell "your command line is wrong (or this binary is stale)"
	// from "this launch is forbidden from an agent session" — two different
	// operator actions. 1 stays "the run itself failed".
	workerUnexpectedArgExitCode = 2

	// workerLogRootRetiredFlag is the flag this verb USED to accept
	// (--worker-log-root DIR), named here ONLY so the refusal message can tell
	// a launcher spelled against the old contract why it was refused and what
	// replaced it (Rule 1b: the spelling lives in a constant, never inline in
	// the format string). Nothing reads it as an option any more — the child's
	// output destination is derived from the row at the spawn seam.
	workerLogRootRetiredFlag = "--worker-log-root"
)

// workerUnexpectedArgMessage renders the Rule 15 refusal for an argv word the
// worker verb does not recognize. It names the word VERBATIM, states that the
// loop did NOT start, and lists the vocabulary this binary does accept — so a
// launcher can distinguish a typo from an option newer than the deployed
// binary (the stale-binary incident this refusal exists for) without reading
// the source.
//
// ONE word gets its own remedy: the RETIRED row selector (`--id <task-id>`).
// It is not a stale-binary case — the spelling was this binary's own until the
// row became positional (epic worker-positional-taskid) — and answering it with
// "upgrade your binary" would send the operator to install the version they
// already have (Rule 15: the two failures need two different actions).
func workerUnexpectedArgMessage(arg string) string {
	remedy := "  if you passed an option a newer gopi supports, the deployed binary is STALE\n"
	if workerIdRetiredSpelling(arg) {
		remedy = fmt.Sprintf("  the row is POSITIONAL now: `gopi worker %s` (the %s flag was retired)\n",
			workerPositionalSyntax, workerIdRetiredFlag)
	}
	return fmt.Sprintf(
		"gopi worker: unexpected argument %q — refusing to start the worker loop\n"+
			"  accepted: %s | %s | "+workerPositionalSyntax+" [%s] [%s <holder>] [%s <path>] [%s <alias>] [%s <target>] | %s\n"+
			"  (the row is an ARGUMENT, not a flag: `gopi worker "+workerPositionalSyntax+"` runs that one row)\n"+
			"  (the retired `%s <dir>` is no longer a flag: a task's output goes to its own\n"+
			"   session directory, <gopi-dir>/sessions/"+workerPositionalSyntax+"/output.log)\n"+
			remedy+
			workerResumeSpellingGateMsg+
			"  usage: `gopi worker %s`\n",
		arg, workerFlagHelpWords, workerFlagOnce, workerFlagTakeover,
		workerFlagAs, workerFlagDB, flagReportTo, flagRepo, workerDaemonFlag,
		workerLogRootRetiredFlag, workerFlagHelp)
}

// workerResumeSpellingGateMsg is the accepted-vocabulary line of the resume
// mode, kept as its own constant because TWO operator-facing refusals carry it
// (the grammar's unexpected-argument message and the resume mode's own
// contradictions): an operator who mistyped a selector and one who passed a
// word this binary does not know must both learn the same accepted set, or the
// second refusal becomes a dead end. One spelling, two readers (Rule 1b/13).
const workerResumeSpellingGateMsg = "  resume: `gopi worker " + workerFlagResume + " " + workerPositionalSyntax + "` (the default) | `gopi worker " +
	workerFlagResume + " " + tools.TaskResumeGroundBranchFlag + " <branch-name>` | `gopi worker " + workerFlagResume + " " +
	tools.TaskResumeGroundWorktreeFlag + " <worktree-path>`\n" +
	"          (the row's own recorded worktree/branch is the ground; the two flags name it\n" +
	"           directly. The ground must ALREADY exist — this mode never creates a worktree)\n"

// isWorkerSubcommand reports whether argv begins with the `worker` verb —
// the M4.20 worker loop's top-level CLI face (`gopi worker`). It is a
// TOP-LEVEL verb (not `gopi task worker`): a worker is a process form of
// the runner, not a ledger verb, and its invocation must be the shortest
// possible surface for a shell loop or a job runner.
//
// P0-1 guard (devdoc/review-cli-task-sync.md): `-h`/`--help` must NEVER have
// side-effects. The old path ran runTaskWorker() on ANY argv starting with
// "worker" — so `gopi worker -h` started a REAL worker, claimed a live
// pending task and executed it (t-1788305178/t-1788305179 burned with
// "broken pipe" failures). Every other subcommand scans for -h; worker was
// the sole offender. Now the argv is scanned BEFORE the loop runs and the
// usage head is printed + exit 0 — claiming nothing.
//
// UNKNOWN ARGUMENTS ARE REFUSED (Rule 15, devdoc/worker-unknown-flag.md): the
// scan used to ignore every word it did not recognize, so a launcher passing
// an option the deployed binary predates got a SILENTLY DEGRADED worker — the
// option dropped, the loop started, nothing said so (the hp-inline-launchers
// sweep: a stale remote binary swallowed `--worker-log-root`, so the worker
// ran with output attribution OFF). An unrecognized word now exits
// workerUnexpectedArgExitCode with workerUnexpectedArgMessage BEFORE the
// guard and BEFORE any claim — the launch is refused, not degraded.
//
// The grammar is deliberately tiny and positional: a recognized VALUE flag
// (`--worker-log-root DIR`) consumes the word right after it as its operand,
// so an arbitrary directory is never mistaken for a stray argument. Every
// other word — a dash-flag this binary does not know, or a positional — is
// the refusal.
func isWorkerSubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbWorker {
		return false
	}
	// -h/--help FIRST (P0-1): a usage query must be a zero-side-effect path —
	// the scanner below never sees it, so help can never start a loop (or a
	// daemon) no matter what else the argv carries.
	//
	// The scan is POSITIONAL-AWARE, because this verb owns one positional
	// VERB WORD: `gopi worker list` is the read-only enumeration's natural
	// spelling (workerControlList), so `gopi worker list -h` must print the
	// same usage page `gopi worker -h` prints — that is the discovery path the
	// alias exists for. The word is recognized ONLY when it stands where a verb
	// word stands (right after the verb): everything after it is that word's
	// own argv, where a `-h` belongs to IT, not to this face.
	if workerVerbWordNamesStatus(args[1:]) && workerHelpRequested(args[2:]) {
		fmt.Print(workerUsageHead)
		os.Exit(0)
	}
	//
	// WHAT A BARE POSITIONAL MEANS (the hazard this ordering closes): below
	// this block a word that is not dash-prefixed is scanned as an EXPLICIT
	// TASK ID. A control word therefore has to be recognized BEFORE the help
	// scan falls through to the loop's scanner, or a read-only question would
	// be answered by claiming a row named "list". The control route below does
	// exactly that; this pre-check only makes `list -h` a usage query.
	if workerHelpRequested(args[1:]) {
		fmt.Print(workerUsageHead)
		os.Exit(0)
	}
	// THE WORKER-FACE STARTUP NOTICES USED TO STAND HERE, and the LAST one left
	// with the state it described (operator ruling 2026-10-23).
	//
	// The `compute_nodes` report went first (2026-10-22, task 01a160a2): its
	// knob had no consumer, so the knob was deleted rather than explained on
	// every worker start. What remained was the EMPTY-dev_repo DEPRECATION — one
	// line per worker start telling a host that `dev_repo` was unset that it was
	// on the "deprecated" worktree channel. That line was withdrawn for the
	// reason a notice is ever withdrawn: the state it called deprecated is the
	// NORMAL one. `dev_repo` exists because two workflow channels once needed a
	// choice; with one channel left, the choice — and the requirement — is gone,
	// so it degrades to an OPTIONAL fallback. A host that never declares it is
	// not misconfigured: a run started inside any git repository works.
	//
	// WHERE THE SUBJECT WENT: the state is now ANSWERED rather than announced.
	// internal/reposource resolves the run's repository in one fixed order (the
	// row's own deliverable.repo → the current working directory's repo →
	// settings `dev_repo`) and the ONLY thing that speaks is the refusal after
	// all three decline, listing each source's own verdict; `gopi config show`
	// reports the same answer read-only (cli_config_run_repo.go). So the
	// question "am I on a deprecated channel?" no longer exists, and no worker
	// start spends a line on it.
	//
	// The -h scan above still answers a usage query before ANY of this runs
	// (P0-1).
	// ONE scanner for the verb's grammar (cli_worker_daemon.go): it yields the
	// mode + the output root, and the FIRST unrecognized word else "".
	//
	// The CONTROL verbs (--status/--stop/--restart/--start, +--force/--wait)
	// route BEFORE that scan (cli_worker_control.go): they act on the RESIDENT
	// service from a SEPARATE process — they neither take the lock nor run a
	// round — and they are not loop flags, so letting them reach the scanner
	// would report them as unknown words (the very failure this routing
	// prevents). --status is READ-ONLY and takes no lock at all.
	//
	// A READ-ONLY flag PLUS an ACTING one (`--status --stop`) is refused HERE,
	// before the status route can return: routing the read first made the
	// combination a silent no-op with a success exit code (the F1 defect —
	// see controlConflictMessage for why a refusal, not an ordering rule).
	if workerControlRequested(args[1:]) {
		// ONE conflict check for the whole face: a read beside an act, or more
		// than one distinct act, is refused HERE with every flag named — through
		// the shared reporter (controlConflictRefuse) rather than a message
		// spelled in this file. It runs BEFORE the status route, which is what
		// makes the refusal reachable at all (the F1 defect: routing the read
		// first turned a read+act into a silent no-op with a success code).
		//
		// The refuser is asked FIRST and the read-only route SECOND, from the
		// SAME parse: a read-only enumeration is a side-effect-free report and
		// must never be reachable when an act rides along.
		opts, _ := workerControlParse(args[1:])
		if controlConflictRefuse(workerControlConflictParse(args[1:], opts.actions, opts.read, opts.act, opts.flags)) {
			return true // unreachable: the emitter exits
		}
		// TARGET SELECTION (`--pid` / `--branch`), decided from the SAME parse:
		// a selector that cannot be read, or one that arrived with no action, is
		// refused HERE — before the status route and before workerControlCommand —
		// so a malformed selector can never reach the loop's scanner (where
		// `--branch` used to be read as the RESUME mode's ground) or the
		// unselected enumeration fallback.
		//
		// A DEFERRAL is not a refusal and not a selection: it is this face
		// saying "that word is not mine to answer without an action" (a bare
		// `--branch`), so the invocation LEAVES this block entirely and the
		// scanner below runs — the resume face's own refusal is then what the
		// operator reads. Falling through to the calls at the END of this block
		// instead would be the same answer by a longer route: workerControlCommand
		// re-parses the selector and takes its own `applied` arm, i.e. the
		// selected route this deferral exists to avoid (measured).
		sentence, refuse, applied, deferred := workerControlSelectionGate(args[1:], opts.action)
		if refuse {
			fmt.Fprint(os.Stderr, sentence)
			os.Exit(workerConflictExitCode)
		}
		if applied {
			// A SELECTED invocation is the control face's, even when the action
			// is the READ-ONLY --status: the selector's answer is one line about
			// the ONE target it resolved (with the chain that reached it), while
			// the unselected --status is an enumeration of every worker. Routing
			// the read here unconditionally would answer the enumeration — the
			// same "which worker?" substitution the epic is about.
			workerControlCommand(args[1:])
			return true
		}
		if !deferred {
			if workerStatusRequested(args[1:]) {
				// The READ-ONLY enumeration: one live line per worker (state file +
				// /proc, argv fallback included), stopped with the reason when there
				// is none, then the state files that are NOT workers. Still lock-free
				// and still creating nothing; the verbose spelling is the same read
				// with every residue path listed, and `--json` is the same read as
				// ONE machine-queryable document (worker_status_json.go).
				os.Exit(workerStatusReport(workerStatusVerboseRequested(args[1:]),
					workerStatusJSONRequested(args[1:])))
			}
			workerControlCommand(args[1:])
			return true
		}
	}
	wa, bad := scanWorkerArgs(args[1:])
	if bad != "" {
		// Nothing else is part of this verb's argv: refuse the launch with the
		// offending word named (Rule 15). Reaching here means the loop (or the
		// daemon) would otherwise have started without the caller's option.
		fmt.Fprint(os.Stderr, workerUnexpectedArgMessage(bad))
		os.Exit(workerUnexpectedArgExitCode)
	}
	if wa.refuse != "" {
		// A CONTRADICTORY argv (--once with a named row, a named-run flag
		// without one, --daemon with one): every word is known, so the upgrade hint
		// would be wrong. The message is the scanner's (Rule 2: one reader of
		// the shape, one sentence) and exits in the same usage class.
		fmt.Fprint(os.Stderr, wa.refuse)
		os.Exit(workerUnexpectedArgExitCode)
	}
	// The RESUME mode dispatches here — BEFORE the agent-spawn guard, and
	// deliberately: `--resume` runs an EXISTING row's checkpoint through the
	// ordinary resume path, exactly as an operator typing `gopi session resume` does.
	// It starts no loop and claims nothing, so the guard's ruling (an agent
	// session must never START the worker LOOP) has nothing to rule on — and
	// applying it here would refuse the very repair path this mode exists to
	// give the operator from inside a session.
	if wa.resume {
		runTaskWorkerResume(wa)
		return true
	}
	// The --daemon branch sits BEHIND the guard below on purpose: detaching the
	// worker does not make it a legal agent launch. The guard's ruling (an agent
	// session must never START the worker loop) is about the loop, not about who
	// holds the terminal — and an agent that could detach it would merely move
	// the same never-returning process out of its own process group, which is
	// exactly what setsid could not fix in the 04:12 incident.
	guardVerbOrExit(CliVerbWorker)
	// NOTE: there is no output-root knob to apply here any more (epic
	// child-stream-task-output). The destination of a task's text is derived
	// from the ROW at the spawn seam (cli_tasks_exec_output.go) — the worker
	// loop never re-points its own streams, so the parent has nothing to
	// configure and nothing to forward to the re-exec'd child.
	if wa.daemon {
		workerDaemonSpawn(args[1:])
		return true
	} // A daemon SEAM flag (--require-scope) without --daemon has nothing to
	// modify: refusing it is Rule 15's answer to a silently-dropped option — an
	// operator who believes they bought a hard scope guarantee must not get a
	// foreground worker that says nothing.
	if wa.seam {
		fmt.Fprint(os.Stderr, workerSeamFlagAloneMessage(daemonScopeRequireFlag))
		os.Exit(workerUnexpectedArgExitCode)
	}
	// THE TERMINAL-SCOPE HINT (task 01a163f3, measured 2026-10-22 22:04:55): a
	// FOREGROUND worker inherits the terminal's systemd scope, so the terminal's
	// teardown hangs the worker up mid-round (`vte-spawn-*.scope`,
	// `signal:SIGHUP`, `sourced:false` in the run's signal-provenance.jsonl).
	// The measurement is a FRESH /proc/self/cgroup read (Rule 24: nothing else
	// is a verdict) and the answer is ONE line — a HINT, never a refusal: a
	// foreground run is the documented way to watch a worker debug a row.
	//
	// IT SITS ON THE FOREGROUND FACE ONLY, AFTER the --daemon branch returned
	// above: the daemon face has already left the terminal's lifetime, so the
	// line would be a false alarm there. It also sits after the -h scan and the
	// control-verb routes (both return earlier), so `-h` stays a
	// zero-side-effect path and `--status` prints one line per worker, not a
	// banner.
	reportWorkerTerminalScopeHint()
	runTaskWorker(wa.once, wa.explicit, wa.runOptions())
	// The PRE-claim reachability refusal has its OWN exit code (deliverable 3 of
	// epic api-preflight-claim-gate): "nothing ran because this NODE cannot
	// reach the API" must be distinguishable from a drained queue (0), a failed
	// row (1), a bad argv (2) and a forbidden launch (3) — otherwise a green
	// scheduler reads a transport-dead node as idleness and retries forever.
	if code := workerApiGateRefusedExitCodeFor(); code != 0 {
		os.Exit(code)
	}
	return true
}

// runOptions renders the explicit face's parsed flags as the run options the
// round executes with: `gopi worker <task-id> [--takeover] [--as HOLDER]
// [--db PATH] [--report-to ALIAS] [--repo TARGET]` is ONE row's run, and these
// are that row's options — the same `tasksOptions` fields the retired
// `gopi task run <task-id>` filled (one option struct, one run path).
//
// It returns nil for a plain loop invocation (no row named), which is what makes
// the loop's "claim and run the next row" arm the ZERO case rather than a second
// code path.
func (wa workerArgs) runOptions() *tasksOptions {
	if wa.explicit == "" {
		return nil
	}
	return &tasksOptions{
		taskID:   wa.explicit,
		takeover: wa.takeover,
		as:       wa.as,
		db:       wa.db,
		reportTo: wa.reportTo,
		repo:     wa.repo,
	}
}

// workerSeamFlagAloneMessage is the refusal for a daemon seam flag used without
// --daemon (one spelling of the sentence, so both faces say the same thing).
func workerSeamFlagAloneMessage(flag string) string {
	return fmt.Sprintf("gopi worker: %s only means something together with %s\n", flag, daemonFlagLong)
}

// workerUsageHead is the help text for the `gopi worker` verb (Rule 1: named
// constant, the single source). Printed by `gopi worker -h` / `--help` — a
// usage query must be a zero-side-effect path, which is exactly what the P0-1
// fix guarantees.
//
// SHAPE (epic "worker help is misleading", 2026-09): a user typing -h asks
// WHAT IT CAN DO. Invocations first, then options and environment; the why/how
// detail — the loop's wire vocabulary, the cgroup measurement behind the
// privilege answer — belongs at the END (the Notes tail), if at all, and the
// mechanism itself lives in devdoc/. `cmd/agent/help_capability_first_test.go`
// is the gate that keeps this shape; TestWorkerUsageHeadCoversLoopContract
// keeps the FACTS (in user words) on the page.
const workerUsageHead = `GoPi worker — run the queued tasks, then exit. No daemon to babysit.

  gopi worker                 run tasks until the queue drains, exit 0 by itself
  gopi worker --once          same drain; exits as soon as the queue is empty
  gopi worker [<task-id>]     run THAT row and nothing else, then exit
                              (exit 1 if it failed)
  gopi worker --resume <task-id>       finish THAT row's own ground (a CLONE when
                              dev_repo is declared) and PUBLISH it in the close-out
  gopi worker --resume --branch B    the same, when you hold the BRANCH
  gopi worker --resume --worktree P  the same, with P naming the ground
  gopi worker --daemon        same loop, detached (returns at once; logs to
                              <gopi-dir>/logs/worker.log)
  gopi worker --status        what is running, and WHICH TASK each one runs
                              (read-only; a bare gopi worker is a FOREGROUND
                              worker — listed by pid)
  gopi worker --status --json  the same read as one document (see Options)
  gopi worker list            the same read, spelled as a question — the alias
                              to reach for when you want to know which workers
                              are running (identical output to --status)
  gopi worker --stop          finish the in-flight round, then exit; the worker
                              is asked, not killed (see Options)
  gopi worker --stop --branch B   stop the worker that holds branch B (the row
                              owning B → its claim stamp → that holder's pid)
  gopi worker --stop --pid N  stop that one worker, as --status listed it
  gopi worker --restart       --stop, then --start (FOREGROUND workers are
                              reported, not signalled: a restart replaces the
                              daemon and never claims to stop them)
  gopi worker --start         start a detached worker (refused if one runs)
  gopi worker -h|--help       this help (claims nothing)

Options:
  --once             stay in the foreground until the queue is empty, then exit
                     0 (a bare "gopi worker" is the same drain; the
                     unambiguous spelling for a launcher script)
  <task-id>          the row to run, as a bare ARGUMENT (no flag): run exactly
                     this ONE row in the foreground, then exit
  --takeover         with a task id: run a row a live process already holds
  --as HOLDER        with a task id: the holder identity to claim/report under
  --db PATH          with a task id: the ledger to run against
  --report-to ALIAS  with a task id: ssh alias a DISPATCHED run reports back
                     through (the remote half of a handoff)
  --repo TARGET      with a task id: git push-back target for the finished branch
  --resume           resume a row that ALREADY has a worktree, and clean it up:
                     name the row, or name the ground with --branch/--worktree
  --branch B         TWO readers, one word. With --resume: the branch whose LIVE
                     worktree is the ground. With --stop/--restart/--status: the
                     branch whose WORKER to address (the row owning B, then its
                     holder pid). A branch no row owns is refused by name — it
                     never falls back to "every worker"
  --worktree P       with --resume: the ground by path — what pwd prints
                     inside it, or the path git worktree list shows for it
  --pid N            with --stop/--restart/--status: address THAT process (the
                     pid --status lists). Alive but not a gopi worker is
                     refused by name, never signalled
  --daemon           detach; at most ONE detached worker per profile
  --status           list live workers (daemon records AND FOREGROUND worker
                     pids), and dead-pid state files
                     ("gopi worker list" is the same read as a POSITIONAL — the
                     natural spelling of the question)
  --status-verbose   the same, with every residue path
  --json             with --status: the same read as ONE machine-queryable
                     document, so the task is a VALUE (man gopi-worker)
  --stop             graceful stop: the in-flight task finishes. Without --pid
                     or --branch the stop addresses every worker of the
                     profile; a signal is added only for a DETACHED daemon
                     whose binary we can verify is this one
  --restart          --stop, then --start, atomically
  --start            start a detached worker only if none is running
  --force            with --stop/--restart: interrupt the round now
  --wait DURATION    bound on the graceful wait (default 25m; 0 = no wait)

Environment:
  GOPI_WORKER_MAX_ROUNDS=N    rounds before exit (default 0 = unlimited)
  GOPI_WORKER_IDLE_SEC=N      seconds between re-checks on a drained
                              queue (default 30; --once exits instead)
  GOPI_WORKER_IDLE_EXITS=N    consecutive empty rounds before the worker
                              exits 0 (default 3 ≈ 90s; 0 = never, kill
                              to stop)

Notes:
  This is the ONLY command that runs a task row. "gopi task" manages the
  ledger (submit/list/show/verify/…); it does not run anything.

  One round: claims one pending task, runs it, records the result, syncs with
  the other nodes, then claims again — a lost claim race yields to the next
  round, so two workers never run the same row. ONLY the QUEUE faces (bare and
  --once) loop like that: a NAMED row is one round and then exit. Rows tagged
  "pa-queue" are never claimed — name a row as the argument to run it.

  Privileges: a worker needs NO privilege, --daemon included — running it as
  root obtains no more than running it as yourself.

  Detached start: --daemon returns only once the worker is really up; with
  --require-scope a start that could not survive your login session FAILS
  rather than detaching a worker that quietly exits with your shell.

  Resume: --resume is the ONLY face that removes the worktree a finished row
  delivered from, because it is the only one not standing in it. The ROW is the
  default selector (its recorded worktree and branch are the ground);
  --branch/--worktree name the ground directly. A refusal is reported with its
  reason and never rewrites the resume's outcome.
  This is the ground-HOLDING resume; a plain SESSION with no row behind it is
  continued with ` + workerResumeSessionAddressSpelled + `, which holds no ground and
  releases none.

  Output: the task's own text goes to its session directory —
  <gopi-dir>/sessions/<task-id>/output.log; the worker's own output stays on
  the stream IT was launched with, so a foreground run is readable live.

  Exit codes: a drained queue is SUCCESS (the loop exits 0 whatever the rows
  it ran did — read the rows). Naming a row reports it: 0 when it completed,
  1 when it failed or could not be run. A launch that is REFUSED never runs
  anything, and reports itself with its own code:
    Exit code: 2  an argument this verb does not accept
    Exit code: 3  the agent-spawn guard refused the launch — an agent
                  session must never start this loop (the refusal says so
                  and names both operator escapes)
    Exit code: 4  the PRE-claim API reachability preflight found this node
                  cannot reach the LLM endpoint: NOTHING was claimed (the
                  queue is untouched and the rows stay pending), and the
                  measured evidence — endpoint, per-attempt durations and
                  remedy — is on the stream above this code
  Read-only faces exit 0 without a lock: --status, -h|--help, and the other
  reporting verbs (--stop, --restart) answer and return.
  The pages behind this one, and why each paragraph is sized as it is:
  devdoc/worker-page-background.md.
`

func main() {
	args := os.Args[1:]

	// broken-pipe guard for task-run children (t-1788305178 incident,
	// 2026-09-02): `gopi task run` spawns this same binary as its child with
	// stdout/stderr inherited from an external launcher's pipe. If the
	// launcher closes the pipe mid-run (hermes turn end / SIGTERM / timeout
	// kill), the child's next stderr write gets EPIPE → Go's runtime raises
	// SIGPIPE on fd 1/2 → default disposition kills the child (observed
	// "signal: broken pipe", exit 141, session truncated, spurious FAILED
	// writeback). Ignoring SIGPIPE ONLY when running as a task-run child
	// (GOPI_TASK_WRAPPER_ID is the env the wrapper exports pre-spawn —
	// also the wrapper-child gate in maybeRegisterSessionTask and the
	// executor's deliverable-worktree key)
	// turns the closed-pipe write into a harmless EPIPE error the stream
	// code swallows, so the run + writeback survive a vanished launcher.
	// Top-level `gopi | head` semantics are untouched — a plain run keeps
	// the default SIGPIPE disposition. (The wrapper-side signal.Ignore in
	// runTaskRun is belt-and-braces for the wrapper's OWN stderr writes; Go
	// resets SIGPIPE to default at exec, so the child must set its own
	// ignore — this is the effective one.)
	if os.Getenv(tools.TaskWrapperEnv) != "" {
		signal.Ignore(syscall.SIGPIPE)
	}

	// Process-global config flag bootstrap (subcommand-side twin of the
	// --settings/--home config block below): apply a leading `--settings
	// PATH` / `--home PATH` to the ENVIRONMENT BEFORE the subcommand chain.
	// The group verbs (gopi task run, worker, memory, web, …) return from
	// main() at the chain below, never reaching the agent-path config block —
	// without this hoist a `gopi --settings X worker T` launch drops X (the
	// child/recovery would fall back to the default provider). Runs BEFORE
	// the subcommand chain so every subcommand and its spawned child inherit
	// the injected env.
	// Dispatch argv for the subcommand chain: the raw args minus the
	// process-global config tokens BootstrapEnvFromArgs just consumed (the
	// env hoist happens as its side effect — GOPI_SETTINGS/GOPI_HOME are set
	// before any subcommand or its spawned child resolves config). The env
	// hoist alone was half the fix: the chain below still matched the RAW
	// argv, so a `gopi --settings X task submit "a b c"` reached
	// isTasksSubcommand with args[0]=="--settings" and its ssh-passthrough
	// branch misread the settings PATH as an ssh HOST, word-splitting the
	// quoted payload into spurious positionals ("unexpected positional
	// argument"). Dispatching on the stripped remainder (a) re-enables the
	// whole subcommand surface under `gopi --settings X …` and (b) preserves
	// the user's argv quoting — each remaining element passes through
	// untouched. The agent path below still reads the ORIGINAL args
	// (FlagOverrides handles --settings/--home itself).
	dispatchArgs := config.BootstrapEnvFromArgs(args)

	// §2b THE REPLY-LANGUAGE KNOB IS VALIDATED HERE — BEFORE the subcommand
	// chain, for the reason the chain's own comment gives: every surface that
	// exposes this knob returns from main() at that chain (`gopi -h`,
	// `gopi --help-env`, `gopi config show`, `gopi chat`, a task run) and never
	// reaches the agent prompt path, which was the ONLY caller of the
	// resolution. A declaration the operator wrote was therefore read by
	// NOTHING on those surfaces: measured on the built binary (2026-09-27),
	// `GOPI_OUTPUT_LANGUAGE=chinese gopi -h` and an illegal `--output-language`
	// each exited 0 with ZERO bytes on stderr — the knob's Rule 15 validation
	// was dead code on the real path, and the help page advertising it
	// disagreed with the behaviour of the process that printed the page.
	//
	// WHY A FUNCTION CALL AND NOT A COMMENT: there is no second validator — the
	// call runs the SAME resolution the prompt path runs (Rule 2), so the two
	// surfaces cannot drift into two opinions of what a language tag is. What
	// it adds is the REPORT for the runs no prompt is ever assembled for: an
	// unusable value is named on stderr instead of being silently read as
	// "unset". A correctly configured knob stays silent (the knob's contract —
	// and `gopi config show` reports the value by name for an operator who
	// wants to see it).
	//
	// Placed AFTER BootstrapEnvFromArgs so the `--output-language` flag has
	// already been published into GOPI_OUTPUT_LANGUAGE (that hoist is what
	// makes the flag visible to these very surfaces) and BEFORE the chain
	// below, which is the earliest point that can see every channel and the
	// latest one that is still reached by all of them.
	//
	// FAIL-CLOSED FOR THE TWO PER-INVOCATION CHANNELS (2026-09-27): the report
	// above is a WARNING — measured on master 6936c787, an unusable
	// `--output-language` and an unusable exported GOPI_OUTPUT_LANGUAGE each
	// printed the refusal and then exited 0 having run the subcommand, so a typo
	// and a correct declaration were indistinguishable to any caller. The call
	// below turns exactly those two channels into a non-zero exit (POSIX usage
	// class, `config.OutputLanguagePerInvocationExitCode`) and it MUST precede
	// the chain for the same reason the report does: every surface that would
	// otherwise proceed — `-h`, `--help-env`, `config show`, `chat`, `version`,
	// a task run — returns from main() right here.
	//
	// The SPLIT is the pinned decision, stated in full at the definition
	// (internal/config/output_language.go): an EXPLICIT per-invocation
	// declaration is fatal because the operator who typed it can fix it on the
	// same command line, while the settings.json value stays warn-and-continue
	// (the report above) because it is read by every gopi process on the host —
	// one stale line there must not fail every command on the machine. This
	// call therefore refuses ONLY on the flag and the exported env; a value
	// that merely reached the environment from settings.json is not the
	// operator's command line and keeps the softer policy.
	config.OutputLanguageRefuseUnusablePerInvocation(args)

	config.OutputLanguageReport()

	// §2 A near-miss GOPI_HOME is refused BEFORE any verb dispatch — even a
	// zero-side-effect one. GOPI_HOME is a HOME, not a data root: the documented
	// arithmetic is <GOPI_HOME>/<profile leaf> (config.ProfileDirIn), so a value
	// that ALREADY names a sandbox's profile (`GOPI_HOME=$SB/.gopi` — exactly
	// the path the sandbox help prints for that sandbox) resolves to
	// $SB/.gopi/.gopi: a SECOND, nested profile that no reclaimer models
	// (`SweepSandboxes` sweeps a sandbox ROOT's children, so a profile inside a
	// profile is invisible to it) while the sandbox's own ledger and sessions
	// stay empty. The miss was SILENT until now — 4 stray ledger rows and 4
	// session dirs, noticed by hand during the 2026-09-26 audit.
	//
	// Checked HERE, before the subcommand chain, because this is the position
	// where nothing has been materialised yet: the refusal is provably free of
	// the very directory it forbids, for every verb including the group verbs
	// that return from main() at the chain below. config.GopiDir enforces the
	// same predicate as its own sentinel, so a caller reaching the library
	// directly cannot slip past (Rule 14 — ONE predicate, two enforcement
	// points, never two predicates).
	refuseNestedSandboxProfile()

	// THE INDEX ROWS' TURN COUNTER IS INSTALLED HERE — before the subcommand
	// chain, because the chain is where `gopi session list` (a group verb)
	// returns from main(): a wiring placed later would leave the very verb this
	// serves with no counter. The dependency cannot be an import in the other
	// direction (internal/tools → internal/gopilog cycles through
	// internal/agent), so the binary hands the ONE implementation over —
	// gopilog.SessionTurns, the same function `session log --turns` prints.
	// Wiring + the measured reason: session_turns_wiring.go.
	installSessionTurnsReader()
	// THE OTHER DIRECTION of the same cycle-break, installed on the same two
	// lines: gopilog renders the LEDGER's turn count (`chats.turn_count`) and
	// cannot import internal/tools to read it, so the binary — which imports
	// both — hands over tools.ChatLedgerTurnCount. Without it `session log`'s
	// COST block states that the count was not measured instead of printing it.
	// Wiring + the measured reason: session_turns_wiring.go.
	installLedgerTurnCounter()

	// settings.json / GOPI_TMPDIR / --tmpdir → gopi's OWN environment, before
	// the subcommand chain: the group verbs (`task worker`, `task run`,
	// `merger`) return from main() at that chain and are the biggest child
	// spawners, so the publication must already have happened by then. Every
	// child gopi starts (task-run children, the bash tool, git, the merger's
	// resolve/verify runs) begins from os.Environ(), so this one call gives the
	// whole process tree one temp workspace instead of the ambient /tmp — the
	// 23.5G residue this epic exists for. Silent on success (the value is a
	// parameter, not an announcement); loud with path + reason + channel when
	// the value cannot be provided or written.
	tmpDirBootstrapFromArgs(args)

	// SIGNAL PROVENANCE (epic: all gopi processes killed by signal,
	// 2026-09-13). Every gopi process — worker, merger, task-run child,
	// resolve/verify child — used to die with nothing durable but a terminal
	// line, so three separate investigations of a mass kill ended in an honest
	// "sender undetermined": Linux never tells a victim who signalled it.
	// Installing the recorder HERE, before the subcommand chain, covers every
	// catchable signal for every verb and every mode by construction (the same
	// placement argument as the peak guard). It only APPENDS to one file and
	// only when a signal actually arrives, and it never changes an exit path —
	// installed alongside, never instead of, each verb's own handler.
	//
	// The sessionLog argument is "" ON PURPOSE and it is NOT a dead argument: no
	// run has minted a logger yet at this point in main(), so there is no path to
	// name here — and the recorder does not need one, because the path arrives
	// later through signalrec.SetSessionDir (see printLogPaths, the one place it
	// becomes true). The handler resolves it at SIGNAL time (resolveSessionLog),
	// so a process killed after its logger exists appends its verdict event to
	// that log. This parameter is the live OTHER half of the same handoff: a
	// caller that already knows the path passes it here instead (see Install's
	// own doc — that is what keeps it from being the docs-lie the previous
	// version was).
	uninstallSignalRecord := signalrec.Install("")
	defer uninstallSignalRecord()

	// Subcommands expose individual tools as CLI commands. They consume the
	// full argv — the caller handles exit. Checked before the general -h scan
	// so `gopi tool web search -h` / `gopi session log -h` / `gopi tool
	// code-search -h` prints the subcommand's usage, not the top-level usage.
	//
	// THE REFUSAL IS THE OTHER HALF OF THIS LINE, and it is why the list below
	// is no longer the whole surface (epic verb-retirement, 2026-10-05). A word
	// REMOVED from this chain is not refused here or anywhere — the dispatch
	// reads the verb from argv[0] and an unrecognized word falls through to the
	// bare-prompt path, where it buys a real agent run (a session directory, an
	// `[auto] <word>` ledger row, tokens). Measured 2026-09-30: three `gopi
	// buildinfo` invocations became three failed tasks. Every retired word
	// therefore lives in a refusal table the LAST predicate below walks
	// (cli_verb_aliases.go), so 摘除 and 接入 ship together, word by word —
	// never one without the other.
	if isConfigSubcommand(dispatchArgs) || isNotifySubcommand(dispatchArgs) || isVersionSubcommand(dispatchArgs) || isWorkerSubcommand(dispatchArgs) || isMergerSubcommand(dispatchArgs) || isMemorySubcommand(dispatchArgs) || isMemoryBackupSubcommand(dispatchArgs) || isWebSearchSubcommand(dispatchArgs) || isWebFetchSubcommand(dispatchArgs) || isTasksSubcommand(dispatchArgs) || isChatSubcommand(dispatchArgs) || isWebSubcommand(dispatchArgs) || isUISubcommand(dispatchArgs) || isTermSubcommand(dispatchArgs) || isUIStateSubcommand(dispatchArgs) || isSessionSubcommand(dispatchArgs) || isScreenSubcommand(dispatchArgs) || isScreenReplaySubcommand(dispatchArgs) || isToolSubcommand(dispatchArgs) || isUndispatchedToolWordSubcommand(dispatchArgs) {
		return
	}

	// Top-level help (and the extended env-var table) handled here, before any
	// config resolution. A bare `gopi help` argument is treated like -h/--help
	// (and `help-env` like --help-env) so the classic help words work too.
	//
	// ONE exception, and it is the whole reason this scan asks first: `-h`
	// after the no-verb resume flag is THAT FLAG's query (`gopi --session -h`
	// asks what the flag does, exactly as `gopi session log -h` asks about a
	// subcommand — the chain above already answers that one for the same
	// reason). Left unguarded, this scan would print the top-level page and the
	// flag's own page would be unreachable, which is the "hand-rolled arm
	// swallows the question" defect this repo has paid for before.
	if !sessionResumeFaceArmed(args) {
		for _, a := range args {
			switch a {
			case HelpFlag, HelpFlagLong:
				fmt.Print(usage)
				os.Exit(0)
			case HelpWord:
				// "gopi help" — only when it is the sole/first argument, so a
				// prompt that happens to contain the word "help" is not clobbered.
				if len(args) == 1 || (len(args) > 1 && args[1] == "-h") {
					fmt.Print(usage)
					os.Exit(0)
				}
			case HelpEnvFlagLong:
				fmt.Print(config.FormatEnvTable()) // + a small preamble defined below
				os.Exit(0)
			case HelpEnvWord:
				if len(args) == 1 {
					fmt.Print(config.FormatEnvTable())
					os.Exit(0)
				}
			}
		}
	}

	// Resolve configuration: CLI flags > env vars > settings.json > defaults.
	// We parse flags first (they also tell us the settings path), then load the
	// settings file, then inject both into the process environment so the
	// existing single-source-of-truth parses in internal/agent and internal/tools
	// pick everything up unchanged. Precedence is preserved by ApplyEnv's
	// fromFlags switch (flags always win; settings only fill unset env vars).
	// SESSION WORKDIR bootstrap (epic task-session-cwd): capture the
	// directory this process was STARTED in before any later path can move
	// it (the task-run wrapper chdirs into a task's session directory; a
	// `task run`/resume chdirs into the row's worktree) and export it so the
	// descendants we spawn inherit the origin rather than a moved cwd
	// (tools.TaskWorkdirEnv — one writer, one reader: taskExecutorWorkdir).
	taskWorkdirCapture()
	if taskRunWorkdir != "" {
		if werr := os.Setenv(tools.TaskWorkdirEnv, taskRunWorkdir); werr != nil {
			// Benign (the in-process latch still carries it), never silent.
			fmt.Fprintf(os.Stderr, "[gopi] workdir export: %v\n", werr)
		}
	}

	// THE NO-VERB RESUME FACE (epic resume-surface convergence, step 1):
	// `gopi --session <id> ["<prompt>"]` continues a session from its
	// checkpoint. It sits AFTER the dispatch chain (a verb spelling still
	// wins) and BEFORE the bare-prompt/agent path — the shape it is a sibling
	// of, differing in the id rather than the surface. It adds no predicate to
	// the chain on purpose: a dispatch entry would have to be mirrored in
	// cli_contracts_test.go's predicate list and advertised in the verb census,
	// while a flag costs neither (Rule 3). Setting --help first is what makes
	// BOTH `gopi -h` (top-level page) and `gopi --session -h` (this page) work:
	// the top-level scan above cannot know which one was meant, and the flag
	// path would otherwise resolve a log to answer a usage question.
	if handled, code := runSessionResumeFlag(args); handled {
		os.Exit(code)
	}

	// THE CONFIGURATION RESOLUTION IS ONE FUNCTION, CALLED BY BOTH RUN-SHAPED
	// FACES (epic resume-surface convergence: the config knobs must mean the
	// same thing on the bare-prompt face and on the no-verb `--session` face).
	// The five steps below — flag parse, --home export, settings read, ApplyEnv,
	// temp-workspace publication — moved to resolveRunConfig VERBATIM, comments
	// and all; their WHY lives there, not here, so the two faces cannot drift
	// into two precedences (Rule 2).
	base, overrides, err := resolveRunConfig(args)
	if err != nil {
		reportRunConfigError(err)
		os.Exit(1)
	}

	// §1 A verb after a run flag is NOT dispatched — refuse instead of running
	// it as a prompt. The dispatch chain above reads the verb from the FIRST
	// argument only (`gopi version`); the run path used to accept ANY leftover
	// positional as the prompt, so `gopi --timeout 3 version` silently started
	// a real agent run — a session dir, a live `[auto] version` ledger row and
	// real tokens — for a word the operator meant as a COMMAND (task
	// 01a0dd02-d0a2, 2026-09-26). The classification (flag-aware positionals
	// vs. the dispatched verb vocabulary) lives in internal/config because the
	// argument slice that decides what the prompt IS only exists there; the
	// vocabulary is pinned against this package's dispatch surface by
	// cli_verb_guard_test.go. An explicit prompt (`--prompt version` /
	// GOPI_PROMPT / settings "prompt") is the deliberate way to say the word
	// literally and is NOT refused.
	//
	// The prompt consulted is the one IN FORCE for THIS invocation, which is
	// why config.LoadMerged runs here FIRST — before ApplyEnv publishes
	// anything into the process environment. Ask after the publication and the
	// question stops being answerable: ApplyEnv writes the config's own prompt
	// into GOPI_PROMPT, indistinguishable from an ambient var inherited from a
	// probe or a parent process (task 01a0de86, 2026-09-26: a leftover
	// GOPI_PROMPT from `--gopi-prompt 1 version` disarmed this refusal for
	// every later invocation, so the run started with prompt "1" and swallowed
	// the `version` it never refused). A malformed flag line is already fatal
	// a few lines above; nothing else can fail here.
	verbCfg, _ := config.LoadMerged(args) //stylegate:ignore a malformed flag line is already fatal a few lines above, and nothing else can fail here
	if verb := config.PositionalCommandVerb(verbCfg); verb != "" {
		fmt.Fprintf(os.Stderr, "gopi: %v\n", config.PositionalCommandVerbRefusal(verbCfg, verb))
		os.Exit(1)
	}

	// §1b A word that is NEITHER a dispatched verb NOR an explicit prompt is
	// refused too, when it is a KNOWN NEAR-MISS of this binary's own surface
	// (`gopi revision`, `gopi status`, `gopi branch list`, `gopi settings
	// show`, `gopi scratch dir` — task 01a0f72b, 2026-10-01). The guard above
	// answers a word it RECOGNISES; this one answers a word the operator
	// believed was a command, which the run path would otherwise spend as a
	// prompt: measured on master, `gopi revision </dev/null` started the
	// interactive console and REGISTERED an `[auto] revision` ledger row —
	// 123 such rows in the 2026-10-01 failed-row audit. The vocabulary is
	// CLOSED (internal/config's commandHintWords) and pinned to this package's
	// dispatch surface by cli_verb_hint_guard_test.go, so ordinary prompt text
	// (`gopi frobnicate`, `gopi summarize the docs`) still runs.
	//
	// `gopi revision -h` IS UNREACHABLE HERE and stays legal — the help scan
	// below matches any `-h` and exits 0 with the top-level page, which is what
	// that query meant. This call answers the shapes the run path would
	// actually spend: the bare word, a near-miss followed by one word, and a
	// near-miss under a run flag (`gopi --timeout 3 revision`). Its message
	// points at `gopi -h` and, when the request has a real command behind it,
	// at that command's `-h`.
	if hint := config.PositionalCommandHintRefusal(verbCfg); hint != "" {
		fmt.Fprintln(os.Stderr, hint)
		os.Exit(1)
	}

	// §1c THE BARE-PROMPT FACE ITSELF is guarded here — the LAST positional
	// screen before money is spent, and the one neither refusal above reaches:
	// they answer words this binary RECOGNISES (a dispatched verb, a near-miss),
	// while an unrecognised word falls through to the bare-prompt path and buys
	// a REAL agent run (a session directory, a ledger row marked `[auto] <word>`,
	// a clone ground under the SHARED workspace root, tokens). That is correct
	// for a person typing a prompt — the documented usage of `gopi "<prompt>"` —
	// and catastrophic for an AGENT, which then recurses with no depth limit
	// (agent → gopi <word> → agent → …). Measured on this host (row 01a1ac14,
	// 2026-11-05): a worker's agent ran `gopi submit-x`, two such cascaded ghosts
	// cost 440,153 + 855,807 prompt tokens, and every layer "investigated" the
	// bare word by running it again.
	//
	// PLACEMENT IS THE WHOLE COST MODEL: it sits AFTER both positional refusals
	// (so a recognised word still gets its own, more useful message — naming the
	// command that answers it) and BEFORE readInput/newAgent, so a refusal has
	// created NOTHING and spent NOTHING. The acceptance test reads that back from
	// the ledger and the workspace root.
	//
	// IT IS A NO-OP FOR A HUMAN: config.GuardBarePrompt asks the same signature
	// the worker/task-run guard asks, so an operator's terminal (and every
	// bare-prompt test fixture, which declares HumanTerminalEnv) runs untouched.
	// `--prompt X` / the settings "prompt" key never reach it at all — the
	// positionals are input FILES then, which is why the screen consults
	// config.BarePromptWord rather than argv directly (Rule 2).
	if word := config.BarePromptWord(verbCfg); word != "" {
		guardBarePromptOrExit(word)
	}

	// The positionals come from FlagOverrides (flag-value-aware — a flag's
	// value like `--model deepseek-v4-flash-vision-exp` can never be mistaken
	// for an input file). A positional is either a literal prompt, a text
	// file whose contents become the prompt, or an image file that is
	// attached natively (vision model sees the pixels directly).
	input, err := readInput(overrides)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if input.prompt == "" && len(input.images) == 0 {
		fmt.Fprintf(os.Stderr, "No input provided\n")
		os.Exit(1)
	}

	// Native image input: the vision model runs the whole loop. The user's
	// explicit --model always wins; otherwise an image input auto-switches to
	// the vision model (image tokens = text tokens — the model sees the
	// pixels directly, no OCR tool round-trip). Resolved BEFORE the agent is
	// constructed so the session_start event records the model that actually
	// goes on the wire (the banner `gopi session log --chat` prints).
	modelOverride := ""
	if len(input.images) > 0 && overrides.Model == "" {
		modelOverride = deepseek.ResolveVisionModel(cfgForVision())
		fmt.Fprintf(os.Stderr, "[gopi] image input detected → using vision model %s\n", modelOverride)
	}

	ag, err := newAgent(modelOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating agent: %v\n", err)
		os.Exit(1)
	}
	// Crash-dump epic ②: orphan-session detection — a previous run that died
	// without a session_end (kill -9, panic before recover, power loss) gets
	// a backfilled <session-id>.dump.json so the death is visible to PA and
	// resumable agents, and a dump is RETRACTED once the session ends
	// cleanly. The classification requires the writer process to be provably
	// GONE and no session_end to exist; a live or unclassifiable session
	// prints nothing (Rule 15 — an unsupported crash count is worse than no
	// line at all). Sessions still being written (the CURRENT one included)
	// are not orphans. Best-effort.
	//
	// GATED (crash-dump-startup-backfill): the sweep walks EVERY session
	// under the data root (~29k entries → ~1.5-2 s and ~450 MB of
	// allocation churn per start, measured on the reference host 2026-09-12).
	// The process used to pay that before doing any work at all. It is a
	// reconciler whose product (a dump for a session that died, or the
	// retraction of one for a session that ended) is worth just as much
	// written tomorrow, so it runs at most once per UTC day
	// (agent.OrphanSweepDue, stamp recorded only after a completed sweep);
	// GOPI_CRASH_DUMP_SWEEP=force runs it now, =off skips it, and nothing at
	// all is walked while today's stamp is current.
	if due, reason := agent.OrphanSweepDue(time.Now()); due {
		written, retracted := agent.BackfillOrphanCrashDumps(sessionIDOf(ag))
		switch {
		case written > 0 || retracted > 0:
			fmt.Fprintf(os.Stderr, "[gopi] crash-dump: %d session(s) died without a clean exit — dump(s) written under %s; %d stale dump(s) retracted\n",
				written, sessionDumpHint(), retracted)
		case reason == agent.CrashDumpSweepReasonForced:
			fmt.Fprintf(os.Stderr, "[gopi] crash-dump: %s, no new orphan dumps\n", reason)
		}
		if err := agent.MarkOrphanSweepDone(time.Now()); err != nil {
			// Benign (the next start sweeps again) but never silent (Rule 15).
			fmt.Fprintf(os.Stderr, "[gopi] crash-dump: sweep stamp not recorded (%v) — the next start will sweep again\n", err)
		}
	}
	printLogPaths(ag)
	// THE CONFIGURATION THIS RUN IS IN FORCE WITH, INTO ITS OWN RECORD, when
	// GOPI_DEBUG_CONFIG says so (debug_config_dump.go). Placed HERE, and the
	// position is the requirement: `ag` exists (so the `agent.*` rows are the
	// ones the request will consume), the session directory is already true
	// (printLogPaths is what makes it so), and RunPrompt below has not yet put
	// anything on the wire. Rendered by the SAME builder and renderer `gopi
	// /config` uses, so the log and the REPL cannot disagree.
	writeDebugConfigDumpToStderr(ag)
	// FRONT-RUN CONSOLE LOG (epic task-session-cwd): a front run cannot chdir
	// into its session directory (its cwd IS the operator's working context —
	// GOPI.md, the search roots and the relative-path tools all read it), so
	// its console output is COPIED there instead, from here to the end of the
	// run (session_console_log.go). Attached only when it can be (a piped,
	// non-interactive front run with session logging on); a `task run` child
	// and an interactive terminal are decided refusals, each documented
	// there. Restored by the deferred call so the closing lines still reach
	// the operator.
	sessionLogPath := ""
	if ag.SessionLog != nil {
		sessionLogPath = ag.SessionLog.Path()
	}
	consoleLogAttachFor(sessionLogPath)
	defer consoleLogRestore()
	if line := config.NodeBanner(string(base.Role), base.SyncTo); line != "" {
		fmt.Fprint(os.Stderr, line)
	}

	// chat-once/bare-prompt SIGTERM zombie fix (cli_web bug003 pattern,
	// extended to the bare-prompt path): a SIGINT/SIGTERM here used to kill
	// the process outright (Go's default disposition) — the runEngine defer
	// that writes session_end + closes the auto task-ledger row never ran,
	// leaving an in_progress zombie with task text done but no session_end
	// (t-1788514086/1788514159/1788514437 evidence). Notify + Agent.Interrupt()
	// turns the signal into a graceful ctx cancellation: the run loop unwinds
	// through its normal end-of-run defer (session_end exit_code=1 + auto row
	// failed) instead of dying mid-defer. SIGKILL remains uncatchable, and
	// since 2026-10-26 NOTHING reconciles the row it strands: the stale
	// report names it and it stays open for a human (waitpid semantics are why
	// the row cannot be closed automatically — devdoc/zombie-sweep-removed.md).
	sigCh := make(chan os.Signal, 1)
	// THE SET COMES FROM THE RECORDER (epic 01a15bca). It used to be
	// `os.Interrupt, syscall.SIGTERM` — and SIGHUP, which signalrec's
	// watchedSignals ALSO registers (i.e. the kernel's default death for it is
	// already gone), was therefore recorded and then dropped in silence: this
	// arm never saw it, and the recorder declines to re-raise for every mode the
	// argv table declares handler-owning — which includes "serve" and every
	// unclassifiable bare prompt that reaches this line. One definition
	// (signalrec.GracefulStopSignals), so a mode here cannot drift from the set
	// the recorder took on the mode's behalf.
	signal.Notify(sigCh, signalrec.GracefulStopSignals()...)
	// The recorder (signalrec.Install, early in main) catches SIGTERM too. The
	// claim tells it THIS process owns the graceful path: without it the
	// recorder's argv table cannot classify a bare prompt (argv[1] IS the prompt
	// text), so it re-raises SIGTERM with the default disposition one grace
	// later -- signal.Reset clears the disposition globally, i.e. it disarms the
	// registration two lines above and kills the process mid-unwind, before
	// runEngine's defer can write session_end (epic 0x01a0d65f).
	signalrec.ClaimSignalHandlers()
	defer signal.Stop(sigCh)
	go func() {
		if sig, ok := <-sigCh; ok {
			fmt.Fprintf(os.Stderr, "\n[gopi] %v received — finalizing session…\n", sig)
			// DECLARE THE CAUSE BEFORE THE UNWIND, the same hand-off the chat
			// carrier makes (cmd/agent/cli_chat.go) and for the same measured
			// reason (session ab9e0023): the end-of-run collector has an arm only
			// for a RUN ERROR, so a handled signal would otherwise be recorded as
			// `category: unknown` — a terminal teardown filed under "something
			// unrecognised happened". The spelling is signalrec's (Rule 14).
			ag.CloseSignal = signalrec.SignalName(sig)
			ag.Interrupt()
		}
	}()

	// THE PLAIN PROMPT REACHES THE MODEL VERBATIM (RP-1, end-user-audit slice
	// 8): `gopi [options] "<prompt>"` is documented as "Run one task from a
	// prompt argument" with `"<prompt>"  The task text` — so the first user
	// message must BE that text. RunPrompt (not Run) selects the unframed
	// first turn; the task surfaces (a dispatched row) keep the
	// [EPIC GOAL]/task-list/reframe framing that is their contract.
	result, err := ag.RunPrompt(input.prompt, imageSources(input.images)...)
	if err != nil {
		// The same settle before this door leaves: an interrupted turn is
		// exactly the death whose record must not be lost.
		settleProvenanceRecord(roleLabelMainPrompt)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(result)

	// THE GROUND THIS RUN TOOK, on the operator's own stderr (epic
	// raw-prompt-clone-channel): with the clone channel ON the bare-prompt face
	// works in a clone workspace rather than a linked worktree, and that
	// workspace is KEPT (its delivery leg is the channel's close-out, which
	// this face does not run). An operator must be able to read where the work
	// landed without parsing the log — and a silent ground is how a run's
	// commits become unfindable. Empty pair ⇒ the worktree channel ⇒ no line
	// (byte-identical behaviour, the compatibility contract).
	// The line reports the GROUND, not the delivery: the delivery LEG
	// (internal/agent/clonedelivery.go) runs inside the end hook and has already
	// printed its own three-state verdict above, so this line must not claim the
	// workspace is undelivered when the push just landed.
	if workspace, branch := ag.CloneRunGround(); workspace != "" {
		fmt.Fprintf(os.Stderr, "[gopi] clone channel: this run worked in %s on branch %s — the workspace is KEPT (reclaimed by nobody); the delivery verdict is the end hook's line above\n", workspace, branch)
	}

	// M3 trigger: after a successful one-shot session, opportunistically
	// brew session logs into the knowledge base (throttled to once per
	// interval; errors are stderr noise — never fail the session for it).
	maybeBrewOnSessionExit()
	// THIS FACE CLAIMS THE RECORDER'S HANDLERS (signalrec.ClaimSignalHandlers,
	// above), so it owes the same settle every claiming door owes: the session is
	// closed and the answer is printed, while the recorder's record is filed by
	// its OWN goroutine — and `return` from main does not wait for it (Rule 2:
	// ONE waiter, in internal/signalrec).
	settleProvenanceRecord(roleLabelMainPrompt)
}

// agentInput is the resolved task input for one gopi run: the text prompt
// plus any image files to attach natively. When images are present the model
// is switched to the vision model and each image travels as an image content
// block in the first user message (image tokens processed natively — no OCR
// round-trip).
type agentInput struct {
	prompt string
	images []*tools.ImageFile
}

// Payload-from-file syntax: a positional (or --prompt value) starting with
// "@" is a file path whose contents become the prompt text. This lets callers
// submit payloads that are impractical on the command line — text with shell
// special characters, quotes, JSON/YAML formats, multi-line documents —
// without any quoting or escaping, e.g.:
//
//	gopi @task.txt
//	gopi --prompt @payload.json
//	gopi "@$(pwd)/plan.md"
//
// Note: "@-" reads the payload from stdin, so `cat payload.json | gopi @-`
// works even when stdin would otherwise be ignored because a positional was
// present. A literal prompt that merely starts with "@" (no such file)
// fails with a clear error rather than being sent as prompt text —
// escape it as "\@..." (the backslash is stripped).
const payloadFilePrefix = "@"
const payloadStdinPath = "-"

// How positional text and an explicit --prompt are combined into ONE task
// prompt. `textJoiner` separates the texts of several positionals (the
// pre-existing behaviour — one positional per line). `promptTextJoiner`
// separates the --prompt instruction from the text carried by the positionals
// beside it: a blank line, so a document appended to an instruction stays
// readable as a document, and `positionalTextBullet` marks such an appended
// block as positional input rather than a continuation of the instruction.
//
// These live here rather than inline because the JOIN is now part of the
// documented contract (internal/config/help.go's --prompt row) — a bare
// literal would let the two drift (Rule 1b/14).
const (
	textJoiner           = "\n"
	promptTextJoiner     = "\n\n"
	positionalTextBullet = "- "
)

// readInput resolves the task input from the parsed CLI overrides:
//
//   - Prompt: --prompt/GOPI_PROMPT wins as the instruction; otherwise the
//     first positional that is NOT a readable file (or a text file, whose
//     contents become the prompt); otherwise stdin (piped only — a TTY
//     prints usage). With --prompt given, the positionals are still task
//     input: images attach natively and the text of file positionals is
//     appended to the instruction (joinPromptAndTexts) — never dropped.
//   - Images: every positional that is an existing image file (jpg/jpeg/png/
//     gif/webp) is loaded and attached natively to the run.
//   - Text files: a positional that is an existing non-image file is read;
//     its contents become (or contribute to) the prompt.
//
// A positional is classified as: literal prompt text (no file exists), text
// file, or image file. Because FlagOverrides already strips flag values, a
// value like `--model deepseek-v4-flash-vision-exp` can never leak in here.
func readInput(overrides *config.Config) (*agentInput, error) {
	// GOPI_PROMPT env channel (P1-3): the var is catalogued + injected by
	// ApplyEnv (from the settings "prompt" key or --prompt) but was never
	// read — the env channel was dead. GOPI_PROMPT behaves exactly like
	// --prompt (same precedence position: flag > env > settings >
	// positional/stdin). ApplyEnv already ran in main(), so this ONE
	// os.Getenv covers the shell env AND the settings-injected value; an
	// explicit --prompt still wins because it already sits in
	// overrides.Prompt. The @payload file-ref syntax is honoured too
	// (same as --prompt, which expands in FlagOverrides).
	if strings.TrimSpace(overrides.Prompt) == "" {
		// The prompt is read from the environment, OR — when the value is too
		// large for the kernel to carry — from the in-process carrier ApplyEnv
		// filled instead of publishing it (config.publishPrompt). Both spellings
		// are the SAME channel, which is why the fallback lives here and not at
		// every reader: an oversized prompt must still reach the run even though
		// it is deliberately absent from the environment (Rule 15 — the skip is
		// announced on stderr with its byte count and is never a silent drop).
		v := os.Getenv(config.PromptEnvVar)
		if v == "" {
			v = config.ProcessOnlyPrompt()
		}
		if v != "" {
			if strings.HasPrefix(v, payloadFilePrefix) && !strings.HasPrefix(v, literalPromptEscape) {
				text, err := readPayloadFile(v)
				if err != nil {
					return nil, err
				}
				overrides.Prompt = strings.TrimSpace(text)
			} else {
				overrides.Prompt = v
			}
		}
	}
	positionals := overrides.Positionals
	// An explicit --prompt/GOPI_PROMPT means every positional is a FILE
	// argument, never the task text (`gopi --prompt TEXT a.png`, help Usage
	// line 4). The RP-2 refusal below is scoped to the token that would
	// otherwise BECOME the task text, because that is the audited defect (the
	// path STRING reaching the model as the prompt): with an explicit --prompt
	// a non-image positional keeps its pre-existing meaning — dropped as an
	// attachment argument that cannot be attached. That silent drop is the
	// SEPARATE finding RP-3 and is deliberately not widened into this fix
	// (measured: `--prompt frobnicate /tmp/fx/nope.txt` is rc=0 both before and
	// after, so no previously-working invocation changes its verdict).
	explicitPrompt := strings.TrimSpace(overrides.Prompt) != ""
	if len(positionals) > 0 {
		// If the first positional is a literal prompt (not a readable file)
		// AND no --prompt was given, it is the task instruction.
		in := &agentInput{}
		var texts []string
		for _, p := range positionals {
			// @prefix → payload from file (or stdin with @-). Handled before
			// the stat classification because "@task.txt" as a literal path
			// would otherwise hit the text-file branch only by luck, and a
			// non-existent path must error loudly, not become prompt text.
			if strings.HasPrefix(p, payloadFilePrefix) {
				text, err := readPayloadFile(p)
				if err != nil {
					return nil, err
				}
				texts = append(texts, text)
				continue
			}
			// Escaped literal: `\@x` is the deliberate way to send a prompt
			// that starts with the payload prefix. The SAME escape
			// readPayloadFile honours is applied HERE, on the positional path,
			// because the RP-2 refusal (inputpath.go) advises it: the advice
			// must be a spelling that works.
			if strings.HasPrefix(p, literalPromptEscape) {
				texts = append(texts, strings.TrimPrefix(p, escapeBackslash))
				continue
			}
			// Classification: an EXISTING file is an image (by extension) or
			// a text input file; anything that is not a readable file is
			// literal prompt text — even if it happens to end in ".png"
			// (e.g. prose mentioning an image path). Stat first: a prompt
			// that merely references an image must never be treated as one.
			if _, err := os.Stat(p); err == nil {
				if tools.IsImagePath(p) {
					img, err := tools.LoadImage(p)
					if err != nil {
						return nil, fmt.Errorf("%w", err)
					}
					in.images = append(in.images, img)
					continue
				}
				data, err := os.ReadFile(p)
				if err != nil {
					return nil, fmt.Errorf("Error reading input file: %w", err)
				}
				// TEXT CONTRACT (epic binary-summary-guard): a file positional
				// is read as the prompt, so a BINARY here is never the
				// operator's intent — reading an executable as the prompt is
				// how 351 bytes of ELF became a ledger task summary and cost a
				// real run. Fail fast, name the path and the offending byte,
				// and point at the two deliberate overrides (a literal prompt,
				// or the @-prefix payload form, which is explicit about
				// reading a file).
				if reason, offset := nonTextFileReason(data); reason != "" {
					return nil, fmt.Errorf("input file %q %s at byte %d — it is not prompt text; pass the prompt as an argument, or force the file with @%s if you really mean its bytes", p, reason, offset, p)
				}
				texts = append(texts, strings.TrimSpace(string(data)))
				continue
			}
			// Not a readable file. A PATH-SHAPED token that names nothing is
			// refused BY NAME when it would have become the task text —
			// before any session dir or ledger row — exactly like its `@PATH`
			// twin (inputpath.go carries the full RP-2 finding, the
			// classification and the message): the path STRING reaching the
			// model as the prompt is the audited defect.
			//
			// With an explicit --prompt the token cannot become the task text
			// (the instruction is already set), so the refusal would instead
			// break the spelling it advises: `--prompt TEXT ./nope.txt` must
			// RUN, not refuse with the message that tells the operator to do
			// exactly that. The token then contributes NOTHING rather than its
			// own path string — appending the path is the same RP-2 defect in
			// the one place the refusal's advice sends the operator.
			if why := pathShapedReason(p); why != "" {
				if explicitPrompt {
					continue
				}
				return nil, missingPathPositional(p, why)
			}
			texts = append(texts, p)
		}
		// Prompt precedence: --prompt/GOPI_PROMPT > positional text. The
		// positionals are NOT discarded when --prompt is present — they
		// still carry task input (an attached image, or a file whose text
		// belongs in the task), and dropping them was the RP-3 defect: with
		// `--prompt TEXT file.txt` the file was read and then thrown away,
		// with nothing on stderr. The text positionals are appended to the
		// instruction, marked as positional input so the boundary between
		// the instruction and the appended document is visible (see
		// promptTextJoiner / positionalTextBullet).
		if p := strings.TrimSpace(overrides.Prompt); p != "" {
			in.prompt = joinPromptAndTexts(p, texts)
		} else {
			in.prompt = strings.TrimSpace(strings.Join(texts, textJoiner))
		}
		return in, nil
	}
	// No positionals: --prompt wins; otherwise read from stdin — but only if
	// it's not a TTY (i.e. piped input).
	if p := strings.TrimSpace(overrides.Prompt); p != "" {
		// --prompt also honours the @prefix payload-from-file syntax.
		if strings.HasPrefix(p, payloadFilePrefix) && !strings.HasPrefix(p, literalPromptEscape) {
			text, err := readPayloadFile(p)
			if err != nil {
				return nil, err
			}
			return &agentInput{prompt: strings.TrimSpace(text)}, nil
		}
		return &agentInput{prompt: p}, nil
	}
	if isTerminal(os.Stdin) {
		fmt.Print(usage)
		os.Exit(1)
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("Error reading stdin: %w", err)
	}
	return &agentInput{prompt: strings.TrimSpace(string(data))}, nil
}

// joinPromptAndTexts combines an explicit instruction (--prompt/GOPI_PROMPT)
// with the text carried by the positionals beside it.
//
// The rule the CLI documents (internal/config/help.go, "Inputs"): with
// --prompt given, the positionals are still task input — an image is
// attached natively and a non-image file contributes its text. Only the
// POSITIONAL-as-prompt precedence changes: it is --prompt that states the
// instruction, and positional text is what the instruction is about.
//
// Each text is bulleted onto its own line so that a multi-line document
// appended to an instruction stays a document (its own lines are never
// folded into the instruction's paragraph) and the source of each block is
// visible in the request body. Empty positionals contribute nothing.
func joinPromptAndTexts(prompt string, texts []string) string {
	appended := make([]string, 0, len(texts))
	for _, t := range texts {
		if strings.TrimSpace(t) == "" {
			continue
		}
		appended = append(appended, positionalTextBullet+strings.ReplaceAll(t, textJoiner, textJoiner+positionalTextBullet))
	}
	if len(appended) == 0 {
		return prompt
	}
	return prompt + promptTextJoiner + strings.Join(appended, textJoiner)
}

// readPayloadFile resolves a "@..." payload argument into prompt text.
//
//   - "@-" reads stdin (piped). Unlike the no-arg path, stdin is read even
//     when positionals exist, so `cat payload.json | gopi @-` works.
//   - "@<path>" reads the file verbatim — bytes are NOT trimmed of leading
//     whitespace so indented formats (YAML, JSON, code) survive intact;
//     only the trailing newline is stripped.
//   - "\@<path>" is the escape hatch: the leading backslash is stripped and
//     the rest becomes literal prompt text (caller wants to talk ABOUT a
//     file, not include it).
//
// A missing/unreadable file is a hard error (never silently prompt text):
// the caller explicitly asked for a payload — sending an empty or wrong
// prompt to the LLM would be worse than failing fast.
func readPayloadFile(arg string) (string, error) {
	body := strings.TrimPrefix(arg, payloadFilePrefix)
	if body == payloadStdinPath {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("Error reading stdin payload: %w", err)
		}
		return strings.TrimRight(string(data), "\n"), nil
	}
	if strings.HasPrefix(arg, literalPromptEscape) {
		// Escaped literal: strip the backslash, keep the rest as text.
		return strings.TrimPrefix(arg, escapeBackslash), nil
	}
	data, err := os.ReadFile(body)
	if err != nil {
		// The %s verbs render the prefix as TEXT: it is a string, not an error, so
		// there is nothing to wrap and %v would only look like a broken chain to
		// the Rule 16 gate (whose predicate is per-CALL: any %v beside an
		// error-shaped argument is reported, however the %v is used).
		return "", fmt.Errorf("Error reading @ payload file %q: %w (escape a literal prompt starting with %s as \\%s)", body, err, payloadFilePrefix, payloadFilePrefix)
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// newAgent resolves the model (DEEPSEEK_MODEL env > settings.json model >
// default; forcedModel, when non-empty, wins over all of them — the vision
// auto-switch path uses this) and creates the agent. Env vars were already
// seeded from flags + settings by main().
func newAgent(forcedModel string) (*agent.Agent, error) {
	cfg, cfgErr := deepseek.LoadConfig(deepseek.SettingsPath())
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "[gopi] config: %v\n", cfgErr)
	}
	model := deepseek.ResolveModel(cfg)
	if forcedModel != "" {
		model = forcedModel
	}
	return agent.NewAgent(model)
}

// newAgentResuming is newAgent for a run that CONTINUES an existing session:
// same model resolution, and the identity is handed to agent.NewAgentResuming
// instead of being minted (epic: `gopi chat --resume <id>` minted a second
// session). It exists rather than a parameter on newAgent because newAgent is
// the seam every OTHER face calls (term, ui, merge handover) and none of them
// may be made to answer the resume question — Rule 14's one-question-one-answer
// applies to a constructor signature as much as to a constant.
func newAgentResuming(forcedModel string, id agent.RunIdentity) (*agent.Agent, error) {
	cfg, cfgErr := deepseek.LoadConfig(deepseek.SettingsPath())
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "[gopi] config: %v\n", cfgErr)
	}
	model := deepseek.ResolveModel(cfg)
	if forcedModel != "" {
		model = forcedModel
	}
	return agent.NewAgentResuming(model, id)
}

// cfgForVision loads the config for vision-model resolution. Env vars were
// already seeded by main() (GOPI_VISION_MODEL etc.), so ResolveVisionModel
// sees them regardless; the settings file only matters when nothing is set
// in the environment.
func cfgForVision() *deepseek.Config {
	cfg, err := deepseek.LoadConfig(deepseek.SettingsPath())
	if err != nil {
		return nil
	}
	return cfg
}

// imageSources converts loaded images to the wire image blocks the agent
// attaches to the first user message.
func imageSources(images []*tools.ImageFile) []*deepseek.ClaudeImageSource {
	srcs := make([]*deepseek.ClaudeImageSource, 0, len(images))
	for _, img := range images {
		srcs = append(srcs, &deepseek.ClaudeImageSource{
			Type:      "base64",
			MediaType: img.MediaType,
			Data:      img.Base64,
		})
	}
	return srcs
}

// printLogPaths reports the raw log and session log locations on stderr.
func printLogPaths(ag *agent.Agent) {
	printLogPathsQuiet(ag, false)
}

// printLogPathsQuiet is printLogPaths with the HUMAN-FACING lines suppressed
// when quiet is true. Only the printing is gated: the side effects below
// (signal provenance's session dir, the report-contract ownership probe) are
// setup, not chatter, and must run exactly the same either way — a UI that
// hides its plumbing must not thereby change what the plumbing does.
//
// The interactive chat REPL passes quiet=true: its banner above the prompt is
// one line, and a session UUID the user did not ask for is not part of it
// (--verbose restores the full listing).
func printLogPathsQuiet(ag *agent.Agent, quiet bool) {
	// bug041: print the node identity FIRST — Bond's "Wait, which node am
	// I?" must be answered by the banner before any reasoning starts. One
	// line on stderr; empty (no line) when the role is undeclared.
	if line := config.NodeIdentityLine(); line != "" && !quiet {
		fmt.Fprintf(os.Stderr, "[gopi] %s\n", line)
	}
	// The task-sync switch rides the same banner and is NEVER suppressed by an
	// undeclared role: the identity line's empty-string convention encodes
	// "nothing is declared", but this axis has no such state — off is a
	// DECISION (the default), and a node whose rows stop crossing the network
	// must be able to see why on the first screen it prints (Rule 15).
	if !quiet {
		fmt.Fprintf(os.Stderr, "[gopi] %s\n", config.TaskSyncLine())
	}
	if ag.RawLog != nil && !quiet {
		fmt.Fprintf(os.Stderr, "[gopi] raw log: %s\n", ag.RawLog.Path())
	}
	if ag.SessionLog != nil {
		if !quiet {
			fmt.Fprintf(os.Stderr, "[gopi] session: %s\n", ag.SessionLog.Path())
		}
		// signal provenance (epic signalrec-session-attribution, 2026-09-14):
		// hand the recorder THIS run's session dir AND ITS OWN session log, so a
		// signal that arrives from here on files its record beside session.jsonl
		// / *.raw.log / task-report.md instead of in the retired public logs
		// dir, and appends its verdict event INTO the log the operator reads
		// afterwards. Rule 2: one writer (here, the one place the session dir
		// and the session-log path become true), one reader
		// (signalrec.sessionDest / resolveSessionLog). Both values come from the
		// same handle in one statement — the log path is never joined or
		// re-derived from the dir, so the witness cannot land beside the session
		// it describes. An empty dir on a later call CLEARS both: a second run
		// in the same process must not inherit the first run's directory, nor
		// append its verdict to the first run's log.
		signalrec.SetSessionDir(sessionlayout.RecordContainer(ag.SessionLog.Path()), ag.SessionLog.TaskID(), ag.SessionLog.Path())
		// REPORT-CONTRACT OWNERSHIP (epic nested-report-path-clobber,
		// 2026-09-15). This is the moment the session's own directory becomes
		// TRUE — so it is the moment the question "is $GOPI_TASK_REPORT_PATH
		// mine?" can be answered. A gopi launched from an agent session's shell
		// inherits the parent's report path and, without this guard, reads it as
		// its own destination and overwrites the parent's task-report.md
		// (measured: session 01a0a4ee's deliverable replaced by a child probe's
		// report). The destination must live in THIS session's directory, or it
		// is refused — loudly (Rule 15: never a silent obedience to a contract
		// that is not ours). See internal/agent/journalreport.go for the full
		// three-face mechanism and why the run id alone is NOT the answer.
		// The guard asks TWO questions (see internal/agent/journalreport.go):
		// is the destination OURS, and is it USABLE. A destination whose parent
		// directory is missing — the inherited-from-another-channel spelling
		// measured on 2026-10-28 — used to pass the ownership test and enter
		// this run's context as a fact, so the contract's own write died on
		// ENOENT far from the cause. The refusal names the path and the reason,
		// and the names leave the environment: a destination that cannot be
		// written is not a fact about this run.
		if refused := agent.SanitizeReportContract(sessionlayout.RecordContainer(ag.SessionLog.Path()), func(reason string) {
			fmt.Fprintf(os.Stderr, "[gopi] %s\n", reason)
		}); len(refused) > 0 {
			fmt.Fprintf(os.Stderr, "[gopi] the inherited report contract was removed from this process's environment (%v) — no report will be written to a destination this run does not own and cannot use\n",
				refused)
		}
		// CONSOLE PROVENANCE, READ BEFORE THIS PROCESS OVERWRITES THE
		// EVIDENCE (chats-table source column, measured 2026-10-03). The two
		// exports below — the executor session handle and the anchor — are
		// unconditional, and each one DESTROYS what the chats table needs: the
		// handle makes this process look like an agent session, and the anchor
		// makes it look like the session itself. Both assertions are TRUE for
		// the guard (this process IS the session) and both are what a chat row
		// written minutes later would misread as "no agent above me". So the
		// reading is taken HERE, while the environment still describes who
		// launched this process, and the chat row answers with that snapshot
		// (agent.CaptureConsoleProvenance is idempotent; an interactive
		// `gopi chat` reaches this same code, so its console is captured once).
		agent.CaptureConsoleProvenance()
		// task-submit-model epic ②: export the session id so the parent
		// `task run`'s writeback can resolve the EXECUTING session's model
		// (session_start event) and backfill it into the ledger row. Rule
		// 2: the env var has exactly one writer (here) and one reader
		// (tools.TaskUpdateWithResult's model resolution).
		if err := os.Setenv(tools.TaskExecutorSessionEnv, ag.SessionLog.SessionID()); err != nil {
			fmt.Fprintf(os.Stderr, "[gopi] session id export: %v\n", err)
		}
		// Agent-spawn guard anchor (2026-09-12): this process IS a gopi agent
		// session, so it exports the marker with its OWN pid — a child that
		// inherits it can tell an agent sits above it, and a runner that
		// re-executed an agent sees the marker as its own. See
		// internal/config/agentguard.go.
		exportAgentSessionAnchor()
	}
}

// sessionIDOf returns the agent's session id, or "" when no session exists
// (backfill then scans everything — the no-session caller case).
func sessionIDOf(ag *agent.Agent) string {
	if ag.SessionLog == nil {
		return ""
	}
	return ag.SessionLog.SessionID()
}

// sessionDumpHint is the operator-facing pointer printed with a truthful
// crash-backfill count: WHERE the dumps landed, so the line is actionable
// rather than a bare number (the old line said "backfilled N orphan session
// dump(s)" and named no location). Resolved best-effort — a gopi-dir lookup
// failure must not fail the startup line.
func sessionDumpHint() string {
	dir, err := config.SessionsRoot()
	if err != nil {
		return "the gopi sessions directory"
	}
	return dir
}

// isTerminal returns true if f is attached to an interactive terminal.
// Uses os.ModeCharDevice as a heuristic: pipes and regular files are not
// character devices, while TTYs are.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// The worker page's mirror of the session page's cross-guidance (Rules 13/14):
// the command comes from internal/cliverbs, the ONE owner of the spelling.
const workerResumeSessionAddressSpelled = "`" + cliverbs.ResumeSessionAddress + "`"
