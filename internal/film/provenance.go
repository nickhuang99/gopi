package film

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/replay"
)

// This file is verify.py's TEXT LAYER, ported: what a beat's text IS, what the
// surface drew for it, and whether the two meet. Every function names the
// verify.py line range it came from; every divergence is named with its reason.

// ── the renderer's own constants, REFERENCED not re-typed (Rule 14) ──────────
//
// verify.py re-read these out of the Go source at import time
// (`read_tool_glyph`, and the pinning test tools/replay/test_verify.py), because
// a Python checker could not import a Go package. Go can, so the port takes them
// from their ONE definition instead of carrying a copy that could drift:
//
//	lineedit.ToolGlyph        — the activity row's marker (verify.py L311-322)
//	replay.DigestKeyOrder     — digestArgs' key order (verify.py L172-174)
//	replay.ToolSummaryRunes   — the digest's cut (verify.py L171)
//	replay.ArgSep             — the digest's field separator
//	replay.Ellipsis           — the surface's cut marker
//	replay.Flatten            — the ledger's own whitespace fold
//	replay.CutRunes           — internal/replay's cutRunes (verify.py L556-L562)

// toolRowText composes an activity row's payload the way the SURFACE composes
// it: internal/replay's toolRowText (replay.go L469-L475), reproduction rather
// than paraphrase — the same glyph, the same digest key order, the same cut.
func toolRowText(name, rawInput string) string {
	digest := toolDigest(rawInput)
	if digest == "" {
		return lineedit.ToolGlyph + " " + name
	}
	return lineedit.ToolGlyph + " " + name + " " + digest
}

// toolDigest is verify.py's `tool_digest` (L563-L587): the first informative
// string-valued field, else the raw object cut to the cap. It never invents a
// value. This is internal/replay's digestArgs (replay.go L480-L502).
func toolDigest(rawInput string) string {
	if rawInput == "" {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(rawInput), &obj); err == nil {
		for _, key := range replay.DigestKeyOrder {
			v, ok := obj[key]
			if !ok {
				continue
			}
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return replay.CutRunes(strings.Join(strings.Fields(s), replay.ArgSep), replay.ToolSummaryRunes)
			}
		}
	}
	return replay.CutRunes(strings.Join(strings.Fields(rawInput), replay.ArgSep), replay.ToolSummaryRunes)
}

// goJSON is verify.py's `go_json` (L588-L597): a value encoded the way the
// renderer's own encoder would encode it. Go's encoding/json already escapes
// `<`, `>` and `&` as \u003c/\u003e/\u0026, which IS the substitution the
// Python needs to make by hand, so this is a straight Marshal.
func goJSON(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(b)
}

// rawInputValue is one `"input":` value on a source line, as its RAW characters,
// with the offset it was found at.
type rawInputValue struct {
	At  int
	Raw string
}

// rawInputValues is verify.py's `raw_input_values` (L675-L729): every
// `"input":` value on a source line, as its RAW characters, with offsets.
//
// THE BYTES THE RENDERER RECEIVED, not a re-serialization of them: internal/replay
// hands digestArgs the recorded line's raw `input` (json.RawMessage), so the
// surface digests the SOURCE's own spelling — escapes and all. Re-encoding would
// normalize `\u003e` to `>` and manufacture a mismatch the film does not have.
//
// The offsets are BYTE offsets here (the Python uses character offsets); the
// pairing in composedToolNeedles uses them only against offsets computed the same
// way over the same string, which is what they are for.
func rawInputValues(line string) []rawInputValue {
	const inputKey = `"input"`
	var out []rawInputValue
	at := 0
	for {
		i := strings.Index(line[at:], inputKey)
		if i < 0 {
			return out
		}
		at += i
		colon := strings.Index(line[at+len(inputKey):], ":")
		if colon < 0 {
			return out
		}
		colon = at + len(inputKey) + colon
		j := colon + 1
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		if j < len(line) && (line[j] == '{' || line[j] == '[') {
			depth, inStr, esc, k := 0, false, false, j
			for k < len(line) {
				c := line[k]
				switch {
				case inStr:
					if esc {
						esc = false
					} else if c == '\\' {
						esc = true
					} else if c == '"' {
						inStr = false
					}
				case c == '"':
					inStr = true
				case c == '{' || c == '[':
					depth++
				case c == '}' || c == ']':
					depth--
					if depth == 0 {
						k++
						out = append(out, rawInputValue{At: at, Raw: line[j:k]})
						goto next
					}
				}
				k++
			}
		}
	next:
		at = colon + 1
	}
}

