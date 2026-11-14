<!-- doc-class: historical -->
# Merge Resolution: fix/peak-guard-cost-class × master (2026-09-14)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: resolve the merge conflict of `fix/peak-guard-cost-class` (tip `05e84cff`,
worktree `/home/user/workspace/gopi-peak-cost`) into `master` (tip `fc9b06d4`).
**Merge commit**: `f6c29122` (parents `05e84cff` `fc9b06d4`).

## What actually conflicted

Two files, three hunks — both call sites of the SAME guard:

| file | hunks |
| --- | --- |
| `cmd/agent/main.go` | the guard's comment block + the gate condition |
| `internal/standards/baseline.go` | the Rule 16 permit in `errorWrapBaseline` |

Everything else merged clean. The branch also grew files master never had
(`cost_class.go`, `cost_class_test.go`, `peak_guard_costaware_e2e_test.go`) and
master grew files the branch never had (the voice/asr/doubao and service-help
work) — no overlap.

## The real clash: two predicates for "is this start free?"

- **master** (`62bbcfa4`, epic peak-guard-token-free-verbs) exempted **BY NAME**
  the invocations it could prove token-free: `costFreeInvocation` in
  `peak_guard.go` — the help/usage queries and the version query.
- **the branch** (epic peak-guard-cost-aware) replaced the exemption with a
  **per-cost-class** table over the whole verb surface: `costClassOf` /
  `spendingStart` in `cost_class.go`. It exists because master's free class was
  far too narrow: a per-verb whitelist left `gopi worker --stop`,
  `gopi log --last` and `gopi memory search` refused for six hours a day (the
  measured defect: 84 red `cmd/agent` cases with the gate armed for 24h).

Neither predicate alone is right after the merge:

- master's whitelist cannot see a verb spelled behind a config token
  (`gopi --settings X log --last`) — the branch's `classifyOn` slice existed for
  exactly that;
- the classifier alone would re-refuse nothing master freed (its free table is a
  strict superset), but the UNION is what keeps the money fail-closed while the
  free class is as wide as the cost table says.

### Resolution

```go
if spendingStart(args) && spendingStart(dispatchArgs) {
    dispatchArgs = applyPeakGuard(dispatchArgs)
}
```

- `spendingStart(args)` — the cost-class reading of the argv **exactly as main
  received it**. A leading `-h` / `--version` with a trailing `--settings X` must
  still read as the free usage query it is.
