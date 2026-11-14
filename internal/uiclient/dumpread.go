package uiclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// sessionDirWalkLevelsMax bounds how far above a dump the session-directory
// resolution looks for the record that owns it. A dump sits either in the
// session dir itself (bare session, legacy flat) or inside ONE artifact
// subdirectory the recipes and the task machinery create — and occasionally one
// level deeper (<session>/evidence/frames/). The bound exists so the walk above
// a stray artifact cannot wander the filesystem: it is a lookup, not a search.
const sessionDirWalkLevelsMax = 4

// dumpread.go — the READ side of the self-observation dump: find the dump a
// live `gopi ui` wrote, prove whose it is, and refuse to guess when it cannot.
//
// WHY A READER IS NOT A FILE READ. `internal/uiclient/dump.go` answers "what
// did the surface paint"; this file answers the question an agent actually
// holds — "which of the surfaces running on this host is the one I mean, and is
// it still there?" Those are different failure modes, and the naive answer
// (`read the dump file`) gets both of them wrong:
//
//   - A dump file is not a live screen. The sink is throttled, and the file's
//     mtime is not evidence of anything: an observer that skipped the liveness
//     check cannot tell a surface painting right now from one that exited an
//     hour ago.
//   - There may be several. The operator's `gopi ui --tools` and the agent's
//     own `gopi ui` are two processes with two dumps; a reader that takes the
//     first one it finds reports a screen nobody asked about. That is why the
//     ambiguity verdict BELOW is a REFUSAL, not a best-effort pick.
//
// THE IDENTITY IS (pid, kernel start ticks, target), NOT THE PATH. The file's
// own record says which pid wrote it, but a pid is recycled within minutes on a
// busy host — so the reader re-reads /proc/<pid>/stat field 22 and requires an
// exact match against the dump's `started=` stamp (the pair that makes the
// identity unforgeable, same discipline as internal/sandbox's owner marker).
// A recycled pid is a DIFFERENT surface, and this reader says so.
//
// SCOPE. The verdicts live here, next to the format they read, so the CLI verb
// (`gopi ui_state`) stays a thin argv->verdict renderer rather than a second
// place where "is this dump mine" is decided.

// DumpFileName is the dump's file name, as `gopi ui` writes it. It is exported
// because a READER that does not know the name cannot find the file, and a
// second spelling of it (the resolver's, a test's) is exactly the drift Rule 13
// closes: the writer (cmd/agent/cli_ui.go) and every reader reference this ONE
// name.
//
// It is the UI surface's name. `gopi chat` writes ChatDumpFileName() beside it;
// both come from the SAME discriminator (dump.go's Surface / DumpFileNames), so
// adding a surface cannot leave a reader scanning for a name that no writer
// spells.
const DumpFileName = UIDumpFile

// ChatDumpFileName is the chat surface's artifact: `gopi chat` writes it (BY
// DEFAULT since 2026-10-01; `--no-dump-screen` declines it) and this scan finds
// it. It is the SECOND name in the same table —
// this constant exists so a caller (a verb rendering an error, a test) can name
// it without spelling the literal, never so it can be spelled twice.
const ChatDumpFileName = ChatDumpFile

// The dump-discovery layout vocabulary: the profile's own directory names and
// the directory prefixes a runtime scratch class uses. They are constants
// (Rule 1b) because a scan that spelled "runtime" by hand would silently stop
// finding the ui-session dumps the day the scratch layout changed.
const (
	// dumpQueueDirName is the scratch home's queue directory, the parent of the
	// per-process runtime scratch roots.
	dumpQueueDirName = "queue"
	// dumpRuntimeDirPrefix heads every runtime scratch entry name
	// ("queue-runtime-<class>-<pid>"); internal/scratch's runtimeEntryPrefix.
	dumpRuntimeDirPrefix = "queue-runtime-"
	// dumpUISessionClass is the scratch class a session-less `gopi ui` (the
	// `--server` case) writes its dump into; internal/scratch's
	// RuntimeClassUISession.
	dumpUISessionClass = "ui-session"
	// dumpScanDepth bounds the walk under each scan root, so a pathological
	// tree cannot turn one observation into a filesystem crawl.
	dumpScanDepth = 3
	// dumpMaxCandidates caps how many candidate files one scan considers. It is
	// a bound on work, not a filter: hitting it is REPORTED (see Candidates),
	// never silently truncated.
	dumpMaxCandidates = 256
)

// The dump file's own provenance vocabulary — the fields of the record line
// `internal/uiclient/dump.go` writes ABOVE the dump body:
//
//	#gopi-ui-dump pid=4711 target=http://… tools=on … started=2026-…Z
//
// Parsed BY NAME (never by position) so the writer can add a field without
// this reader mis-reading its neighbour.
const (
	dumpRecordPIDField     = "pid"
	dumpRecordTargetField  = "target"
	dumpRecordStartedField = "started"
	// dumpRecordToolsField is the record's `tools=` switch, so a caller can
	// tell a `--tools` surface from a plain one.
	dumpRecordToolsField = "tools"
	// dumpRecordGeometryField is the record's `geometry=` field: the AUTHORITY
	// behind the grid's size (GeometrySourceLayout / terminal / fallback). It
	// exists so a reader can tell a grid built at the layout's own geometry from
	// one built at a default nothing measured — the difference between a faithful
	// observation and fabricated corruption.
	dumpRecordGeometryField = "geometry"
	// dumpRecordFieldSep separates the record's fields; dumpRecordFieldAssign
	// is its "name=value" glue.
	dumpRecordFieldSep    = " "
	dumpRecordFieldAssign = "="
)

// State is the observation of one dump's surface. It is the total answer this
// package gives — including the two cases that are NOT "here is a screen": a
// surface that has exited, and a dump written by a process claimed by another
// command.
type State string

const (
	// StateLive: the dump is usable and the surface that wrote it is still the
	// process that wrote it (pid + start ticks matched).
	StateLive State = "live"
	// StateExited: the observing process is GONE — the dump is a snapshot of a
	// surface that ended. It is a complete screen, not an empty one, and a
	// reader must not report it as current (the diagnosis Rule 15 asks for: the
	// effect is named, not shown as fact).
	StateExited State = "exited"
	// StateForeign: a process HOLDS the pid, but it is not the incarnation that
	// wrote the dump (recycled pid) or it is not a `gopi` process at all. The
	// dump is someone else's snapshot; nothing about the pid is claimed.
	StateForeign State = "foreign"
	// StateUnreadable: the file exists and could not be parsed as a dump. An
	// unreadable artifact is reported as what it is — never as an empty screen.
	StateUnreadable State = "unreadable"
)

