package tools

// api_reachability.go — the PRE-claim half of the transport-failure family: a
// bounded, in-process measurement of whether THIS NODE can reach the LLM API
// endpoint, taken BEFORE a row is claimed.
//
// THE DEFECT IT ANSWERS (measured 2026-10-29, HP): one row was claimed on a
// node whose connection to the API host was dead. The run spent 11m38s and three
// retries reaching tokens=0, then the row went `failed` — while a sibling node
// answered the same edge IPs in 150ms.
//
// THE POST-HOC MACHINERY THAT ANSWERS THE AFTERMATH WAS DELETED 2026-10-30
// (task 01a18ef7): task_transport_failure.go read the failure evidence and
// task_reroute.go minted a `-r1` successor — both rode the task-lifecycle pass,
// which had NO production caller, so neither ever ran outside its own tests and
// both went with it (task_lifecycle_tick.go carries the verdict). What survives
// post-hoc is `gopi task reopen` (the operator's `-r1` successor mint) and the
// row's own `failed` trail; this file is the PRE half that was never written,
// and it is still the missing measurement: it runs before a row is claimed.
//
// THE QUESTION HERE IS PRE, AND IT IS A DIFFERENT ONE. It answers "can this node
// reach the API at all" from no row at all — it shares the deleted reroute's
// vocabulary (the digest's `category=network`) but not its verdict ladder: there
// is no row here, no status transition, no successor. It
// adds no process, no timer, no script, no database and no config key (Rule 35) —
// it is one synchronous call on the claim path that already exists.
//
// WHY THE VERDICT IS THREE-VALUED (Rule 15a). "reachable" and "unreachable" are
// measurements; everything else is the absence of one — a probe that could not
// resolve an endpoint, could not be attempted, or was switched off by the
// operator. Collapsing that into "reachable" would be a silent pass (a dead node
// then claims and burns the budget, which is the defect); collapsing it into
// "unreachable" would park a healthy node for a measurement nobody took. So
// unknown carries a REASON and is its own verdict, and the caller's policy is
// stated once, at the caller: unknown does not refuse.
//
// WHY ONE FAILED DIAL IS NOT A VERDICT. A single connect timeout is a transient
// blip (a SYN dropped during a route change, a momentary conntrack eviction), and
// a node parked for one blip is a worse defect than the one this file fixes —
// measured on the same 2026-10-29 evidence: eleven other sessions on the failing
// host wrote 24-114 MB in the SAME window, so that host's link was intermittently
// fine. The verdict therefore comes from apiPreflightAttempts CONSECUTIVE bounded
// attempts inside ONE preflight: ANY success means the endpoint is reachable
// (a success is proof, a failure is only a failure), and only ALL of them failing
// is the machine-health claim that authorizes a refusal.
//
// THE THRESHOLD, AND WHERE IT LIVES. apiPreflightAttempts = 3, each bounded by
// apiPreflightConnectTimeout = 4s, with apiPreflightRetryBackoff = 250ms between
// them: a dead endpoint costs one preflight at most
// 3*4s + 2*250ms ≈ 12.5s — roughly 1/56th of the 11m38s it replaces — and a
// healthy one costs its first connect (measured 150ms). Every one of those three
// numbers is a named constant, and none is read from a knob.

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/deepseek"
)

