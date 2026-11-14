<!-- doc-class: historical -->
# A briefing's self-declared base revision is the stale element — measured on row `01a136a7-dccd`, and the seam that re-measures it at dispatch

**Scope:** the dispatch-time premise seam (`internal/tools/task_briefing_premise.go`
+ the `internal/taskreport` leaf scanner it reads). **Verdict of the measurement: the
dispatch pipeline was CORRECT; the briefing's own `base` declaration was stale, and it
was stale at AUTHORING time — not at dispatch.** The instrument that would have said so
at dispatch now exists (below).

## The chain, measured (task `01a136e8`, 2026-10-14, host peerbox)

| time (+0800) | commit | what it is | measured by |
|---|---|---|---|
| 02:31:19 | `2f697e5` | master's tip; **declared by the briefing as its base** | `git log -1 2f697e5` |
| 02:41:51 | — | the bug row is first submitted, honestly: at that instant master's tip **was** `2f697e5`, so `gopi tool doccheck` really did exit 1 | row `created_at` + CMD3 |
| 02:45:57 | `34693afb3` | **the fix lands — and its parent is `2f697e5`, the briefed base** | `git log -1 34693afb3`, `git merge-base --is-ancestor 2f697e5 34693afb3` |
| 03:05:20 / 03:05:33 | — | two sibling rows (`01a136a7-ab1b`, `01a136a7-dccd`) are authored from the **same evidence text**, both still declaring `base 2f697e5` | `tasks.payload` |
| 03:42:03 | `c06e04e22` | the child's cut point — the fix is already inside it | `git merge-base --is-ancestor 34693afb3 c06e04e22` |

Master's tip was the base for **14 minutes**, then was the repair for the rest of the
story. At 03:05:33 — the moment the briefing was written — master's tip had been the
repair for **20 minutes**. Measured (CMD3, `git rev-list --first-parent -1 --before=<row created_at> master`):

    34693afb3 Wed Oct 14 02:45:57 2026 +0800 fix(docclass): freeze the anchor-NUMBER drift of devdoc/merge-backlog-terminal-guard.md (frozen=168)

So the two sibling rows were authored from a text whose headline measurement had been
superseded 20 minutes earlier, **without re-measuring it**. The text is identical in both
rows, including the parenthetical `(measured 2026-10-14, base 2f697e5)` — a copy, not a
re-measurement.

## The two shapes in the wild — and the sibling row is the OTHER one

The epic's goal is to distinguish these, and they want OPPOSITE remedies:

- **Stale at AUTHORING** (this row's shape) — the evidence text predates the fix, and the
  fix is already inside both the authoring-time tip and the cut point. Fixing it is the
  submitter's job; no dispatch-time instrument can make an author re-measure. What an
  instrument CAN do at dispatch is refuse to let the stale text be read as a live defect.
- **Stale at DISPATCH** — the briefing was correct when written and the premise went
  stale between submit and claim. That is the sibling shape this seam was BUILT for
  (row `01a12c7b`, 2026-10-12: briefing rendered from a 03:37:55 snapshot, the fix an
  ancestor of `origin/master` by 03:44:05, the run handed the briefing at 03:58).

**The discriminator is master's tip AT THE ROW'S OWN `created_at`** — not the named base,
not the cut point, not today's tip:

    # CMD1 — what the named base actually is
    git log -1 --format='%h %cd %s' 2f697e5
    # CMD2 — is the fix a DESCENDANT of the briefed base? (exit 0 = yes)
    git merge-base --is-ancestor 2f697e5 34693afb3 ; echo "exit=$?"
    # CMD3 — master's tip at the instant the row was AUTHORED  ← THE DISCRIMINATOR
    git rev-list --first-parent -1 --before=$(date -d '2026-10-14 03:05:33' +%s) master \
      | xargs git log -1 --format='%h %cd %s'
    # CMD4 — was the pipeline current at CLAIM time? (exit 0 = the fix is inside the cut point)
    git merge-base --is-ancestor 34693afb3 c06e04e22 ; echo "exit=$?"
    # CMD5 — what the seam itself measures, on this briefing's text (below)

Reading: **CMD3's tip is the fix** ⇒ stale at authoring (the briefing was born stale; the
dispatch is blameless). **CMD3's tip is still red while CMD4 is green** ⇒ stale at
dispatch (the seam's shipped landed/unlanded half is the right instrument). Both were
measured here: CMD3 printed the fix, CMD4 exited 0 — this row is the first shape, and the
cut point and the ground's base (`a0693ba`) were both current throughout.

**The sibling row filed 13 seconds earlier is the OTHER shape**, and that is worth
recording because it is the case where an old base does NOT mean the work is done. Row
`01a136a7-ab1b` (authored 03:05:20, same `base 2f697e5`) asserted that
`TestDocClassBannerMatchesManifest` was RED. Measured:

    $ git log --format='%h %cd %s' -- devdoc/ledger-readonly-faces.md
    c02d7c0df Wed Oct 14 04:05:07 docs(rule36): classify devdoc/ledger-readonly-faces.md …
    2e587dbd8 Wed Oct 14 03:18:52 docs(docclass): give devdoc/ledger-readonly-faces.md its class
              banner — the second half of row 01a13646 (index covered the file, the banner was
              still missing, so TestDocClassBannerMatchesManifest was RED on master)
    478728d25 Tue Oct 13 18:30:47 …

    $ git show 2f697e5:devdoc/ledger-readonly-faces.md | head -1
    # Pure-read ledger faces on the read-only opener (task 01a13490)      ← no banner: still RED
    $ git show c06e04e22:devdoc/ledger-readonly-faces.md | head -1
    <!-- doc-class: historical -->                                        ← fixed by then

So that row's base was old AND its surface was still red when it was authored; the banner
landed 13 minutes later, at 03:18:52. Two rows, authored 13 seconds apart, from the same
`base 2f697e5`, with OPPOSITE verdicts on the same sentence shape — which is exactly why
the stamp must say `RE-MEASURE BEFORE REPAIRING` and must not encode a verdict: a
commit-distance reading alone cannot tell these two apart, and only the re-measurement can.


## Why the shipped seam was structurally silent, and what was added

`TaskBriefingPremiseRefresh` (`internal/tools/task_briefing_premise.go`) re-measures the
sentences that **assert** the integration branch does NOT have something, through
`taskreport.UnlandedClaimsIn`'s vocabulary (`"not an ancestor"`, `"never landed"`,
`"absent from"`, …). This briefing asserted nothing of the kind — it *reported a
measurement taken at a revision*: `## Reproduction (measured 2026-10-14, base 2f697e5)`.
A base declaration carries no assertion phrase, so the detector is silent **by
construction**, not by accident. Measured on the real text (CMD5): the sibling half
returned `clause=""`.

The addition is the base half — `taskreport.BaseClaimsIn` (leaf scanner) +
`TaskBriefingPremiseBaseRefresh` (measurement), in the SAME seam, appended after the
sibling clause:

    $ go run ./tmpseamcheck /home/user/work/GoPi \
        ~/.gopi/sessions/01a136e8-6164-7747-bce4-42a877bad4ce/artifacts/briefing-01a136a7.txt
    --- taskreport.BaseClaimsIn (the leaf scanner) ---
      [0] commit=2f697e5 line="## Reproduction (measured 2026-10-14, base 2f697e5)"
    --- tools.TaskBriefingPremiseRefresh (the sibling half, for contrast) ---
      clause=""
    --- tools.TaskBriefingPremiseBaseRefresh (THE CHANGED SURFACE) ---
      record: briefing base → 2f697e5e is 37 commit(s) BEHIND refs/heads/master (exit=0) — STAMPED into the child's briefing (2f697e5e)
      clause:
      [Briefing freshness — the BASE REVISION this briefing names was RE-MEASURED at dispatch]
      - the base this briefing names, `2f697e5e`, is 37 commit(s) BEHIND refs/heads/master
        (`git merge-base --is-ancestor 2f697e5e refs/heads/master` → exit=0;
         `git rev-list --count 2f697e5e..refs/heads/master` → 37).
        RE-MEASURE BEFORE REPAIRING: the surface this briefing calls red was measured at the
        older revision — run THAT surface again HERE, quote its output, and repair it only if
        it still reproduces. If it is already green, report that instead of repairing it.
        This clause is NOT evidence that the work is done.

