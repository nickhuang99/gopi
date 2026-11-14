package shot

// terminalregistry.go — WHICH TERMINAL A FILM RUNS INSIDE, as DATA.
//
// WHY THIS FILE EXISTS. FilmTerminalBinary used to BE the terminal: one
// constant ("xterm") and one flag protocol (-geometry / -e / -T / -l / -lf)
// written into StartFilmTerminal. That is correct for xterm and wrong for every
// other emulator, because a terminal's launch shape is not a convention — each
// one chooses its own exec protocol, its own geometry unit and its own way of
// being addressed:
//
//	xterm           -geometry WxC in CELLS, -e ARGV, title via -T, its own -l/-lf session log
//	xfce4-terminal  --geometry=WxC in CELLS, -x ARGV, title via -T, no session log at all
//	gnome-terminal  --geometry=WxC … and NOT USABLE HERE (see below)
//
// and the operator's desktop is not necessarily xterm's: on zpeer it is
// xfce4-terminal, so a film made with xterm is a picture of a terminal the
// operator never sees. The fix is not a second `if` in the starter — it is a
// REGISTRY: one record per terminal carrying its launch shape, and ONE starter
// that executes a record (Rule 2/14: one definition, referenced, never mirrored).
//
// THE SCREENSHOT IS OF THE TERMINAL'S OWN WINDOW. A film is the product's
// surface photographed, and the product must be told the geometry the film's
// layout was drawn at (the producer's --cols/--rows). This pipeline passes it as
// a CELL geometry, which is why xfce4-terminal is a drop-in for xterm and
// gnome-terminal is not: measured 2026-10-03 on a private display, xfce4-terminal
// reports the requested cell grid through `stty size` (80x24 and 117x26 both
// exact) with no font knowledge, while gnome-terminal reports 24x80 for a
// `--geometry=117x26` request — its geometry is not the client's to set and its
// window is not the client's to draw.
//
// gnome-terminal IS THEREFORE A RECORD WITH A REFUSAL, never a silent fallback:
// the binary does not draw the window — it asks the D-Bus-activated service
// org.gnome.Terminal to draw it, and that service uses the display IT inherits,
// which is not the private display this run owns. Measured 2026-10-03 on a
// private Xvfb: with no session bus reachable the command exits 1 with "Error
// constructing proxy for org.gnome.Terminal: … Timeout was reached" and maps
// nothing; with a session bus it maps only its own 10x10 placeholder (the real
// toplevel is drawn by the shared service, elsewhere). Neither is a film of this
// run's surface. A refusal that NAMES that cause and lists the terminals that do
// work is the honest form (Rule 15); preparing the bus and a window manager is
// an operator prerequisite, not something this driver installs (Rule 35).
//
// WHERE THE SELECTION COMES FROM. The name is resolved by ONE function
// (FilmTerminal resolveFilmTerminalSpec, below) through the project's standing
// CLI > env > settings > default ranking, the same ranking (and the same shape)
// ResolveTmpDirConfigured and ResolveSandboxRoot use: the flag channel is read
// by the verb that owns the flag (cmd/agent) and handed to this one resolver, so
// there is still exactly ONE reader of the terminal's identity.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/lineedit"
)

// The terminal NAMES as the operator spells them. They are the values every
// channel takes, so they are constants rather than inline strings (Rule 1b).
const (
	FilmTerminalNameXterm   = "xterm"
	FilmTerminalNameXfce4   = "xfce4-terminal"
	FilmTerminalNameGnome   = "gnome-terminal"
	FilmTerminalNameListSep = ", "
)

