//go:build linux && amd64

// sigsource_capture_test.go — the three arms the epic demands of the
// siginfo reader, and why EACH one is needed:
//
//	① SELF-KILL (kill(getpid(), SIGUSR2)) → si_code == SI_USER **and**
//	   sender_pid == getpid. This is the arm that proves the capture is not
//	   merely "installed": SI_USER is 0, so a reader that conflated "0" with
//	   "absent" would pass a sloppier test and fail this one.
//	② CHILD KILL (`kill` in a separate process) → sender_pid == THE CHILD'S pid,
//	   not the victim's. Without it, an implementation that recorded
//	   os.Getpid() (or any self-reference) would answer ① perfectly and be
//	   wrong about every external kill — the only case this epic is for.
//	③ REVERSE (tgkill(2)) → si_code == SI_TKILL(-6), i.e. NOT 0. An
//	   implementation whose si_code is a constant 0 (or never written) passes
//	   ①'s "== SI_USER" arm by accident and fails HERE. That is the point of
//	   the arm: it fails in the direction of the defect.
package sigsource

import (
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// captureWait bounds every wait for a delivery. The signal is sent by this same
// test moments earlier, so the budget only has to outlast a scheduler tick.
const captureWait = 5 * time.Second

// armForCapture registers a channel for one signal, installs the trampoline, and
// returns both — the release is deferred so a failing arm cannot leave a capture
// armed for the NEXT arm's signal (which would make ② inherit ③'s numbers).
func armForCapture(t *testing.T, sig syscall.Signal) <-chan os.Signal {
	t.Helper()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sig)
	t.Cleanup(func() { signal.Stop(ch) })
	if err := Arm([]os.Signal{sig}); err != nil {
		t.Fatalf("Arm(%v): %v", sig, err)
	}
	t.Cleanup(func() { Release([]os.Signal{sig}) })
	return ch
}

// TestDeliverySelfKillIsSIUserAndNamesThisProcess is arm ①.
func TestDeliverySelfKillIsSIUserAndNamesThisProcess(t *testing.T) {
	ch := armForCapture(t, syscall.SIGUSR2)
	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR2); err != nil {
		t.Fatalf("self-kill: %v", err)
	}
	select {
	case got := <-ch:
		// THE CHAIN ARM, implicitly: reaching this branch at all proves the
		// trampoline tail-jumped into the runtime's handler. A trampoline that
		// swallowed the signal would time out here instead of failing a field
		// assertion — which is why the timeout message names the chain.
		src := Delivered(got)
		if !src.Valid {
			t.Fatalf("Delivered(%v) reported no capture — the trampoline did not record this delivery", got)
		}
		if src.Code != SICodeUser {
			t.Fatalf("si_code = %d (%s), want %d (%s) for kill(getpid(), …)",
				src.Code, CodeName(src.Code), SICodeUser, CodeName(SICodeUser))
		}
		if want := int32(os.Getpid()); src.SenderPID != want {
			t.Fatalf("sender_pid = %d, want %d (this process) — a self-kill's si_pid IS the caller", src.SenderPID, want)
		}
		if want := uint32(os.Getuid()); src.SenderUID != want {
			t.Fatalf("sender_uid = %d, want %d (this process's real uid) — si_uid is at siginfo offset 20, not 24", src.SenderUID, want)
		}
	case <-time.After(captureWait):
		t.Fatalf("SIGUSR2 was sent to this process but never reached signal.Notify — the chain from the capture trampoline to the runtime handler is BROKEN (the disposition was changed, not observed)")
	}
}

