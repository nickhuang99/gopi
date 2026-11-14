package agent

// memory_feedback_contract_test.go — the PROMPT/SCHEMA ↔ VALIDATOR sync gate.
//
// The defect this file exists to prevent (measured 2026-09-27): the memory
// action=feedback schema and all four description locations taught a parameter
// (`tool_use_id`) that the feedback path never read, and never mentioned `id`,
// which validateToolArgs requires for EVERY rating. A model voting exactly as
// documented was refused ("Missing required parameter: id (memory action=feedback)"),
// the vote never reached the ledger, and the search pair it was trying to keep
// was dropped anyway (association is by RECORD id, never by a call id).
//
// The authority is the VALIDATOR, not this file: the demand list below is
// re-observed against validateToolArgs on every run, and the schema and
// descriptions must agree with it — so a later change to either side fails
// here instead of silently shipping a documented call the engine refuses.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// memoryDescriptionKey is the pseudo-property name the memory tool's own
// description is stored under in scopedSchemaDoc's map — it is not a schema
// property, so the name cannot collide with one.
const memoryDescriptionKey = "<memory tool description>"

// retiredFeedbackParam is the parameter whose promised mechanism (drop the
// originating search pair when a vote names it) was retired when context
// slimming moved to WHITELIST semantics: slimming now drops every search pair
// unless one of its records was voted useful, matched by record id. The
// parameter is unread on the feedback path, so it must appear nowhere in the
// schema or in the descriptions the model reads.
const retiredFeedbackParam = "tool_use_id"

// retiredDropPromise is the sentence fragments the descriptions used to carry —
// the harness removing a search's tool_use + tool_result pair because a vote
// named its call id. No such code path exists (whitelist slimming drops
// unrewarded pairs whether or not a vote ever arrived), so the promise must
// appear nowhere.
var retiredDropPromise = []string{
	"that search's tool_use + tool_result pair from context",
	"drop it from context right away",
}

// requiredDocToken is how a schema parameter's description must mark a
// parameter the validator demands: the word REQUIRED. A parameter the engine
// refuses without is never described as optional.
const requiredDocToken = "REQUIRED"

// whitelistRuleToken is the true survival rule the descriptions must teach:
// a search pair survives context slimming iff one of its records was voted
// useful (matched by record id).
const whitelistRuleToken = "voted useful"

// recordIDToken is the record-id anchor the model needs to send a vote the
// engine accepts: the required id is the #N on the hit line, not a call id.
const recordIDToken = "#N"

// feedbackValidatorDemand is ONE validator requirement, paired with the exact
// message validateToolArgs returns when it is unmet. The message is asserted
// verbatim so a reworded validator cannot silently desynchronize the prompt.
type feedbackValidatorDemand struct {
	param   string
	message string
}

// feedbackValidatorDemands is the required-parameter contract for
// memory(action=feedback). Each entry is re-observed against the validator by
// TestFeedbackSchemaTeachesWhatTheValidatorDemands before it is trusted, so
// this list is a claim the test verifies rather than a second source of truth.
func feedbackValidatorDemands() []feedbackValidatorDemand {
	return []feedbackValidatorDemand{
		{param: "id", message: "Missing required parameter: id (memory action=feedback)"},
		{param: "rating", message: "Missing required parameter: rating (memory action=feedback)"},
	}
}

// feedbackCall builds a complete, valid memory(action=feedback) argument map
// without the named param, so the validator's veto can be observed directly.
func feedbackCall(omit string, rating string) map[string]interface{} {
	args := map[string]interface{}{
		"action": toolnames.ToolMemoryActionFeedback,
		"id":     float64(1),
		"rating": rating,
		"reason": "contract gate",
	}
	delete(args, omit)
	return args
}

// scopedSchemaDoc returns the description text the schema attaches to one
// action: every property whose description routes itself to that action, plus
// the memory tool's own description under memoryDescriptionKey.
func scopedSchemaDoc(t *testing.T, action string) map[string]string {
	t.Helper()
	mem := toolDefByName(t, toolnames.ToolMemory)
	if mem == nil {
		t.Fatal("memory missing from claudeToolDefs")
	}
	scope := "action=" + action
	out := map[string]string{memoryDescriptionKey: mem.Description}
	for name, raw := range schemaProps(t, mem) {
		prop, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		desc, _ := prop["description"].(string)
		if strings.Contains(desc, scope) {
			out[name] = desc
		}
	}
	return out
}

