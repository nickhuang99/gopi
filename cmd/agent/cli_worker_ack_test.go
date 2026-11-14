// cli_worker_ack_test.go — the reverse channel (epic worker-stop-ack): the
// worker's receipt for a stop request, and the sender's poll for it.
//
// The three facts these cases pin, each a way the feature can be wrong:
//
//	① the WORKER writes the receipt when it CONSUMES the request — once, and
//	   only for the request it actually read (a receipt that answers any
//	   request is an answer to nothing);
//	② the SENDER ends its wait on the receipt, for a target it could not
//	   signal — the file is the channel, so a foreign binary (a different
//	   inode, `(deleted)` after a rebuild) answers exactly like the same one;
//	③ a worker that writes NO receipt keeps the old behaviour AND says what
//	   was measured, instead of waiting in silence — and invents no cause
//	   (bug 01a17df2: the old "older binary" attribution was measured false).
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// wackWait is the `--wait` bound the e2e cases declare: far longer than any
	// healthy drain needs, so the case can distinguish "answered in a moment"
	// from "waited the bound out" without racing a slow host.
	wackWait = "2m"
	// wackAnsweredBudget is what the answered stop may take. It is much smaller
	// than wackWait on purpose: the acceptance is `elapsed << bound`.
	wackAnsweredBudget = 20 * time.Second
	// wackUnansweredBound is the bound the NEVER-answering case declares. Short
	// so an unanswering worker's bound is measured in seconds, not minutes.
	wackUnansweredBound = "3s"
	// wackUnansweredSlack tolerates the CLI's own start-up and reporting around
	// the wait.
	wackUnansweredSlack = 6 * time.Second
	// wackReceiptPoll bounds a poll for a file the worker writes at its own
	// boundary.
	wackReceiptPoll = 20 * time.Millisecond
)

// wackWaitReceipt polls for a receipt file to appear and answers when it does.
func wackWaitReceipt(path string, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(wackReceiptPoll) //stylegate:allow-sleep bounded poll for the receipt a worker writes at its boundary
	}
	return false
}

// TestWorkerAckIsWrittenOnConsumeAndAnswersOnlyItsOwnRequest: the CONSUME of a
// durable request publishes one receipt, naming the requester that asked — and a
// second consume of the same request does not rewrite it (idempotency is what
// keeps the three request consumers from producing a receipt each).
func TestWorkerAckIsWrittenOnConsumeAndAnswersOnlyItsOwnRequest(t *testing.T) {
	dir := t.TempDir()
	resetWorkerStopState()
	// RESTORE, don't just clear: the consume below records a REAL stop in the
	// process-wide record, and a test that leaves it standing poisons every
	// later case in the package (measured: this line's absence made the three
	// takeover/watcher tests red in a plain `-run` selection on an unchanged
	// tree). See worker_stop_state_leak_gate_test.go.
	t.Cleanup(resetWorkerStopState)
	ctx := drainTestCtx(t, dir)

	// A request from a DIFFERENT process than the one draining: the receipt must
	// name the requester, not the reader (the whole point of the channel is that
	// the SENDER learns its request was read).
	sender := workerStopSender{PID: os.Getpid() + 1, Argv: "gopi worker --stop", Signal: workerGracefulSignalName, TS: "2026-10-14T00:00:00Z"}
	if err := tools.WriteControlText(tools.ControlRequestPath(dir, workerStopRequestPrefix, os.Getpid()),
		workerRequestBody(os.Getpid(), sender)); err != nil {
		t.Fatalf("write the request: %v", err)
	}
	ctx.consumeDrainRequests()

	path := workerAckPathText(dir, os.Getpid())
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the consume wrote no receipt at %s: %v", path, err)
	}
	a := parseWorkerAck(body, path)
	if !a.workerAckAnswers(sender.PID) {
		t.Errorf("the receipt does not answer the requesting pid: got req_id=%d want %d (body: %s)", a.ReqID, sender.PID, body)
	}
	if a.AckedBy != os.Getpid() {
		t.Errorf("acked_by = %d, want the consuming pid %d", a.AckedBy, os.Getpid())
	}
	if a.Mode != workerAckModeDrain {
		t.Errorf("mode = %q, want %q (a graceful request must not be answered as a force)", a.Mode, workerAckModeDrain)
	}
	if a.Exit != workerAckExitSemantic {
		t.Errorf("the receipt must state the exit semantics it commits to, got %q", a.Exit)
	}
	if !strings.Contains(a.Raw, workerAckFieldAckedAt) {
		t.Errorf("the receipt carries no acked_at stamp: %s", a.Raw)
	}

	// ② IDEMPOTENT: a second consume (the watcher's tick, the forwarder's own
	// re-read) must not rewrite the fact.
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the receipt: %v", err)
	}
	ctx.consumeDrainRequests()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("re-stat the receipt: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("the receipt was REWRITTEN by a repeat consume (%s → %s); a receipt is written once", before.ModTime(), after.ModTime())
	}
}

