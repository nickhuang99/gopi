package testutil

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// A suite ended by a TERMINATING SIGNAL must not leave its per-run root behind —
// and the arm that ensures that must never touch a root whose run is still ALIVE.
// Those two halves are one test, because taking the first one literally is what
// produced the 2026-10-23 defect (the arm removed/moved a LIVE root at signal
// time; four cmd/agent tests then met `TempDir: stat …: no such file or
// directory` and all four were green when run alone).
//
// MEASURED (this host, 2026-10-22): /dev/shm held 19 `suite-*-run-*` roots /
// 1.7G, all minted the same day, and every `.holder` inside them named a pid with
// no /proc entry — the shape of runs that ended without reaching
// FinishSuiteForTestMain. A clean exit runs that cleanup (measured: one
// `make test-budget PKGS=./internal/lineedit` left the root count unchanged);
// a signal skips it, because a signal skips the defer stack AND the statements
// after m.Run.
//
// THE DIVISION OF LABOUR (this file's contract, after the 2026-10-23 fix):
//
//	at signal time   the arm reports and RE-RAISES. It acts on nothing, because
//	                 the run it belongs to is alive at that instant and a live
//	                 registration is never residue (scratch.SelfReapReasonOwn).
//	the child dies   the root is now residue: its registration names a pid the
//	                 kernel proves gone, and the NEXT run of the same suite
//	                 collects it at start, at any age, by name.
//
// So this test asserts BOTH: the child ends BY SIGTERM, the root is intact while
// the process lives, and the start-time sweep of the same suite takes it once the
// owner is dead. Reverting the arm to `signal.Reset`-less re-raise still passes
// the death half; removing the re-raise makes the death half red; making the arm
// act on the root at signal time makes the LIVE half red (that is
// TestSuiteRunRootSurvivesASecondSubscriber's job for the in-process shape, and
// scratch's TestSuiteRunSelfReapNeverTakesALiveOwnersRoot for the predicate).
const (
	// suiteRootSignalChildEnv marks the child half of this test.
	suiteRootSignalChildEnv = "GOPI_TESTUTIL_SUITEROT_SIGNAL_CHILD"
	// suiteRootSignalPathEnv names the file the child writes its pinned root to.
	// The parent reads the path from there rather than recomputing it: WHERE the
	// pin lands (memory base or scratch home) is the host's decision, and a
	// second spelling of it here is exactly the drift this avoids.
	suiteRootSignalPathEnv = "GOPI_TESTUTIL_SUITEROT_SIGNAL_PATH"
	// suiteRootSignalWait bounds the parent's wait for the child to report its
	// root (compile is already done — it is this binary — but a loaded host is
	// still a loaded host).
	suiteRootSignalWait = 60 * time.Second
	// suiteRootSignalReapWait bounds the wait for the handler after the signal.
	suiteRootSignalReapWait = 20 * time.Second
	// suiteRootSignalPeerWait bounds the child's wait for the peer channel after
	// it raises the arm's own signal at itself.
	suiteRootSignalPeerWait = 5 * time.Second
	// suiteRootPeerEnv is set on the child of
	// TestSuiteRunRootSurvivesASecondSubscriber: it turns the child half from
	// "park until signalled from outside" into "install a SECOND subscriber,
	// raise the arm's own signal at this process, and report what happened".
	suiteRootPeerEnv = "GOPI_TESTUTIL_SUITEROT_SIGNAL_PEER"
	// suiteRootPeerMarkerEnv names the file the peer child appends its
	// observations to. A file rather than stdout because a child killed by a
	// signal loses nothing this way: the marker survives with whatever was
	// written before the death, which is the whole point.
	suiteRootPeerMarkerEnv = "GOPI_TESTUTIL_SUITEROT_SIGNAL_PEER_MARKER"
	// suiteRootPeerDrainEnv turns the child's SECOND subscriber into the shape
	// `cmd/agent`'s TestWorkerDrainsOnSIGHUP has when the process is not the
	// first SIGHUP in its run: the peer channel has already been drained by the
	// test's own logic (its buffer is EMPTY at the moment of the arm's
	// re-raise), rather than still holding an undelivered copy. Both shapes are
	// correct for the caller; only one of them is forgiving of a racy raise.
	suiteRootPeerDrainEnv = "GOPI_TESTUTIL_SUITEROT_SIGNAL_PEER_DRAIN"
)

