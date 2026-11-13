package config

// ambiguous_width.go — THE ONE DEFINITION of "this character's width depends on
// the environment" (Unicode East Asian Width property A, Ambiguous), and the ONE
// place that decides which environment gopi assumes it is drawing into.
//
// WHY THIS EXISTS AT ALL. Every width decision gopi makes in a terminal — how
// many cells a rune spends, when a row wraps, where the cursor is put — is a
// decision that A-runes make ENVIRONMENT-DEPENDENT: the same codepoint is one
// cell in a Western terminal and two in a CJK one. gopi's tables used to answer
// "one" silently (the `wideRanges` in internal/lineedit carry the W/F set and
// nothing else), so a pasted box-drawing table (U+2500–257F is A in full) lost one
// cell per symbol, the wrap count came out low and the cursor landed a row above
// the text — measured on this host, see devdoc/lineedit-eaw-ambiguous.md.
//
// WHY THE TABLE IS HERE AND NOT IN internal/lineedit (Rule 14). Three packages
// measure runes: internal/lineedit (the editor's layout AND its screen cell grid,
// via screenattr.go), internal/config (the help-wrap budget), internal/text2img
// (the grid renderer). A table living in any ONE of them cannot be imported by
// the other two without a cycle, so it would be mirrored — which is exactly the
// drift Rule 13/14 forbid. internal/config is the leaf every one of them can
// import (it imports neither lineedit nor text2img), so the FACT lives here and
// each package applies it under its own contract.
//
// NOT A GENERATED TABLE FROM A NEW DEPENDENCY (Rule 35): the ranges below were
// produced ONCE, offline, from CPython's unicodedata (Unicode 14.0.0), and the
// exact generator is recorded so a future reader can re-derive them without
// trusting this file:
//
//   python3 -c "import unicodedata as u; \
//     print([[hex(c),'A'] for c in range(0x110000) if u.east_asian_width(chr(c))=='A'])" \
//     | (coalesce consecutive codepoints into ranges; drop the CJK-machinery
//        blocks that lineedit's wideRanges already carries as W/F, and the
//        private-use / variation-selector / plane-15-16 blocks that render 1
//        cell even in a CJK terminal — see ambiguousWidthExcluded)
//
// MEASURED, NOT ASSUMED, for the boxes that were dropped: U+E000 (private use),
// U+FE0F (variation selector), U+100000+ — the CJK fonts on this host render them
// as ONE cell, so counting them two would invent a drift of its own. The test
// TestAmbiguousRangesAreOrderedAndBounded pins the shape of the table (173
// ranges, sorted, disjoint) so an edit that breaks it is red.
//
// THE KNOWN DEVIATION, stated once: the table also carries the mathematical
// symbols, arrows, enclosed numbers/circles, box-drawing and the Greek/Cyrillic
// blocks. In a CJK terminal those genuinely are 2 cells (measured on this host:
// xterm + a CJK font + LC_ALL=zh_CN.UTF-8 draws U+2500 in two columns), and in a
// Western terminal they genuinely are 1. WHICH one is in force is the knob below
// — it is not invented here, it is DECLARED here.

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// AmbiguousWidth* are the knob's three channels. They exist as constants because
// every one of them is a string that carries meaning (Rule 1b): the settings key,
// the env channel and the CLI spelling must be the SAME three strings in the
// catalog row, the parser and the resolver, and a knob whose env name is spelled
// twice quietly stops working in one of its halves (internal/config/film_terminal.go
// is the precedent this file follows).
const (
	// AmbiguousWidthSettingsKey is the settings.json key ("ambiguous_width").
	AmbiguousWidthSettingsKey = "ambiguous_width"
	// AmbiguousWidthEnv is the env channel (GOPI_AMBIGUOUS_WIDTH).
	AmbiguousWidthEnv = "GOPI_AMBIGUOUS_WIDTH"
	// AmbiguousWidthFlagName is the CLI spelling WITHOUT dashes ("ambiguous-width").
	AmbiguousWidthFlagName = "ambiguous-width"
	// AmbiguousWidthFlag is the FULL spelling, composed from the shared dash
	// prefix so no consumer writes a second one.
	AmbiguousWidthFlag = flagPrefix + AmbiguousWidthFlagName
)

// The knob's two named values. They are the ONLY accepted spellings: a resolver
// that also accepted "eastasian"/"western" would be a second vocabulary for one
// decision (Rule 13).
const (
	// AmbiguousWidthNarrow: an A-rune spends ONE cell (the Western-terminal
	// reading; gopi's historical behaviour).
	AmbiguousWidthNarrow = "narrow"
	// AmbiguousWidthWide: an A-rune spends TWO cells (the CJK-terminal reading).
	AmbiguousWidthWide = "wide"
)

