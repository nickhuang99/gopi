<!-- doc-class: historical -->
# Rule 17 escape: the merge-lock `GOPI_HOME` pin

## What happened

Master tip `0317d09a` was RED in `internal/standards`. `TestStandardGates`
reported exactly one Rule 17 (test env isolation) hit that was neither
baselined nor escaped: the `os.Setenv("GOPI_HOME", home)` statement inside
`mergeLockTestHome` (`cmd/agent/merge_lock_test.go`), added by `5c8a8fac`
(`gopi merge` takes the single-instance lock).

The statement is correct. The fix is one annotation, not a code change.

## Why the plain call is right here

`mergeLockTestHome` is the shared fixture of the single-instance-lock scenarios.
It pins `GOPI_HOME` to a `t.TempDir()` so the holder process, the run and the
test's own lock-path resolution all agree on the file under contention.

- It is the **setter half of a reviewed `testutil.SnapshotEnv` pair**; the
  restore is registered with `t.Cleanup` in the same function. That pair is the
  per-call/test-scoped shape Rule 17 sanctions —
  `internal/stylegate`'s `RuleTestSetenvScope` is the direction check that keeps
  it legitimate (it exists because an automated `os.Setenv`→`t.Setenv`
  conversion broke `cmd/agent`'s dispatch test).
- `SnapshotEnv` restores through `os.LookupEnv`, so an ABSENT `GOPI_HOME` comes
  back ABSENT. The hand-rolled `prev := os.Getenv(k)` + restore form is lossy
  (unset and set-to-`""` are indistinguishable) and would leave the variable
  present, pointing at a `t.TempDir()` the framework had already removed.
- The file has no `t.Parallel`, so there is no sibling-test interaction to
  worry about.

## The two channels, and why this one

| channel | effect | verdict here |
|---|---|---|
| `baseline.go` entry | line-keyed permit; **widens** the ratchet; rots on any line shift above it (one permit → one STALE gate error) | **rejected** |
| inline escape `// env-leak-ok: <reason>` | line-shift-proof; the site stays VISIBLE in `TestStandardGateDataReport` as a reasoned exception | **used** |

Placement is load-bearing: `linesAbove()` (`internal/standards/gate.go`) reads
the offending line plus the CONTIGUOUS comment block directly above it, so the
marker sits at the end of that block. A marker separated from the statement by a
non-comment line escapes nothing.

## The second marker on the same line (and why)

The statement also carries stylegate's `//stylegate:ignore <reason>` marker.

stylegate honours **no** `env-leak-ok` marker, and its `test-setenv` rule has
ceiling 0 — this line was its ONE repo-wide test hit:
`cmd/agent/merge_lock_test.go:200: [test-setenv] os.Setenv leaks into the process
env — use t.Setenv`. The precedent shape carries both markers on one line for
exactly this reason: `internal/tools/memory_sync_assets_test.go:106-107` and
`cmd/agent/dispatch_test.go:260` both read
`os.Setenv(…) //stylegate:ignore <reason> // env-leak-ok: SnapshotEnv restores on
return, absence included`.

Without the stylegate marker the branch would go red on the neighbouring gate
over the same reviewed site — which is not a fix, just a moved failure.

## Measured

- before (master `0317d09a`): Rule 17 `hits=115 baselined=91 escaped=23 NEW=1`,
  `TestStandardGates` FAIL;
- after: Rule 17 `hits=115 baselined=91 escaped=24 NEW=0`, STALE=0, and
  `escape: cmd/agent/merge_lock_test.go:209` appears in the data report;
  `go test ./internal/standards/ -count=1` → ok.

`hits` is unchanged (the predicate sees the same sites); only the escape counter
moved. `internal/standards/baseline.go` is untouched — a new permit would widen
the ratchet, and the escape keeps the site visible instead of suppressing it.

## Not fixed here

`internal/stylegate`'s suite is RED on its own on master (`errorf-verb` 8 > 7,
`ignored-error` production 12 > 0, test 189 > 163). Unrelated to this change,
reproduces on pristine master, and deliberately out of scope — it needs its own
task (Rule 26).

## Rework onto master `4cb77bda` (2026-09-19)

This file and the annotation above first landed on `fix/rule17-merge-lock-escape`
(tip `9de28418`, cut from `0317d09a`). By the time it was to merge, master had
moved and the merge-run reported `CONFLICT (content) cmd/agent/merge_lock_test.go`
— a **reworded-same-sentence** conflict, not two disagreeing edits.

### The two sides, diffed against the merge base

The conflict base is `0317d09a`, where the line was a bare
`if err := os.Setenv("GOPI_HOME", home); err != nil {`.

| side | what it added to that line |
|---|---|
| master, via `44cfb05a` ("master's Rule 16/17 gates were red — clear them where this task made them") | the **`env-leak-ok`** inline escape with the child-holder wording, in master's own words |
| branch, via `3853dcf6` | a 7-line explanatory comment block, the **`env-leak-ok`** marker with *different* wording, **and `//stylegate:ignore`** |

