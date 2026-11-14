<!-- doc-class: historical -->
# Task Report: internal/config function-level coverage 81.2% → 100%

Branch: `feat/cov-config-hp` · worktree `/home/user/workspace/gopi-cov-config-hp`
Charter: **tests only** — no product code was modified (all 5 files are new
`cov_config_*_test.go`; `git status` shows only untracked test files).

## Completed

Closed the 6 uncovered exported functions of `internal/config`
(26/32 → **32/32, 100.0%**, 0 uncovered) with 36 new top-level tests
(including the whole-file table subtests: 150 PASS lines in `-v`):

| Function | File | Contract asserted |
|---|---|---|
| `EnvBool` | `cov_config_envbool_test.go` | full accepted/rejected value matrix, unset/empty-name lookup, per-call re-read, canonical `"off"` written by `ApplyEnv` reads FALSE |
| `ResolveSettingsPath` | `cov_config_settings_path_test.go` | env override verbatim/trimmed, whitespace-only = unset, default follows `GOPI_HOME`, stale default not consulted (end-to-end through `FromSettingsFile`), no-home → `""`, no filesystem side effects |
| `ResolveReasoningEffort` | `cov_config_effort_test.go` | 22-case matrix via the EXPORTED wrapper, "returned value is always on the effective whitelist", default constants stable, settings.json end-to-end path |
| `ResolveComputeNodes` | `cov_config_compute_nodes_test.go` | CLI > env > settings precedence, malformed env is an ERROR (never a silent fallback), whitespace env = unset, missing settings file fails soft / malformed fails hard, no source → `(nil, nil)`, CLI validated+trimmed |
| `ParseComputeNodesJSON` | `cov_config_compute_nodes_test.go` | 7 valid shapes (incl. `[]`/`null`/trimming), 14 malformed shapes, 12 field-validation cases (empty/duplicate name, missing-field zero, ±1 capacity bounds, extra field tolerated) |
| `ResetNodeIdentityForTest` | `cov_config_reset_identity_test.go` | cache actually cleared (role half, sync_to half, settings re-read), precedence preserved after reset, idempotent/no-cache safe, undeclared identity never memoised |

### EnvBool value matrix (explicitly stated, as required)

Truthy-ONLY parsing of `ToLower(TrimSpace(value))`:

- **ACCEPTED → true (11 spellings):** `1`, `on`, `yes`, `true`, `enabled`,
  `TRUE`, `On`, `  yes  `, `Enabled`, `1 `, `\ttrue`
- **REJECTED → false (18 values):** `""`, `" "`, `0`, `off`, `false`, `no`,
  `disabled`, `OFF`, `False`, `NO`, `banana`, `2`, `true1`, `enable`, `y`,
  `t`, `yes!`, `on the way`

Cross-function anchor: `ApplyEnv(&Config{}, true)` force-writes
`GOPI_DEBUG=off` (canonical "off" for a disabled switch) and `EnvBool`
must read that as FALSE — the 2026-08-29 debug-dump / 2026-08-30 retry-log
regression the function's doc comment cites; asserted in
`TestCovCfgEnvBool_CanonicalOffFromApplyEnv`.

### Boundary and error cases (named)

- Boundaries: `computeNodesMinConcurrent` / `computeNodesMaxConcurrent`
  accepted exactly AT the bound; `bound-1` / `bound+1` rejected with the
  `out of range` message (constants referenced, never re-typed literals).
- Error paths: 14 malformed-JSON shapes, malformed settings file (error
  names `ComputeNodesSettingsKey`), malformed env (error names
  `ComputeNodesEnv` + the reason), duplicate/empty compute-node names,
  duplicate CLI names, `max_concurrent` missing-field zero.
- Degenerate: `[]` / `null` / whitespace JSON, absent vs empty
  `compute_nodes` section, missing settings file (soft `nil,nil`),
  missing home directory (`""`), unset/blank env vars.
- Unset semantics: `""` and whitespace-only env values are NOT overrides
  (trimmed-then-checked) for both `GOPI_SETTINGS` and `GOPI_COMPUTE_NODES`.

## Not Done / deviations from the briefing

