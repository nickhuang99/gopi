<!-- doc-class: historical -->
# Gate9's stale permit was a briefing-time observation, already repaired before this ground

**Record of one measurement, 2026-10-16, task `01a141d2-8913-7b17-9b57-636f227d655e`.**
Not a standing claim about master: the fact below was true when it was taken and the
numbers in it will go stale. No production file was changed by this run.

## What was asserted, and what the ground actually says

The briefing asserted a red `internal/standards` git-env gate on the integration tip:
a baseline permit `cmd/agent/cli_tasks_help_surface_basic_test.go:1365` matching no
hit, allegedly pre-existing at base `3124ec4`.

Measured on this ground (`HEAD = 8dd15ee11`, cut from `refs/remotes/gitPushcheck/master`),
with `git status --porcelain` empty — i.e. an unmodified tree:

| reading | value |
|---|---|
| `TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly` | **PASS** — `git-env gate: 1915 test files scanned, 73 hit(s), 72 baseline entries`, 0 stale |
| quoted permit count, `HEAD` / `refs/heads/master` / `branch-refs/home-nick-work-GoPi/master` | 0 / 0 / 0 |
| quoted permit count, `3124ec4` | 1 (the briefing's line, `baseline.go:964`) |
| `os.Environ()` hits in `cmd/agent/cli_tasks_help_surface_basic_test.go`, `HEAD` / `master` / `3124ec4` | 0 / 0 / 1 |

The permit and the site it keyed both disappeared in the same commit,
`b5cb0628c` "fix(standards): repair the 4 fixture git-env leaks and ratchet the Gate9
baseline down" (2026-10-16 09:36:09 +0800), which is an ancestor of this ground's HEAD.
So the briefing described a state that no longer exists here: measured at HEAD there is
**no stale permit to delete, and the Gate9 substantive arm is already green**. The
sanctioned action on this ground is therefore a **zero-line change** — adding or deleting
a permit to "fix" a green gate would move the ratchet the wrong way.

Two shapes are worth naming apart, because they look alike and take opposite actions:

- **the permit was rotted (a line shift)** → the site still exists, so the fix is a
  re-key or a repair; deleting the permit would leave an unbaselined hit.
- **the permit was repaired (this case)** → the site is gone, so the permit's removal
  *is* the ratchet moving down, and it happened with the repair, in one commit.

The stale-permit error text ("delete it (ratchet down)") does not distinguish them; the
`git show <rev>:<file>` reading of the site does.

## The red that IS here is not this one, and not this run's

`go test ./internal/standards -count=1` on this ground is red for two unrelated gates,
and both reproduce identically on a detached probe worktree at `refs/heads/master`
(`01c167412`), so neither is caused by this branch or by this run:

- `TestCommandsDocGeneratedBlockMatchesSource` — `COMMANDS.md`'s generated verb index
  disagrees with the source (`gopi task` `cmd/agent/cli_tasks.go:525` vs the tree's `:531`;
  at master the same test fails one verb earlier, `gopi notify` `:573` vs `:605`).
  **The named remedy is the wrong instrument here**: this is a CROSS-BRANCH artifact —
  the generator runs against the *checkout it sits in*, so a regenerated block can only
  be correct for the line numbering of the tree that produced it. Regenerating it on a
  feature branch bakes that branch's own numbering into a tracked file, which is red the
  moment it merges next to any commit that moves a verb dispatch line; and a run that
  merely `git merge`s master into the branch re-introduces the same disagreement without
  editing anything. That is the measured behaviour of a cross-branch generated artifact,
  not a defect this run may repair from inside one branch. It belongs to whoever owns
  the sweeps at the integration point, where the numbering is final.
- `TestDocClassBannerMatchesManifest` — `devdoc/removal-audit-shell-census.md` carries a
  `<!-- doc-class: historical -->` banner while `DOC-INDEX.tsv:1582` classes it
  `normative`; the doc landed in `8ddf9cf28` (not an ancestor of this ground). The
  banner's own complaint is right ("only [historical] classes are bannered"), so the
  repair is a class decision on a document this run did not write — and the doc's index
  reason cites standing/record usages that make "which side is correct" someone else's
  call, not a mechanical one.

Both are dispatchable residue (Rule 26); this run reports them rather than reaching into
another owner's document or regenerating a cross-branch artifact.

## Note on the line the permit named

At `3124ec4` the site was `cmd/agent/cli_tasks_help_surface_basic_test.go:1365`
(`git show 3124ec4:internal/standards/baseline.go` → line 964), while the git-env gate's
own baseline comment at that revision already recorded the hit as having moved to
`:1367` — a key one line behind its own list's own note. Both are moot here: neither
`1365` nor `1367` is a live hit on this ground, since the site is gone.
