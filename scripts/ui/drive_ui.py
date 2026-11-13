#!/usr/bin/env python3
"""drive_ui.py — run `gopi ui` under a REAL pty and record the session.

WHY A REAL PTY (not a unit test, not a pipe): the acceptance is a real terminal
session. A pipe is not a terminal — bubbletea would see a non-TTY stdin, and
"works on a pipe" says nothing about raw mode, the alternate screen or the
per-frame diffs. So this driver allocates a pty, drives the binary through it,
and writes the RAW bytes the program emitted (ANSI and all) to a transcript.

WHY IT WAITS FOR A MARKER INSTEAD OF SLEEPING: the first version of this driver
typed on a fixed schedule and was WRONG TWICE, both times looking like a broken
TUI rather than a broken driver:

  1. it sent ``\\n`` for Enter. A terminal in raw mode sends ``\\r`` for Enter
     (bubbletea's keyCR == KeyEnter); ``\\n`` is KeyCtrlJ and submits NOTHING.
     Every prompt sat unsubmitted in the input box and no turn ever started.
  2. it typed the next prompt on a timer, so while a turn was still streaming
     the keystrokes were INTERLEAVED with the transcript repaint — the captured
     screen came back scrambled, and the scramble was the driver's, not the
     TUI's.

So the driver READS the screen it is driving: it replays the pty bytes through
the same minimal screen model the transcript is rendered with and waits for the
status line to say `idle · turns N` (or `streaming…`) before it types. The
session then proceeds at the model's pace, not the clock's.

Usage:
  drive_ui.py <binary> <out-raw.txt> [--tools] [--addr H:P] [--serve URL]

Exit codes: 0 = every step's marker was observed; 2 = a marker timed out (the
raw capture is still written, and the timeout names the step).
"""

import argparse
import errno
import fcntl
import os
import pty
import re
import select
import signal
import struct
import sys
import tempfile
import termios
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "lib"))
from render_transcript import Screen  # noqa: E402  (same directory, a rendering aid)
from tmp_ban import make_scratch_dir  # noqa: E402  (scripts/lib/tmp_ban.py, off `/`, GC'd)

# Terminal geometry the child is told about. Named constants: the layout
# depends on them, so a magic 24 would hide why the transcript wraps.
ROWS = 200
COLS = 100
WINSIZE = struct.pack("HHHH", ROWS, COLS, 0, 0)
TIOCSWINSZ = getattr(termios, "TIOCSWINSZ", 0x5414)

# Control bytes the script types. Named because they are a KEYBOARD, not
# punctuation: \r submits a line (Enter), Ctrl+X interrupts the turn in flight,
# Ctrl+C quits — the same three the verb's usage page documents.
ENTER = b"\r"
CTRL_X = b"\x18"
CTRL_C = b"\x03"

# SNAPSHOT_SETTLE: how long the pty must be QUIET before a step's screen is
# recorded. bubbletea emits one frame as a single Write, but a pty delivers it
# in several reads -- so a step's marker text can be visible while the REST of
# that same frame (the erase-to-end-of-line that clears the previous frame's
# tail) is still in flight. Measured: a snapshot taken the instant the marker
# matched showed the PREVIOUS status line's tail -- the screen read
# "idle . turns 2 . tool events 2ents 2", which reads as a broken TUI and is
# this driver capturing mid-frame. Waiting for quiet records the frame's end.
SNAPSHOT_SETTLE = 0.15

# READ_CAP bounds every pty read loop by ITERATION, not only by EOF/OSError:
# a streaming child that never closes would otherwise spin forever. The cap
# (× 64 KiB per read) is far above any real frame.
READ_CAP = 4096

COUNTER_RE = re.compile(r"#gopi-ui-counters (.*)")
# The status line the driver waits on: "<state> · turns N · tool events M".
STATUS_RE = re.compile(r"(idle|streaming…|stopping…) · turns (\d+) · tool events (\d+)")


class Step:
    """One step: wait for `marker` in the reconstructed screen, then type `send`.

    `marker` is a regex over the screen's last lines. `send` may be empty (a
    pure observation step). `delay` is the pause AFTER the marker matched and
    BEFORE the bytes are sent — used once, to interrupt a turn that has visibly
    started streaming rather than one that just began.
    """

    def __init__(self, label, marker, send=b"", timeout=120.0, delay=0.0):
        self.label = label
        self.marker = re.compile(marker)
        self.send = send
        self.timeout = timeout
        self.delay = delay


