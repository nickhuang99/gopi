package replaydrive

// geometry.go — the RECORDING's terminal geometry, read from the session JSONL
// header WITHOUT loading the recording.
//
// WHY A SEPARATE, CHEAP READER. The film driver decides which canvas to
// reproduce BEFORE it starts a display server or spawns the producer that will
// call LoadRecording, so it needs the one number pair out of the file's first
// rows and nothing else (Rule 35: the producer's whole parse is not a cost the
// driver should pay to ask one question).
//
// WHAT "NO GEOMETRY" MEANS HERE. Every session written before the field existed
// has none, and so does a run with no terminal: both answer ok=false, and the
// caller states its own fallback OUT LOUD (Rule 15) rather than being handed a
// zero it might use as a size. A field that IS present but undecodable is an
// ERROR, never a silent absence — the difference between "the recording is
// older than the field" and "the recording carries something I cannot read" is
// exactly what a fallback notice has to be honest about.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/nick/gopi/internal/sessionevent"
)

// geometryHeaderScanRows bounds how many leading JSONL rows this reader will
// look at. The session_start row is the FIRST line a session writes (the
// writer syncs it immediately), so the bound is generous by two orders of
// magnitude; it exists only so a file whose header is damaged cannot turn a
// header question into a full-file scan.
const geometryHeaderScanRows = 64

// LoadRecordedGeometry returns the terminal geometry the session JSONL at path
// was recorded at.
//
// ok=false means the recording carries no geometry field at all (a session
// recorded before the field existed, or one with no terminal) — the caller
// supplies its own fallback and says so. A malformed field is an error.
func LoadRecordedGeometry(path string) (geo sessionevent.TerminalGeometry, ok bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return sessionevent.TerminalGeometry{}, false, fmt.Errorf("read the recording's geometry %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, geometryScannerInitBuf), geometryScannerMaxLine)
	for row := 0; row < geometryHeaderScanRows && scanner.Scan(); row++ {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var fields map[string]json.RawMessage
		if uerr := json.Unmarshal(line, &fields); uerr != nil {
			// A row this reader cannot parse is not this reader's verdict to
			// make: the recording is read as a whole by LoadRecording, whose own
			// error names the line. Skipping keeps the geometry question about
			// the geometry (never a second, weaker parser of the same file).
			continue
		}
		if !isSessionStartRow(fields) {
			continue
		}
		decoded, found, derr := sessionevent.DecodeTerminalGeometry(fields[sessionevent.FieldTerminalGeometry])
		if derr != nil {
			return sessionevent.TerminalGeometry{}, false, fmt.Errorf("%s: %w", path, derr)
		}
		if !found {
			return sessionevent.TerminalGeometry{}, false, nil
		}
		return decoded, true, nil
	}
	if serr := scanner.Err(); serr != nil {
		return sessionevent.TerminalGeometry{}, false, fmt.Errorf("read the recording's geometry %s: %w", path, serr)
	}
	return sessionevent.TerminalGeometry{}, false, nil
}

// isSessionStartRow reports whether a decoded row is the session_start event.
// It reads the row's own "type" field through the shared constant, so a rename
// on the writer's side cannot leave this reader matching nothing.
func isSessionStartRow(fields map[string]json.RawMessage) bool {
	raw, ok := fields[fieldRowType]
	if !ok {
		return false
	}
	var kind string
	if json.Unmarshal(raw, &kind) != nil {
		return false
	}
	return kind == sessionevent.EventTypeSessionStart
}

const (
	// geometryScannerInitBuf is the scanner's initial buffer: a session_start
	// row is well under a kilobyte, so this avoids a reallocation for the common
	// case without claiming anything about the file.
	geometryScannerInitBuf = 4096
	// geometryScannerMaxLine bounds one JSONL line this reader will hold. A row
	// longer than this is not the header this function is looking for; the
	// scanner reports it and the error is returned (never a silent skip: a file
	// whose first line is 8 MiB is a fact its reader should hear).
	geometryScannerMaxLine = 8 << 20
)
