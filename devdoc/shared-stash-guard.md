<!-- doc-class: normative -->
# The shared-stash guard: a stash mutation is refused in a repo with linked worktrees

**Status:** landed (`internal/tools/shared_stash_guard.go`, wired into
`bashInDir` step 2c). Standing claim about master — the guard, its thresholds
and its constants resolve in the tree.

## The collision (measured, three times)

`refs/stash` is a **repository-level** ref: it lives in the repo's COMMON git
dir, which every linked worktree shares. Prove it from any worktree:

```bash
git -C /home/user/work/GoPi            rev-parse --git-common-dir   # /home/user/work/GoPi/.git
git -C /home/user/workspace/<any-wt>   rev-parse --git-common-dir   # the SAME path
git -C /home/user/work/GoPi            stash list                   # sees every worktree's entry
```

A `git stash apply`/`pop` **writes the index and worktree of the checkout it
runs in**. So when two sessions hold different worktrees of one repo:

1. session B leaves uncommitted work in its worktree, another path stashes it
   into the shared stack (or B itself does, from the wrong cwd);
2. session A, in ITS worktree, runs `git stash pop` for its own WIP and gets
   B's tree;
3. the paths A's branch also owns conflict → the apply stops **interrupted**,
   leaving `UU` entries over trunk files (`cmd/agent/cli_verbs.go`,
   `internal/config/*.go`, `internal/pricing/pricing.go` measured);
4. **git KEEPS the entry on a conflicted pop**, so every honest retry applies
   the same foreign tree again.

Measured instances: `devdoc/incident-git-stash-worktree-collision-20260910.md`,
`devdoc/task-session-cwd.md`, and the 2026-09-27 report that produced this fix
(a resident run's `git status --porcelain` non-empty with tracked files its
branch did not own — Rule 30's clean-worktree gate refuses exactly that state,
so the run could not close out).

**Detecting it (the diagnostic that identified the incident):** five `UU`
entries with `MERGE_HEAD` / `CHERRY_PICK_HEAD` / `REVERT_HEAD` / `REBASE_HEAD`
ALL absent ⇒ an interrupted stash apply, not a merge. A stash's file list needs
`git diff stash@{0}^ stash@{0} --name-only` (plain `--name-only` returns 0 — the
stash commit is a merge).

## No production code was the actor

An exhaustive grep of `scripts/`, `testsuite/`, `internal/`, `cmd/` finds no
`git stash` invocation outside comments. The actor is the agent's own `bash`
tool: its cwd is whichever checkout the session was launched in, and a stash
command typed there reaches the shared stack. That is why the fix is a **bash
guard**, not a change to a worktree-transfer routine — there is none to change.

## The guard

`guardSharedStash(command, workdir)` — refuses with a reason when the command:

- contains a stash token that IS the subcommand of a real `git` invocation, in
  a clause of its own (clauses are cut at `;`, `&&`, `||`, `|`, newline) — a
  token inside a path, inside a longer word, or in a clause with no standalone
  `git` command word is not an invocation;
- names a MUTATION verb: `push` (also the bare `git stash` implicit push),
  `apply`, `pop`, `save`, `drop`, `clear`, `branch`, `create`, `store`,
  `import`;
- and the repo at the command's own directory (`-C <dir>` honoured) reports
  **more than one linked worktree** (`git worktree list --porcelain`).

EVERY clause is judged, and the refusal names the MUTATION actually found
(`stashMutationVerb`). A predicate that stops at the FIRST stash invocation lets
a leading read-only clause answer for the whole line — measured, see
"clause-scoped is not clause-exhaustive" below.

Deliberate non-refusals, each because the alternative is a false positive on
the normal case:

| shape | verdict | why |
|---|---|---|
| `git stash list` / `show` | permitted | the diagnosis path; refusing it leaves the operator blind |
| mutation in a 1-worktree repo | permitted | ordinary local stash; stack and checkout are the same person's |
| `grep stash <file>`, no git word | permitted | not a stash command; no probe is even run |
| outside any git repo | permitted | no shared stack to protect; git fails the command loudly by itself |

The refusal text names the repo, the worktree count, the mechanism, and the
NON-DESTRUCTIVE transfer to use instead — `git stash show -p`,
`git diff stash@{N}^ stash@{N} -- <path> | git apply --3way`,
`git show stash@{N}:<path>`, or `git stash branch <name>` — so a blocked
session is redirected, never merely stopped (Rule 15).

`GOPI_ALLOW_DANGEROUS=1` opens the hatch, exactly as it does for the pattern
and shape guards: the operator taking the blast radius is a deliberate act.

