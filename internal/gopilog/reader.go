package gopilog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nick/gopi/internal/boolvocab"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/fullid"
	"github.com/nick/gopi/internal/requestusage"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/toolnames"
)

// sessionsDir is the sessions directory name under the gopi data root
// (alias of config.SessionsDirName — the repo's single source; kept as a
// local name because gopilog predates the config constant and reader/scan
// call sites read "sessions dir" more naturally than the exported join).
const sessionsDir = config.SessionsDirName

// Scanner buffer sizes for session JSONL lines (PROJECT.md Rule 1: no magic
// numbers). The MAXIMUM line length is no longer a small constant: a session
// line's length is set by the model's own output, so the reader STREAMS a line
// of any size up to the memory-safety ceiling — see jsonlline.go
// (jsonlLineSafetyCeiling) for that decision and the named over-ceiling error
// that replaced bufio's bare "token too long".
//
// The INITIAL buffer, by contrast, must stay small. bufio fills its whole
// window on the first Read, so this number is a per-file device-read size:
// reading one session's FIRST LINE with a 1 MiB window read a full MiB off the
// disk, which — multiplied by the thousands of sessions a listing walks —
// dominated `gopi session log --last` (measured 2026-09-23: 1 MiB/file = 2.75s per
// 3.8k files vs 59ms at 4 KiB, i.e. a 47x read amplification for the 1 KiB
// session_start line this scanner is usually asked for). 4 KiB is the page
// size — the smallest read the filesystem can actually avoid rounding — and
// lines longer than it simply stream through extra ReadSlice round trips on
// the same reader, so the ceiling is unchanged, only the read pattern.
const (
	scannerInitBufSize = 4 << 10 // initial scanner buffer (4 KiB — one page)
)

func ResolveSessionDir() (string, error) {
	root, err := config.SessionsRoot()
	if err != nil {
		return "", fmt.Errorf("cannot resolve gopi dir: %w", err)
	}
	return root, nil
}

// ParseFile parses a single JSONL session file
func ParseFile(path string) (*Session, error) {
	return parseFileCeiling(path, jsonlLineSafetyCeiling)
}

// parseFileCeiling is ParseFile with the per-line safety ceiling injected, so a
// test can prove the over-ceiling path on a small file (a real >1 GiB fixture
// is not writable in a test). Production always passes jsonlLineSafetyCeiling.
func parseFileCeiling(path string, lineCeiling int) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &Session{}
	var events []RawEvent
	scanner := newJSONLScanner(path, f, lineCeiling)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			// AN UNPARSABLE LINE IS NOT ONE CONDITION (torn_tail.go states
			// why): the scanner knows whether this line had a terminator, and
			// that is the fact which separates a record whose WRITER WAS CUT
			// OFF from a record that is DAMAGED with good lines after the
			// damage. Both are refused — strict refusal is the default and no
			// line is ever dropped — but each now says which one it is, where,
			// and how much of the record survives.
			// The position and the terminator flag are read BEFORE the rest
			// of the file is walked for countCompleteLinesAfter (which
			// advances this scanner) — Go does not promise the order of an
			// argument list, and a line number that depends on evaluation
			// order is a defect waiting for a compiler change.
			lineNo, lineStart, unterminated, lineLen := scanner.lines, scanner.LineStart(), scanner.Unterminated(), len(line)
			return nil, classifyLineReadError(
				path, lineNo, lineStart, lineLen,
				unterminated, countCompleteLinesAfter(scanner), err)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	processEvents(events, s)
	return s, nil
}

// iterHasContent reports whether an iteration carries anything a reader can
// show: model blocks OR the harness rows that arrived for it. The harness rows
// count because they are the round's own clock line — an iteration holding only
// a tick and no model output is still the exchange the tick announced, and
// flushing it on the NEXT tick would attribute its row to a round it does not
// belong to.
func iterHasContent(it *Iteration) bool {
	return it != nil && (len(it.ToolCalls) > 0 || len(it.TextBlocks) > 0 ||
		len(it.ThinkingBlocks) > 0 || len(it.HarnessPrompts) > 0)
}

// opensTurn is the turn-opening predicate, passed in — never a second rule
// (Rule 14). It reports whether a user row IS a complete input that opens a turn.
//
// IT IS A PARAMETER FOR ONE REASON: the regression arm
// (TestRunSessionWithUnequalRoundsAndTurns) has to drive the RETIRED behaviour
// to prove the new one is the fix, and the honest way to run a retired predicate
// is to hand this function a different answer, not to keep a copy of the old rule
// in the tree. In production the caller passes sessionevent.IsTurnOpeningRow
// (see the call site in processEvents).
type opensTurn func(text, source string) bool

// runSeamFallbackReason names WHY the request-row fallback exists, so the
// session that lands in it says what happened instead of leaving a reader to
// infer it from a number (Rule 15).
const runSeamFallbackReason = "the log recorded no turn-opening row at all — every `user` row it holds is a harness injection — so its `request` rows are the only seam it has"

// readRequestRoundSeams decides whether a `request` row's own seam may open a
// HUMAN round in THIS log: it may only when the log carries NO operator row at
// all (a resumed file whose opening human turn was written elsewhere, a worker
// run nobody typed into). Then the exchange is the only seam the log has, and
// using it is stated rather than silently inventing rounds. In a log with even
// one row that OPENS A TURN, THAT row is the seam and a request row must not
// open one too — a `request` row is an exchange, and counting exchanges is the metric
// this whole file exists to replace (measured on 5af45c3e: 128 exchanges, 16
// operator rows).
//
// THE FALLBACK IS WHERE THIS FILE'S COUNT COLLAPSED ONTO ITS ROUNDS — the
// defect measured on the RUN session 01a1944e-900f-70b7-81ac-24eda447b38b
// (2026-11-01): 127 `request` rows, 136 `user` rows, and NOT ONE of those 136
// rows opened a turn under IsTurnOpeningRow (133 are `[system]` injections and a
// `[WRITE_TASK_AREA …]` state row — this fallback's intended case). The
// fallback then stamped EVERY content round a seam, so `--turns` printed 127
// turns of one round each: the exchange count wearing the turn's word, which is
// exactly the substitution this function's own comment says it exists to
// prevent. The run's REAL input is the one the harness wrote for it — the
// briefings at the top of the log — which IsTurnOpeningRow now recognises
// (sessionevent's briefing leg), so such a log has one seam, not 127, and the
// fallback is reached only by a log that recorded no input whatsoever.
func readRequestRoundSeams(events []RawEvent, opensTurn opensTurn) bool {
	for _, ev := range events {
		if ev.Type != "user" {
			continue
		}
		if opensTurn(extractContent(ev.Message.Content), userRowSource(&ev)) {
			return false
		}
	}
	return true
}

// userRowSource is the FACE a `user` row came from, with the reader's own
// default applied: a row written before the format recorded faces came from the
// person, so `human` is a FACT about those rows rather than a guess (measured —
// every pre-2026-10-25 row in this profile's tree has no `source` field).
//
// It is ONE function so the two places that need the face (the turn-boundary
// predicate and the recorded TurnSource) cannot disagree about the default.
func userRowSource(ev *RawEvent) string {
	return sessionevent.TurnRowSource(ev.Source)
}

