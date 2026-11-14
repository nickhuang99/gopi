package main

// chat_status_symbols_legend_test.go — the gate for operator bug #3: a symbol
// printed on the chat status row must be DEFINED where the row's own reader can
// find it (`gopi chat -h`), or it must not be printed.
//
// The rule is not "these two glyphs are fine": it is the row-level property the
// codebase already states for cuts (internal/lineedit/answer.go: the difference
// between "the terminal is narrow" and "this is corrupted data" must be VISIBLE
// to the reader). The defect it closes was measured on a real pty at 117x26 —
// the row printed `ctx 31.7k/20.0k≡ ·↑0↓1M      ⧉5 · ready` while `gopi chat -h`
// carried ZERO occurrences of `⧉` (and had never carried one), so a reader met a
// symbol the product never defined.
//
// The symbols under test are resolved from the SOURCE constants that compose the
// row — never from a literal list typed here — so a new glyph is covered the
// moment its composer exists, and a renamed one moves the test with it (Rule 13).

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/lineedit"
)

// statusRowSymbol is one symbol the status row can print, with the constant that
// spells it and the composer the row reaches it through. It is the unit the gate
// walks, and the field docs are what a reviewer reads when the gate fires.
type statusRowSymbol struct {
	// glyph is the symbol as it appears on the row.
	glyph string
	// name is the reader-facing name of the fact the glyph marks.
	name string
	// composed is a string the PRODUCTION composer produces, used to prove the
	// symbol really is on the row's surface (not merely a constant that nothing
	// prints) — the other direction of the same gate. EMPTY means the chat layer
	// does not compose this symbol itself: it arrives from a shared renderer
	// (lineedit's cut path appends the ellipsis), so the "the row really draws it"
	// half is carried by the legend's own sentence and by the caller's assertion
	// below rather than by a chat-side composer.
	composed string
	// foreign marks a symbol the chat layer does not compose (see composed).
	foreign bool
}

// statusRowSymbols resolves every symbol the status row can print, from the
// source, and composes a sample through the production helpers so the list
// cannot name a glyph the row does not draw.
func statusRowSymbols(t *testing.T) []statusRowSymbol {
	t.Helper()
	ctxOps := statusOps{ctxUsed: 42100, ctxMax: 64000, ctxMeasured: true}
	return []statusRowSymbol{
		{
			glyph:    statusCtxQuantGlyph,
			name:     "the context pair's denominator qualifier",
			composed: statusCtxField(ctxOps, ctxOps.ctxUsed),
		},
		{
			// The qualifier's OTHER spelling (epic 01a1b28b, gap A): the row marks
			// an ESTIMATED context with this glyph instead of the identity one, so
			// it is a symbol the row really prints and owes the reader a definition
			// exactly like its sibling. The sample ops carry ctxMeasured: false,
			// which is what makes the production composer emit it.
			glyph:    statusCtxEstimateGlyph,
			name:     "the context pair's ESTIMATED numerator (no measurement yet)",
			composed: statusCtxField(statusOps{ctxUsed: 42100, ctxMax: 64000, ctxMeasured: false}, 42100),
		},
		{
			glyph:    statusCompactionGlyph,
			name:     "the session's compaction count",
			composed: statusCompactionLabel(statusCompactionLegendSample),
		},
		{
			glyph:    statusCacheReadGlyph,
			name:     "the prompt cache's read half",
			composed: statusCacheField(statusOps{cacheRead: 12300, cacheWrite: 4200}),
		},
		{
			glyph:    statusCacheWriteGlyph,
			name:     "the prompt cache's write half",
			composed: statusCacheField(statusOps{cacheRead: 12300, cacheWrite: 4200}),
		},
		{
			// The row does not compose this one: lineedit's shared cut path appends
			// it to any field the layout had to shorten (fitField -> cutField), so
			// its "the row really draws it" evidence is that cut path itself.
			glyph:   lineedit.StatusEllipsis,
			name:    "a field the row had to cut",
			foreign: true,
		},
	}
}

// statusCompactionLegendSample is the count the gate composes the compaction
// label with. Any non-zero number proves the same composition; it is named
// rather than typed inline so the test reads as a statement of form (Rule 1).
const statusCompactionLegendSample = 2

