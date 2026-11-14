<!-- doc-class: historical -->
# The merge-time UNION COMPILE gate (⓪c) — closing the silent-green merge

## The defect it closes

2026-09-16, master `e791c9c7`: **every test in `cmd/agent` was unrunnable.** The
package's TEST BINARY did not build:

```
cmd/agent/merger_resume_arm_wiring_test.go:104:6: funcBody redeclared in this block
cmd/agent/cli_tasks_rebuild_pass_test.go:204:6: other declaration of funcBody
```

Two branches had each added a helper with the same name to the same package's
test files — `db1fcef6` (`func funcBody(t, src, decl string)`) and `362278e2`
(`func funcBody(t, src, name string)`). Each was **green alone**; their union was
a redeclaration. Both landed, and the merge path reported success.

The briefed symptom (`funcBody` duplicate) was already fixed by `0ec7d3b0`
(`parseFuncBody` / `braceFuncBody`), so this task closed the **mechanism** — the
reason nobody could see it.

## Why the existing gate could not see it (measured, not argued)

The ⓪b BUILD gate (`internal/tools/merge_build_gate.go`) runs the repo's
**declared** build — Rule 12. For GoPi that declaration is

```json
"build": "CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/"
```

and **`go build` never compiles `_test.go` files.** Measured on the exact shape
(two files in one package declaring the same function, a Go module with a working
package `p`):

| command | result |
| --- | --- |
| `go build ./...` (the ⓪b command) | **exit 0** — green |
| `go test -run '^$' -count=1 ./...` | `p/b_test.go:5:6: funcBody redeclared in this block` |
| `go vet ./...` | `vet: p/b_test.go:5:6: funcBody redeclared in this block` |

So `go build`-green branch + `go build`-green master + **red union**. The gate is
not "missing" a case; it is measuring a different question. Two facts make the
distinction structural:

1. **Build-input equality does not imply output equality.** Each branch and
   master are each fine; the merge result is a **third revision** neither of
   their own builds ever visited.
2. **The declared command is the repo's contract.** Teaching ⓪b to compile tests
   would either change what the declaration means or make the gate run a command
   the repo never declared — a guess, which Rule 12 forbids.

Hence a separate lane that answers a separate question.

## The gate

`internal/tools/merge_union_gate.go`, wired into `taskMergeOne` as **⓪c** (after
the contract gate ⓪ and the build gate ⓪b, before `git merge` — a refusal must
leave master byte-identical by construction).

**The union is materialised from git objects; no working tree, no index and no
branch of the repo is touched:**

1. `git read-tree -m <base> <branch> <master>` with an **alternate index**
   (`GIT_INDEX_FILE` in the child's environment — the plumbing idiom; the repo's
   own index is never read or written).
2. `git write-tree` → the merge result's tree. **A textual conflict yields
   unmerged index stages and no tree** (`git ls-files -u` lists the paths):
   MERGE_RESULT_UNAVAILABLE — the resolver lane owns that, exactly as before
   this gate. This gate owns the complementary half: *texts that merge cleanly
   still have to compile*.
3. `git archive <tree> | tar -x` into scratch (off the `/` partition,
   `scratch.RuntimeClassMergeBuild`). **No worktree is created**, so the repo
   gains no registration and there is no registration to repair — which is why
   this gate does not carry the ⓪b gate's checkout-repair machinery.