func processEvents(events []RawEvent, s *Session) {
	var (
		currentIter *Iteration
		iterStart   time.Time
		lastUserMsg string
		reqCount    int // request events seen (== LLM rounds, for the no-session_end fallback)
		sawEnd      bool
		lastEventTs time.Time
		// The `request` rows AS THEY WERE WRITTEN, in file order, plus the two
		// facts the quantity classification needs (internal/requestusage):
		// whether a session_start/session_end row separated each row from the
		// previous one (a run segment boundary cannot be evidence of a fall),
		// and which content iteration each row's numbers belong to.
		//
		// NOTHING IS FOLDED DURING THE WALK. Whether the column is a per-request
		// increment or a running total is a property of the WHOLE LOG (the same
		// writer produced all of it, and an untagged log is decided from the
		// sequence), so the fold happens once, after the file has been read.
		reqRows      []requestusage.Reading
		reqBases     []requestusage.RequestUsageBase
		reqBoundary  []bool
		reqSeg       []int // run segment (one per `session_start`), 0-based
		reqOwner     []int // index into s.Iterations, -1 when the row stamped none
		segIdx       int
		segMarkSince bool // a session_start/session_end was seen since the last request row
		endTotalSum  int  // Σ session_end.total_tokens — proof for classification
		sawEndTotal  bool
		// pendingHarness holds harness injections that arrived before any
		// iteration was open; ensureIter hands them to the next one (they are
		// part of the round that was about to start, and they must not START it
		// — see the "user" case).
		pendingHarness []string
		// pendingRoundBoundary is whether a HUMAN ROUND is waiting for the
		// content that will carry it (internal/sessionevent.IsTurnOpeningRow
		// — the OPERATOR'S OWN ROW, which the reader sets when it reads one).
		// ensureIter copies the fact onto the iteration it opens; nothing else
		// is read from the injection's text, which stays verbatim in
		// HarnessPrompts for the surfaces that print it.
		pendingRoundBoundary bool
		// pendingTurnSource is the FACE the pending turn-opening row came from
		// (internal/inputsource). It travels beside the flag rather than being
		// re-derived downstream, the same way TurnBoundary itself does.
		pendingTurnSource string
		// pendingTurnOpenedAt is the TIMESTAMP of the pending turn-opening row —
		// the moment the banner shows. It travels with the flag and the face for
		// the same reason: it belongs to THE ROW, and re-deriving it downstream
		// would mean asking a clock (which prints today for yesterday's turn) or
		// re-walking the log (a second reader of one fact, Rule 14).
		pendingTurnOpenedAt time.Time
	)

	flushIter := func(requestOrdinal int) {
		if currentIter == nil {
			return
		}
		// Preserve any iteration that has content (tool calls, text, or thinking).
		if len(currentIter.ToolCalls) == 0 && len(currentIter.TextBlocks) == 0 && len(currentIter.ThinkingBlocks) == 0 {
			// An iteration with nothing but a harness row in it is NOT a round:
			// the row is handed FORWARD to the next one rather than promoted to
			// a round of its own (measured: a harness tick that arrived between
			// two rounds otherwise added one to the round count this file — and
			// `gopi session log <id> --rounds` — prints).
			pendingHarness = append(pendingHarness, currentIter.HarnessPrompts...)
			// A round boundary that arrived with an injection which opened NO
			// content is handed forward too: the content it was waiting for is
			// the thing that begins the round (the tick's own "this round is
			// about to start" shape), and dropping the flag here would lose a
			// round boundary without a body — which is exactly the misalignment
			// a reader must not introduce (Rule 15).
			pendingRoundBoundary = pendingRoundBoundary || currentIter.TurnBoundary
			// The moment travels with the flag for the same reason, and it is
			// taken only when the boundary itself is still pending: an
			// already-forwarded moment must not be overwritten by a later,
			// empty iteration's zero.
			if pendingTurnOpenedAt.IsZero() {
				pendingTurnOpenedAt = currentIter.TurnOpenedAt
			}
			currentIter = nil
			return
		}
		// WHICH `request` ROW THIS CONTENT BELONGS TO. A content round and a
		// request row are not one-to-one: this flush stamps the ordinal of the
		// request row that CARRIED this content in its prompt, which is the row
		// that closes it (the row's own call completed after the content was
		// produced — measured: an assistant tool_use and its tool_result sit
		// BETWEEN two request rows, and the following row's prompt is the
		// longer one). Zero means there was no request row to carry it (a log
		// that recorded none): absence is stated, never a fabricated ordinal
		// (Rule 15).
		//
		// Every tool call the round held is stamped with the same ordinal, so a
		// `--toolcall N` view can name the exchange a call belonged to.
		if requestOrdinal > 0 {
			for i := range currentIter.ToolCalls {
				currentIter.ToolCalls[i].RequestOrdinal = requestOrdinal
			}
			currentIter.RequestOrdinalStamped = true
		}
		currentIter.RequestOrdinal = requestOrdinal
		s.Iterations = append(s.Iterations, *currentIter)
		currentIter = nil
	}

	// rawFallbackSeams is whether this log's human-round seams are its `request`
	// rows themselves: it is true only for a log with no clock tick at all (see
	// readRequestRoundSeams). In that mode every content iteration IS a human
	// round — one exchange, one round, which is the best a log can say about
	// human turns when it recorded no per-request stamp.
	rawFallbackSeams := readRequestRoundSeams(events, sessionevent.IsTurnOpeningRow)

	// ensure iteration exists (lazily)
	ensureIter := func(ts time.Time) {
		if currentIter == nil {
			currentIter = &Iteration{}
			iterStart = ts
			if lastUserMsg != "" {
				currentIter.UserPrompt = lastUserMsg
				lastUserMsg = ""
			}
			// Harness rows that arrived before this round began belong to it.
			if len(pendingHarness) > 0 {
				currentIter.HarnessPrompts = append(currentIter.HarnessPrompts, pendingHarness...)
				pendingHarness = pendingHarness[:0]
			}
			// A pending OPERATOR row is the human round's own opening, so the
			// content it precedes begins one (the seam that operator row set —
			// see internal/sessionevent.IsTurnOpeningRow).
			// ONLY THE ROW-RECORDED ARM SETS TurnSeamRecorded. The no-tick
			// fallback below is the reader's own DERIVATION ("if this log has no
			// operator row at all, an exchange is the only seam there is"), and
			// it says nothing about WHOSE turn an exchange was — which is why it
			// must not make the person-only subset look measured (types.go,
			// SessionHumanTurnsUnmeasured).
			if pendingRoundBoundary {
				currentIter.TurnBoundary = true
				currentIter.TurnSource = pendingTurnSource
				// THE ROW'S OWN MOMENT, carried onto the turn it opened. Zero
				// when the seam was derived rather than recorded — the banner
				// then prints its placeholder instead of inventing a time.
				currentIter.TurnOpenedAt = pendingTurnOpenedAt
				currentIter.TurnSeamRecorded = true
				pendingRoundBoundary = false
				pendingTurnSource = ""
				pendingTurnOpenedAt = time.Time{}
			}
			// The no-tick fallback: the request row is the seam — derived, never
			// claimed as recorded.
			if rawFallbackSeams {
				currentIter.TurnBoundary = true
			}
		}
	}
	for _, ev := range events {
		switch ev.Type {
		case sessionevent.EventTypeSessionStart:
			s.ID = ev.SessionID
			s.Model = ev.Model
			s.Started = ev.Timestamp
			s.Workdir = ev.Workdir
			// THE LAST HEADER'S STAMP — and MEASURED, not assumed (task
			// 01a189b4). A resumed session appends one `session_start` per run,
			// each carrying the pid of the process that wrote THAT segment, and
			// this loop already assigns on every header, so the value that
			// survives is the last one's. OBSERVED before touching this line, on
			// session 66243525-16ab-4ce7-9133-84d74c539840: three headers, pids
			// 2434502 / 823759 / 3954507, and ParseFile returned 3954507 — the
			// third. The stale pid a `session show` used to print came from
			// ANOTHER reader (internal/rundir's first-line header read), not
			// from here; do not "fix" this assignment again on that report.
			// What this file adds is the COUNT: without it a caller cannot say
			// the pid is a later segment's (types.go: Session.StartSegments).
			s.WriterPID = ev.PID
			s.StartSegments++
			segMarkSince = true
			// A `session_start` IS a run segment opening: the writer that
			// follows it starts its CostTracker from zero, which is what makes
			// a cumulative reading comparable only WITHIN one segment.
			if len(reqRows) > 0 {
				segIdx++
			}

		case "user":
			msg := extractContent(ev.Message.Content)
			// Session-start user rows arrive as consecutive user events BEFORE
			// the first assistant turn: the [EPIC GOAL] briefing row is
			// followed by the framework's [system] TASK-START CHECKPOINT row.
			//
			// TWO KINDS OF ROW ARRIVE HERE, and conflating them was a defect
			// measured on a real run this session: the operator's own message,
			// and a HARNESS INJECTION (a per-tool-call clock tick, a start or
			// review checkpoint, a job notice). A tick is written between two
			// calls of the SAME round, so "last user row wins" made the round's
			// prompt read "[system] [t+683s] — first request of this turn" and
			// the operator's actual request unrecoverable from the round view.
			//
			// So the two are separated at the reader, where the distinction is
			// decided once (sessionevent.HarnessMessagePrefix — the marker
			// internal/agent opens its own injections with, shared rather than
			// mirrored here; Rule 14):
			//
			//   - an INJECTION becomes a row of the iteration it belongs to
			//     (HarnessPrompts), it never becomes the prompt, and it never
			//     creates an iteration of its own — a round is not a tick;
			//   - a MESSAGE overwrites the pending prompt (the LAST one wins:
			//     a follow-up the operator typed while a round was still
			//     running is the request the NEXT round is answering).
			// A HUMAN ROUND OPENS AT THE OPERATOR'S OWN ROW — the row that is
			// NOT a harness injection — and that fact travels as a FLAG beside
			// the text rather than being re-derived downstream
			// (sessionevent.IsTurnOpeningRow: the mark's shape has one
			// definition, Rule 14).
			//
			// MEASURED, AND IT IS WHY THE SEAM IS THIS ROW AND NOT A CLOCK TICK
			// (2026-10-11, sessions 5af45c3e-24b6-4578-9f32-f4da2c40cde7 and
			// capture-*, read out of session.jsonl): 5af45c3e carries 128
			// `[system] [t+…] … since your previous request.` rows against 128
			// `request` rows — the tick is stamped ONCE PER REQUEST, so a
			// tick-drawn seam reports exactly the number it exists to replace
			// (128) where the operator's own rows number 16. The second session
			// settles it independently: 36 `request` rows, 18 operator rows and
			// ZERO ticks (an older writer), which no tick-shaped rule can see.
			// THE FACE THIS ROW CAME FROM, decided BEFORE the boundary test
			// because the test needs it (sessionevent.IsTurnOpeningRow): a
			// `gopi notify` delivery is a COMPLETE INPUT the harness opens a
			// turn to report, and its text is the framework's own sentence, so
			// provenance — not the marker — is what makes it a turn.
			rowSource := userRowSource(&ev)
			isBoundary := sessionevent.IsTurnOpeningRow(msg, rowSource)
			// rowSource is carried BESIDE the flag onto the iteration the row
			// opens, so a surface that wants the face asks instead of
			// re-deriving it from the text — which, for a `gopi notify`
			// delivery, is the framework's own sentence and says nothing about
			// who produced the input.
			switch {
			case !isBoundary:
				// A HARNESS INJECTION (a clock tick, a checkpoint, a job
				// notice): it belongs to the round it arrived WHILE — it becomes
				// a row of that round's material (HarnessPrompts, printed
				// verbatim) and it NEVER opens a round of its own. Arriving
				// before any round is open, it is HELD and handed to the next
				// one, the same deferred-attachment rule lastUserMsg follows.
				if currentIter == nil {
					pendingHarness = append(pendingHarness, msg)
				} else {
					currentIter.HarnessPrompts = append(currentIter.HarnessPrompts, msg)
				}
			default:
				// THE OPERATOR'S OWN ROW: it opens a HUMAN round. If an exchange
				// of the previous round is still open, this row IS the seam, so
				// the content before it closes with the round that just ended
				// and the content after it is a NEW round's. One ask of theirs
				// that ran 36 exchanges is ONE round — the whole point:
				// "这个session replay还是没有改完？你看这个根本不是iteration/round，
				// 依然是event/toolcall".
				if currentIter != nil && iterHasContent(currentIter) {
					flushIter(reqCount)
					iterStart = ev.Timestamp
				}
				pendingRoundBoundary = true
				pendingTurnSource = rowSource
				// THE ROW'S TIMESTAMP IS THE BANNER'S SOURCE. It is stored HERE,
				// at the one place that knows a turn-opening row was read, so no
				// display surface has to ask a clock or re-open the file.
				pendingTurnOpenedAt = ev.Timestamp
				lastUserMsg = msg
			}
			if s.Task == "" {
				s.Task = msg
			}

		case "assistant":
			blocks := parseContentBlocks(ev.Message.Content)
			for _, block := range blocks {
				switch block.Type {
				case "text":
					text := strings.TrimSpace(block.Text)
					if text != "" {
						ensureIter(ev.Timestamp)
						currentIter.TextBlocks = append(currentIter.TextBlocks, text)
					}
				case "tool_use":
					ensureIter(ev.Timestamp)
					tc := ToolCall{
						ID:        block.ID,
						Name:      block.Name,
						Timestamp: ev.Timestamp,
						InputRaw:  block.Input,
					}
					if block.Input != nil {
						json.Unmarshal(block.Input, &tc.Args)
					}
					currentIter.ToolCalls = append(currentIter.ToolCalls, tc)
				case "thinking":
					thinking := strings.TrimSpace(block.Thinking)
					if thinking != "" {
						ensureIter(ev.Timestamp)
						currentIter.ThinkingBlocks = append(currentIter.ThinkingBlocks, thinking)
					}
				}
			}

		case "tool_result":
			resultBlocks := parseToolResultBlocks(ev.Message.Content)
			// tool_result can be in currentIter or the last flushed iteration
			for _, rb := range resultBlocks {
				if rb.ToolUseID == "" {
					continue
				}
				// search current iter first, then all flushed iters (most recent first)
				found := matchToolResult(currentIter, rb)
				if !found {
					for i := len(s.Iterations) - 1; i >= 0; i-- {
						if matchToolResult(&s.Iterations[i], rb) {
							break
						}
					}
				}
			}

		case "request":
			// new API request — flush previous iteration, start new one
			reqCount++
			lastEventTs = ev.Timestamp
			// THE ROW IS KEPT RAW (Rule 15: the quantity is decided by the whole
			// log, never row by row). What belongs to this row's content
			// iteration is stamped below; the four numbers are assigned after the
			// fold, from the per-request form internal/requestusage derives.
			reqRows = append(reqRows, requestusage.Reading{
				Prompt:     ev.PromptTokens,
				CacheHit:   ev.CacheHit,
				CacheMiss:  ev.CacheMiss,
				Completion: ev.CompletionTokens,
			})
			base := requestusage.BaseUnknown
			if ev.TaggedUsage {
				base = requestusage.RequestUsageBase(ev.UsageBase)
			}
			reqBases = append(reqBases, base)
			reqBoundary = append(reqBoundary, segMarkSince)
			reqSeg = append(reqSeg, segIdx)
			segMarkSince = false
			if currentIter != nil && (len(currentIter.ToolCalls) > 0 || len(currentIter.TextBlocks) > 0 || len(currentIter.ThinkingBlocks) > 0) {
				currentIter.Number = ev.Iteration
				currentIter.ElapsedMs = ev.Timestamp.Sub(iterStart).Milliseconds()
			}
			before := len(s.Iterations)
			flushIter(reqCount)
			owner := -1
			if len(s.Iterations) > before {
				owner = len(s.Iterations) - 1
			}
			reqOwner = append(reqOwner, owner)
			iterStart = ev.Timestamp

		case sessionevent.EventTypeSessionEnd:
			// No request row closes this content (the log ended after it): it
			// belongs to no canonical round, and ordinal 0 says exactly that.
			flushIter(0)
			sawEnd = true
			segMarkSince = true
			endTotalSum += ev.TotalTokens
			sawEndTotal = true
			lastEventTs = ev.Timestamp
			// THE SAME SPAN AS THE PASS COUNT (below): a session's elapsed time
			// and token totals are sums over its run segments, so an empty
			// resume segment that writes `elapsed_sec: 0.002` cannot claim the
			// whole session took 0.0s (measured on the same log: the render
			// printed `Elapsed: 0.0s` for a run that lasted 43 minutes).
			s.ElapsedSec += ev.ElapsedSec
			s.ExitCode = ev.ExitCode
			// ACCUMULATED, never assigned (task 01a17ae2): a log can hold
			// several run segments, one `session_end` each, and every end
			// reports only ITS OWN passes. A `gopi chat --resume` that ran no
			// turn writes `total_iterations: 0`, so assigning made the empty
			// last segment erase the 29 an earlier segment recorded — measured
			// on session 4a0957a3-bb4e-4337-a761-20606c87d1fd (line 868: 29,
			// line 871: 0). The span of this field is the WHOLE LOG, which is
			// the span SessionTurns counts (types.go: Session.TotalIterations).
			s.TotalIterations += ev.TotalIterations
			s.PassSegments++
			s.TotalTokens += ev.TotalTokens
		}
	}

	// final flush: content the log recorded after its last `request` row (no
	// exchange to number it — ordinal 0, never the previous round's ordinal,
	// which would attribute it to a call that did not produce it).
	flushIter(0)

	// The request-event count is recorded unconditionally: it is a property of
	// the log (how many sends it holds), not of whether the run ended, and both
	// the live-session fallback below and the round surfaces read it.
	s.RequestEvents = reqCount

	// Live-session fallback: when the log has no session_end event yet (the
	// process is still running, was killed, or panicked before the end hook),
	// the authoritative session_end totals never arrived. Derive the session
	// totals from the per-iteration request events instead — they carry the
	// same numbers the end hook sums (verified: request-event sums equal the
	// session_end totals on completed sessions). Without this, gopi session log
	// --last --chat on a live session prints "Iterations: 0 / Prompt: 0 /
	// Completion: 0 / Total: 0" while hit/miss (also request-derived) show
	// real values — the reported stats bug.
	if !sawEnd && reqCount > 0 {
		// TotalIterations is DELIBERATELY NOT filled here. It is the LOOP PASS
		// count — a fact only `session_end` records — and reqCount is the ROUND
		// count (one `request` row = one HTTP exchange). The two coincided on
		// old single-round turns, which is why this line survived so long, but
		// they diverge the moment a turn takes more than one round: measured
		// 2026-10-26 on session 41e5dade-71e1-4efc-a012-456d0532e762, whose
		// `session log` then printed
		//
		//	Iterations: 468   (rounds)
		//	Passes:     468   (loop passes — "derived from JSONL `request` events")
		//
		// — ONE number under two words, and neither of them was the pass count;
		// the live screen of the same run said `turn120`, i.e. 82 passes this
		// run on top of a 38-turn baseline. A caption saying "derived" does not
		// turn a round count into a pass count, so the field stays 0 and every
		// render prints sessionevent.CounterPassUnrecorded in its place (Rule
		// 15: absence is STATED, never filled with the neighbour's number).
		s.TotalTokens = s.TotalPrompt + s.TotalCompletion
		// ExitCode stays 0 here, and that is a MEASUREMENT, not a verdict:
		// nothing recorded an exit at all. Renders must read SawEnd before
		// they print an outcome — a session that was killed mid-run carried
		// real work and no end marker, and calling it "SUCCESS (exit=0)" is
		// the silent-success defect (measured 2026-09-29: a chat SIGKILLed
		// mid tool-call printed exactly that).
		s.ExitCode = 0
		if s.Started.IsZero() {
			s.Started = lastEventTs
		}
		if !s.Started.IsZero() && !lastEventTs.IsZero() {
			s.ElapsedSec = lastEventTs.Sub(s.Started).Seconds()
		}
	}
	// THE FOLD: one pass over the rows, under the quantity each is to be read
	// with (internal/requestusage — the single definition, shared with the
	// writers). This is where a chat session's running totals stop being summed
	// as if they were increments (epic 01a195b0), and where a resumed session's
	// restarted counters become a SUM OF SEGMENTS rather than one reading.
	//
	// A TASK LOG IS UNTOUCHED BY CONSTRUCTION: its rows are increments, a delta
	// row passes through unchanged, so both the per-round numbers and the totals
	// are the ones it always reported.
	if reqCount > 0 {
		var endTotals *int
		if sawEndTotal {
			endTotals = &endTotalSum
		}
		logBase, reason := requestusage.Classify(reqRows, reqBoundary, endTotals)
		tagged := make([]requestusage.Tagged, len(reqRows))
		for i := range reqRows {
			b := reqBases[i]
			if b == requestusage.BaseUnknown {
				b = logBase
			}
			tagged[i] = requestusage.Tagged{Reading: reqRows[i], Base: b, Seg: reqSeg[i]}
		}
		deltas, total := requestusage.Deltas(tagged)
		for i, owner := range reqOwner {
			if owner < 0 || owner >= len(s.Iterations) {
				continue
			}
			it := &s.Iterations[owner]
			it.PromptTokens = deltas[i].Prompt
			it.CacheHit = deltas[i].CacheHit
			it.CacheMiss = deltas[i].CacheMiss
			it.CompletionTokens = deltas[i].Completion
		}
		s.RequestUsageRows = reqRows
		s.RequestUsageBases = make([]requestusage.RequestUsageBase, len(tagged))
		for i := range tagged {
			s.RequestUsageBases[i] = tagged[i].Base
		}
		s.RequestUsageUnknown = total.Unknown
		s.RequestUsageSegments = total.Segments
		s.RequestUsageBasis = reason
		s.TotalPrompt = total.Prompt
		s.TotalCompletion = total.Completion
		if !sawEndTotal {
			s.TotalTokens = total.Prompt + total.Completion
		}
	}

	// Recorded last, because it is the ONLY fact that separates "ended with
	// this exit code" from "no end was recorded": both leave ExitCode at 0
	// when the run was healthy, and a reader that renders an outcome must
	// branch on this flag, never on ExitCode alone.
	s.EndUnrecorded = !sawEnd
}

