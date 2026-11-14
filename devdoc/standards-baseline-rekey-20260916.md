<!-- doc-class: historical -->
# devdoc: re-keying the two standards-baseline entries shifted by 1a5e8ea7

Branch `fix/standards-baseline-rekey` (worktree `/home/user/workspace/gopi-baseline-rekey`),
off master tip `0dac2ae7`. Commits `07b2a152` (re-key), `d6929017` (Rule 17 escapes).

## The failure mode (why master's own tip was red)

`internal/standards/gate.go`'s `enforce()` compares two sets: the gate's hits
(keyed `file:line`) and the baseline's permits (also keyed `file:line`). A hit
that is neither baselined nor inline-escaped fails as NEW; a baseline entry that
matches no hit fails as STALE. **No content fingerprint is part of the key** —
so ANY insertion above a baselined site makes the same unchanged site appear
twice: NEW at the new line, STALE at the old one.

Commit `1a5e8ea7` (merge-time UNION COMPILE gate, ⓪c) added 62 lines and removed
13 in `internal/tools/task_merge.go`, extracting the build-refusal lane into
`taskMergeRefuseUncompilable`. Everything landed ABOVE the two baselined sites,
shifting them −4: Rule 16 `:362 → :366`, Rule 18 `:332 → :336`. Master merged it
without re-keying, so `go test ./internal/standards/` has been red on the tip.

## The discriminating test: pure shift vs. new site

Run the site's own text through both revisions and compare the LINE NUMBERS
only, not the lines:

```bash
diff <(git show 1a5e8ea7^:internal/tools/task_merge.go | grep -n '<site text>') \
     <(git show 1a5e8ea7:internal/tools/task_merge.go  | grep -n '<site text>')
```

Identical text at a different number = pure shift → **re-key**. Different text =
the site really changed → behaviour fix (and only then remove the permit).
Do this per site: the two sites here are both pure shifts, but the test is what
establishes that, not the "an insertion happened nearby" story.

A useful second signal: if EVERY occurrence of the chain moved by the same delta
(−4/+4 across all three `branch","-d"` occurrences), the insertion is above all
of them — consistent with a shift, not with an edit that happened to land on one.

## Re-key, never delete

The ratchet is over **permits**, not reds. Deleting an entry does not "keep the
gate ratcheting" here — it de-licenses the address, so a genuinely new
violation at that spot would be silently accepted later. Re-key in place and
extend the entry's comment with one `re-key: <cause>` line, then verify the
per-list entry COUNT is unchanged. Here: `errorWrapBaseline` 8→8,
`testEnvBaseline` 86→86, `elseAfterReturnBaseline` 3→3.

Note the second-order effect visible in the data report: a re-key moves a hit
from the NEW column into the BASELINED column (`baselined=7→8` for Rule 16,
2→3 for Rule 18) — so the `hits=` total is the thing that must NOT move. If the
total moves, something other than a re-key happened.

## Capture BEFORE evidence in the fixed tree

Do not try to reproduce the red on a pristine master worktree and then apply the
fix — the whole point is that both states differ by ONE file. In the fixed tree:

```bash
git checkout 0dac2ae7 -- internal/standards/baseline.go   # master's baseline
CGO_ENABLED=0 go test ./internal/standards/ -count=1 -v   # NEW@366 + STALE@362, NEW@336 + STALE@332, exit 1
git checkout HEAD -- internal/standards/baseline.go       # restore; git status must be clean
```

This isolates the cause perfectly: same tree, same test files, only the permits
differ — so the red is attributable to the key, not to anything else on the branch.

## The unrelated pre-existing red — verify containment before reporting it

`go test ./internal/standards/` is red on master's tip for a THIRD reason: Rule
17 reports 2 NEW sites in `internal/agent/journalreport_test.go` (`os.Unsetenv`).
Two traps here:

1. **It is not a regression from a re-key** — a baseline re-key cannot introduce
   a hit in another file. Prove it from history: the file's only commit
   (`4a42577e`) is an ancestor of master and does not touch `task_merge.go`.
2. **It may still be pre-existing-on-the-tip** — two unmerged branches
   (`f9311942`, `49f5e49a`) already fix exactly those sites, and master absorbed
   neither. So the red is live but the diagnosis is "awaiting a merge", not
   "nobody noticed".

The remedy for a genuine Rule 17 exception is the gate's own inline escape
(`// env-leak-ok: <reason>`), NOT a baseline entry — Rule 17's premise
(`t.Setenv` restores automatically) is false for this case: `t.Setenv` can only
SET a variable, and after it `os.LookupEnv` reports the name PRESENT holding
`""`, which is exactly the state the covered test's `absent` row must tell apart
from true absence. Escaping keeps both the red and the reasoning visible in the
data report; baselining would have widened the ratchet, which the task forbade.

## Where this can bite again

Any `{file}:{line}` allow-list is armed against every future insertion above its
sites. The instrumentation that makes the failure legible is exactly what the
gate prints: the NEW/STALE **pair**. A single NEW without a STALE is a real new
violation; a NEW + its STALE twin one line apart is a shift. Read them together.

## The land, 2026-09-17 — flattening so master is an ancestor

This branch was parked (`archive/fix/standards-baseline-rekey`) after the merger
refused it, and the refusal was NOT about the conflict: resolving a conflict by
`git merge master --no-commit --no-ff` + commit produces a MERGE COMMIT, so
`git merge-base --is-ancestor master HEAD` exits 1 and the merger (rightly) parks
a non-ancestor. The tree was fine; the LINEAGE was not.

Resolution (branch `fix/standards-baseline-rekey2`, task `01a0aaab`): keep the
merge commit as the resolution RECORD, then flatten the tip.

```bash
git tag backup/standards-baseline-rekey2-mergetip <merge-commit>
git reset --soft master          # index keeps the resolved tree; HEAD -> master
git diff --cached --name-status  # MUST list only what the branch really changes
git commit -m "<the merge commit's message>"
git merge-base --is-ancestor master HEAD && echo FF-ABLE
```

`git reset --soft` is the whole trick: it moves the branch pointer WITHOUT
touching the working tree or the index, so the resolved content is already
staged and one commit lands it on master's tip.

Trade-off, stated rather than hidden: after flattening, this branch's original
three commits (`07b2a152`, `d6929017`, `c48f5436`) are no longer ancestors of the
tip. They survive on the old ref `fix/standards-baseline-rekey` and on the
backup tag; the parked branch deliberately does NOT appear in `git log
master..HEAD` because it carries none of these moves. That is the correct trade
when the deliverable is defined as "a tip for which is-ancestor succeeds" — a
merge commit cannot satisfy it, and a rebase would replay the OLD tree through
the merge with no guarantee of reproducing the resolution.

The two permit keys and the two escapes are unchanged by the flatten: the
content of `baseline.go` and `journalreport_test.go` is byte-identical before and
after, since `reset --soft` never rewrites a file.
