<!-- doc-class: historical -->
# The ceiling-0 ratchet that decays: why internal/stylegate goes red on master, and what would stop it

Measured 2026-10-16 on hosta, ground
`/home/user/workspace/gopi-01a143d21819782092f12d0c273f0cbb`,
HEAD == `refs/heads/master` == `refs/remotes/gitPushcheck/master` ==
`347579b26f9c754a5f6b3005f417e913ee82b91c`, `git status --porcelain` empty.

Task `01a143d21819782092f12d0c273f0cbb` (commit `299c07e46`) closed the red **at the
sites** and left the mechanism alone. This note is the part that outlives that branch.

## The state this note explains

    $ go test ./internal/stylegate/ -count=1
    --- FAIL: TestStyleGate
    gate_test.go:184: stylegate test-sleep: 18 test hit(s), ceiling 0
    gate_test.go:184: stylegate test-skip: 1 test hit(s), ceiling 0
    gate_test.go:184: stylegate ignored-error: 7 production hit(s), ceiling 0
    gate_test.go:184: stylegate unchecked-close: 4 test hit(s), ceiling 0
    FAIL

30 hits, four arms, every ceiling **0**. A pristine master, before any edit — the failure
belongs to no branch.

## Reading the gate correctly (the trap)

`TestStyleGate` fails on the **first** rule whose count exceeds its ceiling, so the output
names one arm and then stops. Master had **four** red arms — a briefing that quotes the
first arm understates the work by 18 sites. Enumerate them yourself:

    go test ./internal/stylegate/ -count=1        # all arms are printed, one after another
    go run ./internal/stylegate/cmd/stylegate -list    # per-finding list, no compile of tests

A `ceiling 0` arm means the sweep closed that rule to zero, so **any** hit is a
regression; a *positive* ceiling that has drifted is a different fault (the ratchet was
never tightened). Here all four are 0.

## Root cause: drift, not lost annotations — and the recurrence is structural

**1. Every hit is new code, not an annotation lost in a merge.** Every hit file was
*added* 2026-10-13 … 10-16, long after the 2026-09-27 sweep `0de16060e` that closed all
four rules to zero. The two hits sitting in files which *did* exist at the sweep are
themselves newer than it — `internal/config/agentguard.go`'s site is absent from that
commit's version of the file:

    $ git show 0de16060e:internal/config/agentguard.go | grep -c "comm, _ := os.ReadFile"
    0

**1422** first-parent merges landed in the window.

**2. Nothing in the landing path runs the ratchet.**

    $ grep -rn "stylegate" internal/smokegate/ Makefile
    (no output)

The merge/verify path runs a **branch-scoped smoke** gate (Rule 27), and the stylegate
package is not in it. A branch is green because its own packages pass; master goes red
because the ten other branches that merged today each dropped a sleep or a discarded
error onto it. The gate's own header predicts this outcome exactly:

> "Without this, the sweep is a one-off cleanup that decays back to the baseline."

Five dedicated fix-up sweeps are on record — `038211066`, `c77c4b1f5`, `1863dbfb3`,
`0de16060e`, `bf75add69` — which is what a decaying ratchet looks like from the outside:
each one closes the tree to green, and nothing holds it there.

**3. Why per-site markers do not hold the line either.** The escapes
(`//stylegate:allow-sleep`, `//stylegate:ignore`, reason ≥ `MinReasonLen`) are the
**correct** instrument — they are what makes a legitimate site reviewable — but they are
*authorial*: they exist only if the author of the new file wrote them. A rule with
ceiling 0 and no check at landing converts every new file into an unowned debt.

## What would prevent recurrence

Put the scan in the path that already runs, so a hit lands red **at its own landing**,
while the author still has the context:

- **Preferred: the existing smoke/verify path.** In-process, no new artifact, and the
  merge that introduces the hit is the merge that reports it.
- **Rejected: a new resident ticker or cron line.** A persistent dependency, needing Rule
  35's four statements; and the standing rule is "prefer what already runs".
- **Not a fix: raising a ceiling or re-baselining.** The ceilings are a one-way ratchet
  (`gate_test.go`: "the number can only go down"); a bump records a regression as policy.

**INFERENCE, labelled as such:** the claim "landing-path enforcement stops the recurrence"
follows from the gate's own stated design and from the five-sweep history, but it has not
been run end to end. Measured are the four arm counts, the add-dates, the 1422 merges, and
the empty `grep`.

## The disposal recipe that worked (reusable)

1. Enumerate **every** over-ceiling arm (above) — never reason from the one the gate names.
2. For each site, read the enclosing function and decide: **can the wait/error actually be
   replaced?** A poll tick with no completion channel, a negative-observation window
   ("the tick did NOT fire again"), and a scripted fixture stall are three shapes where the
   wait *is* the instrument — annotate those; a checkable error is a **real fix** instead.
3. Annotate the **site**, not the file, with a reason that is true of *that* statement.
4. Trailing comment on the offending line, matching the file's existing marker style —
   `gofmt` aligns trailing comments and **rewrites `\t\tcomment` to a single space**, so a
   later scripted edit must match the *rendered* form, not the source tabs.
5. Prove comment-only: `go run ./internal/stylegate/cmd/tokenproof <files>` →
   `OK: N file(s) — identical token streams (comments stripped)`.
6. Prove the ratchet is untouched: `git diff <base>..HEAD -- internal/stylegate/` empty.
7. **Red-proof the green** — a green gate is otherwise indistinguishable from a check that
   stopped working: delete one marker, confirm the arm goes red (it did:
   `test-sleep: 1 test hit(s), ceiling 0`), restore, re-run. Anchor the restore to a
   revision (`git diff --numstat HEAD -- <path>`), not to the index — an index-relative
   diff writes back what the index holds and proves nothing (Rule 5).

## Cross-references

- Filed as its own row (Rule 41): `01a1459e-6383-7dd7-a46c-dc4c3b0a07b3` — the mechanism
  is system layer, so it belongs to peerbox (Rule 28).
- `devdoc/stylegate-drift-and-annotator-defects-20260927.md` — the sweep that closed these
  four rules to zero, and the two gate defects it found beside the drift.
- `devdoc/stylegate-green-20260915.md`, `devdoc/style-sweep-20260912.md` — the earlier
  rounds of the same family.