// matchToolResult finds a tool call by ID and fills its result
func matchToolResult(iter *Iteration, rb ToolResultBlock) bool {
	if iter == nil {
		return false
	}
	for i := range iter.ToolCalls {
		if iter.ToolCalls[i].ID == rb.ToolUseID && iter.ToolCalls[i].Result == "" {
			iter.ToolCalls[i].Result = rb.Content
			if iter.ToolCalls[i].Name == toolnames.ToolBash {
				iter.ToolCalls[i].ExitCode = extractExitCode(rb.Content)
			}
			return true
		}
	}
	return false
}

func extractContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// try string
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// try array of blocks
	var blocks []ContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		for _, b := range blocks {
			if b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}

func parseContentBlocks(raw json.RawMessage) []ContentBlock {
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}
	return blocks
}

func parseToolResultBlocks(raw json.RawMessage) []ToolResultBlock {
	var blocks []ToolResultBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}
	return blocks
}

func extractExitCode(content string) *int {
	// bash results start with "exit=N\n"
	if strings.HasPrefix(content, "exit=") {
		idx := strings.IndexByte(content, '\n')
		if idx > 5 {
			codeStr := content[5:idx]
			var code int
			if _, err := fmt.Sscanf(codeStr, "%d", &code); err == nil {
				return &code
			}
		}
	}
	return nil
}

