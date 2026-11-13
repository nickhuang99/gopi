package gopilog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nick/gopi/internal/pricing"
	"github.com/nick/gopi/internal/sessionevent"
)

// Session statistics (`gopi session stats`). PRIMARY source is the paired raw HTTP
// log: one self-contained file whose REQUEST bodies carry model/messages and
// whose CHUNK usage lines carry every token number, so the aggregate needs no
// other input. When no raw log can be paired the session JSONL's request
// events are used instead and the output is stamped source=jsonl — the two
// sources are never mixed, and each keeps its own round numbering (raw
// ordinals are 1-based and include guard probes; JSONL iterations are
// 0-based), per the cross-indexing rule in IMPLEMENT-SPEC §1.
const (
	// StatsSourceRaw marks numbers taken from the paired raw HTTP log.
	StatsSourceRaw = "raw"
	// StatsSourceJSONL marks numbers taken from the session JSONL request
	// events (raw-log fallback; raw-only dimensions are absent).
	StatsSourceJSONL = "jsonl"
	// StatsSourceAggregate marks a sum over many sessions (gopi session stats --all).
	StatsSourceAggregate = "aggregate"
)

// Trim-cliff detection thresholds. A cliff is a round whose prompt tokens
// dropped BOTH more than statsCliffMinDropTokens (absolute floor — filters
// the noise of a short final round) AND to under statsCliffMaxRatioPct of the
// previous round (relative floor — filters the natural shrink of a session's
// last turns). Calibrated on the 463-round reference session
// 7168c24d-1bad-…: the pair reproduces FINAL-USAGE-PROPOSAL's --rounds
// excerpt exactly (rounds 39 / 171 / 450, Δreq -43.2K / -74.4K / -92.2K).
const (
	statsCliffMinDropTokens = 4000 // min token drop vs the previous round
	statsCliffMaxRatioPct   = 60   // new prompt must be under this % of the previous
)

// Display caps / widths (PROJECT.md Rule 1: every display cap is a constant).
const (
	statsKeyWidth      = 15 // width of the key column in the overview box
	statsRoundToolsMax = 3  // tools listed per row in the --rounds table
	statsToolRankMax   = 20 // rows shown by --tools
	// Token-compaction thresholds (decimal K/M rendering — 14,163 renders as
	// "14.2K", 32,520,000 as "32.52M", matching the approved usage mock).
	tokenKiloThreshold = 1000
	tokenMegaThreshold = 1000 * 1000
	// wall-time rendering
	statsWallSecondsPerMin = 60.0
)

// The ROUND naming of the stats tables (PROJECT.md Rule 1/13/14).
//
// WHAT EVERY ROW OF THESE TABLES COUNTS IS A ROUND — one JSONL `request` row =
// one HTTP request/response = one iteration, sessionevent.CounterUnitRound —
// which is exactly what `gopi session log <id> --rounds` tabulates and what
// `gopi session replay --rounds` prints as `rounds=` (the operator's ruling,
// 2026-10-12). The operator-row quantity is a NEIGHBOUR with its own name
// (sessionevent.CounterUnitHumanTurn, "turns") and is never called a round here.
//
// NOTHING IS DECLARED HERE THAT ALREADY HAS ITS ONE DEFINITION (Rule 13/14: a
// second spelling of a shared value is the defect, a consumer references it):
// the quantity's own word is sessionevent.CounterUnitRound, the column word is
// sessionround.go's sessionRoundColumnWord (read through the exported
// SessionRoundColumnWord, so cmd/agent asserts the same value), and the
// `--rounds` SELECTOR spelling is the CLI's own (cmd/agent/cli_stats.go) — this
// package holds no second "round".
const (
	// statsRoundMoneyHeader heads the --bill table's per-round money column
	// ("ROUND 元"). It is derived from the column word, never typed twice.
	statsRoundMoneyHeader = sessionRoundColumnWord + " 元"
)

// statsTurnDetailFmt is the overview box's request-count detail, printed under
// sessionevent.CounterUnitInnerRequest beside the turn headline. The count is
// the ROUND count (one `request` row each), so the sentence names the request,
// never a round: the shell is here, the word is the constant's (Rule 1/14).
const statsTurnDetailFmt = "%d"

// roundWord is the lower-case singular noun of the counted quantity, for a line
// that numbers ONE round ("@ round 2", "per round"). The word is DERIVED from
// the unit constant rather than typed: a bare literal here would survive the
// next rename of the unit and this surface would disagree with every other one.
func roundWord() string {
	return strings.TrimSuffix(sessionevent.CounterUnitRound, "s")
}

// roundsWord is its plural — the unit constant itself, which is the one
// definition of the word (Rule 14). It exists so a call site reads as prose
// ("(no rounds)") rather than as a constant expression.
func roundsWord() string { return sessionevent.CounterUnitRound }

// RoundStat is one LLM round's numeric fingerprint. Number means "raw round
// ordinal (1-based)" when Source is raw and "JSONL iteration (0-based)" when
// Source is jsonl — the two are never cross-indexed.
type RoundStat struct {
	Number           int      `json:"number"`
	PromptTokens     int      `json:"prompt_tokens"`
	CacheHit         int      `json:"cache_hit"`
	CacheMiss        int      `json:"cache_miss"`
	CacheWrite       int      `json:"cache_write"`
	CompletionTokens int      `json:"completion_tokens"`
	DeltaReq         int      `json:"delta_req"`
	Cliff            bool     `json:"trim_cliff"`
	Tools            []string `json:"tools,omitempty"`
	ToolCalls        int      `json:"tool_calls"`
	StopReason       string   `json:"stop_reason,omitempty"`
	// Probe marks a guard/fail-fast ladder probe round (annotated raw marker).
	// Probe rounds stay in the table (the numbering must stay gap-free for
	// `gopi session raw <id> <round>`) but are excluded from the Δreq chain: a probe's
	// small body would otherwise read as a trim cliff.
	Probe  bool   `json:"probe"`
	Detail string `json:"detail,omitempty"`
	// Turn is the TURN this round belongs to (1-based; 0 = unknown, for a source
	// that recorded no operator boundary — a raw log carries no operator rows).
	// It is what makes this table a per-EXCHANGE breakdown OF the turns rather
	// than a competing headline (operator's ruling, 2026-10-22): the round is the
	// address, the turn is the subject. Derived from gopilog.SessionTurnSpans, the
	// ONE place the boundaries live (Rule 14).
	Turn int `json:"turn,omitempty"`

	// Body-traffic fields (raw source only — the session JSONL carries no
	// wire bodies, so a jsonl-sourced round leaves all five zero). ReqBytes
	// is the request body's exact wire bytes, Messages the number of
	// user/tool messages that body carried (the context-growth signal behind
	// the token numbers), RespBytes the response side's wire bytes (chunks /
	// body incl. marker overhead), RespKind the response-side shape
	// (stream / response / http-error / none), TextChars / ThinkingChars
	// the response's reassembled text/thinking characters (a volume proxy,
	// NOT token counts — see RoundResponse). The --bytes view is built from
	// these.
	ReqBytes      int    `json:"req_bytes,omitempty"`
	Messages      int    `json:"messages,omitempty"`
	RespBytes     int    `json:"resp_bytes,omitempty"`
	RespKind      string `json:"resp_kind,omitempty"`
	TextChars     int    `json:"text_chars,omitempty"`
	ThinkingChars int    `json:"thinking_chars,omitempty"`
}

// ToolCount is one tool's call count (the --tools ranking row).
type ToolCount struct {
	Name  string `json:"name"`
	Calls int    `json:"calls"`
}

