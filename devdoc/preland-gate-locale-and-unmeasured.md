<!-- doc-class: historical -->
# The pre-land gate, two ways it can lie about having looked

Task `01a0de15-c226-7e1f-9921-3a16c355770c`, 2026-09-26. Branch
`fix/preland-gate-unmeasured-refuses`.

Reported as: *"TestMergePrelandGate\* fail on pristine master (scratch checkout
cannot be materialised in cmd/agent suite)"*. There were **two** defects behind
that report, and the one in the title was not the cause of the failure family —
it was the cause of the failure *message*.

## What the reported failure actually was

`merge_preland_gate_test.go` failed in a `cmd/agent` suite run with, verbatim:

```
merge_preland_gate_test.go:262: the abstention does not say WHY it is not refusing
  no scratch checkout of the repo could be materialised, so the conflicted pairs could not be read
--- FAIL: TestMergePrelandGateRedProof
  the gate did not refuse a branch whose own truth-class pair contradicts itself: ABSTAIN / …
--- FAIL: TestMergePrelandGateLeavesNoResidue
  fixture: the gate did not refuse, so residue is not exercised: ABSTAIN
--- FAIL: TestMergePrelandGateIsWiredBeforeTheChild
  mergeHandOver constructed the child for a branch the pre-land gate refuses — the gate is not wired in front of the writer
```

**Six of the eight** assertions in that file were broken by one word: `ABSTAIN`.
Every other failure above is downstream of it — the gate never refused, so the
residue fixture, the wiring guard and the end-to-end test all failed on the same
non-refusal.

### The reproduction, which is a LOCALE and not a scratch problem

The scratch checkout was never the problem. A pristine master tree under
`LC_ALL=zh_CN.UTF-8` reproduces **every** line above, and the same tree passes
with the locale pinned:

```bash
R=<scratch>/pristine; git archive HEAD | tar -x -C "$R"; cd "$R"
LC_ALL=zh_CN.UTF-8 LANG=zh_CN.UTF-8 go test ./cmd/agent/ -run 'TestMergePrelandGate' -count=1
#   --- FAIL: TestMergePrelandGateRedProof   … ABSTAIN / no scratch checkout … could be materialised
#   6 of 8 FAIL
go test ./cmd/agent/ -run 'TestMergePrelandGate' -count=1
#   ok  github.com/nick/gopi/cmd/agent
```

Mechanism (measured on this host, git 2.34.1):

```console
$ LC_ALL=C            git merge --no-commit --no-ff side
Auto-merging f.md
CONFLICT (content): Merge conflict in f.md
Automatic merge failed; fix conflicts and then commit the result.

$ LC_ALL=zh_CN.UTF-8  git merge --no-commit --no-ff side
自动合并 f.md
冲突（内容）：合并冲突于 f.md
自动合并失败，修正冲突然后提交修正的结果。
```

The merge lane classifies a probe merge by reading the **English word
`CONFLICT`** out of git's output — `mergeIsConflict` (`cmd/agent/merge_run.go`),
the union lane's probe (`merge_union.go`), and the caller's conflicted-file count
all do. git translates its diagnostics, so under a non-English locale a real
CONFLICT is misread as a fatal git error:

* `mergeMeasureConflict` → `conflict measurement failed: probe merge: …`
* the pre-land gate's probe arm → "the probe merge could not be run"
* the gate **ABSTAINs**, and `mergePrelandFilter` hands the branch to the writer

That is why the failure looked host-shaped and flaky: it depends on the locale of
whoever started the test binary. A suite that inherits a translated host fails;
the same suite started from an English shell passes. Nothing in the fixture is at
fault, which is why "reproduces on pristine master" was true and still
misleading.

**Also the Arabic/chinese text explains the sibling report's oddity**: the same
run printed `union lane probe failed (union probe merge: … 自动合并 devdoc/preland-doc.md
冲突（内容）：…)` — that lane fell through for the same reason.

### Fix A — pin the locale on the lane's one git seam

`mergeGit` is the lane's single git seam (probe, union probe, pre-land gate,
worktree verbs), so one place pins the child's locale. This is the same rule
`internal/tools/bash.go` already applies to its own children
(`bashChildEnv` / `bashDefaultLocale`); the merge lane was simply missing its
half. `GOPI_MERGE_LOCALE` overrides it (one reader, Rule 2) for a human reading a
diagnostic by hand — nothing in the lane may *parse* a translated one.

Guards: `TestMergeGitEnvPinsTheLocale` (deterministic: the env shape, meaningful
on a host with no locales generated) and
`TestMergeGitKeepsTheConflictMarkerUnderAForeignLocale` (behavioural: a real repo,
a real conflict, the ambient locale forced non-English). The second asks glibc via
`locale charmap`, **not** `locale -a` — the listing spells the same locale
`zh_CN.utf8`, so a string comparison against it reports "cannot generate" on a host
where git translates perfectly (measured; the first draft of the probe did exactly
that and skipped the test for the wrong reason).

### Fix C — the index is the fact, the prose is the fallback

