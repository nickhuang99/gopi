<!-- doc-class: historical -->
# The external edition, made publishable — and the checkpoint that keeps it so

Link `[8/9]` (`capdemo-8`, branch `task/capdemo8`, commit `30f655b1`). This is the
record of finishing `docs/archive/demo/external/` so that **every claim in it is an
anchor**, of the one command that checks that (`scripts/capdemo-external-check.sh`),
and of what the check found when it was first pointed at the bundle.

## 1. What was wrong with the bundle at this head

The bundle's own rule is "every claim is an anchor". Nothing enforced it, and the
rule had already been broken in both directions by the time this link started:

| # | the stale claim | what the measurement said |
|---|---|---|
| 1 | `README.md` §2/§6 + `CAPTIONS.md`: gopi **cannot** composite an image — `0 image/draw`, `0` production `png.Decode`, "0 `annotate` rows in `gopi -h`" | false at this head: `gopi annotate` landed at link `[6/9]`; `gopi -h` prints `gopi annotate Draw marks onto an EXISTING PNG (--out).` and `gopi annotate -h` renders a full page. The code references are there (`internal/text2img/annotate.go`, `cmd/agent/cli_annotate.go`). |
| 2 | two hand-composed **SPECIMEN** stills at `05`/`06` | the verb they were specimens *for* exists, so the specimens were evidence of nothing — replaced by the verb's real output at the same two positions |
| 3 | `02` and `04` captions' digests and byte counts | stale the moment the stills were re-shot at this head (`02`: 189723 → 329234 bytes, `9c578f01…` → `dc3691ac…`; `04`: 38557 → 37655 bytes, `0b96899a…` → `fcd6076a…`) |
| 4 | `devdoc/capability-annotation-handoff.md` cited `docs/archive/demo/external/stills/05-specimen-annotated-cjk-render.png` <!-- CAPDEMO-A1-HISTORICAL --> | that file does not exist (renamed by this link) — a dead cross-reference in a tracked doc |
| 5 | every caption's digest was recorded in prose only | nothing could compare a caption with its file, so #3 was invisible |

The one thing that made #3 invisible is the point: **prose cannot be stale-checked**
— which is why the deliverable is a command, not a paragraph.

## 2. The checkpoint — one command, read-only

```bash
bash scripts/capdemo-external-check.sh [path-to-gopi-binary]   # default ./bin/gopi
```

Exit 0 = every anchor resolves; exit 1 = one `STALE …` line per bad anchor, naming
the file, the recorded value and the measured value. It writes **no** file, creates
no scratch directory, starts no process beyond `git`/`sha256sum`/`stat`/`awk`, runs
no gopi *capability* (it reads `gopi -h`, which prints help), and generates no load
(GOPI.md Rule 27). Beyond `git`/`sha256sum`/`stat`/`awk`/`find`/`sed` it needs
nothing, which is the Rule 35 zero-dependency test: it is a file in the repo idiom,
so there is no new artifact to install, run, or remember. The checks, and the claim each anchors:

| id | check | the claim it anchors |
|---|---|---|
| A1 | every `docs/archive/demo/external/**.png` path cited by a **tracked** doc exists | "this still is here" — a renamed still cannot stay cited (§1 row 4). **One reviewed bypass, per line and counted**: a tracked doc that must *name* a still in order to record that it was replaced carries the token `CAPDEMO-A1-HISTORICAL` on that same line; the run then prints `NOTE A1 N citation line(s) clamped as history`. Per-line, never per-file: the file stays under audit, so a stale citation anywhere else in it still fails. The clamp exists because the check was **red at this link's own first commit** — this document's §1 table quotes the dead specimen path in the row that reports it, and a check that forces a record to be deleted is a check that suppresses evidence (see §4c). |
| A2 | every `stills/*.png` has a `## \`<name>\`` heading in `CAPTIONS.md`, and every heading names an existing file | "no unattributed still, no orphan caption" |
| A3 | the digest and byte count recorded beside a heading equal the file's own | "the caption describes *this* file" (§1 row 3) |
| A4 | each heading carries a fenced producing command; its output names the still; its **verb is advertised by `gopi -h`** | "produced by one of gopi's own verbs" (§1 row 1) |
| A5 | every repo-relative input path in a producing command exists | "the command is reproducible from the repository root" |
| A6 | `CAPTIONS.md` pins the commit the stills were produced at, and the token resolves | "all six stills describe ONE commit" |
| A7 | `README.md` names a census **generator** that exists and a commit it was measured at, which resolves and is an ancestor of HEAD | Rule 38: a census figure names its generator and the commit it was measured at |

