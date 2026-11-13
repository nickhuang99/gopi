package tools

// Regression suite for the TWO defects the independent verification
// (devdoc/task-list-state-honesty-verification-20260912.md, commit 01de5d70)
// reproduced on the merged state, both of them on THIS host's own ledger
// rows. The rework keeps the fix's intent and closes the two ladder holes:
//
//   DEFECT 1 — a row claimed by THIS host whose pid is DEAD rendered
//     "unknown: claim stamp names another host": taskClaimUnverifiableReason's
//     final fallback read the pid-probe's DEAD vote as "foreign host". The
//     knock-on was that "died" became unreachable for any realistically
//     claimed row (a current claimant's stamp stays FRESH when its process is
//     killed, so the stale escape hatch never fires), while the fix's own
//     died fixture used a CLAIMLESS row — a shape no worker row has.
//
//   DEFECT 2 — a row whose claim stamp names ANOTHER host rendered "died"
//     whenever its session JSONL happened to be readable here, because the
//     session-only layer sat ABOVE the claim layer. That contradicts the
//     fix's own three-times-stated promise: a foreign pid is unknowable on
//     this host and must never be called dead.
//
// Every case below is driven through the PRODUCTION entry point
// (taskInProgressStateNow / taskStateSuffix), with NO stub on the pid layer
// where the case is about the pid layer's OWN vote.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionevent"
)

// taskStateTestDeadPid resolves a pid that is guaranteed NOT to be a live
// executor: a high pid the kernel does not use. It is asserted dead (through
// the production pid probe) before it is used as a fixture, so a claim stamp
// built from it really exercises the DEAD vote — a fixture that silently
// resolved to a live pid would make the defect-1 assertions vacuous.
func taskStateTestDeadPid(t *testing.T) int {
	t.Helper()
	for pid := 1 << 30; pid < (1<<30)+64; pid++ {
		if !pidAlive(pid) {
			return pid
		}
	}
	t.Fatalf("no dead pid found above %d — cannot build the dead-holder fixture", 1<<30)
	return 0
}

// taskStateTestStaleHeartbeat stamps the long-stale ledger heartbeat every
// "nobody is running this" fixture needs (the display's own stall-grace
// threshold decides the age, so the fixture stays independent of config).
func taskStateTestStaleHeartbeat(tk *Task, now time.Time) *Task {
	tk.HasToolcall = true
	tk.LastToolcallAt = float64(now.Add(-6 * time.Hour).Unix())
	tk.CreatedAt = float64(now.Add(-7 * time.Hour).Unix())
	return tk
}

// TestReworkDefect1OwnHostDeadPidIsNotForeign is the defect-1 red proof, on
// the verification's own row shape (live ledger row 01a09573:
// claimed_by=<this host>:<dead pid>, hostname==this host, /proc/<pid> absent,
// FRESH stamp). The rendered reason must never claim a foreign host.
func TestReworkDefect1OwnHostDeadPidIsNotForeign(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	now := time.Now()
	dead := taskStateTestDeadPid(t)

	// The claim stamp is minted ON THIS HOST (taskStateTestHolder uses
	// os.Hostname(), the same value taskDefaultClaimHolder emits), the pid is
	// dead, and the stamp is FRESH for the row's in_progress transition — the
	// shape of a worker that was killed. The production probe is bound, so
	// the assertion contradicts the production pid-layer vote if it fires.
	row := taskStateTestStaleHeartbeat(&Task{
		Status: taskStatusInProgress, ClaimedBy: taskStateTestHolder(dead), HasClaim: true,
		ClaimedAt: float64(now.Add(-4 * time.Minute).Unix()),
		// Session evidence stays empty on purpose: the row must be settled by
		// the CLAIM layer, exactly as the live row was.
	}, now)
	row.HasInProgressAt = true
	row.InProgressAt = float64(now.Add(-4 * time.Minute).Unix())

	if freshness := claimStampFreshness(row); freshness != claimStampFresh {
		t.Fatalf("fixture broken: a killed current claimant keeps a FRESH stamp (got %q) — this is the knock-on the verification named", freshness)
	}
	got := taskInProgressStateNow(row, now)
	if strings.Contains(got, taskStateUnknownForeignHost) {
		t.Fatalf("DEFECT 1: own-host dead pid rendered %q — the stamp names THIS host (%s), the pid-probe voted DEAD",
			got, taskStateTestHolder(dead))
	}
	if got != taskStateDied {
		t.Fatalf("own-host dead pid = %q, want %q (a decidable kill/crash on this host)", got, taskStateDied)
	}
}

