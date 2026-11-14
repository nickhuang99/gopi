package gopilog

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/inputsource"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// Round-scoped views of a SESSION (`gopi session log <session-id> <round>`).
//
// The two logs a session leaves behind number their rounds differently, and the
// difference is measured, not guessed:
//
//   - the session JSONL numbers API calls from ZERO: its `request` events carry
//     iteration=0 for the session's first send (2N-1 messages in that round's
//     body, one tool-call delta per iteration).
//   - the raw HTTP log numbers the `→ REQUEST` markers from ONE, and it also
//     counts sends the JSONL never saw: the non-streaming meta calls (a
//     round whose only response is a `← RESPONSE` body — e.g. title/auxiliary
//     generation) and retried sends have no JSONL request event. Measured over
//     the profile's finished runs: raw rounds = jsonl requests + k, with k = 0
//     for some sessions and k = 1..4 for others, so a fixed +1 (or any fixed
//     offset) is wrong.
//
// `gopi session log <id> <round>` therefore counts the SESSION's own rounds, 1-based:
// round 1 is the session's first API call (JSONL iteration 0). The raw ordinals
// are DERIVED per round by pairRawRounds, and a byte view reports the raw
// ordinal it used, so "round 3" is the same round on both surfaces and never
// silently a different one.

// SessionRounds is the number of rounds a session has under the CANONICAL
// definition: one `request` row in the session JSONL — one send to the model,
// one HTTP request/response — is one round (Operator's definition,
// 2026-10-10; sessionRoundCanonicalUnit names it in every surface's output).
//
// WHY THIS REPLACED len(s.Iterations) (measured, 2026-10-11). The old
// definition counted only the rounds that CARRIED content, because the
// reader drops a request whose accumulated blocks were all empty (flushIter).
// That made "round" mean "the rounds that carried content" in internal/gopilog
// while the session's own `request` rows — the sends that actually went over
// the wire — were a different, larger number, and the four session surfaces
// printed four different values for one session (replay 239 / log 251 /
// stats 339 on the session that opened this task). The canonical definition is
// the one the operator reads: the request row is the round, and a round that
// carried no content is still a round (it was a call that ran and returned).
//
// The content count did not disappear — it is SessionContentRounds, a NAMED
// neighbour, so a surface that wants it asks for it by name instead of calling
// it "rounds" (Rule 15).
func SessionRounds(s *Session) int {
	return s.RequestEvents
}

// SessionContentRounds is the neighbouring count under the CANONICAL one: how
// many rounds CARRIED content (the reader's s.Iterations). It is what
// `SessionRounds` used to return, it is what a `--rounds` table can show a body
// for, and it is always <= SessionRounds: the reader drops a request whose
// blocks were all empty, so it can only lose rounds, never invent one.
func SessionContentRounds(s *Session) int {
	return len(s.Iterations)
}

// SessionTurns is the session's TURN count: how many asks a PERSON made — one
// TURN beginning at each operator row, running to the next one.
//
// IT IS THE NEIGHBOUR OF THE ROUND AND NEVER A ROUND. The operator's ruling
// (2026-10-12) is that round ≡ iteration ≡ one HTTP request/response (one JSONL
// `request` row), so the quantity this function counts — the operator's own asks
// — must be named for itself and printed under that name (`gopi session log <id>
// --turns`, sessionevent.CounterUnitHumanTurn). It is NOT what any surface prints as
// `rounds=`.
//
// WHY THE COUNT EXISTS AT ALL (measured, 2026-10-11, sessions 5af45c3e and
// capture-*): the operator's own asks are a real, useful quantity that the round
// count does not answer — that session carries 128 `request` rows under 16
// operator rows, so the two differ by roughly an order of magnitude on exactly
// the long agent runs people read back. SessionTurnBounds decides the boundary
// (an operator row, not a harness injection); this is its count.
//
// A LOG WITH NO OPERATOR ROW AT ALL still has a turn: the reader falls back to
// the `request` row's own seam (internal/gopilog/reader.go, the
// requestOpensTurn path), so the count is never 0 for a session that recorded
// any content — and a session that recorded none reports 0, which is a fact
// about the log and not a rendering fault (Rule 15).
func SessionTurns(s *Session) int {
	return len(SessionTurnBounds(s))
}

// SessionHumanTurns is the PERSON-ONLY SUBSET of SessionTurns: the turns whose
// opening row was RECORDED as the person's own input (inputsource.Human or
// inputsource.Queue — same author, different producer).
//
// WHY IT IS A SECOND FUNCTION AND NOT SessionTurns ITSELF (the defect this
// repairs, measured 2026-10-27 on session a60787b0-9081-451f-bd5a-9e396cfc048b):
// once the turn predicate became source-based (sessionevent.IsTurnOpeningRow),
// EVERY recorded face opened a turn — a `gopi notify` delivery and a
// `chat_surface action=type` self-typed line included. The surfaces that print
// `human turns` (sessionevent.CounterUnitHumanTurn: `gopi session show`'s
// `human turns` line, `session stats`, and the `SESSION HUMAN TURNS` table of
// `session log --turns`) kept reading SessionTurns, so all three printed 16
// where the honest subset was 8 — the subset had collapsed into the total, and
// the word `human` said something the number no longer meant. Rule 14: one
// meaning per word; Rule 15: a subset that equals its superset is a claim the
// measurement stopped supporting.
//
// A turn whose face was NOT recorded (a log written before the `source` field
// existed) is HUMAN: internal/gopilog's reader defaults such a row to
// inputsource.Human, and this function reads the same recorded face, so the two
// cannot disagree about the default.
func SessionHumanTurns(s *Session) int {
	return len(SessionHumanTurnBounds(s))
}

// SessionHumanTurnsUnmeasured reports whether the `human turns` SUBSET above is
// UNMEASURED for this session, and why — the ONE definition of that criterion
// (Rule 13/14: every surface that prints the subset reads this, so no render can
// decide it differently).
//
// WHY A DERIVED COUNT CAN BE UNMEASURED AT ALL (measured 2026-10-29, session
// 794cbd02-4722-43c3-924c-f7ba6f36534d): the COST block printed
//
//	Iterations: 85 / human turns: 85 / Passes: 85
//
// on a log that holds 94 `user` rows and NOT ONE of them is the person's: 91 are
// `[system]` ticks and 3 are `[WRITE_TASK_AREA …]` state rows, so every recorded
// row closes a harness-injection turn and the person opened none. The 85 did not
// come from an operator row. It came from SessionTurnBounds' fallback (a session
// WITH CONTENT and NO recorded seam gets one turn at index 0 — correct for the
// "one pass" producer), amplified by the reader's no-tick fallback marking every
// content round a seam: the derived count collapsed onto len(Iterations), i.e.
// onto the ROUND count, and a reader saw one number printed as three quantities.
//
// THE CRITERION IS THEREFORE "DID THE LOG RECORD A SEAM AT ALL", and not "is the
// count zero": a log that recorded an operator row has a MEASURED subset even
// when the number is 0 (every later turn being a notify), and a log that recorded
// no seam at all has an unmeasured one even when the fallback produced a number.
// Rule 15: the surface says it was not measured instead of printing a number that
// belongs to another counter.
//
// A NIL SESSION AND AN EMPTY LOG ARE MEASURED (0): there is nothing a seam could
// ever have been recorded on, and "no turn: the log recorded no content at all" is
// already what those surfaces say (sessionTurnNoneWord).
func SessionHumanTurnsUnmeasured(s *Session) (note string, unmeasured bool) {
	if s == nil || len(s.Iterations) == 0 {
		return "", false
	}
	for i := range s.Iterations {
		if s.Iterations[i].TurnSeamRecorded {
			return "", false
		}
	}
	return HumanTurnsNoRecordedSeamNote, true
}

