<!-- doc-class: historical -->
# Style sweep — follow-up record (post-merge)

This is the SECOND round: the integration pass had already landed `5349526`, and
then the remaining items were worked to completion. The headline is section 4:
the sweep's own Batch 3 contained a real behavioural regression — it was found
here, bisected, fixed, and is now guarded by a new scanner rule. Everything else
in this file is smaller.

## 1. Master moved again: `16dfe7e` merged

`16dfe7e` (docs: re-anchor the two `loop.go` references that drifted) landed on
master after the `955bb91` integration. Merged into `feat/style-sweep` as
`a8f949c` — clean, one doc file, no conflict with this branch.

Consequence: the pre-existing `TestHippocampusDocAnchorsResolve` failure this
report recorded is **gone upstream**. Verification on the post-merge tree:

    go test ./internal/agent/ -run TestHippocampusDocAnchorsResolve -count=1  ->  ok 0.177s

Bug row `01a08dc7-28f7-73ad-ba99-2904caf62708` closed as completed with the
`16dfe7e` / `a8f949c` evidence.

## 2. Final gate state on the post-merge tree (`a8f949c`)

    go build ./...                      OK
    go vet ./...                        clean
    go test ./internal/standards/       ok  7.3s
    go test ./internal/stylegate/       ok  7.8s

Both ratchets green on the same tree — the point of the integration pass.

## 3. The last detector disagreement, closed (`2858a4b`)

_(small; §4 is the substantive one)_

The integration pass had left ONE site where the two gates disagreed:
`cmd/agent/cli_tasks.go:1305` sat in the standards baseline as a
`mentionsError` false positive (the call already used `%w`; its `%v` verbs
formatted a string constant). Investigating it changed the verdict: `runErr`
there is **never nil** (`fmt.Errorf("synthetic child failure (test escape)")`
two lines up), and the sibling return 52 lines below — the real failed-child
path, `cli_tasks.go:1363` — already emitted the identical message with `%w`.

`%v` was therefore strictly worse: the wrapper could not be unwrapped by
`errors.Is/As`, and the test escape produced a *different* message string from
the production path it stands in for. Fixed to `%w` with that rationale
recorded inline.

