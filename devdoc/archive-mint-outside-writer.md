<!-- doc-class: normative -->
# Which path minted the ten reason-less `archive/*` tags

Task `01a12dff-e091-7569-834b-5b378041b010`, branch
`task/01a12dffe0917569834b5b378041b010`. Rule 5 report + Rule 7 devdoc in one
document.

## §1 The measurement

Ten `archive/task/<id>-20261012` tags exist with no companion
`refs/gopi/archive-reason/<branch>` record. They are loose refs, and their mtimes
settle the shape of the event:

```
$ find "$(git rev-parse --git-common-dir)/refs/tags" -type f -printf '%TY-%Tm-%TdT%TH:%TM:%TS %P\n' | sort
2026-10-12T09:45:09.6502029900 archive/task/01a12d22dac07bdea9f895926cdc64ea
2026-10-12T09:46:14.2296427110 archive/task/01a12d2ca3a67c5db2a1fa03081027d5-20261012
… (ten rows, through) …
2026-10-12T09:46:14.3696415770 archive/task/01a12d62828e78e7bbc31fa110998565-20261012
```

Two facts: the ten span **0.12 s** (one shell loop, not ten considered decisions),
and they arrive **one minute after** a tag whose name is
`ArchiveTagReasoned`'s exact output — i.e. the reasoned writer ran, and then
something else ran beside it.

The `-20261012` suffix is the TELL, and it is not a private convention: it is
**PROJECT.md Rule 9 step 2's own prescription**, as that rule read until this change:

```
2. **败者分支归档清理**：`git tag archive/<name>-<date>` + `git branch -D <name>` + …
```

So the ten were minted by an agent **following gopi's shipped instruction** — and
that instruction was rendered into its context from four places at once:
`GOPI.md`, the pushed-branch visibility hint (`cmd/agent/cli_tasks.go`), the
branch-delete refusal (`internal/tools/branch_delete_guard.go`), and
`scripts/post-merge-cleanup.sh`.

## §2 The named suspect, cleared by measurement

The briefing named `scripts/post-merge-cleanup.sh:123`'s
`TAG_NAME="${TAG_PREFIX}/${BRANCH}-$(date +%Y%m%d)"` as the likely mint. It has the
shape — but it could not have fired, and the reason is structural rather than
lucky. The mint is guarded by `[ "$AHEAD" -gt 0 ]`, `AHEAD` is exactly
`git rev-list --count master.."$BRANCH"`, and the script only ever cleans branches
**already merged into master**. Every branch it can touch therefore has `AHEAD=0`
and the arm is DEAD.

That is not a reading of the source; §3 runs it. The consequence is that a fix
applied only to the script would have covered **nothing**, while leaving the path
that actually minted the ten untouched.

## §3 The two commands that settle the dead arm

```bash
# 1. every tagged tip is an ancestor of master with nothing ahead of it
$ git merge-base --is-ancestor "$tag" master && echo YES      # → YES
$ git rev-list --count "master..$tag"                          # → 0

# 2. run master's own script against a fixture repo and count the mints
$ bash "$(git show master:scripts/post-merge-cleanup.sh)" a/branch test/bare
   🗑  分支 a/branch 已删除 (ahead=0)
   🗑  分支 test/bare 已删除 (ahead=0)
   old-script archive tags minted: 0
```

Zero tags. The `--dry-run` line says it in words: `[dry-run] 将删除: a/branch
(ahead=0, tag=no)`.

## §4 The regression guard, and what it does NOT cover

`internal/standards/archive_mint_gate.go` reports a **create-form** `git tag` in
four in-scope carriers: `GOPI.md`, `scripts/`, and the production Go trees
`internal/` + `cmd/` (test files excluded — a fixture exists to construct the
shape). `devdoc/`, `tasks/` and `briefings/` are OUT on purpose: history must stay
able to quote the pre-fix command, or the record of this defect could not print its
own subject.

