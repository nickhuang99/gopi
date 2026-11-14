package agent

// Hippocampus production binding (epic: hippocampus, child 4/4).
//
// Child 3 (feat/hippocampus-host) shipped the SCHEDULER — the host loop, the
// four delivery levels, the budget and the single wire-level seam
// (*Agent).SetHippocampusRecall. What it left open is the production ORGAN
// behind that seam: with nothing bound, an enabled host archives a "retrieval
// failed" marker every run and the feature can never activate. Real-session
// adoption proof (Rule 11) is therefore impossible without this file.
//
// It closes that seam in the one place that is allowed to see both sides
// (devdoc/hippocampus.md §6): the import direction stays acyclic —
// internal/hippocampus → internal/tools, internal/agent → internal/hippocampus.
// The organ package never imports internal/agent.
//
// The LLM surface is the brew precedent copied verbatim in SHAPE
// (internal/tools/memory_brew_llm.go, brewFlashClient): deepseek-v4-flash
// REQUIRES thinking:disabled (otherwise reasoning_content eats max_tokens and
// content comes back empty), while a GLM always-thinking endpoint REJECTS
// thinking:disabled (HTTP 400, code 1210) and must be throttled with
// reasoning_effort=low instead. One 400 with the always-thinking marker flips
// the adapter into effort mode for the rest of the run.

import (
	"context"
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/hippocampus"
	"github.com/nickhuang99/gopi/internal/thinkmode"
	"github.com/nickhuang99/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// LLM wire knobs (Rule 1: every literal named exactly once).
// ---------------------------------------------------------------------------

const (
	// hippoClientMaxTokens bounds one organ LLM call. Mirrors
	// tools.brewMaxTokens (the brew pipeline's identical JSON-over-flash
	// contract) — the organ's replies are a query list / one decision object.
	hippoClientMaxTokens = 4096
	// hippoClientMaxTokensEffort is the budget once the adapter flipped to
	// reasoning_effort mode: a GLM endpoint's reasoning consumes the output
	// budget even at effort=low (tools.brewMaxTokensEffortMode precedent).
	hippoClientMaxTokensEffort = 8192
	// hippoReasoningEffortLow is the always-thinking fallback throttle.
	hippoReasoningEffortLow = "low"
	// hippoRoleUser / hippoBlockTypeText are the wire role and content-block
	// type names (no constant package exists for them in internal/deepseek —
	// named here so this adapter carries none of its own bare literals).
	hippoRoleUser      = "user"
	hippoBlockTypeText = "text"
)

// Brief synthesis constants. The organ reports raw signals (ontology §2.2);
// these turn a Result into the host's brief WITHOUT inventing anything.
//
// The THREE numbers that are properties of the RETRIEVAL (how many leads a
// briefing shows, its body bound, the identity namespace and its join) are the
// ORGAN's, not a host copy: the organ derives its brief from the DISPLAYED
// leads, so a host spelling its own cap or bound would publish and key a
// different briefing than the one the organ sized (Rule 14 — one definition for
// a value two packages share). What stays here is presentation only, which is
// why hippoHitTextMaxChars has no organ twin.
const (
	// hippoSummaryMaxChars bounds the briefing body handed to the host. It is
	// the organ's brief-body bound (hippocampus.BriefMaxBodyChars) because a
	// digest and the organ's own body are the same artifact seen from two sides:
	// whatever length a brief may have, the host must not extend it.
	hippoSummaryMaxChars = hippocampus.BriefMaxBodyChars
	// hippoLeadKeyNamespace namespaces the displayed-lead identity keys, and
	// hippoBriefBodyKeyNamespace namespaces the rendered-body one. The two
	// namespaces keep the lead keys free of any collision with an action key
	// ("#5" is no longer a lead key) and name the identity dimension a session
	// audit is comparing.
	hippoLeadKeyNamespace      = hippocampus.BriefLeadKeyPrefix
	hippoBriefBodyKeyNamespace = "body:"
	// hippoSummaryMaxHits caps how many leads the digest names. It is the
	// ORGAN's brief budget, not a second number of the host's: how many leads a
	// briefing may show — and therefore which leads its weight is derived from —
	// is the organ's call (hippocampus.BriefTopHits), and the host shows exactly
	// that many.
	hippoSummaryMaxHits = hippocampus.BriefTopHits
	// hippoLeadDelimiter joins the rendered lead lines, so the body the model
	// reads and the identity the convergence gate compares are one rendering:
	// it is the ORGAN's delimiter (hippocampus.BriefLeadKeyDelimiter), the same
	// bytes its lead identity is built with.
	hippoLeadDelimiter = hippocampus.BriefLeadKeyDelimiter
	// hippoHitTextMaxChars bounds one lead's rendered text — a presentation
	// bound (a digest line, not the memory), so it belongs to the side that
	// renders the digest.
	hippoHitTextMaxChars = 160
)

// hippoClaudeClient adapts *deepseek.Client to hippocampus.Client.
type hippoClaudeClient struct {
	c          *deepseek.Client
	model      string
	effortMode bool // flipped on the always-thinking 400 (see Chat)
}

// Chat runs one non-streaming organ call. Shape copied from brewFlashClient
// deliberately: same thinking policy, same empty-content guard (Rule 15 — an
// empty reply is surfaced, never silently returned as "").
func (h *hippoClaudeClient) Chat(ctx context.Context, system, user string) (string, error) {
	req := h.buildRequest(system, user)
	if h.effortMode {
		req.ReasoningEffort = hippoReasoningEffortLow
	} else {
		req.Thinking = &deepseek.ClaudeThinking{Type: thinkmode.Disabled}
	}
	resp, err := h.c.ClaudeChat(ctx, req)
	if err != nil && !h.effortMode && isHippoAlwaysThinkingError(err) {
		h.effortMode = true
		return h.Chat(ctx, system, user) // retry once in effort mode
	}
	if err != nil {
		return "", fmt.Errorf("hippocampus: llm call: %w", err)
	}
	out := resp.Text()
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("hippocampus: llm returned empty content (stop=%s, out=%d in=%d tokens) — is thinking really disabled?",
			resp.StopReason, resp.Usage.OutputTokens, resp.Usage.InputTokens)
	}
	return out, nil
}