// FilmTerminalDefault is the built-in default — the terminal nobody has to ask
// for. It is xfce4-terminal, by the operator's 2026-10-04 ruling: a film of a
// CJK surface must NEVER be made in xterm. xterm's default font is the X11 core
// bitmap `fixed` (6x13) and its charset is ISO8859-1 — measured on this host
// (`xlsfonts -ll fixed`): CHARSET_REGISTRY ISO8859, CHARSET_ENCODING 1 — so it
// carries ZERO CJK glyphs and every Chinese cell films as a □ fallback box,
// while the mis-measured advance width shoves the neighbouring ASCII along.
// Measured 2026-10-04 on one private Xvfb, the SAME line in both terminals: the
// CJK token is hollow boxes in xterm (no interior ink, no component but the
// box) and a real glyph in xfce4-terminal (VTE's fontconfig path finds a CJK
// face and draws it) — see devdoc/film-cjk-default-terminal.md.
//
// The operator named gnome-terminal and xfce4-terminal, both of which go through
// fontconfig with TrueType and fallback; only the second is USABLE here, because
// gnome-terminal is the registry's refused row (its binary does not draw the
// window). Among the terminals that render Chinese, the default must also be one
// a film can actually run inside — so the default is xfce4-terminal.
//
// xterm is NOT removed and its historical measurements stay valid: it remains a
// selectable name (`--film-terminal ` + FilmTerminalNameXterm), and every piece
// of this pipeline's arithmetic was taken with it — the settled grab, the
// frame-per-beat ledger, the 706x342 = 117x26 x 6.034 px/cell geometry, the
// legibility floor. What changed is which terminal a caller who said NOTHING
// gets. A default is not a preference; it is the value nobody had to ask for,
// and it must be a terminal that draws the surface the film is FOR. A host that
// has no xfce4-terminal is not left guessing: the starter refuses by name and
// prints the registry's usable list (Rule 15 — never a silent swap).
const FilmTerminalDefault = FilmTerminalNameXfce4

// The knob's three channels (Rule 2/34): the flag is composed by the verb that
// owns it (`--` + FilmTerminalFlagName), and env + settings key are the two
// channels this resolver reads. One definition each, referenced by the catalog
// row in internal/config — never mirrored (Rules 13/14).
// The knob's three names are NOT re-typed here: they are internal/config's
// (film_terminal.go), which this package imports — one definition each, consumed
// by the catalog, the flag parser and the resolver below (Rules 13/14). A short
// local alias keeps the resolver readable without a second spelling.
const (
	// FilmTerminalEnv is the deployed configuration channel.
	FilmTerminalEnv = config.FilmTerminalEnv
	// FilmTerminalSettingsKey is the declarative channel, the settings.json key.
	FilmTerminalSettingsKey = config.FilmTerminalSettingsKey
	// FilmTerminalFlagName is the CLI spelling WITHOUT dashes (`--film-terminal
	// NAME`), the same bare form the catalog uses for every other flag.
	FilmTerminalFlagName = config.FilmTerminalFlagName
)

// The channel LABELS a resolution reports, so a caller can say where the name
// came from instead of presenting a default as a discovery. They are the channel
// spellings themselves — not a second vocabulary a reader would map back.
const (
	FilmTerminalSourceFlag = "--" + FilmTerminalFlagName
	// FilmTerminalSourceEnv names BOTH spellings that land here, and that is a
	// fact rather than a hedge: the flag is hoisted into this env name before the
	// subcommand chain (config.BootstrapEnvFromArgs → PublishFilmTerminalFlag), so
	// a value that arrived on the command line and a value that arrived from the
	// operator's shell are THE SAME BYTES at the one site that reads them. Naming
	// only the env var would tell an operator who typed the flag that a variable
	// they never set decided their film.
	FilmTerminalSourceEnv      = "from " + FilmTerminalFlagName + " / " + FilmTerminalEnv
	FilmTerminalSourceSettings = "from the settings key " + FilmTerminalSettingsKey
	FilmTerminalSourceDefault  = "the built-in default"
)

