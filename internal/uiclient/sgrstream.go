package uiclient

// sgrstream.go — the RAW append leg beside the interpreted dump.
//
// WHAT IT IS, AND WHAT IT IS NOT. `ScreenDumper` (dump.go) is an INTERPRETER:
// every byte the renderer writes is fed into `lineedit.Screen`, and what reaches
// disk is that interpreter's rendering of the grid — throttled to one refresh
// per `dumpMinInterval`, and reduced to the cells the grid model can express
// (no scrollback past the model's history, no attributes the model folds, no
// bytes at all for a frame whose body is identical to the last written one).
// That is the right artifact for "what does the screen show right now", and it
// is the wrong artifact for "what EXACTLY did the renderer send", which is what
// a replay bug, a colour regression or a render-comparison question needs.
//
// This leg keeps the bytes themselves: every `Write(p)` the sink observes is
// appended — verbatim, unthrottled, no frame dropped — as
//
//	<decimal payload length> "\n" <exactly that many raw bytes>
//
// with a header line at open and a trailer line at close. Nothing is parsed on
// the way in, so the artifact is a truth that does not depend on this program's
// interpretation of it; a reader can replay it through ANY interpreter (the
// program's own, an older build's, a stranger's) and compare.
//
// WHY A LENGTH PREFIX AND NOT A DELIMITER. The payload IS terminal bytes: it
// contains 0x1b, 0x00, CR, LF, and every other value, so no in-band delimiter
// can be unambiguous. A decimal run followed by a single LF cannot be confused
// with a payload byte, because the parser is always positioned at a frame
// boundary when it reads one, and a line starting with `#` is a RECORD (header
// or trailer) rather than a length — `#` is not a digit, so the two grammars
// cannot collide.
//
// COST AND DEFAULT. The switch is OFF by default (DumpConfig.Sgr == nil is the
// off state, and dump.go's Write skips the leg entirely), so an unarmed run
// writes exactly what it wrote before this file existed. Armed, it costs one
// write(2) pair per render write and no interpretation.

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The stream's format vocabulary. Named once (Rule 1b/13): the writer, the
// reader below, the CLI's report line and every test key on these spellings,
// and a second spelling of the header prefix is how a reader silently stops
// recognizing a file the writer still produces.
const (
	// ScreenStreamHeaderPrefix heads the ONE header line. A line that starts with
	// it is a record, never a frame length.
	//
	// THE NAME OF THIS MECHANISM IS `screen`, NOT `sgr` (operator ruling,
	// 2026-11-08). The bytes recorded are a SCREEN PAINT sequence — SGR is one of
	// the escape classes inside it, not the thing being recorded — and the
	// mechanism's purpose ("did the screen come out byte-identical?") is a
	// screen question. So the header this package WRITES is the screen spelling,
	// and the sgr spelling below is kept READABLE for every artifact recorded
	// before the rename — MEASURED on this host 2026-11-08: 177 `.sgr.stream`
	// files against 4 under the new name, i.e. virtually the whole corpus predates
	// the rename, and a reader that stopped recognizing it would destroy the only
	// verbatim recordings that host has.
	ScreenStreamHeaderPrefix = "#gopi-screen-stream"
	// ScreenStreamTrailerPrefix heads the closing record. It is distinct from the
	// header's so a reader that sees it knows the run REACHED close-out, which
	// a truncated stream cannot fake.
	ScreenStreamTrailerPrefix = "#gopi-screen-stream-end"
	// LegacyScreenStreamHeaderPrefix / LegacyScreenStreamTrailerPrefix are the
	// pre-rename spellings. They are NEVER written; they are READ, by the two
	// predicates below, so an artifact from before 2026-11-08 still parses.
	LegacyScreenStreamHeaderPrefix  = "#gopi-sgr-stream"
	LegacyScreenStreamTrailerPrefix = "#gopi-sgr-stream-end"
	// SgrStreamVersion is the on-disk format version. A reader that does not
	// know it must refuse rather than guess (Rule 15).
	SgrStreamVersion = 1
	// SgrStreamFormat names the framing in the header, so a reader never has to
	// infer it from the bytes: `len-prefix` = decimal length, LF, raw payload.
	SgrStreamFormat = "len-prefix"
	// ScreenStreamSuffix is the artifact's file-name suffix. The full default name
	// is `<session-id>.screen.stream` (cmd/agent composes it from
	// sessionlayout.SessionID), so the name says WHICH SESSION's bytes these
	// are, which a fixed name could not.
	ScreenStreamSuffix = ".screen.stream"
	// LegacyScreenStreamSuffix is the pre-rename name. Files with it are still
	// READ — ScreenStreamSuffixes is the ONE list both the resolver and the readers
	// walk, so a reader cannot recognize a name the resolver stopped producing.
	LegacyScreenStreamSuffix = ".sgr.stream"
	// sgrStreamFilePerm is the artifact's mode: readable by the operator, the
	// same 0644 the dump uses.
	sgrStreamFilePerm = 0o644
	// sgrStreamUndeclared is what the header says when the caller could not
	// name the build it is running. It is stated rather than left empty: an
	// empty field reads as "no revision exists", which is a different claim
	// from "this build was not told one" (Rule 15).
	sgrStreamUndeclared = "undeclared"
	// sgrStreamLenPrefixMax bounds the decimal length a frame prefix may carry
	// (bytes). A prefix longer than this is a corrupt stream, not a frame: the
	// largest single render write any surface produces is orders of magnitude
	// below it, and refusing keeps a corrupt prefix from asking the reader to
	// allocate an arbitrary amount.
	sgrStreamLenPrefixMax = 1 << 30
)

