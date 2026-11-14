<!-- doc-class: historical -->
# Task Report: GOPI.md Rule 23 — Never Continue Work on Someone Else's Failed Branch

**Task**: `01a0989c-cc0a-7358-bb93-f75c22323317` — "docs(GOPI.md): never develop on
someone else's failed branch — open a new one (branches are free)"
**Branch**: `docs/no-touching-failed-branches` (worktree `~/workspace/gopi-no-touch-others`)
**Type**: docs-only (one file changed: `GOPI.md`; plus this report)

## Completed

- Operator's general development principle distilled into **GOPI.md Rule 23** —
  *"Never Continue Work on Someone Else's Failed Branch — Open a Fresh One"* —
  with the four required halves in ~19 lines: **ban**, **correct practice**,
  **encouraged reference-reading**, **counter-example**, **why**.
- Placement decided by reading all 22 existing rules first: **appended as
  Rule 23**, not folded into an existing rule (rationale below), with a one-line
  cross-reference to Rule 9 (the archive mechanism = this rule's material basis).
- Non-duplication proven mechanically (18 sentences scanned against Rules 1–22;
  0 verbatim repeats — same predicate as the Rule 22 doc test).
- `go build ./...` green; prompt-injection tests green (evidence below); a real
  short session confirms GOPI.md still injects.

## The operator's six reasons (原话，按原意落进 GOPI.md)

Operator's framing (2026-09-13), quoted verbatim:

> **不要碰别人的代码。它失败了就失败了。你重新开一个 branch —— 很便宜，为什么
> 不新开一个？又不要钱！！！**

1. **一个很难的 bugfixing / feature 是需要多次碰壁的。**
2. 那个分支记录的是**前人走过的路径**——包括那些**看起来错、但不一定是错的尝试**。
3. 在他的分支上改，会**掩盖原来的思路**，看不出"为什么走到这一步"，
   相当于**不同的人在同一份代码上拉扯**（不同的人有不同的路径）。
4. **失败了就失败了，保留现场**——失败的尝试本身是**信息**，不是垃圾。
5. **新开分支的成本是零**——为了省一个零成本的东西去破坏有价值的记录，是算错了账。
6. 可以**参考**前人的工作（去 archive / 分支里看），但**必须保留之前的尝试**
   —— 因为一个问题**可能会反复**，那条老路径以后可能还要回来看。

Point 5 supplies the cost asymmetry that makes the rule *decisive* rather than
balanced: the thing being protected (a recorded failed path) is valuable and
irreplaceable, the thing being saved (one `git worktree add`) is free. Equality
of evidence is not needed — the comparison is zero-cost vs. destroying
information.

## The rule as landed (GOPI.md Rule 23)

| Required element | Where it lands |
|---|---|
| **Ban** | "do not check out a branch someone else's attempt failed on and commit there — not a failed/archived branch" |
| **Correct practice** | "open a FRESH branch from master (`git worktree add ~/workspace/gopi-<task> -b feat/<name>`)" + "Branches cost nothing." |
| **Encouraged reading** | "DO read the old branch first — `git log <branch>`, `git show <branch>:<file>`, `git diff master...<branch>` — the failed path is information, not garbage." |
| **Counter-example** | "`git checkout fix/some-failed-attempt` → edit → commit" + the consequence in the *Why*: "editing on top of it erases that reasoning, so nobody can see why it got there — two people pulling one diff." |
| **Why** | walls are required; the branch records the path walked (incl. attempts that only *look* wrong); failed attempts are evidence kept on purpose; the problem may recur |
| **Cross-ref** | "Ref: Rule 9 (archive keeps the branch = keeps that path)." |

## Placement — why Rule 23, not a merge into an existing rule

I read all 22 rules before writing (`GOPI.md` is injected every round; length is
attention cost, so merging was the preferred outcome if it fit).

**Checked and rejected as the landing site:**

- **Rule 6 / 6a (worktree + isolated work directory)** — closest neighbour, and
  the *mechanism* it prescribes (worktree from master) is shared. But Rule 6 is
  about **where** you work (never the main dir; worktree is the standard;
  clone is the exception) and about **getting the branch back**. It says nothing
  about *whose* work you continue, and its subject is the isolation of a NEW
  task. Folding this in would have meant a rule about a branch you are told
  *not* to touch inside a rule about branches you *create* — one heading, two
  orthogonal constraints, and the "reading is encouraged" half (the point that
  makes it a principle rather than a fence) would read as an exception to
  Rule 6 rather than as the rule's own constructive half.
- **Rule 9 (竞争收尾 / archive)** — the closest *semantic* neighbour, and the
  one the rule now cross-references. Rule 9 is the **closeout procedure** for a
  competition (winner merges; loser is tagged `archive/<name>-<date>`, branch
  deleted, worktree removed; `safe-merge.sh` gate). Rule 23 is a **development
  discipline** that applies to an already-archived/failed branch *later*, in a
  different session, for a different task. Merging would also have made Rule 9
  ~2× longer for a reader looking for the cleanup recipe.
- **Rule 15 (no silent failure)** — shares only the abstract theme "don't erase
  evidence"; it is a code-level error-handling rule. Rejected as too far.
- **Rule 22** — the most recent append; states a *criterion* with mechanical
  halves. Rule 23 is likewise prose + a criterion, but a distinct one.

**Decision: append as Rule 23.** Rationale in the Rule-22 precedent's own terms
(`devdoc/tmpdir-settings-injection.md` §C): it states a **different constraint**
from its neighbours; rule numbering is stable by convention (the append-don't-
renumber discipline recorded for Rules 13–15), no test depends on a rule count,
and the addition is proven non-duplicative by the same mechanical check
(`>= 60`-rune normalized sentence must not appear in the text before it).
Keeping it separate also keeps the *identity* of the rule readable: a future
agent grepping GOPI.md for "failed branch" finds a heading that says exactly
that.

## Why this is a principle, not a preference

A preference is a default you may skip when you have a reason; a principle is a
constraint on the *reasoning itself*. This rule bans a **category of action**
(committing onto someone else's failed branch) because the harm is not a
quality/taste loss — it is **destruction of information that cannot be
reconstructed**, and the cost of avoiding it is exactly zero.

Three properties make the principle framing correct rather than rhetorical:

1. **The loss is irreversible and unauditable.** Once you edit and commit on
   top, the recorded path (which order the walls were hit, which attempt looked
   wrong but was a step toward the answer, what was tried and abandoned) no
   longer exists as a readable artifact. The commit graph still exists, but the
   *reasoning* — the thing an agent or human later needs — is gone. You cannot
   recover it from the merged result.
2. **There is no cost side to trade against** (operator point 5). Rules are
   usually heuristics with a price; this one has none — `git worktree add ... -b
   feat/<name>` is free. When one option is free and the other destroys
   evidence, "it depends" is the wrong frame.
3. **It is not about politeness/ownership but about information fidelity.**
   Rule 23 does not forbid *using* the old work — it *requires reading* it
   (`git log` / `git show` / `git diff master...<branch>`). The failure mode it
   targets is **silent history rewriting by accretion**: the old branch's
   commits become the base of a new line of development, so a later reader
   cannot tell which reasoning was the failed attempt's and which is the
   current one — exactly the operator's point 3 ("不同的人在同一份代码上拉扯").

Corollary worth stating: the rule protects the *next* developer too — the one
who hits the same wall. Point 6 ("一个问题可能会反复") means the archived branch
is a lookup, not a graveyard.

## Relationship to the archive mechanism (Rule 9 + `safe-merge.sh` + tasktags)

The archive mechanism is this rule's **material basis** — the rule is only
obeyable if the failed path is *retrievable*, and Rule 9 makes it retrievable:

- Rule 9 step 2 preserves the loser: `git tag archive/<name>-<date>` +
  `git branch -D <name>` + `git worktree remove <path>`. The **tag** keeps the
  path reachable after the branch ref is deleted — precisely so a later task can
  `git show archive/<name>-<date>:<file>` / `git diff master...archive/...`.
- `scripts/safe-merge.sh` (Rule 9 merge guard) refuses to merge an
  archive-tagged or `loser/*` / `archive/*` branch — so an archived path can
  never be silently landed *as if* it were the winner's work. Rule 23 and the
  guard protect the same artifact from opposite ends: the guard stops the dead
  path from entering master, Rule 23 stops a developer from *editing* it and
  losing it.
- The rule's phrase "Ref: Rule 9 (archive keeps the branch = keeps that path)"
  is deliberate: it tells the reader *where the old path lives*, which is what
  makes "read it, don't edit it" actionable rather than merely restrictive.

**A tension found during this task (reported, NOT changed here).** The
merger's branch-rework briefing (`internal/tools/repo_resolve_row.go`
`resolveRowSubmitRework`) tells its agent: *"Check out branch %q … Fix the
branch so it merges cleanly into master"* — i.e. it **instructs** a developer to
continue work on a branch a previous attempt failed on. Its surrounding comment
states the branch REF is preserved and nothing is force-pushed, and a rework row
is a *dedicated* claim of that branch (not a silent accretion), so it is not a
violation of the operator's intent in spirit — but the briefing's phrasing is
exactly the counter-example Rule 23 forbids, and an agent holding both texts
gets contradictory instructions. Per the epic's constraint I did **not** touch
either the briefing or the kernel prompt; this is recorded for the operator to
rule on. Two candidate resolutions (for a separate task): (a) the rework brief
says "rework = a NEW branch seeded from the failed branch, preserving it", or
(b) the rework brief explicitly whitelists itself as the *owner* of that branch.
Deciding (a) vs (b) is a mechanism change, out of this task's scope.

## Suggestion for a future mechanical gate (recommended, NOT implemented)

The rule's own methodology note applies: *"原则无效，检查才有效"* — a prose rule
about not touching things is exactly the kind that gets skipped. Two candidate
checks, cheapest first:

1. **Queue-side assertion (cheap, deterministic, no AST).** A test over the
   briefing builders: every task payload that instructs a developer to work on
   an existing (non-fresh) branch must *either* contain a "fresh branch"
   instruction *or* carry an explicit owner-whitelist marker. This closes the
   `resolveRowSubmitRework` tension above and is a pure string/AST-free test on
   `internal/tools`. Precedent for "assert a doc/prompt carries the required
   section": `internal/agent/gopidoc_sync_test.go` (Rule 3 inventory sync) and
   `internal/testutil/tmproot_assert_test.go` (Rule 22 heading + needles).
2. **Behavioral gate (stronger, more machinery).** Detect the forbidden action
   itself: a commit whose parent chain includes a commit reachable from an
   `archive/*` tag, or a worktree/branch left checked out on an archive-tagged
   branch. This is the analogue of `scripts/safe-merge.sh` but at *development*
   time rather than merge time. It needs a hook point (there is none today) —
   a bigger task, and it should reuse the existing `internal/tasktags`
   `ArchivePrefix` constant rather than re-spelling the literal (Rules 1b/13/14).

Recommendation: land (1) first — it is a `go test` away, needs no hook, and it
addresses the concrete contradictory-briefing case found above.

## Not Done

- **No kernel-prompt change** (per the epic's constraint). My assessment, argued
  rather than acted on: this rule **belongs in GOPI.md and not in the kernel**.
  GOPI.md is the per-project, cwd-read rules file (Rules 6/12 architecture;
  `internal/agent/loop.go` reads `GOPI.md` from cwd); the kernel is supposed to
  carry no project-specific content (`tasks/system-prompt-generalization-review.md`
  found the kernel already carries too much GoPi-specific text — adding more
  would move in the wrong direction). "Never build on someone else's failed
  branch" is also a **git-workflow** rule, and a repo without git or with a
  different branch model is not bound by it — exactly the project-specific shape
  GOPI.md exists for.
- **No mechanical gate implemented** (per the epic's constraint) — recommended
  above with two concrete candidates.
- **The `resolveRowSubmitRework` briefing was not modified** — reported as a
  tension for the operator (above).
- **Rule 9's text was not extended** — Rule 23 cross-references it instead, to
  keep Rule 9's closeout recipe intact and GOPI.md's injected size minimal.

## Verification

- **Build**: `go build ./...` → exit 0 (in the worktree).
- **Non-duplication** (same predicate as the Rule 22 doc test, 60-rune
  normalized sentences): `18 sentences checked, dupes: 0`.
- **Prompt-injection tests** (GOPI.md injection contract):
  ```
  go test ./internal/agent/ -run 'TestGetMessages_|TestGOPIRule3_MatchesClaudeToolDefs|TestEstimatedTokens_IncludesProjectContext' -v
  ```
  → all PASS (system + project-context second system message unchanged;
  Rule 3's tool inventory still matches `claudeToolDefs`).
- **Rule 22 doc tests** (prove the rule-file doc scan still parses, incl. its
  "nothing after Rule 22 verbatim-duplicates earlier rules" check):
  `go test ./internal/testutil/ -run 'TestGOPI' -v` → PASS.
- **Live injection**: a real short session run from the repo root prints
  `[gopi] loaded GOPI.md (<N> bytes)` and the session's request log shows the
  Rule 23 text present in the injected project context (evidence quoted below).
- **Commit**: `docs/no-touching-failed-branches` (hash in the final message).

### Raw evidence

```
$ go build ./...
exit=0

$ python3 <60-rune normalized sentence probe over GOPI.md>
sentences checked: 18 dupes: 0

$ go test ./internal/agent/ -run 'TestGetMessages_|TestGOPIRule3_MatchesClaudeToolDefs|TestEstimatedTokens_IncludesProjectContext' -v
--- PASS: TestGetMessages_ProjectContextInjection (0.00s)
--- PASS: TestGetMessages_NoProjectContext (0.00s)
--- PASS: TestGetMessages_NoSystemPromptWithProject (0.00s)
--- PASS: TestEstimatedTokens_IncludesProjectContext (0.00s)
--- PASS: TestGOPIRule3_MatchesClaudeToolDefs (0.00s)
ok  github.com/nick/gopi/internal/agent  0.037s

$ go test ./internal/testutil/ -run 'TestGOPI' -v
--- PASS: TestGOPIHasTheTempRootCriterion (0.00s)
--- PASS: TestGOPITempRootCriterionIsNotADuplicate (0.00s)
    tmproot_assert_test.go:516: Rule 22: 74 sentences checked, none duplicated from Rules 1–21
ok  github.com/nick/gopi/internal/testutil  0.029s

$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → BUILD_OK (bin/gopi 21601316 bytes)

# live injection (real session, cwd = the worktree, this branch's GOPI.md)
$ echo "Reply with exactly: OK" | ./bin/gopi
[gopi] loaded GOPI.md (38468 bytes)

# the SAME session's raw request log — the injected project context carries Rule 23
$ grep -o "Rule 23: Never Continue Work on Someone Else's Failed Branch — Open a Fresh One" <raw.log> | head -1
Rule 23: Never Continue Work on Someone Else's Failed Branch — Open a Fresh One
$ grep -o "Ban\*\*: do not check out a branch someone else" <raw.log> | head -1
Ban**: do not check out a branch someone else
$ grep -o "git checkout fix/some-failed-attempt" <raw.log> | head -1
git checkout fix/some-failed-attempt
$ grep -o "Encouraged\*\*: DO read the old branch first" <raw.log> | head -1
Encouraged**: DO read the old branch first — `git log
$ grep -o "Branches cost nothing" <raw.log> | head -1
Branches cost nothing

# isolation self-check (Rule 6b): edits landed ONLY in the worktree
$ cd ~/work/GoPi && git status --short        → (empty)
$ cd ~/workspace/gopi-no-touch-others && git status --short
 M GOPI.md
?? devdoc/no-touching-failed-branches.md
```

Session id with the injection: `771d2744-bb15-4688-b954-cbbbbb28e6a8`
(`~/.gopi/sessions/771d2744-bb15-4688-b954-cbbbbb28e6a8/…raw.log`, 8 occurrences of the
Rule 23 heading = one per logged request payload: the introduction is sent on
every round → the rule is resident, not a one-shot).
