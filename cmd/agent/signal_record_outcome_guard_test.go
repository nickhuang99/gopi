package main

// signal_record_outcome_guard_test.go — the regression guard for the THIRD state
// of the record-filed handshake (epic: a claiming door must not print a FALSE
// LOSS for a record the recorder deliberately REFUSED).
//
// THE DEFECT IT PINS (measured 2026-10-12, task 01a12fba). The handshake had two
// published states and needed three. `settleProvenanceRecord` waited on
// signalrec.AwaitRecordFiled — whose only fact is "bytes on disk" — and then
// decided HOW TO REPORT the failure from a single check, KillVerdictNow().Received:
//
//	a signal arrived + no record on disk  ⟹  recordLostLine
//	                                        ("this kill is recorded NOWHERE")
//
// A REFUSED record (reportUnfiled printing "[gopi] signal provenance NOT FILED (no
// public-dir fallback): …") satisfies exactly that condition — the recorder
// answered, deliberately, and published NOTHING — so a rejection the recorder had
// already explained, on purpose, was reported to the operator as an invisible
// loss. Two different facts, one sentence: the same collapse this handshake family
// exists to end, one level up.
//
// WHAT THE FIX IS, AND WHAT THIS GUARD PROVES. The recorder now publishes a THIRD
// state (signalrec.RecordOutcomeNow: nothing-yet / filed / refused + reason,
// published from reportUnfiled because that is where the refusal is decided), and
// the door reads it OUTSIDE the wait — so the wait's primary fact stays "bytes on
// disk" and settles no earlier than it did. The guard drives the REAL binary in a
// REAL sandbox profile under a REAL SIGTERM and asserts the three arms:
//
//	(a) REFUSED      — the refusal reason is printed, recordLostLine is NOT;
//	(b) FILED        — the record is on disk, recordLostLine is NOT;
//	(c) NOTHING YET  — a signal arrived, no record, no refusal: recordLostLine
//	                   STILL prints. The loss must stay visible; a fix that
//	                   silenced this arm would reinstate the silent loss.
//
// Arm (c) is produced by an EXISTING knob and no new dependency (Rule 35): the
// recorder's classification budget (signalrec.IntentWaitEnv) is raised past the
// door's own budget (signalrec.DefaultRecordFiledWait), which is a real ordering —
// the recorder is still working when the door's unwind runs out of time — not a
// synthetic stall. Each arm gets a FRESH session dir: the classification answers
// immediately once a provenance file exists in the dir, so reusing one dir would
// let an earlier run's file decide a later arm.
//
// WHY REAL SUBPROCESSES (Rule 27, and the reason repeats). These are OS-level
// handlers racing each other, so an in-process test cannot pose the question; and
// a race is not measured once. The repetitions are bounded
// (signalRecordOutcomeRuns) and each is a tiny process that fails fast against an
// unresolvable peer: three processes, no device, nothing saturated.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/signalrec"
)

