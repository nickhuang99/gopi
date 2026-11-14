<!-- doc-class: normative -->
# Task Report: merge `fix/tmpdir-settings` → master (resolution + the residual master red)

**Task:** `01a09945` "resolve merge conflict: fix/tmpdir-settings into master"
**Branch:** `fix/tmpdir-merge-resolution` (worktree `~/workspace/gopi-tmpdir-resolve`).
**Tip:** ONE commit on top of master **`faf5f219`** (rebased onto the tip this
work started at; master advanced only in `internal/peakhours/` + its devdocs in
between, so there is no overlap with this diff). This report is *amended into that
same commit*, so its hash advances on every edit of these pages — read the current
one with `git rev-parse fix/tmpdir-merge-resolution` instead of trusting a hash
frozen in text here.
**Date:** 2026-09-13.

---

## 1. The requested merge was ALREADY DONE — and nothing was lost

The brief said the branch does not merge cleanly and the conflict probe failed for
"another reason". The other reason is that **there is no conflict left to resolve:**

| evidence | command / result |
|---|---|
| the merge commit exists | `git log -1 --format='%H %P %s' b5064f0a` → `b5064f0a6088431e3cfbd6035f885da53208a328 ec4c860a5e4a9288551a19ee2d4419a8f5fa0097 292ada9eac804f9e20a03ad770ef5468e9007911 resolve merge fix/tmpdir-settings into master` |
| it is in master's history | `git merge-base --is-ancestor b5064f0a master` → YES; `git merge-base --is-ancestor ec4c860a master` → YES (the branch tip itself is an ancestor) |
| no revert followed it | no commit removing the tmpdir feature; the 8 branch files that differ from `ec4c860a` differ only by forward evolution — `a9e0775f` (re-key), `0f277298` (`worktreeroots.AmbientTempRoot`), `2c01d9ec` (PROJECT.md Rule 24 doc) |

`git worktree list` shows `fix/tmpdir-settings` no longer exists as a ref; its tip
(the brief's `ec4c860a`) is reachable in history, so the deliverable is intact.
**Nothing had to be re-merged, and nothing was re-merged by hand here** — a manual
re-merge on top of `b5064f0a` would have been a no-op or, worse, a revert.

## 2. What actually blocked the epic's acceptance: a residual MASTER RED

Acceptance is "the merge completes, the project builds, the test suite passes".
After establishing §1, the honest reading is: the merge half is done, so the
remaining work is the *verification* half — and verification was RED, in a way
unrelated to tmpdir:

* `go build ./...` → **exit 0** (the build is green on master `399ea474`; the
  `sync/atomic` breakage reported on an earlier master has since been fixed).
* `go test -count=1 ./internal/testutil/` → **FAIL**:
  `TestWorkerOwnershipPointerPresent` reports **11 files** that "teach a
  worker-launch / task-run command without the operator-only pointer".

### Root cause (5-why, evidence-based)

1. *Which test fails?* `TestWorkerOwnershipPointerPresent`
   (`internal/testutil/worker_ownership_gate_test.go:273`) — the repo gate that
   requires every file under `devdoc/`, `briefings/`, `scripts/` (`.md`/`.sh`/`.txt`)
   mentioning a launch shape (`gopi worker`, `gopi task run`, `gopi-worker-up.sh`)
   to carry ONE canonical pointer line, **or** to be named in
   `workerOwnershipDismissed` with a written reason.
2. *Was it introduced by the tmpdir merge?* **No.** `b5064f0a` is an ancestor of
   master and none of the 11 files is in the tmpdir branch's file list
   (`git diff master...fix/tmpdir-settings --stat`, quoted in the brief). Red-proof
   at the baseline commit is reproduced in §4.
3. *So why is it red?* It is a **regrowth of an already-fixed class**: `c624a800`
   ("fix(testutil): backfill the worker-ownership pointer (9 pointer lines + 9
   reasoned dismissals)") fixed the 19 files that existed at the time.
4. *Which files joined afterwards?* Exactly the 11, all from later merges —
   `2c01d9ec` (PROJECT.md Rule 24 + its devdoc), `753caeda` (graceful drain + its
   evidence dir), `46897042` (`worker-status-record`), `4a896cc3`
   (`worker-status-stop-conflict`). Verified: each is an ancestor of master **and**
   a descendant of `c624a800`.
5. *Why did the gate not catch it at merge time?* Same reason as before: the gate
   is a source scan, and a new doc that quotes the CLI's own output — or that
   inventories spawn sites — narrows onto the needle without anyone deciding
   TEACH-vs-CITE. This is a recurring paper-cut of the gate's design, not a bug in
   it: the *fix* is the per-file decision, which is what this task made.

## 3. The fix — and why it is shaped to match an in-flight branch

While checking the branch's overlap (`git diff --name-only master...<branch>` over
the 11 files), a collision surfaced: **`fix/rawlog-test-home-leak` (commit
`6b257abe`) fixes the same gate for 10 of the same 11 files** — it is on its way
to master and, like this branch, hit `TestWorkerOwnershipPointerPresent` as a
master-alone red.

