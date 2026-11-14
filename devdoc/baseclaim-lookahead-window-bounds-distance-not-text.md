<!-- doc-class: historical -->
# The look-ahead window bounds DISTANCE, never TEXT — a bare full-length revision was read as a 31-hex clip (row `01a1373b`, 2026-10-15)

**Scope:** `internal/taskreport/baseclaim.go` (`baseClaimCommitAfter`) and the two
faces that quote what the leaf scanner returns — `internal/tools/task_briefing_premise.go`
(dispatch seam) and `internal/tools/task_briefing_base_submit.go` (submit seam).

## What the defect was, measured

`baseClaimCommitAfter` applied its look-ahead window to the TEXT before it tokenised:

```go
window := baseClaimRuneWindow(line[at:], baseClaimLookaheadRunes)  // 32 runes
for _, tok := range implClaimTokens(window) { … }
```

A revision standing bare right after the base word therefore got CUT. With
`baseClaimLookaheadRunes = 32` and the four characters of `base`:

    "Measured at base 74f5df769ec605149273b0cd6ad9f918452f2588: the surface is red."
                    └────────── 32 runes from after the base word ──────────┘
    read as commit = 74f5df769ec605149273b0cd6ad9f918452f91   (31 chars)

`BaseClaimsIn` returned a `Commit` the author never wrote, and both producer faces
quoted it into re-runnable `git` commands (`git merge-base --is-ancestor <rev> <ref>`,
`git rev-list --count <rev>..master`) as if it were the author's own spelling.

**The string the seams hand to git IS the clip** (measured): both faces call
`MasterClaimEvidenceAt(repo, c.Commit)` (`task_briefing_premise.go:243`,
`task_briefing_base_submit.go:137`), whose first act is
`repoSweepGit(repo, "rev-parse", "--verify", "--quiet", e.Rev+"^{commit}")`
(`master_claim.go:263`) — `e.Rev` being the leaf's returned token verbatim.

**Git masks the wrong spelling in THIS repository** — measured against an unmodified HEAD:

    git rev-parse --verify --quiet c4a7eca9ac0d10c052a3e12c9d2583c^{commit}
    → c4a7eca9ac0d10c052a3e12c9d2583c77cc487b2      (resolved: the 31-hex clip is unique)

which is why the defect is a wrong VALUE rather than a wrong verdict on every run: with
a unique clip both seams still resolve the same object, and the clause's git commands are
rendered from the RESOLUTION (`ev.ShortCommit`, `ev.Commit`), so the clip is invisible in
the output and only the quoted line betrays it. **What that masks is a property of the
repository, not a guarantee the leaf may lean on** (inference, stated as such): a clip
that is ambiguous resolves to another object or is refused by git, and a clip below git's
minimum abbreviation length fails outright — measured on the same HEAD with the same verb,
a single-hex-character prefix exits 1. A base declaration's artifact is a REVISION, so the
value the leaf yields must be the author's own token, whatever git then does with it.

## The remedy, and the line it is drawn on

The window is a bound on **how far** the revision may stand from the base word — it
must never be a cut on the revision itself. So it is applied at **token level**: the
tail is tokenised whole (`implClaimTokens`, the ONE definition of "what is a hex
run"), and the window decides only which tokens are close enough to belong to the
base word:

- `baseClaimTokensWithin(tail, baseClaimLookaheadRunes)` walks the tail's tokens, takes
  each token's own START offset, and keeps those starting inside the window — a token
  that BEGINS inside is returned WHOLE, whatever its length.
- `baseClaimRuneWindow(s, n)` survives as the MEASURE the bound is read from (the byte
  length of the at-most-n-rune prefix); its comment now says it is read as a measure,
  never as text to scan.
- the ENCLOSURE check (`baseClaimEnclosedAt`) also reads the UNTRUNCATED tail: a fenced
  or parenthesised token's closing delimiter lies past its own start, so truncating
  before that check could hide the very shape the first pass exists for.

The cursor walk is exact rather than a heuristic: `implClaimTokens` emits maximal,
non-overlapping runs in order, so each successive `strings.Index` lands on that token's
own occurrence. A missed index is skipped rather than allowed to desynchronise the walk
(matching is on the token's start, never on "any later occurrence").

**Narrowness is unchanged** — the property that would have died silently under a
lazier fix ("return the longest hex run on the line"): a base word with no
commit-shaped token still yields NO claim, and the glue/word-boundary behaviour
(`codebase`, `database`, `the base case`) does not regress. The test file asserts both
halves, so the obvious over-broad rewrite fails the same commit that fixes the clip.

## Evidence (this run)

    $ go test ./internal/taskreport/ -run 'BaseClaim' -count=1          # PRE-FIX (HEAD c4a7eca9a)
    --- FAIL: TestBaseClaimsReturnsABareFullLengthShaWhole (0.00s)
        baseclaim_test.go:154: declaration "Measured at base 74f5df769ec605149273b0cd6ad9f918452f2588:
        the surface is red.": commit = "74f5df769ec605149273b0cd6ad9f918452f91" (31 chars), want the
        author's 40-character token "74f5df769ec605149273b0cd6ad9f918452f2588"
    FAIL	github.com/nick/gopi/internal/taskreport	11.660s

    $ go test ./internal/taskreport/ -run 'BaseClaim' -count=1          # POST-FIX
    ok  	github.com/nick/gopi/internal/taskreport	4.548s

**The submit seam, run for real against a real repository** (Rule 31 — the binary
built from this branch, an isolated profile, a repo whose base is one commit behind
its tip, and a payload whose base is a BARE FULL-LENGTH sha):

    $ GOPI_HOME=$SB bin/gopi task submit "measured at base with a bare full-length sha" \
        --payload @payload-full-sha.md --repo <repo> --source agent
    Task queued: 01a13787-20f9-71ff-86ba-5bbda03cd49c (status=pending, source=agent, tags=—)
    [Base revision — a declared base was re-measured at submit]
    warning: 1 declared base revision(s) in this payload are already behind the integration branch …
    - base a3f985ee is 1 commit(s) behind 170c5633 (`git rev-list --count a3f985ee27d7807c1c7733a8de552a5a94c4c00a..refs/heads/master` → 1);
      payload line: "Measured at base a3f985ee27d7807c1c7733a8de552a5a94c4c00a: the surface is red."

The quoted command carries the author's FULL 40-character token, verbatim. (This face's
revision VALUE was always the resolution, so this run proves the seam end to end rather
than discriminating the clip; the discriminating measurement is the unit test above.)

Raw command output is kept in this run's session directory (Rule 37):
`~/.gopi/sessions/01a1373b-6853-7aa0-87c1-146bd1f4059a/artifacts/{before,red-proof,green-proof,branch-scoped-tests,cli-submit-real}.txt`.

## Why the SUBMIT-seam test changed in the same commit

`TestSubmitBaseStalenessIsSilentWhenTheBaseIsTheTip` asserted
`strings.HasPrefix(tip, claims[0].Commit)` with a comment explaining that the scanner
"reads a bounded window after the base word, so the token it yields is the commit's
PREFIX — assert on the prefix relation, not on equality, **or the test would pin the
window's width as if it were the API**". That is the defect stated as a contract: the
assertion could not fail while the clip existed. It is now equality against the author's
revision. `TestBriefingBase_ABareFullLengthShaIsMeasuredWhole` (dispatch seam) locks the
same property one level up — a red proof cannot be built there, because it would need
two objects sharing a 124-bit prefix; the red proof lives in the leaf's own unit test.
