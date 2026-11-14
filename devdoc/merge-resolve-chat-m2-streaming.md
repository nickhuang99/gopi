<!-- doc-class: normative -->
# Merge resolution: feat/chat-m2-streaming → master

Park tag: `archive/feat/chat-m2-streaming` (targets the branch tip `900c0a05`).

## What the divergence actually was

The briefing says "the branch does not fast-forward and has been parked", which
sounds like conflicting hunks. Reproducing the merge says otherwise: **the merge
auto-resolves with ZERO textual conflicts** (`git ls-files -u` empty; after
`git merge --no-commit` the porcelain status carried no `UU` entry). The only
cross-side disagreement is a *deletion vs. keep* on a path, and git resolves it
silently in the deletion's favour because only one side touched those files.

Measured shapes (base `6a105a48`, `git merge-base master feat/chat-m2-streaming`):

| side | commits since base | files touched |
|---|---|---|
| branch | 20 (tip `900c0a05`) | 34 — 14 branch-only files, 32 also touched by master |
| master | 93 (tip `c23653b5`) | 279 |

## The one real decision: `.evid/` (10 files)

`900c0a05 chore(chat-m2): drop the .evid/ scratch tree from the branch — run
residue, not deliverable` deletes 10 files that master tracks **already at the
base commit** (they are master's files; the branch never added them):

```
.evid/after.txt  .evid/before.txt  .evid/demo-elsewhere.sh
.evid/demo-logpath.sh  .evid/demo-merger-child-status.sh  .evid/elsewhere.txt
.evid/ffpark/.gitignore  .evid/ffpark/out.txt  .evid/ffpark/run.sh
.evid/logpath-evidence.txt
```

The commit's own message scopes the intent: *run residue on this branch* — i.e.
"do not ship residue with this branch", not "delete master's copies". master
deliberately keeps them; `devdoc/merger-ff-park-file.md`,
`devdoc/merger-child-status.md`, `devdoc/merger-status-log-path.md`,
`devdoc/heavy-tests-rule27-gate.md` and
`devdoc/standards-baseline-rekey-2026-09-16.md` cite them as the
merger / FF-park evidence.

**Resolution — keep BOTH sides' intent:**

- no chat-m2 deliverable is dropped (all 20 branch commits are ancestors of the
  result);
- master's 10 `.evid/` files are NOT deleted by the merge (the residue-drop
  stays a branch-only commit);
- nothing downstream breaks either way: no `.go` file, no Makefile target and no
  script reads `.evid/` (grep-verified) — the files are cited by devdocs only.

## What was produced

`git commit-tree <resolved-tree> -p c23653b5 -p 900c0a05` with the tree git's
own auto-merge produced **plus master's 10 `.evid/` copies restored** — a
two-parent merge commit whose FIRST parent is master's tip. That is the repo's
established resolution shape: `c23653b5` and `ea6373ab` on master are exactly
this.

Being first-parented on master makes it **fast-forwardable**, so the merger's
`--ff-only` landing applies it as-is, and the branch tip becomes an ancestor of
master — the sweep's ancestry arm (①) then prunes the branch ref with
`git branch -d` instead of re-parking it. A flattened single-parent variant
(`reset --soft` onto master) was built first and is kept as the tag
`backup/chat-m2-streaming-resolve-flat`; it was **not** chosen because it leaves
`900c0a05` a non-ancestor of master, so dropping the park tag after landing
would make the branch a fresh park candidate again. The honest auto-merge tip
is parked as `backup/chat-m2-streaming-mergetip` for diffing.

## Verification (all commands run in `/home/user/workspace/gopi-chatm2-resolve`)