// ExitedNotice is the sentence a reader MUST carry when the target has exited.
// It is a constant because the point of the diagnosis is that it is said: a
// report that rendered an exited surface's last frame as "the current screen"
// has made a false claim about the present, and the fix is a fixed sentence
// rather than a phrasing each caller invents.
const ExitedNotice = "the target has exited cleanly: the dump is the LAST frame that surface painted, not the current screen"

// ScreenGridNotice is the FIRST sentence of every dump description. It states
// WHAT the artifact is — an interpreted cell grid — because every wrong
// conclusion about a text dump starts from reading it as pixels or as raw
// bytes: a glyph question is unanswerable here, and so is a sequence-order
// question.
const ScreenGridNotice = "an interpreted cell grid (not bytes, not pixels): the escape sequences were already interpreted by the terminal emulator, so this describes the CELLS of the screen"

// UnpaintedNotice is the sentence a reader MUST carry when the sink never
// interpreted a render frame (`frames=0`). The grid is real and empty, and
// reading it as "the screen is blank" makes a claim about the surface that the
// artifact cannot support: a surface that has not painted yet and a surface
// whose panel was erased on the way out BOTH leave an all-blank grid, and the
// second one is a former screen, not a current one. The empty grid is evidence
// about the OBSERVER's counter, so that is what the reader reports.
const UnpaintedNotice = "the dump sink never interpreted a render frame (frames=0): the grid below is an all-blank snapshot — the surface had not painted yet, or its panel was erased as it quit. It is NOT evidence that the screen is blank."

// UnpaintedFrames is the observer counter value that means "no frame was ever
// interpreted". Named because it is a fact about the artifact, not a count: the
// sink increments at the first interpreted write, so zero is the boundary
// between "painted" and "never painted".
const UnpaintedFrames = 0

// BlankUnpaintedNotice is the sentence a reader MUST carry when the sink DID
// interpret frames but the grid they left is still all-blank — measured on a
// real 117x26 pty as `frames=3 dumps=1` on `gopi chat`, idle, no input.
//
// WHY `frames>0` IS NOT ENOUGH, measured — a scripted input + model turn on the
// same pty reads `frames=117 dumps=10`; a chat that then answers reads
// `frames=687 dumps=45`; an IDLE chat reads `frames=3 dumps=1` and the file does
// not change again (17 samples over 10 s: identical bytes, identical mtime). The
// count is a fact about WRITES, and the chat layout writes only when something
// CHANGED — so a screen that never changed cannot be told, by `frames` alone,
// from a screen that painted nothing at all. A reader that renders such a grid
// as "the surface is live and this is it" has made a claim about the screen that
// the artifact cannot support, which is the failure this capability exists to
// prevent.
//
// So the reader asks the GRID, not the counter: an all-blank grid is reported as
// blank, and its own presence is stated (the surface had not painted its content
// yet, or it painted and every cell was a blank).
const BlankUnpaintedNotice = "the dump sink interpreted frames (frames>0) but the grid is ALL-BLANK: this artifact shows the surface's state before its own painted content appeared, or a painted screen whose every cell was blank — it is NOT evidence that the surface's screen content is empty."

// dumpFileRef pairs a discovered dump file with the surface its NAME declares.
// The scan resolves it ONCE, at discovery, so a candidate can carry the surface
// even when its contents could not be read (an unreadable file still belongs to
// the surface that named it).
type dumpFileRef struct {
	path    string
	surface Surface
}

// Candidate is one dump file the discovery scan found and parsed.
type Candidate struct {
	// Surface is which of gopi's text surfaces wrote this artifact — the ui
	// dump or the chat dump — from the file's NAME (SurfaceForDumpFile, the
	// authority), corroborated by the record's own prefix. A caller that wants
	// one surface's screen filters on THIS, so a chat dump and a ui dump are
	// never confused for two observations of the same surface.
	Surface Surface
	// Path is the dump file this candidate was read from.
	Path string
	// Record is the provenance line parsed from the file.
	Record DumpRecord
	// Dump is the parsed screen state.
	Dump lineedit.ScreenDump
	// State is the identity verdict for this candidate.
	State State
	// Reason explains the verdict in one clause (why foreign, why unreadable,
	// why exited) — empty for StateLive.
	Reason string
	// ModTime is the dump file's modification time, reported so a reader can
	// date the artifact. It is NOT evidence of liveness: a throttled sink does
	// not touch the file while the surface repaints the same frame.
	ModTime time.Time
	// Frames is the sink's own count of render frames it interpreted. Zero
	// means the grid was never painted — see UnpaintedNotice.
	Frames int
	// Dumps is how many times the sink installed a state into the file.
	Dumps int
	// SinkError is the sink's last write error, empty when it had none. It is
	// carried because a dump that is empty BECAUSE every write failed is a
	// different fact from a dump whose writer was simply mid-startup.
	SinkError string
}

// Unpainted reports whether this candidate's grid was never painted. A caller
// that renders the grid anyway must carry UnpaintedNotice, and a caller
// asserting on layout must treat it as "no observation".
func (c Candidate) Unpainted() bool { return c.Frames <= UnpaintedFrames }

// BlankUnpainted reports whether this candidate's grid carries NO content in any
// cell, read from the GRID itself — the one measurement neither the frame count
// nor the writer's own account can replace.
//
// It is deliberately SEPARATE from Unpainted: frames=0 is the observer saying
// "nothing ever reached me", while this is the grid saying "every cell is a
// blank". A grid can be blank with three frames interpreted (an idle chat, with
// its status row unpainted or scrolled away) and a grid can be NON-blank with
// frames=0 on an artifact written by another build — so a reader that wants to
// know what the artifact shows asks THIS, and asks the counter separately for
// what the observer did.
//
// The comparison is on the cell-exact Grid when the value HAS one (every parsed
// artifact does — Screen.Dump fills a Grid entry per row), and falls back to the
// cell-exact Lines only for a value built without a grid (the containment core's
// own fixtures). Neither representation is a trim of the other: a row whose
// content sits at its right edge is blank in Lines and NOT blank in Grid, which
// is why the grid is asked first.
func ScreenDumpAllBlank(d lineedit.ScreenDump) bool {
	if len(d.Grid) > 0 {
		for _, row := range d.Grid {
			if strings.TrimSpace(row) != "" {
				return false
			}
		}
		return true
	}
	// No grid cells at all: the value carries no cell-exact rows, so the only
	// content it could hold is its readable rendering. An empty value (no grid,
	// no lines) therefore reads BLANK — "there is no content to read" — which is
	// the conservative answer for a reader, and the one this predicate's callers
	// refuse on.
	for _, row := range d.Lines {
		if strings.TrimSpace(row) != "" {
			return false
		}
	}
	return true
}

