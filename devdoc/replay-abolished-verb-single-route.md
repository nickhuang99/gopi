<!-- doc-class: normative -->
# The `replay` verb is abolished — replay has ONE route, `gopi chat --replay`

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Date**: 2026-10-03
**Task**: `01a0ffbb-a1bc-7c31-9d2f-631e475ee449` — item 1/5 of the replay-into-gopi epic
**Base commit**: `c8e71550`
**Worktree**: `/home/user/workspace/gopi-01a0ffbba1bc7c319d2f631e475ee449`

---

## What was wrong

Replay had **two routes and two renderers**. `gopi replay` drew its own
band/editor/status surface (`internal/replay/replay.go`, 1176 lines, parsing its
own flags: `--session`, `--scratch`, `--cols`, `--rows`, `--dump`, `--no-redact`,
`--max-beats`, `--ack-timeout-ms`) and a shell pipeline filmed it
(`scripts/replay/replay.sh` + `tools/replay/*.py` + `tools/termemu/`). Beside it,
`gopi chat --replay <session>` re-ran a recording **on the real console**.

Two renderers means two answers to every question a terminal asks, and the split
had already produced a measured defect: the self-drawn renderer classified its
colours from the environment only, never asking the tty it was holding, so the
film came out grey (`devdoc/replay-colour-lost-to-redirected-stderr.md`). The
operator's ruling (2026-10-03) was to tear the second renderer out rather than
patch it, and the comparison with `log`/`raw`/`stats` was about **status**
(replay is a core function), never about **shape** (it is a FLAG, not a verb —
memory #7653).

## What changed

| surface | change |
|---|---|
| `cmd/agent/cli_replay.go` (51 lines) | **deleted** — the verb's dispatch face |
| `cmd/agent/cli_verbs.go` | `CliVerbReplay = "replay"` **deleted** |
| `cmd/agent/main.go:554` | `isReplaySubcommand(dispatchArgs)` removed from the dispatch chain |
| `internal/replay/replay.go` (1176 lines) | **deleted** — the self-drawn renderer: its flag set, its band/editor/status drawing (`statusTyping`/`statusAnswering`/`statusReady`/`statusTurnEnd`/`statusTooSmallFmt`/`statusNoTerminalFmt`), its session reader and its beat emitters |
| `internal/replay/capability_parity_test.go`, `colour_sink_test.go` | **deleted** — both are defined entirely against the deleted renderer's own functions (`replayCapability`, `colourSinkFor`), which is what the task's constraint (7) prescribes. The console-side half of that pair — the guard that the console reaches the one entry point that ASKS the terminal — is `cmd/agent/terminal_capability_wiring_test.go`, which stays green |
| `scripts/replay/`, `tools/replay/`, `tools/termemu/` | **deleted** (3 directories, 6522 lines) — AFTER the criteria were copied out (see below) |
| `Makefile` | the `replay` target removed (and `replay` dropped from `.PHONY`); two comments rewritten. No second entry point was put in its place: the ONE route is the flag |
| `internal/config/help.go` | the `gopi replay` index row removed from the command index |
| `cmd/agent/cli_completion.go`, `cli_completion_test.go`, `cli_contracts_test.go`, `usage_test.go` | every `CliVerbReplay` reference removed — the hand-maintained mirrors and the three contract tables |
| `internal/replay/{ledger,redact,session_ground}.go` + `redact_test.go`, `session_ground_test.go` | **kept**, comments re-pointed: they are the vocabulary `gopi chat --replay` (cmd/agent) imports for the LIVE surface (`LedgerWriter`/`LedgerName`/`Redactor`/`MaxIdentShapeRunes`) |
| stale comments in 8 further files (`chat_banner.go`, `lineedit_wire.go`, `colour.go`, `gifgen.go`, `testsuite/chat_layout_stream_pty_test.go`, `.gitignore`, `devdoc/one-binary-census.md`) | re-pointed off the deleted script paths and off `make replay` |

## Order mattered: the criteria were copied OUT before the directories went

`tools/replay/verify.py` (1609 lines) held the film arm's five self-证 criteria.
They were copied, **byte for byte**, into the run evidence directory as
`verify-py-criteria.md` FIRST — the deletion happens after, so no criterion can
be lost with the file:

| criterion | source lines | lines copied |
|---|---|---|
| `check_traceability` | L1043-L1265 | 223 |
| `check_legibility` | L1279-L1313 | 35 |
| `check_motion` | L1316-L1378 | 63 |
| `check_withholding` | L1381-L1471 | 91 |
| `check_colour` | L1533-L1576 | 44 |

with `sha256(verify.py) = e4f08eda…1d215d` and the L1595-1599 call site, so a
later reader can tell the copy is complete and unweakened. Item 3 of the epic
ports them into Go from that evidence file.

## The verb list did NOT grow

`gopi --help` prints **57** `  gopi <verb>` index rows after the change and **58**
before; the only diff line is

```
<   gopi replay                    Render a RECORDED chat session onto a real terminal.
```

and `./bin/gopi replay -h` no longer refuses a verb — `replay` is not a dispatch
word, so the argv falls through to the ordinary task path and the top-level help
is printed (the same thing that happens for any word that is not a verb).
`grep -rn CliVerbReplay .` returns **0** hits in the whole tree.

## Two GATES the first commit missed, and their fixes

The first cut of this change ran `go build` and `go vet` clean, and the gates
that actually know this surface failed **red**. Both are recorded here because
each is a place a future ablation of a verb will land:

- **`internal/config.PositionalVerbs`** still listed `"replay"`
  (`cli_verb_guard.go:274`). That list refuses a word reached in the positional
  path, and a word with no command behind it must stay prompt text — so
  `TestPositionalVerbGuardVocabularyMatchesDispatch` failed with *"guards
  `replay`, which this binary neither dispatches nor names as a refused
  spelling"*. One line removed.
- **`devdoc/gopi-verb-census.md`** still carried the row
  (`| \`replay\` | 1 | 1 | 0 | 0 | 0 | 4 | 1 | b:doc-recipe |`), so
  `TestVerbCensusMatchesTheDispatch` failed in two directions: *"row `replay`
  claims dispatched=1, but cmd/agent/main.go's chain dispatches no such verb"*
  and *"claims advertised=1, but the built binary's `gopi -h` does not list
  it"*. The table is regenerated, not hand-edited — the generator is
  `bash scripts/g7-verb-census.sh ./bin/gopi` (Rule 38: a census figure names its
  generator and its commit). Re-derived at `971400a7`: **44 → 43 rows**,
  36 `a:test` / 3 `b:usage-log` / 4 `b:doc-recipe`; the whole table moved with
  it because the corpus columns (`usage_log`, `doc_recipe`) are host- and
  corpus-dependent, and every row's `test_drive` was re-counted. Only
  `dispatched`/`advertised`/`test_drive` are gate-checked.

A third failure class looked like my doing and was not: **12 `cmd/agent` tests
refused to run** because `bin/gopi`'s mtime predated the source — the e2e tests
drive the BUILT binary and refuse to verify code that is not in it. Rebuilding
(`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`) cleared all 12; the run after
that is the one reported below.

## `gopi chat --replay` really run (the one route)

Three real recorded sessions were re-run on a **real pty** at 117×26
(`/tmp/pty-run.py`: one terminal, fixed geometry, stderr to a file). The
acceptance's three marks are each demonstrated, though not all in ONE run — the
session I first picked was long enough to outlive the clock I could give it:

| session | turns | tool results | what it proves |
|---|---|---|---|
| `9298236c-…` | 1/1 | **3 served, `recorded=true`, byte-identical** | the executor seam serves the recording verbatim (187/204/94 bytes; the sha256 the binary printed equals the recording's own) |
| `332f7585-…` | **2/2** | 0 | the route runs a recording to `ended (normal)` |
| `f30f87ac-…` | 2/2 typed | 11 installed | a tool-bearing recording drives both turns; only the trailing `/quit` is refused by the surface's documented key ceiling (64/64 keys — a replay spends keys fast) |

The `recorded=true` + byte-identity line, verbatim from the binary's own stderr:

```
[gopi replay] served[0] id=call_00_ET_2t4SVo7rZyjgfvWxY0k65952 tool=read bytes=187 sha256=0524dab457f2a1bb56b28142e6a3455abf2e05a461d2c80e816495c7bc9c3953 recorded=true
[gopi replay] served[1] id=call_00_x9pK8AMOC6xeNaGtW9oR7299 tool=bash bytes=204 sha256=855cdcba0639e82f972cab490e3297ac43c14b19c0c2649be60ec9bd2b287265 recorded=true
[gopi replay] served[2] id=call_00_GwkNlhiSMrGFCSd4PHry4217 tool=read bytes=94 sha256=133c6c8c1f8f6d7bc6cbdb975b53bd3eb5921229a6c6b875fe1a2054afe634d0 recorded=true
```

Evidence: `chat-replay-evidence.txt`, `chat-replay-pty*.err`, and the pty output
files, all in the run's evidence directory.

## What was NOT done here (honest)

- The film pipeline (Xvfb + a real terminal + GIF assembly) is **not** ported —
  that is a later item of this epic, and `Makefile` now carries a comment saying
  so rather than a target that would be a second entry point.
- The default ideal geometry (a first-class chat width/height) is a later item.
- The five criteria are **copied**, not yet ported into Go assertions.
- A shell driver that used to ask `gopi replay session <operand>` for
  `record=`/`dir=`/`artifacts=`/`film=` no longer has that pre-step: the resolver
  behind it (`internal/replay/resolveSessionGround`) is the SAME resolver
  `gopilog.FindSession` + `gopi chat --replay` use, but the printing entry point
  is gone with the verb by design — a session is named by the flag now.
