package main

// The WORKTREE-CWD RELEASE acceptance rig (epic fix/cwd-release-coverage,
// 2026-09-14). It pins the PRODUCER side of the one thing finish-cleanup's
// guard ③ refuses on: a live process whose cwd is inside the worktree.
//
// WHAT IT PROVES, in the same order the defect was measured:
//
//	Step 1 (RED)   an unfixed process — one that never releases its cwd —
//	               keeps the worktree's inode busy after `git worktree remove`
//	               has unregistered it: /proc/<pid>/cwd reads
//	               "<path> (deleted)" and the DIRECTORIES of the removed tree
//	               are still pinned by that process. That is exactly the state
//	               guard ③ reports, and it is why a long-lived worker/merger
//	               that forgets the release withholds its branch forever.
//	Step 2 (GREEN) the SAME process shape, but with the release performed
//	               before the removal: /proc/<pid>/cwd is the process root, the
//	               holder is gone, and a `git worktree remove` of the tree
//	               leaves nothing pinned.
//	Step 3 (GATE)  the release action has exactly ONE definition in the tree
//	               (internal/worktreerelease.Release) and every
//	               worktree-removing producer in cmd/agent references it —
//	               enforced mechanically by TestGateCwdRelease* in
//	               internal/standards, so a future path cannot silently skip it.
//
// WHY A HELPER RE-EXEC AND NOT A SHELL: the release must be performed by the
// HOLDER ITSELF (Linux has no syscall to move another process's cwd), so the
// rig needs a real process that can (a) stand in the worktree and (b) either
// release or not. This test binary re-execs itself in both shapes — no shell,
// no external binary, no sleep-command assumptions (Rule 10.7 zero-dep).
//
// Everything runs under the suite's pinned scratch root (t.TempDir()), never
// /tmp: the worktree-removal rigs are exactly the ones the /-partition ban
// exists for (PROJECT.md Rule 22).

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/worktreerelease"
)

const (
	// cwdReleaseHolderMarker gates the re-exec'd helper: unset ⇒ the helper
	// skips instantly, so a normal `go test` is unaffected.
	cwdReleaseHolderMarker = "GOPI_CWD_RELEASE_RIG_HOLDER"
	// cwdReleaseHolderReleaseArg selects the FIXED shape (release before the
	// removal). Its absence is the RED shape (never release).
	cwdReleaseHolderReleaseArg = "-test.run=TestHelperCwdReleaseHolder"

	// cwdReleaseReadyTimeout bounds the wait for the helper's readiness BYTE
	// (fd 3). The byte is the readiness SIGNAL; this number only converts
	// "never signalled" into a failure instead of a hang, so it is
	// deliberately generous — the byte arrives only after the re-exec'd SUITE
	// binary has finished its own TestMain setup, which on a loaded host costs
	// far more than the signal itself. Measured 2026-09-26 (load 26 on 8 CPUs):
	// the cold start did not fit the former 1s bound and the rig failed with
	// "did not report readiness within 1s" while the release machinery under
	// test was green. Override with cwdReleaseReadyTimeoutEnv (Rule 2).
	cwdReleaseReadyTimeout = 60 * time.Second
	// cwdReleaseReadyTimeoutEnv overrides that bound, in WHOLE SECONDS, for an
	// operator on a heavily loaded host. Exactly one place reads it (Rule 2);
	// a non-numeric or non-positive value falls back to the default.
	cwdReleaseReadyTimeoutEnv = "GOPI_CWD_RELEASE_READY_TIMEOUT_SEC"

	// cwdReleaseProcWaitAttempts/Interval bound the poll for the helper's
	// procfs CWD. It is checked AFTER the readiness byte, so the CWD is
	// already in place by then — procfs offers no notification, so this part
	// stays a poll, on a bounded cadence.
	cwdReleaseProcWaitAttempts = 250
	cwdReleaseProcWaitInterval = 20 * time.Millisecond
	// cwdReleaseHolderHoldTime is how long the helper stays in the worktree
	// after it has reported readiness — long enough for the rig to remove the
	// registration and read /proc back.
	cwdReleaseHolderHoldTime = 20 * time.Second
	// cwdReleaseReadyFd is the pipe fd the helper writes its readiness byte to
	// (an ExtraFile, so the parent learns the helper is IN the worktree without
	// racing a sleep).
	cwdReleaseReadyFd = 3
)

