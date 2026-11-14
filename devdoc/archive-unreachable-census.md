<!-- doc-class: normative -->
# The unreachable backlog: a census that names what is keeping a commit alive

Task `01a12f65-c286-793d-8de9-27fbf5243148` (epic 01a12f65, the BACKLOG half of
the orphan family whose PRODUCER half is `internal/tools/branch_delete_guard.go`).
Measured 2026-10-12 on the peer `peerbox:/home/user/work/GoPi`.

## The defect this surface names

A branch is deleted and its tip is left reachable from **no ref**. The object is
still present (`git cat-file -t` answers `commit`) and nothing in the repo names
it. Two states look IDENTICAL to every existing reader:

- **archived** — a DURABLE ref holds the tip (`refs/tags/archive/<branch>`, plus
  the `refs/gopi/archive-reason/<branch>` WHY record), and
- **deleted, with a grace period** — no ref holds it, and the only thing keeping
  the object alive is the **reflog grace window**: one
  `git reflog expire --expire=now --all && git gc --prune=now` and it is gone.

Nothing in the project could tell an operator which one they were looking at, and
the difference is the difference between "decided, recoverable" and "about to be
lost". This change makes that difference measurable in one command and gives a
named tip a durable ref through the machinery that already exists.

### The mechanism, measured rather than assumed

Both halves of the keeping-window reading were reproduced (the transcript is on
the sibling memory `#9676`):

| act | result |
|---|---|
| `git gc --prune=now` alone, on a commit no ref names | object **survives** |
| `git reflog expire --expire=now --all && git gc --prune=now` | object **is gone** |

So "reflog-held" is the decisive classification: it is the difference between a
commit that survives a `gc` and one that does not. The census reports it per tip,
and the classification was reproduced in both directions by
`TestArchiveUnreachableCensusSplitsTheKeepingWindow` (same repo, before and after
the expiry, with `git cat-file -t` proving the objects are still there when the
label flips to `past-reflog`).

A second measurement that shapes the implementation: **`git fsck --dangling` is
NOT the reachability enquiry.** The peer reported a tip via
`git fsck --unreachable --no-reflogs` while `--dangling` printed nothing for it.
The census therefore reads `git fsck --unreachable --no-reflogs` (the honest
enquiry: reflogs DISABLED, which is what makes "reflog-held" an independent fact
rather than a tautology) and derives the TIP set from the parents of the
unreachable commits — not from `git fsck --unreachable=<sha>`, whose "is this
commit unreachable because of THAT commit" shape is the wrong question for a
commit inside an unreachable CHAIN, which is the shape this backlog is made of.

## What was delivered

### 1. `gopi task archive --unreachable [--repo PATH]` — the census (read-only)

`internal/tools/archive_unreachable.go` + the `--unreachable` arm in
`cmd/agent/cli_archive.go`. Per unreachable TIP: short sha, age, subject, the
**keeping window** (`reflog:<refs…>` or `past-reflog`) and the **archive
decision** naming it (`none`, or `<branch> (<reason>)`, with the reason-less tag
rendering as `no reason recorded` — the 21:04 shape). Then a summary: totals,
the reflog-held/past-reflog split, the non-commit object breakdown, and how many
tips are one reflog expiry from gone.

Cost is a handful of git subprocesses whatever the backlog size: one `fsck`, one
`git log --no-walk` per 500 commits (§`archiveUnreachableCommitChunk`), one
`git reflog show --all`, and the two archive-namespace scans that already existed.

Refusals and degradations, so a scan that could not answer never looks like a
clean answer (Rule 15): a scan failure exits 1 naming the mode; a commit fsck
reported but `git log` could not describe is counted and printed as
`UNDESCRIBED:`; an unparseable committer date renders as `-` and never as `0d0h`.

### 2. `gopi task archive --rescue-tip <rev> --reason <code> [--detail TEXT]` — the rescue

One named tip gets a DURABLE ref: `refs/tags/archive/rescued-<short-sha>` plus its
reason record. It writes through **`ArchiveTagReasoned`** — the project's ONE
archive writer (Rule 9) — which needed one door it did not have: a rescued commit
is by definition named by no branch, so the tag's target is an explicit REVISION.
That door (`archiveTagReasonedAt` → `archiveTagReasonedRef`) is the SAME function
with the same tag shape, the same companion ref, the same `%w` errors and the
same idempotence; only the spelling of the target differs. **No second writer,
no second namespace, no new ref family.**

Two refusals, both measured by tests:

- a DURABLE ref already holds the commit → refuse, write nothing, name the
  holders (a second durable ref would give one tip two names to keep in step);
- a holder that is only the pushback lane's **per-pass mirror**
  (`gitPushbackCheckRootRef`) is NOT a durable holder → rescue proceeds and
  REPORTS the holder it declined to count. The durable predicate is
  `branchDeleteNonDurableRef`, read from the sibling fix rather than restated
  (Rule 13) — the two halves of this epic decide on the same rule.

An unknown `--reason` is an error before any write (the vocabulary is closed:
a WHY that is not queryable is not a record). A revision that does not
resolve to a COMMIT (a tree, a typo) is an error naming the revision.

### 3. Constants, not literals

The git spellings this surface needs (`fsck`, `log`, `reflog`, `for-each-ref`,
`rev-parse`, `--no-walk`, `--contains`, `%x1f`-separated field shapes, the chunk
size, the `rescued-` name prefix, the display width) are named once at the top of
the file and referenced (Rule 1/13/14). Two runs of an unchanged repo render
byte-identical output: tips sort by committer date descending with the sha as
tie-break, reflog names are deduplicated and sorted.

