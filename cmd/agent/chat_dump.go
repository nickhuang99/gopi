package main

// chat_dump.go — `gopi chat`'s own screen dump: `--dump-screen[=PATH]` to pick
// where it goes, `--no-dump-screen` to decline it. The dump is ON BY DEFAULT
// (2026-10-01 — see the DEFAULT/COST note below the vocabulary constants): the
// CHAT surface's own self-observation.
//
// WHAT WAS MISSING. `gopi ui` (the BubbleTea TUI) could already write its
// interpreted screen state to a file an agent reads as text
// (internal/uiclient/dump.go + cli_ui.go). `gopi chat` — the agent's OWN line
// editor, the surface an agent IS while it runs — could not: the observer
// existed, was honest and was mounted on a DIFFERENT process. So the one screen
// the agent most needs to read (its answer band, its editing line, its status
// row) had no instrument, and the only route left was a pixel screenshot read
// back by a vision model (measured: ~55x the bytes, and it says nothing about
// cell coordinates).
//
// WHAT THIS IS, AND WHAT IT IS NOT. It is a THIRD WIRE, not a second observer:
// the same uiclient.ScreenDumper, its same 100ms throttle, its same
// temp-file+rename install, its same provenance record, its same eight blind
// spots and its same `frames=0` honesty. What is new here is only WHERE the
// bytes come from: the reserved chat layout's own paint stream.
//
// WHERE THE BYTES COME FROM (and why that is the right seam). The chat layout
// has exactly ONE writer on the terminal: the editor's read loop, which draws
// the answer band, the editing line, the status row and the menu. That writer's
// destination is an io.Writer the REPL hands it (chatInputWithDumper below tees
// it). Teeing THERE — the editor's own stream — rather than tapping the answer
// renderer has three consequences worth stating:
//
//  1. the dump is what the LAYOUT painted, chrome included: the band's committed
//     rows, the editing line, the status row's columns. An answer-only tap
//     would describe a screen the operator never saw;
//  2. it needs no new render layer and no new hook (Rule 3's constraint): the
//     region already writes absolute placements through this writer, so the
//     observer sees exactly the sequence the terminal received and interprets it
//     with the SAME interpreter the ui dump uses;
//  3. with no flag the writer is the ONE writer it always was — not a wrapper
//     whose identity someone has to reason about (the byte-identity contract).

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// The chat dump's flag and env vocabulary. Named once here and referenced by
// the parser, the usage page and the tests (Rule 1b) — a second spelling of
// "--dump-screen" is how a verb ends up documenting a flag it does not read.
const (
	// chatDumpFlag is `--dump-screen[=PATH]`, the SAME spelling `gopi ui`
	// takes: one capability, one flag name, two surfaces.
	chatDumpFlag = "--dump-screen"
	// chatNoDumpFlag is the OFF SWITCH: the dump is armed by DEFAULT, so the
	// capability needs a way to be declined, and declining must be as spellable
	// as asking (Rule 15 — a default nobody can turn off is not a default, it is
	// a decision taken from the operator).
	chatNoDumpFlag = cliverbs.NoDumpScreen
	// chatDumpEnv is the env switch: GOPI_CHAT_DUMP unset (or empty) leaves the
	// DEFAULT in place — the dump is armed; a falsey value (0/false/off/no/
	// disabled) TURNS IT OFF; GOPI_CHAT_DUMP=1 asks for a dump beside this run's
	// own artifacts; any other value IS the path. It exists for the same reason
	// the ui dump's does: the surface may be started by something that cannot add
	// a flag. The spelling is cliverbs.ChatDumpEnv (Rule 14): internal/agent
	// renders the same name in the tool's refusal, so there is one definition.
	chatDumpEnv = cliverbs.ChatDumpEnv
	// chatDumpFlagSep is the `--flag=value` separator (Rule 13: the ui verb's
	// one spelling is `uiFlagValueSep`, and this is the chat verb's — both are
	// the same character, neither is typed inline at a comparison).
	chatDumpFlagSep = "="
)

// chatDumpNotice is printed BEFORE the layout takes the terminal: after that,
// stderr belongs to the renderer and the path can no longer be reported. A dump
// nobody can find is indistinguishable from no dump.
const chatDumpNotice = "gopi chat: screen dump → %s (plain text; this surface's own interpreted state — no pixels)\n"