// TestFeedbackSchemaTeachesWhatTheValidatorDemands is the SYNC GATE
// (deliverable 4): for every parameter the validator requires on
// memory(action=feedback), the schema must route that parameter to the
// feedback action AND mark it REQUIRED. The demand list is re-observed against
// validateToolArgs first, so the gate reads the engine rather than trusting
// prose. RED on the pre-change tree, where neither `id` nor `rating` carried
// the required marker while the validator refused calls without them.
func TestFeedbackSchemaTeachesWhatTheValidatorDemands(t *testing.T) {
	doc := scopedSchemaDoc(t, toolnames.ToolMemoryActionFeedback)
	for _, want := range feedbackValidatorDemands() {
		if got := validateToolArgs(toolnames.ToolMemory, feedbackCall(want.param, "useful")); got != want.message {
			t.Fatalf("validator no longer demands %s (got %q, want %q): the contract this gate reads has moved — update the demand list deliberately",
				want.param, got, want.message)
		}
		desc, scoped := doc[want.param]
		if !scoped {
			t.Errorf("schema parameter %q is required by the validator for memory(action=feedback) but its description does not route it to action=%s",
				want.param, toolnames.ToolMemoryActionFeedback)
			continue
		}
		if !strings.Contains(desc, requiredDocToken) {
			t.Errorf("schema parameter %q is required by the validator for memory(action=feedback) but its description does not say %s: %q",
				want.param, requiredDocToken, desc)
		}
	}
	// The validator accepts a call that carries exactly the demanded params —
	// the gate's premise, measured rather than assumed.
	for _, rating := range []string{"useful", "useless"} {
		if msg := validateToolArgs(toolnames.ToolMemory, feedbackCall("", rating)); msg != "" {
			t.Errorf("a call with only the demanded feedback params is refused (%q): the demand list is incomplete", msg)
		}
	}
}

// TestFeedbackSchemaScopedParamsAreAcceptedWhenOptional is the gate's other
// direction: any parameter the schema routes to action=feedback that the
// validator does NOT demand must be genuinely optional — omitting it from an
// otherwise complete call must be accepted. A future engine change that starts
// requiring one of them turns this RED, forcing the demand list (and the
// description) to be updated in the same commit instead of drifting.
func TestFeedbackSchemaScopedParamsAreAcceptedWhenOptional(t *testing.T) {
	doc := scopedSchemaDoc(t, toolnames.ToolMemoryActionFeedback)
	demanded := map[string]bool{}
	for _, want := range feedbackValidatorDemands() {
		demanded[want.param] = true
	}
	for param := range doc {
		if param == memoryDescriptionKey || demanded[param] {
			continue
		}
		if msg := validateToolArgs(toolnames.ToolMemory, feedbackCall(param, "useful")); msg != "" {
			t.Errorf("schema routes %q to memory(action=feedback) as optional, but the validator refuses a call without it (%q) — the prompt would teach a refused call",
				param, msg)
		}
	}
}

// TestFeedbackSchemaDropsTheRetiredParam (deliverable 3): the retired
// parameter is gone from the schema AND from every feedback-facing
// description, and the retired drop-on-vote promise is gone with it. The
// parameter was unread on the feedback path — its presence was a standing
// invitation to send a call shape the engine never read.
func TestFeedbackSchemaDropsTheRetiredParam(t *testing.T) {
	mem := toolDefByName(t, toolnames.ToolMemory)
	if mem == nil {
		t.Fatal("memory missing from claudeToolDefs")
	}
	if _, ok := schemaProps(t, mem)[retiredFeedbackParam]; ok {
		t.Errorf("memory schema still declares %q: it is unread on the feedback path (slimming is whitelist-by-record-id) — the parameter must be gone",
			retiredFeedbackParam)
	}
	doc := scopedSchemaDoc(t, toolnames.ToolMemoryActionFeedback)
	for where, text := range doc {
		if strings.Contains(text, retiredFeedbackParam) {
			t.Errorf("%s still names the retired parameter %q (feedback must address a record by its #id alone)", where, retiredFeedbackParam)
		}
		for _, promise := range retiredDropPromise {
			if strings.Contains(text, promise) {
				t.Errorf("%s still promises the retired drop-on-vote mechanism (%q): slimming is whitelist-by-record-id, not vote-triggered", where, promise)
			}
		}
	}
}

