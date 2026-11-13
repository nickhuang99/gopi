package film

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/nick/gopi/internal/replay"
)

// The five criteria, ported. Each function cites the verify.py line range it came
// from, and each returns a Verdict rather than printing: a check that can only
// print is a check a test cannot assert on, and the whole point of this port is
// that the criteria drive REAL produced artifacts from a test.
//
// The pass STRUCTURE is verify.py's, deliberately: the exemptions are named kinds
// (not silent passes), the counts of what was checked and what was exempt are
// reported, and a failure names the beat a reader can go and look at.

// The kinds verify.py classifies beat text by (L290-L295). They are the ledger's
// own `kind` column values, so they are the recording format's vocabulary.
const (
	kindAnswer   = "answer"
	kindToolCall = "tool_call"
	kindRequest  = "request"
	kindChrome   = "chrome"
)

// chromeTurnEnd is the layout label that closes a turn (verify.py L1120).
const chromeTurnEnd = "turn end"

// sourceTextKinds / fieldComposedKinds / unanchoredKinds / exemptKinds are
// verify.py's four kind sets (L290-L301, L171-L176).
var (
	sourceTextKinds    = map[string]bool{kindAnswer: true}
	fieldComposedKinds = map[string]bool{kindToolCall: true}
	unanchoredKinds    = map[string]bool{kindRequest: true, kindChrome: true}
	exemptKinds        = map[string]bool{kindChrome: true, kindRequest: true}
)

// visibilityMinRunes is verify.py's VISIBILITY_MIN_RUNES (L192): how many runes
// of a chunk's tail must be found on the grid for a mid-word chunk to count as
// drawn.
const visibilityMinRunes = 4

// Finding is one failure: the subject a reader can look at and why it failed.
type Finding struct {
	Subject string
	Reason  string
}

func (f Finding) String() string { return fmt.Sprintf("%s: %s", f.Subject, f.Reason) }

// Verdict is one criterion's result: what failed, what was measured, and what was
// exempt BY NAME (verify.py's own discipline: an exemption is counted and
// printed, never a silent pass).
type Verdict struct {
	// Name is the criterion ("traceability", "legibility", …).
	Name string
	// Source is the verify.py line range this check ported.
	Source string
	// Failures are the findings; empty means the criterion holds.
	Failures []Finding
	// Notes are the readable lines a report quotes (the counts, the exemptions,
	// the arithmetic that closed).
	Notes []string
	// N carries the measured counts a caller may assert on.
	N map[string]int
}

// OK reports whether the criterion holds.
func (v Verdict) OK() bool { return len(v.Failures) == 0 }

// Summary renders the verdict as printable lines, in verify.py's own voice.
func (v Verdict) Summary() string {
	var b strings.Builder
	// The Source line says WHERE the criterion came from, in its own voice: the five
	// ported criteria cite a verify.py line range, and a criterion written HERE
	// says so rather than claiming a provenance it does not have (Rule 38's
	// discipline applied to a citation: the generator names itself).
	fmt.Fprintf(&b, "%s — %s\n", v.Name, v.Source)
	for _, n := range v.Notes {
		fmt.Fprintf(&b, "  %s\n", n)
	}
	for _, f := range v.Failures {
		fmt.Fprintf(&b, "  FAIL: %s\n", f)
	}
	if v.OK() {
		fmt.Fprintf(&b, "  OK\n")
	}
	return b.String()
}

func (v *Verdict) fail(subject, reason string) {
	v.Failures = append(v.Failures, Finding{Subject: subject, Reason: reason})
}

func (v *Verdict) note(format string, args ...any) {
	v.Notes = append(v.Notes, fmt.Sprintf(format, args...))
}

// maxReportedFindings is how many failures a criterion lists in full. The rest
// are counted: a run with a systematic fault would otherwise print one line per
// beat, and the count is what says "systematic".
const maxReportedFindings = 10

// ── 1. traceability ─────────────────────────────────────────────────────────