// chatDumpReplayNotice is the same announcement for a REPLAY, and it says the
// two facts a reader of that run needs: where the artifact is, and that the
// destination is not the recording. A plain chatDumpNotice would have been
// true but uninformative — and "the dump is somewhere outside the recording"
// is the whole point of the redirect (internal/replaymode; Rule 15: a write that
// was MOVED must say so, or "nothing was written" and "it was written where I
// did not look" read the same).
const chatDumpReplayNotice = "gopi chat: screen dump → %s (a replay writes nothing into the recording it re-runs, so this artifact lives in the chat's own scratch class, never beside the recording)\n"

// chatDumpPathNotice is the ONE spelling of "where the dump went", chosen once
// for the run. The caller prints the string; it does not decide which sentence
// applies (Rule 13 — a second `if replaymode.Active()` at the print site would
// be a second answer to a question the resolver already answered).
func chatDumpPathNotice(path string) string {
	if replaymode.Active() {
		return fmt.Sprintf(chatDumpReplayNotice, path)
	}
	return fmt.Sprintf(chatDumpNotice, path)
}

// chatDumpDirPattern names the scratch directory the resolver creates when no
// explicit path was given and the run has no session directory.
const chatDumpDirPattern = "chat-screen"

// chatDumpGeomNotice is the LOUD half of a fallback geometry: a run whose grid
// is not the layout's says so on stderr (Rule 15), because the artifact alone is
// not read by everyone holding the terminal. It is printed through the same
// notification seam the status publishes use, so it lands on its own row instead
// of tearing the layout.
const chatDumpGeomNotice = "screen dump: the grid's geometry is a FALLBACK — %s; read wraps and truncation in %s as the default geometry's, not the screen's\n"

// chatDumpReportFormat is the closing one-line record: what the self-observation
// produced, printed once the terminal is the operator's again. It carries the
// SAME facts the ui report does (path, twin, size, grid, installs, frames, blind
// spots) so a reader does not need two vocabularies for one capability.
const chatDumpReportFormat = "#gopi-chat-dump-report path=%s json=%s bytes=%d pid=%d grid=%dx%d geometry=%s dumps=%d frames=%d blind_spots=%d\n"

// chatDumpUnpaintedNotice is printed when the flag asked for a dump but the
// layout never painted: on the cooked path (--no-editor, a pipe) there is no
// band, so the dump stays an empty grid. Saying so is the difference between an
// artifact a reader can bound and one they must guess about (Rule 15 — the
// `frames=0` honesty the reader also carries).
// chatDumpNoPathWord is what the report prints in place of a path the run does
// not have: the sink can be armed as a memory-only observer (a declined dump
// beside an armed raw stream), and an empty field in a record line reads as a
// truncated line rather than as "there is no file".
const chatDumpNoPathWord = "-"

const chatDumpUnpaintedNotice = "gopi chat: no reserved layout is installed on this run, so the screen dump will stay an unpainted grid (frames=0) — the dump observes the answer band, and there is none here.\n"

// chatDumpBlankGridNotice is printed when the frozen grid is all-blank EVEN
// THOUGH the sink interpreted frames: the artifact's own diagnosis of the
// situation an agent most often misreads. It names the idle cause (measured:
// the chat layout paints only when the screen changes) and the remedy from the
// help text, so the reader of a blank dump is not left to conclude that the
// screen was empty.
const chatDumpBlankGridNotice = "gopi chat: the screen dump's grid is ALL-BLANK, and the sink did interpret frames — an idle layout paints only when the screen CHANGES, so this is the state before/without a painted change (measured: frames=3 dumps=1, byte-identical for 10s on an idle 117x26 pty). It is NOT evidence that the screen is empty; read it with `gopi ui_state --target chat` and see the --dump-screen help for the recipe.\n"

