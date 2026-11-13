---
title: GOPI
section: 1
header: GoPi Manual
---

# NAME

gopi - a tool-using AI coding agent

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
  gopi [options] "<prompt>"           Run one task from a prompt argument
  gopi [options] input.txt            Run one task read from a text file
  gopi [options] < input.txt          Run one task read from stdin
  gopi [options] --prompt TEXT a.png  Run one task with an attached image
  gopi -h | --help                    This help
  gopi --help-env                     The complete environment-variable table
  gopi <command> -h                   Full help for one command
  gopi version                        Build version (also --version / -v)
```

<!-- END SYNOPSIS -->

# DESCRIPTION

**gopi** is a tool-using AI coding agent. One run is one task: the model reads
and edits files, runs shell commands, searches code and the web, and stores
distilled lessons.

Run it with a prompt, a prompt file, or a prompt on stdin. A positional that
names an existing text file contributes that file, and a positional that names
an existing image (`.png`/`.jpg`/`.gif`/`.webp`) is attached natively — pixels,
no OCR round-trip — while auto-switching the run to the vision model.

Everything after the prompt is a **verb**: `gopi <verb> ...`. Each verb answers
to the same three-word contract — **`gopi <verb> -h`** prints that verb's own
page and exits 0 — so every detail below has a page of its own. This page is
the index; the detail lives on the verb's page, and the complete knob table is
`gopi --help-env`.

# COMMANDS

The verbs this page's own `gopi -h` index lists. The block below is GENERATED
from that help page (`internal/standards/mancensus`) and checked by
`cmd/agent/man_source_gate_test.go`, so a verb added to the CLI reaches this
page or the build goes red:

<!-- BEGIN VERB INDEX (generated — internal/standards/mancensus; do not edit by hand) -->

- `gopi chat`
- `gopi config`
- `gopi memory`
- `gopi notify`
- `gopi screen`
- `gopi screen-replay`
- `gopi session`
- `gopi task`
- `gopi tool`
- `gopi ui_state`
- `gopi version`
- `gopi web`
- `gopi worker`

<!-- END VERB INDEX -->

A verb's own page is one `gopi <verb> -h` away, and — for the verbs that have
one — one `man gopi-<verb>` away.

# INVOCATION SEMANTICS

What a positional word IS, decided before anything is read as a task:

- **A bare word or sentence is prose** — the task text as typed. No epic goal,
  task-list directive or reframe protocol is added to it: the text is the
  model's first user message verbatim. The run is still governed by the agent's
  system prompt and its own checkpoints.
- **A token that LOOKS like a path** — it carries a `/` or one of
  `.txt/.md/.png/.gif/.jpeg/.webp` — is treated as a FILE. One that names
  nothing is **refused by name** (exit 1), never silently sent as prompt text;
  pass such text with `--prompt TEXT` or `\@TEXT`.
- **`@PATH`** reads the task payload from PATH verbatim; `@-` reads stdin, and
  `\@text` passes a literal prompt that starts with `@`.
- **An existing `.png/.jpg/.gif/.webp`** is attached NATIVELY (pixels, no OCR
  round-trip) and auto-switches the model to the vision one; `--prompt TEXT`
  carries the instruction that goes with it.
- **An existing non-image file** contributes its text to the task: alone it IS
  the prompt, and beside `--prompt TEXT` its text is appended to the
  instruction. For a file read as the prompt verbatim, use `@PATH`.
- **`./GOPI.md`** is the client's own local rules, auto-loaded as a system
  message — accepted as a fallback when only it exists beside the tracked
  `./PROJECT.md`.
- **With no prompt and no stdin** gopi prints this page and exits 1.

A DISPATCHED run — `gopi worker` — is framed
instead: its briefing is wrapped in the `[EPIC GOAL]` / `[TASK_LIST_BEGIN]` /
`[EPIC REFRAME]` frame. The `task run` spelling this line used to name is a
retired verb: it answers exit 1 and names `gopi worker`.

# OPTIONS

Every option below also exists as an environment variable, a `settings.json`
key and a CLI flag. Precedence: **flag > environment variable > settings.json >
default**. The complete table is `gopi --help-env`.

**No single-dash options.** A spelling like "-z" is refused by the parser; pass
it as task text with `--prompt -z`.

`--prompt TEXT`
: the task instruction. Positionals still carry task input — a text file's
contents are appended to the instruction.

`--settings PATH`
: path of `settings.json`.

`--home PATH`
: gopi's data root — sessions, raw logs, `memory.db` and settings under
`$GOPI_HOME`.

`--model NAME`
: the model a task run uses.

`--api-key KEY`
: the credential for the endpoint.

`--base-url URL`
: the API base URL.

`--thinking ON|OFF`
: thinking mode.

`--max-tokens N`
: per-response output cap.

`--context-window N`
: context window budget.

`--timeout SECONDS`
: overall agent timeout; 0 means unlimited.

`--tmpdir PATH`
: temp workspace for gopi and its children.

# FILES

`~/.gopi/sessions/<session-id>/`
: one directory per run: the session log, the raw log and the run's own
artifacts. A run's evidence lives here, never in a checkout.

`$GOPI_HOME/.gopi/memory.db`
: the knowledge base behind `gopi memory` and the agent's `memory` tool.

`./GOPI.md`
: project rules, auto-loaded as a system message when present.

# SEE ALSO

`gopi -h`, `gopi --help-env`, `gopi <verb> -h`, and the operator-facing
`COMMANDS.md` (the CLI manual of record) and `CONFIG.md` (every knob and its
source line).
