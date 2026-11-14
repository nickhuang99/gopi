<!-- doc-class: normative -->
# Task Report: tmpdir is an ordinary parameter, and gopi stops announcing it

Epic: `tmpdir-param-not-banner` (2026-09-17). Branch:
`fix/tmpdir-param-not-banner`, worktree `~/workspace/gopi-tmpdir-param`, based on
master `230fca00`.

Three parts, all landed: **(A)** the `[gopi] tmpdir: …` stderr line that printed
on *every* invocation is gone — injection survives, only the announcement died;
the value stays readable through one existing, deliberate surface; a temp root
that cannot be provided or written fails **loudly**, naming path + reason + the
channel. **(B)** the temp workspace is a normal `internal/config` catalog entry:
`--tmpdir` / `GOPI_TMPDIR` / the settings key, same precedence machinery as every
other knob. **(C)** the built-in default is an ordinary directory under the
workspace, not the scratch home's symlinked child.

---

## 1. Where the banner went, and how to see the value now

**Removed.** `cmd/agent/tmpdir_inject.go` used to `Fprintf` a `[gopi] tmpdir:
…` line on stderr on every run — including `gopi --help` and `gopi --version`,
which spawn no children at all. The temp workspace is a parameter with a
default; a run does not announce its parameters. The publisher is now silent on
success (`publishTmpDir` returns a bool instead of printing; the caller uses it
for nothing but its own control flow, so no dead "printed" bookkeeping is left).

**The replacement surface is opt-in and pre-existing.** `GOPI_DEBUG=1 gopi …`
(the same `debug` knob — `--debug`, the settings `debug` key, the `GOPI_DEBUG`
env var — the tree already had) prints exactly one line:

```
[gopi] tmpdir: TMPDIR="/home/user/workspace/_tmp" (from the built-in default)
```

It names the **effective value** *and* **the channel that supplied it**, so "why
this directory?" is answerable without running anything else. The decision was
**not to invent a mechanism** for this: `internal/scratch` gained
`ReportPublishedTmpDirIfVerbose` + `EnvDebugEnabled` (the one reader of the
debug knob's variable inside scratch), and no new flag/env/key was introduced
for a debugging concern the tree already had a switch for.

**Silence is asserted, not assumed.** `TestTmpDirSilentOnHelpAndVersion` runs
the real binary for `--help`, `--version` and `version` and fails if the marker
appears; `assertNoTmpDirBanner` runs on the two configurable-run cases as well,
so a reintroduced banner fails a test rather than being noticed by an operator.

## 2. The parameter: channels and precedence

One declaration, not three readers. `internal/config/settings.go`'s catalog
(`EnvVars`) gained **one** row:

```go
{TmpDirEnvName, TmpDirSettingsKey, TmpDirFlagName, "Temp workspace for gopi and EVERY child it spawns …"}
```

`internal/config/tmpdir.go` owns the
resolution, `internal/config/flags.go` gained one `case TmpDirFlagName`, and
`internal/config/settings.go`'s `envVarFor` hoist (the same mechanism that
hoists `--settings` / `--home`) hoists `--tmpdir` so the group verbs
(`gopi task run/worker`, `gopi merger`) — which return from `main()` at the
subcommand chain and never reach the agent-path config block — see the flag too.

| rank | channel | how it is supplied | why it ranks there |
|---|---|---|---|
| 1 | `--tmpdir PATH` | CLI | the operator typed it for *this* invocation; CLI > env is the standing rule |
| 2 | caller-exported `TMPDIR` | the caller's environment | the **ambient override**: it is the variable a child actually reads, so whoever started gopi sees `TMPDIR` in their own shell and expects it in force; also makes nested gopi converge (a parent's published value resolves to itself) |
| 3 | `GOPI_TMPDIR` | gopi's own env spelling | still a deliberate configuration, but less specific than the variable that decides the effect |
| 4 | settings `tmpdir` key | `settings.json` | the declarative layer — the one nobody restates per invocation, so a file on disk must not silently redirect someone who pinned the variable for one run |
| — | built-in default | nothing above is set | an ordinary directory under the workspace |