// composedToolNeedles is verify.py's `composed_tool_needles` (L730-L773): every
// activity row the tool calls on a source line compose to.
//
// Each needle is composed OUT OF THE BLOCK'S OWN RAW INPUT — the characters the
// renderer digests — so the needle is what a faithful film would have drawn for
// THAT call, not a guess at it. The raw value is paired with its call by OFFSET,
// never by counting: the recorder writes a block's own `input` immediately after
// its `id`, so the first `"input":` at or past the block's id is that block's.
func composedToolNeedles(raw string) []string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil
	}
	if obj == nil {
		return nil
	}
	msg, ok := obj["message"].(map[string]any)
	if !ok {
		return nil
	}
	blocks, ok := msg["content"].([]any)
	if !ok {
		return nil
	}
	raws := rawInputValues(raw)
	var out []string
	for _, b := range blocks {
		block, ok := b.(map[string]any)
		if !ok || block["type"] != "tool_use" {
			continue
		}
		id, _ := block["id"].(string)
		rawInput := ""
		if pat := `"id"\s*:\s*` + regexp.QuoteMeta(goJSON(id)); pat != "" {
			re, err := regexp.Compile(pat)
			if err == nil {
				loc := re.FindStringIndex(raw)
				if loc != nil {
					for _, v := range raws {
						if v.At >= loc[1] {
							rawInput = v.Raw
							break
						}
					}
				}
			}
		}
		if rawInput == "" {
			rawInput = goJSON(block["input"])
		}
		name, _ := block["name"].(string)
		out = append(out, toolRowText(name, rawInput))
	}
	return out
}

// sourceText is verify.py's `source_text` (L641-L674): everything a source
// line's JSON actually holds, as comparable strings — the text-valued leaves at
// any depth, each leaf on its own so a needle cannot match across two unrelated
// fields. Falling back to the raw line when the JSON holds no text.
func sourceText(obj map[string]any, raw string) []string {
	var out []string
	var walk func(node any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for _, val := range v {
				walk(val)
			}
		case []any:
			for _, val := range v {
				walk(val)
			}
		case string:
			if strings.TrimSpace(v) != "" {
				out = append(out, v)
			}
		}
	}
	if msg, has := obj["message"]; has {
		if m, ok := msg.(map[string]any); ok {
			walk(m["content"])
		} else {
			walk(msg)
		}
	} else {
		walk(obj)
	}
	if len(out) == 0 {
		out = append(out, raw)
	}
	return out
}

// textRun is a RUN of consecutive ledger rows: the chunks of ONE source line's
// one text block, in the order the surface received them.
//
// verify.py L495-L515 (`text_runs`). The ledger's rows are CHUNKS cut at a fixed
// rune count, not drawn units: the renderer receives the source text as a
// STREAM and re-lays it out as it arrives, so the thing whose visibility can be
// asked about is the RUN.
type textRun []LedgerRow

// textRuns is verify.py's `text_runs` (L495-L515).
func textRuns(rows []LedgerRow) []textRun {
	var runs []textRun
	var cur textRun
	for _, r := range rows {
		if len(cur) > 0 {
			last := cur[len(cur)-1]
			if r.SrcLine == last.SrcLine && r.Kind == last.Kind {
				cur = append(cur, r)
				continue
			}
		}
		if len(cur) > 0 {
			runs = append(runs, cur)
		}
		cur = textRun{r}
	}
	if len(cur) > 0 {
		runs = append(runs, cur)
	}
	return runs
}

// runText is verify.py's `run_text` (L516-L524): the run's chunks joined on
// their own bytes, with no separator invented between them — the renderer's
// stream is the concatenation.
func runText(run textRun) string {
	var b strings.Builder
	for _, r := range run {
		b.WriteString(r.Text)
	}
	return b.String()
}

// derivesFrom is verify.py's `derives_from` (L525-L555): whether text is one of
// the source's OWN texts, decoration set aside. Two acceptances — CONTAINED in a
// text leaf, or a PREFIX of one (the ledger's row budget draws the head of a
// block and stops). The comparison is on the decoration-stripped characters with
// whitespace INTACT.
func derivesFrom(text string, hayStrings []string) bool {
	b := bare(text)
	if b == "" {
		return false
	}
	for _, h := range hayStrings {
		hb := bare(h)
		if hb != "" && (strings.Contains(hb, b) || strings.HasPrefix(hb, b)) {
			return true
		}
	}
	return false
}