// ambiguousWidthAuto is the DEFAULT and it is not a third width: "let the locale
// decide" — a value named here so the resolver, the settings catalog and CONFIG.md
// spell the default identically.
const ambiguousWidthAuto = "auto"

// ApplyEnvAmbiguousWidth publishes the knob's settings/CLI value into its ONE env
// representation, exactly as ApplyEnv does for the rest of the catalog — so the
// single reader below (ResolveAmbiguousWidthMode) is the only place that has to
// know the precedence (Rule 2).
func ApplyEnvAmbiguousWidth(v string) { os.Setenv(AmbiguousWidthEnv, v) }

// ResolveAmbiguousWidthMode answers the one question every width decision asks:
// are A-runes two cells on the terminal gopi is drawing into?
//
// PRECEDENCE (Rule 2 — one reader, one order):
//
//	rank 1  GOPI_AMBIGUOUS_WIDTH=wide|narrow  (the env channel, which the settings
//	        key and the --ambiguous-width flag are PUBLISHED into by ApplyEnv /
//	        the flag parser, so all three channels meet here)
//	rank 2  the LOCALE, and only the locale's own variables, in the order the C
//	        library itself resolves them: LC_ALL, then LC_CTYPE, then LANG
//	rank 3  narrow (the historical behaviour, unchanged for everyone else)
//
// WHY THE LOCALE IS THE DEFAULT RATHER THAN THE MEASUREMENT. The epic asked for
// the terminal's REAL rendering width and this host answered it (both readings
// exist here — see devdoc/lineedit-eaw-ambiguous.md): the rendering width of an
// A-rune is a property of the FONT, and xterm's default `fixed` face (the
// `-fn` it starts with, which an operator's real terminal inherits as often as
// not) renders box-drawing ONE cell wide even under LC_ALL=zh_CN.UTF-8. A
// measurement taken at start-up would therefore be WRONG on the operator's own
// terminal. And there is a stronger reason to keep the locale in charge: it is
// the variable the CJK conventions are already keyed on (less(1) treats A as
// two cells iff LC_CTYPE is East Asian), the operator can see and set it, and a
// wrong answer is one export away from being fixed — none of which is true of a
// number gopi inferred from a font it cannot name.
//
// A locale that says nothing (the "C"/POSIX locale, an unset variable) means
// narrow. The C locale IS the Western reading, so this is not a fallback: it is
// the answer.
func ResolveAmbiguousWidthMode(read func(string) string) string {
	raw := strings.TrimSpace(read(AmbiguousWidthEnv))
	switch strings.ToLower(raw) {
	case AmbiguousWidthWide:
		return AmbiguousWidthWide
	case AmbiguousWidthNarrow:
		return AmbiguousWidthNarrow
	case "":
		// Nobody declared anything: the locale decides (below).
	default:
		// Rule 15: a value gopi did not understand is REPORTED, and the next
		// channel decides — never silently read as unset, because "the operator
		// asked for a width I do not know" and "nobody asked" must not look
		// alike. The report is once per distinct bad value: this resolver is
		// called per rune of every row, and a warning per rune would be noise
		// rather than a signal.
		reportAmbiguousWidthRefusal(raw)
	}
	if ambiguousLocaleIsCJK(read) {
		return AmbiguousWidthWide
	}
	return AmbiguousWidthNarrow
}

// The refusal text and the once-per-value report. The spelling of the knob is
// composed from the constants above so the message cannot name a second one
// (Rule 13).
const (
	ambiguousWidthRefusalFmt = "%s: %q is not %q or %q — %s\\n"
	ambiguousWidthUnderstood = "the understood values are " + AmbiguousWidthWide + " and " + AmbiguousWidthNarrow
	ambiguousWidthFallback   = "the locale decides"
)

var (
	ambiguousWidthWarnMu  sync.Mutex
	ambiguousWidthWarned            = map[string]bool{}
	ambiguousWidthWarnOut io.Writer = os.Stderr
)

// reportAmbiguousWidthRefusal writes the refusal ONCE for each distinct bad
// value, and never twice for the same one.
func reportAmbiguousWidthRefusal(value string) {
	ambiguousWidthWarnMu.Lock()
	defer ambiguousWidthWarnMu.Unlock()
	if ambiguousWidthWarned[value] {
		return
	}
	ambiguousWidthWarned[value] = true
	// deliberately ignored: a diagnostic that cannot be written must not abort
	// the width decision it is describing (the fallback below is still correct).
	_, _ = fmt.Fprintf(ambiguousWidthWarnOut, ambiguousWidthRefusalFmt,
		AmbiguousWidthEnv, value, AmbiguousWidthWide, AmbiguousWidthNarrow, ambiguousWidthFallback)
}

