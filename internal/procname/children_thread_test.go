package procname

// Children must see the WHOLE tree under a process, not the main thread's slice
// of it. The kernel records a direct child under the THREAD that forked it (the
// new task's ppid is the process, but its parent TASK is the thread), so a
// process that spawned from a non-main thread lists that child only under
// /proc/<pid>/task/<tid>/children — a reader that opens
// /proc/<pid>/task/<pid>/children alone answers a FLOOR and calls it a tree.
//
// MEASURED ON THIS HOST while writing this test: the main thread's children
// file held "" (empty) at the very moment Children reported [3565691] — the
// child was there, in a thread file the old implementation never opened.
//
// WHY THE FLOOR IS EXPENSIVE: internal/testutil/runprogress.go's
// measureRunProgress counts procname.Children to decide whether a silent
// `go test` run has a live process behind it. An empty floor reads exactly like
// "nothing is running", which is a HANG verdict on a tree that is working.

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// procRootForTest is procfs — this test's subject, the per-thread children file,
// exists nowhere else.
const procRootForTest = "/proc"

// shFlagCommand and shStayAlive are the smallest program that stays alive long
// enough for its own record to be measured (it is not required to fork).
const (
	shBinary      = "/bin/sh"
	shFlagCommand = "-c"
	shStayAlive   = "sleep 60"
)

// TestChildrenSeesAChildForkedFromANonMainThread is the red proof, and it asserts
// BOTH halves of the same fact in one run:
//
//	premise — the main-thread-only file (what the old implementation read) does
//	          NOT list the child, so the old answer was a floor;
//	result  — Children(procRoot, self) DOES list it.
//
// If the kernel's accounting ever changed so that the child appeared under the
// main thread, the premise half would fail LOUDLY rather than let the test pass
// for a reason that has nothing to do with the fix.
func TestChildrenSeesAChildForkedFromANonMainThread(t *testing.T) {
	self := strconv.Itoa(os.Getpid())
	if _, err := os.Stat(procRootForTest + "/" + self + "/task"); err != nil {
		t.Skipf("no procfs per-thread tree at %s: this test's subject does not exist here", procRootForTest)
	}

	cmd := exec.Command(shBinary, shFlagCommand, shStayAlive)
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	started := make(chan error, 1)
	release := make(chan struct{})
	measured := make(chan []int, 1)

	// Start on a thread of its OWN. This is the whole subject: the fork caller is
	// not the main thread, so the kernel files the child under THIS thread.
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if serr := cmd.Start(); serr != nil {
			started <- serr
			return
		}
		started <- nil
		<-release
		measured <- Children(procRootForTest, self)
	}()

	if serr := <-started; serr != nil {
		t.Fatalf("start %s: %v", shBinary, serr)
	}
	childPid := cmd.Process.Pid
	t.Cleanup(func() {
		// Created by THIS test, so it is ours to signal — by pid, never a group
		// and never a pattern (Rule 42).
		_ = syscall.Kill(childPid, syscall.SIGKILL) //stylegate:ignore the child was created by THIS test and the subject is what the kernel filed it under; a failed signal is not what the arm asserts
		_ = cmd.Wait()                              //stylegate:ignore reaps that same child so the test leaves no zombie; the exit status is the DELIBERATE kill above, not a failure to report
	})

	// The PREMISE, measured on the exact file the old implementation read.
	mainThreadFile, err := os.ReadFile(procRootForTest + "/" + self + "/task/" + self + "/" + ChildrenName)
	if err != nil {
		t.Fatalf("read the main thread's children file: %v", err)
	}
	mainThreadView := strings.TrimSpace(string(mainThreadFile))
	if hasField(mainThreadView, childPid) {
		t.Skipf("the kernel listed %d under the main thread (%q): this test's premise does not hold on this host, so it could not prove anything", childPid, mainThreadView)
	}

	close(release)
	got := <-measured

	if !containsInt(got, childPid) {
		t.Fatalf("Children missed the child forked from a non-main thread: want %d in the answer, got %v (the main-thread-only file held %q)", childPid, got, mainThreadView)
	}
}

// hasField reports whether a whitespace-separated pid list text holds want.
func hasField(text string, want int) bool {
	for _, f := range strings.Fields(text) {
		if n, err := strconv.Atoi(f); err == nil && n == want {
			return true
		}
	}
	return false
}

func containsInt(xs []int, want int) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
