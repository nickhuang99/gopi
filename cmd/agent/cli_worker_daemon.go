package main

// cli_worker_daemon.go — the WORKER FACE of the shared self-daemonization
// mechanism (`gopi worker --daemon`).
//
// WHY THE FLAG EXISTS: `gopi worker` runs in the FOREGROUND and, on a drained
// queue, waits (workerIdleInterval) before it self-exits. A caller — the
// operator's terminal, but also a script or an agent shell that must stay
// responsive — has no way to start one and keep its own prompt: the documented
// workaround was the caller's job
//
//	setsid nohup ./bin/gopi worker --worker-log-root DIR >>LOG 2>&1 &
//
// i.e. the caller must remember setsid + nohup + redirect + `&` (forget one
// and the shell blocks, or the worker dies with the caller's terminal), gets
// NO signal that the worker really started, and chooses the log location
// itself. `gopi worker --daemon` moves all three to the daemon.
//
// WHAT THE FLAG DOES **NOT** CHANGE (the briefing's hard requirement): the
// worker's drain-and-self-die contract. There is NO flock (multi-instance is
// by design — the shared seam takes a control fd only when a face supplies
// one, and this face passes none), and there is NO new lifetime semantics: a
// detached worker still waits out workerIdleInterval between drained rounds
// and still exits 0 by itself after workerIdleExits consecutive empty ones.
// Detaching decides WHO owns the terminal, never HOW LONG the loop lives.
//
// THE MECHANISM IS SHARED, NOT COPIED: fork/exec-self, setsid, stdio redirect
// and the parent-handshake live in cli_daemon_spawn.go, together with the
// merger's use of them. This file supplies only the worker's vocabulary.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/borrowedfd"
	"github.com/nick/gopi/internal/daemonlifetime"
	"github.com/nick/gopi/internal/runcmd"
	"github.com/nick/gopi/internal/tools"
)

// --- named constants (PROJECT.md Rule 1: one place to find and change) ---

const (
	// workerDaemonFlagLong is the flag that detaches the worker — the SHARED
	// spelling, so the two faces cannot drift apart.
	workerDaemonFlagLong = daemonFlagLong
	// workerDaemonEnv is the marker the re-exec'd worker daemon child carries.
	// It is the child's proof of identity (a user typing the flag twice can
	// never be mistaken for the daemon) and it is absent from the parent's
	// environment by construction: only workerDaemonSpawn writes it, and only
	// into the child's env.
	workerDaemonEnv = "GOPI_WORKER_DAEMON_CHILD"
	// workerProgTopLevel is the program name the worker's diagnostics are
	// prefixed with — one spelling, referenced by the usage text, the refusal
	// message and now the daemon notes.
	workerProgTopLevel = "gopi " + CliVerbWorker
	// workerDaemonLogBaseName is the file a detached worker's stdio lands in:
	// <gopi data root>/logs/worker.log. It is the DIRECT analogue of the
	// merger's logs/merger.log — one file per COMPONENT, not per launch — and
	// it is deliberately distinct from the launcher's logs/worker-startup.log
	// (that file belongs to scripts/gopi-worker-up.sh, which holds the pipe for
	// a worker IT started; a daemon this flag starts belongs to whoever asked
	// for it and never writes into the launcher's record).
	workerDaemonLogBaseName = "worker.log"
	// workerDaemonLockWait bounds the child's wait for an INHERITED lock that is
	// somehow still held (should be instantaneous — the parent released it).
	// Bounded rather than blocking forever so a bug surfaces as a visible
	// startup failure instead of a silent hang.
	workerDaemonLockWait = 5 * time.Second
	// workerDaemonLockPoll is the poll cadence of that bounded wait.
	workerDaemonLockPoll = 50 * time.Millisecond
	// workerDaemonChildFailNote is the child's pre-handshake failure line (it
	// goes to the daemon's own log AND to the launcher through the handshake).
	workerDaemonChildFailNote = "%s: --daemon: %v\n"
)

// workerDaemonArgs are the worker-loop flags that MUST survive the re-exec
// (Rule 1: one named list, so the parser, the refusal message and the re-exec
// builder cannot drift). --daemon is absent by construction: it is what is
// being consumed, and the child is identified by workerDaemonEnv instead.
// The retired --worker-log-root is absent for the same structural reason: the
// output destination is derived from the row at the spawn seam
// (cli_tasks_exec_output.go), so there is no root left to forward.
//
// The EXPLICIT face's flags are absent too, and for a stronger reason than
// bookkeeping: `--daemon` is refused together with a named row
// (workerArgs.validate),
// so a detached child can never be a named-row run — there is nothing of that
// family to forward. The list stays a list (rather than a bare `--once`
// reference) so a future loop flag has one obvious place to be declared.
var workerDaemonArgs = []string{workerFlagOnce}

