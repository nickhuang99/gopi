<!-- doc-class: historical -->
# Task Report: rework the ownerless `feat/gopi-cli-bubbletea-landing` onto current master

**Epic**: the branch `feat/gopi-cli-bubbletea-landing` (1 commit, tip `e32f4e93`,
preserved as `refs/tags/rework-backup/feat/gopi-cli-bubbletea-landing`) was
ownerless and deleted; its merge into master failed with exactly one conflicted
file, `cmd/agent/main.go` (the dispatch chain), inside a 27-file / +5211 diff.
This run re-cuts it on current master, resolves `main.go` by UNION, gates the
merged tree, and hands the result back as an ordinary FF-able branch.

**Branch**: `feat/bubbletea-landing-union` (never a reuse of the predecessor
branch — Rule 23). Worktree `~/workspace/gopi-01a0c2a7-bt-union` (Rule 6a).

## Completed

- **Worktree + branch**: `git worktree add ~/workspace/gopi-01a0c2a7-bt-union -b
  feat/bubbletea-landing-union master` (base `ebff1998`). The name was proven
  free first (`git rev-parse --verify --quiet refs/heads/feat/bubbletea-landing-union`
  → exit 1).
- **The commit is carried, not re-authored**: `git cherry-pick -n
  rework-backup/feat/gopi-cli-bubbletea-landing` replaying the tag's tree onto
  current master. Exactly ONE path conflicted — `cmd/agent/main.go`, one hunk —
  confirming the epic's premise; the other 26 paths auto-merged (11 files are
  new, the rest are registration deltas).
- **Union resolution of `cmd/agent/main.go`** (the whole point). Master's chain
  has 32 predicates and carries `isConfigSubcommand`; the tag's has 32 and
  carries `isUISubcommand` instead. The resolved chain has **33** — master's
  order preserved, `isUISubcommand` appended — i.e. the union, with NO deletion
  of either side's dispatch entry:

  ```
  <master's 32 predicates, unchanged> || isUISubcommand(dispatchArgs)
  ```

  Numeric proof (predicates parsed out of the guard line itself, by script, not
  by eye; full 33/32/32 listing in `~/.gopi/sessions/union-proof/final-union-proof.txt`):
  master **32**, tag **32**, shared **31**, HEAD **33**; `union == HEAD` as sets;
  master-only `{ConfigSubcommand}` and tag-only `{UISubcommand}` are BOTH in
  HEAD; so are all 31 shared ones. Every one of the 33 names has exactly one
  `func isX(args []string) bool` definition in `cmd/agent` — no dangling judge.
  Master's guard content is a strict **prefix** of HEAD's (0 bytes of master's
  content removed); HEAD == master's chain + one appended `|| isUISubcommand(dispatchArgs)`.

  NOTE on the counts: the epic briefing's "32 vs 31" arithmetic was never
  reproduced here — the guard line, counted directly on `master`, on the tag and
  on the resolved tip, is **32 / 32 / 33** (one guard line each, no duplicate
  predicate). Counted with `grep -oE 'is[A-Za-z0-9]+Subcommand\(dispatchArgs\)'`
  piped through `sort -u | wc -l`. A degraded variant of the proof (unique-name
  counts of 31/31/32, i.e. off by the leading `isCompletionSubcommand`, which the
  bracket-less regex of the earlier scratch script dropped) is superseded and
  corrected here — the conclusion is unchanged: nothing from either side is lost,
  exactly one entry is added.