// HumanTurnsNoRecordedSeamNote is the reason printed for an unmeasured subset.
// It names the fact AND the next step, so the gap is diagnosable rather than
// mysterious (Rule 15): the log's turn seams are what the surfaces below count.
const HumanTurnsNoRecordedSeamNote = "the log recorded no turn seam — every `user` row it holds is a harness injection (a clock tick, a checkpoint, a state row), so no operator-originated turn was recorded and the count cannot be measured from this log"

// SessionHumanTurnBounds is where the PERSON-ONLY turns begin: the subset of
// SessionTurnBounds whose opening row carries a person face. It is the ONE
// place the subset's membership is decided (Rule 14) — the count, the table and
// the index row all read THIS slice.
func SessionHumanTurnBounds(s *Session) []int {
	if s == nil {
		return nil
	}
	var out []int
	for _, i := range SessionTurnBounds(s) {
		if i < 0 || i >= len(s.Iterations) {
			continue
		}
		if isPersonTurnFace(s.Iterations[i].TurnSource) {
			out = append(out, i)
		}
	}
	return out
}

// isPersonTurnFace reports whether a RECORDED face is the person's own input.
// An empty face is the pre-`source`-field default and is the person's, matching
// internal/gopilog/reader.go (which writes inputsource.Human for such a row) —
// the default is stated in ONE place per layer, never guessed at a call site.
func isPersonTurnFace(face string) bool {
	switch face {
	case "", inputsource.Human, inputsource.Queue:
		return true
	}
	return false
}

// SessionTurnBounds is where the TURNS BEGIN: the 0-based iteration indices
// whose content opens a turn, in order. It is the ONE place the boundaries are
// materialised (Rule 14) — the count and the ordinal both read THIS slice, so no
// surface can disagree about where a turn starts.
//
// A session that recorded content but no mark at all gets ONE turn at index 0:
// the content is real, and reporting "0 turns" for a log that shows a
// conversation would be the silent substitution Rule 15 forbids.
func SessionTurnBounds(s *Session) []int {
	if s == nil {
		return nil
	}
	var out []int
	for i := range s.Iterations {
		if s.Iterations[i].TurnBoundary {
			out = append(out, i)
		}
	}
	if len(out) == 0 && len(s.Iterations) > 0 {
		out = append(out, 0)
	}
	return out
}

// TurnOrdinalIn is the 1-based TURN the content at iteration index idx belongs
// to, or sessionevent.RoundIndexUnknown (0) when idx names no iteration of this
// session.
//
// The ordinal of a turn is the position of the boundary that opened it, so two
// contents in one turn share an ordinal — one operator ask that took N rounds is
// ONE turn.
func TurnOrdinalIn(s *Session, idx int) int {
	if s == nil || idx < 0 || idx >= len(s.Iterations) {
		return sessionevent.RoundIndexUnknown
	}
	n := 0
	for _, b := range SessionTurnBounds(s) {
		if b > idx {
			break
		}
		n++
	}
	return n
}

// turnUnitWords is the human round's unit, as every surface prints it
// (Rule 14: one definition; the count, the ordinal and the playback's label all
// read it).
const turnUnitWords = "one TURN = one COMPLETE INPUT to the next (the person typed it, `gopi notify` delivered it, or the surface typed it for itself) — every ROUND and tool call the agent ran in between belongs to that one turn"

// HumanTurnUnitWords is humanTurnUnitWords for the surfaces (Rule 14).
func HumanTurnUnitWords() string { return humanTurnUnitWords }

// humanTurnUnitWords is what the PERSON-ONLY SUBSET of the turns is, in one
// sentence, for the surfaces that print it under its own word
// (sessionevent.CounterUnitHumanTurn). It exists because the subset used to
// borrow the turn's sentence — and the turn's word — while measuring something
// narrower: on 41e5dade-71e1-4efc-a012-456d0532e762 the person typed 66 turns
// out of the 200 the session ran, the other 134 opened by notify deliveries and
// by the model typing to itself, which the operator's ruling (2026-10-27) counts
// as inputs too.
const humanTurnUnitWords = "one HUMAN TURN = one row the person typed (the turn count itself also charges a turn an external notify or the model's own `chat_surface type` opened)"

// sessionTurnsHome names the command whose WHOLE JOB is to print this session's
// turn count — the ledger's number. It is a POINTER, not a partition: every
// surface that renders a COST block now prints the count itself (through the
// injected reader in ledger_turns.go), so this constant exists for the one job
// it still does — naming a real door when a surface has to say where the number
// lives (Rule 15: absence is stated, never substituted).
const sessionTurnsHome = "gopi session show <id>"

// TurnUnitWords is turnUnitWords for the surfaces (Rule 14).
func TurnUnitWords() string {
	return turnUnitWords
}

// sessionTurnNoneWord states the absence of a human round rather than
// substituting a neighbour (Rule 15).
const sessionTurnNoneWord = "(no turn: the log recorded no content at all — neither an operator row nor a `request` row)"

// sessionRoundCanonicalUnit is the word every surface prints for the canonical
// round, so a reader of any of them learns the same unit. Rule 1: it is a named
// constant, never a literal repeated per surface.
const sessionRoundCanonicalUnit = "one JSONL `request` row = one HTTP request/response = one iteration"

// SessionRoundUnitWords is the canonical round's unit, in the words every
// surface prints (Rule 14: one definition, read by the surfaces).
func SessionRoundUnitWords() string {
	return sessionRoundCanonicalUnit
}

// SessionRoundTableWord and SessionRoundColumnWord expose the round
// table's two names to the surfaces outside this package (cmd/agent prints the
// table and asserts on it, so it must read the SAME constant rather than a
// second copy of the string — Rule 13: a shared value is referenced, never
// mirrored).
func SessionRoundTableWord() string { return sessionRoundTableWord }

// SessionRoundColumnWord is the table's own number-column header.
func SessionRoundColumnWord() string { return sessionRoundColumnWord }

// sessionRoundNoContentWord marks a canonical round that carried no content:
// it is a round (it was a send), and it has no body to show. Printing it as
// "missing" or by the NEXT round's content would be a different round than the
// one asked for (Rule 15: absence is stated, never silently substituted).
const sessionRoundNoContentWord = "(this round carried no content — the reader recorded no blocks for it)"

// CanonicalRound is one round of a session under the canonical numbering:
// the 1-based ordinal of its `request` row, and the content the reader holds
// for it (none, when the round's blocks were all empty).
type CanonicalRound struct {
	// Ordinal is the 1-based canonical round number (the `request` row's
	// position in the log's whole request sequence).
	Ordinal int
	// Iter is the index into s.Iterations whose content belongs to this round,
	// or -1 when the round carried no content. A content round and a request
	// row are NOT one-to-one: the reader drops contentless requests, so this
	// mapping is the only correct way from a canonical ordinal to a body.
	Iter int
}

// HasContent reports whether the canonical round carries a body.
func (r CanonicalRound) HasContent() bool { return r.Iter >= 0 }

// SessionCanonicalRounds is the session's canonical rounds in order: one entry
// per `request` row, each carrying the index of the content that belongs to it
// (contentByOrdinal is the reader's per-iteration stamp, s.Iterations[i].RequestOrdinal).
//
// It is the ONE map from "round N" to "the body of round N" (Rule 14), and the
// only place the two numberings are joined: a caller that wants round 3's
// content asks here and never does the arithmetic itself.
//
// A content round whose RequestOrdinal is 0 arrived with no `request` row of
// its own (the trailing content after a log's last request, or a log that
// recorded none). It belongs to NO canonical round and is deliberately absent
// from this slice rather than attached to the last round — attaching it would
// hand a reader the wrong body (Rule 15).
func SessionCanonicalRounds(s *Session) []CanonicalRound {
	out := make([]CanonicalRound, 0, s.RequestEvents)
	for i := 1; i <= s.RequestEvents; i++ {
		out = append(out, CanonicalRound{Ordinal: i, Iter: -1})
	}
	for i := range s.Iterations {
		ord := s.Iterations[i].RequestOrdinal
		if ord < 1 || ord > s.RequestEvents {
			continue
		}
		out[ord-1].Iter = i
	}
	return out
}