// buildRequest assembles the organ request shell (model/max_tokens/messages).
func (h *hippoClaudeClient) buildRequest(system, user string) *deepseek.ClaudeRequest {
	maxTokens := hippoClientMaxTokens
	if h.effortMode {
		maxTokens = hippoClientMaxTokensEffort
	}
	return &deepseek.ClaudeRequest{
		Model:     h.model,
		MaxTokens: maxTokens,
		System:    []deepseek.ClaudeSystemBlock{{Type: hippoBlockTypeText, Text: system}},
		Messages: []deepseek.ClaudeMessage{{
			Role: hippoRoleUser,
			Content: []deepseek.ClaudeContentBlock{{
				Type: hippoBlockTypeText,
				Text: user,
			}},
		}},
	}
}

// isHippoAlwaysThinkingError matches the GLM always-thinking 400 (code 1210),
// the same marker tools.isAlwaysThinkingError matches for the brew pipeline.
func isHippoAlwaysThinkingError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "不支持关闭思考") || strings.Contains(s, "always think")
}

// ---------------------------------------------------------------------------
// Live organ construction (the production binding, devdoc/hippocampus.md §6)
// ---------------------------------------------------------------------------

// newLiveHippocampusEngine builds the LIVE organ: the production transport
// wrapped by the organ package's own host binding.
//
// This is the ONLY constructor of the production organ, and it MUST go through
// hippocampus.NewHostEngine rather than the raw hippocampus.New. NewHostEngine
// is the single source of truth for the host floors (HostEngineClientTimeout /
// HostEngineCycleTimeout — a live host LLM call has to survive a slow flash
// endpoint, which the batch default ClientTimeout does not) and it wraps the
// transport in hippocampus.ClientAdapter, whose bounded retry rescues a reply
// that carries no JSON object at all. The raw New(&hippoClaudeClient{…}) this
// replaced ran the BATCH defaults (10s, no reply retry) for every production
// session while the floors sat declared-but-unused next door.
//
// ok=false means the agent has no transport — the caller must treat that as
// "the organ stays unbound", never as a bound organ that answers nothing
// (Rule 15; NewHostEngine's own contract).
func newLiveHippocampusEngine(c *deepseek.Client, model string) (*hippocampus.Engine, bool) {
	if c == nil {
		return nil, false
	}
	return hippocampus.NewHostEngine(&hippoClaudeClient{c: c, model: model})
}

