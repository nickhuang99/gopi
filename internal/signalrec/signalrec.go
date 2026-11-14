// Package signalrec is gopi's SIGNAL PROVENANCE recorder — the durable answer
// to "who/what killed this gopi process, and when?".
//
// WHY IT EXISTS (epic: all gopi processes killed by signal, 2026-09-13). Every
// gopi process — a `gopi worker`, the merger daemon, a `gopi task run` child, a
// resolve/verify child — used to die with NOTHING durable left behind but a
// line on a terminal:
//
//	[gopi] terminated received — finalizing session…
//	Error: run interrupted by signal
//
//	Terminated
//
// Nothing in the tree signals another gopi process broadly (every production
// kill is scoped to a process group gopi itself created for ONE child), so the
// kills that emptied the fleet came from outside — and outside is exactly where
// a post-mortem has no evidence: ssh sessions log out, shells exit, and the
// receiver of a signal is never told who sent it. The result was three separate
// multi-hour investigations that all ended in an honest "sender undetermined".
//
// WHAT THIS PACKAGE CAN AND CANNOT DO. Linux does not hand a signal handler the
// sender's identity (no SA_SIGINFO sender pid for SIGTERM in the Go stdlib, and
// auditd is not armed on this host), so the SENDER cannot be named from inside
// the victim. What CAN be recorded, at the instant the signal lands, is the
// victim's own position in the process tree plus the environment that says which
// gopi role it was:
//
//   - pid / ppid / pgid / sid and the PARENT's name + argv + cgroup (the
//     supervisor is the first suspect, and its argv survives here even after
//     the parent itself is gone),
//   - the victim's own cgroup (a scope teardown reaches every member — the
//     mechanism behind the 2026-09-12 cohort kill),
//   - the role markers gopi already exports (session id, task run id, the
//     child-session env) and cwd,
//   - a bounded tail of the process's own session log, so the record says WHAT
//     the run was doing when it was cut.
//
// WHERE THE RECORD GOES — THE OWNING PROCESS'S OWN SESSION DIR. The file used
// to be one public path for the whole host (<gopi dir>/logs/signal-provenance.log),
// which made the artifact the exact thing this epic refuses: a host-global file
// nobody can walk back to the session it belongs to. Every record is ONE JSON
// object on ONE line appended to the SESSION DIR of the process that writes it:
//
//	<gopi dir>/sessions/<ledger task id>/signal-provenance.jsonl   (task-run child, verify child, worker round)
//	<gopi dir>/sessions/<run or session uuid>/signal-provenance.jsonl  (bare prompt, chat, resume)
//	<gopi dir>/sessions/process-<pid>/signal-provenance.jsonl      (a resident role with no task: the merger daemon)
//
// so the record lands beside the very artifacts it explains (session.jsonl,
// *.raw.log, task-report.md) and `ls ~/.gopi/sessions/<id>/` answers "how did
// this run end". A fleet-wide `grep -r '"kind":"signal"' <gopi dir>/sessions`
// still reads every death in one command, because the line shape is unchanged
// — only the directory moved.
//
// NO PUBLIC-DIR FALLBACK, DELIBERATELY. A process that cannot name a session dir
// has its record REFUSED (and the refusal reported), never quietly appended to a
// host-global file: a fallback would recreate the unattributable artifact this
// epic exists to remove, and would do it silently, in the one file read during an
// incident. The alias `~/.gopi/logs/signal-provenance.log` remains only as the
// LEGACY name the one-shot migration moves away from — this package never writes
// it again (pinned by TestSignalRecWritesNothingUnderTheLogsDir).
//
// Records carry pid+ts, which is what lets an INTENT record (below) and a SIGNAL
// record be correlated into one story.
//
// THE INTENT HALF. A process that deliberately kills another gopi process
// (the merger's bounded wait caps are today's only such callers) records an
// "intent" line naming the target pid/group and its reason BEFORE signalling.
// Victim + intent in the same file turns "killed by something" into "killed by
// the merger's verify-child wait cap after 45m0s", which is the difference
// between an incident and a retry.
//
// COST: nothing until a signal arrives — the handler opens the file lazily. A
// signal-free run writes no bytes. A host with no gopi dir, or a read-only one,
// degrades to the old behaviour (the terminal line) and never blocks the
// existing graceful shutdown: recording is best-effort by contract, because
// failing to write evidence must not fail the shutdown that evidence exists to
// explain.
package signalrec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/boolvocab"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/procstat"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionid"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/sigsource"
)

// File / directory names. The log lives beside the merger's and the workers'
// logs, which is where an operator already looks (Rule 1b: named constants, the
// values below are the single spelling).
const (
	// LogDirName is the directory under the gopi data root that holds host
	// component logs (merger.log, scratch-gc-watchdog.log, …). It is the
	// LEGACY provenance home: this package no longer writes there (docs on the
	// package explain why), but the name is kept to (a) name the one-shot
	// migration's source and the legacy-location hint printed when a record is
	// refused, and (b) let internal/standards' exhaustive gate prove NO
	// production file still joins it with LogFileName.
	LogDirName = "logs"

	// LogFileName is the RETIRED public provenance sink name. It is never
	// written again — see the package comment ("no public-dir fallback").
	// Kept as the migration's input file name and the refusal hint's target.
	LogFileName = "signal-provenance.log"

	// FileName is the provenance file of a SESSION dir: one JSON object per
	// line, append-only, beside session.jsonl / *.raw.log / task-report.md.
	FileName = "signal-provenance.jsonl"

	// ProcRootName is the procfs mount point; every process fact below is read
	// from it (never shelled out — no pgrep, no ps).
	ProcRootName = "/proc"
)

// SessionDirEnv is the WIRE contract a parent uses to hand a child the session
// dir its artifacts belong in: <gopi dir>/sessions/<ledger task id>. It exists
// because one resolution is impossible from inside the process — `gopi task
// run` MINTS the child's run uuid at claim time and assigns it to the ledger
// row, so the child's own argv (`gopi task run --task-id <id>`) names the
// ledger id while the session-dir KEY is the uuid. The launcher (cli_tasks.go)
// knows both and exports this variable with the CHILD's environment, before
// spawn — the same single-writer pattern as TaskWrapperEnv / TaskReportPath.
//
// Rule 21: the reader is sessionDest.
const SessionDirEnv = "GOPI_SIGNALREC_SESSION_DIR"

// TaskIDEnv is the companion fact of SessionDirEnv: the LEDGER task id when
// the dir is keyed by a run/session uuid (so the record still carries the row
// the process was executing). Empty is a valid value — a bare prompt has no
// row. Rule 21: the reader is sessionDest.
const TaskIDEnv = "GOPI_SIGNALREC_TASK_ID"

// sessionJSONLSuffix is the session-log extension — the value of
// internal/sessionlayout.JSONLSuffix, the shared classification every READER
// of the sessions tree keys on (Rule 14: one value, one home, so the writer's
// idea of "a session log" and the reader's cannot drift).
const sessionJSONLSuffix = sessionlayout.JSONLSuffix

// ProcessDirPrefix keys the session dir of a resident role that owns no task
// (the merger daemon): <gopi dir>/sessions/process-<pid>. The pid form cannot
// be mistaken for a session uuid or a ledger id, so a reader never wonders
// which id form it is looking at. Exported so a reader (a test, an operator
// surface) composes the same name instead of mirroring the literal (Rule 14).
const ProcessDirPrefix = processDirPrefix

// processDirPrefix is the value ProcessDirPrefix exports (one spelling).
const processDirPrefix = "process-"

// sessionDirPerm is the mode of a session dir this package has to create (the
// process-scope case; every other layout's directory is created by the session
// or task-session machinery, and a MISSING one is a refusal, never something
// this package invents — see sessionDest).
const sessionDirPerm = 0o755

// Destination layouts — which id form keys the session dir. They are recorded
// as the "layout" discriminator so a reader can tell an id-keyed dir from a
// process-keyed one without re-deriving the process's role.
const (
	// LayoutTaskRun: the dir is keyed by the ledger task id (task-run child,
	// merge-verify child, a worker's round).
	LayoutTaskRun = "task-run"
	// LayoutSession: the dir is keyed by a run/session uuid (bare prompt,
	// chat, resume).
	LayoutSession = "session"
	// LayoutProcess: the dir is keyed by this process's own pid — the layout
	// for a resident role that executes no task (the merger daemon).
	LayoutProcess = "process"
)

// Record kinds — the "kind" discriminator of every line. They are the two
// halves of one story, so they are spelled here once and compared by constant.
const (
	// KindSignal is a record written BY A VICTIM, in its signal handler.
	KindSignal = "signal"
	// KindIntent is a record written BY A KILLER, before it signals.
	KindIntent = "intent"
	// KindSignalRepeat is a record written BY A VICTIM for a delivery of a
	// signal kind it has ALREADY recorded — the second and every later arrival
	// of the same signal.
	//
	// WHY IT IS A KIND OF ITS OWN, and not a second KindSignal line. Those two
	// sentences are different facts and the readers of this file act on the
	// difference: a KindSignal line says "a gopi process DIED here", which is
	// what `scripts/night-throughput.sh` counts ("terminations recorded in
	// window") and what an operator's incident grep
	// (`grep -r '"kind":"signal"'`) is looking for. A repeated delivery is NOT a
	// second death — the process is alive and about to die ONCE, of the first
	// delivery — so writing it as kind=signal would inflate a fleet-wide kill
	// into twice as many dead processes as there were, exactly the reading the
	// once-per-kind contract was protecting.
	//
	// WHAT IT REPLACES is not the once-per-kind contract but a SILENT DROP
	// (Rule 15). Before this kind existed, Install's handler answered a repeat
	// with a bare `continue`: no record, no line, no fact at all — so a process
	// that absorbed ANY NUMBER of extra stop signals was indistinguishable from
	// one that was signalled exactly once, and an operator pressing Ctrl-C four
	// times (or a stop path re-sending SIGTERM) could not tell from the evidence
	// whether the second request had arrived, been seen, or been lost.
	//
	// A RE-AFFIRM, NOT AN ESCALATION. The record states that the same request
	// arrived again; it changes nothing about the process's disposition — the
	// first delivery's verdict stands, no re-raise is armed, and no handler is
	// moved (see noteRepeatedSignal).
	KindSignalRepeat = "signal-repeat"
)

// EnvAgentChild marks a process as a GOPI AGENT SESSION born from another gopi
// process's spawn seam (cmd/agent's merger verify child sets it). It is the ONE
// extra fact the destination resolver needs for that role, and it exists
// because nothing else in the child's environment can supply it: the child
// MINTS its own agent session (its session dir is keyed by the uuid it owns),
// so it carries no ledger id — while it stands in a TASK's session dir, which
// the gate resolved and handed to it. The marker is the license to file the
// record in that handed dir instead of refusing it, and it is deliberately
// narrower than "is an agent": it names the spawn seam that MEANS its child's
// artifacts belong to the judged row.
//
// Rule 21: the reader is sessionDest.
const EnvAgentChild = "GOPI_SIGNALREC_AGENT_CHILD"

// EnvOff disarms the recorder ("1"/"true"). It exists for tests and for a host
// that must not write the file, and it is read in Install (Rule 21: a declared
// env constant has a real reader).
const EnvOff = "GOPI_SIGNAL_RECORD_OFF"

// EnvNoForward suppresses the re-raise described on Install: the recorder
// writes its record and then leaves the signal handled (the process keeps
// running). It is read in Install and set by in-process tests, which would
// otherwise be killed by their own victim signal — the recorded-and-still-alive
// case is exactly what a test needs to assert on. Production never sets it.
const EnvNoForward = "GOPI_SIGNALREC_NO_FORWARD"

// defaultForwardGrace is how long the recorder waits between writing the record
// and re-raising the signal with its default disposition: the evidence must
// reach the disk before the process dies, or the incident is traceable in
// theory only.
const defaultForwardGrace = 250 * time.Millisecond

// reraiseSurviveWait is the window reRaise waits out after a self-kill before it
// reports that the re-raise did NOT end this process. Measured on this host
// (2026-10-23, 12 runs of a standalone probe): syscall.Kill QUEUES the signal and
// returns at once, with the death itself landing 130µs … 307µs later. A caller
// that treats "Kill returned" as "this process is dead" reports a survival that
// did not happen (the flaky red the testutil arm was fixed for); the window is a
// wide margin over the measured maximum and costs nothing on the dying path,
// which never reaches the far side of it.
const reraiseSurviveWait = 500 * time.Millisecond

// reraiseSurvived reports, on stderr and by name, that this process is still
// running after asking itself to die — Rule 15: a process that quietly continues
// after a re-raise is exactly the silence this repository forbids, and after Rule
// 42 it is also the DIAGNOSIS (something outside this package still subscribes to
// the signal, so no amount of releasing OUR registration reaches the default
// action). Reached only if the raise did not land.
func reraiseSurvived(sig os.Signal) {
	time.Sleep(reraiseSurviveWait)
	fmt.Fprintf(os.Stderr, "[gopi] signal provenance: re-raised %v and this process is still running %s later — "+
		"a signal subscription outside internal/signalrec is swallowing it, so the default action never ran\n",
		sig, reraiseSurviveWait)
}