Consequence, and the reason it was worth doing: the standards gate refused the
now-stale baseline entry ("STALE baseline entry … matches no violation — delete
the entry so the gate keeps ratcheting"), the entry was deleted, and both
detectors now report the **same 9** production sites. The staleness check doing
its job is the argument for keeping the standards gate in any future merge.

Tooling count corrections that followed: `internal/stylegate/gate_test.go`
ceiling `errorf-verb` 10 → 9, and three stale line-number / stale-count passages
in `devdoc/style-sweep-20260912.md` (earlier revisions listed the retained set at
pre-edit line numbers and claimed 10 retained sites). `cmd/agent/main.go:487`
remains the single shape-check-only entry — deliberate, documented on the
baseline itself.

**Load-sensitive suite result, and exactly what is known.** The first full-suite
run of this round reported `FAIL github.com/nick/gopi/internal/tools (245.090s)`,
but its output was dominated by `[task-notify]` lines from the test tree itself,
so the failing test's NAME was never captured. What followed:

| Run | Scope | Result |
|-----|-------|--------|
| `internal/tools` alone | branch | **ok** (173.8s) |
| full suite | branch (`job-10`) | **ok**, `internal/tools 245.9s`, zero FAIL lines |
| full suite | master `0bc42bd` (`job-11`) | **ok**, `internal/tools 238.5s`, zero FAIL lines |

The last two are a clean **A/B**: the same command, run concurrently, on this
branch and on master's tip, with identical outcomes. So there is no
branch-specific `internal/tools` failure to chase, and the single `FAIL` did not
reproduce in three later runs including a full-suite rerun on the same tree.

Two honest caveats, recorded rather than smoothed over: (1) the failing test name
was lost because a `tail`/grep was applied to output that the suite itself floods
with notify lines — **the same output-trimming mistake that hid the Batch 3
regression for a whole round** (see §4); (2) this box runs several other agent
worktrees concurrently (load average 8–15 on 4 cores), so a load-sensitive test
is a plausible-but-unproven cause. No cause is asserted.

**The unreproduced `unchecked-close: 33 vs 32`.** One run of `TestStyleGate`
reported 33 test hits where every other run reports 32. It did not reproduce in
10+ reruns, nor in 3 reruns under synthetic load (12 CPU hogs on 4 cores, load
average 11). Rather than record a guessed cause, the failure output was changed
to be self-diagnosing: the ceiling assertion now lists every hit as
`file:line (detail)` instead of printing only a count, so the next occurrence
names the 33rd site. Verified by temporarily lowering the ceiling to 2 — the
diagnostic printed all 32 sites. That is the honest state: one unreproduced
observation, a diagnosis path added, no cause invented.

## 4. THE regression of the sweep — found here, fixed here (`d7a05f3`)

`go test ./cmd/agent/` was red on this branch. It is **mine**, and it is the
sweep's own Batch 3.

    TestDispatchToNodeDbPushSeedsPeerRowBeforeRun
      node row should mirror the claimed status, got: status: in_progress
      center row did not advance to in_progress:  status: claimed
      center row missing dispatch run provenance

Bisect (detached worktrees, same test, one commit apart):

    bb76825  (branch base)      PASS
    229e1da  (Batch 1, %v→%w)   PASS
    f3d9de7  (Batch 3 landed)   FAIL   ← a73e063 is the Batch-3 commit
    5349526  (post-integration) FAIL

Cause: `t.Setenv`'s undo is registered with `t.Cleanup` and runs when the
**TEST** ends. The code it replaced in the dispatch sandbox closure was
`os.Setenv(...)` + `defer os.Setenv(prev)` **inside the closure**, which restored
when the **CALL** returned. That test flips `GOPI_MEMORY_DB` center→node→center
*within one test*, so the per-call flip is load-bearing: with `t.Setenv` the
node ledger stayed installed, ① read the peer row and ③ read a claimed-status
row out of the wrong database. The previous round's full-suite output had been
trimmed to `tail -70`, which is exactly where this failure fell off the screen —
the reason it survived an integration pass that re-ran the suite.

Two instances, both fixed by restoring the explicit pair:

| File | Site | Was it breaking a test? |
|------|------|------------------------|
| `cmd/agent/dispatch_test.go` | dispatch push + confirm closures | **yes** — the failure above |
| `internal/tools/memory_sync_bidir2_e2e_test.go` | `syncSSHExchangeHook` per-peer flip | no — **latent**; the node helpers re-point the env before every use, so it holds the wrong value only between calls |

The other 5 converted sites (`internal/agent/taskendstore_test.go`,
`internal/agent/taskstartsearch_test.go`, and the test-scoped `t.Setenv`s in the
two files above) are genuinely test-scoped and stay converted.

### The guard: a new `test-setenv-scope` rule

The class is mechanically detectable, so it is guarded rather than remembered:

- a `t.Setenv` **inside a closure**, that
- does **not** own its `*testing.T` (a `t.Run` body owns its own `T` — cleanup
  there IS the subtest end, which is the scope the author meant), and
- sets a **per-call** value (a non-literal argument).

First draft flagged all 10 closure sites in the repo; investigating them showed
8 were table-driven cases setting a constant — a per-test constant cannot mislead
a later assertion — so the predicate was narrowed to the real hazard. Result: 1
true positive (the latent site above), 0 false positives, and
`TestSetenvScopeIsPerCall` pins the discrimination (per-call closure = finding;
constant-in-table closure and `t.Run` body = clean). Ceiling is 0.

**The transferable rule:** a mechanical style fix is only safe when the
replacement has the SAME lifetime as what it replaces. `t.Setenv` for a
test-scoped set is right; `t.Setenv` for a per-call flip inside a closure is
wrong — and the two are textually identical, which is why this needed a rule and
not care.

## 5. Merge is a PA step, and the guard says so

    bash scripts/safe-merge.sh feat/style-sweep
    -> REFUSED: branch is occupied by worktree /home/user/workspace/gopi-style-sweep

That is Rule 9's guard doing its job, not a failure: the branch still has a live
worktree, so the merger must `git worktree remove /home/user/workspace/gopi-style-sweep`
before merging. Neither the sweep nor this follow-up pushes anything — the
branch lives in the shared object store (Rule 6c).

## 6. Second master merge (`0cd8a90` → `5479b35`) and the ceiling it earned

Master advanced twice more while this round was in flight: `0cd8a90` (the
pgid-reaper / `internal/testutil/mockproc.go` work, plus the scratch-residue and
test-code-discipline audits, on top of `0bc42bd`) is the branch point. Merged as
`5479b35` — **two real semantic conflicts**, not textual ones:

1. **`internal/testutil/run.go`** — master rewrote `StartGopi` to spawn through
   `StartMockProcessCmd` (pgid-scoped reaper, `Stop()` → `p.mock.Reap()`), while
   this branch had a `//stylegate:ignore`-annotated `p.cmd.Process.Kill()`. The
   resolution keeps master's behaviour (the reaper owns the group, cleanup runs
   on every exit path including a panic) and drops the now-dead annotation.
2. **`internal/shot/cov_shot_x11_test.go`** — master introduced the fake-X
   `testutil.OnSkip`/`AllowStartFailure` form of the long-lived recorder; this
   branch had annotated the two `f.Close()` sites. Resolution: master's call
   shape, this branch's two reasons attached to it.

The remaining 16 changed files were this branch's annotations applying cleanly
onto master's rewritten tests. The merge ALSO had to annotate the sites master's
new files introduced that land on a **zero-ceiling** rule — otherwise the ratchet
goes red on the next tick. Exactly 8 marker lines differ from master
(`git show 5479b35 -m --first-parent -U0 | grep -c '^+.*stylegate:'`), and each
is load-bearing — 6 are new, 2 are this branch's reasons re-attached to a call
master rewrote:

| site | rule | scope / ceiling | origin | why it would otherwise be red |
|------|------|-----------------|--------|-------------------------------|
| `internal/testutil/mockproc.go:238,242` | `ignored-error` | production, ceiling **0** | new | master's own `// deliberate:` prose is not a marker the scanner reads |
| `internal/testutil/mockproc_test.go` (×4 `time.Sleep`) | `test-sleep` | test, ceiling **0** | new | master's own poll intervals and holder window |
| `internal/shot/cov_shot_x11_test.go:1181,1182` (×2 `f.Close()`) | `unchecked-close` | test, ceiling 32 — **saturated** | carried | the call shape became `OnSkip`; the reasons came from Batch 2 but the lines are new, so the marker must move with them |

The saturated ceiling is the general point: on a rule whose ceiling equals the
current count, ANY merge-introduced site is a red build unless it carries a
reason — the ratchet has no headroom by construction.

The nine NEW `ignored-error` findings master's own files introduced
(`mockproc_test.go` ×4, `legacy_residue_test.go` ×5) were deliberately NOT given
reasons — they are ordinary test-fixture discards, so they join the counted,
gated backlog below instead of being annotated away.

### The merge LOWERED `ignored-error` 211 → 182, so the ceiling came down with it

Master's rewrite of the `cmd/agent` stale/sweep/merger tests through
`internal/testutil` (`StartGopi`, `WaitPID`, the mock-process helpers) deleted
hand-rolled teardown whose discarded errors were part of this branch's 211. Net
per-file movement, measured (both trees scanned with the same binary):

| file | pre-merge | post-merge |
|------|-----------|------------|
| `cmd/agent/cli_tasks_sweep_zombies_test.go` | 6 | 0 |
| `internal/tools/taskzombie_test.go` | 6 | 4 |
| `cmd/agent/merger_singleton_e2e_test.go` | 5 | 0 |
| `internal/shot/cov_shot_x11_test.go` | 5 | 1 |
| `internal/testutil/scratchroot_test.go` | 5 | 1 |
| `cmd/agent/cli_tasks_test.go` | 4 | 3 |
| `cmd/agent/cli_tasks_waitpid_test.go` | 4 | 3 |
| `internal/tools/repo_resolve_row_test.go` | 3 | 1 |
| `cmd/agent/cli_resume_stale_test.go` | 2 | 0 |
| `cmd/agent/cli_serve_stale_test.go` | 2 | 0 |
| `cmd/agent/cli_tasks_list_sweep_test.go` | 2 | 0 |
| `cmd/agent/cli_tasks_recycle_e2e_test.go` | 2 | 0 |
| `internal/tools/task_finish_cleanup_test.go` | 2 | 0 |
| `internal/tools/taskzombie_sweep_test.go` | 2 | 0 |
| `cmd/agent/cli_web_align_test.go` | 2 | 1 |
| `internal/testutil/mockproc_test.go` | 0 | 4 |
| `internal/testutil/legacy_residue_test.go` | 0 | 5 |
| **moved subset (17 files)** | **52** | **23** |
| unchanged remainder | 159 | 159 |
| **rule total** | **211** | **182** |

(each row is `file : pre-merge → post-merge`, counted by `stylegate -json` on
the two trees. The 29 is a net: master's refactor deleted 34 annotated sites and
introduced 5 of its own — hence the per-file table rather than a single delta.)

A ceiling is a ratchet: leaving it at 211 would have silently re-permitted the 29
sites master's refactor removed. This is the ceiling's weak spot versus the
`internal/standards` baseline: a line-keyed baseline FAILS LOUD on a stale entry
("matches no violation — delete it so the gate keeps ratcheting"), while a COUNT
ceiling is merely green when it is too high and only bites when the rule is
exceeded again — so the only defence is re-measuring it after every merge.
`gateCeilings[RuleIgnoredError][true]` is therefore tightened **211 → 182** (a
new commit on this branch), and the documents and comments that quoted 211 now
quote 182. Measured on the merged tree:

    ignored-error 182   unchecked-close 32   errorf-verb 9   TOTAL 223
    test-sleep 0  test-setenv 0  test-setenv-scope 0  test-skip 0

### Verification on the merged tip

    go build ./...                       OK
    go vet ./...                         clean
    go test ./... -count=1               EXIT 0, zero FAIL lines
      cmd/agent 46.2s  internal/agent 60.1s  internal/tools 248.8s
      internal/standards ok 2.3s  internal/stylegate ok 4.8s
    internal/standards data: Rule 16 hits=10 baselined=10 escaped=0 NEW=0;
      Rule 17 hits=112 baselined=105 escaped=7 NEW=0 — and those 7 escapes ARE
      this round's deliberate `os.Setenv`+restore pairs (5 in
      cmd/agent/dispatch_test.go, 2 in internal/tools/memory_sync_bidir2_e2e_test.go),
      i.e. the sites §4 restored; Rule 18 7/7/0/0; Rule 19 and 20 each 1 hit,
      1 escaped, 0 NEW (documented inline escapes, not regressions)
    stylegate -list: errorf-verb 9, ignored-error 182, unchecked-close 32