// WHY THE DUMP IS ARMED BY DEFAULT, and what it costs (the honest basis).
// MEASURED 2026-10-01 on a 117x26 pty (task 01a0f720, sibling epic 01a0f66a's
// measurement): a frame of this artifact is ~5.8 KB (5793 bytes), the sink
// writes at most once per dumpMinInterval = 100ms (internal/uiclient/dump.go)
// and the layout paints only when the screen CHANGES — a long run measured
// frames=18408 against dumps=2718, i.e. roughly ONE FILE WRITE PER SEVEN FRAMES
// (~1/7), and an idle chat freezes at dumps=1 and then writes nothing at all.
// So the cost is a throttled sample, not a firehose, and:
//
//   - ZERO new dependencies (Rule 35): the artifact is one file inside THIS
//     run's own session directory, written by the sink that already existed, at
//     a path the existing `gopi tool scratch-gc` sweep already collects;
//   - the default is what makes the self-observation loop reachable. Measured
//     (task 01a0e070): with the capability opt-in, `gopi ui_state --target chat`
//     answers "no dump found" — the loop existed, was documented and was never
//     armed, and the one screen an agent most needs to read (its own answer
//     band) therefore had no instrument. An observation capability that is off
//     until someone remembers a flag is a capability nobody has;
//   - the run says so: the dump's path is printed before the REPL takes the
//     terminal (chatDumpNotice), so a record of the operator's own screen is
//     never written silently.
//
// WHAT THE DEFAULT DOES NOT CHANGE: a run that declines the dump
// (`--no-dump-screen`, or GOPI_CHAT_DUMP=0/false/off) is BYTE-IDENTICAL to a run
// from before this capability existed — chatDumpWriter returns the caller's
// writer UNCHANGED when there is no sink, and that contract is asserted by
// construction, not by review.

// chatDumpOptions is the resolved dump request, parsed from the flag and the
// env (flag wins). It mirrors the ui verb's uiOptions fields for the same
// capability, deliberately: "off" and "on, path unknown" must not collapse into
// one empty string.
type chatDumpOptions struct {
	// path is the explicit destination, "" when the resolver must choose one.
	path string
	// requested records that this run's OWN command line or environment asked
	// for a dump — either the flag, the env, or the default below. It is the
	// EFFECTIVE decision, so the resolver arms unless disabled is set.
	requested bool
	// disabled records that the run was told to turn the dump OFF
	// (`--no-dump-screen`, or a falsey GOPI_CHAT_DUMP). It is the one state in
	// which no artifact is written and the paint stream is the one it always was.
	disabled bool
	// stated records that the dump was named EXPLICITLY on this run's command
	// line or environment. It is what the resume chain keys on: the DEFAULT is
	// not a statement, so it must not supersede a destination a resumed session
	// recorded (compare chatApplyResumeFlags' dumpStated).
	stated bool
}

// chatDumpDefault is the run's decision when nobody said anything: the dump is
// ARMED. It is a function (not a literal spread through the parse) so the one
// place that can answer "what happens by default?" is here.
func chatDumpDefault() chatDumpOptions {
	return chatDumpOptions{requested: true}
}

// chatDumpFromEnv reads the env switch. A run with NO GOPI_CHAT_DUMP gets the
// DEFAULT (armed, path resolved by the resolver); a value config.IsFalseValue
// accepts (`0`/`false`/`off`/`no`/`disabled`) TURNS IT OFF; `GOPI_CHAT_DUMP=1`
// (any value config.EnvBool accepts) asks for a dump at the resolver's chosen
// path; any OTHER non-empty value IS the path.
//
// Precedence: the flag wins over this (the caller overwrites the whole struct).
func chatDumpFromEnv() chatDumpOptions {
	v := strings.TrimSpace(os.Getenv(chatDumpEnv))
	if v == "" {
		return chatDumpDefault()
	}
	if config.IsFalseValue(v) {
		return chatDumpOptions{disabled: true, stated: true}
	}
	if config.EnvBool(chatDumpEnv) {
		return chatDumpOptions{requested: true, stated: true}
	}
	return chatDumpOptions{path: v, requested: true, stated: true}
}

// dumped reports whether this run should write an artifact. It is the ONE
// predicate the resolver and the arm block read, so "off" has a single
// definition (Rule 13) instead of two spellings that can drift.
func (o chatDumpOptions) dumped() bool { return o.requested && !o.disabled }

