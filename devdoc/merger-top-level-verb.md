<!-- doc-class: historical -->
# Task Report: `gopi merger` is a FIRST-LEVEL command (peer of `gopi worker`)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Branch: `fix/merger-top-level-verb` (worktree `~/workspace/gopi-merger-top`)

## The report (attachment1: the `gopi -h` excerpt the operator pasted)

```
  gopi task merger [--every 5m] [--once] [--repo PATH] [--no-branch-scan]
                   [--db PATH]
      MERGE DAEMON (server-only — the old 'task serve', renamed; 'task
      serve' still works as an alias): branch merge + conflict resolve
      ONLY — never claims or runs tasks (that is gopi worker's job).
```

The operator: *"merger is under 'task' implying it is a sub command of task,
actually merger is a service and runs independently of any task just like
worker which is first command of gopi, this is like a regression bug, because
I recall merger is used to be immediate after gopi."*

## Git archaeology — the premise, checked before the fix (Rule: verify, don't assume)

| probe | result |
|---|---|
| `git log --all -S'isServeSubcommand' --oneline` | **empty** |
| `git log --all -S'CliVerbServe' --oneline` | **empty** |
| birth of the daemon | `3612bfa3` — born `gopi task serve` (P2 dispatcher) |
| rename | `8ef21fda` (serve/worker-split, 2026-09-03) → `gopi task merger` |
| `gopi worker` in the SAME split | `60ab0a3e` `isWorkerSubcommand` — made **top-level** |

An empty `-S` (pickaxe) result for a would-be top-level predicate is a
**refutation**, not a removal: no top-level merge-daemon verb ever existed.
So the "regression" is a **half-memory** — but the report is *right about the
defect*: the split promoted the worker out of the group and left the merger
in it, and the merger is exactly as task-independent as the worker (it sweeps
the `settings.json` `"repos"` keys; whether a ledger row tracks the branch is
irrelevant). Fixing the **inconsistency** is the deliverable; the history is
reported honestly rather than fabricated into a lost verb.

## Completed

1. **Top-level dispatch** — `cmd/agent/main.go`:
   - `isMergerSubcommand(args)` wired into the dispatch chain directly after
     `isWorkerSubcommand`.
   - **P0-1 parity**: the argv scan runs BEFORE any parse/lock, so
     `gopi merger -h` / `--help` prints `mergerUsageHead` and `os.Exit(0)`
     with **zero side-effects** (no `serve.lock`, no pass). This is the exact
     bug the worker had (`gopi worker -h` burned live tasks — memory #1230);
     the merger's analogue would be taking the flock and merging.
   - It reuses the existing `parseTasksArgs` parser (flag set is identical),
     only setting `opts.invokedAs = mergerProgTopLevel` so diagnostics name
     the verb actually typed.
2. **`CliVerbMerger`** (`cmd/agent/cli_verbs.go`) — named constant (Rule 1),
   with the git-history rationale and the "SERVICE, not a ledger verb" doc.
   No bare `"merger"` literal was introduced in production code.
3. **Verb-aware diagnostics** (`cmd/agent/cli_tasks.go`) —
   `tasksOptions.invokedAs` + `mergerProg()` (falls back to
   `mergerProgFamily`); every merger stderr line now names the typed verb:
   ```
   $ gopi merger --every bogus        → gopi merger: invalid --every "bogus" …
   $ gopi task merger --every bogus   → gopi task merger: invalid --every "bogus" …
   ```
   `acquireServeLock`'s message de-hardcoded from `"another gopi task serve is
   already running"` to `"another merge daemon …"`.
4. **Help catalogue** (`internal/config/help.go`) — a new **"Resident
   SERVICES"** block listing `gopi worker` and `gopi merger` FIRST (each
   explained as a process a host starts from its own service unit), then
   "Tools & group sub-commands". The old `gopi task merger` block moved down
   and is relabelled **DEPRECATED ALIAS of the top-level `gopi merger`**;
   `tasksUsage` carries the same note.
5. **Completion** (`cmd/agent/cli_completion.go`) — `CliVerbMerger` added to
   `topLevelCommandNames`; `gopi completion bash` now offers `merger`.
6. **Doc bug fixed in passing**: the usage block advertised
   `[--repo PATH]`, a flag `parseTasksArgs` rejects (`unknown flag --repo`) —
   sweep targets come from the settings `"repos"` table. Dropped from both
   usage surfaces.
7. **Tests** — new `cmd/agent/merger_top_level_e2e_test.go` (real binary,
   hermetic `GOPI_HOME`) + contract/completion/usage suites extended (task 3).
8. **`internal/standards/baseline.go` re-pinned (2 lines)** — inserting the
   `mergerUsageHead` block (main.go) and the longer merger usage block
   (cli_tasks.go) shifted two machine-gate baselines, which are keyed by exact
   `file:line`. Both are the **same sites with the same reasons**, only moved:
   `cmd/agent/main.go:509 → :601` (Rule 16 error-wrap, the documented
   `mentionsError` false positive — the prose comment was updated to say so)
   and `cmd/agent/cli_tasks.go:1140 → :1160` (Rule 18 else-after-return).
   The list did **not** grow (no new permit), and `go test
   ./internal/standards/` is green again — the ratchet stays downward.