// SessionCanonicalRound resolves one canonical ordinal, ok=false when it names
// no round of this session (out of range).
func SessionCanonicalRound(s *Session, ordinal int) (CanonicalRound, bool) {
	if ordinal < 1 || ordinal > s.RequestEvents {
		return CanonicalRound{}, false
	}
	r := CanonicalRound{Ordinal: ordinal, Iter: -1}
	for i := range s.Iterations {
		if s.Iterations[i].RequestOrdinal == ordinal {
			r.Iter = i
			break
		}
	}
	return r, true
}

// SessionCanonicalOrdinal resolves a parsed round SELECTOR against a session
// and returns the CANONICAL 1-based ordinal it names, ok=false when it names no
// round of this session at all (out of range in both directions).
//
// It is a SELECTOR RESOLVER, not a content lookup: a canonical round that
// carried no content still HAS an ordinal here (it was a real request) — the
// caller then asks SessionCanonicalRound for its content and is told there is
// none. Collapsing the two questions is what made "round N" silently mean "the
// Nth round that carried content" (measured: 321 request rows against 256
// content rounds on one session).
func SessionCanonicalOrdinal(s *Session, sel HTTPRoundSelector) (int, bool) {
	return SessionSelectorOrdinal(SessionRounds(s), sel)
}

// SessionSelectorOrdinal resolves a selector to a CANONICAL 1-based ordinal
// against a round COUNT, clamping nothing: ok=false when the selector is the
// `all` form or lands outside 1..n. The arithmetic and the refusal live here
// (Rule 14) so no surface re-derives them.
func SessionSelectorOrdinal(n int, sel HTTPRoundSelector) (int, bool) {
	if n == 0 || !sel.Detail {
		return 0, false
	}
	if sel.Round > n || sel.Round < -n {
		return 0, false
	}
	return selectIndex(n, sel.Round) + 1, true
}

// SessionRoundIndexForCount is SessionSelectorOrdinal for a caller that holds
// the round COUNT rather than a parsed session — `gopi session replay` has the
// count off its material (material.RoundCount) and must resolve `--round N`
// against EXACTLY the numbering this file owns.
func SessionRoundIndexForCount(n int, sel HTTPRoundSelector) (int, bool) {
	ord, ok := SessionSelectorOrdinal(n, sel)
	if !ok {
		return 0, false
	}
	return ord - 1, true
}

// SessionRoundNumber is the canonical 1-based round number the content at
// iteration index idx belongs to — the ordinal of the `request` row that
// carried it. It is RoundIndexUnknown (0) when that content arrived with no
// request row of its own: a caller with no round to report says so instead of
// inventing one (Rule 15).
//
// The offset itself lives in internal/sessionevent (Rule 14):
// internal/tools stores the very same number as a memory row's round_index.
func SessionRoundNumber(idx int) int {
	return SessionRoundNumberIn(nil, idx)
}

// SessionRoundNumberIn is SessionRoundNumber for a caller that holds the
// session, so the ordinal comes from the reader's own stamp (RequestOrdinal)
// rather than from the content's position — the two differ whenever an
// round carried no content (measured: 321 request rows against 256 content
// rounds on one session). ok=false means the caller passed neither a session
// nor an ordinal-bearing index.
func SessionRoundNumberIn(s *Session, idx int) int {
	if s != nil && idx >= 0 && idx < len(s.Iterations) {
		return s.Iterations[idx].RequestOrdinal
	}
	return sessionevent.RoundIndexUnknown
}

// RawRoundOrdinal is one round's counterpart in the raw HTTP log: the 1-based
// raw ordinal (the number `gopi session raw <id> N` takes), and ok=false when no raw
// round could be paired to it (no raw log, or no round matched).
type RawRoundOrdinal struct {
	Raw   int  // 1-based raw ordinal, 0 when unpaired
	Index int  // 0-based index into the parsed round slice, -1 when unpaired
	Exact bool // true when the pairing was CONFIRMED by a usage-token match
}

// pairRawRounds maps each session round to a raw-log round.
//
// PRIMARY rule (a confirmation, not a guess): a round's reported output tokens
// are unique per exchange — the raw round's streamed `output_tokens` equals the
// JSONL iteration's `completion_tokens` — so the pair is found by scanning the
// raw rounds once, in order, for a round that (a) is not already taken and
// (b) reports the same output tokens. That survives the extra raw rounds
// (meta calls, retries) wherever they sit, because it never assumes an offset.
//
// FALLBACK rule (when the session's log carries no usage — a degenerate or
// truncated run, or a provider that reported none): pair the i-th session round
// with the i-th raw round in order, marked Exact=false so the caller can say
// the pairing is positional rather than confirmed.
func pairRawRounds(s *Session, rounds []HTTPRound) []RawRoundOrdinal {
	out := make([]RawRoundOrdinal, len(s.Iterations))
	for i := range out {
		out[i] = RawRoundOrdinal{Index: -1}
	}
	if len(rounds) == 0 {
		return out
	}
	taken := make([]bool, len(rounds))
	anyUsage := false
	for _, it := range s.Iterations {
		if it.CompletionTokens > 0 {
			anyUsage = true
			break
		}
	}
	if anyUsage {
		searchFrom := 0
		for i, it := range s.Iterations {
			want := it.CompletionTokens
			if want <= 0 {
				continue
			}
			for j := 0; j < len(rounds); j++ {
				// Order-preserving search: a round cannot precede the
				// previous one in the file, and a used round is never reused.
				if taken[j] || rounds[j].Response.OutputTokens != want {
					continue
				}
				// Prefer the round at or after the previous match; fall back to
				// a full scan so an out-of-order timestamp cannot lose the pair.
				if j < searchFrom {
					continue
				}
				taken[j] = true
				out[i] = RawRoundOrdinal{Raw: rounds[j].Number, Index: j, Exact: true}
				searchFrom = j + 1
				break
			}
			if out[i].Index < 0 {
				for j := 0; j < len(rounds); j++ {
					if taken[j] || rounds[j].Response.OutputTokens != want {
						continue
					}
					taken[j] = true
					out[i] = RawRoundOrdinal{Raw: rounds[j].Number, Index: j, Exact: true}
					break
				}
			}
		}
	}
	// Positional fallback for whatever the token match could not confirm.
	next := 0
	for i := range out {
		if out[i].Index >= 0 {
			continue
		}
		for next < len(rounds) && taken[next] {
			next++
		}
		if next >= len(rounds) {
			break
		}
		taken[next] = true
		out[i] = RawRoundOrdinal{Raw: rounds[next].Number, Index: next}
		next++
	}
	return out
}

// SessionRoundRawOrdinals is pairRawRounds' exported entry point: the raw
// ordinal paired to each of the session's own rounds, in round order.
func SessionRoundRawOrdinals(s *Session, rounds []HTTPRound) []RawRoundOrdinal {
	return pairRawRounds(s, rounds)
}

// roundToolNames lists the tool names a round called, deduplicated in
// first-seen order (the raw view reports the same list off the wire; this is
// the JSONL side of the same fact).
func roundToolNames(it Iteration) []string {
	seen := make(map[string]bool, len(it.ToolCalls))
	var out []string
	for _, tc := range it.ToolCalls {
		if seen[tc.Name] {
			continue
		}
		seen[tc.Name] = true
		out = append(out, tc.Name)
	}
	return out
}

