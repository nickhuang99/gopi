package main

// cli_worker_resume.go — `gopi worker --resume --branch <name>` /
// `gopi worker --resume --worktree <path>`: the worker OWNS THE GROUND, so a
// resume can end with a ready-to-merge branch and NO worktree behind it.
//
// WHY THIS FACE EXISTS (devdoc/worker-resume-ground.md, the measured breach):
// the close-out chain refuses to remove the checkout the CHAIN'S OWN PROCESS is
// standing in (`internal/tools/guest_row_guard.go`'s
// guestRowOwnCheckoutRefusal — the 2026-09-21 breach on row 01a0c574, where a
// live run's ground was removed out from under it). That refusal is CORRECT and
// is not weakened here. The problem is structural: `gopi session resume <task-id>`
// chdir's INTO the row's worktree (that is where the executor's commits are),
// so the arm that must remove the directory is the arm standing in it — every
// successful resume leaves its worktree registered forever.
//
// The fix is not a more permissive arm; it is an actor that is NOT standing
// there. This face IS that actor:
//
//	① resolve the EXISTING ground (never create one; a missing ground is a
//	   named refusal, Rule 15b) and derive the ledger row from it,
//	② run the EXISTING resume path in a CHILD process — this process's own cwd
//	   never enters the worktree, which is exactly what keeps arm ③ silent,
//	③ run the standard close-out chain from HERE, over a directory nobody is
//	   standing in.
//
// The resume itself is NOT reimplemented (the briefing's hard boundary): the
// child is the same `gopi session resume <raw-log> --task <row>` the operator runs by
// hand, spawned with the resolver's directory as its cwd. No new tool, no shell
// wrapper, no second implementation of anything.

import (
	"github.com/nickhuang99/gopi/internal/cliverbs"

	"errors"
	"fmt"

	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/closeouthandoff"
	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/tools"
	"github.com/nickhuang99/gopi/internal/worktreerelease"
)

// workerFlagResume is the `--resume` spelling. The three ground selectors live
// in internal/tools — the DEFAULT one is the positionals this verb already
// takes (internal/tools.TaskResumeGroundForTask reads the row), and the two
// flags (TaskResumeGroundBranchFlag / TaskResumeGroundWorktreeFlag) sit beside
// the resolver that reads them — one home per value (Rule 14), so the grammar
// and the resolver can never disagree about what the operator typed.
const (
	workerFlagResume = cliverbs.ResumeFlag
)

// scanWorkerResumeFlags consumes the `--resume` FAMILY out of the argv the
// worker scanner walks: the bare switch, its two ground FLAGS (whose operand
// rides the next word or `--flag=VALUE` — the same workerRunFlagValue rule every
// other value flag of this verb follows), and the DEFAULT spelling, which is
// the positionals' task id the scanner already recognized.
//
// It returns the words it consumed so the ONE scanner keeps its shape — the
// resume vocabulary is a package of its own here rather than three more cases
// wedged into scanWorkerArgs, and the refusal for a MISSING operand is the
// scanner's (an empty selector is a usage error, not a silent default).
func scanWorkerResumeFlags(args []string, i int, wa *workerArgs) int {
	switch {
	case args[i] == workerFlagResume:
		wa.resume = true
		return 0
	case args[i] == tools.TaskResumeGroundBranchFlag:
		wa.resumeBranch = workerResumeOperand(args, i)
		if wa.resumeBranch == "" {
			return 0
		}
		return 1
	case args[i] == tools.TaskResumeGroundWorktreeFlag:
		wa.resumeWorktree = workerResumeOperand(args, i)
		if wa.resumeWorktree == "" {
			return 0
		}
		return 1
	}
	name, value := workerFlagNameValue(args[i])
	switch name {
	case strings.TrimPrefix(workerFlagResume, "--"):
		wa.resume = true
	case strings.TrimPrefix(tools.TaskResumeGroundBranchFlag, "--"):
		wa.resumeBranch = value
	case strings.TrimPrefix(tools.TaskResumeGroundWorktreeFlag, "--"):
		wa.resumeWorktree = value
	}
	return 0
}

// workerResumeSpelling reports whether an argv word belongs to the resume
// family — the mode switch or either ground selector, in the bare or the
// `--flag=VALUE` spell. It is the scanner's membership test (one list, read
// twice: the arm and the family's own scanner), so a new spelling cannot be
// recognized in one place and dropped in the other.
func workerResumeSpelling(a string) bool {
	name, _ := workerFlagNameValue(a)
	switch name {
	case strings.TrimPrefix(workerFlagResume, "--"),
		strings.TrimPrefix(tools.TaskResumeGroundBranchFlag, "--"),
		strings.TrimPrefix(tools.TaskResumeGroundWorktreeFlag, "--"):
		return true
	}
	return false
}

// workerResumeOperand takes a selector's operand from the next argv word.
//
// A MISSING operand yields "" rather than an error here, because the two
// failures are different sentences and the validator owns them: `--branch` with
// nothing after it is "this flag needs a value", while `--resume` with neither
// selector is "this mode needs a ground". Returning "" lets validate tell them
// apart; a next word that is itself a flag is NOT eaten as the operand (the
// scanner's own rule — an arbitrary path is never mistaken for a stray
// argument).
//
// The consumed operand is reported back through the returned index delta.
func workerResumeOperand(args []string, i int) string {
	if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
		return ""
	}
	return args[i+1]
}

