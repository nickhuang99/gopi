package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/toolnames"
)

// Trajectory statistics (A6, competition #4, 2026-08-21): the run's final
// summary is GENERATED from the session JSONL at task end instead of being
// reconstructed from memory — gopi's #4 report under-stated thinking by ~12x
// (self-reported ~1,600 chars vs 18,926 measured from the log). The parser
// below feeds printTrajectoryStats, which Run's deferred exit handler prints
// automatically.
//
// Event order in the session log (per round): assistant thinking/text/tool_use
// events, then the request event, then tool_result events. The thinking
// accumulated between two request events belongs to the LATER round; the
// per-round wait is the request-to-request delta.

// TrajectoryStats is the parsed summary of one session log.
type TrajectoryStats struct {
	Rounds int // model requests (rounds)

	// Thinking, measured per round (chars). ThinkingMedian is the median over
	// rounds that carried thinking (comparable to the competition #4 metric);
	// ThinkingRounds is how many rounds that was.
	ThinkingTotal  int
	ThinkingMedian int
	ThinkingMax    int
	ThinkingRounds int

	// Per-round waits (seconds), measured request→request (first round:
	// user message → first request).
	WaitMedian float64
	WaitMean   float64
	WaitMax    float64

	ToolCalls       int            // tool_use blocks
	ToolBreakdown   map[string]int // tool name → calls
	CompletionTotal int            // sum of request completion_tokens
	CacheHitTotal   int            // sum of request cache_hit
	CacheMissTotal  int            // sum of request cache_miss
	ElapsedSec      float64        // session_start → session_end

	// Task-list issuance telemetry (P4, audit v2 RC1/RC4). Computed by
	// scanning assistant text blocks for the [TASK_LIST_BEGIN]/[END] markers,
	// so future audits answer "does this session establish a list, and how
	// late?" with a jq field instead of a role-aware re-parse.
	// FirstTasklistMsgRatio is -1 when the session never established a list.
	FirstTasklistMsgRatio float64
	NLists                int  // count of assistant messages carrying a task list
	NMalformed            int  // BEGIN without a valid END (silent-drop class, P0)
	ListEverEstablished   bool // any list established in the session

	// Memory-adoption telemetry (memory-consult epic, candidate-A instrument).
	// The task-start checkpoint FORCES the search; these fields measure whether
	// the forced search was ever USED — the difference between "查了" and
	// "查了不用" that the epic's threshold question turns on. Measured, not
	// self-reported: every number is parsed from the session JSONL.
	//
	// FirstToolRound / FirstMemorySearchRound are 0-based ROUND indices
	// (round = the span closed by one `request` event; -1 = never). A tool_use
	// seen after round k's events but before round k+1's closes round k.
	FirstToolRound         int  // round carrying the run's first tool_use (-1 none)
	MemoryFirstToolRound   bool // the run's first tool_use was a memory search
	MemorySearchRounds     int  // distinct rounds that issued a memory search
	FirstMemorySearchRound int  // round of the first memory search (-1 never)

	// MemoryRowsReturned is the count of DISTINCT distilled row ids the
	// searches handed back to the model (rendered as "#<id> " hit lines).
	// MemoryRowsConsumed is how many of them a LATER tool call acted on —
	// a memory feedback/update naming that same id. It is a deliberate LOWER
	// BOUND: a row the model merely read and acted on without naming cannot
	// be counted, so consumption is never over-reported.
	MemoryRowsReturned  int
	MemoryRowsConsumed  int
	MemoryEmptySearches int // searches that returned no distilled row ("No distilled memory found")

	// MemoryStoreCalls is the run's memory(action=store) tool_use count and
	// MemoryFeedbackCalls the feedback+update count — stored next to the search
	// counters so the run-end summary answers "did the store-side timing
	// contract fire at all?" with ONE number a report can copy, instead of
	// leaving a 0-store run indistinguishable from a session that never
	// loaded the memory tool (the 0-call case this epic measured).
	// MemorySearchCalls counts search CALLS (MemorySearchRounds above counts
	// distinct ROUNDS, so two searches in one round differ by design).
	MemorySearchCalls   int
	MemoryStoreCalls    int
	MemoryFeedbackCalls int

	// MemorySearchCompletionTokens sums the completion tokens of the rounds
	// that issued a search — the extra spend the checkpoint induced (the round
	// that answers it carries the search call). A cost proxy, not a bill.
	MemorySearchCompletionTokens int

	// ConsultCheckpointInjected reports whether the review-time memory-consult
	// accountability checkpoint was injected into this run (candidate C).
	ConsultCheckpointInjected bool
}