// Display caps for the round view (PROJECT.md Rule 1: every cap is a constant).
const (
	sessionRoundTextMaxLen   = 2000 // max runes of a text/thinking block in the round view
	sessionRoundPromptMaxLen = 400  // max runes of the round's user prompt preview
	sessionRoundToolsMax     = 8    // tool names listed on a round's header line
)

// FormatSessionRound renders ONE round of a session: its header (round number,
// token usage, elapsed) and then the round's content — the user prompt that
// opened it, every tool call with its result, and the assistant's text /
// thinking blocks.
//
// idx is a 0-based index into s.Iterations (SessionRoundIndex resolves a
// selector to it). round is the 1-based number used in the header.
func FormatSessionRound(s *Session, idx int, round int) string {
	return FormatSessionRoundWithRaw(s, idx, round, 0)
}

// FormatSessionRoundWithRaw is FormatSessionRound plus the RAW HTTP log's round
// count, so the header can CROSS-REFERENCE the three counts a reader would
// otherwise conflate: this session's rounds (its own numbering), the JSONL's
// request events (sends, including the empty ones the reader drops), and the raw
// log's rounds (which additionally number the sends the JSONL never recorded —
// non-streaming meta calls and retries).
//
// rawTotal is 0 when the caller could not pair a raw log; the line is then
// omitted rather than printed as a zero, because "no raw log" and "a raw log
// with no rounds" are different facts and a fabricated 0 would state the second
// (Rule 15).
// turnOfIteration returns the 1-based TURN that iteration idx belongs to and the
// session's turn total — derived from SessionTurnSpans, the ONE place the
// boundaries are decided (Rule 14). (0, 0) means the session recorded no turn at
// all (a source with no operator row), which is a fact a caller prints by
// omission rather than as "turn 0 of 0".
func turnOfIteration(s *Session, idx int) (turn, total int) {
	spans := SessionTurnSpans(s)
	for _, sp := range spans {
		if idx >= sp.Start && idx < sp.End {
			return sp.Turn, len(spans)
		}
	}
	return 0, 0
}

func FormatSessionRoundWithRaw(s *Session, idx int, round int, rawTotal int) string {
	if idx < 0 || idx >= len(s.Iterations) {
		// A REAL ROUND WITH NO BODY is not an invalid index: the canonical
		// numbering counts every round, and a round whose blocks were
		// all empty has no content for this view to print. Say exactly that —
		// the alternative (rendering nothing, or rendering a neighbour) would
		// answer "round N" with something that is not round N (Rule 15).
		return fmt.Sprintf(headerSep+"\nSESSION %s — ROUND %d of %d\n  %s\n%s\n",
			s.ID, round, SessionRounds(s), sessionRoundNoContentWord, headerSep)
	}
	it := s.Iterations[idx]
	var sb strings.Builder
	sb.WriteString(headerSep + "\n")
	// THE ROUND IS ADDRESSED, THE TURN IS NAMED (operator's ruling, 2026-10-22):
	// "ROUND 3 of 446" is the ADDRESS this view resolves (and the denominator
	// `gopi session log <id> 3` means), while "turn 1 of 120" is the SUBJECT —
	// which of the person's inputs this exchange helped answer. Printing both, in
	// that order, is what stops the reader taking the address for the subject
	// (the defect the ruling names) without renumbering an address callers pass.
	if turn, total := turnOfIteration(s, idx); total > 0 {
		sb.WriteString(fmt.Sprintf("SESSION %s — ROUND %d of %d  (turn %d of %d)\n",
			s.ID, round, SessionRounds(s), turn, total))
	} else {
		sb.WriteString(fmt.Sprintf("SESSION %s — ROUND %d of %d\n", s.ID, round, SessionRounds(s)))
	}
	// WHAT THIS COUNT IS. "ROUND N of M" numbers the session's ROUNDS — one
	// `request` row each, i.e. one HTTP request/response = one iteration (the
	// operator's ruling, 2026-10-12) — the SAME unit `gopi session log <id>
	// --rounds`, `gopi session replay --rounds`'s `rounds=` and `gopi session
	// stats` all report. The neighbours it is NOT are named below
	// (sessionevent.CounterUnit*), because the defect this answers was a reader
	// taking "round", "iteration" and "raw round" for one thing.
	sb.WriteString(fmt.Sprintf("  unit: the round count is %s (%s)\n",
		sessionevent.CounterUnitRound, sessionRoundCanonicalUnit))
	// The content-carrying neighbour is ALWAYS named, not only when it differs:
	// the two counts differ in general (a caller cannot tell from a single
	// session that they CAN differ), and a reader who sees one number without
	// the other is exactly the reader who conflated them.
	sb.WriteString(fmt.Sprintf("  this session: %d %s (the denominator above); %d %s — the reader records no body for a round whose blocks were all empty",
		SessionRounds(s), sessionevent.CounterUnitRound,
		SessionContentRounds(s), sessionevent.CounterUnitSessionRound))
	if rawTotal > 0 {
		sb.WriteString(fmt.Sprintf("; %d %s in the paired raw log (it also numbers sends the JSONL never recorded — non-streaming meta calls and retries)",
			rawTotal, sessionevent.CounterUnitRawRequest))
	}
	// THE ITERATION WORD IS THE ROUND'S, AND THIS LINE NAMES THE TWO NEIGHBOURS
	// IT IS NOT. It is what the agent loop recorded for itself (session_end's
	// total_iterations); for a chat session it counts REPL passes — including a
	// notify-driven turn and a failed one — so it must be named rather than
	// assumed equal to the round count, and it must not be printed under the
	// word another surface uses for the round count.
	sb.WriteString(fmt.Sprintf("\n  the agent loop's own count is %s %s (%s — this is NOT the round count)\n",
		iterationCountText(s), sessionevent.CounterUnitLoopPass,
		iterationCountSource(s)))
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("Usage:   prompt %s tok (cache hit %s / miss %s)  completion %s tok\n",
		formatNum(it.PromptTokens), formatNum(it.CacheHit), formatNum(it.CacheMiss), formatNum(it.CompletionTokens)))
	if it.ElapsedMs > 0 {
		sb.WriteString(fmt.Sprintf("Elapsed: %dms\n", it.ElapsedMs))
	}
	if names := roundToolNames(it); len(names) > 0 {
		sb.WriteString(fmt.Sprintf("Tools:   %s\n", strings.Join(capStrings(names, sessionRoundToolsMax), ", ")))
	}
	if it.UserPrompt != "" {
		// The round's prompt is session text (an attacker-supplied file can be read as
		// a prompt), so it goes through the SAME guard every other emit site uses
		// (tasktext.go; the emit gate in taskemit_gate_test.go enforces it).
		sb.WriteString("Prompt:  " + truncateRunes(sessionTaskLine(it.UserPrompt), sessionRoundPromptMaxLen) + "\n")
	}
	sb.WriteString("\n")

	// The round's content, in the order the model produced it: reasoning, then
	// text, then the tool calls the round made.
	for _, th := range it.ThinkingBlocks {
		sb.WriteString("THINKING:\n")
		sb.WriteString("  " + truncateRunes(th, sessionRoundTextMaxLen) + "\n\n")
	}
	for _, text := range it.TextBlocks {
		sb.WriteString("TEXT:\n")
		sb.WriteString("  " + truncateRunes(text, sessionRoundTextMaxLen) + "\n\n")
	}
	if len(it.ToolCalls) > 0 {
		sb.WriteString(fmt.Sprintf("TOOL CALLS (%d):\n", len(it.ToolCalls)))
		for i, tc := range it.ToolCalls {
			sb.WriteString(formatToolCall(i+1, tc))
		}
	}
	if len(it.ToolCalls) == 0 && len(it.TextBlocks) == 0 && len(it.ThinkingBlocks) == 0 {
		sb.WriteString("(no content recorded for this round)\n")
	}
	return sb.String()
}

