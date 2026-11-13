package main

// signal_record_filed_guard_test.go — the regression guard for the record-filed
// handshake (epic: a claiming door must not outrun the recorder).
//
// THE DEFECT IT PINS (measured 2026-10-12, pre-fix tree 45b54d81f).
// A gopi verb that owns its OWN terminating-signal handler — the class that must
// call signalrec.ClaimSignalHandlers, because the recorder's argv table cannot
// classify it — unwinds on its own goroutine and exits the process. The
// recorder's record is filed on a DIFFERENT goroutine woken by the same signal
// delivery, and it has real work to do first (resolve a session destination,
// read /proc, tail the session log, append a line). Measured: the claiming verb
// `gopi memory sync --every 5s --push` printed its stop line and exited 0 in
// 12/12 runs with NO signal-provenance record on disk, while the same profile,
// the same destination and the same signal-wait filed it every time on SIGHUP —
// a signal that wakes no verb-side stop channel. The record IS the deliverable
// of the provenance epic, and before this handshake nothing in the tree reported
// its loss: a reader could not tell "never signalled" from "signalled, the record
// lost the race".
//
// WHY THE CHILD IS A REAL RE-EXECUTED PROCESS. The race only exists between
// OS-level handlers, so an in-process test cannot reproduce it: it must drive the
// built binary (gopiTestBinPath — the same build every other binary-driving case
// in this suite uses), kill it for real, and then ask the FILESYSTEM what is on
// disk. The assertion is a file, never an exit code: exit 0 was true on the
// pre-fix tree too, which is exactly why the loss went unnoticed.
//
// WHY IT REPEATS (Rule 27). A race is not measured once — the arm is decided by
// the scheduler, so a single green run proves nothing. The repetition is bounded
// (signalRecordFiledRuns) and each run is a tiny process, never a load: no device
// is touched, nothing is saturated.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// signalRecordDoorStartMarker is the stable fragment of the CLAIMING DOOR's
// start line (`worker: loop start (claim → run → …)`): the handshake that says
// the recorder is armed and this process is inside the loop it will unwind
// from. Shared by both signal guards in this package (Rule 14: one definition,
// two readers), because both kill at exactly this point and neither may fall
// back to a blind sleep.
const signalRecordDoorStartMarker = "worker: loop start"

const (
	// signalRecordFiledRuns is the repetition count. It is >1 because the defect
	// is a race (see the file header): one run cannot distinguish "fixed" from
	// "got lucky".
	signalRecordFiledRuns = 5
	// signalRecordFiledID is the ledger id the child carries and the session dir
	// seeded for it. A FIXED literal id (not a generated one) keeps the fixture
	// readable and keeps the destination out of the ledger namespace: nothing
	// else can be keyed by it.
	signalRecordFiledID = "fixture-signalrec-filed"
	// signalRecordFiledStartWait bounds the wait for the loop's start line —
	// the deterministic point at which the process is inside its own unwindable
	// loop and the recorder is armed.
	signalRecordFiledStartWait = 20 * time.Second
	// signalRecordFiledStopWait bounds the wait for the child to exit after the
	// STOP signal. It is generous on purpose: a door that WAITS for the record
	// takes longer to die than one that does not, and the guard must fail on a
	// MISSING RECORD, never on a slow machine.
	signalRecordFiledStopWait = 30 * time.Second
	// signalRecordFiledStartMarker is the stable fragment of the door's start
	// line (`worker: loop start …`). It is the handshake that says the recorder
	// is armed and this process is inside the loop it will unwind from — the
	// point every run kills at, so no run depends on a blind sleep (Rule 1: a
	// named constant, not a literal at the comparison site).
	signalRecordFiledStartMarker = signalRecordDoorStartMarker
)