// workerArgs is what the `gopi worker` argv scan yields: the mode the face must
// run in plus the explicit row it was handed.
type workerArgs struct {
	// once is the drain-then-exit mode (--once).
	once bool
	// daemon is the detach request (--daemon / --daemon=…).
	daemon bool
	// seam is true when the argv carried a DAEMON SEAM flag (--require-scope):
	// a word that only means anything together with --daemon, which the
	// dispatcher refuses when it appears alone.
	seam bool
	// seamRequired mirrors --require-scope's presence (the seam re-reads it
	// through daemonScopeRequested; this field is what the dispatcher's
	// "seam flag without --daemon" refusal reports).
	seamRequired bool
	// explicit is the row named POSITIONALLY on the argv (`gopi worker <task-id>`):
	// when set, this face runs EXACTLY that row and nothing else (epic
	// worker-run-entry; positional since epic worker-positional-taskid). It is
	// the selection half the retired `gopi task run <task-id>` had, now living
	// on the one runner.
	explicit string
	// takeover is `--takeover` (only meaningful with a task id): override the
	// pre-execution singleton guard for a row a live process holds. It travels
	// with the explicit selection because that is where "I know it is held, run
	// it anyway" is a deliberate operator act (the retired verb carried it the
	// same way).
	takeover bool
	// as is `--as <holder>` (only meaningful with a task id): the claim's holder
	// identity, for a scripted runner that must not be confused with the
	// operator's own.
	as string
	// db is `--db <path>` (only meaningful with a task id): the ledger to run
	// against instead of the profile default.
	db string
	// reportTo is `--report-to <alias>` (only meaningful with a task id): the ssh
	// alias a DISPATCHED run reports its outcome back through — the remote half
	// of the ssh handoff whose command this binary builds (cmd/agent/dispatch.go).
	reportTo string
	// repo is `--repo <target>` (only meaningful with a task id): the git push-back
	// target for the finished branch (the other half of the same handoff).
	repo string
	// resume is the RESUME mode (`--resume`): the worker resolves an EXISTING
	// ground, runs the resume path over it and closes the row out from OUTSIDE
	// the worktree (cli_worker_resume.go). It is a THIRD family of this verb —
	// neither the loop (bare/--once) nor the explicit named row — because its
	// input is a GROUND, not a row: the row is DERIVED from the ground, which is
	// the direction that makes the worker, not the resumed session, the owner of
	// the worktree's lifetime.
	resume bool
	// resumeBranch is `--branch <name>`: the branch whose live worktree is the
	// ground to resume. The REPO is resolved from the ledger rows naming the
	// branch (the operator is not asked to know which host holds it).
	resumeBranch string
	// resumeWorktree is `--worktree <path>`: the ground named as a PATH — the
	// `pwd` an operator pastes after running `git worktree list`. The repo comes
	// from git at that path, and the row from the ledger's worktree lookup.
	resumeWorktree string
	// refuse is the sentence a CONTRADICTORY argv earned ("" = accepted). It is
	// separate from the unknown-word result because the two refusals mean
	// different things: an unknown word is "this binary does not know it" (the
	// stale-deployment case), while this one is "every word here is known, but
	// not together" — rendering them with the same message would tell the
	// operator to upgrade a binary that is not the problem (Rule 15).
	refuse string
}

// workerIdRetiredFlag is the RETIRED explicit-row selector
// (`gopi worker --id <task-id>`): the row is a POSITIONAL argument now
// (`gopi worker <task-id>`) — a task id cannot be mistaken for anything else
// this verb accepts, so naming it needed no keyword. The constant KEEPS its
// value for the same reason runcmd.VerbRun keeps its own: a command that was
// RECORDED while the flag was live (a run_cmd, a dispatch log, a script) must
// still be RECOGNIZED — and recognized here means refused WITH the replacement
// named (Rule 15: never silently ignored). Nothing renders it any more; the
// scanner's retired-spelling arm and the refusal message are its only readers.
//
// Its value comes from internal/runcmd.FlagID — the single home for a spelling
// that TWO packages compose a recorded command from (Rule 14).
const workerIdRetiredFlag = runcmd.FlagID

// workerPositionalSyntax is the task-id placeholder as it appears in EVERY
// operator-facing string this verb renders: the grammar synopsis, the
// contradiction refusals, the usage head, the `--id` remedy — and the path
// placeholder `<gopi-dir>/sessions/<task-id>/output.log`, with which it shares
// the spelling by design (one placeholder, one home — Rule 1b/13). A positional
// has no flag word to reference, so its SHAPE gets a name instead of being
// re-typed in a dozen format strings; the ban on re-typing it is enforced by
// worker_positional_syntax_gate_test.go, which reads every production `.go`
// file in this package.
const workerPositionalSyntax = "<task-id>"

// workerRunFlags are the run-shaped flags the EXPLICIT face accepts (they are
// the flags the retired `gopi task run <task-id>` carried, moved to the runner
// with the function). They are meaningless without a row — the loop claims its
// own rows and takes no holder identity — so the scanner refuses one that
// arrives alone (Rule 15: a silently ignored option is the degradation the
// refusal exists for).
//
// The ROW itself is not a member: it is POSITIONAL (`gopi worker <task-id>`),
// and workerIdRetiredFlag is absent too — the old flag spelling is a refusal,
// never part of the vocabulary.
var workerRunFlags = []string{workerFlagTakeover, workerFlagAs, workerFlagDB, flagReportTo, flagRepo}

// workerFlagTakeover / workerFlagAs / workerFlagDB / flagReportTo / flagRepo are
// those flags' spellings. They are the values internal/runcmd records in a
// run_cmd, so the run-shaped grammar and the recorded command can never drift.
const (
	workerFlagTakeover = "--takeover"
	workerFlagAs       = "--as"
	workerFlagDB       = "--db"
	flagReportTo       = runcmd.FlagReportTo
	flagRepo           = runcmd.FlagRepo
)

// workerRunFlagValue reports whether `name` (a flag's name WITHOUT the leading
// dashes) is a VALUE-carrying member of the explicit face, i.e. one whose
// operand is the next argv word. `--takeover` is the ONE member that is a bare
// switch, so it is excluded here; answering true for the whole face made the
// scanner eat the word AFTER `--takeover` (`--as`) as its operand and refuse
// the launch, leaving the flag the usage head advertises unpassable. Membership
// comes from workerRunFlagSpelling (one list, Rule 1b/13), read by the scanner
// and by the daemon's re-exec builder so the two cannot disagree about what an
// operand is.
func workerRunFlagValue(name string) bool {
	spelling := workerRunFlagSpelling(name)
	return spelling != "" && spelling != workerFlagTakeover
}