// capStrings returns at most max entries of names, appending a "+N more" marker
// so a truncated list never reads as the complete set (Rule 15: the fact of
// truncation is stated, not swallowed).
func capStrings(names []string, max int) []string {
	if len(names) <= max {
		return names
	}
	out := append([]string{}, names[:max]...)
	return append(out, fmt.Sprintf("+%d more", len(names)-max))
}

// AttachRawTraffic copies the raw log's BODY-TRAFFIC dimensions onto a
// JSONL-canonical stats block, pairing each canonical round with its raw round
// through the token identity this file already owns (pairRawRounds) — never by
// ordinal, because the two corpora number different sets of sends (the raw log
// also numbers non-streaming meta calls and retries).
//
// WHY THE DIMENSION NEEDS CARRYING AT ALL: the wire bodies exist only in the raw
// log; the canonical round set exists only in the JSONL. A `--bytes` view is the
// join of the two, and the join is HERE so no surface re-derives a pairing
// (Rule 14). A round with no confirmed partner keeps no traffic and prints
// "-" — an unpaired round is stated as unpaired, never given a neighbour's body.
func AttachRawTraffic(s *Session, rounds []HTTPRound, st *SessionStats) {
	if s == nil || st == nil || len(rounds) == 0 {
		return
	}
	ords := pairRawRounds(s, rounds)
	byIter := make(map[int]RawRoundOrdinal, len(ords))
	for i, o := range ords {
		byIter[i] = o
	}
	for i := range st.RoundStats {
		cr, ok := SessionCanonicalRound(s, st.RoundStats[i].Number)
		if !ok || !cr.HasContent() {
			continue
		}
		o, ok := byIter[cr.Iter]
		if !ok || o.Index < 0 || o.Index >= len(rounds) {
			continue
		}
		r := &rounds[o.Index]
		rs := &st.RoundStats[i]
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
	}
}

// SessionRoundToolNameBash is the bash tool's wire name, kept here so the round
// view's own tests can assert against the constant rather than a literal.
const SessionRoundToolNameBash = toolnames.ToolBash

// SessionTurnRow is ONE turn of a session as a table prints it: the 1-based
// ordinal, how many ROUNDS the turn took, and the prompt the operator typed to
// open it. It is a view of the SAME boundary slice SessionTurns counts
// (Rule 14) — the count and the rows can never disagree.
type SessionTurnRow struct {
	// Turn is the 1-based turn number (TurnOrdinalIn's ordinal).
	Turn int
	// Rounds is how many `request` rows the turn's content owns: an operator
	// row routinely takes many, which is why one turn and one round are
	// different quantities.
	Rounds int
	// Prompt is the operator row that OPENED the round ("" when the log
	// recorded none), already truncated for one table cell.
	Prompt string
}

// SessionTurnRows materialises the session's turns in order.
//
// The boundaries come from SessionTurnBounds — the ONE place they are
// decided — so this table's rows and SessionTurns' number are two views
// of one fact, never two derivations (Rule 14). The per-turn round count is the
// number of DISTINCT canonical `request` ordinals whose content falls inside
// that turn's span: the rounds the agent ran between this operator row and the
// next belong to THIS turn by the unit's own definition.
func SessionTurnRows(s *Session) []SessionTurnRow {
	spans := SessionTurnSpans(s)
	out := make([]SessionTurnRow, 0, len(spans))
	for _, sp := range spans {
		out = append(out, SessionTurnRow{Turn: sp.Turn, Rounds: sp.Requests, Prompt: sp.Prompt})
	}
	return out
}

// sessionHumanTurnRows is SessionTurnRows restricted to the PERSON-ONLY subset
// (SessionHumanTurnBounds), in the SAME turn ordinals the whole table uses.
//
// The ordinal is NOT renumbered: a person turn keeps the number it holds in the
// session's own turn sequence (so the 3rd person row of 16 turns is `TURN 3`,
// not `TURN 2` of 8). A table that renumbered would print two different
// addresses for one turn on two surfaces — the defect this epic removes, one
// layer up — and a reader cross-checking `--round` against `--turns` would be
// misled by a gap they cannot account for.
func sessionHumanTurnRows(s *Session) []SessionTurnRow {
	all := SessionTurnRows(s)
	bounds := SessionHumanTurnBounds(s)
	if len(bounds) == 0 {
		return nil
	}
	keep := make(map[int]bool, len(bounds))
	for _, b := range bounds {
		keep[b] = true
	}
	out := make([]SessionTurnRow, 0, len(bounds))
	for i, sp := range SessionTurnSpans(s) {
		if keep[sp.Start] {
			out = append(out, all[i])
		}
	}
	return out
}

// SessionTurnSpan is ONE TURN as a turn-stepped surface needs it: the iteration
// range the turn's content occupies, how many `request` rows lie inside it, and
// how many tool calls it ran.
//
// WHY THIS TYPE EXISTS (the operator's ruling, 2026-10-22 — see
// sessionevent.CounterUnitTurn). A surface whose SUBJECT is the turn needs
// the turn's own span so it can render everything the turn contained as one
// step, and it needs the two inner quantities so it can print them as the
// step's ANNOTATION. Both are derived here, once (Rule 14): the spans come from
// SessionTurnBounds and the counts from the iterations inside the span, so the
// turn table, the replay's steps and `session stats` cannot disagree.
type SessionTurnSpan struct {
	// Turn is the 1-based turn ordinal.
	Turn int
	// Start and End delimit the turn's content in s.Iterations ([Start, End)).
	Start, End int
	// Requests is how many `request` rows fell inside the span — the quantity
	// sessionevent.CounterUnitRound counts, printed as the turn's detail.
	Requests int
	// ToolCalls is how many tool invocations the turn ran.
	ToolCalls int
	// FirstRound / LastRound delimit the ROUNDS this turn covered, in the
	// ROUND ADDRESS SPACE (`--round N`, `gopi session log <id> N`) — the
	// interval a caller needs to say "turn 11 covers rounds 143-152", which is
	// the ONE fact that stops a reader from addressing a turn by a round
	// number. Both are 0 when the span carried no `request` row at all.
	FirstRound, LastRound int
	// Prompt is the operator row that opened the turn ("" when none).
	Prompt string
}

// SessionTurnSpans materialises the session's turns with their spans and inner
// counts, in order.
//
// The boundaries are SessionTurnBounds' — the ONE place they are decided — so a
// span, SessionTurns' number and SessionTurnRows' table are three views of one
// fact (Rule 14). A `request` row belongs to the turn whose span contains its
// content, which is the unit's own definition: everything the agent ran between
// one operator row and the next belongs to that one turn.
func SessionTurnSpans(s *Session) []SessionTurnSpan {
	if s == nil {
		return nil
	}
	bounds := SessionTurnBounds(s)
	out := make([]SessionTurnSpan, 0, len(bounds))
	for i, start := range bounds {
		end := len(s.Iterations)
		if i+1 < len(bounds) {
			end = bounds[i+1]
		}
		span := SessionTurnSpan{Turn: i + 1, Start: start, End: end}
		if start < 0 {
			span.Start = 0
		}
		if span.End > len(s.Iterations) {
			span.End = len(s.Iterations)
		}
		seen := make(map[int]bool)
		for j := span.Start; j < span.End; j++ {
			if ord := s.Iterations[j].RequestOrdinal; ord > 0 {
				seen[ord] = true
				// The ROUNDS the turn covered, in the row's own address space:
				// the canonical ordinal IS the number `--round N` and `gopi
				// session log <id> N` resolve, so the interval comes from the
				// ordinals themselves rather than from the iteration indices
				// (the reader drops contentless requests, so the two numberings
				// are not one-to-one).
				if span.FirstRound == 0 || ord < span.FirstRound {
					span.FirstRound = ord
				}
				if ord > span.LastRound {
					span.LastRound = ord
				}
			}
			span.ToolCalls += len(s.Iterations[j].ToolCalls)
		}
		span.Requests = len(seen)
		if span.Start >= 0 && span.Start < len(s.Iterations) {
			span.Prompt = truncateRunes(sessionTaskLine(s.Iterations[span.Start].UserPrompt), sessionTurnPromptMaxLen)
		}
		out = append(out, span)
	}
	return out
}

