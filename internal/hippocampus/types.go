package hippocampus

import (
	"context"

	"github.com/nick/gopi/internal/tools"
)

// Client is the narrow LLM surface the hippocampus uses — deliberately the
// same shape as tools.brewClient (internal/tools/memory_brew_llm.go): one
// non-streaming Chat call. Tests inject a stub, so the whole cycle runs
// network-free and deterministic; the real adapters are built by the host
// package (internal/agent) from deepseek.NewClaudeClient().
type Client interface {
	Chat(ctx context.Context, system, user string) (string, error)
}

// Memory is the in-process memory surface (NO tool round-trip, Rule 3).
// *ToolsMemory is the production implementation; tests may substitute a fake
// to observe the exact calls.
type Memory interface {
	Search(query string, limit int, intent string) ([]tools.MemoryHit, error)
	Store(topic, question, answer, source string, keywords []string) (string, error)
	Get(id int) (string, error)
	Update(id int, f tools.MemoryUpdateFields) (string, error)
	Feedback(id int, rating, reason, taskID string) (string, error)
}

// ToolsMemory adapts the in-process internal/tools memory functions to
// Memory. Every method is a direct call — the hippocampus is part of gopi's
// process, not a tool client.
type ToolsMemory struct{}

// Search returns the structured hits of one recall (tools.MemorySearchHits).
func (ToolsMemory) Search(query string, limit int, intent string) ([]tools.MemoryHit, error) {
	return tools.MemorySearchHits(query, limit, intent)
}

// Store distills one record (tools.MemoryStore).
func (ToolsMemory) Store(topic, question, answer, source string, keywords []string) (string, error) {
	return tools.MemoryStore(topic, question, answer, source, keywords)
}

// Get reads one record (tools.MemoryGet).
func (ToolsMemory) Get(id int) (string, error) { return tools.MemoryGet(id) }

// Update edits one record (tools.MemoryUpdate).
func (ToolsMemory) Update(id int, f tools.MemoryUpdateFields) (string, error) {
	return tools.MemoryUpdate(id, f)
}

// Feedback records one vote (tools.MemoryFeedback).
func (ToolsMemory) Feedback(id int, rating, reason, taskID string) (string, error) {
	return tools.MemoryFeedback(id, rating, reason, taskID)
}

// Request is one ask the host (scheduler) makes of the hippocampus. The
// hippocampus never probes the agent's task state itself — the host hands it
// in (ontology §2.2: no context judgment inside the organ).
type Request struct {
	// Context is the textual task context the retrieval fans out from.
	Context string
	// TaskID / SessionID are provenance, written into stored records and
	// votes so the audit trail names the origin.
	TaskID    string
	SessionID string
	// Round is the host's round number (0 when outside a tool loop).
	Round int
}

// Action is one executed decision, recorded verbatim so the host (and tests)
// can audit what the organ did to the knowledge base.
type Action struct {
	Verb     string
	Query    string
	Topic    string
	Question string
	Answer   string
	Source   string
	Keywords []string
	ID       int
	Rating   string
	Reason   string
	Tier     string
	// Detail is the rendered result of the in-process memory call.
	Detail string
	// Err is non-empty when the action was refused or the memory call failed
	// (Rule 15: a failure is recorded, never swallowed).
	Err string
}

// Result is the outcome of one bounded cycle. It is ALWAYS non-nil on
// return, even on timeout (the partial result).
type Result struct {
	// Query is the retrieval query the divergent step produced.
	Query string
	// Hits are the structured leads the retrieval returned.
	Hits []tools.MemoryHit
	// Actions are the executed decisions in order.
	Actions []Action
	// Rounds counts the completed cycles (search + decide + act).
	Rounds int
	// StopReason is one of the Stop* constants: WHY the bounded cycle ended.
	StopReason string
	// Err joins every recorded failure of the cycle (nil when none) — the
	// organ degrades (or does nothing) but never logs nothing.
	Err error
}

// Stop reasons — the bounded-cycle contract, one constant per exit path.
const (
	// StopNoneEmitted: the decision step chose the "none" verb.
	StopNoneEmitted = "none_emitted"
	// StopRoundLimit: the round budget was exhausted.
	StopRoundLimit = "round_limit"
	// StopActBudget: the act budget was exhausted.
	StopActBudget = "act_budget"
	// StopTimeout: the wall-clock ceiling expired (partial result returned).
	StopTimeout = "timeout"
	// StopNoQuery: the divergent step produced no usable query and no work
	// was possible.
	StopNoQuery = "no_query"
	// StopEmptySearch: the retrieval found nothing — the store path; an
	// explicit no-op decision ends the cycle.
	StopEmptySearch = "empty_search"
	// StopClientError: the LLM client failed; the cycle stops instead of
	// retrying unboundedly.
	StopClientError = "client_error"
	// StopMemoryError: the memory surface failed (unopenable DB, foreign
	// file, malformed schema) — recorded and stopped, never retried blind.
	StopMemoryError = "memory_error"
)
