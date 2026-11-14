<!-- doc-class: historical -->
# Verdict: the gate baseline is keyed on site CONTENT — re-expressed on trunk 942b3ac


Task: `01a14330-7642-704a-b18d-2b9625d85ab7` (branch `task/01a143307642704ab18d2b9625d85ab7`)
Re-expresses the intent of branch `task/01a142d6f883779faea37633d9c8728d` (tip `731d43813`)
Ground: `/home/user/workspace/gopi-01a143307642704ab18d2b9625d85ab7`, cut point `942b3ac`.

## The conflict, and why it is a DESIGN conflict and not a merge accident

The merge run reported a single conflicted file, `internal/standards/baseline.go`, one
conflict block. The file is small; the disagreement was not. Each side had written a whole
design for the same question — **what is the identity of a baselined violation?**:

| side | key | consequence of an edit above a baselined site |
|---|---|---|
| trunk (`942b3ac`) | `file:line` | the permit's line no longer holds the site, so the gate reports the SAME defect twice: a NEW violation at the site's new line and a STALE entry at the old one |
| branch (`731d43813`) | `<path>\t<normalized line text>\t#<ordinal>` | the key is unchanged by an insertion; the permit follows the site |

The branch's own record of the defect is `devdoc/standards-baseline-content-key.md`
(blob `041c0e48fb6e04ef952a6b429e5ee7d60d754e71`); it cites two instances measured in one
day — `internal/docclass/anchor_number_material_test.go` `:155`→`:166` and
`internal/tools/git_child_locale_test.go` `:243`→`:248`. Trunk separately recorded and
**patched** the same second instance (`internal/standards/baseline.go`, the RE-MEASURED
AGAIN comment at `942b3ac`: 73 entries → 72, the entry deleted, the site repaired with
`testutil.GitEnv()`). Two independent runs hit the same rot on the same day and each chose a
different remedy: one repaired the instance, the other replaced the key.

## Measurement — the branch is NOT already-merged, and trunk did NOT adopt the key

Both readings were taken in this ground against explicit revisions.

```
$ git rev-parse refs/remotes/gitPushcheck/master refs/heads/master
942b3ac6ad27dd551080e726896d227875565e59      <- the cut point (peer master)
9adad635b9a66ef51783c809436803a8a06b4efa      <- stale local master ref in this tree

$ git ls-tree -r --name-only refs/remotes/gitPushcheck/master | grep -c sitekey
0
$ git show refs/remotes/gitPushcheck/master:internal/standards/gate.go | grep -c sitekey
0
```

So the content-key mechanism exists on **no trunk revision reachable here** — the branch's
value is real and it cannot be discarded as `already-merged`. The conflicting blob:

```
$ git rev-parse 942b3ac:internal/standards/baseline.go
0946500b81331e30c8a1d76bb144a2b06f117969      <- line-keyed (trunk)
$ git rev-parse 731d43813:internal/standards/baseline.go
ea06cc57124a17cabca6021cdb13853b24d476ca      <- content-keyed (branch)
```

The two sides forked at `01c1674121dc089ada8e1d0292a5f2a1cb90a3e5`. Between that fork point
and `942b3ac`, exactly ONE of the four files the branch rewrote moved:

```
$ git diff --stat 01c167412..942b3ac -- internal/standards/gate.go \
      internal/standards/gate_test.go internal/standards/baseline.go \
      internal/standards/screen_assertion_gate_test.go
 internal/standards/baseline.go | 16 +++++++++++++++-
 1 file changed, 15 insertions(+), 1 deletion(-)
```

That single hunk is the trunk-side per-instance repair described above. It is a **stale
permit deletion plus a provenance comment**, i.e. exactly the class of edit the content key
makes unnecessary — but it is also a real ratchet DOWN (73 → 72 entries) that must not be
lost. This is what decided the route below.

## Verdict

**The content key wins, and it is re-expressed on `942b3ac` rather than replayed.**

Reasons, in the order they decided it:

1. **The defect is the key, not the instance.** Trunk repaired `:243`→ its site and the
   branch repaired a different instance the same day. Per-instance repair is whack-a-mole,
   and both records are in this repository: the trunk comment in `baseline.go` and the two
   instances in the branch's devdoc. A repaired instance leaves the mechanism intact.
2. **Nothing is relaxed by re-keying.** `enforce()` keeps BOTH arms — a hit matching no
   entry is still a NEW violation, an entry matching no hit is still STALE — so the change
   is a re-keying of the same accounting, not an exemption. The entry count is unchanged
   (`136` entries before, `136` after; per gate `72 gitEnvBaseline` / `62 testEnvBaseline` /
   `2 errorWrapBaseline` at `942b3ac`), which is the numeric form of "no permit was added".