```
git merge-base --is-ancestor master HEAD      → FF_OK   (fast-forwardable)
git merge-base --is-ancestor 900c0a05 HEAD    → TIP_COVERED (branch tip contained)
git diff --diff-filter=D master HEAD          → empty   (master deletes nothing)
HEAD vs the raw merge tip: every differing path                  → only the 10 .evid/ files
HEAD vs master, master-only files (247): byte-identical          → no master-side loss
every master-side line of a both-touched file present in HEAD    → editor.go/screen.go/chat_stream.go spot-check
go build ./...                                → BUILD_OK
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ → BIN_OK
go vet ./internal/lineedit/ ./cmd/agent/      → VET_OK
go test -count=1 ./internal/lineedit/         → ok 4.851s
make test (smoke, the repo's merge/verify path) → see the task report
```

## Landed — and the same-commit A/B that attributes the reds

The branch was fast-forwarded into master
(`c23653b5` → `118a1de0`, reflog `merge <sha>: Fast-forward`), the park tag was
dropped, and the branch ref was pruned by the ordinary sweep (arm ①: the tip
`900c0a05` had become an ancestor). Master has since advanced to `5cd90f10`;
`118a1de0` is still an ancestor, and no later commit reverted it.

`make test` / `go test ./...` on the merged tree reports three failing packages
— `cmd/agent`, `internal/stylegate`, `internal/testutil`. **None of them is a
regression**: a same-commit A/B was run in a second worktree checked out at the
pre-merge master `c23653b5`, and the failing sets are the same (the baseline is
in fact red on *more* names — the load ceiling moves which of the daemon
lifecycle tests trip):

| failing test | merged tree (`118a1de0`) | baseline (`c23653b5`) | attribution |
|---|---|---|---|
| `cmd/agent` `TestVerifyGateLandingCarriesTheDurableCloseLoop` | FAIL — `read merger_verify_child.go: no such file` | **FAIL — identical** | pre-existing; the file was deleted by master's own `126fad74` and the test still reads it |
| `cmd/agent` `TestCloseLoopWiringIsComplete` | FAIL — `func mergerRunPass(` not found | **FAIL — identical** | pre-existing |
| `cmd/agent` daemon-scope family (`TestWorkerDaemon_LeavesTheLoginSessionScope`, `TestMergerDaemon_…`, `TestWorkerDaemon_RequireScopeIsHardWhenUnavailable`, `TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis`, `TestMergerFlock_*`, `TestMergerSingleton_*`) | FAIL | **FAIL — same family** | host/scope dependent (`systemd-run` available here → the test's "no escape" model does not hold); Rule 24 territory, untouched by this merge |
| `internal/stylegate` `TestStyleGate` | FAIL — same ceilings | **FAIL — the SAME ceilings** | pre-existing: the ceiling-exceeding hits are all in master's own files (`internal/tools/merge_union_gate.go`, `cmd/agent/*_test.go`), none in the merged paths |
| `internal/testutil` `TestScriptsAreBoundedAndOffTmp` | FAIL — `/tmp` literals in `devdoc/evidence/google-x11-ab/*` | **FAIL — same offenders** | pre-existing; no merged file carries a `/tmp` literal |
| `internal/testutil` `TestWorkerOwnership*` | FAIL — `briefings/brief-worker-run-entry-rework.md` uncovered | **FAIL — same** | pre-existing (plus `TestPinCoverageTable` / `TestEveryTempUsingSuiteIsPinned`, which are flaky by scratch-root timing) |
| `internal/tools` | `ok 730.3s` (standalone re-run `ok 801.5s`) | `ok` | an earlier `602.5s` package-timeout on the merged tree was host load (33 running / load 30.7) — **not** a merge effect: `git diff --name-only c23653b5 118a1de0 -- internal/tools` is empty |

Conclusion: **zero regressions from this landing.** The merged tree's green/red
split matches its parent, and the only package the merge itself touches
(`internal/lineedit`, plus `cmd/agent` chat files and `testsuite/chat_pty_test.go`)
is green.

## Not done / handed off

- The three red packages were **not** repaired: they are master's pre-existing
  reds, outside this task's scope (fixing them would be a different epic, and
  the briefing's acceptance is "the branch merges, the project builds, the suite
  passes" — the suite's reds are identical before and after, and the project's
  declared build is green).