const (
	// APIReachabilityPreflightEnv switches the claim-time preflight. It exists
	// because the probe is a REAL network call on the dispatch path, and an
	// operator (or a suite that drives the real binary end to end) must be able
	// to say "do not measure" without editing code or settings. Truthy spelling
	// rules are config.EnvBool's (Rule 2: one reader); an EMPTY value or any
	// disabled spelling leaves the preflight ON — the safe default is "measure",
	// since a silent skip is exactly the defect this file removes.
	APIReachabilityPreflightEnv = "GOPI_API_PREFLIGHT"

	// APIReachabilityEndpointEnv overrides the measured host:port (or a full
	// URL, whose host and port are taken). It is the seam that makes the gate
	// MEASURABLE without touching the operator's account: a test points it at a
	// closed port to produce a provably dead endpoint, and the preflight runs
	// unchanged. A settings.json key would be a new persistent knob for a value
	// only a test sets — this env var, read here and nowhere else, is the whole
	// channel.
	APIReachabilityEndpointEnv = "GOPI_API_PREFLIGHT_ENDPOINT"

	// apiPreflightDefaultPort is the port assumed when the configured base URL
	// names none. The endpoint gopi talks to is HTTPS, so the TCP measurement
	// targets 443; a base URL carrying an explicit port (a mock server, a
	// gateway) uses its own.
	apiPreflightDefaultPort = "443"

	// apiEnvAnthropicBaseURL / apiEnvDeepseekBaseURL are the client's own
	// endpoint overrides (internal/deepseek/claude_client.go resolves them ahead
	// of settings). They are named here ONLY to recognise the per-process
	// override shape described at apiReachabilityEndpoint — the resolution order
	// itself is never re-implemented.
	apiEnvAnthropicBaseURL = "ANTHROPIC_BASE_URL"
	apiEnvDeepseekBaseURL  = "DEEPSEEK_BASE_URL"

	// apiPreflightAttempts is the THRESHOLD the reissue of a verdict turns on:
	// this many CONSECUTIVE failed connects inside ONE preflight is the
	// machine-health claim; anything less is not a verdict at all. Three is
	// chosen against the measured shape (eleven sibling sessions alive in the
	// same window as the failure): a transient blip loses one or two SYNs, and a
	// genuinely dead path loses every one of them.
	apiPreflightAttempts = 3

	// apiPreflightConnectTimeout bounds ONE attempt. It is a TCP connect (not a
	// request and not a TLS handshake), so it is short by construction: the
	// measured healthy connect is 150ms, and a bound of four seconds is ~26x
	// that — generous enough for an intercontinental handshake, far below the
	// minutes a doomed run burns.
	apiPreflightConnectTimeout = 4 * time.Second

	// apiPreflightRetryBackoff spaces the consecutive attempts. Without it the
	// three attempts are one attempt at the same instant and the consecutiveness
	// the verdict rests on would be a fiction.
	apiPreflightRetryBackoff = 250 * time.Millisecond
)

// APIReachabilityVerdict is the three-valued answer. It is a closed enumeration:
// there is no fourth value meaning "probably".
type APIReachabilityVerdict string

const (
	// APIReachabilityReachable — at least one attempt connected. PROOF: the
	// kernel completed a TCP handshake with the endpoint's host.
	APIReachabilityReachable APIReachabilityVerdict = "reachable"

	// APIReachabilityUnreachable — every one of apiPreflightAttempts consecutive
	// bounded attempts failed. This is the only verdict that authorizes a claim
	// refusal.
	APIReachabilityUnreachable APIReachabilityVerdict = "unreachable"

	// APIReachabilityUnknown — no measurement was taken or none could be taken
	// (preflight switched off, no resolvable endpoint, endpoint not parseable).
	// Reason says which. It never refuses a claim: a node must stay able to run
	// local work when the probe cannot be performed.
	APIReachabilityUnknown APIReachabilityVerdict = "unknown"
)

// APIReachabilityProbe is ONE preflight's measurement — the values a caller
// prints, not a bool. Every field is what was observed: no field is inferred.
type APIReachabilityProbe struct {
	Verdict  APIReachabilityVerdict
	Endpoint string // host:port actually dialed ("" when none resolved)
	// Attempts holds the duration of every attempt made, in order, so a refusal
	// reports "3 attempts, 4.0s / 4.0s / 4.0s" rather than a bare "dead".
	Attempts []time.Duration
	// Failures holds each attempt's error text, parallel to Attempts (a
	// successful attempt contributes "").
	Failures []string
	// Reason explains an unknown verdict, and is empty for a measured one.
	Reason string
}

