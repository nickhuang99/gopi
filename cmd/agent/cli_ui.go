package main

// cli_ui.go — `gopi ui`: the Bubble Tea terminal chat surface (POC #2).
//
// THE CARRIER. `gopi ui` is a SECOND VIEW over the SAME engine as `gopi web`:
// it starts the existing web plumbing in-process (the same webServer, the same
// single persistent agent, the same s.mu turn discipline, the same server-held
// s.history, the same slash-command table, the same agent.OnToolEvents tool
// stream, the same POST /api/chat SSE frames) and then runs a Bubble Tea TUI
// against it over LOOPBACK.
//
//	                    ┌──────────────────────────────┐
//	  the TUI (Bubble Tea) ── POST /api/chat (SSE) ──▶ existing engine
//	                    └──────────────────────────────┘
//
// Nothing in gopi web's page, routes or frames is modified: the client consumes
// the wire exactly as the browser does. The loopback bind is what makes the
// per-run access token unnecessary (webGatedHandler mints none for a loopback
// address) — the gate itself is untouched, and pointing the client at a
// non-loopback bind means carrying the token, never weakening it.
//
// WHY A NEW VERB RATHER THAN A FLAG: `gopi web` is the browser carrier and
// `gopi chat` is the pipe/REPL carrier; a third surface with its own terminal
// requirements (raw mode, alternate screen, per-frame diffs) is a peer verb,
// not a mode of either. The word is checked against every existing verb by the
// CLI contract suite (dispatcherOfToken / topLevelCommandNames /
// TestRoleInvocationVerbsExist), so it cannot shadow one.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/uiclient"
	"golang.org/x/sys/unix"
)

// The `gopi ui` flag vocabulary. Named (Rule 1b) because the flag scanner, the
// usage page and the tests must agree on the spellings.
const (
	// uiToolsFlag is `--tools`, the SAME switch `gopi web --tools` takes (and
	// the server's own parser is what reads it — one flag vocabulary, not two).
	uiToolsFlag = "--tools"
	// uiServerFlag is `--server URL`: attach to an ALREADY-RUNNING gopi web
	// server instead of starting one. It is the escape hatch for an operator
	// who wants the TUI against a server that is already up (and, on a
	// non-loopback bind, carries its token: uiTokenFlag).
	uiServerFlag = "--server"
	// uiTokenFlag is `--token TOKEN`: the per-run access token a NON-loopback
	// `gopi web` bind demands. Never required for the loopback default.
	uiTokenFlag = "--token"
	// uiDumpFlag is `--dump-screen[=PATH]`: the surface writes its own
	// interpreted screen state to a file an agent can read — text, not pixels.
	// Without a PATH the file lands in the per-session scratch tree, and the
	// resolved path is printed before the TUI takes the terminal.
	uiDumpFlag = "--dump-screen"
	// uiDumpModeFlag is `--dump-mode ansi|plain`: how the sink interprets the
	// render stream. Keep it short: a constant, not a paragraph.
	uiDumpModeFlag = "--dump-mode"
	// uiFlagValueSep is the `--flag=value` separator (both spellings accepted).
	uiFlagValueSep = "="
	// uiFlagPrefix marks an argument as a FLAG rather than a value. It is what
	// tells a boolean flag's neighbour ("--dump-screen --tools") from a
	// misplaced value ("--dump-screen /a/path"), and it is one spelling used by
	// every such test in the parser (Rule 1b).
	uiFlagPrefix = "-"
)

// The dump's own names and env switches. Each is spelled once here and
// referenced (Rule 1b/13): the flag scanner, the resolver and the usage page all
// read these, so a rename cannot leave one of them saying something else.
const (
	// uiDumpFileName is the dump's file name — inside the per-session scratch
	// directory when no path was given, and the pattern for that directory
	// itself.
	uiDumpFileName = "ui-screen.dump"
	// uiDumpEnv is the env switch: GOPI_UI_DUMP=1 asks for a dump in the
	// scratch tree, GOPI_UI_DUMP=/a/path asks for that path. It exists because
	// the surface may be started by something that cannot add a flag.
	uiDumpEnv = "GOPI_UI_DUMP"
	// uiDumpModeEnv overrides the interpretation mode from the environment.
	uiDumpModeEnv = "GOPI_UI_DUMP_MODE"
)

// uiServerShutdownGrace bounds the HTTP server's shutdown once the TUI has
// ended, so a stuck connection cannot hold the process open.
const uiServerShutdownGrace = 5 * time.Second

// webWildcardHost4 / webWildcardHost6 are the "all interfaces" hosts a bind
// address may name. They are hosts, not paths, so they are spelled once here
// and compared against — never re-typed at a second decision site.
const (
	webWildcardHost4 = "0.0.0.0"
	webWildcardHost6 = "::"
)

// uiIdentityNoticeFormat is the ONE line that answers "which engine/session am
// I actually driving?" for a TUI the operator is about to use — locally, or
// from a browser on another machine. It is printed BEFORE the display takes the
// terminal (after that, stderr belongs to the renderer), and it is the same
// fact the gated /api/identity route reports, so a remote client can confirm it
// reached the engine it thinks it did. The session id is the id
// finalizeChatSession closes and `gopi session list` shows; the model is the
// one the agent resolves for this run (deepseek.ResolveModel's precedence).
const uiIdentityNoticeFormat = "gopi ui: engine session=%s model=%s tools=%v addr=%s\n"

// isUISubcommand reports whether argv begins with the `ui` verb and runs it to
// completion (the process exits inside). `-h` prints the usage page and runs
// NOTHING — the `gopi worker -h` pitfall (a help query once ran the worker
// loop); the contract test asserts this on the real binary, not on the source.
func isUISubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbUI {
		return false
	}
	os.Exit(runUIChat(args[1:]))
	return true
}