// modesThatOwnTheirSignalHandlers are the gopi modes that install their own
// SIGTERM/SIGINT handling and are expected to OUTLIVE the signal long enough to
// finish in-flight work (a merger pass, a worker's task, a chat turn). The
// recorder must not re-raise for these: a re-raise applies the default action
// and would destroy exactly the in-flight work this epic exists to protect.
//
// THE UNIT IS THE MODE, NOT THE VERB. `gopi task list` is a one-shot command
// that must die on SIGTERM like any unix tool; `gopi task run <id>` is a
// long-lived agent that handles the signal itself. Both start with the verb
// "task", so keying the policy off the verb would make the one-shot
// unkillable — the exact regression the forwarding test was written to catch.
// THE RETIRED VERBS ARE NOT IN THAT TABLE, AND `merge` IS THE ONE THAT BIT.
//
// RoleMergeAlias maps argv[1]=="merge" onto RoleMergerVerb, which was right
// while `gopi merger --every` was a resident daemon (the alias let a renamed
// face keep the daemon's policy). The resident merger was RETIRED (2026-09-19,
// devdoc/merger-retired.md) and `gopi merge` was then a FOREGROUND command whose
// `--every D` arm owned its own SIGINT/SIGTERM handler (runMergeLoop, in a
// cmd/agent/merge_run.go that has SINCE been deleted — the whole bare `gopi merge`
// one-shot is gone and refuses with "use `gopi task merge`"; the record below is
// what was measured while it existed). That arm was `merge`, not `merger`: the loop
// never called ClaimSignalHandlers, so the argv table was the ONLY thing standing
// between it and the recorder's morgue.
//
// The alias left it classified RESIDENT, and resident means two things in this
// package: the recorder declines to re-raise (correct — the loop DOES unwind
// gracefully), and sessionDest's step 3 mints a PROCESS-KEYED session dir
// <gopi dir>/sessions/process-<pid> for a record the process does not need.
// Measured on master, 2026-09-28 (task 01a0e5d2):
//   - 1 of 20 killed `gopi merge --every 5s --fast` loops left
//     sessions/process-<pid>/ behind (the residue appears only when the stop
//     lands INSIDE a pass, where the loop legitimately stays alive to let the
//     pass finish — its own start line announces "the in-flight pass finishes,
//     then the loop exits 0");
//   - the condition correlates 1:1 with the loop printing its wait line:
//     iter3 wait=1 residue=1, the other 19 wait=0 residue=0;
//   - a barrier probe (chmod 555 on the data root mid-run) made the writer
//     print itself: the NOT FILED record carries pid=<loop pid>, role="merger",
//     argv="… merge --every 300ms --fast" and destination
//     sessions/process-<loop pid> — the loop's OWN recorder goroutine, not a
//     child and not a sweep;
//   - the operator's live profile carries 50 such dirs, one of them from
//     `gopi merge --every 5m` (the loop currently running on this host).
//
// So the retired merger's policy is dropped rather than inherited: `merge` is a
// foreground command whose records deserve the truth (no session dir → the
// recorder refuses and says so) instead of an empty bucket per killed loop.
//
// WHAT REPLACES THE ALIAS IS AN EXPLICIT CLAIM, not an argv match. The `--every`
// loop calls ClaimSignalHandlers() right after its own signal.Notify
// (cmd/agent/merge_run.go), which is this package's documented contract for a
// process with a graceful path of its own: the recorder writes its record and
// then leaves the signal alone. Without that the FORWARDING half of the old
// alias is genuinely lost — measured, task 01a0e5d2: the loop was killed
// mid-unwind (exit -1, no stop line) and three `merge_every_child_test.go`
// scenarios went red. The alias bought that property by accident, through the
// retired daemon's name; the claim buys it by stating the fact.
//
// The RETIRED face keeps its mapping on purpose: `gopi merger` answers
// "retired" and exits before installing any handler, so classifying it
// resident costs a refusal, never an unbounded process.
var modesThatOwnTheirSignalHandlers = map[string]bool{
	RoleMergerVerb: true, // the RETIRED daemon face: refuses, then exits
	"worker":       true,
	"chat":         true, // a streaming reply is in-flight work
	"serve":        true,
	"web":          true,
	"resume":       true,
	"session":      true, // `session chat` / `session serve`
	taskRunMode:    true, // `task run <id>` — the child agent loop handles it
	taskMergerMode: true, // `task merger` — the resident sweep daemon
}

// The two signal-owning `task` sub-modes (Role 1b: one spelling each).
const (
	taskRunMode    = "task run"
	taskMergerMode = "task merger"
)

// forwardsDefault reports whether a process whose role label is role must
// re-raise a caught signal with its default disposition. The label comes from
// roleFromArgv (which argv mode this process is), so the policy is decided by
// WHAT the process is, never by how it was signalled. An unknown mode forwards:
// a process we cannot classify must be no harder to kill than an ordinary unix
// command.
func forwardsDefault(role string) bool {
	return !modesThatOwnTheirSignalHandlers[role]
}

// handlersClaimed is this process's own declaration that it HAS installed a
// terminating-signal handler of its own (ClaimSignalHandlers). It is a fact
// about the LIVE process, so it cannot come from argv.
var handlersClaimed atomic.Bool

// ClaimSignalHandlers declares that THIS process has installed its own
// SIGTERM/SIGINT handler and will unwind gracefully: the recorder must still
// file its provenance record, and must then leave the signal ALONE.
//
// It exists because the table above cannot classify every handler-owning
// process. A bare-prompt run HAS no mode word -- argv[1] IS the prompt text, and
// roleFromArgv returns that text -- so it reads as unclassifiable, and the old
// policy ("unclassified => forward") re-raised SIGTERM one grace later while the
// graceful unwind was still in flight. The re-raise calls signal.Reset, which
// clears the disposition GLOBALLY: it disarmed the bare-prompt handler's own
// registration, so the process died at exit 143/-1 with session_end never
// written (epic 0x01a0d65f).
//
// CALL IT IMMEDIATELY AFTER signal.Notify -- the window between the two is the
// only one where the recorder can still win. A claim is one-way on purpose: the
// caller's handler stays installed for the rest of the process.
func ClaimSignalHandlers() { handlersClaimed.Store(true) }

// processLayoutEligible reports whether a process whose role label is role may
// have its record placed in the PROCESS-KEYED layout (<sessions root>/process-<pid>)
// — sessionDest's step 3, and the fall-through a ledger id whose container is
// missing reaches.
//
// It is NOT the same predicate as !forwardsDefault, and the difference is the
// whole of this function. !forwardsDefault asks "does this process own its
// signal handlers" — a question about the FORWARDING policy (may the recorder
// re-raise with the default disposition). This asks "is this process one whose
// record has no session to belong to OTHERWISE" — a question about ATTRIBUTION.
// The two answers coincide for the table's resident entries (a daemon owns its
// handlers AND executes no task), but they must NOT coincide for a process that
// owns its handlers only because it declares the claim (ClaimSignalHandlers)
// while running as a ONE-SHOT with no session of its own.
//
// THE DEFECT this exists for (measured 2026-10-12, task 01a12f76-c01f): a
// handler-owning one-shot such as the since-removed `gopi memory sync --every 5s
// --push` (5677cc6e) — a child of an agent session,
// so it INHERITS GOPI_TASK_RUN_ID — launched under a
// NON-default profile (GOPI_HOME=<sandbox>). Step 2 keys the destination by the
// inherited id, the container cannot exist in that profile, and the process is
// not in the resident table, so the recorder REFUSED. The refusal is correct
// (that id is not this process's session to file under) but the OUTCOME was
// not: the record was filable one branch away, and the loss was silent — exit
// code 0 and the verb's own stop line unchanged, so a caller reading the exit
// status saw nothing.
//
// WHY THE CLAIM IS EVIDENCE HERE, and why this is the ownership decision the
// epic asked for (EXECUTOR IDENTITY vs RECORD OWNERSHIP). GOPI_TASK_RUN_ID is
// carried as IDENTITY — it is recorded in the record's task_run field, which is
// what says "this process descends from that run" — and it stops being trusted
// as OWNERSHIP the moment its container is unusable in THIS profile. What
// replaces it is not another id but a FACT about this process: the claim is
// in-process (never argv), it says "I installed a handler and will unwind
// GRACEFULLY", and a graceful unwind that ends in os.Exit is exactly the shape
// whose record is the LAST thing it writes (and the door-side wait
// AwaitRecordFiled is what makes it so). The "litter" objection that retired the
// `merge` argv alias does not apply: that decision (`gopi merge --every` must not
// mint a process dir when killed) is pinned by
// TestMergeEveryKilledLoopLeavesNoResidueInTheProfile, and a loop acquires this
// arm only by stating the claim AND paying the door wait.
func processLayoutEligible(role string) bool {
	if !forwardsDefault(role) {
		return true
	}
	return handlersClaimed.Load()
}

// logTailMax bounds the session-log tail captured with each record: enough to
// say what the run was doing, never an archive of the run (Rule 1).
const logTailMax = 400