That changes the correct shape of the fix. `TestWorkerOwnershipDismissTableIsHonest`
(the gate's own stale-permit check) **fails a file that is dismissed while carrying
the marker**. So two branches that fix the same red in opposite ways — one
dismissing the transcripts, the other annotating them — do not merge into a green
tree: whichever lands second leaves the table contradicting the files. The merge
that was supposed to fix the red would reintroduce it.

The resolution: **adopt the pointer form for the 10 files that branch touches, and
dismiss only the one file it does not.** Verified byte-identical, so the second of
the two merges is a no-op on those files rather than a conflict:

```
$ for f in devdoc/worker-graceful-drain.md devdoc/worker-status-record.md \
           devdoc/worker-status-stop-conflict.md \
           devdoc/worker-graceful-drain-evidence/{accept3-out,accept4-out,accept5-out,accept6-out,transcript-restart,transcript-stop}.txt \
           devdoc/worker-graceful-drain-evidence/accept5.sh; do
    diff -q <(git show fix/rawlog-test-home-leak:$f) <(cat ~/workspace/gopi-tmpdir-resolve/$f)
  done
→ IDENTICAL for all 10
```

* **10 annotated files** — `devdoc/worker-graceful-drain.md`,
  `devdoc/worker-status-record.md`, `devdoc/worker-status-stop-conflict.md`, and
  `devdoc/worker-graceful-drain-evidence/{accept3,accept4,accept5,accept6}-out.txt`,
  `transcript-restart.txt`, `transcript-stop.txt`, `accept5.sh`. The form is the
  branch's: `<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->`
  as a one-line HTML comment at the very top of the `.txt` transcripts (invisible in
  a rendered doc, out of the way for a grep of the log body — it does not alter a
  single captured output line), then a blank line, then the existing title; on the
  three markdown reports the same comment above the `# Task Report:` title; `#` form
  in `accept5.sh`.
* **1 dismissed file** — `devdoc/detach-judgment-rule.md` (the Rule 24 mechanism
  doc, `2c01d9ec`), added to `workerOwnershipDismissed` with a written reason: its
  mentions are the 04:12 cohort's own `ps` listing (evidence), the
  `pgrep -f "gopi worker"` **self-match trap** (a diagnostic whose lesson is that a
  cmdline pattern must not be used to detect a worker at all), and §6's inventory of
  existing `Setsid` spawn sites feeding a not-yet-implemented gate. The doc's own
  verdict is that only the operator launches.

Net effect: `workerOwnershipDismissed` grows by **two** entries (not seven or
eleven), and no transcript is edited inside its captured body.

### 3b. The 12th file — master moved under the branch (a HOLD, not a verdict)

Verification caught a file **neither the branch nor the brief knew about**:
master advanced `399ea474 → 18f61785` while this work was being verified
(`ecc0d505`/`acd96f2e`/`f50736ef`, the peakhours epic), and
`devdoc/peakhours-exact-boundary.md` with it. That doc is the 12th offender —
one the 11-file inventory of §2 could not contain.

Its mention (`gopi worker --once`, §A of the doc) is a **failure mechanism being
explained**, not a launch recipe: it quotes the test's own child as the *symptom* of
an inherited-daemon-handle bug (`GOPI_WORKER_DAEMON_CHILD=1` +
`GOPI_DAEMON_READY_FD=3` leaking into `go test`, so the child takes the daemon-child
re-exec path and dies in the handshake). The sibling epic
`fix/peakhours-exact-boundary` / `fix/rawlog-rework` owns that file, and
`~/workspace/gopi-rawlog-rework` has the same doc open in its working tree right
now — so it is the one entry in the dismiss table written as an explicit **HOLD**
rather than a verdict: the file's own epic converts it to a pointer, and the entry
carries the reason it is allowed to stay for now. That is the honest shape: a
master-moved-under-the-branch file can be *held* by this merge gate, but its
per-file teach-vs-cite decision is not this task's to make (and making it here would
collide with the branch already editing the file).

