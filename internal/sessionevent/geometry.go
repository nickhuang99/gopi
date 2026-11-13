package sessionevent

// geometry.go — the TERMINAL GEOMETRY a session was recorded at, as the ONE
// piece of vocabulary shared by the writer (internal/agent's session_start and
// metadata.json) and every reader (internal/replaydrive's recording header,
// cmd/agent's replay/film geometry resolution).
//
// WHY IT LIVES HERE. The key names and the two state words are written into
// every session JSONL and read back by code in three packages; a second copy of
// a key name in a reader is the defect Rule 1b/13 names — a typo in ONE read
// site silently stops matching the writer, and "the run did not record a
// geometry" becomes indistinguishable from "the reader looked for the wrong
// key" (Rule 15). This package is the leaf both sides can import (zero project
// imports), exactly as it already is for the event names.
//
// WHY AN EXPLICIT STATE, AND NOT 0. A recorded geometry has THREE outcomes, and
// two of them are not numbers:
//
//   - measured — the kernel answered TIOCGWINSZ for one of this process's
//     descriptors, and the terminal really was cols×rows;
//   - unknown — this run had no terminal to measure (stdin/stdout/stderr are all
//     pipes or redirects: the `--once` face, a filmer's producer, a CI run), so
//     there is NOTHING to record.
//
// The bug this shape exists to prevent (Rule 15a): writing `cols: 0, rows: 0`
// for the second case makes "a terminal that reported zero cells" (impossible,
// the kernel answers EINVAL/ENOTTY instead) and "no terminal at all" the same
// value, so a reader cannot tell a measurement from a silence — and a FILM built
// on the zero would be a zero-cell canvas. `state` is therefore the REQUIRED
// field, it is never omitted, and it is decoded before the numbers are believed.

import (
	"encoding/json"
	"fmt"
)

// FieldTerminalGeometry is the session-row / metadata key that carries a
// TerminalGeometry object. It is spelled once: the writer, the reader and the
// tests all reference this constant.
const FieldTerminalGeometry = "terminalGeometry"

// The state words. They are the value of the object's required "state" field.
const (
	// GeometryStateMeasured means cols and rows are a real kernel measurement.
	GeometryStateMeasured = "measured"
	// GeometryStateUnknown means no geometry was measured — the reason field
	// says why, and the numbers are ABSENT rather than zero.
	GeometryStateUnknown = "unknown"
)

// The reason vocabulary for an unknown geometry. Named (Rule 1b) because the
// reader reports it and the tests assert it.
const (
	// GeometryReasonNoTerminal is the reason recorded when none of this
	// process's three standard descriptors answers TIOCGWINSZ.
	GeometryReasonNoTerminal = "no standard descriptor is a terminal"
)

// TerminalGeometry is the recorded measurement of the terminal a session ran
// on.
//
// The JSON shape is `{"state":"measured","cols":117,"rows":26}` or
// `{"state":"unknown","reason":"no standard descriptor is a terminal"}`. cols
// and rows are `omitempty` because in the unknown case they are not a value at
// all — Rule 15a's rule: the field whose absence carries meaning is the one
// that must not be written as a zero. The REQUIRED field is `state`, which is
// never omitted, so the zero-value round trip is covered by a test.
type TerminalGeometry struct {
	State  string `json:"state"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// MeasuredTerminalGeometry records a real measurement. A non-positive dimension
// is not a geometry (Rule 1: the reader divides nothing, but the FILM would
// build a canvas out of it), so such a call answers an UNKNOWN carrying the
// reason instead of a measured zero — the caller's caller cannot write a fake
// measurement through this door.
func MeasuredTerminalGeometry(cols, rows int) TerminalGeometry {
	if cols <= 0 || rows <= 0 {
		return UnknownTerminalGeometry(fmt.Sprintf("the terminal reported %dx%d cells", cols, rows))
	}
	return TerminalGeometry{State: GeometryStateMeasured, Cols: cols, Rows: rows}
}

// UnknownTerminalGeometry records that nothing was measured, with the reason.
func UnknownTerminalGeometry(reason string) TerminalGeometry {
	return TerminalGeometry{State: GeometryStateUnknown, Reason: reason}
}

// Known reports whether this geometry was MEASURED — i.e. whether a caller may
// use Cols×Rows. It is the ONE predicate every reader asks, so "unknown" can
// never be read as "a zero-cell terminal" in one place and not another.
func (g TerminalGeometry) Known() bool {
	return g.State == GeometryStateMeasured && g.Cols > 0 && g.Rows > 0
}

// String renders the geometry for a diagnostic line: the size when it was
// measured, and the state (with its reason) when it was not. It exists so no
// caller composes that sentence out of a bare number, where 0 would print as
// "0x0" and read as a size.
func (g TerminalGeometry) String() string {
	if g.Known() {
		return fmt.Sprintf("%dx%d", g.Cols, g.Rows)
	}
	if g.Reason != "" {
		return GeometryStateUnknown + " (" + g.Reason + ")"
	}
	return GeometryStateUnknown
}

// DecodeTerminalGeometry reads the geometry out of a session row's field.
//
// It returns an error — never a silent unknown — when the field is present but
// does not carry a state this package defines: "the recording says something I
// do not understand" and "the recording says nothing" are different facts about
// a recording, and a reader that collapsed them would report a fallback the
// recording never asked for (Rule 15). A nil/absent field is reported by
// ok=false with no error: the caller decides what silence means.
func DecodeTerminalGeometry(raw json.RawMessage) (g TerminalGeometry, ok bool, err error) {
	if len(raw) == 0 {
		return TerminalGeometry{}, false, nil
	}
	var decoded TerminalGeometry
	if uerr := json.Unmarshal(raw, &decoded); uerr != nil {
		return TerminalGeometry{}, false, fmt.Errorf("%s: %w", FieldTerminalGeometry, uerr)
	}
	switch decoded.State {
	case GeometryStateMeasured:
		if decoded.Cols <= 0 || decoded.Rows <= 0 {
			return TerminalGeometry{}, false, fmt.Errorf(
				"%s: state %q with cols=%d rows=%d", FieldTerminalGeometry, decoded.State, decoded.Cols, decoded.Rows)
		}
		return decoded, true, nil
	case GeometryStateUnknown:
		return decoded, true, nil
	case "":
		return TerminalGeometry{}, false, fmt.Errorf("%s carries no %q field", FieldTerminalGeometry, "state")
	default:
		return TerminalGeometry{}, false, fmt.Errorf("%s carries an unknown state %q", FieldTerminalGeometry, decoded.State)
	}
}