// CheckTraceability is verify.py's `check_traceability` (L1043-L1265, 223 lines).
//
// TWO DIRECTIONS, both required, and each is the other's reason for existing:
//
//	PROVENANCE — every beat's text is derivable from the source line the ledger
//	  names. This catches a beat produced by something other than the recording.
//	VISIBILITY — the text is on the grid that beat photographed. This catches a
//	  beat that claims to have been drawn and never was.
//
// The check is PER TURN END, not per beat, and that is a measured decision (the
// source's own comment): the band SCROLLS as a turn grows, so an early
// fragment's row can be pushed off the top by the time the next beat is
// photographed. What must hold is that by the END of a turn every fragment the
// ledger recorded for that turn is on the screen.
func CheckTraceability(f Film) Verdict {
	v := Verdict{Name: "1. traceability", Source: "L1043-L1265", N: map[string]int{}}
	if strings.TrimSpace(f.SessionJSONL) == "" {
		v.fail("session", "no recorded session given (Film.SessionJSONL): provenance cannot be measured without the source the beats claim")
		return v
	}
	rows, err := f.ReadLedger()
	if err != nil {
		v.fail("ledger", err.Error())
		return v
	}
	src, err := f.SourceLines()
	if err != nil {
		v.fail("session", err.Error())
		return v
	}
	v.N["beats"] = len(rows)
	v.N["source_lines"] = len(src)
	v.note("%d beats in the ledger, %d lines in the source", len(rows), len(src))

	// ── PROVENANCE ──────────────────────────────────────────────────────────
	exempt := 0
	type provenanceBad struct{ beat, reason string }
	var provenance []provenanceBad
	for _, run := range textRuns(rows) {
		head := run[0]
		if unanchoredKinds[head.Kind] {
			exempt += len(run)
			continue
		}
		if head.SrcLine < 1 || head.SrcLine > len(src) {
			provenance = append(provenance, provenanceBad{beat: fmt.Sprintf("beat %d", head.Beat),
				reason: fmt.Sprintf("src_line %d is outside the file (%d lines)", head.SrcLine, len(src))})
			continue
		}
		line := src[head.SrcLine-1]
		hay := []string{line}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err == nil && obj != nil {
			hay = sourceText(obj, line)
		}
		// The ledger holds text the surface was ASKED TO DRAW: the source, already
		// redacted (the mask runs before any pixel exists). The same substitution
		// is applied here, so a redaction does not read as a provenance failure —
		// and the shapes and the stand-in are internal/replay's own, not a copy.
		for i, h := range hay {
			hay[i] = replay.IdentShapes.ReplaceAllStringFunc(h, func(m string) string {
				return strings.Repeat(replay.RedactionStandin, utf8.RuneCountInString(m))
			})
		}
		if fieldComposedKinds[head.Kind] {
			// A tool call has no source text of its own: the drawn row is the
			// renderer's COMPOSITION of the line's fields, checked by reproducing
			// that composition and requiring the row to BE it.
			needles := composedToolNeedles(line)
			if len(needles) == 0 {
				for _, r := range run {
					provenance = append(provenance, provenanceBad{beat: fmt.Sprintf("beat %d", r.Beat),
						reason: fmt.Sprintf("no tool call to compose on source line %d", r.SrcLine)})
				}
				continue
			}
			red := make([]string, 0, len(needles))
			for _, n := range needles {
				red = append(red, replay.IdentShapes.ReplaceAllStringFunc(n, func(m string) string {
					return strings.Repeat(replay.RedactionStandin, utf8.RuneCountInString(m))
				}))
			}
			for _, r := range run {
				want := normalize(r.Text)
				matched := false
				for _, n := range red {
					if strings.Contains(normalize(n), want) {
						matched = true
						break
					}
				}
				if !matched {
					provenance = append(provenance, provenanceBad{beat: fmt.Sprintf("beat %d", r.Beat),
						reason: fmt.Sprintf("text %q is not the activity row its source line's call composes", previewOf(r.Preview))})
				}
			}
			continue
		}
		if !sourceTextKinds[head.Kind] {
			exempt += len(run)
			continue
		}
		// THE RUN, not the chunk: the ledger's rows are chunks at a fixed rune
		// count and the source holds the text as ONE block, so a chunk whose
		// boundary falls mid-token has an opening the source carries only as an
		// interior. The block is compared against the run's JOINED text — exactly
		// the stream the renderer received.
		if _, ok := probeOf(runText(run)); !ok {
			exempt += len(run)
			continue
		}
		if derivesFrom(runText(run), hay) {
			continue
		}
		culprit := head
		for _, r := range run {
			if !derivesFrom(r.Text, hay) {
				culprit = r
				break
			}
		}
		provenance = append(provenance, provenanceBad{beat: fmt.Sprintf("beat %d", culprit.Beat),
			reason: fmt.Sprintf("the %d chunks of source line %d, joined (the stream the surface was handed), are not derivable from it; chunk %q is where the join breaks",
				len(run), head.SrcLine, previewOf(culprit.Preview))})
	}
	for _, p := range provenance {
		if len(v.Failures) >= maxReportedFindings {
			break
		}
		v.fail(p.beat, p.reason)
	}
	v.N["provenance_bad"] = len(provenance)
	v.N["provenance_exempt"] = exempt
	if len(provenance) == 0 {
		v.note("PROVENANCE OK: every beat's text is derivable from the source line it names (%d exempt as structure-only)", exempt)
	} else {
		v.note("%d provenance failure(s), %d listed", len(provenance), min(len(provenance), maxReportedFindings))
	}

	// ── VISIBILITY ──────────────────────────────────────────────────────────
	if !f.gridDirExists() {
		v.fail("dumps", fmt.Sprintf("no %s/ directory under %s — visibility cannot be measured", DumpsDirName, f.ScratchOr()))
		return v
	}
	turns, cur := [][]LedgerRow{}, []LedgerRow{}
	for _, r := range rows {
		cur = append(cur, r)
		if r.Kind == kindChrome && r.Text == chromeTurnEnd {
			turns = append(turns, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		turns = append(turns, cur)
	}
	v.N["turns"] = len(turns)
	v.note("%d turns in the ledger (delimited by the chrome beats)", len(turns))

	checked, visExempt := 0, 0
	type visBad struct{ beat, reason string }
	var visible []visBad
	for _, t := range turns {
		endBeat := t[len(t)-1].Beat
		gp := f.GridPath(endBeat)
		if _, err := os.Stat(gp); err != nil {
			visible = append(visible, visBad{beat: fmt.Sprintf("beat %d", endBeat),
				reason: fmt.Sprintf("no grid snapshot for the turn's last beat (%s)", filepath.Base(gp))})
			continue
		}
		gridRows, err := ReadGridRows(gp)
		if err != nil {
			visible = append(visible, visBad{beat: fmt.Sprintf("beat %d", endBeat), reason: err.Error()})
			continue
		}
		flat := ""
		for _, row := range gridRows {
			flat += normalize(row)
		}
		for _, run := range textRuns(t) {
			if exemptKinds[run[0].Kind] {
				visExempt += len(run)
				continue
			}
			text := runText(run)
			if normalize(text) == "" {
				visExempt += len(run)
				continue
			}
			runOK := runDrawn(text, flat, gridRows)
			if runOK {
				checked += len(run)
				continue
			}
			perBeatAll, unresolved := true, []LedgerRow{}
			for _, r := range run {
				if runDrawn(r.Text, flat, gridRows) {
					continue
				}
				if _, ok := tailOnGrid(r, flat, gridRows); ok {
					// A chunk whose head is a word's INTERIOR: its tail was drawn,
					// and the non-acceptance is NAMED (verify.py's mid_word list).
					v.N["mid_word_fragments"]++
					continue
				}
				perBeatAll = false
				unresolved = append(unresolved, r)
			}
			if perBeatAll {
				checked += len(run)
				continue
			}
			for _, r := range unresolved {
				visible = append(visible, visBad{beat: fmt.Sprintf("beat %d", r.Beat),
					reason: fmt.Sprintf("text %q is on no row of the grid at its turn's end (beat %d)", previewOf(r.Preview), endBeat)})
			}
		}
	}
	for _, b := range visible {
		if len(v.Failures) >= maxReportedFindings {
			break
		}
		v.fail(b.beat, b.reason)
	}
	v.N["visible_checked"] = checked
	v.N["visible_exempt"] = visExempt
	v.N["visible_bad"] = len(visible)
	if len(visible) == 0 {
		v.note("VISIBILITY OK: %d beats' text confirmed on their turn's own grid (%d exempt by kind)", checked, visExempt)
	}
	for kind, why := range exemptKinds {
		v.note("exempt kind %q: %s", kind, exemptKindReason(kind))
		_ = why
	}

	// ── ARITHMETIC ──────────────────────────────────────────────────────────
	// The metadata's own numbers must close: raw - distinct = dropped, and the
	// beat count must agree with the ledger's row count. Two artifacts, one
	// claim — a film whose metadata contradicts its ledger is not traceable.
	meta, ok, err := f.ReadMetadata()
	switch {
	case err != nil:
		v.fail("metadata", err.Error())
	case !ok:
		v.note("no %s in the film directory: the arithmetic clause is not measured (the metadata check reports it by name)", MetadataName)
	default:
		if meta.RawFrames-meta.DistinctFrames != meta.DroppedFrames {
			v.fail("arithmetic", fmt.Sprintf("frame arithmetic does not close: raw %d - distinct %d != dropped %d",
				meta.RawFrames, meta.DistinctFrames, meta.DroppedFrames))
		} else {
			v.note("arithmetic OK: %d raw - %d distinct = %d dropped", meta.RawFrames, meta.DistinctFrames, meta.DroppedFrames)
		}
		if meta.Beats != len(rows) {
			v.fail("beat count", fmt.Sprintf("%s says %d beats, the ledger holds %d", MetadataName, meta.Beats, len(rows)))
		} else {
			v.note("beat count OK: %d (ledger and %s agree)", len(rows), MetadataName)
		}
	}
	return v
}

// exemptKindReason names WHY a kind is exempt — verify.py's own two sentences
// (L172-L175), kept because an exemption without a reason is a silent pass.
func exemptKindReason(kind string) string {
	switch kind {
	case kindChrome:
		return "carries no text of its own (it is a layout event, not a message)"
	case kindRequest:
		return "one chunk of a line being typed; a chunk pushed off the grid's interior margin is unrecoverable by design (scrollback beyond the grid does not exist)"
	}
	return "unanchorable by kind"
}

// previewOf trims a preview to the 40 runes verify.py's messages quote, on a rune
// boundary (a byte slice here would cut a CJK glyph in half).
func previewOf(s string) string { return prefixRunes(s, 40) }

// ── 2. legibility ───────────────────────────────────────────────────────────

// MinPXPerChar is verify.py's MIN_PX_PER_CHAR (L157): px per character of the
// delivered frames, the property a READER needs. It is computed from the frame's
// measured window geometry divided by the terminal's column count, never from the
// file's width alone — a tall narrow GIF would pass a width-only check while its
// glyphs are unreadable.
const MinPXPerChar = 16.0

// pxPerCharSlack is verify.py's `ppc + 0.0005 < MIN_PX_PER_CHAR`: the arithmetic
// is done in floating point on both sides and the floor must not fail a film that
// lands exactly on it.
const pxPerCharSlack = 0.0005

// CheckLegibility is verify.py's `check_legibility` (L1279-L1313, 35 lines).
//
// TWO HALVES, and the second is why the first is not a claim about a number: the
// arithmetic above is about what the metadata SAYS, the measurement below is about
// the FILE — the delivered frame must actually be that wide, or a figure from one
// artifact was applied to another.
func CheckLegibility(f Film) Verdict {
	v := Verdict{Name: "2. legibility", Source: "L1279-L1313", N: map[string]int{}}
	meta, ok, err := f.ReadMetadata()
	switch {
	case err != nil:
		v.fail("metadata", err.Error())
		return v
	case !ok:
		v.fail("metadata", fmt.Sprintf("no %s at %s — the geometry is unrecorded", MetadataName, f.Out))
		return v
	}
	if len(meta.WindowPX) != 2 {
		v.fail("geometry", fmt.Sprintf("%s carries no window geometry (cols=%d window_px=%v)", MetadataName, meta.TermCols, meta.WindowPX))
		return v
	}
	w, h := meta.WindowPX[0], meta.WindowPX[1]
	if meta.TermCols == 0 || w == 0 {
		v.fail("geometry", fmt.Sprintf("%s carries no window geometry (cols=%d window_px=%v)", MetadataName, meta.TermCols, meta.WindowPX))
		return v
	}
	ppc := float64(w) / float64(meta.TermCols)
	v.note("window %dx%d px over %d columns = %.3f px per character (floor %.1f)", w, h, meta.TermCols, ppc, MinPXPerChar)
	if ppc+pxPerCharSlack < MinPXPerChar {
		v.fail("legibility", fmt.Sprintf("px per character %.3f is below the floor %.1f: the film's words are not readable", ppc, MinPXPerChar))
	} else {
		v.note("OK: at or above the floor")
	}

	frames, err := f.Frames()
	if err != nil {
		v.fail("frames", err.Error())
		return v
	}
	if len(frames) == 0 {
		v.fail("frames", fmt.Sprintf("no delivered frames under %s/%s", f.Out, FramesDirName))
		return v
	}
	fw, fh, err := pngSize(frames[0])
	if err != nil {
		v.fail("first frame", err.Error())
		return v
	}
	v.N["frame_width"] = fw
	v.N["frame_height"] = fh
	v.note("first delivered frame is %dx%d px", fw, fh)
	if fw != w {
		v.fail("width", fmt.Sprintf("the delivered frame is %dpx wide but the floor was computed at %dpx: a figure from one artifact was applied to another", fw, w))
	} else {
		v.note("OK: the measured width and the delivered width agree")
	}
	return v
}

// pngSize reads a PNG's dimensions from its header — the cheap half of the
// measurement verify.py got from PIL's Image.open.
func pngSize(path string) (int, int, error) {
	fh, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("open the frame %s: %w", path, err)
	}
	defer fh.Close()
	cfg, err := png.DecodeConfig(fh)
	if err != nil {
		return 0, 0, fmt.Errorf("read the frame header of %s: %w", path, err)
	}
	return cfg.Width, cfg.Height, nil
}

// ── 3. motion ───────────────────────────────────────────────────────────────

// MinFrames is verify.py's MIN_FRAMES (L160): fewer frames is a slide show, not
// an animation of a session.
const MinFrames = 12

// DiffTableName is the per-pair table check_motion writes beside the film
// (verify.py's `diff-table.tsv`, L1341).
const DiffTableName = "diff-table.tsv"

// CheckMotion is verify.py's `check_motion` (L1316-L1378, 63 lines).
//
// THE MEASUREMENT IS OVER THE FILM, NOT OVER A SAMPLE OF IT: the GIF is the
// artifact a reader watches, so its own frame count is what the floor applies to.
// With sampled=true the per-pair table is measured on the frames/ SAMPLE and the
// COUNT is measured on the GIF — the same split verify.py's --allow-sampled-frames
// makes, for the same reason (a 208-frame PNG set is 39 MB).
//
// WHERE THE PIXEL COMPARISON COMES FROM. verify.py shelled out to
// tools/human-ui/pxdiff.py and read its exit status as the verdict. This port
// compares the frames itself — Go's own decoders are the substitute for PIL, and
// keeping the comparison in-process is what lets the verdict be a Verdict. The
// TABLE it writes is byte-compatible with pxdiff's columns for the two columns a
// reader acts on (pair, changed pixels, percent), so the artifact stays readable.
func CheckMotion(f Film, sampled bool) Verdict {
	v := Verdict{Name: "3. motion", Source: "L1316-L1378", N: map[string]int{}}
	frames, err := f.Frames()
	if err != nil {
		v.fail("frames", err.Error())
		return v
	}
	gifPath := f.GIFPath()

	if sampled {
		if _, err := os.Stat(gifPath); err != nil {
			v.fail("film", fmt.Sprintf("sampled mode but there is no film at %s to take the count from", gifPath))
			return v
		}
		n, err := gifFrameCount(gifPath)
		if err != nil {
			v.fail("film", err.Error())
			return v
		}
		v.N["gif_frames"] = n
		if n < MinFrames {
			v.fail("film", fmt.Sprintf("the film has %d frames (floor %d): a slide show, not an animation", n, MinFrames))
			return v
		}
		v.note("the FILM carries %d frames (floor %d), measured on %s", n, MinFrames, filepath.Base(gifPath))
		if len(frames) < 2 {
			v.fail("sample", fmt.Sprintf("the %s/ sample holds %d file(s): motion cannot be measured on it", FramesDirName, len(frames)))
			return v
		}
		v.note("motion is measured on the %d-frame sample in %s/ (the film carries all %d)", len(frames), FramesDirName, n)
	} else if len(frames) < MinFrames {
		v.fail("frames", fmt.Sprintf("only %d delivered frames (floor %d): a slide show, not an animation", len(frames), MinFrames))
		return v
	}
	if !sampled {
		v.note("%d delivered frames (floor %d)", len(frames), MinFrames)
	}

	var prev *image.RGBA
	var rows []string
	var zeroPairs []string
	for i, path := range frames {
		cur, err := decodeRGBA(path)
		if err != nil {
			v.fail("frame", err.Error())
			return v
		}
		if prev != nil {
			changed, total, err := pixelDiff(prev, cur)
			if err != nil {
				v.fail("frames", err.Error())
				return v
			}
			pct := 0.0
			if total > 0 {
				pct = float64(changed) / float64(total) * 100
			}
			rows = append(rows, fmt.Sprintf("pair\t%s\t%s\t%d\t%d\t%.3f%%",
				filepath.Base(frames[i-1]), filepath.Base(path), changed, total, pct))
			if changed == 0 {
				zeroPairs = append(zeroPairs, fmt.Sprintf("%s -> %s", filepath.Base(frames[i-1]), filepath.Base(path)))
			}
		}
		prev = cur
	}
	table := filepath.Join(f.Out, DiffTableName)
	body := "pair\tfrom\tto\tchanged_px\ttotal_px\tchanged\n" + strings.Join(rows, "\n") + "\n"
	if err := os.WriteFile(table, []byte(body), 0o644); err != nil {
		v.fail("table", fmt.Sprintf("write the per-pair table %s: %v", table, err))
		return v
	}
	v.N["pairs"] = len(rows)
	v.N["zero_pairs"] = len(zeroPairs)
	v.note("OK: %d adjacent pair(s) measured; the per-pair table is %s", len(rows), DiffTableName)
	if len(rows) != len(frames)-1 {
		v.fail("pairs", fmt.Sprintf("the table holds %d pairs for %d frames (expected %d)", len(rows), len(frames), len(frames)-1))
	}
	// THE VERDICT verify.py took from pxdiff's exit status: an adjacent pair with
	// ZERO changed pixels is a stall, not motion. It is reported with its pair.
	for _, p := range zeroPairs {
		if len(v.Failures) >= maxReportedFindings {
			break
		}
		v.fail("motion", fmt.Sprintf("adjacent frames %s are byte-identical at the pixel level: the film stalls there", p))
	}
	// The smallest change is worth NAMING: a film whose most-unchanged pair is
	// near zero is moving, but barely, and a reader is entitled to know which pair.
	if len(rows) > 0 {
		lo := rows[0]
		loPct := pairPercent(rows[0])
		for _, r := range rows {
			if p := pairPercent(r); p < loPct {
				lo, loPct = r, p
			}
		}
		v.note("smallest change: %s", lo)
	}
	return v
}

// pairPercent reads the trailing percentage column of a table row.
func pairPercent(row string) float64 {
	fields := strings.Split(row, "\t")
	if len(fields) == 0 {
		return 0
	}
	last := strings.TrimSuffix(fields[len(fields)-1], "%")
	var pct float64
	fmt.Sscanf(last, "%f", &pct)
	return pct
}

// gifFrameCount decodes the animation's frame count — the number the floor is
// applied to (PIL's `n_frames`).
func gifFrameCount(path string) (int, error) {
	fh, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open the film %s: %w", path, err)
	}
	defer fh.Close()
	anim, err := gif.DecodeAll(fh)
	if err != nil {
		return 0, fmt.Errorf("decode the film %s: %w", path, err)
	}
	return len(anim.Image), nil
}

