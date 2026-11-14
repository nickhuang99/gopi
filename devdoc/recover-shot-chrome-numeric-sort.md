<!-- doc-class: historical -->
# Task Report: recover the unlanded work of `archive/fix/shot-chrome-numeric-sort`

## Completed

- Re-ran the archive-tag audit instruments **live** and confirmed the UNLANDED flag:
  the tag is not an ancestor of master, master lacks 9 commits, and all three
  production symbols are absent from master (section *Instruments*).
- Read the tag read-only (`git show` / `git diff` only — no checkout, no commit,
  no merge) and triaged its payload per unit into **SALVAGE** vs **OBSOLETE**
  (section *Per-unit verdicts*).
- Re-derived the salvageable units on a **fresh branch off master**
  (`fix/shot-chrome-numeric-sort`), RED test first, then the fix.
- Build + branch-scoped tests green; commits recorded; worktree clean.

## Not Done

- **Unit C is not recovered** (deliberately): the 815-line `internal/shot/cov_hp_test.go`
  and its 337-line `devdoc/cov-shot-hp.md` were re-derived independently on master
  with more coverage — see *Per-unit verdicts*. Nothing from that unit is in this branch.
- The three `docs(shot): …deliverable declaration` commits in the tag are
  bookkeeping for a session that is over; not recovered (no code value).
- No merge: PA merges (Rule 9 / briefing).
- `devdoc/cov-shot-hp.md` was **not** touched — master's copy is a divergent
  descendant with 4 newer commits.

## Instruments (observed, live)

Instrument commands run against the shared repo (`/home/user/work/GoPi`), master
tip `07198611`:

**1. `git cherry -v master archive/fix/shot-chrome-numeric-sort`** — 9 lines, every
one prefixed `+` (`+` = no equivalent on master; `-` would mean an upstream
equivalent exists):

```
+ 7a8419452ccbca51067177a53a154188dc5f10c4 test(shot): cover Capture + waitDevToolsPort/waitSelector/reachable boundary and error paths
+ 28758c0fcd3a452fe584aec2dd1cc1118dbdce61 test(shot): add handler-driven fake CDP server, viewport/wait boundaries, real-chrome leg
+ 5145977256096960f639020717a1bf3444b51f26 docs(shot): coverage report for cov-shot-hp (source inventory, boundaries, bugs filed)
+ 569f7fdb5bbf495efe1d1418e7cfe755fb2ec0fa docs(shot): record third commit hash in the deliverable declaration
+ 7dedda6739a5037a70af8b331456c867e55c2af8 fix(shot): compare playwright cache versions numerically, not lexically
+ e7bb791987f75288aa12ec6ea977f4fddf5a0137 docs(shot): report + deliverable declaration for the numeric cache-version sort
+ 673fdc0dab398be3c668db8c25712b3e1cd4295c docs(shot): record the actual commit hashes in the deliverable declaration
+ 11e8312cfb674d5ae8f4247c3fc1e75f62a7acfd docs(shot): point the declaration at the branch tip instead of a moving hash
+ b66da3270d86945160163636fdd1a1efadca4f79 docs(shot): record the branch-tip acceptance re-run in the numeric-sort report
```

**2. `git rev-list --count master..archive/fix/shot-chrome-numeric-sort`** → `9`
and `git merge-base --is-ancestor archive/… master` → **exit=1** (NOT an ancestor
⇒ genuinely unlanded).

**3. Per-symbol content test** — `git grep -c -w <symbol> <ref> -- 'internal/shot/*.go'`,
counting the files that carry each symbol:

| symbol | master | tag b66da327 |
|---|---|---|
| `comparePlaywrightVersions` | 0 files | 2 files |
| `trimVersionZeroes` | 0 files | 1 file |
| `versionZeroDigit` | 0 files | 1 file |

**4. The defect is still LIVE on master** — not merely "symbol absent". Master
`internal/shot/chrome.go:90` still sorts the playwright cache with the raw string
compare:

```go
sort.Slice(hits, func(i, j int) bool {
    return playwrightVersion(hits[i]) > playwrightVersion(hits[j])
})
```

