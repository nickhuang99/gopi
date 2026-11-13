package hippocampus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/tools"
)

// Engine is the bounded retrieve → judge → act cycle (spec §6). Both
// collaborators are interfaces so tests inject a stub client (network-free,
// deterministic) and, when needed, a fake memory surface.
type Engine struct {
	client Client
	memory Memory
	cfg    Config
}

// New builds an engine with the constants + the hippocampus env knobs
// resolved (LoadConfig is the single env reader, Rule 2).
func New(c Client, m Memory) *Engine { return NewWithConfig(c, m, LoadConfig()) }

// NewWithConfig builds an engine with an explicit config (tests use it to pin
// tiny budgets so a cycle provably stops).
func NewWithConfig(c Client, m Memory, cfg Config) *Engine {
	return &Engine{client: c, memory: m, cfg: cfg}
}

// Context is the engine's memory surface, exposed for hosts that want to read
// what the organ has access to without running a cycle.
func (e *Engine) Context() Memory { return e.memory }

// Config is the engine's RESOLVED knobs — the budgets the cycle actually runs
// under (cfg.ClientTimeout ceilings one LLM call, cfg.Timeout the whole cycle).
//
// It mirrors Context(): the host that binds an organ in production has no
// other way to assert WHICH budgets are live (everything else about the config
// is unexported). The value is a COPY, so a caller can never re-budget a
// running engine through it — the cycle reads e.cfg.
func (e *Engine) Config() Config {
	if e == nil {
		return Config{}
	}
	return e.cfg
}

// ---------------------------------------------------------------------------
// Prompts (the two-tier LLM split: Flash-style divergence, Pro-style decision).
// ---------------------------------------------------------------------------

const (
	retrievalSystem = `You are the RETRIEVAL half of a memory organ. You never answer the task. ` +
		`Given a task context, emit 1-3 short search queries that would find relevant distilled lessons ` +
		`in a knowledge base (embed the context's key nouns and synonyms; one query per angle). ` +
		`Reply with ONE JSON object: {"queries":["...","..."]}`

	decideSystem = `You are the DECISION half of a memory organ. You never answer the task. ` +
		`Given a task context and the memory leads already retrieved, emit EXACTLY ONE action as ONE JSON object. ` +
		`Legal verbs and shapes:
{"verb":"none","reason":"why nothing is needed"}
{"verb":"store","topic":"...","question":"...","answer":"...","keywords":["k1","k2"]}
{"verb":"fetch","id":123}
{"verb":"update","id":123,"tier":"cold|warm|hot","confidence":0.7,"reason":"..."}
{"verb":"feedback","id":123,"rating":"useful|useless","reason":"..."}
Use "store" only when the context contains a distilled lesson; "feedback" only when a retrieved lead was ` +
		`actually adopted or contradicted; "update" only to re-tier/correct an existing row.`
)

// ---------------------------------------------------------------------------
// The cycle.
// ---------------------------------------------------------------------------

// Run performs ONE bounded cycle and always returns a non-nil Result (a
// partial one on timeout), plus a non-nil error only for a caller-side
// failure (nil client/memory). Every failure INSIDE the cycle is recorded on
// the result (Rule 15) and stops or constrains the loop instead of spinning.
func (e *Engine) Run(ctx context.Context, req Request) (*Result, error) {
	if e.client == nil {
		return nil, errors.New("hippocampus: nil client")
	}
	if e.memory == nil {
		return nil, errors.New("hippocampus: nil memory")
	}

	cctx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()

	res := &Result{StopReason: StopTimeout}
	var errs []error
	record := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	finish := func(reason string) (*Result, error) {
		res.StopReason = reason
		res.Err = errors.Join(errs...)
		return res, nil
	}

	for round := 0; round < e.cfg.MaxRounds; round++ {
		if cctx.Err() != nil {
			return finish(StopTimeout)
		}

		queries, err := e.retrieveQueries(cctx, req.Context)
		if err != nil {
			record(err)
			return finish(stopFor(cctx, StopClientError))
		}
		if len(queries) == 0 {
			return finish(StopNoQuery)
		}
		res.Query = queries[0]

		hits, err := e.fanOut(cctx, queries)
		if err != nil {
			record(err)
			return finish(stopFor(cctx, StopMemoryError))
		}
		res.Hits = hits
		res.Rounds++

		dec, err := e.decide(cctx, req, queries, hits)
		if err != nil {
			record(err)
			return finish(stopFor(cctx, StopClientError))
		}

		act := e.execute(dec, req, hits)
		res.Actions = append(res.Actions, act)
		if act.Err != "" {
			record(fmt.Errorf("hippocampus: %s: %s", act.Verb, act.Err))
		}

		switch dec.Verb {
		case VerbNone:
			return finish(StopNoneEmitted)
		case VerbStore:
			if len(hits) == 0 {
				// Nothing recalled ⇒ the lesson is new; storing it is the
				// terminal act of this cycle (no re-retrieval on a store).
				return finish(StopEmptySearch)
			}
		}
		if len(res.Actions) >= e.cfg.MaxAct {
			return finish(StopActBudget)
		}
		if cctx.Err() != nil {
			return finish(StopTimeout)
		}
	}
	return finish(StopRoundLimit)
}