// SessionStats is the aggregate of one session (or of many, when Source is
// aggregate). All token numbers are provider-reported usage, never estimates.
type SessionStats struct {
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
	// Source is raw | jsonl | aggregate.
	Source string `json:"source"`
	// SourceDetail names the file the numbers came from (raw log base name,
	// session JSONL base name, or "" for an aggregate).
	SourceDetail string `json:"source_detail"`
	// Sessions is 1 for a single session, N for an aggregate.
	Sessions int `json:"sessions"`

	// WireLogDetail names the raw HTTP log whose WIRE BODIES were joined onto
	// this block's rows (the --bytes traffic), when one was paired. It is
	// separate from SourceDetail because the round NUMBERS and the bodies come
	// from two different corpora: the rounds are canonical (the session's
	// `request` rows), the bodies only the raw log holds.
	WireLogDetail string `json:"wire_log,omitempty"`

	// Rounds counts the rounds of the source under the CANONICAL definition when
	// the source is the session JSONL: one `request` row = one HTTP
	// request/response exchange (BuildStatsFromJSONL — the same number
	// `gopi session log --rounds` and `gopi session replay --rounds` print). A
	// raw-sourced block counts raw `→ REQUEST` markers instead, which is a
	// DIFFERENT corpus (it also numbers sends the JSONL never recorded); the
	// source line above the table says which one is in use.
	Rounds int `json:"rounds"`
	// HumanTurns is the PERSON-ONLY SUBSET of the session's turns: how many of
	// them opened at a row the person typed (SessionTurns). It is what this
	// block's overview row prints, and it prints under its own word
	// (sessionevent.CounterUnitHumanTurn) — because this package CANNOT read the
	// turn count itself. `chats.turn_count` lives in the chat index
	// (internal/tools), which imports this one, so the total is the surface
	// `gopi session show <id>`'s to print and this block points at it instead of
	// promoting the subset into its place (measured on
	// 41e5dade-71e1-4efc-a012-456d0532e762: 66 here, 200 there).
	//
	// It is derived from the SAME SessionTurnSpans the replay steps and the
	// `session log --rounds` rows come from (Rule 14), so the surfaces cannot
	// disagree about where a human turn starts. 0 means "this source recorded no
	// operator row" (a raw-log or usage-scan block), never a fabricated count.
	HumanTurns int `json:"human_turns"`
	// HumanTurnsUnmeasured says the SUBSET above is NOT a measurement: the log
	// recorded no turn seam at all, so SessionHumanTurns' fallback produced a
	// number that belongs to another counter (SessionHumanTurnsUnmeasured — the
	// criterion's one definition point). HumanTurnsNote carries the reason.
	//
	// The ZERO VALUE MEANS MEASURED, which is what every constructor in this
	// package sets: a caller that fills HumanTurns with a derived count sets this
	// flag from the criterion, and a caller that leaves it at false is claiming
	// the number it stored is a measurement (measured 2026-10-29, session
	// 794cbd02: the overview printed `human turns 85` beside `requests 85`).
	HumanTurnsUnmeasured bool `json:"human_turns_unmeasured,omitempty"`
	// HumanTurnsNote is the reason the subset is unmeasured (Rule 15: the kind of
	// gap is named, not collapsed into a bare "unknown").
	HumanTurnsNote string `json:"human_turns_note,omitempty"`
	// TurnsOpened is the session LOG's turn count — one per turn boundary the
	// log recorded. It is the axis `--turn N`, a replay band and the `--turns`
	// table number along, so it is what the bare word `turns` prints here (epic
	// 2026-11-04).
	//
	// IT REPLACED TurnCount ON THAT ROW AND THE MEASUREMENT IS WHY: the ledger's
	// `chats.turn_count` advances only when a turn COMPLETES (cmd/agent's
	// `runErr == nil && !interrupted` gate), so on session
	// 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f it read 29 while the log held 30
	// turns and the replay screen ended on `──── turn 30` — the thirtieth turn
	// opened and quit. A completed-count can also run AHEAD (e364e357: ledger
	// 209, log 34), so anchoring an ordinal on it addresses a different turn on
	// either side. This field needs no injected reader: the block is built from
	// the parsed session itself.
	TurnsOpened int `json:"turns_opened"`
	// TurnCount is the LEDGER's count of turns that FINISHED
	// (`chats.turn_count`), read through the injected reader in ledger_turns.go
	// because this package cannot import internal/tools. It is published under
	// its own word (`turns completed`), never as the bare one, so a reader who
	// needs the ledger quantity keeps it and the gap between the two numbers is
	// accounted rather than discovered (Rule 14: one meaning per word).
	//
	// TurnsMeasured=false means the count was NOT read (no reader installed, no
	// index row, a failed read) and TurnCountNote says which — the block prints
	// the reason instead of a number (Rule 15).
	TurnCount     int    `json:"turn_count"`
	TurnsMeasured bool   `json:"turns_measured"`
	TurnCountNote string `json:"turn_count_note,omitempty"`
	// RoundsCounted counts the rounds folded into the token aggregates:
	// non-probe rounds that reported usage. Probes and no-usage rounds keep
	// their table row but never skew a min/max/avg.
	RoundsCounted int `json:"rounds_counted"`
	Probes        int `json:"probe_rounds"`
	NoUsage       int `json:"rounds_without_usage"`
	// UsageSegments is how many RUN SEGMENTS the session's usage numbers are a
	// SUM over (1 for a session that ran once, N for a resumed one, whose
	// writer restarted its counters each time). UsageUnknown is how many
	// `request` rows could NOT be folded because their quantity was never
	// established, and UsageBasis is the evidence the fold rested on.
	//
	// THE THREE EXIST SO THE HEADLINE STATES ITS SPAN (Rule 15). A session that
	// reported 395,832,811 prompt tokens is not a session that sent that many
	// tokens in one context: it is THREE runs summed (measured on
	// 66243525-16ab-4ce7-9133-84d74c539840), and a reader who is not told that
	// will read the number as one conversation. Zero means the source did not
	// derive usage this way (a raw-log or usage-scan block), never "one run".
	UsageSegments int         `json:"usage_segments,omitempty"`
	UsageUnknown  int         `json:"usage_rows_unknown,omitempty"`
	UsageBasis    string      `json:"usage_basis,omitempty"`
	RoundStats    []RoundStat `json:"round_stats,omitempty"`
	PromptTokens  int         `json:"prompt_tokens"`
	CacheHit      int         `json:"cache_hit"`
	CacheMiss     int         `json:"cache_miss"`
	CacheWrite    int         `json:"cache_write"`
	Completion    int         `json:"completion_tokens"`
	MinPrompt     int         `json:"prompt_min"`
	MaxPrompt     int         `json:"prompt_max"`
	ToolCalls     int         `json:"tool_calls"`
	ToolKinds     []ToolCount `json:"tool_kinds"`
	// CliffCount counts the trim cliffs (the --rounds rows flagged *).
	CliffCount int `json:"trim_cliffs"`
	// Cliffs lists the RoundStats indexes flagged as cliffs (single session
	// only; an aggregate leaves it nil and keeps only CliffCount).
	Cliffs []int `json:"trim_cliff_rounds,omitempty"`
	// LargestCliff / LargestCliffRound describe the biggest cliff
	// (LargestCliffRound is a RoundStats index, -1 when none).
	LargestCliff       int `json:"largest_cliff_tokens,omitempty"`
	LargestCliffIdx    int `json:"-"`
	LargestCliffNumber int `json:"largest_cliff_round,omitempty"`

	// WallSeconds is the session's elapsed_sec (session_end event). The raw
	// log carries no timestamps, so this one field always comes from the
	// session JSONL; HasWall reports whether it was available.
	WallSeconds float64 `json:"wall_seconds,omitempty"`
	HasWall     bool    `json:"wall_known"`
	// RawBytes is the paired raw log's payload size (0 when the source is
	// jsonl or an aggregate of none).
	RawBytes int64 `json:"raw_log_bytes,omitempty"`

	// Started is the session's start time (session_start event, session
	// JSONL) — the moment `gopi session stats --cost` prices the usage at
	// (date/time-aware: GLM 五折 window / DS peak hours key off it). A raw
	// session whose JSONL could not be read leaves it zero and cost is
	// computed at a zero time only when the caller supplies one.
	// json:"-" — never serialized directly (would leak a zero time into the
	// stable stats JSON); the pricing Result carries priced_at instead.
	Started time.Time `json:"-"`

	// Cost is the real-money breakdown computed by `gopi session stats --cost`
	// (internal/pricing). Populated by ComputeCost; nil when the session was
	// never priced (no --cost) or its model is not priceable. It is a
	// *pricing.Result via the pricing sub-package — kept here so the struct
	// stays the single stats record.
	Cost *pricing.Result `json:"cost,omitempty"`
}

// BuildStatsFromJSONL is the session-JSONL canon: the numbers come from the
// `request` rows themselves, so `gopi session stats` reports the SAME rounds as
// `gopi session log --rounds` and `gopi session replay --rounds` (Rule 14: one
// grouping definition, internal/gopilog). It is preferred over the raw log
// whenever the JSONL is readable, because the raw log is a DIFFERENT corpus: it
// also numbers sends the JSONL never recorded (non-streaming meta calls,
// retries), which is exactly how `stats` came to print a number no other surface
// agreed with.
//
// The usage numbers are the same quantities either source reports, so the
// cross-source identities (prompt totals, the Δreq chain's cliffs) are
// preserved; what changes is the ROUND SET, and that is the point.
func BuildStatsFromJSONL(s *Session) *SessionStats {
	st := &SessionStats{
		Source:          StatsSourceJSONL,
		Sessions:        1,
		SessionID:       s.ID,
		Model:           s.Model,
		Started:         s.Started,
		LargestCliffIdx: -1,
		HumanTurns:      SessionHumanTurns(s),
	}
	st.TurnsOpened = SessionTurns(s)
	st.TurnCount, st.TurnsMeasured, st.TurnCountNote = SessionLedgerTurnCount(s)
	// The SUBSET's measurement state comes from the SAME criterion every render
	// reads (SessionHumanTurnsUnmeasured), so the box and the COST block cannot
	// disagree about whether the number is a measurement.
	st.HumanTurnsNote, st.HumanTurnsUnmeasured = SessionHumanTurnsUnmeasured(s)
	// turnOf maps an iteration index to its turn, from the SAME derivation the
	// replay steps and `session log --turns` use.
	turnOf := make(map[int]int, len(s.Iterations))
	for _, sp := range SessionTurnSpans(s) {
		for j := sp.Start; j < sp.End; j++ {
			if _, ok := turnOf[j]; !ok {
				turnOf[j] = sp.Turn
			}
		}
	}
	prev := -1
	for _, cr := range SessionCanonicalRounds(s) {
		// ONE ROW PER `request` ROW — the same row count the `--rounds` table of
		// `gopi session log` prints. A round the reader holds no content for
		// still gets its row (it is an exchange that ran): its usage is the one
		// fact recorded about it, and printing it is how the two surfaces stay
		// in step.
		rs := RoundStat{Number: cr.Ordinal, Turn: turnOf[cr.Iter]}
		if cr.HasContent() {
			it := &s.Iterations[cr.Iter]
			rs.PromptTokens = it.PromptTokens
			rs.CacheHit = it.CacheHit
			rs.CacheMiss = it.CacheMiss
			rs.CompletionTokens = it.CompletionTokens
			rs.Tools = roundToolNames(*it)
			rs.ToolCalls = len(it.ToolCalls)
		}
		st.RoundStats = append(st.RoundStats, rs)
		idx := len(st.RoundStats) - 1
		st.Rounds++
		if rs.PromptTokens == 0 {
			st.NoUsage++
			continue
		}
		st.accumulate(idx, prev)
		prev = idx
	}
	if !s.EndUnrecorded {
		st.WallSeconds = s.ElapsedSec
		st.HasWall = true
	}
	st.UsageSegments = s.RequestUsageSegments
	st.UsageUnknown = s.RequestUsageUnknown
	st.UsageBasis = s.RequestUsageBasis
	st.finish()
	return st
}

