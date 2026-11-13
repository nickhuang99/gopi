package lineedit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/nick/gopi/internal/sgr"
)

// Long pastes are SPILLED to a file instead of being typed into the buffer.
//
// WHY: the editing buffer is redrawn in place on every keystroke, so a pasted
// megabyte is a megabyte the editor re-lays-out and repaints — the buffer's own
// bound (valueMaxRunesDefault/valueMaxBytesDefault) refuses it partway, and the
// user watches a frozen prompt for seconds while it does (see Value.InsertRunes
// for the measured cost of growing the buffer). A paste that is spilled costs
// the string it is replaced by: the buffer holds the REFERENCE, the terminal
// holds one short line, and the file holds the bytes.
//
// WHAT COUNTS AS A LONG PASTE is not a taste: it is ONE FULL read (readBufBytes,
// 4096, less the one-byte shortfall a pty reports — see PasteThreshold). A human
// typing produces one key per read at most, so a single read that big cannot be
// typing — it is a paste (a terminal writes a pasted block as whole chunks), a
// redirect, or a bracketed-paste body.
//
// AND A PASTE IS ONE THING: it does not arrive as whole read buffers (measured: a
// 64 KiB paste reaches the loop as sixteen 4095-byte reads plus a 16-byte tail),
// so the reads after the first are CONTINUATIONS of the same paste and are
// appended to the SAME file — one paste, one file, one reference.
//
// WHERE the file goes is not spelled here either: the caller hands in the SESSION
// directory (chat resolves it from its own session log), and the files live in
// one subdirectory of it (PasteDirName) so a session's spills are found, listed
// and swept as a unit.
const (
	// PasteDirName is the subdirectory of a session directory that holds the
	// spilled pastes.
	PasteDirName = "paste"
	// PasteFilePrefix and PasteFileSuffix name one spill file:
	// paste-<stamp>-<n>.txt.
	PasteFilePrefix = "paste-"
	PasteFileSuffix = ".txt"
	// PasteStampLayout is the file name's timestamp (UTC, to the second, '-' and
	// ':' free so the name is a plain component on every filesystem).
	PasteStampLayout = "20060102-150405"
	// pasteDirPerm / pasteFilePerm: the directory is the session's own (0755,
	// like every other session directory) and the file carries the user's own
	// pasted bytes, so it is 0600 like the history ring.
	pasteDirPerm  = 0o755
	pasteFilePerm = 0o600
	// pasteMaxNameTries bounds the collision walk for one stamp: it is a bound on
	// a name generator, not a behaviour value.
	pasteMaxNameTries = 1000
)

// Bracketed-paste framing. gopi's own raw mode turns bracketed paste ON
// (raw.go: escBracketedOn), so a terminal that supports it wraps every pasted
// block in CSI 200 ~ … CSI 201 ~ — and the DECODER's job is to drop those two
// markers (KeyPasteStart/KeyPasteEnd, keys.go) rather than type them.
//
// The spill path writes a read to disk BEFORE the decoder sees it, so it must
// drop the same two markers ITSELF, or the file the user is handed carries the
// terminal's framing as if it were content.
//
// MEASURED 2026-09-28, and it is not an edge case: a paste is NOT delivered as
// one read, and the markers do not sit together at the front of the stream —
// against a real pty, a 42 KiB bracketed paste spilled 42012 bytes for 42000
// bytes of content, and the FIRST byte of the file was the start marker: the
// 4095-byte first read is all content, so the start marker was the first bytes of
// the SECOND read (pasted inline at the head of the file) and the end marker fell
// at the head of a LATER read again. The file was therefore corrupt for every
// paste big enough to spill — the very case the spill exists for.
//
// Only the markers are dropped, and only when they stand WHOLE in one read: a
// marker split across two reads is not recognised (its halves are then content,
// which is the same conservative choice the continuation rule makes at its own
// boundary), and every other byte — including a pasted ESC, a CSI the user really
// pasted, control bytes and invalid UTF-8 — is filed verbatim.
// PasteStartSeq / PasteEndSeq are the bracketed-paste framing as it arrives on
// the wire (the terminal's own spelling, not a mode-setting sequence). They are
// composed through internal/sgr — the ONE owner of the escape vocabulary
// (Rule 13/14) — so this file never types an escape byte itself.
var (
	PasteStartSeq = sgr.BracketedPasteMarkerSeq(sgr.PasteMarkerStartParam)
	PasteEndSeq   = sgr.BracketedPasteMarkerSeq(sgr.PasteMarkerEndParam)
)

