// Package memory — brew pipeline: distill reusable lessons from gopi's own
// session JSONL logs into the embedded distilled knowledge base (M3 of the
// memory roadmap).
//
// Philosophy (hermes 蒸馏哲学): preprocess at WRITE time, never understand at
// READ time. Each session is sliced into conversation chunks, a batch LLM
// call (flash model, thinking disabled — reasoning would eat max_tokens)
// judges whether the chunk holds a lesson worth storing (three-condition
// gate: repeatable pattern / reproducible loop / measurable payoff), and
// surviving lessons are distilled into Q&A rows with 5W1H breadcrumb
// keywords and stored with source="brew" (anti-self-reference: brew output
// is tagged and never re-ingested by a later refill pass).
package tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/thinkmode"
)

// Brew pipeline constants — single place for every tunable (Rule 1/2: no
// magic numbers, env-override defaults live here).
const (
	// BrewModel is the batch-distillation model. Flash tier: distillation is
	// a bulk judgment task, not a reasoning task — thinking:disabled keeps
	// reasoning from eating the completion budget (memroadmap-m3 ④; the
	// consolidate_memory 47%→100% lesson in memory #185).
	BrewModel = "deepseek-v4-flash"

	// brewSourceTag marks every memory this pipeline writes. A future
	// refill/re-backfill pass must skip rows carrying it (③ anti-self-ref).
	BrewSourceTag = "brew"

	// brewMaxTokens per LLM call. A chunk prompt is ~2-4K tokens; the JSON
	// verdict reply is small. With thinking disabled the entire budget is
	// available for content.
	brewMaxTokens = 4096

	// brewThinkingMode: disabled — the hermes lesson (deepseek-v4-flash
	// defaults to thinking ON; reasoning_content consumed the whole
	// max_tokens and content came back empty → JSON parse failure). Points at
	// the shared wire-value constant (Rule 14) rather than re-typing it.
	brewThinkingMode = thinkmode.Disabled

	// brewMaxChunkChars caps each conversation slice sent to the LLM. A
	// 290K-token session must become a handful of bounded prompts, not one
	// oversized one.
	brewMaxChunkChars = 12000

	// brewMaxChunkEvents caps events per chunk on top of the char cap so one
	// giant tool_result line cannot push later events out entirely.
	brewMaxChunkEvents = 40

	// brewChunkPromptReserve reserves chars for the judge prompt template
	// (chunk budget = brewMaxChunkChars - reserve).
	brewChunkPromptReserve = 2500

	// brewMaxLessonChars caps one lesson's evidence excerpt (what the judge
	// sees per candidate lesson).
	brewMaxLessonChars = 4000

	// brewMaxSessions caps a single --dir sweep (cost budget ④: every extra
	// session is extra LLM calls; an accidental "." must not burn the API).
	brewMaxSessions = 64

	// brewMaxLessonsPerSession: one session rarely holds more than a few
	// genuine lessons; beyond this the gate is likely failing.
	brewMaxLessonsPerSession = 3

	// brewMinKeywords: the minimum breadcrumb count for a QA row (5W1H
	// 面包屑哲学 — M1). The LLM must supply mixed 中英 keywords; fewer than
	// this and the row is barely retrievable.
	brewMinKeywords = 6

	// brewMaxKeywordRunes per keyword (M1 lesson #225: dead ASCII breadcrumbs
	// never match; keep them short and mixed-language).
	brewMaxKeywordRunes = 24

	// brewTier / brewConfidence for distilled rows.
	brewTier       = "warm"
	brewConfidence = "0.85"

	// BrewIntervalEnv / BrewDefaultInterval: the session-exit trigger (CLI
	// side owns the state file). The env NAME is declared in internal/config
	// (Rule 14: the leaf package that also owns the catalog row and the
	// `--help-env` entry for this knob), aliased here because the ONE reader
	// (cmd/agent's brew trigger) reaches it through this package.
	BrewIntervalEnv     = config.BrewIntervalEnv
	BrewDefaultInterval = 24 * time.Hour

	// brewLessonType / whenAt defaults written with every row.
	brewLessonType = "pattern"

	// brewReasoningEffortLow: GLM's always-thinking flash refuses
	// thinking:disabled — effort=low is the throttle substitute.
	brewReasoningEffortLow = "low"

	// brewClientTimeout bounds one LLM call. GLM always-thinking flash
	// burns 60-120s per call even at effort=low (measured 2026-08-30); the
	// timeout must cover a full thinking pass + content.
	brewClientTimeout = 180 * time.Second

	// brewTimeoutEnv overrides brewClientTimeout, in seconds (Rule 2: the
	// constant is the default and brewTimeout is the ONE reader). Measured
	// 2026-09-14: a 260s 4-session sweep still lost one distill call to the
	// 180s budget, and the raw "context deadline exceeded" gave the operator
	// no way to tell a slow provider from a bug — the knob plus the
	// budget-carrying error message are how that decision becomes possible.
	brewTimeoutEnv = "GOPI_BREW_TIMEOUT_SEC"

	// brewDistillRepairTries is how many EXTRA distill calls a thin
	// breadcrumb list may buy. Exactly one: a model that returns fewer than
	// brewMinKeywords twice is not going to succeed on the third call, and
	// every extra call costs a full thinking pass (cost budget ④).
	brewDistillRepairTries = 1

	// brewMaxTokensEffortMode: GLM's reasoning_content consumes max_tokens
	// even at reasoning_effort=low (measured: 4096 → all reasoning, empty
	// content, stop=max_tokens). In effort mode the budget doubles so
	// reasoning + JSON content both fit (memory #920: GLM needs ≥8192).
	brewMaxTokensEffortMode = 8192
)

