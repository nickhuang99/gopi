<!-- doc-class: normative -->
# Task Report: sync tag-token CASE POLICY — SQL gates agree with the Go token rule

Branch `fix/sync-tag-case-policy`, worktree `~/workspace/gopi-synctagcase`, base `98876783`
(master). Deliverable: the four ledger-sync gates no longer fold ASCII case, because no Go
consumer of those tokens ever did.

## Completed

- **Decided the policy from evidence** (not assumption): tag tokens are **CASE-SENSITIVE
  persisted values** — the Go-reader census below found no case folding anywhere on the tag
  token axis, and the ONLY mixed-case tokens in the live ledger are real *node names*
  (`from-hostb-Laptop`, 135/1044 rows) whose case is provenance, not spelling.
- **ONE shared SQL fragment** as the single definition of "row carries tag token X"
  (`syncTagTokenMatchSQL` + `syncTagTokenPattern`, `internal/tools/memory_sync_delta.go`,
  Rule 14), spliced by all four gate sites:
  - `memory_sync_task.go:347` — v1 export tag-partition filter (was `:315`)
  - `memory_sync_task.go:370` — v1 anti-reflow exclusion (was `:338`)
  - `memory_sync_delta.go:208` — v2 delta tag-partition filter (was `:207`)
  - `memory_sync_delta.go:231` — v2 anti-reflow exclusion (was `:230`)
  - new named constants `syncTagColumn` (memory_sync_task.go) — no bare `tags` literals left
    in the fragment call sites (Rule 1b/13).
- **Parity + red-proof test** `internal/tools/memory_sync_tagcase_test.go`:
  `TestSyncTagTokenMatch_SQLGoParity` (shipped SQL gate vs Go `hasTag`/`splitSources`, both
  channels, both directions, mixed-case fixture), `TestSyncTagTokenMatch_OldLIKEDiverges`
  (the pre-fix query text run side by side — the divergence is printed), `TestSyncTagTokenFragmentShape`
  (fragment mechanics: GLOB, not LIKE; both comma anchors), `TestSyncTagTokenMatch_LiveLedgerCopyParity`
  (measures the live ledger read-only, 0 divergence asserted).
- **The GLOB's boundary mode is DECIDED, not accidental** — comma anchor only, no space-strip
  (reasoning below).

## The decided policy (what a future agent must not re-litigate)

```
(',' || COALESCE(tags,'') || ',') GLOB ?          -- fragment, declared ONCE
pattern = "*," + token + ",*"                     -- callers
```

1. **Case-sensitive (GLOB, never LIKE).** SQLite's `LIKE` folds ASCII case; the Go readers do
   not. Under `LIKE` a row tagged `BUG` rode the `bug` partition while being invisible to
   every reader; the mirror direction is worse — a wrong-case `from-<peer>` tag made the
   anti-reflow gate exclude a row whose channel origin no reader recognizes (silent drop:
   the 27-row class re-opened from the other side).
2. **Comma anchor = whole-token match** (`bug` never hits `debug-ui` / `bug033` / `bugflow`),
   matching `hasTag`'s exact compare.
3. **No space-strip.** Raw tags carry no surrounding whitespace (import writes
   `normalizeTaskTags` output; the live copy measured **0/1044** spaced rows), and
   `splitSources`' per-token `TrimSpace` means `' bug '` is the *distinct* token ` bug ` — a
   space-stripping SQL side would match what the Go rule does not. Adding the strip "to be
   safe" would create the disagreement this task exists to remove. (A previous sibling fix in
   `fix/bug-tag-population-token-match` did strip spaces because it needed a ` bug`↔`bug`
   tolerance; that tolerance is not wanted here.)
4. **GLOB metacharacters are not escaped** — `*`, `?`, `[...]` in a token are treated as
   wildcards. Exact for this population: the tokens are hostname/path-ish values
   (`splitSources` hashes them raw into `from-<node>`, so the reader already cannot tell a
   folded match from the raw value), the live copy has **0** metacharacter tags, and the
   fixture deliberately contains no GLOB-bait row (the one shape where the asymmetry could
   show) — so the parity test stays a true statement about real data.