// BlankUnpainted reports whether this candidate's grid carries NO content in any
// of its cells, read from the artifact itself — the one measurement neither the
// frame count nor the writer's own account can replace (ScreenDumpAllBlank is
// the predicate; this is the reader's door to it).
//
// It is deliberately SEPARATE from Unpainted: frames=0 is the observer saying
// "nothing ever reached me", while this is the grid saying "there is no content
// here". A grid can be blank with three frames interpreted (the measured idle
// chat) and a grid can be NON-blank with frames=0 on an artifact written by
// another build — so a reader that wants to know what the artifact SHOWS asks
// this, and asks the counter separately for what the observer DID.
func (c Candidate) BlankUnpainted() bool { return ScreenDumpAllBlank(c.Dump) }

// DumpRecord is the observer's provenance line: the facts a reader needs to
// answer "WHOSE screen is this file?", rendered by the dump sink above the
// dump body.
type DumpRecord struct {
	// Surface is the surface that wrote the dump, recovered from the record's
	// own prefix (chat dumps are marked "#gopi-chat-dump"). It is a fact about
	// the WRITER: `gopi ui` and `gopi chat` write the same record shape, and a
	// reader that could not tell them apart would report a chat screen as a ui
	// surface's state.
	Surface Surface
	// PID is the process that wrote the dump.
	PID int
	// Target is the server origin the surface was attached to.
	Target string
	// Tools mirrors the surface's tool switch.
	Tools bool
	// Started is when that surface started.
	Started time.Time
	// Geometry is the AUTHORITY behind the grid's size, from the record's own
	// `geometry=` field. The zero value means the record did not carry the field
	// (an artifact written before it existed), which GeometryOf reports as the
	// fallback — the conservative answer, because an artifact that cannot state
	// its authority must not be read as if a layout had measured it.
	Geometry GeometrySource
	// Fields is every parsed name=value pair, so a caller can report or assert
	// on a field this struct does not model without re-parsing the line.
	Fields map[string]string
	// Line is the raw record line, quoted verbatim in a report.
	Line string
}

// ErrNoCandidates is returned when a scan found no dump at all. It is a
// distinct error (not an empty result) because "nothing to observe" and "a
// screen that is empty" are different facts, and conflating them is how an
// empty screen gets reported as truth.
var ErrNoCandidates = fmt.Errorf("no %s dump found in any scanned location", DumpFileName)

// DumpNotArmedNotice is the ROUTE OUT of ErrNoCandidates, and it is part of the
// representation rather than a message a caller composes — the arming flag is
// read off the record prefix this package itself accepts, so a notice can never
// name a flag no surface writes (Rule 13).
//
// WHY IT IS NOT IN ErrNoCandidates' OWN TEXT. That error is the reader's fact
// ("nothing to read") and it is embedded in other sentences (the refusal
// rendering, the list view, a JSON refusal field); an arming instruction spliced
// into it would leak into all of them, including the ones where the reader is
// looking at a dump it simply could not parse. The route out is a SEPARATE
// sentence, printed beside the absence instead.
//
// MEASURED BEFORE THIS SENTENCE EXISTED (2026-09-27, task
// 01a0e070-d783-72b0-adc7-c5d1cda6bf53): `gopi ui_state --compare "<line>"
// --target chat` with no dump on hand exited 1 and printed exactly one line —
// "no ui-screen.dump dump found in any scanned location" — naming neither the
// flag that would have written one nor the verb that carries the flag. An agent
// reading that sentence learns there is no screen; nothing tells it how to have
// one next time, which is the whole reason the loop went unrun in the first
// place.
var DumpNotArmedNotice = fmt.Sprintf(
	"a dump exists only while the surface is armed: `gopi %s` writes one BY DEFAULT (decline it with `gopi %s %s`), while `gopi %s` needs `gopi %s %s` (artifact %s); with no dump the surface's byte stream is unchanged",
	dumpArmVerbChat, dumpArmVerbChat, dumpNoArmFlag, dumpArmVerbUI, dumpArmVerbUI, dumpArmFlag, DumpFileName)

// The arming verbs and the flag spelling, as THIS package's contract with the
// surfaces that tee the write. They are the same spellings the chat/ui verbs
// advertise (cmd/agent/chat_dump.go, cmd/agent/cli_ui.go); they live here as
// well because the notice above belongs to the reader — and to whether a
// notice names a real flag, not because the strings were copied by hand.
//
// dumpArmFlag is an ALIAS of the leaf constant package's own definition
// (Rule 13/14): the chat dump's arm spelling is compared on in cmd/agent and
// rendered here, and a second literal is how the two drift apart.
const (
	dumpArmVerbChat = "chat"
	dumpArmVerbUI   = "ui"
	dumpArmFlag     = cliverbs.DumpScreen
	// dumpNoArmFlag is the chat dump's OFF switch, which the notice above must
	// name: the chat artifact is written by DEFAULT, so the remedy for "no dump
	// here" on that surface is spelled as its decline, not as its arm.
	dumpNoArmFlag = cliverbs.NoDumpScreen
)

// NoDumpForSessionError is the OTHER shape of "there is nothing to read", and it
// is its own type because it is a different fact from ErrNoCandidates: a scan
// found dumps, none of them belongs to the session the caller names. It is an
// error rather than a formatted string so the ONE predicate below can tell the
// two apart without matching on prose — the refusal path prints a DIFFERENT
// route out for each (arm a dump, vs choose one of the surfaces on hand), and a
// string match would silently stop recognising this shape the next time the
// sentence is edited.
type NoDumpForSessionError struct {
	// Files is the artifact list the scan looked for.
	Files string
	// Want is the session directory the caller named.
	Want string
	// Have is the session list of the dumps that WERE on hand.
	Have string
}

func (e *NoDumpForSessionError) Error() string {
	return fmt.Sprintf("no %s dump belongs to session %s (the dumps on hand belong to: %s) — pass --pid (or --list) to choose a different surface",
		e.Files, e.Want, e.Have)
}

// DumpAbsent reports whether err is the ABSENCE of a dump — either shape of it —
// as opposed to a refusal about dumps that exist (an ambiguity, an unreadable
// pair, a session spelling that resolved nowhere). It is the ONE predicate the
// route-out notice is gated on, and both shapes are unreachable-from-each-other,
// so a caller can never be told to arm a dump it already has.
func DumpAbsent(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNoCandidates) {
		return true
	}
	var noSession *NoDumpForSessionError
	return errors.As(err, &noSession)
}