// probeOf is verify.py's `probe_of` (L460-L494): the beat's whole text as the
// surface was handed it, in comparable form. Returns ok=false when there is
// nothing to look for — an empty text, or one that reduces to structure only.
func probeOf(text string) (string, bool) {
	t := normalize(text)
	if t == "" {
		return "", false
	}
	structural := true
	for _, ch := range t {
		if ch != '-' && ch != '|' && ch != ':' && ch != ' ' {
			structural = false
			break
		}
	}
	if structural {
		return "", false
	}
	return t, true
}

// ── visibility: is the drawn text on the grid that beat photographed? ────────

// visibleOn is verify.py's `visible_on` (L914-L935): whether `probe` reached the
// screen, given the turn-end grid. Three acceptances, each a property of the
// surface rather than a concession.
func visibleOn(probe, flatText string, gridRows []string) bool {
	if strings.Contains(flatText, probe) {
		return true
	}
	if onOneDrawnLine(probe, gridRows) {
		return true
	}
	for _, row := range gridRows {
		n := normalize(row)
		if strings.Contains(n, probe) {
			return true
		}
		if strings.HasSuffix(strings.TrimRight(row, " \t"), ellipsis) && n != "" && runeLen(n) >= 2 &&
			strings.HasPrefix(n, prefixRunes(probe, 2)) && runeHasPrefix(probe, prefixRunes(probe, 2)) {
			if prefixRunes(probe, 2) != "" && strings.HasPrefix(n, prefixRunes(probe, 2)) {
				return true
			}
		}
	}
	return false
}

// onOneDrawnLine is verify.py's `on_one_drawn_line` (L774-L805): whether
// `needle` is inside ONE string, allowing for wrapped display. A grid row is a
// SLICE of the text the surface drew; the needle is rebuilt piece by piece as
// the rows are read, and that walk IS the test.
func onOneDrawnLine(needle string, hayStrings []string) bool {
	at := ""
	started := false
	for _, s := range hayStrings {
		n := normalize(s)
		if n == "" {
			continue
		}
		if !started {
			if strings.HasPrefix(needle, n) {
				if runeLen(n) >= runeLen(needle) {
					return true
				}
				at = n
				started = true
			}
			continue
		}
		if strings.HasPrefix(needle, at+n) {
			at += n
			if runeLen(at) >= runeLen(needle) {
				return true
			}
			continue
		}
		// The needle's continuation is not here: this row may be the needle's own
		// opening again (the live row redraws), a chrome row the wrap ran into, or
		// a re-wrap past the ellipsis. Start over from here.
		if strings.HasPrefix(needle, n) {
			at = n
		} else {
			at, started = "", false
		}
	}
	return false
}

// locateOnWrappedGrid is verify.py's `locate_on_wrapped_grid` (L806-L852): find
// `needle` on a grid that WRAPS its content, starting anywhere. Returns the row
// index the needle's head landed on, or ok=false.
//
// This is a DIFFERENT question from containment: a wrap moves where the text
// appears without changing the text, so the row before the break carries the
// text's OPENING and every following row continues where the previous ended.
// Requiring that chain — rather than a loose subsequence — is what keeps the
// check from accepting a film whose text never was drawn.
func locateOnWrappedGrid(needle string, gridRows []string) (int, bool) {
	var texts []string
	for _, t := range strings.Split(needle, "\n") {
		if n := normalize(t); n != "" {
			texts = append(texts, n)
		}
	}
	if len(texts) == 0 {
		return 0, false
	}
	first := texts[0]
	for i := range gridRows {
		n := normalize(gridRows[i])
		if n == "" {
			continue
		}
		for k := runeLen(first); k > 0; k-- {
			if !strings.HasSuffix(n, prefixRunes(first, k)) {
				continue
			}
			var rest string
			if k == runeLen(first) {
				rest = strings.Join(texts[1:], " ")
			} else {
				rest = fromRune(first, k)
				if len(texts) > 1 {
					rest += " " + strings.Join(texts[1:], " ")
				}
				rest = strings.TrimSpace(rest)
			}
			if wrappedChain(rest, gridRows[i+1:]) {
				return i, true
			}
		}
	}
	return 0, false
}

