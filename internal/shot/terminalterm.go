package shot

// terminalterm.go — the TERMINAL ON A PRIVATE DISPLAY: start an xterm on the
// display this run owns, find ITS window by PID, and grab that window once the
// picture has SETTLED.
//
// WHY A TERMINAL WINDOW AND NOT A PTY. The whole point of filming a chat run is
// to photograph the PRODUCT'S OWN SURFACE: the banner, the editable band, the
// status row — the pixels a human sees. A pty capture returns the byte stream
// and cannot show a layout; a private X server with ONE xterm on it is a
// deterministic source of pixels (nothing else maps, moves or repaints a window
// there), and a terminal-emulator-independent one: the frame is what the terminal
// drew, not what this process believes it drew.
//
// WINDOW IDENTITY IS THE PID, NEVER A SEARCH. "The window this run started" has
// exactly one honest witness — the client that publishes xterm's own pid in
// _NET_WM_PID — so the match is made against that and nothing else
// (matchWindowByPID's discipline, one level up: there is exactly one candidate
// here, and the rule is still applied so a second xterm on the same display can
// never be photographed by accident). A window that never publishes a pid is
// REPORTED, not silently accepted (Rule 15).
//
// SETTLED GRAB — THE JUDGEMENT THIS FILE OWNS. scripts/replay/replay.sh used to
// assume a single capture per beat was enough, because the replay blocks on the
// driver's acknowledgement of a beat so "the screen cannot change" while it
// photographs. MEASURED 2026-10-02 (140-beat run, private Xvfb): the assumption
// is false — a second grab taken 0.35s after the first, with the producer still
// blocked on the SAME beat, differed on 8 of 140 beats, i.e. the first grab had
// photographed the PREVIOUS beat's screen. A still surface and a lost repaint
// then became the same number.
//
// So a frame is taken REPEATEDLY (bounded by filmGrabDefaultTries, spaced by
// filmGrabSettle) and the first grab that AGREES with the one before it is
// delivered: on a blocked producer the settled screen is a fixed point, so
// agreement is the signal that the repaint finished. It never loops forever — a
// window that never settles is delivered with settled=false and REPORTED, never
// hidden.
//
// ZERO NEW DEPENDENCIES (Rule 35): xterm is the existing bare-system dependency
// (the operator's decision was Xvfb + xterm), and this file installs nothing. The
// window listing is this package's own X11 client (x11list.go) and the pixels come
// from its own GetImage path (x11rect.go) — no xdotool, no xwininfo, no python3.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

// The terminal's vocabulary. Every literal is a named constant (Rule 1/1b): the
// binary name, its geometry flag, the env name DISPLAY, and the environment
// override this file performs are all written here once and referenced.
const (
	// FilmTerminalBinary is the terminal this file USED to hard-code, kept as a
	// DEPRECATED alias of the registry's default (Rule 30e of the epic: no
	// behaviour change for a caller that named nothing) so an existing reader
	// compiles and means the same thing. NEW callers resolve a terminal through
	// FilmTerminalDefault / ResolveFilmTerminal (terminalregistry.go) and start
	// it with NewFilmTerminalActor — a terminal's identity is a registry row, not
	// a constant, and this name exists only so nobody re-types "xterm".
	//
	// WHY xterm REMAINS THE DEFAULT: it is the terminal every measurement of this
	// pipeline was taken with, and it draws on its OWN render thread, so a grab
	// takes what the kernel has already committed. The VTE family commits a
	// repaint later on the DCS path (a 2026-10-02 claim of "12–13 frames lost"
	// was NOT reproduced under the Go driver — see
	// devdoc/film-terminal-selection.md, which records both the original
	// measurement and the Go-side refutation), and a terminal is now SELECTABLE
	// precisely because one operator's desktop is not another's.
	//
	// Deprecated: use FilmTerminalDefault, or resolve a spec.
	FilmTerminalBinary = FilmTerminalDefault
	// filmTerminalGeometryFlag is xterm's geometry argument; the value is the
	// layout's own WxH in CELLS, composed by the caller from the band it drew at.
	filmTerminalGeometryFlag = "-geometry"
	// filmTerminalExecFlag is how xterm is told WHICH command to run inside it.
	filmTerminalExecFlag = "-e"
	// filmTerminalTitleFlag gives the window a title that names this run, so a
	// human looking at the display can tell the window from any other.
	filmTerminalTitleFlag = "-T"
	// filmTerminalLogFlag / filmTerminalLogFileFlag switch ON the terminal's own
	// session log and name its file. THE LOG IS THE COMMAND'S OUTPUT: xterm writes
	// everything the child puts on its pty here, so the producer's own words
	// survive even though the terminal owns that pty and this process cannot read
	// it — without it a producer that dies inside the terminal leaves nothing
	// behind at all (measured 2026-10-03: three terminal attempts, an empty log on
	// both sides, and the reason only recoverable by re-running BY HAND). It is
	// xterm's own mechanism, not a wrapper: an `-e /bin/sh -c 'cmd >> file'` shim
	// was tried first and is WRONG — redirecting the TUI's stdout to a file makes
	// its layout probe answer "not a terminal" and the window paints nothing.
	filmTerminalLogFlag     = "-l"
	filmTerminalLogFileFlag = "-lf"
	// displayEnvName is the environment variable every X client resolves its
	// server from. It is set on the CHILD's environment only — this process's own
	// DISPLAY is never touched: the operator's desktop is not this run's to
	// repoint (the same rule that makes the display private in the first place).
	displayEnvName = "DISPLAY"
	// filmTerminalWindowTimeout bounds the wait for the client's window to appear
	// AND to publish its pid. A terminal that never maps a window is reported by
	// name; it is never waited on forever.
	filmTerminalWindowTimeout = 20 * time.Second
	// filmTerminalPollStep is the granularity at which the window is looked for.
	filmTerminalPollStep = 100 * time.Millisecond
	// filmTerminalMinWindowPixels is the smallest side a filmed window may have.
	// It is a DECISION with a failure mode behind it, not a tuned number:
	// measured 2026-10-03, xfce4-terminal maps a 10x10 placeholder window under
	// the SAME pid as its real window, so a size floor is what keeps a listing
	// that found "a window of this pid" from photographing an empty 10x10
	// rectangle. A real filmed terminal is hundreds of pixels on each side (the
	// 117x26 cell grid measured 1186x521), so the floor separates the two by two
	// orders of magnitude rather than by a tuned margin.
	filmTerminalMinWindowPixels = 200
	// filmTerminalGroupSignalRefusal is what Close says when the terminal's GROUP
	// can no longer be proven to be the one this run created. It is a named
	// string because it is a claim the caller reads (Rule 1b), and its reason is
	// Rule 42: a signal's recipient must be a group this run CREATED and STILL
	// HOLDS, so an unprovable group is refused rather than guessed at.
	filmTerminalGroupSignalRefusal = "refusing to signal the process group of film terminal pid %d: %s — a group signal reaches every member, so it is only delivered to a group this run created (Rule 42)"
)

