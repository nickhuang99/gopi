<!-- doc-class: historical -->
# CLI Code Review: `gopi brew` + `gopi memsync` subcommands

> Review branch `review/cli-brew-memsync` (worktree `~/workspace/gopi-review-cli-brew-memsync`).
> Reviewed files: `cmd/agent/cli_brew.go`, `cmd/agent/cli_memsync.go`,
> `internal/tools/memory_brew.go`, `memory_brew_run.go`, `memory_brew_llm.go`,
> `memory_sync.go`, `memory_sync_procedural.go`, `memory_sync_task.go`, `memory.go`,
> `memory_procedural.go` + devdoc reports (memroadmap-m3/m4, memory-procedural-impl,
> memsync-task-extension, memsync-bidir-m49, cli-command-audit).
> Review date: 2026-09-02. Scope: code review only — **no code changes made**.

## Verdict summary

The two subcommands are well-engineered at the engine layer — Rule 1/2 named
constants throughout, single-transaction imports, provenance forced at import,
versioned patch formats with a compatibility gate, anti-reflow export gates,
shared dedup cores between the store path and the import path. The weakest
surfaces are (a) the **brew session-exit trigger, which is effectively dead**
(P0 — goroutine killed at process exit, empirically confirmed), (b) three
places where **documentation/usage text promises idempotence or content
propagation the code does not deliver** (P1), and (c) CLI hygiene gaps
(unknown-flag silence, `--json` contamination) (P1/P2).

---

## Per-verb findings

### `gopi brew` (CLI + session-exit trigger)

Design purpose: sweep `~/.gopi/sessions/*.jsonl` (newest first, max 64), slice
each transcript into bounded chunks (12K chars / 40 events), LLM-judge each
chunk against the three lesson conditions (可重复模式 / 可复现闭环 / 收益达标),
distill accepted lessons into QA rows (kind=workflow → procedural rows), store
tagged `sources=brew`. `--dry-run` judges/distills but stores nothing.

**Findings:**

1. **[P0] The session-exit trigger never completes — background goroutine is
   killed at process exit.** `maybeBrewOnSessionExit()` (cli_brew.go:119) is
   the **last statement of `main()`** (main.go:158). After it returns, `main`
   returns and the Go runtime exits, killing the `go func(){...}()` sweep
   (cli_brew.go:144-157) — whose first real work (NewBrewEngine → config load
   → HTTP LLM call, seconds to minutes) cannot finish in the microseconds
   between `go` and process teardown. There is no WaitGroup/channel/block
   anywhere after the call.
   **Empirical evidence (host DB):** `brew_state.json` mtime = **Sep 2 12:28**
   (trigger fired and touched the state file), yet the newest
   `sources=brew` row in `~/.gopi/memory.db` is **Aug 29 19:53**, and there
   were dozens of one-shot sessions between Aug 29 and Sep 2 (session files
   modified through Sep 2 13:15). The trigger scheduled, the goroutine died,
   nothing was ever distilled by it.
   **Compound harm:** the state file is touched *before* the sweep
   (deliberately — crash-retry storm prevention), so the 24h throttle now
   suppresses retries for a sweep that never ran. Result: **the trigger can
   never recover on its own** — the brew rows that exist (2, both Aug 29) came
   from *manual* `gopi brew --session …` acceptance runs (memroadmap-m3 §验收),
   not the trigger. The feature "runs at most once per 24h at session exit"
   is, in practice, "writes a state file every 24h and does nothing".
   *Also:* the doc comment says "Called by the agent at chat exit", but the
   only call site is main.go's *one-shot* path — interactive `gopi chat`
   sessions never trigger it (memroadmap-m3 "Not Done" admits the chat exit
   hook was never wired). The usage text "at gopi session exit" overstates
   coverage.
   *Fix direction:* run the sweep synchronously (accept the latency), or
   detach via `os/exec` self-invocation (`gopi brew` as a child process), or
   move the trigger into a place that owns process lifetime (worker loop).

2. **[P1] `--json` output is contaminated.** `runBrewSubcommand` prints the
   Progress callback (`fmt.Printf`, cli_brew.go:81-83) and the human summary
   `printBrewStats` (cli_brew.go:99) to **stdout after** (and interleaved
   with) the JSON block. Empirically verified with a real sweep:
   ```
     brew: c15ff8d8….jsonl → 4 lesson(s)      ← not JSON
     brew: 8e990f10….jsonl → 3 lesson(s)      ← not JSON
   { "sessions_scanned": 2, ... }              ← the JSON
   brew done in 38.7s: ...                     ← not JSON
   ```
   A script consuming `brew --json` receives invalid JSON. (The human
   "brew done" line after the JSON block is the worst offender.) The engine
   itself is honest (`BrewStats` marshals cleanly); the CLI layer pollutes it.
   *Fix direction:* under `--json`, route Progress + summary to stderr (or
   suppress them); print only the JSON object to stdout.

