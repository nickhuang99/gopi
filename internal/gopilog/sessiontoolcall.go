package gopilog

// Tool-call addressing: `--toolcall N` — the selector that keeps "the Nth tool
// call" available WITHOUT lending it the word "round".
//
// WHY IT IS A SEPARATE SELECTOR (the defect this answers). One session answered
// "how many rounds?" with four numbers on four surfaces, and the largest of them
// (`gopi session stats --rounds`, 339) was exactly the session's `tool_use` row
// count: a tool call is not a round — several calls can happen inside one
// exchange, and an exchange can happen with none — so counting calls under the
// word "round" was the defect, not a different opinion about rounds. The ability
// to point at "the 3rd tool call" is real and is kept; the WORD is what changed
// (Operator's definition, 2026-10-10; see sessionRoundCanonicalUnit).

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/sessionevent"
)

// SessionToolCall is one tool invocation of a session, addressed by its place in
// the session's whole sequence: Ordinal counts calls, never rounds.
type SessionToolCall struct {
	// Ordinal is the 1-based position of this call among the session's tool
	// calls, in log order — what `--toolcall N` selects.
	Ordinal int
	// RoundOrdinal is the CANONICAL round this call belongs to (the `request`
	// row ordinal of the exchange it ran in), or 0 when its content arrived
	// with no request row of its own. It is how a call and an exchange are
	// named by the same unit instead of being conflated.
	RoundOrdinal int
	// Iter / Index are where the call lives in the reader's model (the content
	// round, and the call's index inside it) — for a caller that needs the
	// surrounding round, never for numbering.
	Iter  int
	Index int
	Call  ToolCall
}

// SessionToolCalls lists the session's tool calls in log order with their
// ordinals stamped. It is the ONE definition of "the Nth tool call" (Rule 14):
// a surface asks here rather than walking the reader's content rounds itself.
func SessionToolCalls(s *Session) []SessionToolCall {
	var out []SessionToolCall
	n := 0
	for i := range s.Iterations {
		it := &s.Iterations[i]
		for j := range it.ToolCalls {
			n++
			out = append(out, SessionToolCall{
				Ordinal:      n,
				RoundOrdinal: it.RequestOrdinal,
				Iter:         i,
				Index:        j,
				Call:         it.ToolCalls[j],
			})
		}
	}
	return out
}

// SessionToolCallCount is how many tool calls the session's log recorded. It is
// the DENOMINATOR of `--toolcall N`, and it is NOT a round count: the two are
// different quantities (a call is an event inside an exchange) and a surface
// that prints one must print which (`gopi session stats --rounds` printed this
// one as "rounds" — the defect that opened this task).
func SessionToolCallCount(s *Session) int {
	n := 0
	for i := range s.Iterations {
		n += len(s.Iterations[i].ToolCalls)
	}
	return n
}

// SessionToolCallIndex resolves a `--toolcall N` selector against a session and
// returns the 0-based index into SessionToolCalls. The grammar is the SHARED
// one (ParseHTTPSelector: N, 0/last, -N from the end); an out-of-range selector
// is refused in BOTH directions and never clamps to a neighbour — a mistyped
// ordinal must be visible, not silently answered with a different call.
func SessionToolCallIndex(s *Session, sel HTTPRoundSelector) (int, bool) {
	n := SessionToolCallCount(s)
	if n == 0 || !sel.Detail {
		return 0, false
	}
	if sel.Round > n || sel.Round < -n {
		return 0, false
	}
	return selectIndex(n, sel.Round), true
}

// toolCallFormatBits bounds the rendered argument/result sizes (Rule 1: caps are
// named constants).
const (
	toolCallArgMaxLen    = 600
	toolCallResultMaxLen = 2000
)

// FormatSessionToolCall renders ONE tool call: which call it is, WHICH ROUND it
// ran in (the canonical exchange — the fact a call-circular reader cannot get
// from a call count), and its input and result.
func FormatSessionToolCall(s *Session, tc SessionToolCall, total int) string {
	var sb strings.Builder
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("SESSION %s — TOOL CALL %d of %d\n", s.ID, tc.Ordinal, total))
	// The unit is named, and so is the neighbour it is NOT: this view counts
	// CALLS, the `--rounds` views count EXCHANGES, and the same session has a
	// different number for each.
	sb.WriteString(fmt.Sprintf("  unit: %s (a tool call is an event INSIDE one request — never a round)\n",
		sessionevent.CounterUnitToolCall))
	if tc.RoundOrdinal > 0 {
		sb.WriteString(fmt.Sprintf("  it ran in round %d of %d (%s) — the same number `--rounds` lists and `--round N` selects\n",
			tc.RoundOrdinal, SessionRounds(s), sessionRoundCanonicalUnit))
	} else {
		sb.WriteString("  it ran in content the log recorded with no `request` row of its own, so it belongs to no numbered round\n")
	}
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("Tool:    %s\n", tc.Call.Name))
	if len(tc.Call.Args) > 0 {
		sb.WriteString(fmt.Sprintf("Args:    %s\n", truncateRunes(spellToolCallArgs(tc.Call), toolCallArgMaxLen)))
	}
	if trimResult := strings.TrimSpace(tc.Call.Result); trimResult != "" {
		sb.WriteString("Result:\n  " + truncateRunes(sessionTaskLine(trimResult), toolCallResultMaxLen) + "\n")
	} else {
		sb.WriteString("Result:  (the log recorded no result for this call)\n")
	}
	return sb.String()
}

// spellToolCallArgs renders a call's arguments deterministically: the preserved
// raw JSON when the reader holds it (it keeps the wire order), else the decoded
// map's keys in a stable sorted order — never Go's randomised map iteration,
// which would make two runs of the same view differ.
func spellToolCallArgs(tc ToolCall) string {
	if len(tc.InputRaw) > 0 {
		return string(tc.InputRaw)
	}
	// encoding/json marshals a map's keys in sorted order, so this is
	// deterministic run to run (Go's own map iteration is not).
	if b, err := json.Marshal(tc.Args); err == nil {
		return string(b)
	}
	return ""
}
