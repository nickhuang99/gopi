package scratch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// gc_lock_test.go — the lock decision's contract: an ABANDONED lock (its recorded
// holder is provably gone) must be broken in the SAME pass, and every case where
// the answer is not decidable must keep deferring (zero deletions).
//
// The red proof is TestGCLock_DecisionRestsOnLivenessNotMtimeAlone: with the
// pre-fix rule (mtime only) the first arm FAILS — the sweep defers to a lock
// whose holder died, which is exactly the measured defect this file closes.

// gcLockFixtureHome builds a home carrying one stale, collectable root plus the
// lock file the case wants, and returns (home, root, lockPath).
func gcLockFixtureHome(t *testing.T, lockContent string, lockAge time.Duration) (string, string, string) {
	t.Helper()
	home, dir := fixtureHome(t, "suite-ghost-run-lock", staleRootAge)
	lockPath := filepath.Join(home, GCLockFileName)
	if lockContent != gcLockAbsent {
		if err := os.WriteFile(lockPath, []byte(lockContent), FilePerm); err != nil {
			t.Fatalf("write lock: %v", err)
		}
		if lockAge > 0 {
			stamp := time.Now().Add(-lockAge)
			if err := os.Chtimes(lockPath, stamp, stamp); err != nil {
				t.Fatalf("age lock: %v", err)
			}
		}
	}
	return home, dir, lockPath
}

// gcLockAbsent marks a fixture that must NOT get a lock file at all.
const gcLockAbsent = "\x00absent"

// gcLockDeadPid returns a pid with no /proc entry on this host: it starts at the
// kernel's own pid ceiling and walks down until a number the kernel says is free.
// It never guesses "no such process" from an arbitrary number — the answer comes
// from the SAME probe the production decision uses (GCLockOwnerLivenessFor), so a
// test can never be red because the fixture picked a pid that happens to be live.
func gcLockDeadPid(t *testing.T) int {
	t.Helper()
	ceiling := 1 << 22
	raw, err := os.ReadFile("/proc/sys/kernel/pid_max")
	if err == nil {
		if n, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && n > 0 {
			ceiling = n
		}
	}
	for pid := ceiling; pid > ceiling-10000 && pid > 1; pid-- {
		if liveness, _ := GCLockOwnerLivenessFor(pid); liveness == GCLockOwnerGone {
			return pid
		}
	}
	t.Skip("no free pid above this host's process table — cannot build a dead-holder fixture")
	return 0
}

