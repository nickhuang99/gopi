package main

// daemon_death_pipe_shape_test.go — the guard that makes daemonDeathPipe's
// documented "is not usable" refusal REACHABLE.
//
// # The defect this file closes
//
// daemonDeathPipe wrapped the number from daemonlifetime.HoldEnvName with
// os.NewFile and refused only when the wrapper came back nil, documenting that as
// "the number is not usable". os.NewFile validates NOTHING: it returns nil only
// for a negative number and otherwise hands back a wrapper whose Fd() is whatever
// it was given. So a FREE number (100000) or a LIVE non-pipe number was accepted,
// appended to child.ExtraFiles, and the only report was os/exec's own "invalid
// argument" at Start — the precise refusal replaced by a generic failure one
// layer down (Rule 15: code claiming a validation it does not perform).
//
// # What the guard measures
//
// The production function is the drive (it reads its number from the environment,
// so a case sets daemonlifetime.HoldEnvName to a number IT owns and calls the
// shipped code): four shapes, each with its own refusal or acceptance, plus the
// re-measured PREMISE that makes the guard non-vacuous — os.NewFile happily wraps
// the same free number the spawn path now refuses, asserted here on every run
// rather than remembered.
//
// # The numbers are claimed raw (never through a wrapper the GC could finalize)
//
// Every fixture claims its number with syscall.Dup / F_DUPFD and releases it with
// syscall.Close, so no *os.File the garbage collector can finalize sits between
// the fixture and the assertion.
//
// # What this file does NOT cover (and why that is a branch statement, not a gap)
//
// The BORROWED-descriptor obligation — that the wrapper handed back here must
// never let the holder's read end be closed by the GC finalizer
// (borrowedfd.Detach, the sibling epic's classification) — is guarded by
// cmd/agent/daemon_death_pipe_wrapper_test.go, which lives ONLY on the unmerged
// branch fix/borrowed-fd-sibling-sites. This branch stacks the shape fix on
// master, which has no internal/borrowedfd package to call, so it does not
// duplicate that guard; the hand-off is stated in devdoc/daemon-death-pipe-fd-shape.md.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/nick/gopi/internal/borrowedfd"
	"github.com/nick/gopi/internal/daemonlifetime"
)

// shapeProbeEnvVar is the variable daemonDeathPipe reads (spelled through the
// production constant — Rule 1b/13).
var shapeProbeEnvVar = daemonlifetime.HoldEnvName

// shapeFreeFD is a number no process in this suite holds.
const shapeFreeFD = 100000

// dupAtLeast places src on the lowest free number >= min (F_DUPFD, the idiom the
// production arming code uses) and returns it.
func dupAtLeast(t *testing.T, src *os.File, min int) int {
	t.Helper()
	n, _, errno := syscall.Syscall(syscall.SYS_FCNTL, src.Fd(), uintptr(syscall.F_DUPFD), uintptr(min))
	if errno != 0 {
		t.Fatalf("dup fd %d onto the lowest free number >= %d: %v", src.Fd(), min, errno)
	}
	return int(n)
}

// releaseRaw frees a number claimed by dupAtLeast at test end.
//
// Through internal/borrowedfd, because a raw close cannot tell "I still hold this"
// from "the kernel recycled it to a stranger" — see release.go there.
func releaseRaw(t *testing.T, n int) {
	t.Helper()
	claim := borrowedfd.Witness(n)
	t.Cleanup(func() {
		var moved *borrowedfd.MovedError
		relErr := claim.Release()
		if relErr != nil && !errors.As(relErr, &moved) {
			t.Errorf("releasing this test's own claimed fd %d failed: %v", n, relErr)
			return
		}
		if moved != nil {
			t.Errorf("%s", moved.Error())
		}
	})
}

// kernelTarget reports what the kernel says number n points at.
func kernelTarget(t *testing.T, n int) string {
	t.Helper()
	target, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(n))
	if err != nil {
		t.Fatalf("read the kernel's target for fd %d: %v", n, err)
	}
	return target
}