// parseChatDumpArg handles ONE argv element in the flag's own vocabulary and
// reports whether it was a dump word — the ARM flag (`--dump-screen`,
// `--dump-screen=PATH`) or the OFF switch (`--no-dump-screen`).
//
// The OFF switch takes NO value: it names the absence of an artifact, so
// `--no-dump-screen=x` is refused BY NAME rather than silently read as "off"
// (Rule 15 — a flag that quietly discards its own value teaches a caller
// something false about the command line).
//
// The JOINED form is the only one accepted WITH a path, and the separated form is
// refused BY NAME (the same contract `gopi ui` keeps, and Rule 15's reason: the
// message must name the fix). It matters more here than there: this verb's
// remaining positional argument is a session seed (a raw-log path or a session
// id), so a separated path would be silently read as "continue that session" —
// a flag that quietly did something else is exactly what the refusal prevents.
func parseChatDumpArg(a string, next string, hasNext bool) (opts chatDumpOptions, matched bool, err error) {
	if a == chatNoDumpFlag {
		if hasNext && next != "" && !strings.HasPrefix(next, chatFlagPrefix) {
			return chatDumpOptions{}, true, fmt.Errorf(
				"%s takes no value (it turns the screen dump OFF); write %s alone, or %s=%s to choose a destination",
				chatNoDumpFlag, chatNoDumpFlag, chatDumpFlag, next)
		}
		return chatDumpOptions{disabled: true, stated: true}, true, nil
	}
	if strings.HasPrefix(a, chatNoDumpFlag+chatDumpFlagSep) {
		// The joined form is refused TOO, rather than falling through to the
		// generic unknown-flag message: `--no-dump-screen=x` is a caller who
		// believes the off switch takes a value, and the honest answer names the
		// flag that does (Rule 15 — "unknown flag" would send them looking for
		// the right spelling in a list that already contains it).
		return chatDumpOptions{}, true, fmt.Errorf(
			"%s takes no value (it turns the screen dump OFF); write %s alone, or %s=%s to choose a destination",
			chatNoDumpFlag, chatNoDumpFlag, chatDumpFlag, strings.TrimPrefix(a, chatNoDumpFlag+chatDumpFlagSep))
	}
	if a != chatDumpFlag && !strings.HasPrefix(a, chatDumpFlag+chatDumpFlagSep) {
		return chatDumpOptions{}, false, nil
	}
	inline := ""
	if k := strings.Index(a, chatDumpFlagSep); k > 0 {
		inline = a[k+1:]
	}
	if inline == "" && hasNext && !strings.HasPrefix(next, chatDumpFlagSep) && !strings.HasPrefix(next, "-") {
		return chatDumpOptions{}, true, fmt.Errorf(
			"%s takes its path joined — write %s=%s (or use the bare %s for a dump beside this run's own artifacts)",
			chatDumpFlag, chatDumpFlag, next, chatDumpFlag)
	}
	if inline == "" && hasNext && next == chatDumpFlag {
		// A bare flag followed by the flag again is not a path; let the next
		// iteration parse it (the operator asked twice — idempotent, not an
		// error).
		return chatDumpOptions{requested: true, stated: true}, true, nil
	}
	return chatDumpOptions{path: inline, requested: true, stated: true}, true, nil
}