// TestHelperCwdReleaseHolder is not a test of its own: it is the re-exec'd
// holder. With the marker unset it skips instantly (a normal run is unaffected).
//
// GATED ON THE MARKER, it stands in the directory its parent gave it as CWD
// (os/exec sets it before the process starts), optionally performs THE
// PRODUCTION RELEASE (internal/worktreerelease.Release — the same call the
// worker round, the merger pass and the executor close-out make), signals
// readiness on fd 3, and then holds — waiting for the parent to remove the
// worktree out from under it, which is the whole point of the rig.
func TestHelperCwdReleaseHolder(t *testing.T) {
	if os.Getenv(cwdReleaseHolderMarker) == "" {
		t.Skip("cwd-release holder helper — only runs as the re-exec'd child the rig starts")
	}
	release := os.Getenv(cwdReleaseHolderMarker) == "release"
	if release {
		if err := worktreerelease.Release(); err != nil {
			t.Fatalf("helper: release cwd: %v", err)
		}
	}
	// Readiness: one byte on the ExtraFile fd the parent gave us (fd 3). A
	// failed write is fatal HERE (the parent would otherwise wait out its
	// poll budget and report a fixture failure) — never swallowed (Rule 15b).
	f := os.NewFile(cwdReleaseReadyFd, "ready")
	if f != nil {
		if _, err := f.Write([]byte{1}); err != nil {
			t.Fatalf("helper: signal readiness: %v", err)
		}
		// Deliberately NOT closed: the parent reads exactly one byte.
	}
	time.Sleep(cwdReleaseHolderHoldTime) //stylegate:allow-sleep the holder must stay alive while the rig reads its procfs cwd and removes the tree
}

// cwdReleaseHolder is one started helper plus the state the rig reads back.
type cwdReleaseHolder struct {
	pid   int
	ready *os.File
}

// startCwdReleaseHolder starts the helper with cwd = wt, waits for its
// readiness byte AND for procfs to report that cwd (both, so a "started but
// not yet in place" helper can never be mistaken for a holder), and returns it.
func startCwdReleaseHolder(t *testing.T, wt string, release bool) cwdReleaseHolder {
	t.Helper()
	shape := "hold"
	if release {
		shape = "release"
	}
	readyR, readyW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	cmd := exec.Command(os.Args[0], cwdReleaseHolderReleaseArg)
	cmd.Dir = wt
	cmd.Env = append(os.Environ(), cwdReleaseHolderMarker+"="+shape,
		// The helper is a re-exec'd SUITE binary, so its TestMain pins scratch
		// roots — inside THIS test's fixture, so the creator owns exactly what
		// it created (the same reasoning the recycle rig documents).
		testutil.SuiteTempRootEnvName+"="+t.TempDir(),
		testutil.TempDirEnvName+"="+t.TempDir())
	cmd.ExtraFiles = []*os.File{readyW}
	mp := testutil.StartMockProcessCmd(t, cmd)
	// The parent's copy of the write end must be closed or the readiness read
	// never sees EOF in the failure path.
	_ = readyW.Close() //stylegate:ignore the parent's copy of the write end must be closed or the readiness read never sees EOF; a close error here cannot change the verdict
	//stylegate:ignore cleanup close of the readiness pipe's read end; the descriptor is being abandoned at test end
	t.Cleanup(func() { _ = readyR.Close() })
	pid := mp.Pid()
	h := cwdReleaseHolder{pid: pid, ready: readyR}
	// ① readiness byte — an EVENT, not a wall-clock guess: the helper writes
	// one byte on fd 3 once it is in place. The child's own exit ALSO ends the
	// wait (the read end then reports EOF), so a helper that never got that far
	// is reported as such instead of being waited out.
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, rerr := h.ready.Read(buf)
		done <- rerr
	}()
	window := cwdReleaseReadyWindow()
	select {
	case rerr := <-done:
		if errors.Is(rerr, io.EOF) {
			t.Fatalf("holder %d exited before signalling readiness", pid)
		}
		if rerr != nil {
			t.Fatalf("holder %d never signalled readiness: %v", pid, rerr)
		}
	case <-time.After(window):
		t.Fatalf("holder %d did not report readiness within %s — the bound is a failure detector, not a measurement; raise %s if this host is slower still",
			pid, window, cwdReleaseReadyTimeoutEnv)
	}
	// ② procfs agreement: the CWD must actually read as wt (or as the escape
	// root when the helper released). Reading it back is what turns "we set
	// cmd.Dir" into a measurement.
	want := wt
	if release {
		want = worktreerelease.ProcessRootPath
	}
	for i := 0; i < cwdReleaseProcWaitAttempts; i++ {
		if got, ok := cwdReleaseHolderCwd(pid); ok && got == want {
			return h
		}
		time.Sleep(cwdReleaseProcWaitInterval) //stylegate:allow-sleep this IS the poll: procfs has no readiness notification, so the CWD is re-read on a bounded cadence under the parent's deadline
	}
	got, ok := cwdReleaseHolderCwd(pid)
	t.Fatalf("holder %d: procfs cwd = %q (readable=%v), want %q within %s — the fixture never reached the state under test",
		pid, got, ok, want, cwdReleaseProcWaitTimeout())
	return h
}

