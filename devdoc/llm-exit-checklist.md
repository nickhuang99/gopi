<!-- doc-class: historical -->
# LLM exit checklist + the rescue-then-reclaim close-out

**Epic:** 01a11548 · **Date:** 2026-10-07 · **Branch:** `task/01a115481a967150bd3aaed26c27f99a`

## The question this epic started from

`gopi run` ends, the row reads `completed`, and the worktree it ran in is still
on disk — sometimes for ever. Why, and what should happen instead?

## Completed

### 1. The measurement (the shape, not a theory)

Three rows were stranded on this host in exactly one shape:

| row | worktree | what it held | state |
|---|---|---|---|
| `01a113c5…` | already gone | — | completed (resumed ×1) |
| `01a11494…` | already gone | had `?? screen-1791348588.png` | completed |
| `01a113ac…` | `/home/user/workspace/gopi-01a113ac…` | 9 tracked `M` files | failed (resumed ×2) |

Two independent facts, and they are **not** the same fact:

* the residue is a **finished run's own scratch** (a probe, a capture) — hygiene;
* or the residue is **uncommitted deliverable work** — an obligation.

`git status --porcelain` shows both as "dirty", and the old close-out treated
them identically: it refused the removal and never came back. The directory
survived, no sweep collected it (the sweep skips a live/bound ground), and the
only exit was a human deleting a file by hand.

`01a113ac` also proves the third state the code must respect: its worktree is
held by a **live process** (two `gopi` pids with `cwd` inside it), so it is not
stranded at all — it is in use. The refusal there is correct.

### 2. The remedy — rescue FIRST, then reclaim (`task_finish_cleanup_salvage.go`)

A dirty ground that is **untracked-only residue** is now rescued, and only a
cleared rescue drops the refusal:

1. **A rescue ref in the repo** — `refs/gopi/residue/<task>-<hash>`, built with a
   throwaway index (`GIT_INDEX_FILE`) and `write-tree`/`commit-tree`, so it is
   addressable for ever by git and **does not touch the run's own index or
   working tree**. `git ls-tree -r <ref>` lists everything; `git archive <ref>`
   retrieves it.
2. **A tar bundle** for the git-**ignored** class, which no tree object can
   hold — under the run's own session directory
   (`<gopi-dir>/sessions/<taskID>/salvage/`), with size and sha256 recorded.
3. **A record on the row's own trail** (`<unclean-event>-salvaged`) naming the
   ref, the entries, the bundle and the digests — plus a stderr line when the
   ledger is unreachable (Rule 15: the record is never silent).
4. **`git worktree remove --force`** on the path that got here — every gate above
   already asked and answered ownership, liveness, run identity and the
   completion fact; the flag is taken only where a cleared rescue proved the
   content is retrievable.

### 3. What it deliberately does NOT do — the asymmetry (Rule 26)

`finishCleanupDirtyReasonIsResidue` matches the **untracked** shape only.

A **tracked** uncommitted change keeps its directory. That is not an oversight:
the tracked shape refuses the `completed` writeback (`reclaimRunTrackedDirtRefusal`),
the row stays OPEN, and a ground belonging to an unfinished run is not this
arm's to collect — removing it would delete the tree a live obligation is
written in. Six existing tests pinned the old "any dirt survives" contract; they
were **narrowed**, not deleted, so the tracked half is still pinned.

### 4. The responsibility boundary (the question the epic asked)

* **Child run duty**: leave `git status --porcelain` empty (Rule 30). The
  checklist in the close-out briefing states it before the run starts.
* **Worker/close-out duty**: never let a finished row's ground outlive its work
  *by inaction* — rescue it, then reclaim it. The rescue is the backstop that
  makes the child's failure recoverable rather than permanent.
* **Backstop**: `refs/gopi/residue/*` lives in the shared object store, so the
  residue outlives the directory whether or not anybody reads the record.

### 5. Reuse, not a second implementation (Rule 8 / Rule 14)

The bundle does not re-implement tar. `dirty_holder_salvage.go` — the existing
dead-holder salvage arm — already owns the ONE gzip+tar writer
(`dirtyHolderUntrackedArchive`) and the ONE size/sha256 evidence recorder
(`captureAppend`). This arm calls them, and adds in front of them the one check
they do not make: an ignored entry whose **resolved** path leaves the ground is
refused by name and the ground is kept (rescuing a ground must not become
exfiltrating the host).