// TestSignalProvenanceRecordFiledWhenAClaimingDoorExits is the guard.
func TestSignalProvenanceRecordFiledWhenAClaimingDoorExits(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: skip binary E2E")
	}
	bin := gopiTestBinPath(t)

	// The sandbox comes from the ONE initializer (Rule 34), never from a
	// hand-built directory: the guard needs a private profile, and this is the
	// product's own way of materialising one. It is created once and reused for
	// every repetition — each run only appends to its own session dir.
	sb := signalRecordFiledSandbox(t, bin)
	sessRoot := filepath.Join(sb, ".gopi", "sessions")
	dest := filepath.Join(sessRoot, signalRecordFiledID)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("seed the session destination %s: %v", dest, err)
	}
	// The destination's marker (the same shape the run-time logger mints). It is
	// what makes the directory a SESSION DIR for signalrec's resolver — without
	// it the record is REFUSED rather than lost, and a refusal is a different
	// finding that this guard must not silently accept.
	marker := filepath.Join(dest, "metadata.json")
	if err := os.WriteFile(marker, []byte(`{"session_id":"`+signalRecordFiledID+`"}`), 0o644); err != nil {
		t.Fatalf("write the session marker %s: %v", marker, err)
	}
	prov := filepath.Join(dest, "signal-provenance.jsonl")

	for run := 1; run <= signalRecordFiledRuns; run++ {
		before := signalRecordFiledLines(t, prov)
		out := signalRecordFiledKillRun(t, bin, sb, run)
		after := signalRecordFiledLines(t, prov)

		if after <= before {
			t.Fatalf("run %d/%d: the claiming door printed its stop line and exited with NO provenance record on disk (%s did not grow: %d -> %d)\n"+
				"the recorder's record is filed on its own goroutine; a door that exits without settling on it loses the record ENTIRELY, "+
				"which makes 'never signalled' and 'record lost' indistinguishable.\n--- child output ---\n%s",
				run, signalRecordFiledRuns, prov, before, after, out)
		}
		// The record must be the kill's own: a stray line from an earlier run
		// would satisfy a line COUNT, so the guard reads the last line.
		if last := signalRecordFiledLast(t, prov); !strings.Contains(last, `"kind":"signal"`) ||
			!strings.Contains(last, "SIGTERM") {
			t.Fatalf("run %d/%d: the new provenance line is not this kill: %s", run, signalRecordFiledRuns, last)
		}
	}
}