// The settled-grab bounds. The bound is a CONSTANT, not a tuned number: agreement
// on the second grab is the normal case (132 of 140 beats measured), and a window
// that has not settled after four grabs is REPORTED, not chased.
const (
	filmGrabDefaultTries  = 4
	filmGrabDefaultSettle = 120 * time.Millisecond
	// filmGrabVanishedRetries bounds how many times ONE frame may be retried
	// after the server said the window is not there. It is a bound with a
	// failure mode behind it, not a tuned number: the measured transient
	// (xfce4-terminal rebuilding its toplevel while a film photographs it,
	// 2026-10-04) is one unmap/map cycle, and a window that is STILL not there
	// after three re-resolutions is not a race any more — it is the terminal
	// being gone, and the caller is told so rather than photographed around.
	filmGrabVanishedRetries = 3
	// filmGrabVanishedStep is how long the driver waits before it re-resolves
	// the window and shoots again. It is shorter than filmTerminalPollStep
	// because this is not a search: the terminal is already mapped, its window
	// list is one request away, and the pause is only the bound on how fast the
	// terminal can finish what it is doing.
	filmGrabVanishedStep = 150 * time.Millisecond
	// filmTerminalReResolveTimeout bounds a window look-up that happens while the
	// frame is already in flight (measured 2026-10-04: a terminal whose client had
	// exited, so its window was gone from the server). It is much shorter than
	// filmTerminalWindowTimeout on purpose:
	// the terminal is already mapped and mid-rebuild, so a window of its pid that
	// is not there within this bound means the terminal is gone, and the frame's
	// re-resolution budget must not be spent waiting out a first-resolution wait.
	filmTerminalReResolveTimeout = time.Second

	// filmTerminalResolveNotice prefixes every line this package prints about a
	// terminal it is driving, so the mechanism's own voice is distinguishable
	// from the driver's (`[gopi film] `) and from the terminal's own output in
	// the file beside it.
	filmTerminalResolveNotice = "shot: "
)

// FilmTerminal is the terminal this run started on its own display.
type FilmTerminal struct {
	Display string
	PID     int
	// Window is the X window id of the client, resolved by PID.
	Window uint32
	// Name is the resolved window's title (reported so the identity is auditable).
	Name string
	// Geometry is the window's rectangle in root coordinates, as measured by the
	// server — the area every frame is grabbed from.
	Geometry Rect

	// spec is the registry row this terminal was started from, so the run can
	// REPORT which terminal it filmed and by which selection rule without asking
	// a second time (Rule 15: a run that cannot name its own terminal cannot be
	// checked).
	spec FilmTerminalSpec
	// cmd is the launched process; logPath is where the terminal's own stderr
	// was sent ("" = inherited), so a failure can name the file the reason is in.
	cmd     *exec.Cmd
	logPath string
	// reaped/status record an exit observed while waiting for the window, so the
	// process is reaped exactly once and Close does not wait for it again.
	reaped bool
	status string
	// StartTicks is the terminal's INCARNATION — /proc <pid>/stat field 22, read
	// at the spawn. PID alone is a number the kernel recycles, and this struct
	// outlives the spawn (a caller closes a terminal it started minutes earlier),
	// so Close compares this reading before it signals anything: a recycled pid
	// leads a group that is somebody else's, and a group signal reaches every
	// member of it (Rule 42).
	StartTicks string
	// groupSignalled records that the GROUP signal was actually delivered, as
	// opposed to refused by the identity guard — the two outcomes look the same
	// from outside (both return an error for a refusal, and a reap for a
	// delivery), so the guard's own claim has to be observable.
	groupSignalled bool
}