// ParseDumpRecord parses the observer's provenance line. A line that is not the
// record prefix is an error, never a zero-valued record: a reader that treated
// a missing record as defaults would attribute the screen to pid 0 and then
// judge liveness against it.
func ParseDumpRecord(line string) (DumpRecord, error) {
	rec := DumpRecord{Fields: map[string]string{}, Line: line}
	trimmed := strings.TrimRight(line, "\r\n")
	surface, body, ok := CutRecordPrefix(trimmed)
	if !ok {
		return rec, fmt.Errorf("not a dump record line: %q starts with neither %q nor %q",
			trimmed, DumpRecordPrefix, ChatDumpRecordPrefix)
	}
	rec.Surface = surface
	for _, field := range strings.Split(body, dumpRecordFieldSep) {
		name, value, ok := strings.Cut(field, dumpRecordFieldAssign)
		if !ok || name == "" {
			continue // a bare word carries no fact; it is skipped, not guessed at
		}
		rec.Fields[name] = value
	}
	if raw, ok := rec.Fields[dumpRecordPIDField]; ok {
		pid, err := strconv.Atoi(raw)
		if err != nil {
			return rec, fmt.Errorf("dump record pid %q is not a number", raw)
		}
		rec.PID = pid
	}
	rec.Target = rec.Fields[dumpRecordTargetField]
	rec.Tools = rec.Fields[dumpRecordToolsField] == dumpBoolWordOn
	rec.Geometry = GeometrySource(rec.Fields[dumpRecordGeometryField])
	if raw, ok := rec.Fields[dumpRecordStartedField]; ok {
		started, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return rec, fmt.Errorf("dump record started %q is not RFC3339: %w", raw, err)
		}
		rec.Started = started
	}
	return rec, nil
}

// ReadDump reads ONE dump artifact: the provenance record from the text dump,
// the screen state from its machine-checkable twin, and the identity verdict
// against the process table.
//
// WHY THE TWIN. The text file is the form a human reads and a report quotes; the
// twin (path + dumpJSONSuffix) is the same observation with its STRUCTURE intact
// — a dump parsed back from the text rendering would be a second parser to keep
// in step with the renderer, which is precisely what the twin exists to avoid.
// The two are written together by the sink, so a pair that is missing or that
// disagrees is reported as UNREADABLE rather than half-read.
func ReadDump(path string) (Candidate, error) {
	cand := Candidate{Path: path}
	blob, err := os.ReadFile(path)
	if err != nil {
		return cand, fmt.Errorf("cannot read the dump %s: %w", path, err)
	}
	if info, err := os.Stat(path); err == nil {
		cand.ModTime = info.ModTime()
	}
	record, _, err := splitDumpRecord(string(blob))
	if err != nil {
		cand.State = StateUnreadable
		cand.Reason = err.Error()
		return cand, err
	}
	cand.Record = record
	twin, err := readDumpTwin(path)
	if err != nil {
		cand.State = StateUnreadable
		cand.Reason = err.Error()
		return cand, err
	}
	if twin.Identity.PID != record.PID {
		cand.State = StateUnreadable
		cand.Reason = fmt.Sprintf("%s names pid %d while its .json twin names pid %d — the pair is inconsistent",
			path, record.PID, twin.Identity.PID)
		return cand, fmt.Errorf("dump %s: %w", path, errors.New(cand.Reason))
	}
	// The file's NAME declares the surface; the record's prefix must AGREE. A
	// mismatch means the artifact was renamed or rewritten by something that
	// did not know the vocabulary, and the disagreement is REPORTED rather than
	// resolved by picking a winner: an artifact whose two halves disagree about
	// its writer cannot be attributed to either (Rule 15).
	surface, known := SurfaceForDumpFile(filepath.Base(path))
	if !known {
		cand.State = StateUnreadable
		cand.Reason = fmt.Sprintf("%s names no known surface's dump (known: %s)", path, knownDumpFileList())
		return cand, fmt.Errorf("dump %s: %w", path, errors.New(cand.Reason))
	}
	if record.Surface != surface {
		cand.Surface = surface
		cand.State = StateUnreadable
		cand.Reason = fmt.Sprintf("%s is named as a %s dump while its record is marked %q (the two halves disagree)",
			path, surface, RecordPrefixFor(record.Surface))
		return cand, fmt.Errorf("dump %s: %w", path, errors.New(cand.Reason))
	}
	cand.Surface = surface
	cand.Dump = twin.Screen
	cand.Frames = twin.Observer.Frames
	cand.Dumps = twin.Observer.Dumps
	cand.SinkError = twin.Observer.Error
	cand.State, cand.Reason = dumpIdentityVerdict(record)
	return cand, nil
}

// dumpTwin is the shape of the machine-checkable twin the sink writes beside the
// text dump: the observer's identity, its own counters, and the screen.
type dumpTwin struct {
	Identity DumpIdentity        `json:"identity"`
	Observer dumpTwinObserver    `json:"observer"`
	Screen   lineedit.ScreenDump `json:"screen"`
}

// dumpTwinObserver is the observer's own measurements (how many frames it
// interpreted, how many dumps it installed, the last write error).
type dumpTwinObserver struct {
	Frames int    `json:"frames"`
	Dumps  int    `json:"dumps"`
	Error  string `json:"error"`
}

// readDumpTwin reads and parses the machine-checkable twin of a text dump.
func readDumpTwin(path string) (dumpTwin, error) {
	var twin dumpTwin
	twinPath := path + dumpJSONSuffix
	blob, err := os.ReadFile(twinPath)
	if err != nil {
		return twin, fmt.Errorf("the dump's machine-checkable twin %s cannot be read: %w", twinPath, err)
	}
	if err := json.Unmarshal(blob, &twin); err != nil {
		return twin, fmt.Errorf("the twin %s is not a dump record: %w", twinPath, err)
	}
	if twin.Screen.Cols == 0 && twin.Screen.Rows == 0 {
		return twin, fmt.Errorf("the twin %s carries no screen geometry", twinPath)
	}
	return twin, nil
}

// splitDumpRecord cuts the file into its provenance record and its JSON body.
// The record is the file's FIRST line (the sink writes it above the dump), and
// a file without one is refused rather than assumed: an anonymous dump cannot
// be attributed to a surface.
func splitDumpRecord(content string) (DumpRecord, string, error) {
	line, rest, ok := strings.Cut(content, "\n")
	if !ok {
		return DumpRecord{}, "", fmt.Errorf("the dump carries no record line and no body")
	}
	record, err := ParseDumpRecord(line)
	if err != nil {
		return DumpRecord{}, "", fmt.Errorf("the dump's first line is not its provenance record: %w", err)
	}
	return record, rest, nil
}

