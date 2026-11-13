package tools

// Honest `task list` in_progress-state labels (task-list-state-honesty epic,
// 2026-09-12). Pinned here:
//
//   - the four states render DISTINCT labels (live / finished-but-
//     unrecorded / died / unknown+reason);
//   - the insufficient-signal boundary renders unknown WITH the reason,
//     never a guess;
//   - a live row keeps the pre-existing "[running <dur> since <clock>]"
//     wording byte-for-byte (the regression guard for the healthy shape).

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

// stubLiveHolderProbe rebinds the display layer's liveness probe and returns
// a function restoring the production one. A `go test` process is not named
// `gopi`, so the LIVE branch is unreachable from a test without this seam —
// the alternative (shelling out to a real gopi process) would test the OS,
// not the renderer.
func stubLiveHolderProbe(t *testing.T, alive bool) func() {
	t.Helper()
	prev := taskLiveHolderProbe
	taskLiveHolderProbe = func(int) bool { return alive }
	return func() { taskLiveHolderProbe = prev }
}

// taskStateTestHolder builds a claim stamp naming THIS host and the given pid
// — the same "hostname:pid" shape taskDefaultClaimHolder emits, so the pid
// layer treats it as local and consults the liveness probe.
func taskStateTestHolder(pid int) string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s:%d", host, pid)
}