// ScreenStreamSuffixes is the ONE list of file-name suffixes this mechanism
// answers to, newest first. The resolver WRITES [0]; every reader accepts any of
// them (Rule 14: one list, so a reader cannot recognize a name the writer
// stopped producing, and a writer cannot produce one no reader knows).
var ScreenStreamSuffixes = [...]string{ScreenStreamSuffix, LegacyScreenStreamSuffix}

// ScreenStreamSuffixOf reports which of ScreenStreamSuffixes names a path, and
// whether one of them does. It is the ONE match both the resolver and the readers
// use, so `--verify` cannot pick a file the loader would refuse.
func ScreenStreamSuffixOf(path string) (string, bool) {
	for _, suf := range ScreenStreamSuffixes {
		if strings.HasSuffix(path, suf) {
			return suf, true
		}
	}
	return "", false
}

// IsSgrStreamHeaderLine reports whether a record line is a HEADER in either the
// screen spelling this package now writes or the sgr spelling recorded before the
// rename. The trailing space is required for the same reason the trailer's is:
// a frame PAYLOAD can begin with any byte, and `#gopi-screen-stream` is a PREFIX
// of `#gopi-screen-stream-end`, so a bare prefix test would read a trailer as a
// header (measured in this package's own grammar walk).
func IsSgrStreamHeaderLine(line string) bool {
	for _, p := range [...]string{ScreenStreamHeaderPrefix, LegacyScreenStreamHeaderPrefix} {
		if strings.HasPrefix(line, p+" ") {
			return true
		}
	}
	return false
}

// IsSgrStreamTrailerLine is IsSgrStreamHeaderLine for the closing record: either
// spelling, with the separating space that distinguishes it from the header.
func IsSgrStreamTrailerLine(line string) bool {
	for _, p := range [...]string{ScreenStreamTrailerPrefix, LegacyScreenStreamTrailerPrefix} {
		if strings.HasPrefix(line, p+" ") {
			return true
		}
	}
	return false
}

// SgrStreamHeaderIsLegacy reports whether a header line carries the pre-rename
// spelling. It is a READING the replay reports, never a difference in how the
// bytes are parsed: both spellings declare the same format, so a caller that
// silently normalized them would hide which generation of recorder wrote a file.
func SgrStreamHeaderIsLegacy(line string) bool {
	return strings.HasPrefix(line, LegacyScreenStreamHeaderPrefix+" ")
}

