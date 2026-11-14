<!-- doc-class: historical -->
# `gopi task branch-refs` — the peer-ref stranded-delivery audit


## The problem it answers

Epic 01a12d5b made the close-out chain RECORD a withheld delivery on the row's
own trail. That answers the question for every row stranded from 2026-10-12
onward and for **none of the rows stranded before it**: an event written at
close-out time cannot exist on a row that closed before the writer existed. On
this host the measured population was 8 rows in that state, and `task list` /
`task show` render them exactly like delivered rows.

The only way to find them was to run `git ls-remote` against the ledger by hand.
This verb IS that manual loop, made one read.

## What it is

`gopi task branch-refs --peer ALIAS [--repo PATH] [--only withheld] [--json] [--db PATH]`

READ-ONLY. It reads **this profile's ledger** and **one peer**, and writes
nothing: no push, no delete, no status move, no event. It names rows; it never
repairs them. The question is asked of the DELIVERY'S OWN CHANNEL —
`git ls-remote --heads`, the channel the close-out push wrote through — and not
of the row's history, which is precisely the source that was silent.

## The verdicts

| verdict | meaning |
|---|---|
| `withheld` | terminal row whose branch this host STILL HAS and the peer does not advertise. **Actionable**: handing it over is a push, and a push is idempotent. |
| `on-peer` | the peer advertises the branch, **or** its commits are already ancestors of the peer's trunk (delivered by LANDING — the merger consumed the ref). |
| `unresolved` | not advertised AND gone from this host too: the delivery cannot be established in either direction, and nothing can be re-pushed. |
| `exempt-branch` | `master`/`main`/`archive/*`: never delivered anywhere by design (Rule 6a). |
| `peer-unreadable` | the peer could not be asked. NO row is judged withheld. |

### Why `unresolved` is a verdict of its own (the measured reason)

The first shape of this file tested ref existence alone. Against the live ledger
that produced **1184 of 1214** rows labelled "not advertised" — the ordinary tail
of two years of merges whose branches were cleaned up after landing. Calling
those `withheld` drowns the ~12 that are genuinely actionable, which is the very
defect this readout exists to remove, reproduced inside the readout. The
`merge-base --is-ancestor` test is what separates them, and it is what makes the
green half of the acceptance true: `task/01a12d3e81c1…` carries no peer ref
while its commits sit on the peer's master (**verified independently**:
`git merge-base --is-ancestor task/01a12d3e81c17028bddb24143aa4a338
refs/remotes/branch-refs/-home-nick-work-GoPi/master` → 0).

## The exit algebra IS the contract

| code | meaning |
|---|---|
| 0 | the peer was read; nothing withheld |
| 2 | the peer was read; ≥1 row withheld |
| 3 | the peer could **NOT** be read — no verdict issued |
| 1 | usage (no `--peer`, unknown flag, unreadable ledger) |

3 is deliberately distinct from both 0 and 2: "I could not look" is not an
answer, and a script that collapsed the two would act on a measurement nobody
took (Rule 15).

## Design decisions worth keeping

1. **Judge everything, bound only the print.** The first implementation sliced
   the candidate list to 200 before judging — so the withheld COUNT was a slice
   and the row set was whatever SQL happened to order last (measured: 1216
   candidates vs a 200-row bound). Every candidate is now judged and the bound
   applies to the **tail only**; **every withheld row is always carried**, in
   text and in `--json` alike (`taskBranchRefsBoundPrint`).
2. **Per repo, not per row.** The question is per row but the facts are per repo:
   "which branches exist here" and "which of them already landed on the peer's
   trunk" are the same two answers for all ~1200 rows that record one repo.
   Asked per row it was two git processes per row and the verb took >2 minutes;
   asked per repo it is seconds. Same principle as the peer probe: one read
   answers the whole ledger, so the answer cannot depend on how many rows it holds.
3. **A private remote-tracking namespace**, `refs/remotes/branch-refs/<slug>/master`
   — deliberately NOT the pushback lane's (`gitPushcheck`), which a pruning pass
   owns; two readers sharing one ref would each delete the other's evidence.