// sessionActiveTypes are the event types that prove a session actually ran a
// user turn (PROJECT.md Rule 1 — no magic strings scattered across call sites).
// A session_start-only file (never followed by a user/request/assistant/
// session_end event) is a degenerate stub — NewAgent loggers opened in unit
// tests and then abandoned write exactly one line. --last N must not let
// these crowd out real sessions.
const (
	eventTypeUser       = "user"
	eventTypeRequest    = "request"
	eventTypeAssistant  = "assistant"
	eventTypeSessionEnd = sessionevent.EventTypeSessionEnd
)

// SessionHasActivity reports whether the JSONL session file contains any
// event beyond a bare session_start — i.e. whether the session actually ran
// (user turn / LLM request / assistant output / session_end). A file whose
// only line is session_start is the fingerprint of an agent NewSessionLogger
// opened by a unit test or a run that died before its first turn; it renders
// as the empty "model 0.0s 0 iter 0 tok OK" row that pollutes --last N.
func SessionHasActivity(path string) bool {
	return scanSessionHead(path).active
}

// ListSessionFiles returns sorted session file paths (newest first).
// Ordered by the session_start timestamp — NOT file mtime, which flip-flops
// while a session is being written or a stale file is re-synced (review P1:
// `--last N` order changed between two runs seconds apart). Files with no
// readable session_start sort by filename (session IDs are UUIDv4 —
// chronological), keeping the order stable.
//
// The head of every session file is read exactly once here (the same pass that
// resolves ids and activity), so callers that need several of those facts must
// use ListSessionRefs instead of re-listing plus re-scanning per fact.
func ListSessionFiles() ([]string, error) {
	refs, err := ListSessionRefs()
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(refs))
	for _, r := range refs {
		files = append(files, r.Path)
	}
	return files, nil
}

