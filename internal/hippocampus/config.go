// Package hippocampus is the core of gopi's active-memory organ: a bounded
// retrieve → judge → act cycle that fans out from the current task context
// into the knowledge base.
//
// The hippocampus NEVER talks to the LLM through a tool round-trip and adds
// NO top-level tool (Rule 3): it reaches the model through the narrow
// injected Client interface below and mutates memory through the in-process
// internal/tools functions (MemorySearchHits / MemoryStore / MemoryGet /
// MemoryUpdate / MemoryFeedback).
//
// Dependency direction: this package imports internal/tools (the memory
// surface) — internal/tools must NEVER import internal/hippocampus (that
// would be a cycle). See devdoc/hippocampus.md §8.4.
package hippocampus

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Wire / keyword constants (Rule 1b, Rule 13: no bare literals at use sites).
// ---------------------------------------------------------------------------

const (
	// VerbNone / VerbStore / VerbFetch / VerbUpdate / VerbFeedback are the
	// five decision verbs the Pro-style decision step may emit — exactly one
	// per cycle.
	VerbNone     = "none"
	VerbStore    = "store"
	VerbFetch    = "fetch"
	VerbUpdate   = "update"
	VerbFeedback = "feedback"

	// SourceTag is written into every record the hippocampus distills, so a
	// future brew/self-recall can tell where a row came from (the brew
	// anti-self-cannibalism precedent, tools.BrewSourceTag).
	SourceTag = "hippocampus"

	// RetrievalIntent is the routing hint handed to the memory engine's
	// multi-route recall (tools.MemorySearchHits intent parameter).
	RetrievalIntent = "hippocampus"
)

// DecisionVerbs lists the legal verbs in a stable order; tests and prompt
// rendering both read it, so the legal set lives in exactly one place.
var DecisionVerbs = []string{VerbNone, VerbStore, VerbFetch, VerbUpdate, VerbFeedback}

// ---------------------------------------------------------------------------
// Tuning defaults (Rule 1/2: every number named, env read in ONE place).
// ---------------------------------------------------------------------------

const (
	// DefaultMaxRounds bounds the retrieve → judge → act cycle: after this
	// many iterations the loop stops even if the budget still allows work
	// (the "bounded cycle, no runaway loop" contract).
	DefaultMaxRounds = 3
	// DefaultRetrievalLimit caps how many hits one recall asks the memory
	// engine for.
	DefaultRetrievalLimit = 5
	// DefaultMaxAct is the per-kinds budget: how many DB mutations one cycle
	// may perform (a store-heavy cycle cannot write unbounded rows).
	DefaultMaxAct = 2
	// DefaultTimeout is the wall-clock ceiling of a whole cycle. On expiry
	// the engine returns the partial result it already has, always non-nil.
	DefaultTimeout = 20 * time.Second
	// DefaultClientTimeout is the per-LLM-call ceiling (mirrors
	// tools.brewClientTimeout's role for the brew pipeline).
	DefaultClientTimeout = 10 * time.Second
	// decideJSONMaxChars bounds how much of a decision reply is scanned for
	// a JSON object — a model that rambles cannot blow up memory here.
	decideJSONMaxChars = 4096
	// retrievalWords counts how many context words are distilled into one
	// retrieval query.
	retrievalWords = 12
)

// ---------------------------------------------------------------------------
// Configuration — GOPI_HIPPOCAMPUS_* env, constants as defaults (Rule 2).
// ---------------------------------------------------------------------------

const (
	// hippocampusEnvPrefix is the shared NAMESPACE of every hippocampus knob —
	// a prefix, not a knob: it is never handed to an env call (the master
	// switch is GOPI_HIPPOCAMPUS itself, spelled by
	// internal/agent.hippocampusEnvEnabled). A gate test asserts this literal
	// appears in config.go ONLY (Rule 13).
	//
	// env-reader-exempt: namespace prefix, deliberately not passed to an env
	// call — its consumers are the GOPI_HIPPOCAMPUS_* names below.
	hippocampusEnvPrefix = "GOPI_HIPPOCAMPUS"

	// envMaxRounds / envRetrievalLimit / envMaxAct / envTimeoutSec override
	// the constants above; they are read ONLY in LoadConfig.
	envMaxRounds      = "GOPI_HIPPOCAMPUS_MAX_ROUNDS"
	envRetrievalLimit = "GOPI_HIPPOCAMPUS_RETRIEVAL_LIMIT"
	envMaxAct         = "GOPI_HIPPOCAMPUS_MAX_ACT"
	envTimeoutSec     = "GOPI_HIPPOCAMPUS_TIMEOUT_SEC"
	envClientTimeout  = "GOPI_HIPPOCAMPUS_CLIENT_TIMEOUT_SEC"
)

// Config is the resolved knobs of one engine. Every field has a constant
// default; positive env overrides win. Zero value is NOT usable — build it
// with LoadConfig.
type Config struct {
	MaxRounds      int
	RetrievalLimit int
	MaxAct         int
	Timeout        time.Duration
	ClientTimeout  time.Duration
}

// DefaultConfig returns the constant-only config (env ignored) — the
// canonical set of defaults, and what tests use.
func DefaultConfig() Config {
	return Config{
		MaxRounds:      DefaultMaxRounds,
		RetrievalLimit: DefaultRetrievalLimit,
		MaxAct:         DefaultMaxAct,
		Timeout:        DefaultTimeout,
		ClientTimeout:  DefaultClientTimeout,
	}
}

// LoadConfig resolves the knobs from GOPI_HIPPOCAMPUS_* env vars, falling
// back to the constants. THIS IS THE ONLY PLACE that reads them (Rule 2):
// a non-positive or unparseable value keeps the default.
func LoadConfig() Config {
	c := DefaultConfig()
	if n, ok := envInt(envMaxRounds); ok && n > 0 {
		c.MaxRounds = n
	}
	if n, ok := envInt(envRetrievalLimit); ok && n > 0 {
		c.RetrievalLimit = n
	}
	if n, ok := envInt(envMaxAct); ok && n > 0 {
		c.MaxAct = n
	}
	if n, ok := envInt(envTimeoutSec); ok && n > 0 {
		c.Timeout = time.Duration(n) * time.Second
	}
	if n, ok := envInt(envClientTimeout); ok && n > 0 {
		c.ClientTimeout = time.Duration(n) * time.Second
	}
	return c
}

// envInt parses a positive-or-zero integer env var; ok=false when unset,
// empty or unparseable.
func envInt(name string) (int, bool) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}
