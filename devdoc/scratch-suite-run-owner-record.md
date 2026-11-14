<!-- doc-class: normative -->

# A recreated per-run suite root gets an owner record

**What this document is.** The mechanism behind commit `7995fbf` (task `01a163ae`):
why every run of the `internal/tools` suite left one `/dev/shm/suite-tools-run-<N>`
root with **no `.holder`**, why the collector was *correct* to refuse it, and why the
repair claims ownership at the creation point instead of widening the collector.

## The symptom, measured

```
$ ls -d /dev/shm/suite-tools-run-* | while read d; do [ -e "$d/.holder" ] || echo "$d"; done | wc -l
23
```

Twenty-three roots, each holding one `Test*` directory and `002/memory.db`, none with
an owner record. Each acceptance run of

```
./scripts/test-budget.sh -run '^TestResumeArmNeverTouchesALiveHolderWorktreeE2E$' ./internal/tools/
```

added exactly one more. The collector refused every one of them — an **unregistered**
root is not collectable (that refusal is the guard working, never a defect) — so the
only bound was the 6h age window.

## The creator, measured

`strace -f -tt -e trace=mkdir,mkdirat,rmdir,unlink,unlinkat,rename` over one whole run
showed the **same path created twice**, 4.6ms apart:

```
[1] 0700  the suite pin (os.MkdirTemp + RegisterHolder)
2378753 22:36:50.692930 mkdirat(AT_FDCWD, "/dev/shm/suite-tools-run-2937975700", 0700) = 0

[2] the pin removes ITS OWN record, then ITS OWN root
2378762 22:36:51.300338 unlinkat(7, ".holder", 0) = 0
2378762 22:36:51.300378 unlinkat(6, "suite-tools-run-2937975700", AT_REMOVEDIR) = 0

[3] the same path comes back 0755, with its Test* child and 002 in one go
2380864 22:36:51.304962 mkdirat(AT_FDCWD, "/dev/shm/suite-tools-run-2937975700", 0755) = 0
2380864 22:36:51.305000 mkdirat(AT_FDCWD, ".../TestResume…E2E3589518047", 0755) = 0
2380864 22:36:51.305026 mkdirat(AT_FDCWD, ".../TestResume…E2E3589518047/002", 0755) = 0
```

[3] is `initKnowledgeBase`'s `os.MkdirAll(filepath.Dir(GOPI_MEMORY_DB), 0o755)`
(`internal/tools/memory.go`): a gopi child whose environment still carried
`GOPI_MEMORY_DB` into a `t.TempDir()` **under the pin root**, so when it opened its
ledger the parent chain it needed was the root the pin had just removed. `os.MkdirAll`
creates the whole missing chain in one call — that is why root, `Test*` and `002` all
appear with the same `0755` mode. The child then wrote its ledger files and exited.

**The root cause, in one sentence**: the *only* writer of a run root's owner record was
the pin, and the pin had already finished with that root — so a root recreated after it
could never be **judged**, only aged out.

## The repair: claim ownership where the directory is created

`scratch.MkdirAllWithSuiteRunOwner(dir)` (`internal/scratch/suite_run_resurrect.go`):

1. walk up from `dir` and collect the ancestors that **do not exist yet** and whose name
   is in the suite-run namespace (`suiteRunTokens`) — i.e. exactly what `MkdirAll` is
   about to bring back;
2. `os.MkdirAll(dir, DirPerm)` — unchanged contract, error included;
3. for each such root, write an owner record naming **this** process, and say so on
   stderr.

Boundaries, each a deliberate choice:

- **an existing root is never repossessed** — ownership belongs to whoever created it;
- **a directory outside the namespace is untouched** — a configured temp root or the
  operator's own directory never grows a `.holder` because gopi wrote a file in it;
- **a failed registration is reported on stderr and is not fatal** (Rule 15b): the
  directory is usable either way and the age window is still the fallback collector;
- **the collector's deletion authority is unchanged**: an unregistered root is still
  refused. Zero lines changed in `suite_run_selfreap.go`, `base_suite_runs.go`,
  `holder.go`.

## How it is verified

The changed surface is executed directly, against a path shape that does not exist yet:

```
strace -f -tt -e trace=mkdir,mkdirat,openat,write \
  env GOPI_MEMORY_DB=/dev/shm/suite-tools-run-<N>/TestDirectRepro/002/memory.db \
  <binary> memory search probe
```

- pre-fix binary (`HEAD~1`'s `memory.go`): three `0755 mkdirat`s, **zero** `.holder` writes;
- this branch: the same three `mkdirat`s, then
  `openat(..., "/dev/shm/suite-tools-run-<N>/.holder", O_WRONLY|O_CREAT|O_TRUNC|O_CLOEXEC, 0644) = 3`
  and the stderr line naming the registered pid.

The acceptance command was run three times around the change: **unowned count 23 → 23
→ 23**, while each run's new root carried a `.holder` and was collected by the *next*
run at zero age.

Regression guard: `internal/scratch/suite_run_resurrect_test.go` — five arms (record on
the root not the leaf; pid + start ticks; reads back as registered; other directories
untouched; an existing root not repossessed; a failed registration reports). Red-proofed
by neutralising the registration loop, which fails the first arm by name.

## What this deliberately does not do

The 23 historical roots stay where they are until their 6h window expires. Deleting a
root that carries no ownership record is exactly what this mechanism exists to prevent,
and a bug fix is not a licence to re-decide it.