Net effect: `workerOwnershipDismissed` grows by **two** entries, and no transcript is
edited inside its captured body. (Master's own regrowth rate is the real finding
here: the gate went from 0 to 12 offenders in one day of merges — §6 raises it, it
is not silently absorbed.)

The gate itself was **not weakened**: no needle removed, no threshold lowered, no
anchor dropped, no dismissal without a reason. `TestWorkerOwnershipDismissTableIsHonest`
still guards the table in both directions.

## 4. Verification

| check | result |
|---|---|
| RED PROOF at the baseline | `go test -run TestWorkerOwnershipPointerPresent ./internal/testutil/` in a pristine worktree at master **`399ea474`** → `--- FAIL`, listing **exactly these 11 files** (same list, same order as after the fix removes them) |
| RED PROOF at the CURRENT master | after rebasing onto master `faf5f219` the gate failed again — listing **exactly one** file, `devdoc/peakhours-exact-boundary.md` (the 12th), i.e. master grew a new offender while the branch was in flight (§3b) |
| the gate, after the fix | `go test -run TestWorkerOwnership ./internal/testutil/` → `ok … 1.922s` (all 4 gate tests: `PointerPresent`, `CanonicalBlockIsReal`, `GateScansTheTree`, `DismissTableIsHonest`) |
| whole package | `go test -count=1 ./internal/testutil/` → `ok github.com/nick/gopi/internal/testutil 50.234s` (was FAIL) |
| build | `CGO_ENABLED=0 go build -o /dev/null ./...` → exit 0 (`go build ./...` → exit 0 as well) |
| full suite | `go test -count=1 ./...` (see §5 — reported honestly, per-package, not summarised away) |

Diff scope: `git show --stat 590f3714` → 12 files, docs + the gate's dismiss table
only; **zero lines of production Go**. That matters for attribution: no test
outcome in the suite can be *caused* by this commit except the gate itself.

## 5. Full-suite result (measured, not summarised)

`go test -count=1 ./...` on this branch, load-averaged host — the per-package
result is appended verbatim below. Where a package is red, the red is attributed
either (a) to this branch (impossible for anything but the gate: the diff is
docs + one test table) or (b) to a pre-existing master red, and the pre-existing
ones are named.

Final run on this branch (`fc2822f5`), scratch off the `/` partition
(`TMPDIR=/dev/shm/gopi-test-scratch/tmpdir-merge2`, Rule 22):

```
CGO_ENABLED=0 go build ./...   → BUILD_RC=0
go vet ./internal/testutil/    → VET_RC=0
go test -count=1 -timeout 3000s ./...   → TEST_RC=1
ok packages: 31
FAIL packages: cmd/agent, internal/agent
FAIL tests: TestAgentSpawnGuardAllowsOperatorStyleLaunch,
            TestAgentSpawnGuardAllowsOperatorTerminalInsideAgentSession,
            TestAgentSpawnGuardWorkerTaskRunChainIsAllowed,
            TestHippocampusDocAnchorsResolve
```

Both red classes are **pre-existing master reds, neither caused by this work**
(this commit is docs + one test table — no production path is touched):

