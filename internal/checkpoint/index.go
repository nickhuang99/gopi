package checkpoint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/rawlogfmt"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// The raw log's two layouts, spelled with the WRITER's own constants (Rules
// 1/13/14): internal/agent.newRawLoggerInDir builds both names from these
// values (scratch.RawLogNamePrefix/RawLogFileSuffix for the legacy flat file,
// scratch.SessionRawLogFileSuffix for the session-dir file), so a reader that
// re-spelled them could drift from the writer without any test noticing.
//
//	legacy flat   <raw dir>/gopi-raw-YYYYMMDD-HHMMSS.log
//	session dir   <sessions>/<run-dir>/<run-uuid>.raw.log   (session-dir-unify S1, 3a816775)
const (
	rawLogNamePrefix        = scratch.RawLogNamePrefix
	rawLogFileSuffix        = scratch.RawLogFileSuffix
	sessionRawLogFileSuffix = scratch.SessionRawLogFileSuffix
)

// The same three values, re-exported. They are EXPORTED because the CLI's id
// resolver must build the very names this package indexes: a caller that
// re-spelled "gopi-raw-" or ".raw.log" beside the indexer would be the
// mirror-literal defect Rules 1/13 name — the two faces would drift and one
// would stop finding what the other accepts.
const (
	RawLogNamePrefix        = rawLogNamePrefix
	RawLogFileSuffix        = rawLogFileSuffix
	SessionRawLogFileSuffix = sessionRawLogFileSuffix
)

// maxSkipsReported caps the self-report's skip list: a sessions tree holds
// thousands of entries and a listing must not turn a diagnosis into a wall of
// text. The cap is REPORTED in the report's SkipsOmitted, never silent.
const maxSkipsReported = 20

// How many levels IndexTree descends below a sessions root to find run
// directories. A run directory is the sessions root's DIRECT child (bare runs:
// <sessions>/<run-uuid>/<run-uuid>.raw.log, internal/agent.sessionDirForID;
// task runs: <sessions>/<task-id>/<run-uuid>.raw.log), so ONE level is the
// co-located layout. It is deliberately NOT a whole-tree walk: the sessions
// tree carries entire private profiles under <run-dir>/evidence/**/.gopi/
// sessions/… (measured on this host: 1298 of 1265 raw logs under the root are
// one task's evidence copies, 32 MB apiece). Walking those would make
// `checkpoint list` re-read a task's own scratch tree — the #1313 defect.
const sessionScanDepth = 1

// Skip reasons — the two rules an entry can fail, as named values so the
// self-report at the CLI and the filter here cannot say different things.
const (
	skipReasonNotARawLog    = "name matches neither layout"
	skipReasonNotAFile      = "not a regular file"
	skipReasonDirUnreadable = "directory could not be read"
)

// MaxScanRootEntries bounds how many entries a directory the scan would DESCEND
// into may hold before IndexTree REFUSES it (Rule 1: a limit is a named
// constant, never a literal in the loop).
//
// WHY A BOUND IS NEEDED AT ALL. A scan root is descended one level, and the
// descent is a directory listing PER CHILD — the price of finding the co-located
// layout (<sessions>/<run-dir>/<run-uuid>.raw.log). That price is invisible on
// an ordinary profile (tens of run directories) and unbounded on a root that has
// accumulated years of runs: measured on peer1 2026-10-07, ~/.gopi/sessions held
// 83960 first-level entries and each child listing cost 8.7 ms, so the walk
// projects to ~12 minutes. `gopi session checkpoint list` was killed at 30 s
// with rc=137 having printed NOTHING (real 4m40s / user 0.002s in the operator's
// report; real 30.0s / user 0.001s in the reproduction) — zero CPU, so the block
// is I/O, and Rule 15's real defect there is the SILENCE, not the wait.
//
// WHY 20000. The value must sit far above a real profile and far below the
// pathological root, so it never refuses work anyone meant to do:
//
//	one run day     ~40-60 entries     (measured: 83960 entries over ~3 years)
//	a busy year     ~2000              → 10x headroom below the bound
//	THIS bound      20000              → ~4x below the observed root
//	observed root   83960
//
// At the bound the walk costs a bounded ~3 minutes worst case (20000 × 8.7 ms),
// and a root that large is a diagnostic case — its owner wants `--dir` or
// GOPI_RAW_LOG_DIR, which the refusal names.
//
// THE CHECK IS ON THE ENTRY COUNT, NEVER ON WALL-CLOCK TIME: a timeout would be
// non-deterministic (the same root passes or fails with the page cache) and
// would silently truncate a walk that a slower day made late. A count is a
// property of the input, so the refusal is reproducible and testable.
const MaxScanRootEntries = 20000

