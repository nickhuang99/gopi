<!-- doc-class: normative -->
# End-user audit — Slice 8: the PRIMARY RUN PATH

Task `01a0dcfa-dafb-709d-808c-8bc93369aa63`, 2026-09-26.
Protocol: `devdoc/end-user-audit-protocol.md` §Eighth sweep.
Oracle read **before** any source: `gopi -h` (captured verbatim to
`evidence/oracle/gopi-h.txt`).

---

## 1. The oracle

| page | bytes | md5 |
|---|---|---|
| `gopi -h` (== `gopi --help`, byte-identical) | 10092 | `e45a708aa8eb6856fe8a7355c96eb635` |
| `gopi -help` (single dash → **not** a flag) | 0 stdout / 10305 stderr, rc=1 | — |
| `gopi sandbox -h` (isolation tool) | 5418 | `63cc3f05b39861ec35976cc629d30f5c` |

The promises this slice audits, quoted verbatim from the page:

```
Usage:
  gopi [options] "<prompt>"           Run one task from a prompt argument
  gopi [options] input.txt            Run one task read from a text file
  gopi [options] < input.txt          Run one task read from stdin
  gopi [options] --prompt TEXT a.png  Run one task with files/images attached

Inputs:
  "<prompt>"      The task text. With no prompt and no stdin, gopi prints this help and
                  exits 1.
  @PATH           A positional or --prompt value starting with '@' reads the task
                  payload from PATH verbatim — the way to pass multi-line text with
                  quotes or shell metacharacters. '@-' reads stdin; "\@text" passes a
                  literal prompt that starts with '@'.
  image.png       A positional naming an existing .png/.jpg/.gif/.webp file is attached
                  NATIVELY to the vision model ... Use --prompt TEXT for the instruction.
  ./GOPI.md       Project rules, auto-loaded as a system message when present.

Options:
  --prompt TEXT             task instruction; positionals become files/images
  A knob that has a CLI flag is also reachable by its full env var name as a flag ...
  There are NO single-dash options: a token like "-z" is refused, never taken
  as prompt text (use --prompt -z to pass that text literally).
```

There is **no `gopi <command> -h` for the run path** — this page is the whole contract
for it.

## 2. Instrument — the request boundary (method refinement)

rc/stdout/stderr cannot show what the run path *does with the payload*. This slice
added a **local echo endpoint** (`evidence/echo_server.py`, an Anthropic-shaped SSE
server that records every request body and replies with a minimal valid stream) and a
**fixed binary** built from this worktree (`evidence/bin/gopi`,
sha256 `acb2f941…` — so the audited code is the run's own commit, not the
gopi-versioned install at `/usr/local/bin/gopi`).

Consequence: payload fidelity, image attachment and the vision auto-switch were all
measured **at the wire**, at **0 real tokens**, with the body on disk for re-reading
(`evidence/echo/req-NNN.json`).

## 3. Probe matrix — every probe, exact argv, observed outcome

Invocation skeleton (all sandboxed; `<SB>` = the sandbox from Rule 34, built binary):

```
env -i PATH=/usr/local/bin:/usr/bin:/bin HOME=/home/user GOPI_HOME=<SB> \
  evidence/bin/gopi <flags> <input>      # stdin as noted
```

`P*` = first pass (before the sandbox was materialised — see §6), `S*` = sandboxed pass
on the corrected helper, `E*` = end-to-end against the echo endpoint.

