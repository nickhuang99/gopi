<!-- doc-class: historical -->
# A premise that expires between CLAIM and GROUND: decision (no mechanism), and the `c37219666` drift class (accepted churn)

Row `01a13534-13bf-794d-8484-b51a7c15b137` · branch `task/01a1353413bf794d8484b51a7c15b137`
· base `0695ee38d` · host **peerbox** · worktree
`/home/user/workspace/gopi-01a1353413bf794d8484b51a7c15b137` · evidence
`~/.gopi/sessions/01a13534-13bf-794d-8484-b51a7c15b137/artifacts/`

This is a **decision document**, not a fix. Two questions were put to it, and both are
answered below with their reasons. Every figure was re-measured by THIS run; each is
quoted with the command that produced it (Rule 31). Measurements and inferences are
kept apart (Rule 41 ③) — the inference lines are marked `⇒ INFERENCE`.

## 0. Provenance of the two questions

The row was dispatched with a causal claim that its own filer then refuted
(`01a1350f`'s session, `artifacts/brief-premise-expired-at-ground-01a1350f.md`). The
refutation is itself re-measured in §1, because a decision built on an unverified
timeline is the very failure this document is about.

## 1. The timeline, re-measured

```
$ TIP=$(git rev-list --first-parent -1 --before='2026-10-13 19:40:00' master); echo $TIP
6ef428c1624e4d7490e459b0022490dc1195ab1d

$ git log -1 --format='tip: %h committer=%cd author=%ad  %s' --date=iso $TIP
tip: 6ef428c16 committer=2026-10-13 18:59:09 +0800 author=2026-10-13 18:59:09 +0800  delivery-point sync: merge master into the delivered branch before close-out

$ git merge-base --is-ancestor 38577cc3e $TIP; echo "rc=$?"
rc=1                                    # the repair was NOT in master at the claim instant

$ git show $TIP:internal/docclass/testdata/anchor-number-baseline.tsv | grep -c hp-push-topology
0                                       # no permit: the drift was live
$ git show $TIP:internal/docclass/testdata/anchor-number-baseline.tsv | grep -c '^[^#]'
163
$ git show $TIP:cmd/agent/cli_tasks_worker.go | grep -n 'serveGitSyncSweep(workerRoleLabel)'
650:			serveGitSyncSweep(workerRoleLabel)
$ git show $TIP:devdoc/DOC-INDEX.tsv | grep -n 'hp-push-topology-audit.md'
996:devdoc/hp-push-topology-audit.md	…	anchor cmd/agent/cli_tasks_worker.go:638 at serveGitSyncSweep(workerRoleLabel)
```

The row's own ledger line fixes the two instants:

```
$ gopi task show 01a1350f-6fe6-72a9-9133-5cc90c5b1012 | grep -E 'created_at|claimed_by'
created_at:     2026-10-13 19:39:26
claimed_by:     peerbox:2441159 (at 2026-10-13 20:03:01)
```

```
claim      19:39:26   TIP 6ef428c1   permit grep 0, code at :650 vs cell :638  ⇒ RED, brief CORRECT
ground     20:03:01   base fcc335e   permit present, cell re-pointed to that anchor
```

The repair's arrival, and the base the ground inherited:

```
$ git merge-base --is-ancestor 38577cc3e fcc335e; echo rc=$?      # rc=0  (the repair IS in the base)
$ git merge-base --is-ancestor fcc335e 5604465ef; echo rc=$?      # rc=0
$ git log -1 --format='%h author=%ad committer=%cd %s' --date=iso fcc335e
fcc335ebb author=2026-10-13 19:02:01 +0800 committer=2026-10-13 20:00:18 +0800 fix(docclass): the anchor-number header declares only re-derivable figures
$ git log --first-parent --format='%h %cd %s' master | grep -n 5604465ef
10:5604465ef 2026-10-13 20:11:12 +0800 pre-merge: verify branch against peer master before pushing
```

**Measured:** at the claim instant the brief was accurate against the exact revision it
named (`6ef428c1`, no permit, code `:650` vs cell `:638`); the base the ground
materialised (`fcc335e`, committer `20:00:18`) already carried the repair; the
first-parent arrival of the repair is `5604465ef` at `20:11:12`.
⇒ INFERENCE: the premise expired **between** the two, and the filer's `%ad`-vs-`%cd`
method error is confirmed — `38577cc3e`'s author date is `19:17:31` but its committer
date is `19:48:14`, so an author-date reading places the repair before a base it is not
an ancestor of.

## 2. QUESTION 1 — is a claim→ground premise expiry actionable?

### DECISION

**No new seam, and no warn. The preflight belongs on the EXECUTOR**, as a briefing
rule: *any row whose acceptance is a single command carries that command as its FIRST
step, with the conclusion pre-declared — exit 0 ⇒ the ask is already delivered ⇒ report
that, never manufacture a diff.*

### Why the seam cannot carry it cheaply (measured, not assumed)

**(a) The premise is not a field, and the surface that could read it cannot reach the
string.** A row carries a `deliverable` blob and briefing **text**; the accepted
revision exists inside prose. The named tip is not stored:

```
$ grep -rn 'base_rev\|baseRev\|BaseCommit\|start_revision' internal/tools/ internal/agent/ cmd/agent/ | wc -l
0
$ grep -n 'DeliverableWorktreeKey\s*=\|DeliverableBranchKey\s*=\|DeliverableRepoKey\s*=' internal/tools/task_finish_cleanup.go
307:	DeliverableWorktreeKey = "worktree"
315:	DeliverableBranchKey = "branch"
316:	DeliverableRepoKey   = "repo"
```
⇒ INFERENCE: a `premise_commit` would be a **new key on the shared deliverable blob**,
i.e. a new writer + reader in the protocol every task path shares (Rules 13/14), to
compare against a revision that the ground is, by construction, about to replace.

**(b) The claim seam already resolves the ground — and therefore already destroys the
comparison it would make.** `cmd/agent/cli_tasks.go:2856` calls
`taskClaimGroundStamp(target, childDir)` inside `runTaskRunOnce`, immediately after
`taskWorkdirEnter`. Its own header states the contract: the ground is stamped
**additive by contract**, and the seam is "best-effort, like every other side-channel
stamp: the caller ignores the result". A check on the same line would be a **verdict
from a function whose contract is "never blocks the run"**; the only way to give it
teeth is to give it a different contract, which is the seam's whole design inverted.
⇒ INFERENCE: the cheap form (warn) is contract-compatible; the expensive form (refuse)
is not, and the cheap form is what (d) rejects.

**(c) The window is a structural property of this pipeline, not a rare race.** The
ground-cut instant is precisely when master is moving, because merges are most of the
motion:

```
# generator: awk over `git log --first-parent --since='2026-10-12 22:00' --name-only master`;
# 81 distinct anchored paths read from master:internal/docclass/testdata/anchor-number-baseline.tsv
commits touching >=1 anchored file: total=43 merge=39 non-merge=4
all first-parent commits in window: 143 (of which merges=102, 71%)
$ git log --all --format=%s | grep -c '^delivery-point sync: merge master into the delivered branch'
492
```
(master tip `0695ee38d`; evidence `artifacts/m10-q2-census.out`)

⇒ INFERENCE: a per-row comparison at claim time fires constantly and warns about a
condition the executor already detects in one seconds-long command.

**(d) The cost asymmetry is decisive.** Refusing is the ONLY form with teeth, and in
this scenario refusing is **wrong**: the row was filed *because* the acceptance command
was red, so a green acceptance is exactly what success looks like — refusing on "the
premise no longer holds" would refuse the honest outcome. Warning has no teeth and
duplicates (e)'s one command.

**(e) Where this class has actually been caught, twice, by no machinery.** The family
has two producers and both were closed by the executor running the acceptance command:
`01a0a028` (repair landed **before** the claim — the row sat unclaimed) and `01a1350f`
(repair landed **between** claim and ground). No seam saw either. A third member of the
family is open in the ledger right now (`01a13534-c0e1`, "Unowned carrier: a devdoc
BODY's inline `path.go:NNN` … validated by no mechanism"), filed from the same session.

### What this decision obliges, and its honest limit

- **On the briefing writer** (both human and agent): a row whose acceptance is a command
  SHOULD carry that command as step 1, with `exit 0 ⇒ nothing to repair; report it`.
  `01a1350f`'s own briefing did contain the command and the executor ran it first — which
  is why the row cost one no-op report instead of a fabricated diff.
- **Limit, stated plainly:** this is a writer discipline with **no gate**. No test can
  observe "the executor checked first" in the general case; what the ledger CAN show is
  the outcome (`01a1350f` reported a fixpoint instead of a diff). Do not mistake this
  decision for a guarantee.
- **A negative result is a result:** the row's predecessor claim ("the brief was never
  re-run at submit time") is refuted above; nothing at the submit seam *could* have
  caught this, because at submit-and-claim time there was nothing to catch.

## 3. QUESTION 2 — does the `c37219666`-class drift justify a mechanism?

### DECISION

**No mechanism. Accepted churn — the existing validator (the freeze table) is the right
instrument, and it is already load-bearing.** The secondary attribution in the round's
briefing is confirmed with one refinement (§3.1).

### 3.1 The attribution, re-measured

```
$ git log -1 --format='%h committer=%cd author=%ad %s' --date=iso 30350b652
30350b652 committer=2026-10-13 18:24:47 +0800 author=2026-10-13 18:24:47 +0800 feat(sync): advance the integration ref from the worker tick, and say when it is stale
$ git show 30350b652 -- devdoc/DOC-INDEX.tsv | grep -E '^[+-].*hp-push-topology-audit'
-devdoc/hp-push-topology-audit.md	…	anchor cmd/agent/cli_tasks_sync_git.go:58 at // node that produced it: …
+devdoc/hp-push-topology-audit.md	…	anchor cmd/agent/cli_tasks_worker.go:638 at serveGitSyncSweep(workerRoleLabel)

$ git log --oneline -S'cli_tasks_worker.go:638' -- devdoc/DOC-INDEX.tsv        # → 30350b652
$ git log --oneline -S'serveGitSyncSweep(workerRoleLabel)' -- cmd/agent/cli_tasks_worker.go   # → 30350b652

$ for c in 30350b652 c37219666 6ef428c1 master; do printf '%-12s ' $c; \
    git show $c:cmd/agent/cli_tasks_worker.go | grep -n 'serveGitSyncSweep(workerRoleLabel)' | head -1; done
30350b652    638:			serveGitSyncSweep(workerRoleLabel)
c37219666    650:			serveGitSyncSweep(workerRoleLabel)
6ef428c1     650:			serveGitSyncSweep(workerRoleLabel)
master       650:			serveGitSyncSweep(workerRoleLabel)
$ git log -1 --format='%h parents=%p committer=%cd %s' --date=iso c37219666
c37219666 parents=c707e3cd1 82f8b9529 committer=2026-10-13 18:32:27 +0800 delivery-point sync: merge master into the delivered branch before close-out
```

⇒ CONFIRMED: doc, manifest cell and call site landed **coherent in one commit**
(`30350b652`, `18:24:47`, cell re-pointed to `:638` in that same commit); the file then
moved `638→650` in `c37219666`, a **delivery-point close-out merge** 7m40s later. The
drifted anchor therefore lived **34 minutes** before the first legal repair (`38577cc3e`,
committer `19:48:14`) and was correctly RED at the brief's tip. The refinement to the
round's briefing: the cell re-point itself happened in `30350b652`, so the sequence is
re-point (coherent) → move (drift created) → permit.

### 3.2 Why the class has no discriminating predicate

The natural mechanism ("notice when a close-out merge moves a line an anchor points
at") would have to act on a class whose members have opposite sign — measured in one
window:

```
# generator: per merge, `git diff --name-only <merge>^1 <merge>` ∩ the 81 anchored paths
merges touching anchored files: mover-of-bytes=39  carrier-only=63  (checked 102 merges)
```
(master tip `0695ee38d`; evidence `artifacts/m11-q2-census-d.out`)

`c37219666` (`delivery-point sync`, `18:32:27`) **is** a mover of an anchored file;
`5604465ef` (`pre-merge: verify branch against peer master before pushing`, `20:11:12`)
**is also** a sync-shaped merge and also a mover (`cmd/agent/chat_dump.go`) — and it is
the very commit that **carried the repair** that closed this drift. Two commits of the
same class, opposite causal sign; 39 movers vs 63 carrier-only in 24h.
⇒ INFERENCE: any predicate strict enough to flag `c37219666` flags `5604465ef` too, and
one that flags `5604465ef` is flagging the merge machinery itself.

### 3.3 Why the freeze table is the right instrument, and what "accepted churn" commits us to

Measured, the existing validator already does the job at negligible cost — and the
table's own header states the property that makes it survivable:

```
$ grep -c '^[^#]' internal/docclass/testdata/anchor-number-baseline.tsv          # 166 now
$ git show 6ef428c1:…/anchor-number-baseline.tsv | grep -c '^[^#]'               # 163 at the claim tip
$ grep -n 'hp-push-topology' internal/docclass/testdata/anchor-number-baseline.tsv
97:devdoc/hp-push-topology-audit.md	number-drifted	cmd/agent/cli_tasks_worker.go:638	serveGitSyncSweep(workerRoleLabel)	first-frozen at 0b3c1de29; the recorded text is present in its file, but not on the recorded line
# header of the same file:
# A permit's IDENTITY is its key — every column but the last (the detector's
# AnchorFinding.Key). The reason is a reviewable EXPLANATION and carries NO live
# line figure, so an edit that moves lines above a frozen site cannot rewrite a
# permit
```

The **identity is the key** and the reason carries no live line figure — which is why
the frozen row survives the next move of its file. The ratchet moved `163 → 166` across
the same 24h window in which 39 merges rewrote an anchored file. Cost of the instrument:
one `go run internal/docclass/testdata/anchor-number-census.go … --check` (seconds,
exit 0/1) — quoted with its generator and the tip it was measured at, per Rule 38.

⇒ INFERENCE: "accepted churn" is a decision with an obligation, not a shrug: the
recurrence is **expected**, the freeze table is the named owner of the noise, and the
only thing that must not happen is a mechanism that duplicates it.

### 3.4 A limit on the evidence above (stated, not papered over)

`first-frozen at 0b3c1de29` is a **stored string**, not an ancestry proof: `0b3c1de29`
is dated 2026-10-12 06:16:34, the day before the cell was written by `30350b652`, and
the tree already carries `devdoc/anchor-number-false-first-frozen-verdict.md` for
exactly this reason. Nothing in §3 depends on that field — the mover chain is proven by
content (`git show <rev>:<path> | grep -n`), not by it.

## 3.5 A THIRD instance, measured live: this run's own base (added after §1–§3 were written)

§2 and §3 decided on two instances (`01a0a028`, `01a1350f`). While verifying this
branch, this run found a **third, live one — its own ground** — so the decision is
recorded against a measured population of three, not two.

```
$ git log -1 --format='base   0695ee38d committer=%cd' --date=iso 0695ee38d
base   0695ee38d committer=2026-10-13 21:29:53 +0800
$ git log -1 --format='master 52d63402c committer=%cd' --date=iso master
master 52d63402c committer=2026-10-13 21:49:07 +0800

$ for r in 0695ee38d master; do printf '%-12s COMMANDS.md cell=' $r; \
    git show $r:COMMANDS.md | grep -o 'cli_doccheck.go:[0-9]*' | head -1; \
    printf '%-12s source func line=' $r; git show $r:cmd/agent/cli_doccheck.go | grep -n '^func isDocCheckSubcommand' | cut -d: -f1; done
0695ee38d    COMMANDS.md cell=cli_doccheck.go:234
0695ee38d    source func line=246          ← STALE at the base: 234 ≠ 246
master       COMMANDS.md cell=cli_doccheck.go:246
master       source func line=246          ← in step

$ git merge-base --is-ancestor 94776f0c9 0695ee38d; echo rc=$?     # rc=0 — the mover IS in my base
$ git log -1 --format='%h committer=%cd %s' --date=iso 94776f0c9
94776f0c9 committer=2026-10-13 19:43:39 +0800 fix(cmd/agent): doccheck's abstention names the half it measured, not a file that is present
$ git merge-base --is-ancestor 05c2a3a36 0695ee38d; echo rc=$?     # rc=1 — the regeneration is NOT
$ git merge-base --is-ancestor 05c2a3a36 master;   echo rc=$?     # rc=0
$ git log -1 --format='%h committer=%cd %s' --date=iso 05c2a3a36
05c2a3a36 committer=2026-10-13 21:32:18 +0800 fix(gendoc): rebuild the COMMANDS.md verb index from its writer, and let the edge catch a region anchor that resolves to prose
```

**Measured:** the line mover (`94776f0c9`, `19:43:39`) sits in this run's base; the
regeneration that re-points the cell (`05c2a3a36`, `21:32:18`) does not — my base was
cut `21:29:53`, i.e. **2 minutes 25 seconds BEFORE the repair landed**, and this run's
branch-scoped suite is RED at its base because of it
(`TestCommandsDocGeneratedBlockMatchesSource`: `got: …cli_doccheck.go:234` / `want: …:246`).

⇒ INFERENCE: the window is not rare and it is not attributable to a bad actor — it is
the **normal ground state of a merge-heavy integration ref**, and a third independent
occurrence inside a single afternoon closes that question. The same three shapes appear:
`01a0a028` (repair before claim), `01a1350f` (repair between claim and ground), this run
(repair **2m25s after the ground's base stamp**, not yet in any branch on the machine
that needed it). It does **not** change either decision; it corroborates both —
(a) for §2: the executor is exactly the party who meets the red suite, so the preflight
"is this red mine?" is the only check that can be cheap, and a ground-time check would
face the same question the executor already faces; (b) for §3: a class with three
members in one day, no discriminating predicate, and a freeze table that already has a
row for it is churn to be **absorbed**, not machinery to be built.

The cost, named precisely and paid by this run: a red base costs one triage
(which gate, whose input, is it in my diff?) before the branch's own test evidence can
be trusted. That triage is `git diff --name-only <base>..HEAD` intersected with the
failing tests' own named inputs — three commands, seconds — and it is quoted in
`artifacts/v4-test-budget-verdict.out`. It is a **read**, not a mechanism.

## 4. What this document is NOT

- **Not a fix.** No code changed; no table bytes changed; no new key, flag, gate or
  script. Rule 35's four questions are answered by *not creating the dependency*.
- **Not a re-litigation of `01a1350f`.** That row's own report already carries the
  correction; this document re-measured it and adds the two decisions.
- **Not a claim that stale briefs are harmless.** The cost is real: `01a1350f` burned a
  full run (27m45s) on a repair that was already in its base. The decision says the
  remedy is upstream of any seam — in what the briefing writer puts in step 1.

## 5. Evidence index

`~/.gopi/sessions/01a13534-13bf-794d-8484-b51a7c15b137/artifacts/`

| file | what it holds |
|---|---|
| `m1-ledger-rows.out` | both ledger rows' own fields (`created_at` 19:39:26, `claimed_by` 20:03:01) |
| `m2-claim-timeline.out` | claim-instant tip/ancestry/permit/line + author-vs-committer table + first-parent timeline |
| `m3-attribution.out` | `30350b652`'s DOC-INDEX rewrite, the `-S` origins, `c37219666` |
| `m4-ground-and-ancestry.out` | `is_ancestor(38577cc3e,fcc335e)=0`, `fcc335e→5604465ef`, first-parent arrival |
| `m5-drift-mover.out` | the call site's line per revision; the file's line count per revision |
| `m6-census-churn.out` | 43/143 commits touch an anchored file; 39 of them merges |
| `m7-first-frozen-class.out` | the `first-frozen` field's class (and its limit, §3.4) |
| `m8-claim-seam.out` | `cli_tasks.go:2856` + its documented best-effort contract |
| `m10-q2-census.out` | the corrected Q2 generator (merge share, sync counts, ratchet 163→166) |
| `m11-q2-census-d.out` | mover-of-bytes vs carrier-only, per merge |
| `m12-my-own-ground-third-instance.out` | §3.5 — this run's own base (cell `:234` vs source `:246`), the mover in-base, the regeneration out-of-base, the 2m25s window |
| `v2-test-budget.out` | the branch-scoped budget run's own output and `stamp:` line (exit=1, `outcome=fail`, budget did NOT trip) |
| `v4-test-budget-verdict.out` | the reds judged: per-failing-test `in-diff=0` against `git diff --name-only 0695ee38d..HEAD`, plus the three owning ledger rows |