// readDirChunkSize is how many entries one ReadDir call returns (Rule 1: a
// chunk is a limit like any other). It bounds what the refusal path WASTES:
// entries are read one chunk at a time and the scan stops the moment the bound
// is crossed, so refusing a root costs at most MaxScanRootEntries + this many
// entries of listing — never the whole root. The gap is real on this host: the
// full listing of the operator's sessions root took 0.33 s warm and 16.9 s while
// another run saturated the same disk, so a bound that had to count all 83960
// entries would inherit that variance — and could be killed before it refused.
const readDirChunkSize = 4096

// RefusalScanRootTooLarge is the CODE of the one refusal this package raises: a
// directory the scan would descend into holds more entries than
// MaxScanRootEntries. It is a named value (Rules 1b/13) because the error text,
// the CLI's next-step note and the tests all key on the same string — a second
// spelling at any of the three is the drift the constant prevents.
//
// It says SCAN ROOT, not sessions root, because the bound is decided by the
// WALK, not by the directory's name: the default path is a sessions root, and a
// `--dir` naming any other directory is descended the same way (one level of
// children), so one limit governs both and the refusal must not misname the
// directory it refused.
const RefusalScanRootTooLarge = "scan-root-too-large"

// NamedRefusal is a REFUSAL WITH A NAME: the caller must not merely learn that
// the scan stopped, it must be able to tell WHICH bound stopped it, on WHICH
// path, with WHICH numbers — so it can add the next step without re-deriving
// anything (Rule 15: a refusal explains itself; Rule 14: the CLI and the indexer
// share the type rather than agreeing on prose).
type NamedRefusal struct {
	Code string // RefusalScanRootTooLarge
	Path string // the directory the scan refused to descend into
	// Entries is a LOWER BOUND on the refused directory's entry count, and Limit
	// is the bound it crossed. Entries is a lower bound because the read stops at
	// Limit+1 — reading the rest would buy exactly the cost the refusal exists to
	// avoid — so the text says "at least" instead of claiming a total nobody
	// counted.
	Entries int
	Limit   int
}

// Error renders the refusal verbatim: the code, the path and both numbers, in
// one sentence a log reader can act on without the source.
func (e *NamedRefusal) Error() string {
	return fmt.Sprintf("%s: %s holds at least %d entries, over the %d-entry scan limit",
		e.Code, e.Path, e.Entries, e.Limit)
}

// scanCriteriaDescription is the acceptance rule, spelled ONCE as a format so
// the report's own numbers are part of the judgement an operator reads. A scan
// that finds nothing must be able to say by what rule it looked (Rule 15) —
// otherwise "no raw logs found" cannot be told apart from a wrong directory, a
// filename rule the writer has outgrown, or a layout this indexer has not
// learned.
const scanCriteriaDescription = `a file is a raw log iff its name ends %q (co-located with its run) or starts %q and ends %q (legacy flat); a sessions root is descended %d level(s) into its run directories`

func criteriaText() string {
	return fmt.Sprintf(scanCriteriaDescription, sessionRawLogFileSuffix, rawLogNamePrefix, rawLogFileSuffix, sessionScanDepth)
}

