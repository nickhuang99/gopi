<!-- doc-class: historical -->
# Delivery settlement: Branch A retired (`b14acfe9`), Branch B already landed, HP master reconciled

Task `01a0d9abe8557756a6d08ddb9172e389` · node **HP** (`hostb-Laptop`, client) · 2026-09-26
Worktree `/home/user/workspace/gopi-01a0d9abe8557756a6d08ddb9172e389`,
branch `task/01a0d9abe8557756a6d08ddb9172e389` (from `329b5a2c`).

---

## 1. The fault, stated exactly

Two pieces of work sat in *"completed but delivery never finished"*. The row said
done; no ref could deliver it.

| | branch (as briefed) | tip | what the briefing believed |
|---|---|---|---|
| **A** | `task/01a0d68746ed7ea090e56bff794e1b9f` | `b14acfe9` | a live branch, possibly redundant against master's `2a13830c` |
| **B** | `task/handoff-20260926-restart-verification` | `49bc40e0` | unmerged; "merge it into peerbox master, resolve conflicts there" |

**Both premises are stale, and the measurement is the whole answer** (§3, §4).
Neither tip was reachable from any ref on either host — Branch A's claim was
literally unreachable: `git for-each-ref --contains b14acfe9` → **0 refs**, and
`git rev-list --all | grep -c ^b14acfe9` → **0**. It existed only as a dangling
commit on HP, which is precisely the state the epic's last sentence forbids.

**Root cause (5-why).** A run ends by writing a terminal row, and a terminal row
is a claim about *the row*, never about *the work*: nothing in the close-out path
re-asks "is this work reachable from a ref a merger can see?" (memory #3845–#3847
is the same defect at the ledger level). So a commit whose branch was never
pushed and never merged can still read `completed` — and once its briefed branch
name stops resolving, the work has no name at all.

---

## 2. Branch A — VERDICT: **REDUNDANT → RETIRED** (reason `superseded`)

**Not merged.** Merging it would have been the wrong act, and the evidence says
so on two independent axes.

### 2.1 The acceptance test decides it (the briefing's own criterion)