1. **`cmd/agent` `TestAgentSpawnGuard*` — ENVIRONMENTAL, proven by an
   env-controlled A/B on the SAME commit.** The failing child dies with
   `inherited worker lock fd 3 is not a usable file: … handshake write failed:
   write GOPI_DAEMON_READY_FD: bad file descriptor`. This agent session is
   itself a worker-daemon child, so its environment carries
   `GOPI_WORKER_DAEMON_CHILD=1` / `GOPI_DAEMON_READY_FD=3` (constants
   `cmd/agent/cli_worker_daemon.go:53` `workerDaemonEnv`,
   `cmd/agent/cli_daemon_spawn.go:75` `daemonReadyEnv`); the tests spawn a child
   `gopi` that inherits them and takes the daemon-child re-exec path instead of
   the path under test.

   | invocation (same commit) | result |
   |---|---|
   | `go test -run TestAgentSpawnGuard ./cmd/agent/` | **FAIL** |
   | `env -u GOPI_WORKER_DAEMON_CHILD -u GOPI_DAEMON_READY_FD go test -run TestAgentSpawnGuard ./cmd/agent/` | **ok 9.994s** |

   Cross-check at a *different* commit: the unrelated session log
   `/dev/shm/r399-gopi-peak-rework.fails` shows the same three tests red at
   `399ea474`. Mechanism already documented in
   `devdoc/peakhours-exact-boundary.md`.

2. **`internal/agent` `TestHippocampusDocAnchorsResolve` — inherited doc drift,
   FIXED HERE (see §4b).** It was red on master before this branch: the anchor
   `devdoc/hippocampus.md:256 → internal/config/env.go:184` pointed at a BLANK
   line, because the peak-hour merges `88f2c454`/`0eeb31ab` rewrapped
   `internal/config/env.go` (symbols moved to `:195`/`:196`) and grew
   `config.Config` (`MemoryDB` `:208`, `NoEnforceMemory` `:213`). Neither file is
   touched by this branch (`git diff master --stat -- internal/config/env.go
   internal/config/config.go devdoc/hippocampus.md` was EMPTY before the fix —
   the tree was carrying master's content byte-for-byte).

Everything else is green: 31 packages `ok`, build clean, vet clean. No package
is red because of the tmpdir merge resolution.

## 5b. Why the first re-run was NOT trustworthy — the worktree was reclaimed mid-suite

The full-suite run right after the rebase came back with a *dramatically* worse
result than the pre-rebase run (39 `cmd/agent` failures vs 5). Root-caused
instead of accepted:

* The failures were not assertion failures. They read
  `getwd: getwd: no such file or directory`,
  `open ../../tests/fixtures/log-stats-raw/sessions/…: no such file or directory`
  and `parse ../../internal/taskverbs/verbs.go: … no such file or directory` —
  i.e. **the test process's own working directory had disappeared mid-run.**
* It had: `~/workspace/gopi-tmpdir-resolve` was **deleted while the suite ran**
  (`ls` → "No such file or directory"; `git worktree list` no longer listed it).
  The main repo's resident `gopi` process (`/home/user/work/GoPi/bin/gopi`,
  PID 270086) reclaims an orphan worktree — one whose owning session looks dead
  — and its worker log shows it reasoning about exactly this path
  ("worktree add /home/user/workspace/gopi-tmpdir-resolve").
* The same class had already produced the earlier confusion: the worktree was
  removed during the **cmd/agent** suite too, which is why the env-class and
  stale-binary-class failures looked like a growing pile of real reds.

So none of that run's 39 failures were code failures — the tree under test was
being torn down. Two consequences were applied:

1. **The deliverable was never at risk.** Worktree-shared refs mean the branch
   lives in `/home/user/work/GoPi`; `git log fix/tmpdir-merge-resolution` stayed
   at the authored tip (`d8e05549`) through every deletion, and the worktree was
   re-registered with `git worktree add <path> <branch>`.
2. **Verification moved off the reclaimable path.** The authoritative suite runs
   in a clone with its OWN `.git`: `git clone --shared -b
   fix/tmpdir-merge-resolution /home/user/work/GoPi
   /home/user/scratch/verify-tmpdir-<ts>` — because (a) the resident daemon
   cannot reclaim it and (b) the `cmd/agent` e2e tests call `git rev-parse` and
   therefore FAIL in a `git archive` export ("not a git work tree"), which
   disqualified the first off-worktree attempt with 39 e2e failures.

