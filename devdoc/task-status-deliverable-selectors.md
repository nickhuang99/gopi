<!-- doc-class: historical -->
# Task Report: `gopi task status` accepts `--branch` / `--worktree`

**Task**: 01a0f97b-4b76-7e46-8041-30c62719dc76 — epic "task status should accept branch/worktree".
**Branch**: `task/01a0f97b4b767e46804130c62719dc76` (worktree `/home/user/workspace/gopi-01a0f97b4b767e46804130c62719dc76`, the ground the runner created for this run).

## Completed

1. **The verb now takes the same three addresses `gopi task show` takes.**
   ```
   gopi task status [<task_id>] [--branch B | --worktree PATH] [--json] [--db PATH]
   ```
   The positional row id, or the DELIVERABLE the operator standing in a task
   worktree actually holds (`git branch --show-current` / `pwd`). The zombie
   question ("is the row that owns this ground still alive?") is asked FROM a
   worktree, where the row id is the one fact the operator does not have.
2. **ONE resolution seam, two verbs** — `resolveDeliverableSelector`
   (`cmd/agent/cli_tasks_show.go`), called by `runTaskShow` and `runTaskStatus`.
   It wraps the ledger-side resolver `tools.ResolveTaskIDByDeliverable`
   (live-first policy, plurality note on stderr) and the labelled git-provenance
   answer for a selector no row records. The two verbs cannot disagree about
   which row a selector names, because there is only one place that decides.
   The canonical address pays nothing for the machinery: a bare `task_id` comes
   back unchanged, no ledger read, no git command.
3. **The selector fields were renamed to name the concept, not the verb**:
   `tasksOptions.showBranch/showWorktree` → `selBranch/selWorktree`. They are now
   shared by two verbs, so a field called `showBranch` would have been a lie in
   `runTaskStatus`.
4. **Parse admission is ONE arm for `show` + `status`** (three addresses, exactly
   one of them), with the "no address at all" refusal produced by ONE shared
   function `taskRowAddressRefusal()` — two copies of the sentence would drift
   exactly the way two admission tables would (Rule 14). `taskFlagMember`'s
   `Status` case admits `--branch`/`--worktree` beside `db`/`json`.
5. **A selector no row records exits 4, NEVER 1** — `taskStatusExitNoRow`
   (`cmd/agent/cli_tasks.go`). On this verb **1 already means "running; do NOT
   resume"** (`taskStatusExitRunning`), so a selector naming nothing would answer
   "the row is alive" to every script that reads the code — a wrong answer that
   looks like a certain one, which is the exact defect this verb exists to
   remove. 4 is the family's existing "the ledger WAS read and no row names it"
   code (`tools.TaskForBranchExitNoOwner`, e.g. `gopi task for-branch`), so no
   third code was minted.
6. **Help + the surface gate it feeds**, in one commit: the status synopsis, the
   description and the exit-code sentence; the G1 for-branch entry END-marker
   literal in `cli_tasks_help_surface_basic_test.go`; the value probes for
   `--branch`/`--worktree` in the advertised-flag sweep; the inventory doc
   (`devdoc/gopi-task-help-surface-inventory.md`: TSV lines for `show`/`status`,
   the two Markdown rows, and the totals 51 → 55 advertised verb-flag pairs).
   The `scanned < 50` floor is unchanged; the sweep now inspects 55.
7. **Six new tests** (`cmd/agent/cli_tasks_status_selector_test.go`) + the G1
   status case extended — see Verification.

## Not Done

- **The G1 status case drives `--branch` only, not `--worktree` end to end.** The
  worktree address is covered in process (`resolveDeliverableSelector` resolves a
  worktree to the same row id) and by the pre-existing `show --worktree`
  end-to-end test, but I did not build a second binary probe for it. The three
  addresses converge on one resolved row id; a second binary probe would re-test
  the same resolution with a different string.