// workerRunFlagSpelling returns the `--flag` spelling of a name given without
// dashes, or "" when the name is not a member of the explicit face.
func workerRunFlagSpelling(name string) string {
	for _, f := range workerRunFlags {
		if name == strings.TrimPrefix(f, "--") {
			return f
		}
	}
	return ""
}

// scanWorkerArgs walks the worker verb's argv (argv WITHOUT the verb word) and
// returns what the face must act on, plus the FIRST unrecognized word ("" when
// the argv is accepted).
//
// ONE scanner for both consumers (Rule 2/14): the CLI face renders its refusal
// from the word this returns, and the daemon's pre-detach validation
// (workerDaemonSpec.prep) re-runs the SAME scan — so a word can never be
// accepted in one path and refused in the other.
//
// The grammar is deliberately tiny: the positionals this verb takes ARE the
// row (`gopi worker <task-id>`, added by epic worker-positional-taskid), the
// recognized flags above carry the rest of the vocabulary, and every OTHER
// word — a dash-flag this binary does not know, a second positional — is
// refused (Rule 15: a silently ignored option is the degradation the refusal
// exists for).
//
// TWO FAMILIES (epic worker-run-entry, 2026-09-16), one runner:
//
//	the LOOP flags      --once, --daemon[=…], the daemon seam flags
//	the EXPLICIT row    <task-id> POSITIONAL, plus the run-shaped options that
//	                    only make sense beside it: --takeover, --as <holder>,
//	                    --db <path>, --report-to <alias>, --repo <target>
//
// The explicit family is the flag set the retired `gopi task run <task-id>`
// carried: the selection moved to the runner, so its options moved with it
// rather than disappearing. A value flag always consumes the NEXT word as its
// operand, so an arbitrary holder/path is never mistaken for a stray argument;
// the ROW needs no such rule because a task id is unmistakable (and a second
// positional is refused rather than folded into one).
func scanWorkerArgs(args []string) (workerArgs, string) {
	var wa workerArgs
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value := workerFlagNameValue(a)
		switch {
		case a == workerFlagOnce:
			wa.once = true
		case daemonSeamFlag(a):
			// The daemon SEAM's own flags (--require-scope): part of the
			// --daemon vocabulary, consumed by the seam, never forwarded to the
			// child (workerDaemonLoopArgs transcribes loop flags only). One
			// predicate, shared with the merger face.
			wa.seam = true
			wa.seamRequired = wa.seamRequired || a == daemonScopeRequireFlag
		case a == workerDaemonFlagLong || strings.HasPrefix(a, workerDaemonFlagLong+workerFlagValueSep):
			wa.daemon = true
		case workerResumeSpelling(a):
			// The RESUME family (--resume, --branch, --worktree): the mode
			// switch and its two ground selectors are consumed by their own arm,
			// which reports how many words it took (the bare switch takes none;
			// a selector takes its operand from the next word or from
			// `--flag=VALUE`).
			if delta := scanWorkerResumeFlags(args, i, &wa); delta > 0 {
				i += delta
			}
		case workerIdRetiredSpelling(a):
			// The RETIRED selector (`--id <task-id>` / `--id=<task-id>`). The
			// row is POSITIONAL now, so this arm exists for ONE purpose: to
			// recognize the old spelling and REFUSE it with the replacement
			// named (Rule 15 — a command recorded while the flag was live must
			// still be understood as "you mean this, spelled that way", never
			// silently ignored). Yielding it as the offending word routes the
			// caller through the single refusal renderer, which special-cases
			// this spelling.
			return wa, a
		case !strings.HasPrefix(a, "-"):
			// The EXPLICIT row, POSITIONAL: `gopi worker <task-id> …`. A task
			// id cannot be mistaken for anything else this verb accepts — the
			// only other positional IS this one — so the selector needs no
			// keyword. A SECOND positional is refused (Rule 15: a stray word
			// must never be dropped), which is what keeps one-row-per-invocation
			// true.
			if wa.explicit != "" {
				return wa, a
			}
			wa.explicit = a
		case workerRunFlagSpelling(name) != "":
			// A run-shaped flag. `--takeover` is a bare switch; every other
			// member carries an operand, taken from `--flag=VALUE` or from the
			// next word (which must exist — a flag with a missing operand is a
			// refusal, not a silent empty value).
			if workerRunFlagValue(name) && value == "" {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
					return wa, a
				}
				value = args[i+1]
				i++
			}
			switch name {
			case strings.TrimPrefix(workerFlagTakeover, "--"):
				wa.takeover = true
			case strings.TrimPrefix(workerFlagAs, "--"):
				wa.as = value
			case strings.TrimPrefix(workerFlagDB, "--"):
				wa.db = value
			case strings.TrimPrefix(flagReportTo, "--"):
				wa.reportTo = value
			case strings.TrimPrefix(flagRepo, "--"):
				wa.repo = value
			}
		default:
			return wa, a
		}
	}
	wa.refuse = wa.validate()
	return wa, ""
}