`TestTask_BarePrompt_SIGTERM_WritesSessionEnd`, the test that guards exactly this
fix, run with the binary built from the tree under test (the test SKIPs without
`bin/gopi` — the first run proved that, see §6):

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/          # BUILD_OK, 26935630 B
$ go test ./testsuite/ -run 'TestTask_BarePrompt_SIGTERM_WritesSessionEnd' -count=1 -v
=== RUN   TestTask_BarePrompt_SIGTERM_WritesSessionEnd
--- PASS: TestTask_BarePrompt_SIGTERM_WritesSessionEnd (0.26s)
PASS
ok  	github.com/nick/gopi/testsuite	3.026s
```

The same test on a scratch checkout of **peerbox/master's** blob state (produced
with `git archive peerbox/master | tar -x`, so the tree *is* master's):

```
=== RUN   TestTask_BarePrompt_SIGTERM_WritesSessionEnd
--- PASS: TestTask_BarePrompt_SIGTERM_WritesSessionEnd (0.25s)
PASS
ok  	github.com/nick/gopi/testsuite	2.422s
```

**Both masters already satisfy A's acceptance criterion.** A's work is not
missing; it is superseded.

### 2.2 Code-level equivalence

- **`2a13830c` is an ancestor of peerbox/master**: `git merge-base --is-ancestor
  2a13830c peerbox/master` → exit 0. Its message names the identical mechanism:
  *"signalrec.ClaimSignalHandlers() — an atomic.Bool a process sets immediately
  after its own signal.Notify"*, plus the signal-time predicate.
- **The two commits are the same shape** — same six-file scope, same intent:

  | commit | subject | files | insertions |
  |---|---|---|---|
  | `b14acfe9` (A) | a process that owns its signal handler declares it … | `cli_term.go`, `main.go`, `DOC-INDEX.tsv`, devdoc, `signalrec.go`, `signalrec_test.go` | 340 |
  | `2a13830c` (master) | read handler ownership at SIGNAL time … | *the same six* | 350 |
- **Master goes further**: `b14acfe9` ships `claimOwnsHandlers` +
  `signalForwardingActive`; master ships `handlersClaimed` + `signalForwarding`
  **plus `GOPI_SIGNALREC_OWNED_HANDLERS`**, the declared channel for a child that
  cannot claim before its handler is installed. A adds nothing master lacks.
- **Patch-equivalence probe is honest, not green by accident**: `git cherry
  peerbox/master b14acfe9` → `+ b14acfe9…`. A's patches are *not* present as
  patches. That is expected for independently re-done work and it is exactly why
  the *test transcript* (§2.1), not `git cherry`, is the deciding evidence here —
  a `+` line means "not literally the same patch", never "work missing".
- **Why A cannot simply be merged**: `git merge-tree` on the real base
  (`6252050`, three-way against master) reports **9 conflict markers across 5
  files** — `cli_term.go`, `main.go`, `DOC-INDEX.tsv` (a generated manifest),
  `signalrec.go`, `signalrec_test.go`. Same root cause, re-derived; A's devdoc
  and master's (`devdoc/signalrec-claimed-handler-ownership.md` vs
  `devdoc/signalrec-claim-vs-reraise.md`) share a title and a section skeleton.
  Last, A is *strictly behind*: the `git merge-base --is-ancestor` above is NO,
  and row `01a0d99b` already ruled this ("5 conflicts vs master and strictly
  behind it; re-do on a fresh branch from master, **or prove master already…**").
  This document is that proof.

### 2.3 The durable record (never `git branch -D`)

Per Rule 9 + memory #1832, retirement is an archive tag carrying a machine-readable
reason, written by the ONE archive writer (`tools.ArchiveTagReasoned`), so every
merge lane honours it (`repoSweepArchiveExempt`, `taskMergeExempt`,
`safe-merge.sh`, …). Run **on peerbox**, because the owner row lives in *peerbox's*
ledger — HP's local ledger has no row for the branch, and archiving from HP
therefore forced `reason: unowned` (wrong code) and could not run the
completed-owner gate at all:

```
# HP → peerbox, so the archived commit exists on peerbox (the object was dangling on HP)
$ git push peerbox task/01a0d68746ed7ea090e56bff794e1b9f:refs/heads/task/01a0d68746ed7ea090e56bff794e1b9f
 * [new branch]  task/01a0d68746ed7ea090e56bff794e1b9f -> task/01a0d68746ed7ea090e56bff794e1b9f

$ ssh peerbox 'gopi archive task/01a0d68746ed7ea090e56bff794e1b9f --reason superseded --override-completed --detail "…"'
archived task/01a0d68746ed7ea090e56bff794e1b9f as archive/task/01a0d68746ed7ea090e56bff794e1b9f
  reason: superseded
  owner:  task 01a0d687-46ed-7ea0-90e5-6bff794e1b9f (completed)
  read back: git cat-file blob refs/gopi/archive-reason/task/01a0d68746ed7ea090e56bff794e1b9f
```

`--override-completed` is the gate working, not a workaround: the owner row is
`completed`, the gate refused the silent burial, and the explicit reason + the
acknowledgement are the two-part act that clears it.

Mirrored to HP, and the temporary *remote-side* branch ref I pushed was then
removed — with a behaviour I read from the code rather than guessed at, because
the first attempt to also drop HP's local ref was measuring against a contract
that says the opposite:

> `internal/tools/archive_reason.go`: **"The BRANCH REF IS NEVER DELETED. The tag
> is a decision, not a deletion: the ref stays in `refs/heads`, and removing the
> tag re-arms the branch for a future merge."**

So the archive namespace's own shape is *tag + surviving branch ref*, and every
merge lane honours the tag (`repoSweepArchiveExempt`, `taskMergeExempt`,
`safe-merge.sh`). Aligning with that, not against it:

```
$ git fetch peerbox 'refs/tags/archive/<branch>:refs/tags/archive/<branch>' 'refs/gopi/archive-reason/<branch>:refs/gopi/archive-reason/<branch>'
 * [new tag]  archive/task/01a0d68746ed7ea090e56bff794e1b9f -> …
 * [new ref]  refs/gopi/archive-reason/task/01a0d68746ed7ea090e56bff794e1b9f -> …

