package lineedit

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nick/gopi/internal/inputsource"
)

// pasteref.go — the READER's half of the paste reference vocabulary, and the
// input line's own capacity, both exported for the ONE downstream consumer that
// must make a decision about a whole message rather than about one read.
//
// WHY A SECOND FILE (and not an edit to pastestore.go): pastestore.go is
// adopted verbatim from the editor-side work that produced the spill mechanism,
// so the two halves of this feature compose without a hand-merge. Everything
// here is ADDITIVE — it reads the spelling paste-storage.go writes and exports
// the bound value.go already owns, and it changes neither.
//
// The distinction the two files sit on either side of is the reason both
// exist: PasteThreshold (pastestore.go) answers "was THIS ONE READ a paste?",
// which is a question about a read(2); InputLineRuneBound (here) answers "has a
// whole ASSEMBLED message outgrown the input line?", which is a question about
// the message. A pipe that delivers a megabyte in 4 KiB reads produces no
// single read at the paste threshold while producing a message far past the
// line's capacity — so the read-side answer cannot stand in for the
// message-side one, and a boundary that asked the read-side question would be
// blind to exactly the shape it exists to catch.

// PasteRefKeyword — the reference's marker word — lives in pastestore.go, the
// producer that renders it, which EXPORTS it so a consumer that must RECOGNISE
// a reference never spells the word itself (Rule 1b/14). This file is the
// reader's half and DEFINITIONS only: it declares no alias, because a second
// declaration of the same name would not compose with the producer's own
// (measured on merge: `PasteRefKeyword redeclared in this block`). Every reader
// below resolves that one constant.

// InputLineRuneBound is the input line's own rune capacity — the point past
// which Value refuses to accept more text and the editor is KNOWN to be
// truncating what the user typed.
//
// It is exported because the truncation is the reason a message-side boundary
// must exist: a caller that assembles the outbound message from a completed
// line can only guarantee "the model received everything the user sent" by
// acting BEFORE this bound turns the line into a prefix of the input. The value
// is not re-tuned here and not spelled here — it IS valueMaxRunesDefault, so
// the capacity has one definition and the boundary cannot drift from it.
const InputLineRuneBound = valueMaxRunesDefault

// The punctuation and the field names of the reference spelling PasteRefFor
// renders. They are named (Rule 1b) because HasPasteRef and PasteRefPaths parse
// against them, and because a reader that spelled "source" or "size" itself
// would keep passing after the field was renamed.
//
// THE SPELLING, and the two shapes it must both read:
//
//	[paste 4095: /s/paste-1.txt]                        (old: runes + path)
//	[paste 4095: /s/paste-1.txt source=notify size=12B] (now: + provenance + bytes)
//
// COMPATIBILITY HAS ONE DIRECTION (operator ruling, 2026-10-06): the NEW code
// reads the OLD data — a missing field is simply NOT SHOWN, which is what
// "compatible" means. Nothing here reads a new-format reference with an old
// parser, because the old parser no longer runs.
const (
	refOpen = "["
	// refClose ends the reference.
	refClose = "]"
	// refSep separates the rune count from the rest of the reference. The old
	// spelling wrote a SPACE after it and the new one does not — the parser
	// accepts either (refFieldGap is trimmed wherever it may appear) and the
	// renderer writes the new one.
	refSep = ":"
	// refFieldGap is the one-byte separator between the reference's parts.
	refFieldGap = " "
	// refAssign joins a named field's key to its value.
	refAssign = "="
	// refSourceKey / refSizeKey are the two named fields this extension adds.
	// refSourceKey's value comes from internal/inputsource (one definition of
	// the vocabulary, Rule 14); refSizeKey's value is an exact BYTE count, the
	// same unit the store counts the file in.
	refSourceKey = "source"
	refSizeKey   = "size"
	// pasteSizeUnitBytes is the unit suffix the size carries, so a reader cannot
	// mistake a byte count for the rune count beside it (the two differ for any
	// non-ASCII payload, which is exactly when a reader would guess wrong).
	pasteSizeUnitBytes = "B"
)

