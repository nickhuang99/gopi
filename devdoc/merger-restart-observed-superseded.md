<!-- doc-class: historical -->
> **CORRECTION-ANCHOR (2026-09-19).** Record. The `--restart`-during-a-round rule
> below belonged to the resident merger's control face, which was deleted
> (`f9ff1a89`, `55c4d6f3`): every spelling of `gopi merger` now refuses and the
> one-tick merge is the bare `gopi merge`. The reasoning about rounds vs loops is
> kept as the design record.

# `fix/merger-restart-observed` — RESOLVED AS SUPERSEDED, with the remainder named

**Task**: `01a0b47e-6d26-745d-9f64-c1d80ac36fc6` — rework of the archived branch
`fix/merger-restart-observed` (tip `b7948ada`, 2 commits), whose merge into
master conflicted in `cmd/agent/merger_restart.go` and
`cmd/agent/merger_restart_e2e_test.go`.

**Outcome: no production change is landed. The branch's design is superseded on
current master, and the branch's stop half cannot even COMPILE against it.**
This document is the deliverable: the proof, the exact resolution command, and
the one narrowed finding that is still open.

## The conflict, measured (not quoted from the briefing)

A real merge attempt on current master (`a9a96cd7`) in a throwaway detached
worktree (`~/workspace/gopi-mrst-conflict-probe`):

```
$ git merge --no-commit --no-ff b7948ada
Auto-merging cmd/agent/merger_restart.go
CONFLICT (content): Merge conflict in cmd/agent/merger_restart.go
CONFLICT (modify/delete): cmd/agent/merger_restart_e2e_test.go deleted in HEAD
        and modified in b7948ada. Version b7948ada of … left in tree.
Auto-merging internal/tools/merger_schedule.go
Automatic merge failed; fix conflicts and then commit the result.
```

Two files, both named by the briefing — and each one is a **different failure**:

| file | shape | why |
|---|---|---|
| `cmd/agent/merger_restart_e2e_test.go` | **modify/delete** | master DELETED it in `f9ff1a89` (888 lines) |
| `cmd/agent/merger_restart.go` | content | two competing generations of the same control face |

The modify/delete is the louder half: the file does not exist on master, so this
is not a text conflict to resolve — it is a surface on the other side of the
retirement. `git apply -3` on the archived patch says the same thing in its own
words (`error: cmd/agent/merger_restart_e2e_test.go: does not exist in index`).

## Why master supersedes the branch

### 1. The retirement closed both live entry points

`f9ff1a89` (2026-09-19, *feat(merger): retire the resident merger daemon by
entry-point closure*) makes every `gopi merger …` spelling exit 1 before the
parser runs:

```
$ sed -n '358,372p' cmd/agent/main.go
	// RETIRED (2026-09-19): the resident merger daemon is closed at BOTH of its
	// live entry points …
	retireMergerFace(CliVerbMerger)
	return false // unreachable: retireMergerFace exits
```

`cmd/agent/merger_retired.go` states the cause: *"`--stop` is only answered at a
TICK BOUNDARY, so an in-flight tick cannot be interrupted"*. Every surface
`fix/merger-restart-observed` develops lives behind that call, and this
repository's own Rule 12 in `devdoc/merger-retired.md` is explicit that the
implementation is kept only as the ground for a re-development.

### 2. The contract the branch developed had already LANDED on master

Master carries the same diagnosis, in the same shape, with its own design record:

| the branch's clause | master's implementation | master's record |
|---|---|---|
| the stop observes the SIGNALLED PID, never the flock | `mergerWaitLoopGone` + `tools.PidAlive` | `devdoc/merger-stop-takeover-contract.md` |
| the holder after the stop is named, not asserted | `mergerLiveLockHolder` + `mergerStopTakeoverHoldFmt` | same doc |
| a `--restart` during a round is REFUSED before signalling | `mergerRestartRoundInFlightFmt` | same doc |
| the class decision table lives in the source | header of `cmd/agent/merger_restart.go` | `19e06bb2` |

`19e06bb2` (*fix(merger): --stop concludes on the LOOP, not on the flock the
round holds*) is an ancestor of master, and it was merged from the **sibling**
dispatch `fix/merger-stop-takeover-contract` (task `01a0b3c8`) — the branch
`b7948ada` was itself fast-forwarded over. A THIRD independent task
(`01a0b400`) re-derived this same defect and reached the same conclusion in
`devdoc/merger-stop-observable-already-fixed.md`: *"no production change is
needed. The defect is fixed on master."*

