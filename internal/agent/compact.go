package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/thinkmode"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// LLM-assisted context compaction (memory management).
//
// When the conversation approaches the context window, the agent's oldest
// messages are summarized by the LLM into a compact memory block instead of
// being dropped wholesale (TrimToFit). The agent keeps its goal, decisions,
// findings and next steps — a plain trim would silently forget them. The
// recent history stays verbatim (cache-friendly), and the summary replaces
// only the oldest portion, so the context stays bounded without losing
// coherence.
//
// The summarizer call is small (no tools, no thinking, bounded output) and
// runs only when the trigger ratio trips — a few times per very long session
// at most. Its input is bounded too (serializeForSummary truncates each
// message), so the summarizer never pays the full context cost.

// CompactionAnnouncement is the operator-visible record that a compaction
// happened: it is printed (with a 🧠 prefix and the count of messages that went
// into the memory block) at the ONE success return of compactContext, and it is
// the surface the chat status row's own legend points a reader at when they ask
// what "⧉N" counts. It is exported, and therefore a constant here rather than a
// format string typed at both sites, so the legend quotes the line the product
// really prints: TestStatusRowSymbolsHaveAReaderVisibleDefinition matches the
// legend against this string, and a reworded announcement fails that test
// instead of leaving the help quoting a line the product no longer emits.
const CompactionAnnouncement = "memory compaction: summarized %d oldest message(s) into a memory block"

// DefaultCompactTargetRatio is the fraction of the context window the history
// is compacted down to (GOPI_COMPACT_TARGET). Compaction triggers at
// DefaultCompactTriggerRatio; after compaction the estimated size is ≤ this
// ratio (plus the summary block), buying headroom for many more rounds before
// the next compaction. 0.35 (was 0.5) is the safe waterline at the 200K
// deployment window — a compacted context of ~70K.
const DefaultCompactTargetRatio = 0.35

// DefaultCompactCooldown pauses summarizer attempts for this long after a
// summarizer failure (GOPI_COMPACT_COOLDOWN; Hermes' no-provider cooldown).
const DefaultCompactCooldown = 600 * time.Second

// compactRecoveryProbeDelay is the anti-thrash recovery window: once repeated
// ineffective compressions latch the breaker, auto-compaction stays blocked
// this long, then ONE probation probe is allowed (counter drops to 1 so the
// next ineffective pass re-trips immediately). Long enough that a genuinely
// incompressible session does not compact in a loop; short enough that a
// session which has since accumulated real compressible material recovers
// well before hitting the provider's context limit (Hermes
// _ANTI_THRASH_RECOVERY_SECONDS=300 :2982).
const compactRecoveryProbeDelay = 300 * time.Second

// Compaction effectiveness thresholds. A compression that removes less than
// compactEffectiveSavingsPct of the context is INEFFECTIVE (Hermes
// :1200-1209) — it cost an LLM call and bought no headroom. Two consecutive
// ineffective compressions latch the anti-thrash breaker
// (compactIneffectiveStrikes). Both are named because the start-of-round
// pre-check and the post-compaction verdict must judge by the SAME numbers.
const (
	compactEffectiveSavingsPct = 10.0
	compactIneffectiveStrikes  = 2
)

// maxCompactSliverMessages is how few messages a compaction window may take
// before it stops being worth a summarizer call. An anchor floor that leaves
// only a sliver while the estimate stays above the trigger means the summary
// cannot free the pinned region — the livelock shape (see compactContext).
const maxCompactSliverMessages = 2

// compactStructuralNoOpBackoff is how long summarizer attempts defer after a
// structural no-op (no compression window / too few messages). Distinct from
// the ineffective breaker: "nothing to compress right now" must not strike the
// anti-thrash guard (a short session would otherwise permanently disarm
// auto-compaction), it just defers the scan so a can-never-shrink transcript
// does not re-fire every turn. Compaction resumes automatically once the
// backoff lapses or the window outgrows it (Hermes
// _STRUCTURAL_NO_OP_BACKOFF_SECONDS=300 :2993).
const compactStructuralNoOpBackoff = 300 * time.Second

// compactPinnedAnchorWarning is the once-per-session operator warning for a
// context that compaction can no longer reach: an anchor floor pins the
// window, the estimate stays above the trigger, and every later round refuses
// silently. It names the pinned anchor (anchorStopLastUser /
// anchorStopLastVisibleAssistant) because that is what the operator has to act
// on — a new instruction re-anchors the run, and the ephemeral anchor floor is
// not otherwise inferable from the repeating per-round log line. Once per
// session: the state is sticky, so repeating it every round would be noise.
const compactPinnedAnchorWarning = "compaction is PINNED: the "

// warnPinnedAnchor logs the pinned-anchor state at most once per session.
func (a *Agent) warnPinnedAnchor(taken int) {
	if a == nil || a.compactPinnedAnchorWarned || a.Logger == nil {
		return
	}
	a.compactPinnedAnchorWarned = true
	a.Logger.Printf("⚠️ %s%s anchor pins the compressible window at %d message(s) and the context stays above the compaction target — compaction cannot free this region; the anchor is the newest user instruction (a new operator message re-anchors it). Trimming continues, so the session stays bounded.",
		compactPinnedAnchorWarning, a.Ctx.AnchorStop, taken)
}

