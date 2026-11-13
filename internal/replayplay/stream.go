package replayplay

// stream.go — THE VERBATIM LEG: a recorded screen stream read into the material.
//
// WHY THIS IS THE PREFERRED SOURCE, and the dump is the fallback. `chat-screen.dump`
// is this program's INTERPRETATION of a paint sequence: it is throttled
// (dumpMinInterval), and it is reduced to the cells the grid model can express. The
// session JSONL is a second opinion of the same class: it carries events, and any
// picture built from it is OUR rendering of them. A screen stream
// (`<session>.screen.stream`, or the pre-rename `.sgr.stream`) is the bytes the
// renderer WROTE — nothing was parsed on the way in, so drawing it answers what the
// screen actually was, on this build or any other.
//
// SO THE SOURCE LINE MUST NEVER LOOK ALIKE. Rule 15 is not a style preference here:
// a reader who cannot tell a verbatim replay from a reconstructed one will trust the
// wrong one, and the difference is exactly the thing this mechanism exists to
// measure. Hence two labels (SourceLabelStream / SourceLabelDump), printed on the
// same line the old `source=` occupied.
//
// THE FREAMES ARE INTERPRETED AT READ TIME, ON PURPOSE. The stream is raw bytes; to
// STEP it as pictures this package must paint them, and painting is internal/lineedit's
// job — the same interpreter the live surface uses, so a replay and the live screen
// cannot disagree. What stays verbatim is the BYTES, which is why `--verify` compares
// them frame for frame without consulting this interpreter at all.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/uiclient"
)

// The header's `source=` vocabulary. Two spellings, and the parenthesis carries the
// reason a reader must not confuse them: `verbatim` = the renderer's own bytes;
// `interpreted, throttled` = this program's reduced, sampled picture of them.
const (
	// SourceLabelStream is the verbatim leg.
	SourceLabelStream = "sgr-stream(verbatim)"
	// SourceLabelDump is the fallback, and it names BOTH of its limits in the same
	// breath so nobody has to know what dumpMinInterval is to read the line honestly.
	SourceLabelDump = "dump(interpreted, throttled)"
)

// streamStepFrameCap bounds how many recorded frames this package snapshots into
// steppable Frame values. A long chat records thousands of paint writes (measured on
// this host: 4,084 frames / 310,521 payload bytes in one session), and a snapshot per
// frame is a full grid copy; the frames a reader actually steps are the TAIL of the
// recording, so the cap keeps the first frame (what the screen opened as) and the last
// N. The count that was NOT snapshotted is stated by the caller, never hidden.
const streamStepFrameCap = 120

// ScreenStream is a recording read as steppable frames, plus the facts a caller
// reports about it.
type ScreenStream struct {
	// Path is the artifact these frames came from.
	Path string
	// Meta is the header/trailer reading (`grid=`, `revision=`, `started=`, and
	// whether the run reached close-out at all).
	Meta uiclient.SgrStreamMeta
	// Total is how many frames the FILE holds — not how many were snapshotted.
	Total int
	// PayloadBytes is the counted payload total (never the trailer's declaration).
	PayloadBytes int64
	// Frames are the steppable pictures, oldest first. When Total >
	// streamStepFrameCap this holds the FIRST frame and the last
	// streamStepFrameCap-1, and Sampled says so.
	Frames []Frame
	// Sampled is true when Total exceeded the cap and Frames is a sample.
	Sampled bool
	// LegacyHeader records that the file carries the pre-rename header spelling.
	// It is a READING: the bytes parse identically either way, and a caller that
	// normalized it away would hide which generation of recorder wrote the file.
	//
	// IT IS ONLY MEANINGFUL WHEN HeaderUnreadable IS EMPTY. `false` is a CLAIM
	// ("this header carries the current spelling"), so it may not stand for "the
	// first line was never read" — that is what HeaderUnreadable is for. A caller
	// that renders the reading must ask HeaderUnreadable first.
	LegacyHeader bool
	// HeaderUnreadable says the header line could not be read, so LegacyHeader
	// carries no reading at all. It is a DEGRADE, not a failed load: the frames
	// were parsed by the reader above and are steppable, and this field exists so
	// the provenance question can be reported as unanswered (Rule 15) instead of
	// answering itself with a default that happens to look like a finding.
	HeaderUnreadable string
}

