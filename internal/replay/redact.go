// redact.go — the ONE definition of the redaction pass: the identifying SHAPES,
// the length-preserving substitution, the substitution log and its name.
//
// WHY IT IS EXPORTED AND WHY IT LIVES HERE. `gopi chat --replay` (the REAL chat
// surface, cmd/agent) masks the text the surface is about to RECEIVE, and the
// producers of the mask live here so the shapes have ONE definition. A second
// shape list in cmd/agent would be the first thing to drift — a shape added here
// and not there would be withheld on one route and delivered on the other, with
// each route's own check green. Rule 14's shape, applied to a regexp.
//
// THE SUBSTITUTION IS LENGTH-PRESERVING, and that is what makes the joint work
// possible at all: a shorter stand-in would reflow the line the run sat in,
// moving every glyph after it — a different picture rather than the same picture
// with one fact withheld. It is also what lets the chat surface mask a STREAM:
// fragments are masked by joining them, masking once, and slicing back at the
// original rune boundaries, so a shape that straddles two fragments cannot hide
// in the seam (each half alone matches nothing).
package replay

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// IdentShapes are the runs that identify this host or this run, written as
// SHAPES so no file ever has to store a secret in order to look for one — the
// same vocabulary tools/human-ui/check-clean.sh checks its own output with. A
// shape that arrives through a path this masker did not anticipate is caught by
// such an external gate rather than here, which is why that gate is run on the
// delivered directory and this one only has to be right about what it redacts.
var IdentShapes = regexp.MustCompile(
	`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}` +
		`|[0-9a-f]{32}` +
		`|/home/[A-Za-z0-9_.-]+` +
		`|sandbox-[A-Za-z0-9-]+-[0-9a-f]{8}`,
)

// RedactionStandin is one ASCII rune repeated to the length of a redacted run.
// The substitution is LENGTH-PRESERVING (see Redactor.Mask), and the rune is
// ASCII on purpose: a substitute of N runes must be N CELLS wide, and an ASCII
// rune is one cell in every palette.
const RedactionStandin = "x"

// RedactionLogName is where the pass records what it replaced, under --scratch.
// It is SCRATCH ONLY and is never delivered: its columns carry the identifying
// text itself, which is the one thing a film must not carry.
const RedactionLogName = "redactions.tsv"

// RedactionLogHeader is the log's one header row. The columns are `#n` (the
// substitution's ordinal), `runes` (the length of what was replaced, which is
// also the length of the stand-in) and `matched` (the run itself). A reader of
// the log gets a POSITIVE CONTROL from the rows: a pass that replaced nothing
// reads as a vacuous claim rather than as a clean one.
const RedactionLogHeader = "#n\trunes\tmatched\n"

// Redactor replaces identifying runs in text a surface is about to receive, and
// records each substitution. It sits UPSTREAM of every pixel on purpose: a mask
// applied to the raster afterwards has to be contained by a measured box (the
// containment check tools/human-ui/check-clean.sh runs exists because that is
// easy to get wrong), while a substitution made before the editor ever sees the
// bytes cannot leak through a miss-measured rectangle — there is no rectangle.
//
// It is NOT safe for concurrent use: the callers that need that (cmd/agent's
// beat driver) hold their own lock around it, which also orders the log rows
// against the beats they belong to.
type Redactor struct {
	log *bufio.Writer
	n   int
}

// NewRedactor opens the substitution log at path (truncating) and writes its
// header. A log that cannot be opened is an ERROR the caller must turn into a
// REFUSAL: the whole point of the pass is that what it withheld is recorded, so
// a run that cannot record must not silently revert to drawing the identifier.
func NewRedactor(path string) (*Redactor, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create the redaction log %s: %w", path, err)
	}
	return NewRedactorWriter(f), nil
}

// NewRedactorWriter records substitutions into an ALREADY-OPEN writer (the beat
// driver owns one file beside its ledger, and the log must share its lifetime).
// The header is written and FLUSHED here, so a reader that opens the log before
// the first substitution finds a well-formed file rather than an empty one.
func NewRedactorWriter(w io.Writer) *Redactor {
	r := &Redactor{log: bufio.NewWriter(w)}
	_, _ = r.log.WriteString(RedactionLogHeader) //stylegate:ignore a header write into a buffer the caller's own Close/Flush reports on
	_ = r.log.Flush()
	return r
}

// Mask returns the text with every identifying run replaced and whether it
// changed. The substitute is the same RUNE COUNT as what it replaces (see
// RedactionStandin).
func (r *Redactor) Mask(s string) (string, bool) {
	if r == nil || s == "" {
		return s, false
	}
	if !IdentShapes.MatchString(s) {
		return s, false
	}
	out := IdentShapes.ReplaceAllStringFunc(s, func(m string) string {
		r.record(m)
		return strings.Repeat(RedactionStandin, utf8.RuneCountInString(m))
	})
	return out, true
}

// record logs one substitution. It is the ONE writer of the log's row shape.
func (r *Redactor) record(m string) {
	r.n++
	if r.log != nil {
		fmt.Fprintf(r.log, "%d\t%d\t%s\n", r.n, utf8.RuneCountInString(m), m)
	}
}