// Format renders the stats as the terminal block printed at task end.
// Every number is a measured value, so reports can copy it verbatim.
func (s *TrajectoryStats) Format() string {
	var b strings.Builder
	b.WriteString("[trajectory stats]\n")
	fmt.Fprintf(&b, "rounds=%d thinking=%d chars (median %d/round, max %d, %d/%d rounds carried thinking)\n",
		s.Rounds, s.ThinkingTotal, s.ThinkingMedian, s.ThinkingMax, s.ThinkingRounds, s.Rounds)
	fmt.Fprintf(&b, "waits: median %.1fs mean %.1fs max %.1fs\n", s.WaitMedian, s.WaitMean, s.WaitMax)
	if s.ToolCalls > 0 {
		names := make([]string, 0, len(s.ToolBreakdown))
		for name := range s.ToolBreakdown {
			names = append(names, name)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, name := range names {
			parts = append(parts, fmt.Sprintf("%s %d", name, s.ToolBreakdown[name]))
		}
		fmt.Fprintf(&b, "tool_calls=%d: %s\n", s.ToolCalls, strings.Join(parts, ", "))
	} else {
		b.WriteString("tool_calls=0\n")
	}
	fmt.Fprintf(&b, "completion_tokens=%d cache: %d hit / %d total (%.1f%%)\n",
		s.CompletionTotal, s.CacheHitTotal, s.CacheHitTotal+s.CacheMissTotal,
		cacheHitRate(s.CacheHitTotal, s.CacheMissTotal))
	fmt.Fprintf(&b, "tasklist: ever_established=%t n_lists=%d n_malformed=%d first_ratio=",
		s.ListEverEstablished, s.NLists, s.NMalformed)
	if s.FirstTasklistMsgRatio < 0 {
		b.WriteString("-1.0 (never established)\n")
	} else {
		fmt.Fprintf(&b, "%.2f\n", s.FirstTasklistMsgRatio)
	}
	fmt.Fprintf(&b, "memory: first_tool_round=%d memory_first=%t search_rounds=%d first_search_round=%d\n",
		s.FirstToolRound, s.MemoryFirstToolRound, s.MemorySearchRounds, s.FirstMemorySearchRound)
	fmt.Fprintf(&b, "memory adoption: rows_returned=%d rows_consumed=%d (%.0f%% consumed) empty_searches=%d search_completion_tokens=%d\n",
		s.MemoryRowsReturned, s.MemoryRowsConsumed, memoryConsumeRate(s.MemoryRowsReturned, s.MemoryRowsConsumed),
		s.MemoryEmptySearches, s.MemorySearchCompletionTokens)
	fmt.Fprintf(&b, "memory store calls: search=%d store=%d feedback_or_update=%d\n",
		s.MemorySearchCalls, s.MemoryStoreCalls, s.MemoryFeedbackCalls)
	fmt.Fprintf(&b, "memory consult checkpoint injected: %t\n", s.ConsultCheckpointInjected)
	fmt.Fprintf(&b, "elapsed=%.1fs\n", s.ElapsedSec)
	return b.String()
}

// memoryConsumeRate returns the share of returned rows the run acted on, as a
// percentage (0 when nothing was returned — an empty search has no rows whose
// use could be measured, and reporting 0% there would read as "the model
// ignored hits" when in fact there were none to ignore).
func memoryConsumeRate(returned, consumed int) float64 {
	if returned == 0 {
		return 0
	}
	return float64(consumed) / float64(returned) * 100
}

// cacheHitRate returns the cache hit percentage (0 when nothing was billed).
func cacheHitRate(hit, miss int) float64 {
	total := hit + miss
	if total == 0 {
		return 0
	}
	return float64(hit) / float64(total) * 100
}

// sessionEvent is the minimal shape every JSONL line shares.
type sessionEvent struct {
	Type       string `json:"type"`
	Timestamp  string `json:"timestamp"`
	Iteration  int    `json:"iteration"`
	CacheHit   int    `json:"cache_hit"`
	CacheMiss  int    `json:"cache_miss"`
	Completion int    `json:"completion_tokens"`
	Message    *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// sessionToolResult is a tool_result block: ToolUseID names the tool_use block
// it answers, Content carries the rendered text. Both are needed by the
// memory-adoption telemetry (which tool_use the hits belong to, and whether
// the result was the empty-search hint).
type sessionToolResult struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

// toolResultText flattens a tool_result content field to text: the field is a
// plain string in the wire shape gopi writes, but the Anthropic schema allows
// a block array — both are handled, and a shape neither matches yields "".
func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return ""
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(blk.Text)
	}
	return b.String()
}

