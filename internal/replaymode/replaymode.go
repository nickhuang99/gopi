package replaymode

// replaymode — THE ONE PLACE THAT KNOWS THIS PROCESS IS RE-RUNNING A RECORDING
// (epic: `gopi chat --replay <session>` must be really read-only).
//
// THE DEFECT THIS PACKAGE CLOSES (measured 2026-10-12, real pty run of
// `gopi chat --replay bd3ecf46-…`). The replay already took the MODEL and the
// TOOL DISPATCH out of the run's hands (the in-process stub, and the agent's
// exported executor seam), and a previous task relocated the run's own SESSION
// into scratch (cmd/agent/chat_sessions_root.go). What was left untouched is
// every OTHER write a real REPL performs simply by being a real REPL, and one
// real run wrote all three of these into the OPERATOR's own profile:
//
//	~/.gopi/chat_session_id_2.txt    rewritten with the replay's own identity
//	~/.gopi/chat_history            +3840 bytes of recorded prompts appended
//	~/.gopi/memory.db  (chats)      a row for the replay's scratch session id
//
// None of that is a copied session — it is the operator's own index, history and
// ledger, edited by a run whose whole purpose is to draw a picture of a session
// that already happened. The keyboard layer (lineedit's hold) had closed only
// "a human's key changes the line"; the writes above come from the SURFACE
// being alive, not from anyone typing.
//
// WHY A GLOBAL FLAG AND NOT A PARAMETER. The writers are in three packages on
// four different code paths (the slot registry and the history path in
// cmd/agent, the chats-table hooks in internal/agent), none of which is a caller
// of the replay driver and none of which has a seam that reaches it. Threading a
// bool through all of them would be a second, silently different answer per
// path — which is exactly Rule 13's defect. So the STATE lives in one leaf
// package, this one, and every write path asks the SAME question: Active().
//
// WHAT THIS PACKAGE IS NOT. It is not a policy about WHAT a replay should do —
// it answers "am I replaying?" and nothing else. It holds no path, no config
// key and no environment variable (Rule 35: no new dependency, and no escape
// hatch — there is deliberately no way to arm this from outside the binary,
// because a flag that a caller could turn OFF would be a switch that defeats the
// guarantee). The one writer is the replay entry point (cmd/agent's startReplay)
// and the one clearing is its own cleanup.
//
// RULE 15 ON THE OTHER SIDE OF THE QUESTION. A write path that asks Active() and
// finds true must not go SILENT: every short-circuit below is announced in a
// sentence naming the write it did not perform, so a reader of a replay's stderr
// can count what the run refused. Silence here would make "the replay wrote
// nothing" and "the replay's writes went somewhere I did not look" the same
// observable — the failure mode this whole epic exists to end.

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// var (never a const): the state is per-PROCESS and mutable, and it is a single
// bool behind a mutex rather than an atomic so the source string and the flag are
// always read as one fact (a torn read of "armed, but whose reason?" would let a
// diagnostic name the wrong recording).
var (
	mu     sync.RWMutex
	armed  bool
	source string
	// noticeSink is where THIS process's replay diagnostics are written. It is
	// os.Stderr until a caller installs another destination, which is what a
	// replay run does: the screen it draws is the recording, and a `[gopi replay]
	// …` line landing there is a sentence FROM this run inside a picture of
	// ANOTHER session (measured 2026-11-04: a plain `gopi chat --replay <id>`
	// wrote 15 such lines to the operator's screen).
	//
	// It lives HERE, in the package that already answers "is this process
	// re-running a recording", because that is the criterion the destination
	// depends on (Rule 13: one question, one place). The writers it serves span
	// two packages and five files — a sink each of them invented would be five
	// definitions of "where a replay's diagnostics live".
	// nil means "os.Stderr, read at WRITE time". That is not the same as storing
	// os.Stderr here: a test (or a capturing caller) that swaps the os.Stderr
	// VARIABLE would then be invisible to every notice, which is the failure mode
	// that makes a capture-based test pass while the sentence goes elsewhere.
	noticeSink io.Writer
)

// NoticeSinkWriter reports the destination a replay diagnostic written RIGHT NOW
// should use. It resolves per call rather than returning a snapshot, so a route
// installed after a writer was handed out still takes effect.
func NoticeSinkWriter() io.Writer {
	mu.RLock()
	defer mu.RUnlock()
	if noticeSink == nil {
		return os.Stderr
	}
	return noticeSink
}