### 3. The branch's stop half does not COMPILE against current master

This is the fact that ends the "rebase and reland" question. The branch's
`merger_restart.go` was placed on current master and built:

```
$ git checkout b7948ada -- cmd/agent/merger_restart.go \
      cmd/agent/merger_restart_stop_holder_test.go internal/tools/merger_schedule.go \
      internal/tools/merger_takeover_live.go devdoc/merger-restart-contract.md
$ CGO_ENABLED=0 go build ./...
# github.com/nick/gopi/cmd/agent
cmd/agent/merger_restart.go:414:10: opts.noBranchScan undefined (type *tasksOptions has no field or method noBranchScan)
build EXIT=1
```

`opts.noBranchScan` is not merely absent — master **retired it on purpose** in
`30b421fc` (*"fix(merger): retire the --no-branch-scan knob F4 orphaned (Rule
15/21)"*) and added a gate that keeps it retired:

```
$ grep -n 'func TestRetiredFlagIsNotAdvertised' cmd/agent/cli_tasks_audit_test.go
149:func TestRetiredFlagIsNotAdvertised(t *testing.T)
```

That test (a) refuses `gopi merger --no-branch-scan` by name, (b) asserts
`taskFlagMember` does not admit it, and (c) scans `main.go`, `cli_tasks.go` and
**`merger_restart.go`** for the code spelling and fails if it is found. So
restoring the branch's line would not be "resolving a conflict" — it would be
reintroducing a retired knob and turning a master gate red. The branch's own
commit `cf695f54` — *"fix(merger): retire the --no-branch-scan knob F4
orphaned"* — is precisely the change master already has, so the branch is a
different-generation copy of landings master made independently.

## The resolution command (the reported deliverable)

Nothing is merged and nothing is applied. The archived series is resolved as
superseded, and this document is the record. The command that produces the
conflict set above — reproducible, and the one that justifies the decision:

```bash
# reproduce the conflict the take-over reported (throwaway detached worktree)
cd ~/work/GoPi
git worktree add --detach ~/workspace/gopi-mrst-conflict-probe master
cd ~/workspace/gopi-mrst-conflict-probe
git merge --no-commit --no-ff b7948ada     # -> UU cmd/agent/merger_restart.go,
                                            #    DU cmd/agent/merger_restart_e2e_test.go
git merge --abort
```

and the check that decides it:

```bash
cd ~/workspace/gopi-merger-restart-rework
git checkout b7948ada -- cmd/agent/merger_restart.go    # the branch's stop half
CGO_ENABLED=0 go build ./...                            # -> build EXIT=1, noBranchScan
git reset --hard HEAD                                   # leave the tree clean
```

**Supersession, not deletion**: `b7948ada` and `cf695f54` remain reachable on the
archived branch, so the branch's path is preserved as evidence (Rule 23) —
including the parts master chose differently.

## Not Done (honest) — the one narrowed remainder

The branch contains **one thing master's control face does not have**, and it is
recorded here rather than silently dropped:

- **The two-bound escalation ladder.** Master's `mergerStopLiveLoop` sends SIGTERM
  and then waits a single `mergerStopDeliverTimeout = 60 * time.Second`; it never
  escalates (`git grep SIGKILL master -- cmd/agent/merger_restart.go` → no
  matches; the escalation shape exists elsewhere as `cmd/agent/child_stop.go`,
  which the branch's own comment cites as the repo standard). The branch's ladder
  — `mergerStopGraceWait = 5s`, then an ANNOUNCED SIGKILL, then
  `mergerLoopExitWait = 10s` — makes the command's wall time a property of that
  code rather than of the tick's work.
- **This is not dispatachable as-is.** It is a change to a face that is currently
  UNREACHABLE (`retireMergerFace` exits before every merger spelling), and its
  motivating measurement was taken on a resident loop. Landing an escalation for
  a dormant surface would add untested behaviour to code the operator cannot
  reach. The honest place for it is the re-development `devdoc/merger-retired.md`
  already frames: it belongs with whichever attempt brings the resident loop (or
  its successor) back to a live entry point.
- **The branch's second finding (re-parenting) is retained, not relanded.**
  `ProcParentPID`/`ProcAge`/`TakeoverArgv0Of`/`TakeoverProbeCandidate` and the
  pre-signal descendant scan exist to tell *which* holder is the round. Master
  answers the same question from `tools.LockEvidences` (which drops dead rows),
  so the two are alternative mechanisms for one question — not a gap.

## One flake met on the way, and why it is NOT this resolution's

The scoped smoke above failed once with
`merger_takeover_scope_test.go:80: close writer: close |1: bad file descriptor`.
It is **pre-existing on master** and filed as its own row
(`01a0b5cd-daa8-7c68-ba6d-86762bb71fa2`, tag `bug`):

- the SAME command fails **1 run in 4** on a pristine detached worktree at
  master `a9a96cd7` with no local changes — observed there as
  `testing.go:1267: TempDir RemoveAll cleanup: unlinkat …/TestMergerTakeoverBriefingRegistersTheRoundsRecord…/001: bad file descriptor`;
- the named tests never fail alone: 6/6 green for
  `TestMergerTakeoverBriefingRegistersTheRoundsRecord`, and `-count=8` green for
  `TestMergerTakeoverEscapeSelectsRungOne`;
- my own tree is source-identical to master except this document.

The fix and the acceptance are in the filed row; it does not gate this
resolution, whose deliverable is a document plus a clean worktree.

## Verification

- Build/vet on the fresh branch off master (`fix/merger-restart-observed-rework`
  @ `master` `a9a96cd7`), observed:
  `CGO_ENABLED=0 go build ./...` → `build EXIT=0`; `go vet ./...` → `vet EXIT=0`.
- Scoped smoke on the packages the resolution touches, observed green after the
  flake above was isolated: `go test ./cmd/agent/ -run 'TestMergerRetired|TestMergerTakeover|TestMergerWorkLock' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 8.363s` / `11.430s` / `8.338s`, 3/3
  `EXIT=0` at the final commit `eafa4804`;
  `go test ./internal/tools/ -run 'TestLock' -count=1` →
  `ok github.com/nick/gopi/internal/tools 1.293s`, `tools EXIT=0`.
- Conflict reproduction: `git merge --no-commit --no-ff b7948ada` → the two-file
  conflict table above; `git merge --abort` → clean.
- Supersession: `git merge-base --is-ancestor 19e06bb2 master` → exit 0;
  `git merge-base b7948ada master` → `9ce8e159` (the branch's own base).
- Negative compile proof: `CGO_ENABLED=0 go build ./...` with the branch's
  `merger_restart.go` in place → `build EXIT=1`, `opts.noBranchScan undefined`.
- Worktree clean: `git status --porcelain` → 0 lines (Rule 30).

## Evidence (self-filed)

| claim | command | observed |
|---|---|---|
| master builds/vets clean at the start point | `CGO_ENABLED=0 go build ./... ; go vet ./...` | `build EXIT=0`, `vet EXIT=0` |
| the reported conflict set is exactly 2 files | `git merge --no-commit --no-ff b7948ada` | `UU cmd/agent/merger_restart.go`, `DU cmd/agent/merger_restart_e2e_test.go` |
| the branch's stop half cannot compile | `git checkout b7948ada -- cmd/agent/merger_restart.go && CGO_ENABLED=0 go build ./...` | `build EXIT=1`, `merger_restart.go:414:10: opts.noBranchScan undefined` |
| master forbids that knob's return | `grep -n 'func TestRetiredFlagIsNotAdvertised' cmd/agent/cli_tasks_audit_test.go` | `149:func TestRetiredFlagIsNotAdvertised(t *testing.T)` (scans `merger_restart.go`) |
| master has no escalation in the stop half | `git grep -c SIGKILL master -- cmd/agent/merger_restart.go` | no matches (0) |
| the sibling contract landed | `git merge-base --is-ancestor 19e06bb2 master` | exit 0 |
| scoped smoke green | `go test ./cmd/agent/ -run 'TestMergerRetired\|TestMergerTakeover\|TestMergerWorkLock' -count=1` ×3 | `ok … 11.430s` / `8.363s` / `8.338s`, `EXIT=0` ×3 at `eafa4804` |
| the flake is pre-existing on master | same command on a pristine worktree at `a9a96cd7` ×4 | 3 green + 1 `FAIL … TempDir RemoveAll … bad file descriptor`; the named tests alone 6/6 green |
| worktree clean | `git status --porcelain` | 0 lines |
