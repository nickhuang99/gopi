<!-- doc-class: historical -->
# devdoc: the "inode 0 after unlink" red is the FIXED first-match defect — the reporter ran a baseline that predates the fix

**Class**: historical record of one run's verdict (task `01a12cd9-cf2b-7174-9016-3edff10748b6`,
2026-10-12). It is not a standing rule; the standing design lives in
`devdoc/userbus-probe-unlinked-row-fold.md` (the fix's own devdoc) and in
`cmd/agent/daemon_userbus_probe.go`.

## The question this run answered

Bug row `01a12c9c-…` reported `TestUserBusProbe_RealUnlinkedSocketIsDetected`
**stably red** inside a whole-package `./cmd/agent` run:

```
--- FAIL: TestUserBusProbe_RealUnlinkedSocketIsDetected (0.00s)
    daemon_userbus_probe_test.go:458: the kernel must still hold the socket after
      the path was unlinked: {path:/dev/shm/…/ubdiag…/bus kind:bus
      statErr:0xc00021b470 bound:true listen:false inode:0 connectErr:… connectErrno:2}
    daemon_userbus_probe_test.go:461: the bound socket's inode must be unchanged
      by the unlink: before 18013307, after 0
```

The suspicion under test: master already carries a fix for this phenomenon
(`0f9840c68`, "fold every /proc/net/unix row for a path — one connect() puts a
client row first"), and the reporter ran a **branch baseline that predates it**.
If so, the bug row needs closing, not a new fix.

## Verdict — covered, and the baseline gap is proven at git level

**Measured (commands and output in the run's session dir `evidence/`):**

1. **Ancestry.** `0f9840c68` is an ancestor of this run's HEAD (`d3cdb619a`) and of
   master — `git merge-base --is-ancestor 0f9840c68 HEAD` → 0. It is **NOT** an
   ancestor of either reporter baseline:
   `git merge-base --is-ancestor 0f9840c68 0b743cbac` → 1 (and `… 71543117d` → 1).
   The reporter's own session names its baseline verbatim
   (`task/01a12c70c12c750fa30f4b483fdd110d`, tip `71543117d`) — the grep and the
   raw line are in `evidence/reporter-baseline-declaration.txt`. The same log's
   own arm-run line reads `71543117d  ===  ok  github.com/nick/gopi/cmd/agent
   0.539s  ARM_RC=0`, so the tip is the reporter's own reading, not a later
   reconstruction.
2. **Timeline.** reporter's failing whole-package run stamped
   `2026-10-12T05:05:27+08:00` (exit=1); `0f9840c68` was authored
   `2026-10-12 05:10:57 +0800` — **five and a half minutes later** — and reached
   master through `ce2a9ad56` at `05:14:24`. The failing run could not have
   contained the fix.
3. **The code shape the reporter ran** (`git show 71543117d:cmd/agent/daemon_userbus_probe.go`):
   `userBusKernelBinding(path)` scanned `/proc/net/unix` and **returned at the
   first matching row**. The fix replaced it with a whole-table fold
   (`userBusFoldUnixRows`). `grep -c userBusFoldUnixRows` on that baseline file = **0**.
   *Limits of this item, stated as a limit*: the **test file** also changed between
   the baseline and HEAD (214 insertions), so a diffstat alone cannot say which of
   the two flipped the verdict — the A/B in item 5 is what carries that, and the
   probe-only isolation that would have separated them does not compile (below).
4. **Record the trap that makes the finding: the pre-fix fixture could not MASK the defect.**
   `ubUnlinkedSocketPath` was a one-shot value captured **before** the unlink, so
   the `bound && !listen` pair in the report is not "two states disagreeing" — it
   is one observation whose `bound=true` came from the first branch and whose
   `listen/inode` came from the first **row**: the kernel's client row
   (`St=02`, `Inode=0`), which `unix_mkname` makes real at the moment the probe's
   own `dial` runs. That is exactly what `0f9840c68` fixes.
   (Inferred from reading the two revisions; the measured evidence is items 1–3
   and the A/B in item 5. Stated separately because Rule 41 requires it.)

## Independent reproduction on this run's tree (Rule 31 — the case was run, not imagined)

`/proc/net/unix` row order is a **kernel** property, so a green cannot be assumed —
it was reproduced on both sides of the fix:

| tree | invocation | result |
|---|---|---|
| this run's HEAD `d3cdb619a` (has the fix) | `go test ./cmd/agent/ -count=30 -run TestUserBusProbe_RealUnlinkedSocketIsDetected -v` | **30 PASS / 30 RUN, 0 FAIL, ok 0.500s** |
| baseline `71543117d` (predates the fix) | same case, `-count=10` | **10 FAIL / 10 RUN** — every one `before <nonzero inode>, after 0`, `listen:false inode:0` |

The failing side reproduces the reporter's **exact** output shape (`listen:false
inode:0`, `connectErrno:2`, "the bound socket's inode must be unchanged by the
unlink: before N, after 0"). The inode differs per run (`18780084`…`18780112`
here, `18013307` in the reporter's run) — it is an inode, not a constant.

The fix is therefore **attributable**: same invocation, same host, opposite
verdicts, and the only difference is the fold.

**And the reporter's own log already shows the red was not a whole-package
coupling at all** — it ran the case **standalone** on its own baseline
(`go test ./cmd/agent/ -count=1 -timeout 5m -v -run
'TestUserBusProbe_RealUnlinkedSocketIsDetected'`) and got `=== RUN … --- FAIL`
with the same reading, `before 18013307, after 0`
(`evidence/reporter-standalone-check.txt`). That is exactly what this run's
baseline A/B reproduces (10/10 FAIL, inode varying per run), so the defect was
**deterministic on the pre-fix lookup**, not load-dependent — a whole-package run
was merely where it was noticed first.

**A sharper isolation was attempted and is NOT available — recorded so nobody
re-tries it.** Reverting `cmd/agent/daemon_userbus_probe.go` alone (HEAD's tree,
that one file replaced by `71543117d`'s blob `40ee1d337`, the test left at HEAD's
`138d75b5…`) does not compile: the commit changed the struct
(`found`/`bound`/`rows`) and the signature (`userBusKernelBinding`:
`(binding, bool)` → `(binding, error)`) as well as the lookup, so probe and test
move together — 12 build errors, `evidence/isolation-probe-reverted.log`. The
attribution rests on the two-tree A/B above, plus one measured fact that does
bound it from the other side: `0f9840c68` is the **only** commit in
`71543117d..HEAD` that touches `cmd/agent/daemon_userbus_probe.go`
(`git log --oneline 71543117d..HEAD -- cmd/agent/daemon_userbus_probe.go` → that
one hash).

## Whole-package run

The reporter's red occurred inside a whole-package run, so the close-out check
was the same shape: `make test-budget PKGS='./cmd/agent'` on this tree — the
declared runner. Raw stream and the runner log are in the session dir.

**The case's own verdict inside that live whole-package run is MEASURED** — this is
the exact shape the reporter's red came from, so it is the one that had to be
re-measured, not only the standalone one:

```
"Test":"TestUserBusProbe_RealUnlinkedSocketIsDetected","Output":"=== RUN   TestUserBusProbe_RealUnlinkedSocketIsDetected\n"
"Test":"TestUserBusProbe_RealUnlinkedSocketIsDetected","Output":"--- PASS: TestUserBusProbe_RealUnlinkedSocketIsDetected (0.01s)\n"
```

with the stream counters at that instant at **4014 run / 3971 pass / 0 fail / 42
skip** (`evidence/inpackage-progress.txt`, snapshotted 06:55:10). The reporter's
whole-package run was this shape and printed `--- FAIL`; here it prints `--- PASS`.

**The run then FINISHED, and the final numbers are in** (`evidence/wholepkg-final-verdict.txt`,
extracted after the process exited at 06:58:01):

```
stamp: 2026-10-12T06:42:21+08:00 host=hosta pkg=./cmd/agent budget=25m elapsed=933s
       load_end=5.17 3.66 3.68 cores=32 exit=1 outcome=fail stream=run-1791758541-1425364.json
--- failures (assertion/build; the budget did NOT trip) ---
  --- FAIL: TestChatVoicePrivacyRowCarriesOnePrefix (3.20s)
```

The whole-package run is `outcome=fail`, and the **only** failure in it is
`TestChatVoicePrivacyRowCarriesOnePrefix` (row `01a12d20-d091-756f-92c3-477267ab1c41`
is already filed and pending; two earlier rows on that same case are `cancelled`).
**This case is not among the failures**, and its own `RUN`/`PASS` pair appears in
the finished run's stream — so the shape the reporter's red came from now re-measures
green end to end, in the same declared runner, on the same host, with the same
`-count=1` whole-package invocation.

*One measurement caveat, stated rather than smoothed over:* the stream's
package-level `pass`/`fail` action line could not be extracted by the grep used
here, and the stream's `fail` action count (2) is higher than the runner's one
named failure — the runner's own summary is the authoritative verdict and is what
is quoted above. What is measured directly for THIS case is its `--- PASS` line
inside the finished run plus its absence from the failure list.

## Why the sibling row `01a12cca-…` does not contradict this

A sibling bug row had already observed the same case as a *flake* (6 runs, 4 red)
in a worktree whose `origin/master` predated the fix. Its report **already read
the working tree**: `observeUserBusPath` → `userBusKernelBinding` → and found
`grep -c userBusFoldUnixRows = 0` — i.e. it named the fix that its baseline
lacked. Two rows describing one already-fixed defect; this one closes it.

Its *other* argument — offered as exoneration — is the one a later reader must
not copy: that row's briefing records
`git diff --quiet HEAD origin/master -- daemon_userbus_probe{,_test}.go` → **exit 0**
("byte-identical to `origin/master`"), plus
`sha256sum …daemon_userbus_probe_test.go = e95f3180…`. Both numbers are sound;
the conclusion read off them is inverted, because **the ref it certified against
was pre-fix too**: `e95f3180…` is exactly the test file at `71543117d`, and it
becomes `4acff90b…` at `0f9840c68` (the fix changed the test file as well, 224
insertions). So byte-identity certified sameness with a tree that also lacked the
repair — and the exoneration sentence says "not caused by this row's changes",
which is true and irrelevant, while the reader takes away "this red is not real
here".

**And that step is now MEASURED, not inferred** (it was flagged as an inference in
the first revision of this document; `evidence/sibling-identity-rerun-today.txt`
records the replacement). The sibling's own recorded HEAD survives in the object
store even though its branch and worktree were reclaimed:

```
$ git show b7e5eddd6:cmd/agent/daemon_userbus_probe_test.go | sha256sum
e95f3180530a8f9f4753ac58cc77d5ee9b00fdf81b2d174e1b1f1aff27124236  -
$ git show b7e5eddd6:cmd/agent/daemon_userbus_probe.go | grep -c userBusFoldUnixRows
0
$ git merge-base --is-ancestor 0f9840c68 b7e5eddd6 ; echo exit=$?
exit=1
```

So that row ran the **pre-fix** tree by direct measurement: its test file hashed to
`e95f3180…` (the pre-fix blob), its probe carried **zero** occurrences of
`userBusFoldUnixRows` (the symbol the fix introduces), and `0f9840c68` is not its
ancestor. Its "byte-identical to `origin/master`" line therefore certified sameness
with a tree that also lacked the repair. Whatever ref a byte-identity claim names,
resolve that ref's relation to the repair before trusting it —
`evidence/hash-boundary.txt` carries the hashes and the diffstat.

## Companion files (same run, same commit chain)

- `userbus-unlinked-inode-zero-diagnosis.verification.md` — the commands and their
  verbatim results behind §1–§5, plus the isolation attempt's exact build errors and
  the disclosure that this row's closure verification was **not** auto-enqueued
  (backlog guard at cap 100, `task_verify_closure.go` — by design, not a defect).
- `userbus-unlinked-inode-zero-attribution.txt` — the raw transcript: commit stats,
  ancestry probes, blob diffstats, the pre-fix vs post-fix function bodies.

## Standing consequence

Nothing here narrows the original report: the case *was* stably red, on a tree
that genuinely lacked `0f9840c68`. What the report could not see is that the
repair had already been authored and merged five minutes after the failing run.
The lesson is the pair (both are Rule 41's job): **a bug filed from a whole-package
run carries that run's baseline**, and a reader deciding "already fixed" must
resolve the baseline from git rather than from the reporter's green, because a
*flake* report and a *baseline* report of the same red look identical in prose.