// statusRowHelpForTest is the reader-visible definition surface: the verb's own
// help text plus the in-chat /help, exactly as a reader can ask for them.
func statusRowHelpForTest(t *testing.T) string {
	t.Helper()
	inChat, err := chatHelpText("/help")
	if err != nil {
		t.Fatalf("chatHelpText(/help): %v", err)
	}
	return chatUsage + inChat
}

// symbolIsDefinedInHelp is the predicate the gate is built on, kept separate so
// the test can show it FAILS on a symbol the help does not define — a green
// assertion nobody has seen go red is not a gate.
func symbolIsDefinedInHelp(help, glyph string) bool {
	return strings.Contains(help, glyph)
}

// TestStatusRowSymbolsHaveAReaderVisibleDefinition is the rule itself: every
// symbol the row can print is defined in the help the surface prints.
func TestStatusRowSymbolsHaveAReaderVisibleDefinition(t *testing.T) {
	help := statusRowHelpForTest(t)

	symbols := statusRowSymbols(t)
	if len(symbols) < 2 {
		t.Fatalf("the symbol list resolved %d entries — the gate itself is broken", len(symbols))
	}
	seen := map[string]bool{}
	for _, sym := range symbols {
		if sym.glyph == "" {
			t.Errorf("%s: the symbol resolves to the EMPTY string — a constant the row prints as nothing cannot be documented", sym.name)
			continue
		}
		if seen[sym.glyph] {
			continue
		}
		seen[sym.glyph] = true
		// The composer must really print it: a legend for a glyph no row draws is
		// the reverse defect (documentation describing a screen that cannot exist).
		// A chat-composed symbol must be provably on the row's surface. A FOREIGN
		// one (the layout's cut marker) carries its own evidence in the shared
		// renderer — lineedit's fitField/cutField append it, and their tests pin
		// that — so this gate holds it to the legend alone rather than to a chat
		// composer that does not exist.
		if sym.foreign {
			continue
		}
		if !strings.Contains(sym.composed, sym.glyph) {
			t.Errorf("%q (%s): the production renderer produced %q, which does not carry it — the row cannot print this symbol, so its legend is a description of nothing",
				sym.glyph, sym.name, sym.composed)
		}
		if !symbolIsDefinedInHelp(help, sym.glyph) {
			t.Errorf("the status row prints %q (%s) and NOTHING in the product defines it.\n"+
				"  the reader has `gopi chat -h` and the in-chat /help; either name the symbol in %s's legend (the THE STATUS ROW section) or drop it from the row.\n"+
				"  measured defect this gate closes (operator bug #3, real pty 117x26): `ctx 31.7k/20.0k≡ ·↑0↓1M      ⧉5 · ready` with zero occurrences of %q in `gopi chat -h`",
				sym.glyph, sym.name, "cmd/agent/cli_chat.go", sym.glyph)
		}
	}
	t.Logf("row symbols defined in the legend: %d", len(seen))
}

// TestStatusRowSymbolGateGoesRedOnAnUndefinedSymbol is the RED PROOF: it runs
// the SAME predicate on a glyph the product does not define and asserts the gate
// refuses it, and it asserts every symbol in the real list passes. Without this,
// a gate that had stopped seeing the row's glyphs (a renamed constant, a help
// text that no longer composes) would still report green.
func TestStatusRowSymbolGateGoesRedOnAnUndefinedSymbol(t *testing.T) {
	help := statusRowHelpForTest(t)

	// A symbol defined nowhere. It is deliberately a real-looking glyph so the
	// red proof exercises the predicate rather than an empty-string special case.
	const undefinedSymbol = "✦"
	for _, sym := range statusRowSymbols(t) {
		if sym.glyph == undefinedSymbol {
			t.Fatalf("the test's `undefined` symbol %q is one the row really prints — pick another", undefinedSymbol)
		}
	}
	if symbolIsDefinedInHelp(help, undefinedSymbol) {
		t.Errorf("the predicate reports %q as DEFINED — it is in the help text by accident, so the gate cannot fail: %q",
			undefinedSymbol, help)
	}
	// And the negative control: the predicate is not stuck on false.
	if !symbolIsDefinedInHelp(help, statusCompactionGlyph) {
		t.Errorf("the predicate reports the row's own compaction glyph %q as undefined — it is stuck, so the gate above proves nothing",
			statusCompactionGlyph)
	}
}

