#!/usr/bin/env python3
"""termgrid.py — run a TUI on a real PTY and render the FINAL SCREEN as text,
with the per-cell SGR attributes it was painted with.

Why: a screenshot of a terminal is pixels; the vision model reads it back as
prose and garbles the exact columns (measured: `·` → `Å:`, a UUID split across
the wrong line). Defect-hunting needs the grid the terminal itself would show —
character cell (row, col) → rune — so "the status row is overwritten at column
34" is a fact and not an impression.

Why attributes: the first version of this emulator DROPPED every SGR sequence
("SGR changes colour, not cells"). It could therefore prove WHERE text landed
and nothing about how it was painted — a dimmed reasoning line and a coloured
tool row rendered identically to plain text, so no claim about colour, dim or
bold was checkable from a capture. The grid now carries an attribute per cell
(the same model internal/lineedit/attr.go implements, so the Go unit tests and
this harness agree on one shape).

This is a minimal VT100/xterm screen emulator: enough of CUP/HVP/ED/EL/SGR,
CR/LF/BS/TAB and the alternate screen to reconstruct the visible grid of the
gopi chat UI (which draws with CSI positioning — internal/lineedit).

Usage: termgrid.py <bin> <cols> <rows> <script>
  script lines:  sleep <sec> | type <text> | submit | send <text> | key <name>
                 | shot <label> | waitfor <text> | exit | probe | quit
  <text> understands \\n and \\r escapes; `key` knows: enter esc tab up down
  ctrl-d backspace.

Writes, per `shot <label>` in the working directory:
  <label>.grid.txt      the screen, one numbered row per line (text only)
  <label>.grid.attr.txt the screen PLUS one attribute-run line per row
  <label>.raw.txt       the raw byte stream

The attribute dump's shape is the harness contract consumed by tests and by
devdoc/evidence/sgr-grid/capture.sh's grep probes:

  12|the red words here
    ~ 0-3 default | 4-6 fg=1 bold | 7-18 default

Every column of a row appears in exactly one run, so "column 45 is red" is
greppable, and an all-default row reads as one `default` run. Run lengths are
written as `<start>-<end>` inclusive. Colour fields: `fg=<n>` / `bg=<n>` for a
palette index, `fg=rgb:<0xRRGGBB>` for a truecolour value (38;2/48;2). A cell
with no colour and no emphasis prints the single word `default`.
"""
import fcntl
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import time

# `--selftest` needs no app, no geometry and no script at all — it is the
# emulator proving itself — so it is answered before the positional arguments
# are read. Usage: termgrid.py --selftest
SELFTEST = len(sys.argv) > 1 and sys.argv[1] == "--selftest"
BIN = "" if SELFTEST else sys.argv[1]
COLS = 0 if SELFTEST else int(sys.argv[2])
ROWS = 0 if SELFTEST else int(sys.argv[3])
SCRIPT = "" if SELFTEST else sys.argv[4]

# A whole line written in one os.write() is a PASTE BURST to the editor, whose
# Enter-suppression window then eats the submit CR as pasted content — the line
# never runs and the harness silently tests nothing (measured 2026-09-19).
# BURST_GAP is the inter-character delay that keeps a `type` out of that window;
# SUBMIT_GAP waits the window out before the user's own Enter.
BURST_GAP = 0.012
SUBMIT_GAP = 1.0

# WAIT_TIMEOUT bounds a `waitfor` op (in seconds). Generous because the wait is
# on an observed screen fact, not on the clock: it returns the moment the fact
# appears and only burns this budget when the state never arrives — which is
# itself the failure the harness must report loudly (a capture whose caption
# never came true is not evidence).
WAIT_TIMEOUT = 45.0
WAIT_FAILED = False