// chatResolveDumpPath decides where the dump goes, and fails LOUDLY rather than
// silently dropping the capability (Rule 15). Precedence: the explicit path
// (flag, then env) wins; otherwise the run's OWN session directory — beside
// session.jsonl, i.e. INSIDE the sessions tree `gopi ui_state` scans, so the
// dump is discoverable by the verb that reads dumps; otherwise a scratch
// directory of the chat's own class (never /tmp — Rule 22).
//
// A REPLAY GOES TO SCRATCH, AND ONLY TO SCRATCH. The session-directory branch
// below is `RecordContainer(ag.SessionLog.Path())/chat-screen.dump` — and while
// a replay is armed, `ag.SessionLog` IS THE RECORDING'S OWN LOGGER when the agent
// was handed one, so that branch would write the replay's observation INTO the
// recording directory it is replaying. The guarantee this verb prints
// ("a replay writes nothing at all: it reads the recording in place") would then
// be false, and false in the worst way: the write stays invisible until someone
// hashes the recording. So the flag is asked FIRST, before any consumer of
// ag.SessionLog — the ordering replaymode's own doc requires ("a flag armed after
// the writer it is meant to stop has already run is prose").
//
// It is a REDIRECT and not a refusal: the dump is this surface's own
// visualization product, and a replay is precisely a run whose picture someone
// wants to look at. Nothing about the artifact is lost — it moves outside the
// recording tree, into the chat's own scratch class (Rule 22/35) — and the move
// is announced (chatDumpReplayNotice), because a dump nobody can find is
// indistinguishable from no dump.
func chatResolveDumpPath(opts chatDumpOptions, ag *agent.Agent) (string, error) {
	if !opts.dumped() {
		return "", nil
	}
	// ASKED BEFORE THE EXPLICIT PATH AND BEFORE ag.SessionLog. An explicit path
	// can also name the recording (a shell glob, a resumed command line), and
	// this resolver cannot tell that apart from a destination the operator
	// chose on purpose — so the replay's answer does not depend on it.
	if replaymode.Active() {
		return chatReplayDumpPath()
	}
	if opts.path != "" {
		if dir := filepath.Dir(opts.path); dir != "" && dir != "." {
			if err := scratch.MkdirAllWithSuiteRunOwnerPerm(dir, dumpDirPerm); err != nil {
				return "", fmt.Errorf("cannot create the dump directory %s: %w", dir, err)
			}
		}
		return opts.path, nil
	}
	if ag != nil && ag.SessionLog != nil {
		return filepath.Join(sessionlayout.RecordContainer(ag.SessionLog.Path()), uiclient.ChatDumpFileName), nil
	}
	dir, err := scratch.MkdirTemp(scratch.RuntimeClassUISession, chatDumpDirPattern)
	if err != nil {
		return "", fmt.Errorf("no destination is available for the screen dump: %w", err)
	}
	return filepath.Join(dir, uiclient.ChatDumpFileName), nil
}

// chatReplayDumpPath is the ONE destination a replay's dump may have: a
// directory of the chat's own scratch class, never the recording's own session
// directory. It is the same scratch shape the ordinary resolver falls back to
// when a run has no session, so a replay adds NO new artifact class and no new
// dependency (Rule 35): `gopi tool scratch-gc` already sweeps this class.
//
// WHY IT IS A SEPARATE FUNCTION AND NOT AN IF INSIDE THE RESOLVER: the
// exhaustiveness gate (chat_replay_dump_catalogue_test.go) asserts this
// function's answer is outside the recording tree, and a reader can then check
// ONE name rather than re-deriving the resolver's precedence. The failure mode
// is also different: this one must never fall back to the operator's sessions
// root, while the ordinary one must never leave it.
func chatReplayDumpPath() (string, error) {
	dir, err := scratch.MkdirTemp(scratch.RuntimeClassUISession, chatDumpDirPattern)
	if err != nil {
		return "", fmt.Errorf("a replay's screen dump needs a scratch destination, and none could be created (a dump is never written into the recording's session directory): %w", err)
	}
	return filepath.Join(dir, uiclient.ChatDumpFileName), nil
}

