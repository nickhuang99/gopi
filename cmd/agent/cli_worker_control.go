package main

// cli_worker_control.go — the worker DAEMON's operator control surface
// (`gopi worker --status | --stop | --restart | --start`), the analogue of the
// merger's cli_merger_stop.go.
//
// WHY IT EXISTS (epic worker-daemon-singleton, 2026-09-13): `gopi worker
// --daemon` used to be unmanageable — nothing said whether a resident worker
// was running, and the only way to end one was to find its pid by hand and
// kill it (which abandons an in-flight round's close-out). With the daemon
// face now guarded by a single-instance flock (only ONE resident worker may
// run), the operator needs the same three answers the merger already gives:
// is it alive, what is it doing, and how do I restart it cleanly.
//
// THE COMMAND SHAPE mirrors the merger's deliberately (gopi merger --stop /
// --restart / --start / --status):
//
//	gopi worker --status
//	gopi worker --stop    [--wait 25m] [--force]
//	gopi worker --restart [--wait 25m] [--force]
//	gopi worker --start
//
//   - `--stop` is the ONE primitive; `--start` starts a daemon and `--restart`
//     is stop-then-start. One primitive = one exit path to get right.
//   - The cross-process step is UNAVOIDABLE in any shape: the resident daemon
//     holds the flock for its whole life, so a "start the new one" that did not
//     wait would simply be refused. `--restart` is the atomic spelling of the
//     two steps an operator would otherwise type by hand.
//   - The durable request file (tools.ControlRequestPath) is the RECORD; the
//     signal is the NUDGE. Both are always sent, so either mechanism alone is
//     sufficient — and a worker binary that predates this epic still stops on
//     the signal.
//
// EXIT CODES (the machine-readable half of the contract, for scripts/units):
//
//	0  the requested end state holds (nothing was running / it stopped in time)
//	1  the end state was NOT reached (still busy past --wait, the new daemon
//	   refused, the stop was refused)
//
// THE FOREGROUND FACE IS UNTOUCHED: `gopi worker` (no --daemon) still holds NO
// lock and needs no control surface. `--status` reports the DAEMON; a
// foreground worker is deliberately invisible to it (that is the observability
// the briefing asks for — you debug by running one in the front).

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/signalrec"
	"github.com/nick/gopi/internal/tools"
)

// workerControlSignals are the signals that end the CONTROL command's wait. It
// is deliberately NOT workerStopSignals: that set is the worker's own drain
// vocabulary and carries SIGUSR1, which means "interrupt the round" — the
// sender must never read the escalation spelling as "stop waiting", or a
// `--force`-shaped signal aimed at this process would silently consume itself.
// SIGTERM is here because a service unit's stop must not park either.
var workerControlSignals = signalrec.GracefulStopSignals()

// --- named constants (PROJECT.md Rule 1: one place to find and change) ---

const (
	// workerStateDirName is the directory (under the gopi data root) holding the
	// worker daemon's per-instance control files. One directory per COMPONENT,
	// so an operator has one place to look and a sandboxed worker (GOPI_HOME)
	// never writes into the real profile.
	// workerStateDirName is deliberately its OWN spelling and NOT
	// CliVerbWorker: the directory is persisted data, so a future rename of the
	// CLI verb must not silently move every operator's state directory.
	workerStateDirName = "worker"
	// workerStateFilePrefix names an instance's state file: "worker-state-<pid>".
	// Separate naming from the request file so one directory listing
	// distinguishes "someone asked this pid to stop" from "this pid is running".
	workerStateFilePrefix = "worker-state-"
	// workerStopRequestPrefix names the completion request file for pid. The
	// spelling carries the signal's INTENT (terminate gracefully) — a bare
	// "12345" would not say why the file exists. It is deliberately the SAME
	// prefix the merger uses for its request files (one vocabulary across the
	// components), but scoped to the worker's own directory.
	workerStopRequestPrefix = tools.StopRequestPrefix

	// workerControlStatus / workerControlStop / workerControlRestart /
	// workerControlStart / workerControlForce / workerControlWait are the
	// control flags this face owns. They are NOT worker-loop flags: they name an
	// ACTION on the resident service, so they live here (Rule 1b: each spelling
	// defined once, referenced by the parser, the usage text and the tests).
	// workerControlStatus is the flag spelling of the read-only enumeration.
	workerControlStatus = "--status"
	// workerControlList IS the same read, spelled the way an operator reaches
	// for it (epic worker-list-alias, 2026-10-16): `gopi worker list` is the
	// natural question "which workers are running?", and until this alias
	// existed the ONLY door was the --status flag — a capability the operator
	// could not find, so the answer existed and went unasked. It is a
	// POSITIONAL (no dashes) and it carries the SAME action as --status, so the
	// two spellings cannot come to answer differently (Rule 2: one behaviour,
	// two spellings).
	//
	// TWO gates must know this word, and passing one does NOT pass the other —
	// the defect measured on the shipped binary 2026-09-14 for the sibling
	// read-only alias, restated here because it repeats:
	//   - ROUTING (workerControlRequested → workerStatusRequested): the word
	//     must open the control route. Without it the word falls through to the
	//     worker loop's argv scanner, where a bare positional is an EXPLICIT
	//     TASK ID — so `gopi worker list` would claim and run a row named
	//     "list", i.e. a read-only question turned into a mutation;
	//   - ROLE (workerControlRoleOf, which adds this face's own read-only
	//     aliases to the shared controlActionRoleOf): an unclassified alias
	//     leaves the read+act refusal half blind, so `gopi worker list --stop`
	//     would print the report and exit 0 with the --stop silently DROPPED —
	//     the F1 defect (a monitor concludes the service was stopped).
	workerControlList    = "list"
	workerControlStop    = "--stop"
	workerControlRestart = "--restart"
	workerControlStart   = "--start"
	// workerControlForce's SPELLING derives from the shared action word, exactly
	// as the merger's does (Rule 13/14) — see the merger face's comment.
	workerControlForce = "--" + controlModifierActionWord
	workerControlWait  = "--wait"

	// workerControlExitOK / workerControlExitFailed are the two documented exit
	// codes (Rule 1b: declared once so the usage text and the code cannot drift).
	workerControlExitOK     = 0
	workerControlExitFailed = 1

	// workerWaitBoundDefault is how long `--stop`/`--restart` wait for the
	// resident worker to finish its in-flight round before reporting failure.
	// The worker's own drain contract is shorter than the merger's pass, so a
	// generous bound costs nothing and a too-tight one would report a healthy
	// worker as stuck.
	workerWaitBoundDefault = 10 * time.Minute

	// workerControlPoll is the CLI's liveness poll cadence while it waits.
	workerControlPoll = 500 * time.Millisecond

	// workerControlAbortExit is the exit status of a control invocation the
	// OPERATOR'S OWN Ctrl-C ended (measured 2026-10-16, task
	// 01a14316-b7cb). It is neither 0 nor 1: the requested end state was never
	// observed, so claiming success would be a lie, and the two pre-existing
	// codes already mean "the end state holds" / "the end state was not
	// reached" — a THIRD spelling is the only one that lets a wrapper tell
	// "the operator gave up on the wait" from "the wait expired". It is
	// deliberately non-zero (the acceptance contract) and below 128 so it
	// cannot be confused with a signal death.
	workerControlAbortExit = 3

	// workerControlAbortRecordSettle bounds the wait, at Ctrl-C time, for the
	// recorder's provenance record to reach the disk (signalrec's
	// AwaitRecordFiled). The record is filed by another goroutine, and
	// os.Exit ends the process outright — so without this the operator's very
	// act of giving up would destroy the evidence of it. Bounded, because an
	// operator pressing Ctrl-C is owed a prompt exit; the abort always
	// proceeds, whether or not the record made it.
	workerControlAbortRecordSettle = 2 * time.Second

	// workerControlRewaitAfterCtrlC is how long a REPEATED Ctrl-C waits for the
	// process to leave naturally before the interruptible path exits anyway.
	// It is the visible half of "a repeat re-affirms, it never escalates":
	// this process has no escalation to perform (it signals nobody), so its
	// only choices are to keep waiting or to leave — and it leaves.
	workerControlRewaitAfterCtrlC = 500 * time.Millisecond

	// workerControlAbortRecheckEvery / workerControlAbortRecheckTries close the
	// race a signal handler can lose: a Ctrl-C landing microseconds after the
	// loop established "the pid is gone" would print "no longer waiting" and
	// exit non-zero for a stop that in fact SUCCEEDED. Re-confirming the fact
	// twice, a tick apart, is what lets the abort report the truth instead of
	// the ordering of two goroutines. It is a confirmation, never a re-wait.
	workerControlAbortRecheckEvery = 25 * time.Millisecond
	workerControlAbortRecheckTries = 2

	// workerStartSettleWait bounds the wait for a daemon that has not yet
	// OPENED ITS CLAIM LOOP (no round boundary in its state file). It is
	// deliberately a SECOND, separately named bound rather than part of the
	// operator's `--wait`: the durable request is consumed at the TOP of a
	// round, so a `--stop` delivered while the daemon is still in its start-up
	// prologue cannot be honoured until that prologue ends — and the prologue
	// is a MEASUREMENT OF THE MACHINE (measured 2026-10-03: banner -> first
	// round boundary 3-8s at load 4-10, and more than 20s at load 15.9, where
	// the resident scratch sweep alone spent 4.5s and the rest went to one
	// ledger read over a 56k-entry sessions tree). Letting start-up consume the
	// in-flight-round bound made the command report "pid N is STILL running 20s
	// after the graceful stop request" while the worker had never reached a
	// boundary — a Rule 15 violation: the message named the wrong wait. This
	// bound exists so "never opened its claim loop" is said BY NAME.
	workerStartSettleWait = 60 * time.Second

	// workerStartSettlePoll is that wait's poll cadence (procfs and the state
	// file have no notification channel, so a poll is the honest shape — and a
	// cheap one: one ReadDir plus one small JSON read).
	workerStartSettlePoll = 200 * time.Millisecond

	// workerStartSettleEnv overrides workerStartSettleWait, read in exactly ONE
	// place (workerStartSettleBudget, Rule 2). Its purpose is the A/B proof
	// that the wait is a live detector: a small value must make an unstarted
	// daemon fail BY NAME, which is how a bound is shown to still bite.
	workerStartSettleEnv = "GOPI_WORKER_START_SETTLE_SEC"

	// workerStopProgressEvery throttles the "still working" lines so a long
	// wait stays informative without flooding the terminal. (Named here, not
	// borrowed from the merger's face: the two services' cadences are
	// independent facts, and a shared spelling would make one of them change
	// when the other did — Rule 1b/14.)
	workerStopProgressEvery = 30 * time.Second

	// workerSettleWait is how long `--force` waits for the interrupted worker to
	// disappear: long enough for its close-out (writeback + own-worktree
	// release) to run, bounded so an operator who asked for an immediate stop is
	// told rather than waited on.
	workerSettleWait = 30 * time.Second

	// workerRestartSpawnWait bounds the wait for the REPLACEMENT daemon to
	// publish its state file after a successful start — the restart report's
	// version line depends on it, and a daemon that stayed silent must be named
	// rather than assumed. Generous relative to the handshake
	// (daemonHandshakeWait already proved the child is running), because all
	// that is left is the child's first state write.
	workerRestartSpawnWait = 10 * time.Second

	// workerStateMaxNote bounds the free-text activity note stored in the state
	// file — a status line, not a log sink.
	workerStateMaxNote = 400

	// workerLockRefusal is the single-instance refusal text, named once (Rule
	// 1b: rendered to the operator and asserted by the e2e). %s = lock path,
	// %s = the holder's description (pid + liveness).
	workerLockRefusal = "another worker daemon is already running (lock %s held by %s) — refusing a second resident worker: run `gopi worker --status`, or `gopi worker --restart` to replace it (a FOREGROUND `gopi worker` is unaffected and needs no lock)"

	// workerLockFilename is the single-instance lock file (flock
	// LOCK_EX|LOCK_NB) the DAEMON face takes under <gopi data root>/. It is
	// deliberately NOT serve.lock: the worker and the merger are different
	// services, and one running must never refuse the other (they guard
	// different things — the claim queue vs the merge queue).
	workerLockFilename = "worker.lock"

	// workerGracefulSignalName is the operator-facing spelling of the graceful
	// stop signal, rendered in the --stop message and the help text.
	workerGracefulSignalName = "SIGTERM"

	// workerDrainVerb is the ONE word every drain report is built from ("the
	// worker is draining / already draining"). It is a CONSTANT because the
	// repeated-request contract is asserted on it: `--stop` and `--restart`
	// both have to say the same thing when the worker is already leaving, and
	// a second spelling would let the two answers drift (Rule 1b).
	workerDrainVerb = "draining"

	// workerDrainUnknownAge is rendered when a drain is in progress but its
	// request file carries no readable age (removed by a peer, or a drain
	// observed before the request landed). Honesty means naming the unknown
	// rather than printing a made-up duration.
	workerDrainUnknownAge = "age unknown"
)

