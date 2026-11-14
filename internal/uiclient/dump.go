package uiclient

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// dump.go — the LIVE dump sink for the `gopi ui` surface.
//
// THE PROBLEM THIS SOLVES. `internal/lineedit.Screen` can already interpret a
// terminal byte stream into a cell grid, but a grid nobody feeds is a grid that
// says nothing about a RUNNING surface. An agent next to a live `gopi ui` had
// exactly one observation route: grab the desktop's pixels and ask a vision
// model what it sees. That route is expensive (a 1920×1080 PNG is ~400 KB), it
// is imprecise (a model describes "a left dock, a main area" — never "the
// status row is row 29 and says idle"), and it is INDISCRIMINATE: it captures
// every other window on the display as a side effect.
//
// The surface is text. So the self-observation is text: the render path tees
// every byte it writes into the SAME interpreter, keeps the resulting grid, and
// writes it to a file the agent reads. Zero pixels, zero vision, zero
// cross-process access — the dump is produced by the surface itself, about
// itself.
//
// WHERE IT HANGS. ScreenDumper is an io.Writer placed BESIDE ScreenWriter on
// the one stream bubbletea paints (`Run`: cfg.Out / cfg.Screen). It interprets
// what bubbletea actually WROTE, so the dump describes the surface as rendered
// — not a model re-derived from the view function, which could disagree with
// what was painted.
//
// COST CONTROL. Dumping on every Write would put a full-grid snapshot plus two
// file renames on the render path, once per frame. So a write only REFRESHES
// the in-memory dump when at least dumpMinInterval has passed since the last
// one, and the file is written only when the dump actually CHANGED. The state a
// reader finds is therefore always the last frame older than the interval: for
// an agent polling a file, indistinguishable from a per-frame dump, at a
// fraction of the cost.

// DumpMode names how much of the render stream the sink interprets.
type DumpMode string

const (
	// DumpModeANSI interprets the ANSI byte stream. It is the DEFAULT: what
	// bubbletea writes is ANSI whatever terminal it is speaking to, and a dumb
	// terminal is a terminal whose escape sequences are dropped by ITS renderer,
	// not by the producer.
	DumpModeANSI DumpMode = dumpModeANSI
	// DumpModePlain treats the bytes as literal text: no escape sequence is
	// interpreted, so a row is exactly the characters between newlines. It is
	// for a pipe/captured stream where the reader wants the raw lines, and it
	// is honest about what it gives up (cursor and attributes are meaningless).
	DumpModePlain DumpMode = "plain"
	// dumpModeANSI / dumpModePlain are the two accepted mode spellings. They
	// are the VALUES of the --dump-mode flag, so they live here once and the
	// CLI compares against these constants rather than retyping them (Rule 1b).
	dumpModeANSI  = "ansi"
	dumpModePlain = "plain"
)

// dumpMinInterval is how stale a dump may be while the renderer keeps painting.
// It is the sink's own bound: the render path pays for a snapshot at most this
// often, and a reader of the file sees a frame no more than this old.
const dumpMinInterval = 100 * time.Millisecond

// dumpTailFlushGrace is the extra margin the window-closing timer carries over
// dumpMinInterval. The timer's deadline is measured from the instant a window
// was OPENED, while `now` is read from a wall clock that can step; the margin
// exists so a timer that fires a hair early cannot find the window "not yet due"
// and leave it open again. It only lengthens a bound the reader already tolerates
// (dumpMinInterval), so a generous value costs staleness the artifact already
// declares and never costs a missing frame.
const dumpTailFlushGrace = 5 * time.Millisecond

// File modes and names for the dump's two artifacts.
const (
	dumpFilePerm = 0o644
	dumpTmpPerm  = 0o600
	// dumpJSONSuffix is appended to the text dump's path for the
	// machine-checkable twin, so one flag produces both shapes side by side.
	dumpJSONSuffix = ".json"
	// DumpRecordPrefix heads the provenance line above the dump body. It is
	// distinct from the dump's own "#gopi-screen-dump" header, so a reader can
	// tell the OBSERVER's record from the OBSERVED grid.
	//
	// It is EXPORTED because it is the default SURFACE MARKER (SurfaceUI): the
	// reader identifies a dump's writer by that line, and a second spelling of
	// it (a reader's, a test's) is exactly the drift Rule 13 closes.
	DumpRecordPrefix = "#gopi-ui-dump"
	// ChatDumpRecordPrefix heads the provenance line of a `gopi chat` dump —
	// the SAME record shape, marked with the surface that wrote it. One record
	// vocabulary with a surface marker, never a second record format: the
	// chat and the ui sink both go through THIS package's ScreenDumper, so the
	// line's fields (pid, target, geometry, mode, frames, dumps, started) mean
	// the same thing in both files.
	ChatDumpRecordPrefix = "#gopi-chat-dump"
)

// Surface names WHICH of gopi's text surfaces wrote a dump. `gopi ui`
// (BubbleTea) and `gopi chat` (the line editor) are both self-observing now,
// and their artifacts must be distinguishable in three places: the record
// prefix a reader parses, the file NAME the scanner walks for, and the text of
// a refusal that has to explain what the operator is looking at.
//
// It is a string type rather than an int because the spelling IS the artifact's
// contract: the value appears in the dump's first line, so it is greppable,
// stable and quotable.
type Surface string

const (
	// SurfaceUI is the default: a `gopi ui` surface, and the marker the
	// historic dump carried before this discriminator existed.
	SurfaceUI Surface = "ui"
	// SurfaceChat is `gopi chat`: the in-process line editor with the reserved
	// chat layout (answer band + editing line + status row).
	SurfaceChat Surface = "chat"
)

// The two artifacts' file names, spelled ONCE here and referenced by every
// writer, the discovery scan and the tests (Rule 13). They live beside the
// Surface discriminator because the NAME is half of what identifies an
// artifact: a reader that scanned for a name no writer spells would see
// nothing, and a second literal at either end is the drift Rule 13 forbids.
const (
	// UIDumpFile is `gopi ui --dump-screen`'s artifact. The exported
	// DumpFileName (dumpread.go) carries this same value.
	UIDumpFile = "ui-screen.dump"
	// ChatDumpFile is `gopi chat --dump-screen`'s artifact: the agent's OWN
	// surface, named distinctly so one directory can hold both dumps without
	// either masquerading as the other.
	ChatDumpFile = "chat-screen.dump"
)

// DumpFileNames maps each surface to the file name its dump lands under. It is
// the ONE enumeration of the artifacts (the discovery scan iterates it), so a
// third surface cannot be added to the discriminator while every reader keeps
// scanning for two names.
func DumpFileNames() map[Surface]string {
	return map[Surface]string{
		SurfaceUI:   UIDumpFile,
		SurfaceChat: ChatDumpFile,
	}
}

