<!-- doc-class: normative -->
# The capability demo — external edition

Class: normative. Why: it is the standing description of what
`docs/archive/demo/external/` delivers, which claims each file carries, and which claims
are deliberately absent — a reader who finds the bundle can verify every one of
them from this document.

Chain link `[2/9]` (row `capdemo-2`, branch `task/capdemo2`). Producer: link
`[1/9]`, the census instrument (`devdoc/capability-census-instrument.md`,
`scripts/gopi-stats.sh` `== capability census ==`). The internal rendition is NOT
built here: mechanism findings, defects, the adoption table and the
annotation-gap finding belong to the internal link, whose raw material is
`devdoc/capability-demo-decomposition.md`.

**Files this document is about** (every one of them a live path at the commit
this document lands on — the classification below is checkable, not asserted):
`scripts/gopi-stats.sh` (the census section and the read-order fix),
`internal/shot/chrome.go` and `cmd/agent/cli_voice.go` (the two definition sites
the census reads its names from), and `docs/archive/demo/external/` (the bundle).

## The bundle, file by file

| Path | What it is | Anchors it carries |
|---|---|---|
| `docs/archive/demo/external/README.md` | the structured outline an outsider reads: 7 sections, one line each | every section names its anchor and its own omission |
| `docs/archive/demo/external/stills/01-gopi-self-webui.png` | gopi's own web chat UI | `gopi shot --self` (verbatim in the captions file) |
| `docs/archive/demo/external/stills/02-live-desktop-window.png` | the live X11 desktop's active window | `gopi capture --window` |
| `docs/archive/demo/external/stills/03-text2img-cjk.png` | CJK rendered by the pure-Go rasterizer | `gopi text2img` |
| `docs/archive/demo/external/stills/04-gopi-shot-url.png` | an arbitrary URL rendered by the same headless path | `gopi shot --url` |
| `docs/archive/demo/external/stills/CAPTIONS.md` | provenance: the verbatim command, exit status, PNG facts and the privacy note, per still | the bundle's Rule-31 evidence |

Four stills, not the required three minimum: `shot` was driven in both of its
shapes (`--self`, `--url`) because the two prove different things — the embedded
UI renders, and an arbitrary page renders without any served file.

### The bundle contract (a criterion, not a list)

The bundle's standing rules, and they are the reason a later link can check it
rather than trust it:

- Every capability claim in the bundle **MUST** resolve to one of exactly three
  anchor kinds: a still with a verbatim producing command, a census figure with
  its generator and base commit, or a report path. A claim resolving to none is
  not in the bundle.
- `scripts/gopi-stats.sh` is the **single source of truth** for every figure the
  bundle quotes; no number is retyped from a previous run (Rule 38).
- Every committed still **MUST** carry its verbatim producing command in
  `stills/CAPTIONS.md`; a PNG without provenance is not a deliverable. **Enforced
  by** review at link `[8/9]`, the chain's publish checkpoint, against this list.
- The bundle is static and zero-dependency: Markdown and PNG only, no server, no
  build step, no JavaScript.

Static and zero-dependency, in the strict sense this repository uses it: the
bundle is Markdown and PNG. No server, no build step, no JavaScript, no external
tooling to *read* it. `git clone` and an image viewer are the whole requirement.

## The claim → anchor map

Every capability claim in the outline resolves to one of exactly three anchor
kinds. A claim that resolves to none is not in the bundle.

**(1) A still, with its verbatim producing command.** The commands, exactly as
run (full entries with exit status and digests in `stills/CAPTIONS.md`):

```bash
gopi shot --self --out docs/archive/demo/external/stills/01-gopi-self-webui.png
gopi capture --window --out docs/archive/demo/external/stills/02-live-desktop-window.png
gopi text2img /home/user/.gopi/sessions/capdemo-2/still-03-sample.txt docs/archive/demo/external/stills/03-text2img-cjk.png
gopi shot --url "$(cat /home/user/.gopi/sessions/capdemo-2/card.enc)" --out docs/archive/demo/external/stills/04-gopi-shot-url.png
```

The four PNGs' digests, read back from the committed files with
`sha256sum docs/archive/demo/external/stills/*.png`:

