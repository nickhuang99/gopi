package hippocampus

// Host-binding tests (epic: hippocampus host binding).
//
// host.go is the package's PRODUCTION binding — NewHostEngine + ClientAdapter +
// the two host floors — and internal/agent/hippocampus_binding_test.go owns the
// LIVE path that calls it. These tests own the binding's own contract.
//
// Division of labour with cov_hippocampus_test.go (which already pins the base
// ClientAdapter contract: retry-once-on-JSON-less with the default budget,
// Retries<0 disabled, transport-error propagation, unbound refusal): this file
// adds what is NOT covered there — the binary search on the FLOORS through the
// exported Config() accessor (both directions: raised when tiny, preserved when
// larger), the copy semantics of Config(), an explicit positive retry budget,
// and cancellation stopping the retry loop.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// hostTestClient is a scripted HostClient double: canned replies in order (the
// last one repeats), a recorded call count, and an optional hard error. No
// network, fully deterministic.
type hostTestClient struct {
	replies []string
	err     error
	calls   int
}

func (c *hostTestClient) Chat(ctx context.Context, system, user string) (string, error) {
	idx := c.calls
	c.calls++
	if c.err != nil {
		return "", c.err
	}
	if len(c.replies) == 0 {
		return "", nil
	}
	if idx < len(c.replies) {
		return c.replies[idx], nil
	}
	return c.replies[len(c.replies)-1], nil
}

// hostTestJSONReply / hostTestProseReply are the two reply shapes the adapter
// must tell apart: a usable reply and one with no JSON object at all.
const (
	hostTestJSONReply  = `{"queries":["host adapter retry"]}`
	hostTestProseReply = `I would search the knowledge base for lessons about this.`
)

// ---------------------------------------------------------------------------
// The nil guard — an unbound organ is a recorded failure (Rule 15)
// ---------------------------------------------------------------------------

// TestNewHostEngineNilClientStaysUnbound: ok=false is the contract the live
// binder branches on, so it is asserted here against a nil HostClient.
//
// Scope note: a TYPED nil (an interface holding a nil *T) is deliberately not
// covered — a non-nil interface cannot be told apart from a bound transport
// without reflection, and no caller can produce one (the live binder always
// allocates its transport). The devdoc records this as a known limitation
// rather than pretending the guard is total.
func TestNewHostEngineNilClientStaysUnbound(t *testing.T) {
	engine, ok := NewHostEngine(nil)
	if ok || engine != nil {
		t.Fatalf("NewHostEngine(nil) = (engine=%v, ok=%v), want (nil, false)", engine, ok)
	}
	if memory := engine; memory != nil {
		t.Errorf("engine = %v, want nil so the caller cannot Run a half-built organ", memory)
	}
}

// TestNewHostEngineSurfacesTransportFailureThroughTheCycle: NewHostEngine's
// ok=true means "the organ is bound" — NOT "the organ works". A failing
// transport must reach the caller as a RECORDED cycle failure, never as a
// silent success.
func TestNewHostEngineSurfacesTransportFailureThroughTheCycle(t *testing.T) {
	setMemoryDB(t)
	engine, ok := NewHostEngine(&hostTestClient{err: errors.New("hosttest transport down")})
	if !ok || engine == nil {
		t.Fatal("NewHostEngine reported unbound for a non-nil client")
	}
	res, err := engine.Run(context.Background(), Request{Context: "hosttest unbound transport"})
	if err != nil {
		t.Fatalf("Run returned a caller-side error: %v", err)
	}
	if res.StopReason != StopClientError || res.Err == nil ||
		!strings.Contains(res.Err.Error(), "hosttest transport down") {
		t.Errorf("transport failure not recorded: stop=%q err=%v", res.StopReason, res.Err)
	}
}

// ---------------------------------------------------------------------------
// The floors ARE the engine's config (single source of truth, on the live path)
// ---------------------------------------------------------------------------

