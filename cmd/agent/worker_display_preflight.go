package main

// worker_display_preflight.go — the DISPLAY ANNOUNCEMENT a worker makes at loop
// start (epic worker-spawn-doors, GAP 1; decision record:
// devdoc/worker-display-ownership.md).
//
// THE DEFECT THIS CLOSES. A worker (or a worker daemon) is a DETACHED process:
// setsid + nohup put it in its own session but inherit the launcher's
// environment verbatim. DISPLAY is inherited, never acquired — so a worker
// started from a shell whose DISPLAY is an SSH forward (localhost:N) carries a
// display whose socket dies with the forward. Every GUI tool call it makes
// afterwards fails with exit 1, and NOTHING said so at start: the failure
// surfaced on the first screen_capture, hours later, as a tool error attributed
// to the tool rather than to the launch (Rule 15: no silent failure).
//
// WHY ANNOUNCE AND NOT REFUSE (decision 1, with its rejected alternatives in
// the devdoc): DISPLAY is not what makes a worker a worker. A worker that only
// drains text tasks is perfectly useful with no display at all, so refusing the
// start would take the operator's fleet down for a capability most tasks never
// use — and the launcher would then have to be the arbiter of a policy
// ("do you intend to use the GUI?") it cannot observe. Announcing is the
// honest form of the same information: the operator reads one line at start
// and knows whether the GUI half of the fleet is armed.
//
// WHY HERE (one seam, every spawn door): every worker face — the launcher
// script's `setsid nohup gopi worker`, the foreground `gopi worker`, and
// `gopi worker --daemon` — converges on runTaskWorkerLoop, so a single call
// there covers all three. The launcher script itself is the WRONG place: it
// would have to re-implement the display grammar and the auth lookup (Rule
// 14 violation), and it could not cover the other two doors.
//
// ZERO NEW KNOBS (Rule 1b/13/21 + internal/testutil/procname_gate_test.go): the
// preflight adds NO environment contract. The display is read through
// internal/shot's ONE reader (shot.ProbeDisplay → captureDisplayName →
// $DISPLAY, the same lookup screen_capture and act use), so the announcement
// cannot disagree with the capability it describes.

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/shot"
)

// workerDisplayLabel is the one-word prefix of every line this file prints. It
// is spelled once: the announcement, the log line and the tests all read it, so
// a grep for the worker's display verdict finds every site.
const workerDisplayLabel = "display preflight"

// workerLocalDisplay and workerForwardedDisplayShape name the two displays the
// remedy sentence contrasts. They are constants, not inline literals (Rule 1b
// as extended by devdoc/worker-localhost-display.md):
// the same two display names are the subjects of docs and tests, so a rename
// must be a one-line change here rather than a grep-and-hope. This host's
// durable local display IS the SSH-forward target the operator's own shells
// carry — the REMEDY names exactly the display the CONSEQUENCE sentence
// warns about, and that paradox is the measured fact (localhost:NN.0 and the
// local socket answer to the same :NN), documented in
// devdoc/worker-display-ownership.md.
const (
	workerLocalDisplay          = "Xvfb :99"
	workerForwardedDisplayShape = "localhost:N"
)

// workerUnreachableRoutesTried is the sentence that explains WHY an
// UNREACHABLE announcement is final for this process: the resolver already
// tried the display's own transport and the other local route (Rule 1b: one
// holder, referenced by the announcement and the test alike).
const workerUnreachableRoutesTried = "both local routes for that display number were tried and nothing answered"

// announceWorkerDisplay prints the worker's display verdict: one line, always,
// whatever the outcome. It never fails the start and never returns an error —
// the worker's contract is to run, and this line is evidence about a capability
// (not a precondition for the loop).
//
// The note is deliberately SPECIFIC about the consequence and the remedy: an
// operator who reads "UNREACHABLE" must not have to guess whether the worker is
// broken (it is not) or what to do (restart it from a shell whose DISPLAY is
// the durable local one).
//
// What the verdict MEASURES now includes the local fallback route (see
// internal/shot x11Routes): a display whose own transport is dead but whose
// number THIS host serves locally reports reachable, because every GUI tool the
// worker runs will reach it. UNREACHABLE therefore means "no X server on this
// host answers that display number", which is stronger and more useful than the
// old verdict ("the inherited DISPLAY name does not resolve").
func announceWorkerDisplay() {
	display, err := shot.ProbeDisplay("")
	if err != nil {
		fmt.Printf("[worker] %s: DISPLAY=%q UNREACHABLE: %v\n", workerDisplayLabel, display, err)
		fmt.Printf("[worker] %s: this worker inherits DISPLAY from its launcher and can NEVER re-acquire one — "+
			"every screen_capture/act it runs will fail (exit 1). %s, so no X server on this host serves it: "+
			"restart the worker from a shell whose DISPLAY is the durable LOCAL display (%s on this host), "+
			"not a dead SSH forward (%s)\n",
			workerDisplayLabel, workerUnreachableRoutesTried, workerLocalDisplay, workerForwardedDisplayShape)
		return
	}
	fmt.Printf("[worker] %s: DISPLAY=%q reachable (screen_capture/act will work from this process)\n", workerDisplayLabel, display)
}