// dumpIdentityVerdict decides whether the process named by a dump is still the
// process that WROTE it — the (pid, start ticks) pair, with the command name as
// corroboration.
//
// The three verdicts are deliberately distinct:
//
//   - the pid is gone               -> StateExited (a clean end: the snapshot is
//     the last frame, and the reader must say
//     so instead of reporting it as current);
//   - the pid is alive, other process -> StateForeign (a recycled pid: the dump
//     is a snapshot of a process that no longer
//     exists, and the live holder belongs to
//     somebody else — this reader does not
//     touch it);
//   - start ticks unreadable        -> StateForeign ("I could not tell" must
//     never authorise a live claim; the same
//     conservative one-way rule as the sandbox
//     reclaimer).
func dumpIdentityVerdict(record DumpRecord) (State, string) {
	if record.PID <= 0 || record.Started.IsZero() {
		return StateUnreadable, "the dump's record names no pid/started stamp, so its surface cannot be identified"
	}
	procPath := filepath.Join(procRoot, strconv.Itoa(record.PID))
	if _, err := os.Stat(procPath); err != nil {
		if os.IsNotExist(err) {
			return StateExited, ExitedNotice
		}
		return StateForeign, fmt.Sprintf("cannot read %s: %v", procPath, err)
	}
	stat, err := os.ReadFile(filepath.Join(procPath, procStatName))
	if err != nil {
		return StateForeign, fmt.Sprintf("cannot read the process stat for pid %d: %v", record.PID, err)
	}
	ticks, err := procname.StatStartTime(string(stat))
	if err != nil {
		return StateForeign, fmt.Sprintf("pid %d carries no readable start ticks: %v", record.PID, err)
	}
	if !startStampsMatch(processStartTime(ticks), record.Started) {
		return StateForeign, fmt.Sprintf("pid %d is alive but is NOT the surface that wrote this dump (start ticks %d is %s, the dump says %s): the pid has been recycled",
			record.PID, ticks, processStartTime(ticks).UTC().Format(time.RFC3339),
			record.Started.UTC().Format(time.RFC3339))
	}
	if cmd, err := os.ReadFile(filepath.Join(procPath, procCmdlineName)); err == nil && !cmdlineIsGopi(cmd) {
		return StateForeign, fmt.Sprintf("pid %d is the writing incarnation but its command is not a gopi surface", record.PID)
	}
	return StateLive, ""
}

// The /proc paths and names this reader needs, spelled once (Rule 1b).
const (
	procRoot        = "/proc"
	procStatName    = "stat"
	procCmdlineName = "cmdline"
)

// dumpBoolWordOn is the dump record's own spelling of a true switch; the sink
// renders it via boolWord, and this reader compares against the same word.
const dumpBoolWordOn = "on"

// dumpStartSkew is how far apart two renderings of the SAME start instant may
// be and still be treated as the same instant. It is not slop: the instant is
// derived twice from different observations — the writer records it while the
// process starts, the reader derives it again from /proc/<pid>/stat field 22
// against /proc/uptime — and the two derivations share neither the clock read
// nor the uptime's own 10 ms resolution. Anything wider than this window would
// stop separating a recycled pid (minutes to hours away) from an identity match,
// which is the one thing this check exists to do.
const dumpStartSkew = 2 * time.Second

// startStampsMatch reports whether a derived start instant and a recorded one
// name the same moment, within dumpStartSkew.
func startStampsMatch(derived, recorded time.Time) bool {
	if derived.IsZero() || recorded.IsZero() {
		// Neither side could be read: "I cannot tell" is NOT a match. The
		// caller turns this into the foreign verdict, the conservative
		// direction (an unprovable liveness claim must never authorise calling
		// a dump live).
		return false
	}
	delta := derived.Sub(recorded)
	if delta < 0 {
		delta = -delta
	}
	return delta <= dumpStartSkew
}

// cmdlineIsGopi reports whether a /proc/<pid>/cmdline belongs to a gopi
// process. It is CORROBORATION, never the identity: the (pid, ticks) pair
// already decided that, and a build under test (a `go test` binary driving the
// surface) is a legitimate gopi surface whose argv0 is not the word "gopi".
// Corroboration that rejected it would make the reader unusable in its own
// test suite, so the check only refuses a command line that names a clearly
// different program.
func cmdlineIsGopi(raw []byte) bool {
	if len(raw) == 0 {
		return true // no readable cmdline: nothing to contradict the ticks match
	}
	argv0 := strings.SplitN(strings.TrimRight(string(raw), "\x00"), "\x00", 2)[0]
	base := filepath.Base(argv0)
	if strings.Contains(base, gopiProcessWord) {
		return true
	}
	// A go-built test binary is `agent.test` / `uiclient.test`; they run the
	// same code, so they are not a foreign program.
	return strings.HasSuffix(base, goTestBinarySuffix)
}

const (
	// gopiProcessWord is the substring every gopi executable's name carries.
	gopiProcessWord = "gopi"
	// goTestBinarySuffix is the tail of a compiled Go test binary's name.
	goTestBinarySuffix = ".test"
)

// processStartTime converts a starttime tick count (field 22 of
// /proc/<pid>/stat) into a wall-clock instant, so it can be compared against
// the dump record's own `started=` stamp.
//
// The conversion is boot = now - uptime, then start = boot + ticks/HZ. It is
// the SAME binding the surface makes when it writes that stamp (cmd/agent's
// ui_state arm builds the dump identity from this function), so the two sides
// can only agree if there is one conversion — a second one would drift by the
// rounding and make every dump read as a recycled pid.
func processStartTime(ticks uint64) time.Time {
	up, err := uptimeSeconds()
	if err != nil {
		// No uptime to read: the identity check cannot be made, so it must not
		// be faked. Returning the zero time makes every comparison fail, which
		// is the conservative direction (a dump then reads as foreign, never as
		// live on an unproven claim).
		return time.Time{}
	}
	boot := time.Now().Add(-time.Duration(up * float64(time.Second)))
	return boot.Add(time.Duration(float64(ticks) / clockTicksPerSec * float64(time.Second)))
}

// uptimeSeconds reads the host uptime (first field of /proc/uptime).
func uptimeSeconds() (float64, error) {
	raw, err := os.ReadFile(filepath.Join(procRoot, procUptimeName))
	if err != nil {
		return 0, fmt.Errorf("cannot read %s: %w", filepath.Join(procRoot, procUptimeName), err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, fmt.Errorf("%s carries no fields", filepath.Join(procRoot, procUptimeName))
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("%s: uptime %q is not a number: %w", filepath.Join(procRoot, procUptimeName), fields[0], err)
	}
	return secs, nil
}

// ProcessStartTime reports the wall-clock start instant of pid, in the SAME
// binding processStartTime converts recorded ticks into. A surface uses it when
// it writes its dump identity, so the dump it produces reads back as ITS OWN
// incarnation rather than as a recycled pid.
func ProcessStartTime(pid int) (time.Time, error) {
	stat, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), procStatName))
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot read the stat of pid %d: %w", pid, err)
	}
	ticks, err := procname.StatStartTime(string(stat))
	if err != nil {
		return time.Time{}, err
	}
	started := processStartTime(ticks)
	if started.IsZero() {
		return time.Time{}, fmt.Errorf("cannot place pid %d's start on the wall clock (uptime unreadable)", pid)
	}
	return started, nil
}

