<!-- doc-class: historical -->
# The merge-time BUILD gate learns to compile the TEST tree

**2026-10-12** · task `01a12fcd-e63e-77c1-87df-82c2015b5c30` · host hostb-Laptop

## The defect

`go build ./...` reads no `*_test.go` file. Neither does the build this repo
declares for itself in its own settings table
(`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`, CONFIG.md §2d). So a branch
whose **test tree** does not compile is green to every gate shaped like a build:

```
$ perl -0pi -e 's/\nconst NotifyEventLedgerDivergence = taskNotifyEventLedgerDivergence\n//' internal/tools/tasknotify.go
$ CGO_ENABLED=0 go build ./...
               BUILD_EXIT=0
$ go vet ./...
vet: cmd/agent/task_ledger_agreement_redproof_test.go:311:21: undefined: tools.NotifyEventLedgerDivergence
               VET_EXIT=1
```

The deleted declaration was referenced from a **redproof test in another
package** (`cmd/agent`) while production code used only the unexported twin, so
nothing outside the test tree noticed. The branch could land, master's declared
rebuild would pass, and the red account would be handed to whoever ran the
suite next.

## Why it is a real gap and not a duplicate of an existing gate

Measured coverage against the gates that actually run:

| gate | when | does it catch a test-only break? |
|---|---|---|
| merge-time BUILD gate (`mergeBuildGate`) | before `git merge` | **NO — it was only the declared build** |
| union gate (`mergeUnionGate`) | before the merge, on the merged result | yes *when* a conflict arises — it compiles a union tree only for conflicted pairs, so a clean merge never gets there |
| post-merge VET gate (`runPostMergeVetGate`) | after master moved | yes, but **after the fact** — the merge already landed, and the remedy is an unwind |
| client pre-merge smoke (`finishCleanupPreMergeMergedChecks`) | author's own hand-over | yes for the CLIENT premerge path — but it is the author's gate, not the queue's |
| Rule-12 declared rebuild | after the merge lands | **NO** — same declared build |

So the only machine check that refused a test-only break *before* the merge was
the union gate, and that one only asks when the merge conflicts.

## The fix

`internal/tools/merge_build_gate.go` — the gate now resolves a **list of legs**
per revision instead of a single command. Leg 0 is unchanged (the repo's
declared build, or the Go default for a Go module). A checkout that is a **Go
module root** gains leg 1:

```
go test -run '^$' -count=1 ./...
```

`-run '^$'` matches no test, so every test binary is **BUILT** and nothing is
**run**: no ledger opened, no daemon started, no fixture executed. The command
is spelled exactly as `mergeUnionGateGoTestCompileCmdFmt` spells it, and the
budget is the union gate's own default (`mergeBuildGateTestCompileTimeoutSec =
mergeUnionGateDefaultTimeoutSec` — referenced, not re-typed).

Deliberate choices, each with the alternative that was rejected:

- **Not `go vet`.** Vet is *not* a merge-hazard measurement: the union gate
  already rejected a vet leg by name, because `unsafe.Pointer`→`uintptr` is
  reported by vet on Go 1.24 and would refuse branches that only touch a field
  offset. Compiling tests is the property this gap is about, and nothing more.
- **Not a settings key.** The leg is derived from the tree the gate is already
  looking at (is there a `go.mod`?), so it needs no new configuration channel
  (Rule 35). A non-Go repo keeps exactly its previous measurement.
- **The premise is measured in the CHECKOUT, not in the repo argument.** The
  first cut passed the scratch work ROOT instead of the detached checkout, and
  the leg silently never fired — caught by the acceptance test, not by reading
  the code (the debug print shows `status=PASS cmd="go build ./..."` on a
  revision whose tests do not compile).
- **First FAIL wins; an ABSTAIN never masks a later leg's FAIL.** A leg whose
  command word is not on the gate's PATH is pre-flighted with `exec.LookPath`
  and skipped with its own reason — a host without the toolchain must be
  distinguishable from a clean branch, and must never read as a branch defect.
- **The refusal names the leg that failed.** With more than one leg "the branch
  does not compile" no longer tells the reworker which command to verify with,
  so `mergeBuildGateVerdict.BuildCmd` is the failing leg's own command and the
  detail line is suffixed when that is not the declared build.

## Verification

`go test ./internal/tools/ -run 'MergeBuildGate|MergeUnionGate|TaskMerge|RepoSweep|VetGate|MergePass'`
— see the task report for the recorded run.

The gate was also driven end to end against a throwaway fixture repo
(`/home/user/workspace/gopi-probe-vet-fix`), with the rebuilt binary:
`gopi task merge <branch>` **refused** the test-only break
(`build-refused=1`, master tip byte-identical, branch archived for rework), and
**landed** the clean branch that adds a const together with the test using it.