// workerResumeGround resolves the mode's selector. THE ROW IS THE DEFAULT: a
// task id named as the verb's ordinary positional (`gopi worker --resume
// <task-id>`) is the primary spelling, and the two flags are its alternates —
// the row is what the operator knows when they are repairing a row, and the
// deliverable it already carries is where the ground is recorded.
//
// The row spelling is the same positional `gopi worker <task-id>` already takes
// when it RUNS a row (epic worker-positional-taskid): one slot, two meanings,
// chosen by the mode switch, which is exactly the shortcut the flags are —
// derivable names for ground the ledger already knows.
//
// Mixing the spellings is refused rather than merged: the row's OWN recorded
// ground and a ground named by hand can disagree (a row moved between worktrees,
// a stale deliverable), and silently preferring one of two answers is the
// degradation every other contradiction of this verb is refused for.
func workerResumeGround(wa workerArgs) (tools.TaskResumeGround, error) {
	switch {
	case wa.explicit != "" && (wa.resumeBranch != "" || wa.resumeWorktree != ""):
		// The grammar layer refuses this shape too (workerResumeRefusal), and
		// that is deliberate rather than duplicated: the scanner's refusal
		// reaches an operator before anything runs, while this one is what stops
		// a resolution from CHOOSING between two answers to one question. Both
		// read the SAME sentence (one spelling, two readers) so they can never
		// disagree about the repair.
		return tools.TaskResumeGround{}, errors.New(workerResumeMixedSpellingsRefusal(wa))
	case wa.explicit != "":
		return tools.TaskResumeGroundForTask(wa.explicit)
	}
	return tools.TaskResumeGroundFor(wa.resumeBranch, wa.resumeWorktree)
}

// workerResumeMixedSpellingsRefusal is the ONE sentence for `--resume <task-id>`
// beside a ground flag. It has two readers — the scanner's contradiction arm and
// the resolution itself — because they guard two different moments: a malformed
// argv must be refused before any resolution runs, and a resolution must never
// silently CHOOSE between the row's recorded ground and a hand-named one (a row
// moved between worktrees, a stale deliverable: two answers, one question).
func workerResumeMixedSpellingsRefusal(wa workerArgs) string {
	return fmt.Sprintf("gopi worker: the row %s and %s name ONE ground two ways, not a filter pair — "+
		"the row records its own ground, so pass the task id alone (`gopi worker %s %s`) "+
		"or the ground alone (%s/%s)\n",
		wa.explicit, workerResumeSpellingList(wa),
		workerFlagResume, workerPositionalSyntax,
		tools.TaskResumeGroundBranchFlag, tools.TaskResumeGroundWorktreeFlag)
}

// workerResumeSpellingList names which of the two FLAG spellings rode along —
// the contradiction refusal reports the word the operator actually typed rather
// than a fixed pair (a message that names a flag nobody passed sends the reader
// looking for a typo that is not there). Both cannot be set: validate refuses
// that pair before this function is reached.
func workerResumeSpellingList(wa workerArgs) string {
	if wa.resumeBranch != "" {
		return tools.TaskResumeGroundBranchFlag + " " + wa.resumeBranch
	}
	return tools.TaskResumeGroundWorktreeFlag + " " + wa.resumeWorktree
}