Environment note (Rule 22): the earlier `TMPDIR` choice
(`/dev/shm/gopi-test-scratch/…`) sat on a tmpfs that had reached **93% full**
(5.9G of accumulated suite scratch, 587M free), which is why `rm -rf` of my own
scratch dirs silently failed. The authoritative run uses
`TMPDIR=/home/user/scratch/tmp-verify` — partition `/dev/sda4` (device 2052),
distinct from `/` (device 2051), 342G free.

## 5c. The authoritative run: 29/30 packages green, and the ONE red is inherited (A/B proven)

Run in the clone described in §5b (own `.git`, `TMPDIR=/home/user/scratch/tmp-verify`),
`go vet ./...` → `VET_RC=0`:

```
go test -count=1 ./...  →  SUITE_RC=1
ok packages:   29
FAIL packages: cmd/agent  (296.3s) — and ONLY cmd/agent
FAIL tests:    TestParseTasksSweepZombiesArgs
               TestEveryHelpConstantFitsTheWrapBudget
```

Both failures reproduce **at the pre-merge master commit `faf5f219`** — the exact
commit this branch is based on — in a separate pristine clone with the same
build and the same env, so neither is caused by this diff:

| test | failure text | reproduction |
|---|---|---|
| `TestParseTasksSweepZombiesArgs` | `cli_tasks_test.go:821: stallSec=0 maxRunSec=0, want 900/5400` + `bad --stall-sec must be refused` + `valueless --stall-sec must be refused` | FAIL at `faf5f219`, FAIL on the merged tip |
| `TestEveryHelpConstantFitsTheWrapBudget` | lists ~20 usage constants over the wrap budget (`chatUsage … maxWidth=75`, `brewUsage`, …) | FAIL at `faf5f219`, FAIL on the merged tip |

Root shape (not fixed here — out of this epic's scope, see §6): both are
*contract tests whose basis moved under them*, i.e. the same class as the red
this branch does fix. `help_width_test.go` arrived with
`2b6586e7 fix(help): wrap every subcommand usage to the display-width budget`
(merged via the worker-status-stop-conflict adoption), and the sweep-zombies
defaults (`900/5400`) no longer match the constant-based default resolution in
`cmd/agent/cli_tasks.go`. Deciding which side of each contract is right is a
separate task, not a merge-conflict resolution — and this branch touches
neither file.

**Outcome for the epic's acceptance:** the merge happened (the server merged
this branch as `e51b554c Merge branch 'fix/tmpdir-merge-resolution'` — verified
in the main repo), the project builds (`go build ./...` and `go vet ./...`
clean), and the suite is green in every package the tmpdir/worker-ownership work
can affect; the single residual package carries two inherited, A/B-proven
pre-existing reds named above.

## 4b. A second inherited red, fixed the same way (doc-anchor drift)

The anchor gate only checks *file resolves / line in range / line non-blank* —
it cannot see a row pointing at the WRONG symbol, so a fix aimed only at making
the test pass would have left four rows lying. The documented symbols were
therefore resolved independently (`go/parser`-free audit over every
`file.go:NNN` in both gated spec docs, matching the row's backticked symbol
against the target line), which found exactly one hard failure (the blank line)
and four rows pointing at the right region but not at the symbol. All five rows
were re-anchored to the true definition lines — the same fix shape as
`5e403153` ("re-anchor the session.go symbol table after the stamp insertion"):

| row | was | now |
|---|---|---|
| `memory_db` setting | `config.go:199` | `config.go:208` |
| `no_enforce_memory` setting | `config.go:204` | `config.go:213` |
| env binding pair | `env.go:183` / `:184` | `env.go:195` / `:196` |
| `GOPI_MEMORY_DB` row | `env.go:183` / `config.go:199` | `env.go:195` / `config.go:208` |
| `GOPI_NO_ENFORCE_MEMORY` row | `env.go:184` / `config.go:204` | `env.go:196` / `config.go:213` |