# The deliberate two-colour probe (`termgrid.py --selftest`): red text, a
# return to the default colour, then bold+dim. It is the sample that PROVES the
# emulator records attributes — an emulator that discards SGR dumps this line as
# a single all-default run, so the self-test's assertion (a red run, then a
# default run, then an emphasised run) is red on the OLD emulator and green on
# this one. It is run without the app, so the instrument is proved even if a
# capture run itself goes wrong.
PROBE_LINE = "\x1b[31mRED-ON-DEFAULT\x1b[0m and \x1b[1;2mBOLD-DIM\x1b[0m"

CSI_RE = re.compile(r"\x1b\[([0-9;?]*)([A-Za-z])")
OSC_RE = re.compile(r"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)")

# --- SGR model (the shape internal/lineedit/attr.go implements) --------------
# A palette index, COLOR_DEFAULT (unset), or COLOR_RGB_NONE with the truecolour
# value carried in the rgb slot.
COLOR_DEFAULT = -1
COLOR_RGB_NONE = -1

SGR_RESET = 0
SGR_BOLD = 1
SGR_DIM = 2
SGR_FG_BASE = 30
SGR_FG_LAST = 37
SGR_EXTENDED = 38
SGR_DEFAULT_FG = 39
SGR_BG_BASE = 40
SGR_BG_LAST = 47
SGR_EXTENDED_BG = 48
SGR_DEFAULT_BG = 49
SGR_FG_BRIGHT = 90
SGR_FG_BRIGHT_LAST = 97
SGR_BG_BRIGHT = 100
SGR_BG_BRIGHT_LAST = 107
SGR_COLOR_MODE_PALETTE = 5
SGR_COLOR_MODE_TRUECOLOR = 2
PALETTE_SIZE = 8


class Attr:
    """One cell's SGR state: fg/bg (palette index or truecolour) + emphasis."""

    __slots__ = ("fg", "bg", "fgrgb", "bgrgb", "bold", "dim")

    def __init__(self, fg=COLOR_DEFAULT, bg=COLOR_DEFAULT,
                 fgrgb=COLOR_RGB_NONE, bgrgb=COLOR_RGB_NONE,
                 bold=False, dim=False):
        self.fg, self.bg = fg, bg
        self.fgrgb, self.bgrgb = fgrgb, bgrgb
        self.bold, self.dim = bold, dim

    def copy(self):
        return Attr(self.fg, self.bg, self.fgrgb, self.bgrgb, self.bold, self.dim)

    def key(self):
        return (self.fg, self.bg, self.fgrgb, self.bgrgb, self.bold, self.dim)

    def is_default(self):
        return self.key() == DEFAULT_KEY

    def sgr(self, params):
        """Apply one CSI m parameter list to this pen (in place)."""
        i = 0
        n = len(params)
        while i < n:
            p = params[i]
            if p == SGR_RESET:
                self.fg = self.bg = COLOR_DEFAULT
                self.fgrgb = self.bgrgb = COLOR_RGB_NONE
                self.bold = self.dim = False
            elif p == SGR_BOLD:
                self.bold = True
            elif p == SGR_DIM:
                self.dim = True
            elif SGR_FG_BASE <= p <= SGR_FG_LAST:
                self.fg, self.fgrgb = p - SGR_FG_BASE, COLOR_RGB_NONE
            elif SGR_FG_BRIGHT <= p <= SGR_FG_BRIGHT_LAST:
                self.fg = p - SGR_FG_BRIGHT + PALETTE_SIZE
                self.fgrgb = COLOR_RGB_NONE
            elif p == SGR_DEFAULT_FG:
                self.fg, self.fgrgb = COLOR_DEFAULT, COLOR_RGB_NONE
            elif SGR_BG_BASE <= p <= SGR_BG_LAST:
                self.bg, self.bgrgb = p - SGR_BG_BASE, COLOR_RGB_NONE
            elif SGR_BG_BRIGHT <= p <= SGR_BG_BRIGHT_LAST:
                self.bg = p - SGR_BG_BRIGHT + PALETTE_SIZE
                self.bgrgb = COLOR_RGB_NONE
            elif p == SGR_DEFAULT_BG:
                self.bg, self.bgrgb = COLOR_DEFAULT, COLOR_RGB_NONE
            elif p in (SGR_EXTENDED, SGR_EXTENDED_BG):
                # 38;5;n / 38;2;r;g;b — the colour consumes the following
                # parameters. A truncated payload changes NOTHING: reading it
                # as further SGR parameters would invent attributes (38;2;1;2
                # would become "dim, bold") that a terminal never applied.
                background = p == SGR_EXTENDED_BG
                if i + 1 >= n:
                    break
                mode = params[i + 1]
                if mode == SGR_COLOR_MODE_PALETTE:
                    if i + 2 >= n:
                        break
                    if background:
                        self.bg, self.bgrgb = params[i + 2], COLOR_RGB_NONE
                    else:
                        self.fg, self.fgrgb = params[i + 2], COLOR_RGB_NONE
                    i += 3
                    continue
                if mode == SGR_COLOR_MODE_TRUECOLOR:
                    if i + 4 >= n:
                        break
                    rgb = (params[i + 2] << 16) | (params[i + 3] << 8) | params[i + 4]
                    if background:
                        self.bg, self.bgrgb = COLOR_DEFAULT, rgb
                    else:
                        self.fg, self.fgrgb = COLOR_DEFAULT, rgb
                    i += 5
                    continue
            i += 1


