package testsuite

// chat_pty_ctx_qualifier_test.go — bug F's RULE-31 evidence, as a test: the
// status row's context read-out, painted by the SHIPPED binary on a real pty, with
// a MEASURED context (the numbers arrive through the transport path from a
// scripted provider, never from a test fixture inside package main).
//
// WHAT IT ASSERTS, and why each leg is a rendered-truth claim rather than a
// restatement of the code:
//
//	1. the row on SCREEN carries the denominator's qualifier IMMEDIATELY after the
//	   denominator ("92.1k/200.0k≡"). The marker is one string, so it can only
//	   match if the qualifier is in the cell beside the number — a sibling
//	   read-out carrying the glyph somewhere else would not satisfy it;
//	2. the retired heavy marker does not appear (no "92.1k/200.0k!");
//	3. the pair is not CUT: the whole denominator is on the screen, in the window
//	   the provider was scripted to report against the deployed 200.0k budget;
//	4. THE RATE IS STILL THERE, on the SETTLED row — the pair's presence is not
//	   evidence about the rate, and the defect this case was opened for was a row
//	   that kept ONE of the two; and
//	5. the PAIR LEADS the rate, with the cache transport in the cells between them
//	   — the re-partition's own invariant, measured in byte order.
//
// The row is painted by the reserved layout, so the cells are the production
// ones: the assertion is on the byte stream the terminal received, which is what
// the operator reads.
//
// WHERE THE EXPECTED CELLS COME FROM, and why they are no longer spelled here. This
// case used to wait for the literal "92.1k/200.0k≡" and it went red without the row
// changing what it MEANS: commit 81c29f95 (devdoc/status-row-one-count-rule.md)
// retired the k-only formatter that produced it ("%.1fk") so that both count families
// on the row climb ONE ladder, and a two-significant-digit ladder renders the same
// occupancy as "92k/200k≡". So the expected cells are DERIVED from that ladder, read
// out of its source at run time (ptyStatusLadder); the numbers under test stay the
// scripted ones, and the retyping of the product's spelling — a second copy of a
// product rule, with nothing to keep the copies in step — is gone.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/uiclient"
)

// The scripted provider's usage, and why the numbers are spelled as a SUM: the
// row's context numerator is the measured prompt total, which this stack computes
// as input + cache_creation + cache_read (internal/agent/loop.go's
// SetPromptTokens call site) — so a stub that wants the row to read a given
// occupancy must split it across the three counts rather than put it in
// input_tokens alone. The denominator is the deployed window: the row prints
// whatever the agent's own ContextWindow field holds, which is
// agent.DefaultContextWindow here (read at the pair's derivation below, never
// typed — Rule 14; MockEnv strips GOPI_CONTEXT_WINDOW, so a run cannot have a
// window other than the default).
const (
	ptyCtxUsedTokens     = 92100
	ptyCtxCacheReadTok   = 12300
	ptyCtxCacheCreateTok = 4200
	// ptyCtxInputTok is the remainder of the occupancy after the two cache counts:
	// 92100 − 12300 − 4200.
	ptyCtxInputTok = ptyCtxUsedTokens - ptyCtxCacheReadTok - ptyCtxCacheCreateTok
)

// The cells the row must paint are DERIVED from the product's own count ladder
// (ptyStatusCountRendered below), never spelled here — and the pin that used to be
// spelled here is exactly why this case went red. It waited for the literal
// "92.1k/200.0k≡": the output of the retired k-only formatter ("%.1fk", deleted in
// commit 81c29f95 — devdoc/status-row-one-count-rule.md), while the deployed row
// paints one ladder for every count on it, which renders the same occupancy as
// "92k/200k≡". The row never changed its MEANING and bug F never came back: a
// two-significant-digit ladder prints 92100 as "92k" and rounds 200000 up a rung
// to "1M", and the qualifier still rides the denominator. So the fix is to read the
// spelling from the product rather than re-type it: a second spelling of a product
// RULE is a test that has to be edited whenever the product improves its formatting
// — it asserts the retired formatter, not the row.
const (
	// ptyCtxQuantGlyph is the cell the row paints beside the denominator (the
	// qualifier under test). Spelled here because a `package main` constant is not
	// visible to a suite that drives the shipped binary, and the assertion is about
	// the PAINTED cell.
	ptyCtxQuantGlyph = "≡"
	// ptyCtxLabelRendered is the context field's own label, and ptyCtxPairSep the
	// separator between the pair's two sides. Both are what the row PAINTS: the
	// label is composed only where it fits the sub-cell together with the pair and
	// the qualifier (statusCtxField), so asserting it is asserting that the field
	// was not squeezed into its yield form.
	ptyCtxLabelRendered = "ctx"
	ptyCtxPairSep       = "/"
	// ptyCtxRetiredMarker is the glyph the row no longer paints (the retired
	// heavy-context mark): its presence would mean the defect is back.
	ptyCtxRetiredMarker = "!"
)

// ptyStatusSourceFiles are the status row's count sources, addressed from THIS file's
// package directory. The suite drives the SHIPPED binary and reads the row off the
// screen; what it takes from the source is the SPELLING RULE — the ladder the row
// renders every count through. Two files, because the ladder and the scale its rungs
// are built from live in the two halves of one rule: the rung table and the bounded
// renderer in the first, the scale constant and the digit policy in the second.
var ptyStatusSourceFiles = []string{
	"../cmd/agent/chat_status_cache_bound.go",
	"../cmd/agent/chat_stream.go",
}

// The source's own spellings, used to read the ladder back. They are the marks the
// declaration is parsed by; a rename moves the test with it (it fails loudly, see
// ptyStatusLadder) rather than silently deriving nothing.
const (
	ptyStatusLadderHead = "var statusCountUnits = []boundedCountUnit{"
	ptyStatusUnitMark   = "{unit: \""
	ptyStatusScaleMark  = "scale: "
	ptyStatusScaleName  = "statusCountCompactKilo"
	// ptyStatusLadderRungs is how many NAMED rungs follow the whole-number one (k, M,
	// G). The parser checks the count so a rung added to the product is met by a
	// failure here rather than by an expectation that quietly misses it.
	ptyStatusLadderRungs = 3
	// ptyStatusWholeDigitsMax mirrors the CONTEXT PAIR's digit policy
	// (statusCountCtxDigitsWidest, 3): a context side may state three whole digits,
	// which is what makes 200000 print as "200k" — the DEPLOYED budget, stated as the
	// budget it is — instead of climbing a rung to "1M" and overstating the window five
	// fold. The cache pair keeps its own two-digit allowance (the same cells carry an
	// arrow each, so three digits do not fit there); this file is about the context
	// pair, so three is the policy it reads.
	ptyStatusWholeDigitsMax = 3
	// ptyStatusSmallestMantissa mirrors the ladder's floor for a whole part that
	// rounds away (statusCountSmallestMantissa, 1): a rung never prints a bare unit.
	ptyStatusSmallestMantissa = 1
)

// ptyStatusCountUnit is one rung of the row's ladder as the source states it: the
// unit it prints and the scale it divides the count by.
type ptyStatusCountUnit struct {
	unit string
	div  int
}

// ptyStatusScaleBase reads the scale the ladder's named rungs are built from and
// verifies it is a power of ten — the row's counts climb a decimal ladder, and a base
// that was not one would make every expected cell wrong, so it is checked rather than
// assumed.
func ptyStatusScaleBase(t *testing.T, texts []string) int {
	t.Helper()
	n, found := 0, false
	for _, text := range texts {
		m := ptyStatusScaleDecl.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		// The match is the DECLARATION — anchored at a line's start and closed by the
		// line's end — never prose about it: the constant's own doc comment quotes the
		// same text ("statusCountCompactKilo = 10 × 10 × 10), so its digit count …"),
		// and a parser that took the first occurrence would read the sentence.
		parsed, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("%s's declaration %q is not a literal this test can read: %v",
				ptyStatusScaleName, m[0], err)
		}
		n, found = parsed, true
		break
	}
	if !found {
		t.Fatalf("%v no longer declare %s on a line of its own: the ladder's base is what the row's expected cells are derived from",
			ptyStatusSourceFiles, ptyStatusScaleName)
	}
	for v := n; v > 1; v /= 10 {
		if v%10 != 0 {
			t.Fatalf("%s = %d is not a power of ten: the row's counts climb a decimal ladder, and this test derives its cells from it",
				ptyStatusScaleName, n)
		}
	}
	return n
}

