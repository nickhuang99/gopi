package main

// worker_claim_api_gate_test.go — the PRE-claim reachability gate's regression
// suite (epic api-preflight-claim-gate).
//
// THE ARMS, because each refuses a different thing:
//
//	① THE DECISION, MEASURED ON THE LEDGER — a PROVABLY DEAD endpoint (a closed
//	  local port, a real TCP connect that really fails, no network of consequence
//	  and no generated load) makes workerRoundClaiming() refuse: the row stays
//	  `pending`. The positive control does the same setup against a LISTENING
//	  local socket and the row leaves `pending`, so a gate that refused
//	  unconditionally could not look like a pass.
//	② THE UNKNOWN ARM — an endpoint that cannot be measured (a base URL that does
//	  not parse) yields `unknown` and does NOT refuse: a node must keep running
//	  while the probe cannot be taken (Rule 15a).
//	③ THE MEASUREMENT ITSELF (internal/tools) — the ladder: all attempts failing
//	  is `unreachable`; one success inside the ladder is `reachable` (the
//	  transient blip); the kill switch and the unset-endpoint cases are
//	  `unknown` with a reason, never a silent pass.
//
// THE RED-BEFORE is recorded in the run's report: with the gate's call removed
// from workerRoundClaiming, arm ①'s refusal assertion fails because the row is
// claimed — which is exactly the 11m38s defect (2026-10-29, HP).
//
// WHY THE FIXTURE PORT IS A REAL SOCKET AND NOT A FABRICATED BOOL: the epic's
// acceptance asks for a measurement, and the preflight's whole value is that it
// talks to the kernel. A closed port on 127.0.0.1 fails a connect for the same
// reason a dead route does (the SYN goes nowhere), at zero cost and with no
// dependency on any network (Rule 27 forbids generating load; a connect attempt
// is not load).

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tools"
)

// workerClaimApiGateTestSetup pins the sandbox a claiming round needs: its own
// ledger, its own gopi data root, and a synthetic child so no model call is made.
// It returns the control dir the round's context needs.
func workerClaimApiGateTestSetup(t *testing.T) string {
	t.Helper()
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	dir := t.TempDir()
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv(config.GopiDirEnv, t.TempDir())
	// The child is synthetic (taskChildFailTestEnv): no model call, no API key.
	// The round fails its run and releases the claim — what is asserted here is
	// the CLAIM, which is the only thing the gate touches.
	t.Setenv(taskChildFailTestEnv, "1")
	prevCtx := workerCtx
	t.Cleanup(func() { workerCtx = prevCtx })
	workerCtx = drainTestCtx(t, dir)
	return dir
}

// apiGateClosedPort opens a listener, notes its port, and CLOSES it — the port
// is then provably closed on this host, so a connect attempt really fails.
func apiGateClosedPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close the probe listener: %v", err)
	}
	return addr
}

// apiGateLivePort opens a listener that STAYS OPEN for the test — a genuinely
// reachable endpoint, so the positive control measures a real handshake.
func apiGateLivePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() }) //stylegate:ignore the fixture's own listener; the test's cleanup closes it once
	// Accept in the background so a connect completes rather than sitting in the
	// backlog: the preflight only needs the handshake, and a parked accept is
	// what keeps the backlog from filling under the ladder's three attempts.
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close() //stylegate:ignore the fixture releases the accepted probe socket immediately
		}
	}()
	return ln.Addr().String()
}

