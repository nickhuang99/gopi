<!-- doc-class: historical -->
# The chat-prompt self-reference salvage: the intent had already landed, and the archive said so


**This is a record of one measurement run** (task
`01a0e92275447da7b93c7cea58dfa431`, 2026-09-29). It claims nothing about master
that a test does not already hold; it records how the question "what is left of
this salvage branch?" was answered, so the next run asked the same question can
answer it in one command instead of an afternoon.

## The question

The branch `salvage/chat-prompt-self-reference-20260928` had been archived: its
ref deleted, a `master...branch` diff kept as an attachment, and the work item
asked for a rework — "carry whatever of that intent is NOT already on master".

## The measurement, in order

1. **Is the tip really gone, or merely unreferenced?** `git cat-file -t
   0ae17e87` → `commit`. A deleted ref is not a deleted object; the archive
   commit that the salvage sweep writes (`archive: salvage tip preserved (no
   other ref contained it)`) is what keeps it reachable, and it is the only ref
   that does:
   `git for-each-ref --contains 0ae17e87` → 1 ref,
   `refs/tags/archive/salvage/chat-prompt-self-reference-20260928`.
2. **Is the salvage tip an ancestor of master?** `git merge-base --is-ancestor
   0ae17e87 master` → `rc=1`. The natural reading is "the intent never landed".
   **That reading is wrong here, and this is the pitfall the record exists for.**
3. **Read the tip's own commit messages.** The salvage commit sits on top of the
   feature branch's real commits — `3e80686d feat(chat): name THIS run's own
   transcript file in the chat prompt`, `7a887af5 style(chat): …`,
   `eb8c1ee6 feat(chat): chat-scoped tool to see its own screen …`,
   `13bf69aa`, `b01ed101 docs(chat): correct the self-reference note to what was
   actually measured` — all under `6936c787 Merge branch
   'feat/chat-prompt-self-reference'`.
4. **Check the merge, not just the tip.**
   `git merge-base --is-ancestor 6936c787 master` → `rc=0`, and
   `git log --oneline --ancestry-path 3e80686d..HEAD` walks straight to the
   rework branch's own base. So master descends from the *feature branch*; only
   the salvage commit (uncommitted workspace content, rescued and committed
   afterwards) hangs off it.
5. **Measure the residue per file, both directions.** For every added line of the
   attachment diff **longer than 12 characters, blanks excluded, comments INCLUDED**
   (the probe's exact criterion), test verbatim presence in `git show master:<file>`;
   for every removed line, test that it is *gone* from master. Result, reproducible
   from the attachment alone: **per-file totals match master exactly for 13 of 14
   files — 709 added lines hit, 9 miss**, and all 9 misses sit in ONE window of
   `internal/agent/chat_ops.go`: 7 comment lines of the union rationale plus the two
   argument lines of the same `BuildClaudeRequest` call (`chatOpsSystemPrompt(a)` and
   the project argument, where the salvage had an empty-string placeholder and master
   has `a.Ctx.ProjectContext`). Master already carries that union decision, measured
   at `internal/agent/chat_ops.go:590-600` with the argument at line 601 — i.e. the
   misses are an earlier spelling of a paragraph, and two argument spellings, master
   made better afterwards, not an absence. Every substantive removal is gone from
   master. A naive union assertion on `devdoc/DOC-INDEX.tsv` finds the same shape:
   0 rows exist only in the salvage tip, 86 rows only on master.

   Reproduce with the attachment verbatim (no `git apply` anywhere in this run):

   ```bash
   gunzip -c <attachment1> > /tmp/salv.diff
   # probe: for each '+'-line >12 chars (blanks skipped, comments kept), is it a
   # verbatim line of `git show master:<file>`?                    → 709 hit / 9 miss
   ```

## What this means for a salvage rework

**A salvage commit's content is a snapshot of work that may already be upstream.**
An archived salvage branch is "unmergeable" for a structural reason — it is the
feature branch plus an out-of-band rescue commit, so `master...branch` carries
every commit the feature branch already merged *plus* the rescue — and a
non-ancestor tip therefore proves **nothing** about whether the intent landed.
The two questions are different, and only the second one matters:

```bash
git merge-base --is-ancestor <salvage-tip> master            # rc=1 is EXPECTED, not a verdict
git merge-base --is-ancestor <the merge commit it hangs off> master   # this is the verdict
```

The cheap tell is in the tip's commit graph: a salvage commit whose parent is far
behind, and whose own subject is "archived: salvage tip preserved", is a rescue
commit, and what it rescued is whatever the *parent's* history did not carry.
Compare the tip against the **feature branch tip** (here `b01ed101`, the merge's
second parent), not against the salvage tip, to see the true extra: 3 files, all
superseded.

## Outcome of this run

Survivor set **empty**; the deliverable is the evidence, not a change. The branch
carries the report and this record. Do not re-apply the attachment: its
`chat_ops.go` hunk would revert master's corrected union comment for the stale
one.

## A second pitfall, in the same run: `doccheck --fix` is not union-safe

`gopi doccheck --fix` (Rule 36's tool, run to classify this run's own devdoc)
**rewrote the manifest against the branch's base tree and silently dropped a row
master had landed minutes earlier** — `devdoc/chat-turn-spill-header.md`. Its
output said `repaired 1 path(s)`, which is true and also not the whole truth:
repairing against a stale base is a *reset* of every row the base never had. The
check that caught it was re-measuring the path-column set difference against
`master`, not trusting the tool's success line:

```bash
# rows master has, the branch's manifest does not — must be EMPTY
comm -23 <(git show master:devdoc/DOC-INDEX.tsv | cut -f1 | sort) \
         <(cut -f1 devdoc/DOC-INDEX.tsv | sort)
```

Restore by taking the row verbatim from `git show master:…` at its
master-adjacent position, then rebase onto current master so the stale-row gap
closes too. Rule 36 says a devdoc that missed the write hook is classified in the
same commit; the corollary learned here is that **classification must be measured
as a union against the integration branch, never as a rewrite of a snapshot**.
