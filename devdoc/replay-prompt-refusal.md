<!-- doc-class: normative -->
# `gopi replay <session-id>` is refused by name — a briefing line must not buy an agent run

**Date**: 2026-10-06
**Task**: `01a108aa67987887b369cc0d7a928f50`
**Worktree**: `/home/user/workspace/gopi-01a108aa67987887b369cc0d7a928f50` (branch `task/01a108aa…`)

---

## The defect, measured

A task briefing opened with `gopi replay 56507166-ef62-4d2d-bafe-af504fca9311` — the
spelling the recorded-chat playback had **before** the 2026-10-03 consolidation
abolished the top-level verb (`devdoc/replay-abolished-verb-single-route.md`).

That word is in no dispatch table and was in no refusal table either, so the run path
did what it does with any unrecognised positional: **it spent the briefing line as the
PROMPT**. A real agent session was minted (a session directory, an `[auto] <line>`
ledger row, tokens) and the reply was the model asking what "replay" meant.

This is the same shape as the near-miss family that prompted the second refusal surface
(`cli_verb_hint_guard`, task 01a0f72b: 123 `[auto]` rows in the 2026-10-01 audit), with
one difference that makes it worse: the wrong word arrived from a **document**, so the
cost is paid repeatedly and silently by a machine rather than once by a person.

## The fix, and why it is not the two tables that came to mind

One row in `internal/config`'s near-miss vocabulary (`commandHintWords`) plus the
OPERAND shape that answers it. Three tables could have carried the word; two of them are
wrong, and the reasons are what a later reader needs:

| candidate | why not |
|---|---|
| `verbAliases` (`cmd/agent/cli_verb_aliases.go`) — the RETIRED-word table | A row there is refused at **argv[0] in every shape, `-h` included**. Measured: `gopi buildinfo -h` → exit 1 with the refusal (`TestVerbAliasIsRefusedByNameNoRun` drives `{alias, "-h"}` and requires exit 1). This word must keep `-h` legal — `gopi replay -h` prints the top-level page and exits 0 — so an alias row would break the very acceptance contract this change serves. |
| `config.PositionalVerbs` — the after-a-flag guard | That list refuses a word by **casting it as a COMMAND** ("put the command first: `gopi <address>`"), and its contract (`cli_verb_guard_test.go`) requires every word in it to be one this binary **dispatches** or refuses by name. `replay` is neither: it is a word for a command under **another door**. Putting it there would also make the sibling abolition gate's face (a) red — that gate asserts the word is **absent** from `PositionalVerbs`. |
| `commandHintWords` — the near-miss vocabulary (chosen) | It is the only run-path surface that answers a word the dispatch chain does not know, it refuses with exit 1, and it is reached **after** the help scan — so `-h` never gets there and the top-level page survives. |

The word is therefore refused, `-h` stays legal, and the census/`PositionalVerbs`
tables are untouched — which is the epic's stated "without reviving the top-level verb".

## The narrow criterion: the OPERAND is a session id, not "a word near replay"

The near-miss vocabulary answers two shapes by ARITY: one positional (`gopi revision`),
or a near-miss followed by **one** word (`gopi branch list`). A session id is neither —
it is an operand, not a sub-verb — so the play word gets one additional predicate:

```
gopi replay <session-id>            → refused   (the word, a session-id, ≤1 more word)
gopi --timeout 3 replay <session-id> → refused   (the same shape after a run flag)
gopi "replay my day in three lines"  → RUNS      (four positionals: prompt text)
gopi "replay the failed attempt and tell me why it died" → RUNS
```

The id is checked by **SHAPE only** (`isSessionIDShape`: the RFC 4122 8-4-4-4-12 run
layout over hex), never by a ledger lookup. This is deliberate and it is the stronger
choice: the id in a briefing usually names a session **this machine does not hold**
(another host's profile, a reclaimed scratch root), so a lookup-based refusal would fall
through to a whole agent run for exactly the case that hurt — and it would make this
guard's answer depend on profile state, which no other refusal here does.

The bound is one trailing word (`commandHintReplayOperandMax`), so a sentence that
merely **contains** an id stays prompt text.

## The message names BOTH doors, because the capability has two

The refusal prints the sub-verb's own page **and** both live routes, each with the
question it answers:

```
gopi: "replay" is not a gopi command — this binary would have started a RUN with it as
the prompt (and registered an [auto] task row). To send the word as a prompt, say so
explicitly: `--prompt replay`. `gopi session replay -h` shows the command that answers
this request; `gopi -h` lists every command this binary has. This request has more than
one live door: `gopi session replay` PLAY the recording back as the screen it was (with
transport controls) and `gopi chat --replay` RE-RUN the recorded conversation on a live
console (the recording is its input).
```

One door alone would be the dead end Rule 15 bans from the other side: the reader would
try the wrong one and conclude the capability is gone. Both routes are **table rows**
(`commandHintReplayRoutes`) projected to the test, so the live-binary contract drives
every command line the message prints — a route that stopped running fails the build
instead of shipping as a second dead hint.

## The player it points at: provenance, recorded honestly

The briefing's second half assumed a sibling commit `456c459b4` (`gopi session replay` +
`internal/replayplay/`). **Measured: that commit is NOT an ancestor of this HEAD** — it
exists here only under the tag `archive/hp-pre-realign-20261006`, and neither HEAD nor
peer1/master carries `internal/replayplay/`. What HEAD has is `gopi chat --replay`
(`internal/replaydrive`), the re-run.

