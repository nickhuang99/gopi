#!/usr/bin/env python3
"""render_transcript.py — replay a raw pty capture into readable screen text.

The pty capture is what the PROGRAM wrote (ANSI escapes included): bubbletea's
diff renderer moves the cursor with `\\r\\x1b[<n>A` and rewrites only the lines
that changed. Grepping those bytes gives you escapes and half-lines, which is
useless as evidence. This script replays them through a minimal screen model
(the handful of sequences this surface emits) and prints the VISIBLE screen.

It also prints, per recorded frame, the lines that CHANGED — the "how much is
being repainted per token" view that the report quotes.

Usage: render_transcript.py <raw> [--frames] [--turn N]
"""

import argparse
import codecs
import sys

# The escape sequences this surface emits. Anything else is dropped (a
# rendering aid must never pretend to be a terminal emulator).
# The geometry the capture was taken at. These MUST equal drive_ui.py's ROWS /
# COLS: the driver tells the child that geometry (TIOCSWINSZ) and the child wraps
# its lines to it. Replaying at any other width puts a line the terminal wrapped
# onto a single row here, so the frames that follow land a row short of where the
# renderer put them — measured: a 400-column replay of the 100-column capture
# interleaved the tool-call turn's rows into character soup, which reads as a
# broken TUI and is an artifact of THIS aid.
DEFAULT_ROWS = 200
DEFAULT_COLS = 100

CSI_FINAL_MIN = 0x40   # ANSI: a CSI ends at the first byte in 0x40..0x7E
CSI_FINAL_MAX = 0x7E


def scan_csi(text, i):
    """Return the index just past the CSI starting at i, or None if incomplete.

    The regex form (`\x1b\[([0-9;?]*)([A-Za-z])`) cannot tell "not a CSI" from
    "a CSI whose final byte has not arrived yet" — and that difference decides
    whether the tail is BUFFERED for the next read or DROPPED (see feed).
    """
    j = i + 2  # past ESC [
    while j < len(text):
        o = ord(text[j])
        if CSI_FINAL_MIN <= o <= CSI_FINAL_MAX:
            return j + 1
        if o == 0x1B:
            return None  # a new ESC began: this one is malformed/truncated
        j += 1
    return None


def scan_osc(text, i):
    """Return the index just past the OSC starting at i, or None if incomplete."""
    j = i + 2  # past ESC ]
    while j < len(text):
        if text[j] == "\x07":
            return j + 1
        if text[j] == "\x1b":
            if j + 1 >= len(text):
                return None
            if text[j + 1] == "\\":
                return j + 2
        j += 1
    return None