The vocabulary is fixed and greppable (`produced at commit`, `Measured at commit`,
the pinned-generator path), so a caption that drops its pin **fails** rather than
silently passing.

### 2b. It is also a build gate, and the wrapper is deliberately thin

`internal/standards/capdemo_external_gate_test.go` runs the same script and fails
the package when it is red. It re-implements none of the audit (so the two
verdicts cannot disagree); what it adds is an owner — `go test ./internal/standards/`
exercises the checkpoint instead of waiting for someone to remember a command.

**A finding the wrapper produced, worth keeping:** `go test` **cached** the gate
green on the injected (stale) tree. The gate reads the bundle through a
subprocess, and Go's cache cannot see a file read a subprocess performs — so the
red proof must be run with `-count=1`, and a `(cached)` line is not a measurement
of the tree in front of you. Both proofs in §4 carry the flag and say so.

## 3. Rule 35 — the four questions, answered for this artifact

1. **What breaks without it.** A claim goes stale silently and a reader acts on
   it. Observed, not hypothetical: at the start of this link the bundle asserted
   that `gopi` cannot composite images, four days after the compositor shipped
   (link `[6/9]`), and two captions quoted digests their files no longer had. No
   build, test or check noticed either.
2. **Why it cannot be fixed in-process.** The failure is not a misbehaviour of a
   running gopi process — there is no process: the claims live in Markdown and
   the evidence lives in PNG files, and the only reader is a human (or a
   publisher). The mechanism that can notice is therefore a **check over the
   repository**, and the only place one can live is the repo itself. "Have the
   next link re-measure it" is what was tried for four links and is what failed
   (§1) — it is a promise, not a mechanism.
3. **Who owns its lifecycle, and how it is reclaimed.** Nothing to reclaim: the
   artifact is a **script in the repository** and a **test in the existing
   suite** — no cron line, no daemon, no state file, no lock, no config key, no
   temp path, no new dependency (it uses bash, `git`, `sha256sum`, `stat`,
   `awk`). It runs when a human types it or when the suite runs, and stops when
   it returns. Deleting the two files removes it completely.
