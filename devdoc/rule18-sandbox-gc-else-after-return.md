<!-- doc-class: historical -->
# Task Report: `sandbox_gc.go` — the Rule 18 violation this branch introduced, repaired

Task: `01a12f48-e17f-70fb-9771-752987c2f8e5` · branch `task/01a12f48e17f70fb9771752987c2f8e5`

## Completed

- **`internal/scratch/sandbox_gc.go` — the liveness arm became two guards, not an
  `if/else`.** The construct at line 247

  ```go
  if live, ok := holders.HeldByProcesses(path); !ok {
      keep.Reason = HolderScanWithheldReason()
      return keep
  } else if live {
      return SandboxPlan{Path: path, Keep: true, Reason: SandboxKeepHeld}
  }
  ```

  is now

  ```go
  held, scanned := holders.HeldByProcesses(path)
  if !scanned {
      keep.Reason = HolderScanWithheldReason()
      return keep
  }
  if held {
      return SandboxPlan{Path: path, Keep: true, Reason: SandboxKeepHeld}
  }
  ```

  The call was **hoisted to a named statement** — that is the whole repair: Go
  scopes an `if` init statement to the whole `if/else` chain, so the `else`
  existed only to keep `live` visible, not to express a branch. No `else` is
  left after a terminating body, both guards are flat, and the semantics are
  unchanged: `!scanned` ⇒ keep with the withheld reason; `held` ⇒ keep with
  `SandboxKeepHeld`; otherwise fall through to the collectable path. The comment
  above gained one sentence naming the two facts as separate guards, so the next
  reader does not re-merge them.
- **`internal/scratch/sandbox_gc_test.go` — the sandbox arm's truncated-scan
  direction, which had no test.** `TestSweepKeepsASandboxWhenTheHolderScanWasTruncated`
  drives the REAL predicate `judgeSandbox` three ways: a `HolderScan{Truncated: true}`
  keeps the directory AND discloses the truncation AND does **not** claim a live
  holder (`SandboxKeepHeld`); the control (a complete scan that found nobody, same
  directory) is collectable, so the kept case cannot pass vacuously. The sibling
  arms already had this proof (`internal/scratch/holder_scan_test.go:145,174,227`),
  the sandbox planner did not.
- **Gate provenance, measured not assumed** (see Verification): the Rule 18 NEW
  hit was introduced by `e32a74237` on THIS branch, and the fix brings the whole
  gate package green.

## Not Done

- **Two `CommandsDoc` tests fail in `./internal/standards/` on this branch, and
  they are PRE-EXISTING.** `TestCommandsDocGeneratedBlockMatchesSource` and
  `TestCommandsDocGateRedProofStaleGeneratedBlock` are red because `COMMANDS.md`'s
  generated block cites `cmd/agent/cli_archive.go:136` while the source now says
  `:145` (the branch's own `cli_archive.go` change moved it without regenerating the
  block). **They are NOT gate hits and NOT part of this task's acceptance command**
  (`-run TestStandardGates` is green), and they are **not** fixed here: regenerating
  `COMMANDS.md` rewrites a generated artifact of a different change. They were
  **measured on pristine master** (detached worktree at `88b7584af`, same two tests
  red) — the row to regenerate the block belongs to whoever owns the `cli_archive.go`
  line move. Filed for a fresh session rather than fixed here, per the "don't widen
  the change" constraint (see the report's task reference).
- **No `// else-ok:` escape and no baseline row.** The gate is a ratchet-down
  allow-list; a new permit is itself an error, so the only correct repair was the
  de-indent above.

## Verification

- Build: `go build ./...` → OK (exit 0); `CGO_ENABLED=0 go build -o <session>/artifacts/gopi-rule18 ./cmd/agent/` → OK; `gofmt -l internal/scratch/sandbox_gc.go` → prints nothing.
- Tests:
  - `go test ./internal/standards/ -run 'TestStandardGates' -count=1` → **`ok github.com/nick/gopi/internal/standards 6.414s`** (acceptance clause).
  - `go test ./internal/standards/ -run 'TestStandardGates|TestStandardGateDataReport' -count=1 -v` → `Rule 18 (no else after terminating if) (checklist 4.3): hits=2 baselined=0 escaped=2 NEW=0` (was `hits=3 … NEW=1`), and the other ten rules NEW=0.
  - `go test ./internal/scratch/ -run 'Sandbox|Holder' -count=1` → `ok github.com/nick/gopi/internal/scratch 4.712s` (exit 0).
  - `make test-budget PKGS='./internal/scratch ./internal/standards'` → exit 1 in **71s of a 25m budget** (`budget did NOT trip`), with exactly the two pre-existing `CommandsDoc` failures and **no scratch failure**.