It reports nothing for the forms this tree uses to READ the namespace
(`git tag -l 'archive/*'`, `git tag --contains <tip>`, and a bare `git tag`, which
prints the list) and nothing for a `#`/`//` comment that documents the shape.
**The baseline is empty by construction** — a permit here would be a permanent
exemption for a silent mint, which is the defect.

Two limits are stated rather than hidden (Rule 15):

- a shape inside a multi-line `/* … */` block **is** reported — a text scan cannot
  tell a block comment from code;
- a string literal SPLIT across two lines is **not** seen, and an `eval`-assembled
  command is invisible. No text scan catches every spelling of a shell command.

**One defect in the guard was caught by its own proof, and it is worth recording.**
The first version read Go files through the AST (literals only). That is DEAD in the
real scan: a text gate's traversal builds `parsed{rel, src}` with no file set, so
`ast.Inspect` over a nil file returns nothing — the unit test passed while the
production scan reported zero Go hits. The predicate is now text-based for every
carrier, with `//` line comments stripped for `.go`, and the red proof is run
against the **real tree**, not only against synthetic sources.

## §5 The funnel

`gopi task archive <branch> --reason <code>` is the ONE writer: it writes
`archive/<branch>` and the companion `refs/gopi/archive-reason/<branch>` in one
operation. All four carriers now teach that verb:

| carrier | before | after |
|---|---|---|
| `GOPI.md` Rule 9 | `git tag archive/<name>-<date>` | `gopi task archive <branch> --reason <code>` |
| `cmd/agent/cli_tasks.go` hint | `archive it (git tag archive/<name> + branch -D)` | `archive it (gopi task archive <name> --reason <code>)` |
| `internal/tools/branch_delete_guard.go` | `git tag archive/<branch> <branch>` | `gopi task archive <branch> --reason <code>` |
| `scripts/post-merge-cleanup.sh` | `git tag -f "$TAG_NAME" "$BRANCH"` | `"$GOPI_BIN" task archive … --override-completed` |