// chatArmDump builds the chat surface's dump sink. cols/rows are the geometry the
// CHAT REPL measured at arm time (a first hint); the AUTHORITATIVE geometry
// arrives from the LAYOUT itself once the reserved region is installed
// (chatGeomObserver → ScreenDumper.SetGeometry), because the sink re-interprets
// the stream that layout paints: a grid built from any other number would read
// the layout's own wraps as truncation.
//
// It also attaches the RAW append leg (sgr) to the SAME sink, so the two
// artifacts of one run share one Write call and therefore one ORDER: a dump
// frame and the bytes behind it cannot disagree about what came first. A nil
// sgr is the off state (see chat_sgr_stream.go) and changes nothing.
func chatArmDump(path string, cols, rows int, tools bool, onFallback func(), sgr *uiclient.SgrStream) (*uiclient.ScreenDumper, error) {
	if path == "" && sgr == nil {
		// NEITHER artifact was asked for: no sink at all, so the editor's paint
		// stream is the one writer it always was.
		return nil, nil
	}
	// A sink with no PATH is legal (DumpConfig.Path: "empty = no file,
	// snapshot only"): that is exactly the shape a run gets when it declines the
	// screen dump (`--no-dump-screen`) but asks for the RAW stream — the raw leg
	// does not depend on the interpreted artifact, and refusing to arm here
	// would make one switch silently disable the other.
	return uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: path,
		Sgr:  sgr,
		Cols: cols,
		Rows: rows,
		// The authority behind the constructor's numbers: the REPL measured a
		// descriptor. It stays `terminal` only while the layout agrees; the
		// layout's own install rewrites it to `layout` (SetGeometry).
		Geometry:          uiclient.GeometrySourceTerminal,
		OnGeometryChanged: onFallback,
		Identity: uiclient.DumpIdentity{
			// The DISCRIMINATOR: this artifact says `chat` in its record's own
			// prefix and in its JSON twin, so a reader holding both dumps never
			// has to guess which surface it is looking at.
			Surface: uiclient.SurfaceChat,
			PID:     os.Getpid(),
			Target:  chatDumpTarget(tools),
			Tools:   tools,
			// THE HINT ONLY. These are the numbers the caller measured before the
			// layout existed; they are recorded here so a reader of the artifact
			// never sees a zero, and they are OVERWRITTEN by the first
			// SetGeometry. Do not read them as "the model geometry of this run":
			// the chat layout reads its width from the tty at every repaint and
			// reports it through chatGeomObserver, so after a resize the record's
			// terminal= and model= follow the grid and all three are the same
			// number — because the sink MOVES them, not because the two were
			// equal by construction (measured 2026-11-09: a run resized
			// 121x24 → 101x22 reported grid=101x22 between terminal=121x24 and
			// model=121x24, the constructor's numbers, frozen).
			ColsRows:         [2]int{cols, rows},
			TerminalColsRows: [2]int{cols, rows},
			Started:          uiDumpStartStamp(),
		},
	})
}

// chatRecordCapability records the run's colour-capability decision on the dump
// sink, so the artifact answers "why are these cells coloured THESE colours?"
// beside the cells themselves. It is the ONE translation from lineedit's
// capability report to uiclient's record fields, so the record's vocabulary is
// spelled once (Rule 13).
//
// A nil sink (the dump declined with --no-dump-screen, or off by GOPI_CHAT_DUMP)
// is not an error: the run simply has no artifact
// to annotate, and the report is dropped rather than accumulated.
func chatRecordCapability(d *uiclient.ScreenDumper, input *chatInput) {
	if d == nil || input == nil {
		return
	}
	capability := input.Capability()
	report := uiclient.CapabilityReport{
		Source:        capability.DepthSource(),
		Depth:         lineedit.DepthName(capability.Depth()),
		DeclaredDepth: lineedit.DepthName(capability.DeclaredDepth()),
		Palette:       lineedit.PaletteNameFor(input.Palette()),
	}
	if m := input.Measurement(); m.Answered || m.Bytes > 0 {
		report.Probe = m.Summary()
	}
	d.SetCapabilityReport(report)
	if err := d.Settle(); err != nil {
		// The artifact is the reason the field exists: a record nobody could
		// write must not look like one that carried the decision (Rule 15).
		fmt.Fprintf(os.Stderr, "[gopi] screen dump: %v\n", err)
	}
}

// chatDumpTarget is the record's `target=` field for a chat surface. A chat run
// has no server origin — the honest answer is the model endpoint class the run
// resolved, else the plain word for "this process, no external target". It is
// never left empty: an empty target reads as a parse failure, and the field's
// whole job is to answer "which run is this?" when two are on hand.
func chatDumpTarget(tools bool) string {
	mode := chatModePlain
	if tools {
		mode = chatModeTools
	}
	return chatTargetSelf + chatDumpTargetSep + mode
}

// The chat dump target's vocabulary: a self-describing target with the run's
// mode, because "where does gopi chat's screen live" has no address — the
// surface is the process itself.
const (
	chatTargetSelf    = "self"
	chatDumpTargetSep = ":"
	chatModePlain     = "chat"
	chatModeTools     = "chat-tools"
)

