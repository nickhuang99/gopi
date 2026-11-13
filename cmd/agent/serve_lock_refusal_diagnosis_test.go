package main

// serve_lock_refusal_diagnosis_test.go — the CONTENTION refusal must pair the
// pid STAMPED into serve.lock with that pid's LIVENESS, and must report the
// kernel's own record for the inode the acquirer was actually refused on.
//
// The defect (measured twice): a refusal read "another merge daemon is already
// running (… held by pid 1630650)" — c50e7f2a's E2E — and the operator's own
// session hit the same with pid 1653050 — while the named pid was GONE. A flock
// lives on the OPEN FILE DESCRIPTION and is not invalidated by unlink/replace,
// so a descriptor copy inherited by a child (os/exec ExtraFiles) keeps the lock
// alive after the process that STAMPED the file died: the stamp is a
// diagnostic, not a holder.
//
// The instrument is a REAL held flock in this process plus a REAL reaped child
// pid for the dead half — no synthetic load, no fabricated /proc (Rule 27).

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tools"
)

// serveLockRefusalDiagHelperEnv drives the re-exec'd helper process whose ONLY
// job is to exit immediately: the parent reaps it, which leaves a pid that is
// PROVABLY dead (the "stamped pid is a corpse" half of the refusal).
const serveLockRefusalDiagHelperEnv = "GOPI_TEST_SERVE_LOCK_DIAG_HELPER"

// TestServeLockRefusalDiagHelperProcess is not a test: it is the re-exec target
// that produces a reaped (guaranteed-dead) pid.
func TestServeLockRefusalDiagHelperProcess(t *testing.T) {
	if os.Getenv(serveLockRefusalDiagHelperEnv) != "1" {
		t.Skip("helper process body — re-exec target of TestServeLockRefusalPairsTheStampedPidWithItsLiveness")
	}
}

// deadReapedPid returns a pid that is provably gone: a child re-exec of this
// test binary, waited for.
func deadReapedPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestServeLockRefusalDiagHelperProcess")
	cmd.Env = append(os.Environ(), serveLockRefusalDiagHelperEnv+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the short-lived helper: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the helper must exit cleanly: %v", err)
	}
	if tools.PidAlive(pid) {
		t.Fatalf("pid %d is still alive after Wait — the dead-stamp half cannot be tested", pid)
	}
	return pid
}

// lockFileWithFlock opens <gopi dir>/serve.lock and takes the exclusive flock on
// it, returning the held descriptor (the shape a CONTENTION refusal is built
// from) and its path.
func lockFileWithFlock(t *testing.T) (*os.File, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)            // serveLockPathResolved resolves via config.GopiDir
	t.Setenv(config.GopiDirEnv, dir) // …and GOPI_HOME outranks HOME, so pin it too
	path, err := serveLockPathResolved()
	if err != nil {
		t.Fatalf("resolve the lock path: %v", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("open the lock file: %v", err)
	}
	t.Cleanup(func() { f.Close() }) //stylegate:ignore test cleanup of the held lock; the assertions have run and the OS reclaims the fd
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("flock the lock file: %v", err)
	}
	return f, path
}

// stampPID writes pid into the held lock file — the diagnostic stamp the
// refusal reads (never the flock itself).
func stampPID(t *testing.T, f *os.File, pid int) {
	t.Helper()
	if err := f.Truncate(0); err != nil {
		t.Fatalf("truncate the stamp: %v", err)
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(pid)), 0); err != nil {
		t.Fatalf("write the stamp: %v", err)
	}
}