// BuildStatsFromRounds derives session statistics from parsed raw-log rounds.
// Rounds with no usage numbers (an HTTP-ERROR round, a non-streamed response)
// keep their row in RoundStats but are excluded from the token aggregates and
// the min/max, so a dead round cannot drag the averages down. The retained
// request-body bytes are released as they are digested — stats never needs
// them, and a multi-hundred-MB log would otherwise sit in memory for nothing.
func BuildStatsFromRounds(rounds []HTTPRound) *SessionStats {
	st := &SessionStats{Source: StatsSourceRaw, Sessions: 1, LargestCliffIdx: -1}
	var rawBytes int64
	// prev is the index of the last round counted into the Δreq chain.
	prev := -1
	for i := range rounds {
		r := &rounds[i]
		rawBytes += int64(len(r.RequestRaw))
		rs := RoundStat{
			Number:           r.Number,
			Probe:            r.Annotation != "",
			Detail:           r.Annotation,
			StopReason:       r.Response.StopReason,
			CompletionTokens: r.Response.OutputTokens,
			CacheWrite:       r.Response.CacheWrite,
			Tools:            append([]string(nil), r.Response.ToolUses...),
			ToolCalls:        len(r.Response.ToolUses),
		}
		// Body-traffic fields (raw source only): the request body's exact
		// wire bytes and its message count (the context-growth signal behind
		// the token numbers), plus the response side's wire bytes, its shape
		// and its reassembled text/thinking characters. Captured HERE —
		// RequestRaw / Request are released below, and stats never needs
		// them again.
		rs.ReqBytes = len(r.RequestRaw)
		rs.RespBytes = r.Response.Bytes
		rs.RespKind = r.Response.Kind
		if rs.RespKind == "" {
			rs.RespKind = "none"
		}
		rs.TextChars = r.Response.TextChars
		rs.ThinkingChars = r.Response.ThinkingChars
		if r.Request != nil {
			rs.Messages = len(r.Request.Messages)
		}
		// Anthropic reports the UNCACHED input in input_tokens and the cached
		// part separately; the round's prompt total is their sum (verified to
		// equal the session JSONL request event's prompt_tokens on real logs).
		if r.Response.InputTokens > 0 || r.Response.CacheRead > 0 {
			rs.PromptTokens = r.Response.InputTokens + r.Response.CacheRead + r.Response.CacheWrite
			rs.CacheHit = r.Response.CacheRead
			rs.CacheMiss = r.Response.InputTokens
		}
		if st.Model == "" && r.Request != nil {
			st.Model = r.Request.Model
		}
		r.RequestRaw = nil
		r.Request = nil

		st.RoundStats = append(st.RoundStats, rs)
		idx := len(st.RoundStats) - 1
		st.Rounds++
		if rs.Probe {
			st.Probes++
			continue
		}
		if rs.PromptTokens == 0 {
			st.NoUsage++
			continue
		}
		st.accumulate(idx, prev)
		prev = idx
	}
	st.RawBytes = rawBytes
	st.finish()
	return st
}

// accumulate folds one counted round into the aggregate and sets its Δreq /
// cliff fields. idx is the round's index in st.RoundStats; prev is the index
// of the previous counted round (-1 when this is the first).
func (st *SessionStats) accumulate(idx, prev int) {
	rs := &st.RoundStats[idx]
	if prev >= 0 {
		p := &st.RoundStats[prev]
		rs.DeltaReq = rs.PromptTokens - p.PromptTokens
		if p.PromptTokens-rs.PromptTokens > statsCliffMinDropTokens &&
			rs.PromptTokens*100 < p.PromptTokens*statsCliffMaxRatioPct {
			rs.Cliff = true
		}
	}
	st.RoundsCounted++
	st.PromptTokens += rs.PromptTokens
	st.CacheHit += rs.CacheHit
	st.CacheMiss += rs.CacheMiss
	st.CacheWrite += rs.CacheWrite
	st.Completion += rs.CompletionTokens
	st.ToolCalls += rs.ToolCalls
	st.MinPrompt = minNonZero(st.MinPrompt, rs.PromptTokens)
	st.MaxPrompt = maxInt(st.MaxPrompt, rs.PromptTokens)
	for _, name := range rs.Tools {
		st.addTool(name, 1)
	}
	if rs.Cliff {
		st.CliffCount++
		st.Cliffs = append(st.Cliffs, idx)
		if rs.DeltaReq < st.LargestCliff {
			st.LargestCliff = rs.DeltaReq
			st.LargestCliffIdx = idx
			st.LargestCliffNumber = rs.Number
		}
	}
}

// addTool bumps a tool's call count (a kind is created on first sight).
func (st *SessionStats) addTool(name string, n int) {
	for i := range st.ToolKinds {
		if st.ToolKinds[i].Name == name {
			st.ToolKinds[i].Calls += n
			return
		}
	}
	st.ToolKinds = append(st.ToolKinds, ToolCount{Name: name, Calls: n})
}

// finish computes the derived ordering (tool ranking) and is the shared tail
// of every builder.
func (st *SessionStats) finish() {
	sort.Slice(st.ToolKinds, func(i, j int) bool {
		if st.ToolKinds[i].Calls != st.ToolKinds[j].Calls {
			return st.ToolKinds[i].Calls > st.ToolKinds[j].Calls
		}
		return st.ToolKinds[i].Name < st.ToolKinds[j].Name
	})
}