```
3bdfe2fb3e8e4c81c173ef40af66aac03ea257f58230400df1ca22cec230a70b  01-gopi-self-webui.png
9c578f018388c33fe0627e81d979a0ce5e16a3b1b4254259bfe3c30d8a1eff96  02-live-desktop-window.png
e26faca6290b78db6b918046c3766e1fe2dd7bc8479ca892bfba95a22a51d185  03-text2img-cjk.png
0b96899a66f8217131bb6cb4542c1d448629fdb4cf2046cc54dedb4bdb99acd3  04-gopi-shot-url.png
```

**(2) A census figure, with its generator and its base commit — never retyped.**
Generator, verbatim, from the repository root:

```bash
bash scripts/gopi-stats.sh
```

Base commit measured for every figure quoted below:
**`00cb4ec763d0af69d5a06ac9951bcbeda038ccda`** (the merge commit this branch was
on when the runs were made; the script prints its own `== identity ==` block, so
the claim is re-derivable rather than trusted). The census section was extracted
with the same `sed` range the instrument's own document uses, and its raw output
is kept at
`/home/user/.gopi/sessions/capdemo-2/census-v2-section1.txt` (its identity block
reads `dirty files: 2` — this link's fix under test, in the worktree and not yet
committed; the census reads its sources with `git show HEAD:` / `git grep HEAD`,
so **every figure that comes from a commit-pinned source is attributable to
`00cb4ec7`**). The `(b) the eyes` rows are the one exception and they are
labelled as such below: their content was produced by the *fixed* script, which
this same commit lands. A reader re-running the generator at the deliverable
commit should expect the three corpus/doc counts to move by this link's own three
committed Markdown files and every other row to be identical.

| Figure (as the census prints it) | Which section prints it |
|---|---|
| `576 production files / 268142 LOC / 7004 functions in 66 packages` | `== summary (for a reader meeting the project) ==` (`size` row) — NOT the census section |
| `1247 test files / 378826 LOC`, `22 standards gate files` | same block, `verification` row |
| `2197 markdown files / 429834 LOC` | same block, `documentation` row |
| `29 LLM tool names, 34 CLI verbs, 17 task subcommands, 90 env vars` | same block, `surface` row |
| `one Go binary, zero CGO, 5 direct deps (28 pinned in go.mod)` | same block, `what it is` / `direct deps` rows |
| `fresh build size: 28617953 bytes` | `== binary ==` |
| `dispatch predicates: 39`, `verbs declared: 35`, `renders=34 fallthrough-to-toplevel=0 refused-by-design=1 empty=0` | `== capability census ==` → `(a) help-surface reachability` |
| `LLM tools with a schema entry: 17` | `== capability census ==` → `(a)` |
| `11` eyes (verbs declaring a precondition), `present here: DISPLAY=… chrome=… tty=no` | `== capability census ==` → `(b) the eyes` — **produced by the fixed script** (see "The defects this link found and fixed" below): the `chrome` channel was silently missing before the fix |
| `markdown files carrying non-ASCII: 2182 of 2197 of which CJK-lead: 717`, `faces present on this host: 7 of 11 candidates`, `render probe: … rc=0, png=1056 bytes` | `== capability census ==` → `(c) the non-ASCII / CJK axis` |
| `merger rc=1`, `task serve rc=0` (deprecated alias), `task merger rc=0` (deprecated alias) | `== capability census ==` → `(d) deferred / degraded capabilities` |

**Read this table as scope, not as formality**: the first six rows come from the
snapshot's **summary block**, not from the capability census. They are printed by
the same generator in the same run, but they belong to the section that was
already there before this chain extended the instrument, and the census's own
contract (`devdoc/capability-census-instrument.md`) is about the `== capability
census ==` section alone. An external claim that cited the census for the
`production files` figure would be attributing it to a section that does not
print it — the exact failure Rule 38 exists to catch, one level down.

**(3) A report path.** The chain's own records: the producer's
`/home/user/.gopi/sessions/capdemo-1/task-report.md` and this link's
`/home/user/.gopi/sessions/capdemo-2/task-report.md`.

## What each section deliberately omits, and why

The omission is a claim of the same standing as the claim: the outline's seven
sections each carry theirs, and they are collected here so a later link can
change them in one place.