4. **A ref under `refs/remotes/` here is an OBJECT SOURCE, not an AUTHORITY.**
   The authority for "does the peer hold this branch" stays the `ls-remote`
   advertisement; the fetched trunk exists only so the ancestry test has objects
   to walk.
5. **A path peer is a whole repository.** `taskBranchRefsPeerTarget` appends the
   peer checkout path for an ssh-alias peer (matching `finishCleanupServerTarget`
   — the reader asks about the repository the writer actually pushed to), but a
   PATH operand is used as-is. Without the distinction the fixture's own repo
   became `…/peer-repo:/home/user/work/GoPi` and every row read `peer-unreadable`.

## Verification recipe (reuse this)

```bash
go test ./internal/tools/ -run TestTaskBranchRefs -count=1     # red/green/fail-closed/order/three-answers
go test ./cmd/agent/ -run 'TestTaskHelpSurface|TestTaskVerbBranchRefs' -count=1
# real run, live ledger, live peer:
gopi task branch-refs --peer peerbox --repo /home/user/work/GoPi --only withheld ; echo $?
gopi task branch-refs --peer peerbox --repo /home/user/work/GoPi --json   # probe_read_at + local_unread
# independent check of ONE row:
git ls-remote --heads peerbox refs/heads/<branch>            # empty = not advertised
git merge-base --is-ancestor <branch> <peer-master-mirror> # 0 = delivered by merge
```

## Known defect (filed, then FIXED — 2026-10-12, task 01a12dba)

The verb returned a FALSE NEGATIVE on rows of its own acceptance sample
(`01a12d22`, `01a12d2c`, `01a12d2f`, `01a12d30`, `01a12d32`, `01a12d3f`,
`01a12d47`): measured 09:28 on this host, each branch existed locally
(`show-ref --verify` rc=0), was not advertised by the peer, and was not merged —
i.e. each WAS stranded — yet none appeared in the withheld set.

**The mechanism (measured, not inferred).** The rows record the GROUND they ran
in as their `deliverable.repo` (`/home/user/workspace/gopi-<task>`) while the
ref lives in the repository that owns it (`/home/user/work/GoPi`, the delivery
repo). The close-out chain reclaims the ground when the run ends, so by the time
the audit ran, `taskBranchRefsRepoUsable(row.repo)` was false for every one of
those rows, `loadRepo` left the per-repo cache empty, and the two-value question
`hasLocalRef` read that empty cache as **"the branch is gone from this host
too"** — reporting a live, one-push-away delivery as cleaned up. The probe
directory (`--repo`) knew better the whole time; nothing ever asked it.

**What the fix changes (three separate answers, not two).**
`localRefState` now distinguishes `present` / `absent` / `unchecked`, where
`unchecked` means *this run never read the repo* — the state the two-value
question could not express and flattened into "absent". A branch found in the
run's own probe directory answers `present` (a positive answer, which is what
makes the ordinary reclaimed-ground row actionable again); a repo that was read
and does not carry the branch still answers `absent`, so the 1208-row tail is
unchanged. `local_unread` is now carried per row and counted in the report, so
"unresolved" can never silently absorb "I never asked".

**Two honest limits, stated so they are not read as coverage.**

1. The union of the row's repo with the probe directory recovers exactly the
   strands whose delivery repository is the one the audit was pointed at. A row
   whose branch lives in some *third* repository is reported `local_unread`, not
   `withheld` — the reader does not go looking through the filesystem for where a
   branch might be, because that would be a guess wearing a measurement's clothes.
2. The audit reads the peer's advertisement at one instant (`probe_read_at`), and
   that read is unstable: measured 2026-10-12, `git ls-remote --heads peerbox`
   answered 21 refs at 09:05, 2–3 refs from 09:27 on, and 1–3 refs thereafter
   while `refs/heads/master` held the same hash. The withheld/unresolved split
   therefore also drifts between runs. Read the timestamp before acting.

**Lesson for the next reader of this file**: the false negative was not in the
git plumbing. It was a *bool* replacing a three-valued question — the audit could
not say "I did not look", so it said "it is not there".