// memoryConsultState accumulates the memory-adoption telemetry across one
// ordered scan of the session log. All fields are keyed by the tool_use ids
// the log itself carries, so a result can always be traced to the call that
// produced it.
type memoryConsultState struct {
	firstToolSeen bool
	// owners maps a memory-search tool_use id to the distilled row ids its
	// result handed back — the set whose downstream use is the adoption signal.
	owners map[string][]int
	// consumed marks a row id some later memory feedback/update named.
	consumed map[int]bool
	// lastSearchRound guards MemorySearchRounds against double-counting two
	// parallel searches issued in the same round.
	lastSearchRound int
	// searchRoundPending is set when a search was issued since the last
	// round boundary, so the closing request's completion tokens can be
	// attributed to it.
	searchRoundPending bool
}

// memoryActionOf reads the action wire value out of a memory tool_use's
// arguments ("" when the block does not parse). ONE parser for every caller —
// the search / store / feedback classification the run-end summary reports
// must not be able to disagree with itself (Rule 14's single-source shape).
func memoryActionOf(raw json.RawMessage) string {
	var args struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ""
	}
	return args.Action
}

// isMemorySearchInput reports whether a memory tool_use block's arguments ask
// for a search (the action wire value, never a bare literal — Rule 1b).
func isMemorySearchInput(raw json.RawMessage) bool {
	return memoryActionOf(raw) == toolnames.ToolMemoryActionSearch
}

// isMemoryConsumeInput reports whether a memory tool_use block is a
// feedback/update naming a distilled row id — the observable form of "the
// model acted on a row a search had handed it". Returns the id and whether it
// was found: feedback and update are the only actions that address a row by
// number.
func isMemoryConsumeInput(raw json.RawMessage) (int, bool) {
	var args struct {
		Action string  `json:"action"`
		ID     float64 `json:"id"` // JSON numbers decode to float64
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return 0, false
	}
	switch args.Action {
	case toolnames.ToolMemoryActionFeedback, toolnames.ToolMemoryActionUpdate:
	default:
		return 0, false
	}
	if args.ID <= 0 {
		return 0, false
	}
	return int(args.ID), true
}