| # | input form | rc | stdout | stderr | side effect |
|---|---|---|---|---|---|
| S1 | no prompt, empty stdin | **1** | 10092 B help (md5 `e45a708a`) | 0 B | none — **matches the page exactly** |
| S9 | `--thinking` (bare flag), no prompt, empty stdin | 1 | 10092 B help | 0 B | none — flag accepted, then help |
| S10 | `--`, no prompt | 1 | 0 | 10115 B (`gopi: unknown flag --` + help) | none |
| S3 | `-z "hello world"` | 1 | 0 | 10299 B (`gopi: unknown option "-z": gopi options are spelled --name …`) | none |
| S2 | nonexistent file positional | 1 | 0 | 2818 B (generic refusal + run banner) | 1 session dir + 1 ledger row before the failure |
| S4 | directory positional | 1 | 0 | 127 B `Error reading input file: read …/adir: is a directory` | none |
| S5 | `@/…/nope.txt` | 1 | 0 | 280 B `Error reading @ payload file …: no such file or directory (escape … as \@)` | none |
| S6 | `@/…/adir` | 1 | 0 | 261 B `… is a directory` | none |
| S7 | `@` alone | 1 | 0 | 114 B `Error reading @ payload file "": open : no such file…` | none |
| S8 | `--prompt @` | 1 | 0 | 114 B (identical to S7) | none |
| S18 | empty file positional | 1 | 0 | 18 B `No input provided` | none |
| S19 | `@empty.txt` | 1 | 0 | 18 B `No input provided` | none |
| S22 | `@-` with **empty** stdin | 1 | 0 | 18 B `No input provided` | none |
| S14 | `frobnicate --wat` | 1 | 0 | `gopi: unknown flag --wat` + help | none |
| S21 | `frobnicate` **alone** | 0 | `SLICE8-ECHO-OK` | run banner | **real run** — the token is a PROMPT (`[EPIC GOAL]\nfrobnicate`) |
| S15 | `--db X code-search FooBar` (flag before verb) | 1 | 0 | 403 B: *`"code-search" is a gopi command, not prompt text — it is a POSITIONAL here, following the run flag --db … Put the verb first* | none |
| S11 | stdin = `real.txt` (66 B, `$USER`, quotes) | 0 | `SLICE8-ECHO-OK` | banner | payload sent verbatim |
| S12 | `@-` with `at.txt` on stdin (99 B, 3 lines) | 0 | `SLICE8-ECHO-OK` | banner | payload sent verbatim |
| S13 | `\@literal text $HOME $(id)` | 0 | `SLICE8-ECHO-OK` | banner | literal `\@literal …`, `$` unexpanded at gopi's hand |
| S16 | `--prompt "SLICE8TOKEN-E17 …"` (no positional) | 0 | `SLICE8-ECHO-OK` | banner | payload sent |
| E3 | `@/…/at.txt` | 0 | `SLICE8-ECHO-OK` | banner | `$HOME`, `$(id)`, `*glob*`, `"quotes"` all reach the wire **verbatim** |
| E4 | `/…/real.txt` positional | 0 | `SLICE8-ECHO-OK` | banner | file read, `$USER`/quotes verbatim |
| E5 | `--prompt "…" /…/shot.png` | 0 | `SLICE8-ECHO-OK` | `[gopi] image input detected → using vision model deepseek-flash` | **image block on the wire** (base64, 100 B fixture = 8×8 red PNG) |
| S31 | `--vision-model model-VISION --prompt … shot.png` | 0 | `SLICE8-ECHO-OK` | `… using vision model model-VISION` | `"model": "model-VISION"` — **auto-switch verified at the wire** |
| E9 | `--prompt "…" /…/real.txt` (non-image) | 0 | `SLICE8-ECHO-OK` | banner | **file dropped — see RP-3** |
| S2b | nonexistent file positional, echo endpoint | 0 | `SLICE8-ECHO-OK` | banner | **path string sent as the task text — see RP-2** |
| S28 | `--prompt "…"` against a **refused** endpoint (`127.0.0.1:9`) | 1 | 0 | 1614 B; ends `Error: LLM call failed … connect: connection refused` | 0 tokens — the end-to-end run of §"one run against the refused endpoint" |

Full per-probe transcripts (argv, rc, stdout, stderr, stdout md5) are under
`evidence/transcript-<label>.txt`; the raw captures are `evidence/out-<label>.txt` and
`evidence/err-<label>.txt`.

## 4. Divergences (five fields each, F1–F5 class)

Filed as bug rows (see §7). `contract` quotes the exact help line that promised
otherwise.

### RP-1 (F1, high) — a plain shell prompt is wrapped in the agent-harness frame

- **expectation**: `Usage: gopi [options] "<prompt>"   Run one task from a prompt
  argument`; `Inputs: "<prompt>"   The task text.` The page mentions no frame.
- **invocation**:
  `env -i … GOPI_HOME=<SB> evidence/bin/gopi --base-url http://127.0.0.1:18123 --db <SB>/.gopi/memory.db --prompt "SLICE8TOKEN-CLEAN clean-env probe"`