The locale pin makes the prose English again on every path this lane runs, but it
leaves the *dependency* in place: a classifier that reads rendered text is only as
stable as the pin, and `GOPI_MERGE_LOCALE` exists precisely so a caller can ask for
a translated diagnostic. So the classification grew a second, locale-free arm —
`mergeProbeConflict(dir, err)` (`cmd/agent/merge_run.go`):

1. **ASK THE INDEX** — `git ls-files -u` (`mergeUnmergedIndexPaths`). A conflicted
   `git merge --no-commit` leaves unmerged entries (stages 1/2/3) in the index, and
   that listing is a machine format no locale can translate. The lane already read
   it for the conflicted-path list; the parser now has ONE home (Rule 14) that the
   gate's pair list and the classifier both call.
2. **FALL BACK TO THE PROSE** only when the index cannot be asked at all (a
   directory git refuses) — i.e. exactly the case the pin covers.

All three probe call sites (the conflict measurement, the union lane, the pre-land
gate's probe arm — the last of which is what the reported failure ran through) now
call it. `mergeIsConflict` survives as the documented prose-only fallback, so no
call site reads it as the truth about the index.

Guard: `TestMergeProbeConflictReadsTheIndexNotTheProse` is red-and-green in ONE
repository — it deliberately asks for the translated diagnostic (through the
documented knob), asserts the prose-only classifier is **blind** there, then asserts
the index-reading classifier still recognises the conflict.

## The second defect — a gate that cannot look must not say "nothing to do"

`ABSTAIN` in this gate meant **two different things**:

| situation | was | should be |
|---|---|---|
| probe ran, pairs read, no class being decided | ABSTAIN | ABSTAIN (`no conflicted path…`, `sides agree…`) |
| probe could not run at all — precondition unmet | ABSTAIN | **FAIL**, with the reason |

The second row is the dangerous one. `mergePrelandFilter` only withholds a branch
on `FAIL`, so a gate that could not build its scratch checkout reported the same
word a genuinely clean branch gets and **handed the branch to the one writer that
commits into the integration branch** — with the run's report naming it
unmeasured. An unlooked-at branch was indistinguishable from a checked one.

Fix B: `mergePrelandCheckout` returns its **reason** instead of a bool, and each
arm (scratch dir could not be created / the derived path is not fresh / `git
worktree add` failed / the probe merge could not be attempted) becomes a refusal
naming itself and the underlying error. The cost is explicit and accepted, on the
same terms as the build gate's hard-refusal arm: a host that cannot provide the
scratch tree stops hand-overs. **Reproduce-or-refuse; never reproduce-or-hope.**

Deliberately unchanged: the genuine abstentions (no conflicted path, sides agree,
gate disabled by `GOPI_MERGE_PRELAND_GATE=0`) still do not refuse, and the
assertions the briefing named as guards were not weakened — in particular
`TestMergePrelandGateIsWiredBeforeTheChild` and
`TestMergePrelandGatePreExistingRedAbstains` (a pre-existing red must not
deadlock the queue) still hold, and both still pass.

## Why the two defects hid each other

Defect A (locale) made the probe fail; defect B (ABSTAIN) reported that failure as
"nothing to decide". Each alone is quieter than the pair: A alone would have
surfaced as `conflict measurement failed` on a translated host, and B alone would
have stayed invisible until a host genuinely could not make the scratch checkout.
Together they produced a message about scratch checkouts that had, in fact,
materialised fine — and sent two reports after the wrong subsystem.

## Verification (see the task report for the full evidence block)

* Red→green, A: pristine master under `LC_ALL=zh_CN.UTF-8` → 6 of 8
  `TestMergePrelandGate*` FAIL with the briefing's own lines; the branch under the
  same locale → 25/25 PASS.
* Red→green, B: three new tests run against the **pre-fix** gate →
  `ABSTAIN / no scratch checkout … could be materialised` (FAIL); against the fix
  → PASS. They use string literals rather than the new constants on purpose, so
  they are their own red proof on master.
* Pre-existing reds found while verifying and NOT caused by this branch (both
  reproduce on a pristine `git archive HEAD` tree, and both were reproduced with
  the branch's change reverted): `TestMergeUnionRowCheckOnTheRealManifest`
  (`merge_union_rows_test.go:135`, a manifest-lint fixture staged in the worktree)
  and the Rule 17 standards gate on
  `cmd/agent/cli_tasks_list_order_test.go:236` (a **rule-compliant**
  `testutil.SnapshotEnv` pair missing the gate's inline escape marker — filed as
  bug task `01a0de4f-22aa-71b8-bd09-4c47f81d64df`).

## Rejected alternatives

* **"Drop the `CONFLICT`-string classification; use the index instead."** —
  ADOPTED as Fix C above. It was first deferred as a redesign belonging to its own
  task, on the grounds that the locale pin was the minimal fix. Reconsidered once
  the measurement showed the index arm costs one helper and removes the dependency
  the pin was papering over: the prose is now the *fallback* rather than the
  evidence, which is what makes the fix survive a caller who deliberately asks for
  a translated diagnostic.
* **"Treat an unmaterialisable checkout as a PASS."** That is the current bug
  restated: it means handing over a branch nothing looked at.
* **"Make the wiring test skip when the gate abstains."** The briefing forbade
  weakening it, and it would be wrong independently — that guard is the only thing
  asserting the gate sits in front of the writer.