// ptyStatusScaleDecl matches the scale constant's DECLARATION — anchored at the start of
// a line and ended by that line — so the constant's own prose (which quotes the same
// text mid-sentence) can never be mistaken for it.
var ptyStatusScaleDecl = regexp.MustCompile(`(?m)^[ \t]*(?:const[ \t]+)?` + ptyStatusScaleName + `[ \t]*=[ \t]*([0-9]+)[ \t]*$`)

// ptyStatusLadder reads the row's count ladder out of the product source. It fails
// loudly rather than guessing or returning nothing: a ladder it could not read would
// make the expected cells a fiction, and an EMPTY ladder would leave the test waiting
// for the empty string — which every screen contains, i.e. a green that asserts
// nothing.
func ptyStatusLadder(t *testing.T) []ptyStatusCountUnit {
	t.Helper()
	var texts []string
	for _, name := range ptyStatusSourceFiles {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read the status row's count ladder (%s): %v — the row's expected cells cannot be derived without it",
				name, err)
		}
		texts = append(texts, string(src))
	}
	base := ptyStatusScaleBase(t, texts)
	head := strings.Index(texts[0], ptyStatusLadderHead)
	if head < 0 {
		t.Fatalf("%s no longer declares %q — the ladder moved, and this test's expected cells would be stale",
			ptyStatusSourceFiles[0], ptyStatusLadderHead)
	}
	decl := texts[0][head:]
	end := strings.Index(decl, "\n}")
	if end < 0 {
		t.Fatalf("%s's %q declaration is not closed on a line of its own any more", ptyStatusSourceFiles[0], ptyStatusLadderHead)
	}
	var units []ptyStatusCountUnit
	for _, rung := range strings.Split(decl[:end], ptyStatusUnitMark)[1:] {
		unit, rest, ok := strings.Cut(rung, "\"")
		if !ok {
			t.Fatalf("the ladder's rung %q carries no closing quote", rung)
		}
		_, scaleExpr, ok := strings.Cut(rest, ptyStatusScaleMark)
		if !ok {
			t.Fatalf("the ladder's rung %q states no %q", rung, ptyStatusScaleMark)
		}
		scaleExpr, _, _ = strings.Cut(scaleExpr, "}")
		scaleExpr = strings.TrimSpace(scaleExpr)
		if n, err := strconv.Atoi(scaleExpr); err == nil {
			units = append(units, ptyStatusCountUnit{unit: unit, div: n})
			continue
		}
		// A named scale is the base the rungs are built from, multiplied by itself once
		// per occurrence — the same expression the ladder writes.
		mentions := countNameMentions(scaleExpr, ptyStatusScaleName)
		if mentions == 0 {
			t.Fatalf("the ladder's rung %q states a scale (%q) this test cannot read", rung, scaleExpr)
		}
		div := 1
		for i := 0; i < mentions; i++ {
			div *= base
		}
		units = append(units, ptyStatusCountUnit{unit: unit, div: div})
	}
	if len(units) != ptyStatusLadderRungs+1 {
		t.Fatalf("the ladder reads as %d rung(s) (%v), want a whole-number rung plus %d named ones — a rung the product added must be met by this test, not missed",
			len(units), units, ptyStatusLadderRungs)
	}
	if units[0].unit != "" || units[0].div != 1 {
		t.Fatalf("the ladder's lowest rung is %+v, want the whole-number rung (no unit, scale 1)", units[0])
	}
	for i := 1; i < len(units); i++ {
		if units[i].div <= units[i-1].div {
			t.Fatalf("the ladder's rungs do not climb: %+v", units)
		}
	}
	return units
}

// countNameMentions counts how often name occurs in expr as a whole identifier (the
// parser's own guard: a mention is a scale term, and a name that occurred inside
// another identifier would make the power wrong).
func countNameMentions(expr, name string) int {
	n := 0
	for _, term := range strings.FieldsFunc(expr, func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) {
		if term == name {
			n++
		}
	}
	return n
}

// ptyStatusCountRendered renders a count the way the ROW's ladder renders it: the rung
// its magnitude selects, rounded ONCE to the whole part that rung may state (at most
// ptyStatusWholeDigitsMax digits — which is what turns 200000 into "1M"), climbing a
// rung when that rounding runs the whole part past the digits the rung may print, and
// stating the smallest mantissa when the rounding runs the whole part away. The
// whole-number rung is not a unit: below the first named scale the value is printed as
// it is.
func ptyStatusCountRendered(t *testing.T, n int) string {
	t.Helper()
	return ptyStatusCountRenderedUnits(n, ptyStatusLadder(t))
}

// ptyStatusCountRenderedUnits is ptyStatusCountRendered against a ladder the caller
// already read — the form the guard below uses, so one test reads the source once.
func ptyStatusCountRenderedUnits(n int, units []ptyStatusCountUnit) string {
	rung := 0
	for i := 1; i < len(units); i++ {
		if n >= units[i].div {
			rung = i
		}
	}
	if rung == 0 {
		return strconv.Itoa(n)
	}
	whole := 0
	for {
		whole = (n + units[rung].div/2) / units[rung].div
		if whole < ptyStatusSmallestMantissa {
			whole = ptyStatusSmallestMantissa
		}
		if len(strconv.Itoa(whole)) <= ptyStatusWholeDigitsMax || rung == len(units)-1 {
			break
		}
		rung++
	}
	return strconv.Itoa(whole) + units[rung].unit
}

// ptyStatusSideRefusal returns the reason `side` is NOT a spelling this ladder renders
// for the count n, or "" when it is one. It is the predicate the guard below states over
// RENDERED SIDES rather than over counts, so the retired formatter's own output can be
// handed to the same oracle the live spelling is judged by (a red proof that
// re-implemented the check would only prove the copy agrees with itself).
func ptyStatusSideRefusal(side string, n int, units []ptyStatusCountUnit) string {
	want := ptyStatusCountRenderedUnits(n, units)
	if side == want {
		return ""
	}
	return fmt.Sprintf("%d is rendered %q by the row's ladder, not %q — %q is a spelling from a second unit system",
		n, want, side, side)
}

// ptyCtxPairRendered is the pair the row must paint: BOTH sides through the product's
// own ladder — the measured occupancy the stub was scripted with, over the agent's own
// deployed window (its ONE definition, agent.DefaultContextWindow, which is what the
// row's ctxMax reads off Agent.ContextWindow).
func ptyCtxPairRendered(t *testing.T) string {
	t.Helper()
	return ptyStatusCountRendered(t, ptyCtxUsedTokens) +
		ptyCtxPairSep +
		ptyStatusCountRendered(t, agent.DefaultContextWindow)
}

// ptyCtxDumpGridNote reports the CELL grid the surface's own dump last installed, for a
// failure message. It never gates: the dump's sink installs a state only when the screen
// CHANGES and is throttled (cmd/agent/chat_dump.go documents the same trap), so its last
// frame can be one change behind the terminal — useful to LOOK at, unfit to WAIT on. It
// returns a printable note either way, so a red run shows both carriers.
func ptyCtxDumpGridNote(t *testing.T, dumpPath string) string {
	t.Helper()
	cand, err := uiclient.ReadDump(dumpPath)
	if err != nil {
		return fmt.Sprintf("dump (%s): unreadable: %v\n", dumpPath, err)
	}
	if refusal := uiclient.CompareGridUnusable(cand); refusal != "" {
		return fmt.Sprintf("dump (%s): not a usable screen: %s\n", dumpPath, refusal)
	}
	rows := cand.Dump.Grid
	if len(rows) == 0 {
		rows = cand.Dump.Lines
	}
	from := 0
	if len(rows) > ptyCtxGridTailRows {
		from = len(rows) - ptyCtxGridTailRows
	}
	var b strings.Builder
	fmt.Fprintf(&b, "dump (%s): frames=%d dumps=%d, last rows:\n", dumpPath, cand.Frames, cand.Dumps)
	for i := from; i < len(rows); i++ {
		fmt.Fprintf(&b, "  row[%02d]=%q\n", i, strings.TrimRight(rows[i], " "))
	}
	return b.String()
}

