<!-- doc-class: normative -->
# The Standards Gate — turning the checklist into a lock (Rules 16–21)

**Input:** `devdoc/coding-standards-checklist.md` (the research document — 10
classes, 14 sources, a Top-8 execution list). This document is the *landing*
report: what became a machine check, what did not, and the real numbers.

**Branch:** `feat/standards-gate` · **Worktree:** `~/workspace/gopi-standards-gate`
· **Commits:** `81bec21` (Rules 16–18), `98c4a72` (Rules 19–20 + checklist §16 +
PROJECT.md Rules 16–20).

> **SUPERSEDED NUMBERS — read this first.** The baseline sizes quoted below
> (Rule 16 = 352, Rule 17 = 120) are this landing round's measurements and were
> correct at `81bec21`/`98c4a72`. The two ratchet items in the backlog, **R1**
> (the `%v`→`%w` codemod) and **R2** (`os.Setenv`→`t.Setenv`), were then
> **delivered** by the style sweep (branch `feat/style-sweep`, report
> `devdoc/style-sweep-20260912.md`), which also had to prune the now-stale
> baseline entries this gate fails on. Current sizes: **Rule 16 = 10**
> (production; every remaining site is nil-guarded, where `%w` would print
> `%!w(<nil>)`), **Rule 17 = 80** (test-only; the list itself is the reason for
> most entries — only 26 of the 80 carry an on-line marker, see the Rule 17
> allow-list note below), Rule 18 = 7. The gate itself is
> unchanged.
>
> **RATCHETED AGAIN 2026-09-26 (`feat/standards-reds`): Rule 17 = 80** (was 88;
> the figures in the table below are the landing round's split and stay as that
> round's measurement). The 8 entries retired were all of
> `internal/agent/rawlog_test.go`'s `os.Unsetenv("GOPI_RAW_LOG")` sites: they
> became `t.Setenv("GOPI_RAW_LOG", "")`, which carries the identical INPUT to the
> reader — the raw logger is disabled by the value `"OFF"` and by nothing else,
> so absent and empty are the same default-enable path — while restoring the
> caller's state *including absence*, where the bare unset left the variable set
> to `""`. The permits were DELETED, never re-keyed (a line-keyed permit rots on
> the next edit above it): measured on the branch,
> `grep -cP '^-\t"internal/'` = 8 removed, `'^\+\t"internal/'` = 0 added, and the
> gate's own line reads `80 baseline entries` with `0 new (unannotated)`.
> (Counts re-measured on `feat/style-sweep` after its second master merge; the
> `internal/standards` data report is the source — `Rule 16 hits=10 baselined=10`,
> `Rule 18 hits=7 baselined=7`.)

---

## 1. The thesis, and why the first 15 rules were not locks

PROJECT.md Rules 1–15 are **prose addressed to an agent's judgement**: "no magic
numbers", "no silent failure", "constants exist to be referenced". A prose rule
is enforced by the reader noticing, and the reader is the thing being audited —
an agent that misses a `%v` wrap is the same agent that would miss a rule saying
"wrap errors". Two design notes make that concrete:

* The hardcoding review (checklist §12, Rule 13/14) found the *same* defect
  shape 10× in one package **after** the rule against it existed.
* GOPI Rule 11 itself records that `git_blame` and `code_search` went unused at
  0 %/0.3 % adoption until an adoption proof was required — a rule that nobody
  executes is a preference, not a constraint.

A lock is different in one property: **the violation is computed, not
recognised**. Nothing in Rules 16–20 asks an agent to be diligent. If a new
`panic(...)` lands in a non-`Must` function, the build goes red whether or not
anyone read the rule — and if the check itself silently stops matching, the
red-proof probes fail. The lock does not trust, and that is exactly why it works
where prose did not.

**Answers to the acceptance question "why are these five locks and the original
15 not":** because each of the five is (a) a deterministic predicate over the
AST, (b) green on the day it lands, (c) red only on a *new* violation, and
(d) unable to rot silently (baseline staleness + red-proof probes + escape
visibility). The original 15 fail (a) and, historically, (c) and (d).

---

## 2. The mechanism (one paragraph, six gates)

Each gate scans `internal/` + `cmd/`, and classifies every hit into exactly one
of three states:

| state | meaning | result |
|---|---|---|
| **baselined** | a known legacy site, recorded as an exact `file:line` in `internal/standards/baseline.go` (reason next to it) | passes; a *stale* entry is an error |
| **escaped** | a true positive the team deliberately keeps, annotated in the code with the gate's marker | passes; **stays visible** in the data report |
| **unannotated** | everything else — a real, new violation | **fails** |

Three properties do the work:

1. **The baseline is exact `file:line`, never a file glob** — a violating line
   that *moves* is reported again, so an allow-list cannot rot into a blanket
   exemption. A stale entry is an error ("a silent permission").
2. **The escape is not a suppression.** The rule's own marker sits where the
   code is, with the reason (`// panic-ok: …`), and the site keeps appearing in
   `TestStandardGateDataReport` — a deliberate violation is auditable, never
   invisible.
3. **Every gate ships a red proof** (`TestGateN…RedProof`): a violating snippet
   that must be reported *and* a compliant snippet (plus, for the new gates, a
   deliberate near-miss) that must not be. Without the probes a predicate could
   stop matching and the gate would read green forever.

---

## 3. Landed gates — real numbers, allow-list reasons

Command for everything below:

```
go test -v -run 'TestStandardGates|TestStandardGateDataReport' ./internal/standards/
```

| GOPI rule | checklist row | hits (whole tree) | baselined | escaped (deliberate) | new (unannotated) | false positives |
|---|---|---|---|---|---|---|
| **Rule 16** `%v` → `%w` in `fmt.Errorf` | 2.2 | 352 | 352 | 0 | **0** | 0 |
| **Rule 17** tests must not `os.Setenv` (allow-list is **91** entries; the row is the counted split, not an arithmetic derivation) | 5.2 | 107 | 96 | 11 | **0** | 0 |
| **Rule 18** no `else` after a terminating `if` | 4.3 | 7 | 7 | 0 | **0** | 0 |
| **Rule 19** no `panic` outside `init`/`main`/`Must*` | 2.7 | 1 | 0 | **1** | **0** | 0 |
| **Rule 20** no `get`/`is`/`handle` accessor prefix | 1.4 | 1 | 0 | **1** | **0** | 0 |
| **Rule 21** every env-name constant has a real reader | 1b/env | 0 | 0 | 0 | **0** | 0 |
| **Rule 17 (shape)** lossy `os.Getenv`-captured restore — gate 2's second predicate (`fix/rule17-env-restore`) | 5.2 | 0 | 0 | **0** | **0** | **0** (4 negative probes in `TestGate2EnvRestoreShapeRedProof`) |

**Rule 21 (gate 6) landed after this document's first round** (branch
`feat/scratch-gc-resident-tick`); its full account — predicate, the two real
defects it found, the negative tests — is in
`devdoc/scratch-gc-resident-tick.md` §"Gate 6". Summary: an env-name **constant**
must be referenced inside the argument list of an env call somewhere in the tree
(`os.Getenv`, `t.Setenv`, `config.EnvBool`, a helper funnel) — a declared hook
read by nobody is a control surface that lies. Its baseline is EMPTY by
construction, and its escape is `// env-reader-exempt: <reason>`.