// The WINDOW SELECTION rules, named. A rule is a POLICY with a measurement
// behind it, and a registry field is how a reader sees which one a terminal is
// held to.
const (
	// FilmWindowRulePIDTitleRect is the rule every usable terminal is held to:
	// the window must publish this process's pid (identity, from the kernel's
	// process table through _NET_WM_PID — "the first X client on the display" is
	// the heuristic that photographs the wrong window), the TITLE disambiguates
	// when one pid owns several windows, and the rectangle must be real (a
	// placeholder that is 10x10 or unmeasurable is not a picture). Measured
	// 2026-10-03: xfce4-terminal maps THREE top-levels under one pid — its own
	// 10x10 "Xfce Terminal" placeholder, a 1x1 auxiliary, and the real window
	// carrying the -T title — so the pid alone is not enough and a size filter
	// alone is not identity.
	FilmWindowRulePIDTitleRect = "pid+title+measured-rect"
)

// The UNKNOWN-NAME reason and the USABLE-LIST spelling. Every refusal that
// names a terminal composes these, so the list an operator reads is the same
// list in the resolver, in the starter and in the help (Rule 13).
const (
	// filmTerminalExactNameReason is WHY an unusable name is refused rather than
	// searched for: there is no PATH lookup and no substring match behind this
	// name, so a value one edit away from a real terminal is a typo, and a typo
	// that silently films with some other emulator is a picture of a terminal
	// nobody chose.
	filmTerminalExactNameReason = "the filmed terminal is an EXACT name from this driver's registry (no PATH lookup, no substring match), so a name that is one edit away from a real terminal is a typo rather than a request"
	// filmTerminalRefusalConsequence states what happens next, so the refusal is
	// actionable rather than merely loud: NOTHING falls back.
	filmTerminalRefusalConsequence = "nothing falls back to the default: a film whose terminal nobody chose would be a picture of the wrong screen"
)

// FilmTerminalSpec is ONE terminal's LAUNCH SHAPE — everything the starter needs
// to run a command inside it, plus the reason it is unavailable when it is.
type FilmTerminalSpec struct {
	// Name is the operator-facing name (the value every channel takes).
	Name string
	// Binary is the executable name searched on PATH (the SAME string as Name
	// for all three terminals, and kept separate because a name is not a
	// promise about a binary).
	Binary string
	// GeometryFlag is how a CELL geometry is requested (`-geometry` for xterm,
	// `--geometry=` for xfce4-terminal, which joins its value with `=`).
	GeometryFlag string
	// GeometryJoined says the flag carries its value with the separator below
	// (`--geometry=117x26`) instead of in the next argv slot.
	GeometryJoined bool
	// GeometryAccepted is false for a terminal whose geometry request is NOT the
	// client's to make (gnome-terminal is told one and its child reports a
	// different grid). A caller that needs the film's geometry is REFUSED
	// explicitly rather than handed a frame drawn at another size.
	GeometryAccepted bool
	// GeometrySeparator joins flag and value when GeometryJoined.
	GeometrySeparator string
	// ExecFlag is how the command to run is named ("" when the terminal takes it
	// after ArgSeparator).
	ExecFlag string
	// ExecSeparator is the token that ends the terminal's own options, after
	// which the command follows (`--` for gnome-terminal).
	ExecSeparator string
	// ArgSeparatorEndsOptions says the separator above is EMITTED before the
	// command (gnome-terminal needs it; xterm's -e already ends its options).
	ArgSeparatorEndsOptions bool
	// TitleFlag names the window ("" = this terminal has no title channel).
	TitleFlag string
	// GroundBackgroundFlag / GroundForegroundFlag are how this terminal spells
	// the two settings that materialise the GROUND the surface's palettes were
	// composed against (internal/lineedit.GroundBackgroundHex / …ForegroundHex).
	// A terminal with no such channel for one of them cannot be filmed at all:
	// the surface's golds are legible on a dark ground and illegible on the
	// default white one, so TerminalArgs REFUSES a pair it cannot express rather
	// than photographing a white window ("" = no channel for that half).
	GroundBackgroundFlag string
	GroundForegroundFlag string
	// GroundJoined says the ground flag carries its colour with the separator
	// below (`--color-bg=#131926`) instead of in the next argv slot — the same
	// distinction GeometryJoined carries for the geometry, for the same reason.
	GroundJoined bool
	// SessionLogFlags is the terminal's own session-log switch (nil = it has
	// none, so the filmed command's output reaches the pty and nothing else).
	SessionLogFlags []string
	// WindowRule names the selection predicate (see the FilmWindowRule* consts).
	WindowRule string
	// Refusal is NON-EMPTY for a terminal that is KNOWN but NOT USABLE here, and
	// holds the whole reason plus what to use instead — as a TEMPLATE whose one
	// %s is the usable list (refusalText below fills it). A spec with a refusal
	// is never started, and never silently replaced.
	//
	// WHY A TEMPLATE AND NOT THE FINISHED SENTENCE: the usable list is derived
	// from THIS table, so a row carrying the finished sentence would make the
	// table depend on itself — measured 2026-10-03, that recursion overflowed the
	// stack the moment the list was asked for. A function evaluated at the call
	// keeps one definition of the sentence and one of the list, with no cycle.
	Refusal string
}