// TestServeLockRefusalPairsTheStampedPidWithItsLiveness is the acceptance test
// for the refusal diagnosis: the SAME held flock renders differently for a dead
// stamp and a live one, and the kernel's record for the refused inode is
// reported in both.
func TestServeLockRefusalPairsTheStampedPidWithItsLiveness(t *testing.T) {
	f, path := lockFileWithFlock(t)

	// (a) DEAD stamped pid → the refusal must SAY SO. The lock IS held (the
	// kernel just refused our flock), so the dead half must never be rendered
	// as the shared "the lock is FREE" note: that text contradicts the refusal
	// it is printed inside.
	dead := deadReapedPid(t)
	stampPID(t, f, dead)
	err := serveLockContentionError(path, f)
	if !errors.Is(err, errServeLockHeld) {
		t.Fatalf("a contention must still wrap errServeLockHeld (the handover waits on it), got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "pid "+strconv.Itoa(dead)) {
		t.Errorf("the refusal must still name the stamped pid %d, got:\n%s", dead, msg)
	}
	if !strings.Contains(msg, "no such process") {
		t.Errorf("a dead stamped pid must be reported as no such process, got:\n%s", msg)
	}
	if strings.Contains(msg, serveLockHolderUnknown) {
		t.Errorf("the stamp WAS readable (%d) — reporting an unknown holder hides it:\n%s", dead, msg)
	}
	if strings.Contains(msg, tools.LockHolderNote(dead, false)) {
		t.Errorf("the refusal claims the lock is FREE while the kernel is refusing it:\n%s", msg)
	}

	// (b) LIVE stamped pid → TODAY'S wording, unchanged. Pinned as the exact
	// pre-change message being a prefix of the refusal, so a reworded alive
	// rendering cannot slip through.
	live := os.Getpid()
	stampPID(t, f, live)
	liveErr := serveLockContentionError(path, f)
	if !errors.Is(liveErr, errServeLockHeld) {
		t.Fatalf("a live contention must wrap errServeLockHeld, got %v", liveErr)
	}
	wantPrefix := fmt.Sprintf(serveLockHeldRefusal, path, "pid "+strconv.Itoa(live)+tools.LockHolderNote(live, true))
	if !strings.Contains(liveErr.Error(), wantPrefix) {
		t.Errorf("a LIVE holder must keep today's refusal verbatim:\nwant prefix: %s\ngot:         %s", wantPrefix, liveErr.Error())
	}
	if strings.Contains(liveErr.Error(), "no such process") {
		t.Errorf("a live stamped pid must NOT be reported as gone:\n%s", liveErr.Error())
	}

	// (c) The kernel's OWN record for the inode the acquirer was refused on is
	// reported, and the fd-holder half names THIS process (the live holder of
	// the descriptor that keeps the flock alive).
	//
	// The evidence carries master's kernel-record line (the rework union puts
	// BOTH vocabularies on ONE join key rather than printing the records
	// twice): the kernel's own FLOCK row for the refused inode, and the fd
	// holders. The inode is asserted STRING-EXACT and spaced, so the record
	// list and the fd-holder scan provably name the same file.
	ino := inodeStringOf(t, f)
	// The fd-holder label depends on whether the scan could judge EVERY process
	// (epic 01a14845: an incomplete scan says "PROVEN …", a complete one says
	// "live pids holding an fd …"). Which one this host yields is not the
	// assertion — that the refusal carries the label the PRODUCTION renderer
	// chooses for this very scan is, so the two can never disagree.
	fdHolders, fdComplete := serveLockFdHolders(ino)
	wantFdLabel := strings.SplitN(
		strings.TrimSuffix(serveLockFdHoldersLine(fdHolders, fdComplete), strings.TrimSpace(serveLockPidList(fdHolders, fdComplete))), "%s", 2)[0]
	if strings.TrimSpace(wantFdLabel) == "" {
		t.Fatalf("the fd-holder renderer produced no label for %v (complete=%v)", fdHolders, fdComplete)
	}
	for _, want := range []string{
		serveLockKernelRecordsPrefix,   // master's label — the evidence line the refusal appends
		"inode " + ino,                 // the inode the refusal was against
		serveLockFlockTypeColumn,       // the kernel's own FLOCK row, not a paraphrase
		strings.TrimSpace(wantFdLabel), // the fd-holder half (the rework union's)
		strconv.Itoa(os.Getpid()),      // the live holder of the refused descriptor
	} {
		if !strings.Contains(liveErr.Error(), want) {
			t.Errorf("the contention refusal must report %q, got:\n%s", want, liveErr.Error())
		}
	}
	if !strings.Contains(liveErr.Error(), " "+ino+": ") {
		t.Errorf("the evidence must be joined on the refused inode %s, spelled as the kernel's table spells it:\n%s", ino, liveErr.Error())
	}
}

// TestServeLockKernelEvidenceReportsAnAbsentRecord: a refused flock on an inode
// the kernel's table has NO record for is a REPLACED lock path, not a live
// holder — the evidence must say which, and must carry the inode so the two can
// be told apart (this is the diagnosis c50e7f2a's E2E had to infer by hand).
func TestServeLockKernelEvidenceReportsAnAbsentRecord(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)            // serveLockPathResolved resolves via config.GopiDir
	t.Setenv(config.GopiDirEnv, dir) // …and GOPI_HOME outranks HOME, so pin it too
	path, err := serveLockPathResolved()
	if err != nil {
		t.Fatalf("resolve the lock path: %v", err)
	}
	// An open descriptor with NO flock: exactly the state a successor sees when
	// the refusal it got was against a replaced path.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("open the lock file: %v", err)
	}
	defer f.Close() //stylegate:ignore test cleanup: the assertions have run; the OS reclaims the fd

	// The production renderer for the kernel half, driven directly: the refusal
	// path needs a CONTENTION to reach it, and this test is about the rendering
	// of the no-record case.
	ino := inodeStringOf(t, f)
	evidence := serveLockKernelRecordDiagnosis(f)
	if !strings.Contains(evidence, fmt.Sprintf(serveLockKernelNoMatch, ino)) {
		t.Errorf("an unheld inode must be reported as carrying no record:\n%s", evidence)
	}
	if !strings.Contains(evidence, serveLockKernelRecordsPrefix) {
		t.Errorf("the evidence must be labelled (the operator must know it is the kernel's own table):\n%s", evidence)
	}
	if !strings.Contains(evidence, "inode "+ino) {
		t.Errorf("the kernel half must name the inode it was joined on (%s):\n%s", ino, evidence)
	}
}