// chatGeomObserver is the sink's geometry binding, expressed as the function the
// editor calls whenever the reserved layout's geometry is (re)installed: on
// install, on every resize that the layout followed, and — with ok=false — when
// the layout could NOT be installed or had to be taken down.
//
// WHY THE EDITOR, AND NOT A SECOND PROBE. The sink re-interprets the layout's
// paint stream, so its grid is faithful only if it is built at the width the
// layout wrapped for. The two numbers used to come from two different
// mechanisms (the sink's caller probed os.Stderr while the editor measures
// os.Stdin through lineedit.Terminal.Size), so a probe failure on one descriptor
// left the grid at a default while the layout drew at the real width — measured
// 2026-09-26 on a 117-column pty: `grid=80x24` over frames the layout had
// painted at 117, i.e. correct wraps re-read as truncation — fabricated
// corruption an agent cannot distinguish from real corruption. One source of
// truth is the region the layout was installed with, and the editor is its only
// owner.
//
// A nil sink, or a null observer, means the dump is not requested at all: no
// binding happens and no notice is printed.
func chatGeomObserver(d *uiclient.ScreenDumper) func(cols, rows int, ok bool) {
	if d == nil {
		return nil
	}
	return func(cols, rows int, ok bool) {
		if !ok {
			// No layout geometry exists: the sink must NOT let its constructed
			// default pass for a measured one.
			d.SetGeometry(uiclient.GeometrySourceFallback, 0, 0)
			return
		}
		d.SetGeometry(uiclient.GeometrySourceLayout, cols, rows)
	}
}

// chatDumpNoLayoutReason is the ONE sentence both loud-fallback paths use (the
// observer's and the cooked path's), so a reader who greps for it finds every
// run that dumped at a geometry no layout drew (Rule 13: one spelling).
const chatDumpNoLayoutReason = "no reserved layout is installed on this terminal"

// chatDumpFallbackNotice is the sink's loud-fallback hook: it prints the fact on
// stderr the moment a fallback geometry takes effect, through the same channel
// the run's other notices use. It fires from SetGeometry, i.e. AFTER the layout
// install has declined — at which point the console is still the operator's
// (the first paint happens later), so the sentence is readable and cannot tear a
// frame.
func chatDumpFallbackNotice() func() {
	return func() {
		fmt.Fprintf(os.Stderr, chatDumpGeomNotice, chatDumpNoLayoutReason, "the dump")
	}
}

// chatDumpWriter tees the editor's paint stream into the dump sink. It is the
// WHOLE wiring on the render side: the terminal still receives the same bytes
// (the sink always reports the full length and never errors into the render
// path), and with no sink the writer is returned UNCHANGED — so "no flag" is not
// "a slightly different writer", it is the same one.
//
// It also owns the ORDER the two consumers see: see chatPaintWriter below for
// the measured defect that made a bare io.MultiWriter here produce an artifact
// no run had drawn.
func chatDumpWriter(out io.Writer, d *uiclient.ScreenDumper) io.Writer {
	if d == nil {
		return out
	}
	return &chatPaintWriter{tee: io.MultiWriter(out, d)}
}

// chatPaintWriter serializes the paint stream while a dump sink rides it.
//
// WHY IT EXISTS (measured 2026-09-26). The chat paint stream has two painters:
// the turn goroutine's answer renderer (Begin/Delta/Finish → writeRow →
// region.commitScroll) and the editor's read loop (Tick's status repaint, the
// menu). With a sink installed the writer is an io.MultiWriter, whose Write
// fans the same bytes out to each of its writers SEQUENTIALLY — so two
// concurrent callers can reach the two consumers in different orders:
//
//	A: terminal(pA) ... B: terminal(pB), sink(pB) ... A: sink(pA)
//
// Each consumer then interprets a stream the other never saw. The sink's grid
// is the interpreted EFFECT of its own byte order, so the artifact held
// `▌ a b` / `▌ a   c` where the harness's replayed screen — the SAME recorded
// writes, replayed in RECORD order — held `▌ a b c` (measured: 2 of 120
// iterations, with 3 observed write-overlaps in the failing one).
//
// The lock makes one paint WRITE at a time; it is the seam the run already
// owns, and it changes nothing at all when no dump was asked for (the nil-sink
// path above returns the caller's writer UNCHANGED, byte-identical).
type chatPaintWriter struct {
	mu  sync.Mutex
	tee io.Writer
}