**What the stamp may NOT say is the load-bearing part.** A base behind the tip is a fact
about the briefing's AGE, and it is NOT evidence the defect is gone — the surface may
still be red for a new reason. So the clause says `RE-MEASURE BEFORE REPAIRING`, names the
surface rather than the verdict, and denies the completion reading in its own words
(`This clause is NOT evidence that the work is done`). Its sibling half's `VERIFY ONLY`
would be the WRONG stamp here: it would tell the child the work is landed on the strength
of a moved base, which is the very defect this epic is about, one level up.

## The predicate, and why it is narrow twice over

The failure this check must never make is stamping an honest briefing (the seam's own
positive control exists for exactly that), so `BaseClaimsIn` fires only where **a base word
and a commit-shaped token stand together on one line**:

- the word must be **standalone** — `codebase 2f697e5` and `database 34693afb3` carry the
  letters but name no base, so a substring match would fire on them (asserted);
- a line with a base word and **no** revision yields no claim at all (not an unmeasured
  one), so a briefing that merely says *"the base case of the recursion"* produces nothing
  (asserted);
- the revision must be within a bounded look-ahead of the word, so a revision discussed a
  sentence later is not attributed to it;
- a base that **is** the integration tip stamps nothing — the positive control.

The revision it names is measured through `master_claim.go`'s ancestry probe and
`repo_sweep.go`'s git runner: **one** git prober and **one** runner, never a second
(Rules 8/13). The package stays a leaf (stdlib only); the constants are named and spelled
once, and the two strings the child and the tests read are exported from the one place
that defines them (Rules 1/13/14).

## The two blind spots, closed (epic row 01a13715, 2026-10-15)

`BaseClaimsIn` shipped narrow in two places where **narrow was a silent hole**, not a
conservatism: a briefing spelling a base the scanner could not read produced the SAME
output as a briefing that declared no base at all — nothing. The seam consuming it
(`tools.TaskBriefingPremiseBaseRefresh`) then had no way to tell "nothing to measure" from
"a measurement I could not take", which is the shape Rule 15 forbids. Both were measured at
the real seam, not argued (`Ran:` block in the task report).

**Blind spot 1 — a CJK base word glued to its revision (`基线2f697e5`).** The word-boundary
guard
(`baseClaimWordBoundary`) rejected the word because the rune after it is a Latin hex digit:
the guard read the base word and its own revision as one longer word. A briefing in this
repo is written in both languages, and 基线 is a standalone word in a way no Latin substring
is — so the guard's Latin rule was applied to a spelling it does not describe. The fix is
`baseClaimGlueWords` + the one exception in the guard: a **non-Latin** base word followed
directly by a rune that can START a revision (`isHexDigit`) is the word and its revision.
It stays narrow by construction — `codebase`/`database` are still refused (the exception is
non-Latin only), and `基线zz2f697e5` is still refused (the glue must be hex).

**Blind spot 2 — an uppercase-hex revision (`2F697E5`).** `implClaimShaShape`'s letter test
was `[a-f]` only, so a revision spelled the way some tools print it carried no "letter",
was refused as if it were a bare count, and the seam went silent. This was a **false
negative of the predicate, not a conservatism**, and it was measured against real git
before the fix:

    git rev-parse --verify --quiet 95F4DE7^{commit}   → 95f4de7123f77344a60c8a195c057a20b99d8eab
    git merge-base --is-ancestor 95F4DE7 master       → exit=0