// record is one provenance line. Every field is omitempty-able except ts/kind,
// so the shape can grow without a migration.
type record struct {
	TS     string `json:"ts"`
	Kind   string `json:"kind"`
	Signal string `json:"signal,omitempty"`
	PID    int    `json:"pid,omitempty"`
	PPID   int    `json:"ppid,omitempty"`
	PGID   int    `json:"pgid,omitempty"`
	SID    int    `json:"sid,omitempty"`
	Cgroup string `json:"cgroup,omitempty"`
	Role   string `json:"role,omitempty"`
	Argv   string `json:"argv,omitempty"`
	// AgentAnchor is the gopi agent-session anchor this process INHERITED
	// (GOPI_AGENT_SESSION). It is a hint about which agent tree the process
	// descends from, never proof of who killed it: an inherited value says
	// "spawned under that session", not "that session signalled me".
	AgentAnchor string `json:"agent_anchor,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
	Session     string `json:"session,omitempty"`
	TaskRun     string `json:"task_run,omitempty"`
	// SessionDir is the session dir this record was filed in, written INTO the
	// record as well as being its location: a record copied out of the tree
	// (a pasted grep line, a ticket) still says which session it belongs to.
	SessionDir string `json:"session_dir,omitempty"`
	Layout     string `json:"layout,omitempty"`
	ChildSes   string `json:"child_session,omitempty"`
	// Parent facts: the supervisor is the first suspect for a broadcast kill,
	// and its argv is still readable while it is alive.
	ParentPID  int    `json:"parent_pid,omitempty"`
	ParentName string `json:"parent_name,omitempty"`
	ParentArgv string `json:"parent_argv,omitempty"`
	// Target facts (KindIntent only).
	TargetPID   int    `json:"target_pid,omitempty"`
	TargetGroup int    `json:"target_group,omitempty"`
	Reason      string `json:"reason,omitempty"`
	// LogTail is the last non-empty line(s) of the session log, trimmed.
	LogTail string `json:"log_tail,omitempty"`
	Note    string `json:"note,omitempty"`
	// Sourced is the CONSUMER-side verdict of a KindSignal line: did a
	// known gopi stop path announce this kill (a KindIntent line naming this
	// pid as its target) before the signal arrived? It is a POINTER so the
	// three states are distinguishable on the wire and a reader never has to
	// guess which one an absent field means:
	//
	//	nil   → not classified (an Intent line, a legacy line, or a recorder
	//	        whose classification could not be attempted)
	//	true  → SOURCED: a matching intent record precedes this one
	//	false → UNSOURCED: no gopi stop path announced it (external kill)
	//
	// Field name and value come from the constants below (Rule 1b/14: the
	// operator's grep and the tests reference SourcedField, never a literal).
	Sourced *bool `json:"sourced,omitempty"`
	// IntentEvidence quotes the intent record a sourced verdict rests on
	// ("<ts> <role> → <reason>"), so the claim is auditable from the same line
	// and Rule 15's "recorded, never asserted" holds for a provenance verdict.
	IntentEvidence string `json:"intent_evidence,omitempty"`
	// SICode / SICodeName / SenderPID / SenderComm are the KERNEL'S OWN WORD
	// about who sent this signal (epic signal-provenance-si-code), captured from
	// the siginfo_t the kernel built at delivery by internal/sigsource.
	//
	// SICode and SenderPID are POINTERS for the same reason Sourced is: si_code's
	// most important value is SI_USER == 0, so a plain int32 whose zero value
	// means "not captured" would silently report every ordinary kill() as an
	// absence — and a reader that inverted that would then accuse the wrong
	// sender (Rule 15a). A nil field says "no source captured on this host /
	// for this signal", which is the old record shape, byte for byte.
	//
	// The two halves answer DIFFERENT questions and must not be conflated:
	// SICode separates "a process sent this" (<= 0, the SI_FROMUSER class) from
	// "the kernel raised it" (SI_KERNEL = 128 and the fault codes), while
	// Sourced separates "a gopi stop path ANNOUNCED it" from "nobody did".
	SICode     *int32 `json:"si_code,omitempty"`
	SICodeName string `json:"si_code_name,omitempty"`
	SenderPID  *int32 `json:"sender_pid,omitempty"`
	// SICapture is the STATE of the kernel-word capture for THIS delivery, and
	// SICaptureReason says why when that state is an absence. They exist because
	// si_code's own absence was SILENT (measured 2026-10-27: 290 of 297 records
	// on this host carried no si_code at all, and a reader could not tell that
	// from "the kernel named no sender" or "the arm was refused").
	//
	// Rule 15a applies here in the OPPOSITE direction from si_code: this field is
	// a REQUIRED semantic value, and its ordinary value ("captured") is NOT the
	// zero value — so `omitempty` would delete the key on any record written by a
	// path that forgot to stamp it, which is exactly the silence this field
	// removes. The tag is therefore NOT omitempty: an empty string is a
	// recordable defect rather than an invisible one, and the closed vocabulary
	// lives in internal/sigsource (CaptureCaptured / CaptureNotCaptured /
	// CaptureArmFailed / CaptureUnsupported / CaptureNoDelivery).
	SICapture string `json:"si_capture"`
	// SICaptureReason explains a NON-captured state ("why is the kernel's word
	// missing"). It is empty for CaptureCaptured, so omitempty here means "no
	// reason to give = good news", not silence.
	SICaptureReason string `json:"si_capture_reason,omitempty"`
	// SenderComm is /proc/<sender_pid>/comm read AT SIGNAL TIME. It is
	// best-effort by nature — a child that ran `kill` and exited is already gone
	// when its victim is scheduled — so an empty value means "the sender was not
	// readable when we looked", never "the sender had no name".
	SenderComm string `json:"sender_comm,omitempty"`
}

// SourcedField is the JSON key of record.Sourced — the machine-readable field
// a consumer (an operator's jq filter, a session-log reader, a test) keys on to
// tell an UNSOURCED kill from a graceful operator drain. It is spelled once
// here because Rule 14 applies to a WIRE FIELD exactly as it applies to a value:
// a reader that re-types "sourced" drifts the moment the tag changes.
const SourcedField = "sourced"

// The two verdicts Sourced can carry, as operator-facing words. They are the
// vocabulary of the session log line too (sessionLogVerdict*), so the file and
// the console cannot disagree about what was recorded.
const (
	// VerdictSourced is the value of a kill a known gopi stop path announced.
	VerdictSourced = "sourced"
	// VerdictUnsourced is the value of a kill NO gopi stop path announced: the
	// signal arrived from outside every gopi stop surface (an operator's
	// kill(1), a supervisor's timeout(1), a service manager's stop, a session
	// teardown) — the shape the 2026-09-12/16 mass kills had, and the shape
	// that used to be byte-identical to a graceful drain in the log.
	VerdictUnsourced = "unsourced"
	// EventPrefix is the ledger-event prefix of the kill verdict
	// ("signal-sourced" / "signal-unsourced" in task_events). It lives here, in
	// the package that owns the verdict vocabulary, so the writer (internal/tools)
	// and the reader (a test, an operator surface) share one definition instead
	// of mirroring a spelling (Rule 14).
	EventPrefix = "signal-"
)

// IntentWaitEnv is the file-read budget the classification gets before it
// gives up and records "not classified". It exists because the two writers are
// DIFFERENT processes: the killer appends its intent line and then signals, so
// the victim's handler can win the race against the killer's own fsync. The
// wait is bounded and tiny (the intent is written microseconds before the
// signal), and a budget miss degrades to nil (unknown), never to a false
// "unsourced" — the verdict that accuses an operator needs the evidence to be
// READ, not assumed.
//
// Rule 21: the reader is intentWait.
const IntentWaitEnv = "GOPI_SIGNALREC_INTENT_WAIT_MS"

// SessionLogVerdictEnv is RETIRED — the knob is GONE, and deliberately NOT
// declared as a constant any more (Rule 21: an env-name constant with no
// environment reader is a claim the product does not keep).
//
// It used to make the recorder append a one-line verdict to the session log
// "for a caller that wants it", with a mode-derived default — and nothing ever
// set it: the sole production caller passed a literal "" for the log path, so
// appendSessionLogVerdict returned immediately and the documented line was
// written by NO process (the Rule 15 docs-lie this epic closes).
//
// The question it answered — "does THIS process write the verdict?" — is not a
// policy at all: the ownership predicate it deferred to (signalForwarding, the
// re-raise half) has moved ONTO the wire, where it belongs. Every captured
// signal now writes the verdict event, and the predicate travels IN the event
// as SourcedField (true = announced, false = unannounced), so a reader gets
// both the kill and the verdict instead of the kill alone. A knob on top of
// that could only ever DELETE evidence.
//
// Inert by construction: exporting a constant for the name would change
// nothing, and setting it changes nothing either.

// Record is the exported view of one provenance line — a TYPE ALIAS, not a copy,
// so a reader outside this package (an end-to-end test asserting on a real
// victim's line, an operator surface) parses the exact struct the writer
// marshals and cannot drift from its JSON tags (Rule 14). It carries no
// behaviour: the alias exists so the wire contract has one definition.
type Record = record

// destination is the resolved sink of one record: the session dir it belongs
// to, the file inside it, and WHICH id form keyed the dir (so a reader can tell
// a task-keyed dir from a process-keyed one without re-deriving the role).
type destination struct {
	Dir    string
	Path   string
	Layout string
	TaskID string
	// Note, when non-empty, says the destination is NOT the process's own
	// session log dir but a justified stand-in (the resident-role case). It is
	// recorded in the record itself, so the placement is auditable rather than
	// inferred.
	Note string
}

// sessionDirOverride / taskIDOverride / sessionLogOverride are the in-process
// handoff of the destination and of the run's OWN session log (Install's own
// process learned them after the recorder was installed — see SetSessionDir).
// Guarded by overrideMu: a signal can arrive on any goroutine while another
// sets them.
var (
	overrideMu              sync.Mutex
	installedMu             sync.Mutex
	sessionDirOverride      string
	taskIDOverride          string
	sessionLogOverride      string
	installedSessionLogPath string
)

// setInstalledSessionLog records Install's own argument (the caller that knew
// the path up front). Written once per Install; a re-install replaces it, which
// is the correct reading of "the latest caller's answer".
func setInstalledSessionLog(path string) {
	installedMu.Lock()
	defer installedMu.Unlock()
	installedSessionLogPath = strings.TrimSpace(path)
}

// SetSessionDir hands this process's provenance destination to the recorder.
//
// It exists because the recorder is installed in main() BEFORE any session
// exists (that placement is what covers every verb), while the owning session
// dir is only known once the run has minted its logger — so the fact arrives
// later, from the one place it becomes true (cmd/agent's printLogPaths, next to
// the TaskExecutorSessionEnv export). One writer, one reader (sessionDest).
//
// sessionLog is the run's OWN session JSONL (agent.SessionLogger.Path()) and
// this call is the ONE place it becomes true: the same caller, the same
// statement, as the session DIR the provenance record files into. The two
// resolutions are handed over together on purpose — a witness that landed beside
// the session it describes would be a second, drifting answer to "which log is
// mine" (Rule 14). Install's own sessionLog argument is the other half (a caller
// that knows the path before the logger exists); resolveSessionLog is where the
// two meet, and neither is re-derived from the other.
//
// An empty dir CLEARS the overrides (a following run in the same process must
// not inherit the previous run's dir, nor append its verdict to the previous
// run's log — the recorder's own tests rely on it).
func SetSessionDir(dir, taskID, sessionLog string) {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	sessionDirOverride = strings.TrimSpace(dir)
	taskIDOverride = strings.TrimSpace(taskID)
	sessionLogOverride = strings.TrimSpace(sessionLog)
}

// resolveSessionLog returns the session JSONL this process appends its verdict
// event to, or "". The handoff made later (sessionLogOverride — the run's own
// logger, set by SetSessionDir) WINS when it names one, and Install's argument
// (installSessionLog — the caller that knew the path up front) is the fallback;
// both empty means this process owns no session log and nothing is appended.
//
// Rule 2's discipline applied to a fact rather than to a deadline: ONE reader of
// each source (installedSessionLog / sessionLogOverride), and the precedence
// between them stated ONCE, here, instead of being re-derived at each call site.
func resolveSessionLog() string {
	if log := sessionLogOverride; log != "" {
		return log
	}
	return installedSessionLog()
}

// installedSessionLog is the half of the handoff Install's caller provided.
func installedSessionLog() string {
	installedMu.Lock()
	defer installedMu.Unlock()
	return installedSessionLogPath
}

// SessionDir returns the session directory THIS process recorded for itself
// ("" when it recorded none). It is the in-process authority on "which session
// directory is mine" — the fact the report-ownership guard
// (internal/taskreport.ProcessOwnsContract) decides on, so a tool that must
// judge an inherited report contract does not have to re-derive the directory
// from a task id (and cannot disagree with the recorder about it).
func SessionDir() string { return recordedSessionDir() }

// recordedSessionDir returns the in-process override ("" when unset) — the
// reader half of SetSessionDir, exported for `gopi task show`-style surfaces and
// for tests that must know where a record was sent.
func recordedSessionDir() string {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	return sessionDirOverride
}

// isSessionDir reports whether dir is PROVABLY a gopi session dir: it must exist
// as a directory and carry the session container's own marker (metadata.json,
// written by both the submit skeleton and the run-time logger) or a JSONL
// artifact. Requiring proof is the difference between attributing a record and
// guessing at a directory name: a record filed under an id that turns out to be
// a typo is worse than a refused record, because it reads as evidence.
func isSessionDir(dir string) bool {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return false
	}
	if fi, err := os.Stat(filepath.Join(dir, sessionMetadataName)); err == nil && fi.Mode().IsRegular() {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), sessionJSONLSuffix) {
			return true
		}
	}
	return false
}

// sessionMetadataName is the session container's identity file (the submit
// skeleton and the run-time logger both write it).
const sessionMetadataName = "metadata.json"

// sessionDest resolves where THIS process's records must go, or explains why the
// record cannot be written at all. It NEVER falls back to a public/shared path:
// a process that cannot name a session dir is a REFUSAL the caller reports (the
// victim path prints it; Intent returns it), because an unattributable record
// silently written to a host-global file is the defect this package just fixed.
//
// Resolution order, most specific first:
//
//  1. the in-process handoff (SetSessionDir — this run's own session dir, the
//     normal front-run/chat case), else the exported SessionDirEnv (the same
//     fact handed to a CHILD by its launcher, which knows both ids);
//  2. the ledger id this process carries (GOPI_TASK_RUN_ID / GOPI_TASK_WRAPPER_ID)
//     → <sessions root>/<id>; when that dir cannot hold the record, a RESIDENT
//     mode falls through to 3 (with a note naming the unusable id) and a
//     one-shot mode is refused — the id IS its session;
//  3. the process layout (<sessions root>/process-<pid>) for a role that
//     executes no task and owns no session log (the merger daemon) — created
//     here, and recorded with a Note so the placement is visible;
//  4. otherwise: a refusal. No public dir, no silently-dropped record.
func sessionDest() (destination, error) {
	root, err := config.SessionsRoot()
	if err != nil || root == "" {
		return destination{}, fmt.Errorf("signalrec: no gopi dir to record into (rule: no public-dir fallback)")
	}
	// The retired public sink is named in the refusals below for a reader who
	// greps the old path; it hangs off the PROFILE, not off the sessions root
	// (which a run may have relocated), so it is resolved from its own owner. An
	// unresolvable profile leaves the hint relative rather than inventing a home
	// — the sentence is about a path nobody writes any more.
	gd, gdErr := config.GopiDir()
	if gdErr != nil {
		gd = ""
	}

	// 1. Handed the dir (in-process override, then the env a launcher exported).
	handoff := recordedSessionDir()
	fromEnv := false
	if handoff == "" {
		handoff = strings.TrimSpace(os.Getenv(SessionDirEnv))
		fromEnv = handoff != ""
	}
	d, ok, herr := handedDest(fromEnv, handoff)
	if herr != nil {
		return destination{}, herr
	}
	if ok {
		return d, nil
	}

	// 1b. The AGENT-CHILD handoff (marker + dir): a process another gopi spawned
	// as an AGENT SESSION standing in a TASK's session dir — the merger's verify
	// child. It cannot key that dir itself: it mints its own session uuid at
	// startup (so it carries no ledger id, and the gate STRIPS the daemon
	// contract on purpose), and its argv mode owns its signal handlers, so the
	// resident process-keyed layout of step 3 is unavailable to it by
	// construction. The launcher knows the dir, so it hands it over WITH the
	// marker — and the marker is what makes this a handoff rather than the
	// over-attribution an inherited pair would be (see handedDest).

	// 2. Keyed by the ledger id this process carries (task-run child, verify
	// child, a worker's round).
	label := processLabel()
	resident := !forwardsDefault(label)
	if id := ledgerID(); id != "" {
		// THE SHAPE GATE (bug row 01a12f3c-d888-720e-b7b5-f458f55e98aa,
		// 2026-10-12): the id is about to become a PATH ELEMENT, and whether it
		// may is ONE definition (internal/sessionid — the same rule the two
		// session-directory writers apply, Rule 13/14), never a belief about
		// what the caller meant. The near-miss it refuses is measured, not
		// hypothetical: the branch/worktree spelling (a uuid with every
		// non-alphanumeric character dropped, taskGroundIDStem) was joined
		// SILENTLY into a SECOND container beside the row's own.
		//
		// Why a REFUSAL and not the resident fallback below: that fallback
		// exists for an id whose container is missing (a real row in a profile
		// where no dir can exist), and it FILINGS the record under the process
		// key. An id this tree does not spell is a different defect — it names
		// no row's container at all — and filing there would attribute the
		// record to a directory the run it explains never wrote. The refusal is
		// about the RECORD only and never blocks the caller's own work.
		if !sessionid.IsAdmissible(id) {
			return destination{}, fmt.Errorf("signalrec: %w (ledger id %q, mode %s) — refusing to file the record under a directory this tree did not mint; no public-dir fallback", sessionid.Refuse(id), id, label)
		}
		dir := filepath.Join(root, id)
		if isSessionDir(dir) {
			return destination{Dir: dir, Path: filepath.Join(dir, FileName), Layout: LayoutTaskRun, TaskID: id}, nil
		}
		// The id is carried but its dir cannot hold the record. A RESIDENT mode
		// (step 3) is the one case where that is not a dead end: the process
		// layout is the layout that exists for it, so the record is FILED there
		// and the note says why the id was not used. This is the operator-stop
		// defect (2026-09-14): a resident process whose argv mode owns its
		// signal handlers (the merger daemon) is often launched from a shell
		// that INHERITED a ledger id — an id that names a task in a profile
		// where no session dir can exist — and the strict branch turned a
		// filable record into a refusal that read like the stop had failed.
		//
		// A NON-resident mode keeps the refusal: its ledger id IS the session
		// the record belongs to (a `task run` child, a verify child), and a
		// one-shot command has no lifecycle to mint a bucket for (step 4).
		if processLayoutEligible(label) {
			// WHICH ARM admitted the process layout is recorded, because the
			// two are different facts about the process: an argv RESIDENT mode
			// has no session to belong to by construction, while a CLAIMING
			// one-shot (a loop whose unwind ends in os.Exit) has one because it
			// said so — and the placement must stay auditable from the record.
			why := "mode " + label + " is resident"
			if forwardsDefault(label) {
				why = "mode " + label + " " + ClaimMarker + " (ClaimSignalHandlers)"
			}
			return processDest(root, label, "ledger id "+id+" has no usable session dir ("+dir+") and "+why+
				" — process-keyed session dir; the id could not be honoured")
		}
		return destination{}, fmt.Errorf("signalrec: session dir %q for task %s is missing or unusable — refusing to write (no public-dir fallback; legacy public sink %s is NOT used; this refusal is about the RECORD only and never blocks the caller's own work)", dir, id, legacySinkHint(gd))
	}

	// 3. Process layout — reserved for a resident role that owns no task and no
	// session log. A one-shot command that cannot name a session dir is NOT
	// given a dir (see the refusal below): it has no lifecycle the record
	// belongs to, and minting an empty bucket per invocation would litter the
	// sessions root.
	//
	// A RESIDENT MODE is looked up by its PID-KEYED path FIRST, even when the env
	// names it (Step 1 would otherwise reject it as an unmarked handoff): that
	// path is the layout the mode owns BY DEFINITION, so accepting it is not a
	// trust decision — it is the same path this function is about to compute
	// itself. It matters for the kill guard: the STOP PATH resolves the victim's
	// dir from OUTSIDE and mirrors its intent there (targetSessionDir), so a
	// resident victim that checked only for a marker would classify its own
	// announced stop as UNSOURCED.
	if dir, ok := residentDirHandoff(root, label); ok {
		return destination{Dir: dir, Path: filepath.Join(dir, FileName), Layout: LayoutProcess}, nil
	}
	if resident {
		return processDest(root, label, "")
	}

	// 4. Refusal. NOT a fallback: the record must not be filed anywhere the
	// operator cannot walk back to a session.
	return destination{}, fmt.Errorf(
		"signalrec: cannot attribute this record: no session dir handed over (%s / %s), no ledger id (%s), and role %q is not a resident mode; legacy public sink %s is NOT used (no public-dir fallback)",
		SessionDirEnv, "SetSessionDir", taskRunEnvName, label, legacySinkHint(gd))
}

// processDest builds the process-keyed destination of a resident role (the
// merger daemon: it executes no task and owns no session log). The dir is
// created here, and note — when non-empty — is recorded IN the record, so the
// placement is auditable rather than inferred.
//
// The proof required here is USABILITY, not session-ness: this dir is created
// by this package (a resident role owns no session log, so there is no marker to
// demand) — but it must at least be a real, reachable directory, or the write
// would fail silently.
func processDest(root, label, note string) (destination, error) {
	dir := residentDirFor(os.Getpid(), root)
	if err := os.MkdirAll(dir, sessionDirPerm); err != nil {
		return destination{}, fmt.Errorf("signalrec: create process session dir %q: %w", dir, err)
	}
	// The two failure shapes are split (and the guard de-indented) because a
	// single combined condition cannot carry `%w`: the !IsDir() arm reaches the
	// format call with serr == nil, and `%w` on a nil error renders
	// `%!w(<nil>)` while the message claims a stat error. Split, the stat error
	// stays IN the chain and nothing is ever unwrapped from nil.
	fi, serr := os.Stat(dir)
	if serr != nil {
		return destination{}, fmt.Errorf("signalrec: process session dir %q is unusable after creation: %w", dir, serr)
	}
	if !fi.IsDir() {
		return destination{}, fmt.Errorf("signalrec: process session dir %q is unusable after creation: not a directory", dir)
	}
	if note == "" {
		note = "resident role with no task and no session log (mode " + label + ") — process-keyed session dir"
	}
	return destination{Dir: dir, Path: filepath.Join(dir, FileName), Layout: LayoutProcess, Note: note}, nil
}

// handedDest validates a handed-over session dir (the in-process override or the
// exported SessionDirEnv) and builds the destination. ok=false with a nil error
// means nothing was handed over (keep resolving); a non-nil error means the
// handoff was PROVEN wrong — and a handed dir that is not a session dir is an
// error the caller must see (Rule 15), never silently ignored in favour of
// another resolution, because the caller believed it knew better.
func handedDest(fromEnv bool, dir string) (destination, bool, error) {
	if dir == "" {
		return destination{}, false, nil
	}
	// The AGENT-CHILD handoff is classified by the DIR it names (Rule 2: the
	// record's layout must describe where it actually landed), while the ledger
	// row it carries is whatever the launcher DECLARED (TaskIDEnv) — a session
	// whose dir is keyed by a run uuid still belongs to the row being executed.
	//
	// The marker is a CONSISTENCY ASSERTION from the launcher, not a licence to
	// skip the check: it claims "the declared ledger row IS the key of the dir I
	// handed you", and a marked handoff whose two facts disagree is REFUSED
	// loudly (Rule 15) — the launcher may not silently relabel a record into a
	// directory another row owns.
	//
	// WHAT PROVES THE DIR, and why it differs between the two handoffs. An env
	// handoff with no marker is a caller naming an EXISTING session of its own,
	// so the dir must LOOK like one (isSessionDir: the container's marker or a
	// run JSONL). A MARKED agent-child handoff is the opposite situation: the
	// launcher resolved the judged row's container and created it, and it is
	// legitimately EMPTY at spawn time — the child has not written its session
	// log yet, and the kill at the wait cap may arrive before it ever does. The
	// proof is therefore IDENTITY, not contents: the dir exists and its name IS
	// the ledger row the launcher declared. Requiring the contents would refuse
	// the handoff at exactly the moment this epic exists for (the cap kill),
	// which a first cut of this fix did — measured, not theorized.
	agentChild := fromEnv && envFlag(EnvAgentChild)
	keyed := dirIsKeyedBy(dir, declaredTaskID())
	if agentChild {
		if !keyed {
			return destination{}, false, fmt.Errorf("signalrec: %s claims a spawned agent child, but the handed dir %q is not the session container of the declared row %q — refusing to guess which of the two facts is wrong (no public-dir fallback)", EnvAgentChild, dir, declaredTaskID())
		}
		if err := dirExists(dir); err != nil {
			return destination{}, false, fmt.Errorf("signalrec: %s named %q, which is not a usable session container: %w", EnvAgentChild, dir, err)
		}
		return destination{Dir: dir, Path: filepath.Join(dir, FileName), Layout: LayoutTaskRun, TaskID: declaredTaskID()}, true, nil
	}
	if !isSessionDir(dir) {
		src := "SetSessionDir"
		if fromEnv {
			src = SessionDirEnv
		}
		return destination{}, false, fmt.Errorf("signalrec: %s named %q, which is not a session dir — refusing to write (no public-dir fallback)", src, dir)
	}
	layout := LayoutSession
	if fromEnv && keyed {
		layout = LayoutTaskRun
	}
	return destination{Dir: dir, Path: filepath.Join(dir, FileName), Layout: layout, TaskID: declaredTaskID()}, true, nil
}

// dirExists reports whether path is an existing DIRECTORY (a file named like the
// row, or a dangling path, is not a container to file evidence in).
func dirExists(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

// declaredTaskID is the LEDGER row the launcher declared for this process:
// TaskIDEnv (the companion fact of a handed dir — the row a run-uuid-keyed
// session executes) when set, else the ledger id this process carries itself.
// Reading the pair in that order is what keeps a record's task_run honest: the
// declared row outranks an inherited id, and an inherited id is still recorded
// rather than dropped.
func declaredTaskID() string {
	if id := strings.TrimSpace(os.Getenv(TaskIDEnv)); id != "" {
		return id
	}
	return ledgerID()
}

// dirIsKeyedBy reports whether a handed dir is the sessions-root child named by
// id — the task-keyed shape. It compares FILE NAMES, never paths, so the
// sessions root stays spelled in exactly one place in this package
// (sessionDest). A dir with no id to compare against (an untitled handoff) is
// never task-keyed: an unnamed row must not be inferred from a directory name,
// because a record filed under a guessed id reads as evidence.
func dirIsKeyedBy(dir, id string) bool {
	return id != "" && filepath.Base(dir) == id
}

// envFlag reports whether a boolean env marker is armed (the canonical truthy
// spelling set, read from the ONE vocabulary definition in internal/boolvocab —
// a leaf, so this package imports it instead of re-spelling a copy; Rule 13/14.
// One spelling therefore governs every knob in this package).
func envFlag(name string) bool {
	return boolvocab.IsTrue(os.Getenv(name))
}

// reportUnfiled is the anti-silent-failure half of the no-fallback contract: a
// record that could NOT be filed is LOUD (stderr — where the launcher, the
// worker's round log and the session console log all collect it) and the record
// itself is printed as the same one-JSON-per-line shape it would have had, so the
// evidence survives in a captured transcript even when no session dir can hold
// it. Reporting happens ONCE per process (a fleet of signals must not bury the
// operator), and it never fails the shutdown it exists to explain: this package's
// best-effort contract is about not BLOCKING the stop, never about hiding it.
func reportUnfiled(err error, rec record) {
	// THE THIRD STATE IS PUBLISHED HERE, ON THE ONE PATH THAT KNOWS THE ANSWER.
	// Before this call the door could only see RecordFiledNow()==false, which is
	// indistinguishable from "the recorder has not finished yet" — so it burned
	// DefaultRecordFiledWait and then printed recordLostLine ("recorded
	// NOWHERE") for a rejection this line had already explained. The reason
	// travels WITH the state (noteRecordRefused) so the two surfaces cannot
	// disagree about why.
	noteRecordRefused(err.Error())
	refuseOnce.Do(func() {
		fmt.Fprintf(os.Stderr, "[gopi] signal provenance %s: %v\n", RefusalMarker, err)
		if line, merr := json.Marshal(rec); merr == nil {
			fmt.Fprintf(os.Stderr, "%s\n", line)
		}
	})
}

// refuseOnce guards reportUnfiled (see it).
var refuseOnce sync.Once

// SignalSIGTERM is the name classifySignal renders for SIGTERM: the expected
// signal of every bounded-wait kill in the tree, spelled once so a consumer
// (an end-to-end kill assertion, an operator filter) references it instead of
// re-typing the string (Rule 14).
const SignalSIGTERM = "SIGTERM"

// RefusalMarker is the stable fragment of the refusal line — the wording an
// operator greps during an incident and an end-to-end test asserts on. It has
// ONE producer (reportUnfiled) and is spelled here so a consumer references the
// phrase instead of re-typing it (Rule 14: a shared value has one home; a
// re-typed phrase drifts the moment the message is reworded, and the assertion
// then fails for a wording change rather than for a behaviour change).
const RefusalMarker = "NOT FILED (no public-dir fallback)"

// ClaimMarker is the stable fragment of the ATTRIBUTION sentence a record
// carries when the process-keyed layout was admitted by the process's OWN claim
// (ClaimSignalHandlers) rather than by its argv mode being resident. It has ONE
// producer (the note built in sessionDest) and is spelled here so a consumer (a
// regression guard asserting WHICH attribution was used) references the phrase
// instead of re-typing it — the same Rule 14 reason as RefusalMarker above.
const ClaimMarker = "claimed its own signal handlers"

// ledgerID returns the ledger task id this process carries ("" when none). Both
// names are read because they describe different positions in the same tree: the
// wrapper sets GOPI_TASK_RUN_ID on the child it executes, GOPI_TASK_WRAPPER_ID
// identifies a wrapper to itself.
func ledgerID() string {
	for _, name := range []string{taskRunEnvName, wrapperEnvName} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// legacySinkHint names the retired public sink for a refusal message, so an
// operator who greps the old path is told where to look instead (and that the
// old path is no longer written). It is the ONE place the legacy path fragment is
// joined — and it joins a path only to REPORT it, never to write it.
func legacySinkHint(gopiDir string) string {
	return filepath.Join(gopiDir, LogDirName, LogFileName)
}

// processLabel is the role label of THIS process, used to decide the process
// layout (and reported in the record). It is roleFromArgv's label, with the
// unknown case answered by the anchor marker so a nested process is not
// mislabelled.
func processLabel() string {
	label := roleFromArgv(os.Args)
	if label == RoleUnknown {
		if anchor := strings.TrimSpace(os.Getenv(config.AgentSessionAncestorEnv)); anchor != "" {
			return anchor
		}
	}
	if label == "" {
		return RoleUnknown
	}
	return label
}

// Path returns the provenance file of THIS process's session dir, or "" when no
// session dir can be resolved (the caller then treats the record as refused —
// see sessionDest). It never returns a public/shared path.
func Path() string {
	d, err := sessionDest()
	if err != nil {
		return ""
	}
	return d.Path
}

// write appends one JSON line to this process's session-dir sink AND publishes
// the door's outcome (see the PUBLICATION STEP below).
//
// The victim path treats every failure as best-effort (this runs on a shutdown
// path), but a failure is RETURNED so the one caller that can act on it does:
// the signal handler prints it, Intent returns it. Silence about a refused
// record would be the same defect as the fallback it replaces.
//
// The append itself is appendRecord; the publication step belongs HERE because
// it answers a door's question about the process's DEATH record (recordFiled),
// which is why the re-affirm arm (noteRepeatedSignal) appends through
// appendRecord and never through this function.
func write(rec record) error {
	if err := appendRecord(rec); err != nil {
		return err
	}
	// PUBLICATION STEP: the bytes are in the file (an O_APPEND write is atomic
	// and visible to every other process through the page cache the instant it
	// returns — the kernel keeps that cache after this process dies, which is
	// exactly the level of durability the record needs). A CLAIMING DOOR's
	// unwind waits on this fact before it exits, because the door's own handler
	// and this one are different goroutines racing the same signal
	// (AwaitRecordFiled). It is published HERE — after the successful write and
	// nowhere else — so the waiter can never settle on a record that was
	// refused or failed.
	noteRecordFiled()
	return nil
}

// appendRecord resolves the destination and appends ONE record line to it. It
// publishes no outcome: a caller that needs the door's filed/refused answer
// states that itself, so a re-affirm can never move the answer a claiming door
// is waiting on.
func appendRecord(rec record) error {
	d, err := sessionDest()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d.Dir, sessionDirPerm); err != nil {
		return fmt.Errorf("signalrec: create session dir %q: %w", d.Dir, err)
	}
	rec.SessionDir = d.Dir
	rec.Layout = d.Layout
	if d.TaskID != "" {
		rec.TaskRun = d.TaskID
	}
	if d.Note != "" && rec.Note == "" {
		rec.Note = d.Note
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("signalrec: marshal record: %w", err)
	}
	f, err := os.OpenFile(d.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("signalrec: open %s: %w", d.Path, err)
	}
	defer func() { _ = f.Close() }() //stylegate:ignore best-effort close of an append-only evidence sink; the write below is what matters
	// ONE write per record on a single O_APPEND fd: the kernel's append is
	// atomic, so concurrent writers (a fleet of processes cannot share a session
	// dir, but a process's own goroutines and a re-entrant signal can) never
	// interleave into a half line.
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("signalrec: append record to %s: %w", d.Path, err)
	}
	return nil
}

// roleFromArgv derives WHAT this process is from its own command line — the
// only self-identifying fact a gopi process reliably has (a launcher-started
// worker exports no role marker at all). argv is already recorded verbatim, so
// this label is a convenience for the operator's grep, never a substitute for
// the argv it is derived from.
func roleFromArgv(args []string) string {
	if len(args) < 2 {
		return RoleUnknown
	}
	verb := args[1]
	if verb == verbTask && len(args) > 2 && !strings.HasPrefix(args[2], "-") {
		return verb + " " + args[2] // e.g. "task run", "task merger"
	}
	if verb == RoleMergerVerb {
		return RoleMergerVerb
	}
	return verb
}

// role reads the gopi-exported identity/mode markers this process carries.
// Empty values are simply absent facts.
func role() (label, session, taskRun, childSession string) {
	session = strings.TrimSpace(os.Getenv(config.AgentSessionEnv))
	anchor := strings.TrimSpace(os.Getenv(config.AgentSessionAncestorEnv))
	taskRun = strings.TrimSpace(os.Getenv(taskRunEnvName))
	childSession = strings.TrimSpace(os.Getenv(childSessionEnvName))
	label = roleFromArgv(os.Args)
	if label == RoleUnknown {
		// No argv mode (an argv the bin name plus nothing): fall back to the
		// inherited anchor so the line still says something about the tree.
		label = anchor
	}
	if label == "" {
		label = RoleUnknown
	}
	anchorOfProcess = anchor
	return label, session, taskRun, childSession
}

// anchorOfProcess carries the inherited anchor out of role() for the record
// snapshot (kept as a package var rather than widening role()'s return).
var anchorOfProcess string

// The verb words this label derives from — the CLI's own vocabulary, spelled
// once (Rule 1b). The task alias is recognized so a label never reads
// "task merger". RoleMergeAlias is the FOREGROUND command's own word: it is
// deliberately NOT mapped onto RoleMergerVerb (see the note above
// modesThatOwnTheirSignalHandlers) — the two faces differ in everything but
// their first four letters.
const (
	verbTask       = "task"
	RoleMergerVerb = "merger"
	RoleMergeAlias = "merge"
)

// RoleUnknown labels a process whose argv names no mode (a bare `gopi` run).
const RoleUnknown = "unknown"

// The two identity env names the tools package owns. They are duplicated here
// ONLY as already-existing exported constants, imported through a tiny
// indirection so this leaf package stays importable by cmd/agent without a
// cycle; the values mirror internal/tools' exported names and a test pins the
// pair so a rename there cannot silently stop this record from carrying them.
const (
	taskRunEnvName      = "GOPI_TASK_RUN_ID"
	childSessionEnvName = "GOPI_TASK_CHILD_SESSION"
	wrapperEnvName      = "GOPI_TASK_WRAPPER_ID"
)

// TaskRunEnvName / TaskWrapperEnvName are the two launch-handoff identities
// declaredTaskID consults, exported so a caller can NAME them: a test that stages
// the UNANNOUNCED process shape has to clear every channel the reader consults, and
// a test spelling the literals itself would silently stop clearing one of them on
// the next rename (Rule 13/14).
const (
	TaskRunEnvName     = taskRunEnvName
	TaskWrapperEnvName = wrapperEnvName
)

// classifySignal renders a signal the way an operator reads it (`terminated`,
// `interrupt`, …) — never a bare number.
func classifySignal(sig os.Signal) string {
	if s, ok := sig.(syscall.Signal); ok {
		switch s {
		case syscall.SIGTERM:
			return SignalSIGTERM
		case syscall.SIGINT:
			return "SIGINT"
		case syscall.SIGHUP:
			return "SIGHUP"
		case syscall.SIGUSR1:
			return "SIGUSR1"
		case syscall.SIGUSR2:
			return "SIGUSR2"
		case syscall.SIGQUIT:
			return "SIGQUIT"
		}
		return fmt.Sprintf("signal-%d", int(s))
	}
	return (sig).String()
}

// SignalName renders a signal the way an operator reads it (`SIGTERM`,
// `SIGINT`, …) — never a bare number. It is the EXPORTED face of
// classifySignal (Rule 14: one spelling for the whole tree), so the process
// that CATCHES a signal and the process that SENDS one name it identically:
// the termination line and the requester's request-file header must agree, or
// a reader comparing the two would see two different events.
func SignalName(sig os.Signal) string { return classifySignal(sig) }

// proc facts, all best-effort (an empty string is a missing fact, never a
// fabricated one).
func procString(pid int, name string) string {
	b, err := os.ReadFile(filepath.Join(ProcRootName, strconv.Itoa(pid), name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " "))
}

func procCgroup(pid int) string {
	raw := procString(pid, cgroupFileName)
	if raw == "" {
		return ""
	}
	// One line per hierarchy; the unified hierarchy (0::) is the one that
	// answers "which systemd scope owns this".
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "0::") {
			return line
		}
	}
	return raw
}

// cgroupFileName is procfs's per-process cgroup file.
const cgroupFileName = "cgroup"

// logTailFromEnv locates the run's own log and returns its last non-empty
// lines, bounded. Two shapes exist: the session JSONL path (`gopi/logs/…
// session_log`) is not exported, so the recorder uses the env markers it does
// have plus a caller override (Install's sessionLog argument), which every
// caller that knows its log passes.
func logTail(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(b)
	if len(text) > logTailMax {
		text = text[len(text)-logTailMax:]
	}
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}

// Install wires this process's signal provenance and returns the uninstall
// function its caller must defer.
//
// sessionLog is the run's own log path when the caller knows it (may be ""),
// and it is the caller-side half of a LIVE handoff — not a dead argument: it is
// recorded here and read at SIGNAL time by resolveSessionLog, with the later
// SetSessionDir handoff taking precedence when it names a path (a run whose
// logger was minted after Install). Both halves empty means this process owns no
// session log, and then no verdict event is appended (there is nothing to append
// to — see appendSessionLogVerdict, which never creates the file).
// The handler records ONCE per signal kind for the DEATH (a second SIGTERM does
// not append a duplicate death record — a fleet-wide kill must not look like
// twice as many dead processes as there were); a REPEAT is still recorded, as a
// re-affirm (KindSignalRepeat + the session log's own EventTypeSignalRepeat),
// so no delivery is ever dropped in silence (Rule 15).
//
// DEATH SEMANTICS ARE PRESERVED, which is the subtle half. Registering a signal
// with signal.Notify REPLACES its default disposition — a bare `gopi task list`
// would stop dying on SIGTERM merely because a recorder wanted to write a line.
// So after recording, a process that owns no handler of its own RE-RAISES the
// signal with the default disposition (signal.Reset + kill self): it dies
// exactly as it did before, one evidence line richer. The modes that DO own
// handlers (merger, worker, chat, … see verbsThatOwnTheirSignalHandlers) are
// left to them — re-raising there would apply the default action to a process
// that was deliberately finalising in-flight work, the very damage this epic is
// about. That decision is made by signalForwarding AT SIGNAL TIME: a process
// that installs its own handler LATER -- hundreds of lines below Install -- and
// declares the claim (ClaimSignalHandlers) is left to unwind, while an
// unclassified one-shot argv keeps the default death.
//
// Returned function is safe to call twice.
func Install(sessionLog string) func() {
	if off() {
		return func() {}
	}
	// The caller's half of the session-log handoff, recorded ONCE here: Install
	// runs from main() before any run has minted a logger, so "" is the honest
	// answer for every verb whose logger is created later (the path then arrives
	// through SetSessionDir) — while a caller that DOES know the path (a run
	// re-installing the recorder with its logger already open) is not ignored
	// either. resolveSessionLog owns the precedence.
	setInstalledSessionLog(sessionLog)
	ch := make(chan os.Signal, signalBufSize)
	signal.Notify(ch, watchedSignals...)
	// THE KERNEL'S OWN WORD ABOUT THE SENDER (epic signal-provenance-si-code).
	// signal.Notify above has ALREADY replaced each signal's disposition with
	// Go's runtime trampoline (installed with SA_SIGINFO), so this installs a
	// capturing trampoline ON TOP of it and chains into it — see
	// internal/sigsource's package doc for why that is the whole mechanism and
	// why no disposition changes. A failure is REPORTED and recording goes on:
	// a provenance record without si_code is the shape every release before this
	// epic wrote, and losing it because the capture could not arm would be the
	// worse trade (Rule 15: say it, never swallow it).
	if uerr := sigsource.Arm(watchedSignals); uerr != nil {
		fmt.Fprintf(os.Stderr, "[gopi] signal provenance: siginfo capture NOT ARMED: %v\n", uerr)
	}
	done := make(chan struct{})
	go func() {
		seen := map[string]int{}
		for {
			select {
			case sig := <-ch:
				key := classifySignal(sig)
				// REPEATED DELIVERIES ARE RECORDED, NEVER SILENTLY DROPPED.
				// This branch used to be a bare `continue`: the second and every
				// later delivery of a signal kind left NO trace anywhere, so "the
				// operator pressed Ctrl-C four times" and "one signal arrived"
				// produced byte-identical evidence (Rule 15: a silent drop is a
				// failure, not a policy). The record written here is a RE-AFFIRM
				// (KindSignalRepeat): the process's disposition, its verdict and
				// its one re-raise are all decided by the FIRST delivery and are
				// untouched by this one — which is why the arms below (note
				// the received/verdict/filed publications, the session-log
				// verdict event and reRaise) are deliberately NOT reached.
				if n := seen[key]; n > 0 {
					seen[key] = n + 1
					noteRepeatedSignal(sig, n+1, sessionLog)
					continue
				}
				seen[key] = 1
				noteSignalReceived(key)
				r := snapshot(key, sessionLog)
				// THE KERNEL'S WORD ABOUT THE SENDER goes on EVERY signal record
				// (epic signal-provenance-si-code). It is stamped HERE, in the
				// goroutine that just received the delivery, because the capture
				// table holds the most recent siginfo per signal — reading it one
				// goroutine later (a consumer, a later round) could describe a
				// different delivery of the same signal.
				stampSignalSource(&r, sig)
				// THE CONSUMER SIDE (epic signal-provenance-unsourced,
				// 2026-09-17): decide whether a known gopi stop path ANNOUNCED
				// this kill before recording it. A SIGTERM nobody announced is
				// the 2026-09-12/16 mass-kill shape, and until this call it was
				// indistinguishable from a graceful operator drain — both wrote
				// a bare "signal" line and both printed "draining".
				classifySourced(&r)
				noteVerdict(r)
				werr := write(r)
				if werr != nil {
					reportUnfiled(werr, r)
				}
				// THE OUTCOME IS PUBLISHED FOR THE DOOR (AwaitRecordFiled): a
				// claiming process's unwind ends in os.Exit on ANOTHER
				// goroutine and would otherwise outrun this write — measured,
				// 3/3 lost records. Published on the ONE path where the bytes
				// are on disk (master's publication step), so the waiter can
				// never settle on a record that was refused or failed.
				if werr == nil {
					noteRecordFiled()
				}
				// THE VERDICT REACHES THE RUN'S OWN SESSION LOG. Every signal
				// that is recorded is also written into the run's log when this
				// process owns one, and the ownership predicate travels IN the
				// event (SourcedField) instead of gating the write — so an
				// unannounced kill is stated, not filtered (see
				// SessionLogVerdictEnv's retirement note).
				appendSessionLogVerdict(sessionLogVerdictEvent(r))
				if signalForwarding() {
					reRaise(sig)
				}
			case <-done:
				return
			}
		}
	}()
	var stopOnce sync.Once
	// Declared, then assigned: the release clears the slot holding itself, so the
	// literal cannot take its type from its own body.
	var stop func()
	stop = func() {
		stopOnce.Do(func() {
			signal.Stop(ch)
			close(done)
			// The slot is CLEARED on release, so RecorderInstallIsLive answers
			// about NOW rather than about whether Install was ever called: a
			// released registration that still reported "live" would make the
			// reader lie in exactly the direction that hides an unkillable
			// process.
			installStop.CompareAndSwap(&stop, nil)
		})
	}
	// ONE recorder per process, so ONE uninstall is the process-lifetime
	// registration this package holds. ReleaseSignalHandlers reads it (see
	// there for why that is the only way a re-raise can reach the default
	// disposition without a blanket Reset).
	installStop.Store(&stop)
	return stop
}

// installStop holds the CURRENT process's recorder uninstall (nil before
// Install, or when recording is disarmed). A package-level one-entry slot is the
// honest shape: Install is documented as idempotent per process, and a second
// call replaces the slot rather than accumulating (an old registration left
// behind would be exactly the invisible subscriber this type exists to expose).
var installStop atomic.Pointer[func()]

// ReleaseSignalHandlers stops the recorder's OWN process-lifetime registration
// and reports whether SIGINT/SIGTERM are now unsubscribed by this package.
//
// WHY A CALLER NEEDS THIS, AND WHY `signal.Stop(callerCh)` IS NOT ENOUGH
// (measured 2026-10-24, standalone probe, this host):
//
//	one registration, Stop, self-kill                    → exit 130 (dies)
//	a PEER registration live, Stop, self-kill            → exit 0  (STILL ALIVE)
//	the same peer, Stop, signal.Reset, self-kill         → exit 130 (dies)
//	the same peer stopped too, Stop, self-kill           → exit 130 (dies)
//
// The peer in the measured run stands in for THIS package: main.go installs the
// recorder at startup and the recorder holds SIGINT/SIGTERM for the process's
// whole life, so a stop path that drops only its own channel leaves a runtime
// handler installed and its re-raise is SWALLOWED — the process becomes
// unkillable by the very signal the operator just pressed. That is the failure
// mode a blanket `signal.Reset(sig)` was written to avoid, and PROJECT.md Rule 42
// bans that remedy because it is not a remedy: Reset is
// `cancel(sig, disableSignal)` over EVERY registered channel, so it dismantles a
// handler some OTHER package installed as a side effect of this one's exit
// (measured: cmd/agent's second-Ctrl-C path, and the testutil arm that hung for
// 90s). Releasing each registration through its owner is the same outcome
// without that collateral damage.
//
// So the re-raise gets the default disposition by the OTHER route: every
// subscriber that means to die releases its OWN registration, and the LAST one
// out reaches the kernel's default action with nothing left to swallow the
// signal. This function is that release for the recorder.
//
// It DOES NOTHING when the recorder was never installed (a library process, a
// `go test` binary) and reports true, because there is then no registration of
// this package's to leave behind.
func ReleaseSignalHandlers() bool {
	p := installStop.Load()
	if p == nil {
		return true
	}
	(*p)()
	// The stop clears the slot itself, so this only settles the case where a
	// concurrent Install replaced the slot between the load and the call.
	installStop.CompareAndSwap(p, nil)
	return true
}

// RecorderInstallIsLive reports whether this process currently holds the
// recorder's registration. It is the reader a stop path uses to say — without
// guessing — that SOMETHING still subscribes, so a Stop-only re-raise would be
// swallowed; the answer selects between "release and re-raise" and "there is
// nothing of mine left to release".
func RecorderInstallIsLive() bool { return installStop.Load() != nil }

// signalBufSize / watchedSignals: the catchable signals worth a record. SIGKILL
// cannot be caught and therefore cannot be recorded — the INTENT line is the
// only evidence for it, which is why Intent exists.
//
// SIGQUIT IS DELIBERATELY ABSENT. Registering it with signal.Notify would
// disable Go's runtime default for SIGQUIT — the goroutine stack dump an
// operator gets from `kill -QUIT <pid>`, which is the FIRST tool for diagnosing
// a wedged process. Trading that away for a provenance line on the one signal
// that is already self-describing (it dumps before it exits) would make the
// fleet harder to debug, not easier.
const signalBufSize = 4

// gracefulStopSignals is the TERMINATING set: the signals that mean "this
// process is going away, finish what is in flight and leave". It is the
// obligation every mode listed in modesThatOwnTheirSignalHandlers takes on — the
// recorder registers the same signal with os/signal (which REPLACES the kernel's
// default disposition) and then declines to re-raise it for those modes, on the
// stated premise that the mode's own handler will act. A mode that registers
// LESS than this set therefore turns a recorded signal into a SILENT DROP: the
// process neither dies nor drains (measured, task 01a15bb9: worker and chat both
// lacked SIGHUP).
//
// SIGUSR1/SIGUSR2 are NOT in here: they are gopi's own vocabulary (force /
// secondary spellings), not termination, and a mode may legitimately not take
// them. So the invariant this slice exists for is one-directional — every owning
// mode's set must be a SUPERSET of this one — and it is machine-checked by
// TestEveryHandlerOwningModeRegistersTheRecordersTerminatingSet (cmd/agent).
var gracefulStopSignals = []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP}

