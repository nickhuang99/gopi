<!-- doc-class: historical -->
# The task-session-cwd E2E fixture: an unborn HEAD made the walk measure its own scaffolding

**Task**: 01a0df40-5617-738c-8fdb-55fa6f1b03c8 · **Branch**: `task/01a0df405617738c8fdb55fa6f1b03c8`
**File**: `cmd/agent/task_session_cwd_e2e_test.go` · **Class**: record (a measured defect + its pin)

## The symptom, and why it pointed at the wrong place

`TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` was the ONE name red in BOTH
trees (branch and base) of task `01a0de63` — a shared red, which is the shape of a defect in
the test, not in the product. The failure the walk reported was:

```
task_session_cwd_e2e_test.go:134: the task output log must contain "(task t-session-cwd-e2e:"
    ... cannot resolve the ground for task t-session-cwd-e2e in repo <tmp>/003/repo:
        git worktree add <tmp>/004/gopi-tsessioncwde2e -b task/tsessioncwde2e in <tmp>/003/repo:
        exit status 255: Preparing worktree (new branch 'task/tsessioncwde2e')
        fatal: Not a valid object name: 'HEAD'. — refusing to start the run ...
task_session_cwd_e2e_test.go:148: no session JSONL under <gopiHome>/.gopi/sessions/t-session-cwd-e2e
```

Two assertions DOWNSTREAM of the real cause. The cause is three lines above them, in the
fixture: `cwdProbeE2ERepo` ran `git init` + `config` + `checkout -b master`, wrote `a.txt`,
and **never committed** — so the repository's HEAD was unborn.

## Root cause (5-why)

1. The run refused to start. 2. Because `git worktree add <ground> -b task/<id>` could not
resolve HEAD. 3. Because the fixture repository has no commit, so HEAD points at an unborn
branch — not a valid object name. 4. Because `cwdProbeE2ERepo` built a repository that only
*looked* like one: enough for the submit path's `git rev-parse` verification, not enough for
the ground creation that follows. 5. Because a fixture's sufficiency was never pinned to the
operations the product actually performs on it — the test's expectation of the fixture was
written against an earlier product (before epic `worker-creates-child-ground`, which is what
introduced the `worktree add` the row's run now does).

**The product is right.** A row pointing at a repository it cannot branch in is a row that
cannot be executed, and refusing with a cause + remedy
(`TaskRunGroundRefusal`: "refusing to start the run in the repo's main checkout; remedy:
re-point the row at a live git work tree") is the correct behaviour. Nothing in the product
was changed.

## The fix

1. **The fixture commits.** `git add a.txt` + `git commit -q -m fixture` after the write, so
   HEAD resolves and the ground can be branched off it.
2. **The shape pin** — `cwdProbeAssertResolvableHEAD`, called at the END of the fixture:
   `git rev-parse --verify HEAD` in the built repo; a nonzero status fails the test **there**,
   naming the cause (the fixture committed nothing), the consequence (the product will
   refuse, and this walk will measure the fixture) and the remedy (keep the add+commit pair
   and the pin — never delete them to make a red go away).

   - The probe asks the SAME question the product asks (`rev-parse --verify HEAD`, status =
     verdict), not a text match on git's message. **Deliberately not locale-pinned**: git
     inherits `LANG` from the host, and `internal/tools/bash.go` intentionally does not
     override it, so a pin on the English wording (`unambiguous argument` vs
     `Not a valid object name` — both were observed on this host, in different git versions'
     wording) would be a contract with the operator's locale, not with git's behaviour.
   - It runs at the fixture, so the red is one line that names the fixture — not three
     assertions later at a log read.

3. **The reference is the run's own record, not a path the test re-derives.** With HEAD
   resolvable the run proceeds and creates the row's GROUND — a linked worktree of the row's
   repo (epic `worker-creates-child-ground`, 2026-09-22) — so the child's `pwd` result is the
   ground, **not** the row's main checkout. The old assertion pinned equality with `repo` and
   so asserted pre-ground behaviour: with only the commit added, the walk failed at
   `_test.go:211` with `the pwd result was "<tmp>/004/gopi-tsessioncwde2e"` — a red against a
   CORRECT product (measured, commit-only probe). The walk now judges the pwd result against
   the directory the SPAWN SEAM recorded for this run in the run's own log
   (`executor cwd → <dir> (task <id>: <source>)`, marker taken from the product's
   `taskCwdResolvedFmt` — Rule 13: the shape lives in the const, tests included). An absent
   record is a FAILURE, never a fallback to a guessed path (Rule 15).

   The two assertions kept, and they are the epic's claim unchanged:
   - ① the child's pwd is NOT the wrapper's session dir (or under it) — the defect the walk
     exists for, and the assertion that must never be relaxed;
   - ② the child's pwd IS the directory the run resolved for it.

## Evidence (measured on this host)

| step | command (see the session's `evidence/`) | result |
|---|---|---|
| pre-fix red | the walk as it stood (unborn HEAD) | FAIL at `_test.go:134/148` (log + JSONL), cause three lines away in the fixture |
| commit only (probe) | commit added, old assertion kept | FAIL at `_test.go:161` — pwd was the GROUND, not the main checkout: the old reference was stale, not the product |
| red control | commit line removed, **pin kept** | FAIL at `_test.go:136` — "fixture repo … has NO resolvable HEAD" (0.03s, at the fixture) |
| green ×3 | the full walk, 3 consecutive reps | PASS ×3 (`ok github.com/nick/gopi/cmd/agent`) |

A note on WHY the pin exists rather than trusting the reviewer: this same fixture was fixed
once before on another branch (`4877da59`, "unborn HEAD" is in its subject) — a fix that
lives in another branch's tree is not a fix in this one, and without a pin at the fixture the
next rebase can drop the commit line silently and re-arm the shared red with no signal at the
site.

## Not done / boundaries

- Only this walk's fixture and its reference assertion were touched. The three sibling
  fixture fixes in `4877da59` (strict `--repo` default via `SandboxRepoContextInFixtureRepo`,
  the retry-rewritten exit code read, the shared `cwdProbeResolvedChildDir` helper) live on
  `feat/...` of task `01a0deb7` and are NOT in this branch — this branch carries the minimal
  fix its own epic named. A merge of both will conflict in this file; the resolution is the
  union (this file's fixture + pin, the sibling's other two fixes), and neither side's
  discriminating power is weakened by the other.
- The walk still skips when `bin/gopi` is absent (`testutil.GopiBin`), unchanged.
