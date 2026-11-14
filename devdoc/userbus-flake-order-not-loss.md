<!-- doc-class: historical -->
---
class: historical
---

# The unlinked-socket flake: a GREEN was the absence of a guard, and RED was the truth

**Task**: `01a12cca-5850-76d2-81da-b0e60ba58948` (session
`/home/user/.gopi/sessions/01a12cca-5850-76d2-81da-b0e60ba58948`)
**Host**: hosta, Linux 6.8.0-138-generic, suite scratch root on `/dev/shm` (tmpfs)
**Subject**: `cmd/agent`'s `TestUserBusProbe_RealUnlinkedSocketIsDetected`, reported as
flaky on an UNMODIFIED tree — 6 runs, 4 RED / 2 GREEN.

## 1. The question the reporting row could not answer

The row (parent `01a12ca8-71d1`) measured the flake at its own HEAD `b7e5eddd6` and
carried the honest note that it had NOT instrumented `observeUserBusPath` and could not
say whether the cause was a fixture defect, an observation defect, or a real product
signal. It also could not re-run at `origin/master`, because the reporter had already
pushed base commits of its own.

## 2. What this run measured

| tree (sha of the test file) | command | N | result |
|---|---|---|---|
| current master tip `1508e3821` (`4acff90b…`) | `go test ./cmd/agent/ -run TestUserBusProbe_RealUnlinkedSocketIsDetected -count=1` | 30 | **30 GREEN / 0 RED** |
| the fix tip `0f9840c68` (`4acff90b…`, byte-identical to master's) | same | 12 | **12 GREEN / 0 RED** |
| the reporter's ancestor `b7e5eddd6` (`e95f318…`) | same | 10 | **3 GREEN / 7 RED** |

The fold fix (`userBusFoldUnixRows`, `0f9840c68`, landed on master 05:10:57) repairs the
flake, and master is byte-identical to the fix tip for both files.

## 3. The row-level dump: the kernel never lost the socket — the ORDER decided the answer

Two independent stdlib probes (archived as `artifacts/ubdump/main.go`,
`artifacts/ubdump2-main.go` in the task's session directory) reproduced the baseline
observation sequence
(`bind+listen → scan → dial+close → unlink → scan`) and printed EVERY `/proc/net/unix`
row carrying the path:

```
== baseline-sequence observation over 20 rounds: firstMatch==LISTEN(GREEN)=4 firstMatch!=LISTEN(RED)=16 no-row=0 ==
== over 40 rounds: firstMatch=CLIENT(RED)=34 firstMatch=LISTEN(GREEN)=6  roundsWhereClosedClientRowPersisted=40
```

The first line is the ARCHIVED artifact's own summary
(`artifacts/baseline-sequence-20rounds.txt`, in the task's session directory), the
second is `artifacts/order-probe-40rounds.txt`. The rate is NOT a constant: five
further 20-round batches of the SAME probe (`artifacts/order-rate-spread.txt`) put the
client's row first in 20, 18, 14, 5 and 5 rounds out of 20 — i.e. the client-first rate
swings between 0.25 and 1.00 across batches on one host. That swing IS the flake,
stated as a measurement.

Three facts read off the rows, not inferred:

1. `no-row=0` — the binding row (`state=01`, non-zero inode) **always** survives the
   unlink. The kernel never dropped it; that was the reporter's standing hypothesis and
   it is now refuted as the mechanism.
2. `roundsWhereClosedClientRowPersisted=40` — the client row (`state=02`, `inode=0`)
   survives the client's own `close()`. So the offending row does NOT depend on a live
   client; it depends only on the KERNEL'S ORDERING of same-path rows.
3. That ordering is the flaky bit, and it swings: the archived batches put the client's
   row first in 16/20 and 34/40 rounds, and five further 20-round batches in 20/20,
   18/20, 14/20, 5/20 and 5/20 — a rate that varies between batches, not a constant.

Therefore the pre-fix `userBusKernelBinding` (b7e5eddd6 lines 507–529: `return b, true`
inside the scan loop) described the CLIENT — `bound:true / listen:false / inode:0` —
which is verbatim the reported failure. The flake is a real, deterministic property of
the code meeting a kernel-side ordering that varies per run.

## 4. Why the reporter saw 4/6 red and a different host saw 3/10 (no contradiction)

The two ratios are not in tension — they are the same defect at two orderings. `n=6`
(4R/2G) and `n=10` (7R/3G) both have wide intervals around a per-round client-first
rate that this host measured swinging from 0.25 to 1.00 *between batches of one probe*
(`artifacts/order-rate-spread.txt`: 20, 18, 14, 5, 5 client-first rounds out of 20);
a shorter sample simply draws a different number of greens. Nothing about either tree
differs — the ordering is the variable.

## 5. Why a GREEN here means LESS than a RED

The strongest single piece of evidence in this run inverts the usual reading:
**the green was the absence of a guard.** The reporter's `b7e5eddd6` tree contains
`TestUserBusProbe_RealUnlinkedSocketIsDetected`, but `grep -c
TestUserBusKernelBinding_FoldsEveryRowForThePath` on that tree returns **0** — the
behavioural lock on the fold arrived with the fix. So on the pre-fix tree: RED = the
defect firing; GREEN = the defect not firing *this time*, with nothing watching it.
A review rule worth carrying forward: for a flake, a green run is the weaker datum —
prefer counting reds, and require a guard that fails deterministically on the mutant.

## 6. Mutant proof — the lock is real (Rule 10.5 / 命中≠实现)

Reverting ONLY the fold to first-match on a throwaway copy of master
(`artifacts/mutant-firstmatch-proof.sh`) makes the behavioural test fail
deterministically, with a failure anatomy identical to the reported one:

```
--- FAIL: TestUserBusKernelBinding_FoldsEveryRowForThePath
    fold = {found:true bound:false listen:false inode:0 rows:1}, want {found:true bound:true listen:true inode:17620230 rows:2}
    fold = {found:true bound:true listen:true inode:17620230 rows:1}, want {… rows:2}
MUTANT live control over 5 runs: GREEN=1 RED=4
```

So the property the flake is about is locked by a test that cannot pass by luck — which
is the difference between "the flake went away" and "the flake cannot come back".

## 7. Why this run added no product fix

There is nothing left to fix: the fix is in master and the flake's residue is a TEST
premise, not a product path. Inventing a second fix would be 重复造轮子 (Rule 8.8).
What this run adds is the missing evidence the reporting row could not produce
(a master-tip verdict, the raw ordering dump, the mutant proof) plus one durable doc so
the analysis is not re-derived for a third time.

## 8. A SECOND red on the same command — and it is the base, not the flake
The epic's acceptance is "`make test-budget` 无红". The budget run on THIS run's branch
while it was still based on the pre-move master came back **red — on a different test**:

```
--- FAIL: TestMergeUnionRowCheckOnTheRealManifest (0.23s)
    merge_union_rows_test.go:135: the check reports 2 finding(s) against the repo's OWN tree …:
      devdoc/plain-prompt-first-user-message-scope.md (line 1317): evidence anchor:
        cmd/agent/cli_runpath_boundary_rp1_test.go no longer carries the recorded text
        "// did not start the message at all." …
      devdoc/session-record-read-path-double-filter-gate.md (line 1616): evidence anchor:
        internal/agent/clock.go no longer carries the recorded text "stamp := a.turnStampLine()" …
```

That is NOT the userbus flake and NOT a userbus regression. Measured two ways in the same
minute:

| tree | `go test ./cmd/agent/ -run TestMergeUnionRowCheckOnTheRealManifest` |
|---|---|
| a materialised copy of current master (`git archive master`, `0b3c1de29`) | **3 GREEN / 0 RED** |
| the branch's OLD base, `git archive 1508e3821` (master's own tip at the time) | **0 GREEN / 2 RED** |
| this run's branch, then based on that old base | **0 GREEN / 3 RED**, the same 2 findings each time |
| this run's branch AFTER `git merge master` | **3 GREEN / 0 RED** |

The middle row is the one that localises it: the red was **master's own** at `1508e3821`,
and a later merge (`0b3c1de29`) repaired it by re-keying the two rows. This run's branch
never introduced it — it merely inherited it and held it one merge longer.

The only difference that mattered was `devdoc/DOC-INDEX.tsv`: `git diff HEAD master --
devdoc/DOC-INDEX.tsv` showed 3 changed rows, and the two failing ones are exactly the two
rows master had **re-keyed** (the anchor text was re-pointed at the line that really
carries it now). The manifest is a claim about the tree it ships with, so a branch that is
behind the tree its manifest was generated for fails the check — **deterministically, not
flakily**. The check itself is a floor by design
(`merge_union_rows_test.go`, "resolve the REPOSITORY'S OWN manifest against the
repository itself").

**The remedy is the delivery-point sync (Commit Path step 2), and it is measured**:
`git merge master`, then the same command → **3 GREEN / 0 RED**, and `gopi tool doccheck`
stays exit 0. So a red `make test-budget ./cmd/agent` on a live branch has at least two
very different causes, and they are told apart by scope, not by re-running: **a red that
reproduces on a materialised master is a real defect; a red that disappears after the
sync was the base.** Re-running is the wrong instrument for either.

**One further reading of the middle row, worth keeping**: at `1508e3821` the manifest rows
disagreed with the tree they shipped with, and the test that caught it lives in the SAME
package as the flake. So a "cmd/agent is red" report from that window names two unrelated
facts, and neither of them is evidence about the other — the flake was the userbus
ordering, and this was a stale manifest anchor. An investigator who re-ran `cmd/agent` and
saw red would have learned nothing about either.

## 9. The lesson this run files about its own reporting (Rule 5 / Rule 41)

While verifying the numbers above against the archived artifacts, this run found two
unbacked figures **in its own already-committed devdoc**, and fixed both in a follow-up
commit (`d774cecbe`):

- §3 quoted the summary line `firstMatch==LISTEN(GREEN)=1 …(RED)=19` — a batch that was
  **never archived**. The archived file's own summary line reads `GREEN=4 RED=16`.
- The same section stated the client-first rate as a CONSTANT `0.85–0.95`. Five further
  batches of the same probe measured 20, 18, 14, 5 and 5 client-first rounds out of 20 —
  a swing between 0.25 and 1.00, and **the swing is the flake's measurement**.

A figure whose run was not archived has no backing, even when it really occurred; and in a
flake, a RATE is the wrong shape — report the spread.

## 10. A THIRD finding, and it is another REAL flake in the same package

Chasing the epic's acceptance ("`make test-budget` 无红") past the sync found a **second
live flake**, and this one IS a flake, measured on master's own tree:

```bash
D=$(mktemp -d /dev/shm/gopi-master-XXXX); cd <repo> && git archive master | tar -x -C $D
cd $D && for i in $(seq 1 8); do go test ./cmd/agent/ \
    -run 'TestChatVoicePrivacyRowCarriesOnePrefix' -count=1; done
```

| tree (materialised archive) | N | result |
|---|---|---|
| master tip `0b3c1de29` | 8 | 4 GREEN / 4 RED |
| master tip `0b3c1de29`, second batch | 8 | 6 GREEN / 2 RED |
| **total, master's own tree** | **16** | **10 GREEN / 6 RED** (≈0.37) |

It was also the ONLY red of the synced tree's budget run
(`make test-budget PKGS='./cmd/agent'`, 911s, budget 25m NOT tripped). The failure anatomy
is the same every time — the grid dump carries three `[voice] no text in the last 6s
window` rows and **no `PRIVACY` row at all**, so the test times out at
`voice_module_defects_test.go:63` waiting for the privacy row; its SECOND wait
("no text in the last") is already satisfied, i.e. the timeout is on step one.

**Filed, not fixed** (Rule 41 — the finder does not fix; Rule 37 · the evidence lives in
this run's session directory): task `01a12d20-d091-756f-92c3-477267ab1c41`, with
`artifacts/bug-chatvoice-privacy-row-flake.md` + `artifacts/cv-master-rerun.txt` attached.
That row is PARKED behind this one (`parent_task_id` = this run's id), which is the
harness's own chaining, not a claim about ordering.

**The generalisation, which is the point of this whole document**: `cmd/agent` held TWO
independent live flakes plus ONE deterministic base-drift red inside one run of one
command. "the package is red" is not a finding; the finding is always WHICH test, on WHICH
tree, at WHICH rate — and each of the three has a different remedy (a fold fix already
landed / a sync / a still-open defect).