// CutRecordPrefix splits a dump's first line into the surface that wrote it and
// the record body. BOTH prefixes are accepted — one record vocabulary, two
// surface markers — so a reader cannot mistake a chat dump for a malformed ui
// dump (or the reverse) merely because it was written by the other surface.
//
// ok is false for a line that carries neither marker, and the caller must then
// REFUSE the file: a first line that is not a provenance record cannot be
// attributed, and a guess would attribute a screen to a process that never
// wrote it.
func CutRecordPrefix(line string) (Surface, string, bool) {
	for _, s := range Surfaces() {
		prefix := RecordPrefixFor(s) + dumpRecordFieldSep
		if strings.HasPrefix(line, prefix) {
			return s, strings.TrimPrefix(line, prefix), true
		}
	}
	return "", "", false
}

// SurfaceForDumpFile names the surface a dump FILE belongs to, by its name.
// ok is false for a file no surface owns, so a caller can refuse rather than
// guess (an unnamed dump must never be silently attributed).
func SurfaceForDumpFile(name string) (Surface, bool) {
	for _, s := range Surfaces() {
		if DumpFileNames()[s] == name {
			return s, true
		}
	}
	return "", false
}

// RecordPrefixFor returns the provenance prefix the given surface writes, and
// ParseDumpRecordsFor is its reader-side counterpart.
func RecordPrefixFor(s Surface) string {
	if s == SurfaceChat {
		return ChatDumpRecordPrefix
	}
	return DumpRecordPrefix
}

// Surfaces returns every known surface. It exists so a caller that must
// enumerate them (the discovery scan, a report that lists what it could see)
// reads the ONE list rather than a copy that a third surface would miss.
func Surfaces() []Surface { return []Surface{SurfaceUI, SurfaceChat} }

// ScreenDumper is the surface's self-observation sink: an io.Writer that feeds
// the render stream into a lineedit.Screen, keeps the latest dump, and mirrors
// it into a file an agent can retrieve.
//
// It is safe for concurrent use: bubbletea writes from its render loop, while a
// poller may read the snapshot. A method on it must never be called without the
// lock held internally.
type ScreenDumper struct {
	// path is the text dump's destination. "" makes the sink memory-only — a
	// legal configuration for a caller that wants the snapshot without a file.
	path string
	// mode is how the stream is interpreted (DumpModeANSI / DumpModePlain).
	mode DumpMode
	// identity is the provenance the record carries: what surface this is, in
	// which process, against which target — the facts that answer "WHICH
	// `gopi ui` am I reading?" when two of them are running.
	identity DumpIdentity

	mu      sync.Mutex
	screen  *lineedit.Screen
	last    time.Time
	lastSig string // signature of the last WRITTEN dump: unchanged = no write
	dump    lineedit.ScreenDump
	written int
	// frames counts Write calls seen — the render stream's own volume, which
	// the record reports so a reader can date its dump against the traffic.
	frames int
	// err is the last dump-writing failure, carried into the record instead of
	// being dropped (Write cannot return it to the render loop — see Write).
	err string
	// closed freezes the sink (see Close). A frozen sink keeps the frame it was
	// closed on: the bytes a surface writes while it TEARS ITS LAYOUT DOWN
	// (erase, scroll-region reset, cursor show) describe the exit, not the
	// screen, and a reader asserting on layout wants the last painted screen.
	closed bool
	// now is the clock, injectable so a test drives the refresh interval
	// without sleeping.
	now func() time.Time
	// tail is the ARMED window closure (armTailFlush): a one-shot timer that
	// installs the frame a window ended on, so the artifact does not depend on
	// the render stream producing another byte. nil means no window is open.
	tail *time.Timer
	// dirty records that the grid has received bytes since the last closure, so
	// the closure can tell "this window still carried paint" from "the surface
	// went idle": the first re-arms (the frame is not final yet), the second does
	// not — which is what keeps a long idle run from writing files forever.
	dirty bool
	// geometry is the AUTHORITY behind the grid's current size (GeometrySource):
	// the layout that drew into it, the terminal that was measured, or the
	// model default a failed measurement left behind. It is state, not a
	// constant, because the layout learns its geometry only AFTER the region is
	// installed — SetGeometry moves the grid there.
	geometry GeometrySource
	// capability is the colour-capability decision this surface drew with, in the
	// record's own fields (SetCapabilityReport). A POINTER, so "nobody reported"
	// and "reported an empty decision" cannot be confused: nil records nothing,
	// and the record line keeps the shape a pre-capability reader parses.
	capability *CapabilityReport
	// onGeom is the loud-fallback hook (see DumpConfig.OnGeometryChanged). It is
	// called OUTSIDE the lock: a caller that prints from it must not be able to
	// deadlock the render path.
	onGeom func()
	// sgr is the RAW append leg (DumpConfig.Sgr). nil is the off state, and the
	// off state must be a branch the render path can SKIP, never a writer that
	// discards: measured-by-construction zero writes when unarmed (its own test
	// asserts the artifact does not exist at all after a full run).
	sgr *SgrStream
}

// DumpIdentity is what the record says about WHOSE screen this is. Without it a
// dump is an anonymous grid, and an agent holding two of them cannot tell the
// operator's `gopi ui --tools` from its own session's surface.
type DumpIdentity struct {
	// Surface names which of gopi's text surfaces wrote this dump. It is what
	// makes the artifact attributable when several are on hand, and it selects
	// the record's own prefix (RecordPrefixFor) — so a reader can tell a chat
	// dump from a ui dump by the file's first line alone.
	Surface Surface `json:"surface"`
	// PID is the process that owns the surface.
	PID int `json:"pid"`
	// Target is the server origin the surface is attached to (the header's
	// own "gopi ui → <target>").
	Target string `json:"target"`
	// Tools mirrors the surface's tool switch.
	Tools bool `json:"tools"`
	// ColsRows is the geometry the MODEL was told to lay out for.
	ColsRows [2]int `json:"model_geometry"`
	// TerminalColsRows is the geometry read from the tty itself; it may differ
	// from the model's when the model was built before a resize.
	//
	// BOTH fields follow the sink: SetGeometry stamps them with the geometry the
	// layout installed, so after a resize `terminal=`, `model=` and `grid=` are
	// the same number. A difference between them is a real statement about a
	// surface whose model lags its terminal — never an artifact of the record
	// being frozen at construction (measured 2026-11-09: they were).
	TerminalColsRows [2]int `json:"terminal_geometry"`
	// Mode is the interpretation mode in force.
	Mode DumpMode `json:"mode"`
	// Started is when the surface started, so a reader can date the dump.
	Started time.Time `json:"started"`
}