// ScreenStreamPath resolves the stream artifact a session directory holds, if any.
//
// BOTH SUFFIXES ARE ASKED FOR, newest first, through uiclient's ONE list — a reader
// that spelled `.screen.stream` itself would stop seeing the 177 recordings this
// host made before the rename (measured 2026-11-08: 177 `.sgr.stream` against 4
// `.screen.stream`). A directory holding both
// (a session that was recorded across the rename boundary) resolves to the CURRENT
// spelling: the newer artifact is the more complete recording of the two.
func ScreenStreamPath(dir string) (string, bool) {
	for _, suf := range uiclient.ScreenStreamSuffixes {
		cand := filepath.Join(dir, filepath.Base(filepath.Clean(dir))+suf)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, true
		}
	}
	// A renamed/moved session directory keeps working: the file's name carries the
	// session id it was recorded under, so the directory's own name is only the
	// first guess. The glob is over the SUFFIX list, so this cannot pick up an
	// unrelated artifact.
	for _, suf := range uiclient.ScreenStreamSuffixes {
		matches, err := filepath.Glob(filepath.Join(dir, "*"+suf))
		if err != nil || len(matches) == 0 {
			continue
		}
		return matches[0], true
	}
	return "", false
}

// LoadScreenStream reads a recording into steppable frames.
//
// THE INTERPRETER IS internal/lineedit's, and the grid is the HEADER's — the size the
// sink was BUILT at. A stream recorded across a resize carries the end geometry in its
// trailer; this reader paints on the header's grid because that is what the first
// frame was painted for, and a caller that wants the end grid reads Meta.EndGrid. The
// difference is REPORTED rather than silently reconciled: two different grids are a
// finding (the run was resized), not a parse error.
func LoadScreenStream(path string) (ScreenStream, error) {
	var out ScreenStream
	meta, raw, err := uiclient.ReadSgrFramesAtPath(path)
	if err != nil {
		return out, err
	}
	out.Path = path
	out.Meta = meta
	out.Total = len(raw)
	for _, f := range raw {
		out.PayloadBytes += int64(len(f.Payload))
	}
	if out.Total == 0 {
		// A stream that recorded no frame is not a picture. Saying so beats handing
		// the player an empty sequence it would render as a blank screen (Rule 15).
		return out, fmt.Errorf("%s holds no frame: the recording opened and wrote no paint bytes, so there is nothing to step", path)
	}
	cols, rows := meta.Grid[0], meta.Grid[1]
	if cols <= 0 || rows <= 0 {
		return out, fmt.Errorf("%s declares grid=%dx%d, which cannot be painted: a screen needs both dimensions", path, cols, rows)
	}
	// THE HEADER READING IS TAKEN BEFORE THE LEGACY FLAG, and a failure to read it
	// is RECORDED rather than folded into `false`: see screenStreamHeaderOf's note
	// and ScreenStream.HeaderUnreadable.
	if hdr, herr := screenStreamHeaderOf(path); herr != nil {
		out.HeaderUnreadable = herr.Error()
	} else {
		out.LegacyHeader = uiclient.SgrStreamHeaderIsLegacy(hdr)
	}
	keep := screenStreamKeptIndexes(out.Total)
	out.Sampled = len(keep) != out.Total
	screen := lineedit.NewScreen(cols, rows)
	next := 0
	for i, f := range raw {
		screen.Feed(f.Payload)
		if next >= len(keep) || keep[next] != i {
			continue
		}
		next++
		out.Frames = append(out.Frames, Frame{
			Kind:  FrameScreen,
			Label: fmt.Sprintf("frame %d/%d", i+1, out.Total),
			Rows:  screenStreamRows(screen, rows),
		})
	}
	return out, nil
}

// screenStreamKeptIndexes is which frame indexes are snapshotted: all of them when
// the recording is short, else the first plus the last cap-1. It is a pure function so
// the sampling policy is testable without a recording of 4,000 frames.
func screenStreamKeptIndexes(total int) []int {
	if total <= streamStepFrameCap {
		keep := make([]int, total)
		for i := range keep {
			keep[i] = i
		}
		return keep
	}
	keep := make([]int, 0, streamStepFrameCap)
	keep = append(keep, 0)
	for i := total - (streamStepFrameCap - 1); i < total; i++ {
		keep = append(keep, i)
	}
	return keep
}

// screenStreamRows snapshots a painted grid as FrameRows. The cells come from
// Screen.Line, which is rune-per-cell with a wide rune's continuation column
// already skipped — the same shape the dump leg produces, so the renderer cannot
// tell the two sources apart by the row it is handed.
func screenStreamRows(s *lineedit.Screen, rows int) []FrameRow {
	out := make([]FrameRow, 0, rows)
	for r := 0; r < rows; r++ {
		out = append(out, FrameRow{Cells: s.Line(r)})
	}
	return out
}

// screenStreamHeaderProbeBytes is how much of the file screenStreamHeaderOf reads.
// It is a PROBE, not a line limit: the header record is a prefix-carrying line far
// shorter than this, and the legacy test only asks about the prefix, so a recording
// whose first line were somehow longer is still answered correctly from the part
// that was read.
const screenStreamHeaderProbeBytes = 256