// SessionTurnView is ONE TURN expressed in the ROUND address space: the turn's
// ordinal and the interval of `request` records it covered.
//
// WHY A SECOND TYPE WHEN SessionTurnSpan EXISTS. A span carries iteration
// INDICES as well (Start/End), which only a caller holding the whole Session in
// memory can use. A surface whose subject is ONE round — `gopi session context`
// exports one request body, the chat's `/context` does the same — has the round
// number and nothing else, and needs the turn that round belongs to, printed as
// "turn 2, rounds 277–332". It cannot see the iterations, so it must not be
// handed a type that invites it to re-derive them; it gets the two numbers the
// turn itself states (Rule 14: the numbers come from SessionTurnSpans, never
// from a second walk).
type SessionTurnView struct {
	// Turn is the 1-based turn ordinal (the number `session log --turns` prints).
	Turn int
	// FirstRound / LastRound delimit the rounds the turn covered, in the ROUND
	// address space (`--round N`, `gopi session log <id> N`). Both are 0 when
	// the turn carried no `request` row at all.
	FirstRound, LastRound int
}

// SessionTurnViews materialises the session's turns as ROUND-SPACE addresses.
//
// It reads SessionTurnSpans and nothing else, so its numbers and the turn
// table's, the count's and the replay's are views of ONE fact (Rule 14).
func SessionTurnViews(s *Session) []SessionTurnView {
	spans := SessionTurnSpans(s)
	out := make([]SessionTurnView, 0, len(spans))
	for _, sp := range spans {
		out = append(out, SessionTurnView{Turn: sp.Turn, FirstRound: sp.FirstRound, LastRound: sp.LastRound})
	}
	return out
}

// FormatTurnStepDetail renders the ANNOTATION a turn-stepped surface prints
// beside a step: the inner quantities, in parentheses, in one spelling
// (sessionevent.CounterDetail*).
//
// IT IS THE SHAPE `gopi chat --replay` ALREADY HAD (chatReplayUses: the turn
// count first, the recorded responses and tool results as a parenthetical) and
// the shape the operator asked the session surfaces to adopt (2026-10-22). One
// renderer, so `session replay`'s status line, `session log --rounds`' rows and
// `session stats`' headline cannot phrase it three ways.
//
// An empty annotation would render as "()" — a shape that reads like a
// rendering fault rather than a fact — so a turn with nothing inside says
// sessionevent.CounterUnitTurnNoDetail instead (Rule 15: absence stated).
func FormatTurnStepDetail(requests, toolCalls int) string {
	var parts []string
	if requests > 0 {
		parts = append(parts, fmt.Sprintf(sessionevent.CounterDetailRequestsFmt, requests))
	}
	if toolCalls > 0 {
		parts = append(parts, fmt.Sprintf(sessionevent.CounterDetailToolCallsFmt, toolCalls))
	}
	body := sessionevent.CounterUnitTurnNoDetail
	if len(parts) > 0 {
		body = strings.Join(parts, sessionevent.CounterDetailJoin)
	}
	return sessionevent.CounterDetailOpen + body + sessionevent.CounterDetailClose
}

// FormatTurnRoundRange spells the interval of ROUNDS one turn covered, in the
// ROUND ADDRESS SPACE (`--round N`, `gopi session log <id> N`) — "rounds
// 143–152".
//
// WHY THIS SENTENCE EXISTS. A listing that printed a row number without naming
// its unit and a label reading "round 152/152" beside it let an operator read
// the ROW NUMBER as a turn and ask for "turn 11" as `--round 152` — one round of
// ten, and none of the eight tool calls the turn ran. So a surface that knows a
// turn's rounds says the interval out loud, in the round's own words, next to
// the turn's number.
//
// A turn that covered no round has no interval to state, and a bare "rounds "
// would read as a rendering fault — it says so instead (Rule 15).
func FormatTurnRoundRange(first, last int) string {
	if first <= 0 || last <= 0 {
		return sessionevent.CounterRoundRangeEmpty
	}
	return fmt.Sprintf(sessionevent.CounterRoundRangeFmt, first, last)
}

// FormatTurnStepLabel renders ONE step's label: the turn first, its detail in
// parentheses — "turn 3/8  (5 request(s), 12 tool call(s))".
//
// total is the session's TURN count (SessionTurns), so the label's "k/N" names
// the same N the status line and every other surface carry (Rule 14). It is the
// ONE place the step label is spelled; a caller that typed its own format would
// be the fourth surface phrasing this sentence by hand.
func FormatTurnStepLabel(index, total, requests, toolCalls int) string {
	return fmt.Sprintf(sessionevent.CounterUnitTurnLabelFmt, index, total) +
		FormatTurnStepDetail(requests, toolCalls)
}

// sessionTurnPromptMaxLen caps the prompt cell of the human-round table
// (Rule 1: every cap is a named constant).
const sessionTurnPromptMaxLen = 60