// TestGCLock_DeadHolderIsBrokenAndTheSweepRuns is the epic's core case: a lock
// whose recorded holder is DEAD must not be able to park the sweep. Before the
// fix the lock was released by mtime alone, so a fresh lock from a crashed GC
// deferred every pass forever while the CLI printed a successful `removed=0`.
func TestGCLock_DeadHolderIsBrokenAndTheSweepRuns(t *testing.T) {
	deadPid := gcLockDeadPid(t)
	home, dir, lockPath := gcLockFixtureHome(t, strconv.Itoa(deadPid)+"\n", 0)

	removed, verdict, err := SweepHomeDetailed(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 || exists(t, dir) {
		t.Fatalf("a dead holder's lock must not park the sweep: removed=%d rootExists=%v", removed, exists(t, dir))
	}
	if verdict.Outcome != GCLockOutcomeBrokeAbandoned {
		t.Fatalf("verdict must say the abandoned lock was broken, got %q (%s)", verdict.Outcome, verdict.Reason)
	}
	if !verdict.BrokeAbandoned {
		t.Errorf("BrokeAbandoned must be set on %q", verdict.Outcome)
	}
	if !verdict.HasOwnerPid || verdict.OwnerPid != deadPid {
		t.Errorf("verdict must name the dead holder pid %d, got hasPid=%v pid=%d",
			deadPid, verdict.HasOwnerPid, verdict.OwnerPid)
	}
	if !strings.Contains(verdict.Reason, strconv.Itoa(deadPid)) || !strings.Contains(verdict.Reason, GCLockFileName) {
		t.Errorf("the sentence must name the dead pid and the lock file, got %q", verdict.Reason)
	}
	// The evidence the CLI prints must carry the same facts (Rule 15: the
	// decision reaches the operator, not only the test).
	evidence := GCLockEvidence(verdict)
	for _, want := range []string{string(GCLockOutcomeBrokeAbandoned), "pid:" + strconv.Itoa(deadPid)} {
		if !strings.Contains(evidence, want) {
			t.Errorf("evidence %q must carry %q", evidence, want)
		}
	}
	if exists(t, lockPath) {
		t.Errorf("the sweep releases the lock it took: %s must be gone", lockPath)
	}
}

// TestGCLock_LiveHolderDefersAndNamesThePid is the conservative half: a lock held
// by a LIVE process must keep the sweep out, and the verdict must NAME that pid —
// this is the difference between "nothing to collect" and "another GC ran".
func TestGCLock_LiveHolderDefersAndNamesThePid(t *testing.T) {
	// This test process is alive for the whole run, so its pid is a live holder.
	live := os.Getpid()
	home, dir, _ := gcLockFixtureHome(t, strconv.Itoa(live)+"\n", 0)

	removed, verdict, err := SweepHomeDetailed(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 0 || !exists(t, dir) {
		t.Fatalf("a live holder must defer: removed=%d rootExists=%v", removed, exists(t, dir))
	}
	if verdict.Locked {
		t.Fatalf("the verdict must not claim the lock was taken: %+v", verdict)
	}
	if verdict.Outcome != GCLockOutcomeDeferredLive {
		t.Fatalf("outcome must be %q, got %q (%s)", GCLockOutcomeDeferredLive, verdict.Outcome, verdict.Reason)
	}
	if !verdict.HasOwnerPid || verdict.OwnerPid != live {
		t.Errorf("the defer sentence must name the live holder: hasPid=%v pid=%d want %d",
			verdict.HasOwnerPid, verdict.OwnerPid, live)
	}
	if !strings.Contains(verdict.Reason, strconv.Itoa(live)) {
		t.Errorf("reason must name pid %d, got %q", live, verdict.Reason)
	}
}

// TestGCLock_UnknownOwnerDefersAndDeletesNothing is the "unknown is never a
// licence to break" invariant, in both shapes the unknowns come in: a lock that
// records NO pid (empty or non-numeric — what a truncated write leaves), and a
// lock whose holder the kernel would not answer for. Both must defer, and the
// deferral must be marked as UNMEASURED rather than as "alive" — the two are
// different facts (Rule 15a).
func TestGCLock_UnknownOwnerDefersAndDeletesNothing(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"empty lock file", ""},
		{"non-numeric content", "not-a-pid\n"},
		{"zero pid", "0\n"},
		{"negative pid", "-4\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, dir, lockPath := gcLockFixtureHome(t, tc.content, 0)
			removed, verdict, err := SweepHomeDetailed(home, DefaultStaleAge)
			if err != nil {
				t.Fatalf("sweep: %v", err)
			}
			if removed != 0 || !exists(t, dir) {
				t.Fatalf("an unmeasurable holder must defer: removed=%d rootExists=%v", removed, exists(t, dir))
			}
			if verdict.Outcome != GCLockOutcomeDeferredUnknown {
				t.Fatalf("outcome must be %q, got %q (%s)", GCLockOutcomeDeferredUnknown, verdict.Outcome, verdict.Reason)
			}
			if verdict.HasOwnerPid {
				t.Errorf("no pid was readable, so HasOwnerPid must be false (got %d)", verdict.OwnerPid)
			}
			if exists(t, lockPath) == false {
				t.Errorf("the lock must still be there — a deferral breaks nothing")
			}
			if !strings.Contains(GCLockEvidence(verdict), string(GCLockOwnerUnknown)) &&
				!strings.Contains(GCLockEvidence(verdict), GCLockOwnerNone) {
				t.Errorf("evidence must mark the owner unmeasured/absent, got %q", GCLockEvidence(verdict))
			}
		})
	}
}