## Not Done

- The daemon was **not moved**, only its CLI face was promoted: `runTaskMerger`
  stays in `cli_tasks.go` and the `task merger` / `task serve` group forms stay
  wired as aliases (deliberate — running deployments and systemd units launch
  them; removing them is a separate deprecation task with an operator notice).
- No systemd unit / installer was touched (`gopi merger` is available to any
  unit that wants to switch; nothing was rewritten under the operator's feet).
- `--repo` was **not** reimplemented: the flag never worked, and the sweep list
  is settings-driven by design (architecture-fix-v2). Only the stale doc line
  was removed.

## Verification

Build / vet:

```
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → exit 0 ("BUILD_OK")
go vet ./cmd/agent/                               → clean
go test ./cmd/agent/ -run 'Merger|Usage|Completion|Contract' → ok 27.9s
go test ./...                                     → see "Full suite" below
```

Live binary evidence (the real `bin/gopi`, isolated homes):

```
$ ./bin/gopi merger -h          # top-level contract; EXIT=0
GoPi merger — the branch-merge DAEMON (server-side service).
TOP-LEVEL BECAUSE IT IS A SERVICE: … the peer of 'gopi worker' …
Deprecated aliases (still accepted for running deployments):
  gopi task merger …   # the daemon's pre-promotion home
  gopi task serve …    # the daemon's pre-split name

$ GOPI_HOME=$tmp ./bin/gopi merger -h >/dev/null ; ls $tmp/.gopi
→ no serve.lock created, "NO LOCK (GOOD)"     (P0-1 zero-side-effect proof)

$ GOPI_HOME=$sandbox ./bin/gopi --settings … merger --once      # a REAL pass
[serve] [adopt] scanned=1 adopted=0 …
[merger] [/home/user/mtl-repo.RZTk] branch feat/live-top-level (merged)
[merger] [/home/user/mtl-repo.RZTk] merged=1
[merger] [rebuild] skip: no build contract for … (settings repos table)
[merger] --once: single pass done                         EXIT=0
→ branches before: feat/live-top-level master
→ branches after : master           (the seeded branch landed: 41bc1c7 …work)

$ ./bin/gopi task merger -h / task serve -h   → still dispatch (family usage), EXIT=0
$ ./bin/gopi task merger --once               → still merges (alias path healthy)
$ ./bin/gopi completion bash | grep merger    → merger (offered by tab completion)
$ ./bin/gopi -h
  line 235: Resident SERVICES — top-level because each is an independent process …
  line 244:   gopi merger [--every 5m] [--once] [--no-branch-scan] [--db PATH]
  line 347:   gopi task merger … DEPRECATED ALIAS of the top-level 'gopi merger'
```

E2E suite (`cmd/agent/merger_top_level_e2e_test.go`, real binary, hermetic
sandbox — 4 cases): A1 `-h`/`--help` prints the head, exits 0, **takes no
lock**, changes no branch; A2 `merger --once` lands the seeded branch in
master; A3 `task merger --once` + `task serve --once` still run a pass;
A4 the top-level catalogue advertises `gopi merger` **outside** the task group
while still documenting the deprecated family form.

## Full suite

```
go test ./cmd/agent/ -count=1       → ok 242.0s   (the promotion's home package)
go test ./internal/config/ -count=1  → ok   0.3s
go test ./internal/standards/ -count=1 → ok  10.9s
go test ./internal/tools/ -count=1 -timeout 20m → ok 635.2s
go test ./internal/stylegate/ -count=1 → FAIL — PRE-EXISTING on master, not mine
go test ./testsuite/                 → ok (from the earlier ./... run)
```

Two failures were investigated before being attributed — both are **outside
this branch**:

- **`internal/stylegate`** — `unchecked-close: 1 test hit, ceiling 0` at
  `internal/agent/cov_agent_cr_test.go:909`. That file is not mine; I checked
  **master itself** in the main repo (`go test ./internal/stylegate/` at
  `e45e379e`) and it fails the same way, plus `ignored-error: 185 > ceiling
  182`. The **live repo is red on this gate independent of my change**.
- **`internal/tools`** — an earlier `go test ./...` run hit `-test.timeout=10m`
  inside this package. Re-run in isolation with a 20m cap: **`ok 635.2s`** —
  i.e. the package legitimately takes >10m on this box (shared with several
  other `./...` sweeps), so the earlier failure was the package's own default
  timeout under load, **not a defect** — and nothing in this branch touches
  `internal/tools`.

### Merge with master (mid-task)

master advanced while this ran, so the branch was merged up
(`b04cdfae`). master's own `cli_tasks.go` edits had re-keyed the **same two
gate baselines** at different line numbers, which conflicted with my re-key:
resolution keeps **one entry per site at the merged line number** —
`cmd/agent/main.go:607` (Rule 16) and `cmd/agent/cli_tasks.go:1181` (Rule 18).
`go test ./internal/standards/` green after the union.

## Deliverable location

Branch `fix/merger-top-level-verb` in the shared repo (worktree
`~/workspace/gopi-merger-top`). Recovery: `git show
fix/merger-top-level-verb:devdoc/merger-top-level-verb.md`.