// runTaskWorkerResume is the face itself, entered from main.go's worker
// dispatch the moment the scanner reports `--resume` (before the agent-spawn
// guard: this face starts no loop and claims nothing, so the guard's ruling —
// an agent session must never START the worker LOOP — has nothing to rule on,
// and applying it here would refuse the very repair path this mode gives an
// operator standing inside a session).
//
// Exit code is the named-row contract (`gopi worker <task-id>`'s): 0 when the
// row ran and closed out, non-zero when it could not be run at all. A REFUSAL
// before anything ran — an unresolvable ground, a row with no checkpoint, a
// dirty tree — exits non-zero with the reason, because an operator who asked
// for a specific ground must not read a refusal as success.
func runTaskWorkerResume(wa workerArgs) {
	// The graceful-stop contract every runner face installs: a SIGTERM must not
	// kill this process between the child's exit and the close-out, or the
	// worktree this whole mode exists to release is left registered (one
	// implementation, Rule 14). The task-lifecycle mark follows it for the same
	// reason the other faces take it.
	uninstallStop := installWorkerStopSignals()
	defer uninstallStop()
	releaseTaskLifecycle := acquireTaskLifecycleOwnership()
	defer releaseTaskLifecycle()
	// THE RESUME FACE CONSUMES STOP REQUESTS TOO (bug 01a17df2, defect B).
	//
	// This face runs ONE resumed task in a child and never takes a claim round
	// (`consumeDrainRequests` is wired to the claim loop and its watcher), so it
	// used to be the one worker shape that could not answer `gopi worker --stop`
	// at all: the request landed, nobody read it, no receipt was written, and the
	// sender was left to attribute the silence to the target's BINARY AGE — a
	// guess, and a measurably wrong one (the silent pid was 33 minutes old while
	// three older pids answered).
	//
	// Wiring the request channel here is the same builder the foreground face
	// uses (workerRequestCtxFor: request files only, no state file — a resume
	// must not look like a running daemon to --start/--status). Reading the
	// request at START-UP is the right point for this face: it is the only point
	// before the resumed task begins, so a request that was already standing is
	// answered BEFORE any work starts, and the receipt states the resume face's
	// own exit sentence (workerAckExitResume) rather than the claim-loop one.
	//
	// It does not change the DRAIN semantics (epic 01a17da0): the resumed task is
	// not interrupted — this face has no in-flight round of its own to cut short,
	// and its child is left alone. The receipt says so. The request file is also
	// removed on the way out (cleanupRequests), because a request left behind
	// would address a recycled pid later.
	resumeCtx := workerRequestCtxFor(os.Getpid())
	resumeCtx.ackExit = workerAckExitResume
	resumeCtx.consumeDrainRequests()
	defer resumeCtx.cleanupRequests()

	ground, err := workerResumeGround(wa)
	if err != nil {
		// The resolution refused: NOTHING was created, nothing was entered and
		// nothing was removed (the resolver is a pure read — Rule 15b).
		fmt.Fprintf(os.Stderr, "%s: %v\n", workerProgTopLevel, err)
		os.Exit(workerExplicitExitFailed)
	}
	fmt.Fprintln(os.Stderr, ground.Report())

	// The resume runs in a CHILD, and that is the mechanism rather than a
	// preference: this process's cwd must stay OUT of the ground for the
	// close-out below to be allowed to remove it. `gopi session resume` chdir's into
	// deliverable.worktree itself, so the child ends up in the right directory
	// by the same code path the operator's own resume uses.
	code, err := workerResumeChild(ground)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: resume of %s: %v\n", workerProgTopLevel, ground.TaskID, err)
		os.Exit(workerExplicitExitFailed)
	}
	if code != 0 {
		fmt.Fprintf(os.Stderr, "%s: resume of %s exited %d — the ground is left EXACTLY as it was "+
			"(an unfinished run keeps its ground; no cleanup is attempted)\n",
			workerProgTopLevel, ground.TaskID, code)
		os.Exit(workerExplicitExitFailed)
	}

	// ④ THE CLOSE-OUT, from HERE — a process that does not stand in the ground.
	// It is the SAME seam the resume path's own tail calls
	// (runResumeFinishCleanup → the close-out chain — RETIRED in 58b5618b, so this
	// records the shape the seam used to have): one chain,
	// one implementation, every guard intact. The only thing this face changes
	// is WHO asks, and where they are standing when they ask.
	//
	// ⑤ THIS FACE'S OWN GROUND, when the child handed its report over. The
	// child's end hook declines its own finish-cleanup in that case (the ask is
	// covered), and the row it declines covers is the one the CHILD's own
	// finish-cleanup would have freed — the row that names THIS FACE's working
	// directory, not the resumed row. Naming that row here is what keeps the
	// decline honest; see runTaskWorkerResumeCleanupFor.
	//
	// THE DIRECTORY IS CAPTURED HERE, BEFORE THE RELEASE (measured defect
	// 2026-10-15). runTaskWorkerResumeCleanup re-parents this process's CWD to
	// the filesystem root on purpose (it must not hold the ground its own next
	// step is allowed to remove), so a reading of the cwd taken AFTER that call
	// answers "/" — and "/" is not a work tree, so every successful run ended
	// with the false pair of lines
	// `close-out for /: resume: --worktree / is not a work tree: …`.
	// The cwd is read ONCE, here, and only ONCE per run: the cleanup function no
	// longer asks for it at all (the second call site was the same reading, one
	// release too late).
	ownDir := pwdOrEmpty()
	rep := runTaskWorkerResumeCleanup(ground)
	// NO FAILURE FRAME HERE, and that is the state's own answer (measured
	// 2026-10-31, epic 01a18799 report). This arm used to print
	// `close-out for <row> produced NO report: no cause in this chain's own
	// output (…)` whenever `rep` was nil — which was written when a nil meant
	// "the chain was asked and failed". Since the finish-cleanup mechanism was
	// retired there is no chain to ask, so `rep` is nil on EVERY healthy run: the
	// frame told a healthy operator their close-out had failed, and its cause
	// sentence claimed nothing above named why, after the machinery that could
	// have named it had been removed. The absence and its cause are now stated
	// ONCE, in the normal frame, by the seam that knows it
	// (runTaskWorkerResumeCleanup, workerResumeCloseoutRetiredFormat) — Rule 15,
	// and Rule 43: a reader is retired together with the mechanism that fed it.
	runTaskWorkerResumeCleanupFor(ownDir, rep)
	os.Exit(workerExplicitExitOK)
}

