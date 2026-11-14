<!-- doc-class: historical -->
# Task Report: a version name should mean something

Epic (verbatim, 2026-09-21):

> a version name should give a meaningful name, or a literal numbered version,
> such a tag name is meaningless to human eyes. what info can this almost
> binary number convey to human? why cannot we give a tag name by its git log
> title words combination so as to pass on to some info. also, that does this
> commits since tag: 552 mean do user? is it misleading as stale version?

The artifact the operator pasted (`~/work/GoPi`, `gopi version`):

```
v20260918-1400-0317d09a+552 (53354e86f341)
revision: 53354e86f341ea2f6082aaeecbd30174ebf35fab
tag:      version/v20260918-1400-0317d09a
commits since tag: 552
```

Three complaints, measured before touching anything (all numbers re-measured
on master tip `0652b5db`):

1. **`0317d09a` conveys nothing.** The name is stamp + short revision; the
   annotated MESSAGE that does say what landed is invisible unless the reader
   runs `git show`.
2. **`+552` / `commits since tag: 552` reads as staleness.** The tag is a MERGE
   ROUND's name — `git describe` puts master 552 commits past it, and **145 of
   those 552 are merge commits** (`git rev-list --count --merges <tag>..HEAD`),
   i.e. they are landings no round ever named. Nothing in the output said so.
3. **The tag body was EMPTY.** `git tag -l -n99 version/v20260918-1400-0317d09a`
   printed the name with no subject: that mint ran with no branch list (the
   adoption/verify-gate landing path), so `git show` — the one place the full
   story lives — had nothing to show either.

## Completed

### 1. The name now carries the words (`internal/version/name.go`, new)

Minted shape: `version/v<UTC stamp>-<title slug>-<short revision>`

```
version/v20260921-1430-pool-claim-race-6be1718      (fixture, measured)
version/v20260921-1430-brief-t2-look-without-fixing-bound-0652b5db83ac   (this repo's own tip, measured)
```