// workerIdRetiredSpelling reports whether an argv word is the RETIRED row
// selector — `--id <task-id>` or `--id=<task-id>`. One predicate for the
// scanner arm and anything else that must recognize the old grammar (a
// dispatch script's argv), so "which spelling was that" has one answer.
func workerIdRetiredSpelling(a string) bool {
	return a == workerIdRetiredFlag || strings.HasPrefix(a, workerIdRetiredFlag+workerFlagValueSep)
}

// workerFlagNameValue splits one argv word into its flag NAME (without the
// leading dashes) and its `--flag=VALUE` operand ("" when the operand rides the
// next word, or when the word is a bare switch or a positional).
func workerFlagNameValue(a string) (name, value string) {
	if !strings.HasPrefix(a, "--") {
		return "", ""
	}
	name = strings.TrimPrefix(a, "--")
	if eq := strings.Index(name, workerFlagValueSep); eq >= 0 {
		value = name[eq+1:]
		name = name[:eq]
	}
	return name, value
}

// validate refuses the argv SHAPES that would otherwise be silently meaningful:
//
//	--once with a row    two contradictory selections (drain the queue vs run
//	                     THIS row). Accepting both would make the behaviour
//	                     depend on an ordering nobody can see (Rule 15).
//	a run flag without a row  --as/--db/--takeover/--report-to/--repo describe a
//	                     NAMED run; the loop claims its own rows and has no
//	                     holder identity to set, so a lone one is a dropped
//	                     option — the exact silent degradation the scanner's
//	                     unknown-word refusal exists to prevent.
//	--daemon with a row  a detached run of one named row is not a thing: the
//	                     daemon face is the resident loop (and its single
//	                     instance lock would fight the loop's own claim set).
//	an option-shaped --report-to / --repo
//	                     the two flags that become argv ELEMENTS OF SSH later
//	                     (the remote writeback: `ssh <alias> …`, `git push
//	                     <repo> …`). ssh parses a leading "-" as an OPTION, so
//	                     `--report-to -oProxyCommand=…` is a one-element
//	                     option injection, not a host name. The retired
//	                     `gopi task run <id>` refused this at parse time
//	                     (peerFlagError / ValidateSSHGitTarget) and the
//	                     explicit face INHERITED the flags — so the check must
//	                     move with them, or the rework silently WEAKENS an
//	                     injection guard while looking like a pure rename
//	                     (internal/tools/peer_alias.go; the cmd/agent half of
//	                     the fix).
//
// The returned string is the refusal SENTENCE ("" means accepted), because a
// contradiction needs more than "unexpected argument": the operator has to be
// told which of the two selections to drop, or which flag needs a row.
func (wa workerArgs) validate() string {
	// The resume family's own refusals come FIRST: `--resume` is a mode, so a
	// contradictory argv inside it must be named as such ("this mode's ground is
	// missing") rather than fall through to the run-flag arm below, which would
	// report `--branch` as a flag needing a task id — a true sentence about the
	// wrong question.
	if wa.resume {
		if r := workerResumeRefusal(wa); r != "" {
			return r
		}
		for _, f := range workerRunFlags {
			if workerArgSets(f, wa) {
				return fmt.Sprintf("gopi worker: %s is the ORDINARY named-run path's option — %s takes the ground "+
					"(and derives the row from it), so it cannot also take a claim/holder/ledger override\n"+
					"  run the row through the ordinary path instead: `gopi worker "+workerPositionalSyntax+"`\n",
					f, workerFlagResume)
			}
		}
		return ""
	}
	if r := workerResumeOnlyFlag(wa); r != "" {
		return r
	}
	if wa.once && wa.explicit != "" {
		return fmt.Sprintf("gopi worker: %s and the row %s ask for different work — %s drains the QUEUE, a row runs exactly that one\n"+
			"  pick one: `gopi worker %s` (drain) or `gopi worker "+workerPositionalSyntax+"` (that row)\n",
			workerFlagOnce, wa.explicit, workerFlagOnce, workerFlagOnce)
	}
	if wa.daemon && wa.explicit != "" {
		return fmt.Sprintf("gopi worker: %s with the row %s is not a thing — %s detaches the resident LOOP, which claims its own rows\n"+
			"  run the named row in the foreground instead: `gopi worker "+workerPositionalSyntax+"`\n",
			workerDaemonFlagLong, wa.explicit, workerDaemonFlagLong)
	}
	if wa.explicit == "" {
		for _, f := range []string{workerFlagTakeover, workerFlagAs, workerFlagDB, flagReportTo, flagRepo} {
			if workerArgSets(f, wa) {
				return fmt.Sprintf("gopi worker: %s describes a NAMED run — it needs a task id: `gopi worker "+workerPositionalSyntax+"`\n"+
					"  (the plain loop claims its own rows and has no holder identity to set)\n",
					f)
			}
		}
	}
	// A MALFORMED value is a refusal in every spell: an option-shaped ssh peer
	// alias / git target is exactly the argv element that must never reach the
	// transport, and an unknown row on the wire is cheaper to refuse here. An
	// ABSENT flag is not a malformed one — the check runs only on a value that
	// was actually given (Rule 15's mirror image: never refuse a word the
	// caller did not say).
	if wa.reportTo != "" || wa.repo != "" {
		if err := peerFlagError(flagReportTo, wa.reportTo); err != nil {
			return fmt.Sprintf("gopi worker: %v\n", err)
		}
		if err := repoFlagError(flagRepo, wa.repo); err != nil {
			return fmt.Sprintf("gopi worker: %v\n", err)
		}
	}
	return ""
}