class Screen:
    def __init__(self, rows=200, cols=400):
        self.grid = [[" "] * cols for _ in range(rows)]
        self.row = 0
        self.col = 0
        self.rows = rows
        self.cols = cols
        # Read-boundary state: see feed().
        self._dec = codecs.getincrementaldecoder("utf-8")("replace")
        self._tail = ""

    def _put(self, ch):
        # Autowrap, like a real terminal: the banner lines are printed by
        # fmt.Fprintf (not width-aware), so a 128-column line in a 100-column
        # pty WRAPS. Without this the model clamped the column and the tail was
        # overwritten by the last character — an artifact of the aid, not the
        # surface.
        if self.col >= self.cols:
            self.row += 1
            self.col = 0
        if self.row >= self.rows:
            self.grid.append([" "] * self.cols)
            self.rows += 1
        self.grid[self.row][self.col] = ch
        self.col += 1

    def feed(self, data: bytes):
        # A pty read is an arbitrary SLICE of the byte stream: an escape
        # sequence, or even one UTF-8 character, can straddle two reads. Two
        # pieces of state make the model boundary-correct:
        #   _dec  — an incremental UTF-8 decoder (a split character is held,
        #           not turned into U+FFFD),
        #   _tail — the text tail of an escape sequence whose end has not
        #           arrived yet, carried into the next feed.
        # DROPPING that tail is the bug this fixes: the dropped sequence was
        # usually an erase-to-end-of-line, so the PREVIOUS frame's text stayed
        # on the screen and the new line was written over it — measured on a
        # real run, the status line read
        # "idle · turns 2 · tool events 2ents 2".
        text = self._tail + self._dec.decode(data)
        self._tail = ""
        i = 0
        while i < len(text):
            ch = text[i]
            if ch == "\x1b":
                if i + 1 >= len(text):
                    self._tail = text[i:]
                    break
                nxt = text[i + 1]
                if nxt == "]":
                    end = scan_osc(text, i)
                    if end is None:
                        self._tail = text[i:]
                        break
                    i = end
                    continue
                if nxt == "[":
                    end = scan_csi(text, i)
                    if end is None:
                        self._tail = text[i:]
                        break
                    params = text[i + 2:end - 1]
                    self._csi(params, text[end - 1])
                    i = end
                    continue
                i += 2  # a two-character escape (ESC c, ESC 7, ...): no screen effect
                continue
            if ch == "\r":
                self.col = 0
            elif ch == "\n":
                # A pty is in ONLCR mode: the tty driver turns every LF the
                # program writes into CR+LF, which is exactly what bubbletea's
                # renderer relies on when it emits a bare "\n" to skip an
                # UNCHANGED line. Treating LF as row+1 while keeping the column
                # (the obvious first reading, and the bug this model shipped
                # with) writes the next line at the previous line's end column
                # and scrambles the transcript — the "TUI is broken" look of
                # the first capture was THIS aid's error, not the surface's.
                self.row += 1
                self.col = 0
                if self.row >= self.rows:
                    self.grid.append([" "] * self.cols)
                    self.rows += 1
            elif ch == "\b":
                self.col = max(0, self.col - 1)
            elif ch == "\t":
                self.col = (self.col // 8 + 1) * 8
            else:
                self._put(ch)
            i += 1

    def _csi(self, params, final):
        n = 1
        if params.strip("?") != "":
            try:
                n = int(params.strip("?").split(";")[0] or 1)
            except ValueError:
                n = 1
        if final == "A":
            self.row = max(0, self.row - n)
        elif final == "B":
            self.row = min(self.rows - 1, self.row + n)
        elif final == "C":
            self.col = min(self.cols - 1, self.col + n)
        elif final == "D":
            self.col = max(0, self.col - n)
        elif final == "K":
            mode = params.split(";")[0] if ";" in params else params
            if mode in ("2", "?2"):
                self.grid[self.row] = [" "] * self.cols
            elif mode == "0":
                for c in range(self.col, self.cols):
                    self.grid[self.row][c] = " "
        elif final == "J":
            for r in range(self.row + 1, self.rows):
                self.grid[r] = [" "] * self.cols
            for c in range(self.col, self.cols):
                self.grid[self.row][c] = " "
        elif final == "H":
            parts = [p for p in params.split(";") if p]
            self.row = (int(parts[0]) - 1) if parts else 0
            self.col = (int(parts[1]) - 1) if len(parts) > 1 else 0

    def lines(self):
        out = []
        for r in self.grid:
            s = "".join(r).rstrip()
            out.append(s)
        # trim trailing blank rows
        while out and out[-1] == "":
            out.pop()
        return out


def frames(raw: bytes):
    """Split the capture at each cursor-home (`\\r\\x1b[<n>A`) — one per flush."""
    return re.split(rb"(?=\r\x1b\[\d+A)", raw)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("raw")
    ap.add_argument("--frames", action="store_true", help="print per-frame changed lines")
    ap.add_argument("--turn", type=int, default=0, help="with --frames, show only frames after this mark")
    ap.add_argument("--cols", type=int, default=DEFAULT_COLS,
                    help="screen width to replay at — must be the width the child was "
                         "TOLD (drive_ui.py COLS). A wider model does not wrap where the "
                         "real terminal wrapped, so every later line lands on the wrong "
                         "row and the transcript looks interleaved.")
    ap.add_argument("--rows", type=int, default=DEFAULT_ROWS, help="screen height to replay at")
    args = ap.parse_args()

    raw = open(args.raw, "rb").read()
    if args.frames:
        s = Screen()
        prev = []
        for n, f in enumerate(frames(raw)):
            if n == 0:
                s.feed(f)          # the banner, written from a fresh screen
            else:
                # A frame is defined as starting with `\r\x1b[<n>A`: the nth
                # frame rewrites the lines since frame n-1, so it is a PATCH on
                # the accumulated screen, never a standalone document.
                s.feed(f)
            cur = s.lines()
            changed = [l for i, l in enumerate(cur) if i >= len(prev) or prev[i] != l]
            print(f"--- frame {n}: {len(f)} bytes, {len(changed)} changed line(s)")
            for l in changed:
                print("    | " + l)
            prev = cur
        return 0

    s = Screen(rows=args.rows, cols=args.cols)
    s.feed(raw)
    for l in s.lines():
        print(l)
    return 0


if __name__ == "__main__":
    sys.exit(main())