// TestServeLockTableRecordsJoinsOnTheKernelSpelledDeviceAndInode pins the join
// key: a FLOCK record is matched on the kernel's OWN "<maj>:<min>:<ino>" of the
// refused descriptor, not on the inode number alone and not on the path. A
// record list joined on the inode alone would report another filesystem's
// coincidentally-equal inode as the holder of THIS lock, and one joined on the
// path would lose the lock entirely after a replacement (the descriptor's inode
// is then no longer what the name resolves to).
func TestServeLockTableRecordsJoinsOnTheKernelSpelledDeviceAndInode(t *testing.T) {
	f, _ := lockFileWithFlock(t)
	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("stat the held descriptor: %v", err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("no syscall.Stat_t on this platform — the kernel's device field cannot be spelled")
	}
	want := serveLockDeviceField(st)

	records, readable := serveLockTableRecords(f)
	if !readable {
		t.Skip("the kernel's lock table is not readable on this host — the join cannot be judged")
	}
	if len(records) == 0 {
		t.Fatalf("the held flock is not in the kernel's table — the rig proves nothing (want a record for %s)", want)
	}
	found := false
	for _, rec := range records {
		if strings.Contains(rec, want) && strings.Contains(rec, serveLockFlockTypeColumn) {
			found = true
		}
	}
	if !found {
		t.Errorf("the record must be matched on the descriptor's own %q and be a FLOCK row, got:%v", want, records)
	}

	// The fd-holder half: THIS process holds an open descriptor on that inode,
	// and it is the only authoritative answer to "who keeps this flock alive".
	holders, _ := serveLockFdHolders(strconv.FormatUint(st.Ino, 10))
	seen := false
	for _, pid := range holders {
		if pid == os.Getpid() {
			seen = true
		}
	}
	if !seen {
		t.Errorf("the fd-holder scan must name this process (%d) as holding the refused inode %d, got %v", os.Getpid(), st.Ino, holders)
	}
}

// inodeStringOf renders the descriptor's inode the way the production evidence
// does, so the assertion and the rendering share ONE join key (the tools
// package's inode accessor, not a re-derived stat).
func inodeStringOf(t *testing.T, f *os.File) string {
	t.Helper()
	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("cannot stat %s", filepath.Base(f.Name()))
	}
	return strconv.FormatUint(tools.InodeOfFileInfo(fi), 10)
}

// serveLockRefusalDiagCrossProcessEnv drives the re-exec'd cross-process
// participant: its ONLY job is to take a REAL flock on the lock path IN ITS OWN
// PROCESS, so the test process is a genuine SECOND process contending for it —
// the state the refusal exists for, and the one the E2E caught the production
// path getting wrong.
const serveLockRefusalDiagCrossProcessEnv = "GOPI_TEST_SERVE_LOCK_DIAG_CROSS"

// serveLockRefusalDiagPathEnv hands the lock path to the re-exec'd participant
// (a test-local env handoff, so the helper and the test provably hold the SAME
// path — the join key of every assertion in the cross-process test).
const serveLockRefusalDiagPathEnv = "GOPI_TEST_SERVE_LOCK_DIAG_PATH"