// --- sender provenance on the durable stop request -------------------------
//
// THE DEFECT THIS CLOSES (epic: "terminated received" is unattributable).
// Every investigation of "who killed the workers?" starts from the worker's own
// termination line, and that line named neither the sender, nor the signal, nor
// the task that was running, nor the wallclock. Measured on zpeer's
// ~/.gopi/logs/worker.log (2026-09-16, 31 termination lines): 31/31 read
// `terminated received`, 0/31 named a sender, only 3/31 could be resolved to a
// task id — and the SAME string was printed for an operator's graceful `--stop`
// and for an unsourced external cohort kill, so an operator reading 31
// "draining" lines concluded "routine shutdowns" while at least three of them
// were an external kill of an unrelated cohort.
//
// WHY THE SENDER IS KNOWABLE HERE AT ALL. Linux hands a signal handler NO
// sender identity (there is no SA_SIGINFO sender pid in the Go stdlib), so a
// victim of an outside `kill -TERM` can never name its sender from inside
// itself — that fact is the whole premise of internal/signalrec. The operator's
// `--stop` is different: THAT process writes this request file before it
// signals, so the file can carry the sender's own pid and argv — provenance the
// victim could not otherwise receive. The file body is therefore a
// newline-delimited `key=value` header (pid= / argv= / signal= / ts=) followed
// by the addressed pid's line.
//
// COMPATIBILITY, DELIBERATELY PRESERVED IN BOTH DIRECTIONS. Every in-tree
// reader keys on the FILE NAME, not the body: tools.ControlPidFromName (name
// only) and tools.LiveStopRequestPIDs (name + liveness) are untouched. A
// pre-provenance worker that receives a new body never parses it — it STATs the
// file at its claim boundary — so the stop still lands; it just cannot report
// the sender. A provenance-aware worker that receives an old body (a
// pre-provenance sender, or tools.WriteControlText from elsewhere) parses no
// header and must render the termination as UNSOURCED rather than invent an
// identity (Rule 15).
const (
	// workerStopFieldSep separates a header key from its value. The body is
	// line-oriented, so a value may contain anything but a newline (an argv with
	// spaces is therefore safe, and is written verbatim).
	workerStopFieldSep = "="

	// workerAckPrefix names the REVERSE channel's file: "<prefix><pid>" is the
	// WORKER's receipt for a stop request it has CONSUMED. It is the mirror of
	// tools.StopRequestPrefix — the request travels operator→worker, this
	// travels worker→operator — and it lives in the same control directory, so
	// one listing answers both "was the stop asked?" and "was it answered?".
	//
	// WHY IT IS NOT A SIGNAL (epic worker-stop-ack, measured 2026-10-14): the
	// sender used to blind-wait the whole --wait bound (10m foreground / 25m
	// daemon) because a signal can only say "the receiver has no answer for
	// you" — Linux hands the receiver no sender identity and gives it no way
	// back. A FILE is bidirectional without either requirement: it needs no
	// shared binary (so a (deleted) or different-inode worker still answers)
	// and it survives the process that wrote it, so the sender can poll it
	// AFTER the worker is gone.
	workerAckPrefix = "ACK_"

	// workerAckSep is the key/value separator of the receipt body. It is
	// deliberately the SAME spelling the request body uses
	// (workerStopFieldSep): one wire vocabulary for both directions, so a
	// reader that already parses one parses the other (Rule 13/14).
	workerAckSep = workerStopFieldSep

	// workerAckField* are the receipt's keys. req_id/req_ts name WHICH request
	// is being answered (the sender's own pid and the instant it wrote the
	// request), acked_at is when the worker consumed it, and acked_by is the
	// worker's pid — so a receipt found on disk is self-describing and cannot
	// be mistaken for an answer to a later request from the same sender.
	workerAckFieldReqID   = "req_id"
	workerAckFieldReqTS   = "req_ts"
	workerAckFieldAckedAt = "acked_at"
	workerAckFieldAckedBy = "acked_by"
	workerAckFieldMode    = "mode"
	workerAckFieldRound   = "round"
	workerAckFieldExit    = "exit"

	// workerAckModeDrain / workerAckModeForce name WHICH request shape the
	// receipt answers. A receipt never invents the other spelling: the sender
	// asked for a drain or for a force, and the receipt says which one it read.
	workerAckModeDrain = "drain"
	workerAckModeForce = "force"

	// workerAckExitSemantic is the machine-readable half of the receipt: it
	// states what the worker COMMITTED to, in words an operator can act on
	// without reading this file. It is deliberately a sentence about the
	// BOUNDARY (the in-flight round finishes first), because that is the whole
	// shape of a graceful stop and the reason the sender may still be waiting.
	workerAckExitSemantic = "exit at the next round boundary (in-flight round is not interrupted)"

	// workerAckPollDefault is the sender's poll cadence for the receipt. It is
	// faster than the liveness poll (workerControlPoll, 500ms): the receipt is
	// the ANSWER to the operator's question, and the cost of asking is one
	// ReadDir-free stat of a path it already knows.
	workerAckPollDefault = 200 * time.Millisecond

	// workerAckForeignWait is how long the sender KEEPS POLLING after the
	// worker's pid is gone without any receipt: the window in which a receipt
	// written just before exit is still observable. Without it the sender
	// checked a receipt file one poll before the worker wrote it and reported
	// "no receipt" for a worker that answered.
	workerAckForeignWait = 2 * time.Second

	// workerAckArchivedSep / workerAckArchivedSuffix name the ARCHIVED copy of a
	// receipt the sender has read: "<ACK_ name><sep><req_id><suffix>", e.g.
	// "ACK_542818.1234.consumed".
	//
	// WHY THE READER MOVES THE FILE INSTEAD OF DELETING IT. The receipt is the
	// ONLY evidence that a stop was acknowledged — the request file says someone
	// ASKED, and nothing else on the disk says anyone ANSWERED. Deleting it the
	// instant it was printed meant the question "was that stop ever confirmed,
	// and by which worker, at what time?" could only be answered from the
	// operator's scrollback (measured 2026-10-27: an investigation into a stuck
	// worker found one ACK_ file for a pid whose receipt had just been consumed,
	// and the deletion left nothing to compare against). So the verdict is
	// KEPT: the file is renamed out of the channel's namespace (the poll reads
	// only the live name, so a renamed receipt can never answer a later request)
	// and an index line is appended, and the archive is collected by AGE on the
	// resident tick — one rule for the normal path and for a sender that died
	// before it read anything.
	//
	// req_id is IN the archived name because a pid is recycled: two stops of two
	// different workers can share a pid, and one archive must not overwrite the
	// other's evidence.
	workerAckArchivedSep    = "."
	workerAckArchivedSuffix = ".consumed"

	// workerAckArchiveLogName is the append-only index of consumed receipts,
	// beside them in the control directory: one line per archived receipt, so
	// the FACT survives after its file has been reclaimed. Its fields are the
	// receipt's own vocabulary (req_id/acked_by/acked_at), tab-separated,
	// prefixed by the unix second the line was written — a reader with `awk`
	// can answer "which worker acknowledged which request" without this code.
	workerAckArchiveLogName     = "acks-consumed.log"
	workerAckArchiveLogFieldSep = "\t"

	// workerAckOrphanStaleAge is how long an ANSWERED receipt may sit UNREAD in
	// the control directory before the resident sweep archives it anyway. It
	// covers the one path that cannot archive on its own: the sender that wrote
	// the request and then died (Ctrl-C, a killed unit, an OOM) never reads the
	// receipt, so without this the file would stand forever. Archiving it (and
	// not deleting it) is the same rule the reader applies — a receipt outlives
	// the process that reads it, which is the whole reason it is a file.
	workerAckOrphanStaleAge = 7 * 24 * time.Hour

	// workerAckArchiveRetention is how long an ARCHIVED receipt is kept before
	// the resident sweep collects it — measured from the receipt's own acked_at,
	// never from the file's mtime: rename(2) preserves mtime, so an mtime-based
	// retention would collect a just-archived receipt at the same instant it was
	// archived. A stop investigation happens days after the event, so the window
	// is long enough to cover one and short enough that the directory cannot
	// grow without bound (a receipt is ~200 bytes; a busy host stops a handful
	// of workers a day).
	workerAckArchiveRetention = 30 * 24 * time.Hour

	// workerAckImmediateText is what a sender that did NOT wait can honestly
	// say about the receipt window: it made no wait, so the elapsed time is a
	// reading taken at the moment of the ask rather than a duration observed
	// over a poll. Spelled once (Rule 1b) — it appears in both the answered and
	// the unanswered sentence, and the two must not disagree about what was
	// measured.
	workerAckImmediateText = "no wait; read at the moment of the request"

	// workerAckForeignGrace bounds how long the sender waits for a receipt from
	// a worker that NEVER writes one (an older binary). It is deliberately
	// short — the sender has already been told such a worker cannot answer, so
	// this is a courtesy look, not a second wait, and the operator's --wait
	// bound stays untouched behind it.
	workerAckForeignGrace = 5 * time.Second

	// workerStopFieldPID / workerStopFieldArgv / workerStopFieldSignal /
	// workerStopFieldTS are the header keys this package both writes and reads
	// (Rule 2: ONE encoder/decoder pair; a second spelling would silently stop
	// parsing an existing file).
	workerStopFieldPID    = "pid"
	workerStopFieldArgv   = "argv"
	workerStopFieldSignal = "signal"
	workerStopFieldTS     = "ts"

	// workerStopForceSignalName is the escaping spelling the FORCE request
	// records (the merger's convention: SIGUSR1 means "interrupt what you are
	// doing"). It is named rather than inlined so the recorded spelling and the
	// predicate that recognises it cannot drift.
	workerStopForceSignalName = "SIGUSR1"

	// workerStopUnsourcedMarker is what the SENDER field of a termination line
	// reads when no sender record exists — a bare signal from an unknown
	// process, a session teardown, a pre-provenance caller. It is a WORD and not
	// an empty field on purpose: a blank sender would read like "no sender was
	// involved", which is the opposite of what is known (Rule 15). It is also
	// the grep target that separates an unsourced kill from a graceful stop.
	workerStopUnsourcedMarker = "UNSOURCED"

	// workerStopNoTaskMarker / workerStopNoSignalMarker are the honesty fills
	// for the two other facts a termination line carries: a worker that was
	// terminated before claiming anything genuinely has no task, and a stop
	// delivered by FILE ALONE genuinely has no signal spelling — an operator
	// must be able to tell that apart from "the signal was not recorded".
	workerStopNoTaskMarker   = "none"
	workerStopNoSignalMarker = "none"

	// workerStopFieldSenderLabel / workerStopFieldSignalLabel /
	// workerStopFieldTaskLabel / workerStopFieldAtLabel are the field labels of
	// a termination line's provenance suffix. Fixed spellings, so a log reader
	// (and the regression test) can address each fact without a regex over the
	// free text around it.
	workerStopFieldSenderLabel = "SENDER"
	workerStopFieldSignalLabel = "SIGNAL"
	workerStopFieldTaskLabel   = "TASK"
	workerStopFieldAtLabel     = "AT"
)

// workerStopSender is the sender identity a durable stop request carries. Every
// field is optional: a request filed by a pre-provenance binary (or by a caller
// that can only write a file) carries none of them, and an absent fact is
// rendered as absent — never invented (Rule 15).
type workerStopSender struct {
	// PID and Argv describe the REQUESTER: the only sender identity a victim
	// can ever hold, because the requester wrote this file itself.
	PID  int
	Argv string
	// Signal is the spelling the requester sent (workerGracefulSignalName, or
	// workerStopForceSignalName for the force spelling), recorded so the victim
	// does not have to infer the sender's intent from which file arrived.
	Signal string
	// TS is the requester's wallclock (RFC3339) — when the request was FILED,
	// which is not when it was consumed: a claim-boundary consumer may be a
	// whole round late, and the two moments are different facts.
	TS string
}

// Sourced reports whether the request carried a SENDER identity (a pid and/or
// an argv). The signal spelling is deliberately NOT part of this test: it is a
// separate fact, rendered as its own field — a record that knew only the signal
// would name no sender, and this predicate is what routes it to the unsourced
// marker rather than to a "pid none argv none" line that looks halfway
// attributed.
func (s workerStopSender) Sourced() bool { return s.PID > 0 || s.Argv != "" }

// Render is the SENDER field of a termination line: the requester's pid and
// argv, or the unsourced marker. It never returns an empty string — a
// termination line with a blank sender field is exactly the unattributable
// artifact this work exists to remove — and it carries NO signal, because the
// signal is already the line's own SIGNAL field (rendering it twice gave a
// reader two places to disagree with each other, observed on the first live
// stop: `signal SIGTERM SIGNAL SIGTERM`).
func (s workerStopSender) Render() string {
	if !s.Sourced() {
		return workerStopUnsourcedMarker
	}
	pid := strconv.Itoa(s.PID)
	if s.PID <= 0 {
		pid = workerStopNoTaskMarker
	}
	argv := s.Argv
	if argv == "" {
		argv = workerStopNoTaskMarker
	}
	// %q keeps an argv with spaces on ONE readable field (and makes an empty
	// argv impossible to confuse with a missing one).
	return fmt.Sprintf("pid %s argv %q", pid, argv)
}

// --- the control-file face -------------------------------------------------

// workerControlAction is what the operator asked for ("" = no control request,
// i.e. an ordinary foreground run or a plain --daemon start).
type workerControlAction string

const (
	workerActionStop    workerControlAction = "stop"
	workerActionRestart workerControlAction = "restart"
	workerActionStart   workerControlAction = "start"
	workerActionForce   workerControlAction = controlModifierActionWord
	// workerActionStatus is the READ-ONLY action: it takes no lock and creates
	// nothing, so it is handled before any of the mutating paths resolve a
	// directory.
	workerActionStatus workerControlAction = "status"
)

// workerConflictExitCode is the exit code the worker's CONFLICT refusals carry
// — the usage class 2, spelled once (Rule 1b/13). It is deliberately NOT
// workerControlExitFailed (1): "you asked for two things and I refuse to guess"
// is a malformed command line, which an operator (or a script) distinguishes
// from "the requested end state was not reached". main.go's
// workerUnexpectedArgExitCode is the same class; this constant exists so the
// face's refusers and its e2e suite reference ONE name instead of importing the
// argv scanner's.
const workerConflictExitCode = workerUnexpectedArgExitCode

// workerControlOptions is one parsed control invocation.
type workerControlOptions struct {
	action workerControlAction
	// actions is the FULL list of distinct actions argv carried, in argv order
	// (D2, 2026-09-14 — the list the refusal names; see workerDedupeActions).
	actions []workerControlAction
	// flags renders actions back into the operator's spellings, in the same
	// order — what the conflict message names, so the operator reads their own
	// words rather than a canonicalized set (Rule 1b: the parser consumed the
	// flags and this is the only place the direction is rendered).
	flags []string
	// read / act are the read-only flag and the first ACT-CLASS flag in argv
	// order; either may be empty. They decide WHICH conflict the argv carries
	// (controlConflictMessage), not which flags it lists.
	read  string
	act   string
	force bool
	wait  string
}

// workerControlRequested reports whether argv asks for a control action. The
// caller (isWorkerSubcommand) uses it to route BEFORE the loop's own argv
// validation, whose accepted-flag list deliberately does not carry these action
// flags — they are not daemon-loop flags.
//
// It ALSO opens the gate for the READ-ONLY status spellings, which the action
// parser deliberately does not carry (workerStatusRequested is the other half of
// that one route). The alias must open it: `workerControlParse` returns false
// for a bare `--status-verbose` — it holds no action — so the word fell through
// to the loop's scanner and came back as
// "unexpected argument \"--status-verbose\" — refusing to start the worker
// loop" (exit workerUnexpectedArgExitCode). The residue block PRINTS that very
// spelling as its "rerun with …" remedy (workerStatusResidueMoreFmt), so the
// advice the report handed the operator was the one command the binary refused
// — a dead end with an exit code, and the operator's only way to see the file
// list. Routing the read on BOTH spellings is what makes the printed hint
// executable; the conflict checks downstream are unchanged, so a read beside an
// act is still refused.
func workerControlRequested(args []string) bool {
	if _, ok := workerControlParse(args); ok {
		return true
	}
	if workerStatusRequested(args) {
		return true
	}
	// A TARGET SELECTOR ALSO ROUTES HERE — even with no action beside it. This
	// is the routing half of the epic's defect: `--branch` is the RESUME family's
	// ground selector (cli_worker_resume.go), so without this test the word fell
	// through to the resume arm — and a `--stop --branch B` invocation was read
	// as "resume the ground B", with the stop silently discarded. Routing it here
	// makes the control face the OWNER of the word and lets it answer: with an
	// action, resolve and act; without one, refuse by name
	// (workerControlSelectionGate) instead of starting a resume.
	//
	// `--pid` has no such rival today, and is covered by the same test on purpose:
	// one rule for the selector family, so adding a third selector cannot
	// reintroduce the hole by being forgotten here.
	//
	// `--resume` IS THE ONE EXCEPTION, and it is a real one: `gopi worker --resume
	// --branch B` is the RESUME mode's ordinary spelling, where the word names the
	// GROUND that mode starts in — a different question from "which worker do I
	// address". Claiming it here would take that invocation away from the face
	// that owns it (measured: doing exactly that turned a resume into "names a
	// TARGET but no ACTION"). So the resume mode keeps the word, and a control
	// action beside `--resume` is refused by the conflict reporter instead.
	//
	// `--pid` IS still claimed beside `--resume`: no face but this one reads it.
	for _, a := range args {
		name, _ := workerControlSelectorNameValue(a)
		if name == workerControlPIDName {
			return true
		}
		if name == workerControlBranchName && !workerResumeModeRequested(args) {
			return true
		}
	}
	return false
}

// workerResumeModeRequested reports whether the argv asks for the RESUME mode —
// the one reader of `--branch` this face must yield to. It is a READ of the
// resume family's own vocabulary (workerFlagResume), so the two faces cannot
// drift about which word it is.
func workerResumeModeRequested(args []string) bool {
	for _, a := range args {
		if a == workerFlagResume {
			return true
		}
	}
	return false
}

// workerControlParse reads the control flags out of the worker's argv. The
// second return value is false when NEITHER an action flag nor --force is
// present (the ordinary foreground/daemon invocation). Conflicting actions are
// left for the caller to refuse — never a silent precedence rule (Rule 15: an
// operator who typed two actions must be told which one ran).
func workerControlParse(args []string) (workerControlOptions, bool) {
	var o workerControlOptions
	actions := []workerControlAction{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == workerControlStop:
			actions = append(actions, workerActionStop)
		case a == workerControlRestart:
			actions = append(actions, workerActionRestart)
		case a == workerControlStart:
			actions = append(actions, workerActionStart)
		case a == workerControlStatus || a == workerControlList:
			// ONE action, TWO spellings: `--status` (the flag) and `list` (the
			// positional an operator reaches for). They are deliberately not
			// two actions — a second action would make `list --status` a
			// multi-act conflict for what is one read.
			actions = append(actions, workerActionStatus)
		case a == workerControlForce:
			// --force is BOTH a modifier of --stop/--restart and an action of
			// its own ("stop now, interrupt the round"): the operator who types
			// only --force clearly wants the interruption, and refusing it for a
			// missing --stop would be pedantry with a bad failure mode.
			o.force = true
			actions = append(actions, workerActionForce)
		case a == workerControlWait:
			if i+1 < len(args) {
				o.wait = args[i+1]
				i++
			}
		case strings.HasPrefix(a, workerControlWait+workerFlagValueSep):
			o.wait = strings.TrimPrefix(a, workerControlWait+workerFlagValueSep)
		}
	}
	o.action, o.actions = workerDedupeActions(actions)
	// The two views the shared conflict reporter reads are taken from the SAME
	// parse: the ordered distinct ACTION flags it names, and the read/acts role
	// breakdown that decides which conflict argv carries. A word may be both
	// (--daemon acts, and it consumes no action slot), so --daemon is reported
	// as an act beside an action flag rather than in place of it — the operator
	// sees every word they typed, in the order they typed it (o.actions).
	read, act := "", ""
	for _, a := range args {
		r := workerControlRoleOf(a)
		switch {
		case r == controlActionReadOnly && read == "":
			read = a
		case r == controlActionActs && act == "":
			act = a
		}
	}
	o.read, o.act = read, act
	o.flags = workerControlConflictFlags(o.actions)
	return o, o.action != ""
}

// workerDedupeActions collapses the action list into ONE effective action,
// folding the bare --force spelling into a stop request, AND returns the FULL
// list of DISTINCT actions in argv order (D2, 2026-09-14).
//
// The two halves answer two different questions, which is why both must be
// returned rather than one derived from the other:
//
//   - effective = "which action may this invocation run" — the caller acts on
//     it, and >1 distinct action must be REFUSED before it is ever used (this
//     function never picks silently: the first entry is the argv-order
//     spelling of the collision, never a precedence rule);
//   - distinct  = "what did the operator actually ask for" — the refusal
//     message names EVERY word (see controlConflictMessage), so a `--stop
//     --restart --start` argv has to report three flags, not one.
//
// Returning only the effective action (the previous shape) is what made >1
// distinct action unreportable: the caller had to re-derive the list from argv
// with a SECOND, slightly different rule — exactly the drift that produced
// `gopi worker --status --stop --restart` answering a READ+ACT refusal while
// `--stop --restart` answered the multi-act one.
func workerDedupeActions(actions []workerControlAction) (workerControlAction, []workerControlAction) {
	seen := map[workerControlAction]bool{}
	distinct := []workerControlAction{}
	for _, a := range actions {
		if seen[a] {
			continue
		}
		seen[a] = true
		distinct = append(distinct, a)
	}
	if len(distinct) == 0 {
		return "", nil
	}
	if len(distinct) == 1 && distinct[0] == workerActionForce {
		// "--force" with no explicit action means "stop, and interrupt". The
		// single-entry list still travels: it names the word the operator typed.
		return workerActionStop, distinct
	}
	// More than one distinct action: the first is only the EFFECTIVE spelling
	// (the caller refuses the argv before acting); the full list travels beside
	// it so the refusal can name every flag.
	return distinct[0], distinct
}