// DirScan is one directory the scan listed, with what it found there.
type DirScan struct {
	Path    string // the directory's path
	Entries int    // entries listed in it
	Logs    int    // raw logs this scan accepted in it
	Err     string // listing error, if any — an unreadable directory is REPORTED, never silently empty (Rule 15)
}

// Skip is one entry the scan refused, with the rule that refused it.
type Skip struct {
	Path   string
	Reason string
}

// ScanReport is the indexer's SELF-REPORT: every directory it actually listed
// and by what rule it accepted or refused each entry. A scan that finds
// nothing must be able to say WHY (Rule 15) — otherwise "no raw logs found"
// is indistinguishable from a wrong directory, a filename rule that no longer
// matches what the writer mints, or a layout this indexer has not learned.
type ScanReport struct {
	// ScanRoot is the directory the caller named (the scan's own root), so a
	// report stays readable when it covers more than one directory.
	ScanRoot string
	Roots    []DirScan // one per directory listed, in scan order
	Skips    []Skip    // entries the name rule refused (capped at maxSkipsReported)
	Files    int       // regular files considered across all directories
	Skipped  int       // total refusals, including ones past the cap
}

// Criteria returns the acceptance rule this scan applied, verbatim — the
// "judgement" half of the self-report.
func (r ScanReport) Criteria() string { return criteriaText() }

// Describe renders the self-report as the human-facing lines `checkpoint
// list` prints (both when it found logs and when it found none). Kept here,
// beside the filter it describes, so the printed judgement IS the applied one.
func (r ScanReport) Describe() []string {
	out := []string{fmt.Sprintf("scanned %d directory(ies) for raw logs; criteria: %s",
		len(r.Roots), r.Criteria())}
	for _, d := range r.Roots {
		line := fmt.Sprintf("  %s — %d entries, %d raw log(s)", d.Path, d.Entries, d.Logs)
		if d.Err != "" {
			line += " [" + d.Err + "]"
		}
		out = append(out, line)
	}
	for _, s := range r.Skips {
		out = append(out, fmt.Sprintf("  skipped %s (%s)", s.Path, s.Reason))
	}
	if omitted := r.Skipped - len(r.Skips); omitted > 0 {
		out = append(out, fmt.Sprintf("  … and %d more skipped entries", omitted))
	}
	return out
}

// LogIndex is the checkpoint registry entry for one raw log: how many
// requests it contains and what the LAST one is (the resume point).
type LogIndex struct {
	Path        string // full path to the raw log
	Requests    int    // number of → REQUEST entries
	LastModel   string // model of the last request
	LastMsgs    int    // message count of the last request
	LastBytes   int    // payload size of the last request (bytes)
	LastSHA256  string // sha256 of the last request payload (dedupe)
	LastMessage string // first 60 chars of the last request's last message
	ModTime     string // file mtime (RFC3339)

	// RequestsTruncated reports that Requests is a HEAD-SAMPLE count, not
	// the exact total: counting is bounded by countScanBudgetBytes to keep
	// `checkpoint list` fast on multi-GB logs (a full scan of the raw dir
	// used to read every byte of every log — 19GB → minutes of hang).
	RequestsTruncated bool
}

// TrimmedLogIndexLabel is what "gopi session checkpoint list" shows in the
// last-model column of a log THIS TOOL trimmed: the file is intact and its
// marker is intact, but its checkpoint cannot be answered from it.
const TrimmedLogIndexLabel = "TRIMMED (no checkpoint)"

// IsRawLogName reports whether a directory entry's name is a raw log this
// system writes, in EITHER layout — the one predicate every reader shares
// (Rule 14: the value's owner is internal/scratch, the rule lives here once).
func IsRawLogName(name string) bool {
	if strings.HasPrefix(name, rawLogNamePrefix) && strings.HasSuffix(name, rawLogFileSuffix) {
		return true
	}
	return strings.HasSuffix(name, sessionRawLogFileSuffix)
}

