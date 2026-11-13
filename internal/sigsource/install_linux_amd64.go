//go:build linux && amd64

package sigsource

import (
	"fmt"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// The kernel's struct kernel_sigaction, as rt_sigaction(2) reads and writes it
// on x86_64. Field ORDER and the 8-byte sigsetsize are the ABI: the prototype
// that produced this file round-tripped the real descriptor through it and the
// kernel echoed back exactly what Go had installed
// (sa_flags=0x1c000004, sa_restorer=0x473620, sa_mask=~[KILL STOP]).
type kernelSigaction struct {
	handler  uintptr
	flags    uint64
	restorer uintptr
	mask     uint64
}

// sigaction flag bits (asm-generic/signal-defs.h). golang.org/x/sys/unix does
// not export them on this target (measured: no SA_SIGINFO in zerrors_linux*),
// so they are named here (Rule 1) rather than written as hex in the arm below.
const (
	saSIGINFO = 0x00000004 // deliver a siginfo_t, not just the signo
	saONSTACK = 0x08000000 // run on the registered alternate signal stack
	saRESTART = 0x10000000 // restart interrupted syscalls
)

// sigMax bounds the per-signal capture table: Linux's _NSIG + 1 (signal
// numbers are 1..64). Rule 1: a named bound, and the array is sized from it.
const sigMax = 64

// captured holds, per signal, the LAST siginfo the kernel handed the
// trampoline: si_code, si_pid, si_uid packed as int32 at [sig*4 .. sig*4+3).
//
// It is written by ASSEMBLY (trampoline_amd64.s) and read by Go, which is why
// it is a plain array rather than a typed atomic: the writer cannot use Go
// atomics, and the ordering that matters is structural — the trampoline stores
// the fields BEFORE it tail-jumps into the handler it displaced, so every
// consumer of the delivery (signal.Notify's channel, this package's Delivered)
// is downstream of the store on the same thread. The Go reads still use
// atomic.LoadInt32 so a stale read is a stale VALUE and never a torn one.
var captured [sigMax * 4]int32

// captureSlot layout inside captured, in int32 words per signal.
const (
	slotCode  = 0
	slotPID   = 1
	slotUID   = 2
	slotWords = 4
)

// prevHandler holds, per signal, the handler the trampoline DISPLACED — for
// every signal Go watches that is runtime.sigtramp, and the trampoline
// tail-jumps into it so the delivery reaches signal.Notify exactly as before.
var prevHandler [sigMax]uintptr

// armed marks the signals whose deliveries are being CAPTURED right now.
var armed [sigMax]int32

// installed marks the signals whose trampoline is in place in the kernel. It is
// a separate fact from armed on purpose: Release stops the CAPTURE but leaves
// the chain standing (see Release), so a later Arm must be able to tell "the
// trampoline is still installed above someone else's handler" from "nothing was
// ever installed" — without it, re-arming read its OWN trampoline as the
// displaced handler and armed nothing (measured: the second and third test arms
// reported Valid=false while the first passed).
var installed [sigMax]int32

// armMu serialises Arm/Release: the pairs (get-sigaction, set-sigaction) and
// (prevHandler[sig] write, armed[sig] store) are not atomic against each other,
// and two goroutines arming concurrently would otherwise both install and one
// would leak the other's chain target.
var armMu sync.Mutex

// tramp is the SA_SIGINFO entry point (trampoline_amd64.s). It is declared with
// no arguments because the kernel calls it with the C ABI — the three real
// arguments (sig, *siginfo_t, *ucontext_t) arrive in DI/SI/DX and the assembly
// reads them there. Only its CODE ADDRESS is used.
func tramp()

// Delivered returns the kernel's account of the most recent delivery of sig to
// THIS process. Valid is false when this signal was never armed or no delivery
// has been captured yet — the caller then writes no si_code fields at all, which
// is the pre-epic record shape.
//
// It is a read of a snapshot, not a subscription: call it from the goroutine that
// is handling the delivery (internal/signalrec does exactly that) so the answer
// describes the signal being recorded and not a later one of the same kind.
func Delivered(sig os.Signal) Source {
	s, ok := sig.(syscall.Signal)
	if !ok {
		return Source{}
	}
	n := int(s)
	if n <= 0 || n >= sigMax {
		return Source{}
	}
	if atomic.LoadInt32(&armed[n]) == 0 {
		return Source{}
	}
	code := atomic.LoadInt32(&captured[n*slotWords+slotCode])
	pid := atomic.LoadInt32(&captured[n*slotWords+slotPID])
	uid := atomic.LoadInt32(&captured[n*slotWords+slotUID])
	return Source{Valid: true, Code: code, SenderPID: pid, SenderUID: uint32(uid)}
}

// Arm installs the capturing trampoline for every signal in sigs, chaining to
// whatever handler currently owns it (in a gopi process that is Go's own
// runtime.sigtramp — see the package doc). It is idempotent per signal: arming a
// signal twice re-reads the CURRENT handler, so the chain can never point at
// this package's own trampoline and recurse.
//
// An error means the kernel refused an rt_sigaction; every signal that succeeded
// before the failure stays armed (the caller's only sane response is to record
// the failure and go on recording, which is what internal/signalrec does).
func Arm(sigs []os.Signal) error {
	armMu.Lock()
	defer armMu.Unlock()
	// THE OUTCOME IS A RECORDABLE FACT, not only a line on stderr. Every success
	// CLEARS a previous refusal (a re-arm that worked must not leave the process
	// permanently labelled "arm-failed"), and every refusal is KEPT so the
	// record's si_capture reason can quote it instead of leaving a reader to
	// guess why the kernel's word is missing (Rule 15: say it, never swallow it).
	noteArmFailure("")
	for _, sig := range sigs {
		s, ok := sig.(syscall.Signal)
		if !ok {
			continue
		}
		n := int(s)
		if n <= 0 || n >= sigMax {
			continue
		}
		if err := armOne(n); err != nil {
			noteArmFailure(err.Error())
			return err
		}
	}
	return nil
}

// armOne is Arm's per-signal step, called with armMu held.
func armOne(n int) error {
	var old kernelSigaction
	if err := rtSigaction(n, nil, unsafe.Pointer(&old)); err != nil {
		return fmt.Errorf("sigsource: read the descriptor of signal %d: %w", n, err)
	}
	fn := tramp
	pc := reflect.ValueOf(fn).Pointer()
	if pc == old.handler {
		if atomic.LoadInt32(&installed[n]) != 0 {
			// ALREADY OURS, chain intact: only the capture flag needs setting.
			// Re-installing here would make the chain point at this very
			// trampoline and the first delivery would jump to itself forever.
			atomic.StoreInt32(&armed[n], 1)
			return nil
		}
		// Our trampoline sits in the kernel but the chain is not ours to
		// trust (a Release/arm cycle crossed another layer's re-install).
		// Fall through and chain on prevHandler as recorded; if that is
		// unset the delivery has nowhere to go, which is the state the
		// recursion guard must never create.
		if prevHandler[n] == 0 {
			return fmt.Errorf("sigsource: signal %d already carries an untraceable trampoline; refusing to chain", n)
		}
		atomic.StoreInt32(&installed[n], 1)
		atomic.StoreInt32(&armed[n], 1)
		return nil
	}
	prevHandler[n] = old.handler
	act := kernelSigaction{
		handler: pc,
		// The flags are INHERITED, then SA_SIGINFO is ensured: the previous
		// handler's SA_ONSTACK (run on the signal stack), SA_RESTART and
		// SA_RESTORER are part of how the displaced handler was reached, and a
		// trampoline that dropped them would change the disposition it is only
		// supposed to observe. SA_ONSTACK in particular: Go's sigtramp must run
		// on gsignal's stack, not on the goroutine's.
		flags:    old.flags | saSIGINFO | saONSTACK | saRESTART,
		restorer: old.restorer,
		mask:     old.mask,
	}
	if err := rtSigaction(n, unsafe.Pointer(&act), nil); err != nil {
		prevHandler[n] = 0
		return fmt.Errorf("sigsource: install the capture trampoline for signal %d: %w", n, err)
	}
	atomic.StoreInt32(&installed[n], 1)
	atomic.StoreInt32(&armed[n], 1)
	return nil
}

// Release stops capturing: Delivered reports Valid=false from now on, and the
// next Arm re-enables it without re-chaining. It does NOT restore a disposition
// — that is the runtime's and os/signal's business, and a package whose whole
// promise is "observation only" must not be the thing that puts a handler back
// (or, worse, a SIG_DFL onto a signal some other layer still owns). The
// trampoline therefore stays in the kernel, still tail-jumping into the handler
// it displaced: an unarmed signal keeps its exact disposition, it is simply no
// longer recorded.
func Release(sigs []os.Signal) {
	armMu.Lock()
	defer armMu.Unlock()
	for _, sig := range sigs {
		s, ok := sig.(syscall.Signal)
		if !ok {
			continue
		}
		n := int(s)
		if n <= 0 || n >= sigMax {
			continue
		}
		atomic.StoreInt32(&armed[n], 0)
	}
}

// rtSigaction is rt_sigaction(2) with the kernel's 8-byte sigset size, so the
// descriptor is read and written in the shape the kernel actually stores (libc
// wraps a 128-byte sigset_t; using it here would mis-size the struct).
func rtSigaction(sig int, act, old unsafe.Pointer) error {
	_, _, errno := syscall.Syscall6(syscall.SYS_RT_SIGACTION, uintptr(sig),
		uintptr(act), uintptr(old), kernelSigsetSize, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// kernelSigsetSize is the sigset size rt_sigaction(2) expects on this target —
// the kernel's own 64-bit signal mask, NOT libc's 128-byte sigset_t.
const kernelSigsetSize = 8

// UnsupportedReason returns "" on this target: the capture is implemented.
func UnsupportedReason() string { return "" }