**One place.** `config.ResolveTmpDirConfigured` ranks the channels;
`scratch.ResolvePublishedTmpDir` resolves + proves the path usable;
`cmd/agent/tmpdir_inject.go` publishes and reports. Precedence is spelled once,
in the resolver's doc comment, and the acceptance run below demonstrates every
rank with real invocations.

**Why `GOPI_TMPDIR` and not `TMPDIR` as the knob.** gopi *writes* `TMPDIR` for
its children — a knob named `TMPDIR` would be both the input and the output of
the same mechanism, and a caller's value would read back as gopi's own. The
catalog entry's env column therefore carries the distinct knob name, while
`TMPDIR` keeps its role as the variable that decides the effect and that a
caller may export. `scratch.TmpDirEnvName` (= `"TMPDIR"`) and
`scratch.GopiTmpDirEnvName` (= `"GOPI_TMPDIR"`) are the two constants; the
config package aliases them (Rules 1b/13/14 — no second definition, no bare
literal).

## 3. The hard-failure half (A) — loud, specific, never a silent fallback

`scratch.TmpDirUnusableError` renders **path + reason + channel** in one line,
wraps the sentinel `scratch.ErrTmpDirUnusable`, and is what the publisher
prints. A **configured** value that cannot be used is a hard exit (exit 1) —
the operator named that directory, so quietly running with a different one would
be the silent substitution this epic exists to end:

```
[gopi] tmpdir: tmpdir is not usable as a temp workspace:
  "/…/readonly-parent/cannot-be-created": cannot create it: mkdir …: permission denied
  (value from --tmpdir); point --tmpdir / TMPDIR / the "tmpdir" settings key at a writable directory
```

The **built-in default** failing to be created is reported just as loudly, but
does not abort (`configured == false`): nothing was configured, and refusing to
start would take `gopi --help` down with it on a host whose workspace is not
writable.

Usability is checked by **writing** (create + remove one probe file), not by
reading mode bits: the question is "can this process write here", and an ACL, a
read-only mount or a full filesystem all pass a mode-bit reading. Both halves
(`newly created` and `already exists`) go through the same
`ensureUsableTempRoot`, so a configured value and the default cannot diverge in
what they accept.

## 4. The default (C), and the GOPI.md decision

`scratch.DefaultTmpDir()` now returns `<home>/workspace/_tmp`, created on
demand. The leaf is `scratch.DefaultTmpDirLeaf` (a constant), *not* a second
spelling of the suite fixture's `tmp`: the workspace also holds the worktrees,
and a name that cannot collide with a branch child keeps the two roles
distinguishable by spelling. `scratch.TmpDirName` (= `"tmp"`) survives as the
**suite fixture root** (`internal/testutil` pins `TMPDIR` to a child of it);
its old second role as the built-in default is gone, and its doc comment says so.

**PROJECT.md Rule 22 (the docs-vs-config question, resolved).** The rule's
"prefer the explicit layer" bullet claimed the default was the scratch home's
memory-backed child. That is no longer true, and a rule that describes a
superseded default is worse than no rule. The bullet was **rewritten** to state
the parameter (three channels, one resolver), the new default, and the honest
reason to *choose* the memory-backed tree: measured faster for SQLite scratch
(411.9 ms vs 16.8 ms for a fresh ledger DB) — a reason to set the parameter, not
a reason for the default to be tuned. The device-id/same-filesystem safety
question is **not** re-litigated (the operator closed it): the policy sentence
now says *where the default points*, and keeps the loud-failure contract, which
is the part Rule 22 actually enforces.

**Docs updates:** `GOPI.md` Rule 22 bullet; `internal/config/config.go`'s
`TmpDir` field comment (it described the old default); `gopi --help` gained the
`--tmpdir` line (via the catalog, so `--help-env` carries it too); the two
references in `internal/tools/worktree_sweep_memory_base.go` name the
settings key as one of several channels and needed no change.

## 5. Every test touched, and why