// GeometrySource names WHERE the grid's geometry came from. It is the field
// that stops a reader from mistaking a mis-scaled grid for a faithful one: a
// dump painted at 117 columns and re-interpreted at 80 fabricates wraps and
// truncations that look exactly like real corruption, so the artifact must SAY
// which geometry it was built at and on whose authority (Rule 15 — a fallback
// that is not recorded is a silent failure).
//
// It is a string type for the same reason Surface is: the spelling IS the
// contract, it appears in the record's first line, and it is greppable.
type GeometrySource string

const (
	// GeometrySourceLayout is the AUTHORITATIVE source: the geometry the
	// surface's own layout just installed (lineedit's reserved region for
	// `gopi chat`, the run configuration for `gopi ui`). A grid built at this
	// geometry cannot disagree with the painted frame, because both numbers are
	// the same number.
	GeometrySourceLayout GeometrySource = "layout"
	// GeometrySourceTerminal is a geometry read from the output descriptor's
	// own TIOCGWINSZ — measured, but measured at ARM time: a resize after this
	// instant leaves the grid behind the layout, which is why the layout source
	// outranks it.
	GeometrySourceTerminal GeometrySource = "terminal"
	// GeometrySourceFallback is the LOUD one: nothing measured the terminal, so
	// the grid is the model's 80x24 default. Every artifact carrying it is
	// qualified in words as well (see GeometryFallbackNotice): a reader must
	// re-interpret nothing and trust nothing at this geometry.
	GeometrySourceFallback GeometrySource = "fallback"
)

// GeometryFallbackNotice is printed — by the sink's own caller and echoed in
// the record — whenever a FALLBACK geometry is in force. It names the
// consequence (a grid that may not be the screen's) so the operator never has
// to know which geometry the sink was built at to read the artifact correctly.
const GeometryFallbackNotice = "the dump's grid geometry is a FALLBACK (no terminal size was measured): the grid is that default, NOT the layout's — do not read wraps or truncation off it as if they were the screen's"

// DumpConfig configures a ScreenDumper. It is a struct rather than a parameter
// list because the fields are not positional in meaning (cols before rows, or
// the reverse, is not something a caller should have to remember).
type DumpConfig struct {
	// Path is the text dump's file. Empty = no file, snapshot only.
	Path string
	// Mode selects the interpretation; empty resolves to DumpModeANSI.
	Mode DumpMode
	// Cols / Rows are the grid's geometry. A non-positive value falls back to
	// the model's default so a caller that could not measure its terminal still
	// gets a usable grid rather than a 1×1 one.
	Cols int
	Rows int
	// Identity is copied into every record.
	Identity DumpIdentity
	// Geometry names the AUTHORITY behind Cols/Rows. Empty means the caller
	// stated a geometry it measured itself (GeometrySourceTerminal, or
	// GeometrySourceConfigured for a config-derived one) — and a caller that
	// stated nothing positive (Cols or Rows <= 0, so the model default was
	// substituted) is recorded as GeometrySourceFallback NO MATTER what was
	// passed here. The fallback fact is the sink's to state, never the
	// caller's to suppress (Rule 15).
	Geometry GeometrySource
	// OnGeometryChanged, when set, is called ONCE — after the reserved layout
	// has been installed — if and only if SetGeometry moves the grid to a
	// DIFFERENT geometry. The chat surface uses it to print the fallback
	// notice where the operator can still read it (after the banner, before
	// the first paint); a sink without it records the fact in the artifact
	// only.
	OnGeometryChanged func()
	// Sgr, when non-nil, is the RAW append leg (sgrstream.go): every byte this
	// sink is handed is appended verbatim — unthrottled, no frame dropped, no
	// interpretation — as a length-prefixed frame. nil is the OFF state and
	// costs the render path NOTHING: Write checks the field and stops (the
	// switch's default is nil, and the CLI only allocates a stream when the
	// operator asked for one).
	//
	// The leg is deliberately NOT reachable from the interpreted path: it never
	// reads the grid, so a reader of its bytes is not testing this package's
	// interpreter, and a defect in the interpreter cannot corrupt the record of
	// what was actually written.
	Sgr *SgrStream
}

// defaultDumpCols / defaultDumpRows are the grid's fallback geometry when the
// caller knows nothing about its terminal: the same 80×24 default bubbletea's
// own renderer assumes.
const (
	defaultDumpCols = 80
	defaultDumpRows = 24
)

// NewScreenDumper builds the sink and takes its first (empty) snapshot, so a
// file asked for now exists immediately — an agent that polls for the dump can
// distinguish "the surface has not painted yet" from "there is no dump".
func NewScreenDumper(cfg DumpConfig) (*ScreenDumper, error) {
	cols, rows := cfg.Cols, cfg.Rows
	geom := cfg.Geometry
	if geom == "" {
		// The caller stated a geometry of its own (no authority word needed):
		// it is measured or configured, not the default.
		geom = GeometrySourceTerminal
	}
	if cols <= 0 {
		cols = defaultDumpCols
		geom = GeometrySourceFallback
	}
	if rows <= 0 {
		rows = defaultDumpRows
		geom = GeometrySourceFallback
	}
	mode := cfg.Mode
	if mode == "" {
		mode = DumpModeANSI
	}
	if mode != DumpModeANSI && mode != DumpModePlain {
		return nil, fmt.Errorf("uiclient: unknown dump mode %q", mode)
	}
	id := cfg.Identity
	id.Mode = mode
	if id.Surface == "" {
		// The default is the surface this capability was born on. An EXPLICIT
		// unknown value is refused below rather than defaulted: an empty string
		// is "the caller did not say", a wrong word is a caller who believes
		// something false about where the dump goes.
		id.Surface = SurfaceUI
	}
	if _, known := DumpFileNames()[id.Surface]; !known {
		// An unnamed surface would write a dump NO reader can attribute: the
		// record's marker and the file name are both derived from it, so this
		// is refused at construction rather than rendered as "ui" (Rule 15 —
		// a default that silently claims a surface is a lie about the writer).
		return nil, fmt.Errorf("uiclient: unknown dump surface %q", id.Surface)
	}
	if id.PID == 0 {
		id.PID = os.Getpid()
	}
	if id.Started.IsZero() {
		id.Started = time.Now()
	}
	d := &ScreenDumper{
		path:     cfg.Path,
		mode:     mode,
		identity: id,
		geometry: geom,
		onGeom:   cfg.OnGeometryChanged,
		sgr:      cfg.Sgr,
		screen:   lineedit.NewScreen(cols, rows),
		now:      time.Now,
	}
	d.mu.Lock()
	d.dump = d.screen.Dump()
	d.mu.Unlock()
	if d.path != "" {
		// The first write is unconditional: the file must exist even for a
		// surface that has not painted a frame yet.
		if err := d.flush(true); err != nil {
			return nil, err
		}
	}
	// WHY THE FALLBACK IS NOT ANNOUNCED HERE. A sink constructed with no measured
	// geometry holds the fallback, but that state is usually TRANSIENT: the
	// layout installs a moment later and SetGeometry moves the grid to the real
	// number (measured 2026-09-26: a run at a 117-column pty announced the
	// fallback and then closed out at grid=117x26 geometry=layout — a notice
	// that was true for a millisecond and wrong for the whole run). The loud
	// half therefore rides CHANGES (SetGeometry) and the CLOSE-OUT report, which
	// states the geometry the artifact was frozen at (cmd/agent's chat/ui dump
	// reports). Rule 15 is served by those two: nothing reaches a reader without
	// the artifact saying which geometry it was built at.
	return d, nil
}

