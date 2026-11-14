<!-- doc-class: historical -->
# compute_nodes: the dead knob is DELETED, not wired — and the per-start essay is gone

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a160a2-2cda-7c1e-8fa4-b925ba69bc1d`. Branch `task/01a160a2-2cda-7c1e-8fa4-b925ba69bc1d`.

This document records a DECISION and the measurement it rests on. It is the
follow-up to `devdoc/compute-nodes-unwired-20260930.md` (which MARKED the knob)
and `devdoc/compute-nodes-report-trigger.md` (which narrowed the report's trigger
to the worker face). Both of those documents describe a state that no longer
exists — the knob is gone.

## 1. The decision

**DELETE `compute_nodes`, its resolver, its three sources, its `--nodes`/`--nodes-only`
spellings, its stderr essay, and `cmd/agent/dispatch.go` — the dangling dispatcher
it fed.**

The rejected alternative was to WIRE it: give `runDispatchOnce` a caller so the
whitelist finally reaches a consumer. That alternative is refused by Rule 35.
The only caller that ever existed was a resident serve daemon (one dispatch pass
per tick, SSH fan-out to nodes, ledger db-push per handoff). Re-creating it means
adding a resident face to this machine — exactly the class of dependency Rule 35
requires an operator to authorise (what breaks without it, why it cannot be
in-process, who reclaims it, what the next reader sees), and the retired-verb
history of `gopi task serve` is the record of the last time that face existed.

**A knob with no consumer is a defect (Rule 21/13). When the consumer is a
resident face the operator has not asked for, the honest repair is removal, not
resurrection** — and it is a strictly smaller change than wiring, with no
machinery left behind.

The second half of the epic — stop printing the ~400-char "NO CONSUMER" essay on
every worker start — falls out of the same edit. The essay was honest and still
wrong: it asked the operator to act on a decision that was not theirs to make at
a worker start, on the one face they start dozens of times a day.

## 2. The three facts, measured on the pre-change tree

Captured verbatim before any editing (working tree at `0516e14`, clean):

```
$ grep -rn "runDispatchOnce(" --include=*.go . | grep -v "func runDispatchOnce("
--- exit=1 (1 = zero call sites)
```

Every other occurrence of the name is prose: four comments in `cli_tasks.go`,
one in `main.go`, three in the deleted report file, and the sentence constant.
**Zero call sites, not even in a test.**

`gopi task serve` is a retired-verb refusal — `retireMergerFace` runs at the
alias dispatch before any parser:

```
$ /tmp/gopi-01a160a2-PREFIX task serve --nodes '[]'
serve: retired — the resident merger daemon was retired for re-development: …
```

and `gopi task run --nodes` went with that verb, so the parse arm that filled
`opts.nodes` was unreachable too.

## 3. What was deleted, in one commit

| surface | before | after |
|---|---|---|
| `internal/config/config.go` | `ComputeNode` struct, `ComputeNodesEnv`, `ComputeNodesSettingsKey`, `ComputeNodesSectionStatus`, `ComputeNodesNoConsumer`, `ResolveComputeNodes`, `ParseComputeNodesJSON`, `normalizeComputeNodes`, the capacity bounds, the `Config.ComputeNodes` field | gone |
| `internal/config/settings.go` | `GOPI_COMPUTE_NODES` catalog row | gone |
| `internal/config/help.go` | the whole `Remote dispatch (declared — NO CONSUMER)` section | gone |
| `cmd/agent/cli_tasks.go` | `taskNodesFlagName`/`taskNodesOnlyFlagName`, the `--nodes` refusal arm, the `taskFlagMember` admission, `parseNodesFlag`, `opts.nodes`/`opts.nodesOnly` | gone |
| `cmd/agent/dispatch.go` (478 lines) | the whole M4.7 remote dispatcher | gone |
| `cmd/agent/compute_nodes_unwired.go` | the essay + `isWorkerFaceArgv` + the dev_repo notice | **renamed** to `worker_start_notices.go`, essay gone, `isWorkerFaceArgv` and `reportDevRepoEmptyDeprecation` kept |
| `cmd/agent/main.go` worker face | two notices | one (`reportDevRepoEmptyDeprecation`) |
| `CONFIG.md` | the `compute_nodes` catalogue row + 2 prose references | row gone, references corrected |

**What deliberately survives, and why.**

- **`isWorkerFaceArgv`** — the `-h` exclusion predicate is not about
  compute_nodes; the empty-dev_repo notice still needs it, and it is the pin that
  keeps `gopi worker -h` a zero-side-effect path (P0-1). Deleting it would have
  meant re-inventing it for the surviving notice.
- **`reportDevRepoEmptyDeprecation`** — a live notice about a different subject
  that happened to live in the same file. It moved, unchanged in behaviour.
- **`config.DevRepoEmptyNoticeLine`** and its tests — untouched.

**A stale `compute_nodes` key in settings.json is now simply an unknown key.**
The settings reader ignores keys the typed `Config` does not model, so it starts
nothing, refuses nothing and prints nothing. Verified below.

## 4. Verification (red → green, same settings document, two binaries)

Both binaries were built from real trees: the PRE binary from `git archive HEAD`
into a scratch directory, the POST binary from this branch.

Settings fixture (a real host's shape — the goal carried it):

```json
{ "api_key": "sk-not-a-real-key", "model": "fixture-model",
  "compute_nodes": [ { "name": "peerbox", "max_concurrent": 4,
                       "report_to": "peerbox", "repo": "/home/user/work/GoPi" } ] }