// osNewFileWrapsAnything re-measures the PREMISE this whole guard rests on: the
// number the spawn path must refuse is one os.NewFile wraps without complaint.
// Without this assertion the guard could pass against a future Go whose os.NewFile
// DID validate — the premise is checked, not remembered.
//
// The wrapper is RETAINED for the life of the test binary rather than dropped:
// os.NewFile installs a GC finalizer that closes the number, and a dropped
// wrapper over a number THIS test still owns would close it behind the fixture —
// after which the kernel could hand the same number to an unrelated live handle
// and the fixture's release would close that stranger (the fd-recycling hazard of
// the sibling epic, in the guard rather than in the code).
func osNewFileWrapsAnything(t *testing.T, n int) {
	t.Helper()
	f := os.NewFile(uintptr(n), shapeProbeEnvVar)
	if f == nil {
		t.Fatalf("os.NewFile(%d) returned nil — this guard exists because os.NewFile does NOT validate (it returns nil only for a negative number); if that changed, the refusal in daemonDeathPipe must be re-derived rather than left to guard a state that can no longer occur", n)
	}
	if int(f.Fd()) != n {
		t.Fatalf("os.NewFile(%d) produced a wrapper for fd %d — the premise is re-measured here, so a changed os.NewFile must fail this guard loudly", n, f.Fd())
	}
	shapeProbeWrappers = append(shapeProbeWrappers, f)
}

// shapeProbeWrappers retains every wrapper this file builds over a number the
// process does not own, so no finalizer can close a number out from under a
// fixture (see osNewFileWrapsAnything).
var shapeProbeWrappers []*os.File

// TestDaemonDeathPipeRefusesANumberNoProcessHolds is the case the sibling branch
// OBSERVED and could not assert: a free number must be refused by THIS function,
// with a verdict naming the variable, the number and what was found — not handed
// to os/exec to fail there.
func TestDaemonDeathPipeRefusesANumberNoProcessHolds(t *testing.T) {
	if _, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(shapeFreeFD)); err == nil {
		t.Fatalf("fd %d is open in this process — the case needs a number nothing holds", shapeFreeFD)
	}
	osNewFileWrapsAnything(t, shapeFreeFD)
	t.Setenv(shapeProbeEnvVar, strconv.Itoa(shapeFreeFD))

	death, err := daemonDeathPipe()
	if err == nil {
		t.Fatalf("daemonDeathPipe ACCEPTED %s=%d, a number no process here holds — the number would be appended to child.ExtraFiles and os/exec's own \"invalid argument\" would be the only report, instead of this function's refusal",
			shapeProbeEnvVar, shapeFreeFD)
	}
	if death != nil {
		t.Fatalf("daemonDeathPipe refused %s=%d but also returned a wrapper (%v) — a refusal must leave the caller nothing to pass to os/exec",
			shapeProbeEnvVar, shapeFreeFD, death)
	}
	for _, want := range []string{shapeProbeEnvVar, strconv.Itoa(shapeFreeFD), "not a usable death pipe", "not open"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not mention %q — it must name the variable, the number and what was found, so an operator knows which of the three shapes failed (Rule 15)", err, want)
		}
	}
}