// newHippocampusRecallFunc builds the production recall function for one
// agent. logger is the agent's own logger: every degraded cycle is recorded
// there (Rule 15 — the organ's failure must be visible somewhere, and the
// host only archives its own marker).
//
// A nil return means the organ could NOT be bound (no transport): the reason
// is logged here, and the caller must not substitute a function that answers
// nothing.
func newHippocampusRecallFunc(c *deepseek.Client, model string, logger func(format string, args ...any)) HippocampusRecallFunc {
	if logger == nil {
		logger = func(string, ...any) {}
	}
	engine, ok := newLiveHippocampusEngine(c, model)
	if !ok || engine == nil {
		logger("🧠 hippocampus enabled but no LLM transport — organ left unbound")
		return nil
	}
	return func(ctx context.Context, req HippocampusRecallRequest) (*HippocampusBrief, error) {
		res, err := engine.Run(ctx, hippocampus.Request{
			Context:   req.TaskContext,
			TaskID:    req.TaskID,
			SessionID: req.SessionID,
			Round:     req.Round,
		})
		if res == nil {
			return nil, err
		}
		if res.Err != nil {
			// Degrade, but never silently: the cycle's own failures are
			// recorded with the stop reason so a session can be audited.
			logger("🧠 hippocampus cycle degraded (stop=%s, round=%d): %v", res.StopReason, req.Round, res.Err)
		} else {
			logger("🧠 hippocampus cycle done (stop=%s, round=%d, hits=%d, actions=%d)",
				res.StopReason, req.Round, len(res.Hits), len(res.Actions))
		}
		return hippoBriefFromResult(res), nil
	}
}

// ---------------------------------------------------------------------------
// Recall adapter — hippocampus.Result → the host's brief
// ---------------------------------------------------------------------------

// hippoBriefFromResult synthesizes the brief. It carries ONLY what the
// retrieval produced (summary text + the raw signals) — never a delivery
// level (the scheduler's call) and never an answer to the task.
func hippoBriefFromResult(res *hippocampus.Result) *HippocampusBrief {
	// Rendered ONCE and passed to BOTH consumers — the body the model reads and
	// the identity the convergence gate compares cannot drift (Rule 13's
	// "constant exists to be referenced", applied to a rendering).
	summary := hippoBriefSummary(res)
	return &HippocampusBrief{
		Confidence:   hippoBriefConfidence(res),
		MemoryWeight: hippoBriefMemoryWeight(res),
		Summary:      summary,
		Memories:     len(res.Hits),
		Keys:         hippoBriefKeys(res, summary),
	}
}