// decodeRGBA decodes a PNG into a form two frames can be compared pixel by pixel.
func decodeRGBA(path string) (*image.RGBA, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open the frame %s: %w", path, err)
	}
	defer fh.Close()
	img, err := png.Decode(fh)
	if err != nil {
		return nil, fmt.Errorf("decode the frame %s: %w", path, err)
	}
	rgba, ok := img.(*image.RGBA)
	if ok {
		return rgba, nil
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out, nil
}

// pixelDiff counts the pixels two frames disagree on. Differing BOUNDS are a
// refusal, not a diff: every frame of one film shares a canvas (internal/gifgen
// refuses a mismatched frame by name, so this is the checker's own half of the
// same rule).
func pixelDiff(a, b *image.RGBA) (int, int, error) {
	if a.Bounds().Dx() != b.Bounds().Dx() || a.Bounds().Dy() != b.Bounds().Dy() {
		return 0, 0, fmt.Errorf("frames of different sizes (%dx%d vs %dx%d): a film's frames share a canvas",
			a.Bounds().Dx(), a.Bounds().Dy(), b.Bounds().Dx(), b.Bounds().Dy())
	}
	w, h := a.Bounds().Dx(), a.Bounds().Dy()
	changed := 0
	// The comparison walks the backing slices, not At(): this is the hot loop of
	// the criterion (measured on the real artifact in the report).
	rowBytes := w * 4
	for y := 0; y < h; y++ {
		off := y * a.Stride
		for x := 0; x < rowBytes; x += 4 {
			i := off + x
			if a.Pix[i] != b.Pix[i] || a.Pix[i+1] != b.Pix[i+1] ||
				a.Pix[i+2] != b.Pix[i+2] || a.Pix[i+3] != b.Pix[i+3] {
				changed++
			}
		}
	}
	return changed, w * h, nil
}