// repoFlagError is the --repo half of peerFlagError (the git push-back target a
// dispatched run carries): a TARGET carrying a ":" is an ssh git URL, so its
// alias half must survive the same option-shaped refusal.
func repoFlagError(flag, repo string) error {
	if !strings.Contains(repo, ":") {
		return nil
	}
	if _, err := tools.ValidateSSHGitTarget(repo); err != nil {
		return fmt.Errorf("%s: %w", flag, err)
	}
	return nil
}

// workerArgSets reports whether the parsed argv actually carried flag f — the
// predicate behind validate's "a run flag without a task id" arm, so the arm and the
// parse cannot disagree (Rule 13: one list, read twice).
func workerArgSets(f string, wa workerArgs) bool {
	switch f {
	case workerFlagTakeover:
		return wa.takeover
	case workerFlagAs:
		return wa.as != ""
	case workerFlagDB:
		return wa.db != ""
	case flagReportTo:
		return wa.reportTo != ""
	case flagRepo:
		return wa.repo != ""
	}
	return false
}

// workerDaemonChildArgs is the child's argv: argv[0]=the stable detached-process
// name, then the worker verb, then the loop flags — the SAME worker entry point
// reached with --daemon consumed. One builder, so the launch and any test
// asserting the child's shape cannot diverge.
func workerDaemonChildArgs(args []string) []string {
	return append([]string{daemonSpecArgv0(CliVerbWorker), CliVerbWorker}, workerDaemonLoopArgs(args)...)
}

// workerDaemonLoopArgs transcribes ONLY the loop flags workerDaemonArgs names
// (Rule 1: the list is a constant, so a new flag cannot be forgotten here and
// `--daemon` can never leak into the child — the recursion is structurally
// impossible). Unknown flags are ignored on purpose: scanWorkerArgs already
// refused them upstream.
func workerDaemonLoopArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		name := strings.TrimPrefix(a, "--")
		value := ""
		if eq := strings.Index(name, workerFlagValueSep); eq >= 0 {
			value = name[eq+1:]
			name = name[:eq]
		}
		keep := false
		for _, want := range workerDaemonArgs {
			if name == strings.TrimPrefix(want, "--") {
				keep = true
				break
			}
		}
		if !keep {
			continue
		}
		out = append(out, "--"+name)
		if value != "" {
			out = append(out, value)
			continue
		}
		// `--flag value` pair: take the next element when it is not itself a
		// flag (the same rule the scanner applies).
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			out = append(out, args[i+1])
			i++
		}
	}
	return out
}

// workerDaemonChild reports whether THIS process is a self-detached worker
// daemon child (workerDaemonEnv is present, which only workerDaemonSpawn sets).
func workerDaemonChild() bool {
	return daemonChild(workerDaemonEnv)
}

// workerDaemonSpec is the worker's specification of the shared daemonization
// mechanism (cli_daemon_spawn.go).
//
// control != nil: the DAEMON face is a SINGLETON. control is acquireWorkerLock
// — the flock on <gopi data root>/worker.lock (cmd/agent/cli_worker_control.go;
// worker.lock is a SEPARATE file from the merger's serve.lock, so a worker and
// a merger never refuse each other). The SPAWNING PARENT takes the lock, so a
// second start is refused synchronously, and the child inherits it via
// ExtraFiles, re-takes it on the INHERITED fd (workerDaemonInheritLock — never a
// re-open by path) and CloseOnExec's it. readyChildFD is therefore
// daemonReadyChildFDCtrl: with a control fd at fd 3 the handshake pipe is fd 4
// (daemonReadyChildFDSolo, fd 3, is the number for a face with NO control fd —
// no current face uses it; the mechanism still supports both).
//
// The FOREGROUND face (`gopi worker`, no --daemon) takes NO lock: the lock
// gates the RESIDENT daemon, never a foreground worker a debugger may want to
// watch (workerUsageHead's "at most ONE detached worker daemon per profile").
//
// HISTORICAL (do NOT restore): this comment used to say "control is nil — the
// worker has NO single-instance lock (multi-instance is by design, and the
// briefing forbids adding one), hence daemonReadyChildFDSolo". That was the
// PRE-singleton contract: the worker-daemon-singleton epic superseded "the
// worker is multi-instance by design" for the DAEMON face (devdoc/
// worker-daemon-singleton.md), and the fd follows from control, not from a
// policy. The old text described a face that no longer exists.
func workerDaemonSpec() daemonSpec {
	return daemonSpec{
		component: CliVerbWorker,
		prog:      workerProgTopLevel,
		markerEnv: workerDaemonEnv,
		// The pre-detach validation is the SAME scan the CLI face ran a moment
		// ago (one predicate, two callers): it is a safety net that keeps the
		// invariant "the parent validates before anything is detached" true by
		// construction rather than by convention.
		prep: func(args []string) error {
			wa, bad := scanWorkerArgs(args)
			if bad != "" {
				// ONE renderer for the refusal shape (main.go): the parent must
				// refuse with the same words the foreground face prints, so a
				// launcher reading either path learns the same thing.
				return errors.New(strings.TrimRight(workerUnexpectedArgMessage(bad), "\n"))
			}
			if wa.refuse != "" {
				// The contradiction is the scanner's to name (Rule 2: one
				// reader, one sentence) — the parent must refuse with the same
				// words the foreground face would print.
				return errors.New(strings.TrimRight(wa.refuse, "\n"))
			}
			return nil
		},
		control:      acquireWorkerLock,
		readyChildFD: daemonReadyChildFDCtrl,
		childArgv:    workerDaemonChildArgs,
		startDetail:  workerDaemonStartDetail,
		logPath:      workerDaemonLogPath,
	}
}