// GracefulStopSignals is the EXPORTED face of gracefulStopSignals, returned as a
// COPY: callers install it into their own Notify registration and must not be
// able to mutate the recorder's own definition (Rule 14 — one definition, every
// consumer imports it instead of re-typing the list).
func GracefulStopSignals() []os.Signal {
	out := make([]os.Signal, len(gracefulStopSignals))
	copy(out, gracefulStopSignals)
	return out
}

// IsGracefulStopName reports whether a RECORDED signal spelling (the "signal"
// field of a provenance line — "SIGTERM", "SIGINT", "SIGHUP") names one of the
// terminating signals above.
//
// WHY IT TAKES THE NAME AND NOT an os.Signal: the record is the only witness a
// killed process leaves behind, and a READER (a close-out sweep deciding whether
// a row died of a stop signal, an operator's jq filter) has the spelling on disk
// and nothing else. Deriving the set from gracefulStopSignals via SignalName is
// what makes this the ONE definition (Rule 14) — a reader that re-typed the three
// names would drift the moment a signal is added above, and the drift would show
// up as a row nobody closed.
//
// SIGUSR1/SIGUSR2 answer false on purpose: they are gopi's own control
// spellings (force / secondary), not termination, and a row whose runner received
// one is not a row whose runner was killed.
func IsGracefulStopName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, sig := range gracefulStopSignals {
		if SignalName(sig) == name {
			return true
		}
	}
	return false
}