// maxCompactAnchorSliverMessages is how few messages an ANCHOR-pinned
// compaction may take before the pin itself is worth telling the operator
// about, if it leaves the context above the compaction trigger. The field
// session's shape (one message behind the head, freeing a couple of percent)
// is the case that must not be silent: the context stays over the limit for
// the rest of the run while every round looks like a normal compaction.
const maxCompactAnchorSliverMessages = 1

// defaultMaxSummaryTokens caps the summarizer's output budget: min(5% of the
// window, 12000) tokens, overridable via GOPI_COMPACT_MAX_SUMMARY_TOKENS.
// Replaces the fixed 2048-token cap — at the 200K window that is 10000.
func defaultMaxSummaryTokens(window int) int {
	n := window / 20 // 5%
	if n > 12000 {
		n = 12000
	}
	return n
}

// summaryInputMaxChars caps the total summarizer input (serialized excerpt)
// so a compression window with hundreds of messages cannot assemble a
// multi-hundred-KB prompt that slow backends time out on. Per-message
// truncation alone is not enough (Hermes _SUMMARY_INPUT_MAX_CHARS=160_000:
// ≈40K tokens ≈ the practical aux-model bound). Exceeding the cap keeps the
// beginning head (task setup) and the recent tail (current state), dropping
// the explicit middle with an omission marker.
const summaryInputMaxChars = 160_000

// anchorHeading heads the mechanically-extracted "Anchor Index" block appended
// to the LLM summary. LLM summarization at 10:1 always loses needle-facts
// (SHAs, exact file paths, error strings, version numbers, URLs) — the
// deterministic regex harvest below preserves them for the agent's memory and
// memory_search retrieval. Zero LLM cost, nothing can be paraphrased away
// (Hermes _LEAN_ANCHOR_HEADING / _build_anchor_index :1019-1065).
const anchorHeading = "## Anchors (mechanically extracted, exact)"

// anchorBudgetChars caps the total Anchor Index block so it stays a compact
// tail, not a concurrent summary (Hermes _LEAN_ANCHOR_BUDGET_CHARS=7000).
const anchorBudgetChars = 4000

// anchorNoise excludes identifiers that appear in every transcript (the
// session owner handle etc.) so they do not crowd out load-bearing anchors.
var anchorNoise = map[string]bool{
	"@gopi": true,
	"@user": true,
}

// anchorCategory pairs a label, its regex and the per-category cap of how many
// distinct anchors to keep. Within a category, most-frequent first (frequency
// proxies load-bearing-ness), ties broken by last-seen order (recency).
// Mirrors Hermes _ANCHOR_PATTERNS (:1004-1017).
var anchorCategories = []struct {
	label string
	re    *regexp.Regexp
	cap   int
}{
	{"commits", regexp.MustCompile(`\b[0-9a-f]{7,40}\b`), 40},
	{"files", regexp.MustCompile(`\b[\w./-]+/[\w.-]+\.(?:go|py|ts|tsx|js|rs|md|yaml|yml|json|toml|sh|mod|sum)\b`), 80},
	{"errors", regexp.MustCompile(`\b(?:[A-Z][a-zA-Z]*Error|Exception|panic:|fatal error:|ENOSPC|EACCES|SIGKILL|Traceback|exit code \d+)[^\n]{0,90}`), 40},
	{"versions", regexp.MustCompile(`\bv?\d+\.\d+(?:\.\d+)?(?:[-+][0-9A-Za-z.-]+)?\b`), 40},
	{"urls", regexp.MustCompile(`https?://[^\s)"']{10,120}`), 30},
	{"handles", regexp.MustCompile(`@[A-Za-z0-9-]{3,30}\b`), 40},
}