// TestSuiteRunRootIsReapedOnSignal is the red→green arm.
func TestSuiteRunRootIsReapedOnSignal(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child test binary")
	}
	if _, err := os.Stat(os.Args[0]); err != nil {
		t.Fatalf("cannot re-exec this test binary (%s): %v", os.Args[0], err)
	}
	reportDir := t.TempDir()
	reportPath := filepath.Join(reportDir, "child-root.txt")
	// The child must NOT inherit a scratch-root override from this process: it
	// would pin inside THIS test's fixture tree, which is removed with the test
	// and would make the assertion vacuous.
	cmd := exec.Command(os.Args[0], "-test.run", "^TestSuiteRunRootSignalChild$", "-test.v")
	cmd.Env = append(scrubbedSuiteRootChildEnv(), suiteRootSignalChildEnv+"=1", suiteRootSignalPathEnv+"="+reportPath)
	var out lockedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the child suite: %v", err)
	}
	root := waitForReportedRoot(t, reportPath, &out, cmd)
	// The child is parked; SIGTERM it the way a budget timeout would. This is the
	// LIVE half: while the child is alive its root must still be EXACTLY where the
	// pin put it — the arm may report the signal, never act on the tree.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal the child suite: %v", err)
	}
	// The child still has to DIE from that signal: signal.Notify disables the
	// default action, and a handler that only cleans up would leave a hung run.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		ws, ok := childExitStatus(err, cmd)
		if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
			t.Fatalf("the child did not end BY SIGTERM (wait=%v) — the re-raise is missing; output:\n%s",
				err, out.String())
		}
	case <-time.After(suiteRootSignalReapWait):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		t.Fatalf("the child never exited after SIGTERM — signal.Notify was not reset; output:\n%s", out.String())
	}
	// THE LIVE HALF, read off the child's own tree after it is gone: the arm
	// reported the signal and wrote nothing — every file the run created is still
	// there, under the SAME name, with its registration intact. (A move-aside, the
	// pre-fix shape, is a loss for a run that survives; here the child did die, so
	// the residue is the start sweep's.)
	if _, err := os.Lstat(filepath.Join(root, scratch.HolderFileName)); err != nil {
		t.Fatalf("the dead child's run root lost its registration %s: %v — the arm acted on a live run's tree",
			root, err)
	}
	// THE RESIDUE HALF: the next run of the same suite collects it at start, at
	// ANY age, on the evidence of the now-dead registration. This is the ONE
	// collector entitled to the root, and it is the arm the fix relies on.
	parent := filepath.Dir(root)
	removed, kept, err := scratch.ReapDeadSuiteRunRootsUnder(parent, scratchRunDirName(internalTestutilSuite))
	if err != nil {
		t.Fatalf("start sweep over %s: %v", parent, err)
	}
	if removed < 1 {
		t.Fatalf("the start sweep removed %d roots under %s (want at least %s); kept=%v",
			removed, parent, root, kept)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("the dead run's root %s survived the start sweep (err=%v)", root, err)
	}
}