// Write implements io.Writer: the render stream arrives here beside the real
// terminal. It always reports the FULL length so the writer above never sees a
// short write — the dump is an observer, and an observer must not be able to
// break the thing it observes (Rule 15: an error it cannot return to the
// renderer is recorded instead, on the record's own line).
func (d *ScreenDumper) Write(p []byte) (int, error) {
	if d == nil || len(p) == 0 {
		return len(p), nil
	}
	d.mu.Lock()
	if d.closed {
		// Frozen (Close ran): the frame is the deliverable, and the teardown
		// bytes are not part of it. Reporting the FULL length keeps the
		// observer invisible to the render path (Write's contract).
		d.mu.Unlock()
		return len(p), nil
	}
	d.frames++
	// THE RAW APPEND LEG runs here, FIRST — before the interpretation below —
	// so the artifact is the bytes the renderer handed over, never a
	// reconstruction of them. It is a nil check on the off path (one branch,
	// no allocation, no syscall): an unarmed run's Write is the one it always
	// was.
	//
	// It sits INSIDE the lock (the frames must appear in the order the
	// interpreter saw them, or a replay of this file would describe a screen
	// that never existed), so its error is carried out and recorded AFTER the
	// release: recordError takes the same lock (Rule 15 — an armed leg whose
	// write failed must not read as a healthy record).
	var sgrErr error
	if d.sgr != nil {
		sgrErr = d.sgr.Write(p)
	}
	// This write changed the grid: a window that is open (or that this write is
	// about to open) now has paint in it to install, so a closure is owed.
	d.dirty = true
	switch d.mode {
	case DumpModePlain:
		// Literal text: no escape interpretation at all. LF moves to the next
		// row and CR returns to column 0 — the two control characters a pipe
		// actually carries.
		for _, b := range p {
			switch b {
			case '\n':
				d.screen.Feed([]byte("\r\n"))
			case '\r':
				d.screen.Feed([]byte("\r"))
			default:
				d.screen.Feed([]byte{b})
			}
		}
	default:
		d.screen.Feed(p)
	}
	due := d.now().Sub(d.last) >= dumpMinInterval
	d.mu.Unlock()

	if sgrErr != nil {
		// The RAW leg's failure is the ONLY one this branch can report, and it
		// is reported even when the grid's own write is not due: a stream that
		// silently stopped recording is worse than no stream at all.
		d.recordError(sgrErr)
	}

	if !due {
		// Inside an OPEN window: this frame still reaches the GRID (the feed
		// above ran), but the artifact waits for the window's own closure
		// (armTailFlush) rather than for a future write of its own — so a
		// window opened by a FLUSH (SetGeometry's, whose install restamps the
		// interval mid-burst) is closed by the paint that follows it, exactly
		// like one opened by a write.
		d.armTailFlush()
		return len(p), nil
	}
	// The serialization point is the dump write, not the feed: a frame that
	// arrives inside the interval still reaches the grid. The window this write
	// may have opened is closed by flush itself (its ONE arming site).
	if err := d.flush(false); err != nil {
		d.recordError(err)
	}
	return len(p), nil
}

// armTailFlush closes the throttle window that a due write just opened, on the
// SINK's own clock rather than on the render stream's next byte.
//
// THE DEFECT THIS CLOSES (measured 2026-10-02, task 01a0fd34 — the chat REPL's
// `/info`, a REPL-native slash line answered in-process with no model turn).
// A window used to be closed by the NEXT due write and by nothing else, so a
// burst's tail was written only if the stream kept going. Measured on a real
// pty, from this sink's own trace: the paint that OPENS the window flushes at
// t0 with the answer block NOT yet in it (the block's bytes arrive 1–35 ms
// later, all of them inside the just-opened window, every one of them `due=false`),
// and after the line is answered the REPL parks in ReadLine in raw mode — no
// more paints, no more writes. Measured: `frames=89 dumps=7`, artifact
// byte-identical over 50 samples / 10 s, grid row 036/037 still the previous
// lines while the pty stream carried all 14 `session info:` rows. ONE later
// keystroke closed the window and installed the block (`frames=140 dumps=8`),
// which is what named the mechanism: not a lost write, a window that never closed.
//
// WHY A TIMER HERE, when Settle's comment above argues against one. That
// argument is about the TURN boundary, where the producer KNOWS the screen is
// final and a timer would have to guess — and where an unconditionally armed
// timer would write files on an idle surface forever. This timer is neither:
// it is armed ONLY by a real paint (so an idle surface never arms it), it is
// one-shot (the callback clears the armed flag and does not re-arm itself), and
// it fires at most once per window — the sink's own staleness bound
// (dumpMinInterval), which the record's `recorded=` field already claims. The
// producer's "this frame is final" knowledge is still Settle's job; this closes
// the window the throttle left open, which no producer can know about because
// the throttle is this sink's private bookkeeping.
//
// EVERY ROAD THAT OPENS A WINDOW ARRIVES HERE, and that is what makes the bound
// real rather than a property of one caller (measured 2026-10-08, task
// 01a119e6 — the second sighting of this exact defect, from a road this function
// used to miss). There are TWO ways a window opens: a render WRITE that finds the
// interval elapsed (Write's `due` branch), and any call to `flush` — which
// restamps `last` whatever opened it, so SetGeometry's forced flush after the
// layout installs opens one too. When only Write armed the timer, a window opened
// by a flush was closed by nothing: the paint that followed was fed to the grid
// (`due=false`, correctly) and then waited forever for a due write that never
// came, because this sink's next write of any size arrives inside the same
// interval — on a live chat that is the whole startup burst. Measured at 100x30
// with no keystroke: `frames=72 dumps=2` and an ALL-BLANK grid with
// `cursor row=0 col=0`, frozen on the frame that preceded the paint; the in-memory
// grid held all 22 painted rows and the cursor at row 28 col 5 the entire time, so
// the artifact described a screen that had not existed for 60 ms. Arming here —
// the ONE place that knows a window just opened — is the fix, and it makes the
// rule unconditional: every opened window has a closure.
func (d *ScreenDumper) armTailFlush() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.armTailFlushLocked()
}

