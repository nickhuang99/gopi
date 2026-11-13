package hippocampus

import (
	"context"
	"errors"
	"time"
)

// Host-facing adapter (epic: hippocampus, child 3/4 — the scheduler half in
// internal/agent needs a production organ, not a stub).
//
// The dependency direction of devdoc/hippocampus.md §8.4 is preserved: this
// package never imports internal/agent. The host hands its LLM transport IN
// through the narrow HostClient interface below, and the agent-side binding
// (internal/agent/hippocampus_agent.go, newLiveHippocampusEngine) is the only
// code that knows both sides — it calls NewHostEngine below, so these floors
// and the ClientAdapter retry ARE the live path (a raw New(&adapter{}, …)
// would silently run the batch defaults instead).

// HostClient is the narrow LLM surface a host must provide. It mirrors the
// brew pipeline's transport (internal/tools/memory_brew_llm.go): one
// NON-streaming, thinking-disabled Chat call.
type HostClient interface {
	Chat(ctx context.Context, system, user string) (string, error)
}

// ---------------------------------------------------------------------------
// Reply-shape robustness (the brew lessons, memory #185 / #915).
// ---------------------------------------------------------------------------

const (
	// ClientAdapterDefaultRetries is how many ADDITIONAL attempts one prompt
	// gets when the reply carries no JSON object at all. A thinking model that
	// rambles through a whole reasoning preamble and returns no JSON is a
	// retryable shape; accepting it silently would archive a brief with no body.
	ClientAdapterDefaultRetries = 1
)

// ClientAdapter turns a HostClient into the engine's Client by adding the one
// behaviour the engine's strict-JSON prompts depend on: reply validation. The
// engine's callJSON extracts the first JSON object of the reply, so a reply
// WITHOUT one is retried (bounded) instead of being handed over to fail
// parsing.
//
// The transported HostClient owns the request shape (model / thinking flag /
// max_tokens) — the adapter judges replies only, so there is exactly one place
// per side.
type ClientAdapter struct {
	// Client is the transport. Required.
	Client HostClient
	// Retries is how many additional attempts a JSON-less reply earns
	// (0 = ClientAdapterDefaultRetries). Negative disables retrying.
	Retries int
}

// Chat implements Client.
func (c *ClientAdapter) Chat(ctx context.Context, system, user string) (string, error) {
	if c == nil || c.Client == nil {
		// Rule 15: an unbound organ is a recorded failure, never a silent "".
		return "", errors.New("hippocampus: no host LLM client bound")
	}
	attempts := c.Retries
	if attempts == 0 {
		attempts = ClientAdapterDefaultRetries
	}
	var out string
	var err error
	for try := 0; ; try++ {
		out, err = c.Client.Chat(ctx, system, user)
		if err != nil {
			return "", err
		}
		if extractJSONObject(out) != "" || try >= attempts {
			// Either a usable reply or the retries are spent: hand it to the
			// engine, which owns the "no JSON object in reply" error record.
			return out, nil
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
	}
}

// ---------------------------------------------------------------------------
// Production organ construction.
// ---------------------------------------------------------------------------

const (
	// HostEngineClientTimeout is the floor for ONE organ LLM call. The engine's
	// own default (DefaultClientTimeout) is a batch-pipeline value; a live run's
	// host call must survive a slow flash endpoint.
	HostEngineClientTimeout = 90 * time.Second
	// HostEngineCycleTimeout is the floor for a WHOLE cycle. The host's own
	// recall budget (hippocampusDefaultRecallTimeout, 30s) is the real
	// ceiling — it cancels the recall context — so this is only the outer guard
	// for a host that never cancels.
	HostEngineCycleTimeout = 4 * time.Minute
)

// NewHostEngine builds the production organ for a host (internal/agent).
//
// Knobs come from this package's OWN single reader (config.go's LoadConfig), so
// a deployment keeps tuning the cycle exactly as before; only the two timeouts
// are RAISED to their host floors (a larger configured value wins — the floors
// never lower an operator's deliberate setting).
//
// ok=false means the host passed no client: the caller must treat that as "the
// organ stays unbound", never as a bound organ that answers nothing.
func NewHostEngine(c HostClient) (*Engine, bool) {
	if c == nil {
		return nil, false
	}
	cfg := LoadConfig()
	if cfg.ClientTimeout < HostEngineClientTimeout {
		cfg.ClientTimeout = HostEngineClientTimeout
	}
	if cfg.Timeout < HostEngineCycleTimeout {
		cfg.Timeout = HostEngineCycleTimeout
	}
	return NewWithConfig(&ClientAdapter{Client: c}, ToolsMemory{}, cfg), true
}