// usable reports whether this terminal can be filmed at all.
func (s FilmTerminalSpec) usable() bool { return s.Refusal == "" }

// SupportsSessionLog reports whether this terminal can tee the FILMED COMMAND's
// own output to a file (xterm's -l/-lf does; the VTE terminals hold the child's
// pty and offer no counterpart).
//
// It is a QUESTION, not a precondition: a film is the frames, and the session log
// is its EVIDENCE — so a terminal without the switch still films, and the missing
// evidence is ANNOUNCED rather than silently absent (Rule 15). Measured
// 2026-10-03: refusing the run instead was wrong in the most expensive way, since
// the operator's own terminal (xfce4-terminal) is exactly the one without it.
func (s FilmTerminalSpec) SupportsSessionLog() bool { return len(s.SessionLogFlags) > 0 }

// SessionLogAbsentNotice is the sentence the driver prints once when the filmed
// terminal has no session-log channel. It names WHAT is missing and WHERE the
// evidence then lives, so nobody later reads an empty producer.log as "the
// producer printed nothing".
func SessionLogAbsentNotice(spec FilmTerminalSpec, sessionLogPath string) string {
	return fmt.Sprintf("%s offers no session-log switch, so the filmed command's own output cannot be captured to %s — it reaches that terminal's pty only (%s keeps its own -l/-lf channel). The film itself is unaffected: the frames are the evidence, and a producer that died inside the terminal is visible in them",
		spec.Name, sessionLogPath, FilmTerminalNameXterm)
}

// refusalText renders the refusal for a known-but-unusable terminal, filling the
// template's one %s with the usable list. Calling it on a USABLE spec is how a
// caller finds out it asked the wrong question: it returns "" rather than
// inventing a reason (Rule 15 — a refusal the driver does not have is a refusal
// nobody can act on).
func (s FilmTerminalSpec) refusalText() string {
	if s.usable() {
		return ""
	}
	return fmt.Sprintf(s.Refusal, FilmTerminalUsableList())
}

// TerminalArgs composes the FULL argv for running argv inside this terminal.
//
// groundArgs returns this terminal's argv items for the GROUND the surface's
// palettes were composed against, or a refusal naming the half it cannot express.
//
// WHY THIS IS NOT OPTIONAL, AND WHY IT IS A REFUSAL RATHER THAN A DEFAULT. The
// chat surface draws with FOREGROUND tables whose contrast was measured against a
// dark ground (internal/lineedit: Hermes' golds on #131926; `HermesLightPalette`
// exists for white). A terminal that keeps its own default therefore decides the
// film's ground by accident — and on this host xterm's default is WHITE, which is
// the measured defect that produced this function (2026-10-04: 150 frames of
// 96 % pure white with the gold text at roughly 1.2:1 against it). Substituting
// something here would be the driver choosing the film's colours; refusing names
// the terminal that cannot be told, and the caller picks another.
func (s FilmTerminalSpec) groundArgs() ([]string, error) {
	if s.GroundBackgroundFlag == "" || s.GroundForegroundFlag == "" {
		return nil, fmt.Errorf("film terminal %s cannot be given the surface's ground: this driver has no %s/%s switch for it, and the chat surface's palettes are composed against a dark ground (%s on %s) — filming it would photograph the terminal's own default instead (internal/shot's registry is where the switch is declared)",
			s.Name, lineedit.GroundBackgroundName, lineedit.GroundForegroundName,
			lineedit.GroundForegroundHex, lineedit.GroundBackgroundHex)
	}
	pairs := []struct{ flag, value string }{
		{s.GroundBackgroundFlag, lineedit.GroundBackgroundHex},
		{s.GroundForegroundFlag, lineedit.GroundForegroundHex},
	}
	args := make([]string, 0, len(pairs)*2)
	for _, p := range pairs {
		if s.GroundJoined {
			args = append(args, p.flag+s.GeometrySeparator+p.value)
			continue
		}
		args = append(args, p.flag, p.value)
	}
	return args, nil
}

