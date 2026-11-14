<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Merge resolution — two different documents at one devdoc path (the voice_input rig vs the design doc)

Task: `01a115bb-fe1e-7251-8ba4-0698a3ba5a01` · host: **peerbox**
Branch: `task/01a113e467767eb9a4c33cb1df1cdfbb` · worktree: `/home/user/workspace/gopi-01a115bbfe1e72518ba40698a3ba5a01`

This document records ONE merge resolution: a run of `git merge master` inside a
delivery branch, the refusal that preceded it, the measurement that explained the
refusal, and the resolution. It is the record of a past run, not a standing claim
about master — hence `historical`.

The resolution was performed **six times**, which is itself part of the record:
the first pass merged `master` at `e153b15c7`; while this run's verification was
running, master advanced to `7e084ba22` (merges of a sibling task's work) and the
delivery branch re-merged it as `2fb334242`; then master advanced **again** to
`24c3c5537`, which had **already landed this branch's rig document** — the third
pass merged that and deduplicated, and §4 is its record; the fourth, fifth and
sixth syncs took `39c233720`, `39711d146` and `fb9835b55`. §5 records them. The
class measurement of §1 was taken on `e153b15c7` (the tip the pre-land gate
refused); it was re-measured after each merge, and the shared path's row is still
exactly one, still `normative`. The sections below are in the order the passes
happened, and `Files`/`Verification` describe the **final** state — measured after
the sixth sync, at the close-out commit recorded in
`artifacts/closeout-measurement-2.txt`.

## Completed

### 1. What the refusal actually was: an add/add of two DIFFERENT documents

The pre-land gate refused the merge because the class a path claims is a
**standing claim about master**, and the two sides of the merge wanted to make two
different ones for the SAME path (`devdoc/voice-input-default-local.md`):

| side | commit | blob | lines | DOC-INDEX row for that path |
|---|---|---|---|---|
| master | `e153b15c7` | `4bfa9699` | 556 | `normative` (line 1947 at `e153b15c7`; line 1950 at the `7e084ba22` this branch now merges) |
| branch tip | `dba11bb2` | `127f5130` | 452 | `historical` (line 1909) |
| merge base | `3255973cc` | — | — | the path **does not exist** (0 copies) |

The merge base carrying **neither** copy is the whole of the difficulty: this is
not a content conflict between two revisions of one document, it is an
**add/add of two unrelated documents that happen to want the same name**. Master's
copy is HP task `01a113e467e07191`'s design doc (the target flag table, the
parse-time refusal inventory, the file-precise change list). The branch's copy is
peerbox's rig doc for `01a113e4-6776`: the deterministic local-capture rig
(`module-null-sink` + `module-remap-source`), its measurement table and its
re-verification. They share a title prefix and nothing else — master's document
contains **zero** occurrences of every symbol the branch's later fixes name.

### 2. The branch's intent, separated from its bytes

The branch's six commits against that path are one document plus five drift fixes
to it:

```
09ad05915  the deterministic local-capture rig for voice_input (peerbox, null-sink + remap-source)   +311
8cc4e7e86  correct the race/probe mechanism in the rig doc after re-reading the code               +23 -10
7374cd07a  carry the capture artifact's sha256 in the measurement table                             +1
9fedce85e  fix the voiceResolveSelection citation range                                             +1 -1
e2331156f  record the rig's re-verification on peerbox and the determinism boundary                  +93 -2
dba11bb24  attribute the listener figure by pid and re-measure the session-copy artifacts          +39 -3
```

Each of the five fixes is **about the rig's own run** — the exit status of
`module-remap-source` (`cli_voice_client.go:284-295`), `voiceVerifyDevice`'s
early return for the wildcard `pulse:@DEFAULT_SOURCE@`
(`cli_voice_device_policy.go:260-263`), the capture artifact's sha256, the
close-out re-measurement of the session copies, and the `gopi_listeners=1`
figure's pid attribution. Measured against master's document, the subjects are
all absent:

```
  voiceResolveSelection    in master-doc=0  in rig-doc=0   (the NAME is absent on both
  voiceVerifyDevice        in master-doc=0  in rig-doc=1    sides; the fix is a file:line
  voicePulseMonitorSuffix  in master-doc=0  in rig-doc=1    range in a §1.3 rig recipe)
  voiceCaptureGraceSec     in master-doc=0  in rig-doc=1
  voiceVerifySeconds       in master-doc=0  in rig-doc=1
  DEFAULT_SOURCE           in master-doc=0  in rig-doc=7
  sha256                   in master-doc=0  in rig-doc=13
  remap-source             in master-doc=0  in rig-doc=5
  monitor                  in master-doc=0  in rig-doc=11
  listener                 in master-doc=0  in rig-doc=6
  probe                    in master-doc=0  in rig-doc=5
```