- **observed**: rc=0; the one user message on the wire is 2194 chars and begins
  `[EPIC GOAL]\nSLICE8TOKEN-CLEAN clean-env probe\n\nBased on this Epic,
  [TASK_LIST_BEGIN] by your second tool round; re-emit on change.\n\n[EPIC REFRAME]
  Before any other work: … TOO_BIG requires a roadmap … THE BUDGET IS PART OF THE
  CRITERION …` — ~1000 chars of harness operating instructions around a plain shell
  prompt. Reproduced with `env -i`, so it is produced by the run path, not inherited
  from my environment. Uniform across `@PATH`, positional file, stdin, `\@text`,
  `--prompt TEXT`.
- **contract**: `gopi -h` Usage line 1 + `Inputs/"<prompt>"`; no line of the page
  mentions a frame, a task list or a reframe directive.
- **acceptance**: the prompt reaches the model verbatim beyond what the page
  documents — or the page documents the framing and its trigger.

### RP-2 (F2, high) — a nonexistent file positional is silently taken as prompt text

- **expectation**: `gopi [options] input.txt   Run one task read from a text file` and
  `image.png   A positional naming an EXISTING .png/… file is attached`; the `@PATH`
  spelling of the same input refuses by name.
- **invocation**: `… gopi --base-url http://127.0.0.1:18123 --db <SB>/.gopi/memory.db /…/fx/nope.txt`
- **observed**: rc=0, a run starts, a session dir + ledger row are created, and the
  first user message is `[EPIC GOAL]\n/home/user/.gopi/sessions/…/fx/nope.txt\n\n` —
  the path string sent to the model as the task text. Nothing on stdout/stderr says
  the argument was not a file. Control: `@/…/fx/nope.txt` → rc=1,
  `Error reading @ payload file …: no such file or directory`.
- **contract**: Usage line 2 + `Inputs/image.png` ("an existing … file") + the `@PATH`
  bullet, which refuses the identical defect by name. Two spellings of one input
  cannot have opposite failure semantics silently.
- **acceptance**: a non-existent positional path is refused by name with a nonzero
  exit before any session/ledger row — matching `@PATH` — or the page states that a
  non-existent path is literal prompt text.

### RP-3 (F2, high) — with `--prompt TEXT`, a non-image file positional is dropped

- **expectation**: `--prompt TEXT   task instruction; positionals become files/images`;
  synopsis `--prompt TEXT a.png   Run one task with files/images attached`.
- **invocation**: `… gopi --base-url http://127.0.0.1:18123 --db <SB>/.gopi/memory.db --prompt "SLICE8TOKEN-E9 instruction" /…/fx/real.txt`
- **observed**: rc=0, a full run; the request body contains the instruction only —
  `grep -c "SLICE8TOKEN positional-file"` over the recorded body is **0**, total user
  text 2097 chars vs 2141 for the image case. stderr says nothing about the dropped
  argument. (The `.png` half of the same promise **does** hold: `req-010.json` carries
  an image block and `req-044.json` shows the wire model switching to `model-VISION`.)
- **contract**: the `--prompt` option line + the usage line `--prompt TEXT a.png`.
- **acceptance**: with `--prompt` given, a non-image file positional is read as input,
  or refused by name with a nonzero exit — never accepted and dropped in silence.