// AllowsClaim is the ONE policy decision this file makes about its own verdict:
// only a PROVEN-dead node refuses. unknown does not refuse — an unperformed
// measurement must never be able to park a healthy node (Rule 15a: silence is not
// a verdict).
func (p APIReachabilityProbe) AllowsClaim() bool {
	return p.Verdict != APIReachabilityUnreachable
}

// Evidence renders the measurement as the sentence an operator reads next to a
// refusal or in a worker log. It names the endpoint, every attempt's duration and
// error, and the remedy — a bare "cannot reach the API" is the shape Rule 15
// bans. Empty-attempt cases (unknown) render their reason instead.
func (p APIReachabilityProbe) Evidence() string {
	switch p.Verdict {
	case APIReachabilityReachable:
		if len(p.Attempts) == 0 {
			return fmt.Sprintf("api reachability: %s reachable (no attempt recorded — internal inconsistency)", p.Endpoint)
		}
		return fmt.Sprintf("api reachability: %s reachable (connected in %s)",
			p.Endpoint, p.Attempts[len(p.Attempts)-1].Round(time.Millisecond))
	case APIReachabilityUnreachable:
		parts := make([]string, 0, len(p.Attempts))
		for i, d := range p.Attempts {
			msg := ""
			if i < len(p.Failures) {
				msg = p.Failures[i]
			}
			parts = append(parts, fmt.Sprintf("attempt %d: failed after %s (%s)",
				i+1, d.Round(time.Millisecond), msg))
		}
		return fmt.Sprintf("api reachability: %s UNREACHABLE — %d/%d consecutive bounded attempts failed [%s] — remedy: `ping %s` / `curl -sv https://%s`; a proxy or firewall in this node's path is the usual cause, and nothing about the queued work is at fault",
			p.Endpoint, len(p.Attempts), apiPreflightAttempts, strings.Join(parts, "; "),
			p.host(), p.host())
	default:
		reason := p.Reason
		if reason == "" {
			reason = "no reason recorded"
		}
		if p.Endpoint == "" {
			return fmt.Sprintf("api reachability: unknown (%s)", reason)
		}
		return fmt.Sprintf("api reachability: %s unknown (%s)", p.Endpoint, reason)
	}
}

// host answers the endpoint's host part without the port, for the remedy text
// (a reader pastes `ping <host>`, not `ping <host>:443`).
func (p APIReachabilityProbe) host() string {
	if p.Endpoint == "" {
		return "the configured api host"
	}
	if h, _, err := net.SplitHostPort(p.Endpoint); err == nil {
		return h
	}
	return p.Endpoint
}

// APIReachabilityPreflight performs the measurement. It is SYNCHRONOUS and
// BOUNDED: apiPreflightAttempts dials, each bounded by
// apiPreflightConnectTimeout, spaced by apiPreflightRetryBackoff, with no
// goroutine left behind and no state written anywhere.
//
// What it measures is deliberately the NARROWEST fact that could have saved the
// measured 11 minutes: whether a TCP handshake to the API host's port completes.
// It does not send a request, does not authenticate, and does not judge the
// account — an API-level refusal (402 balance, 429 concurrency) is a fact about
// the work and is a question about the account, not the link — the division
// that keeps this file from becoming a status-code ladder. (The file that
// carried that ladder, task_transport_failure.go, was deleted 2026-10-30 with
// the reroute leg it served — task 01a18ef7, task_lifecycle_tick.go's header.)
func APIReachabilityPreflight() APIReachabilityProbe {
	return apiReachabilityPreflightWith(apiReachabilityDialContext)
}