// TestSuiteRunRootSignalChild is the child half. It reports the root the PIN
// created and then parks until the parent kills it.
func TestSuiteRunRootSignalChild(t *testing.T) {
	reportPath := os.Getenv(suiteRootSignalPathEnv)
	if reportPath == "" {
		t.Skip("not the child half of TestSuiteRunRootIsReapedOnSignal")
	}
	root := SuiteTempRoot()
	if root == "" {
		t.Fatalf("the child's pin produced no root")
	}
	// The registration is what the reap cites, and the pin wrote it — assert it
	// here so a failure is attributed to the pin rather than to the handler.
	if _, err := os.Stat(filepath.Join(root, scratch.HolderFileName)); err != nil {
		t.Fatalf("the pin did not register %s: %v", root, err)
	}
	if err := os.WriteFile(reportPath, []byte(root), 0o644); err != nil {
		t.Fatalf("report the child root: %v", err)
	}
	// THE TWO-SUBSCRIBER SHAPE, reproduced in a child that really dies from it
	// (TestSuiteRunRootSurvivesASecondSubscriber): subscribe to the arm's OWN
	// signal the way a test guards itself, then raise it at this process. With
	// the arm's blind `signal.Reset` the peer registration is cancelled, the
	// raise lands on the default disposition and the process dies here; with the
	// arm's Stop-only re-raise the peer keeps the signal, this process survives
	// and the assertion below runs.
	if os.Getenv(suiteRootPeerEnv) != "" {
		marker := os.Getenv(suiteRootPeerMarkerEnv)
		peer := make(chan os.Signal, 2)
		signal.Notify(peer, suiteRunRootReapSignal)
		if os.Getenv(suiteRootPeerDrainEnv) != "" {
			// The DRAINED shape: a prior delivery is consumed here, so the buffer
			// is EMPTY when the arm re-raises — exactly cmd/agent's keepAlive on a
			// repeat SIGHUP (see the parent's table).
			_ = syscall.Kill(os.Getpid(), suiteRunRootReapSignal) //stylegate:ignore signal to a process group we are already abandoning; ESRCH is the expected error
			select {
			case <-peer:
			case <-time.After(suiteRootSignalPeerWait):
			}
		}
		// THE ORDER BELOW IS THE FIXTURE, and getting it wrong makes this test a
		// flake rather than a proof (measured: 1 red in 4 repetitions before the
		// join was added, always "the child was KILLED"). The arm's re-raise runs
		// on the ARM's goroutine, i.e. concurrently with this one: a child that
		// returned as soon as it saw its own peer delivery would hit its deferred
		// signal.Stop(peer) BEFORE the arm's Kill, removing the only surviving
		// registration and dying of its own test's timing. So the child WAITS for
		// the arm to finish (JoinSuiteRunRootSignalHandler publishes exactly that
		// join) and only then decides — after which both the reap and the re-raise
		// are complete and nothing is pending.
		killErr := syscall.Kill(os.Getpid(), suiteRunRootReapSignal)
		writeMarker(marker, fmt.Sprintf("alive_after_self_raise=%v peer_got_split=true\nkill_err=%v\nroot=%s\n",
			killErr == nil, killErr, root))
		select {
		case got := <-peer:
			writeMarker(marker, fmt.Sprintf("peer received %v: this test's own registration survived the arm's own exit\n", got))
		case <-time.After(suiteRootSignalPeerWait):
			writeMarker(marker, "peer never received the signal: the arm consumed it\n")
		}
		// THE JOIN: wait for the arm to have finished reaping and re-raising.
		JoinSuiteRunRootSignalHandler()
		// A signal the arm re-raised and the PEER received is the contract: the
		// arm must not have taken this registration away.
		select {
		case got := <-peer:
			writeMarker(marker, fmt.Sprintf("peer received the arm's re-raise too (%v)\n", got))
		default:
			writeMarker(marker, "peer did not receive a second signal (the raise was delivered once)\n")
		}
		// The root must still be here: a survivor keeps its root.
		if _, err := os.Stat(root); err != nil {
			writeMarker(marker, fmt.Sprintf("SURVIVOR-LOST-ROOT %s: %v\n", root, err))
			t.Fatalf("this process survived the re-raise but its run root is gone: %v", err)
		}
		writeMarker(marker, "STILL-ALIVE-AT-END\n")
		return
	}
	// Park. The parent signals this process; a test that returned normally would
	// exercise the CLEAN path, which is a different (already covered) arm.
	//stylegate:allow-sleep the wait IS the fixture: this process must still be
	// PARKED when the parent's SIGTERM arrives, and returning would exercise a
	// different (already covered) arm of the handler
	time.Sleep(5 * time.Minute)
}