// OwningModeNames returns the modes modesThatOwnTheirSignalHandlers declares —
// sorted, so the answer is stable for a reader and for a test. It is the reader a
// guard uses to insist that EVERY declared mode is accounted for by a real
// registration (or by a stated refusal), instead of the guard carrying its own
// copy of the list and drifting from the table.
func OwningModeNames() []string {
	out := make([]string, 0, len(modesThatOwnTheirSignalHandlers))
	for name := range modesThatOwnTheirSignalHandlers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// watchedSignals is what the recorder takes: the terminating set plus gopi's own
// two control spellings (Rule 13: composed from the one definition above, never
// re-typed).
var watchedSignals = append(GracefulStopSignals(), syscall.SIGUSR1, syscall.SIGUSR2)

// WatchedSignals returns the recorder's own registration as a COPY. It exists so
// a guard can assert the relationship it depends on — that what the recorder
// TAKES (and therefore takes the kernel default away for) is a superset of what
// every handler-owning mode is held to — without re-typing either list.
func WatchedSignals() []os.Signal {
	out := make([]os.Signal, len(watchedSignals))
	copy(out, watchedSignals)
	return out
}

// snapshot builds a victim record from the live process state.
func snapshot(sigName, sessionLog string) record {
	pid := os.Getpid()
	ppid := os.Getppid()
	label, session, taskRun, childSession := role()
	cwd, _ := os.Getwd() //stylegate:ignore recorded as an OPTIONAL fact (an empty cwd is a missing fact, never a fabricated one); a failed Getwd must not cost the record its signal
	r := record{
		TS:          nowStamp(),
		Kind:        KindSignal,
		Signal:      sigName,
		PID:         pid,
		PPID:        ppid,
		PGID:        syscall.Getpgrp(),
		SID:         sidOf(pid),
		Cgroup:      procCgroup(pid),
		Role:        label,
		Argv:        strings.Join(os.Args, " "),
		Cwd:         cwd,
		Session:     session,
		TaskRun:     taskRun,
		ChildSes:    childSession,
		AgentAnchor: anchorOfProcess,
		ParentPID:   ppid,
		ParentName:  procString(ppid, commFileName),
		ParentArgv:  procString(ppid, cmdlineFileName),
		LogTail:     logTail(sessionLog),
	}
	return r
}

// procfs file names (one spelling, Rule 1b).
const (
	commFileName    = "comm"
	cmdlineFileName = "cmdline"
)

// sidOf reads the session id from /proc/<pid>/stat (field 6). Best-effort: a
// missing/odd stat yields 0 rather than a wrong number. The field is read
// through internal/procname.StatField — the ONE parse that counts from the LAST
// ')' (Rules 13/14); comm may contain spaces and parens, so a naive split would
// return a neighbouring column as a session id.
func sidOf(pid int) int {
	raw := procString(pid, statFileName)
	if raw == "" {
		return 0
	}
	sessionRaw, ok := procname.StatField(raw, procname.StatFieldSession)
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(sessionRaw)
	if err != nil {
		return 0
	}
	return n
}

// statFileName is procfs's per-process stat file. Its value lives in
// internal/procname (Rules 13/14), which also owns the ONE parse of its fields.
const statFileName = procname.StatName

// nowStamp renders the record timestamp in the local ISO-8601 form the rest of
// gopi's logs use.
func nowStamp() string {
	return time.Now().Format(time.RFC3339)
}

// Intent records that THIS process is about to signal another gopi process and
// why. It is written BEFORE the signal is sent, so a SIGKILL (uncatchable, and
// therefore invisible to the victim) still leaves a traceable line.
//
// It is written TWICE, into two sinks, and the second is the one that makes the
// CONSUMER side possible (epic signal-provenance-unsourced, 2026-09-17):
//
//  1. THIS process's own session dir — the record that answers "who signalled
//     the fleet?" when an operator greps the sender's trail;
//  2. the TARGET's own session dir — the record the VICTIM reads in its signal
//     handler to decide whether the kill was ANNOUNCED. Without this mirror the
//     victim cannot answer that question at all: it would have to enumerate
//     every session dir on the host while a signal handler is running, which is
//     both unbounded and wrong (the 1,032 intent records in the tree show how
//     many candidates a naive scan would face).
//
// The mirror is filed under the same O_APPEND fd protocol as every other
// record, so two senders racing on one victim still write whole lines. A mirror
// that cannot be filed is REPORTED through the returned error, never swallowed
// (Rule 15b) — but it never blocks the signal: the caller's stop is the
// operation, the record is its evidence.
//
// It returns the write error: unlike the signal path, a caller here is ordinary
// code that may log or assert on it (tests do). Production callers ignore it
// deliberately — see killWithProvenance.
func Intent(kind, reason string, targetPID, targetGroup int) error {
	if off() {
		return nil
	}
	label, session, taskRun, _ := role()
	pid := os.Getpid()
	rec := record{
		TS:          nowStamp(),
		Kind:        KindIntent,
		Role:        label,
		PID:         pid,
		PPID:        os.Getppid(),
		Cgroup:      procCgroup(pid),
		Argv:        strings.Join(os.Args, " "),
		Session:     session,
		TaskRun:     taskRun,
		AgentAnchor: anchorOfProcess,
		TargetPID:   targetPID,
		TargetGroup: targetGroup,
		Reason:      kind + ": " + reason,
		// THE STATE FIELD IS PRESENT ON EVERY RECORD KIND, and an intent is the
		// one kind that is NOT a delivery: it is written by the KILLER before it
		// sends anything. Saying so by name keeps si_capture a CLOSED vocabulary
		// instead of a key that means different things on different kinds
		// (Rules 15/15a: an empty string here would be indistinguishable from a
		// producer that forgot to stamp it).
		SICapture:       sigsource.CaptureNoDelivery,
		SICaptureReason: captureNoDeliveryReason,
	}
	own := writeIntentToTarget(rec, targetPID)
	if err := write(rec); err != nil {
		if own != nil {
			return fmt.Errorf("intent recorded in the target's dir but not in this process's own: %w", err)
		}
		return err
	}
	// The mirror's failure is reported when the own-dir write succeeded (the
	// reverse is already in the chain above). A nil `own` means the mirror was
	// not applicable (no resolvable target dir) — not an error.
	if own != nil {
		return fmt.Errorf("intent recorded in this process's own dir but not in the target's: %w", own)
	}
	return nil
}

// recordIntentLine renders one intent record as its JSON line — the shared
// half of both sinks, so the two lines a single kill produces are byte-identical
// apart from the session-dir/layout stamp write() adds.
func recordIntentLine(rec record) ([]byte, error) {
	line, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("signalrec: marshal intent record: %w", err)
	}
	return append(line, '\n'), nil
}