// SgrStreamConfig configures the append leg. Cols/Rows are the geometry the
// sink was constructed with — recorded, not enforced: this file never
// interprets, so a later resize cannot make it wrong, only less informative
// (the trailer restates the geometry the sink ENDED at).
type SgrStreamConfig struct {
	// Path is the artifact. Required: a stream with no file is not a stream.
	Path string
	// Cols/Rows are the grid the sink was built at (the header's `grid=`).
	Cols int
	Rows int
	// Revision is the build's own revision, recorded so a replay can say which
	// binary produced these bytes. Empty is recorded as `undeclared`.
	Revision string
}

// SgrStream is the append leg: a frame-length-prefixed log of the raw bytes a
// render stream wrote. It is safe for concurrent use (its own lock), though in
// practice every call arrives under the sink's lock.
type SgrStream struct {
	mu      sync.Mutex
	f       *os.File
	path    string
	cols    int
	rows    int
	frames  int
	payload int64
	err     string
	closed  bool
}

// sgrHeaderLine composes the ONE header record. Both writers use it — the
// recording's OpenSgrStream and a range artifact's WriteSgrPrefix — so the two
// cannot drift in shape. The `started=` value is the CALLER's: a range artifact
// carries the START of the stream it was cut from, not the moment of the copy,
// which would date the file to a run it did not record.
func sgrHeaderLine(grid [2]int, revision, started string) string {
	rev := revision
	if rev == "" {
		rev = sgrStreamUndeclared
	}
	if started == "" {
		started = sgrStreamUndeclared
	}
	return strings.Join([]string{
		ScreenStreamHeaderPrefix,
		"version=" + strconv.Itoa(SgrStreamVersion),
		"format=" + SgrStreamFormat,
		"terminator=" + string(ScreenStreamHeaderPrefix[0]),
		"grid=" + geomWord(grid),
		"revision=" + rev,
		"started=" + started,
	}, " ") + "\n"
}

// sgrTrailerLine composes the ONE closing record, with the INCLUDED counts (a
// range artifact states what it holds, not what the source held).
func sgrTrailerLine(frames int, payload int64, grid [2]int) string {
	return strings.Join([]string{
		ScreenStreamTrailerPrefix,
		"frames=" + strconv.Itoa(frames),
		"bytes=" + strconv.FormatInt(payload, 10),
		"grid=" + geomWord(grid),
	}, " ") + "\n"
}

