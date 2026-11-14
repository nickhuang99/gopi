package lineedit

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sgr"
)

// palette_status_salience_test.go — the regression guard for the measured
// defect of task 01a0dacf ("the status row is so dim it is hardly visible"), and
// for its correction by task 01a0e2e6.
//
// THE MEASURED CAUSE (2026-09-26, captured from a real terminal on a real
// display): the status row painted TWO theme-dependent colours, and the user's
// theme resolved both to a mid grey. SGR 90 (the label's "bright black") is
// remapped by the palette, and SGR 2 (the live hint's "dim") is a blend defined
// by the terminal — measured on this machine's own gnome-terminal theme against
// a #300A24 background, both landed at 4.39:1 with only 36.3% of the glyph's
// pixels at the band's maximum contrast, while uncoloured text on the same
// screen measured 17.58:1. The bytes carry no defect; the CODES are the defect,
// because neither code pins what the terminal will draw.
//
// WHAT THE FIRST FIX GOT WRONG. 80a53890 read that measurement as "the row may
// not be coloured" and left SGR 39 — the terminal's DEFAULT foreground — on it.
// 39 is a colour too, and the one the operator reported as missing: the row read
// as "default color" where the Hermes-derived design puts GOLD. The rule the
// measurement actually supports is narrower: the row may not carry a code the
// USER'S THEME RESOLVES (SGR 2, SGR 7, the 8/16 named colours — and not the
// default foreground either, which is the theme's own text colour), and it MUST
// carry the table's own gold, in the one spelling each tier can pin (SGR 33 named
// on the 8-colour floor, `38;5;n` on the 256 tier, `38;2;r;g;b` in the gold
// tables). Emphasis stays ATTRIBUTE-based (weight, slant, underline) and is added
// ON TOP of the colour, never instead of it.

// themeResolvedStatusCodes are the SGR openings the status row may NOT use:
//
//   - SGR 2 (dim), SGR 7 (reverse) and SGR 39 (the default foreground — the
//     THEME's own text colour, which is what the operator saw instead of gold) —
//     properties the terminal resolves against its own background, and 2 in
//     particular is a blend with the background, so it is faint by construction
//     on ANY theme (and on a light theme a blend toward a light background is
//     close to invisible);
//   - the eight named foregrounds (SGR 30-37) and their bright forms (SGR
//     90-97) — the codes every theme remaps, which is how SGR 90 came back as a
//     mid grey at 4.39:1;
//   - SGR 3 (italic), reasoning as above: an ATTRIBUTE the row has carried since
//     80a53890 — part of what that commit measured as legible, and not a colour
//     decision at all. It is also why this set cannot be tested by substring over
//     the row: "3" lives inside "33", the named gold the floor now paints. The
//     guard parses the row's sequences into parameters and classifies each
//     COLOUR selection on its own (see statusRowColourSpellings below), so the
//     attribute and the colour are never confused.
//
// The list is deliberately spelled from the SAME named constants the palette
// emits (Rule 13: a constant is referenced, never mirrored), so a future edit to
// an emitter cannot leave this guard checking a stale literal.
//
// SGR 33 (the named gold) is deliberately ABSENT: it is the one named code the row
// may carry, on the floor tier that can pin no other spelling (see statusGold). A
// row that carries it where a numbered spelling exists is caught by the per-tier
// spelling assertion (TestStatusRowColourIsGoldAndNotThemeResolved and the two
// tables' own guards), which requires the 256 / truecolour form — so the
// exception cannot leak upward into a tier that has a better spelling.
//
// It is a list of CODES, not of mean colours: the status row may — and now does —
// carry a colour, as long as that colour is one of the numbered spellings the
// theme cannot reinterpret (or the single named gold the floor tier can pin — see
// statusGold).
func themeResolvedStatusCodes() map[string]string {
	codes := map[string]string{}
	for name, seq := range map[string]string{
		"sgrDim":       sgrDim,
		"sgrReverse":   sgrReverse,
		"sgrBlack":     sgrBlack,
		"sgrRed":       sgrRed,
		"sgrGreen":     sgrGreen,
		"sgrBlue":      sgrBlue,
		"sgrMagenta":   sgrMagenta,
		"sgrCyan":      sgrCyan,
		"sgrGray":      sgrGray,
		"sgrDefaultFg": escCSI + strconv.Itoa(sgr.DefaultFg) + "m",
	} {
		codes[seq] = name
	}
	return codes
}

