// worker_stop_delivery_criterion_test.go — the DELIVERY CRITERION of the stop
// channel (bug 01a17df2).
//
// THE DEFECT, IN ONE SENTENCE: `gopi worker --stop` decided whether the stop had
// been DELIVERED by comparing the target's executable identity with its own
// (device+inode) — a physical proxy — while the fact the channel actually
// produces (the worker's receipt, ACK_<pid>, naming the requester's req_id) was
// already being written by the very pids the proxy was refusing to trust.
//
// WHY THE PROXY IS ALWAYS WRONG ON THE ORDINARY SHAPE: `go build -o bin/gopi`
// writes a NEW file and renames it over the old one, so the inode changes on
// every build while a running worker keeps the inode it started with — its
// /proc/<pid>/exe then reads "/path/bin/gopi (deleted)". That is not a foreign
// program; it is the SAME PATH with the bytes replaced. Measured consequence:
// three workers acknowledged a drain request normally while the sender printed
// "runs a DIFFERENT binary … requesting BY FILE ONLY" for each of them.
//
// THE THREE FACTS THIS FILE SEPARATES (they used to be one):
//
//	proven   — same device+inode: the signal is safe to add.
//	replaced — same PATH, kernel's " (deleted)" marker: an ordinary rebuild.
//	foreign  — a different FILE: its identity can never be proven.
//
// The regression construction is `cp` + `rename` over ONE path
// (Rule 5 / the briefing's own requirement): no real build runs, and the
// scenario is produced by the same mechanism real builds use.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// wsdcOldBuildBody / wsdcNewBuildBody are the two payloads of the rebuilt
	// file: their difference is what `cp` writes, and it makes the "the bytes
	// changed" half of the scenario a real change rather than an empty copy.
	wsdcOldBuildBody = "#!/bin/sh\n# the build the worker was started from\nexit 0\n"
	wsdcNewBuildBody = "#!/bin/sh\n# the build installed by cp+rename\nexit 0\n"
	// wsdcReceiptPoll is the poll cadence for a receipt the fixture worker
	// writes — the shared cadence (Rule 1: no second spelling of the same wait).
	wsdcReceiptPoll = wackReceiptPoll
	// wsdcReceiptWait bounds that poll (a load-tolerant window, not a sleep).
	wsdcReceiptWait = 15 * time.Second
)

// wsdcReplacedImage is the target spelling a fixture /proc/<pid>/exe must carry
// for the REPLACED case: a fixture symlink is resolved BY PATH, so the kernel's
// marker has to be part of the target string — the real /proc entry appends it
// itself, and reproducing the marker is reproducing the observation, not faking
// the mechanism.
func wsdcReplacedImage(path string) string {
	return path + procname.DeletedImageMarker
}

// wsdcSetExe repoints a fixture pid's /proc/<pid>/exe link — the channel
// workerBinaryIdentity reads — REPLACING the one the fixture builder planted.
// procFixtureExe cannot be reused for this: it assumes the default link is
// still there (it removes it first), while here the same pid is repointed a
// second time on purpose.
func wsdcSetExe(t *testing.T, root string, pid int, target string) {
	t.Helper()
	link := filepath.Join(root, strconv.Itoa(pid), procExeName)
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		t.Fatalf("clear the exe link for pid %d: %v", pid, err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("plant the exe link for pid %d: %v", pid, err)
	}
}

// wsdcRebuildInPlace replaces the file at path the way `go build -o` does: write
// a NEW file, then rename it over the old one. The old inode survives, pinned by
// whoever has it open — which is exactly the running process's situation.
func wsdcRebuildInPlace(t *testing.T, path, body string) {
	t.Helper()
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(body), 0o755); err != nil {
		t.Fatalf("write the replacement file %s: %v", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename %s over %s: %v", tmp, path, err)
	}
}