// hippoBriefKeys renders the memory STATE this cycle reports, one stable key
// per executed action, per DISPLAYED lead and per delivered body. The host's
// convergence gate compares these key sets, which is what keeps a repeated
// briefing loop out of the model's context (epic child 4/4 noise control): the
// keys only change when the state the model can actually SEE changed.
//
// The keys are deliberately DERIVED FROM THE SAME RENDERING the host delivers
// (hippoLeadLines for the leads, hippoLeadDelimiter for their join — both also
// used by hippoBriefSummary). Keying on all res.Hits while the body names only
// the first hippoSummaryMaxHits would make the identity FINER than the display:
// a brief whose top-3 leads repeat but whose tail moved would pass "some key is
// new" and be re-injected with a byte-identical body — the prompt-budget leak
// the gate exists to stop, one narrower trigger.
//
// Two dimensions cover what the model is shown, because two deliveries are
// distinguishable in two ways:
//
//	leads — the displayed lead lines (topic/question/tier), the semantic content
//	body  — the rendered briefing text, the ONLY identity a brief with no lead
//	        can have (and what catches a body that moved for any other reason,
//	        e.g. a re-phrased query). Passed in rather than recomputed so the
//	        identity can never drift from the delivered text.
//
// An empty dimension contributes no key (see the body branch below), so a brief
// with no hits and an empty body yields NO keys — which the host's convergence
// gate reads as "nothing to compare, the model stays the judge" (a keyless
// brief never converges).
func hippoBriefKeys(res *hippocampus.Result, body string) []string {
	if res == nil {
		return nil
	}
	keys := make([]string, 0, len(res.Actions)+hippoSummaryMaxHits+1)
	seen := make(map[string]bool, len(res.Actions)+hippoSummaryMaxHits+1)
	add := func(k string) {
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		keys = append(keys, k)
	}
	for _, a := range res.Actions {
		if a.Err != "" {
			// A refused/failed action is NOT a state the model was told about.
			continue
		}
		switch {
		case a.Verb == hippocampus.VerbStore:
			// A store carries no row id on the action (the write result is in
			// Detail), so the key is its CONTENT: the same lesson stored twice
			// is the same state, a different lesson is not.
			add(fmt.Sprintf("%s:%s|%s", a.Verb,
				hippoOneLine(a.Topic, ""), hippoOneLine(a.Question, "")))
		case a.ID > 0:
			add(fmt.Sprintf("%s#%d", a.Verb, a.ID))
		}
	}
	for _, lead := range hippoLeadLines(res.Hits) {
		add(hippoLeadKeyNamespace + lead)
	}
	// The body dimension is emitted only for a body that EXISTS: "" must add no
	// key, or every hitless cycle would carry the same "body:" key and the host
	// would read two empty briefs as a converged state (a keyless brief must
	// stay keyless — prefer over-delivery to silence).
	if body != "" {
		add(hippoBriefBodyKeyNamespace + body)
	}
	return keys
}

// hippoLeadLines renders the DISPLAYED leads — the exact lines the briefing
// body shows the model, at most hippoSummaryMaxHits of them, in retrieval
// order. It is the ONE rendering shared by the body (hippoBriefSummary) and by
// the convergence identity (hippoBriefKeys), so "what the model sees" and "what
// the gate compares" are the same bytes by construction. res.Hits is never
// re-sorted anywhere in the pipeline (search order = relevance order), so the
// first N are the top N.
func hippoLeadLines(hits []tools.MemoryHit) []string {
	if len(hits) == 0 {
		return nil
	}
	n := len(hits)
	if n > hippoSummaryMaxHits {
		n = hippoSummaryMaxHits
	}
	lines := make([]string, 0, n)
	for _, h := range hits[:n] {
		lines = append(lines, fmt.Sprintf("- #%d [%s] %s", h.ID, h.Tier,
			hippoOneLine(h.Topic, h.Question)))
	}
	return lines
}

// hippoBriefConfidence is the organ's self-assessed relevance, as the organ
// defines it (hippocampus.ClampedBriefConfidence): the best lead's own
// confidence, damped by how many of the recall's leads matched. No hits ⇒ the
// organ's under-the-floor reading, which the host's scheduler archives at Lv-0
// and never injects.
func hippoBriefConfidence(res *hippocampus.Result) float64 {
	return hippocampus.ClampedBriefConfidence(res)
}

// hippoBriefMemoryWeight is the intrinsic weight of the memory the brief rests
// on, as the organ defines it (hippocampus.ClampedBriefMemoryWeight): the mean
// effective confidence of the DISPLAYED leads, tier-scaled and normalised by
// the tier weights so the reading keeps its [0,1] range.
func hippoBriefMemoryWeight(res *hippocampus.Result) float64 {
	return hippocampus.ClampedBriefMemoryWeight(res)
}

