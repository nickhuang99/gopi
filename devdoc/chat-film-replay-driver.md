<!-- doc-class: normative -->
# `gopi chat --film <outdir>` — the replay driver, in pure Go

**Status**: delivered on `task/01a0ffbba54b754e8f0aab872fb4d0be`, commit `02c1e523`.
**Scope**: the DRIVER side of replay filming only — P1–P5. No GIF composition, no
`tools/replay/verify.py` port, and the old script directory is left in place (a
later sub-item retires it).

## What the old driver was

Two artifacts did the work, and neither is a gopi program:

- `scripts/replay/replay.sh` — pick a free display, start Xvfb on it, start an
  `xterm` inside it, run the producer, walk `beats.tsv` one row at a time, grab a
  frame per beat, ack the beat, write `done.txt`.
- `tools/replay/grab.py` — find the terminal window that belongs to the producer's
  PID, and consider a frame "ready" only when two consecutive grabs agree.

Both are shell/Python, so the driver could only run where those interpreters and
`xdotool`/`xwininfo`/ImageMagick happen to be installed. The port removes that
whole class of requirement: **one gopi binary, plus `Xvfb` and `xterm`**.

## What landed

| file | what it is |
|---|---|
| `internal/shot/displayalloc.go` | display-number allocation (three-step determination), real failure classification, film scratch check |
| `internal/shot/terminalterm.go` | window location by `_NET_WM_PID`, the `SettledGrab` predicate, xterm session log, `Pdeathsig` |
| `cmd/agent/chat_film.go` | `--film` wiring: auto scratch, beat protocol, `beats.tsv`/`ack`/`done.txt`, `frames.tsv`, `producer.log`/`terminal.log` |
| `cmd/agent/cli_chat.go` | `--film` reaches `gopi chat` as a FLAG |

### Design decisions worth keeping

**A flag, not a verb.** The deliverable is `gopi chat --film <outdir>`, an
argument on the surface that already replays — not a `gopi film` subcommand. The
producer is the *same* `gopi chat` binary re-entered with `--replay <session>
--scratch <dir> --cols W --rows H`; the driver is a thin, honest wrapper that
films it. Adding a verb would have created a second answer to "how do I replay".

**The display number is derived, never spelled.** Candidates come from what
`/tmp/.X11-unix` actually holds (the socket names that exist), never from a
literal `:99`. Three steps: the scratch must be writable → `Xvfb`/`xterm` must be
on `PATH` → only then is a number taken. Each failure is classified to the true
cause and **Xvfb's own log is read** rather than guessed at:

- not installed → names the missing executable and the package to install, and
  says plainly that no display number is the reason;
- number taken → says which number, and that it moved on;
- scratch unwritable → names the directory.

**Frames settle, they are not sampled.** `SettledGrab` re-grabs until two
consecutive `GetImage` reads are byte-identical, with a bounded attempt ceiling;
a frame that never settles is counted separately in the final summary rather than
silently shipped. This is `grab.py`'s criterion expressed as a Go predicate.

**Nothing is spawned that outlives the driver.** The xterm carries `Pdeathsig`,
and both children are reaped on every exit path. Measured: after `SIGKILL` of the
driver, neither Xvfb nor xterm remains.

**Artifacts go to the session directory, never the worktree** (Rule 30/37). The
frames, `beats.tsv`, `producer.log` and `terminal.log` all land under the
`--film` outdir the operator names; in this task that is
`~/.gopi/sessions/<id>/film-run-*`.

## Verification (this run's own observations)

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- **Tests**: `make test-budget PKGS='./internal/shot ./cmd/agent'` → see the
  run's report; the new unit tests are
  `TestDisplayCandidatesComeFromTheSocketDirectory`,
  `TestDisplayFailureIsClassifiedFromTheServersOwnLog`,
  `TestCheckFilmScratchRefusesAnUnwritableDirectory`,
  `TestSettledGrabDeliversOnlyAnAgreedFrame`,
  `TestFilmWindowIsSelectedByPID`, `TestChatParseFilmArg`,
  `TestReadFilmBeatsReadsOnlyCompleteRows`.
- **Ran** (the surface itself, with real arguments):

  ```
  bin/gopi chat --replay cb220845-5a2a-46fe-a84e-9674b9359786 --film ~/.gopi/sessions/<id>/film-run-12
  → 150 frames from 150 ledger rows, 150 settled, 0 unsettled; 147 distinct
    (md5) — the replay is genuinely moving, not 150 copies of one frame
  bin/gopi chat --replay 5894f07c-… --film …/film-run-13
  → 127 frames, 118 distinct
  ```

  The **PATH-shadowing** run (`PATH` with an `emptybin/` in front carrying zero
  user-level tools) produced **150 frames with a 0-byte witness file** — proof
  that no `python3`, `xdotool`, `xwininfo`, `xprop` or ImageMagick binary was
  invoked: had one been needed, the shim would have recorded the call, and the
  run would have failed.
- **Three failure paths, each with its real reason**:
  - nonexistent session → the terminal exits with status 0, no completion marker,
    and the message says exactly that and points at `producer.log`/`terminal.log`;
  - unwritable outdir → `--film /proc/gopi-no-such-dir cannot be created: mkdir
    /proc/gopi-no-such-dir: no such file or directory`;
  - no `Xvfb` on `PATH` → names the missing executable, the install command per
    distro, and states that no display number is implicated.
- **Leak check**: `SIGKILL` the driver mid-film → no `Xvfb`, no `xterm` left.

## Not Done (honest)

- **GIF composition** — out of scope for this sub-item; the frames are the input
  for whatever composes them next.
- **`verify.py` was not ported**, and it was not consulted for behaviour beyond
  the frames' own shape.
- **`scripts/replay/` and `tools/replay/grab.py` still exist.** Retiring them is
  a separate sub-item; this branch adds a replacement, it does not delete the
  old path. Anyone reading today should treat the shell driver as still present.
- **`--film` and `--replay` were exercised together only**; a plain
  `gopi chat --film` (no `--replay`) is refused by the existing replay-surface
  argument check, not by new code, and that refusal was not separately filmed.