// TestWorkerAckIsNotAcceptedForAnotherRequest: the guard that makes a receipt
// usable without trusting the file name — a receipt is only this command's
// answer when it names THIS process as the requester. A recycled pid, or two
// operators on one profile, must not read each other's answers.
func TestWorkerAckIsNotAcceptedForAnotherRequest(t *testing.T) {
	dir := t.TempDir()
	if err := workerWriteStopAck(dir, 4242, workerAckModeDrain, 0,
		workerStopSender{PID: 999999, TS: "2026-10-14T00:00:00Z"}); err != nil {
		t.Fatalf("write the foreign receipt: %v", err)
	}
	poll := newWorkerAckPoll(dir, 4242)
	// The poll predates this file, so the STALENESS guard alone would refuse it;
	// back-date the start so the case tests the IDENTITY guard on its own.
	poll.start = time.Now().Add(-time.Hour)
	if _, ok := poll.seen(); ok {
		t.Fatalf("a receipt naming pid 999999 was accepted as this process's answer")
	}
}

// TestWorkerStopForegroundEndsOnTheReceiptOfAForeignTarget: the acceptance's
// core, at the seam that decides the wait. The target runs a DIFFERENT
// executable FILE (two distinct paths, as another build would be), so the only
// delivery is the request file; the receipt is what
// ends the wait, and it must end it BEFORE the process exits, because a
// foreground worker still has its in-flight round to finish.
func TestWorkerStopForegroundEndsOnTheReceiptOfAForeignTarget(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	target := liveWorkerChildren(t, 1)[0]
	wffFixture(t, root, home, target, wffExecFile(t, filepath.Join(root, wffForeignExe)))
	procFixtureExe(t, root, os.Getpid(), binaryNameGopi, []string{"gopi", procname.VerbWorker, workerControlStop},
		os.Getpid(), wffExecFile(t, filepath.Join(root, wffSelfExe)))
	if rel, _, _ := workerBinaryIdentity(target); rel.workerBinarySignallable() {
		t.Fatal("fixture is wrong: the two fixture executables must NOT be the same file")
	}
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve control dir: %v", err)
	}

	// The receipt is written by "the worker" — a goroutine standing in for the
	// pid that cannot actually consume the request (it is a sleeper). It names
	// THIS process as the requester, exactly as the real worker's consume does
	// (the sender header rides the request file the ask just wrote).
	done := make(chan struct{})
	go func() {
		defer close(done)
		path := tools.ControlRequestPath(dir, workerStopRequestPrefix, target)
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if b, rerr := os.ReadFile(path); rerr == nil {
				sender := parseWorkerStopSender(b)
				if sender.PID > 0 {
					// The consuming worker's own act, reproduced faithfully: the
					// receipt names the sender the request file carried.
					if werr := workerWriteStopAck(dir, target, workerAckModeDrain, 3, sender); werr != nil {
						t.Errorf("the fixture worker could not write its receipt: %v", werr)
					}
					return
				}
			}
			time.Sleep(wackReceiptPoll) //stylegate:allow-sleep bounded poll for the request the ask is about to write
		}
	}()

	start := time.Now()
	out := captureStdoutAndStderr(t, func() int {
		if !workerControlStopForeground([]int{target}, 60*time.Second, false) {
			t.Errorf("the stop did not reach its end state for pid %d", target)
		}
		return 0
	})
	<-done
	elapsed := time.Since(start)

	if elapsed > wackAnsweredBudget {
		t.Errorf("the stop took %s (want the receipt to end the wait well inside %s)", elapsed, wackAnsweredBudget)
	}
	if !strings.Contains(out, "ACKNOWLEDGED") {
		t.Errorf("the sender must print the worker's receipt:\n%s", out)
	}
	if !strings.Contains(out, workerAckFieldAckedBy+"=") {
		t.Errorf("the printed receipt must carry the body an operator can check:\n%s", out)
	}
	if !strings.Contains(out, "stopped waiting") {
		t.Errorf("the sender must say WHY it stopped waiting:\n%s", out)
	}
	// The sender owns the receipt once read — and "owns" now means ARCHIVES, not
	// deletes (epic 01a17df9-524a: the receipt is the only evidence that a stop
	// was acknowledged, so reading it must not destroy it). Two facts: the live
	// name is free (a later request from the same pid cannot read this answer),
	// and the evidence is still on disk, complete.
	if _, serr := os.Stat(workerAckPathText(dir, target)); serr == nil {
		t.Errorf("the read receipt is still in the LIVE namespace at %s — a later request could read it as its own answer", workerAckPathText(dir, target))
	}
	archived := wackArchivedReceipt(t, dir, 0)
	if archived.ReqID <= 0 || archived.AckedBy != target {
		t.Errorf("the archived receipt lost its evidence: req_id=%d acked_by=%d (want the requester's id and the worker pid %d)",
			archived.ReqID, archived.AckedBy, target)
	}
	if archived.AckedAt == "" || archived.Mode != workerAckModeDrain || archived.Exit != workerAckExitSemantic {
		t.Errorf("the archived receipt is incomplete: acked_at=%q mode=%q exit=%q", archived.AckedAt, archived.Mode, archived.Exit)
	}
}

