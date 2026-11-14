<!-- doc-class: normative -->

# gopi

**A single Go binary with zero runtime dependencies, built for the one-person software company, the very small shop, and any digital developer who has to be a swiss-army-knife generalist — its job is to dispatch work, keep the books and answer questions later, not only to chat.**

```
$ gopi --version
v0.4.0
go: go1.24.0
```

> This file is gopi's **front door**: someone who has just heard of it should finish
> this page knowing what it is, what it can do, how to install it, and where to look
> up the details.
> If you already use it and want the arguments of some verb — that manual is
> [`COMMANDS.md`](COMMANDS.md), not this page.

---

## Three ways it differs from the mainstream agents

Most coding agents (claude code, codex, cline, crush, and the like) take the shape
"a REPL + a dependency tree + a `--verbose`". gopi took a different road on these three things:

### 1. One binary, zero runtime dependencies

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
```

`cmd/agent/` is the **only** main package in the whole tree — that is not a convention, it is a
rule pinned down by tests (the comment at the head of the `Makefile`, the gates in
`internal/standards`).
**No Node version manager, no Python virtualenv, no `node_modules` tree.**
Copy one binary onto the target machine and it runs; its configuration is a single JSON file.

### 2. A task is a row in a ledger, not a stretch of conversation in a context window

The unit of work in a mainstream agent is the **conversation**: you finish talking, the
context is compacted, and that thing leaves no trace. The unit of work in gopi is the
**task row**:

```bash
$ gopi task submit "把 config 加载路径从相对改成绝对" --tags refactor
Task queued: 01a1ad83-33d1-76a3-b4be-15704b366409 (status=pending)
```

Each row has its own **id, state machine, claimer, delivery branch, and re-readable
session directory**. It can be tracked through `pending` → `claimed` → `in_progress` →
`completed`; a failure can be `--resume`d (the same id, the same directory, the checkpoint
appended), or `clone`d into a clean-room copy that redoes the work from scratch.

**This means**: "is this thing done" is a fact you can query (`gopi task show <id>`),
not a summary you have to take on faith.

### 3. Three layers of transcript, not one switch

A mainstream agent's audit surface is one `--verbose` or one jsonl file. gopi splits the
same history into four tiers:

| What you want to see | Command | Granularity |
|---|---|---|
| Which sessions ran, and what they cost | `gopi session list` / `gopi session stats` | session level |
| What happened in every round | `gopi session log <id>` | round level |
| The complete request the model saw | `gopi session raw --file <id>.raw.log` | request-body level |
| The actual bytes on the wire | the same `.raw.log` | raw-wire level |

**When "it said it did it" and "what it actually sent" disagree, the fourth tier is the only
layer that can adjudicate.**

---

## What it can do

17 capability surfaces. Each one gives a command that **really runs** (not an illustration):

### Conversation and execution

| I want to | Command |
|---|---|
| Talk to it | `gopi chat` |
| Have it finish one small job in one shot, without entering the loop | `gopi chat --once -p "把这个函数的错误分支补上"` |
| Dispatch a self-contained piece of work and collect the result later | `gopi task submit "<这件事>"` |
| See which step a piece of work is at right now | `gopi task show <id>` |
| Find work nobody has claimed yet | `gopi task list --status pending` |

### Memory and retrieval

| I want to | Command |
|---|---|
| Make it remember one reusable lesson | `gopi memory store --topic "..." --question "..." --answer "..."` |
| Ask whether it has hit this problem before | `gopi memory search "为什么 build 会红"` |
| Find a symbol definition in the code | `gopi tool code-search <Symbol>` |
| Full-text search | `gopi tool keyword_search "<pattern>"` |
| Find out who wrote a line and why | the `git_blame` tool inside a session; `git blame` on the command line |

### Seeing and acting

| I want to | Command |
|---|---|
| Capture the current screen | `gopi tool capture --screen --out s.png` |
| Capture the terminal window it is itself running in | `gopi tool capture --self` |
| Drive the desktop (move the mouse, click, type) | `gopi tool act move 100 100` |
| Read what is inside an image | `gopi tool vision_analyze --image p.png` |
| Render a piece of text into an image | `gopi tool text2img --src notes.txt` |
| Screenshot a web page (headless, never touching the real screen) | `gopi tool shot --url https://example.com` |

### Configuration and introspection