// ambiguousLocaleIsCJK reports whether the calling environment's locale is an
// East Asian one — the predicate the knob's "auto" default is made of.
//
// It reads LC_ALL, then LC_CTYPE, then LANG (the C library's own precedence, so
// gopi's reading agrees with the one less(1) and the fontconfig stack already
// made) through the CALLER's reader, which is what keeps this function testable
// without touching the process environment (Rule 17: a test flips a map, never
// os.Setenv).
//
// The two prefixes are the language codes whose scripts are East Asian. `C`,
// `POSIX` and every Western locale match neither and answer false, which is the
// whole of the "no CJK environment" case.
func ambiguousLocaleIsCJK(read func(string) string) bool {
	locale := ""
	for _, name := range ambiguousLocaleVars {
		if v := read(name); v != "" {
			locale = v
			break
		}
	}
	lang := strings.ToLower(locale)
	for _, code := range ambiguousLocaleCodes {
		if strings.HasPrefix(lang, code) {
			return true
		}
	}
	return false
}

var (
	// ambiguousLocaleVars is the C library's resolution order: the strongest
	// variable wins, and an EMPTY one is not set.
	ambiguousLocaleVars = []string{"LC_ALL", "LC_CTYPE", "LANG"}
	// ambiguousLocaleCodes are the language-code prefixes whose conventional
	// script is East Asian: Chinese, Japanese, Korean. They are prefixes, not
	// equality, so `zh_CN.UTF-8`, `ja_JP.eucJP` and `ko` all match.
	ambiguousLocaleCodes = []string{"zh", "ja", "ko"}
)

// IsAmbiguousWide reports whether r is one of the East Asian Width AMBIGUOUS
// runes AND the environment in force says such runes are two cells. It is the
// predicate a width table consults for the A class — the W/F set stays with its
// own owner, because W/F is environment-INDEPENDENT (always two) and folding the
// two classes into one table would make the next reader unable to tell which
// fact the table is claiming.
//
// A caller with a per-render decision of its own (internal/text2img paints into a
// grid and takes the decision as a parameter) passes it through
// IsAmbiguousRune plus its own mode; this function is the process-environment
// reading.
func IsAmbiguousWide(r rune) bool {
	if !IsAmbiguousRune(r) {
		return false
	}
	return ResolveAmbiguousWidthMode(os.Getenv) == AmbiguousWidthWide
}

