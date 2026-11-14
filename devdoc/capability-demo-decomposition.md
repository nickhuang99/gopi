<!-- doc-class: historical -->
# The capability-demo super-task — investigation, and why the decomposition is two chains

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a0eb77-e6b9-7b7c-bfca-0befa4991241` (branch
`task/01a0eb77e6b97b7cbfca0befa4991241`), run 2026-09-29. This is the
investigation behind the `task(action=reframe, verdict=TOO_BIG)` roadmap: the
operator asked for a decomposition with acceptance criteria, and this document
is the reasoning a later reader would otherwise have to re-derive.

Every factual claim below is either a file:line in this repository or a command
whose output was observed in this run; the verbatim observations live under this
run's session directory — `probe-chain.txt` (the ledger probe),
`probe-shot-self.png`, `probe-capture.png`, `probe-t2i.png` (the eyes) — and are
quoted in this run's report (`## Evidence (self-filed)`).

## The goal, as the operator stated it

The external demo is the primary deliverable; the honest self-examination is
the OBJECTIVE RESULT of pursuing it, not a second design goal — so the work is
ordered "build the deliverable, report what building it found", never "audit
first, present second". Two renditions come out of ONE body of evidence
(external / internal), the linear dependency is a deliberate present-day
simplification, and the annotation layer (a bug shown on a screenshot instead of
described in prose) is the interesting part, not the screenshots.

## 1. The linear chain: the mechanism supports it, at a price, and the price is priced correctly

### 1.1 The inversion the operator saw is real, and the reading here is not the one the briefing offered