// Spec is the registry row this terminal was started from — its Name is what a
// report names, and its WindowRule is the rule the window was chosen by.
func (t *FilmTerminal) Spec() FilmTerminalSpec {
	if t == nil {
		return FilmTerminalSpec{}
	}
	return t.spec
}

// Name-geometry helpers: the geometry xterm takes is in CELLS, while the grabbed
// rectangle is in PIXELS, and the two are read from different places on purpose
// (the caller states the cells it drew at; the server states the pixels it
// mapped).

// StartFilmTerminal starts the terminal on the private display and resolves its
// window by PID.
//
// geometryCells is the terminal's own geometry ("117x26"), argv is the command to
// run inside the terminal (empty means a plain shell), title names the window, and
// logPath is the file the terminal's OWN stderr is appended to ("xterm: Can't
// open display"), and sessionLogPath is the file the terminal writes the FILMED
// COMMAND's output to. Both are on disk on purpose: a terminal whose only
// explanation went to a pipe that closed is a failure with no evidence, and a
// producer's output that only ever reached the pty is evidence nobody kept. An
// empty path means "leave that channel alone" (stderr is then inherited).
//
// It is the DEFAULT TERMINAL's entry point (the xterm row of the registry), kept
// for callers that name no terminal: the terminal's identity belongs to
// ResolveFilmTerminal, and a caller that has resolved one hands the SPEC to
// StartFilmTerminalSpec. Everything it does is composed from the registry row, so
// there is no second copy of xterm's flag protocol anywhere.
func StartFilmTerminal(display string, geometryCells string, argv []string, title, logPath, sessionLogPath string) (*FilmTerminal, error) {
	spec, err := FilmTerminalSpecByName(FilmTerminalDefault)
	if err != nil {
		return nil, fmt.Errorf("film terminal: the built-in default %q is not in the registry: %w", FilmTerminalDefault, err)
	}
	return StartFilmTerminalSpec(spec, display, geometryCells, argv, title, logPath, sessionLogPath)
}

// StartFilmTerminalSpec starts a RESOLVED terminal (one registry row) on the
// private display and resolves its window (windowForSpec's rule).
//
// It is the ONE starter: every terminal's launch shape comes from the spec
// (TerminalArgs), so this function never branches on a terminal's name and adding
// a terminal is a registry row rather than a second if — the property that makes
// "which terminal is filmed" answerable from one table.
func StartFilmTerminalSpec(spec FilmTerminalSpec, display string, geometryCells string, argv []string, title, logPath, sessionLogPath string) (*FilmTerminal, error) {
	if !spec.usable() {
		// A KNOWN-BUT-UNUSABLE terminal never reaches exec: its refusal is the
		// answer, and this second gate is what makes that true on every entry
		// point (Rule 15 — the decision is not the caller's to forget).
		return nil, errors.New(spec.refusalText())
	}
	if strings.TrimSpace(display) == "" {
		return nil, fmt.Errorf("film terminal %s: no display was given", spec.Name)
	}
	if strings.TrimSpace(geometryCells) == "" {
		return nil, fmt.Errorf("film terminal %s: no geometry was given (the layout's own WxC, e.g. 117x26)", spec.Name)
	}
	args, err := spec.TerminalArgs(geometryCells, argv, title, sessionLogPath)
	if err != nil {
		return nil, err
	}
	exe, err := exec.LookPath(spec.Binary)
	if err != nil {
		return nil, fmt.Errorf("the terminal %s is NOT INSTALLED: no executable named %s on PATH (%s) — this films a real terminal, so the emulator is required; usable terminals on this driver are %s",
			spec.Name, spec.Binary, os.Getenv("PATH"), FilmTerminalUsableList())
	}
	cmd := exec.Command(exe, args...)
	childEnv, err := FilmTerminalChildEnv(display, os.Environ())
	if err != nil {
		// The filmed terminal's LOCALE could not be pinned to a UTF-8 one. The
		// terminal is not started at all: under a non-UTF-8 locale it decodes the
		// pty stream per BYTE, films the product's Chinese ≈15.41 cell-widths wide
		// instead of ≈11.17 and pushes the neighbouring ASCII out of place — a
		// film of a layout the product never drew (filmlocale.go). A refusal the
		// caller can act on beats a film nobody can trust.
		return nil, fmt.Errorf("film terminal %s: %w", spec.Name, err)
	}
	cmd.Env = childEnv
	cmd.Stdin = nil
	// The terminal's own chatter goes to this run's stderr: a terminal that refuses
	// to start (`xterm: Can't open display`) says WHY there and nowhere else, and
	// swallowing it would leave "the window never appeared" as the only evidence.
	// The terminal's own chatter is APPENDED to a file beside the caller's logs
	// rather than wired to this process's stderr, so the reason a terminal refused
	// to start survives the driver's own exit.
	if logPath != "" {
		lf, lerr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if lerr != nil {
			return nil, fmt.Errorf("open the terminal log %s: %w", logPath, lerr)
		}
		defer lf.Close()
		cmd.Stdout = lf
		cmd.Stderr = lf
	}
	// IT LEADS ITS OWN PROCESS GROUP, AND Close ONLY EVER SIGNALS THAT GROUP.
	// Setpgid makes the terminal a group LEADER (pgid == pid in the kernel's own
	// allocation — the one legitimate source of a group id), which is what makes
	// `syscall.Kill(-t.PID, …)` mean "the group this run created" instead of "a
	// number the kernel may have recycled into somebody else's group". The old
	// shape set only Pdeathsig, so the terminal inherited THIS run's group while
	// Close addressed -t.PID: an ESRCH at best, and at worst a signal into an
	// unrelated group after the pid was reused — a violation of Rule 42, whose
	// criterion is that a signal's recipient must be created and held by the
	// caller. The group is still the right recipient for the reason it always
	// was: a driver killed outright (SIGKILL, a harness timeout) runs no cleanup,
	// so a GROUP is the only handle that reaches both the emulator and the filmed
	// command it started. Pdeathsig stays as the second net for the case where
	// this process dies and its group signal never runs.
	cmd.SysProcAttr = filmTerminalSysProcAttr()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s on %s: %w", spec.Name, display, err)
	}
	t := &FilmTerminal{Display: display, PID: cmd.Process.Pid, StartTicks: displayStartTicks(cmd.Process.Pid), cmd: cmd, logPath: logPath, spec: spec}
	if err := t.windowForSpec(spec); err != nil {
		// A terminal that started but never mapped a window is stopped again: it
		// would otherwise hold the display with nothing to photograph.
		if cerr := t.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "shot: film terminal cleanup: %v\n", cerr)
		}
		return nil, err
	}
	return t, nil
}