// IndexDir scans a SINGLE directory for raw logs (legacy flat layout) and
// returns a registry of the checkpoint each log ends at, newest filename
// first. Read-only — safe on the live raw directory.
//
// It is IndexTree with no descent: the directory is listed once and only the
// files in it are considered.
func IndexDir(dir string) ([]LogIndex, error) {
	idxs, _, err := IndexTree(dir)
	return idxs, err
}

// IndexTree scans dir and — when dir is a sessions root (see isSessionsRoot) —
// the run directories below it, returning the registry of raw logs found along
// with the SCAN REPORT that says which directories were listed and by what rule
// each entry was accepted or refused.
//
// Why two layouts (session-dir-unify S1, 3a816775): a run's raw HTTP log used
// to land flat in <gopi dir>/raw (gopi-raw-YYYYMMDD-HHMMSS.log) and now
// co-locates with the session that wrote it —
//
//	<sessions>/<task-id>/<run-uuid>.raw.log        (task run)
//	<sessions>/<run-uuid>/<run-uuid>.raw.log       (bare run / chat)
//
// — so a reader that only knew the flat layout indexed nothing at all and
// `checkpoint list` answered "no raw logs found" while gigabytes sat one
// directory below. Both layouts are accepted here; legacy behaviour is
// unchanged (a flat raw dir still lists exactly what it listed before).
//
// Depth: descendants are considered only when dir's own name is the sessions
// root name — a `--dir` pointing at a flat raw directory or at one run
// directory is listed as a plain directory, never walked. The bound is
// sessionScanDepth, a named constant, never a literal in the loop.
func IndexTree(dir string) ([]LogIndex, ScanReport, error) {
	var idxs []LogIndex
	var rep ScanReport
	seen := map[string]bool{}
	// dirs is a worklist rather than recursion: each entry carries the depth it
	// was reached at, so the descent bound is data (sessionScanDepth) and not a
	// property of the call stack.
	type scanDir struct {
		path  string
		depth int
	}
	queue := []scanDir{{path: dir}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur.path] {
			continue
		}
		seen[cur.path] = true

		entries, over, err := readEntries(cur.path)
		if err != nil {
			// An unreadable directory is REPORTED in the scan report, never
			// silently empty — but the FIRST directory is the caller's own
			// argument, and a refusal there is an error to return (Rule 15).
			if len(rep.Roots) == 0 {
				return nil, rep, fmt.Errorf("checkpoint: list %s: %w", cur.path, err)
			}
			rep.Roots = append(rep.Roots, DirScan{Path: cur.path, Err: skipReasonDirUnreadable + ": " + err.Error()})
			continue
		}
		if len(rep.Roots) == 0 {
			rep.ScanRoot = cur.path
		}
		found := 0
		descend := cur.path == dir || isSessionsRoot(cur.path)
		// THE BOUND (see MaxScanRootEntries): a directory the scan WOULD descend
		// into is refused BEFORE any child is queued or listed, and `over` comes
		// from readEntries' own stop-at-the-bound read — so the refusal costs one
		// bounded read, never the full listing of the root it is refusing. The
		// refused root is a DIAGNOSTIC case, so it is returned as a refusal
		// rather than scanned as a first-N slice: a truncated listing cannot be
		// told apart from a complete one (Rule 15), and the caller needs the
		// numbers to offer the operator a next step.
		if descend && cur.depth < sessionScanDepth && over {
			return nil, rep, &NamedRefusal{
				Code:    RefusalScanRootTooLarge,
				Path:    cur.path,
				Entries: len(entries),
				Limit:   MaxScanRootEntries,
			}
		}
		for _, de := range entries {
			child := filepath.Join(cur.path, de.Name())
			if de.IsDir() {
				if descend && cur.depth < sessionScanDepth {
					queue = append(queue, scanDir{path: child, depth: cur.depth + 1})
				}
				continue
			}
			rep.Files++
			if !IsRawLogName(de.Name()) {
				rep.recordSkip(child, skipReasonNotARawLog)
				continue
			}
			fi, err := de.Info()
			if err != nil || !fi.Mode().IsRegular() {
				rep.recordSkip(child, skipReasonNotAFile)
				continue
			}
			idx, err := indexOne(child)
			if err != nil {
				// A corrupt/partial log must not break the whole list — report
				// what we can and keep going.
				idx = LogIndex{Path: child, LastModel: "ERROR: " + err.Error()}
			}
			idxs = append(idxs, idx)
			found++
		}
		rep.Roots = append(rep.Roots, DirScan{Path: cur.path, Entries: len(entries), Logs: found})
	}
	sort.Slice(idxs, func(i, j int) bool { return idxs[i].Path > idxs[j].Path }) // newest filename first
	return idxs, rep, nil
}