// buildAnchorIndex mechanically harvests exact identifiers from the messages
// being compressed and renders them as a bounded "## Anchors" block. It is the
// deterministic bedrock under the LLM summary — the facts the model would
// paraphrase away survive verbatim. Returns "" when nothing is found.
func buildAnchorIndex(msgs []deepseek.Message) string {
	var textParts []string
	for _, m := range msgs {
		if m.Content != "" {
			textParts = append(textParts, m.Content)
		}
		for _, tc := range m.ToolCalls {
			if tc.Name != "" {
				textParts = append(textParts, tc.Name)
			}
			if tc.RawArgs != "" {
				textParts = append(textParts, tc.RawArgs)
			}
		}
	}
	if len(textParts) == 0 {
		return ""
	}
	text := strings.Join(textParts, "\n")

	var sections []string
	used := 0
	for _, cat := range anchorCategories {
		counts := make(map[string]int)
		lastSeen := make(map[string]int)
		for n, m := range cat.re.FindAllStringSubmatch(text, -1) {
			val := strings.TrimSpace(m[0])
			val = strings.TrimRight(val, ".,;:")
			if val == "" || anchorNoise[val] {
				continue
			}
			counts[val]++
			lastSeen[val] = n
		}
		if len(counts) == 0 {
			continue
		}
		type kv struct {
			v string
			c int
			n int
		}
		ranked := make([]kv, 0, len(counts))
		for v, c := range counts {
			ranked = append(ranked, kv{v, c, lastSeen[v]})
		}
		sort.Slice(ranked, func(i, j int) bool {
			if ranked[i].c != ranked[j].c {
				return ranked[i].c > ranked[j].c
			}
			return ranked[i].n > ranked[j].n
		})
		if len(ranked) > cat.cap {
			ranked = ranked[:cat.cap]
		}
		parts := make([]string, 0, len(ranked))
		for _, k := range ranked {
			if k.c > 1 {
				parts = append(parts, fmt.Sprintf("%s(x%d)", k.v, k.c))
			} else {
				parts = append(parts, k.v)
			}
		}
		line := cat.label + ": " + strings.Join(parts, ", ")
		if used+len(line) > anchorBudgetChars {
			break
		}
		sections = append(sections, line)
		used += len(line)
	}
	if len(sections) == 0 {
		return ""
	}
	return "\n\n" + anchorHeading + "\n" + strings.Join(sections, "\n")
}

// boundSummaryInput caps the total serialized summarizer input while keeping
// the beginning (task setup) and the recent tail (current state), marking the
// omitted middle explicitly so the summarizer knows context was intentionally
// pruned before it saw the prompt (Hermes _bound_summary_input :4591-4623).
func boundSummaryInput(content string) string {
	return boundSummaryInputMax(content, summaryInputMaxChars)
}

// boundSummaryInputMax is the pure cap logic behind boundSummaryInput, split
// out so tests can exercise it with a small cap. Keeps head (45%) + tail
// (55%) of the budget, explicitly marking the omitted middle.
func boundSummaryInputMax(content string, max int) string {
	if max <= 0 {
		max = summaryInputMaxChars
	}
	if len(content) <= max {
		return content
	}
	markerTemplate := "\n\n...[summary input capped: omitted %d chars from the middle to keep the prompt bounded]...\n\n"
	// The omitted-count in the marker is derived from the provisional budget
	// (max minus the unexpanded marker length) before the real head+tail are
	// recomputed below with the marker's final length subtracted. This mirrors
	// the original two-pass logic (the old first pass recomputed remaining /
	// headChars / tailChars pointlessly — only 'omitted' feeding the marker's
	// %d actually mattered, so it is computed directly here).
	provisional := max - len(fmt.Sprintf(markerTemplate, len(content)))
	if provisional < 0 {
		provisional = max
	}
	omitted := len(content) - provisional
	marker := fmt.Sprintf(markerTemplate, omitted)
	remaining := max - len(marker)
	if remaining < 0 {
		marker = ""
		remaining = max
	}
	headChars := int(float64(remaining) * 0.45)
	tailChars := remaining - headChars
	tail := ""
	if tailChars > 0 {
		tail = strings.TrimLeft(content[len(content)-tailChars:], " \n")
	}
	out := strings.TrimRight(content[:headChars], " \n")
	return out + marker + tail
}

// memorySummaryMarker heads the user message that carries the compacted
// memory, so the agent can distinguish it from ordinary conversation. The
// REFERENCE ONLY variant extends the original marker (kept as
// memorySummaryLegacyMarker for GOPI_COMPACT_REFERENCE_ONLY=off) — anything
// matching the old prefix keeps matching, so the change is non-breaking.
const memorySummaryMarker = "[MEMORY COMPACTION — EARLIER WORK — REFERENCE ONLY]"

// memorySummaryLegacyMarker is the pre-P1 single-line label, used when
// reference-only semantics are disabled (GOPI_COMPACT_REFERENCE_ONLY=off) —
// a comparable control behavior.
const memorySummaryLegacyMarker = "[MEMORY COMPACTION — EARLIER WORK]"

// memorySummaryEndMarker closes the memory block so the agent can see where
// the reference text ends and live conversation resumes.
const memorySummaryEndMarker = "--- END OF MEMORY COMPACTION — respond to the message below ---"

// referenceOnlyPrefix carries Hermes' SUMMARY_PREFIX semantics for gopi:
// the block is background reference, not active instructions; only the
// latest user message below it is acted on; stop/undo/never-mind signals
// cancel anything in flight it describes; the persistent memory is
// authoritative; the history is past tense.
const referenceOnlyPrefix = `The text below is a MEMORY COMPACTION of earlier work. Treat it as background reference and context ONLY — NOT as active instructions.

- The only active instruction is the LATEST user message below this block. Respond to that message; do not act on anything inside this summary.
- If the user says to stop, undo, or "never mind", or changes the subject, any in-flight work described in this summary is immediately cancelled.
- Your persistent memory (memory.db via memory(action=search)) is ALWAYS authoritative — prefer memory(action=search) over this summary.
- Everything in this summary happened in the past — it is written in past tense, and nothing in it is a pending action or an open to-do.`