// TestWorkerStopSeesTheReplacedImageAsTheSamePath is the criterion's own unit
// proof: after a cp+rename install, the target's executable is NOT "a different
// binary" — it is this command's own path with the file replaced, and the code
// must say so rather than collapsing it into the foreign case.
//
// THE CONSTRUCTION IS THE MEASUREMENT (no real build, Rule 27-safe): the fixture
// /proc/<pid>/exe link points at a real file; that file is then replaced in
// place, so the link resolves to " (deleted)" while the path stayed the same.
func TestWorkerStopSeesTheReplacedImageAsTheSamePath(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	target := liveWorkerChildren(t, 1)[0]
	// ONE path for both pids — this is the whole point: a rebuild does not move
	// the binary, it replaces it.
	image := filepath.Join(root, wffSelfExe)
	if err := os.WriteFile(image, []byte(wsdcOldBuildBody), 0o755); err != nil {
		t.Fatalf("create the image %s: %v", image, err)
	}
	// The target's image is the OLD build, pinned by the running process; this
	// command's own image is the NEW one on disk. That pair IS the rebuild.
	wffFixture(t, root, home, target, image)
	procFixtureExe(t, root, os.Getpid(), binaryNameGopi, []string{"gopi", procname.VerbWorker, workerControlStop},
		os.Getpid(), image)

	// The install: cp + rename over the SAME path.
	wsdcRebuildInPlace(t, image, wsdcNewBuildBody)
	// …and the target's link now names the unlinked old file, exactly as the
	// kernel's /proc entry does for a process that survived the install.
	wsdcSetExe(t, root, target, wsdcReplacedImage(image))

	if got := workerExePath(target); got != wsdcReplacedImage(image) {
		t.Fatalf("fixture is wrong: the target's exe link resolved to %q, want %q "+
			"(the kernel's marker is what a replaced image looks like)", got, wsdcReplacedImage(image))
	}
	rel, selfPath, otherPath := workerBinaryIdentity(target)
	if rel != workerBinaryReplaced {
		t.Fatalf("workerBinaryIdentity = %v (self %q, target %q) — a cp+rename install over ONE path is the "+
			"REPLACED case (same program, previous copy), not the foreign one; classifying it as foreign is the "+
			"bug that made --stop distrust every worker after every build", rel, selfPath, otherPath)
	}
	if rel.workerBinarySignallable() {
		t.Fatalf("the replaced case must NOT be signallable: the binary loaded is not the file on disk, " +
			"so inode identity is genuinely unprovable — the request file is the delivery")
	}
}

// TestWorkerStopDeliversByHandshakeAfterAnInPlaceRebuild is the acceptance's
// regression case: with the target's image replaced in place (the ordinary
// post-build shape), the stop must still be DELIVERED and PROVEN — by the
// receipt, which is written by the worker and names this process's req_id.
//
// It also pins what must NOT appear: the inode verdict, as a reason. The old
// output said "runs a DIFFERENT binary … requesting BY FILE ONLY" for a pid that
// was running the same program; after the fix that pid's silence or answer is
// stated as itself, and a receipt that arrives is reported as PROVEN delivery.
func TestWorkerStopDeliversByHandshakeAfterAnInPlaceRebuild(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	target := liveWorkerChildren(t, 1)[0]
	image := filepath.Join(root, wffSelfExe)
	if err := os.WriteFile(image, []byte(wsdcOldBuildBody), 0o755); err != nil {
		t.Fatalf("create the image %s: %v", image, err)
	}
	wffFixture(t, root, home, target, image)
	procFixtureExe(t, root, os.Getpid(), binaryNameGopi, []string{"gopi", procname.VerbWorker, workerControlStop},
		os.Getpid(), image)
	wsdcRebuildInPlace(t, image, wsdcNewBuildBody)
	wsdcSetExe(t, root, target, wsdcReplacedImage(image))

	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve control dir: %v", err)
	}
	// THE WORKER'S OWN ACT, reproduced faithfully: it reads the request it was
	// addressed by and answers with the receipt naming THAT sender (the identity
	// the request file carries — a receipt for anyone else's request is not an
	// answer, workerAckAnswers).
	done := make(chan struct{})
	go func() {
		defer close(done)
		path := tools.ControlRequestPath(dir, workerStopRequestPrefix, target)
		deadline := time.Now().Add(wsdcReceiptWait)
		for time.Now().Before(deadline) {
			body, rerr := os.ReadFile(path)
			if rerr == nil {
				sender := parseWorkerStopSender(body)
				if sender.PID > 0 {
					if werr := workerWriteStopAck(dir, target, workerAckModeDrain, 3, sender); werr != nil {
						t.Errorf("the fixture worker could not write its receipt: %v", werr)
					}
					return
				}
			}
			time.Sleep(wsdcReceiptPoll) //stylegate:allow-sleep bounded poll for the request the ask is about to write
		}
	}()

	out := captureStdoutAndStderr(t, func() int {
		if !workerControlStopForeground([]int{target}, wackAnsweredBudget, false) {
			t.Errorf("the stop did not reach its end state for pid %d after an in-place rebuild:\n%s", target, "")
		}
		return 0
	})
	<-done

	// ① THE DELIVERY IS PROVEN, by the receipt and by the latency statement.
	if !strings.Contains(out, "ACKNOWLEDGED") {
		t.Errorf("the receipt must be reported as the delivery:\n%s", out)
	}
	if !strings.Contains(out, "not waiting for the round to finish") {
		t.Errorf("the answered stop must end the wait early:\n%s", out)
	}
	// ② THE INODE VERDICT IS GONE as a reason: no "different binary" claim, in
	// either the old wording or a synonym of it.
	for _, banned := range []string{"DIFFERENT binary", "DIFFERENT executable FILE", "signal contract we cannot verify"} {
		if strings.Contains(out, banned) {
			t.Errorf("the inode verdict %q is back — this pid runs the SAME path, and the receipt proved "+
				"delivery:\n%s", banned, out)
		}
	}
	// ③ THE REBUILD IS NAMED AS A REBUILD, not as an alien program.
	if !strings.Contains(out, "REPLACED since it started") {
		t.Errorf("the rebuild shape must be named as exactly that:\n%s", out)
	}
}

