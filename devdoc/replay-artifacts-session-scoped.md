<!-- doc-class: historical -->
# Task Report: `gopi replay` lands its artifacts in the SESSION's own directory

Task `01a0fd14-8c35-7587-9824-6e831a2da827` · branch `feat/replay-session-artifacts`
(superseding the worktree this file was written in, named after that id)

## The problem, in one line

`make replay` could not be run by the person it was built for: its resolution
lived in the shell (a join of a sessions path plus a prefix scan that only
accepted a directory holding `session.jsonl`, so a **worker** session was
invisible), and its default landing was a directory **of the checkout**
(`docs/archive/demo/replay/arm-<id>-<date>/`), so filming a session dirtied the git tree
the operator was standing in.

## Completed

### 1. ONE session resolver, and it is the Go one (Rule 14)

`scripts/replay/replay.sh` no longer composes a sessions path and no longer scans
one. It asks the binary:

```
$ gopi replay session 4ca8cd29-019c-4dda-a000-6dafd782bf91
record=/home/user/.gopi/sessions/4ca8cd29-.../session.jsonl
dir=/home/user/.gopi/sessions/4ca8cd29-...
artifacts=/home/user/.gopi/sessions/4ca8cd29-.../artifacts
film=/home/user/.gopi/sessions/4ca8cd29-.../artifacts/replay/20261002-231612
```

- **New**: `internal/replay/session_ground.go` — `resolveSessionGround()`, over
  `gopilog.ResolveSessionDir` + `gopilog.FindSession` (the resolver `gopi log` /
  `gopi stats` / `gopi raw` and the agent all use) and
  `sessionlayout.RecordInDir`. It knows the three layouts: bare
  (`<id>/session.jsonl`), task-run (`<task_id>/<run-uuid>.jsonl`), and flat
  (`<sessions>/*.jsonl`).
- **New**: `gopi replay session <operand>` — a pre-step that prints
  `record=`/`dir=`/`artifacts=`/`film=` and draws nothing (`internal/replay/replay.go`,
  `printSession`).
- **Deleted from the shell**: `gopi_sessions_dir()`, the candidate scan, the
  short-id machinery (`SHORT_ID_RE`, `MAX_CANDIDATES`, the "did you mean" list),
  `OUT_TAG`, `replay_derive_out_tag`, `ARM_OUT`, `OUT_TAG_*` constants.

Two behaviours changed on purpose, and both are refusals rather than guesses:

- **A truncated id is refused.** It used to resolve a prefix; `FindSession`
  requires the full id, and the refusal says so and names the tree. A prefix that
  matches two sessions used to film whichever sorted first.
- **A worker session now resolves**, to the container that run belongs to
  (`<sessions>/<task_id>/`), not to nothing.
- **The flat layout is refused by name** (`FLAT layout`) rather than resolved: a
  record directly under the sessions root has no container of its own, and
  `artifacts/` would then land among ~80k sibling entries. Refusing is honest;
  inventing a container beside a root's own files is not.

### 2. Landing: `<session dir>/artifacts/replay/<UTC stamp>/`

- `sessionlayout.ArtifactsDirName = "artifacts"` is the **one definition** (Rule 14);
  `tools.ChatSessionArtifactsDirName` is now an **alias of it** (it used to be a
  second literal that merely happened to match), and `internal/replay` reads the
  same constant. Two writers, one spelling.
- `SessionGround.FilmDir(now)` creates and returns a directory that does **not**
  exist, so two replays of one session never share one: a second start in the same
  second gets a `-N` suffix (`·stampAttempts` bounds an absurd input, not a
  cadence). Replay no longer overwrites its own previous film.
- `--out` survives as a deliberate **override of location, not of the rule**: the
  film directory the verb created is *moved* to `<--out>/<its own name>` (so the
  uniqueness still comes from one implementation, no empty directory is left in the
  session's tree, and a name already at the destination is refused rather than
  written into).
- `--out-tag` is **gone**, with the `arm-<tag>` container it named.

### 3. The Makefile target: no `build` prerequisite

`make replay SESSION=<full id>` now passes only the flags the operator set, so
every other value is the script's own default (a second copy in the Makefile would
be a second answer, drifting).

