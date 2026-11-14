<!-- doc-class: historical -->
# The capability-demo chain, closed — what it produced, what it found, and what is reusable

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Link `[9/9]` (row `capdemo-9`, branch `task/capdemo9`, worktree
`/home/user/workspace/gopi-capdemo9`). This is the **assembly** link: it consumes
all eight upstream links, merges `master` into the branch so that one commit
carries everything, re-runs both editions' checkpoints at that frozen head, and
answers the one question a linear chain is uniquely able to settle — whether its
nine `parent_task_id` edges are the producer→consumer edges the design intended,
or whether some link ran out of order.

Its raw material is the two renditions themselves — `docs/archive/demo/external/`
(the edition an outsider reads) and `docs/archive/demo/internal/` (the evidence base) —
plus the ledger. It adds no capability and fixes no defect (Rule 23/26: those are
new tasks, and a closure that widened its own diff would be a repair pretending
to be a report).

---

## 1. What the exercise produced

| Artifact | Where | Link |
|---|---|---|
| a repeatable census instrument | `scripts/gopi-stats.sh` (extended; generator of every figure in both editions) | 1/9 |
| the **external edition** — a static, zero-dependency bundle an outsider reads | `docs/archive/demo/external/` (README + 6 stills + `CAPTIONS.md`) | 2/9, v2 at 5/9, **v3 at 8/9** |
| the **internal edition** — the evidence base, including gopi's own defeats | `docs/archive/demo/internal/README.md`, `ANCHORS.md` | 3/9, **v2 at 7/9** |
| the first defect, fixed end to end: `gopi text2img --out` | code + repro test on `task/capdemo3` | 3/9 |
| the annotation gap, answered **by measurement** then designed | `devdoc/capability-annotate.md`, `capability-annotation-design.md` | 4/9 |
| the implementable contract + 2 specimen stills | `devdoc/capability-annotation-handoff.md`, `docs/archive/demo/external/` v2 | 5/9 |
| `gopi annotate` — the smallest verb, composing from the existing rasterizer | `internal/text2img/annotate.go`, `cmd/agent/cli_annotate.go` + tests | 6/9 |
| the annotated walkthrough: one defect, reported as prose **and** as an annotated still | `docs/archive/demo/internal/` §10 + `stills/10-*` | 7/9 |
| every claim in the external bundle anchored, and a **publish checkpoint** that keeps it so | `scripts/capdemo-external-check.sh`, `internal/standards/capdemo_external_gate_test.go` | 8/9 |
| the frozen delivery point: both editions + this document in one commit | `task/capdemo9` tip | 9/9 |

The two editions are now **one artifact with two faces**, and the rule binding
them is one-directional and stated in both: when a finding in the internal
edition changes a claim in the external one, the claim changes there — and the
external edition's own checkpoint fails a build when a claim's anchor goes stale.

---

## 2. The frozen delivery point

The chain's design says the assembly happens **after** the delivery-point sync, so
that both renditions are read at one commit rather than two. That is this head:

```
$ git rev-parse HEAD        # task/capdemo9, after merging master in
d98f1f02db49ad1fe1945e450c6b05e8ab0c62f2
$ git merge-base --is-ancestor 91e96019 d98f1f02 && echo "master 91e96019 is an ancestor of the delivery point"
master 91e96019 is an ancestor of the delivery point
```

Both editions resolve their evidence **at this head**:

| Edition | Its own checkpoint, run at `d98f1f02` | Result |
|---|---|---|
| external (L2/L5/L8) | `bash scripts/capdemo-external-check.sh ./bin/gopi` | `OK — every anchor in docs/archive/demo/external resolves (1 note(s), 0 stale)`, **exit 0** |
| external (build gate) | `go test ./internal/standards/ -count=1 -run TestCapdemoExternalBundleAnchorsResolve` | `ok github.com/nick/gopi/internal/standards 0.890s` |
| internal (L3/L7) | `gopi text2img -h \| grep -- '--out out.png'` then the two derived-surface tests | advertised (exit 0); **both tests green** |
| internal (its own figures) | `bash scripts/gopi-stats.sh` re-run at this head | 578 files / 269 504 LOC / 35 verbs — recorded in `docs/archive/demo/internal/README.md` §1 |