DEFAULT_KEY = (COLOR_DEFAULT, COLOR_DEFAULT, COLOR_RGB_NONE, COLOR_RGB_NONE, False, False)


def default_attr():
    return Attr()


def format_attr(a):
    """Render one Attr as the dump's field list; untouched → `default`."""
    if a.is_default():
        return "default"
    fields = []
    if a.fgrgb != COLOR_RGB_NONE:
        fields.append("fg=rgb:%d" % a.fgrgb)
    elif a.fg != COLOR_DEFAULT:
        fields.append("fg=%d" % a.fg)
    if a.bgrgb != COLOR_RGB_NONE:
        fields.append("bg=rgb:%d" % a.bgrgb)
    elif a.bg != COLOR_DEFAULT:
        fields.append("bg=%d" % a.bg)
    if a.bold:
        fields.append("bold")
    if a.dim:
        fields.append("dim")
    return " ".join(fields) if fields else "default"


class Screen:
    def __init__(self, cols, rows):
        self.cols, self.rows = cols, rows
        self.reset()

    def reset(self):
        self.grid = [[" "] * self.cols for _ in range(self.rows)]
        self.attrs = [[default_attr() for _ in range(self.cols)] for _ in range(self.rows)]
        self.x = self.y = 0
        self.pen = default_attr()
        # DECSTBM scroll region (CSI top;bottom r). The chat editor sets
        # [1;N r for its scrollback band and relies on LF inside it scrolling
        # ONLY that band — an emulator that scrolls the whole screen renders
        # interleaved garbage that is the EMULATOR's artifact, not the UI's.
        self.top, self.bottom = 0, self.rows - 1

    def put(self, ch):
        if ch == "\r":
            self.x = 0
            return
        if ch == "\n":
            if self.y == self.bottom:
                self.scroll()
            else:
                self.y = min(self.rows - 1, self.y + 1)
            return
        if ch == "\b":
            self.x = max(0, self.x - 1)
            return
        if ch == "\t":
            self.x = min(self.cols - 1, (self.x // 8 + 1) * 8)
            return
        if ord(ch) < 0x20:
            return
        if self.x >= self.cols:
            self.x = 0
            self.y += 1
            if self.y >= self.rows:
                self.scroll()
        self.grid[self.y][self.x] = ch
        # The attribute travels WITH the cell — a colour recorded one cell to
        # the left of the rune it belongs to is worse than no colour at all.
        self.attrs[self.y][self.x] = self.pen.copy()
        self.x += 1

    def scroll(self):
        self.grid.pop(self.top)
        self.grid.insert(self.bottom, [" "] * self.cols)
        self.attrs.pop(self.top)
        self.attrs.insert(self.bottom, [default_attr() for _ in range(self.cols)])
        self.y = self.bottom

    def erase_display(self, mode):
        if mode == 2:
            self.reset()
        elif mode == 0:
            for c in range(self.x, self.cols):
                self.grid[self.y][c] = " "
                self.attrs[self.y][c] = default_attr()
            for r in range(self.y + 1, self.rows):
                self.grid[r] = [" "] * self.cols
                self.attrs[r] = [default_attr() for _ in range(self.cols)]
        elif mode == 1:
            for c in range(0, min(self.x + 1, self.cols)):
                self.grid[self.y][c] = " "
                self.attrs[self.y][c] = default_attr()
            for r in range(0, self.y):
                self.grid[r] = [" "] * self.cols
                self.attrs[r] = [default_attr() for _ in range(self.cols)]

    def erase_line(self, mode):
        if mode == 2:
            self.grid[self.y] = [" "] * self.cols
            self.attrs[self.y] = [default_attr() for _ in range(self.cols)]
        elif mode == 0:
            for c in range(self.x, self.cols):
                self.grid[self.y][c] = " "
                self.attrs[self.y][c] = default_attr()
        elif mode == 1:
            for c in range(0, min(self.x + 1, self.cols)):
                self.grid[self.y][c] = " "
                self.attrs[self.y][c] = default_attr()

    def feed(self, data):
        i = 0
        n = len(data)
        while i < n:
            ch = data[i]
            if ch == "\x1b":
                m = CSI_RE.match(data, i)
                if m:
                    self.csi(m.group(1), m.group(2))
                    i = m.end()
                    continue
                m = OSC_RE.match(data, i)
                if m:
                    i = m.end()
                    continue
                # unknown/simple escape: skip it and its introducer
                i += 2
                continue
            self.put(ch)
            i += 1

    def csi(self, params, final):
        vals = [int(p) if p.isdigit() else 0 for p in params.split(";")] if params.strip("?") else []

        def p(idx, default):
            return vals[idx] if idx < len(vals) and vals[idx] else default

        if final == "H" or final == "f":
            self.y = min(self.rows - 1, p(0, 1) - 1)
            self.x = min(self.cols - 1, p(1, 1) - 1)
        elif final == "A":
            self.y = max(0, self.y - p(0, 1))
        elif final == "B":
            self.y = min(self.rows - 1, self.y + p(0, 1))
        elif final == "C":
            self.x = min(self.cols - 1, self.x + p(0, 1))
        elif final == "D":
            self.x = max(0, self.x - p(0, 1))
        elif final == "G":
            self.x = min(self.cols - 1, p(0, 1) - 1)
        elif final == "d":
            self.y = min(self.rows - 1, p(0, 1) - 1)
        elif final == "r":
            self.top = p(0, 1) - 1
            self.bottom = min(self.rows - 1, p(1, self.rows) - 1)
            self.x = self.y = self.top
        elif final == "J":
            self.erase_display(p(0, 0))
        elif final == "K":
            self.erase_line(p(0, 0))
        elif final == "m":
            # A bare `CSI m` is parameter 0 (a reset), exactly as a terminal
            # treats it — which is why the empty list becomes [0] here.
            self.pen.sgr(vals if vals else [SGR_RESET])
        elif final == "h" and params.startswith("?"):
            if params in ("?1049", "?47", "?1047"):
                self.reset()
        # mode / other: no grid effect

    def runs(self, y):
        """Maximal spans of one attribute in row y: (start, end, Attr)."""
        out = []
        start = 0
        row = self.attrs[y]
        for col in range(1, self.cols + 1):
            if col < self.cols and row[col].key() == row[start].key():
                continue
            out.append((start, col - 1, row[start]))
            start = col
        return out

    def render(self):
        return "\n".join("%2d|%s" % (y, "".join(row).rstrip())
                         for y, row in enumerate(self.grid))

    def render_attrs(self):
        out = []
        for y, row in enumerate(self.grid):
            out.append("%2d|%s" % (y, "".join(row).rstrip()))
            out.append("  ~ " + " | ".join("%d-%d %s" % (s, e, format_attr(a))
                                           for s, e, a in self.runs(y)))
        return "\n".join(out)


def selftest():
    """Feed the two-colour probe into a fresh grid and assert what it recorded.

    This is the emulator's own proof, independent of any capture: it fails
    (exit 1) if the grid drops SGR, which is exactly the old behaviour
    ("SGR changes colour, not cells"). Run by capture.sh before every capture,
    so a capture can never be produced by an attribute-blind instrument.
    """
    screen = Screen(len(PROBE_LINE) + 8, 2)
    screen.feed(PROBE_LINE)
    runs = [(s, e, format_attr(a)) for s, e, a in screen.runs(0)]
    problems = []
    red = next((r for r in runs if r[2].startswith("fg=1")), None)
    after = next((r for r in runs if r[0] > 0 and r[2] == "default"), None)
    emph = next((r for r in runs if "bold" in r[2] and "dim" in r[2]), None)
    if red is None:
        problems.append("no fg=1 (red) run in the probe row: SGR is being dropped")
    if after is None:
        problems.append("no default run AFTER the red one: the reset was dropped")
    if emph is None:
        problems.append("no bold+dim run: emphasis is being dropped")
    if red is not None and after is not None and red[0] == after[0]:
        problems.append("red and default collapse to the same span")
    print("probe row: %s" % "".join(screen.grid[0]).rstrip())
    for r in runs:
        print("  %d-%d %s" % r)
    if problems:
        for p in problems:
            sys.stderr.write("selftest FAIL: %s\n" % p)
        sys.exit(1)
    print("selftest OK: the grid records per-cell attributes")


def wait_for(screen, drain, needle, timeout=WAIT_TIMEOUT):
    """Block until `needle` appears on the grid (or the timeout expires).

    Returns True when it was OBSERVED on screen. The grid is re-rendered each
    poll, so this watches the same truth the dump records — not the byte stream
    (bytes may carry a row the screen never kept) and not the clock.
    """
    end = time.time() + timeout
    while time.time() < end:
        drain(0.25)
        if needle in screen.render():
            return True
    return False


def main():
    global WAIT_FAILED
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    proc = subprocess.Popen([BIN, "chat", "--tools"], stdin=slave, stdout=slave,
                            stderr=slave, preexec_fn=os.setsid, env=dict(os.environ),
                            close_fds=True)
    os.close(slave)

    screen = Screen(COLS, ROWS)
    raw = bytearray()
    started = time.time()

    # DRAIN_CAP bounds drain() by iteration, not only by its deadline.
    DRAIN_CAP = 5000

    def elapsed():
        # Every op reports WHEN it happened. Without a timestamp a capture's own
        # log cannot be read against the run's other clocks, and each shot's
        # cost is invisible — measured 2026-09-19, that invisibility is what let
        # a "toolrunning" shot land after the turn it was meant to show.
        return "t=%.1fs" % (time.time() - started)

    def drain(timeout):
        # Bounded by ITERATION as well as by the wall clock: the deadline check
        # sits in the body, but a body that blocks on select/read can outlive
        # it, and an unpinned loop is what the 2026-09-11 exhaustion looked
        # like. DRAIN_CAP x the 0.2s select slice is far past any real frame.
        end = time.time() + timeout
        for _ in range(DRAIN_CAP):
            left = end - time.time()
            if left <= 0:
                return
            r, _, _ = select.select([master], [], [], min(left, 0.2))
            if not r:
                continue
            try:
                chunk = os.read(master, 65536)
            except OSError:
                return
            if not chunk:
                return
            raw.extend(chunk)
            screen.feed(chunk.decode("utf-8", "replace"))

    def send(s):
        os.write(master, s.encode())

    keys = {"enter": "\r", "cr": "\r", "esc": "\x1b", "tab": "\t", "up": "\x1b[A",
            "down": "\x1b[B", "backspace": "\x7f", "ctrl-d": "\x04", "left": "\x1b[D",
            "right": "\x1b[C"}

    with open(SCRIPT) as f:
        for line in f:
            line = line.rstrip("\n")
            if not line or line.startswith("#"):
                continue
            op, _, arg = line.partition(" ")
            if op == "sleep":
                drain(float(arg))
            elif op == "type":
                # Type CHARACTER BY CHARACTER. A single write of a whole line is
                # a paste BURST to the editor, whose Enter-suppression window
                # then eats the submit CR as pasted content — the line never
                # runs and the harness silently tests nothing. This is the
                # harness bug that made the first slash-command probe read as
                # "the output never printed" (measured 2026-09-19).
                for ch in arg.replace("\\n", "\n"):
                    os.write(master, ch.encode())
                    drain(BURST_GAP)
            elif op == "submit":
                # The user's own Enter: wait out the editor's paste-burst window
                # so the CR is unambiguously a submit, never content.
                drain(SUBMIT_GAP)
                os.write(master, b"\r")
            elif op == "send":
                send(arg.replace("\\n", "\n").replace("\\r", "\r"))
            elif op == "key":
                send(keys[arg.strip()])
            elif op == "shot":
                drain(0.4)
                with open(arg + ".grid.txt", "w") as g:
                    g.write(screen.render() + "\n")
                with open(arg + ".grid.attr.txt", "w") as g:
                    g.write(screen.render_attrs() + "\n")
                with open(arg + ".raw.txt", "wb") as rb:
                    rb.write(bytes(raw))
                print("shot %s (%s)" % (arg, elapsed()))
            elif op == "waitfor":
                # Anchor a shot on an OBSERVED screen fact, never on a guessed
                # sleep. A `sleep` anchors on the clock while the shot's caption
                # claims a state; measured 2026-09-19, exactly that made shot 1
                # and shot 2 BYTE-IDENTICAL (same pixels, two captions) in the
                # first committed baseline. `waitfor` blocks until the text is
                # ON the screen — the observation the caption makes.
                if not wait_for(screen, drain, arg):
                    global WAIT_FAILED
                    WAIT_FAILED = True
                    print("waitfor TIMEOUT: %r never appeared on screen" % arg,
                          file=sys.stderr)
                else:
                    print("waitfor %r OK (%s)" % (arg, elapsed()))
            elif op == "exit":
                # The ONE observation that backs "the session ended": the app
                # process is gone. A sleep would assert the same caption without
                # checking anything (the v1 baseline's `-5-quit` shot was taken
                # 0.0 s after submit and showed the help panel, still alive).
                try:
                    proc.wait(timeout=WAIT_TIMEOUT)
                    # Drain whatever the app wrote on its way out: `wait` returns
                    # when the process is gone, and bytes still in the pty buffer
                    # are part of the final screen the next `shot` records.
                    drain(0.5)
                    print("exit OK (%s)" % elapsed())
                except subprocess.TimeoutExpired:
                    WAIT_FAILED = True
                    print("exit TIMEOUT: the app never exited", file=sys.stderr)
            elif op == "quit":
                break

    try:
        os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
    except ProcessLookupError:
        pass
    try:
        proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
    os.close(master)
    # A `waitfor` that timed out means a shot's caption never came true: exit
    # non-zero so the capture reports it instead of committing an unproven claim.
    return 1 if WAIT_FAILED else 0


if SELFTEST:
    selftest()
else:
    sys.exit(main())