// IsAmbiguousRune reports whether r carries East Asian Width property A,
// independent of any environment — the class question, not the width question.
func IsAmbiguousRune(r rune) bool {
	lo, hi := 0, len(ambiguousRanges)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch rg := ambiguousRanges[mid]; {
		case r < rg.lo:
			hi = mid - 1
		case r > rg.hi:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// ambiguousRangeCount is the number of ranges the generator emits, named so the
// table's own test can pin it (Rule 1: a count with a failure mode behind it is
// a named constant, not a literal in a test).
const ambiguousRangeCount = 173

// ambiguousRanges is the A class as 173 sorted, disjoint [{lo, hi}] pairs —
// Unicode East Asian Width property A, minus the three families listed above the
// import block. It is DATA with a recorded generator, never hand-edited (Rule 38
// applies to a figure; this is the table that figure was measured from): the test
// TestAmbiguousRangesAreOrderedAndBounded asserts the shape so an edit that
// breaks the ordering or the count is red rather than silently wrong.
var ambiguousRanges = []struct{ lo, hi rune }{
	{0x00A1, 0x00A1}, {0x00A4, 0x00A4}, {0x00A7, 0x00A8}, {0x00AA, 0x00AA},
	{0x00AD, 0x00AE}, {0x00B0, 0x00B4}, {0x00B6, 0x00BA}, {0x00BC, 0x00BF},
	{0x00C6, 0x00C6}, {0x00D0, 0x00D0}, {0x00D7, 0x00D8}, {0x00DE, 0x00E1},
	{0x00E6, 0x00E6}, {0x00E8, 0x00EA}, {0x00EC, 0x00ED}, {0x00F0, 0x00F0},
	{0x00F2, 0x00F3}, {0x00F7, 0x00FA}, {0x00FC, 0x00FC}, {0x00FE, 0x00FE},
	{0x0101, 0x0101}, {0x0111, 0x0111}, {0x0113, 0x0113}, {0x011B, 0x011B},
	{0x0126, 0x0127}, {0x012B, 0x012B}, {0x0131, 0x0133}, {0x0138, 0x0138},
	{0x013F, 0x0142}, {0x0144, 0x0144}, {0x0148, 0x014B}, {0x014D, 0x014D},
	{0x0152, 0x0153}, {0x0166, 0x0167}, {0x016B, 0x016B}, {0x01CE, 0x01CE},
	{0x01D0, 0x01D0}, {0x01D2, 0x01D2}, {0x01D4, 0x01D4}, {0x01D6, 0x01D6},
	{0x01D8, 0x01D8}, {0x01DA, 0x01DA}, {0x01DC, 0x01DC}, {0x0251, 0x0251},
	{0x0261, 0x0261}, {0x02C4, 0x02C4}, {0x02C7, 0x02C7}, {0x02C9, 0x02CB},
	{0x02CD, 0x02CD}, {0x02D0, 0x02D0}, {0x02D8, 0x02DB}, {0x02DD, 0x02DD},
	{0x02DF, 0x02DF}, {0x0391, 0x03A1}, {0x03A3, 0x03A9}, {0x03B1, 0x03C1},
	{0x03C3, 0x03C9}, {0x0401, 0x0401}, {0x0410, 0x044F}, {0x0451, 0x0451},
	{0x2010, 0x2010}, {0x2013, 0x2016}, {0x2018, 0x2019}, {0x201C, 0x201D},
	{0x2020, 0x2022}, {0x2024, 0x2027}, {0x2030, 0x2030}, {0x2032, 0x2033},
	{0x2035, 0x2035}, {0x203B, 0x203B}, {0x203E, 0x203E}, {0x2074, 0x2074},
	{0x207F, 0x207F}, {0x2081, 0x2084}, {0x20AC, 0x20AC}, {0x2103, 0x2103},
	{0x2105, 0x2105}, {0x2109, 0x2109}, {0x2113, 0x2113}, {0x2116, 0x2116},
	{0x2121, 0x2122}, {0x2126, 0x2126}, {0x212B, 0x212B}, {0x2153, 0x2154},
	{0x215B, 0x215E}, {0x2160, 0x216B}, {0x2170, 0x2179}, {0x2189, 0x2189},
	{0x2190, 0x2199}, {0x21B8, 0x21B9}, {0x21D2, 0x21D2}, {0x21D4, 0x21D4},
	{0x21E7, 0x21E7}, {0x2200, 0x2200}, {0x2202, 0x2203}, {0x2207, 0x2208},
	{0x220B, 0x220B}, {0x220F, 0x220F}, {0x2211, 0x2211}, {0x2215, 0x2215},
	{0x221A, 0x221A}, {0x221D, 0x2220}, {0x2223, 0x2223}, {0x2225, 0x2225},
	{0x2227, 0x222C}, {0x222E, 0x222E}, {0x2234, 0x2237}, {0x223C, 0x223D},
	{0x2248, 0x2248}, {0x224C, 0x224C}, {0x2252, 0x2252}, {0x2260, 0x2261},
	{0x2264, 0x2267}, {0x226A, 0x226B}, {0x226E, 0x226F}, {0x2282, 0x2283},
	{0x2286, 0x2287}, {0x2295, 0x2295}, {0x2299, 0x2299}, {0x22A5, 0x22A5},
	{0x22BF, 0x22BF}, {0x2312, 0x2312}, {0x2460, 0x24E9}, {0x24EB, 0x254B},
	{0x2550, 0x2573}, {0x2580, 0x258F}, {0x2592, 0x2595}, {0x25A0, 0x25A1},
	{0x25A3, 0x25A9}, {0x25B2, 0x25B3}, {0x25B6, 0x25B7}, {0x25BC, 0x25BD},
	{0x25C0, 0x25C1}, {0x25C6, 0x25C8}, {0x25CB, 0x25CB}, {0x25CE, 0x25D1},
	{0x25E2, 0x25E5}, {0x25EF, 0x25EF}, {0x2605, 0x2606}, {0x2609, 0x2609},
	{0x260E, 0x260F}, {0x261C, 0x261C}, {0x261E, 0x261E}, {0x2640, 0x2640},
	{0x2642, 0x2642}, {0x2660, 0x2661}, {0x2663, 0x2665}, {0x2667, 0x266A},
	{0x266C, 0x266D}, {0x266F, 0x266F}, {0x269E, 0x269F}, {0x26BF, 0x26BF},
	{0x26C6, 0x26CD}, {0x26CF, 0x26D3}, {0x26D5, 0x26E1}, {0x26E3, 0x26E3},
	{0x26E8, 0x26E9}, {0x26EB, 0x26F1}, {0x26F4, 0x26F4}, {0x26F6, 0x26F9},
	{0x26FB, 0x26FC}, {0x26FE, 0x26FF}, {0x273D, 0x273D}, {0x2776, 0x277F},
	{0x2B56, 0x2B59}, {0x3248, 0x324F}, {0xFFFD, 0xFFFD}, {0x1F100, 0x1F10A},
	{0x1F110, 0x1F12D}, {0x1F130, 0x1F169}, {0x1F170, 0x1F18D}, {0x1F18F, 0x1F190},
	{0x1F19B, 0x1F1AC},
}