// TestReworkDefect1OwnHostDeadPidWithReadableSession is the second half of
// defect 1: the same own-host dead holder, now with a READABLE session that
// never wrote its terminal event. The claim layer already knows the process
// is gone, so the verdict is the same "died" — never the foreign-host reason.
func TestReworkDefect1OwnHostDeadPidWithReadableSession(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	now := time.Now()
	dead := taskStateTestDeadPid(t)
	taskStateTestSessionJSONL(t, "sess-own-dead", sessionevent.EventTypeSessionStart)

	row := taskStateTestStaleHeartbeat(&Task{
		Status: taskStatusInProgress, SessionID: "sess-own-dead",
		ClaimedBy: taskStateTestHolder(dead), HasClaim: true,
		ClaimedAt: float64(now.Add(-4 * time.Minute).Unix()),
	}, now)

	// Prove the session layer really resolved (the verification's own trap:
	// an unresolved fixture silently falls through the ladder).
	if st := taskProbeRowState(row, now); !st.SessionTailKnown || st.SessionEnd {
		t.Fatalf("fixture broken: session must resolve and carry no terminal event (tailKnown=%v end=%v)", st.SessionTailKnown, st.SessionEnd)
	}
	if got := taskInProgressStateNow(row, now); got != taskStateDied {
		t.Errorf("own-host dead pid + silent session = %q, want %q", got, taskStateDied)
	}

	// The rendered tail must carry the same verdict.
	if suffix := taskStateSuffix(row, now); !strings.Contains(suffix, taskStateDied) {
		t.Errorf("suffix = %q, want the died label", suffix)
	}
}

// TestReworkDefect1ForeignHostIsUnknown covers the shape defect 1's fallback
// was written for: a stamp whose pid this host can never probe. With a
// readable, silent session the row must STILL render unknown with the
// foreign-host reason — the signal that is missing, never a guess.
func TestReworkDefect1ForeignHostIsUnknown(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	now := time.Now()
	taskStateTestSessionJSONL(t, "sess-foreign-silent", sessionevent.EventTypeSessionStart)

	row := taskStateTestStaleHeartbeat(&Task{
		Status: taskStatusInProgress, SessionID: "sess-foreign-silent",
		ClaimedBy: "some-other-host:4242", HasClaim: true,
		ClaimedAt: float64(now.Add(-4 * time.Minute).Unix()),
	}, now)

	got := taskInProgressStateNow(row, now)
	want := taskStateUnknown + taskStateSeparator + taskStateUnknownForeignHost
	if got != want {
		t.Errorf("foreign-host row with a readable silent session = %q, want %q", got, want)
	}
}