// workerDaemonLogPath is where a detached worker's stdio lands:
// <gopi data root>/logs/worker.log (GOPI_HOME aware through the shared
// daemonLogPathUnder, like every other gopi-data path — a sandboxed worker
// never writes into the real profile).
func workerDaemonLogPath() (string, error) {
	return daemonLogPathUnder(workerDaemonLogBaseName)
}

// workerDaemonInheritLock re-takes the flock the parent handed over as
// daemonControlChildFD. The lock is taken DIRECTLY from the inherited fd (no
// re-open, no path re-resolution): the fd refers to the very file the parent
// locked, and the parent's flock died with the parent's own fd, so this child
// is the sole owner from here on.
//
// OWNERSHIP: the descriptor is BORROWED — os/exec dup2'd the parent's lock file
// onto this number for the child (ExtraFiles), so this process never opened it.
// The REFUSAL exits below therefore hand the wrapper to borrowedfd.Detach:
// dropping it would let its finalizer close the parent's descriptor, and the
// kernel would RECYCLE the number onto an unrelated live handle (the measured
// cmd/agent EBADF shape). The SUCCESS exit deliberately does NOT detach — it
// returns the wrapper and the daemon keeps it as its own handle for life
// (workerDaemonLockRef), so no finalizer may outlive this call's decision to
// hand it over.
//
// The re-take ALSO fences the lock in (syscall.CloseOnExec): the inherited fd
// is an ExtraFiles dup with no O_CLOEXEC, so without the fence every process the
// daemon execs (the run child, its build/test grandchildren) would carry a copy
// of the lock fd — a flock lives on the open file description and dies only
// with the LAST copy, so one such survivor would keep the single-instance lock
// held by a DEAD pid and refuse every later start (the merger's 2026-09-12
// incident, whose fix this reuses verbatim).
func workerDaemonInheritLock() (*os.File, error) {
	return workerDaemonInheritLockAt(daemonControlChildFD)
}

// workerDaemonInheritLockAt is workerDaemonInheritLock against an EXPLICIT
// descriptor number, for the same reason mergerTakeoverLockAdoptAt takes one: a
// Go test process has fd 3 occupied by the harness, so a body welded to
// daemonControlChildFD could only be exercised by a test that clobbers the
// suite's own descriptor. The number is a parameter here and the entry point
// above forwards the production constant.
//
// OWNERSHIP: BORROWED (see the note on the entry point). The three refusal exits
// hand the wrapper to borrowedfd.Detach — the ONE definition of the retention
// idiom (Rule 14) — because a dropped borrowed wrapper's finalizer closes the
// parent's descriptor. The success exit deliberately does not.
func workerDaemonInheritLockAt(fd int) (*os.File, error) {
	inherited := os.NewFile(uintptr(fd), workerLockFilename)
	if inherited == nil || inherited.Fd() == ^uintptr(0) {
		borrowedfd.Detach(inherited) // nil-tolerant: nothing to forget when there is no wrapper
		return nil, fmt.Errorf("inherited worker lock fd %d is not open", fd)
	}
	if _, err := inherited.Stat(); err != nil {
		borrowedfd.Detach(inherited)
		return nil, fmt.Errorf("inherited worker lock fd %d is not a usable file: %w", fd, err)
	}
	deadline := time.Now().Add(workerDaemonLockWait)
	for {
		err := syscall.Flock(int(inherited.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			syscall.CloseOnExec(int(inherited.Fd()))
			return inherited, nil
		}
		if time.Now().After(deadline) {
			// This refusal is the one an ordinary contended start takes, so it is
			// the path that fires in production: without the detach the wrapper
			// dies here and its finalizer closes the inherited lock descriptor.
			borrowedfd.Detach(inherited)
			return nil, fmt.Errorf("inherited worker lock fd %d not acquirable within %s: %w",
				fd, workerDaemonLockWait, err)
		}
		time.Sleep(workerDaemonLockPoll) //stylegate:allow-sleep bounded poll for the inherited worker flock
	}
}

// workerDaemonRestampPID writes the DAEMON's own pid into the inherited lock
// file. acquireWorkerLock stamped the SPAWNING parent's pid, and that parent
// exits immediately — so without this the refusal message and `--status` would
// point an operator at a dead pid instead of the live daemon. Diagnostic only:
// the flock is what guards, so a failed stamp is reported and never fails the
// start.
func workerDaemonRestampPID(lock *os.File) {
	if err := lock.Truncate(0); err != nil {
		fmt.Fprintf(os.Stderr, "%s: could not restamp the lock pid: %v\n", workerProgTopLevel, err)
		return
	}
	if _, err := lock.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0); err != nil {
		fmt.Fprintf(os.Stderr, "%s: could not restamp the lock pid: %v\n", workerProgTopLevel, err)
		return
	}
	_ = lock.Sync() //stylegate:ignore durability flush of the lock pid; the flock itself is what guards
}

// workerDaemonRun is the child's whole life: take the inherited single-instance
// lock, prove readiness to the parent, then run the ordinary worker loop. It
// never returns (runTaskWorkerWithDaemon loops or exits); the error path reports
// and exits non-zero.
func workerDaemonRun(once bool) {
	// LIFETIME BINDING (opt-in): when a harness handed this daemon the read end
	// of a pipe, a watchdog thread kills THIS process the instant that harness
	// dies — SIGKILL included, with no cleanup hook to run. Armed before the
	// lock handshake so a spawner dying during startup is still caught.
	daemonlifetime.WatchFromEnv()
	readyFD := daemonReadyFD()
	lock, err := workerDaemonInheritLock()
	if err != nil {
		daemonNote(workerProgTopLevel, readyFD, daemonFailPrefix+err.Error())
		fmt.Fprintf(os.Stderr, workerDaemonChildFailNote, workerProgTopLevel, err)
		os.Exit(1)
	}
	// The lock file must now name THIS process — the parent's pid is about to
	// die. The fd is deliberately NOT closed for the daemon's life: the flock IS
	// the single-instance guard.
	workerDaemonRestampPID(lock)
	runTaskWorkerWithDaemon(once, lock)
}

