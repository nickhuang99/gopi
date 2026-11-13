// Package runcmd owns the RUN-PROVENANCE vocabulary of the task ledger and
// the ONE renderer that turns a launcher argv into the shell-quoted,
// re-runnable `run_cmd` string `gopi task show` prints.
//
// It is a LEAF constant package (Rule 14 — the internal/toolnames
// precedent): stdlib-only imports, zero project dependencies, so every
// consumer of this repo can import it without an import cycle, and no
// consumer mirrors the values or types them inline (Rules 1b/13).
//
// WHY IT IS SHARED: `run_cmd` is RENDERED where the real argv exists
// (cmd/agent's launch path) and STAMPED/READ in internal/tools (the
// TaskStampRun face, the merger's resolve rows, the task-show renderer).
// Before this package each site hand-built the string — which is exactly
// how a hand-written PSEUDO command ("gopi worker (claim→run round,
// target <id>)") came to be stored on rows whose whole purpose is to be
// pasted into a shell and re-run. One renderer, one method vocabulary.
package runcmd

import (
	"os"
	"path/filepath"
	"strings"
)

// Run-provenance methods — the value of the tasks.run_method column. They
// name WHICH launcher path executed the row (the enum is documented at
// TaskStampRun in internal/tools/tasks.go); consumers must reference these
// constants instead of bare literals.
//
// Since epic worker-run-entry (2026-09-16) `gopi worker` is the ONLY command
// that executes a row, so MethodWorker is what a CURRENT run stamps — the loop
// face (plain `gopi worker` / `--once`) and the named-row face
// (`gopi worker <task-id>`, positional since the epic worker-positional-taskid)
// alike. The remaining values are preserved
// because the ledger is append-only: rows stamped before the rework keep
// reading truthfully, and `gopi task show` must render them as they were.
const (
	// MethodAuto is a run that created its OWN ledger row at session start
	// (the harness auto-register hook) — a bare prompt/chat/web run that no
	// runner wrapper owns.
	MethodAuto = "auto"

	// MethodChild is a runner CHILD whose wrapper row owns the run (the
	// mirror-row linkage); the method of the PARENT is what run_cmd replays.
	MethodChild = "child"

	// MethodManual is a plain single-row run: historically
	// `gopi task run <id>` / `gopi task run next`, now
	// `gopi worker <task-id>`. The value is unchanged so pre-rework rows
	// keep their meaning; the run_cmd it accompanies carries the CURRENT
	// spelling, because run_cmd must re-run.
	MethodManual = "manual"

	// MethodWorker is one round of the `gopi worker` claim→run loop.
	MethodWorker = "worker"

	// MethodDispatch is a center that handed the row to a remote node over
	// ssh (`gopi worker <id> --report-to <alias>`; historically
	// `gopi task run <id> --report-to <alias>`).
	MethodDispatch = "dispatch"

	// MethodResume is `gopi session resume <raw-log> --task <id>` — a continuation
	// of an interrupted session.
	MethodResume = "resume"

	// MethodMerger is the merger's own conflict-resolver work (the
	// merger-owned `resolve merge conflict: …` rows); the daemon is
	// `gopi task merger` (the deprecated alias is `gopi task serve`).
	MethodMerger = "merger"
)

// TargetNext is the canonical claim-and-run target of a runner that claims its
// own row (`gopi worker`, historically `gopi task run`): the verb claims the
// next eligible pending row itself. An EMPTY target means
// the same thing (see cmd/agent's runTaskRunOnce) — a recorded run_cmd
// substitutes the id the run actually claimed, so the stored command
// re-runs THAT task instead of claiming a fresh one.
const TargetNext = "next"