// TestWorkerStopTwiceArchivesBothReceiptsE2E is THE ACCEPTANCE of epic
// 01a17df9-524a, driven through the real CLI twice against one sandbox profile:
//
//	① the first `--stop` acknowledges, and its receipt is ARCHIVED (readable
//	   afterwards, with req_id / acked_at / acked_by intact) instead of deleted;
//	② a SECOND worker, a different pid, is started and stopped; its receipt is
//	   archived too, under its OWN req_id — one archive entry does not overwrite
//	   the other, and the second stop does not read the first's answer.
//
// The two receipts are compared on req_id: they must differ, because two
// handshakes from two pids must be two facts.
func TestWorkerStopTwiceArchivesBothReceiptsE2E(t *testing.T) {
	bin := wdBin(t)
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wdEnv(t, home, settings,
		workerIdleIntervalEnv+"="+wsDaemonIdleSec, workerIdleExitsEnv+"="+wsDaemonIdleExits,
		// The resident sweep is switched OFF for this case (its own off switch,
		// not a second one), so what the assertions see is the READER's act and
		// never a tick's.
		scratch.GCIntervalEnvName+"=0")
	dir := filepath.Join(home, ".gopi", workerStateDirName)

	var reqIDs []string
	for round := 1; round <= 2; round++ {
		pid, _ := wsStartDaemon(t, bin, env)
		code, out := wdRun(t, bin, env, CliVerbWorker, workerControlStop, workerControlPIDFlag, strconv.Itoa(pid), workerControlWait, wackWait)
		if code != 0 {
			t.Fatalf("round %d: `%s %s --pid %d` exited %d:\n%s", round, CliVerbWorker, workerControlStop, pid, code, out)
		}
		if !strings.Contains(out, "ACKNOWLEDGED") {
			t.Errorf("round %d: the stop did not report the worker's receipt:\n%s", round, out)
		}
		// The ACK ends the WAIT, not the worker: the daemon still finishes its
		// in-flight round and only then releases its singleton lock. Round 2
		// starts a NEW daemon on the same profile, so it must outlive that
		// release — polling is the honest way to wait (a fixed sleep would be a
		// flake on a loaded host, which is what the first draft was).
		if !wdWaitPIDGone(t, pid, wdDaemonWait) {
			t.Fatalf("round %d: the stopped daemon pid %d never exited, so the singleton lock it holds would block round %d", round, pid, round+1)
		}
		// ① the EVIDENCE: the printed receipt is still on disk, archived, with
		// every field the operator was shown. It is found by the WORKER pid that
		// acknowledged it — round 1 already left an archive behind.
		archived := wackArchivedReceiptByWorker(t, dir, pid)
		if archived.AckedBy != pid {
			t.Errorf("round %d: the archived receipt names acked_by=%d, want the worker pid %d", round, archived.AckedBy, pid)
		}
		if archived.ReqID <= 0 || archived.AckedAt == "" || archived.Mode != workerAckModeDrain {
			t.Errorf("round %d: the archived receipt is incomplete: %+v", round, archived)
		}
		if _, err := os.Stat(workerAckPathText(dir, pid)); err == nil {
			t.Errorf("round %d: pid %d's receipt is still in the LIVE namespace", round, pid)
		}
		// The printed request id is the archived one — the operator's line and the
		// file agree, so the line can be looked up afterwards.
		reqIDs = append(reqIDs, strconv.Itoa(archived.ReqID))
		if !strings.Contains(out, "requested by pid "+reqIDs[round-1]) {
			t.Errorf("round %d: the stop printed a request id that does not match the archived receipt (%s):\n%s", round, reqIDs[round-1], out)
		}
	}
	// ② two handshakes, two facts: the ids differ (two pids, two requests), and
	// each archived receipt is still readable under its own name.
	if reqIDs[0] == reqIDs[1] {
		t.Errorf("both stops archived the SAME req_id (%s) — the archive would overwrite one handshake with the other", reqIDs[0])
	}
	kept := 0
	for _, e := range wackNamesIn(dir) {
		if workerAckArchivedReqID(e) > 0 {
			kept++
		}
	}
	if kept != 2 {
		t.Errorf("want TWO archived receipts after two stops, found %d: %v", kept, wackNamesIn(dir))
	}
	// ③ the INDEX is append-only evidence that outlives the files.
	idx, err := os.ReadFile(filepath.Join(dir, workerAckArchiveLogName))
	if err != nil {
		t.Fatalf("read the archive index: %v", err)
	}
	if got := strings.Count(string(idx), "\n"); got != 2 {
		t.Errorf("want TWO index lines for two stops, got %d:\n%s", got, idx)
	}
	for _, id := range reqIDs {
		if !strings.Contains(string(idx), id) {
			t.Errorf("the index does not record req_id %s:\n%s", id, idx)
		}
	}
}