The fix is ONE helper, `implClaimShaLetter` (both cases), referenced by `implClaimShaShape`
— the class has one definition (Rules 1/13).

**The silence itself, closed.** A token that is revision-**length** but not
revision-**shaped** (`12345678` — no hex letter; `deadbeef` — no digit; an over-long run)
standing beside a base word is now a **`BaseClaimMiss`**: `taskreport.BaseClaimMissesIn`
names the token and the line, and the seam prints one record line per refused declaration
naming the token, the reason and the briefing line. Nothing is stamped — a refused token
names no revision to measure — so the two silences are now distinguishable at the seam:
*a briefing that declares no base emits nothing; a declaration the predicate refuses emits
a line that says so.*

## The residual limitation, stated (not a hole — a decision)

The refusal is stated **on shape alone**, and that is deliberate: `the base of the tree is
12345678 nodes deep` produces a record line even though a human reads it as prose. Judging
"did the author mean a revision" is a guess, and the two costs are asymmetric — an
over-broad record line costs one line an operator skims, while a refusal the seam cannot
see is exactly the defect this epic was filed for. The arm
`TestBriefingBase_ARefusalIsStatedOnShapeEvenForProseShapedLines` pins that choice so it
cannot be quietly narrowed back.

Two arms stay SILENT by design and are pinned: a line with a base word but **no** token
(`the base case of the recursion`), and a refusal that is not bounded as a word on either
side (`this codebase 12345678 grew a database 34693afb3` — the tokens are glued into longer
words, so nothing was "declared" to refuse).

**What is still not closed, honestly:** the CJK glue exception names its words in
`baseClaimGlueWords` (today: `基线`). A base word in a THIRD language, or another
punctuation shape (`基于 2f697e5` is covered by ordinary spacing; `(2f697e5被作为基线` is
not), would be refused — but it would now be **stated** wherever it stands beside a
revision-length token, so the next such shape arrives as a record line rather than as
silence. That is the difference this epic bought.

## Honest limitation (filed as its own row, Rule 41)

The clause fires on ANY named base behind the tip, and a briefing may legitimately name an
older revision as a reproducibility anchor — "the red was seen at `abc1234`" — a shape
where `RE-MEASURE BEFORE REPAIRING` is a reminder rather than a correction. It is a
reminder in the sibling row's case too: measured above, that row's base was old while its
surface was genuinely still red, so re-measuring was the right instruction and the stamp
would have been *harmless but not corrective*.

The clause therefore cannot and does not claim to identify stale work — it identifies a
stale **base declaration** and defers the verdict to the reader, which is the only honest
place for it. Narrowing it further would mean discriminating the motivating shape (base is
the direct parent of a commit that later became the tip) from the anchor shape, and that is
not decidable from the briefing text alone — it needs `git rev-list --first-parent` walking
history around the base at dispatch time, which buys little: the stamp is already
report-only. Left as a measurement, not a guess.

## Repro

```
# the whole story, five commands, ~5 seconds (the artifact is under this run's session dir)
bash ~/.gopi/sessions/01a136e8-6164-7747-bce4-42a877bad4ce/artifacts/five-command-repro.txt  # by content:
git log -1 --format='%h %cd %s' 2f697e5
git merge-base --is-ancestor 2f697e5 34693afb3 ; echo exit=$?
git rev-list --first-parent -1 --before=$(date -d '2026-10-14 03:05:33' +%s) master | xargs git log -1 --format='%h %cd %s'
git merge-base --is-ancestor 34693afb3 c06e04e22 ; echo exit=$?
go run ./tmpseamcheck <repo> <the row's briefing text>
```