// TestWorkerClaimRefusedWhenApiEndpointIsDead is arm ①'s REFUSAL: with the
// preflight measuring a provably closed endpoint, the worker must NOT claim —
// the row stays `pending`, and the refusal is visible to the loop (the flag the
// loop turns into its own line).
func TestWorkerClaimRefusedWhenApiEndpointIsDead(t *testing.T) {
	workerClaimApiGateTestSetup(t)
	t.Setenv(tools.APIReachabilityPreflightEnv, "1")
	t.Setenv(tools.APIReachabilityEndpointEnv, apiGateClosedPort(t))
	drainSeedPendingRow(t, "api-gate-dead-1")
	//stylegate:ignore the gate's own refusal is what makes ran==false here; the ledger row's status is the assertion
	if ran, _ := workerRoundClaiming(); ran {
		t.Fatal("the worker claimed a row on a provably unreachable API endpoint — that is the 11m38s defect")
	}
	if status, err := tools.TaskStatusOf("api-gate-dead-1"); err != nil {
		t.Fatalf("read row status: %v", err)
	} else if status != tools.TaskStatusPending {
		t.Errorf("row status = %q after the refused claim, want %q (nothing may be claimed on a transport-dead node)",
			status, tools.TaskStatusPending)
	}
	if !workerApiGateConsumeRefusal() {
		t.Fatal("the round did not publish its refusal to the loop — a loop that cannot tell this apart from a drained queue counts a dead node toward its idle exit and reports 'queue drained' while work waits")
	}
}

// TestWorkerClaimsWhenApiEndpointIsLive is arm ①'s POSITIVE CONTROL: the very
// same setup against a listening socket claims the row. Without it a gate that
// refused everything would read as a pass (the refusal must be CAUSED by the
// measurement, not by the new code).
func TestWorkerClaimsWhenApiEndpointIsLive(t *testing.T) {
	workerClaimApiGateTestSetup(t)
	t.Setenv(tools.APIReachabilityPreflightEnv, "1")
	t.Setenv(tools.APIReachabilityEndpointEnv, apiGateLivePort(t))
	drainSeedPendingRow(t, "api-gate-live-1")
	//stylegate:ignore the round runs the row and releases the claim; the ledger status leaving `pending` is the assertion
	_, _ = workerRoundClaiming()
	status, err := tools.TaskStatusOf("api-gate-live-1")
	if err != nil {
		t.Fatalf("read row status: %v", err)
	}
	if status == tools.TaskStatusPending {
		t.Errorf("row stayed %q on a REACHABLE endpoint — the gate refuses a healthy node (the false positive Rule 35's whole design fights)", status)
	}
}

// TestWorkerClaimsWhenApiEndpointCannotBeMeasured is arm ②: an endpoint that
// cannot even be resolved to a host:port is `unknown`, and unknown NEVER refuses
// — the row is claimed. This is the arm that keeps a node able to run local work
// when the probe cannot be performed.
func TestWorkerClaimsWhenApiEndpointCannotBeMeasured(t *testing.T) {
	workerClaimApiGateTestSetup(t)
	t.Setenv(tools.APIReachabilityPreflightEnv, "1")
	t.Setenv(tools.APIReachabilityEndpointEnv, "not a url at all")
	drainSeedPendingRow(t, "api-gate-unknown-1")
	//stylegate:ignore the round runs the row and releases the claim; the ledger status leaving `pending` is the assertion
	_, _ = workerRoundClaiming()
	status, err := tools.TaskStatusOf("api-gate-unknown-1")
	if err != nil {
		t.Fatalf("read row status: %v", err)
	}
	if status == tools.TaskStatusPending {
		t.Errorf("row stayed %q when the endpoint could not be MEASURED — an unperformed measurement must never park a node", status)
	}
	if workerApiGateConsumeRefusal() {
		t.Error("an unknown verdict was published as a refusal — that is the silent-park defect Rule 15a forbids")
	}
}

