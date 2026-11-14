<!-- doc-class: historical -->
> **Retired mechanism (merge pass verify/withdraw deletion, 2026-11-01).** The
> post-pass verification and its withdrawal (`internal/tools/merge_pass_verify.go`:
> `CaptureMergePassScope` / `VerifyMergePass` / `WithdrawMergePass`, plus the
> `merge-withdrawn` notify alarm) were DELETED on 2026-11-01 with the last slice
> of the retired top-level `gopi merge` verb — its last production caller,
> `cmd/agent/merge_run.go`, was already gone. **`gopi task merge` does not verify
> the merged tree and does not withdraw a landing.** Everything below that speaks
> of the post-pass in the present tense is the record of a capability that
> existed, measured on the day each page states; it is not a promise about the
> shipped engine. `man/gopi-merge.md` carries the corrected contract.
# Task Report: why `gopi merge --fast` costs minutes — the build-cache bound fired on normal work

Task `01a0d084-a98a-7179-b6a4-cc7d9438e1c9`. Operator report: a deterministic-only
merge (pure git fast-forwards) took >2 minutes in a repo where the round landed ONE
branch, and the log showed the merger dropping its own build cache
(`holds 399 MiB (bound 512 MiB)` … `holds 652 MiB (bound 512 MiB)` …
`exceeds its 512 MiB bound … dropping the cache body`).

## Root cause (answered, with the measurement behind each link)

**What the cache is for.** `mergeBuildGateGoCache()` points the merge-time gate's
`GOCACHE` at a **shared, disk-backed, per-repo** directory,
`<worktree root>/gopi-build-cache/<repo-key>/gocache`. It exists because a Go build
cache is a *cross-task reusable asset*: before it, the gate used a per-**process**
directory on the RAM-backed tmpfs scratch home, so every gate call rebuilt from cold
(68s) *and* filled `/dev/shm` (the 2026-09-15 incident). One shared location per repo
is what makes a warm cache possible at all.

**Why `--fast` is not slow by design.** `--fast` never compiles a branch: it stops
before the conflict measurement and before any LLM hand-over. What it *does* owe is
the pass's **one** post-pass verification — `go build ./...` then `go vet ./...` per
repo that landed something (`merge_pass_verify.go`, `VerifyMergePass`) — and, on red,
a withdrawal of the whole batch. One build+vet per pass, not one per branch, is
already the optimised shape; it is not the problem.

**The problem is that the verification ran COLD.** `mergeBuildGateBoundGoCache` runs
on **every** call to `mergeBuildGateGoCache` (7 call sites, incl. all three gates of
each pre-land decision and the post-pass verify). Its bound was 512 MiB, chosen
*because* it sat below the 726M–1.14G the old per-process cache reached — i.e. the
bound was deliberately placed **inside the workload's own steady-state range**.
Measured on this host, 2026-09-24:

| observation | value | source |
|---|---|---|
| gate cache body, key `1a6fd2a82928067e`, before another build | **434 MiB** | measured now |
| same, after `go build ./...` + `go vet ./...` into it | **461 MiB** | measured now |
| same, after `go build ./cmd/agent/` (operator's own cache) | 449 MiB / 419 MiB | operator's host |
| the body that tripped the bound | **652 MiB** | the operator's log |

So the bound sits a few percent above the *resting* size and inside the range the
workload reaches after a couple of builds — hence the intermittent drop. The cost of
one drop, direct A/B through the gate's own cache (`go build ./...` then `go vet ./...`
in this repo):

| | build | vet | total |
|---|---|---|---|
| warm | **10.8s** | **12.2s** | **~23s** |
| cold | **52.9s** | **19.0s** | **~72s** |

`~49s` per landed pass, spent re-compiling the standard library because a guard fired
on normal work. That is the operator's "more than 2 minutes" (the pass also runs the
per-branch gate builds and the pre-land vet, each paying the same cold-cache tax once
the body was dropped).

## Completed

