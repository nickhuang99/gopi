<!-- doc-class: normative -->
# Judging whether a delivery landed: the criterion, and the three probes it forbids

**The question this page answers:** *"does the integration branch already carry this
work?"* — asked at close-out, at rework, and by any reader who has just been told
"master is missing my commit".

**The answer is a criterion with three arms, measured against a NAMED ref, at a
NAMED sha.** Two of the probes that look obvious answer the wrong question, and
one of them (the commit hash) is a false *negative* generator that has already cost
this project two full runs.

Everything below is executable: `internal/tools/landing_criterion_probe_test.go`
pins each arm and each banned probe against real git repositories, and its own
`-run` line is the reproduction recipe.

```
go test ./internal/tools/ -run TestLandingCriterion -count=1 -v
```

## The incident — one verdict, both halves false

Row `01a17dd1` asserted in its briefing (2026-10-27 22:46) that master lacked four
functions and a 235-line red proof, and filed work to re-implement them. Re-measured
against `refs/remotes/origin/master` = `84beb309ce3c90875e85142eb12769564778ea45`:

```
$ for f in CloneDeliveryTip TaskDeliveryRecordLanded deliveryProofLandedRecorded deliveryProofParseLandedRecord; do git grep -c "func $f" origin/master -- '*.go'; done
origin/master:internal/tools/task_delivery_proof.go:1     ← CloneDeliveryTip
origin/master:internal/tools/task_delivery_proof.go:1     ← TaskDeliveryRecordLanded
origin/master:internal/tools/task_delivery_proof.go:1     ← deliveryProofLandedRecorded
origin/master:internal/tools/task_delivery_proof.go:1     ← deliveryProofParseLandedRecord
$ git show origin/master:internal/tools/zz_redproof_delivery_proof_rebase_test.go | wc -l
235
```

All four exist; the red proof is exactly 235 lines.

**The verdict was TRUE when it was measured and FALSE when it was acted on, and the
gap is 8 minutes.** The timeline, each line measured:

| time (+0800) | commit | what it is | measured by |
|---|---|---|---|
| 22:08:24 | `a802b47` | master's tip when the briefing measured it | `git log -1 a802b473e196` |
| 22:46 | — | the briefing's own measurement: four functions **absent**, red proof **absent** | `git grep -c "func <name>" a802b473e196` → 0 for all four; `git cat-file -e a802b473e196:internal/tools/zz_redproof_delivery_proof_rebase_test.go` → ABSENT |
| 23:33:54 | `1ae64e138` | **the fix lands — all four functions and the 235-line red proof** | `git merge-base --is-ancestor 1ae64e138 origin/master` → exit 0 |
| 23:41:59 | — | the row's run **starts** | `gopi task show 01a17dd1-85c7-7f02-b3cf-41fa269c6112` → `run:            5d2c07ed-eef4-443d-a784-53acdaf1793b  2026-10-27 23:41:59  completed exit=0` |

So this is the **stale-at-dispatch** shape, not the stale-at-authoring shape
(`devdoc/briefing-base-revision-staleness-at-dispatch.md` distinguishes them): the
briefing was accurate at writing, the fix landed 8 minutes before the run was handed
it, and nothing re-measured the premise at that boundary. A reader who repeats the
probe today gets the opposite answer from the row's own text — which is why a verdict
must carry its ref AND its sha, not just its conclusion.

Three separate errors produced that verdict, and each has its own arm below.

## Error 1 — a commit hash is not a landing proof (it is a false-negative generator)

The clone channel delivers with `pull --rebase → build → push`
(`internal/agent/clonedelivery.go`). A rebase **replays** the run's commits onto a
destination that has MOVED, so the shas are recomputed: the run's `HEAD` is a commit
the destination has never had, and `git merge-base --is-ancestor <tip> <ref>`
answers "not landed" for work that landed perfectly.

So `01a17abb`'s three commits were on master under **other shas**, and the same
patch reached it twice in three hours under two identities:

```
$ for c in 1ae64e138 cd1412c9d d010dfd0a; do echo "$c $(git show $c | git patch-id --stable | cut -d' ' -f1)"; done
1ae64e1380472cf2003abb6a32c74c8bb902dd5f 686d800e435cf8238691e52d8a8a2ae36c9a59d4
cd1412c9d96ea923eb6e4cbb720236a0264edc59 5fe5c7ab8707a8020011c2e02807be957de7e8db
d010dfd0a81f7d04ffc9c029a14ca5a6a6edc668 c564ca14cb99f3cbfba0b2d622a53c059419a2a7
```

The three patch-ids a prior session recorded **match exactly** — which is the whole
point: the content was already there, under hashes nobody had written down.

**Therefore:** arm ① may only ever be used as a POSITIVE proof. Its negative is not
a verdict; it is a question for arm ②.

## Error 2 — a commit subject is not an identity

`git log --grep="<subject>"` (and `--all` around it) answers "present" for a
different change that merely borrowed the title. This repo carries the shape in the
open: the same `delivery-proof` subject names two commits whose patches are
**different**,

```
$ for c in $(git log --format='%H' --fixed-strings --grep "fix(delivery-proof): a rebase that re-derived the content is still a delivery" origin/master); do echo "$c $(git show $c | git patch-id --stable | cut -d' ' -f1)"; done
700ce928b77abc330b49de5fbb002d2db7184b62 d454427c4edd42a23ecc679859448b9386beeaf4
1ae64e1380472cf2003abb6a32c74c8bb902dd5f 686d800e435cf8238691e52d8a8a2ae36c9a59d4
```

and on this revision 15 subject strings appear more than once among 668 commits.

**Therefore:** a subject is a *signal* for a human to go read the landed commit —
never a decision. `internal/tools/rework_landing_probe.go` says exactly this in its
own text ("it is not a verdict and it takes no write"), and states the inverse case
too: two fixes written twice have DIFFERENT patch-ids, so that probe compares
subjects on purpose.

## Error 3 — `--all` is not a delivery point

`--all` widens the probe to every ref the clone happens to carry, including
`refs/tags/archive/*`. Rule 9 archives a losing branch as a tag, so an archived
commit answers "present anywhere" while the integration branch has never seen it —
a false POSITIVE, and the mirror of Error 1.

Measured on this checkout: of the 16 `refs/tags/archive/*` refs, **15 tips are
ancestors of `origin/master` and 1 is not** — the one that is not is
`archive/task/linked` → `c0c17aed1f7784e7ad521b9b57c85a6029fdbb5c`, whose subject is
`base`. The tags are *usually* landed, which is precisely why a probe that counts
them cannot tell you whether a *particular* commit's work arrived.

**Therefore:** the verdict is asked of ONE ref, named, with the sha it resolved to.
A clone channel's delivery point is `refs/remotes/origin/<branch>`; the recorded
repo's `refs/heads/master` on a client host is a manual ref that goes stale by
design (`devdoc/integration-claim-ref-staleness.md`).

## What arm ② cannot answer — its own limits, stated

`patch-id` is a fingerprint of ONE commit's patch, so it inherits that commit's
shape. It answers "is this patch present", never "is this work present", in these
cases — and each is a question for arm ③, not a licence to call the work landed:

- **a change SPLIT across commits** — every part has its own patch-id and none
  equals the whole;
- **a change SQUASHED or merged** — the patch-id of the combined commit is new;
- **an EMPTY commit or a mode-only change** — there is no content to fingerprint
  (`git patch-id` prints nothing);
- **the same patch applied TWICE** — two commits share one patch-id, and ② cannot
  tell "landed" from "landed twice".

(The first three are `git patch-id`'s documented behaviour, not measurements taken
in this run; the fourth is the reason the ledger's landed record names the tip —
`TaskDeliveryRecordLanded` in `internal/tools/task_delivery_proof.go` — rather than
counting applications.)

## The criterion