// runUIChat is the whole verb: resolve the target (start a loopback server
// unless --server was given), run the TUI, then shut the server down and
// finalize the chat session.
func runUIChat(args []string) int {
	opts, wantHelp, err := uiParseFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi ui: %v\n\n%s", err, uiUsage)
		return 2 // the POSIX usage class: the command line is wrong
	}
	if wantHelp {
		fmt.Print(uiUsage)
		return 0
	}

	// THE CONFIGURATION IS RESOLVED HERE, ONCE FOR THE WHOLE SERVICE — the same
	// defect `gopi chat` had (6fd8b134, chat_config_resolution.go): main()
	// resolves the configuration AFTER the subcommand chain and this face exits
	// from INSIDE that chain, so config.ApplyEnv never ran and no settings.json
	// knob reached the process environment. internal/agent reads thinking /
	// reasoning_effort from the ENVIRONMENT only and defaults to
	// thinking=disabled, so every turn this display served went out with
	// thinking off while the operator's settings said "high".
	//
	// IT COMES BEFORE uiStartServer, and that position is the whole point: the
	// in-process engine, its agent and the system prompt it assembles are all
	// built inside that call, so a resolution placed after it would publish the
	// values to a process that had already asked for them. With `--server` the
	// engine is somebody else's and this face still resolves its own
	// configuration: the CLIENT's model/effort are not the server's to decide,
	// and a run that skipped it would silently send the defaults.
	//
	// WHY HERE AND NOT IN newWebAgent: the engine's agent factory can be called
	// again (a resumed conversation, a second engine). See
	// service_config_resolution.go — one call, one precedence (Rule 2).
	if code := resolveUIRunConfig(args); code != serviceConfigExitOK {
		return code
	}

	engine, base, err := uiStartServer(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi ui: %v\n", err)
		return 1
	}
	// The TUI holds the terminal from here on; anything printed to stderr
	// would smear the rendered frames, so the attach notice is printed only in
	// the --server case (where this process owns no server and its stderr is
	// not being painted on).
	if engine == nil {
		fmt.Fprintf(os.Stderr, "gopi ui: attaching to %s\n", base)
	}
	// A server this process started is loopback by construction unless the
	// operator asked otherwise; with a non-loopback --addr the token its own
	// gate minted is what the client must present. Resolved BEFORE the probe and
	// the identity block below: both of those authenticate with it.
	if engine != nil && engine.token != "" {
		opts.token = engine.token
	}

	// WAIT FOR THE ENGINE THE CLIENT WILL CALL. The in-process server was
	// accepting at the end of uiStartServer, so this is a formality in the
	// ordinary case — but it is what turns "the display started and every turn
	// times out" into a startup failure that names the origin (Rule 15).
	if err := uiEngineReady(base, opts.token); err != nil {
		if engine != nil {
			engine.shutdown()
		}
		fmt.Fprintf(os.Stderr, "gopi ui: %v\n", err)
		return 1
	}

	// The ONE client for this session: the TUI streams through it, and the
	// interrupt hook below uses the same transport (so an attached engine is
	// told with the same token the turns carry).
	client := &uiclient.Client{BaseURL: base, Token: opts.token}
	if engine == nil {
		// An attached engine is interrupted over the wire through this client.
		uiAttachedClient = client
	}

	// SAY WHO WE ARE, while stderr is still ours. The TUI takes the terminal
	// from here on, and the engine's token is re-minted per run — so the
	// identity block (session id + model + tools + counters) is the one place a
	// remote operator can see WHICH session/model the URL they hold drives. For
	// --server the engine is not ours: its identity is fetched over the gated
	// wire (never guessed), and a peer that does not answer is reported, not
	// silently assumed to be fine (Rule 15).
	uiPrintEngineIdentity(engine, base, opts)
	code := 0
	engineLog := ""
	if engine != nil {
		engineLog = engine.logPath
	} else {
		// Attached to a server this process did not start: there is no local
		// session directory to share, and the display still owns the terminal
		// — so the redirect takes a scratch directory. It is never /tmp: the
		// scratch tree's MkdirTemp is what picks a directory OFF the root
		// filesystem (Rule 22).
		engineLog = uiServerAttachLogPath()
	}
	// WHERE THE SELF-OBSERVATION GOES. Resolved BEFORE the TUI starts, because
	// the path has to be printed while the terminal still belongs to the shell,
	// and a resolution failure must stop the run rather than leave an observer
	// waiting for a file nothing will write (Rule 15).
	dumpPath, err := uiResolveDumpPath(opts, engine.dumpAgent())
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi ui: %v\n", err)
		if engine != nil {
			engine.shutdown()
		}
		return 1
	}
	if dumpPath != "" {
		fmt.Fprintf(os.Stderr, uiDumpNotice, dumpPath)
	}
	if err := uiRunTUI(base, opts, dumpPath, engineLog, os.Stdout, client, uiInterruptFunc(engine), uiSessionIDFor(engine, base, opts.token)); err != nil {
		// A terminal that went away is the operator closing the window, not a
		// fault: bubbletea reports it as ErrProgramKilled with the context
		// cause attached. Everything else is reported (Rule 15).
		if !errors.Is(err, uiTerminalClosedError) && !isTerminalGone(err) {
			fmt.Fprintf(os.Stderr, "gopi ui: %v\n", err)
			code = 1
		}
	}

	if engine != nil {
		engine.shutdown()
	}
	return code
}

// engineLogPerm is the mode for the per-session engine log: owner read/write,
// the same posture as the session's other artifacts.
const engineLogPerm = 0o644

// uiEngineLogName is the file the in-process engine's console output is
// redirected into while the TUI owns the terminal. It lives beside the run's
// other artifacts (the raw log and session.jsonl) so the operator has ONE
// place to look.
const uiEngineLogName = "ui-engine.log"

// uiEngineLogPath resolves the engine-log destination for an in-process
// session: beside the session's own artifacts (the raw log and session.jsonl),
// so the operator has ONE directory to look in. A session with no directory of
// its own falls back to the scratch tree — never /tmp, because the scratch
// tree's own MkdirTemp is what picks a directory OFF the root filesystem
// (Rule 22) — and a scratch tree that cannot be created yields "" so the
// caller fails LOUDLY rather than writing to a path nobody chose.
func uiEngineLogPath(ag *agent.Agent) string {
	if ag != nil && ag.SessionLog != nil {
		return filepath.Join(sessionlayout.RecordContainer(ag.SessionLog.Path()), uiEngineLogName)
	}
	if dir, err := scratch.MkdirTemp(scratch.RuntimeClassUISession, uiEngineLogName); err == nil {
		return filepath.Join(dir, uiEngineLogName)
	}
	return ""
}

// uiScreenCounterEnv asks the surface to report its own renderer counters on
// stdout when the session ends. It exists for the pty transcript (the report's
// per-turn screen-write evidence): the counters are a MEASUREMENT the run can
// quote, not a property anyone has to take on trust.
const uiScreenCounterEnv = "GOPI_UI_SELFTEST"

// uiEngineConsoleEnv, when set, lets the in-process engine keep printing to
// the terminal during a TUI session. It is a DEBUGGING switch: the engine's
// diagnostics are ordinary terminal output, and the TUI owns the cursor of the
// same terminal, so the two interleave into a scrambled screen. Read here and
// nowhere else (Rule 2 / Rule 21: a declared read).
const uiEngineConsoleEnv = "GOPI_UI_ENGINE_STDERR"