// Write hands one chunk to every consumer under the lock. A sink that fails
// still reports the full length (chatDumpWriter's contract: the observer must
// not be able to break the render path), which io.MultiWriter already does.
func (w *chatPaintWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tee.Write(p)
}

// chatReportDump prints what the self-observation produced, AFTER the layout has
// released the terminal. A run that wrote no dump says so — silence would read as
// success.
func chatReportDump(d *uiclient.ScreenDumper) {
	if d == nil {
		return
	}
	snap := d.Snapshot()
	geom, _, _ := d.Geometry()
	size := int64(-1)
	if d.Path() == "" {
		// A sink with NO file is the legal shape a run gets when it declines the
		// screen dump but keeps the RAW stream (chat_sgr_stream.go): the grid is
		// still kept in memory for this report, and there is no file to stat.
		// Saying so is cheaper than a stat on "" whose error would read as a
		// broken artifact (Rule 15).
		fmt.Fprintf(os.Stderr, chatDumpReportFormat,
			chatDumpNoPathWord, chatDumpNoPathWord, size, os.Getpid(), snap.Cols, snap.Rows, geom,
			d.Writes(), d.Frames(), len(snap.BlindSpots))
		return
	}
	if info, err := os.Stat(d.Path()); err == nil {
		size = info.Size()
	} else {
		fmt.Fprintf(os.Stderr, "gopi chat: cannot stat the screen dump %s: %v\n", d.Path(), err)
	}
	fmt.Fprintf(os.Stderr, chatDumpReportFormat,
		d.Path(), d.JSONPath(), size, os.Getpid(), snap.Cols, snap.Rows, geom,
		d.Writes(), d.Frames(), len(snap.BlindSpots))
	if geom == uiclient.GeometrySourceFallback {
		// The fallback is stated in the closing record's own word AND in a
		// sentence: a reader who quotes either one learns that this grid is not
		// the layout's (GeometryFallbackNotice carries the same fact for an
		// artifact reader).
		fmt.Fprintf(os.Stderr, "gopi chat: %s\n", uiclient.GeometryFallbackNotice)
	}
	if d.Frames() == uiclient.UnpaintedFrames {
		// The `frames=0` case is stated in words as well as in the record: the
		// grid is real and empty, and a reader must not take it for "the screen
		// is blank" (uiclient.UnpaintedNotice, the same honesty the reader of a
		// ui dump gets).
		fmt.Fprint(os.Stderr, chatDumpUnpaintedNotice)
		return
	}
	if uiclient.ScreenDumpAllBlank(snap) {
		// The measured chat case: frames>0 and an all-blank grid. `frames` alone
		// cannot tell it from a painted screen, so the grid is asked, and the
		// answer is said out loud on the same channel as the frozen-dump report.
		fmt.Fprint(os.Stderr, chatDumpBlankGridNotice)
	}
}

// chatDumpFinalize freezes the sink on its last PAINTED frame. It runs on EVERY
// exit path including a failure, because a dump that stopped one frame short of
// the screen the operator saw is an artifact a reader would have to
// second-guess.
func chatDumpFinalize(d *uiclient.ScreenDumper) {
	if d == nil {
		return
	}
	if err := d.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "gopi chat: the screen dump could not be finalized: %v\n", err)
	}
}

// chatNextArg returns the argv element after i ("" when there is none), so the
// flag parser can refuse a SEPARATED path by name — the check needs the
// neighbour, and threading the slice keeps the parser a pure function of argv.
func chatNextArg(args []string, i int) string {
	if i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

// chatTerminalGeometry reads the geometry the chat layout draws into. It asks
// the output file the SAME way `gopi ui` does (cli_ui.go's terminalGeometry) so
// the two surfaces cannot disagree about what "the terminal's size" means, and a
// failure returns 0,0 — the sink then keeps its own default rather than
// propagating a zero-width grid.
func chatTerminalGeometry(out *os.File) (cols, rows int) { return terminalGeometry(out) }
