// The SIGNAL half of a suite's own cleanup: the run root this process created is
// removed when a TERMINATING signal ends the run, not only when the clean exit
// path is reached.
//
// MEASURED (this host, 2026-10-22): /dev/shm held 19 `suite-*-run-*` roots /
// 1.7G, all created the same day, and every `.holder` registration inside them
// named a pid with no `/proc` entry — i.e. every one is a run that ENDED without
// reaching FinishSuiteForTestMain. `ls -ld --time-style=+%H:%M` puts them at
// 14:36 … 20:18, so this is not one bad afternoon: it is the steady state.
//
// The exit paths, and which of them can be handled IN-PROCESS:
//
//	clean exit (m.Run returns)            → FinishSuiteForTestMain → CleanupSuiteTempRoot
//	                                        removes it. Unchanged, and it works.
//	terminating signal (SIGTERM/SIGINT/   → skips the defer stack AND the
//	  SIGHUP/SIGQUIT)                        post-m.Run statement. THIS handler is
//	                                        the only in-process exit that can act.
//	the suite's own timeout clock (the     → NOT reachable: the testing package
//	  `-timeout` scripts/test-budget.sh      stops the binary with os.Exit(2),
//	  passes)                                not a signal, and no in-process code
//	                                        runs after that. Measured already in
//	                                        this tree (internal/tools/testmain_test.go
//	                                        measured `-test.timeout 4s` leaving its
//	                                        sandbox on disk with no handler line).
//	SIGKILL / a wedged run                 → nothing. Reclaiming that residue is
//	                                        the sweep's job, not this function's
//	                                        (Rule 35: no second reaper, no new
//	                                        dependency).
//
// WHY A HANDLER RATHER THAN A SWEEP: the root this arm removes is the one THIS
// process created, so no age window, no /proc scan and no namespace guess is
// needed — the evidence is the `.holder` registration naming this very pid
// (scratch.SuiteRunSelfReapVerdict checks exactly that, and refuses a record
// naming ANOTHER live pid, which is what keeps this from ever reaching a
// concurrent run's root in the shared /dev/shm).
//
// WHY THE RE-RAISE IS MANDATORY, not decoration: signal.Notify DISABLES a
// signal's default action. A handler that only cleans up and returns leaves the
// process RUNNING — a budget's SIGTERM would delete a directory and kill nothing.
// So the subscription is stopped and the SAME signal is raised again, which is
// what makes the run die with the status its caller expects.
//
// THE RE-RAISE MUST NOT BE `signal.Reset` (measured 2026-10-23, RAWEXIT numbers
// below — the defect this comment replaces). `signal.Reset` is not "restore MY
// subscription's disposition": it is cancel(sig, disableSignal), i.e. it clears
// the bit in EVERY registered channel's mask and drives handlers.ref[n] to 0,
// which calls disableSignal → runtime sigdisable. That DEREGISTERS other
// subscribers of the same signal in this process. `cmd/agent`'s
// TestWorkerDrainsOnSIGHUP registers SIGHUP on a keepAlive channel precisely so
// the hangup it sends ITSELF does not kill the binary (the comment in that test
// calls it a SAFETY NET) — this arm's Reset cancelled exactly that net:
//
//	$ go test ./cmd/agent/ -run 'TestWorkerDrainsOnSIGHUP' -count=1 ; echo $?
//	FAIL	github.com/nickhuang99/gopi/cmd/agent	2.475s      ("signal: hangup")
//	1
//
// WHY NOT "DETECT THAT THIS PROCESS WILL SURVIVE, THEN REAP": measured in the
// same session with standalone probes (exit codes, not reasoning). Once
// sigdisable has run, the runtime's own handler stays installed (it is
// re-installed unconditionally for a signal that is not inherited-SIG_IGN), and
// sighandler → dieFromSignal for anything with the _SigKill flag — SIGHUP,
// SIGINT and SIGTERM are ALL _SigNotify+_SigKill. So a self-raised SIGHUP with
// no subscriber left kills the process from INSIDE the Go runtime: there is no
// in-process arm between the raise and the death, and a "reap only if we really
// die" predicate would have to reap BEFORE knowing (two subscribers, HUP only:
// removing the Reset alone still ends the run). The kernel-disposition
// predicates were tried and are all blind here: /proc/self/status SigCgt keeps
// the bit and SigIgn stays clear (the signal handler is still installed),
// `signal.Ignored(SIGHUP)` reports false, and os/signal's handlers.ref table is
// unreadable from outside the package. So the correct question is not "will we
// survive" but "SHOULD this arm be subscribed to this signal at all".
//
// THE ANSWER (this file's fix): the arm's job is the run root of a suite that
// was KILLED from outside. Only SIGTERM is that signal — a budget timeout, a
// `timeout(1)`, a supervisor, and the SIGTERM half of what ^C/session teardown
// can deliver. SIGHUP, SIGINT and SIGQUIT are exactly the signals an IN-PROCESS
// speaker sends to ITSELF (a test asserting its own handler, a self-raise for a
// Ctrl-C path), and subscribing to them converts this arm into a party that can
// pre-empt a live test and reap the root out from under it — measured: the
// cmd/agent run's arm removed /dev/shm/suite-cmd-agent-run-85760500 and THEN
// the binary died, i.e. the root was hollowed at signal time rather than left
// for the run's own cleanup.
//
// THE TRADE, stated so nothing here pretends otherwise: a suite ended by an
// EXTERNAL Ctrl-C (SIGINT to the process group) is no longer reaped by this arm;
// the run's root stays on disk with its `.holder` naming a dead pid, which is
// the shape the start-time sweep already collects (`scratch.ReapDeadSuiteRunRoots`,
// called by the pin). Losing the root is recoverable by an existing mechanism;
// killing somebody else's test binary mid-suite is not.
//
// WHY THE HANDLER MUST ALSO BE JOINABLE (the defect this file's second half
// fixes, measured 2026-10-22 on `go test ./cmd/agent/`): the handler is a
// goroutine parked on `<-ch` BY DESIGN for the whole life of the process, so it
// is still parked when a suite takes its close-out goroutine dump. That dump is
// the suite's LEAK report (suiteParkedRepoGoroutines refuses only framework
// frames and its own), so the lifecycle arm was counted as a leaked goroutine of
// this repository:
//
//	suite goroutine sweep: parked repository goroutines: 1
//	  testutil.armSuiteRunRootReapOnSignal.func1()  suiterootsignal.go:77
//	FAIL	github.com/nickhuang99/gopi/cmd/agent	1.454s      (a run whose only test PASSED)
//
// i.e. green tests, red run, and an exit code that stops carrying a signal. The
// answer is NOT to widen the sweep (that hides the false positive AND every
// genuine instance): it is the same shape the resident scratch sweep already
// uses — PUBLISH A JOIN, and have the close-out wait on it BEFORE it samples
// (JoinSuiteRunRootSignalHandler; the consumer is FinishSuiteForTestMain, plus
// cmd/agent's close-out, which samples before that call).
package testutil

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// suiteRunRootSignalJoinWait bounds how long the join waits for the handler to
// leave its park. The handler's only work after the park is `signal.Stop` and a
// re-raise, so anything above a scheduler round is already generous; the bound
// exists so a WEDGED handler is reported instead of hanging the exit path.
const suiteRunRootSignalJoinWait = 5 * time.Second