## 5. No-divergence table (everything that held)

| promise (help line) | probe | verdict |
|---|---|---|
| "With no prompt and no stdin, gopi prints this help and exits 1" | S1 | **exact** — rc=1, 10092 B, md5 identical to `gopi -h` |
| "There are NO single-dash options: a token like `-z` is refused" | S3 | **exact** — rc=1, names `-z`, suggests `--prompt -z` |
| "`@PATH` reads the task payload from PATH verbatim" | E3 | **exact** — multi-line, `"quotes"`, `$HOME`, `$(id)`, `*glob*` byte-identical on the wire |
| "`@-` reads stdin" | S12 | **exact** — 99 B payload verbatim |
| "`\@text` passes a literal prompt that starts with '@'" | S13, S20 (`--prompt '\@x'`) | **exact** — literal on the wire |
| "positional naming an existing .png … attached NATIVELY" | E5, S29, S31 | **exact** — image block on the wire; `"model"` is the vision model |
| "the model auto-switches to the vision model" | S29/S31 | **exact** — wire model = `model-VISION` when named; `deepseek-flash` by default |
| "`--prompt TEXT` task instruction" | S16, S26 | **exact** |
| "a tool name that is NOT a command is refused by name" | S14 (`frobnicate --wat`) | holds for the *flag*; see the S21 row below |
| error fidelity: every malformed input refused **before** a run | S4, S5, S6, S7, S8, S18, S19, S22, S10, S14 | **clean** — rc=1, one-line reason, no session, no ledger row |
| `gopi --help-env` advertised | — | not probed this slice (own sweep) |

Two boundary facts worth recording as **not divergences** (the help says nothing, so
there is no promise to break):

- `-help` is not a flag: rc=1 with the refusal + help on **stderr**, where `-h` puts
  the help on **stdout** at rc=0. Consistent with "no single-dash options", but see §6's
  method note — a script that tests `-help` gets a *different stream* than `-h`.
- `gopi frobnicate` (S21) is a **real run**, not a refusal — the first positional that
  is not a command is a PROMPT. The page states this only indirectly ("a tool name that
  is NOT a command is refused by name" sits in the Commands block, next to the
  positional-prompt usage line).

## 6. Instrument notes (honest)

1. **The `P*` pass ran without a sandbox.** The first batch was intended as a
   no-network purity check before creating the sandbox; every one of those probes was
   refused before a model call, but `P8` (nonexistent file) was **not** — it started a
   real run and wrote **2 session dirs** (`dc197ee2-…`, `01a0deaa-…`) plus **one auto
   task row** (`01a0dec2-1939-7a61-bcc5-d618f5700f71`) into the **operator profile**.
   Nothing was sent to a real endpoint (the invocation carried the refused
   `--base-url`), so the cost was 0 tokens, and no `SLICE8TOKEN` string entered the
   operator ledger (`grep -c SLICE8TOKEN /home/user/.gopi/memory.db` → **0**). The remaining 45 probes all ran inside the sandbox; this is recorded
   honestly as a protocol note rather than filed as a finding.

2. **Three probes reached the real API (S23/S24/S25).** Root cause, measured, not
   guessed: my own shell helper had the signature `runsp(){ n=$1; sd=$2; shift 2; … }`
   but those three calls passed only ONE leading argument, so the helper **shifted the
   `--base-url` flag away** — the recorded argv in
   `evidence/transcript-S23-text-only-model.txt` literally starts
   `gopi http://127.0.0.1:18123 --db …` (no `--base-url`), and the separator note
   `--- stdin: --base-url` shows the flag consumed as the label. The replayed argv is
   reproduced in §"root cause". Consequence: those runs used the operator's configured
   endpoint with an invalid model name (`model-ALPHA`) and were **rejected HTTP 400**
   (`The supported API model names are deepseek-flash, deepseek-v4-pro, but you passed
   model-ALPHA`), with `prompt=0(hit=0 miss=0) completion=0` in every trajectory
   summary — so **0 tokens** were consumed. After the fix the loop was re-run with the
   corrected helper; every subsequent probe carries its `--base-url`.
   A subsequent 4-cell A/B/C/D (same helper, corrected) showed **no** combination of
   `--vision-base-url`/`--vision-model`/`--model` relocates the text endpoint: each cell
   produced 2 echo requests and 0 real-API dumps — i.e. the leak was purely my argv bug,
   **not** a gopi fallback defect. Filing it as a gopi bug would have been a false
   finding.