// recordSkip appends one refusal, capped at maxSkipsReported; the overflow is
// counted so the report can say how many it did not print.
func (r *ScanReport) recordSkip(path, reason string) {
	r.Skipped++
	if len(r.Skips) < maxSkipsReported {
		r.Skips = append(r.Skips, Skip{Path: path, Reason: reason})
	}
}

// readEntries lists dir in chunks of readDirChunkSize and STOPS as soon as it
// holds more than MaxScanRootEntries entries, reporting over=true. It returns
// the entries it read (for a directory at or under the bound: all of them).
//
// Why not one os.ReadDir call: a single call materialises and sorts every entry
// of the directory before the caller can apply any limit, so the bound would be
// decided only AFTER paying the full listing — measured on this host, 0.33 s
// warm and 16.9 s under another run's disk load for the operator's 83960-entry
// sessions root. Reading in chunks moves the decision in front of the cost: a
// root that will be refused is read to Limit+1 and no further, while every
// directory at or under the bound is read exactly as before (os.ReadDir's own
// ordering, and the caller sorts what it keeps).
//
// os.File.ReadDir (unlike os.ReadDir) returns entries in directory order — fine
// here: the caller's output is sorted by path (IndexTree) and the entry COUNT is
// what the bound and the scan report are about, neither of which depends on
// order.
func readEntries(dir string) ([]os.DirEntry, bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	var all []os.DirEntry
	for {
		chunk, err := f.ReadDir(readDirChunkSize)
		all = append(all, chunk...)
		if len(all) > MaxScanRootEntries {
			// Truncate to the bound + 1: the caller only needs "over the limit,
			// and by how much at least", and the chunk that crossed the bound can
			// overshoot by up to readDirChunkSize entries.
			return all[:MaxScanRootEntries+1], true, nil
		}
		if err == io.EOF {
			return all, false, nil
		}
		if err != nil {
			return all, false, err
		}
		if len(chunk) == 0 {
			// Defensive: a zero-length read with no EOF would spin forever.
			// os.File.ReadDir documents EOF at end-of-directory, so this arm is
			// unreachable — reported as EOF rather than trusted to never happen
			// (Rule 15: no silent loop).
			return all, false, nil
		}
	}
}

// isSessionsRoot reports whether dir is a sessions root — the directory whose
// children are run directories — by its OWN name (config.SessionsDirName, the
// single definition the writer uses). Name, not path: the root is whatever
// `<profile dir>/sessions` resolves to, and a relocated profile (GOPI_HOME, a
// sandbox, --dir) keeps the name. A user's `--dir ./my-logs` is therefore
// listed flat, exactly as before this change.
func isSessionsRoot(dir string) bool {
	return filepath.Base(filepath.Clean(dir)) == config.SessionsDirName
}