// pasteStartMarker / pasteEndMarker are those sequences as the BYTES this file
// matches on: a read arrives as bytes, so the framing is recognised byte-wise.
var (
	pasteStartMarker = []byte(PasteStartSeq)
	pasteEndMarker   = []byte(PasteEndSeq)
)

// stripPasteMarkers removes the bracketed-paste framing from one read's bytes.
// An EMPTY result means the read carried nothing but framing (see the callers).
//
// THE MARKERS ARE ANCHORED TO THE READ'S EDGES — start at the head, end at the
// tail — and that is not a guess about the wire, it is what the wire does: a
// terminal writes the framing around the block it is pasting, so the bytes it
// flanks are the paste's own, and the measured case is the head one (a 4095-byte
// read began with the start marker followed by 4083 bytes of content).
//
// ANCHORING IS WHAT KEEPS THE RULE FROM EATING CONTENT. Searching anywhere in the
// read would drop a pasted sequence that merely LOOKS like the framing (a user
// pasting text that contains the literal bytes) — the failure this rule exists to
// prevent, inverted. Content in the MIDDLE of a read is never touched, so a
// pasted ESC, a pasted CSI, control bytes and invalid UTF-8 all reach the file
// verbatim.
//
// A marker SPLIT across two reads is not recognised here (its halves are then
// content): reading further would mean holding bytes back, and holding is exactly
// what the spill path must not do — it files what arrived, when it arrived.
func stripPasteMarkers(data []byte) []byte {
	if bytes.HasPrefix(data, pasteStartMarker) {
		data = data[len(pasteStartMarker):]
	}
	if bytes.HasSuffix(data, pasteEndMarker) {
		data = data[:len(data)-len(pasteEndMarker)]
	}
	return data
}

// pasteSeq makes two spills written inside one second distinct (the stamp is
// second-resolution, and a fast paste can produce a second chunk immediately).
var pasteSeq atomic.Uint64

// PasteRef is the placeholder a spilled paste leaves on the input line.
//
// Its spelling is fixed and machine-readable: `[paste N: /abs/path]`. The number
// N is the ordinal OF THE BYTES, not of the file — it is the rune count of the
// pasted content — so the reader can tell how much content the reference stands
// for, and a line carrying two spills states both sizes. The path is ABSOLUTE, so
// the file can be opened from any working directory (the reference is the only
// handle the user has on the bytes).
//
// PasteRef is the ONE producer of the spelling and PasteRefLen the ONE measure
// of it; the reader (a human, or the model) sees exactly what this renders.
//
// SOURCE AND SIZE ARE THE EXTENSION (2026-10-06). Size is an exact BYTE count
// beside the rune count, because the two differ for any non-ASCII payload and a
// reader that had only one of them would guess wrong about the other. Source
// names the face that spilled the bytes (internal/inputsource: human / queue /
// notify / llm) — a reader must be able to tell "the operator sent this" from "another
// agent sent this", and a queue message's trust does not survive that fact being
// dropped.
//
// BOTH ARE OPTIONAL ON THE WAY IN, which is what makes a reference written
// before this extension readable: a zero Size and an empty Source render
// NOTHING, so an old-shaped reference is produced by the same renderer that
// produces the new one (Rule 15a — an absent optional field stays absent; the
// renderer never invents a provenance).
type PasteRef struct {
	// Path is the absolute path of the spilled file.
	Path string
	// Bytes is how many BYTES the file holds (the pasted bytes, verbatim). It
	// renders as the reference's `size` field.
	Bytes int
	// Runes is how many runes those bytes decode to.
	Runes int
	// Source names the face that spilled the payload ("" = not recorded).
	Source string
}

// PasteRefFor renders the reference for one spilled paste.
//
// The `source` and `size` fields are appended ONLY when they carry something, so
// this single function renders both the old shape (`[paste N: /abs/path]`, from
// a PasteRef that predates the extension or a caller that has no provenance to
// state) and the new one (`[paste N: /abs/path source=human size=12B]`). One
// renderer, two shapes, no branch for the READER to keep in sync — the reader
// tolerates the absence (see PasteRefInfo).
func PasteRefFor(p PasteRef) string {
	ref := refOpen + PasteRefKeyword + refFieldGap + strconv.Itoa(p.Runes) + refSep + refFieldGap + p.Path
	if p.Source != "" {
		ref += refFieldGap + refSourceKey + refAssign + p.Source
	}
	if p.Bytes > 0 {
		ref += refFieldGap + refSizeKey + refAssign + formatPasteSize(p.Bytes)
	}
	return ref + refClose
}