// filmTerminalSysProcAttr is the launch shape of the filmed terminal, stated
// once so the deadline the teardown depends on cannot drift from the spawn that
// established it:
//
//   - Setpgid: the terminal LEADS its own process group (the kernel allocates
//     pgid == pid — the one legitimate source of a group id, Rule 42). This is
//     what makes the teardown's `syscall.Kill(-t.PID, SIGTERM)` address a group
//     this run created. Without it the terminal inherits THIS run's group while
//     the negative address names the pid as a group — an ESRCH at best, and at
//     worst, once the kernel recycles the pid, a signal into an unrelated group.
//   - Pdeathsig: the second net. A driver killed outright runs no cleanup, and
//     this makes the KERNEL stop the terminal when this process dies.
//
// The group is the right recipient because a bare pid is not enough: the terminal
// runs a child (the filmed command), and killing only the emulator would leave
// that child holding the display.
func filmTerminalSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}

// resolveWindow waits (bounded) for the default terminal's window and resolves
// it by PID. It is windowForSpec with the registry's default rule, kept because
// the default terminal's window IS the pid's window.
func (t *FilmTerminal) resolveWindow() error {
	spec, err := FilmTerminalSpecByName(FilmTerminalDefault)
	if err != nil {
		return fmt.Errorf("the built-in default %q is not in the registry: %w", FilmTerminalDefault, err)
	}
	return t.windowForSpec(spec)
}

// windowForSpec waits (bounded) for the client's window and resolves it through
// the spec's OWN selection rule.
//
// The wait is not a fixed sleep: the condition is the window LISTING containing a
// window this terminal owns — and WHICH window that is, is the rule's job
// (FilmWindowRulePIDTitleRect), not a guess made here.
func (t *FilmTerminal) windowForSpec(spec FilmTerminalSpec) error {
	return t.windowForSpecWithin(spec, filmTerminalWindowTimeout)
}