4. The **compile-only pass** in that checkout, with the gate's own Go caches
   (reused from ⓪b, never the operator's): `go test -run '^$' -count=1 ./...`
   builds every test binary and runs **nothing** — no ledger is opened, no
   fixture, no daemon.

**The comparison is measured, never assumed.** The union runs first (the common
case is one compile and a PASS). Only when it fails does the gate measure the
premises — the branch's own tree and master's own tree, with the *same*
command — and refuse only when **both compile**:

| branch alone | union | verdict |
| --- | --- | --- |
| PASS | PASS | pass (log line) |
| FAIL | FAIL | ABSTAIN — branch-introduced, not union-introduced (⓪b + rework own it) |
| PASS | FAIL | **REFUSE** — the shape this gate exists for |
| (abstain/unmeasurable) | any | ABSTAIN |

The red-target arm is a SAFETY PROPERTY rather than a description of master's
current state (measured 2026-09-16: `go test -run '^$' -count=1 ./...` on master
exits 0 in 190s). It matters because the state is reachable — a test-only break
landing on master turns every candidate into "branch and union red", and an
enforcing gate would then refuse branches that caused nothing: the ⓪b gate's own
2026-09-13 lesson. The arm is pinned by a fixture that breaks master's test
build deliberately (`TestMergeUnionGate_TargetAlsoRedAbstains`).

**ABSTAIN is the default** (the merger's DETECT-never-block contract): no Go
module (no known way to compile tests — Rule 12, never a guess), a missing
toolchain/tar, an unreadable index, an unmerged tree, a module-resolution
failure (the 2026-09-15 false-refusal predicate, reused — Rule 14), a timeout,
an unusable scratch tree or cache, a branch the target already contains.
Escape hatch: `GOPI_MERGE_UNION_GATE=0`; budget override:
`GOPI_MERGE_UNION_GATE_TIMEOUT_SEC`.

## One lane, two measurements

A refusal reuses the ⓪b lane **entirely** — the `build-refused` action, the
archive tag (`refs/tags/archive/<branch>`), the deduped branch-rework
notification row and its six-element brief — because the fix a reworker owes is
the same: code that must compile. The refusal arm is now the shared
`taskMergeRefuseUncompilable`; only the gate **name** differs in the log
("BUILD gate" vs "UNION COMPILE gate"), so an operator can tell which
measurement refused the branch.

## What it does NOT cover (honest scope)

* **A textual conflict's union is not compiled.** `read-tree -m` reports stages
  and no tree; the resolver lane materialises and fixes it. If the resolver's
  result is not built either, nothing catches it — that gap is unchanged by this
  gate and is recorded here rather than papered over.
* **`go vet` was deliberately rejected as a leg.** It is the incident's own repro
  command and does catch the duplicate — but so does the test compile, and in Go
  1.24 vet reports `unsafe.Pointer`→`uintptr` conversions in test files (a field
  offset is inherently unsafe), which would refuse branches whose union is
  identical to master in the failing package. The refusal stays as narrow as the
  defect.
* **Non-Go repos abstain.** A repo that is not a Go module declares no test
  compile, and this gate does not guess one.
* **Master's own red test build makes the gate silent** (fail-open by design)
  until it is repaired. The residue it leaves is a log line naming the red
  target, not a refusal.
* **`fix/ledger-firstopen-budget` was the live hazard, and it is no longer one
  (re-probed 2026-09-16).** That branch carries the OLD name — `git grep -n
  '^func funcBody' fix/ledger-firstopen-budget -- cmd/agent/` →
  `cmd/agent/cli_tasks_rebuild_pass_test.go:204:func funcBody(...)` — cut from a
  base whose sibling used the same name, so an earlier revision of this devdoc
  recorded that landing it un-resolved re-creates the collision. The measurement
  says otherwise: the three-way merge puts that file at **stage 0** (clean), and
  the merged content is master's renamed `braceFuncBody` — the branch never
  touched that file or its sibling since the merge base (`git diff --name-only
  e1229f85 fix/ledger-firstopen-budget -- <both helper files>` is EMPTY), so the
  sibling's rename travels with master and wins without a resolver decision. The
  union's `cmd/agent` tests were compiled from the materialised union tree and
  pass (`go test -run '^$' -count=1 ./cmd/agent/` → `ok … 4.793s`). The same
  probe still reports `internal/tools/tasks.go` unmerged, i.e. a TEXTUAL
  conflict elsewhere in that branch — the resolver lane's case, which this gate
  abstains on. So the hazard paragraph is RETIRED as a hazard: what remains is
  the ordinary resolver work on `tasks.go`, and the gate's job there is to
  refuse a union that fails *after* the resolver commits one.

## Verification

* `go build ./...` / `go vet ./internal/tools/` — green.
* `internal/tools` suite — see the task report for the run.
* RED proof: `TestMergeUnionGate_RefusesTheIncidentShape` (union measured to be
  the collision *and* to fail compiling, then refused),
  `TestMergeUnionGate_ControlUnionCompilesIsNotRefused` (same shape, one name
  changed: the union is measured to COMPILE),
  `TestMergeUnionGate_TargetAlsoRedAbstains`,
  `TestMergeUnionGate_BranchAlsoRedAbstains`,
  `TestMergeUnionGate_TextualConflictAbstains`,
  `TestMergeUnionGate_DisabledAbstains`,
  `TestMergeUnionGate_NoGoModuleAbstains`,
  `TestMergeUnionGate_TimeoutIsAnAbstention`,
  `TestTaskMerge_UnionRefusalArchivesAndNotifies` (end-to-end: archive tag,
  master unmoved, exactly one rework row carrying the brief).