```

`gopi worker --status`, stderr captured separately:

| binary | stderr bytes | first line |
|---|---|---|
| PRE (HEAD) | **409** | `compute_nodes: 1 node(s) declared (peerbox) — NO CONSUMER: …` |
| POST (this branch) | **0** | *(nothing)* |

And the named-row face, `worker <bogus-row-id>` (bounded, nothing claimed):

```
[gopi] worker: agent-spawn guard bypassed (…) — the operator ruling of 2026-09-12 forbids this from an agent
[worker] display preflight: DISPLAY=":0" reachable …
worker: running 01a1dead-0000-7000-8000-000000000000 (explicit)
… gopi task: workdir → <scratch>/sessions/01a1dead-…
```

No `compute_nodes` line anywhere. `strings <post binary> | grep -c "NO CONSUMER"`
returns **0** — the essay is not merely unreached, it is not in the binary.

`gopi --help-env`: no `GOPI_COMPUTE_NODES` row and no `Remote dispatch` group.
`gopi config show` with the stale key present: exit 0, normal output.

## 5. Tests that pinned the old state, and what they assert now

| test | was | now |
|---|---|---|
| `cmd/agent/compute_nodes_unwired_test.go` | 5 tests over the essay, its call site, its trigger | **deleted with its subject** |
| `internal/config/compute_nodes_unwired_test.go` | pinned the shared sentence | deleted |
| `internal/config/cov_config_compute_nodes_test.go` | 300+ lines over the resolver | deleted |
| `cmd/agent/dispatch_test.go` (484 lines) | the dispatcher's load-balance / db-push / rollback | deleted with the code |
| `TestDispatchRefusesOptionShapedNode` | option-injection on dispatch node names | deleted (no dispatch node names exist) |
| `TestFlagOverridesEnvNameNoFlagInCatalog` | used `GOPI_COMPUTE_NODES` as its "real knob, no CLI flag" fixture | **re-pointed to `GOPI_SYNC_PEERS`** — a live catalog row with an empty `CLIFlag` |
| `settings_write_guard_test.go` | `compute_nodes` as a measured loss casualty | **re-pointed to `memory_peers`**, with the premise re-derived: the fixture survives only because `omitempty` cannot round-trip a zero value, not because the key is unmodelled |
| `internal/standards/config_doc_flag_gate_test.go` | `configDocFlagRowCount = 75` | `74`, with the audit-trail comment |
| `internal/testutil/worker_ownership_gate_test.go` | anchored `cmd/agent/dispatch.go:dispatchRemoteCommand` | empty map + the reason (the walk stays, so a re-introduction is still caught) |

**The doc gate earned its keep here**: deleting code from `internal/config/config.go`
shifted four `CONFIG.md` anchors and the gate named every one
(`ResolveRepoBuild`, `ResolveMergerRepos`, `RepoConfig`, `ResolveNotify`,
`PublishScratchGCStaleMin`, `BootstrapEnvFromArgs`, `ResolveSettingsPath`). Each
was re-pointed at its declaration's real line in the same commit.

## 6. Not Done (honest)

- **No consumer was wired, by design.** If a remote-dispatch layer is ever wanted
  again, it is a Rule 35 decision for the operator: a one-shot face
  (`gopi task dispatch --once`) would avoid a resident daemon entirely and is the
  cheaper door. Nothing here forbids that; this change removes an unreachable
  dispatcher, it does not argue against dispatch.
- **The default settings path still resolves to the operator's real file** when
  neither `GOPI_SETTINGS` nor a fixture pins it (`config show` reports
  `/home/user/.gopi/settings.json`). That is existing behaviour, unrelated to this
  change, and it is why every invocation above pinned `GOPI_HOME` and, for the
  settings check, `GOPI_SETTINGS` explicitly.
- **`internal/testutil`'s `TestWorkerOwnershipDismissTableIsHonest` fails on this
  tree**, naming three devdoc files this change never touched. Verified
  pre-existing at HEAD (`devdoc/tasks/merge-serve-worker-split-epic2-20260903.md`
  does not exist in `HEAD` either). It is outside this task's scope and is not
  repaired here.