func indexOne(path string) (LogIndex, error) {
	var idx LogIndex
	last, req, err := LastRequest(path)
	if err != nil {
		// A TRIMMED LOG IS STILL INDEXABLE, and saying so is the point: the
		// list must not print a bare parse error for a file this system itself
		// rewrote (Rule 15 — a refusal names its cause). Its tail is intact, so
		// a bounded marker count still answers "how big was this run"; its
		// checkpoint fields stay empty because the file cannot answer them.
		if errors.Is(err, ErrTrimmedRawLog) {
			idx.Path = path
			idx.LastModel = TrimmedLogIndexLabel
			idx.Requests, idx.RequestsTruncated, _ = CountRequests(path) //stylegate:ignore a count is decorative here; the row is already labelled as untrimmed-answerable and the parse refusal is what the caller must see
			if fi, serr := os.Stat(path); serr == nil {
				idx.ModTime = fi.ModTime().Format("2006-01-02 15:04:05")
			}
			return idx, nil
		}
		return idx, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return idx, err
	}
	idx.Path = path
	idx.LastModel = req.Model
	idx.LastMsgs = len(req.Messages)
	idx.LastBytes = len(last.Payload)
	sum := sha256.Sum256(last.Payload)
	idx.LastSHA256 = hex.EncodeToString(sum[:8])
	idx.ModTime = fi.ModTime().Format("2006-01-02 15:04:05")
	if n := len(req.Messages); n > 0 {
		m := req.Messages[n-1]
		if len(m.Content) > 0 {
			b, _ := json.Marshal(m.Content[0]) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
			s := string(b)
			if len(s) > 60 {
				s = s[:60] + "…"
			}
			idx.LastMessage = s
		}
	}
	// Request count via a bounded head scan (countScanBudgetBytes). A full
	// marker-walking scan over every byte of a multi-GB log is too slow on
	// the accumulated raw dir (19GB across ~1200 logs → `checkpoint list`
	// read everything = minutes of hang). A payload line can never begin
	// with "→ REQUEST" (request/response payloads are JSON — newlines
	// escaped; chunk payloads are SSE lines starting "event:"/"data:"), so
	// line-boundary counting is exact up to the budget cutoff.
	idx.Requests, idx.RequestsTruncated, err = CountRequests(path)
	if err != nil {
		return idx, err
	}
	return idx, nil
}

// countScanBudgetBytes bounds how much of a raw log CountRequests reads.
// Real logs reach hundreds of MB to >1GB; the request count is a display
// hint (resume picks the log by last-checkpoint fields, which the cheap
// tail scan provides), so reading the whole file for an exact total is
// wasted I/O. Request density is ~4-5 per MB, so a 16MB head sample shows
// ~70-80 requests — representative while bounding `checkpoint list` to
// ≤ budget × #logs bytes (a 19GB raw dir drops from ~19GB of reads to
// ~5GB worst case, and typical runs far less).
const countScanBudgetBytes = 16 * 1024 * 1024 // 16MB head sample per log