// FormatSessionTurnTable renders the session's TURN table
// (`gopi session log <id> --turns`): one row per turn — the operator's own row
// to the next — with the number of ROUNDS that turn took.
//
// IT TABULATES THE `human turns` SUBSET, AND ITS BANNER SAYS SO. The table is
// built from operator rows (`--turns` is the flag that asks for them), but the
// session's TURN COUNT is the ledger's `chats.turn_count`, which this package
// cannot read (internal/tools → internal/gopilog is a cycle). Printing this
// table's length under the bare word was therefore a claim the table cannot
// back: measured on 41e5dade-71e1-4efc-a012-456d0532e762, 66 here against 200
// there. The banner, the unit word and the count all name the SUBSET, and the
// line under them points at the surface that prints the total (Rule 15:
// absence stated, never substituted). The rounds a turn took stay a COLUMN,
// named by their own unit (Rule 14).
func FormatSessionTurnTable(s *Session) string {
	var sb strings.Builder
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("SESSION %s — %s (%s)\n", sessionTurnTableWord, s.ID, humanTurnsTableBanner(s)))
	sb.WriteString(fmt.Sprintf("  %s = %s — the SUBSET of turns opened at a row the person typed\n",
		sessionTurnTableWord, sessionevent.CounterUnitHumanTurn))
	sb.WriteString(fmt.Sprintf("    %s\n", humanTurnUnitWords))
	// THE TOTAL, UNDER THE BARE WORD — the ledger's count, from the injected
	// reader, printed ABOVE the subset it is the total of. It used to be only a
	// pointer at another command (`the turn count itself is the LEDGER's`), which
	// left this table as the one session surface that named the word without the
	// number (measured 2026-10-27: `session show` said `turns 16`, this table
	// said only `human turns 8`). An unmeasured count still says so, with its
	// reason (Rule 15).
	sb.WriteString(fmt.Sprintf("  %s = %s — %s\n",
		sessionevent.CounterUnitTurn, turnCountValue(s), sessionevent.CounterUnitTurnIsNot))
	sb.WriteString(fmt.Sprintf("    %s\n", TurnCountSource))
	// THE LEDGER'S COMPLETED COUNT, UNDER ITS OWN WORD — the neighbour the line
	// above is NOT, and the number this table used to print under the bare word.
	//
	// TWO COLLISIONS LED HERE. First (2026-11-04, session
	// e1e22e9f-eee0-4c33-a238-0fca04534ad9): the bare line read `turns = 619`
	// while the TURN column below ran to 621, so a lookup of "turn 621" landed on
	// 619's rows. Second, the reverse (session 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f):
	// `session list` said 29 while replay ended on `──── turn 30`, because the
	// ledger charges a turn only when it COMPLETES and that turn opened and quit.
	// The bare word now carries the OPENED count on every surface — the axis the
	// TURN column, `--turn N` and every replay band number along — and the ledger's
	// completed count prints here, in numbers, only when it differs (Rule 14: one
	// meaning per word; Rule 15: the gap stated, never left to reconcile).
	// THE COMPLETED LINE PRINTS ONLY WHEN THE LEDGER WAS READ AND DISAGREES.
	// Two numbers under two words are a reader's cue that something needs
	// explaining; two IDENTICAL numbers under two words are noise, and an
	// unmeasured ledger is not a number at all (Rule 15 — but the reason is the
	// stats block's job, which has a row for it and does not run out of room).
	if completed := turnCompletedValue(s); completed != "" {
		sb.WriteString(fmt.Sprintf("  %s = %s\n", sessionevent.CounterUnitTurnCompleted, completed))
		sb.WriteString(fmt.Sprintf("    %s\n", sessionevent.CounterUnitTurnCompletedIsNot))
	}
	sb.WriteString(fmt.Sprintf("  %s = %s — %s (the count `--%s` tabulates)\n",
		sessionRoundTableWord, sessionevent.CounterUnitRound, sessionRoundCanonicalUnit, SessionRoundFlagName))
	sb.WriteString(fmt.Sprintf("  %s = %s %s (%s — the agent loop's own count, printed by `--chat` as Passes)\n",
		sessionPassRowWord, iterationCountText(s), sessionevent.CounterUnitLoopPass, iterationCountSource(s)))
	sb.WriteString(headerSep + "\n")
	if note, unmeasured := SessionHumanTurnsUnmeasured(s); unmeasured {
		// NOT sessionTurnNoneWord: that sentence speaks about a log with no
		// content at all, and this log HAS content — what it never recorded is
		// whose turns it was (Rule 15: the two gaps are different facts and get
		// different words).
		sb.WriteString(TurnCountUnmeasuredPrefix + note + "\n")
		return sb.String()
	}
	if SessionHumanTurns(s) == 0 {
		sb.WriteString(sessionTurnNoneWord + "\n")
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("%7s %10s  %s\n", sessionTurnColumnWord, sessionRoundTableWord, sessionTurnPromptColumnWord))
	for _, r := range sessionHumanTurnRows(s) {
		sb.WriteString(fmt.Sprintf("%7d %10d  %s\n", r.Turn, r.Rounds, r.Prompt))
	}
	return sb.String()
}

// The turn table's column headers (Rule 1/13: named, never inline).
const (
	sessionTurnColumnWord       = "TURN"
	sessionTurnPromptColumnWord = "PROMPT"
)

// sessionTurnBoundaryIs is the one sentence the turn table prints about the
// LOG's own turn axis — the axis its TURN column numbers along.
//
// It EXISTS BECAUSE THE COLUMN AND THE HEADER NUMBERED TWO DIFFERENT
// QUANTITIES UNDER ONE WORD (measured 2026-11-04, session
// e1e22e9f-eee0-4c33-a238-0fca04534ad9): `turns = 619` above a TURN column that
// ran to 621, with only the header's sentence explaining either number, so a
// reader who looked up "turn 621" landed on 619's rows. The row values come from
// gopilog's boundary slice (SessionTurnSpans → the reader's TurnBoundary), so the
// column has to say which axis it walks (Rule 14: one meaning per word, and a
// column is a word).
//
// IT CLAIMS NO DIRECTION, and that is deliberate — measured on session
// e364e357-a9ea-4210-a0d3-c606c6997f03, the two numbers run the OTHER way (the
// ledger holds 209, this log recorded 34 boundaries), so a sentence saying the
// boundary count "reaches past the ledger's count" was false on it. Which figure
// is larger is a fact about ONE session; it belongs in the gap line, which reads
// its own numbers, never in a standing sentence (Rule 15: a wrong explanation is
// worse than none).
const sessionTurnBoundaryIs = "the axis the TURN column numbers along: a boundary is recorded each time a turn OPENS, while the ledger's count advances when a turn COMPLETES — so the two figures answer different questions and need not be equal"

// sessionTurnFlagName and sessionRoundFlagName are the CLI spellings of the two
// tables' flags — each the unit's own word, so the flag that reaches a table and
// the table's unit cannot drift apart (Rule 14). They are plain (unprefixed)
// names because the tables name them in prose; cmd/agent's logFlagRounds and
// logFlagTurns read THESE constants and add the dashes for the CLI grammar
// (Rule 13: never a bare literal at the use site).
const sessionTurnFlagName = "turns"

// sessionPassRowWord is the label the two tables give the agent loop's own
// count — the LOOP PASS (sessionevent.CounterUnitLoopPass). It is spelled
// "PASSES", never "ITERATIONS": while it read ITERATIONS these tables printed
// 31 under a word the round table next door used for 134 (measured 2026-10-16,
// session 5c6cd329), which is the collision this whole change removes.
const sessionPassRowWord = "PASSES"

// SessionPassRowWord is sessionPassRowWord for the surfaces and their tests
// (Rule 14): the two tables' own label for the loop's count, so a test asserts
// the word the renderer actually prints rather than a literal that could drift
// from it.
func SessionPassRowWord() string { return sessionPassRowWord }

// SessionRoundFlagName is the ROUND table's flag (Rule 14).
const SessionRoundFlagName = sessionRoundFlagName

// sessionRoundFlagName is the ROUND table's flag — the word the ROUND table's
// cross-reference line prints when it names its sibling table.
const sessionRoundFlagName = "rounds"

// SessionTurnFlagName is sessionTurnFlagName for the surfaces outside
// this package (Rule 14).
const SessionTurnFlagName = sessionTurnFlagName

// The ROUND table's two names (declared above, beside the turn table's) come
// from the unit's own word (Rule 14: one meaning per word), so the banner, the
// column header and the flag all carry it from ONE place (Rule 1/13/14: no bare
// literal, no second spelling).
const ()

// sessionRoundTableWord heads the ROUND table's banner — the table
// `gopi session log <id> --rounds` prints, the SAME unit `gopi session replay
// --rounds` prints as `rounds=` (Rule 14: one meaning per word — ROUND is the
// `request` row, on every surface that spells it, and it counts notify messages
// and human input alike).
//
// sessionRoundColumnWord heads that table's own number column.
const (
	sessionRoundTableWord  = "ROUNDS"
	sessionRoundColumnWord = "ROUND"
)

// sessionTurnTableWord heads the banner of the table `gopi session log <id>
// --turns` prints. It names the SUBSET it tabulates (`human turns`), not the
// session's turn count: the word TURNS is the ledger's quantity and this package
// cannot read the ledger (see FormatSessionTurnTable). Rule 14: one meaning per
// word, including in a banner a reader skims.
const sessionTurnTableWord = "HUMAN TURNS"

// SessionTurnTableWord is sessionTurnTableWord for the surfaces
// outside this package (Rule 14: cmd/agent asserts the same value rather than a
// second copy of the string).
func SessionTurnTableWord() string { return sessionTurnTableWord }