// dedupeNames drops repeats from a tool-name list, keeping first-seen order.
func dedupeNames(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, n := range in {
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// ToolCost is one tool's marginal money share (the per-tool cost ranking).
type ToolCost struct {
	Name  string  `json:"name"`
	Calls int     `json:"calls"`
	Yuan  float64 `json:"yuan"`
	Pct   float64 `json:"pct"` // share of the priced rounds' money, 0-100
}

// ToolCosts ranks the tools by marginal money: each round's 元 (at the
// session's resolved rate card) is split EQUALLY among the distinct tools the
// round called — a round's tokens pay for the whole shared context, so an
// equal split is the honest "marginal cost per round where this tool
// participated" (LangSmith/Langfuse per-trace aggregation idea, adapted: per
// trace they attribute cost to the trace root; gopi has no trace tree, so the
// round is the attribution unit). Rounds with no tool call pool into the
// "(no tool call)" residual so the ranking always sums to the box total.
// Requires a priceable session (the caller resolves ComputeCost first);
// returns nil when unpriced. st.RoundStats nil (aggregate) → nil.
func (st *SessionStats) ToolCosts() []ToolCost {
	if st.Cost == nil || len(st.RoundStats) == 0 {
		return nil
	}
	acc := map[string]float64{}
	calls := map[string]int{}
	var residual float64
	for i := range st.RoundStats {
		r := &st.RoundStats[i]
		if r.PromptTokens == 0 && r.CompletionTokens == 0 {
			continue // no-usage rounds (probe / error) cost nothing
		}
		row := st.Cost.Price.CacheHit*float64(r.CacheHit)/pricing.Unit +
			st.Cost.Price.Input*float64(r.CacheMiss)/pricing.Unit +
			st.Cost.Price.Output*float64(r.CompletionTokens)/pricing.Unit
		names := dedupeNames(r.Tools)
		if len(names) == 0 {
			residual += row
			continue
		}
		share := row / float64(len(names))
		for _, n := range names {
			acc[n] += share
			calls[n]++
		}
	}
	total := 0.0
	for _, v := range acc {
		total += v
	}
	total += residual
	out := make([]ToolCost, 0, len(acc)+1)
	for n, v := range acc {
		pct := 0.0
		if total > 0 {
			pct = v / total * 100
		}
		out = append(out, ToolCost{Name: n, Calls: calls[n], Yuan: v, Pct: pct})
	}
	if residual > 0 {
		out = append(out, ToolCost{Name: "(no tool call)", Calls: 0, Yuan: residual, Pct: residual / total * 100})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Yuan != out[j].Yuan {
			return out[i].Yuan > out[j].Yuan
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// minNonZero keeps the smallest positive value seen (0 = unset).
func minNonZero(cur, n int) int {
	if n <= 0 {
		return cur
	}
	if cur == 0 || n < cur {
		return n
	}
	return cur
}

func maxInt(a, b int) int {
	if b > a {
		return b
	}
	return a
}

// UsageRound is one JSONL request event's usage sample (the raw-log fallback's
// per-round row). The JSONL request event carries prompt/cache-hit/cache-miss
// and completion tokens only — there is no cache-creation field, so a
// jsonl-sourced stats block always reports cache write 0.
type UsageRound struct {
	Iteration        int
	PromptTokens     int
	CacheHit         int
	CacheMiss        int
	CompletionTokens int
	// Tools are the tool_use names logged by the assistant events that
	// preceded this request event (first-seen order, deduplicated).
	Tools []string
}

// SessionUsage is the usage-shaped scan of a session JSONL: only the events
// that carry numbers (session_start / request / assistant tool_use /
// session_end) — cheap enough to run across every session for --all.
type SessionUsage struct {
	SessionID  string
	Model      string
	Workdir    string
	RawLogPath string
	Started    time.Time
	// ElapsedSec comes from the session_end event (HasElapsed false when the
	// session never ended — a live or crashed run).
	ElapsedSec float64
	HasElapsed bool
	// TotalIterations is the SUM of every `session_end` total_iterations in the
	// log — one per run segment — or 0 when the session recorded no end. It is
	// a sum and not the last segment's number, because each end reports only
	// its own run and a resume that ran no turn writes 0 (task 01a17ae2,
	// measured on session 4a0957a3-bb4e-4337-a761-20606c87d1fd: 29 then 0).
	// It is NEVER filled with len(u.Rounds): a round is one `request` row and a
	// loop pass is the REPL turn the loop ran, and folding the first into the
	// second is the defect task 01a17492 measured (a session whose
	// `session log` printed "Iterations: 468 / Passes: 468"). A caller that
	// renders a pass count branches on HasIterations.
	TotalIterations int
	// HasIterations is whether session_end actually reported a pass count.
	HasIterations bool
	// PassSegments is how many `session_end` events fed TotalIterations — the
	// SPAN of the count (1 for a session never resumed, 2+ for a resumed one).
	PassSegments int
	Rounds       []UsageRound
}

// ScanSessionUsage reads a session JSONL and returns its usage-shaped events.
// Ordering note: a round's assistant tool_use events are logged BEFORE that
// round's request event (the request event records the completed API call),
// so tool names accumulate between request events and attach to the request
// that follows them.
func ScanSessionUsage(path string) (*SessionUsage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	u := &SessionUsage{}
	var pending []string
	sc := newJSONLScanner(path, f, jsonlLineSafetyCeiling)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // a torn trailing line must not kill the whole scan
		}
		switch ev.Type {
		case sessionevent.EventTypeSessionStart:
			u.SessionID = ev.SessionID
			u.Model = ev.Model
			u.Workdir = ev.Workdir
			u.RawLogPath = ev.RawLogPath
			u.Started = ev.Timestamp
		case "assistant":
			blocks := parseContentBlocks(ev.Message.Content)
			for _, b := range blocks {
				if b.Type != "tool_use" || b.Name == "" {
					continue
				}
				pending = append(pending, b.Name)
			}
		case "request":
			u.Rounds = append(u.Rounds, UsageRound{
				Iteration:        ev.Iteration,
				PromptTokens:     ev.PromptTokens,
				CacheHit:         ev.CacheHit,
				CacheMiss:        ev.CacheMiss,
				CompletionTokens: ev.CompletionTokens,
				Tools:            append([]string(nil), pending...),
			})
			pending = nil
		case sessionevent.EventTypeSessionEnd:
			// Summed over the log's run segments, like TotalIterations below:
			// the session's wall time is not its LAST segment's (task 01a17ae2).
			u.ElapsedSec += ev.ElapsedSec
			u.HasElapsed = true
			// ACCUMULATED over the log's run segments, never the last one's
			// number (task 01a17ae2): `gopi session stats` made the same
			// last-writer-wins assignment as the full reader, so a resumed
			// session whose last segment ran no turn reported 0 passes.
			u.TotalIterations += ev.TotalIterations
			u.PassSegments++
			u.HasIterations = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return u, nil
}

// SessionWallSeconds reads a session JSONL's session_end elapsed_sec without
// parsing the conversation. ok is false when the session never ended (live or
// crashed run) or the file is unreadable.
func SessionWallSeconds(path string) (float64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()

	sc := newJSONLScanner(path, f, jsonlLineSafetyCeiling)
	// session_end is the LAST event a session writes, so there is no early
	// exit — only a cheap substring pre-filter before the decode.
	var sec float64
	found := false
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, sessionEndProbe) {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Type == sessionevent.EventTypeSessionEnd {
			sec = ev.ElapsedSec
			found = true
		}
	}
	// A session that could not be read has no trustworthy wall time: report it
	// as absent rather than as zero seconds. The reason is deliberately not
	// swallowed at the SOURCE — the reader's named error (jsonlLineTooLongError)
	// reaches the caller through the primary path (ParseFile, `gopi session log`); this
	// summarising helper is a best-effort listing probe whose ok=false already
	// means "no value here".
	if sc.Err() != nil {
		return 0, false
	}
	return sec, found
}

// SessionStarted reads a session JSONL's session_start timestamp — the
// moment `gopi session stats --cost` prices usage at (date/time-aware). ok is false
// when the file is unreadable or has no session_start.
func SessionStarted(path string) (time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()

	sc := newJSONLScanner(path, f, jsonlLineSafetyCeiling)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, sessionStartProbe) {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Type == sessionevent.EventTypeSessionStart && !ev.Timestamp.IsZero() {
			return ev.Timestamp, true
		}
	}
	// ok=false covers both "no session_start" and "unreadable at the line
	// ceiling"; the named reader error reaches the caller via ParseFile.
	return time.Time{}, false
}

// sessionStartProbe is the pre-filter for the session_start scan (the
// session_start event is the FIRST line of the JSONL — the probe keeps the
// scan cheap and the decode rare).
var sessionStartProbe = []byte(`"` + sessionevent.EventTypeSessionStart + `"`)

// sessionEndProbe is the pre-filter for the session_end scan: every event line
// carries its type as a compact JSON string value, so a line that cannot
// contain the literal cannot be a session_end event. A content string that
// embeds the literal only causes one extra (harmless) decode, since the
// decoded ev.Type is what decides.
var sessionEndProbe = []byte(`"` + sessionevent.EventTypeSessionEnd + `"`)

// BuildStatsFromUsage derives session statistics from the session JSONL's
// request events (the no-raw-log fallback). Round numbers stay the JSONL
// iteration numbers (0-based) — they are never presented as raw ordinals.
func BuildStatsFromUsage(u *SessionUsage) *SessionStats {
	st := &SessionStats{
		Source:          StatsSourceJSONL,
		Sessions:        1,
		SessionID:       u.SessionID,
		Model:           u.Model,
		LargestCliffIdx: -1,
		Started:         u.Started,
	}
	prev := -1
	for i := range u.Rounds {
		r := &u.Rounds[i]
		// THE NUMBER IS THE CANONICAL ROUND, not the log's `iteration` field:
		// that field is the agent loop's counter and it RESTARTS at 0 for every
		// new prompt in one session (measured: 0..20 repeating on a session with
		// 321 request rows), so printing it as a round number made two different
		// exchanges share a number and the table's last row read "28" for round
		// 321. ScanSessionUsage appends one UsageRound per `request` row in log
		// order, so the row's 1-based position IS the canonical ordinal — the
		// same one BuildStatsFromJSONL assigns and `gopi session log --rounds`
		// prints.
		st.RoundStats = append(st.RoundStats, RoundStat{
			Number:           i + 1,
			PromptTokens:     r.PromptTokens,
			CacheHit:         r.CacheHit,
			CacheMiss:        r.CacheMiss,
			CompletionTokens: r.CompletionTokens,
			Tools:            append([]string(nil), r.Tools...),
			ToolCalls:        len(r.Tools),
		})
		st.Rounds++
		if r.PromptTokens == 0 {
			st.NoUsage++
			continue
		}
		st.accumulate(len(st.RoundStats)-1, prev)
		prev = len(st.RoundStats) - 1
	}
	if u.HasElapsed {
		st.WallSeconds = u.ElapsedSec
		st.HasWall = true
	}
	st.finish()
	return st
}

// AggregateStats sums several sessions' statistics into one --all block.
// Per-round rows are dropped (an aggregate has no round numbering); the
// per-round min/max, the tool ranking and the cliff count stay meaningful.
func AggregateStats(list []*SessionStats) *SessionStats {
	agg := &SessionStats{
		Source:          StatsSourceAggregate,
		Sessions:        len(list),
		LargestCliffIdx: -1,
	}
	for _, st := range list {
		agg.Rounds += st.Rounds
		agg.RoundsCounted += st.RoundsCounted
		agg.Probes += st.Probes
		agg.NoUsage += st.NoUsage
		agg.NoUsage += st.NoUsage
		agg.PromptTokens += st.PromptTokens
		agg.CacheHit += st.CacheHit
		agg.CacheMiss += st.CacheMiss
		agg.CacheWrite += st.CacheWrite
		agg.Completion += st.Completion
		agg.ToolCalls += st.ToolCalls
		agg.CliffCount += st.CliffCount
		agg.RawBytes += st.RawBytes
		if st.HasWall {
			agg.WallSeconds += st.WallSeconds
			agg.HasWall = true
		}
		agg.MinPrompt = minNonZero(agg.MinPrompt, st.MinPrompt)
		agg.MaxPrompt = maxInt(agg.MaxPrompt, st.MaxPrompt)
		if st.LargestCliff < agg.LargestCliff {
			agg.LargestCliff = st.LargestCliff
		}
		for _, tk := range st.ToolKinds {
			agg.addTool(tk.Name, tk.Calls)
		}
		switch {
		case agg.Model == "":
			agg.Model = st.Model
		case agg.Model != st.Model:
			agg.Model = "(mixed)"
		}
	}
	agg.finish()
	return agg
}

// ToolRanking returns the tool ranking sorted by calls (desc, then name).
func (st *SessionStats) ToolRanking() []ToolCount {
	out := append([]ToolCount(nil), st.ToolKinds...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Calls != out[j].Calls {
			return out[i].Calls > out[j].Calls
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// --- rendering -------------------------------------------------------------

// humanTokens renders a token count compactly for a table column
// (13170 → "13.2K", 106139 → "103.7K", 32861953 → "31.33M").
func humanTokens(n int) string {
	switch {
	case n >= tokenMegaThreshold:
		return fmt.Sprintf("%.2fM", float64(n)/tokenMegaThreshold)
	case n >= tokenKiloThreshold:
		return fmt.Sprintf("%.1fK", float64(n)/tokenKiloThreshold)
	default:
		return strconv.Itoa(n)
	}
}

// signedTokens renders a signed Δreq with the table's compaction.
func signedTokens(n int) string {
	switch {
	case n > 0:
		return "+" + humanTokens(n)
	case n < 0:
		return "-" + humanTokens(-n)
	default:
		return "0"
	}
}

// humanTokensAuto renders a token count for the overview box: verbatim while
// it stays readable, then compact (32,520,000 → "32.52M", 337,000 → "337K").
func humanTokensAuto(n int) string {
	switch {
	case n >= tokenMegaThreshold:
		return fmt.Sprintf("%.2fM", float64(n)/tokenMegaThreshold)
	case n >= tokenKiloThreshold:
		return fmt.Sprintf("%.0fK", float64(n)/tokenKiloThreshold)
	default:
		return commaInt(n)
	}
}

// commaInt renders n with thousands separators (32861953 → "32,861,953").
func commaInt(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var sb strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		sb.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if sb.Len() > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(s[i : i+3])
	}
	return sb.String()
}

// humanWall renders an elapsed-seconds value the way the overview shows it
// (2680.97 → "44m41s", 45.2 → "45s").
func humanWall(sec float64) string {
	if sec < statsWallSecondsPerMin {
		return fmt.Sprintf("%.0fs", sec)
	}
	total := int(sec + 0.5)
	return fmt.Sprintf("%dm%ds", total/statsWallSecondsPerMinuteInt, total%statsWallSecondsPerMinuteInt)
}

const statsWallSecondsPerMinuteInt = int(statsWallSecondsPerMin)

// The SPAN of the usage numbers, and the word it is stated under (Rule 1/15).
const (
	// usageSpanKey is the row label. It is a key in the same column as
	// "prompt tokens", so a reader meets the span beside the number it qualifies.
	usageSpanKey = "usage span"
	// usageSpanOneRun / usageSpanRunsFmt render the span. One run prints
	// nothing (there is no span to state — the session ran once), which is why
	// the single-run case is a constant and not an omitted branch.
	usageSpanRunsFmt = "%d runs summed (a resumed session restarts its counters; each run counts its own last reading)"
	// usageSpanUnknownFmt states how many rows the fold left out.
	usageSpanUnknownFmt = "%d request row(s) NOT counted: their quantity was never established"
	// usageSpanJoiner joins the two facts when both apply.
	usageSpanJoiner = " · "
)

// usageSpanRow renders the span of st.PromptTokens / st.Completion as ONE row
// for the overview box, or "" when there is nothing to state.
//
// TWO FACTS PRINT, and each is a correction of a real misreading:
//
//  1. A RESUMED session's totals are a SUM OVER RUNS (measured: chat session
//     66243525-16ab-4ce7-9133-84d74c539840 holds three, 44.2M + 40.7M + 310.9M
//     prompt). Without this row the headline reads as one conversation's
//     context, which no single context in that log ever was.
//  2. ROWS THE FOLD LEFT OUT are named. A count that quietly skipped rows is
//     the same class of lie as summing a cumulative column (Rule 15).
func (st *SessionStats) usageSpanRow() string {
	span := usageSpanText(st.UsageSegments, st.UsageUnknown)
	if span == "" {
		return ""
	}
	return fmt.Sprintf("%-*s%s", statsKeyWidth, usageSpanKey, span)
}

// usageSpanText is the ONE wording of the span, shared by the overview box
// (usageSpanRow) and by the COST SUMMARY of `gopi session log`
// (formatter.go) — the two surfaces that print the counted totals.
func usageSpanText(segments, unknown int) string {
	parts := make([]string, 0, 2)
	if segments > 1 {
		parts = append(parts, fmt.Sprintf(usageSpanRunsFmt, segments))
	}
	if unknown > 0 {
		parts = append(parts, fmt.Sprintf(usageSpanUnknownFmt, unknown))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, usageSpanJoiner)
}

// statsSourceLabel names the source line of the overview box.
func (st *SessionStats) statsSourceLabel() string {
	switch st.Source {
	case StatsSourceRaw:
		return "raw log " + orDash(st.SourceDetail)
	case StatsSourceJSONL:
		name := st.SourceDetail
		if name == "" {
			name = "session jsonl"
		}
		if st.WireLogDetail != "" {
			// The bodies came from the wire: say so, because the ROUND NUMBERS
			// did not (they are the session's own `request` rows).
			return "session jsonl " + name + " (wire bodies from " + st.WireLogDetail + ")"
		}
		return "session jsonl " + name + " (no raw log)"
	case StatsSourceAggregate:
		// --all sums the JSONL request events (the raw corpus is tens of GB);
		// say so, because the numbers then include guard-probe rounds.
		return fmt.Sprintf("%d sessions (jsonl totals)", st.Sessions)
	}
	return st.Source
}

// roundBasis states the numbering basis once per table, so one source's
// ordinal is never mistaken for the other's (IMPLEMENT-SPEC §1).
//
// EVERY TABLE THAT PRINTS IT COUNTS ROUNDS, so the basis says "round" and
// takes the quantity's own word from sessionevent (Rule 1/13/14: the unit's
// name has one definition and a consumer references it — a literal here would
// keep the old spelling through the next rename and this surface would silently
// disagree with `gopi session log --rounds`). The word ROUND is this unit
// (sessionevent.CounterUnitRound) and nothing else; the operator-row quantity is
// the neighbour `turns` (sessionevent.CounterUnitHumanTurn), which these tables do
// not count.
//
// The JSONL basis is the CANONICAL round — the same one every session
// surface numbers (one `request` row = one HTTP request/response, 1-based):
// BuildStatsFromJSONL assigns RoundStat.Number from exactly that ordinal, so a
// table row, a `gopi session log --rounds` row and `--round N` all name the same
// round. The raw basis is the raw log's own `→ REQUEST` ordinal, 1-based over
// a LARGER corpus (it also numbers sends the JSONL never recorded).
func (st *SessionStats) roundBasis() string {
	if st.Source == StatsSourceJSONL {
		return sessionevent.CounterUnitRound + ", 1-based (" + sessionRoundCanonicalUnit + ")"
	}
	return sessionevent.CounterUnitRawRequest + " ordinal, 1-based"
}

// roundNoun is the short numbering noun of the source's numbering — the word
// the overview's cliff line prints before the number it names ("@ round 2").
// It names WHAT THE ORDINAL COUNTS, which is not the same corpus on the two
// sources: the JSONL's ordinals are the canonical rounds
// (sessionevent.CounterUnitRound), the raw log's are its own
// `→ REQUEST` markers (sessionevent.CounterUnitRawRequest, a larger corpus that
// also numbers sends the JSONL never recorded). Borrowing roundBasis's full
// sentence here would bury the number, and calling both "round" is the defect
// this surface avoids by naming the raw one.
func roundNoun(source string) string {
	if source == StatsSourceJSONL {
		return roundWord()
	}
	return sessionevent.CounterUnitRawRequest
}

// FormatStatsOverview renders the one-session aggregate box.
func FormatStatsOverview(st *SessionStats) string {
	avg := 0
	if st.RoundsCounted > 0 {
		avg = st.PromptTokens / st.RoundsCounted
	}

	var rows []string
	rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, "model", orDash(st.Model)))
	rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, "source", st.statsSourceLabel()))
	var caveats []string
	if st.Probes > 0 {
		caveats = append(caveats, fmt.Sprintf("%d probe", st.Probes))
	}
	if st.NoUsage > 0 {
		caveats = append(caveats, fmt.Sprintf("%d without usage", st.NoUsage))
	}
	// TWO ROWS, TWO WORDS: the TOTAL under `turns` (the ledger's
	// `chats.turn_count`, through the injected reader) and the PERSON-ONLY
	// SUBSET under `human turns`. Printing the subset under the bare word is
	// exactly the collision this epic removes (measured on
	// 41e5dade-71e1-4efc-a012-456d0532e762: 66 under `turns` here against 200 in
	// the ledger); printing ONLY the subset, as this block used to, left the same
	// reader comparing two screens with no way to tell which number answered
	// "how many turns ran".
	//
	// A count that was not read says so, with its reason, under the same word
	// (Rule 15) — never a 0, and never the subset standing in for it.
	//
	// THE LINE EXISTS ONLY WHERE A TURN CAN EXIST AT ALL: the SESSION JSONL
	// (whose rows carry the input faces and whose id the ledger is keyed on).
	// A raw-log or usage-scan block has no turn boundary to count, and an
	// aggregate is several sessions' columns — `chats.turn_count` is ONE
	// session's number, and a sum over sessions is a quantity nobody measured.
	// Those shapes keep the round headline they always had, so the box's first
	// row still names the quantity the block actually holds.
	if st.Source == StatsSourceJSONL && st.Sessions == 1 {
		// THE BARE WORD CARRIES THE LOG'S OPENED COUNT (epic 2026-11-04): it is
		// the axis `--turn N` and every replay band number along, so this row and
		// those surfaces can never disagree about "how many turns".
		rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, sessionevent.CounterUnitTurn, strconv.Itoa(st.TurnsOpened)))
		// THE LEDGER'S COMPLETED COUNT, UNDER ITS OWN WORD — AND WHEN IT COULD
		// NOT BE READ, THE ABSENCE IS STATED ON THAT SAME ROW (Rule 15: an
		// unread number is a word plus its reason, never a silent omission and
		// never a fabricated 0). It rides `completed` and not the bare word,
		// because the bare word's count above is the parsed session's own and is
		// always measured — the ledger is the ONLY one of the pair that can fail
		// to be read (epic 2026-11-04).
		//
		// IT PRINTS ONLY WHEN IT DIFFERS from the opened count: on a session that
		// closed every turn it opened the two are equal, and a second identical
		// number under a second word is noise a reader learns to skip.
		switch {
		case !st.TurnsMeasured:
			rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, sessionevent.CounterUnitTurnCompleted,
				TurnCountUnmeasuredPrefix+st.TurnCountNote))
		case st.TurnCount != st.TurnsOpened:
			rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, sessionevent.CounterUnitTurnCompleted, strconv.Itoa(st.TurnCount)))
		}
	}
	if st.HumanTurnsUnmeasured {
		// AN UNMEASURED SUBSET IS PRINTED AS UNMEASURED, on its own row under its
		// own word, and the block then takes the round headline it uses for a
		// source with no turn detail at all — because that is exactly what this
		// is (Rule 15: the number it used to print here was the ROUND count of
		// the log, session 794cbd02).
		rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, sessionevent.CounterUnitHumanTurn,
			TurnCountUnmeasuredPrefix+st.HumanTurnsNote))
	} else if st.HumanTurns > 0 {
		detail := fmt.Sprintf(statsTurnDetailFmt, st.Rounds)
		if len(caveats) > 0 {
			detail += " (" + strings.Join(caveats, ", ") + ")"
		}
		rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, sessionevent.CounterUnitHumanTurn, strconv.Itoa(st.HumanTurns)))
		// THE RESIDUAL, NAMED. Without it the two rows above are two numbers a
		// reader must subtract — and can only read as a disagreement (measured
		// 2026-11-01: `turns 329` over `human turns 83` with nothing saying where
		// 246 came from). The residual is DERIVED here, never stored, and it is
		// printed only when the TOTAL above is a measurement too, so the two can
		// never fail to account for each other.
		// THE RESIDUAL OF THE TOTAL PRINTED ABOVE — which is the OPENED count, so
		// it is derived from st.TurnsOpened, not from the ledger's completed count
		// (30 - 17 = 13 here; the ledger's 29 would have said 12 and failed to
		// account for the row it sits under).
		if st.TurnsOpened >= st.HumanTurns {
			rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, sessionevent.CounterUnitOtherTurn,
				strconv.Itoa(st.TurnsOpened-st.HumanTurns)))
		}
		rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, sessionevent.CounterUnitInnerRequest, detail))
		rows = append(rows, fmt.Sprintf("%-*s%s   (avg %s/%s)", statsKeyWidth, "prompt tokens",
			commaInt(st.PromptTokens), commaInt(avg), sessionevent.CounterUnitInnerRequestOne))
		if span := st.usageSpanRow(); span != "" {
			rows = append(rows, span)
		}
		// The per-row min/max are per REQUEST here, so the row says so: the
		// aggregates are folded over rounds, and calling them "per-turn" would
		// claim an average the number does not carry (Rule 15).
		rows = append(rows, fmt.Sprintf("%-*smin %s │ max %s", statsKeyWidth,
			"  per-"+sessionevent.CounterUnitInnerRequestOne,
			commaInt(st.MinPrompt), commaInt(st.MaxPrompt)))
	} else {
		rounds := strconv.Itoa(st.Rounds)
		if len(caveats) > 0 {
			rounds += " (" + strings.Join(caveats, ", ") + ")"
		}
		rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, sessionevent.CounterUnitRound, rounds))
		rows = append(rows, fmt.Sprintf("%-*s%s   (avg %s/%s)", statsKeyWidth, "prompt tokens",
			commaInt(st.PromptTokens), commaInt(avg), roundWord()))
		if span := st.usageSpanRow(); span != "" {
			rows = append(rows, span)
		}
		rows = append(rows, fmt.Sprintf("%-*smin %s │ max %s", statsKeyWidth, "  per-"+roundWord(),
			commaInt(st.MinPrompt), commaInt(st.MaxPrompt)))
	}
	cache := fmt.Sprintf("%.2f%% hit (%s) / %s miss", st.cacheHitPct(),
		humanTokensAuto(st.CacheHit), humanTokensAuto(st.CacheMiss))
	if st.CacheWrite > 0 {
		cache += fmt.Sprintf(" · %s written", humanTokensAuto(st.CacheWrite))
	}
	rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, "cache", cache))
	rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, "completion", commaInt(st.Completion)))
	rows = append(rows, fmt.Sprintf("%-*s%d calls / %d kinds", statsKeyWidth, "tools",
		st.ToolCalls, len(st.ToolKinds)))

	cliffs := "0"
	if st.CliffCount > 0 {
		cliffs = fmt.Sprintf("%d (largest %s", st.CliffCount, signedTokens(st.LargestCliff))
		if st.LargestCliffIdx >= 0 {
			cliffs += fmt.Sprintf(" @ %s %d", roundNoun(st.Source), st.RoundStats[st.LargestCliffIdx].Number)
		}
		cliffs += ")"
	}
	rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, "trim cliffs", cliffs))

	wall := "n/a (no session_end)"
	if st.HasWall {
		wall = humanWall(st.WallSeconds)
	}
	rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, "wall time", wall))
	if st.RawBytes > 0 {
		rows = append(rows, fmt.Sprintf("%-*s%s", statsKeyWidth, "raw log size", humanBytes(int(st.RawBytes))))
	}
	return statsBox(statsTitle(st), rows)
}