**Why the build dependency was dropped — the answer the earlier round asked for:**
`gopi replay` has been a subcommand of the ONE binary since 2026-10-02, so the
prerequisite compiled the console, the agent loop, every tool and every provider
client to run a verb whose own package (`internal/replay`) imports only
`internal/lineedit` and `internal/uiclient`. Measured here (warm cache,
2026-10-03): `make build` 1.14 s vs `go build ./internal/replay/` 0.06 s. Two
costs argue louder than the ratio: it imposes go.mod/toolchain integrity on an
operator whose only question is "what did that session look like", and it
**rewrites `bin/gopi` under a running console** (the operator's own shell holds
that file open). What it was really protecting — "do not run a stale binary" — is
now the script's own `--bin` check, which refuses **by name**:

```
$ make replay SESSION=<id>          # with no bin/gopi
[replay] FATAL: no gopi binary at <repo>/bin/gopi (pass --bin, or run make build)
```

**And the 0664 comment:** the script is **not** executed — the target invokes
`bash $(CURDIR)/scripts/replay/replay.sh`, which is the spelling the script's own
usage already documents. The mode is left as the index has it, because `chmod +x`
would add a second supported spelling without removing the first. The comment now
says exactly that, where it used to read "permission workaround".

### 4. The regression test, rebuilt around the script

`scripts/replay/test-session-operand.sh` drives **the script itself** against a
**real binary** (built into the scratch), over a hand-built fixture sessions tree —
never the live ledger. 9 cases, `20 passed, 0 failed`:

| case | what it pins |
|---|---|
| C1 | an operand naming no session is refused **with the verb's own reason** (the script invents no message) |
| C2 | a full id resolves; `record=`/`dir=`/`artifacts=`/`film=` are the session's own, and the film directory carries a UTC stamp |
| C3 | a **worker** session (task-run layout) resolves — the layout the deleted shell scan could not see |
| C4 | a truncated id is refused (never the first prefix match) |
| C5 | two resolutions of ONE session name two DIFFERENT directories, both created |
| C6 | a run leaves the checkout **byte-identical**, and mints no `arm-*` container |
| C7 | `--bin` defaults to `<repo>/bin`, named on failure |
| C8 | the flat layout is refused, with its reason |
| C9 | `--out-tag` is an unknown flag; `--out` is still accepted |

Two more consequences of the same change, both visible in the script's own usage
(`bash scripts/replay/replay.sh -h`): the session's *filmed* run is unchanged —
`--session` now takes the **record** (the resolved `session.jsonl`-shaped log)
rather than a directory — and `--out-tag` / the `arm-<tag>` container are gone
from the flag surface entirely.

One test-side lesson worth keeping: `printf … | grep -q` under `set -o pipefail`
reports a **matching** assertion as a failure (the left side takes SIGPIPE the
moment grep exits). The assertions are `case(1)` substring tests now — noted in the
file, because the next person will reach for the pipeline again.

## End-to-end evidence (Rule 31 — the surface really run)

`scripts/replay/replay.sh` over **real sessions in the operator's own profile**,
with the repo's own porcelain measured around them:

| run | operand | rc | where it landed |
|---|---|---|---|
| chat1 | `4ca8cd29-019c-4dda-a000-6dafd782bf91` (a chat session, bare layout) | **0** | `…/sessions/4ca8cd29-…/artifacts/replay/20261002-231532/` — `chat-replay.gif` (19,613,759 B), `frames/` (209 PNG), `beats.tsv`, `diff-table.tsv`, `replay.json` (`frames=198`, `distinct_frames=198`, `px_per_char=19.017`, `session_ref` = "1252649 bytes, 1059 records") |
| chat2 | same id, second run | **0** | `…/artifacts/replay/20261002-233039/` — **the two films coexist**, each with its own GIF (19,768,677 B) and `replay.json`. This is the "repeat runs must not overwrite" requirement, observed, not asserted |
| worker | `0fc775e0-924c-4dfd-9377-a2cde9a52715` (a TASK-RUN layout session — `<sessions>/<task_id>/<run-uuid>.jsonl`) | resolution OK | resolved to `dir=<sessions/01a0fd2e-e8f4-7705-a009-3e05445eb2df`, `record=…/0fc775e0-….jsonl`; the film directory was created there (each attempt a fresh `-N` when a name was taken — the coexistence rule again). Attempts 1–2 did **not** assemble: `beat 95 was never acknowledged within 30s (the driver stopped)` |
| worker (attempt 3) | same id | **0** | `…/artifacts/replay/20261002-233735/` — the film **assembled**: `chat-replay.gif` (9,006,550 B, 135 frames), `frames/` (8 sampled PNG), `replay.json` (`frames=135`, `distinct_frames=135`, `dropped_frames=0`), `diff-table.tsv`, `mask-boxes.json` |
| — | `01a0fd14-8c35-7587-9824-6e831a2da827` (a TASK id passed as a session id) | 2 | refused: `names no session under <sessions> — a session id is the FULL id … a truncated id is refused rather than guessed`. A task id is not a session id, and the resolver says so instead of picking a near match |