// humanTurnsTableBanner is the parenthetical the `--turns` banner carries after
// the session id: the subset's number, or `unknown` when the log recorded no
// turn seam (SessionHumanTurnsUnmeasured — the criterion's one definition
// point).
//
// A FABRICATED NUMBER HERE WAS A CORRECTION, NOT A COSMETIC ONE: the banner used
// to print SessionHumanTurns unconditionally, and on a log whose user rows are all
// harness ticks that is the ROUND count (measured: 85 rows headed by 85 rounds of
// one `request` each, session 794cbd02). The table under it then listed 85 rows
// that no person opened. Rule 15: the gap is stated.
func humanTurnsTableBanner(s *Session) string {
	if _, unmeasured := SessionHumanTurnsUnmeasured(s); unmeasured {
		return HumanTurnsUnknownWord
	}
	return strconv.Itoa(SessionHumanTurns(s))
}

// FormatSessionRoundTable renders the session ROUND table (`gopi session log
// <id> --rounds`): each round's number, the raw-log ordinal it pairs with, its
// token usage and the raw-log byte span of that round.
//
// The table's name is its unit's name, and the unit is THE round (Rule 14):
// sessionevent.CounterUnitRound, one `request` row = one HTTP request/response
// = one iteration — the SAME number `gopi session replay <id> --rounds` prints
// as `rounds=` and `gopi session stats` reports (the operator's ruling,
// 2026-10-12, which revoked the EXCHANGE spelling). The operator-row quantity
// has a table of its own under its own name (FormatSessionTurnTable, --turns),
// so neither table has to carry a disclaimer about the other surface's number.
// Measured on the fixture session fc426fed: this table 9 rounds, the turn table
// 1 turn.
//
// ords may be nil (no raw log paired, or none parsed) and rounds may be nil:
// the session's own round numbering is a property of the session, so the table
// still renders with the raw columns dashed. A round whose raw counterpart could
// not be paired — and a pairing that was positional rather than token-confirmed
// — are both marked, because a byte view taken through an unconfirmed pairing is
// a different claim from a confirmed one.
func FormatSessionRoundTable(s *Session, ords []RawRoundOrdinal, rounds []HTTPRound) string {
	var sb strings.Builder
	sb.WriteString(headerSep + "\n")
	// THE HEADLINE IS THE TURN (operator's ruling, 2026-10-22), and the table
	// below it is the per-EXCHANGE breakdown NESTED UNDER that headline: a turn
	// is one complete input to the model (sessionevent.CounterUnitTurn). Its
	// NUMBER is the ledger's `chats.turn_count`, which this package cannot reach
	// (internal/tools imports it) — it arrives through the injected reader in
	// ledger_turns.go, and an unmeasured count says so instead of substituting
	// the person-only subset under the bare word. The count in parentheses is
	// still the round count — the table's own subject — and it is named with the
	// round's word, never the turn's (Rule 14).
	sb.WriteString(fmt.Sprintf("SESSION %s — %s (%s, %d %s)\n",
		sessionRoundTableWord, s.ID, turnCountClause(s), SessionRounds(s), sessionevent.CounterUnitRound))
	// The table's own number column and its RAW column are DIFFERENT counts from
	// DIFFERENT producers, and the header says so: this is the exact place the
	// "same thing, four values" misreading was born (a table whose last row read
	// "28   28?" was taken for "28 raw rounds" when the raw log held a different
	// number entirely).
	sb.WriteString(fmt.Sprintf("  %s = %s — %s\n",
		sessionRoundColumnWord, sessionevent.CounterUnitRound, sessionRoundCanonicalUnit))
	sb.WriteString(fmt.Sprintf("  CONTENT = %s — %d of these carried content (a row shows '-' in every column when its round recorded none)\n",
		sessionevent.CounterUnitSessionRound, SessionContentRounds(s)))
	sb.WriteString(fmt.Sprintf("  RAW   = %s from the paired raw log (it also numbers sends the JSONL never recorded; `?` marks a pairing that is positional, not token-confirmed)\n",
		sessionevent.CounterUnitRawRequest))
	// The neighbouring count is named with ITS unit, and nothing here explains
	// itself against another surface's word: the two tables each print their own
	// quantity (FormatSessionTurnTable prints the turns), so a reader comparing
	// them compares two units that are each already declared.
	sb.WriteString(fmt.Sprintf("  %s = %s %s (%s — the agent loop's own count, printed by `--chat` as Passes)\n",
		sessionPassRowWord, iterationCountText(s), sessionevent.CounterUnitLoopPass, iterationCountSource(s)))
	sb.WriteString(fmt.Sprintf("    %s\n", sessionevent.CounterUnitLoopPassIsNot))
	sb.WriteString(headerSep + "\n")
	if SessionRounds(s) == 0 {
		sb.WriteString("(no rounds: the session ended before its first API call)\n")
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("%5s %8s %8s %8s %10s %9s %9s %10s  %s\n",
		sessionTurnColumnWord, sessionRoundColumnWord, "RAW", "ELAPSED", "PROMPT", "CACHE-HIT", "COMPLETION", "RAW LINES", "TOOLS"))
	// WHICH TURN A ROUND BELONGS TO is what makes this a per-exchange DETAIL of
	// the turn-led view rather than a competing one: the column is derived from
	// gopilog.SessionTurnSpans, the ONE place the boundaries live, so the number a
	// row shows is the same turn number `session log --turns` and `session
	// replay`'s status line use.
	turnOf := make(map[int]int, len(s.Iterations))
	for _, sp := range SessionTurnSpans(s) {
		for j := sp.Start; j < sp.End; j++ {
			if _, ok := turnOf[j]; !ok {
				turnOf[j] = sp.Turn
			}
		}
	}
	// The rows are the CANONICAL rounds (one per `request` row, in log order),
	// each printing the content that belongs to IT — never "the next content
	// round", which is what an index-into-Iterations loop printed as soon as an
	// round carried nothing (measured: 65 such rows on one session).
	ordsByIter := make(map[int]RawRoundOrdinal, len(ords))
	for i, o := range ords {
		ordsByIter[i] = o
	}
	for _, cr := range SessionCanonicalRounds(s) {
		turnCol := "-"
		if t, ok := turnOf[cr.Iter]; ok {
			turnCol = strconv.Itoa(t)
		}
		if !cr.HasContent() {
			// A round the reader holds no content for still belongs to a TURN
			// (it was an exchange the turn made), so the turn column carries its
			// number while the round's own columns stay dashed.
			sb.WriteString(fmt.Sprintf("%5s %6d %8s %8s %10s %9s %9s %10s  %s\n",
				turnCol, cr.Ordinal, "-", "-", "-", "-", "-", "-", sessionRoundNoContentWord))
			continue
		}
		it := &s.Iterations[cr.Iter]
		rawCol, spanCol := "-", "-"
		if o, ok := ordsByIter[cr.Iter]; ok && o.Index >= 0 {
			rawCol = fmt.Sprintf("%d", o.Raw)
			if !o.Exact {
				// Positional pairing (the session log carried no usage to
				// confirm it): say so rather than implying a match.
				rawCol += "?"
			}
			if o.Index < len(rounds) {
				r := &rounds[o.Index]
				spanCol = fmt.Sprintf("%d-%d", r.LineStart, r.LineEnd)
			}
		}
		sb.WriteString(fmt.Sprintf("%5s %6d %8s %7dms %10s %9s %9s %10s  %s\n",
			turnCol, cr.Ordinal, rawCol, it.ElapsedMs,
			formatNum(it.PromptTokens), formatNum(it.CacheHit),
			formatNum(it.CompletionTokens), spanCol,
			strings.Join(capStrings(roundToolNames(*it), sessionRoundToolsMax), ",")))
	}
	return sb.String()
}