// apiReachabilityDialer is the seam the tests use to produce a MEASURED verdict
// without depending on any network at all: the real preflight dials TCP, a test
// substitutes a dialer whose every call fails or whose first call fails and the
// rest succeed (the transient-blip arm). It is a package-level variable for the
// same reason the process-signal primitives are (Rule 3: no new tool, no new process).
var apiReachabilityDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

// apiReachabilityPreflightWith runs the attempt ladder through the given dialer.
func apiReachabilityPreflightWith(dial func(ctx context.Context, network, addr string) (net.Conn, error)) APIReachabilityProbe {
	if !apiReachabilityEnabled() {
		return APIReachabilityProbe{
			Verdict: APIReachabilityUnknown,
			Reason:  fmt.Sprintf("%s is off for this process — the preflight did not run", APIReachabilityPreflightEnv),
		}
	}
	endpoint, reason := apiReachabilityEndpoint()
	if endpoint == "" {
		return APIReachabilityProbe{Verdict: APIReachabilityUnknown, Reason: reason}
	}

	probe := APIReachabilityProbe{Endpoint: endpoint}
	for attempt := 0; attempt < apiPreflightAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(apiPreflightRetryBackoff)
		}
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), apiPreflightConnectTimeout)
		conn, err := dial(ctx, "tcp", endpoint)
		elapsed := time.Since(start)
		cancel()
		probe.Attempts = append(probe.Attempts, elapsed)
		if err == nil {
			// A success is PROOF of reachability and ends the ladder: earlier
			// failures are the transient blip this ladder exists to absorb.
			//stylegate:ignore the probe socket exists only to be released — a close error on it says nothing about reachability, which the handshake already proved
			_ = conn.Close()
			probe.Failures = append(probe.Failures, "")
			probe.Verdict = APIReachabilityReachable
			return probe
		}
		probe.Failures = append(probe.Failures, err.Error())
	}
	probe.Verdict = APIReachabilityUnreachable
	return probe
}

// apiReachabilityHostShaped reports whether s can be a bare host: letters,
// digits, dots, hyphens and colons only (a DNS name, an IPv4 literal or a
// bracketed IPv6 address). Anything else — a space, a slash, a quote — is not an
// endpoint and must not be dialed as one.
func apiReachabilityHostShaped(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == ':':
		default:
			return false
		}
	}
	return true
}

// apiReachabilityEnabled reports whether the preflight may run. An unset or empty
// value means ON (the safe default: a silent skip is the defect); otherwise the
// value goes through config.EnvBool, the single boolean vocabulary (Rule 2).
// envNames_ANTHROPIC_BASE_URL / envNames_DEEPSEEK_BASE_URL are the readers of
// the two override names (Rule 21: a name constant with no reader is a knob
// nothing consumes).
func envNames_ANTHROPIC_BASE_URL() string {
	return strings.TrimSpace(os.Getenv(apiEnvAnthropicBaseURL))
}
func envNames_DEEPSEEK_BASE_URL() string { return strings.TrimSpace(os.Getenv(apiEnvDeepseekBaseURL)) }

func apiReachabilityEnabled() bool {
	raw := strings.TrimSpace(os.Getenv(APIReachabilityPreflightEnv))
	if raw == "" {
		return true
	}
	return config.EnvBool(APIReachabilityPreflightEnv)
}

