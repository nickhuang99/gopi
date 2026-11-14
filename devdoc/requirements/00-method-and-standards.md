<!-- doc-class: normative -->
# Reverse Requirement Analysis — Method, Standards and Boundaries

Umbrella document for the requirement-reconstruction series (`devdoc/requirements/*.md`).
Status: method + standards + boundaries. Module documents are dispatched from here.

## 0. Provenance — this document has a history, and a reader must know it

| fact | value |
|---|---|
| owner task | `01a0ab12-2537-7b89-9fe2-43d7c3933a2a` — `[1/15] UMBRELLA [0/N]`, `source=llm`, `in_progress` at this revision |
| first version | commit `12e1ac74` "umbrella — method, module map and dispatch-ready briefings"; its `--stat`: 298 + 323 + 555 + 97 lines in one commit |
| **rebuilt version** | commit `8e2fa780`, same title, replacing the first — 513 lines added to THIS file (§2–§11 below). The version you are reading is the `8e2fa780` text, not the `12e1ac74` text |
| module-dispatch contract added | `a2d1af6d` |
| this pass | 2026-09-17, host HP, branch `docs/req-umbrella-verify` from master `9ce8e159`: §4.3 (the briefing template) added; §7.1–§7.3 rebuilt with package-level coverage; every measured number re-measured and the stale ones corrected in place |
| the epic briefing's line count | "537 + 329 lines" is the **master `9ce8e159`** revision (`wc -l`); `12e1ac74`'s own `--stat` reports **298 + 323** for the same two files. Both readings are true of *different revisions* — a citation must say which revision it measured |

A sibling series (`01a0ab11-*`, its module rows `01a0ab3d-*`) produced
`01-task-ledger.md` … `11-build-and-repos.md` from an earlier state of this method.
**Those module documents are not rewritten or duplicated here** — they are the
series' product, produced under this standard. Where this document's text
contradicts one of them, §7.4 lists the contradiction; **the module document
governs its own module's content**, this umbrella governs the *standard*.

- Series shape: one document per **user-visible responsibility boundary** (§7), plus this one.
- Docs only: **no code changed, no tests written** by this document or its modules.

---

## 1. The problem this series exists to solve