// silenceEngineConsole redirects the PROCESS's console writers (os.Stdout and
// os.Stderr, the package variables every engine notice is written through) to
// the null device for the duration of a TUI session, and returns the restore
// function.
//
// WHY THIS IS THE WHOLE FIX rather than a per-package log probe: the engine is
// `gopi web`'s engine and it talks to the terminal by design — the startup
// banner, one `[gopi] chat table: …` line per turn, the `[thinking]` trace, the
// budget warnings. Under the browser carrier that text lands in the terminal
// the server was started from and nobody minds; under `gopi ui` the SAME
// terminal is being painted by the renderer, which tracks how many lines IT
// wrote. Let any other writer put a byte there and the renderer's model of the
// screen is wrong: the cursor-up navigation lands in the wrong place, and the
// transcript comes out interleaved character soup (measured: the first pty run
// produced exactly that).
//
// Nothing is lost: the engine writes its own raw log (ag.RawLog) and session
// JSONL regardless, the model's text and its tool activity reach the operator
// through the SSE frames the TUI renders, and a fatal fault reaches the client
// as an SSE error frame. The redirect is bounded by the caller's defer, so the
// post-session lines (the chat table's "ended", the server's shutdown notice)
// still land in the operator's scrollback — which is where they are useful.
func silenceEngineConsole(logPath string) func() {
	if os.Getenv(uiEngineConsoleEnv) != "" {
		return func() {}
	}
	out, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, engineLogPerm)
	if err != nil {
		// Not worth failing a chat session over: the screen may smear, and the
		// operator is told WHY and where the output was supposed to go
		// (Rule 15 — a failed redirect must not be a silent one).
		fmt.Fprintf(os.Stderr, "gopi ui: cannot write the engine log %s (%v) — engine notices may interleave with the display\n", logPath, err)
		return func() {}
	}
	// Say where the engine's own output went BEFORE the console is taken over:
	// a redirect the operator cannot see is indistinguishable from an engine
	// that said nothing.
	fmt.Fprintf(os.Stderr, "gopi ui: engine output → %s (the display owns this terminal)\n", logPath)
	realOut, realErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, out
	return func() {
		os.Stdout, os.Stderr = realOut, realErr
		if err := out.Close(); err != nil {
			// Recorded, not swallowed (Rule 15b): a close error here says the
			// log could not be flushed, not that the session failed.
			fmt.Fprintf(os.Stderr, "gopi ui: closing %s: %v\n", logPath, err)
		}
	}
}

// uiInterruptFunc builds the surface's Ctrl+X hook: it asks the ENGINE to
// abandon the turn in flight, by the shortest honest route available.
//
// IN PROCESS when this carrier started the engine (the common case): the
// agent's own run-scoped interrupt, so no HTTP round-trip and no token dance.
// OVER THE WIRE when the engine is somebody else's (--server): the interrupt
// route, carrying the same client (and therefore the same token) the turn
// stream uses.
//
// It NEVER returns without saying something: a hook that quietly did nothing
// would leave the surface printing "interrupted (Ctrl+X)" while the turn kept
// running and holding its lock — exactly the state this hook exists to end. The
// notice goes through the engine log, because the display owns the terminal
// (stderr is redirected for the session — see silenceEngineConsole).
func uiInterruptFunc(engine *uiEngine) func() {
	return func() {
		if engine != nil && engine.srv != nil && engine.srv.agent != nil {
			if !engine.srv.agent.InterruptRun() {
				fmt.Fprintf(os.Stderr, "gopi ui: no turn was in flight to abandon\n")
			}
			return
		}
		uiAttachedInterrupt()
	}
}

// uiAttachedInterrupt tells an engine this process did not start to abandon its
// turn in flight, over the wire (POST uiclient.InterruptPath).
func uiAttachedInterrupt() {
	client := uiAttachedClient
	if client == nil {
		fmt.Fprintf(os.Stderr, "gopi ui: cannot interrupt: no engine client is configured\n")
		return
	}
	if err := client.Interrupt(); err != nil {
		fmt.Fprintf(os.Stderr, "gopi ui: interrupt failed: %v (the turn may still be running on the engine)\n", err)
	}
}

// uiAttachedClient is the transport the ATTACHED-engine interrupt uses. It is a
// package var because the hook is handed to the model in Config.OnInterrupt,
// whose signature carries no client — the alternative would be threading a
// closure through CountedWriter/ScreenWriter, which are display plumbing and
// must not learn about the wire.
var uiAttachedClient *uiclient.Client

// uiRunTUI runs the surface and, when the environment asks for it, prints the
// measured counters as a one-line machine-readable record. It returns the
// surface's error (a terminal that went away is not one — see runUIChat).
// WHEN A DUMP IS ASKED FOR, the sink is armed BEFORE the program starts and
// reported AFTER it ends. Both halves matter: armed first because a dump that
// missed the first frames would describe a screen the reader never saw, and
// reported last because the TUI owns the terminal while it runs.
func uiRunTUI(base string, opts uiOptions, dumpPath, engineLog string, out *os.File, client *uiclient.Client, onInterrupt func(), sessionID string) error {
	if engineLog == "" {
		return fmt.Errorf("no destination is available for the engine log")
	}
	restore := silenceEngineConsole(engineLog)
	screen := uiclient.NewScreenWriter(out)
	cols, rows := terminalGeometry(out)
	writer, dumper, err := uiArmDump(dumpPath, opts, base, cols, rows, screen)
	if err != nil {
		restore()
		return err
	}
	// THE BAR'S TURN COUNTER STARTS WHERE THE LEDGER IS (see ui_turn_baseline.go):
	// a resumed session, or one that `gopi notify` deliveries opened turns on,
	// already holds a count that `gopi session log` and `gopi session show`
	// print — the bar must not restart at 1 and disagree with both.
	turnBaseline := uiTurnBaselineFor(sessionID)
	stats, runErr := uiclient.Run(uiclient.Config{
		Client:       client,
		Target:       base,
		Tools:        opts.tools,
		TurnBaseline: turnBaseline,
		Out:          writer,
		Screen:       screen,
		// The emphasis table comes from the ONE capability decision
		// (lineedit.CapabilityForEnv: NO_COLOR, TERM=dumb, and the sink's own
		// terminal-ness, in that precedence — then, on the colour-ON side,
		// whether the terminal advertises 24-bit colour via COLORTERM and which
		// table GOPI_PALETTE asked for). `out` is the real terminal fd, not
		// the counting writer — the counting writer is not a tty by construction,
		// so asking IT would disable colour on every terminal. Colour off means
		// the table is the zero value and every transcript row is written as its
		// plain text, byte for byte.
		Palette: lineedit.PaletteForEnvCapability(false, out),
		Width:   cols,
		Height:  rows,
		// Ctrl+X means "abandon the turn in flight", and only the ENGINE can do
		// that: this surface's own cancel ends the client's stream while the
		// turn keeps running on the server and holds the lock the operator's
		// next message needs. The hook is the engine's own interrupt — in
		// process (the agent this carrier started) when there is one, over the
		// wire (POST uiclient.InterruptPath) when the engine is somebody else's.
		OnInterrupt: onInterrupt,
		// The dump's grid follows the FRAME's geometry: the model reports what
		// each frame was laid out for and the sink is re-sized to that number, so
		// `grid=` in the artifact cannot disagree with the frame the terminal
		// received (a caller that only knew Config.Width would keep the startup
		// hint after a WindowSizeMsg moved the model).
		OnFrameGeometry: func(cols, rows int) {
			dumper.SetGeometry(uiclient.GeometrySourceLayout, cols, rows)
		},
	})
	restore()
	if dumper != nil {
		uiReportDump(dumper, opts, base, cols, rows)
	}
	if os.Getenv(uiScreenCounterEnv) != "" {
		// After the program has left the alternate screen and the console is
		// the operator's again, so this line lands in the scrollback where a
		// transcript can quote it.
		fmt.Fprintf(out, "#gopi-ui-counters render_frames=%d render_bytes=%d turns=%d turn_frames=%v\n",
			stats.RenderFrames, stats.RenderBytes, len(stats.TurnFrames), stats.TurnFrames)
	}
	return runErr
}