// ── 4. withholding ──────────────────────────────────────────────────────────

// CheckWithholding is verify.py's `check_withholding` (L1381-L1471, 91 lines).
//
// TWO HALVES, because either alone is vacuous:
//
//	POSITIVE CONTROL — the pass must have had work to do. "no identifier in the
//	  film" is trivially true of a run whose source never held one, and a vacuous
//	  pass reads as evidence.
//	CONTAINMENT — every run the pass recorded must be ABSENT from every delivered
//	  artifact, not merely absent from the pixels.
//
// WHY THERE IS NO RASTER CONTAINMENT CHECK (the source's own reason, kept): this
// film masks at the TEXT level, before the editor is handed the bytes, so the
// identifier never reaches a pixel. What has teeth here is that the mask is not
// reintroduced later, so the per-beat grids are scanned too — a shape on a grid
// means a shape the editor was handed.
//
// THE IDENTIFYING SHAPES ARE NOT DEFINED HERE: internal/replay.IdentShapes and
// RedactionStandin are the ONE definition (Rule 14), which is what makes a leak
// of the SHAPES visible to both the masker and this check.
func CheckWithholding(f Film) Verdict {
	v := Verdict{Name: "4. withholding", Source: "L1381-L1471", N: map[string]int{}}

	runs, err := f.RedactedRuns()
	if err != nil {
		v.fail("redaction log", err.Error())
		return v
	}
	v.N["redacted_runs"] = len(runs)
	if len(runs) == 0 {
		v.fail("positive control", "the redaction pass replaced NOTHING: the withholding claim would be vacuous "+
			"(either the source held no identifying run, or the masker's shapes did not match)")
	} else {
		v.note("POSITIVE CONTROL OK: %d identifying run(s) were actually replaced", len(runs))
	}

	type leak struct{ file, reason string }
	var leaks []leak
	binaryFiles, textFiles := 0, 0
	err = filepath.Walk(f.Out, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			// A file the scan cannot read is NOT a pass: it is reported, because a
			// scan that found nothing must be distinguishable from one that never
			// ran (Rule 15). An unreadable delivered file is a finding in itself.
			leaks = append(leaks, leak{file: relOf(f.Out, path), reason: fmt.Sprintf("the scan could not read it: %v", err)})
			return nil
		}
		rel := relOf(f.Out, path)
		if looksBinary(blob) {
			// The delivered PNG/GIF bytes are binary, so a run cannot be grepped
			// for; what the mask guarantees is that no TEXT artifact carries one.
			binaryFiles++
			return nil
		}
		textFiles++
		for _, m := range replay.IdentShapes.FindAllString(string(blob), -1) {
			leaks = append(leaks, leak{file: rel, reason: fmt.Sprintf("still carries the shape %s", previewOf(m)+"…")})
		}
		for _, run := range runs {
			if run != "" && strings.Contains(string(blob), run) {
				leaks = append(leaks, leak{file: rel, reason: "still carries a redacted run verbatim"})
			}
		}
		return nil
	})
	if err != nil {
		v.fail("scan", fmt.Sprintf("walk the delivered film %s: %v", f.Out, err))
		return v
	}
	v.N["binary_files"] = binaryFiles
	v.N["text_files"] = textFiles
	for _, l := range leaks {
		if len(v.Failures) >= maxReportedFindings {
			break
		}
		v.fail("LEAK", fmt.Sprintf("%s: %s", l.file, l.reason))
	}
	v.N["text_leaks"] = len(leaks)
	if len(leaks) == 0 {
		v.note("TEXT CONTAINMENT OK: no delivered artifact carries a masked run or an identifying shape (%d text file(s), %d binary file(s))", textFiles, binaryFiles)
	}

	// The grids, which are the surface's own record of what it drew.
	if f.gridDirExists() {
		entries, err := os.ReadDir(filepath.Join(f.ScratchOr(), DumpsDirName))
		if err != nil {
			v.fail("grids", err.Error())
			return v
		}
		gridLeaks := 0
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			blob, err := os.ReadFile(filepath.Join(f.ScratchOr(), DumpsDirName, e.Name()))
			if err != nil {
				gridLeaks++
				v.fail("grid "+e.Name(), fmt.Sprintf("the scan could not read it: %v", err))
				continue
			}
			for _, m := range replay.IdentShapes.FindAllString(string(blob), -1) {
				gridLeaks++
				if gridLeaks <= maxReportedFindings {
					v.fail("grid "+e.Name(), fmt.Sprintf("carries the shape %s: the editor was handed it", previewOf(m)+"…"))
				}
			}
		}
		v.N["grid_leaks"] = gridLeaks
		if gridLeaks == 0 {
			v.note("GRID OK: no dumped grid carries an identifying shape")
		}
	}
	return v
}

