<!-- doc-class: historical -->
# The notify.sh slot-name pin pinned a spelling, not a property

Task report for the row filed out of the discovery run `01a12dff` ("9 red tests
on master, three classes"). Measured 2026-10-12 on host `hosta`, worktree
`/home/user/workspace/gopi-01a12e2aff3877d48a3f66c5096234a4`, branch
`task/01a12e2aff3877d48a3f66c5096234a4` — the branch tip that carries this file
is the same tip the handed-over `task-report.md` records as FINAL TIP (writing a
hash into the file the hash identifies makes it stale on the next amend).

## Completed

- **Reproduced all three classes on pristine master `d7fb65344`** (detached
  worktree `git worktree add --detach <scratch> d7fb65344`), with the brief's own
  three commands, raw output kept untruncated.
- **Fixed the surviving class (2)** — the `notify.sh` slot-name pin — in three
  files (below), red-first measured, and re-measured green at this branch's tip.
- **Diagnosed class (2) by measurement, not inference**: the script is correct;
  the pin asserted a literal, and the literal was renamed by a refactor.
- **Executed the changed surface for real** (Rule 31): the sink script itself,
  against a private profile, showing the slot file names it builds and reads.
- **Reported classes (1) and (3) as INHERITED**, with the commit that carries
  each and the measured ancestry that says which refs hold them.
- **Rule 41**: searched the tree for the same shape elsewhere; nothing filed,
  because everything found was a different (correct) shape — recorded rather
  than guessed.

## Files

| file | change |
|---|---|
| `cmd/agent/chat_session_slots_script_test.go` | the pin: three expansions get one definition each; `notifyScriptSlotLoopVar` reads the script's own counter; `notifyScriptAssignedSlotName` requires the build to be an assignment, not any occurrence |
| `cmd/agent/chat_session_slots_script_loopvar_test.go` | new: the regression arm (three counter spellings, the old predicate asserted false, a comment-only mention rejected, the shipped script read through both readers) |
| `devdoc/notify-slot-name-pin-pinned-a-spelling.md` | this report |
| `devdoc/DOC-INDEX.tsv` | the row for it (Rule 36, same commit) |
| `scripts/notify/notify.sh` | **not changed** — the script was never wrong |

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → `BUILD_OK` (no output before it).
- **Tests** — the brief's three commands, both arms measured:

  | brief's command | pristine master `d7fb65344` | this branch's final tip |
  |---|---|---|
  | `go test ./internal/standards/ -run 'TestStandardGates\|TestRepoHygiene\|TestStandardGateDataReport'` | **FAIL** (4 sub-failures: Gate2TestEnvRedProof + TestStandardGateDataReport, and 2 Rule 39 hygiene) | **ok** 41.129s |
  | `go test ./internal/tools/ -run 'TestCovFinishCleanupClientArmNoPeerRecyclesWorktree\|TestCovFinishCleanupLinkedWorktreeRemovedBranchKept\|TestFinishCleanupClientDirtyStillDelivers'` | **FAIL** 3/3 ("the published-anchor edge is BROKEN — no generated-document edge was discovered at all") | **ok** 2.452s |
  | `go test ./cmd/agent/ -run 'TestNotifyScriptAgreesWithTheRegistryConstants'` | **FAIL** ("notify.sh does not build the slot name from ${SLOT_PREFIX}${i}${SLOT_SUFFIX}") | **ok** 1.110s |
  | `go test ./cmd/agent/ -run 'TestNotifyScript'` (pin + its two regression arms) | — | **ok** 1.275s |
  | `go test ./internal/tools/ -run 'TestCovFinishCleanup.*\|TestFinishCleanup.*\|TestRunCloseout.*'` (the whole family) | — | **ok** 71.314s |
  | `go test ./internal/gendoc/` | — | **ok** 2.145s |

  Raw: `artifacts/repro-master.txt` (pristine master, untruncated),
  `artifacts/repro-base.txt` (this branch's base), `artifacts/repro-final-tip.txt`
  (the final tip, the run above), `artifacts/family-check.txt` — all under this
  run's session directory `~/.gopi/sessions/01a12e2a-ff38-77d4-8a3f-66c5096234a4/`.
- **Ran** (Rule 31) — the changed surface is a test that reads a script as text,
  so the script was also **executed**:

  ```
  $ bash artifacts/real-run-notify.sh /home/user/workspace/notify-probe-real \
        <worktree>/scripts/notify/notify.sh
  notify.sh: slot /home/user/workspace/notify-probe-real/.gopi/chat_session_id_0.txt
             (/home/user/workspace/notify-probe-real/.gopi/sessions/probe-session):
             delivery refused: gopi notify: unknown session "…/probe-session" — there are
             no sessions under …/.gopi/sessions (nothing has run in this profile yet)
  notify.sh: slot …/.gopi/chat_session_id_7.txt holds not-an-identity, which is not
             <pid|self>:<session-dir> — skipped
  notify.sh: delivered=0 refused=1
  notify.sh: peer peerbox: first read (no cursor) — starting at 2026-10-12T11:29:25+08:00, 0 delivered
  EXIT=0
  ```

  **Result**: both slot files were found **by name** (`chat_session_id_0.txt`,
  `chat_session_id_7.txt`) and both were read — slot 0 reached the delivery call
  (refused for its *content*: no such session in the probe profile), slot 7 was
  rejected for its *content* — which is what "the name is built from the declared
  parts" means. The anchor file was written byte-identical to the payload. Raw:
  `artifacts/real-run-notify.txt`.
- **Worktree**: `git status --porcelain` prints nothing (Rule 30).
- **Docs**: `gopi tool doccheck` exit 0.

## The defect, explained by measurement

Class (2) is the same shape as class (1): **a check that pinned a spelling
instead of the property it wanted to protect.**

The pre-rename script walked its slots at top level with the counter `i` and built
the name as `${SLOT_PREFIX}${i}${SLOT_SUFFIX}`. Commit `9e8446fdd` ("feat(notify):
pull peer task events in notify.sh before local delivery") lifted that loop body
into a function `deliver_all()` — which makes every loop variable local to the
function — so the counter became `_da_i` and the name followed it, correctly.
The pin had been written the day before (`3a107a06a`) as
`strings.Contains(text, "${SLOT_PREFIX}${i}${SLOT_SUFFIX}")`: it asserted the
**literal**, so master went red against a correct script, and the failure text
("the name is spelled somewhere else") described the assertion, not the script.

Measured, claim by claim:

| claim | measurement | result |
|---|---|---|
| the literal vanished at `9e8446fdd` | `git show 9e8446fdd^:scripts/notify/notify.sh \| grep -c '\${SLOT_PREFIX}\${i}\${SLOT_SUFFIX}'` → `1`; the same at `9e8446fdd` → `0` | measured |
| the rename is inside that commit | its removed hunk is the top-level `i=0` / `while [ "$i" -lt "$SLOT_LIMIT" ]` / `slot=…${i}…` block; its added hunk is `deliver_all()`'s `_da_i=0` / `_da_slot="${PROFILE}/${SLOT_PREFIX}${_da_i}${SLOT_SUFFIX}"` | measured |
| no second spelling of the name exists | `grep -vE '^[[:space:]]*#' scripts/notify/notify.sh \| grep -n 'chat_session_id_'` → one hit, the `readonly SLOT_PREFIX=` line | measured |
| the script really reads those files | the sink was executed (Ran, above): both slot files found and read by name | measured |
| the pin was the thing at fault, not the script | pristine master red / this branch green with **no change to `notify.sh`** | measured |

## Per-class state (fix commit + which ref carries it)

| # | class | fix commit | carried by | authored here? |
|---|---|---|---|---|
| 1 | the published-anchor read floor read an EMPTY discovery as a BROKEN edge, so finish-cleanup refused delivery and never released the worktree | `f252ee8fe` "feat(gendoc): a tree that takes part in no published-anchor edge ABSTAINS, it does not REFUSE" | this branch's history (via `d4bbf6954`) and `remotes/gitPushcheck/master`; **not on local `master`** | **no — inherited** |
| 3a | Rule 39 hygiene baseline lacked the oversize permit for `internal/tools/task_finish_cleanup.go` | `c31eff04c` "fix(standards): freeze the oversize permit …" | this branch's history (via `08b97562c`) and `remotes/gitPushcheck/master`; **not on local `master`** | **no — inherited** |
| 3b | Rule 17 gate: 4 NEW + 4 STALE permits — the same four `os.Unsetenv` sites, moved three lines | `186935607` "fix(standards): delete the redundant notify restore and the 4 stale Rule 17 permits" | this branch's **base commit** (sibling row `01a12e26-a5ed`) and `remotes/gitPushcheck/master` | **no — inherited** |
| 2 | the `notify.sh` slot-name pin failed a script that builds the name correctly | **this run's commit** | this branch `task/01a12e2aff3877d48a3f66c5096234a4` | **yes** |

Ancestry, measured rather than read off a label:

```
$ git merge-base --is-ancestor f252ee8fe HEAD   → Y   (same for c31eff04c, 186935607)
$ git merge-base --is-ancestor f252ee8fe master → N   (same for c31eff04c, 186935607)
$ git branch -a --contains f252ee8fe
  this branch, task/01a12e26a5ed7d388eb4b164137bc5d5, task/01a12de43c6178daa8aef01214a2c9e2,
  remotes/gitPushcheck/master
```

So all three inherited fixes are reachable from this branch and from the peer's
master, and none is on local `master` yet — which is exactly why pristine master
still shows classes (1) and (3). Nothing was merged by this run, as instructed.

## Not Done (honest)

- **Classes (1) and (3) were not re-fixed here.** They are inherited and already
  green on this base; the row asked for the surviving class to be fixed and the
  rest reported. Their commits are named above. Local `master` stays red until
  they are merged there — this run did not merge.
- **The family count differs from the brief.** On pristine master the brief's
  standards command shows **four** sub-failures (Rule 17's NEW+STALE pair and the
  two Rule 39 hygiene checks), and the tools command is 3/3 red — the brief's
  "9 red tests … (standards × 2)" is a count at the discovering run's rev. A
  reader re-running the same commands should expect this.
- **Rule 17's red is not this run's diagnosis.** `9e8446fdd`'s report (inherited
  commit `186935607`) explains it as four `os.Unsetenv` sites that moved three
  lines under line-keyed permits; that explanation is **inherited, not
  re-measured here** — this run measured only that the command is red on master
  and green at this branch's tip.
- **No whole-suite run.** Scope was branch-scoped (`cmd/agent`, `internal/tools`,
  `internal/gendoc`, `internal/standards`), per the smoke rule. `make test-budget
  PKGS='./cmd/agent'` was started and **killed at ~13 minutes** on the 120s
  no-output net (it buffers per package and prints nothing while running); the
  narrower `go test` invocations above are the measurements this report rests on.
  Its raw stream is `artifacts/test-budget-cmd-agent.txt`.
- **Rule 41: nothing further filed.** The "whole-file containment pin" shape was
  searched for with
  `grep -rn 'strings.Contains(\(text\|body\|script\|content\|src\),' --include=*_test.go cmd/ internal/`;
  the hits pin *runtime output* (`cli_uistate_test.go`, `host_lifecycle_log_test.go`)
  or *declared constants* (`uiclient.UnpaintedNotice`,
  `procname.WorkerLoopStartLine`), and `cmd/agent/task_pull_switch_test.go:123`（**该文件已随 `task_pull` 开关一起删除，2026-11-05**）
  pins one function's body via `sourceFuncBody` — each is the property it wants,
  not a refactorable identifier. Filing on that reading would be an unreproduced
  claim, so the finding is recorded here with its command and left unfiled.

## Evidence (self-filed)

One line per gate claim — the command and what this run saw:

- Build: `CGO_ENABLED=0 go build ./...` → no output, exit 0 ("BUILD_OK" echoed next).
- Pristine master `d7fb65344` (detached worktree `git worktree add --detach`):
  `go test ./cmd/agent/ -run TestNotifyScriptAgreesWithTheRegistryConstants` →
  `--- FAIL: … chat_session_slots_script_test.go:87: notify.sh does not build the
  slot name from ${SLOT_PREFIX}${i}${SLOT_SUFFIX}`, `FAIL github.com/nick/gopi/cmd/agent 1.520s`.
- Pristine master, tools: `--- FAIL` × 3, `cov_finish_cleanup_test.go:170:
  linked-worktree cleanup skipped: the published-anchor edge is BROKEN — no
  generated-document edge was discovered at all …`, `is_server_node_test.go:231:
  untracked residue must be RESCUED and the ground reclaimed`,
  `cov_finish_cleanup_test.go:224: server-arm recycle must release worktree …`.
- Pristine master, standards: `--- FAIL: TestStandardGates/TestGate2TestEnvRedProof`,
  `--- FAIL: TestStandardGateDataReport`, `--- FAIL: TestRepoHygieneRealTreeIsBaselined`
  ("NEW tracked blobs (1) … internal/tools/task_finish_cleanup.go [oversize] 262502 bytes
  > the 262144-byte limit"), `--- FAIL: TestRepoHygieneCensusReDerivesTheTable`.
- Final tip: the four greps of that raw file print `ok  	github.com/nick/gopi/cmd/agent 1.110s`,
  `ok … cmd/agent 1.275s`, `ok … internal/tools 2.452s`, `ok … internal/standards 41.129s`.
- The literal's disappearance: `git show 9e8446fdd^:scripts/notify/notify.sh |
  grep -c '${SLOT_PREFIX}${i}${SLOT_SUFFIX}'` → `1`; `git show 9e8446fdd:…` → `0`.
- Ancestry: `git merge-base --is-ancestor f252ee8fe HEAD` exit 0; against `master`
  exit 1 (same for `c31eff04c`, `186935607`).
- Rule 30: `git status --porcelain` in the worktree printed nothing.
- Rule 36: `gopi tool doccheck` exit 0.
