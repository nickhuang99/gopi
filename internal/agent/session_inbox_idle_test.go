package agent

// session_inbox_idle_test.go — the IDLE half of `gopi notify --session <id>
// --message <text>`: a delivery must become the NEXT TURN of a console that is
// sitting at its prompt with nobody typing, which is what
// SessionInboxIdleLoad/SessionInboxIdlePoll add.
//
// WHY THE CASES LIVE HERE AND NOT ONLY IN cmd/agent. The end-to-end case at the
// CLI level proves the whole path; it cannot show WHY the path works, and the
// two facts that make it work are both in this package:
//
//  1. the idle load merges the durable inbox into the ONE queue WITHOUT taking
//     from it (the take point must stay where it is — the turn's round
//     boundary), and
//  2. it raises an edge the parked reader can select on, because a
//     cross-process delivery can raise no in-process edge by itself.
//
// A case that asserted only "the queue became non-empty" would pass with the
// edge missing, and a console parked in a `select` would then never look.

import (
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// sessionInboxIdleTestModel is the model name the run's own session record
	// is opened for (the logger only stamps it; no request is made here).
	sessionInboxIdleTestModel = "deepseek-v4-pro"
	// sessionInboxIdleTestMessage is the delivered text. Distinctive so that
	// "the queue holds the delivery" cannot be satisfied by anything else.
	sessionInboxIdleTestMessage = "NOTIFY-IDLE-7b34: delivered to a session nobody is typing into"
	// sessionInboxIdleTestWait bounds every wait below: the poller's own cadence
	// is sessionInboxIdleLoadInterval, so the deadline is a small multiple of
	// what the poll must reach on its own — never a load generator (Rule 27).
	sessionInboxIdleTestWait = 30 * time.Second
	// sessionInboxIdleTestStep is the poll cadence of the waits.
	sessionInboxIdleTestStep = 25 * time.Millisecond
)

// drainSessionInboxIdleWake empties the idle edge so a case starts from a known
// state (a token left by another case would make the wake assertion pass for
// the wrong reason).
func drainSessionInboxIdleWake() {
	for {
		select {
		case <-SessionInboxIdleWake():
		default:
			return
		}
	}
}

// idleInboxAgent opens a real session record under an isolated profile and
// returns the agent plus the session directory a delivery resolves to. It is
// the SAME resolution the CLI uses (the directory of the log the run writes),
// so the case cannot pass against a directory the run does not own.
func idleInboxAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	t.Setenv("GOPI_HOME", t.TempDir())
	sl, err := NewSessionLogger(sessionInboxIdleTestModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() {
		if cerr := sl.Close(); cerr != nil {
			t.Errorf("close session log: %v", cerr)
		}
	})
	return &Agent{SessionLog: sl}, sessionlayout.RecordContainer(sl.Path())
}

// TestSessionInboxIdleLoadMergesWithoutTaking is the idle half's own contract:
// it makes a pending delivery VISIBLE to an idle reader and leaves the take
// where it has always been.
func TestSessionInboxIdleLoadMergesWithoutTaking(t *testing.T) {
	drainSessionInboxIdleWake()
	tools.TakeJobNotices()
	t.Cleanup(func() { tools.TakeJobNotices() })

	ag, dir := idleInboxAgent(t)

	// A session with no inbox at all: nothing pending, nothing raised. The
	// steady state of every ordinary console is exactly this call, and it must
	// be silent (the "no inbox" path is one stat).
	if pending := ag.SessionInboxIdleLoad(); pending {
		t.Error("an idle load reported a pending notice on a session that has no inbox at all")
	}
	if tools.JobNoticePending() {
		t.Error("an idle load on a session with no inbox left the queue non-empty")
	}
	select {
	case <-SessionInboxIdleWake():
		t.Error("an idle load with nothing delivered raised the idle edge — a reader would wake for no reason")
	default:
	}

	// THE DELIVERY, made the way the CLI makes it: through internal/tools, into
	// the session's own directory, from a caller that is not this process (the
	// case simply does not have this process's queue in hand — that is the whole
	// difference the idle half exists for).
	if _, err := tools.SessionInboxDeliver(dir, "", sessionInboxIdleTestMessage, ""); err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}

	if pending := ag.SessionInboxIdleLoad(); !pending {
		t.Fatal("the idle load did not report the delivery waiting in the file — an idle console would never wake for it")
	}
	if !tools.JobNoticePending() {
		t.Fatal("the idle load reported a pending notice but the LEVEL (tools.JobNoticePending) is false — the two answers to one question disagree")
	}
	select {
	case <-SessionInboxIdleWake():
	default:
		t.Fatal("the idle edge was not raised for a delivery that made the level true — a parked select would never look")
	}

	// THE TAKE POINT IS UNTOUCHED: the notice is still in the queue, and it
	// leaves through the ONE drain the round boundary owns.
	notices := tools.TakeJobNotices()
	if len(notices) != 1 {
		t.Fatalf("the queue holds %d notice(s), want exactly the 1 delivery — the idle load must merge, never take", len(notices))
	}
	if notices[0].External == nil || notices[0].External.Message != sessionInboxIdleTestMessage {
		t.Fatalf("the queued notice = %+v, want the delivered message", notices[0])
	}

	// AT-MOST-ONCE: the message has now been injected once, so a further idle
	// load must not hand it over again (the marker on disk is what says so).
	if pending := ag.SessionInboxIdleLoad(); pending {
		t.Error("a second idle load re-delivered an already marked message — the instruction would run twice")
	}
}

// TestSessionInboxIdlePollCarriesADeliveryThatArrivesWhileIdle pins the POLLER,
// not the one-shot call: the delivery is made AFTER the poll loop is running, so
// no direct call can be the thing that carried it — the ticker is.
func TestSessionInboxIdlePollCarriesADeliveryThatArrivesWhileIdle(t *testing.T) {
	drainSessionInboxIdleWake()
	tools.TakeJobNotices()
	t.Cleanup(func() { tools.TakeJobNotices() })

	ag, dir := idleInboxAgent(t)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ag.SessionInboxIdlePoll(stop)
	}()
	defer func() {
		close(stop)
		<-done
	}()

	// The console is now idle with the poller running. The delivery arrives
	// after this line, exactly as an operator's peer would make it.
	if _, err := tools.SessionInboxDeliver(dir, "", sessionInboxIdleTestMessage, ""); err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}

	deadline := time.Now().Add(sessionInboxIdleTestWait)
	for !tools.JobNoticePending() {
		if time.Now().After(deadline) {
			t.Fatalf("the idle poller did not merge the delivery within %s (its cadence is %s)",
				sessionInboxIdleTestWait, sessionInboxIdleLoadInterval)
		}
		time.Sleep(sessionInboxIdleTestStep) //stylegate:allow-sleep bounded wait for the poller's own tick, with sessionInboxIdleTestWait owning the deadline
	}

	// The edge, not just the level: the poller must have signalled a PARKED
	// reader. It is read after the tick that merged, so the token is either in
	// the buffer or already consumed by nobody — a lost token here is the
	// bounded-staleness case the level re-check covers, and the reader that
	// follows this channel asks the level on the way in.
	select {
	case <-SessionInboxIdleWake():
	case <-time.After(sessionInboxIdleTestWait):
		t.Fatal("the idle poller merged the delivery but never raised the idle edge — a parked console would keep waiting")
	}
}