- The slug is derived from the **commit titles the position carries** (the
  operator's own suggestion), `git log -n24 --pretty=format:%s`, newest first.
- A merge subject (`Merge branch 'feat/x'`) keeps the branch name — the
  boilerplate drops, what the merge NAMES survives.
- A conventional-commit prefix is *opened* (`docs(brief):` → `docs brief:`)
  so the SCOPE survives and the type word (which is on the drop list) does not
  swallow it.
- A trailing reference (`(#412)`, `(65fd7d2f)`) drops.
- Fallback chain: commit titles → the merger's landed BRANCH names → the
  title-less shape. A mint NEVER fails for history reasons.
- Budgets, all named constants: `maxSlugLen` 48 runes (measured against this
  repo's longest real subject), `maxSlugWords` 8, `maxTitleProbe` 24,
  `minSlugWordLen` 2, `minSlugLen` 4.
- `isRevision` guard: a slug that reads as a hash keeps the title-less shape —
  `v…-deadbeefcafe-<rev>` would be the defect this epic removes wearing words.
- **The short revision is ALWAYS the last dash token**, so `RevisionOf`,
  `MatchesRevision` and `BorrowedName` provenance are untouched by the
  readable half.

### 2. The distance says what it means

One line (`Display`):

```
v20260918-1400-0317d09a+554 unminted (0652b5db83ac)
```

Report (`Description`), measured on master tip:

```
revision: 0652b5db83ac05f21e7157fd73ac35236405507b
tag:      version/v20260918-1400-0317d09a
describes: the merge round that minted this name (2026-09-18T21:59:12+08:00)
distance: 554 commits (145 merge rounds) past it
landing: an unminted tail — the name below still identifies the merge round this build descends from, it is not a claim that the build is stale
```

- `unminted` is a named constant (`unmintedMarker`, Rule 1b) used by both the
  one-liner and the report, and ONE word is enough to kill the "stale
  version" reading: the count is landings past the nearest NAME.
- The count is git's OWN describe distance, which EXCLUDES the named commit
  itself — measured on the real repo: `git rev-list --count <tag>..HEAD` = 554
  = the `-554-` in `git describe --tags --long` (the `tag^` form would be 555),
  so the number agrees with whatever the reader checks it against.
- New `Info` fields: `MergeCommitsSince`, `TagCommitDate`, `UnnamedLandings`.
  All best-effort (a repo that cannot answer leaves them zero and the renderer
  phrases without them rather than inventing a number).
- A build AT its name prints `landing: this build is the named position
  exactly` and no distance line — never a `0` that could read as stale.
- New report keys are exported constants (`ReportDescribesKey`,
  `ReportDistanceKey`, `ReportLandingKey`) alongside the existing two, so
  producer and reader cannot drift (Rule 13).

### 3. The tag body explains its own name

`tagMessage` gained `from titles: <slug>` above the `merged branches:` list, so
even a round whose branch list is empty (the adoption/verify-gate path that
produced complaint 3) answers `git show <tag>` with words.

### 4. One parser fix the new marker made necessary

`RevisionOf` read a tagged display's dirtiness as `"-dirty ("` adjacency. The
unminted marker is appended AFTER the dirty marker, so the predicate became
`nameHalfIsDirty(name)` — the dirty witness is the producer's own token on the
name half, not a position in the string. `IsDirty` (used by the rebuild
verifier) is unchanged and still sees both shapes.

## Not Done (honest)

- **Existing 146 `version/*` tags are NOT retro-renamed.** A minted name is a
  historical fact referenced by devdocs, merger logs and every past
  `gopi version` output; rewriting them would orphan that evidence. The
  improvement applies to names minted from here on. Retro-naming is a
  separate decision (it would need `git tag` rewrites of annotated tags and a
  devdoc sweep) and is not in this branch.
- **The one-liner's `+N` position is kept** (`v…+554 unminted`), not dropped:
  the rebuild verifier and the merger log parse this line, and a distance is
  genuinely useful — it is the WORD that was missing.
- **`devdoc/worker-daemon.md` line 194** still quotes the old
  `version/<stamp>-<rev>` shape in an evidence citation about daemon restart
  behaviour. That is a record of what a past run observed, not a spec; left
  as-is deliberately (same reason this report does not rewrite the epic's
  pasted output).
- **Go stamps no VCS info in a linked worktree** (measured: `go build` in
  `/home/user/workspace/gopi-version-name` embeds no `vcs.revision`, while the
  same build in `/home/user/work/GoPi` or in a clone does; `.git` is a FILE
  there). So an unstamped build from a worktree prints `dev`, and the
  end-to-end `gopi version` demo for the tagged report had to be produced from
  the real checkout (or via `Describe` directly). **This is pre-existing and
  independent of this change** (verified: an untouched `master` worktree build
  behaves identically), it is not caused by the readable half — but it is worth
  a follow-up task, because the post-merge rebuild that runs *inside a
  worktree* is exactly the consumer that wants the VCS fallback.

## Verification

- Build: `go build ./...` → OK; `gofmt -l internal/version` → empty
- Tests (branch-scoped smoke, Rule 27):
  - `scripts/test-budget.sh ./internal/version/` → **exit=0 outcome=pass**
  - `scripts/test-budget.sh -run 'Rebuild|Version|Probe|DirtyWitness|Tag|Mint' ./internal/tools/` → **exit=0 outcome=pass**
  - `scripts/test-budget.sh -run 'Version|Merge|Rebuild|Mint|VersionTag' ./cmd/agent/` → **exit=0 outcome=pass**
  - `scripts/test-budget.sh ./internal/standards/` → exit=0 outcome=pass
- End-to-end mint on a real fixture repo (measured, `internal/version`
  `Describe`/`MintTag` driven directly):

```
=== BEFORE (position carries no version tag) ===
6be1718716cb
revision: 6be1718716cb3a437a3925794f815a7930283c4b

=== MINTED ===
version/v20260921-1430-pool-claim-race-6be1718

=== AFTER ===
v20260921-1430-pool-claim-race-6be1718 (6be1718716cb)
revision: 6be1718716cb3a437a3925794f815a7930283c4b
tag:      version/v20260921-1430-pool-claim-race-6be1718
describes: the merge round that minted this name (2026-09-21T14:48:43+08:00)
landing: this build is the named position exactly

$ git tag -l -n99
version/v20260921-1430-pool-claim-race-6be1718 gopi version tag — post-merge position 6be1718
    from titles: pool-claim-race
    merged branches:
      feat/pool-claim-race
```

## Evidence (self-filed)

- Red proof #1 (the readable half is load-bearing): with
  `mintNameSlug` forced to return `""`,
  `TestMintTagNamesThePositionWithTitleWords` (internal/version) AND
  `TestMintedNameIsWhatTheUserFacingVersionPrints` (internal/tools) FAILed;
  both passed again on restore.
- Red proof #2 (the marker is load-bearing): with the `" "+unmintedMarker`
  append removed from `decorate`, `TestDisplayMarksTheDistanceAsUnminted`
  FAILed with `Display() = "v20260918-1400+552 (0652b5db83ac)"; a tagged build
  past its name must say the count is unminted`; green on restore.
- Measured tag/lag facts (real repo, `0652b5db`): `git describe --tags --long`
  → `version/v20260918-1400-0317d09a-552-g53354e86`; after fetching master's
  newer commits → 554; `git rev-list --count --merges <tag>..HEAD` → 145;
  `git tag -l -n99 version/v20260918-1400-0317d09a` → name only, no body.
- Worktree state after the commit:
  `git status --porcelain` → prints nothing.

## Files

- `internal/version/name.go` (new) — the naming policy.
- `internal/version/name_test.go` (new) — policy tests pinned to real subjects.
- `internal/version/version.go` — `unmintedMarker`, the report keys, the
  distance/describes/landing lines, `nameHalfIsDirty`, the new `Info` fields.
- `internal/version/git.go` — `gitLines`, `gitSubjects`, `parseUint`.
- `internal/version/tag.go` — `mintedName`, slug in the tag message.
- `internal/version/version_test.go`, `internal/tools/rebuild_tagged_version_test.go`,
  `cmd/agent/cli_tasks_rebuild_test.go` — expectations moved from a re-typed
  shape to the minted tag itself (`BorrowedName(tag)`), plus the new red-proof
  tests.
- `cmd/agent/cli_version.go` — doc only (the display example).