// hippoBriefSummary renders the briefing body: WHAT was retrieved and WHAT the
// organ did to the knowledge base. Deliberately a digest, not an answer — the
// model must still do the work (the organ's 职责漂移防线).
//
// A cycle that found nothing returns an EMPTY body: the host archives it at
// Lv-0 and injects nothing, which is what keeps an irrelevant recall quiet.
func hippoBriefSummary(res *hippocampus.Result) string {
	if res == nil || len(res.Hits) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("检索命中 ")
	sb.WriteString(fmt.Sprintf("%d 条", len(res.Hits)))
	if q := strings.TrimSpace(res.Query); q != "" {
		sb.WriteString("（查询：")
		sb.WriteString(q)
		sb.WriteString("）")
	}
	sb.WriteString("：")
	// The lead block is built from the SHARED rendering helper — the same lines
	// the convergence gate keys on (see hippoBriefKeys), joined by the same
	// delimiter, so display and identity cannot drift apart.
	leads := hippoLeadLines(res.Hits)
	if len(leads) > 0 {
		sb.WriteString(hippoLeadDelimiter)
		sb.WriteString(strings.Join(leads, hippoLeadDelimiter))
	}
	if acts := hippoActionDigest(res.Actions); acts != "" {
		sb.WriteString("\n已执行：")
		sb.WriteString(acts)
	}
	out := sb.String()
	if len(out) > hippoSummaryMaxChars {
		out = out[:hippoSummaryMaxChars]
	}
	return out
}

// hippoOneLine joins a hit's topic and question into one bounded line.
func hippoOneLine(topic, question string) string {
	s := strings.TrimSpace(topic)
	if q := strings.TrimSpace(question); q != "" {
		if s != "" {
			s += " — "
		}
		s += q
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > hippoHitTextMaxChars {
		s = s[:hippoHitTextMaxChars]
	}
	return s
}

// hippoActionDigest renders the executed decisions (the memory effect of the
// cycle) — the "act" half of retrieve → judge → act, which is what a session
// audit must be able to see. A refused action (Err) is rendered as such
// (Rule 15: never imply a write that failed).
func hippoActionDigest(actions []hippocampus.Action) string {
	if len(actions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(actions))
	for _, a := range actions {
		switch {
		case a.Err != "":
			parts = append(parts, fmt.Sprintf("%s 失败(%s)", a.Verb, a.Err))
		case a.Verb == hippocampus.VerbNone:
			parts = append(parts, hippocampus.VerbNone)
		case a.ID > 0:
			parts = append(parts, fmt.Sprintf("%s #%d", a.Verb, a.ID))
		case a.Topic != "":
			parts = append(parts, fmt.Sprintf("%s %q", a.Verb, hippoOneLine(a.Topic, "")))
		default:
			parts = append(parts, a.Verb)
		}
	}
	return strings.Join(parts, "; ")
}

// bindHippocampusRecall installs the production organ on a freshly built
// agent. It is a deliberate no-op when the gate is off — NewAgent always calls
// it, and the nil host IS the default-off guarantee (devdoc/hippocampus.md
// §4.2 口径 1). A binding failure is reported, never swallowed (Rule 15).
func bindHippocampusRecall(a *Agent, logger func(format string, args ...any)) {
	if a == nil || a.Hippocampus == nil {
		return
	}
	if a.Client == nil {
		if logger != nil {
			logger("🧠 hippocampus enabled but no LLM client — organ left unbound")
		}
		return
	}
	fn := newHippocampusRecallFunc(a.Client, a.Model, logger)
	if fn == nil {
		// newHippocampusRecallFunc has already logged WHY it could not build the
		// organ. Leaving the host unbound degrades to the gate-off behaviour
		// instead of archiving a retrieval-failure marker every round (the
		// reason IS recorded, so this is not a silent failure — Rule 15).
		return
	}
	if !a.SetHippocampusRecall(fn) {
		if logger != nil {
			logger("🧠 hippocampus recall binding refused — organ left unbound")
		}
	}
}
