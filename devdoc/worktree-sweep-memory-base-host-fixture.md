<!-- doc-class: historical -->
# The worktree-sweep memory-base test asserted a host it never constructed

**Task** `01a0acc2-c8ef-7391-9e42-9eacbd2a8d8d` · branch `fix/sweep-memory-base-label` (from master `ee1d0fd9`)
**File changed** `internal/tools/worktree_sweep_memory_base_test.go` (test only)

## Symptom

`TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy` reported (line 357):

```
the refusal reports the no-root label "no-root" — this arm DID resolve roots;
the plan must name what was consulted
```

The briefing recorded it as a pre-existing red on master. It could not be
reproduced on master here — it was green, on the same commits, in the main repo
and in a detached worktree.

## Root cause: the test is host-state dependent, and its precondition was implicit

The memory-base arm of the sweep reports the root that the **scratch home
resolves onto**, measured — never spelled (Rule 22):

- `worktreeSweepConsultedRoot` (`internal/tools/worktree_sweep.go:615`) falls back
  to `memoryBaseRoot()` (`worktree_sweep_memory_base.go:68`);
- `memoryBaseRoot()` needs the scratch home to resolve onto a **memory
  filesystem**, and says so: *"A home ON `/` is not a memory base"*; an
  unmeasurable home returns `ok=false` because "cannot tell is never authorise"
  (Rule 15).

On a host whose scratch home is the symlink
`<home>/workspace/gopi-test-scratch → /dev/shm/gopi-test-scratch`, the arm names
`/dev/shm` and the test passes. On a host whose scratch home is disk-backed the
arm honestly reports `worktreeSweepNoRootLabel` (`"no-root"`) plus the reason's
"no memory-base root is available here" arm — and the test fails, deterministically.

So the producer is right and the **assertion** was the wrong half: it forbade an
outcome the producer must be able to produce, without ever constructing the host
state that produces it.

### Deterministic repro of the red (the briefing's line 357, verbatim)

```bash
GOPI_TEST_TMPDIR=/home/user/workspace/scratch \
  go test ./internal/tools/ -run TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy -count=1
```

`GOPI_TEST_TMPDIR` is the honoured scratch-home override
(`internal/testutil/scratchroot.go`: `SuiteTempRootOverride` → `ScratchHomeDir`,
then `PinSuiteTempRoot` writes it back as `GOPI_TEST_SCRATCH_HOME`). **Setting
`GOPI_TEST_SCRATCH_HOME` directly does not work** — that writeback overwrites it
inside TestMain, which is why a first attempt with it looked green.

| scratch home | result |
|---|---|
| symlink onto `/dev/shm` (tmpfs) | PASS |
| `GOPI_TEST_TMPDIR` on the block-backed `/home` | FAIL @ line 357 |

Measured on master `ee1d0fd9`; identical behaviour in the main repo and in a
detached worktree, i.e. **the commit is not the variable — the host's scratch
home filesystem is.**

## Fix (test-only)

The fixture constructs the arm instead of inheriting it: pin the scratch home
onto a **measured** memory filesystem with the file's own existing helper
`memoryBaseFixtureRoot(t)` (measures `/dev/shm` and `os.TempDir()` by statfs
magic; skips, never fakes, on a host with no writable memory filesystem), and
place the fixture registration inside that base while the only declared root sits
elsewhere. The assertions keep their substance.

Why not "(b) fix the assertion properly" by accepting `"no-root"`: accepted, the
test would skip its whole subject — the out-of-policy refusal's obligation to NAME
what it consulted. Constructing the precondition keeps the obligation under test
on every host.

## Red-proof (the assertion still discriminates)

Re-injecting `return worktreeroots.Root{}` into `worktreeSweepConsultedRoot`
makes the test fail at line 374 with the original message in **both** host states
— so the fix does not buy green by weakening the check. Reverted by file restore.

## Reusable recipes

1. **A test whose result depends on the host's scratch home must pin it.**
   `GOPI_TEST_TMPDIR` is the only effective override (`GOPI_TEST_SCRATCH_HOME` is
   overwritten by the suite's own writeback); point it at a directory on a
   block-backed filesystem to reproduce the "no memory base" host, and use the
   package's measured tmpfs fixture helper for the other arm.
2. **"Red on master" is a claim about a commit — verify it in a pristine
   detached worktree before treating it as a delta.** Two failures that look
   identical can come from the environment, not the tree; the same test passing
   and failing across hosts pins the variable fast.
3. **When a label has an honest "I cannot know this" arm, the producer must keep
   it and the test must construct the state — never delete the arm to go green.**