// procUptimeName is the /proc uptime file; clockTicksPerSec is the kernel clock
// tick rate the starttime field counts in (USER_HZ, 100 on every Linux the
// product targets — the same value internal/tools' procStartTimeDisplay uses).
const (
	procUptimeName   = "uptime"
	clockTicksPerSec = 100
)

// SessionDirOfDump returns the session DIRECTORY a dump belongs to, and whether
// it found one. It is how the compare answers "which session does this surface
// belong to?" — the question the caller already answered by naming a session on
// its own command line.
//
// WHY IT IS A WALK AND NOT A FIXED DEPTH. A dump is written into the directory
// tree of the session it observes, but HOW DEEP it sits is the layout's
// business, and the layouts differ:
//
//	<sessions>/<id>/session.jsonl        bare session → the dump sits beside it
//	<sessions>/<id>/frames/chat-screen.dump           one artifact subdir
//	<sessions>/<task-id>/<uuid>.jsonl    task run    → one dir up
//	<sessions>/<task-id>/evidence/frames/<dump>       … two, three
//
// The old compare hard-coded `<dir>/session.jsonl` and `<dir>/../session.jsonl`
// — the first two rows only — so a dump written under the layout the TASK
// MACHINERY ITSELF USES derived nothing and the verb refused to check the very
// surface the caller was looking at (measured 2026-09-27: the chat dump of a
// task run, its run-uuid JSONL one directory up, unreadable to the compare).
//
// The walk ascends at most sessionDirWalkLevelsMax levels, and at each level
// asks internal/sessionlayout for the record — the package that owns "what is a
// session record in this tree" — so an artifact subdirectory, a legacy flat
// file and a task-run file are all classified by ONE rule rather than by a
// second, narrower one here. The FIRST level holding a record wins: the nearest
// session to the dump is the one that painted it.
//
// It stops at the sessions root: the search is "which session owns this file",
// never "find me a session somewhere", and a walk that left the tree could
// answer with an unrelated profile's record.
func SessionDirOfDump(dumpPath string) (string, bool) {
	if strings.TrimSpace(dumpPath) == "" {
		return "", false
	}
	dir := filepath.Dir(dumpPath)
	root, rootErr := gopiSessionsRoot()
	for level := 0; level < sessionDirWalkLevelsMax; level++ {
		if _, ok := sessionlayout.RecordInDir(dir); ok {
			return dir, true
		}
		if rootErr == nil && samePath(dir, root) {
			// The root is a directory of session DIRS: it is not itself a
			// session, and going above it would look for a record outside the
			// tree this dump belongs to.
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
	return "", false
}

// gopiSessionsRoot resolves <gopi dir>/sessions — the boundary of the walk
// above. It is resolved through config (the package that owns the profile path)
// and the sessions dir NAME is config's own constant (Rule 13: never a
// re-typed spelling of "sessions").
func gopiSessionsRoot() (string, error) {
	return config.SessionsRoot()
}

// samePath compares two paths as locations, tolerating the link/trailing-slash
// spellings a caller may pass. It falls back to a textual comparison when either
// side cannot be resolved, because a walk boundary is not worth failing a read
// over.
func samePath(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// SelectBySession picks the ONE candidate whose dump belongs to the named
// session.
//
// IT IS THE DISAMBIGUATOR THE REFUSAL LACKS. `--pid` answers "which surface did
// I mean" for a caller who knows the pid; a caller holding a session — an agent
// asking about its OWN surface, or anyone who just read the session id — has no
// equivalent, so a verb with twelve exited dumps on hand refused every compare
// even though the caller had already said which session it meant. The session
// IS the surface's identity for that caller: a surface writes its dump into the
// session directory of the run that produced it, so "this dump belongs to the
// session I named" is a determinate fact about the artifact, not a guess.
//
// Matching is by session DIRECTORY (not by parsing the operand into an id and
// comparing ids) because that is the one thing both sides can answer without a
// second resolver: the caller names a session file, the candidate knows where it
// was written.
//
// A NAME THAT MATCHES NO CANDIDATE IS NOT AN EXCLUSION. `--session` has two jobs
// in this verb and they must not be confused: it DISAMBIGUATES (which of these
// dumps did you mean) and it OVERRIDES the authority (check this dump against a
// different transcript than its own). The second job is documented and older, so
// a name that identifies no candidate must not veto the check: with exactly ONE
// candidate on hand it is selected — the caller is overriding, not choosing — and
// with SEVERAL the refusal names the sessions the dumps on hand actually belong
// to, which is the fact a caller who mistyped (or whose dump is elsewhere) needs
// to see. Rule 15 in both directions: never a silent substitution, never a
// refusal that hides why.
func SelectBySession(cands []Candidate, session string) (Selection, error) {
	sel := Selection{All: cands}
	if strings.TrimSpace(session) == "" {
		return sel, ErrNoCandidates
	}
	dir := sessionlayout.RecordContainer(session)
	if dir == "" || dir == "." {
		return sel, fmt.Errorf("cannot tell which session directory %q names: pass the path of the session record (e.g. <sessions>/<id>/%s) or its id, which the verb resolves first", session, sessionlayout.RecordName)
	}
	want := filepath.Clean(dir)
	var hits []Candidate
	for _, c := range cands {
		candDir, ok := SessionDirOfDump(c.Path)
		if !ok {
			continue
		}
		if filepath.Clean(candDir) == want {
			hits = append(hits, c)
		}
	}
	switch len(hits) {
	case 1:
		sel.Candidate = hits[0]
		return sel, nil
	case 0:
		// No candidate belongs to it: the override case. One dump on hand is one
		// screen, exactly as SelectLive would answer.
		if len(cands) == 1 {
			sel.Candidate = cands[0]
			return sel, nil
		}
		return sel, &NoDumpForSessionError{
			Files: knownDumpFileList(),
			Want:  want,
			Have:  candidateSessionList(cands),
		}
	}
	sel.Ambiguous = hits
	return sel, &AmbiguityError{Candidates: hits}
}

// candidateSessionList describes the candidates a --session matched none of, by
// the session directory each belongs to — the fact that would have matched, so
// the caller can see the spelling it is missing rather than only that it missed.
func candidateSessionList(cands []Candidate) string {
	if len(cands) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(cands))
	for _, c := range cands {
		dir, ok := SessionDirOfDump(c.Path)
		if !ok {
			dir = filepath.Dir(c.Path)
		}
		parts = append(parts, dir)
	}
	return strings.Join(parts, ", ")
}

// Candidates scans the given roots for dump files and reads each one. Roots
// that do not exist are skipped silently (a fresh profile has no session
// directories yet — that is not an error, and reporting it as one would make
// the common case noisy). The returned "truncated" flag reports that
// dumpMaxCandidates bounded the scan, so a caller can say the list is partial
// rather than presenting it as complete.
func Candidates(roots []string, now time.Time) (cands []Candidate, truncated bool, err error) {
	paths := []dumpFileRef{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		if _, statErr := os.Stat(root); statErr != nil {
			continue
		}
		found, cut := findDumpFiles(root)
		paths = append(paths, found...)
		truncated = truncated || cut
	}
	for _, ref := range paths {
		cand, readErr := ReadDump(ref.path)
		if readErr != nil && cand.State == "" {
			// The file vanished between the walk and the read, or could not be
			// opened: recorded as an unreadable candidate rather than dropped,
			// because a dump that exists and cannot be read is a fact about the
			// observation, not an absence of one (Rule 15).
			cand = Candidate{Path: ref.path, Surface: ref.surface, State: StateUnreadable, Reason: readErr.Error()}
		}
		cands = append(cands, cand)
	}
	sortCandidates(cands)
	if len(cands) == 0 {
		return nil, truncated, ErrNoCandidates
	}
	return cands, truncated, nil
}

// findDumpFiles walks one root (bounded by dumpScanDepth and dumpMaxCandidates)
// and returns every file that names a KNOWN surface's dump (DumpFileNames: the
// ui artifact and the chat artifact), each paired with the surface it belongs
// to. A file whose name no surface owns is not a candidate.
func findDumpFiles(root string) (found []dumpFileRef, cut bool) {
	var out []dumpFileRef
	base := root
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable subtree is skipped, not fatal to the scan
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr == nil && rel != "." {
			if depth := len(strings.Split(rel, string(os.PathSeparator))); depth > dumpScanDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			// Prune the runtime queue entries that belong to OTHER surfaces'
			// classes: a dump can only ever be under the ui-session class or a
			// session directory, and walking the rest is pure cost.
			if name := d.Name(); strings.HasPrefix(name, dumpRuntimeDirPrefix) && !strings.Contains(name, dumpUISessionClass) {
				return filepath.SkipDir
			}
			return nil
		}
		surface, known := SurfaceForDumpFile(d.Name())
		if !known {
			// The machine-checkable twin is deliberately NOT a candidate: it is
			// the same observation in another rendering, and offering both
			// would make every ambiguity check see double. A file that names no
			// surface is not a dump at all.
			return nil
		}
		out = append(out, dumpFileRef{path: path, surface: surface})
		if len(out) >= dumpMaxCandidates {
			return filepath.SkipAll
		}
		return nil
	}) //stylegate:ignore listing the dump directory for a diagnostic; an unreadable entry is omitted and the caller reports what it could read
	return out, len(out) >= dumpMaxCandidates
}

