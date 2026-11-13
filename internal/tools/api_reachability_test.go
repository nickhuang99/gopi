package tools

// api_reachability_test.go — the MEASUREMENT half of the pre-claim reachability
// gate (epic api-preflight-claim-gate). The caller's decision is asserted in
// cmd/agent; what is asserted here is the ladder that decision rests on.
//
// THE LADDER'S WHOLE VALUE IS "ONE FAILURE IS NOT A VERDICT": a single connect
// failure must not park a node (measured 2026-10-29: eleven sibling sessions on
// the "dead" host wrote 24-114 MB in the same window), so `unreachable` requires
// apiPreflightAttempts CONSECUTIVE failures inside ONE preflight, and a single
// success anywhere in the ladder is proof of reachability.
//
// The dialer is INJECTED (apiReachabilityDialContext) so these arms cost nothing
// and touch no network: Rule 27 forbids generating load to satisfy a test, and a
// fabricated dialer measures the ladder, which is what this file is about. The
// REAL connect is measured end to end by cmd/agent's gate tests (a closed local
// port and a listening one), and by the run's own recorded verification.

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// apiReachTestDialer installs a dialer for the duration of one test and pins the
// endpoint the preflight resolves, so neither the operator's settings file nor
// the ambient environment can influence a measurement.
func apiReachTestDialer(t *testing.T, dial func(ctx context.Context, network, addr string) (net.Conn, error)) {
	t.Helper()
	prev := apiReachabilityDialContext
	apiReachabilityDialContext = dial
	t.Cleanup(func() { apiReachabilityDialContext = prev })
	t.Setenv(APIReachabilityPreflightEnv, "1")
	t.Setenv(APIReachabilityEndpointEnv, "https://api.invalid.example")
}

// apiReachFakeConn is the success arm's connection: the preflight closes it and
// reads nothing else.
type apiReachFakeConn struct{}

func (apiReachFakeConn) Read([]byte) (int, error)         { return 0, fmt.Errorf("closed") }
func (apiReachFakeConn) Write([]byte) (int, error)        { return 0, fmt.Errorf("closed") }
func (apiReachFakeConn) Close() error                     { return nil }
func (apiReachFakeConn) LocalAddr() net.Addr              { return apiReachFakeAddr{} }
func (apiReachFakeConn) RemoteAddr() net.Addr             { return apiReachFakeAddr{} }
func (apiReachFakeConn) SetDeadline(time.Time) error      { return nil }
func (apiReachFakeConn) SetReadDeadline(time.Time) error  { return nil }
func (apiReachFakeConn) SetWriteDeadline(time.Time) error { return nil }

type apiReachFakeAddr struct{}

func (apiReachFakeAddr) Network() string { return "tcp" }
func (apiReachFakeAddr) String() string  { return "fixture:0" }

// TestAPIReachabilityConsecutiveFailuresAreUnreachable: every attempt failing is
// the ONLY shape that produces `unreachable`, and the ladder must actually run
// apiPreflightAttempts of them (a ladder that gave up after one would be the
// false-positive machine this design refuses).
func TestAPIReachabilityConsecutiveFailuresAreUnreachable(t *testing.T) {
	calls := 0
	apiReachTestDialer(t, func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, fmt.Errorf("dial tcp: i/o timeout")
	})
	probe := APIReachabilityPreflight()
	if probe.Verdict != APIReachabilityUnreachable {
		t.Fatalf("verdict = %q with every attempt failing, want %q (%s)",
			probe.Verdict, APIReachabilityUnreachable, probe.Evidence())
	}
	if calls != apiPreflightAttempts {
		t.Errorf("the ladder made %d attempt(s), want apiPreflightAttempts=%d — a verdict built from fewer is one dial, not a consecution",
			calls, apiPreflightAttempts)
	}
	if len(probe.Attempts) != apiPreflightAttempts || len(probe.Failures) != apiPreflightAttempts {
		t.Errorf("recorded %d duration(s) / %d error(s), want %d of each (a refusal must report its evidence)",
			len(probe.Attempts), len(probe.Failures), apiPreflightAttempts)
	}
	if probe.AllowsClaim() {
		t.Error("AllowsClaim() = true on a PROVEN-dead endpoint — the only verdict that may park a node is unreachable, and it just did not")
	}
	ev := probe.Evidence()
	if !strings.Contains(ev, "UNREACHABLE") || !strings.Contains(ev, "remedy:") {
		t.Errorf("evidence does not carry the verdict and the remedy: %q", ev)
	}
	if !strings.Contains(ev, fmt.Sprintf("%d/%d", apiPreflightAttempts, apiPreflightAttempts)) {
		t.Errorf("evidence does not report the attempt tally: %q", ev)
	}
}