| Section | Deliberately omits | Why |
|---|---|---|
| 1 One binary | performance, model quality, cost | none of it was measured here; a number nobody measured is exactly what Rule 38 exists to stop |
| 2 The eyes | glyph **fidelity** (IoU / pixel-difference) for rendered text | the census's render probe measures exit status, byte count and composition — its own `Not Done` list says accuracy is a different instrument |
| 3 The surface is derived | per-verb flag inventories, per-tool schema prose | the instrument measures what is **printed** and **declared** at a commit; it never runs a capability to see whether it works (Rule 27) |
| 4 The eyes are conditional | "the call succeeded" | a channel being present is not a successful call — `socket`/`network` are printed `declared` because a listener or an upstream URL is not a precondition that can be true *before* the call |
| 5 CJK | "CJK is supported everywhere" | the census counts corpus files and renders one fixed 60-char sample; that measures the renderer and the corpus, not every string in every locale |
| 6 What refuses on purpose | a roadmap of re-implementation | why a capability is retired is a decision with its own record; the bundle reports state, not intent |
| 7 Anchors | nothing | it is the section that makes the other six permissible |
| the bundle as a whole | mechanism findings, defects, tool adoption | they are the internal rendition's job, and they are *not* retracted — this is a selection from them, and link `[8/9]` is the checkpoint that keeps the selection honest |

Two omissions are worth calling out because they are the ones a reader is most
likely to assume away: **adoption** (the census counts the 17 schema entries the
model reads; whether the model calls a tool is a different measurement, made by a
different section — so nothing here says "the agent uses X"), and **the live
desktop still is a capture of a real desktop**, including the operator's own
prompt line. That still carries a privacy note in its caption rather than being
quietly prettified.

## The defects this link found and fixed — the census's eyes block

Running the producer's instrument at its own commit exposed **three** defects, all
in the same section and all of one class: *a wrong verdict that reads exactly
like a correct one*. None of them fails anything; every line still renders, the
section is still stable across two runs, and the only witnesses are lines on
stderr that nothing in the demo reads. They are recorded here because the
external edition selects from what the instrument measures, so an instrument
defect is a demo defect.

**1. Read order under `set -u` — three constants used before declaration.**

*Symptom*, on stderr at the commit that introduced the section:

```
scripts/gopi-stats.sh: line 357: VOICE_SRC: unbound variable
scripts/gopi-stats.sh: line 358: CHROME_ENV_SRC: unbound variable
scripts/gopi-stats.sh: line 358: CHROME_ENV_CONST_ERE: unbound variable
```

*Mechanism*: the script runs under `set -uo pipefail` (line 24); the three
constants were declared in the block that also exports `GOTMPDIR`, which sits
**after** the eyes code that reads them. A shell reads top to bottom, and under
`set -u` a use-before-declaration is a hard failure rather than an empty value.

*Fix*: the three declarations moved above the eyes section, with a comment at
the declaration site naming the failure mode. Values and reads unchanged; both
are commit-pinned reads from their definition sites (Rule 13/14), which is the
property the moved block exists for.

**2. `"${!name}"` is fatal when the named variable is unset — so chrome was
always `absent`.**

*Mechanism*: `census_chrome_bin` resolves the browser through the same chain the
`shot` tool uses (env override → playwright cache → PATH), and its first step was
`envval="${!envname}"`. Under `set -u` an *indirect* expansion of an **unset**
variable is a fatal error — and "the operator did not set the override" is the
ordinary case, not an error. The whole resolver died there, so the cache and PATH
fallbacks never ran.

*Consequence*: `chrome=absent` on a host that has chrome, for **every** verb that
declares the channel — `chat`, `scratch-gc`, `shot` — while `shot` had just
produced four PNGs on that same host with that same chrome. The instrument's own
`present here:` line printed `chrome=` (empty) beside `chrome=absent` rows.

*Fix*: `envval="${!envname-}"` — the `-` default is what turns the unset case
into an empty value instead of a fatal expansion.

**3. Six unguarded `git show` calls per verb — a failed read silently deleted a
channel.**

*Mechanism*: each channel was tested with its own
`git show "HEAD:$f" 2>/dev/null | grep -qE …`. A failed `git show` (stderr
discarded) produced an empty body, `grep -q` failed, and the channel was dropped
from the row with nothing said.