// workerResumeChild spawns the EXISTING resume path for the resolved row and
// returns its exit code.
//
// WHAT THE CHILD IS TOLD, and why each part:
//
//   - `<task-id>` — the resolver's row, hence the raw log comes from the row's
//     own run directory (tools.TaskResumeTarget / rundir), which is the ONE
//     address the resume path resolves from. No raw-log hunting here.
//   - `--task <task-id>` — pinned EXPLICITLY even though the target implies it:
//     the brief's spelling. It is the same row, and it makes the linkage
//     visible in the child's own output (the resumed:<session> tag line) rather
//     than implicit in the target's resolution.
//   - its CWD is the ground. `task run` hands its child the row's ground for
//     the same reason (the executor's commits live there); a resume that starts
//     anywhere else can still resolve the checkpoint, but nothing else about
//     the run should have to guess where the work is.
//
// The child's streams are passed through, exactly as the operator would run it:
// the resume's own output reaches the terminal and is not swallowed by the
// wrapper.
func workerResumeChild(ground tools.TaskResumeGround) (int, error) {
	argv := runcmd.ResumeArgv(ground.TaskID, ground.TaskID)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = ground.Dir
	// The child's environment is this process's own (it carries GOPI_HOME, the
	// ledger path and the endpoint knobs the resume needs). Nothing is added
	// and nothing is stripped: the resume path is the operator's own command,
	// and a wrapper that rewrote its environment would be a second, unreviewed
	// answer to "what does a resume run with?"
	// …plus ONE declaration (epic worker-resume-report-once): the child is told
	// that a resuming parent exists and will make the close-out report itself,
	// so the child's two writers (its end hook and its resume tail) both defer
	// and NAME the parent instead of each printing the same refusal. The
	// declaration goes through the environment because two processes share no
	// memory: it is set before the child's first instruction, so no writer has
	// to guess what another will do later — the same deterministic convention
	// GOPI_TASK_WRAPPER_ID already uses for the same reason.
	//
	// …and the RESUME DECLARATION itself (epic: notify payload carries
	// resumed_from). This face IS a door — it decided, before anything ran, to
	// CONTINUE the row's existing work — so the fact travels to the child the
	// same way the `task run` seam sends it: through tools.TaskRunResumeFromEnv,
	// whose ONE reader (tools.taskRunResumeWitness) stamps it onto the notify
	// payload of the run's terminal transition. Without this the resumed
	// close's event is byte-identical to an ordinary one, which is the defect
	// the epic exists for. The value is the session being continued: the row's
	// own run-dir record (tools.TaskRedispatchResumeFrom — the same question
	// the `task run` door asks), falling back to the row id, which is the
	// target the argv above already names.
	resumedFrom := tools.TaskRedispatchResumeFrom(ground.TaskID)
	if resumedFrom == "" {
		resumedFrom = ground.TaskID
	}
	cmd.Env = append(os.Environ(),
		closeouthandoff.ParentReportsEnv+"="+closeouthandoff.ParentReportsValue(),
		tools.TaskRunResumeFromEnv+"="+resumedFrom)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			// A non-zero exit is the CHILD's verdict, reported as a code rather
			// than as a spawn failure — the two are different answers.
			return ee.ExitCode(), nil
		}
		return 0, fmt.Errorf("spawn %s: %w", runcmd.BinaryName, err)
	}
	return 0, nil
}

// runTaskWorkerResumeCleanup is the close-out step of the mode, and its
// reporting is the point rather than a courtesy (Rule 15): the incident's
// symptom was a chain that never ran and never said anything.
//
// It reports THREE outcomes, exactly as the resume path's own tail does —
// performed, refused-with-reason, errored — and it NEVER rewrites the resumed
// session's success: a cleanup refusal is a fact about a directory, not a
// verdict on the work (the same fail-soft posture as runResumeFinishCleanup,
// whose reporting shape this mirrors deliberately: an operator on either face
// must read the same sentence).
//
// ONE REPORT PER RUN, and this is the one (epic worker-resume-report-once,
// measured 2026-09-26). This face is the LAST reporter in the chain and the only
// one outside the child's process: the child's end hook and its resume tail
// both deferred to it, each naming it, because this face declared itself before
// spawning the child (closeouthandoff.ParentReportsEnv — see workerResumeChild).
// So the parent's report must be UNCONDITIONAL: a child that died before its
// hook declared nothing, and this is then the only report there is. The chain
// is asked either way; only the printing is single-sourced.
//
// The CWD release runs FIRST, before the chain, and the order is a producer
// obligation rather than style: this process may have started in the repo whose
// row is being closed out, and the chain removes directories — releasing first
// is what keeps the arm's liveness reading honest (internal/worktreerelease).
//
// IT NO LONGER ASKS FOR THIS FACE'S OWN GROUND, and that is the fix rather than
// an omission (measured 2026-10-15): the release above puts this process at
// worktreerelease.ProcessRootPath, so a reading of the cwd taken AFTER it — the
// reading this function used to take, and took TWICE per run with the caller's
// own call — asked the resolver about "/", which is not a work tree. The report
// for this face's own ground is made by runTaskWorkerResumeCleanupFor from the
// directory the CALLER captured before the release; one reading, one report.
func runTaskWorkerResumeCleanup(ground tools.TaskResumeGround) *tools.FinishCleanupReport {
	worktreerelease.ReleaseReporting(os.Stderr, workerResumeProgName)
	resumeClosedOutRow = ground.TaskID
	// THE CLOSE-OUT CHAIN IS RETIRED (task 01a18799, slice 3/4), so this face has
	// nothing left to ask it. What used to happen here — this process closing its
	// own row out through the chain — now belongs to the producer-side release the
	// delivery loop runs from the run's own END hook (Rule 6c). The release ABOVE
	// still runs, and still runs FIRST, for the reason its own comment gives: it
	// is a producer obligation, not a step of the chain.
	//
	// SO NO REPORT EXISTS, ON EVERY RUN, and that is stated ONCE, HERE, as the
	// ordinary condition of this mode rather than as a failed close-out (Rule 15:
	// the fact is printed, not dropped). It is NOT a cause the reader has to
	// invent and NOT a failure to attribute: with the mechanism gone, "no report"
	// is what a healthy run's close-out looks like, and the row it would have
	// reported is named by the release line above (measured 2026-10-31: the
	// caller used to say `close-out for … produced NO report: no cause in this
	// chain's own output`, i.e. it kept the failure frame of a mechanism that no
	// longer exists — Rule 43's reader half).
	//
	// A nil return is therefore the honest answer, not a failure to report: the
	// caller (runTaskWorkerResumeCleanupFor) reads it as "no reading to compare
	// against" and claims nothing it did not measure.
	var rep *tools.FinishCleanupReport
	fmt.Fprintf(os.Stderr, workerResumeCloseoutRetiredFormat, workerResumeProgName, ground.TaskID)
	return rep
}