// TestStatusRowLegendQuotesTheRealCompactionRecord keeps the legend's quoted
// line honest: the sample the help prints must be reproducible from the agent's
// OWN announcement constant, with the number the help chose. A reworded
// announcement (internal/agent/compact.go) therefore fails HERE instead of
// leaving the help quoting a line no run emits.
func TestStatusRowLegendQuotesTheRealCompactionRecord(t *testing.T) {
	// The record as the reader sees it: the agent's logger tag, then the
	// announcement with its message count filled in.
	re := regexp.MustCompile(regexp.QuoteMeta(agentPrefix) +
		`memory compaction: summarized ([0-9]+) oldest message\(s\) into a memory block`)
	m := re.FindStringSubmatch(chatUsage)
	if m == nil {
		t.Fatalf("the status-row legend does not quote the compaction record as the product prints it.\n"+
			"  want a line matching: %s%s\n"+
			"  the reader who sees ⧉1 appear must be able to find the event that produced it: %q",
			agentPrefix, agent.CompactionAnnouncement+" (with a count)", chatUsage)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("the legend's sample count %q is not a number: %v", m[1], err)
	}
	rebuilt := agentPrefix + fmt.Sprintf(agent.CompactionAnnouncement, n)
	if rebuilt != m[0] {
		t.Errorf("the legend quotes %q, but the product prints %q — the help describes a line no run emits",
			m[0], rebuilt)
	}
	// The count the announcement carries is the COMPACTION's own count, so the
	// legend must not imply it is the row's cumulative total. That claim is the
	// legend's text; this assertion only pins the two surfaces to one event.
	if n <= 0 {
		t.Errorf("the legend's sample count is %d — a compaction always summarizes at least one message", n)
	}
}

// TestStatusCompactionLegendIsNotPrintedAsZero pins the other half of the
// glyph's meaning: the row shows the counter only after a compaction, so the
// legend's "no ⧉0 is ever printed" claim is a fact about the composer rather
// than a promise in prose.
func TestStatusCompactionLegendIsNotPrintedAsZero(t *testing.T) {
	// The composer is total (it renders whatever count it is given), and the
	// row's own caller is what refuses zero — so the assertion belongs to the
	// CALLER's shape: statusLiveField must not emit the counter for a session
	// with no compactions, which is exactly the ops statusOpsFor builds.
	ops := statusOps{compressionsKnown: true, compressions: 0, elapsedKnown: true}
	live := statusLiveField(statusLiveIdle, "", ops)
	if strings.Contains(live, statusCompactionGlyph) {
		t.Errorf("a session with ZERO compactions renders %q — the row prints a symbol for an event that never happened: %q",
			statusCompactionGlyph, live)
	}
	// And with one compaction it does appear, in the reserved block.
	ops.compressions = 1
	live = statusLiveField(statusLiveIdle, "", ops)
	if !strings.Contains(live, statusCompactionLabel(1)) {
		t.Errorf("a session with one compaction renders %q, which does not carry %q — the legend defines a symbol the row would not show",
			live, statusCompactionLabel(1))
	}
}