// TestWorkerStopDeliversByHandshakeWhenTheImageIsForeign keeps the OTHER half
// honest: a genuinely different executable FILE is still delivered to (the file
// is the channel) and the receipt still proves it — while the signal stays
// withheld, named by its real reason (identity not provable) rather than framed
// as an unverifiable "signal contract".
func TestWorkerStopDeliversByHandshakeWhenTheImageIsForeign(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	target := liveWorkerChildren(t, 1)[0]
	wffFixture(t, root, home, target, wffExecFile(t, filepath.Join(root, wffForeignExe)))
	procFixtureExe(t, root, os.Getpid(), binaryNameGopi, []string{"gopi", procname.VerbWorker, workerControlStop},
		os.Getpid(), wffExecFile(t, filepath.Join(root, wffSelfExe)))

	if rel, _, _ := workerBinaryIdentity(target); rel != workerBinaryForeign {
		t.Fatalf("fixture is wrong: two distinct files must classify as foreign, got %v", rel)
	}

	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve control dir: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		path := tools.ControlRequestPath(dir, workerStopRequestPrefix, target)
		deadline := time.Now().Add(wsdcReceiptWait)
		for time.Now().Before(deadline) {
			body, rerr := os.ReadFile(path)
			if rerr == nil {
				sender := parseWorkerStopSender(body)
				if sender.PID > 0 {
					if werr := workerWriteStopAck(dir, target, workerAckModeDrain, 0, sender); werr != nil {
						t.Errorf("the fixture worker could not write its receipt: %v", werr)
					}
					return
				}
			}
			time.Sleep(wsdcReceiptPoll) //stylegate:allow-sleep bounded poll for the request the ask is about to write
		}
	}()

	out := captureStdoutAndStderr(t, func() int {
		if !workerControlStopForeground([]int{target}, wackAnsweredBudget, false) {
			t.Errorf("a foreign-binary target must still be DELIVERED to and proven by its receipt")
		}
		return 0
	})
	<-done

	if !strings.Contains(out, "ACKNOWLEDGED") {
		t.Errorf("the receipt of a foreign-binary target must be reported as the delivery:\n%s", out)
	}
	if !strings.Contains(out, "DIFFERENT executable FILE") {
		t.Errorf("the foreign case must keep naming the real reason the signal is withheld:\n%s", out)
	}
	if strings.Contains(out, "cannot verify") {
		t.Errorf("the old 'signal contract we cannot verify' framing is back:\n%s", out)
	}
}

// TestWorkerResumeConsumesAStandingStopRequest pins defect B of the same bug:
// `gopi worker --resume` runs ONE resumed task and never enters a claim loop, so
// it used to read no request and write no receipt — and the sender attributed
// that silence to the target's binary AGE, which was measurably false (the
// silent pid was the youngest one alive).
//
// The resume face now consumes at start-up with its OWN exit sentence. This case
// drives the two steps that face performs — the context it builds and the consume
// it calls — and asserts the receipt ANSWERES the requester and states the resume
// face's semantics rather than promising a round boundary it does not have.
func TestWorkerResumeConsumesAStandingStopRequest(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState)

	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve control dir: %v", err)
	}
	self := os.Getpid()
	// The operator's request, written for THIS process — the shape a standing
	// TERM_<pid> has when a resume starts under it.
	if werr := workerRequestStopFile(dir, self); werr != nil {
		t.Fatalf("write the standing stop request: %v", werr)
	}
	t.Cleanup(func() { workerClearStopRequestFor(dir, self) })

	// THE RESUME FACE'S OWN WIRING, step for step (cli_worker_resume.go).
	ctx := workerRequestCtxFor(self)
	ctx.ackExit = workerAckExitResume
	if !ctx.consumesRequests() {
		t.Fatal("the resume face built a context with no request channel — it could never answer a stop")
	}
	ctx.consumeDrainRequests()
	defer ctx.cleanupRequests()

	path := workerAckPathText(dir, self)
	body, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("the resume face read a standing request but wrote NO receipt at %s (%v): that is the silence "+
			"the bug reported as 'an older binary'", path, rerr)
	}
	ack := parseWorkerAck(body, path)
	if !ack.workerAckAnswers(self) {
		t.Fatalf("the receipt does not answer this process's request (req_id=%d, want %d): the sender would "+
			"ignore it and wait out its bound:\n%s", ack.ReqID, self, ack.Raw)
	}
	if ack.Mode != workerAckModeDrain {
		t.Errorf("receipt mode = %q, want %q (a stop request is a DRAIN, never a STOP — epic 01a17da0)", ack.Mode, workerAckModeDrain)
	}
	if ack.Exit != workerAckExitResume {
		t.Errorf("the resume face's receipt must state ITS OWN exit semantics, not the claim-loop one:\n got %q\nwant %q",
			ack.Exit, workerAckExitResume)
	}
	if ack.Exit == workerAckExitSemantic {
		t.Errorf("the resume face promised a round boundary it does not have: %q", ack.Exit)
	}
}

