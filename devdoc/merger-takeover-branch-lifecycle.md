<!-- doc-class: normative -->
# devdoc: one branch-lifecycle rule set for the merger take-over prompt

> **ROUTING UPDATE (2026-10-16).** The lines below are updated where the ONE writer replaces
> a raw ref removal: `gopi task archive <branch> --reason superseded` mints the archive tag, records
> the WHY and removes the ref, in one arm. The measurement each note carries is unchanged.


> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** Standing claim made
> before the change it describes: the take-over prompt this document specifies
> (`cmd/agent/merger_takeover_prompt.md`) was deleted with the resident merger
> (`f9ff1a89`, `55c4d6f3`), so there is no prompt to keep self-consistent today.
> The branch-lifecycle reasoning is kept as the design record for the
> re-development. See `devdoc/merger-retired.md` and the dated correction in
> `devdoc/merger-self-driving.md`.

Epic: **the take-over prompt contradicts itself on branch lifecycle → the rework
loop never ends** (2026-09-17) · branch `feat/merger-takeover-lifecycle` (from
`feat/merger-takeover-landed` tip `f6ec3c0c`) · worktree
`~/workspace/gopi-takeover-lifecycle`

Deliverable: `cmd/agent/merger_takeover_prompt.md` (493 → 612 lines) +
`cmd/agent/merger_takeover_prompt_gate_test.go` (the gate that pins it).

## 1. The defect, and what the old ban was protecting

The prompt shipped two answers to one question. §3.6 argued the *reason* the
rework hand-off uses a diff — "the diff is **self-contained**. It survives the
branch being deleted, moved or garbage-collected" — and then, four lines later,
banned the very act it had just justified:

```
**Never**: delete an unmerged branch, force a merge.
```

§3.5 repeated it absolutely ("You still do **not** delete the branch") and §3.2
stated a third variant ("do not delete its branch"), while §3.10 said "do not
delete branches … that hold work you did not verify". A model reading three
bans and one justification takes the safe-looking branch: **it never deletes**.
So after a rework task is filed the ref stays in `refs/heads/`, the next tick's
sweep set (§1 = branches not in `master`, read fresh every tick) contains it
again, it fails to merge again, and a **second** rework task is filed for work
already queued. The loop is unbounded and branches pile up. The operator's
judgement on it: **"just delete"**.

**Intent judgement — what the original ban protected, and why it was the wrong
instrument.** The ban was not nonsense; it protected the real invariant
*never lose work*. But it expressed that invariant as a **prohibition on the
delete** instead of a **precondition on the delete**, and those differ exactly
where the defect lives. The invariant is not "a branch ref must exist"; it is
"the work must have a surviving copy somewhere before the ref goes". §3.6 itself
supplies that copy — the diff attachment inside a queued task — so in the rework
case the ban was protecting a ref whose *content* had already been moved, i.e.
guarding the pointer and losing the invariant. Rewritten as an ordering, the
invariant survives intact and the loop dies:

> a branch is deleted when its work has a home; when it has no home, or when the
> home cannot be judged, the branch stays and the case is **reported**.

That single move — ban → ordering + report-case — is the whole change. The
no-work-lost guarantee is *stronger* than before, because "the work has a home"
is now a checked fact (see §4) rather than an implication of leaving a ref
behind.

## 2. The new lifecycle, in one place: §3.6.1

New section `<!-- takeover-section: §3.6.1 lifecycle -->` — "★ Branch lifecycle —
one rule set, six cases". Every other place in the prompt that used to state a
lifecycle verdict now cross-references it instead of restating it (that
restating is how the document drifted into three bans).

| # | case | verdict | action |
|---|---|---|---|
| ① | you landed it (ff, or a clean merge you committed) and `master` is verified | **merged** | `gopi task archive <branch> --reason superseded` once no worktree holds it |
| ② | `git merge-base --is-ancestor <branch> master` exits 0 | **already merged** | `gopi task archive <branch> --reason superseded` |
| ③ | `git rev-list --count master..<branch>` == `0` (empty shell) | **no work left** | `gopi task archive <branch> --reason superseded` |
| ④ | it cannot merge cleanly (§3.6) — diff archived **and** the rework submit SUCCEEDED | **rework filed** | **`gopi task archive <branch> --reason superseded`** — the attachment is the work now |
| ⑤ | you cannot judge the branch's ownership or state (§1, §3.5) | **undecidable** | **touch nothing** — no merge, no delete; report |
| ⑥ | a live peer holds it (live row / live claim / occupied checkout / moving HEAD) | **live work** | **touch nothing**; report |