// uiArmDump builds the dump sink and returns the writer the TUI must paint
// through. With no dump requested it returns the plain counting writer
// UNCHANGED — the capability is additive, and an off switch that subtly
// re-routed the render stream would be a change to the surface, not a
// measurement of it.
func uiArmDump(dumpPath string, opts uiOptions, base string, cols, rows int, screen *uiclient.ScreenWriter) (io.Writer, *uiclient.ScreenDumper, error) {
	if dumpPath == "" {
		return screen, nil, nil
	}
	dumper, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: dumpPath,
		Mode: opts.dumpMode,
		Cols: cols,
		Rows: rows,
		Identity: uiclient.DumpIdentity{
			PID:              os.Getpid(),
			Target:           base,
			Tools:            opts.tools,
			ColsRows:         [2]int{cols, rows},
			TerminalColsRows: [2]int{cols, rows},
			// Started is the process's KERNEL start instant, not "now", because
			// that is the value a reader can VERIFY: it re-reads /proc/<pid>/stat
			// and requires an exact match before it will call this dump live
			// (uiclient's read side). A wall-clock "now" here would make every
			// dump read as a recycled pid — the identity check would be unfalsifiable
			// in the direction that matters. A failure reports rather than
			// fabricating a stamp (Rule 15): the reader then says "foreign", which
			// is the honest verdict for a dump whose identity could not be stated.
			Started: uiDumpStartStamp(),
		},
	})
	if err != nil {
		return nil, nil, err
	}
	// The sink reads the SAME bytes the terminal gets: it wraps the counting
	// writer, so the per-turn numbers the report quotes stay the terminal's
	// own volume and never include the observer's traffic.
	return io.MultiWriter(screen, dumper), dumper, nil
}

// uiDumpStartStamp resolves the start instant the dump identity carries: this
// process's kernel start time (the value uiclient's reader re-derives and
// compares). A failure is REPORTED on stderr and the zero time is returned, so
// the dump says plainly that its identity could not be stated — never a
// fabricated stamp that would make the identity check pass for the wrong reason.
func uiDumpStartStamp() time.Time {
	started, err := uiclient.ProcessStartTime(os.Getpid())
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi ui: the screen dump cannot carry a verifiable start stamp: %v\n", err)
		return time.Time{}
	}
	return started
}

// uiReportDump prints what the self-observation produced, AFTER the console is
// the operator's again. It is the observable half of the claim: the file's
// path, its size, and how many dumps the sink installed against how many frames
// the renderer painted. A run that wrote no dump says so here — silence would
// read as success.
func uiReportDump(d *uiclient.ScreenDumper, opts uiOptions, base string, cols, rows int) {
	snap := d.Snapshot()
	geom, _, _ := d.Geometry()
	size := int64(-1)
	if info, err := os.Stat(d.Path()); err == nil {
		size = info.Size()
	} else {
		fmt.Fprintf(os.Stderr, "gopi ui: cannot stat the screen dump %s: %v\n", d.Path(), err)
	}
	fmt.Fprintf(os.Stderr,
		"#gopi-ui-dump-report path=%s json=%s bytes=%d pid=%d target=%s terminal=%dx%d grid=%dx%d geometry=%s mode=%s attrs=%t dumps=%d frames=%d blind_spots=%d\n",
		d.Path(), d.JSONPath(), size, os.Getpid(), base, cols, rows,
		snap.Cols, snap.Rows, geom, opts.dumpMode, snap.AttrsRecorded, d.Writes(), d.Frames(), len(snap.BlindSpots))
	if geom == uiclient.GeometrySourceFallback {
		// Rule 15: a grid that is NOT the layout's says so on stderr as well as
		// in its own record, because the artifact alone is not read by everyone
		// watching the terminal.
		fmt.Fprintf(os.Stderr, "gopi ui: %s\n", uiclient.GeometryFallbackNotice)
	}
}

// terminalGeometry reads the terminal's size in cells from the output file.
//
// The caller (not Bubble Tea) must supply it: the counting writer passed as the
// program's output is not a tty, so the framework never emits a WindowSizeMsg
// and the model would lay out for its 80x24 default in any terminal (measured
// on a 100-column pty: the model's own lines broke at 76 columns while the
// kernel wrapped the banner at 100). A failure returns 0,0 — the caller then
// keeps the model's default rather than propagating a zero-width layout.
func terminalGeometry(out *os.File) (cols, rows int) {
	if out == nil {
		return 0, 0
	}
	ws, err := unix.IoctlGetWinsize(int(out.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		// Not a terminal (or no size to report): the surface degrades to the
		// model's default. Recorded, not swallowed (Rule 15b) — the debug
		// channel is where a wrong-looking layout gets diagnosed.
		if os.Getenv("GOPI_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "gopi ui: %s: %v (using the default layout)\n", out.Name(), err)
		}
		return 0, 0
	}
	return int(ws.Col), int(ws.Row)
}

// uiTerminalClosedError is the sentinel class for "the terminal went away" —
// the one TUI outcome that is not a fault.
var uiTerminalClosedError = errors.New("terminal closed")

// isTerminalGone reports whether the TUI's error means the terminal was closed
// under it rather than a genuine failure. A signal-driven exit surfaces as
// context.Canceled (bubbletea cancels its context on SIGINT/SIGTERM), which is
// the normal end of an interactive session.
func isTerminalGone(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "program was killed") || strings.Contains(msg, uiTerminalClosedError.Error())
}

// uiOptions is the parsed command line.
type uiOptions struct {
	// addr is the listen address for a server this process starts (empty when
	// --server names an external one).
	addr string
	// server is the base URL of an external server (--server), or "".
	server string
	// token is the per-run access token for a non-loopback server (--token).
	token string
	// tools mirrors the server's --tools switch.
	tools bool
	// dumpPath is where the surface writes its own screen dump (--dump-screen,
	// or GOPI_UI_DUMP). Empty = the capability is off.
	dumpPath string
	// dumpRequested records that a dump was asked for WITHOUT a usable path: the
	// resolver then chooses one (the per-session scratch tree). It is separate
	// from dumpPath so "off" and "on, path unknown" cannot be confused — the
	// difference decides whether a dump is produced at all.
	dumpRequested bool
	// dumpMode is how the dump sink interprets the render stream.
	dumpMode uiclient.DumpMode
	// thinking is the live-reasoning switch (--thinking-live, or the shared
	// GOPI_THINKING_LIVE env the browser carrier reads). It is handed to the
	// engine this process starts, which is where it is ENFORCED: the frames
	// exist only if the server was built to emit them.
	//
	// It is deliberately NOT a rendering switch: the surface draws whatever
	// frames arrive, so "should I show reasoning?" is answered once, at the
	// engine, and a client that guesses otherwise would disagree with the wire
	// it is reading.
	//
	// With --server the flag is refused by uiStartServer's caller (the remote
	// engine's own switches are its own) — see uiParseFlags.
	thinking bool
}

// uiToolsFromEnv reads the SHARED tools switch: `gopi ui` takes the same
// GOPI_WEB_TOOLS the browser carrier does (one switch, two surfaces), so the
// env name is referenced — never re-spelled (Rule 1b).
func uiToolsFromEnv() bool { return webToolsEnabled() }

