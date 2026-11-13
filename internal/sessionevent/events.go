// Package sessionevent is the single source of truth for the session-JSONL
// event names that delimit a session's life: the first line an agent writes
// (session_start) and the last one (session_end).
//
// Why a package and not a const in the owning package (PROJECT.md Rule 14): the
// value is written by internal/agent, read by internal/gopilog,
// internal/tools and internal/testutil. internal/agent and internal/gopilog
// import each other (gopilog/formatter.go -> agent), so no single consumer
// package can hold a definition the others reach — the shared value needs a
// leaf package with zero project imports, exactly like internal/toolnames.
//
// Rule 1b/13: never type the session_start literal inline where the event
// name is compared, switched on, or written. Before this package the
// session_start name was bypassed in 16 production sites across five files,
// while its session_end sibling already had a single (package-private)
// constant — a typo in ONE read site silently stopped matching the writer,
// and the reader degraded to timestamp heuristics with no error at all
// (PROJECT.md Rule 15: no silent failure).
//
// The string VALUES are the persisted wire format of every session JSONL on
// disk (and of already-recorded sessions): they are read back by `gopi session log`,
// `gopi session stats`, `gopi task show` and the chat backfill scan. Do not change a
// value here without a migration for existing session files.
package sessionevent

// Session lifecycle event names as they appear in the session JSONL's "type"
// field. Each is the SINGLE definition of its value — every producer and
// consumer references these constants (internal/sessionevent's literal gate
// test fails the build if any production file re-inlines one).
const (
	// EventTypeSessionStart is the session's first event, written and synced
	// by (*agent.SessionLogger).NewSessionLogger before the first turn.
	// It carries sessionId, timestamp, model, workdir and (when capture is
	// on) rawLogPath — the timestamp is what session ordering and
	// `stats --cost` date-aware pricing key off.
	EventTypeSessionStart = "session_start"

	// EventTypeSessionEnd is the session's last event, written by
	// (*agent.SessionLogger).LogSessionEnd at agent exit. It carries
	// exit_code, total_iterations, total_tokens and elapsed_sec, and its
	// optional "error" block is itself the crash marker.
	//
	// IT ALSO CARRIES HOW THE RUN ENDED, and that is a separate fact from the
	// exit code (task 01a163f8). Measured on master 5758f23 against the real
	// binary: a `/quit`, a closed stdin and a Ctrl+D all wrote
	// `{"exit_code":0,…}` with nothing else — three different endings, one
	// byte-identical record — so "the session ended" was answerable and "why it
	// ended" was not. The cause rides two keys (SessionEndFieldReason /
	// SessionEndFieldSignal), both ABSENT on a log written by an older binary,
	// which is why every reader must treat absence as "not recorded" rather
	// than manufacture a default (Rule 15).
	EventTypeSessionEnd = "session_end"

	// The wire keys of session_end's ending cause. ONE definition, in the leaf
	// package, because the writer is internal/agent and the readers are
	// internal/gopilog and internal/testutil — the same Rule 14 argument the
	// resume_options keys below state.
	//
	// SessionEndFieldReason is the machine-readable WHY: one of the
	// SessionEndReason* values. Always written by a binary that knows about it,
	// so "the key is missing" identifies a log from an older binary rather than
	// a run with no cause.
	SessionEndFieldReason = "reason"
	// SessionEndFieldSignal is the recorder's own spelling of the terminating
	// signal ("SIGHUP", "SIGTERM", … — signalrec.SignalName's vocabulary), and
	// is written ONLY beside SessionEndReasonSignalled: it is the elaboration of
	// that one reason, never an independent axis, so a record cannot claim
	// reason=quit and signal=SIGHUP.
	SessionEndFieldSignal = "signal"
	// SessionEndFieldEOFKind refines SessionEndReasonEof, and is written ONLY
	// beside it (the same one-axis discipline as SessionEndFieldSignal): an EOF
	// ending has two very different meanings — a human pressed Ctrl+D, or the
	// INPUT SOURCE went away under the run (a pipe's writer exited, a terminal
	// was torn down) — and the original report of this defect could not tell
	// which had happened. The reason stays `eof` for both; this key says which.
	SessionEndFieldEOFKind = "eof_kind"

	// EventTypeTaskRowClosed is the ledger terminal-write witness: it is
	// written by the END hook (internal/agent/taskregister.go
	// finishSessionTaskRecorded) at the moment the run closes its OWN ledger
	// row, and carries the row's task_id, the resulting ledger status, the run
	// end reason and the close-out error, if the write failed.
	//
	// WHY A SESSION EVENT: the row's terminal status must be provable from the
	// run's own record. Without it, "the end hook never performed the write"
	// (the truncated-run defect: the hook returned early and the row stayed
	// `in_progress` forever) and "the write happened but is not in this slot"
	// are indistinguishable from the transcript — which is exactly how the
	// defect survived a status rule that was already CORRECT.
	//
	// Readers must tolerate it: it is a plain JSON object with a "type" field,
	// no "message" key, and it is written AFTER session_end (the ledger write
	// runs at the same close-out step). Consumers that switch on event type
	// without a default branch skip it structurally (internal/gopilog, and
	// internal/tools/memory_brew's brewRenderEvent, which returns "" for an
	// event with no message).
	EventTypeTaskRowClosed = "task_row_closed"

	// EventTypeSignalVerdict is the signal recorder's terminal write into a
	// run's OWN session log: a gopi process that received a terminating signal
	// states, in the record an operator reads afterwards, that it was signalled
	// and — through the writer's own "sourced" field — whether a gopi stop path
	// had announced the kill.
	//
	// WHY A SESSION EVENT AND NOT A PROSE LINE. The line started life as prose
	// appended to the session file, and the session readers do NOT agree about
	// junk: internal/gopilog.ParseFile (the unified reader behind
	// `gopi session log --jsonl`, `gopi session log --chat`, `gopi session stats`) json.Unmarshals every
	// line and fails the WHOLE file on any unparsable one, while
	// ValidateSessionFile and SessionRawLogPath unmarshal per line and silently
	// `continue`. A prose line would therefore be invisible to two readers and
	// fatal to the third — `gopi session log --jsonl` would fail on exactly the session
	// an operator opens after a kill. A JSON event with its own type is
	// tolerated by all of them structurally: it has no "message" key, so every
	// formatter that switches on event type without a default renders nothing.
	//
	// The writer is internal/signalrec (appendSessionLogVerdict), which imports
	// this package — never the other way round, so the value keeps ONE home
	// (Rule 14). An event whose kill could not be classified simply omits the
	// writer's "sourced" field rather than guessing a verdict.
	EventTypeSignalVerdict = "signal_verdict"

	// EventTypeSignalRepeat is the re-affirm witness of a REPEATED delivery of a
	// signal kind this process has already recorded (the writer is
	// internal/signalrec's noteRepeatedSignal, and the rationale for the
	// separate type is stated there).
	//
	// WHY IT IS NOT EventTypeSignalVerdict. That type answers "this run was
	// signalled, and here is whether a gopi stop path announced it" — ONE answer
	// per run, the question an operator's post-kill filter asks. A repeat is a
	// different fact (the same request arrived again, the first delivery's
	// disposition untouched) and it arrives any number of times, so reusing the
	// verdict type would turn "Ctrl-C pressed four times" into "four verdicts".
	// It carries the writer's own "repeat" counter, so a reader can see HOW MANY
	// extra deliveries arrived without parsing anything.
	EventTypeSignalRepeat = "signal_repeat"

	// EventTypeTurnInterrupted is the per-turn interrupt witness: written by
	// (*agent.Agent).recordTurnInterruption when an interactive carrier
	// abandoned ONE turn with a run-scoped cancel (a Ctrl+C at the incoming
	// prompt of `gopi chat`) while the session itself stayed OPEN.
	//
	// WHY IT IS NOT SESSION_END. The two cancellations are different events and
	// the record has to tell them apart: Interrupt() ends the PROCESS (its
	// session_end carries exit_code=1), whereas InterruptRun() ends a TURN —
	// the next prompt is answered normally. Without a witness of its own, the
	// abandoned turn was recorded as nothing at all (a partial answer with no
	// statement of why it stops), which is exactly what a reader of the
	// transcript cannot reconstruct: the stream's last delta and the model's
	// own stopping point look identical on disk.
	//
	// It carries "reason" (the carrier's own word for the interruption — see
	// agent.InterruptReasonSet: ctrl-c, escape for the pure interrupt, ctrl-d
	// when the operator pressed end-of-input mid-turn, which claims the turn so
	// the session can actually leave, or shutdown when the long-lived carrier's
	// own SIGTERM abandoned it) and
	// "partial_chars" (how much of the answer had already reached the screen when
	// the cancel landed), so a partial answer is not mistaken for a short one —
	// and so a reader who was not at the terminal can tell WHICH key was pressed.
	EventTypeTurnInterrupted = "turn_interrupted"

	// EventTypeResumeOptions records the OPTIONS a resumable run was started
	// with, so a later run can restore them without the operator replaying the
	// command line.
	//
	// WHY IT IS RECORDED AND NOT DERIVED. A checkpoint restores a session's
	// CONVERSATION and its tool DEFINITIONS, never its command line: a
	// tool-enabled chat resumed without `--tools` inherits a history full of
	// tool_use/tool_result turns while the run itself has no tools, so the
	// model answers by DESCRIBING the tool call it cannot make (measured
	// 2026-09-29, task 01a0eb0e…). The capability the crashed run had has to
	// come from somewhere, and the only party that ever knew it is the run
	// itself — at start-up, before anything can go wrong.
	//
	// Its WRITER is internal/agent (NewSessionLoggerForRun), called from the
	// chat verb immediately after the session is minted and SYNCED, so the
	// line is on disk before the first turn can crash. Its READER is
	// internal/gopilog.ResumeOptions, and it is deliberately tolerant in the
	// same way EventTypeSignalVerdict and EventTypeTaskRowClosed are: a plain
	// JSON object with a "type" field and no "message" key. Every reader that
	// switches on event type without a default branch skips it structurally,
	// and ParseFile's whole-file json.Unmarshal accepts it — which is exactly
	// what a file written by an OLD binary (no such event) and read by a NEW
	// one, or the reverse, requires.
	//
	// The event is ADDITIVE and OPTIONAL: it is absent for every session that
	// predates it and for every run that has nothing to record, and a reader
	// that finds it absent must say "the options were not recorded" (Rule 15)
	// rather than manufacture a default. Precedence — what the command line
	// states explicitly always wins over what the record carries — lives in the
	// chat verb, not here.
	EventTypeResumeOptions = "resume_options"

	// EventTypeTokenReconciliation is the PER-REQUEST reconciliation of the two
	// numbers a reader has to compare but could not, until this event existed: the
	// LOCAL prediction (what this engine thinks the prompt costs) and the SERVER's
	// own prompt_tokens for the same request.
	//
	// THE DEFECT IT CLOSES. The deviation between the two was measured ONCE, by
	// hand, and written into a source comment (internal/agent/context_tokens.go:
	// 141,548 predicted against 144,967 charged, i.e. 2.4%). Nothing reconciled it
	// at RUN time, so nothing could answer the question the number actually
	// raises — is 2.4% a CONSTANT of the tokenizer's accounting (a one-off framing
	// overhead), or does it DRIFT with the content? A single manual sample cannot
	// tell the two apart, and a factor that drifts is a status row that silently
	// lies by a growing amount.
	//
	// WHY AN EVENT AND NOT A WIDER STATUS ROW: the row's budget is spent to
	// equality (statusPinSlackCells is 0), and this is a PER-REQUEST fact — the
	// row is a live snapshot and has no cells to hold a history. The session JSONL
	// already IS that history, and `gopi session log <id>` already reads it, so
	// the reconciliation needs no new command, no resident process and no timer
	// (Rule 35): one row per request, written where every other usage fact is.
	//
	// ADDITIVE AND OPTIONAL, like every event beside it: absent on a log written by
	// an older binary, and absent on a request the server measured nothing for. A
	// reader that finds none says so rather than manufacturing a 0 (Rule 15).
	EventTypeTokenReconciliation = "token_reconciliation"

	// The wire keys of the resume_options event's payload. They live HERE, in
	// the leaf package, for the reason this package exists (Rule 14): the
	// WRITER is internal/agent and the READER is internal/gopilog, and those
	// two cannot share a definition — gopilog/formatter.go imports
	// internal/agent, so the reader's package is not importable from the
	// writer's. A key typed twice is a key that drifts; a key declared once in
	// the package both may import is the rule's own remedy.
	ResumeOptionsFieldFlags     = "flags"
	ResumeOptionsFieldSessionID = "sessionId"
	ResumeOptionsFieldWorkdir   = "workdir"

	// The wire keys of a RESUME witness — the session_start a resumed run APPENDS
	// to the record it continues.
	//
	// WHY A RESUMED RUN APPENDS ONE AT ALL (epic: `gopi chat --resume <id>`
	// minted a second session). Once resume reuses the id instead of minting a
	// new one, one session file legitimately holds SEVERAL session_start rows —
	// one per run. Nothing else in the file can say why: a reader that sees two
	// headers cannot tell a continued session from a corrupt or concatenated
	// one, and a reader that sees the file grow while its session_end stays the
	// one from the run that died cannot tell "still running" from "resumed".
	// These two keys are that statement:
	//
	//	SessionStartFieldResumedFrom — the session this run continues (the id the
	//	    operator passed). Equal to the row's own sessionId by construction, and
	//	    still written: the field states the run's INTENT, and a reader must be
	//	    able to check the two agree rather than infer it from the layout.
	//	SessionStartFieldResumedFromRawLog — the raw HTTP log this run APPENDED
	//	    to. It is the same file the previous run's session_start named, which
	//	    is what makes "the raw log was continued, not recreated" checkable from
	//	    the record alone.
	//
	// Both ABSENT on the first run of a session: absence keeps its single meaning
	// ("this run started the session"), exactly as every other optional key in
	// this package does (Rule 15a — a required fact is never carried by a key
	// whose zero value reads as "not recorded").
	SessionStartFieldResumedFrom       = "resumedFrom"
	SessionStartFieldResumedFromRawLog = "resumedFromRawLog"
)