// ListSessionRefs returns every session record under the sessions root, newest
// first, with the head facts a listing needs already resolved from ONE scan
// per file: the display id and whether the file recorded any user activity.
//
// This is the shape a CLI listing wants — `gopi session log --last` needs the order
// AND the activity filter AND (for a view) the id — so reading the head once
// here replaces the historical chain of three per-file scans (id scan, sort
// stamps, activity scan) whose wall time grew with the session count
// (measured 2026-09-23 on a 3.5k-session profile: 11s + 17s + 5s).
func ListSessionRefs() ([]SessionRef, error) {
	dir, err := ResolveSessionDir()
	if err != nil {
		return nil, err
	}
	refs, err := collectSessionFiles(dir)
	if err != nil {
		return nil, err
	}
	sortSessionRefsNewestFirst(refs)
	out := make([]SessionRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, SessionRef{Path: r.path, ID: r.id, Active: r.active})
	}
	return out, nil
}

// sortSessionRefsNewestFirst orders refs newest-first by the session_start
// timestamp captured during their own head scan (no re-read), falling back to
// the descending path when the stamps tie or are missing. Both keys are
// deterministic, so repeated runs agree.
func sortSessionRefsNewestFirst(refs []sessionFileRef) {
	sort.SliceStable(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if a.startedOK && b.startedOK && !a.started.Equal(b.started) {
			return a.started.After(b.started)
		}
		return a.path > b.path
	})
}

// LoadAllSummaries loads session-end summaries for all files
func LoadAllSummaries(maxAge time.Duration) ([]SessionSummary, error) {
	files, err := ListSessionFiles()
	if err != nil {
		return nil, err
	}
	var summaries []SessionSummary
	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().Add(-maxAge)
	}

	for _, fp := range files {
		f, err := os.Open(fp)
		if err != nil {
			continue
		}

		var ss SessionSummary
		ss.Path = fp
		if maxAge > 0 {
			fi, _ := f.Stat() //stylegate:ignore stat probe; nil means 'not old' and the file is kept — the safe default
			if fi != nil && fi.ModTime().Before(cutoff) {
				f.Close() //stylegate:ignore close of a file already written and flushed by the checked calls
				continue
			}
		}

		// A bare session_start file (logger opened, never ran a user turn —
		// unit-test artifacts, runs that died before their first turn) must
		// not surface as an empty "0 iter 0 tok" summary row. Track whether
		// any activity event was seen while scanning.
		sawActivity := false
		sawEnd := false
		scanner := newJSONLScanner(fp, f, jsonlLineSafetyCeiling)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var ev RawEvent
			if err := json.Unmarshal(line, &ev); err != nil {
				// THE TOLERANT READER STILL SAYS WHAT IT SKIPPED. A listing
				// survives an unreadable row (it must: one damaged record must
				// not blind `session log --last` to every other session), but
				// surviving silently is how the torn-tail family stayed
				// invisible here long after the strict reader was screaming
				// about the same file. The row is skipped AND named, with the
				// same classification the strict reader uses, so the two
				// readers can never disagree about what happened.
				warnLineUnreadable(classifyLineReadError(
					fp, scanner.lines, scanner.LineStart(), len(line),
					scanner.Unterminated(), 0, err))
				continue
			}
			switch ev.Type {
			case sessionevent.EventTypeSessionStart:
				ss.SessionID = ev.SessionID
				ss.Model = ev.Model
				ss.Started = ev.Timestamp
			case "user":
				sawActivity = true
				if ss.Task == "" {
					// `gopi session log --last` rows are rendered text: the guard runs
					// here as well as at the formatter, so a caller that prints
					// ss.Task directly still cannot emit NULs (the same
					// defence-in-depth the ledger applies at write AND render).
					ss.Task = oneLine(sessionTaskLine(extractContent(ev.Message.Content)), summaryTaskMaxLen)
				}
			case "request":
				sawActivity = true
			case "assistant":
				sawActivity = true
			case sessionevent.EventTypeSessionEnd:
				sawActivity = true
				ss.ElapsedSec += ev.ElapsedSec
				ss.ExitCode = ev.ExitCode
				// Same span as the full reader: the summary row counts every
				// run segment the log holds, so a resumed session's empty last
				// segment cannot print PASSES 0 (task 01a17ae2 — this walk made
				// the same last-writer-wins assignment, and `session log --last`
				// rows showed the same 0).
				ss.TotalIterations += ev.TotalIterations
				ss.PassSegments++
				sawEnd = true
				ss.TotalTokens += ev.TotalTokens
			}
		}
		f.Close() //stylegate:ignore close of a file already written and flushed by the checked calls
		// A session with no session_end recorded NO pass count; the summary row
		// prints the dash rather than a 0 that would read as a measurement. The
		// test is the EVENT, not the number: a session that ended before its
		// first API call legitimately recorded 0 passes, and that 0 is a
		// measurement rather than an absence.
		ss.EndUnrecorded = !sawEnd
		if ss.SessionID != "" && sawActivity {
			summaries = append(summaries, ss)
		}
	}
	return summaries, nil
}