// statsTitle is the overview box's title line.
func statsTitle(st *SessionStats) string {
	if st.Source == StatsSourceAggregate {
		return fmt.Sprintf("All sessions (%d)", st.Sessions)
	}
	id := st.SessionID
	if id == "" {
		id = st.SourceDetail
	}
	return "Session " + id
}

// The frame's geometry (PROJECT.md Rule 1/13/14: a width is a named constant,
// never a literal). Every line of a box is W + statsBoxFrameOverhead runes wide,
// where W is the widest content column: a title line and a row each spend one
// corner glyph and one space on each side, and the bottom rule spends two corner
// glyphs and nothing else — so its dash run is the content column minus the
// corners the line already carries.
const (
	// statsBoxFrameOverhead is what a frame line carries BESIDES its W-rune
	// content column ("┌ " … " ┐", "│ " … " │", "└" … "┘").
	statsBoxFrameOverhead = 4
	// statsBoxCornerPair is the two corner glyphs a frame line spends; a line
	// with no inner spaces (the bottom rule) keeps W + overhead − corners
	// dashes, which is what lands it on the same width as the other two.
	statsBoxCornerPair = 2
)

// statsBox renders the ┌ title ─┐ / │ rows │ / └ ─┘ frame around the given
// rows. The border adapts to the widest row so a long value can never spill.
// Width arithmetic (rune counts — the frame uses full-width box glyphs), all
// three lines landing on the SAME width:
//
//	top    "┌ " + title + " " + ─*(W-len(title))  + "┐"   = W+4 runes
//	row    "│ " + row   + " "*(W-len(row))        + " │"  = W+4 runes
//	bottom "└"   + ─*(W+4-2)                      + "┘"   = W+4 runes
//
// The bottom rule's dash count was W+3 until 2026-10-27 (task 01a17b5f), which
// made it the only line one rune wider than the frame — the snapshots had
// recorded that drift (stats-overview.txt 123/124, stats-overview-jsonl.txt
// 88/89, stats-cost.txt 126/127), so the committed goldens were the evidence
// OF the defect, not a specification of it. This comment's claim (three lines,
// one width) is the design; the arithmetic above is now what the code does.
func statsBox(title string, rows []string) string {
	w := len([]rune(title))
	for _, r := range rows {
		if n := len([]rune(r)); n > w {
			w = n
		}
	}

	var sb strings.Builder
	sb.WriteString("┌ " + title + " " + strings.Repeat("─", maxInt(0, w-len([]rune(title)))) + "┐\n")
	for _, r := range rows {
		sb.WriteString("│ " + r + strings.Repeat(" ", maxInt(0, w-len([]rune(r)))) + " │\n")
	}
	sb.WriteString("└" + strings.Repeat("─", maxInt(0, w+statsBoxFrameOverhead-statsBoxCornerPair)) + "┘\n")
	return sb.String()
}