// runTaskWorkerResumeCleanupFor resolves the row THIS FACE's own working
// directory belongs to and REPORTS it, so the ground the mode stands in is not
// a silent loose end.
//
// THE DIRECTORY IS AN ARGUMENT, NEVER A READING (measured defect 2026-10-15).
// This function is called after runTaskWorkerResumeCleanup has re-parented the
// process's CWD to the filesystem root, so reading the cwd HERE answers "/" —
// and asking git about "/" produced the false pair of lines every successful run
// printed:
//
//	close-out for /: resume: --worktree / is not a work tree: git rev-parse
//	--show-toplevel in /: exit status 128
//
// The caller captured the directory before the release and passes it in; this
// function asks the process nothing. The reading itself is the tools layer's
// (tools.TaskResumeGroundForOwnDir — never a path convention), so the report is
// re-derivable from the same reader a resume by path uses.
//
// SCOPED HONESTLY — this epic's own words, because overstating coverage is
// worse than a gap: running a SECOND chain for that row, and the path an
// operator uses to free this ground in general (`gopi worktree release`), are a
// WIDER change than the report-once fix. What this step does is the part the
// count depends on: it resolves the row through the LEDGER's own binding, and
// NAMES the step that frees it when the row is not the one just closed out. A
// checkout no row names is reported as exactly that rather than guessed at
// (Rule 15: never a silent skip).
func runTaskWorkerResumeCleanupFor(dir string, rep *tools.FinishCleanupReport) {
	path := strings.TrimSpace(dir)
	if path == "" {
		fmt.Fprintf(os.Stderr, "%s: close-out: this face's own working directory is unavailable\n", workerResumeProgName)
		return
	}
	ground, err := tools.TaskResumeGroundForOwnDir(path)
	switch {
	case err != nil:
		// The directory WAS named and the reading still failed: the ledger could
		// not be read, or git could not be asked. That is an error about the
		// reading, not a normal answer below — the two must never render the
		// same way (Rule 15). It KEEPS the close-out frame on purpose: it is a
		// failure of the same seam, and a reader must be able to tell the plain
		// acknowledgement apart from it by its first words.
		fmt.Fprintf(os.Stderr, workerResumeOwnGroundFailedFormat,
			workerResumeProgName, path, err)
	case strings.TrimSpace(ground.TaskID) != "" && samePath(path, workerResumeNormalOwnGround(rep)):
		// THE NORMAL CONDITION, stated as one. This face stands in the ground the
		// close-out just released: the reading came back as the row that was
		// closed out AND the chain reported that same directory removed. Nothing
		// is left to do, and saying so plainly is the whole point (Rule 15).
		fmt.Fprintf(os.Stderr, workerResumeOwnGroundNormalFormat, workerResumeProgName, workerResumeOwnGroundNormalSummary, path)
	case strings.TrimSpace(ground.TaskID) == "":
		// THE ORDINARY HEALTHY SHAPE, and the one three sessions misread: this
		// face stands where the operator launched it, which is NOT the task's
		// ground (that is the design of this mode). The chain may also have just
		// removed this directory, which is why the answer says which of the two
		// readings was taken rather than only "no row".
		fmt.Fprintf(os.Stderr, workerResumeOwnGroundNormalFormat, workerResumeProgName,
			workerResumeNoRowSummary(rep, path), path)
	case ground.TaskID == resumeClosedOutRow:
		fmt.Fprintf(os.Stderr, "%s: this face's own ground %s is the row just closed out (%s)\n",
			workerResumeProgName, path, ground.TaskID)
	default:
		fmt.Fprintf(os.Stderr, "%s: this face's own ground %s belongs to row %s — release it with `%s`\n",
			workerResumeProgName, path, ground.TaskID, workerResumeOwnGroundHint)
	}
}