// OpenSgrStream creates (truncating) the artifact and writes its header line.
// It fails loudly — an armed switch that cannot produce its artifact must stop
// the caller, never leave a run that believes it is recording (Rule 15).
//
// TWO WRITERS ON ONE PATH IS REFUSED, NOT SILENTLY ALLOWED (measured 2026-11-07,
// task 01a1b336-4160). This function truncates, and truncation while another
// recorder is APPENDING is not a reset — it is a file in two pieces: the second
// writer's header+trailer land at offset 0, the first writer keeps writing at its
// own fd offsets, the kernel fills the gap with NULs, and the result carries TWO
// trailers and 38 MB nobody can parse. Measured on this host: a 38,151,147-byte
// stream whose records were `header … grid=0x0`, then `trailer frames=0 bytes=0`,
// then 38,150,984 bytes (3,874,076 of them real frame bytes, the rest a sparse
// hole), then the true `frames=885963 bytes=35656031 grid=117x24`. The reader sent
// its operator to inspect the RENDERER.
//
// So the open takes the artifact's own kernel lock BEFORE it truncates (a second
// live recorder is refused by name), and it truncates only after the lock is
// held. A lock is dropped by the kernel when its holder exits — panic, SIGKILL,
// power loss included — so a RESUME that reopens a dead run's path is unaffected:
// it takes the free lock and truncates exactly as before.
//
// WHAT THIS DOES *NOT* PREVENT, stated because a comfortable version of this claim
// would close a live defect (Rule 41). A process that dies abnormally never runs its
// own Close, so the path is usually CLEAN when the next run opens it — the damaged
// file therefore still needs its other half, and where that half lives is measured:
//
//	$ python3 walk of session 1918704d's ledger
//	12 × session_start, 10 × session_end `exit_code=1 reason=signalled`
//	  (pids 3652633…3675218, four minutes on 2026-11-06 08:05–08:06)
//	1 × session_end `exit_code=0 reason=quit` at 2026-11-07T03:21:08.496
//	the damaged stream's mtime 2026-11-07 03:21:08.491  ← the SAME second
//
// and the header it carries says `started=2026-11-06T00:06:10Z`, i.e. 08:06:10 local
// — the timestamp of the LAST of those session_starts. So the shape is: one long-lived
// writer (885,963 frames) opened the artifact at 08:06:10, and something wrote a
// header + `frames=0 bytes=0` trailer pair over its first 163 bytes WHILE it kept
// appending. Which process did that is NOT established here: production opens this
// artifact exactly once (cmd/agent/cli_chat.go's single chatOpenSgrStream call), and
// the ledger names only one start at that second. This lock refuses it either way —
// flock is per open-file-description, so a SECOND open of the same artifact from the
// SAME process is refused too — but WHY a second open happened is a session-lifecycle
// question, filed rather than guessed at.
func OpenSgrStream(cfg SgrStreamConfig) (*SgrStream, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("uiclient: the SGR stream needs a destination path")
	}
	f, err := os.OpenFile(cfg.Path, os.O_WRONLY|os.O_CREATE, sgrStreamFilePerm)
	if err != nil {
		return nil, fmt.Errorf("uiclient: cannot open the SGR stream %s: %w", cfg.Path, err)
	}
	if lerr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); lerr != nil {
		_ = f.Close() //stylegate:ignore this open is being refused; its fd carries no lock to release
		return nil, fmt.Errorf("uiclient: the SGR stream %s is already being written by another live recorder (a second writer would truncate a recording in progress: %w)", cfg.Path, lerr)
	}
	// THE TRUNCATION COMES AFTER THE LOCK, and that order is the whole fix: a
	// truncate-then-find-out-the-lock-is-taken sequence would have already
	// destroyed the bytes it was refusing to destroy.
	if terr := f.Truncate(0); terr != nil {
		_ = f.Close() //stylegate:ignore the truncation failed, so no artifact was produced; the close cannot change the error reported below
		return nil, fmt.Errorf("uiclient: cannot truncate the SGR stream %s for a new recording: %w", cfg.Path, terr)
	}
	s := &SgrStream{f: f, path: cfg.Path, cols: cfg.Cols, rows: cfg.Rows}
	header := sgrHeaderLine([2]int{cfg.Cols, cfg.Rows}, cfg.Revision, time.Now().UTC().Format(time.RFC3339))
	if _, err := io.WriteString(f, header); err != nil {
		_ = f.Close() //stylegate:ignore the open already failed; the close cannot change the error reported below
		return nil, fmt.Errorf("uiclient: cannot write the SGR stream header to %s: %w", cfg.Path, err)
	}
	return s, nil
}

// Path is the artifact's location.
func (s *SgrStream) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Frames reports how many render writes were appended. A nil stream reports 0,
// which is also what an unarmed run has: the report line's `frames=0` with no
// path is the off state, never a silent failure to record.
func (s *SgrStream) Frames() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frames
}

// Bytes reports how many PAYLOAD bytes were appended (the frame prefixes and
// the two record lines are framing, not payload).
func (s *SgrStream) Bytes() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.payload
}