Live RED proof (fresh test, master code, before the fix):

```
--- FAIL: TestFindChromeCacheOrderingIsNumeric
    findChrome picked …/chromium-9/chrome-linux/chrome, want the newest
    …/chromium-1228/chrome-linux/chrome — the cache sort is comparing version
    strings again (chromium-9 beats chromium-1228; stale=…/chromium-9/…)
FAIL	github.com/nick/gopi/internal/shot	0.516s
```

## Per-unit verdicts

The tag's payload is five files (diff vs merge-base `b7b8000d`):
`internal/shot/chrome.go` (+59/-4), `internal/shot/cov_hp_test.go` (+815, new),
`internal/shot/shot_test.go` (+49), `devdoc/cov-shot-hp.md` (+337, new),
`devdoc/shot-chrome-numeric-sort.md` (+71, new).

| unit | content | verdict | evidence |
|---|---|---|---|
| **A** | `internal/shot/chrome.go` prod fix (`comparePlaywrightVersions` / `trimVersionZeroes` / `versionZeroDigit`, `sort.SliceStable`) | **SALVAGE** | Per-symbol content test: 0 files on master, 2/1/1 on the tag. Master still carries the buggy `sort.Slice` string compare (instrument 4) and the red test fails against it. Re-derived fresh → commit `92173106`. |
| **B** | `internal/shot/shot_test.go` +49: `TestPlaywrightVersionOrderRelation` (10 ordering rows + antisymmetry) | **SALVAGE** | Same defect; master's `shot_test.go` has only `TestPlaywrightVersion` (extraction) and `TestFindChromeEnvOverride` — no ordering contract test. Re-derived fresh (renamed `TestComparePlaywrightVersions`, helper `compareSign` to avoid a redeclaration) plus the end-to-end `TestFindChromeCacheOrderingIsNumeric` → commit `47d6ec08`. |
| **C** | `internal/shot/cov_hp_test.go` (+815) + `devdoc/cov-shot-hp.md` (+337): broad Capture / `waitDevToolsPort` / `waitSelector` / CDP-stub coverage report | **OBSOLETE** | Superseded on master by `baf3ff95` "test(shot): cover Capture end-to-end via injected chrome stub + fake CDP" (the same scenario set, re-derived), then `4b6ba19b` "fix(shot): findChrome rejects a non-executable GOPI_SHOT_CHROME override at discovery" and `1f2e95ed` "docs(cov-shot): scope out the ENOEXEC-at-start residual case (P2…)". Overlap measured: of the file's 28 test funcs, 27 are tag-only *by name* but master's 68 test funcs across `internal/shot/*_test.go` cover the same surfaces (`waitDevToolsPort`/`waitSelector` in `capture_test.go` + `helpers_test.go` + `launch_test.go`; stub-CDP in `capture_test.go`/`helpers_test.go`/`jsonlist_retry_test.go`), and the name overlap is only `TestCaptureOverCDPNoPageTarget`. Re-porting 815 lines would duplicate master coverage and deliberately lose master's newer non-executable-override case. Not recovered. |
| **D** | `devdoc/shot-chrome-numeric-sort.md` (+71) | **OBSOLETE** | The report of the *session* that is being recovered. Its one durable statement (the ordering contract) lives in the code comment + this report; the file itself carries deliverable-declaration bookkeeping for a closed session. |
| **E** | 3× `docs(shot): …record the deliverable declaration` commits | **OBSOLETE** | Bookkeeping for the closed session, superseded by this report's declaration section. |

## What was re-derived (branch `fix/shot-chrome-numeric-sort`)

- `internal/shot/chrome.go` — commit **`92173106`**: `versionZeroDigit` const;
  `comparePlaywrightVersions` (numeric compare, leading zeroes trimmed, `""`
  ranks last, `-1/0/+1` like `strings.Compare`); `trimVersionZeroes`; the cache
  sort switched to `sort.SliceStable` + `comparePlaywrightVersions`.