1. **"symlink resolution" for `ResolveSettingsPath` was not tested — because it
   does not exist.** `grep -rn "EvalSymlinks\|Symlink" internal/config/*.go`
   returns nothing: the function is pure textual resolution
   (`GOPI_SETTINGS` → `DefaultSettingsPath()`), no canonicalization anywhere.
   Asserting a symlink/canonicalize contract would have asserted
   non-existent behavior (memory #1695's lesson). I pinned the REAL contract
   instead, including the symlink-adjacent case that actually matters
   operationally: a **stale default** `$GOPI_HOME/.gopi/settings.json` must not
   leak into a run launched with an explicit override (verified end-to-end:
   the override's `role: client` is read, the stale file's `role: server` /
   `sync_to: stale-node` never appear). The briefing's briefing-vs-source
   mismatch is recorded here rather than papered over.
2. `ResolveReasoningEffort`'s unexported engine already has a table in
   `settings_effort_test.go`; the new file pins the EXPORTED wrapper contract
   (plus a "result is always whitelist-member" invariant the old table did not
   state). No existing test was modified.
3. **No bug tickets filed.** Nothing counter-contractual surfaced in this
   package (unlike the internal/tools slices). One observation, filed as a
   recorded finding rather than a bug: with both `GOPI_HOME` and `HOME` unset,
   `ResolveSettingsPath()` returns `""` and `FromSettingsFile("")` returns an
   EMPTY config with a nil error — every settings-driven knob (role,
   compute_nodes, notify) silently degrades to its default. This is the
   DOCUMENTED behavior (a missing settings file is not an error), reachable
   only in a sandbox/container with no HOME; asserted as
   `TestCovCfgResolveSettingsPath_NoHomeDirectoryIsEmpty` so the consequence is
   at least visible. Not filed as a bug because it is by-design and
   tests-only charter forbids changing it.

## Verification

Test-helper prefix is **`covCfg*`** (task-unique, Rule 14 / memory #1694):
`covCfgProbeEnv`, `covCfgAcceptedEnvBool`, `covCfgRejectedEnvBool`,
`covCfgSnapshotEnv`, `covCfgEffortCases`, `covCfgComputeSettings`,
`covCfgPinComputeSandbox`, `covCfgNodeSandbox`. Checked before writing:
`git worktree list` shows no sibling coverage slice of `internal/config`
(the 5 worktrees are hippo-bind / non200-probe / screen-act / verdict-unknown
/ webcam-capture — none has `*_test.go` under `internal/config`), and existing
package helpers are `writeSettings` / `writeTemp` / `writeNodeSettings` /
`writeReposSettings` / `clearGraceEnv` / `clearNotifyEnv` / `contains` /
`indexOf` / `repoRoot` — zero overlap with `covCfg*`.

Isolation: every filesystem fixture lives under `t.TempDir()`; every env
mutation uses `t.Setenv`; `covCfgSnapshotEnv` snapshots+restores the WHOLE
process env around `ApplyEnv(..., true)` (it force-writes every scalar knob —
including `"0"` for unset ints — so the new values would otherwise leak into
sibling tests). No network, no sleeps, no `~/.gopi` access.
`make fmt` was NOT run (forbidden); `gofmt -l internal/config/` prints nothing.

```
$ gofmt -l internal/config/
(no output)

$ go test -count=1 ./internal/config/          # RUN A
ok  	github.com/nick/gopi/internal/config	0.242s

$ go test -count=1 ./internal/config/          # RUN B
ok  	github.com/nick/gopi/internal/config	0.217s

$ go build ./...
(no output, exit 0)

$ go test -count=1 -v -run '^TestCovCfg' ./internal/config/ | grep -c '^--- PASS'
36
$ go test -count=1 -v -run '^TestCovCfg' ./internal/config/ | grep -c 'PASS'
150                       # 36 top-level + 114 subtests

$ python3 scripts/func_coverage.py --uncovered --pkg internal/config
package                                     funcs    cov    pct  tests
--------------------------------------------------------------------------
internal/config                                32     32 100.0%
--------------------------------------------------------------------------
TOTAL                                          32     32 100.0%

Uncovered exported funcs (by package):

NOTE: conservative metric — a func counts as covered only if its name
appears in a *_test.go file of the same package. Target: 100%.
```

### Metric honesty check (call-sites, not mentions)

The metric is name-appearance; each function is genuinely EXERCISED. Call-site
(`Fn(`) vs bare-mention (`Fn`) counts inside the new files:

```
EnvBool                    calls=20 mentions=25
ResolveSettingsPath        calls=21 mentions=23
ResolveReasoningEffort     calls=11 mentions=12
ResolveComputeNodes        calls=16 mentions=18
ParseComputeNodesJSON      calls=8  mentions=10
ResetNodeIdentityForTest   calls=9  mentions=13
```

## code_search evidence (definitions located before writing)

```
ResolveSettingsPath     internal/config/config.go:555  func
ParseComputeNodesJSON   internal/config/config.go:565  func
ResolveComputeNodes     internal/config/config.go:578  func
```

Plus (read, not guessed): `EnvBool` env.go:22, `ResetNodeIdentityForTest`
node_role.go (tail), `ComputeNodesEnv` config.go:421,
`ComputeNodesSettingsKey` config.go:425, `computeNodesMinConcurrent/MaxConcurrent`
config.go:546-547, `SettingsEnv` settings.go:12, `RoleEnv`/`SyncToEnv`
node_role.go, `defaultEffortValues`/`defaultReasoningEffort`/`effortOff`
settings.go (referenced, never re-typed).

## Rule 1b/13 constant discipline

Referenced exported/internal constants instead of bare literals:
`SettingsEnv`, `ComputeNodesEnv`, `ComputeNodesSettingsKey`, `GopiDirEnv`,
`RoleEnv`, `SyncToEnv`, `NodeRoleServer`, `NodeRoleClient`, `effortOff`,
`defaultReasoningEffort`, `defaultEffortValues`,
`computeNodesMinConcurrent`, `computeNodesMaxConcurrent`.
The only re-typed strings are (a) the test-only probe env NAME
(`covCfgProbeEnv` const) and (b) JSON fixture documents, which must be literal
to exercise the decoder.

## Deliverable declaration (Rule 6d)

- Branch: **`feat/cov-config-hp`** (worktree `/home/user/workspace/gopi-cov-config-hp`)
- Recovery: `git show feat/cov-config-hp:devdoc/cov-config-hp.md`
  and `git show feat/cov-config-hp:internal/config/cov_config_envbool_test.go`
- Commit hash: see the commit on `feat/cov-config-hp` (worktree shares the
  main repo's refs — the commit is already in `/home/user/work/GoPi`).