Rule 33's gate is `claimable iff parent_task_id empty OR parent.status ∈
{completed, verified}` (`internal/tools/tasks.go:5789-5792`
`taskParentReleaseStatuses`; the predicate at `:5816` `taskParentGateDenied`;
its caller `taskRowClaimable` at `:5934`).

Read against an *assembly* node the inversion falls out in one step. Under the
gate, "my parents are released" is exactly the precondition for "I may run". So:

- With the edge drawn **producer → consumer** (the true dependency direction,
  and the shape the operator described), the gate is **correct as it stands**:
  the assembly is a row whose several parents are the producers, and it comes
  loose only when every one of them is completed/verified.
- With the edge drawn as a **tree** (parent = decomposer, children = work
  items), the same predicate reads backwards — the child waits for the
  decomposer, which is precisely the inversion the operator named.

So the gate is not the defect. What is missing is that **the ledger cannot
express an edge pointing at a producer that does not exist yet** — and that is
forced by *row immutability*, not by the gate.

### 1.2 Why the operator's "linear chain" is the right call, proved by refusal

`gopi task update <id> --parent-task-id <other>` is refused:

```
$ gopi task update 01a0eb77-… --parent-task-id deadbeef
exit=1
gopi task update: unknown flag --parent-task-id
```

The flag is whitelisted for `submit` only
(`cmd/agent/cli_tasks.go:1444`; the flag table at `:735`). And `--parent-task-id`
is validated **at the child's own `INSERT`** — `TaskSubmit` verifies the repo
before the insert for the same reason (`internal/tools/tasks.go:5104` ff.), so a
parent that will not exist yet cannot be named at submit time.

Consequence, and it is the whole argument: in a chain `q1..qN` where every
`q_i` consumes `q_{i-1}`, each child names a producer whose id **is already
known** (it was pre-allocated by `task submit --task-id X`, an existing flag —
see below). Every edge is `child → parent` = **consumer → producer**, and the
gate releases each step at the exact moment its input exists. No mechanism has
to change; only the *roadmap* has to be submitted in the order the allocations
are made.

**What the mechanism cannot express, stated for the report:** the TRUE
assembly shape, where ONE child names MANY producers. A producer's id is only
smaller than its consumer's when the producer is allocated first, but "allocate
all producers first, then one assembly" requires the assembly row's parent to be
set at ITS insert — and that insert happens before the producers have ids (or
requires pre-allocating ids for rows whose payloads are not written yet, which
`--task-id` permits but which no verb does today). With ONE producer the two
shapes converge, which is why the linear chain is not a workaround but the
supported spelling.

**The route that IS expressible, not taken here:** because ids are known at
submit, a multi-parent node is writable with N plain `gopi task submit
--parent-task-id` calls issuing one INSERT each (the parent gate is a
single-column check, so the *claim* side is correct; only the *write* side
insists on one parent per insert). It is flagged in the internal rendition's
"mechanism findings" rather than used, for the reason in §1.4.

### 1.3 The dependency is PARKED, not "undispatched" — the distinction that makes the chain safe

Measured in a sandbox ledger (Rule 34, `gopi sandbox create --label
reframe-chain`):

```
#probe-link2  [pending]  link2 probe … [queue-skip] parent probe-link1 not released (status=pending; releases on completed|verified)
#probe-link1  [pending]  link1 probe … [next]
```

Two things follow, and both matter for the roadmap:

1. **A parked row is genuinely out of every dispatcher's reach.** `[queue-skip]`
   is computed by the claim funnel's own predicate, so `gopi worker` with no id
   will not take `q2` while `q1` is running. The chain is therefore serial
   *without a dispatcher having to know it is a chain*.
2. **A parked row is still executable by hand.** `gopi worker <explicit-id>`
   runs a parked row (Rule 33 says so in as many words: "the explicit id is the
   operator's own door"). This is the escape hatch an operator needs when a
   chain link is skipped, and it is also the hazard — a hand-run of `q2` before
   `q1` completes produces an un-anchored link whose consumers inherit a false
   premise.

**Do not route around it with the enumerator.** `gopi task serve` is
RETIRED and refuses:

```
$ gopi task serve -h
gopi task serve is a deprecated alias — the merger daemon is RETIRED; a merge pass is the one-shot 'gopi merge'
```

**But this host already has a resident dispatcher, measured** (`gopi scratch-gc
deps`, read-only):

```
deps kind=resident name="gopi worker"    where="pid=2211811" origin=gopi detached=no cgroup="/user.slice/user-1000.slice/session-14.scope"
deps kind=resident name="gopi merge --every 5m" where="pid=1761784" origin=gopi detached=no cgroup="/user.slice/user-1000.slice/session-9.scope"
deps summary scheduled=0 units_gopi=0 units_foreign=33 resident=3 gopi=3 foreign=33 total=36
```

So a released chain link is claimed **automatically** by that live worker — the
chain drains itself one link at a time with nobody re-dispatching. Two caveats,
both measured, and both belong in the internal rendition rather than being
assumed away: the worker is **`detached=no`** (still inside a login-session
scope — Rule 24: it dies with the session that spawned it, and `PPID=1` would
not have been evidence either), and `scheduled=0` says nothing installs it.
"Always a worker alive" is therefore a property of *this* session, not of the
machine; a chain left to it stops when the session does.

### 1.4 What a linear chain costs, and why it is still the right call

The honest cost, stated rather than hidden: a linear chain **serializes
independent work**. In the roadmap below, C1 (external edition) and C2 (internal
edition) share a body of evidence and could be produced in parallel; making C2
depend on C1 buys nothing but the convenience of a known id. Two things justify
it anyway:

- The operator explicitly chose it, for a cost reason ("至少目前的依赖关系解决了
  不用改" — no mechanism change today).
- The alternative is the multi-parent write route of §1.2, which works against
  the grain of an immutable-content ledger. Exercising it belongs in the
  internal rendition's findings, as evidence, not in the delivery path — a
  chain that stalls because a link's parents cannot be written is a worse
  failure than a chain that is merely slower.

**One thing the roadmap does NOT do.** The harness auto-enqueues every roadmap item with `ParentTaskID: <reframing run's row>`
(`internal/agent/reframe.go:455-466` `reframeSubmitOptions`, called from
`dispatchReframeRoadmap` `:533-556`). So **the decomposer becomes the common
parent of every child** — the TREE shape, not the producer → consumer shape, and
the children are **siblings**, not a chain: every one of them parks on the same
row and is released at the same instant when that row completes. The
auto-enqueue therefore CANNOT produce the operator's serialization at all, and
the argument is airtight rather than an inference from row ordering:

- a `parent_task_id` must name a row that EXISTS at the child's insert
  (`TaskSubmit` verifies before the INSERT; the flag is submit-only), and no
  reparent write path exists anywhere in `internal/` or `cmd/` — so an edge is
  decided once, at creation, and never repaired;
- the only row that can be created before anything else is the chain's ROOT, and
  the harness gives no child a predecessor as its parent — it gives every child
  the decomposer.

So the decomposition the operator asked for is built by **hand**, in EXECUTION
order, with each link's parent set to its PRODUCER, and the root's parent set to
the decomposer row (`01a0eb77-…`, which exists at insert time because it is the
run doing the submitting). The graph that results is the true dependency graph —
link *k* claimable iff link *k−1* is completed — and it was measured:

```
  before the decomposer row completes:
  #q1 [queue-skip] parent q0 not released (status=pending; releases on completed|verified)
  #q2 [queue-skip] parent q1 not released (status=pending; releases on completed|verified)
  #q3 [queue-skip] parent q2 not released (status=pending; releases on completed|verified)
      #q0  (the decomposer, claimable)

  after q0 -> completed:
  #q1  (no marker  =  claimable)      #q2 [queue-skip] on q1      #q3 [queue-skip] on q2
```

