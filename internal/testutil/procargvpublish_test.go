package testutil

// The regression guard for the ARGV PUBLICATION RACE — the defect that made
// TestProcIdentityRefusesASignalledRecycledPID flaky (~1/2 red, green on retry).
//
// The defect, in one sentence: a child read the instant it was spawned has an
// EMPTY /proc/<pid>/cmdline (the argv block is published after the kernel's
// task entry exists), so a CAPTURE that stored that emptiness later failed its
// own re-check — which read the published argv — and reported
// `pid N was REUSED: … starttime=<same> cmdline="sleep 300", was … cmdline=""`:
// a REUSE verdict whose two halves carry the SAME starttime, i.e. one
// incarnation. Measured 194/200 spawns (see ArgvUnpublished's comment and
// artifacts/capture-race-probe in the bug row's session dir).
//
// The guard has three arms, because the defect has three separable halves:
//
//  1. the RULE of the wait (deterministic, injected reader — no timing at all);
//  2. the TRUTH TABLE of "unpublished" (a zombie's empty cmdline is a DIFFERENT
//     fact and must NOT be waited on);
//  3. the CONTRACT on a LIVE, really-spawned child, over a bounded number of
//     rounds, with the race's frequency REPORTED rather than asserted (an
//     assertion on the window would reintroduce the load sensitivity being
//     fixed — the premise is an observation, the contract is the assertion).

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// procArgvGuardRounds bounds the live arm. Each round spawns one short-lived
	// mock and kills it, so the loop is bounded in processes, in time and in
	// load — never a saturation loop (Rule 27). The count is chosen so the arm
	// almost certainly MEETS the window (it is met on roughly one spawn in eight)
	// without making the suite pay for it.
	procArgvGuardRounds = 40
	// procArgvGuardPid is the pid the injected-reader arm pretends to read. It is
	// deliberately a pid this process will never be: the arm must be exercisable
	// with NO live process at all.
	procArgvGuardPid = 424242
	// procArgvGuardUnpublishedRounds is how many unpublished reads the synthetic
	// reader serves before publishing — enough to pin "it waits", small enough to
	// keep the arm instant.
	procArgvGuardUnpublishedRounds = 3
	// procArgvGuardNeverBudget is the budget the never-publishing arm gets: it
	// must be short, because the arm's point is the REFUSAL, not the wait.
	procArgvGuardNeverBudget = 20 * time.Millisecond
)

// TestProcArgvPublicationWaitRule pins the RULE of the wait with an injected
// reader: an unpublished read is retried, a published one is returned, and a
// child that never publishes is REFUSED (never returned half-filled).
//
// This arm is where the fix is red-on-revert WITHOUT any timing dependence:
// production returning its first read (or not refusing) fails it every run, on
// every host, under any load.
func TestProcArgvPublicationWaitRule(t *testing.T) {
	published := ProcIdentity{Pid: procArgvGuardPid, StartTime: 987654, Cmdline: "sleep 300", State: 'S'}
	calls := 0

	// (a) unpublished, unpublished, unpublished, then published → the wait
	// returns the PUBLISHED identity, after exactly that many reads.
	got, err := awaitProcIdentity(procArgvGuardPid, func(int) (ProcIdentity, error) {
		calls++
		if calls <= procArgvGuardUnpublishedRounds {
			return ProcIdentity{Pid: procArgvGuardPid, StartTime: published.StartTime, State: 'R'}, nil
		}
		return published, nil
	}, time.Second)
	if err != nil {
		t.Fatalf("the wait refused an identity that DID publish its argv: %v", err)
	}
	if !got.SameIncarnation(published) || got.Cmdline != published.Cmdline || got.State != published.State {
		t.Errorf("wait returned %+v, want the published identity %+v — an unpublished read must never be returned", got, published)
	}
	if calls != procArgvGuardUnpublishedRounds+1 {
		t.Errorf("the wait read %d times, want %d — it must poll until the argv is published", calls, procArgvGuardUnpublishedRounds+1)
	}

	// (b) a reader that never publishes → a REFUSAL naming the pid, never a
	// half-filled identity (the whole point: an empty cmdline stored as an
	// identity is what later reads as a false REUSE).
	never := 0
	_, err = awaitProcIdentity(procArgvGuardPid, func(int) (ProcIdentity, error) {
		never++
		return ProcIdentity{Pid: procArgvGuardPid, StartTime: published.StartTime, State: 'R'}, nil
	}, procArgvGuardNeverBudget)
	if err == nil {
		t.Fatal("a pid whose argv never publishes must be REFUSED, not returned as an empty-cmdline identity")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(procArgvGuardPid)) || !strings.Contains(err.Error(), procArgvGuardNeverBudget.String()) {
		t.Errorf("the refusal must name the pid and the budget, got %q", err)
	}
	if never < 2 {
		t.Errorf("the refusal read only %d times — a budget must not be spent on a single sample", never)
	}

	// (c) a read ERROR is propagated as itself, never converted into a wait: a
	// gone pid is gone, and polling for a publication that cannot come would
	// hide it.
	sentinel := errors.New("process is gone")
	_, err = awaitProcIdentity(procArgvGuardPid, func(int) (ProcIdentity, error) {
		return ProcIdentity{}, sentinel
	}, time.Second)
	if !errors.Is(err, sentinel) {
		t.Errorf("a read error must come back as itself, got %v", err)
	}
}