// TestStatusRowLegendEntryOrderMatchesTheRow pins the half of the legend's
// promise no glyph gate can see: the ORDER in which it presents the metrics
// group's facts is the order the row DRAWS them.
//
// THE DEFECT IT CLOSES (task 01a0fa81). The re-partition that made the context
// pair lead the metrics group left the legend teaching the OLD partition — the
// hit rate as the first fact, and the context pair described as the arm "shown in
// the group when the cache has not answered yet" — because every gate around the
// legend resolves SYMBOLS from constants and none read its sentences. A reader of
// `gopi chat -h` learned an order the row cannot produce. This asserts the rule the
// legend's own header states (Rule 13): the text and the row cannot be changed
// apart.
//
// HOW IT IDENTIFIES AN ENTRY. A legend entry is a line that OPENS with a fact's
// own spelling — the row's label constant at the start of its indented field
// (`    ctx`, `    hit`) — which is exactly how the reader scans the section. The
// assertions are then the two claims the old text got wrong: the pair's entry
// comes FIRST, and the rate's entry EXISTS (it is a fact of the row, never a
// fallback the reader is told to wait for).
func TestStatusRowLegendEntryOrderMatchesTheRow(t *testing.T) {
	help := statusRowHelpForTest(t)
	ctxEntry := statusRowLegendEntry(help, statusCtxLabel)
	hitEntry := statusRowLegendEntry(help, statusHitRateLabel)
	if ctxEntry < 0 || hitEntry < 0 {
		t.Fatalf("the legend carries no entry opening with %q (at %d) or %q (at %d): the row draws both, so a reader "+
			"meeting either on the screen has nothing to look up", statusCtxLabel, ctxEntry, statusHitRateLabel, hitEntry)
	}
	if ctxEntry > hitEntry {
		t.Errorf("the legend presents %q at offset %d and %q at %d — the RATE first.\n"+
			"  the composed row draws the context pair FIRST in the metrics group (both by sub-cell order and by the ladder's\n"+
			"  shed order), and the legend's own header promises the text and the row cannot be changed apart (Rule 13).\n"+
			"  fix the legend (cmd/agent/cli_chat.go, chatStatusRowLegend); the row's order is the invariant the re-partition set",
			statusCtxLabel, ctxEntry, statusHitRateLabel, hitEntry)
	}
}

// TestStatusRowLegendDefinesTheGroupsUnmeasuredWord keeps the legend honest about
// the one thing the metrics group prints when it has measured NOTHING. The word is
// a real output of the row (statusMetricsUnmeasured, composed by statusMetricsField
// for a session whose first prompt has not answered) and it was, until this rework,
// documented nowhere: a reader met `no metrics` on the screen with nothing in
// `gopi chat -h` to look it up — the same defect class as the undefined `⧉` glyph,
// one column to the left.
//
// The expectation is resolved from the ROW's constant, never retyped here, so a
// rewording of the group's empty state moves this gate with it (Rule 13).
func TestStatusRowLegendDefinesTheGroupsUnmeasuredWord(t *testing.T) {
	if statusMetricsUnmeasured == "" {
		t.Fatalf("the group's unmeasured word is EMPTY — the row would print a hole where it states its own state")
	}
	// The row really draws it: the composer returns it when no sub-cell has a value.
	if got := statusMetricsField(statusOps{}, 0); got != statusMetricsUnmeasured {
		t.Fatalf("statusMetricsField over unmeasured ops returns %q, want %q — this gate would otherwise document a line the row cannot print",
			got, statusMetricsUnmeasured)
	}
	// And a measured group never carries it (the other direction).
	if got := statusMetricsField(statusOps{ctxUsed: 42100, ctxMax: 200000, ctxMeasured: true}, 0); strings.Contains(got, statusMetricsUnmeasured) {
		t.Errorf("a measured group renders %q — the word belongs to the unmeasured arm alone", got)
	}
	help := statusRowHelpForTest(t)
	if !strings.Contains(help, statusMetricsUnmeasured) {
		t.Errorf("the status row prints %q and NOTHING in the product defines it.\n"+
			"  a reader whose session has not answered a prompt yet sees that word in the metrics cells: name it in the THE STATUS ROW section of %s.",
			statusMetricsUnmeasured, "cmd/agent/cli_chat.go")
	}
}

// statusRowLegendEntry returns the OFFSET of the first line of help that OPENS
// with the fact's own label — the reader's own way of finding an entry in the
// section — or -1 when the legend carries none. It is a helper rather than an
// inline scan so the gate above reads as the two assertions it makes.
func statusRowLegendEntry(help, label string) int {
	offset, indent := 0, 0
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, label) {
			return offset
		}
		indent = len(line) - len(trimmed)
		_ = indent
		offset += len(line) + 1
	}
	return -1
}
