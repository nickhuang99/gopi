<!-- doc-class: normative -->
# Test infrastructure and machine-checkable gates — the requirement behind Rules 16–21, the heavy-test classification, and the test-support layer

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Boundary:      **making a decided standard stick.** What the operator decided must still be true after the
               next agent, who never read the rule, has finished writing code.
In scope:      `internal/standards` (the gate engine + Rules 16–21, 13, 14; the heavy inventory; the smoke-path
               text gate; the flake registry), `internal/stylegate` (the mechanical style scanner and its
               per-rule ceilings), `internal/heavytest` (the classification + the operator boundary),
               `internal/testutil` (the suite's shared support), `internal/smokegate` (the one command the
               merge path runs), `testsuite` (the top-level e2e tree), `testdata/heavy-tests.tsv`,
               `testdata/flakes.tsv`, `Makefile` test targets, `scripts/test-budget.sh`.
Out of scope:  the *rules'* subject matter — Rules 22 (temp off `/`), 24 (cgroup), 25 (privilege), 29 (borrowed
               worktree), 30 (dirty worktree) are M10's module; the merge/verify *workflow* is M2's; the
               build-after-merge close loop is M12's. This document reads the gates that ENFORCE standards,
               not the standards they enforce.
Boundary why:  this is a user-visible responsibility ("do the things I decided still hold?") and not a
               directory, because it spans five packages and its unit is *an enforced rule*, not a file.
Revision read: **master `32932668`** (2026-09-18) — the branch's base commit; `master` has since advanced
with other worktrees' merges, so this document's review surface is `32932668..HEAD`, not `master..HEAD`. Every
`file:line` below resolves at that revision.

---

## 1. What this is for

**A rule that only a person can enforce is not a rule — it is a hope, and it dies with the person's attention.**

The product I actually own is not a script, it is an *outcome I decided once*: my code will not carry magic
numbers, wrapped errors will stay in a chain, a test will not leak environment into its neighbours, a helper
that nobody reads will not pretend to be a control knob. I decided those things because each of them cost me
something real once. What I need is for them to still be true a month later — when the person doing the work
has never spoken to me and has no memory of why.

This module is that promise. Its parts are:

- **A promise with a mechanical referee.** Where a decision is decidable by looking at the code — not by
  judgement — the machine decides, on every build, and the build goes red when the decision is broken. Nothing
  here asks anyone to be diligent. I do not want to *read* a rule and remember it; I want to be **told**, in a
  place I cannot miss, the moment a rule is broken again.
- **A promise that can be adopted without a big-bang cleanup.** A rule that would go red on the day it lands
  gets switched off the same day. So a new referee lands *green*, with an explicit, written, per-line list of
  the old violations it is not yet asking me to fix — and then it may only ever tighten. My debt is recorded,
  not hidden, and it can only shrink.
- **A promise that my day-to-day work stays fast.** The expensive tests exist and must keep existing, but they
  must not run while I am waiting for a merge. The cheap ones run all the time; the expensive ones run when I
  deliberately ask, and the record of what did not run is written down where I can read it.
- **A promise that the rules themselves can be appealed.** Sometimes the referee is wrong, or a violation is
  genuinely the right thing. There is a way to say so *at the code*, with a reason, and stay visible.

The three parts are one product. Take away the mechanical referee and I am back to relying on memory. Take away
the adoptable landing and the referee gets switched off. Take away the fast/slow split and I stop running tests.

---

## 2. Who is served

| actor | the need it has | evidence |
|---|---|---|
| **the operator** (the operator) | my decided standards must still hold when I am not looking; my development loop must stay fast; my machine must not be used as a load generator to make a test pass; I must not be handed a *green* that a skipped test produced | `GOPI.md:1145` ("**The rule is checked, not trusted** (原则无效，检查才有效)"); `GOPI.md:906` Rule 27; ledger `01a0a88e` (operator verbatim on the heavy classification, quoted in §4.4) |
| **the agent** (Bond) | I must learn a standard *at the moment I break it*, from the build, not from a document I did not read | `internal/standards/gate.go:1-16` ("the reader is the thing being audited"); §4.1 below |
| **the next agent** | I must be able to appeal a referee I believe is wrong, without weakening it | the inline-escape mechanism, `internal/standards/gate.go:462,530,692,736,792`; `GOPI.md:538-716` |
| **the future maintainer** | I must be able to read a rule's *why* and see which alternative was deliberately rejected, so I do not re-propose it | `GOPI.md:538-716` each carries a "Deliberately rejected:" paragraph; `devdoc/standards-gate.md:237-252` |
| **the host / the machine** | the test suite must not damage the machine it runs on | `GOPI.md:906` Rule 27's three incidents; `internal/testutil/rule27_gate_test.go:3` |

The actor whose need distinguishes this module from every other is the **operator at t+1 month**: the module
exists because *he* will not be in the room when the rule is broken.

---

## 3. The workflow it sits in

**Entry condition.** A defect has recurred — or a class of defect has been recognised — often enough that a
written rule either does not exist or does not hold. The recognition moment has a recorded shape: the same
defect shape found **ten times in one package after the rule against it existed**
(`devdoc/standards-gate.md:37-39`), and GOPI Rule 11's own record that two tools sat at 0 %/0.3 % adoption
until an adoption proof was required (`devdoc/standards-gate.md:40-41`).

**The end-to-end run.**

1. **A predicate is written** — a pure function over one parsed file returning hits. Deterministic, no
   judgement call, stdlib only (`internal/standards/gate.go:1-16`).
2. **The tree is measured, and the existing violations are listed** — exact `file:line` entries in a baseline,
   each with the gate's one-line reason (`internal/standards/baseline.go:53,479,594,683,694,701`).
3. **The gate is registered** with id / checklist row / red-proof test name / escape marker / baseline reason
   (`internal/standards/gate_test.go:23-76`).
4. **A red proof ships with it** — a violating snippet that must be reported, a compliant snippet and a
   near-miss that must not be (`internal/standards/gate_test.go:154,191`; §4.1).
5. **From then on, every build runs it.** `go test ./internal/standards/` fails on any hit that is neither
   baselined nor escaped — so the rule is enforced in the project's ordinary build/test step, with no CI and
   no external linter (`devdoc/standards-gate.md:296-298`, honest: "**No CI.**").
6. **The list may only shrink.** A baseline entry whose site has been fixed is itself an error — a stale
   permit is a silent permission (`internal/standards/gate.go:26-40`).
7. **The cost split is maintained in parallel.** A test that is expensive declares itself heavy and skips
   unless the operator opts in; a gate asserts the inventory matches the call sites; a boundary refuses a
   *work* process's inherited opt-in; the merge path runs smoke, branch-scoped, never the whole tree.

**Exit condition.** A future agent breaks the standard and the build goes red, naming the file and line —
*without anyone having read the rule*. And on the merge path: a verdict arrives fast, from the packages the
diff touched, with the heavy set recorded as skipped rather than silently absent.

---

## 4. Why it exists

### 4.1 The need: a decided standard must survive the next agent's forgetfulness

- **Need:** a rule I decided once must still be true after the person doing the work has changed. I must not
  have to be in the room, and I must not have to rely on anyone reading a document.
- **Test:** I write a violation of a rule I decided; the build goes red and names the file and the line.
  Nobody read anything.
- **Evidence:** `internal/standards/gate.go:1-16` states the mechanism and its reason — *"GoPi has no linter
  history and ~160 production files carrying existing violations. A whole-tree blocking linter would be red on
  day one and would therefore be switched off. A `go test` gate with a *baseline allow-list* is green the day
  it lands and turns red on the first NEW violation."* `devdoc/standards-gate.md:29-46` gives the thesis and
  the two measured failures that produced it: the hardcoding review's same defect shape **10× in one package
  after the rule existed** (`:37-39`), and `git_blame`/`code_search` at 0 %/0.3 % adoption until Rule 11
  required an adoption proof (`:40-41`).
