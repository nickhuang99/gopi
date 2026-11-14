<!-- doc-class: historical -->
# Task Report: PTY agent channel — feasibility study

Task `01a0d5d1-4bd2-7767-a345-c4188dfa6286` — evaluate a PTY (pseudo-terminal) based
channel between two agent instances: one gopi's keystrokes as another's input, one
gopi's screen as another's output. Verdict up front: **the mechanism is real,
already in this repo, and the loop was executed end to end on this host** — but the
*purpose* is the weak half, and the honest answer is that a PTY pays only when the
peer's surface is interactive and terminal-shaped. Details, evidence and the
use-case analysis below; §7 says explicitly what was not done.

## 1. The question, as the attachment frames it

`pipe(2)` gives a byte stream with no terminal semantics: the child's `isatty()`
answers false, so Bubble Tea (gopi's `ui`/`chat` surface) turns off ANSI, cursor
control and raw-mode input and degrades to plain text. A PTY gives the child a
**slave fd that is a real tty**, so the full TUI runs — and the controller holds the
**master fd**, writing into it (keystrokes) and reading from it (the rendered
screen). The attachment's closing question is the right one to split: *which
use-case needs that*, and *what is the cheapest mechanism for each*.

## 2. The mechanism, settled against this repo's own code

| Fact | Where it lives in this tree | Consequence |
|---|---|---|
| The terminal decision is an **ioctl**, not a mode guess | `internal/lineedit/raw.go:95` `func IsTerminal` — "It asks the kernel (the termios ioctl succeeds) instead of trusting ModeCharDevice" | `/dev/null` is a character device with no termios: the crude test answers "yes" and the real one answers "no" (measured in §5) |
| The editor is entered **only** on a terminal | `cmd/agent/lineedit_wire.go:309` `if !noEditor && lineedit.IsTerminal(in) {` | a pipe silently takes the cooked path: text in, text out, no TUI. The degradation is a code path, not a failure |
| gopi's chat states the same contract to users | `cmd/agent/cli_chat.go:35` ("Automatic whenever stdin is not a terminal — a pipe, a redirect, a test harness") | the pipe choice is a PRODUCT decision already, and `--no-editor` makes it explicit |
| The Bubble Tea surface takes its input/output as `io.Reader`/`io.Writer` | `internal/uiclient/run.go:33` (`Screen *ScreenWriter`), run from `cmd/agent/cli_ui.go:394` | a pty is a drop-in for both directions at the Go level |
| A real VT interpreter already lives here | `internal/lineedit/screen.go:93` `NewScreen`, `:127` `Feed`, `:492` `Line` | the ANSI stream can be turned back into **screen rows** with zero new dependencies (no `vt100`/`tcell`) |
| The pty ioctls already live here | `internal/lineedit/pty_linux.go:20` `PtyNumber` (TIOCGPTN), `:25` `PtyUnlock` (TIOCSPTLCK) | `/dev/ptmx` needs no `creack/pty`; `golang.org/x/sys/unix` already provides `TIOCSWINSZ` |
| The pattern is already exercised twice | `internal/shot/textcapture.go:141` `CaptureTextSurface` (production: `gopi shot --surface chat`) and `testsuite/chat_pty_test.go:72` `openPty` (tests) | this is **not a new capability to build** — it is an existing one to be pointed at a new use |

So the attachment's correction is right and worth restating in this repo's terms:
the architecture is not "A's stdout is B's stdin". It is **one controller process
holding a pty master**, with the child's stdin/stdout/stderr bound to the slave.
The controller's *read* side is not business text: it is a terminal byte stream
that must be replayed through a grid before it means anything.

## 3. Zero-dep dependency preflight (Rule 10.7)

Nothing new is needed: `/dev/ptmx` + `TIOCGPTN`/`TIOCSPTLCK`/`TIOCSWINSZ`
(`internal/lineedit`, `x/sys/unix`), `os/exec` with `SysProcAttr{Setsid, Setctty}`,
and `internal/lineedit.Screen` as the interpreter. **`creack/pty` is not needed and
was not added.** A second VT parser was not written either.

## 4. What was executed (the loop, verbatim)

A standalone driver (study code, living OUTSIDE the worktree at
`~/.gopi/sessions/<this task>/ptyeval/driver/main.go`) does exactly what the
channel requires:

1. opens `/dev/ptmx`, resolves + unlocks the slave (repo helpers), sets the
   geometry with `unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, …)`;
2. starts `gopi chat` with the slave as stdin **and** stdout **and** stderr,
   `SysProcAttr{Setsid: true, Setctty: true}`;
3. feeds every master byte into `lineedit.NewScreen(cols, rows)` and reads rows
   with `Screen.Line(r)` — the controller's only view of the child;
4. injects a line by writing to the master one character at a time
   (`lineedit.PasteBurstCharInterval * 4` apart) followed by `"\r"` after
   `lineedit.PasteEnterSuppressWindow * 2` — below those gaps the editor reads the
   whole thing as a **paste**, and in a paste the Enter is content, never a submit.

The child ran against a private profile (`gopi sandbox create`, Rule 34) so the
operator's `~/.gopi` ledger was never touched.

```
$ driver -bin …/gopi -args "chat" \
    -line "what is 17*23? answer with the number only" -expect 391 -cols 200 -rows 50
[driver] argv=[…/gopi chat] pid=962796 slave=/dev/pts/9 cols=200 rows=50 master-char-device=true
[driver] prompt "you>" visible after 5.132953408s; 579 bytes read, 39 escape bytes
[driver] ---- screen-before-typing (200x50) ----
[screen:48] you>
[screen:49] deepseek-flash   ·  iter 0  ·        ·                       ·                         ready
[driver] ---- end screen-before-typing ----
[driver] typed "what is 17*23? answer with the number only" + CR
[driver] EXPECTED-MARKER "391" on the rendered screen (NOT on the editor row) after 7.969722279s
[driver] ---- screen-at-marker (200x50) ----
[screen:43] ──── turn 1 · what is 17*23? answer with the number only ────…
[screen:44] ▌ 391
[screen:45] ───────────────────────────────────────────────────────────…
[screen:48] you>
[screen:49] deepseek-flash   ·  iter 1  ·        ·                       ·   thinking…
[driver] ---- end screen-at-marker ----
[driver] exit=false bytes=5354 escape-bytes=441 scrollback-rows=5
```

`391` is a value the controller never typed — it exists only downstream of the
child's model call. **That is the round trip: injected keys → the child's TUI →
the child's answer → read back off the interpreter's grid.**

### The two-turn derived loop (`-turn2`) — "one's input is another's output"

The single round trip proves the transport. The two-turn run proves the *loop*:
the controller asks for a token whose **shape** it can recognize but whose **value**
it cannot know ("a six-digit number whose six digits are all identical"), reads it
off the grid, and builds the next turn's input out of it — then derives turn 2's
answer the same way.

```
$ driver -bin …/gopi -args chat \
    -line "Reply with a six-digit number whose six digits are all identical, digits only, nothing else." \
    -turn2 "Change exactly one digit of %d and reply with the resulting number only."
[driver] prompt "you>" visible after 5.132874319s
[driver] typed "Reply with a six-digit number whose six digits are all identical, …" + CR
[driver] fresh redraw settled after 8.621573008s (no marker given)
[driver] DERIVED token=444444 (9.13090178s) from the child's turn-1 answer -> turn-2 line "Change exactly one digit of 444444 and reply with the resulting number only."
[screen:44] ▌ 444444
[driver] typed "Change exactly one digit of 444444 and reply with the resulting number only." + CR
[driver] TURN2 DERIVED token=444445 (child applied the instruction to 444444) — the controller never held this value
[screen:43] ──── turn 2 · Change exactly one digit of 444444 and reply with the resulting number only. ────…
[screen:44] ▌ 444445
[driver] exit=false bytes=21880 escape-bytes=1625 scrollback-rows=12
```

Neither operand (`444444`) nor the turn-2 answer (`444445`) was in the controller's
input: turn 2 required reading turn 1 off the screen, and its verification required
reading turn 2. The earlier attempt at this loop (asking for an arithmetic product)
is instructive in the other direction — the model got the arithmetic *wrong*, and
the driver correctly refused to report a round trip; the shape-based task removes
the model's arithmetic from the proof without removing the child from it.

### The pipe contrast, measured on the same binary

```
$ printf 'reply with exactly: PTYOK\n' | gopi chat
you> PTYOK…            (exit 0)

bytes=464  ESC=0  CSI=0     ← the pipe run
bytes=5227 ESC=429 CSI=427   ← the pty run, same chat, same prompt
```

Zero escape bytes over a pipe (cooked reader, no grid, no cursor control) versus
429 in the pty stream. And the isatty criterion, measured through the repo's own
predicate plus the crude one, on three descriptors:

```
[probe] /dev/null            lineedit.IsTerminal=false ModeCharDevice=true  termios=false
[probe] pipe(2) read end     lineedit.IsTerminal=false ModeCharDevice=false termios=false
[probe] /dev/ptmx master     lineedit.IsTerminal=true  ModeCharDevice=true  termios=true
```

Note the first line: the crude `ModeCharDevice` test that `cmd/agent/main.go:1021`
uses **lies about `/dev/null`** — which is exactly why the editor path consults the
ioctl-based predicate instead. The attachment's `isatty()` argument holds, and this
repo already implements it the right way.

## 5. The trap that matters most for anyone who repeats this (a FALSE round trip)

Three of the first four runs "proved" a round trip that had not happened. The
controller's own keystrokes are **echoed by the child's editor**, and the chat
surface re-prints the request in its turn separator, so a `Contents()`-contains
check on the marker fires before the child has answered anything:

* expecting `PTYOK` while typing "…: PTYOK" → matched the echoed editor row;
* expecting `1703638882` after asking `Multiply 31742 by 53671` → never matched, but
  the derived-turn run had already read the **echoed** request's digits;
* asking for a "six identical digits, for example 333333" answer and deriving the
  first repetition token on screen → picked up the **echoed example** (`333333`),
  not the child's answer (`999999`).

Two rules fall out, and both are now baked into the driver:

1. **The expectation must be a value the controller never typed** (it computed the
   answer of an arithmetic question; it asked for a token whose *shape* it could
   recognize but whose *value* it could not know).
2. **The match must exclude the controller's own rows** — the editor row (the one
   carrying the prompt) and any row containing a stretch of text the controller
   typed. Same for anything *derived* from the screen.

