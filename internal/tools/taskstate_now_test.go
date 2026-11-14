package tools

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// TestTaskStateNowDistinctFromZombieAge pins the DISPLAY probe against the
// zombie detector's age-based renderer: the state a row renders must come
// from the row's EVIDENCE, not from how long it has been sitting. A row whose
// session id does not resolve to any session file on this host (a foreign /
// deleted / never-stamped session — exactly the writeback-gap zombie shape)
// has no session evidence and no claim pid to probe, so it must render
// unknown with the MISSING signal named, never the elapsed "running" story.
func TestTaskStateNowDistinctFromZombieAge(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	restore := stubLiveHolderProbe(t, false)
	defer restore()
	now := time.Now()
	zombie := &Task{
		Status: taskStatusInProgress, SessionID: "no-such-session-on-this-host",
		CreatedAt: float64(now.Add(-7 * time.Hour).Unix()),
	}
	got := taskInProgressStateNow(zombie, now)
	if !strings.HasPrefix(got, taskStateUnknown+taskStateSeparator) {
		t.Fatalf("a row with a session id that resolves to no file must be unknown (<reason>), got %q", got)
	}
	if want := taskStateUnknown + taskStateSeparator + taskStateUnknownNoClaim; got != want {
		t.Errorf("state = %q, want %q", got, want)
	}
	// The rendered suffix must not claim a running window for that row.
	if suffix := taskStateSuffix(zombie, now); strings.Contains(suffix, "since ") {
		t.Errorf("suffix = %q — an unverifiable row must not render the running story", suffix)
	}
}

// TestTaskStateNowLiveRowIsProbeDriven pins the LIVE branch end to end: a row
// whose claim stamp names a live, freshness-passing holder renders the
// pre-existing running label with its elapsed window — the branch the
// production probe (pidAlive + the pid-reuse guard) drives.
func TestTaskStateNowLiveRowIsProbeDriven(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	restore := stubLiveHolderProbe(t, true)
	defer restore()
	now := time.Now()
	live := &Task{
		Status: taskStatusInProgress, ClaimedBy: taskStateTestHolder(os.Getpid()),
		HasClaim: true, ClaimedAt: float64(now.Add(-90 * time.Minute).Unix()),
		HasToolcall: true, LastToolcallAt: float64(now.Add(-time.Minute).Unix()),
		CreatedAt: float64(now.Add(-2 * time.Hour).Unix()),
	}
	if got := taskInProgressStateNow(live, now); got != taskStateRunning {
		t.Errorf("state = %q, want %q", got, taskStateRunning)
	}
	if suffix := taskStateSuffix(live, now); !strings.Contains(suffix, taskStateRunning) || !strings.Contains(suffix, "since ") {
		t.Errorf("suffix = %q, want the running label with its elapsed window", suffix)
	}
}

// TestTaskStateNowFinishedNeedsBothFacts pins the finished-but-unrecorded
// verdict as a TWO-signal claim: the verdict requires the session's terminal
// event AND a recycled worktree, and the label carries both facts so the
// operator can see what the row's verdict rests on.
func TestTaskStateNowFinishedNeedsBothFacts(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	restore := stubLiveHolderProbe(t, false)
	defer restore()
	now := time.Now()
	taskStateTestSessionJSONL(t, "sess-done", sessionevent.EventTypeSessionStart, sessionevent.EventTypeSessionEnd)
	done := &Task{
		Status: taskStatusInProgress, SessionID: "sess-done",
		Deliverable: `{"worktree":` + `"` + sandbox + `/already-recycled"}`,
		CreatedAt:   float64(now.Add(-8 * time.Hour).Unix()),
		HasToolcall: true, LastToolcallAt: float64(now.Add(-7 * time.Hour).Unix()),
	}
	if _, err := os.Stat(sandbox + "/already-recycled"); err == nil {
		t.Fatal("fixture worktree must not exist — the verdict requires WorktreeGone")
	}
	got := taskInProgressStateNow(done, now)
	if !strings.HasPrefix(got, taskStateFinished) || !strings.Contains(got, "worktree gone") {
		t.Errorf("state = %q, want the finished label naming the recycled worktree", got)
	}
}