## Go-reader census (the justification, not an assumption)

Every production consumer of these tokens compares them **exactly, case-sensitively**:

| Reader | Site | Comparison |
| --- | --- | --- |
| `hasTag` (worker tag, conflict tag, channel tag) | `memory_sync_delta.go:260` | `t == want` exact |
| `syncDeltaMergeTags` (strips `from-*` on merge) | `memory_sync_delta.go:247` | `strings.HasPrefix(t, SyncTaskTagPrefix)` |
| `syncSelfTag` / `syncGateTag` (gate token builder) | `memory_sync_delta.go:103`, `:128` | string concat; the writer's spelling (`:556`, `:1622`) is byte-identical |
| tag-priority / bug predicates | `tasks.go` (master face) | exact token compare |
| `splitSources` | `memory.go:3664` | comma split + `TrimSpace`, **no** case folding |

Repo-wide census of the folding primitives (`strings.EqualFold` / `strings.ToLower` /
`lower()` in SQL) finds them in bash-command filtering, memory *retrieval scoring*, branch
names, env values and provenance SQL — **none of them a tag token**. The single case-folding
comparison in the sync path (`IsServerNode`, `memory_sync_delta.go:787`) compares *peer
names* as a set-membership question, a different axis. `LIKE` was the only case-folding
participant in this class, and it was on the SQL side alone.

## Verification — before/after divergence (measured on a COPY of the live ledger)

`cp ~/.gopi/memory.db .tmp-ledger-copy.db` (the live file is never written; the copy was read
with `mode=ro`), 2026-09-12, **1044 rows / 417 distinct comma-split tokens / 135 rows bearing
uppercase tokens** (the ledger is live — the in-test probe later measured 1045 rows, i.e. the
ledger grew by one row between the two readings; the per-token counts below are the sweep's
snapshot):

| Probe | old `LIKE` | new `GLOB` | divergence |
| --- | --- | --- | --- |
| partition gate, token `bug` (real spelling) | 197 | 197 | **0** |
| peer gate, token `from-peer1` (real node) | 37 | 37 | **0** |
| peer gate, token `from-hp` | 54 | 54 | **0** |
| peer gate, token `from-hostb-Laptop` (mixed case) | 134 | 134 | **0** |
| peer gate, token `from-workerpar-test` | 18 | 18 | **0** |
| peer gate, **uppercase spelling** `from-PEERBOX` | 37 | 0 | **37** |
| source-token filter `node:HP` (adjacent site, see below) | 1 | 0 | **1** |

**Honest reading: the defect is LATENT on today's ledger — 0 divergence for every real
token**, because every peer's node name today resolves to the spelling the Go readers use.
The upper-spelling probe is the landmine the fix removes: had a peer announced itself as
`PEERBOX`, 37 pending rows would have been classified "channel-born" by the gate and **silently
swallowed** by the very exclusion the epic describes — no error, no log. The ledger's 135
mixed-case rows (`from-hostb-Laptop` family) are the proof that node-derived tokens really
do carry case in this population; today they happen to be spelled the same on both sides.

## Red-proof (pre-fix behaviour restored → the new test fails)

Reverting ONLY the shared fragment to the old text, then running the new tests:

- `TestSyncTagTokenMatch_SQLGoParity` — **FAIL**, all 9 partition tokens and 4 peer probes
  diverge, e.g. `tag "BUG": want [t-bug-upper], got []` (the uppercase row vanishes from the
  gate) and `peer "hostb": … unexpected [t-from-host-lower]` (the wrong-case channel
  row reflows).