**The separator dimension is pinned separately.** The acceptance set covers the
bypass on ONE chain shape; a predicate keyed on a single separator is a
predicate the next command shape walks around, so
`TestSharedStashGuard_RefusesMutationAnywhereInACompoundCommand` covers every
separator that can carry a second command (`;`, `&&`, `||`, `|`, newline), both
orders (mutation first, mutation last, mutation in the middle), a `cd` preamble,
and the epic's exact four-clause chain — each driven through `BashInDir`. It
also carries the two permits that keep the exhaustiveness honest: a chain of
read-only verbs (`stash list`/`show` — how a session inspects the shared stack)
and the single-worktree chained flow.

Red-proof observed 2026-09-27: swapping ONLY `shared_stash_guard.go` for
`4d8b7474^` (the first-clause predicate) and re-running this test file unchanged
→ **RED, 9 of 12 refusal subtests** fail (the three that still pass are the
mutation-first spellings, which the first-clause predicate happened to answer
correctly, and the epic's minimal `true ; git stash pop`, whose first clause
carries no stash word at all). Against master's exhaustive predicate: GREEN.
The red is therefore the CEILING's, not this branch's.

## Pitfalls found while building it (for the next reader)

- **`GIT_DIR=` is not "unset".** Setting the env var to the empty string makes
  git answer `fatal: The empty string is not a valid path`; the probe must
  REMOVE the entry (`withoutGitDirEnv`, name-matched on `GIT_DIR` /
  `GIT_WORK_TREE` / `GIT_INDEX_FILE`). A guard whose probe fails this way fails
  OPEN — it permits everything.
- **Signals an apply came from a stash** (not a merge): `UU`/`AA` entries plus
  no `MERGE_HEAD`, and `git stash list` still holding the entry.
- **A conflicted pop keeps the entry** — the retry loop that re-applies another
  session's tree is git's documented behaviour, not a user mistake.
- **Judge the stash word's OWN clause, never the whole command line.** The first
  version of the predicate searched the command for `stash` and let a "bare
  stash" fallback fire on a missing word — so a session that chained prose after
  the verb defeated it, MEASURED 2026-09-27 on the guard's first landing:

  ```
  git stash pop; echo "git stash pop is repo-wide"   → PERMITTED, git ran pop raw
  ```

  A clause-blind predicate is a false negative waiting for the next session, and
  the fix direction is the same one that removes the matching false positive
  (`git stash pop; grep stash file`): take the verb from the stash word's own
  simple command — `stashClausePattern` cuts at `;`, `&&`, `||`, `|`, newline —
  and treat "only options follow" (`git stash --quiet`, `git stash --keep-index`)
  as the implicit push it is. The regression test pins both the bypass spelling
  and the read-only shapes.

- **A substring is not a subcommand — and the wrong VERB is a diagnostic bug.**
  The clause-scoped predicate above still matched the token wherever it appeared,
  and Go's `\b` treats `-` as a boundary, so the word inside a PATH counted.
  Measured 2026-09-21 (the Rule-31 adoption session, which filed it as task
  `01a0c54a`): workdirs and report names here literally carry the word, so

  ```
  cd /home/user/workspace/gopi-stash-guard && git status --porcelain   → REFUSED as `git stash push`
  echo /home/user/workspace/stash-pop-verbatim-report.md; git log -1   → REFUSED as `git stash push`
  ```

  i.e. the guard blocked ordinary commands in exactly the repo it governs — and
  the epic's own `git stash pop` was announced with the WRONG verb (`push`),
  because a trailing-prose clause hits the implicit-push fallback. `\bstash\b`
  is therefore only a *candidate* occurrence; `stashWordIsInvocation` decides
  whether it is a git storage subcommand: the token must FOLLOW the `git`
  command word in its clause, with only git's options (and their operands,
  `-C <dir>`) between them, and only wrapper words (`sudo`, `time`, `xargs`, …)
  before it. `echo git stash`, `grep -rn stash file` and every path-shaped token
  are excluded by construction. The pinning cases are in the regression test:
  refusal for the mutation spellings, permission for the two measured false
  positives, and a check that the refusal text names the verb git was handed.