type SessionSummary struct {
	Path       string
	SessionID  string
	Model      string
	Started    time.Time
	Task       string
	ElapsedSec float64
	ExitCode   int
	// TotalIterations is the session_end pass count, and EndUnrecorded says
	// whether one was recorded at all. The pair is deliberately two fields: a
	// summary that printed 0 for a LIVE session would claim a measurement the
	// log never made (task 01a17492 — the same overload that let the reader
	// substitute the round count for the pass count).
	TotalIterations int
	// PassSegments is how many `session_end` events fed TotalIterations — the
	// count's SPAN, stated rather than guessed (see Session.PassSegments).
	PassSegments  int
	EndUnrecorded bool
	TotalTokens   int
}

// sessionFileRef is one session JSONL found under the sessions root, with the
// facts a listing needs — display id, session_start timestamp and whether the
// file ever recorded user activity — all read in the SAME single head pass
// (see scanSessionHead). The display id is the session_start event's sessionId
// (authoritative across all three layouts — legacy flat <uuid>.jsonl, task-run
// <task_id>/<uuid>.jsonl, bare <uuid>/session.jsonl); an unreadable file's
// basename-minus-.jsonl is the fallback (legacy files predate nothing — their
// session_start always matches, but a truncated head must not make a session
// vanish from listings).
type sessionFileRef struct {
	path string
	id   string
	// started / startedOK are the session_start timestamp and whether one was
	// read at all; a file with no readable session_start sorts by filename
	// rather than by the zero time.
	started   time.Time
	startedOK bool
	// active reports whether the file ever recorded user activity (a user /
	// request / assistant / session_end event) — the `gopi session log --last` filter
	// for "sessions that actually did something".
	active bool
}

// SessionRef is one session record from the sessions tree with the head facts
// a listing needs (see sessionFileRef). It exists so a CLI listing runs the
// head scan ONCE per file (gopilog.ListSessionRefs) instead of once per fact
// per surface.
type SessionRef struct {
	Path   string
	ID     string
	Active bool
}

// sessionHead is everything a single head scan of a session file yields.
type sessionHead struct {
	id        string
	started   time.Time
	startedOK bool
	active    bool
}

// scanSessionHead reads a session file's head ONCE and returns its
// session_start identity (id + timestamp) plus whether the file recorded any
// user activity. One file, one open, one pass: the listing needs all three
// facts, and a per-fact reader costs its own open per fact per file — with
// thousands of sessions that multiplier WAS the wall time of
// `gopi session log --last` (measured 2026-09-23: 11s + 17s + 5s of head reads).
//
// The scan stops as soon as both facts are known. session_start is the first
// line and the activity events follow it, so a live session costs two lines;
// a file that never records activity has to be read to EOF (there is no other
// way to know it is empty).
func scanSessionHead(path string) sessionHead {
	var h sessionHead
	f, err := os.Open(path)
	if err != nil {
		return h
	}
	defer f.Close()
	scanner := newJSONLScanner(path, f, jsonlLineSafetyCeiling)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		switch ev.Type {
		case sessionevent.EventTypeSessionStart:
			if h.id == "" {
				h.id = strings.TrimSpace(ev.SessionID)
			}
			h.started, h.startedOK = ev.Timestamp, true
		case eventTypeUser, eventTypeRequest, eventTypeAssistant, eventTypeSessionEnd:
			h.active = true
		}
		if h.startedOK && h.active {
			return h
		}
	}
	return h
}

// sessionIDFromStart reads the session_start event's sessionId from path
// ("" when the file is unreadable or carries no event).
func sessionIDFromStart(path string) string { return scanSessionHead(path).id }

// sessionWalkProbe is the OBSERVABLE the exact-id fast-path guard reads
// (session_fastpath_test.go): it turns "did this resolution enumerate the
// sessions tree?" into a COUNT instead of a duration. Both fields are
// incremented at the ONE place that enumeration happens — collectSessionFiles,
// the full-tree walk every listing path shares:
//
//	walks   — how many full-tree enumerations ran
//	records — how many record heads those enumerations read
//
// A resolution that answers from the operand's own name leaves BOTH at zero,
// and that is a property of the CODE PATH — unlike a wall-clock ratio, it
// cannot be moved by the host's load. The ratio this replaced was measured
// red on a loaded host on 2026-10-27: a parallel suite made the DENOMINATOR (a
// 2000-record walk) read 30.7 ms while the same walk on an idle host reads far
// more, so the guard tripped with no change to the fast path at all. A ratio
// is a property of the host; a counter is a property of the code.
//
// DIAGNOSTIC ONLY (Rule 15: named, so nothing reads it "silently"): no
// production decision consumes these totals — they exist so a test can take
// two snapshots around the call it is judging. Reading them is cheap (one
// atomic add per record per walk).
var sessionWalkProbe struct {
	walks   atomic.Int64
	records atomic.Int64
}

// sessionFastPathRedProofEnv is the in-tree RED-PROOF switch for the exact-id
// fast path (PROJECT.md Rule 5). Setting it to a truthy value makes FindSession
// skip exactSessionPath, i.e. behave exactly like the pre-fix code that walked
// the whole tree for an answer its operand already named.
//
// It exists so the regression guard can be shown to FAIL ON DEMAND without
// editing a production file and restoring it afterwards — a mutated-file red
// proof is a gate claim that has to be re-verified minutes later, and this one
// cannot drift: nothing is written, so there is nothing to restore.
//
//	$ GOPI_GOPILOG_SESSION_FASTPATH_OFF=1 go test ./internal/gopilog \
//	      -run TestFindSession_ExactIdDoesNotReadEveryRecord -count=1
//
// Default (unset, or any non-truthy value) is the fast path. Nothing outside a
// test sets it: it is a diagnostic arm, never a knob the product turns.
const sessionFastPathRedProofEnv = "GOPI_GOPILOG_SESSION_FASTPATH_OFF"

