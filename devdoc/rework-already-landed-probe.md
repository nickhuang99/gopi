<!-- doc-class: historical -->
# The already-landed probe: does the integration branch already carry this branch's change?

**Task** `01a1310a-a6f8-71f1-86a9-3da940335fa8` · branch
`task/01a1310aa6f871f186a93da940335fa8` · host peerbox · git 2.34.1 · 2026-10-13.

## The defect, measured

A merge run filed a branch-rework row (`01a12b85-…`) for branch
`task/01a12b6388b57199892195533ff2aaa8` (tip `8fafbafee`, "fix(code_search):
accept a ctags index whose pseudo-tag block is not at byte 0"). Master already
carried a commit with that **exact subject** — `ffa1cf4f8`, from the identical
base — because a sibling branch had landed the same fix written separately.

Nothing in the rework briefing said so. The briefing could only say *why the
branch did not merge* (`merge_ceiling_rework.go`'s `MergeCeilingPayloadFmt`), so
the executor re-derived "is this already on master?" by hand, with `git log -S`,
at the END of a run instead of at the start. The row's own briefing, verbatim:

```
REWORK TASK — branch task/01a12b6388b57199892195533ff2aaa8 cannot be auto-merged into master.
…
ACCEPTANCE
1. Rebase (or merge master into) the branch; resolve both conflicts preserving BOTH sides' semantics:
   - internal/tools/symbol.go: master changed the same region; the branch's fix … must survive.
```

## The measurement that chose the discriminator

The probe one reaches for first does **not** answer this question. Measured on
the real pair, at `/home/user/work/GoPi`:

```
$ git show 8fafbafee --format= | git patch-id --stable
088b1f30fa26082cd0f00099771de720acd5119d   ← the branch's tip
$ git show ffa1cf4f8 --format= | git patch-id --stable
ec53b3cf3f7db46b99d212912705b5af4d6aa506   ← the landed commit
$ git cherry ffa1cf4f8 8fafbafee
+ 8fafbafee88258bb0c01b9af6033fee29c3cd627 ← "+" = patch-unique, i.e. NOT landed
```

The two fixes were written **twice**, not copied, so their patch-ids differ and
`git cherry` reports the tip as unique work — the exact inversion of the truth.
The discriminator that DOES fire is the commit **subject**, which is
byte-identical (`git log -1 --format=%s` on both commits prints the same line).

So `ProbeReworkLanding` compares subjects, and the section it renders **says
which discriminator it used and which ones do not work** — otherwise the next
reader "simplifies" it back into patch-ids and silently restores the defect.

## The second defect: a recency window made the probe lie (found by running it)

The first implementation read the newest **400** commits of the integration
branch (`git log -400 --format=%H%x09%s refs/heads/master`) and matched the
subject in Go. It passed its tests and was wrong, in the worst available way:

```
$ git rev-list --count ffa1cf4f8..master     # /home/user/work/GoPi
1313
```

The landed commit of the very pair this probe exists for sits **1313 commits
below master's tip**, so a 400-commit window never sees it — and the probe then
prints `NOT ALREADY LANDED (probe ran)`: a **confident false negative**, stated
with exactly the same authority as a true negative. The tests did not catch it
because every fixture's landing was 1–2 commits deep.

Two things changed, and both are pinned:

- **No window.** The subject is filtered **in git over the whole history**:
  `git log --fixed-strings --grep=<subject> --format=%H%x09%s refs/heads/master`
  (measured: 0.19 s over master's 10 614 commits). `--fixed-strings` because a
  commit subject is LITERAL text — `fix(x): [a-z]+ (v2)` is a regex, and a regex
  match is not an equality.
- **The cap is on HITS, not on the walk** (`reworkLandingHitScanCap`), so it
  bounds the probe's output rather than its search.

`TestReworkLandingProbeFindsADeeplyBuriedLanding` buries the equivalent commit
450 commits deep and asserts the precondition (`rev-list --count` > the old
window) before asserting the signal — so reintroducing a recency window turns it
red. Writing that test caught a **second** measurement worth keeping: two
**empty** commits with the same parent, message and timestamp collapse to the
**same sha**, so an empty-commit fixture compares a commit to itself and quietly
stops modelling the case. The fixture therefore writes a file per commit (same
subject, different patch — the measured shape).

The old window's blindness was then confirmed on the REAL shas, in a scratch repo
carrying the real pair (`ffa1cf4f8` as master, `8fafbafee` as the branch tip,
buried 450 commits):

```
$ git log -400 --format='%H%x09%s' master | awk -F'\t' -v s="$S" '$2==s'
                                   ← nothing: the old window MISSED the real landing
$ git log --fixed-strings --grep="$S" --format='%H%x09%s' master | awk -F'\t' -v s="$S" '$2==s'
  EXACT MATCH ffa1cf4f806ecfe40c4bc6cc0fc19c07f8ad7daa
```

## What it is not

- **Not a verdict.** A subject match is a signal to read the landed commit. Two
  commits can share a subject and differ in content, and this repo is the proof:
  master carries **456** "delivery-point sync: …" commits and **312** "pre-merge:
  verify branch against peer master before pushing" — one bookkeeping subject
  written on every branch. `reworkLandingSubjectCarriesSignal` therefore refuses
  to found an equivalence on a bookkeeping subject (those prefixes, `Revert `,
  `Merge branch … into …`, `Merge master into …`) or on a subject shorter than 12
  characters, and reports "no signal" rather than a guess.
- **Not a write.** No ledger write, no state file, no cron, no script — one
  in-process `git rev-parse` + `git log` (+ one `git merge-base` for the evidence
  line). Rule 35 is satisfied by construction: nothing survives the process.
- **Not cover for `already-merged`.** A match that *is* the tip (the branch is an
  ancestor, i.e. merged outright) yields no signal: the merge run already reports
  that case as `already-merged` and files no rework row.

## The three answers, one spelling each

`ReworkLandingEvidence.Section()` renders exactly one of:

| case | text |
|---|---|
| `Signal()` | `MASTER MAY ALREADY CARRY THIS CHANGE` — names both shas, both subjects, the ancestry proof, and the patch-id/cherry warning |
| probe ran, nothing matched | `NOT ALREADY LANDED (probe ran)` — states the negative, so absence of a signal is distinguishable from absence of a probe |
| probe could not run | `ALREADY-LANDED PROBE DID NOT RUN (<why>)` — explicitly **not** a statement that the branch is unlanded (Rule 15) |

## Where it is wired

Two lanes, each through ONE builder so the payload text and the test cannot
drift (Rule 14):

- ceiling lane — `mergeCeilingPayload` (`internal/tools/merge_ceiling_rework.go`),
  reached from `gopi merge` when a branch's conflict exceeds the ceiling;
- hand-over escalation lane — `mergeHandoverEscalatePayload`
  (`internal/tools/merge_handover_escalate.go`), reached when the hand-over has
  failed `MergeHandoverFailCap` times.

**Not wired, and honestly so:** the third producer of this row shape is the merge
**LLM child** (`cmd/agent/merge_prompt.md` §3.6, which wrote the measured row
`01a12b85`). Its briefing is model-authored prose, not a Go format string, so
the only place to put the signal is the prompt section — a `cmd/agent` change
that is prompt text and must be verified by a real LLM run (Rule 11), which this
session's budget does not cover. Filed as a follow-up task, not claimed here.

## Verification

See `tasks/01a1310aa6f871f186a93da940335fa8-report.md` for the full
`Ran:`/`Result:` evidence: build, scoped suite, the red→green proof with its
blob-identity restore check, and the real `gopi merge` run in a sandbox profile
that filed a row whose payload names the landed commit.
