package config

// ambiguous_width_test.go — the A class's two halves, asserted where they are
// decided: the TABLE is the right set of runes, and the WIDTH is the environment's
// answer rather than a constant.

import (
	"os"
	"strings"
	"testing"
)

// TestAmbiguousRangesAreOrderedAndBounded pins the SHAPE of the table, because
// IsAmbiguousRune's binary search is only correct on a sorted, disjoint list —
// an edit that appends a range out of order would make the search silently miss
// runes rather than fail (Rule 15: no silent wrong answer). The count is pinned
// too: the table is generated, not grown by hand, so a change in its size is a
// regeneration and should be a deliberate one.
func TestAmbiguousRangesAreOrderedAndBounded(t *testing.T) {
	if got, want := len(ambiguousRanges), ambiguousRangeCount; got != want {
		t.Errorf("ambiguousRanges has %d ranges, want %d — the table is generated; regenerate it "+
			"with the command in ambiguous_width.go's header rather than editing the count", got, want)
	}
	for i, rg := range ambiguousRanges {
		if rg.lo > rg.hi {
			t.Errorf("range %d: lo %#x > hi %#x", i, rg.lo, rg.hi)
		}
		if i == 0 {
			continue
		}
		if prev := ambiguousRanges[i-1]; rg.lo <= prev.hi {
			t.Errorf("range %d [%#x,%#x] overlaps or touches its predecessor [%#x,%#x] — the search "+
				"needs the list sorted, disjoint and coalesced", i, rg.lo, rg.hi, prev.lo, prev.hi)
		}
	}
}

// TestIsAmbiguousRuneIsTheEastAsianAmbiguousClass is the table's meaning, asserted
// on members the defect is about and on the neighbours that must NOT be captured:
// a table that swallowed the W/F blocks would make the two classes agree for the
// wrong reason, and one that missed box-drawing would leave the reported bug live.
func TestIsAmbiguousRuneIsTheEastAsianAmbiguousClass(t *testing.T) {
	ambiguous := []struct {
		name string
		r    rune
	}{
		{"box drawing light horizontal", '─'},  // U+2500 — the reported defect
		{"box drawing light vertical", '│'},    // U+2502
		{"box drawing double horizontal", '═'}, // U+2550
		{"box drawing heavy horizontal", '━'},  // U+2501
		{"block half left", '▌'},               // U+258C — gopi's own TurnMarker
		{"em dash", '—'},                       // U+2014
		{"middle dot", '·'},                    // U+00B7
		{"rightwards arrow", '→'},              // U+2192
		{"circled digit one", '①'},             // U+2460
		{"greek capital alpha", 'Α'},           // U+0391 (Greek is A, not W)
	}
	for _, c := range ambiguous {
		if !IsAmbiguousRune(c.r) {
			t.Errorf("IsAmbiguousRune(%s %#x) = false, want true", c.name, c.r)
		}
	}
	narrow := []struct {
		name string
		r    rune
	}{
		{"ascii a", 'a'},
		{"space", ' '},
		{"cjk ideograph", '中'},              // U+4E00 — W, owned by the W/F tables
		{"fullwidth A", 'Ａ'},                // U+FF21 — F
		{"hiragana a", 'あ'},                 // U+3042 — W
		{"private use", '\uE000'},           // excluded: renders 1 cell even in CJK
		{"variation selector 16", '\uFE0F'}, // excluded (combining anyway)
	}
	for _, c := range narrow {
		if IsAmbiguousRune(c.r) {
			t.Errorf("IsAmbiguousRune(%s %#x) = true, want false", c.name, c.r)
		}
	}
}

// TestAmbiguousWidthEnvOutranksLocaleAndPublishesOnce is Rule 2 on this knob: the
// env channel wins over the locale, both named values are understood, an empty
// value is "nobody asked" (the locale decides), and the settings/flag channel
// reaches the reader only by being published into that one env name.
func TestAmbiguousWidthEnvOutranksLocaleAndPublishesOnce(t *testing.T) {
	// The reader is the CALLER's environment accessor (which is what keeps this
	// function testable without the process env): locale variables are answered
	// here, everything else — including the knob itself — falls through to the
	// real environment, where t.Setenv pinned it.
	cjk := func(k string) string {
		if k == "LC_ALL" {
			return "zh_CN.UTF-8"
		}
		return os.Getenv(k)
	}
	t.Setenv(AmbiguousWidthEnv, AmbiguousWidthNarrow)
	if got := ResolveAmbiguousWidthMode(cjk); got != AmbiguousWidthNarrow {
		t.Errorf("env=narrow with a CJK locale: got %q, want %q (the env channel outranks the locale)", got, AmbiguousWidthNarrow)
	}
	t.Setenv(AmbiguousWidthEnv, AmbiguousWidthWide)
	if got := ResolveAmbiguousWidthMode(cjk); got != AmbiguousWidthWide {
		t.Errorf("env=wide: got %q, want %q", got, AmbiguousWidthWide)
	}
	t.Setenv(AmbiguousWidthEnv, "")
	if got := ResolveAmbiguousWidthMode(cjk); got != AmbiguousWidthWide {
		t.Errorf("env unset with a CJK locale: got %q, want %q (the locale decides)", got, AmbiguousWidthWide)
	}

	// The published channel: ApplyEnvAmbiguousWidth is the writer the settings
	// and flag paths share, so a settings value must arrive as the env value.
	ApplyEnvAmbiguousWidth(AmbiguousWidthNarrow)
	if got := ResolveAmbiguousWidthMode(cjk); got != AmbiguousWidthNarrow {
		t.Errorf("after ApplyEnvAmbiguousWidth(narrow): got %q, want %q", got, AmbiguousWidthNarrow)
	}
	t.Setenv(AmbiguousWidthEnv, "")
}