So the two halves shipped together, on this branch, in this order:

1. **the refusal** (this change's own deliverable) — it is correct whether or not the
   player exists, because the word is not dispatched either way;
2. **the player**, applied from that commit (`git cherry-pick --no-commit 456c459b4`),
   with its conflicts against HEAD resolved by hand: HEAD had since added the
   `session checkpoint` sub-group and **retired the four flat spellings** the sibling
   commit still dispatched, so its contract-table rows (`stats`, `raw`, `resume`,
   `checkpoint`, `proxy` under their flat argv) were **dropped** and the family
   spellings kept — the same words now live in `cli_verb_aliases.go` as retired rows.

That ordering matters and is recorded because it is not reversible later: without step
2 the refusal would have had to send the reader to a command that does not exist in this
tree, which the epic's narrow criterion forbids. With step 2 the address it prints is
**executed by the gate** (`gopi session replay -h` → exit 0, the player's page).

## Verification

Three instruments for "no run happened", because one alone lies (a refused run and a
fast run can both print little): the exit status, the absence of a session directory
under an isolated `GOPI_HOME`, and an unchanged ledger row count.

| probe | result |
|---|---|
| `gopi replay 56507166-ef62-4d2d-bafe-af504fca9311` | exit **1**, the refusal above, **no** `$SB/.gopi/sessions`, **no** ledger row |
| `gopi --timeout 3 replay 56507166-…` | exit **1**, the same refusal |
| `gopi replay` (bare) / `gopi replay <uuid> x` | exit **1** |
| `gopi replay -h` | exit **0**, the TOP-LEVEL page (the word is not advertised there) |
| `gopi "replay my day in three lines"` | **not** refused — reaches the run path and mints a session |
| `gopi session replay -h` (the printed address) | exit **0**, `gopi session replay — play a recorded session back as a PICTURE.` |
| `gopi chat --replay -h` (the second printed route) | exit **0** |

Gates: `go test ./internal/config ./internal/replayplay ./internal/standards ./cmd/agent`
(the branch-scoped set for this diff) — see the task report for the raw run.

New regression gate: `cmd/agent/cli_replay_prompt_refusal_test.go` — four live-binary
tests (row well-formedness + every printed route runs; the operand shape refused with
three no-run instruments; `-h` legal and unadvertised; the negative control), which is
the epic's deliverable (2).

## Not done (honest)

- **The census and `PositionalVerbs` are deliberately untouched.** `replay` stays out of
  `devdoc/gopi-verb-census.md` (it is not a verb) and out of `config.PositionalVerbs`
  (that list's contract forbids a word with no command). The sibling abolition gate
  asserts both absences, and it is green.
- **The player's own history is not re-litigated.** `456c459b4` was authored on HP
  against a realigned-away base; its `devdoc/session-replay-subverb.md` arrived with it
  and is classified in `devdoc/DOC-INDEX.tsv` by the same commit. What is NOT verified
  here is the player's behaviour beyond its `-h` page and its own unit tests — driving
  it over a real terminal is its own acceptance, and this change is about the word that
  must not be spent as a prompt.
- **No second refusal spellings were added — and the flag-first shapes are refused by a
  DIFFERENT surface.** Measured on the built binary: `gopi replay --terminal 5 <uuid>`
  and `gopi replay --session <uuid>` both exit 1 with `unknown flag --terminal` /
  `unknown flag --session` plus the top-level usage — the flag parser refuses them, not
  this row (the row's own message is not printed). The outcome is the one that matters
  (exit 1, no run), but it is a different mechanism and this change does not claim it.