Transcript: `/home/user/.gopi/sessions/capdemo-9/assembly-checks.txt`. The census
run: `/home/user/.gopi/sessions/capdemo-9/census-d98f1f02-clean.txt` (clean
worktree, `dirty files: 0`).

---

## 3. What the exercise found

### 3.1 The mechanism finding: the gate is a consumer→producer edge, and the chain is its proof

`internal/tools/tasks.go:5789` (`taskParentReleaseStatuses` — the CLOSED set
`{completed, verified}`), `:5816` (`taskParentGateDenied`), the funnel's single
call site `:5696` → `taskRowClaimable`. A pending row is claimable **iff** its
parent is absent, or the parent's status is in that release set. The gate is not
backwards; what it cannot do is *generate* a chain — the only auto-built tree is
`internal/agent/reframe.go:460`'s fan (N siblings, all parented to the
decomposer). A linear chain has to be written by the caller, one `TaskSubmit` per
edge, and a row's parent can never be rewritten afterwards
(`cmd/agent/cli_tasks.go:1444` — the flag is submit-only).

### 3.2 The chain's execution, read out of the ledger (§4 below quotes it in full)

Nine rows created inside **four seconds** (13:01:49 → 13:01:52, one enqueue) ran
strictly serially over **two hours twelve minutes**, one live link at a time,
because the park is computed per candidate at claim time by that one predicate.
Every edge is the intended producer→consumer edge, and **no link ran out of
order** — including the negative check: `gopi worker <explicit-id>` (Rule 33's
operator door, which bypasses the gate by design) was never used anywhere in the
chain.

### 3.3 A delivery race the chain's own design predicted, measured

