<!-- doc-class: normative -->
# Capability census instrument — the numbers the demo reads

Class: normative. Why: this is the instrument's own contract; the demo's
capability claims are quoted from its output, so the rules for quoting them
live here.

## What this is

`scripts/gopi-stats.sh` is the project's reproducible statistics snapshot: it
measures its numbers from the repository instead of trusting numbers typed into
prose. This document covers the ONE section added on branch `task/capdemo1`
(`== capability census ==`), which gives the capability demo its facts.

**Generator, verbatim** (run from the repository root):

```bash
bash scripts/gopi-stats.sh
```

**Base commit measured: `757b5adf`** (the commit that carries this document
alongside the census), worktree clean (`git status --porcelain` empty), branch
`task/capdemo1`. The script prints its own base commit, branch and dirty-file
count in its `== identity ==` section, so a reader never has to trust this
paragraph: run it and read the first four lines.

**Determinism** — the property that makes it an instrument rather than a
rumour: two consecutive runs at that commit produce a byte-identical census
section.

```
$ bash scripts/gopi-stats.sh > run1.txt ; bash scripts/gopi-stats.sh > run2.txt
$ sed -n '/^== capability census ==$/,/^== binary ==$/p' run1.txt > census-run1.txt   # (same for run2)
$ diff census-run1.txt census-run2.txt        # prints nothing
$ sha256sum census-run1.txt census-run2.txt
055f804b…  census-run1.txt   # identical digests (see "Evidence" below)
055f804b…  census-run2.txt
```

HONEST SCOPE of that claim, because the whole transcript is NOT deterministic:
the two full runs differ in **8 lines**, all of them live-corpus counters whose
inputs change *while the runs happen* — the `== sessions & tool adoption ==`
section counts the newest 400 session logs, and this very session appends to
one of them between the two runs (`tool calls: 18486 → 18487`). The census
section itself reads only commit-pinned sources (`git show HEAD:…`, `git grep …
HEAD`) and host facts, so it is byte-identical. The snapshot's other
relative-time rows (task cadence over the last 7/30 days) have the same
character. A census figure that named one of those would be a rumour with a
commit attached, which is why the census prints none of them.

## The four dimensions, and why each is derived rather than listed

### (a) Help-surface reachability — does every verb answer its own page?

WHY derived: a hand-kept list of verbs goes stale the moment a verb is added,
and nobody notices. So the **set** comes from `cmd/agent/main.go`'s dispatch
chain at HEAD and each **word** from the constant its predicate compares
`argv[0]` against. Three body shapes are read, because the mapping is not
name-derived: `args[0] == CliVerbX`, `switch args[0] { case CliVerbX: … }`, and
one helper hop (`isBrewSubcommand` → `brewSubcommand(args)`). A constant that is
an ALIAS to another package's (`CliVerbMemory = tools.CliVerbMemoryGroup`) is
resolved one hop further.

WHY the verdict is a word and not a byte count: the probe reproduces `cmd/agent/main.go`'s
own order — an `-h` query is answered **before** dispatch (P0-1), so a word whose
help prints the TOP-LEVEL page is not a command at all; it is prompt text the
query path intercepted. That verdict is `fallthrough-to-toplevel`, and it exists
so the census cannot report "help works" for a word that is not a command.

```
dispatch predicates: 39  (source: git show HEAD:cmd/agent/main.go | grep -oE 'is[A-Za-z]*Subcommand\(dispatchArgs\)')
verbs declared:     35  (word resolved from the const the predicate compares argv[0] against)
verbs unresolved:   (none)
two-word verbs:     (none)
refusal-path predicates (not commands): refusal-path(isBrewSubcommand) refusal-path(isUndispatchedToolWordSubcommand)
verb -h verdicts:   renders=34 fallthrough-to-toplevel=0 refused-by-design=1 empty=0
LLM tools with a schema entry: 17
```

