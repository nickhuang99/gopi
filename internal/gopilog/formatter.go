package gopilog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/fullid"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/toolnames"
)

const (
	headerSep = "═══════════════════════════════════════════════════════════════"
)

// Task line length limits for single-line summaries.
// Defined once here (Rule 2: single source of truth) and shared by
// reader.go's loadAllSummaries and the summary formatters.
const (
	summaryTaskMaxLen   = 120 // max task chars kept in SessionSummary
	summaryRowTaskMax   = 100 // max task chars per row in --last N summaries
	summaryTableTaskMax = 60  // max task chars per row in --all --summary table
)

// Display truncation caps for view rendering (PROJECT.md Rule 1: no magic
// numbers). All are rune-count caps applied via truncateRunes so a CJK
// multi-byte rune is never sliced mid-sequence — the old byte cuts
// ([:300]/[:77]/[:57]/[:500]) produced U+FFFD replacement chars on CJK
// sessions (review P1).
const (
	formatTextMaxLen   = 300 // max runes per text block in FormatSession
	bashResultMaxLen   = 80  // max runes of a bash result first line (77 + "...")
	editPreviewMaxLen  = 60  // max runes of an edit old/new string preview (57 + "...")
	chatThinkingMaxLen = 500 // max runes of the combined thinking block in FormatChat
	// grepMatchWindowRunes caps the context a --grep hit renders around its
	// match, PER FIELD. Without it a content search answers a ~75-byte question
	// with a whole block: measured on session 004c3578-37f7-4e57-852f-b9c362e5c460,
	// `--grep "timed out after 1s"` rendered 19393 bytes because the matching
	// write call carried a 17933-byte `content` argument — 99.6% of the answer
	// was prose the reader did not ask for. A search surface must show WHERE the
	// match is and enough of it to be usable; a field that fits inside the
	// window is still rendered in full, so nothing a search can legitimately
	// need is hidden.
	grepMatchWindowRunes = 400
	// grepMatchFieldResults / grepMatchFieldInput name WHICH field of a tool
	// call the window applies to, so the renderer can say where the match was
	// found instead of leaving the reader to scan two blocks.
	grepMatchFieldInput   = "input"
	grepMatchFieldResults = "result"
	// grepPromptKind is the grepMatch kind for a USER row (the operator's own
	// message, or a harness injection). Named (Rule 1) because three sites
	// compare on it: the collector, the renderer and the tests.
	grepPromptKind = "prompt"
	// grepPromptTurnLabel / grepPromptHarnessLabel name WHICH face a prompt row
	// came from. The distinction is the reader's own (Iteration.UserPrompt vs
	// Iteration.HarnessPrompts), so a search hit never has to guess.
	grepPromptTurnLabel    = "operator turn"
	grepPromptHarnessLabel = "harness row"
)

// truncateRunes truncates s to at most maxLen runes, appending "..." when
// truncated. Rune-safe: never slices a UTF-8 rune mid-sequence (byte slicing
// produced U+FFFD replacement chars on CJK sessions — review P1). Returns s
// unchanged when it already fits.
func truncateRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen > 3 {
		return string(runes[:maxLen-3]) + "..."
	}
	return string(runes[:maxLen])
}

// oneLine collapses all whitespace runs (incl. newlines) into single spaces
// and truncates to maxLen runes (reserving room for a "..." suffix).
func oneLine(s string, maxLen int) string {
	s = strings.Join(strings.Fields(s), " ")
	return truncateRunes(s, maxLen)
}

// FormatSession formats a full session output
func FormatSession(s *Session) string {
	var sb strings.Builder

	// Header
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("Session: %s\n", s.ID))
	sb.WriteString(fmt.Sprintf("Model:   %s\n", s.Model))
	sb.WriteString(fmt.Sprintf("Started: %s\n", formatTime(s.Started)))
	sb.WriteString(fmt.Sprintf("Workdir: %s\n", s.Workdir))
	sb.WriteString(headerSep + "\n\n")

	// Task — the session's accepted prompt (first user row). Guarded: the
	// prompt may itself be a binary file read as prompt text (see tasktext.go).
	if s.Task != "" {
		sb.WriteString("TASK:\n")
		for _, line := range sessionTaskLines(s.Task) {
			sb.WriteString("  " + line + "\n")
		}
		sb.WriteString("\n")
	}

	// Iterations (grouped by API call)
	globalToolNum := 0
	for _, iter := range s.Iterations {
		// header: TOOLS (elapsed)
		if iter.ElapsedMs > 0 {
			sb.WriteString(fmt.Sprintf("TOOLS (%dms):\n", iter.ElapsedMs))
		} else {
			sb.WriteString("TOOLS:\n")
		}

		// tool calls
		for _, tc := range iter.ToolCalls {
			globalToolNum++
			sb.WriteString(formatToolCall(globalToolNum, tc))
		}

		// text blocks (if no tool calls, show response)
		if len(iter.ToolCalls) == 0 {
			for _, text := range iter.TextBlocks {
				sb.WriteString("  " + truncateRunes(text, formatTextMaxLen) + "\n\n")
			}
		}

		sb.WriteString("\n")
	}

	// NO COST BLOCK HERE, AND THAT IS THE POINT OF THIS CHANGE.
	//
	// A `COST:` block at the tail of a LOG reads as something the session wrote
	// at its end. Nothing in it is logged: every number was CALCULATED by a
	// formatter from the parsed `request` rows. The numbers now live on
	// `gopi session show <id>` — the view backed by a RECORD, reading what the
	// session recorded about itself (internal/gopilog/cost.go carries the ruling
	// and the reasoning). What a log prints here is the POINTER, never a second
	// copy: silence would be indistinguishable from "this session recorded no
	// cost" (Rule 15).
	sb.WriteString(CostPointerLine() + "\n")

	// Exit. A log with NO session_end event is NOT a successful exit: nothing
	// recorded how the run ended (killed, panicked before the end hook, still
	// running). Rendering it as success is a false claim about the one fact
	// an operator reads this block FOR (measured 2026-09-29: a chat SIGKILLed
	// mid tool-call printed "EXIT: SUCCESS (exit=0)"), so the no-end shape
	// gets its own words.
	sb.WriteString("\nEXIT: ")
	switch {
	case s.EndUnrecorded:
		sb.WriteString(sessionNoEndLabel + "\n")
	default:
		if s.ExitCode == 0 {
			sb.WriteString("SUCCESS (exit=0)\n")
		} else {
			sb.WriteString(fmt.Sprintf("FAILED (exit=%d)\n", s.ExitCode))
		}
	}

	return sb.String()
}

// sessionNoEndLabel is what the exit row prints for a session whose log
// carries no session_end event. ONE spelling, referenced by the formatter (and
// by the tests that pin the shape), so the wording cannot drift from the fact.
const sessionNoEndLabel = "UNKNOWN (no session_end recorded)"

// FormatSummary formats a single session summary line. The SESSION column
// carries the FULL session id: it is the operand an operator pastes into
// `gopi session log <session-id>`, and a truncated id is refused there (epic
// full-id-no-prefix), so printing a chunk would print something unusable.
// Spelled once in the header and once here so the table cannot drift.
func FormatSummary(s *Session) string {
	task := oneLine(sessionTaskLine(s.Task), summaryRowTaskMax)
	exitStr := "OK"
	if s.ExitCode != 0 {
		exitStr = fmt.Sprintf("EXIT=%d", s.ExitCode)
	}
	return summaryRow(s.ID, s.Model, s.ElapsedSec, summaryPassText(s.EndUnrecorded, s.TotalIterations),
		formatNum(s.TotalTokens), exitStr, task)
}