// anyRoundHasTurn reports whether any row carries a turn number, i.e. whether the
// source recorded operator boundaries at all. It decides whether the table prints
// the TURN column rather than printing a column of dashes.
func anyRoundHasTurn(rows []RoundStat) bool {
	for i := range rows {
		if rows[i].Turn > 0 {
			return true
		}
	}
	return false
}

// FormatStatsRounds renders the per-round table (--rounds). Rows keep every
// round (probe rounds and rounds without usage included) so the numbering is
// gap-free and a `gopi session raw <id> <round>` built from it always resolves.
func FormatStatsRounds(st *SessionStats) string {
	var sb strings.Builder
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("PER-%s — %s (%s)\n", sessionRoundColumnWord, statsTitle(st), st.roundBasis()))
	sb.WriteString(headerSep + "\n")
	if len(st.RoundStats) == 0 {
		sb.WriteString("(no " + roundsWord() + ")\n")
		return sb.String()
	}
	// THE TURN COLUMN LEADS: every row says which ASK this exchange answered, so
	// the table reads as the detail of the turns above it (operator's ruling,
	// 2026-10-22). It is filled only for a jsonl source (a raw log records no
	// operator rows), and the column is present either way so the reader is not
	// left wondering where it went — an empty cell states "unknown", a missing
	// column would state nothing.
	turnCol := anyRoundHasTurn(st.RoundStats)
	if turnCol {
		sb.WriteString(fmt.Sprintf("%5s %8s %9s %10s %7s %4s %-22s %s\n",
			sessionTurnColumnWord, sessionRoundColumnWord, "PROMPT", "Δreq", "CACHE%", "N", "TOOLS", "STOP"))
	} else {
		sb.WriteString(fmt.Sprintf("%8s %9s %10s %7s %4s %-22s %s\n",
			sessionRoundColumnWord, "PROMPT", "Δreq", "CACHE%", "N", "TOOLS", "STOP"))
	}
	cliffNote := false
	for i := range st.RoundStats {
		r := &st.RoundStats[i]
		num := strconv.Itoa(r.Number)
		if r.Probe {
			num += "p"
		}
		prompt := "-"
		if r.PromptTokens > 0 {
			prompt = humanTokens(r.PromptTokens)
		}
		delta := "-"
		if r.DeltaReq != 0 {
			delta = signedTokens(r.DeltaReq)
			if r.Cliff {
				delta += "*"
				cliffNote = true
			}
		}
		cache := "-"
		if total := r.CacheHit + r.CacheMiss; total > 0 {
			cache = fmt.Sprintf("%.1f", float64(r.CacheHit)/float64(total)*100)
		}
		tools := "-"
		if len(r.Tools) > 0 {
			names := dedupeNames(r.Tools)
			if len(names) > statsRoundToolsMax {
				names = append(append([]string(nil), names[:statsRoundToolsMax]...),
					fmt.Sprintf("+%d", len(r.Tools)-statsRoundToolsMax))
			}
			tools = strings.Join(names, ",")
		}
		stop := orDash(r.StopReason)
		if r.Probe {
			stop = "probe"
		}
		turn := "-"
		if r.Turn > 0 {
			turn = strconv.Itoa(r.Turn)
		}
		if turnCol {
			sb.WriteString(fmt.Sprintf("%5s %8s %9s %10s %7s %4d %-22s %s\n",
				turn, num, prompt, delta, cache, r.ToolCalls, tools, stop))
		} else {
			sb.WriteString(fmt.Sprintf("%8s %9s %10s %7s %4d %-22s %s\n",
				num, prompt, delta, cache, r.ToolCalls, tools, stop))
		}
	}
	if cliffNote {
		sb.WriteString("(* = trim cliff — Δreq vs the previous counted " + roundWord() + ")\n")
	}
	if turnCol {
		sb.WriteString("(" + sessionTurnColumnWord + " = the TURN this " + roundWord() +
			" answered — one operator input to the next; a turn routinely spans several " + roundsWord() + ")\n")
	}
	return sb.String()
}