- `spendingStart(dispatchArgs)` — the same reading of the **bootstrap-stripped**
  argv (`BootstrapEnvFromArgs`, master's own addition). `gopi --settings X log
  --last` classifies as `log`, not as a bare prompt.
- `classifyOn` **removed**: it was the branch's third slice, equivalent to
  `dispatchArgs`, kept only because the branch never had that variable. One
  reading per spelling, no third slice, no second parser (Rule 1).
- **Refuse only when BOTH readings say spending.** Both readings default to
  SPENDING for argv they do not recognize (`costClassOf`), so an unclassified
  verb is refused in both spellings — the money stays fail-closed. Where the
  union is *wider* than master's whitelist (a locally free verb master refused),
  that widening **is** this branch's deliverable, and it is measured in both
  directions by `TestPeakGuardIsCostAwareBothDirections`.
- The guard invocation, the banner (`config.PeakRefusal`), the exit code
  (`config.PeakGuardExitCode`) and the escape hatches are untouched by both
  sides.

`costFreeInvocation` is **kept** (master's tests and the token-free epic's
documentation reference it). The union is a superset of it by construction —
pinned by the NEW `TestCostFreeInvocationsAreZeroCostClass` in
`cost_class_test.go`, which feeds the whitelist's own vocabulary through the
classifier: a future widening of `costFreeInvocation` that the classifier cannot
see fails there, at test time, instead of at 09:00 in a live window.

### Two master tests had to change (test-time findings, not preference)

1. `TestCostClassVersionFlagsMatchTheDispatchSite` — the branch kept a private
   copy of `"--version"` / `"-v"` and pinned it by file-reading `cli_version.go`
   for the inline literal. Master's service-help epic moved that dispatch site
   onto the SHARED constants (`VersionFlagLong` / `VersionFlagShort`,
   `cli_verbs.go`), so the reading test failed on the merged tree for a reason
   that was never about cost: it was asserting an inline literal that had become
   a constant reference (Rule 13 wanted it). Fix: `outputOnlyVerbs` now uses the
   shared constants (no copy to drift), and the guard asserts the constant name.
2. `TestPeakGuardKeepsCostFreeVerbsAvailable` — it listed `gopi worker --status`
   as a "spending mode". That was true only under the name whitelist, where
   everything outside it was refused; under the cost-aware classifier a control
   verb on a resident service is free (no lock, no round, no model). The read
   moved to the exempt half **with its reason**; the teeth stay because the
   guarded half keeps `task run` and adds a BARE `gopi worker` (the foreground
   round-runner — the shape the operator could not start).

### baseline.go (Rule 16)

The %w permit for the site in `cmd/agent/main.go`. **Both** parents re-keyed it from 1035:
HEAD → 1063 (its cost-class comment grew), master → 943 (the service-help epic
shrank the usage heads above it). **Neither number survives the merge**: the
merged tree's comment is a targeted rewrite (a union paragraph, not two
appendable sides). The key was therefore **re-measured in the merged tree**,
never picked from a parent's number — a line-keyed permit may only name the tree
it was measured in (a stale permit is a silent permission):

```
$ grep -n 'escape a literal prompt' cmd/agent/main.go
990:  ...%w (escape a literal prompt starting with %v as \%v)", body, err, payloadFilePrefix, payloadFilePrefix)
```

Exactly one line; same site; same reason (the only rendered `%v` args are the
`payloadFilePrefix` STRING, twice, and the error IS wrapped with `%w`).

## Verification

See the task report: `go build ./...`, `go test ./internal/standards/
./internal/config/ ./internal/tools/`, `go test ./cmd/agent/`, plus the
guard's own both-directions e2e.

## Addendum (2026-09-14, post-verification): the doc gate + the verification environment

### 1. The two devdocs this merge adds must carry the worker-ownership pointer

`internal/testutil`'s `TestWorkerOwnershipPointerPresent` flags any file under
`devdoc/`, `briefings/` or `scripts/` that mentions a worker-launch / task-run
shape without the ONE canonical pointer line. Both documents added by this
merge quote exactly those shapes as the MEASURED DEFECT (`gopi worker --daemon`
unstartable, `gopi task run` refused) — so the gate is right to ask: the quote
is evidence, and without the pointer a future reader can read it as a recipe.
Commit `b3a95bf1` adds the line to both
(`<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->`).

**The remaining five hits are pre-existing master debt, not this merge's** —
measured on a clean master worktree (`/home/user/workspace/gopi-rawlog-newest`,
`79738cb0`), the same five files are already reported:
`devdoc/daemon-cwd-escape.md`, `devdoc/rule18-voice-device-else.md`,
`devdoc/task-list-inprogress-scan-20260914.md`,
`devdoc/worker-help-capability-first.md`, `devdoc/worker-status-lock-assert.md`.
They are skipped here deliberately: fixing another branch's documents is a
different task, and the gate's own message is a one-line backfill — reported
below rather than smuggled into a conflict resolution.

**The alternative disposition was considered and rejected.** The sibling
peak-guard epic took the OTHER option — `workerOwnershipDismissed` with the
reason "evidence: the probe matrix … the denial ITSELF being the measurement"
(`devdoc/peak-guard-token-free-verbs.md`, reviewed on master). It fits a
captured transcript whose only copy-able command is the exempted one. It does
NOT fit these two files: both print copy-able spellings (`./bin/gopi worker
--daemon`, `./bin/gopi task run t-1`) as probes a reader re-runs, so the
pointer is a TRUE instruction rather than the false one that dismiss reason
describes. Pointer chosen on that distinction, not on convenience.

### 2. The red suite was mostly the HOST, not the merge — measured

The first full run showed `cmd/agent` (602s, panic in a dispatch test),
`internal/tools` (`database or disk is full (13)`,
`TestFailedViewHidesArchived`) and `internal/testutil` red. Root cause is
environmental, and it is upstream of the merge:

- the suite scratch home is `GOPI_TEST_SCRATCH_HOME` →
  `/dev/shm/gopi-test-scratch` (**tmpfs**), and `df -h /dev/shm` reported
  **7.8G / 7.8G used, 12M free** — a full tmpfs under `/home/user/.gopi/sessions`-sized
  sqlite fixtures IS the "disk is full (13)" and the source of the hangs;
- 6.1 G of that was stale per-run residue (86 `suite-cmd-agent-run-*` roots,
  many `live-holder-rig-*` at 150–330 M each);
- `gopi scratch-gc` is NOT the unblock: it reported `removed=0` because the age
  floor is `scratch.StaleAge` = 6 h and the residue was younger (Rule 22's GC,
  working as designed).

The distinguishing test was a **differential run**, never a single red suite: on
a clean master worktree at the merge base, `internal/daemoncwd` — red on the
branch's first sweep — is **green**, i.e. a scratch-full artifact, not a
conflict artifact. The re-run therefore pins the workspace off `/` per Rule 22
(`GOPI_TEST_SCRATCH_HOME` + `GOPI_TEST_HOME` + `TMPDIR` on `/home`, verified
`stat -c %d` ≠ `/`), with the worker/parent env stripped
(`GOPI_TASK_ID/GOPI_TASK_SESSION/GOPI_SESSION_ID/GOPI_DAEMON_READY_FD/
GOPI_WORKER_DAEMON_CHILD/GOPI_WORKER_LOCK_FD`).