// signalRecordFiledKillRun drives ONE real run: start the verb, wait for the
// loop to be running (its start line — never a blind sleep), STOP it, and return
// the child's combined output.
//
// The child's environment is built from the RECORDING side of the picture, which
// is the point: the record's destination must be resolvable in the child, or the
// guard would measure a REFUSAL instead of the race.
func signalRecordFiledKillRun(t *testing.T, bin, sandbox string, run int) string {
	t.Helper()
	// THE CLAIMING DOOR. It must own its terminating-signal handlers
	// (signalrec.ClaimSignalHandlers) and be a NEVER-RETURNING loop, so a
	// SIGTERM lands in a real unwind rather than in a process that had already
	// answered and left. `gopi worker` is that door: it claims at its own start
	// and waits on a drained queue (30 s ticks, three idle rounds) without
	// needing a model, a peer or a network — the previous fixture verb
	// (`memory sync --every`, the cross-node sync family) was DELETED
	// 2026-11-05 with the whole ssh ledger family, which is why this arm names
	// the surviving door instead.
	cmd := exec.Command(bin, "worker")
	cmd.Env = append(signalRecordFiledCleanEnv(),
		"GOPI_HOME="+sandbox,
		"GOPI_SETTINGS="+filepath.Join(sandbox, ".gopi", "settings.json"),
		"GOPI_SESSIONS_ROOT="+filepath.Join(sandbox, ".gopi", "sessions"),
		// The ledger id the child carries: this is what keys the record to the
		// seeded session dir (the door's own id, not an inherited one).
		"GOPI_TASK_RUN_ID="+signalRecordFiledID,
		"GOPI_MEMORY_DB="+filepath.Join(sandbox, ".gopi", "memory.db"),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("run %d: stdout pipe: %v", run, err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatalf("run %d: start %s: %v", run, bin, err)
	}
	// The process is stopped in EVERY arm — a guard that leaks a live loop would
	// leave a process behind for the next case (and for the sandbox reclaimer).
	stopped := false
	defer func() {
		if !stopped {
			_ = cmd.Process.Signal(syscall.SIGKILL) //stylegate:ignore signals the child the test spawned; the transition it causes is what is asserted
			_, _ = cmd.Process.Wait()               //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
		}
	}()

	started := make(chan string, 1)
	go func() {
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 512)
		for {
			n, rerr := stdout.Read(tmp)
			buf = append(buf, tmp[:n]...)
			// The loop prints its start line before its first round: that line
			// IS the handshake that says "the recorder is armed and this process
			// is now in the loop it will unwind from".
			if strings.Contains(string(buf), signalRecordFiledStartMarker) {
				started <- string(buf)
				// Drain the rest so the child never blocks on a full pipe.
				for {
					if _, rerr := stdout.Read(tmp); rerr != nil {
						return
					}
				}
			}
			if rerr != nil {
				started <- string(buf)
				return
			}
		}
	}()

	var seen string
	select {
	case seen = <-started:
	case <-time.After(signalRecordFiledStartWait):
		t.Fatalf("run %d: the loop never announced itself within %s — nothing was measured", run, signalRecordFiledStartWait)
	}
	if !strings.Contains(seen, signalRecordFiledStartMarker) {
		t.Fatalf("run %d: the child did not reach its loop: %s", run, seen)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("run %d: SIGTERM: %v", run, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case werr := <-done:
		stopped = true
		if werr != nil {
			t.Fatalf("run %d: the door did not stop cleanly (a stop request is a success): %v", run, werr)
		}
	case <-time.After(signalRecordFiledStopWait):
		t.Fatalf("run %d: the door did not exit within %s of SIGTERM — the loop never unwound", run, signalRecordFiledStopWait)
	}
	return seen
}

// signalRecordFiledSandbox materialises the private profile through the ONE
// sandbox initializer and returns its path (the last stdout line, per
// `gopi tool sandbox create`).
func signalRecordFiledSandbox(t *testing.T, bin string) string {
	t.Helper()
	cmd := exec.Command(bin, "tool", "sandbox", "create",
		"--label", "signalrecfiled", "--task-id", signalRecordFiledID)
	cmd.Env = signalRecordFiledCleanEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tool sandbox create: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	sb := strings.TrimSpace(lines[len(lines)-1])
	if sb == "" || !filepath.IsAbs(sb) {
		t.Fatalf("tool sandbox create printed no usable path (got %q):\n%s", sb, out)
	}
	//stylegate:ignore best-effort temp cleanup; the OS/harness sweeps leftovers too
	t.Cleanup(func() { _ = os.RemoveAll(sb) })
	return sb
}

// signalRecordFiledCleanEnv is os.Environ() with the RECORDING-relevant
// variables removed, so this suite's own pin (a scratch profile, a scratch
// sessions root, and the agent signature that would make the child refuse its
// own record) cannot decide the child's behaviour.
func signalRecordFiledCleanEnv() []string {
	drop := map[string]bool{
		"GOPI_HOME": true, "GOPI_SETTINGS": true, "GOPI_SESSIONS_ROOT": true,
		"GOPI_TASK_RUN_ID": true, "GOPI_TASK_WRAPPER_ID": true,
		"GOPI_SIGNALREC_SESSION_DIR": true, "GOPI_SIGNAL_RECORD_OFF": true,
		"GOPI_MEMORY_PEERS": true, "GOPI_MEMORY_DB": true,
		"GOPI_SIGNALREC_INTENT_WAIT_MS": true,
		"GOPI_AGENT_SESSION":            true, "GOPI_EXECUTOR_SESSION_ID": true,
	}
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		name, _, ok := strings.Cut(kv, "=")
		if ok && drop[name] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// signalRecordFiledLines counts the provenance file's lines (0 when absent).
func signalRecordFiledLines(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Count(strings.TrimRight(string(raw), "\n"), "\n") + 1
}

// signalRecordFiledLast returns the file's last line ("" when absent).
func signalRecordFiledLast(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	return lines[len(lines)-1]
}