// Err reports the last append failure, so a caller's close-out can state it. It
// is recorded rather than returned into the render path for the same reason the
// dump's own errors are: an observer must not be able to break what it observes.
func (s *SgrStream) Err() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Write appends ONE frame: the decimal payload length, a newline, then the
// bytes exactly as received.
//
// The two writes are not merged into one buffer on purpose: a frame can be large
// and the sink already copies it into the interpreter, so a second full-size
// allocation per frame would be the artifact's cost paid twice. Ordering is
// guaranteed by the lock, and a crash between the prefix and the payload leaves
// a TRUNCATED stream — which a reader detects (it asks for N bytes and gets
// fewer) instead of silently reading the next frame's bytes as this one's.
func (s *SgrStream) Write(p []byte) error {
	if s == nil || len(p) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.f == nil {
		return nil
	}
	if err := s.append(p); err != nil {
		s.err = err.Error()
		return err
	}
	s.frames++
	s.payload += int64(len(p))
	return nil
}

// append is the frame write itself, with the stream's lock already held.
func (s *SgrStream) append(p []byte) error {
	if _, err := io.WriteString(s.f, strconv.Itoa(len(p))+"\n"); err != nil {
		return fmt.Errorf("uiclient: cannot write the SGR stream frame prefix to %s: %w", s.path, err)
	}
	if _, err := s.f.Write(p); err != nil {
		return fmt.Errorf("uiclient: cannot write the SGR stream frame to %s: %w", s.path, err)
	}
	return nil
}

// Close writes the trailer and closes the artifact. It is idempotent, and it
// reports the failure it met rather than swallowing it: a stream whose trailer
// is missing looks exactly like a run that died mid-frame, which is a
// diagnosis a healthy run must never fabricate (Rule 15).
func (s *SgrStream) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.f == nil {
		return nil
	}
	trailer := sgrTrailerLine(s.frames, s.payload, [2]int{s.cols, s.rows})
	_, werr := io.WriteString(s.f, trailer)
	cerr := s.f.Close()
	s.f = nil
	if werr != nil {
		s.err = werr.Error()
		return fmt.Errorf("uiclient: cannot write the SGR stream trailer to %s: %w", s.path, werr)
	}
	if cerr != nil {
		s.err = cerr.Error()
		return fmt.Errorf("uiclient: cannot close the SGR stream %s: %w", s.path, cerr)
	}
	return nil
}

// SetGeometry restates the geometry the sink moved to, so the trailer names the
// grid the run ENDED at. The header keeps the geometry the sink was BUILT at:
// the two together answer "was this stream recorded across a resize?", which one
// number alone cannot.
func (s *SgrStream) SetGeometry(cols, rows int) {
	if s == nil || cols <= 0 || rows <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols, s.rows = cols, rows
}

// SgrStreamMeta is what a reader learned from a stream's record lines.
type SgrStreamMeta struct {
	// Version is the format version the header declared (0 when the stream has
	// no readable header — a truncated or foreign file).
	Version int
	// Grid is the geometry the header named — the grid the sink was BUILT at.
	Grid [2]int
	// EndGrid is the geometry the TRAILER named — the grid the run ENDED at, as
	// last restated by SetGeometry. It is the zero value when the stream has no
	// trailer (a run that died mid-frame), and it is deliberately NOT defaulted
	// to Grid: "no trailer" and "the run never moved" are different findings,
	// and a reader that flattened them would erase the one question the pair of
	// fields exists to answer ("was this stream recorded across a resize?").
	EndGrid [2]int
	// Revision is the build the header named.
	Revision string
	// Started is the header's own start stamp, carried VERBATIM so a range
	// artifact can restate the moment the SOURCE began rather than the moment
	// the copy was made (a copy stamped with its own clock dates the file to a
	// run that recorded nothing).
	Started string
	// Frames / Bytes are the TRAILER's counts, and Trailer reports whether the
	// trailer was there at all: without it the counts are the reader's own, and
	// the stream is a run that did not reach close-out.
	Frames  int
	Bytes   int64
	Trailer bool
}