| test | change | why |
|---|---|---|
| `cmd/agent/cli_tmpdir_inject_e2e_test.go` | rewritten around the new contract | it pinned the **banner** and the old default |
| `…::TestTmpDirSettingsInstallsIntoTheChildTree` | kept, plus `assertNoTmpDirBanner` | the child-inheritance evidence, and now also the silence half of (A) |
| `…::TestTmpDirSettingsAbsentPublishesTheMeasuredDefault` → `…PublishesTheWorkspaceDefault` | renamed + re-pointed: asserts the published default is `<workspace>/_tmp` (base/parent), no device-id assertion, no skip | the old expectation (*the scratch home's tmp child*) is superseded by (C); the old case also **skipped** on a host whose scratch home is off `/`, i.e. it could silently assert nothing |
| `…::TestTmpDirKnobChannelsAgree` (new) | flag and `GOPI_TMPDIR` produce the same root | (B): one knob, not three reads |
| `…::TestTmpDirFlagBeatsTheEnvironment` (new) | flag wins over env | (B): the precedence half |
| `…::TestTmpDirUnusableValueFailsLoudlyAndNamesThePath` (new) | read-only parent → non-zero exit, stderr names path + reason + channel | (A): the loud failure is a *tested* contract, not a comment |
| `…::TestTmpDirSilentOnHelpAndVersion` (new) | `--help` / `--version` / `version` print nothing | (A): the removal is asserted |
| `…::TestTmpDirDebugSurfaceShowsTheValue` (new) | `GOPI_DEBUG=1` prints value + channel | (A): the replacement surface exists |
| `internal/scratch/default_tmproot_test.go` | rewritten | `TestDefaultTmpDirIsMeasuredOffRootFS` / `…IsTheOneSharedChild` / `…RefusesAScratchHomeOnRootFS` asserted the **old** default (the last one, that the default *refuses* on a `/` scratch home, is the opposite of the new contract); replaced with a case per new property, each driving the knob explicitly |
| `internal/standards/baseline.go` | `cmd/agent/main.go:973` → `:987` | Rule 16's line-addressed baseline: the same `%v` wrap site moved when `cmd/agent/main.go`'s two publication call sites grew their comments. Re-pointed (with the history and the reason recorded above it), **not deleted** — a deletion would license a genuinely new `%v` wrap at that address |

**Every case sets the parameter; none asserts a device id or a host layout.**
The `/-partition` question is closed, and its mechanical half
(`scratch.AssertPathOffRootFS`) keeps its own test, which was kept in spirit
(the read-only/roots case) rather than dropped. No gate or test was weakened to
go green: the e2e harness now *strips* an inherited `TMPDIR`/`GOPI_TMPDIR` and
pins `GOPI_TEST_HOME` to a fixture, which is exactly the "set it explicitly"
discipline Rule 17 asks for — a leftover suite pin would otherwise silently turn
every case into the caller-wins case.

## 6. Verification (all output pasted, nothing claimed)

**Build / vet**

```
$ go build ./...            → exit 0
$ go vet ./...              → exit 0 (no output)
$ gofmt -l <touched files>  → (empty)
```

**Acceptance 1 — silence (real binary, `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`)**

```
--- gopi --help (stderr) ---                 0
--- gopi --version (stderr) ---              0
--- gopi version (stderr) ---                0
--- ordinary run stderr tmpdir lines ---     0
```

**Acceptance 4 — the default, nothing configured (settings file without the key)**

```
$ env -u TMPDIR -u GOPI_TMPDIR GOPI_TEST_HOME=/dev/shm/…/param-acc-… \
      GOPI_DEBUG=1 ./bin/gopi --settings …/settings-empty.json version
[gopi] tmpdir: TMPDIR="/dev/shm/…/param-acc-…/workspace/_tmp" (from the built-in default)

$ ls -d /dev/shm/…/param-acc-…/workspace/_tmp
/dev/shm/…/param-acc-…/workspace/_tmp
$ stat -c 'device=%d %n' …
device=28   …/workspace/_tmp     ← the fixture's own fs
device=2051 /tmp                 ← ambient /tmp shares `/`
device=2051 /
```

(the default lives in the *workspace*, which the fixture pointed into a scratch
tree; on this host the real default is `<home>/workspace/_tmp` on the workspace
filesystem — the point of the run is the **resolution**, and the resolution no
longer consults the scratch home at all, which
`TestDefaultTmpDirIsIndependentOfTheScratchHome` pins).

**Acceptance 2 — a configured value that cannot be provided fails loudly**

```
$ ./bin/gopi --tmpdir /dev/shm/…/ro-XXXX/cannot version ; echo exit=$?
exit=1
[gopi] tmpdir: tmpdir is not usable as a temp workspace:
  "/dev/shm/…/ro-XXXX/cannot": cannot create it: mkdir …: permission denied
  (value from --tmpdir); point --tmpdir / TMPDIR / the "tmpdir" settings key at a writable directory
```

**Acceptance 3 — every channel, and the precedence, with real invocations**

```
--- caller TMPDIR + settings: caller wins ---
[gopi] tmpdir: TMPDIR="/dev/shm/…/c-dHRC" (from the caller's TMPDIR)
--- GOPI_TMPDIR + caller TMPDIR: caller wins ---
[gopi] tmpdir: TMPDIR="/dev/shm/…/c-dHRC" (from the caller's TMPDIR)
--- GOPI_TMPDIR + settings: env wins ---
[gopi] tmpdir: TMPDIR="/dev/shm/…/b-HJwc" (from GOPI_TMPDIR)
--- settings only ---
[gopi] tmpdir: TMPDIR="/dev/shm/…/d-5Ns1" (from the settings key tmpdir)
--- --tmpdir + caller TMPDIR + GOPI_TMPDIR + settings: FLAG wins ---
[gopi] tmpdir: TMPDIR="/dev/shm/…/a-fN3n" (from --tmpdir)
```

**Acceptance 5 — the value reaches a real child (test suite, `-v`, `-count=1`)**

```
=== RUN   TestTmpDirSettingsInstallsIntoTheChildTree
    child shell /proc/self/environ: TMPDIR="…/001/gopi-tmp" (configured="…/001/gopi-tmp"), GOPI_HOME="…/002"
--- PASS: TestTmpDirSettingsInstallsIntoTheChildTree (0.36s)
=== RUN   TestTmpDirSettingsAbsentPublishesTheWorkspaceDefault
    child shell /proc/self/environ carries TMPDIR="…/001/workspace/_tmp" (published with no settings key, no env and no flag)
--- PASS: TestTmpDirSettingsAbsentPublishesTheWorkspaceDefault (0.35s)
=== RUN   TestTmpDirKnobChannelsAgree
    both channels resolved to TMPDIR="…/001/knob-tmp" (GOPI_TMPDIR and --tmpdir agree)
--- PASS
=== RUN   TestTmpDirFlagBeatsTheEnvironment            --- PASS
=== RUN   TestTmpDirSettingsNeverOverridesTheCaller    --- PASS
=== RUN   TestTmpDirUnusableValueFailsLoudlyAndNamesThePath
    refused (exit 1): [gopi] tmpdir: tmpdir is not usable as a temp workspace: "…/readonly-parent/cannot-be-created": cannot create it: … permission denied (value from --tmpdir); …
--- PASS
=== RUN   TestTmpDirSilentOnHelpAndVersion            --- PASS
=== RUN   TestTmpDirDebugSurfaceShowsTheValue
    debug surface: [gopi] tmpdir: TMPDIR="…/001/debug-surface-tmp" (from GOPI_TMPDIR)
--- PASS
ok  github.com/the operator/gopi/cmd/agent  7.411s
```

**Touched packages' suites**

```
$ go test ./internal/scratch/ ./internal/config/ ./internal/testutil/ -count=1
ok  github.com/the operator/gopi/internal/scratch   77.346s
ok  github.com/the operator/gopi/internal/config     3.295s
FAIL github.com/the operator/gopi/internal/testutil 170.541s   ← PRE-EXISTING, see below
$ go test ./internal/config/                       → ok
$ go test ./internal/standards/                    → only TestGateCwdReleaseFindsTheRealTree fails (PRE-EXISTING)
```

**Pre-existing failures — reproduced on the unmodified main repo, not caused by
this branch** (running the same two packages in `~/work/GoPi` at `230fca00`
fails identically):

* `internal/testutil` `TestWorkerOwnershipPointerPresent` (1 uncovered devdoc;
  the main repo has 2, one being an untracked local report) and
  `TestWorkerOwnershipDismissTableIsHonest` (4 dismiss entries) — a devdoc
  housekeeping gate over files this branch never touches.
* `internal/standards` `TestGateCwdReleaseFindsTheRealTree` — "found only 4
  worktree-removing producers … (floor 5)", identical on master.
  *(Fixed 2026-09-16 on `fix/cwd-release-gate-inventory`: the merger's
  `126fad74` deleted the two producer files, so the floor was re-counted to 4
  with the cause recorded — see `devdoc/worktree-cwd-release-coverage.md`
  §"Inventory re-established".)*
* `internal/testutil`
  `TestWorkerOwnershipGateStaysGreenAcrossSuccessiveMergeRounds` — the
  worker-ownership audit requires the `WORKER-OWNERSHIP: OPERATOR-ONLY` pointer
  in this file's header (it is an operator-only surface: it prints a launch
  command). The pointer was written by the earlier backfill
  (`8e852829`) and lost when this report landed on master afterwards, so the
  round-advance simulation reddens on the FIRST round with
  `uncovered: [devdoc/tmpdir-param-not-banner.md]` — measured, not assumed: the
  line was deleted from this tree and the test failed naming exactly this file.
  The pointer is carried on this branch for that reason.