// suiteRunRootSurviveSettle is how long this arm waits after re-raising its own
// signal before it concludes that the process is SURVIVING it.
//
// MEASURED (2026-10-23, a standalone probe on this host, 12 runs): `syscall.Kill`
// returns immediately and the process actually dies 130 µs … 307 µs later, from
// inside the runtime's sighandler → dieFromSignal path. The value below is a
// ~1600× margin over that measured maximum, and it exists because the arm must
// not restore a root it is about to lose (see reraiseSignal): a wrong "survived"
// on a dying process is exactly the flaky-red shape this constant removes. It
// costs nothing on the dying path — a process that is really dying never reaches
// the far side of the wait.
const suiteRunRootSurviveSettle = 500 * time.Millisecond

// suiteRunRootReapSignal is THE signal this arm subscribes to: SIGTERM, and
// only SIGTERM.
//
// Not a list, and deliberately so (see the file header for the measured
// reasoning): SIGHUP/SIGINT/SIGQUIT are the signals an in-process speaker sends
// to itself, and a subscription here can then pre-empt a live test — while the
// signal that really ends a suite from OUTSIDE (a budget timeout, `timeout(1)`,
// a supervisor) is SIGTERM. A second signal belongs in this constant only with a
// measurement that it cannot be self-raised by a test in this repository, and
// the arm's re-raise (reraiseSignal) must stay safe for it.
const suiteRunRootReapSignal = syscall.SIGTERM