Commits added by this section: `5479b35` (merge master `0cd8a90`) + `0dde942`
(the 211 → 182 ceiling tightening, the count corrections in
`internal/standards/gate.go`, `devdoc/standards-gate.md` and
`devdoc/style-sweep-20260912.md`, and this document) + `00d351c` / `3f005de` /
`c082d21` (the movement table, the annotation itemisation, and the rule/scope/
origin labels) + `6aab37e` (the supersede banners on the first report's numbers).

`devdoc/style-sweep-20260912.md` (the first report) carried a full-suite result
that was true at ITS tip but was red on `TestHippocampusDocAnchorsResolve`; that
banner is not cosmetic — a later reader taking the old section as current would
think the branch's suite is red and re-investigate a closed upstream failure.

### Reproducing the movement table

The count table above is the ONLY place the per-file movement is recorded — the
scanner reports counts, not history. Reproduce it with:

    git worktree add --detach /tmp/pre 905686f   # the pre-merge tip
    (cd /tmp/pre && go run ./internal/stylegate/cmd/stylegate -json -o /tmp/pre.json)
    go run ./internal/stylegate/cmd/stylegate -json -o /tmp/post.json
    # join on (rule, file) and print the rows whose count changed

Both throwaway worktrees were removed after measuring (`git worktree list`
confirms). The merge's own contribution on top of master is exactly the
annotations — `git show 5479b35 -m --first-parent -U0 | grep -c '^+.*stylegate:'`
returns 8 (4 `ignore` + 4 `allow-sleep`) — while the branch-vs-master diff is the
whole sweep, which is why the per-file movement table above was produced by
scanning the two trees rather than by reading a diff.


## 7. Closing state — what came after the full-suite run, and what was re-verified

**UPDATE (supersedes the "inference" paragraph below): the full suite HAS now been
run at the tip.** `CGO_ENABLED=0 go test ./... -count=1` at `98ecff4` →
**EXIT 0, 18/18 packages `ok`, zero `FAIL` lines** (`cmd/agent` 43.6s,
`internal/agent` 48.5s, `internal/tools` 211.1s, `internal/shot` 39.7s,
`internal/standards` 2.5s, `internal/stylegate` 4.5s, `testsuite` 0.8s). The
inference below is kept only as the record of why it was ever needed.

The commits that carry §7–§8 (this document's own closing edits, and §8) postdate
that run and are **`devdoc/style-sweep-followup.md` only** — `git diff
--name-only 98ecff4..HEAD` is the one-command proof, and a `.md` file cannot move
a test result. The Go tree the `EXIT=0` describes is exactly `98ecff4`'s.

**A measurement trap worth recording.** The first attempt at this run was
mistaken for lost across a session boundary and restarted, which left **two**
`go test ./...` processes running concurrently while writing the *same* log path
— their output interleaved and the shared log showed a `FAIL` that neither run
would produce alone: `TestMemorySearchNonBlocking` ("concurrent entries not
searchable") in `internal/tools`. It is a **documented pre-existing load-sensitive
SQLite first-open flake**, not a regression from this branch:

- it passes 5/5 in isolation (`go test ./internal/tools/ -run
  TestMemorySearchNonBlocking -count=5` → ok), and the clean solo full-suite run
  above passes it inside the suite;
- this branch's *only* changes to `internal/tools/memory_test.go` are two
  `//stylegate:ignore` comments; `setMemoryDB` (the fixture it depends on) is
  byte-identical to the base commit;
- the flake is independently recorded in `devdoc/bidir2-fix.md`,
  `devdoc/preexisting-test-reds-fix.md`, `devdoc/e2e-live-merge-report.md` and
  `devdoc/branch-scan-archive-filter.md` as a known environment flake.

So: **never run two full suites at once** — the interleaved output is not a
measurement, and the cost here was a false red plus a re-run. The solo
`EXIT=0` in the paragraph above is the real result.

**The superseded inference** — the full-suite run quoted in §6 was taken at
`5479b35` + the ratchet commit, i.e. **before** the count corrections and this
document's own sections landed. Nothing after it touches Go behaviour, which is
why "still holds" was the claim; but "still holds" is an inference, and the edits
are exactly the kind that can turn a gate red (a ceiling moved in
`gate_test.go`, a count-bearing comment moved in `internal/standards/gate.go`).
What was verified **at the tip** is therefore
listed separately:

    tip = d7512b9

    go test ./internal/standards/ ./internal/stylegate/ -count=1
      ok  github.com/nick/gopi/internal/standards  5.4s
      ok  github.com/nick/gopi/internal/stylegate  6.2s
    go test ./internal/agent/ -run TestHippocampusDocAnchorsResolve -count=1
      ok  github.com/nick/gopi/internal/agent  0.112s

The two ratchets re-run green *because* they are the assertion that the counts in
the prose are real: the ceiling `RuleIgnoredError{true: 182}` in
`internal/stylegate/gate_test.go` fails if the tree carries one site more, and the
`standards` gate fails if a baseline entry no longer matches a violation. So the
doc corrections could not have silently desynced the gates.

Only three of the commits in this round are non-doc:

| Commit | Change | Why it is safe |
|--------|--------|----------------|
| `0dde942` | `gate_test.go` ceiling `211 → 182` | tightened, asserted by the gate itself |
| `0dde942` | `internal/standards/gate.go` comment `11 → 10` | comment only |
| `ae683ec` | the ceiling assertion names every hit as `file:line` | message text only |

Everything else — `00d351c`, `3f005de`, `c082d21`, `6aab37e`, `4623704`,
`d7512b9` — is a `.md` file. `git diff --name-only 5479b35..HEAD` is the
one-command check: it lists exactly `devdoc/standards-gate.md`,
`devdoc/style-sweep-20260912.md`, `devdoc/style-sweep-followup.md`,
`internal/standards/gate.go`, `internal/stylegate/gate_test.go`.

**Gate state at the tip** (`stylegate -list`): `errorf-verb 9`,
`ignored-error 182`, `unchecked-close 32`, `TOTAL 223`; `test-sleep`,
`test-setenv`, `test-setenv-scope`, `test-skip` all `0`. The retained
`errorf-verb` sites are the 9 nil-guarded ones enumerated in
`devdoc/style-sweep-20260912.md`, and `cmd/agent/main.go:487` remains the one
documented shape-check-only entry in the standards baseline — 9/10 agreement
between the two detectors, the difference being deliberate.

**Pre-flight for the handoff merge (§8)**: `git merge-tree` against master
`e6a5555` resolves clean; the branch is 12 commits behind master, whose own new
changes (8 files) add no gate hits to this tree. See §8.


## Verification

This section is the `a8f949c`/`d7a05f3` snapshot; **§6 supersedes its counts** and
**§7 supersedes its tip** (the final full-suite run at `5479b35`+ratchet, and the
re-run gates at `d7512b9`).

- Build: `go build ./...` → OK
- Vet: `go vet ./...` → clean
- Gates (same tree, commit `d7a05f3`):
  - `internal/standards` → **ok**; data report shows `Rule 16 hits=10 baselined=10
    escaped=0 NEW=0`, `Rule 17 hits=112 baselined=105 escaped=7 NEW=0`, `Rule 18
    hits=7 baselined=7 NEW=0`, `Rule 19/20 escaped, NEW=0` — zero NEW violations,
    zero stale entries (the Rule-18 `stylegate.go:263` entry was deleted after
    the function was refactored to the guard-clause form).
  - `internal/stylegate` → **ok**; `stylegate` report at that commit: `errorf-verb 9`,
    `ignored-error 211`, `unchecked-close 32`, `TOTAL 252`, and the four hygiene
    rules (`test-sleep`, `test-setenv`, `test-setenv-scope`, `test-skip`) all at
    **0** — the last is the new rule guarding the regression above. (Superseded by
    §6: the merge with master `0cd8a90` removed 29 of those sites through
    `internal/testutil`, so the ceiling is now 182 and `TOTAL` 223.)
- The regression itself: `go test ./cmd/agent/ -run 'TestDispatchToNode' -count=1`
  → **ok** (was 3 assertion failures before `d7a05f3`); `internal/tools -run
  TestBidir` → **ok** (the latent site).
- Anchor test: `internal/agent` `TestHippocampusDocAnchorsResolve` ok 0.177s.
- Retained sites: `stylegate -list` → `errorf-verb 9`; standards baseline → 10
  entries (those 9 + the one documented shape-check false positive).
- Commits: `a8f949c` (merge master `16dfe7e`) + `b3802f7` + `2858a4b`
  (`cli_tasks.go:1305` → `%w`, ceiling/entries corrected) + `d7a05f3` (the
  regression fix + the `test-setenv-scope` rule) + `5479b35` (merge master
  `0cd8a90`) + the ceiling tightening and this document (§6).


## 8. Handoff pre-flight — the branch is 13 commits behind master, and the merge is clean

Everything above was verified with master at `0cd8a90` merged in. Since then
master itself advanced to `e6a5555` (12 commits: two merges — `feat/pricing-deepseek-flash`,
`fix/test-scratch-ambient-tmp` — plus their docs). A branch that has been open for
many rounds must not be handed off on the assumption that the eventual merge is
trivial, so the merge was **predicted, not performed** (a merge is the PA's step):

    git merge-tree $(git merge-base HEAD master) HEAD master
    → exit 0, exactly one `changed in both` file

**The one overlap is understood.** `internal/agent/testmain_isolation_test.go` is
touched by both sides, on different lines of the same function:

| Side | Change |
|------|--------|
| master (`b0df0ce`-era work, ticket close-out) | `os.MkdirTemp("", …)` → `os.MkdirTemp(testutil.SuiteTempRoot(), …)` plus a `HasPrefix(pinned, root)` assertion — the ambient-`/tmp` pin bypass |
| this branch (`a73e063`) | `//stylegate:ignore` reasons on the adjacent `os.Setenv` / `os.RemoveAll` lines |

`merge-tree` resolves it by keeping **both** edits, and they are coherent: master
changes the *call*, this branch annotates *neighbouring* lines. Manual resolution
should not be needed — but whoever merges must confirm the two annotation
comments still sit on the lines they describe (a re-attached reason that no
longer matches its call is the failure mode auto-merge cannot see).

**Master's new gate was checked against this tree, not assumed.** The same
window added `assertTestSourcesCreateNoTmpPath` (`internal/testutil/scripts_gate_test.go`,
part of `fix/test-scratch-ambient-tmp`), which scans **every** `_test.go` file —
including the 106 this branch rewrote. Its predicate needs a `/tmp` spelling (or
an ambient-root spelling, `os.MkdirTemp(""…` / `os.TempDir()`) **and** a
filesystem-creating call **on the same line** (`stripComment` first, so prose
never trips it). Extracting this branch's added test lines
(`git diff 0cd8a90..HEAD -U0 -- '*_test.go'`, 824 lines) and running that
predicate over them yields **0 violations** — so the post-merge gate does not
turn red on this branch's work. Its exemption table names three files
(`scripts_gate_test.go`, `pin_coverage_test.go`, `cov_shot_x11_test.go`) and the
gate itself asserts each still exists; all three exist on this tree.

**Re-checked after master advanced again** (to `d78b83e`, a 13th commit: `scripts/s3cost.py`,
`scripts/s3log_stats.py`, `scripts/test_s3log_stats.py` — Python-only, zero Go files):
`git merge-tree` is still `exit 0`, still exactly ONE changed-in-both file
(`internal/agent/testmain_isolation_test.go`, the case analysed above), and the
overlap set (`git diff --name-only merge-base..master` ∩ `..HEAD`) is unchanged.
Python scripts are outside every ratchet gate in this repo (`internal/standards`
and `internal/stylegate` scan `.go` only) and outside the declared Go build, so
this commit adds no pre-flight surface. The counts in this section read "12"
where they describe the pre-flight snapshot at `e6a5555`; the live delta is now 13.

**What is *not* pre-flighted**: the 13 new master commits were not merged here, so
the tip `2c039bd` has *not* run against master's new `internal/pricing` tests or
the new test-source gate. That is fine for handoff — after the merge the PA's
merge-round runs the declared build (`CGO_ENABLED=0 go build -o bin/gopi
./cmd/agent/`) and the gate suite on the merged tree — but it is why the
`EXIT=0` above is a statement about `2c039bd`, not about `2c039bd + master`.

Reproducing the pre-flight:

    git fetch            # or: the shared object store already has master
    git merge-tree $(git merge-base HEAD master) HEAD master   # exit 0 = clean
    git diff --name-only $(git merge-base HEAD master)..master # master-only changes
    # overlap = intersection with: git diff --name-only $(git merge-base HEAD master)..HEAD

## 9. Third master merge executed — textually clean, but master brought a RED gate (`0066019`, `dca15ff`)

§8 pre-flighted the handoff and called the merge clean. This section records what
actually happened when the merge was **executed** rather than predicted, because
the two differ in a way that matters.

### 9.1 The merge itself

    git tag -f style-sweep-premerge-00c9c02 00c9c02    # rollback anchor
    git merge master -m "merge master d78b83e (13 commits: /tmp scratch ban, pricing, s3 ops)"

Merge-base was `0cd8a90`; master had advanced to `d78b83e` (13 commits: the /tmp
scratch ban, the DeepSeek V4.1-Flash price-table repair, the S3 cost scripts).
Result: `0066019`, **no conflict**, 11 files changed / +1585 −21. Exactly one file
was changed on both sides — `internal/agent/testmain_isolation_test.go` — and the
auto-merge kept both edits coherently (master's `testutil.SuiteTempRoot()`
rewrite plus this branch's adjacent annotations). §8's `merge-tree` prediction
held.

### 9.2 What §8 could not predict: master was already RED

`go test ./internal/standards/` **on the merged tree failed**, with 4 NEW Rule 17
violations at `internal/agent/testmain_isolation_test.go:76-79` and 4 STALE
baseline entries at the same file's `:62-65`.

The important step was *not* to fix it on the branch first. Running the gate on
**pristine master in the main repo** (`/home/user/work/GoPi` @ `d78b83e`, evidence
`/tmp/mst-full.txt`) produced the **byte-identical** 4 NEW + 4 STALE. So:

  * master's `internal/standards` gate is red **independent of this branch**;
  * this branch's tip was green immediately before the merge (re-verified:
    `ok github.com/nick/gopi/internal/standards`);
  * therefore the merge *inherited* a failure it did not cause, and every
    remaining red line after merging is `master`'s to answer for.

Root cause (5-why): the gate keys baseline entries by `file:LINE`. Commit
`6093c76` ("test(scratch): close the /tmp ban's ambient hole") rewrote
`testmain_isolation_test.go` (+24 lines), pushing its Rule 17 sites from `:62-65`
down to `:76-79` — and did not update `internal/standards/baseline.go` (its stat:
that test file, two `internal/testutil` test files, one devdoc; **not**
baseline.go). Any line-keyed baseline is invalidated by an edit above it, so the
gate went red in the very commit that moved the lines and stayed red. The merge
made the drift *visible on this branch*; it did not create it.

Fix carried on this branch (`dca15ff`): re-point the 4 entries `:62-65` → `:76-79`
— exactly what master's own commit should have done. With that single 4-line
change the merged tree's standards gate is green (`ok ... 1.405s`).

Independently reported to the queue as bug **`01a08e0f-f3be-7a34-ac08-3618b20381c8`**
(payload = repro + pristine-master evidence + the 5-why + this fix), so master can
be repaired without depending on this branch being merged.

### 9.3 Standing hazard this exposes

A *line-keyed* baseline and a *long-lived* branch are a two-sided drift machine:
the branch prunes baselines against its own file layout, master moves the same
lines, and a downstream merge either conflicts or silently re-breaks whichever
copy is stale. The concrete rule for the PA at handoff: **fix master's baseline
first (or take `dca15ff`), then merge** — do not merge a baseline prune into a
master whose own baseline is stale, or the red will be attributed to the wrong
side. (Contrast §6: the stylegate ceiling is a *count*, which has the opposite
failure mode — it stays silently green when too high. Both are recorded.)

### 9.4 Measurement status of this section

`gofmt -l` clean, `CGO_ENABLED=0 go build ./...` OK, `go vet ./...` clean,
`internal/standards` + `internal/stylegate` both green, on the merged tree at
`dca15ff`. The full `go test ./... -count=1` on the merged tip is run as a single
dedicated job-log — **one suite at a time**, per the trap already paid for in §7
(two concurrent full suites interleaving into one log produced a false red).

**CLOSED — measured, not inferred.** `CGO_ENABLED=0 go test ./... -count=1` on the
merged tip `9163d7c` → **exit 0, 18/18 packages `ok`, zero `FAIL` lines**
(`cmd/agent` 45.2s, `internal/agent` 62.2s, `internal/tools` 266.5s, `internal/shot`
43.3s, `internal/standards` 2.1s, `internal/stylegate` 3.3s, `testsuite` 1.1s;
5 packages report no test files). Log: `/tmp/stylegate-fullsuite-merged-tip.log`.

Note on that run's conditions: `internal/tools` took 266.5s (vs ~211s solo in §7)
because a *different* worktree (`gopi-read-tilde-home`) was running its own
`go test ./internal/tools/` at the same time. That contention is exactly the §7
false-red condition — and this run stayed green through it, so the merged tip is
green both solo and under the load that previously manufactured an artifact.

## 10. Fourth master merge — the `git stash` trap that turns a merge into a non-merge (`f57e2b5`)

Master advanced 25 commits (`d78b83e` → `805b313`: the scratch-GC resident tick,
the new Rule 21 env-reader gate, the read-tilde test-HOME fix). Two things in it
are exactly the shape this branch is most exposed to, and both were measured
rather than assumed.

### 10.1 The dual-touched files, and the one conflict

`d78b83e..805b313` touches 26 files; 4 intersect this branch, and only one
conflicts:

| file | why both sides touch it | outcome |
|---|---|---|
| `cmd/agent/cli_tasks.go` | master's `%w` fix vs this branch's adjacent work | auto-merged |
| `devdoc/standards-gate.md` | master's Rule 21 doc vs this branch's corrected counts | auto-merged |
| `internal/standards/baseline.go` | master's Rule 21 baseline vs this branch's prune/re-point | auto-merged |
| `internal/testutil/scratchroot.go` | **master MOVED the helpers out** | CONFLICT — resolved |
| `internal/tools/read_test.go` | another task's tilde fix vs this branch's annotation | auto-merged |

The conflict is worth stating precisely, because the merge-tree output reads
backwards at first glance: **master** is the side holding
`func pathWithin(...) { return scratch.PathWithin(...) }` — a one-line delegator.
Master's refactor moved `parseProcStartTime` / `procEntryHolds` / `pathWithin` /
`isPID` / `acquireGCLock` into `internal/scratch` (as `ProcStartTime` /
`ParseProcStartTime` / `PathWithin` / `IsPID` / `AcquireGCLock`, with
`GCLockFileName`), leaving `testutil` a thin surface. This branch's side still
carried the pre-refactor local copies, because the branch forked before the move.
Resolution: take master's side, delete the duplicates.

### 10.2 What master's new files did to the swept budgets (the §6 hazard, twice)

Master's new files re-introduced the **same logical sites** this branch had
already annotated — in `testutil` under the old location, now living in
`internal/scratch` where the annotations did not follow:

- Rule 21's baseline is **empty by construction** (its own doc says so), so
  master's brand-new gate 6 is pointed at all 192 files this branch changed.
  Measured: it did **not** fire — required to be checked, not assumed.
- The count ceilings fired exactly as §6 predicted: 4 production discards,
  3 test discards, 2 test sleeps landed against budgets of 0 / 182 / 0.

The fix was to re-apply the **identical reasons** the sweep had used for the same
sites, and to add the two genuinely-new callee keys (`holder.Process.Wait`,
`filepath.Walk`) to the reviewed reason table so a future `-annotate` run
reproduces them. Verified by diffing the complete finding sets against a
worktree at the pre-merge tip: **identical**, except `read_test.go:44` → `:50` —
an already-annotated site the read-tilde fix merely shifted. Net effect on the
budgets: **zero** — no ceiling re-tune is warranted, and this is a measurement,
not a judgement.

### 10.3 THE TRAP — `git stash` mid-merge silently produces a NON-merge commit

The first attempt produced commit `f5510f1` with **one parent**. It is easy to
mistake for a merge: it carries master's new files, and `git log --oneline` shows
a merge-looking subject. The mechanism:

1. `git merge master` → conflict; the auto-merged modifications are in the
   **worktree only** (index has base, hence `M` on some and `A` on others).
2. A `git stash` (used to compare trees) **stashes worktree+index**, rewinding the
   tracked files to `HEAD` — i.e. undoing the auto-merge for every tracked path.
   *Untracked* staged adds (`A`) survive as index entries, and the conflicted
   file's content can settle to a merged-looking state.
3. `git stash pop` re-applies the diff onto `HEAD` — the merge's parent identity
   (`MERGE_HEAD` is not itself stashed) is now divorced from the tree.
4. Staging the 4 edited files and committing yields a **single-parent commit**
   whose tree is a *partial* merge (master's adds, minus the auto-merged
   modifications of pre-existing files).

**Detection** (cheap, do it every merge):
```bash
git rev-parse --verify HEAD^2        # must succeed for a merge commit
git log --oneline --parents -1 HEAD  # one hash = NOT a merge
```
**Recovery**: reset to the pre-merge tip (`git reset --hard <tip>`), re-run
`git merge master`, re-apply the conflict resolution, stage, and commit. A
`git diff` of the bad state was saved before resetting, so nothing was lost.

**Rule for the next agent: never run `git stash` between `git merge` and the
merge commit.** If you need a clean-tree comparison, create a second worktree at
the tip — never stash the merge in progress. The check above costs one command
and is the only thing that catches it, because the resulting commit *looks* fine.

### 10.4 Measurement status of this section

At the merge tip `f57e2b5` (parents `b56193e` + `805b313`, behind-master = 0):
`gofmt -l internal cmd scripts` clean, `go vet ./...` clean,
`CGO_ENABLED=0 go build ./...` OK, `internal/stylegate` + `internal/standards`
both green, and the finding sets byte-identical to the pre-merge tip. The full
`go test ./... -count=1` is run as a single dedicated job on its own log — one
suite at a time (§7's trap; note a peer worktree may still be running its own
`internal/tools`, which inflates that package's wall time without changing the
result, as seen in §9.4).

### 10.5 Master's Rule-17 red is STILL UNFIXED — the fix lives only on this branch

Re-checked at the merge tip against master `805b313`: master's
`internal/standards/baseline.go` still lists
`internal/agent/testmain_isolation_test.go:62..65`, while master's own copy of
that file has its env sites at `:76..79` (verified by reading both sides). So the
red gate reported in §9.4 and filed as bug task `01a08e0f` is **live on master
right now** — master did not repair it during the 25 commits since.

This branch carries the correct re-point (`dca15ff`, preserved through this
merge). Consequence for the PA, unchanged from §9.3 and now measured a second
time: **repair master's baseline (or take `dca15ff`) before merging this branch**,
or the merge will land on top of a red gate and the red will be attributed to the
wrong side.

### 10.6 Measured at the merge tip

`CGO_ENABLED=0 go test ./... -count=1` at `43e8add` → **FULLSUITE_EXIT=0, 19/19
test packages `ok`, zero `FAIL` lines** (4 packages have no test files). Notably
`internal/tools` took **189.0s** — the uncontended solo figure, versus 266.5s in
§9.4 when a peer worktree ran its own copy. Same green result either way; the
wall-clock difference is contention, not behaviour.
Log: `/tmp/stylegate-fullsuite-merge3.log`.