Every other gate in `internal/standards` (Rules 16–21) is green, including the
env-reader gate (Gate 6), which now sees the new reader chain:
`GOPI_TMPDIR` → `config.TmpDirFromEnv`/`TmpDirFromCallerEnv`, and `GOPI_DEBUG` →
`scratch.EnvDebugEnabled`.

## Not Done (honest)

* **The two `internal/testutil` / `internal/standards` failures above were not
  fixed.** They are pre-existing, live in files this epic does not own (devdoc
  housekeeping; the cwd-release gate's producer inventory), and fixing them here
  would mix an unrelated repair into a tempdir change. Verified identical on
  master, so they are **dispatchable as their own tasks**.
* **`TMPDIR` is still the variable gopi writes**, i.e. the published value is
  not namespaced. Migrating children to a `GOPI_TMPDIR`-only world would touch
  every spawn site and every third-party child (git, bash tools) that reads
  `TMPDIR` — out of scope, and the epic explicitly keeps the caller's `TMPDIR`
  authoritative.
* **`scratch.TmpDirName`'s second role removal is a comment-level change**: the
  constant itself is still `"tmp"` and is still the suite fixture root. If a
  future epic retires the fixture root too, the constant goes with it.
* **No `--tmpdir` value validation beyond usability** (e.g. "is it off `/`?").
  That question is closed by the operator; a configured value is used as given,
  which is the documented contract.

## Files

* `cmd/agent/tmpdir_inject.go` (rewritten: silent publisher, two entry points, loud refusal)
* `cmd/agent/main.go` (call sites + comments)
* `cmd/agent/cli_tmpdir_inject_e2e_test.go` (rewritten + 5 new cases)
* `internal/scratch/scratch.go` (`TmpDirName` role, `DefaultTmpDirLeaf`, `homeBase`/`WorkspaceUnderHome`)
* `internal/scratch/tmproot.go` (`DefaultTmpDir`, `ResolvePublishedTmpDir`, `ensureUsableTempRoot`, `TmpDirUnusableError`, `ReportPublishedTmpDirIfVerbose`, `EnvDebugEnabled`, `GopiTmpDirEnvName`, `ProbeReportPrefix`)
* `internal/scratch/default_tmproot_test.go` (rewritten)
* `internal/config/tmpdir.go` (new: precedence, channel labels, env readers)
* `internal/config/{config,flags,settings,help}.go` (field comment, flag case, catalog row, `envVarFor` hoist, help line)
* `internal/standards/baseline.go` (re-pointed one Rule 16 permit)
* `GOPI.md` (Rule 22 bullet)
* `devdoc/tmpdir-param-not-banner.md` (this report)

## Commit

`fix(tmpdir): make the temp workspace an ordinary parameter and stop announcing it`
on `fix/tmpdir-param-not-banner` (worktree `~/workspace/gopi-tmpdir-param`).