So the two halves of the intent are separable without loss, and this is the
resolution that was taken **at that moment** (superseded by §4 — master later
shipped the rig doc itself, at `devdoc/voice-input-capture-rig.md`, and the final
resolution is a dedup rather than an addition):

- **The rig doc keeps its class.** It moves to a path master does not use,
  `devdoc/voice-input-local-capture-rig.md`, **byte-identical** to the branch
  tip's document (blob `127f5130`, 452 lines, `git hash-object` equal). Every one
  of the five drift fixes rides along in it, because they are all fixes to that
  same document. Its `<!-- doc-class: historical -->` banner stays, and its new
  row is `historical` with the writer's own reason.
- **Master's document is not touched.** Its copy at the shared path is
  byte-identical to master's (sha1 `54b12a73…`, 556 lines, blob `4bfa9699`), and
  its `normative` row (DOC-INDEX line 1950 on master `7e084ba22`) is left exactly
  as master has it. A `historical` document cannot correctly overwrite a
  `normative` one, and the gate is right to refuse that — the truth-class is a
  claim about master, not a preference of the writer.
- **No second class claim for one path.** The branch's duplicate row for the
  shared path is dropped. The manifest gains exactly **one** row for the
  intent's document (plus this report's own row, which every report owes the
  corpus) and the shared path keeps the single `normative` row master gave it.

### 3. Why the drift fixes did not need to be transposed into master's document

Because they describe a run master's document never made. Transposing them would
require inventing a rig section inside HP's design doc — a section about a rig
that document does not have and does not cite — and would have turned a
`historical` record into an edit of a `normative` document, which is the same
class collision in a new spelling. The fix that belongs in master's document is
the one master's document itself needs, and nothing in this branch is that.

### 4. The third pass: master landed the rig itself, so this branch deduplicates

While the verification of §2/§3 was running, master advanced again —
`7e084ba22` → **`24c3c5537`**, the merge of `feat/rescue-01a113e4-capture-rig` —
and that merge **landed the very same rig document**, at the path
`devdoc/voice-input-capture-rig.md`:

```
$ git rev-parse master:devdoc/voice-input-capture-rig.md
127f5130cb18a49fddad09e5d234fdbb4f401417        ← identical to this branch's copy
$ sha1sum devdoc/voice-input-capture-rig.md devdoc/voice-input-local-capture-rig.md
40be032ee11334f60239e24826737705395398bd  devdoc/voice-input-capture-rig.md
40be032ee11334f60239e24826737705395398bd  devdoc/voice-input-local-capture-rig.md
$ diff devdoc/voice-input-capture-rig.md devdoc/voice-input-local-capture-rig.md
(no output — byte-identical, 452 lines each)
```

So after the delivery-point sync the branch's own difference was **the same
document at a second path** — content master already ships, under a name master
does not use. Landing that would improve nothing and would leave the repo with two
identical copies, the same defect the branch already fixed for the other
document, one path over. A rescue branch re-derived the same resolution this run
did (Rule 23: a fresh branch, not the dead one) and got there first.

The resolution therefore folded into the sync: master's two documents stay
exactly as master has them, master's own `historical` row for
`devdoc/voice-input-capture-rig.md` stays, the branch's duplicate document is
removed and its row with it. The branch's difference vs master is now **this
report alone** — which is what an intent-preserving re-land looks like when the
intent has already landed: the drift fixes §2 enumerates are master's now, inside
`devdoc/voice-input-capture-rig.md` (blob `127f5130`, the branch tip's own
document), and the third document master's merge carried
(`devdoc/voice-input-rig-orphan-disposition-01a113e4.md`) records the rescue.