// TestReworkDefect2ForeignHostReadableSessionIsNeverDied is DEFECT 2's red
// proof, through the production entry point. The session JSONL IS readable on
// this host and carries NO terminal event (the shape the verification
// reproduced: "other-host:4242" -> "died"), and the row records no worktree —
// i.e. the worktree-held hatch does not apply. A row whose holder lives on
// another machine must never be asserted dead from evidence gathered here.
func TestReworkDefect2ForeignHostReadableSessionIsNeverDied(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	now := time.Now()
	taskStateTestSessionJSONL(t, "sess-other-host", sessionevent.EventTypeSessionStart)

	row := taskStateTestStaleHeartbeat(&Task{
		Status: taskStatusInProgress, SessionID: "sess-other-host",
		ClaimedBy: "other-host:4242", HasClaim: true,
		ClaimedAt: float64(now.Add(-4 * time.Minute).Unix()),
	}, now)

	st := taskProbeRowState(row, now)
	// Fixture proof, mirroring the verification's own trap note: the session
	// must really resolve, carry no end, and record no worktree.
	if !st.SessionTailKnown || st.SessionEnd {
		t.Fatalf("fixture broken: session must resolve silently (tailKnown=%v end=%v)", st.SessionTailKnown, st.SessionEnd)
	}
	if st.Worktree != "" {
		t.Fatalf("fixture broken: no worktree must be recorded (the case is about the session-only layer), got %q", st.Worktree)
	}
	got := taskInProgressStateNow(row, now)
	if got == taskStateDied {
		t.Fatalf("DEFECT 2: foreign-host row rendered %q from session evidence gathered on this host", got)
	}
	if want := taskStateUnknown + taskStateSeparator + taskStateUnknownForeignHost; got != want {
		t.Errorf("foreign-host row = %q, want %q", got, want)
	}
	if suffix := taskStateSuffix(row, now); strings.Contains(suffix, taskStateDied) {
		t.Errorf("suffix = %q — a foreign-host row must never render the died label", suffix)
	}
}

// TestReworkClaimedDiedDistinguishableFromFinishedAndRunning is the epic's
// core table re-asserted on CURRENTLY-CLAIMED rows (the shape the original
// suite never built): an own-host claim with a dead pid ("died"), a
// session_end + recycled worktree row ("finished-but-unrecorded"), and a row
// whose holder is demonstrably alive ("running") must render three distinct
// labels — and none of the three may render an "unknown (…)" reason.
func TestReworkClaimedDiedDistinguishableFromFinishedAndRunning(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	now := time.Now()
	dead := taskStateTestDeadPid(t)

	restore := stubLiveHolderProbe(t, false)
	defer restore()

	deadRow := taskStateTestStaleHeartbeat(&Task{
		Status: taskStatusInProgress, ClaimedBy: taskStateTestHolder(dead), HasClaim: true,
		ClaimedAt:       float64(now.Add(-4 * time.Minute).Unix()),
		HasInProgressAt: true, InProgressAt: float64(now.Add(-4 * time.Minute).Unix()),
	}, now)

	taskStateTestSessionJSONL(t, "sess-finished", sessionevent.EventTypeSessionStart, sessionevent.EventTypeSessionEnd)
	finishedRow := taskStateTestStaleHeartbeat(&Task{
		Status: taskStatusInProgress, SessionID: "sess-finished",
		Deliverable: `{"repo":"/repo","worktree":"` + filepath.Join(sandbox, "recycled") + `"}`,
	}, now)

	liveRow := &Task{
		Status: taskStatusInProgress, ClaimedBy: taskStateTestHolder(os.Getpid()), HasClaim: true,
		ClaimedAt:   float64(now.Add(-2 * time.Hour).Unix()),
		HasToolcall: true, LastToolcallAt: float64(now.Add(-time.Minute).Unix()),
		CreatedAt: float64(now.Add(-2 * time.Hour).Unix()),
	}

	// The dead/finished rows are evaluated FIRST, with the live-holder probe
	// held OFF: a stub answering "alive" for the dead holder would make the
	// death assertion below vacuous (the probe is process-global, so the ON
	// stub is bound only for the live row, which is the ONLY case that needs
	// it — a test binary is not named `gopi`, so the LIVE branch is reachable
	// from a test only through the seam).
	labels := map[string]string{
		"finished": taskInProgressStateNow(finishedRow, now),
		"died":     taskInProgressStateNow(deadRow, now),
	}
	restoreLive := stubLiveHolderProbe(t, true)
	defer restoreLive()
	labels["running"] = taskInProgressStateNow(liveRow, now)
	if labels["running"] != taskStateRunning {
		t.Errorf("running row = %q, want %q", labels["running"], taskStateRunning)
	}
	if labels["finished"] != taskStateFinished+taskStateWorktreeSuffix {
		t.Errorf("finished row = %q, want %q", labels["finished"], taskStateFinished+taskStateWorktreeSuffix)
	}
	if labels["died"] != taskStateDied {
		t.Errorf("died row = %q, want %q", labels["died"], taskStateDied)
	}
	seen := map[string]string{}
	for name, label := range labels {
		if strings.HasPrefix(label, taskStateUnknown) {
			t.Errorf("%s row rendered an unverifiable label %q — this shape is decidable", name, label)
		}
		if prev, dup := seen[label]; dup {
			t.Errorf("%s and %s render the SAME label %q — states must be distinguishable", prev, name, label)
		}
		seen[label] = name
	}
}