Notes on the shape of the rule:

- **`-d` vs `-D` is deliberate.** ①②③ are cases where git itself already knows
  the work is in `master` (ancestry / empty rev-list) or where the take-over
  just did the merge, so the safe form suffices and it *fails safe*: if the
  merge did not actually land, `-d` refuses. ④ is the case where git cannot see
  the landing (the work was **not** merged) so the force form is required — it is
  reachable only after the archived+queued check below.
- **"no third state" is stated explicitly**: a branch is in `refs/heads/` or it
  is deleted. No `conflict/<branch>` rename, no `archive/<branch>` tag, no
  "parked" marker. Rule 9 forbids archive tags as a hiding mechanism, and a
  rename/tag only re-creates the "who deletes this, and when" question the
  section answers.
- **⑤/⑥ are not holes.** They are the only two cases where a branch
  legitimately stays, and they are **reported** (`did not touch:` line in the
  §3.11 report template) so the loop is a *visible* re-report that a human can
  end — as opposed to the old *invisible* loop where every tick re-did a rework
  filing nobody could see repeating.

## 3. The contradictions, and what each became

| old text | where | new text |
|---|---|---|
| `Never: delete an unmerged branch, force a merge.` | §3.6 | `Never: … delete a branch **before its work has a surviving copy** — the ordering in §3.6.1 case ④ (diff archived → `Task queued:` → *then* the delete) is the whole of that rule.` (the `git tag archive/<branch>` and "never force a merge" halves of that line are kept — they are correct and Rule 9 needs them) |
| `You still do **not** delete the branch` | §3.5, DEAD verdict | `The **branch** is then governed by the lifecycle rules like any other branch (§3.6.1): it is deleted once it has landed, or once its diff is archived and its rework task submit has succeeded — and never merely because its author died.` |
| `do not delete its branch` | §3.2, occupied worktree | `→ Do nothing — that is case ⑥ of §3.6.1: a live peer's branch is reported, never merged and never deleted. Report it.` |
| §0 rule 1 implied the delete was forbidden | §0 | `read "must move" literally: this rule requires the copy, **it does not forbid the deletion**. Once the copy is made and (for a rework) the task is queued, a stale branch ref is litter and is deleted (§3.6.1, cases ①–④).` |
| §3.6 step 4 `file the rework task; leave the branch for the…` | §3.6 | `**file the rework task and DELETE the branch** — the diff IS the work now (§3.6.1 case ④). Do not park, retag, rename or leave it for the next tick.` |
| §3.10 `Do not delete branches, worktrees or tags that hold work you did not verify is already in master` | §3.10 | same sentence + `— **unless** its work has a surviving, verified copy in a queued task, which is case ④ of §3.6.1 and the single exception to this line. In every other situation a branch you cannot account for is case ⑤/⑥: touch nothing and report it.`, plus a new bullet `Do not invent a third branch state.` |
| §3.3 `Delete the branch *only* if it is also an empty shell …` | §3.3 | kept, now explicitly `(These are cases ② and ③ of §3.6.1 — where the delete is the endpoint, not a risk.)` |

`grep` over the shipped prompt for all three removed phrases returns **nothing**
(exit 1) — see §6.

## 4. The ordering guarantee (deliverable 2)

Stated as §3.6.1 "④ in full", with the invariant written as an *ordering* and
each link justified by a machine fact:

```
1. anchor = git merge-base master <branch>
2. git -C <repo> diff <anchor> <branch> > ~/workspace/<branch>-rework.diff
3. gopi task submit "rework <branch>: cannot auto-merge into master" \
     --payload @<briefing file> --tags rework,merge --repo <repo> \
     --attachment ~/workspace/<branch>-rework.diff
4. READ the submit's answer. It must be the SUCCESS line, exit status 0:
        Task queued: <task-id> (status=pending, source=…, tags=rework,merge)
   Anything else — non-zero exit, usage error, unreadable diff path — is a
   FAILED hand-off: fix it and submit again. A failed submit is case ⑤ (the
   work has no home yet): do not delete anything.
5. ONLY NOW: gopi task archive <branch> --reason superseded   # (a) archived, (b) queued — in that order
6. remove the worktree that held it if it is yours to free and it is clean
   (§3.2) — a dirty one is committed first, never discarded
7. the payload must say: the original branch was DELETED and its diff is
   attachment <path> in this task, so the reworker starts a FRESH branch from
   current master and does not go looking for the old ref
```