// writeIntentToTarget files the SAME intent record in the target's own session
// dir, so the victim's handler can read it. A nil error with a nil return means
// the mirror was not applicable; a non-nil error means it was attempted and
// failed (the caller reports it).
//
// It never creates a session dir: the victim's dir must ALREADY exist (it is
// the victim's own dir), and minting one here — keyed by a pid the resolver
// merely guessed — would file evidence under a path the victim never reads. A
// target with no resolvable dir is silently skipped for the same reason: there
// is nothing to mirror into, and a refusal would have to become an error on
// every unrelated kill (a bash tool child, a build) that is not a session
// process at all.
func writeIntentToTarget(rec record, targetPID int) error {
	dir := targetSessionDir(targetPID)
	if dir == "" {
		return nil
	}
	// The reason is the ARGUMENT that makes the kill legible on the victim's
	// own line, so it is stamped with the sender's pid: the victim's trail must
	// name a sender even when the sender's own dir is unreadable afterwards.
	rec.Reason = rec.Reason + " [by pid " + strconv.Itoa(rec.PID) + "]"
	rec.Note = "intent MIRRORED into the target's session dir so the victim can classify the kill (epic signal-provenance-unsourced)"
	rec.SessionDir = dir
	rec.Layout = LayoutMirror
	line, err := recordIntentLine(rec)
	if err != nil {
		return err
	}
	return appendLine(filepath.Join(dir, FileName), line)
}

// LayoutMirror marks a record filed in a dir OTHER than the writer's own — the
// intent mirror. A reader that sums records per session must not count a mirrored
// intent twice, and this discriminator is how it tells the difference.
const LayoutMirror = "mirror"

// targetSessionDir resolves the session dir a gopi process's records belong in,
// from OUTSIDE that process. Three facts, most specific first, all read from
// procfs (never shelled out, never enumerated across every session dir):
//
//  1. GOPI_SIGNALREC_SESSION_DIR in the target's environment — the SAME wire
//     contract a launcher uses to hand a child its dir, so a process that
//     received one is found exactly where it will write;
//  2. the target's CWD when it IS a session dir — every resident gopi role
//     (worker, task-run child) is started with its session dir as its working
//     directory, which is what the 2026-09-16 records show verbatim;
//  3. the process-keyed layout (sessions/process-<pid>) — the merger's own
//     layout, and the only dir a resident-without-task can have.
//
// Returns "" when none applies (an ordinary process — a bash tool child, a
// git, a build — has no session dir and must not be given one).
func targetSessionDir(pid int) string {
	if pid <= 0 {
		return ""
	}
	root, err := config.SessionsRoot()
	if err != nil || root == "" {
		return ""
	}
	if dir := strings.TrimSpace(envValueOf(pid, SessionDirEnv)); dir != "" && isSessionDir(dir) {
		return dir
	}
	if cwd, cerr := os.Readlink(filepath.Join(ProcRootName, strconv.Itoa(pid), cwdLinkName)); cerr == nil {
		cwd = strings.TrimSuffix(cwd, procstat.DeletedSuffix)
		if isSessionDir(cwd) && filepath.Dir(cwd) == root {
			return cwd
		}
	}
	// 4. The victim IS the resident mode its argv names → its dir is
	// residentDirFor, the layout it owns by definition, and it is CREATED here
	// when absent. Creating it is sound — unlike minting a dir for a guessed
	// pid — because the path is a pure function of the pid, so the victim reads
	// exactly this file when it records its own death (its own processDest
	// computes the same string). Without this the mirror would fail for every
	// resident victim on its FIRST kill, which is the case the guard exists for.
	if residentMode(argvOf(pid)) {
		dir := residentDirFor(pid, root)
		if err := os.MkdirAll(dir, sessionDirPerm); err == nil {
			return dir
		}
	}
	return ""
}