// apiReachabilityEndpoint resolves the host:port to measure, or "" plus the
// reason it could not. Order: the test/caller override (a host:port or a URL),
// then the API client's OWN base-URL resolution — deepseek.ResolveClaudeBaseURL,
// so the probed host is by construction the host the client would use
// (ANTHROPIC_BASE_URL > DEEPSEEK_BASE_URL > settings base_url > default). A
// second copy of that precedence here would be a probe that can measure a
// different host than the one it is vouching for.
func apiReachabilityEndpoint() (string, string) {
	raw := strings.TrimSpace(os.Getenv(APIReachabilityEndpointEnv))
	source := APIReachabilityEndpointEnv
	// A PER-PROCESS ENV OVERRIDE NAMES A HARNESS'S ENDPOINT, NOT THIS NODE'S
	// PATH TO ITS API. ANTHROPIC_BASE_URL / DEEPSEEK_BASE_URL are how a test
	// harness, a mock server or an operator's one-shot run redirects the client
	// — and the thing at the other end is that harness's responsibility, not
	// this node's connectivity. Measuring it would let a fixture (a mock URL
	// with no listener) park a perfectly healthy node, which is the false
	// positive this design exists to avoid, and it would judge the harness
	// instead of the machine. Measured: the suite's own worker fixtures
	// (cli_tasks_worker_renew_test.go, cli_tasks_worker_writeback_test.go) went
	// red on this gate's first landed version for exactly that reason.
	// The operator's REAL configuration (settings base_url, then the built-in
	// default) is what gets measured — and GOPI_API_PREFLIGHT_ENDPOINT, the
	// explicit channel, overrides everything and IS measured.
	if raw == "" && (envNames_ANTHROPIC_BASE_URL() != "" || envNames_DEEPSEEK_BASE_URL() != "") {
		return "", fmt.Sprintf("the endpoint came from %s/%s, a per-process override (a harness or a one-shot redirect) — this node's own path to the configured API is what the preflight measures, and it does not judge a harness's endpoint",
			apiEnvAnthropicBaseURL, apiEnvDeepseekBaseURL)
	}
	if raw == "" {
		cfg, err := deepseek.LoadConfig(deepseek.SettingsPath())
		if err != nil {
			return "", fmt.Sprintf("settings could not be read (%v)", err)
		}
		raw = deepseek.ResolveClaudeBaseURL(cfg)
		source = "the configured base URL"
	}
	if strings.TrimSpace(raw) == "" {
		return "", "no API endpoint is configured (no base_url, no ANTHROPIC_BASE_URL)"
	}
	// Accept BOTH shapes: a URL (the production spelling, from base_url /
	// ANTHROPIC_BASE_URL) and a bare host[:port] (the shape an operator or a
	// test types by hand). WHICH FORM IS PRESENT IS DECIDED BY THE SEPARATOR, not
	// by trying one parser first: `net.SplitHostPort` accepts
	// "https://api.deepseek.com/anthropic" as host "https" + port
	// "//api.deepseek.com/anthropic" (measured on this very gate's first live
	// run: a HEALTHY node was reported UNREACHABLE with
	// `lookup tcp///api.deepseek.com/anthropic: unknown port` — the false
	// positive this whole design exists to avoid, produced by the resolver
	// rather than by the network). So a value carrying a scheme is a URL, and one
	// that does not is host[:port].
	if !strings.Contains(raw, "://") {
		if h, port, err := net.SplitHostPort(raw); err == nil && h != "" {
			if port == "" {
				port = apiPreflightDefaultPort
			}
			return net.JoinHostPort(h, port), ""
		}
		// A bare host with no port at all ("api.deepseek.com"). IT MUST LOOK
		// LIKE ONE: an arbitrary string is not an endpoint, and treating it as
		// one would turn "I cannot tell what this is" into a MEASURED
		// `unreachable` — the false positive that parks a node because the
		// resolver, not the network, failed (measured on this file's own test:
		// "not a url at all" became a dial target named `not a url at all:443`).
		host := strings.Trim(raw, "[]")
		if !apiReachabilityHostShaped(host) {
			return "", fmt.Sprintf("%s (%q) is neither a URL nor a host[:port]", source, raw)
		}
		return net.JoinHostPort(host, apiPreflightDefaultPort), ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Sprintf("%s (%q) is not a parseable endpoint (want a base URL or host[:port])", source, raw)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Sprintf("%s (%q) carries no host", source, raw)
	}
	port := u.Port()
	if port == "" {
		port = apiPreflightDefaultPort
	}
	return net.JoinHostPort(host, port), ""
}