| I want to | Command |
|---|---|
| See the configuration currently **in effect** (not the file's contents — the parsed result) | `gopi config show` |
| See which environment variables exist | `gopi config show --help-env` |
| Bring the machine to a runnable state with one command | `gopi config setup` |
| Look up the full usage of some verb | `gopi <verb> -h` |

### Isolation and cleanup

| I want to | Command |
|---|---|
| Run once inside a private profile without polluting the main ledger | `gopi tool sandbox run --label probe -- <cmd>` |
| See what can be reclaimed in the temporary directories | `gopi tool scratch-gc --dry-run` |
| Clean up the working directories of finished tasks | `gopi task sweep-worktrees` |

### Service surface

| I want to | Command |
|---|---|
| Start a local web UI | `gopi web --addr 127.0.0.1:8080` |
| Deliver a message to another gopi session | `gopi notify --session <id> --message "..."` |
| Stay resident in the background and keep consuming the task queue | `gopi worker` (long-running; start it in the background with `job`) |

---

## Installation

gopi is currently **built from source**. It needs Go 1.24 or newer.

```bash
git clone <this repo>
cd gopi
make install
```

`make install` does two things: it builds `bin/gopi` (statically linked, `CGO_ENABLED=0`),
and it puts that binary at `$GOPATH/bin/gopi` (so that `gopi` is on your `PATH` and you can
type it directly).

If you would rather not touch `$GOPATH`:

```bash
make build          # build only, the artefact is ./bin/gopi
./bin/gopi --version
```

To install it somewhere else:

```bash
make install PREFIX=/usr/local/bin
```

### The first run

```bash
gopi config setup     # idempotent provisioning: brings home to a runnable state
gopi config show      # prints the configuration values in effect (not a file read — the parsed result)
gopi chat             # start talking
```

`gopi config setup` is idempotent — **running it again has no side effects**; it only fills in
what is missing.

### Dependencies

**Zero runtime dependencies.** The only build-time dependency is the Go toolchain itself.
The `require` block of `go.mod` is the complete list; no CGO is pulled in, no C compiler is
needed, and neither Node nor Python is needed.

---

## Documentation map

| What you want to know | Where to look |
|---|---|
| **The full usage of every CLI verb** | [`COMMANDS.md`](COMMANDS.md) — 421 lines, listed by verb, each line giving the source location |
| **Every configuration key's default / precedence / who reads it** | [`CONFIG.md`](CONFIG.md) — 1649 lines, one line per knob, with the source line that reads it |
| **The official description of some subsystem** | [`man/`](man/) — 14 pages, `gopi-chat.md`, `gopi-task.md`, `gopi-memory.md` and so on |
| **The engineering constraints (why the code looks this way)** | [`PROJECT.md`](PROJECT.md) — 37 rules, each paired with the test that catches a violation |
| **The history and measurements behind design decisions** | [`devdoc/`](devdoc/) — every document declares its doc-class on its own first line |
| **What changed** | [`CHANGELOG.md`](CHANGELOG.md) |

> **Every figure in these tables is measured, and each row names the command that re-derives it:**
> The row is the ONLY place a measured number is written down — no figure is repeated here, so this
> paragraph cannot drift away from the table above it. Line counts come from `wc -l <file>`, the page
> count from `ls man/*.md | wc -l`, and the rule count counts the DISTINCT rule NUMBERS that carry a
> `## Rule N` heading
> (`grep -oE '^## Rule [0-9]+' PROJECT.md | sed 's/.* //' | sort -un | wc -l`).
> A heading that merely STARTS with `## Rule ` is not necessarily a rule of its own: a lettered
> section (`## Rule 30a`) and a `## Rule 41 的下半` section belong to the rule they extend, and
> Rules 16–21 share the single `## Machine-Checkable Gates` heading — so the highest number a
> briefing may cite runs past the number of sections, and the TABLE's rule count is the count.
> These figures are guarded by `go test ./internal/standards/ -run TestDocMap`, which turns red
> when a document moves and its row does not, and again when a number is written into this
> paragraph instead of the row.

> This README answers only "what is it, and what can it do".
> For a specific argument, a specific default, the boundary of a specific behaviour — **look it
> up in the tables above**; they are derived from the source, not written from memory.

---

## Its boundaries (an honest section)

**gopi is not "does everything".** The following are deliberate design trade-offs, not a to-do list:

- **No MCP.** There is a plugin tree, but no Model Context Protocol client.
  Adding MCP would mean a new protocol plus a resident connection — which trades against
  "zero dependencies, one binary". This trade-off is **awaiting a ruling**, not **awaiting
  implementation**.
- **No hosted install script.** There is no `curl ... | sh`. Installing is a `make` target plus
  a source tree, because a script that comes off the network and executes with your
  privileges is a dependency that should not be pulled in lightly.
- **It will not install software on someone else's machine for you.** Gathering evidence,
  probing and verifying are all allowed, but "install a third-party agent so that a test can
  run once" is explicitly forbidden.
- **It makes no promise to stay fast under heavy device load.** gopi has no latency promise,
  and it does not generate load in order to measure latency.

---

## Status

`v0.4.0` — under active development. The interfaces will change, but every change is recorded
in `CHANGELOG.md`.

```bash
$ gopi version
v0.4.0
```

The specific revision it was built from:

```bash
$ ./bin/gopi --version | grep revision
```