// PasteRefKeyword is the reference s marker word — the vocabulary a reader (and
// the tests that assert the rendering) share, in one place.
//
// It is EXPORTED because a reader outside this package identifies a reference by
// it (the PTY suite scans the child's own output for the marker, and a test that
// spelled "paste" itself would keep passing after the marker changed).
const PasteRefKeyword = "paste"

// PasteThreshold is how many bytes of ONE read make it a paste rather than
// typing: the read buffer (readBufBytes), less the one-byte shortfall a real
// terminal's read reports.
//
// THE SHORTFALL IS MEASURED, not defensive: a 64 KiB paste written to a pty
// reaches this loop as reads of 4095 bytes (the pty's own accounting takes the
// byte the buffer's length assumes), so a strictly-equal comparison against
// readBufBytes would never fire on the very delivery the threshold exists for —
// measured 2026-09-28 with a pty driver against `gopi chat`: chunk=4095,
// canSpill=false, and the 64 KiB paste was typed into the buffer.
const (
	// readChunkShortfall is how far short of the read buffer a full read may
	// report.
	readChunkShortfall = 1
	// PasteThreshold is the paste criterion: one read this large cannot be
	// typing.
	PasteThreshold = editReadBuf - readChunkShortfall
	// pasteContinuationGap is how long after a spilled read the next one may
	// arrive and still belong to the SAME paste. It is the editor's existing
	// "these arrived as one paste" window (PasteEnterSuppressWindow — the same
	// notion the pasted-newline suppression uses), not a second number.
	//
	// WHY CONTINUATION EXISTS AT ALL (measured): a paste is not delivered in
	// whole read buffers. 64 KiB written to a pty arrives as 16 reads of 4095
	// bytes PLUS a 16-byte tail, so a rule that spilled only full reads filed the
	// content but typed the tail — the message then read
	// `[paste 4095: …]…[paste 4095: …]pasted content line with some words in it`,
	// mixing a reference with escaped content. A paste is one thing, so its
	// bytes go to one file.
	pasteContinuationGap = PasteEnterSuppressWindow
	// pasteContinuationMinBytes is the smallest read that can be a paste
	// CONTINUATION. Below it a read is indistinguishable from a keystroke — a
	// typed rune is up to 4 bytes (a CJK character 3, an emoji 4) — and typing it
	// is the harmless choice, while filing a keystroke into the user's paste file
	// is not. A paste whose final read is this short loses nothing but its last
	// few bytes of text.
	pasteContinuationMinBytes = 8
)

// PasteStore writes spilled pastes into one session's paste directory.
//
// It is deliberately NOT a cache or an index: it writes the file, and the file
// IS the state. Nothing is recorded in a database, nothing is pruned by a
// background process — a session directory's own lifetime is the store's (Rule
// 35: no new dependency, no new configuration key).
type PasteStore struct {
	dir string
	// source is the provenance this store stamps on every reference it renders
	// (internal/inputsource). It lives on the STORE rather than being passed per
	// call because one store belongs to one face: the chat input path's store
	// serves the operator, a session's inbox gate serves notify. Leaving it on
	// the call site would make every renderer carry an argument its own store
	// already knows.
	source string
	now    func() time.Time
	seq    *atomic.Uint64
}

// NewPasteStore builds a store rooted at one session directory, recording NO
// provenance — the form a caller with nothing to state uses (a test editor, a
// tool with no inbound face of its own). An empty dir yields a nil store (the
// editor then keeps its historic behaviour: no spill), which is what a caller
// with no session (a bare test editor) gets.
func NewPasteStore(sessionDir string) *PasteStore {
	return NewPasteStoreFor(sessionDir, "")
}

// NewPasteStoreFor builds a store that stamps `source` (internal/inputsource:
// human / queue / notify / llm) on every reference it renders, so a reader of the
// spilled payload can tell which face put the bytes on the line.
func NewPasteStoreFor(sessionDir, source string) *PasteStore {
	dir := strings.TrimSpace(sessionDir)
	if dir == "" {
		return nil
	}
	return &PasteStore{dir: filepath.Join(dir, PasteDirName), source: source, now: time.Now, seq: &pasteSeq}
}