// cwdReleaseReadyWindow is the bound on the readiness wait: the named default,
// overridable in whole seconds by cwdReleaseReadyTimeoutEnv. An unparseable or
// non-positive override is ignored so the default stands — never a silent zero,
// which would fail every run for a reason no message could explain (Rule 15a).
func cwdReleaseReadyWindow() time.Duration {
	if v := os.Getenv(cwdReleaseReadyTimeoutEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return cwdReleaseReadyTimeout
}

// cwdReleaseProcWaitTimeout bounds the poll for the helper's procfs CWD.
func cwdReleaseProcWaitTimeout() time.Duration {
	return time.Duration(cwdReleaseProcWaitAttempts) * cwdReleaseProcWaitInterval
}

// cwdReleaseHolderCwd reads a process's cwd via procfs (the SAME source guard
// ③ uses). ("", false) = unreadable.
func cwdReleaseHolderCwd(pid int) (string, bool) {
	target, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
	if err != nil || target == "" {
		return "", false
	}
	return target, true
}

// TestCwdReleaseRig pins both halves against a real linked worktree:
//
//	RED   — a holder that does NOT release keeps the removed tree pinned
//	        ("<wt> (deleted)" in /proc), which is the state guard ③ refuses on;
//	GREEN — the same holder, having called the production release, lets the
//	        directory go: its cwd is the process root and nothing is pinned.
//
// The removal is the ordinary `git worktree remove` (never --force) — the same
// call the reclaim arms make — so the rig measures the production path.
func TestCwdReleaseRig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, tc := range []struct {
		name    string
		release bool
		// wantPinned is the state the holder leaves behind: true = the removed
		// directory is still pinned by it (the DEFECT), false = it let go.
		wantPinned bool
	}{
		{name: "red_unreleased_holder_pins_the_removed_tree", release: false, wantPinned: true},
		{name: "green_released_holder_lets_the_tree_go", release: true, wantPinned: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := cwdReleaseRigGround(t)
			h := startCwdReleaseHolder(t, f.wt, tc.release)

			// The removal the reclaim arms run: ordinary `git worktree remove`.
			releaseGit(t, f.repo, "worktree", "remove", f.wt)

			// The directory is gone from the filesystem and from git either way
			// — `git worktree remove` does not consult /proc. What differs is
			// whether the HOLDER still pins it.
			if _, err := os.Stat(f.wt); !os.IsNotExist(err) {
				t.Fatalf("worktree %s still present after `git worktree remove` (err=%v)", f.wt, err)
			}
			if branchListedInWorktrees(t, f.repo, f.branch) {
				t.Fatalf("git still holds the registration for %s after removal", f.branch)
			}
			// The deliverable survives: removal never deletes the branch.
			if tip := releaseGit(t, f.repo, "rev-parse", f.branch); tip != f.tip {
				t.Fatalf("branch %s tip moved: %s → %s (removal must never touch the branch)", f.branch, f.tip, tip)
			}

			got, ok := cwdReleaseHolderCwd(h.pid)
			pinned := ok && strings.HasSuffix(got, " (deleted)")
			if pinned != tc.wantPinned {
				t.Fatalf("holder %d cwd = %q (readable=%v): pinned=%v, want %v — %s",
					h.pid, got, ok, pinned, tc.wantPinned,
					map[bool]string{
						true:  "the RED shape must pin the removed directory (that is the defect: guard ③ refuses on it)",
						false: "the released holder must NOT pin the directory (the fix: cwd is the process root)",
					}[tc.wantPinned])
			}
			t.Logf("holder %d cwd after removal: %q", h.pid, got)
		})
	}
}

// cwdReleaseGround is the rig's git world: a repo + one linked worktree on its
// own branch, with one commit (the deliverable's fingerprint). Everything lives
// under the suite's pinned scratch root.
type cwdReleaseGround struct {
	repo   string
	wt     string
	branch string
	tip    string
}

// cwdReleaseRigGround builds the fixture (one spelling for the type and the
// builder would collide, so the builder is named for what it returns).
func cwdReleaseRigGround(t *testing.T) cwdReleaseGround {
	t.Helper()
	root := t.TempDir()
	f := cwdReleaseGround{
		repo:   filepath.Join(root, "repo"),
		wt:     filepath.Join(root, "wt-cwd-release"),
		branch: "feat/cwd-release-rig",
	}
	releaseGit(t, root, "init", "-b", "master", f.repo)
	releaseGit(t, f.repo, "config", "user.email", "rig@rig")
	releaseGit(t, f.repo, "config", "user.name", "rig")
	if err := os.WriteFile(filepath.Join(f.repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	releaseGit(t, f.repo, "add", "base.txt")
	releaseGit(t, f.repo, "commit", "-m", "base")
	releaseGit(t, f.repo, "worktree", "add", "-b", f.branch, f.wt, "master")
	if err := os.WriteFile(filepath.Join(f.wt, "deliverable.txt"), []byte("the delivery\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	releaseGit(t, f.wt, "add", "deliverable.txt")
	releaseGit(t, f.wt, "commit", "-m", "the deliverable")
	f.tip = releaseGit(t, f.repo, "rev-parse", f.branch)
	return f
}