- **Don't re-inline the git argv — this package has a Rule 13 gate for it.**
  The probe spelled `"worktree"`, `"list"`, `"--porcelain"` and the
  `"worktree "` porcelain prefix inline. `TestFinishCleanupWorktreeVerbLiteralGate`
  (the anti-regression gate over `internal/tools`' production files) caught it:
  the worktree verb/porcelain values already live in constants
  (`finishCleanupWorktreeVerb` / `finishCleanupWorktreeListVerb` /
  `finishCleanupWorktreeListFlag` / `finishCleanupPorcelainWorktreePrefix`,
  `task_finish_cleanup.go`) and every other parser in the package references
  them. A new file that re-spells them is exactly the drift the gate exists to
  catch — reference the constants. (Red-then-green: the gate was red on the
  branch's first landed commit, green after the fix, and green on an unmodified
  master checkout, so the red was the branch's.)

- **Clause-scoped is not enough: the predicate must judge EVERY clause.** The
  clause-scoped rewrite above took the verb from the stash word's own simple
  command — but it then **returned on the first clause that carried a stash
  invocation at all**. So a leading READ-ONLY clause satisfied it: the guard
  answered "read-only, permitted" for a command that went on to mutate the
  shared stack. MEASURED 2026-09-27 (task `01a0c545`, reported as this guard's
  residual bypass):

  ```
  git rev-parse --show-toplevel; git stash list; echo "---run---"; git stash pop
  → PERMITTED; git ran `stash pop` raw ("No stash entries found." in the chain's output)
  ```

  This is the same false-negative family as the clause-blind version one round
  earlier, one level down: clause-BLIND → clause-scoped-but-FIRST-ONLY →
  clause-EXHAUSTIVE. The shape an agent naturally emits is the chain; the
  single-verb spelling (`git stash pop`) was always the easy case, and a guard
  that is right only on the easy case is not a guard. The fix is
  `stashInvokedVerb` → `stashMutationVerb`: **keep scanning every clause** and
  refuse on the first stash MUTATION, so a clause that mutates nothing can never
  carry the verdict. Two details that matter:

  - **Read-only verbs skip EXPLICITLY**, through the named set
    `sharedStashReadOnlyVerbs` (`list`, `show`) — not by "absent from the
    mutation set". A permissive branch inferred by omission would silently
    bless any future mutation verb that nobody remembered to add to the
    mutation set (Rule 1b: the value's meaning comes from a named source).
  - **The refusal names the MUTATION**, not the read-only clause that preceded
    it: `git stash list; git stash pop` is reported as `` `git stash pop` ``.
    The diagnostic is part of the fix — the old first-clause predicate
    announced the wrong verb (the `push` bug recorded above), and this is the
    same failure mode on the chained spelling.

- **Re-measured on master, through the real entry point (2026-09-27, task
  `01a0c54a`).** The exhaustive rewrite above landed on
  `fix/stash-guard-compound-chain` and its later delivery-point sync, never on
  master; master's tree carried only the token/clause-scoped rewrite that
  preceded it. So the three items above were re-verified by driving each
  acceptance case through **`BashInDir`** — the actual bash-tool entry point
  wired at `bash.go` step 2c, not the predicate beside it — on a fixture repo
  with a main checkout plus the `…-stash-…`-named linked worktree the false
  positive was measured on:

  | case (in the 2-worktree repo) | master | this branch |
  |---|---|---|
  | `cd <…-stash-…-wt> && git status --porcelain` | permitted | permitted |
  | `echo <…stash-pop-verbatim-report.md>; git log --oneline -1` | permitted | permitted |
  | `cat stash-guard-notes.md` | permitted | permitted |
  | `git stash list` / `git stash show -p` | permitted | permitted |
  | `git stash pop; echo "git stash pop is repo-wide"` | REFUSED, names `pop` | REFUSED, names `pop` |
  | `git -C <repo> stash drop` | REFUSED, names `drop` | REFUSED, names `drop` |
  | bare `git stash`, `stash --quiet pop`, `stash push -m wip` | REFUSED | REFUSED |
  | every mutation verb in a 1-worktree repo | permitted | permitted |
  | `git rev-parse --show-toplevel; git stash list; echo "---run---"; git stash pop` | **permitted — git ran `pop` raw** | REFUSED, names `pop` |
  | `git stash list` ⏎ `git stash clear` | **permitted — git ran `clear`** | REFUSED, names `clear` |
  | `echo x \| xargs -r git stash pop` | **permitted — git ran `pop`** | REFUSED, names `pop` |

  master's substring/clause-scoped predicate therefore passes the epic's two
  named false positives, but is a false NEGATIVE on any chain whose first stash
  clause is read-only — the exact shape a session emits while diagnosing the
  shared stack. `shared_stash_guard.go` in this branch is master's file with the
  exhaustive rewrite applied (`stashMutationVerb` + `sharedStashReadOnlyVerbs`),
  plus one extra guard the case above exposed: `gitWordPattern` now requires a
  real shell separator on BOTH sides of `git`, because `\bgit\b` also matches
  the `git` inside a path-shaped token (`my-git-repo`) and could let a line with
  no git command reach the probe.

  **Red-proof (observed 2026-09-27; captured under the session's
  `evid/`, quoted verbatim in the task report).** The acceptance test is
  `TestSharedStashGuard_AcceptanceSet`
  (`internal/tools/shared_stash_guard_test.go`) — 39 subtests (`go test -run
  TestSharedStashGuard_AcceptanceSet -v -count=1`), each driven through
  `BashInDir`, permitted when `verb == ""` and refused-by-name otherwise.
  Swapping ONLY `shared_stash_guard.go` for each pre-fix version and re-running
  that same test file (unchanged) gives:

  | predicate swapped in | result | failing subtests |
  |---|---|---|
  | `8610eab6` (substring `\bstash\s+…`) | RED (7) | `permit/echo-git-stash`, `permit/quoted-prose-is-not-an-invocation`, `permit/git-log-named-stash-ref` refused as `` `git stash push` ``; `refuse/read-only-clause-…`, `refuse/newline-separated`, `refuse/and-separator`, `refuse/pipe-separator` permitted |
  | `05aee6fe` (substring + repo-named refusal) | RED (7) | same seven |
  | `f77d5c6c` (clause-scoped `\bstash\b`) | RED (9) | the epic's two false positives (`permit/cd-into-a-stash-named-worktree`, `permit/echo-stash-path-then-git-log`) refused as `` `git stash push` ``, plus those same seven |
  | this branch | GREEN | 39/39 |

  The two red families are distinct and both are real: the pre-`f77d5c6c`
  versions answer the epic's false positives wrongly (a PATH read as a stash
  invocation, and the wrong verb `push`), and every pre-fix version is a false
  NEGATIVE on a chain whose first stash clause is read-only. Nothing else needed
  swapping — the same test file produced all four verdicts.

  **The residual closed by the rework (2026-09-27, task
  `01a0c676`-family).** The exhaustive predicate (`stashMutationVerb` +
  `sharedStashReadOnlyVerbs`) LANDED on master via the sibling
  (`4d8b7474`, merged as `6ac573c4`), so `shared_stash_guard.go` is byte-
  identical on master and on the archived branch
  `archive/fix/stash-guard-acceptance` — there is no guard hunk left to
  re-author. What master's acceptance set did NOT pin is the separator
  dimension: master's rows covered `;` (`refuse/read-only-clause-…`) and
  newline (`refuse/newline-separated`), but had no `&&`, `||`, `|`, no
  second-clause-only chain, no wrapper word before `git`, and no permit row for
  quoted prose / a bare path / `git log … -- stash` / a read-only pipeline.
  Those nine rows are re-authored above **from the archived branch's set, onto
  master's own fixture** (`stashGuardAcceptanceFixture` + `BashInDir`) rather
  than by re-applying the archived test file — which now conflicts with master
  and whose remaining rows are a strict subset. They are what turns the
  red-proof's `refuse/and-separator` / `refuse/pipe-separator` rows red on the
  pre-fix predicates above.

  **The guard's own spellings, gated (Rule 13).** The archived branch also
  carried `shared_stash_guard_literal_gate_test.go`, which never landed on
  master; it is re-authored here with two changes: the scan set is the two
  PRODUCTION roots (`internal/tools`, walked recursively, plus `cmd`) rather
  than one directory of `internal/tools`, because the defect it guards — a
  second spelling of the stash word in a sibling file matching text the
  predicate never judged — is not confined to the guard's own package; and the
  vacuity guard is a reachable-FILE FLOOR (≥50 production files scanned)
  instead of `scanned == 0`, so a broken walk cannot pass green on a tree it
  never read. `_test.go` files stay exempt (the acceptance test must spell the
  commands it drives — Rule 31) and `testdata/` subtrees are skipped as
  fixtures. Measured green: no file outside `shared_stash_guard.go` carries
  `"stash"`, and none anywhere carries `"git stash"` / `"stash pop"` /
  `"stash push"`.

  **Rule 31 evidence, on the REAL bash-tool entry point (this session).** The
  guard is wired at `bash.go` step 2c inside `bashInDir` — the function every
  bash-tool call goes through — so a `bash` invocation made while the current
  workdir resolves to a repo with 12 linked worktrees IS the production path:
  `git stash list` returned the shared entry (permitted, exit 0), while
  `git stash list && git stash pop; echo "NEVER-REACHED"` was refused —
  `` dangerous command rejected: `git stash pop` in … would write THIS
  checkout's index/worktree from a stash stack shared by 12 linked worktrees ``
  — and the trailing `echo` never ran. That is the separator case
  (`refuse/and-separator`) and the acceptance criterion observed end to end,
  not through the predicate.