# THE SESSION. Each step's marker is the PREVIOUS step's outcome, so the driver
# cannot run ahead of the turn (the fixed-schedule version did, and its
# scramble was the driver's, not the TUI's).
#
# The streaming step's marker is the WORD "streaming…" and NOT a turn count:
# the counter increments when a turn ENDS, so `streaming… · turns 3` describes
# the turn AFTER the third — a marker that couples the two either fires late or
# never (it waited out a whole turn here and timed out).
STEPS = [
    Step("surface ready", r"idle · turns 0",
         b"Reply with exactly: pty-ok.\r"),
    Step("turn 1 done", r"idle · turns 1 · tool events 0",
         b"run the bash tool with exactly: head -3 go.mod\r"),
    Step("turn 2 done: the bash tool ran live", r"idle · turns 2 · tool events [1-9]",
         CTRL_X),                       # an interrupt with nothing running
    Step("idle interrupt is harmless", r"nothing to interrupt",
         b"In one short sentence, what did the bash command you just ran print?\r"),
    Step("turn 3 done: history carried across turns", r"idle · turns 3",
         b"Write the numbers 1 to 40, one per line, nothing else.\r"),
    Step("turn 4 is streaming", r"streaming…", CTRL_X, delay=6.0),
    Step("turn 4 interrupted (not an error)", r"idle · turns 4", CTRL_C),
]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("binary")
    ap.add_argument("out")
    ap.add_argument("--tools", action="store_true")
    ap.add_argument("--addr", default="", help="listen address for the surface's own server")
    ap.add_argument("--serve", default="", help="attach to a running server instead of starting one")
    ap.add_argument("--cwd", default="",
                    help="scratch working directory for the driven turn; the turn has "
                         "tools ENABLED, so anything the model writes lands here, not in "
                         "the worktree under test. Default: a fresh temp dir.")
    ap.add_argument("--selftest", action="store_true",
                    help="ask the surface to report its render counters")
    ap.add_argument("--settle", type=float, default=50.0,
                    help="extra seconds to keep reading after Ctrl+C")
    ap.add_argument("--screens", default="",
                    help="directory to write the VISIBLE screen at each step into")
    args = ap.parse_args()

    pid, fd = pty.fork()
    if pid == 0:  # child
        try:
            fcntl.ioctl(0, TIOCSWINSZ, WINSIZE)
        except OSError:
            pass
        env = dict(os.environ)
        env["TERM"] = "xterm-256color"
        if args.selftest:
            env["GOPI_UI_SELFTEST"] = "1"
        argv = [args.binary, "ui"]
        if args.tools:
            argv.append("--tools")
        if args.addr:
            argv += ["--addr", args.addr]
        if args.serve:
            argv += ["--server", args.serve]
        os.execvpe(argv[0], argv, env)
        os._exit(127)

    os.set_blocking(fd, False)
    # The driven turn has tools ENABLED, so the model can write files. Give it a
    # scratch cwd: whatever it creates belongs there, never in the worktree
    # under test (Rule 30 — a run leaves its worktree clean). The directory
    # comes from scripts/lib/tmp_ban.py, which lands it off `/` (by device id,
    # never by path spelling) and removes it at interpreter exit.
    work = args.cwd or make_scratch_dir("ui-drive")
    os.makedirs(work, exist_ok=True)
    if hasattr(os, "chdir"):
        try:
            os.chdir(work)
        except OSError:
            pass
    raw = bytearray()
    screen = Screen(rows=ROWS + 200, cols=COLS)
    counters = []
    step = 0
    step_deadline = time.time() + STEPS[0].timeout
    status = None
    failure = ""

    def screen_lines():
        """The live screen, top to bottom, blank rows dropped from the ends."""
        lines = list(screen.lines())
        while lines and not lines[-1].strip():
            lines.pop()
        return lines

    def screen_text():
        return "\n".join(l.rstrip() for l in screen_lines()) + "\n"

    def screen_tail(n=8):
        lines = [l for l in screen_lines() if l.strip()]
        return "\n".join(lines[-n:])

    while step < len(STEPS):
        now = time.time()
        if now > step_deadline:
            failure = f"step {step} ({STEPS[step].label}) timed out after {STEPS[step].timeout:.0f}s"
            os.kill(pid, signal.SIGKILL)
            status = 2
            break
        try:
            r, _, _ = select.select([fd], [], [], 0.2)
        except InterruptedError:
            continue
        if fd in r:
            try:
                chunk = os.read(fd, 65536)
            except OSError as e:
                if e.errno == errno.EIO:
                    chunk = b""
                else:
                    raise
            if not chunk:
                failure = f"the child closed the pty during step {step} ({STEPS[step].label})"
                status = 2
                break
            raw.extend(chunk)
            screen.feed(chunk)
            for m in COUNTER_RE.finditer(chunk.decode("utf-8", "replace")):
                counters.append(m.group(1).strip())

        tail = screen_tail()
        if STEPS[step].marker.search(tail):
            if STEPS[step].delay > 0:
                # Let the turn stream visibly before the interrupt: a Ctrl+X
                # fired the instant the status flipped tests the key path, not
                # the interrupt-during-a-stream path.
                time.sleep(STEPS[step].delay)
                try:
                    # Bounded by ITERATION as well as by the pty's own EOF/OSError:
                    # a streaming child that never closes leaves this spinning, and
                    # the 2026-09-11 exhaustion was one unpinned read loop. The cap
                    # is far above any real frame (64 KiB per read).
                    for _ in range(READ_CAP):
                        chunk = os.read(fd, 65536)
                        if not chunk:
                            break
                        raw.extend(chunk)
                        screen.feed(chunk)
                except OSError:
                    pass
                STEPS[step].delay = 0.0
            if args.screens:
                # Drain the rest of the frame before recording (see
                # SNAPSHOT_SETTLE): read until the pty has been quiet that long,
                # so the snapshot is a COMPLETED frame, not a half-delivered one.
                quiet_until = time.time() + SNAPSHOT_SETTLE
                while time.time() < quiet_until:
                    try:
                        r2, _, _ = select.select([fd], [], [], SNAPSHOT_SETTLE)
                    except InterruptedError:
                        continue
                    if fd not in r2:
                        break
                    try:
                        more = os.read(fd, 65536)
                    except OSError:
                        break
                    if not more:
                        break
                    raw.extend(more)
                    screen.feed(more)
                    for m2 in COUNTER_RE.finditer(more.decode("utf-8", "replace")):
                        counters.append(m2.group(1).strip())
                    quiet_until = time.time() + SNAPSHOT_SETTLE
                # Record what was ON SCREEN at the moment the marker matched.
                # This is the transcript that matters: bubbletea ERASES its
                # rendered region when the program exits, so a capture read
                # only at the end legitimately shows the banner and nothing
                # else — the surface's own output is gone by then, by design.
                snap = os.path.join(args.screens, f"step{step + 1:02d}-{STEPS[step].label.replace(' ', '-').replace('/', '-')}.txt")
                with open(snap, "w") as fh:
                    fh.write(screen_text())
                # The raw bytes read SO FAR, next to the live screen state:
                # replaying this prefix offline must reproduce the snapshot, or
                # the difference is the live screen's own state, not the bytes.
                with open(snap + ".raw", "wb") as fh:
                    fh.write(bytes(raw))
            send = STEPS[step].send
            step += 1
            if step < len(STEPS):
                step_deadline = time.time() + STEPS[step].timeout
            if send:
                try:
                    os.write(fd, send)
                except OSError as e:
                    if e.errno not in (errno.EIO, errno.EBADF):
                        raise
                    failure = f"pty write failed at step {step}: {e}"
                    status = 2
                    break
        wpid, wstatus = os.waitpid(pid, os.WNOHANG)
        if wpid == pid:
            status = os.waitstatus_to_exitcode(wstatus)
            break

    # drain whatever is left
    drain_deadline = time.time() + (args.settle if status is None else 3.0)
    while time.time() < drain_deadline:
        try:
            r, _, _ = select.select([fd], [], [], 0.2)
        except InterruptedError:
            continue
        if fd not in r:
            if status is not None:
                break
            continue
        try:
            chunk = os.read(fd, 65536)
        except OSError:
            break
        if not chunk:
            break
        raw.extend(chunk)
        screen.feed(chunk)
        for m in COUNTER_RE.finditer(chunk.decode("utf-8", "replace")):
            counters.append(m.group(1).strip())
        wpid, wstatus = os.waitpid(pid, os.WNOHANG)
        if wpid == pid:
            status = os.waitstatus_to_exitcode(wstatus)
            break

    try:
        os.kill(pid, signal.SIGKILL)
        os.waitpid(pid, 0)
    except (ProcessLookupError, ChildProcessError):
        pass
    try:
        os.close(fd)
    except OSError:
        pass

    with open(args.out, "wb") as f:
        f.write(bytes(raw))
    print(f"raw bytes: {len(raw)} -> {args.out}", file=sys.stderr)
    print(f"steps completed: {step}/{len(STEPS)}", file=sys.stderr)
    for c in counters:
        print("COUNTERS " + c)
    if failure:
        print("FAILURE " + failure, file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