// armTailFlushLocked is the arming itself, for the caller that ALREADY holds the
// sink's lock: flush restamps `last` while holding it, so the arming has to
// happen there (taking the lock again would deadlock the render path).
//
// An arm that is already pending is left alone — ONE closure per window — and
// the tail closure's own flush does NOT come back through here (tailFlush calls
// flushUnarmed), so a window that closes cannot open the next one: an idle
// surface arms once and then never again.
func (d *ScreenDumper) armTailFlushLocked() {
	// No file to write, already armed (one closure per window), nothing new in
	// the grid to install (an idle surface must not keep a timer alive), or
	// frozen (Close's frame is the deliverable — a tail flush must not re-open
	// it).
	if d.path == "" || d.closed || d.tail != nil || !d.dirty {
		return
	}
	// The grace margin is what keeps a timer that fires a hair early from
	// finding the window "not yet due" — see dumpTailFlushGrace.
	d.tail = time.AfterFunc(dumpMinInterval+dumpTailFlushGrace, func() { d.tailFlush() })
}

// tailFlush is the window's closure. It is the SAME flush the render path
// takes — including the unchanged-body suppression — so a window whose frames
// changed nothing writes nothing, and a window that carried the burst's tail
// installs exactly the frame the stream had reached.
func (d *ScreenDumper) tailFlush() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.tail = nil
	// The window is closed: the paint it carried is about to be installed, so
	// the flag starts clean. A write that arrives after this instant sets it
	// again and arms the NEXT closure — which is how a burst longer than one
	// interval converges on its final frame instead of stopping one window
	// short of it.
	d.dirty = false
	// THE INSTALL HAPPENS UNDER THE SAME LOCK the render path holds. It did not
	// before, and that is the defect this branch announces: flushUnarmed reads
	// and writes the SINK's own fields (dump, last, lastSig, written) and reads
	// d.screen — the grid a concurrent Write is feeding under d.mu. Dropping the
	// lock around the call made this timer goroutine a SECOND writer of the
	// grid, so `go test ./cmd/agent -race` reported the pair by name (measured
	// 2026-10-27, task 01a1756e): Read/Write at the same address by
	// ScreenDumper.flushUnarmed (this goroutine, via tailFlush) and by
	// ScreenDumper.Write / flushUnarmed (the render path), plus every grid-level
	// frame under them — Screen.setScrollRegion vs Screen.ScrollRegion,
	// Screen.controlSequence vs Screen.Dump, Screen.put vs Screen.attrRuns.
	// flush (867) already holds d.mu across the very same call, so this is the
	// one shape: the sink's state is mutated only while its lock is held.
	err := d.flushUnarmed(false)
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return
	}
	if err != nil {
		// The timer goroutine has no caller to return to, so the failure rides
		// the record's own error field (Rule 15) — the same road Write takes.
		// recordError takes the lock itself, so it runs AFTER the release.
		d.recordError(err)
	}
}

// Settle records that the surface's paint stream has gone QUIET at a frame a
// caller judges COMPLETE, and installs that frame as the artifact's state even
// when it is byte-identical to the one already on disk.
//
// THE DEFECT THIS CLOSES (measured 2026-09-27, task 01a0e433 — the handoff doc's
// own re-verification method could not be executed). The artifact used to be
// written on ONE trigger only: a paint write that both (a) landed outside
// dumpMinInterval and (b) produced a rendering different from the last one
// written (`flush`'s body comparison). A chat REPL paints its answer from the
// turn's goroutine and then STOPS — the next paint is the next turn, i.e. the
// next keystroke — so the last written frame was whichever one the THROTTLE
// happened to catch mid-turn. Measured: `frames=35 dumps=28 ... iter 0 - ready`,
// the answer absent, byte-identical for 10s over 40 samples. An agent observing
// an armed run (no keyboard input, therefore no further paint at all) could not
// read the screen it had just watched draw, and the only workaround was to TYPE
// — which repaints the editing line and can be caught mid-paint, hence the
// literal escape-sequence cells the same report measured (`...1449-150m`,
// `...1451-1524;104H`) in the artifact.
//
// THOSE LITERAL CELLS ARE NOT A STALE FRAME'S SIGNATURE (established 2026-09-28,
// task 01a0e81b). The attribution above was a plausible story, not a measurement:
// the frame HAD been captured mid-paint, so the sequence bytes were assumed to be
// visible simply because they were recent. They are not — the interpreter reads
// every escape of every frame it is fed, whoever owned the cursor. What actually
// put parameters on the grid is the carry policy in internal/lineedit/screen.go
// (holdPending): a held sequence past its bound was truncated to its LAST bytes,
// which drops the introducer and re-offers the parameters as printable
// characters. Measured on the artifact of session 884eca28, whose grid row 002
// carries `;61H24;73H5;230m` — three placed sequences, every `ESC[` gone — and
// reproduced deterministically without a pty by
// internal/lineedit/screen_introducer_lost_test.go. A frame boundary is still
// where the defect is REACHED (a read that splits a sequence), which is why the
// settle path and this sink's one-Write-per-read feeding are how it surfaces.
// The fix and its regression test are in screen.go and the two
// `*_introducer_lost_test.go` files; this comment is corrected so the next reader
// does not re-derive the wrong cause from it.
//
// WHY A CALL AND NOT A TIMER. A settle is the moment the PRODUCER knows the
// screen is final for this turn: the turn's tail is committed and its bytes are
// already in this sink (the renderer writes synchronously on the turn's
// goroutine). A timer would have to guess that moment, and would write files on
// an idle surface forever; the caller already knows.
//
// It is idempotent, it takes the same lock the paint stream takes (so a settle
// can never be sliced by a concurrent write), and a CLOSED sink stays closed —
// the frozen frame is the deliverable and a settle must not re-open it (Close's
// contract). Its error is RETURNED rather than recorded: unlike Write, a settle
// is called from a path that can report it (Rule 15).
func (d *ScreenDumper) Settle() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return nil
	}
	// force=true: the caller has judged this frame the screen to keep, so the
	// unchanged-body suppression does not apply — the artifact states WHICH
	// frame it was settled at, and a suppressed write would leave it claiming an
	// older one.
	return d.flush(true)
}

// Close installs the CURRENT frame as the artifact's final state and freezes the
// sink, so the bytes a surface writes while it tears its layout down (the erase,
// the scroll-region reset, the cursor show) cannot overwrite the screen a reader
// is about to quote. It is idempotent, and it is the caller's last chance to
// make the file agree with the screen: without it the file would hold whatever
// frame the throttle last let through.
//
// A closed sink reports the full length and feeds NOTHING (see Write), so a
// teardown write can neither fail nor change the artifact.
func (d *ScreenDumper) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	d.closed = true
	// A frozen sink must not be re-opened by a window closure that was armed
	// before the freeze: Close's frame is the deliverable (see the closed
	// field), so the pending timer is stopped and its slot cleared.
	if d.tail != nil {
		d.tail.Stop()
		d.tail = nil
	}
	d.mu.Unlock()
	err := d.flush(true)
	// The RAW leg is closed with the sink: its trailer is the record that this
	// run REACHED close-out (a stream without one is a run that died mid-frame,
	// and the two must not look alike). Its error is recorded on the sink, so a
	// caller that only prints the dump's own report still learns about it.
	if d.sgr != nil {
		if serr := d.sgr.Close(); serr != nil {
			d.recordError(serr)
		}
	}
	return err
}

