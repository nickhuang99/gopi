<!-- doc-class: normative -->
# Branch lifecycle ruling — `conflict/` is void, and what to do when a branch CANNOT be deleted

> **ROUTING UPDATE (2026-10-16).** The lines below are updated where the ONE writer replaces
> a raw ref removal: `gopi task archive <branch> --reason superseded` mints the archive tag, records
> the WHY and removes the ref, in one arm. The measurement each note carries is unchanged.


Task `01a0af2f-b94d-76e3-8565-b35df602aa05` (supplemental ruling; sibling
`01a0af2b-021a-786c-805e-6367df5f8d99`).
Written 2026-09-17 on `feat/branch-lifecycle-ruling`.

---

## 1. The ruling: `git branch -m <branch> conflict/<branch>` is VOID

Row `01a0ab04` proposed retiring the `archive/<branch>` tag carrier by renaming
a conflicted branch into a `conflict/<branch>` namespace — a "permanent,
distinguishable fossil". **It is withdrawn.**

the operator, 2026-09-17:

> "if we decide to resolve it, then there is no need to rename it, so, just
> combine the task."

The reasoning is not a preference, it is a consequence of where the lifecycle
ends. **The endpoint of a branch's lifecycle is DELETION.** A branch exists to
carry work into `master`; once that is settled — one way or the other — the ref
is a leftover. A rename does not settle anything: it keeps the branch, and a
branch that still exists still needs an owner to answer *"who deletes this, and
when?"*. That question is exactly what the rename was supposed to answer, and it
answers it by asking it again. It is the same shape as the tag it replaces:

| carrier | ref count | the question it leaves open |
|---|---|---|
| `archive/<branch>` tag | 2 (branch + tag) | who deletes the branch? |
| `conflict/<branch>` rename | 1 (renamed branch) | who deletes the renamed branch? |
| **deletion** | **0** | **none — the answer IS the action** |

The proposal was also never built (`git log --all` contains the implementing
commit `c972cdd3`, but it is on no ref — neither master nor any branch; the
whole-tree grep for the namespace returns zero hits outside three planning docs,
§3 below). So there is nothing to un-implement: what remains is to make the void
explicit in the places that still *prescribe* it.