// workerDaemonLockRef keeps the inherited single-instance flock REACHABLE for
// the daemon's whole life. This is not bookkeeping: an *os.File that becomes
// unreachable is finalized by the GC, which CLOSES the descriptor — and flock
// lives on the open file description, so closing the last copy would silently
// RELEASE the single-instance guard mid-run (the daemon would keep sweeping
// while a second daemon could now start). The reference must therefore outlive
// the loop and be touched by nothing.
//
// It is ALSO the descriptor runTaskWorkerLoop's per-round identity guard
// re-proves against the path — nil for the foreground face, which holds no
// daemon lock to guard.
var workerDaemonLockRef *os.File

// The per-round identity guard's operator-facing lines, named once (Rule 1b:
// they are read by an operator and matched by tests, never typed inline twice).
const (
	// workerDaemonGuardDownNote is printed the moment the daemon discovers its
	// guarded inode is no longer the one worker.lock names.
	workerDaemonGuardDownNote = "%s: single-instance lock %s was replaced underneath the daemon — the guard is DOWN; stray holder %s; re-acquiring\n"
	// workerDaemonGuardRestoredNote is the repair's success line.
	workerDaemonGuardRestoredNote = "[worker] guard restored: %s is again LOCKED by this daemon (pid %d)\n"
	// workerDaemonGuardLostNote is the STOP line: another process holds the
	// fresh file, so continuing would sweep the queue beside it.
	workerDaemonGuardLostNote = "%s: %v — stopping instead of working unguarded\n"
)

// workerDaemonGuardIdentity re-proves, once per round, that the flock this
// daemon holds is still the file worker.lock NAMES — and repairs or stops when
// it is not. It returns true when the daemon must STOP.
//
// WHY THIS EXISTS (serve-lock race, 2026-09-17): `flock` lives on the INODE,
// and it is NOT invalidated by an unlink. An `rm worker.lock` (or any
// unlink+recreate) while the daemon runs leaves that daemon locking an inode no
// name points at, while the path gets a FRESH inode whose lock is free — so the
// next daemon's acquireWorkerLock SUCCEEDS and two resident workers sweep the
// same queue. The Merger has had this re-check since the 2026-09-12 incident
// (ensureServeLockIdentity, once per tick); the worker's copy of the same lock
// machinery had the acquisition-time checks only, so the same unlink turned a
// resident worker into an unguarded sweeper for the rest of its life. Measured
// 2026-09-17: the replacement leaves the holder's flock intact on the OLD inode
// (see TestServeLockReplacementMechanism), which is why the daemon cannot
// notice this by looking at its own descriptor — only a comparison against the
// PATH can see it, and only a per-round one can see it in time.
//
// The repair is the merger's, verbatim in shape: report the stray holder, drop
// the detached lock (its inode is unreachable — worthless), take a FRESH lock
// on the current path (acquireWorkerLock re-proves identity and stamps our pid),
// and STOP when another process already owns the fresh file — at that instant a
// second daemon is sweeping, and stopping is the only safe move.
//
// An UNPROVABLE state (the path or the comparison cannot be resolved) never
// stops the daemon and never passes silently: it is announced and the round
// proceeds under the lock we hold (Rule 15 — the conservative half, and the
// same choice ensureServeLockIdentity makes).
func workerDaemonGuardIdentity() bool {
	lock := workerDaemonLockRef
	if lock == nil {
		return false // the foreground face holds no daemon lock to guard
	}
	path, err := workerLockPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot verify the single-instance lock (%v) — continuing under the held lock\n", workerProgTopLevel, err)
		return false
	}
	held, err := sameFileAsPath(lock, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot verify the single-instance lock (%v) — continuing under the held lock\n", workerProgTopLevel, err)
		return false
	}
	if held {
		return false
	}
	fmt.Fprintf(os.Stderr, workerDaemonGuardDownNote, workerProgTopLevel, path,
		strayHolderNote(tools.OrphanLockHolder(path)))
	lock.Close() //stylegate:ignore the detached flock's inode is unreachable by name; closing it is the correct repair step before re-acquiring
	reacquired, err := acquireWorkerLock()
	if err != nil {
		fmt.Fprintf(os.Stderr, workerDaemonGuardLostNote, workerProgTopLevel, err)
		return true
	}
	workerDaemonLockRef = reacquired
	fmt.Printf(workerDaemonGuardRestoredNote, path, os.Getpid())
	return false
}

// runTaskWorkerWithDaemon runs the ordinary worker loop on the inherited lock:
// the lock is retained (see workerDaemonLockRef) and the loop then behaves
// exactly as the foreground face does — same rounds, same drain-and-self-die
// contract, only the control surface differs.
func runTaskWorkerWithDaemon(once bool, lock *os.File) {
	workerDaemonLockRef = lock
	runTaskWorkerLoop(once)
}

// workerDaemonSpawn detaches a worker daemon, exiting with the named failure
// when it cannot start (the `--daemon` face's own contract).
func workerDaemonSpawn(args []string) {
	daemonSpawnOrExit(workerDaemonSpec(), args)
}