// SetNoticeSink points this process's replay diagnostics at w and returns the
// restore that puts the previous destination back. A nil w is REFUSED by name
// (the sink is left where it was): a route to nowhere would DELETE the
// diagnostics instead of moving them (Rule 15).
func SetNoticeSink(w io.Writer) (restore func()) {
	if w == nil {
		return func() {}
	}
	mu.Lock()
	prev := noticeSink
	noticeSink = w
	mu.Unlock()
	return func() {
		mu.Lock()
		noticeSink = prev
		mu.Unlock()
	}
}

// Noticef writes one replay diagnostic to the current sink, so a refusal spoken
// from internal/agent and one spoken from cmd/agent land in the same place. The
// words are not changed by this call — only where they land (the notice texts
// stay the constants they are).
func Noticef(format string, args ...any) {
	_, _ = fmt.Fprintf(NoticeSinkWriter(), format, args...) //stylegate:ignore a failed write to the run's own notice channel cannot be reported: the channel IS the report
}

// Arm marks this process as re-running a recording, and names the recording for
// every later diagnosis. It is idempotent: arming twice keeps the first source
// (a second recording in one process is not a shape this verb has, and silently
// overwriting the name would make the announcement point at the wrong run).
//
// It is called by the replay entry point the moment the recording is loaded —
// BEFORE the agent (and therefore the session logger, the surface and every
// writer it arms) is built — which is the ordering the whole guarantee rests on:
// a flag armed after the writer it is meant to stop has already run is prose.
func Arm(recording string) {
	mu.Lock()
	defer mu.Unlock()
	if armed {
		return
	}
	armed = true
	source = recording
}

// Disarm returns the process to its ordinary state. The replay's own cleanup
// calls it so the flag describes the run in flight rather than the process's
// history — a test process, or a caller that replays and then does something
// else, must not keep refusing writes for a run that has ended.
func Disarm() {
	mu.Lock()
	defer mu.Unlock()
	armed = false
	source = ""
}

// Active is THE question every write path asks: is this process re-running a
// recording? It is the ONE decision point (Rule 13/14) — no caller compares a
// mode string, checks a session path or re-derives the fact for itself.
func Active() bool {
	mu.RLock()
	defer mu.RUnlock()
	return armed
}

// Source names the recording this process is re-running, or "" when it is not.
// It exists so an announcement can say WHICH recording caused the refusal
// instead of only that something was refused.
func Source() string {
	mu.RLock()
	defer mu.RUnlock()
	return source
}

// Refuse is the ONE spelling of the short-circuit sentence: a write path that
// asked Active() and found true calls this to say WHAT it did not do and WHY, so
// the reason and the vocabulary cannot drift between the four sites (Rule 14).
// It returns a sentence (rather than printing) because the destination differs
// per site — a surface writes to stderr, an agent hook rides the run's own
// logger — and a package that chose one of them would be wrong at three sites.
func Refuse(what string) string {
	mu.RLock()
	src := source
	mu.RUnlock()
	return refusePrefix + what + ": " + Reason(src)
}

// Reason is the one statement of WHY a replay performs no write, and of WHAT a
// replay therefore does not produce. It exists because the sentence is now
// spoken at SEVEN sites and two of them are the mint of the run's own session
// (internal/agent's raw-log and session-JSONL constructors), which is where the
// claim has to be most exact: a replay neither appends to the recording it reads
// NOR opens a session tree of its own. The old tail — "writes nothing outside its
// own session tree" — described the period when a replay DID mint a discarded
// session tree in scratch (cmd/agent/chat_sessions_root.go, deleted); with that
// tree gone the wording would point a reader at an artifact that no longer
// exists (Rule 15: an announcement must describe what actually happened).
func Reason(src string) string {
	if src == "" {
		return "this process is re-running a recording, and a replay writes nothing at all: it reads the recording in place and mints no session of its own"
	}
	return "this process is re-running " + src + ", and a replay writes nothing at all: it reads that recording in place and mints no session of its own"
}

// refusePrefix is the ONE opening clause of every refusal (Rule 13): the
// vocabulary a reader greps for ("refused to …"), and the reason that follows it
// comes from Reason.
const refusePrefix = "[gopi replay] refused to "

// RefuseMarker is refusePrefix under an EXPORTED name, for callers that need to
// match the vocabulary rather than speak it — a test asserting "this line is one
// of the replay's refusals", or a reader counting them. It re-exports the ONE
// definition above instead of repeating the literal (Rule 13/14): a second copy
// would be the drift the spelling exists to prevent.
const RefuseMarker = refusePrefix