// statusRowGoldSeq is the sequence the FLOOR table must paint on the row: the
// named gold (SGR 33), which is what BasicPalette emits — the constant itself, so
// a change to the floor's gold moves the assertion with it (Rule 13/14).
func statusRowGoldSeq() string { return statusGold }

// TestStatusRowCarriesNoThemeResolvedColour is the regression guard: for every
// status-row state the chat can be in (idle, thinking, tool running, narrow
// terminal, no live hint at all), the composed row is scanned for the SGR codes
// whose rendering the user's terminal theme decides.
func TestStatusRowCarriesNoThemeResolvedColour(t *testing.T) {
	banned := themeResolvedStatusCodes()
	label := "model-x" + StatusSep + "turn 2" + StatusSep + ToolGlyph + " bash ls" + StatusSep + "12 tokens"
	states := []struct {
		name string
		st   Status
		cols int
	}{
		{"idle", Status{Label: label, Live: "ready"}, 200},
		{"thinking", Status{Label: label, Live: "thinking…", Thinking: true}, 200},
		{"no-live", Status{Label: label}, 200},
		{"narrow", Status{Label: label, Live: "ready"}, 12},
		{"long-tool", Status{Label: "m" + StatusSep + ToolGlyph + " read /home/user/work/GoPi/internal/lineedit/palette.go", Live: "ready", Thinking: true}, 200},
	}
	for _, tc := range states {
		row := styledStatusLine(tc.st, tc.cols, BasicPalette)
		// The ban is applied to the row's CODES (never a substring over the row:
		// "3" lives inside "33", so a substring test cannot tell italic from the
		// named gold — see statusRowSGRCodes).
		for _, code := range statusRowSGRCodes(row) {
			if name, bad := banned[escCSI+code+"m"]; bad {
				t.Errorf("status row (%s) carries %s (SGR %s) — the terminal's theme decides how that renders, and it measured faint on the theme this defect was reported from:\n%q",
					tc.name, name, code, row)
			}
		}
		// The row must carry the table's GOLD in every state — a guard that only
		// forbids codes would be satisfied by the SGR 39 row the operator
		// complained about, and the states below are the five the chat can be in.
		for _, spelling := range statusRowColourSpellings(row) {
			if spelling != floorGoldToken() {
				t.Errorf("status row (%s) selects %s, want the row's own named gold %s — any other colour here is one the theme resolves:\n%q",
					tc.name, spelling, floorGoldToken(), row)
			}
		}
		if len(statusRowColourSpellings(row)) == 0 {
			t.Errorf("status row (%s) selects no colour at all — the operator sees the terminal's default text colour:\n%q", tc.name, row)
		}
		// ...and it must still be EMPHASISED: an uncoloured, unbolded row is the
		// other way to render nothing.
		if !strings.Contains(row, escCSI) {
			t.Errorf("status row (%s) carries no emphasis at all:\n%q", tc.name, row)
		}
	}
}