- Ran: the changed surface invoked for real through the freshly built binary (Rule 31), `gopi tool scratch-gc sandboxes --dry-run` (the exact verb that calls `PlanSandboxes` → `judgeSandbox`), on real aged sandboxes under `/home/user/workspace/gopi-sandbox`:
  - **A sandbox with a live process inside** (`sh <script>` that `cd`s into it, verified by `readlink /proc/3446245/cwd` = that sandbox) and `touch -d '5 days ago'`:
    `keep /home/user/workspace/gopi-sandbox/sandbox-rule18-held-a3a4ec40 reason="a live process stands inside it"` → the `held` guard.
  - **The control, same sweep**: an aged sandbox whose recorded owner is gone →
    `would-remove … reason="aged and unowned (recorded owner pid 3401649, start ticks 316292205, is gone)"` → the fall-through path still authorises a delete, so the kept case is not kept by accident.
  - **The real (non-dry) sweep**, after aging all three proof sandboxes: `removed=3 … bytes=4546`, and `ls -d /home/user/workspace/gopi-sandbox/sandbox-rule18-*` → `No such file or directory` (the directories are gone; the proof left no residue).
  - `GOPI_HOLDER_SCAN_DEADLINE_SEC=1 gopi tool scratch-gc sandboxes --dry-run` printed the same plan, so **the truncated arm was not reachable end-to-end on this host** (the process table was scanned inside 1s) — it is covered by the predicate-level test above instead, and that is stated rather than implied.
- Result: `git status --porcelain` in the worktree prints nothing after the commit below.
- Commit: `db8c76c5e` "fix(scratch): drop the else after the terminating liveness guard (Rule 18)".

## Evidence (self-filed)

| claim | observation |
|---|---|
| Rule 18 NEW=1 before, NEW=0 after | `TestStandardGateDataReport`: `Rule 18 … hits=3 baselined=0 escaped=2 NEW=1` → `hits=2 baselined=0 escaped=2 NEW=0` |
| introducing commit is on this branch | `git log --oneline -S 'else if live {' -- internal/scratch/sandbox_gc.go` → `e32a74237 feat(scratch): bound the /proc holder scan and make truncation fail-closed` (only hit, all refs) |
| pristine master is innocent of the Rule 18 hit | detached worktree at master `88b7584af`: `git show master:internal/scratch/sandbox_gc.go` has no `else` at that site |
| the live-holder keep is real | `readlink /proc/3446245/cwd` → `…/sandbox-rule18-held-a3a4ec40`; sweep printed `keep … reason="a live process stands inside it"` |
| the change is formatting-clean | `gofmt -l internal/scratch/sandbox_gc.go` → no output |

## Deliberate design decisions

1. **The `else` was Go statement scoping, not a branch** — so the repair is a
   hoist (`held, scanned := …`), not a rewrite of the decision. Both guards read
   as the two facts the scan can report.
2. **The added test pins the DIRECTION the branch touched**, at the smallest
   sample: the sandbox planner alone (no `/proc` walk, no process spawning), with
   a control in the same test so an accidentally-always-keep predicate fails too.
3. **The evidence sandboxes were reclaimed by the sweep itself**, not deleted by
   hand: the proof that the code path works is the same act as leaving no residue.

## Post-commit addendum (measured after the commit above)

- **`git status --porcelain` is empty** in the worktree (accepted).
- **`go test ./internal/standards/ -count=1`** (the whole package, not only the
  acceptance selection): the `TestStandardGates` family is green; the two red
  tests are `TestCommandsDocGeneratedBlockMatchesSource` and
  `TestCommandsDocGateRedProofStaleGeneratedBlock`, both **reproduced on pristine
  master** `88b7584af` in a detached worktree — their message names
  `cmd/agent/cli_archive.go:136` (document) vs `:145` (source), the line the
  branch's own `cli_archive.go` change moved. Not this task's surface, not fixed
  here.
- **`gofmt -l internal/scratch/` prints `internal/scratch/attachments_archive.go`,
  and that file is a PRE-EXISTING gofmt debt of this branch, not of this task.**
  Measured: the same file at master `88b7584af` (`git show master:… > /tmp/aa_master.go`,
  `gofmt -l`) is unformatted too, and the branch's own one-line change to it
  (`heldSet(LiveHolders(), …)` → `heldSet(ScanHolders(), …)`) is not a formatting
  change. The file this task edited (`sandbox_gc.go`, `sandbox_gc_test.go`)
  prints nothing under `gofmt -l`.