// workerControlConflicts lists the raw distinct ACTION flags in argv, in argv
// order (>1 = refuse), folding only duplicates.
//
// It is the argv-level view (it sees flags, not a parse), kept for callers that
// have no parsed invocation and for the tests that pin the conflict predicate
// itself. It deliberately does NOT decide anything: the refusal message is
// built from the PARSER's report (workerControlParse's actions), because the
// parser is what decides which words are actions at all — a second vocabulary
// here is how a flag goes missing from a refusal message.
//
// --daemon is absent for the same reason it is absent from the merger's list:
// it ACTS (by role) but takes no ACTION slot, so it never makes an argv
// ambiguous by itself; the read+act rule reports it when a --status is present
// (controlActionRoleOf is the one classifier both paths share).
func workerControlConflicts(args []string) []string {
	set := map[string]bool{}
	for _, a := range args {
		switch a {
		case workerControlStop, workerControlRestart, workerControlStart, workerControlStatus:
			set[a] = true
		}
	}
	out := make([]string, 0, len(set))
	for _, candidate := range []string{workerControlStatus, workerControlStop, workerControlRestart, workerControlStart} {
		if set[candidate] {
			out = append(out, candidate)
		}
	}
	return out
}

// workerHelpRequested reports whether argv carries a usage-query word. It is
// the ONE reader of the -h/--help vocabulary on this face (the words themselves
// are main.go's constants), so the main -h scan, the control face's own scan
// and the `list -h` pre-check cannot drift about what a usage query is.
func workerHelpRequested(args []string) bool {
	for _, a := range args {
		if a == workerFlagHelp || a == workerFlagHelpLong {
			return true
		}
	}
	return false
}

// workerVerbWordNamesStatus reports whether the FIRST word of the worker verb's
// argv is the POSITIONAL read-only verb word (workerControlList) — i.e. whether
// the rest of the argv belongs to that word rather than to the loop's grammar.
// It is what makes `gopi worker list -h` a usage query instead of a positional
// task id, without weakening the rule that a word only counts where a verb word
// stands (never anywhere in the argv).
func workerVerbWordNamesStatus(args []string) bool {
	return len(args) > 0 && args[0] == workerControlList
}

// workerStatusRequested reports whether argv asks for the READ-ONLY status
// report (which takes no lock and creates nothing, so it must be routed before
// any path that resolves/creates the state directory). The verbose spelling is
// the SAME read at a longer altitude, not a second action: it only appends the
// full path of every residue row.
//
// workerControlList is the POSITIONAL spelling of the same read (the alias this
// face publishes exactly so an operator can find the report at all). Routing it
// here is load-bearing in a way the flag spellings' routing is not: a bare
// positional that reaches the worker loop's scanner is read as an EXPLICIT TASK
// ID, so `gopi worker list` would CLAIM AND RUN a row called "list" — a
// read-only question answered by a mutation (Rule 15: silence is the defect).
func workerStatusRequested(args []string) bool {
	status := false
	for _, a := range args {
		switch a {
		case workerControlStatus, workerControlList, workerStatusResidueVerboseFlag:
			status = true
		}
	}
	if status {
		return true
	}
	// `--json` is a MODIFIER of the read, never a read on its own: a bare
	// `gopi worker --json` is not "show me the status in JSON" (the operator
	// never asked for the status), so it keeps falling through to the loop
	// scanner and is refused as an unknown word. Routing it alone here would
	// turn a misspelled worker launch into a successful status report — a
	// silent substitution of one question for another.
	return false
}

// workerStatusJSONRequested reports whether the operator asked for the
// MACHINE-readable form of the same read (worker_status_json.go). It reads the
// same argv the status route is routed on — one vocabulary, no second flag list
// to drift.
func workerStatusJSONRequested(args []string) bool {
	for _, a := range args {
		if a == workerStatusJSONFlag {
			return true
		}
	}
	return false
}

// workerStatusVerboseRequested reports whether the operator asked for the
// UNBOUNDED residue listing. It is read from the same argv the status route is
// routed on, so there is no second flag vocabulary to drift.
func workerStatusVerboseRequested(args []string) bool {
	for _, a := range args {
		if a == workerStatusResidueVerboseFlag {
			return true
		}
	}
	return false
}

// --- read + act in one argv: the shared refusal ---------------------------
//
// WHY THIS EXISTS (devdoc/worker-daemon.md F1, 2026-09-13): `gopi worker
// --status --stop` used to print the status report and exit 0 — the read-only
// route in main.go returns before workerControlConflicts can refuse "pick ONE
// action", so a read PLUS an act was a silent no-op with a SUCCESS exit code
// (Rule 15: the operator's monitor concludes the service was stopped; it was
// not). The defect is the COMBINATION, not either flag: `--status` alone must
// stay a lock-free side-effect-free read, and `--stop` alone must still stop.
//
// The refusal is preferred over an ordering rule: a read plus an act in one
// argv is ambiguous BY CONSTRUCTION (does the operator want the state before
// the stop, after it, or both?), and any precedence chosen silently answers a
// question only the operator can answer. Refusing names the two flags and
// leaves the exit code non-zero, so a caller cannot mistake it for success.
//
// The predicate is deliberately spelled on the ROLE classes (read-only vs
// acting), not on the concrete flags of one component: the worker face and the
// merger face share the worker/merger control flags as wire values but are
// different services, and a future fifth action must be classified rather than
// silently routed. It lives HERE — alongside workerControlConflicts, its
// ACTION-class sibling — because the worker face owns the status-action class
// (mergerControlConflicts deliberately does not list --status).

// THE ONE CONFLICT REPORTER (D2, 2026-09-14)
//
// The two ambiguities an argv can carry — a READ beside an ACT, and MORE THAN
// ONE act — reported by ONE builder and ONE emitter. Before this, two modules
// rendered the refusal by hand (the act-role argv probe that used to sit here and the inline
// Fprintf in mergerControlCommand/workerControlCommand), so the same "pick ONE
// action" sentence was spelled three times and the producer of the flag list
// (the dedupe helper) could disagree with the reporter of it.
//
// The division of labour is deliberate:
//
//   - controlConflictMessage DECIDES the words: it is a pure function of argv,
//     returning "" for an unambiguous argv. Its precedence is read+act first
//     (broader: a read beside acts is BOTH a read+act and a multi-act argv, and
//     "a status query must never act" is the more pointed explanation), then
//     the multi-act conflict.
//   - controlConflictRefuse SPEAKS them: prefix (the face's prog), the face's
//     usage head, the face's exit code. Those three are the ONLY things a face
//     may vary — one wording, the emitter is shared (the merger's own face went
//     with its resident service: epic daemon-machinery-removal, 2026-09-17).
//
// Both the flag list and the act word come FROM THE PARSER (parse.name /
// parse.actions), never from a second scan of argv — that is what makes
// "refuse" and "what would run" two views of ONE decision instead of two rules
// that drift apart.
//
// THE FLAG LISTS ARE COMPLETE BY CONSTRUCTION. workerControlConflicts /
// mergerControlConflicts are a SECOND vocabulary, hand-maintained beside the
// parser's, and they have already fallen behind: at master cec425cc
// `gopi worker --stop --restart` reports both flags, but
// `gopi merger --start --force --stop` reports only "--stop and --start" —
// --force is an action in BOTH parsers (it folds to a stop) and is listed by
// NEITHER, so the operator could not see the word they typed. Deriving the
// list from the parser's own distinct-action list fixes the class, not the
// instance.

// controlConflictParse is the DECODED argv the shared reporter works on: the
// face's program name (the message prefix), its usage head, its exit code, and
// the parser's own report — the ordered distinct flags plus the read/acts role
// breakdown. A face builds this with one call.
type controlConflictParse struct {
	prog  string   // the face's program name as the operator spelled it
	usage string   // the face's usage head, printed after the refusal line
	exit  int      // the face's documented refusal exit code
	flags []string // the ORDERED distinct control flags argv carried
	read  string   // the read-only flag ("" = none)
	act   string   // a flag in the act CLASS ("" = none); may be --daemon
	// distinct is the parser's action list in argv order, INCLUDING the modifier
	// --force, and it answers TWO questions — which is why it is ONE field, the
	// parser's own report, never two derived ones a face could set apart:
	//
	//   - READBACK: the refusal NAMES this list, and naming every word the
	//     operator typed is D2's contract (2026-09-14);
	//   - COLLISION: the multi-act refusal counts controlRequests(distinct) —
	//     the same list with the MODIFIER removed — because `--stop --force` is
	//     ONE request with a modifier, not two (P0 regression 2026-09-14:
	//     counting the words refused the documented spelling).
	//
	// Both readings are taken INSIDE controlConflictMessage from this one field
	// (Rule 2/13), so no face can hand over a list the decider reads as the
	// wrong one.
	distinct []string
	message  string // the sentence to render ("" when unambiguous)
}

// controlModifierActionWord is the ONE spelling of the MODIFIER action in the
// control-action vocabulary both faces share (Rule 13/14: the merger's and the
// worker's typed action constants BOTH derive from this word, so the collision
// view below can compare a rendered list once instead of per face).
//
// --force is a modifier: it does not ask for a second thing to run, it
// qualifies the thing already asked for (stop/restart, and a bare --force folds
// to a stop). That is why the multi-act refusal counts REQUESTS (see
// controlRequests), never the words the operator typed — counting words
// refused the documented `merger --stop --force` spelling, the P0 of
// 2026-09-14.
const controlModifierActionWord = "force"

// controlRequests is the COLLISION view of the parser's action list: the same
// actions in argv order with the MODIFIER removed, i.e. what the operator asked
// to RUN. 0 or 1 entry = unambiguous (the caller may act), >1 = the multi-act
// refusal.
//
// It is a derived view, not a second input: controlConflictMessage takes it
// from the SAME `distinct` it reads back (Rule 2), so the two readings of one
// argv cannot disagree. The distinction they preserve is real and neither
// implies the other (Rule 13): the readback must name every word the operator
// typed (D2, 2026-09-14: `--start --force --stop` must read "start and force
// and stop"), while the collision must count only what runs (a modifier beside
// its own action is not a collision).
func controlRequests(actions []string) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		if a == controlModifierActionWord {
			continue
		}
		out = append(out, a)
	}
	return out
}

// controlConflictMessage is the ONE decision of which conflict an argv carries:
// "" when it is unambiguous, else the refusal sentence with EVERY conflicting
// flag named.
func controlConflictMessage(p controlConflictParse) string {
	// READ + ACT first: it is the more pointed explanation (a monitor asking
	// for state must not be handed an action), and it is the only branch needed
	// for a read beside several acts.
	if p.read != "" && p.act != "" {
		return fmt.Sprintf("%s and %s cannot be combined — pick ONE action (a status query must never act, and an action must never be mistaken for a successful status)", p.read, p.act)
	}
	if len(controlRequests(p.distinct)) > 1 {
		// The DECISION is the requests count (what runs); the SENTENCE speaks
		// the readback (what was typed) — so a modifier the operator typed is
		// never dropped from the explanation that tells them to remove
		// something (`--start --force --stop` reads "start and force and
		// stop"). Both readings come from THIS parse (one input, two
		// questions), which is why they cannot disagree.
		return fmt.Sprintf("%s — pick ONE action", strings.Join(p.distinct, " and "))
	}
	return ""
}

func controlConflictRefuse(p controlConflictParse) bool {
	// The conflict is decided HERE, from the parse the caller handed over, so
	// there is exactly ONE wording and ONE exit path for every refuser on every
	// face (D2, 2026-09-14).
	msg := controlConflictMessage(p)
	if msg == "" {
		return false
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n%s", p.prog, msg, p.usage)
	os.Exit(p.exit)
	return true
}

// workerControlConflictFlags renders a []workerControlAction into the operator's
// spellings — the ONE action→flag rendering. It used to be the worker half of a
// pair (the merger had a []mergerControlAction overload); that overload went with
// the merger's control face (epic daemon-machinery-removal, 2026-09-17), and this
// is now the single renderer, so there is no second vocabulary left to drift
// from (Rule 13).
func workerControlConflictFlags(actions []workerControlAction) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		out = append(out, string(a))
	}
	return out
}

// controlActionRole classifies one control flag by what it does to the
// resident service. The zero value is "not a control flag at all".
type controlActionRole int

const (
	controlActionNone     controlActionRole = iota // not a control flag (a loop flag, a value, …)
	controlActionReadOnly                          // reports; takes no lock and mutates nothing
	controlActionActs                              // stops / restarts / starts: mutates the service
)

// controlActionRoleOf classifies one argv word. `--status` is the ONLY
// read-only control flag on either face; every other control verb is an act.
// `--daemon` (daemonFlagLong, shared by both faces) is an act too, and it is
// classified HERE rather than at its route so that a route cannot forget it:
// measured 2026-09-13, `gopi merger --daemon --status` DETACHED a resident
// merger and exited 0 while the operator asked to look — the flag is not a
// control verb of the --stop family, but it is exactly as much of an act
// (controlDetachDaemonRefusal). A word this function does not know is
// controlActionNone — it is somebody else's business (the loop scanners, the
// value flags), never this refusal's.
func controlActionRoleOf(word string) controlActionRole {
	switch word {
	case workerControlStatus:
		return controlActionReadOnly
	case workerControlStop, workerControlRestart, workerControlStart, workerControlForce:
		return controlActionActs
	case daemonFlagLong:
		// --daemon=... is a spelling of the same act (the scanner matches the
		// bare flag; the "=" form must not slip past the classification).
		return controlActionActs
	default:
		if strings.HasPrefix(word, daemonFlagLong+workerFlagValueSep) {
			return controlActionActs
		}
		return controlActionNone
	}
}

// workerControlRoleOf is the WORKER face's action role for one argv word: the
// shared classifier (controlActionRoleOf) PLUS this face's own read-only
// aliases. It is the ONE place the aliases are declared as read-only, and both
// the parse (which builds the read/act views the refusal reads) and any caller
// asking "is this word a read?" go through it, so a second alias cannot be
// added to one gate and forgotten at the other (Rule 2/13).
//
// WHY THE ALIASES ARE NOT IN controlActionRoleOf: that function is shared with
// the merger face, which does not own them (mergerControlConflicts deliberately
// does not list --status). Classifying them at the FACE keeps the shared
// classifier about the vocabulary both faces really share.
//
// THE MEASURED COST OF GETTING THIS WRONG (2026-09-14, shipped binary): an
// unclassified read-only alias made the READ+ACT refusal blind —
// `gopi worker --status-verbose --stop` printed the status report and exited 0
// with the --stop silently DROPPED: the F1 defect, where a monitor concludes
// the service was stopped and it was not.
func workerControlRoleOf(word string) controlActionRole {
	if r := controlActionRoleOf(word); r != controlActionNone {
		return r
	}
	if word == workerStatusResidueVerboseFlag || word == workerControlList || word == workerStatusJSONFlag {
		return controlActionReadOnly
	}
	return controlActionNone
}

// workerControlConflictParse is the WORKER face's one call into the shared
// reporter: it carries the face's prog/usage/exit code and the parse's OWN
// report (ordered distinct action flags in workerControlAction spelling + the
// read/acts role breakdown). Every worker refuser — the argv scanner in main.go,
// the control command and the --daemon probe — goes through here, so the face
// can no longer drift from the other two by spelling the sentence itself.
// workerControlConflictParse builds the shared conflict report for the worker
// control face.
//
// THE FLAG LIST IS THE OPERATOR'S OWN WORDS, PLUS the resume spelling when it is
// present: `--stop --branch B` was silently read as `--resume --branch B` (the
// branch selector's only reader WAS the resume family — the epic's defect), so a
// control invocation that also carries the resume mode is a CONTRADICTION to
// refuse by name rather than an ordering to decide. Appending it here — in the
// builder both call sites share — keeps the listing in ONE place (Rule 2).
func workerControlConflictParse(args []string, opts []workerControlAction, read, act string, flags []string) controlConflictParse {
	flags = append(append([]string{}, flags...), workerResumeConflictFlags(args)...)
	return controlConflictParse{
		prog:     workerProgTopLevel,
		usage:    workerUsageHead,
		exit:     workerConflictExitCode,
		flags:    flags,
		read:     read,
		act:      act,
		distinct: workerControlConflictFlags(opts),
	}
}

// workerResumeConflictFlags reports the resume-mode words an argv carries. It is
// a READ of the vocabulary, not a second parser: the spelling comes from the
// resume family's own constant (Rule 13/14), so naming it here cannot drift from
// the face that owns it.
func workerResumeConflictFlags(args []string) []string {
	for _, a := range args {
		if a == workerFlagResume || strings.HasPrefix(a, workerFlagResume+workerFlagValueSep) {
			return []string{workerFlagResume}
		}
	}
	return nil
}

// workerWaitBound resolves the wait bound: the --wait value when given, else
// workerWaitBoundDefault (0 = do not wait). ONE resolver (Rule 2) so the usage
// text and the actual wait cannot disagree.
func workerWaitBound(raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return workerWaitBoundDefault, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid %s %q (want %s; 0 = do not wait)", workerControlWait, raw, workerControlWait)
	}
	return d, nil
}