// TestServeLockRefusalDiagCrossProcessHelperProcess is not a test: it is the
// re-exec target that takes and holds the flock in a SEPARATE process.
func TestServeLockRefusalDiagCrossProcessHelperProcess(t *testing.T) {
	if os.Getenv(serveLockRefusalDiagCrossProcessEnv) != "1" {
		t.Skip("helper process body — re-exec target of TestServeLockRefusalReportsTheRefusedInodeAcrossProcesses")
	}
	path := os.Getenv(serveLockRefusalDiagPathEnv)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("helper: open the lock: %v", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("helper: flock the lock: %v", err)
	}
	// A FRESH file: the stamp is the first write, so no truncation is needed.
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0); err != nil {
		t.Fatalf("helper: stamp our pid: %v", err)
	}
	// Report the path and the inode we HOLD so the parent can prove the refusal
	// is about THIS inode rather than re-derive a key of its own.
	fmt.Printf("%s %s\n", path, mustInodeStringOf(t, f))
	// Hold the flock until the parent is done with it: block on stdin, which the
	// parent closes as its cleanup. No timed sleep — the release is an EVENT the
	// parent controls, so the hold can be neither too short (a refused contention
	// that never was held) nor an unnecessary wait.
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatalf("helper: hold the flock until stdin closes: %v", err)
	}
}

// mustInodeStringOf renders a held descriptor's inode (the join key the refusal
// must name), failing the caller if it cannot.
func mustInodeStringOf(t *testing.T, f *os.File) string {
	t.Helper()
	return inodeStringOf(t, f)
}

// TestServeLockRefusalReportsTheRefusedInodeAcrossProcesses is the regression
// guard for the CONTENTION arm reading a CLOSED descriptor.
//
// The production bug (measured 2026-09-17, .evid/serve-lock-refusal-e2e.txt):
// lockAndVerifyIdentity closed the descriptor BEFORE building the refusal, so
// every real contention rendered
//
//	"kernel lock table: cannot stat the refused descriptor — its inode is unknown"
//
// — the evidence was dead at the one moment it exists for. The in-process tests
// above cannot see it (they build the refusal while their own descriptor is
// open); this test drives the REAL acquisition path across processes, which is
// where the close ordering lives.
func TestServeLockRefusalReportsTheRefusedInodeAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)            // serveLockPathResolved resolves via config.GopiDir
	t.Setenv(config.GopiDirEnv, dir) // …and GOPI_HOME outranks HOME, so pin it too
	path, err := serveLockPathResolved()
	if err != nil {
		t.Fatalf("resolve the lock path: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestServeLockRefusalDiagCrossProcessHelperProcess")
	cmd.Env = append(os.Environ(), serveLockRefusalDiagCrossProcessEnv+"=1",
		serveLockRefusalDiagPathEnv+"="+path)
	// Read the helper's report from its stdout PIPE, not from Wait/Output: the
	// flock must still be held while this process contends for it, so the test
	// must proceed the moment the line arrives rather than after the helper
	// exits (reading the report and waiting for the process are not the same
	// event).
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("pipe the helper's stdout: %v", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("pipe the helper's stdin: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the holding helper: %v", err)
	}
	closeStdin := func() {
		// Closing the pipe is how the helper is told to exit; a failure to kill it
		// afterwards is reported rather than swallowed (Rule 15).
		if cerr := stdin.Close(); cerr != nil {
			t.Errorf("close the helper's stdin to release its flock: %v", cerr)
		}
		_, _ = cmd.Process.Wait() // deliberately ignored: the helper's exit status is asserted by its own report, read above
	}
	t.Cleanup(closeStdin)
	held, err := firstHeldLine(pipe)
	if err != nil {
		t.Fatalf("read the held path+inode from the helper: %v", err)
	}
	if held[0] != path {
		t.Fatalf("the helper holds %s, the test expects %s", held[0], path)
	}
	heldIno := held[1]

	// The acquisition MUST fail: the helper is a live process holding the flock.
	lock, err := lockAndVerifyIdentity(path)
	if err == nil {
		lock.Close() //stylegate:ignore the assertion is that the refusal must happen; releasing the unexpectedly won lock is cleanup
		t.Fatal("a second acquirer won the flock while another process held it")
	}
	if !errors.Is(err, errServeLockHeld) {
		t.Fatalf("the refusal must wrap errServeLockHeld, got %v", err)
	}
	msg := err.Error()
	if strings.Contains(msg, serveLockKernelNoInode) {
		t.Errorf("the refusal stat'd the descriptor AFTER closing it — the evidence is dead:\n%s", msg)
	}
	for _, want := range []string{serveLockKernelRecordsPrefix, "inode " + heldIno, serveLockFlockTypeColumn} {
		if !strings.Contains(msg, want) {
			t.Errorf("a real cross-process contention must report %q, got:\n%s", want, msg)
		}
	}
}

// firstHeldLine reads the holding helper's report (path + inode) from its stdout
// — the FIRST line carrying two fields, because the helper's exit also prints
// the test runner's own output (which must never be mistaken for the report).
func firstHeldLine(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if fields := strings.Fields(sc.Text()); len(fields) == 2 {
			return fields, nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("the helper reported no path/inode line")
}