// screenStreamHeaderOf reads the recording's first line, for the legacy-spelling
// READING only, and it reports WHY when it cannot.
//
// IT USED TO RETURN "" ON FAILURE, AND THAT WAS A FALSE READING RATHER THAN A
// MISSING ONE. The caller turns this string into `SgrStreamHeaderIsLegacy`, whose
// answer is a BOOL: an unreadable file produced "" → `false` → the material
// published `LegacyHeader=false`, which in the field's own words claims "this file's
// header carries the CURRENT spelling". So a read that never happened came out as a
// positive claim about a line nobody saw — and the frames were still drawn, so the
// replay looked entirely normal (Rule 15: the degrade must be stated, not implied by
// a zero value).
//
// A SHORT READ IS NOT A FAILURE: `Read` on a regular file returns fewer than
// screenStreamHeaderProbeBytes bytes whenever the file is shorter than the buffer,
// usually with io.EOF, and the bytes it returned are still the first line. Only a
// read that yields NO byte — or a real error alongside it — leaves the question
// unanswered, and that is what comes back as an error.
func screenStreamHeaderOf(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //stylegate:ignore a read-only handle; the close cannot change the reading
	buf := make([]byte, screenStreamHeaderProbeBytes)
	n, rerr := f.Read(buf)
	if n == 0 {
		if rerr == nil {
			return "", fmt.Errorf("%s returned no byte for its first line", path)
		}
		return "", fmt.Errorf("%s holds no header line to read: %w", path, rerr)
	}
	line, _, _ := strings.Cut(string(buf[:n]), "\n")
	return line, nil
}

// The caller's SOURCE CHOICE (`gopi session replay --from …`). StreamFromAuto is
// the default and the reason this epic exists: the verbatim leg wins whenever the
// session has one. StreamFromDump is the deliberate downgrade, kept because a
// render-regression question sometimes needs THIS build's interpretation of the
// dump rather than the bytes — and because a criterion that cannot be overridden
// cannot be tested.
const (
	StreamFromAuto   = "auto"
	StreamFromStream = "stream"
	StreamFromDump   = "dump"
)

// ParseStreamFrom validates the `--from` operand. It refuses an unknown word BY
// NAME (Rule 15) rather than silently replaying the default: a caller who typed
// `--from dum` believes they forced the dump, and answering with the stream would
// make the flag a decoration.
func ParseStreamFrom(word string) (string, error) {
	switch w := strings.TrimSpace(word); w {
	case "":
		return StreamFromAuto, nil
	case StreamFromAuto, StreamFromStream, StreamFromDump:
		return w, nil
	default:
		return "", fmt.Errorf("--from takes %s, %s or %s — %q names no source",
			StreamFromAuto, StreamFromStream, StreamFromDump, word)
	}
}

// loadStreamLeg resolves and reads the verbatim leg for a session directory.
//
// THREE OUTCOMES, and a caller can tell all three apart:
//   - a usable recording → (stream, "", nil), and its grid becomes the material's;
//   - nothing recorded     → (nil, "", nil). An ORDINARY state (a worker run paints
//     no screen), so it is not an error and no empty artifact is invented;
//   - recorded but unusable → (nil, note, err). The err is carried so the caller
//     can refuse honestly when nothing else has frames; the NOTE travels on the
//     material so a session that still replays from its dump says WHY the better
//     artifact was passed over.
//
// A `from` of StreamFromDump never even looks: the caller asked for the dump.
func loadStreamLeg(dir, from string, cols, rows *int, geoSource *string) (*ScreenStream, string, error) {
	if from == StreamFromDump {
		return nil, "forced by --from " + StreamFromDump, nil
	}
	path, ok := ScreenStreamPath(dir)
	if !ok {
		if from == StreamFromStream {
			// `--from stream` is a REQUIREMENT, not a preference: the caller asked
			// for the renderer's own bytes and this session has none. Falling back
			// to the dump here would answer a different question than the one asked,
			// and the answer would look identical to a correct one (Rule 15).
			return nil, "", fmt.Errorf("session %s recorded no screen stream (%s holds no %s file) and --from %s requires one: a run that painted no screen — a worker run is the ordinary case — has no verbatim bytes to draw. Use --from %s for the dump instead",
				filepath.Base(filepath.Clean(dir)), dir, uiclient.ScreenStreamSuffix, StreamFromStream, StreamFromDump)
		}
		return nil, "", nil
	}
	stream, err := LoadScreenStream(path)
	if err != nil {
		return nil, fmt.Sprintf("%s is unreadable (%v)", filepath.Base(path), err), err
	}
	// THE RECORDING'S OWN GRID WINS over both the dump and the terminal probe:
	// these are the bytes that were painted at that size, so painting them
	// anywhere else would re-wrap a screen that was never wrapped that way.
	if stream.Meta.Grid[0] > 0 && stream.Meta.Grid[1] > 0 {
		*cols, *rows = stream.Meta.Grid[0], stream.Meta.Grid[1]
		*geoSource = GeometryFromStream
	}
	return &stream, "", nil
}
