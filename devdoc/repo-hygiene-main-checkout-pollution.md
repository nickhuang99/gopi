<!-- doc-class: historical -->
# Report: the main checkout is polluted by WORKTREE runs (root cause + clean-up)

Task `01a0db1e-3d4c-7acf-88ab-41e9d56a9639` (HP). Complaint: *"HP git repo is
dirty and this must be solved before git can pull from peerbox … root-cause why
some task is polluting this repo."*

## Completed

### 1. The dirty set, and a correction to the premise

At the start, `/home/user/work/GoPi` (branch `master`) held **two** untracked
entries — no tracked change, no staged change:

```
?? devdoc/evidence/tui-two-defects/
?? internal/uiclient/live_emphasis_attr_test.go
```

**The dirtiness is not what blocks the pull.** Measured: `pull.ff=only`,
master **10 ahead / 2 behind** `peerbox/master` → `git pull` refuses with
`fatal: 无法快进` (fast-forward impossible). A merge is the operator's
decision; dirtiness is orthogonal. What dirtiness *does* cost: it is
uncommitted work standing in the repo's main checkout, and it is what made the
operator *notice*.

### 2. Root cause — one mechanism, two deposits

A worker run is given a **linked worktree** (`gopi worktree add … -b task/<id>`)
and its briefing also names the repository's **main checkout**. Measured in the
live run's own session log
(`~/.gopi/sessions/01a0dabe-3fa1-7cc8-b601-66f1456b8ffd/fa8915f4-….jsonl`):

> `Repo: /home/user/work/GoPi (branch master at 4c802614). Re-verify every line number before relying on it.`

Nothing in `internal/` or `cmd/` renders that line (`grep '"Repo: '` → no
hits), so it is **payload text authored by the submitter**, not a gopi
template. The writer stated the absolute path of the main checkout; the run
then honored it for the two things a run must write:

* **evidence artifacts** — `devdoc/evidence/tui-two-defects/` was created with
  `mkdir -p /home/user/work/GoPi/devdoc/evidence/…`; the writer session is
  named by the file itself (`grep -rl defectB-after.txt` → the run
  `01a0dabe-…` only);
* **a new test file** — `internal/uiclient/live_emphasis_attr_test.go`
  (mtime 2026-09-25 21:10).

So: **the agent's writes went to the path the briefing named, and the briefing
named the main checkout instead of the run's own worktree.** The contributing
mechanism is that no machine check exists — gopi refuses to *release* a foreign
worktree (`internal/tools/guest_row_guard.go`,
`guestRowOwnReleaseRefusal`) but nothing notices a `read`/`write`/`edit`/`bash`
landing outside the run's ground.

### 3. Is the polluter still running? Which of the two deposits is whose?

| deposit | writer | still running? | worth checking in? |
|---|---|---|---|
| `internal/uiclient/live_emphasis_attr_test.go` | an earlier run (2026-09-25 21:10); a **resumed sibling** `01a0d9ab-e855` re-referenced the path | **no** — every `01a0d9ab-*` row is `completed`; the tree's own log said *"everything was committed except … live_emphasis_attr_test.go"* | **YES** — see below |
| `devdoc/evidence/tui-two-defects/` | the run `01a0dabe-3fa1-7cc8-b601-66f1456b8ffd` (`fix(chat-ui): status line goes silent after submit…`), **`[in_progress]`, pid 1578682, 35 min, session-30627.scope, cwd = its own worktree**, still writing (08:10, 08:29) | **YES** | **not mine to judge** — a live run's artifact |

`devdoc/evidence/` is a **tracked convention** (234 files under
`master:devdoc/evidence/`), so the live run did intend to deliver that file;
its home is just wrong — the same relative path exists in its own worktree.

### 4. Disposition (no data destroyed)

* **The live run's artifact → moved out of the repo, never deleted**:
  `…/sessions/01a0dabe-…/attachments/recovered/main-repo-litter/tui-two-defects/`
  (the run keeps writing its evidence; whoever closes that row can pick it up).
