<!-- doc-class: normative -->
# Reverse Requirement Analysis — M12: Verification and Gates

Module `B12` / `M9` of the reverse-requirement series (`devdoc/requirements/*.md`).
Produced under `00-method-and-standards.md` (method, §3 why-test, §4 8-section shape,
§5 constraints C1–C6, §6 evidence canon, §8 honesty rules).

```
Boundary:      the machinery of trust — the arrangements that let one person accept
               a change he did not read, and the decision (for each standard) of
               whether it is computed or merely written down.
In scope:      the check registry and its exemption machinery; the census the
               operator can read; the classification that keeps expensive checks
               off the merge-decision path; the code-side check that pins the
               rule file against the tool surface.
               Evidence paths: internal/standards/, internal/stylegate/,
               internal/smokegate/, internal/smoke/, internal/ledgerwindow/,
               internal/heavytest/, internal/testutil (the check-bearing parts
               only), testdata/heavy-tests.tsv, testdata/flakes.tsv,
               GOPI.md (Rules 16–21, 27 and the "Machine-Checkable Gates" chapter),
               devdoc/standards-gate.md, devdoc/heavy-tests-rule27-gate.md.
Out of scope:  the CONTENT of any check — what each one verifies is the owning
               module's business (temp roots → M10, the %w family → the coding
               standards, worktree residue → M10). Also out of scope: the test
               SUITE's composition and cost (M12 owns "is the cheap check the one
               that decides a merge", not "which tests exist"); the merge policy
               itself (M2); the CLI surface that starts a run (M7).
Boundary why:  this is a user-visible responsibility, not a directory, because the
               operator holds a need that no directory owns: "I must be able to
               accept a change without reading every line of it." Every other
               module's needs are downstream of that one — a standard nobody
               enforces is a standard the next agent forgets. The directory
               `internal/standards/` is where ONE implementation of that need
               lives today; the need does not shrink to it, which is exactly the
               finding §6.2 records.
```

**Revision pinned.** Every `file:line` below was read at master
`90e664e04977a353cea605e7b571e4275db4b563` (`git rev-parse HEAD`) unless a sentence
names a different revision. Working tree: `~/workspace/gopi-req-b12`, branch
`feat/req-b12-verification-gates`.

---

## §1 What this is for

*(This section describes the need in product-owner language. It names no package,
file, function, flag or type — the evidence for every sentence is in §4, the
acceptance in §5.)*

Everyone who writes down a rule already knows the same uncomfortable thing: **a rule
that is only written down is a rule that will be broken again, by a careful person
who did not read it.** This module is the project's answer to that. It is not about
any one rule. It is about the arrangements that make a rule *checkable*, and about
who is allowed to look at the result.

Four promises are made here, and they are made to four different people.

**To the person who has to accept a change he did not read.** He is promised that a
class of mistakes is caught before he ever sees the change, and that the catching
does not depend on anyone remembering to run anything. He is promised something
harder too: that the catching is **honest about its own exceptions**. When a check
finds a problem the team has decided to live with, that decision is written on the
exact line of the exact file, with the reason, and it keeps appearing in a report he
can ask for at any time. Nothing is silenced quietly.

**To the person who is checked.** He is promised that a check will tell him *what to
do instead*, not only that he failed; that a check will not forbid something the
team actually wants done; and that when he legitimately needs an exception, the way
to take it is a visible, reviewed sentence next to his code — not a disappearing act
in a configuration file that the next reader cannot see.

**To the person who pays.** He is promised that checking costs nothing he has to
import, install, licence or babysit: the checks run as part of the ordinary test run
he was already doing, using only the standard toolchain. And he is promised that the
expensive checks — the slow, device-hungry, minutes-long ones — are classified and
kept off the path that decides whether work may land, because a check nobody dares to
wait for is a check nobody runs.

**To the future maintainer.** He is promised the record of what was *rejected on
purpose*. The most useful line in this whole area is not a check — it is the sentence
saying "we decided not to check that, and here is why". Without it, the next agent
re-proposes the rejected idea as an improvement, and the work of deciding is thrown
away and done again.

The one-sentence version, and it is the operator's own:

> **A principle changes nobody's behaviour. A check does.**

Everything in this module follows from taking that sentence seriously — including the
unpleasant consequence that the project must be willing to *measure how much of its
own rule book is still only a principle*. That measurement is §6.2, and it is the most
important number in this document.

---

## §2 Who is served

| actor | the distinct need it has here | where it shows up |
|---|---|---|
| **the operator (the operator)** — a single person who must accept work he did not read | to know that a decided standard cannot silently lapse, and to be able to *see* every deliberate exception rather than trust that none exists | the check census he can print on demand (§4.4); the recorded rejections (§4.9); the copy budget of the rule file (§6.4 F4) |
| **the agent (Bond)** — the worker that writes the change | to be told, in the run that introduced the problem, which line to fix and which alternative to use — and to have a visible, reviewed way to say "this one is correct, and here is why" | the failure text carries the fixture and the command to re-run (§4.10, §5); the inline escape mechanism (§4.2); the "don't re-add this" clause (§4.9) |
| **the reviewer / verifier** — whoever stamps a change as verified | a cheap, *bounded*, reproducible verdict — not "the whole test suite passed" but "the packages this change touched were exercised, with this command" | the branch-scoped smoke command frozen in one place (§4.7); the merge-verdict language |
| **the server node** — the machine that merges and rebuilds | that the checks run without a human present, inside the same test invocation the machine already runs, and that the checks themselves are never classified as "too expensive to run on the merge path" | the checks are ordinary tests in ordinary packages (§4.1, §4.3); the gate packages are excluded from heavy classification (§6.5 C3) |
| **the future maintainer** — the next agent, with no memory of this one | the reason a check is shaped the way it is, so the rejected alternative is not re-proposed and a red check is fixed rather than baselined | the "deliberately rejected" clause per rule (§4.9); the ratchet rule that makes a stale permit an *error* (§4.12) |

The actors are not served equally well. §6 says which one is currently let down.

---

## §3 The workflow it sits in