// CostKeyWidth / cost table formatting constants (PROJECT.md Rule 1 — display
// widths are named constants, not inline magic).
const (
	costKeyWidth = 16 // width of the key column in the COST box
)

// FormatStatsCost renders the real-money box (`gopi session stats --cost`): the
// session's total 元 split into its three billable dimensions (cache-hit /
// input-miss / output), each with its money share, plus the regime + price
// provenance that produced the number (epic hard rule: money must be
// traceable to its source/date). The price is resolved at the session's
// Started moment (date/time-aware). err is non-nil when the session has no
// priceable model — the caller surfaces it as the reason no money is shown.
func FormatStatsCost(st *SessionStats) (string, error) {
	if !st.ComputeCost(st.Started) {
		return "", fmt.Errorf("cannot price model %q (unknown provider — no price table; see internal/pricing)", orDash(st.Model))
	}
	c := st.Cost
	var rows []string
	rows = append(rows, fmt.Sprintf("%-*s%s @ %s", costKeyWidth, "total cost",
		roundMoney(c.Total)+" 元", orDash(c.Currency)))
	rows = append(rows, fmt.Sprintf("%-*s%s  (%s tokens)", costKeyWidth, "  cache hit",
		roundMoney(c.Hit.Yuan)+" 元", humanTokensAuto(int(c.Hit.Tokens))))
	rows = append(rows, fmt.Sprintf("%-*s%s  (%s tokens)", costKeyWidth, "  input miss",
		roundMoney(c.Miss.Yuan)+" 元", humanTokensAuto(int(c.Miss.Tokens))))
	rows = append(rows, fmt.Sprintf("%-*s%s  (%s tokens)", costKeyWidth, "  output",
		roundMoney(c.Output.Yuan)+" 元", humanTokensAuto(int(c.Output.Tokens))))
	rows = append(rows, fmt.Sprintf("%-*s%5.1f%% / %5.1f%% / %5.1f%%", costKeyWidth, "money split",
		c.Hit.Pct, c.Miss.Pct, c.Output.Pct))
	rows = append(rows, fmt.Sprintf("%-*s%s", costKeyWidth, "regime", orDash(c.Regime)))
	// The n² amplifier, in money: cache-hit spend ÷ miss spend. Where the
	// token-count n² (overview) says 22x, money says 0.7x on DS — because a
	// cache-hit token costs 1/30 of a miss. Naming both tells the full story.
	amp := "-"
	if c.Amplification > 0 {
		amp = fmt.Sprintf("%.2fx (cache-hit spend is %.2fx the miss spend — tokens are %.1fx in count but the hit price is far lower)",
			c.Amplification, c.Amplification, st.cacheAmpTokens())
	}
	rows = append(rows, fmt.Sprintf("%-*s%s", costKeyWidth, "n² amp (money)", amp))
	// Provenance: WHEN it was priced + the source doc.
	priced := "unknown"
	if !c.PricedAt.IsZero() {
		priced = c.PricedAt.In(pricing.Beijing()).Format("2006-01-02 15:04 -0700") + " Beijing"
	}
	rows = append(rows, fmt.Sprintf("%-*s%s", costKeyWidth, "priced at", priced))
	rows = append(rows, fmt.Sprintf("%-*s%s", costKeyWidth, "price ref", orDash(c.Ref)))
	return statsBox("COST — "+statsTitle(st), rows) + "\n", nil
}

// cacheAmpTokens is the token-count cache amplification (hit÷miss) the
// overview shows as "22.4x" — the MONEY n² line compares against it.
func (st *SessionStats) cacheAmpTokens() float64 {
	if st.CacheMiss == 0 {
		return 0
	}
	return float64(st.CacheHit) / float64(st.CacheMiss)
}