- **The recognition that a gate is the instrument, not prose**, is `internal/standards/gate.go:5-8` and, in the
  project's own words at the site of a gate's own reason,
  `internal/standards/cwd_release_gate_test.go:22`: *"原则无效，检查才有效" — a principle is worthless, a check
  is what counts.* It is an operator's sentence, kept verbatim in the code.

### 4.2 Rules 16–21: each gate, its motivating defect, its mechanism

The live census below was produced in this session by
`go test -v -run 'TestStandardGates|TestStandardGateDataReport' ./internal/standards/` on master `32932668`
(all eight PASS; `NEW=0` for every gate). The baseline sizes quoted in `devdoc/standards-gate.md:12-18`
(352 / 120) are **superseded** — that document says so itself, and this census is the current reading.

| gate | rule | checklist row | motivating defect | citation | live census (2026-09-18) | baseline reason |
|---|---|---|---|---|---|---|
| 1 | Rule 16 — `%w` not `%v` | 2.2 | `errors.Is`/`errors.As` branches silently never fire: a wrapped error rendered with `%v` leaves the chain | `GOPI.md:538-558`; checklist row 2.2 (`devdoc/coding-standards-checklist.md`) | hits=7 baselined=6 escaped=1 NEW=0 | every entry predates the gate; the `%w` codemod is its own task, the list may only shrink (`internal/standards/gate_test.go:27`) |
| 2 | Rule 17 — `t.Setenv` not `os.Setenv` | 5.2 | `os.Setenv` in a test leaks process state into sibling tests → order-dependent flakes | `GOPI.md:559-604`; the worker's own evidence `internal/agent/loop_test.go:824` whitelist-setenv leak (ledger `01a08975`) | hits=114 baselined=91 escaped=23 NEW=0 | 89 legacy sites the sweep left behind + the **shape rule**; the list is their reason, not an adjacent marker (`internal/standards/gate_test.go:38`) |
| 3 | Rule 18 — no `else` after a terminating `if` | 4.3 | dead weight: the `else` branch is unreachable; the guard-clause form is behaviour-identical | `GOPI.md:605-621` | hits=2 baselined=2 escaped=0 NEW=0 | purely cosmetic, outside the refactor scope (`internal/standards/gate_test.go:47`) |
| 4 | Rule 19 — no `panic` outside `init`/`main`/`Must*` | 2.7 | panicking **instead of** returning an error takes the choice away from the caller | `GOPI.md:622-644` | hits=1 baselined=0 escaped=1 NEW=0 | **EMPTY by construction** — the one site is the crash-dump re-panic, annotated at the function (`internal/standards/gate_test.go:56`) |
| 5 | Rule 20 — no `get`/`is`/`handle` on an accessor | 1.4 | a reader that reads should be named for the value | `GOPI.md:645-667`; Google Style `#getters` | hits=1 baselined=0 escaped=1 NEW=0 | the one site mirrors the X11 `GetImage` protocol name (`internal/standards/gate_test.go:74`) |
| 6 | Rule 21 — every env-name constant has a REAL reader | 1b/env | **the defect that motivated it:** the scratch GC shipped `GOPI_TEST_SCRATCH_GC_MIN` with documentation and **no reader at all** — an operator who set it believed the sweep was running | `internal/standards/env_reader.go:3-9`; `GOPI.md:668-716` | hits=0 baselined=0 escaped=0 NEW=0 | **EMPTY by construction** — an unread hook fails on the commit that introduces it (`internal/standards/gate_test.go:65`) |
| 7 | Rule 13 — no hardcoded worktree-root literal (worktree-root gate) | 1b/worktree-root | a constant defined and then **bypassed by a bare literal**; the fix lands at the definition site and the bypassed sites keep the old value | `internal/standards/worktree_root_gate.go:1-30`; `GOPI.md:1-68` Rule 1b; Rule 13 `GOPI.md` | hits=1 baselined=0 escaped=1 NEW=0 | the one site is the X11 socket directory the protocol mandates (`internal/standards/worktree_root_gate.go:344`; data report `internal/shot/x11.go:137`) |
| 8 | Rules 13/14 — one production home per scratch env name | 1b/scratch-env | two parallel landings auto-merged two attempts at the SAME Rule-14 constant: `BaseEnvName` declared twice with **different values**, `tmpfsMagic`/`ramfsMagic` twice — the package did not compile, and the quiet half was a misstated mirror | `internal/standards/scratch_env_home_gate.go:1-25` (defect at `:11`) | hits=0 baselined=0 escaped=0 NEW=0 | EMPTY by construction |

**Three further gate families live in the same package and complete the picture.**

| gate | the defect it freezes | citation | live test name |
|---|---|---|---|
| tracked-scratch hygiene | the 2026-09 repo-hygiene sweep found **457 junk paths committed to the repo root** (a 19 MB `go test -c` binary, a fake-`$HOME` holding a real `~/.gopi` copy, a whole go-build cache); `git rm --cached` fixed the instance, the gate is the ratchet — **`.gitignore` does not help, because a tracked path ignores every ignore rule** | `internal/standards/scratch_gate.go:9-21` | `TestRepoRootHasNoTrackedJunk`, `TestRepoRootJunkGateScansTheRealIndex` |
| ambient-profile gate | a `go test ./cmd/agent/` fixture resolved the developer's **real** `~/.gopi`; `gopi worker --status` reported **14 phantom worker entries**, one per test-binary pid — the test still **passed**, because it only asserted on the private home it handed the child | `internal/standards/ambient_profile_gate_test.go:1-21` | `TestNoTestResolvesTheAmbientGopiProfile`, `TestAmbientProfileGateRedProof` |
| worktree-release CWD gate | `cmd/agent` carried ONE cwd release while **four other process paths** could stand inside a worktree and never let go; Linux cannot move another process's cwd, so the holder is the only party who can release it, and the finished deliverable is never merged with nothing anywhere saying why | `internal/standards/cwd_release_gate_test.go:1-26` | `TestGateCwdRelease_RedProof`, `TestGateCwdReleaseFindsTheRealTree` |

The CWD gate is the clearest instance of §4.1's thesis in the tree: the function's own comment **already named**
the long-lived callers — *knowing was not enough* (`internal/standards/cwd_release_gate_test.go:23-25`).

### 4.3 The mechanism, and why its two halves are the requirement (not decoration)

Three states, and only three (`devdoc/standards-gate.md:58-84`):

| state | meaning | result |
|---|---|---|
| **baselined** | a known legacy site, an exact `file:line` in `internal/standards/baseline.go` | passes; a *stale* entry is an error |
| **escaped** | a true positive the team deliberately keeps, annotated at the code | passes; **stays visible in the data report** |
| **unannotated** | everything else | **fails** |

- **Need:** a new referee must be adoptable on the day it lands, and my debt must be **visible and shrinking**.
- **Test:** I can read the allow-list in one sitting; fixing one of its sites makes the build fail until the
  entry is removed; no entry is a file glob.
- **Evidence:** exact `file:line`, never a glob, *"so a violating line that MOVES is reported again — the
  allow-list cannot rot into a blanket file exemption"* (`internal/standards/gate.go:17-25`); the review
  budget `maxBaselineEntries = 400` with a **ratchet target of one-third** and a data-report `RATCHET:` line
  above 50 (`internal/standards/gate.go:44-58`; `devdoc/standards-gate.md:186-204`).
- **The escape is not a suppression:** the marker sits at the code with its reason
  (`errorWrapEscape` `internal/standards/gate.go:462`, `testEnvEscape` `:530` as `// env-leak-ok:`,
  `elseAfterReturnEscape` `:692`, `panicOutsideInitEscape` `:736`, `accessorPrefixEscape` `:792`), and the
  site keeps appearing in `TestStandardGateDataReport` (`internal/standards/gate_test.go:120`).
- **Non-vacuity is itself a requirement:** every gate ships a red proof so a predicate that stopped matching
  cannot read green forever (`internal/standards/gate.go:12-16`).

### 4.4 The heavy-test classification: fast by default, real coverage still auditable