Mechanically the branch was a superset (it carried both markers); but master's
wording is live master work on the same sentence, and the two `env-leak-ok`
reasons say the same thing. Resolved by re-deriving against master rather than
concatenating: **master's `env-leak-ok` wording is kept verbatim**, the branch's
comment block is kept (compressed to drop the sentence that only restated
master's marker), and **the branch's `//stylegate:ignore` marker is the
substantive half that master still lacks**.

### Why that marker is the point of this rework — measured

`internal/stylegate`'s `test-setenv` rule honours **no** `env-leak-ok` marker and
has ceiling 0. On master:

```
$ go test -count=1 -run TestStyleGate ./internal/stylegate/     # MAIN repo, master 4cb77bda
gate_test.go:174: stylegate test-setenv: 2 test hit(s), ceiling 0
      sites:
        cmd/agent/merge_lock_test.go:200 (os.Setenv leaks into the process env — use t.Setenv)
        internal/shot/launch_test.go:100 (os.Setenv leaks into the process env — use t.Setenv)
```

Both are red on master. This branch suppresses only its own — the
`internal/shot` one is out of scope and is recorded under "Not fixed here"
above. On the rework tree the same command prints `test-setenv: 1 test hit(s)`
with `internal/shot/launch_test.go:100` as the sole site, so the branch's
marker is exactly what it claims to be: the neighbouring gate's own prescribed
remedy for the one site this task is about, not a widened threshold.

### Final landing on master `26883d07` — the code half became redundant

A second master move (`dcaebcaa` → `26883d07`, "merge rework/proc-identity-single-predicate")
landed the **same** two markers on this line, derived independently:

```
if err := os.Setenv("GOPI_HOME", home); err != nil { //stylegate:ignore the profile stays pinned for the whole SCENARIO, so the undo belongs at test cleanup (t.Cleanup(restoreHome) above), not at this helper's return // env-leak-ok: the child merge-lock holder inherits this profile at exec time, so t.Setenv's automatic restore would come too late; the SnapshotEnv pair above is the restore
```

and gave it a better home for the long form of the argument: a file-level comment
above the helper that names Rule 17's scope carve-out and stylegate's
`RuleTestSetenvScope` explicitly, rather than the inline comment block this branch
carried.

So the code half of this rework is **now redundant**, and the honest resolution is
master's own bytes rather than a third wording of the same two markers —
`git diff master -- cmd/agent/merge_lock_test.go` is empty on this branch. What this
branch still contributes is the **record**: the conflict's two sides, why the
`//stylegate:ignore` marker (not the `env-leak-ok` one) was the load-bearing half
while master still lacked it, and the docclass row that classifies this file — the
`internal/standards` manifest gate requires every `devdoc/` file to be classified,
and master's own move left this file unlisted.

This is the "wrong shape, right conclusion" outcome Rule 26 describes: the rework
did not need to land a second fix — it needed to prove the fix is present and
record how it got there.

### Resolution command (verbatim)

```
git worktree add ~/workspace/gopi-rule17-rework -b rework/rule17-merge-lock-escape master
cd ~/workspace/gopi-rule17-rework
git merge --no-commit --no-ff 9de28418      # CONFLICT (content) cmd/agent/merge_lock_test.go
#  -> resolve the one hunk as described above
git commit
```

### Measured on the rework tree

- `go build ./...` → exit 0; `go vet ./...` → exit 0; `gofmt -l cmd/agent/merge_lock_test.go` → empty.
- `go test -count=1 -run TestStandardGateDataReport -v ./internal/standards/` →
  `escape: cmd/agent/merge_lock_test.go:208` (was `:200` pre-master-move).
  **Re-measured at close-out on the final tip `de12b839` (master `36e0f0bc` merged up): the gate now prints
  `escape: cmd/agent/merge_lock_test.go:207`.** The counter is a line number in a file this branch does not
  modify (it is byte-identical to master), so it moves with master's own edits — the 208→207 shift is
  master's, not the branch's. What matters is unchanged: the marker is present and the file has no permit in
  `internal/standards/baseline.go`.
- `grep -c '<<<<<<<'` over the merged file → 0.
- `git merge-base --is-ancestor master HEAD` → exit 0 (FF-able).

The Rule 17 counter moved off the pre-master `hits=115 … NEW=1` shape: master's
`internal/tools` test suite landed a block of new unannotated `os.Setenv` sites
after `0317d09a` (`TestGate2TestEnvRedProof` reports `123 total hits, 72
baselined, 28 escaped, 23 new` on master AND on this tree — identical), so the
site this task fixes is now visible as an *escape* rather than as the whole
gate's single NEW hit. Those 23 are pre-existing and unrelated — see the
rework report's differential.