- `internal/tools/merge_build_gate_gocache.go`
  - `mergeBuildGateGoCacheMaxBytes` **512 MiB → 4 GiB**, with the reasoning written
    into the constant: a bound's promise is "this is a *cache*, not an archive", not
    "the cache is as small as possible". 4 GiB is ~9x the observed per-key steady
    state (449/461 MiB) and ~15x the cache one full build+vet measures (273 MB), so it
    cannot fire on healthy work, while still refusing unbounded growth. The tree's real
    accumulation bound is **age** (`internal/scratch.BuildCacheStaleAge` = 24h), which
    collects whole keys — the size bound is the runaway guard, and it now reads like one.
  - **New root-litter check** `mergeBuildGateReportRootLitter` +
    `mergeBuildGateGoCacheRootLitter`: a shard-shaped directory *at the cache root* is
    reported. The bound walks `<root>/<repo-key>/gocache/<shard>/<file>`, so a shard at
    the root is structurally invisible to it — and one was measured on this host (256
    shards / 6902 files / 733 MiB). The gate **reports and never deletes**: it did not
    create that tree (the tree's existing age sweep owns collection).
- `internal/tools/merge_build_gate_gocache_test.go`
  - `TestMergeBuildGate_GoCacheBoundSitsAboveTheWorkloadPeak` — pins the bound against
    the **observed** body (`mbgcObservedCacheBodyBytes = 652 << 20`, the operator's own
    log line) with a 2x headroom factor, and pins the other direction too (the bound is
    still finite and a meaningful ceiling). **RED on the old 512 MiB value** — output in
    `## Evidence`.
- **Two PRE-EXISTING reds repaired** (they blocked `./internal/standards/` and were
  NOT introduced by this task — verified against HEAD's own `DOC-INDEX.tsv`, which had
  neither row):
  - `devdoc/DOC-INDEX.tsv`: rows for this file (`normative`, `sym` spot-check on
    `mergeBuildGateGoCacheMaxBytes`) and for `devdoc/docanchor-corrections.md`, which
    **master carried with no row at all** (landed by `fa75f7bf`; the docclass gate was
    red on master before this task started).
  - `devdoc/docanchor-corrections.md`: the `<!-- doc-class: historical -->` banner its
    classification requires (a classed-as-historical markdown doc without the banner
    fails `TestDocClassBannerMatchesManifest`).
  - `TestDocAnchorsResolveInNormativeDocs` is **drift-neutral**: measured at HEAD with
    these two files' deltas reverted, it printed the identical
    `249 pre-existing drift entries permitted, 10 new, 7 stale permits` — only the
    verified count moves (2525 → 2526). The gate's red at this commit is HEAD's own.
- `internal/tools/merge_build_gate_gocache_test.go`
  - `TestMergeBuildGate_GoCacheRootLitterIsReported` — a root holding only repo keys,
    bookkeeping files and an operator's directory is silent; seeded shards (`ab`, `0f`,
    `ff`) are reported; an UPPERCASE `ABC` is not (the predicate is the toolchain's
    lowercase two-hex shape). Drives the shipped detector, not a copy of its predicate.

## Not Done

- **The root-litter residue was NOT deleted, and I did not hunt its origin.** The
  operator's screenshot showed 733 MiB / 256 shards at the cache root on 2026-09-24;
  **by the time this task ran (08:31 today) that residue was already gone** — the root
  holds exactly the two repo-key directories (868 MB total). The `ls` before/after
  evidence is therefore "already swept before I started", not "swept by me". The
  briefing's acceptance item 4 (sweep 733 MiB, report bytes reclaimed) is honestly
  **not** delivered: there was nothing left to sweep, and I did not spend budget on
  archaeology for a tree that no longer exists (`git log -S` shows the path join has
  one correct introduction, so the current writer does not mint it).