// windowForSpecWithin is windowForSpec with the wait given by the caller: the
// FIRST resolution may wait out filmTerminalWindowTimeout (a client that has
// been started but has not mapped yet), while a RE-resolution after the window
// vanished mid-frame is bounded much tighter — the terminal is already mapped
// and mid-rebuild, so "no window of that pid half a second from now" means the
// terminal is gone, and spending the full first-resolution budget three times
// over would turn one missing frame into a minute of silence.
func (t *FilmTerminal) windowForSpecWithin(spec FilmTerminalSpec, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastCount int
	var lastErr error
	for time.Now().Before(deadline) {
		list, err := ListWindows(t.Display)
		if err != nil {
			lastErr = err
			time.Sleep(filmTerminalPollStep) //stylegate:allow-sleep polling the display for the terminal's window
			continue
		}
		lastCount = len(list.Windows)
		// A terminal that already EXITED will never map a window: report it at
		// once, with the reason its own stderr holds, instead of waiting out the
		// whole bound and then describing a timeout (the two have different
		// causes and different fixes).
		if done, status := t.reapIfExited(); done {
			return fmt.Errorf("film terminal %s pid %d %s before mapping a window on %s (its own stderr is appended to %s)",
				spec.Name, t.PID, status, t.Display, t.logPath)
		}
		if w, ok := windowForSpec(list, spec, uint32(t.PID), t.Name); ok {
			win, perr := strconv.ParseUint(strings.TrimPrefix(w.ID, "0x"), 16, 32)
			if perr != nil {
				return fmt.Errorf("film terminal %s pid %d: the server reported window id %q, which is not a hex id: %w", spec.Name, t.PID, w.ID, perr)
			}
			t.Window = uint32(win)
			t.Name = w.Name
			t.Geometry = Rect{X: w.X, Y: w.Y, W: w.W, H: w.H}
			if !w.Measured || t.Geometry.W <= 0 || t.Geometry.H <= 0 {
				return fmt.Errorf("film terminal %s pid %d: window %s was listed but its rectangle could not be measured (%s) — there is no area to photograph",
					spec.Name, t.PID, w.ID, t.Geometry)
			}
			return nil
		}
		time.Sleep(filmTerminalPollStep) //stylegate:allow-sleep polling the display for the terminal's window
	}
	if lastErr != nil {
		return fmt.Errorf("film terminal %s pid %d: the display %s could not be listed within %s: %w", spec.Name, t.PID, t.Display, timeout, lastErr)
	}
	return fmt.Errorf("film terminal %s pid %d: no window publishing %s=%d appeared on %s within %s (%d top-level window(s) were listed) — the terminal started but did not map a window (%s)",
		spec.Name, t.PID, netWMNameName, t.PID, t.Display, timeout, lastCount, spec.WindowRule)
}

// Exited reports whether the terminal has exited, and how — the same non-blocking
// reap resolveWindow uses, exported for the DRIVER's own loop: a producer that
// failed the instant it started must not leave the driver polling its ledger
// (measured 2026-10-03: a nonexistent session id produced exactly that — the
// terminal was gone in a second and the driver waited out its full bound before
// saying anything).
func (t *FilmTerminal) Exited() (bool, string) {
	if t == nil {
		return false, ""
	}
	return t.reapIfExited()
}

// reapIfExited reports whether the terminal has EXITED, and how. It is a real
// non-blocking reap (wait4 WNOHANG), not a signal probe: a signal probe answers
// nil for a ZOMBIE, so a terminal that died a second after it started would keep
// the caller waiting out the whole bound and then be described as "did not map a
// window" — a timeout story for a process that had already given its status.
// MEASURED 2026-10-03: that is exactly what the unreaped zombie produced.
func (t *FilmTerminal) reapIfExited() (bool, string) {
	if t.cmd == nil || t.cmd.Process == nil || t.reaped {
		return t.reaped, t.status
	}
	var ws syscall.WaitStatus
	pid, err := syscall.Wait4(t.PID, &ws, syscall.WNOHANG, nil)
	if err != nil {
		if err == syscall.ECHILD {
			// Already reaped elsewhere (or never ours): the exit is known, the
			// status is not.
			t.reaped, t.status = true, "exited (status already reaped)"
			return true, t.status
		}
		// EINTR and anything else: not an exit we can report.
		return false, ""
	}
	if pid == 0 {
		return false, ""
	}
	t.reaped = true
	switch {
	case ws.Signaled():
		t.status = "was killed by " + ws.Signal().String()
	case ws.Exited():
		t.status = "exited with status " + strconv.Itoa(ws.ExitStatus())
	default:
		t.status = "stopped"
	}
	return true, t.status
}

// filmWindowForPID is the whole window-selection rule for the DEFAULT terminal,
// in one pure function: the window whose _NET_WM_PID is this terminal's pid.
//
// A window that publishes no pid is NOT a candidate — the server agrees with the
// kernel's process table or it is not the answer, and "the first X client on the
// display" is exactly the heuristic that photographs the wrong window on a
// machine with anything else on it.
func filmWindowForPID(list XWindowList, pid uint32) (XWindowInfo, bool) {
	return windowForSpec(list, FilmTerminalSpec{WindowRule: FilmWindowRulePIDTitleRect}, pid, "")
}

// windowForSpec is the WHOLE window-selection rule for a filmed terminal, in one
// pure function, and it is the rule the registry row NAMES.
//
// FilmWindowRulePIDTitleRect has three clauses, each measured rather than
// assumed (2026-10-03, xfce4-terminal on a private display):
//
//	(a) the pid      — the ONE honest witness of "this run started it" is the
//	                   client's own _NET_WM_PID, read against the kernel's process
//	                   table. Never "the first X client", never WM_CLASS.
//	(b) the title    — ONE pid maps THREE top-levels (its 10x10 placeholder,
//	                   a 1x1 auxiliary, and the real window). The title is what
//	                   separates them, and it is known BEFORE the terminal starts
//	                   (the caller composed it), so it is passed in. An EMPTY
//	                   wanted title matches any title, which is the honest
//	                   degradation for a caller that named none.
//	(c) the rectangle — a listed window is not necessarily a drawable one: the
//	                   placeholder is 10x10 and an unmeasured window has no area.
//	                   filmTerminalMinWindowPixels is the floor.
//
// It returns the FIRST window satisfying all three; when the rule is unknown it
// refuses rather than falling back to a weaker one (Rule 15: a selection rule a
// reader cannot name is a selection nobody can check).
func windowForSpec(list XWindowList, spec FilmTerminalSpec, pid uint32, wantedTitle string) (XWindowInfo, bool) {
	if pid == 0 || spec.WindowRule != FilmWindowRulePIDTitleRect {
		return XWindowInfo{}, false
	}
	for _, w := range list.Windows {
		if w.PID != pid {
			continue
		}
		if wantedTitle != "" && w.Name != wantedTitle {
			continue
		}
		if !w.Measured || w.W < filmTerminalMinWindowPixels || w.H < filmTerminalMinWindowPixels {
			continue
		}
		return w, true
	}
	return XWindowInfo{}, false
}