// FormatSummaryTable formats a table of all session summaries
func FormatSummaryTable(summaries []SessionSummary) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-*s %-16s %8s %*s %9s  %s  %s\n",
		summarySessionIDWidth, summarySessionHeaderSESSION, summarySessionHeaderMODEL,
		summarySessionHeaderTIME, len(summaryPassColumnWord), summaryPassColumnWord,
		summarySessionHeaderTOKENS, summarySessionHeaderEXIT, summarySessionHeaderTASK))
	sb.WriteString(strings.Repeat("-", 130) + "\n")

	for _, ss := range summaries {
		exitStr := "OK"
		if ss.ExitCode != 0 {
			exitStr = fmt.Sprintf("EXIT=%d", ss.ExitCode)
		}
		task := oneLine(sessionTaskLine(ss.Task), summaryTableTaskMax)
		sb.WriteString(summaryRow(ss.SessionID, ss.Model, ss.ElapsedSec, summaryPassText(ss.EndUnrecorded, ss.TotalIterations),
			formatNum(ss.TotalTokens), exitStr, task))
	}
	return sb.String()
}

// The SESSION column of both summary views carries a FULL session id, so its
// width is a full uuid (fullid.UUIDLen) — never the 8-character chunk it used
// to print. The row format is DERIVED from the widths and built ONCE, so the
// two summary views cannot drift apart (they used to spell their own inline
// width strings, and the header already disagreed with the body).
const (
	summarySessionIDWidth = fullid.UUIDLen // the IDENTITY column: never truncated
	summaryModelWidth     = 16
	summaryExitWidth      = 6
	// summaryPassColumnWord heads the summary views' loop-count column. It is
	// the LOOP PASS's short word (sessionevent.CounterUnitLoopPass, "loop
	// passes"), never "iters": the old header printed the loop's own count under
	// a word the round surfaces use for the `request`-row count, so a reader
	// comparing `session log --last 3` with `session log --rounds` read two
	// different quantities as one (Rule 14; measured 2026-10-16).
	summaryPassColumnWord = "PASSES"
)

// The summary table's other column heads, spelled once (Rule 1b/13: a header
// typed at two call sites is two headers).
const (
	summarySessionHeaderSESSION = "SESSION"
	summarySessionHeaderMODEL   = "MODEL"
	summarySessionHeaderTIME    = "TIME"
	summarySessionHeaderTOKENS  = "TOKENS"
	summarySessionHeaderEXIT    = "EXIT"
	summarySessionHeaderTASK    = "TASK"
)

// summaryRowFmt is the ONE row format both summary views render with: the
// SESSION column is sized by summarySessionIDWidth, so widening the identity
// column (or renaming a column) changes every view at once.
var summaryRowFmt = fmt.Sprintf("%%-%ds %%-%ds %%6.1fs %%%ds %s %%7s tok  %%-%ds  %%s\n",
	summarySessionIDWidth, summaryModelWidth, len(summaryPassColumnWord),
	summaryPassColumnWord, summaryExitWidth)

// summaryRow renders one summary line from its already-formatted fields. Both
// FormatSummary (--last N) and FormatSummaryTable (--all --summary) go through
// it: the views differ in their TASK truncation, never in their identity
// column.
func summaryRow(id, model string, elapsed float64, passes, tokens, exit, task string) string {
	return fmt.Sprintf(summaryRowFmt, id, model, elapsed, passes, tokens, exit, task)
}

// summaryPassText is the PASSES column's cell: the recorded pass count, or the
// unrecorded dash for a session that never wrote `session_end`.
//
// THE COLUMN USED TO PRINT A NUMBER THAT WAS NOT A PASS COUNT. For a live or
// killed run the reader filled TotalIterations with the `request`-row count, so
// `session log --last 3` printed "468 PASSES" for a session whose own live
// screen said turn120 — the ROUND count wearing the PASS word (measured
// 2026-10-26, task 01a17492). The dash is the honest cell: it says the count
// does not exist rather than showing a neighbouring quantity in its place.
func summaryPassText(endUnrecorded bool, passes int) string {
	if endUnrecorded {
		return sessionPassUnrecordedDash
	}
	return strconv.Itoa(passes)
}

func formatToolCall(num int, tc ToolCall) string {
	var sb strings.Builder

	// format: [N] tool_name: args
	switch tc.Name {
	case toolnames.ToolBash:
		if cmd, ok := tc.Args["command"].(string); ok {
			sb.WriteString(fmt.Sprintf("  [%d] bash: %s\n", num, cmd))
		} else {
			sb.WriteString(fmt.Sprintf("  [%d] bash: (no command)\n", num))
		}
		// show result summary
		if tc.ExitCode != nil {
			sb.WriteString(fmt.Sprintf("      → exit=%d, %d bytes\n", *tc.ExitCode, len(tc.Result)))
		} else if tc.Result != "" {
			// show first line of result
			lines := strings.SplitN(tc.Result, "\n", 2)
			summary := truncateRunes(lines[0], bashResultMaxLen)
			sb.WriteString(fmt.Sprintf("      → %s\n", summary))
		}
		// highlight git commits
		if strings.Contains(tc.Result, "git commit") {
			sb.WriteString("      🔷 GIT COMMIT FOUND\n")
		}

	case toolnames.ToolRead:
		if path, ok := tc.Args["path"].(string); ok {
			sb.WriteString(fmt.Sprintf("  [%d] read: %s", num, path))
			if offset, ok := tc.Args["offset"]; ok {
				sb.WriteString(fmt.Sprintf(" (offset=%v", offset))
				if limit, ok := tc.Args["limit"]; ok {
					sb.WriteString(fmt.Sprintf(", limit=%v", limit))
				}
				sb.WriteString(")")
			}
			sb.WriteString(fmt.Sprintf("\n      → %d bytes read\n", len(tc.Result)))
		}

	case toolnames.ToolWrite:
		if path, ok := tc.Args["path"].(string); ok {
			contentLen := 0
			if content, ok := tc.Args["content"].(string); ok {
				contentLen = len(content)
			}
			sb.WriteString(fmt.Sprintf("  [%d] write: %s (%d bytes)\n", num, path, contentLen))
			if tc.Result != "" {
				firstLine := strings.SplitN(tc.Result, "\n", 2)[0]
				sb.WriteString(fmt.Sprintf("      → %s\n", firstLine))
			}
		}

	case toolnames.ToolEdit:
		if path, ok := tc.Args["path"].(string); ok {
			oldStr := ""
			newStr := ""
			if s, ok := tc.Args["old_string"].(string); ok {
				oldStr = s
			} else if s, ok := tc.Args["oldStr"].(string); ok {
				oldStr = s
			}
			if s, ok := tc.Args["new_string"].(string); ok {
				newStr = s
			} else if s, ok := tc.Args["newStr"].(string); ok {
				newStr = s
			}
			sb.WriteString(fmt.Sprintf("  [%d] edit: %s\n", num, path))
			sb.WriteString(fmt.Sprintf("      old: %s\n", truncateRunes(oldStr, editPreviewMaxLen)))
			sb.WriteString(fmt.Sprintf("      new: %s\n", truncateRunes(newStr, editPreviewMaxLen)))
		}

	case toolnames.ToolGitBlame:
		var fp string
		if s, ok := tc.Args["file_path"].(string); ok {
			fp = s
		}
		var start, end float64
		if s, ok := tc.Args["start_line"].(float64); ok {
			start = s
		}
		if s, ok := tc.Args["end_line"].(float64); ok {
			end = s
		}
		if start > 0 && end > 0 {
			sb.WriteString(fmt.Sprintf("  [%d] git_blame: %s (lines %d-%d)\n", num, fp, int(start), int(end)))
		} else {
			sb.WriteString(fmt.Sprintf("  [%d] git_blame: %s\n", num, fp))
		}
		if tc.Result != "" {
			firstLine := strings.SplitN(tc.Result, "\n", 2)[0]
			sb.WriteString(fmt.Sprintf("      → %s\n", firstLine))
		}

	default:
		sb.WriteString(fmt.Sprintf("  [%d] %s: %v\n", num, tc.Name, tc.Args))
	}
	return sb.String()
}

