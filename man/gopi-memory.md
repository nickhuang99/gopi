---
title: GOPI-MEMORY
section: 1
header: GoPi Manual
---

# NAME

gopi-memory - the memory tool family as one subcommand group

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
  gopi memory <subcommand> [args]
```

<!-- END SYNOPSIS -->

# DESCRIPTION

**gopi memory** puts the knowledge base behind the agent's own `memory` tool on
the shell, so a person can read and maintain it without running an agent. The
rows it touches are the same rows the tool writes: a lesson stored in a chat is
visible here, and a record demoted here steers the next chat's retrieval.

Three families live under the one noun, and the distinction is the reason they
are documented together:

- **the row verbs** — `search`, `store`, `get`, `list`, `update`, `delete`,
  `feedback` — read and write ONE record. This is the maintenance loop the help
  page names: `list` to enumerate ids, `get` to inspect one, then `update` or
  `delete`.
- **the transfer verb** — `sync` — moves rows between this node and one declared
  peer over one ssh leg. It is **MANUAL BY DESIGN**: one invocation runs one
  unique-only session and exits. Nothing schedules it — no timer, no daemon, no
  loop, and no `memory store` triggers it — so a session happens when a person
  asks for one. `memsync`, the patch-FILE sibling that produced a reviewable JSON
  artifact, is **deleted** and typing it still reaches a refusal: use `sync` for a
  converging session, or `backup` for a one-way ledger copy (`backup` imports
  nothing back, so it REPLACES the peer's knowledge base rather than merging).
- **the producing and governance verbs** — `brew`, `backfill-tokens`,
  `backup`, `provenance`, `capture-stats` — distil new rows from session logs,
  re-derive a derived column, snapshot the ledger, and measure where rows came
  from.

`gopi memory backup` **documents** the restore procedure and never performs it:
the command snapshots the live ledger with SQLite's own `VACUUM INTO` (never a
file copy) and proves the artifact openable and consistent read-only.

A `--db PATH` that does not exist is **not** an empty knowledge base. The
read-only verbs (`search`, `get`, `list`, the reporting modes of `provenance`,
`feedback --list` / `--suggest-cold` / `--recompute`, and the `--dry-run` modes
of `converge-keywords` and `backfill-tokens`) refuse an explicitly named path
that is absent: they exit non-zero, name both the path they were given and the
ledger that is active, and create no file — a read never leaves a 128 KiB
database behind. The writing verbs (`store`, `update`, `delete`, a
`feedback <id>` vote, `provenance --backfill`, and `converge-keywords` /
`backfill-tokens` without `--dry-run`) keep the ability to create a ledger, and
print `created a new knowledge base at <path>` when they do, so "empty" and
"newly created" are never the same sentence. `capture-stats` opens no ledger at
all, and refuses `--db` by name.

Search results print `tokens=N`, which is how many query terms the hit covers.
That number is the signal for spotting a low-quality row, and the tier value
`cold` exists for exactly that purpose: a demotion rather than a deletion.

**The shape of a search result, field by field.** The result's FIRST line is
`now=<RFC3339>`, the instant every `age=` below it was measured against. Each
hit then carries `stored=<when the row was WRITTEN> age=<N>d` and, when the
row's own text states a date, `about=<YYYY-MM-DD>`. `stored=` and `about=` are
**two different facts and neither is derived from the other**: a row about a
2026-08-01 incident written today prints `stored=` today and `about=2026-08-01`,
and `about=` is OMITTED (not defaulted to `stored=`) when the text states no
date, or states a FUTURE one — a future date is dropped rather than rendered.
Rows written before the `distilled_at` column existed fall back to the older
`when_at` value for `stored=`.

`converge-keywords` prints the tail-block histogram before and after it writes,
so a ledger that needs no work reports `{0,1}` on both lines — measure first,
and read `{0,1}` as "nothing to collapse".

`provenance --harvest-latent` harvests a row whose TOPIC declares a file-shaped
path that the row's own question/answer repeats — measured: **100%** of such
declared paths resolve, versus **63%** of the paths a row merely records in its
`sources` text. That gap is the rule's justification, and `--latent-calibrate`
re-derives both rates from the current ledger.

The harvesting CRITERION, stated because the page cannot carry it in one line:
the row's `sources` column must be **EMPTY**, its TOPIC must declare a
file-shaped path, and its own question/answer must REPEAT that path — a mere
MENTION of a path is not harvested. The write is **additive only**: the columns
`source_type_latent` / `provenance` / `provenance_evidence` are set, and
`sources` and `source_type` are never modified by this arm.

The legacy flat verbs (`gopi memory_search`, `memory_store`, …) still work but
print a deprecation note pointing here; the grouped form is the canonical
surface.

# SEE ALSO

`gopi memory <sub> -h` (every sub-verb answers its own page), `gopi -h`,
`man gopi`, and `man gopi-task` — the task ledger lives in the SAME `memory.db`,
so a backup or a sync of one covers the other.