// workerControlCommand runs one control invocation. The process exit code IS
// the answer (see the file header).
func workerControlCommand(args []string) {
	// THE SENDER'S OWN SIGNALS (measured 2026-10-16, task 01a14316-b7cb).
	//
	// This command can wait for up to the operator's own bound (default
	// workerWaitBoundDefault, 10m) and it is the tool an operator runs to stop
	// something ELSE — so it must obey the terminal contract itself. Before
	// this, `gopi worker --stop --pid N` had NO signal.Notify of its own:
	//
	//   - Ctrl-C was decided entirely by the SIGNAL DISPOSITION THIS PROCESS
	//     INHERITED from whoever launched it. With the default disposition
	//     (SIG_DFL, an ordinary interactive shell) the kernel killed the
	//     command outright — which is why this defect hid so well: on the
	//     normal path Ctrl-C "works", it just bypasses every line of this
	//     file. With SIG_IGN inherited (nohup/setsid/a non-interactive
	//     launcher/`&`), Ctrl-C was DISCARDED by the kernel and nothing could
	//     re-enable it — measured: four real Ctrl-C bytes on a pty, no
	//     termination line, the process running to the bound (45s ceiling).
	//   - On the SIG_DFL path the kill also landed BEFORE the request was
	//     delivered, so the operator's Ctrl-C both failed to stop the sender
	//     AND left the stop unrequested (measured: no TERM_<pid> afterwards).
	//
	// Installing the handler here fixes both halves at once, and does it the
	// way panel C of the mechanism probe measures: signal.Notify RESETS an
	// inherited SIG_IGN and installs Go's own handler, so the delivery path
	// stops depending on a disposition this process never chose. The refusal
	// arm is `workerControlClassifySignal`: a stop request is being made, so a
	// Ctrl-C is read as "stop WAITING", never as "stop the WORKER".
	defer installWorkerControlSignals()()
	// ONE conflict check, before anything acts: it refuses a read beside an act
	// AND more than one distinct act, naming EVERY flag argv carried (D2,
	// 2026-09-14). This replaced two checks whose vocabularies had drifted apart
	// (see controlConflictMessage).
	o, _ := workerControlParse(args)
	if controlConflictRefuse(workerControlConflictParse(args, o.actions, o.read, o.act, o.flags)) {
		return // unreachable: the emitter exits
	}
	bound, err := workerWaitBound(o.wait)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", workerProgTopLevel, err)
		os.Exit(workerControlExitFailed)
	}
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", workerProgTopLevel, err)
		os.Exit(workerControlExitFailed)
	}

	// TARGET SELECTION (epic worker-stop-selector, 2026-10-13): `--pid <n>` /
	// `--branch <b>` name WHICH worker this invocation addresses. When one is
	// present it REPLACES the unselected routes entirely — the enumeration
	// fallback and the freshest-daemon probe both answer "any worker?", which is
	// a different question, and answering it under a selector is the defect this
	// arm exists to end (a `--branch` that reached the fallback stopped every
	// foreground worker of the profile, or none, while the operator believed
	// they had named one).
	if sel, serr := workerControlSelectionFrom(args); serr != nil {
		workerControlEmitSelectionRefusal(serr)
	} else if sel.applied {
		workerControlSelected(sel, dir, bound, o)
		return // unreachable: workerControlSelected exits
	}
	// Resolve the target FIRST, for every action: --start on a service that is
	// already running is a refusal (the flock would refuse it anyway; saying so
	// up front names the reason), and stop/restart need the pid.
	live, running, err := workerFreshestLiveState(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", workerProgTopLevel, err)
		os.Exit(workerControlExitFailed)
	}

	switch o.action {
	case workerActionStart:
		if running {
			fmt.Fprintf(os.Stderr, "%s: a worker daemon is already running (pid %d: %s) — nothing to start\n",
				workerProgTopLevel, live.PID, live.Report())
			os.Exit(workerControlExitOK)
		}
		workerControlStartDaemon(args)

	case workerActionRestart:
		oldPID, oldVersion := 0, ""
		if running {
			oldPID, oldVersion = live.PID, live.Version()
			if !workerControlStopOne(live, bound, o.force) {
				os.Exit(workerControlExitFailed)
			}
			// THE RECEIPT ENDS THE WAIT, NOT THE PROCESS (epic worker-stop-ack —
			// the regression this line fixes, measured 2026-10-16). A successful
			// stop now returns as soon as the worker ACKNOWLEDGES, while that
			// worker is still finishing its in-flight round; reading that `true`
			// as "the pid is gone" made this branch below observe the OLD daemon
			// still running and report "already running again … not starting a
			// second" — a `--restart` that started nothing and left the old
			// process in place. The restart's own promise is stop-then-start, so
			// it must observe the EXIT, bounded, and say so when it does not.
			if !workerAwaitGone(live.PID, workerSettleWait) {
				fmt.Fprintf(os.Stderr, "%s: pid %d acknowledged the stop but is still running %s later — `%s %s` will not start a second daemon beside a live one; check `%s %s`\n",
					workerProgTopLevel, live.PID, workerSettleWait, workerProgTopLevel, workerControlRestart,
					workerProgTopLevel, workerControlStatus)
				os.Exit(workerControlExitFailed)
			}
			// The drain is over — but a CONCURRENT restart may have already
			// spawned the replacement while this one waited. Re-resolve before
			// spawning so a repeated `--restart` is an honest no-op instead of a
			// second daemon (Rule 15: the answer must describe reality, and the
			// flock refusal would otherwise be the first the operator hears of
			// it).
			if fresh, nowRunning, rerr := workerFreshestLiveState(dir); rerr == nil && nowRunning {
				fmt.Printf("%s: a worker daemon is already running again (pid %d, binary %s) — the restart end state already holds; not starting a second\n",
					workerProgTopLevel, fresh.PID, fresh.Version())
				workerReportSpawnedDaemon(dir, oldPID, oldVersion)
				os.Exit(workerControlExitOK)
			}
		} else {
			// No daemon: the honest note (which also lists any FOREGROUND
			// workers it found). The restart still starts a daemon below —
			// that is what the verb promises — and it deliberately does NOT
			// signalled the foreground workers: an operator replacing the
			// daemon has not asked for them to leave.
			workerForegroundStopNote(dir)
		}
		workerControlEnsureDaemonOrExit(dir, args, oldPID)
		workerReportSpawnedDaemon(dir, oldPID, oldVersion)

	default: // workerActionStop
		if !running {
			// NO DAEMON — but that is NOT the same claim as "no worker is
			// running" (bug 01a131c8-0500). Say what was measured, and stop
			// what the process table found: the most common shape of this
			// command's target is a bare `gopi worker`, which holds no daemon
			// lock and writes no daemon state file, so the daemon-shaped probe
			// can never see it.
			fg := workerForegroundStopNote(dir)
			if len(fg) == 0 {
				os.Exit(workerControlExitOK)
			}
			if !workerControlStopForeground(fg, bound, o.force) {
				os.Exit(workerControlExitFailed)
			}
		} else if !workerControlStopOne(live, bound, o.force) {
			os.Exit(workerControlExitFailed)
		}
	}
	os.Exit(workerControlExitOK)
}

// workerReportSpawnedDaemon waits (bounded) for the replacement daemon's state
// file and reports WHICH binary it runs, beside the one it replaced. This is
// the restart's VERIFIABLE half: "the new worker runs the current binary" is a
// fact an operator (and the acceptance run) must be able to read, not infer
// from a pid change. A daemon that never publishes (an old binary without a
// state file) is NAMED, never silently omitted.
func workerReportSpawnedDaemon(dir string, oldPID int, oldVersion string) {
	deadline := time.Now().Add(workerRestartSpawnWait)
	for {
		for _, f := range workerLiveStateFiles(dir) {
			if f.PID == oldPID {
				continue
			}
			fmt.Printf("%s: new daemon pid %d, binary %s (replaced pid %d, binary %s)\n",
				workerProgTopLevel, f.PID, f.Version(), oldPID, workerVersionOrUnknown(oldVersion))
			return
		}
		if !time.Now().Before(deadline) {
			fmt.Fprintf(os.Stderr, "%s: the replacement daemon published no state within %s (its log is %s) — check `%s %s`\n",
				workerProgTopLevel, workerRestartSpawnWait, workerLogPathFor(), workerProgTopLevel, workerControlStatus)
			return
		}
		time.Sleep(workerControlPoll) //stylegate:allow-sleep bounded wait for the replacement daemon's first state write
	}
}

// workerVersionOrUnknown renders a version for a report where the value may be
// absent (an old binary's state file carried no version).
func workerVersionOrUnknown(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

// workerNoDaemonNote was removed by bug 01a131c8-0500: it asserted "no worker
// daemon is running … a FOREGROUND `gopi worker` holds no lock and is
// unaffected" — the absence of a daemon STATE FILE (a proxy) rendered as the
// absence of a worker, on a host where the operator's foreground worker was
// alive and had no command-line exit. The honest answer now lives in
// cli_worker_foreground.go's workerForegroundStopNote, which names what was
// MEASURED and lists the foreground pids it found.

// installWorkerControlSignals makes this control invocation obey the terminal
// contract WHILE IT WAITS: Ctrl-C ends the WAIT, not the worker.
//
// The contract, stated so the three arms of the handler cannot drift:
//
//	Ctrl-C #1  stop waiting: the request is already delivered, print what was
//	           delivered, exit workerControlAbortExit. The durable request is
//	           NEVER withdrawn — the worker still leaves at its own round
//	           boundary, which is the whole point of a graceful stop.
//	Ctrl-C #2+ a RE-AFFIRM, not an escalation: nothing to do but leave faster.
//	           Escalation in this system is the operator's explicit spelling
//	           (`--force`, i.e. FORCE_<pid> + SIGUSR1) — see
//	           installWorkerStopSignals' own escalation note. This command
//	           signals no worker on a repeat; the only thing a repeat moves is
//	           how long this process is willing to keep waiting.
//	SIGTERM    the same path: a supervisor's stop must not leave the command
//	           parked either.
//
// SIGINT is taken from the terminal as a raw byte by the tty discipline, so
// reaching this handler does not depend on a disposition this process had no
// say in (see workerControlCommand's note on inherited SIG_IGN).
func installWorkerControlSignals() func() {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, workerControlSignals...)
	// The recorder (signalrec.Install, early in main) catches these too. The
	// claim tells it THIS process owns the graceful path: without it the
	// recorder may re-raise with the default disposition and kill us
	// mid-abort, before the line below states why we are leaving.
	signalrec.ClaimSignalHandlers()
	done := make(chan struct{})
	go func() {
		first := true
		for {
			select {
			case sig := <-ch:
				if first {
					// Record the fact and wake the waiter — NOTHING MORE. The
					// printing and the exit belong to the waiter (one printer,
					// one exit, no goroutine race): the first shape of this
					// printed here and lost the race to the main goroutine's
					// ordinary exit every time (measured: exit 1, no line).
					first = false
					workerControlAbortSignal.Store(signalrec.SignalName(sig))
					workerControlAborted.Store(true)
					workerControlInterruptNote()
					continue
				}
				// A repeat re-affirms: give the abort a moment to publish its
				// line and leave, then leave regardless.
				time.Sleep(workerControlRewaitAfterCtrlC)
				os.Exit(workerControlAbortExit)
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(ch)
			close(done)
		})
	}
}

// workerControlAbortNow is the ONE abort renderer AND this command's only
// non-ordinary exit path. It names the signal,
// the target, the idempotency verdict and the standing request, then exits
// workerControlAbortExit — never 0, because the requested end state was not
// observed (Rule 15: a message that could be read as success is a defect).
//
// The durable request's path is printed rather than a bare claim that it
// stands: the acceptance criterion is that Ctrl-C does NOT withdraw it, and a
// line an operator cannot check is not evidence of that.
func workerControlAbortNow() {
	// ORDER IS LOAD-BEARING, and the first attempt got it wrong (measured:
	// exit 1 with no abort line — the main goroutine's ordinary "still busy"
	// exit overtook this goroutine while it was still settling). So:
	//
	//   1. decide the message — the recheck first, because it is what makes the
	//      message TRUE (see the recheck constants' note);
	//   2. PRINT it, immediately: the operator's own Ctrl-C must never be
	//      answered by a silent exit, whatever happens next;
	//   3. only then give the provenance record its bounded moment on disk;
	//   4. leave with the abort code.
	pid := workerControlAbortPID()
	sigName := workerControlAbortSignalName()
	gone := false
	if pid > 0 {
		gone = true
		for i := 0; i < workerControlAbortRecheckTries && gone; i++ {
			time.Sleep(workerControlAbortRecheckEvery)
			gone = !tools.PidAlive(pid)
		}
	}
	if gone {
		fmt.Fprintf(os.Stderr, "\n%s: %s — but pid %d is already gone: the stop finished before the interrupt landed\n",
			workerProgTopLevel, sigName, pid)
		fmt.Fprintf(os.Stderr, "%s: stop request file still standing: %s (exit %d)\n",
			workerProgTopLevel, workerControlRequestPathOrUnknown(pid), workerControlAbortExit)
	} else {
		fmt.Fprintf(os.Stderr, "\n%s: %s — the stop request has been delivered; this command is no longer waiting for pid %s to exit (exit %d)\n",
			workerProgTopLevel, sigName, workerControlTargetNote(), workerControlAbortExit)
		if note := workerControlStandingRequestNote(); note != "" {
			fmt.Fprintf(os.Stderr, "%s: %s\n", workerProgTopLevel, note)
		}
		fmt.Fprintf(os.Stderr, "%s: the request is NOT withdrawn — the worker still leaves at its own round boundary. Pressing Ctrl-C again does not escalate it; to interrupt the round use `%s %s %s`\n",
			workerProgTopLevel, workerProgTopLevel, workerControlStop, workerControlForce)
	}
	// The record is filed by another goroutine and os.Exit ends the process
	// outright; this bounded wait is what keeps the operator's own act from
	// destroying the evidence of it (see the constant's note).
	signalrec.AwaitRecordFiled(workerControlAbortRecordSettle)
	os.Exit(workerControlAbortExit)
}

// workerControlRequestPathOrUnknown renders the durable request's path, or says
// why it cannot be named — never an empty claim.
func workerControlRequestPathOrUnknown(pid int) string {
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		return "unknown (" + err.Error() + ")"
	}
	return tools.ControlRequestPath(dir, workerStopRequestPrefix, pid)
}

// workerControlAbortObserver renders the standing-request line once by asking
// the live facts, so the abort states what it observed instead of asserting
// what it hoped.
func workerControlStandingRequestNote() string {
	pid := workerControlAbortPID()
	if pid <= 0 {
		return ""
	}
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		return fmt.Sprintf("could not read the request directory (%v) — the request file's state is UNKNOWN here", err)
	}
	path := tools.ControlRequestPath(dir, workerStopRequestPrefix, pid)
	if _, serr := os.Stat(path); serr != nil {
		return fmt.Sprintf("no stop request file is standing at %s (%v) — the request may have landed after this command was interrupted", path, serr)
	}
	return fmt.Sprintf("stop request file still standing: %s", path)
}

// workerControlTargetNote names the pid the abort was waiting on, or says that
// the wait ended before a target was resolved.
func workerControlTargetNote() string {
	if pid := workerControlAbortPID(); pid > 0 {
		return strconv.Itoa(pid)
	}
	return "the worker"
}

// workerControlAbortTarget is the pid this invocation is waiting on, published by
// the arm that resolves the target so the Ctrl-C path can name it without
// re-deriving the selection (Rule 14: one writer of that fact). 0 means the
// wait ended before any target was resolved.
var workerControlAbortTarget atomic.Int64

// workerControlAbortPID reads the published target pid.
func workerControlAbortPID() int { return int(workerControlAbortTarget.Load()) }

// workerControlAborted is the LATCH that says "the operator ended this wait".
//
// It is set by the handler and cleared by NOTHING — deliberately. The first
// shape of this used the wake channel itself as the fact, and that is a defect
// measured on the fixed binary: the WAIT consumes the channel, so by the time
// the caller asked "did a signal arrive?" the answer was already false, the
// ordinary "still busy" exit ran first, and the process left with 1 while the
// abort line had not been printed. One latch, written by the handler, read by
// everyone else, consumed by no one.
var workerControlAborted atomic.Bool

// workerControlSignalPending reports whether a signal arrived while this
// invocation was waiting — the fact a wait uses to suppress a verdict that a
// Ctrl-C made meaningless.
func workerControlSignalPending() bool { return workerControlAborted.Load() }

// workerControlAbortSignal carries the NAME of the signal that ended the wait
// (the handler's only other hand-off to the renderer, which prints it).
var workerControlAbortSignal atomic.Value

// workerControlAbortSignalName reads it, never inventing a value it was not
// given: an operator reading "signal" on a line that never learned which one it
// was is owed the honesty, not a guess.
func workerControlAbortSignalName() string {
	if v, ok := workerControlAbortSignal.Load().(string); ok && v != "" {
		return v
	}
	return "signal"
}

// A bounded park used to live here (workerControlAwaitAbort) as a fallback for
// "a signal is pending but the handler has not exited yet". Removing it is the
// fix, not a tidy-up: it existed only because the HANDLER was a second exit
// path racing the waiter. Now the waiter IS the exit path — it calls
// workerControlAbortNow itself — so there is no race to paper over and no
// second exit to keep. workerControlAbortNow's own display settle
// (signalrec.AwaitRecordFiled) is what protects the provenance record.

