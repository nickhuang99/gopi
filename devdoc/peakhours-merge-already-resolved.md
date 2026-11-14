<!-- doc-class: historical -->
# fix/peakhours-exact-boundary — the merge was already resolved (verification record)

Task: "resolve merge conflict: fix/peakhours-exact-boundary into master"
(dispatched 2026-09-14, branch tip `ae90cb0e`, repo /home/user/work/GoPi).

**Finding: no conflict remains. The branch is already merged into master.**
The work is a verification, not a textual merge; this record is carried so the
same task is not dispatched a third time.

## Evidence (all commands run on master @ 40fe5e81)

1. **Merge commit exists and is an ancestor of master**
   - `git show --stat 8a29ab93` →
     `resolve merge fix/peakhours-exact-boundary into master`, 2026-09-14 04:24 +0800,
     parents `ae90cb0e` (branch tip) + `0a5687d9` (then-master).
   - `git merge-base --is-ancestor 8a29ab93 master` → exit 0.
   - `git merge-base master ae90cb0e` → `ae90cb0e` (the branch tip *is* the merge base,
     i.e. master contains it in full).
2. **Every code path the branch touched is byte-identical on master to the branch tip**
   - `git rev-parse ae90cb0e:<path>` == `git rev-parse master:<path>`:
     `internal/peakhours/peakhours.go`, `internal/peakhours/peakhours_test.go`,
     `internal/config/peakhours_decision_test.go`,
     `devdoc/task-process-identity-rework-2026-09-13.md`.
   - No commit after `ae90cb0e` touched the peakhours packages:
     `git log --oneline ae90cb0e..master -- internal/peakhours internal/config` → empty.
3. **The single differing path differs in master's favour**
   `devdoc/peakhours-exact-boundary.md` — master's blob equals the blob at the
   *merge commit* (the merge's own resolution, +6/−3: it adds the
   `ecc0d505` / `4b09f114` / `91a5b240` superseded-series hashes). The branch
   side is the older, poorer text; no merge action can improve on what is there.
4. **The branch is gone** — `git rev-parse fix/peakhours-exact-boundary` fails and no
   worktree holds it: the merger sweep deleted it after the merge
   (`git branch -d` only succeeds for a merged branch — itself evidence of ancestry).

## Branch intent, confirmed live on master

`internal/peakhours/peakhours.go` `NextOffPeak` walks the **hour-start lattice**
(`hourStart(bt).Add(time.Hour)`), not hours counted from the current minute, so the
refusal window is no longer over-stated by up to 59m. The regression tests are present
and pass: `TestNextOffPeakNamesTheBoundaryNotTheNextHourFromNow`,
`TestNextOffPeakMidHourRegression`, `TestNextOffPeakAtClosingBoundaryIsTheBoundaryItself`.

## Verification of this record
- `CGO_ENABLED=0 go build ./...` → BUILD OK
- `go test ./internal/peakhours/ ./internal/config/` → PASS (transcript in the session report)

## Not done
- No code change was made and none is needed: re-merging `ae90cb0e` from a fresh
  worktree returns "Already up to date", and Rule 23 forbids reworking a vanished
  branch's path.