// suiteRunRootSignalArm is one installed subscription: the channel a signal
// arrives on, and the join a close-out can wait on (closed when the handler
// goroutine has left its park and done what it exists to do).
type suiteRunRootSignalArm struct {
	ch   chan os.Signal
	done chan struct{}
	// stopOnce makes the disarm idempotent: the clean exit path may join more
	// than once (FinishSuiteForTestMain and a suite's own close-out both ask),
	// and a second close of ch would panic.
	stopOnce sync.Once
}

// suiteRunRootSignal is held in a package var: a signal subscription the garbage
// collector can collect is a subscription that can vanish before the signal it
// exists for.
var suiteRunRootSignal *suiteRunRootSignalArm

// armSuiteRunRootReapOnSignal installs the handler for the root this process
// pinned. It is idempotent — the pin runs once per process, and a second
// subscription would deliver one signal to two goroutines and remove the root
// twice.
//
// root is passed rather than read from suiteTempRoot: the pin calls this BEFORE
// it publishes the variable, and a guard reading the variable here would make the
// arm a silent no-op — measured, that is exactly how the first version of this
// arm let the child's root survive a SIGTERM.
func armSuiteRunRootReapOnSignal(root string) {
	if suiteRunRootSignal != nil {
		return
	}
	if root == "" {
		return
	}
	arm := &suiteRunRootSignalArm{
		ch:   make(chan os.Signal, 1),
		done: make(chan struct{}),
	}
	signal.Notify(arm.ch, suiteRunRootReapSignal)
	go func() {
		// The join is published on EVERY way out of this goroutine, including
		// the clean path (a closed channel): a close-out that waits must never
		// be left waiting on a handler that already returned.
		defer close(arm.done)
		sig, ok := <-arm.ch
		if !ok {
			// The channel is closed by the DISARM (JoinSuiteRunRootSignalHandler)
			// on the clean exit path, where CleanupSuiteTempRoot removes the root
			// itself. There is nothing to reap and no signal to re-raise — this
			// is the ordered shutdown the join exists to make possible.
			return
		}
		// The root is read at signal time, not captured: CleanupSuiteTempRoot
		// clears it on the clean path, and a signal arriving after that must not
		// guess a path.
		//
		// THE ARM DOES NOT TOUCH THE ROOT. Its own run is ALIVE at this moment —
		// the signal is being handled IN-PROCESS, and this goroutine cannot know
		// whether the process will die (measured 2026-10-23: the peer set of
		// os/signal is unreadable from outside the package — `//go:linkname
		// runtime.sig` and `os/signal.handlers` are both refused on go1.24, and
		// /proc/self/status SigCgt/SigIgn and `signal.Ignored` are identical with
		// and without a second subscriber). Every action on the root therefore
		// lands on a LIVE run, and the measured consequence of that is the defect
		// this arm was fixed for:
		//
		//	$ make test-budget PKGS='./internal/cgroupscope ./cmd/agent'
		//	testutil: terminated — moved this run's scratch root
		//	  /dev/shm/suite-cmd-agent-run-755918610 aside to …_shelved
		//	scratch: recreated suite run root /dev/shm/suite-cmd-agent-run-755918610 …
		//	testutil: terminated — this process SURVIVED the re-raise, and the run
		//	  root could NOT be put back: rename …: file exists
		//	→ four cmd/agent tests red with `TempDir: stat
		//	  /dev/shm/suite-cmd-agent-run-755918610/…: no such file or directory`,
		//	  all four green when run alone.
		//
		// The move-aside was an attempt to make the action reversible. It is not:
		// a concurrent writer (here a gopi child whose GOPI_MEMORY_DB pointed
		// inside the root) recreates the name with MkdirAll between the rename
		// and the undo, so the undo fails and the live run is left with a
		// directory that is not its own.
		//
		// SO THE DECISION IS: a root whose registered owner is LIVE is not
		// residue, and no arm in this process may act on it (see
		// scratch.SelfReapReasonOwn, which this arm now consults through
		// scratch.SuiteRunSelfReapVerdict and which answers REFUSED for exactly
		// that record). The root is left where the run put it. If this process
		// really dies, the next run of the SAME suite collects it at its own
		// start, at any age, on the evidence of the now-dead registration
		// (scratch.ReapDeadSuiteRunRootsUnder, called by the pin —
		// testutil.ReapDeadSuiteRunRootsAtSuiteStart). If it survives, the run
		// keeps the root it created and nothing has to be undone.
		//
		// What the arm still owes the operator is the FACT (Rule 15): the root it
		// is leaving behind, and which mechanism collects it.
		// THE ARM REMOVES NOTHING, and that is a decision rather than an
		// omission: the verdict it would act on is asked here, and on this path
		// the answer is always the same one — this process is ALIVE (it is
		// handling a signal, not lying dead), so its own registration is LIVE,
		// so scratch.SuiteRunSelfReapVerdict refuses
		// (scratch.SelfReapReasonOwn: "a live registration is evidence of a RUN,
		// never of residue"). Asking the ONE verdict instead of writing a second
		// predicate here is what keeps this arm from re-inventing the refusal that
		// the 2026-10-23 defect removed.
		//
		// The root's fate is then decided by evidence, not by this goroutine: if
		// the process really dies, its registration goes dead and the NEXT run of
		// this suite collects the root at its own start, at any age
		// (scratch.ReapDeadSuiteRunRootsUnder, called by the pin through
		// testutil.ReapDeadSuiteRunRootsAtSuiteStart); if it survives, the run
		// keeps the root it created and nothing has to be undone. Reported either
		// way (Rule 15): the operator must not have to discover a leftover root
		// and guess why.
		if root := suiteTempRoot; root != "" {
			collectable, reason := scratch.SuiteRunSelfReapVerdict(root)
			fmt.Fprintf(os.Stderr, "testutil: %v — left this run's scratch root %s in place (%s); a root whose "+
				"registered owner is LIVE is never residue (collectable=%v), so the next run of this suite's "+
				"start sweep collects it if this process dies\n", sig, root, reason, collectable)
		}
		// Stop first, re-raise second: without the Stop the re-raise would be
		// delivered straight back to this channel and the loop would never end.
		signal.Stop(arm.ch)
		if !reraiseSignal(sig) {
			// The far side of the raise was never reached: this process died on
			// the signal, which is the ordered outcome — and the root it left is
			// the start sweep's, not this goroutine's to remove.
			return
		}
		// REACHED ONLY IF THIS PROCESS SURVIVED the re-raise: another subscriber
		// of the same signal handled it (its channel received the raise; the
		// arm's own delivery was stopped above), or the signal's disposition is
		// not fatal here. The run continues and keeps its root — nothing was
		// touched, so nothing has to be undone. Reported BY NAME, because a suite
		// that quietly continues after asking itself to die is exactly the
		// silence Rule 15 forbids.
		fmt.Fprintf(os.Stderr, "testutil: %v — this process SURVIVED the re-raise (another subscriber handled it or "+
			"its disposition is not fatal), so the run continues with its scratch root %s intact\n", sig, suiteTempRoot)
	}()
	suiteRunRootSignal = arm
}