// THE OWN-GROUND NOTE MUST NOT LOOK LIKE THE CLOSE-OUT'S FAILURE CAUSE
// (measured 2026-10-28, epic 01a17e23).
//
// WHAT WAS WRONG. The arm that answers "no ledger row names this directory"
// printed inside the close-out's own frame:
//
//	gopi worker --resume: close-out for /path/to/ground: no ledger row names this directory
//
// — immediately after the close-out result, on runs that SUCCEEDED. The
// sentence was true and the fact belongs in the report (Rule 15), but a reader
// holding a failed count (`close-out report=0`) saw two lines about the same
// seam, the second one saying "no ledger row names…", and attributed the
// failure to a ledger lookup miss. THREE separate sessions did exactly that
// (rows 01a17da0, 01a17bf6, 01a17dd7), each spending its turn chasing a
// lookup that had not failed.
//
// TWO CHANGES, and both are needed:
//
//	① THE FRAME. The normal answer no longer opens with "close-out": it names
//	   itself as the own-ground NOTE and leads with the verdict (nothing to do
//	   here is the EXPECTED reading), so a grep for the close-out's frame no
//	   longer returns a healthy run's line. The failure arm above KEEPS the
//	   frame deliberately — that is what makes the two unmistakable.
//	② THE CAUSE. The close-out's own sentence no longer has to be inferred
//	   either — it is printed by the seam that knows it (see the retired-chain
//	   block below, which replaced the 2026-10-28 "produced NO report" sentence
//	   once the mechanism it described was deleted). A reader no longer has to
//	   choose between two neighbouring sentences, because only one of them is
//	   about a failure, and it is the one that says so.
//
// "normal" is the word the epic asks for and the word an operator needs: the
// directory this face stands in is not the task's ground, and that is the
// DESIGN of this mode, not a defect in it.
const (
	// workerResumeOwnGroundNormalLabel names WHAT the healthy line is about, so
	// it can never be read as part of the close-out above it (Rule 1: one
	// spelling, referenced by the format and by the tests).
	workerResumeOwnGroundNormalLabel = "own ground"
	// workerResumeOwnGroundNormalWord is the one word that says this is the
	// DESIGN rather than a defect — the word the epic asks for, and the one
	// three sessions needed and did not get.
	workerResumeOwnGroundNormalWord = "(normal)"
	// workerResumeOwnGroundNormalFormat renders the own-ground note. %s is the
	// face's program name, then the SUMMARY that states which normal condition
	// was read, then the directory asked about. Reading as the tests do: a
	// literal, on purpose — editing the sentence at a call site must turn the
	// regression RED rather than be followed silently (the same posture as the
	// three sentences in cli_worker_resume_emission_test.go).
	workerResumeOwnGroundNormalFormat = "%s: " + workerResumeOwnGroundNormalLabel + " " + workerResumeOwnGroundNormalWord + ": %s — %s\n"
	// workerResumeOwnGroundNoRowSummary is the "nothing names it, nothing to
	// do" answer: the ordinary case, and the one three sessions misread.
	workerResumeOwnGroundNoRowSummary = "no ledger row names it, so this face has no ground to close out"
	// workerResumeOwnGroundNormalSummary is the same fact said by the reading
	// that RESOLVED: the directory came back as the row that was just closed
	// out, so this face is standing in the ground it released.
	workerResumeOwnGroundNormalSummary = "this directory is the row just closed out, which this close-out already reported"
	// workerResumeOwnGroundRemovedSummary is the reading AFTER the chain removed
	// the directory — the lookup missing it is the result, not a cause.
	workerResumeOwnGroundRemovedSummary = "the ledger no longer names it because the close-out above REMOVED this directory (worktree-freed)"
	// workerResumeOwnGroundFailedFormat KEEPS the close-out frame: this arm is a
	// real failure of the same seam, and the frame is what tells a reader which
	// one they are looking at.
	workerResumeOwnGroundFailedFormat = "%s: close-out for %s FAILED: %v\n"
)

// THE CLOSE-OUT'S ABSENCE IS THE MODE'S ORDINARY CONDITION, NOT A FAILURE
// (measured 2026-10-31, this epic's own re-measurement at 07171cb).
//
// WHAT WAS WRONG. The retirement of the finish-cleanup chain (58b5618b, task
// 01a18799 slice 3/4) left this face with nothing to ask, so its
// `runTaskWorkerResumeCleanup` stopped returning a report on ANY run — and the
// caller's `rep == nil` arm still printed the sentence that had been written for
// the opposite meaning:
//
//	gopi worker --resume: close-out for t-…: produced NO report: no cause in
//	this chain's own output (the close-out report is absent and nothing above
//	names why)
//
// On a healthy, exit-0 run. Two defects in one line, and they are different
// ones: the FRAME was borrowed from a failure (3e6b687's whole point — memory
// #19830), and the CAUSE claimed that nothing in the chain named why, after the
// only machinery that could have named it had been deleted. A reader gets the
// false verdict first and the false reason second.
//
// WHAT REPLACES IT. One sentence, in this face's NORMAL frame, stating the
// three facts a reader needs and nothing else: no report is produced for which
// row, that this is the design (the chain is retired), and that the release the
// operator cares about has already run above it (Rule 15: the fact is printed
// once — a silent removal would be the other defect). The failure frame is now
// reserved for what it says: `close-out for … FAILED:` remains in
// workerResumeOwnGroundFailedFormat, reachable only when a READING fails.
const (
	// workerResumeCloseoutRetiredLabel names WHAT the line is about, so it can
	// never be read as part of another seam's sentence (Rule 1: one spelling,
	// referenced by the format and by the tests).
	workerResumeCloseoutRetiredLabel = "close-out"
	// workerResumeCloseoutRetiredWord is the one word that says the mechanism
	// the reader was looking for no longer exists — the word that replaces the
	// invented cause.
	workerResumeCloseoutRetiredWord = "(retired)"
	// workerResumeCloseoutRetiredReason is the CAUSE, said by the seam that knows
	// it: not "nothing names why", but the named mechanism and where its duty
	// went. A reader who came for a report learns why there is none and what did
	// happen instead (the release line above this one).
	workerResumeCloseoutRetiredReason = "the finish-cleanup chain is retired, so no report exists for it; the cwd release above already ran and is the only close-out step this face performs"
	// workerResumeCloseoutRetiredFormat renders the whole sentence. %s is the
	// face's program name, then the row the resume closed out. Reading as the
	// tests do: a literal, on purpose — editing the sentence at a call site must
	// turn the regression RED rather than be followed silently.
	workerResumeCloseoutRetiredFormat = "%s: " + workerResumeCloseoutRetiredLabel + " " + workerResumeCloseoutRetiredWord + ": no report for row %s — " + workerResumeCloseoutRetiredReason + "\n"
)