// ptyCtxGridTailRows is how many rows of the dumped grid a failure message quotes.
const ptyCtxGridTailRows = 4

// ptyCtxHitRateTier / ptyCtxHitRatePctTail and the fraction count are the SHAPE of
// the rate's reading, spelled here for the same reason ptyCtxLabelRendered is: a
// `package main` constant is not visible to a suite that drives the shipped binary,
// and the assertion is about the PAINTED cells. The MAGNITUDE is not spelled — it is
// computed from the counters this file scripts, so the expected reading and the
// stub's usage are one number (a second copy of the product's arithmetic would go
// stale silently, which is the defect this case was repaired for).
//
// THE TIER IS NOW A ONE-CELL MARKER, NOT THE WORD "hit" (the two-reading epic): the
// row draws TWO readings — this session's cumulative rate and this turn's — and it
// is their markers (Σ / Δ) that tell them apart and state each one's denominator. The
// word and its gap gave back exactly the four cells the Δ reading's marker and three
// digits take, so the pinned geometry is untouched.
const (
	ptyCtxHitRateSessionTier = "Σ"
	ptyCtxHitRateTurnTier    = "Δ"
	ptyCtxHitRatePctTail     = "%"
	ptyCtxHitRateDecimals    = 1
	// ptyCtxTransportGlyph is the cache transport's READ side — the cell that says the
	// two counters between the pair and the rate are the ones the rate is computed from.
	ptyCtxTransportGlyph = "↑"
)

// ptyCtxRateReading is the SESSION-cumulative hit-rate reading the row must paint for
// the usage this file scripts, derived from those counters rather than typed: the rate
// is served/(served+written) as a percentage with ptyCtxHitRateDecimals fraction
// digits, introduced by the cumulative tier's marker.
//
// The WRITE side is the sum of the two miss-billed counts — the provider's own
// convention, which the row inherits: every input token the cache did NOT serve was
// written or re-read at the miss rate (deepseek's ClaudeUsage accounting adds
// cache_creation AND input to the miss total).
func ptyCtxRateReading() string {
	return ptyCtxHitRateSessionTier + ptyCtxHitRateMagnitude()
}

// ptyCtxRateTurnReading is THIS TURN's reading as the row paints it, in the same
// terms.The turn's own denominator is the DIFFERENCE of the counters since the turn's
// request was issued — and on this surface there is exactly ONE turn, whose whole
// prompt-cache traffic is the scripted usage, so the turn's counters ARE the session's
// and the two readings must land on the SAME magnitude. That is what makes the pty
// case able to assert the turn reading at all: it is a value this file can compute
// from its own script rather than a number it would have to be told.
//
// (The Diverge case — the two readings on different numbers — is the unit-level
// assertion in cmd/agent: a second pty turn would need a second scripted request, and
// the property under test there is the ARITHMETIC, not the repaint.)
func ptyCtxRateTurnReading() string {
	return ptyCtxHitRateTurnTier + ptyCtxHitRateMagnitudeWhole()
}

// ptyCtxHitRateMagnitude is the session rate's painted number, from the scripted
// counters; ptyCtxHitRateMagnitudeWhole is the same ratio at the turn reading's own
// resolution (whole percent — the turn is one fresh sample, so a tenth of a percent
// of it is noise).
func ptyCtxHitRateMagnitude() string {
	return strconv.FormatFloat(ptyCtxHitRateRatio()*100, 'f', ptyCtxHitRateDecimals, 64) + ptyCtxHitRatePctTail
}

func ptyCtxHitRateMagnitudeWhole() string {
	return strconv.FormatFloat(ptyCtxHitRateRatio()*100, 'f', ptyCtxHitRateWholeDecimals, 64) + ptyCtxHitRatePctTail
}

func ptyCtxHitRateRatio() float64 {
	return ptyCtxSessionRatio([]ptyCtxTurnUsage{ptyCtxDefaultUsage})
}

// ptyCtxHitRateWholeDecimals is the turn reading's resolution — whole percent.
const ptyCtxHitRateWholeDecimals = 0

// ptyCtxDivergentTurn is the SECOND turn's transport usage in the divergence case
// below. It is chosen so that the two readings land on visibly DIFFERENT rates: the
// session ratio becomes an average of two dissimilar turns, while this turn's own
// ratio is the fresh sample alone.
//
// THE NUMBERS ARE CHOSEN, NOT FOUND, and the choice is stated so a reader can check
// it: turn 1 serves little of its prompt from the cache (ptyCtxCacheReadTok of
// ptyCtxUsedTokens), while this turn is a long cached conversation (80000 of 82000
// served). The session average (53.0%) therefore sits BETWEEN the two turns while
// this turn reads 98% — a divergence no rounding can hide, and one whose two numbers
// are also distinguishable from EACH OTHER by their digit count (five cells vs
// three), so a row that painted the same reading twice cannot pass by accident.
var ptyCtxDivergentTurn = ptyCtxTurnUsage{input: 1000, create: 1000, read: 80000}

// ptyCtxSessionRatio is the SESSION-cumulative rate for a scripted list of turns: the
// sum of every turn's served tokens over the sum of every turn's cache traffic — the
// row's own formula applied to the counters the row accumulates.
func ptyCtxSessionRatio(turns []ptyCtxTurnUsage) float64 {
	read, written := 0, 0
	for _, u := range turns {
		read += u.read
		written += u.create + u.input
	}
	return float64(read) / float64(read+written)
}

// ptyCtxTurnRatio is ONE turn's own rate — the difference form the row composes the
// delta reading from, which for a single scripted turn is that turn's counters.
func ptyCtxTurnRatio(u ptyCtxTurnUsage) float64 {
	return float64(u.read) / float64(u.read+u.create+u.input)
}

// ptyCtxRateReadingOf / ptyCtxRateTurnReadingOf render those two ratios in the row's
// own spellings (marker + percentage), at each reading's own resolution.
func ptyCtxRateReadingOf(turns []ptyCtxTurnUsage) string {
	return ptyCtxHitRateSessionTier +
		strconv.FormatFloat(ptyCtxSessionRatio(turns)*100, 'f', ptyCtxHitRateDecimals, 64) + ptyCtxHitRatePctTail
}

func ptyCtxRateTurnReadingOf(u ptyCtxTurnUsage) string {
	return ptyCtxHitRateTurnTier +
		strconv.FormatFloat(ptyCtxTurnRatio(u)*100, 'f', ptyCtxHitRateWholeDecimals, 64) + ptyCtxHitRatePctTail
}

// The settle read's two bounds: how long the stream must be QUIET before it is taken as
// the settled row, and the ceiling that keeps a chatty child from hanging the case.
const (
	ptyCtxSettleQuiet = 700 * time.Millisecond
	ptyCtxSettleLimit = 10 * time.Second
)