// readSgrHeader decodes a stream's header record into the meta a reader reports.
// It is the ONE decoder for that record: both readers below (the full parse and
// the range walk) call it, so a header this package accepts cannot be accepted by
// one of them and refused by the other.
//
// The version is the file's own claim, so a claim this reader cannot DECODE is
// refused here rather than silently becoming zero: reading `version=one` as 0
// would make the refusal below say "declares format version 0", which quotes a
// value the file never carried (Rule 15). An ABSENT field and an ILLEGAL one are
// refused separately for the same reason — `version=` and no `version=` at all are
// different findings, and Atoi("") would flatten both into the same 0.
func readSgrHeader(line string) (SgrStreamMeta, error) {
	var meta SgrStreamMeta
	if !IsSgrStreamHeaderLine(line) {
		return meta, fmt.Errorf("not a screen stream: the first line is %q, not a %s or %s header",
			line, ScreenStreamHeaderPrefix, LegacyScreenStreamHeaderPrefix)
	}
	fields := parseStreamFields(line)
	versionWord, versionErr := requiredField(fields, "version")
	if versionErr != nil {
		return meta, fmt.Errorf("SGR stream header: %w", versionErr)
	}
	v, err := strconv.Atoi(versionWord)
	if err != nil {
		return meta, fmt.Errorf("SGR stream header's version field %q is not a number: %w", versionWord, err)
	}
	if v != SgrStreamVersion {
		return meta, fmt.Errorf("SGR stream declares format version %d; this reader knows %d", v, SgrStreamVersion)
	}
	meta.Version = v
	gridWord, gridErr := requiredField(fields, "grid")
	if gridErr != nil {
		return meta, fmt.Errorf("SGR stream header: %w", gridErr)
	}
	meta.Grid, err = parseStreamGeom(gridWord)
	if err != nil {
		return meta, fmt.Errorf("SGR stream header's grid field %q is not a COLSxROWS geometry: %w", gridWord, err)
	}
	meta.Revision = fields["revision"]
	meta.Started = fields["started"]
	return meta, nil
}

// readSgrTrailer decodes the closing record into meta, by the same rules: both
// counts and the end geometry are decoded or the read is refused, and an ABSENT
// field is refused by its own sentence (Atoi("") would otherwise report a missing
// field as a non-numeric one).
//
// The trailer's counts are a HALF RESULT if either is unreadable: Trailer=true
// with Frames=0 reads exactly like a completed run that wrote no frames, which is
// a diagnosis a corrupt record must not be able to fabricate (Rule 15).
func readSgrTrailer(line string, meta *SgrStreamMeta) error {
	tf := parseStreamFields(line)
	framesWord, framesErr := requiredField(tf, "frames")
	if framesErr != nil {
		return fmt.Errorf("SGR stream trailer: %w", framesErr)
	}
	frames, err := strconv.Atoi(framesWord)
	if err != nil {
		return fmt.Errorf("SGR stream trailer's frames field %q is not a number: %w", framesWord, err)
	}
	bytesWord, bytesErr := requiredField(tf, "bytes")
	if bytesErr != nil {
		return fmt.Errorf("SGR stream trailer: %w", bytesErr)
	}
	nbytes, err := strconv.ParseInt(bytesWord, 10, 64)
	if err != nil {
		return fmt.Errorf("SGR stream trailer's bytes field %q is not a number: %w", bytesWord, err)
	}
	// The trailer's geometry is the run's OWN last word about the grid, decoded by
	// the same rule as the header's: a value this reader cannot decode is a record
	// it cannot read, refused by name, never a `0x0` — which is a REAL geometry (a
	// width probe that returned nothing) and therefore cannot double as
	// "unreadable". It is a distinct FIELD from the header's because the two answer
	// different questions ("built at" vs "ended at").
	tgridWord, tgridErr := requiredField(tf, "grid")
	if tgridErr != nil {
		return fmt.Errorf("SGR stream trailer: %w", tgridErr)
	}
	endGrid, err := parseStreamGeom(tgridWord)
	if err != nil {
		return fmt.Errorf("SGR stream trailer's grid field %q is not a COLSxROWS geometry: %w", tgridWord, err)
	}
	meta.Frames, meta.Bytes, meta.EndGrid, meta.Trailer = frames, nbytes, endGrid, true
	return nil
}