// SetGeometry points the sink at the geometry the surface's LAYOUT just drew
// into, and records whose number it now is. It is the ONE write path for the
// grid's size after construction, and the reason the artifact can be trusted:
// the caller hands over the geometry of the region it INSTALLED, not a second
// probe of a descriptor that may not even be the one the layout measured.
//
// semantics — a no-op unless the move is real:
//
//   - src is the authority (GeometrySourceLayout for a region that was
//     installed; GeometrySourceFallback for a caller whose layout did NOT
//     install, e.g. a terminal too small for the reserved band);
//   - cols/rows <= 0 means "no layout geometry to state": the grid keeps the
//     geometry it has, and the SOURCE becomes the fallback if it was not
//     already one — the sink will not pretend a default grid is a layout's;
//   - an identical geometry with an identical source changes nothing, fires
//     nothing and writes nothing, so a caller may call this on every repaint
//     without paying for it (the ui face does, at its first frame);
//   - a source change at an IDENTICAL geometry moves the provenance and flushes
//     the artifact, but NEVER re-seeds: the cells already in the grid were fed
//     by a real render stream and a fresh Screen would discard them while that
//     stream kept writing from its own cursor (measured 2026-09-26 — see the
//     comment at the check itself);
//   - a REAL size change MOVES the grid (Screen.Resize — never a fresh Screen:
//     see the comment at the call), stamps the new source, flushes the artifact
//     so the file is never stale about its own size, and fires the loud-fallback
//     hook exactly once;
//   - and the record's `terminal=`/`model=` move WITH the grid, on EVERY path
//     above, under the same lock as the resize (stampIdentityGeometryLocked) —
//     a provenance line whose two geometry fields stayed at the constructor's
//     numbers while `grid=` followed the terminal is a line that contradicts
//     itself (measured 2026-11-09: 121x24 / 121x24 over a 101x22 grid).
func (d *ScreenDumper) SetGeometry(src GeometrySource, cols, rows int) {
	if d == nil {
		return
	}
	if src == "" {
		src = GeometrySourceFallback
	}
	d.mu.Lock()
	cur := d.screen.Dump()
	if cols <= 0 || rows <= 0 {
		// Nothing to install: the only honest change left is to stop claiming a
		// layout measured this grid. The SIZE does not move, so the record says
		// the size the grid actually has — the same number on all three fields,
		// with `geometry=fallback` (not a geometry field) carrying the fact that
		// nothing measured a layout for it.
		changed := d.geometry != GeometrySourceFallback
		d.geometry = GeometrySourceFallback
		d.stampIdentityGeometryLocked(cur.Cols, cur.Rows)
		notify := d.onGeom
		fallback := d.geometry == GeometrySourceFallback
		d.mu.Unlock()
		if changed && notify != nil && fallback {
			notify()
		}
		return
	}
	// THE RECORD MOVES WITH THE GRID, on every path below — including the
	// early return for "same size, same authority". `terminal=` and `model=`
	// are rendered from identity, so leaving them at the CONSTRUCTOR's numbers
	// made a resized run report `grid=101x22` between `terminal=121x24` and
	// `model=121x24`: two frozen numbers that describe a screen the layout
	// stopped drawing into, in a line whose whole purpose is to say which
	// geometry the cells are in. Measured 2026-11-09 on a real pty resized
	// 121x24 → 101x22. The stamp is under this same lock as the Resize below,
	// so no reader can catch the grid and the record mid-move.
	d.stampIdentityGeometryLocked(cols, rows)
	if cur.Cols == cols && cur.Rows == rows {
		// The SIZE of this grid is already the one being announced: re-seeding it
		// would DISCARD cells the render stream has already fed into it, while the
		// stream itself keeps writing from its own live cursor — so every row
		// after the re-seed lands displaced (measured 2026-09-26: a chat dump
		// froze `▌ + added **d**` and `you>  c` as ONE frame, the answer's tail
		// painted onto the row the editor's prompt owns, because a re-seed had
		// blanked this grid mid-paint while the painter's cursor stayed where it
		// was). A fresh grid can only be honest when the geometry really changes.
		//
		// The SOURCE still has to move — a fallback grid that receives the
		// layout's own measurement must stop claiming to be a fallback — and the
		// artifact still has to carry that provenance, so the flush below keeps
		// the record and the file in step without touching a cell.
		if d.geometry == src {
			d.mu.Unlock()
			return
		}
		d.geometry = src
		notify := d.onGeom
		loud := d.geometry == GeometrySourceFallback
		d.mu.Unlock()
		if err := d.flush(true); err != nil {
			d.recordError(err)
		}
		if loud && notify != nil {
			notify()
		}
		return
	}
	// A NEW SIZE MOVES THE GRID — it does not replace it. The cells, the styles,
	// the cursor and (above all) the installed scrolling region belong to the
	// PAINT STREAM, which is already underway: a fresh screen would drop the
	// region the layout sent as `CSI 1;24r`, and every later `\n` would then fall
	// through to the screen's own last row instead of scrolling the band —
	// measured 2026-09-28, the answer crawled down the grid and the artifact held
	// the editing line with none of it (Screen.Resize carries the full account).
	d.screen.Resize(cols, rows)
	d.geometry = src
	// The RAW leg's trailer restates the geometry the run ENDED at, so a
	// stream recorded across a resize says so (its header keeps the geometry it
	// was built at). It interprets nothing — this is metadata about the run,
	// not about the bytes.
	if d.sgr != nil {
		d.sgr.SetGeometry(cols, rows)
	}
	notify := d.onGeom
	loud := d.geometry == GeometrySourceFallback
	d.mu.Unlock()
	// The artifact is refreshed AFTER the lock is dropped (flush takes it), so a
	// reader that polls the file never sees a grid of the old width wearing the
	// new record: the install is atomic, so the two move together.
	if err := d.flush(true); err != nil {
		d.recordError(err)
	}
	if loud && notify != nil {
		notify()
	}
}