// workerControlStopOne asks ONE resident worker to leave and waits for it.
// Returns true when the process is gone (the requested end state), false when
// it is still running (the caller exits 1, with the escape hatch printed).
//
// IDEMPOTENCY (epic worker-graceful-drain): the request is a DURABLE FACT, so
// the command asks "is a request already standing for this pid?" before it
// writes or signals anything. If one is, this invocation is a no-op that
// REPORTS THE TRUE STATE (draining / drain requested, with the request's age)
// and then waits for the same end state — no second signal, no rewritten file,
// and therefore no chance of a duplicate request being read as an escalation.
// workerAwaitGone waits, bounded, for pid to LEAVE the process table. It is the
// second half of the stop contract and it exists because the two halves are no
// longer the same event (epic worker-stop-ack): `workerWaitForExit` returns as
// soon as the request is ACKNOWLEDGED, while the worker is still finishing its
// in-flight round — so a caller whose own promise is "the process is gone"
// (a restart's stop-then-start) must observe the exit itself instead of reading
// the acknowledgement as one.
//
// It answers `true` immediately when the pid is already gone, so a caller may
// use it unconditionally.
func workerAwaitGone(pid int, bound time.Duration) bool {
	if !tools.PidAlive(pid) {
		return true
	}
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		time.Sleep(workerAckPollDefault) //stylegate:allow-sleep bounded wait for an acknowledged worker's own exit
		if !tools.PidAlive(pid) {
			return true
		}
	}
	return !tools.PidAlive(pid)
}

// workerControlStopOne stops one daemon-shaped target: it ASKS (request file,
// plus a signal only when the binary's signal contract is verifiable), WAITS for
// the answer, and reports whether the requested end state holds.
//
// ITS `true` MEANS "THE REQUEST IS ANSWERED OR THE PROCESS IS GONE" — never
// "the process is gone". The distinction is load-bearing since the receipt
// channel landed: a worker answers and THEN finishes its round, so a caller that
// needs the exit (a restart) must ask workerAwaitGone itself.
func workerControlStopOne(st workerStateFile, bound time.Duration, force bool) bool {
	pid := st.PID
	workerControlAbortTarget.Store(int64(pid))
	dir := filepath.Dir(st.Path)
	if force {
		return workerControlForceOne(st)
	}
	// TWO ARMS, ONE ORDER (epic worker-graceful-drain, legacy-binary finding).
	//
	// The durable request file is the PREFERRED delivery: it never enters the
	// victim through its signal handler. On a worker that consumes it the
	// in-flight round finishes — the request is read at the CLAIM BOUNDARY, and
	// (since this epic) a signal alone no longer forwards into the run child.
	//
	// A worker running a DIFFERENT binary has UNKNOWN signal semantics: the
	// pre-drain generation FORWARDS the stop into the in-flight RUN CHILD, so a
	// SIGTERM from here KILLS the very round this command promised to protect
	// (measured 2026-09-13: `--restart` against a master-tip worker left its
	// task row `failed: signal: terminated`, the child killed mid-run). For such
	// a worker the request FILE ALONE is both sufficient and safe: it is the
	// merge-runtime `--stop` semantics (a durable request a daemon consumes, no
	// signal), and the pre-drain worker consumes it at its next loop tick
	// (measured). Only a worker whose recorded version EQUALS this binary's gets
	// the signal — see workerForeignDrainBinary.
	//
	// Either way a repeat is a reported no-op: the standing-request check comes
	// FIRST, so nothing is re-written and nothing is re-signalled.
	already := workerDrainRequestedFor(dir, pid)
	foreign := workerForeignDrainBinary(st)
	switch {
	case already:
		fmt.Printf("%s: pid %d is already %s (%s) — nothing to do\n",
			workerProgTopLevel, pid, workerDrainVerb, workerDrainAgeNote(dir, pid, st))
	case foreign:
		fmt.Printf("%s: asking pid %d to finish the in-flight round and exit (stop request file, bound %s)\n",
			workerProgTopLevel, pid, bound)
		fmt.Printf("%s: pid %d runs a DIFFERENT binary than this command (worker %s, this %s) — requesting by FILE ONLY, no signal: a worker whose signal contract we cannot verify may forward the stop into the in-flight run child and kill the round this request exists to protect\n",
			workerProgTopLevel, pid, workerVersionOrUnknown(st.Version()), workerProgVersion())
	default:
		fmt.Printf("%s: asking pid %d to finish the in-flight round and exit (%s, bound %s)\n",
			workerProgTopLevel, pid, workerGracefulSignalName, bound)
	}
	fmt.Printf("%s: current state — %s\n", workerProgTopLevel, st.Report())
	if !already {
		if err := workerRequestStopFile(dir, pid); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", workerProgTopLevel, err)
		}
		if foreign {
			fmt.Printf("%s: nothing is signalled — the worker reads the request at its next loop tick, so the in-flight round finishes first\n",
				workerProgTopLevel)
		} else {
			workerSignalPID(pid, syscall.SIGTERM, "operator ran "+workerProgTopLevel+" "+workerControlStop)
		}
	}
	if bound == 0 {
		fmt.Printf("%s: request %s — not waiting (%s 0); the worker exits after the in-flight round\n",
			workerProgTopLevel, alreadyOrDelivered(already), workerControlWait)
		return false
	}
	// AN EARLIER DELIVERY IS NOT WAITED ON AGAIN (the second-invocation hang).
	// A request that is already standing was written by a sender that has had its
	// one acknowledgement — the worker writes exactly one receipt per request and
	// the sender that reads it archives it — so this invocation returns the state
	// it can read instead of paying the whole --wait bound for a reply the
	// protocol cannot produce twice. The verdict is still honest: an
	// acknowledgement on record (or a pid that is gone) is the end state; a
	// delivery with no acknowledgement anywhere is reported as standing and
	// UNOBSERVED, never as stopped.
	if already {
		prior := readWorkerStopPriorDelivery(dir, pid, false)
		if prior.Receipts > 0 || !tools.PidAlive(pid) {
			fmt.Printf(workerAlreadyDeliveredFmt, workerProgTopLevel, workerFaceDaemon, pid,
				workerStopSenderText(prior), prior.RequestPath, prior.Receipts, workerAckRecordText(prior))
			return true
		}
		fmt.Fprintf(os.Stderr, workerAlreadyDeliveredNoAckFmt, workerProgTopLevel, workerFaceDaemon, pid,
			workerStopSenderText(prior), prior.RequestPath)
		fmt.Fprintf(os.Stderr, workerPriorUnprovenFmt, workerProgTopLevel, pid,
			prior.RequestPath, workerProgTopLevel, workerControlForce, pid)
		return false
	}
	// START-UP SETTLE (Rule 15, measured 2026-10-03): the request above is
	// consumed at a ROUND BOUNDARY, so a daemon still in its start-up prologue
	// cannot honour it yet — and the prologue's length is a measurement of the
	// MACHINE (resident sweeps + the first ledger read), not of the worker's
	// round. Waiting for the worker on the operator's `--wait` bound here made
	// the command report "pid N is STILL running 20s after the graceful stop
	// request" while the worker had never opened its claim loop: the failure
	// named the wrong wait. So: wait (on its OWN bound) until the worker's own
	// record says the loop is open, and if it never opens, say THAT by name.
	if !workerClaimLoopOpened(st, workerStartSettleBudget()) {
		// An interrupt is not a failure to report: workerControlAbortNow owns
		// the message and the exit code, and it is the only exit that may
		// print here.
		if workerControlSignalPending() {
			workerControlAbortNow()
		}
		fmt.Fprintf(os.Stderr,
			"%s: pid %d never opened its claim loop within %s — no round boundary was reached, so the stop request has not been consumable; last state: %s\n",
			workerProgTopLevel, pid, workerStartSettleBudget(), workerRereadState(st).Report())
		return false
	}
	return workerWaitForExit(st, bound, false, newWorkerAckPoll(dir, pid))
}

// workerStartSettleBudget resolves the start-up settle bound (Rule 2: the env
// is read in exactly ONE place). A non-positive or unparsable value falls back
// to the named default rather than becoming a zero bound (Rule 15a: a silent
// zero would turn "wait for the claim loop" into "never wait at all").
func workerStartSettleBudget() time.Duration {
	raw := strings.TrimSpace(os.Getenv(workerStartSettleEnv))
	if raw == "" {
		return workerStartSettleWait
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "%s: %s=%q is not a positive number of seconds — using %s\n",
			workerProgTopLevel, workerStartSettleEnv, raw, workerStartSettleWait)
		return workerStartSettleWait
	}
	return time.Duration(n) * time.Second
}

// workerControlSignalWait is how the interruptible waits below race their poll
// against the operator's Ctrl-C. The signal handler fires it and then, after
// its bounded settle, aborts the process — so a wait that returns false has an
// abort in flight behind it and must not publish an ordinary verdict.
var workerControlSignalWaitCh = make(chan struct{}, 1)

// workerControlInterrupted reports whether a signal arrived while this
// invocation was waiting.
func workerControlInterrupted() <-chan struct{} { return workerControlSignalWaitCh }

// workerControlInterruptNote is the non-blocking publication of a signal to
// the waits, called by the handler itself (one channel, one writer).
func workerControlInterruptNote() {
	select {
	case workerControlSignalWaitCh <- struct{}{}:
	default:
	}
}

// workerControlDrainInterrupt is what a wait calls when it is about to return
// its NORMAL verdict: it clears a signal that landed during the final poll, so
// a stale interrupt cannot make a LATER wait (a --restart's second stop) report
// an abort that belongs to a wait which in fact finished on its own terms.
func workerControlDrainInterrupt() {
	select {
	case <-workerControlSignalWaitCh:
	default:
	}
}

// workerClaimLoopOpened polls the worker's state record until it says the claim
// loop is open, the process is gone (nothing left to wait for — the caller's
// exit check reports THAT), or the bound expires. It never decides whether the
// stop succeeded: that verdict belongs to workerWaitForExit, on the operator's
// own bound.
func workerClaimLoopOpened(st workerStateFile, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for {
		if workerOpenedClaimLoop(st) {
			workerControlDrainInterrupt()
			return true
		}
		if !tools.PidAlive(st.PID) {
			workerControlDrainInterrupt()
			return true // gone before the loop ever opened: the exit check has the verdict
		}
		if !time.Now().Before(deadline) {
			workerControlDrainInterrupt()
			return false
		}
		// INTERRUPTIBLE, and that is the fix rather than a nicety: a bare
		// time.Sleep here is a window in which Ctrl-C cannot be observed at
		// all (measured 2026-10-16, task 01a14316-b7cb). The handler publishes
		// the signal and then aborts the process, so false means an abort is in
		// flight and the caller must not publish an ordinary verdict.
		select {
		case <-time.After(workerStartSettlePoll): // bounded wait for the worker's claim loop to open
		case <-workerControlInterrupted():
			workerControlAbortNow()
		}
	}
}

// workerOpenedClaimLoop reads the CURRENT state record (never the snapshot the
// command took at request time — the loop opens after it) and reports whether
// the daemon has reached a round boundary. An unreadable record is false: "I
// cannot see the evidence" is not evidence (Rule 15).
func workerOpenedClaimLoop(st workerStateFile) bool {
	return workerRereadState(st).State.OpenedClaimLoop()
}

// workerRereadState re-reads pid's state record from disk, falling back to the
// caller's snapshot when the file is gone or unreadable (a stopped daemon
// clears it), so every message quotes the freshest fact available.
func workerRereadState(st workerStateFile) workerStateFile {
	for _, f := range workerLiveStateFiles(filepath.Dir(st.Path)) {
		if f.PID == st.PID {
			return f
		}
	}
	return st
}

// alreadyOrDelivered names what the record holds at a no-wait exit: the
// difference between "this invocation did nothing (a request was standing)" and
// "this invocation delivered the request". An operator reading a log must not
// have to guess which happened.
func alreadyOrDelivered(already bool) string {
	if already {
		return "already standing"
	}
	return "delivered"
}

// workerForeignDrainBinary reports whether the live worker runs a DIFFERENT
// binary than this command (a version mismatch, or no version to compare), and
// therefore whether its stop semantics are UNKNOWN to us.
//
// WHY IT DECIDES THE DELIVERY (measured 2026-09-13, acceptance v4): a worker
// whose SIGTERM contract differs — the pre-drain one — FORWARDS the signal into
// its in-flight RUN CHILD. `--restart` against such a worker (master-tip,
// version 7c24ea065580, task in flight) left the task row
// `failed: signal: terminated`: the very round the drain exists to protect was
// killed by the request that promised to protect it. The DURABLE REQUEST FILE,
// by contrast, is consumed by both generations (measured: the same master-tip
// binary consumed TERM_<pid> with no signal and exited at its boundary).
//
// So: the file is always written; the SIGNAL is added only when the target
// PROVABLY shares this binary's semantics — the recorded version is equal to
// this process's own. A version mismatch or an unknown version means "request
// by file only", which is the conservative direction: a file-only drain can
// only ever be slower to exit, never destructive.
//
// This is evidence, not proof (two distinct builds can both report "dev"), so
// the message NAMES the observed versions rather than asserting equivalence.
func workerForeignDrainBinary(st workerStateFile) bool {
	if !st.Parsed {
		// Nothing readable: the version is unknown by absence of information,
		// which is not evidence of a match. File only.
		return true
	}
	return st.Version() != workerProgVersion()
}

// workerDrainAgeNote renders how long the drain has been standing: the state
// file's own timestamp when the worker has consumed the request, else the
// request file's age (the worker has not read it yet — it is mid-round). One
// renderer for both answers, so the two --stop paths cannot disagree.
func workerDrainAgeNote(dir string, pid int, st workerStateFile) string {
	if st.Parsed && st.State.Draining {
		return workerDrainVerb + " since " + st.State.DrainReport()
	}
	return "request standing for " + workerDrainRequestAge(dir, pid)
}

// workerControlForceOne is the --force path: the durable FORCE request PLUS the
// SIGUSR1 fast path (the worker's force spelling), then a bounded wait while
// the daemon closes out what it interrupted.
func workerControlForceOne(st workerStateFile) bool {
	pid := st.PID
	fmt.Printf("%s: FORCE requested for pid %d — interrupting the in-flight round, then exiting\n",
		workerProgTopLevel, pid)
	fmt.Printf("%s: state at the moment of the request — %s\n", workerProgTopLevel, st.Report())
	dir := filepath.Dir(st.Path)
	if err := workerRequestForceFile(dir, pid); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", workerProgTopLevel, err)
	}
	workerSignalPID(pid, syscall.SIGUSR1, "operator ran "+workerProgTopLevel+" "+workerControlForce)
	// SIGTERM as well: a worker binary that predates this epic has no SIGUSR1
	// handler, and an operator who typed --force must not be left with a process
	// that ignored the request. (That old binary's SIGTERM is a graceful stop,
	// which is what the operator asked for.)
	workerSignalPID(pid, syscall.SIGTERM, "operator ran "+workerProgTopLevel+" "+workerControlForce+" (fallback stop)")
	gone := workerWaitForExit(st, workerSettleWait, true, newWorkerAckPoll(dir, pid))
	fmt.Printf("%s: read what was interrupted at %s\n", workerProgTopLevel, workerLogPathFor())
	return gone
}

// workerSignalPID records the INTENT before signalling, then sends sig — the
// merger's mergerSignalPID contract (signal-provenance epic): the victim's own
// "stop requested (terminated received)" line becomes answerable, and a failure
// is reported instead of inferred from a hang.
func workerSignalPID(pid int, sig syscall.Signal, reason string) {
	// A signal to a pid that is already gone is not an event: record nothing
	// (an intent line for a dead process would be a false record).
	if err := syscall.Kill(pid, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return
		}
	}
	if err := signalrec.Intent(workerProgTopLevel, reason, pid, pid); err != nil {
		// FORENSICS ONLY — see mergerSignalPID: the wording must never read as
		// "the stop failed" (the worker's stop is delivered by the request file
		// and the signal, and the exit code is decided by the worker).
		fmt.Fprintf(os.Stderr, "%s: note — the forensic intent record could not be filed (%v); the stop itself is unaffected and proceeds\n", workerProgTopLevel, err)
	}
	if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		fmt.Fprintf(os.Stderr, "%s: could not signal pid %d with %v: %v (the request file is still in place — the worker reads it at its next claim boundary)\n",
			workerProgTopLevel, pid, sig, err)
	}
}

// workerWaitForExit polls pid until it is gone or the bound elapses — and, in
// the same loop, polls the worker's RECEIPT (epic worker-stop-ack): a receipt
// that answers THIS process's request ends the wait at once, which is what turns
// "the stop still lands, it just cannot report the sender" into an answer the
// operator (or a unit's ExecStop) can act on in milliseconds instead of minutes.
// The final line always says WHY it is waiting (Rule 15: a timeout must name
// itself). `ack` is nil for the arms that have no control directory (the
// signal-only case) and is then inert.
func workerWaitForExit(st workerStateFile, bound time.Duration, force bool, ack *workerAckPoll) bool {
	deadline := time.Now().Add(bound)
	next := time.Now().Add(workerStopProgressEvery)
	start := time.Now()
	for {
		if a, ok := ack.seen(); ok {
			ack.print(a)
			ack.consume(a)
			return true
		}
		if !tools.PidAlive(st.PID) {
			// The worker is gone — but the receipt is the FINAL act of a worker
			// that is leaving, so a receipt written an instant before its exit
			// may still be observable. The courtesy window is bounded and tiny;
			// without it the sender would report "no receipt" for a worker that
			// answered (the ordering trap).
			ack.awaitBriefly()
			fmt.Printf("%s: pid %d exited %s after the %s request\n", workerProgTopLevel, st.PID,
				time.Since(start).Round(time.Millisecond), workerStopWord(force))
			return true
		}
		now := time.Now()
		if now.After(deadline) {
			// An interrupt outranks the bound: if the operator's Ctrl-C is what
			// ended the wait, workerControlAbortNow owns the message and the
			// exit code, and the "STILL running after the bound" verdict must
			// not be published on top of it.
			if workerControlSignalPending() {
				workerControlAbortNow()
			}
			ack.missing("the worker is still running past the bound; the request is not withdrawn")
			fmt.Fprintf(os.Stderr, "%s: pid %d is STILL running %s after the %s request\n",
				workerProgTopLevel, st.PID, bound, workerStopWord(force))
			workerReportStillBusy(st, force)
			return false
		}
		if now.After(next) {
			workerReportStillBusy(st, force)
			next = now.Add(workerStopProgressEvery)
		}
		ack.noneYet(bound)
		// Interruptible for the same reason as workerClaimLoopOpened above: the
		// wake is a 500ms window in which a Ctrl-C could otherwise not be seen.
		select {
		case <-time.After(workerAckPollDefault): // bounded wait for the worker to finish the in-flight round
		case <-workerControlInterrupted():
			workerControlAbortNow()
		}
	}
}