// TestAPIReachabilityOneSuccessIsProof is the TRANSIENT BLIP arm: the first
// attempt fails, the second succeeds — the node is reachable and the ladder must
// say so. This is the arm that keeps a single dropped SYN from parking a floor.
func TestAPIReachabilityOneSuccessIsProof(t *testing.T) {
	calls := 0
	apiReachTestDialer(t, func(context.Context, string, string) (net.Conn, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("dial tcp: i/o timeout")
		}
		return apiReachFakeConn{}, nil
	})
	probe := APIReachabilityPreflight()
	if probe.Verdict != APIReachabilityReachable {
		t.Fatalf("verdict = %q after one failed attempt and one success, want %q — a single dial failure must never park a node",
			probe.Verdict, APIReachabilityReachable)
	}
	if !probe.AllowsClaim() {
		t.Error("AllowsClaim() = false on a measured-reachable endpoint")
	}
	if calls != 2 {
		t.Errorf("the ladder made %d attempt(s), want 2 (the ladder must stop at the first success)", calls)
	}
	if ev := probe.Evidence(); !strings.Contains(ev, "reachable") {
		t.Errorf("evidence does not say reachable: %q", ev)
	}
}

// TestAPIReachabilityUnknownIsItsOwnVerdict: a measurement that CANNOT be taken
// is `unknown` with a reason — never a silent pass (which would claim and burn
// the budget) and never `unreachable` (which would park a healthy node). Two
// shapes reach it: the operator's kill switch, and an endpoint that does not
// resolve to a host:port.
func TestAPIReachabilityUnknownIsItsOwnVerdict(t *testing.T) {
	t.Run("kill switch", func(t *testing.T) {
		apiReachTestDialer(t, func(context.Context, string, string) (net.Conn, error) {
			t.Error("the preflight dialed with the kill switch off")
			return nil, fmt.Errorf("must not dial")
		})
		t.Setenv(APIReachabilityPreflightEnv, "0")
		probe := APIReachabilityPreflight()
		if probe.Verdict != APIReachabilityUnknown {
			t.Fatalf("verdict = %q with %s=0, want %q", probe.Verdict, APIReachabilityPreflightEnv, APIReachabilityUnknown)
		}
		if probe.Reason == "" {
			t.Error("an unknown verdict carried no reason — Rule 15a: the absence of a measurement must say so")
		}
		if !probe.AllowsClaim() {
			t.Error("an unmeasured endpoint refused a claim — an unperformed probe must never park a node")
		}
	})

	t.Run("unparseable endpoint", func(t *testing.T) {
		apiReachTestDialer(t, func(context.Context, string, string) (net.Conn, error) {
			t.Error("the preflight dialed an endpoint it could not parse")
			return nil, fmt.Errorf("must not dial")
		})
		t.Setenv(APIReachabilityEndpointEnv, "not a url at all")
		probe := APIReachabilityPreflight()
		if probe.Verdict != APIReachabilityUnknown {
			t.Fatalf("verdict = %q for an unparseable endpoint, want %q", probe.Verdict, APIReachabilityUnknown)
		}
		if probe.Reason == "" || probe.Endpoint != "" {
			t.Errorf("unknown verdict carried reason=%q endpoint=%q, want a reason and no endpoint", probe.Reason, probe.Endpoint)
		}
	})
}

// TestAPIReachabilityEndpointResolution: the measured host:port comes from ONE
// source — the API client's own base-URL resolution (or the test override) — and
// the port defaults to 443 when the URL names none. A probe that measured a
// different host than the client uses would vouch for a path nothing takes.
func TestAPIReachabilityEndpointResolution(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want string
	}{
		{"base url without a port", "https://api.deepseek.com/anthropic", "api.deepseek.com:443"},
		{"explicit port is kept", "http://127.0.0.1:18080/v1/messages", "127.0.0.1:18080"},
		{"bare host with scheme", "http://fixture.invalid", "fixture.invalid:443"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(APIReachabilityEndpointEnv, tc.env)
			got, reason := apiReachabilityEndpoint()
			if got != tc.want {
				t.Errorf("apiReachabilityEndpoint(%q) = %q (reason %q), want %q", tc.env, got, reason, tc.want)
			}
		})
	}
}

// TestAPIReachabilityThresholdsAreNamed pins Rule 1/13 for this file: the
// attempt count and the per-attempt bound exist as constants and are REFERENCES,
// not literals in the ladder. A bare 3 or 4*time.Second inside the loop would be
// the defect (a value nothing can point at when it needs to change).
func TestAPIReachabilityThresholdsAreNamed(t *testing.T) {
	if apiPreflightAttempts < 2 {
		t.Fatalf("apiPreflightAttempts = %d — a ladder of one attempt cannot tell a blip from a dead node", apiPreflightAttempts)
	}
	if apiPreflightConnectTimeout <= 0 || apiPreflightRetryBackoff <= 0 {
		t.Fatalf("bounds must be positive: timeout=%s backoff=%s", apiPreflightConnectTimeout, apiPreflightRetryBackoff)
	}
	// The worst case a dead endpoint costs ONE preflight — the number the epic's
	// 11m38s complaint is measured against.
	worst := time.Duration(apiPreflightAttempts)*apiPreflightConnectTimeout +
		time.Duration(apiPreflightAttempts-1)*apiPreflightRetryBackoff
	if worst > time.Minute {
		t.Errorf("one preflight's worst case is %s — that is no longer a bounded pre-claim cost", worst)
	}
}