// uiDumpFromEnv reads the screen-dump switch. `GOPI_UI_DUMP=1` (any value
// config.EnvBool accepts) asks for a dump at the resolver's chosen path;
// any OTHER non-empty value IS the path. The distinction matters: "off" and
// "on, path to be resolved" must not collapse into one empty string.
func uiDumpFromEnv() (path string, requested bool) {
	v := strings.TrimSpace(os.Getenv(uiDumpEnv))
	if v == "" {
		return "", false
	}
	if config.EnvBool(uiDumpEnv) {
		return "", true
	}
	return v, true
}

// uiDumpModeFromEnv reads the interpretation mode. A value it cannot parse is
// REPORTED and the default kept — the surface still runs, and the operator
// learns which value was ignored rather than silently getting the other
// rendering (Rule 15).
func uiDumpModeFromEnv() uiclient.DumpMode {
	v := strings.TrimSpace(os.Getenv(uiDumpModeEnv))
	if v == "" {
		return uiclient.DumpModeANSI
	}
	mode, err := uiParseDumpMode(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi ui: %v (using %s)\n", err, uiclient.DumpModeANSI)
		return uiclient.DumpModeANSI
	}
	return mode
}

// uiParseDumpMode validates a mode spelling against the constants that own
// them (Rule 13/14): the accepted words are the constants, never retyped here.
func uiParseDumpMode(v string) (uiclient.DumpMode, error) {
	if v == string(uiclient.DumpModeANSI) {
		return uiclient.DumpModeANSI, nil
	}
	if v == string(uiclient.DumpModePlain) {
		return uiclient.DumpModePlain, nil
	}
	return "", fmt.Errorf("%s must be %s or %s, got %q",
		uiDumpModeFlag, uiclient.DumpModeANSI, uiclient.DumpModePlain, v)
}

// dumpDirPerm is the mode for a directory the dump needs and does not have —
// the operator named the path, so creating its parent is what honouring the
// request means.
const dumpDirPerm = 0o755

// uiDumpDirPattern names the scratch directory the resolver creates when no
// explicit path was given (the file inside it is uiDumpFileName).
const uiDumpDirPattern = "ui-screen"

// uiDumpNotice is printed BEFORE the TUI takes the terminal: the TUI cannot
// print anything afterwards, and the path is the one fact an observer needs and
// cannot guess. A dump nobody can find is indistinguishable from no dump.
const uiDumpNotice = "gopi ui: screen dump → %s (plain text; the surface's own interpreted state — no pixels)\n"

// uiResolveDumpPath decides where the dump goes, and fails LOUDLY rather than
// silently dropping the capability (Rule 15). Precedence: the explicit path
// from the flag/env wins; otherwise the run's OWN session directory (beside
// session.jsonl and the engine log, so one directory holds everything about the
// run); otherwise a scratch directory (never /tmp — Rule 22).
func uiResolveDumpPath(opts uiOptions, ag *agent.Agent) (string, error) {
	if !opts.dumpRequested {
		return "", nil
	}
	if opts.dumpPath != "" {
		if dir := filepath.Dir(opts.dumpPath); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, dumpDirPerm); err != nil {
				return "", fmt.Errorf("cannot create the dump directory %s: %w", dir, err)
			}
		}
		return opts.dumpPath, nil
	}
	if ag != nil && ag.SessionLog != nil {
		return filepath.Join(sessionlayout.RecordContainer(ag.SessionLog.Path()), uiDumpFileName), nil
	}
	dir, err := scratch.MkdirTemp(scratch.RuntimeClassUISession, uiDumpDirPattern)
	if err != nil {
		return "", fmt.Errorf("no destination is available for the screen dump: %w", err)
	}
	return filepath.Join(dir, uiDumpFileName), nil
}

// dumpAgent returns the in-process engine's agent, or nil when this process
// started no engine (the --server case) — the resolver's input for "does this
// run have a session directory of its own?".
func (e *uiEngine) dumpAgent() *agent.Agent {
	if e == nil || e.srv == nil {
		return nil
	}
	return e.srv.agent
}

// uiParseFlags reads the verb's argv. Unknown flags are REFUSED (never an
// agent-loop fallthrough: a typo must not burn an LLM session).
func uiParseFlags(args []string) (uiOptions, bool, error) {
	opts := uiOptions{addr: webDefaultAddr(), tools: uiToolsFromEnv()}
	opts.thinking = webThinkingEnabled()
	thinkingFlagSeen := false
	opts.dumpPath, opts.dumpRequested = uiDumpFromEnv()
	opts.dumpMode = uiDumpModeFromEnv()
	for i := 0; i < len(args); i++ {
		a := args[i]
		value := func(flag string, inline string) (string, error) {
			if inline != "" {
				return inline, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", flag)
			}
			i++
			return args[i], nil
		}
		inline := ""
		name := a
		if k := strings.Index(a, uiFlagValueSep); k > 0 {
			name, inline = a[:k], a[k+len(uiFlagValueSep):]
		}
		switch {
		case a == HelpFlag || a == HelpFlagLong:
			return opts, true, nil
		case name == uiDumpFlag:
			// BOTH spellings are meaningful: `--dump-screen=/a/path` writes
			// there, and the bare `--dump-screen` asks for one in the session's
			// own scratch tree (the resolver picks it, and the path is printed
			// so nobody has to guess).
			//
			// A SEPARATED path is refused BY NAME rather than falling through
			// to an "unknown argument" for the path itself: the mistake is the
			// flag's form, and the error must say so (Rule 15 — the message
			// names the fix, not just the blame).
			if inline == "" && i+1 < len(args) && !strings.HasPrefix(args[i+1], uiFlagPrefix) {
				return opts, false, fmt.Errorf(
					"%s takes its path joined — write %s=%s (or use the bare %s for a dump beside this run's artifacts)",
					uiDumpFlag, uiDumpFlag, args[i+1], uiDumpFlag)
			}
			opts.dumpRequested = true
			opts.dumpPath = inline
		case name == uiDumpModeFlag:
			v, err := value(uiDumpModeFlag, inline)
			if err != nil {
				return opts, false, fmt.Errorf("%s needs a mode (%s or %s)",
					uiDumpModeFlag, uiclient.DumpModeANSI, uiclient.DumpModePlain)
			}
			mode, err := uiParseDumpMode(v)
			if err != nil {
				return opts, false, err
			}
			opts.dumpMode = mode
		case name == uiToolsFlag:
			opts.tools = true
		case name == webThinkingFlag:
			// The SAME switch `gopi web` takes (webThinkingFlag, one spelling —
			// Rule 13): here it turns THIS process's engine into a live-reasoning
			// engine, which is what makes the frames exist on the wire. With
			// --server it is refused: see the borrowRefused block below.
			opts.thinking = true
			thinkingFlagSeen = true
		case name == webAddrFlag:
			v, err := value(uiServerFlag, inline)
			if err != nil {
				return opts, false, fmt.Errorf("%s needs a value, e.g. %s=%s", webAddrFlag, webAddrFlag, webDefaultAddrValue)
			}
			opts.addr = v
		case name == uiServerFlag:
			v, err := value(uiServerFlag, inline)
			if err != nil {
				return opts, false, fmt.Errorf("%s needs a URL, e.g. %s=%s", uiServerFlag, uiServerFlag, uiclient.DefaultServerBase)
			}
			opts.server = v
		case name == uiTokenFlag:
			v, err := value(uiTokenFlag, inline)
			if err != nil {
				return opts, false, fmt.Errorf("%s needs the per-run access token printed by `gopi web`", uiTokenFlag)
			}
			opts.token = v
		default:
			return opts, false, fmt.Errorf("unknown argument %q", a)
		}
	}
	if opts.server != "" {
		// --server names an external engine: this process starts nothing, so a
		// listen address would be a contradiction. Refusing it keeps one story
		// about where the conversation lives (Rule 15 — a silently ignored
		// flag is a lie about what the command did).
		if opts.addr != webDefaultAddr() {
			return opts, false, fmt.Errorf("%s and %s are mutually exclusive: %s attaches to a running server", uiServerFlag, webAddrFlag, uiServerFlag)
		}
		opts.addr = ""
		// The live-reasoning switch describes the ENGINE this process STARTS,
		// and with --server it starts none: the frames a remote engine emits are
		// its own decision. So the flag is REFUSED rather than accepted-and-
		// ignored (an operator who set it got a verb that reads as "reasoning
		// on" while nothing changed — Rule 15), and the env-derived default is
		// dropped for the same reason: with no local engine there is nothing
		// for it to configure.
		if thinkingFlagSeen {
			return opts, false, fmt.Errorf("%s starts no engine: pass it to the server instead (%s %s), then %s to this client", uiServerFlag, CliVerbWeb, webThinkingFlag, uiServerFlag)
		}
		opts.thinking = false
	}
	return opts, false, nil
}