// stopFor reports fallback unless the cycle context has expired, in which
// case the truthful stop reason is the timeout (a deadline cancels the LLM
// call and looks like a client error otherwise).
func stopFor(ctx context.Context, fallback string) string {
	if ctx.Err() != nil {
		return StopTimeout
	}
	return fallback
}

// retrieveQueries is the Flash-style divergent step: one LLM call that turns
// the task context into several retrieval angles.
func (e *Engine) retrieveQueries(ctx context.Context, taskContext string) ([]string, error) {
	taskContext = strings.TrimSpace(taskContext)
	if taskContext == "" {
		return nil, nil
	}
	var out struct {
		Queries []string `json:"queries"`
	}
	if err := e.callJSON(ctx, retrievalSystem, retrievalUser(taskContext), &out); err != nil {
		return nil, err
	}
	return normalizeQueries(out.Queries), nil
}

// fanOut runs each divergent query against the memory engine and unions the
// leads (deduped by ID, first query wins), capped at cfg.RetrievalLimit.
func (e *Engine) fanOut(ctx context.Context, queries []string) ([]tools.MemoryHit, error) {
	seen := map[int]bool{}
	hits := make([]tools.MemoryHit, 0, e.cfg.RetrievalLimit)
	for _, q := range queries {
		if ctx.Err() != nil {
			return hits, nil // partial: the timeout path records the stop.
		}
		found, err := e.memory.Search(q, e.cfg.RetrievalLimit, RetrievalIntent)
		if err != nil {
			return nil, fmt.Errorf("hippocampus: recall %q: %w", q, err)
		}
		for _, h := range found {
			if seen[h.ID] {
				continue
			}
			seen[h.ID] = true
			hits = append(hits, h)
			if len(hits) >= e.cfg.RetrievalLimit {
				return hits, nil
			}
		}
	}
	return hits, nil
}

// decide is the Pro-style decision step: exactly one verb, rendered as JSON.
func (e *Engine) decide(ctx context.Context, req Request, queries []string, hits []tools.MemoryHit) (Decision, error) {
	var d Decision
	user := decideUser(req, queries, hits)
	if err := e.callJSON(ctx, decideSystem, user, &d); err != nil {
		return Decision{}, err
	}
	d.Verb = strings.ToLower(strings.TrimSpace(d.Verb))
	if !legalVerb(d.Verb) {
		return Decision{}, fmt.Errorf("hippocampus: decision emitted illegal verb %q (legal: %s)",
			d.Verb, strings.Join(DecisionVerbs, ", "))
	}
	return d, nil
}

// execute runs the chosen verb through the IN-PROCESS memory functions. A
// refused action is recorded on the Action (never swallowed) and returned as
// a no-mutation step: the cycle stays bounded and auditable.
func (e *Engine) execute(d Decision, req Request, hits []tools.MemoryHit) Action {
	act := Action{
		Verb:     d.Verb,
		Query:    d.Query,
		Topic:    strings.TrimSpace(d.Topic),
		Question: strings.TrimSpace(d.Question),
		Answer:   strings.TrimSpace(d.Answer),
		Source:   SourceTag,
		Keywords: normalizeKeywords(d.Keywords),
		ID:       d.ID,
		Rating:   strings.ToLower(strings.TrimSpace(d.Rating)),
		Reason:   strings.TrimSpace(d.Reason),
		Tier:     strings.TrimSpace(d.Tier),
	}
	if req.TaskID != "" {
		act.Keywords = append(act.Keywords, req.TaskID)
	}
	if req.SessionID != "" {
		act.Keywords = append(act.Keywords, req.SessionID)
	}

	switch d.Verb {
	case VerbNone:
		act.Detail = "no action"
	case VerbStore:
		if act.Topic == "" || act.Question == "" || act.Answer == "" {
			act.Err = "store needs topic+question+answer"
			return act
		}
		out, err := e.memory.Store(act.Topic, act.Question, act.Answer, act.Source, act.Keywords)
		if err != nil {
			act.Err = err.Error()
			return act
		}
		act.Detail = out
	case VerbFetch:
		out, err := e.memory.Get(act.ID)
		if err != nil {
			act.Err = err.Error()
			return act
		}
		act.Detail = out
	case VerbUpdate:
		f := tools.MemoryUpdateFields{Tier: act.Tier, Topic: act.Topic, Question: act.Question, Answer: act.Answer}
		if d.Confidence != nil {
			f.Confidence = d.Confidence
		}
		out, err := e.memory.Update(act.ID, f)
		if err != nil {
			act.Err = err.Error()
			return act
		}
		act.Detail = out
	case VerbFeedback:
		if act.Rating != tools.RatingUseful && act.Rating != tools.RatingUseless && act.Rating != tools.RatingUnused {
			act.Err = fmt.Sprintf("feedback rating %q must be %s/%s/%s",
				act.Rating, tools.RatingUseful, tools.RatingUseless, tools.RatingUnused)
			return act
		}
		out, err := e.memory.Feedback(act.ID, act.Rating, act.Reason, req.TaskID)
		if err != nil {
			act.Err = err.Error()
			return act
		}
		act.Detail = out
	default:
		act.Err = fmt.Sprintf("illegal verb %q", d.Verb)
	}
	return act
}