// ReadSgrFrames parses a stream into its frames. It is the READ side of the
// format, and it exists so the artifact's claim ("only raw bytes and frame
// boundaries") is checkable by something other than the writer: a replay, a
// comparison, or an acceptance run.
//
// Tolerance is stated, not incidental: a stream cut mid-frame (a crashed run)
// returns the frames it completed, the meta with Trailer=false, and NO error —
// that is a real state of the world, and a reader that refused it could not read
// the artifacts most worth reading. A stream whose header is missing or whose
// version is unknown IS an error: that is not truncation, it is a different
// format wearing this one's name.
func ReadSgrFrames(r io.Reader) (SgrStreamMeta, [][]byte, error) {
	var meta SgrStreamMeta
	br := newLineReader(r)
	head, err := br.nextRecord()
	if err != nil {
		return meta, nil, fmt.Errorf("uiclient: SGR stream has no readable header: %w", err)
	}
	if meta, err = readSgrHeader(head); err != nil {
		return meta, nil, fmt.Errorf("uiclient: %w", err)
	}
	var frames [][]byte
	var payload int64
	for {
		line, err := br.nextRecord()
		if err == io.EOF {
			break
		}
		if err != nil {
			return meta, frames, fmt.Errorf("uiclient: SGR stream ended inside a record: %w", err)
		}
		if IsSgrStreamTrailerLine(line) {
			tf := parseStreamFields(line)
			// The trailer's counts are a HALF RESULT if either is unreadable:
			// Trailer=true with Frames=0 reads exactly like a completed run
			// that wrote no frames, which is a diagnosis a corrupt record must
			// not be able to fabricate (Rule 15). Both fields are therefore
			// decoded or the read is refused — and an ABSENT field is refused
			// by its own sentence, because Atoi("") would otherwise report a
			// missing field as a non-numeric one.
			framesWord, framesErr := requiredField(tf, "frames")
			if framesErr != nil {
				return meta, frames, fmt.Errorf("uiclient: SGR stream trailer: %w", framesErr)
			}
			meta.Frames, err = strconv.Atoi(framesWord)
			if err != nil {
				return meta, frames, fmt.Errorf("uiclient: SGR stream trailer's frames field %q is not a number: %w", framesWord, err)
			}
			bytesWord, bytesErr := requiredField(tf, "bytes")
			if bytesErr != nil {
				return meta, frames, fmt.Errorf("uiclient: SGR stream trailer: %w", bytesErr)
			}
			meta.Bytes, err = strconv.ParseInt(bytesWord, 10, 64)
			if err != nil {
				return meta, frames, fmt.Errorf("uiclient: SGR stream trailer's bytes field %q is not a number: %w", bytesWord, err)
			}
			// The trailer's geometry is the run's OWN last word about the grid,
			// and it is decoded by the same rule as the header's: a value this
			// reader cannot decode is a record it cannot read, refused by name,
			// never a `0x0` — which is a REAL geometry (a width probe that
			// returned nothing) and therefore cannot double as "unreadable".
			// It is a distinct FIELD from the header's for the same reason: the
			// two answer different questions ("built at" vs "ended at"), and a
			// reader that returned one value for both could not tell a stream
			// recorded across a resize from one that never moved.
			tgridWord, tgridErr := requiredField(tf, "grid")
			if tgridErr != nil {
				return meta, frames, fmt.Errorf("uiclient: SGR stream trailer: %w", tgridErr)
			}
			meta.EndGrid, err = parseStreamGeom(tgridWord)
			if err != nil {
				return meta, frames, fmt.Errorf("uiclient: SGR stream trailer's grid field %q is not a COLSxROWS geometry: %w", tgridWord, err)
			}
			meta.Trailer = true
			break
		}
		n, cerr := strconv.Atoi(line)
		if cerr != nil || n < 0 || n > sgrStreamLenPrefixMax {
			return meta, frames, fmt.Errorf("uiclient: SGR stream frame prefix %q is not a length in 0..%d", line, sgrStreamLenPrefixMax)
		}
		frame := make([]byte, n)
		if _, err := io.ReadFull(br, frame); err != nil {
			// A frame the writer announced but did not finish: the stream is
			// truncated there, and everything before it is intact.
			meta.Frames = len(frames)
			meta.Bytes = payload
			return meta, frames, nil
		}
		frames = append(frames, frame)
		payload += int64(n)
	}
	if meta.Trailer {
		// The trailer's own count is the writer's statement; the reader keeps
		// it so a caller can compare the two (a mismatch is a defect in the
		// writer, and the reader must not paper over it).
		return meta, frames, nil
	}
	meta.Frames = len(frames)
	meta.Bytes = payload
	return meta, frames, nil
}