// TestFeedbackVoteLandsWithoutTheRetiredParam (acceptance): a call carrying
// exactly the documented shape — action=feedback, id=#N, rating — validates
// AND lands a ledger row, for rating=useful and rating=useless alike, with the
// retired parameter absent. The pre-change tree could not satisfy the
// documented shape at all: the descriptions taught the call-id parameter and
// never mentioned `id`, which the validator demanded, so a compliant model was
// refused and the vote never reached the ledger.
func TestFeedbackVoteLandsWithoutTheRetiredParam(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	if _, err := tools.MemoryStore(
		"contract gate topic",
		"does a documented feedback call land in the ledger?",
		"it does: id + rating are the whole documented shape, and the vote is appended to memory_feedback",
		"contract-gate", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	first, err := tools.MemoryGet(1)
	if err != nil {
		t.Fatalf("seeded record missing: %v", err)
	}
	if !strings.Contains(first, "contract gate topic") {
		t.Fatalf("seeded record does not render its topic: %s", first)
	}
	for _, rating := range []string{"useful", "useless"} {
		// A distinct session per rating: one vote per record per task, so the
		// second vote would be deduped rather than recorded.
		ag := &Agent{SessionLog: &SessionLogger{sessionID: "contract-gate-" + rating}}
		if msg := validateToolArgs(toolnames.ToolMemory, feedbackCall("", rating)); msg != "" {
			t.Errorf("rating=%s: documented call shape refused by the validator: %q", rating, msg)
		}
		res := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{
			"action": toolnames.ToolMemoryActionFeedback,
			"id":     float64(1),
			"rating": rating,
			"reason": "contract gate",
		}})
		if strings.HasPrefix(res, "memory error:") || !strings.Contains(res, "rating="+rating) {
			t.Errorf("rating=%s: vote did not land: %s", rating, res)
		}
	}
	hist, err := tools.FeedbackHistory(1)
	if err != nil {
		t.Fatalf("feedback history: %v", err)
	}
	if !strings.Contains(hist, "#1 ") || !strings.Contains(hist, "useful=1.00") || !strings.Contains(hist, "useless=1.00") {
		t.Errorf("ledger does not carry both votes: %s", hist)
	}
}

// TestFeedbackDescriptionsTeachTheWhitelistRule (deliverables 1+2): the
// descriptions must teach the TRUE survival rule — a search pair survives iff
// one of its records was voted useful — and the record-id anchor (#N) the
// required `id` comes from, in place of the retired call-id mechanism.
func TestFeedbackDescriptionsTeachTheWhitelistRule(t *testing.T) {
	mem := toolDefByName(t, toolnames.ToolMemory)
	if mem == nil {
		t.Fatal("memory missing from claudeToolDefs")
	}
	if !strings.Contains(mem.Description, whitelistRuleToken) {
		t.Errorf("memory description never states the whitelist survival rule (%q): a pair survives iff one of its records was voted useful", whitelistRuleToken)
	}
	if !strings.Contains(mem.Description, recordIDToken) {
		t.Errorf("memory description never anchors the required id to the record number (%q)", recordIDToken)
	}
	// The feedback action's own description must state that id is required.
	doc := scopedSchemaDoc(t, toolnames.ToolMemoryActionFeedback)
	if !strings.Contains(doc[memoryDescriptionKey], "id=#N") {
		t.Errorf("memory description's feedback guidance never requires id=#N: %q", doc[memoryDescriptionKey])
	}
}

// ---------------------------------------------------------------------------
// THE NUMERIC-CLAIM DRIFT (the defect this section exists to prevent)
// ---------------------------------------------------------------------------
//
// Measured 2026-09-27: the feedback description stated two NUMBERS about what a
// vote does — "each moves the score ≤0.2" and "≥2 useless votes auto-demote to
// cold (reversible)".
//
//   * The AUTO-DEMOTION claim is stale: no automatic tier transition exists.
//     The merged P0 design replaced it with a suggestion-only verb
//     (`--suggest-cold`, gated on ≥3 decayed useless votes AND zero useful), and
//     the tier column is written only by the explicit update path — measured by
//     voting past the threshold and reading the trail back (TestFeedbackVote-
//     NeverMovesTheTier). The TEXT was the wrong artifact here.
//
//   * The "≤0.2" claim was replaced rather than refuted: the review note that
//     proposed this task justified the claim's falsity with "fbScore max is
//     |0.5| … attainable max ~0.39", which is a statement about the TERM's
//     range, not about what ONE VOTE moves — and it is itself wrong (measured
//     max |fbScore| = 0.4756, approaching fbWeight/2 = 0.5). One vote moves the
//     ranking term by AT MOST 0.1190 (measured over the (useful,useless) grid),
//     which does not exceed 0.2. The description was replaced anyway, because
//     the sentence named neither the term it bounded nor the effect the engine
//     actually reports (the stored-confidence step: +0.05 useful, −0.10
//     useless) — so it could not be checked and hid the larger, real per-vote
//     effect. The NOTE is the wrong artifact here.
//
// The authority is the ENGINE, not this file: every expected token below is
// paired with a LIVE observation through the real surface — the vote result
// prints the step it applied, the cool-down threshold is measured by voting
// until the engine itself says COOLDOWN, and the demotion the engine suggests
// is read from that same result — so a constant change goes red here instead of
// the prompt silently teaching yesterday's engine.