const (
	// signalRecordOutcomeRuns is the per-arm repetition count: the door's report
	// is decided on a different goroutine than the recorder's answer, so one run
	// cannot distinguish "fixed" from "got lucky" (Rule 27 bounds it: tiny
	// processes, never load).
	signalRecordOutcomeRuns = 3
	// signalRecordOutcomeStartWait bounds the wait for the loop's start line —
	// the deterministic point at which the process is inside the loop it will
	// unwind from and the recorder is armed.
	signalRecordOutcomeStartWait = 20 * time.Second
	// signalRecordOutcomeStopWait bounds the wait for the door to exit after the
	// STOP signal. Generous on purpose: a door that WAITS takes longer to die, and
	// this guard must fail on a WRONG REPORT, never on a slow machine.
	signalRecordOutcomeStopWait = 30 * time.Second
	// signalRecordOutcomeRefusedID names the arm (a) handoff: a directory that
	// EXISTS and is NOT a session dir (no metadata.json, no *.jsonl), which is the
	// one shape signalrec refuses as a proven-wrong handoff — the same refusal the
	// operator meets in the wild (a launcher handing a dir this profile did not
	// mint). It is a fixed literal so the fixture is readable.
	signalRecordOutcomeRefusedID = "fixture-signalrec-outcome-refused"
	// signalRecordOutcomeFiledID names the arm (b) handoff. The dir IS keyed by
	// this id (the guard creates it with the session marker), so the record is
	// filed under the task-run layout rather than merely a handed session dir.
	signalRecordOutcomeFiledID = "fixture-signalrec-outcome-filed"
	// signalRecordOutcomeLostID names the arm (c) handoff, likewise keyed and
	// marked — the refusal must NOT be what this arm measures.
	signalRecordOutcomeLostID = "fixture-signalrec-outcome-lost"
	// signalRecordOutcomeLostIntentWaitMS is the recorder's classification budget
	// for arm (c), well past signalrec.DefaultRecordFiledWait (500ms): the
	// recorder is still classifying when the door's unwind expires.
	//
	// The VALUE is deliberately larger than the door's budget and small enough
	// that the child dies promptly; it is a constant (Rule 1) rather than a
	// literal at the call site.
	signalRecordOutcomeLostIntentWaitMS = 3500
	// signalRecordOutcomeStartMarker is the stable fragment of the door's start
	// line — the handshake that says the recorder is armed and this process is
	// inside the loop it will unwind from. It is the point the arms kill at, so
	// no arm ever depends on a blind sleep (Rule 1: the fragment is a named
	// constant, not a literal at the comparison site). ONE definition, shared
	// with the record-filed guard beside this file (Rule 14).
	signalRecordOutcomeStartMarker = signalRecordDoorStartMarker
)