// argvOf reads another process's argv from procfs, NUL-separated. "" when the
// process is gone or unreadable — the caller then has no mode to trust and falls
// through rather than guessing.
func argvOf(pid int) []string {
	raw, err := os.ReadFile(filepath.Join(ProcRootName, strconv.Itoa(pid), cmdlineFileName))
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range strings.Split(string(raw), "\x00") {
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

// cwdLinkName / environFileName are the procfs entries the resolver above reads
// (one spelling each, Rule 1b).
const (
	cwdLinkName     = "cwd"
	environFileName = "environ"
)

// envValueOf reads one variable out of another process's environment block
// (/proc/<pid>/environ, NUL-separated). "" when absent or unreadable — the
// resolver then falls through to its next fact rather than guessing.
func envValueOf(pid int, name string) string {
	raw, err := os.ReadFile(filepath.Join(ProcRootName, strconv.Itoa(pid), environFileName))
	if err != nil {
		return ""
	}
	prefix := name + "="
	for _, kv := range strings.Split(string(raw), "\x00") {
		if after, ok := strings.CutPrefix(kv, prefix); ok {
			return after
		}
	}
	return ""
}

// appendLine appends one already-newline-terminated line to path with a single
// atomic O_APPEND write (the same protocol write() uses, so a mirror and an own
// record can never interleave into a half line).
func appendLine(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("signalrec: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }() //stylegate:ignore best-effort close of an append-only evidence sink; the write below is what matters
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("signalrec: append to %s: %w", path, err)
	}
	return nil
}

// noForward reports whether the re-raise is suppressed for this process.
func noForward() bool {
	return envFlag(EnvNoForward) || envFlag(envOwnsHandlers)
}

// signalForwarding decides whether THIS signal is re-raised with the default
// disposition. It is evaluated AT SIGNAL TIME, never once at install time.
//
// The distinction is the whole bug. Install() runs near the top of main(), long
// before the caller installs its own handler and claims ownership, so a decision
// captured at install time can only ever read "not claimed yet" -- it would
// re-raise the very processes the claim exists to protect. Reading it at signal
// time also matches the lifetime: the claim is about the process AS IT IS when
// the signal arrives (epic 0x01a0d65f, memory #5176).
func signalForwarding() bool {
	return forwardsDefault(roleFromArgv(os.Args)) && !handlersClaimed.Load() && !noForward()
}

// envOwnsHandlers is the DECLARED ownership channel, for a REGRESSION GUARD and
// for the cross-process case: a child process cannot call ClaimSignalHandlers()
// before it has run Install(), so its spawner states the claim in its
// environment instead. It feeds exactly the same predicate as the in-process
// claim (Install's forward decision), so there is ONE ownership decision with
// two ways to state it -- never two policies.
//
// Rule 21: read in noForward.
const envOwnsHandlers = "GOPI_SIGNALREC_OWNED_HANDLERS"

// CurrentTaskID is the ledger row THIS process is currently executing, as the
// launch handoff declared it ("" when it executes none — a resident daemon
// between rounds, a bare prompt). It exists so a stop surface can write the
// kill verdict against the row the death actually interrupted, without
// re-deriving the attribution the launcher already made (Rule 14: one writer of
// that fact, read here).
//
// IT IS THE ANNOUNCED VALUE, NOT THE OBSERVED ONE, and the difference is
// measurable. The handoff states it once, in the environment, BEFORE the process
// starts; a resident `gopi worker` therefore answers "" for its whole life — it
// declares no single row, because it claims one per round. Callers that have
// WATCHED a row (a worker's own claimed-round latch) hold better evidence and
// must prefer it; this function is the fallback for the case no observation
// reaches, which is the point below.
func CurrentTaskID() string { return declaredTaskID() }

// AnnouncedTaskID is CurrentTaskID under its honest name: the row the launch
// handoff ANNOUNCED for this process ("" when it announced none).
//
// The rename exists because of a MEASURED misattribution (2026-09-27): a worker's
// stop surface printed, on adjacent lines of one session log,
//
//	SENDER UNSOURCED SIGNAL SIGINT TASK 01a0deb7-… AT 2026-09-27T02:39:54+08:00
//	the ledger will carry unsourced for task ""
//
// One line knew the task id and the next said `task ""` — two producers, one
// piece of knowledge. The first line renders the worker's own claimed-round latch
// (workerStopTargetTask); the second wrote the ledger event keyed on THIS
// function, which for a long-lived worker is empty by construction. Reading a
// function called CurrentTaskID it looks like the same fact; it is not, and the
// name invited exactly that reading.
func AnnouncedTaskID() string { return declaredTaskID() }

// KillVerdict is the classification of the signal THIS process received, as the
// recorder computed it — the value the worker/merger stop surface renders for
// the operator and writes to the ledger. It is read (never re-derived) by the
// caller, so the session record, the console line and the ledger event all rest
// on ONE classification (Rule 14: one decision, three sinks).
type KillVerdict struct {
	// Received is false when no signal reached this process (the zero value):
	// the caller then writes nothing at all.
	Received bool
	// Signal is the recorded signal name (SignalSIGTERM, "SIGINT", …).
	Signal string
	// Classified is false when the verdict could not be established from
	// evidence (see classifySourced). Sourced is only meaningful when true.
	Classified bool
	// Sourced is the verdict: true = a known gopi stop path announced this
	// kill, false = no gopi stop path announced it.
	Sourced bool
	// IntentEvidence quotes the intent record a sourced verdict rests on.
	IntentEvidence string
}

// AwaitKillVerdict returns the classification of the signal this process
// received, WAITING (bounded by budget) for the recorder to finish classifying.
//
// The wait is not politeness: the recorder's handler and the stop surface are
// DIFFERENT goroutines, and the surface can win the race — the worker's loop
// scans its stop flag the instant the signal lands, which is before the
// handler has read the intent trail off disk. Reading here without waiting
// would make the guard silently no-op exactly on the fast path.
//
// It returns immediately when NO signal reached this process (Received false),
// so an ordinary exit never pays the budget — the wait is only ever entered on
// a death the recorder already knows about.
func AwaitKillVerdict(budget time.Duration) KillVerdict {
	deadline := time.Now().Add(budget)
	for {
		v := KillVerdictNow()
		if !v.Received || v.Classified || !time.Now().Before(deadline) {
			return v
		}
		time.Sleep(verdictPollInterval)
	}
}

// verdictPollInterval is the step of that wait (Rule 1).
const verdictPollInterval = 5 * time.Millisecond

// DefaultVerdictWait is the budget AwaitKillVerdict callers use on the shutdown
// path. It must cover the classification's own retry window
// (defaultIntentWait) plus scheduling: the verdict is worth a few milliseconds
// on a path that is already ending the process.
const DefaultVerdictWait = defaultIntentWait + 150*time.Millisecond

// latestVerdict holds the classification of the FIRST signal this process
// recorded. The recorder's handler already dedupes by signal name (seen map), so
// the earliest verdict is the one that describes WHY the process is leaving — a
// later signal must not overwrite the fact an operator is reading.
//
// It is published in TWO steps (received, then classified) so a reader that
// arrives during the classification sees "a signal arrived, verdict pending"
// rather than "nothing happened" — the difference between a bounded wait and a
// silently missing record.
var latestVerdict struct {
	mu sync.Mutex
	v  KillVerdict
}

// KillVerdictNow returns the classification of the signal this process received
// (the zero value — Received false — when none did). It is the exported reader
// the stop surfaces use, so no caller has to re-derive the verdict or guess at
// the recorder's internal state.
func KillVerdictNow() KillVerdict {
	latestVerdict.mu.Lock()
	defer latestVerdict.mu.Unlock()
	return latestVerdict.v
}

// noteSignalReceived publishes the FIRST step: a signal arrived (the signal's
// name is known, the verdict is not yet). Called before the classification so
// the stop surface's wait has something to wait for.
func noteSignalReceived(sigName string) {
	latestVerdict.mu.Lock()
	defer latestVerdict.mu.Unlock()
	if latestVerdict.v.Received {
		return
	}
	latestVerdict.v = KillVerdict{Received: true, Signal: sigName}
}

// repeatVerb is the machine-readable word every re-affirm carries — in the
// record's Note and in the sentence the handler prints. One spelling (Rule 14):
// an operator's filter and a regression guard reference it instead of re-typing
// a phrase that drifts the moment the wording changes.
const repeatVerb = "re-affirm"

// captureNoDeliveryReason is the reason an intent record carries beside
// sigsource.CaptureNoDelivery. One spelling (Rule 14): an operator's filter and
// the regression arm key on it instead of on prose that drifts.
const captureNoDeliveryReason = "this record is not a signal delivery: it was written by the KILLER before it sent anything, so there is no siginfo to capture"

// repeatCountField is the JSON key of the "which delivery is this" counter in
// the session-log re-affirm event. It is its own field (not the record's Note)
// because a reader that wants to know HOW MANY extra signals arrived must not
// have to parse an English sentence out of it.
const repeatCountField = "repeat"

// stampSignalSource puts the KERNEL'S OWN account of a delivery onto the record:
// si_code, its name, and — when the code says a PROCESS sent it — the sender's
// pid and the comm it had at that instant.
//
// WHY IT IS A SEPARATE STEP FROM snapshot(). snapshot answers "where was this
// process when it was hit"; this answers "who hit it". Keeping them apart is what
// lets the degradation be honest: on a host where internal/sigsource captured
// nothing (a non-Linux build, an unarmed signal, a refused rt_sigaction),
// Delivered reports Valid=false and NONE of these fields is written — the record
// keeps the exact shape it had before this epic. A zero si_code written in that
// case would be read as SI_USER, i.e. "an ordinary process sent this", which is a
// fabricated accusation (Rule 15a).
//
// sender_comm IS BEST-EFFORT AND ITS ABSENCE IS NOT AN ERROR. The sender is
// typically a short-lived child (`kill -INT <pid>`), and it can be gone before
// the victim is scheduled; /proc then has no entry to read. The record keeps
// sender_pid (the kernel already told us) and says nothing about the name — never
// "the sender had no name" (Rule 15: report what you measured, not a story).
func stampSignalSource(r *record, sig os.Signal) {
	// THE STATE GOES ON FIRST AND UNCONDITIONALLY, because it answers "was the
	// kernel asked at all" while the si_code fields below only answer "what did
	// it say". Putting it behind the capture check is precisely the pre-fix
	// shape: a record whose capture was absent lost every trace of the QUESTION
	// (Rule 15a — the state is a required value and can never be an absent key).
	r.SICapture, r.SICaptureReason = sigsource.Describe(sig)
	src := sigsource.Delivered(sig)
	if !src.Valid {
		return
	}
	code := src.Code
	r.SICode = &code
	r.SICodeName = sigsource.CodeName(code)
	if !sigsource.FromProcess(code) {
		// SI_KERNEL (128) and the positive fault codes: the kernel raised this
		// itself, so si_pid/si_uid carry no sender — the union those fields live
		// in holds an unrelated word (a fault address, a trap number). Writing it
		// as a pid would invent a suspect.
		return
	}
	pid := src.SenderPID
	r.SenderPID = &pid
	if comm := procString(int(pid), commFileName); comm != "" {
		r.SenderComm = comm
	}
}

// SICodeField, SICodeNameField, SenderPIDField and SenderCommField are the JSON
// keys of the four fields stampSignalSource adds. They are spelled ONCE here
// (Rule 14) so an operator's jq filter, a session-log reader and a test key on
// the same tag, and a rename cannot drift them apart.
const (
	SICodeField     = "si_code"
	SICodeNameField = "si_code_name"
	SenderPIDField  = "sender_pid"
	SenderCommField = "sender_comm"
	// SICaptureField / SICaptureReasonField are the JSON keys of the capture
	// STATE (see record.SICapture). Same rule, same reason: an operator's jq
	// filter and a test must key on one spelling.
	SICaptureField       = "si_capture"
	SICaptureReasonField = "si_capture_reason"
)

// noteRepeatedSignal records the SECOND and every later delivery of a signal
// kind this process has already recorded — the fact Install's handler used to
// drop with a bare `continue`.
//
// IT CHANGES NOTHING ELSE, and that is the contract, not an omission. It
// publishes no received/verdict/filed outcome (those answer what happened TO
// this process, and the re-affirm happened to nothing: recordFiled must keep
// naming the DEATH record a claiming door waits on), it never re-raises, and it
// moves no handler. A repeat re-affirms the same request and never escalates —
// the same words `gopi worker --stop` states on the console
// (cmd/agent/cli_worker_control.go) and the same contract its SIGUSR1/--force
// arm carries.
//
// TWO SINKS, BOTH LOUD ON FAILURE (Rule 15). The provenance sink is the file
// every existing reader already parses (the record's own `kind` discriminator
// keeps it out of the death counts), and the run's own session log is the file
// an operator actually opens after a kill. Failures are printed and never
// swallowed; neither sink may cost the process its exit.
func noteRepeatedSignal(sig os.Signal, delivery int, sessionLog string) {
	sigName := classifySignal(sig)
	r := snapshot(sigName, sessionLog)
	r.Kind = KindSignalRepeat
	stampSignalSource(&r, sig)
	r.Note = fmt.Sprintf("%s: delivery %d of %s — the first delivery's disposition stands (no re-raise, no duplicate death record)",
		repeatVerb, delivery, sigName)
	// The sink is appendRecord, NOT write: a re-affirm must not move the answer
	// a claiming door is waiting on (see write's publication step), so this path
	// neither publishes FILED nor reports a REFUSAL — it prints its own failure
	// and leaves recordFiled exactly as the death record left it.
	if err := appendRecord(r); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] signal provenance: repeated signal NOT RECORDED: %v\n", err)
		if line, merr := json.Marshal(r); merr == nil {
			fmt.Fprintf(os.Stderr, "%s\n", line)
		}
	}
	appendSessionLogVerdict(sessionLogRepeatEvent(r, delivery))
}

// recordFiledAt is publication step ONE of the "the record LANDED on disk" fact:
// the instant write() returned nil for this process's first record. It is the
// only thing a claiming door's unwind may synchronise on, and it is deliberately
// NOT the same fact as noteSignalReceived — "the handler woke up" is not "the
// record exists".
//
// WHY THIS EXISTS (measured 2026-10-12, pre-fix tree 45b54d81f). A verb that
// owned its own SIGTERM handler (the since-removed `gopi memory sync --every …
// --push`, deleted by 5677cc6e) printed its
// stop line and exited 0 in 12/12 runs with NO record on disk, while the same
// profile, same destination and same 2s wait filed the record on SIGHUP (which
// wakes no verb-side stop channel). The recorder's handler and the verb's
// handler are DIFFERENT goroutines woken by the same signal delivery, and the
// verb's unwind — sessionDest resolution, /proc reads, the session-log tail and
// the append — is expensive enough that os.Exit routinely wins. The provenance
// file is the deliverable of the whole epic, so a door that can outrun it is a
// hole in the evidence, and (before this) nothing in the tree reported the loss:
// a reader could not tell "never signalled" from "signalled, record lost".
var recordFiled struct {
	mu sync.Mutex
	// at is publication step ONE (above): non-zero only when the bytes are on
	// disk. outcome is the THREE-state answer a door needs (see noteRecordRefused)
	// and is published separately so that adding it could not weaken `at`.
	at      time.Time
	outcome RecordOutcome
	// reason is the refusal's own explanation, copied from the error
	// reportUnfiled already prints, so a reader of the state gets the SAME
	// sentence rather than a second rendering that could drift from it.
	reason string
}

// RecordOutcome is the record-filed handshake's ANSWER for one process: the
// three distinguishable states a claiming door must be able to tell apart. It is
// the vocabulary of the publication side (Rule 14: one home), and the door
// (cmd/agent) compares against these constants rather than re-typing them.
type RecordOutcome string

const (
	// RecordOutcomeNothingYet is the zero value: this process has neither filed
	// its record nor been refused one. A door that sees it while a signal has
	// arrived is looking at a race it must keep waiting on — the recorder's
	// goroutine has not reached its answer yet.
	RecordOutcomeNothingYet RecordOutcome = ""
	// RecordOutcomeFiled is set by write() on the ONE path where the bytes are
	// known to be on disk.
	RecordOutcomeFiled RecordOutcome = "filed"
	// RecordOutcomeRefused is set by reportUnfiled: the recorder decided, and
	// said so on stderr with its reason, that this record must NOT be written
	// anywhere (no public-dir fallback). It is the state the handshake used to
	// lack, and its absence is what made a deliberate refusal print as a LOSS
	// (measured 2026-10-12: the door burned its whole budget and then claimed
	// "this kill is recorded NOWHERE" for a rejection that was reported, on
	// purpose, with its own reason).
	RecordOutcomeRefused RecordOutcome = "refused"
)

// Refused reports whether this outcome IS the refusal state. It is a niladic
// accessor named for the value it answers (PROJECT.md Rule 20: no `is`/`get` prefix,
// and `Refused` reads as the question the door is asking).
func (o RecordOutcome) Refused() bool { return o == RecordOutcomeRefused }

// Filed reports whether this outcome IS the filed state.
func (o RecordOutcome) Filed() bool { return o == RecordOutcomeFiled }

// noteRecordFiled publishes the record-landed fact (see recordFiledAt). It is
// called by write() on the ONE path where the bytes are known to be on disk, so
// the fact can never be published for a refused or failed record — a waiter that
// settled on "we tried" would be the same silent loss in a new costume.
//
// The FILED answer WINS over a refusal: a process that wrote one record and was
// refused another has its evidence on disk, which is the fact the door reports.
func noteRecordFiled() {
	recordFiled.mu.Lock()
	defer recordFiled.mu.Unlock()
	if recordFiled.at.IsZero() {
		recordFiled.at = time.Now()
	}
	recordFiled.outcome = RecordOutcomeFiled
	recordFiled.reason = ""
}

// noteRecordRefused publishes the REFUSAL state with the reason the recorder
// already printed. It is the third state, and it exists because "the record did
// NOT land" has two causes that a door must not conflate: the recorder never
// finished (a race the door must keep waiting on, and a real loss if the budget
// runs out), or the recorder DELIBERATELY refused (an answer, already reported,
// printed by reportUnfiled).
//
// Rule 15 both ways: this function reports nothing itself — the reason was
// already printed by the caller — and it never overwrites a FILED answer,
// because a process with bytes on disk has not lost anything.
func noteRecordRefused(reason string) {
	recordFiled.mu.Lock()
	defer recordFiled.mu.Unlock()
	if !recordFiled.at.IsZero() {
		return
	}
	recordFiled.outcome = RecordOutcomeRefused
	recordFiled.reason = reason
}

// RecordFiledNow reports whether this process's provenance record has landed on
// disk. It is the exported reader of the publication step (Rule 14), so a caller
// that wants to ASSERT the record exists reads the same fact the waiter settles
// on instead of re-deriving it from the filesystem.
//
// ITS SEMANTICS ARE UNCHANGED: bytes on disk, nothing else. A refused record
// answers false here, exactly as before — the refusal is a THIRD state
// (RecordOutcomeNow), never a redefinition of this one.
func RecordFiledNow() bool {
	recordFiled.mu.Lock()
	defer recordFiled.mu.Unlock()
	return !recordFiled.at.IsZero()
}

// RecordOutcomeNow reports which of the three states this process reached:
// nothing yet (the zero value), filed, or refused. It is the exported reader the
// DOOR consults to tell a deliberate refusal from a pending (or lost) record —
// the distinction recordLostLine's false claim was made of.
func RecordOutcomeNow() RecordOutcome {
	recordFiled.mu.Lock()
	defer recordFiled.mu.Unlock()
	return recordFiled.outcome
}

// RecordRefusedNow reports whether the recorder REFUSED this process's record,
// and why. It is the one-call form of RecordOutcomeNow for the door that only
// needs the refusal arm; the reason is the sentence reportUnfiled printed
// (never a second rendering of it).
func RecordRefusedNow() (bool, string) {
	recordFiled.mu.Lock()
	defer recordFiled.mu.Unlock()
	return recordFiled.outcome == RecordOutcomeRefused, recordFiled.reason
}

// recordFiledPollInterval is the step of the wait below (Rule 1). It is small
// because the wait is on a shutdown path that already costs more.
const recordFiledPollInterval = 2 * time.Millisecond

// recordFiledDeliverySettle bounds the wait for a signal that has NOT yet
// reached the recorder's own goroutine. A SIGTERM wakes the recorder's handler
// and the door's handler independently, so "the recorder has not seen it YET" is
// not "there is nothing to wait for" — but it must not turn an ordinary exit
// into a budget-sized sleep either. Past this window the door concludes no
// signal reached the recorder and leaves immediately (Rule 1).
const recordFiledDeliverySettle = 5 * time.Millisecond

// DefaultRecordFiledWait is the budget a claiming door's unwind gives the
// recorder to get its record onto disk. It must exceed the recorder's own worst
// path — the classification's retry window (defaultIntentWait) plus the /proc
// reads, the session-log tail and the append — so that the arm is decided by the
// CODE and never by which goroutine the scheduler happened to run first.
const DefaultRecordFiledWait = 500 * time.Millisecond

// AwaitRecordFiled is THE waiter: it blocks (bounded by budget) until this
// process's provenance record has LANDED, and is the ONE synchronisation a
// claiming door's unwind performs before it lets the process exit.
//
// It returns true when the record is on disk, false when there is nothing to
// wait for (no signal reached this process: an ordinary exit pays only
// recordFiledDeliverySettle) or when the budget ran out — the caller MUST report
// that false, because an expired wait means the record is still missing and the
// whole point is that such a loss stops being invisible (Rule 15).
func AwaitRecordFiled(budget time.Duration) bool {
	if off() {
		return false
	}
	deadline := time.Now().Add(budget)
	settleBy := time.Now().Add(recordFiledDeliverySettle)
	for {
		if RecordFiledNow() {
			return true
		}
		if KillVerdictNow().Received {
			if !time.Now().Before(deadline) {
				return false
			}
		} else if !time.Now().Before(settleBy) {
			// No signal has reached the recorder: there is no record to wait
			// for, and an ordinary exit must not pay the budget.
			return false
		}
		time.Sleep(recordFiledPollInterval)
	}
}