// workerResumeCloseoutRetiredLine renders the whole line for a row — the ONE
// sentence this face prints about the report it can no longer produce.
//
// Extracted rather than inlined for the reason the failure sentence was: the
// regression must be able to read the exact operator-facing text, and the
// end-to-end capture must be able to count it (exactly once — Rule 15's "the
// fact is not deleted, it is stated once").
func workerResumeCloseoutRetiredLine(taskID string) string {
	return fmt.Sprintf(workerResumeCloseoutRetiredFormat, workerResumeProgName, taskID)
}

// workerResumeNoRowSummary picks WHICH normal reading to state when no ledger row
// names the directory — the two readings have different evidence, and a reader
// needs the one that was actually taken rather than a generic "nothing here":
//
//	· the chain removed THIS directory: the lookup missing it is the RESULT of
//	  the close-out above, so the note says that and names the marker which
//	  proves it (`worktree-freed=`);
//	· otherwise: nothing names the directory and nothing removed it. This face
//	  simply stands where it was launched, which is the design of the mode.
//
// THE PATH MUST MATCH, and that is the whole point of taking it (measured in
// this change's own run): that predicate is true
// whenever the chain's worktree step succeeded over ANY directory, and the
// ordinary resume frees the ROW's ground (`…/wt`) while this face stands in its
// launch directory (`…/repo`). Reading the flag alone would have printed "the
// close-out above REMOVED this directory" about a directory still on disk —
// an unverified claim in the very sentence that exists to stop readers inventing
// causes. So the answered claim is an identity between two spellings (Rule 15:
// the report states what was measured).
//
// It reads the report it is handed and never re-derives one (Rule 2).
func workerResumeNoRowSummary(rep *tools.FinishCleanupReport, dir string) string {
	// NO REPORT CAN EXIST ANY MORE (task 01a18799, slice 3/4): the close-out chain
	// that produced it is retired, so this face never holds one and the sentence
	// that existed to answer "did the close-out above remove this directory?" has
	// no measurement behind it. Returning the no-row answer is the honest one —
	// it claims nothing about removal (Rule 15: a verdict carries its reason, and
	// an unverified claim is not made). `rep` and `dir` stay in the signature so
	// the call site and its own doc (the path-identity reasoning above) keep
	// reading true the day a report exists again.
	_, _ = rep, dir
	return workerResumeOwnGroundNoRowSummary
}

// workerResumeNormalOwnGround renders the directory the normal-condition arm
// compares against: the row's own recorded ground, when the report carries one.
//
// WHY THIS COMPARISON AND NOT "ANY ANSWER IS NORMAL" (honesty about coverage):
// the arm above must not claim "normal" for a directory that genuinely belongs
// to a DIFFERENT live row — that is a loose end this face can name, and the
// `belongs to row …` arm still does. So only the two shapes with nothing left to
// do read as normal: no row at all (the launch directory of an ordinary resume),
// and the row this very pass closed out. An empty answer means "no reading to
// compare against", which keeps the consequence arm (removed worktree) in reach.
func workerResumeNormalOwnGround(rep *tools.FinishCleanupReport) string {
	// ALWAYS "" now: the normal-condition arm compares against a reading from the
	// close-out report, and no close-out runs any more, so there is no reading to
	// compare against. "" means "no reading", which keeps the row-naming arms —
	// the ones that report a live loose end — in reach (Rule 15).
	_ = rep
	return ""
}