The internal edition recorded, at link `[7/9]`, a defect that was **already fixed
on `master` in parallel** (`8d6e0582`, 15:14) but not in the link's snapshot: the
help text advertised `gopi text2img report.txt --out out.png --size 28` one layer
ahead of the generated inventory, and two assertions were red against the
snapshot. At the frozen delivery point both are green — the sync carried the fix
in (`git merge-base --is-ancestor 8d6e0582 HEAD` → exit 0 at `d98f1f02`, exit 1 at
the link's head). **The general statement, now measured rather than argued: until
the delivery-point sync runs, a red assertion in a chain link is a statement
about the SNAPSHOT, not about the product.** Rule 30a already fixes the order
(sync last); this chain is the worked example of what the rule buys.

### 3.4 The one thing the two upstream links genuinely DISAGREED about — resolved here

Link `[8/9]` merged its external-edition work into master carrying an edit to
`docs/archive/demo/internal/README.md`, and **that edit deleted the internal edition's
two committed stills** (`stills/10-defect-raw-window.png`,
`stills/10-defect-annotated-walkthrough.png`) and everything that cited them.
The internal edition at `master` still cited both. Neither link noticed; nothing
failed.

At the assembly the merge conflicted on exactly that file, and the resolution
took the side that keeps the evidence — the stills are restored in this branch,
byte-identical to link `[7/9]`'s committed blobs
(`sha256_16 1e27ca078310ff84` and `29d1369e425e9b69`), as are §10's prose and
`ANCHORS.md` §H. This is a **finding about the chain, not a code defect**: a
linear chain gives each link its own branch and no link sees the next one's diff,
so a link can delete another link's committed evidence. Link `[8/9]`'s report does
name the file it edited (`docs/archive/demo/internal/README.md` — "superseded claims
bannered"), but not that the edit took two committed stills with it, so a reader
following the report would not know. The pattern to carry forward is narrow and
cheap: *a link that edits a document it does not own states the effect of the
edit, not just the filename*.

---

## 4. The chain's parentage, answered against the real ledger

Verbatim, this head — `gopi task list --all | grep '\[./9\]'` and
`gopi task show <id>` per link; full transcript in
`/home/user/.gopi/sessions/capdemo-9/evidence-chain-edges-capdemo9.txt`:

```
#capdemo-1 … [1/9] THE CENSUS INSTRUMENT, EXTENDED      (created 2026-09-29 13:01:49) [via worker]
#capdemo-2 … [2/9] THE EXTERNAL EDITION                  (created 2026-09-29 13:01:50) [via worker]
…
#capdemo-9 … [9/9] ASSEMBLY — both renditions delivered together … (created 2026-09-29 13:01:52)  [running]
```

| Row | `parent_task_id` (its producer) | claimed at | run: started / status |
|---|---|---|---|
| `capdemo-1` | `01a0eb77-e6b9-7b7c-bfca-0befa4991241` (the decomposer, `completed`) | 13:04:04 | 13:04:07 completed exit=0 (162 turns) |
| `capdemo-2` | `capdemo-1` | 13:29:25 | 13:29:28 completed exit=0 (130 turns) |
| `capdemo-3` | `capdemo-2` | 13:40:51 | 13:40:54 completed exit=0 (83 turns) |
| `capdemo-4` | `capdemo-3` | 13:50:39 | 13:50:41 completed exit=0 (63 turns) |
| `capdemo-5` | `capdemo-4` | 14:02:00 | 14:02:02 completed exit=0 (255 turns) |
| `capdemo-6` | `capdemo-5` | 14:25:17 | 14:25:20 completed exit=0 (274 turns) |
| `capdemo-7` | `capdemo-6` | 15:16:19 | 15:16:22 completed exit=0 (150 turns) |
| `capdemo-8` | `capdemo-7` | 15:36:11 | 15:36:15 completed exit=0 (240 turns) |
| `capdemo-9` | `capdemo-8` | 16:04:46 | 16:04:48 (this run) |

**Every edge is the intended producer→consumer edge: link *n*'s row is parented
to link *n−1*'s row, and each claim timestamp falls AFTER its producer's run
started and inside its producer's window.** The one edge that is not
link-to-link is the head of the chain: `capdemo-1` is parented to the decomposer
`01a0eb77-…` (status `completed`), not to a sibling — i.e. the chain's root is an
auto-enqueued roadmap ITEM (shape A), and links 2..9 are the caller-written chain
(shape B). That is the design, not a deviation.

**No link ran out of order.** Three independent observations say so:

1. **Ordering.** Claim times are strictly increasing and each sits inside its
   producer's run window (producer started before, consumer claimed after) — see
   the table.
2. **The gate's own fingerprint, quoted.** Where a consumer was still parked, the
   ledger printed the predicate's own reason string, e.g.
   `[queue-skip] parent capdemo-3 not released (status=in_progress; releases on
   completed|verified)` — that sentence *is* `taskParentGateDenied`'s output, not
   a paraphrase of the rule.
3. **The negative result.** The gate's escape hatch — `gopi worker
   <explicit-task-id>`, exempt by Rule 33 — was not used anywhere in the chain:
   every one of the nine rows was claimed by the resident worker
   (`claimed_by: peerbox:2211811` on all nine), which is the funnel path.

Checked against the ledger, the dispatcher is a real resident process, not an
inference:

```
$ ps -p 2211811 -o pid,ppid,sid,lstart,cmd
    PID    PPID     SID                  STARTED CMD
2211811   23485   23485 Tue Sep 29 12:45:34 2026 gopi worker
$ grep -q 'session-.*\.scope' /proc/2211811/cgroup && echo "NOT detached" || echo detached
NOT detached      # cgroup 0::/user.slice/user-1000.slice/session-14.scope (Rule 24: judged by cgroup, not PPID)
```

Its `lstart` (12:45:34) precedes the first claim (13:04:04), so the same resident
worker claimed all nine links. Two limits stated honestly: the ledger keeps
`created_at` at second resolution, so the **insert** order of the nine rows is not
recoverable (the **claim** order is what the gate orders, and that is legible);
and this is one chain, one worker pid, one host, one head — a single sample.

**Correction to the upstream record.** `docs/archive/demo/internal/ANCHORS.md` §I (link
`[7/9]`) states the same claim with the chain's first **seven** links
(13:04:04 → 15:16:19). That was true when it was written; §I is at this head
extended with the rows that existed after it, so the table above is the whole
chain.

---

## 5. Template verdict

**Partially reusable.** Named, part by part — this is the answer the exercise
exists to produce, and it is deliberately not a single word:

| Part | Verdict | Why |
|---|---|---|
| **The publish checkpoint** (`scripts/capdemo-external-check.sh` + `internal/standards/capdemo_external_gate_test.go`) | **reusable** | It is a bundle-shape checker: digests, byte counts, captions, producing verbs, commit pins, generator + measured-commit for census figures. Point it at another `docs/<name>/` bundle by changing one constant. Nothing about it is demo-specific except the directory name. |
| **The census instrument** (`scripts/gopi-stats.sh`) | **reusable** | Already the project instrument; the chain only exercised it. Genuine prior art that both editions cite by generator + commit — the one artifact every figure in the demo rests on. |
| **`gopi annotate`** (`internal/text2img/annotate.go`, `cmd/agent/cli_annotate.go`) | **reusable** | A shipped verb with its own tests and refusals (off-image rectangle refused by name, never clipped). It is not a demo prop. |
| **The two-editions structure** (external = selection, internal = evidence base; one-directional rule between them) | **reusable** | It survived three revisions and one genuine disagreement between two links, and the rule ("a finding here changes a claim there") is what made that disagreement visible at all. |
| **The annotated-walkthrough device** (prose report + the same finding as an annotated still, pixel-counted) | **reusable** | Cheap, and the pixel measurement (`ink_pixels` = an independent PIL count, input digest unchanged) is the part that makes the still evidence rather than decoration. |
| **The 9-link linear chain as a way to run a demo build** | **one-off** | It cost **2h12m** of strictly serial time for nine links, and it needed the operator to write every edge by hand (`reframe` makes siblings, never a chain — §3.1). A demo that must be *shipped* should be one task. The chain is a **demonstration of the ledger's gate**, not a production recipe. |
| **The `data:`-URL still (`04`)** | **one-off** | The device (a self-contained page for `gopi shot --url`) is worth remembering; this specific still is a capture of one page at one head. |
| **The stills themselves** | **one-off** | Two are dynamic (`02` a live desktop, `04` a rebuilt page), so their digests are records of *that* capture. `CAPTIONS.md` says which is which; a re-shoot moves them by design. |

**What would have to change for the chain to be "reusable" as a method**: the
edges are hand-written and the run is serial, so the cost is O(links) wall-clock
with an operator in the loop at submit time. Until the ledger can express a
decomposition whose items run in order (today it cannot — §3.1), a chain like this
is a **deliberate demonstration**, and the honest place for it is a demo, not a
pipeline.

---

## 6. What remains open (honest)

- **The registry of the nine links' reports is not machine-checked.** Nothing
  fails if a future link's report goes missing; the table in
  `docs/archive/demo/internal/ANCHORS.md` §G is prose. (Deliberately not fixed here: it is
  a new mechanism, and Rule 35's four questions are not answered for it.)
- **`docs/archive/demo/internal/` still carries link-scoped readings in the present
  tense.** This link corrected the two that mattered (the census column and the
  `--out` defect's status); a full tense sweep of §1–§9 is a documentation pass,
  not an assembly step.
- **The stale acceptance criterion is still stale.** `docs/archive/demo/internal/README.md`
  §10 quotes the pre-sync `--- FAIL` transcript as link `[7/9]`'s record; per the
  assembly's landing check this materiality is **recorded, not fixed** (the
  transcript is evidence of that link's head, and rewriting it would destroy the
  record).
- **The chain's own artifact — `devdoc/capability-demo-decomposition.md` — still
  describes the plan, not the outcome**, and was not brought to this head.
- **Nothing in this link repairs the deletion upstream, and the deletion is live
  on `master` today.** Measured at this head: `git rev-parse master` →
  `91e96019` (unchanged through this run); `git ls-tree --name-only 41c22340
  docs/archive/demo/internal/stills` → empty (link `[8/9]`'s tip, the parent of the merge
  that landed in this branch, carries no stills); `git ls-tree --name-only
  9f162686 docs/archive/demo/internal/stills` → the two stills. So the removal travels
  with `41c22340`, whose landing on `master` is the merger's call, not this
  link's — this branch's delivery simply re-states the stills. If `master` merges
  `task/capdemo9`, the stills come back; if it merges `task/capdemo8` first, the
  deletion wins.
- **`gopi worker <explicit-id>` remains an unexercised door** — that it bypasses
  the gate is code, not measurement (Rule 33's exemption); this chain only proves
  it was not used.
- **The always-a-worker property is a session property.** `/proc/2211811/cgroup`
  puts the dispatcher inside a login-session scope (Rule 24), so the chain
  advances only while that session lives. Nothing in the design depends on it.