// ptyReadSettled returns seed plus everything the child writes until the stream has been
// silent for quiet (or limit elapses). It exists because waitFor returns the instant its
// marker arrives — MID-repaint — so the bytes it hands back are a PREFIX of the row the
// terminal ends up showing: measured here, they end exactly at "ctx 92k/200k≡" while the
// rate is written one repaint later. The quiet window is what makes the reading below a
// screen fact rather than a statement about repaint chunking.
// TestChatPTY_TwoReadingsCarryTheirOwnColourInTheDump is the epic's `attr_runs`
// acceptance leg: the dump the surface itself wrote must carry the hit rate's TWO
// readings as TWO COLOURED RUNS, and the run's expected value must be resolved from
// the palette TABLE rather than spelled here (Rule 14) — a table whose tier entry moved
// moves this expectation with it instead of leaving the guard asserting yesterday's
// colour.
//
// WHY THE DUMP AND NOT THE BYTE STREAM. TestChatPTY_TwoReadingsDivergeOnScreen asserts
// the readings are on the SETTLED STREAM inside their own SGR runs, which is the right
// carrier for "did the row paint them". This case asserts the OTHER artifact: what a
// reader of `gopi ui_state --target chat` sees — the interpreted CELL GRID with its
// per-row attr runs, where a colour is a run's style STRING and not a sequence's
// position. The two legs fail differently (a sequence that never reached the grid, a
// run the reader cannot attribute) and neither substitutes for the other.
//
// THE TABLE IS PINNED, so the palette is a constant of the measurement rather than of
// the operator's environment: GOPI_PALETTE=palette256 + TERM=xterm-256color selects
// Palette256, whose StatusFair entry is the cube's amber (the tier the scripted rate
// lands in), and the expectation is read back through sgr.Style.Fields() — the ONE
// definition of how a style renders as a string.
func TestChatPTY_TwoReadingsCarryTheirOwnColourInTheDump(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := startUsageLLM(t)
	dumpPath := filepath.Join(t.TempDir(), uiclient.ChatDumpFileName)
	env := append(testutil.MockEnv(t, home, llm.URL),
		lineedit.EnvTerm+"="+ptyCtxDumpTerm,
		lineedit.EnvPalette+"="+lineedit.PaletteNamePalette256,
	)
	chat := startPTYChat(t, []string{"chat", "--dump-screen=" + dumpPath}, env, work)
	t.Cleanup(func() {
		if chat.cmd.Process != nil {
			_ = chat.cmd.Process.Kill() //stylegate:ignore best-effort teardown of a failed run
		}
		_ = chat.master.Close() //stylegate:ignore the master is the test's own descriptor
	})
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	chat.resize(t, ptyResizeCols, ptyResizeRows)
	time.Sleep(ptyResizeSettled) //stylegate:allow-sleep lets the resize redraw finish before typing (the pin the resize test carries)

	chat.writeGapped(t, "probe the rates' colour")
	chat.write(t, "\r")
	session := ptyCtxRateReadingOf([]ptyCtxTurnUsage{ptyCtxDefaultUsage})
	turn := ptyCtxRateTurnReadingOf(ptyCtxDefaultUsage)
	// The wait is on the CUMULATIVE reading alone: the raw stream separates the two
	// readings with SGR runs (each reading carries its own tier), so their concatenation
	// is not a substring of the stream even though the PAINTED row carries them adjacent.
	// The adjacency itself is asserted by the sibling case, on the stripped stream; here
	// the subject is the ARTIFACT's attr runs, and the dump poll below is what waits for
	// both readings to be installed in the grid.
	chat.waitFor(t, session, ptyPromptTimeout)

	// The run is read off the surface's OWN artifact, through the reader `gopi
	// ui_state` uses — the interpreted grid and its per-row attribute runs.
	cand := ptyCtxAwaitDump(t, dumpPath, []string{session, turn})
	if refusal := uiclient.CompareGridUnusable(cand); refusal != "" {
		t.Fatalf("the dump cannot be read as a screen, so no colour verdict can be measured from it: %s", refusal)
	}
	if !cand.Dump.AttrsRecorded || len(cand.Dump.AttrRuns) == 0 {
		t.Fatalf("the artifact carries no per-cell attributes (%s), so the colour of the readings cannot be measured from it", dumpPath)
	}
	row := ptyCtxRowWith(cand.Dump.Grid, []string{session, turn})
	if row < 0 {
		t.Fatalf("the grid carries no single row holding both readings %q and %q\n%s",
			session, turn, ptyCtxGridTail(cand.Dump.Grid, 4))
	}
	gridRow := cand.Dump.Grid[row]
	// The tier the two readings land in is the palette's OWN judgement, resolved from
	// the same floors the product grades with (Rule 14) — never a colour typed here.
	pct, ok := ptyCtxReadingPercent(session)
	if !ok {
		t.Fatalf("the cumulative reading %q is not a percentage this case can grade", session)
	}
	wantStyle := ptyCtxTierStyleName(t, pct)
	if wantStyle == "" {
		t.Fatalf("the tier resolved for %q is the uncoloured default, so this case could not tell a coloured run from an uncoloured one", session)
	}

	// Each reading must sit inside a run whose style IS its tier's — the colour the
	// artifact attributes to it. The two readings are NOT required to occupy two runs:
	// when both land in the SAME tier (the ordinary single-turn case below) the painter
	// emits one continuous run over both, and a case demanding two would be asserting a
	// boundary that the tier model does not draw. What IS asserted is that each reading's
	// own cells carry the tier's colour rather than the transport's, and that the two
	// readings are at DIFFERENT columns.
	var cols [2]int
	sharedRun := true
	var firstRun lineedit.DumpAttrRun
	for i, reading := range []string{session, turn} {
		col := ptyCtxRuneCol(gridRow, reading)
		if col < 0 {
			t.Fatalf("the reading %q is not on the grid row %d the poll selected: %q", reading, row, gridRow)
		}
		cols[i] = col
		run, found := ptyCtxRunCovering(cand, row, col)
		if !found {
			t.Fatalf("no attr run covers cell %d of row %d (the reading %q): the grid records no colour there\n%s",
				col, row, reading, ptyCtxDumpRunsNote(cand, row))
		}
		if run.Style != wantStyle {
			t.Fatalf("the reading %q (row %d, cells %d..%d) carries the style %q, want the tier's own %q resolved from the palette table and the source floors\n%s",
				reading, row, run.Col, run.Col+run.Cells-1, run.Style, wantStyle, ptyCtxDumpRunsNote(cand, row))
		}
		if run.Cells < len([]rune(reading)) {
			t.Fatalf("the run covering the reading %q is %d cells but the reading is %d: the colour does not cover what was measured\n%s",
				reading, run.Cells, len([]rune(reading)), ptyCtxDumpRunsNote(cand, row))
		}
		if i == 0 {
			firstRun = run
		} else if run.Col != firstRun.Col || run.Cells != firstRun.Cells || run.Style != firstRun.Style {
			sharedRun = false
		}
	}
	if cols[0] == cols[1] {
		t.Fatalf("the two readings resolve to the same cell column %d on row %d: the row does not draw them as two", cols[0], row)
	}
	// WHICH of the two shapes was measured is part of the result, not a detail: the tier
	// model merges the readings only while they share a tier, and a reader of this test's
	// evidence needs to know which one this run witnessed (Rule 5's Evidence section).
	shape := "one run over both readings (they share a tier here)"
	if !sharedRun {
		shape = "a run per reading (their tiers differ here)"
	}
	t.Logf("pty dump colour: row %d carries %q at col %d then %q at col %d; tier %q ⇒ %s (palette %s, TERM %s)",
		row, session, cols[0], turn, cols[1], wantStyle, shape, lineedit.PaletteNamePalette256, ptyCtxDumpTerm)
	t.Logf("pty dump runs on the row:\n%s", ptyCtxDumpRunsNote(cand, row))

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 — the session ends at the deliberate /quit", code)
	}
}

// ptyCtxDumpTerm is the terminal capability the colour leg declares. It is the pair the
// 256 table needs to be SELECTED rather than downgraded (lineedit's palette resolver
// reads TERM), and it is stated so the run cannot silently measure another table.
const ptyCtxDumpTerm = "xterm-256color"

// ptyStatusTierFloors reads the hit rate's two tier floors out of the SOURCE the
// product grades with, so this file's colour expectation follows a floor that moved
// instead of pinning yesterday's judgement. The marks below are the declarations'
// own spellings, and a rename fails here loudly rather than deriving nothing.
func ptyStatusTierFloors(t *testing.T) (good, fair int) {
	t.Helper()
	src, err := os.ReadFile(ptyStatusTierSourceFile)
	if err != nil {
		t.Fatalf("read the hit rate's tier floors (%s): %v — the expected colour cannot be derived without them",
			ptyStatusTierSourceFile, err)
	}
	good = ptyStatusTierFloor(t, string(src), ptyStatusGoodFloorMark)
	fair = ptyStatusTierFloor(t, string(src), ptyStatusFairFloorMark)
	if good <= fair {
		t.Fatalf("the tier floors read as good=%d fair=%d, want the good floor above the fair one", good, fair)
	}
	return good, fair
}

// ptyStatusTierFloor reads one floor's value from the source text.
func ptyStatusTierFloor(t *testing.T, src, mark string) int {
	t.Helper()
	at := strings.Index(src, mark)
	if at < 0 {
		t.Fatalf("%s no longer declares %q — the tiers moved, and this test's expected colour would be stale",
			ptyStatusTierSourceFile, mark)
	}
	rest := src[at+len(mark):]
	rest, _, _ = strings.Cut(rest, "\n")
	n, err := strconv.Atoi(strings.TrimSpace(rest))
	if err != nil {
		t.Fatalf("the floor declared by %q does not read as a whole percent (%q): %v", mark, rest, err)
	}
	return n
}

