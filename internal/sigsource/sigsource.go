// Package sigsource reports the LINUX KERNEL'S OWN ANSWER to "who sent this
// signal" — si_code (and, for the codes that carry one, the sender's pid) from
// the siginfo_t the kernel builds at delivery.
//
// WHY THIS PACKAGE EXISTS (epic signal-provenance-si-code). internal/signalrec
// records, at the instant a signal lands, the victim's position in the process
// tree and its role — everything EXCEPT who sent the signal, because the Go
// standard library exposes no SA_SIGINFO sender to a handler (os/signal hands a
// bare os.Signal). Every post-mortem therefore ended "sender undetermined", and
// the file's `sourced` verdict could only say whether a GOPI STOP PATH announced
// the kill — silence meant "unsourced", which reads as "an operator did it" even
// when the sender was the kernel (a terminal hangup, a cgroup teardown, a timer).
//
// WHY IT CAN BE DONE WITHOUT CGO. Go installs every watched signal with
// SA_SIGINFO and an ABI0 trampoline (runtime/signal_unix.go setsig:
// `_SA_SIGINFO|_SA_ONSTACK|_SA_RESTORER|_SA_RESTART`, sa_handler =
// runtime.sigtramp). So the kernel ALREADY passes a full siginfo_t to this
// process; the runtime just does not read the sender fields out of it. This
// package installs a thin SA_SIGINFO trampoline of its own — chained to the one
// it displaces — that copies si_code/si_pid/si_uid into memory and then jumps to
// the ORIGINAL handler. The signal ends where it always did, on the same
// goroutine, with the same disposition, one evidence record richer.
//
// WHAT IT DOES NOT DO (Rule 42 and the epic's "observation only"):
//
//   - it never SENDS a signal. The only sender in this package is a test.
//   - it never changes a disposition: SIG_DFL stays SIG_DFL, every installed
//     handler keeps being called (the chain), and the flags/restorer it writes
//     are the ones it READ from the descriptor it replaced.
//   - an unarmed signal, a non-Linux host and an arming failure all degrade to
//     "no source" (Source.Valid == false), which the recorder renders by leaving
//     its fields out — the OLD record shape, byte for byte.
package sigsource

import (
	"fmt"
	"sort"
)

// Source is what the kernel said about one delivered signal. Valid is the
// capture flag, and it is what makes "the sender is SI_USER(0)" distinguishable
// from "nobody looked": si_code's most important value IS the zero value
// (Rule 15a) — a reader that treated 0 as absent would silently turn every
// self-kill into an unknown sender.
type Source struct {
	// Valid reports that a trampoline was installed for this signal AND it has
	// captured at least one delivery. Every other field is meaningless when it
	// is false.
	Valid bool
	// Code is si_code: SI_USER(0), SI_TKILL(-6), SI_KERNEL(128), SI_QUEUE(-1) …
	Code int32
	// SenderPID is si_pid — the pid of the process that called kill()/tgkill()/
	// sigqueue(). The kernel fills it for the SI_FROMUSER codes (si_code <= 0)
	// and does NOT for kernel-generated ones, where the value is whatever the
	// kernel chose to put there; read it only through Sender().
	SenderPID int32
	// SenderUID is si_uid, the real UID of the sender, same rule.
	SenderUID uint32
}

// Code is the si_code vocabulary (asm-generic/siginfo.h), declared here because
// golang.org/x/sys/unix does NOT export these names (measured: no SI_USER /
// SI_KERNEL / SI_TKILL symbol in unix@v0.37.0 for any GOOS) and a bare literal
// 0x80 in a comparison is exactly what Rule 1 forbids.
const (
	// SICodeUser — "sent by kill, sigsend, raise": an ordinary process.
	SICodeUser = int32(0)
	// SICodeKernel — "sent by the kernel from somewhere" (a fault, an execve
	// thread teardown, a cgroup OOM kill). 0x80, never a small negative number.
	SICodeKernel = int32(0x80)
	// SICodeQueue — "sent by sigqueue" (a queued signal carries si_value).
	SICodeQueue = int32(-1)
	// SICodeTimer — "sent by timer expiration" (POSIX interval timer).
	SICodeTimer = int32(-2)
	// SICodeMesgq — "sent by real time mesq state change".
	SICodeMesgq = int32(-3)
	// SICodeAsyncIO — "sent by AIO completion".
	SICodeAsyncIO = int32(-4)
	// SICodeSigIO — "sent by queued SIGIO".
	SICodeSigIO = int32(-5)
	// SICodeTKill — "sent by tkill system call": tgkill(2)/tkill(2), which is
	// what a thread-directed kill looks like (Go's own signal forwarding uses
	// raise/tgkill on some paths).
	SICodeTKill = int32(-6)
	// SICodeDThread — "sent by execve() killing subsidiary threads".
	SICodeDThread = int32(-7)
	// SICodeRTFloor is the lowest si_code Linux uses for the realtime band
	// (-SIGRTMAX): every code in [SICodeRTFloor, SICodeQueue] is "SIGRTMIN+n
	// arrived", which is why CodeName renders those as SI_RT<n> instead of an
	// unknown number.
	SICodeRTFloor = int32(-64)
)

