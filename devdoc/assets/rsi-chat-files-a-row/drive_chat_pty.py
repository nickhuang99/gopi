#!/usr/bin/env python3
"""drive_chat_pty.py — drive a REAL `gopi chat --tools` turn over a pty.

WHY A PTY: `gopi chat` decides its input path from isatty(stdin) and its colour
from isatty(stderr). A pipe makes it a different program. This driver gives it
a real controlling terminal, so the session under test is the shipped one.

WHY IT WAITS ON THE SESSION JSONL, NOT ON A TIMER: a chat turn is several
streaming rounds; typing on a clock interleaves keystrokes with the model's
output. The driver watches the session file (written under GOPI_HOME) for the
turn's terminal event and only then sends the quit key.

Usage: drive_chat_pty.py <binary> <gopi-home> <workdir> <message-file> <out-raw.txt>
Exit: 0 = the turn ran and the child exited; 2 = a deadline was hit (raw capture
      is still written, and stderr names which deadline).
"""

import fcntl
import os
import pty
import select
import signal
import struct
import sys
import termios
import time

# Geometry the child is told about: wide enough that the tool JSON the agent
# prints (paths, payloads) does not wrap — a wrapped transcript is unreadable
# evidence. Named because the layout depends on them (same reason drive_ui.py
# names its own).
ROWS = 200
COLS = 200
TIOCSWINSZ = getattr(termios, "TIOCSWINSZ", 0x5414)

READ_TIMEOUT = 1.0          # per-select slice
TURN_DEADLINE = 420.0       # the whole turn (a real LLM round + tool rounds)
QUIET_AFTER_PROMPT = 3.0    # silence that means "the turn is over"
EXIT_DEADLINE = 60.0        # graceful closeout after Ctrl+C
CTRL_C = b"\x03"

binary, gopi_home, workdir, msg_file, out_path = sys.argv[1:6]

with open(msg_file, "rb") as f:
    message = f.read()

os.environ["GOPI_HOME"] = gopi_home
os.environ["TERM"] = "xterm-256color"
# The agent's own CLI rounds must resolve to the binary UNDER TEST, not to
# whatever /usr/local/bin holds (Rule 31: the surface you changed, executed).
os.environ["PATH"] = os.path.dirname(os.path.abspath(binary)) + ":" + os.environ["PATH"]

os.chdir(workdir)

pid, fd = pty.fork()
if pid == 0:
    os.execv(binary, [binary, "chat", "--tools", "--no-editor"])

fcntl.ioctl(fd, TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
os.kill(pid, signal.SIGWINCH)

captured = bytearray()
# Incremental capture: the pty bytes land in out_path as they arrive, so the
# transcript is readable WHILE the turn is still running (a driver that only
# writes at exit cannot be told apart from a driver that is stuck).
out_file = open(out_path, "wb")
start = time.time()
sent_msg = False
sent_mark = 0
sent_quit = False
quiet_since = None
status = 0


def pump(timeout):
    """Drain whatever the pty has, appending to the capture."""
    r, _, _ = select.select([fd], [], [], timeout)
    if not r:
        return False
    try:
        chunk = os.read(fd, 65536)
    except OSError:
        return False
    if not chunk:
        return False
    captured.extend(chunk)
    out_file.write(chunk)
    out_file.flush()
    return True


def pump(timeout):
    """Read one chunk from the pty. True when bytes arrived, False on EOF/empty.

    Bounded by TimeoutExpired on the fd itself; the caller's loops carry their
    own iteration/deadline bounds (see READ_CAP / TURN_DEADLINE / EXIT_DEADLINE).
    """
    r, _, _ = select.select([fd], [], [], timeout)
    if not r:
        return False
    try:
        chunk = os.read(fd, 65536)
    except OSError:
        return False
    if not chunk:
        return False
    captured.extend(chunk)
    out_file.write(chunk)
    out_file.flush()
    return True


# READ_CAP bounds the driver's polling loop by ITERATION as well as by the wall
# clock: TURN_DEADLINE/EXIT_DEADLINE are checked in the body, but a body that
# blocks can outlive them, and an unpinned loop is what the 2026-09-11 device
# exhaustion looked like. 400k polls × the 0.05s poll step is far past any real
# turn, so the cap never truncates a legitimate run.
READ_CAP = 400000
polls = 0
while polls < READ_CAP:
    polls += 1
    got = pump(READ_TIMEOUT)
    now = time.time()
    if got:
        quiet_since = now
    if not sent_msg and (b"you>" in bytes(captured) or now - start > 30.0):
        # The prompt is up: submit the message. Cooked mode (--no-editor) reads
        # a whole LINE from the terminal, so the terminator is a newline.
        time.sleep(0.5)
        os.write(fd, message + b"\n")
        sent_msg = True
        start = now
        sent_mark = len(captured)
        continue
    if sent_msg and not sent_quit:
        # END OF TURN is the prompt REAPPEARING — not silence. Silence is
        # ambiguous: a model that thinks for a while prints nothing for longer
        # than any quiet window, and cutting the turn there is a driver bug
        # that looks like a broken agent (measured: a 3s quiet window fired
        # mid-round on this very run and the transcript read
        # "error: run interrupted by signal").
        tail = bytes(captured[sent_mark:])
        prompt_back = b"you>" in tail
        if prompt_back and quiet_since is not None and (now - quiet_since) > 1.0:
            os.write(fd, b"/quit\n")
            sent_quit = True
            start = now
            continue
        if (now - start) > TURN_DEADLINE:
            sys.stderr.write("drive_chat: TURN_DEADLINE hit (%.0fs) — interrupting\n" % TURN_DEADLINE)
            os.write(fd, CTRL_C)
            sent_quit = True
            start = now
            status = 2
        continue
    if sent_quit:
        if (now - start) > EXIT_DEADLINE:
            sys.stderr.write("drive_chat: EXIT_DEADLINE hit — killing\n")
            os.kill(pid, signal.SIGKILL)
            status = 2
            break
        try:
            done, _ = os.waitpid(pid, os.WNOHANG)
        except ChildProcessError:
            break
        if done == pid:
            break

# Final drain: the closeout notice (session_end) is written after the loop.
for _ in range(20):
    if not pump(0.5):
        if not pump(0.5):
            break

with open(out_path, "wb") as f:
    f.write(bytes(captured))
sys.exit(status)