// ptyStatusTierSourceFile is the file the two floors live in, and the two marks are
// the declarations they are read by.
const (
	ptyStatusTierSourceFile = "../cmd/agent/chat_status_hitrate_emphasis.go"
	ptyStatusGoodFloorMark  = "statusHitRateGoodFloorPct = "
	ptyStatusFairFloorMark  = "statusHitRateFairFloorPct = "
)

// ptyCtxReadingPercent reads the percentage out of a rendered reading ("Σ53.0%" → 53.0).
func ptyCtxReadingPercent(reading string) (float64, bool) {
	s := strings.TrimSuffix(reading, ptyCtxHitRatePctTail)
	s = strings.TrimPrefix(s, ptyCtxHitRateSessionTier)
	s = strings.TrimPrefix(s, ptyCtxHitRateTurnTier)
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

// ptyCtxTierStyleName resolves the style NAME a percentage wears in the pinned table,
// through the product's own floors (statusHitRateGoodFloorPct / FairFloorPct, read from
// the source by ptyStatusTierFloors) and the palette's own StatusGood/Fair/Bad entries.
//
// The NAME is not read off a constant: it is what the dump's own reader produces for
// that style, obtained by running the style's SGR sequence through the SAME
// lineedit.Screen the dump is a snapshot of and asking it for the cell's style. A
// palette entry that moved, a floor that moved, or a change in how a style renders as
// a string therefore moves this expectation with it (Rule 14) — nothing about the
// colour is typed here.
func ptyCtxTierStyleName(t *testing.T, pct float64) string {
	t.Helper()
	good, fair := ptyStatusTierFloors(t)
	switch {
	case pct >= float64(good):
		return ptyCtxStyleName(lineedit.Palette256.StatusGood)
	case pct >= float64(fair):
		return ptyCtxStyleName(lineedit.Palette256.StatusFair)
	default:
		return ptyCtxStyleName(lineedit.Palette256.StatusBad)
	}
}

// ptyCtxStyleName runs one palette style through the product's own screen parser and
// returns the style string a dump would carry for a cell painted with it.
func ptyCtxStyleName(style lineedit.Style) string {
	if style.Empty() {
		return ""
	}
	scr := lineedit.NewScreen(len(ptyCtxStyleProbe), 1)
	scr.Feed([]byte(style.Open() + ptyCtxStyleProbe))
	return scr.Attr(0, 0).String()
}

// ptyCtxStyleProbe is the cell the style-name resolver paints: one printable rune.
const ptyCtxStyleProbe = "x"

// ptyCtxDumpRunsNote renders one dump row's attr runs, so a failure quotes the runs it
// judged instead of a coordinate.
func ptyCtxDumpRunsNote(cand uiclient.Candidate, row int) string {
	if row < 0 || row >= len(cand.Dump.AttrRuns) {
		return fmt.Sprintf("(no attr runs recorded for row %d)", row)
	}
	var b strings.Builder
	for _, run := range cand.Dump.AttrRuns[row] {
		fmt.Fprintf(&b, "  cols %d..%d = %s\n", run.Col, run.Col+run.Cells-1, run.Style)
	}
	if b.Len() == 0 {
		return fmt.Sprintf("(row %d carries no runs)", row)
	}
	return b.String()
}

// ptyCtxAwaitDump polls the surface's own dump until its CELL GRID carries every
// wanted cell, and returns the settled candidate.
//
// It polls the ARTIFACT rather than the byte stream because the artifact is what the
// assertions below read: the dump's sink writes on a 100ms throttle and installs a
// state only when the screen CHANGES (cmd/agent/chat_dump.go), so its last frame can
// legitimately be one change behind the terminal. A case that read it once would be
// asserting the sampler's timing; polling it until the row it is about is IN the grid
// makes the read a statement about the screen that was installed (the shape
// lblCadenceAwaitAnswer uses for the same reason).
func ptyCtxAwaitDump(t *testing.T, dumpPath string, want []string) uiclient.Candidate {
	t.Helper()
	deadline := time.Now().Add(ptyPromptTimeout)
	var last string
	for time.Now().Before(deadline) {
		cand, err := uiclient.ReadDump(dumpPath)
		if err == nil {
			// The grid is the column-exact carrier; Lines trims trailing blanks and
			// skips a wide rune's continuation cell, so a column read off it would be
			// the wrong number for the attr runs below.
			row := ptyCtxRowWith(cand.Dump.Grid, want)
			if row >= 0 {
				time.Sleep(ptyCtxDivergenceQuiet) //stylegate:allow-sleep lets the throttled sampler install the settled frame, so the grid is read after the paint stopped rather than mid-paint
				cand, err = uiclient.ReadDump(dumpPath)
				if err == nil && ptyCtxRowWith(cand.Dump.Grid, want) >= 0 {
					return cand
				}
			}
			last = fmt.Sprintf("grid has %d rows; tail: %q", len(cand.Dump.Grid), ptyCtxGridTail(cand.Dump.Grid, 3))
		} else {
			last = err.Error()
		}
		time.Sleep(ptyCtxFrameGap) //stylegate:allow-sleep polls the artifact the surface writes on a throttle; it is a reader's cadence, not the interval under measurement
	}
	t.Fatalf("the dump %s never showed a row carrying %v within %s; last read: %s", dumpPath, want, ptyPromptTimeout, last)
	return uiclient.Candidate{}
}

// ptyCtxFrameGap is how often the dump poll above re-reads the artifact: an order of
// magnitude under the sink's own 100ms throttle, so the poll cannot be the thing that
// makes a frame late.
const ptyCtxFrameGap = 20 * time.Millisecond

// ptyCtxRowWith returns the index of the grid row carrying every wanted cell, or -1.
func ptyCtxRowWith(grid []string, want []string) int {
	for i, row := range grid {
		all := true
		for _, w := range want {
			if !strings.Contains(row, w) {
				all = false
				break
			}
		}
		if all {
			return i
		}
	}
	return -1
}

// ptyCtxGridTail renders the last n grid rows, for a failure message.
func ptyCtxGridTail(grid []string, n int) string {
	from := 0
	if len(grid) > n {
		from = len(grid) - n
	}
	var b strings.Builder
	for i := from; i < len(grid); i++ {
		fmt.Fprintf(&b, "row[%03d]=%q\n", i, strings.TrimRight(grid[i], " "))
	}
	return b.String()
}

// ptyCtxRunCovering returns the attr run covering one RUNE column of a dump row, and
// whether one was found. Columns are runes on the grid (one rune per cell), which is
// the unit the runs index — using byte offsets here would report the wrong run on any
// row carrying a multi-byte rune before the cell under test.
func ptyCtxRunCovering(cand uiclient.Candidate, row, runeCol int) (lineedit.DumpAttrRun, bool) {
	if row < 0 || row >= len(cand.Dump.AttrRuns) {
		return lineedit.DumpAttrRun{}, false
	}
	for _, run := range cand.Dump.AttrRuns[row] {
		if runeCol >= run.Col && runeCol < run.Col+run.Cells {
			return run, true
		}
	}
	return lineedit.DumpAttrRun{}, false
}

// ptyCtxRuneCol is the RUNE column of a wanted cell inside a grid row, or -1.
func ptyCtxRuneCol(row, want string) int {
	rr, w := []rune(row), []rune(want)
	for i := 0; i+len(w) <= len(rr); i++ {
		if string(rr[i:i+len(w)]) == want {
			return i
		}
	}
	return -1
}

func ptyReadSettled(chat *ptySession, seed string, quiet, limit time.Duration) string {
	var got strings.Builder
	got.WriteString(seed)
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		select {
		case b, ok := <-chat.rx:
			if !ok {
				return got.String()
			}
			got.WriteByte(b)
		case <-time.After(quiet):
			return got.String()
		}
	}
	return got.String()
}