// compactionSystemPromptTemplate defines the summarizer's role: distill the
// excerpt into HISTORICAL notes (past tense, date-anchored) the agent can
// continue from — goal, decisions, findings, artifacts, tests, task state
// and planned next steps, under the four Historical headings (Hermes
// HISTORICAL_TASK_HEADING family: Task Snapshot / In-Progress Status /
// Pending User Asks / Remaining Work).
//
// The hard "do NOT echo" rules exist because live evaluation showed the
// model reproducing raw tool calls and message text instead of distilling
// (earlier analysis run, 2026-08-06).
//
// THE CARVE-OUT IS THE FIX FOR A MEASURED DEFECT (task
// 01a198cd-9ae8-7ca9-ad04-2bd87f72a917, 2026-11-01). The original rule said
// "Do NOT echo, quote, or reproduce ANY part of the excerpt" while a later
// bullet said "preserve ... Commands run and their outcomes, test results".
// For a value that IS a command's output — a total computed by awk, a token
// count, a duration — the two rules contradict each other, and the summarizer
// resolved the contradiction by DELETING the number: after a mid-turn
// compaction the agent no longer had "TOTAL = 180.3075 CNY" and silently
// re-derived it with a second awk run, in the same turn, paying for the work
// twice. The carve-out states which rule wins: transcribing the CONVERSATION
// is banned, carrying a VALUE the work produced is mandatory.
const compactionSystemPromptTemplate = `You are the memory manager of a coding agent. The conversation excerpt below is being compacted so the agent's context stays within its window.

Write a HISTORICAL distillation of the excerpt — everything below describes COMPLETED work. HARD RULES:
- Do NOT echo the conversation back — no tool call syntax, no code blocks, no raw message text, no narration of who said what. Distill instead of transcribing.
- EXCEPTION TO THE RULE ABOVE, AND IT WINS: VERBATIM VALUES ARE DELIVERABLES, NOT ECHO. Every exact value the work produced — a number a command computed (a total, a percentage, a token count, a duration), a file or directory path, an identifier, an error string, a commit hash, a version — MUST appear in your notes CHARACTER-FOR-CHARACTER as it appeared, together with the one-line label that makes it mean something (e.g. "TOTAL = 180.3075 CNY", not "a total was computed"). Quoting such a value is NOT "reproducing the excerpt": it is the only thing that lets the agent continue without re-running the command that produced it. Dropping it is the single most expensive mistake you can make — the agent will silently redo the work. A number rounded, paraphrased, or summarized away counts as dropped.
- Do NOT mention that tools were called; capture only what was learned, plus the verbatim values above.
- Keep it to 300-500 words of plain bullet notes. The verbatim values above do NOT count against that budget — never drop one to save words; drop your own adjectives instead.
- Completed actions are PAST-TENSE FACTS with the date they happened (today is %s).
- Next steps are recorded as "historical plans" (planned but not yet executed), NOT as an active to-do list.

Preserve everything needed to continue the task seamlessly:
- Historical Task Snapshot: the overall goal and the current stage of work as it was
- Historical In-Progress Status: key decisions and their rationale (past tense facts)
- Historical Pending User Asks: user requests that were pending
- Historical Remaining Work: planned next steps as history (e.g. "Next planned: ...")
- Findings, file paths, files created/modified, and their state
- Commands run and their outcomes, test results

The agent has NO other memory of this excerpt — your notes are all it gets.`

// compactionSystemPrompt injects the current date so completed actions get
// an absolute time anchor (Hermes :1747-1758).
func compactionSystemPrompt() string {
	return fmt.Sprintf(compactionSystemPromptTemplate, time.Now().Format("2006-01-02"))
}

// compactConfig carries the compaction parameters resolved from the
// environment in NewAgent (all GOPI_COMPACT_*).
type compactConfig struct {
	TriggerRatio     float64
	TargetRatio      float64
	KeepRecent       int
	MaxSummaryTokens int
	Cooldown         time.Duration
	ReferenceOnly    bool
}

// compactConfigFromEnv resolves the compaction parameters, mirroring the
// other GOPI_* parses in NewAgent: read the env var, warn on invalid values,
// fall back to the default. The max summary budget defaults dynamically to
// min(5% of the window, 12000) unless explicitly set.
func compactConfigFromEnv(window int) compactConfig {
	cfg := compactConfig{
		TriggerRatio:     DefaultCompactTriggerRatio,
		TargetRatio:      DefaultCompactTargetRatio,
		KeepRecent:       DefaultCompactKeepRecent,
		MaxSummaryTokens: defaultMaxSummaryTokens(window),
		Cooldown:         DefaultCompactCooldown,
		ReferenceOnly:    true,
	}
	cfg.TriggerRatio = envFloat("GOPI_COMPACT_TRIGGER", DefaultCompactTriggerRatio, cfg.TriggerRatio, func(f float64) bool { return f > 0 && f < 1 })
	cfg.TargetRatio = envFloat("GOPI_COMPACT_TARGET", DefaultCompactTargetRatio, cfg.TargetRatio, func(f float64) bool { return f > 0 && f < 1 })
	cfg.KeepRecent = envInt("GOPI_COMPACT_KEEP_RECENT", DefaultCompactKeepRecent, cfg.KeepRecent, func(n int) bool { return n >= 0 })
	cfg.MaxSummaryTokens = envInt("GOPI_COMPACT_MAX_SUMMARY_TOKENS", cfg.MaxSummaryTokens, cfg.MaxSummaryTokens, func(n int) bool { return n > 0 })
	cfg.Cooldown = time.Duration(envInt("GOPI_COMPACT_COOLDOWN", int(DefaultCompactCooldown/time.Second), cfg.Cooldown, func(n int) bool { return n >= 0 })) * time.Second
	if v := os.Getenv("GOPI_COMPACT_REFERENCE_ONLY"); v != "" {
		if strings.EqualFold(v, "off") || v == "0" || strings.EqualFold(v, "false") {
			cfg.ReferenceOnly = false
		}
	}
	return cfg
}