4. **What the next person sees.** One script whose header states what it is for,
   why it exists (with the incident), what it reads, and that it writes nothing;
   one test that names the script and fails with the script's own output. A
   reader who finds neither running nor scheduled finds a text file they can read
   in a minute — and if they delete the script, the test fails by name ("restore
   it rather than deleting this test"), so it cannot be removed quietly.

The alternative that was **rejected**: a resident watcher or a scheduled job that
re-runs the check. It would be a new resident dependency (an invisible hand in
the system), it would cover the same failure the suite already covers, and a
stale demo bundle is not an outage — it can wait for the next `go test`.

## 4. Red, then green — the checkpoint actually failing

Both proofs were run in this worktree (the pair below was taken **while the
bundle was still being edited**; §4c re-runs the pair against the *committed*
tree, which is the one that counts). The script-level proof is the important
one; the gate-level proof shows the same failure through `go test`.

**(a) The script** (full transcript in the session's `check-red-proof.txt`).
The clean bundle first, then a **deliberately stale** claim injected into
`stills/CAPTIONS.md` (the pre-re-shoot digest and byte count of `02`, which is
exactly the staleness §1 row 3 describes — the same edit the bundle itself once
carried by accident):

```console
$ bash scripts/capdemo-external-check.sh ./bin/gopi
capdemo-external-check: OK — every anchor in docs/archive/demo/external resolves (0 note(s), 0 stale)
exit=0

# inject: 02's v2 digest + byte count into CAPTIONS.md, via sed

$ bash scripts/capdemo-external-check.sh ./bin/gopi
STALE  A3 02-live-desktop-window.png: caption records sha256 9c578f018388c33fe0627e81d979a0ce5e16a3b1b4254259bfe3c30d8a1eff96 but the file is dc3691ac606f5b26ca627afb8da149b7ee73fc9cb18f968a4e24982e5424ed56 (STALE — re-shoot the caption, or the still)
STALE  A3 02-live-desktop-window.png: caption records 189723 bytes but the file is 329234 bytes

capdemo-external-check: 2 STALE/missing anchor(s) in docs/archive/demo/external — fix each line above, then re-run
exit=1

# restore from the session copy (md5 7ecc8ce9909c47d2365c0979a8dff6dd before and after)

$ bash scripts/capdemo-external-check.sh ./bin/gopi
capdemo-external-check: OK — every anchor in docs/archive/demo/external resolves (0 note(s), 0 stale)
exit=0
```

**(b) The gate** (`internal/standards`, same injection, `-count=1`):

```console
$ go test ./internal/standards/ -count=1 -run TestCapdemoExternalBundleAnchorsResolve
ok  	github.com/nick/gopi/internal/standards	0.786s

# inject the same stale claim

$ go test ./internal/standards/ -count=1 -run TestCapdemoExternalBundleAnchorsResolve
--- FAIL: TestCapdemoExternalBundleAnchorsResolve (0.20s)
    capdemo_external_gate_test.go:73: docs/archive/demo/external has stale or missing anchors …
        STALE  A3 02-live-desktop-window.png: caption records sha256 9c578f01… but the file is dc3691ac…
        STALE  A3 02-live-desktop-window.png: caption records 189723 bytes but the file is 329234 bytes
FAIL	github.com/nick/gopi/internal/standards	0.823s

# restore (md5 identical) and re-run
$ go test ./internal/standards/ -count=1 -run TestCapdemoExternalBundleAnchorsResolve
ok  	github.com/nick/gopi/internal/standards	0.781s
```

**The first, un-injected run of the checkpoint was itself red** — that is the
useful measurement, not a formality. Pointed at the bundle as this link received
it (before any edit), it reported **9** stale anchors: the dead specimen path
(A1), two stills with no caption heading (A2), four digest/byte-count mismatches
(A3), and the two missing commit pins (A6, A7). Those nine lines are the
inventory of §1, produced by the tool rather than by reading.

### 4c. The second red was against this link's OWN commit — and that is the finding

The transcript above was taken while the bundle was still being edited. The proof
that matters is the one taken against a **committed** tree, and it produced a
result the earlier run could not: at this link's first commit the checkpoint was
**still red** —

```console
$ cd <worktree> && git log --oneline -1
<this link's first commit: the bundle, the checkpoint and the gates, all staged>

$ bash scripts/capdemo-external-check.sh ./bin/gopi
STALE  A1 a tracked doc cites a still that does not exist: <bundle>/stills/05-specimen-…-cjk-render.png

capdemo-external-check: 1 STALE/missing anchor(s) in docs/archive/demo/external — fix each line above, then re-run
exit=1
```

*(The bundle path in that quoted line is abridged to `<bundle>/` and the still's
name elided at the point of the report itself: the checker's own output names the
dead path, so quoting it in full would re-create the very citation A1 exists to
catch. The §1 row 4 above is where the path is recorded, with the clamp — and the
unabridged transcript, which is evidence rather than publication, is at
`/home/user/.gopi/sessions/capdemo-8/redgreen-final.txt`.)*

The citation is **this document's own §1 row 4**, which names the dead specimen
path precisely in order to record that it died. So the check was demanding that a
record delete itself: the two available "fixes" were to suppress the finding
(break the path in the prose) or to let the check stay red on a committed tree —
both worse than the defect. The clause added instead is a **per-line, counted**
bypass (`CAPDEMO-A1-HISTORICAL` on the line that names the still), and it is the
shape GOPI.md's reviewed-exception idiom already uses: visible in the artifact,
scoped as narrowly as the claim, and **printed on every run** (`NOTE A1 1
citation line(s) clamped as history …`) so an exemption that grows is a number a
reviewer sees rather than a silence. This is also why the checkpoint is red on a
*committed* tree rather than only on a dirty one: the first version of this link
would have handed over a green-looking bundle whose own report was the one thing
blocking it.

**The final red/green pair, verbatim, at this link's final commit** (the object
name is in the link's report; full transcript kept at
`/home/user/.gopi/sessions/capdemo-8/redgreen-final.txt`):

```console
$ bash scripts/capdemo-external-check.sh ./bin/gopi
NOTE   A1 1 citation line(s) clamped as history by the CAPDEMO-A1-HISTORICAL token

capdemo-external-check: OK — every anchor in docs/archive/demo/external resolves (1 note(s), 0 stale)
exit=0

# inject 02's pre-re-shoot digest + byte count (sed -i)

$ bash scripts/capdemo-external-check.sh ./bin/gopi
STALE  A3 02-live-desktop-window.png: caption records sha256 9c578f018388c33fe0627e81d979a0ce5e16a3b1b4254259bfe3c30d8a1eff96 but the file is dc3691ac606f5b26ca627afb8da149b7ee73fc9cb18f968a4e24982e5424ed56 (STALE — re-shoot the caption, or the still)
STALE  A3 02-live-desktop-window.png: caption records 189723 bytes but the file is 329234 bytes

capdemo-external-check: 2 STALE/missing anchor(s) in docs/archive/demo/external — fix each line above, then re-run
exit=1

# git checkout -- docs/archive/demo/external/stills/CAPTIONS.md   (restore)
# sha256 before and after: f25ba4a4881d5eec3a4342e2d443e2ec206f0c9093bd7cd6f3e2a31421581c51 both sides

$ bash scripts/capdemo-external-check.sh ./bin/gopi
NOTE   A1 1 citation line(s) clamped as history by the CAPDEMO-A1-HISTORICAL token

capdemo-external-check: OK — every anchor in docs/archive/demo/external resolves (1 note(s), 0 stale)
exit=0
```

The **gate** went red and green on the same two edits (`go test
./internal/standards/ -count=1 -run TestCapdemoExternalBundleAnchorsResolve`:
`FAIL … 0.846s` with both STALE lines quoted, `ok … 0.870s` after the restore),
so the build-side half fails exactly when the command does.

## 5. The stills, re-generated at this head

All six were re-produced by gopi's own verbs in this worktree at commit
`30f655b1` (verbatim commands, exit statuses, JSON and pixel measurements in
`stills/CAPTIONS.md`). The digests, before → after:

| still | verb | v2 (link `[5/9]`) | this head | why it moved (or did not) |
|---|---|---|---|---|
| `01` | `gopi shot --self` | `3bdfe2fb…` 33764 B | **identical** | the producer and the page are the same — a byte-identical reproduction is itself the measurement |
| `02` | `gopi capture --window` | `9c578f01…` 189723 B | `dc3691ac…` 329234 B | a **live** subject: the desktop changed between runs. The stable part (1353×740, origin 343,80, `_NET_ACTIVE_WINDOW`, bands 6) is unchanged and is what the caption now says is stable |
| `03` | `gopi text2img` | `e26faca6…` 9470 B | **identical** | deterministic render for a fixed input — which is why `05`'s base does not move |
| `04` | `gopi shot --url` | `0b96899a…` 38557 B | `fcd6076a…` 37655 B | the **page was rebuilt at this head** (v2's lived in the `capdemo-2` session); the caption now quotes the HTML and the encoder, so it is reproducible without the session file |
| `05` | `gopi annotate` | `5a2ecea6…` 18029 B (SPECIMEN, hand-composed) | `232834de…` 10598 B | the verb's real output: same base, same subject, same marks' geometry — measured from the ink bands rather than declared |
| `06` | `gopi annotate` | `b26fd9eb…` 203397 B (SPECIMEN, hand-composed) | `b805e488…` 331104 B | the verb's real output over the re-shot `02` |

The pixel-level claims in the new captions were **measured, not asserted**
(PNG zlib decode + unfiltering in plain Python, no image library — the same
recipe the earlier links used), and two are worth naming here because they are
the natural questions a reader asks of an annotation:

- `05`: 285×120 (the **same size as its input** — `annotate` composites, it never
  pads or adds a band), 4777 pixels changed, **all of them inside** the bounding
  box x 11…271, y 13…111 (mark outlines + the two label chips, which the verb
  placed *below* their marks because there was no room above). Outside that box:
  **0** pixels differ.
- `06`: 1353×740, 11698 pixels changed, all inside x 0…272, y 34…112 (chip above
  the mark, mark outline). Outside: **0**.
- The base stills are not modified by annotating them: `03`'s digest after the
  `05` run is still `e26faca6…`, and `annotate` refuses an `--out` equal to its
  input.

The two marks of `05` are placed on the ink bands **measured** in `03`'s caption
(rows 14…34 x 12…270 and rows 46…65 x 12…181), and the "89 px shorter" label is
that measurement (259 vs 170 px of ink), not an eyeball.

## 6. Not done (honest)

- **A1's clamp is a convention, not a proof.** Any tracked doc could type
  `CAPDEMO-A1-HISTORICAL` on a line citing a path that is merely missing, and the
  check cannot tell a true history record from a false one. It is deliberately
  loud (a counted `NOTE` on every run, whatever the verdict) instead of silent,
  and the alternative was measured worse (§4c): the check was red on this link's
  own commit, and the only ways to green it were to delete a record or to leave
  the checkpoint permanently failing.
- **`02`'s digest will move again on the next re-shoot.** It is a live desktop.
  The check therefore verifies *caption = file*, which is the property that can be
  verified; it cannot verify that a live capture is "the same picture". The
  caption says so in as many words.
- **A5 is a heuristic.** It skeleton-parses the producing command's whitespace-
  separated tokens and skips anything that is a flag, an absolute path (a
  session path, outside the checkout by design), a shell expansion, a quoted
  fragment or an assignment. A repo-relative input passed inside a substitution
  would not be checked. It is a net, not an oracle.
- **A4 checks that the verb is advertised by `gopi -h`, not that the verb wrote
  the bytes.** "The caption's command really produced this file" is verifiable
  only by re-running it, which is a capability execution — deliberately not done
  inside a read-only checkpoint (Rule 27 forbids the load-generating version of
  it, and a demo bundle does not warrant re-rendering on every suite run). The
  captions give the command; re-derivation stays a human act.
- **The census snapshot was taken with this bundle's edits in the worktree**, so
  its `dirty files:` line is not zero. Rule 38 wants the commit a figure was
  measured at; the generator's own identity block names
  `30f655b1` as the base commit, and the report for this link states that the
  figures describe this branch head **with this bundle staged for it**. A later
  link can re-run the generator on the merged tree if it wants a clean-tree
  snapshot; the README says which snapshot it transcribed.
- **`docs/archive/demo/internal/` was not re-measured end to end.** Its stale claims
  were fixed with history banners (the §6.5 "no verb exists" paragraph, the
  §6b "specified, not implemented" paragraph, §12's specimen section, §7.6's
  "closed item" note) and each banner names the commit that superseded it, but
  the internal edition as a whole is link `[3/9]`/`[4/9]`'s document on its own
  base commit, and this link did not re-run its figures. Only the claims this
  bundle contradicts were touched.

## 7. Files

- `docs/archive/demo/external/README.md` — v3: the verb's existence, the refusals, the
  census at this head, the revision note.
- `docs/archive/demo/external/stills/CAPTIONS.md` — v3: six stills, each with verbatim
  command, observed exit, digest, size, pixel measurement, pinned commit.
- `docs/archive/demo/external/stills/0{1..6}-*.png` — re-generated at this head; the two
  specimens removed.
- `scripts/capdemo-external-check.sh` — the publish checkpoint (new).
- `internal/standards/capdemo_external_gate_test.go` — the same check as a build
  gate (new).
- `docs/archive/demo/internal/README.md`, `devdoc/capability-annotation-handoff.md` —
  superseded claims marked, each banner dated and committed.
- `devdoc/DOC-INDEX.tsv` — the row for this document (Rule 36; written by
  `gopi doccheck`, never by hand).

The commit is the one carrying this file (`task/capdemo8`; the bundle, the
checkpoint, its gate and this document land together); the red/green transcript
above is kept at `/home/user/.gopi/sessions/capdemo-8/redgreen-final.txt` (Rule
37: a run's evidence lives in its session directory, not in the checkout). The
commit's object name is stated in the link's report rather than here — a file
cannot carry the hash of the commit that contains it.