// ptyCtxTurnUsage is one TURN's scripted transport usage: the three prompt-side
// counts the row reads, in the same split as ptyCtxUsedTokens/ptyCtxCacheReadTok/
// ptyCtxCacheCreateTok above. It exists because the divergence case below needs two
// turns whose cache traffic DIFFERS — a stub that answers every request with the
// same numbers makes the session ratio equal to the turn ratio by construction (an
// average of identical samples IS the sample), so no single-usage stub can show the
// two readings apart.
type ptyCtxTurnUsage struct {
	input  int
	create int
	read   int
}

// ptyCtxDefaultUsage is the usage the single-turn cases script — the three named
// counts above, in the split the row's context numerator requires.
var ptyCtxDefaultUsage = ptyCtxTurnUsage{
	input:  ptyCtxInputTok,
	create: ptyCtxCacheCreateTok,
	read:   ptyCtxCacheReadTok,
}

// usageSum is the prompt-side total of one usage record: the row's context
// numerator (input + create + read) and the reading's denominator (read + the
// miss-billed create+input).
func (u ptyCtxTurnUsage) usageSum() int { return u.input + u.create + u.read }

// startUsageLLM starts a provider stub serving the scripted usages: request n is
// answered with turns[n], and a request past the script repeats the LAST entry (the
// harness's own convention — a turn whose confirmation round arrives must not
// receive a usage the caller never scripted). No turns at all is the single-turn
// case (ptyCtxDefaultUsage), so the one-turn call sites read unchanged.
func startUsageLLM(t *testing.T, turns ...ptyCtxTurnUsage) *httptest.Server {
	t.Helper()
	if len(turns) == 0 {
		turns = []ptyCtxTurnUsage{ptyCtxDefaultUsage}
	}
	var mu sync.Mutex
	served := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		u := turns[len(turns)-1]
		if served < len(turns) {
			u = turns[served]
		}
		served++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		start := fmt.Sprintf(`{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":%d,"output_tokens":0,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d}}}`,
			u.input, u.create, u.read)
		write := func(event, data string) {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		}
		write("message_start", start)
		write("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		write("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pty probe answer"}}`)
		write("content_block_stop", `{"type":"content_block_stop","index":0}`)
		// The usage that reaches the ROW arrives on message_delta: the harness
		// records the prompt count there (internal/agent/loop.go's message_delta
		// arm — input + cache_creation + cache_read). message_start's copy is the
		// pre-delta fallback and is deliberately NOT what this test measures.
		write("message_delta", fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":%d,"output_tokens":12,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d}}`,
			u.input, u.create, u.read))
		write("message_stop", `{"type":"message_stop"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestChatPTY_ContextDenominatorIsQualifiedOnScreen is the rendered-truth check of
// bug F: the chat status row states WHAT the context denominator is, on the screen,
// beside the number, at the width the row is designed for (100 columns).
func TestChatPTY_ContextDenominatorIsQualifiedOnScreen(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := startUsageLLM(t)

	// The screen is read from the SURFACE'S OWN dump (--dump-screen), back through the
	// tool's own reader — the CELL GRID, never a substring of the raw pty stream. The
	// stream is a sequence of incremental REPAINTS: a redraw writes only the cells that
	// changed, so two adjacent cells of one row routinely arrive in different reads
	// (measured here: while the field was being composed the row arrived one changed
	// cell per frame, and only the composed row carries "ctx 92k/200k≡" whole). An
	// assertion over that stream would be testing the repaint's chunking rather than
	// the screen — and a dump is the artifact the operator's own tooling reads.
	dumpPath := filepath.Join(t.TempDir(), uiclient.ChatDumpFileName)
	chat := startPTYChat(t, []string{"chat", "--dump-screen=" + dumpPath}, testutil.MockEnv(t, home, llm.URL), work)
	// The session is closed ONCE, at the deliberate /quit below (the shape
	// TestChatPTY_ResizeDoesNotFreezeTheEditor uses). A deferred close in ADDITION
	// would call cmd.Wait a second time and report "exec: Wait was already called"
	// as a test failure — a harness artifact, not a row defect. The cleanup path
	// only kills a still-running child; it never waits.
	t.Cleanup(func() {
		if chat.cmd.Process != nil {
			_ = chat.cmd.Process.Kill() //stylegate:ignore best-effort teardown of a failed run
		}
		_ = chat.master.Close() //stylegate:ignore the master is the test's own descriptor
	})
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	// The row the defect was reported on is the row at the width it is DESIGNED
	// for: the metrics group is the first thing the width ladder sheds, so on the
	// pty's own default geometry the ctx pair is legitimately absent and this test
	// would be asserting the degradation rather than the read-out. The resize is a
	// REAL SIGWINCH (the same call the resize test makes), never a flag.
	chat.resize(t, ptyResizeCols, ptyResizeRows)
	time.Sleep(ptyResizeSettled) //stylegate:allow-sleep lets the resize redraw finish before typing, so the keystroke tests the drain and not a redraw race (the same pin the resize test in chat_pty_test.go carries)

	chat.writeGapped(t, "probe the context row")
	chat.write(t, "\r")

	// The cells under assertion are read from the product's own count ladder
	// (ptyCtxPairRendered → ptyStatusCountRendered), never spelled here: the row's
	// spelling is the product's rule, and a copy of it in this file is a copy that
	// goes stale silently — which is exactly the defect this case was repaired for.
	//
	// WHERE THE ASSERTION IS MADE, and why: on the pty BYTE STREAM (the carrier this case
	// has always used), not on the --dump-screen grid. The dump is sampled by a throttled
	// sink that installs a state only when the screen CHANGES (cmd/agent/chat_dump.go says
	// so in its own words), so the change that lands inside the throttle window is dropped
	// and — the screen then standing still — never installs: measured here, the dump's last
	// frame stayed on "responding … no metrics" for the whole 71s while the terminal had
	// already received the qualified row. A grid that can be one change behind is not the
	// right carrier for a wait; the dump is still armed and written (this run's own
	// artifact, listed by `gopi ui_state`), and the CELL-grid reader is what a reader can
	// point at afterwards, but the assertion below reads the bytes the terminal actually got.
	//
	// The stream is a sequence of incremental repaints — a redraw writes only the cells
	// that changed — so the assertions are stated over the ACCUMULATED stream (one
	// substring test per cell run), never over one read: a substring that spans two
	// repaints is not a screen fact.
	pair := ptyCtxPairRendered(t)
	qualified := pair + ptyCtxQuantGlyph

	// The row is repainted with the turn's usage once the round-trip lands. The marker is
	// the QUALIFIED denominator as one string, which is both the wait and the adjacency
	// assertion: the qualifier must be the denominator's next cell.
	out := chat.waitFor(t, qualified, ptyPromptTimeout)
	gridNote := ptyCtxDumpGridNote(t, dumpPath)
	// Logged on success too: the dump is the surface's own artifact, and a reader of a
	// green run should be able to see the grid the surface had installed — including the
	// one-change-behind staleness this case documents, which is why it asserts on the stream.
	t.Logf("pty stream carried %q; %s", qualified, gridNote)

	if !strings.Contains(out, pair) {
		t.Fatalf("the row does not carry the whole denominator %q — the pair was cut\n%sstream:\n%s", pair, gridNote, out)
	}
	if !strings.Contains(out, ptyCtxLabelRendered) {
		t.Fatalf("the context field's label %q never reached the terminal: the field was squeezed into its yield "+
			"form at the width it is designed for\n%sstream:\n%s", ptyCtxLabelRendered, gridNote, out)
	}
	if strings.Contains(out, pair+ptyCtxRetiredMarker) {
		t.Fatalf("the retired heavy marker is painted beside the denominator (%q) — bug F's second half is back"+
			"\n%sstream:\n%s", pair+ptyCtxRetiredMarker, gridNote, out)
	}

	// THE OTHER HALF OF THE SAME DEFECT, and the reason this case reads on rather than
	// stopping at the pair: the row painted "· hit 13.4% ·↑12k↓80k" — the RATE leading
	// and the pair absent — so a case that asserted the pair ALONE would go green on the
	// mirror image of the same failure (the pair present, the rate dropped) without
	// noticing. The wait above returns the instant the pair's LAST cell arrives, i.e.
	// MID-repaint: measured here, the bytes received by then end exactly at "ctx 92k/200k≡"
	// and the rate is one repaint later. So the settled stream is read first, and the two
	// facts are asserted on it together.
	settled := ptyReadSettled(chat, out, ptyCtxSettleQuiet, ptyCtxSettleLimit)
	plain := chatRenderStripANSI(settled)
	rate := ptyCtxRateReading()
	if !strings.Contains(plain, rate) {
		t.Fatalf("the settled row does not carry the hit rate %q: the rate was dropped at the width this row is "+
			"designed for, which is acceptance (e) reversed (the RATE yields only where the cells force a choice, "+
			"and never silently)"+
			"\n%sstream:\n%s", rate, gridNote, settled)
	}
	// ... AND THE SECOND READING IS ON THE SAME ROW (this epic). The row states two
	// rates, each introduced by its own marker: the session-cumulative one (Σ) and this
	// turn's (Δ). Asserting only the first would let the epic's whole deliverable — the
	// turn reading existing, and being DISTINGUISHABLE from the cumulative one — be
	// dropped silently, which is the same mirror-image blind spot the pair/rate clause
	// above was written for, one reading further along.
	//
	// The turn reading's MAGNITUDE is this file's own script's (the pty session runs ONE
	// turn, so the turn's counters and the session's are the same numbers) — computed,
	// never typed, for the same reason the cumulative one is.
	turn := ptyCtxRateTurnReading()
	if !strings.Contains(plain, turn) {
		t.Fatalf("the settled row does not carry THIS TURN's reading %q beside the cumulative one %q: the two "+
			"readings must stand side by side and be told apart by their markers (Σ over the whole session, Δ over "+
			"this turn)"+
			"\n%sstream:\n%s", turn, rate, gridNote, settled)
	}
	// The two readings are ADJACENT and in the cumulative-first order the composers fix
	// — a row that painted them apart, or in the other order, would satisfy the two
	// Contains above and still not be the read-out the epic describes.
	if got := strings.Index(plain, rate+turn); got < 0 {
		t.Fatalf("the cumulative reading %q and the turn reading %q are not adjacent on the settled row (in that "+
			"order): the row draws ONE read-out carrying both, not two numbers with a gap between them"+
			"\n%sstream:\n%s", rate, turn, gridNote, settled)
	}
	// THE ORDER, measured in cells on the settled stream rather than inferred from the
	// composers: the ctx pair's reading is written BEFORE the rate's, and the cells
	// between them are the cache transport's — which is what "the pair LEADS the metrics
	// group, the rate keeps a visible home beside the counters it is computed from" means
	// on the screen. A row that put the rate first would satisfy every pair-only
	// assertion above and fail here.
	pairAt, rateAt := strings.LastIndex(plain, qualified), strings.LastIndex(plain, rate)
	if pairAt < 0 || rateAt < 0 || pairAt > rateAt {
		t.Fatalf("the ctx pair does not LEAD the hit rate on the settled row (pair at byte %d, rate %d): the group "+
			"must state the pair first and the rate after the transport it is computed from"+
			"\n%sstream:\n%s", pairAt, rateAt, gridNote, settled)
	}
	between := plain[pairAt:rateAt]
	if !strings.Contains(between, ptyCtxTransportGlyph) {
		t.Fatalf("the cells between the ctx pair and the hit rate (%q) do not carry the cache transport %q: the rate's "+
			"home is the sub-cell whose two counters it is computed from"+
			"\n%sstream:\n%s", between, ptyCtxTransportGlyph, gridNote, settled)
	}
	t.Logf("pty settled row: pair at %d, readings %q then %q at %d, transport between: %q",
		pairAt, rate, turn, rateAt, between)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 — the session ends at the deliberate /quit", code)
	}
}

// TestChatPTYCtxQualifierExpectedCellsFollowTheRowsLadder is the minimal regression guard
// for the repair above, and it is the clause that makes the DEFECT reproducible without
// a 72-second pty run: the cells this case waits for must be the row's own ladder's
// output, at the ladder's documented boundaries.
//
// What it pins, in the ladder's own terms:
//
//	92 100  -> "92k"   (two whole digits — statusCountWholeDigitsWidest)
//	200000  -> "200k"  (three whole digits — the context pair's own allowance)
//
// Those two boundaries are what the retired k-only formatter ("92.1k", "200.0k") could
// not express, so a ladder that quietly changed its digit policy — or a spelling put
// back into this file by hand — fails HERE, in milliseconds, naming the count, instead
// of as a 71.69s pty timeout.
func TestChatPTYCtxQualifierExpectedCellsFollowTheRowsLadder(t *testing.T) {
	units := ptyStatusLadder(t)

	// The ladder's own boundaries, stated over the counts this case scripts.
	for _, tc := range []struct {
		n    int
		want string
	}{
		{ptyCtxUsedTokens, "92k"},            // the scripted occupancy
		{agent.DefaultContextWindow, "200k"}, // the deployed window, at the pair's own allowance
		{999, "999"},                         // below the first named scale, no unit
		{42100, "42k"},                       // one rung, two digits
	} {
		if got := ptyStatusCountRenderedUnits(tc.n, units); got != tc.want {
			t.Fatalf("the row's ladder renders %d as %q, want %q — this case's expected cells are derived from that ladder",
				tc.n, got, tc.want)
		}
	}

	// The composed pair and the marker the pty case waits for. The expectation is the
	// LADDER's output for the two counts under test, never a re-typed spelling.
	wantPair := "92k" + ptyCtxPairSep + "200k"
	if got := ptyCtxPairRendered(t); got != wantPair {
		t.Fatalf("the expected denominator pair is %q, want %q — the deployed window (%d) and the scripted occupancy (%d) "+
			"must render through the row's ladder", got, wantPair, agent.DefaultContextWindow, ptyCtxUsedTokens)
	}
	if got := wantPair + ptyCtxQuantGlyph; strings.HasSuffix(wantPair, ptyCtxQuantGlyph) || got != ptyCtxPairRendered(t)+ptyCtxQuantGlyph {
		t.Fatalf("the qualifier does not ride the pair: pair=%q qualified=%q", wantPair, got)
	}

	// The retired formatter's convention, refused by name: a fraction separator on this
	// row is the k-only ladder's forced ".1" tail, which the row no longer draws.
	if strings.Contains(ptyCtxPairRendered(t), ptyStatusRetiredFractionSep) {
		t.Fatalf("the expected pair %q carries a %q: that is the retired formatter's forced decimal tail, not this row's ladder",
			ptyCtxPairRendered(t), ptyStatusRetiredFractionSep)
	}
	// And no side may state more whole digits than a rung may print.
	for _, side := range strings.Split(ptyCtxPairRendered(t), ptyCtxPairSep) {
		digits := strings.TrimRightFunc(side, func(r rune) bool { return r < '0' || r > '9' })
		if len(digits) > ptyStatusWholeDigitsMax {
			t.Fatalf("the expected side %q states %d whole digits, over the %d a rung may print",
				side, len(digits), ptyStatusWholeDigitsMax)
		}
	}
}

// ptyStatusRetiredFractionSep is the retired formatter's decimal point. It is spelled
// locally because the production spelling is GONE (statusCountFractionSep belongs to the
// bounded ladder, whose fraction digits are 0) — this constant exists so the guard above
// can name what it refuses.
const ptyStatusRetiredFractionSep = "."

// TestChatPTYCtxQualifierExpectationRefusesTheRetiredSpelling is the RED PROOF of that
// guard, and it is stated over RENDERED SIDES through the SAME predicate (a red proof
// that re-implemented the check would only prove the copy agrees with itself): the
// retired formatter's own output for the two counts under test — "92.1k" and "200.0k",
// spelled here as the literals it produced, since its code is deleted — must be REFUSED
// by name, and the live sides accepted. Restore the retired spelling as this case's
// expectation (or soften the ladder's digit policy) and the pty case goes back to its
// 71.69s timeout while this test fails in milliseconds.
func TestChatPTYCtxQualifierExpectationRefusesTheRetiredSpelling(t *testing.T) {
	units := ptyStatusLadder(t)
	for _, tc := range []struct {
		retired string
		n       int
	}{
		{"92.1k", ptyCtxUsedTokens},            // "%.1fk" of 92100
		{"200.0k", agent.DefaultContextWindow}, // "%.1fk" of the window
	} {
		why := ptyStatusSideRefusal(tc.retired, tc.n, units)
		if why == "" {
			t.Fatalf("the retired spelling %q is ACCEPTED for %d — the guard no longer witnesses the defect it closes",
				tc.retired, tc.n)
		}
		if !strings.Contains(why, "ladder") || !strings.Contains(why, "unit system") {
			t.Fatalf("the refusal of %q does not name the ladder/unit-system reason: %s", tc.retired, why)
		}
		if live := ptyStatusCountRenderedUnits(tc.n, units); live == tc.retired {
			t.Fatalf("the live spelling of %d is still the retired form %q", tc.n, live)
		}
		if why := ptyStatusSideRefusal(ptyStatusCountRenderedUnits(tc.n, units), tc.n, units); why != "" {
			t.Fatalf("the live spelling of %d is refused by the guard: %s", tc.n, why)
		}
	}
}

// ptyCtxDivergenceQuiet / ptyCtxDivergenceLimit bound the settle read of the
// divergence case. They are the same shape as ptyCtxSettleQuiet/Limit and are named
// separately because this case reads TWO settled rows (one per turn) and must give
// the second turn's repaint its own window.
const (
	ptyCtxDivergenceQuiet = 700 * time.Millisecond
	ptyCtxDivergenceLimit = 10 * time.Second
)

// TestChatPTY_TwoReadingsDivergeOnScreen is the DIVERGENCE leg of the epic's
// rendered-truth requirement, on a real pty, from the shipped binary.
//
// WHAT IT ADDS OVER TestChatPTY_ContextDenominatorIsQualifiedOnScreen. That case runs
// ONE turn, so the session counters and the turn's are the same numbers and the two
// readings coincide by construction — it proves both readings are ON the row, ordered
// and coloured, and it can prove nothing about whether they are the SAME quantity.
// The defect that would survive it is precise and worth naming: a row that painted the
// cumulative rate twice (once per marker) would satisfy every assertion there. This
// case scripts TWO turns with DIFFERENT cache traffic, so the session average and the
// fresh sample must land on different numbers, and it asserts the row shows BOTH at
// once, each on its own computed value, in the cumulative-first order.
//
// The expected cells are DERIVED from the script (ptyCtxRateReadingOf /
// ptyCtxRateTurnReadingOf), never typed: a rate spelled here would be a second copy of
// the row's formula, free to drift from the product the day the formula moves.
func TestChatPTY_TwoReadingsDivergeOnScreen(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	turns := []ptyCtxTurnUsage{ptyCtxDefaultUsage, ptyCtxDivergentTurn}
	llm := startUsageLLM(t, turns...)

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL), work)
	t.Cleanup(func() {
		if chat.cmd.Process != nil {
			_ = chat.cmd.Process.Kill() //stylegate:ignore best-effort teardown of a failed run
		}
		_ = chat.master.Close() //stylegate:ignore the master is the test's own descriptor
	})
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	// The width the row is DESIGNED for: the metrics group is the first thing the
	// ladder sheds, and a narrow row would drop the Δ reading — which is a legitimate
	// degradation, but not the state this case is about.
	chat.resize(t, ptyResizeCols, ptyResizeRows)
	time.Sleep(ptyResizeSettled) //stylegate:allow-sleep lets the resize redraw finish before typing (the pin the resize test carries)

	// TURN 1 — the baseline the session accumulates from. Its own reading is the
	// cumulative one SO FAR (one turn in, they coincide), so this turn is not asserted
	// on: it exists to make turn 2's session average differ from turn 2's own rate.
	chat.writeGapped(t, "probe turn one")
	chat.write(t, "\r")
	first := chat.waitFor(t, ptyCtxRateReadingOf(turns[:1]), ptyPromptTimeout)
	_ = ptyReadSettled(chat, first, ptyCtxDivergenceQuiet, ptyCtxDivergenceLimit)

	// TURN 2 — the diverging turn. The first turn's own mark was already seen; the
	// wait is therefore for the CUMULATIVE reading of BOTH turns, which is a number
	// the row cannot have painted before turn 2's usage landed.
	session := ptyCtxRateReadingOf(turns)
	chat.writeGapped(t, "probe turn two")
	chat.write(t, "\r")
	out := chat.waitFor(t, session, ptyPromptTimeout)
	settled := ptyReadSettled(chat, out, ptyCtxDivergenceQuiet, ptyCtxDivergenceLimit)
	plain := chatRenderStripANSI(settled)

	turn := ptyCtxRateTurnReadingOf(ptyCtxDivergentTurn)
	// The two readings must be DIFFERENT numbers, or this case proves nothing: the
	// whole point is that a row repeating one value under two markers cannot pass.
	if ptyCtxSessionRatio(turns) == ptyCtxTurnRatio(ptyCtxDivergentTurn) {
		t.Fatalf("the script does not diverge: session %q and turn %q are the same number — fix the scripted turns, "+
			"this case cannot witness the epic's property with them", session, turn)
	}
	if !strings.Contains(plain, session) {
		t.Fatalf("the settled row does not carry the SESSION-cumulative reading %q over BOTH turns", session)
	}
	if !strings.Contains(plain, turn) {
		t.Fatalf("the settled row does not carry THIS TURN's reading %q — over the two different turns the readings "+
			"must diverge, and a row that repeats the cumulative value under the %q marker is exactly the defect this "+
			"case exists to catch\nstream:\n%s", turn, ptyCtxHitRateTurnTier, settled)
	}
	if i := strings.Index(plain, session+turn); i < 0 {
		t.Fatalf("the cumulative reading %q and the turn reading %q are not ADJACENT and in that order on the settled "+
			"row — the row draws ONE read-out carrying both", session, turn)
	}
	// BOTH spans must be COLOURED, and by their own tier runs — the epic's acceptance
	// asks for the readings to be distinguishable, and the emphasis channel is what
	// distinguishes them visually. The check is on the RAW stream (before the strip):
	// each reading must be preceded by an SGR run carrying the colour the palette gives
	// its tier. The tier is resolved from the row's own floors, so a reading that moved
	// tiers moves this expectation with it (Rule 14) rather than leaving the test
	// asserting yesterday's colour.
	if !strings.Contains(settled, session) || !strings.Contains(settled, turn) {
		t.Fatalf("a reading is present only after stripping SGR — the row painted it uncoloured")
	}
	if !ptyCtxReadingIsColoured(settled, session) {
		t.Fatalf("the cumulative reading %q carries no SGR run of its own on the raw stream: the emphasis channel "+
			"is inert at exactly the state it exists for\nstream:\n%s", session, settled)
	}
	if !ptyCtxReadingIsColoured(settled, turn) {
		t.Fatalf("the turn reading %q carries no SGR run of its own on the raw stream\nstream:\n%s", turn, settled)
	}
	t.Logf("pty divergent row: session %q and turn %q both on the settled stream, each inside its own SGR run", session, turn)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 — the session ends at the deliberate /quit", code)
	}
}

// ptyCtxReadingIsColoured reports whether reading appears on the raw (un-stripped)
// stream immediately preceded by an SGR sequence that sets a colour — the evidence
// that the emphasis channel put a tier on it.
//
// It is stated over the raw bytes because stripping is what erases the distinction:
// `plain` cannot tell a coloured reading from a monochrome one, and the epic's
// acceptance is about the picture. A reading is "coloured" when the LAST SGR sequence
// opened before its first byte carries a colour parameter; an SGR that only resets is
// not a tier.
func ptyCtxReadingIsColoured(stream, reading string) bool {
	at := strings.LastIndex(stream, reading)
	if at < 0 {
		return false
	}
	head := stream[:at]
	open := strings.LastIndex(head, "\x1b[")
	if open < 0 {
		return false
	}
	end := strings.IndexByte(head[open:], 'm')
	if end < 0 {
		return false
	}
	params := head[open+2 : open+end]
	// A colour is either a 30..37 / 90..97 basic pair, a `38;5;N` index, or a
	// 38;2;r;g;b truecolour triple. Anything else (a bare reset, a bold) is not a tier.
	for _, p := range strings.Split(params, ";") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			continue
		}
		if (n >= 30 && n <= 37) || (n >= 90 && n <= 97) || n == 38 {
			return true
		}
	}
	return false
}