// Close stops the terminal's whole process GROUP and reaps it: the terminal runs
// a child (the filmed command), and killing only the emulator would leave that
// child holding the display. The group is the terminal's OWN (Setpgid at the
// spawn made it a leader), and it is signalled only while that is still
// measurably true — see filmTerminalGroupSignalGuard.
func (t *FilmTerminal) Close() error {
	if t == nil || t.PID <= 0 {
		return nil
	}
	if err := t.signalGroup(syscall.SIGTERM); err != nil {
		return err
	}
	if !t.reaped {
		if done, _ := t.reapIfExited(); !done {
			// Still alive (or becoming so): reap it so the run leaves no zombie.
			if _, err := syscall.Wait4(t.PID, nil, 0, nil); err != nil && err != syscall.ECHILD {
				return fmt.Errorf("reap film terminal pid %d: %w", t.PID, err)
			}
		}
	}
	return nil
}

// signalGroup delivers sig to the terminal's process group, and REFUSES to
// deliver it at all unless the group is measurably still the one this run
// created. A refusal is an ERROR, not a silent skip: the terminal and the
// command it is filming are still holding the display, and the caller has to
// know that (Rule 15 — a failure is stated, never swallowed).
func (t *FilmTerminal) signalGroup(sig syscall.Signal) error {
	if reason, held := t.groupStillOurs(); !held {
		return fmt.Errorf(filmTerminalGroupSignalRefusal, t.PID, reason)
	}
	// The negative address is now a group THIS RUN created and has just measured
	// itself to still hold (Rule 42): Setpgid at the spawn made pgid == t.PID.
	if err := syscall.Kill(-t.PID, sig); err != nil && err != syscall.ESRCH {
		return fmt.Errorf("stop film terminal pid %d: %w", t.PID, err)
	}
	t.groupSignalled = true
	return nil
}

// groupStillOurs reports whether -t.PID still names a group this run created and
// still holds, and if not, WHY — the two readings are measured, never assumed:
//
//  1. the incarnation: the pid must still carry the starttime recorded at the
//     spawn. A recycled number is a DIFFERENT process, and the group it leads is
//     somebody else's (the measured class: cmd/agent/ground_kill_identity_test.go,
//     where a recycled pid put SIGINT into `systemd --user` and stopped a GNOME
//     session). An EMPTY record is not usable identity, so it refuses too rather
//     than falling back to the bare number.
//  2. the leadership: the live group of that pid must equal the pid itself —
//     the Setpgid invariant SetProcAttr established. If it does not hold, -t.PID
//     would address a group this run never created, or no group at all.
//
// One caller closes a terminal it started earlier, so both readings are taken at
// the moment of the signal rather than trusted from the record.
func (t *FilmTerminal) groupStillOurs() (string, bool) {
	if t.StartTicks == "" {
		return "the spawn recorded no incarnation (starttime) for it, so the pid cannot be told from a recycled one", false
	}
	nowTicks, ok := procname.StartTicks(procname.DefaultProcRoot, strconv.Itoa(t.PID))
	if !ok {
		return fmt.Sprintf("no process carries pid %d any more, so -%d names no group of this run's", t.PID, t.PID), false
	}
	if strings.TrimSpace(nowTicks) != strings.TrimSpace(t.StartTicks) {
		return fmt.Sprintf("its incarnation changed (pid %d now started at %s, this run started it at %s), so the pid was recycled and the group under that number belongs to a stranger",
			t.PID, strings.TrimSpace(nowTicks), strings.TrimSpace(t.StartTicks)), false
	}
	pgid, err := syscall.Getpgid(t.PID)
	if err != nil {
		return fmt.Sprintf("its process group could not be read (%v), so this run cannot prove it holds one under pid %d", err, t.PID), false
	}
	if pgid != t.PID {
		return fmt.Sprintf("it is not the leader of its own group (pid %d is in group %d), so -%d would address a group this run never created",
			t.PID, pgid, t.PID), false
	}
	return "", true
}