- **Need** (operator verbatim, ledger `01a0a88e`, 2026-09-16, after the machine was saturated for the third
  time): *"it is a convention that some of testsuite can not be run for every time, especially those pressure
  test, high resource consumption testing, etc. those are special testing case, which can stall other
  tests/processes, can only be run periodically at a offpeak period. so, normal merging verification or other
  development/bugfixing task, usually only run smoke testing for basic verifications in order to lighten
  system pressure."*
- **Test:** I run the merge path; it finishes fast; the expensive tests did not run; and the run tells me
  **which** ones it did not run, so nobody can later claim coverage that did not happen.
- **Evidence:** the criterion — *"a reasonable person would not want it running on a shared machine while other
  work proceeds"* — with five named constants (`internal/heavytest/heavy.go:69-96`); the opt-in
  `EnvName = "GOPI_HEAVY_TESTS"` read by exactly one function, `Enabled()`
  (`internal/heavytest/heavy.go:43,105`); the declaration `Require(t, criterion)`
  (`internal/heavytest/heavy.go:120`); the inventory `testdata/heavy-tests.tsv` (**42 non-comment rows** at
  master `32932668` — the file is the source of truth, not this number); `make test` vs `make test-heavy`
  (`Makefile:28,35`); and the record of what did not run, `GOPI_HEAVY_SKIP_LEDGER` →
  `.heavy-skipped.tsv` (`internal/heavytest/heavy.go:48`; `Makefile:29`).

**Two selection rules that are requirements about test design, not taste** (`devdoc/heavy-tests-rule27-gate.md:31-47`):

- **The measured floor is ≥5 s per test** — the classification may not grow past the measurement, because a
  classification that outgrows its evidence is a guess.
- **Gate packages are NEVER heavy.** `internal/standards` and `internal/stylegate` stay on the smoke path: *"a
  merge must not be able to land with its own gates skipped"* — and
  `TestHeavyTestInventoryMatchesItsCallSites` **fails** if anyone classifies a test in a gate package as heavy.
  That is the line between classifying cost and weakening a gate.

### 4.5 Rule 27 as a requirement about TEST DESIGN (not about machines)

Rule 27 is in this module because **the thing it forbids was a test-design decision**:
`GOPI.md:906` states it as a ban with no exceptions, and `GOPI.md:906-1010` records the reasoning that
produces the violation as *itself* the defect — *"the load was never measuring our code; it was manufacturing
a broken environment and then measuring that."*

Reconstructed as a requirement about how tests are designed:

- **Need:** a test must measure **the thing under test**, never manufacture a sick environment and then
  measure the sickness. Also: a test I run must not damage the machine other work depends on.
- **Test:** the acceptance evidence for a slowness question is a direct measurement of the implicated call —
  and I can see where a test with no such instrument was refused.
- **Evidence — the three incidents** (`devdoc/heavy-tests-rule27-gate.md:104-115`): 2026-08-27 a `dd
  oflag=direct conv=fsync` loop filled `/`; 2026-09-11 a `while :; do dd … conv=fsync; done` took `/` to 100 %
  and hermes had to be killed; 2026-09-16 three `iopress.sh` batches × 16 workers, **PPID=1, outliving their
  producer**, load 56.42, every `dd` in state D — *"hand-written into a worktree's `.evid/` — untracked,
  invisible to every checked-in gate."*
- **The retired instrument, replaced by a direct one** (`devdoc/heavy-tests-rule27-gate.md:157-179`): the
  epic's acceptance clause `≥90% /proc/pressure/io full avg300` is **withdrawn**; the replacement measures the
  run's own artifacts (`go test -json` per-package wall time + the slowest tests) and the duration of the
  implicated call itself, the first-open schema batch's `tx.Commit()` in `execSchemaBatchOnce`
  (`internal/tools/memory.go`). The **diagnosis is unchanged**; only the instrument changed.
- **Design requirement that follows: heavy tests are classified and off-path, and skipping is never silent**
  (`GOPI.md:906-1010`, "Heavy tests are classified, not tolerated"; a silent skip is tracked as
  *"a deleted test wearing a passing green"*). Every skip logs
  `HEAVY TEST SKIPPED (set GOPI_HEAVY_TESTS=1 to run): <criterion>` and appends to the ledger
  (`devdoc/heavy-tests-rule27-gate.md:78-86`).

### 4.6 Rule 30's own sentence: the gate is checked, not trusted

The same principle is written into `GOPI.md:1145`: **"The rule is checked, not trusted" (原则无效，检查才有效)**.
This is the module's requirement stated in the operator's own words — and it is the reason this document is
graded on *where the checking mechanism is absent or wrong*, not on how complete the rule list looks.

---

## 5. Acceptance in user terms

1. I break a rule the project decided; the ordinary build fails and names the file and the line. I never had
   to read a document.
2. I can read the list of existing violations in one sitting, and each entry tells me why it is still there.
3. I fix one of those violations; the build now fails telling me the permit is stale. The list can only shrink.
4. I believe a referee is wrong; I can write the reason *at the code* and the site stays visibly listed as a
   deliberate exception — never silently vanished.
5. Every referee proves it can still see: a deliberately broken snippet is reported, a compliant one is not.
6. On the merge path my verdict arrives fast, covers the packages my change touched, and **tells me which
   expensive tests it did not run**.
7. A background worker or an executor child **cannot** turn on the expensive set by inheritance; if it tries,
   the refusal is loud, names the reason, and tells me the route that works.
8. No test improves its own verdict by loading the machine. When the question is "why is this slow", the
   answer comes from measuring the slow call.
9. Adding an expensive test without declaring it is a build failure (an unlisted heavy test is a silent skip).
10. A test cannot quietly resolve my real project directory, and a test cannot quietly leak environment into
    its neighbours — the build refuses both.

---

## 6. Gap analysis

Four surfaces. §6.1 is where this module is *served*; §6.2–§6.4 are where it is not.

### 6.1 Serves

- **One gate per recurring defect shape, each with a red proof and a named rejected alternative.** All eight
  registered gates PASS with `NEW=0` (census, §4.2). The rejected-alternative paragraphs are the load-bearing
  part: they are what stops the next agent re-proposing the gate that would have gone red on day one
  (`GOPI.md:538-716`; `devdoc/standards-gate.md:237-252`).
- **The ratchet genuinely moves down, measured.** `internal/stylegate`'s ceilings carry dated down-ratchets
  with their reasons — ignored-error 211 → 182 → 163, `errorf-verb` 10 → 9 → 8 → 7, unchecked-close → 0
  (`internal/standards/gate.go:44-58`; `internal/stylegate/gate_test.go:23-83`), each carrying the *date and
  the reason it could not go further*.
- **Rule 27 became mechanical, and the check states its own blind spots.** `TestRule27NoDeviceLoadGenerators`
  (`internal/testutil/rule27_gate_test.go:331`) scans the **working tree including untracked files and dot
  directories** — precisely where the third incident lived — and the devdoc states what it does **not** catch
  rather than glossing it (`devdoc/heavy-tests-rule27-gate.md:116-156`).
- **The heavy boundary answers a question a variable cannot.** `internal/heavytest/boundary.go:11-45`: an
  inherited `GOPI_HEAVY_TESTS=1` — a dev shell, a briefing that says "run the full suite" — would have bought
  an hour of host CPU the default forbids. A work context is detectable
  (`GOPI_EXECUTOR_SESSION_ID` / `GOPI_AGENT_SESSION` / `GOPI_TASK_WRAPPER_ID` / `GOPI_TASK_RUN_ID`), and the
  operator declares his own terminal per invocation (`GOPI_HUMAN_TERMINAL`).

### 6.2 Falls short

**F1 — the gate engine is not shared with the style scanner, and the operator must hold two mental models.**
`internal/standards` and `internal/stylegate` are two independent engines. The standards engine recognises an
inline escape (five markers, `internal/standards/gate.go:462,530,692,736,792`); the style engine's
`errorf-verb` rule **honours no inline escape at all** — the ceiling is where a deliberate unwrap must be
declared (`internal/stylegate/gate_test.go:62-70`). So the *same* question — "may this `%v` stay?" — is answered
by four different instruments depending on which one looks:

| instrument | verdict for the SAME site | citation |
|---|---|---|
| `internal/standards` Rule 16 | baselined → green | `internal/standards/baseline.go:53` (entry for `task_merge.go`) |
| `internal/standards` Rule 16 | escaped → green, visible | marker `// no-wrap:` |
| `internal/stylegate` errorf-verb | **counted against the ceiling of 7** | `internal/stylegate/gate_test.go:23-83` |
| `internal/tools`' own regression test | **FAILS if the site carries `// no-wrap:`** | `internal/tools/task_merge_wrapchain_test.go:83-88` |

For the same site that is *fail*, *pass*, *pass-but-visible*, and *pass-by-permit* depending on the reader.
Whether a **third** state is intended is not recorded anywhere I can find — see §8 (U1).
*Weakest link:* my list of instruments is the four I found by reading; I did not exhaustively enumerate every
consumer of the error-wrapping obligation.

**F2 — the escape audit is one-sided, and the gate says so itself while nothing audits the style engine at
all.** Baselines are audited in both directions (a stale entry fails — that is `TestStandardGates`'s stale-permit
half). The **inline escapes are not enumerated anywhere machine-readable**, and gate 2's own baseline reason
records the asymmetry verbatim (`internal/standards/gate_test.go:38`, quoted in gate 2's `baselineReason`):

> "Annotated coverage is thin AND one-sided: **26 entries carry a marker on the offending line** and 38 within
> +/-3 lines (43 within +/-6) — **every one of the 26 on-line markers sits on an `os.Setenv` line and NONE on
> the 63 `os.Unsetenv` lines**"

So "each escaped site carries a reason" is true for ~26 of 89 baselined sites, and for the other 63 the *list*
is the reason — a documented, deliberate asymmetry (a bare `t.Setenv` cannot express ABSENCE, which is why the
`os.Unsetenv` sites exist at all). What **no** artifact does is enumerate the escapes as a set: I could compare
the two engines' exception populations only because the two mechanisms use different spellings — **29
`env-leak-ok` markers** in the test tree (`grep -rn 'env-leak-ok' --include='*_test.go' internal cmd testsuite`
→ 29) against **538 `stylegate:ignore` markers** in the same tree
(`grep -rn 'stylegate:ignore' --include='*_test.go' internal cmd testsuite` → 538, re-measured this session).
A reviewer asking "how many deliberate exceptions does this repo carry, and is each one reviewed?" cannot
answer from any single command.

**F3 — 50 % of the classified-heavy inventory is not classifiable, and the gate is correctly built to accept
that.** The inventory has **42 non-comment rows** (`testdata/heavy-tests.tsv`; `grep -vc '^#'`) while the
classification's own derivation recorded **45 tests ≥5 s, −1 gate-package test, −3 subtests → 41 rows**
(`devdoc/heavy-tests-rule27-gate.md:49-58`). So ~21 of the 42 rows carry **no criterion** (bare skip). The gate
**cannot** distinguish those from a classified one — and that is correct, not a bug: inventing a criterion
would be worse than the blank. **What is missing is the other half of the operator's §4.4 sentence** — "can
only be run periodically at a offpeak period". Nothing in the tree schedules
`make test-heavy` / `make flake-catch`, so "off-peak" is a convention with no mechanism behind it. The operator
said the expensive tests "can only be run periodically"; nothing runs them periodically. Compare the peak-hour
cost guard, which was **removed** precisely because a start-time refusal could not be made honest
(ledger `01a0a08b`).

**F4 — the operator's sentence that named this module is only half-implemented, and the guard for the
unimplemented half was deleted.** Ledger `t-178817` (2026-08-31) is the operator asking *"need more testsuite of
gopi"*. What that produced is a `testsuite/` **tree** (`internal/standards/gate.go:79-83`, `e2eTestRoot`) and a
nightly test family (ledger `t-178851`: NIGHT-TEST runs over `internal/agent`, `cmd/agent`, `internal/tools`,
small packages, E2E smoke) — **not additional unit coverage where it is thin**. The unit-coverage campaign that
would have met it exists as **22+ filed task rows** (ledger `01a08bab`, `01a08bc6` ×2, `01a08bc8`, `01a08bc9`
×2, `01a08bce` ×2, `01a08bcf`) — read with a file-level report on coverage gaps, not as merged work. The
grading question — *"is unit coverage still thin, three weeks after the operator said so?"* — is not answerable
from the repo. *Weakest link:* I inferred "not merged work" from the row summaries and the absence of a
coverage gate (F4.1 below); I did not run a coverage measurement, and a coverage threshold is not something
this series may invent.

**F4.1 — a lead I chased and had to retract; the sentence that looked like a deleted guard is a live negative
control.** My first draft reported the `t.Skip` at
`internal/standards/heavy_drift_report_gate_test.go:252` as the remains of a deleted guard, on the strength of a
`git log -S` run that returned nothing. **The retraction is the finding, because the way I got it wrong is the
way a careless reader will get it wrong too.** Three measured corrections:

1. **The commit exists.** `git log --all --oneline -S "classified control"` →
   **`1219e8b4`**: *"feat(heavy): name unclassified long tests + document the re-derivation recipe"*
   (2026-09-18), which **added** the whole file (`git show 1219e8b4` shows `--- /dev/null`) and added that
   `t.Skip` line. Nothing was deleted. My first `-S` query used a longer phrase than the tree carries and
   returned empty for a reason that had nothing to do with deletion.
2. **The skip does not fire.** `go test -v -run TestHeavyDriftReportNamesAnUnclassifiedSlowTest ./internal/standards/`
   → `--- PASS … (0.10s)`. The condition it guards (`no internal/tools row in the inventory`) is false — the
   inventory carries **9** `internal/tools/` rows (`grep -c 'internal/tools' testdata/heavy-tests.tsv` → 9), so
   the loop finds its classified control. The line is a **negative control guarding against a vacuous pass**,
   i.e. the §4.3 non-vacuity discipline, not a hole.
3. **The lesson is a requirement about this document class.** An empty `git log -S` is evidence about *my
   query string*, not about the tree; the falsifying step is running the test (`PASS`) or searching a shorter
   phrase. A reconstruction that had shipped the first draft would have handed the operator a false alarm with
   a plausible mechanism attached. Rule 27's own discipline applies to this document's instruments: *measure
   the subject directly* — here, run the test — rather than infer its absence from a negative search.

**Retained as a real (smaller) gap:** the file's four tests
(`TestHeavyThresholdReportIsWiredToTheSmokeRun` `:77`, `TestHeavyDriftReportNamesAnUnclassifiedSlowTest`
`:227`, `TestHeavyDriftReportAgreesWithTheInventoryAlone` `:286`) are the *whole* coverage of the drift report,
and one of them can go vacuous by `t.Skip` without any gate noticing — `RuleTestSkip` only requires a reason
string (`internal/stylegate/gate_test.go:88,100`), which this line has. So the skip is honest and
**unaccountable**, and the only way to know it is live is to run the test.

### 6.3 Over-builds

**O1 — a gate policing a style rule the operator never asked for, with a zero ceiling.**
`RuleTestChdir` is registered in `internal/stylegate` with ceiling **0** and the comment *"any hit is a new
silent cwd restore"* (`internal/stylegate/gate_test.go:84-87`; rule id
`internal/stylegate/stylegate.go:53`). The operator has a *product* stake in "temp work never fills the boot
disk" (Rule 22) — a test's cwd restore is a step removed from that. It is deterministic and cheap, so it is a
perfectly good lock; but it is the clearest instance in the tree of a gate whose subject is **coding style**,
adopted from the Google Guide with no operator ask, incident, or ledger row behind it — unlike every one of
Rules 16–21 (which trace to the checklist's Top-8 execution list and, for Rule 21, to a named defect) and
unlike Rule 27 (three incidents). The `-h`/docs distinction is worth stating plainly: **the requirement the
operator stated is "my machine must not be damaged"; the requirement the gate enforces is "a test must restore
cwd".** They are adjacent, not the same, and only the first is a need.

**O2 — the standard quality is applied unevenly, and that is the honest reading of the drift question.**
`devdoc/standards-gate.md:237-252` **excludes, with reasons**, six Google-Style items because they are not
decidable (name proportionality, local consistency, interfaces-defined-by-consumer, custom context types,
package-doc completeness). That exclusion is exactly right and it is the model. What is NOT recorded is the
inverse list: *which adopted items are decidable but have no user need behind them*. `RuleTestChdir` is my
best-evidenced candidate for that list (above); the honest statement is that the tree records the standard's
own thesis — deterministic + cheap + green-on-landing — and **does not** record a relevance criterion.

**O3 — `RuleTestSkip`: a genuine need implemented so strongly that the *reason* became the only thing checked
— the clearest instance of the drift question, found by asking what it does NOT check.**
The requirement behind it is real and operator-shaped: **a silent skip is a deleted test wearing a passing
green** — the phrase is the doctrine's own (`internal/heavytest/heavy.go:26-30`, `GOPI.md:906` region), and a
test that quietly stops running is exactly the "prose rule did not hold" failure this whole module exists to
close. What was built is narrower: `RuleTestSkip` reports `t.Skip`/`t.Skipf` **that carry no literal reason**
(`internal/stylegate/stylegate.go:1165-1270`, rule id `:52`), with ceiling **0** and the recorded reason
*"every t.Skip in the repo now carries a literal reason or a reason comment"*
(`internal/stylegate/gate_test.go:88,100`). The need is *the skip is visible and countable*; the check is *the
skip has a string*. A skip with a perfect reason string satisfies the gate **while removing the coverage**, and
nothing in the tree compares the skipped set against anything — measured: no gate, report or script reads
`GOPI_HEAVY_SKIP_LEDGER`'s **contents** (the only writers are `internal/heavytest/heavy.go:150` and
`internal/heavytest/boundary.go:219`; the ledger path appears in exactly **three** non-test, non-doc places, none of which reads
it — `Makefile:29` (writes it), `internal/heavytest/boundary.go:107` (names it in a refusal message) and
`scripts/heavy-drift-report.sh:12` (a comment saying a reviewer sees it) — and `scripts/test-budget.sh` never
mentions it at all). The one artifact built to make skips enumerable is written on every `make test` and read by
nobody — while the refusal path's own operator-facing text asserts the opposite mechanism: *"the skip ledger
records a … row **so the refusal is countable** (`make test` leaves its ledger at `.heavy-skipped.tsv`)"*
(`internal/heavytest/boundary.go:99-108`). Countability by a file nobody opens is a claim, not an instrument —
and it is the claim a reader of that message will believe. Both halves of §5's acceptance sentence are unmet at
the same site:
the reason-format half is over-served (0 ceiling, 507 `t.Skip*` call sites, 293 of them `t.Skipf`), and the
countability half has no instrument at all.