Corollary on timing: dump the screen **at the moment the marker matches**. A
settle-then-dump shows a scrolled screen where the answer has already moved into
scrollback — a capture that looks like a failure of the channel when it is only a
capture taken too late.

## 6. Use cases — the part the operator is still deciding

Six candidates, each judged against what gopi already has. "PTY-shaped" means the
peer's surface is interactive and terminal-shaped, and there is no other handle.

| # | Candidate purpose | What the PTY actually buys | Cheaper thing that already exists | Verdict |
|---|---|---|---|---|
| 1 | **Two gopi chats talking to each other** ("A's message is B's prompt") | nothing: text in, text out | `task_submit`/ledger payloads, files, `gopi web` `POST /api/chat` (a typed JSON API, `cli_web.go:16`), plain pipes | **No.** A PTY is strictly worse here: the transport is ANSI-encrusted, needs a VT interpreter to read, and the peer's own editor rule (turn 2's driver must respect paste-burst gaps) adds failure modes for zero capability |
| 2 | **A panel**: several models debate, a chair summarizes | nothing | loop over model calls in ONE agent; memory/ledger rows | **No** (unless the panelists are foreign interactive programs, which is #4) |
| 3 | **Supervising/verifying a gopi TUI** — an agent checks what another agent's screen shows, or recovers a live session nobody attached to | yes: this is the ONLY way to see a TUI's rendered screen and to act on it | `--dump-screen` (`internal/uiclient/dump.go`) and `gopi shot --surface chat` cover the *read* half in production today; the *write* half (injecting keystrokes) is the new part | **Already shipped for reading; the writing half is a small, well-scoped addition** |
| 4 | **A foreign interactive program as a tool** — drive `python`/`psql`/`ssh`/a vendor CLI that only exists behind a prompt | yes: PTY is the only real handle; this is the expect-vs-pty case, and `ssh` even requires it | nothing else reaches these | **This is the strongest PTY-shaped use case** — but it is *program* automation, not agent-to-agent chatting |
| 5 | **Handing a live session between humans/agents across hosts** (attach, take over, hand back) | yes: `ssh` into the host, attach to the pty, continue | `gopi log`/raw logs are read-only after the fact; the chat table row can be resumed but not *attached* | **Real, larger feature** (`gopi chat --attach`), needs the controller to own the session for its whole life |
| 6 | **A gopi instance fronting a Bubble Tea app it did not write** (QA/acceptance harness for TUIs) | yes, and it is deterministic: geometry set by `TIOCSWINSZ`, screen read as text | `gopi shot --surface chat` (a one-shot capture) | **A good test-harness use**; the repo's own `testsuite/chat_pty_test.go` is this idea in miniature |

**Recommendation.** Do not build an "agent chat channel" on a PTY: for agents that
are gopi instances, the typed surfaces (task ledger, web API, files) are strictly
better, and candidate #1 — the one the phrase "two agents communicating" suggests —
is the weakest of the six. Build (or rather, finish) the two PTY-shaped ones:

* **(#3/#6) the TUI as an observable, drivable artifact** — reading is `gopi shot
  --surface chat` today; what is missing is *acting*: injecting keystrokes into a
  running interactive session and reading the resulting screen, with the
  echo/marker discipline of §5. That is the piece this study actually validated,
  and it is small: the driver above is ~200 lines and adds no dependency.
* **(#4) a foreign REPL prompt as a tool** — a `gopi run-interactive <argv>` shape
  where a gopi agent drives a program that only speaks through a prompt. That is a
  genuine new capability, and it is where a PTY is not an alternative but the
  requirement.

And the honest half: **there is no evidence that agent-to-agent chat is worth
building**. The operator's own doubt ("what is the purpose of chat of two agents")
survives this study as a *no* — not because it cannot be done (it can, and was),
but because every purpose we could name is served better by a mechanism gopi
already has.

## 7. Risks, costs, and what is not done

* **Reading the stream is the expensive part**, not the transport: only the
  interpreted grid is the screen (§2), so any consumer must run a VT interpreter —
  fine inside gopi (one already exists), a dependency for anyone else.
* **The echo/marker discipline is load-bearing** (§5). A naive implementation
  reports success on the echo. Anything shipped on this must carry the
  "expectation not typed by the controller" + "exclude own rows" rule.
* **Keystroke injection must respect the paste-burst policy** (§4 step 4) or the
  peer treats typed input as a paste.
* **pty lifetime**: the driver kills the session by process-group signal; a real
  feature needs ownership and a reclaimer, the same shape as a sandbox (Rule 34).
* **Not done**: no product surface was added (Rule 3 — this is a study; the
  recommended `#4`/`#3-write` work is a separate task); no production code was
  changed, so no repo test suite was run (the driver is study code outside the
  worktree); `gopi ui` (Bubble Tea over `uiclient`) was read and reasoned about but
  not driven — the loop was exercised against `gopi chat`, which shares the editor,
  the geometry source and the terminal gating, but a `gopi ui` run would exercise
  the `uiclient` half as well.

## 8. Verification

```
Build:  CGO_ENABLED=0 go build -o <scratch>/gopi ./cmd/agent/   → BUILD_OK (job-1, exit 0)
        CGO_ENABLED=0 go build ./...                            → BUILD_OK
        go build ./.ptyprobe/ (driver)                          → DRIVER_BUILD_OK (exit 0)
Tests:  go test ./internal/standards/ -run 'TestDocManifestRowsAreWellFormed|TestDocClassBannerMatchesManifest' -count=1
          → ok  	github.com/nick/gopi/internal/standards	0.160s
        go test ./internal/standards/ -run TestDocManifestCoversEveryDevdocFile -count=1
          → FAIL, and NOT because of this doc: five OTHER devdoc files are tracked on
            master with no manifest row (devdoc/raw-cli-slow-and-silent.md,
            resume-worktree-cleanup-self-position.md,
            self-knowledge-prompt-section-followup.md, self-knowledge-prompt-section.md,
            worktree-release-self-veto.md; `git show HEAD:devdoc/DOC-INDEX.tsv | grep -c`
            = 0 for each). Classing a doc you have not read is the exact failure the
            manifest exists to prevent, so this is REPORTED, not guessed at: dispatched as
            task 01a0d619-1423-7b90-8a58-ab195d43d5d6 (tags bug,docs,gate). This task's
            own row IS present (devdoc/DOC-INDEX.tsv:1535) and the gate's count moved
            2341 → 2342 rows.
        No production code was changed, so no other suite was run; the pre-existing pty
        harness these results agree with is testsuite/chat_pty_test.go.
Ran:    <scratch>/driver -isatty-probe
        <scratch>/driver -bin <scratch>/gopi -args chat -line "what is 17*23? …" -expect 391 -cols 200 -rows 50
        <scratch>/driver -bin <scratch>/gopi -args chat -line "Reply with a six-digit number whose six digits are all identical, digits only, nothing else." -turn2 "Change exactly one digit of %d and reply with the resulting number only." -reply 150s -cols 200 -rows 50 -tee <scratch>/raw-loop5-stream.bin
        printf 'reply with exactly: PTYOK\n' | <scratch>/gopi chat        (the pipe contrast)
Result: probe → IsTerminal: /dev/null false, pipe false, /dev/ptmx master true (termios true).
        pty run → prompt "you>" on the grid after 5.13s; marker "391" seen on row 44
        (`▌ 391`) 7.97s after the injected CR; the TUI's own status row shows iter 1.
        two-turn run → turn 1 derived off the screen (`▌ 444444`), turn 2's line built
        from it, turn 2's answer derived back (`▌ 444445`); 21880 bytes / 1625 escape
        bytes on the master; exit 0.
        pipe run → exit 0, 464 bytes, 0 escape bytes, cooked reader (no grid).
Commit: 723ecde8  docs(pty): feasibility study — a gopi TUI as an agent channel, executed end to end
```

## 9. Reproduction recipe (for whoever runs this next)

The loop, in the order it must be done — every step is a mistake waiting to happen:

1. `gopi sandbox create --label <x> --task-id <id>` (Rule 34) — never the operator's
   profile; export `HOME`/`GOPI_HOME`/`GOPI_SETTINGS` into it, and pass the provider
   env through (`env -i` + explicit vars keeps the child reproducible).
2. Open `/dev/ptmx`; `lineedit.PtyNumber(fd)` → slave number; `lineedit.PtyUnlock(fd)`;
   `unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, …)`; open `/dev/pts/<n>`.
3. `exec.Command(bin, "chat")` with stdin=stdout=stderr=the slave and
   `SysProcAttr{Setsid: true, Setctty: true}`.
4. One goroutine: `master.Read` → `lineedit.Screen.Feed`. The grid is the screen; the
   byte stream is not.
5. Wait for the prompt on the **grid**, never on a sleep.
6. Type one character at a time (`lineedit.PasteBurstCharInterval * 4`), then `"\r"`
   after `lineedit.PasteEnterSuppressWindow * 2`.
7. Only accept a marker that is (a) a value the controller never typed and (b) on a
   row that is not the controller's own echo — and dump **at the moment of the
   match**. Poll for a derived value; a single read after "the redraw settled" reads
   the echo.

## 9. Where the code and the raw evidence live

The driver is study code and deliberately NOT committed (Rule 30: a worktree
carries the deliverable, not scratch): it lives at
`~/.gopi/sessions/01a0d5d1-4bd2-7767-a345-c4188dfa6286/ptyeval/driver/main.go`,
with the raw runs (`run-pty*.txt`, `run-loop*.txt`, `run-pipe.txt`) and the raw pty
byte streams (`raw-*-stream.bin`) beside it. This document is the committed
deliverable.

**Deliverable declaration (Rule 6d):** branch
`task/01a0d5d14bd27767a345c4188dfa6286` in the main repo (this per-task worktree);
recover the doc with `git show task/01a0d5d14bd27767a345c4188dfa6286:devdoc/pty-agent-channel-feasibility.md`.