// uiEngine is the in-process server `gopi ui` started: the web engine plus the
// facts this verb needs and `gopi web` keeps in its own locals (the bound
// address, the minted token, the *http.Server, and the reachable URLs a REMOTE
// browser needs). Held on the CALLER's side rather than by adding fields to
// webServer, so this carrier touches no line of the browser carrier's state
// (Rule 4 / constraint 3: the surface is additive).
type uiEngine struct {
	srv   *webServer
	http  *http.Server
	token string
	// identityURL is the token-carrying webIdentityPath URL for THIS engine —
	// the address a remote operator re-checks the facts with. Complete (origin
	// + path + token when the bind demands one), so no caller assembles it.
	identityURL string
	// logPath is where the engine's own console output was redirected. Kept so
	// the caller can name it (the operator has to be able to READ what the
	// engine said while the display owned the terminal).
	logPath string
}

// shutdown stops the server and finalizes the chat session (ONE session_end +
// one task-ledger row for the whole conversation, exactly as the browser
// carrier finalizes it).
func (e *uiEngine) shutdown() {
	e.srv.shuttingDown = true
	ctx, cancel := context.WithTimeout(context.Background(), uiServerShutdownGrace)
	defer cancel()
	if e.http != nil {
		_ = e.http.Shutdown(ctx) //stylegate:ignore the session is finalized below either way; a shutdown failure leaves no state to recover//nolint:errcheck // the session is finalized below either way
	}
	e.srv.finalizeChatSession(true)
}

// uiStartServer starts an in-process `gopi web` engine bound to a loopback
// address (opts.addr) and returns it, or (nil, base, nil) when --server names
// an external one.
//
// The construction is runWeb's, deliberately: ONE persistent agent
// (PersistChatSession), the same LAN gate (webGatedHandler), the same
// webServer fields finalizeChatSession needs. The engine is shared, so it is
// built the same way rather than by a second copy of the rules.
func uiStartServer(opts uiOptions) (*uiEngine, string, error) {
	if opts.server != "" {
		return nil, strings.TrimRight(opts.server, "/"), nil
	}

	ln, err := net.Listen("tcp", opts.addr)
	if err != nil {
		return nil, "", fmt.Errorf("cannot listen on %s: %w", opts.addr, err)
	}
	// The bound port is what the TUI must call: with `--addr 127.0.0.1:0` the
	// kernel picks one, and the client cannot guess it.
	base, err := uiLoopbackBaseURL(opts.addr, ln.Addr())
	if err != nil {
		//stylegate:ignore the listener is abandoned on this error path and the caller returns the real failure
		_ = ln.Close()
		return nil, "", err
	}

	// The write boundary is frozen here for the same reason runWeb freezes it:
	// this engine executes tools, and a boundary left at the package defaults
	// would judge every write against `prompt *` instead of the operator's
	// ```chat-policy fence and declared posture.
	chatFreezeChatPolicyState(chatFacePrefixUI, "")
	srv := &webServer{addr: opts.addr, tools: opts.tools, thinking: opts.thinking, carrier: webChatCarrierUI, newWebAgent: func() (*agent.Agent, error) { return newAgent("") }}
	ag, err := srv.newWebAgent()
	if err != nil {
		//stylegate:ignore same abandoned listener as above; the caller returns the real failure
		_ = ln.Close()
		return nil, "", fmt.Errorf("failed to create agent: %w", err)
	}
	ag.PersistChatSession = true
	srv.agent = ag
	srv.startedAt = time.Now()
	srv.visionModel = deepseek.ResolveVisionModel(cfgForVision())
	printLogPaths(ag)

	// POINT THE AGENT'S OWN LOGGER AT THE ENGINE LOG. The agent is built by
	// newAgent with log.New(os.Stderr, …) — a logger BINDS the writer it is
	// given at construction, so redirecting os.Stderr afterwards does not move
	// it (measured: the budget notice and the chat-table lines kept landing on
	// the terminal through the swap and smeared the display). Rebinding the
	// field is the same single-writer decision the rest of this file makes,
	// applied one level down.
	engineLog, err := uiOpenEngineLog(ag)
	if err != nil {
		//stylegate:ignore same abandoned listener as above; the caller returns the real failure
		_ = ln.Close()
		return nil, "", err
	}
	ag.Logger = log.New(engineLog, engineLogLinePrefix, 0)

	trust := webLocalTrust(opts.addr)
	handler, token, err := webGatedHandler(opts.addr, srv.webChatHandler(), trust)
	if err != nil {
		//stylegate:ignore same abandoned listener as above; the caller returns the real failure
		_ = ln.Close()
		return nil, "", err
	}
	// A non-loopback bind mints a token; the TUI must carry the one its own
	// server just made. That is the gate WORKING (the same token the banner
	// prints is handed to the client in-process), never a bypass.

	httpSrv := &http.Server{Handler: handler}
	//stylegate:ignore Serve's error is http.ErrServerClosed on a clean shutdown; the stop path owns the verdict
	go func() { _ = httpSrv.Serve(ln) }() //nolint:errcheck // Serve's error is http.ErrServerClosed on a clean shutdown

	// THE REMOTE HANDOFF (the epic): a non-loopback bind is gated behind a
	// per-run token, and until now `gopi ui` minted that token and printed
	// NOTHING — a second machine had no URL to open, so remote browser access
	// was impossible in practice. Print the SAME banner the browser carrier
	// prints (one helper, one URL resolution), through the SAME shared block
	// that carries the exposure and tools notices. It must happen BEFORE the
	// display takes the terminal.
	if !addressIsLoopback(opts.addr) {
		uiPrintRemoteHandoff(opts.addr, ln, token, trust, srv.tools)
	}
	if srv.tools {
		fmt.Fprintf(os.Stderr, "%s\n", uiToolsNotice)
	}
	if srv.thinking {
		fmt.Fprintf(os.Stderr, "%s\n", uiThinkingNotice)
	}
	return &uiEngine{srv: srv, http: httpSrv, token: token, identityURL: uiIdentityURL(opts.addr, ln, token, trust), logPath: engineLog.Name()}, base, nil
}