// parseHitRowIDs extracts the distilled row ids a memory search rendered back
// (the "#<id> " hit-line prefix). Scoped to search results by the caller, so a
// '#' in any OTHER tool's output can never be mistaken for a memory hit.
func parseHitRowIDs(text string) []int {
	var ids []int
	seen := make(map[int]bool)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(line, "#") {
			continue
		}
		end := 1
		for end < len(line) && line[end] >= '0' && line[end] <= '9' {
			end++
		}
		if end == 1 {
			continue
		}
		id, err := strconv.Atoi(line[1:end])
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// tallyMemoryToolUse folds one assistant tool_use block into the telemetry.
// round is the 0-based round index the block belongs to (the count of request
// events seen so far — an assistant event precedes the request that closes its
// own round).
func (m *memoryConsultState) tallyMemoryToolUse(blk sessionBlock, round int, stats *TrajectoryStats) {
	if !m.firstToolSeen {
		m.firstToolSeen = true
		stats.FirstToolRound = round
		if blk.Name == toolnames.ToolMemory && isMemorySearchInput(blk.Input) {
			stats.MemoryFirstToolRound = true
		}
	}
	if blk.Name != toolnames.ToolMemory {
		return
	}
	// Store-side counts (the epic's B mechanism): every memory tool_use is
	// classified by its action value BEFORE the search branch returns, so a
	// run that never called store reads store=0 in the run-end summary — the
	// observable form of the gap this epic exists to close.
	switch memoryActionOf(blk.Input) {
	case toolnames.ToolMemoryActionSearch:
		stats.MemorySearchCalls++
	case toolnames.ToolMemoryActionStore:
		stats.MemoryStoreCalls++
	case toolnames.ToolMemoryActionFeedback, toolnames.ToolMemoryActionUpdate:
		stats.MemoryFeedbackCalls++
	}
	if isMemorySearchInput(blk.Input) {
		if m.lastSearchRound != round || stats.MemorySearchRounds == 0 {
			stats.MemorySearchRounds++
		}
		m.lastSearchRound = round
		m.searchRoundPending = true
		if stats.FirstMemorySearchRound < 0 {
			stats.FirstMemorySearchRound = round
		}
		if blk.ID != "" {
			m.owners[blk.ID] = nil // resolved when its tool_result arrives
		}
		return
	}
	if id, ok := isMemoryConsumeInput(blk.Input); ok {
		m.consumed[id] = true
	}
}

// tallyToolResult folds one tool_result into the telemetry: a result whose
// tool_use_id belongs to a tracked memory search contributes either the row
// ids it handed back or one empty-search count.
func (m *memoryConsultState) tallyToolResult(text string, toolUseID string, stats *TrajectoryStats) {
	if _, tracked := m.owners[toolUseID]; !tracked {
		return
	}
	ids := parseHitRowIDs(text)
	if len(ids) == 0 {
		stats.MemoryEmptySearches++
		return
	}
	m.owners[toolUseID] = ids
}

// finish resolves the returned-row totals once the whole log has been read:
// a row counts as returned when a search handed it back, and as consumed when
// a later feedback/update named that same id.
func (m *memoryConsultState) finish(stats *TrajectoryStats) {
	for _, ids := range m.owners {
		for _, id := range ids {
			stats.MemoryRowsReturned++
			if m.consumed[id] {
				stats.MemoryRowsConsumed++
			}
		}
	}
}

// sessionBlock is the content-block shape of assistant events.
type sessionBlock struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
	Name     string `json:"name"` // tool_use
	Text     string `json:"text"` // text block (task-list marker source)
	// ID is the tool_use block's id and Input its arguments — the
	// memory-adoption telemetry keys on both (which search produced a result,
	// and whether a later feedback/update names the same row id).
	ID    string          `json:"id"`
	Input json.RawMessage `json:"input"`
}

// tallyTaskListText inspects one assistant text block for task-list markers
// and updates the running telemetry counters (P4):
//   - textMsgIdx is the 0-based index of the text message being examined.
//   - firstListIdx/everEstablished/nLists/nMalformed are updated in place.
//
// A message is a valid list when it carries [TASK_LIST_BEGIN] and a
// [TASK_LIST_END] (mirrors ParseTaskList's marker check). BEGIN without END
// is the silent-drop malformed class (P0) and counts into nMalformed only.
func tallyTaskListText(text string, textMsgIdx int, firstListIdx *int, everEstablished *bool, nLists, nMalformed *int) {
	if !strings.Contains(text, TaskListBeginMarker) {
		return
	}
	hasEnd := strings.Contains(text, TaskListEndMarker)
	if !hasEnd {
		*nMalformed++
		return
	}
	*nLists++
	if firstListIdx != nil && *firstListIdx == -1 {
		*firstListIdx = textMsgIdx
	}
	if everEstablished != nil {
		*everEstablished = true
	}
}