// looksBinary reports whether a delivered file is one the text scan cannot read —
// a PNG or GIF by its signature, or a NUL byte within the first 8 KiB.
func looksBinary(blob []byte) bool {
	for _, sig := range [][]byte{{0x89, 'P', 'N', 'G'}, {'G', 'I', 'F', '8'}} {
		if len(blob) >= len(sig) && string(blob[:len(sig)]) == string(sig) {
			return true
		}
	}
	n := len(blob)
	if n > 8192 {
		n = 8192
	}
	for i := 0; i < n; i++ {
		if blob[i] == 0 {
			return true
		}
	}
	return false
}

func relOf(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

// ── 5. colour ───────────────────────────────────────────────────────────────

// CheckColour is verify.py's `check_colour` (L1533-L1576, 44 lines).
//
// WHY THIS CLAIM EXISTS (the source's own words, and they are measured): the film
// is the product's surface photographed, and the surface is coloured. A run that
// loses the capability decision draws the SAME text with the SAME layout and NO
// colour — every other claim in this file stays true and the delivered GIF is a
// grey film of a colour product. The arm delivered 2026-10-02 had zero chromatic
// pixels in every frame.
//
// WHY IT IS A HUE COUNT AND NOT "SOME COLOURED PIXELS": this terminal's background
// is (19,25,38), so a "fraction of coloured pixels" test would PASS on a film with
// no colour of gopi's at all — it would be measuring the background's own hue. So
// the measurement is the number of DISTINCT HUE BUCKETS carried by genuinely
// saturated, genuinely bright pixels, AND the anchored palette roles.
func CheckColour(f Film) Verdict {
	v := Verdict{Name: "5. colour", Source: "L1533-L1576", N: map[string]int{}}
	gifPath := f.GIFPath()
	if _, err := os.Stat(gifPath); err != nil {
		v.fail("film", fmt.Sprintf("no film at %s: the colour of the delivered artifact cannot be measured", gifPath))
		return v
	}
	m, err := MeasureColour(gifPath)
	if err != nil {
		v.fail("film", err.Error())
		return v
	}
	v.N["frames"] = m.TotalFrames
	v.N["chromatic_pixels"] = m.Chromatic
	v.N["hue_buckets"] = len(m.Hues)
	v.N["saturated_rgb_kinds"] = m.Kinds
	v.N["palette_roles"] = len(m.Roles)
	v.N["palette_entries"] = m.PaletteEntries
	v.N["palette_used"] = m.UsedPaletteEntries
	v.N["palette_saturated"] = m.SaturatedPaletteEntries
	v.note("the film's %d frame(s) carry %d distinct hue bucket(s) of saturated, bright ink (%d such pixels; %d distinct saturated bright RGB kind(s); floors: saturation >= %.2f, value >= %.2f)",
		m.TotalFrames, len(m.Hues), m.Chromatic, m.Kinds, ColourMinSat, ColourMinValue)
	v.note("palette: %d slot(s), %d referenced, %d saturated (the slot count is reported, never decided on: it saturates at the table's size)",
		m.PaletteEntries, m.UsedPaletteEntries, m.SaturatedPaletteEntries)
	if m.Chromatic < MinColouredPixels || len(m.Hues) < MinHueBuckets {
		v.fail("palette did not reach the artifact",
			fmt.Sprintf("the film has %d saturated bright pixel(s) in %d hue bucket(s) (floors %d / %d): this is a grey-scale film of a colour product (its background is blue-tinted and is excluded by the value floor on purpose)",
				m.Chromatic, len(m.Hues), MinColouredPixels, MinHueBuckets))
		return v
	}
	v.note("palette roles present: %s", strings.Join(m.Roles, ", "))
	if len(m.Roles) < MinPaletteRoles {
		roles, err := PaletteRoles()
		if err != nil {
			// The film's colours cannot be compared against a palette that does
			// not resolve: the failure is the criterion's, not a note (Rule 15).
			v.fail("palette table", "the declared palette roles could not be resolved: "+err.Error()+": the artifact cannot be checked against a palette whose own anchors do not convert")
			return v
		}
		names := make([]string, 0, len(roles))
		for _, r := range roles {
			names = append(names, r.Name)
		}
		v.fail("colour but not THE palette",
			fmt.Sprintf("none of gopi's declared palette colours is on the film (%s, tolerance %d): the artifact carries colour, but not THE PALETTE — the surface drew its fallback table",
				strings.Join(names, ", "), PaletteRGBTolerance))
		return v
	}
	v.note("OK: gopi's own palette is on the artifact, not only in the renderer")
	return v
}

// runtimeGo reports the toolchain that produced a measurement — quoted in the
// report beside the port's own timing, because the colour pass is the one whose
// substitute (scalar Go arithmetic for numpy) has a performance claim attached.
func runtimeGo() string { return runtime.Version() }

// The package's standing description — the criteria's line-by-line provenance, the
// substitutions and their reasons, the evidence sets the suite drives, the oracle
// recipe and the honest gaps — is devdoc/film-assembly-and-verification.md.