// TestWorkerClaimApiGatePolicy pins the one decision workerClaimApiGate makes,
// as a table, so the three-valued policy is asserted without a ledger: only
// `unreachable` refuses.
func TestWorkerClaimApiGatePolicy(t *testing.T) {
	cases := []struct {
		name  string
		probe tools.APIReachabilityProbe
		want  bool
	}{
		{"reachable", tools.APIReachabilityProbe{Verdict: tools.APIReachabilityReachable, Endpoint: "h:443"}, true},
		{"unreachable", tools.APIReachabilityProbe{
			Verdict:  tools.APIReachabilityUnreachable,
			Endpoint: "h:443",
			// The measured shape: three attempts, each bounded and each failing.
			// The evidence line renders this tally, and a refusal with an empty
			// tally would be a claim about a measurement nobody took.
			Attempts: []time.Duration{time.Second, time.Second, time.Second},
			Failures: []string{"i/o timeout", "i/o timeout", "i/o timeout"},
		}, false},
		{"unknown", tools.APIReachabilityProbe{Verdict: tools.APIReachabilityUnknown, Reason: "off"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := workerClaimApiGate(func() tools.APIReachabilityProbe { return tc.probe })
			if got != tc.want {
				t.Errorf("workerClaimApiGate(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestClaimApiGateRunsBeforeTheClaimScan is arm ③: the gate is PRE-claim by
// construction, which is a property of the SOURCE ORDER in the claiming round —
// a gate that ran after the scan would be post-hoc and could not save a minute.
func TestClaimApiGateRunsBeforeTheClaimScan(t *testing.T) {
	src, err := os.ReadFile("cli_tasks_worker.go")
	if err != nil {
		t.Fatalf("read cli_tasks_worker.go: %v", err)
	}
	body := funcBody(t, string(src), "func workerRoundClaiming(")
	gate := strings.Index(body, "workerClaimApiGateDefault()")
	claim := strings.Index(body, "tools.TaskNextFiltered(")
	if gate < 0 {
		t.Fatalf("workerRoundClaiming no longer calls workerClaimApiGateDefault() — the pre-claim gate is gone:\n%s", body)
	}
	if claim < 0 {
		t.Fatalf("workerRoundClaiming lost its claim scan:\n%s", body)
	}
	if gate > claim {
		t.Errorf("the API gate is called AFTER the claim scan — that is the post-hoc shape this epic exists to remove:\n%s", body)
	}
}

// TestApiGateRefusalHasItsOwnExitCode is epic deliverable 3's machine check: a
// PRE-claim reachability refusal must be TELLABLE APART from every other outcome
// of `gopi worker`. A drained queue exits 0, a failed named row 1, a bad argv 2
// and a forbidden launch 3 — a transport-dead node gets 4 and nothing else, so a
// scheduler reading the code learns "this node cannot reach the API, nothing was
// claimed" instead of "the queue is empty".
func TestApiGateRefusalHasItsOwnExitCode(t *testing.T) {
	already := map[int]string{
		workerExplicitExitOK:             "a named row completed",
		workerExplicitExitFailed:         "a named row failed",
		workerUnexpectedArgExitCode:      "an argument this verb does not accept",
		config.AgentGuardRefusalExitCode: "the agent-spawn guard refused the launch",
	}
	if why, clash := already[workerApiGateRefusedExitCode]; clash {
		t.Fatalf("workerApiGateRefusedExitCode = %d already means %q — a refusal that shares a code is not distinguishable from the outcome it shares it with",
			workerApiGateRefusedExitCode, why)
	}
	// The flag is the transport from the loop to the entry point; both arms and
	// the cleared state must be asserted, or a process could exit 4 having
	// refused nothing.
	prev := workerApiGateRefusedExit
	t.Cleanup(func() { workerApiGateRefusedExit = prev })
	workerApiGateRefusedExit = false
	if got := workerApiGateRefusedExitCodeFor(); got != workerExplicitExitOK {
		t.Errorf("with no refusal recorded the exit code is %d, want %d", got, workerExplicitExitOK)
	}
	workerApiGateRecordRefusedExit()
	if got := workerApiGateRefusedExitCodeFor(); got != workerApiGateRefusedExitCode {
		t.Errorf("after a refusal the exit code is %d, want %d", got, workerApiGateRefusedExitCode)
	}
}