// TestWorkerStopUnwaitedReportsTheDeliveryNotTheBinary pins the zero-bound
// answer: `--stop` with no wait must still report, per pid, whether a receipt is
// on disk — and must never render the absence of one as a statement about the
// target's executable (the retired sentence named a binary mismatch nothing had
// measured).
func TestWorkerStopUnwaitedReportsTheDeliveryNotTheBinary(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	target := liveWorkerChildren(t, 1)[0]
	image := filepath.Join(root, wffSelfExe)
	if err := os.WriteFile(image, []byte(wsdcOldBuildBody), 0o755); err != nil {
		t.Fatalf("create the image %s: %v", image, err)
	}
	wffFixture(t, root, home, target, image)
	procFixtureExe(t, root, os.Getpid(), binaryNameGopi, []string{"gopi", procname.VerbWorker, workerControlStop},
		os.Getpid(), image)
	wsdcRebuildInPlace(t, image, wsdcNewBuildBody)
	wsdcSetExe(t, root, target, wsdcReplacedImage(image))

	out := captureStdoutAndStderr(t, func() int {
		workerControlStopForeground([]int{target}, 0, false)
		return 0
	})
	if !strings.Contains(out, "request delivered") {
		t.Errorf("the zero-bound answer must state the delivery:\n%s", out)
	}
	if !strings.Contains(out, "wrote no stop receipt") {
		t.Errorf("it must state, per pid, that no receipt is on disk yet:\n%s", out)
	}
	for _, banned := range []string{"DIFFERENT binary", "OLDER binary", "older binary", "unchanged timeout path"} {
		if strings.Contains(out, banned) {
			t.Errorf("the answer invented a cause about the target's binary (%q):\n%s", banned, out)
		}
	}
	// The target is alive and was NOT signalled: the file is the delivery.
	if !tools.PidAlive(target) {
		t.Fatalf("pid %d was signalled — a replaced image gets the request file, not a signal", target)
	}
}

// TestWorkerStopSignalsNothingForAReplacedImageButStillDeliversTheFile keeps the
// two halves of the replaced case apart: no signal (identity unprovable) yet the
// durable request is on disk at the path the worker's own consume reads.
func TestWorkerStopSignalsNothingForAReplacedImageButStillDeliversTheFile(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	// A child this test owns and reaps: a signal would land in `reaped`, and its
	// absence is the assertion.
	cmd := exec.Command("/bin/sleep", "60")
	// Pdeathsig rides this DIRECT child (no fork in between), so a SIGKILLed test
	// binary cannot leave it behind; the cwd is not the subject (the image
	// identity is), so it is parked outside every checkout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.Dir = t.TempDir()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start target child: %v", err)
	}
	target := cmd.Process.Pid
	reaped := make(chan struct{})
	go func() {
		_ = cmd.Wait() //stylegate:ignore reaping our own child is the point; its error is uninteresting here
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //stylegate:ignore best-effort teardown of the test's own child
	})
	image := filepath.Join(root, wffSelfExe)
	if err := os.WriteFile(image, []byte(wsdcOldBuildBody), 0o755); err != nil {
		t.Fatalf("create the image %s: %v", image, err)
	}
	wffFixture(t, root, home, target, image)
	procFixtureExe(t, root, os.Getpid(), binaryNameGopi, []string{"gopi", procname.VerbWorker, workerControlStop},
		os.Getpid(), image)
	wsdcRebuildInPlace(t, image, wsdcNewBuildBody)
	wsdcSetExe(t, root, target, wsdcReplacedImage(image))

	workerControlStopForeground([]int{target}, 0, false)

	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve control dir: %v", err)
	}
	if _, serr := os.Stat(tools.ControlRequestPath(dir, workerStopRequestPrefix, target)); serr != nil {
		t.Fatalf("no durable request for pid %d after a replaced-image stop (%v) — the file IS the delivery", target, serr)
	}
	select {
	case <-reaped:
		t.Fatalf("pid %d was signalled: a replaced image's identity is unprovable, so no signal may be sent", target)
	case <-time.After(wffForeignAliveWait):
	}
}
