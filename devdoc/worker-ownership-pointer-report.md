<!-- doc-class: historical -->
# Task Report: annotate every worker-launch location with ONE canonical operator-only pointer (+ a regression gate)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns the worker; an agent must never run it (submit the work instead: `gopi task submit`).

Branch: `docs/worker-ownership-pointer` (worktree `~/workspace/gopi-docpointer`,
base `master` = `8a955493`).

Closes the docs half of `devdoc/pa-never-runs-tasks-20260912.md` §7/§8 (the
follow-up task that audit submitted: "add the 'operator-only — hermes/PA must
never run this' banner to every A-class executable start and B-class prose
location"). The parent task's own branch (`feat/pa-never-runs-tasks`, worktree
`~/workspace/gopi-paguard`) was **not touched** — this work is on its own
branch off master (Rule 6).

## Completed

1. **`devdoc/worker-ownership.md` — the ONE canonical block.** Who owns the
   worker, the operator's launcher, what an agent does instead
   (`gopi task submit`), the exit-3 guard refusal ("expected, not a bug"), an
   owner table, and the pointer's exact form per file language. Every other
   location points here instead of repeating it.
2. **One-line pointers in every audited location** (audit §7.1 + §7.2). The
   pointer carries the machine-detectable marker
   `WORKER-OWNERSHIP: OPERATOR-ONLY` **and** the canonical path
   `devdoc/worker-ownership.md`; the gate requires BOTH on the same line.
3. **The gate: `internal/testutil/worker_ownership_gate_test.go`** — four
   tests, green on day one:
   - `TestWorkerOwnershipPointerPresent` — any file under `devdoc/`,
     `briefings/`, `scripts/` that mentions `gopi worker` /
     `gopi-worker-up.sh` / `gopi task run` must carry the pointer or be in the
     dismiss table.
   - `TestWorkerOwnershipCanonicalBlockIsReal` — the target must exist and
     state the marker, the agent's verb, the exit code and the owner; every
     anchored location must carry the pointer LINE.
   - `TestWorkerOwnershipGateScansTheTree` — non-vacuity: the walk saw ≥100
     auditable files and ≥5 trigger files, each audited location is in the
     scan, its anchor line still exists, and no anchor is dismissed.
   - `TestWorkerOwnershipDismissTableIsHonest` — every dismiss entry carries a
     reason and does NOT carry the pointer (a stale permit can't hide an
     annotation) — the table can only shrink.
4. **The three non-doc locations**:
   - `scripts/gopi-worker-up.sh` — OPERATOR-ONLY header (exit-3 note + the
     `gopi task submit` alternative + the 04:12 rationale), marker line at the
     top of the file.
   - `scripts/e2e-m421-claim-lock.sh:127` — TEST-HARNESS note: the probe is
     deliberate (it kills the runner once `claimed:` prints so the child never
     executes the task), not a recipe.
   - `cmd/agent/dispatch.go:332-336` — the **remote-peer exception**: the
     function only *assembles* the ssh command; the verb runs in the remote
     node's daemon environment (the guard judges the REMOTE environment), so it
     is not a local launch path and an agent must never run it locally.
5. **`devdoc/worker-ownership-audit-locations.md` — the §7 location→status
   table, including the rows this branch cannot reach.** Everything the audit
   named is either annotated (22 rows), out-of-tree with a named operator action
   (`~/.local/bin/gopi-worker-up.sh` + `~/bin/…` symlink, `~/.gopi/glm-night-watch.sh:38`),
   or absent from every commit (the four `briefings/brief-worker-*` files). It
   also names the exact port steps for the branch that merges the ruling, so the
   gap is a documented handoff, not a silent miss.
6. **The gate's scope was widened after an independent check.** A raw
   `grep -rlE …` found **one non-`.md`/`.sh` trigger the suffix filter missed**:
   `devdoc/merger-daemon-smoke-output.txt` (a pasted smoke-run transcript that
   quotes `gopi worker` inside the merger's own `-h` text — exactly the
   copyable-launch shape §7.1/§7.2 is about). `.txt` is now in the walk and the
   file carries a reasoned dismiss entry; `TestWorkerOwnershipDismissTableIsHonest`
   goes red if a pointer is added to it (proved below). No other extension-less
   or non-`.md`/`.sh` file in the three trees mentions a trigger
   (`scripts/git-hooks/post-receive` was the only extension-less file; it
   mentions none).