3. **The trunk-side repair survives, it is not reverted.** `942b3ac`'s baseline.go is the
   input to the re-key generator: the deleted `git_child_locale_test.go` entry stays deleted
   because the generator reads what is in the file, never what the branch used to say. This
   is why the branch's blob is *not* copied — see the next section.
4. **The alternative (line key wins) buys nothing.** Its only argument is "the key is
   already written down", and by the same evidence it costs one defect-report pair per edit
   above a baselined site, which is what the two measured instances cost.

## What was re-expressed, and what was NOT copied

The generator is the arbiter, never the branch's list. `942b3ac`'s `baseline.go` was left
**as is** and handed to the generator; every entry was re-read out of the current tree:

```
$ go run internal/standards/testdata/baseline-rekey.go        # rewrite
2 file(s) carried baselines; 136 entry line(s) changed.

$ go run internal/standards/testdata/baseline-rekey.go -check  # idempotent, report only
2 file(s) carried baselines; 0 entry line(s) WOULD CHANGE.
```

`git diff --stat` for the re-key is `136 insertions(+), 136 deletions(-)` — one converted
line per entry, zero added, zero removed. Had the branch's `baseline.go` been copied in, the
trunk repair would have been silently reverted and the count would have drifted.

The blob identities make the difference measurable rather than argued:

```
$ git rev-parse 942b3ac:internal/standards/baseline.go     # trunk, line-keyed
0946500b81331e30c8a1d76bb144a2b06f117969
$ git rev-parse 731d43813:internal/standards/baseline.go   # branch, content-keyed at the fork
ea06cc57124a17cabca6021cdb13853b24d476ca
$ git hash-object internal/standards/baseline.go           # THIS tree, re-keyed on trunk's truth
2b195beea4604ec51b2113f1f5f7ae493cbeab06
```

The third hash is deliberately NOT the second: it is `0946500b…` re-keyed, so the trunk
repair it carries survives. The `-check` run above is the proof that the result is stable
(zero deltas), and the equal entry counts are the proof that nothing was excused.

Files carried over from the branch **unchanged** (blob equality with `731d43813`, which is
the strongest available statement that the mechanism is the branch's own design and not a
re-invention):

```
$ git hash-object internal/sitekey/sitekey.go
9ade54836962e14bed454f5bf1e96905e00b9db6   = 731d43813:internal/sitekey/sitekey.go
$ git hash-object internal/standards/gate.go
534077b302c3b80b50ae3258eb1021b7b2ddb0d4   = 731d43813:internal/standards/gate.go
```

Carried unchanged: `internal/sitekey/sitekey.go` (the ONE key definition),
`internal/standards/gate.go` (digest stamping + both ratchet arms + `staleDetail`),
`gate_test.go`, `baseline_key_test.go` (216 lines of key/ordinal/locate proofs),
`internal/standards/testdata/baseline-rekey.go` (the generator, `sitekey.KeyCreatedBy`),
`screen_assertion_gate_test.go`.

**Not carried**: the branch's `baseline.go` blob and its four devdoc files
(`standards-baseline-rekey-20260916.md`, `standards-baseline-rekey-2026-09-16.md`,
`merger-conflict-resolution-baseline-rekey.md`, `screen-assertion-baseline-rekey.md`).
Three of them are a past run's record and one is the design essay this document supersedes
on the current trunk; landing them as well would put four documents on a trunk that did not
ask for them.

## The key, stated once so it is not re-derived

```
<path> \t <normalized line text> \t #<ordinal>
```

`Normalize` collapses each whitespace run to one space and trims the ends, so indentation
and gofmt column alignment are not part of the identity. The ordinal is the 1-based position
of the line among the lines of the SAME file whose normalized text is identical — two
identical lines therefore get two different keys, so one permit can never silently cover
both. The line number is deliberately absent.

Two honest limits, both on the far side of the change and both stated by the code rather
than discovered later:

- **A hit with no key is reported, never dropped.** `sitekey.Key` returns an error for a
  line outside the file and for a line that normalizes to empty; `enforce()` collects those
  and fails the gate by name, because a permit that names nothing must not become a silence.
- **A stale entry says which of three things happened.** `staleDetail` distinguishes "the
  text is gone" (delete the permit), "the text is still there at another line, ordinal
  changed" and "the predicate changed, not the tree". A bare entry string could not.

## The generator is the only writer (Rule 38 / Rule 1)

`sitekey.KeyCreatedBy = "go run internal/standards/testdata/baseline-rekey.go"` is the
single spelling of the command, and the STALE-entry error message renders it — so the gate
that reports a rotted key also tells the reader how keys are minted. A hand-written key is
never needed and is not accepted by the generator; a key whose text is gone is a hard error
there, because that is a repaired site whose permit a human must delete.

## Check

`.gopi`-side gates for this decision:

```
go test ./internal/sitekey/ ./internal/standards/ -count=1
go run internal/standards/testdata/baseline-rekey.go -check   # exit 0 = no delta
```
