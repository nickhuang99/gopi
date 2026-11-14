<!-- doc-class: historical -->
# Task Report: verification of `fix/worker-unknown-flag` (independent re-check)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Branch: `fix/worker-unknown-flag` — worktree `~/workspace/gopi-workerbadflag`
Commit under verification: `9e6dfba7` (`fix(worker): refuse unknown argv words
before the loop starts (Rule 15)`)
Recovery: `git show fix/worker-unknown-flag:devdoc/worker-unknown-flag.md` /
`git show fix/worker-unknown-flag:cmd/agent/cli_tasks_worker_unknown_arg_test.go`

This session did **not** touch `cmd/agent/main.go`: the fix was already
implemented and committed on the branch (a prior worker's deliverable). The work
here was to re-verify it against the EPIC's acceptance criteria and to record
the evidence. No implementation line was changed.

## Completed

- **Located the deliverable.** `git worktree list` → the branch is checked out
  at `~/workspace/gopi-workerbadflag`; `git status --short` there is **empty**
  (everything committed); `git merge-base --is-ancestor 9e6dfba7 master` →
  **NOT merged into master** (`e1e8cf21`), i.e. the fix is on the branch only
  and PA's merge is still pending.
- **Re-verified every acceptance criterion on the real binary** (the built
  `bin/gopi` from this branch, run from the worktree with a scratch `GOPI_HOME`,
  `GOPI_AGENT_SPAWN_GUARD_OFF=1` so the loop start is observable):

  | argv | rc | loop started? | refusal names the word? |
  |---|---|---|---|
  | `worker --totally-bogus-flag-xyz --once` | **2** | no (`loop start` count 0) | yes |
  | `worker --once --nope` (unknown AFTER a recognized one) | **2** | no | yes (`--nope`) |
  | `worker --newflag=value --once` (a newer binary's spelling) | **2** | no | yes |
  | `worker straypositional` | **2** | no | yes |
  | `worker --once` | 0 | yes | — (no refusal printed) |
  | `worker -h` | 0 | **no** | — (usage head, zero side effects) |
  | `worker --once` **without** the guard bypass | **3** | no | — (agent-spawn guard, a *different* outcome) |

  Refusal text emitted verbatim:

  ```
  gopi worker: unexpected argument "--totally-bogus-flag-xyz" — refusing to start the worker loop
    accepted: -h/--help | --once | --worker-log-root <dir>
    if you passed an option a newer gopi supports, the deployed binary is STALE
    usage: `gopi worker -h`
  ```
  The three acceptance facts therefore hold by measurement: refused **loudly**,
  exit **2** (distinct from the guard's **3**, and from the run-failure `1`),
  the offending word **named**, and the loop **not started** for every rejected
  form. `--worker-log-root DIR`, `--once`, `-h/--help` still parse.
- **Tests + build green** (outputs below), including the table-driven
  `cmd/agent` acceptance test the briefing asked for
  (`TestWorkerUnknownArgumentIsRefused`, `TestWorkerAcceptedArgumentsParse`,
  `TestWorkerUnknownArgumentMessageNamesTheStaleBinaryCase`).
- **No-claim property.** The acceptance test asserts the seeded pending row is
  still `pending` after a refusal (`task show` = pending, no attribution line),
  and the pre-fix red proof recorded in `devdoc/worker-unknown-flag.md` shows the
  injected silent path actually claiming and running that row ("worker: run
  t-worker-arg-refused failed"). The refusal provably precedes the claim.
- **Gate comparison is against master, not an absolute.** `internal/stylegate`
  fails on this branch with **190** "ignored error-typed value" hits — and the
  identical **190** on master `e1e8cf21`. Pre-existing, untouched by this
  change; the branch must not be held to a green it inherits from nowhere.
  `internal/standards` (the six machine gates) → **ok** on the branch, which
  covers the `baseline.go` line re-key the diff carries.

## Not Done (honest limits)

- **Not merged.** PA's merge of `fix/worker-unknown-flag` is still outstanding;
  this session did not merge (Rule 6e — never merge your own branch).
- **`~/bin/gopi-worker-up.sh` (the installed launcher) is stale relative to
  `scripts/gopi-worker-up.sh`** — it executes the worker with no
  `--worker-log-root`, so output attribution is OFF on that deployment path.
  Correctly filed by the previous session as a separate (deployment, not
  argv-parsing) task rather than folded into this fix; it is still open, and it
  is the live instance of the same silent-degradation class.
- Only the `worker` verb was audited. `merger` already has its hint path; no
  other verb was swept in this task.

## Verification

```
Build:  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/            → build_ok
Tests:  go test ./cmd/agent/ -count=1                              → ok (153.4s)
        go test ./cmd/agent -run 'TestWorkerUnknownArgument|TestWorkerAcceptedArguments'
                                                                   → ok (9.3s)
Gates:  go test ./internal/standards/ -count=1                     → ok (8.9s)
        go test ./internal/stylegate/ -count=1                     → FAIL 190 hits
                                                                     = FAIL 190 hits
                                                                       on master @e1e8cf21
                                                                       (pre-existing)
Binary: the rc/loop-start table above, measured on the branch binary
Worktree: git status --short → clean;  commit 9e6dfba7 present in the main repo
```

## Deliverable location (Rule 6d)

- Branch: `fix/worker-unknown-flag` (worktree `~/workspace/gopi-workerbadflag`;
  `9e6dfba7` is already a ref in the main repo `~/work/GoPi` — worktrees share
  the object store, so no push is needed).
- Recovery: `git show fix/worker-unknown-flag:devdoc/worker-unknown-flag-verification.md`,
  `git show fix/worker-unknown-flag:devdoc/worker-unknown-flag.md`.
- The worktree may only be removed after PA merges.

## Re-verification after master moved (2026-09-12, tip 0244f3d1)

The branch was built and re-measured after master advanced past `e1e8cf21`
(`git diff --stat e1e8cf21..master -- cmd/agent/main.go internal/standards/baseline.go`
→ empty, so nothing the fix touches moved):

- `git merge-base`-based `git merge-tree fix/worker-unknown-flag master` → no conflicts.
- `CGO_ENABLED=0 go build ./cmd/agent/` → OK.
- `go test ./cmd/agent -run TestWorker` → ok (44.8s); `go test ./internal/standards/` → ok (15.4s).
- Real binary `/tmp/gopi-uwf`, `GOPI_AGENT_SPAWN_GUARD_OFF=1`:

  | argv | rc | loop start lines |
  |---|---|---|
  | `--totally-bogus-flag-xyz --once` | 2 | 0 |
  | `--newflag=value --once` | 2 | 0 |
  | `stray-positional --once` | 2 | 0 |
  | `--once` | 0 | 1 |
  | `-h` / `--help` | 0 | 0 |
  | `--worker-log-root /tmp/uwf-logs --once` | 0 | 1 |
  | `--once` without the bypass (guard path) | 3 | 0 |

  Refusal text names the word: `gopi worker: unexpected argument "<word>" — refusing to start the worker loop`.
  Exit codes are distinct: 2 = usage-class refusal, 3 = the spawn guard's own outcome.

## Closing acceptance run (branch tip `f8856d00`, master `0244f3d1`)

Built fresh from the branch tip (`CGO_ENABLED=0 go build -o /tmp/gopi-final ./cmd/agent/`)
and re-measured on the real binary; `GOPI_AGENT_SPAWN_GUARD_OFF=1`,
scratch `GOPI_HOME` + scratch log root:

```
argv=[--totally-bogus-flag-xyz --once]              rc=2 loop_start=0  → names "--totally-bogus-flag-xyz"
argv=[--newflag=value --once]                       rc=2 loop_start=0  → names "--newflag=value"
argv=[stray-positional --once]                      rc=2 loop_start=0  → names "stray-positional"
argv=[--once]                                       rc=0 loop_start=1
argv=[-h]                                           rc=0 loop_start=0
argv=[--help]                                       rc=0 loop_start=0
argv=[--worker-log-root <tmpdir> --once]            rc=0 loop_start=1
guard path (no GOPI_AGENT_SPAWN_GUARD_OFF)          rc=3 loop_start=0
```

Gate re-run at the same tip: `go test ./cmd/agent -run 'TestWorkerUnknownArgument|TestWorkerAcceptedArguments|TestWorkerHelp' -count=1`
→ **ok** (11.3s); `go test ./internal/standards/ -count=1` → **ok** (11.7s);
`git status --porcelain` in the worktree → **empty** (0 lines).

Status: fix verified and complete on the branch; the epic's remaining step is
PA's merge (Rule 6e — this session does not merge its own branch). The stale
installed launcher is tracked separately as task `01a09587`.