**Allow-list reasons (why each baselined/escaped site is permitted):**

* **Rule 16 (352 entries).** Every entry predates the gate. The `%w` migration
  is a mechanical codemod and is its own task; until it runs, the gate's job is
  to make the count monotone — it can only go DOWN, and a fixed site leaves a
  STALE entry that fails the build. The list is deliberately exact-line so the
  codemod's re-baselining forces a human to look at each site.
* **Rule 17 (91 entries).** The landing round's 120 are now 91 after the sweep,
  the `fix/rule17-env-restore` addendum, and this branch's re-measurement. The
  surviving entries are NOT "hand-rolled save/restore that `t.Setenv` performs" —
  a later bisect showed most are **per-call flips inside a closure**, where
  `t.Setenv`'s test-scoped undo is the wrong tool; the rest are child-process
  handoffs. Measured on the list that actually exists (`testEnvBaseline`, parsed
  programmatically — never hand-counted): **65** entries sit on an `os.Unsetenv`
  line (**49** of them a bare unset whose nearest `os.Setenv` is more than 8 lines
  away — **33** forcing a default/false branch in a test body, **14** the undo
  half of a hand-rolled pair written as an explicit `t.Cleanup`/`defer`, **2** a
  suite-wide hook reset in `TestMain` — and **16** inside a hand-rolled
  `Setenv`/`Unsetenv` region; the class-wide reason is that a bare `t.Setenv`
  cannot express ABSENCE), while the other **26** are `os.Setenv` — **20** marked
  a child-process handoff a bare `t.Setenv` would erase before the child runs,
  **4** a synchronous read or restore half. Marker coverage is **26 at ±0, 28 at
  ±1, 38 at ±3, 42 at ±6** — and it is **one-sided**: all 26 on-line markers sit
  on an `os.Setenv` line and **none** of the 65 `os.Unsetenv` entries carries one,
  so "each site carries a reason" is true only for the `os.Setenv` third and the
  *list* is the reason for the rest. What IS a defect
  is the *shape* of a hand-rolled restore (`prev := os.Getenv(k)` +
  `os.Setenv(k, prev)` cannot restore an ABSENT variable); gate 2 reports it and
  the reviewed form is `internal/testutil.SnapshotEnv`. (Note: `t.Setenv` is
  incompatible with `t.Parallel`, so "just parallelise the test" is not the
  fix — see §5.) The measurement method and its curve are in
  `devdoc/standards-gate-accounting.md`.