// sessionFastPathDisabled reports whether the red-proof arm asked for the
// pre-fix behaviour. Truthiness is spelled out (1/true/yes, any case) rather
// than "any non-empty value", so that a shell that exports the variable as
// "0" or "false" does not silently disable the fast path in a real run.
func sessionFastPathDisabled() bool {
	return boolvocab.IsTrue(os.Getenv(sessionFastPathRedProofEnv))
}

// collectSessionFiles walks the sessions root for every session RECORD in the
// three layouts: legacy flat files (<root>/<uuid>.jsonl), task-run run files
// (<root>/<task_id>/<uuid>.jsonl) and bare session files
// (<root>/<uuid>/session.jsonl). The walk is internal/sessionlayout.Records —
// the repo's shared, layout-keyed classification (Rule 14) — so this reader
// sees exactly one record per session however many side files that session
// dir holds (signal-provenance.jsonl is an artifact of the SAME session, not
// a second one). Display ids resolve from session_start (fallback:
// basename-minus-.jsonl, or the dir name for the fixed bare-session record).
//
// Each record's head is scanned exactly once, here; the id, the session_start
// timestamp and the activity flag all come from that single pass.
func collectSessionFiles(dir string) ([]sessionFileRef, error) {
	sessionWalkProbe.walks.Add(1)
	records, err := sessionlayout.Records(dir)
	if err != nil {
		return nil, err
	}
	sessionWalkProbe.records.Add(int64(len(records)))
	refs := make([]sessionFileRef, 0, len(records))
	for _, r := range records {
		h := scanSessionHead(r.Path)
		id := h.id
		if id == "" {
			// Task-run files are named <uuid>.jsonl — basename works; a bare
			// session.jsonl without a start event is degenerate, so its
			// display id falls back to the session dir (FileToID's rule).
			id = strings.TrimSuffix(filepath.Base(r.Path), sessionlayout.JSONLSuffix)
			if filepath.Base(r.Path) == sessionlayout.RecordName {
				id = filepath.Base(r.Dir)
			}
		}
		refs = append(refs, sessionFileRef{
			path:      r.Path,
			id:        id,
			started:   h.started,
			startedOK: h.startedOK,
			active:    h.active,
		})
	}
	return refs, nil
}

// FindSession locates a session JSONL in dir by EXACT ID match across all
// three layouts (legacy flat, task-run dir, bare-session dir — see
// collectSessionFiles). Nothing else resolves: a TRUNCATED id is never
// matched against a prefix, it is refused with the full ids it could have
// meant (epic full-id-no-prefix, 2026-09-23).
//
// WHY THE PREFIX MATCH IS GONE. It used to resolve a unique short id and error
// only on ambiguity. Measured on this host that is the wrong trade twice over:
// 3771 session records over 75517 tree entries make an 8-character chunk
// ambiguous often enough that the resolution is a coin flip the operator
// cannot see (the ledger has 35 rows sharing `01a0b51d`), and the chunk
// `gopi session log --last` prints (`2ab3facd`) is not even a task id — it is a
// session prefix that resolves to one arbitrary session. A prefix names more
// than one thing, so it is refused and the refusal says so.
//
// An operand that matches NO session as a prefix (a stale or misspelled id, a
// legacy non-uuid session name) keeps the answer it always had: an empty path
// with a nil error, which callers render as "not found".
func FindSession(dir, id string) (string, error) {
	// FAST PATH — the EXACT match needs no walk. A session id IS the run's
	// uuid, so all three layouts NAME its file (see sessionIDCandidates):
	// the resolution is a stat on a name, not a pass over the tree.
	//
	// WHY THIS EXISTS (measured 2026-09-25 on this host): the exact match used
	// to run collectSessionFiles, which reads EVERY session record's head. Over
	// a 76,207-entry tree holding 3,665 records that cost 173,589 openat +
	// 333,583 getdents64 — ~2.4s of raw syscall time, and the user-visible
	// 3.2s of `gopi session raw <id>` / `gopi session log <id>`, for an answer that is one
	// stat. The walk belongs to the PREFIX case below, where the operand
	// genuinely names no single file; here the name is already known.
	//
	// The guard for that claim is TestFindSession_ExactIdDoesNotReadEveryRecord,
	// and it reads sessionWalkProbe — a COUNT of tree walks and record heads,
	// never a duration: the ratio this replaced moved with the host's load
	// (measured red on a loaded host 2026-10-27 with the fast path intact).
	if id != "" && !sessionFastPathDisabled() {
		if p, ok := exactSessionPath(dir, id); ok {
			return p, nil
		}
	}
	refs, err := collectSessionFiles(dir)
	if err != nil {
		return "", err
	}
	// exact match (session display id — authoritative, from session_start)
	for _, r := range refs {
		if r.id == id {
			return r.path, nil
		}
	}
	// No exact match. A prefix is NEVER resolved: when the operand matches one
	// or more sessions as a prefix, it is refused, and the refusal names every
	// identity it could have meant. An operand that matches nothing keeps the
	// ordinary "not found" answer ("" with a nil error) it always had.
	var cands []string
	for _, r := range refs {
		if strings.HasPrefix(r.id, id) {
			cands = append(cands, r.id)
		}
	}
	if len(cands) > 0 {
		return "", fullid.Refusal(fullid.KindSession, id, cands)
	}
	return "", nil
}

// exactSessionPath returns the session record path that a full uuid operand
// names under dir, trying each layout's own name and returning the first that
// EXISTS AND RECORDS that id. ok=false means no candidate held a record under
// that name, and the caller falls through to the tree walk (which is also what
// reports a truncation refusal — a prefix names no file, so the fast path never
// applies to it).
//
// A NAME THAT EXISTS IS NOT YET A MATCH. Being named for the id makes a file a
// CANDIDATE; it becomes the answer only when it also (a) survives the shared
// classifier's exclusions for a file inside a session dir — a registered side
// file (signal-provenance.jsonl) or any JSONL beside a bare session's record is
// an artifact of that dir's session, not a session of its own — and (b) returns
// the operand as its display id through FileToID, which is the SAME identity
// rule collectSessionFiles applies. Both checks are what keep the fast path a
// shortcut and never a different answer: measured on this host it turned 7,332
// per-record head reads into 1 stat (plus one small head read of the one file
// that exists).
func exactSessionPath(dir, id string) (string, bool) {
	for _, p := range sessionIDCandidates(dir, id) {
		fi, err := os.Stat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		parent := filepath.Dir(p)
		if parent != dir {
			// Inside a session dir: the two exclusions internal/sessionlayout
			// applies there (sessionlayout.recordsIn).
			if sessionlayout.IsSideFile(filepath.Base(p)) {
				continue
			}
			if sessionlayout.HasBareRecord(parent) && filepath.Base(p) != sessionlayout.RecordName {
				continue
			}
		}
		if FileToID(p) == id {
			return p, true
		}
	}
	if p, ok := runDirRecordForID(dir, id); ok {
		return p, true
	}
	return "", false
}