// CLI words and flags a run_cmd is composed from. They live here because
// TWO packages compose the same command — cmd/agent renders its own launch
// (runMethodFor) while this package's callers stamp the merger's resolve
// rows — and a mirrored copy would drift into a command that no longer
// re-runs. cmd/agent's cliVerbs is the authority for the top-level verbs
// and asserts these values in cmd/agent's contract test (one spelling, two
// packages, a drift guard between them).
const (
	// VerbTask is the `gopi task` group verb.
	VerbTask = "task"
	// VerbRun is the `gopi task run` sub-verb — RETIRED (epic worker-run-entry,
	// 2026-09-16). The constant KEEPS its value because it is the spelling of a
	// command that historically ran rows and is recorded verbatim in run_cmd /
	// dispatch logs: a previously recorded command line must still be readable
	// (and a `gopi task run` typed today must still reach its pointed removal
	// message — taskverbs.RemovedVerbs). New code does NOT render it: running a
	// row is VerbWorker + the POSITIONAL id. NOTE it is not a legal render either — a
	// freshly stamped run_cmd must RE-RUN, and this spelling only prints the
	// removal sentence.
	VerbRun = "run"
	// VerbWorker is the `gopi worker` verb — the claim→run loop.
	VerbWorker = "worker"
	// VerbMerger is the `gopi task merger` verb — the merger that owns
	// the resolve rows.
	VerbMerger = "merger"
	// VerbResume is the resume WORD. NOTE THE SPELLING IT IS NO LONGER: since
	// verb-retirement phase 1 (2026-10-06) the top-level `gopi resume` is
	// REFUSED by name and the live command is the session group's sub-verb
	// (`gopi session resume`). The constant keeps the bare word because the
	// ledger is append-only — a run_cmd recorded while the top-level spelling
	// was live must still be READABLE — and because it is the sub-verb token the
	// group dispatcher switches on. A freshly rendered command line uses
	// ResumeArgv (group word first), never this value alone: a run_cmd must
	// RE-RUN, and `gopi resume …` no longer does.
	VerbResume = "resume"
	// VerbSession is the `gopi session` GROUP verb — the owner of the session
	// views (list/show) and of the four thin sub-verbs (log/stats/raw/resume).
	// It is named here for the same reason VerbTask is: two packages compose the
	// same command line.
	VerbSession = "session"

	// FlagReportTo is the ssh alias the remote reports its outcome back
	// through (a dispatched node's own row records it, so the node's command
	// can be repeated as written).
	FlagReportTo = "--report-to"
	// FlagRepo is the repo half of the same handoff (git push-back target on
	// a run, the work repo on a submit).
	FlagRepo = "--repo"
	// FlagID is the RETIRED explicit-row selector — `gopi worker --id <task-id>`
	// (epic worker-run-entry, 2026-09-16; retired by epic
	// worker-positional-taskid, which made the row a POSITIONAL argument:
	// `gopi worker <task-id>`). The constant KEEPS its value for the same
	// reason VerbRun keeps its own — a command RECORDED while the flag was live
	// (a run_cmd, a dispatch log, a handoff script) must still be recognized,
	// and recognized here means refused WITH the replacement named (Rule 15),
	// never silently ignored. Nothing renders it any more; cmd/agent's
	// retired-spelling arm is its only reader. The spelling lives HERE because
	// the remote-launch command and the ledger's refusal both need the SAME
	// string (one spelling, one home — Rule 14).
	FlagID = "--id"
	// FlagTask is the `--task <id>` linkage a resume carries (which ledger
	// row the continued session belongs to).
	FlagTask = "--task"
	// FlagSession is the `--session <id|raw-log-path>` address of the NO-VERB
	// resume face (`gopi --session <target> ["<prompt>"]`, epic resume-surface
	// convergence step 1) — the shape a resume child is spawned WITH, and the
	// shape its run_cmd records, since step 2.
	//
	// WHY IT IS DECLARED HERE and not taken from internal/cliverbs.SessionFlag:
	// that package's constant is the SAME WORD for a different invocation
	// (`gopi ui_state --compare … --session <id>`) and this package's job is
	// the resume COMMAND LINE. The two spellings are held together by a drift
	// guard in cmd/agent (TestRuncmdFlagAgreesWithCliverbs), the same
	// one-spelling-two-packages convention the verb constants above use.
	FlagSession = "--session"
)

// ResumeArgv is the ONE renderer of a resume command line — the argv a caller
// executes AND the argv a run_cmd records.
//
// WHY IT IS SHARED (verb-retirement phase 1, 2026-10-06). Before the
// retirement, three call sites built this command independently
// (cmd/agent's worker-resume child and its retry spawner, internal/tools'
// dead-holder spawner) and each typed the bare word, because the bare word WAS
// the command. The retirement moved the live spelling under the group verb and
// left the constant readable-but-not-runnable: every site that kept composing
// the old argv would spawn a refuse-and-exit child, and — worse — would STAMP
// the dead command into `run_cmd`, the one field whose whole purpose is to be
// pasted back into a shell. The two halves then disagreed in a way no compiler
// sees: an executed argv and a recorded argv that no longer match.
//
// THE SHAPE IT RENDERS NOW (epic resume-surface convergence, step 2 of 3):
// the NO-VERB face, `gopi --session <target> [--task <id>]`. The reason is the
// same one that moved it the first time, one layer further out: the resume has
// ONE implementation with TWO addresses, and the address a MACHINE should spawn
// is the one an operator would also type without learning where the resume verb
// lives — the bare invocation. `gopi session resume` still dispatches (its
// retirement is step 3, and until then the old face must keep working), so both
// spellings run; this renderer emits the converged one.
//
// WHAT THE SHAPE MUST CARRY, and this is not decoration: the no-verb face takes
// a session id OR a raw-log path as its target (it resolves through the same
// resolveResumeTarget as the verb face) and `--task <id>` for the row linkage.
// Both halves are load-bearing for a SPAWNED child — a worker/merger recovery
// that stopped linking the row would run the resumed session SESSION-ONLY and
// leave the crashed row unclosed, which is exactly the recovery it exists to
// perform.
//
// Parameters: target is the resume target as the caller resolved it (a row's
// session id, a task id, or a raw log path — the face accepts all three),
// taskID the row to link ("" omits the `--task` half).
func ResumeArgv(target, taskID string) []string {
	argv := []string{Launcher(), FlagSession, target}
	if taskID != "" {
		argv = append(argv, FlagTask, taskID)
	}
	return argv
}