// awaitBriefly gives a receipt written by a worker that is already exiting its
// one chance to be observed (see workerWaitForExit). It is a short poll, not a
// second wait: nothing here can extend the operator's bound.
func (p *workerAckPoll) awaitBriefly() {
	if p == nil || p.dir == "" {
		return
	}
	deadline := time.Now().Add(workerAckForeignWait)
	for time.Now().Before(deadline) {
		if a, ok := p.seen(); ok {
			p.print(a)
			p.consume(a)
			return
		}
		time.Sleep(workerAckPollDefault) //stylegate:allow-sleep bounded look for the receipt of a worker that just exited
	}
}

// workerStopWord names the request in progress messages.
func workerStopWord(force bool) string {
	if force {
		return "FORCED stop"
	}
	return "graceful stop"
}

// workerReportStillBusy prints the current state of the worker being waited on
// and, when a graceful stop is what is waiting, the escape hatch. The state is
// re-read (not the snapshot taken at request time) because the point of the
// line is to show where the worker is RIGHT NOW.
func workerReportStillBusy(st workerStateFile, force bool) {
	dir := filepath.Dir(st.Path)
	fresh := st
	for _, f := range workerLiveStateFiles(dir) {
		if f.PID == st.PID {
			fresh = f
			break
		}
	}
	fmt.Fprintf(os.Stderr, "%s: still working — %s\n", workerProgTopLevel, fresh.Report())
	if force || fresh.State.Busy() {
		fmt.Fprintf(os.Stderr, "%s: a graceful stop never interrupts a round; to interrupt it, run `%s %s %s`\n",
			workerProgTopLevel, workerProgTopLevel, workerControlStop, workerControlForce)
	}
}

// workerControlStartDaemon starts a detached worker daemon with the LOOP flags
// of THIS invocation (--once). The control flags themselves
// are not loop flags and are dropped: the spawn validation refuses any word the
// worker verb does not accept, so forwarding `--restart` would turn a restart
// into "unrecognized worker argument". It reuses the --daemon machinery
// verbatim — lock-then-hand-over, pipe handshake, an exit code that means
// something.
func workerControlStartDaemon(args []string) {
	workerDaemonSpawn(workerDaemonLoopArgs(args))
}

// workerControlEnsureDaemon is workerControlStartDaemon's HONEST spelling,
// used by --restart: it starts the replacement and then checks what the refusal
// actually MEANT.
//
// WHY (epic worker-graceful-drain, the concurrent-restart acceptance): two
// restarts run against the same draining worker. Both wait out the drain, both
// then try to start; the flock lets exactly ONE through. The loser's spawn is
// refused — and exiting 1 there would be a LIE, because the operator's
// requested end state (exactly one fresh daemon) holds. So a refusal is
// re-resolved against reality: is a live daemon running NOW? If yes, that is
// the winner, the restart succeeded, and the message says which pid won. If no,
// nothing took over and the refusal is reported as the real failure it is.
//
// The re-resolution WAITS (bounded by workerRestartSpawnWait, the same budget
// the success report uses): the refusal proves only that the winner holds the
// lock, not yet that its daemon has published state — a daemon that is
// mid-handshake is a success the instant it writes, and reading too early
// would turn it back into a false failure.
//
// prevPID is the daemon this restart replaced (0 when none was running): a
// state file for THAT pid is not the replacement, so it is skipped.
//
// Returns true when a resident daemon is live on the other side (whether this
// invocation started it or a concurrent one did).
func workerControlEnsureDaemon(dir string, loopArgs []string, prevPID int) bool {
	reused, err := workerDaemonSpawnErr(loopArgs)
	if err == nil {
		if reused {
			fmt.Printf("%s: the resident worker daemon was REUSED (it already held this profile's scope unit) — no second daemon was started\n", workerProgTopLevel)
		}
		return true
	}
	deadline := time.Now().Add(workerRestartSpawnWait)
	for {
		if live, ok := workerOtherDaemonLive(dir, prevPID); ok {
			fmt.Printf("%s: a concurrent restart already started the replacement (pid %d, binary %s) — this start was refused by the single-instance lock, and the restart end state holds; not starting a second\n",
				workerProgTopLevel, live.PID, workerVersionOrUnknown(live.Version()))
			return true
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(workerControlPoll) //stylegate:allow-sleep bounded wait for a concurrent winner's daemon to publish its state
	}
	fmt.Fprintf(os.Stderr, daemonFailNote, workerProgTopLevel, err)
	return false
}

// workerOtherDaemonLive reports whether a resident worker OTHER than prevPID is
// live: the predicate that decides whether a refused start was covered by a
// concurrent winner (see workerControlEnsureDaemon). prevPID = 0 means "any live
// daemon counts" (nothing was replaced).
func workerOtherDaemonLive(dir string, prevPID int) (workerStateFile, bool) {
	live, running, err := workerFreshestLiveState(dir)
	if err != nil || !running || live.PID == prevPID {
		return workerStateFile{}, false
	}
	return live, true
}

// workerControlEnsureDaemonOrExit wraps workerControlEnsureDaemon with the
// restart's exit contract: a refused start that nothing replaced is a real
// failure (exit 1), while a refusal a concurrent winner covered is a success.
func workerControlEnsureDaemonOrExit(dir string, args []string, prevPID int) {
	if !workerControlEnsureDaemon(dir, workerDaemonLoopArgs(args), prevPID) {
		os.Exit(workerControlExitFailed)
	}
}

// --- the single-instance lock --------------------------------------------

// workerLockPath resolves <gopi data root>/worker.lock and makes sure its
// directory exists (the creation half shared by every acquisition path).
func workerLockPath() (string, error) {
	gd, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("resolve gopi dir: %w", err)
	}
	path := filepath.Join(gd, workerLockFilename)
	if err := os.MkdirAll(filepath.Dir(path), serveLockDirMode); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	return path, nil
}

// acquireWorkerLock takes the exclusive non-blocking flock on
// <gopi data root>/worker.lock. It is the DAEMON-ONLY guard: the foreground
// `gopi worker` never calls it, so a developer can always run a worker in the
// front while a resident daemon is up (the epic's hard requirement).
//
// The refusal names the holder (pid + liveness) so an operator is told WHICH
// process to stop, and the message points at --status/--restart rather than
// leaving them to guess.
func acquireWorkerLock() (*os.File, error) {
	path, err := workerLockPath()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close() //stylegate:ignore close of the lock we could not take; releasing it is the point of the refusal
		return nil, fmt.Errorf(workerLockRefusal, path, serveLockHolder(f))
	}
	// The same unlink hazard the merger guards against: winning the flock on a
	// FRESH inode proves nothing when a running daemon still locks an unlinked
	// copy of this path (its guard is detached). Refuse while such a stray
	// holder exists, so two resident workers can never sweep the same queue.
	if stray := tools.OrphanLockHolder(path); stray > 0 && stray != os.Getpid() {
		refusal := serveLockStrayHolderRefusalError(f, stray, path)
		f.Close() //stylegate:ignore close of the freshly won flock we refuse to keep; releasing it before refusing is the point
		return nil, refusal
	}
	held, err := sameFileAsPath(f, path)
	if err != nil {
		f.Close() //stylegate:ignore close of the lock we cannot verify; refusing is the safe half of the check
		return nil, err
	}
	if !held {
		f.Close() //stylegate:ignore close of the flock bound to a replaced inode; releasing it is correct, the lock is unreachable by name
		return nil, fmt.Errorf("the worker lock file was replaced while acquiring it (%s) — refusing to start; retry", path)
	}
	if err := f.Truncate(0); err == nil {
		if _, werr := f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0); werr == nil {
			_ = f.Sync() //stylegate:ignore durability flush of the worker-lock pid; the flock itself is what guards
		}
	}
	return f, nil
}

const (
	// workerLockHandoverWait bounds the HANDOVER acquisition: how long the
	// successor's start keeps re-attempting the single-instance flock while its
	// predecessor is finishing its exit. It is a SEPARATE, separately named bound
	// (never folded into the operator's `--wait`, which measures the in-flight
	// round): the predecessor's teardown — the flock is released when the file's
	// last descriptor closes, which is strictly after the pid stops being visible to
	// workerWaitForExit — is a measurement of the MACHINE (scheduler delay, the
	// daemon's own exit path), not of the operator's request.
	workerLockHandoverWait = 10 * time.Second

	// workerLockHandoverPoll is that wait's poll cadence. It is the whole
	// acquisition that is retried (flock + stray-holder + identity + stamp), so one
	// tick is one complete, idempotent attempt.
	workerLockHandoverPoll = 100 * time.Millisecond
)

// acquireWorkerLockWait is acquireWorkerLock with the HANDOVER wait: it retries
// the complete acquisition until it wins or workerLockHandoverWait expires, and
// on expiry returns the LAST refusal — the diagnosis itself (which names the
// holder and its liveness), never a generic timeout.
//
// It is the worker's controlWait (workerDaemonHandoverSpec), used only by the
// restart path. A plain `gopi worker --daemon` takes acquireWorkerLock directly
// and still refuses on the FIRST attempt: a genuinely live daemon is not waited
// for, it is reported.
func acquireWorkerLockWait() (*os.File, error) {
	deadline := time.Now().Add(workerLockHandoverWait)
	for {
		f, err := acquireWorkerLock()
		if err == nil {
			return f, nil
		}
		if !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(workerLockHandoverPoll) //stylegate:allow-sleep bounded handover poll for the predecessor's flock release
	}
}

// workerLockHolderNote renders the lock's stamped holder for --status ("" when
// no pid is stamped at all).
//
// The liveness boolean is passed through so LockHolderNote can state the two
// measurements apart, and it is PROBED HERE (PidAlive) rather than assumed: the
// `lock holder:` line's whole job is to tell the operator which of "gone" and
// "alive, but not the flock holder" they are looking at.
func workerLockHolderNote(path string) string {
	stamped := tools.ReadStampedPID(path)
	if stamped <= 0 {
		return ""
	}
	live := tools.PidAlive(stamped)
	return fmt.Sprintf("pid %d%s", stamped, tools.LockHolderNote(stamped, live))
}

// --- the per-instance state file -----------------------------------------