// wackArchivedReceipt finds the archived receipt in dir (reqID 0 means "any
// archived receipt, there must be exactly one"). It FAILS the case when there is
// none: after a consume, an empty archive is the defect this epic closes.
func wackArchivedReceipt(t *testing.T, dir string, reqID int) workerAck {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the control dir %s: %v", dir, err)
	}
	var found []string
	for _, e := range entries {
		name := e.Name()
		id := workerAckArchivedReqID(name)
		if id == 0 {
			continue
		}
		if reqID != 0 && id != reqID {
			continue
		}
		found = append(found, name)
	}
	if len(found) != 1 {
		t.Fatalf("want exactly ONE archived receipt for req_id %d in %s, found %d: %v (the read receipt was not archived — nothing on this host can answer \"was that stop acknowledged?\" afterwards)",
			reqID, dir, len(found), wackNamesIn(dir))
	}
	path := filepath.Join(dir, found[0])
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the archived receipt %s: %v", path, err)
	}
	return parseWorkerAck(body, path)
}

// wackArchivedReceiptByWorker finds the archived receipt that a given WORKER pid
// acknowledged, which is what a two-round case needs: two handshakes leave two
// archives, and "the one from this round" is identified by acked_by, never by
// position in a directory listing.
func wackArchivedReceiptByWorker(t *testing.T, dir string, workerPID int) workerAck {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the control dir %s: %v", dir, err)
	}
	var found []string
	for _, e := range entries {
		name := e.Name()
		if workerAckArchivedReqID(name) == 0 {
			continue
		}
		path := filepath.Join(dir, name)
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read the archived receipt %s: %v", path, rerr)
		}
		if parseWorkerAck(body, path).AckedBy == workerPID {
			found = append(found, name)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly ONE archived receipt acknowledged by worker pid %d in %s, found %d: %v",
			workerPID, dir, len(found), wackNamesIn(dir))
	}
	path := filepath.Join(dir, found[0])
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the archived receipt %s: %v", path, err)
	}
	return parseWorkerAck(body, path)
}