- `TestSyncTagTokenMatch_OldLIKEDiverges` — **FAIL** as designed, with the divergence printed:
  `RED-PROOF ① token "BUG": old LIKE=4 rows [t-bug-bounded t-bug-exact t-bug-upper
  t-from-host-mixed-bug] | Go rule=1 rows [t-bug-upper]` (over-inclusion of 3) and
  `RED-PROOF ② peer "peer1-X": old LIKE ships 11 | Go rule ships 12 (swallowed-by-old:
  [t-from-peer-upper])`.
- `TestSyncTagTokenFragmentShape` — **FAIL** (`must be a GLOB`).
- Fragment restored → all four tests PASS.

## Adjacent site found, measured, deliberately NOT changed

`internal/tools/memory_sync.go` (distilled_qa channel) carries the same SQL shape twice: the
`node:%` anti-self exclusion and the optional source-token partition filter — the identical
"each node syncs its partition" design, driven by the same PA tagging convention. Measured on
the same live copy: the `node:` exclusion diverges **0** (1910 vs 1910), the uppercase
`node:HP` filter probe diverges **1 vs 0**. It is **out of this task's scope** (the epic named
the four `tasks` gates; the `sources` column has its own reader census — `splitSources` +
the `node:`-prefix readers in `memory_sync*.go`/`memory_provenance.go`, where `lower()` is
used deliberately in some SQL) and editing it here would be an unverified direction change.
Recorded so the next agent starts from the measurement instead of re-discovering it.

## Not Done

- The `distilled_qa.sources` twin (above) — measured, deferred, not edited.
- `taskBugPopulationWhere` / the bug-flow population semantics — **untouched by design**
  (hard scope boundary: that branch is unmerged; this change does not couple to it).
- No backfill of wrong-case tokens: the case policy is read-side only. `from-hostb-Laptop`
  stays exactly as persisted (rewriting provenance spelling would be data mutation).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` — OK
- gofmt: `gofmt -l internal/tools/` — clean (empty output)
- Tests: `go test ./internal/tools/ -run TestSyncTagTokenMatch|TestSyncTagTokenFragment -v` — 4/4 PASS;
  `TestSyncTagTokenMatch_LiveLedgerCopyParity` logs `live-ledger copy /home/user/.gopi/memory.db:
  1045 rows, LIKE-vs-GLOB divergence for "bug" = 0 row(s) []`
- Commit: `f41e8b8c` on `fix/sync-tag-case-policy` (worktree `~/workspace/gopi-synctagcase`).

## Full-suite gate

- `go test ./internal/standards/ -count=1` — **ok 16.99s** (machine gates 16–21 green).
- `CGO_ENABLED=0 go test ./internal/tools/ -count=1` — **ok 552.6s** (whole package, no
  regression from the fragment swap).
- Red-proof (re-run as the final gate, on the committed tree): mutating ONLY the two shared
  helpers back to the pre-fix text (`LIKE ?` + `%,token,%`) → `TestSyncTagTokenMatch_SQLGoParity`
  FAIL (9 partition tokens × 2 channels + 4 peer probes diverge, e.g. `tag "BUG"` want
  `[t-bug-upper]` got the 4-row LIKE set; peer `hostb` unexpectedly ships
  `t-from-host-mixed`), `TestSyncTagTokenMatch_OldLIKEDiverges` FAIL with the divergence
  printed (`old LIKE=4 | Go rule=1`; `old LIKE ships 11 | Go rule ships 12`,
  swallowed-by-old `[t-from-peer-upper]`), `TestSyncTagTokenFragmentShape` FAIL
  (`must be a GLOB`). Restoring the fragment → **4/4 PASS** (`ok … 0.962s`), and
  `git diff --stat` confirms the restore is byte-identical to the commit.
- Live ledger untouched: the probe opens `mode=ro&query_only(true)`; the live
  `~/.gopi/memory.db` was never written (the per-token sweep in "before/after divergence"
  ran against a `cp`'d copy).
- Scope: `git diff --name-only` lists exactly two production files
  (`internal/tools/memory_sync_delta.go`, `internal/tools/memory_sync_task.go`) —
  `internal/tools/tasks.go` (bug-flow population) is NOT touched.
