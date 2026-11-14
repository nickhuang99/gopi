<!-- doc-class: historical -->
# Task Report: the compute-node whitelist is declared on three surfaces and read by nobody — Rule 21 fixed by MARKING, not by re-wiring

> **SUPERSEDED 2026-10-22 (task `01a160a2`)**: the `compute_nodes` knob, its
> resolver, its three sources and `cmd/agent/dispatch.go` were DELETED — see
> `devdoc/compute-nodes-knob-deleted-20261022.md`. This page is the record of the
> state it was written in (a knob marked, not wired); it describes no live code.

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY -->
<!-- the ONE statement of why is a constant: config.ComputeNodesNoConsumer -->
<!-- standing claim: every ad surface of this knob now names its own unwired status -->

Branch `task/01a0ed9f`, worktree
`/home/user/workspace/gopi-01a0ed9f2ce775589440ed68878dedb3`.

## 1. The defect, measured

`GOPI_COMPUTE_NODES` / the settings key `compute_nodes` / the `--nodes` flag are
advertised on three operator-facing surfaces. The resolved whitelist had **zero
production consumers**:

- `cmd/agent/dispatch.go:446` — `runDispatchOnce` — is called by **nothing**
  (not by tests either). The only entry point it ever had is the resident
  dispatcher reached through `gopi task serve`.
- `gopi task serve` is a **retired-daemon refusal**: `cli_tasks.go` reaches
  `retireMergerFace(verb)` *before* any parser runs, so the one arm that ever
  filled `opts.nodes` / `opts.nodesOnly` (`parseTasksArgs`'s `Serve, Merger`
  case) is unreachable.
- `gopi task run --nodes` was removed with the `run` verb. `taskFlagMember`
  still *admits* the two spellings, but the `Run` parse arm never reads them.

**Red proof** (pre-change binary `/tmp/gopi-prefix-01a0ed9f`), with
`GOPI_COMPUTE_NODES='[{"name":"hp","max_concurrent":2}]'`:

| invocation | declared | undeclared |
|---|---|---|
| `gopi -h` | sha256 `dff94bb0…` | sha256 `dff94bb0…` |
| `gopi --help-env` | sha256 `fb6b7ad8…` | sha256 `fb6b7ad8…` |

**Byte-identical**, and stderr was empty in all four runs — including for a
**malformed** declaration (exit 0, no message). The declaration reached nothing
and said nothing. The single observable effect of the knob was a side effect on
the startup banner's role line.

## 2. What was fixed, and what was deliberately NOT

**Not re-wired.** Re-attaching `runDispatchOnce` to a live caller is a separate
epic (it needs a resident face, which Rule 35 governs). This task fixes **the
Rule 21 defect** — an advertised knob that reaches nothing — by making the state
true on every surface that advertises it. Rule 26's answer for the remainder is
a new dispatched task, not a silent half-measure here; §4 names the remainder
honestly rather than claiming coverage it does not have.

### 2.1 Surface A — `gopi --help-env` (`internal/config/help.go`)

The group heading is now
`Remote dispatch (declared — NO CONSUMER)`, using the constant
`config.ComputeNodesSectionStatus`. A heading that reads `Remote dispatch`
alone tells an operator the subsystem is live; the status belongs in the
heading because scanning is what this table is for.

The row's prose is `config.ComputeNodesNoConsumer` — the full sentence naming
`runDispatchOnce`, the retired `gopi task serve`, and the removed
`gopi task run --nodes` arm.

### 2.2 Surface B — the settings catalogue row (`internal/config/settings.go`)

The `EnvVars` row for `GOPI_COMPUTE_NODES` now ends with
`config.ComputeNodesNoConsumer`, and its `settings key` name is rendered from
`ComputeNodesSettingsKey` instead of a re-typed literal (Rule 1b/13 — the row
and the resolver must not drift).

### 2.3 Surface C — the `--nodes` / `--nodes-only` flag arm
(`cmd/agent/cli_tasks.go`)

The two spellings are **refused by name** in the parse arm:

```
--nodes: removed — NO CONSUMER: the remote-dispatch pass that read this
whitelist (cmd/agent/dispatch.go: runDispatchOnce) is called by nothing …
```

Three decisions worth recording:

- **The spellings stay in `taskFlagMember` ON PURPOSE.** Removing them there
  would produce `unknown flag: --nodes` — which reads as a *typo* and sends the
  reader hunting a spelling the binary never had. The admission table keeps
  them so the refusal can *explain the removal*. This is the same reasoning that
  keeps the `Run` case's arm.
- **The fields `opts.nodes` / `opts.nodesOnly` are kept, commented as unwired.**
  `runDispatchOnce` still compiles, so the day it gets a caller the code is not
  resurrected from git archaeology. They simply have no CLI feeder any more.
- **Rule 15 at the boundary:** the arm now *refuses* a flag it used to *accept
  and ignore*. Accepting-and-ignoring was the defect.