// TestStatusRowEmphasisSurvivesTheColour asserts the property the fix actually
// buys, in the one form a test can check without a terminal: the row's emphasis is
// carried by ATTRIBUTES that the SGR sequence names explicitly — so its strength
// does not depend on which colour the theme picked — AND the four row states are
// distinguishable from each other by attribute set AND colour, not merely by
// name.
func TestStatusRowEmphasisSurvivesTheColour(t *testing.T) {
	row := styledStatusLine(Status{Label: "model-x" + StatusSep + ToolGlyph + " bash ls", Live: "ready", Thinking: true}, 200, BasicPalette)
	if !strings.Contains(row, sgrBold) {
		t.Fatalf("the status row carries no bold — the label's emphasis would again be a colour choice:\n%q", row)
	}
	if !strings.Contains(row, sgrUnder) {
		t.Fatalf("the tool-activity row carries no underline — the row would be distinguished by colour alone:\n%q", row)
	}
	// The distinctions the row makes must remain visible as DIFFERENT sequences,
	// not merely as four names (the epic's "emphasis is legible as emphasis"
	// clause) — and each of them must carry the table's gold, so "different" is
	// not achieved by leaving one state uncoloured.
	tool := BasicPalette.ToolRow.Open()
	live := BasicPalette.StatusLive.Open()
	label := BasicPalette.StatusLabel.Open()
	thinking := BasicPalette.Thinking.Open()
	seen := map[string]string{}
	for name, seq := range map[string]string{"toolRow": tool, "statusLive": live, "statusLabel": label, "thinking": thinking} {
		if other, dup := seen[seq]; dup {
			t.Errorf("%s and %s emit the same sequence (%q) — the two states would be indistinguishable", name, other, seq)
		}
		seen[seq] = name
		if !strings.Contains(seq, statusGold) {
			t.Errorf("%s = %q carries no gold (%q) — a row state that is not gold is the default-colour row again", name, seq, statusGold)
		}
	}
}

// TestStatusRowSalienceIsAttributeBased asserts the ROW the fix produces carries
// the attribute openings the palette names — so the emphasis is in the BYTES, not
// merely in the table (a table whose fields no composition reads would pass every
// other test in this file) — and, since task 01a0e2e6, the gold with them.
func TestStatusRowSalienceIsAttributeBased(t *testing.T) {
	row := styledStatusLine(Status{
		Label:    "glm-5.3-flash" + StatusSep + "turn 2" + StatusSep + ToolGlyph + " bash git diff" + StatusSep + "1.2k tokens",
		Live:     "thinking…",
		Thinking: true,
	}, 100, BasicPalette)
	for name, want := range map[string]string{
		"bold":   sgrBold,
		"italic": sgrItalic,
		"under":  sgrUnder,
		"gold":   statusGold,
	} {
		if !strings.Contains(row, want) {
			t.Errorf("the composed status row carries no %s (%q):\n%q", name, want, row)
		}
	}
	// ...and the row is still EXACTLY its text once the SGR bytes are stripped:
	// the fix may not have moved a single cell.
	plain := Status{
		Label: "glm-5.3-flash" + StatusSep + "turn 2" + StatusSep + ToolGlyph + " bash git diff" + StatusSep + "1.2k tokens",
		Live:  "thinking…", Thinking: true,
	}
	if stripSGR(row) != statusLine(plain, 100) {
		t.Fatalf("the emphasised row's TEXT differs from the plain row:\n got %q\nwant %q", stripSGR(row), statusLine(plain, 100))
	}
}

// ---------------------------------------------------------------------------
// The colour each tier must paint the status row with, read from the ROW's own
// bytes.
// ---------------------------------------------------------------------------

// statusRowColourSequences returns the SGR colour selections the row's bytes
// carry, each as the parameter list between the introducer and the final `m`
// (e.g. "1", "33", "38;5;220", "1;3;38;2;218;165;32").
//
// It SCANS the row rather than reading the palette, and it scans by SEQUENCE
// rather than by substring: "33" is the named gold and "3" is italic, so a
// strings.Contains guard cannot tell a ban on the code from a ban on the digits
// (the two live in the same bytes). The colour decision each sequence makes is
// then classified from its own parameters — the same way a terminal reads it.
func statusRowColourSequences(row string) []string {
	var seqs []string
	for i := 0; i+len(escCSI) <= len(row); {
		if !strings.HasPrefix(row[i:], escCSI) {
			i++
			continue
		}
		j := i + len(escCSI)
		for j < len(row) && row[j] != 'm' {
			j++
		}
		if j >= len(row) {
			break
		}
		seqs = append(seqs, row[i+len(escCSI):j])
		i = j + 1
	}
	return seqs
}

