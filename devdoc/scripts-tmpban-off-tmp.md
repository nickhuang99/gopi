<!-- doc-class: historical -->
# Task Report: scripts gate (TestScriptsAreBoundedAndOffTmp) green — three scripts rehomed off `/`

Branch: `fix/scripts-tmpban-off-tmp` (worktree `~/workspace/gopi-scripts-tmpban`)
Base: master `fff3c9a3`

## Completed

The master-red half of `internal/testutil` this task was dispatched for:
`TestScriptsAreBoundedAndOffTmp` (scripts_gate_test.go:153/155) reported 15 `/tmp`
literals across three dev/test scripts plus one script that created scratch data
without the shared contract. All three now route their scratch through
`scripts/lib/tmp-ban.sh`:

| script | before | after |
|---|---|---|
| `scripts/voice-e2e-fixture.sh` | `OUTDIR=${1:-/tmp/voice-e2e-fixture}` | helper-sourced; default OUTDIR = `gopi_scratch_mkdir voice-e2e-fixture` (removed on exit); a caller-supplied OUTDIR is device-id asserted off `/` |
| `scripts/verify-rehome.sh` | `FIX="$(mktemp -d)"` | `FIX="$(gopi_scratch_mkdir verify-rehome)"` + `trap gopi_scratch_cleanup EXIT INT TERM` |
| `scripts/voice-e2e-hp.sh` | `WORKDIR=${WORKDIR:-/tmp/voice-e2e-hp}`, `FIXTURE_WAV/REF` under `/tmp`, 8 control-side log paths under `/tmp` | `WORK_ROOT` from the helper; `WORKDIR/FIXTURE_OUTDIR/LOGDIR` all derive from it; HP's twin directory is created and device-id asserted off `/` with `stat -c %d`; `cleanup_all` removes HP's directory then the local one |

Every `readonly` default that used to name a literal path now derives from one
named variable, so no path is typed at a use site (Rule 1). No test file was
touched — the package's only changed files are the three scripts plus the
`devdoc/voice-e2e-hp.md` run recipe.

### Design decisions worth keeping

1. **Fixture hand-off across nodes.** The fixture is *produced on HP* (the
   capture node) and *consumed by the control side*'s log/CER steps. Both sides
   therefore derive the fixture path from the same scratch tree:
   `FIXTURE_OUTDIR` (env-overridable) → `$WORK_ROOT/voice-e2e-fixture`, and the
   recipe is `bash scripts/voice-e2e-fixture.sh "$FIXTURE_OUTDIR"` on HP with the
   same two env vars passed to the rig. No path is copied by hand, and an
   explicit `FIXTURE_WAV`/`FIXTURE_REF` override still wins (verified below).
2. **Per-script scratch, not a shared one.** The fixture script creates its own
   default directory (and removes it on exit); the rig creates a *separate*
   `queue-voice-e2e-hp-<pid>` and takes the fixture path by hand-off. Deriving the
   fixture directory from the rig's own `$$` would not be portable — HP's twin is
   a second, independent `gopi_scratch_mkdir` call.
3. **The `gopi_scratch_cleanup`-in-`cleanup_all` ordering** is deliberate: a
   failed remote `rm -rf` reports to stderr and the local cleanup still runs
   (Rule 15b — no silent swallow, and no leaked local scratch).
4. **The capture node's side of the ban is measured, not assumed.** HP has no
   checkout of this repo (so it cannot source `scripts/lib/tmp-ban.sh`); the rig
   runs the same criterion on it instead — `stat -c %d "$WORKDIR"` vs
   `stat -c %d /` — prints both ids as evidence, and exits 2 with the fix text
   when they match (probed below with `WORKDIR=/tmp/voice-e2e-hp-probe`:
   `dev=66307 (/ dev=66307)` → **FAIL, exit 2**). The control side's own
   directory got the same check through `gopi_scratch_mkdir`.
5. **Negative probes for the two new asserts** (Rule 10.4 — verify the smallest
   sample, do not trust "looks right"): `bash scripts/voice-e2e-fixture.sh
   /tmp/probe-fixture` → helper's device message + exit 2, and the directory it
   created is removed again; the rig's HP probe above → exit 2 before any device
   work. Both measured, both green.

## Not Done

- The full 228 s leg-B capture and the real ASR run were **not** re-executed
  end-to-end: the acceptance run below uses `CUT_AT=8` and `LEG_B_SECONDS=15` to
  keep the proof inside the session budget (a full run is ~8 minutes of audio).
  What that still exercises is every changed line: both legs' capture, transport,
  scoring and cleanup. The capture *quality* claims (CER numbers) belong to the
  original measurement in `devdoc/voice-e2e-hp.md`, unchanged by this task.
- `devdoc/voice-e2e-hp.md`'s historical §2 recipe now describes the scratch
  path; the recorded measurement logs under `devdoc/assets/voice-e2e-hp/` were
  left untouched (they are evidence, not instructions).

## Verification

- Gate (the acceptance criterion), in the worktree:
  `CGO_ENABLED=0 go test -count=1 ./internal/testutil/ -run TestScriptsAreBoundedAndOffTmp` → **ok** (0.163s).
- Whole package: `CGO_ENABLED=0 go test -count=1 ./internal/testutil/` → **ok** (18.7s) — no sibling test disturbed.
- Whole-tree build: `CGO_ENABLED=0 go build ./...` → **OK**.
- Parse check: `bash -n` on all three scripts → clean; the gate's own `bash -n` pass agrees.
- `/tmp` literals left in the three files: **0** (only the helper's name and the
  explanatory comments mention it).
- `scripts/verify-rehome.sh` end-to-end (the full fixture pass: 4 records, dry-run
  no-op, apply, per-session destinations, unattributable lines kept):
  fired=2, unattributed=2, `source=kept`, scratch under
  `…/gopi-test-scratch/queue/queue-verify-rehome-<pid>/…`, **no leftover scratch
  directory after exit** (trap cleanup verified with `ls -d`).
- `scripts/voice-e2e-fixture.sh` on peerbox: env-var override respected
  (`…/queue/smoke-fixture-XXXX`), the pinned SOURCE hashes still asserted, and
  the default-directory form leaves nothing behind.
- Real two-node run (see the acceptance evidence section below).

## Acceptance evidence (real runs, this task)

_Pending — filled in with the rig's captured output._