// TestDeliveryFromAChildProcessNamesTheChild is arm ②.
//
// The sender is a REAL second process (`kill -USR2 <pid>`), so its pid is known
// independently of anything this package computes: it is the pid of the child the
// test started. An implementation that filled sender_pid with os.Getpid() — the
// plausible shortcut — passes arm ① and fails here.
func TestDeliveryFromAChildProcessNamesTheChild(t *testing.T) {
	ch := armForCapture(t, syscall.SIGUSR2)
	cmd := exec.Command("kill", "-USR2", strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the sender child: %v", err)
	}
	senderPID := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the sender child (%s) failed: %v — no signal was delivered, so this arm would assert nothing", "kill -USR2 "+strconv.Itoa(os.Getpid()), err)
	}
	select {
	case got := <-ch:
		src := Delivered(got)
		if !src.Valid {
			t.Fatalf("Delivered(%v) reported no capture for a signal sent by pid %d", got, senderPID)
		}
		if int(src.SenderPID) != senderPID {
			t.Fatalf("sender_pid = %d, want %d (the child that ran `kill -USR2`) — the pid this process reports must be the SENDER's, never its own (%d)",
				src.SenderPID, senderPID, os.Getpid())
		}
		if src.Code != SICodeUser {
			t.Fatalf("si_code = %d (%s), want %d (%s): `kill(1)` sends with kill(2)/sigqueue, both SI_USER",
				src.Code, CodeName(src.Code), SICodeUser, CodeName(SICodeUser))
		}
	case <-time.After(captureWait):
		t.Fatalf("pid %d ran `kill -USR2 %d` successfully but no delivery reached signal.Notify", senderPID, os.Getpid())
	}
}

// TestDeliveryTKillIsNotSIUser is arm ③, the REVERSE verification.
//
// tgkill(2) is a THREAD-directed kill, so the kernel stamps si_code = SI_TKILL
// (-6) instead of SI_USER (0). The assertion is deliberately "the code is the
// specific non-zero value", and that is what makes a CONSTANT-ZERO si_code fail:
// such an implementation satisfies arm ① ("code == SI_USER") by construction and
// is caught only here.
func TestDeliveryTKillIsNotSIUser(t *testing.T) {
	ch := armForCapture(t, syscall.SIGUSR2)
	if err := syscall.Tgkill(os.Getpid(), syscall.Gettid(), syscall.SIGUSR2); err != nil {
		t.Fatalf("tgkill to this thread: %v — this arm needs a real thread-directed delivery", err)
	}
	select {
	case got := <-ch:
		src := Delivered(got)
		if !src.Valid {
			t.Fatalf("Delivered(%v) reported no capture for a tgkill delivery", got)
		}
		if src.Code == SICodeUser {
			t.Fatalf("si_code = %d (%s) for a tgkill(2) delivery, want %d (%s) — a si_code that is always SI_USER/0 cannot tell a kill() from a tkilli(); the capture is not reading the kernel's word",
				src.Code, CodeName(src.Code), SICodeTKill, CodeName(SICodeTKill))
		}
		if src.Code != SICodeTKill {
			t.Fatalf("si_code = %d (%s), want %d (%s) for tgkill(2)",
				src.Code, CodeName(src.Code), SICodeTKill, CodeName(SICodeTKill))
		}
		if int(src.SenderPID) != os.Getpid() {
			t.Fatalf("sender_pid = %d, want %d (this process) for a self-directed tgkill", src.SenderPID, os.Getpid())
		}
	case <-time.After(captureWait):
		t.Fatalf("tgkill(2) to this thread never reached signal.Notify — the chain is broken for thread-directed signals too")
	}
}

// TestUnarmedSignalReportsNoSource pins the DEGRADATION half: a signal this
// package never armed reports Valid=false, so the recorder writes the OLD record
// shape instead of a fabricated si_code=0 (which would read as "an ordinary
// process sent this" — a false accusation, the exact failure Rule 15a describes).
func TestUnarmedSignalReportsNoSource(t *testing.T) {
	if src := Delivered(syscall.SIGINT); src.Valid {
		t.Fatalf("Delivered(SIGINT) reported Valid with si_code=%d for a signal that was never armed", src.Code)
	}
	if src := Delivered(nil); src.Valid {
		t.Fatal("Delivered(nil) reported Valid — a non-syscall signal names no si_code")
	}
}