Three arms, cheapest first. A POSITIVE from any arm is a proof. A NEGATIVE from all
three is "undelivered" — checked in that order, never in one arm alone.

| arm | probe | survives | answers |
|---|---|---|---|
| ①ancestry | `git merge-base --is-ancestor <tip> <ref>` | nothing moved | landed (positive only) |
| ②patch-id | `<tip>`'s `git patch-id --stable` ∈ `<ref>`'s commits | a pure rebase, a cherry-pick | landed |
| ③content | every line `<tip>` ADDED (normalised) is present at `<ref>` | a rebase that RE-DERIVED the patch | landed |

Arm ③ exists because ② is guaranteed to answer false for a re-derived hunk, and
that shape is real (a regenerated table measured against a moved base —
`internal/tools/task_delivery_proof_content.go`, and the measured grounds it names).
Arm ③ also cannot rescue an undelivered row: work that exists nowhere has every
added line absent, and the refusal quotes what is missing.

**"Normalised" is a NARROW promise, and it has one shape rule.** Arm ③ drops blank
lines, trailing whitespace, self-referential measurement metadata (`measured-at:` —
anchored at the start of the line, never a sub-string) and comment-only lines — and
that last class is where a prefix list is NOT enough. `*` is the one byte whose
meaning is decided by what follows it: `*state += x` is a deref assignment (WORK),
` * a block comment continuation` is not. A bare `"*"` in the prefix list therefore
dropped every deref line, and a commit whose additions are ALL deref lines
normalised to zero lines — i.e. `content-free`, i.e. LANDED at every destination
without a comparison. That is the false-positive direction this arm exists to
prevent, so the byte is judged by a shape predicate
(`deliveryProofIsBlockCommentContinuation`: marker + whitespace or end-of-line =
continuation; marker + identifier/`(`/marker = work), and both directions are
pinned by `TestDeliveryProofContent_StarShapeKeepsDerefWorkAndDropsBlockComments`.

**The four-row self-check this page ships as code**
(`internal/tools/landing_criterion_probe_test.go`):

```
$ go test ./internal/tools/ -run TestLandingCriterion -count=1 -v
PASS  TestLandingCriterion_AReplayedCommitIsLandedThoughItsHashIsNot
      hash probe: NOT landed (false negative) — patch-id probe: landed
PASS  TestLandingCriterion_ASharedSubjectIsNotAnIdentity
      subject probe: LANDED (false positive) — patch-id probe: NOT landed
PASS  TestLandingCriterion_ACommitOnAnArchiveTagIsNotDelivered
      --all probe: PRESENT (false positive, found on archive/task/lc) — the integration ref does not carry it
PASS  TestLandingCriterion_ARederivedRebaseNeedsTheContentArm
      patch-id probe: NOT landed (false negative) — content arm: landed
PASS  TestLandingCriterion_TrulyUndeliveredWorkFailsAllThreeArms
PASS  TestLandingCriterion_EveryVerdictNamesItsRefAndSha
```

Each test STATES the banned probe's wrong answer in its log line and then asserts
the arm that answers correctly — so a future reader re-running one of them reads the
whole lesson from the output, without this page.

## What this does not replace

`TaskDeliveryProof` (`internal/tools/task_delivery_proof.go`) is the criterion wired
to a LEDGER ROW: it resolves the row's ground, tip and destination, and its refusal
is the thing that stops a wrong `completed`. `reworkLandingProbe` is the *subject*
signal that saves a rework executor a run. This page and its test judge a RAW GIT
PAIR — tip against integration ref — so that a reader holding only two commits can
answer the question with no ledger, no session and no row. It adds no production
mechanism (Rule 35): every invocation is `rev-parse`, `show`, `rev-list`,
`merge-base` or `patch-id`; no fetch, no push, no ref move, no write.

## The rule in one line

**A delivery verdict names the ref it was asked of and the sha that ref resolved
to, and it is answered by content — not by a commit's hash, not by its subject, and
never by every ref the clone happens to carry.**