// TestDaemonDeathPipeRefusesANonPipeNumber covers the shape os.NewFile and Stat
// both accept: a live descriptor that is not a pipe, ABOVE the floor so the case
// cannot pass for the floor's reason. The refusal must quote the kernel's target,
// or the operator is told a shape failed without being told what was found.
func TestDaemonDeathPipeRefusesANonPipeNumber(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "death-pipe-shape-*")
	if err != nil {
		t.Fatalf("create the non-pipe fixture: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() }) //stylegate:ignore release of this test's own fixture
	number := dupAtLeast(t, file, daemonlifetime.InheritFDFloor)
	releaseRaw(t, number)
	if number < daemonlifetime.InheritFDFloor {
		t.Fatalf("the fixture claimed fd %d, below the floor %d — this case would be testing the floor rather than the shape", number, daemonlifetime.InheritFDFloor)
	}
	osNewFileWrapsAnything(t, number)
	t.Setenv(shapeProbeEnvVar, strconv.Itoa(number))

	death, err := daemonDeathPipe()
	if err == nil {
		t.Fatalf("daemonDeathPipe ACCEPTED %s=%d, which the kernel reports as %q — a daemon watching a regular file would never learn that its holder died, so the start must be refused rather than reported as bound",
			shapeProbeEnvVar, number, kernelTarget(t, number))
	}
	if death != nil {
		t.Fatalf("daemonDeathPipe refused %s=%d but also returned a wrapper (%v)", shapeProbeEnvVar, number, death)
	}
	for _, want := range []string{shapeProbeEnvVar, strconv.Itoa(number), "not a pipe", file.Name()} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not mention %q — it must quote the kernel's target, not merely assert a shape failed", err, want)
		}
	}
}

// TestDaemonDeathPipeRefusesANumberBelowTheFloor pins the renumbering rule with a
// number that is a PIPE and still refused: a pipe below the floor fails on the
// floor alone, so this case cannot be satisfied by the shape test.
func TestDaemonDeathPipeRefusesANumberBelowTheFloor(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create the low pipe fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = r.Close() //stylegate:ignore release of this test's own fixture
		_ = w.Close()
	})
	// F_DUPFD from 0 asks for the lowest free number: in a fresh test binary that
	// is far below the floor. Measured, not assumed — the case says so if not.
	number := dupAtLeast(t, r, 0)
	releaseRaw(t, number)
	if number >= daemonlifetime.InheritFDFloor {
		t.Skipf("this process has no free descriptor below the floor %d (the lowest free number is %d) — the below-the-floor refusal cannot be exercised here", daemonlifetime.InheritFDFloor, number)
	}
	if target := kernelTarget(t, number); !strings.HasPrefix(target, "pipe:[") {
		t.Fatalf("the fixture's fd %d is %q, not a pipe — this case must fail on the FLOOR, not on the shape", number, target)
	}
	t.Setenv(shapeProbeEnvVar, strconv.Itoa(number))

	death, err := daemonDeathPipe()
	if err == nil {
		t.Fatalf("daemonDeathPipe ACCEPTED %s=%d, a pipe BELOW the ExtraFiles floor %d — os/exec renumbers ExtraFiles from fd 3 upwards and would silently replace that descriptor with whatever the launcher passes there (MEASURED 2026-09-14: the replaced descriptor made a bound daemon self-terminate at once)",
			shapeProbeEnvVar, number, daemonlifetime.InheritFDFloor)
	}
	if death != nil {
		t.Fatalf("daemonDeathPipe refused %s=%d but also returned a wrapper (%v)", shapeProbeEnvVar, number, death)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(daemonlifetime.InheritFDFloor)) {
		t.Errorf("the refusal %q does not name the floor %d — a below-the-floor number must be distinguishable from a closed or non-pipe one", err, daemonlifetime.InheritFDFloor)
	}
}