**Honest scope of that refusal (measured, not assumed).** On the real CLI the
`--nodes` spelling is *intercepted above the parser*: `gopi task serve --nodes
…` reaches `retireMergerFace` (`cmd/agent/cli_tasks.go:5850`) and exits with the
retired-daemon sentence before `parseTasksArgs` ever runs, and `gopi task run
--nodes …` reaches the removed-verb sentence at `cli_tasks.go:5703` — both
verified on the post-change binary (exit 0, neither prints anything about
`--nodes`). So the parse-arm refusal is **defence in depth for the day the
serve/merger face comes back** (the arm is unreachable *because* those two
verbs are retired — that is exactly the disease being fixed), and it is pinned
by `TestComputeNodesFlagArmIsRefusedByName`, which drives `parseTasksArgs`
directly. What an operator sees *today* is the retirement sentence, which is
already the correct answer to "why does my `--nodes` do nothing" — and the
new report line, which is the surface that speaks about the knob itself.

### 2.4 The ONE reporting site (`cmd/agent/compute_nodes_unwired.go`)

`reportComputeNodesWithoutConsumer()` is called from `tmpDirBootstrapFromArgs`
(the pre-chain moment every verb passes through), beside the existing
`reportRetiredMergeTickKnobs` — same pattern: **report, do not refuse**. A stale
declaration written before the retirement is not a reason to take the operator's
command down.

- `config.ResolveComputeNodes(nil)` — the **same** resolver a re-wired consumer
  would call (Rule 2: one resolver per knob, so the report cannot disagree with
  the dispatch layer).
- Non-empty whitelist → **exactly one** stderr line naming the node(s):
  `compute_nodes: 1 node(s) declared (hp) — NO CONSUMER: …`
- Nothing declared → **silent** (the ordinary single-machine start).
- Malformed declaration → the resolver's error, reported **by name**, exit 0 —
  the knob reaches nothing, so refusing the command for a value nobody reads
  would be the wrong loudness.

The sentence is `config.ComputeNodesNoConsumer`, rendered by all three surfaces
— one statement, so they cannot drift into three stories (Rule 1b).

### 2.5 `CONFIG.md` (same commit — Rule 12's doc gate)

The `compute_nodes` row carries the **DECLARED, NOT WIRED (2026-09-30)** note
and cites §3 of this file.

## 3. Green proof (post-change binary `/tmp/gopi-post-01a0ed9f`)

Same inputs as the red run:

| check | declared | undeclared |
|---|---|---|
| `gopi --help-env` heading | `Remote dispatch (declared — NO CONSUMER)` | same (it is the help text) |
| `gopi --help-env` row | carries `ComputeNodesNoConsumer` | same |
| stderr | `compute_nodes: 1 node(s) declared (hp) — NO CONSUMER: …` | *empty* |
| `gopi -h` | sha256 `dff94bb0…` (**unchanged**) | — |

`gopi -h`'s bytes are unchanged on purpose: the everyday index prints section
names, and the `--help-env` table is where a variable's own row is read
(`gopi --help-env` is the documented door). Marking the section *title* is what
reaches the `-h` reader, and that is `ComputeNodesSectionStatus` — verified in
`gopi --help-env`'s rendering of the same `Sections` value.

Settings channel: a `compute_nodes` key in `settings.json` prints the same line
with the channel named (`set-node`). Malformed env: reported by name, exit 0.

`--nodes` / `--nodes-only`: refused with the shared reason (test-pinned).

## 4. Not Done (honest)

- **`runDispatchOnce` is still unreachable.** This task marks the knob; it does
  not restore a dispatcher. Whether a remote-dispatch layer should come back is a
  Rule 35 question (does it need a resident face, or can it be a one-shot?) and
  belongs to the operator, not to a marking task. The knob now says so on every
  surface instead of lying by omission.
- **`cmd/agent/dispatch.go` is untouched.** Per the epic's constraint it was not
  deleted wholesale; its `runDispatchOnce` remains the documented (dangling)
  consumer, referenced by name from `ComputeNodesNoConsumer`.
- **No test drives the real binary end to end** (the `Ran:` evidence in the
  report is a manual invocation, recorded verbatim). In-process tests pin the
  help text, the resolver report, and the refusal; the binary-level red/green
  pairing was done by hand and its hashes are recorded here.

## 5. Verification

See the report at `GOPI_TASK_REPORT_PATH` for the `Build:` / `Tests:` / `Ran:` /
`Result:` block. Gate-claim evidence: `go build ./...` exit 0; `go vet
./cmd/agent/ ./internal/config/` exit 0; `go test ./internal/config/ ./cmd/agent/
-run 'ComputeNodes|Help|Env'` ok; `go test ./internal/standards/ -run
'TestConfigDocGate|TestUnknownNode'` ok; `gopi doccheck` → "the devdoc corpus is
consistent".