Exactly one link is live at a time, and the release is automatic: the moment a
link writes `completed`, its successor becomes claimable with no scheduler
involved. Two consequences worth stating: this host runs a resident
`gopi worker` (measured below), so the next link is picked up without anyone
re-dispatching; and the harness's OWN nine siblings — created by the
`task(action=reframe)` call that records this verdict — are cancelled by name in
the same run, each cancellation's reason pointing at the live link it duplicates.
Leaving them would be worse than noise: they would all become claimable at the
instant this run closes, and run the same work twice.

This is the concrete, file:line answer to part (c) of the operator's §1.4: **the
mechanism cannot express the chain as an edge the harness writes; it can express
it as an edge a caller writes, and that is what was done.**

## 2. What gopi actually has for "eyes" — measured, with the preconditions that bite

The operator's rule is "prefer gopi's own capabilities, install only what it
genuinely lacks — and using our own capability IS the test, because a defect
found while using it is a first-class finding." So the surfaces were driven, not
read.

| Surface | Verdict this run | Precondition / evidence |
|---|---|---|
| `gopi shot --self` | **Works** | Chrome is launched as a transient systemd USER unit to escape the inherited address-space clamp. First attempt failed with `chrome did not report a DevTools port within 15s … falling back to an attached launch`; the immediate retry wrote a 33 764-byte PNG, exit 0. **Non-deterministic first-try failure — a finding, not a nuisance.** |
| `gopi capture --window` | **Works** | X11 only. `window=true` returned `0x2c0000a` via `_NET_ACTIVE_WINDOW`, 1353×740, `sha256_16=352bd9d5c5edd280`. XDG_SESSION_TYPE=tty with an ssh-forwarded `DISPLAY=localhost:15.0`. |
| `gopi text2img` | **Works**, one CLI gap | Rendered `probe-t2i.txt` (36 bytes) → 5301-byte PNG, exit 0, font chain named in the output (`wqy-zenhei.ttc` first). **Gap:** the CLI documents only `<input.txt> [output.png]`; `--out` (the *tool*'s parameter, and the spelling its own `-h` never advertises as a flag) is refused — `gopi text2img: unknown flag --out`, exit 1. Positional output works. |
| `gopi ui --dump-screen`, `gopi chat --dump-screen`, `gopi ui_state` | **Present, not driven here** | The dump is the *attribute-carrying cell grid* — text truth without pixels and without a vision model. This is the anchor the annotation layer must be reconciled with (§4). |
| `scripts/gopi-stats.sh` | **Works, re-runnable** | Ran against master `68e42825`, dirty files 0. It is read-only, names its base commit, and re-reads at any rev out of the object store — i.e. the "repeatable instrument" the operator's §1.5 asks for already exists for the *numeric* census. |

**The gap is not capture and not text rendering. The gap is composition**: an
image that already exists (a PNG) with vector/text drawn ON it. `text2img`
renders text into a PNG *it creates* (`internal/text2img/render.go` imports
`image`, `image/color`, `image/png` and nothing else); nothing in `internal/` or
`cmd/` uses `image/draw` or composites one image onto another (searched: zero
production hits). The cell grid is a text model with attributes, not a raster.

## 3. The census instrument is already there — the repeatable instrument beats the frozen list

`scripts/gopi-stats.sh` at master `68e42825` printed, among others: 576
production files / 268 142 production LOC / 7004 production funcs; 1246 test
files / 378 532 test LOC; 29 tool names; 34 CLI verbs; 17 task subcommands; 90
env vars; 64 settings keys; 2194 markdown files with 33 rules in `GOPI.md` and
661 lines in `CONFIG.md`. It also carries the **adoption** table that no prose
can fake: of 18 525 tool calls in the 400 newest session logs, `bash` 62.4 %,
`edit` 8.4 %, `job` 8.0 %, `task` 6.6 %, `read` 6.3 %, `memory` 4.0 % — and at
the bottom, `web_fetch` 0.02 %, `screen_capture` 0.02 %, `feedback` 0.01 %. Read
against the demo's purpose that table is the story: gopi's declared surface is
wide, and a handful of verbs carry the work.

The consequence for the roadmap follows the operator's §1.5 directly: the
deliverable must **consume** this instrument (and extend it where the demo needs
a number it does not print), not type a one-time list of capabilities into a
document. A number quoted in a deck that no script re-derives is a rumour with a
number attached (Rule 38).

## 4. The annotation layer — the interesting part, and the smallest shape that gives it

The operator's point: a defect the *user* sees but gopi cannot describe is
reached by pointing at it, not by writing a paragraph about it.

**Today: nothing in gopi draws on an image.** What exists is (a) capture to a
fresh PNG, (b) text into a fresh PNG, (c) a cell grid with attributes as *text*.
What does not exist is any path that takes an existing PNG and adds a label, an
arrow or a rectangle.

**The smallest thing that gives it, in this repo's own idiom, is a four-point
annotator (x, y, w, h) plus a text label** — no fonts to embed (the system CJK
font is already autodetected by `internal/text2img`), no new module, and it
composes only from what is already imported: `image/png` to read and write the
base, `image/color` and the existing rasterizer to draw the label, integer
line-drawing for the arrow. That is one new CLI verb's worth of code, and the
first question the roadmap asks about it is Rule 35's four, not "how pretty".

**The reconciliation that must not be skipped** (it is the one research item in
the roadmap): the cell grid is gopi's *pixel-free* view of a screen, and an
annotated raster is a *pixel* view of the same screen. The standing engineering
rule "渲染真值" — render truth is PIXEL truth, not an OCR/vision score — means
the annotator must be able to anchor a mark on coordinates that BOTH views agree
on, or the two views drift and the annotation points at the wrong cell. Deciding whether
the anchor is a cell range, a pixel rect, or both is the research item, and the
answer decides the annotator's data model.

## 5. Two renditions, one evidence base

The external and internal renditions are not two products: they are two
renderings of one investigation, and the rule that keeps them honest is the
direction of the arrow between them. A capability claim in the external version
is a **selection** from the internal version's measured findings; it is never
written independently. The concrete failure mode to guard against is the one
the briefing names: the internal pass finds a defect that changes a capability
claim, and the external version still asserts the old claim. That guard is a
publish-step checkpoint in the roadmap, not an intention.

What the internal version carries that the external one does not: the mechanism
findings of §1, the defects of §2 (the `--out` gap, the first-try `shot`
failure), the adoption table read honestly, and the annotation-gap finding of
§4.

## What was deliberately NOT done in this run

- **No mechanism change.** Rule 33's gate, the row-immutability rule and the
  reframe auto-enqueue shape were read and reported; none was edited. The
  operator's decision was explicit ("至少目前的依赖关系解决了不用改"), and a
  change here would land on the system layer (Rule 28: that half is peerbox's).
- **No new artifact created.** Nothing was added to the tree except this
  document; every probe wrote under this run's session directory. The one
  filesystem object this run created is the ordinary sandbox of Rule 34
  (`gopi sandbox create --label reframe-chain`), used to measure the chain on a
  ledger that is not the operator's; it is not cleaned up by hand because the
  existing reclaimer owns it, and the reclaimer's own verdict was read:
  `gopi scratch-gc sandboxes --dry-run` →
  `keep …/sandbox-reframe-chain-d84fe686 reason="not aged"`.
- **No `--resume`, no second path.** `devdoc/chat-crash-resume.md`'s
  "deliberately NOT done" section was read and is honoured: the positional seed
  already resumes, and nothing here re-opens it.
- **No annotation tool built.** Building it is a roadmap item, not a this-run
  edit — it needs its Rule 35 four questions answered first, and it belongs on a
  feature branch (Rule 28).
- **The chain was NOT left to the auto-enqueue.** The nine siblings the harness
  creates for this roadmap are cancelled by name in this run, each reason naming
  the live link that supersedes it; the chain itself is built by hand in
  execution order (§1.4). Leaving the siblings would have run the whole
  decomposition twice, all nine at once, the instant this run closed.

## Mechanism findings (for the internal rendition)

1. **Row content is immutable and the parent is set only at insert**
   (`internal/tools/tasks.go:5104` `TaskSubmit`; the update path's own error at
   `:3803`: "task content is immutable — cancel and re-submit"). Together with
   the absence of any reparent path, this makes the true assembly shape
   (one node, many producers) expressible only by pre-allocating the consumer's
   id before its parents have one — or by N single-parent INSERTs, which the
   claim side already supports.
2. **`task(action=reframe)` cannot express the operator's edge.** The
   auto-enqueue parents every roadmap item to the decomposer
   (`internal/agent/reframe.go:455-466`, `:533-556`), so a roadmap cannot hand a
   chain the producer → consumer shape; only the briefing can carry it.
3. **The chain is enforced by parking, not by a scheduler.** Measured:
   `[queue-skip] parent <id> not released`. And the one enumerator that could
   have drained a chain is retired (`gopi task serve` refuses by name), so
   progress through a chain costs one deliberate dispatch per link.
4. **A parked link is still hand-runnable** (`gopi worker <id>`), by design.
   A chain is therefore advisory against the dispatcher and authoritative only
   for the *claim* funnel.