**The repo stayed clean through all of it** — `git status --porcelain` before the
runs and after them differs by nothing the runs did (the before/after difference is
this task's own edit, `M internal/replay/session_ground.go`, made between the two
snapshots by me, not by a replay). The old default wrote into
`docs/archive/demo/replay/arm-<id>-<date>/`; nothing lands in the checkout now.

**A second defect the worker session exposed (and this task fixed):** the first
worker film assembled and then failed verification with
`LEAK: replay.json: still carries the shape 0fc775e0-924…`. The leaking string was
`session_ref`'s FILE NAME — and the three layouts name a record differently
(`session.jsonl` in the bare layout, `<uuid>.jsonl` in a task-run container), so
reporting the name publishes a session uuid exactly when the session is a
worker's. `tools/replay/assemble.py`'s `session_facts()` now reports size + record
count + timestamp span only (the stronger identifier anyway). The rebuilt
`replay.json` reads `574289 bytes, 360 records, 2026-10-02T23:17:18.99… .. …23:34:46.82…`,
and that film's GIF is 8,795.5 KiB / 135 frames. This was reachable before too —
the driver used to hand `assemble.py` a directory, whose basename it never
printed; handing it the record file is what made the name visible.

**The worker film: assembled on the third attempt, and what that costs.** The
resolution, the landing directory and the per-film uniqueness behaved for the
worker session exactly as for the chat one; attempts 1 and 2 hit the driver's
ack path (`beat 95 was never acknowledged within 30s`), and attempt 3 assembled
(135 frames, a 9,006,550 B GIF, its own `replay.json`) and verified green through
`verify.py` with the driver's own sampler flag:

```
$ timeout 400 python3 tools/replay/verify.py --out <…>/20261002-233735 \
      --scratch <worker> --session <…>/0fc775e0-….jsonl --allow-sampled-frames
[verify] PROVENANCE OK … VISIBILITY OK … arithmetic OK: 135 raw - 135 distinct = 0 dropped
[verify] beat count OK: 135 (ledger and replay.json agree)
[verify] OK: at or above the floor / the measured width and the delivered width agree
[verify] OK: 7 adjacent pairs differ; the per-pair table is diff-table.tsv
[verify] POSITIVE CONTROL OK: 108 identifying run(s) were actually replaced
[verify] RESULT: all five claims measured true            # rc=0
```

Two consequences to state rather than bury. (a) **A failed attempt leaves an
EMPTY film directory** (`20261002-233211/`, `20261002-233211-2/`, `20261002-233415/`
each hold zero entries for this session): the landing is created before the film
is written, so an aborted run costs a name and a `mkdir`. It is harmless (no
artifact is overwritten, the next run takes `-N`), but it is a residue, and it is
NOT this change's to clean — `replay.sh` deletes nothing it did not create whole.
(b) `verify.py`'s motion stage samples `FRAMES_SAMPLE_KEEP`-th frames as stills and
refuses a floor of 12 without `--allow-sampled-frames` — which the driver itself
passes. That is the sampler's setting versus the checker's floor: pre-existing and
independent of where artifacts land (my manual re-runs for this report deliberately
omitted the flag and were refused). **Nothing about the landing is in doubt**: two
chat films coexist with their own GIFs and `replay.json`, a worker session resolved
and assembled into its container, and every run left `git status --porcelain`
unmoved.

## Not Done (honest)

1. **`docs/archive/demo/replay/` is untouched — 140 tracked files, 38 MB** (old arms such
   as `arm-sess-4ca8cd29-20261002` beside the doc pipeline `shoot-replay.sh`,
   `verify-frames.py`, README). They are *landed demo assets*: they are what
   `devdoc/replay-*.md` and the terminal-comparison docs cite as evidence, so
   deleting them erases citations, and `README.md` links into them.
   **Recommendation (advice only, nothing done):** leave the README's own reference
   frames, and treat the *old arms* as archivable rather than deletable — the honest
   move is a follow-up task that first rewrites the citing docs, then `git rm`s the
   arms in one commit. Nothing was removed here because a doc's evidence is the
   doc's to keep, not this task's to delete.
