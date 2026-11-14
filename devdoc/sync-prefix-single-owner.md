<!-- doc-class: historical -->

# Task Report: one owner for the `gopi task sync` stderr prefix (double-prefix bug)

## The defect

Every refusal of the `gopi task sync` parser printed its group prefix **twice**:

```
$ ./bin/gopi task sync --push-only --tags x
gopi task sync: task sync: --push-only takes no --tags (push the row/whole window; partition belongs to sessions)
```

Two owners, one string. `parseTasksArgs` (`cmd/agent/cli_tasks.go`) baked
`"task sync: "` into each refusal it returns, and `runTasksSubcommand` printed
`"gopi <verb>: <err>"` on the way out:

```go
fmt.Fprintf(os.Stderr, "gopi task %s: %v\n%s", verb, err, tasksUsage)
```

So the group spelling was emitted once by the message and once by the printer.
This was the real mechanism — the briefing's first hypothesis (`syncFatal`
re-prefixing) was *not* the cause: every parse-time refusal exits through
`runTasksSubcommand`, never through `syncFatal`. What the briefing measured was
correct; the attribution of the second prefix to `syncFatal` was not. Measured
by running the real binary (both binaries in `## Verification`).

## What was changed

**One owner: the printer that owns the exit.** The constant now exists:

```go
syncErrorPrefix = "gopi task sync: "   // cmd/agent/cli_tasks_sync.go
```

- `syncFatal` prints `syncErrorPrefix` once (it already did — it is now the
  *named* owner rather than one of two literal sites).
- the per-peer session failure line prints the same constant once.
- **every message of the group now carries the BARE body** — the eight Sync
  parse-case refusals in `cli_tasks.go` and `syncTaskIDSessionRefusal` all
  dropped their `"task sync: "` prefix, so the single prefix the operator reads
  is the printer's.

The `--peer` alias validator's label went through the same rule: it was the
literal `"task sync: --peer"` at two call sites (a parse-time one and the
push-only one), i.e. the group prefix baked into a *shared* label. It is now
`syncSubverbFlag(syncPeerFlag)` → `"sync: --peer"`, deliberately bare.

### Two spellings, deliberately apart

The fix keeps two prefixes that are easy to conflate — conflating them is what
produced the bug:

| constant | value | where it appears |
|---|---|---|
| `syncErrorPrefix` | `"gopi task sync: "` | opens a **stderr line** (syncFatal, the per-peer line) |
| `syncProgPrefix` | `"gopi task sync "` (no colon) | **inside** a message body, opening a re-runnable command excerpt |

A message body that names the group (`taskDNSuffix`) does so as a *command the
operator can paste* — never as the error prefix. `TestSyncProgPrefixIsNotTheErrorPrefix`
pins the two apart, because the session refusal's excerpt must remain runnable.

### A regression introduced and caught inside this change

Replacing the refusal's literal argument list with constants first produced
`` `gopi task sync push-only --task-id T` `` — the `--` was lost, because
`syncPushOnlyFlag` is the flag's **NAME** (`push-only`), not its argv spelling.
A new constant closes it (`syncPushOnlyArgv = syncFlagPrefix + syncPushOnlyFlag`)
and the test asserts the excerpt is a pasteable command. Without the argv
assertion this would have shipped as a *broken instruction* while the prefix
count went green — a test that only counts the prefix cannot see it.

## The acceptance test, and the counting method that matters

The refusal assertion now lives in **both** surface files that drive the real
binary:

- `TestSyncRefusalsPrintTheGroupPrefixOnce`
  (`cmd/agent/cli_tasks_sync_taskid_surface_test.go`) — all nine refusals
  (eight parse-time + unknown-flag), table-driven, with the wording each must
  keep;
- `TestSyncRefusalsOnTheSurfaceCarryTheGroupPrefixOnce` and
  `TestSyncPushOnlyRefusesTagsCarriesTheGroupPrefixOnce`
  (`cmd/agent/cli_tasks_sync_surface_test.go`, the G2 surface inventory's own
  test half) — the briefing's three repro invocations, through the rig.

Both call the ONE predicate `assertSyncGroupPrefixOnce`, so the property cannot
drift between files.

**The counting method is the interesting half — and MEASURING it corrected the
briefing's stated metric.** The briefing asks for a "prefix-count assertion"
counting `"gopi task sync: "`. Measured against the unfixed binary:

```bash
printf 'gopi task sync: task sync: --push-only takes no --tags (x)\n' | grep -o 'gopi task sync: ' | wc -l
1        # ← the FULL prefix counts ONCE even on the doubled line
```

because the prefix's leading `gopi ` is spent by the first match, and the
body's re-baked half is only `task sync: ` — there is no second `gopi `. So
`strings.Count(out, "gopi task sync: ") == 1` is **a false green on the very
defect it was written for**, and a per-line count is false for the same reason
(the output is one line). The discriminating metric is the group **spelling**:

```
strings.Count(broken, "task sync: ") == 2     # broken (printer + re-baked body)
strings.Count(fixed,  "task sync: ") == 1     # fixed
```

The predicate asserts the spelling count, keeps the full-prefix count as a
documented false-green witness, and keeps the structural check
(`syncGroupPrefix + syncGroupSpelling`) as the second, independent witness.
`TestSyncRefusalsCountThePrefixByOccurrence` pins the arithmetic itself, so a
future edit that "simplifies" the metric back to the full prefix fails loudly.

(The same trap caught this task's own shell harness; `_repro_evidence.sh`
documents it.)

### Red-proof, measured this session

With ONLY `cli_tasks.go` + `cli_tasks_sync.go` restored to master bytes
(md5 `c44bfe4bf39a307bd022ef70e9e2736c` / `2ac8443ce52a06a4e108ce08cb722cdb`,
both verified) and the two constant-undefined tests dropped so the copy
compiles, all three guards fail:

```
--- FAIL: TestSyncRefusalsOnTheSurfaceCarryTheGroupPrefixOnce/--task-id_on_a_session_face
    the group spelling "task sync: " occurs 2 times on stderr, want exactly 1 …
        gopi task sync: task sync: --task-id applies to --push-only only — --peer surface-peer is a SESSION …
    the refusal body still leads with the group spelling (doubled prefix)
--- FAIL: TestSyncPushOnlyRefusesTagsCarriesTheGroupPrefixOnce
--- FAIL: TestSyncRefusalsPrintTheGroupPrefixOnce
        (… eight more refusals, every one 2 vs 1)
```

Fixed tree: all green. The `--task-id` case is the briefing's own first repro.

## Not Done (honest)

- `cli_tasks_sync_pushonly_rowfilter_test.go` does **not** carry a prefix-count
  assertion: its subject is the WIRE row count for `--push-only --task-id T`
  (a transport leg, not a refusal), and its refusal-shaped cases already route
  through the two files above. Its own subject is covered where it belongs.
- The `${s#*"$P"}`-loop prefix counter in the ORIGINAL shell harness was
  unreliable (it reported 1 for a visibly doubled prefix). Superseded: the Go
  tests use `strings.Count` on the **spelling** (measured correct), and the
  reason the naive counter is wrong is now written down in the predicate and in
  `TestSyncRefusalsCountThePrefixByOccurrence` rather than left as folklore.
- `gofmt -l` flags three files in this worktree (`cli_tasks.go`,
  `cli_config.go`, `cli_web.go`). **Pre-existing**: the flagged regions are far
  from this diff and master's own copy of each file is flagged identically
  (verified by feeding `git show master:<file>` to `gofmt -l`). Not
  reformatted: a whole-file gofmt would bury this change's diff in unrelated
  churn. **The two files this change touches are gofmt-clean** (`gofmt -l` over
  `cli_tasks_sync*.go` and both surface tests prints nothing).

## Verification

- Build: `CGO_ENABLED=0 go build -o _bin/gopi-sp-fixed ./cmd/agent/` → **OK**
  (the same declared build `settings.json` runs after a merge, Rule 12)