// CountRequests head-samples a raw log and counts → REQUEST entries,
// requiring each marker to sit at a line boundary. It stops after reading
// countScanBudgetBytes and reports truncated=true when the file is bigger
// than the budget (the returned count is then a lower bound from the head).
// O(min(file size, budget)) time, O(chunk) memory.
func CountRequests(path string) (count int, truncated bool, err error) {
	// TWO FRAMINGS, and the discriminator is DETECTED, never assumed: counting
	// `→ REQUEST` lines in a framed log returns 0 for every file the writer now
	// produces, and a zero here reads as "this run sent nothing", which is a
	// confident wrong answer rather than a failure (Rule 15).
	format, ferr := rawlogfmt.Detect(path)
	if ferr != nil {
		return 0, false, fmt.Errorf("checkpoint: %s: %w", path, ferr)
	}
	if format == rawlogfmt.FormatFramed {
		return countRequestsFramed(path)
	}
	const (
		readChunkSize = 4 * 1024 * 1024
		markerLen     = len("→ REQUEST\n")
	)
	f, err := os.Open(path)
	if err != nil {
		return 0, false, fmt.Errorf("checkpoint: open %s: %w", path, err)
	}
	defer f.Close()

	read := 0
	carry := make([]byte, 0, markerLen-1) // tail of the previous chunk (boundary check)
	buf := make([]byte, readChunkSize)
	needle := []byte("→ REQUEST\n")
	for read < countScanBudgetBytes {
		chunk := buf
		if budget := countScanBudgetBytes - read; budget < len(chunk) {
			chunk = buf[:budget]
		}
		n, rerr := f.Read(chunk)
		if n > 0 {
			read += n
			data := append(carry, chunk[:n]...)
			// Count line-boundary markers via bytes.Index (memmem-fast) —
			// a per-byte scan would be O(n·len(marker)) over multi-GB logs.
			from := 0
			for {
				j := bytes.Index(data[from:], needle)
				if j < 0 {
					break
				}
				abs := from + j
				if abs == 0 || data[abs-1] == '\n' {
					count++
				}
				from = abs + markerLen
			}
			// Keep the last markerLen-1 bytes so a marker split across the
			// chunk boundary is still caught; a marker at a line boundary
			// can only start after the previous newline, which the carry
			// preserves.
			keep := markerLen - 1
			if len(data) < keep {
				keep = len(data)
			}
			carry = append(carry[:0], data[len(data)-keep:]...)
		}
		if rerr == io.EOF {
			return count, false, nil
		}
		if rerr != nil {
			return count, false, fmt.Errorf("checkpoint: read %s: %w", path, rerr)
		}
	}
	// Budget consumed. EOF may already have been reported above (file
	// smaller than or exactly the budget → not truncated); otherwise probe
	// one byte: EOF on the probe means the file ends exactly at the budget,
	// any byte means it continues past it (truncated head sample).
	one := make([]byte, 1)
	_, rerr := f.Read(one)
	if rerr == io.EOF {
		return count, false, nil
	}
	if rerr != nil {
		return count, false, fmt.Errorf("checkpoint: read %s: %w", path, rerr)
	}
	return count, true, nil
}

// countRequestsFramed counts request records in a FRAMED raw log with the same
// budget contract as the legacy head-sample: it reads at most
// countScanBudgetBytes and reports truncated=true when the file continues past
// them, so the number it returns is a lower bound in exactly the same sense.
//
// It decodes RECORDS rather than matching lines, because in this framing a payload
// may contain the marker vocabulary verbatim — a line count would report rounds the
// log does not have.
//
// A record cut in half by the budget is not counted (its run has not finished), and
// the cut is what truncated=true reports: a partially read record is a fact about
// the SAMPLE, not a malformed file.
func countRequestsFramed(path string) (count int, truncated bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false, fmt.Errorf("checkpoint: open %s: %w", path, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, false, fmt.Errorf("checkpoint: stat %s: %w", path, err)
	}
	head := fi.Size()
	truncated = head > countScanBudgetBytes
	if truncated {
		head = countScanBudgetBytes
	}
	buf := make([]byte, head)
	if head > 0 {
		if _, rerr := f.ReadAt(buf, 0); rerr != nil && rerr != io.EOF {
			return 0, truncated, fmt.Errorf("checkpoint: read %s: %w", path, rerr)
		}
	}
	r := rawlogfmt.NewReaderFrom(bytes.NewReader(buf), rawlogfmt.FormatFramed)
	for {
		rec, rerr := r.Next()
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return count, truncated, nil
			}
			// A decode failure at the sample's end is the cut, not a defect: the
			// record that straddles the budget boundary cannot be decoded and cannot
			// be counted, and the caller already knows the count is a lower bound.
			if truncated {
				return count, true, nil
			}
			return count, false, fmt.Errorf("checkpoint: %s: %w", path, rerr)
		}
		if rec.Tag == rawlogfmt.TagRequest || rec.Tag == rawlogfmt.TagProbeRequest {
			count++
		}
	}
}