// sortCandidates orders candidates so a caller's output is stable: live first
// (the surface a reader most likely wants), then by descending pid — the newest
// incarnation of a respawned surface has the larger pid.
func sortCandidates(cands []Candidate) {
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && candidateLess(cands[j], cands[j-1]); j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}
}

func candidateLess(a, b Candidate) bool {
	if (a.State == StateLive) != (b.State == StateLive) {
		return a.State == StateLive
	}
	if a.Record.PID != b.Record.PID {
		return a.Record.PID > b.Record.PID
	}
	return a.Path < b.Path
}

// Selection is the verdict of choosing ONE candidate out of a scan.
type Selection struct {
	// Candidate is the chosen dump (zero when the verdict is an error).
	Candidate Candidate
	// All is every candidate the scan found, so a refusal can LIST them.
	All []Candidate
	// Ambiguous holds the live surfaces that made the verdict a refusal; it is
	// non-empty exactly when the returned error is an *AmbiguityError.
	Ambiguous []Candidate
	// Truncated reports that the scan hit its candidate bound.
	Truncated bool
}

// AmbiguityError is the refusal when more than one surface could be the
// target. It LISTS the candidates: a caller told only "ambiguous" cannot
// resolve it, while a caller told the pids and targets can pass --pid.
//
// The set is NOT always live: SelectLive refuses over the live candidates when
// several are live, but over ALL of them when none is (several dead surfaces
// are still several screens). The headline therefore states the composition of
// the set it actually carries — see Error.
type AmbiguityError struct {
	// Candidates are the surfaces that matched.
	Candidates []Candidate
}

func (e *AmbiguityError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s could be the target; refusing to guess — pass --pid (or --list) to choose:",
		len(e.Candidates), ambiguitySurfaceClause(e.Candidates))
	for _, c := range e.Candidates {
		fmt.Fprintf(&b, "\n  pid=%d target=%s started=%s dump=%s",
			c.Record.PID, c.Record.Target, c.Record.Started.UTC().Format(time.RFC3339), c.Path)
	}
	return b.String()
}

// ambiguitySurfaceClause is the headline's subject: the count's noun plus the
// state wording the candidates THEMSELVES justify.
//
// It is derived, never typed: the refusal used to carry one fixed adjective
// ("N live gopi surfaces could be the target"), which is exactly right for a
// live pair and FALSE for a pair of dead ones — whose very next lines read
// `state=exited`. A headline that contradicts its own listing costs the caller
// the work of reconciling the two, and the fix is to say what the set is:
//
//   - every candidate in the same state -> that state's own word ("live gopi
//     surfaces", "exited gopi surfaces") — a uniform live pair keeps the
//     sentence callers quote;
//   - a mix                            -> the composition, counted per state
//     ("gopi surfaces of mixed state (exited 1, foreign 1)"), because naming
//     any single state would be a claim about the others.
//
// The state words come from the State constants (Rule 1b); "live" is not
// special-cased here, it falls out of StateLive like every other verdict.
func ambiguitySurfaceClause(cands []Candidate) string {
	if len(cands) == 0 {
		// A refusal that lists nothing has no composition to state (SelectLive
		// never builds one). It says what it holds instead of inventing a
		// state word for an empty set.
		return fmt.Sprintf("%s surfaces", gopiProcessWord)
	}
	if word, uniform := ambiguityUniformState(cands); uniform {
		return fmt.Sprintf("%s %s surfaces", word, gopiProcessWord)
	}
	return fmt.Sprintf("%s surfaces of mixed state (%s)", gopiProcessWord, ambiguityStateCounts(cands))
}

// ambiguityUniformState reports the one state every candidate carries, and
// whether there was such a state at all.
func ambiguityUniformState(cands []Candidate) (State, bool) {
	first := cands[0].State
	for _, c := range cands[1:] {
		if c.State != first {
			return "", false
		}
	}
	return first, true
}