// Source reports the provenance this store stamps on its references ("" when it
// stamps none). The editor reads it here rather than being told twice, so the
// store that wrote the file and the reference that names it state one source.
func (s *PasteStore) Source() string {
	if s == nil {
		return ""
	}
	return s.source
}

// ForSource returns the same store stamping a DIFFERENT provenance: same
// directory, same name sequence, one reference field changed. It exists because
// the boundary that spills an already-assembled line knows the face from the read
// arm that produced it (see chatSealMessage), while the store was built before
// that face was known — a copy with a replaced field is cheaper and safer than a
// second store pointed at the same directory.
func (s *PasteStore) ForSource(source string) *PasteStore {
	if s == nil {
		return nil
	}
	return &PasteStore{dir: s.dir, source: source, now: s.now, seq: s.seq}
}

// Dir returns the directory the spills are written to ("" for a nil store).
func (s *PasteStore) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// append continues a paste that is already being spilled: the bytes go to the
// SAME file as the paste's earlier reads, and nothing is put on the input line —
// the reference already on it stands for the whole paste. It reports whether the
// append succeeded; false means the caller must fall back to typing the bytes
// (the reason was reported).
//
// A paste is ONE MESSAGE, and its content belongs in ONE file: the tail a pty
// delivers after its last full read is part of the same paste, not a new one
// (see PasteThreshold's continuation constants).
func (s *PasteStore) appendBytes(path string, data []byte) error {
	if s == nil {
		return fmt.Errorf("paste store: no session directory was supplied")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, pasteFilePerm)
	if err != nil {
		return fmt.Errorf("paste store: cannot append to %s: %w", path, err)
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("paste store: cannot append to %s: %w", path, werr)
	}
	if cerr != nil {
		return fmt.Errorf("paste store: cannot close %s: %w", path, cerr)
	}
	return nil
}

// Spill writes the pasted bytes verbatim and returns the reference to put on the
// input line. The bytes are written EXACTLY as received — no rune conversion, no
// newline translation — so the file is the paste's own truth (a paste carrying
// CR/LF, ESC sequences, control bytes or invalid UTF-8 round-trips).
//
// The FILE is authoritative and is written before the caller inserts anything:
// a reference whose file does not exist is worse than no reference at all, so
// the failure is reported and the caller keeps the bytes in the buffer instead
// (the editor's own decision, see Editor.spillPaste).
func (s *PasteStore) Spill(data []byte) (PasteRef, error) {
	if s == nil {
		return PasteRef{}, fmt.Errorf("paste store: no session directory was supplied")
	}
	if len(data) == 0 {
		return PasteRef{}, fmt.Errorf("paste store: nothing to spill")
	}
	if err := os.MkdirAll(s.dir, pasteDirPerm); err != nil {
		return PasteRef{}, fmt.Errorf("paste store: cannot create %s: %w", s.dir, err)
	}
	path, err := s.write(data)
	if err != nil {
		return PasteRef{}, err
	}
	return PasteRef{Path: path, Bytes: len(data), Runes: utf8.RuneCount(data), Source: s.source}, nil
}

// write creates the spill file, walking the name forward until one is free. The
// file is created with O_EXCL, so a name that is taken is never overwritten —
// another paste's bytes are not this paste's to replace.
func (s *PasteStore) write(data []byte) (string, error) {
	stamp := s.now().UTC().Format(PasteStampLayout)
	for i := 0; i < pasteMaxNameTries; i++ {
		n := s.seq.Add(1)
		path := filepath.Join(s.dir, pasteName(stamp, n))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, pasteFilePerm)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", fmt.Errorf("paste store: cannot create %s: %w", path, err)
		}
		_, werr := f.Write(data)
		cerr := f.Close()
		if werr != nil {
			// The half-written file is removed: a truncated spill whose
			// reference names it would silently lose the paste's tail.
			_ = os.Remove(path) //stylegate:ignore removing the just-created file after a failed write; nothing else to report the failure to
			return "", fmt.Errorf("paste store: cannot write %s: %w", path, werr)
		}
		if cerr != nil {
			return "", fmt.Errorf("paste store: cannot close %s: %w", path, cerr)
		}
		return path, nil
	}
	return "", fmt.Errorf("paste store: no free file name under %s after %d tries", s.dir, pasteMaxNameTries)
}

// pasteName builds one spill's file name.
func pasteName(stamp string, n uint64) string {
	return PasteFilePrefix + stamp + "-" + strconv.FormatUint(n, 10) + PasteFileSuffix
}