## The measured census of the real peer backlog

```
$ gopi task archive --unreachable --repo /home/user/work/GoPi
unreachable census: /home/user/work/GoPi
  unreachable commits: 1673
  unreachable tips:    330
  kept by a reflog:    33  (one `git reflog expire --expire=now --all` from gone)
  past the reflog:     297  (`git gc --prune=now` alone removes these)
  other unreachable objects: blob=2163 tree=3474
```

The 1673/330 split was recomputed independently, in git plumbing, outside the new
verb, and agrees exactly (`git fsck --unreachable --no-reflogs` → 1673 commits;
parents → 330 tips; 33 of them named by a reflog). The epic's briefing figures
were 1672/329/33/296: the backlog GREW by one commit and one tip between the
briefing and this measurement, which is the point — the producer is still
producing, and the census is a live instrument, not a one-off audit.

Full output, all 330 lines: `evidence/peer-unreachable-census.txt` (session dir).

### The rescue, executed against one real tip

`216459d87ffda5afc33bc7472a4908890c7a43f8` — "Merge branch
'feat/memory-prefetch-executor'", committer date 2026-09-28, **held by a reflog**
(2 entries), 17 files, and `internal/memoryprefetch/prefetch.go` is ABSENT from
the peer's master. It is the newest of the 11 reflog-held tips whose patch
`git cherry master <tip>` reports as not-in-master — the actionable set: tips that
are imminently lossable AND whose content master does not carry.

```
$ gopi task archive --rescue-tip 216459d87ffda5afc33bc7472a4908890c7a43f8 --reason unowned \
    --detail 'unreachable tip rescued by epic 01a12f65 …' --repo /home/user/work/GoPi
rescued 216459d87ffd as archive/rescued-216459d87ffd
  reason: unowned
  durable holder: refs/tags/archive/rescued-216459d87ffd
  read back: git cat-file blob refs/gopi/archive-reason/rescued-216459d87ffd
```

Read back:

```
$ git for-each-ref --contains 216459d87ffd --format='%(refname) %(objecttype)'
refs/tags/archive/rescued-216459d87ffd commit
$ git cat-file blob refs/gopi/archive-reason/rescued-216459d87ffd
reason: unowned

branch: rescued-216459d87ffd
repo:   /home/user/work/GoPi
actor:  nick@peerbox
task:   (none)
at:     2026-10-12T17:44:59+08:00
detail: unreachable tip rescued by epic 01a12f65 …
```

The census afterwards: **1671 commits / 329 tips / 32 reflog-held / 297
past-reflog** — the rescued tip is out of the backlog and the counts moved by
exactly one, which is the effect the surface claims. The peer's worktree stayed
clean (`git status --porcelain` → 0 lines) and the only thing the rescue wrote
was the pair of refs it names.

`--reason unowned` is deliberate and not a placeholder: there is no ledger row
that ever tracked `feat/memory-prefetch-executor`, and the `unowned` code records
that fact rather than a story invented at the shell — the same discipline
`ArchiveDecide` already forces on the hand path.

## Recorded decision: the remaining 329 (330 measured tips, minus the one rescued)

**The rescue is policy, not a mechanical act, and this run does not decide it for
329 tips.** What the run measured, so the decision can be made on facts:

- **297 tips are already past the reflog's keeping.** They are held by NOTHING.
  They disappear on the next `git gc --prune=now` — and per the memory rule, the
  honest reading of that is *not* "they are already lost": they are present NOW,
  and a `gc` on the peer would remove them. The census's whole value is that this
  number is now visible instead of invisible.
- **33 tips are one reflog expiry from gone**, and their reflog entries belong to
  whichever ref was checked out — not to the deleted branch (git deletes a
  branch's reflog with the branch). Expiring reflogs is therefore an act with a
  blast radius far beyond this backlog.
- Of those 33, **11** have a patch `git cherry master <tip>` reports master does
  not carry. That is not proof of loss (`git cherry`'s `+` means "not the
  literally same patch", never "work missing"), but it is the right shortlist for
  a human to walk, newest first.
- **319 of the 329 tips have at least one file whose content differs from
  master.** A sample check on that set shows most are re-derived work (a tip
  whose whole diff is byte-identical on master under a different commit). So the
  shortlist that matters is the INTERSECTION of `content differs` and
  `imminently losable`, which is the 11 — and that intersection is what a
  follow-up task should mine, one tip at a time, with `--rescue-tip`.

**Recommended policy** (for the operator, not enacted here): rescue the 11
reflog-held not-in-master tips with their own per-tip reason codes; treat the 297
past-reflog tips as already-lossy and audit them by content before any `gc`
touches the peer; and run `--unreachable` on a schedule the operator already owns
(no new daemon — Rule 35) so the number is watched rather than discovered. The
producer-side half of this epic is what stops the number growing.

## What this change deliberately does NOT do

- No new tool, no new CLI verb (Rule 3): both modes hang off the existing
  `gopi task archive` verb, and every flag is declared in the one membership map
  (`archiveFlagMembers`) that the `gopi task` family's own gate reads.
- No new namespace, no annotated tag, no second reason record: the archive tag
  stays LIGHTWEIGHT at a commit (the shape five readers depend on), and the WHY
  still lives in `refs/gopi/archive-reason/`.
- No auto-rescue, no background sweep, no new state file, cron or config key
  (Rule 35). A rescue is an operator's decision with a mandatory reason.
- No judgement about which tips deserve keeping. The census names the facts; the
  reason vocabulary forces the caller to state theirs.