// FilmGrab is ONE frame and the evidence behind it: how many grabs were taken,
// how many of them photographed a screen that then MOVED (stale), whether two
// consecutive grabs ever agreed, and how many times the window had to be
// re-resolved because the server said it was not there for a moment.
type FilmGrab struct {
	PNG     []byte
	Tries   int
	Stale   int
	Settled bool
	// Vanished counts the times the server refused the window as not-present
	// (BadWindow / BadDrawable) and the grab RECOVERED by re-resolving it. Zero
	// is the normal case; a non-zero value is reported by the caller, so a
	// window that is churning is visible as a number rather than only as a
	// slower run.
	Vanished int
}

// filmWindowCapture is the seam at the PIXEL boundary of a settled grab: the
// function that actually photographs the window. Production never assigns it;
// a test replaces it to drive the criterion itself with no display, no X socket
// and no terminal (PROJECT.md Rule 31: a surface driven only up to its parser has
// not been driven) — the criterion is the deliverable here, and it must be
// provable without a screen.
var filmWindowCapture = CaptureX11Window

// filmTerminalLog is where this package's own notices about a terminal it is
// driving go. It is a variable (like the capture seam) so a test can observe
// the notices instead of a human having to be watching stderr, and so the film
// driver can also keep them in the run's own directory.
var filmTerminalLog io.Writer = os.Stderr

// FilmTerminalNoticeSink points this package's terminal notices at w and
// returns the restore. A caller that wants the notices in its own record (the
// film driver does: a recovery line that only ever reached a terminal is not
// evidence) passes a writer that fans out to both. The restore is returned
// rather than left to the caller so a run cannot leave the sink diverting
// another run's output.
func FilmTerminalNoticeSink(w io.Writer) (restore func()) {
	prev := filmTerminalLog
	if w != nil {
		filmTerminalLog = w
	}
	return func() { filmTerminalLog = prev }
}

// WindowVanishedError is the refusal a frame is abandoned with when the window
// is STILL not there after the re-resolution bound was spent. It names the
// window last resolved, so the reader can tell "the terminal is gone" from
// "the terminal is churning faster than we can photograph it" — and it says
// explicitly that NOTHING was delivered, because the defect this path removes
// was a frame that reached the caller after the window had already gone.
type WindowVanishedError struct {
	Window uint32
	Steps  int
	Cause  error
}

func (e *WindowVanishedError) Error() string {
	return fmt.Sprintf("window 0x%x was not on the display for %d consecutive grab attempt(s) — no frame was delivered (the terminal was re-resolved by %s between attempts): %v",
		e.Window, e.Steps, FilmWindowRulePIDTitleRect, e.Cause)
}

func (e *WindowVanishedError) Unwrap() error { return e.Cause }

