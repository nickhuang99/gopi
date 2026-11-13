package agent

// session_inbox.go — the agent side's half of `gopi notify --session <id>
// --message <text>`: the round-boundary LOAD of the durable inbox into the ONE
// in-process notice queue.
//
// WHAT IT DOES NOT DO, and this is the whole design. It does not read a
// message, it does not inject anything, it does not take from the queue and it
// does not add a channel: it merges the session's own inbox FILE into
// `theJobs.notices` (internal/tools/session_message.go) and returns. The take
// stays where it has always been — `tools.TakeJobNotices`, called by the round
// boundary two lines further down — so an external message and a background-job
// completion notice leave through the SAME take point and are injected at the
// SAME boundary, one message each, in the order they arrived.
//
// WHICH BOUNDARIES CALL IT: both of them (the chat turn's, chat_ops.go, and the
// task loop's, loop.go), immediately before their take. A session is a session
// whichever face is answering, and a message delivered to a session the task
// loop is running must not be visible only to the chat face.
//
// AND ONE MORE CALLER, WHICH IS NOT A BOUNDARY AT ALL: the IDLE console. A
// delivery is made by ANOTHER process, so it can raise no in-process edge — and
// a message that only became visible at a boundary the operator has not reached
// yet would wait for a keystroke that may never come. `SessionInboxIdlePoll`
// therefore runs the SAME merge on a ticker while the console is parked at its
// prompt (`SessionInboxIdleLoad`), and signals `SessionInboxIdleWake` so the
// parked `select` re-reads the LEVEL. That is what makes a delivered message the
// NEXT TURN by itself. It takes nothing and adds no queue: the notice still
// leaves through the one `tools.TakeJobNotices`, at the turn it opened.

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/tools"
)

// The IDLE half's two numbers (Rule 1).
const (
	// sessionInboxIdleLoadInterval is how often an IDLE console asks its own
	// inbox file whether something was delivered while it was parked at the
	// prompt.
	//
	// WHY AN INTERVAL EXISTS AT ALL, and why it is IN-PROCESS. The delivery is
	// made by ANOTHER process (`gopi notify --session … --message …`), so it
	// cannot signal this one: there is no channel between them and inventing one
	// would be a resident artifact with its own lifecycle (Rule 35). What this
	// process CAN do is look at its own inbox file — the same file the round
	// boundary already reads — which is what this ticker does.
	//
	// THE COST, stated rather than hidden: one goroutine and one `stat`+tail-read
	// of a file that is usually absent or tiny, every interval, for as long as the
	// console is open. Measured on a session with no inbox at all, the read
	// returns after a single `os.Stat` (SessionInboxLoad returns early when the
	// file does not exist), so the steady-state cost is one stat per interval.
	// Nothing is written, nothing is created, and no file outside the session
	// directory is touched.
	sessionInboxIdleLoadInterval = 2 * time.Second
	// sessionInboxIdleWakeBuf is the idle edge's buffer. ONE, for the same reason
	// jobNoticeWakeBuf is one: a pending token already means "the level may have
	// gone true" and the LEVEL is what the reader consults, so a second token
	// would carry no information the first did not.
	sessionInboxIdleWakeBuf = 1
)

// sessionInboxDir returns the directory this run's durable inbox lives in: the
// directory of the log this run actually opened.
//
// WHY THE LOG'S OWN PATH AND NOT sessionDirForID: the inbox must sit beside the
// session record the run is WRITING, and that path is the one fact about the
// session's location that cannot be stale — it was resolved once, when the log
// was opened, and every reader (gopilog, the task ledger, this) agrees with it.
// Re-deriving it here would be a second resolution of the same tree, free to
// drift from the file being written (Rules 2/13/14).
//
// ok=false when the run has no session log at all (an Agent{} built by a test,
// a run whose logging failed): there is then no inbox to read either, and a
// missing session log is not a delivery failure.
func (a *Agent) sessionInboxDir() (string, bool) {
	if a.SessionLog == nil {
		return "", false
	}
	p := strings.TrimSpace(a.SessionLog.Path())
	if p == "" {
		return "", false
	}
	return sessionlayout.RecordContainer(p), true
}

// loadSessionInbox merges every not-yet-delivered external message addressed to
// this session into the in-process notice queue. Call it at a round boundary
// BEFORE tools.TakeJobNotices, so the take below is the only reader.
//
// A failure is REPORTED and the boundary continues (Rule 15, never silent): the
// messages are still in the file, nothing was consumed, and the next boundary
// tries again — the honest degradation, because aborting a model turn over an
// inbox that could not be read would be a worse failure than a delivery that
// arrives one round later.
func (a *Agent) loadSessionInbox() {
	dir, ok := a.sessionInboxDir()
	if !ok {
		return
	}
	if _, err := tools.SessionInboxLoad(dir); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] session inbox: %v\n", err)
	}
}