**The operator's diagnosis — this is the premise, and every later reader should
know it is a person's complaint, not a documentation preference.** Quoted verbatim
from the series briefing `briefings/brief-requirements-reverse-analysis.md:13-24`
(the briefing records it as the operator's own words, 2026-09-16):

> "这个问题的核心是什么？你看到的现象是典型的软件公司很多写测试的常见问题，并不是
> 新鲜的问题。核心是程序员根本不懂业务，他们根本不知道用户需要什么，业务流程是什么，
> 他们只是看到一些代码有一些文档，但是文档的目的不是给业务经理看的那种用户需求，
> 而是实现细节，简单说就是说了什么 what/when/where/how，但是没有说为什么 why，
> 因为只有程序员明白 why 就是用户真正需要什么，程序员才能自己设计测试里，明白什么是
> bug 什么是允许的。
>
> 这个就是核心，我们的项目迭代太快，又没有真的需求来检验，gopi 完全不明白应用的场景
> 是什么，所以，根本不理解要做什么测试。
>
> 我觉得我们应该派一个 gopi 做整个项目的需求场景分析，等于是逼迫 bond 理解它的任务
> 就是 gopi 的应用场景是什么，有什么不足，有什么优点，这个是一个反向的需求分析，
> 然后写成文档，让我们最后来评判看看程序员对于项目的理解和项目经理的理解的差别。"

Two consequences of reading it literally, both binding on this series:

1. **The purpose is a comparison, not a manual.** The series is judged by putting
   "the programmer's understanding of the project" beside "the project manager's
   understanding" (the operator's framing). A document that only restates the code
   gives the operator nothing to compare — hence §2.1 (the gap is the deliverable).
2. **The audience is a business owner, not a maintainer.** "文档的目的不是给业务经理
   看的那种用户需求" is the failure; the fix is §3.1's acceptance test.

At this revision gopi has **1,960 files under `devdoc/`** (`find devdoc -type f | wc -l`,
re-measured 2026-09-17 on master `9ce8e159`; the briefing's "930" and the first
version's "1835" were each true of their own revision) and **no document stating a
requirement** other than this series. The docs answer *what / when / where / how*.
None of them answer **why**, so neither a human nor an agent can tell a bug from
allowed behaviour.

**The worked instance (the reason this series was ordered).** `internal/tools/task_finish_cleanup.go:3-18`
documents a 4-step client close-out chain whose ordering *is* the safety argument:
① guard clean → ② push → ③ `worktree remove --force` → ④ `branch -d`. The push is
idempotent and harmless (line 16-18: "a removed worktree with an unpushed branch =
work loss"); the worktree removal is destructive. A dirty worktree means *a live
writer is in there*, which justifies deferring **③ and ④** — it does not justify
deferring **②**. Two tests pin the stronger, wrong rule:

- `internal/tools/task_finish_cleanup_test.go:201` — `t.Errorf("no steps may run on a dirty worktree: %+v", rep.Steps)`
- `internal/tools/task_finish_cleanup_test.go:917` — same assertion in the server-recycle arm

"no steps may run" is a **correct-assertion / wrong-requirement** failure: the
assertion is green, and it encodes a need nobody holds. Without a recorded why, the
next reader cannot see the difference between the test and the need. That is the
class of defect this series is built to find.

## 2. The method — reverse requirement analysis

The direction is inverted from ordinary requirements engineering:

```
   ORDINARY:   need ──▶ requirement ──▶ design ──▶ code ──▶ test
   REVERSE:    code + evidence ──▶ INFERRED requirement ──▶ gap analysis
                                                     ▲
                                        the value is HERE, not in the inference
```

A requirement is **inferred**, never invented. The inference is a hypothesis; the
conclusion of a module document is the **gap** between that hypothesis and the code:

| §6 asks | meaning | worked shape |
|---|---|---|
| **serves** | the code implements the inferred need — cite the site | push-then-remove ordering (`task_finish_cleanup.go:3-18`) |
| **falls short** | the need is real, the code does not meet it | a delivery whose push never runs because the tree was dirty |
| **over-builds** | the code does something no need asks for | (module-doc business; named there, not here) |
| **never decided** | nobody ever decided — the code is an accident of history | the "no steps may run" rule above |

**The inference must be falsifiable.** Every inferred requirement states, in the same
paragraph, the evidence it rests on and what would refute it. An inference that no
observation could refute is not a finding, it is an opinion — label it as one (§8).

### 2.1 The gap is the deliverable

A module document that only describes the code has failed. It is graded on
**§6 and §8 being load-bearing**: at least one named gap, and at least one named
**unknown**. Section §6 may legitimately contain zero "falls short" rows for a
module that is well served — but then it must say so explicitly and show the §6
sweep that established it (which surfaces were examined and came back clean).

## 3. What counts as a `why` statement

### 3.1 The acceptance test (one question)

> **Could the product owner read the statement, and accept it as a need they hold?**

If only a programmer can read it, it is still implementation detail and **fails**,
regardless of how true it is. Truth is not the bar; **recognisability as a need** is.

Corollary — the **audience rule**: a `why` statement may name a *user-visible actor*
("the operator", "the agent", "the server node", "a task"), but may not name a package,
file, function, flag or type. Those belong in the *evidence* slot beside it, never in
the statement itself. The 8-section shape (§4) enforces this separation structurally:
§1 forbids code identifiers, §4 is nothing but citations.

### 3.2 A `why` statement has two halves

A statement is complete only when both are present:

1. **the need** — what a user must have, in product-owner language; and
2. **the test** — how the user would personally verify it was met (the observable).

A need with no observable is a slogan; an observable with no need is a GUI element.
Both halves, or it is not a requirement.

### 3.3 GOOD / BAD, worked from gopi's own code

Three real cases. The BAD forms are all *true statements about the code* — that is
the point: the defect is not falsehood, it is that only a programmer can read them.

**(i) The close-out chain — need vs mechanism**

- BAD (mechanism; acceptable only as *evidence*, never as a `why`):
  > "`runFinishCleanupChain` runs `git push`, then `git worktree remove --force`,
  > then `git branch -d`, and skips when `git status --porcelain` is non-empty."

- GOOD (`why`):
  > **Need:** when a run finishes on my laptop, the work it produced must reach the
  > server and stop occupying my disk, and nothing that has not been handed over may
  > be destroyed. **Test:** after a clean finish, the peer has the branch, my worktree
  > directory is gone, and the local branch is gone; after a finish interrupted by a
  > failed push, the worktree and branch are still there.
  > **Evidence:** `internal/tools/task_finish_cleanup.go:3-18` (the ordering *is* the
  > safety argument), `:191-192` (`finishCleanupPushVerb`).

- **Gap this exposes:** the BAD form cannot distinguish "defer the destructive step"
  from "defer everything", so a test author reasonably wrote `task_finish_cleanup_test.go:201`.
  The GOOD form makes the right assertion obvious: **the push must have run; the
  removal must not have.**

**(ii) The daemon — need vs design argument**

- BAD: "the merger holds no in-flight state and spawns no child."
- GOOD:
  > **Need:** a background worker I leave running for days must not be able to wedge,
  > and when it misbehaves I must be able to say what it did. **Test:** I can stop and
  > restart it without unwedging anything, and `--status` never answers "unknown".
  > **Evidence:** ledger `01a0a995-8742-77fc-a164-53054ea652c5`, operator verbatim:
  > *"A daemon must be simple. Every branch of logic it carries multiplies its failure
  > surface."* The same row shows the two incidents each caused by the daemon *having*
  > logic: the one-value channel with two waiters (`01a0a949`, fixed `1c41bc25`) and
  > the durable state file whose shape drifted.

**(iii) The `why` behind a whole subsystem — the operator's own words**

- BAD: "memory rows are scored with useful/useless feedback and demoted to cold
  tier at ≥2 useless votes."
- GOOD:
  > **Need:** I want to find what worked last time instead of re-deriving it, and I
  > want the store to stop returning what has gone bad. **Test:** a repeated task
  > surfaces the earlier lesson; a lesson I voted useless stops coming back.

### 3.4 Where a `why` legitimately comes from

| source | strength | note |
|---|---|---|
| operator **verbatim** in a ledger row | ★★★ | quote it; name the task id |
| operator decision recorded in a **briefing** (`briefings/*.md`, 154 on master `9ce8e159` — re-measured 2026-09-17; the first version of this doc said 122, true of its own revision) | ★★★ | the brief *is* the operator's dispatch text |
| a **GOPI.md** rule | ★★ | a rule is a decided constraint; its own "Why:" line is the intent |
| **commit message** body | ★★ | often states intent the code cannot show |
| **intent-stating comment** at the site | ★★ | e.g. `task_finish_cleanup.go:3-18` |
| **inference** from code shape | ★ | must be marked inferred (§8) |
| an agent's *plausible* reasoning | — | **not evidence**; see §7 |

## 4. The mandatory 8-section shape

Every module document is exactly these sections, in this order. Deviating is a
review failure, not a style choice — the sections are the grading surface.

| # | Section | Must contain | Fails if |
|---|---|---|---|
| 1 | **What this is for** | product-owner language; **zero code identifiers** (no package/file/flag/type names) | a reader needs the repo to understand it |
| 2 | **Who is served** | every actor named, each with a need: operator (nick) / the agent (Bond, the harness user) / server & client nodes / future maintainers | actors listed but no need distinguished |
| 3 | **The workflow it sits in** | the end-to-end run it is part of, with **entry and exit conditions** | the module is described in isolation |
| 4 | **Why it exists** | the need per §3, each with a **citation**: task id, `file:line`, commit SHA, GOPI.md rule | any claim uncited and unlabelled |
| 5 | **Acceptance in user terms** | observable statements concrete enough that a test author can separate a bug from allowed behaviour | phrased as "returns nil" / internal invariants |
| 6 | **Gap analysis** | four explicit surfaces: serves / falls short / over-builds / never decided | no gap, or gaps unnamed |
| 7 | **Implications for tests** | **names existing tests** whose verdict the requirement changes — and the direction of change. **Writes no tests.** | test advice is generic |
| 8 | **Confidence and limits** | confidence per claim; every inference marked *inferred*, each with its **weakest link**; unknowns explicit | inference presented as fact |

**Why §6 and §8 are load-bearing.** The series' premise is that the missing *why*
hides defects and makes bugs indistinguishable from allowed behaviour. §1–§5
reconstruct the need; they are *input*. §6 is where the reconstruction pays: it is
the only section that can contradict the code. §8 is what keeps §6 honest: an
inference stated with its weakest link is a usable finding, an inference stated as
fact is the next agent's misinformation. A module doc with a strong §6 and a
missing §8 is *more* dangerous than one with neither.

### 4.1 Required front matter per module document

```
Boundary:      <the responsibility, in one line>
In scope:      <paths/packages covered>            (evidence, not the statement)
Out of scope:  <what is deliberately excluded, and where it is analysed instead>
Boundary why:  <why this is a user-visible responsibility and not a directory>
```

### 4.2 Self-contained module briefing (dispatch template)

Dispatched verbatim to each module worker, who never sees this umbrella. The
worker's payload must additionally carry: the 8-section table above, §3.1–§3.3 in
full, the six constraints (§5), the evidence canon (§6.1) **with a live query to
run**, the honesty rules (§7), and its own in-scope/out-of-scope list.

**The template itself** (fill the `<…>` slots; hand the worker this text and nothing
else — it is the acceptance property of this task that a worker who never opens this
umbrella can produce a conforming document):

```
You are writing ONE module document of the reverse requirement analysis of gopi:
    devdoc/requirements/<module>.md
Do not write code. Do not write tests. Do not run `make fmt`.

THE METHOD (inverted requirements engineering):
    code + evidence ──▶ INFERRED requirement ──▶ GAP ANALYSIS
The inference is the input; the GAP is the deliverable. A document that only
describes the code has failed.

STEP 0 (do this before writing): `git worktree add ~/workspace/gopi-<task> -b docs/requirements-<module>`
 — never continue another attempt's branch (Rule 6a, Rule 23). Record the master
 SHA you read at the top of your document; every `file:line` you cite is that revision.

STEP 1 — MINE THE EVIDENCE (run these; the numbers must be produced in YOUR session):
  sqlite3 ~/.gopi/memory.db "select task_id, summary, payload from tasks
    where source='human' and length(payload)>200 order by created_at;"
  sqlite3 ~/.gopi/memory.db "select task_id, datetime(created_at,'unixepoch'), summary
    from tasks where source='human' and summary not like 'gopi run %'
      and (length(payload)<=200 or payload='') order by created_at;"
  sqlite3 ~/.gopi/memory.db "select task_id, summary, payload from tasks
    where source='human' and (payload like '%nick 原话%' or payload like '%nick 终审%'
      or payload like '%nick 拍板%' or payload like '%nick 明确要求%' or payload like '%nick 同意%'
      or payload like '%nick 裁决%' or payload like '%不采用%' or payload like '%我们不%' or payload like '%I want%');"
  sqlite3 ~/.gopi/memory.db "select t.task_id, substr(t.summary,1,90) from tasks_fts f
    join tasks t on t.id=f.rowid where tasks_fts MATCH '<your module vocabulary>' and t.source='human';"
  # secondary, in this order: briefings/*.md (the operator's dispatch text) →
  # GOPI.md rules (each rule's own "Why:" line) → devdoc/roadmap.md → commit
  # messages → intent-stating comments at the site.
  # Then NARROW by your module's vocabulary (e.g. worktree|dirty|residue|clean).
  # Both id shapes exist in `tasks` (t-<unix> before 2026-09-09, UUID after) —
  # a grep for one silently misses the other.
  # A row with a short payload is NOT a lesser row: 48 of the 608 `source=human`
  # rows are summary-only and carry the operator's FINAL decision (the nine
  # `[M4.5 补充] … (nick 终审: …)` rows). Read set B before you conclude anything.

STEP 2 — what a `why` statement is. It is a need + the test the user would apply,
in the user's language; it may name a user-visible actor (the operator / the agent /
a server or client node / a task) but may NOT name a package, file, function, flag
or type — those go in the evidence slot beside it. THREE WORKED PAIRS:

  BAD (true, and still a failure — only a programmer can read it):
    "`runFinishCleanupChain` runs `git push`, then `git worktree remove --force`,
     then `git branch -d`, and skips when `git status --porcelain` is non-empty."
  GOOD:
    "Need: when a run finishes on my laptop, the work it produced must reach the
     server and stop occupying my disk, and nothing that has not been handed over
     may be destroyed. Test: after a clean finish the peer has the branch, my
     worktree is gone, the local branch is gone; after a finish whose push failed,
     worktree and branch are still there."
     → the gap it makes visible: a test pinned the stronger, wrong rule
       `internal/tools/task_finish_cleanup_test.go:202` "no steps may run on a dirty
       worktree" — the push is harmless and must RUN, the removal is destructive and
       must DEFER.

  BAD: "the merger holds no in-flight state and spawns no child."
  GOOD: "Need: a background worker I leave running for days must not be able to wedge,
     and when it misbehaves I must be able to say what it did. Test: I can stop and
     restart it without unwedging anything, and `--status` never answers 'unknown'."
     (evidence: operator verbatim, ledger `01a0a995-8742-77fc-a164-53054ea652c5`)

  BAD: "memory rows are scored with useful/useless feedback and demoted at ≥2 useless votes."
  GOOD: "Need: I want to find what worked last time instead of re-deriving it, and I
     want the store to stop returning what has gone bad. Test: a repeated task surfaces
     the earlier lesson; a lesson I voted useless stops coming back."

STEP 3 — THE SHAPE (all eight sections, in this order; deviating is a review failure):
  1 What this is for — product-owner language, ZERO code identifiers.
  2 Who is served — every actor named, each with the need it has.
  3 The workflow it sits in — end-to-end, with entry and exit conditions.
  4 Why it exists — the need per STEP 2, each with a citation token.
  5 Acceptance in user terms — observable, so a test author can tell bug from allowed.
  6 Gap analysis — four explicit surfaces: serves / falls short / over-builds /
    never decided. (Zero "falls short" rows is allowed ONLY if you show the sweep
    that established it.)
  7 Implications for tests — NAME existing tests whose verdict your requirement
    changes, and the direction. Write no tests.
  8 Confidence and limits — confidence per claim, every inference marked [inferred]
    WITH its weakest link, unknowns explicit.
  Sections 6 and 8 are the load-bearing ones. A doc with a strong §6 and no §8 is
  MORE dangerous than one with neither.

  Front matter (evidence, not statement): Boundary / In scope / Out of scope /
  Boundary why — why this is a user-visible responsibility and not a directory.

STEP 4 — CHECK YOUR NEEDS AGAINST THE SIX BINDING CONSTRAINTS (§5: C1 one static
binary, zero deps / C2 the operator controls the whole thing / C3 daemons are simple /
C4 the agent cleans up after itself / C5 small team, limited budget / C6 why over
what). A requirement that violates one is not a gopi requirement — record the
conflict as a §6 gap rather than dropping it, and say which constraint it hits.

STEP 5 — HONESTY. Never invent a requirement to fill a section: an empty section
says so. "The intent is not recorded anywhere I can find; the weakest link is X" is
a FINDING and outranks a plausible reconstruction. Every claim carries a citation
token (ledger task id / `file:line` / commit SHA / GOPI.md rule) or is prefixed
[opinion]; every inference is marked [inferred] with its weakest link. Quote the
operator in the original language — a paraphrase is an inference.

STEP 6 — CLOSE. `git status --short` must list only your document. Commit it.
End the document with "What I could not determine".

ACCEPTANCE: the eight sections present and §6/§8 load-bearing; every claim cited or
labelled; at least one finding the operator did not already know (name which
sentence it is); a closing unknowns list. Judged by a human against his own
understanding of the project.
```

### 4.3 Shared packages: name the files you own

`internal/tools` is one Go package holding five different responsibilities —
106 production files, 61,646 lines (`find internal/tools -maxdepth 1 -name '*.go'
! -name '*_test.go' | wc -l` → `106`; `xargs wc -l` → `61646`, master `9ce8e159`).
It is assigned to M1/M4/M6/M7/M8 in §7.3. **A worker whose boundary touches
`internal/tools` names each file it covers, explicitly**, so two workers cannot
describe the same file twice and the union of their lists is checkable.

## 5. The operator's vision as binding constraints on requirements

These are not background. **A requirement that violates one of them is not a gopi
requirement** — it is a requirement for a different system. Each module doc must
check its inferred requirements against all six and record any conflict as a §6 gap.
The set is the series briefing's own list, read as constraints
(`briefings/brief-requirements-reverse-analysis.md:125-137`, which introduces them
with "Read these as **constraints on every requirement**, not as context"), plus C6,
which is the premise of the whole series rather than a bullet in that list.

**On ids: module documents in this series cite "§5.1/§5.2" for two of these
constraints** (e.g. `00-module-map.md` M13, "the standing constraints §5.1/§5.2").
There are no numbered subsections in this chapter — **`§5.1` means C1 (one static
binary, zero deps) and `§5.2` means C2 (the operator controls the whole thing)**.
The mapping is stated here because a reader following a module document's
cross-reference would otherwise fail to find it, which is the same class of defect as
the `M1`/`X1` collision recorded in §7.1.

**C1 — One static binary, zero dependencies, no plugin architecture.**
Evidence: `devdoc/roadmap.md:64` ("CGO_ENABLED=0: 始终编译为静态二进制文件"),
`:5` ("静态单二进制"), `:60` ("最小化、无花哨功能"), and the operator's "最小化、无花哨功能";
the briefing's own wording is *"`gopi` is one static binary; no plugin ecosystem,
deliberately ('anti-trendy: stick to single binary')"*
(`briefings/brief-requirements-reverse-analysis.md:125`); the agent prompt
carries it as a rule (`internal/agent/loop.go:895`, "7. 依赖预算: preflight the
dependency need — ZERO-DEP default", pinned by the assertion at
`internal/agent/prompt_test.go:166`). Today `go.mod` has exactly one direct
dependency (`modernc.org/sqlite v1.44.0`, pure-Go). **Bounding consequence:** a need
satisfiable only by a plugin system is *out of scope by construction*. The one live
violation-shaped thing is the websearch plugin (`internal/tools/websearch.go:22-33`,
"routes to the gopi websearch plugin scripts"), whose fetching logic lives in a
deployed Python tree excluded from the script gate
(`internal/testutil/scripts_gate_test.go:580`: "plugins/ is deliberately NOT here").
Whether that is a precedent or a wart is a **§6 `never decided` finding for the
tools/routing module** — not something to answer here.

**C2 — The operator must be able to control the whole thing.**
Evidence: ledger `t-1788383728` (a `source=human` row filed by the operator —
"FEATURE: rebuild project binary after successful merge round …"), whose requirement
text carries the operator verbatim *"this is worth and necessary — this is the
logic"*; the positive half in the operator's own words is the briefing's
`briefings/brief-requirements-reverse-analysis.md:128`: *"我想能够掌控这一切，而不是
一个个独立的工具，虽然每个工具都很棒，但集成运行有障碍有成本"*. Bounding consequence:
coherent integration is preferred over best-of-breed silos, and an action the
operator cannot observe or reverse is a defect. (Corollary and a candidate finding:
the phrase "one place he understands" is the *agent's* paraphrase of this row, not
the operator's words — flag any requirement resting on it.)

**C3 — A daemon must be simple: every branch of logic multiplies its failure surface.**
Evidence: ledger `01a0a995-8742-77fc-a164-53054ea652c5`, operator verbatim:
*"A daemon must be simple. Every branch of logic it carries multiplies its failure
surface. The merger's logic itself is the defect — not any one bug in it."*
Bounding consequence: for a daemon, "do less" beats "handle more cases"; a proposed
requirement that adds a branch to a daemon must justify the branch against this rule.
(Related, same operator: *"限制优于清理"* — limit over clean up — `01a08da4-15…`.)

**C4 — The agent must clean up after itself; dirty is a violation, not a condition to tolerate.**
Evidence: ledger `01a0975a-3e3f-7ae5-a848-a98cd24d6dc3`, operator verbatim:
*"is gopi clean up after development? … these are serious questions to ask"* (asked about
worktrees in `/dev/shm`). The repo's answer so far is a *bounded sweep* — age + holder,
never a name pattern, never a global scan, fail-open, switchable — plus a blast-radius
guard (`internal/tools/path_policy.go:210` `rejectOutsideDeclaredRoots`, pinned by
`internal/tools/bash_path_policy_test.go:42,70`). Bounding consequence: "leave it for
the human to clean" is not an acceptable requirement; a cleanup need must name its
bound and its refusal behaviour. **This constraint is the one that makes the §1
worked example decidable**: under C4 the residue was always gopi's own debt, which is
exactly why the push (harmless) should have run and the removal (destructive) should
have deferred.

**C5 — A small team with limited resources; solutions one person can run.**
Evidence: ledger `01a08b9a-e5…`, operator verbatim: *"我们不是一个公司，资源是很有限的。
能做出一些有用的小工具就可以满足了"*; `01a08b92-76…`: *"我们不要过度期待我们能够做大而全的，
我们只是需要先从整个流程里发现适合我们个人创业能够有独特贡献的小领域"*; the briefing's
own phrasing at `briefings/brief-requirements-reverse-analysis.md:135`. Bounding
consequence: cost and cognitive budget are requirements. The named symptom to measure
is the CLI surface, and **the first version of this document got its size wrong**:
`cmd/agent/cli_*.go` is **45** files (`ls cmd/agent/cli_*.go | grep -v _test | wc -l`),
not 47; the per-verb registry is `cmd/agent/cli_verbs.go` (164 lines, Rule 1's
single source of truth for top-level verbs); and the "65 verb-shaped literals"
figure does not survive re-measurement — `grep -rhoE '"[a-z-]+"' cmd/agent/*.go`
over all 78 non-test files returns **517 distinct literals** (367 over the `cli_*.go`
files alone), which is mostly flag and value strings, not verbs. **A count of verbs
must count the registry (`internal/taskverbs`/`cli_verbs.go`) and the subcommand
predicates, not string literals** — the C5 question ("can one person hold this
surface?") must be answered from the registry, because the literal count is not a
measurement of it. Rejecting a stale number is part of the standard (§6.3).

**C6 — `why` over `what`: the project must be able to say what a feature is FOR.**
Evidence: the operator's diagnosis itself (§1, quoted verbatim), plus the briefing's
own single-line statement of it (`briefings/brief-requirements-reverse-analysis.md:137`,
"**`why` over `what`** — this task"). Bounding consequence: **this is the only
constraint that can fail the way the operator described** — a feature can satisfy
C1–C6 and still be unmaintainable because nobody wrote down its purpose. It is the
reason §3 exists, the reason §4's §1 forbids code identifiers, and the reason a
document that only restates the code is graded a failure no matter how accurate it is.
The worked instance is §1's: a green assertion (`task_finish_cleanup_test.go:202`)
that encodes a need nobody holds, which is exactly what "the programmer cannot tell
a bug from allowed behaviour" looks like in a build log.

## 6. The evidence canon

### 6.1 Primary source: the `source=human` ledger rows

`~/.gopi/memory.db` is the closest thing this project has to a user-need record: an
operator-typed row is the only place a need may appear **in the operator's own words**.
Measured state — **re-measured 2026-09-17 on `9ce8e159`; the previous pass's numbers
are kept in the right-hand column so the reader can see the drift rather than trust a
stale table**:

| measure | now | first version (2026-09-16) |
|---|---|---|
| ledger rows, all sources (`select count(*) from tasks`) | **1,907** | 1,607 |
| rows `source='human'` | **608** | 630 |
| of those, telemetry (`summary like 'gopi run …'`) — **no need content** | **69** | 70 |
| substantive rows (human minus telemetry) | **539** | 560 |
| rows with a real payload (`length(payload)>200`), all human rows | **556** | 551 ← *this is the briefing's "551"* |
| substantive rows that are **summary-only** (`length(payload)<=200 or payload=''`) | **48** | — (not measured) |
| of those, the `[M4.5 补充] … (nick 终审: …)` family | **9** | 9 |
| rows carrying an operator quote marker (`nick 原话/终审/拍板/明确要求/同意/裁决`) | **12** | 9 |
| FTS index rows (`select count(*) from tasks_fts`) | **1,907** | — |

The counts *fall* as the ledger ages because rows are archived and reclaimed, not
because the operator spoke less — which is the reason the standard is "re-measure in
your own session", not "quote the brief".

**Two numbers, reconciled.** The briefing says "551 `source=human` rows are the
evidence canon". 551 is exactly the count at `length(payload)>200` (556 today). It is
a good working set, but it **excludes ~48 substantive rows that have no payload and
carry the requirement in the summary** — every one of the 2026-08-29
`[M4.5 补充] … (nick 终审: …)` rows, e.g.:

```
t-1788039140 … task update 收窄为 status-only（nick 终审：payload/summary 全不可变——a job is just a job）
t-1788037892 … task new 彻底移除（nick 终审：shell 层不接受此接口）
```

Those summary-only rows **outrank** almost everything in the 551 and are trivially
missed. **Use 560 (all non-telemetry rows), query on the summary too** — that is the
correction this document contributes to the mining method.

**The mining query (run this; it is the required first step of every module doc):**

```sql
-- set A: substantive payloads (the briefing's 551)
SELECT task_id, summary, payload
  FROM tasks
 WHERE source = 'human'
   AND length(payload) > 200
 ORDER BY created_at;

-- set B: THE ONE-LINE BRIEFS — summary-only rows that carry the *final* decision.
--         these must never be lost behind a payload-length filter.
SELECT task_id, datetime(created_at,'unixepoch') AS ts, summary
  FROM tasks
 WHERE source = 'human'
   AND summary NOT LIKE 'gopi run %'
   AND (length(payload) <= 200 OR payload = '')
 ORDER BY created_at;

-- set C: verbatim operator statements anywhere in the row (quote these; the
--        12-row marker count above understates it, since the marker is not fixed).
SELECT task_id, summary, payload
  FROM tasks
 WHERE source = 'human'
   AND (payload LIKE '%nick 原话%' OR payload LIKE '%nick 终审%'
     OR payload LIKE '%nick 拍板%' OR payload LIKE '%nick 明确要求%'
     OR payload LIKE '%nick 同意%'   OR payload LIKE '%nick 裁决%'
     OR payload LIKE '%不采用%'      OR payload LIKE '%我们不%'
     OR payload LIKE '%I want%');

-- set D: full-text, when the module has a vocabulary of its own (fts5 is live;
--         the index carries 1,907 rows at this revision)
SELECT t.task_id, substr(t.summary,1,90)
  FROM tasks_fts f JOIN tasks t ON t.id = f.rowid
 WHERE tasks_fts MATCH '<module keywords>' AND t.source = 'human';
```

Then narrow by the module's own vocabulary (e.g. a worktree module greps
`worktree|dirty|residue|clean`). Prefer `t-<unix>` ids for rows before 2026-09-09 and
UUID ids after — **both id shapes exist in the same table**; a grep for one silently
misses the other.

**Two operational traps, both hit in this session.** (a) The ledger is a live SQLite
database with concurrent writers: `Error: database is locked (5)` is an ordinary
answer, so a mining query retries rather than reporting a number it could not
produce. (b) `~/.gopi/sessions/` is large enough that `ls -d ~/.gopi/sessions/*/`
can fail with "Argument list too long" — use `find ~/.gopi/sessions -maxdepth 1 -type d | wc -l`,
and treat any session-log count as a **sample** (this session's 300-newest-log sample
is 29,000+ tool calls), never as an audit.

### 6.2 Secondary sources (use in this order)

1. **`briefings/*.md`** — 154 files on master `9ce8e159` (`ls briefings/*.md | wc -l`,
   re-measured 2026-09-17). A brief *is* the operator's dispatch text and often quotes
   him verbatim; the series' own brief is
   `briefings/brief-requirements-reverse-analysis.md` (204 lines) and it is the single
   richest source in the repo for this series — §1's verbatim diagnosis, the six
   constraints and the 8-section shape all come from it.
2. **`GOPI.md`** — **30** numbered rules in 1,187 lines (re-measured; Rules 16–21 are
   `### Rule N —` subsections, so count with `grep -cE '^#{2,3} Rule [0-9]+' GOPI.md`).
   Each rule's own "Why:" line is a recorded intent, and a rule is a *decided
   constraint* (e.g. Rule 23, `GOPI.md:777`: never continue on someone else's failed
   branch; Rule 28, `GOPI.md:1035`: two hosts, two jobs; Rule 29, `GOPI.md:1078`: the
   main repo hosts work in progress; Rule 30, `GOPI.md:1120`: a run leaves its
   worktree clean). These are requirements even though they were written as
   engineering discipline.
3. **`devdoc/roadmap.md`** — the founding document (2026-08-01): goal at `:5`,
   constraints at `:56-64`. It is where the project's first requirements live.
4. **Commit messages** — the body carries intent the code cannot show.
5. **Intent-stating comments** — the site's own "why", e.g. `task_finish_cleanup.go:3-18`.
   Trust these more than the code shape, less than an operator row.

### 6.3 Citation and labelling standard

Every claim carries one of: a ledger task id, `path:line`, a commit SHA, or a
`GOPI.md` rule number. **A claim with none of these must be prefixed `[opinion]`.**
An inference carries `[inferred]` **and names its weakest link**. This is not
decoration: the difference between a module doc and the 1,960 devdoc files is that
this one may be trusted as a *need* without re-reading the code.

**Two rules that keep citations true as the tree moves** (both learned from this
document's own drift, §0):

1. **Pin the revision.** Every `file:line` is only meaningful with the revision it was
   read at, so each document states its master SHA in the front matter and a line
   number is checked against *that* revision. The module documents in this series pin
   `17cec86b` or `bf4d1941`; this umbrella pins `9ce8e159`. A number quoted without a
   revision (the first version of this document did this) silently rots.
2. **Re-measure, never restate.** A measured number is copied only with the command
   that produces it, run in the session that writes it. Two concrete failures this
   rule caught here: `cmd/agent/cli_*.go` is 45 files, not the "47" of the first
   version; and GOPI.md now carries **30** numbered rules (1,187 lines), not "29" —
   Rules 16–21 are written as `### Rule N —` subsections, so a `^## Rule` count must
   use `grep -cE '^#{2,3} Rule [0-9]+'`. **A rule number cited from memory is a
   citation class of its own only if it resolves** — `GOPI.md:1002` is a line in the
   module map that points at Rule 28's line, `GOPI.md:1035`.

## 7. The modules — boundary map

**The rule: one module = one user-visible responsibility.** Not a directory, not a
package, not a file. A boundary is right when (a) a user can state a need that only
touches it, and (b) all of its implementation can be re-written without changing any
other module's need statement.

### 7.1 Where directory and responsibility disagree (named, as required)

These are **not** boundary choices — they are the mismatches that make a
directory-shaped decomposition wrong, and they must be stated in the affected
documents. **They are numbered independently of the module ids in §7.2/§7.3** —
the first version of this document numbered its *boundaries* `B1…B12` and its
*mismatches* `M1…M5`, and the module map numbered its *boundaries* `M1…M15`, so
"mismatch M1" and "module M1" collide in the same paragraph. The split is recorded
in §7.4; **the letter is what disambiguates: `X<n>` is always a mismatch, `M<n>` is
always a module (or `B<n>` a legacy module).**

| # | mismatch | evidence |
|---|---|---|
| X1 | `internal/tools` is not "the tools". It holds the agent's tool implementations **and** the task ledger, dispatch/claim, verification, the merger's git execution, worktree lifecycle, sync, and the CLI's own verbs. | **106** production files, **61,646** lines (`find internal/tools -maxdepth 1 -name '*.go' ! -name '*_test.go' \| wc -l` → 106; `xargs wc -l` → 61646, master `9ce8e159`). The file-level doc comment is `// Package memory — brew pipeline` (`internal/tools/memory_brew.go:1`) — one file's Go doc naming a 106-file package. (The first version said "53,901 non-test LOC in 291 files": the LOC was true of its own revision and the file count appears to have counted test files too. Both corrected here by re-measurement.) |
| X2 | The "memory" tool spans while "memory" the store does not. | `internal/tools/memory*.go` (the tool's face) vs `internal/hippocampus/` (the organ, 1,154 L): one responsibility, two directories |
| X3 | Merging is split across three homes. | `internal/tools/merge_*.go` + `cmd/agent/merger*.go` + the ledger rows a parked branch files. Measured: 8 `internal/tools/merge*.go` + 10 `cmd/agent/merger*.go` non-test files |
| X4 | The CLI is a *surface*, not a module. | **45** non-test `cmd/agent/cli_*.go` files, **33** further non-CLI `cmd/agent/*.go` files (78 production files total), and the verb registry `cmd/agent/cli_verbs.go:6` ("cliVerbs is the single source of truth for the TOP-LEVEL gopi CLI verb words"). The verbs belong to the modules whose needs they expose. The first version's "65 verbs" is a **retired measurement** — see §5 C5 |
| X5 | Gates are infrastructure, not a product surface. | `internal/standards` (**3,015** LOC) + `internal/stylegate` (**1,954** LOC) are how C1/C5 are *enforced* (re-measured; the first version said 4,968 for standards alone) |

### 7.2 The boundaries — **two id schemes exist in this series, and both are live**

The first version of this document proposed **12** boundaries `B1…B12`; the module
map (`00-module-map.md`, and the `8e2fa780` rebuild of *this* file) settled on
**14** modules `M1…M14`, plus `M0` (umbrella) and `M15` (summary). The 9 module
documents on master are written against the **M** scheme. So:

- **The authoritative boundary list is `M1…M14` in `00-module-map.md`** — it is
  package-complete, it names each boundary's reason, and it is what the module
  documents cite. The module map is the reader's single source for *which module owns
  what*.
- **`B1…B12` remains useful as the *dispatch* lineage**: 12 module task rows were
  filed from it (`01a0ab3d-*`) and the module documents record which B-row dispatched
  them. Treat `B<n>` as a historical id, never as an ownership claim.
- **They are not 1:1.** The reconciling table (this file's contribution, so a reader
  never has to diff two documents):

| B (dispatch lineage) | M (canonical boundary) | note |
|---|---|---|
| B1 task ledger & lifecycle | **M1** | same responsibility |
| B2 merge & branch lifecycle | **M2** | same |
| B3 worktree & cwd | **M10** (worktree/cwd/cgroup/scratch/temp root) | M10 is *wider*: it adds cgroup/scratch/temp-root (Rules 22/24/25) |
| B4 sync & node identity | **M3** | same |
| B5 context & memory | **M4** | same |
| B6 agent loop, tasklist & working area | **M5** | same |
| B7 tools & routing | **M6** | same; M13 takes the *environment adapters* (voice/proxy/shot/text2img) that the first version had put in B7 |
| B8 the CLI surface | **M7** | same |
| B9 chat & interactive UI | **M8** | same |
| B10 notifications & observability | **M11** | same |
| B11 build & repo declaration | **M12** | same |
| B12 verification & gates | **M9** | same |
| — (no B) | **M13** environment adapters | B-scheme had these inside B7 |
| — (no B) | **M14** cross-cutting obligations | B-scheme spread these over B1/B6/B10 |

### 7.3 Coverage — every package and every surface, and what the assignment means here

The canonical, both-directions table lives in `00-module-map.md` §2 (43 `internal/`
packages + every `cmd/agent` surface + 3 `cmd/` dirs). **Three things this section
adds to it**, because the first version's coverage claim was not checkable:

1. **The enumeration is the acceptance test, and it must be run at this revision.**
   `ls -d internal/*/` → **43** packages; `ls -d cmd/*/` → **3** directories
   (`cmd/agent`, `cmd/ledger-window`, `cmd/text2img-verify`). The first version's
   table was complete against the intermediate `M`-numbered blocks of §7.1 but its
   prose walk said "40 packages" — true of master `17cec86b`, **stale today**.
2. **Packages that did not exist when the map was written** — `internal/ledgerwindow`
   (335 L), `internal/smoke` (375 L), `internal/smokegate` (82 L) — are assigned here
   for the first time, all three to **M9** (verification & gates): they exist to make
   the merge/verify path's smoke command one definition with one shape
   (`internal/smokegate/smokegate.go:1-12`), and `cmd/ledger-window` is the shell-side
   entry point to `internal/ledgerwindow`'s single window conversion
   (`cmd/ledger-window/main.go:1-8`), so **M1** consumes it (a ledger surface) while
   M9 owns the gate that pins it. `internal/textguard` → **M13**.
3. **`internal/tools` is deliberately in five modules** (M1/M4/M6/M7/M8) and the
   claim "every package is in exactly one module" is therefore **at package
   granularity, not file granularity** for this one package. That is why §4.3 requires
   a worker to name each file it covers: the package-level assignment is coarse by
   construction and the file-level lists are what make it checkable.

**`cmd/agent` surfaces = 78 production files = 45 `cli_*.go` + 33 others.** The
non-CLI files are the surfaces that are *not* verb dispatchers — daemons
(`merger_loop.go`, `merger_tick.go`, `task_lifecycle_tick.go`, `scratch_gc_tick.go`),
child management (`child_registry.go`, `child_stop.go`, `task_child_env.go`,
`task_child_fail_guard.go`), the session/cwd plumbing (`task_session_cwd.go`,
`tmpdir_inject.go`, `lineedit_wire.go`, `inputtext.go`) and the takeover machinery
(`merger_takeover*.go`, 7 files). Each is assigned in the map's coverage table; none
is left to "the CLI module by default".

**Directory-vs-workflow mismatches X1–X5 must be restated in the affected document's
"Boundary why" front matter** — a reader of M1 must learn that `internal/tools` also
contains M2's executor, without leaving M1's document.

### 7.4 Contradictions between this umbrella and the module documents, recorded

The 9 module documents on master were written while this file's text was still the
`12e1fa74`-era version. Three of them record the series' section numbering or the map
by hand, and the numbers they printed no longer match the tables above. **None of
these is a defect in the module document** — each is a fact about the revision it
read, and the module document governs its own module's content:

1. `03-worktree-and-cwd.md:3` says the evidence tokens follow "`00-method-and-standards.md`
   §1 (E1 … E6, I, O)" — **that evidence-class list is not in this file at any
   revision.** This umbrella's evidence classes are §6.2's ranked sources and §6.3's
   citation tokens; the E1–E6/I/O taxonomy is the *briefing's* vocabulary, which the
   module documents invented independently. A reader must treat the two as the same
   idea expressed twice, not as one list defined here.
2. `04-sync-and-nodes.md:41,44` cites "umbrella mismatch **M1**" and "**(M4)**" for
   X1 and X4 — the collision §7.1 names.
3. `01-task-ledger.md:127` cites "the **M1 mismatch** in miniature" for X1 again.
4. `02-merge-and-branches.md:40` cites "umbrella map's **mismatch M3**" for X3.
5. `04-sync-and-nodes.md` also reports the umbrella's own id as `8e2fa780`/`a2d1af6d`,
   i.e. the rebuilt text, while `01-task-ledger.md`/`07-tools-and-routing.md` cite the
   `12e1ac74` era. Both are true of their own reads; the module documents pin their
   master revision in front matter, and this file pins `9ce8e159`.

**The repair chosen here is the cheap one and it is deliberate:** the umbrella gains
the X/M distinction and this contradiction list, and **no module document is edited**.
Rewriting nine merged documents to renumber their cross-references would be a much
larger change than the acceptance asks for, and it would destroy the record of what
each worker read.

## 8. Honesty rules

1. **Never invent a requirement to fill a section.** An empty section says so.
   "The intent is not recorded anywhere I can find; the weakest link is X" is a
   **finding** and outranks a plausible reconstruction.
2. **A named unknown beats a confident wrong requirement.** §8 of every module doc
   exists so a reader can tell which claims to re-check. A confident wrong
   requirement is worse than no requirement: it will be coded, tested, and defended.
3. **Mark inference as inference**, with its weakest link named in the same breath.
4. **Citation or `[opinion]`** — no third option (§6.3).
5. **Never overwrite a requirement the operator stated verbatim.** If the code
   disagrees with a verbatim statement, that is a §6 gap to file, never a
   requirement to rewrite.
6. **Quote, don't characterise.** When an operator row is the evidence, quote it in
   the original language and give the task id. A paraphrase is an inference and must
   be labelled one — the brief's own "one place he understands" is exactly this
   failure mode (§5 C2).
7. **No tests written, no code changed** by any document in this series. §7 of a
   module doc is a *judgement about existing tests*, delivered as prose.
8. **A gap with no owner is still a gap.** Do not soften a finding to keep a module
   looking coherent; the series' value is precisely its negative results.

## 9. Dispatch and non-duplication

**Three series for this epic exist in the ledger** (`sqlite3 ~/.gopi/memory.db "select substr(task_id,1,8),status,substr(summary,1,70) from tasks where summary like '%REQUIREMENT%'"`,
re-measured 2026-09-17):

| series | rows | state | what it produced |
|---|---|---|---|
| `01a0aac3` / `01a0aac4` (`REQUIREMENTS [0/11]`…`[2/11]`) | 3 | **all `cancelled`** | nothing — no document, no note anywhere |
| **`01a0ab11`** — the 12-boundary series | 12 modules | **11 modules `completed`**, `B12` (`01a0ab3f-7069…`) still **`pending`** | the 9 module documents on master today |
| **`01a0ab12`** — the 15-row series whose umbrella ordered *this* document | 15 | umbrella `in_progress`; 13 module children **`pending`**; 1 evidence-index child `pending` | **the two umbrella documents** (`12e1ac74`, rebuilt `8e2fa780`, + `a2d1af6d`) |

The earlier reading "**Both series are `pending`**" (first version) is **stale, and in
the good direction**: the `01a0ab11` module rows ran and their documents are merged,
while the `01a0ab12` children have not started. **The two are therefore not duplicate
work to cancel — they are the same method applied at two moments**, and
`01-per-module-briefings.md` (`[1/14]…[14/14]`) tells each worker to *judge* an
existing sibling document rather than re-write it. That is what the executed workers
did: each module document carries an "Overlap" line naming its sibling row.

**Four merge-conflict rows are `cancelled`** — `01a0ab1c` (`docs/requirements-umbrella`),
`01a0ab49` (`feat/requirements-umbrella`), `01a0ab57`
(`docs/requirements-merge-branch-lifecycle`), `01a0ab70` (`docs/requirements-task-ledger`).
That is the mechanical reason the umbrella text reached master through a *rebuild*
commit (`8e2fa780`) instead of the branch's own history, and why §0 has a provenance
table: **the branch that first produced this text no longer exists under that name**
(`git branch -a --list '*requirement*'` returns nothing today). **The 12 module tasks dispatched from this document carry
the standards INLINE in their payloads** (the acceptance property of this task): a
worker that never sees this umbrella can produce a conforming document, and a module
already produced by the sibling series is judged complete by the executor rather than
re-done. Each module payload names its sibling task id so the executor can read the
overlap and decide for itself.

Per-module brief, one per B1–B12, self-contained: front matter (in/out of scope,
boundary why), the 8-section table, §3.1–§3.3 verbatim, the six constraints C1–C6,
the mining query from §6.1 with the substantive-row correction, the honesty rules §8,
the named mismatches (X1–X5) that touch the boundary, and its own acceptance line.
**The M-scheme equivalent — what a worker actually needs — is §4.2's template.**

The 12 dispatches the first version of this document made (id, boundary, status
re-measured 2026-09-17):

| boundary (B-scheme) | ledger task id | status |
|---|---|---|
| B1 task ledger & lifecycle | `01a0ab3d-1e94-7590-bb98-e6fe771f5403` | completed |
| B2 merge & branch lifecycle | `01a0ab3d-4c03-7a5f-9a55-8c2671551957` | completed |
| B3 worktree & cwd | `01a0ab3d-7e16-7405-a31d-c82f57c7c528` | completed |
| B4 sync & node identity | `01a0ab3d-d541-7b98-98b6-b715334250ca` | completed |
| B5 context & memory | `01a0ab3e-02ef-7540-921e-baa5159087e8` | completed |
| B6 agent loop & tasklist | `01a0ab3e-3057-76c4-b6ee-a493a95cedfc` | completed |
| B7 tools & routing | `01a0ab3e-652f-74e4-b9f8-0bbf79b0122e` | completed |
| B8 CLI surface | `01a0ab3e-94f9-7bb4-bf34-767ae30add9e` | completed |
| B9 chat & interactive UI | `01a0ab3e-c46a-7685-a7af-ae092ee122af` | completed |
| B10 notifications & observability | `01a0ab3e-f98d-7164-aea2-d265fbac7e40` | completed |
| B11 build & repo declaration | `01a0ab3f-2c2c-7d55-9cca-39a52063af90` | completed |
| B12 verification & gates | `01a0ab3f-7069-7ea5-ab41-068cc6c03248` | **pending** ← the one still open |

**The M-scheme's 13 module children under `01a0ab12`** (`…-299e`, `-2d2d`, `-3115`,
`-35e7`, `-38ae`, `-3e7d`, `-5113`, `-5b52`, `-62a1`, `-7761`, `-7bcc`, `-7cf9`,
`-7dbe`) are **all still `pending`**. A worker picking one up reads the matching
B-series document first and decides: judge it complete, or repair the part that fails
its acceptance. **That instruction — not a re-dispatch — is the non-duplication
mechanism**, and it is why §7.4's contradiction list exists.

## 10. What this document adds that the operator did not already know

Findings this analysis produced, each checkable:

1. **The briefing's 551-row canon is a payload-length artifact, and it hides the
   sharpest evidence.** The `source=human` set is 608 rows; the 551 figure is exactly
   `length(payload)>200`, which **excludes 48 substantive rows whose requirement lives
   in the summary** — among them the nine `[M4.5 补充] … (nick 终审: …)` rows carrying
   final decisions (`t-1788039140` "a job is just a job", `t-1788037892` "shell 层不接受
   此接口"). A payload-length filter loses them. **The mining query now reads the
   summary too.**
2. **The gopi prohibition is phrased as a *negative, logical* constraint, not vendor
   aversion.** The anti-plugin clause in the agent's own prompt is
   `internal/agent/loop.go:895`, "依赖预算: preflight the dependency need — ZERO-DEP
   default", pinned at `internal/agent/prompt_test.go:166`. It is a **preflight duty**
   ("assess before adding"), weaker than the absolute "zero deps" reading a module
   author would otherwise assume. The absolute reading comes from C1's other citations
   (`devdoc/roadmap.md:5,60,64`).
3. **There is already a blast-radius guard, and it postdates the operator's question
   by a day.** The `/dev/shm` worktree concern (`01a0975a-…`, 2026-09-12) is answered
   in code by `internal/tools/path_policy.go:210` + `bash_path_policy_test.go:42,70`.
   Any module doc on cleanup must not re-raise it as an open gap — it is closed, and
   the closure is *the mechanism C4 names*.
4. **The series was rebuilt mid-flight, and the two umbrella texts collide on one
   letter.** This file's §7.1 first numbered *mismatches* `M1…M5` while
   `00-module-map.md` numbers *modules* `M1…M15`, and three module documents
   (`01-task-ledger.md:127`, `02-merge-and-branches.md:40`, `04-sync-and-nodes.md:41,44`)
   cite the colliding ids. §7.4 records every such contradiction and the repair chosen
   (rename the mismatches to `X1…X5`; edit no module document). **Cost of not
   knowing: a reader follows "M1" into the wrong table and concludes the map says
   something it does not.**
5. **"One person can run this" (C5) is enforced today only through the gates.**
   `internal/standards` (3,015 LOC) + `internal/stylegate` (1,954 LOC) are the only
   place C1/C5 become enforceable; no product surface measures them. That is why the
   gates are a boundary (M9) and not a footnote — and it is why §5 C5's **retired
   measurement** matters: the first version's "65 verbs" was a literal count that no
   longer reproduces (517 distinct `"[a-z-]+"` literals over 78 files, mostly flags and
   values), so the C5 question has **no honest current instrument** and the CLI module
   must build one from `cli_verbs.go` + `internal/taskverbs`.
6. **The coverage proof had drifted by five package-level entries.** Three packages
   added to master since the map was drawn — `internal/ledgerwindow`, `internal/smoke`,
   `internal/smokegate` — plus `cmd/ledger-window/` and `internal/textguard` appeared
   in **no** requirements document (`grep -rln 'ledgerwindow\|smokegate\|internal/smoke'
   devdoc/requirements/` → empty). A coverage claim that is not re-enumerated at the
   branch's revision is a claim about a revision, not about the map.
7. **Nine of the twelve B-series module documents are merged, and the twelfth is
   still open.** `01a0ab3f-7069…` (B12 verification & gates) is the only `pending` row
   of the twelve; the 13 `01a0ab12` module children have not started. The series is
   therefore **nearly complete and not cancelled** — a fact the first version could
   not state because it read the ledger one revision earlier.

## 11. What I could not determine

Honest unknowns — each is a dispatchable question, not a gap I have filled by guessing.

1. **Whether the operator's vision has a single canonical statement.** The five
   constraints are recovered from six different rows (`01a0a995-…`, `01a0975a-…`,
   `01a08b9a-e5…`, `01a08b92-76…`, `t-1788383728`, plus `devdoc/roadmap.md`). I found
   no *one* document the operator wrote stating the vision. **Weakest link: the
   constraints are a corpus reconstruction, and a future row may contradict one.**
2. **The "one person can hold it" threshold is unquantified.** C5 gives the direction
   and the 65-verb measurement; it does not give the operator's number. The CLI module
   must measure and *ask*, not assume.
3. **Which of `01a0ab11` / `01a0ab12` is the live series** — a ledger/PA decision I
   cannot make from the evidence (§9).
4. **The websearch plugin's status** — sanctioned exception or accumulated debt? The
   B7 module (`07-tools-and-routing.md`) is the place this is decided; this umbrella
   still does not resolve it. Evidence points both ways (`websearch.go:22-33` treats it
   as deployed; the script gate deliberately excludes it,
   `internal/testutil/scripts_gate_test.go:580`).
5. **Whether the merger redesign (`01a0a995-…`) fully landed.** Re-checked this pass:
   the row is `completed` ("redesign the merger: ff-only merge, park-and-file the
   conflict as an ordinary task"), `awaitChildExit` survives only in
   `cmd/agent/child_stop.go:115,129` where it is described as "THE bounded wait on a
   spawned merger child, in one place", and `cmd/agent/merger_tick_no_child_gate_test.go:146`
   names it as "the retired child-wait ladder". That is consistent with the redesign —
   but I still did not execute the merger's tick, so C3's *behavioural* status is
   **unverified by me**.
6. **The exact intent behind the `archive/` namespace.** Multiple rows reference it
   (`repoSweepArchiveBranch`, Rule 9, Rule 23) with subtly different meanings (loser
   archive vs park vs tombstone), and `00-module-map.md`'s M2 entry already records a
   **withdrawal** of one of them (`devdoc/branch-lifecycle-ruling.md` §1, task
   `01a0af2f`: the rename is void; the lifecycle's endpoint is deletion). Which
   meaning is the live requirement is M2's job, and the branch-lifecycle module
   document is where it must be settled.
7. **`internal/tools`'s own package doc is wrong** (`// Package memory — brew
   pipeline` at the top of a 106-file package, `internal/tools/memory_brew.go:1`).
   Whether that is a defect worth a task, or a symptom of X1, is not decided here.

## Verification

- **Deliverable:** this file, on **branch `docs/req-umbrella-verify`** in worktree
  `~/workspace/gopi-req-umbrella-verify`, created from master `9ce8e159`
  (`git worktree add ~/workspace/gopi-req-umbrella-verify -b docs/req-umbrella-verify`,
  Rule 6a).
- **No code changed:** measured against **this branch's own base** `9ce8e159`, not
  against a moving `master` — `git diff --name-only 9ce8e159..HEAD` lists exactly the
  two `devdoc/requirements/00-*.md` files and nothing else, and `git status
  --porcelain` in the worktree is empty (Rule 30). **The form matters**: this branch
  was cut from `9ce8e159` and `master` advanced to `a9c20fa3` *during* this session
  (a sibling merge: `docs(merge): file the table defect under the review section`),
  so `git diff --name-only master..HEAD` now lists that sibling's 15 files too. That
  is the same revision-pinning defect §6.3 warns about, met live; the base of record
  is the commit the branch was cut from.
- **What this pass changed and what it deliberately did not.** Changed in this file:
  a provenance section (§0), the operator's verbatim diagnosis (§1), the dispatch
  template (§4.2), the six constraints with C6 added and every stale number corrected
  (§5), the mining query's counts re-measured (§6.1), the citation standard's
  revision rule (§6.3), the `X1…X5` mismatch rename plus the `B→M` reconciler and the
  contradiction list (§7), the measured dispatch state (§9), and the findings (§10).
  **Not changed: the nine module documents, `01-per-module-briefings.md` and
  `README.md`** — they are the series' product and are judged, not rewritten (§7.4).
- **Every number in this file was produced by a command run in this session**
  (2026-09-17, master `9ce8e159`): 1,960 devdoc files; 43 `internal/` packages; 3
  `cmd/` dirs; 78 `cmd/agent` production files (45 `cli_*` + 33 others); 160,428
  production LOC / 203,191 test LOC; `internal/tools` 106 files / 61,646 LOC;
  `internal/standards` 3,015 LOC; `internal/stylegate` 1,954 LOC; ledger 1,907 rows
  (608 human / 69 telemetry / 539 substantive / 556 payload>200 / 48 summary-only /
  12 marker rows); `tasks_fts` 1,907 rows; `GOPI.md` 1,187 lines / 30 rules; 154
  briefings. Each is re-runnable from the text of the sentence that uses it.
- **Build/tests deliberately not run:** no code touched, and no test yet exists for a
  requirement document (Rule 5's deliverable for a docs task is the document; Rule 27
  is trivially satisfied — this task performed no device load).