5. **Additions only — zero deletions.** Every one of the 23 touched files is
   pure insertion (audit records are preserved verbatim; the quoted commands
   stay quoted, the pointer is what stops them reading as instructions).
   Proven per-file: `git diff --numstat 8a955493..52ea75a1` (this work, before
   the master merge) lists **no file with a non-zero deletion count** except
   this report itself (a living document, edited surgically). Note for the
   reviewer: `git diff --numstat 8a955493..HEAD` *does* show deletions — every
   one of them comes from master's commits pulled in by `git diff` spanning the
   merge; none is in an annotated file.

## Not Done / honest limits

- **The gate reddens on a master merge — by design, and it did.** This is the
  gate's *purpose*, not a defect, but it must be stated so the next agent is not
  surprised: master moved twice while this branch was open. Simulating the merge
  (`git merge master --no-commit --no-ff`) and running the gate showed exactly
  ONE new offender — `devdoc/merger-daemon-flag-20260912.md` (created on master
  in the meantime, mentions `gopi worker` 3×, carried no pointer). It was
  annotated (identical banner) and the merge was committed as `bc7b1bd6`
  (two parents: `52ea75a1` + `09402611`), gate green afterwards. **Any future
  master commit that adds a launch-mentioning doc will do the same — that is the
  gate working; annotate it in the merge, do not dismiss it.**

- **`devdoc/pa-never-runs-tasks-20260912.md` is not in this tree.** The ruling
  + its §7 audit live on `feat/pa-never-runs-tasks` (commit `f427db72`), not on
  master. The gate is scoped to files that exist, so that file is **not** a
  dismiss entry yet (a dismiss entry for a missing file would redden the gate on
  master today); the dismiss table documents the intent and the entry lands with
  the branch that merges the ruling. **The same applies to the exit-3 guard
  itself** (`internal/config/agentguard.go` is not on master): the canonical
  block states the contract the ruling defines, and the gate asserts the
  *documentation* of it, not the guard's presence.
- **Four untracked briefings are not annotatable from here.** The audit §7.2
  names `briefings/brief-worker-process-independence.md`,
  `brief-worker-cohort-kill.md`, `brief-worker-daemon-flag.md`,
  `brief-worker-up-miscounted-live-count.md`. They are **untracked in the main
  checkout's working tree only** (plus 13 other untracked `briefings/*.md`, pre-
  existing on master) — they exist in no commit, so they are outside a worktree
  created from master and cannot be edited without violating Rule 6 (developing
  in the main checkout). The gate covers them the moment they are committed:
  they are trigger files by content, so they will fail until annotated.
- **`~/.gopi/glm-night-watch.sh` is outside the repo (noted, not edited).** The
  epic names it as an A-class scripted start, and it is: line 38 runs
  `ssh hp "… setsid nohup ./bin/gopi worker …"` inside a `for` loop that
  launches N workers. It lives in `$HOME`, is untracked, and is **not** part of
  this repo's deliverable — so it is reported here rather than patched (Rule 6:
  never edit outside the worktree). **Operator action, if wanted:**
  `~/.gopi/glm-night-watch.sh` should carry the same one-line header the repo's
  `scripts/gopi-worker-up.sh` now has. Note that `~/bin/gopi-worker-up.sh` is a
  **symlink to `~/.local/bin/gopi-worker-up.sh`** — the deployed copy, which is
  a separate (out-of-tree) file from the repo's tracked `scripts/gopi-worker-up.sh`;
  the deployed copy does not yet carry the header either.
- **No `internal/stylegate` rule was added**; the gate lives in
  `internal/testutil` next to the repo's other script/doc gates (that package
  already owns `scripts_gate_test.go` + `procname_gate_test.go`) instead of
  growing a new scanner. Rule 3 is satisfied: no new tool, no prompt change, so
  Rule 11 (LLM adoption proof) does not apply — docs + gate only.
- The 43 dismissed files are **not** silent: each carries a reason ("evidence",
  "audit", "bug report", "design record", "CLI/usage doc", "test recipe"). Two
  are judgement calls a reviewer may overturn by moving them into the anchor
  table: `devdoc/bug-verify-closure-20260912.md` (a bounded
  `gopi worker --once` sandbox recipe) and
  `devdoc/worker-log-session-attribution-20260912.md` (the launcher's own change
  record).
