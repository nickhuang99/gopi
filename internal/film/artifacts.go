package film

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The delivered film's own shape, in ONE place (Rule 1: no bare literals; Rule
// 13: the constant must be REFERENCED, not merely defined). Every reader and
// every check names these, so "the frames live in frames/" cannot drift between
// the assembler and the check that measures them.
const (
	// FramesDirName is where the delivered frames live inside a film directory.
	FramesDirName = "frames"
	// GIFName is the assembled animation. NOTE the name: verify.py's
	// check_motion/check_colour look for `chat-replay.gif` (the name
	// tools/replay/assemble.py wrote, its own GIF_NAME constant, L57) while this
	// item's acceptance names `film.gif`. The check therefore takes the GIF's
	// path from the film's own metadata, with the delivered default for a film
	// that does not state one — one definition, no second search path.
	GIFName = "film.gif"
	// MetadataName is the run's arithmetic and provenance.
	MetadataName = "replay.json"
	// LedgerName is the beat ledger: one row per drawn beat, with its source line.
	LedgerName = "beats.tsv"
	// DumpsDirName is the per-beat grid dumps — the surface's own record of what
	// it drew, and the authority visibility is measured against.
	DumpsDirName = "dumps"
	// RedactionLogName is the masker's record of what it withheld.
	//
	// It is internal/replay.RedactionLogName, referenced (not re-typed) in
	// checks.go for the check that reads it; the value here is the same string
	// because a film directory carries one ledger and its name is not a
	// per-package decision (Rule 14 — the ONE definition is internal/replay's).
	RedactionLogName = "redactions.tsv"
	// SessionJSONLName is the recorded source session a film's frames came from.
	SessionJSONLName = "session.jsonl"
)

// PNGGlobSuffix is the extension a delivered frame carries. A frame is a PNG by
// construction (internal/shot writes them), so the check reads the directory.
const PNGGlobSuffix = ".png"

// ledgerMinColumns and ledgerHeaderPrefix are the ledger format's framing. The
// row shape itself (six columns, base64 text) is internal/replay's, whose
// ledger.go writes it and whose LedgerHeader is the ONE definition of the header
// line; the column count is read here because a reader must refuse a truncated
// row rather than index past it (Rule 15: no silent failure).
const (
	ledgerHeaderPrefix = "#"
	ledgerMinColumns   = 6
	ledgerBeatCol      = 0
	ledgerKindCol      = 1
	ledgerSrcLineCol   = 2
	ledgerAtMSCol      = 3
	ledgerPreviewCol   = 4
	ledgerTextCol      = 5
)

// LedgerRow is one beat of the ledger (verify.py's read_ledger, L219-L240).
type LedgerRow struct {
	Beat    int
	Kind    string
	SrcLine int
	AtMS    int
	Preview string
	Text    string
}

// Film is a film directory as the checks see it: the delivered output and the
// scratch the recording left behind.
//
// The split is verify.py's own (`--out` vs `--scratch`): the delivered directory
// holds what a reader gets, the scratch holds what the RUN left — the beat
// ledger, the grid dumps and the redaction log. A check that needs the run's own
// record reads the scratch; a check that measures the artifact reads the output.
type Film struct {
	// Out is the delivered film directory (frames/, the GIF, replay.json).
	Out string
	// Scratch holds the run's own record (beats.tsv, dumps/, redactions.tsv).
	// Empty means "look in Out first, then beside it" — see ScratchOr.
	Scratch string
	// SessionJSONL is the recorded session the frames came from (provenance).
	SessionJSONL string
}

// ScratchOr resolves the run's record directory: the explicit Scratch when one
// was given, else the film's own directory. It exists because a film copied for
// review carries its ledger beside it, and a caller that knows better passes the
// original scratch.
func (f Film) ScratchOr() string {
	if strings.TrimSpace(f.Scratch) != "" {
		return f.Scratch
	}
	return f.Out
}

// ReadLedger reads the beat ledger at scratch/beats.tsv.
//
// verify.py L219-L240. A row with fewer than six columns is SKIPPED, exactly as
// the Python does: the ledger is written by another process and a truncated tail
// row is not a reason to fail the read (the count check in the traceability
// criterion is what catches a short ledger).
func (f Film) ReadLedger() ([]LedgerRow, error) {
	path := filepath.Join(f.ScratchOr(), LedgerName)
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the beat ledger at %s: %w", path, err)
	}
	var rows []LedgerRow
	for _, line := range strings.Split(string(blob), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, ledgerHeaderPrefix) {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < ledgerMinColumns {
			continue
		}
		beat, err1 := strconv.Atoi(parts[ledgerBeatCol])
		srcLine, err2 := strconv.Atoi(parts[ledgerSrcLineCol])
		atMS, err3 := strconv.Atoi(parts[ledgerAtMSCol])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		text, err := base64.StdEncoding.DecodeString(parts[ledgerTextCol])
		if err != nil {
			continue
		}
		rows = append(rows, LedgerRow{
			Beat:    beat,
			Kind:    parts[ledgerKindCol],
			SrcLine: srcLine,
			AtMS:    atMS,
			Preview: parts[ledgerPreviewCol],
			Text:    string(text),
		})
	}
	return rows, nil
}