`34 + 1 = 35`: every declared verb either answers its own page or is in the
deferred list of (d). The 17 tools are the tools among the 29 names declared in
`internal/toolnames/names.go` — a tool has no CLI page, so the reachable surface
measured for it is the SCHEMA entry the model reads
(`git show HEAD:internal/agent/loop.go | grep -cE 'Name:[[:space:]]+toolnames\.Tool[A-Za-z]+'`).

HONEST PART: the census counts a tool's schema entry; it does NOT measure
whether the model calls the tool (that is a runtime adoption question, and the
snapshot's `== sessions & tool adoption ==` section measures it from session
logs by a different route). The group sub-verbs are probed from a **declared
probe set** (`SUBVERB_PROBES`); a sub-verb outside it is not measured and is not
claimed.

### (b) The eyes — which preconditions, and which of them hold HERE

WHY a verdict per channel: "the eyes exist" and "the eyes work on this machine"
are two different claims, and a demo that shows the second while quoting the
first is the classic capability lie. So each verb that names a precondition gets
its channels printed, and each channel is then measured **on this host**.

```
  verb            precondition channel (from its own file)   channel present on THIS host
  act             X11                                        X11=present
  capture         X11                                        X11=present
  chat            X11 chrome                                 X11=present chrome=present
  cli             socket network                             socket=declared network=declared
  proxy           socket network                             socket=declared network=declared
  scratch-gc      X11 chrome                                 X11=present chrome=present
  screen_capture  X11                                        X11=present
  shot            X11 chrome network                         X11=present chrome=present network=declared
  ui              socket network                             socket=declared network=declared
  vision_analyze  network                                    network=declared
  voice_input     model-file network                         model-file=present network=declared
  web_fetch       network                                    network=declared
  present here: DISPLAY=localhost:15.0  chrome=/usr/bin/chromium-browser  tty=no  stdout_isatty=no
```