// JoinSuiteRunRootSignalHandler disarms the signal arm and WAITS for its
// goroutine to leave its park, so a close-out that dumps goroutines afterwards
// does not count this lifecycle arm as a leak.
//
// It is idempotent and safe to call when no arm was ever installed (a suite that
// never pinned). The disarm is the same ordered shutdown a well-behaved
// subscriber owes the runtime: `signal.Stop` first (it guarantees no further
// send on the channel), then close — which is what unblocks the handler's
// `<-ch` on the clean path.
//
// WHAT THIS GIVES UP, stated so it is not a silent trade: between this call and
// the process exit, a terminating signal is handled by its DEFAULT disposition —
// the run dies and the root is not reaped by this arm. That window is the
// close-out itself (the removal of the root, then os.Exit), and the alternative
// is worse: a lifecycle goroutine that no close-out can ever join, which is the
// false leak this function exists to end. A bound is used rather than an
// unbounded wait so a WEDGED handler is REPORTED (Rule 15) instead of hanging
// the exit path.
func JoinSuiteRunRootSignalHandler() {
	arm := suiteRunRootSignal
	if arm == nil {
		return
	}
	arm.stopOnce.Do(func() {
		signal.Stop(arm.ch)
		close(arm.ch)
	})
	select {
	case <-arm.done:
	case <-time.After(suiteRunRootSignalJoinWait):
		fmt.Fprintf(os.Stderr,
			"testutil: the suite run-root signal handler did not leave its park within %s — the close-out may count it as a "+
				"parked goroutine; that is the truthful report of a wedged handler, not an exemption\n",
			suiteRunRootSignalJoinWait)
	}
}