// workerDaemonSpawnErr is the RETURNING spelling, for the control face: a
// --restart must be able to tell "the start failed" from "a concurrent restart
// already won the flock" (see daemonSpawn's doc and workerControlEnsureDaemon).
// Its first result is daemonSpawn's reuse flag: true means the daemon was
// ALREADY there (this profile's own live daemon held the scope unit), so the
// caller must not look for a fresh process it never started.
func workerDaemonSpawnErr(args []string) (bool, error) {
	return daemonSpawn(workerDaemonHandoverSpec(), args)
}

// workerDaemonHandoverSpec is workerDaemonSpec with the HANDOVER acquisition:
// the control fd is WAITED FOR (bounded) instead of being taken on the first
// attempt.
//
// WHY THE RESTART PATH NEEDS IT (measured 2026-10-04, epic
// cmd/agent-restart-lock-handover): `--restart` stops the predecessor and then
// starts the successor, and the successor's start is a HANDOVER — it is asking
// the very flock its predecessor is in the middle of releasing. Taking it must
// therefore be a bounded wait, not a single LOCK_NB attempt: the predecessor's
// teardown is ASYNCHRONOUS. `workerWaitForExit` observes the daemon's pid
// DISAPPEAR (a signal-0 + /proc/<pid>/stat state-letter read), but the kernel
// releases the flock when the file's last descriptor is closed, and between
// those two facts the predecessor still runs its exit path (state write, request
// cleanup, the loop's own unwind, plus ordinary scheduler delay). The failing
// shape, verbatim from the saturation run: "pid 322819 exited 501ms after the
// graceful stop request" followed by "daemon start failed: another worker daemon
// is already running (lock … held by pid 322819 (no such process: the flock
// outlived its owner …))" — the successor was refused by a lock whose owner the
// SAME command had just watched exit.
//
// The mechanism is not new: daemonSpec.controlWait is the documented handover
// primitive ("a stop/restart is a REQUEST the old daemon answers at its own pass
// boundary: the successor cannot take the flock the instant it starts, and
// refusing it would make --restart fail for doing exactly what it was asked to
// do. The wait is the HANDOVER primitive — the ordering lives in the flock, so
// there is no sleep and no 'quiet window' to tune"). The merger's spec was its
// only user; the worker's restart needs the same wait and never had it.
//
// The SINGLETON contract is untouched: only this spec waits, and only the
// restart path (`workerDaemonSpawnErr`) uses it. A plain `gopi worker --daemon`
// keeps the IMMEDIATE acquisition (workerDaemonSpec), so "a second resident
// worker" is still refused at once and by name.
func workerDaemonHandoverSpec() daemonSpec {
	spec := workerDaemonSpec()
	spec.controlWait = acquireWorkerLockWait
	return spec
}

// workerDaemonStartDetail describes what the detached worker will do, for the
// start note. Reporting only — the child decides from its own argv — but the
// line must not LIE about it: a detached one-shot and a detached idle-exiting
// loop are different lifetimes, and the operator reads this line to know which
// one they just started (and how long it is expected to live).
func workerDaemonStartDetail(args []string) string {
	if wa, bad := scanWorkerArgs(args); bad == "" && wa.refuse == "" && wa.once {
		return "--once (drain the queue, then exit 0)"
	}
	// The idle self-exit budget is the loop's drain-and-self-die contract; the
	// numbers come from its OWN resolvers (Rule 2: one reader each) so this
	// line cannot drift from what the loop will really do.
	if workerIdleExits() == 0 {
		return fmt.Sprintf("idle wait %s between rounds, never self-exits (GOPI_WORKER_IDLE_EXITS=0)",
			workerIdleInterval())
	}
	return fmt.Sprintf("idle wait %s between rounds, self-exits after %d idle round(s)",
		workerIdleInterval(), workerIdleExits())
}

// workerDaemonNoteReady is the child's half of the handshake: it writes the
// single readiness byte to the launcher and CLOSES the fd, then the caller
// enters the ordinary loop.
//
// It is called at the LAST point where nothing has failed yet (the output
// root is resolved, the argv is understood) — the parent's "started" verdict
// must be backed by a process that is provably about to run, not by a fork
// that succeeded. A failure to report is announced instead of silently
// swallowed: the launcher would otherwise wait out daemonHandshakeWait and
// then report a failure that did not happen (Rule 15).
// OWNERSHIP: the descriptor is BORROWED (its number arrives through
// daemonReadyEnv, encoded by the launcher — this process never opened it), and
// the wrapper is CLOSED on every exit (`defer f.Close()`), which is correct and
// needs no detach: os.File.Close clears the finalizer (GOROOT/src/os/file_unix.go),
// so the close is explicit and happens once, after the handshake byte is
// written. Only a DROPPED borrowed wrapper needs borrowedfd.Detach.
func workerDaemonNoteReady() {
	readyFD := daemonReadyFD()
	if readyFD < 0 {
		// A hand-started child (no pipe): there is no launcher to prove
		// anything to, so this is not an error — just no handshake.
		return
	}
	f := os.NewFile(uintptr(readyFD), daemonReadyEnv)
	if f == nil {
		fmt.Fprintf(os.Stderr, workerDaemonChildFailNote, workerProgTopLevel,
			fmt.Errorf("readiness fd %d is not usable", readyFD))
		return
	}
	defer f.Close()
	if _, err := f.WriteString(daemonReadyByte); err != nil {
		fmt.Fprintf(os.Stderr, workerDaemonChildFailNote, workerProgTopLevel, err)
	}
}