// BrewStats summarizes one brew run — printed by the CLI, asserted by tests.
type BrewStats struct {
	SessionsScanned int      `json:"sessions_scanned"`
	ChunksJudged    int      `json:"chunks_judged"`
	LessonsAccepted int      `json:"lessons_accepted"`
	LessonsRejected int      `json:"lessons_rejected"`
	Stored          int      `json:"stored"`
	Distilled       int      `json:"distilled"`
	Deduped         int      `json:"deduped"`
	Errors          []string `json:"errors,omitempty"`
	ElapsedSec      float64  `json:"elapsed_sec"`
}

// sessionEvent is one line of a session JSONL. Only the fields the slicer
// needs are decoded; everything else is ignored (SessionLogger writes many
// event types — session_start/assistant/request/session_end/injections).
type sessionEvent struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

// sessionMessage mirrors the JSONL message envelope (role + content blocks).
type sessionMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// brewContentBlock is one assistant content block we care about.
type brewContentBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
}

// LoadSessionText renders a session JSONL into a readable transcript:
// user prompts, assistant thinking (trimmed — it often holds the reasoning
// worth distilling), assistant text and tool_use calls. Injection events and
// request-stats lines are skipped. Brew-source events are never present in
// session logs (source is only tagged in the DB), but the anti-self-reference
// guard lives in storeBrewLesson regardless — defense in depth (③).
func LoadSessionText(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("brew: open session: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024) // lines can be large
	for sc.Scan() {
		var ev sessionEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue // best-effort: skip malformed lines
		}
		line := brewRenderEvent(&ev)
		if line == "" {
			continue
		}
		if sb.Len()+len(line) > brewMaxChunkChars*4 { // whole-session cap
			break
		}
		sb.WriteString(line)
	}
	return sb.String(), sc.Err()
}

// brewRenderEvent flattens one JSONL event into transcript text ("" = skip).
func brewRenderEvent(ev *sessionEvent) string {
	if ev.Message == nil {
		return ""
	}
	var msg sessionMessage
	if err := json.Unmarshal(ev.Message, &msg); err != nil {
		return ""
	}
	switch msg.Role {
	case "user":
		return "[user] " + oneLine(brewBlockText(msg.Content)) + "\n"
	case "assistant":
		return brewRenderAssistant(msg.Content)
	}
	return ""
}

// brewRenderAssistant renders an assistant message: thinking (trimmed — the
// reasoning often states the lesson directly), text, and tool_use calls.
func brewRenderAssistant(content json.RawMessage) string {
	var blocks []brewContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ""
	}
	var sb strings.Builder
	for _, b := range blocks {
		switch b.Type {
		case "thinking":
			brewTrimAppend(&sb, "[thinking] ", b.Thinking)
		case "text":
			brewTrimAppend(&sb, "[assistant] ", b.Text)
		case "tool_use":
			sb.WriteString("[tool_use] " + b.Name + "(" + oneLine(string(b.Input)) + ")\n")
		}
	}
	return sb.String()
}

// brewBlockText extracts plain text from a message content that may be a
// JSON string or an array of blocks.
func brewBlockText(content json.RawMessage) string {
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}
	var blocks []brewContentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// brewTrimAppend appends prefix + value flattened to one line, capping the
// value at 400 runes (long tool inputs / thinking chains add noise, not
// signal, for the judge).
func brewTrimAppend(sb *strings.Builder, prefix, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	runes := []rune(value)
	const trimAt = 400
	if len(runes) > trimAt {
		value = string(runes[:trimAt]) + "…"
	}
	sb.WriteString(prefix + oneLine(value) + "\n")
}

// oneLine collapses all whitespace runs (incl. newlines) to single spaces —
// transcript lines must stay one-line-per-event for the chunk slicer.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