3. **[P2] Unknown flags are silently accepted.** `runBrewSubcommand` uses
   `newMemoryFlagSet().parse()` but never applies the per-subcommand `memberOf`
   whitelist that `parseMemoryArgs` (cli_memory.go:184) uses for every memory
   verb. Verified: `gopi brew --dry-run --bogus-flag` runs normally, exit 0.
   A typo'd flag silently runs the *default* behavior — for brew that default
   is a **real LLM sweep of the live sessions dir** (expensive, and it writes
   rows). Same gap on `memsync` (finding 8).

4. **[P2] Exit code 0 on a fully-failed run.** `runBrewSubcommand` exits 2
   only when `errors>0 && stored+deduped>0` (cli_brew.go:100-101). A run where
   every chunk failed (e.g. LLM endpoint down → all judge calls error) prints
   `errors=N` but **exits 0** — a silent-failure footgun for cron/CI/`--json`
   consumers. Batch resilience (one bad chunk shouldn't fail the run) is
   right; total failure should not look like success.

5. **[P2] `--limit` silently ignored when `--session` is given.** With
   explicit session paths, `opts.Limit` is never consulted (listSessionJSONLs
   is only reached on the sweep path). Usage text ("caps the number of
   sessions") over-promises for the `--session` combination; a clear error or
   a doc note would be honest.

6. **[P2] `GOPI_BREW_INTERVAL_SEC` missing from `--help-env`.** The env-var
   catalog (internal/config EnvVars/settings.go) documents `GOPI_MEMORY_DB`
   but not the brew interval knob that `brew -h` itself advertises — the
   catalog-driven env table (the config layer's mature pattern) does not cover
   it. Also: malformed interval values are silently treated as *disabled*
   (cli_brew.go:123-125) — the sibling review (#1235) flagged the same
   silent-disable pattern as drift against readers that error loudly.

7. **[P2] brew state file JSON is decorative.** Only `os.Stat` mtime is ever
   read (cli_brew.go:135); the `{"last_run": …}` payload is written and never
   parsed. Not a bug (mtime is the actual state) — but the file pretends to be
   JSON state while corruption of it is harmless *and* unnoticed. A comment or
   a plain `.timestamp` file would be more honest.

### `gopi memsync --export / --import` (brew QA channel)

Design purpose: export brew-sourced QA rows (`sources` contains `brew`, and
never a `node:` tag) as a versioned JSON patch carrying **no ids**; import
re-applies through `memoryStoreRow` — the *same* exact+similarity dedup core
MemoryStore/brew use — on **one BEGIN IMMEDIATE transaction**
(all-or-nothing), forcing `node:<exporting-node>` provenance at import.

**Findings:**

8. **[P1] Unknown/conflicting mode flags are silently accepted** (shared with
   brew, finding 3): no `memberOf` whitelist, and the mode switch
   (cli_memsync.go:106-121) silently picks one when several of
   `--export/--import/--export-proc/--import-proc/--export-tasks/
   --import-tasks/--dedup-scan` are given together. Verified:
   `memsync --export --out x.json --import-proc y.json` quietly ran the
   import-proc branch (switch order) and failed on the missing file. A
   user passing both export and import gets whichever case sorts first, not
   an error. The `parseMemoryArgs` `memberOf` gate is the established in-repo
   pattern; brew/memsync should use it (or an equivalent allowed-set check +
   a mutually-exclusive-mode check).

9. **[P2] Idempotence holds for row counts but the "human wins" guard has a
   long-question blind spot.** `syncQuestionOwnedQ` probes with
   `truncateRunes(question, 200)` compared against the **full** stored
   `question` column (`WHERE question=?`, memory_sync.go:391), while
   `memoryStoreRow`'s own dedup compares truncated-vs-truncated via
   `substr(question,1,?)` AND scopes on `topic` (memory.go:2038-2043). For a
   stored question longer than 200 runes with a matching topic, the ownership
   probe misses → `memoryStoreRow` then dedups onto the human row and **merges
   `node:<exporter>` into a human-owned row's sources** — the "human always
   wins" refusal is bypassed and the row silently becomes sync-managed (which
   then flips its export gate). The m4 acceptance note (memory.go:2035-2037)
   explicitly fixed this exact truncated-vs-full comparison on the store side;
   the guard two layers up (`syncQuestionOwnedQ`) and the brew-side guard
   (`brewGuardBlocked`, memory_brew_run.go:266-273 — same `question=?` full
   comparison) were not updated to match. **Dedup-key drift between the guard
   and the store core** — the exact class this epic asked to look for.

### `gopi memsync --export-proc / --import-proc` (procedural channel)

Design purpose: export procedural rows on the `procedural` tag (not `brew`),
import with name-exact + name+trigger-similarity dedup, provenance forced at
import, version/updated_at max semantics.

**Findings:**

10. **[P1] Cross-node workflow content never propagates — the merge discards
    the patch's steps.** `proceduralStoreRow` marshals the incoming steps and
    passes `stepsJSON` into `proceduralMergeRow` — which **never writes it**;
    the merge only does `SET version=?, updated_at=?, sources=?` (memory_
    sync_procedural.go:284-286). `stepsJSON` is a dead parameter. Consequence:
    when node B imports a workflow that node A **evolved** (new steps,
    version bumped at A), B dedups onto its older row, bumps *its* version,
    and keeps the **stale steps**. The procedural sync channel can therefore
    never converge workflow *content* across nodes — it is a version ticker.
    merge-not-overwrite is the right policy for *idempotent re-runs of the
    same content* (brew re-distillation), but the sync channel exists
    precisely to move *evolution*; the same-name/same-trigger case should
    carry the newer body forward (or refuse loudly when versions diverge
    without content). As written, `--export-proc/--import-proc` cannot deliver
    an improved workflow to a peer — a genuine gap between the channel's
    purpose and its merge.

11. **[P1] Version semantics: usage/comment say "max", code does
    "max+1", and re-import inflates forever.** Usage text: "version/updated_at
    take max(local, patch)" and "Re-import is a no-op (idempotent)". Code
    (memory_sync_procedural.go:279): `maxInt(version, r.Version) + 1`.
    Empirically verified: importing a version-1 patch into a fresh DB lands
    version=2; **re-importing the same patch bumps it to 3** (still reported
    "deduped"). The devdoc (memory-procedural-impl.md §6) documents "+1" as a
    deliberate concurrent-evolution guard, and `TestProceduralSyncRoundtrip`
    only asserts `RowsDeduped==1` (never version stability) — so the *code*
    is internally consistent with its devdoc, but the **CLI usage text and the
    code comment ("take max(local, patch)") are wrong**: re-import is not a
    no-op on the version column, and an older patch imported over a newer
    local row inflates the local version. Three sources disagree with each
    other. Fix: align usage/comment to "max+1" (and state that re-import
    bumps version), or make re-import of an identical patch a true no-op.

### `gopi memsync --export-tasks / --import-tasks / --dedup-scan` (task channel)

Design purpose: task-ledger sync with the five mechanics (payload-hash dedup,
task_id preservation, forward-only migration, forced from-<node> tag,
no-overwrite), plus the M4.8 dedup-scan audit (exact/canonical/similar twin
pairs, report-only, exit 2 on twins).

**Findings:**

12. **[P2] CLI help text only covers the QA/procedural channels in its first
    block but does document export-tasks well.** Minor doc inconsistency: the
    memsyncUsage doc comment (cli_memsync.go:3-7) says "Two channels: brew
    rows and task rows" — omitting the procedural channel that was added
    later (the actual usage text does include it). Cosmetic, but the header
    comment is the first thing a reader sees.

13. **[P2] `memsyncStatsJSON` is dead code.** Defined at cli_memsync.go:294,
    never referenced anywhere (verified by repo-wide search). The comment
    claims "kept for scripted consumers" — no consumer exists. Remove or wire.

14. **[Positive] Task channel mechanics are the most hardened surface here**:
    the in-patch + live dedup ladder (exact sha256 → canonical hash with
    path/timestamp/whitespace normalization → trigram-Jaccard at a CJK-tuned
    0.35 threshold) is shared between the live probe and the audit scan via
    `syncTaskDedupDecide` — no drift between import-gate and audit (the QA
    channel's guard-vs-store drift, finding 9, does not exist here). All
    thresholds/limits are named constants (Rule 1). Single-transaction import
    with busy_timeout 15s is documented and enforced.

### Cross-cutting

15. **[Positive] Rule 1/2 compliance is strong.** Every threshold — brew
    chunk caps (12000 chars / 40 events / 2500 prompt reserve), lesson caps,
    similarity thresholds (`trigramJaccardMerge=0.5`, `syncTaskSimilarity
    Threshold=0.35`, `proceduralNameJaccardMerge=0.3` — all commented with
    their tuning rationale), scan caps (2000), timeouts (180s), interval
    default (24h) — is a named constant in exactly one place. The three
    similarity thresholds *look* like they might drift (0.5 vs 0.35 vs 0.3)
    but each is deliberately tuned and documented for its domain (QA rows vs
    CJK task payloads vs workflow names) — this is defensible, not drift.

16. **[Positive] Idempotence and provenance discipline on the import paths**
    are genuinely well done: whole-patch single-transaction (QA + procedural +
    task), forced node provenance decided at import (never trusted from the
    file), versioned patch formats with a bidirectional compatibility gate
    (newer-format patches refuse loudly instead of silently dropping fields),
    anti-reflow export gates (node-tagged rows never re-export → no ping-pong),
    dry-run honesty (verified: `brew --dry-run` writes zero rows).

17. **[P2] Exit-code conventions are inconsistent across verbs.** brew: 0/1/2
    (2 = partial with progress). memsync import-proc: 2 when rows refused;
    brew import: 2 when refused; dedup-scan: 2 when twins found. Each is
    documented in its own usage, but there is no shared contract — a script
    driving several verbs must learn each meaning. Low risk today (no common
    consumer), worth a one-line convention note.

---

## Prioritized list

### P0
1. **brew session-exit trigger is dead**: fire-and-forget goroutine as the
   last statement of `main()` is killed at process exit; state file is
   touched first so the 24h throttle suppresses retries forever. Empirically:
   state touched Sep 2 12:28, zero brew rows since Aug 29 despite daily
   sessions. (cli_brew.go:119-158 + main.go:158) — run synchronously /
   self-exec detached / move trigger under process-lifetime ownership; add a
   regression test that exercises the trigger to completion.

### P1
2. **Procedural cross-node content never propagates** — merge discards the
   patch's steps (`stepsJSON` dead parameter); sync channel is a version
   ticker that cannot deliver evolved workflows. (memory_sync_procedural.go:
   242-290)
3. **Version semantics contradicted in three places** — usage/comment promise
   "max + no-op re-import"; code does max+1 and bumps on every re-import
   (empirically 1→2→3). Align docs or make re-import a true no-op.
   (memory_sync_procedural.go:279, cli_memsync.go:41-43)
4. **`brew --json` stdout is contaminated** with Progress lines and the human
   summary after the JSON object — scripts get invalid JSON. (cli_brew.go:
   81-83, 99)
5. **Unknown flags silently accepted on brew AND memsync** (no `memberOf`
   whitelist) — a typo'd flag runs the default behavior; on brew the default
   is a real, expensive LLM sweep. Conflicting mode flags on memsync also
   silently pick one by switch order. (cli_brew.go:56, cli_memsync.go:92,
   106-121)

### P2
6. **Dedup-key drift**: `syncQuestionOwnedQ` + `brewGuardBlocked` compare a
   200-rune-truncated probe against the full stored question (topic-agnostic),
   while `memoryStoreRow` dedups truncated-vs-truncated scoped on topic —
   long-question rows bypass the "human wins" refusal and get node/brew
   provenance merged onto human rows. (memory_sync.go:391, memory_brew_run.go:
   266-273, memory.go:2038-2043)
7. **brew exit 0 on a fully-failed run** (errors>0, nothing stored).
8. `--limit` silently ignored with `--session`; malformed `GOPI_BREW_INTERVAL_SEC`
   silently disables; interval knob absent from `--help-env`.
9. `memsyncStatsJSON` dead code; memsyncUsage header comment omits the
   procedural channel; brew state-file JSON payload never read (decorative).

---

## Files reviewed (branch review/cli-brew-memsync, no code changes)

- cmd/agent/cli_brew.go, cmd/agent/cli_memsync.go, cmd/agent/main.go:158
- internal/tools/memory_brew.go, memory_brew_run.go, memory_brew_llm.go,
  memory_brew_test.go (CLI), cli_brew_test.go, cli_memsync_test.go
- internal/tools/memory_sync.go, memory_sync_procedural.go, memory_sync_task.go
- internal/tools/memory.go (MemoryStore core, dedup constants), memory_procedural.go
- devdoc/: memroadmap-m3.md, memory-procedural-impl.md, memsync-task-extension.md,
  memsync-bidir-m49.md, cli-command-audit.md

## Verification notes (this review)

- `gopi brew -h`, `gopi memsync -h` read from the live binary.
- Real-sweep run exercised the pipeline (2 sessions, 12 chunks, 9 accepted,
  7 distilled, 2 judged+distill rejections) — engine batch resilience and
  dry-run honesty confirmed live.
- Procedural re-import version inflation reproduced in an isolated GOPI_HOME.
- Host DB forensics (memory.db brew rows vs brew_state.json mtime vs session
  file mtimes) back the P0 trigger finding.