// ParseTrajectoryStats reads a session JSONL and computes the trajectory
// summary. Errors are returned for unreadable files; malformed individual
// lines are skipped (best-effort — the file is written append-only).
func ParseTrajectoryStats(path string) (*TrajectoryStats, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("trajectory: cannot open session log: %w", err)
	}
	defer f.Close()

	stats := &TrajectoryStats{ToolBreakdown: make(map[string]int)}

	// No list yet → FirstTasklistMsgRatio is -1 (overwritten once a list is
	// established). Default here so a never-established session reads -1.
	stats.FirstTasklistMsgRatio = -1

	// Memory-adoption telemetry: -1 until the corresponding event is seen (an
	// absent observation must be distinguishable from a round-0 observation).
	stats.FirstToolRound = -1
	stats.FirstMemorySearchRound = -1
	consult := memoryConsultState{owners: make(map[string][]int), consumed: make(map[int]bool)}

	var thinkingValues []int // per-round thinking chars (rounds with thinking)
	var waitSamples []float64
	var lastReq time.Time // previous round boundary (user msg or request)
	var start time.Time
	var thinkingChars int
	var thinkingSeen bool

	// Task-list telemetry counters (P4): assistant text message index (0-based)
	// and the index of the first message that carried a task list. A text
	// message with [TASK_LIST_BEGIN] but no [TASK_LIST_END] is the silent-drop
	// malformed class (P0) — counted into NMalformed, not a valid list.
	var textMsgIdx int
	var firstListIdx int = -1

	// closeRound attributes the thinking accumulated since the previous
	// boundary to a round and samples the wait.
	closeRound := func(ts time.Time) {
		stats.Rounds++
		if thinkingSeen {
			stats.ThinkingRounds++
			stats.ThinkingTotal += thinkingChars
			thinkingValues = append(thinkingValues, thinkingChars)
			if thinkingChars > stats.ThinkingMax {
				stats.ThinkingMax = thinkingChars
			}
		}
		thinkingChars = 0
		thinkingSeen = false
		if !lastReq.IsZero() {
			waitSamples = append(waitSamples, ts.Sub(lastReq).Seconds())
		}
		lastReq = ts
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev sessionEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		ts, terr := time.Parse(time.RFC3339Nano, ev.Timestamp)
		if terr != nil {
			continue
		}
		switch ev.Type {
		case sessionevent.EventTypeSessionStart:
			start = ts
		case "user":
			// First-round wait baseline: user message → first request.
			lastReq = ts
			// The review-time memory-consult checkpoint (candidate C) is
			// injected as a user message; seeing it in the log is the
			// auditable proof the accountability question reached the model
			// (a mechanism that leaves no trace is unmeasurable).
			if ev.Message != nil && strings.Contains(toolResultText(ev.Message.Content), consultCheckpointMarker) {
				stats.ConsultCheckpointInjected = true
			}
		case "tool_result":
			if ev.Message != nil {
				text := toolResultText(ev.Message.Content)
				var blocks []sessionToolResult
				if err := json.Unmarshal(ev.Message.Content, &blocks); err == nil {
					for _, blk := range blocks {
						if blk.Type == "tool_result" {
							consult.tallyToolResult(toolResultText(blk.Content), blk.ToolUseID, stats)
						}
					}
				} else {
					consult.tallyToolResult(text, "", stats)
				}
			}
		case "request":
			// Thinking accumulated since the previous boundary belongs to
			// THIS round (assistant events precede their request event).
			closeRound(ts)
			if consult.searchRoundPending {
				stats.MemorySearchCompletionTokens += ev.Completion
				consult.searchRoundPending = false
			}
			stats.CompletionTotal += ev.Completion
			stats.CacheHitTotal += ev.CacheHit
			stats.CacheMissTotal += ev.CacheMiss
		case "assistant":
			if ev.Message == nil || len(ev.Message.Content) == 0 {
				continue
			}
			// Content is either an array of blocks or a single object.
			if ev.Message.Content[0] == '[' {
				var blocks []sessionBlock
				if err := json.Unmarshal(ev.Message.Content, &blocks); err != nil {
					continue
				}
				for _, blk := range blocks {
					switch blk.Type {
					case "thinking":
						thinkingSeen = true
						thinkingChars += utf8.RuneCountInString(blk.Thinking)
					case "tool_use":
						stats.ToolCalls++
						if blk.Name != "" {
							stats.ToolBreakdown[blk.Name]++
							consult.tallyMemoryToolUse(blk, stats.Rounds, stats)
						}
					case "text":
						if blk.Text != "" {
							tallyTaskListText(blk.Text, textMsgIdx, &firstListIdx,
								&stats.ListEverEstablished, &stats.NLists, &stats.NMalformed)
						}
						textMsgIdx++
					}
				}
			} else {
				var blk sessionBlock
				if err := json.Unmarshal(ev.Message.Content, &blk); err != nil {
					continue
				}
				if blk.Type == "thinking" {
					thinkingSeen = true
					thinkingChars += utf8.RuneCountInString(blk.Thinking)
				} else if blk.Type == "tool_use" {
					stats.ToolCalls++
					if blk.Name != "" {
						stats.ToolBreakdown[blk.Name]++
						consult.tallyMemoryToolUse(blk, stats.Rounds, stats)
					}
				} else if blk.Type == "text" && blk.Text != "" {
					tallyTaskListText(blk.Text, textMsgIdx, &firstListIdx,
						&stats.ListEverEstablished, &stats.NLists, &stats.NMalformed)
					textMsgIdx++
				}
			}
		case sessionevent.EventTypeSessionEnd:
			// Rounds are delimited by request events only; the trailing
			// tool_results belong to the last round.
			stats.ElapsedSec = ts.Sub(start).Seconds()
		}
	}

	// A truncated log without session_end still yields the rounds seen.
	if !start.IsZero() && stats.ElapsedSec == 0 {
		stats.ElapsedSec = time.Since(start).Seconds()
	}

	if len(waitSamples) > 0 {
		sort.Float64s(waitSamples)
		stats.WaitMedian = medianFloat(waitSamples)
		var sum float64
		for _, w := range waitSamples {
			sum += w
			if w > stats.WaitMax {
				stats.WaitMax = w
			}
		}
		stats.WaitMean = sum / float64(len(waitSamples))
	}
	if len(thinkingValues) > 0 {
		sort.Ints(thinkingValues)
		stats.ThinkingMedian = medianInt(thinkingValues)
	}

	// Task-list issuance ratio (P4): first-list message index over total
	// assistant text messages. -1 when no list was ever established.
	if firstListIdx >= 0 {
		if total := textMsgIdx; total > 0 {
			stats.FirstTasklistMsgRatio = float64(firstListIdx) / float64(total)
		}
	}

	// Memory-adoption totals: resolve the tracked searches' row ids and count
	// how many of them the run later acted on.
	consult.finish(stats)
	return stats, nil
}

// medianInt returns the median of a sorted int slice, averaging the two
// middle values on even counts (the Python statistics.median convention used
// by the competition reports).
func medianInt(sorted []int) int {
	if len(sorted)%2 == 1 {
		return sorted[len(sorted)/2]
	}
	return (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
}

// medianFloat is medianInt for float64.
func medianFloat(sorted []float64) float64 {
	if len(sorted)%2 == 1 {
		return sorted[len(sorted)/2]
	}
	return (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
}

// printTrajectoryStats parses the run's session log and prints the trajectory
// summary to stderr. Best-effort: a missing/unreadable log only logs a note.
func (a *Agent) printTrajectoryStats() {
	if a.SessionLog == nil {
		return
	}
	stats, err := ParseTrajectoryStats(a.SessionLog.Path())
	if err != nil {
		if a.Logger != nil {
			a.Logger.Printf("trajectory stats: %v", err)
		}
		return
	}
	fmt.Fprint(os.Stderr, stats.Format())
}