The invariant is spelled out as (a)/(b)/(c) and the prompt is explicit about the
one subtlety that makes `--attachment` alone *insufficient* as proof:

- **(a)** the diff is archived **where the task recorded it**. `--attachment` is
  not "a copy we hope happened": `task submit` copies every attachment into the
  task's session dir **before the row is inserted** and aborts the whole submit
  if a copy fails (`internal/tools/tasks.go`, verified by reading the submit
  path) — so the submit's success line means the replica exists.
- **(b)** the submit **RETURNED SUCCESS** — `Task queued: <id> (status=pending,
  …)` with exit 0. A failed submit created no row, and with no row "the
  attachment is the work's only copy" is false.
- **(c)** the delete comes **third**. The writer's ref removal is reachable only after
  (a)+(b): deleting first and filing second loses the work in the window between
  them, and a crash in that window loses it permanently.

Two consequences are stated because they are where a reader would otherwise
improvise: the diff **on disk** is not the deliverable (the attachment *inside a
queued task* is — a `/tmp` file is not a home), and the ordering is also what
makes **recovery** possible (a failed submit leaves the branch in hand, so the
payload is fixed and the submit retried with nothing lost). Read backwards it is
the same rule: diff archived + submit FAILED = case ⑤, touch nothing.

## 5. Why the next tick no longer sees it

A short subsection under §3.6.1 says it in the prompt, because the mechanism is
the justification for deleting at all:

The sweep set is *computed*, not stored: §1 defines it as "branches that are not
in `master`", read fresh from `refs/heads` each tick. A branch deleted in case
①–④ is therefore **not in the next tick's input** — there is no state to
remember, no marker to expire, no suppression list to maintain, no `archive/`
tag to consult, and nothing for a future agent to clean up. Deleting the ref is
not a cleanup after the decision; **it is the decision taking effect**. The
cases ⑤/⑥ that stay in the sweep set stay *by design*, and they are the visible
ones (reported every tick until a human or the rework owner resolves them).

## 6. Verification

```
$ cd ~/workspace/gopi-takeover-lifecycle
$ go test ./cmd/agent/ -run 'MergerTakeover|PromptGate' -v
--- PASS: TestMergerTakeoverBriefingCarriesPromptAndFrame
--- PASS: TestMergerTakeoverReposFromEnvRejectsRelativePaths
--- PASS: TestMergerTakeoverMarkerIsNotInheritedByGrandchildren
--- PASS: TestMergerTakeoverPromptHasNoBranchLifecycleContradiction
--- PASS: TestMergerTakeoverPromptCoversEveryRequiredSection
--- PASS: TestMergerTakeoverPromptIsEmbedded
--- PASS: TestMergerTakeoverPromptGateIsCwdIndependent
ok  	github.com/the operator/gopi/cmd/agent	1.563s
$ go vet ./cmd/agent/ ; echo $?
0
```

Acceptance greps (`cmd/agent/merger_takeover_prompt.md`):

```
$ grep -n 'delete an unmerged branch\|you still do \*\*not\*\* delete the branch\|do not delete its branch'
(no output, exit 1)                      ← no surviving half of the old contradiction

$ grep -n 'task archive <branch>\|Task queued:\|no third state' …
373:| ④ | … | **`gopi task archive <branch> --reason superseded`** — the attachment is the work now |
402:        Task queued: <task-id> (status=pending, source=…, tags=rework,merge)
406:5. ONLY NOW: gopi task archive <branch> --reason superseded   # (a) archived, (b) queued — in that order
```

**RED-PROOF of the new gate half (Rule 11 falsification).** The absence test is
not vacuous — restoring the exact line this epic removed turns it red:

```
RED-PROOF: restoring the removed ban
--- FAIL: TestMergerTakeoverPromptHasNoBranchLifecycleContradiction (0.00s)
    merger_takeover_prompt_gate_test.go:157: the prompt still carries the
    contradicting phrase "delete an unmerged branch" (§3.6 (old) — banned the very
    delete §3.6.1 case ④ requires, while §3.6's own 'the diff survives the branch
    being deleted' argued the opposite)
=== restore + GREEN ===  (all 7 tests PASS, vet exit 0)
```

### The gate change (deliverable 3)

`cmd/agent/merger_takeover_prompt_gate_test.go` — the prompt's coverage gate
pins the shipped TEXT by invisible HTML anchors:

- `mtpRequiredSections += {"§3.6.1 lifecycle", …}` — the new section cannot be
  deleted without failing `TestMergerTakeoverPromptCoversEveryRequiredSection`.
- `mtpRequiredBehaviours += {the routed action cell of the ONE writer, Task queued:, the
  original branch was DELETED, no third state}` — the *positive* half: the whole
  rule set, both delete forms, and the success line that gates the delete.
- **new** `mtpForbiddenContradictions` +
  `TestMergerTakeoverPromptHasNoBranchLifecycleContradiction` — the *negative*
  half: the three old phrases must be absent. They are checked as an ABSENCE
  because the defect WAS a surviving copy; a future edit that re-inlines one of
  them fails immediately, and each entry names the section that replaced it.

Absence alone would be satisfied by deleting the whole lifecycle section — which
is why the two halves ship together (the section anchors + behaviour phrases are
what make the absence meaningful).

## 7. Audit: every other self-contradiction found (and how it was handled)

The epic asked for an audit beyond the three known bans. I read all 612 lines in
section order and checked each §-anchor against every other place it is
referenced. Findings:

1. **§3.3 "Archive tags are history, never a filter" vs §3.6's own `Never: git
   tag archive/<branch>`** — *not* a contradiction (one bans *creating* a tag,
   the other bans *consulting* one), but adjacent enough to be misread. Left as
   is; §3.6.1's "no third state" sentence now states the conclusion for both.
2. **§3.3's "Delete the branch *only* if it is also an empty shell"** vs §3.6.1
   cases ②/③ — *not* a contradiction, on inspection: `--is-ancestor <branch>
   master` exiting 0 and `rev-list --count master..<branch>` == `0` are the same
   condition, so §3.3's "also" was redundant rather than narrowing. It is still
   a *readability* trap (a reader can mistake the sentence for two independent
   preconditions), and §3.3's own trap 2 already wants the count to be the thing
   you check — so the bullet was rewritten to say that directly (ancestry =
   already landed; **confirm with the count**, because that number is checkable
   without trusting the flag's semantics) plus the `§3.6.1 cases ② and ③`
   cross-reference, so all three statements read as one rule. Recorded here as a
   wording unification, not a repaired contradiction.
3. **§3.5 "you may free the dead peer's worktree" vs §3.2 "Do nothing if
   occupied"** — reconciled by §3.2's occupied case naming `readlink
   /proc/<pid>/cwd` (a *live* process) while §3.5's take-over presupposes the
   DEAD verdict, i.e. no process at all. Not a contradiction once both
   conditions are read; §3.5's rewritten paragraph now says the branch is
   "governed by the lifecycle rules … never merely because its author died",
   which removes the impression that death authorises a *delete*.
4. **§0 "Doing nothing is a correct, successful outcome" vs §3.6.1 "delete is
   the endpoint"** — the only apparent tension left, and worth stating plainly:
   doing nothing is a correct outcome for a *branch you cannot judge* (cases
   ⑤/⑥), not for one whose verdict is already known (①–④, where doing nothing
   *is* the loop). The §3.6.1 ⑤/⑥ paragraph states exactly this ("a visible
   re-report is information, while the loop this section removes is the
   invisible one").
5. Non-contradictions checked and deliberately left alone: §3.4's ~20/~45 min
   caps vs §3.9's "never hold the lock across your whole run" (different
   objects — a branch vs the flock); §3.8 "revert a bad merge" vs §3.9 "the
   rebuild is part of landed" (revert is for broken `master`, a failed rebuild
   is explicitly **not** a rollback, Rule 12); §3.7 "do not resume" vs §3.3
   "land orphan branches" (a session vs a branch).

## 8. Not done / honest gaps

- **No LLM-adoption run of the rewritten prompt** (Rule 11 asks for one: the
  change is a prompt change, and the honest acceptance is a real take-over tick
  that deletes a rework-filed branch). What is proven here is the *text* gate
  (section + behaviour + absence, with a red-proof). A behaviour run needs a
  repo with a genuinely unmergeable branch and a live `gopi merger` tick; I did
  not stage one on this host, so the claim "the model now deletes" is
  **unproven** and should be checked on the next real rework filing.
- The gate pins the *phrases*; it cannot pin that §3.6.1's six cases stay
  mutually exclusive as the text evolves. The section is now the single source
  of truth for lifecycle, so any future lifecycle statement belongs there — that
  is a review discipline, not a machine check.
