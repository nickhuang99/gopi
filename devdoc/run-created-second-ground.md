<!-- doc-class: historical -->
# The Second-Ground Axis — a run that creates its own ground now records it

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Epic**: 01a0dff1 · **Date**: 2026-09-27 · **Verdict**: DOABLE, delivered in one session
**Root-cause target**: the PRODUCER of "a live ground with no owner" (the wording
half was sibling fix `64b6952d`).

## 1. The defect, measured

Row `01a0ddc4-c410` (session `3592a5ef`) ran, mid-flight:

```
git worktree add /home/user/workspace/gopi-evi-pushback -b fix/pushback-rule31-evidence
```

while its own deliverable blob already named
`…/gopi-01a0ddc4c410…` on branch `task/01a0ddc4…`. The directory it created was
**recorded nowhere**, and three mechanisms each declined to be the one that
notices:

| record | why it held nothing |
|---|---|
| the row's deliverable blob | the primary worktree/branch are single-valued, and the writer refuses to overwrite an existing stamp — correctly, because a detection seam must never REBIND a row's own ground (the 2026-09-12 foreign-owner hijack) |
| the creation-detection seam | its candidate loop `return`ed at the FIRST candidate that resolved, and candidate #1 IS the run's own ground by construction — so the created directory was never probed |
| the run-ground anchor | `TaskGroundAnchorRecord` is called from that same seam, which had already returned: the sink held **154** anchors and **none** for `gopi-evi-pushback` |

Consequences, both observed: an owner-less LIVE ground — `gopi task for-branch
fix/pushback-rule31-evidence` answered *"no owner"* while the branch sat in a
registered linked worktree (tip `42fc17d6`) — and no row to resume
(`gopi worker --resume --branch B`: *"no ground to resume"*).

## 2. The repair: a PLACE for the fact

No new store, file, key-value box or daemon (Rule 35). The axis rides the ONE
deliverable blob the ledger already keeps, written by the SAME writer family:

```
{"repo":"…/main","branch":"task/…","worktree":"…/gopi-…",
 "grounds":[{"repo":"…/main","branch":"fix/…","worktree":"…/gopi-evi-pushback"}]}
```

* `internal/tools/task_session.go` — one parser, one rebuild. `taskDeliverableParse`
  now returns a **struct** carrying `Repo/Branch/Worktree/Grounds/Attachments`;
  `taskDeliverableRebuild` takes that struct. A writer that rebuilds from a parse
  therefore carries the **whole** blob forward — the attachments precedent
  (a backfill must never drop the manifest) extended to the grounds axis, so a
  future axis cannot be silently erased by a writer that never heard of it. All
  six mutation call sites were threaded through the struct.
* `internal/tools/task_deliverable_grounds.go` (new) — `taskBackfillDeliverableGrounds`
  appends ONE secondary ground, with the family's ONE write shape (parse → mutate
  one axis → rebuild → CAS on the raw blob read back). It refuses, silently and by
  contract, when the "second" ground IS the row's own (compared on both axes with
  the family's path comparator) or is already listed.
* `internal/tools/task_deliverable_worktree.go` — `taskBashWorkdirCandidates` split
  into `taskBashWorkdirCandidateGroups`, which returns **bound** and **created**
  directories separately. The bound group keeps the old first-hit-wins rule (a
  primary axis is single-valued); the created group is walked on its own. Which
  axis a created ground lands on is decided by **what the row already records**,
  read once — not by the resolver's return, which reports *resolved* even when its
  stamp was refused (that ambiguity silently swallowed the whole pass during
  development; the first cut of this file had exactly that bug, and the test above
  caught it).
* The lookups — `TaskForBranch`, `TaskForWorktree` — match the plural axis through
  the same ONE scan, and answer with a **provenance** sentence naming the created
  ground AND the row's own, because a reader checking `gopi task show <id>` sees a
  different primary pair.
* `task show` renders `deliverable.grounds:` (one line per ground) — the axis
  existed in the blob but in no render, and the provenance sentence points at it.

## 3. What was deliberately NOT done

* **No rebinding, ever.** A created ground never touches the primary axes; the
  row's own ground stays authoritative. The e2e run asserts this on the raw blob.
* **No amnesty for pre-existing rows.** A row that recorded no ground, and whose
  ground has since been recycled, stays refused. The incident's live ground is
  fixed by the *producer* recording the fact going forward. Reconstructing a
  mis-bound row from a dangling `deliverable.branch` is a separate, riskier
  question (it means guessing a ground from a ref) and is not silently smuggled
  in here.
* **Nothing weakened.** The resume refusals (`64b6952d`) are untouched — the no-row
  refusal must simply stop being TRUE, which is what the producer fix achieves.

## 4. Evidence

* **Red-first, in place**: neutering the two halves (the seam walks no created
  ground; the lookups read only the primary axes) reproduced the incident exactly
  — `for-branch <created>` → *"no owner"*, blob with no `grounds` key. Restored.
* **A real run** (Rule 31): `scripts/…` — see the run's own report for the verbatim
  invocation and output. Real `gopi worker <row>` against a real ledger, a real
  git world, and a real child agent process; only the language model is stubbed
  (a local Anthropic-shaped SSE endpoint), because the seam fires from the agent's
  own bash round and that decision must be the incident's, not a coin flip.
  Observed afterwards: `deliverable.grounds:` on `task show`, exit 0 + provenance
  on `for-branch <created>`, exit 0 on `worker --resume --branch <created>`.