// runDirRecordForID answers a TASK-ID operand: the record held by the run
// directory NAMED by that operand (<root>/<id>/).
//
// WHY THIS IS A SEPARATE RULE FROM THE ID MATCH ABOVE (epic: one task row, one
// id — the BACKWARD-COMPATIBILITY half). A run directory is named by the RUN ID
// (internal/rundir states it: the address is the run id, and for a task row
// those are one and the same string), and the files inside it were historically
// named by a MINTED uuid. So `gopi session log <task-id>` — the operand the
// operator actually has — names the DIRECTORY correctly and no file at all:
// the fast path's name candidates miss, and the tree walk compares session ids,
// where the task id appears nowhere. Measured before this change: the row's
// record was found only by the run's own uuid.
//
// The rule is the DIRECTORY, not the file name, which is what makes it work for
// BOTH layouts at once — old (<task-id>/<uuid>.jsonl, whose ids never match the
// operand) and new (<task-id>/<task-id>.jsonl, whose ids do). A directory named
// <id> that holds no record answers false, so a miss stays a miss.
func runDirRecordForID(dir, id string) (string, bool) {
	if id == "" {
		return "", false
	}
	runDir := filepath.Join(dir, id)
	if fi, err := os.Stat(runDir); err != nil || !fi.IsDir() {
		return "", false
	}
	rec, ok := sessionlayout.NewestRecordInDir(runDir)
	if !ok {
		return "", false
	}
	return rec.Path, true
}

// sessionIDCandidates lists, cheapest first, the record paths a full session id
// can name under dir — one per layout internal/sessionlayout classifies:
//
//	legacy flat   <root>/<id>.jsonl
//	task run      <root>/<task_id>/<id>.jsonl   (task_id not derivable — the
//	                                             root's child DIRS are listed,
//	                                             one level, never the tree)
//	bare session  <root>/<id>/session.jsonl     (always LAST: it is the one
//	                                             candidate the name alone
//	                                             cannot confirm)
//
// The directory listing is what makes the task-run layout reachable at all: a
// run file is named for the RUN uuid, not the task, so no name can be composed
// from the session id. One ReadDir of the root is O(children) and reads no
// session file — the cost this exists to avoid is the per-RECORD head read.
func sessionIDCandidates(dir, id string) []string {
	cands := []string{filepath.Join(dir, id+sessionlayout.JSONLSuffix)}
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Unreadable root: the bare-session candidate is still worth trying,
		// and the caller's fallback walk reports the real error.
		return append(cands, filepath.Join(dir, id, sessionlayout.RecordName))
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cands = append(cands, filepath.Join(dir, e.Name(), id+sessionlayout.JSONLSuffix))
	}
	return append(cands, filepath.Join(dir, id, sessionlayout.RecordName))
}

// FileToID extracts a session file's display session id: the session_start
// event's sessionId when readable, else the layout-aware name fallback. The
// layouts:
//
//	legacy flat  <root>/<uuid>.jsonl           → basename stem (<uuid>)
//	task run     <root>/<task_id>/<uuid>.jsonl → basename stem (run uuid)
//	bare session <root>/<uuid>/session.jsonl   → parent dir name (= uuid)
//
// A bare session file is literally named session.jsonl — its basename stem
// ("session") is useless as a display id, so the fallback takes the parent
// directory's name (the session's uuid) instead.
// It is a THIN ALIAS of internal/sessionlayout.SessionID — the one definition
// (Rule 14), because a writer that stamps an id into a FILE (the orphan crash
// dump) and a reader that displays one must not answer "who is this session?"
// with two copies of the same algorithm that can drift apart.
func FileToID(path string) string { return sessionlayout.SessionID(path) }

// contextTrimResultRe matches the outcome line inside a context_trim
// tool_result. The result may carry a leading tick prefix (see
// Agent.withTick) and the "[tool_use id: …]" header the loop stamps onto
// every result, so the match is anchored on the "context_trim:" text itself,
// never on the start of the string. The three outcome shapes are the exact
// strings produced by the agent loop's executeTool (internal/agent/loop.go):
//
//	context_trim: replaced result of call X with conclusion (pair kept, roles unchanged)
//	context_trim: removed call X + its result (N message(s) dropped from context)
//	context_trim: no-op — call X not found, incomplete pair, or already trimmed (silent)
var (
	contextTrimReplacedRe = regexp.MustCompile(`context_trim: replaced result of call (\S+) with conclusion`)
	contextTrimRemovedRe  = regexp.MustCompile(`context_trim: removed call (\S+) \+ its result \((\d+) message\(s\) dropped`)
	contextTrimNoopRe     = regexp.MustCompile(`context_trim: no-op`)
)

// collectContextTrims scans a parsed session and extracts every context_trim
// activity: the context_trim call's own id, its call_id / replace_with
// arguments, the outcome class parsed from its tool_result, and the size
// comparison between the trimmed call's original logged result and its
// post-trim replacement. Call ids and results are matched across all
// iterations (a trim can target a call from an earlier iteration), in session
// order.
func collectContextTrims(s *Session) []ContextTrimEvent {
	var trims []ContextTrimEvent
	for _, iter := range s.Iterations {
		for _, tc := range iter.ToolCalls {
			if tc.Name != toolnames.ToolContextTrim {
				continue
			}
			ev := ContextTrimEvent{
				CallID:      tc.ID,
				Timestamp:   tc.Timestamp,
				ReplaceWith: strArg(tc.Args, "replace_with"),
			}
			target := strArg(tc.Args, "call_id")
			ev.TargetCallID = target
			// Outcome class + removed-message count come from the tool_result
			// the loop generated for this context_trim call.
			if m := contextTrimReplacedRe.FindStringSubmatch(tc.Result); m != nil {
				ev.Outcome = "replaced"
				if m[1] != "" {
					ev.TargetCallID = m[1]
				}
			} else if m := contextTrimRemovedRe.FindStringSubmatch(tc.Result); m != nil {
				ev.Outcome = "removed"
				if m[1] != "" {
					ev.TargetCallID = m[1]
				}
				ev.RemovedMessages, _ = strconv.Atoi(m[2]) //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
			} else if contextTrimNoopRe.MatchString(tc.Result) {
				ev.Outcome = "noop"
			} else if tc.Result != "" {
				// A context_trim call whose result is neither of the known
				// shapes (e.g. an older binary's phrasing): report it so the
				// activity is still visible, with a neutral outcome.
				ev.Outcome = "unknown"
			}
			// Size comparison: locate the trimmed call's original tool_result
			// in the log (context mutation is in-memory only, so the logged
			// result keeps the original content). BeforeBytes is the FULL
			// logged content — exactly the string RemoveToolPair replaces in
			// context (tick prefix + stamped [tool_use id: …] header +
			// payload; see Agent.executeTool), so the before/after delta is
			// the real number of context bytes freed by the trim.
			if target != "" {
				if orig, ok := findToolCallByID(s, target); ok {
					ev.TargetName = orig.Name
					ev.BeforeBytes = len(orig.Result)
				}
			}
			switch ev.Outcome {
			case "replaced":
				ev.AfterBytes = len(ev.ReplaceWith)
			case "removed":
				ev.AfterBytes = 0 // the whole pair is gone from context
			}
			trims = append(trims, ev)
		}
	}
	return trims
}

// strArg reads a string argument from a tool call's parsed args map (absent
// or mistyped → "").
func strArg(args map[string]interface{}, key string) string {
	s, _ := args[key].(string)
	return s
}

// findToolCallByID returns the first tool call (across all iterations, in
// session order) whose tool_use id matches id.
func findToolCallByID(s *Session, id string) (ToolCall, bool) {
	if id == "" {
		return ToolCall{}, false
	}
	for _, iter := range s.Iterations {
		for _, tc := range iter.ToolCalls {
			if tc.ID == id {
				return tc, true
			}
		}
	}
	return ToolCall{}, false
}