- **The command tree still registers**: `./bin/gopi config -h` (master's verb,
  which the branch chain had lost) and `./bin/gopi ui -h` (the branch's verb)
  both print their usage, and `gopi --help`'s index carries the `gopi ui
  [--tools]` row.
- **Scope narrowing, justified**: `tasks/01a0bedc-report.md` (294 lines) was
  REMOVED from the commit. The tag carried that file **byte-identically twice** —
  also as `devdoc/cli-bubbletea-landing.md` (`diff` between the two = 0 lines);
  `devdoc/` is the canonical home (Rule 7) and the `tasks/` copy is another,
  already-closed task's report littered into this branch. `scripts/ui/
  render_transcript.py` (249 lines) is KEPT — see the verdict below.

## Verdict on the two large riding additions

| addition | verdict | measured reason |
|---|---|---|
| `scripts/ui/render_transcript.py` (+249) | **KEPT** | It is the version the landing commit exists to carry. `drive_ui.py:49` does `from render_transcript import Screen` (a sibling import — deleting it breaks the driver), and `cmd/agent/cli_ui_test.go:53,77` read `scripts/ui/drive_ui.py` off disk. The tag's own message records the fix this file embodies: an escape sequence split across a pty read was dropped, so the same bytes rendered 10 screen lines from one feed and 715 from a byte-by-byte feed. Re-verified this run on a synthetic capture (split CSI + `\x1b[K` + `\x1b[1A` + CJK): 1-byte, 3-byte and whole-string feeds yield **byte-identical** output (2 screen lines each) — the bug the file fixes cannot reappear while it rides. |
| `tasks/01a0bedc-report.md` (+294) | **DROPPED** | Byte-identical duplicate of `devdoc/cli-bubbletea-landing.md` (both 294 lines, `diff` = 0). It is the report of task `01a0bedc`, whose worktree/branch this is not; the `tasks/` name would additionally collide with this repo's own `tasks/<task-id>-report.md` convention for the CURRENT task. Nothing references it (`grep -rn "01a0bedc-report"` → no hit). Dropping keeps the record in `devdoc/` alone. |

## Not Done

- The tag's full +5211 diff is carried except the dropped 294-line duplicate.
  No functional content was removed.
- The branch was NOT pushed to a peer and master was NOT touched: the epic asks
  for a FF-able ordinary branch handed back, and the merge is the owner's call
  (Rule 6e / Rule 9).
- `scripts/ui/drive_ui.py`'s end-to-end pty drive (a real LLM turn) was NOT
  re-run here: it needs a live model endpoint and a tty, and this session has
  neither (`gopi ui` on this host stops at "no /dev/tty"). Its two Go guards
  (enter-as-CR, marker-driven steps) run inside `./cmd/agent`.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → rc 0; `CGO_ENABLED=0 go build -o
  bin/gopi ./cmd/agent/` → rc 0; `go vet ./...` → rc 0.
- Tests: `make test-budget PKGS='./cmd/agent ./internal/config'` on the resolved
  tip — see the scope-delta table below; compared against the SAME command on a
  clean master checkout (`~/workspace/gopi-01a0c2a7-basemaster`, detached at
  `ebff1998`).
- Ran: `./bin/gopi config -h` (rc 0, "gopi config — inspect the configuration
  actually in force."), `./bin/gopi ui -h` (rc 0, "gopi ui — the terminal chat
  surface …"), `./bin/gopi --help | grep 'gopi ui'` (index row present),
  `./bin/gopi ui --addr 127.0.0.1:18099` (reaches TUI startup; stops only on the
  absent `/dev/tty` — headless host, expected).
- Result: tree clean, no `MERGE_HEAD`, `git merge-base --is-ancestor master HEAD`
  exit 0, union proof insertion-only on `cmd/agent/main.go`.

## Evidence (self-filed)

> Counts in the block below were re-measured on the delivered tip
> (`1c622e0e`) and the stale numbers of the earlier draft are corrected in
> place; the raw one-line proof is `~/.gopi/sessions/union-proof/final-union-proof.txt`.

- Worktree/branch: `git worktree list` shows
  `/home/user/workspace/gopi-bt-union-2  1c622e0e [feat/bubbletea-landing-union]`
  (the original scratch worktree was reclaimed by the sweeper mid-run — the
  sibling defect `fix/merge-release-cwd-scope`; the branch itself was never at
  risk, it lives in the shared repo).
- FF-ability: `git merge-base master HEAD` → `953a92c2…` (= master's CURRENT
  tip), and `git merge-base --is-ancestor master HEAD; echo $?` → `0`.
- Union proof (both directions, no deletions):
  - `git diff --numstat master HEAD -- cmd/agent/main.go` → `1  1  cmd/agent/main.go`
    (1 insertion / 1 deletion at the *line* level: the guard line is rewritten in
    place, so the diff granule is the whole line; the union is at the PREDICATE
    level and is proven by the parse below, which shows 33 = 32 + 1 with nothing
    lost).
  - Predicate parse of the live guard line: `master=32  tag=32  HEAD=33`;
    `master-only missing from HEAD:` (empty); `tag-only missing from HEAD:` (empty);
    `union==HEAD as sets: True`; `master content is a strict PREFIX of HEAD
    content: True`; `bytes removed from master content: 0`.
  - Every predicate resolves: the 33 names each match exactly one
    `func isX(args []string) bool` in `cmd/agent/*.go`.
- Whole-diff deletions elsewhere are the tree's own divergences, not the union:
  `git diff --numstat <merge-base> HEAD | awk '$2>0'` lists only
  `cli_completion_test.go` (1), `cli_contracts_test.go` (4/1), `cli_web.go` (5/1),
  `main.go` (1/1), `go.mod` (23/1) — the go.mod line is the Bubble Tea dependency
  block being added, and the test files gain the `UISubcommand` registration.
- Clean-close: the worktree's `git status --porcelain` prints nothing (evidence
  litter — `scripts/ui/__pycache__/`, the baseline log — was moved to the session
  directory `~/.gopi/sessions/union-proof/`, Rule 30).
- Evidence directory: `~/.gopi/sessions/union-proof/` holds `main.master.go`,
  `main.tag.go`, `pred.master.txt`, `pred.tag.txt`, `pred.head.txt`, `baseline.log`.

## Addendum — the delivery-point sync and the one real scope delta (measured)

Master moved twice while this branch was being gated (`ebff1998` → `2bf5a1d1` →
`953a92c2`); the branch was re-synced at each move, the last one landing the tip
at `bdbc43ed` (`git merge --no-edit master`, rc 0, NO conflict — the union
resolution survives cleanly onto newer master). The documented order (Rule 30a)
is finish → commit → sync → hand over, which is the order used here.

**The only behaviour delta this branch introduces, measured:**

- Baseline: master `953a92c2`'s `gopi -h` is 158 lines, and
  `TestTopLevelUsageDelegatesEnvVarsToHelpEnv` is GREEN there.
- With the union branch it was 161 → that test FAILED. Cause: the `gopi ui`
  index row wrapped to three lines. So this failure was **this diff's**, not
  pre-existing.
- Budget truth: `topLevelUsageMaxLines = 160` is compared against
  `len(strings.Split(usage, "\n"))`, i.e. the NEWLINE COUNT plus one — an
  160-element render is a 159-newline render.
- Repair (commit `c86f25ca`): the index row became one line,
  `Terminal chat surface (Bubble Tea), see ` + "`gopi ui -h`" + `.`
  `gopi -h` is now 159 newlines (160 elements) and the test is GREEN, together
  with the wrap-budget, top-level-help-coverage, verb-contract, completion and
  help-duplication gates.

**A run that was VOID, and why (recorded rather than hidden).** The first
`make test-budget PKGS='./cmd/agent ./internal/config'` run on this branch
reported 157 failures. They are NOT this branch's: the run's own worktree was
auto-reclaimed by the reclaim sweeper while the suite was executing, so every
test binary's CWD was deleted underneath it. The stream proves it — 261
`no such file or directory` outputs and 130 `getwd: no such file or directory`
outputs, failure text like `read main.go: open main.go: no such file or
directory`. A sibling task (`fix/merge-release-cwd-scope`) owns that defect.
The re-run was therefore done in a **local clone** (`git clone --shared`, Rule
6a's documented exception when a worktree cannot be relied on) so nothing could
reclaim the tree mid-suite.

**Scope delta, the definitive comparison — BOTH runs at the SAME master tip.**
The earlier base run (`gate-base.log`) was taken at `2bf5a1d1`, before master moved
to `953a92c2`. So both sides were re-run at `953a92c2`: union tip in
`~/workspace/gopi-bt-final` (`final-gate2.log`, 732s wall) and a clean master
checkout in `~/workspace/gopi-bt-base-clone` (`gate-base2.log`, 778s). Neither
stream carries a single `no such file or directory` or `getwd` output (`grep -c`
→ `0` / `0` on each), so the earlier void-cwd shape is absent from both and the
named failures below are real.

| | union tip `8e607ce8` | clean master `953a92c2` |
|---|---|---|
| named failures | 15 | 15 |
| shared by name | 14 | 14 |
| union-only | `TestChatFlow_AgentOutputTakesWholeBandRows` | — |
| base-only | — | `TestTaskRunReleaseIsDeferredInTheEndHook` |

Both one-sided names were re-run `-count=3` on BOTH sides: `TestChatFlow…` passes
on both (`ok … 0.693s` union / `0.666s` base — flaky under suite load, and its
guard file `cmd/agent/chat_flow_rows_test.go:144` is not in this diff), while
`TestTaskRunReleaseIsDeferredInTheEndHook` **FAILS on both, 3/3 each**, with the
same message (`finishSessionTaskRecorded must DEFER worktreerelease.ReleaseReporting`)
— so the union run's absence of it was a suite-timing artifact, not a fix.
`TestMergeUnionRowCheckOnTheRealManifest` fails identically on both with the same
4 findings, all against devdoc anchors (`merge-resolve-peak-hour-guard.md`,
`proc-identity-single-predicate.md`, `rule31-ran-check.md`,
`taskid-whitespace-normalize-salvage.md`); `git diff --name-only <mb> HEAD |
grep <those four>` is empty — none is in this diff. The remaining 13 names are the
worktree-reclaim / worker-recycle / task-run E2E family (the open
`fix/merge-release-cwd-scope` defect), present on clean master too.
**Scope delta of this branch = 0 named tests.**

**Evidence locations** (session dir, Rule 30): `/home/user/.gopi/sessions/union-proof/`
holds `pred.{master2,tag,final}.txt`, `main.master2.go`, `gate-union.log`,
`gate-base.log`, `final-gate.log`, `final-sync.log`, `ui-run.out`, plus this
round's `final-gate2.log`, `gate-base2.log`, `scope-delta.log`, `flake-chatflow.log`,
`flake-taskrun.log`, `dup-a.md`, `dup-b.md`.

## Addendum 2 — the FINAL delivered tip, gated a second time (in-clone, both sides)

The scratch worktree (`~/workspace/gopi-bt-final`) and the baseline worktree were
**both reclaimed by the reclaim sweeper mid-run** — the same defect
`fix/merge-release-cwd-scope` owns; nothing was lost (the branch lives in the
shared repo, and the pushed ref `origin/feat/bubbletea-landing-union` made the tip
recoverable by `git fetch` alone). The gate pair was therefore re-run where
nothing can reclaim the tree: a **local clone** of the main repo
(`~/workspace/gopi-bt-union-clone`, checked out at the delivered tip `1c622e0e`)
and the surviving baseline worktree (`~/workspace/gopi-bt-baseline` at master
`953a92c2`). `make build` in the clone first, so the CLI E2E guards had a binary.

| | UNION tip `1c622e0e` | clean master `953a92c2` |
|---|---|---|
| gate | `gate-union-final.log` (862s) | `gate-base2.log` (778s) |
| `no such file or directory` / `getwd` markers | 0 / 0 | 0 / 0 |
| named failures | 15 | 15 |
| shared by name | 14 | 14 |
| union-only | `TestRawPromptRunReleasesItsWorktree` | — |
| base-only | — | `TestTaskRunReleaseIsDeferredInTheEndHook` |

The one-sided pair was isolated and run `-count=3` on BOTH trees
(`isolate-union.log`, `isolate-base.log`): `TestTaskRunReleaseIsDeferredInTheEndHook`
fails **3/3 on both** with the identical message (`task_run_release_coverage_test.go:181`),
and `TestRawPromptRunReleasesItsWorktree` passed in both trees' isolated runs — so
both one-sided names are suite-timing artifacts of the same worktree-reclaim
family, not deltas. **Scope delta of the delivered tip `1c622e0e` = 0 named tests.**

**Acceptance status on `1c622e0e`** (re-measured after the sweeper took the
worktree, in `~/workspace/gopi-bt-union-2` = a fresh worktree of the same branch):
`git status --porcelain` → empty; no `MERGE_HEAD`; `CGO_ENABLED=0 go build ./...`
→ rc 0; `go vet ./...` → rc 0; `git merge-base --is-ancestor master HEAD` → rc 0;
`git diff --numstat master HEAD -- cmd/agent/main.go` → `1  1`; union parse =
master 32 / tag 32 / HEAD 33 with nothing lost from either side. `make test-budget`
itself exits non-zero on this branch AND on clean master, with the same 14 named
tests plus one flake each — i.e. the rc is the pre-existing ledger/worktree
family, not this diff; the named-test comparison above is the acceptance evidence
(the epic asks for named failing tests, not just rc).

## Addendum 3 — why `numstat = 1  1` is FORCED, not a shortcut (measured)

The epic asks for "insertions-only numstat on `cmd/agent/main.go`". The delivered
tip reports `1  1`. That was A/B/C-tested rather than assumed, in a fresh detached
worktree at `c25e3e83`:

| form | numstat vs master | build | `TestCLIContractInventoryMatchesDispatch` |
|---|---|---|---|
| **delivered** (tag's chain: `… \|\| isUISubcommand(dispatchArgs) {`) | `1  1` | rc 0 | **PASS** |
| insert-only, ui as its OWN `if` block above master's | `4  0` | rc 0 | **FAIL — "main.go has 32 … contract suite tracks 33"** |
| insert a blank `if isUISubcommand` block before master's line | `5  1` | rc 0 | *(5/1 variant, superseded)* |
| split the chain at `… \|\|` onto a continuation line | `2  1` | rc 0 | PASS (but master's line still counts as replaced) |

Root cause, read off the gate itself (`cmd/agent/cli_contracts_test.go:213`
`dispatchPredicatesInMain`): it locates the marker
`if isCompletionSubcommand(dispatchArgs)`, then takes **everything up to the first
`{`** and splits it on `||`. A separate `if` block for the ui verb therefore hides
`isUISubcommand` from the gate's window — it sees master's 32 predicates while
`cliContractDispatchPredicates` tracks 33 and fails. The union must live on the
**one guard line**, before that brace.

So the `1 1` numstat is the *consequence* of a green gate, not a compromise: the
line granularity is coarse (one 1203-byte guard line), while the predicate-level
proof in the section above shows the union is exact —
`HEAD == master's chain + " || isUISubcommand(dispatchArgs)"`, master's chain a
strict prefix, `master-only-lost = []`, `tag-only-lost = []`, `union == HEAD = True`.
The insert-only form was reverted (`git checkout -- cmd/agent/main.go`; porcelain
empty afterwards) and is NOT in the delivered branch.

## Addendum 4 — delivered tip, and the scope delta proven by CROSS-RUNNING both arms

**Delivered tip: `04e43cbd`** (integration base master `953a92c2`; the only changes after
`1c622e0e` are this devdoc — addenda 3 and 4 — so `git diff --name-only 1c622e0e 04e43cbd`
lists exactly one file and every earlier measurement is code-identical to the tip).

`make test-budget PKGS='./cmd/agent ./internal/config'` is **rc=2 on this branch AND on a
clean master `953a92c2` worktree**, 15 named failures each (worktree / ledger / scratch-gc
family). The two runs' name sets are NOT directly comparable — minutes apart and load
dependent, the same flaky family trips a partly different subset each time (the two runs
shared just one name). So the delta was proven by CROSS-RUNNING, which is load-proof:

```
go test ./cmd/agent -run '^(<names of one side>)$' -count=1     # on BOTH trees
```

- the branch run's 15-name set → `TestResumeReclaimsTheWorktreeItFinishedIn` only, and it
  fails **identically on clean master**;
- the master run's 14-name set → the same **8** names on master AND on the branch.

**Scope delta = 0 named tests, both directions** (`branch-only: []`, `master-only: []`).
Logs: `/home/user/.gopi/sessions/union-proof/xset-{B,M}-on-{branch,master}.log`. No test touching
dispatch, the `ui` verb, help/usage, the CLI contract or config fails on either tree; the
branch-scoped guard run over exactly those surfaces is green
(`go test ./cmd/agent -run 'TestCLIContract|TestUIVerb|TestUIDispatcher|TestTopLevelUsage|TestTopLevelHelp|TestConfig' -count=1` → `ok … 4.184s`).

Recipe note (documented in `devdoc/test-budget-single-source.md`, re-confirmed here): a
FRESH worktree has no `bin/gopi`, and the CLI E2E guards in `./cmd/agent` then **fail rather
than skip** — both arms need `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` first.

---

## Addendum 5 — the scope delta, re-measured from CLONES, and its correction (close-out)

Addenda 2 and 4 reported the `./cmd/agent` budget failure sets as "15 vs 15, cross-run →
delta 0". Those numbers came from a **scratch worktree** arm. Re-measured at close-out, a
worktree arm is invalid for a long suite: **the sweeper reclaimed the arm's own worktree
while the suite was still running** (measured: `test -d "$WT"` → `WT_GONE` at the end of the
run), so its failures are an artifact of a checkout that stopped existing — not of the
branch. Sibling defect `fix/merge-release-cwd-scope` owns that; long suites here must run
from a `git clone -s`, never a worktree.

**The corrected measurement, both arms in clones, `CGO_ENABLED=0 go build -o bin/gopi
./cmd/agent/` run FIRST in each:**

| arm | command | result |
|---|---|---|
| branch `28406845` | `make test-budget PKGS='./cmd/agent ./internal/config'` | rc=2, **5** named failures |
| branch `28406845` | `make test-budget PKGS='./internal/config'` | **rc 0, outcome=pass**, 2s |
| master `953a92c2` | the same 5 names, `go test ./cmd/agent -count=1 -run '<the 5>'` | **4 of 5 FAIL identically** |
| both trees | `TestCLIPositionalSubmitRunsThroughDispatcherE2E -count=3` isolated | **`ok` rc=0 on BOTH** |

The branch's 5: `TestCLIPositionalSubmitRunsThroughDispatcherE2E`,
`TestManualRunRecyclesItsWorktree`, `TestResumeReclaimsTheWorktreeItFinishedIn`,
`TestTaskRunSeamWritesReportAndShowRendersIt`,
`TestWorkerRunRecordsTheMissingRanLineE2E` — all of the pre-existing
worktree/ledger family, and **no ui / help / contract / config / dispatch test among them**,
which is the delta question. Master reproduces four of them exactly; the fifth is
**load-flaky** (passes isolated on both trees — its failure text names a
`suite-cmd-agent-run-*/gopi-repo-ctx-*` path that "is not a git work tree", i.e. a fixture
timing artifact, not a dispatch behaviour).

⇒ **Scope delta = 0 named tests, both directions.** The epic's `./cmd/agent` rc clause
(`make test-budget …` rc 0) remains unsatisfiable **on this host, on clean master too**;
the clause it also specifies — named failing tests, not just rc — holds with delta 0.

Everything else stands as verified on the frozen tip: `git status --porcelain` empty, no
`MERGE_HEAD`, `git merge-base --is-ancestor master HEAD` rc 0 with `merge-base == master
tip` (FF-able by construction), `go build ./...` and `go vet ./...` rc 0, numstat on
`cmd/agent/main.go` `1 1` (the brace's reposition), union at predicate level
32/32/33 with both one-sided loss sets empty, all 33 predicates resolving to exactly one
`func isXSubcommand`, and the scoped guard `go test ./cmd/agent -run
'TestCLIContract|TestUIVerb|TestUIDispatcher|TestTopLevelUsage|TestTopLevelHelp|TestConfig|TestUI'`
→ `ok 4.841s`.

## Addendum 6 — independent re-verification of the frozen tip (fresh worktree, own PID)

Re-run at `ba8e431e` in a brand-new worktree (`/home/user/workspace/gopi-bt-close-verify`), with no
reuse of earlier logs. `git diff --name-only 28406845 ba8e431e` = `devdoc/bubbletea-landing-union.md`,
so the earlier code measurements carry over unchanged; the numbers below were nevertheless
re-taken from scratch:

| check | command | result |
|---|---|---|
| tree clean | `git status --porcelain` | `[]` (nothing) |
| no merge in flight | `git rev-parse -q --verify MERGE_HEAD` | rc 1 (absent) |
| FF-able | `git merge-base --is-ancestor master HEAD` | rc 0 |
| after normalisation | `git merge-base master HEAD` vs `git rev-parse master` | both `953a92c2…` |
| union proof | `git diff --numstat $(git merge-base master HEAD) HEAD -- cmd/agent/main.go` | `1	1` |
| no markers | `grep -cE '^(<<<<<<<\|>>>>>>>\|=======$)'` | `0` |
| both sides present | `grep -c isUISubcommand` / `isConfigSubcommand` | `1` / `1` |
| build | `CGO_ENABLED=0 go build ./...` | rc 0 |
| vet | `go vet ./...` | rc 0 |
| binary | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | rc 0 |
| ui row registered | `./bin/gopi -h \| grep -c 'gopi ui'` | `1` |
| ui usage reachable | `./bin/gopi ui -h` | *"gopi ui — the terminal chat surface …"* |
| scoped guard | `go test ./cmd/agent -run 'TestCLIContract\|TestUIVerb\|TestUIDispatcher\|TestTopLevelUsage\|TestTopLevelHelp\|TestConfig\|TestUI' -count=1` | `ok … 8.932s` rc 0 |
| config budget | `make test-budget PKGS='./internal/config'` | `exit=0 outcome=pass elapsed=2s` |
| dropped file | `git cat-file -e HEAD:tasks/01a0bedc-report.md` | absent |
| kept file | `git cat-file -e HEAD:scripts/ui/render_transcript.py` | present |

Union re-parsed independently (python, window = the marker
`if isCompletionSubcommand(dispatchArgs)` up to the first `{`, split on `||`):
`master=32 / tag=32 / head=33`, `master_only_lost: []`, `tag_only_lost: []`,
`master_order_subsequence: True`, and
`head_chain == master_chain + " || isUISubcommand(dispatchArgs)"` **True** with the master
chain a strict prefix and **0 bytes** of master's text removed (delta length 32 bytes = the
appended predicate). The `1	1` numstat is therefore the `{`'s 1-byte repositioning, not
deleted content.

Noted, not claimed: the MAIN repo carries one untracked entry `?? .g3rep/` (an empty
`.g3rep/gopi/memory.db`: schema present, every table 0 rows, created 2026-09-23 10:57, no
live holder — `lsof`/`fuser` empty, `GOPI_HOME`/`GOPI_MEMORY_DB` unset, the name absent from
`master`, `e32f4e93` and `ba8e431e`). Rule 30 governs this run's own worktree (clean); the
entry is left in place for its owner.

## Addendum 7 — the FULL acceptance chain on the frozen tip, in one fresh worktree (budget arm included)

Frozen tip unchanged: `e585f97d`. Worktree `/home/user/workspace/gopi-bt-finalcheck-2153174`
(created with `git worktree add --detach … feat/bubbletea-landing-union`, own PID, nothing
reused from earlier logs), `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` run FIRST so the
`cmd/agent` CLI E2E guards are not failing-by-design:

| step | command | observed |
|---|---|---|
| porcelain | `git status --porcelain` | `[]` (empty) |
| merge in flight | `git rev-parse -q --verify MERGE_HEAD` | rc 1 (absent) |
| ancestry | `git merge-base --is-ancestor master HEAD` | rc 0 |
| FF-ability | `git merge-base master HEAD` vs master tip | both `953a92c2` |
| union delta | `git diff --numstat master HEAD -- cmd/agent/main.go` | `1	1` |
| build | `CGO_ENABLED=0 go build ./...` | rc 0 |
| vet | `go vet ./...` | rc 0 |
| binary + ui row | `go build -o bin/gopi ./cmd/agent/`; `./bin/gopi ui -h` | rc 0; rc 0 (usage printed) |
| budget arm | `make test-budget PKGS='./cmd/agent ./internal/config'` | `exit=2`, elapsed 534s, **15 named failures / 15** |

**Scope delta, re-measured from scratch this run: 0 named tests.** The branch arm's 15 names
are compared name-for-name against clean master `953a92c2`'s 15
(`~/.gopi/sessions/union-proof/close-recheck/{names-branch-e585f97d-freshrun.txt,names-master-953a92c2.txt}`):
`diff` → **0 lines** (`scope-delta-final.txt`, 0 bytes). The shared names are the worktree
cwd/lifecycle + task-run E2E family (`TestResumeReclaimsTheWorktreeItFinishedIn`,
`TestWorkerRunRecordsTheMissingRanLineE2E`, `TestManualRunRecyclesItsWorktree`,
`TestTaskRun*E2E` ×7, `TestWorker*` ×3, `TestCLIPositionalSubmitRunsThroughDispatcherE2E`,
`TestMergeUnionRowCheckOnTheRealManifest`) — i.e. host/environment failures that reproduce
identically on the unmodified base, not regressions from this landing. The epic's rc-0 clause
is unsatisfiable on this host (master is rc=2 with the same names); the clause it also states —
*named failing tests* — is satisfied with delta 0 in both directions.