// TestSuiteRunRootSurvivesASecondSubscriber is the regression arm for the defect
// the arm's own re-raise used to CAUSE rather than suffer (measured 2026-10-23,
// raw exit codes below).
//
// THE SHAPE, and it is the shape a real test has: this process holds TWO
// registrations for the arm's own signal — the suite arm (installed by the pin,
// for the whole run) and a SECOND one a test installs to protect itself while it
// deliberately raises that signal at its own pid. `cmd/agent`'s
// TestWorkerDrainsOnSIGHUP is the measured instance (`signal.Notify(keepAlive,
// syscall.SIGHUP)` under a comment calling it a SAFETY NET).
//
// THE DEFECT: `signal.Reset(sig)` is cancel(sig, disableSignal) over EVERY
// registered channel, so the arm's own exit DEREGISTERED the second subscriber —
// after which the re-raised signal reached the default disposition and killed the
// whole test binary mid-suite; the arm had already deleted the run root, so every
// later test in that run met ENOENT.
//
//	$ go test ./cmd/agent/ -run 'TestWorkerDrainsOnSIGHUP' -count=1 ; echo $?
//	FAIL	github.com/nickhuang99/gopi/cmd/agent	1.497s      ("signal: hangup")
//	1                                    ← pre-fix, this file at 117/200
//	0                                    ← post-fix, same command, twice re-measured
//
// The child of this test reproduces exactly that shape IN-PROCESS (the only way
// to observe it: from outside, a dead child is just a dead child). It raises the
// arm's signal at itself while holding its own registration, then reports what it
// saw:
//
//	pre-fix  → the child is KILLED by that signal (it exits signal-terminated),
//	           and its report stops at "alive_after_self_raise" — the missing
//	           "peer received" line IS the cancelled registration.
//	post-fix → the child SURVIVES (exit 0), its peer channel receives the signal,
//	           and its run root is still on disk.
func TestSuiteRunRootSurvivesASecondSubscriber(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child test binary")
	}
	if _, err := os.Stat(os.Args[0]); err != nil {
		t.Fatalf("cannot re-exec this test binary (%s): %v", os.Args[0], err)
	}
	// TWO SHAPES, and the second one is the one that actually catches a racy arm.
	//
	//	drained: the peer channel is EMPTY when the arm re-raises. That is
	//	         cmd/agent on any repeat SIGHUP, and a buffered channel that is
	//	         momentarily empty refuses the signal — so the peer's survival
	//	         depends on the ARM KILLING THE PROCESS SYNCHRONOUSLY, inside the
	//	         same SIGKILL-from-the-runtime window where no second delivery can
	//	         beat it (a survivable raise does not exist in this bin).
	//	full:    the peer still holds its first (undelivered) copy, as cmd/agent
	//	         is on the FIRST hangup of a run. A raise is queued behind it and
	//	         the process can outlive the arm's goroutine.
	//
	// Both must end the same way: the child alive, its peer registered, its root
	// kept. The drained shape is stricter and is checked FIRST so a fix that only
	// works when the caller's buffer happens to be full cannot pass.
	for _, shape := range []struct {
		name  string
		drain bool
	}{
		{name: "drained", drain: true},
		{name: "full", drain: false},
	} {
		shape := shape
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			runSecondSubscriberChild(t, shape.drain)
		})
	}
}

