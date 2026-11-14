<!-- doc-class: historical -->
# Shared `refs/heads/*` guard — verb coverage

**Epic**: extend `internal/tools/shared_integration_ref_guard.go` so it refuses the
verbs that still silently rewrite a `refs/heads/<b>` held by ANOTHER worktree on
git 2.34.1. Prior state (task `01a12da8`) covered only `update-ref` /
`symbolic-ref`; the file head asserted that "git refuses the siblings" — a
premise that was **false**, and was measured false.

## The measured verb census

Generator: `~/.gopi/sessions/01a12db7-79cd-7a17-846d-9799c5a27abb/artifacts/verb-census.sh`
Raw output: `verb-census.txt` beside it (Rule 38: the figure names its generator),
with the two re-measured rows in `a6a7-live.{sh,txt}` and
`switch-long-form.{sh,txt}` in the same directory.
`git version 2.34.1`. Fixture: a MAIN checkout holding `refs/heads/main`, a
LINKED worktree of the same repo holding `refs/heads/other`; every case is issued
from the linked worktree, so the target `refs/heads/main` is held by ANOTHER
worktree.

### Holes the widening closes (exit 0, ref really moved)

| verb | exit | observed |
|---|---|---|
| `update-ref refs/heads/main <sha>` | 0 | ref MOVED (the incident's own verb) |
| `update-ref -d refs/heads/main` | 0 | ref DELETED |
| `branch -f main <sha>` | 0 | ref MOVED |
| `branch --force main <sha>` | 0 | ref MOVED (same flag, long form) |
| `branch -M main <new>` | 0 | ref RENAMED AWAY → absent |
| `checkout -B main <sha>` | 0 | ref MOVED, **caller switched onto it** |
| `switch -C main <sha>` | 0 | same (switch's force-create) |
| `fetch . +src:main` (and `+src:<short>`) | 0 | ref MOVED |
| `fetch <remote>` via `remote.<r>.fetch=+refs/heads/*:refs/heads/*`, no refspec, caller DETACHED | 0 | ref MOVED (6th hole) |
| `fetch --refmap=+refs/heads/*:refs/heads/* <remote> '<other-refspec>'` | 0 | ref MOVED |

The census also measured `fetch . src:main` **without** the `+` (plain
fast-forward move, exit 0, ref MOVED) and `branch -C <src> main` (force COPY onto
the held ref, exit 0, ref MOVED) — both now judged by the same predicate.

### What git itself refuses (so the guard need not)

| verb | exit | git's message |
|---|---|---|
| `branch -D main` | 1 | `Cannot delete branch 'main' checked out at '<other worktree>'` |
| `worktree add -B main` | 128 | `'main' is already checked out at '<other worktree>'` |
| `branch -c src main` | 128 | `A branch named 'main' already exists.` |
| `fetch . src:refs/heads/other` (the CALLER's own) | 128 | `Refusing to fetch into current branch … of non-bare repository` |
| `fetch . +refs/heads/*:refs/heads/*` (wildcard incl. caller) | 128 | same message |

Note git keys on **two different conditions**: `branch -D` / `worktree add -B`
refuse a branch held by another worktree; `branch -f <own>` and `fetch` into it
refuse the CALLER'S OWN current branch. Neither covers the verbs above.

### Deliberately PERMITTED (measured, not laziness)

| case | measurement | why permitted |
|---|---|---|
| `branch -M main main-renamed` (rename a foreign-held branch AWAY) | exit 0, ref `main` → absent, **trunk porcelain empty, staged=0** | nothing staged, no index desync; the holder merely has a branch no longer answering to its name. A refusal would false-positive an operator's ordinary rename. |
| `branch -M main` (in place) | exit 0 | moves no ref at all (measured) |
| `branch -m <held> <held2>` | 128 | git refuses |
| `branch -f <free> <held>` | exit 0 | the BASE is READ, never written |
| `branch -f refs/heads/main` (fully-qualified) | nests harmlessly | measured |
| `fetch` with git's **default** tracking mapping (`refs/remotes/<r>/*`) | exit 0, no local branch written | measured: an ordinary fetch must stay allowed |
| `branch --move --force main <new>` | 128 | git refuses (flag combination) |
| `-fmain` / `--force=x` glued spellings | 129 / refused | git's own parser rejects |

### Boundary measurements that shaped the parsing rule

- `fetch . src:feat/x` → exit 0, creates `refs/heads/feat/x`. A destination with a
  SLASH is a branch ref (not a path); the guard does not key on the slash.
- `fetch` dst `refs/tags/v1`, `refs/gopi/keep`, `refs/remotes/origin/main` → NOT
  branch refs; not judged.
- Caller DETACHED: `branch -f main <sha>` exit 0, `checkout -B main <sha>` exit 0,
  `fetch . +src:main` exit 0, `branch -M main <new>` exit 0 — all MOVED. The
  detached caller holds nothing of its own, so the holder is purely the other
  worktree; every one of these is refused.

### Two census rows that did NOT measure what they claimed — re-measured (honest)

Census rows **A6** (`checkout -B main <sha>`) and **A7**
(`checkout --force-create main <sha>`) recorded `exit=128` / `exit=129` with
`fatal: reference is not a tree: <sha>` and `unknown option 'force-create'`. That
is a **fixture artifact, not a verdict about the verb**. The generator's shape is
the cause: `mk` is called once **as a pre-seed** so that the `probe` call sites can
expand `$OLDER` / `$HEADSHA` into their argument lists, and `probe` then calls
`mk` **again** — so every sha a call site captured is only valid while both commit
sets happen to land in the same clock second. When they do not, the sha is dead
and **git's own argv validation fails before anything is written**. A7 additionally
exercised `git checkout --force-create`, which git 2.34.1's `checkout` genuinely
does not accept — `--force-create` is **`switch`'s** long form.

Re-measured with the sha resolved **inside** the fixture, once per form
(generators + raw output in the same artifacts directory as the census):

| re-measured row | exit | observed |
|---|---|---|
| `checkout -B main <live src sha>` | **0** | ref MOVED to `src` (`89e8…`→`a252…` on the switch run), **caller switched onto `main`**, trunk `M trunk.txt` staged=1 |
| `switch --force-create main <live src sha>` | **0** | same: ref MOVED, caller switched onto it, trunk staged=1 |
| `checkout --force-create main <live src sha>` | 129 | `error: unknown option 'force-create'` — **the flag is not checkout's**; nothing written |

So the hole is real for both spellings, and the census's `A6` label was right about
the verb while its measurement was wrong. The authoritative evidence is the suite's
RED half (`TestSharedIntegrationRefGuard_VerbFamilyRedThroughRawGit`), which builds
a fresh fixture per case and asserts through raw git that `checkout -B <held>` and
`switch -C <held>` exit 0 and really move the held ref — and the same dead-sha
mechanism is a candidate explanation for any other census row whose `exit` is a
git argv error rather than a refusal about a ref. **Rule 41: the artifact's rows are
quoted as measured, with the exceptions named rather than silently dropped.**

## The predicate is unchanged

The widening adds **verbs**, never a new rule. The judge is still the HOLDER,
derived from `git worktree list --porcelain` plus each worktree's branch:

1. is the caller in a linked worktree or the main checkout? (main checkout always
   holds its own current branch → never refused: the merger's own lane);
2. does the repo have more than one worktree? (a single checkout shares nothing);
3. which worktree holds the target branch? — the caller's own entry is excused,
   any OTHER entry is the refusal and its PATH is named.

Because the predicate is the holder and never the flag or the name, gopi's own
lanes keep working: `worktree add -B` (git refuses a held name anyway, and a bare
`-B` moves no ref), `checkout -B` / `branch -f` on one's OWN branch, and every one
of these verbs aimed at a branch NO worktree holds.

Failing CLOSED is preserved: a worktree whose checkouts cannot be read leaves the
target UNJUDGED, and an unjudged write to a shared namespace is refused. A
worktree directory that has already VANISHED is a different fact — provably
holding nothing — and is skipped.

## Wiring

`internal/tools/bash.go:435` — step 2d, beside the shared-stash guard
(`bash.go:419`). **The same escape hatch, no second chain**: both are gated by
`RejectDangerous && !escapeDangerous` (i.e. `GOPI_ALLOW_DANGEROUS`), so an
operator who has already opted out of the dangerous-command gate sees one
consistent behaviour.

## Files

- `internal/tools/shared_integration_ref_guard.go` — widened verb recognition
  (+773/−119 lines against the `01a12da8` baseline): `refGuardGovernedVerb`
  now admits `branch` / `checkout` / `switch` / `fetch`; per-verb target
  extraction (`refGuardBranchTargets`, `refGuardCheckoutTargets`,
  `refGuardFetchTargets`, `refGuardConfiguredFetchDests`) and the flag scanner
  `refGuardScanOptions`. The measured table above lives in the file head.
- `internal/tools/shared_integration_ref_guard_test.go` — fixture extended with a
  SOURCE branch, a renamed target, an `origin` bare remote and the named
  constants the new rows share.
- `internal/tools/shared_integration_ref_guard_verbs_test.go` — the new
  acceptance set (563 lines): RED through raw git, GREEN through `BashInDir`,
  post-refusal re-read, and the no-weakening set.
- `internal/tools/shared_stash_guard.go` — `gitDashCDirOption` extracted as a
  named constant (the `-C` operand is now read by two guards — Rule 1b/13) plus
  `gitProbeExitCode`, the verdict-shaped probe (`rev-parse --verify --quiet`).

## Why one fixture per case

A PERMITTED case legitimately moves the caller (`checkout -B <free>` switches
HEAD, `branch -M` renames under it), so a shared fixture lets one case decide
another's world. That cascade was measured, not theorised: the first version of
the acceptance set shared one fixture, and a single refused row that had moved the
caller turned seven later rows red.

## Rule 31: the guard was RUN, through the wiring it lives in

The bash tool has no CLI subverb, so its production entry point is the agent loop.
This branch's binary (`/home/user/workspace/_tmp/gopi-refguard-01a12db7`, built
10:29:41 from this worktree) was run against a disposable two-checkout fixture
(trunk holds `main`, the linked `wt` holds `other`), on a task whose only route is
the bash tool:

```
Ran:  A=$( ... ); echo "Use the bash tool to run EXACTLY this command and then report
      verbatim what the tool returned: git -C <wt> branch -f main <src>
      ..." | /home/user/workspace/_tmp/gopi-refguard-01a12db7
Result: tool_result (verbatim, from the sandbox's own session jsonl):
      Bash error: dangerous command rejected: `git git refs/heads/main` from
      /home/user/workspace/_tmp/refguard-r31/wt would rewrite the SHARED branch
      `main` of /home/user/workspace/_tmp/refguard-r31/wt, which is checked out by
      ANOTHER worktree (/home/user/workspace/_tmp/refguard-r31/trunk). …
      Nothing was run and no ref was touched. …
      and the ref afterwards: refs/heads/main = 2d3952dd… (unchanged), src = 6b0bca8f… (neq)
```

Generator + raw output in the artifacts directory (`r31-setup.sh`, `r31-run.sh`,
`r31-run.txt`, `r31-agent-out.txt`, `r31-refguard-raw-toolresult.txt`). The
post-hoc read is the second half of the proof: the refused verb left
`refs/heads/main` at `2d3952dd…` while the operand was `6b0bca8f…`.

**One defect this run surfaced, and it was fixed inside this branch** (Rule 41: the
sentence is evidence; a defect found while taking it is not dropped). The first
run rendered the command as `` `git git refs/heads/main` `` — the binary twice, the
subcommand and the whole argv gone — where the real invocation was
`git -C <dir> branch -f main <sha>`. Root cause, not a typo: `refGuardRefusal`'s
format string was a base-era `\`git %s %s\`` (verb, ref), while this branch moved
`commands` onto a different value (the per-clause command TEXT, taken as
`clause[0]` = the binary word) — the two spellings of the same sentence disagreed,
so the operand landed in the slot meant for the verb. Fixed at both ends, in one
place each: `refGuardRefusal` now renders `` `%s` `` (the invocation already
carries its own `git`) and `refGuardRecognisedTargets` records the clause
**joined**, so the refusal quotes the invocation AS SPELLED — `git -C <repo>
update-ref …` no longer reads as having acted on the session directory. Covered by
the acceptance set: every REFUSED row asserts the refusal contains its own
spelled command, and that it never renders `git git`. Re-measured through the same
entry point after the fix (`r31-run-fixed.txt`, `r31-fixed-raw-toolresult.txt`):

```
Result: Bash error: dangerous command rejected: `git -C /home/user/workspace/_tmp/refguard-r31/wt
        branch -f main 6f4489f70454a522bb904ddda489307322ae158a` from
        /home/user/workspace/_tmp/refguard-r31/wt would rewrite the SHARED branch `main` of
        /home/user/workspace/_tmp/refguard-r31/wt, which is checked out by ANOTHER worktree
        (/home/user/workspace/_tmp/refguard-r31/trunk). … Nothing was run and no ref was touched.
        and afterwards: refs/heads/main = 0bb9cc11… (unchanged), src = 6f4489f7… (≠)
```

The defect was live only in THIS branch's unpushed work (`git merge-base --is-ancestor
dc1ee9661 master` → not an ancestor), so master never carried the broken sentence.

## What this still does not cover (honest)

- `git push` writing a local branch through a refspec, and `git reset --hard` /
  `git checkout` moving a branch pointer indirectly, are outside this guard.
- A `-M` rename of a foreign-held branch AWAY stays permitted by measurement (0
  staged paths); if a future incident shows that desyncs a holder, that is a new
  row with its own evidence, not a silent widening.
- The escape hatch `GOPI_ALLOW_DANGEROUS` disables both guards; the guard's
  verdict cannot be observed in a process started with it set.
