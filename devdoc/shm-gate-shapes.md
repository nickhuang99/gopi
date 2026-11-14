<!-- doc-class: normative -->
# The /dev/shm code sites, and the worktree-root gate's two topologies

Task: `feat/shm-gate-shapes` (worktree `~/workspace/gopi-shm-gate`, base `45c6ecc1`).
Gate: `internal/standards/worktree_root_gate.go` (Rule 13 /
checklist 1b/worktree-root). Devdoc written in the same change (Rule 7).

## 1. What was wrong

`/dev/shm` appeared on 22 non-comment code lines. A constant
(`worktreeroots.ShmDir`) and the gate that polices it already existed, but the
gate reached only a subset of the shapes:

1. it scanned **production files only** (`wantTest=false`, `prodRoots`), so
   every `_test.go` site was outside its reach;
2. its `/dev/shm` pattern was anchored `^/dev/shm/.` — **an entry BELOW the
   directory** — so a bare `"/dev/shm"` went unreported.

Both misses are the same defect shape: a rule whose footprint did not match the
rule's own description.

## 2. The classification rule (and the boundary the gate now states)

Two topologies, two rules — this is the section quoted at the top of the gate
file, so a site can be classified without reading the predicate:

| topology | rule | why not the same rule |
|---|---|---|
| **production** (non-test `.go` under `internal/` + `cmd/`) | **reference, not literal** — any hardcoded worktree root (`/dev/shm…`, `/tmp…`, home-rooted `…/workspace/…`) must come from `worktreeroots`, or the site must carry `// worktree-root-ok:` with its reason | a production literal is a LOCATION a program acts on; the chosen name below the root is exactly what drifts |
| **fixtures** (`_test.go` under `internal/`, `cmd/`, `testsuite/`) | **no bare host root** — the bare `"/dev/shm"` / `"/tmp"` must be a declared value in that file (named fixture) or carry `// fixture-path-ok:` | a fixture's point is very often a path OUTSIDE the declared roots: deriving it from `worktreeroots.ShmDir` would make it a path the policy under test may authorise — **the fixture's meaning would invert** |

Deliberate asymmetries, each measured rather than assumed:

- **Why the bare root counts in production.** The original anchor ("the bare
  directory is a system fact, the path below it is a choice") held only for the
  definition site. The measured tree shows the opposite elsewhere: a fixture
  naming the directory ITSELF (`worktree_sweep_finding_root_gate_test.go`,
  `worktree_sweep_memory_base_test.go`) and a production value that IS the
  definition. The bare literal is now reported in both topologies; the
  definition package is exempt because it IS the definition (exemption by
  DIRECTORY, not by shape).
- **Why the fixture anchor is stricter** (no entry below the root). In a fixture
  the string below the root is the FINGERPRINT — the distinguishing leaf a test
  reads back (`"/tmp/x/wt"` is a worktree, `"/tmp/serve.lock"` a lock file) —
  and that spelling is the fixture's own data, not a root choice. Held to the
  production rule, a fixture that writes a fake deliverable
  (`{"worktree":"/tmp/x/wt"}`) would be pushed to reference `worktreeroots`,
  which is exactly the inversion above.
- **Named fixture is classified BY POSITION, never by identifier.** A literal is
  exempt when some `const`/`var` in that file *declares* it — a property of the
  AST (`lit` inside a `ValueSpec`), not of the identifier a reader sees. A
  reference to `worktreeroots.ShmDir` is a different token and is allowed for a
  different reason (there is no literal to report at all). Writes are not
  declarations: `d = "/tmp/probe-" + pid` stays reported.
- **The derivation rule was REMOVED after measuring it.** A first draft also
  reported a host path *derived* at run time (`filepath.Join(os.TempDir(), "/x")`,
  `os.MkdirTemp(os.TempDir(), "/probe-")`). Measured on the tree: **29 hits,
  every one a false positive** (env-key spellings like `"HOME"`/`"sessions"`
  inside `os.Getenv`/`filepath.Join`, `os.MkdirTemp(dir, "prefix-")` name
  patterns). Zero true positives. "Is this literal a NAME for a path or a second
  way of WRITING one" is not decidable here, and a noisy gate gets switched off —
  so the shape is left out and the decision is written into the boundary note.

**Comments are out of scope** (the 91 `/dev/shm` comment mentions are NOT
rewritten). The predicate walks `ast.BasicLit` string literals only — what a
program can ACT on — so prose is structurally invisible to it, and a text scan
that punished the explanation would be the wrong instrument.

## 3. All 22 code sites, classified

Class codes: **DEF** = definition/definition-adjacent · **PROD-TEXT** =
production string that is prose/message, not a path · **GATE** = the gate's own
pattern or proof · **FIX-BARE** = fixed (was a bare root) · **FIX-REF** = fixed
to a reference · **FP-LEAF** = fingerprint/below-root path in a fixture (silent
by design) · **ELSEWHERE** = a path genuinely outside the /dev/shm rule.

| # | site | class | action |
|---|---|---|---|
| 1 | `internal/worktreeroots/roots.go:63` | DEF | the ONE definition (`ShmDir = "/dev/shm"`) — untouched |
| 2 | `internal/worktreeroots/roots_test.go:208` | DEF | in-package value pin (`ShmDir != "/dev/shm"`) — exempt with its directory (the definition's own tests must spell the value) |
| 3 | `internal/standards/worktree_root_gate.go:76` (now :164/:187) | GATE | the gate's own `^/dev/shm(/|$)` pattern — self-scan is green because the pattern value is anchor-prefixed; the gate deliberately does NOT import the package it polices |
| 4 | `internal/standards/worktree_root_gate_test.go:24` | GATE | red-proof snippet (`filepath.Join("/dev/shm/gopi-"+task, …)`) — below-root, fixture topology |
| 5 | `…:28` | GATE | proof FAILURE MESSAGE text (prose, not a literal the program acts on) |
| 6 | `…:63` | GATE | false-positive control `"/dev/shm-cache/x"` — neither `/` nor end after the directory: must NOT report |
| 7 | `…:111` | GATE | injected probe source (a Go-file TEXT, not a path) |
| 8 | `…:117` | GATE | injected fixture probe source (a Go-file TEXT) |
| 9 | `internal/testutil/scripts_gate_test.go:99` | FIX-REF | was `scratchMemoryBaseDefault = "/dev/shm"` → now `= worktreeroots.ShmDir` (alias, Rule 14). Not the script's env fixture — see §4 |
| 10 | `internal/tools/worktree_sweep_finding_root_gate_test.go:448` | **FIX-BARE** | was `Path: "/dev/shm"` → `memoryBasePath := worktreeroots.ShmDir` |
| 11 | `…:450` | **FIX-BARE** | the `wantRole` expectation now formats `memoryBasePath` (a fixture derived from the constant; the rendering under test is unchanged) |
| 12 | `…:459` | **FIX-BARE** | roleless root now `Path: memoryBasePath` |
| 13 | `internal/tools/worktree_sweep_memory_base_test.go:56` | **FIX-BARE** | candidate list now `{worktreeroots.ShmDir, os.TempDir()}` — the fixture measures whether that filesystem is usable, which the constant answers exactly |
| 14 | `internal/tools/bash_path_policy_test.go:103` | FP-LEAF | `"echo \"git worktree add /dev/shm/x\""` — a command TEXT the policy must not refuse; the fixture's point is the quoted spelling |
| 15 | `internal/tools/bash_path_policy_test.go:165` | FP-LEAF | `rejectOutsideDeclaredRoots("git worktree add /dev/shm/gopi-adhoc …")` — **the refusal subject**: a path derived from the declared root would be the ALLOWED case, inverting the test |
| 16 | `internal/tools/worktree_sweep_test.go:237` | FP-LEAF | an out-of-root registration (`Repo: "/fixture/repo"`) — the sweep must REPORT it; deriving the path from a declared root would make it sweepable |
| 17 | `internal/tools/task_offpolicy_worktree_test.go:49` | FP-LEAF | off-policy fixture list (`/tmp/gopi-rg-master`, `/dev/shm/gopi-bisect`, `/var/tmp/gopi-x`, `home`) — the list of things OUTSIDE policy |
| 18 | `internal/tools/merge_build_gate_unresolved_test.go:48` | FP-LEAF | `mkdir /dev/shm/.../gomodcache/cache/lock: permission denied` — error TEXT matched by a classifier |
| 19 | `internal/scratch/rootgocache.go:59` | PROD-TEXT | the refusal MESSAGE naming the incident ("the tmpfs alternative is the 2026-09-15 one (/dev/shm at 100%)") |
| 20 | `internal/scratch/rootgocache.go:156` | PROD-TEXT | the incident comment ("726M-1.14G per build into /dev/shm") |
| 21 | `internal/taskreport/gateclaim_test.go:114` | ELSEWHERE | `printed task_report: /dev/shm/x/task-report.md` — an accepted CLAIM LINE string; the /dev/shm path is arbitrary payload |
| 22 | `cmd/agent/cli_tasks.go:310` | PROD-TEXT | the `sweep-worktrees` usage text "(/dev/shm assessment, ranked guard 5)" — operator prose |

No site is unclassified. Of the 22: **5 changed** (#9 reference, #10-#13 bare →
reference), 4 are the gate's own apparatus, 2 are definition/definition-adjacent,
5 are fixture fingerprints, 4 are prose/messages, 1 is elsewhere, 1 (this file)
new.

## 4. Fixtures whose meaning would INVERT if derived from the constant

Called out per the briefing (none of these was changed to a reference):

- `bash_path_policy_test.go:165` — a worktree path is refused *because* it is
  outside the repo's declared root. Referencing the declared root would make the
  command legal and the assertion meaningless.
- `worktree_sweep_test.go:237` — the registration must be *out of root* for the
  sweep to report it.
- `task_offpolicy_worktree_test.go:49` — the list IS the set of non-compliant
  paths.
- `merge_build_gate_unresolved_test.go:48` — a temp path appearing in an error
  string; the fixture's point is the message CLASSIFICATION.
- `testutil/scripts_gate_test.go:99` — one more, subtle: the constant is the base
  the retired setup script defaults to, and the test PROVES the script changes
  nothing. It was fixed to an alias of `worktreeroots.ShmDir` (the value has one
  home), but **not** rewritten to the script's env fixture
  (`GOPI_SCRATCH_MEMORY_BASE`): the epics' constraint forbids touching
  `scripts/setup-scratch-home.sh` or its test, and the host's scratch home is
  block-backed since the 2026-09-18 ruling, so `/dev/shm` is simply unusable
  there and the test SKIPS — the honest reading.

## 5. The gate's extension (one gate, additively)

`internal/standards/worktree_root_gate.go` — still ONE gate in `gates()`:

- the predicate gained a topology switch: `_test.go` → `worktreeFixtureFind`,
  else → `worktreeRootFind`; both call one walk (`worktreeLiteralHits`), so the
  literal walk is not duplicated;
- the patterns lost their below-root anchor (`^/dev/shm(.|$)` and `^/tmp(.|$)`),
  which is the shape that was missed;
- `gate.escapeTest` (new field, dispatched per FILE via `gate.escapeFor`)
  lets the fixture topology carry `// fixture-path-ok:` while production keeps
  `// worktree-root-ok:` — a second general escape would have let a production
  site hide behind a fixture reason;
- `gate.scanTests` + `gate.secondRoots` (new fields) add a SECOND traversal over
  the `_test.go` set after the existing production pass. The first pass is
  untouched (same roots, same `wantTest`), so no other gate's behaviour moves.
  `prodAndTestRoots` is a one-line alias of `testRoots` — the root list has one
  home (Rule 14).
- **baseline stays EMPTY** and `maxBaselineEntries` is unaffected.

## 6. Red proofs (one per shape)

In-process predicate proofs (`worktree_root_gate_test.go`):

| proof | shape | expectation |
|---|---|---|
| `TestGate7WorktreeRootLiteralRedProof` (extended) | bare `/dev/shm`, bare `/tmp`, below-root `/dev/shm/…`, `/tmp/…`, `~/workspace/…`, `/home/<u>/workspace/…`, `$HOME/workspace/…` in production | all REPORTED; compliant reference + lookalike `"/dev/shm-cache/x"` silent; escape reported-by-predicate then suppressed by `classify` |
| `TestGate7FixtureTopologyRedProof` (NEW) | (1) bare `/dev/shm` inlined in a fixture → REPORTED; (2) same as a DECLARED fixture constant → silent; (3) `// fixture-path-ok:` → reported by the predicate, suppressed by `classify`; (4) fingerprint `/tmp/x/wt`, reference, relative leaf → silent | pins all four arms |
| `TestGate7ScansTheFixtureTopology` (NEW) | the SCAN, not the predicate | `scanTests` set; ≥1 fixture and ≥1 production file traversed (a dropped second pass fails here instead of silently un-gating 686 files) |
| `TestGate7WorktreeRootLiteralInjectionRedProof` (original) | a violating production file written into a fixture tree | exactly the injected file is reported |
| `TestGate7WorktreeRootLiteralRealTreeIsClean` (original) | the real tree | `NEW=0` |

Live-tree plant/revert matrix (planted in `internal/standards`, run, removed —
never committed; script `/tmp/plant3.py`, output quoted in `tasks/report`):

```
[OK] A bare root, PRODUCTION (the new shape): gate RED ... hits=2 escaped=1 NEW=1
[OK] B bare root, FIXTURE (the epic's gap):  gate RED ... hits=2 escaped=1 NEW=1
[OK] C below-root, PRODUCTION (original):    gate RED ... hits=2 escaped=1 NEW=1
[OK] D below-root, FIXTURE (fingerprint):    gate green ... NEW=0
[OK] E bare root, FIXTURE, DECLARED const:   gate green ... NEW=0
[OK] F bare root, FIXTURE, escaped:          gate green ... escaped=2 NEW=0
[OK] G reverted tree:                        gate green ... NEW=0
```

The original const-decl red proof still passes unchanged (arm 1 of §6, row 1:
below-root production + all home-rooted spellings).

## 7. Before / after violation counts

| measurement | before | after |
|---|---|---|
| sites the gate could REACH (files scanned) | 355 production | 355 production + **686 fixture** |
| gate hits over that set | 0 (the 4 bare-root fixture sites were outside the scan; the bare-root shape was outside the pattern) | 1 (the reviewed `// worktree-root-ok:` X11 subject literal) |
| NEW (unannotated) violations | 0 reported / 3 actually present in the scanned set pre-fix | **0** |
| baseline entries | 0 | **0** |

Pre-fix, the extended predicate found **3 sites** in the tree it now scans
(the epic's missed shape); the fourth, `internal/testutil/scripts_gate_test.go:99`,
was a mirror fixed in the same change. After: `hits=1 baselined=0 escaped=1 NEW=0`.

## 8. Verification

The task report (the harness-tracked `GOPI_TASK_REPORT_PATH`, i.e.
`~/.gopi/sessions/<run-id>/task-report.md`) carries the commands and their
observed output. The 91 `/dev/shm` comment mentions were not touched; the gate's
predicate walks string literals, so prose stays invisible by structure.