// truncateMiddle caps a string at max runes, keeping the first head runes and
// the last tail runes (tail 0 keeps no tail and appends "...").
func truncateMiddle(s string, max, head, tail int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return elideRunes(r, head, tail, middleEllipsis)
}

// middleEllipsis is the marker truncateMiddle splices between the kept head and
// tail — the repository's one spelling of "text was cut out of the middle here".
const middleEllipsis = "\n...[truncated]...\n"

// elideAtBytes is elideRunes' SHAPE expressed in BYTE offsets, for a caller that
// already resolved them. It exists because converting a megabyte-string to
// []rune to slice it costs two allocations and two full copies: measured on this
// host, a 560 KB tool result spent ~1.3 s in that conversion alone, more than
// the excerpt's own token count. The rune-offset arithmetic lives in ONE place
// (runeOffsets) and both functions share this splicing code.
func elideAtBytes(s string, headByte, tailByte int, marker string) string {
	if headByte < 0 {
		headByte = 0
	}
	if headByte > len(s) {
		headByte = len(s)
	}
	if tailByte < headByte {
		tailByte = headByte
	}
	if tailByte > len(s) {
		tailByte = len(s)
	}
	if tailByte == len(s) && headByte == 0 {
		return s
	}
	if tailByte >= len(s) {
		return s[:headByte] + "..."
	}
	return s[:headByte] + marker + s[tailByte:]
}

// runeOffsets resolves "keep the first head and the last tail RUNES" into byte
// offsets in ONE pass, with no rune slice allocated.
func runeOffsets(s string, head, tail int) (headByte, tailByte int) {
	if head < 0 {
		head = 0
	}
	if tail < 0 {
		tail = 0
	}
	// Head: advance `head` runes from the start.
	i, seen := 0, 0
	for i < len(s) && seen < head {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		seen++
	}
	headByte = i
	// Tail: walk back `tail` runes from the end.
	j, seen := len(s), 0
	for j > headByte && seen < tail {
		_, size := utf8.DecodeLastRuneInString(s[:j])
		j -= size
		seen++
	}
	return headByte, j
}

// elideRunes is the SHAPE both middle-elision sites use: keep the first head
// runes, keep the last tail runes, splice marker between them. It takes the
// rune slice because the caller has already converted (it needed the length);
// converting inside would walk the string a second time.
//
// IT EXISTS SO A SECOND SITE REUSES THIS SHAPE instead of growing its own
// off-by-one: the tool-result token cap (toolresult_cap.go) needs the same
// elision but must say WHERE the dropped text can be recovered, so the marker
// is a parameter rather than a constant baked into the function body.
func elideRunes(r []rune, head, tail int, marker string) string {
	if head < 0 {
		head = 0
	}
	if head > len(r) {
		head = len(r)
	}
	if tail < 0 {
		tail = 0
	}
	if tail > len(r)-head {
		tail = len(r) - head
	}
	if tail == 0 {
		return string(r[:head]) + "..."
	}
	return string(r[:head]) + marker + string(r[len(r)-tail:])
}

