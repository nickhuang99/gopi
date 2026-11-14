package uiclient

import (
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// Config is everything the caller decides about the surface. The package owns
// the BEHAVIOUR (keys, streaming, layout); the caller owns the environment.
type Config struct {
	// Client is the SSE consumer. Required.
	Client *Client
	// Target is the server origin, shown in the header (normally
	// Client.BaseURL).
	Target string
	// Tools mirrors the server's --tools switch, for the header only. The
	// server decides what actually runs.
	Tools bool
	// In is the terminal input. nil = stdin. A pty (or the process's own
	// tty) is what makes this a real terminal session rather than a pipe.
	In io.Reader
	// Out is the terminal output. nil = stdout. Wrapping it in a
	// ScreenWriter is how the per-turn screen-write counts are MEASURED.
	Out io.Writer
	// Screen, when set, is the SAME counter as Out and is what turns the
	// per-turn write counts into a measured number rather than a claim. The
	// caller owns it (so it can report before and after), the model marks the
	// turn boundaries on it.
	Screen *ScreenWriter
	// Palette is the emphasis table the transcript draws with. It is the
	// CALLER's capability decision (cmd/agent/cli_ui.go, which owns the sink and
	// the environment flags) passed in as a table, exactly as the banded editor
	// takes one — the model resolves nothing itself. The zero value is the
	// monochrome table, so a caller that says nothing gets plain text.
	Palette lineedit.Palette
	// Width / Height are the terminal's geometry in cells, when the caller
	// knows it; 0 keeps the model's own default.
	//
	// WHY THE CALLER MUST SUPPLY IT (measured): Bubble Tea decides whether to
	// query the terminal size by testing its OUTPUT for being a terminal, and
	// the caller wraps that output in ScreenWriter — an io.Writer, not a tty —
	// so no WindowSizeMsg ever arrives and the model would lay out for its
	// 80x24 default in every terminal. The surface read the real geometry from
	// the tty itself instead (cmd/agent/cli_ui.go, TIOCGWINSZ) and passes it
	// here. A zero value is legal: the default is then the model's own.
	Width int
	// Height is the terminal's row count (see Width).
	Height int
	// OnInterrupt asks the ENGINE to abandon the turn this surface is showing.
	// The surface's own cancel only ends ITS side of the stream: the engine runs the turn to completion regardless (cmd/agent/cli_web.go ignores client
	// disconnect) and holds its single-turn lock, so without this hook Ctrl+X
	// would report an interruption that never happened and leave every later
	// message waiting behind the abandoned turn. The caller owns the transport
	// (it knows the server and its token), so the caller owns this call.
	// nil = no engine to tell (a test, or a client with no interrupt route).
	OnInterrupt func()
	// TurnBaseline is how many turns the session this surface is attached to
	// ALREADY HELD when the surface opened — the LEDGER's `chats.turn_count`
	// (`gopi session log` and `gopi session show` print the same number).
	//
	// WHY THE CALLER SUPPLIES IT INSTEAD OF THE MODEL READING IT: uiclient knows
	// nothing about the ledger, and the column lives in internal/tools — the
	// surface counts the turns IT ran, which is a different number the moment a
	// session is resumed or a `gopi notify` delivery opens a turn. Without the
	// baseline the bar restarts at 1 while every other surface says 17
	// (measured twice on 2026-10-27: the operator's own message "重启之后这个数字
	// 又归零了" and the resume notice "this session already holds 16 turn(s) —
	// the next turn is turn 17"). Zero is the ordinary fresh-session answer.
	TurnBaseline int
	// OnFrameGeometry, when set, is told the geometry the MODEL actually laid
	// the surface out for, once per frame that renders — the authoritative
	// number for anything that re-interprets this surface's paint stream (the
	// screen dump). It is reported rather than derived from Config.Width by the
	// observer because Config.Width is only the caller's INITIAL hint: Bubble Tea
	// may emit a WindowSizeMsg afterwards, and an observer that kept the hint
	// would re-interpret later frames at a size the model no longer uses.
	OnFrameGeometry func(cols, rows int)
}

// Stats is what one session measured: the renderer's own write activity. It is
// returned rather than logged so the CALLER decides how to report it (the
// `GOPI_UI_SELFTEST` line is one such decision; a report's evidence section is
// another).
type Stats struct {
	// RenderFrames is the number of Write calls the renderer made — one per
	// flushed frame.
	RenderFrames int64
	// RenderBytes is how many bytes those frames carried.
	RenderBytes int64
	// TurnFrames is the per-turn delta of RenderFrames: how many frames each
	// completed turn cost (the "does streaming repaint everything?" number).
	TurnFrames []int64
	// Turns is how many turns completed.
	Turns int
}

// Run starts the surface and blocks until the operator quits (Ctrl+C) or the
// terminal input ends. It returns the program's error, which is nil for an
// ordinary quit.
func Run(cfg Config) (Stats, error) {
	if cfg.Client == nil {
		return Stats{}, fmt.Errorf("uiclient: no client configured")
	}
	target := cfg.Target
	if target == "" {
		target = cfg.Client.BaseURL
	}
	m, err := newModel(cfg.Client, target, cfg.Tools)
	if err != nil {
		return Stats{}, err
	}
	m.screen = cfg.Screen
	m.pal = cfg.Palette
	// The caller's geometry wins over the model's default (see Config.Width).
	applyGeometry(&m, cfg)
	// The engine interrupt hook travels into the model: Ctrl+X is only honest if
	// the turn behind the surface is actually abandoned.
	m.onInterrupt = cfg.OnInterrupt
	// The turn baseline travels the same way, and for the same reason: the
	// caller is the layer that can read the ledger (Rule 14 — one producer for
	// the number every surface prints).
	m.turns = cfg.TurnBaseline
	// The frame-geometry seam travels the same way: the model reports what each
	// frame was laid out for, and the caller (which owns the sink) re-sizes the
	// grid to it.
	m.onFrameGeometry = cfg.OnFrameGeometry

	opts := []tea.ProgramOption{}
	if cfg.In != nil {
		opts = append(opts, tea.WithInput(cfg.In))
	}
	if cfg.Out != nil {
		opts = append(opts, tea.WithOutput(cfg.Out))
	}
	// The transport stays alive across turns; the model cancels a turn, never
	// the program, so a cancelled turn cannot take the TUI down with it.
	p := tea.NewProgram(m, opts...)
	final, err := p.Run()
	stats := Stats{}
	if fm, ok := final.(model); ok {
		stats = fm.stats()
	}
	if cfg.Screen != nil {
		// The renderer's own totals override the model's: the counter lives on
		// the writer, so it sees every frame even one written after the last
		// turn concluded.
		stats.RenderFrames = cfg.Screen.Calls()
		stats.RenderBytes = cfg.Screen.Bytes()
	}
	if err != nil {
		return stats, fmt.Errorf("ui: %w", err)
	}
	return stats, nil
}

// applyGeometry applies the caller's terminal geometry to the model. A zero
// field means "not known": the model keeps the default it was built with, so a
// caller that cannot measure its terminal degrades to the previous behaviour
// rather than to a zero-width layout.
func applyGeometry(m *model, cfg Config) {
	if cfg.Width > 0 {
		m.width = cfg.Width
	}
	if cfg.Height > 0 {
		m.height = cfg.Height
	}
}