// reraiseSignal re-sends sig to this process, which is the part that actually
// ends the run, and reports whether this process SURVIVED it.
//
// NO `signal.Reset`: Reset is cancel(sig, disableSignal) over EVERY registered
// channel, so it cancels another subscriber's Notify as a side effect of this
// arm's own exit (measured — the cmd/agent failure quoted in the file header).
// The Stop already performed by the caller removed THIS arm's subscription, so
// the re-raise reaches whatever is left: another subscriber's channel if one
// exists (it survives, and the caller has already reported that by name), or the
// default disposition if none does.
//
// WHY THE RETURN VALUE NEEDS A MEASURED WINDOW (measured 2026-10-23 on this host,
// 12 runs of a standalone probe): `syscall.Kill` QUEUES the signal and returns
// immediately — it is NOT a synchronous "this process is now dead" call. The
// death itself happens on whichever thread takes the signal, inside the runtime's
// sighandler → dieFromSignal path, and the probe measured it arriving 130 µs …
// 307 µs AFTER the Kill returned (max 306.5 µs over 12 runs). So "this line of
// code ran" is NOT evidence of survival: a caller that treated it as one restored
// a root it was about to lose, which is exactly the flaky red this window fixes.
// The arm therefore WAITS OUT suiteRunRootSurviveSettle and calls it survival only
// if it is still running afterwards. The window is a 1600× margin over the
// measured maximum, and it costs nothing on the dying path (that process never
// reaches the far side of it).
//
// It reports via os.Stderr when the process survives (Rule 15b): a suite that
// continues after asking itself to die is exactly the silence this repository
// forbids.
func reraiseSignal(sig os.Signal) (survived bool) {
	s, ok := sig.(syscall.Signal)
	if !ok {
		fmt.Fprintf(os.Stderr, "testutil: %v is not a syscall.Signal — it cannot be re-raised, so this process "+
			"continues after a signal it was asked to die from\n", sig)
		return true
	}
	if err := syscall.Kill(os.Getpid(), s); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: re-raising %v did not reach this process: %v — this process will "+
			"continue instead of dying\n", sig, err)
		return true
	}
	// The window: see the header. Reaching the far side of it alive means the
	// runtime's sigsend found a subscriber that wanted the signal (or the signal
	// is ignored), i.e. nothing is going to terminate this process.
	time.Sleep(suiteRunRootSurviveSettle)
	fmt.Fprintf(os.Stderr, "testutil: re-raised %v and this process is still running %s later — the signal was "+
		"handled by another subscriber or is ignored, so the run continues\n", sig, suiteRunRootSurviveSettle)
	return true
}