// samePath reports whether two directory spellings name the same directory.
// Compared after filepath.Clean so a trailing separator (the fixture's own
// spelling) cannot make an identical directory look foreign — and ONLY that
// much: an unresolved symlink is deliberately not followed, because the caller
// compares two spellings of a path it was handed, not two filesystem objects.
func samePath(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// resumeClosedOutRow is the row the cleanup above just reported, so the pass
// below can tell "that WAS my own ground" from "a different row is waiting" —
// one writer, one reader (Rule 2).
var resumeClosedOutRow string

// workerResumeOwnGroundHint is the step this face NAMES for freeing its own
// ground. It is quoted as text, never invoked: a bare `gopi worktree release`
// from here would remove the directory this process is standing in.
const workerResumeOwnGroundHint = "gopi worktree release"

// pwdOrEmpty is this face's own working directory, or "" when the OS cannot
// report it (a deleted cwd is the real case) — a named helper so the close-out
// pass reads as one decision, and so the empty answer is a plain refusal rather
// than a silently skipped step (Rule 15).
func pwdOrEmpty() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// workerResumeProgName names this face in the diagnostics it emits on its own
// behalf — one spelling per face, one grep (the resumeProgName /
// taskProgName / workerProgTopLevel convention, Rule 1b).
const workerResumeProgName = "gopi worker --resume"

// workerResumeRefusal renders the CONTRADICTION refusals of the resume mode.
// It is a package function (not a method) so the validator's one caller reads
// as one sentence per shape, and so the red-proof tests can assert each shape
// without constructing a workerArgs by hand.
//
// Four shapes, each named because the REPAIR differs:
//
//	no row and no ground          the mode needs a row (the default positional)
//	                              or a ground (`--branch`/`--worktree`)
//	--branch AND --worktree       two spellings of ONE ground, not a filter pair
//	--resume with --daemon        a detached resume is not a thing: the daemon
//	                              face is the resident loop, which claims its
//	                              own rows and would drop the selector entirely
//	--resume with a run flag      --takeover/--as/--db/--repo/--report-to
//	                              describe the ordinary claim-and-run path,
//	                              which this mode does not take
//
// A ROW BESIDE A GROUND FLAG is refused one layer down (workerResumeGround): it
// is a shape about which of the two answers to trust, so the sentence belongs
// where the resolution happens.
func workerResumeRefusal(wa workerArgs) string {
	branch := strings.TrimSpace(wa.resumeBranch)
	worktree := strings.TrimSpace(wa.resumeWorktree)
	switch {
	case wa.daemon:
		return fmt.Sprintf("gopi worker: %s with %s is not a thing — %s detaches the resident LOOP, "+
			"which claims its own rows; a resume names ONE existing ground\n"+
			"  run it in the foreground: `gopi worker %s %s %s`\n",
			workerDaemonFlagLong, workerFlagResume, workerDaemonFlagLong,
			workerFlagResume, tools.TaskResumeGroundBranchFlag, workerResumeValueSyntax)
	case branch != "" && worktree != "":
		return fmt.Sprintf("gopi worker: %s and %s name ONE ground two ways, not a filter pair — pass one\n"+
			"  `gopi worker %s %s %s` or `gopi worker %s %s %s`\n",
			tools.TaskResumeGroundBranchFlag, tools.TaskResumeGroundWorktreeFlag,
			workerFlagResume, tools.TaskResumeGroundBranchFlag, workerResumeValueSyntax,
			workerFlagResume, tools.TaskResumeGroundWorktreeFlag, workerResumePathSyntax)
	case wa.explicit != "" && (branch != "" || worktree != ""):
		return workerResumeMixedSpellingsRefusal(wa)
	case branch == "" && worktree == "" && wa.explicit == "":
		return fmt.Sprintf("gopi worker: %s needs the row to resume — the default spelling is the TASK ID, "+
			"and the ground follows from it:\n"+
			"  `gopi worker %s %s`   (the row is named; its recorded worktree+branch are resolved)\n"+
			"  or name the ground directly:\n"+
			"  `gopi worker %s %s %s`   (the branch is the input; the row is derived from it)\n"+
			"  `gopi worker %s %s %s`   (pwd of the ground: `git worktree list` names it)\n"+
			"  it resolves an EXISTING ground and never creates one\n",
			workerFlagResume,
			workerFlagResume, workerPositionalSyntax,
			workerFlagResume, tools.TaskResumeGroundBranchFlag, workerResumeValueSyntax,
			workerFlagResume, tools.TaskResumeGroundWorktreeFlag, workerResumePathSyntax)
	}
	return ""
}

// workerResumeValueSyntax / workerResumePathSyntax are the placeholder words
// this mode's operator-facing strings use for its two operands. Spelled once
// (Rule 1b) so the grammar synopsis, the refusals and the usage head agree.
const (
	workerResumeValueSyntax = "<branch-name>"
	workerResumePathSyntax  = "<worktree-path>"
)

// workerResumeOnlyFlag is the refusal for `--branch`/`--worktree` arriving
// WITHOUT `--resume`: they are the resume mode's vocabulary and mean nothing on
// their own (a bare `gopi worker --branch X` would otherwise be silently
// dropped, which is the same degradation every other lone option is refused
// for).
func workerResumeOnlyFlag(wa workerArgs) string {
	lonely := ""
	for _, f := range []string{tools.TaskResumeGroundBranchFlag, tools.TaskResumeGroundWorktreeFlag} {
		if workerResumeSelectorSet(f, wa) {
			lonely = f
			break
		}
	}
	if lonely == "" {
		return ""
	}
	return fmt.Sprintf("gopi worker: %s is the GROUND of a resume — it means nothing without %s\n"+
		"  `gopi worker %s %s %s`\n",
		lonely, workerFlagResume, workerFlagResume, lonely, workerResumeValueSyntax)
}

// workerResumeSelectorSet reports whether the argv carried one of the resume
// mode's two selectors. It reads the parsed fields directly rather than going
// through the run-flag predicate (workerArgSets): the selectors are not run
// flags, and a membership test that answered for the wrong family is how
// `--branch` would end up reported as a flag needing a task id.
func workerResumeSelectorSet(flag string, wa workerArgs) bool {
	switch flag {
	case tools.TaskResumeGroundBranchFlag:
		return wa.resumeBranch != ""
	case tools.TaskResumeGroundWorktreeFlag:
		return wa.resumeWorktree != ""
	}
	return false
}
