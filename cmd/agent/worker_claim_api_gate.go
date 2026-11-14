package main

// worker_claim_api_gate.go — the PRE-claim half: before the worker's claim scan
// runs, this node measures whether it can reach the LLM API endpoint, and a
// PROVABLY dead node refuses to claim.
//
// WHY IT SITS ON THE CLAIM LEG AND NOWHERE ELSE. The measured cost of NOT having
// it (2026-10-29, HP): a row was claimed on a node with no working path to the
// API host; the run spent 11m38s, three retries and tokens=0, then the row went
// `failed` — while a sibling node answered the same edge IPs in 150ms.
// CORRECTED 2026-10-30 (task 01a18ef7): this line named the post-hoc machinery
// as "internal/tools/task_reroute.go", which reopened the row only AFTERWARDS.
// That file rode the task-lifecycle pass, which had no production caller, so it
// never ran outside its own tests and was deleted. The post-hoc answer an
// operator still has is `gopi task reopen`. A claim is the one instant where
// "should this node take work at
// all" can still be answered for free: nothing has been spent yet, and the row
// stays `pending` for a node that CAN reach the API.
//
// WHAT IT IS NOT. It is not a sweep, a timer, a daemon leg or a new resident
// duty: it is one synchronous bounded call (tools.APIReachabilityPreflight) on the
// claim path, with no state written, no goroutine left behind and no config key
// (Rule 35). It is also not a decision about the WORK: it never touches a row's
// status, never mints a successor and never judges a payload.
//
// THE THREE-VALUED POLICY, STATED HERE BECAUSE IT IS THE CALLER'S:
//
//	reachable    claim as usual.
//	unreachable  REFUSE this round (leave the row pending), print the measured
//	             evidence and the remedy, and let the loop back off and retry —
//	             a node that comes back must resume without an operator.
//	unknown      claim as usual, and say why the measurement was not taken. A
//	             node must still be able to run local work when the probe cannot
//	             be performed; refusing on an unmeasured endpoint would be the
//	             silent-pass defect inverted into a silent park (Rule 15a).
//
// ONE FAILED DIAL IS NEVER THE VERDICT: the ladder inside the preflight is
// tools.apiPreflightAttempts consecutive bounded attempts, so a transient blip
// loses one or two SYNs and the round still claims. Only three consecutive
// failures refuse.

import (
	"time"

	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// workerApiGateRefusalPrefix heads the line a refused claim prints, so an
	// operator grepping a worker log can tell "this node cannot reach the API"
	// apart from "this row failed" — the two are DIFFERENT diagnoses and the
	// ledger must never make one look like the other (epic deliverable 3).
	workerApiGateRefusalPrefix = "worker: claim refused — "

	// workerApiGateBackoff is how long a refused round waits before it may try
	// again. It is the node's own recovery clock: short enough that a link that
	// comes back is used within a minute, long enough that a dead link is not a
	// busy loop of probe ladders (each ladder is apiPreflightAttempts bounded
	// connects). It is NOT a new timer — it is the sleep this loop already has
	// on its drained-queue arm (workerIdleRecheckMS), reused.
	workerApiGateBackoff = time.Second

	// workerApiGateUnknownPrefix heads the once-per-round note that says the
	// measurement was NOT taken, and why. It is a note, not a refusal: the round
	// claims (see the policy above).
	workerApiGateUnknownPrefix = "worker: api reachability not measured — "

	// workerApiGateRefusedExitLine is the --once face's closing line. `--once`
	// is also the unit/E2E seam, so the process must not exit 0 having claimed
	// nothing without saying why — "nothing ran because this node cannot reach
	// the API" is a DIFFERENT outcome from "the queue was drained" and the two
	// lines must never be confused (epic deliverable 3).
	workerApiGateRefusedExitLine = "worker: round not run — this node cannot reach the LLM API endpoint (row left pending; see the measurement above)"

	// workerApiGateRefusedExitCode is what a `--once` worker exits with when it
	// REFUSED to claim because this node is transport-dead. It gets its OWN code
	// for the same reason the argv refusal (2) and the agent-guard refusal (3)
	// have theirs: a caller must be able to tell "nothing ran because this NODE
	// cannot reach the API" from "the queue was drained" (0), "the row failed"
	// (1), "your command line is wrong" (2) and "this launch is forbidden"
	// (3). Collapsing them is the defect this epic's deliverable 3 names: a
	// transport-dead node reported as a drained queue looks like idleness, and
	// a green scheduler retries forever without anyone learning why.
	workerApiGateRefusedExitCode = 4
)