* **Rule 18 (7 entries).** Purely cosmetic shape, behaviour-identical to the
  guard-clause form, in functions outside this task's refactor scope.
* **Rule 19 (0 baselined, 1 escaped).** `internal/agent/crashdump.go`
  `recoverRunPanic` **re-panics** the panic it caught, so the process exit code
  stays non-zero — that is the crash-dump contract, not an in-band error. The
  gate reports at the enclosing **function** line (so the marker can live in the
  func's comment block), marker `panic-ok:`, and the site stays in the report.
  An in-band panic written as `panic(err)` *is* reported (see the probe).
* **Rule 20 (0 baselined, 1 escaped).** `internal/shot/x11.go` `getRootImage`
  mirrors the X11 `GetImage` protocol request name — renaming it would obscure
  the wire spec it implements. Marker `name-ok:`. Three more X11 names
  (`getImageBand`, `handshake`) are annotated too; `handshake` is a *lowercase*
  near-miss that the predicate must not report (it takes arguments).

**False positives: 0 for all six gates.** The reason is visible in the probes —
each predicate is deliberately narrow, and the narrowing is itself tested:

* Rule 16 reports only `fmt.Errorf` whose `%v` argument is *error-shaped*
  (`err*`/`*Err`/`…Err()`/`Unwrap()`); `fmt.Errorf("bad count: %v", n)` is not
  reported.
* Rule 17 only fires on the `os.` package selector — `t.Setenv` is untouched.
* Rule 18 only when the `if` body's last statement terminates.
* Rule 20 only on **niladic methods** with `+Uppercase` (`Get(key)` is a
  command; a package function named `island` is not a method; lowercase
  `handshake` is not a prefix).
* Rule 19 exempts `init`/`main`/`Must*` by name (tested with a `Must*` that has
  arguments).

**Red proofs (constraint 10).** Two layers:

1. **Snippet probes** (in-repo, always run): each gate reports a deliberately
   violating snippet, stays silent on the compliant and near-miss snippets.
2. **Real-file injection, run once during this task:** a helper containing
   `panic("empty path")` was added to `internal/standards/gate.go` →
   `TestStandardGates/TestGate4…` reported `NEW violation … gate.go:260` and the
   data report flagged `NEW=1`; an inserted `getRootImageY()` method in
   `internal/shot/x11.go` → `NEW violation … x11.go:440` for Rule 20. Both
   reverted; the suite returns green. The three older gates were verified the
   same way when they landed (`81bec21`).

---

## 4. Baseline hygiene: the review budget is what stops an allow-list becoming a suppression

A gate whose baseline nobody can read is a suppressed tree wearing a green
badge. So the two mechanisms are separated and both are enforced:

* **`maxBaselineEntries = 400`** (a ceiling, in `gate.go`): a gate may not grow
  past it — hitting it *fails* the build, so "add another entry" is not an
  available move forever. The two legacy gates (352 / 120) sit under it.
* **The ratchet target is one-third of that ceiling (~130)** and the intended
  direction is *down*: `TestStandardGateDataReport` prints a `RATCHET:` line for
  any gate above 50 entries (the review-readability bar) and the two shrinking
  tasks are in §5.
* **Every gate must carry a `baselineReason`**, rendered next to the entries
  (`baselineOf(g).render()`), and a test asserts it is present — an unexplained
  allow-list is a silent permission (this is GOPI Rule 15b's own discipline
  applied to the gate's own config).

---

## 5. Backlog — checkable items deliberately not landed in this round

Written as executable specs (predicate + evidence + why it was not wired now) so
a future session can land them without re-deriving. `B` = backlog, `R` = ratchet
task.

| # | Item | Predicate | In-repo hits (est.) | Why not now |
|---|---|---|---|---|
| **B1** | **G1** no underscores in production identifiers (Google Style guide `#mixed-caps`, decisions `#underscores`) | `ast.Ident`/field containing `_`, excluding test-func names and generated files | 0 production | Trivial to land; deferred only to keep this round at five gates. Highest-value next gate (a 10-line predicate, 0 baseline). |
| **B2** | **G2** initialisms keep case (`Id`→`ID`, `Url`→`URL`, …) (decisions `#initialisms`) | identifier/leading word matching `(?i)^(Id|Url|Http|Json|Api|Xml|Sql|Ip|Uuid)($\|[A-Z_])` | 0 production | Needs a decided exemption surface (JSON tags are wire-frozen: `json:"user_id"` must NOT be "fixed"), so it needs the rule's escape baked in first. Equivalent to staticcheck ST1003. |
| **B3** | **G4** error strings not capitalised / not punctuated (decisions `#error-strings`) | first arg of `errors.New`/`fmt.Errorf`: literal starting uppercase (unless an exported name) or ending `.`/`!`/`:` | 0 | Complements Rule 16; needs the exported-name carve-out tested to avoid false positives on `errors.New(ErrX.Code)`. |
| **B4** | **G6** keys use `crypto/rand`, never `math/rand` (decisions `#crypto-rand`) | `rand.Read`/`rand.Intn` in a file that also touches a key/token/secret/nonce/salt identifier | 0 | GoPi already complies; the predicate needs a name-shape heuristic (a proxy), so it is lower value than B1. |
| **B5** | **G7** no naked return in a non-small func (decisions `#naked-returns`) | `ReturnStmt` with no results in a named-result func spanning >15 lines | 0 | Needs a size threshold — a proxy; land with B1's batch. |
| **B6** | **G8** `%w` only in the last verb position (best-practices `Placement of %w`) | `fmt.Errorf` literal with `%w` before another verb | 0 | Same file/area as Rule 16; land inside the codemod task so the migration and the placement rule agree. |
| **B7** | **G9** keyed struct literals for external types (decisions `#field-names`) | `CompositeLit` of a `SelectorExpr` type with any unkeyed element | 0 | Checkable, 0 hits; borderline value (the compiler already catches most real breakage). |
| **B8** | **G10** `gofmt -l` must be empty (guide `#formatting`) | `gofmt -l internal cmd` prints nothing | 0 today | A one-line test, but on its first violation it would demand a formatting diff over ~100 files; land it *with* the formatting sweep, not before. G13 (one field per line) is subsumed here. |
| **B9** | **G11** exported names need doc comments (decisions `#doc-comments`; checklist row 1.3) | exported top-level `Func`/`Type`/`Var`/`Const` without a doc comment starting with the name | ~35 of ~120 | The list is large enough that a bare gate would be red on day one — it needs a baseline, i.e. the same ratchet treatment; batch it with a doc-comment sweep. |
| **B10** | **G12** package names must be informative (best-practices `Util packages`) | directory basename ∈ {`util`,`helpers`,`common`,`misc`,`shared`,`base`,`core`} rejected | 0 | Multi-root (`internal/`, `cmd/`, plus future foreign repos) needs a root allow-list; cheap, do it with B1. |
| **R1** | **Ratchet: the `%v`→`%w` codemod** (Rule 16's 352 entries) | mechanical rewrite with a per-site `// no-wrap:` decision | 352 | Its own task by design (this round's job was the *lock*, not the migration). Success = baseline under ~130 and each removal leaving no stale entry. |
| **R2** | **Ratchet: `os.Setenv`→`t.Setenv`** (Rule 17's 120 entries + 5 fresh) — **DELIVERED** as far as a mechanical rewrite can go: the style sweep converted the trivial sites (`a73e063`), then had to **revert three** (`d7a05f3`), because a closure that flips an env var per CALL needs the flip undone when the call returns, and `t.Setenv`'s undo is test-scoped. Baseline 100 → **95** (`devdoc/rule17-env-restore.md`), then **91** after this branch's re-measurement. The genuinely broken residue was the hand-rolled restore's *shape* (`os.Getenv` cannot restore ABSENCE) — now a machine check: gate 2's `testEnvRestoreShapeFind` + `TestGate2EnvRestoreShapeRedProof`, reviewed fix `internal/testutil.SnapshotEnv`. **Do NOT re-run the mechanical rewrite over the surviving sites.** | mechanical rewrite; must not add `t.Parallel` in the same change | **DELIVERED** (100 → 95 → 91) | `t.Setenv` forbids `t.Parallel`, so the sweep stays a prerequisite for any parallelism work (checklist §13); `internal/stylegate` `RuleTestSetenvScope` enforces the per-call direction from now on. |
| **R3** | **Top-8 items not landed** (checklist §11 rows 2/3/6/8: `-race` run, `errcheck`/`bodyclose`, DTO wire-contract registry, single-reader env vars) | see checklist §11 | — | Rows 2/3/8 need an external tool or a scheduling decision (`-race` cost, CI location) — checklist §11/§13 explicitly rejects a whole-tree external linter as a *first step*. Row 6 (DTO registry) is medium false-positive risk by its own rating and needs a curated registry artefact. |

**Why the Top-8 was not implemented wholesale:** its rows split into three
groups — (i) adoptable *domestically* with a narrow deterministic predicate
(rows 1/5/7 → the five gates above), (ii) needing an external linter or a
scheduling decision (rows 2/3/8), and (iii) needing a new curated artefact whose
false-positive risk is rated medium (row 6). Only group (i) can be green on the
day it lands without a migration first, which is the adoption constraint the
checklist itself set (§11, §13).

---

## 6. Checklist items that are NOT mechanically checkable (excluded, with reason)

Copied from the authoritative list in `devdoc/coding-standards-checklist.md`
§16.2 so this report is self-contained, and so no future agent re-proposes them
as a "gap":

| Source rule | Why excluded |
|---|---|
| Name *proportionality* / single-letter-in-large-scope (decisions `#variable-names`) | Needs scope-size + readability judgement; a length threshold is a proxy that fires on idiomatic `i`, `n`, `r`, `w`. |
| *"A name should be as short as it needs to be"* (naming guidance) | Not decidable from syntax. |
| *Local consistency* — a file's own style wins (guide `#local-consistency`) | It IS a human override; a checker contradicts the rule it implements. |
| *Interfaces defined by the consumer, only when one exists* (decisions `#interfaces`) | Needs call-graph + intent; "1 implementation" is a proxy that flags legitimate seams. |
| *Do not create your own `context` type* (decisions `#custom-contexts`) | Checkable in principle but over-fires on any type accidentally matching the method set; GoPi has no such type. Listed, not gated. |
| *Package doc completeness* (decisions `#package-comments`) | Presence ≠ informativeness; existence checks reward `// Package foo is foo.` noise (checklist §13 already rejects comment-density rules). |

---

## 7. Dependencies

**None added.** The gates use `go/parser`, `go/token`, `go/ast`, `regexp`,
`strings`, `sort`, `strconv`, `unicode`, `testing` — all stdlib, zero CGO, zero
external binary, zero `go.mod` change (`git diff --stat` touches no `go.mod`).
This satisfies constraint 5 by construction: no new module, therefore no upper
bound to pin.

---

## 8. How to run / how to add a gate

```bash
cd <worktree>
go test ./internal/standards/                       # all six gates (in `go test ./...`)
go test -v -run TestStandardGateDataReport ./internal/standards/   # the numbers report
go test -v -run 'TestGate.*RedProof' ./internal/standards/         # the red proofs
```

**Which arm to run for a red proof of gate 9** (and, generally, of any gate): the
gate's own PROBE SUBTEST —

```bash
go test ./internal/standards/ -run 'TestStandardGates/TestGate9GitEnvRedProof' -count=1
```

— because that is the arm whose `enforce()` FAILS on a new violation and names
the site (`NEW violation (checklist 1b/git-env) at <file>:<line> [<func>]`).

**The published name has ONE source: `gate.probePath()`** (`internal/standards/gate.go`,
`probeArmRoot + "/" + g.probe`). It is what the registry's baseline block, the
NEW-violation message and every doc take their command from, and the difference
it encodes is the whole point: `TestGate9GitEnvRedProof` alone selects the
standalone RED PROOF — a predicate test over a planted snippet that never reads
the tree — so it exits 0 on a dirty tree. Measured 2026-10-28 (task `01a18235`):
three unannotated gate-9 hits were in the tree while the command quoted below
printed `ok` — quoted in full because this paragraph IS the record of the defect:
<!-- probe-bare-ok: quoted on purpose — this paragraph records the 2026-10-28 command that exits 0 on a dirty tree; the command a reader RUNS is the subtest path above -->
```
go test ./internal/standards/ -run 'TestGate9GitEnvRedProof'
```
`TestGateProbePathsSelectTheTreeArm`
(`probe_paths_test.go`) now holds the contract down, one arm per half:

- every published path is the subtest path of a `TestStandardGates` registration
  whose body calls `scan` — the published name selects an arm that can see the
  tree;
- no registry probe's own body calls `scan`/`scanText`/`classifyAll`/`enforce`/
  `repoRoot` — a red proof proves the PREDICATE and stays off the tree (a
  fixture proof may still build and walk its OWN `t.TempDir()` tree);
- every gate in `gates()` is still dispatched, by name or through the dynamic
  `t.Run(g.probe, …)` registration.

Naming the gate's FILE is not naming its ARM: `internal/standards/git_env_gate_test.go`
holds THREE top-level arms, and only the third one fails on a fresh breach:

| arm | what it judges | a NEW unbaselined Gate9 hit |
|---|---|---|
| `TestGate9GitEnvRedProof` | the PREDICATE, over a planted snippet; reads no file | passes — deliberately (probe_paths_test.go arm (b) forbids a registered probe from reading the tree) |
| `TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly` (renamed 2026-10-16 from `TestGate9GitEnvRealTreeScansTestFiles`) | a read floor + baseline staleness | passes |
| `TestGate9GitEnvScansTheRealTree` (added 2026-10-28, task `01a19730`) | floor + the gate's own ONE accounting (`scan` → `classifyAll` → `enforce`) over the real tree | **FAILS, naming the site** |

The third arm exists because the arm that could fail was reachable ONLY as the
registry subtest `TestStandardGates/TestGate9GitEnvRedProof`, which a
single-segment `-run` on the probe name never selects — while the sibling gates
that own a top-level real-tree arm (`TestBranchForceGateScansTheRealTree`,
`TestSandboxSettingsDocGateScansTheRealTree`,
`TestRemovalAuditGateSeesTheRealTree`) have always been runnable by their own
name. It is a SEPARATE arm on purpose: folding the tree read into the red proof
would break probe_paths_test.go arm (b) and destroy
`git_env_gate.go`'s distinction between "the predicate rotted" and "the tree grew
a violation".

Measured 2026-10-16 (task `01a1417e-3b3b-7bd6-abee-1943754ebefe`): a reverted
repair run against the coverage arm printed `ok` in `11.717s` and was read as a red
proof — a vacuous one. Measured 2026-10-28 (task `01a19730`) on the same ground:
the real-tree arm reports `real tree: 2262 test file(s), 59 hit(s), 59 baselined,
0 escaped, 0 NEW`, and one line changed back to the inherited-environment
spelling at `internal/tools/gitblame_enrich_test.go:41` turns the pair
`-run 'TestGate9GitEnvRedProof|TestGate9GitEnvScansTheRealTree'` into `exit 1`
naming that file and line, while the red proof alone stays `ok`.

Adding a gate = a predicate `func(g gate, p *parsed) []hit` + a gate record in
`gate_test.go` (id, checklist row, probe, escape marker, reason, baseline) + a
`TestGateN…RedProof` with a violating and a compliant snippet + a baseline var
in `baseline.go`. The hygiene tests (`TestBaselinesAreSortedAndUnique`,
`TestBaselineReasonsAreCarried`) enforce the rest. A gate whose subject is TEXT
rather than Go sets `scanTextRoot` (the existing whole-tree text walk) and may
declare the corpus it owns with `textCorpus` — a nullable `gate` field added for
the red-proof restore gate below, so a text gate states its file set in ONE place
instead of excluding trees inside its predicate.

### Addendum — the red-proof restore gate (`redproof_restore_gate.go`)

Landed 2026-10-12 on branch `task/01a1306e10c6705191d2413c1cc4601d`, EMPTY
baseline, its own red proof + read floors + a LIVE fixture. Subject: the pair
`git checkout -- <path>` (writes the INDEX into the worktree) with an
index-relative diff criterion (`git diff <path>`, `git diff --stat <path>`) used
as the proof that a red proof's mutation was restored — on an ADDED file the
restore is a no-op that exits 0 and the criterion prints nothing, so "restored"
is claimed with the mutation live. `git diff <path>` with no revision is
worktree-vs-INDEX and is reported as a hit; the corrected spelling is
`git diff HEAD -- <path>` (or `git hash-object` vs `git rev-parse HEAD:<path>`).
Recipe, measured failure and scope (records are NOT scanned): 
`devdoc/redproof-restore-criterion.md`; see also PROJECT.md Rule 5's closing
paragraph.

**Second arm — content-identity anchor (added 2026-10-14).** The same gate now
carries arm (c), the OTHER half of Rule 5's restore criterion. Arm (a)/(b) police
*where the criterion reads from* (the index, which the restore writes); arm (c)
polices *which two things it compares*. A "content-identity" reading whose sides
are BOTH artifacts of the same run — a snapshot copy and its recorded digest —
cannot see a write made after it was taken, and does not say so (measured: task
`01a138317d847ac3806c8a701eb652f3`, a `same=YES` reading across an 11:13:12
re-write, with the delivered blob a different one). Reported shape: a digest
command (`git hash-object`, `sha256sum`, `md5sum`, `cmp`, …) **on one line** with
an equality operator, or a recorded verdict (`same=YES`) in a unit carrying ≥2
digests — in a unit that names NO revision anywhere. Exempt: any unit naming a
revision (`rev-parse`/`cat-file`/`ls-tree`, `HEAD`, a branch, a `rev:path`
qualifier, a bare digest that is not a `key=` value). Inline escape:
`red-proof-identity-ok:`. LIVE proof: `TestGateRedProofRestoreIdentityLive`.
Recipe and the timeliness rule:
`devdoc/redproof-restore-content-identity.md`.

---

## 9. Not done / honest gaps

* **Not landed:** B1–B10 (see §5). In particular the *highest-value cheap* gate
  (B1, underscores) is a 10-line predicate with no baseline — it was left for the
  next round rather than smuggled in, to keep this round's five gates fully
  verified.
* **The 352/120 baselines are debt.** They are honest (every entry is a real
  pre-existing site) but a gate at 352 entries is a ratchet, not yet a lock for
  *that* rule; §3's "lock" claim holds for Rules 18/19/20 today and for 16/17
  only as "no new violations". This is stated rather than smoothed over.
* **Rule 20's escapes need review at each X11 touch.** The four `name-ok:`
  annotations are protocol-name justifications; if the X11 client is ever
  rewritten to hide the protocol, the escapes should be removed with it.
* **No CI.** These gates run wherever `go test ./...` runs; GoPi still has no CI
  pipeline (checklist finding 6.4), so "runs in CI" is not yet true — it is true
  that they run in the build/test step that the project already performs.

---

## 10. Verification (as shipped)

* **Build:** `go build ./...` → OK; `go vet ./internal/standards/` → clean;
  `gofmt -l internal/standards internal/shot internal/agent` → empty.
* **Tests:** `go test ./internal/standards/ ./internal/shot/` → ok (gates 5/5
  PASS; shot 39.5s).
* **Gate run:** `TestStandardGates` → 5/5 PASS; data report →
  `hits=352/120/7/1/1`, `baselined=352/120/7/0/0`, `escaped=0/0/0/1/1`,
  `NEW=0` across all five.
* **Red proofs:** 5 snippet-probe tests PASS (each asserts red on a violating
  snippet, green on a compliant and a near-miss snippet) + 2 real-file
  injections observed failing and then reverted to green.
* **Commits:** `81bec21` (Rules 16–18), `98c4a72` (Rules 19–20, GOPI.md
  Rules 16–20, checklist §16).

### Addendum — gate 6 (Rule 21), branch `feat/scratch-gc-resident-tick`

* **Tests:** `go test ./internal/standards/` → ok; `TestStandardGates` → 6/6
  PASS; data report → `hits=352/120/7/1/0/1`,
  `escaped=0/0/0/1/0/1`, `NEW=0` across all six.
* **Red proof:** `TestGate6EnvReaderRedProof` re-runs the **registered** gate
  against synthetic sources (unread hook reported; `os.Getenv(const)` silent; a
  helper funnel silent; a following statement's identifiers not readers;
  `// env-reader-exempt:` classifies as escaped), and
  `TestGate6EnvReaderFindsTheRealTree` fails if the whole-tree reader index is
  empty (a green-but-blind scan is itself a failure).
* **Real defects the gate found and this branch fixed:**
  `internal/agent/taskheartbeat.go` carried a second spelling of
  `GOPI_TASK_STALE_SEC` with no reader (deleted);
  `internal/hippocampus/config.go` `hippocampusEnvPrefix` is a namespace, not a
  knob (annotated `env-reader-exempt:`).
* **Also fixed in the gate's own plumbing:** the comment stripper mis-handled
  `/*` inside a string literal, and a multi-line raw string containing a path
  glob (`sessions/*.jsonl`) blanked a whole file — a blind-green scan. Gate
  registration also moved from positional index lookups to `gateByProbe`
  (inserting gate 6 had shifted the indices).