**Entry condition.** A change exists in its own working directory, committed to its
own branch, with the working directory reported clean. *(The change's own delivery
rules are M10/M2's business; this module begins when the change is a candidate.)*

**The workflow, end to end.**

1. **The author runs the ordinary test command.** The checks that live in the test
   tree run because they *are* tests — nothing extra to remember.
   `[E: `Makefile:28-29`; `GOPI.md:522` "it runs as part of the module suite"]`
2. **Each check scans its subject and sorts every finding into exactly one of three
   bins:** a known legacy site (permitted, line by line, with a reason),
   a reviewed inline exception (permitted, and *still printed*), or a new violation
   (fails). `[E: `internal/standards/gate.go:536-593`]`
3. **A permit that no longer matches anything is also a failure** — a stale permit is
   treated as a silent permission, so the allow-list can only shrink.
   `[E: `internal/standards/gate.go:562-575`]`
4. **The author either fixes the finding, or takes a reviewed exception, or — only
   for a genuine legacy site — adds a permit with a one-line reason.** The failure
   text hands him the command to re-run. `[E: `internal/standards/gate.go:551-560`]`
5. **The author can print the census** and check that the arithmetic reconciles:
   every finding is either permitted or visible. `[E: §4.4]`
6. **The change is committed.** *(Delivery is M2/M10's business.)*
7. **The merge decision runs the cheap, branch-scoped command** — the packages the
   change touched — never the whole tree. `[E: `internal/smokegate/smokegate.go:44-82`,
   `internal/tools/verify_gate.go:568`]`
8. **Expensive checks are classified** and are skipped unless the operator opts in
   deliberately, off-peak. `[E: `internal/heavytest/heavy.go:122`, `Makefile:35-36`]`

**Exit condition.** The change is either (a) accepted: every check that applies ran
green, every exception is visible in the census, and no permit is stale; or (b)
refused with a message that names the fixture, the line, and the command to reproduce
it. A third exit is possible and is a defect rather than an outcome: a check that is
red on the shared baseline and that nobody has been dispatched to fix — see F1.

---

## §4 Why it exists

Each need below is stated as **need** + **test the user would apply**, then its
evidence. A need with no citation carries `[opinion]`.

### §4.1 The premise, in the operator's own words — a principle changes nobody

The requirement this whole module implements is not an engineering preference; it is
the operator's diagnosis, filed as a ledger row. Quoted verbatim, in the original
language, with its task id:

> **Task `01a08d70-2259-70c7-ac9d-192453cfc51e`** (source `human`, filed 2026-09-10,
> summary: *"Lock down GOPI.md: land the collected coding standards as
> MACHINE-CHECKABLE rules"*):
>
> > "我觉得这个是所有 coding agent 的基本理念，就是一些长期实践总结的代码基本原则：
> > 比如很基本的不要硬编码，要变量不要 magic number，要资源回收等，这些我本来以为
> > LLM 在预训练已经成熟了，没想到还是像初学者。看来我们需要把 google 的编码原则
> > 找来给所有的 gopi 的 GOPI.md 上枷锁。"

The same row states the proposition the project had already tested:

> "**核心命题（本项目已反复验证）**：**原则无效，检查才有效**。
> LLM 知道"不该硬编码"（问它它答得对），但写代码时照样裸写 23 处工具名、
> 10 处 `taskSessionsRootName`。**陈述原则改变不了行为，可机械检查的规则才能。**"

*(The "23 bare tool names / 10 bypassed constants" figures are the row's own
measurement at that date; the fix landed as Rules 1b/13/14 and their grep-gates. This
document cites the row as the operator's statement of the premise, not as a current
census.)*

- **Need.** When I write down a standard, I need the machine to enforce it — because
  I have measured that writing it down does not.
- **Test.** I can name a rule, and there is a command whose failure names the line
  that broke it, without anyone having read the change.
- **Evidence.** Ledger `01a08d70-2259-70c7-ac9d-192453cfc51e` (verbatim above);
  `GOPI.md:517-521` ("Rules 1–15 are prose: they describe the right thing to do and
  trust an agent to notice. **These six are enforced by a test that fails red** — they
  land in the build where prose never did.").

### §4.2 A decided standard must be computed, and its exceptions must stay visible

The mechanism has three parts, and the *distinction between two of them* is the
design requirement, not an implementation detail:

- **Need.** A standard I have decided on must be computed rather than written down;
  and when the team decides to keep a violation, that decision must be visible on the
  code — not hidden in a list that suppresses the checker.
- **Test.** I can open the file at the offending line and read, in that file, why it
  is allowed; and the same line keeps appearing in a report I can print, so a
  deliberate exception can never become invisible.
- **Evidence.** `internal/standards/gate.go:577-593`, quoted because it *is* the
  requirement:

  > "a baseline entry records 'the linter was told to be quiet about this line', an
  > escape records 'this line is correct and here is why'. Only the second survives
  > reading the code."

  and the visibility half, `:588-591`: an escape "passes, but stays visible in the
  data report (`TestStandardGateDataReport` counts it), so a self-inflicted deliberate
  violation is never invisible." The reason a permit is *not* the default remedy:
  `internal/standards/worktree_root_gate.go:361` — a new violation "must be FIXED, not
  baselined."

### §4.3 Checking must not cost a dependency, and must run where tests already run

- **Need.** I need a check to cost me nothing I must install, licence or remember —
  and to be executed by the same command that already runs the tests, because a check
  that needs its own invocation is a check that will be forgotten.
- **Test.** On a machine with the standard toolchain and no extra software, the check
  runs green as part of the ordinary test command; nothing else can be missing.
- **Evidence.** `GOPI.md:522-526`: *"Run all six: `go test ./internal/standards/` (it
  runs as part of the module suite …; no external linter, no new dependency — stdlib
  `go/parser` + `go/ast`, zero CGO)"*. The operator's dispatch said the same as a
  preference between two admissible mechanisms: *"优先选择**能作为 `go test` 的一部分
  运行**的形式（这样它在 CI/本地都会被跑到），而不是需要人记得手动跑的外部命令"*
  (`01a08d70-2259-70c7-ac9d-192453cfc51e`). The style checker honours the same budget by
  construction: `internal/stylegate/stylegate.go:19` — *"Rule 7 (dependency preflight):
  stdlib only. Type information comes from …"*.

### §4.4 I must be able to see the exceptions, on demand

- **Need.** I must be able to ask the system, at any time, what it is deliberately
  allowing — and get an answer I can check by arithmetic.
- **Test.** I run one named command and it prints, per check, how many findings were
  permitted, how many were reviewed exceptions, and how many were new; the three add
  up to the total, and the "new" figure is zero.
- **Evidence.** `GOPI.md:527-537` makes the report part of the mechanism's
  definition. The command and its **live output, measured in this session** at master
  `90e664e0`:

  ```
  go test -v -count=1 -run 'TestStandardGates|TestStandardGateDataReport' ./internal/standards/
  ```

  ```
  Rule 16 (%w error wrapping):               8 total hits, 7 baselined, 1 escaped, 0 new, 7 baseline entries
  Rule 17 (test env isolation):            123 total hits, 95 baselined, 28 escaped, 0 new, 90 baseline entries
  Rule 18 (no else after terminating if):    1 total hits, 1 baselined, 0 escaped, 0 new, 1 baseline entries
  Rule 19 (no panic outside init/main/Must*):1 total hits, 0 baselined, 1 escaped, 0 new, 0 baseline entries
  Rule 21 (env-name constant … real reader): 0 total hits, 0 baselined, 0 escaped, 0 new, 0 baseline entries
  Rule 20 (no get/is/handle prefix):         1 total hits, 0 baselined, 1 escaped, 0 new, 0 baseline entries
  Rule 13 (no hardcoded worktree root):      1 total hits, 0 baselined, 1 escaped, 0 new, 0 baseline entries
  Rule 13/14 (one production home per scratch env name): 0 / 0 / 0 / 0
  Rule 16-21 family (no unresolved conflict marker):     0 / 0 / 0 / 0
  ```

  Every row satisfies `baselined + escaped + new == total`, and the test itself
  asserts that invariant on every gate (`internal/standards/gate_test.go:103-110`,
  with the comment that a row failing it "would be an arithmetic lie"). The command's
  exit code was `0` — measured, not inferred.

### §4.5 A red proof must exist for every check

- **Need.** I must know that each check *can* fail — a check that has never been seen
  red is a check I cannot trust.
- **Test.** For each check there is a test that feeds it a deliberate violation and
  asserts that it objects.
- **Evidence.** The operator's dispatch made it an acceptance clause:
  *"每条检查都要有**一个测试证明它真的会失败**（造一个违反样例 → 断言检查报错）"*
  (`01a08d70-…`). Deployed: `TestGate1ErrorWrapRedProof` … `TestGate6EnvReaderRedProof`,
  `TestGate7WorktreeRootLiteralRedProof`, `TestGate8ScratchEnvHomeRedProof` (both in
  `internal/standards/gate_test.go`) and `TestGateConflictMarkerRedProof`
  (`internal/standards/conflict_marker_gate_test.go:17`), and
  `TestRule27SyncWriteRedProof` / `TestRule27GeneratedScriptIsCaughtRedProof`
  (`internal/testutil/rule27_gate_test.go:514,552`).

### §4.6 A high false-positive rate means the check is wrong, not the code

- **Need.** I must be able to abandon or reshape a check that cries wolf; a check that
  is usually wrong trains people to ignore all of them.
- **Test.** When a new check is proposed, its measured hit count on the existing tree
  is reported, and a check whose rate is unusable is adjusted or dropped.
- **Evidence.** Operator, verbatim: *"**若误报率高到不可用，说明该检查被错误设计，
  应当调整或放弃**——不要为了"有检查"而强行接入"* (`01a08d70-…`). This is the clause
  that admitted baselines at all: the day-one-red option was rejected because "the tree
  has 352 hits, a red-on-day-one gate gets disabled" (`GOPI.md:555-557`).

### §4.7 A merge verdict must be cheap, bounded and reproducible

- **Need.** Accepting a change must not require re-running the entire project's tests;
  the verdict must be "the packages this change could have broken", stated as one
  command anybody can repeat.
- **Test.** Two people, given the same change, run the same one-line command and get
  the same scope; nobody widens it to the whole tree to feel safe.
- **Evidence.** The command is frozen in exactly one place,
  `internal/smokegate/smokegate.go:54` (`Command = "make test"`) and `:66`
  (`ScopedCommand = "make test-budget PKGS='<the Go packages your diff touched>'"`),
  with `:82` `Doc = "the merge/verify path runs SMOKE tests, BRANCH-SCOPED — never the
  whole tree"`. It is consumed by the merge verdict at
  `internal/tools/verify_gate.go:568` and by the merge prompt at
  `internal/tools/tasks.go:6518,6563`. The whole-tree command is *retired by name*:
  `internal/smokegate/smokegate.go:78` (`WholeTreeNeedle = "go test ./..."`), pinned by
  `TestSmokePathPrescribesNoWholeTreeSuite` (`internal/standards/smoke_path_gate_test.go:189`).

### §4.8 Money and attention are requirements: expensive checks are classified

- **Need.** The check I must wait for has to be affordable, and the check that decides
  a merge has to be one a person will actually run.
- **Test.** The slow checks are marked, they are skipped unless I opt in, and they are
  never on the path that decides whether work may land.
- **Evidence.** A heavy test declares itself (`internal/heavytest/heavy.go:122`,
  `heavytest.Require(t, criterion)`) and skips unless `GOPI_HEAVY_TESTS` is truthy
  (`GOPI.md:1012-1016`); the one named opt-in is `make test-heavy` (`Makefile:35-36`);
  the inventory is a checked-in file (`testdata/heavy-tests.tsv`, 63 lines) with a
  matching gate (`TestHeavyTestInventoryMatchesItsCallSites`,
  `internal/standards/heavy_inventory_gate_test.go:88`). The statement that a merge
  must never land with its own gates skipped is in `scripts/heavy-derive.sh:17`.

### §4.9 A rejection must be recorded, or it will be re-proposed

- **Need.** When the team decides *not* to check something, that decision must be
  written where the next reader will find it — otherwise the next agent proposes it
  again as an improvement.
- **Test.** For each recorded check I can read the alternative that was considered and
  the reason it was refused, and the reason survives in the repository.
- **Evidence.** The operator's dispatch: *"**必须包含"刻意拒绝"的说明**：如果 GOPI.md
  要写"我们不采用某规则"，给一行理由（防止未来 agent 又把它当 gap 提出来）"*
  (`01a08d70-…`). Deployed: six `**Deliberately rejected:**` clauses in `GOPI.md`
  (`:555`, `:602`, `:619`, `:641`, `:664`, `:699`), plus the closing instruction
  "**do not re-propose them as gaps**" (`GOPI.md:713-716`). §6.1 of this document
  treats each of them as a decided design requirement.

### §4.10 The allow-list may only shrink

- **Need.** An exemption that outlives the problem it was granted for is a permanent
  hole; I need the list of permits to be unable to grow quietly, and to shrink as the
  code is fixed.
- **Test.** If I fix a permitted site and leave the permit, the build fails and tells
  me to delete the permit.
- **Evidence.** `internal/standards/gate.go:562-575` — a permit matching no finding is
  an `Errorf`: *"STALE baseline entry … matches no violation — the site was fixed or
  moved. Delete the entry so the gate keeps ratcheting."* The rule-file wording:
  `GOPI.md:530-533` ("a stale permit is a silent permission, so the allow-list can
  only ratchet down").

### §4.11 The census must never be able to lie

- **Need.** I will make decisions from the printed counts, so the counts must be
  incapable of reporting a reassuring number that the scan did not produce.
- **Test.** The per-class figures are recomputed from one pass over the findings and
  asserted to sum to the total, so no column can be derived by subtracting unlike
  quantities.
- **Evidence.** `internal/standards/gate_test.go:92-110`, including the recorded
  reason: the row *"used to print `countHits(hits) - len(g.baseline)` as 'new/gated',
  mixing units … and logging a positive number on a run that raised zero errors — a
  green gate read like a red one."* This is the same failure family as the operator's
  own Rule 15a (a value that silently vanishes). `[inferred: I classify the two as one
  family because both are "the report and the reality can disagree"; weakest link —
  nobody recorded them as related.]`

### §4.12 The rule file and the tool surface must not drift apart

- **Need.** The tool list the agent is told about must equal the tool list that
  exists, because the agent routes by what the rule file says.
- **Test.** Add a tool in code without declaring it and the build fails.
- **Evidence.** `GOPI.md:89-91` — "`claudeToolDefs` in `internal/agent/loop.go` is the source of truth for that
  list — `TestGOPIRule3_MatchesClaudeToolDefs` asserts the two agree, so a tool added
  in code without updating this line fails the build." The test exists:
  `internal/agent/gopidoc_sync_test.go:54`. *(This is a *documentation* parity check,
  not a coding-standard gate; it is filed here because it is the only automated guard
  on the rule file's own truth — see F2.)*

### §4.13 The constraints C1–C5, checked against this module's requirements

Per the series method, every requirement above is checked against the constraints, and
a conflict is filed in §6 rather than softened. **No requirement above violates a
constraint**; the C5 finding is about *cost that exists*, which §6.4 records as a
measured cost, not a violated requirement.

| | constraint | how the requirements here satisfy it | citation |
|---|---|---|---|
| **C1** | one static binary, zero deps, no plugin architecture | the checks are stdlib `go/parser` + `go/ast` inside the ordinary test binary, no external linter, no new dependency, zero CGO; the one code-side checker resolves types through the standard compiler interface | `GOPI.md:522-526`; `internal/stylegate/stylegate.go:19`; `go.mod` has one direct dependency (`modernc.org/sqlite`, pure Go) |
| **C2** | the operator must control the whole thing; anything he cannot observe or reverse is a defect | the census is the machine form of this: one command prints every deliberate exception per check, and the exemption itself lives in the code as a reversible one-line comment | §4.4; §4.2; `GOPI.md:527-537` |
| **C3** | a daemon must be simple — every branch of logic multiplies its failure surface | a check with more judgement in it is a worse check: the selected predicates are deterministic, cheap and adoptable, and the rejections exist to keep judgement out; **but see §6.5**, where a *second* checker with a 180-site "undecidable" class is recorded as the C3 tension | `GOPI.md:703-716` ("Why these six and not the other ten") |
| **C4** | the agent must clean up after itself; dirty is a violation | a stale permit, an unexplained skip and an unreplaced exemption are all uncleaned debts here, and the permit rule makes the first of them an *error* rather than a condition to tolerate | §4.10; `GOPI.md:1131-1188` (Rule 30); the temp-root criterion this module measures against is `internal/scratch.AssertTempRootOffRootFS` / `scripts/lib/tmp-ban.sh` (M10's territory, cited not re-raised) |
| **C5** | small team, limited resources; one person must be able to run it | this is the whole point here: checks exist so one person need not re-read everything, and the classification exists so the suite stays something one person will actually run | §4.7, §4.8; `GOPI.md:1012-1016`; `Makefile:28-41` |

---

## §5 Acceptance in user terms

Each statement is observable by the operator without reading the implementation, and
each is concrete enough that a test author can separate a bug from allowed behaviour.

- **A1.** I commit a change that breaks a decided standard; the ordinary test command
  fails, and the failure names the file, the line and the command that reproduces it —
  without anyone reading my change. `[E: `internal/standards/gate.go:551-560`]`
- **A2.** I deliberately keep a violation; I write a one-line reason next to it; the
  build goes green, **and** the exception still appears in the census on every later
  run. `[E: §4.2, §4.4]`
- **A3.** I fix a permitted site and forget the permit; the build *fails* and tells me
  to delete the permit. `[E: §4.10]`
- **A4.** I print the census; for every check, permitted + reviewed + new equals the
  total, and new is zero. `[E: §4.4 — measured, exit 0]`
- **A5.** I ask for the merge verdict on a change; it names the packages that change
  touched and one command; it does not ask me to run the whole tree and does not
  accept "the whole tree passed" as an answer. `[E: §4.7]`
- **A6.** I run the ordinary test command on a machine that has no extra software
  installed; the checks run. Nothing needs installing first. `[E: §4.3]`
- **A7.** A check I cannot afford is skipped by default, and I can run it deliberately
  with one named command, and it never runs on the path that decides a merge.
  `[E: §4.8]`
- **A8.** I read the rule file and find, for a check I might have proposed, the reason
  it was refused. `[E: §4.9]`
- **A9.** When I add a tool to the agent's surface without declaring it, the build
  fails. `[E: `internal/agent/gopidoc_sync_test.go:54`]`
- **A10.** *[This promise is currently broken — see F1.]* I can trust that a red check
  on the shared baseline means somebody is fixing it: today one of the two checkers is
  red on the shared baseline and no ledger row owns it.

---

## §6 Gap analysis

Four surfaces, then the two questions this module was specifically required to answer.

### §6.1 The recorded rejections are decided requirements (not gaps)

Six alternatives were considered and refused, and the refusal is a *decision of
record*. Each below is treated as a requirement in force: **do not re-propose it.**

| # | decided requirement | reason recorded | where |
|---|---|---|---|
| D1 | We do **not** adopt a whole-tree external linter as the enforcement mechanism; a gate that is red on the day it lands gets disabled | "the tree has 352 hits, a red-on-day-one gate gets disabled. The baseline is the adoption mechanism, the gate is the ratchet." | `GOPI.md:555-557` |
| D2 | We do **not** mandate parallel test execution across the suite; the env-isolation situation must be cleaned up first | interacts badly with the env sites until the cleanup lands | `GOPI.md:602-604` |
| D3 | We do **not** adopt the wider `if/else` chain ruleset; the check stays purely syntactic | the wider ruleset rewrites `if/else-if` chains, which is a judgement call | `GOPI.md:619-621` |
| D4 | A repository-wide ban on panicking is **not** adopted; the crash-dump re-panic is correct behaviour and is annotated instead | the ban "would forbid the crash-dump re-panic, which is the correct behaviour" | `GOPI.md:641-644` |
| D5 | A blanket ban on `get`-prefixed identifiers is **not** adopted; the check is scoped to argument-less reads only | a prefixed package-level function is a command or a tool name | `GOPI.md:664-667` |
| D6 | Flagging an inline bare environment-variable literal is **not** this check's business | that is Rule 1b's subject; keeping the predicate scoped is what keeps false positives at zero | `GOPI.md:699-702` |

Two of these are load-bearing for the module's honesty claim rather than for its
coverage: **D1** is why a *baseline* exists at all (without it, "check everything
strictly" would have been adopted and would have died), and **D4** is the clearest
case in the repository of a check being *deliberately narrower than its rule text* —
which is the same defect class §6.2 finds in the other direction.

*(The rule file's own remaining backlog instruction — "the 13 remaining
Google-extracted items … are listed with their predicates in
`devdoc/coding-standards-checklist.md` §16.1" — is also decided: those items were
checked for mechanical decidability and the ones that failed are excluded with
reasons. `GOPI.md:708-716`)*

### §6.2 **First mandated question: which requirements are still prose-only?**

The method requires this module to count how much of the rule book is *computed* and
how much is merely *written*. **Answer: 5 of the project's 30 numbered rules are
mechanically enforced; 25 are not.** Broken down honestly by rule class:

| rules | mechanically enforced? | evidence |
|---|---|---|
| **Rules 16–21** (the six with red-proof tests) | **yes** — 6 checks | `GOPI.md:517-521`; live census §4.4 |
| **Rule 27** (no device load to satisfy a test; heavy tests classified) | **yes, partly** — the ban and the heavy-test classification are both checked | `internal/testutil/rule27_gate_test.go:331,514,552,618`; `internal/standards/heavy_inventory_gate_test.go:88` |
| **Rule 30** (a run leaves its worktree clean) | **yes, by the run's own machinery** — not a repo-scanning check but a close-out gate | `GOPI.md:1131-1188` (the measured-worktree gate, refuse-the-`completed`-writeback clause) |
| **Rule 3** (tools are minimal) | **yes, as a parity check** — not a contradiction of "never add a tool" but of "declared = existing" | `internal/agent/gopidoc_sync_test.go:54`; `GOPI.md:89-91` |
| **Rule 1** (no magic numbers / bare string keywords) | **partly** — the *values* are pinned (Rules 13/14 grep-gates, tool names, status values, archive namespace) but there is **no general gate**: a newly-invented magic number is caught by nothing | `internal/tools/memory_sync_task_test.go:823`, `internal/tools/task_merged_cleanup_test.go:409`, `internal/sessionevent/literal_gate_test.go:3` — each pins **one** literal, not the class |
| **Rules 2, 4, 5, 6, 7, 8, 9, 10, 11, 12, 15, 22, 23, 24, 25, 26, 28, 29** | **no** — prose, plus scattered per-package tests | see the named cases below |

This is the module's headline number and it deserves to be stated as the module's
first new fact for the operator: **the enforcement rate is 5 of 30.** It is not a
complaint — D1 explains why a 30-of-30 target would be wrong — but nothing in the
repository states the ratio, and every reader of the rule file currently reads it as
if all 30 were computed. `[inferred: the ratio is mine, computed by reading
`gates()` (`internal/standards/gate_test.go:21-85`) against the numbered-rule list
(`grep -cE '^#{2,3} Rule [0-9]+' GOPI.md` → 30); weakest link — "enforced" is my
judgement for the rows where a check exists but covers part of the rule.]`

**Which prose-only rules most deserve a check, ranked by (operator pain × mechanical
decidability).** The first two are the ones I would dispatch today; each names why it
is decidable, which is the property D1 says a check must have before it may land.

1. **Rule 1b/13 — "no magic numbers, no bypassed constants" is the class with no
   class-level gate.** What exists are *per-literal* grep-gates, each written after a
   specific incident. A newly-invented literal is caught by nothing.
   *Decidable?* Partly: "every exported constant has at least one reference outside
   its definition file" is AST-decidable and zero-false-positive-prone. "Is this number
   the right number" is not decidable and must **not** be attempted. **Recommendation:
   gate the bypass half, never the value half.** `[opinion — the split is mine.]`
2. **Rule 15a — `omitempty` on a field the far side requires.** The defect class has a
   recorded incident and a recorded regression test (`internal/deepseek/future_field_regression_test.go`),
   and the rule states the decision rule precisely, but **no gate scans for it**.
   *Decidable?* Partly: "a field whose doc comment says required carries `omitempty`"
   is not; **"a struct field that is unmarshalled by a strict decoder and carries
   `omitempty`" is** for the one decoder that has the problem. The narrow, decidable
   version is available and is not built.
3. **Rule 22 — temp workspaces never on the `/` partition.** Its *criterion* is already
   measured in code (`internal/scratch.AssertTempRootOffRootFS`), so the hard half is
   done; what is not gated is the *prose* half — that no script names a fixed temp path.
   `scripts/lib/tmp-ban.sh` provides the shell primitive but is a library, not a gate.
   *Decidable?* Yes, and cheaply. `[inferred: that no gate currently calls it over the
   script tree; weakest link — I did not run an exhaustive call-site search for
   `gopi_assert_off_root_fs`.]
4. **Rule 10.5 — "every fix ships a minimal repro test."** Recorded as an engineering
   habit with a reason (`GOPI.md:308-310`), cited by name in test comments, never checked.
   *Decidable?* Weakly — "a commit that changes production code must touch a test file"
   is decidable and would be wrong often enough to trip D1. `[opinion — I would not
   build this one.]`
5. **Rule 24's sibling trap — `pgrep -f` self-matching.** A helper exists
   (`internal/testutil/procname_gate_test.go`), which suggests the rule is *partly*
   mechanised; the rule text itself (`GOPI.md:796-824`) is prose.
   `[inferred: that the helper is the rule's mechanisation — weakest link, I read the
   filename, not the file.]`

**Rules 8, 11 and 26 are deliberately left prose-only and should stay that way.**
Rule 8 forbids a specific shell invocation (`git add -A`); a check would have to
pattern-match shell strings and would false-positive on every legitimate `git add`
(and D1 says a noisy check is a disabled check). Rule 11 requires that a language model
*actually used* a tool — the acceptance is a behaviour audit over session logs, which is
what the rule file already prescribes (`GOPI.md:318-330`), and a gate would encode a
threshold nobody has decided. Rule 26 requires that an incomplete fix be *re-dispatched*;
its evidence is a ledger row, not the tree, and the check would be "is there a task for
this" — a question about intent. `[opinion for all three; each rests on D1's
mechanism, not on my preference.]`

### §6.3 **Second mandated question: is the six-check set complete against Rules 1–15?**

**No — and the sharper answer is that "the six" is not the set.** Three findings, in
increasing order of consequence.

**F-assembly (completeness).** Rules 1–15 are the prose standard the operator asked to
be locked down. The six checks cover, at most, checklist rows 2.2, 5.2, 4.3, 2.7, 1.4 and
1b/env — i.e. **six rows out of the checklist's ten categories**
(`devdoc/coding-standards-checklist.md`, cited by `GOPI.md` at each rule). Named classes
of the operator's own complaint are left ungated: **magic numbers / bare literals** as a
class (Rule 1a/1b — only the specific Values have gates), **swallowed errors as a class**
in *test* code (Rule 15b — the production half is swept, the test half is a ceiling of
163, see F1), and **the resource-collection half of the operator's sentence** ("要资源回收";
Rule 15's unchecked-close) at ceiling 0 for production but red in the test tree in the
same run as F1.

**F-coverage (the kind that matters).** The completeness question presumes a closed set
of checks. **Deployed, there are nine — not six.** The registry that `TestStandardGates`
actually iterates (`internal/standards/gate_test.go:21-85`) appends three checks with no
counterpart anywhere in the rule file:

| check | id as the census prints it | escape marker | occurrences of that marker in `GOPI.md` |
|---|---|---|---|
| `TestGate7WorktreeRootLiteralRedProof` | Rule 13 (no hardcoded worktree root literal) | `// worktree-root-ok:` (+ `// fixture-path-ok:`) | **0** |
| `TestGate8ScratchEnvHomeRedProof` | Rule 13/14 (one production home per scratch env name) | `// scratch-env-literal-ok:` | **0** |
| `TestGateConflictMarkerRedProof` | Rule 16-21 family (no unresolved conflict marker in a text file) | `conflict-marker-ok:` | **0** |

Counted live in this session: `grep -c` of each marker string in `GOPI.md` returns `0`,
while `grep -rn <marker> --include=*.go internal cmd testsuite` returns 6, 2 and 1
respectively. Meanwhile `GOPI.md:517` states the opposite as fact: *"**These six are
enforced by a test that fails red**"*.

**Why this is a defect and not a bookkeeping nit.** The rule file is the only
description of the mechanism the next agent reads, and it is the *authority the agent
cites when it writes an exemption*. An agent that hits the worktree-root check reads a
rule file in which that check does not exist: it finds no escape marker, no red-proof
name, and no rejection clause for it — so its cheapest green path is a **baseline
permit**, the one remedy all three of those checks' own reasons forbid ("must be FIXED,
not baselined", `internal/standards/worktree_root_gate.go:361`). The mechanism's own
documentation therefore routes new violations into the mechanism's worst outcome.
`[inferred: the routing claim is mine — weakest link, I did not observe an agent take
that path; what I observed is that the marker strings appear zero times in the rule
file.]`

### §6.4 **F1 (new to the operator): one of the two checkers is red on the shared baseline, and no row owns it**

Measured in this session, in **my own worktree at clean master `90e664e0`**:

```
go test -count=1 -run TestStyleGate ./internal/stylegate/
--- FAIL: TestStyleGate (24.50s)
FAIL    github.com/the operator/gopi/internal/stylegate   26.218s
```

The failures are six rules over their declared ceilings, each reported by the same
message: *"new violations of a swept rule are regressions; fix them or document an
explicit reason"*:

| rule | measured | ceiling | direction |
|---|---|---|---|
| ignored-error (test) | **226** | 163 | **up 63** |
| ignored-error (production) | **9** | 0 | up 9 |
| test-sleep (test) | **9** | 0 | up 9 |
| test-setenv (test) | **1** | 0 | up 1 |
| test-skip (test) | **2** | 0 | up 2 |
| unchecked-close (test) | **2** | 0 | up 2 |

`TestGateCeilingsAreReachable` passes in the same run, so the ceilings are *declared*
and *compared*; what fails is the comparison against reality. The ledger holds **no
pending, claimed or in-progress row mentioning this checker** (`sqlite3
~/.gopi/memory.db "select task_id,status from tasks where summary like '%stylegate%'
and status in ('pending','in_progress','claimed')"` → empty). The most recent document
about this class of red (`devdoc/stylegate-ceiling-audit.md`) describes a *different,
previously fixed* red (`ignored-error: 1 production hit, ceiling 0` and
`unchecked-close: 34 test hits, ceiling 32`) and is marked `<!-- doc-class: historical -->`
— so the earlier repair landed and the class recurred.

**Why it is exactly this module's finding.** §1 promises the operator that he need not
re-read everything *because the machine checks*. That promise has two failure modes and
the repository has separate machinery for both: a check red on a *branch* (the author
fixes it — §3 steps 2–4) and a check red *on the shared baseline before any work starts*
(nobody's diff caused it, so nobody's exit condition notices it). The second is the
invisible one — it is invisible for the same reason the baseline's own comment names:
*a permit is silent*. Here the permission was granted as a **ceiling** rather than a
line, and a ceiling nobody owns is a permission nobody ever revisits.

**The gap, stated without softening.** There is no requirement in this module — and none
in the rule file — that says **a check that is red on the shared baseline must be
attributed to a ledger row within some bounded time**. §4.10 makes a stale *permit* an
error; nothing makes a stale *red* one. `[inferred: "nothing" is my exhaustive
conclusion from reading the two checkers' failure paths and grepping the ledger;
weakest link — a notification or a cron sweep outside the repository (the merge tick,
`GOPI.md:947`'s "invisible to every gate" family) could exist and I did not audit the
tick's behaviour.]`

**Recommended treatment (a requirement, not a repair — this document changes no code
and adds no test).** Two clauses, both cheap and both reversible, in the shape the
project already uses:

- **R-new-1.** *A check that is red on the shared baseline must name its owner.* The
  failure text gains one line — the ledger task id that owns the red, or the literal
  string `unowned` — and a red whose owner line says `unowned` is itself reported to the
  operator on the next merge tick. The precedent for gating *text* rather than behaviour
  is already in this repository (`cmd/agent/daemon_scope_no_privilege_test.go` gates the
  operator-facing wording, `GOPI.md:825-876`).
- **R-new-2.** *The rule file must be generated-or-checked against the check registry.*
  F-coverage above is not fixable by adding three paragraphs to a 1,188-line file that
  every agent already pays for (that is F4's budget problem). The cheaper form is the
  one the project already uses for the tool list: a test asserting
  **rule-file check ids == registry check ids**, in both directions. `[opinion — the
  parity-test shape is my proposal; the precedent is §4.12, which is why I believe it
  is affordable under C5.]`

### §6.5 The C3 tension in the second checker (a §6 finding, not a softened gap)

C3 says a daemon must be simple because every branch of logic multiplies its failure
surface. This module's requirements apply the same principle to checks — §4.13's C3 row
asserts that "a check with more judgement in it is a worse check". **The second checker
strains that requirement and I am filing it rather than smoothing it.** Its own audit
output this session begins:

```
gate_test.go:139: undecidable sites (reported, never guessed): 180
```

180 sites that the checker's type-aware analysis could not decide, reported and
deliberately not guessed. To its credit this is the *honest* form of the trade (it
reports rather than invents — the same choice §8 of the series method demands), and its
escape marker count (887 `//stylegate:ignore` + 170 `//stylegate:allow-sleep` = 1,057
reviewed exceptions) is two orders of magnitude above the six checks' escape census
(`no-wrap:` 14, `env-leak-ok:` 36, `else-ok:` 2, `panic-ok:` 5, `name-ok:` 3,
`env-reader-exempt:` 6 — 66 in total). So the file records a real C3 cost: **the
judgement-heavy mechanism needs 1,057 human-written reasons to stay green, and it is
currently red anyway (F1).** Whether to keep it, narrow it, or replace the ceiling
mechanism with the line-permit mechanism is a decision the operator should make
explicitly; what this document contributes is that the cost is now measured.
`[inferred: that the 180 and the 1,057 are costs of *judgement* rather than of *scale*;
weakest link — I did not classify the exceptions, so some fraction may be mechanical.]`

### §6.6 What this module serves well (the sweep that established it)

Per the method, a "zero falls-short rows" claim requires the sweep that established
it. This module's **serves** rows are §4.1–§4.13 — thirteen needs, each with a
citation — and the sweep behind them was: (a) the registry read end to end
(`internal/standards/gate_test.go:21-85`); (b) every check's failure path read
(`internal/standards/gate.go:536-593`); (c) the live census run, all nine rows
reconciling with `NEW=0` and exit 0; (d) the rule file's gate chapter read in full
(`GOPI.md:492-716`), including all six rejection clauses; (e) the two mandated
questions answered above; (f) the second checker run to completion on the shared
baseline; (g) the three auxiliary mechanisms located (heavy classification, branch-scoped
smoke, tool-list parity). **Surfaces examined and clean:** the six checks' predicates,
baselines, escapes and red proofs; the ratchet (no stale permit in the live run); the
heads-up classification (inventory and call sites agree,
`TestHeavyTestInventoryMatchesItsCallSites`); the ledger-wrapper window
(`internal/ledgerwindow/`, assigned to this module by `00-method-and-standards.md:§7.3`);
the flake registry (`internal/standards/flake_registry_gate_test.go:…`,
`testdata/flakes.tsv`); the doc-class manifest
(`TestDocManifestCoversEveryDevdocFile`, `internal/standards/docclass_gate_test.go`); and
the "no test resolves the ambient profile" gate
(`TestNoTestResolvesTheAmbientGopiProfile`). Five checks beyond the registry's nine are
therefore also confirmed live — which is itself the honest form of the completeness
answer: **the enforcement surface is larger than any document describes, and no document
enumerates it.**

### §6.7 Never decided

- **The enforcement ratio has no owner.** §6.2's 5-of-30 is computable, meaningful, and
  owned by nobody. No rule, no row and no devdoc asks what it should be, and D1 explains
  why 30-of-30 is wrong without saying what right is. `[opinion: this is the module's
  largest single unknown.]`
- **The ceiling mechanism vs the line-permit mechanism was never decided** — one exists
  because the other was insufficient, and no document argues the choice. §6.5 records
  the cost; nobody recorded the decision.
- **Nobody decided whether an escape should be enumerable in reverse.** Baselines are
  audited in both directions (stale = error, §4.10). Escapes are not: there is no
  machine-readable list of which sites carry which escape markers, so "how many
  deliberate exceptions does this project have" is answerable only by grep. The census
  counts escapes *per check* in the data report, which is close — but the report is
  printed, not asserted, so no test would notice an escape census that silently changed
  shape. `[inferred; weakest link — I read the report's print sites, not every assertion
  in the file.]`

---

## §7 Implications for tests

**No tests are written by this document** (series honesty rule 7). This section names
existing tests whose verdict this module's requirements change, and the direction.

| existing test | whose verdict it pins | the direction this requirement points |
|---|---|---|
| `TestStandardGates` + its nine sub-cases (`internal/standards/gate_test.go:87`) | every check's enforcement verdict | **unchanged and load-bearing** — this is the green the module's §1 promise rests on. It passed at `90e664e0` in this session (`ok … 10.410s`, exit 0). |
| `TestStandardGateDataReport` (`:121`) | the census the operator reads (§4.4) | **strengthen, do not change** — it prints what §4.4 quotes. If R-new-1 is adopted, the census is the natural place for the owner column. |
| `TestStyleGate` (`internal/stylegate/gate_test.go:128`) | the second checker's ceiling verdict | **currently RED (F1).** The direction: the *red* is the finding — it should either go green or be attributed (R-new-1). Nothing here asks for its ceiling to be raised; raising a ceiling to silence a regression is the failure mode the test itself names. |
| `TestGateCeilingsAreReachable` (`:193`) | the checker's own census | **unchanged** — it passes, which is how F1 was localised to the comparison rather than the declaration. |
| `TestGOPIRule3_MatchesClaudeToolDefs` (`internal/agent/gopidoc_sync_test.go:54`) | rule-file ↔ code parity for the tool list | **generalise** — it is the working precedent for R-new-2 (parity between a document and its source of truth). |
| `TestSmokePathPrescribesNoWholeTreeSuite` (`internal/standards/smoke_path_gate_test.go:189`) | the merge verdict's wording and scope (§4.7) | **unchanged** — this is the module's model case of a requirement enforced *as text*, which is why R-new-1 proposes the same shape. |
| `TestHeavyTestInventoryMatchesItsCallSites` (`internal/standards/heavy_inventory_gate_test.go:88`) | the heavy classification (§4.8) | **unchanged** — inventory and call sites agree in this session. |
| `TestRule27NoDeviceLoadGenerators` + red proofs (`internal/testutil/rule27_gate_test.go:331,514,552,618`) | the device-load ban (§4.1's most expensive lesson) | **unchanged** — and note the rule file's own instruction (`GOPI.md:974-976`) that a found generator means the *acceptance condition* that motivated it must be corrected too; F1's "nobody owns the red" is a mild instance of the same shape. |
| `TestFlakeRegistryMatchesTheTestsItNames`, `TestDocManifestCoversEveryDevdocFile`, `TestNoTestResolvesTheAmbientGopiProfile` | three auxiliary gates §6.6 confirmed live | **unchanged** — listed because §6.3's completeness answer depends on them existing. |

**The one direction that matters most.** Every other row is "keep green". The row for
`TestStyleGate` is "this is red and nobody owns it" — and the mechanism's own history
(`devdoc/stylegate-ceiling-audit.md`, `<!-- doc-class: historical -->`) shows the class
has been repaired once before. A second repair without R-new-1 reproduces F1.

---

## §8 Confidence and limits

Confidence is stated per claim. Every inference is marked `[inferred]` with its weakest
link named in the same breath.

**High confidence (measured in this session, command named, output quoted).** The live
census and its exit code (§4.4). The stylegate red, its six rows, its counts and its
FAIL line (§6.4). The `grep -c` counts for the three undocumented check markers in
`GOPI.md` (§6.3). The extraction of the check registry from
`internal/standards/gate_test.go:21-85` (§6.3). Every cited test name resolves:
verified one by one with `grep -rl "func <Name>(" --include=*_test.go .` — **26 of 26
found**, zero missing. `git rev-parse HEAD` → `90e664e04977a353cea605e7b571e4275db4b563`.

**Medium confidence.** The 5-of-30 enforcement ratio (§6.2). The registry read and the
rule count are exact; the *classification* of the five enforced rules is mine, and one
of them (Rule 30) is enforced by run machinery rather than by a repository scan — a
different kind of thing counted in the same column. `[inferred — weakest link: the
column mixes two enforcement shapes.]` The prose-only ranking's decidable/undecidable
splits (§6.2 items 1–2) are `[opinion]`.

**Low confidence / explicitly inferred.**

- **The routing claim in F-coverage** — that an agent hitting an undocumented check
  will reach for a baseline permit. `[inferred; weakest link — I did not observe an
  agent do this; the observation is only that the marker strings occur zero times in the
  rule file.]`
- **That no mechanism outside the repository attributes a baseline red** (§6.4).
  `[inferred; weakest link — I did not audit the merge tick's behaviour, only the
  repository's failure paths and the ledger's open rows.]`
- **That the 180 undecidable sites and 1,057 exceptions are a cost of judgement rather
  than of scale** (§6.5). `[inferred; weakest link — no classification of the
  exceptions was attempted.]`
- **That the script tree is not gated against fixed temp paths** (§6.2 item 3).
  `[inferred; weakest link — I found the library and the criterion assertion but did not
  run an exhaustive call-site search for `gopi_assert_off_root_fs`. **This is the single
  cheapest open item in this document to close** and it would move one item from the
  ranking's "inferred" column to "measured".]`
- **That `internal/testutil/procname_gate_test.go` is Rule 24's mechanisation**
  (§6.2 item 5). `[inferred; weakest link — read by filename, not by content.]`

**Known limits of the whole document.**

1. **The census is a snapshot.** Every count above is true of `90e664e0` and will drift
   the moment a commit lands. §4.4's command is the only durable statement.
2. **Two checkers, one document, different shapes.** The six-registry checker is
   audited in full; the ceiling checker is audited only far enough to establish F1 and
   §6.5. A dedicated audit of the second checker's 180 undecidable sites is a separate
   piece of work and is **not** done here.
3. **§1's promise is a reconstruction, not a quotation.** No ledger row states "I must
   be able to accept a change I did not read" in those words. It is `[inferred]` from
   `01a08d70`'s premise plus `GOPI.md`'s own framing ("they land in the build where
   prose never did"). `[inferred; weakest link — the four promises in §1 are mine; the
   operator's own words are quoted only in §4.1.]`
4. **Nothing here is a copy of `devdoc/requirements/test-infrastructure.md`.** That
   document was produced by the sibling task `01a0ab12` (module 01a0ab12's own
   dispatch), whose branch was not on the tree I read at `90e664e0`; where the two
   overlap I cite the same primary sources rather than either document, and the
   overlap itself is named here so a reviewer does not read the duplication as
   derivation.

---

## What I could not determine

1. **What the enforcement ratio *should* be.** §6.2 produces 5 of 30 and no document
   anywhere states a target, a floor, or who owns the number. The cheapest way to find
   out: one sentence from the operator, or a rule row. Cheapest evidence to gather
   first: re-run §6.2's count at a later revision to see whether the ratio moves.
2. **Whether the stylegate red is known to the operator.** The ledger shows no open row
   for it and the last document about the class is `historical`, but "no row" is not the
   same as "not known" — the operator reads his own notifications and those live outside
   the repository. `ls -la ~/.gopi/notify` over the last week would settle it; I did not
   look, because a notification audit belongs to M11 and I would be reading a live
   operator-facing channel from a docs task.
3. **Which of the 1,057 stylegate exceptions are mechanical and which are judgement.**
   No classification was attempted. Cheapest way: group them by rule and by the reason
   string's shape; a reason that names a fixture is mechanical, one that argues a design
   decision is judgement.
4. **Whether the 180 "undecidable sites" grow monotonically.** One measurement exists
   (180, this session). Whether it was 150 or 300 a month ago is knowable —
   `git log -p internal/stylegate/` and the audit docs — and I did not do it. It is the
   single number that would say whether the judgement-heavy mechanism is worsening.
5. **Whether Rules 16–21's six checks were *selected* from a longer candidate list by
   the operator or by the worker.** The rule file says "the six that were chosen"
   (`GOPI.md:703`) and the dispatch said "at least the 3 lowest-false-positive-risk of
   the Top-8" (`01a08d70`), which are not the same set. Which candidates were dropped and
   why is not recorded anywhere I found. Cheapest way: the dispatch row's later comments
   or the session log of `01a08d70`.
6. **Whether `testdata/flakes.tsv` has a ceiling or an owner.** I confirmed the registry
   gate exists and matches its named tests; I did not read the file's rows or check for a
   ratchet direction. It is the one auxiliary gate whose *enforcement posture* I did not
   classify.