// stampIdentityGeometryLocked writes the grid's geometry into the record's two
// geometry fields. The caller MUST hold d.mu and must have the grid at (cols,
// rows) — or be about to put it there under the same lock.
//
// WHY THE RECORD CARRIES THREE NUMBERS AT ALL. `terminal=` is the tty this
// surface is attached to, `model=` the geometry the surface was told to lay out
// for, `grid=` the geometry the dump's cells were interpreted at. They can
// LEGITIMATELY differ (a model built before a resize, a socket whose peer is
// another size), which is why the record reports all three rather than one —
// and they are two fields of the identity, not one, because a reader comparing
// them is asking a real question.
//
// What is NOT legitimate is two of them staying at the CONSTRUCTOR's numbers
// while the third follows the terminal, because then the comparison answers
// "the model is stale" about a surface whose model was re-laid out. The chat
// surface's layout repaints at the tty's size on every frame and reports that
// size here, so after a resize all three are the same number — and they are the
// same number because this function moved them, never because a comment said
// they were.
func (d *ScreenDumper) stampIdentityGeometryLocked(cols, rows int) {
	d.identity.ColsRows = [2]int{cols, rows}
	d.identity.TerminalColsRows = [2]int{cols, rows}
}

// Geometry reports the authority behind the grid's current size, and the size
// itself — the pair a caller renders as the record's `geometry=` provenance.
func (d *ScreenDumper) Geometry() (GeometrySource, int, int) {
	if d == nil {
		return "", 0, 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	sd := d.screen.Dump()
	return d.geometry, sd.Cols, sd.Rows
}

// Snapshot returns the CURRENT interpreted state of the render stream.
//
// It reads the grid itself rather than the last file the sink installed, and
// the split is deliberate: the THROTTLE bounds what the RENDER path pays (a
// snapshot plus up to two file installs), while a holder of the sink asks for
// the truth NOW and pays for its own copy. A snapshot that lagged its own
// inbound bytes would make every assertion a race against dumpMinInterval.
func (d *ScreenDumper) Snapshot() lineedit.ScreenDump {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.screen.Dump()
}

// Text returns the latest dump's readable rendering.
func (d *ScreenDumper) Text() string { return d.Snapshot().Text() }

// Path reports where the dump is written ("" when memory-only), so a caller can
// tell the operator — and the report — how to retrieve it.
func (d *ScreenDumper) Path() string { return d.path }

// Writes reports how many dump files this sink has installed. It is the
// MEASURED cost of the capability: a session that ran a hundred frames and
// wrote three dumps is a different claim from one that wrote a hundred.
func (d *ScreenDumper) Writes() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.written
}

// Frames reports how many render writes the sink interpreted.
func (d *ScreenDumper) Frames() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.frames
}

// JSONPath is the machine-checkable twin's path.
func (d *ScreenDumper) JSONPath() string {
	if d.path == "" {
		return ""
	}
	return d.path + dumpJSONSuffix
}

// flush snapshots the grid and, when forced or when the RENDERING changed,
// installs the files. A rendering identical to the last one is not rewritten:
// the common steady state (an idle surface repainting the same frame) costs one
// string comparison, not two file renames.
//
// The comparison is made on the dump BODY, never on the whole record: the
// record carries the write time and a dump counter, so its own text differs on
// every flush and would make every frame look like a change.
//
// It returns the first error it hits; the caller records it (Write cannot
// return it without breaking the render stream).
func (d *ScreenDumper) flush(force bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.armTailFlushLocked()
	return d.flushUnarmed(force)
}

// flushUnarmed is flush with the tail timer left alone. It is the closure's own
// road (tailFlush): a window that has just been closed must not arm the next
// one, or an idle surface would keep a timer alive forever for frames that change
// nothing.
func (d *ScreenDumper) flushUnarmed(force bool) error {
	d.dump = d.screen.Dump()
	d.last = d.now()
	if d.path == "" {
		return nil
	}
	body := d.dump.Text()
	if !force && body == d.lastSig {
		return nil
	}
	if err := writeFileAtomic(d.path, []byte(d.record(d.dump, body)), dumpFilePerm); err != nil {
		return err
	}
	blob, err := d.dumpRecordJSON()
	if err == nil {
		err = writeFileAtomic(d.JSONPath(), blob, dumpFilePerm)
	}
	if err != nil {
		return err
	}
	d.lastSig = body
	d.written++
	return nil
}

// record renders the file's contents: a provenance line, then the dump's own
// readable rendering. The provenance sits ABOVE the dump body so the body stays
// exactly what Text() returns (a report can quote the file, and a reader can
// still find the dump's own "#gopi-screen-dump" header line).
func (d *ScreenDumper) record(sd lineedit.ScreenDump, body string) string {
	return d.provenanceLine(sd) + "\n" + body
}

// provenanceLine is the observer's record about the observed surface. Every
// field answers a question an agent holding the file would otherwise have to
// guess: which process, which target, how big, how stale, how expensive.
//
// A recorded write ERROR is carried here rather than swallowed: a dump nobody
// could install must not read as a healthy dump (Rule 15).
func (d *ScreenDumper) provenanceLine(sd lineedit.ScreenDump) string {
	line := RecordPrefixFor(d.identity.Surface) +
		" pid=" + strconv.Itoa(d.identity.PID) +
		" target=" + d.identity.Target +
		" tools=" + boolWord(d.identity.Tools) +
		" terminal=" + geomWord(d.identity.TerminalColsRows) +
		" model=" + geomWord(d.identity.ColsRows) +
		" grid=" + geomWord([2]int{sd.Cols, sd.Rows}) +
		// geometry= is the AUTHORITY behind grid=: which geometry the grid was
		// built at, and whether anything measured it. It is placed IMMEDIATELY
		// after the grid so a reader that quotes one line quotes the number and
		// its provenance together — a grid without its authority is exactly the
		// mis-scaling this field exists to prevent (GeometryFallbackNotice).
		" geometry=" + string(d.geometry) +
		" mode=" + string(d.mode) +
		" frames=" + strconv.Itoa(d.lastFrameCount()) +
		" dumps=" + strconv.Itoa(d.written) +
		" recorded=" + d.now().UTC().Format(time.RFC3339) +
		" started=" + d.identity.Started.UTC().Format(time.RFC3339) +
		d.depthFields()
	if d.err != "" {
		line += " error=" + strconv.Quote(d.err)
	}
	return line
}

// depthFields renders the CAPABILITY decision's own fields onto the record line.
//
// WHY ON THE RECORD AND NOT IN THE GRID. `ScreenDump` (lineedit) describes CELLS:
// a change there would change the wire format every dump reader parses, and the
// capability is not a property of any cell — it is a property of the RUN, decided
// once before the first frame, and it EXPLAINS the attributes the cells carry
// (two runs on two terminals can hold the same text and differ in every cell's
// colour, and only this line says why). So the record's vocabulary takes the new
// fields and the dump's own version stays where it is.
//
// The values are SINGLE TOKENS, because the record's fields are space-separated
// (`ParseDumpRecord`): the full probe evidence carries spaces and belongs in the
// JSON twin, while what a text reader needs from this line is the verdict and the
// PROVENANCE of it — which evidence decided, and what the loser claimed.
//
// Nothing is rendered when no capability was reported (a sink a test built by
// hand, a surface with no colour decision): the line then has exactly the shape a
// pre-capability reader parses.
func (d *ScreenDumper) depthFields() string {
	r := d.capability
	if r == nil {
		return ""
	}
	line := " " + CapabilityField + r.Source +
		" " + DepthField + r.Depth +
		" " + DeclaredDepthField + r.DeclaredDepth
	if r.Palette != "" {
		line += " " + PaletteField + r.Palette
	}
	return line
}