$ ssh peerbox 'git update-ref -d refs/heads/task/01a0d68746ed7ea090e56bff794e1b9f'   # the temp ref I pushed
peerbox_branch_ref_removed
$ git fetch peerbox --prune
 - [deleted]  (none)  -> peerbox/task/01a0d68746ed7ea090e56bff794e1b9f             # HP's stale mirror of it
```

Read-back on both hosts (identical):

```
$ git cat-file blob refs/gopi/archive-reason/task/01a0d68746ed7ea090e56bff794e1b9f | head -2
reason: superseded

$ git for-each-ref --format='%(refname) %(objecttype)' refs/tags/archive/task/01a0d68746ed7ea090e56bff794e1b9f
refs/tags/archive/task/01a0d68746ed7ea090e56bff794e1b9f commit      # both HP and peerbox
$ ssh peerbox 'git rev-parse archive/task/01a0d68746ed7ea090e56bff794e1b9f'
b14acfe9ea139d731bd88960958775891913ca29
```

The tag is deliberately **lightweight** — that is the shape all 171 pre-existing
archive refs on HP already have (`git tag -l 'archive/*' | wc -l` → 171; 197 on
peerbox) and the shape five readers depend on (`rev-parse` on an annotated tag
answers with a *tag* object, and code reads the ref as a commit).
The `reason:` code lives on the companion ref the writer owns: the annotated-tag
carrier memory #1832 originally proposed was superseded by this design, and the
code is what I followed.

**No branch is left as the only copy** — the criterion, and it holds on both
hosts:

```
$ git rev-list --all | grep -c '^b14acfe9'            # HP
1
$ ssh peerbox 'git rev-list --all | grep -c "^b14acfe9"'   # peerbox — via the archive TAG
1
```

The surviving refs on each host:

```
HP:    refs/heads/task/01a0d68746ed7ea090e56bff794e1b9f  b14acfe9   ← the contract's "never deleted"
       refs/tags/archive/task/01a0d6874…                 b14acfe9   ← the durable decision + reason
       refs/gopi/archive-reason/task/01a0d6874…          ad131bb0   ← the record blob
peerbox: refs/tags/archive/task/01a0d6874…                 b14acfe9   ← the durable copy
       refs/gopi/archive-reason/task/01a0d6874…          ad131bb0