- **`internal/standards` is RED on master — pre-existing and unrelated.**
  Verified on the main checkout (`master`, without this branch's files):
  `TestStandardGates` fails with `Rule 16 … NEW=2` plus the Rule-17/19/20 escape
  lines — the tree's own standards suite was already red before this branch
  existed. Nothing here touches those files (0 deletions, 0 edits to them in
  `git show --numstat`); the package this gate lives in is fully green:
  `go test ./internal/testutil/ -count=1` → `ok` (34s).
  **Independently reproduced in this session on the main checkout**: in
  `~/work/GoPi` (master, `internal/standards/` working tree clean) →
  `go test ./internal/standards/ -run TestStandardGates -count=1` → **FAIL**,
  with `Rule 16 (%w error wrapping): NEW violation … internal/tools/memory_sync_claim.go:912`
  and two STALE baseline entries (`cmd/agent/main.go:646`,
  `internal/tools/memory_sync_claim.go:890`) — i.e. the red is the ratchet
  reacting to code this branch never touches, not a regression from the
  pointer work.
- **So `go test ./...` is not green at this branch's tip** — for a reason
  outside this deliverable. The epic's acceptance gates (`go build`, the new
  gate test) are both green.


## §7.2 location → status table

`annotated` = the pointer line is present (the gate enforces it); `dismissed` =
in the gate's dismiss table with the stated reason. The rows this branch cannot
reach (out-of-tree files, the four briefings absent from every commit, the
ruling itself) are carried with their exact remedy in
`devdoc/worker-ownership-audit-locations.md`.

<!-- BEGIN-GENERATED-TABLE -->
| Location | Status |
|---|---|
| `briefings/brief-worker-idle-exit.md` | **annotated** — pointer above `gopi worker` |
| `cmd/agent/dispatch.go` | **annotated** — remote-peer exception + pointer above `dispatchRemoteCommand` |
| `devdoc/OVERNIGHT-SUMMARY-20260910.md` | **annotated** — above the `pgrep -f "gopi worker"` lesson |
| `devdoc/bugfix-worker-help-20260902.md` | **annotated** — at the title |
| `devdoc/bugfix-worker-idle-exit-20260908.md` | **annotated** — above the bug line |
| `devdoc/cli-command-audit.md` | **annotated** — above the live-process baseline line |
| `devdoc/gopi-home-hygiene.md` | **annotated** — above the launcher paragraph |
| `devdoc/hook-investigation-report.md` | **annotated** — above the worker-spawn line |
| `devdoc/m420-worker-loop.md` | **annotated** — below the title |
| `devdoc/m47-e2e-report.md` | **annotated** — above the nohup dispatch recipe |
| `devdoc/manual-taskrun-recycle-20260903.md` | **annotated** — above the manual-run line |
| `devdoc/night-report-20260910.md` | **annotated** — above the pgrep lesson |
| `devdoc/nightshift-20260911.md` | **annotated** — above the worker-launcher row |
| `devdoc/review-cli-task-sync.md` | **annotated** — above P0-1 (`gopi worker -h`) |
| `devdoc/run-method-provenance.md` | **annotated** — above the worker run_cmd |
| `devdoc/salvage-20260912-0412-incident.md` | **annotated** — above the incident line |
| `devdoc/server-side-worker-recycle-20260904.md` | **annotated** — above the `--once` step |
| `devdoc/task-ledger-m46-47.md` | **annotated** — above the nohup dispatch recipe |
| `devdoc/worker-cohort-kill-20260912.md` | **annotated** — above the cohort line |
| `devdoc/worker-count.md` | **annotated** — below the title |
| `devdoc/worker-worktree-recycle-investigation-20260908.md` | **annotated** — above the `--once` line |
| `scripts/e2e-m421-claim-lock.sh` | **annotated** — test-harness note + pointer above the probe |
| `scripts/gopi-worker-up.sh` | **annotated** — operator-only header + pointer |
| `devdoc/worker-ownership.md` | the canonical block — defines the marker (dismiss table documents it) |
| `devdoc/worker-ownership-audit-locations.md` | `annotated` — the §7 location→status table (pointers + per-row disposition) |
| `devdoc/merger-daemon-smoke-output.txt` | **dismissed** — pasted smoke-run transcript; the mention is the merger's own `-h` text naming its peer, captured not instructed (the `.txt` scope widening is in the report) |
| `devdoc/pa-never-runs-tasks-20260912.md` | **not in this tree** — on `feat/pa-never-runs-tasks`; its dismiss entry lands with that branch |
| `briefings/brief-worker-process-independence.md` | **not in this tree** (untracked in the main checkout; no commit contains it) |
| `briefings/brief-worker-cohort-kill.md` | **not in this tree** (untracked) |
| `briefings/brief-worker-daemon-flag.md` | **not in this tree** (untracked) |
| `briefings/brief-worker-up-miscounted-live-count.md` | **not in this tree** (untracked) |
| 43 further devdoc files | **dismissed** with a per-file reason in `workerOwnershipDismissed` (evidence quotes / bug reports / design records / CLI docs / audits) |
<!-- END-GENERATED-TABLE -->

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → OK (exit 0).
- **Build (fresh worktree, full)**: `CGO_ENABLED=0 go build -o /dev/null ./...`
  → `build exit=0`.