// TestGCLock_DecisionRestsOnLivenessNotMtimeAlone is the RED PROOF (Rule 5):
// with the pre-fix rule — the mtime valve alone — the FIRST arm of this test
// fails, because a FRESH lock whose holder is dead would be honoured and the
// stale root would survive.
//
// It drives the decision through the package seam so the liveness answer is the
// only variable: the lock file and its mtime are IDENTICAL across the three arms,
// and only the answer about the holder changes.
func TestGCLock_DecisionRestsOnLivenessNotMtimeAlone(t *testing.T) {
	realLiveness := gclockOwnerLiveness
	t.Cleanup(func() { gclockOwnerLiveness = realLiveness })

	// A pid the fixture writes into the lock. Its only role is to be PRESENT:
	// the arms below decide what the kernel's answer about it is.
	const fixturePid = 4711

	cases := []struct {
		name        string
		answer      GCLockOwnerLiveness
		wantOutcome GCLockOutcome
		wantRemoved int
	}{
		{"holder is gone — a fresh lock is still abandoned", GCLockOwnerGone, GCLockOutcomeBrokeAbandoned, 1},
		{"holder is live — the pass defers", GCLockOwnerLive, GCLockOutcomeDeferredLive, 0},
		{"holder is unmeasurable — the pass defers", GCLockOwnerUnknown, GCLockOutcomeDeferredUnknown, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gclockOwnerLiveness = func(pid int) (GCLockOwnerLiveness, string) {
				if pid != fixturePid {
					t.Errorf("the decision asked about pid %d, want %d", pid, fixturePid)
				}
				return tc.answer, "seam answer for the red proof"
			}
			// mtime = now, i.e. YOUNGER than GCLockStaleAge: the pre-fix rule
			// alone would honour this lock in every one of the three arms.
			home, dir, _ := gcLockFixtureHome(t, strconv.Itoa(fixturePid)+"\n", 0)

			removed, verdict, err := SweepHomeDetailed(home, DefaultStaleAge)
			if err != nil {
				t.Fatalf("sweep: %v", err)
			}
			if verdict.Outcome != tc.wantOutcome {
				t.Fatalf("outcome=%q want %q (%s)", verdict.Outcome, tc.wantOutcome, verdict.Reason)
			}
			if removed != tc.wantRemoved {
				t.Fatalf("removed=%d want %d (rootExists=%v)", removed, tc.wantRemoved, exists(t, dir))
			}
		})
	}
}

// TestGCLock_StaleValveStillBreaksAnUnmeasurableLock keeps the PRE-EXISTING
// contract intact: a lock older than GCLockStaleAge is broken whatever it records
// — including a lock that records nothing, which is the shape the original
// stale-age test writes (os.Create then Chtimes).
func TestGCLock_StaleValveStillBreaksAnUnmeasurableLock(t *testing.T) {
	home, dir, _ := gcLockFixtureHome(t, "", GCLockStaleAge+time.Minute)

	removed, verdict, err := SweepHomeDetailed(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 || exists(t, dir) {
		t.Fatalf("the stale-age valve must still break an unparsable lock: removed=%d rootExists=%v", removed, exists(t, dir))
	}
	if verdict.Outcome != GCLockOutcomeBrokeAbandoned {
		t.Fatalf("outcome=%q want %q (%s)", verdict.Outcome, GCLockOutcomeBrokeAbandoned, verdict.Reason)
	}
	if !strings.Contains(verdict.Reason, GCLockStaleAge.String()) {
		t.Errorf("the sentence must name the valve it used (%s), got %q", GCLockStaleAge, verdict.Reason)
	}
}

// TestGCLock_RetriesOnceAfterBreakingAbandoned pins the retry: the break is
// followed by a SECOND create attempt, so the sweep runs in the SAME pass rather
// than waiting a whole tick. The lock must be gone afterwards (released by the
// sweep), which is what "the retry succeeded" looks like from outside.
func TestGCLock_RetriesOnceAfterBreakingAbandoned(t *testing.T) {
	deadPid := gcLockDeadPid(t)
	home, _, lockPath := gcLockFixtureHome(t, strconv.Itoa(deadPid)+"\n", 0)

	release, verdict, err := AcquireGCLockVerdict(home)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !verdict.Locked || !verdict.BrokeAbandoned {
		t.Fatalf("the abandoned lock must be broken and retaken: %+v", verdict)
	}
	release()
	if exists(t, lockPath) {
		t.Errorf("release must remove the lock it took: %s still exists", lockPath)
	}
}

// TestGCLock_ParseOwnerPidSpellsTheUnmeasuredCase pins the reading half: a
// missing, blank, non-numeric or non-positive content is UNMEASURED, never a
// guessed pid (a zero that meant "not measured" once read as a value would let a
// caller break a lock on the strength of nothing).
func TestGCLock_ParseOwnerPidSpellsTheUnmeasuredCase(t *testing.T) {
	cases := []struct {
		in      string
		wantPid int
		wantOK  bool
	}{
		{"4242\n", 4242, true},
		{"  77  ", 77, true},
		{"", 0, false},
		{"\n", 0, false},
		{"abc", 0, false},
		{"0", 0, false},
		{"-1", 0, false},
	}
	for _, tc := range cases {
		pid, ok := ParseGCLockOwnerPid([]byte(tc.in))
		if ok != tc.wantOK || pid != tc.wantPid {
			t.Errorf("ParseGCLockOwnerPid(%q) = (%d,%v), want (%d,%v)", tc.in, pid, ok, tc.wantPid, tc.wantOK)
		}
	}
}