// TestSignalProvenanceDoorNeverClaimsALossForARefusedRecord is the guard.
func TestSignalProvenanceDoorNeverClaimsALossForARefusedRecord(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: skip binary E2E")
	}
	bin := gopiTestBinPath(t)
	// The private profile comes from the ONE initializer (Rule 34), shared by the
	// three arms: each arm only touches its own session dir.
	sb := signalRecordFiledSandbox(t, bin)
	sessRoot := filepath.Join(sb, ".gopi", "sessions")

	refusedDir := filepath.Join(sessRoot, signalRecordOutcomeRefusedID)
	if err := os.MkdirAll(refusedDir, 0o755); err != nil {
		t.Fatalf("seed the non-session handoff dir %s: %v", refusedDir, err)
	}
	filedDir := signalRecordOutcomeSeedDir(t, sessRoot, signalRecordOutcomeFiledID)
	lostDir := signalRecordOutcomeSeedDir(t, sessRoot, signalRecordOutcomeLostID)
	filedProv := filepath.Join(filedDir, signalrec.FileName)

	t.Run("refused", func(t *testing.T) {
		for run := 1; run <= signalRecordOutcomeRuns; run++ {
			out := signalRecordOutcomeRun(t, bin, sb, run, []string{
				"GOPI_TASK_RUN_ID=" + signalRecordOutcomeRefusedID,
				signalrec.SessionDirEnv + "=" + refusedDir,
			})
			// (a) the refusal is REPORTED — with the recorder's own reason, so the
			// operator can act on it...
			if !strings.Contains(out, signalrec.RefusalMarker) {
				t.Fatalf("run %d/%d: the recorder refused the record but the refusal line (%q) is not in the child's output — the refusal must stay reported (Rule 15)\n--- child output ---\n%s",
					run, signalRecordOutcomeRuns, signalrec.RefusalMarker, out)
			}
			// ...and the door must NOT restate it as an invisible loss.
			if strings.Contains(out, recordLostLine) {
				t.Fatalf("run %d/%d: the door printed recordLostLine for a record the recorder DELIBERATELY REFUSED — a false loss claim for a rejection that was already reported with its own reason\n--- child output ---\n%s",
					run, signalRecordOutcomeRuns, out)
			}
			// The door states the third state instead, so its silence is never
			// read as "the record is still coming".
			if !strings.Contains(out, recordRefusedLine) {
				t.Fatalf("run %d/%d: the door printed neither recordLostLine (correct) nor recordRefusedLine — the refusal must be stated by the door, not left implicit\n--- child output ---\n%s",
					run, signalRecordOutcomeRuns, out)
			}
			if lines := signalRecordFiledLines(t, filepath.Join(refusedDir, signalrec.FileName)); lines != 0 {
				t.Fatalf("run %d/%d: a refused record reached disk (%s has %d lines) — this arm is measuring the wrong defect", run, signalRecordOutcomeRuns, refusedDir, lines)
			}
		}
	})

	t.Run("filed", func(t *testing.T) {
		for run := 1; run <= signalRecordOutcomeRuns; run++ {
			before := signalRecordFiledLines(t, filedProv)
			out := signalRecordOutcomeRun(t, bin, sb, run, []string{
				"GOPI_TASK_RUN_ID=" + signalRecordOutcomeFiledID,
				signalrec.SessionDirEnv + "=" + filedDir,
			})
			// (b) the record IS on disk: the handshake's ordinary success.
			if after := signalRecordFiledLines(t, filedProv); after <= before {
				t.Fatalf("run %d/%d: the record was not filed (%s: %d -> %d)\n--- child output ---\n%s",
					run, signalRecordOutcomeRuns, filedProv, before, after, out)
			}
			if !strings.Contains(signalRecordFiledLast(t, filedProv), signalrec.SignalSIGTERM) {
				t.Fatalf("run %d/%d: the new provenance line is not this kill's: %s",
					run, signalRecordOutcomeRuns, signalRecordFiledLast(t, filedProv))
			}
			if strings.Contains(out, recordLostLine) {
				t.Fatalf("run %d/%d: the door claimed a LOSS although the record is on disk\n--- child output ---\n%s",
					run, signalRecordOutcomeRuns, out)
			}
			if strings.Contains(out, signalrec.RefusalMarker) {
				t.Fatalf("run %d/%d: the recorder refused a handoff that names a real session dir — this arm is measuring the wrong defect\n--- child output ---\n%s",
					run, signalRecordOutcomeRuns, out)
			}
		}
	})

	// (c) THE LOSS MUST STAY VISIBLE. This arm is the guard's own floor: a future
	// change that silenced recordLostLine by making the door settle on "we tried"
	// (or by treating every false as a refusal) would leave (a) and (b) green while
	// destroying the handshake's whole purpose.
	t.Run("nothing_yet", func(t *testing.T) {
		for run := 1; run <= signalRecordOutcomeRuns; run++ {
			out := signalRecordOutcomeRun(t, bin, sb, run, []string{
				"GOPI_TASK_RUN_ID=" + signalRecordOutcomeLostID,
				signalrec.SessionDirEnv + "=" + lostDir,
				signalrec.IntentWaitEnv + "=" + strconv.Itoa(signalRecordOutcomeLostIntentWaitMS),
			})
			if strings.Contains(out, signalrec.RefusalMarker) {
				t.Fatalf("run %d/%d: the recorder refused this record — arm (c) measures the recorder OUTRUN not a refusal\n--- child output ---\n%s",
					run, signalRecordOutcomeRuns, out)
			}
			if !strings.Contains(out, recordLostLine) {
				t.Fatalf("run %d/%d: a signal arrived, no record landed and the recorder refused nothing — the loss MUST still be reported (a door that goes quiet here is the silent loss this handshake exists to remove)\n--- child output ---\n%s",
					run, signalRecordOutcomeRuns, out)
			}
		}
	})
}