### 6.4 Never decided

**N1 — "aggressive style enforcement" versus "adopt cheap deterministic rules" was never chosen.**
`internal/stylegate/gate_test.go:1-21` frames the engine as *"holds them with a measured, per-rule ceiling, so
the number can only go down"* and carries the per-rule reasons — but no document states whether the intent is
a *capped* enforcement of a broad style surface (current shape: 163…0 ceilings) or *elimination* of style
issues entirely. A ceiling of 163 with an explicit "the ratchet may only go down" is defensible as either. The
two intents have different endpoints — one stops at an accepted level, the other runs to zero — and nobody
wrote down which one owns the number.

**N2 — a flake registry is created, and whether a `RACE` class belongs beside it was never decided.**
`testdata/flakes.tsv` + the registry gate deal with the ORDER/DETERMINISM class and explicitly reject a row
whose cause column is not a deterministic mechanism, because *"a registry that cannot be told from a list of
excuses is worse than no registry"* (`internal/standards/flake_registry_gate_test.go:1-30`,
`testdata/flakes.tsv:1-12`). A live race reported as a flake (ledger `01a0b402`: `TestMergerRestartDoesNotTouchTheTakeoverInFlight`
at `cmd/agent/merger_restart_e2e_test.go:677`, flaky with "two distinct assertions; lock inherited on fd 3
vs take-over being killed") has **no home** in the registry — the gate
would reject it as an excuse, correctly, and correctly leaves it nowhere to be recorded. The catch mechanism
(`make flake-catch`) is itself documented as *"built for the ORDER class, not the RACE class"*. Whether a
second class is needed is unrecorded. *Weakest link:* the row's own text was truncated at 130 characters in my
query; I read its classification (ORDER vs RACE) from the phrase "two distinct assertions" and the registry
gate's own boundary text, not from the full row.

---

## 7. Implications for tests

This section **names existing tests whose verdict the requirement changes** — and the direction. No tests are
written here. Two directions appear: **tighten** (the test should now also assert the behaviour the requirement
names) and **loosen** (the test currently forbids a behaviour the requirement permits — the failure class the
series is built to find).

### 7.1 The gate that would forbid CORRECT behaviour — carried by the test, not by the gate

**The finding.** `internal/tools/task_finish_cleanup_test.go:202` and `:923` assert

```go
t.Errorf("no steps may run on a dirty worktree: %+v", rep.Steps)
```

The requirement (§1's own worked instance, `devdoc/requirements/00-method-and-standards.md:46-58`, ledger
`01a0aabb` "the tests ASSERT the bug", and the operator's verbatim complaint recorded there) is:
**the push is harmless and must RUN; the removal is destructive and must DEFER.** "no steps may run" asserts
the stronger, wrong rule: it makes the harmless step a violation. Today it is *partly* repaired — the producer
side is pinned by a separate regression test that fails if the site carries an `// no-wrap:`-style escape
(`internal/tools/task_merge_wrapchain_test.go`), and the dirty-worktree path has its own coverage — but the two
assertions above **still encode the wider rule**, and a future agent reading them learns the wrong one.

| test | file:line | direction | why |
|---|---|---|---|
| `TestFinishCleanupChainDirtyWorktree` (epic case ②) | `internal/tools/task_finish_cleanup_test.go:202` | **loosen** | "no steps may run on a dirty worktree" forbids the harmless push |
| the server-recycle arm | `internal/tools/task_finish_cleanup_test.go:923` | **loosen** | same assertion, same wrong requirement, in the second arm |

### 7.2 The same site has FOUR instruments giving the same question FOUR answers

Named in §6.2 F1. The requirement ("a deliberately-kept `%v` is an auditable exception") changes the verdict
of the instruments that **cannot represent the exception**:

| test | citation | direction | why |
|---|---|---|---|
| `TestStandardGates` (gate 1, Rule 16) | `internal/standards/gate_test.go:90`; census §4.2 | **loosen** | a baselined or escaped site PASSES — the gate accepts the exception |
| `TestGate1ErrorWrapRedProof` | `internal/standards/gate_test.go:154` | **tighten** | the probe proves red/green on snippets only; it cannot see a *whole-tree* second instrument disagreeing |
| `TestStandardGateDataReport` | `internal/standards/gate_test.go:120` | **tighten** | it prints the escaped site (visible, §4.3) but not that another instrument counts it as a violation |
| `TestStyleGate` (errorf-verb ceiling) | `internal/stylegate/gate_test.go:128` | **loosen** | it fails at *measured > ceiling* for a site the standards gate calls deliberate — the exception is invisible here and the ceiling carries it instead |
| `TestGateCeilingsAreReachable` | `internal/stylegate/gate_test.go:193` | **tighten** | it only checks a ceiling is a non-negative number for a known rule — it cannot detect that the ceiling is silently standing in for an escape |
| `TestTaskMerge_NoMergeToAbortKeepsTheChain` (the wrap-chain regression) | `internal/tools/task_merge_wrapchain_test.go:42`, assertion at `:83-88` | **loosen** | it FAILS on the `// no-wrap:` escape — i.e. it forbids the mechanism, in one direction, without recording the other direction's permit |

**Direction of the whole family:** the requirement is "one decision, and every instrument must agree with it".
Six tests disagree today, and **no single test fails when they disagree** — that absence is the finding.

### 7.3 The gate inventory itself: verdicts the heavy requirement changes

| test | citation | direction | why |
|---|---|---|---|
| `TestHeavyTestInventoryMatchesItsCallSites` | `internal/standards/heavy_inventory_gate_test.go:88` | **tighten** | it enforces inventory↔call-site agreement both ways and rejects a heavy test in a gate package (the §4.4 "gate packages are never heavy" rule); the requirement adds nothing to *this* test — it is the model the others are missing |
| `TestRule27NoDeviceLoadGenerators` | `internal/testutil/rule27_gate_test.go:331` | **tighten** | it reports (never fails) a generator found in a *sibling* checkout; the requirement ("a test must never manufacture load") would make a same-`$HOME` sibling report actionable — the current split is the honest limit, but nothing says which host owns it |
| `TestRule27SyncWriteRedProof` | `internal/testutil/rule27_gate_test.go:514` | **loosen** | it pins that a *bounded, sync-free* fixture write is NOT reported — the exception is the requirement working, and this test is what keeps the gate honest |
| `TestRule27ExemptionsAreNarrowAndReal` | `internal/testutil/rule27_gate_test.go:618` | **losen/tighten** | it already requires the exemption set to stay narrow; the requirement ("no exceptions") makes each remaining exemption a thing to justify in this document, and this test is the instrument that keeps the list open (`devdoc/heavy-tests-rule27-gate.md:116-156`) |
| `TestBoundaryDefaultStaysSmokeOnly` | `internal/heavytest/boundary_test.go:247` | **tighten** | the requirement is that the *default* is smoke; the test asserts it today — the added obligation is that §6.2 F3's schedule gap cannot be closed by making the default heavier |
| `TestBoundaryRefusesEveryWorkSignature` | `internal/heavytest/boundary_test.go:132` | **tighten** | it refuses every work signature; the requirement adds the inverse — a refused heavy run must be *countable*, which `TestBoundaryRefusalWritesTheLedger` (`:264`) already asserts |

### 7.3b The skip-countability requirement: the gate's verdict must change from "has a reason" to "is on the ledger"

| test / gate | citation | direction | why |
|---|---|---|---|
| `TestStyleGate` (`RuleTestSkip`, ceiling 0) | `internal/stylegate/gate_test.go:88,100`; rule `internal/stylegate/stylegate.go:52,1256-1270` | **tighten** | today a skip with a literal reason passes and nothing else is asked; the requirement ("what a smoke run did NOT run stays enumerable") adds the second assertion — the skip is present in the skip ledger, or the run is not a believable smoke |
| `TestBudgetExemptionsStillExist` | `internal/testutil/testbudget_gate_test.go:325` | **loosen (adjacent)** | it is the nearest live "stale exemption fails" mechanism and the model the skip-ledger check should copy; the requirement makes its *shape* mandatory rather than one script's private hygiene |
| ledger-is-written coverage: `TestBoundaryRefusalWritesTheLedger` | `internal/heavytest/boundary_test.go:265` | **tighten** | it asserts the ledger is WRITTEN; no test asserts it is READ, which is exactly O3 |
| `TestHeavyDriftReportAgreesWithTheInventoryAlone` | `internal/standards/heavy_drift_report_gate_test.go:286` | **tighten** | its own header comment (`:8`) states the requirement — a long test *"not in .heavy-skipped.tsv"* is a lead — but the test never reads that file, so the sentence is a doc claim with no instrument |

### 7.4 The adoptability requirement, and the tests that would have to change if the mechanism were unified

| test | citation | direction | why |
|---|---|---|---|
| `TestRepoRootHasNoTrackedJunk` / `TestRepoRootJunkGateScansTheRealIndex` | `internal/standards/scratch_gate.go:9-21`; `internal/standards/scratch_gate_test.go:12,93` | **tighten** | the 457-junk-path incident is the reason the ratchet exists; these are the model for "the gate is what stops the next one" and the requirement (R1 below) would generalise them |
| `TestBaselinesAreSortedAndUnique`, `TestBaselineReasonsAreCarried` | referenced by `devdoc/standards-gate.md:264-279` | **tighten** | they enforce the *baseline* half of adoptability; the requirement's second half — **escapes are enumerated and audited** (F2) — has no test |
| `TestGateCeilingsAreReachable` | `internal/stylegate/gate_test.go:193` | **tighten** | it is the only cross-engine hygiene test; unifying exemptions (F1) means this test's subject widens to cover the escapes too |
| `TestAmbientProfileGateRedProof`, `TestGateCwdRelease_RedProof`, `TestGate7FixtureTopologyRedProof` | `internal/standards/ambient_profile_gate_test.go:271`, `internal/standards/cwd_release_gate_test.go:389`, `internal/standards/worktree_root_gate_test.go:114` | **tighten** | each gate's red proof is the non-vacuity requirement (§4.3); the requirement is that this be true for every gate **and** for every escape's visibility, which the data report covers only in aggregate |

### 7.5 The requirement whose test verdict flips because the requirement is about the TEST INFRASTRUCTURE itself

| test | citation | direction | why |
|---|---|---|---|
| `TestSmokePathPrescribesNoWholeTreeSuite` | `internal/standards/smoke_path_gate_test.go:170` | **tighten** | it fails on a whole-tree command span in any merge/verify-path briefing; the requirement (merge path = smoke, **branch-scoped**, never the whole tree) is now text-checked rather than remembered (`internal/smokegate/smokegate.go:53-81`) — and this is the most direct case in the repo of a rule about *test infrastructure* being enforced by a test |
| `TestScopedCommandStaysOneSingleQuotedArgument`, `TestCommandIsNotTheWholeTree` | `internal/smokegate/smokegate_test.go:20,37` | **tighten** | they pin the one command and its shape — the "one definition, three consumers" need (`internal/smokegate/smokegate.go:1-30`) |
| `TestFlakeRegistryMatchesTheTestsItNames` | `internal/standards/flake_registry_gate_test.go:174` | **tighten** | both directions; the requirement (N2) adds the RACE class it deliberately refuses to hold |
| `TestFlakeCatchMechanismStaysOffTheSmokePath` | `internal/standards/flake_registry_gate_test.go:438` | **loosen** | the catch mechanism is off the smoke path by design — the "off-peak" half of the operator's sentence (§6.2 F3) has a mechanism *here* and nowhere else |
| `TestTestBudgetHasASingleSourceAndNamesATimeout` | `internal/testutil/testbudget_gate_test.go:90` | **tighten** | the budget is the thing that makes a fast merge path possible; the requirement ("my verdict arrives fast") changes this from infrastructure detail to a stated need |

### 7.6 Tests that carry an exception the requirement would erase

| test | citation | direction | why |
|---|---|---|---|
| `TestGate2TestEnvRedProof` | `internal/standards/gate_test.go:195` | **loosen** | it pins the `os.Setenv` call form as red; the requirement (F2) says the *shape* rule and the 23 escapes must stay representable — a naive tightening would re-break the per-call flip case (F1's own defect) |
| `TestSetenvScopeIsPerCall`, `TestPerCallFlip` | `internal/stylegate/stylegate_test.go:255,268` | **loosen** | these exist because an automated `os.Setenv`→`t.Setenv` conversion **broke** `cmd/agent`'s dispatch test: `t.Setenv`'s undo scope is the TEST, so a per-call flip needs the explicit pair. Any requirement that "all env writes become `t.Setenv`" is refuted by these two tests — and by `internal/testutil.SnapshotEnv` (`internal/testutil/envsnapshot.go:46`) |
| `TestRule27ExemptionsAreNarrowAndReal` | `internal/testutil/rule27_gate_test.go:618` | **loosen** | same shape as above: the requirement "no exceptions" is *not* what the test enforces, and the test is right |

---

## 8. Operator constraints (the six filters, applied)

| constraint | how this module sits against it |
|---|---|
| **C1 — one static binary, zero dependencies** | **Satisfied, and by construction.** Every gate is stdlib-only (`go/parser`, `go/token`, `go/ast`, `regexp`, `strings`, `sort`, `strconv`, `unicode`, `testing`) — `devdoc/standards-gate.md:254-262` ("**None added.** … zero `go.mod` change"), `internal/standards/gate.go:1-3`, `internal/stylegate/stylegate.go:1-21` (type info from `go list -export -json -deps`, the compiler's own export data — no `golang.org/x/tools`). |
| **C2 — the operator must be able to control the whole thing** | **Satisfied at the surface, with one friction.** The control surfaces are plain and inspectable: `make test`, `make test-heavy`, `make test-budget`, `make flake-catch` (`Makefile:28,35,40`), and the gate run is `go test ./internal/standards/`. Friction: §6.2 F1 — the operator must hold **two** engines' mental models (standards vs style) and remember that one honours an inline escape and the other does not. |
| **C3 — a daemon must be simple** | **Not applicable directly** — no daemon here. Where the spirit applies it is honoured: `internal/smokegate` is a **leaf package with constants and no logic** (`internal/smokegate/smokegate.go:1-30`), and the heavytest boundary has exactly **one** live-process predicate, `WorkRefusal()`, which the tested predicate *is* (`internal/heavytest/boundary.go:144-168`). |
| **C4 — the agent must clean up after itself** | **Satisfied and extended to the machine.** The scratch-hygiene gate makes "junk is tracked" a build failure (`internal/standards/scratch_gate.go:9-21`), the ambient-profile gate stops tests writing into the operator's real `~/.gopi` (`internal/standards/ambient_profile_gate_test.go:1-21`), and Rule 27's check exists because the machine was treated as a workspace (`GOPI.md:906`). |
| **C5 — a small team, limited resources** | **The module's raison d'être, and the source of its honest cost.** It is zero-dependency and needs no CI (`devdoc/standards-gate.md:296-298`), it lands green on day one so it never needs a big-bang migration, and it caps review effort (`maxBaselineEntries = 400`, ratchet target ~130, `internal/standards/gate.go:44-58`). The cost it does add: the escape audit is manual and one-sided (§6.2 F2), and 42 heavy rows plus 163 ignored-error ceilings are a standing review surface. |
| **C6 — `why` over `what`** | **The module's whole thesis, stated in its own code:** `devdoc/standards-gate.md:29-46`, `internal/standards/gate.go:5-8`, and `GOPI.md:1145`'s *"checked, not trusted"*. This is the one module where the operator's diagnosis is directly the subject. The gap it still has is §6.3 O2 — the *relevance* criterion ("does this rule touch a need the operator holds?") is not recorded anywhere, so a rule can be deterministic, cheap, green-on-landing, and still police style. |

---

## 9. Findings new to the operator (which sentence is the finding)

1. **O3 — the artifact built to make skipped tests enumerable is written on every `make test` and read by
   nothing.** `make test` sets `GOPI_HEAVY_SKIP_LEDGER=.heavy-skipped.tsv` (`Makefile:29`), `heavytest` appends
   one line per skip (`internal/heavytest/heavy.go:150`), and the doctrine's stated purpose is *"what a smoke
   run did NOT run stays enumerable"* (`internal/heavytest/heavy.go:24-30`) — but **no gate, report or script
   reads its contents** — the path occurs in three non-test/non-doc places (the writer `Makefile:29`, a refusal
   message `internal/heavytest/boundary.go:107`, and a comment `scripts/heavy-drift-report.sh:12`), and
   `scripts/test-budget.sh` never mentions it. Meanwhile `RuleTestSkip` (`internal/stylegate/stylegate.go:52`, ceiling **0**) checks only that
   a skip has a literal *reason*, so a skip with a perfect reason string can remove coverage with nothing
   objecting. **This is the finding the operator did not already know**, and it is a *substitution*: the gate
   enforces the reason, the need was the count.
2. **F4.1, RETRACTED (and the retraction is itself the finding — kept in full in §6.2).** I first reported the
   `t.Skip` at `internal/standards/heavy_drift_report_gate_test.go:252` as a *deleted* guard, on the strength of
   an empty `git log -S`. It is neither deleted nor dead: **commit `1219e8b4`** (2026-09-18) *added* the file and
   that line, and the test **PASSES** (`go test -v -run TestHeavyDriftReportNamesAnUnclassifiedSlowTest
   ./internal/standards/` → PASS, 0.10s) because the inventory carries **9** `internal/tools/` rows. It is a
   negative control against a vacuous pass. The lesson — *an empty negative search is evidence about my query
   string, not about the tree; run the test* — is a requirement about this document class.
2. **F1 — the same question has four instruments giving four answers.** A deliberately-kept `%v` is *pass*,
   *pass-but-visible*, *fail-vs-ceiling*, and *fail-on-the-escape-itself* depending on which of the four
   readers looks (`internal/standards/gate_test.go:27` vs `internal/stylegate/gate_test.go:23-83` vs
   `internal/tools/task_merge_wrapchain_test.go:83-88`).
3. **F2 — 538 `stylegate:ignore` markers are the real exception population, and nothing counts or audits
   them.** The *baselines* are audited in both directions; the inline escapes are not enumerated in any
   machine-readable place, and the one place I could compare counts (29 `env-leak-ok` vs 538
   `stylegate:ignore`) required knowing both spellings by heart.
4. **F3 — half the heavy inventory carries no criterion, and nothing schedules the off-peak half of the
   operator's own sentence.** The operator said the expensive tests "can only be run periodically at a offpeak
   period" (ledger `01a0a88e`); no mechanism runs them periodically.
5. **O1 — `RuleTestChdir` is a style rule with a zero ceiling and no ask behind it** — the cleanest instance of
   "a gate policing style rather than user need", and the reason §6.3 O2's missing relevance criterion matters.
6. **The §7.1 family is still live.** `internal/tools/task_finish_cleanup_test.go:202` and `:923` still assert
   *"no steps may run on a dirty worktree"*, which forbids the harmless push — the exact
   correct-assertion/wrong-requirement shape this series exists to find.

---

## 10. Confidence and limits

| claim | confidence | basis / weakest link |
|---|---|---|
| the eight registered gates and their live counts | **high** | produced in this session by `go test -v -run 'TestStandardGates\|TestStandardGateDataReport' ./internal/standards/`; all PASS, `NEW=0`; the numbers are the run's own output |
| `devdoc/standards-gate.md`'s 352/120 baselines are superseded | **high** | that document says so (`:12-18`); the live run reads 6/91/2 baselined |
| each gate's motivating defect | **medium-high** | the rule text (`GOPI.md:538-716`) and the gate's own header comment carry it; the *original incident* behind Rules 16–20 is the coding-standards checklist, not a ledger row — `GOPI.md` does not name a task id for Rules 16–20, so "which incident" is **INFERENCE** from the checklist's Top-8 execution list |
| heavy classification criterion + floor + gate-packages-never-heavy | **high** | `internal/heavytest/heavy.go:69-96`; `devdoc/heavy-tests-rule27-gate.md:31-47`; enforced by `TestHeavyTestInventoryMatchesItsCallSites` |
| Rule 27's three incidents and the retired instrument | **high** | `devdoc/heavy-tests-rule27-gate.md:104-179`; `GOPI.md:906` |
| the heavy boundary's motivation | **high** | `internal/heavytest/boundary.go:11-45`, with the operator's absolute rule quoted in ledger `01a0b1b5` |
| F1 — four instruments, four answers | **medium** | the four instruments are real and cited; **weakest link**: I read the four I found, so a fifth (a doc, a reviewer convention) could exist |
| F2 — escape population 29 vs 538 | **medium** | both counts are greps I ran; **weakest link**: the two mechanisms use different spellings, so a third spelling could exist and be uncounted. `devdoc/standards-gate.md:128-145` gives 26 markers on-line out of 91 baseline entries for gate 2 — the *shape* of the finding, not a complete census |
| F3 — ~21 of 42 rows carry no criterion | **medium** | `grep -vc '^#' testdata/heavy-tests.tsv` → 42 is a fact; the 41-row derivation is the devdoc's arithmetic (`:49-58`); **weakest link**: the devdoc calls 41 "at the time of this change", so the drift may be re-derivation or later additions — I did not diff the two sets row by row |
| F3 — nothing schedules the off-peak run | **medium-high** | no schedule unit, cron entry or daemon is cited anywhere in the heavy docs; the only "periodic" mechanism in the tree is the flake catch, which is itself off-path and manual (`internal/standards/flake_registry_gate_test.go:438`) |
| F4 (the coverage half) | **low** | **weakest link:** I did not measure unit coverage; "not merged work" is inferred from row summaries, and a coverage threshold is not something this series may invent. A reader must treat this as a lead, not a verdict |
| F4.1 (retracted) / O3 (the finding that replaced it) | **high** | the retraction rests on three measurements reproducible in one command each: `git log --all --oneline -S "classified control"` → `1219e8b4`; the run → `PASS`; `grep -c 'internal/tools' testdata/heavy-tests.tsv` → 9. O3's "no reader" claim is an exhaustive grep over the tree including docs, scripts, Makefile and `.gitignore`; **weakest link:** a reader could exist *outside* the tree (an operator's own script), which I cannot see |
| O1 (`RuleTestChdir` = no ask behind it) | **medium-high** | every other gate in this document traces to a rule text, an incident or a ledger row; `RuleTestChdir` traces to none, and the absence is the finding. **Weakest link**: absence of evidence in the places I searched is not proof no one asked |
| N1 / N2 | **low** | stated as "never decided"; the honest form is that I found no record, not that no record exists |
| §7.1's direction (loosen) | **medium-high** | the requirement is reconstructed in `devdoc/requirements/00-method-and-standards.md:46-58` and the operator's own complaint is ledger `01a0aabb`; the assertions are quoted verbatim from the file at master `32932668` |

---

## What I could not determine

1. **What (if anything) actually consumes `.heavy-skipped.tsv`.** No reader exists in the tree — but the
   operator may review it by hand, and the doctrine was written as if a reviewer would. The file is
   git-ignored (`.gitignore:87`), so there is no artifact of a past review to check either way.
2. **Whether unit coverage is still thin three weeks after ledger `t-178817`.** The coverage campaign exists as
   22+ filed rows; no coverage threshold or measurement is cited, and I did not run one (and may not invent the
   acceptance).
3. **Which single incident motivated Rules 16, 17, 18 and 20.** `GOPI.md` names the checklist row for each but
   no task id; Rule 17's `os.Setenv` leak has a real row (`01a08975`) and Rule 21 has a named defect
   (`internal/standards/env_reader.go:3-9`), so the pattern asymmetry is real.
4. **Whether a third exemption mechanism exists beyond the five standards markers and the two stylegate
   markers.** I found seven spellings; nothing enumerates the set.
5. **Whether the style engine *should* honour the standards engine's escapes.** §6.2 F1 shows four answers to
   one question; whether that is a defect or an intended layering is not recorded.
6. **What "periodically" means for the operator's off-peak sentence** (ledger `01a0a88e`): weekly, monthly, or
   only when something looks slow. No number and no mechanism exist.
7. **Whether the 457 junk paths of the scratch-hygiene gate are fully swept.** I read the incident and the
   gate; I did not re-derive the current count from `git ls-files`.
8. **Whether the gate text or the gate check is the defect in O3.** `RuleTestSkip`'s own recorded reason
   ("every t.Skip now carries a literal reason") describes what the check does, and the doctrine describes the
   need ("countable"); whether the original author intended the count check and dropped it, or never intended
   it, is not recorded anywhere I could find.

11. **Is a skip that removes coverage the OPERATOR's problem or the gate's?** §5 (6) was my reconstruction of
    the operator's need from the doctrine's own phrase ("tells me which expensive tests it did not run",
    `Makefile:22-30`, `internal/smokegate/smokegate.go:42-51`). It appears in **no** `source=human` ledger row I
    could resolve: the row ids the heavy devdocs cite (`01a0a88e`, `01a0b1b5`) are not in the local ledger at
    read time, and `devdoc/heavy-tests-rule27-gate.md:83-95` is agent-authored prose. So §5 (6)'s ledger handle
    is a **primary-source gap**, not an inference about the file — marked here rather than in the finding, since
    it weakens O3's framing (the *artifact* is real and unread; whose ask it answers is the open half).

---

## Verification of this document

**Build + test: NOT APPLICABLE, honestly.** The diff is one Markdown file
(`git diff --name-only 32932668..HEAD` → `devdoc/requirements/test-infrastructure.md` only — the branch's own
base, *not* `master..HEAD`: `master` has since advanced past this branch's base with other worktrees' merges and
is not this branch's review surface); there is no Go
change for `go build`/`go test` to judge, and running the suite would measure the tree, not this document.

**The substitute gates, and their output** (all run in this worktree, over the committed blob):

| gate | command | result |
|---|---|---|
| section presence | `grep -nE '^## ' <doc>` over the committed blob | 12 own `##` headings: the 8 mandated (§1–§8) + §9 findings new to the operator + §10 confidence and limits + `## What I could not determine` + this section — every mandated heading greppable by its own title |
| citation resolution | every `path:line` / `path:a-b` in the committed blob resolved against the branch base `32932668` (`test -f <path> && wc -l <path>`) | **ok=119, bad=0, bare-handle=0** — every `file:line` anchor names a path that exists and a line inside the file. Three earlier "failures" were artifacts of the extractor (substrings of longer anchors); the only genuinely bare tokens left are quoted *filenames inside prose*, and the one historical file among them (`iopress.sh`) was never committed — Rule 27 records it as hand-written into a worktree, so it has no path to cite |
| cited test-name resolution | every `Test*` token in the blob searched as `func <Name>(` across the tree | **37 cited, 37 resolve, 0 unresolved** (the extractor excludes the English word "Tests") |
| commit-SHA resolution | `git cat-file -t <sha>` for every 7+-hex token | The 14 hex tokens divide exactly: **2 resolve as git objects** (`32932668` the base, `1219e8b4` the commit that added the drift-report gate), **12 are ledger row ids** (`01a0…`, cited as rows, not commits) — so 0 tokens are *claimed* to be unresolvable. |
| number reproduction | the three counts the findings rest on | `grep -rn 'env-leak-ok' --include='*_test.go' internal cmd testsuite \| wc -l` → **29**; `… 'stylegate:ignore' …` → **538**; `grep -vc '^#' testdata/heavy-tests.tsv` → **42**; `grep -c 'internal/tools' testdata/heavy-tests.tsv` → **9** |
| the gate census | `go test -v -run 'TestStandardGates\|TestStandardGateDataReport' ./internal/standards/` | PASS, eight gates, `NEW=0` for every one (the run's own output, §4.2) |
| Rule 27's own discipline | no load was generated to produce any number here | the census and the counts are reads of an existing tree; the one behavioural check I ran (`-run TestHeavyDriftReportNamesAnUnclassifiedSlowTest`) is a synthetic-stream unit test, 0.10s |
| worktree cleanliness (Rule 30) | `git status --porcelain` in this worktree at close | prints nothing |

**Deliverable statement (Rule 6d).** Branch **`docs/req-test-infrastructure`**, worktree
`/home/user/workspace/gopi-req-test-infra`, off master `32932668`. A linked worktree shares the main repo's
refs, so the commit is already in the main repo — recover the file with
`git show docs/req-test-infrastructure:devdoc/requirements/test-infrastructure.md`. No other worktree was
entered (Rule 29).

---

*Read at master `32932668`. Every gate claim in §4.2 is the census of a run performed in this session; every
`file:line` resolves at that revision; every cited test name resolves via `grep -rn "func <Name>("`.*