// TestReworkReasonVocabularyNamesTheMissingSignal pins the reason vocabulary
// on the three unverifiable shapes an operator actually meets, so the reason
// can never be recycled for a situation it does not describe.
func TestReworkReasonVocabularyNamesTheMissingSignal(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	now := time.Now()

	cases := []struct {
		name string
		row  *Task
		want string
	}{
		{
			name: "no claim stamp at all",
			row:  &Task{Status: taskStatusInProgress, CreatedAt: float64(now.Add(-9 * time.Hour).Unix())},
			want: taskStateUnknown + taskStateSeparator + taskStateUnknownNoClaim,
		},
		{
			name: "stamp predates the row start (previous claimant)",
			row: taskStateTestStaleHeartbeat(&Task{Status: taskStatusInProgress,
				ClaimedBy: taskStateTestHolder(os.Getpid()), HasClaim: true,
				ClaimedAt:       float64(now.Add(-2 * time.Hour).Unix()),
				HasInProgressAt: true, InProgressAt: float64(now.Add(-time.Minute).Unix())}, now),
			want: taskStateUnknown + taskStateSeparator + taskStateUnknownStaleClaim,
		},
		{
			name: "stamp minted on another host",
			row: taskStateTestStaleHeartbeat(&Task{Status: taskStatusInProgress,
				ClaimedBy: "some-other-host:4242", HasClaim: true,
				ClaimedAt: float64(now.Add(-4 * time.Minute).Unix())}, now),
			want: taskStateUnknown + taskStateSeparator + taskStateUnknownForeignHost,
		},
	}
	for _, c := range cases {
		if got := taskInProgressStateNow(c.row, now); got != c.want {
			t.Errorf("%s = %q, want %q", c.name, got, c.want)
		}
	}

	// Malformed stamps carry no pid at all: they are unknown for the CLAIM
	// reason (nothing to probe here), never "died" and never foreign.
	for _, bad := range []string{"   ", "no-colon-here", "host:", "host:notanumber", ":-5"} {
		row := taskStateTestStaleHeartbeat(&Task{Status: taskStatusInProgress,
			ClaimedBy: bad, HasClaim: true, ClaimedAt: float64(now.Add(-4 * time.Minute).Unix())}, now)
		got := taskInProgressStateNow(row, now)
		if got == taskStateDied {
			t.Errorf("malformed stamp %q rendered %q — an unattributable holder is never an assertion of death", bad, got)
		}
		if !strings.HasPrefix(got, taskStateUnknown+taskStateSeparator) {
			t.Errorf("malformed stamp %q = %q, want unknown (<reason>)", bad, got)
		}
	}
}