// wackNamesIn lists a directory for an error message (never a gate: the caller
// has already failed).
func wackNamesIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{"<unreadable: " + err.Error() + ">"}
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestWorkerStopCommandSaysAnUnansweringWorkerKeepsTheTimeoutBehaviour is the
// BACKWARD-COMPATIBILITY case: a worker that writes no receipt must behave
// exactly as before — the operator's bound still governs — and the output must
// SAY what was measured, rather than waiting out the bound in silence.
//
// WHAT IT ASSERTS, AND WHAT IT NOW REFUSES (bug 01a17df2). The sentence used to
// attribute the silence to "a worker from an older binary … the unchanged
// timeout path". That was a GUESS: this command holds a pid, a request file and a
// deadline, and it inspects none of them for a binary's age. The case therefore
// asserts the two facts the command CAN state (the receipt is absent; the
// request file is delivered and standing) and REFUSES the invented causes — a
// whole class, not one spelling, so a re-worded guess cannot slip back in.
//
// The target is a live process with the worker's shape that consumes nothing, so
// the bound really expires: the case asserts the OLD end state (exit non-zero,
// "STILL running") AND the NEW sentence.
func TestWorkerStopCommandSaysAnUnansweringWorkerKeepsTheTimeoutBehaviour(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	target := liveWorkerChildren(t, 1)[0]
	wffFixture(t, root, home, target, wffExecFile(t, filepath.Join(root, wffForeignExe)))
	procFixtureExe(t, root, os.Getpid(), binaryNameGopi, []string{"gopi", procname.VerbWorker, workerControlStop},
		os.Getpid(), wffExecFile(t, filepath.Join(root, wffSelfExe)))

	start := time.Now()
	out := captureStdoutAndStderr(t, func() int {
		if workerControlStopForeground([]int{target}, wackUnansweredBoundDur, false) {
			t.Errorf("a target that never answers and never exits must NOT report success")
		}
		return 1
	})
	elapsed := time.Since(start)

	// ① WHAT WAS MEASURED: no receipt was observed, and the request itself is
	// still on disk — the two facts the command actually holds.
	if !strings.Contains(out, "no stop receipt") {
		t.Errorf("the sender must state the measurement (no receipt was observed):\n%s", out)
	}
	if !strings.Contains(out, "DELIVERED and still standing") {
		t.Errorf("the sender must name the fact that survives the silence (the request file):\n%s", out)
	}
	// ② WHAT IT MUST NOT INVENT: a cause about the target's binary. The class is
	// checked, not one phrasing, because the defect WAS the phrasing and a
	// re-worded guess would slip back in unchanged in substance.
	for _, banned := range []string{"OLDER binary", "older binary", "unchanged timeout path", "does not acknowledge stop requests"} {
		if strings.Contains(out, banned) {
			t.Errorf("the sender invented a cause it never measured (%q):\n%s", banned, out)
		}
	}
	if !strings.Contains(out, "STILL running") {
		t.Errorf("the unchanged timeout verdict must still be printed:\n%s", out)
	}
	if elapsed < wackUnansweredBoundDur {
		t.Errorf("the wait ended in %s, before the %s bound — the bound is what governs for an unanswering worker", elapsed, wackUnansweredBoundDur)
	}
	if elapsed > wackUnansweredBoundDur+wackUnansweredSlack {
		t.Errorf("the wait ran %s, far past the %s bound", elapsed, wackUnansweredBoundDur)
	}
	// Nothing was consumed, so no receipt can exist — and no READER may have
	// invented one.
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve control dir: %v", err)
	}
	if _, serr := os.Stat(workerAckPathText(dir, target)); serr == nil {
		t.Errorf("a receipt appeared for a worker that never wrote one")
	}
}

// TestWorkerAwaitGoneSeparatesTheReceiptFromTheExit is the regression guard for
// the bug this change itself introduced (Rule 5 — the fix ships its own
// repro). The receipt made the stop-wait return while the worker is STILL
// finishing its round, and `--restart` read that `true` as "the process is
// gone": it re-resolved, saw the same pid alive, and took the "already running
// again — not starting a second" branch, so `--restart` started nothing and left
// the old daemon in place (measured: TestWorkerDaemon_RestartReplacesTheDaemon).
//
// The contract under test: "answered" and "exited" are TWO events, and a caller
// whose promise is the second must be able to ask for it separately.
func TestWorkerAwaitGoneSeparatesTheReceiptFromTheExit(t *testing.T) {
	t.Run("a live process is not gone", func(t *testing.T) {
		child := liveWorkerChildren(t, 1)[0]
		if workerAwaitGone(child, 50*time.Millisecond) {
			t.Errorf("workerAwaitGone reported pid %d gone while it is alive — an acknowledged-but-running worker would be read as exited", child)
		}
	})
	t.Run("an already-gone pid answers immediately", func(t *testing.T) {
		root := t.TempDir()
		dead := spawnExitingProcess(t, root)
		if !workerAwaitGone(dead, 5*time.Second) {
			t.Errorf("workerAwaitGone did not observe pid %d leaving within the bound", dead)
		}
	})
}

// spawnExitingProcess starts a process that exits on its own and returns its pid,
// so the wait observes a REAL exit (not a kill, not a simulated one).
func spawnExitingProcess(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the exiting process: %v", err)
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }() //stylegate:ignore reaps this test's own child; the assertion is the /proc reading, not its status
	return pid
}