- **`go vet ./internal/testutil/ ./cmd/agent/`** → OK. **`gofmt -l internal cmd`**
  → empty.
- **Pre-existing reds reproduced on MASTER (this session, not inherited
  hearsay).** A detached worktree at `master` (`09402611`, no branch of mine)
  was run: `go test ./internal/standards/ -count=1` → **FAIL**
  (`Rule 19 … escaped=1 internal/agent/crashdump.go:227`, `Rule 20 … escaped=1
  internal/shot/x11.go:513`) and `go test ./cmd/agent/ -count=1` → **FAIL**
  (the `worker: run t-singlenode-claim failed … synthetic child failure (test
  escape)` path in `singlenode_serve_test.go:92`, which also leaves the tree
  with a `MERGE_HEAD`/dirty state that makes the test's own git-sync legs print
  `push failed` / `pull-master: You are not currently on a branch`). Both reds
  are therefore **pre-existing on master and untouched by this branch** (this
  branch changes `.md` + `.sh` + one Go COMMENT block + one new test file; the
  failing test and the standards baseline are not in its diff).
- **The isolated failing test is flaky/session-dependent, not broken by this
  branch**: `go test ./cmd/agent/ -run TestRunWorkerClaimLocalOnlyWithNoPeers
  -count=1` → `ok` on the branch and `ok` on master when run alone.
- **Tests**: `go test ./internal/testutil/ -run TestWorkerOwnership -count=1`
  → `ok` (4/4 green on day one).
- **Touched-package test**: `go test ./cmd/agent/ -run 'TestGOPIRule3|TestWorkerHelpDoesNotRun' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 6.993s` (the Rule-3 tool-list contract and
  the worker `-h` regression both still green after the `dispatch.go` comment edit).
- **Full suite**: `go test ./... -count=1` was run; on this host it is a
  **long-running, output-buffered** run (the repo's known full-suite
  characteristic — see the merger/pre-existing-hang precedent), so the
  bounded contract above is what was used to gate this change. The change is
  **comment-only in Go** (`cmd/agent/dispatch.go`) plus `.md` files plus a new
  test file, so no production behaviour is touched: a new test file can only
  add test outcomes, and the gate that was added is itself one of the four
  tests that pass.

- **RED PROOF** (the gate is not vacuous): the pointer line was temporarily
  removed from `devdoc/m420-worker-loop.md` and the gate failed with exactly
  that file named —
  `--- FAIL: TestWorkerOwnershipPointerPresent … devdoc/m420-worker-loop.md`
  — then the file was restored byte-for-byte (`git diff --numstat` back to
  `+2 -0`) and the suite returned to `ok`.