// PasteRefInfo is one reference as a READER sees it: the path PasteRefPaths
// returns, plus the provenance and the byte count this extension added.
//
// Source and Size are the ZERO VALUE when the reference predates the extension.
// That is not an error and not a default — it is the old data saying nothing,
// and a renderer that showed "source=" for it would be inventing provenance
// (Rule 15a: an absent optional field stays absent).
type PasteRefInfo struct {
	// Path is the absolute path of the spilled file.
	Path string
	// Runes is the count the reference carries (the editor's, which lags the
	// file while a paste is still arriving — see SpillFacts for the file's own
	// answer).
	Runes int
	// Size is the payload's size in bytes, or 0 when the reference is old.
	Size int
	// Source names the face that spilled the payload (internal/inputsource), or
	// "" when the reference is old.
	Source string
}

// formPasteSize renders a byte count with its unit — the ONE producer of the
// size field's value, paired with parsePasteSize below (Rule 13: the render and
// the parse of one format change in the same commit).
func formatPasteSize(bytes int) string {
	return strconv.Itoa(bytes) + pasteSizeUnitBytes
}

// parsePasteSize reads back what formatPasteSize wrote, tolerating a bare
// number as well (a hand-written or future reference without the unit). It
// reports false rather than guessing when the token is not a size.
func parsePasteSize(v string) (int, bool) {
	digits := strings.TrimSuffix(v, pasteSizeUnitBytes)
	if digits == "" {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// pasteRefFieldCut returns the byte offset in body at which the PATH ends and
// the named fields begin — the first ` <key>=` occurrence — or -1 when the body
// carries no named field (every old-format reference).
//
// It is the ONE place that decides where a path stops, which is why the path is
// taken from here rather than from the first space: an absolute path may itself
// contain a space, and cutting at the first one would truncate it.
func pasteRefFieldCut(body string) int {
	cut := -1
	for _, key := range []string{refSourceKey, refSizeKey} {
		at := strings.Index(body, refFieldGap+key+refAssign)
		if at >= 0 && (cut < 0 || at < cut) {
			cut = at
		}
	}
	return cut
}

// pasteRefParse parses ONE reference out of s, returning it and the byte index
// just past its closing bracket. It is the reader both PasteRefPaths, HasPasteRef
// and SourcesIn are built on, so none of them can disagree about what a
// reference is.
//
// It does NOT unescape the path: a path is whatever sat between the separator
// and the closing bracket, which is exactly what PasteRefFor wrote.
func pasteRefParse(s string) (info PasteRefInfo, next int, ok bool) {
	marker := refOpen + PasteRefKeyword + refFieldGap
	i := strings.Index(s, marker)
	if i < 0 {
		return PasteRefInfo{}, 0, false
	}
	rest := s[i+len(marker):]
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return PasteRefInfo{}, 0, false
	}
	runes, err := strconv.Atoi(rest[:digits])
	if err != nil {
		return PasteRefInfo{}, 0, false
	}
	after := rest[digits:]
	if !strings.HasPrefix(after, refSep) {
		return PasteRefInfo{}, 0, false
	}
	// The gap after the separator is TOLERATED, not required: the old spelling
	// wrote it and the new one does not, and both are read here.
	untrimmed := after[len(refSep):]
	after = strings.TrimLeft(untrimmed, refFieldGap)
	base := i + len(marker) + digits + len(refSep) + (len(untrimmed) - len(after))
	end := strings.Index(after, refClose)
	if end < 0 {
		return PasteRefInfo{}, 0, false
	}
	body := after[:end]
	info = PasteRefInfo{Runes: runes}
	cut := pasteRefFieldCut(body)
	if cut < 0 {
		info.Path = body
		return info, base + end + len(refClose), true
	}
	info.Path = body[:cut]
	for _, field := range strings.Fields(body[cut:]) {
		key, value, hasValue := strings.Cut(field, refAssign)
		if !hasValue {
			continue
		}
		switch key {
		case refSourceKey:
			info.Source = value
		case refSizeKey:
			if n, isSize := parsePasteSize(value); isSize {
				info.Size = n
			}
		}
	}
	return info, base + end + len(refClose), true
}

// PasteRefInfoFor returns the FIRST reference in s, or false when s carries
// none. It is the reader a consumer uses when it needs the provenance or the
// size rather than only the path.
func PasteRefInfoFor(s string) (PasteRefInfo, bool) {
	info, _, ok := pasteRefParse(s)
	return info, ok
}

// StripPasteRefs removes every spilled-paste reference from s, leaving the words
// the operator (or the model) wrote around them.
//
// WHY THIS EXISTS (measured 2026-10-06, the paste half of the turn-boundary
// retrieval). A reference is a POINTER, not content: `[paste 5411: /home/nick/
// workspace/gopi-sandbox/sandbox-turn-prefetch-daa50fca/.gopi/sessions/7349789c-
// …/paste/paste-20261006-041850-1.txt source=human size=5411B]` contributed NINE
// candidates — the path's own components and the size — and the extractor's
// keyword budget (Config.MaxKeywords) is EIGHT. The budget was therefore spent on
// the pointer, and the payload's words (read separately, from the file the
// reference names) were cut off before they entered the key: the live run
// recorded `"query":"paste turn 5411 home nick workspace gopi sandbox"` for a turn
// whose pasted text was entirely about the retrieval seam. A consumer that has
// ALREADY read the payload must strip the pointer, or the pointer wins.
//
// It is the ONE reader of the spelling, like every other function here: the
// removal reuses pasteRefParse rather than re-matching the shape.
func StripPasteRefs(s string) string {
	var b strings.Builder
	marker := refOpen + PasteRefKeyword + refFieldGap
	for rest := s; ; {
		if _, next, ok := pasteRefParse(rest); ok {
			at := strings.Index(rest, marker)
			b.WriteString(rest[:at])
			// The reference stood where a word could sit ("turn[paste 1: …]"), so
			// it leaves a SEPARATOR rather than nothing: joining the two sides
			// would invent a word neither of them wrote.
			b.WriteString(refFieldGap)
			rest = rest[next:]
			continue
		}
		b.WriteString(rest)
		break
	}
	return strings.TrimSpace(b.String())
}

// SourcesIn reports which provenance values the references in s carry, in
// first-appearance order, skipping the references that carry none (every
// old-format one). It exists so a caller can say WHERE a payload came from
// without re-parsing the spelling itself.
func SourcesIn(s string) []string {
	var out []string
	seen := map[string]bool{}
	for rest := s; ; {
		info, next, ok := pasteRefParse(rest)
		if !ok {
			return out
		}
		if info.Source != "" && !seen[info.Source] {
			seen[info.Source] = true
			out = append(out, info.Source)
		}
		rest = rest[next:]
	}
}

// SourcesAreKnown reports whether every provenance value s carries is one of
// the vocabulary's own (internal/inputsource). An unknown spelling is a value a
// reader must not silently treat as one of the four, so callers that care
// report it (Rule 15).
func SourcesAreKnown(s string) bool {
	for _, src := range SourcesIn(s) {
		if !inputsource.Known(src) {
			return false
		}
	}
	return true
}

// ExceedsInputLineBound reports whether s is longer than the input line can
// hold, in RUNES — the same unit Value measures its own capacity in, so a line
// and this predicate can never disagree about whether the line overflowed.
//
// It is the boundary's own question ("has the assembled message outgrown the
// line?"), and it is deliberately NOT PasteThreshold: that one asks about a
// single read(2), and an input assembled from many small reads never crosses it
// while assembling a message arbitrarily past the line. See InputLineRuneBound.
func ExceedsInputLineBound(s string) bool {
	return utf8.RuneCountInString(s) > InputLineRuneBound
}

// PasteRefPaths returns the absolute paths of every spilled-paste reference in
// s, in the order they appear.
//
// It is the parser of the spelling PasteRefFor renders, and it exists so a
// CONSUMER never has to re-derive the format — the boundary that must name the
// payload's file to the model reads it here rather than slicing a string, which
// is how a second, drifting copy of the format starts.
//
// The scan is deliberately narrow (the marker, digits, the separator, up to the
// closing bracket) and it does NOT unescape: a path is whatever sat between the
// separator and the bracket, which is exactly what PasteRefFor wrote (the store
// joins it from the session directory, and the suffix is a fixed ".txt").
func PasteRefPaths(s string) []string {
	var out []string
	for rest := s; ; {
		info, next, ok := pasteRefParse(rest)
		if !ok {
			return out
		}
		out = append(out, info.Path)
		rest = rest[next:]
	}
}

// HasPasteRef reports whether s carries a spilled-paste reference of the shape
// PasteRefFor renders (`[paste <runes>: /abs/path]`).
//
// It is the ONE reader of that spelling. It is deliberately a SHAPE test rather
// than an equality test: a reference is expected to sit inside a line the user
// (or the editor) typed around it, so a predicate requiring the whole string to
// be a reference would miss the ordinary case.
//
// A false positive is harmless by construction — the caller's use of this
// answer is "treat this message as carrying pasted data", and a user who types
// a reference-shaped literal is asking for exactly that treatment. A false
// NEGATIVE is what the caller must not rely on this predicate to exclude, which
// is why it is OR-ed with the capacity test rather than used alone.
func HasPasteRef(s string) bool {
	_, _, ok := pasteRefParse(s)
	return ok
}

// SpillFacts is what a turn header needs to know about a spilled payload it did
// not itself write: how big it is, how many lines it holds, and the opening lines
// that name it.
//
// It is READ FROM THE FILE rather than taken from the reference, and that is a
// correction rather than a convenience: the `N` in `[paste N: /abs/path]` is the
// count the editor had when it last rewrote the reference, which lags the file
// while a paste is still arriving. A header that repeated the line's number would
// print the same wrong size the spill form exists to replace; a header that reads
// the file states what the file holds.
type SpillFacts struct {
	// Bytes is the file's size in bytes.
	Bytes int
	// Runes is how many runes those bytes decode to, counted over the WHOLE file.
	Runes int
	// Lines is how many lines the payload holds. A payload with no trailing
	// newline still ends a line, so a one-line paste reads 1, never 0.
	Lines int
	// Head is the payload's opening lines, at most turnSpillHeadMaxLines of them,
	// each already trimmed of its newline and of surrounding whitespace.
	Head []string
	// HeadTruncated reports whether a head line was longer than the capture bound
	// (spillHeadLineCapture) and was therefore captured in part. The header cuts
	// head lines to its own cell budget anyway, so this only says whether the
	// stored line is the whole line or a prefix of it — a fact a reader of the
	// struct must not have to infer from a missing marker.
	HeadTruncated bool
}

// The reader's own bounds. They exist so that "show a few lines of the payload"
// can never come to mean "load the payload": the capture keeps a fixed number of
// lines and a fixed number of bytes per line, whatever the file's size.
const (
	// spillFactsBufBytes is the streaming buffer SpillFacts is read through. The
	// file is never held in memory as a whole — a 64 MiB paste is counted with a
	// 64 KiB buffer — because the header's job is to describe the payload, not to
	// carry it.
	spillFactsBufBytes = 64 << 10
	// spillHeadLineCapture is how many BYTES of one head line are kept. It is
	// turnSpillHeadMaxCells cells' worth at the widest UTF-8 rune (utf8.UTFMax
	// bytes), so a line of CJK text is captured whole however the header later
	// cuts it.
	spillHeadLineCapture = turnSpillHeadMaxCells * utf8.UTFMax
	// spillRuneCarry is how many trailing bytes of a read may hold a rune split
	// across the boundary. They are held back and decoded with the bytes that
	// complete them, so the rune count is exact rather than one-too-high per
	// boundary.
	spillRuneCarry = utf8.UTFMax - 1
)

// ReadSpillFacts reads one spilled payload's facts. Reading FAILS LOUDLY (Rule
// 15): a header that silently showed no size for a file it could not read would
// be reporting a paste it never verified.
//
// The payload is STREAMED, never held: bytes and runes are counted as they arrive
// through a fixed buffer, and only the head's own few lines are kept (bounded by
// spillHeadLineCapture). The exactness that matters is the RUNE COUNT's: a rune
// split across two reads is held back and counted with the bytes that complete
// it, so the count does not depend on where the read boundary landed.
func ReadSpillFacts(path string) (facts SpillFacts, err error) {
	f, err := os.Open(path)
	if err != nil {
		return SpillFacts{}, fmt.Errorf("paste facts: cannot read %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("paste facts: cannot close %s: %w", path, cerr)
		}
	}()
	buf := make([]byte, spillFactsBufBytes)
	var (
		carry   []byte
		line    []byte
		last    byte
		sawByte bool
	)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			facts.Bytes += n
			// The carried bytes come FIRST: a rune split by the previous read is
			// held back and decoded with the bytes that complete it. They are
			// copied into a fresh slice rather than appended into carry's own
			// array — `append(carry[:0], buf...)` writes the read OVER the carry
			// and silently drops it, which costs exactly one rune per straddling
			// boundary (measured: 44999 runes for a 45000-rune payload).
			chunk := make([]byte, 0, len(carry)+n)
			chunk = append(chunk, carry...)
			chunk = append(chunk, buf[:n]...)
			complete, tail := splitWholeRunes(chunk)
			for _, b := range complete {
				last, sawByte = b, true
				facts = spillFactsFeed(facts, &line, b)
			}
			// The rune count comes from the SAME complete prefix the line folding
			// reads, so the two cannot disagree about which bytes are a rune's
			// first: a continuation byte never starts one, and an invalid byte is
			// its own rune (utf8.RuneStart) — the count utf8.RuneCount would give,
			// without re-scanning the chunk.
			for _, b := range complete {
				if utf8.RuneStart(b) {
					facts.Runes++
				}
			}
			carry = append(carry[:0], tail...)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return SpillFacts{}, fmt.Errorf("paste facts: cannot read %s: %w", path, rerr)
		}
	}
	if len(carry) > 0 {
		// A trailing partial rune at end of file: it is still rune count in the
		// only sense a reader has, and dropping it would understate the payload.
		facts.Runes += utf8.RuneCount(carry)
		for _, b := range carry {
			last, sawByte = b, true
			facts = spillFactsFeed(facts, &line, b)
		}
	}
	// A trailing partial line IS a line — a paste with no final newline is not
	// zero lines long, which is the one count a naive newline count gets wrong.
	if sawByte && last != '\n' {
		facts.Lines++
		facts = spillFactsKeepLine(facts, line)
	}
	return facts, nil
}