- **No change to `task list` / `task search`.** The epic names `status`; the same
  selector on the LIST views is a separate surface with its own question ("which
  rows, and does this selector narrow them?"), not a copy of this one.
- **The `gopi task -h` demo captures under `docs/archive/demo/materials/*/02-output.txt`
  still quote the old status synopsis.** They are recorded captures of past runs,
  not gates (no test reads them for this text), and I did not rewrite history to
  match today's help.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK; `go build ./...` → OK.
- Tests (branch-scoped, the packages the diff touched):
  - `go test -count=1 -run 'TestTaskStatus|TestTaskShow|TestTaskVerbStatus|TestTaskVerbShow|TestTaskHelpSurface|TestTaskVerbForBranch' ./cmd/agent/` → `ok github.com/nick/gopi/cmd/agent 6.100s` (before the G1-case extension), and `… -run 'TestTaskVerbStatusRunsAsAdvertised|TestTaskHelpSurface|TestTaskStatus'` → `ok 2.862s` after it.
  - the change-detection ratchet the diff tripped: `TestSyncBarePeersTestCoversEveryChangedFile` failed on the five `cmd/agent` files this branch touches; they are now listed in `syncPeersBareUnrelatedFiles` WITH their reason, and `go test -run TestSyncBarePeers ./cmd/agent/` → `ok 1.873s`.
  - `make test-budget PKGS='./cmd/agent/ ./internal/tools/'` → exit 1 on `./cmd/agent`, 6 failing tests, ALL pre-existing on this shared branch and unrelated to this diff (`TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--no-color`, `TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator`, `TestCodeSearchBackendFindsDefinition`, `TestSyncBarePeersTestCoversEveryChangedFile`) — each re-run in isolation: the first three pass, the last was the ratchet above and passes after the list extension. `./internal/tools` was green in that same run.
  - `make test-budget PKGS='./cmd/agent/'` re-run after the fix → **`exit=0
    outcome=pass`, elapsed 653s**, stream
    `/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1790894802-3498917.json`
    (read back: 0 fail records for the whole package, the six pre-existing reds
    of the earlier wider run among the tests that ran green here).
- Ran (the surface I changed, verbatim, on this host — probes 1–8):
  ```
  ./bin/gopi task status                                    → exit 1, usage refusal (names both selectors)
  ./bin/gopi task status --branch feat/definitely-nobody-owns-this
                                                            → exit 4; stdout = the LABELLED git provenance block
  ./bin/gopi task status 01a0f97b-4b76-7e46-8041-30c62719dc76   → exit 1, verdict: running
  ./bin/gopi task status --branch task/01a0f97b4b767e46804130c62719dc76
                                                            → exit 1, byte-IDENTICAL to the row-id render
  ./bin/gopi task status --worktree /home/user/workspace/gopi-01a0f97b4b767e46804130c62719dc76
                                                            → exit 1, the same row's verdict
  ./bin/gopi task status --branch b --worktree w             → exit 1, "two ways to name ONE row … pass one"
  ./bin/gopi task show --branch task/01a0f97b4b767e46804130c62719dc76  → unchanged
  scripts/g1-task-help-surface.sh ./bin/gopi                 → status  branch,db,json,worktree  4
  ```
  The four probes' raw stdout/stderr are in this run's evidence directory
  (`/home/user/.gopi/sessions/01a0f97b-4b76-7e46-8041-30c62719dc76/p{1,2,3,4,6,7}.{out,err}`).
- Result: `diff p3.out p4.out` is empty — **the verdict reached from the branch
  the operator is standing in IS the verdict the row id gives**; exit codes are
  `1` (running) for both, `4` for a selector no row records, `1` for the two
  usage refusals. `task status --branch …` on THIS run's own row answered
  `verdict: running` with `holder: peerbox:3408523` — the live holder of this very
  task, which is the epic's use case ("query the running status of a worktree /
  branch, or decide whether it is a zombie") exercised against a real row.
- Commit: `89153841 feat(task): status accepts the deliverable selectors --branch/--worktree`

## Evidence (self-filed)

- `gopi task status --branch <no-owner>` → exit code `4` (printed above); the
  forbidden outcome (exit 1) is asserted absent by
  `TestTaskStatusSelectorWithNoOwnerIsRefusedWithItsOwnCode`, which reads the
  code off the REAL binary.
- HEAD-render equality: `diff` of probe 3 vs probe 4 → empty (and the same
  assertion inside the built binary's probe).
- `scripts/g1-task-help-surface.sh ./bin/gopi` → `status  branch,db,json,worktree  4`
  and `show  branch,db,worktree  3`, matching the two TSV rows written into the
  inventory doc in this commit.
- `TestTaskHelpSurfaceAdvertisedFlagsAreAdmitted` scan count: 55 ≥ the 50 floor
  (the sweep does not fail on the floor; the count is stated in the doc).