// The names si_code_name carries — one spelling, matched to the kernel's own
// macro names so an operator can copy the value into a man-page search instead
// of decoding a number (Rule 14: a shared value has ONE home).
const (
	SICodeNameUser    = "SI_USER"
	SICodeNameKernel  = "SI_KERNEL"
	SICodeNameQueue   = "SI_QUEUE"
	SICodeNameTimer   = "SI_TIMER"
	SICodeNameMesgq   = "SI_MESGQ"
	SICodeNameAsyncIO = "SI_ASYNCIO"
	SICodeNameSigIO   = "SI_SIGIO"
	SICodeNameTKill   = "SI_TKILL"
	SICodeNameDThread = "SI_DETHREAD"
	// SICodeNameRT signals a realtime-signal si_code (SIGRTMIN+n → -(n)), which
	// carries no named macro.
	SICodeNameRT = "SI_RT"
	// SICodeNameUnknown prefixes a value with no name in this vocabulary —
	// never an empty string, because "we do not know this code" is a fact and
	// silence is not (Rule 15).
	SICodeNameUnknown = "SI_UNKNOWN"
)

// siCodeNames is the value→name table. Rule 13: the constants above are the
// only place these numbers are written.
var siCodeNames = map[int32]string{
	SICodeQueue:   SICodeNameQueue,
	SICodeTimer:   SICodeNameTimer,
	SICodeMesgq:   SICodeNameMesgq,
	SICodeAsyncIO: SICodeNameAsyncIO,
	SICodeSigIO:   SICodeNameSigIO,
	SICodeTKill:   SICodeNameTKill,
	SICodeDThread: SICodeNameDThread,
	SICodeKernel:  SICodeNameKernel,
}

// CodeName renders si_code the way an operator reads it. Total: every int32 has
// a name (SI_USER for 0 by construction of the map's missing key is NOT assumed
// — see below), so a reader never has to ask "and what is an empty name?".
//
// SI_USER is handled explicitly rather than as the map's zero-value answer: a
// missing key and "the sender was an ordinary process" would otherwise be the
// same answer, which is the confusion this whole package exists to remove.
func CodeName(code int32) string {
	if code == SICodeUser {
		return SICodeNameUser
	}
	if name, ok := siCodeNames[code]; ok {
		return name
	}
	if code <= 0 && code >= SICodeRTFloor {
		// Linux maps si_code values -1..-64 onto the realtime signals
		// (SIGRTMIN+n ⇒ -(n)); queued realtime deliveries land here.
		return fmt.Sprintf("%s%d", SICodeNameRT, -code)
	}
	return fmt.Sprintf("%s(%d)", SICodeNameUnknown, code)
}

// FromProcess reports whether the kernel attributed this delivery to a PROCESS
// (si_code <= 0, the SI_FROMUSER class) — the二分 this epic is about: a kill()
// somebody made vs a signal the kernel raised on its own. SI_USER, SI_TKILL,
// SI_QUEUE, SI_TIMER, SI_MESGQ, SI_ASYNCIO and the realtime codes are all
// SI_FROMUSER; SI_KERNEL(128) and positive fault codes are not.
func FromProcess(code int32) bool { return code <= 0 }

// names lists the vocabulary in a stable order — for a caller that must PRINT
// what it understands (a diagnostic, a test's failure message) instead of
// re-deriving the set from the map (Rule 13).
func names() []string {
	out := []string{SICodeNameUser}
	for _, code := range []int32{SICodeQueue, SICodeTimer, SICodeMesgq, SICodeAsyncIO, SICodeSigIO, SICodeTKill, SICodeDThread, SICodeKernel} {
		out = append(out, siCodeNames[code])
	}
	sort.Strings(out)
	return out
}