## Not Done (honest)

* **The three stranded worktrees were not disposed of.** Two of them
  (`01a113c5`, `01a11494`) had **already been reclaimed** before this run — their
  directories do not exist, so there was nothing to dispose of, and the ledger
  row for each names a worktree that is gone. The third (`01a113ac`) is held by
  **live processes** (pids 2159778, 2497634, `cwd` inside it): it is in use, not
  stranded, and touching it would be the Rule 29 defect. No probe file or
  capture was found on disk to delete.
* **The rescue clears the removal reason for the untracked shape only.** A row
  whose ground is uncommitted **tracked** work is still refused — by design, and
  the refusal keeps the directory.
* **`refs/gopi/residue/*` has no reaper.** Each rescue ref is ~1 tree object; the
  refs are cheap and never collected. A collector would be a new persistent
  mechanism (Rule 35) and none is proposed here.
* **The standards suite is red on this branch's base**, for files this change
  never touches: `internal/shot/x11.go` (Rule 20 `getRootImage`, Rule 13
  `/tmp/.X11-unix`), `internal/agent/crashdump.go:228`, `internal/replaydrive/replaydrive.go:521`.
  Reported, not fixed (Rule 26: out of this task's scope).

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...` → **OK** (exit 0, no output)
- **Tests:** `go test ./internal/tools/ -run 'FinishCleanup|Salvage|Reclaim|Dirty|Residue|Worktree|ServerRecycle|DeliverySurvives|AlignmentPrecedes' -count=1` → **ok 73.6s** (0 failures)
- **Ran:** the REAL verb end to end against a REAL ledger row in a sandbox profile (Rule 34's `gopi tool sandbox create`), whose deliverable was a real repo plus a real linked worktree holding `zz_selfprobe_probe_test.go` + `screen-1791348588.png`:

  ```
  gopi task submit "salvage demo: a finished run's ground holding a probe" --repo <repo>
  gopi task update <id> --status claimed / in_progress / completed
  gopi task finish-cleanup --task <id> --dry-run
  gopi task finish-cleanup --task <id> --apply
  ```

- **Result:**
  - before: `git -C <wt> status --porcelain` → `?? screen-1791348588.png` / `?? zz_selfprobe_probe_test.go`
  - dry-run: `candidates=1 would-run=1 …`
  - apply: the report's `salvage` step `OK:true` naming `refs/gopi/residue/…`, followed by the `worktree` step `OK:true`
  - after: `ls -d <wt>` → **No such file or directory** (the ground is reclaimed)
  - after: `git -C <repo> for-each-ref refs/gopi/residue` → one ref; `git ls-tree -r <ref>` → the probe file; subject = the rescue commit
  - after: `~/.gopi/sessions/<id>/salvage/` → the bundle on disk for the ignored class
  - after: `gopi task show <id>` → the rescue record on the row's trail
- **Red→green (each arm proved to fail without its own fix):**
  - rescue call disabled (`if false && …`) → `TestSalvageClearedResidueLetsTheRemovalProceed` **FAIL** ("the ground must be RECLAIMED")
  - `--force` removed → same test **FAIL** (git refuses the untracked directory)
  - both restored → **ok**
- **Regression:** `go test ./internal/tools/ -run 'TestSalvage' -count=1` → **ok**
- **Commit:** _see the branch head_

## Evidence (self-filed)

- `CGO_ENABLED=0 go build ./...` → exit 0, empty output.
- `go test ./internal/tools/ -run 'FinishCleanup|Salvage|Reclaim|Dirty|Residue|Worktree|ServerRecycle|DeliverySurvives|AlignmentPrecedes' -count=1` → `ok github.com/nick/gopi/internal/tools 73.617s`, no `--- FAIL` line.
- `go test ./internal/standards/ -count=1` → **FAIL**, and every named hit is in a file this change does not touch (`internal/shot/x11.go:1064`, `internal/shot/x11.go:193`, `internal/agent/crashdump.go:228`, `internal/replaydrive/replaydrive.go:521`) — a pre-existing base condition, reported not fixed.
- The end-to-end run's own output: the post-apply `ls -d <wt>` printed "No such file or directory" and `for-each-ref refs/gopi/residue` printed the rescue ref — the two observations that are the deliverable's whole point.