**Hard boundary (this ruling, and the sibling's):** a branch is either in
`refs/heads/` or it is deleted. No third state, no second namespace, no
`conflict/` rename, no `archive/` tag, no "parked" marker. Every disposition
below is expressed in those two states.

## 2. The three cases where a branch CANNOT be deleted

Three situations genuinely cannot end in a deletion **this tick**. Each gets one
disposition, and the disposition must not re-trigger every tick.

### ① Ownership is undecidable (whose branch is this? is anyone working it?)

**Disposition: touch nothing. Report `undecidable`, naming the signals read.**

This is §3.5's asymmetry, and it is deliberate: skipping a branch costs one cron
interval; taking over live work costs two writers in one worktree. §3.5 already
forbids a verdict from a single signal and names the only conclusive dead
signal. `gopi task for-branch <branch> --repo <repo>` is the ownership probe
(§1); its exit code 4 is the *named* "no ledger row names this branch" answer —
which is itself a fact (< 4 is I/O failure, not "no owner" — TaskForBranchExitNoOwner).

### ② A peer holds it: live row, live claim, occupied checkout, moving HEAD

**Disposition: touch nothing. Report `live work`.**

Not a hole in the rule — a *different question*, answered by facts, and the
answer is stable: the peer is still working it. Nothing to do but wait one tick,
which is what §3.1 already prescribes (宁可少做，不可误杀).

### ③ The diff is not filed / the rework task is not yet submitted

**Disposition: touch nothing YET — complete the hand-off, then delete, in one
ordered sequence that cannot stop halfway.**

This is the case that produces the loop, so the ordering is the whole answer:

```
(a) archive  : git -C <repo> diff <anchor> <branch-tip> > ~/workspace/<branch>-rework.diff
(b) file     : gopi task submit "rework <branch>: cannot auto-merge into master" \
                 --payload @<briefing> --tags rework,merge --repo <repo> \
                 --attachment <the diff from (a)>
               → READ the answer: "Task queued: <task-id> (status=pending, …)", exit 0
(c) delete   : gopi task archive <branch> --reason superseded   ← only now — the ONE writer: the
               archive tag, the WHY record and the ref removal happen in one arm
```

**Why this ordering is safe, and measured rather than asserted:**

- `--attachment` is not "a copy we hope happened". `task submit` copies every
  attachment into the task's session dir *before* the row is inserted and
  **aborts the whole submit if a copy fails** — `internal/tools/tasks.go`:
  *"A path that is not absolute, not a readable regular file, or whose copy
  fails ABORTS the whole submit."* So the success line (b) is the proof that the
  replica exists inside a queued task.
- Therefore (c) is reachable only when the work has a home: the attachment is
  the work now, and the branch ref is litter.
- The reverse is the guard: **a branch whose diff was archived but whose submit
  FAILED is case ③, not a deletion** — there is no row, so "the attachment is
  the only copy" is false, so nothing may be deleted. Fix the payload and submit
  again; nothing is lost because the branch is still there.
- Never "delete first, the diff is already on disk": the diff on disk is not the
  deliverable, the attachment *inside a queued task* is, and a crash between the
  two orderings loses the commits permanently (a branch ref is the only handle
  on them; once it is gone the commits are reachable from nothing and are
  GC-able).

This is the sibling's §3.6 deletion sentence, stated from the ordering side; the
two agree (see §6).

## 3. The next tick — what happens to a retained branch, and why it does not loop

The loop was never a merge bug. It was **the same input arriving every tick**.
Two questions decide it: (i) is a retained branch still in the sweep set, and
(ii) does re-seeing it produce a row?

### (i) The candidate set is read fresh from `refs/heads`, and a worktree-held branch is NOT in it

`repoSweepClassify` (`internal/tools/repo_sweep.go`) walks
`for-each-ref refs/heads` and hands each head to exactly one bucket:

```go
case name == masterBranchName || name == mainBranchName:   inv.Integration++
case protected[name]:                                      inv.Protected++   // ← a worktree holds it
case taskMergeExempt(name):                                inv.Exempt++
case repoSweepArchiveExempt(repo, name):                   inv.Archived++
default:                                                   mergeable = append(...)
```

`protected` is `repoSweepProtectedBranches` — the branches checked out in a
linked worktree. So:

- **case ② (peer holds it) never even becomes a merge candidate.** The tick does
  not try it, does not re-merge it, does not file anything. The loop cannot
  start; the branch is invisible to the merge path by construction while a
  worktree holds it. This is the measurement that retires the objection "isn't
  'do nothing' just the loop in another shape?" — for ② it is not a loop at all,
  it is a stable state.
- **cases ①/③ stay candidates and are RE-REPORTED** (deliberate: re-reporting is
  the only signal that the branch is still waiting — one git probe per branch per
  tick). What matters is the next paragraph.

### (ii) Re-seeing it does not manufacture a row — the ledger delta is the loop

- **case ①** : an undecidable branch is *reported*. There is no "submit on
  undecidable" arm anywhere — no `if undecidable { submit }`. Zero rows by
  construction. `repo_sweep_notff_test.go`
  (`TestRepoSweepNonFFTickWritesNothing`) pins the mechanism's invariant with an
  ABSENCE check: a tick with nothing to land must change the ledger by exactly
  **0** rows.
- **case ③** : the hand-off is complete within the tick — the diff is archived
  and the submit succeeds *before* the branch is deleted. A half-finished (a)
  or a failed (b) leaves the branch, and the next tick retries the **same**
  hand-off. Two possible shapes, and both must be named:
  - Retry the *same* submit: row count stays 1 → **no loop**, one pending row.
  - On a NOT-ff branch the merger's own lane files nothing at all
    (`repoSweepNotFFOutcome`: *"no rework row filed here; the take-over path
    hands its diff over"*), so on this path there is not even a first row.
  - The dangerous third shape would be *a second row for already-queued work*,
    and the code already refuses it in two independent places:
    `reworkSummaryLive` (the branch-rework lane's dedup: the deterministic
    summary `resolveRowReworkSummaryPrefix + <branch>`, live statuses only) and
    `resolveRowHasOpenRework` (the same identity, held until the reworker closes
    the row). So re-seeing the branch costs a retry of an idempotent step.

### (iii) The three loop shapes, and which one is actually closed

| loop shape | mechanism | closed by |
|---|---|---|
| `park → rework → park` | every tick minted `archive/<branch>` + a row for a non-ff branch | **already removed** (commit `925194b0`, epic merger-remove-park): the non-ff outcome reports and writes nothing |
| `rework filed → branch stays → next tick files a SECOND rework row` | the branch is still in the sweep set after its work was queued | **delete (case ③)** + the live-row dedup that already prevents the second row |
| `retained branch → new "rework" row every tick` | would require a submit arm on the retained-path | **does not exist**: ① reports, ② is not even a candidate, ③ retries an idempotent hand-off |

A retained branch is therefore either (a) not a merge candidate at all (②), or
(b) re-reported with no ledger write (①), or (c) transiently retained while an
idempotent hand-off completes (③). None of the three re-files work every tick.
The one shape that *would* loop — a fresh row per tick for the same branch — is
refused by the live-row dedup, which is why case ③'s disposition is "complete the
hand-off", not "park it and hope".

### (iv) The honest cost of case ①

Case ① is the one that can persist indefinitely: nobody is working the branch and
nobody can judge it, so it is re-reported tick after tick until a human decides.
That is **not** a loop in the harmful sense — no row is minted, no merge is
attempted, one git probe is spent — but it is unbounded, and the brief's own
warning ("处理了一半的状态没有终点") deserves an explicit answer rather than a
shrug. The answer **within the two-state rule** is the report itself: a
`did not touch: <branch> — undecidable (<the signals read>)` line that names the
branch every tick, which is the standing evidence a human needs to decide it. The
alternative remedies are all carriers this ruling forbids (a marker namespace, a
tag, a "parked" ledger row) — each of which buys visibility by reintroducing
exactly the state that made the loop invisible in the first place.

## 4. What was changed

| artifact | change |
|---|---|
| `cmd/agent/merger_takeover_prompt.md` | **§3.6.2** added: the three cannot-delete cases with their dispositions, the two-state rule, and why a retained branch does not loop (cross-referencing §3.6.1's case table). |
| `cmd/agent/merger_takeover_prompt_gate_test.go` | new gate: the prompt must state the two-state rule and must carry **zero** occurrences of the voided `conflict/` namespace rename. |
| `internal/tools/branch_lifecycle_ruling_test.go` | measured loop-proof: the sweep classifies a worktree-held branch as protected (never a candidate), and a rework row for a branch does not dedup once it is terminal (so the delete — not a retained branch — is what ends the hand-off). |
| `devdoc/requirements/00-module-map.md`, `01-per-module-briefings.md`, `merge-branch-lifecycle.md` | the stale carriers that still *prescribe* the `conflict/<name>` rename now record it as withdrawn by this ruling (G-7 in `merge-branch-lifecycle.md`). |
| `briefings/brief-conflict-branch-rename.md` | the brief that carries the voided mechanism is marked VOID at the top, with a pointer to this ruling — it is the payload an agent would otherwise still follow. |

## 5. Not Done (honest)

- **Case ① has no terminating mechanism, by design.** It ends when a human
  decides (or does not). Recorded above rather than papered over with a carrier
  the ruling forbids.
- **Prompt §3.6.1 is the sibling's (`01a0af2b`).** This ruling adds §3.6.2 and
  does not restate, rewrite or override §3.6.1's six-case table or its "delete
  is the endpoint" sentence. Both tasks edited
  `cmd/agent/merger_takeover_prompt.md` in the same region (§3.6 → §3.7), so the
  merge is a both-added hunk: **both texts are wanted**, and the resolution is to
  keep §3.6.1 followed by §3.6.2.
- **The two planning docs under `devdoc/requirements/` were written before the
  void and are requirement transcripts.** They are annotated (withdrawn), not
  rewritten: their value is as a record of what was decided and then reversed.

## 6. Coordination with sibling `01a0af2b`

- The sibling owns: prompt §3.6's deletion sentence + its §3.6.1 six-case
  lifecycle table (delete is the endpoint; ordering diff → queued → delete).
- This task owns: the **canonical ruling document** (`conflict/` void, the operator's
  reason quoted), the **three cannot-delete dispositions** (①/②/③ as posed in
  the brief), the **loop analysis** (§3), the **retirement of the stale
  `conflict/` carriers**, and a **measured loop-proof test**.
- No duplicate implementation: nothing in this task rewrites the sibling's text,
  and nothing in the sibling's work states the three cases or the loop proof.
- **The two agree on the substance**: deletion is the endpoint, the delete comes
  after the hand-off, and there is no third state.