// TestDaemonDeathPipeAcceptsTheSuitesOwnShape is the ACCEPTANCE half and the
// regression guard for the daemon e2e: the suite arms a process-wide pipe whose
// read end sits at fd >= InheritFDFloor (internal/daemonlifetime.ArmThisProcess)
// and every daemon a test binary starts is handed that number. A shape test that
// refused it would break the binding the leak epic exists for.
//
// The fixture claims its number the way ArmThisProcess does (F_DUPFD at the
// floor), so what is accepted here is the suite's real shape.
func TestDaemonDeathPipeAcceptsTheSuitesOwnShape(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create the suite-shape pipe fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = r.Close() //stylegate:ignore release of this test's own fixture
		_ = w.Close()
	})
	number := dupAtLeast(t, r, daemonlifetime.InheritFDFloor)
	// NOT releaseRaw here. This test hands the number to daemonDeathPipe, which
	// returns a wrapper denoting it, and then closes that wrapper ITSELF at the end
	// ("close the accepted fixture wrapper"). A teardown release on top of that is a
	// DOUBLE close on one number — the defect this file's releaseRaw exists to
	// prevent, committed by the fixture's own shape. Measured before the fix: the
	// teardown reported "refusing to close descriptor 201: it is no longer the
	// descriptor this call claimed ... observed now: nothing (the number is closed)",
	// and the pre-instrument version had been swallowing it as an ignored EBADF.
	//
	// So the ONE release of this number is registered here, it goes through the
	// HOLDER the production path returned, and it asks the kernel first.
	claimed := borrowedfd.Witness(number)
	if number < daemonlifetime.InheritFDFloor {
		t.Fatalf("the fixture's read end landed at fd %d, below the floor %d — the suite's binding shape cannot be reproduced", number, daemonlifetime.InheritFDFloor)
	}
	t.Setenv(shapeProbeEnvVar, strconv.Itoa(number))

	death, err := daemonDeathPipe()
	if err != nil {
		t.Fatalf("daemonDeathPipe refused the suite's own binding shape (a pipe at fd %d, >= the floor %d): %v — this is the regression the shape test must never introduce: the test suite arms exactly this and every daemon started under it would fail to start",
			number, daemonlifetime.InheritFDFloor, err)
	}
	if death == nil {
		t.Fatalf("daemonDeathPipe returned no wrapper and no error for %s=%d — a binding that was asked for must either be provided or refused, never silently skipped", shapeProbeEnvVar, number)
	}
	// The wrapper must actually denote the holder's number, and the kernel must
	// still report that number as the pipe: an acceptance that pointed somewhere
	// else would bind the daemon to a stranger.
	if got := int(death.Fd()); got != number {
		t.Fatalf("the wrapper denotes fd %d, want the holder's %d", got, number)
	}
	target := kernelTarget(t, number)
	if !strings.HasPrefix(target, "pipe:[") {
		t.Fatalf("fd %d is now %q — the accepted hand-off must leave the holder's pipe descriptor intact", number, target)
	}
	// This fixture's number is a dup THIS TEST opened, so closing the wrapper
	// closes the test's own duplicate (the source end stayed with the fixture and
	// is released above). The production obligation — never close the HOLDER's
	// descriptor — is about a number the spawner did not open, and is the subject
	// of the sibling branch's wrapper guard (see this file's header).
	//
	// THE ONE RELEASE, through the holder: the identity check runs first, so a
	// number the kernel had already recycled would NOT be closed even here.
	if checkErr := claimed.Check(); checkErr != nil {
		t.Fatalf("the accepted wrapper's number is no longer what this fixture claimed, so it is NOT released: %v", checkErr)
	}
	if err := death.Close(); err != nil {
		t.Errorf("close the accepted fixture wrapper: %v", err)
	}
}

// TestDaemonDeathPipeRefusesANonNumber is the pre-existing shape, kept beside the
// new ones so the whole refusal surface of this function is in one place.
func TestDaemonDeathPipeRefusesANonNumber(t *testing.T) {
	for _, raw := range []string{"not-a-number", "-1", ""} {
		t.Run(fmt.Sprintf("%q", raw), func(t *testing.T) {
			t.Setenv(shapeProbeEnvVar, raw)
			death, err := daemonDeathPipe()
			if raw == "" {
				if err != nil || death != nil {
					t.Fatalf("an UNSET binding variable must mean \"not armed\": death=%v err=%v", death, err)
				}
				return
			}
			if err == nil || death != nil {
				t.Fatalf("daemonDeathPipe accepted %s=%q (death=%v, err=%v) — a start that was asked for a binding and cannot provide one must fail, never proceed unbound",
					shapeProbeEnvVar, raw, death, err)
			}
		})
	}
}