// wackUnansweredBoundDur is wackUnansweredBound as a duration, kept beside the
// spelling the case passes to the wait so the two cannot drift (Rule 1b).
var wackUnansweredBoundDur = func() time.Duration {
	d, err := time.ParseDuration(wackUnansweredBound)
	if err != nil {
		panic("wackUnansweredBound is not a duration: " + err.Error()) //panic-ok: a malformed TEST constant must fail loudly at package init, never silently shorten the assertion
	}
	return d
}()

// TestWorkerStopDaemonEndsOnTheReceiptE2E is the ACCEPTANCE (Rule 31): a real
// `gopi worker --daemon` under its own profile, stopped by a real
// `gopi worker --stop --wait 2m`, where the command returns as soon as the
// daemon's receipt lands — measured against the bound it declared, and against
// the receipt's own body, which it must print.
func TestWorkerStopDaemonEndsOnTheReceiptE2E(t *testing.T) {
	bin := wdBin(t)
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wdEnv(t, home, settings, workerIdleIntervalEnv+"="+wsDaemonIdleSec, workerIdleExitsEnv+"="+wsDaemonIdleExits)

	pid, _ := wsStartDaemon(t, bin, env)
	dir := filepath.Join(home, ".gopi", workerStateDirName)

	start := time.Now()
	code, out := wdRun(t, bin, env, CliVerbWorker, workerControlStop, workerControlPIDFlag, strconv.Itoa(pid), workerControlWait, wackWait)
	elapsed := time.Since(start)
	if code != 0 {
		t.Fatalf("`%s %s --pid %d` exited %d — the stop did not reach its end state:\n%s",
			CliVerbWorker, workerControlStop, pid, code, out)
	}
	if !strings.Contains(out, "ACKNOWLEDGED") {
		t.Errorf("the stop must print the worker's receipt:\n%s", out)
	}
	if elapsed > wackAnsweredBudget {
		t.Errorf("the stop took %s (declared %s) — the wait did not end on the receipt", elapsed, wackWait)
	}
	if !wdWaitPIDGone(t, pid, wdDaemonWait) {
		t.Fatalf("pid %d is still alive after an acknowledged stop", pid)
	}
	// The receipt was READ, so it left the LIVE namespace — and it was ARCHIVED,
	// not deleted (epic 01a17df9-524a). The stop printed a request id the
	// operator may still have to look up days later, so the file it printed must
	// still be on this host.
	if _, err := os.Stat(workerAckPathText(dir, pid)); err == nil {
		t.Errorf("the read receipt is still in the LIVE namespace at %s", workerAckPathText(dir, pid))
	}
	archived := wackArchivedReceipt(t, dir, 0)
	if archived.AckedBy != pid {
		t.Errorf("the archived receipt names acked_by=%d, want the stopped worker %d", archived.AckedBy, pid)
	}
	if archived.ReqID <= 0 {
		t.Errorf("the archived receipt carries no req_id: %+v", archived)
	}
}

