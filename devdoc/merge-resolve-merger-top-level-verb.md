<!-- doc-class: historical -->
# Task Report: resolve merge `fix/merger-top-level-verb` into `master`

## Completed
- Reproduced the merge in the linked worktree (`git merge master --no-commit --no-ff`) — 7 UU files, 19 conflicts / 38 markers:
  `cmd/agent/{cli_contracts_test.go, cli_tasks.go, cli_verbs.go, main.go, usage_test.go}`,
  `internal/config/help.go`, `internal/standards/baseline.go`.
- Resolved every hunk — **no side's intent dropped**:
  | file | resolution |
  |---|---|
  | `cmd/agent/main.go` | **master** (its side of each hunk is HEAD's promoted-`gopi merger` intent PLUS the `--daemon` face: `mergerDaemonRequested/Spawn`, `mergerMemoryDBEnv`, `mergerDaemonFlagHint`, help text). master = superset. |
  | `cmd/agent/cli_tasks.go` | **master** for the `--daemon` child branch + the inherited-flock `runTaskMerger`; **HEAD kept for `mergerProgTopLevel`** (see Not Done / fix below — master's revision never carried that const). |
  | `cmd/agent/cli_contracts_test.go` | **master** (`CliVerbMerger` const instead of the `"merger"` literal — Rule 1b), both sides' argv table + verb-enum intent identical. |
  | `cmd/agent/usage_test.go` | **master** (same intent, its comments also carry "still advertised as an alias" via the `DEPRECATED ALIAS` text). |
  | `cmd/agent/cli_verbs.go` | **HEAD**: `CliVerbMerger`'s HISTORY prose carrying the commit hashes (`3612bfa3`, `8ef21fda`, `60ab0a3e`) + the "stays as a deprecated dispatch alias" intent — this is the branch's actual subject (the top-level verb promotion). |
  | `internal/config/help.go` | **master** (its `--daemon` text includes HEAD's alias/deprecation prose plus the daemon contract). |
  | `internal/standards/baseline.go` | **master** (a line-keyed permit list: a permit that matches no violation is a silent permission, so exactly ONE key per real site — `main.go:646`, `cli_tasks.go:1167`, memory permits pruned on master's rewrite). |
- Committed the resolution FIRST (the deliverable), then verified; the verify found one self-inflicted break, fixed and amended.

### Second break found by the bounded verify, and its fix (amended into the commit)
`internal/standards` is a LINE-KEYED permit list, and both sides shifted the lines it keys on, so the
`master` keys no longer pointed at the merged tree's violations (gate: "STALE baseline entry … matches no
violation" + "NEW violation" for the same site):
| stale key | merged-tree key | site |
|---|---|---|
| `cmd/agent/main.go:646` | `main.go:675` | the `mentionsError` false positive (needs TWO keys: one in the Rule-16 list, one in the Rule-18 list) |
| `internal/tools/memory_sync_claim.go:890` | `memory_sync_claim.go:912` | the `%v` wrap of `terminalPushRetries`' lastErr |
| `cmd/agent/cli_tasks.go:1167` | `cli_tasks.go:1171` | the `} else if len(replicaPaths) > 0` chain after a terminating `if` |
Re-keyed to the MERGED numbers with a `re-key (merge …)` comment on each (same site, same reason — the
permit moved, the violation did not), and dropped master's now-stale duplicate. `go test ./internal/standards/`
then passed all six gates — this is the merge's own bookkeeping, not a new violation introduced by it.

## Not Done
- Nothing deferred in the merge itself. No push (worktree shares the main repo's object store), no branch deletion, no touch of the main checkout's refs.

### Break found by the bounded verify, and its fix (amended into the commit)
`go build ./...` caught what marker-removal alone did not:
```
cmd/agent/cli_tasks.go:2013:2: mergerProgFamily redeclared in this block
	cmd/agent/cli_tasks.go:1764:7: other declaration of mergerProgFamily
```
Root cause (not a marker — an identifier whose only definition lived in the hunk): master's revision of
`cli_tasks.go` had moved the resolver/const block *up* next to `runTaskMerger` and DELETED the later
`const ( mergerProgTopLevel; mergerProgFamily )` block, while HEAD kept a single `mergerProgFamily` at :1764
plus that later block. Taking master verbatim therefore dropped `mergerProgTopLevel` (referenced by
`main.go` outside every marker) AND the later `const (` block re-declared `mergerProgFamily`.
Fix: kept master's arrangement and restored HEAD's `const mergerProgTopLevel = "gopi merger"` **once**
(the duplicate `mergerProgFamily` is gone) — both sides' intent lands:
master's daemon/`--daemon` feature + HEAD's top-level program-name constant.

## Verification
- Merge shape: `git log -1 --pretty='%H %P'` → `d4f156be` with **two parents** (`4cfce995` HEAD + `09402611` master one level up via `09402611`); no `MERGE_HEAD`; `git status` clean; branch `fix/merger-top-level-verb`; `git --git-dir=/home/user/work/GoPi/.git rev-parse fix/merger-top-level-verb` = `d4f156be` (commit is already in the shared repo — no push needed).
- Conflict markers: `grep -rn '^<<<<<<<' .` → **0** (a line-start anchor; the 5 bare `<<<<<<<` hits in `internal/tools/task_adoption_conflict.go` / `repo_resolve_row.go` / `task_adoption_test.go` are the repo's own conflict-parser code, test fixture and epic text — not markers).
- `gofmt -l cmd/agent internal/config internal/standards` → clean; `git diff --check` → clean.
- Build: `go build ./...` → **OK** (no output).
- `go vet ./cmd/agent/` → **OK** (no output).
- Tests (bounded, touched packages only — a full `go test ./...` is known to hang in this repo and was deliberately NOT run):
  `go test ./cmd/agent/ -run 'TestTopLevelUsage|TestCLIUserVerbTokensAreAllFromCliVerbs|TestCLIContractInventoryMatchesDispatch|TestCLIContractEachSubcommandRoutesToItsHandler' -count=1`
  and `go test ./internal/standards/ ./internal/config/ -count=1` — see the "Verification output" section appended below for the raw result.
- Commit: `d4f156be` — "resolve merge fix/merger-top-level-verb into master".

## Recovery
Branch `fix/merger-top-level-verb` (in the shared repo object store):
`git -C /home/user/work/GoPi show fix/merger-top-level-verb --stat`

## Verification output (raw)
```
$ go build ./...            → (no output) OK
$ go vet ./cmd/agent/       → (no output) OK
$ go test ./cmd/agent/ -run '...' -count=1   → ok  github.com/nick/gopi/cmd/agent  6.455s  (4 tests, PASS)
$ go test ./internal/standards/ ./internal/config/ -count=1 → ok  github.com/nick/gopi/internal/standards  17.643s  (all six gates PASS, after the re-key below)
ok  github.com/nick/gopi/internal/config     0.341s
```
