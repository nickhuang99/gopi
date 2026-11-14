<!-- doc-class: historical -->
# The deployment gap behind a dispatch-time guard: three layers, and why "rebuild it" is wrong

Measured 2026-10-14 on host `hostb-Laptop`, task
`01a137cb-b958-7a03-b8f0-7bad6acf267c` (epic from row `01a13781`). Every number
below was observed in that run; the raw transcript is in
`~/.gopi/sessions/01a137cb-b958-7a03-b8f0-7bad6acf267c/artifacts/`.

## The symptom

A dispatch-time guard (`TaskBriefingPremiseBaseRefresh`,
`internal/tools/task_briefing_premise.go`) is **correct in the source**, **wired at
the ONE spawn seam** (`cmd/agent/cli_tasks.go`, `runTaskRunOnce`), and has been
green for 74 commits — and the machine that dispatched the incident ran an
artifact that never contained it. The child therefore received a briefing with no
freshness stamp and paid its budget re-measuring surfaces that were already green.

## Face one — the artifact (`strings` is the instrument)

```
$ ls -la /usr/local/bin/gopi
lrwxrwxrwx 1 root root 29 Aug  6 15:09 /usr/local/bin/gopi -> /home/user/work/GoPi/bin/gopi
$ sha256sum /usr/local/bin/gopi /home/user/work/GoPi/bin/gopi | cut -c1-20
5e0340ee9d301c1601a1   (both — ONE file, not two)
$ stat -c '%y %s' /home/user/work/GoPi/bin/gopi
2026-10-14 02:20:55.653025947 +0800 67071588
$ strings /home/user/work/GoPi/bin/gopi | grep -c "a premise was RE-MEASURED at dispatch"   # sibling
1
$ strings /home/user/work/GoPi/bin/gopi | grep -c "BASE REVISION this briefing names"       # this clause
0
```

Timeline arithmetic: `b0890a420` landed the clause **and** its wiring at
`05:04:37`; the binary predates it by 2h43m; `git rev-list --count b0890a420..refs/remotes/gitPushcheck/master`
→ **74**.

## Face two — the seam DOES fire, so deployment is the whole story here

Driven over the incident's own received prompt (`[EPIC GOAL]`, 10257 chars,
extracted from the run's session log), the seam produced a 978-character stamp —
`the base this briefing names, 'bbf8d976', is 45 commit(s) BEHIND
refs/remotes/gitPushcheck/master … RE-MEASURE BEFORE REPAIRING`. So the verdict
"the guard has a coverage hole on this class" would have been **wrong**: the
clause covers it, the artifact did not.

## Face three — the gap is three layers deep, and the last one inverts the remedy

This is the finding that matters, because the obvious remedy is wrong twice.

1. The artifact attests revision `c3a3f4eae74c123fbc57441a0682f978a0dad1ed`
   (`gopi version`).
2. That revision **IS** `refs/heads/master` in `/home/user/work/GoPi`
   (`git merge-base --is-ancestor c3a3f4eae… refs/heads/master` → `exit=0`).
3. `RebuildAfterMerge` decides "already current" with
   `gitTip(repo)` = `git -C repo rev-parse --short HEAD` (rebuild.go:342)
   compared through `versionMatches` — so on this checkout it compares the
   artifact against **local HEAD**, finds them equal, and **skips**.
4. But `refs/heads/master` is **97 commits behind**
   `refs/remotes/gitPushcheck/master` — the mirror every ground is cut from — and
   the wiring commit `b0890a420` is **not an ancestor of local master**
   (`exit=1`), whose copy of `task_briefing_premise.go` contains the clause
   **0** times while the mirror's contains it **4** times.

So: the already-ticking close loop cannot repair this class (it reports the
artifact current, correctly by its own definition), and a manual rebuild from this
checkout would produce a binary that still lacks the guard. The honest remedy is
two steps **in order** — bring the checkout's ref current with the integration
mirror, *then* run the declared build — and both are system-layer work (Rule 28).

## The remedy this branch ships: a report, not a checker

`gopi version --drift` (`internal/tools/deploy_drift.go`, wired into the existing
`gopi version` verb) measures, read-only, for the checkout you are standing in:
the revision its `bin/` artifact attests, and its distance behind
`refs/heads/master` and `refs/remotes/gitPushcheck/master`, reusing
`binaryVersion` + `versionMatches` — the rebuild path's OWN probe and comparison,
so the report and the rebuild cannot disagree about what "follows the tip" means.

```
$ cd /home/user/work/GoPi && gopi version --drift
drift-repo: /home/user/work/GoPi
drift-artifact: /home/user/work/GoPi/bin/gopi
drift-revision: c3a3f4eae74c123fbc57441a0682f978a0dad1ed
refs/heads/master:  follows the ref — the artifact was built from it — c3a3f4eae74c123fbc57441a0682f978a0dad1ed
refs/remotes/gitPushcheck/master:  97 behind (this checkout would build from 7d0f7848b9e87e7197e8de9a853a35964e0aa02f)
STALE: …
```

Rule 35 note — the four things: **what breaks without it** is the measured
incident (a guard green for 74 commits and absent from the running artifact);
**why not in-process** is that the mechanism which would do it already ticks and
cannot see this state (layer 3 above), so the missing piece is not automation but
*visibility*; **who owns its lifecycle** is nobody, because it creates nothing —
no cron, no unit, no daemon, no state file, no lock; it is a flag on an existing
verb, writes nothing, schedules nothing, refuses nothing; **what the next person
sees** is a report that names the refs, the distance and the two-step remedy in
its own output, and a report that could not measure says `UNMEASURED` with the
reason rather than printing nothing.

## The coverage hole, decided by measurement

The epic's secondary finding — a briefing whose base declaration is CURRENT (or
absent) while its quoted **measurement** is stale — is real, and this branch
answers it with a predicate plus the measurement that bounds it.

`taskreport.CountClaimsIn` reads "this file carries N rows/lines/permits"
figures; `tools.TaskBriefingCountRefresh` re-measures each against the run's own
ground and stamps `RE-MEASURE THE FIGURE BEFORE REPAIRING` when this ground reads
a different number. The predicate's bounds were **tuned against the real corpus**,
not imagined — the operator's own ledger, 400 most recent payloads:

| pass | firings | what the bounds removed |
|---|---|---|
| 1st | 8 | — |
| + delta/plan filter | 5 | `0 permit(s) added, 168 permit(s) removed`, `would write 1 row(s)`, `+69 new file` |
| + signed-figure filter | 3 | `(+1 row)` |
| + zero-figure filter | 2 | `scans 0 rows` |
| + whole-token file check | **1** | a wrap-tail separator mistaken for a path (`… 167 row(s)` `/`) |

The surviving firing is the incident's own sentence — a true positive. **That
table is the argument for the predicate being narrow rather than absent**: 7 of
the 8 first-pass firings were honest briefings, and a stamp on an honest briefing
costs the child's budget on a surface with nothing wrong with it.

**What the clause does NOT do, stated because the temptation is real**: it counts
**lines**, so it does not re-derive a figure whose unit is not a line (the
incident's own `252 anchor rows scanned`). It says so in the clause text itself —
it names what it counted and sends the child back to the surface that produced the
number.

## Residuals filed

- `01a137cb`'s sibling row (see `artifacts/bug-deploy-drift-local-ref-only.md`):
  `gitTip` measures the LOCAL tip, so a checkout behind its own upstream is
  reported "already current" and skipped. System layer (Rule 28) — filed, not
  fixed here.
- The pre-existing deployment gap itself was filed by the predecessor run
  (row `01a13781`).