// TestWorkerAckConsumeArchivesTheEvidence: the DEFECT this epic closes, at the
// seam where it happened. `--stop` printed the receipt (req_id / acked_at /
// acked_by) and then DELETED the file, so thirty seconds later the only record
// that the stop was ever acknowledged was the operator's scrollback. Reading is
// not destroying: the file leaves the poll's namespace and lands in the archive,
// complete, with one index line recording the fact.
func TestWorkerAckConsumeArchivesTheEvidence(t *testing.T) {
	dir := t.TempDir()
	// The receipt must also pass the STALENESS half of the acceptance
	// (workerAckAnswersRequest): a request stamped before the poll started is
	// somebody else's, so the fixture stamps it now, as a real sender does.
	// req_id IS the requesting process's own pid — that is what the channel
	// means by "which request is this" (workerAckPoll.reqID is os.Getpid()), so
	// the fixture names this process, exactly as a real sender does.
	sender := workerStopSender{PID: os.Getpid(), Argv: "gopi worker --stop", Signal: workerGracefulSignalName, TS: time.Now().UTC().Format(time.RFC3339)}
	if err := workerWriteStopAck(dir, 7777, workerAckModeDrain, 12, sender); err != nil {
		t.Fatalf("write the receipt: %v", err)
	}
	live := workerAckPathText(dir, 7777)

	poll := newWorkerAckPoll(dir, 7777)
	poll.start = time.Now().Add(-time.Minute) // the poll predates nothing here; identity is what is tested
	a, ok := poll.seen()
	if !ok {
		t.Fatalf("the poll did not accept its own receipt at %s", live)
	}
	poll.consume(a)

	if _, err := os.Stat(live); err == nil {
		t.Fatalf("the LIVE receipt still exists at %s — a later request from this pid could read it as its own answer", live)
	}
	archived := wackArchivedReceipt(t, dir, sender.PID)
	// EVERY field survives: these are exactly what an operator asks for days
	// later, and the whole point is that they are not merely printed.
	checks := []struct {
		name string
		got  string
		want string
	}{
		{"req_id", strconv.Itoa(archived.ReqID), strconv.Itoa(sender.PID)},
		{"req_ts", archived.ReqTS, sender.TS},
		{"acked_by", strconv.Itoa(archived.AckedBy), "7777"},
		{"mode", archived.Mode, workerAckModeDrain},
		{"round", strconv.Itoa(archived.Round), "12"},
		{"exit", archived.Exit, workerAckExitSemantic},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("the archived receipt lost %s: got %q want %q (body: %s)", c.name, c.got, c.want, archived.Raw)
		}
	}
	if archived.AckedAt == "" {
		t.Errorf("the archived receipt lost acked_at — the timestamp the epic exists to preserve (body: %s)", archived.Raw)
	}

	// The INDEX LINE, not just the file: it is what survives the retention
	// sweep, so it must name the same facts.
	logPath := filepath.Join(dir, workerAckArchiveLogName)
	idx, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read the archive index %s: %v", logPath, err)
	}
	if n := strings.Count(string(idx), "\n"); n != 1 {
		t.Errorf("want exactly ONE index line after one consume, got %d:\n%s", n, idx)
	}
	for _, want := range []string{strconv.Itoa(sender.PID), "7777", workerAckModeDrain, filepath.Base(archived.Path)} {
		if !strings.Contains(string(idx), want) {
			t.Errorf("the index line does not carry %q:\n%s", want, idx)
		}
	}
}

// TestWorkerAckArchivedReceiptDoesNotAnswerALaterRequest: the reason the archive
// is a SEPARATE namespace rather than "leave the file where it is". The request
// channel is keyed on the pid, so the same pid asks again (a restart, a recycled
// pid) and the answer to the PREVIOUS stop must not be readable as the answer to
// this one — a sender that "acknowledges" its own request by finding yesterday's
// receipt is worse than one that waits.
func TestWorkerAckArchivedReceiptDoesNotAnswerALaterRequest(t *testing.T) {
	dir := t.TempDir()
	first := workerStopSender{PID: os.Getpid(), Argv: "gopi worker --stop", Signal: workerGracefulSignalName, TS: time.Now().UTC().Format(time.RFC3339)}
	if err := workerWriteStopAck(dir, 6060, workerAckModeDrain, 1, first); err != nil {
		t.Fatalf("write the first receipt: %v", err)
	}
	p1 := newWorkerAckPoll(dir, 6060)
	p1.start = time.Now().Add(-time.Hour)
	a, ok := p1.seen()
	if !ok {
		t.Fatal("the first receipt was not accepted")
	}
	p1.consume(a)

	// ① the ARCHIVE is not the channel: a poll for the same request finds
	// nothing, because the live name is gone. A receipt left in place would let
	// a second stop read the FIRST answer and report "acknowledged" without
	// anybody having read the new request.
	p2 := newWorkerAckPoll(dir, 6060)
	p2.start = time.Now().Add(-time.Hour)
	if got, ok := p2.seen(); ok {
		t.Fatalf("after the consume, the poll still read a receipt at %s: %+v", got.Path, got)
	}

	// ② the channel still works for the same pid: the worker's own writer
	// produces a LIVE receipt for the next request, and it answers THAT one.
	second := workerStopSender{PID: os.Getpid(), Argv: "gopi worker --stop", Signal: workerGracefulSignalName, TS: time.Now().UTC().Add(time.Second).Format(time.RFC3339)}
	if err := workerWriteStopAck(dir, 6060, workerAckModeForce, 2, second); err != nil {
		t.Fatalf("write the second receipt: %v", err)
	}
	livePath := workerAckPathText(dir, 6060)
	body, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatalf("the second request produced no LIVE receipt at %s: %v (an archived receipt must not block the channel)", livePath, err)
	}
	live := parseWorkerAck(body, livePath)
	if live.ReqTS != second.TS {
		t.Errorf("the live receipt answers req_ts=%q, want the second request %q (a stale answer would be reported as this request's)", live.ReqTS, second.TS)
	}
	if live.Mode != workerAckModeForce {
		t.Errorf("the live receipt says mode=%q, want %q", live.Mode, workerAckModeForce)
	}

	// ③ and the archive still holds the FIRST request, unchanged and complete —
	// the evidence is what this epic exists to keep.
	archived := wackArchivedReceipt(t, dir, first.PID)
	if archived.AckedBy != 6060 || archived.Mode != workerAckModeDrain || archived.ReqTS != first.TS {
		t.Errorf("the archived receipt was disturbed by the second handshake: %+v", archived)
	}
}