// signalRecordOutcomeSeedDir materialises one arm's session dir with the session
// marker — the proof signalrec's resolver requires before it will file a record
// there (without it the arm would measure a REFUSAL instead).
func signalRecordOutcomeSeedDir(t *testing.T, sessRoot, id string) string {
	t.Helper()
	dir := filepath.Join(sessRoot, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed the session dir %s: %v", dir, err)
	}
	marker := filepath.Join(dir, "metadata.json")
	if err := os.WriteFile(marker, []byte(`{"session_id":"`+id+`"}`), 0o644); err != nil {
		t.Fatalf("write the session marker %s: %v", marker, err)
	}
	return dir
}

// signalRecordOutcomeRun drives ONE real run: start the verb under the sandbox
// profile plus this arm's extra environment, wait for the loop's start line (never
// a blind sleep), STOP it for real, and return the child's combined output.
//
// The child is stopped in EVERY arm: a guard that leaked a live loop would leave a
// process behind for the next case (and for the sandbox reclaimer).
func signalRecordOutcomeRun(t *testing.T, bin, sandbox string, run int, extra []string) string {
	t.Helper()
	// THE CLAIMING DOOR is `gopi worker` — the same never-returning loop the
	// record-filed guard drives, and the ONE surviving claiming door that needs
	// no model, no peer and no network. The fixture verb this arm used
	// (`memory sync --every`, the cross-node sync family) was DELETED 2026-11-05
	// with the whole ssh ledger family; a guard that keeps typing a deleted verb
	// measures `unknown subcommand`, not the handshake.
	cmd := exec.Command(bin, "worker")
	cmd.Env = append(signalRecordFiledCleanEnv(),
		"GOPI_HOME="+sandbox,
		"GOPI_SETTINGS="+filepath.Join(sandbox, ".gopi", "settings.json"),
		"GOPI_SESSIONS_ROOT="+filepath.Join(sandbox, ".gopi", "sessions"),
		"GOPI_MEMORY_DB="+filepath.Join(sandbox, ".gopi", "memory.db"),
	)
	cmd.Env = append(cmd.Env, extra...)

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("run %d: stdout pipe: %v", run, err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatalf("run %d: start %s: %v", run, bin, err)
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = cmd.Process.Signal(syscall.SIGKILL) //stylegate:ignore signals the child the test spawned; the transition it causes is what is asserted
			_, _ = cmd.Process.Wait()               //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
		}
	}()

	started := make(chan struct{}, 1)
	// The child's output is accumulated into a SHARED buffer under a mutex, and
	// the start marker only SIGNALS — it must not stop the accumulation, because
	// the lines this guard asserts on (the refusal, the door's report) are printed
	// AFTER it, on the child's way out.
	var outMu sync.Mutex
	out := make([]byte, 0, 4096)
	go func() {
		tmp := make([]byte, 512)
		for {
			n, rerr := pipe.Read(tmp)
			outMu.Lock()
			out = append(out, tmp[:n]...)
			reached := strings.Contains(string(out), signalRecordOutcomeStartMarker)
			outMu.Unlock()
			if reached {
				select {
				case started <- struct{}{}:
				default:
				}
			}
			if rerr != nil {
				select {
				case started <- struct{}{}:
				default:
				}
				return
			}
		}
	}()

	select {
	case <-started:
	case <-time.After(signalRecordOutcomeStartWait):
		t.Fatalf("run %d: the loop never announced itself within %s — nothing was measured", run, signalRecordOutcomeStartWait)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("run %d: SIGTERM: %v", run, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		stopped = true
	case <-time.After(signalRecordOutcomeStopWait):
		t.Fatalf("run %d: the door did not exit within %s of SIGTERM — the loop never unwound", run, signalRecordOutcomeStopWait)
	}
	// The exit status is NOT the assertion (it was 0 on the pre-fix tree too, which
	// is why the false loss claim went unnoticed) — the printed lines are.
	outMu.Lock()
	defer outMu.Unlock()
	return string(out)
}