// workerControlState is the worker daemon's self-reported state, written at
// round boundaries. It answers "what would a stop have to wait for?" without
// probing the process.
type workerControlState struct {
	PID     int    `json:"pid"`
	Started string `json:"started"`
	// Version is the binary's build version (buildVersion) — the fact that
	// answers "is the resident worker running the code master built?" without
	// digging through ps and the log.
	Version string `json:"version,omitempty"`
	Rounds  int    `json:"rounds"`
	// State is the idle|busy word (workerStatusState*) published at every
	// boundary. ADVISORY: readers derive the state from the timestamps below
	// (a word can drift; the timestamps are the round's own record).
	State string `json:"state,omitempty"`
	// TaskID / SessionID name the round in flight — "" between rounds. They are
	// what `gopi worker --status` renders as "what is it doing?".
	TaskID         string `json:"task_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	LastRoundStart string `json:"last_round_start,omitempty"`
	LastRoundEnd   string `json:"last_round_end,omitempty"`
	// Updated is the stamp of the LAST write to this file — the fact that makes
	// "this worker stopped updating 4 minutes ago" visible.
	Updated string `json:"updated,omitempty"`
	Note    string `json:"note,omitempty"`
	// Draining records that this worker has CONSUMED a stop request: it refuses
	// to claim new work and exits at the in-flight round's boundary. It is the
	// answer `--stop`/`--restart` report on a second invocation, and it is why
	// a repeat can be an honest no-op instead of a re-signal (which would have
	// been an escalation).
	Draining bool `json:"draining,omitempty"`
	// DrainingSince is when the drain request was consumed (RFC3339). An
	// operator asking twice reads how long the worker has been leaving.
	DrainingSince string `json:"draining_since,omitempty"`
	// Claiming records that this worker has OPENED ITS CLAIM LOOP — it reached
	// the top of its first round, so from here on a durable stop request is
	// consumed at the round boundary. It is the observable a `--stop` waits on
	// before it may attribute its own elapsed time to the worker: BEFORE it, a
	// stop cannot be honoured at all (the request is read at a round boundary,
	// and none has happened yet), and a bound applied to "has it left" would be
	// reporting the start-up prologue while claiming to report the round.
	//
	// It is a FIELD and not a derived value because reaching the boundary is
	// not the only way to be idle: the idle face publishes "queue drained"
	// without ending a round (the two are different facts — see SetIdle), so
	// "has it ever reached the top of a round" has no other witness here.
	Claiming bool `json:"claiming,omitempty"`
}

// OpenedClaimLoop reports whether the worker has reached the top of its first
// round — the fact that makes a durable stop request honour-able. A worker
// that has not is still in its start-up prologue (banner, cgroup escape,
// resident sweeps, the first ledger read), and a stop delivered then is
// necessarily served by the END OF THAT PROLOGUE, not by the round machinery.
func (s workerControlState) OpenedClaimLoop() bool { return s.Claiming }

// Busy reports whether the state describes an IN-FLIGHT round — i.e. whether a
// graceful stop right now would have to wait. The comparison is on the recorded
// timestamps, never on a boolean (a boolean would be a second source of truth
// for "a round is running" and could drift from what a reader sees).
func (s workerControlState) Busy() bool {
	if s.LastRoundStart == "" {
		return false
	}
	if s.LastRoundEnd == "" {
		return true
	}
	return s.LastRoundStart > s.LastRoundEnd
}

// Render formats the state for an operator (the --status report renders the
// same record through workerStateRecord; the drain word and its age have their
// ONE spelling in the consts below, so the fields and their labels cannot
// drift).
func (s workerControlState) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "pid %d, started %s, rounds %d", s.PID, s.Started, s.Rounds)
	if s.Version != "" {
		fmt.Fprintf(&b, ", binary %s", s.Version)
	}
	if s.Draining {
		// The drain is reported BEFORE the round state: it is the fact that
		// explains why no new work will be claimed, and an operator who asked
		// twice must see it (that is the whole idempotency contract).
		fmt.Fprintf(&b, ", %s since %s", workerDrainVerb, s.DrainReport())
	}
	if s.Busy() {
		fmt.Fprintf(&b, ", IN-FLIGHT round (started %s)", s.LastRoundStart)
	} else if s.LastRoundEnd != "" {
		fmt.Fprintf(&b, ", idle since %s", s.LastRoundEnd)
	}
	if s.Note != "" {
		fmt.Fprintf(&b, ", note: %s", s.Note)
	}
	return b.String()
}

// DrainReport renders WHEN the drain started, honestly: a state file written
// by a binary that recorded the flag but no timestamp says so, rather than
// printing an empty field an operator would read as "just now".
func (s workerControlState) DrainReport() string {
	if s.DrainingSince == "" {
		return workerDrainUnknownAge
	}
	return s.DrainingSince
}

// workerStateFile is one live instance's state file: the pid the NAME encodes
// plus the parsed (or unparsable, hence reported verbatim) record.
type workerStateFile struct {
	PID    int
	Path   string
	Raw    string
	State  workerControlState
	Parsed bool
}

// Report renders the record for an operator (an unparsable file is quoted
// rather than hidden — Rule 15).
func (f workerStateFile) Report() string {
	if f.Parsed {
		return f.State.Render()
	}
	return "unparsable worker state file: " + f.Raw
}

// Version is the binary this instance reports ("" when the record is
// unparsable or predates the version field — the caller renders that as
// "unknown" rather than pretending to know).
func (f workerStateFile) Version() string {
	if !f.Parsed {
		return ""
	}
	return f.State.Version
}

// workerStateWriter is the running worker's own state store: it owns the state
// struct, renders it to the file at every round boundary, and clears the file
// on the way out.
type workerStateWriter struct {
	dir string
	pid int
	mu  sync.Mutex
	st  workerControlState
}

// newWorkerStateWriter builds the writer for THIS process and writes the
// initial state, so a `--status` a moment later already sees the instance.
func newWorkerStateWriter(dir string, pid int, note string) *workerStateWriter {
	w := &workerStateWriter{dir: dir, pid: pid}
	w.mu.Lock()
	w.st = workerControlState{
		// The stamp layout is shared with the --status reader (Rule 2), so the
		// two sides parse the same string back.
		Started: workerStatusNow(),
		PID:     pid,
		Note:    note,
		// The binary identity is stamped ONCE, at construction: a worker runs
		// one binary for its whole life, so a later restamp could only ever
		// introduce a lie (and the restart acceptance is asserted on it).
		Version: buildVersion(),
		State:   workerStatusStateIdle,
	}
	w.mu.Unlock()
	w.flush()
	return w
}

// MarkDraining publishes that this worker has consumed a stop request: it
// stops claiming and leaves at the in-flight round's boundary. The timestamp is
// written once (a repeated request must not restart the clock — the age of the
// drain is exactly what it is). The round timestamps are deliberately NOT
// touched: the drain is not a round boundary, and rewriting LastRoundEnd here
// would hide the in-flight round a waiting operator is asking about.
func (w *workerStateWriter) MarkDraining(t time.Time, note string) {
	w.mu.Lock()
	if !w.st.Draining {
		w.st.Draining = true
		w.st.DrainingSince = t.Format(time.RFC3339)
	}
	w.st.Note = note
	w.mu.Unlock()
	w.flush()
}

// SetRoundStart records the beginning of a round, naming what it is running
// (the state becomes Busy, which is exactly what a stop would have to wait for).
func (w *workerStateWriter) SetRoundStart(t time.Time, note string) {
	w.mu.Lock()
	w.st.LastRoundStart = workerStatusStamp(t)
	w.st.LastRoundEnd = ""
	w.st.State = workerStatusStateBusy
	w.st.Note = note
	w.mu.Unlock()
	w.flush()
}

// SetRoundTask names the task (and its session) the in-flight round is
// running. It is called at the CLAIM boundary — the round-start writer knows
// only the task id, and the session id becomes knowable a few lines later —
// so the state file always carries the row an operator would have to
// investigate. The failed lookup is still recorded (the task id alone), so a
// half-known round is reported as half-known rather than as unknown.
func (w *workerStateWriter) SetRoundTask(taskID, sessionID string) {
	w.mu.Lock()
	w.st.TaskID = taskID
	w.st.SessionID = sessionID
	w.mu.Unlock()
	w.flush()
}

// SetRoundEnd records a finished round — the timestamp plus the running count
// kept HERE (not passed in) so the count can never disagree with the file's own
// history: one writer, one increment.
func (w *workerStateWriter) SetRoundEnd(t time.Time, note string) {
	w.mu.Lock()
	w.st.LastRoundEnd = workerStatusStamp(t)
	w.st.Rounds++
	w.st.State = workerStatusStateIdle
	w.st.TaskID = ""
	w.st.SessionID = ""
	w.st.Note = note
	w.mu.Unlock()
	w.flush()
}

// SetIdle publishes what the daemon is doing while the queue is drained,
// WITHOUT counting a round (an empty poll is not a round: counting it would
// make the operator's "rounds" figure meaningless).
func (w *workerStateWriter) SetIdle(t time.Time, note string) {
	w.mu.Lock()
	if w.st.LastRoundStart == "" || w.st.LastRoundEnd == "" || w.st.LastRoundStart <= w.st.LastRoundEnd {
		w.st.LastRoundEnd = workerStatusStamp(t)
		w.st.State = workerStatusStateIdle
	}
	w.st.Note = note
	w.mu.Unlock()
	w.flush()
}

// rounds reports the claim-loop round counter the receipt carries ("which
// boundary answered you"). It is nil-safe: a context without a state writer
// (the foreground face) has no round count, and the receipt then simply omits
// the field rather than inventing a zero.
func (w *workerStateWriter) rounds() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.st.Rounds
}

// MarkClaiming publishes that this worker reached the top of a round — i.e.
// that its claim loop is OPEN and a durable stop request will now be consumed
// at the round boundary. It is written once (the fact never changes back).
func (w *workerStateWriter) MarkClaiming() {
	w.mu.Lock()
	if w.st.Claiming {
		w.mu.Unlock()
		return
	}
	w.st.Claiming = true
	w.mu.Unlock()
	w.flush()
}

// Forget removes this process's state file — the exit path's cleanup (a file
// left behind would describe a pid that no longer exists, and could be
// mistaken for a live instance after a pid recycle).
func (w *workerStateWriter) Forget() {
	if w == nil {
		return
	}
	path := tools.ControlStatePath(w.dir, workerStateFilePrefix, w.pid)
	if err := tools.ClearControlText(path); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", workerProgTopLevel, err)
	}
}

// workerStatusStamp formats one round boundary timestamp. It is the ONE place
// the layout is applied (Rule 2), and the reader parses it back through the
// matching workerStatusStampLayout.
func workerStatusStamp(t time.Time) string {
	return t.UTC().Format(workerStatusStampLayout)
}

// workerStatusNow is workerStatusStamp for the current instant.
func workerStatusNow() string { return workerStatusStamp(time.Now()) }

// flush writes the current state durably (best-effort by contract: a state file
// that cannot be written must never stop a worker from working — the failure is
// REPORTED, never swallowed).
func (w *workerStateWriter) flush() {
	w.mu.Lock()
	// The update stamp belongs to the WRITE, not to the caller: every writer
	// method publishes through this one function, so "updated" can never be
	// forgotten by a new boundary (Rule 2).
	w.st.Updated = workerStatusNow()
	st := w.st
	w.mu.Unlock()
	if len(st.Note) > workerStateMaxNote {
		st.Note = st.Note[:workerStateMaxNote]
	}
	b, err := json.Marshal(st)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: marshal worker state: %v\n", workerProgTopLevel, err)
		return
	}
	path := tools.ControlStatePath(w.dir, workerStateFilePrefix, w.pid)
	if err := tools.WriteControlText(path, b); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", workerProgTopLevel, err)
	}
}

// workerLiveStateFiles lists the state files of LIVE worker daemons (pids
// ascending). One decoder, shared with the stop path (Rule 2).
func workerLiveStateFiles(dir string) []workerStateFile {
	var out []workerStateFile
	for _, inst := range tools.LiveStateFiles(dir, workerStateFilePrefix) {
		f := workerStateFile{PID: inst.PID, Path: inst.Path, Raw: inst.Raw}
		if err := json.Unmarshal([]byte(inst.Raw), &f.State); err == nil {
			f.Parsed = true
		}
		out = append(out, f)
	}
	return out
}

// workerFreshestLiveState returns the state of the single live worker daemon
// the control verbs must act on (ok=false when none is running, or when the
// live set is ambiguous). Ambiguity is REFUSED, never guessed: the flock
// guarantees at most one resident daemon, so two live state files mean the
// guard is already compromised and picking one would hide exactly that.
func workerFreshestLiveState(dir string) (workerStateFile, bool, error) {
	live := workerLiveStateFiles(dir)
	switch len(live) {
	case 0:
		return workerStateFile{}, false, nil
	case 1:
		return live[0], true, nil
	default:
		pids := make([]string, 0, len(live))
		for _, f := range live {
			pids = append(pids, strconv.Itoa(f.PID))
		}
		return workerStateFile{}, false, fmt.Errorf(
			"%d live worker state files (%s) — the single-instance guarantee is already compromised; stop them explicitly by pid",
			len(live), strings.Join(pids, ", "))
	}
}

// --- the durable stop request --------------------------------------------

// workerRequestBody encodes the durable request for pid, carrying the SENDER's
// identity in a `key=value` header so the victim can print who asked it to stop
// (see the sender-provenance block at the top of this file). The addressed pid
// remains the LAST line, so the file still reads as a request addressed to a
// pid even with the header stripped.
func workerRequestBody(pid int, sender workerStopSender) []byte {
	var b strings.Builder
	writeField := func(key, val string) {
		if val == "" {
			return
		}
		b.WriteString(key)
		b.WriteString(workerStopFieldSep)
		b.WriteString(val)
		b.WriteString("\n")
	}
	writeField(workerStopFieldPID, strconv.Itoa(sender.PID))
	writeField(workerStopFieldArgv, sender.Argv)
	writeField(workerStopFieldSignal, sender.Signal)
	writeField(workerStopFieldTS, sender.TS)
	b.WriteString(strconv.Itoa(pid))
	b.WriteString("\n")
	return []byte(b.String())
}

// parseWorkerStopSender decodes a request body's sender header. A body with no
// parsable header — an empty file, a bare pid line from a pre-provenance sender
// — yields the zero workerStopSender, i.e. UNSOURCED. Parsing NEVER fails: a
// malformed header must degrade to "the sender is unknown", never to a
// fabricated identity and never to a refused stop (the request is honoured by
// the file NAME, not by its body).
func parseWorkerStopSender(body []byte) workerStopSender {
	var s workerStopSender
	for _, line := range strings.Split(string(body), "\n") {
		key, val, ok := strings.Cut(line, workerStopFieldSep)
		if !ok {
			continue // the addressed pid's own line, or a blank line
		}
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		switch key {
		case workerStopFieldPID:
			// A pid that does not parse is an absent fact, not a zero sender:
			// falling through leaves PID 0 and the field renders "none".
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				s.PID = n
			}
		case workerStopFieldArgv:
			s.Argv = val
		case workerStopFieldSignal:
			s.Signal = val
		case workerStopFieldTS:
			s.TS = val
		}
	}
	return s
}

// workerStopSenderNow is THIS process's sender identity — the identity recorded
// into every request file this package writes, and the one an operator's
// `gopi worker --stop` leaves on the record. It is built here (not at each call
// site) so the two spellings of a stop cannot describe themselves differently
// (Rule 2).
func workerStopSenderNow(signal string) workerStopSender {
	return workerStopSender{
		PID:    os.Getpid(),
		Argv:   strings.Join(os.Args, " "),
		Signal: signal,
		TS:     time.Now().Format(time.RFC3339),
	}
}

// workerRequestStopFile creates pid's completion request, recording THIS
// process as the sender. It does NOT signal (the caller signals separately): a
// worker running with a different signal disposition still sees the durable
// request at its next claim boundary.
func workerRequestStopFile(dir string, pid int) error {
	return tools.WriteControlText(tools.ControlRequestPath(dir, workerStopRequestPrefix, pid),
		workerRequestBody(pid, workerStopSenderNow(workerGracefulSignalName)))
}

// workerRequestForceFile creates pid's FORCE request: the durable half of
// `--force`, read by the daemon's signal listener and re-read at the claim
// boundary, so a force that raced the signal still escalates. It carries the
// same sender header — the force spelling is recorded as the signal, because
// "who asked, and with what" is exactly what a forced truncation must answer.
func workerRequestForceFile(dir string, pid int) error {
	return tools.WriteControlText(filepath.Join(dir, tools.ForceRequestPrefix+strconv.Itoa(pid)),
		workerRequestBody(pid, workerStopSenderNow(workerStopForceSignalName)))
}

// workerStopSenderFromRequest reads the sender header of pid's standing
// graceful request. Absent/unreadable file, or a body with no header, yields
// the UNSOURCED zero value — never an error: the caller is rendering a
// termination line, and a request that cannot be read is missing evidence, not
// a failure of the stop (which already happened).
func workerStopSenderFromRequest(dir string, pid int) workerStopSender {
	body, err := os.ReadFile(tools.ControlRequestPath(dir, workerStopRequestPrefix, pid))
	if err != nil {
		return workerStopSender{}
	}
	return parseWorkerStopSender(body)
}

// workerStopRequestLocation reports WHERE this process's durable graceful
// request would be written: the control dir and this pid. It is the read side
// of the stop contract for waiters that have to poll the file (the drained
// wait of the never-self-exit face), AND the locator of the sender record a
// termination line prints (the requester's pid/argv are in this file). It is
// deliberately tolerant — an unresolvable control dir yields "" (no location)
// rather than an error, because the caller is a WAITER that must keep its other
// door (the in-process flag) working when the profile's state directory is
// unavailable, and a termination line rendered without a sender is still a
// termination line (it just says UNSOURCED).
func workerStopRequestLocation() (string, int) {
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil || dir == "" {
		return "", 0
	}
	return dir, os.Getpid()
}

// workerStopRequestFileExists reports whether pid's durable graceful request
// exists, never consuming it. The error is returned rather than folded into
// "false" (Rule 15b): a real stat failure means the request could not be
// observed, and the wait's caller reports it instead of waiting out the whole
// interval in silence.
func workerStopRequestFileExists(dir string, pid int) (bool, error) {
	_, err := os.Stat(tools.ControlRequestPath(dir, workerStopRequestPrefix, pid))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// workerFileStopRequested reports whether the durable request for pid exists
// (the file-only spelling of a stop, for a caller that cannot signal).
func workerFileStopRequested(dir string, pid int) bool {
	_, err := os.Stat(tools.ControlRequestPath(dir, workerStopRequestPrefix, pid))
	return err == nil
}

// workerLiveDrainRequests lists the pids under dir with a PENDING graceful
// request whose process is still alive (the shared decoder, Rule 2): the
// repeated-request contract is decided from this set, and it is the same set
// the worker's own watcher reads, so an operator and the worker can never
// disagree about whether a stop is already in flight.
func workerLiveDrainRequests(dir string) []int {
	return tools.LiveStopRequestPIDs(dir, workerStopRequestPrefix)
}

// workerDrainRequestedFor reports whether THIS pid already has a durable
// graceful request standing — the predicate that turns a repeated `--stop`
// into an honest no-op instead of a second signal.
func workerDrainRequestedFor(dir string, pid int) bool {
	for _, p := range workerLiveDrainRequests(dir) {
		if p == pid {
			return true
		}
	}
	return false
}

// workerDrainRequestAge renders how long pid's request file has been standing
// (workerDrainUnknownAge when it cannot be read): the honest half of "already
// draining" — an operator asking twice learns whether the worker has been
// leaving for a second or for ten minutes.
func workerDrainRequestAge(dir string, pid int) string {
	info, err := os.Stat(tools.ControlRequestPath(dir, workerStopRequestPrefix, pid))
	if err != nil {
		return workerDrainUnknownAge
	}
	return time.Since(info.ModTime()).Round(time.Second).String()
}

// workerFileForceRequested reports whether the durable FORCE request for pid
// exists.
func workerFileForceRequested(dir string, pid int) bool {
	_, err := os.Stat(filepath.Join(dir, tools.ForceRequestPrefix+strconv.Itoa(pid)))
	return err == nil
}

// workerClearStopRequestFor removes THIS process's durable request files — the
// cleanup half of the exit path. A request file left behind would address a
// recycled pid later.
//
// workerClearStopAckFor is the SAME cleanup for the RECEIPT (epic
// worker-stop-ack), and it is deliberately separate rather than folded in: a
// request file is removed because the address would be recycled, while a receipt
// leaves the LIVE namespace because it has been READ (epic 01a17df9-524a).
// "Leaves" is now an ARCHIVE, not a delete: the reader renames it to
// `ACK_<pid>.<req_id>.consumed` and appends one index line, because the receipt
// is the only evidence that a stop was acknowledged and deleting it destroyed
// that evidence (see devdoc/worker-stop-receipt-archive.md). A receipt is never
// touched at the moment it is WRITTEN (it must survive the poll that looks for
// it, including one that runs a moment after this process exits), and an
// unread one is archived — not deleted — by the resident sweep once it is old
// enough that its requester is clearly gone.
func workerClearStopRequestFor(dir string, pid int) {
	if dir == "" {
		return
	}
	for _, path := range []string{
		tools.ControlRequestPath(dir, workerStopRequestPrefix, pid),
		filepath.Join(dir, tools.ForceRequestPrefix+strconv.Itoa(pid)),
	} {
		if err := tools.ClearControlText(path); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", workerProgTopLevel, err)
		}
	}
}

// workerAckAnswersRequest is the ONE acceptance predicate of the channel: a
// receipt is this command's answer iff it names THIS process as the requester
// AND its req_ts does not predate this command's own request. It is shared by
// the polling reader and by nothing else, so "already answered" cannot mean two
// things (Rule 2).
//
// WHY THE ANCHOR IS THE RECEIPT'S OWN req_ts AND NOT THE FILE'S MTIME (measured
// 2026-10-16 on host-a): the first shape compared the receipt file's mtime
// against the instant the poll was armed, and on this filesystem mtime is
// TRUNCATED to a granularity coarser than time.Now() — a file created 1.6ms
// AFTER the reading still carried an mtime BEFORE it, so a legitimate receipt
// was refused and the sender waited out the whole bound for nothing (measured:
// 32.08s against an 8s expectation). The receipt's own body has no such
// problem: req_ts is the sender's timestamp as the worker read it back, so a
// received receipt is provably posterior to (or at) the request this process
// wrote, whatever the filesystem does to mtimes.
//
// A receipt whose req_ts is missing or unparsable is REFUSED rather than
// trusted: an unknown provenance cannot be this process's answer, and the
// conservative direction here costs one wait, never a false "it answered"
// (Rule 15).
func workerAckAnswersRequest(a workerAck, reqID int, since time.Time) bool {
	if reqID <= 0 || a.ReqID != reqID {
		return false
	}
	ts, err := time.Parse(time.RFC3339, a.ReqTS)
	if err != nil {
		return false
	}
	// RFC3339 carries second resolution, so the floor is the tolerance: a
	// request written and answered inside one second must not read as
	// predating its own sender (workerAckReqTSTolerance).
	return !ts.Before(since.Add(-workerAckReqTSTolerance))
}

// workerAckReqTSTolerance absorbs the truncation of the request's own second
// resolution timestamp plus a slow clock read, so a receipt written moments
// after its request is never refused for being "early" (Rule 1: a named
// tolerance, not a magic number at the comparison).
const workerAckReqTSTolerance = 2 * time.Second

// workerLogPathFor is where the detached daemon's stdio lands — the pointer
// every stop/force message ends with, so the operator can read what was
// interrupted.
func workerLogPathFor() string {
	p, err := workerDaemonLogPath()
	if err != nil {
		return "(log path unresolved)"
	}
	return p
}

// workerDaemonCtx is a worker's control plumbing for THIS process. Two
// capabilities live in it, and they are INDEPENDENT on purpose:
//
//	PUBLISH (writer; the state file)  — built for the resident DAEMON only.
//	  A state file is what `--start`/`--status`/`--restart` read to decide
//	  whether a worker is already running, so a foreground worker must never
//	  write one (it would make `--start` refuse with "already running" and
//	  `--status` report a daemon that does not exist).
//	REQUESTS (requests; the durable stop/force files) — built for BOTH faces
//	  (workerCtxFor). This is the delivery channel `gopi worker --stop` uses:
//	  before it existed, a foreground worker could only be reached by a SIGNAL,
//	  so the control face had to refuse whenever the target ran a different
//	  binary (bug 2026-10-13: `--stop` degraded to "stop it yourself: kill
//	  -SIGTERM <pid>" on the normal post-rebuild state) — a refusal with no
//	  fallback, because there was no file to fall back to.
//
// Every method below asks the capability it actually needs, so the two cannot
// be conflated again (Rule 15: a surface that cannot be written must not be
// read as a surface that is not watched).
type workerDaemonCtx struct {
	dir    string
	pid    int
	writer *workerStateWriter
	// ackExit is the exit sentence this face writes into its stop receipt.
	// Empty means the claim-loop sentence (workerAckExitSemantic) — the answer
	// for every face that stops at a round boundary. The RESUME face declares
	// its own, because it has no round boundary to promise (bug 01a17df2).
	ackExit string
}

// publishes reports whether this context writes the DAEMON STATE file. False
// for the foreground face, whose presence is read from the process table
// (foregroundWorkerPIDs) rather than from a daemon record.
func (c workerDaemonCtx) publishes() bool { return c.writer != nil }

// consumesRequests reports whether this context reads the durable stop/force
// REQUEST files this pid is addressed by — the channel `gopi worker --stop`
// delivers through.
//
// It is derived from the DIRECTORY, not from a flag: a context that resolved a
// control dir has the channel, and the zero value (no dir) is inert. A separate
// boolean would be one more field every constructor has to remember, and a
// fixture that forgot it would silently stop consuming requests — a test-only
// hang, which is exactly the failure this shape removes.
func (c workerDaemonCtx) consumesRequests() bool { return c.dir != "" }

// workerDaemonCtxFor builds the control plumbing for a resident daemon (both
// capabilities: it publishes a state file AND consumes requests). A failure to
// resolve the state directory is REPORTED and yields an inert context rather
// than aborting the daemon (Rule 15: a reporting surface that cannot be written
// must never stop the work it reports on).
func workerDaemonCtxFor(pid int) workerDaemonCtx {
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v — the daemon runs without a state file (--status will not see it)\n",
			workerProgTopLevel, err)
		return workerDaemonCtx{}
	}
	return workerDaemonCtx{dir: dir, pid: pid, writer: newWorkerStateWriter(dir, pid, "daemon starting")}
}

// installFileWatcher starts the poll that turns an operator's DURABLE request
// file into the in-process stop flag. It is the second half of the stop
// contract: the signal is the nudge, the file is the record, and either alone
// is sufficient — a worker whose signal was missed (a race with start-up) still
// stops. The watcher stops with the process (the goroutine blocks on the file
// poll until then; the loop exits shortly after the flag flips).
//
// It CONSUMES through the same implementation the loop's claim boundary uses
// (consumeDrainRequests): one reader of a request, so "the file said stop" and
// "the boundary said stop" can never disagree. The ticker only makes the stop
// snappier than the boundary — it is never the mechanism.
//
// It is installed by BOTH faces (a foreground worker is a stop target too, and
// its only other door is a signal — see workerDaemonCtx), so the guard is on the
// REQUEST capability, not on the state writer.
//
// The watcher is STOPPABLE, and the caller owns the teardown: production exits
// the process when the loop returns (so the goroutine used to be reclaimed by
// the kernel), but this package's suite drives the loop IN-PROCESS
// (TestRunTaskWorker_OnceSweepsZombieFirst, TestRunTaskWorker_SurvivesFailedRun),
// where a return leaves the poll parked for the life of the test binary and the
// suite's close-out goroutine sweep — correctly — reports it and fails the run.
// A watcher that can only be ended by exiting is therefore not installable from
// a harness: it returns its own uninstall, the same shape as
// installWorkerStopSignals.
//
// THE UNINSTALL JOINS. Signalling the loop is not enough: close(stopped) only
// makes the goroutine RUNNABLE, and the harness that reads this returns to
// MEASURE the process on its very next instruction (TestMain's close-out sweep
// samples the stack, then fails the run when it reports a parked repository
// goroutine). A stop function that returns before the thing it stopped is not a
// stop function — the same defect, and the same fix, as
// startSuiteStallWatchdog's (suite_stall_watchdog_test.go, measured 2026-09-25:
// a fully green run failed on `parked repository goroutines: 1` naming the
// suite's OWN watchdog). So the loop closes `exited` as it leaves and the
// uninstall waits for that.
//
// `once` is what makes a SECOND call safe: the deferred teardown and an
// explicit stop in the same function are both ordinary shapes, and a bare
// close(stopped) panics on the second ("close of closed channel"). The join is
// outside the once — every caller waits, the close happens once.
func (c workerDaemonCtx) installFileWatcher() (uninstall func()) {
	if !c.consumesRequests() {
		return noopWorkerFileWatcherStop
	}
	// done carries the REQUEST, exited carries the goroutine's LEAVING. Both are
	// reference-typed channels created HERE, so the value receiver costs nothing:
	// the closure that ends the poll is the same instance that started it.
	done := make(chan struct{})
	exited := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(exited)
		t := time.NewTicker(workerFileWatchInterval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				c.consumeDrainRequests()
			case <-done:
				return
			}
		}
	}()
	return func() {
		once.Do(func() { close(done) })
		<-exited
	}
}

// noopWorkerFileWatcherStop is the uninstall the INERT face (a context with no
// control dir — nothing was installed) returns. It is a NAMED function, not an
// inline `func() {}`, so the caller contract is one object: an installed
// watcher's stop is a JOIN whose return is itself the evidence that the poll has
// left the stack, while this one has nothing to observe and may be deferred
// unconditionally. The name is also what makes the two arms distinguishable in a
// stack trace — an anonymous closure is indistinguishable from the join.
func noopWorkerFileWatcherStop() {}

// cleanupRequests removes this process's durable request files on the way out
// (a request left behind would address a recycled pid later).
func (c workerDaemonCtx) cleanupRequests() {
	if !c.consumesRequests() {
		return
	}
	workerClearStopRequestFor(c.dir, c.pid)
}

// setRoundStart / setRoundEnd / setIdle publish the round boundary and the idle
// state in the daemon's state file (its answer to "what would a stop have to
// wait for?"). All three are no-ops for the foreground face.
func (c workerDaemonCtx) setRoundStart(note string) {
	if !c.publishes() {
		return
	}
	c.writer.SetRoundStart(time.Now(), note)
}

// consumeDrainRequests is the CLAIM-BOUNDARY half of the durable drain
// contract (epic worker-graceful-drain): it reads this pid's request files and
// turns a standing request into the in-process stop flag, so the worker stops
// claiming even when the signal was missed (a race with start-up) or was never
// sent (a caller that can only write a file). It is called by the loop at the
// TOP of every round — i.e. exactly at the claim boundary — and by the
// ticker-driven watcher, and it is IDEMPOTENT: a request that is already
// consumed re-affirms the same flag, and the state file's drain timestamp is
// written once (MarkDraining keeps the FIRST one).
func (c workerDaemonCtx) consumeDrainRequests() {
	if !c.consumesRequests() {
		return
	}
	if workerFileForceRequested(c.dir, c.pid) {
		// The force file is read here too: a --force that raced the signal (or
		// was written by a process that could not signal) must still escalate
		// at the boundary. Force implies the drain — the worker stops claiming
		// either way.
		//
		// FRESHNESS IS READ BEFORE THE BODY WRITES ITS FLAGS (see the drain arm
		// below and notifyWorkerStopFresh): only the consume that ESTABLISHES
		// the escalation wakes the forwarder.
		fresh := !workerStopEscalated()
		reason := workerForceReason(c.dir, c.pid)
		if fresh {
			c.markDraining(reason)
		}
		workerRequestStop(reason, true)
		workerMarkDrainRequested()
		workerRecordStopSignal(workerStopSenderFromRequest(c.dir, c.pid).Signal)
		// THE RECEIPT (epic worker-stop-ack): the request is CONSUMED, and this
		// is the instant the sender's blind wait can end — so the answer is
		// written here, on the fresh transition only, next to the flags that
		// describe it. The force request carries its own sender header (the
		// spelling --force recorded), so the receipt answers the caller that
		// actually asked.
		if fresh {
			c.publishStopAck(workerAckModeForce, workerAckSenderFromForceRequest(c.dir, c.pid))
		}
		// The forwarder may be waiting for a wake-up: the durable request IS
		// the stop, and a worker that only looks at the file at its claim
		// boundary would keep the in-flight round running under a request the
		// operator already made (the forwarder re-reads the file and, seeing a
		// DRAIN, still forwards nothing — the drain guarantee is untouched).
		if fresh {
			notifyWorkerStopNudge()
		}
		return
	}
	if workerFileStopRequested(c.dir, c.pid) {
		// FRESHNESS, AND WHY IT IS NOT AN OPTIMISATION (measured 2026-10-15,
		// host host-a). consumeDrainRequests is called from THREE places: the
		// loop's claim boundary, the ticker watcher, and — decisively — the
		// forwarder ITSELF, as its first act (forwardWorkerStopDuringRound).
		// With the wake-up unconditional, that was a self-re-arming loop: the
		// forwarder consumes, the consume nudges, the forwarder (back in its
		// select) receives its own nudge at once, consumes again, nudges again —
		// a hot loop that never blocks. Measured on a draining worker:
		// 514 ticks of CPU in 5s (100% of a core), 93 state-file rename
		// failures in 3m49s, for a drain that was doing nothing.
		//
		// Each round of that loop also re-published the state record, which is
		// what turned a harmless re-publish into the ENOENT storm: two
		// goroutines flushing the SAME control file through ONE shared tmp name
		// (see tools.WriteControlText). Both halves are fixed: the tmp is unique
		// per write, and the wake-up fires only on the transition — so the loop
		// ends after ONE consume, and the state record stops being rewritten
		// hundreds of times a minute for no new fact.
		fresh := !workerDrainRequestedByFile()
		reason := workerStopReasonForRequest(c.dir, c.pid)
		if fresh {
			c.markDraining(reason)
		}
		workerRequestStop(reason, false)
		workerMarkDrainRequested()
		// SENDER PROVENANCE (see the sender-provenance block in
		// cli_worker_control.go): the requester's pid/argv/signal ride the
		// request file, and this boundary is where the victim can still hand
		// them to the termination line. Recording is idempotent and
		// first-writer-wins, so the ticker watcher and the loop's boundary
		// consume cannot overwrite the identity of the request that started
		// the drain.
		sender := workerStopSenderFromRequest(c.dir, c.pid)
		workerRecordStopSender(sender)
		workerRecordStopSignal(sender.Signal)
		// THE RECEIPT (epic worker-stop-ack): written on the fresh transition
		// only — see the force arm above and this file's header. It carries the
		// sender's own identity, so the sender can accept it as the answer to
		// ITS request and no one else's.
		if fresh {
			c.publishStopAck(workerAckModeDrain, sender)
		}
		// Wake the forwarder: the durable request is a stop too, and the
		// round in flight must learn about it while it is still running (the
		// forwarder re-reads the file first and forwards nothing for a DRAIN,
		// so the drain guarantee is untouched). See `fresh` above — the wake-up
		// is the TRANSITION, never a re-arm.
		if fresh {
			notifyWorkerStopNudge()
		}
	}
}

// markDraining publishes the drain in the state file — a no-op when there is no
// state file to write (the FOREGROUND face: no daemon record addresses it).
// The DRAIN ITSELF is not lost there: it lives in the in-process stop record
// (workerRequestStop above), which is what every exit path reads.
func (c workerDaemonCtx) markDraining(note string) {
	if !c.publishes() {
		return
	}
	c.writer.MarkDraining(time.Now(), note)
}

// drainRequestPending reports whether a drain request is standing for this
// process (the loop's cheap boundary predicate; the CONSUME is separate so the
// loop's boundary and the watcher read through one implementation).
func (c workerDaemonCtx) drainRequestPending() bool {
	if !c.consumesRequests() {
		return false
	}
	return workerFileForceRequested(c.dir, c.pid) || workerFileStopRequested(c.dir, c.pid)
}

// workerStopReasonForRequest is the recorded reason for a FILE-delivered
// graceful stop (the signal path records its own spelling).
func workerStopReasonForRequest(dir string, pid int) string {
	return "stop request file " + tools.ControlRequestPath(dir, workerStopRequestPrefix, pid)
}

// workerForceReason is the recorded reason for a FILE-delivered force.
func workerForceReason(dir string, pid int) string {
	return "force request file " + filepath.Join(dir, tools.ForceRequestPrefix+strconv.Itoa(pid))
}

// setRoundEnd publishes a finished round and what it ran.
func (c workerDaemonCtx) setRoundEnd(note string) {
	if !c.publishes() {
		return
	}
	c.writer.SetRoundEnd(time.Now(), note)
}

// setRoundTask publishes the task (and session) the in-flight round is running
// — the field `--status` shows as "what is it doing?".
func (c workerDaemonCtx) setRoundTask(taskID, sessionID string) {
	if !c.publishes() {
		return
	}
	c.writer.SetRoundTask(taskID, sessionID)
}

// setIdle publishes the drained-queue state (never counted as a round).
func (c workerDaemonCtx) setIdle(note string) {
	if !c.publishes() {
		return
	}
	c.writer.SetIdle(time.Now(), note)
}

// markClaiming publishes that the loop reached the TOP OF A ROUND — its claim
// boundary. It is the observable `--stop` reads before it may charge its own
// elapsed time to the worker: the durable request is consumed at a boundary,
// so before the first one there is nothing that could honour a stop.
func (c workerDaemonCtx) markClaiming() {
	if !c.publishes() {
		return
	}
	c.writer.MarkClaiming()
}

// workerFileWatchInterval is how often a worker re-reads its request directory —
// coarse enough to cost nothing, fine enough that an operator's `--stop` is
// noticed well within the wait bound. It is the ONLY timer either face adds
// (the signal path needs none).
const workerFileWatchInterval = 2 * time.Second

// workerCtxFor returns the control plumbing appropriate to THIS process.
//
// BOTH faces get the REQUEST channel (the durable stop/force files) — that is
// the channel `gopi worker --stop` delivers through, and a foreground worker is
// as much a stop target as a daemon (before this, its only door was a signal,
// so a target running another binary could not be stopped at all — see
// workerDaemonCtx). The STATE FILE stays the daemon's alone: it is the record
// `--start`/`--restart`/`--status` read to answer "is a worker already
// running?", and a foreground worker that published one would make `--start`
// refuse as "already running" and `--status` report a daemon that is not there.
func workerCtxFor(pid int) workerDaemonCtx {
	if workerDaemonChild() {
		return workerDaemonCtxFor(pid)
	}
	return workerRequestCtxFor(pid)
}

// workerRequestCtxFor builds the REQUEST-ONLY control plumbing — the foreground
// face's shape. An unresolvable state directory is REPORTED and yields an inert
// context (Rule 15: a process that never starts because a control directory
// could not be created is a worse failure than one that cannot be stopped by
// file), which leaves the stop face with its signal arm.
func workerRequestCtxFor(pid int) workerDaemonCtx {
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v — this worker takes no durable stop request (only a signal can stop it)\n",
			workerProgTopLevel, err)
		return workerDaemonCtx{}
	}
	return workerDaemonCtx{dir: dir, pid: pid}
}

// workerForceSignalRequested reports whether sig is the FORCE spelling
// (SIGUSR1, the merger's convention for "interrupt what you are doing"). The
// graceful set itself lives in cli_tasks_worker_stop.go's workerStopSignals —
// including SIGUSR1, so the signal is not "unhandled" and the escalation is
// decided by the same one flag every other exit path reads.
func workerForceSignalRequested(sig os.Signal) bool {
	return sig == syscall.SIGUSR1
}

// --- the read-only status face -------------------------------------------

// workerLockPathReadOnly resolves the lock path WITHOUT creating anything (a
// status probe must not have the side effect of creating the directory).
func workerLockPathReadOnly() (string, error) {
	gd, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("resolve gopi dir: %w", err)
	}
	return filepath.Join(gd, workerLockFilename), nil
}

// workerLockState answers "is a resident worker daemon running?" WITHOUT taking
// the lock — by looking the lock file's inode up in the kernel's lock table
// (the merger's mergerLockProbe discipline, reused through the shared helper).
func workerLockState(path string) (state string, holder string) {
	holderPID, live := tools.LockProbe(path)
	switch {
	case live:
		return tools.StatusAlive, fmt.Sprintf("pid %d", holderPID)
	case holderPID > 0:
		return tools.StatusNotRunning, fmt.Sprintf("pid %d%s", holderPID, tools.LockHolderNote(holderPID, false))
	default:
		return tools.StatusNotRunning, ""
	}
}