// ---------------------------------------------------------------------------
// LLM plumbing (mirrors tools.callBrewJSON: JSON extracted from prose).
// ---------------------------------------------------------------------------

// callJSON runs one prompt with the per-call timeout and unmarshals the first
// JSON object of the reply into out.
func (e *Engine) callJSON(ctx context.Context, system, user string, out interface{}) error {
	cctx, cancel := context.WithTimeout(ctx, e.cfg.ClientTimeout)
	defer cancel()
	raw, err := e.client.Chat(cctx, system, user)
	if err != nil {
		return fmt.Errorf("hippocampus: llm call: %w", err)
	}
	js := extractJSONObject(raw)
	if js == "" {
		return fmt.Errorf("hippocampus: no JSON object in reply: %.200s", raw)
	}
	if err := json.Unmarshal([]byte(js), out); err != nil {
		return fmt.Errorf("hippocampus: JSON reply unparseable: %w (reply: %.200s)", err, raw)
	}
	return nil
}

// extractJSONObject returns the first balanced {...} object of s ("" when
// there is none). Models wrap JSON in prose despite the instruction — same
// lesson as the brew pipeline.
func extractJSONObject(s string) string {
	if len(s) > decideJSONMaxChars {
		s = s[:decideJSONMaxChars]
	}
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case ch == '\\':
				esc = true
			case ch == '"':
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Task-context digestion (pure, deterministic).
// ---------------------------------------------------------------------------

// retrievalUser renders the retrieval prompt body: the context tail matters
// most (the current step), so the last retrievalWords words are used as the
// implicit query seed.
func retrievalUser(taskContext string) string {
	seed := strings.Join(lastWords(taskContext, retrievalWords), " ")
	return "TASK CONTEXT:\n" + taskContext + "\n\nCURRENT STEP (weight this highest):\n" + seed
}

// decideUser renders the decision prompt body: the context, the queries
// tried, and the retrieved leads (id/topic/tier/confidence + question) — the
// minimum a decision needs to pick a verb.
func decideUser(req Request, queries []string, hits []tools.MemoryHit) string {
	var b strings.Builder
	b.WriteString("TASK CONTEXT:\n")
	b.WriteString(req.Context)
	b.WriteString("\n\nQUERIES TRIED: ")
	b.WriteString(strings.Join(queries, " | "))
	if len(hits) == 0 {
		b.WriteString("\n\nLEADS: none — nothing on this topic has been distilled yet.")
	} else {
		b.WriteString("\n\nLEADS:")
		for _, h := range hits {
			fmt.Fprintf(&b, "\n#%d [%s conf=%.2f] %s", h.ID, h.Tier, h.Confidence, h.Topic)
			if q := strings.TrimSpace(h.Question); q != "" {
				b.WriteString("\n    Q: " + q)
			}
		}
	}
	return b.String()
}

// lastWords returns at most n trailing whitespace-separated words of s.
func lastWords(s string, n int) []string {
	f := strings.Fields(s)
	if len(f) <= n {
		return f
	}
	return f[len(f)-n:]
}

// normalizeQueries trims, drops empties and dedupes the divergent queries,
// preserving order.
func normalizeQueries(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, q := range in {
		q = strings.TrimSpace(q)
		if q == "" || seen[q] {
			continue
		}
		seen[q] = true
		out = append(out, q)
	}
	return out
}

// normalizeKeywords trims and drops empty keyword breadcrumbs.
func normalizeKeywords(in []string) []string {
	out := make([]string, 0, len(in))
	for _, k := range in {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// legalVerb reports whether v is one of the five decision verbs.
func legalVerb(v string) bool {
	for _, l := range DecisionVerbs {
		if l == v {
			return true
		}
	}
	return false
}