- `internal/shot/shot_test.go` — commit **`47d6ec08`**:
  `TestFindChromeCacheOrderingIsNumeric` (RED before the fix, green after) and
  `TestComparePlaywrightVersions` (10 rows + antisymmetry).

`go build ./...` green; `gofmt -l internal/shot/` silent.

## Not Done (honest)

- Unit C's 815 lines are **not** in this branch. The verdict is OBSOLETE with the
  superseding commits named; if a future reviewer disagrees, the tag's copy is
  readable with `git show archive/fix/shot-chrome-numeric-sort:internal/shot/cov_hp_test.go`
  — nothing was deleted.
- `devdoc/shot-chrome-numeric-sort.md` was not recreated; this file replaces it.
- **Pre-existing red gates, not caused by this diff** (reported, not fixed — out of
  scope): `go test ./internal/standards/` is red on CLEAN master with exactly the
  same numbers this branch produces (`Rule 16 %w: NEW=1`; `Rule 17 test env:
  NEW=4`), and `./internal/stylegate/` fails on 10 sites in files this diff never
  touches (`cmd/agent/merge_lock_test.go`, `cmd/agent/cli_tasks_child_retry_test.go`,
  `internal/scratch/gc_stale_age_test.go`, `internal/tools/procstat_parse_delegation_test.go`,
  `internal/tools/merge_build_gate_gocache_test.go`). `TestDocManifestCoversEveryDevdocFile`
  is likewise already red on master over ~5 unclassified devdoc files
  (`devdoc/DOC-INDEX.tsv` 2063 rows / 2068 files) — this branch's report carries
  its own classification row so it does not ADD one.

## Verification

- Build: `go build ./...` → **OK** (worktree `/home/user/workspace/gopi-shot-numsort`).
- Red→green: `go test ./internal/shot/ -run 'TestFindChromeCacheOrderingIsNumeric' -count=1`
  → **FAIL** before the fix (transcript in instrument 4), then
  `go test ./internal/shot/ -run 'TestComparePlaywrightVersions|TestFindChromeCacheOrderingIsNumeric|TestFindChrome|TestPlaywrightVersion' -count=1`
  → **ok … 0.661s**.
- Branch-scoped suite (the only package the diff touches):
  `go test ./internal/shot/ -count=1` → **ok github.com/nick/gopi/internal/shot 33.331s**.
- `gofmt -l internal/shot/` → no output.
- Commits: `92173106` (fix), `47d6ec08` (test), plus the report commit that carries this file.

## Evidence (self-filed)

- `git cherry -v master archive/fix/shot-chrome-numeric-sort` → 9 lines, all `+`; quoted verbatim above.
- `git rev-list --count master..archive/fix/shot-chrome-numeric-sort` → `9`; `git merge-base --is-ancestor … master` → `exit=1`.
- Per-symbol `git grep -c -w` matrix: master 0/0/0 files, tag 2/1/1 files (table above).
- Red test on master code: `--- FAIL: TestFindChromeCacheOrderingIsNumeric` … exit code **1**, message quoted above; same test green after the fix.
- `git status --porcelain` in the worktree at close: `(no output)` — see the deliverable declaration.
- Tag/branch untouched: no `git checkout`, `git commit`, `git tag`, `git branch -d` or `git push` ever ran against `archive/fix/shot-chrome-numeric-sort`; every read was `git show` / `git diff` / `git grep` / `git rev-list` on the ref name.

## Deliverable declaration

- **Branch**: `fix/shot-chrome-numeric-sort`, worktree `/home/user/workspace/gopi-shot-numsort`.
- **Commits**: `92173106` (prod fix), `47d6ec08` (tests), + the report commit.
- **Recovery**: `git show fix/shot-chrome-numeric-sort:devdoc/recover-shot-chrome-numeric-sort.md`
  (and `git show fix/shot-chrome-numeric-sort:internal/shot/chrome.go` for the fix).
- The worktree shares the main repo's object store, so the commits are already on
  the branch in `/home/user/work/GoPi` — no push needed (Rule 6c).