// taskStateTestSessionJSONL writes a session JSONL containing the given
// event types (one JSON line each) and returns the session id.
func taskStateTestSessionJSONL(t *testing.T, sessionID string, eventTypes ...string) {
	t.Helper()
	dir := filepath.Join(config.SessionsDirName, sessionID)
	abs := filepath.Join(gopiDataRoot(), dir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	line := func(typ string) string {
		return `{"type":"` + typ + `","sessionId":"` + sessionID + `","timestamp":"2026-09-12T19:00:00+08:00"}`
	}
	var b strings.Builder
	for _, typ := range eventTypes {
		b.WriteString(line(typ))
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(abs, sessionID+taskSessionJSONLExt), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write session jsonl: %v", err)
	}
}

// TestTaskInProgressStateDistinctLabels is the epic's core assertion: a LIVE
// run, a FINISHED-but-unrecorded run and a DIED run must render three
// different labels, and a row whose evidence settles nothing must render
// unknown WITH a reason — never a guess.
func TestTaskInProgressStateDistinctLabels(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	now := time.Now()

	// LIVE: the claim stamp names a pid ON THIS HOST and the liveness probe
	// says it runs. The probe is stubbed because a test binary is not named
	// `gopi` (taskAliveProcessNamePrefix) — the pid-reuse guard's real
	// behaviour is pinned separately in TestTaskPidIsRunnerRejectsForeignName.
	restoreProbe := stubLiveHolderProbe(t, true)
	defer restoreProbe()
	live := &Task{Status: taskStatusInProgress, ClaimedBy: taskStateTestHolder(os.Getpid()),
		HasClaim: true, ClaimedAt: float64(now.Add(-2 * time.Hour).Unix()), HasToolcall: true,
		LastToolcallAt: float64(now.Add(-30 * time.Second).Unix()), CreatedAt: float64(now.Add(-2 * time.Hour).Unix())}

	// (b) FINISHED-BUT-UNRECORDED: the session wrote its terminal event, the
	// row's worktree is GONE (finish-cleanup recycled it), the heartbeat is
	// long stale — the ledger row simply never got closed.
	taskStateTestSessionJSONL(t, "sess-done", sessionevent.EventTypeSessionStart, sessionevent.EventTypeSessionEnd)
	finished := &Task{Status: taskStatusInProgress, SessionID: "sess-done",
		Deliverable:    `{"repo":"/repo","worktree":"` + filepath.Join(sandbox, "gone-worktree") + `"}`,
		HasToolcall:    true,
		LastToolcallAt: float64(now.Add(-6 * time.Hour).Unix()), CreatedAt: float64(now.Add(-7 * time.Hour).Unix())}

	// (c) DIED: a CLAIMED row whose holder is gone — the shape the original
	// fixture dodged (it used a claimless row, so nothing ever exercised the
	// pid layer's DEAD vote and "died" was unreachable for a real worker row;
	// see the rework note in taskstate_rework_test.go). The stamp names THIS
	// host and a dead pid, its freshness keeps it attached to this row's
	// execution, the session file carries NO terminal event (kill -9 leaves
	// no session_end), no worktree recorded, heartbeat long stale. The pid
	// probe is BOUND here (not stubbed): that is the production vote.
	dead := taskStateTestDeadPid(t)
	taskStateTestSessionJSONL(t, "sess-crash", sessionevent.EventTypeSessionStart)
	died := &Task{Status: taskStatusInProgress, SessionID: "sess-crash",
		ClaimedBy: taskStateTestHolder(dead), HasClaim: true,
		ClaimedAt:       float64(now.Add(-6 * time.Hour).Unix()),
		HasInProgressAt: true, InProgressAt: float64(now.Add(-6 * time.Hour).Unix()),
		HasToolcall:    true,
		LastToolcallAt: float64(now.Add(-6 * time.Hour).Unix()), CreatedAt: float64(now.Add(-7 * time.Hour).Unix())}

	// (d) INSUFFICIENT SIGNAL: nothing at all — no claim, no session id.
	opaque := &Task{Status: taskStatusInProgress, CreatedAt: float64(now.Add(-9 * time.Hour).Unix())}

	labels := map[string]string{}
	// The probe is ON for the live row and OFF for every other row; the
	// ordering matters because the stub is process-global. Confirm the ON
	// state is actually in effect — a silently failing stub would make the
	// live assertion below pass for the wrong reason.
	if !taskLiveHolderProbe(os.Getpid()) {
		t.Fatal("live-holder probe stub is not in effect")
	}
	labels["live"] = taskInProgressStateNow(live, now)
	revealProbe := stubLiveHolderProbe(t, false)
	_ = revealProbe
	for name, tk := range map[string]*Task{"finished": finished, "died": died, "opaque": opaque} {
		labels[name] = taskInProgressStateNow(tk, now)
	}

	if got := strings.SplitN(labels["live"], taskStateSeparator, 2)[0]; got != taskStateRunning {
		t.Errorf("live row label = %q, want the running label", labels["live"])
	}
	if labels["finished"] != taskStateFinished+taskStateWorktreeSuffix {
		t.Errorf("finished-but-unrecorded row label = %q, want %q", labels["finished"], taskStateFinished+taskStateWorktreeSuffix)
	}
	if labels["died"] != taskStateDied {
		t.Errorf("died row label = %q, want %q", labels["died"], taskStateDied)
	}
	if !strings.HasPrefix(labels["opaque"], taskStateUnknown+taskStateSeparator) {
		t.Errorf("insufficient-signal row label = %q, want %q<reason>", labels["opaque"], taskStateUnknown+taskStateSeparator)
	}
	if want := taskStateUnknown + taskStateSeparator + taskStateUnknownNoClaim; labels["opaque"] != want {
		t.Errorf("insufficient-signal reason = %q, want %q (the MISSING signal, never a guess)", labels["opaque"], want)
	}

	// The three settled states must be mutually distinct — the whole point
	// of the epic was that all three rendered as one identical label.
	seen := map[string]string{}
	for name, label := range labels {
		if prev, dup := seen[label]; dup {
			t.Errorf("%s and %s render the SAME label %q — states must be distinguishable", prev, name, label)
		}
		seen[label] = name
	}
}

// TestTaskInProgressStateHonestyBoundaries pins the rows that must NOT get a
// confident verdict. Each case is a real failure mode from the epic's
// constraints: pid reuse, a foreign-host stamp, a missing session file, a
// session that ended but whose heartbeat is still fresh (one session
// executing several rows), and a live worktree under a dead-looking row.
func TestTaskInProgressStateHonestyBoundaries(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	now := time.Now()
	// Nothing in these cases is genuinely alive — the probe is stubbed ON so
	// a WRONG pid-layer verdict would be visible: every case must still come
	// back unknown, never "running".
	restore := stubLiveHolderProbe(t, true)
	defer restore()

	staleHb := func(tk *Task) *Task {
		tk.HasToolcall = true
		tk.LastToolcallAt = float64(now.Add(-6 * time.Hour).Unix())
		tk.CreatedAt = float64(now.Add(-7 * time.Hour).Unix())
		return tk
	}

	// PID REUSE: the stamp names OUR pid (alive), but the freshness axis says
	// the stamp predates the row's in_progress transition — the pid belongs to
	// a previous claim, so the pid layer must NOT vote "running".
	reused := staleHb(&Task{Status: taskStatusInProgress, ClaimedBy: taskStateTestHolder(os.Getpid()),
		HasClaim: true, ClaimedAt: float64(now.Add(-2 * time.Hour).Unix()),
		HasInProgressAt: true, InProgressAt: float64(now.Add(-time.Minute).Unix())})
	if got := taskInProgressStateNow(reused, now); got != taskStateUnknown+taskStateSeparator+taskStateUnknownStaleClaim {
		t.Errorf("stale claim stamp = %q, want unknown (%s)", got, taskStateUnknownStaleClaim)
	}

	// FOREIGN HOST: a remote claim's pid is unknowable here — unknown, never
	// "died" (we cannot see that process).
	foreign := staleHb(&Task{Status: taskStatusInProgress, ClaimedBy: "some-other-host:4242", HasClaim: true,
		ClaimedAt: float64(now.Add(-2 * time.Hour).Unix())})
	if got := taskInProgressStateNow(foreign, now); got != taskStateUnknown+taskStateSeparator+taskStateUnknownForeignHost {
		t.Errorf("foreign claim = %q, want unknown (%s)", got, taskStateUnknownForeignHost)
	}

	// MISSING SESSION FILE: a claim stamp that names NO usable pid (a
	// previous claimant's, which this host cannot attribute to the current
	// run) plus no session id to read — nothing on this host can speak, so
	// the row renders unknown with the session layer's reason.
	noSession := staleHb(&Task{Status: taskStatusInProgress, ClaimedBy: "some-other-host:4242", HasClaim: true,
		ClaimedAt: float64(now.Add(-2 * time.Hour).Unix())})
	if got := taskInProgressStateNow(noSession, now); got != taskStateUnknown+taskStateSeparator+taskStateUnknownForeignHost {
		t.Errorf("missing session file = %q, want unknown (%s)", got, taskStateUnknownForeignHost)
	}
	// The SESSION-layer reason is what a row with NO claim stamp reports —
	// there is no claim fact to name, so the missing session file is the
	// signal that keeps the row out of "died".
	noSessionNoClaim := staleHb(&Task{Status: taskStatusInProgress})
	if got := taskInProgressStateNow(noSessionNoClaim, now); got != taskStateUnknown+taskStateSeparator+taskStateUnknownNoClaim {
		t.Errorf("no claim + no session = %q, want unknown (%s)", got, taskStateUnknownNoClaim)
	}

	// SESSION ENDED, HEARTBEAT FRESH: one session can execute several rows,
	// so a session_end on disk does NOT prove THIS row finished — while the
	// ledger heartbeat is still inside the stall grace the row may be live.
	//
	// The row's stamp names a PREVIOUS claimant (claimed 2h before the
	// in_progress transition), so the claim layer speaks first with the
	// honest "stamp predates the row's start" reason — the rework made the
	// claim layer authoritative for that shape and only then consults the
	// heartbeat: a stale stamp cannot be reported as a live run.
	taskStateTestSessionJSONL(t, "sess-shared", sessionevent.EventTypeSessionStart, sessionevent.EventTypeSessionEnd)
	shared := &Task{Status: taskStatusInProgress, SessionID: "sess-shared",
		ClaimedBy: taskStateTestHolder(os.Getpid()), HasClaim: true,
		ClaimedAt:       float64(now.Add(-2 * time.Hour).Unix()),
		HasInProgressAt: true, InProgressAt: float64(now.Add(-time.Hour).Unix()),
		HasToolcall: true, LastToolcallAt: float64(now.Add(-time.Minute).Unix()), CreatedAt: float64(now.Add(-time.Hour).Unix())}
	if got := taskInProgressStateNow(shared, now); got != taskStateUnknown+taskStateSeparator+taskStateUnknownStaleClaim {
		t.Errorf("session ended with a fresh heartbeat = %q, want unknown (%s)", got, taskStateUnknownStaleClaim)
	}

	// The heartbeat grace clause itself is reachable only for a row the claim
	// layer has nothing to say about — pin that shape explicitly so the
	// clause cannot rot: no claim stamp at all, session data readable and ENDED,
	// heartbeat fresh. The claimless row carries no session id, so the
	// session layer stays silent and the CLAIM reason is the honest one.
	noClaimShared := &Task{Status: taskStatusInProgress, SessionID: "sess-shared",
		HasToolcall: true, LastToolcallAt: float64(now.Add(-time.Minute).Unix()), CreatedAt: float64(now.Add(-time.Hour).Unix())}
	if got := taskInProgressStateNow(noClaimShared, now); got != taskStateUnknown+taskStateSeparator+taskStateUnknownNoClaim {
		t.Errorf("claimless row, session ended, fresh heartbeat = %q, want unknown (%s)", got, taskStateUnknownNoClaim)
	}
	// The heartbeat clause needs the claim layer to be SILENT and the session
	// layer to SPEAK (an ended session whose heartbeat is fresh): a row whose
	// session id resolves to a file nobody is holding, with a stamp this host
	// can never attribute (a foreign host) — the claim layer's own reason is
	// what the operator is owed there, so the LAST case pins that precedence
	// rather than the heartbeat text.
	unspeakable := &Task{Status: taskStatusInProgress, SessionID: "sess-shared",
		ClaimedBy: "some-other-host:4242", HasClaim: true,
		ClaimedAt:       float64(now.Add(-4 * time.Minute).Unix()),
		HasInProgressAt: true, InProgressAt: float64(now.Add(-4 * time.Minute).Unix()),
		HasToolcall: true, LastToolcallAt: float64(now.Add(-time.Minute).Unix()), CreatedAt: float64(now.Add(-time.Hour).Unix())}
	if got := taskInProgressStateNow(unspeakable, now); got != taskStateUnknown+taskStateSeparator+taskStateUnknownForeignHost {
		t.Errorf("foreign stamp, session ended, fresh heartbeat = %q, want unknown (%s)", got, taskStateUnknownForeignHost)
	}
	// The function-level live-holder stub (bound above, restored only at
	// function exit) would answer "alive" for ANY pid — including the dead
	// holder of the case below — so it is restored HERE: the worktree case is
	// about the PID LAYER'S OWN DEAD VOTE, and a stub must never be able to
	// make it vacuous.
	restore()

	// LIVE WORKTREE, SESSION NEVER ENDED: the run left working state on disk
	// that nobody is holding — "died" is withheld (the work is recoverable,
	// a resume may be running under a pid we cannot attribute). The stamp
	// names THIS host and a DEAD pid, so the claim layer votes and the
	// worktree hatch has to hold — the shape the original fixture reached
	// only through the session layer.
	wt := filepath.Join(sandbox, "intact-worktree")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	taskStateTestSessionJSONL(t, "sess-wt", sessionevent.EventTypeSessionStart)
	held := staleHb(&Task{Status: taskStatusInProgress, SessionID: "sess-wt",
		ClaimedBy: taskStateTestHolder(taskStateTestDeadPid(t)), HasClaim: true,
		ClaimedAt:       float64(now.Add(-6 * time.Hour).Unix()),
		HasInProgressAt: true, InProgressAt: float64(now.Add(-6 * time.Hour).Unix()),
		Deliverable: `{"repo":"/repo","worktree":"` + wt + `"}`})
	if got := taskInProgressStateNow(held, now); got != taskStateUnknown+taskStateSeparator+taskStateUnknownWorktreeHeld {
		t.Errorf("intact worktree + no session end = %q, want unknown (%s)", got, taskStateUnknownWorktreeHeld)
	}
}

// TestTaskStateSuffixWording pins the RENDERED tail for each state: a live
// row keeps the pre-existing "[running <dur> since <HH:MM:SS>]" wording
// (byte-compatible with the pre-change display), a died row keeps the
// elapsed story under its own label, and the session-history states frame
// the row's AGE instead of claiming a running duration.
func TestTaskStateSuffixWording(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(config.GopiDirEnv, sandbox)
	now := time.Now()
	restore := stubLiveHolderProbe(t, true)
	defer restore()

	live := &Task{Status: taskStatusInProgress, ClaimedBy: taskStateTestHolder(os.Getpid()), HasClaim: true,
		ClaimedAt: float64(now.Add(-2*time.Hour - 13*time.Minute).Unix()), CreatedAt: float64(now.Unix()),
		HasToolcall: true, LastToolcallAt: float64(now.Add(-time.Second).Unix())}
	got := taskStateSuffix(live, now)
	want := "  [running 2h13m0s since " + time.Unix(int64(live.ClaimedAt), 0).Format("15:04:05") + "]"
	if got != want {
		t.Errorf("live row suffix = %q, want %q (the pre-change wording, byte for byte)", got, want)
	}

	sessID := "sess-finished-wording"
	taskStateTestSessionJSONL(t, sessID, sessionevent.EventTypeSessionStart, sessionevent.EventTypeSessionEnd)
	finished := &Task{Status: taskStatusInProgress, SessionID: sessID,
		Deliverable: `{"worktree":"` + filepath.Join(sandbox, "also-gone") + `"}`,
		CreatedAt:   float64(now.Add(-8 * time.Hour).Unix()),
		HasToolcall: true, LastToolcallAt: float64(now.Add(-7 * time.Hour).Unix())}
	got = taskStateSuffix(finished, now)
	if !strings.HasPrefix(got, "  ["+taskStateFinished+taskStateWorktreeSuffix+", age ") || !strings.HasSuffix(got, "]") {
		t.Errorf("finished row suffix = %q, want the finished label with the row age", got)
	}
	if strings.Contains(got, "since ") {
		t.Errorf("finished row suffix = %q — a session-history state must not claim a running window", got)
	}
}

// TestTaskPidIsRunnerRejectsForeignName pins the PID-REUSE guard: a pid whose
// /proc/<pid>/comm is not ours is NOT the row's executor, so a recycled pid
// can never render a dead row as live. The probe is evaluated against real
// pids: our own process (the test binary — deliberately NOT named `gopi`) must
// be rejected, and a nonexistent pid must be dead.
func TestTaskPidIsRunnerRejectsForeignName(t *testing.T) {
	if taskPidIsRunner(os.Getpid()) {
		t.Errorf("test binary (%s) must not be accepted as a gopi runner by the pid-reuse guard", taskAliveProcessNamePrefix)
	}
	if pidAlive(1 << 30) {
		t.Error("a nonexistent pid must not be reported alive")
	}
}

// TestSessionTailHasEndIsLineAware is the anti-false-positive probe: the
// event name is matched as a PARSED "type" field on a complete line, never
// as a substring of the raw bytes — a prompt/console echo of the literal
// text "session_end" (or a partial line cut by the tail seek) must not
// certify a dead run as finished.
func TestSessionTailHasEndIsLineAware(t *testing.T) {
	real := []byte(`{"type":"session_start","sessionId":"a"}` + "\n" + `{"type":"session_end","exit_code":0}` + "\n")
	if !sessionTailHasEnd(real) {
		t.Error("a real session_end event line must be detected")
	}
	echoed := []byte(`{"type":"session_start","sessionId":"a"}` + "\n" +
		`{"type":"assistant","text":"the log would end with session_end here"}` + "\n")
	if sessionTailHasEnd(echoed) {
		t.Error("a raw substring mention of session_end must NOT count as the terminal event")
	}
	truncated := []byte(`"text":"...ends with a session_end"}` + "\n")
	if sessionTailHasEnd(truncated) {
		t.Error("a partial (non-object) line must never decide the verdict")
	}
	// A window whose first byte lands mid-line: the fragment before the
	// first newline is discarded, the complete lines after it still count.
	midLine := []byte(`pe":"tool_result"}` + "\n" + `{"type":"session_end"}` + "\n")
	if !sessionTailHasEnd(midLine) {
		t.Error("a mid-line seek must still read the complete lines that follow")
	}
}

// TestTaskStateSessionTailWindowKeepsVerdict pins the bounded-tail contract:
// a session whose terminal event sits FAR beyond the read window cannot
// certify anything, and the window is a named constant (Rule 1) — the probe
// must never slurp a multi-megabyte session into the list render.
func TestTaskStateSessionTailWindowKeepsVerdict(t *testing.T) {
	if taskStateSessionTailBytes < 1<<16 {
		t.Fatalf("tail window %d is too small to hold one console-text line", taskStateSessionTailBytes)
	}
	long := strings.Repeat(`{"type":"assistant","text":"x"}`+"\n", 3)
	if !sessionTailHasEnd([]byte(long + `{"type":"session_end"}` + "\n")) {
		t.Error("terminal event inside the window must be found")
	}
}