// wrappedChain is verify.py's `_wrapped_chain` (L853-L890): whether the
// following rows carry `remainder`, in order, rows in sequence. Each row must
// continue exactly where the previous one ended, the chain may step over blank
// rows and one chrome row, and a row the surface CUT (trailing ellipsis) carries
// only the head of its piece — its own truncation, not a licence to match
// loosely.
func wrappedChain(remainder string, rows []string) bool {
	left := []rune(remainder)
	stepped := 0
	for _, s := range rows {
		n := strings.TrimRight(normalize(s), " \t")
		cut := strings.HasSuffix(n, ellipsis)
		n = strings.TrimRight(n, ellipsis)
		if n == "" {
			continue
		}
		nr := []rune(n)
		if strings.HasPrefix(string(left), n) {
			left = []rune(strings.TrimLeft(string(left[len(nr):]), " \t\r\n"))
			if len(left) == 0 {
				return true
			}
			if cut {
				return false // the row was cut, so the rest of its piece is gone
			}
			continue
		}
		if strings.HasPrefix(n, strings.TrimRight(string(left), " \t")) {
			// The row continues PAST the fragment: it shows the fragment's tail and
			// the rest of the line the fragment was cut from. The fragment is
			// complete here — the ledger's boundary is where it ends.
			return true
		}
		if cut {
			return false
		}
		if isGutterRow(n) || stepped == 0 {
			stepped++
			continue // one chrome row the wrap ran into
		}
		return false
	}
	return len(left) == 0
}

// isGutterRow is Python's `n in GUTTER_GLYPHS`: the row IS one of the surface's
// markers, so it carries no text of its own.
func isGutterRow(n string) bool {
	r := []rune(n)
	return len(r) == 1 && strings.ContainsRune(gutterGlyphs, r[0])
}

// skeletonInOrder is verify.py's `skeleton_in_order` (L974-L991): whether the
// grid's own characters carry the needle's, contiguous and in order.
func skeletonInOrder(needle, flat string) bool {
	sk := skeleton(needle)
	if sk == "" {
		return false
	}
	return strings.Contains(skeleton(flat), sk)
}

// locateInFlatOrder is verify.py's `locate_in_flat_order` (L1003-L1020):
// locateOnWrappedGrid's sibling for the constructs whose ROW BREAKS the surface
// owns — the needle is looked for in the FLATTENED rows, in order.
func locateInFlatOrder(text string, gridRows []string) (int, bool) {
	for i := range gridRows {
		if skeletonInOrder(text, strings.Join(gridRows[i:], "")) {
			return i, true
		}
	}
	return 0, false
}

// runDrawn is verify.py's `run_drawn` (L974-L1002): whether the surface's grid
// carries a run of chunks as ONE drawn text. THE ACCEPTANCE PREDICATE.
func runDrawn(text, flat string, gridRows []string) bool {
	n := normalize(text)
	if n == "" {
		return false
	}
	if visibleOn(n, flat, gridRows) {
		return true
	}
	if _, ok := locateOnWrappedGrid(text, gridRows); ok {
		return true
	}
	if skeletonInOrder(n, flat) {
		return true
	}
	if _, ok := locateInFlatOrder(text, gridRows); ok {
		return true
	}
	return false
}

// tailOnGrid is verify.py's `tail_on_grid` (L1021-L1042): whether a chunk's TAIL
// was drawn, and the ONE named non-acceptance.
//
// A chunk that begins MID-WORD has an opening no row can carry (the boundary is
// a fixed rune count; the surface drew that word as one run). Found: the chunk
// is a non-acceptance, NAMED by the caller with its beat, preview and how many
// runes were located — never passed silently.
func tailOnGrid(r LedgerRow, flat string, gridRows []string) (int, bool) {
	probe := normalize(r.Text)
	total := runeLen(probe)
	for n := total; n > visibilityMinRunes-1; n-- {
		tail := suffixRunes(probe, n)
		if strings.Contains(flat, tail) {
			return n, true
		}
		if onOneDrawnLine(tail, gridRows) {
			return n, true
		}
		if _, ok := locateOnWrappedGrid(tail, gridRows); ok {
			return n, true
		}
	}
	return 0, false
}