// TerminalArgs builds the whole argv this terminal is started with: its own
// geometry request, the window title, the GROUND the surface is drawn on, its
// session-log switch when it has one, and the command to run inside it.
//
// It is the ONE place a terminal's flag protocol is spelled: the starter passes
// the spec in and never branches on the terminal's name again. It refuses — with
// the terminal's own words — the three shapes it cannot express honestly: a
// session log for a terminal that has no such switch (writing the flag anyway
// would start the terminal with an argument it does not know, or worse, silently
// film a command that is not the one asked for), a geometry for a terminal whose
// child does not report the requested grid, and a GROUND for a terminal this
// driver has no switch for (see groundArgs — the film would otherwise be a
// picture of a ground nobody chose).
func (s FilmTerminalSpec) TerminalArgs(geometryCells string, argv []string, title, sessionLogPath string) ([]string, error) {
	if strings.TrimSpace(geometryCells) == "" {
		return nil, fmt.Errorf("film terminal %s: no geometry was given (the layout's own WxC, e.g. 117x26)", s.Name)
	}
	if !s.GeometryAccepted {
		return nil, fmt.Errorf("film terminal %s cannot be asked for the film's geometry: %s", s.Name, filmTerminalGeometryRefusedReason)
	}
	ground, err := s.groundArgs()
	if err != nil {
		return nil, err
	}
	args := []string{}
	if s.GeometryJoined {
		args = append(args, s.GeometryFlag+s.GeometrySeparator+geometryCells)
	} else {
		args = append(args, s.GeometryFlag, geometryCells)
	}
	if title != "" && s.TitleFlag != "" {
		args = append(args, s.TitleFlag, title)
	}
	args = append(args, ground...)
	if sessionLogPath != "" && s.SupportsSessionLog() {
		args = append(args, s.SessionLogFlags...)
		args = append(args, sessionLogPath)
	}
	if len(argv) == 0 {
		return args, nil
	}
	if s.ArgSeparatorEndsOptions {
		args = append(args, s.ExecSeparator)
	} else if s.ExecFlag != "" {
		args = append(args, s.ExecFlag)
	}
	return append(args, argv...), nil
}

// The two per-terminal reasons above, named so the refusal text and the devdoc
// cannot drift (Rule 13).
const (
	filmTerminalGeometryRefusedReason = "its geometry request is not the client's to make — gnome-terminal accepts --geometry and the child inside it reports a DIFFERENT grid, so the film would be photographed at a size its own layout was not drawn for"
	filmTerminalNoSessionLogReason    = "its emulator holds the child's pty and offers no log switch (xterm's -l/-lf has no counterpart), so a producer that dies inside it leaves nothing behind; the film's producer output is a file the DRIVER enables, not something this registry can conjure"
)

