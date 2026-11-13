// settle.go — the ONE forced-GC window the borrowed-fd guards measure through.
//
// It lives in internal/borrowedfd because a GC finalizer is the hazard the
// retention idiom in borrowedfd.go exists for, and because this package is the
// leaf every consumer of that idiom can import: one definition of the measuring
// instrument (Rule 14) instead of a copy per test package — and, after
// 2026-09-19, instead of borrowing one from a test file another task may retire.
//
// WHY A WINDOW IS NEEDED AT ALL: runtime.SetFinalizer's callback runs
// asynchronously on the runtime's own goroutine (runtime.runfinq), and a single
// runtime.GC() usually does not run it. A guard that drops a wrapper and
// immediately asks the kernel about the number therefore observes an OPEN
// descriptor whether or not the wrapper was detached — it is green for the wrong
// reason. The rounds below give runfinq a bounded chance to act, so the
// observation separates "detached" from "dropped".
//
// A SCHEDULING WINDOW, NEVER A WORKLOAD (PROJECT.md Rule 27 — measure the code,
// never the host): no device I/O, no parallelism, no pressure. The pause is
// awaited so runfinq can be scheduled; nothing here measures the machine.
//
// Production never calls this. It is exported so the guards of every package
// that borrows a descriptor — cmd/agent, internal/daemonlifetime,
// internal/testutil — can measure through the same window.
package borrowedfd

import (
	"runtime"
	"time"
)

const (
	// finalizerSettleRounds and finalizerSettlePause bound the window
	// SettleFinalizers opens for runfinq. Named constants (Rule 1): the window is
	// the instrument, so its size is stated once and read by every guard that
	// reports it.
	finalizerSettleRounds = 8
	finalizerSettlePause  = 10 * time.Millisecond
)

// SettleFinalizers gives the runtime's asynchronous finalizer goroutine
// (runfinq) a bounded window to act.
//
// A caller that wants to know what a DROPPED wrapper did to a borrowed number
// must let its wrapper become unreachable first (a local that goes out of scope,
// a `_ =` assignment), then call this, then ask the KERNEL about the number.
func SettleFinalizers() {
	for i := 0; i < finalizerSettleRounds; i++ {
		runtime.GC()
		runtime.Gosched()
		time.Sleep(finalizerSettlePause) //stylegate:allow-sleep bounded scheduling window for the runtime's asynchronous finalizer goroutine (runfinq) — a GC-settle wait, never a poll for an external event, and never a workload (Rule 27)
	}
}

// FinalizerSettleRounds is how many forced-GC rounds SettleFinalizers runs, so a
// guard can name the window it measured through without repeating the number
// (Rule 1).
func FinalizerSettleRounds() int { return finalizerSettleRounds }