// TestWorkerAckSweepRetiresByAgeOnOneRule: the reclaim leg, both paths, ONE
// rule. The normal path archives on read (the sender's act); the crash path is a
// sender that died before reading, which only the resident sweep can retire —
// and it archives first, exactly like the reader does. Collection by age is then
// the same for both.
func TestWorkerAckSweepRetiresByAgeOnOneRule(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	write := func(pid int, reqID int, mode string, ackedAt time.Time) string {
		body := strings.Join([]string{
			workerAckFieldReqID + workerAckSep + strconv.Itoa(reqID),
			workerAckFieldReqTS + workerAckSep + ackedAt.Format(time.RFC3339),
			workerAckFieldAckedAt + workerAckSep + ackedAt.Format(time.RFC3339),
			workerAckFieldAckedBy + workerAckSep + strconv.Itoa(pid),
			workerAckFieldMode + workerAckSep + mode,
			workerAckFieldExit + workerAckSep + workerAckExitSemantic,
		}, "\n") + "\n"
		if err := tools.WriteControlText(workerAckPathText(dir, pid), []byte(body)); err != nil {
			t.Fatalf("write the fixture receipt for pid %d: %v", pid, err)
		}
		return workerAckPathText(dir, pid)
	}
	// ① a LIVE receipt nobody read, older than the orphan age: archived.
	orphan := write(9001, 9001, workerAckModeDrain, now.Add(-workerAckOrphanStaleAge-time.Hour))
	// ② a LIVE receipt nobody read, recent: untouched (its sender may still be
	// polling — an ACT of reading is what archives it).
	fresh := write(9002, 9002, workerAckModeDrain, now.Add(-time.Minute))
	// ③ an ARCHIVED receipt past retention: collected.
	old := write(9003, 9003, workerAckModeDrain, now.Add(-workerAckArchiveRetention-time.Hour))
	if err := os.Rename(old, workerAckArchivedPath(old, 9003)); err != nil {
		t.Fatalf("age the archived receipt: %v", err)
	}
	// ④ an ARCHIVED receipt inside retention: kept (an investigation may still
	// need it).
	keep := write(9004, 9004, workerAckModeForce, now.Add(-time.Hour))
	if err := os.Rename(keep, workerAckArchivedPath(keep, 9004)); err != nil {
		t.Fatalf("archive the recent receipt: %v", err)
	}

	archived, removed, errs := sweepWorkerAckArchive(dir, now)
	for _, e := range errs {
		t.Errorf("the sweep reported an error: %v", e)
	}
	if archived != 1 {
		t.Errorf("archived = %d, want 1 (the unread receipt past the orphan age)", archived)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1 (the archived receipt past retention)", removed)
	}
	if _, err := os.Stat(orphan); err == nil {
		t.Errorf("the unread receipt %s is still LIVE past the orphan age — nothing else on this host will ever retire it", orphan)
	}
	if a := wackArchivedReceipt(t, dir, 9001); a.AckedBy != 9001 {
		t.Errorf("the crashed sender's receipt was not archived with its evidence: %+v", a)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("the recent unread receipt was retired early: %v", err)
	}
	if _, err := os.Stat(workerAckArchivedPath(old, 9003)); err == nil {
		t.Errorf("the archived receipt past retention %s was not collected", workerAckArchivedPath(old, 9003))
	}
	if _, err := os.Stat(workerAckArchivedPath(keep, 9004)); err != nil {
		t.Errorf("the archived receipt inside retention was collected: %v", err)
	}

	// The archive index is NOT swept: the fact outlives every file (that is the
	// whole reason it is appended).
	if _, err := os.Stat(filepath.Join(dir, workerAckArchiveLogName)); err != nil {
		t.Fatalf("the sweep touched the index: %v", err)
	}
}