// filmTerminalRegistry is the registry itself — the ONE table. Order is the order
// the usable list is printed in.
//
// EVERY USABLE ROW CARRIES ITS GROUND SWITCHES, and that is a requirement rather
// than a nicety: the chat surface's palettes are FOREGROUND tables composed
// against a dark ground (internal/lineedit's ground constants), so a terminal
// started on its own default — a white xterm on this host — photographs a white
// window with pale-gold text on it. The rows below declare how their own terminal
// spells those two settings; a terminal this driver could not give a ground is
// REFUSED by TerminalArgs rather than filmed on a default nobody chose.
func filmTerminalRegistry() []FilmTerminalSpec {
	return []FilmTerminalSpec{
		{
			Name:             FilmTerminalNameXterm,
			Binary:           FilmTerminalNameXterm,
			GeometryFlag:     filmTerminalGeometryFlag,
			GeometryAccepted: true,
			ExecFlag:         filmTerminalExecFlag,
			TitleFlag:        filmTerminalTitleFlag,
			SessionLogFlags:  []string{filmTerminalLogFlag, filmTerminalLogFileFlag},
			WindowRule:       FilmWindowRulePIDTitleRect,
			// xterm takes the ground as two value-carrying options, the colour in
			// the next argv slot.
			GroundBackgroundFlag: filmTerminalXtermGroundBGFlag,
			GroundForegroundFlag: filmTerminalXtermGroundFGFlag,
		},
		{
			Name:              FilmTerminalNameXfce4,
			Binary:            FilmTerminalNameXfce4,
			GeometryFlag:      filmTerminalXfceGeometryFlag,
			GeometryJoined:    true,
			GeometrySeparator: filmTerminalXfceFlagSeparator,
			GeometryAccepted:  true,
			ExecFlag:          filmTerminalXfceExecFlag,
			TitleFlag:         filmTerminalXfceTitleFlag,
			WindowRule:        FilmWindowRulePIDTitleRect,
			// xfce4-terminal JOINS the colour with `=`, like its geometry.
			GroundBackgroundFlag: filmTerminalXfceGroundBGFlag,
			GroundForegroundFlag: filmTerminalXfceGroundFGFlag,
			GroundJoined:         true,
		},
		{
			Name:                    FilmTerminalNameGnome,
			Binary:                  FilmTerminalNameGnome,
			GeometryFlag:            filmTerminalGnomeGeometryFlag,
			GeometryJoined:          true,
			GeometrySeparator:       filmTerminalXfceFlagSeparator,
			GeometryAccepted:        false,
			ExecSeparator:           filmTerminalGnomeExecSeparator,
			ArgSeparatorEndsOptions: true,
			TitleFlag:               filmTerminalGnomeTitleFlag,
			WindowRule:              FilmWindowRulePIDTitleRect,
			Refusal:                 filmTerminalGnomeRefusalTemplate,
		},
	}
}

// The xfce4-terminal / gnome-terminal protocol constants. Each is written ONCE
// and referenced from the registry row, so a flag spelling cannot differ between
// the table and the text (Rule 1b).
const (
	// filmTerminalXfceGeometryFlag is xfce4-terminal's geometry switch; it JOINS
	// its value with `=`.
	filmTerminalXfceGeometryFlag = "--geometry"
	// filmTerminalXfceFlagSeparator is that join.
	filmTerminalXfceFlagSeparator = "="
	// filmTerminalXfceExecFlag is xfce4-terminal's exec switch. Measured
	// 2026-10-03: -x takes the command and its arguments as the FOLLOWING argv
	// items (no `--` needed, no shell involved), and it is emitted only when
	// argv is non-empty — a bare `-x` with nothing after it would start a
	// terminal that immediately fails.
	filmTerminalXfceExecFlag = "-x"
	// filmTerminalXfceTitleFlag is xfce4-terminal's window-title switch.
	filmTerminalXfceTitleFlag = "-T"
	// filmTerminalGnomeGeometryFlag / filmTerminalGnomeExecSeparator are
	// gnome-terminal's own spellings, kept because the REFUSAL text names them
	// (a reader who wants to check the claim needs the exact command line).
	filmTerminalGnomeGeometryFlag  = "--geometry"
	filmTerminalGnomeExecSeparator = "--"
	// filmTerminalGnomeTitleFlag is gnome-terminal's title switch, likewise.
	filmTerminalGnomeTitleFlag = "-t"

	// The two GROUND switches, per terminal. They carry the colours internal/lineedit's
	// ground constants name — the dark ground the gold chrome was composed against —
	// and they are the reason a filmed window is a picture of the product's
	// surface rather than of an unconfigured emulator's default (see
	// FilmTerminalSpec.GroundBackgroundFlag).
	//
	// xterm: two options, the colour in the NEXT argv slot.
	filmTerminalXtermGroundBGFlag = "-bg"
	filmTerminalXtermGroundFGFlag = "-fg"
	// xfce4-terminal: the same two settings, each JOINED with `=`.
	filmTerminalXfceGroundBGFlag = "--color-bg"
	filmTerminalXfceGroundFGFlag = "--color-text"
)