This is Rule 30a's lesson taken seriously: an alignment proves nothing about the
work, and the numbers must be **re-measured** after the integration branch moves,
not carried over from the reading that was true an hour ago. Master moved **once
more** while this report was being written (`24c3c5537` → `39c233720`, the rescue
branch's disposition record growing), and the sync was taken again — the fourth
merge. `Files` and `Verification` below are the reading taken **after**
that merge (`HEAD` = the merge commit's child), on which the branch's difference
is still exactly what `Files` lists and the shared path's blob is still
`4bfa9699` on both sides.

### 5. The later syncs, and why the count of syncs is unbounded by design

**This is the honest shape of the rule rather than an exception to
it**: between the fourth merge and close-out, master advanced again —
`39c233720` → `39711d146`, one more `feat/rescue-01a113e4-capture-rig` merge whose
only delta is another 145 lines in
`devdoc/voice-input-rig-orphan-disposition-01a113e4.md` (the rig document itself
unchanged: blob `127f5130` on both revs). The fifth delivery-point merge was taken
(`8f8325e64`), `git merge-base --is-ancestor master HEAD` → YES, and the reading
was taken again rather than carried: `git diff master...HEAD --name-status` is
still exactly `M devdoc/DOC-INDEX.tsv` + `A this report`, the shared document is
byte-identical at `4bfa9699` on both sides, and master's rig document is
byte-identical at `127f5130` on both sides. A reader should treat the count of
merges as unbounded by design: this branch's difference is invariant under them,
which is the whole point, and a further master movement needs another sync, not a
rewrite.

**Sixth sync** — the same case, one more time, at close-out: master advanced from
`39711d146` to `fb9835b55` (the `/voice` chat feature plus the DOC-INDEX union
lane: `cmd/agent/chat_voice.go`, `internal/lineedit/lineinsert.go`,
`devdoc/chat-voice-slash-command.md`, and the index row union). The sixth
delivery-point merge was taken (`72a3746dc`), master is an ancestor of `HEAD`, and
the reading was taken again: the branch's whole difference from master is still
`M devdoc/DOC-INDEX.tsv` (its `+1` row is this report's own) + `A this report`;
the shared document is byte-identical at `4bfa9699`; master's rig document is
byte-identical at `127f5130`; `git status --porcelain` prints nothing. Recorded in
`artifacts/closeout-measurement-2.txt`. This is the sixth merge for a branch whose
diff has not changed through any of them, which is the property being reported,
not a problem being worked around.

## Files

| path | change | evidence |
|---|---|---|
| `devdoc/voice-input-capture-rig.md` | **unchanged vs master** — the rig doc master already ships (blob `127f5130`, 452 lines); this branch's copy of it was removed in favour of master's | `git diff master --name-status` does not list it; `git rev-parse master:devdoc/voice-input-capture-rig.md` = `127f5130…` = the branch tip's own blob |
| `devdoc/voice-input-default-local.md` | **unchanged vs master** (byte-identical, blob `4bfa9699`, sha1 `54b12a73…`) | `git diff master --name-status` does not list it |
| `devdoc/voice-input-local-capture-rig.md` | **removed before it ever reached master** — it was this branch's duplicate of master's `voice-input-capture-rig.md` | `git ls-files devdoc/voice-input-local-capture-rig.md` → empty; `diff` against master's copy printed nothing |
| `devdoc/DOC-INDEX.tsv` | **M** — `+1` row, and it is **this report's own** row (Rule 36: a doc classified in the same commit that lands it). No row for either rig path is added or edited by hand; the duplicate row for the removed path went with the file | `git diff master --numstat -- devdoc/DOC-INDEX.tsv` = `1 0`; the added line names this report |
| `devdoc/merge-resolution-voice-input-rig-conflict.md` | **A** — this report | classified in the same commit (Rule 36) |

`git diff master --name-status` (master is an ancestor of this branch after the
fourth merge, so the two-dot form is the whole difference):

```
M	devdoc/DOC-INDEX.tsv
A	devdoc/merge-resolution-voice-input-rig-conflict.md
```

## Verification

- **Build**: `make build` → `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`, exit 0 (re-run after the third merge).
- **Tests**: `make test-budget PKGS='./internal/docindex ./internal/standards ./cmd/agent'` — branch-scoped to the packages the diff's surfaces touch (the doc corpus reader, the standards gates, and the `cmd/agent` doc/anchor gates); no whole-tree run (Rule 27). **Read honestly, not as a green**: the per-package verdicts were `pass internal/docindex`, `pass internal/standards`, `fail cmd/agent` — `cmd/agent` failed with **8 tests** that this branch does not touch, and they were measured on the **baseline** as well to prove they are not this branch's:
  - `TestMergeUnionRowCheckOnTheRealManifest`, `TestSpawnersPinTheRepoContext`, `TestMergePromptCoversEveryRequiredSection`, `TestLedgerOpenersUseProductionDSNContract`, `TestSubmitSpawnersGiveTheChildAResolvableRepo`, `TestSubmitSpawnersGiveTheChildAResolvableRepo_ExactMatchRule` — **fail on master with nothing of this branch present** (red-proof: `artifacts/baseline-vs-branch-tests.txt` for the earlier set, and `artifacts/baseline-on-new-master.txt` for the final one: a detached worktree at master `39711d146`, in which `git ls-files devdoc/merge-resolution-voice-input-rig-conflict.md` is empty, produces the **same 8 failures**). They are pre-existing master reds — a stale evidence anchor in `devdoc/requirements/01-per-module-briefings.md`, a spawn-site scan floor that drifted, a prompt phrase, a bare-DSN opener and two spawner-repo assertions — none of them a path, row or file this branch changes.
  - `TestWorkerResumeKeepsTheGuardsIntact`, `TestSyncTaskIDRefusedOnEverySessionFace` — also present on master (same artifacts); they are environment/ledger-sensitive (`TestWorkerResume…` failed on *residue in its own scratch*), and this branch's diff is three `devdoc/` text files.
  - `./internal/docindex ./internal/standards` — the two packages that actually own the corpus machinery. Read honestly: `internal/docindex` **passes** (`ok … 1.9s`); `internal/standards` **fails 5 tests on the branch tip AND on a clean master `fb9835b55` with none of this branch present** (`artifacts/baseline-standards-on-new-master.txt` vs `artifacts/doc-anchor-gates.txt` run set) — `TestCommandsDocGeneratedBlockMatchesSource`, `TestCommandsDocAnchorsResolve`, `TestCommandsDocGateRedProofStaleGeneratedBlock`, `TestStandardGates` (Rule 20's `isOn` at `cmd/agent/chat_voice.go:488`), `TestStandardGateDataReport` (40 env-isolation escapes). Every one of those files was **added by master's own `/voice` merge `fb9835b55`** — the branch cannot reach them, and the baseline worktree proves it. The earlier `make test-budget` reading of this package (`pass internal/standards`) was taken against the older master `39c233720`, before that merge; the newer master is redder, not the branch.
  - **The doc surfaces themselves, run standalone and green**: `go test ./cmd/agent/ -count=1 -run '^(TestDocCheck.*|TestAnnotateCellAnchor.*|TestCaptureVerbIsDispatchedAndDocumented)$' -v` → 7/7 `--- PASS` (the three `TestDocCheck*` contract tests, three cell-anchor tests, the capture-verb documentation gate), `ok github.com/nick/gopi/cmd/agent 1.192s`, exit 0 — `artifacts/doc-anchor-gates.txt`.
  - The per-package reading of the run stream is `artifacts/test-run-final.txt`; the baseline pair is `artifacts/baseline-on-new-master.txt`. The set is **8**, not 6, on the newest master: master itself gained two more reds in the `/voice` merge, which the baseline worktree reproduces without any branch content. Honest coverage caveat: `TestWorkerResumeKeepsTheGuardsIntact` is also known to be order/residue-sensitive, so its presence on both sides is weaker evidence than the other seven.
- **Ran** (Rule 31 — the surface this change is about, invoked for real):
  - `./bin/gopi tool doccheck` → `doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row`, exit 0. (`gopi doccheck` is retired and says so; the live verb is `gopi tool doccheck`, which is what Rule 36's check names.)
  - `python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`, exit 0.
  - **The red→green of the stale-row rule, observed directly** (Rule 36's own defect class): after `git rm devdoc/voice-input-local-capture-rig.md`, the row that had been written for it was left in place and `./bin/gopi tool doccheck` **refused** — `row-with-no-file devdoc/voice-input-local-capture-rig.md — the tree does not contain it — a stale permission`, exit 1; removing that one row turned it green (`0 gap(s)`), and the removed row is quoted verbatim in §4's evidence. A check that never fires is not a checked check, so it was made to fire on purpose.
  - `python3 tools/docclass/classify.py . --only devdoc/merge-resolution-voice-input-rig-conflict.md` → this report's own row, written by the corpus's own writer, in the same commit that lands it (Rule 36).
- **Result**: the corpus is consistent (0 gaps), one row per path; master's `normative` row for `devdoc/voice-input-default-local.md` intact and the path absent from `git diff master --name-status`; master's rig doc and its `historical` row untouched; this branch's duplicate of that document removed before it could land twice.
- **Commit**: this branch is a series of merges and prose commits, all on
  `task/01a113e467767eb9a4c33cb1df1cdfbb`: `c6ef3ed92` (the report, classified by
  the corpus's own writer in that same commit — Rule 36), `98bce2855`, the second
  merge `2fb334242` (master `7e084ba22`), `f92a015ca`, `b26ede8e9`, the third merge
  (master `24c3c5537`), `ad5bdbe5c` (the dedup: `git rm` of the duplicate document,
  its stale row dropped — the exit-1→exit-0 pair above — and the third pass added
  to this report), the fourth merge (master `39c233720`), the fifth merge `8f8325e64`
  (master `39711d146`), the sixth merge `72a3746dc` (master `fb9835b55`, which carried
  the `/voice` feature and the DOC-INDEX union lane), and the prose commits carrying
  this paragraph and the close-out notes. No commit in this series waits for a later
  one to classify its document, and no row was ever hand-written.
  Final tip after the fifth sync: the commit that carries this sentence (the hash
  is deliberately not written here — a report cannot name the commit that
  contains it without invalidating itself; the exact tip, with its master
  ancestor check, is the first block of
  `artifacts/closeout-measurement.txt`), and the branch's whole difference from
  master is the two `devdoc/` entries above.

The session's own evidence — the raw measurement outputs this report quotes — is
under `/home/user/.gopi/sessions/01a115bb-fe1e-7251-8ba4-0698a3ba5a01/artifacts/`
(`pre-merge-snapshot.txt`, `merge-attempt.txt`, `class-vs-class.txt`,
`doccheck.txt`, `remerge-and-master-movement.txt`, `master-already-ships-the-rig-doc.txt`,
`doccheck-revalidation.txt`, `baseline-vs-branch-tests.txt`, `final-intent-check.txt`,
`final-intent-check-2.txt`, `closeout-measurement.txt`, `closeout-measurement-2.txt`,
`test-run-final.txt`, `baseline-on-new-master.txt`). Rule 37: the
artifacts of a task run are evidence and stay in the session directory; only this
report is committed.

A note on why the first pass's numbers and the close-out's numbers differ, so a
reader is not misled by them: the class measurement of §1 was taken against
`e153b15c7`, and at that moment `git diff e153b15c7 --name-status` also listed
the sibling task's files (`internal/tools/git_peer_fetch.go`,
`internal/tools/task_ground_baseline.go`, `devdoc/worktree-base-align.md`,
`devdoc/memory-prefetch-prefix-key-verdict.md`, …) — **not** because this branch
touched them, but because master had moved past `e153b15c7` and this branch did
not carry them yet. Two further merges took master in as it moved (`2fb334242`
for `7e084ba22`, the third for `24c3c5537`), and against current master the
branch's own difference is exactly what `Files` lists. This is Rule 30a's order,
observed twice: a delivery-point sync is the LAST step, and the integration branch
moved again during verification — each time, the reading was re-measured instead
of carried over.

### Evidence (self-filed)

- **pre-merge snapshot** — `git merge` refused with `CONFLICT (add/add): Merge conflict in devdoc/voice-input-default-local.md` and `CONFLICT (content): Merge conflict in devdoc/DOC-INDEX.tsv`; the snapshot (both tips, the merge base, both blobs `4bfa9699`/`127f5130`, 556/452 lines, the merge base's 0 copies) is `artifacts/pre-merge-snapshot.txt` and `artifacts/merge-attempt.txt`. Observed directly, verbatim above.
- **class-vs-class measurement** — `artifacts/class-vs-class.txt`: master's row for the path reads `normative`, the branch tip's reads `historical`, the merge base carries the path 0 times.
- **build** — `make build` → `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` then `MAKE_BUILD_EXIT=0`, `bin/gopi` 64407384 bytes, `./bin/gopi --version` → `v0.1.0`.
- **branch-scoped tests** — `make test-budget PKGS='./internal/docindex ./internal/standards ./cmd/agent'`, plus the 6-test baseline-vs-branch pair in `artifacts/baseline-vs-branch-tests.txt` (the same 6 red on master `24c3c5537` with none of this branch present). No whole-tree run (Rule 27).
- **real invocation** — `artifacts/doccheck.txt` and `artifacts/doccheck-revalidation.txt`: the commands above with their exit codes, verbatim, including the deliberate **exit 1** stale-row refusal and the green after the row was dropped.
- **master's document untouched** — `sha1sum devdoc/voice-input-default-local.md` = `54b12a73…` = `git show master:devdoc/voice-input-default-local.md | sha1sum`; blob `4bfa9699` on both sides; `git diff master --name-status` does not list the path.
- **master already ships the rig** — `artifacts/master-already-ships-the-rig-doc.txt`: master `24c3c5537`'s `devdoc/voice-input-capture-rig.md` is blob `127f5130` = the branch tip's own document, sha1 `40be032e…` identical at both paths, `diff` printing nothing; the branch's copy was therefore removed rather than landed twice.
- **master movement + the three merges** — `artifacts/remerge-and-master-movement.txt` + `master-already-ships-the-rig-doc.txt`: `master` at the first merge `e153b15c7`, at `2fb334242` `7e084ba22`, at the third `24c3c5537`; `git merge-base --is-ancestor master HEAD` → YES at close-out; the shared path's single `normative` row re-measured after each merge; master's document byte-identical on every side.
- **`git status --porcelain`** — printed nothing before the report was added (Rule 30); re-measured at close-out.

## Not Done

- **The branch-scoped suite is not all-green, and saying otherwise would be a false
  claim**: 8 tests in `cmd/agent` fail, and they **fail on master too** with none of
  this branch present (`artifacts/baseline-vs-branch-tests.txt` and
  `artifacts/baseline-on-new-master.txt`); `internal/standards` fails 5 tests on
  master `fb9835b55` for the same reason (`artifacts/baseline-standards-on-new-master.txt`).
  This branch's diff is `devdoc/` text only and cannot reach any of them. Repairing
  them is a separate task against master, not part of a merge resolution — filed as
  such rather than papered over here (Rule 26/28: the system layer is peerbox's, and a
  stale anchor, a spawn-site scan floor, a prompt phrase, a bare-DSN test, two
  spawner-repo assertions, an `isOn` accessor and 40 env-isolation escapes are all
  master's). The filing names 6; the newest master carries more, so the repair
  task's evidence is the artifacts rather than the count. Two filings exist, both
  with the artifacts in their payloads: `01a11683-3423-7538-9a4a-41dc8372df7f`
  (the 6 `cmd/agent` reds measured on `24c3c5537`) and
  `01a11692-65b0-79f1-a488-8f3650adcc79` (the widened set on `fb9835b55`: 8 in
  `cmd/agent` plus 5 in `internal/standards` — the `isOn` accessor at
  `cmd/agent/chat_voice.go:488`, 40 env-isolation escapes, a stale `COMMANDS.md`
  generated block and its unresolvable anchor — every one of them introduced by
  master's own `/voice` merge). The second row is parked behind the first by the
  harness's parent gate, which is the correct order: same family, narrower first.
- **§2's resolution (the rig doc at `devdoc/voice-input-local-capture-rig.md`) was
  superseded by §4** and its document never landed: master shipped the same rig doc
  first, at `devdoc/voice-input-capture-rig.md`. The measurement in §2 was correct
  when taken; §4 records why the final resolution is a dedup instead. A reader who
  stops at §2 will think this branch adds a file — it does not.
- **The rig doc's content was not re-verified against the code on this run** — this
  run lands the merge, it does not re-perform peerbox's capture rig. The document's
  measurements are the branch's own records; its sha256 figure and its
  `cli_voice_device_policy.go:260-263` citation were **spot-checked** against the
  file layout on master (both symbol locations resolve on master:
  `voicePulseMonitorSuffix` at `cmd/agent/cli_voice_client.go:104`,
  `voiceVerifyDevice` at `cmd/agent/cli_voice_device_policy.go:260`), and nothing
  in this resolution asserts more than that.
- **The `blob 3cec3171` from the original briefing could not be resolved**: it is
  not an object in this repository (neither the worktree's object store nor
  `/home/user/work/GoPi`'s — `fatal: Not a valid object name 3cec3171`). The
  briefing's *description* of the branch's file matches what was measured here
  (452 lines, `historical`, the rig doc) via the branch tip `dba11bb2`'s blob
  `127f5130`, which is what this resolution used; the shortened name itself was
  not verifiable.
- **No transposition of the drift fixes into master's document** — deliberately,
  with the reason given in §3. If a reviewer disagrees and wants master's design
  doc to cite the rig, that is a new task against master's document, not a merge
  resolution.