- Tests:  `go test ./cmd/agent/ -run 'TestSyncRefusalsOnTheSurfaceCarryTheGroupPrefixOnce|TestSyncPushOnlyRefusesTagsCarriesTheGroupPrefixOnce|TestSyncRefusalsCountThePrefixByOccurrence|TestSyncRefusalsPrintTheGroupPrefixOnce|TestSyncProgPrefixIsNotTheErrorPrefix|TestSyncPushOnlyRefusesTags|TestSyncTaskIDRefused' -count=1`
  → **ok  github.com/nick/gopi/cmd/agent  1.760s**
  Whole-package run: `job-1` = 600s **FAIL**, but NOT from this change —
  `TestG5InventoryMatchesTheHelp` + `TestG5SurfaceCensusIsDerivedFromConstants`
  fail on a STALE `gopi chat` help inventory ("the inventory lists flags
  [no-editor tools verbose], the help advertises [no-color no-editor tools
  verbose]"), in `cli_chat_web_help_surface_basic_test.go` — a file this diff
  does not touch (last written by commit `1574e802`, another agent's in-flight
  G5 work; present with `g5Probe` on master). Its goroutines blocked 9 minutes
  on a `g5Probe.runLive` pipe read. Every sync test passed. (Reported, not
  hidden: the package does not run green end-to-end in this worktree, for a
  reason outside this diff and already named by the failing assertion.)
- Ran:    the three briefing invocations, verbatim, against the freshly built
  fixed binary:
  `HOME=<rt> GOPI_SETTINGS=<rt>/nonexistent.json GOPI_SYNC_PEERS= ./_bin/gopi-sp-fixed task sync --peer surface-peer --task-id t-seed1`
  (and the `--push-only --tags x`, `--serve-remote --peer p` pair)
- Result: each prints `gopi task sync: …` — full-prefix count 1, group-spelling
  count 1, on stderr, exit non-zero. Sibling wording unchanged (see Evidence).
- Commit: **3fc463b7** `fix(cli): one owner for the `gopi task sync` stderr prefix`

## Evidence (self-filed)

**Unfixed binary** (master bytes for `cli_tasks.go` + `cli_tasks_sync.go`,
built side-by-side in a scratch tree):

```
$ gopi task sync --peer surface-peer --task-id t-seed1
gopi task sync: task sync: --task-id applies to --push-only only — --peer surface-peer is a SESSION …;
    to push ONE row use `gopi task sync --push-only --task-id t-seed1`, …
$ gopi task sync --push-only --tags x
gopi task sync: task sync: --push-only takes no --tags (push the row/whole window; …)
$ gopi task sync --serve-remote --peer p
gopi task sync: task sync: --serve-remote takes no peer flags (the initiator identifies itself in the patch)
```

**Fixed binary** (this worktree):

```
$ gopi task sync --peer surface-peer --task-id t-seed1
gopi task sync: --task-id applies to --push-only only — --peer surface-peer is a SESSION …;
    to push ONE row use `gopi task sync --push-only --task-id t-seed1`, …
$ gopi task sync --push-only --tags x
gopi task sync: --push-only takes no --tags (push the row/whole window; …)
$ gopi task sync --serve-remote --peer p
gopi task sync: --serve-remote takes no peer flags (the initiator identifies itself in the patch)
```

Also measured on the fixed binary (unchanged wording, single prefix):
`--push-only --status`, `--status --tags x`, `--claim-remote --peer p`,
`--claim-remote --tags x`, `--peers a,b --serve-remote`, `--peer <undeclared>`
(the whitelist refusal, which routes through the push path), `--bogusflag`.

**Red-proof (the test is red on the unfixed production code):** with the two
production files reverted to master bytes and the new test kept,
`TestSyncRefusalsPrintTheGroupPrefixOnce` fails with

```
--- FAIL: TestSyncRefusalsPrintTheGroupPrefixOnce/push-only_takes_no_--tags
    the refusal body still leads with the group spelling (doubled prefix):
        gopi task sync: task sync: --push-only takes no --tags (…)
```

and passes once the fix is restored.

**Occurrence-count vs line-count, measured** (the false-green that hid this):

```
$ printf 'gopi task sync: task sync: --push-only takes no --tags (x)\n' | grep -o 'gopi task sync: ' | wc -l
1        ← a LINE count: 1 line, 2 prefixes → the trap
$ # strings.Count on the same line
2        ← the assertion the test makes
```

**Stale-binary trap, measured and explained here because it cost a false red:**
`$GOPI_TEST_BIN` overrides `gopiTestBinPath`'s fresh build (`gopiTestBinEnv`).
A suite run that inherits it from the environment executes whatever binary it
names — this task's first full-suite run did exactly that and reported the
*unfixed* wording, which is not a code state that can be reached by a fresh
build of this worktree (confirmed: `go build -o probe ./cmd/agent/` prints the
single prefix). The gate's wording (`resolveGopiTestBin`: "so the tests never
execute a stale prebuilt") is true of the DEFAULT path and not of the override;
recorded rather than patched, since the override is a deliberate operator knob.