// TestAmbiguousWidthLocalePrecedence is the C library's own order, asserted in the
// three positions that can disagree: LC_ALL beats LC_CTYPE, LC_CTYPE beats LANG,
// and any one of them is enough to make the environment East Asian.
func TestAmbiguousWidthLocalePrecedence(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
		why  string
	}{
		{"lc_all C beats a CJK lang", map[string]string{"LC_ALL": "C", "LANG": "zh_CN.UTF-8"}, AmbiguousWidthNarrow, "LC_ALL is the strongest variable"},
		{"lc_ctype C beats a CJK lang", map[string]string{"LC_CTYPE": "C", "LANG": "zh_CN.UTF-8"}, AmbiguousWidthNarrow, "LC_CTYPE outranks LANG"},
		{"lc_all unset, cjk ctype", map[string]string{"LC_ALL": "", "LC_CTYPE": "ja_JP.eucJP"}, AmbiguousWidthWide, "an empty variable is not set"},
		{"only lang", map[string]string{"LANG": "ko_KR.UTF-8"}, AmbiguousWidthWide, "the last variable still decides"},
		{"western lang", map[string]string{"LANG": "en_US.UTF-8"}, AmbiguousWidthNarrow, "an installed Western locale is not East Asian"},
		{"c locale", map[string]string{"LANG": "C"}, AmbiguousWidthNarrow, "the C locale IS the Western reading"},
		{"nothing set", map[string]string{}, AmbiguousWidthNarrow, "the built-in default is narrow"},
	}
	for _, c := range cases {
		// A locale-only question: the knob is answered as unset so the locale is
		// the channel under test, and the case cannot be decided by an ambient
		// GOPI_AMBIGUOUS_WIDTH this host happens to carry.
		read := func(k string) string {
			if k == AmbiguousWidthEnv {
				return ""
			}
			return c.env[k]
		}
		if got := ResolveAmbiguousWidthMode(read); got != c.want {
			t.Errorf("%s: got %q, want %q — %s", c.name, got, c.want, c.why)
		}
	}
}

// TestUnknownAmbiguousWidthIsReportedAndTheNextChannelDecides is Rule 15's arm: a
// value gopi does not understand must not look like "nobody asked" — the operator
// is told, and the locale still decides. The report is once per distinct value
// (this resolver runs per rune of every drawn row), which is what makes a
// per-value warning audible rather than noise.
func TestUnknownAmbiguousWidthIsReportedAndTheNextChannelDecides(t *testing.T) {
	var buf strings.Builder
	ambiguousWidthWarnMu.Lock()
	ambiguousWidthWarned = map[string]bool{}
	ambiguousWidthWarnOut = &buf
	ambiguousWidthWarnMu.Unlock()
	t.Cleanup(func() {
		ambiguousWidthWarnMu.Lock()
		ambiguousWidthWarnOut = nil
		ambiguousWidthWarnMu.Unlock()
	})

	read := func(k string) string {
		if k == AmbiguousWidthEnv {
			return "double"
		}
		return ""
	}
	if got := ResolveAmbiguousWidthMode(read); got != AmbiguousWidthNarrow {
		t.Errorf("a bogus value: got %q, want %q (the next channel decides)", got, AmbiguousWidthNarrow)
	}
	ResolveAmbiguousWidthMode(read)
	if got, want := strings.Count(buf.String(), AmbiguousWidthEnv), 1; got != want {
		t.Errorf("the refusal was printed %d times for one value, want %d (per-rune callers must not "+
			"flood the terminal): %q", got, want, buf.String())
	}
	if !strings.Contains(buf.String(), AmbiguousWidthWide) || !strings.Contains(buf.String(), AmbiguousWidthNarrow) {
		t.Errorf("the refusal %q does not name both understood values", buf.String())
	}
}

// TestIsAmbiguousWideReadsTheProcessEnvironment is the bridge the width tables
// call: the class predicate plus the resolved mode, over the real environment.
func TestIsAmbiguousWideReadsTheProcessEnvironment(t *testing.T) {
	t.Setenv(AmbiguousWidthEnv, AmbiguousWidthWide)
	if !IsAmbiguousWide('─') {
		t.Error("IsAmbiguousWide('─') = false with the env pinned wide")
	}
	t.Setenv(AmbiguousWidthEnv, AmbiguousWidthNarrow)
	if IsAmbiguousWide('─') {
		t.Error("IsAmbiguousWide('─') = true with the env pinned narrow")
	}
	if IsAmbiguousWide('中') {
		t.Error("IsAmbiguousWide('中') = true — W is not this predicate's business")
	}
}