// CapabilityReport is the recorded capability decision: the fields the record
// carries, and the evidence behind them. It is what a caller hands the sink
// (SetCapabilityReport) — defined here, beside the fields it fills, so the writer
// and the JSON twin cannot drift.
type CapabilityReport struct {
	// Source names WHICH evidence decided the depth (lineedit's
	// DepthSourceMeasured / DepthSourceDeclared / DepthSourceDefault). It is the
	// field that stops "256 colours" being read as a fact about the terminal when
	// it was a string in the environment.
	Source string `json:"source"`
	// Depth is the decided tier as a word (`basic`, `256`, `truecolor`).
	Depth string `json:"depth"`
	// DeclaredDepth is what the environment claimed, kept even when the
	// measurement overruled it: a record showing only the winner cannot answer
	// "which did the run use?".
	DeclaredDepth string `json:"declared_depth"`
	// Palette is the table the run drew with (the resolved name), "" when the
	// caller did not report one.
	Palette string `json:"palette,omitempty"`
	// Probe is the terminal's own answer, verbatim — evidence and verdict in one
	// line (lineedit.CapabilityMeasurement.Summary), empty when nothing measured.
	// It lives in the twin rather than on the record line because it contains
	// spaces, and the record line is a space-separated field list.
	Probe string `json:"probe,omitempty"`
}

// SetCapabilityReport records the capability decision this surface drew with, so
// the artifact says which evidence chose its colours (and what the other evidence
// claimed). It is a ONE-WAY setter called before the first frame; a sink nobody
// reports to records nothing rather than recording a default dressed up as a
// measurement (Rule 15).
func (d *ScreenDumper) SetCapabilityReport(r CapabilityReport) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	copied := r
	d.capability = &copied
}

// CapabilityReport returns the recorded decision, and whether one was recorded.
func (d *ScreenDumper) CapabilityReport() (CapabilityReport, bool) {
	if d == nil {
		return CapabilityReport{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.capability == nil {
		return CapabilityReport{}, false
	}
	return *d.capability, true
}

// The record's capability vocabulary. Named (Rule 1b) because the writer, the
// JSON twin and every reader (a test, `gopi ui_state`, a report) key on them.
const (
	// CapabilityField carries the evidence that decided the depth.
	CapabilityField = "capability="
	// DepthField carries the decided tier.
	DepthField = "depth="
	// DeclaredDepthField carries what the environment claimed.
	DeclaredDepthField = "declared_depth="
	// PaletteField carries the table the run drew with.
	PaletteField = "palette="
)

// lastFrameCount reports how many render writes the sink has interpreted.
func (d *ScreenDumper) lastFrameCount() int { return d.frames }

// recordError keeps the last dump-writing failure for the next record's
// `error=` field. Write cannot return it without breaking the render stream,
// and dropping it would make a dump that was never installed look healthy
// (Rule 15 — a failed write is recorded, never swallowed).
func (d *ScreenDumper) recordError(err error) {
	if err == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.err = err.Error()
}

// dumpRecordJSON renders the machine-checkable twin: the same dump, plus the
// observer's identity, in one object.
func (d *ScreenDumper) dumpRecordJSON() ([]byte, error) {
	blob, err := json.MarshalIndent(struct {
		Identity DumpIdentity        `json:"identity"`
		Observer jsonFields          `json:"observer"`
		Screen   lineedit.ScreenDump `json:"screen"`
	}{
		Identity: d.identity,
		Observer: d.jsonFields(),
		Screen:   d.dump,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("uiclient: cannot render the dump JSON: %w", err)
	}
	return append(blob, '\n'), nil
}

// jsonFields is the observer's own measurements, carried beside the screen so a
// reader can date the dump against the render traffic it came from.
type jsonFields struct {
	// Frames is how many render writes the sink interpreted.
	Frames int `json:"frames"`
	// Dumps is how many dump files it installed.
	Dumps int `json:"dumps"`
	// Error is the last dump-writing failure, empty when there was none.
	Error string `json:"error"`
	// Geometry is the AUTHORITY behind the grid's size — the record's own
	// `geometry=` field, carried into the twin so a JSON reader cannot mistake a
	// fallback grid for a layout-measured one.
	Geometry GeometrySource `json:"geometry"`
	// Capability is the colour decision the surface drew with — the record line's
	// `capability=`/`depth=`/`declared_depth=` fields AND the probe's own
	// evidence, which the line cannot carry (the probe summary holds spaces, and
	// the line is a space-separated field list). A JSON reader gets the whole
	// measurement here; a text reader gets the verdict and its provenance. Nil
	// when the surface reported none.
	Capability *CapabilityReport `json:"capability,omitempty"`
}

// jsonFields snapshots the observer's counters for the JSON rendering.
func (d *ScreenDumper) jsonFields() jsonFields {
	return jsonFields{Frames: d.frames, Dumps: d.written, Error: d.err, Geometry: d.geometry, Capability: d.capability}
}

// boolWord renders a switch as the word the record uses ("on"/"off") — never a
// bare "true"/"1" that a reader has to know the convention of.
func boolWord(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// geomWord renders a geometry pair as "COLSxROWS".
func geomWord(g [2]int) string { return strconv.Itoa(g[0]) + "x" + strconv.Itoa(g[1]) }

// writeFileAtomic installs contents at path by writing a sibling temporary file
// and renaming it, so a reader NEVER sees a half-written dump: it sees the
// previous complete frame or the new one. A dump that could be read mid-write
// would make every layout assertion a race.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp")
	if err != nil {
		return fmt.Errorf("uiclient: cannot create the dump temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// Every failure path removes the temp file: a stale .tmp beside the dump
	// would be litter in the operator's directory (Rule 30's spirit, applied to
	// a runtime artifact rather than a worktree).
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName) //stylegate:ignore best-effort cleanup on an error path; the write error is the one being returned//nolint:errcheck // best-effort cleanup on an error path
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() //stylegate:ignore the write error is the cause; a close failure would mask it//nolint:errcheck // the write error is the cause; the close would mask it
		return fmt.Errorf("uiclient: cannot write the dump temp file %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close() //stylegate:ignore the write error is the cause; a close failure would mask it//nolint:errcheck // as above
		return fmt.Errorf("uiclient: cannot set the dump file mode on %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("uiclient: cannot close the dump temp file %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("uiclient: cannot install the dump at %s: %w", path, err)
	}
	tmpName = "" // installed: nothing left to clean up
	return nil
}
