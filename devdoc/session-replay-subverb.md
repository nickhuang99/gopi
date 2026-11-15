<!-- doc-class: normative -->
# `gopi session replay` — the PLAYBACK, as a sub-verb of the `session` family

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Date**: 2026-10-04
**Task**: `01a1076d-460f-76d8-8bf1-6a9041e187d5`
**Deliverable branch**: `task/01a1076d460f76d88bf16a9041e187d5`
**Hand-over branch ported**: `task/01a1072658a47827a9dc5c32d5af40bf` (tip `6c47bd24`)
**Abolition this must not undo**: `devdoc/replay-abolished-verb-single-route.md` (`971400a7`)

---

## What the door is, and why this one

The playback — a recorded session drawn back onto a terminal as the SCREEN it
was, with transport controls — was written once as the top-level `gopi replay`
(`6c47bd24`) and the consolidation of 2026-10-03 **abolished that verb**: replay
has ONE route, `gopi chat --replay <session>`, which **RE-RUNS** the recording
against a stub. The abolition's own reason is a rule about the verb list, not
about the capability: `gopi log` (the recording as text), `gopi raw` (the wire
bytes) and `gopi stats` (the numbers) are all readers of ONE session id, and the
`session` family is where this binary already keeps them.

So the value comes back through the door `cmd/agent/cli_session.go` had already
reserved for it:

> `session replay` (the recorded-chat renderer) is re-homed by a SIBLING task
> and this file does not implement it.

| spelling | question it answers | the recording is |
|---|---|---|
| `gopi chat --replay <id>` | what happened when the model ran again | the **input** |
| `gopi session replay <id>` | what was **on the screen** | the **material** |
| `gopi replay` | — | **abolished, un-dispatched, and gated** |

```bash
gopi session list --mode chat          # prints full session uuids: the operand below
gopi session replay <session-id>       # play it in THIS terminal
gopi session replay <session-id> --frames        # the frame sequence, no playing
gopi session replay <session-id> --frame N       # one frame, plain text, no controls
gopi session replay <session-id> --terminal NAME # play in a NEW terminal window
```

## What was ported, and the ONE coupling that had to go

`internal/replayplay` came over **verbatim** (material / render / loop / help /
run + their tests, `6c47bd24`) with one edit: the package no longer names a
**dispatch word**.

Before | After
---|---
`const CliVerbReplay = cliverbs.Replay` (run.go), aliased from `internal/cliverbs` | `const PlaybackCommand = "gopi session replay"` — the **description of the operation** the chrome prints
`internal/cliverbs.Replay` (and `CliVerbReplay` in cmd/agent, its completion/contract/inventory tables, `internal/config/help.go`, `PositionalVerbs`, the census) | **gone**: `grep -rn CliVerbReplay cmd internal` = 0 hits (the occurrences left anywhere are prose in `devdoc/` and the gate’s own literal)

`cliverbs.Replay` was dropped rather than kept, and that is a Rule 14 decision,
not tidiness: Rule 14 wants a value SHARED ACROSS PACKAGES to have one
definition — but once the word is a **sub-verb of the `session` group**, the only
thing that dispatches it is `cmd/agent`'s own sub-verb table
(`sessionSubcmdReplay`, beside `sessionSubcmdList`/`sessionSubcmdShow`), which
`internal/replayplay` can neither import (package main) nor needs. A leaf constant
for a word with one consumer is a second definition site waiting to drift, so
there are now exactly two strings and each has ONE owner: the dispatch word
(`cmd/agent`) and the printed name (`internal/replayplay.PlaybackCommand`).

## The dispatch — no new verb, no census edit

* `isSessionSubcommand` (already the chain entry the G7 census counts) → `runSessionGroup`
  → a new `case sessionSubcmdReplay` whose arm calls `runSessionReplay(rest)`.
  It is dispatched **directly**, not through the argv-substitution arm the four
  aliases (`log|stats|raw|resume`) use: there is no top-level spelling of it to
  substitute back in.