// splitWholeRunes splits a read into the prefix that ends on a rune boundary and
// the trailing bytes of a rune that continues in the next read (empty when the
// chunk ended cleanly). It is what makes the rune count independent of the read
// size: a naive per-chunk count inflates by one per boundary, which on a large
// CJK payload is a wrong number that still looks plausible.
//
// The walk is bounded by one rune's width (utf8.UTFMax), because at most
// utf8.UTFMax-1 bytes can belong to the rune in progress.
func splitWholeRunes(data []byte) (complete, tail []byte) {
	cut := len(data)
	for i := 0; i < spillRuneCarry && cut > 0 && !utf8.RuneStart(data[cut-1]); i++ {
		cut--
	}
	if cut == 0 {
		return nil, data
	}
	if r, size := utf8.DecodeRune(data[cut-1:]); size == 0 || r == utf8.RuneError && size <= 1 {
		// The last rune-start byte opens a rune that is NOT whole inside this
		// chunk: hold it back rather than count a truncated rune now.
		return data[:cut-1], data[cut-1:]
	}
	return data[:cut], data[cut:]
}

// spillFactsFeed folds one byte into the facts being gathered: a newline ends the
// head line in progress, and any other byte extends it — up to the capture bound,
// past which the line is still counted and still scanned but its bytes are no
// longer kept (and the cut is RECORDED, not left for a reader to infer).
func spillFactsFeed(facts SpillFacts, line *[]byte, b byte) SpillFacts {
	if b == '\n' {
		facts.Lines++
		facts = spillFactsKeepLine(facts, *line)
		*line = (*line)[:0]
		return facts
	}
	if len(facts.Head) >= turnSpillHeadMaxLines {
		// The head is full: this line is past it, so nothing about it is kept and
		// its length is not this struct's business.
		return facts
	}
	if len(*line) >= spillHeadLineCapture {
		facts.HeadTruncated = true
		return facts
	}
	*line = append(*line, b)
	return facts
}

// spillFactsKeepLine stores one finished line as a head entry, if there is room
// for it and it carries anything a reader would recognise.
func spillFactsKeepLine(facts SpillFacts, line []byte) SpillFacts {
	if len(facts.Head) >= turnSpillHeadMaxLines {
		return facts
	}
	text := strings.TrimSpace(string(line))
	if text == "" {
		return facts
	}
	facts.Head = append(facts.Head, text)
	return facts
}