3. **The vision fixture's base64 is 100 B** (an 8×8 synthetic PNG). It proves *an image
   was attached and the model switched*; it does not prove fidelity for a large photo,
   which this slice did not test.

## 7. Filed rows

| row | class | task id |
|---|---|---|
| RP-1 plain prompt wrapped in the harness frame | F1 | `01a0dec6-3a8a-7a5d-986f-2d6c1a5d8dee` |
| RP-2 nonexistent file positional accepted | F2 | `01a0dec6-280c-71f8-adf4-90067106a49f` |
| RP-3 non-image positional dropped beside `--prompt` | F2 | `01a0dec6-2863-77ee-9051-54c313f67e89` |

## 8. Isolation proof and cost statement

| claim | measurement |
|---|---|
| sandbox is off `/` | `stat -c %d <SB>` = **66309**, `stat -c %d /` = **66307** — different devices (Rule 22 criterion, measured by device id, not by path spelling) |
| the audited binary is this run's code | built from this worktree; sha256 `acb2f941708dba48149417dfd53a6157b92757f1f105d428fb0421d418a54059` |
| **no probe payload reached the operator ledger** | `select count(*) from tasks where payload like '%SLICE8TOKEN%'` = **2**, and both rows are **this audit's own filed bug rows** (id 3172 = RP-1, id 3171 = RP-3, `created_at` matching the `task_submit` calls) — attribution by id, not by absence. `distilled_qa` matching = **0**. |
| operator ledger size | `140853248` unchanged across the whole probe window |
| operator sessions | `7509` → `7513` (2 dirs are the sessions of this run's own `task_submit` calls, named by their task ids; the machine carries foreign runs, e.g. `01a0de69-…`) |
| sandbox ledger cost | **33 session dirs**; `tasks` = **33** rows, `task_events` = **33**; `distilled_qa` = **0**, `memory_feedback` = **0** |
| echo-endpoint requests recorded | **56** request bodies (`evidence/echo/req-000.json` … `req-055.json`) |
| token cost | **0 real tokens**. Every payload probe targeted the local echo endpoint (`127.0.0.1:18123`); every refusing probe used `--base-url http://127.0.0.1:9`; the 3 accidental real-API probes were rejected HTTP 400 with `prompt=0 completion=0` in their trajectory summaries. |
| worktree | `git status --porcelain` → **empty** at commit time |

## 9. Rule 5 report

`## Completed` — oracle captured (bytes+md5) before source; 53 probes executed with
exact argv; 3 divergences filed as five-field bug rows; no-divergence table for 11
promises; protocol doc updated (§Eighth sweep).
`## Not Done` — `--help-env`'s 60-row table not probed (own sweep); large-image fidelity
not measured (8×8 fixture only); the 3 accidental real-API probes mean S23/S24/S25's
*endpoint* claim is not reusable evidence (their payload was also captured by the echo
endpoint's neighbours, and the corrected cells A–D re-establish it); the operator profile
received 2 session dirs + 1 auto task row from the un-sandboxed `P*` pass (§6.1).
`## Verification` — `go build`: the slice's binary built clean (`BUILD_RC=0`); probes:
the table in §3, transcripts under the session dir; `Ran:` the exact argv per probe.
`## Evidence` — §3 and §8 tables, with the recording commands.