- **No second `gopi merge --fast` on the real repo was possible.** 1a0d08a8 and
  1a0d0ccf are the other in-flight tasks and their branches are not landed, so
  `/home/user/work/GoPi` has no branch needing a decision ("no branch … needs a merge
  decision" — run, verbatim, in `## Verification`). Acceptance item 3's two consecutive
  real-repo runs are therefore replaced by: (a) the same command against a fixture repo
  where a landing DOES happen, and (b) the bound's verdict computed on the real repo's
  own cache at its current measured size.
- Not touched, deliberately: the ABSTAIN paths, the trim policy (drop-the-body is kept
  — it is correct and simple once the bound is above the working set), `settings.json`,
  the scratch sweep (it already walks this root and hands the root itself to
  `IsDeclaredTreeRoot`, so extending it was unnecessary).

## Verification

- Build: `CGO_ENABLED=0 go build -o /tmp/gopi-mbgc ./cmd/agent/` → **BUILD_OK**;
  `go build ./...` green; `go vet ./internal/tools/` green.
- Tests: `go test ./internal/tools/ -run 'TestMergeBuildGate_GoCache|TestBuildCacheKey' -count=1` → **ok 2.4s**;
  `go test ./internal/scratch/ -run 'BuildCache|DeclaredTree' -count=1` → **ok 3.8s**;
  `make test-budget PKGS='./internal/tools ./internal/scratch ./internal/standards'` → the §Tests line below.
- Ran: `cd /home/user/work/GoPi && GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings.json /tmp/gopi-mbgc merge --fast`
  with `SB=/home/user/workspace/gopi-sandbox/sandbox-mbgc-verify-30e92f20` (the sandbox from
  `gopi sandbox create`) and `settings.json` = the operator's single-repo table.
  Result: `no branch in /home/user/work/GoPi needs a merge decision` — the real repo has
  nothing landable, so this invocation exercises the flag, the lock and the repo walk but
  not the landing path.
- Ran (the landing path, fixture repo `/home/user/workspace/gopi-mbgc-fixture`, a real
  git repo with `feat/fixture-landing` fast-forwardable):
  `GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings-fixture.json /tmp/gopi-mbgc merge --fast`.
  Result: `feat/fixture-landing landed  fast-forwarded into master, ref pruned`,
  `BUILD gate shared build cache …/4a7dc6b1821ef7c0/gocache holds 30 MiB (bound 4.00 GiB)`,
  `VET gate PASS`, `MERGE pass verify PASS … post-pass verification PASSED (go build ./... + go vet ./... green)`,
  `run over — 1 branch decision(s)`.
- Ran (the NEW check, deliberately provoked): the same command with
  `GOPI_MERGE_BUILD_GATE_GOCACHE_ROOT=$SB/mbgc-root` where `mbgc-root/ab` and `mbgc-root/0f`
  are seeded shard directories. Result, verbatim from the run:
  `BUILD gate shared build cache ROOT …/mbgc-root holds 2 stray cache shard(s) (0f ab) — the gate writes its caches one level down, under its own per-repo key, so a shard at this level is a SECOND cache the 4.00 GiB bound cannot measure and no build reads; it is REPORTED, never deleted here`
  and afterwards `ls $SB/mbgc-root/` → `0f  4a7dc6b1821ef7c0  ab` (the gate's own key dir appeared;
  the seeded shards survived untouched).
- Result of the bound on real data: `find <gate cache> -maxdepth 2 -type f -mindepth 2 | awk` →
  body **434 MiB** at rest → 512 MiB bound would `keep` **today**; after `go build ./...` +
  `go vet ./...` into the same cache → **461 MiB**, still `keep` — which is precisely why the
  operator saw it fire *intermittently* rather than always (652 MiB on 2026-09-24).
- Re-run at the committed tip (final sweep): `go build ./...` → **BUILD OK**;
  `go test ./internal/tools/ -run 'GoCache|BuildCacheKey' -count=1` → **ok 2.496s**;
  `go test ./internal/standards/ -count=1` → **ok 26.720s** (was FAIL before the manifest
  rows + banner were added, for a file master itself had left unclassified).
- Commit: `35614d1d` fix(merge): lift the BUILD gate's cache bound above the workload peak.

## Evidence (self-filed)

- **RED→GREEN of the bound arm** (Rule 10.5). With `mergeBuildGateGoCacheMaxBytes = 512 << 20`
  and the new test in place:
  `--- FAIL: TestMergeBuildGate_GoCacheBoundSitsAboveTheWorkloadPeak` /
  `merge_build_gate_gocache_test.go:581: the build cache bound is 512 MiB, less than 2x the largest cache body this workload has been OBSERVED to produce (652 MiB — the 652 MiB line in the operator's merge log) …`
  → `FAIL github.com/nick/gopi/internal/tools 2.337s`. Restored to `4 << 30`:
  `ok github.com/nick/gopi/internal/tools 2.328s`.
- **Root-litter line observed in a real run** (not a unit test): the verbatim
  `holds 2 stray cache shard(s) (0f ab)` line quoted in `## Verification`, from
  `gopi merge --fast` against the fixture repo.
- **Root-litter residue already gone before this task**: `ls /home/user/workspace/gopi-build-cache/`
  → `034fa09aeba92685  1a6fd2a82928067e` (only), `du -sh` → `868M`, and
  `ls … | grep -E '^[0-9a-f]{2}$'` → empty. Recorded as evidence for `## Not Done`, not as a
  claim that I cleaned it.
- **No claim about the framework's worktree/branch state is made here** — Rule 30's
  `git status --porcelain` reading is the run's own to report.