// SettledGrab photographs a window until two consecutive grabs AGREE, and
// delivers the agreed one.
//
// It is the Go form of tools/replay/grab.py's judgement, which is the deliverable
// and not an optimisation: a single grab after an acknowledgement is measurably
// one repaint late (8 of 140 beats, measured 2026-10-02). `tries` is a BOUND, not
// a guarantee: a window that never settles (a blinking cursor on a focused
// window, say) is delivered on the last grab with Settled=false, and the caller
// reports it — a still surface and a lost repaint must never become the same
// number (Rule 15).
//
// The comparison is over the PNG BYTES of the same rectangle: the encoder is
// deterministic for identical pixels, so byte equality IS pixel equality here, and
// it costs no decode.
//
// t names the terminal this window belongs to, and it is what makes the two
// mechanisms below possible. The film's default terminal (xfce4-terminal, since
// 01a10370) can lose the window this grab resolved — measured 2026-10-04: the
// client exits, the server destroys its windows, and the next request against
// the old id is answered with the server's own refusal (BadDrawable, code 9).
// That is a TRANSIENT of the same family as "the surface has not settled yet",
// which this function already handles by carrying on; treating it as fatal (the
// pre-fix behaviour) ends the whole film on one beat, with no frames and no
// explanation. So: the window is RE-RESOLVED through the ONE selection rule
// (windowForSpec — pid + title + measured rectangle, never a second matching
// rule), the new id is announced where the caller can see it, and the grab
// continues. When even that fails the bound is spent and the frame is REFUSED
// with *WindowVanishedError — never delivered.
//
// Note what is NOT claimed here: a window that is merely UNMAPPED is not what
// produced the field's code 9 (measured: the server still answers GetImage for
// an unmapped window whose client is alive, and the film kept shooting through a
// 2-second unmap). The refusal this recovers from is the window being GONE.
func SettledGrab(display string, t *FilmTerminal, tries int, settle time.Duration) (FilmGrab, error) {
	if tries <= 0 {
		tries = filmGrabDefaultTries
	}
	if settle <= 0 {
		settle = filmGrabDefaultSettle
	}
	var prev []byte
	g := FilmGrab{}
	if t == nil || t.Window == 0 {
		return FilmGrab{}, fmt.Errorf("settled grab on %s: no window to photograph (the film terminal was not started, or never mapped a window)", display)
	}
	window := t.Window
	vanished := 0
	// ONE frame's grab, with the vanished-window retry wrapped around a SINGLE
	// attempt: a re-resolution restarts the agreement count (the pixels before
	// the rebuild and the pixels after it are not the same screen), so the
	// retry lives outside the settled-grab loop rather than inside it.
	grabOnce := func() ([]byte, error) {
		cur, _, err := filmWindowCapture(display, window)
		if err == nil {
			return cur, nil
		}
		if !IsWindowVanishedError(err) {
			return nil, fmt.Errorf("grab window 0x%x on %s (grab attempt for the frame): %w", window, display, err)
		}
		lastErr := err
		for step := 1; step <= filmGrabVanishedRetries; step++ {
			time.Sleep(filmGrabVanishedStep) //stylegate:allow-sleep the terminal is mid-rebuild; the pause is the bound on how fast it can finish
			// A re-resolution that finds NO window of this terminal is the same
			// statement as the server's refusal, one request later: the rebuild has
			// not finished. It spends a step of the SAME bound rather than ending
			// the frame here, which is the difference between "the terminal takes a
			// moment" and "the terminal is gone" — only running out of steps tells
			// those apart.
			if rerr := t.ReResolveWindow(); rerr != nil {
				lastErr = rerr
				continue
			}
			window = t.Window
			vanished++
			fmt.Fprintf(filmTerminalLog, "%s%s window vanished mid-frame — re-resolved it to 0x%x (%q, area %s); the frame continues (retry %d of %d)\n",
				filmTerminalResolveNotice, err, window, t.Name, t.Geometry, step, filmGrabVanishedRetries)
			cur, _, cerr := filmWindowCapture(display, window)
			if cerr == nil {
				return cur, nil
			}
			// The fresh window is not photographable yet either. It is only worth
			// another re-resolution when the server still says "not there"; any
			// other refusal is a different failure and is reported as itself.
			lastErr = cerr
			if !IsWindowVanishedError(cerr) {
				return nil, fmt.Errorf("grab the re-resolved window 0x%x on %s: %w", window, display, cerr)
			}
		}
		// Both causes travel: the server's own refusal (the reason this path was
		// entered) and whatever the last attempt met, so a reader can tell a window
		// that kept refusing from a terminal that stopped publishing one.
		return nil, &WindowVanishedError{Window: window, Steps: filmGrabVanishedRetries, Cause: errors.Join(err, lastErr)}
	}
	for i := 0; i < tries; i++ {
		if i > 0 {
			time.Sleep(settle) //stylegate:allow-sleep the settle IS the measurement: the screen must be given time to finish its repaint
		}
		cur, err := grabOnce()
		if err != nil {
			return FilmGrab{}, err
		}
		if len(cur) == 0 {
			return FilmGrab{}, fmt.Errorf("grab window 0x%x on %s (grab %d of %d) produced no bytes", window, display, i+1, tries)
		}
		g.Tries = i + 1
		g.Vanished = vanished
		if prev != nil && bytes.Equal(prev, cur) {
			g.PNG = cur
			g.Settled = true
			return g, nil
		}
		if prev != nil {
			g.Stale++
		}
		prev = cur
	}
	g.PNG = prev
	g.Vanished = vanished
	return g, nil
}

// WindowVanishedSteps reports how many re-resolution attempts a frame gets
// before it is refused — the bound, read from its ONE definition (Rule 13/14)
// so a report can state it without re-typing the number.
func WindowVanishedSteps() int { return filmGrabVanishedRetries }

// ReResolveWindow re-applies the terminal's OWN window-selection rule and
// adopts the result, so a caller that lost its window mid-frame can carry on.
//
// It is deliberately thin: windowForSpec owns the rule (pid + title + measured
// rectangle), the bounded wait and every one of its refusal messages, and this
// method adds NO matching of its own (Rule 13/14). It exists as a method so the
// rule is reachable from the grab loop, not duplicated there.
func (t *FilmTerminal) ReResolveWindow() error {
	if t == nil || t.PID <= 0 {
		return fmt.Errorf("re-resolve the film terminal's window: there is no terminal to re-resolve (the terminal was never started)")
	}
	if t.spec.WindowRule == "" {
		return fmt.Errorf("re-resolve the film terminal's window (pid %d): the terminal carries no window-selection rule — it was not started by this package's starter, so there is nothing to re-apply", t.PID)
	}
	return t.windowForSpecWithin(t.spec, filmTerminalReResolveTimeout)
}

// TermSpec is the registry row this terminal was started from, exported for the
// same reason Spec() has an accessor: the driver's own report says which terminal
// it filmed and by which rule, and it must not have to hold a second copy of
// those two facts to say it (Rule 13/14).
func (t *FilmTerminal) TermSpec() FilmTerminalSpec {
	if t == nil {
		return FilmTerminalSpec{}
	}
	return t.spec
}

// FilmTerminalDisplayEnvName is exported so a caller can name the channel it is
// filling in a report without re-typing the variable (Rule 13/14).
func FilmTerminalDisplayEnvName() string { return displayEnvName }

// FilmTerminalSessionLogFlag is the flag pair that turns the terminal's own
// session log on, exported for the same reason: the driver's usage text names it.
func FilmTerminalSessionLogFlag() (string, string) {
	return filmTerminalLogFlag, filmTerminalLogFileFlag
}
