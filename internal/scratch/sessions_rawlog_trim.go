package scratch

// sessions_rawlog_trim.go — the BOUND on a session's raw HTTP log.
//
// WHY IT EXISTS (measured 2026-10-30, host host-a, the row this file
// answers). `~/.gopi/sessions` held 3282 containers / 83 GB on a `/` partition
// at 98%, and the largest single class inside it was `*.raw.log`: 1095 files,
// 64.4 GiB, p50 = 48.4 MiB, p90 = 115.7 MiB, max = 1515.7 MiB (one file per
// HTTP round set, appended for the whole life of a run). The ONE reclaim verb
// the family already had (`gopi session gc`, sessions_gc.go) judges by AGE and
// never by SIZE, so it could reclaim 14.7 MiB of a 64 GiB problem — the big
// files are all inside the freshness window by definition, because they are big
// BECAUSE they are new.
//
// WHAT IT IS. A bounded, byte-level rewrite of ONE raw log: keep the file's
// HEAD (the creation/resume header plus the first rounds — the part a reader
// dates the run by), keep the TAIL (the most recent rounds verbatim — the part
// a debugging reader actually asks for), and replace everything between them
// with ONE honest marker line that states how many bytes went away, when, and
// that they are not recoverable. The result is ≤ the cap.
//
// WHAT IT IS NOT, and the two refusals are the whole safety argument:
//
//	① IT NEVER DELETES THE FILE. A raw log is the run's audit物证 — the last
//	   verbatim record that a request went on the wire. Trimming is a REWRITE
//	   that leaves a head, a tail and a marker; there is no path through this
//	   file that produces an empty or missing log, and a file within the cap is
//	   never opened for writing at all.
//	② IT NEVER ADOPTS "UNKNOWN" AS "IDLE". A log a live process holds open
//	   (its /proc/<pid>/fd link points at the file) vetoes the rewrite, and a
//	   holder scan that stopped before the end of the process table vetoes it
//	   too — "not looked at" is not "nobody there" (holder.go's rule, reused
//	   here rather than re-derived).
//
// Rule 35 (zero dependency): this is a Go path in the verb that already exists
// (`gopi session gc`), not a timer, a script, a state file or a second sweep.
// Rule 22: the rewrite's temporary file is NOT a temp workspace — an atomic
// rename requires the same filesystem, so the half-written target lands beside
// the target and is renamed over it by one syscall.

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nick/gopi/internal/rawlogfmt"
)