// uiPrintRemoteHandoff prints the remote-access block for a GATED bind: the
// reachable URL(s) with the token embedded (the same webPrintListening the
// browser carrier uses) plus the LAN / local-trust notices. It is called only
// for a non-loopback bind — a loopback bind has no remote handoff to make and
// its banner would be noise on the display's first frame.
//
// The notices are the SHARED texts rendered in this carrier's name, so the
// warning about a network-reachable tool-executing agent cannot be weaker here
// than it is for `gopi web`.
func uiPrintRemoteHandoff(addr string, ln net.Listener, token string, trust, tools bool) {
	webPrintListeningRuntime(webCarrierUI, addr, ln, token, trust)
	if token == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", fmt.Sprintf(webLANWarningFormat, webCarrierUI))
	if trust {
		fmt.Fprintf(os.Stderr, "%s\n", fmt.Sprintf(webLocalTrustNoticeFormat, webCarrierUI))
	}
	if tools {
		fmt.Fprintf(os.Stderr, "%s\n", fmt.Sprintf(webToolsLANWarningFormat, webCarrierUI))
	}
}

// uiIdentityURL is the identity route's URL for this engine — the address a
// REMOTE operator re-checks the facts with, so an interface address is
// preferred over a loopback-only one (a `localhost` URL pasted from the banner
// would only work on the machine that printed it).
//
// The token goes in the QUERY SEGMENT, before the route: the gate reads the
// query from the URL's query string, so `…/api/identity?t=<token>` is the shape
// that authenticates. (Appending the route after a query — `…?t=<token>/api/
// identity` — puts the whole thing inside the query's VALUE: measured, the gate
// reads `t=<token>/api/identity`, the constant-time compare fails and the URL
// from the operator's own banner 401s.) A loopback bind mints no token and
// keeps the plain origin + route.
func uiIdentityURL(addr string, ln net.Listener, token string, localTrust bool) string {
	urls := webReachableURLs(addr, ln)
	if len(urls) == 0 {
		return ""
	}
	pick := urls[0]
	for _, u := range urls {
		if !webURLIsLoopback(u) {
			pick = u
			break
		}
	}
	return webRouteURL(pick, webIdentityPath, token, localTrust)
}

// uiEngineStartupGrace is how long the TUI waits for its OWN in-process server
// to accept on the bound socket before running the terminal surface against it.
// The listener is already accepting when uiRunTUI is called, so this is a
// formality for the ordinary case; it exists so a bind that never became
// reachable is reported as a startup failure instead of as a turn that
// mysteriously times out (Rule 15).
const uiEngineStartupGrace = 10 * time.Second

// uiEngineReadyPoll is the retry interval of uiEngineReady — a local round trip
// is sub-millisecond, so this only needs to be short enough to feel immediate.
const uiEngineReadyPoll = 100 * time.Millisecond

// uiEngineReady waits until the engine's origin ANSWERS AN HTTP REQUEST. Any
// status counts: this asks "is the socket serving?", not "am I authorized?" — a
// 401 from the gate is the engine answering, and the token question is reported
// by the identity fetch that follows (in its own words). Conflating the two
// would make a token mistake look like a dead engine.
//
// A base nothing answers within uiEngineStartupGrace returns an error naming the
// origin, so a startup failure is diagnosable instead of silent.
func uiEngineReady(base, token string) error {
	deadline := time.Now().Add(uiEngineStartupGrace)
	url := strings.TrimRight(base, "/") + webIdentityPath
	var lastErr error
	for {
		lastErr = uiProbeEngine(url, token)
		if lastErr == nil {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("the engine at %s did not answer within %s: %w", base, uiEngineStartupGrace, lastErr)
		}
		time.Sleep(uiEngineReadyPoll)
	}
}

// uiProbeEngine issues one GET and reports nil as soon as ANY HTTP status comes
// back (the response body is discarded — this probe reads no facts). Only a
// transport-level failure is an error.
func uiProbeEngine(url, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), webIdentityFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build probe request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("probe %s: %w", url, err)
	}
	// Drain-and-close so the connection can be reused; a close error on a probe
	// is not a fact about the engine (Rule 15b: recorded, not swallowed — the
	// server side of this exchange is the engine's own access log).
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, webIdentityBodyLimit)); err != nil {
		fmt.Fprintf(os.Stderr, "gopi ui: probe body of %s: %v\n", url, err)
	}
	if err := resp.Body.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "gopi ui: probe close of %s: %v\n", url, err)
	}
	return nil
}

// uiPrintEngineIdentity prints the identity of the engine `gopi ui` is about to
// drive — while stderr is still the operator's.
//
//   - own engine: the facts are taken from the running server directly
//     (s.identityPayload) — no round trip, no guess.
//   - --server: the identity is FETCHED over the gated wire with the token the
//     TUI will use. A peer that does not answer, or answers 401, is REPORTED
//     (Rule 15) — the gated bind's commonest failure is a wrong/missing token,
//     and the operator must hear it before typing rather than as a stream of
//     failed turns.
func uiPrintEngineIdentity(engine *uiEngine, base string, opts uiOptions) {
	if engine != nil {
		webPrintIdentity(webCarrierUI, engine.identityURL, engine.srv.identityPayload())
		return
	}
	p, err := webFetchIdentity(base, opts.token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot read the engine identity at %s: %v\n", webCarrierUI, base, err)
		return
	}
	webPrintIdentity(webCarrierUI, webRouteURL(base, webIdentityPath, opts.token, false), p)
}

// uiServerAttachLogPath gives the engine-log destination for the --server case
// (no session of this process's own). An empty return means no destination is
// available and the caller reports it rather than falling back to a path
// nobody chose.
func uiServerAttachLogPath() string {
	dir, err := scratch.MkdirTemp(scratch.RuntimeClassUISession, uiEngineLogName)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, uiEngineLogName)
}

// engineLogLinePrefix is the tag the engine's own log lines carry — the same
// `[gopi] ` the terminal used to get, so a line moved into the file still reads
// as the engine speaking (one tag, one meaning).
const engineLogLinePrefix = "[gopi] "

// uiOpenEngineLog opens the per-session engine log beside the session's own
// artifacts and returns it. The caller prints the path, because a redirect the
// operator cannot find is indistinguishable from an engine that said nothing.
func uiOpenEngineLog(ag *agent.Agent) (*os.File, error) {
	path := uiEngineLogPath(ag)
	if path == "" {
		return nil, fmt.Errorf("no scratch directory is available for the engine log")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, engineLogPerm)
	if err != nil {
		return nil, fmt.Errorf("cannot write the engine log %s: %w", path, err)
	}
	return f, nil
}