// TestReworkOwnHostLivePidButForeignCommIsUnknown pins the PID-REUSE shape the
// original suite dodged (it built the case STALE, so the pid layer never
// voted): the stamp is FRESH and names THIS host, and the pid IS alive — but
// /proc/<pid>/comm is not a gopi runner, so the pid-reuse guard refuses to
// call it our executor.
//
// The rework's binding invariant here is NEGATIVE and it is the one defect 1
// violated: whenever the claim layer actually VOTED on this host
// (aliveChecked), the renderer must never fall back to "claim stamp names
// another host" — the stamp names THIS host, whatever the vote was. The
// positive verdict (died vs unknown) stays the ladder's business: the probe's
// own witness (pidAlive resolves EPERM to "alive", the comm guard then refuses
// the name) makes a live-but-unattributable pid indistinguishable from a
// recycled one, and the ladder treats an unattributable vote as death
// (taskInProgressState's aliveChecked-&&-!alive branch).
func TestReworkOwnHostLivePidButForeignCommIsUnknown(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	now := time.Now()

	// NO stub may be bound here: this case is ABOUT the pid layer's own vote.
	restore := stubLiveHolderProbe(t, false)
	defer restore()

	// The test binary itself is a live pid whose comm is NOT a gopi runner.
	recycled := os.Getpid()
	if taskPidIsRunner(recycled) {
		t.Skipf("test binary comm unexpectedly matches %q — pick another pid", taskAliveProcessNamePrefix)
	}
	row := taskStateTestStaleHeartbeat(&Task{
		Status: taskStatusInProgress, ClaimedBy: taskStateTestHolder(recycled), HasClaim: true,
		ClaimedAt:       float64(now.Add(-4 * time.Minute).Unix()),
		HasInProgressAt: true, InProgressAt: float64(now.Add(-4 * time.Minute).Unix()),
	}, now)

	// Fixture proof: the claim layer really accepted this stamp and voted, so
	// the assertion below constrains the LADDER, not an unprobed shape.
	if _, _, ok := claimHolder(row.ClaimedBy); !ok || claimStampFreshness(row) != claimStampFresh {
		t.Fatalf("fixture broken: the pid layer must accept this stamp (ok=%v fresh=%q)", ok, claimStampFreshness(row))
	}
	got := taskInProgressStateNow(row, now)
	if got == taskStateRunning {
		t.Fatalf("a pid whose comm is not %q must not render %q", taskAliveProcessNamePrefix, taskStateRunning)
	}
	if strings.Contains(got, taskStateUnknownForeignHost) {
		t.Fatalf("state = %q — the claim layer VOTED on this host, so the foreign-host reason is a false statement", got)
	}
	switch got {
	case taskStateDied, taskStateUnknown + taskStateSeparator + taskStateUnknownWorktreeHeld,
		taskStateUnknown + taskStateSeparator + taskStateUnknownNoSession:
	default:
		t.Errorf("state = %q, want a verdict or an unknown naming a signal this host really lacks", got)
	}
}

// TestReworkWorktreeHeldWithDeadHolderIsUnknown pins the hatch that survives
// in the rework: a dead holder whose run left a LIVE worktree on disk is
// "in-progress work nobody is running" — recoverable state, so the death
// verdict is withheld and the reason names the worktree.
func TestReworkWorktreeHeldWithDeadHolderIsUnknown(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	now := time.Now()
	dead := taskStateTestDeadPid(t)
	wt := filepath.Join(sandbox, "intact-worktree")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	row := taskStateTestStaleHeartbeat(&Task{
		Status: taskStatusInProgress, ClaimedBy: taskStateTestHolder(dead), HasClaim: true,
		ClaimedAt: float64(now.Add(-4 * time.Minute).Unix()),
		// Attached to this row's own execution (fresh stamp = the pid layer
		// really votes), and no session id: the claim layer alone decides.
		HasInProgressAt: true, InProgressAt: float64(now.Add(-4 * time.Minute).Unix()),
		Deliverable: fmt.Sprintf(`{"repo":"/repo","worktree":%q}`, wt),
	}, now)

	got := taskInProgressStateNow(row, now)
	if got == taskStateDied {
		t.Fatalf("a dead holder with an INTACT worktree must not render %q — the work is recoverable", got)
	}
	if want := taskStateUnknown + taskStateSeparator + taskStateUnknownWorktreeHeld; got != want {
		t.Errorf("dead holder + intact worktree = %q, want %q", got, want)
	}
}