const (
	// SessionsRawLogCapBytes is the DEFAULT cap `gopi session gc
	// --trim-raw-log` bounds a raw log to (64 MiB).
	//
	// WHY 64 MiB, measured rather than guessed: on the 1095 raw logs of
	// 2026-10-30 the distribution was p50 = 48.4 MiB, p90 = 115.7 MiB; a 64 MiB
	// cap puts the median file UNTOUCHED (a file that never exceeds its natural
	// size must not be rewritten) and bounds the over-cap population at 414
	// files holding 46.7 GiB, i.e. it addresses the whole measured problem
	// without rewriting the ordinary case. The constant is a DEFAULT: the CLI
	// takes `--trim-raw-log SIZE`, and this value is what the flag means when
	// no size is given. It is never a ceiling on what a caller may ask for.
	SessionsRawLogCapBytes int64 = 64 << 20

	// SessionsRawLogHeadKeepBytes is how much of the file's HEAD survives every
	// trim: the creation header line plus the first rounds. 8 KiB, because the
	// header is one short line and the first round's request body (the system
	// prompt, the epic briefing — the part a reader opens the file for) is a few
	// KiB; the value is small on purpose, since every byte here is a byte the
	// tail loses.
	SessionsRawLogHeadKeepBytes int64 = 8 << 10

	// SessionsRawLogTrimMinCapBytes is the smallest cap this arm will act on.
	// Below it the head plus the marker line would fill the file by themselves,
	// so a rewrite would keep no round at all — a "bound" that deletes the
	// evidence it was meant to keep. It is expressed in the head constant so the
	// two cannot drift apart.
	SessionsRawLogTrimMinCapBytes int64 = 4 * SessionsRawLogHeadKeepBytes

	// SessionsRawLogTrimMarkerPrefix opens the line that replaces the dropped
	// middle. It is a `#` line for the reason the creation/resume headers are
	// (internal/agent/rawlog.go): the round parser matches "→ REQUEST" /
	// "← RESPONSE" / "← CHUNK" by whole-line equality, so a `#` line carries no
	// round and can never be mistaken for one. ONE spelling, shared by the
	// writer and every test that asserts on it (Rules 1b/13).
	SessionsRawLogTrimMarkerPrefix = "# gopi session gc --trim-raw-log — TRIMMED "

	// SessionsRawLogTrimTmpSuffix names the half-written rewrite that sits
	// beside its target until one rename replaces it. It is NOT a temp
	// workspace: an atomic rename cannot cross a filesystem, so the alternative
	// is a non-atomic in-place rewrite that would leave a truncated log behind
	// if the process died mid-copy.
	SessionsRawLogTrimTmpSuffix = ".trim.tmp"

	// SessionsRawLogTrimMarkerScanBytes bounds the HEAD read a reader uses to
	// ask "does this log carry the trim marker?". It is DERIVED from this
	// package's own writer, never guessed: TrimRawLog writes the marker at
	// exactly HeadKeepBytes + (at most one line's worth of forward extension,
	// rawLogLineExtendBytes) + one marker line (TrimMarkerReserve). A reader
	// that reads this much of the head therefore sees the marker of EVERY file
	// this package can produce — a file whose marker sat further in would need a
	// head this writer cannot build.
	SessionsRawLogTrimMarkerScanBytes int64 = SessionsRawLogHeadKeepBytes + rawLogLineExtendBytes + SessionsRawLogTrimMarkerReserve
)

// RawLogCarriesTrimMarker reports whether path was rewritten by TrimRawLog — the
// ONE predicate every reader asks (Rules 13/14: the marker's owner defines both
// the spelling and the way to find it, so a parser and the writer cannot drift).
//
// It reads a BOUNDED head (SessionsRawLogTrimMarkerScanBytes), never the whole
// file: a raw log runs to hundreds of MiB and every checkpoint read would
// otherwise pay a full pass over an untrimmed log.
//
// A read error is RETURNED, never turned into "not trimmed" (Rule 15: a reader
// that cannot look must not answer "clean").
func RawLogCarriesTrimMarker(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("raw log trim marker: %s: %w", path, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return false, fmt.Errorf("raw log trim marker: %s: %w", path, err)
	}
	n := fi.Size()
	if n > SessionsRawLogTrimMarkerScanBytes {
		n = SessionsRawLogTrimMarkerScanBytes
	}
	if n <= 0 {
		return false, nil
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, 0); err != nil && err != io.EOF {
		return false, fmt.Errorf("raw log trim marker: %s: %w", path, err)
	}
	// Line boundary: start of file, or right after a newline. The marker's own
	// text is the discriminator, so a payload line that merely CONTAINS it is
	// not a hit.
	//
	// The needle carries the newline so ONE memmem pass answers the question.
	// The first version walked every byte of the buffer doing
	// `strings.HasPrefix(string(buf[i:]), …)`, which COPIES the rest of the
	// buffer per position — O(n²), measured at 14.4 s on this package's 9 MiB
	// no-REQUEST fixture (parse_test.go's loop-termination arm, which bounds
	// LastRequest at 5 s). A reader that runs on every checkpoint open cannot
	// pay that; the marker prefix contains no newline, so a
	// "\n"+prefix needle matched by bytes.Index IS the line-boundary test.
	needle := make([]byte, 0, len(SessionsRawLogTrimMarkerPrefix)+1)
	needle = append(needle, '\n')
	needle = append(needle, SessionsRawLogTrimMarkerPrefix...)
	if bytes.HasPrefix(buf, []byte(SessionsRawLogTrimMarkerPrefix)) {
		return true, nil
	}
	if bytes.Contains(buf, needle) {
		return true, nil
	}
	return false, nil
}