2. **`--out-tag` survives elsewhere, deliberately:** `docs/archive/demo/replay/shoot-frames.py`
   and `shoot-replay.sh` still have their own `--out-tag` for `img-<tag>/`
   comparison arms, and `docs/archive/demo/terminal-compare.sh` passes it. That is a
   different pipeline (frame shooting into the demo tree, driven by a
   terminal comparison), not the `gopi replay` landing; it was out of scope and is
   left exactly as it was. `tools/replay/{grab,assemble,verify}.py` still take a
   `--session` path — they are handed `$SESSION_RECORD` (a file), which is what
   their own `session_facts()` already accepted.
3. **A worker session's first two films did not assemble — the third did.** The
   resolution, landing and uniqueness are proven for all three layouts; the ack
   path (`beat 95 was never acknowledged within 30s`) failed attempts 1–2 and
   attempt 3 assembled and verified green. The ack path is outside this change, and
   the follow-up (if it recurs) is a task on that path, not a re-open of this one.
   **Related residue named, not cleaned:** each aborted attempt left an EMPTY film
   directory under the session's `artifacts/replay/` (three for this session).
4. **C7 is skipped in a checkout where `bin/gopi` exists** (the refusal needs an
   absent binary). The case states that skip in its own output rather than passing
   silently.
5. **`internal/replay`'s own Go tests were not extended** with a `session_ground`
   unit test — the shell file's cases drive the same three layouts end to end
   through the real binary, which is the stronger evidence, but a direct Go test of
   `recordForPath`'s refusals would survive a shell-less CI better. Named here
   rather than implied.

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` — OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK.
  `go vet ./internal/replay/ ./internal/sessionlayout/ ./internal/tools/` — clean.
- **Tests**: `bash scripts/replay/test-session-operand.sh` — **20 passed, 0 failed**
  (9 cases; C7 self-declares its skip in a checkout that has `bin/gopi`).
- **Ran** (the changed surfaces, verbatim):

  ```
  $ GOPI_HOME=/home/user/workspace/tmp-replayprobe-1eeC bin/gopi replay session cccccccc-1111-4111-8111-111111111111
  record=…/.gopi/sessions/cccccccc-…/session.jsonl
  dir=…/.gopi/sessions/cccccccc-…
  artifacts=…/.gopi/sessions/cccccccc-…/artifacts
  film=…/.gopi/sessions/cccccccc-…/artifacts/replay/20261002-231519       # rc=0

  $ GOPI_HOME=… bin/gopi replay session dddddddd-2222-4222-8222-222222222222   # task-run layout
  record=…/.gopi/sessions/t-worker/dddddddd-….jsonl
  dir=…/.gopi/sessions/t-worker
  artifacts=…/.gopi/sessions/t-worker/artifacts
  film=…/.gopi/sessions/t-worker/artifacts/replay/20261002-231519         # rc=0

  $ GOPI_HOME=… bin/gopi replay session eeeeeeee-3333                        # rc=1
  gopi replay: eeeeeeee-3333 names no session under …/.gopi/sessions — a session id is the FULL id
  (the one `gopi log --last` prints); a truncated id is refused rather than guessed

  $ make replay
  usage: make replay SESSION=<full session id|path> [OUT=<dir> EMU=<terminal> SCRATCH=<dir>]
  ```
- **Result**: the pre-step resolves all three layouts and refuses both an unknown
  id and a truncated one; `make replay` with no argument prints usage and exits 2
  (the recipe gate, not "No rule to make target"). The full film run
  (`scripts/replay/replay.sh` over a real chat session and a real worker session)
  is recorded in `## Evidence` below.

## Evidence (self-filed)

- `GOPI_HOME=<fixture> bin/gopi replay session cccccccc-…` — the four `key=value`
  lines above, `rc=0`; with `dddddddd-…` (task-run layout) the `dir=` is the task
  container, `rc=0`; with `eeeeeeee-3333`, `rc=1` and the refusal on stderr.
- `make replay` (no argument) — the usage line printed, `make` reported `Error 2`.
- `bash scripts/replay/test-session-operand.sh` — `session-operand: 20 passed, 0 failed`.
- `go build ./...` — exit 0, no output.