// uiLoopbackBaseURL resolves the client's base URL from the listen address and
// the bound listener. A wildcard host ("" or 0.0.0.0) is reached at loopback,
// which is where the TUI runs.
func uiLoopbackBaseURL(addr string, bound net.Addr) (string, error) {
	port := ""
	if _, p, err := net.SplitHostPort(bound.String()); err == nil {
		port = p
	}
	if port == "" {
		if _, p, err := net.SplitHostPort(addr); err == nil {
			port = p
		}
	}
	if port == "" {
		port = strconv.Itoa(webDefaultPort)
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" || host == webWildcardHost4 || host == webWildcardHost6 {
		// A wildcard bind is reached at loopback, and it is reached by the SAME
		// spelling the client's default base URL uses.
		host = uiclient.LoopbackHostName
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

// uiThinkingNoticeFormat is printed when the live-reasoning switch is on: the
// engine will stream the model's own reasoning as `event: thinking` frames and
// THIS display draws them. It is said out loud — and it is not a warning — so a
// reader who sees a low-brightness `· ` block appearing above the answer knows
// what put it there (the same reason the tools notice exists).
const uiThinkingNoticeFormat = "%s: live reasoning ON — the model's own thinking streams to this display (drop `--thinking-live` for the default)"

// uiThinkingNotice is that notice with the carrier prefix.
var uiThinkingNotice = fmt.Sprintf(uiThinkingNoticeFormat, webCarrierUI)

// uiToolsNoticeFormat is printed when tool turns are enabled — the same trust
// warning the browser carrier prints, in this carrier's name.
const uiToolsNoticeFormat = "%s: tools ENABLED — turns run the full gopi toolset with no approval gate (loopback, local-shell trust)"

// uiToolsNotice is that notice with the carrier prefix (webCarrierUI, derived
// from the verb constant — the message can never name a surface that is not
// this one).
var uiToolsNotice = fmt.Sprintf(uiToolsNoticeFormat, webCarrierUI)

// uiUsageFingerprint is the substring the CLI contract suite asserts on
// `gopi ui -h`: the page's own FIRST line, so the assertion cannot drift from
// the text the binary prints (cliContractFingerprint's other entries mirror a
// literal; here the page itself is the source — Rule 13).
var uiUsageFingerprint = uiUsageTitle(uiUsage)

// uiUsageTitle returns a usage page's first line (the title the fingerprint
// asserts). Kept here rather than in the test file because the REAL binary's
// output is what it describes.
func uiUsageTitle(usage string) string {
	head, _, _ := strings.Cut(strings.TrimSpace(usage), "\n")
	return head
}

// uiUsage is the `gopi ui -h` page: what it can do first, the rationale in the
// tail (the capability-first shape the service verbs are held to).
const uiUsage = `gopi ui — the terminal chat surface: a scrolling transcript, live tool lines and an input box, against the same engine as ` + "`gopi web`" + `.

  gopi ui                     start a loopback chat server and open the TUI
  gopi ui --tools             the same, with the model's tools enabled
  gopi ui --addr HOST:PORT    bind a specific address (default ` + webDefaultAddrValue + `)
  gopi ui --server URL        attach to an ALREADY-RUNNING ` + "`gopi web`" + `
  gopi ui --token TOKEN       the per-run access token a NON-loopback server needs
  gopi ui --thinking-live     stream the model's own reasoning into the transcript
  gopi ui --dump-screen       write this surface's own screen state to a text file
  gopi ui -h                  this help (runs nothing)

Keys:
  Enter        send the typed message
  Ctrl+X       interrupt the turn in flight (not reported as an error)
  PgUp/PgDn    scroll the transcript
  Ctrl+A/Ctrl+E, Left/Right, Backspace/Delete, Ctrl+U   edit the input line
  Ctrl+C       quit (the chat session is finalized)

Options:
  --tools        tool-enabled turns (the full gopi toolset — the same switch
                 ` + "`gopi web --tools`" + ` takes). Without it the model answers from its
                 own knowledge and calls no tool.
  --addr H:P      listen address for the server this command starts (default
                 ` + webDefaultAddrValue + `; loopback, so no token is needed).
  --server URL   do not start a server: talk to the one already running.
  --token T      with a NON-loopback --server URL, the per-run access token the
                 banner printed (?t=<token>). Loopback needs none.
  --thinking-live
                 stream the model's live reasoning into the transcript: the
                 engine emits it as ` + "`event: thinking`" + ` frames and this display
                 draws them low-brightness, under a ` + "`· `" + ` gutter, above the
                 answer they belong to, as a rolling window of the newest few
                 lines. OFF by default (the env channel is ` + webThinkingEnv + `).
                 The same switch ` + "`gopi web`" + ` takes — one vocabulary, two
                 carriers, and a ` + "`--server`" + ` attach refuses it (a remote engine's
                 own switches are its own).

  --dump-screen[=/PATH]
                 ALSO write this surface's own interpreted screen to a text
                 file, refreshed as it repaints. Without a PATH the file lands
                 beside this run's session artifacts (a scratch directory when
                 the run has none) and the resolved path is printed before the
                 TUI takes the terminal; a PATH is created if its directory is
                 missing. The file carries geometry, the cursor's row and
                 column, the scroll region, the full-width cell grid, each row's
                 per-cell attribute runs, the scrollback, and an explicit list
                 of what the representation cannot see. A ".json" twin lands
                 beside it for machine reading.

                 WHY: this surface is TEXT. An agent can therefore read it as
                 text — the cell grid the renderer already produced — instead of
                 grabbing the desktop's pixels and asking a vision model what it
                 sees. The dump is precise (a row and a column, not "a panel on
                 the left"), it costs kilobytes instead of megabytes, and it
                 reads THIS surface rather than every window on the display.
  --dump-mode MODE
                 how the dump sink interprets the render stream: ansi (default —
                 the escape sequences bubbletea writes, interpreted as a
                 terminal would) or plain (bytes as literal text, with LF/CR as
                 the only controls).

Notes:
  The engine is not duplicated. ` + "`gopi ui`" + ` starts the same web plumbing
  ` + "`gopi web`" + ` uses — one persistent agent, one session, the same server-held
  history and the same SSE frames (delta / tool / command / queued / done).
  The TUI only DECODES and RENDERS them, which is why the conversation
  survives a restart of the surface and why no token is needed on loopback:
  the request never leaves this host.

  A user-initiated interrupt (Ctrl+X) is a NORMAL end of the turn — the
  status line says "interrupted", never an error. The engine is told to abandon
  the turn as well: cancelling only this side would leave the turn running on
  the server holding the lock the next message needs. A malformed or unknown
  frame is reported as a fault and is never rendered as assistant text.

  A turn waiting on the engine (another turn is already running) says so in the
  status line — "queued… <seconds>" — so a wait is never mistaken for a frozen
  surface.

  Environment: ` + webToolsEnv + ` enables tools (the SAME switch ` + "`gopi web`" + `
  reads — one tool switch, two carriers). ` + uiDumpEnv + ` asks for a screen
  dump (` + uiDumpEnv + `=1 for the default path, any other value IS the path)
  and ` + uiDumpModeEnv + ` sets its mode; the flags above win over both.
  --addr / --server / --token also take the joined --flag=value form.
`