*Measured, before the fix*: `census-fixed-determinism.diff` — **one line**, two
consecutive runs of the same script at the same commit, `chat` printing `X11` in
one run and `X11 chrome` in the other. That is also why this document does not
repeat link 1's byte-identical determinism claim without qualification: the claim
was measured on a script whose eyes block could silently disagree with itself.

*Fix*: one read per verb, stderr no longer discarded, and an unreadable body
prints `(unreadable at HEAD: <file> — verb skipped)` and skips the verb instead
of reporting a channel set that was not measured (Rule 15).

**The fix, measured.** Two consecutive runs after it, census sections extracted
with the instrument's own `sed` range:

```
$ diff census-v2-section1.txt census-v2-section2.txt      # prints nothing (0 lines)
$ sha256sum census-v2-section1.txt census-v2-section2.txt
0b6836f5a920a759b452cae51dd0da875f6980f6400e2f5a38e3031c8056c0a3  census-v2-section1.txt
0b6836f5a920a759b452cae51dd0da875f6980f6400e2f5a38e3031c8056c0a3  census-v2-section2.txt
$ grep -n 'present here' census-v2-section1.txt
75:  present here: DISPLAY=localhost:15.0  chrome=/usr/bin/chromium-browser  tty=no  stdout_isatty=no
```

The `chrome` channel is back on all three verbs that declare it
(`chat`, `scratch-gc`, `shot`), and the section is byte-identical across runs —
this time on a script whose eyes block has one read per verb.

**Why a later link should care.** The external edition's job is to *select* from
measured findings, and the selection is only as good as the instrument. This is
the same class the chain's own decomposition predicted, found in the first place
a reader would look: the numbers the demo quotes.

**Why a later link should care.** This is the same class the chain's own
decomposition predicted: the external edition's job is to *select* from measured
findings, and the selection is only as good as the instrument. The internal
rendition carries this as a finding, with the run that produced it.

## Verification of this link's own deliverable

- **The three-difference check against the producer's run** (this is the honest
  form of "the instrument still agrees with itself"): the census section at
  `00cb4ec7` was `diff`ed against link 1's recorded section
  (`/home/user/.gopi/sessions/capdemo-1/census-extended-run1.txt`, extracted with
  the identical `sed` range). Three lines differ:
  1. the `verbs unresolved:` line — the producer's run was taken against an
     **intermediate draft** of the script (its own identity block reads
     `base commit: a86eab72 … dirty files: 1`), so the wording changed before the
     commit; my run is at a commit;
  2. the `chat` row — the read-order defect above (their recorded run has the
     channel; the committed script did not print it);
  3. the three corpus counts (`2180/2195/715` → `2182/2197/717`) — the markdown
     corpus grew by exactly the two documents committed between those commits.
     This is the normal drift Rule 38 demands a base commit for, not a
     disagreement.
- **Determinism under the fix**: two consecutive runs, census sections `diff`ed.
- **Still provenance**: every PNG's command is in `stills/CAPTIONS.md`, and the
  digests there were read back from the committed files with
  `sha256sum docs/archive/demo/external/stills/*.png`.
- **Rule 36**: `gopi doccheck` exits 0 with this document classified in the same
  commit that lands it.
- **Rule 31**: every surface this link changed was executed — the census
  (`bash scripts/gopi-stats.sh`, twice), and all four still verbs run for real
  with real arguments. The invocations and their observed results are in the
  report's `## Verification` section and in the captions file.

## What this link did NOT do

- **No internal rendition.** By construction (chain link `[3/9]` and later).
- **No new tool, verb, or dependency** (Rule 35): the bundle's only new artifacts
  are Markdown, PNG and one moved-line fix inside an existing script.
- **No annotation capability.** Nothing in gopi composites one image onto another
  — the decomposition measured that gap; building it is links `[4/9]`–`[6/9]`.
- **No pixel-fidelity measurement** of the CJK still. The ink count in the
  captions is a decode-and-count sanity check (standard library only), not an
  IoU acceptance, and it is labelled as such.
- **No publication.** The bundle is in-repo and static; publishing it, and
  re-shooting the live-desktop still if it ever leaves the repository, is the
  caller's decision.