// workerClaimApiGate runs the preflight and reports whether this round may claim.
// When it may not, the evidence is PRINTED (host, every attempt's duration and
// error, and the remedy) — a bare refusal would leave an operator with no way to
// tell a dead node from a broken worker (Rule 15).
//
// The measurement is injected as a parameter so the decision — refuse only on a
// PROVEN-dead node — is testable without any network at all, while the production
// caller passes tools.APIReachabilityPreflight unchanged.
func workerClaimApiGate(measure func() tools.APIReachabilityProbe) bool {
	probe := measure()
	if probe.AllowsClaim() {
		if probe.Verdict == tools.APIReachabilityUnknown {
			hostLifecyclePrint(workerApiGateUnknownPrefix+"%s\n", probe.Evidence())
		}
		return true
	}
	hostLifecyclePrint("%s%s\n", workerApiGateRefusalPrefix, probe.Evidence())
	return false
}

// workerClaimApiGateDefault is the production reading of the gate: measure with
// the real preflight and print through the loop's own lifecycle sink.
func workerClaimApiGateDefault() bool {
	return workerClaimApiGate(tools.APIReachabilityPreflight)
}

// workerApiGateBackoffWait is the wait between a refused round and the next
// attempt. It is a variable so a test drives the loop's recovery without spending
// the real second; production keeps the constant.
var workerApiGateBackoffWait = func() { time.Sleep(workerApiGateBackoff) }

// workerApiGateRefused is the process-local record of the LAST claiming round's
// verdict: true when that round refused to claim because the endpoint is
// unreachable. It exists so the LOOP can tell this refusal apart from a drained
// queue — the two produce the same `ran == false` but mean opposite things, and
// a loop that conflated them would count a transport-dead node toward its idle
// self-exit and report "queue drained" while work was waiting (Rule 15: never one
// message for two causes).
//
// It is a mutable package-level flag for the same reason workerStopState is: the
// round and the loop are two functions on one goroutine, and this is the value
// passed between them. Nothing reads it across processes, nothing persists it.
var workerApiGateRefusedFlag bool

// workerApiGateMarkRefused publishes a refused round to the loop.
func workerApiGateMarkRefused() { workerApiGateRefusedFlag = true }

// workerApiGateConsumeRefusal reads and CLEARS the flag, so one refusal is acted
// on once (the consume shape the drain guard uses for its own request file).
func workerApiGateConsumeRefusal() bool {
	defer func() { workerApiGateRefusedFlag = false }()
	return workerApiGateRefusedFlag
}

// workerApiGateRefusedExit is read once by the process entry point after the
// loop returns; nothing else reads it and nothing persists it.
var workerApiGateRefusedExit bool

// workerApiGateRecordRefusedExit records that this process's `--once` loop ended
// on a REFUSED claim, so the caller leaves with workerApiGateRefusedExitCode.
//
// WHY A FLAG AND NOT os.Exit FROM THE LOOP: the loop's own deferred teardown (the
// state writer, the request cleanup, the stop-file watcher) is what makes a
// worker's exit observable at all, and os.Exit skips every one of them. The loop
// therefore returns normally and the ENTRY POINT reads this, exactly as the
// drained/self-exit arms do.
func workerApiGateRecordRefusedExit() { workerApiGateRefusedExit = true }

// workerApiGateRefusedExitCodeFor reports the exit code this process must leave
// with, 0 when no refusal happened.
func workerApiGateRefusedExitCodeFor() int {
	if !workerApiGateRefusedExit {
		return 0
	}
	return workerApiGateRefusedExitCode
}