* `sessionSubcommandNames` gained `sessionSubcmdReplay`, and
  `sessionGroupCompletionNames` **IS** that slice — so TAB completion, the
  unknown-subcommand error and the family page all follow by construction
  (Rule 13).
* `sessionUsage` gained the synopsis line `gopi session replay <id> …`, which is
  what `TestSessionFamilyUsageAdvertisesEverySubVerb` demands.
* **The census was not touched.** `TestVerbCensusMatchesTheDispatch` and
  `TestPositionalVerbGuardVocabularyMatchesDispatch` are green with **no edit to
  `devdoc/gopi-verb-census.md`** and **no word added to
  `internal/config.PositionalVerbs`** — the verb count did not grow, because
  `replay` here is a second level of the `session` group, not a verb.
* `cmd/agent/cli_contracts_test.go` gained one inventory row
  (`session replay -h` → the player's page), in the same second-level shape as
  the existing `session list -h` / `session log -h` rows.

## The regression gate

`cmd/agent/session_replay_abolition_test.go` — the four surfaces that would have
to move together for the top-level verb to come back, checked together:

* **(a) the guard vocabulary** — `config.PositionalVerbs` must not carry
  `replay`, with a non-vacuity floor on the list (`< 20 words` = truncated, so
  the check proves nothing).
* **(b) the census** — no row for `replay`, again with the row floor.
* **(c) the built binary** — `gopi replay -h` must print the **top-level help**,
  never the player's page, and the top-level help must not advertise `gopi
  replay`; `gopi session replay -h` **is** the player's page (the positive half,
  so "came back through the right door" is distinguishable from "was dropped").
* **(d) the source** — no `CliVerbReplay` identifier in any `.go` file under `cmd/` or
  `internal/`, scanned as a TREE (that identifier came back twice in history
  through tables nobody thought of as "the verb"), with the test file that spells
  it on purpose excluded **by name** — never by substring, which would also
  exclude any file merely quoting it.

The gate is written to fail on the RED path by construction (`replay` is a
literal there, so it cannot follow a constant to a new home).

## Carried defects (ported as-is, stated because a reader will hit them)

* **The ~5 s unanswered-probe window is pre-existing, not this sub-verb's.**
  Every gopi invocation on a terminal that answers nothing pays it: bubbletea's
  `init()` calls `lipgloss.HasDarkBackground()`, which waits `termenv.OSCTimeout`
  (5 s) for an OSC 11 reply. The strace is in the original record
  (`devdoc/replay-verb.md`, on the hand-over branch); the suite's pty driver
  answers the probe the way `internal/lineedit`'s own driver does. A fix belongs
  to a decision about the bubbletea startup path.
* **`--terminal NAME` is not exercised end to end in the suite** — the case
  asserts the REFUSAL (gnome-terminal, from the registry) and the flag's parsing;
  spawning an emulator needs a real display. Run
  `gopi session replay <id> --terminal xfce4-terminal` by hand to watch a window
  open at the recorded geometry.
* **Timeline frames are our rendering of the events** (a session that recorded no
  dump has no cells and no colour), and the playback says so twice: the `source=`
  field, the frame-kind tag, and a distinct tint.

## Files

| path | what |
|---|---|
| `internal/replayplay/{material,render,loop,help,run}.go` | the ported playback (frame model, cell-exact renderer, transport, ONE key map) — verb coupling removed |
| `internal/replayplay/*_test.go` | material, render and transport, no terminal needed |
| `cmd/agent/cli_session_replay.go` | the sub-verb: selector, `--frames`/`--frame`, `--terminal` via `internal/shot`, pty transport |
| `cmd/agent/cli_session_replay_test.go` | the sub-verb driven for real: pty keyboard, static views, refusals, help page |
| `cmd/agent/session_replay_abolition_test.go` | the abolition gate + the positive door check |
| `cmd/agent/cli_session.go` | the dispatch arm, the vocabulary entry, the family-page synopsis |
| `cmd/agent/cli_contracts_test.go` | the `session replay -h` inventory row |