// lineReader reads LF-terminated records and then exact byte counts, which is
// exactly the frame grammar. It buffers so a payload is not re-read byte by
// byte.
type lineReader struct {
	src     io.Reader
	buf     []byte
	r, w    int
	eofSeen bool
}

func newLineReader(r io.Reader) *lineReader { return &lineReader{src: r, buf: make([]byte, 64*1024)} }

// Read implements io.Reader so io.ReadFull can pull a payload through it.
func (l *lineReader) Read(p []byte) (int, error) {
	if l.r == l.w {
		if l.eofSeen {
			return 0, io.EOF
		}
		n, err := l.src.Read(l.buf)
		l.r, l.w = 0, n
		if err != nil {
			l.eofSeen = true
			if n == 0 {
				return 0, err
			}
		}
	}
	n := copy(p, l.buf[l.r:l.w])
	l.r += n
	return n, nil
}

// nextRecord returns the next LF-terminated line WITHOUT its LF.
func (l *lineReader) nextRecord() (string, error) {
	var out []byte
	var one [1]byte
	for {
		n, err := l.Read(one[:])
		if err != nil {
			if len(out) > 0 {
				return string(out), nil
			}
			return "", err
		}
		if n == 0 {
			continue
		}
		if one[0] == '\n' {
			return string(out), nil
		}
		out = append(out, one[0])
	}
}

// parseStreamFields splits `prefix k=v k=v …` into its key/value pairs, ignoring
// the prefix word itself.
func parseStreamFields(line string) map[string]string {
	out := make(map[string]string)
	f := strings.Fields(line)
	for _, kv := range f[1:] {
		if i := strings.Index(kv, "="); i > 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}

// requiredField returns a record's value for key, or a named error when the
// record does not carry the key at all. It exists because strconv.Atoi("") is
// an error of the same SHAPE as strconv.Atoi("x"), so a decoder that reads the
// map directly cannot tell "the field is absent" from "the field is unreadable"
// — two different findings about the file, and a diagnosis that merged them
// would name the wrong one (Rule 15).
func requiredField(fields map[string]string, key string) (string, error) {
	v, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("the record names no %s field", key)
	}
	return v, nil
}

// parseStreamGeom reads back geomWord's `COLSxROWS`. The error is the point: the
// writer emits this field unconditionally, so a header whose geometry does not
// decode is a record this reader cannot read, not a stream with a `0x0` grid —
// and a zero geometry is a REAL value (the width probe returned 0,0), so it
// cannot double as the "unreadable" marker.
func parseStreamGeom(v string) ([2]int, error) {
	var g [2]int
	i := strings.Index(v, "x")
	if i <= 0 {
		return g, fmt.Errorf("%q has no COLSxROWS separator", v)
	}
	cols, err := strconv.Atoi(v[:i])
	if err != nil {
		return g, fmt.Errorf("column count: %w", err)
	}
	rows, err := strconv.Atoi(v[i+1:])
	if err != nil {
		return g, fmt.Errorf("row count: %w", err)
	}
	g[0], g[1] = cols, rows
	return g, nil
}