// statusRowSGRCodes returns the row's SGR codes that carry EXACTLY one parameter
// ("1", "2", "7", "33", "90", "39"), one per such sequence. It is how a CODE list
// is matched against codes: an extended colour (`38;5;220`) has more than one
// parameter and is classified by statusRowColourSpellings instead, and no guard
// ever substring-matches digits inside a longer sequence — which is what keeps
// italic's "3" and the floor's gold "33" apart.
func statusRowSGRCodes(row string) []string {
	var codes []string
	for _, seq := range statusRowColourSequences(row) {
		if !strings.Contains(seq, string(sgr.ParamSeparator)) {
			codes = append(codes, seq)
		}
	}
	return codes
}

// sgrColourParam reports whether one SGR parameter selects a foreground colour,
// and in which spelling: "named" for the 8/16 named codes (30-37, 90-97), "def"
// for the default foreground (39), "256" or "truecolor" for the extended forms
// (38;5;n / 38;2;r;g;b), and "" for an attribute — Dim and Reverse are attributes
// of the TERMINAL's background decision rather than foreground selections, so they
// are left to the exact-code scan.
func sgrColourParam(params []int, i int) string {
	if i >= len(params) {
		return ""
	}
	p := params[i]
	switch {
	case p >= sgr.FgBase && p <= sgr.FgLast:
		return "named:" + strconv.Itoa(p)
	case p >= sgr.BrightFgBase && p <= sgr.BrightFgLast:
		return "named:" + strconv.Itoa(p)
	case p == sgr.DefaultFg:
		return "def"
	case p == sgr.ExtendedFg && i+1 < len(params):
		switch params[i+1] {
		case sgr.ColorModePalette:
			return "256"
		case sgr.ColorModeTruecolor:
			return "truecolor"
		}
	}
	return ""
}

// statusRowColourSpellings returns, per colour-selecting sequence on the row, a
// token that NAMES the colour it selects: `256:220`, `truecolor:218;165;32` or
// `named`/`def` for the theme-resolved spellings. It is what "the row is gold, in
// a spelling the terminal cannot reinterpret" is asserted against, read from the
// row's own bytes rather than from the table.
func statusRowColourSpellings(row string) []string {
	var out []string
	for _, seq := range statusRowColourSequences(row) {
		var params []int
		for _, f := range strings.Split(seq, string(sgr.ParamSeparator)) {
			n, err := strconv.Atoi(f)
			if err != nil {
				continue
			}
			params = append(params, n)
		}
		for i := 0; i < len(params); i++ {
			kind := sgrColourParam(params, i)
			if kind == "" {
				continue
			}
			switch kind {
			case "256":
				out = append(out, kind+":"+strconv.Itoa(params[i+2]))
				i += 2
			case "truecolor":
				out = append(out, kind+":"+strings.Join(intsToStrings(params[i+2:min(i+5, len(params))]), ";"))
				i += 4
			default:
				out = append(out, kind)
			}
		}
	}
	return out
}

// intsToStrings renders SGR parameters for a token (a token is read in a failure
// message, where "truecolor:218;165;32" says which colour was painted).
func intsToStrings(v []int) []string {
	out := make([]string, 0, len(v))
	for _, n := range v {
		out = append(out, strconv.Itoa(n))
	}
	return out
}

// statusRowState is the one canonical row the colour assertions are stated
// against: the production shape (five label fields, a tool glyph, counters) with
// the in-flight live hint, which is the state whose colour the operator reads
// while the agent works.
func statusRowState() Status {
	return Status{
		Label:    "glm-5.3-flash" + StatusSep + "turn 2" + StatusSep + ToolGlyph + " bash git diff" + StatusSep + "1.2k tokens",
		Live:     "thinking…",
		Thinking: true,
	}
}