func formatTime(t interface{}) string {
	switch v := t.(type) {
	case time.Time:
		return v.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", t)
	}
}

func formatNum(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	result := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

func sumPromptCacheHit(s *Session) int {
	total := 0
	for _, iter := range s.Iterations {
		total += iter.CacheHit
	}
	return total
}

func sumPromptCacheMiss(s *Session) int {
	total := 0
	for _, iter := range s.Iterations {
		total += iter.CacheMiss
	}
	return total
}

// SessionPromptCacheHit / SessionPromptCacheMiss are the two sums above, EXPORTED
// for the one caller outside this package that needs the same answer: the chat
// status row's Σ reading on a RESUMED session (cmd/agent/cli_chat_cache_base.go),
// which must include the cache usage the session carried before this run started.
//
// THE VALUE MUST BE THE ONE THE COST BLOCKS PRINT, which is why the caller READS
// this function rather than adding the column itself: `gopi session stats` and
// `gopi session log`'s COST block both print these two numbers, and the
// acceptance criterion for that fix is that the row's Σ agrees with `session
// stats` — a second addition written at the caller would be a second definition
// free to drift from this one (Rule 14).
//
// WHAT THE NUMBER MEANS. s.Iterations carry each request's OWN usage, already
// derived from the row's declared quantity by internal/requestusage (a chat
// session's rows are running totals, a task session's are increments), so this
// sum is the session's counted cache total over EVERY run segment in the log —
// not just the last one.
func SessionPromptCacheHit(s *Session) int { return sumPromptCacheHit(s) }

// SessionPromptCacheMiss is SessionPromptCacheHit's counterpart for the tokens
// the cache did NOT serve (the write side: on this transport the miss that
// creates the entry). Same fold, same definition; see that function's note.
func SessionPromptCacheMiss(s *Session) int { return sumPromptCacheMiss(s) }

// FormatResponse extracts only user prompts and assistant text responses.
// Filters out thinking, tool_use, and tool_result events.
func FormatResponse(s *Session, textMode bool) string {
	var sb strings.Builder

	for _, iter := range s.Iterations {
		for _, text := range iter.TextBlocks {
			if textMode {
				sb.WriteString(text)
				sb.WriteString("\n")
			} else {
				sb.WriteString(fmt.Sprintf("RESPONSE [iteration %d]:\n", iter.Number))
				for _, line := range strings.Split(text, "\n") {
					sb.WriteString("  " + line + "\n")
				}
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// FormatThinking extracts only reasoning/thinking blocks.
func FormatThinking(s *Session, textMode bool) string {
	var sb strings.Builder

	for _, iter := range s.Iterations {
		for _, thinking := range iter.ThinkingBlocks {
			if textMode {
				sb.WriteString(thinking)
				sb.WriteString("\n")
			} else {
				sb.WriteString(fmt.Sprintf("THINKING [iteration %d]:\n", iter.Number))
				for _, line := range strings.Split(thinking, "\n") {
					sb.WriteString("  " + line + "\n")
				}
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// filteredToolMatches is a single filtered tool invocation rendered for
// --filter/--memory output: the tool_use event plus its matching tool_result.
type filteredToolMatch struct {
	call ToolCall
}

// FormatToolFilter renders only tool calls whose name is in names, in session
// order, each as: header line (timestamp + tool_use_id + name), the complete
// tool input (query/topic/question/answer ...), and the matching tool_result's
// full content. Tools with no tool_result are still shown with "(no result)".
// It is a VIEW filter: raw session JSONL (--json) and all other formats are
// untouched.
func FormatToolFilter(s *Session, names map[string]bool) string {
	var sb strings.Builder

	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("Session: %s\n", s.ID))
	sb.WriteString(fmt.Sprintf("Filter:  %s\n", strings.Join(keysInOrder(names), ", ")))
	sb.WriteString(fmt.Sprintf("Started: %s\n", formatTime(s.Started)))
	sb.WriteString(headerSep + "\n\n")

	if len(names) == 0 {
		sb.WriteString("(no tool filter names given)\n")
		return sb.String()
	}

	var matches []filteredToolMatch
	for _, iter := range s.Iterations {
		for _, tc := range iter.ToolCalls {
			if names[tc.Name] {
				matches = append(matches, filteredToolMatch{call: tc})
			}
		}
	}

	if len(matches) == 0 {
		sb.WriteString("No matching tool calls found in this session.\n")
		sb.WriteString("(Try a family shortcut --memory/--search/--web/--job/--context/--vision/--text2img/--git or\n")
		sb.WriteString(" --filter <tool-name>; available names are the tool_use \"name\"\n")
		sb.WriteString(" field, e.g. code_search, keyword_search, web_search, web_fetch,\n")
		sb.WriteString(" memory_search, memory_store, job,\n")
		sb.WriteString(" context_trim, vision_analyze, text2img, git_blame, bash, read, write, edit.)\n")
		return sb.String()
	}

	for i, m := range matches {
		sb.WriteString(formatFilteredTool(i+1, len(matches), m.call))
		if i < len(matches)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// keysInOrder returns the sorted keys of a set for a deterministic header.
func keysInOrder(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// grepMatch is one entry matched by FormatGrep.
type grepMatch struct {
	kind  string // grepMatch's kind: "tool" | "text" | "thinking" | grepPromptKind
	iter  int
	call  ToolCall
	block string
	// label names a prompt row's FACE (grepPromptTurnLabel /
	// grepPromptHarnessLabel); empty for the other kinds.
	label string
}

// grepWindow renders s around its first match of the already-lowercased query
// q, capped at windowRunes runes of CONTEXT — the match itself is
// always inside the window, which is what makes a capped result still a usable
// answer. A field that fits entirely is returned unchanged (so a caller can
// compare the result against its input to tell "this is the whole thing").
//
// A window that did cut states its shape ([head…tail] and the full rune count),
// because a reader who cannot see the elision would read a truncated result as
// a complete one. Rune-safe: it never slices a UTF-8 rune mid-sequence, the
// same discipline truncateRunes follows for CJK sessions.
func grepWindow(s, q string) string { return grepWindowSized(s, q, grepMatchWindowRunes, true) }

// grepWindowSized is grepWindow with the cap and the on/off switch as
// parameters, so FormatGrep's `full` opt-out and the tests share ONE window
// implementation instead of a second rendering path that could drift.
func grepWindowSized(s, q string, windowRunes int, capped bool) string {
	runes := []rune(s)
	if !capped || len(runes) <= windowRunes {
		return s
	}
	idx := strings.Index(strings.ToLower(s), q)
	if idx < 0 {
		// No match in this field (the match was in a sibling field): a plain
		// head keeps the entry identifiable without pretending to be context.
		return string(runes[:windowRunes]) +
			fmt.Sprintf("\n...[%d runes total, no match in this field]...", len(runes))
	}
	// The byte offset must become a rune offset before slicing: a CJK match
	// that starts at byte 300 starts at rune 100.
	matchRune := utf8.RuneCountInString(s[:idx])
	matchRunes := utf8.RuneCountInString(q)
	head := matchRune - (windowRunes-matchRunes)/2
	if head < 0 {
		head = 0
	}
	tail := head + windowRunes
	if tail > len(runes) {
		tail = len(runes)
		head = tail - windowRunes
		if head < 0 {
			head = 0
		}
	}
	var b strings.Builder
	if head > 0 {
		b.WriteString("[...")
	}
	b.WriteString(string(runes[head:tail]))
	if tail < len(runes) {
		b.WriteString("...]")
	}
	fmt.Fprintf(&b, "\n[%d runes total]", len(runes))
	return b.String()
}

// FormatGrep renders a session content search: every USER row (the operator's
// own message and the harness's injected rows), tool call, text response and
// thinking block whose content contains query (case-insensitive) is shown.
// When toolSet is non-empty, only tool calls whose name is in the set are
// searched — that is how `gopi session log <id> --grep TEXT --search/--web/--job/...`
// scopes the match, the log-side analogue of code_search/keyword_search.
//
// USER ROWS ARE SEARCHED (measured gap, 2026-10-31): until then the loop below
// walked only Iterations' tool calls, text and thinking, so the most-cited
// content of a session — what the OPERATOR said — was the one thing `--grep`
// could not find. Measured on session 01a18f0f-2a05-71b5-8ceb-5e7a0dfe1cd3:
// the phrase `EPIC GOAL` occurs in the run's own briefing (a `user` row,
// session.jsonl line 2) and `--grep "EPIC GOAL"` returned it in NO match,
// while `grep -c "EPIC GOAL"` on the same file counted it. A content search that
// skips half the conversation answers "no entries contain that" about text that
// is right there.
func FormatGrep(s *Session, query string, toolSet map[string]bool, full bool) string {
	var sb strings.Builder
	q := strings.ToLower(query)

	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("Session: %s\n", s.ID))
	if len(toolSet) > 0 {
		sb.WriteString(fmt.Sprintf("Grep:    %q in %s\n", query, strings.Join(keysInOrder(toolSet), ", ")))
	} else {
		sb.WriteString(fmt.Sprintf("Grep:    %q (all content)\n", query))
	}
	sb.WriteString(fmt.Sprintf("Started: %s\n", formatTime(s.Started)))
	sb.WriteString(headerSep + "\n\n")

	scoped := len(toolSet) > 0
	var matches []grepMatch
	matchPrompt := func(iter int, text, label string) {
		if strings.Contains(strings.ToLower(text), q) {
			matches = append(matches, grepMatch{kind: grepPromptKind, iter: iter, block: text, label: label})
		}
	}
	for _, iter := range s.Iterations {
		// The operator's own rows and the harness's rows are searched BEFORE
		// the tool calls of the same iteration, so a hit list reads in the
		// order the model met the material. A tool-name scope (--search/--web)
		// is about TOOLS, so it suppresses prompt rows — that scope asks "which
		// tool call said X", a different question.
		if !scoped {
			// sessionTaskText is the package's ONE guard on captured prompt
			// text (taskemit_gate_test.go's Rule-13 sweep): a session can carry
			// control bytes a terminal cannot print, and every emit site routes
			// through it rather than slicing the field directly.
			matchPrompt(iter.Number, sessionTaskText(iter.UserPrompt), grepPromptTurnLabel)
			for _, hp := range iter.HarnessPrompts {
				matchPrompt(iter.Number, sessionTaskText(hp), grepPromptHarnessLabel)
			}
		}
		for _, tc := range iter.ToolCalls {
			if scoped && !toolSet[tc.Name] {
				continue
			}
			if strings.Contains(strings.ToLower(tc.Name), q) ||
				strings.Contains(strings.ToLower(tc.Result), q) ||
				strings.Contains(strings.ToLower(fmt.Sprint(tc.Args)), q) ||
				strings.Contains(strings.ToLower(string(tc.InputRaw)), q) {
				matches = append(matches, grepMatch{kind: "tool", iter: iter.Number, call: tc})
			}
		}
		if scoped {
			// With a tool filter set, --grep searches only tool calls.
			continue
		}
		for _, tb := range iter.TextBlocks {
			if strings.Contains(strings.ToLower(tb), q) {
				matches = append(matches, grepMatch{kind: "text", iter: iter.Number, block: tb})
			}
		}
		for _, th := range iter.ThinkingBlocks {
			if strings.Contains(strings.ToLower(th), q) {
				matches = append(matches, grepMatch{kind: "thinking", iter: iter.Number, block: th})
			}
		}
	}

	if len(matches) == 0 {
		sb.WriteString(fmt.Sprintf("No entries in this session contain %q.\n", query))
		return sb.String()
	}

	for i, m := range matches {
		switch m.kind {
		case grepPromptKind:
			sb.WriteString(fmt.Sprintf("── PROMPT %d/%d: %s, iteration %d ──\n", i+1, len(matches), m.label, m.iter))
			sb.WriteString(indentBlock(grepWindowSized(m.block, q, grepMatchWindowRunes, !full), "    "))
		case "tool":
			sb.WriteString(formatGrepTool(i+1, len(matches), m.call, q, full))
		case "text":
			sb.WriteString(fmt.Sprintf("── TEXT %d/%d: iteration %d ──\n", i+1, len(matches), m.iter))
			sb.WriteString(indentBlock(grepWindowSized(m.block, q, grepMatchWindowRunes, !full), "    "))
		case "thinking":
			sb.WriteString(fmt.Sprintf("── THINKING %d/%d: iteration %d ──\n", i+1, len(matches), m.iter))
			sb.WriteString(indentBlock(m.block, "    "))
		}
		if i < len(matches)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func formatFilteredTool(num, total int, tc ToolCall) string {
	var sb strings.Builder

	ts := tc.Timestamp
	if ts.IsZero() {
		ts = time.Time{}
	}
	sb.WriteString(fmt.Sprintf("── TOOL %d/%d: %s @ %s ──\n", num, total, tc.Name, ts.Format("15:04:05.000")))
	sb.WriteString(fmt.Sprintf("  tool_use_id: %s\n", tc.ID))
	sb.WriteString("  input:\n")
	sb.WriteString(indentBlock(formatToolInput(tc), "    "))
	sb.WriteString("\n  result:\n")
	if tc.Result == "" {
		sb.WriteString("    (no tool_result matched)\n")
	} else {
		sb.WriteString(indentBlock(tc.Result, "    "))
	}
	return sb.String()
}

// formatGrepTool renders one matched tool call for --grep, windowing BOTH
// fields (input and result) around their first match so a hit on a large
// argument cannot dump the whole block into the answer. It names which field
// matched, because with two windows open a reader otherwise has to diff them
// by eye to find the needle. The window never throws the match away, and a
// field that fits is rendered whole (grepWindow decides).
func formatGrepTool(num, total int, tc ToolCall, q string, full bool) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("── TOOL %d/%d: %s @ %s ──\n", num, total, tc.Name, tc.Timestamp.Format("15:04:05.000")))
	sb.WriteString(fmt.Sprintf("  tool_use_id: %s\n", tc.ID))
	sb.WriteString(fmt.Sprintf("  matched in: %s\n", grepMatchedFields(tc, q)))
	sb.WriteString("  input:\n")
	sb.WriteString(indentBlock(grepWindowSized(formatToolInput(tc), q, grepMatchWindowRunes, !full), "    "))
	sb.WriteString("\n  result:\n")
	if tc.Result == "" {
		sb.WriteString("    (no tool_result matched)\n")
	} else {
		sb.WriteString(indentBlock(grepWindowSized(tc.Result, q, grepMatchWindowRunes, !full), "    "))
	}
	return sb.String()
}

// grepMatchedFields names the field(s) of tc that actually contain q — the
// same four fields FormatGrep's collector tests, so the two cannot disagree
// about why a call was listed.
func grepMatchedFields(tc ToolCall, q string) string {
	var fields []string
	if strings.Contains(strings.ToLower(tc.Name), q) {
		fields = append(fields, "tool name")
	}
	if strings.Contains(strings.ToLower(tc.Result), q) {
		fields = append(fields, grepMatchFieldResults)
	}
	if strings.Contains(strings.ToLower(fmt.Sprint(tc.Args)), q) ||
		strings.Contains(strings.ToLower(string(tc.InputRaw)), q) {
		fields = append(fields, grepMatchFieldInput)
	}
	if len(fields) == 0 {
		return "(none)"
	}
	return strings.Join(fields, ", ")
}

// formatToolInput renders a tool call's complete input. It prefers the raw
// block.Input so key order and all fields (query/topic/question/answer) are
// preserved exactly as logged; falls back to the parsed Args map.
func formatToolInput(tc ToolCall) string {
	if len(tc.InputRaw) > 0 {
		return prettyJSONNoEscape(tc.InputRaw)
	}
	if len(tc.Args) == 0 {
		return "(empty input)"
	}
	return prettyJSONValue(tc.Args)
}

// Context-trim view constants (Rule 1: no magic numbers).
const (
	contextTrimIDCol = 40 // width of the "context_trim call" column
)

// FormatContextTrim renders every context_trim activity in a session as a
// table: the context_trim call's own id, the trimmed call (id + tool name),
// the outcome (replaced/removed/no-op), and the byte-size comparison between
// the trimmed result's original logged size and its post-trim size. Sessions
// with no context_trim calls get a clear empty state.
func FormatContextTrim(s *Session) string {
	var sb strings.Builder

	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("Session: %s\n", s.ID))
	sb.WriteString(fmt.Sprintf("Model:   %s\n", s.Model))
	sb.WriteString(fmt.Sprintf("Started: %s\n", formatTime(s.Started)))
	sb.WriteString("View:    context_trim activity\n")
	sb.WriteString(headerSep + "\n\n")

	trims := collectContextTrims(s)
	if len(trims) == 0 {
		sb.WriteString("No context_trim activity in this session.\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("CONTEXT TRIM — %d event(s)\n", len(trims)))
	sb.WriteString(fmt.Sprintf("%-*s %-*s %-10s %12s %12s %s\n",
		contextTrimIDCol, "CONTEXT_TRIM CALL", contextTrimIDCol, "TRIMMED CALL",
		"OUTCOME", "BEFORE", "AFTER", "DETAIL"))
	sb.WriteString(strings.Repeat("─", 130) + "\n")

	for i, ev := range trims {
		target := ev.TargetCallID
		if ev.TargetName != "" {
			target = ev.TargetName + " " + target
		}
		if target == "" {
			target = "(unknown)"
		}
		outcome := ev.Outcome
		if outcome == "" {
			outcome = "unknown"
		}
		detail := "deleted pair from context"
		if ev.Outcome == "replaced" {
			detail = "replaced with " + strconv.Itoa(len(ev.ReplaceWith)) + "B conclusion"
		} else if ev.Outcome == "removed" {
			detail = fmt.Sprintf("%d message(s) dropped", ev.RemovedMessages)
		} else if ev.Outcome == "noop" {
			detail = "call not found / already trimmed"
		}
		sb.WriteString(fmt.Sprintf("%-*s %-*s %-10s %12d %12d  %s\n",
			contextTrimIDCol, ev.CallID, contextTrimIDCol, target,
			outcome, ev.BeforeBytes, ev.AfterBytes, detail))

		// The replacement conclusion is the "防遗忘" payload of a replace
		// — show it in full (indented) so the trim's reason survives the
		// view; conclusions are short by design (one-line instructions).
		if ev.Outcome == "replaced" && ev.ReplaceWith != "" {
			sb.WriteString("    replace_with: " + ev.ReplaceWith + "\n")
		}
		if i < len(trims)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// prettyJSONNoEscape pretty-prints a raw JSON object as indented JSON while
// disabling Go's default HTML escaping, so literal < > & characters render as
// themselves instead of \u003c \u003e \u0026. Key and array order are preserved
// via a json.Decoder token walk (block.Input is kept verbatim per design).
//
// It deliberately never calls dec.More(): json.Decoder.Token() skips the
// inter-element comma/colon tokens transparently, and container end is
// detected from the closing { } [ ] Delim tokens. This avoids the documented
// More()+Token() recursion quirk where a parent container's More() returns
// false right after a nested container value has been consumed.
func prettyJSONNoEscape(raw []byte) string {
	var sb strings.Builder
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	first, err := dec.Token()
	if err != nil {
		return string(raw)
	}
	if err := emitJSON(&sb, dec, 0, first); err == nil {
		return sb.String()
	}
	// The raw bytes are not a well-formed single JSON value (unexpected stray
	// content): fall back to the raw text so nothing is dropped or mangled.
	return string(raw)
}

// emitJSON writes one complete JSON value whose first token is tok (a Delim
// '{'/'[' or a scalar), consuming the remainder of that value from dec and
// indenting at depth. Array/object termination is detected by reading the next
// token and checking for the closing Delim — since Token() skips commas and
// colons, no separator handling is needed and key order is preserved.
func emitJSON(sb *strings.Builder, dec *json.Decoder, depth int, tok json.Token) error {
	delim, _ := tok.(json.Delim)
	switch delim {
	case '{':
		sb.WriteString("{\n")
		i := 0
		for {
			kt, err := dec.Token() // key string, or '}' closing the object
			if err != nil {
				return err
			}
			if d, ok := kt.(json.Delim); ok && d == '}' {
				sb.WriteString("\n" + strings.Repeat("  ", depth) + "}")
				return nil
			}
			if i > 0 {
				sb.WriteString(",\n")
			}
			sb.WriteString(strings.Repeat("  ", depth+1))
			writeEscapedString(sb, kt.(string))
			sb.WriteString(": ")
			vt, err := dec.Token() // first token of the value
			if err != nil {
				return err
			}
			if err := emitJSON(sb, dec, depth+1, vt); err != nil {
				return err
			}
			i++
		}
	case '[':
		sb.WriteString("[\n")
		i := 0
		for {
			et, err := dec.Token() // first token of an element, or ']'
			if err != nil {
				return err
			}
			if d, ok := et.(json.Delim); ok && d == ']' {
				sb.WriteString("\n" + strings.Repeat("  ", depth) + "]")
				return nil
			}
			if i > 0 {
				sb.WriteString(",\n")
			}
			sb.WriteString(strings.Repeat("  ", depth+1))
			if err := emitJSON(sb, dec, depth+1, et); err != nil {
				return err
			}
			i++
		}
	default:
		writeScalar(sb, tok)
	}
	return nil
}

// writeScalar emits a non-container JSON token (string, number, bool, null).
func writeScalar(sb *strings.Builder, tok json.Token) {
	switch v := tok.(type) {
	case string:
		writeEscapedString(sb, v)
	case json.Number:
		sb.WriteString(v.String())
	default:
		b, _ := json.Marshal(v) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		sb.Write(b)
	}
}

// prettyJSONValue pretty-prints an arbitrary Go value (the parsed Args map)
// with indentation and HTML escaping disabled. Key order is not guaranteed by
// encoding/json for maps, but all fields and values are preserved.
func prettyJSONValue(v interface{}) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Sprintf("%v", v)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// writeEscapedString writes s as a JSON string literal, escaping only the
// characters that require it (quote, backslash) — Go's encoding/json defaults
// to HTML-safe escaping of < > & which is undesirable for the filtered view.
func writeEscapedString(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
		case '\r':
			sb.WriteString(`\r`)
		default:
			if r < 0x20 {
				sb.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
}

// indentBlock prefixes every non-empty line of s with indent.
func indentBlock(s, indent string) string {
	if s == "" {
		return indent
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	var sb strings.Builder
	for i, ln := range lines {
		if ln == "" {
			sb.WriteString("\n")
			continue
		}
		sb.WriteString(indent + ln)
		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// ValidateSessionFile checks that a session file is usable: non-empty and
// carrying a session_start event (a crash mid-write leaves a file with no
// session_start; an empty file is a leftover). Returns a descriptive error
// for the unusable cases so --json and the human views refuse them instead
// of printing garbage/empty output (review P2 — "empty file prints
// nothing, no session_start prints verbatim with no warning").
func ValidateSessionFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return fmt.Errorf("session file is empty (no events): %s", path)
	}
	// The same JSONL reader every other session-file path uses (jsonlline.go):
	// a bare bufio.Scanner here capped a line at 10 MiB and then reported the
	// CAP as "no session_start event (partial/crash write?)" — a misattributed
	// error on exactly the session an operator is inspecting (Rule 15). The
	// shared reader streams the line and, if it ever exceeds the safety
	// ceiling, says so by name and position.
	scanner := newJSONLScanner(path, bytes.NewReader(data), jsonlLineSafetyCeiling)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Type == sessionevent.EventTypeSessionStart {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("session file has no session_start event (partial/crash write?): %s", path)
}

// FormatJSONL reads a session file and returns its raw content.
func FormatJSONL(path string) (string, error) {
	if err := ValidateSessionFile(path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FormatChat renders a session as a human-readable conversation transcript.
func FormatChat(s *Session) string {
	var sb strings.Builder

	// Header
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("Session: %s\n", s.ID))
	sb.WriteString(fmt.Sprintf("Model:   %s\n", s.Model))
	sb.WriteString(fmt.Sprintf("Started: %s\n", formatTime(s.Started)))
	sb.WriteString(headerSep + "\n\n")

	// Iterations
	for _, iter := range s.Iterations {
		// Iteration divider
		sb.WriteString(fmt.Sprintf("── Iteration %d (%s hit / %s miss) ──\n",
			iter.Number,
			formatNum(iter.CacheHit),
			formatNum(iter.CacheMiss)))
		sb.WriteString("\n")

		// User prompt — the same captured-prompt boundary as s.Task (the
		// binary-briefing defect): the renderer presents it as text, so a
		// prompt that WAS a binary file must not print its raw bytes.
		if iter.UserPrompt != "" {
			sb.WriteString("▸ USER\n")
			for _, line := range sessionTaskLines(iter.UserPrompt) {
				sb.WriteString("  " + line + "\n")
			}
			sb.WriteString("\n")
		}

		// Thinking blocks
		if len(iter.ThinkingBlocks) > 0 {
			sb.WriteString("▸ THINKING\n")
			combined := strings.Join(iter.ThinkingBlocks, "\n")
			truncated := len([]rune(combined)) > chatThinkingMaxLen
			combined = truncateRunes(combined, chatThinkingMaxLen)
			if truncated {
				combined += " (truncated)"
			}
			for _, line := range strings.Split(combined, "\n") {
				sb.WriteString("  " + line + "\n")
			}
			sb.WriteString("\n")
		}

		// Tool calls
		sb.WriteString("▸ TOOLS\n")
		if len(iter.ToolCalls) == 0 {
			sb.WriteString("  (none this iteration)\n")
		} else {
			for _, tc := range iter.ToolCalls {
				sb.WriteString(formatChatToolCall(tc))
			}
		}
		sb.WriteString("\n")

		// Response
		if len(iter.TextBlocks) > 0 {
			sb.WriteString("▸ RESPONSE\n")
			for _, text := range iter.TextBlocks {
				for _, line := range strings.Split(text, "\n") {
					sb.WriteString("  " + line + "\n")
				}
			}
			sb.WriteString("\n")
		}
	}

	// Cost summary
	totalTokens := s.TotalPrompt + s.TotalCompletion
	sb.WriteString("─────────────────────────────────────────────\n")
	sb.WriteString("COST SUMMARY\n")
	// THE THREE COUNTS, EACH UNDER ITS OWN WORD — see costCounterLines: this
	// block and `gopi session log <id>`'s COST block are the SAME three lines
	// from one function, which is what stops the word iteration from meaning
	// one number here and another there.
	sb.WriteString(costCounterLines(s))
	sb.WriteString(fmt.Sprintf("  Elapsed:    %.1fs\n", s.ElapsedSec))
	sb.WriteString(fmt.Sprintf("  Prompt:     %s tokens  (hit: %s  miss: %s)\n",
		formatNum(s.TotalPrompt),
		formatNum(sumPromptCacheHit(s)),
		formatNum(sumPromptCacheMiss(s))))
	sb.WriteString(fmt.Sprintf("  Completion: %s tokens\n", formatNum(s.TotalCompletion)))
	sb.WriteString(fmt.Sprintf("  Total:      %s tokens\n", formatNum(totalTokens)))
	// THE SPAN OF THOSE NUMBERS, on its own line, under the same helper the
	// overview box uses (Rule 14: one rendering of one fact). A resumed
	// session's totals are a SUM OVER ITS RUNS — three of them on
	// 66243525-16ab-4ce7-9133-84d74c539840 — and rows whose quantity was never
	// established are named rather than folded in (Rule 15).
	if span := usageSpanText(s.RequestUsageSegments, s.RequestUsageUnknown); span != "" {
		sb.WriteString("  " + span + "\n")
	}
	sb.WriteString("─────────────────────────────────────────────\n")

	return sb.String()
}

// iterationCountSource names WHICH PRODUCER fed the loop's own count — the
// session's own recorded end, or the JSONL fallback the reader applies to a run
// that recorded none. The two are different quantities (see
// sessionevent/counters.go), so a surface printing the number must print which
// one it is (Rule 15: absence is stated, never left to be guessed).
func iterationCountSource(s *Session) string {
	if s.EndUnrecorded {
		return sessionevent.CounterPassUnrecorded
	}
	// A RESUMED SESSION's count is a SUM over its run segments, and the caption
	// says so: the log's own last segment may report 0 passes for the run that
	// just ended, so naming only the producer would leave the SPAN wrong (task
	// 01a17ae2 — the number and its span are one claim).
	if s.PassSegments > 1 {
		return sessionevent.CounterSourceSessionEndAccumulated
	}
	return sessionevent.CounterSourceSessionEnd
}

// iterationCountText is the ONE rendering of the loop's pass count: the number
// when the session recorded one, and sessionevent.CounterPassUnrecorded's dash
// when it did not.
//
// IT EXISTS BECAUSE A PASS COUNT HAS EXACTLY ONE PRODUCER (session_end's
// total_iterations). The reader used to fill the field with the `request`-row
// count for a run that recorded no end, and three render sites printed that
// number under the PASS word — measured 2026-10-26 on session
// 41e5dade-71e1-4efc-a012-456d0532e762, whose `session log` read
//
//	Iterations: 468   (rounds)
//	Passes:     468   (loop passes — "derived from JSONL `request` events")
//
// while the LIVE screen of that same run read `turn120`. One number under two
// words, and it was the ROUND count both times. Printing the dash is the
// honest answer (Rule 15), and it makes the absence visible instead of making
// it look like an equal measurement.
func iterationCountText(s *Session) string {
	if s.EndUnrecorded {
		return sessionPassUnrecordedDash
	}
	return strconv.Itoa(s.TotalIterations)
}

// sessionPassUnrecordedDash stands where a pass count would be when the session
// recorded none. A dash, not "0": zero passes is a MEASUREMENT and this is the
// absence of one.
const sessionPassUnrecordedDash = "—"

// costCounterLines renders the session's counters — the ROUND, the TURN and the
// LOOP PASS — one per line, each under its own word and each declaring which
// neighbour it is NOT.
//
// WHY THIS IS ONE FUNCTION AND NOT TWO INLINE BLOCKS. The defect (measured
// 2026-10-16 on session 5c6cd329-3e34-42c0-96b2-9a406961b1e1): the COST block
// printed "Iterations: 31" while `--rounds`, one flag away, headed a 134-row
// table "one JSONL `request` row = one iteration". One word, two values, in one
// CLI — and the two strings lived in two different functions, so neither could
// see the other. They now come from here, so a rename moves both (Rule 14: one
// definition, and the call sites reference it rather than restating it).
//
// THE THREE NUMBERS, and this is the whole point of the block:
//
//	ROUNDS      — one JSONL `request` row = one HTTP request/response. The word
//	              iteration belongs to THIS quantity and to nothing else.
//	TURNS       — `chats.turn_count`, the LEDGER's count of the turns the
//	              session RAN (every input face equal). The column lives in the
//	              chat index, which this package cannot reach, so the number
//	              arrives through the INJECTED reader in ledger_turns.go — the
//	              same one `gopi session show <id>` prints under this word.
//	PASSES      — `session_end`'s total_iterations: the REPL loop charged one
//	              per pass it ran, which is NOT the same quantity as a turn.
//
// A FOURTH line names the PERSON-ONLY SUBSET under its own word — `human turns`
// — and it is a DETAIL, printed after the total and never beside it as a second
// count of the same thing (the operator's ruling, 2026-10-27: an external notify
// is an input too, so the subset is not the primary reading of the word). It is
// omitted when this reader can see no operator row at all: absence stated by the
// omission of a whole quantity, never as a fabricated 0 beside a real number.
//
// WHY THE TURN LINE IS THE LEDGER'S AND NOT A LOCAL COUNT. The count the
// operator reads is `chats.turn_count`, which lives in the chat index
// (internal/tools) — and internal/tools cannot be imported here (the cycle
// chat_turns.go documents). While this block could only count operator rows, it
// printed THAT number under the bare word, and on 41e5dade-71e1-4efc-a012-456d0532e762
// that read `Turns: 66` beside a ledger saying 200. It now prints the ledger's
// number through the injected reader (ledger_turns.go) — measured 2026-10-27 on
// session a60787b0-9081-451f-bd5a-9e396cfc048b: this block, `session show` and
// the status bar all read 16, and the person's own 8 prints under `human turns`
// — and when no reader is installed it says so instead of substituting the
// subset.
func costCounterLines(s *Session) string {
	var sb strings.Builder
	// ROUND — the word iteration means this and only this.
	sb.WriteString(fmt.Sprintf("  Iterations: %d  — %s\n",
		s.RequestEvents, sessionRoundCanonicalUnit))
	sb.WriteString(fmt.Sprintf("    %s: %s\n", sessionevent.CounterUnitRound, sessionevent.CounterUnitRoundIsNot))
	// TURN — the OPENED count under the bare word (the axis every replay band and
	// `--turn N` number along); the ledger's COMPLETED count follows under its
	// own word, and the person-only subset after that, also under its own.
	sb.WriteString(fmt.Sprintf("  %s: %s\n", sessionevent.CounterUnitTurn, turnCountValue(s)))
	sb.WriteString(fmt.Sprintf("    %s: %s\n", sessionevent.CounterUnitTurn, sessionevent.CounterUnitTurnIsNot))
	// Only when MEASURED and DIFFERENT — see the same clause in sessionround.go.
	if completed := turnCompletedValue(s); completed != "" {
		sb.WriteString(fmt.Sprintf("  %s: %s\n", sessionevent.CounterUnitTurnCompleted, completed))
		sb.WriteString(fmt.Sprintf("    %s\n", sessionevent.CounterUnitTurnCompletedIsNot))
	}
	if note, unmeasured := SessionHumanTurnsUnmeasured(s); unmeasured {
		// AN UNMEASURED SUBSET IS PRINTED AS UNMEASURED — never omitted (which
		// would read as "no subset to speak of") and never filled with another
		// counter's number (the defect: `human turns: 85` beside `Iterations: 85`
		// on a log whose user rows are all harness ticks). The criterion is
		// SessionHumanTurnsUnmeasured's, declared once (Rule 13/14).
		sb.WriteString(fmt.Sprintf("  %s: %s\n",
			sessionevent.CounterUnitHumanTurn, TurnCountUnmeasuredPrefix+note))
	} else if human := SessionHumanTurns(s); human > 0 {
		sb.WriteString(fmt.Sprintf("  %s: %d  — %s\n",
			sessionevent.CounterUnitHumanTurn, human, humanTurnUnitWords))
	}
	// LOOP PASS — the loop's own count, under a word that collides with nothing.
	sb.WriteString(fmt.Sprintf("  Passes:     %s  — %s\n",
		iterationCountText(s), iterationCountSource(s)))
	sb.WriteString(fmt.Sprintf("    %s: %s\n", sessionevent.CounterUnitLoopPass, sessionevent.CounterUnitLoopPassIsNot))
	return sb.String()
}

func formatChatToolCall(tc ToolCall) string {
	var sb strings.Builder

	switch tc.Name {
	case toolnames.ToolBash:
		cmd := ""
		if c, ok := tc.Args["command"].(string); ok {
			cmd = c
		}
		sb.WriteString(fmt.Sprintf("  ● bash: %s\n", cmd))
		if tc.ExitCode != nil {
			sb.WriteString(fmt.Sprintf("    → exit=%d, %d bytes\n", *tc.ExitCode, len(tc.Result)))
		} else if tc.Result != "" {
			sb.WriteString(fmt.Sprintf("    → %d bytes\n", len(tc.Result)))
		} else {
			sb.WriteString("    → TIMEOUT\n")
		}

	case toolnames.ToolRead:
		path := ""
		if p, ok := tc.Args["path"].(string); ok {
			path = p
		}
		sb.WriteString(fmt.Sprintf("  ● read: %s\n", path))
		sb.WriteString(fmt.Sprintf("    → %d bytes read\n", len(tc.Result)))

	case toolnames.ToolWrite:
		path := ""
		if p, ok := tc.Args["path"].(string); ok {
			path = p
		}
		contentLen := 0
		if content, ok := tc.Args["content"].(string); ok {
			contentLen = len(content)
		}
		sb.WriteString(fmt.Sprintf("  ● write: %s\n", path))
		sb.WriteString(fmt.Sprintf("    → %d bytes written\n", contentLen))

	case toolnames.ToolEdit:
		path := ""
		if p, ok := tc.Args["path"].(string); ok {
			path = p
		}
		sb.WriteString(fmt.Sprintf("  ● edit: %s\n", path))
		sb.WriteString(fmt.Sprintf("    → %d bytes result\n", len(tc.Result)))

	case toolnames.ToolCodeSearch, toolnames.ToolKeywordSearch, toolnames.ToolWebSearch, toolnames.ToolWebFetch:
		// Search-family tools: show the query/URL and a compact result summary
		// instead of the raw %v of Args (the full input is one --search/--web
		// --grep away).
		sb.WriteString(fmt.Sprintf("  ● %s: %s\n", tc.Name, searchToolQuery(tc)))
		first := strings.SplitN(tc.Result, "\n", 2)[0]
		if len(first) > 120 {
			first = first[:117] + "..."
		}
		if tc.Result == "" {
			sb.WriteString("    → (no tool_result matched)\n")
		} else if first == "" {
			sb.WriteString(fmt.Sprintf("    → %d bytes result\n", len(tc.Result)))
		} else {
			sb.WriteString(fmt.Sprintf("    → %s\n", first))
		}

	default:
		sb.WriteString(fmt.Sprintf("  ● %s: %v\n", tc.Name, tc.Args))
		if tc.Result != "" {
			sb.WriteString(fmt.Sprintf("    → %d bytes\n", len(tc.Result)))
		}
	}

	return sb.String()
}

// searchToolQuery extracts the user-facing query/URL field of a search-family
// tool call for the chat view. It falls back to the raw Args rendering when the
// field is missing or non-string.
func searchToolQuery(tc ToolCall) string {
	var field string
	switch tc.Name {
	case toolnames.ToolCodeSearch:
		field = "symbol"
	case toolnames.ToolKeywordSearch, toolnames.ToolWebSearch:
		field = "query"
	case toolnames.ToolWebFetch:
		field = "url"
	}
	if field != "" {
		if s, ok := tc.Args[field].(string); ok && s != "" {
			return s
		}
	}
	return fmt.Sprint(tc.Args)
}

// Task list marker constants — must match GoPi's tasklist.go.
const (
	taskListBeginMarker = "[TASK_LIST_BEGIN]"
	taskListEndMarker   = "[TASK_LIST_END]"
	taskStatusCompleted = "completed"
)

// flexibleString and todoItem are aliases for the canonical agent types
// (agent/tasklist.go). Previously these were a byte-for-byte copy maintained
// in lockstep with agent — a cross-package duplication that risked silent
// divergence. Now agent.TaskItem (with its FlexibleString ID fields and
// resolve() alt-name fallback) is the single source of truth; this package's
// formatter renders it identically.
type flexibleString = agent.FlexibleString
type todoItem = agent.TaskItem

// todoSnapshot is a task list found in a specific iteration's response.
type todoSnapshot struct {
	Iteration int        // iteration number (0 if not available)
	Tasks     []todoItem // parsed task list entries
}

// extractAllTaskLists scans all iterations and returns every task list found.
// Each snapshot represents one LLM response that contained a
// [TASK_LIST_BEGIN]...[TASK_LIST_END] block.
func extractAllTaskLists(s *Session) []todoSnapshot {
	var snapshots []todoSnapshot
	for _, iter := range s.Iterations {
		for _, text := range iter.TextBlocks {
			// Find ALL task list blocks within a single text block
			// (there may be multiple if LLM sent multiple responses in one block).
			remaining := text
			for {
				beginIdx := strings.Index(remaining, taskListBeginMarker)
				if beginIdx < 0 {
					break
				}
				remaining = remaining[beginIdx+len(taskListBeginMarker):]
				endIdx := strings.Index(remaining, taskListEndMarker)
				if endIdx < 0 {
					// BEGIN without END → balanced-prefix recovery (P0 port of
					// the agent-side ParseTaskList fix; devdoc
					// tasklist-p0-evaluation.md + tasklist-cli-todo-fix.md).
					// The LLM often forgets the closing marker; breaking here
					// silently dropped real task lists from --todo — session
					// 68827372 contains two complete JSON blocks with no END
					// and rendered as "No task list found in this session."
					// Recovered lists are first-class: they render identically
					// to closed-block lists, matching the agent-side contract
					// (ErrTaskListRecovered carries non-nil tasks).
					tasks, err := agent.ParseTaskListBalancedPrefix(remaining)
					if errors.Is(err, agent.ErrTaskListRecovered) && len(tasks) > 0 {
						snapshots = append(snapshots, todoSnapshot{
							Iteration: iter.Number,
							Tasks:     tasks,
						})
					}
					// Unrecoverable tail (mid-JSON truncation / prose
					// reference): drop it like the closed-block path drops
					// malformed JSON — no garbage list is ever fabricated.
					break
				}
				jsonStr := strings.TrimSpace(remaining[:endIdx])
				remaining = remaining[endIdx+len(taskListEndMarker):]
				if jsonStr == "" {
					continue
				}
				var tasks []todoItem
				if err := json.Unmarshal([]byte(jsonStr), &tasks); err != nil {
					continue
				}
				// Resolve alternate field names for backward compatibility.
				for i := range tasks {
					tasks[i].Resolve()
				}
				snapshots = append(snapshots, todoSnapshot{
					Iteration: iter.Number,
					Tasks:     tasks,
				})
			}
		}
	}
	return snapshots
}

// extractTaskList returns only the LAST task list (for backward compatibility).
// Deprecated: use extractAllTaskLists for full history.
func extractTaskList(s *Session) []todoItem {
	snapshots := extractAllTaskLists(s)
	if len(snapshots) == 0 {
		return nil
	}
	return snapshots[len(snapshots)-1].Tasks
}

// FormatTodo renders all task list snapshots from a session in human-readable
// tables, showing how the task list evolved across LLM responses.
func FormatTodo(s *Session) string {
	var sb strings.Builder

	snapshots := extractAllTaskLists(s)

	// Header
	sb.WriteString(headerSep + "\n")
	sb.WriteString(fmt.Sprintf("Session: %s\n", s.ID))
	sb.WriteString(fmt.Sprintf("Model:   %s\n", s.Model))
	sb.WriteString(fmt.Sprintf("Started: %s\n", formatTime(s.Started)))
	sb.WriteString(headerSep + "\n\n")

	if s.Task != "" {
		sb.WriteString("EPIC GOAL:\n")
		for _, line := range sessionTaskLines(s.Task) {
			sb.WriteString("  " + line + "\n")
		}
		sb.WriteString("\n")
	}

	if len(snapshots) == 0 {
		sb.WriteString("No task list found in this session.\n")
		sb.WriteString("(Task lists appear as [TASK_LIST_BEGIN]...[TASK_LIST_END] blocks\n")
		sb.WriteString(" in assistant responses — requires GoPi with task management enabled.)\n")
		return sb.String()
	}

	// Column widths.
	const (
		colStatus = 12
		colID     = 38
	)

	for snapIdx, snap := range snapshots {
		// Count statuses. Three-state vocabulary (pending/progressing/completed):
		// anything unknown still reads as progressing so a legacy two-state
		// session renders identically to before.
		var done, pend, prog int
		for _, t := range snap.Tasks {
			switch t.Status {
			case taskStatusCompleted:
				done++
			case "pending":
				pend++
			default:
				prog++
			}
		}

		counts := fmt.Sprintf("%d total, %d completed, %d progressing", len(snap.Tasks), done, prog)
		if pend > 0 {
			counts = fmt.Sprintf("%d total, %d completed, %d progressing, %d pending", len(snap.Tasks), done, prog, pend)
		}
		if snap.Iteration > 0 {
			sb.WriteString(fmt.Sprintf("TASK LIST #%d — Iteration %d  (%s)\n", snapIdx+1, snap.Iteration, counts))
		} else {
			sb.WriteString(fmt.Sprintf("TASK LIST #%d  (%s)\n", snapIdx+1, counts))
		}
		sb.WriteString(fmt.Sprintf("%-*s %-*s SUMMARY\n", colStatus, "STATUS", colID, "TASK ID"))
		sb.WriteString(strings.Repeat("─", 80) + "\n")

		for _, t := range snap.Tasks {
			statusIcon := "▶ progressing"
			switch t.Status {
			case taskStatusCompleted:
				statusIcon = "✓ completed"
			case "pending":
				statusIcon = "· pending"
			}
			sb.WriteString(fmt.Sprintf("%-*s %-*s %s\n", colStatus, statusIcon, colID, t.TaskID, t.Summary))
		}
		sb.WriteString(strings.Repeat("─", 80) + "\n")
		if snapIdx < len(snapshots)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