// filmTerminalGnomeRefusal is gnome-terminal's refusal — the WHOLE sentence,
// printed by the resolver and by the starter alike. It is a FUNCTION, not a
// constant, for one reason worth stating: it quotes FilmTerminalUsableList, which
// is derived from THIS registry, and a package-level value would close an
// initialization cycle (the table would need the sentence, the sentence the
// table). A function is evaluated at the moment it is used, so the list inside
// the sentence is always the list of the version doing the talking.
//
// Every clause is a measurement, not a hypothesis: the binary asks a D-Bus
// service to draw the window, the service uses the display it inherits, and the
// two observable outcomes on a private display are the proxy error and the
// placeholder. Naming the cause matters more than naming the remedy: the failure
// mode being prevented is "the film silently photographed the operator's own
// session, or nothing at all".
const filmTerminalGnomeRefusalTemplate = FilmTerminalNameGnome + " cannot be filmed: its binary does not draw the window — it asks the D-Bus-activated service org.gnome.Terminal to draw it, and that service uses the display IT inherits, not the private display this run owns (measured 2026-10-03 on a private Xvfb: with no session bus reachable `gnome-terminal -- …` exits 1 with \"Error constructing proxy for org.gnome.Terminal: … Timeout was reached\" and maps nothing; with a session bus it maps only its own 10x10 placeholder window). Use %s instead. A host that must film inside gnome-terminal needs its own session bus AND a window manager before the command — both are operator prerequisites this driver does not install (it starts no daemon and adds no dependency)"

// FilmTerminalGnomeRefusal is the RENDERED refusal — the public face of the gnome
// row's template, for a caller that wants to quote it without resolving the spec
// first (the help page does). It goes through the table, so it cannot drift from
// what the resolver prints.
func FilmTerminalGnomeRefusal() string {
	for _, s := range filmTerminalRegistry() {
		if s.Name == FilmTerminalNameGnome {
			return s.refusalText()
		}
	}
	return ""
}

// FilmTerminalUsableList is the whole list of terminals a film can actually run
// inside, derived from the registry (never typed): every refusal above and every
// unknown-name error prints THIS, so the list an operator reads cannot be a
// stale copy of the table.
func FilmTerminalUsableList() string {
	names := make([]string, 0, len(filmTerminalRegistry()))
	for _, s := range filmTerminalRegistry() {
		if s.usable() {
			names = append(names, s.Name)
		}
	}
	return strings.Join(names, FilmTerminalNameListSep)
}

// FilmTerminalKnownNames is every name the registry knows, usable or not. A
// caller that reports a name which is known-but-refused differently from one
// that is unknown entirely needs both halves.
func FilmTerminalKnownNames() []string {
	specs := filmTerminalRegistry()
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	return names
}