// TestNewHostEngineFloorsReachTheEngineConfig: the two floors are the value the
// engine actually runs under — read through the exported Config() accessor,
// which is the only way a caller (the live binder's test, an audit) can see
// them at all.
func TestNewHostEngineFloorsReachTheEngineConfig(t *testing.T) {
	setMemoryDB(t)

	t.Run("tiny configured knobs are raised to the floors", func(t *testing.T) {
		t.Setenv(envClientTimeout, "1")
		t.Setenv(envTimeoutSec, "1")
		engine, ok := NewHostEngine(&hostTestClient{})
		if !ok || engine == nil {
			t.Fatal("NewHostEngine reported unbound")
		}
		cfg := engine.Config()
		if cfg.ClientTimeout != HostEngineClientTimeout {
			t.Errorf("ClientTimeout = %v, want the host floor %v", cfg.ClientTimeout, HostEngineClientTimeout)
		}
		if cfg.Timeout != HostEngineCycleTimeout {
			t.Errorf("Timeout = %v, want the host floor %v", cfg.Timeout, HostEngineCycleTimeout)
		}
	})

	t.Run("a larger configured knob is preserved", func(t *testing.T) {
		// Only the client knob is overridden: the cycle knob stays at its
		// default, so this subtest isolates ONE direction (the floors must never
		// lower an operator's deliberate setting).
		sec := int(HostEngineClientTimeout/time.Second) + 60
		t.Setenv(envClientTimeout, intToString(sec))
		engine, ok := NewHostEngine(&hostTestClient{})
		if !ok || engine == nil {
			t.Fatal("NewHostEngine reported unbound")
		}
		if got := engine.Config().ClientTimeout; got != time.Duration(sec)*time.Second {
			t.Errorf("ClientTimeout = %v, want the configured %ds (floors must never lower it)",
				got, sec)
		}
		if got := engine.Config().Timeout; got != HostEngineCycleTimeout {
			t.Errorf("Timeout = %v, want the host floor %v (untouched by the client knob)",
				got, HostEngineCycleTimeout)
		}
	})

	t.Run("Config is a copy, never a lever on a bound engine", func(t *testing.T) {
		engine, ok := NewHostEngine(&hostTestClient{})
		if !ok || engine == nil {
			t.Fatal("NewHostEngine reported unbound")
		}
		cfg := engine.Config()
		cfg.ClientTimeout = time.Nanosecond // a caller's local edit
		if again := engine.Config(); again.ClientTimeout == time.Nanosecond {
			t.Error("mutating the returned Config re-budgeted the engine — Config must return a copy")
		}
	})

	t.Run("an unbound engine's Config is the zero value, not a panic", func(t *testing.T) {
		var unbound *Engine
		if got := unbound.Config(); got != (Config{}) {
			t.Errorf("(*Engine)(nil).Config() = %+v, want the zero Config", got)
		}
	})
}

// ---------------------------------------------------------------------------
// The reply-shape retry — bounded, cancellable, never a spin
// ---------------------------------------------------------------------------

// TestClientAdapterRetryHonoursAnExplicitBudgetAndCancellation: the budget is
// configurable (a positive Retries is the round count, not a flag) and a
// cancelled context stops the loop rather than spending another call. The base
// contract (default budget / disabled / transport error) lives in
// cov_hippocampus_test.go.
func TestClientAdapterRetryHonoursAnExplicitBudgetAndCancellation(t *testing.T) {
	t.Run("an explicit positive budget sets the attempt count", func(t *testing.T) {
		host := &hostTestClient{replies: []string{hostTestProseReply}}
		out, err := (&ClientAdapter{Client: host, Retries: 3}).Chat(context.Background(), "sys", "user")
		if err != nil {
			t.Fatalf("Chat: %v", err)
		}
		if want := 1 + 3; host.calls != want {
			t.Errorf("calls = %d, want %d (1 attempt + the configured 3)", host.calls, want)
		}
		if out != hostTestProseReply {
			t.Errorf("out = %q, want the JSON-less reply handed to the engine verbatim", out)
		}
	})

	t.Run("a cancelled context stops the retry loop", func(t *testing.T) {
		// The stub does not inspect ctx: the ADAPTER must be the one that refuses
		// to spend another attempt on a dead context (the engine's callJSON would
		// otherwise pay a full round trip for a reply nobody will read).
		host := &hostTestClient{replies: []string{hostTestProseReply}}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := (&ClientAdapter{Client: host}).Chat(ctx, "sys", "user"); err == nil {
			t.Errorf("calls = %d, want the adapter to report the cancelled context", host.calls)
		}
		if host.calls > 1 {
			t.Errorf("calls = %d, want at most 1 for a cancelled context", host.calls)
		}
	})

	t.Run("a usable reply costs exactly one attempt", func(t *testing.T) {
		host := &hostTestClient{replies: []string{hostTestJSONReply}}
		out, err := (&ClientAdapter{Client: host, Retries: 5}).Chat(context.Background(), "sys", "user")
		if err != nil {
			t.Fatalf("Chat: %v", err)
		}
		if host.calls != 1 {
			t.Errorf("calls = %d, want 1 (a JSON object needs no retry)", host.calls)
		}
		if out != hostTestJSONReply {
			t.Errorf("out = %q, want %q", out, hostTestJSONReply)
		}
	})
}