The **channel test is textual** (the verb's own file names the channel), which
is a deliberate limit: it answers "what does this verb need", never "did the
call succeed". The **host test** is real: `X11` = `$DISPLAY` set, `chrome` = the
same resolution chain the shot tool uses (env override → playwright cache →
PATH), `TTY` = stdin is a tty, `model-file` = the voice model root from
`voiceDefaultModelRoot` (read from its definition site) exists. `socket` and
`network` print `declared` rather than `present`, because a listener or an
upstream URL is not a precondition that can be true or false before the call —
printing `present` there would be an invented verdict.

The two `tty=no` readings matter for reading (a) honestly: this census ran with
no controlling terminal for stdin AND no tty on stdout, and the help pages still
rendered — the CLI answers usage queries without a terminal.

### (c) The non-ASCII / CJK axis of the renderer

```
classifier self-test: ok (cjk-class: CJK-char=1 ascii-char=0, want 1/0)
markdown files carrying non-ASCII: 2181 of 2196   of which CJK-lead: 716
go files carrying non-ASCII:       1821   of which CJK-lead: 280
top-level help lines carrying non-ASCII: 14
renderer candidate faces (from internal/text2img/render.go): 11
  truetype/dejavu/DejaVuSans.ttf … truetype/wqy/wqy-zenhei.ttc
renderer search roots (read from fontDirs() in internal/text2img/render.go): 2  (/usr/local/share/fonts /usr/share/fonts )
faces present on this host: 7 of 11 candidates
  render probe: text2img census-cjk-sample.txt (60 CJK chars) -> rc=0, png=1056 bytes
  render summary: ✅ … (21 字符, 2 行, 字号 20px, 行高 32, 折行 50, 墨迹 4160 像素, 字体链 …)
  summary names measured ink: yes   names the font chain: yes
```

The corpus counts are SELF-REFERENTIAL, and that is worth knowing before
quoting them: the two counts include this document, so a doc that quotes "2181
markdown files carry non-ASCII" changes that number by adding itself. The figure
is still a census figure (re-runnable, commit-pinned); it is a census of a
corpus that the census's own readership is part of. Quote it with its commit,
never as a constant.

WHY the classifier carries its own self-test: the census classifies CJK with a
**raw-byte** class spanning 0xE4..0xE9 (the lead bytes of 3-byte UTF-8
sequences). The first spelling used in this session was built by passing hex
through `printf`'s `%s`, which emits a LITERAL `\x` — the class then read as
`{x,e,3..9}` and matched **2195 of 2195** markdown files while failing the CJK
control. The self-test (one CJK char must match, one ASCII char must not) is
printed on every run, and a failure WITHHOLDS the two corpus counts rather than
printing a number with nothing behind it (Rule 15). The escape must therefore
live in `printf`'s FORMAT string:
`CJK_LEAD_BYTE_ERE=$(printf '[\xE4-\xE9]')`.

The candidate faces and the search roots are read from `render.go`: the face
names from the four candidate lists, the roots from `fontDirs()`'s **own body**
(the `return []string{…}` block), so a path is never re-typed here. The face
lines are HOST facts (whether the file exists on this machine), the roots are
SOURCE facts (the fixed directories the renderer searches).

The render probe is the smallest meaningful sample (one repeated CJK character,
60 chars) and its summary is read through the producer's OWN markers
(`墨迹` = measured ink, `字体链` = font chain), which is what makes a "✅ over a
blank page" impossible to hide. It measures **rc, PNG byte count and summary
composition** — pixel-level truth (IoU against a reference face) is a different
instrument with a different acceptance, and this census does not claim it.

### (d) The deferred / degraded list — the verbs that refuse ON PURPOSE

```
  merger          rc=1  merger: retired — the resident merger daemon w…
  task serve      rc=0  gopi task serve is a deprecated alias — the me…
  task merger     rc=0  gopi task merger is a deprecated alias — the m…
```

WHY the classification is by CONTENT first: a retired verb can answer its
refusal with **exit 0** (`gopi task serve -h` prints "is a deprecated alias …"
and exits 0, measured), so a status-only rule would file two dead verbs as
`renders`. `census_help_verdict` therefore checks the refusal's own words first,
then the status, then emptiness, then the top-level-head fallthrough. `merger`
(exit 1) is the resident merger daemon's retirement; `task serve` and
`task merger` are the aliases that retirement closed. Both spellings come from
`DEFERRED_PROBES`, a declared probe set.

This list is why the demo needs it: a surface that does NOT answer belongs in
the census. Without it, a demo shows a capability nobody can reach.

## The instrument's contract

- The census MUST stay re-runnable with no manual steps: one command, from the
  repository root, at any commit.
- Its figures MUST be deterministic for a fixed commit; a non-deterministic
  dimension is declared, never quietly included.
- Its output MUST be quoted VERBATIM: no hand-rounding, no retyping.
- It MUST be able to measure a HISTORICAL commit (`git show HEAD:…` / `HEAD`-pinned
  greps), so a doc can cite a snapshot that is not today's.
- A figure that a reader acts on MUST name this document's generator line and the
  base commit it was measured at.

These five are the properties that make the output a measurement rather than a
transcript, and they are enforced by how the section reads the tree — every
answer is pinned to `HEAD`, never to the worktree.

## What the census does NOT print (declared)

Per-verb flag inventories, per-tool schema prose, and any behaviour that needs a
display, a browser or a model file AT CALL TIME. This instrument measures what
is PRINTED and what is DECLARED at a commit; it never runs a capability to see
whether it works (Rule 27: no device load, no saturation — every number is a
static count or one bounded `-h`/render probe).

## Rule 35 — the four answers (why this is not a new dependency)

1. **What breaks without it.** Nothing breaks for the PRODUCT; what breaks is a
   DEMO CLAIM. The demo chain's links 2..9 quote capability figures ("every verb
   has help", "the eyes exist", "the renderer draws CJK", "some verbs refuse by
   design"). Typed into prose, each goes stale the moment a verb is added — the
   repo's own measured precedent is the 2026-09-27 snapshot claiming 52 files
   where the classification said 48 prose + a handful of code, and the
   2026-09-20 delta whose baseline had drifted. The failure is observed, not
   hypothetical.
2. **Why it cannot be fixed in-process.** It IS in-process: the change is 453
   lines appended to the script that already exists and already runs. No new
   Go code, no new tool, no new process — the answer to "can this be handled by
   code that already runs" is yes, and that is what was done.
3. **Who owns its lifecycle.** The script's existing owner: whoever runs the
   snapshot. It has no lifecycle of its own — no install, no schedule, no
   resident process, no service unit, no state file. Its temp files live under
   `$GOTMPDIR` (the script's existing scratch root, off the `/` partition by
   default, Rule 22) and are removed before the run ends: `CENSUS_TMP`,
   `CENSUS_TMP.verdicts`, `CENSUS_TMP.fonts`, `CENSUS_TMP.dirs`, the sample
   `.txt` and its `.png`.
4. **What the next person sees.** They see the section's own header block: it
   states the four dimensions, says the figures are derived from the dispatch
   chain and the definition sites, and names the Rules the section obeys. There
   is nothing to find running and wonder about — the artifact is a script
   section that only ever writes its own stdout.

**No new file format, no new database, no new scheduled job, no new config key,
no new env var read by this section.** The census reads three existing sources
(`cmd/agent/main.go`, the verb/constant definitions under `cmd/agent`, `internal/toolnames`,
`internal/agent/loop.go`, `internal/text2img/render.go`, `internal/shot/chrome.go`,
`cli_voice.go`) and two existing runtime facts (`$DISPLAY`, the PATH/playwright
chrome resolution) — nothing is added to the environment.

## Figures and their generators (Rule 38)

| figure | generator |
|---|---|
| all of the section | `bash scripts/gopi-stats.sh` → `== capability census ==` |
| base commit, branch, dirty files | same script → `== identity ==` |
| dispatch predicates | `git show HEAD:cmd/agent/main.go \| grep -oE 'is[A-Za-z]*Subcommand\(dispatchArgs\)'` |
| verbs declared / two-word / refusal-path | `census_pred_word` over each predicate (the script prints the count and the odd rows) |
| verb `-h` verdicts | `census_help_verdict` → `'<verb>' -h`, first line compared against the top-level page's head |
| LLM tools with a schema entry | `git show HEAD:internal/agent/loop.go \| grep -cE 'Name:[[:space:]]+toolnames\.Tool[A-Za-z]+'` |
| the eyes' channels | the verb's own file, grepped with the declared `EYES_PRE_*` EREs |
| host channel verdicts | `$DISPLAY`, `census_chrome_bin`, `[[ -t 0 ]]`, `$HOME/$EYES_VOICE_ROOT` |
| non-ASCII / CJK corpus counts | `git grep -lIE` with the two declared classes at HEAD (+ the printed self-test) |
| font candidates / roots / present faces | `git show HEAD:internal/text2img/render.go` (name list; `fontDirs()` body) + `-f` on the two roots |
| render probe | `text2img <sample> <out.png>` on the fixed 60-char CJK sample |
| deferred list | the same `-h` probes, classified by refusal content first |

## Evidence (session dir, never committed)

All under `/home/user/.gopi/sessions/capdemo-1/`: the pre-extension baseline
transcript (`census-baseline.txt`), the two runs at the base commit (`run1.txt`,
`run2.txt`), their full diff (`full-determinism.diff` — 8 lines, all in the
live-corpus adoption counters), the extracted census sections
(`census-run1.txt`, `census-run2.txt`) and their diff
(`census-section-determinism.diff` — empty), plus the earlier exploratory runs.
Evidence lives outside the tree on purpose: transcripts are not deliverables,
and the worktree ends clean (Rule 30).