// ambiguityStateCounts renders the composition of a mixed set as
// "<state> <n>" pairs, in the order the (sorted) candidate list presents them —
// so the headline counts the same surfaces the listing below it shows, in the
// same order.
func ambiguityStateCounts(cands []Candidate) string {
	order := make([]State, 0, len(cands))
	counts := map[State]int{}
	for _, c := range cands {
		if _, seen := counts[c.State]; !seen {
			order = append(order, c.State)
		}
		counts[c.State]++
	}
	parts := make([]string, 0, len(order))
	for _, s := range order {
		parts = append(parts, fmt.Sprintf("%s %d", s, counts[s]))
	}
	return strings.Join(parts, ambiguityCountSeparator)
}

// ambiguityCountSeparator glues the per-state counts of a mixed refusal.
const ambiguityCountSeparator = ", "

// SelectByPID picks the dump written by an explicit pid. It is the unambiguous
// route: the caller has said WHICH surface, so a match is answered directly and
// a miss is answered with the list of what IS there (never a fallback to
// "the only dump I found" — a silent substitution is how a caller ends up
// asserting on a screen it did not ask about).
func SelectByPID(cands []Candidate, pid int) (Selection, error) {
	sel := Selection{All: cands}
	for _, c := range cands {
		if c.Record.PID == pid {
			sel.Candidate = c
			return sel, nil
		}
	}
	return sel, fmt.Errorf("no %s dump written by pid %d (dumps on hand: %s)", DumpFileName, pid, candidatePIDList(cands))
}

// knownDumpFileList renders every artifact name, for a refusal that must say
// what it was looking for.
func knownDumpFileList() string {
	names := make([]string, 0, len(Surfaces()))
	for _, s := range Surfaces() {
		names = append(names, DumpFileNames()[s])
	}
	return strings.Join(names, ", ")
}

// FilterSurface narrows a candidate list to ONE surface's artifacts. want == ""
// keeps the list unchanged (the caller did not ask for a target).
//
// WHY THIS EXISTS. Two self-observing surfaces now write a dump each into the
// same session tree, so "which dumps are on hand" and "which surface do you
// mean" stopped being the same question: without a filter, a chat run beside a
// ui run turns every read into a false AMBIGUITY (two live dumps, one screen
// wanted). The filter is applied BEFORE the selection rule, so the refusal
// messages and the exit codes keep describing the set the caller asked about.
func FilterSurface(cands []Candidate, want Surface) []Candidate {
	if want == "" {
		return cands
	}
	out := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		if c.Surface == want {
			out = append(out, c)
		}
	}
	return out
}

// ParseSurface reads a --target value against the constants that own the
// spellings (Rule 13): the accepted words are the Surface values, never retyped
// by the CLI.
func ParseSurface(v string) (Surface, error) {
	for _, s := range Surfaces() {
		if string(s) == v {
			return s, nil
		}
	}
	return "", fmt.Errorf("unknown target %q (accepted: %s, %s)", v, SurfaceUI, SurfaceChat)
}

// SelectLive picks the ONE live surface, or refuses. The rule:
//
//   - exactly one live candidate       -> it;
//   - several live candidates          -> *AmbiguityError* (a REFUSAL that
//     lists them);
//   - none live, exactly one candidate -> that candidate, carrying its own
//     verdict, so the caller can report "exited"
//     rather than an empty screen;
//   - none live, several candidates    -> *AmbiguityError* over all of them
//     (several dead surfaces are still
//     several screens);
//   - nothing found                    -> ErrNoCandidates.
func SelectLive(cands []Candidate, truncated bool) (Selection, error) {
	sel := Selection{All: cands, Truncated: truncated}
	if len(cands) == 0 {
		return sel, ErrNoCandidates
	}
	live := []Candidate{}
	for _, c := range cands {
		if c.State == StateLive {
			live = append(live, c)
		}
	}
	switch {
	case len(live) == 1:
		sel.Candidate = live[0]
		return sel, nil
	case len(live) > 1:
		sel.Ambiguous = live
		return sel, &AmbiguityError{Candidates: live}
	case len(cands) == 1:
		sel.Candidate = cands[0]
		return sel, nil
	}
	sel.Ambiguous = cands
	return sel, &AmbiguityError{Candidates: cands}
}

// candidatePIDList renders the pids of a candidate list for an error message.
func candidatePIDList(cands []Candidate) string {
	if len(cands) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(cands))
	for _, c := range cands {
		parts = append(parts, strconv.Itoa(c.Record.PID))
	}
	return strings.Join(parts, ", ")
}

// ScanRoots builds the discovery roots for a profile directory and a scratch
// home. Each root is a place a self-observing surface's dump can legally land:
//
//   - <gopiDir>/sessions — the per-session directory of a surface that owns its
//     session (the dump sits beside session.jsonl). BOTH surfaces land here:
//     `gopi ui` writes ui-screen.dump, `gopi chat` writes chat-screen.dump, and
//     the scan matches either name (SurfaceForDumpFile);
//   - <scratchHome>/queue/queue-runtime-ui-session-<pid> — the scratch class a
//     session-less surface (`--server`) writes into.
//
// The scratch home is taken as an argument rather than read from
// internal/scratch, because internal/scratch imports this package: the caller
// that already knows the scratch home passes it (Rule 14 — one resolution, at
// the edge, not a second one here).
func ScanRoots(gopiDir, scratchHome string) []string {
	if scratchHome == "" {
		return []string{filepath.Join(gopiDir, config.SessionsDirName)}
	}
	queue := filepath.Join(scratchHome, dumpQueueDirName)
	roots := []string{
		filepath.Join(gopiDir, config.SessionsDirName),
		filepath.Join(queue, dumpRuntimeEntryName(dumpUISessionClass, 0)),
	}
	// The class-pid glob is what actually matches: the entry name carries the
	// writer's pid, which this reader cannot know in advance.
	if entries, err := os.ReadDir(queue); err == nil {
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, dumpRuntimeDirPrefix) && strings.Contains(name, dumpUISessionClass) {
				roots = append(roots, filepath.Join(queue, name))
			}
		}
	}
	return roots
}

// dumpRuntimeEntryName renders a runtime scratch entry name
// ("queue-runtime-<class>-<pid>"), the layout internal/scratch uses. It is here
// so this reader can NAME the entry it expects to find rather than only glob
// for it, and a pid of 0 renders the class-wide prefix.
func dumpRuntimeEntryName(class string, pid int) string {
	name := dumpRuntimeDirPrefix + class
	if pid <= 0 {
		return name + "-"
	}
	return name + "-" + strconv.Itoa(pid)
}

// ClassPID extracts the pid a runtime scratch entry name carries
// ("queue-runtime-ui-session-4711" -> 4711). The pid is the name's LAST field:
// the fields before it are the prefix and the class, which may themselves carry
// hyphens (the class "ui-session" does), so counting from the left would read a
// class word as a pid.
func ClassPID(entryName string) (int, bool) {
	fields := strings.Split(entryName, "-")
	last := fields[len(fields)-1]
	pid, err := strconv.Atoi(last)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}