// The two ways a launcher publishes the path of the binary it is: the
// build-time variable (nothing sets it yet, so it also documents the
// intended override) and the env var a wrapper exports to its child. Both
// are read at exactly one place (Launcher/SelfPath) — Rule 2.
const (
	// EnvBinary names the gopi executable on PATH/file-system, exported by
	// a launcher so its children (and their run_cmd stamps) can name the
	// REAL binary instead of guessing "gopi".
	EnvBinary = "GOPI_BINARY"
	// EnvSelf is the executed binary as the caller saw it (argv[0] of the
	// launcher process), exported alongside EnvBinary when the caller
	// knows it.
	EnvSelf = "GOPI_SELF"
)

// fallbackGopiName is what a run_cmd says when the launcher cannot name its
// own binary — the conventional command word on PATH. Exported as BinaryName
// because a REMOTE-executed command (a dispatch's `gopi task run …`, which
// runs on the peer node, not here) always spells it this way.
const fallbackGopiName = BinaryName

// BinaryName is the conventional gopi command word: the launcher fallback and
// the name a command that executes on ANOTHER node must use (whose binary is
// the peer's, never this process's path).
const BinaryName = "gopi"

// ContinuationIndent is the two-space indent the task-show renderer uses
// for a qualified field line (e.g. the attachment "  from:" detail).
const ContinuationIndent = "  "

// Field labels of the run provenance as `task show` renders them. One
// spelling for the renderer and for any test that asserts the output.
const (
	// FieldRunMethod is the label of the launcher-method line.
	FieldRunMethod = "run_method"
	// FieldRunCmd is the label of the verbatim-command line.
	FieldRunCmd = "run_cmd"
)

// Command renders an argv as the single re-runnable command line recorded
// in run_cmd: argv[0] is reduced to its BASENAME (the ledger records a
// portable command — `/home/nick/go/bin/gopi worker` re-runs as `gopi
// worker`), every element is shell-quoted when needed, and the elements
// are joined with single spaces. An empty argv renders "" — the honest
// "no command known" value, never a fabricated placeholder.
func Command(argv []string) string {
	parts := make([]string, 0, len(argv))
	for i, a := range argv {
		if i == 0 {
			a = filepath.Base(a)
		}
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ")
}

// Field renders one task-show line: the label, a ':', and the value
// padded to column width (the caller derives width from the widest label
// so every field of a block aligns). An EMPTY value renders nothing at all
// — an absent fact is not printed (an old row must not gain a fictitious
// line), and a value that already reads as text (a run_cmd) is left
// verbatim so the operator can copy it.
func Field(label, value string, width int) string {
	if value == "" {
		return ""
	}
	pad := width - len(label) - 1 // the ':'
	if pad < 1 {
		pad = 1
	}
	return label + ":" + strings.Repeat(" ", pad) + value + "\n"
}

// Launcher returns the command word a run_cmd should start with: the
// launcher's own binary when it published one (EnvBinary, then EnvSelf),
// else this process's executable as the OS reports it (SelfPath — the most
// truthful answer for a process stamping its own row), else the
// conventional "gopi". The value is returned verbatim — Command reduces it
// to a basename and quotes it. A TEST that asserts a literal command pins
// this with EnvBinary, because a test binary is (correctly) named after
// itself.
func Launcher() string {
	if v := strings.TrimSpace(os.Getenv(EnvBinary)); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(EnvSelf)); v != "" {
		return v
	}
	if exe := SelfPath(); exe != "" {
		return exe
	}
	return fallbackGopiName
}

// SelfPath is this process's own executable path — the most truthful
// answer to "which binary ran this row" when no env override is present.
// Returns "" when the OS cannot report it (the caller falls back to
// Launcher()).
func SelfPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// quote shell-quotes one argv element so the rendered command survives a
// paste into a shell: a value made only of shell-safe characters is
// emitted verbatim (the common case — a path-less command word stays
// readable), anything else is single-quoted with the POSIX '\” escape for
// an embedded quote. An empty element becomes ” so it does not vanish.
func quote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`&;()|<>*?[]{}~!") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