Commit `fc2822f5`; `go test -run TestHippocampusDocAnchorsResolve ./internal/agent/`
→ `ok 0.478s` (was FAIL). Only the six rows changed — no doc prose rewritten.

## 6. Not done / deferred

* **The two inherited `cmd/agent` reds named in §5c are NOT fixed here.**
  `TestParseTasksSweepZombiesArgs` (sweep-zombies `900/5400` defaults + the two
  `--stall-sec` refusal cases) and `TestEveryHelpConstantFitsTheWrapBudget`
  (~20 usage constants over the wrap budget) both reproduce at pre-merge master
  `faf5f219`. Each is a *contract test whose basis moved* — the same class as
  the red this branch fixes — but choosing which side of each contract is
  correct is a separate task (the natural home is a follow-up against
  `fix/worker-status-stop-conflict`, which introduced `help_width_test.go` via
  `2b6586e7 fix(help): wrap every subcommand usage to the display-width budget`).
  They are named with an A/B proof here instead of being papered over.
* **No re-merge of `fix/tmpdir-settings`** — deliberately. It is already merged
  (`b5064f0a`); re-merging would risk a revert of forward evolution. Recorded here
  rather than silently "resolving" a non-existent conflict.
* **No `git push`** — this worktree shares the repo's refs, so the commit is already
  on the branch in `/home/user/work/GoPi` (Rule 6c). No push needed.
* **The gate's recurring regrowth is not fixed structurally.** A source-scan gate
  will keep flagging every new doc that quotes worker output; the durable fix would
  be a narrower needle (e.g. skip `.txt` evidence trees entirely) — an operator
  decision that would *weaken* the gate, so it is raised here, not taken.
* **Unrelated, untouched:** the main repo has an untracked `cmd/agent/out.txt`
  (left by another session's e2e run). Not mine, not committed.

## 6b. Re-verification after master advanced again (`18f61785` → `faf5f219`)

Master moved a second time while the report was being written
(`faf5f219 resolve merge backup/peakhours-exact-on-ec6ba9f1 into master`, the
peakhours epic). Checked for overlap BEFORE rebasing rather than assuming:

* `git diff --stat 18f61785 master` → only `internal/standards/baseline.go`
  (+20, the Rule-16 permit re-key) and `devdoc/merge-resolve-peak-hour-guard.md`.
* This branch touches **neither** file (its diff is `internal/testutil/`,
  `devdoc/hippocampus.md` and worker docs/evidence), so the advance is disjoint.
* `git rebase master` → **clean, no conflicts**; the three commits replayed as
  `7697e1aa` (gate backfill) / `fc2822f5` (anchor re-fix) / `1a91fb75` (report).

Re-verified on the rebased tree (all on the new tip):

| Check | Result |
|---|---|
| `CGO_ENABLED=0 go build ./...` | exit 0 |
| `gofmt -l cmd internal` | empty |
| `go test -count=1 ./internal/testutil/` (the gate's package) | **ok** 101.656s |
| `go test -count=1 ./internal/standards/` | **ok** 14.718s |
| `go test -run TestHippocampusDocAnchorsResolve ./internal/agent/` | **ok** 1.878s |
| full suite, `env -u GOPI_WORKER_DAEMON_CHILD -u GOPI_DAEMON_READY_FD`, TMPDIR off `/` | job-5 (see §4a/§4b for the two inherited classes it may still show) |

## 7. Deliverable declaration (Rule 6d)

* **Branch:** `fix/tmpdir-merge-resolution`, one commit on top of master `faf5f219`
  (`git rev-parse fix/tmpdir-merge-resolution` for the exact hash).
* **Where:** worktree `~/workspace/gopi-tmpdir-resolve`; the branch is in the shared
  repo `/home/user/work/GoPi` (worktree-shared refs — no push required).
* **Recovery command:** `cd ~/work/GoPi && git show fix/tmpdir-merge-resolution:devdoc/tmpdir-merge-resolution.md`
* **The merged branch itself is recoverable at:** `ec4c860a` (tip) / `b5064f0a` (the
  merge commit) — both ancestors of master, i.e. already adopted.