// serializeForSummary renders the excerpt for the summarizer as a single
// bounded text block, so the summarizer input is O(compaction) instead of
// O(context) (Hermes context_compressor input truncation). Per message:
// content is capped at 6000 chars (head 4000 + tail 1500), tool-call
// arguments at 1500 chars (head 1200). The truncation applies only to the
// copy sent to the summarizer — Ctx.Messages is never rewritten (the
// wire-safe primitive is whole-message deletion, not in-message edits).
func serializeForSummary(msgs []deepseek.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		// SELF-RECORDING: a self-recording note block goes into the summarizer's
		// input VERBATIM. It is not distilled material — the model already wrote
		// the one-line distillation itself — so handing it over as-is costs nothing
		// the model has not already paid, and it has two properties no excerpt has:
		// the note carries exact identifiers (paths, SHAs, error strings) and it is
		// written in past tense with a turn number.
		//
		// What it must NOT be is replaced by a placeholder. The placeholder version
		// of this branch asked the summarizer about a history from which the one
		// lossless thing in it had been deleted, so the memory block was a summary
		// of the lossy material alone, and the record arrived at the model through a
		// SECOND channel (the carrier) as a separate message. Two channels for one
		// event, and the summary's own account of the round said nothing about it.
		// Passing it through makes the summarizer's word-for-word rule (see
		// compactionSystemPromptTemplate) apply to the block like any other value.
		b.WriteString("[" + m.Role + "]: ")
		b.WriteString(truncateMiddle(m.Content, 6000, 4000, 1500))
		for _, tc := range m.ToolCalls {
			args := tc.RawArgs
			if j, err := json.Marshal(tc.Arguments); err == nil {
				args = string(j)
			}
			b.WriteString(" [tool_call " + tc.Name + "(" + truncateMiddle(args, 1500, 1200, 0) + ")]")
		}
		if m.ToolCallID != "" {
			b.WriteString(" [tool_call_id=" + m.ToolCallID + "]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// summarizeMessages compacts the given history excerpt into a memory block
// via a single non-streaming Claude call (no tools, no thinking — cheap).
// The excerpt is serialized and truncated to a bounded block
// (serializeForSummary) and sent as one user message; the output budget
// scales with the excerpt — max(2000, 20% of the serialized length), capped
// at CompactMaxSummaryTokens — with 1.3x headroom in max_tokens.
func (a *Agent) summarizeMessages(ctx context.Context, msgs []deepseek.Message) (string, error) {
	serialized := serializeForSummary(msgs)
	// Cap total summarizer input so a many-message window cannot assemble a
	// huge prompt: keep the head and recent tail, mark the omitted middle.
	serialized = boundSummaryInput(serialized)
	budget := int(float64(len(serialized)) * 0.20)
	if budget < 2000 {
		budget = 2000
	}
	if maxSummary := a.CompactMaxSummaryTokens; maxSummary > 0 && budget > maxSummary {
		budget = maxSummary
	}
	maxTokens := int(float64(budget) * 1.3)
	req := deepseek.BuildClaudeRequest(
		a.Model,
		[]deepseek.Message{{Role: "user", Content: serialized}},
		compactionSystemPrompt(),
		"",
		nil,
		maxTokens,
		&deepseek.Thinking{Type: thinkmode.Disabled},
	)
	resp, err := a.Client.ClaudeChat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("memory compaction: %w", err)
	}
	if a.Cost != nil {
		a.Cost.AddClaudeUsage(resp.Usage)
	}
	summary := resp.Text()
	if len(summary) == 0 {
		return "", fmt.Errorf("memory compaction: empty summary")
	}
	// Deterministic bedrock: mechanically harvest exact identifiers (SHA,
	// file paths, error strings, versions, URLs) from the compressed region
	// and append them as a compact Anchor Index, so needle-facts the LLM
	// 10:1 summary would paraphrase away survive verbatim for memory_search
	// and the agent's recovery.
	if anchors := buildAnchorIndex(msgs); anchors != "" {
		summary += anchors
	}
	return summary, nil
}

// isMemorySummaryMessage reports whether the message is a compaction memory
// block (either the reference-only or the legacy marker).
func isMemorySummaryMessage(m deepseek.Message) bool {
	return strings.HasPrefix(m.Content, memorySummaryMarker) ||
		strings.HasPrefix(m.Content, memorySummaryLegacyMarker)
}

// memorySummaryMessage builds the user message that carries the memory block:
// marker + REFERENCE ONLY prefix + summary + end marker when reference-only
// semantics are on (default), or the legacy single-line marker without the
// prefix/end marker when off (GOPI_COMPACT_REFERENCE_ONLY=off).
func (a *Agent) memorySummaryMessage(summary string) deepseek.Message {
	if a.CompactReferenceOnly {
		return deepseek.Message{Role: "user",
			Content: memorySummaryMarker + "\n" + referenceOnlyPrefix + "\n" + summary + "\n\n" + memorySummaryEndMarker}
	}
	return deepseek.Message{Role: "user", Content: memorySummaryLegacyMarker + "\n" + summary}
}

// protectedStoreRounds marks memory_store distillation rounds — the assistant
// message carrying the memory_store tool call plus its trailing tool results
// — as protected, so compaction never consumes the distilled lesson or its
// memory id pointer (the tool_use input IS the distillation, ~1.25K chars
// with topic/keywords; the result carries the retrieval id). Identification
// is by tool name only: memory_store's schema forces topic/question/answer
// distillation, so every call is a distillation round by construction — no
// result-string parsing needed.
func (a *Agent) protectedStoreRounds() map[int]bool {
	if a.Ctx == nil {
		return nil
	}
	msgs := a.Ctx.Messages
	var protected map[int]bool
	for i, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		isStore := false
		for _, tc := range m.ToolCalls {
			// Action-aware (tool-family merge, memory family): the store
			// round is the merged `memory` tool with action "store" (formerly
			// the standalone memory_store tool).
			if tc.Name == toolnames.ToolMemory {
				if action, _ := tc.Arguments["action"].(string); action == toolnames.ToolMemoryActionStore {
					isStore = true
					break
				}
			}
		}
		if !isStore {
			continue
		}
		if protected == nil {
			protected = make(map[int]bool)
		}
		protected[i] = true
		for j := i + 1; j < len(msgs) && msgs[j].Role == "tool"; j++ {
			protected[j] = true
		}
	}
	return protected
}

// compactContext replaces the oldest messages with an LLM-generated memory
// block when the estimated size exceeds the compaction target. Returns true
// when a compaction happened. On LLM failure the oldest messages stay
// removed (equivalent to a plain trim), a failure cooldown starts, and false
// is returned. Tracks compression effectiveness for the anti-thrash guard.
func (a *Agent) compactContext(ctx context.Context) bool {
	if a.ContextWindow <= 0 || a.Ctx == nil {
		return false
	}
	targetRatio := a.CompactTargetRatio
	if targetRatio <= 0 {
		targetRatio = DefaultCompactTargetRatio
	}
	target := int(float64(a.ContextWindow) * targetRatio)
	// beforeHeuristic is the pre-compaction estimate on the heuristics-only
	// basis: the post-compaction verdict has to compare like with like (the
	// API measurement is reset by the compaction itself).
	beforeHeuristic := a.Ctx.heuristicTokens()
	// limitTokens is deliberately NOT recomputed here: the gate that reached
	// this function is trimContext's own `before > limit` test on the measured
	// basis, and a mid-mutation re-derivation would compare across bases (the
	// mixed-basis defect the livelock guard used to carry).

	protected := a.protectedStoreRounds()
	oldest := a.Ctx.TakeOldest(target, a.CompactKeepRecent, protected)
	if len(oldest) == 0 && len(protected) > 0 {
		// Protection (plus keep-recent/anchors) consumed the whole
		// compressible zone: retry without the store-round protection so a
		// store-heavy session cannot wedge compaction into a no-op loop.
		if a.Logger != nil {
			a.Logger.Printf("⚠️ store rounds protected everything — compressing store rounds")
		}
		oldest = a.Ctx.TakeOldest(target, a.CompactKeepRecent, nil)
	}
	if len(oldest) == 0 {
		// No compression window — nothing to compress right now. This is a
		// structural no-op, NOT an ineffective attempt: a short session with
		// too little compressible material must not strike the anti-thrash
		// breaker (that would permanently disarm auto-compaction even after
		// it later grows real material, Hermes #93022). Defer retries for
		// the structural no-op backoff so a can-never-shrink transcript does
		// not re-fire the scan every turn, and resume automatically when the
		// backoff lapses or the window outgrows it.
		//
		// OPERATOR VISIBILITY (option (c) of the anchor-trigger fix): with
		// every harness injection exempt from the instruction floor, an empty
		// window stopped BY AN ANCHOR FLOOR means the operator's own
		// instruction (or the last visible answer) sits at the head — the
		// session can never compact again, and every later round silently
		// refuses. One loud warning per session names the pinned anchor so the
		// state is visible instead of inferred from a repeating
		// "anchored at …" line.
		if a.Ctx.AnchorStop != "" {
			a.warnPinnedAnchor(len(oldest))
		}
		a.compactStructuralNoOpUntil = time.Now().Add(compactStructuralNoOpBackoff)
		return false
	}
	// LIVELOCK GUARD (the reported cycle): an ANCHOR floor stopped the deletion
	// (AnchorStop set — the loop wanted to keep deleting and could not) and what
	// it managed to take is only a SLIVER (maxCompactSliverMessages) whose
	// removal is INEFFECTIVE by this package's own definition — it frees less
	// than compactEffectiveSavingsPct of the context. Such a compaction cannot
	// converge: the block the summarizer prepends lands at index 0, the block
	// from the previous compaction moves to index 1 and the anchors sit right
	// behind it — the next round re-enters with the identical pinned window.
	// That is a fixed point (field session: "anchored at last user" /
	// "summarized 1 oldest message(s)" / "assertion: after ≤ target+summary =
	// false", repeating with the same numbers 157995 → 118285 → 118616 →
	// 119145 …, 305 "anchored" rounds and 305 "summarized 1" rounds in one
	// session — the two counts equal, i.e. every round re-summarized, one
	// summarizer round-trip each).
	//
	// The verdict is deliberately expressed as a SHARE of the context measured
	// on ONE basis (the chars/4 heuristic, available before and after the
	// deletion). An earlier version of this guard asked the other question —
	// "is EstimatedTokens() still above the trigger?" — and never fired on the
	// field shape: TakeOldest's deletion invalidates the API measurement's
	// index, so EstimatedTokens() silently fell back to the heuristic (118285)
	// while the trigger had fired on the measured count (157995), and the
	// comparison came out below the limit. Same defect class as the mixed-basis
	// savings verdict below: never compare numbers from two bases.
	//
	// Such a window is treated as a STRUCTURAL NO-OP: no summarizer call, the
	// sliver is put back (the context is restored exactly as it was), the
	// existing backoff is armed, and the deterministic trim (TrimToFit has no
	// anchor floors, so it CAN reach the pinned region) bounds the context
	// instead — cheap, and bounded.
	//
	// Convergence: any window that is NOT a sliver, or whose removal frees at
	// least compactEffectiveSavingsPct of the context, keeps the normal path —
	// and such a pass shrinks the context by ≥ that share, so the estimate
	// strictly decreases until the context reaches the target or the remaining
	// window becomes an ineffective sliver, at which point this guard takes
	// over. The cycle is therefore terminated, not merely deferred.
	if len(oldest) <= maxCompactSliverMessages && a.Ctx.AnchorStop != "" {
		removedPct := 0.0
		if beforeHeuristic > 0 {
			afterDeletionHeuristic := a.Ctx.heuristicTokens()
			removedPct = (float64(beforeHeuristic) - float64(afterDeletionHeuristic)) / float64(beforeHeuristic) * 100
		}
		if removedPct < compactEffectiveSavingsPct {
			// An ANCHOR floor that leaves one message while the context stays
			// over the limit is the pinned state the operator cannot see from
			// a repeating per-round line: name it once per session. (A
			// memory-block floor is the merged livelock guard's own case — it
			// reports itself below and is a normal, converging stop.)
			if len(oldest) <= maxCompactAnchorSliverMessages && a.Ctx.AnchorStop != anchorStopMemoryBlock {
				a.warnPinnedAnchor(len(oldest))
			}
			if a.Logger != nil {
				a.Logger.Printf("🛡️ compaction skipped — only %d message(s) compressible (%s floor pins the window) freeing %.1f%% (< %.0f%%) of the context — trimming instead, no summarizer call (livelock guard)",
					len(oldest), a.Ctx.AnchorStop, removedPct, compactEffectiveSavingsPct)
			}
			a.Ctx.Messages = append(oldest, a.Ctx.Messages...)
			a.Ctx.AnchorStop = ""
			a.compactStructuralNoOpUntil = time.Now().Add(compactStructuralNoOpBackoff)
			return false
		}
	}
	if a.Ctx.AnchorStop != "" && a.Logger != nil {
		a.Logger.Printf("🛡️ compaction anchored at %s — keeping est above target rather than losing the task anchor", a.Ctx.AnchorStop)
	}
	summary, err := a.summarizeMessages(ctx, oldest)
	if err != nil {
		cooldown := a.CompactCooldown
		if cooldown <= 0 {
			cooldown = DefaultCompactCooldown
		}
		a.compactFailureCooldownUntil = time.Now().Add(cooldown)
		if a.Logger != nil {
			a.Logger.Printf("⚠️ memory compaction failed (%v) — pausing summary attempts for %v", err, cooldown)
		}
		return false
	}
	a.compactFailureCooldownUntil = time.Time{}
	a.Ctx.Messages = append([]deepseek.Message{a.memorySummaryMessage(summary)}, a.Ctx.Messages...)
	// The API measurement that calibrated EstimatedTokens covered the old,
	// larger context — it must be invalidated or the next round re-triggers
	// on the stale count (Hermes model-switch reset, same semantics).
	a.Ctx.PromptTokens = 0
	a.Ctx.PromptTokensIndex = 0
	// The effectiveness verdict MUST compare numbers from the same basis.
	// PromptTokens was just reset, so EstimatedTokens() would fall back to the
	// chars/4 heuristic and the post-compaction estimate could come out LOWER
	// than the pre-compaction (measured) count even when the context actually
	// GREW — reporting phantom "savings" for a compression that made things
	// worse (field log: before=119145 measured → after=118285 heuristic
	// "saved 0.7%"). Both sides of the verdict are therefore computed on the
	// heuristic, which is available on both sides of the reset.
	if beforeHeuristic > 0 {
		afterHeuristic := a.Ctx.heuristicTokens()
		pct := (float64(beforeHeuristic) - float64(afterHeuristic)) / float64(beforeHeuristic) * 100
		a.lastCompactSavingsPct = pct
		if pct < compactEffectiveSavingsPct {
			a.compactIneffectiveCount++
			// Arm the recovery probe the moment the breaker latches so the
			// loop can resume once the window elapses. (A successful probe
			// resets the count; an ineffective one keeps/re-trips it — the
			// probe logic in compactionPaused manages the drop-to-1.)
			if a.compactIneffectiveCount >= compactIneffectiveStrikes && a.compactRecoveryProbeAt.IsZero() {
				a.compactRecoveryProbeAt = time.Now().Add(compactRecoveryProbeDelay)
			}
		} else {
			a.compactIneffectiveCount = 0
			a.compactRecoveryProbeAt = time.Time{}
		}
	}
	if a.Logger != nil {
		a.Logger.Printf(CompactionAnnouncement+"\n", len(oldest))
	}
	// The compression counter the status row reads: incremented HERE, at the one
	// place a compaction actually happened, so the count cannot include an
	// attempt that a guard refused or a window that was structurally empty.
	a.CompactCount++
	return true
}