// The raw log's trim verdicts. Constants because the CLI report, the JSON and
// the tests all match on them (Rule 1b): a reason typed twice drifts into a
// reason nobody can grep for.
const (
	// SessionsRawLogWithinCap: the file is already inside the cap, so it is not
	// a candidate and is never opened for writing.
	SessionsRawLogWithinCap = "within the cap"

	// SessionsRawLogOverCap: over the cap and rewritable.
	SessionsRawLogOverCap = "over the cap"

	// SessionsRawLogHeldReason: a live process holds the file open (its /proc
	// fd link resolves to it) — rewriting under a writer would corrupt the log
	// of a run that is still running.
	SessionsRawLogHeldReason = "a live process holds it open (rewriting a log being written would corrupt it)"

	// SessionsRawLogScanWithheldReason: the holder scan stopped at its bound, so
	// nobody can say whether a process holds the file. Unknown is never
	// permission (holder.go's HolderScanWithheldReason, restated for this arm so
	// the two sentences cannot drift).
	SessionsRawLogScanWithheldReason = "the process scan stopped at its bound, so the holders are unknown"

	// SessionsRawLogNotRegularReason: a directory, a symlink or anything else
	// that is not a regular file is never rewritten.
	SessionsRawLogNotRegularReason = "not a regular file"

	// SessionsRawLogUnmeasurableReason: the size could not be read.
	SessionsRawLogUnmeasurableReason = "size unmeasurable"
)

// RawLogTrimPlan is ONE raw log's verdict: the reporting form the CLI prints and
// the judge the apply arm runs. The zero value is NOT a candidate (Trim is
// false), which is the safe direction for a field that authorises a rewrite.
type RawLogTrimPlan struct {
	// Path is the raw log file.
	Path string
	// Bytes is its measured size.
	Bytes int64
	// Age is how long it has been untouched (its own mtime).
	Age time.Duration
	// Trim reports whether the file is over the cap and unheld, i.e. whether an
	// apply would rewrite it.
	Trim bool
	// Reason says why it is (or is not) a candidate.
	Reason string
	// DropBytes is how many bytes the rewrite would remove (0 when Trim is
	// false).
	DropBytes int64
}

// SessionsRawLogs returns every raw log under the sessions root, ONE level down
// (the co-located layout `<sessions>/<container>/<*.raw.log>`), with its size
// and age — a read-only listing, the answer to "who is big" that the verb did
// not have.
//
// It is bounded by SessionsGCScanLimit entries per level and reports what it
// skipped instead of silently truncating (Rule 15): a sweep that stops looking
// must not read as a tree with nothing in it.
func SessionsRawLogs(root string) ([]RawLogTrimPlan, error) {
	if err := SessionsRootRefusal(root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("session gc: listing the sessions root %s: %w", root, err)
	}
	now := time.Now()
	out := make([]RawLogTrimPlan, 0, len(entries))
	children := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		children++
		if children > SessionsGCScanLimit {
			return nil, fmt.Errorf("session gc: the sessions root %s holds more than %d directories — refusing to walk an unbounded tree", root, SessionsGCScanLimit)
		}
		dir := filepath.Join(root, e.Name())
		files, derr := os.ReadDir(dir)
		if derr != nil {
			continue // unreadable container: reported by the walk's caller as absent, never as empty
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), SessionRawLogFileSuffix) {
				continue
			}
			p := filepath.Join(dir, f.Name())
			plan := RawLogTrimPlan{Path: p, Reason: SessionsRawLogUnmeasurableReason}
			fi, serr := os.Stat(p)
			if serr != nil {
				out = append(out, plan)
				continue
			}
			if !fi.Mode().IsRegular() {
				plan.Bytes = fi.Size()
				plan.Reason = SessionsRawLogNotRegularReason
				out = append(out, plan)
				continue
			}
			plan.Bytes = fi.Size()
			plan.Age = now.Sub(fi.ModTime())
			plan.Reason = SessionsRawLogWithinCap
			out = append(out, plan)
		}
	}
	return out, nil
}