// sessionInboxIdleWake is the IDLE EDGE: the in-process signal that says "the
// notice LEVEL may have gone true". It is a process-local channel, created by
// this package's own initialisation, never written to disk and needing no
// cleanup — the same argument internal/tools/job_notice_wake.go makes for its
// own channel (Rule 35: nothing outside the process can observe it, so no
// lifecycle has to be owned).
//
// WHY IT IS A SECOND CHANNEL AND NOT `tools.JobNoticeWake()`. That one is the
// TOOLS package's edge and its producer is the job reaper — a goroutine of THIS
// process. The idle poller below is the only party that can learn about a
// CROSS-PROCESS delivery, and it lives in the agent package, so it needs an edge
// of its own to break a park it did not cause. Neither channel carries a
// payload, and neither is a take point: both only say "re-read the LEVEL".
var sessionInboxIdleWake = make(chan struct{}, sessionInboxIdleWakeBuf)

// SessionInboxIdleWake is the channel an IDLE reader selects on, alongside its
// own input. Receiving from it means "the level may have become true" — the
// level itself is tools.JobNoticePending, and a receiver that skips that check
// will mint a turn out of a wake that no longer has anything behind it (the
// same bounded-staleness contract job_notice_wake.go states).
func SessionInboxIdleWake() <-chan struct{} { return sessionInboxIdleWake }

// notifySessionInboxIdleWaiters breaks the park of an idle reader. Safe to call
// from any goroutine, with or without a reader: it never blocks, and a full
// buffer IS a pending wake (the LEVEL is the message).
func notifySessionInboxIdleWaiters() {
	select {
	case sessionInboxIdleWake <- struct{}{}:
	default:
	}
}

// SessionInboxIdleLoad is the round boundary's OWN work, run while the console
// is IDLE: it merges this session's durable inbox into the ONE in-process notice
// queue and, when that made anything pending, signals the idle edge.
//
// WHY THE IDLE PROMPT NEEDS IT. The merge used to happen only at a round
// boundary — a point reachable from INSIDE a turn — and a turn only started
// when the operator typed a line. A delivery made by another process therefore
// sat on disk until the operator happened to type something, which is the cycle
// this closes: the console now consults its own inbox while it waits, so a
// `gopi notify --session <id> --message <text>` becomes the NEXT TURN by itself.
//
// IT ADDS NO TAKE POINT. It calls the SAME loadSessionInbox the boundaries call
// and takes nothing: the merge only appends to the queue, and the notice still
// leaves through the single `tools.TakeJobNotices` at the turn's own boundary,
// which injects it "BEFORE the request is built". A caller that has loaded must
// therefore still check the LEVEL (tools.JobNoticePending) before minting a
// turn — that is what the wake arm does.
//
// IT LIVES HERE, EXPORTED AND AGENT-NAMED, because TWO carriers idle: the REPL
// (cmd/agent/cli_chat.go) and any future console, and only the Agent knows where
// its own inbox is (sessionInboxDir). The alternative — the carrier resolving
// the inbox itself — would be a second resolution of the same tree, free to
// drift from the log the run is actually writing (Rules 2/13/14).
//
// A failure is REPORTED and returns false (Rule 15): the messages stay in the
// file, nothing was consumed, and the next tick tries again. It never panics and
// never blocks — the caller is a goroutine whose only job is to keep the console
// awake.
func (a *Agent) SessionInboxIdleLoad() bool {
	dir, ok := a.sessionInboxDir()
	if !ok {
		return false
	}
	if _, err := tools.SessionInboxLoad(dir); err != nil {
		// A damaged record is reported here as well as at the boundary: on an
		// idle console this is the ONLY place the operator could learn that a
		// delivery arrived unreadable, and silence would be the failure Rule 15
		// forbids.
		fmt.Fprintf(os.Stderr, "[gopi] session inbox: %v\n", err)
	}
	if !tools.JobNoticePending() {
		return false
	}
	notifySessionInboxIdleWaiters()
	return true
}

// SessionInboxIdlePoll is the idle console's OWN loop: it calls
// SessionInboxIdleLoad once per sessionInboxIdleLoadInterval until `stop` is
// closed. It is started by the carrier (cmd/agent/cli_chat.go) as a goroutine
// and ends with the session — the carrier closes the channel, this returns, and
// the ticker is stopped on the way out.
//
// WHY THE CARRIER DOES NOT OWN THE INTERVAL. The cadence and the merge are two
// halves of ONE decision ("how often does an idle console look at its own
// inbox?"), and the merge needs the Agent's own inbox path — so both live in one
// place, and a second carrier gets the same behaviour without restating either
// (Rules 2/13/14).
func (a *Agent) SessionInboxIdlePoll(stop <-chan struct{}) {
	ticker := time.NewTicker(sessionInboxIdleLoadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			a.SessionInboxIdleLoad()
		}
	}
}