// The session_end reason vocabulary — the values SessionEndFieldReason may
// carry, declared once here because the writer (internal/agent) and every
// reader that must tell two endings apart need the same spelling (Rule 14).
//
// The set is CLOSED on purpose: a reason nobody can read is a reason nobody
// can act on. A run whose ending fits none of these says so with
// SessionEndReasonUnknown rather than inventing a word — the same
// deny-by-default discipline internal/deepseek's error taxonomy uses.
const (
	// SessionEndReasonEof is the input layer reaching its end: Ctrl+D on an
	// empty line, or a stdin that closed (a pipe whose writer went away, a
	// terminal that went away). It is the leg a chat run leaves on with NO
	// error and NO signal, and it was the one indistinguishable from /quit.
	SessionEndReasonEof = "eof"
	// SessionEndReasonQuit is the operator asking to leave (`/quit`, `/exit`).
	SessionEndReasonQuit = "quit"
	// SessionEndReasonSignalled is a terminating signal this process CAUGHT
	// and unwound through its normal close-out. The signal's own name rides
	// SessionEndFieldSignal beside it.
	SessionEndReasonSignalled = "signalled"
	// SessionEndReasonInputError is the input layer failing (a read error that
	// is not EOF): the run ends, but not because anyone asked it to.
	SessionEndReasonInputError = "input_error"
	// SessionEndReasonRunFailed is a failed turn: the close-out ran because the
	// run itself went wrong. The "error" block beside it carries the detail.
	SessionEndReasonRunFailed = "run_failed"
	// SessionEndReasonPanic is the recover path: the process is dying of a
	// panic and the close-out is running from the deferred collector. Distinct
	// from run_failed because a panic is a PROGRAM defect while a failed turn
	// is a provider/run outcome, and the two are triaged differently.
	SessionEndReasonPanic = "panic"
	// SessionEndReasonReplayAborted is the replay DRIVER stopping without the
	// watcher's quit — the failure arm of replayAbortExit.
	SessionEndReasonReplayAborted = "replay_aborted"
	// SessionEndReasonComplete is the run reaching its own natural end with no
	// close-out caller stating anything more specific (the one-shot face, the
	// non-persistent per-turn path, the web/term carriers). It says "the work
	// finished", which is NOT the same claim as "the operator quit".
	SessionEndReasonComplete = "complete"
	// SessionEndReasonNone is the value a caller passes when it genuinely does
	// not know the ending — the reason key is then written as
	// SessionEndReasonUnknown rather than omitted, so an absent key keeps its
	// single meaning: "this log was written by a binary that predates the
	// field".
	SessionEndReasonNone = ""
	// SessionEndReasonInterrupt is Ctrl+C at the PROMPT (no turn in flight): the
	// operator's key, arriving as the 0x03 byte through the input layer. It is
	// its own word because it is neither an EOF nor a signal — the run ended
	// cleanly and on purpose, and calling it "quit" would erase which key it was.
	SessionEndReasonInterrupt = "interrupt"
	// SessionEndReasonUnknown is what an unstated cause is recorded AS. It is
	// deliberately visible: a run that ends and cannot say why is the worst
	// outcome, and a record that admits it is strictly better than one that
	// silently reuses another leg's value.
	SessionEndReasonUnknown = "unknown"
)

// The SessionEndFieldEOFKind vocabulary — WHICH end of input it was, read only
// beside SessionEndReasonEof.
const (
	// SessionEndEOFKindKey is Ctrl+D on an empty line: a human asked to stop.
	SessionEndEOFKindKey = "key"
	// SessionEndEOFKindSource is the input source ending under the run (a
	// closed stdin, a pipe whose writer exited, a terminal that went away).
	SessionEndEOFKindSource = "source"
	// SessionEndEOFKindUnknown is end of input on a path that cannot tell the
	// two apart (the cooked reader has no key stream to ask). Said plainly
	// rather than guessed (Rule 15).
	SessionEndEOFKindUnknown = "unknown"
)