// FormatStatsBill renders the per-round money table (`gopi session stats --bill`):
// each round's cache-hit / input-miss / output token volumes re-priced at the
// session's resolved rate card, plus a cumulative 元 total — the curve that
// shows WHERE the session's money went round by round. Requires --cost's
// resolution (a session must be priceable) and per-round rows (a single
// session, not an aggregate).
func FormatStatsBill(st *SessionStats) (string, error) {
	if len(st.RoundStats) == 0 {
		return "", nil
	}
	if !st.ComputeCost(st.Started) {
		return "", fmt.Errorf("cannot price model %q (unknown provider — no price table; see internal/pricing)", orDash(st.Model))
	}
	c := st.Cost
	var sb strings.Builder
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("PER-%s COST — %s (%s) · priced at %s (%s)\n",
		sessionRoundColumnWord, statsTitle(st), st.roundBasis(), c.Regime,
		c.PricedAt.In(pricing.Beijing()).Format("2006-01-02 15:04")))
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("%8s %10s %10s %9s %10s %12s\n",
		sessionRoundColumnWord, "HIT 元", "MISS 元", "OUT 元", statsRoundMoneyHeader, "CUM 元"))
	var cum float64
	for i := range st.RoundStats {
		r := &st.RoundStats[i]
		if r.PromptTokens == 0 && r.CompletionTokens == 0 {
			continue // no-usage rounds (probe / error) cost nothing
		}
		// Row money at the exact per-1M arithmetic; cum accumulates the
		// UNROUNDED row value so the curve's endpoint matches the box total
		// (rounding each row to 1e-6 first lets up to N×0.5e-6 of noise
		// accumulate and the last cell drifts a 4th-decimal hair).
		h := c.Price.CacheHit * float64(r.CacheHit) / pricing.Unit
		m := c.Price.Input * float64(r.CacheMiss) / pricing.Unit
		o := c.Price.Output * float64(r.CompletionTokens) / pricing.Unit
		row := h + m + o
		cum += row
		num := strconv.Itoa(r.Number)
		if r.Probe {
			num += "p"
		}
		sb.WriteString(fmt.Sprintf("%8s %10s %10s %9s %10s %12s\n",
			num, money4(h), money4(m), money4(o), money4(row), money4(cum)))
	}
	sb.WriteString(fmt.Sprintf("%8s %10s %10s %9s %10s %12s\n",
		"TOT", money4(c.Hit.Yuan), money4(c.Miss.Yuan), money4(c.Output.Yuan),
		money4(c.Total), money4(cum)))
	sb.WriteString("(元 per " + roundWord() + " at the session's rate card; * = trim cliff)\n")
	return sb.String(), nil
}

// FormatStatsToolCosts renders the per-tool marginal-money ranking (`gopi
// stats --bill` second block): which tools drove the session's spend. Money
// attribution is per-round equal-split (see ToolCosts) — a ranking is honest
// only when it sums back to the box total, so the footer states the covered
// share. Requires a priceable session with per-round rows.
func FormatStatsToolCosts(st *SessionStats) (string, error) {
	if len(st.RoundStats) == 0 {
		return "", nil
	}
	if !st.ComputeCost(st.Started) {
		return "", fmt.Errorf("cannot price model %q (unknown provider — no price table; see internal/pricing)", orDash(st.Model))
	}
	tc := st.ToolCosts()
	if len(tc) == 0 {
		return "", nil
	}
	var sb strings.Builder
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("PER-TOOL COST — %s · marginal 元 per %s where the tool ran\n", statsTitle(st), roundWord()))
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("%-28s %9s %8s %7s\n", "TOOL", "CALLS", "MARG 元", "SHARE"))
	for _, t := range tc {
		sb.WriteString(fmt.Sprintf("%-28s %9d %8s %6.1f%%\n",
			t.Name, t.Calls, money4(t.Yuan), t.Pct))
	}
	sb.WriteString("(share of the priced " + roundsWord() + "' money; a " + roundWord() + "'s 元 splits equally among the tools it called)\n")
	return sb.String(), nil
}

// money4 renders 元 to 4 decimals fixed (0.1579).
func money4(f float64) string { return strconv.FormatFloat(f, 'f', 4, 64) }

// FormatStatsTools renders the --tools ranking.
func FormatStatsTools(st *SessionStats) string {
	var sb strings.Builder
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("TOOL CALLS — %s · %d calls / %d kinds\n",
		statsTitle(st), st.ToolCalls, len(st.ToolKinds)))
	sb.WriteString(headerSep + "\n")
	rank := st.ToolRanking()
	if len(rank) == 0 {
		sb.WriteString("(no tool calls)\n")
		return sb.String()
	}
	for i, tk := range rank {
		if i == statsToolRankMax {
			sb.WriteString(fmt.Sprintf("… %d more kinds\n", len(rank)-i))
			break
		}
		sb.WriteString(fmt.Sprintf("%6d  %5.1f%%  %s\n", tk.Calls,
			float64(tk.Calls)/float64(st.ToolCalls)*100, tk.Name))
	}
	return sb.String()
}

// FormatStatsTraffic renders the --bytes body-traffic table: one row per
// round with the request-body wire bytes, the message count, the response
// wire bytes, the reassembled text/thinking characters and the response
// shape, then a TOTAL row. Rendered inline from the RoundStats the stats
// block already carries (no intermediate struct — the same style as
// FormatStatsRounds). Every round INCLUDING probe rounds and rounds without
// usage is folded in — body traffic happened whether or not the provider
// reported token usage, so a row never silently vanishes from the sum. The
// table is raw-source only: jsonl-sourced stats have no wire bodies and say
// so rather than print an empty box.
func FormatStatsTraffic(st *SessionStats) string {
	var sb strings.Builder
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("BODY TRAFFIC — %s (%s)\n", statsTitle(st), st.roundBasis()))
	sb.WriteString(headerSep + "\n")
	if len(st.RoundStats) == 0 {
		sb.WriteString("(no " + roundsWord() + ")\n")
		return sb.String()
	}
	hasSizes := false
	for i := range st.RoundStats {
		if st.RoundStats[i].ReqBytes > 0 || st.RoundStats[i].RespBytes > 0 {
			hasSizes = true
			break
		}
	}
	if !hasSizes {
		sb.WriteString("(no raw log — request/response body sizes unavailable)\n")
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("%8s %10s %8s %10s %8s %8s %s\n",
		sessionRoundColumnWord, "REQ", "MSGS", "RESP", "TEXT", "THINK", "KIND"))
	var totReq, totResp, totText, totThink, totMsgs, totKinds int
	var maxMsgs int
	kinds := map[string]bool{}
	for i := range st.RoundStats {
		r := &st.RoundStats[i]
		totReq += r.ReqBytes
		totResp += r.RespBytes
		totText += r.TextChars
		totThink += r.ThinkingChars
		totMsgs += r.Messages
		maxMsgs = maxInt(maxMsgs, r.Messages)
		kind := r.RespKind
		if kind == "" {
			// The raw parser records no response side for a round that ended
			// at the request — name it plainly.
			kind = "none"
		}
		if !kinds[kind] {
			kinds[kind] = true
			totKinds++
		}
		num := strconv.Itoa(r.Number)
		if r.Probe {
			num += "p"
		}
		msg := "-"
		if r.Messages > 0 {
			msg = strconv.Itoa(r.Messages)
		}
		text := "-"
		if r.TextChars > 0 {
			text = commaInt(r.TextChars)
		}
		think := "-"
		if r.ThinkingChars > 0 {
			think = commaInt(r.ThinkingChars)
		}
		sb.WriteString(fmt.Sprintf("%8s %10s %8s %10s %8s %8s %s\n",
			num, humanBytes(r.ReqBytes), msg, humanBytes(r.RespBytes),
			text, think, orDash(kind)))
	}
	sb.WriteString(fmt.Sprintf("%8s %10s %8s %10s %8s %8s %s\n",
		"TOT", humanBytes(totReq), commaInt(totMsgs), humanBytes(totResp),
		commaInt(totText), commaInt(totThink),
		fmt.Sprintf("%d kinds (%s)", totKinds, strings.Join(sortedKinds(kinds), ","))))
	return sb.String()
}

// sortedKinds returns the keys of a kind set in a stable order for the TOTAL
// row's "(n kinds (…))" suffix.
func sortedKinds(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// statsJSON is the machine-readable shape of `gopi session stats --json`: the stats
// struct plus the derived fields the box shows.
type statsJSON struct {
	*SessionStats
	CacheHitPct float64 `json:"cache_hit_pct"`
	AvgPrompt   int     `json:"prompt_avg"`
}

// FormatStatsJSON renders the full stats as indented JSON (pipe-friendly).
func FormatStatsJSON(st *SessionStats) (string, error) {
	out := &statsJSON{SessionStats: st, CacheHitPct: st.cacheHitPct()}
	if st.Rounds > 0 {
		out.AvgPrompt = st.PromptTokens / st.Rounds
	}
	b, err := json.MarshalIndent(out, "", jsonIndent)
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// cacheHitPct is the overview's hit rate (0 when the log reported no cache
// numbers at all, so an empty field never renders as 100%).
func (st *SessionStats) cacheHitPct() float64 {
	if total := st.CacheHit + st.CacheMiss; total > 0 {
		return float64(st.CacheHit) / float64(total) * 100
	}
	return 0
}

// ComputeCost prices the session's real money under internal/pricing at the
// given moment (the session's Started time — date/time-aware). It fills
// st.Cost and reports ok=false when the model is not priceable (unknown
// provider / no tier) — the caller says so rather than inventing a number.
// For an aggregate (mixed models) the per-model breakdown would need
// per-session state the aggregate drops, so --cost on --all is rejected at
// the CLI instead of mis-pricing here.
func (st *SessionStats) ComputeCost(at time.Time) bool {
	if st.Cost != nil {
		return true
	}
	res, ok := pricing.CostOf(st.Model, int64(st.CacheHit), int64(st.CacheMiss), int64(st.Completion), at)
	if !ok {
		return false
	}
	st.Cost = res
	return true
}

// roundMoney renders 元 to 4 decimals (per-dimension rows) — 0.1579 元.
func roundMoney(f float64) string {
	return strconv.FormatFloat(f, 'f', 4, 64)
}