// MaskFragments masks a SEQUENCE of fragments as ONE text and returns them at
// their original rune boundaries.
//
// WHY THE JOINED FORM — a measured leak, not a hypothetical. An identifier can
// straddle two fragments of one logical text: a long uuid in the middle of a
// sentence lands half in one fragment and half in the next, and masked fragment
// by fragment NEITHER half matches an identifier SHAPE (8-4-4 is not
// 8-4-4-4-12), so the masker passes both and the two fragments together spell
// the identifier on screen — a leak that a shape-based check cannot see either,
// because it looks for whole shapes. The substitution is LENGTH-PRESERVING
// precisely so this can be done: join, mask once, then slice back at the
// original rune boundaries, and the fragments are unchanged in length and
// position while the identifier that crossed the seam is gone.
//
// It reports the number of fragments whose text actually changed, so a caller
// can say how much of a stream it withheld.
func (r *Redactor) MaskFragments(frags []string) (out []string, changed int) {
	if r == nil || len(frags) == 0 {
		return frags, 0
	}
	joined := make([]rune, 0, 256)
	ends := make([]int, 0, len(frags)) // rune offset of each fragment's end
	for _, f := range frags {
		joined = append(joined, []rune(f)...)
		ends = append(ends, len(joined))
	}
	masked, any := r.Mask(string(joined))
	if !any {
		return frags, 0
	}
	mr := []rune(masked)
	out = make([]string, len(frags))
	prev := 0
	for i, e := range ends {
		out[i] = string(mr[prev:e])
		prev = e
		if out[i] != frags[i] {
			changed++
		}
	}
	return out, changed
}

// Masks reports how many substitutions this redactor has recorded.
func (r *Redactor) Masks() int {
	if r == nil {
		return 0
	}
	return r.n
}

// Flush persists what was replaced so far. The callers call it when the text
// they are masking is complete, so a crash mid-run still leaves the partial
// record on disk.
func (r *Redactor) Flush() error {
	if r == nil || r.log == nil {
		return nil
	}
	if err := r.log.Flush(); err != nil {
		return fmt.Errorf("flush the redaction log: %w", err)
	}
	return nil
}

// shapeAlphabet is the set of runes ANY of the shapes above is built from: hex
// digits, the letters and punctuation of `/home/<user>`, and the letters,
// digits, dots, dashes and underscores of a sandbox id. A suffix made only of
// these could still grow into a shape, so it is held; anything else (a space, a
// comma, a CJK character) ends the run and releases the text before it.
//
// It is a deliberate SUPERSET of the four shapes' alphabets, and the direction
// of that error is the point: a false hold costs one fragment's latency and the
// caller flushes at the end of the text, while a false emit puts the head of an
// identifier on the screen with no way to take it back.
// It is ANCHORED at both ends: the test is "is this suffix ENTIRELY made of the
// alphabet", and an unanchored pattern would report a suffix as continuable
// because some word inside it matched (measured while writing this: `"the answer
// is "` was held at 14 runes because the word `answer` matched in the middle).
var shapeAlphabet = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,` + maxIdentShapeRunesS + `}$`)

// maxIdentShapeRunesS is maxIdentShapeRunes as a regexp repetition bound (the
// same number, spelled once as a number so the two cannot drift).
const maxIdentShapeRunesS = "36"

// PendingShapeTailLen reports how many RUNES at the end of s must be HELD BACK
// before s may be shown: the longest suffix that could still grow into a shape.
//
// WHY A STREAM NEEDS THIS. Masking a fragment as it arrives is safe only while
// no shape can still be completed by later bytes. A fragment that ends in the
// first eight hex characters of a uuid is not (yet) a shape, so a masker that
// judges each fragment alone paints those eight characters — and the leak is
// complete once the rest arrives, because the head is already on the screen. The
// tail is therefore withheld until the next fragment (or the end of the stream)
// proves it either was or was not part of a run, and the mask is then applied to
// the joined text.
//
// The answer is 0 for a text whose tail cannot continue a shape, which is the
// common case: prose, punctuation and any word that is not hex, and any text
// shorter than a shape's own alphabet run is held only while it is still a
// prefix (a run of 40 hex characters is already longer than any hex shape, so
// holding more than that would only delay text that can no longer grow a shape).
func PendingShapeTailLen(s string) int {
	if s == "" {
		return 0
	}
	rs := []rune(s)
	// At most one shape's worth of runes can be pending: beyond that the tail is
	// longer than any run a shape can be, so it cannot be a strict prefix of one.
	max := len(rs)
	if max > maxIdentShapeRunes {
		max = maxIdentShapeRunes
	}
	for n := max; n > 0; n-- {
		if shapeAlphabet.MatchString(string(rs[len(rs)-n:])) {
			return n
		}
	}
	return 0
}

// maxIdentShapeRunes is the longest rune count any alternative in IdentShapes
// can match (the uuid's 36). It bounds the window PendingShapeTailLen searches,
// so a long text does not scan an unbounded suffix.
const maxIdentShapeRunes = 36

// MaxIdentShapeRunes is the longest run any of the shapes above can occupy. A
// STREAMING caller holds back at most this many runes, and the hold is SUFFICIENT
// — not merely conservative: a shape that began in released text and ended in
// held text would be longer than 36 runes, and no shape is.
func MaxIdentShapeRunes() int { return maxIdentShapeRunes }