// runSecondSubscriberChild spawns the child half in the two-subscriber shape and
// holds it to the three assertions the arm owes a caller that SURVIVES.
func runSecondSubscriberChild(t *testing.T, drain bool) {
	t.Helper()
	reportDir := t.TempDir()
	markerPath := filepath.Join(reportDir, "peer-markers.txt")
	rootPath := filepath.Join(reportDir, "child-root.txt")
	cmd := exec.Command(os.Args[0], "-test.run", "^TestSuiteRunRootSignalChild$", "-test.v")
	cmd.Env = append(scrubbedSuiteRootChildEnv(),
		suiteRootSignalChildEnv+"=1",
		suiteRootSignalPathEnv+"="+rootPath,
		suiteRootPeerEnv+"=1",
		suiteRootPeerMarkerEnv+"="+markerPath,
	)
	if drain {
		cmd.Env = append(cmd.Env, suiteRootPeerDrainEnv+"=1")
	}
	var out lockedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the child suite: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(suiteRootSignalWait):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		t.Fatalf("the child never finished — it neither died from the re-raise nor reported survival; output:\n%s", out.String())
	}
	markers := readMarkerFile(markerPath)
	// (1) THE SURVIVAL, measured as an exit STATUS: a process killed by the
	// signal cannot exit 0. This is the assertion a `signal.Reset` arm breaks.
	ws, ok := childExitStatus(waitErr, cmd)
	if !ok {
		t.Fatalf("cannot read the child's exit status (wait=%v); markers:\n%s\noutput:\n%s", waitErr, markers, out.String())
	}
	if ws.Signaled() {
		t.Fatalf("the child was KILLED by %v — the arm cancelled the second subscriber's registration "+
			"(this is the signal.Reset defect); markers:\n%s\noutput:\n%s", ws.Signal(), markers, out.String())
	}
	if !ws.Exited() || ws.ExitStatus() != 0 {
		t.Fatalf("the child did not survive cleanly (wait=%v, status=%v); markers:\n%s\noutput:\n%s",
			waitErr, ws, markers, out.String())
	}
	// (2) THE OTHER SUBSCRIBER really received the signal: survival alone could
	// also mean the signal was swallowed somewhere, and that is not the contract
	// this arm owes — it owes "every other subscriber keeps working".
	if !strings.Contains(markers, "peer received signal") && !strings.Contains(markers, "peer received") {
		t.Fatalf("the second subscriber never received the re-raised signal — the arm consumed it (markers:\n%s)", markers)
	}
	// (3) THE ROOT: a process that SURVIVES must keep its root. Reaping it would
	// hollow every later test in the same run (the ENOENT half of the defect).
	if strings.Contains(markers, "SURVIVOR-LOST-ROOT") {
		t.Fatalf("the child survived but its run root was removed by the arm (markers:\n%s)", markers)
	}
	root := strings.TrimSpace(readOrEmpty(rootPath))
	if root == "" {
		t.Fatalf("the child never reported its root; markers:\n%s\noutput:\n%s", markers, out.String())
	}
	// The child's own clean exit removes the root through the ORDINARY path, so
	// "gone now" is expected; what must never have happened is the arm removing
	// it at SIGNAL time, which (3) reads off the child's own observation.
}

// readMarkerFile returns the child's marker file, or a note that it is absent.
func readMarkerFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("(no markers at %s: %v)", path, err)
	}
	return string(raw)
}

// readOrEmpty reads a small report file, returning "" when it is absent.
func readOrEmpty(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(raw)
}

// writeMarker appends one observation to the peer child's marker file. Append,
// never truncate: the whole point is that the lines written BEFORE a signal death
// survive it, so the last line on disk says how far the child got.
func writeMarker(path, line string) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()            // the file is the deliverable; a close error cannot be acted on here
	_, _ = f.WriteString(line) //stylegate:ignore append to a best-effort dispatch log; a full disk is reported on the checked calls
}

// scrubbedSuiteRootChildEnv drops the scratch-root overrides so the child pins
// in the REAL placement (see the caller).
func scrubbedSuiteRootChildEnv() []string {
	drop := map[string]bool{SuiteTempRootEnvName: true, ScratchHomeEnvName: true}
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		name, _, _ := splitEnvKV(kv)
		if drop[name] {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func splitEnvKV(kv string) (name, value string, found bool) {
	name, value, found = strings.Cut(kv, "=")
	return name, value, found
}

// waitForReportedRoot waits until the child reports the root it pinned.
func waitForReportedRoot(t *testing.T, reportPath string, out *lockedBuffer, cmd *exec.Cmd) string {
	t.Helper()
	deadline := time.Now().Add(suiteRootSignalWait)
	for {
		if raw, err := os.ReadFile(reportPath); err == nil && len(raw) > 0 {
			return string(raw)
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
			t.Fatalf("the child never reported its root (report %s); output:\n%s", reportPath, out.String())
		}
		//stylegate:allow-sleep bounded poll interval: suiteRootSignalWait owns the
		// deadline and the loop exits on the first non-empty report
		time.Sleep(20 * time.Millisecond)
	}
}

// childExitStatus pulls the process state out of a Wait result for the assertion
// above, without asserting on the error itself.
func childExitStatus(err error, cmd *exec.Cmd) (syscall.WaitStatus, bool) {
	_ = err //stylegate:ignore the error is already reported by the caller's checked call; this site only reaps or logs the leftover
	if cmd.ProcessState == nil {
		return 0, false
	}
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	return ws, ok
}

// lockedBuffer is a minimal concurrency-safe sink for a child's output.
type lockedBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}