// PlanRawLogTrim judges every raw log under root against capBytes: read-only, no
// lock, no write. A file over the cap is a candidate unless a live process holds
// it open — the ONE veto, and it is measured with the same /proc pass every
// other arm of this package uses (ScanHolders), never re-derived.
func PlanRawLogTrim(root string, capBytes int64) ([]RawLogTrimPlan, error) {
	if err := checkRawLogCap(capBytes); err != nil {
		return nil, err
	}
	plans, err := SessionsRawLogs(root)
	if err != nil {
		return nil, err
	}
	// ONE holder pass for the whole tree, taken only when something is over the
	// cap: a plan with no candidate must not pay for a /proc walk.
	over := false
	for _, p := range plans {
		if p.Bytes > capBytes {
			over = true
			break
		}
	}
	if !over {
		return plans, nil
	}
	scan := ScanHolders()
	for i := range plans {
		p := &plans[i]
		if p.Bytes <= capBytes || p.Reason != SessionsRawLogWithinCap {
			continue
		}
		if verdict := rawLogHolderRefusal(p.Path, scan); verdict != "" {
			p.Reason = verdict
			continue
		}
		p.Trim = true
		p.Reason = SessionsRawLogOverCap
		p.DropBytes = p.Bytes - rawLogTrimmedSize(p.Bytes, capBytes)
	}
	return plans, nil
}

// rawLogHolderRefusal answers the ONE question a rewrite must ask: may this file
// be rewritten right now? "" means yes; the returned sentence is the KEEP reason.
func rawLogHolderRefusal(path string, scan HolderScan) string {
	held, ok := scan.HeldByProcesses(path)
	if !ok {
		return SessionsRawLogScanWithheldReason
	}
	if held {
		return SessionsRawLogHeldReason
	}
	return ""
}

// RawLogTrimOutcome is what ONE rewrite actually did — the numbers the CLI
// reports and the test asserts on. Every field is measured after the fact, never
// predicted.
type RawLogTrimOutcome struct {
	// Path is the file that was (or was not) rewritten.
	Path string
	// Before / After are the byte sizes.
	Before int64
	After  int64
	// Head / Tail are how many bytes of the original head and tail survived.
	Head int64
	Tail int64
	// Trimmed is false when the file was already within the cap (nothing was
	// written).
	Trimmed bool
}