// ReadGridRows reads one grid dump's rows, WITH their gutters (verify.py's
// read_grid_rows, L891-L913): the gutters are what the surface drew, so they are
// kept here and removed by bare()/normalize() at the comparison sites.
func ReadGridRows(path string) ([]string, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the grid dump at %s: %w", path, err)
	}
	rows := map[int]string{}
	inside := false
	for _, raw := range strings.Split(string(blob), "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if strings.HasPrefix(line, dumpLinesBegin) {
			inside = true
			continue
		}
		if strings.HasPrefix(line, dumpLinesEnd) {
			inside = false
			continue
		}
		if !inside {
			continue
		}
		m := dumpLinePrefix.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		rows[n] = m[2]
	}
	keys := make([]int, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, rows[k])
	}
	return out, nil
}

// SourceLines reads the recorded session a film's beats claim to come from
// (verify.py's source_lines, L266-L268). Line NUMBERS are 1-based, which is what
// the ledger's src_line column carries.
func (f Film) SourceLines() ([]string, error) {
	blob, err := os.ReadFile(f.SessionJSONL)
	if err != nil {
		return nil, fmt.Errorf("read the recorded session at %s: %w", f.SessionJSONL, err)
	}
	lines := strings.Split(string(blob), "\n")
	// A trailing newline produces one empty element Python's readlines() would
	// not have counted; it changes the line COUNT the provenance check reports,
	// so it is dropped rather than carried.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines, nil
}

// Metadata is replay.json: the arithmetic and the geometry a reader acts on.
//
// The field names are the file's OWN spelling (verify.py reads them by name:
// `raw_frames`, `distinct_frames`, `dropped_frames`, `beats`, `term_cols`,
// `window_px`), so the struct is the format rather than a preference — renaming
// a field here would silently make the check read a zero (Rule 15).
type Metadata struct {
	Beats          int     `json:"beats"`
	Frames         int     `json:"frames"`
	RawFrames      int     `json:"raw_frames"`
	DistinctFrames int     `json:"distinct_frames"`
	DroppedFrames  int     `json:"dropped_frames"`
	TermCols       int     `json:"term_cols"`
	TermRows       int     `json:"term_rows"`
	WindowPX       []int   `json:"window_px"`
	GIF            string  `json:"gif"`
	GIFDelayCS     int     `json:"gif_delay_cs"`
	PXPerChar      float64 `json:"px_per_char"`
	Method         string  `json:"method"`
	SessionRef     string  `json:"session_ref"`
	Surface        string  `json:"surface"`
	WindowID       string  `json:"window_id"`
}

// ReadMetadata reads out/replay.json. A missing file returns ok=false rather
// than an error: "there is no metadata" is a verdict two of the criteria report
// by name, and an error would make the caller treat an absent file as a fault
// rather than as the thing the check is there to catch.
func (f Film) ReadMetadata() (Metadata, bool, error) {
	path := filepath.Join(f.Out, MetadataName)
	blob, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Metadata{}, false, nil
	}
	if err != nil {
		return Metadata{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var m Metadata
	if err := json.Unmarshal(blob, &m); err != nil {
		return Metadata{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	return m, true, nil
}

// Frames lists the delivered frames in the order they will be displayed, which
// is the file-name order (`f00001.png`, `f00002.png`, …).
func (f Film) Frames() ([]string, error) {
	dir := filepath.Join(f.Out, FramesDirName)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read the delivered frames at %s: %w", dir, err)
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), PNGGlobSuffix) {
			continue
		}
		names = append(names, filepath.Join(dir, e.Name()))
	}
	sort.Strings(names)
	return names, nil
}

// GIFPath resolves the film's animation: the name its own metadata states, else
// the delivered default (see GIFName's comment for why both spellings are legal).
func (f Film) GIFPath() string {
	if m, ok, err := f.ReadMetadata(); err == nil && ok && strings.TrimSpace(m.GIF) != "" {
		return filepath.Join(f.Out, filepath.Base(m.GIF))
	}
	return filepath.Join(f.Out, GIFName)
}

// RedactedRuns reads the masker's log — the runs the pass REPLACED — which is
// the positive control of the withholding criterion (verify.py L1381-L1471: the
// check's second column of input).
func (f Film) RedactedRuns() ([]string, error) {
	path := filepath.Join(f.ScratchOr(), RedactionLogName)
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the redaction log at %s: %w", path, err)
	}
	var runs []string
	for _, line := range strings.Split(string(blob), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, ledgerHeaderPrefix) {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= redactionMatchedCol+1 {
			runs = append(runs, parts[redactionMatchedCol])
		}
	}
	return runs, nil
}

// redactionMatchedCol is the column of the masker's log that holds the matched
// run, and the minimum a row must carry to be a record rather than a header.
const (
	redactionMatchedCol = 2
)

// gridDumpPath is the grid a turn's last beat photographed: `beat-%04d.dump`.
const gridDumpFormat = "beat-%04d.dump"

// GridPath is where the dump for one beat lives.
func (f Film) GridPath(beat int) string {
	return filepath.Join(f.ScratchOr(), DumpsDirName, fmt.Sprintf(gridDumpFormat, beat))
}

// gridDirExists reports whether the run left grid dumps at all — the difference
// between "visibility found nothing" and "visibility could not be measured",
// which a check must not conflate (Rule 15).
func (f Film) gridDirExists() bool {
	st, err := os.Stat(filepath.Join(f.ScratchOr(), DumpsDirName))
	return err == nil && st.IsDir()
}

// identShapesPattern is the identifying-shape vocabulary, kept as a local
// compiled handle so this file's readers do not import regexp twice for one use.
// The PATTERN ITSELF is not here (Rule 14): checks.go takes it from
// internal/replay.IdentShapes, the ONE definition.
var _ = regexp.MustCompile