// floorGoldToken is the token the FLOOR table's gold must appear as, derived
// from the palette's own statusGold constant (Rule 13: reference, never mirror),
// so a change to the floor's gold moves the assertion with it.
func floorGoldToken() string {
	return "named:" + strings.TrimSuffix(strings.TrimPrefix(statusGold, escCSI), "m")
}

// cubeColourToken / truecolorToken spell the token
// statusRowColourSpellings reports, so a case's expectation is composed the same
// way the reader composes what it observes (never a hand-typed "256:220" that
// could drift from the constant).
func cubeColourToken(idx int) string { return "256:" + strconv.Itoa(idx) }

func truecolorToken(rgb int) string {
	return "truecolor:" + strings.Join(intsToStrings([]int{
		rgb >> rgbRedShift & 0xFF, rgb >> rgbGreenShift & 0xFF, rgb & 0xFF,
	}), ";")
}

// TestStatusRowColourIsGoldAndNotThemeResolved is the epic's acceptance, stated
// per TIER and read from the ROW: whatever table is in use, the status row paints
// a gold the terminal cannot reinterpret, and it carries none of the codes whose
// rendering the user's theme decides.
//
// The two halves are different claims and both are made here:
//
//   - the gold is PRESENT, in the spelling the tier can pin — `38;5;n` on the 256
//     tier and `38;2;r;g;b` in the gold tables, both colour SPACES with fixed
//     values (the xterm cube and sRGB), and the named gold on the 8-colour floor,
//     where neither numbered spelling exists. wantColours is the COMPLETE set, so
//     a row that painted two of its three states gold and one grey fails;
//   - the banned set — SGR 2/7, the named codes other than the floor's gold, the
//     default foreground — is read from the same bytes: a table that pinned the
//     gold AND smuggled a grey back in would fail here.
func TestStatusRowColourIsGoldAndNotThemeResolved(t *testing.T) {
	cases := []struct {
		name string
		pal  Palette
		// wantColours is EVERY colour the row must select, as tokens: the set is
		// compared, not a membership, because one of the row's four states
		// falling back to default is exactly the defect being fixed.
		wantColours []string
	}{
		// The floor: one named gold for the whole row (see statusGold).
		{"BasicPalette", BasicPalette, []string{floorGoldToken()}},
		// The 256 tier: one cube gold for the whole row (see Palette256).
		{"Palette256", Palette256, []string{cubeColourToken(cubeGoldAccent)}},
		// The gold tables: Hermes' own roles — the label's goldenrod, primary
		// for the live/tool row, muted for the thinking state.
		{"HermesDarkPalette", HermesDarkPalette, []string{
			truecolorToken(hermesDarkLabel), truecolorToken(hermesDarkPrimary), truecolorToken(hermesDarkMuted),
		}},
		{"HermesLightPalette", HermesLightPalette, []string{
			truecolorToken(hermesLightLabel), truecolorToken(hermesLightPrim),
		}},
	}
	banned := themeResolvedStatusCodes()
	// bannedNamedExceptGold is the ban list keyed by the BARE code (the sequence
	// minus its introducer and `m`), which is how the scan below reports one. SGR
	// 33 is absent for the same reason it is absent from the list itself: the
	// floor may carry it (see statusGold), and no other tier does — which the
	// per-tier spelling assertion below proves, so the exception cannot leak
	// upward into a tier that has a numbered spelling.
	bannedNamedExceptGold := map[string]string{}
	for seq, name := range banned {
		bannedNamedExceptGold[strings.TrimSuffix(strings.TrimPrefix(seq, escCSI), "m")] = name
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := styledStatusLine(statusRowState(), 200, tc.pal)
			spellings := statusRowColourSpellings(row)
			if len(spellings) == 0 {
				t.Fatalf("%s: the status row selects no colour at all — the operator sees the terminal's default text colour again:\n%q", tc.name, row)
			}
			got := map[string]bool{}
			for _, s := range spellings {
				got[s] = true
			}
			for _, want := range tc.wantColours {
				if !got[want] {
					t.Errorf("%s: the status row does not select %s — one of its states is not gold:\n%q", tc.name, want, row)
				}
			}
			for s := range got {
				known := false
				for _, want := range tc.wantColours {
					if s == want {
						known = true
					}
				}
				if !known {
					t.Errorf("%s: the status row selects %s, which is not one of its table's golds %v:\n%q", tc.name, s, tc.wantColours, row)
				}
			}
			// The banned set, read from the row's own CODES: exact single-parameter
			// codes only, so italic's "3" can never be read as a prefix of the
			// floor's gold "33" (and the named-code ban is not applied to the
			// floor's own gold).
			for _, code := range statusRowSGRCodes(row) {
				if name, bad := bannedNamedExceptGold[code]; bad {
					t.Errorf("%s: the status row carries %s (SGR %s) — its rendering is the theme's decision:\n%q",
						tc.name, name, code, row)
				}
			}
			// The row's ATTRIBUTES survive the colour (the epic keeps 80a53890's
			// emphasis): bold and underline are on it, and the TEXT is the plain
			// row's, so nothing moved a cell.
			if !strings.Contains(row, sgrBold) || !strings.Contains(row, sgrUnder) {
				t.Errorf("%s: the status row lost its attribute emphasis (bold/underline):\n%q", tc.name, row)
			}
			if stripSGR(row) != statusLine(statusRowState(), 200) {
				t.Errorf("%s: the status row's TEXT differs from the plain row:\n got %q\nwant %q",
					tc.name, stripSGR(row), statusLine(statusRowState(), 200))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The salience invariant on the EDITING ROW (C1's reversal).
// ---------------------------------------------------------------------------

// TestEditingRowCarriesNoThemeResolvedColour applies the SAME invariant this file
// states for the status row to the surface the C1 reversal turned on: the editing
// line's BUFFER bytes.
//
// The two rows are one decision, so they are guarded by one vocabulary: the banned
// set is themeResolvedStatusCodes() — the named 8/16-colour codes and the default
// foreground, minus the single named gold the floor tier may pin — and the
// spelling expected per tier is the one the TABLE emits, read back through statusRowColourSpellings.
//
// The row is composed by the RENDERER (rendererRowBytes), not by hand, so what is
// scanned is the bytes a terminal receives. The comparison is made on the BUFFER
// REGION's own colour selections: the prompt still carries the named green on the
// floor tier (a chrome accent, untouched by this change), so the assertion is
// stated per region rather than over the whole row — scanning the whole row would
// either fail on the prompt's legitimate green or force the prompt to be
// re-derived, and neither is what the invariant is about.
func TestEditingRowCarriesNoThemeResolvedColour(t *testing.T) {
	const typed = "why did the row shear?"
	banned := themeResolvedStatusCodes()
	bannedNamedExceptGold := map[string]string{}
	for seq, name := range banned {
		bannedNamedExceptGold[strings.TrimSuffix(strings.TrimPrefix(seq, escCSI), "m")] = name
	}

	for _, tc := range editingRowCases() {
		t.Run(tc.name, func(t *testing.T) {
			row := rendererRowBytes(t, tc.pal, typed)
			buffer := bufferRegionBytes(t, row, typed)
			if strings.TrimSpace(stripSGR(buffer)) == "" {
				t.Fatalf("%s: the buffer region carries no text — the scan would be vacuous:\n%q", tc.name, row)
			}

			// 1. NO CODE THE THEME RESOLVES: exact single-parameter codes only, so
			// italic's "3" can never be read as a prefix of the floor's gold "33".
			for _, code := range statusRowSGRCodes(buffer) {
				if name, bad := bannedNamedExceptGold[code]; bad {
					t.Errorf("%s: the editing row's buffer carries %s (SGR %s) — its rendering is the theme's decision, which is the defect C1 was reversed for:\n%q",
						tc.name, name, code, buffer)
				}
			}

			// 2. ...AND IT IS STILL GOLD: a row that carried no colour at all
			// would satisfy (1) and be the default-coloured input again.
			spellings := statusRowColourSpellings(buffer)
			if len(spellings) == 0 {
				t.Fatalf("%s: the buffer region selects no colour at all — the operator sees the terminal's default text colour again:\n%q", tc.name, buffer)
			}
			// The spelling must be the one THIS table paints: the same tokens the
			// status-row guard uses for the same tier, derived from the constants
			// rather than typed here (Rule 13).
			for _, want := range editingRowGoldTokens(t, tc.pal) {
				if !slices.Contains(spellings, want) {
					t.Errorf("%s: the buffer region does not select %s (it selects %v) — the editing line is not the gold this table names:\n%q",
						tc.name, want, spellings, buffer)
				}
			}

			// 3. The TEXT did not move: the styling is on top of the same glyphs.
			if got := stripSGR(row); !strings.Contains(got, typed) {
				t.Errorf("%s: the styled editing row lost its text:\n got %q", tc.name, got)
			}
		})
	}
}

// editingRowGoldTokens is the set of spellings the given table's buffer may
// select — composed from the SAME token builders the status-row cases use, so the
// two rows' expectations cannot drift (Rule 14: one composition).
func editingRowGoldTokens(t *testing.T, pal Palette) []string {
	switch {
	case pal.Buffer.Open() == BasicPalette.Buffer.Open():
		return []string{floorGoldToken()}
	case pal.Buffer.Open() == Palette256.Buffer.Open():
		return []string{cubeColourToken(cubeGoldAccent)}
	default:
		// Both gold tables paint a 24-bit value; which one is read back from the
		// table's own Buffer field rather than guessed from its name.
		st := declaredStyle(t, pal.Buffer, "Buffer")
		return []string{truecolorToken(st.FgRGB)}
	}
}

// rendererRowBytes draws one editing line through the REAL renderer (the same
// redrawEditingLine helper the cell-grid assertion uses — one drawing path, not a
// second one written for this guard) at the capture's own geometry, and returns
// the bytes it wrote.
func rendererRowBytes(t *testing.T, pal Palette, typed string) string {
	t.Helper()
	return redrawEditingLine(t, pal, typed, captureCols, paletteTestRows)
}

// bufferRegionBytes cuts the editing row's byte string down to the buffer's own
// region: the SGR sequence that opens the user's span (the last sequence before
// the typed text), the text itself, and the reset that closes it.
//
// The prompt's own bytes are DROPPED, so what is scanned is the buffer's colour
// decision alone: the prompt legitimately carries the named green on the floor
// tier, and a scan of the whole row would either fail on that green or force the
// prompt's own accent to be re-derived here — neither is what this invariant is
// about. The cut is located by the TYPED TEXT (which is user input and carries no
// escape bytes), so it cannot drift with the layout.
func bufferRegionBytes(t *testing.T, row, typed string) string {
	t.Helper()
	idx := strings.Index(row, typed)
	if idx < 0 {
		t.Fatalf("the typed text %q is not in the drawn row — the scan would be vacuous:\n%q", typed, row)
	}
	start := strings.LastIndex(row[:idx], escCSI)
	if start < 0 {
		// No sequence opens the buffer's span: the scan then covers the bare
		// text, and the caller's "selects no colour at all" assertion is the one
		// that fires.
		start = idx
	}
	region := row[start:]
	end := idx - start + len(typed)
	if j := strings.Index(region[end:], sgrReset); j >= 0 {
		end += j + len(sgrReset)
	}
	return region[:end]
}