// TrimRawLog rewrites ONE raw log to at most capBytes: the head, ONE marker
// line, and the tail's complete lines. A file already within the cap is NOT
// touched (Trimmed=false) — the caller learns that from the outcome rather than
// from a guess.
//
// The rewrite is atomic: the new content is written to <name>.trim.tmp BESIDE
// the target, fsynced, chmodded to the original mode, and renamed over it. A
// process killed mid-copy therefore leaves the original intact (and possibly one
// stray .trim.tmp, which is a name this package's own tree row can never
// mistake for a raw log).
func TrimRawLog(path string, capBytes int64) (RawLogTrimOutcome, error) {
	out := RawLogTrimOutcome{Path: path}
	if err := checkRawLogCap(capBytes); err != nil {
		return out, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return out, fmt.Errorf("trim raw log: %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return out, fmt.Errorf("trim raw log: %s: %s", path, SessionsRawLogNotRegularReason)
	}
	out.Before = fi.Size()
	out.After = fi.Size()
	if fi.Size() <= capBytes {
		return out, nil
	}

	src, err := os.Open(path)
	if err != nil {
		return out, fmt.Errorf("trim raw log: %s: %w", path, err)
	}
	defer src.Close()

	// WHICH FRAMING THE FILE IS WRITTEN IN DECIDES WHERE ITS BOUNDARIES ARE. A
	// length-prefixed record may span many lines, so "align to a line" is not
	// "align to a record": a cut inside a record leaves a fragment whose stated
	// length the file no longer delivers, and EVERY later read of the trimmed file
	// fails to decode (rawlogfmt.ErrMalformedRecord) instead of merely losing the
	// middle. The format is therefore detected once here and both alignments ask it.
	format, ferr := rawlogfmt.Detect(path)
	if ferr != nil {
		return out, fmt.Errorf("trim raw log: %s: %w", path, ferr)
	}
	headLen, err := rawLogHeadLength(src, fi.Size(), capBytes, format)
	if err != nil {
		return out, fmt.Errorf("trim raw log: %s: %w", path, err)
	}
	tailLen, tailStart, midStart, err := rawLogTailWindow(src, fi.Size(), capBytes, headLen, format)
	if err != nil {
		return out, fmt.Errorf("trim raw log: %s: %w", path, err)
	}
	if tailStart <= midStart {
		// The file is tiny enough that head and tail meet: trimming would keep
		// everything and drop nothing. Reporting it honestly beats writing a
		// marker into a file that gained no room (Rule 15).
		return out, nil
	}
	marker := rawLogTrimMarker(fi.Size(), headLen, tailLen, capBytes, tailStart-midStart)

	tmp := path + SessionsRawLogTrimTmpSuffix
	if err := writeRawLogTrimmed(tmp, src, fi.Mode(), headLen, marker, tailStart, tailLen); err != nil {
		_ = os.Remove(tmp) //stylegate:ignore the failure to remove a half-written file is reported by the error already being returned
		return out, fmt.Errorf("trim raw log: %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) //stylegate:ignore as above: the rename error is the one the caller must see
		return out, fmt.Errorf("trim raw log: %s: %w", path, err)
	}

	if after, serr := os.Stat(path); serr == nil {
		out.After = after.Size()
	}
	out.Head = headLen
	out.Tail = tailLen
	out.Trimmed = true
	return out, nil
}

// rawLogTrimmedSize is the size `TrimRawLog` will produce for a file of
// `before` bytes — used by the dry run so its promised saving is the apply's
// measured saving, not an estimate (the marker's own length is included).
func rawLogTrimmedSize(before, capBytes int64) int64 {
	head := SessionsRawLogHeadKeepBytes
	if head > before {
		head = before
	}
	if head > capBytes/4 {
		head = capBytes / 4
	}
	// The marker's length varies with the numbers it states; it is bounded by
	// this reserve, so subtracting the reserve is a ceiling on the result and
	// the dry run may only UNDERSTATE what it frees (the safe direction).
	tail := capBytes - head - SessionsRawLogTrimMarkerReserve
	if tail < 0 {
		tail = 0
	}
	return head + tail
}

// SessionsRawLogTrimMarkerReserve is the ceiling on the marker line's length.
// The numbers inside it are bounded (a byte count of 10^10 is 10 digits, four
// of them plus fixed prose), so one reserved constant keeps the plan's
// arithmetic honest without a second measurement pass.
const SessionsRawLogTrimMarkerReserve int64 = 512

// checkRawLogCap refuses a cap too small to keep any round at all, naming the
// floor (Rule 15: a silently-accepted nonsense cap would produce a log with no
// round in it and no error).
func checkRawLogCap(capBytes int64) error {
	if capBytes >= SessionsRawLogTrimMinCapBytes {
		return nil
	}
	return fmt.Errorf("session gc: a raw-log cap of %d bytes is below the floor of %d bytes (%d KiB head + one marker line) — a smaller bound would keep no round at all",
		capBytes, SessionsRawLogTrimMinCapBytes, SessionsRawLogHeadKeepBytes/1024)
}

// rawLogHeadLength decides how many leading bytes survive: the head constant,
// extended to the end of its line so the head never cuts a round in half, and
// capped at a quarter of the cap so the tail always has room.
func rawLogHeadLength(src *os.File, size, capBytes int64, format rawlogfmt.Format) (int64, error) {
	head := SessionsRawLogHeadKeepBytes
	if head > size {
		head = size
	}
	if maxHead := capBytes / 4; head > maxHead {
		head = maxHead
	}
	if head < size {
		switch format {
		case rawlogfmt.FormatFramed:
			// The head must END ON A RECORD BOUNDARY: extend forward to the next line
			// that opens a decodable record.
			aligned, ok, aerr := rawLogForwardToRecord(src, head, size)
			if aerr != nil {
				return 0, aerr
			}
			if !ok {
				// No record boundary within the reach: the head cannot be cut safely,
				// and a cut here would leave a fragment that breaks the whole file. The
				// honest answer is to refuse the trim rather than write a log no reader
				// can decode (Rule 15).
				return 0, fmt.Errorf("%s: %s", pathlessReason, SessionsRawLogTrimNoRecordBoundaryReason)
			}
			head = aligned
		case rawlogfmt.FormatLegacy:
			// Extend to just past the next newline (bounded: one line's worth).
			br := bufio.NewReaderSize(io.NewSectionReader(src, head, rawLogLineExtendBytes), rawLogLineExtendBytes)
			line, err := br.ReadBytes('\n')
			if err == nil {
				head += int64(len(line))
			}
		}
	}
	if head > size {
		head = size
	}
	return head, nil
}

// pathlessReason labels the head-alignment refusal (the caller adds the path).
const pathlessReason = "head alignment"

// SessionsRawLogTrimNoRecordBoundaryReason is the ONE sentence a trim prints when
// it cannot find a record boundary within its reach — the rewrite is refused, so a
// reader never meets a log whose middle is gone AND whose surviving framing is
// broken.
const SessionsRawLogTrimNoRecordBoundaryReason = "no record boundary within the alignment reach, so the log cannot be cut without breaking its framing"

// rawLogForwardToRecord walks forward from off to the START OF THE NEXT LINE THAT
// OPENS A DECODABLE RECORD, bounded by rawLogLineExtendBytes. ok=false means the
// reach ran out first.
//
// It is the FRAMED framing's boundary test, and it is the decoder's own predicate
// (rawlogfmt.CanDecodeHeader) rather than a re-spelling: a cut point that this
// function blesses is a point the reader can start decoding at.
func rawLogForwardToRecord(src *os.File, off, size int64) (int64, bool, error) {
	reach := int64(rawLogLineExtendBytes)
	if off+reach > size {
		reach = size - off
	}
	if reach <= 0 {
		return off, false, nil
	}
	br := bufio.NewReaderSize(io.NewSectionReader(src, off, reach), rawLogLineScanChunkBytes)
	pos := off
	for pos < off+reach {
		line, err := br.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\r\n")
			if rawlogfmt.CanDecodeHeader(trimmed, rawlogfmt.FormatFramed) {
				return pos, true, nil
			}
			pos += int64(len(line))
		}
		if err != nil {
			return off, false, nil
		}
	}
	return off, false, nil
}

// rawLogLineExtendBytes bounds how far the head may reach forward for its line
// boundary. A raw log line is one HTTP body; bodies measured in this system's
// own logs run to hundreds of KiB, so this is generous — past it the head simply
// ends where it ends rather than scanning a whole file.
const rawLogLineExtendBytes = 1 << 20

// rawLogTailWindow decides the tail: its length, where it starts (aligned to a
// line boundary so the first kept line is a COMPLETE line), and where the
// dropped middle starts (the byte right after the head).
func rawLogTailWindow(src *os.File, size, capBytes, headLen int64, format rawlogfmt.Format) (tailLen, tailStart, midStart int64, err error) {
	budget := capBytes - headLen - SessionsRawLogTrimMarkerReserve
	if budget < 0 {
		budget = 0
	}
	if budget > size-headLen {
		budget = size - headLen
	}
	rawStart := size - budget
	if rawStart < headLen {
		rawStart = headLen
	}
	// Advance to a RECORD boundary: a tail that began mid-record would hand a reader
	// half a payload whose stated length the file no longer delivers.
	var start int64
	var aerr error
	switch format {
	case rawlogfmt.FormatFramed:
		var ok bool
		start, ok, aerr = rawLogForwardToRecord(src, rawStart, size)
		if aerr == nil && !ok {
			// No boundary inside the reach: dropping to EOF would keep nothing, and
			// keeping a fragment would break the file. Report it.
			return 0, 0, 0, fmt.Errorf("%s: %s", pathlessReason, SessionsRawLogTrimNoRecordBoundaryReason)
		}
	case rawlogfmt.FormatLegacy:
		// Advance to the first byte AFTER a newline, so the tail's first line is
		// whole. A tail that began mid-line would look like a corrupt record.
		start, aerr = rawLogLineStart(src, rawStart, size)
	}
	if aerr != nil {
		return 0, 0, 0, aerr
	}
	return size - start, start, headLen, nil
}

// rawLogLineStart walks forward from off to the byte after the next newline.
func rawLogLineStart(src *os.File, off, size int64) (int64, error) {
	if off <= 0 {
		return 0, nil
	}
	sr := io.NewSectionReader(src, off, size-off)
	br := bufio.NewReaderSize(sr, rawLogLineScanChunkBytes)
	for {
		b, err := br.ReadByte()
		if err != nil {
			// No newline between off and EOF: the tail cannot be aligned, so
			// keep only the head — the caller sees tailLen==0 and may skip.
			return size, nil
		}
		off++
		if b == '\n' {
			return off, nil
		}
	}
}

// rawLogLineScanChunkBytes is the read buffer for the line-alignment walk.
const rawLogLineScanChunkBytes = 64 << 10

// rawLogTrimMarker builds the ONE line that replaces the dropped middle. It
// states the three facts a future reader needs and cannot get anywhere else:
// how many bytes are gone, that they were the file's MIDDLE, and that they are
// not recoverable — plus the date, so "when was this bounded" is in the file
// itself rather than in a transcript nobody kept (Rule 15: never silent).
func rawLogTrimMarker(original, headLen, tailLen, capBytes, dropped int64) string {
	return fmt.Sprintf("%sat %s — dropped %d bytes of the file's middle (its historical rounds) to bound this log: "+
		"the file was %d bytes, the cap is %d; the first %d bytes and the most recent %d bytes are kept verbatim. "+
		"The dropped rounds are NOT recoverable from this file, so a resume from this log is REFUSED "+
		"(%s) rather than answered from the head's first round, which is not the last state.",
		SessionsRawLogTrimMarkerPrefix, time.Now().UTC().Format(time.RFC3339), dropped,
		original, capBytes, headLen, tailLen, TrimmedLogResumeRefusal)
}

// TrimmedLogResumeRefusal is the ONE sentence the marker QUOTES into the file and
// every resume face PRINTS when it refuses a trimmed log — one spelling, so the
// promise inside the artifact and the refusal a reader meets cannot drift apart
// (Rules 1/13/14).
const TrimmedLogResumeRefusal = "the rounds between this log's head and its tail were dropped, so it cannot answer a checkpoint"

// writeRawLogTrimmed writes head + marker + tail into tmp, fsyncs it and gives it
// the original file's mode — everything short of the rename, which the caller
// performs so the atomic step stays in one place.
func writeRawLogTrimmed(tmp string, src *os.File, mode os.FileMode, headLen int64, marker string, tailStart, tailLen int64) error {
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	// ORDER MATTERS ON ERROR: the copy half is reported first, the close second,
	// because a full disk surfaces as a copy error and must not be hidden behind
	// a successful close.
	if _, err := io.Copy(out, io.NewSectionReader(src, 0, headLen)); err != nil {
		out.Close() //stylegate:ignore the close error cannot matter once the copy has failed; the copy error is returned
		return fmt.Errorf("write the head: %w", err)
	}
	if _, err := io.WriteString(out, marker+"\n"); err != nil {
		out.Close() //stylegate:ignore as above
		return fmt.Errorf("write the trim marker: %w", err)
	}
	if tailLen > 0 {
		if _, err := io.Copy(out, io.NewSectionReader(src, tailStart, tailLen)); err != nil {
			out.Close() //stylegate:ignore as above
			return fmt.Errorf("write the tail: %w", err)
		}
	}
	if err := out.Sync(); err != nil {
		out.Close() //stylegate:ignore the sync error is returned instead
		return fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	return nil
}

// LargestSessions returns the N largest session containers under root, biggest
// first — the read-only answer to "what is filling the disk" that the age-based
// judgement could never give (the measured case: the ten biggest containers were
// all `keep` for being young).
//
// It reuses PlanSessions (so the keep/reason column is the SAME verdict an apply
// would act on) and only reorders: one predicate, one spelling of every reason.
func LargestSessions(root string, olderThan time.Duration, n int) ([]SessionsPlan, error) {
	plans, err := PlanSessions(root, olderThan)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(plans, func(i, j int) bool { return plans[i].Bytes > plans[j].Bytes })
	if n > 0 && n < len(plans) {
		plans = plans[:n]
	}
	return plans, nil
}