// feedbackSuggestionVerb is the engine's suggestion-only verb as the caller
// invokes it (`gopi memory feedback --suggest-cold`), the flag the description
// must name so a model can reach the engine's only vote-driven cold path.
const feedbackSuggestionVerb = "--suggest-cold"

// coldDemotionFlag is the flag spelling that EXECUTES a cold demotion, as the
// engine's own suggestion text renders it (`memory_update <id> --tier cold`).
const coldDemotionFlag = "--tier cold"

// feedbackSuggestionOnlyToken is the word the description must carry to mark
// that cold path as a SUGGESTION (the machine never writes the tier column):
// "suggest" — the same word the engine's own result text uses.
const feedbackSuggestionOnlyToken = "suggest"

// feedbackStaleNumberClaims is every stale NUMERIC claim the old description
// carried. Each is a claim about what a vote does, and each is contradicted by
// the engine (see the section comment): none may appear where the model reads.
var feedbackStaleNumberClaims = []string{
	"auto-demote to cold", // no automatic tier transition exists (measured below)
	"≤0.2",                // named no term and no effect the engine reports; replaced by the engine's own step numbers
}

// feedbackVote lands ONE vote through the real dispatch path in its own
// session (one vote per record per task is the engine's anti-stacking gate) and
// returns the engine's own result text.
func feedbackVote(t *testing.T, id int, rating, session string) string {
	t.Helper()
	ag := &Agent{SessionLog: &SessionLogger{sessionID: session}}
	res := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolMemory, Arguments: map[string]interface{}{
		"action": toolnames.ToolMemoryActionFeedback,
		"id":     float64(id),
		"rating": rating,
		"reason": "numeric-claim gate",
	}})
	if strings.HasPrefix(res, "memory error:") {
		t.Fatalf("rating=%s: vote did not land: %s", rating, res)
	}
	return res
}

// feedbackObservedField returns the value of one "key=value" field in the
// engine's vote result (the engine's own rendering, not a re-derivation).
func feedbackObservedField(t *testing.T, result, key string) string {
	t.Helper()
	marker := key + "="
	i := strings.Index(result, marker)
	if i < 0 {
		t.Fatalf("the engine's vote result carries no %s field — the description cannot be checked against it: %s", key, result)
	}
	val := strings.TrimSpace(result[i+len(marker):])
	if j := strings.IndexAny(val, " \n"); j >= 0 {
		val = val[:j]
	}
	return val
}