// noteVerdict publishes the SECOND step: the classification of that signal.
func noteVerdict(r record) {
	latestVerdict.mu.Lock()
	defer latestVerdict.mu.Unlock()
	v := KillVerdict{Received: true, Signal: r.Signal, IntentEvidence: r.IntentEvidence}
	if r.Sourced != nil {
		v.Classified = true
		v.Sourced = *r.Sourced
	}
	latestVerdict.v = v
}

// residentDirHandoff returns the process-keyed dir a RESIDENT mode was handed
// through SessionDirEnv, and whether it applies. It is the accepting half of
// the same rule residentDirFor computes: a resident mode's dir is a function of
// its pid, so an env that names exactly that path is not a foreign claim to be
// validated — it is this process's own dir, stated.
//
// Non-resident modes and paths outside the resident namespace return false, so
// the strict handoff validation still governs every other case (a one-shot
// command inheriting a pair must still be refused).
func residentDirHandoff(root, label string) (string, bool) {
	if forwardsDefault(label) {
		return "", false
	}
	handed := strings.TrimSpace(os.Getenv(SessionDirEnv))
	if handed == "" {
		handed = recordedSessionDir()
	}
	if filepath.Clean(handed) != filepath.Clean(residentDirFor(os.Getpid(), root)) {
		return "", false
	}
	if err := os.MkdirAll(handed, sessionDirPerm); err != nil {
		return "", false
	}
	if dirExists(handed) != nil {
		return "", false
	}
	return handed, true
}

// residentDirFor is THE definition of a resident mode's session dir:
// <sessions root>/process-<pid>. It is a pure function of the pid and the root,
// so the writer (this package, in processDest) and any reader outside the
// process (targetSessionDir, classifying a kill) compute the same path from the
// same two facts — which is what makes a kill verdict readable at all.
func residentDirFor(pid int, root string) string {
	return filepath.Join(root, processDirPrefix+strconv.Itoa(pid))
}

// residentMode reports whether argv names a mode that owns its signal handlers
// (the modes with a lifecycle, which is what a resident dir is for).
func residentMode(args []string) bool { return !forwardsDefault(roleFromArgv(args)) }

// classifySourced decides, ON THE VICTIM SIDE, whether a known gopi stop path
// announced the signal that is being recorded — and stamps the verdict on the
// record. It is the consumer half of the two-record protocol this package has
// written since the signal-provenance epic and never read.
//
// THE PROTOCOL. A gopi stop path writes a KindIntent record naming its target
// (Intent) and only THEN delivers the signal, so on the wire the intent line
// PRECEDES the victim's own line. The killer files that line in the target's
// session dir (writeIntentToTarget), which is what makes this a single file
// read rather than a scan of the host's 73,845 session dirs.
//
// THE THREE VERDICTS, and why the third exists. A matching intent → SOURCED
// (true). No intent, and the file was READABLE → UNSOURCED (false) — the
// 2026-09-12/16 shape: eight gopi processes SIGTERMed inside one second with
// zero intent records in the tree. No intent, and the classification could not
// be attempted or completed (no session dir, no file, a read error, the
// killer's write still in flight) → NOT CLASSIFIED (nil). The third case is
// deliberately NOT folded into "unsourced": the word accuses an operator of a
// kill nobody announced, and it must rest on a read that actually happened.
//
// A process that never received a session dir to record into is left
// unclassified for the same reason — there is nowhere the intent could have
// been mirrored, so absence is not evidence.
func classifySourced(r *record) {
	d, err := sessionDest()
	if err != nil {
		return
	}
	dir := filepath.Dir(d.Path)
	sourced, evidence, ok := intentFor(dir, r.PID)
	if !ok {
		return
	}
	r.Sourced = &sourced
	if evidence != "" {
		r.IntentEvidence = evidence
	}
}

// intentFor reads dir's provenance file and reports whether a KindIntent record
// naming pid precedes this signal. ok=false means the question could not be
// answered from evidence (no session dir for the victim, or an unreadable
// file); ok=true is a verdict worth recording either way.
//
// ABSENCE OF THE FILE IS AN ANSWER, and this is the case the guard exists for:
// a victim writes its own record here, so before the first kill the file simply
// is not there. "This dir holds no intent naming me" is exactly what the
// 2026-09-16 cohort looked like. Only a READ FAILURE (permissions, I/O) leaves
// the question unanswered — a false "unsourced" must never rest on an error.
//
// The read is retried within intentWait because the killer writes its line and
// signals in the same breath: the victim's handler can win that race by
// microseconds. The budget is bounded (Rule 1) and a miss yields unsourced,
// which is the honest verdict — nothing was filed, at the moment we looked.
func intentFor(dir string, pid int) (sourced bool, evidence string, ok bool) {
	path := filepath.Join(dir, FileName)
	deadline := time.Now().Add(intentWait())
	for {
		raw, err := os.ReadFile(path)
		switch {
		case err == nil:
			if ev, matched := matchingIntent(raw, pid); matched {
				return true, ev, true
			}
			return false, "", true
		case errors.Is(err, os.ErrNotExist):
			if !time.Now().Before(deadline) {
				return false, "", true
			}
			time.Sleep(intentPollInterval)
		default:
			// A file we cannot read is not evidence of anything; say so rather
			// than accusing an operator (Rule 15: no silent guess).
			fmt.Fprintf(os.Stderr, "[gopi] signal provenance: cannot read %s: %v — kill left UNCLASSIFIED\n", path, err)
			return false, "", false
		}
	}
}

// matchingIntent scans a provenance file's lines for an intent record whose
// target is pid, and renders the evidence string the record carries ("<ts>
// <role> → <reason>"). A line that is not JSON, or not an intent, is skipped —
// a hand-edited or truncated trail must never crash the signal handler.
func matchingIntent(raw []byte, pid int) (string, bool) {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Kind != KindIntent || rec.TargetPID != pid {
			continue
		}
		return rec.TS + " " + rec.Role + " → " + rec.Reason, true
	}
	return "", false
}

// intentWait resolves the classification budget (Rule 2: the env is read in ONE
// place, the constant is the default).
func intentWait() time.Duration {
	raw := strings.TrimSpace(os.Getenv(IntentWaitEnv))
	if raw == "" {
		return defaultIntentWait
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return defaultIntentWait
	}
	return time.Duration(n) * time.Millisecond
}

// defaultIntentWait is how long the classification retries for the killer's
// intent line. It is short by construction: the intent is written immediately
// before the signal (microseconds), so this covers only scheduling jitter — and
// the victim's own shutdown must not be delayed by a forensic read.
const defaultIntentWait = 50 * time.Millisecond

// intentPollInterval is the retry step inside that budget.
const intentPollInterval = 5 * time.Millisecond

// sessionLogVerdictEvent renders the verdict a victim appends to its session
// log: "the session log AND the provenance trail record this kill". The caller
// appends it whenever this process owns a session log; the DECISION that used to
// sit here — write the line or not, taken from the ownership predicate — is now
// the event's own CONTENT. That is the improvement, not a simplification:
// "nobody announced this kill" is the fact an operator needs after a mass kill,
// so it must be stated rather than filtered out, and "we could not find out"
// keeps its own spelling (an absent SourcedField).
//
// IT IS A STRUCTURED EVENT, NOT PROSE, and that is a correctness requirement
// rather than a style choice: internal/gopilog reads a session file with two
// parsers that disagree about junk — ValidateSessionFile / SessionRawLogPath
// json.Unmarshal each line and silently `continue` on a failure, while ParseFile
// (the unified reader behind `gopi session log --jsonl`, `gopi session log --chat`, `gopi
// stats`) returns a hard "JSON parse error" on ANY unparsable line. A prose line
// would therefore make the session invisible to the tolerant readers and
// UNREADABLE to the strict one — `gopi session log --jsonl` would fail on exactly the
// session an operator opens after a kill. Every reader tolerates this shape
// structurally instead: the envelope is the same JSON object every other event
// is (a "type" and a timestamp, no "message" key), so each formatter's type
// switch falls through and renders nothing.
//
// The verdict travels in the record's OWN field name (SourcedField), so the
// operator's jq filter over the SESSION LOG is the one they already run over the
// provenance trail.
func sessionLogVerdictEvent(r record) []byte {
	return eventLine(sessionLogEvent(r))
}

// sessionLogRepeatEvent renders the event a victim appends for a REPEATED
// delivery of a signal kind it has already recorded.
//
// IT IS ITS OWN TYPE (sessionevent.EventTypeSignalRepeat), and the separation is
// load-bearing rather than cosmetic. An operator's post-kill filter over the
// session log is `"type":"signal_verdict"`, and that question — "was this run
// killed, and did a gopi stop path announce it?" — must keep ONE answer per run
// whatever the key was pressed. Folding a re-affirm into that type would make a
// four-press Ctrl-C read as four verdicts, i.e. it would inflate exactly the
// count the verdict event exists to state.
//
// STRUCTURALLY TOLERANT, like every other gopi session event: a plain JSON
// object with a `type` and no `message` key, so every reader that switches on
// event type without a default skips it (internal/gopilog, internal/testutil's
// accounting). The alternative — prose — is the format this package already
// rejected: one session reader hard-fails on an unparsable line, so a prose line
// would be invisible to two readers and fatal to the third.
func sessionLogRepeatEvent(r record, delivery int) []byte {
	return eventLine(map[string]any{
		envelopeKeyType:      sessionevent.EventTypeSignalRepeat,
		envelopeKeyTimestamp: time.Now().Format(time.RFC3339Nano),
		signalKeySignal:      r.Signal,
		signalKeyPID:         r.PID,
		signalKeyRole:        r.Role,
		repeatCountField:     delivery,
	})
}

// Envelope and payload key names of the verdict event. They are named constants
// (Rule 1) because each is a wire identifier a reader matches on; they live here
// because this package is the WRITER — the readers own the format.
const (
	envelopeKeyType      = "type"
	envelopeKeyTimestamp = "timestamp"
	signalKeySignal      = "signal"
	signalKeyPID         = "pid"
	signalKeyRole        = "role"
	signalKeyIntent      = "intent_evidence"
)

// sessionLogEvent is sessionLogVerdictEvent without the encoding — the value a
// test asserts on and the encoder consumes.
func sessionLogEvent(r record) map[string]any {
	ev := map[string]any{
		envelopeKeyType:      sessionevent.EventTypeSignalVerdict,
		envelopeKeyTimestamp: time.Now().Format(time.RFC3339Nano),
		signalKeySignal:      r.Signal,
		signalKeyPID:         r.PID,
		signalKeyRole:        r.Role,
	}
	// SourcedField is written ONLY when the kill was classified. The three
	// states must stay distinguishable on the wire (record.Sourced's own doc):
	// an absent field is "the question could not be answered", never a
	// fabricated verdict — and never the same claim as the false this epic
	// exists to surface.
	if r.Sourced != nil {
		ev[SourcedField] = *r.Sourced
	}
	if r.IntentEvidence != "" {
		ev[signalKeyIntent] = r.IntentEvidence
	}
	return ev
}

// eventLine encodes one event as a single JSONL line, reporting an unencodable
// event rather than dropping it silently (Rule 15). The values here are strings,
// an int and a bool, so a failure is not expected — which is exactly why it must
// be visible if it ever happens instead of the fact of the kill vanishing.
func eventLine(ev map[string]any) []byte {
	b, err := json.Marshal(ev)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] signal provenance: session-log verdict event not encoded: %v\n", err)
		return nil
	}
	return append(b, '\n')
}

// appendSessionLogVerdict appends one verdict event line to the victim's OWN
// session log — the path resolved from ONE place (resolveSessionLog, fed by
// Install's argument and the SetSessionDir handoff). It is best-effort by design
// (this runs on a shutdown path) and it never fails the shutdown: a session log
// that cannot be appended to must not cost the process its record, which is
// already on disk by the time this is called.
func appendSessionLogVerdict(line []byte) {
	if len(line) == 0 {
		return
	}
	path := resolveSessionLog()
	if path == "" {
		return
	}
	// O_APPEND and nothing else: NEVER O_CREATE, unlike the package's own
	// provenance sink. The session log belongs to the run's logger, and this is a
	// witness appended to a file that already exists. Creating one here would
	// mint a session record the logger never wrote — a file internal/sessionlayout
	// counts as a real session — which is exactly the fabricated evidence this
	// package refuses.
	if err := appendLine(path, line); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] signal provenance: session-log verdict event not written: %v\n", err)
	}
}

// reRaise restores a signal's default disposition and re-delivers it to this
// process, so the recorder never turns a terminating signal into a no-op. It
// waits defaultForwardGrace first so the record is on disk: the whole point is
// that the evidence outlives the process.
//
// NO `signal.Reset`, AND RELEASING IS THE PART THAT REPLACES IT (PROJECT.md Rule
// 42; measured 2026-10-24). The recorder only re-raises when the process owns no
// handler of its own (see Install and ClaimSignalHandlers), so this package's
// registration is the one that would swallow the raise. Releasing it FIRST — the
// same uninstall the recorder's own stop function performs, one entry point
// (ReleaseSignalHandlers) — leaves the signal with no subscriber, and the
// default action therefore succeeds with no blanket reset. `signal.Reset(sig)`
// here was Rule 42's violation in this file: Reset is `cancel(sig,
// disableSignal)` over EVERY registered channel, so it dismantled handlers other
// packages had installed as a side effect of this one's exit. The measured
// replacement, on this host:
//
//	recorder registration live, Stop only, self-kill        → exit 0 (STILL ALIVE)
//	recorder registration released, no Reset, self-kill     → dies by the signal
//	(and the peer-registration probe in ReleaseSignalHandlers' header)
//
// If the raise is STILL swallowed (something outside this package subscribes),
// the process must say so rather than continue as though it had died — see
// reraiseSurvived.
func reRaise(sig os.Signal) {
	s, ok := sig.(syscall.Signal)
	if !ok {
		return
	}
	time.Sleep(defaultForwardGrace)
	ReleaseSignalHandlers()
	//stylegate:ignore the deliberate re-raise IS the feature (default-
	// disposition fidelity); ESRCH here means we already died, which is fine.
	if err := syscall.Kill(os.Getpid(), s); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] signal provenance: re-raising %v: %v\n", sig, err)
		return
	}
	reraiseSurvived(sig)
}

// off reports whether recording is disarmed.
func off() bool {
	return envFlag(EnvOff)
}

// EnvRecordingOff reports whether the recorder is DISARMED in this environment —
// the exported reader of EnvOff for a process that must know before it installs
// (a test stand-in asserts it is armed, so a stray disable cannot turn a
// provenance assertion vacuous).
func EnvRecordingOff() bool { return off() }

// GOOSName is exported for tests that must skip the procfs-only assertions on a
// non-Linux host (the recorder degrades to "no parent facts", never to a wrong
// one).
func GOOSName() string { return runtime.GOOS }