The script deliberately does **not** degrade to a bare `git tag` when gopi is
missing: it warns and continues with the branch deletion (the commits remain in
master's history either way), because "minted a tag with no reason" is the outcome
being eliminated.

No new script, daemon, config key or file was created (Rule 35); `gopi task
archive` already existed, and the gate reuses the existing standards scan
framework. The eleven existing tags were left untouched — a tag is the durable half
of a decision, `gopi task archive --check` already reports them, and restyling them
was out of scope.

## §6 Verification (Rule 5)

- Build: `go build ./...` → OK; `gofmt -l internal/standards/` → clean (apart from
  the pre-existing `generated_doc_deps_test.go`).
- Tests: `go test ./internal/standards/ -run TestArchiveMintGate -count=1` → ok.
- Tests: `go test ./internal/standards/ -run TestStandardGates -count=1` → the
  `archive-why` gate green (`0 total hits, 0 baselined, 0 escaped, 0 new`, verified
  with `-v`); the FAILs that remain are all pre-existing (§7, measured on master).
- Tests: `make test-budget PKGS='./internal/standards ./internal/tools ./cmd/agent'`
  → 9 red tests across three packages, **each reproduced red on a clean detached
  master worktree** (§7 carries the table and the master evidence).
- Ran: `go test ./internal/standards/ -run TestStandardGates` with the three
  pre-fix carriers temporarily restored → exactly 3 violations, at their real
  locations (`GOPI.md:320`, `cmd/agent/cli_tasks.go:3655`,
  `internal/tools/branch_delete_guard.go:241`); after restoring the fix, zero.
- Ran: `$BIN task archive a/branch --reason superseded --detail "…"` (binary built
  fresh with `CGO_ENABLED=0 go build -o …/bin/gopi ./cmd/agent/`) against a fixture
  repo and a fixture ledger → `archived a/branch as archive/a/branch`, and
  `git for-each-ref refs/tags/archive/` + `refs/gopi/archive-reason/` afterwards
  show BOTH refs; the reason blob carries reason/branch/repo/actor/task/at/detail.
- Ran: `GOPI_BIN=$BIN bash scripts/post-merge-cleanup.sh a/branch test/bare` on a
  fixture repo → both branches deleted, `tags minted: 0` (both `ahead=0`).
- Ran: `bash "$(git show master:scripts/post-merge-cleanup.sh)" a/branch test/bare`
  → `0` tags minted, both branches `ahead=0`.

Evidence directory (this run's session, Rule 37 — never a git-tracked path):
`/home/user/.gopi/sessions/01a12dff-e091-7569-834b-5b378041b010/artifacts/`
(`real-run2.sh` and its fixture repos, `redproof-backup/`, `bin/gopi`).

The master baseline worktree used for §7 was a detached linked worktree created at
`…/artifacts/mw2` — i.e. OUTSIDE the declared `worktree_root`, which gopi's bash
guard permits for `git worktree add` but refuses for `git worktree remove`; it was
removed with the documented escape (`GOPI_WORKTREE_ROOT=<session> git worktree
remove --force …`, exit 0) and `git worktree list` no longer lists it. The
asymmetry is filed as its own bug rather than silently worked around.

## §7 Not Done (honest) — and the pre-existing red, measured

**Nine tests are red, and every one of them is red on master too.** The
classification is not a reading of the diff: a detached `master` worktree
(`git worktree add --detach …/artifacts/mw2 master` → `d7fb65344`, whose `GOPI.md`
still carries the pre-fix line) was run with the same commands, and each test below
fails there with the identical message. So this branch introduces **no new
failure**, and none of them is this task's scope.

| test | verdict |
|---|---|
| `standards/TestStandardGates/TestGate2TestEnvRedProof` | red on master (also fails STANDALONE-clean: `-run TestGate2TestEnvRedProof` alone → `ok`, inside the family → red, so the family interaction is the defect) |
| `standards/TestStandardGateDataReport` | red on master |
| `standards/TestRepoHygieneRealTreeIsBaselined` | red on master — `internal/tools/task_finish_cleanup.go [oversize] 262502 > 262144` |
| `standards/TestRepoHygieneCensusReDerivesTheTable` | red on master — the same row is un-baselined |
| `tools/TestCovFinishCleanupLinkedWorktreeRemovedBranchKept` | red on master — "the published-anchor edge is BROKEN — no generated-document edge was discovered at all" |
| `tools/TestCovFinishCleanupClientArmNoPeerRecyclesWorktree` | red on master |
| `tools/TestFinishCleanupClientDirtyStillDelivers` | red on master (same published-anchor edge) |
| `agent/TestNotifyScriptAgreesWithTheRegistryConstants` | red on master — "notify.sh does not build the slot name from `${SLOT_PREFIX}${i}${SLOT_SUFFIX}`" |
| `cmd/agent` package | red because of that one test |

Two consequences, both stated rather than smoothed over:

- **This is a filing task, not a fixing task.** Nine red tests on master is the
  scenario Rule 41 exists for: the evidence (the master worktree path, the command,
  the message) is recorded here and in the session's `artifacts/`, and it belongs to
  whoever owns those gates — not to this branch, whose diff touches none of them.
- **The one thing this report can assert about its own work is the direction of the
  red**: `archive-why` reports `0 total hits, 0 baselined, 0 escaped, 0 new` on the
  real tree (the gate ran — verified with `-v`), and with the pre-fix carriers
  restored it reported exactly 3 violations at their real locations. The gate is
  neither silently skipped nor vacuously green.

Also not done, deliberately:

- **The eleven existing tags remain reason-less.** `gopi task archive --check`
  already lists them; inventing a reason for a decision nobody remembers would be
  fabricating evidence, which is worse than the gap.
- **`scripts/post-merge-cleanup.sh`'s dead arm was left in place** (funnelled, not
  deleted). Removing the `AHEAD > 0` branch would be a behaviour change with its own
  review; funnelling it makes it harmless, and the gate now forbids any replacement
  from re-introducing a bare mint.