// cooldownVoteCount measures how many useless votes the engine needs before its
// own result says COOLDOWN — the gate the description must state. Measured, not
// read from a constant, so the number in the text and the number in the engine
// cannot drift apart.
func cooldownVoteCount(t *testing.T) (int, string) {
	t.Helper()
	testutil.NewTestDBPath(t)
	if _, err := tools.MemoryStore(
		"cooldown threshold gate topic",
		"how many useless votes before the engine suggests cold?",
		"vote until the engine itself says COOLDOWN — that count is the number the prompt must state",
		"cooldown-threshold-gate", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	const maxVotes = 10
	for n := 1; n <= maxVotes; n++ {
		res := feedbackVote(t, 1, tools.RatingUseless, fmt.Sprintf("cooldown-gate-%d", n))
		if strings.Contains(res, "COOLDOWN") {
			return n, res
		}
	}
	t.Fatalf("no COOLDOWN after %d useless votes: the engine's suggestion gate has moved or vanished", maxVotes)
	return 0, ""
}

// TestFeedbackDescriptionStatesTheEnginesNumbers (deliverables 1+2 of the
// numeric-claim drift): the description must state the numbers the ENGINE
// produces — the stored-confidence step of a vote, for BOTH ratings, and the
// measured cool-down threshold — and must teach the suggestion-only demotion
// instead of the retired automatic one. RED on the pre-change tree, whose text
// states "≤0.2" and "≥2 useless votes auto-demote to cold": arm 1 flags both
// stale claims, and arms 2/3 fail because the engine's numbers appear nowhere.
func TestFeedbackDescriptionStatesTheEnginesNumbers(t *testing.T) {
	doc := scopedSchemaDoc(t, toolnames.ToolMemoryActionFeedback)
	text := doc[memoryDescriptionKey]
	if strings.TrimSpace(text) == "" {
		t.Fatal("memory tool description not found — the gate cannot read the text the model is sent")
	}
	// Arm 1: the stale claims are gone.
	for _, stale := range feedbackStaleNumberClaims {
		if strings.Contains(text, stale) {
			t.Errorf("feedback description still teaches the retired claim %q: the engine does not implement it (see the section comment)", stale)
		}
	}
	// Arm 2: the per-vote confidence step the description states is the step
	// the engine APPLIES, measured per rating (the engine prints it).
	testutil.NewTestDBPath(t)
	if _, err := tools.MemoryStore(
		"confidence step gate topic",
		"what does one vote move?",
		"the engine prints confidence_step — the number the prompt must teach",
		"confidence-step-gate", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	for _, rating := range []string{"useful", "useless"} {
		step := feedbackObservedField(t, feedbackVote(t, 1, rating, "confidence-step-"+rating), "confidence_step")
		magnitude := strings.TrimLeft(step, "+-")
		if !strings.Contains(text, magnitude) {
			t.Errorf("rating=%s: the engine moves the stored confidence by %s, but the feedback description never states %s — the text and the engine disagree",
				rating, step, magnitude)
		}
	}
	// Arm 3: the cool-down threshold the description states is the measured one.
	n, suggestLine := cooldownVoteCount(t)
	if !strings.Contains(text, fmt.Sprintf("%d decayed useless", n)) {
		t.Errorf("the engine suggests cold after %d useless votes, but the feedback description does not state that number: the text and the engine disagree", n)
	}
	// Arm 4: the demotion that DOES exist is taught by its own verbs, and taught
	// as a SUGGESTION (only-up: the machine never writes the tier column).
	if !strings.Contains(text, feedbackSuggestionVerb) {
		t.Errorf("feedback description never names %q: the engine's only vote-driven cold path is a suggestion, and a model told nothing about it cannot act on it", feedbackSuggestionVerb)
	}
	if !strings.Contains(text, feedbackSuggestionOnlyToken) {
		t.Errorf("feedback description does not mark the cold path as a suggestion (%q): the machine never writes the tier column on a vote", feedbackSuggestionOnlyToken)
	}
	if !strings.Contains(suggestLine, coldDemotionFlag) {
		t.Errorf("the engine's own suggestion does not name the flag that executes the demotion (%s): %s", coldDemotionFlag, suggestLine)
	}
}

// TestFeedbackVoteNeverMovesTheTier (the engine half of the claim the
// description had to drop): a useless vote — repeated past the suggestion
// threshold — leaves the tier column exactly as it was, while the engine's own
// result says the demotion is a suggestion naming the flag that executes it.
// This is the observation that makes "auto-demote" a claim about nothing.
func TestFeedbackVoteNeverMovesTheTier(t *testing.T) {
	testutil.NewTestDBPath(t)
	if _, err := tools.MemoryStore(
		"tier-immobility gate topic",
		"does a vote move the tier?",
		"no: the tier column is written only by the explicit update path",
		"tier-immobility-gate", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	before, err := tools.MemoryGet(1)
	if err != nil {
		t.Fatalf("read seeded record: %v", err)
	}
	n, last := cooldownVoteCount(t)
	t.Logf("engine suggested cold after %d useless votes", n)
	if !strings.Contains(last, feedbackSuggestionOnlyToken) {
		t.Errorf("after crossing the suggestion threshold the engine's result does not mark the demotion as a suggestion: %s", last)
	}
	if !strings.Contains(last, coldDemotionFlag) {
		t.Errorf("the engine's suggestion does not name the flag that executes the demotion (%s): %s", coldDemotionFlag, last)
	}
	after, err := tools.MemoryGet(1)
	if err != nil {
		t.Fatalf("re-read record: %v", err)
	}
	beforeTier := memoryRecordTier(t, before)
	afterTier := memoryRecordTier(t, after)
	if beforeTier != afterTier {
		t.Errorf("votes moved the tier column %s → %s: the description must then teach that automatic transition — it does not, and only-up is the contract", beforeTier, afterTier)
	}
}

// memoryRecordTier extracts the tier= field from a rendered record line.
func memoryRecordTier(t *testing.T, rendered string) string {
	t.Helper()
	const marker = "tier="
	i := strings.Index(rendered, marker)
	if i < 0 {
		t.Fatalf("rendered record carries no tier field: %q", rendered)
	}
	rest := rendered[i+len(marker):]
	if j := strings.IndexAny(rest, " \n"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}