* **The worth-keeping stray test → adopted into this branch** as
  `internal/uiclient/live_emphasis_attr_test.go`, byte-identical
  (`sha256 e89bfe78cef0…`), plus the tracked handoff
  `devdoc/handoff-20260924-ui-perception.md` §2.3 → line 280: *"the emphasis
  tests are text/cell-level; nobody has yet confirmed on the real surface that
  a `**bold**` span shows as a non-default attr run"* — this test is exactly
  that missing live-surface check, and the six tracked
  `internal/uiclient/emphasis_test.go` cases are all text/cell-level. It
  compiles and passes (`ok github.com/nick/gopi/internal/uiclient`).
* `/home/user/work/GoPi` `git status --porcelain` → **empty**.

## Not Done (honest)

* **The durable machine check is NOT in this branch, and cannot be**: the
  affording surfaces (briefing rendering, worker start, the
  `read`/`write`/`edit`/`bash` funnel) are the **system layer**, which by
  Rule 28 belongs to **peerbox**, not HP. Filed as a task instead (below).
* **The divergence itself is untouched**: `/home/user/work/GoPi` is still
  10 ahead / 2 behind `peerbox/master`. Fixing that means merging or rebasing
  the operator's own local master — not a worktree run's call.

### The fix, filed for peerbox (system layer)

1. **Briefing composer should not hand out the main-checkout path.** The run's
   ground is `<worktree of this row>`; the repo identity is what a briefing
   needs, not a path to the checkout the run does not own. Ordering matters:
   the absolute worktree path already lands in the briefing, so a *prohibition*
   without that path leaves the run nowhere to write.
2. **A machine check, because prose has no enforcement.** Two shapes, either
   defends the invariant:
   * *Start/claim-time*: if the briefing's repo path is a **main checkout**
     (not the row's linked worktree), rewrite it to the worktree or refuse the
     claim loudly.
   * *Close-out-time*: before `completed`, if the recipe names the repo's main
     checkout, measure `git -C <main-checkout> status --porcelain` and record
     *untracked entries attributable to this row* on its trail
     (the `completed-unclean` arm already reads the run's **own** worktree —
     extending it to the main checkout is what was missing).

## Verification

* Build: `go build ./...` — see ledger below (run before commit).
* Tests: `go test -count=1 -v -run TestLiveSurfaceEmphasisReachesAttrs ./internal/uiclient/` — PASS.
* Ran: `git -C /home/user/work/GoPi status --porcelain` (the surface this task
  changes — the repository's own state), invoked before and after.
* Result: before 2 entries, after **empty**; the adopted test passes in this
  worktree; the live run's artifact is recoverable at the path named above.
* Commit: see the branch tip.

## Evidence (self-filed)

* Dirtiness, before: `git -C /home/user/work/GoPi status --porcelain` → exactly
  the two `??` lines quoted in §1.
* Dirtiness, after: same command → **no output**; count 0.
* Pull blocker: `git rev-list --left-right --count peerbox/master...master` → `2  10`; `pull.ff=only`.
* Authorship of the pollution: briefing line quoted in §2 (session jsonl), the two write recipes in that session (`mkdir -p /home/user/work/GoPi/devdoc/evidence/tui-two-defects`, `cp $M/internal/uiclient/…`), and `grep -rl defectB-after.txt ~/.gopi/sessions/*/` → the run's own session only.
* No code renders `Repo: `: `grep -rn '"Repo: ' internal cmd --include=*.go` → no hits.
* Live polluter: `pgrep`/`/proc/1578682/cwd` → its own worktree `/home/user/workspace/gopi-01a0dabe3fa17cc8b60166f1456b8ffd`; `gopi task list` → that row `in_progress`.
* Nothing destroys the live run's artifact: the moved directory is listed under `…/attachments/recovered/main-repo-litter/`.