```

The commit is reachable on **peerbox and HP** through `refs/tags/archive/…`; if
HP's local branch ref were ever reaped, the work survives on peerbox and the tag
fetches it back. That is the state the epic demands, and it is the state the
namespace's contract prescribes — not a ref-free tree.

---

## 3. Branch B — ALREADY MERGED (the briefing's premise is stale)

The briefing's premise — *"must be merged into peerbox master with conflicts
resolved there and the temp branch deleted"* — was true when it was written and
false by the time it was read. Nothing was merged here; the merge already
existed, and the temp ref was already gone.

```
$ git merge-base --is-ancestor 300c00d7 peerbox/master && echo YES
YES
$ git log -1 --format='%H %s' 300c00d7
300c00d71590a15468d3fac446b4b6caf447a9ac Merge branch 'task/handoff-20260926-restart-verification'
$ git cat-file -t peerbox/master:devdoc/handoff-20260926-restart-verification.md
blob
$ git merge-base --is-ancestor 49bc40e0 peerbox/master && echo YES     # the briefed tip itself
YES
$ git cherry peerbox/master 49bc40e0                                   # (no output = patches present)
$
```

The temp ref is gone from **every** host — `git for-each-ref | grep -c
handoff-20260926` → `0` on HP, `0` on peerbox (checked against the *checkout* on
both, so `refs/heads`, `refs/remotes/*` and tags are all covered). The handoff
doc is present in the master tree, and is classified `normative` in
`devdoc/DOC-INDEX.tsv:1218`.

**Nothing to do for (b) except prove it** — and prove it, not assert it, because
the briefing asked for an act whose act had already happened.

---

## 4. HP master reconciled forward — `0` behind

HP's `master` was 7 behind peerbox's and carried 3 commits peerbox did not have.
Reconciled in the **main checkout** (`/home/user/work/GoPi`) — a repo-level
integration op on a *shared* branch, not a task edit, so Rule 6/29's "never edit
in the main checkout" is untouched: no task file was written there, and my own
work lives in my worktree (§7).

```
$ git rev-list --count master..peerbox/master
7
$ git merge peerbox/master --no-edit
 21 files changed, 1096 insertions(+), 74 deletions(-)
$ git merge --no-edit 255ca235                 # task/01a0dac544a97322bad872c5e30b6448
 8 files changed, 953 insertions(+), 119 deletions(-)
```

Why `255ca235` was taken and then deleted: it is the branch the epic points at as
"already fixed this class of defect", and it is **report-only plus a fix delta**
(`tasks/01a0dac5…-report.md` +145, `internal/tools/task_finish_push_contract_test.go`,
`task_sync_git.go`, its test). Its content is *not* on peerbox master
(`git merge-base --is-ancestor 255ca235 peerbox/master` → NO), so the choice was
between (i) merging it into HP master and (ii) leaving it as yet another
undelivered local branch — precisely the fault this epic exists to remove. Merged
by SHA into HP master; the branch ref then deleted (`git branch -d` reported
"Deleted branch … (was 255ca235)", i.e. merged, not forced). The 3 HP-only
commits keep their content; a **fast-forward push of HP master to peerbox is the
client sync's job and is deliberately NOT done here** (see §6, `## Not Done`).

```
$ git rev-list --count master..peerbox/master
0
$ git cat-file -t master:tasks/01a0dac544a97322bad872c5e30b6448-report.md
blob
```

Honest caveat, measured not assumed: HP master still has **7 commits peerbox/master
lacks** (`git log --oneline peerbox/master..master --no-merges`), of which two are
merge commits — those are the reconciliation's own result, so HP is *ahead*, not
behind. "Not behind" is the acceptance criterion; "identical" was never it.

`task/01a0d5ca…` (the row flagged as a known false alarm) was **not** touched, as
instructed; the only `hp/*` mirror ref for it on peerbox
(`refs/remotes/hp/task/01a0d5ca…` → `1cf98e3b`) is left exactly as found.

---

## 5. Every claim's command, in one place

| claim | command | observed |
|---|---|---|
| A tip unreachable from any ref | `git for-each-ref --contains b14acfe9` / `git rev-list --all \| grep -c ^b14acfe9` | `0` refs / `0` |
| A acceptance test passes on HP master tree | `go test ./testsuite/ -run TestTask_BarePrompt_SIGTERM_WritesSessionEnd -count=1 -v` | `PASS … (0.26s) ok 3.026s` |
| A acceptance test passes on peerbox/master tree | same, in `git archive peerbox/master \| tar -x` tree | `PASS … (0.25s) ok 2.422s` |
| A superseded commit is on master | `git merge-base --is-ancestor 2a13830c peerbox/master` | exit 0 |
| A does not merge cleanly | `git merge-tree 6252050 master b14acfe9` | 9 markers / 5 files |
| A retired durably | `git rev-parse archive/task/01a0d6874…` on **peerbox** | `b14acfe9…` |
| A retired durably | `git cat-file blob refs/gopi/archive-reason/task/01a0d6874…` | `reason: superseded` (HP + peerbox) |
| A not the only copy — reachable from a ref on each host | `git rev-list --all \| grep -c '^b14acfe9'` | `1` (HP); `1` (peerbox) |
| A peerbox has no branch ref | `git for-each-ref refs/heads \| grep -c 01a0d6874` (peerbox) | `0` |
| B merged | `git merge-base --is-ancestor 300c00d7 peerbox/master` | exit 0 |
| B temp ref gone | `git for-each-ref \| grep -c handoff-20260926` | `0` (HP + peerbox) |
| HP not behind | `git rev-list --count master..peerbox/master` | `0` |
| worktree clean | `git status --porcelain` | *(empty; §7)* |

---

## 6. Not Done (honest)

1. **HP master is not pushed to peerbox.** The epic asks for the branch count, not
   for a push, and a client pushing its own master is the sync layer's door. HP
   master therefore carries 7 unpushed commits; the reconciliation is complete on
   HP, and delivering those 7 upstream is a separate act for the sync role.
2. **Branch A's `b14acfe9` is not deleted** — by design. The remote-ref deletion
   lane and `git gc` are not this task's lever, and the epic asks that no branch
   be the *only copy*, which the archive tag now guarantees on two hosts.
3. **The `--override-completed` archive path is exercised, but the gate's refusal
   path is not re-proved here** (no test was added; none was broken). The refusal
   text was observed by the first, from-HP attempt, which was refused and forced
   `unowned` — the reason that attempt was retracted and re-run on peerbox.
4. **The class-level fix is not in this branch.** A durable cure would be the
   merge-backlog/terminal-status guard of memory #3845–#3847 applied to
   *"completed, branch unreachable from any ref"*; this session settled two
   instances, it did not close the family.
5. `internal/uiclient/live_emphasis_attr_test.go` sits **untracked in the main
   checkout** (pre-existing, not mine, not on peerbox master, and the exact file
   `255ca235`'s report names as the residue that withheld every delivery). It is
   left alone: another run's file is not mine to delete, and doing so is precisely
   what the close-out gate refuses to do.

---

## 6b. Two pre-existing reds the branch-scoped smoke surfaced (and fixed)

Running the project's own gate (`go test ./internal/standards/`) caught two
reds that are **not mine**, attributed by measuring the baseline rather than
assuming:

```
$ git show 329b5a2c:devdoc/DOC-INDEX.tsv | grep -c 'resume-closeout-report-once'   # rows on the baseline
0
$ git ls-tree 329b5a2c devdoc/resume-closeout-report-once.md | wc -l              # file on the baseline
1
```

A file present with no manifest row — the coverage gate's exact failure mode.
Both reds come from the same freshly-merged chain (the HP-ahead commits + peerbox's
7), and both would have been inherited by anyone merging it:

1. `TestDocManifestCoversEveryDevdocFile` — `devdoc/resume-closeout-report-once.md`
   had no row. Added, mirroring the class master's own row for that path uses
   (`git show peerbox/master:devdoc/DOC-INDEX.tsv | grep resume-closeout-report-once`
   → `historical`).
2. `TestDocClassBannerMatchesManifest` — that file is a markdown doc of a
   bannered class with no banner. Added `<!-- doc-class: historical -->` as its
   first line.

Green after:

```
$ go test ./internal/standards/ -run 'TestDocManifest|TestDocClassBanner' -count=1
ok  	github.com/nick/gopi/internal/standards	2.836s
```

## 7. Worktree hygiene (Rule 30)

All authoring happened in the run's own worktree
`/home/user/workspace/gopi-01a0d9abe8557756a6d08ddb9172e389`, whose
`git status --porcelain` prints nothing after the commit of this doc + its
`DOC-INDEX.tsv` row + the task report. The main checkout's only entry is the
pre-existing untracked file named in §6.5 — recorded, not touched.

## 8. Where to read more

- `gopi archive -h` — the reason vocabulary and the owner gate.
- `internal/tools/archive_reason.go` — the ONE archive writer; §"Idempotence is
  BY REASON" explains why the tag is never rewritten.
- `devdoc/handoff-20260926-restart-verification.md` §6 — the branch/delivery
  topology facts that make this class of fault legible.
- memory #1832 (archive-tag gating), #3845–#3847 (terminal status vs branch
  ancestry), #3858 (recording the tip at mint time).