// TestProcIdentityArgvUnpublishedTruthTable pins the predicate the wait is built
// on, in BOTH directions — an empty cmdline is "unpublished" only while the
// process is not a zombie. Folding the two together would make the capture wait
// a full budget for a process that will never publish anything (a zombie: the
// kernel dropped its argument block), and would let a zombie's emptiness be read
// as a capture artifact (Rule 15a: two readings of one evidence are two facts,
// never one).
func TestProcIdentityArgvUnpublishedTruthTable(t *testing.T) {
	cases := []struct {
		name string
		id   ProcIdentity
		want bool
	}{
		{
			name: "empty cmdline on a RUNNING process — the measured capture window",
			id:   ProcIdentity{Pid: 1, State: 'R', Cmdline: ""},
			want: true,
		},
		{
			name: "empty cmdline on a SLEEPING process — same window, other state letter",
			id:   ProcIdentity{Pid: 1, State: 'S', Cmdline: ""},
			want: true,
		},
		{
			name: "empty cmdline on a ZOMBIE — the kernel dropped the args; this is NOT unpublished argv",
			id:   ProcIdentity{Pid: 1, State: 'Z', Cmdline: ""},
			want: false,
		},
		{
			name: "a published cmdline is never unpublished, whatever the state",
			id:   ProcIdentity{Pid: 1, State: 'R', Cmdline: "sleep 300"},
			want: false,
		},
	}
	for _, c := range cases {
		if got := c.id.ArgvUnpublished(); got != c.want {
			t.Errorf("%s: ArgvUnpublished() = %v, want %v (%+v)", c.name, got, c.want, c.id)
		}
	}
}

// TestProcIdentityAwaitNeverStoresAnUnpublishedArgv is the LIVE arm: a bounded
// number of really-spawned children are captured through the production entry
// point, and no capture may come back with an unpublished argv.
//
// The race's FREQUENCY is logged, never asserted: on this host the window is met
// on roughly one spawn in eight, and asserting "the window was met" would make
// this guard fail on a host (or under a load) where the read is simply slower —
// the exact class of false red the fix exists to remove.
//
// RED-ON-REVERT: with the capture reverted to a bare ReadProcIdentity (the shape
// this test was written against), the failing rounds are reported by pid with
// the empty cmdline that was stored, and the log line below names the round
// count that met the window.
func TestProcIdentityAwaitNeverStoresAnUnpublishedArgv(t *testing.T) {
	sawUnpublished, captured := 0, 0
	for i := 0; i < procArgvGuardRounds; i++ {
		mock := StartMockProcess(t, MockDaemonBinary, MockDaemonHoldSeconds)
		pid := mock.Pid()
		// The PREMISE, observed at the earliest possible instant (the bare read
		// is exactly what the capture used to store). Reported, not asserted.
		if raw, err := ReadProcIdentity(pid); err == nil && raw.ArgvUnpublished() {
			sawUnpublished++
		}
		id, err := AwaitProcIdentity(pid)
		if err != nil {
			t.Fatalf("round %d: capture of the just-spawned pid %d refused: %v", i, pid, err)
		}
		captured++
		if id.Cmdline == "" {
			t.Errorf("round %d: the captured identity of the just-spawned pid %d has an EMPTY cmdline (%s) — storing it makes the caller's own re-check report a REUSE verdict with a matching starttime", i, pid, id)
		}
		mock.Reap()
	}
	t.Logf("capture contract held on %d/%d spawns; the publication window was observed on %d of them (premise only — the contract is the assertion)",
		captured, procArgvGuardRounds, sawUnpublished)
}