// FilmTerminalSpecByName resolves ONE name against the registry.
//
// It is EXACT and CASE-SENSITIVE, and both refusals carry the reason and the
// usable list (Rule 15): a name matching nothing is told it is not a terminal
// this driver knows, and a name matching a KNOWN but unusable terminal is told
// that terminal's own reason — the two are different facts and must not read as
// the same sentence.
func FilmTerminalSpecByName(name string) (FilmTerminalSpec, error) {
	want := strings.TrimSpace(name)
	if want == "" {
		return FilmTerminalSpec{}, fmt.Errorf("no filmed terminal was named: %s", filmTerminalExactNameReason)
	}
	for _, s := range filmTerminalRegistry() {
		if s.Name != want {
			continue
		}
		if !s.usable() {
			return FilmTerminalSpec{}, fmt.Errorf("film terminal %q is known but NOT usable: %s", want, s.refusalText())
		}
		return s, nil
	}
	return FilmTerminalSpec{}, fmt.Errorf("film terminal %q is not a terminal this film driver knows (%v): %s. TERMINALS THAT WORK: %s. %s",
		want, FilmTerminalKnownNames(), filmTerminalExactNameReason, FilmTerminalUsableList(), filmTerminalRefusalConsequence)
}

// ResolveFilmTerminal resolves the filmed terminal through its ONE reader
// (Rule 2/34), and reports WHICH channel decided:
//
//	rank 1  flagValue      — the verb's own `--film-terminal NAME`, already read
//	                         by the verb that owns the flag (a flag's value is
//	                         not a config-file read, so it cannot live below the
//	                         CLI: the caller hands it in)
//	rank 2  GOPI_FILM_TERMINAL
//	rank 3  settings.json "film_terminal"
//	rank 4  the built-in default (FilmTerminalDefault, a name — never a copy of
//	         it, so this line cannot drift from the constant below)
//
// An UNREADABLE settings file is an ERROR, never "nobody asked" (Rule 15): a
// film whose terminal cannot be determined must not be made with a terminal
// nobody chose. A name given on ANY channel is resolved through
// FilmTerminalSpecByName, so a typo, an unknown name and a known-but-unusable
// terminal all keep their own refusal, whichever channel carried it.
func ResolveFilmTerminal(flagValue string) (FilmTerminalSpec, string, error) {
	if v := strings.TrimSpace(flagValue); v != "" {
		spec, err := FilmTerminalSpecByName(v)
		if err != nil {
			return FilmTerminalSpec{}, FilmTerminalSourceFlag, err
		}
		return spec, FilmTerminalSourceFlag, nil
	}
	if v := strings.TrimSpace(os.Getenv(FilmTerminalEnv)); v != "" {
		spec, err := FilmTerminalSpecByName(v)
		if err != nil {
			return FilmTerminalSpec{}, FilmTerminalSourceEnv, err
		}
		return spec, FilmTerminalSourceEnv, nil
	}
	cfg, err := config.FromSettingsFile(config.ResolveSettingsPath())
	if err != nil {
		return FilmTerminalSpec{}, FilmTerminalSourceSettings, fmt.Errorf("the filmed terminal could not be resolved: the settings file %s cannot be read (%w), and reading a terminal name out of a file that failed to load would pick one nobody chose",
			config.ResolveSettingsPath(), err)
	}
	if raw := strings.TrimSpace(cfg.FilmTerminal); raw != "" {
		spec, serr := FilmTerminalSpecByName(raw)
		if serr != nil {
			return FilmTerminalSpec{}, FilmTerminalSourceSettings, serr
		}
		return spec, FilmTerminalSourceSettings, nil
	}
	spec, serr := FilmTerminalSpecByName(FilmTerminalDefault)
	if serr != nil {
		return FilmTerminalSpec{}, FilmTerminalSourceDefault, serr
	}
	return spec, FilmTerminalSourceDefault, nil
}

// FilmTerminalHelpRow is the usage page's sentence for the knob, composed from
// the registry so the help cannot offer a name the driver would refuse (Rules
// 1/13: a value spelled twice is two definitions, and the help is a place a
// reader believes).
func FilmTerminalHelpRow() string {
	return "             " + FilmTerminalSourceFlag + " NAME films it inside NAME instead of the\n" +
		"             built-in default (" + FilmTerminalDefault + "). Usable names: " + FilmTerminalUsableList() + ".\n" +
		"             Same knob via " + FilmTerminalEnv + " or the settings key " + FilmTerminalSettingsKey + ".\n"
}