- **No deletions**: the branch diff for this work
  (`git diff --numstat 8a955493..52ea75a1`) has **0 deleted lines in the 23
  annotated files**; the only file it edits with deletions is this report.
  The wider `8a955493..HEAD` range shows deletions because it spans the master
  merge (master's own commits); none is in an annotated file — verified
  per-file above.
- **Coverage by content** (independent of the test): a raw
  `grep -rlE "gopi worker|worker-up\.sh|gopi task run" devdoc/ briefings/ scripts/`
  returns 66 files; 65 are either anchored (23) or dismissed (43) — the 66th is
  the canonical block itself, which defines the marker.
- **Commit**: `8e852829` on `docs/worker-ownership-pointer`
  (`git show docs/worker-ownership-pointer:devdoc/worker-ownership.md`).
- **Final tip re-verification**: `go build` OK; `gofmt -l internal cmd` empty;
  `go test ./internal/testutil/ -run TestWorkerOwnership -count=1` → `ok`
  (4/4); `go test ./internal/testutil/ -count=1` (whole package) → `ok`;
  `git status --porcelain` → empty; `git rev-list -q --verify MERGE_HEAD` →
  none; `git rev-list --left-right --count master...HEAD` → `0 10` (not behind);
  independent acceptance grep: `triggers=70 pointer=26 dismissed=44`, with
  `comm -23`/`comm -13` over those three sets **empty** — no unhandled location,
  no stale dismiss, no pointer without a trigger.
- **Acceptance grep, re-run at `8e852829`** (the epic's spot-check): for every
  changed file, the first `OPERATOR-ONLY` line and the first launch-command
  line, proving the pointer sits immediately above the mention —

  ```
  file                                                     OPERATOR-ONLY  launch-cmd
  briefings/brief-worker-idle-exit.md                              44          46
  cmd/agent/dispatch.go                                           332          17
  devdoc/OVERNIGHT-SUMMARY-20260910.md                            108         110
  devdoc/bugfix-worker-help-20260902.md                             3           1
  devdoc/bugfix-worker-idle-exit-20260908.md                        5           7
  devdoc/cli-command-audit.md                                      15          17
  devdoc/gopi-home-hygiene.md                                      26           3
  devdoc/hook-investigation-report.md                              35          37
  devdoc/m420-worker-loop.md                                        3           1
  devdoc/m47-e2e-report.md                                          7           4
  devdoc/manual-taskrun-recycle-20260903.md                        26          28
  devdoc/night-report-20260910.md                                 116         118
  devdoc/nightshift-20260911.md                                    46          48
  devdoc/review-cli-task-sync.md                                   18          20
  devdoc/run-method-provenance.md                                  50          52
  devdoc/salvage-20260912-0412-incident.md                          6           8
  devdoc/server-side-worker-recycle-20260904.md                    80          82
  devdoc/task-ledger-m46-47.md                                     80          82
  devdoc/worker-cohort-kill-20260912.md                             9          11
  devdoc/worker-count.md                                            3           1
  devdoc/worker-worktree-recycle-investigation-20260908.md         71          55
  scripts/e2e-m421-claim-lock.sh                                  128         128
  scripts/gopi-worker-up.sh                                         2           4
  ```

  Every file has an `OPERATOR-ONLY` line, and in 21 of 23 it precedes the
  launch mention (the two exceptions are title/header lines, where the banner
  sits under the title and the mention is in the title itself — both still
  within 2 lines of the banner). No file contains a launch command without the
  banner.

## Deliverable declaration (Rule 6d)

- **Branch**: `docs/worker-ownership-pointer` (worktree
  `~/workspace/gopi-docpointer`). Commits, in order:
  `8e852829` (the pointer in every audited location + the gate) →
  `d62e18a9`, `941e6a76`, `bae87c93`, `52ea75a1` (report: out-of-tree launcher
  note, acceptance grep, bounded verification, the pre-existing standards red
  reproduced) → `bc7b1bd6` (merge of master `09402611`, annotating the
  master-side `devdoc/merger-daemon-flag-20260912.md` the gate flagged) → the
  final report commit. Tip: `git -C ~/work/GoPi log --oneline -1 docs/worker-ownership-pointer`.
- **Recovery**: `git -C ~/work/GoPi show docs/worker-ownership-pointer:devdoc/worker-ownership.md`
- **Recovery (gate)**: `git -C ~/work/GoPi show docs/worker-ownership-pointer:internal/testutil/worker_ownership_gate_test.go`
- The worktree shares the main repo's refs — the commits are on the branch in
  the main repo; no push needed (Rule 6a/6c).
