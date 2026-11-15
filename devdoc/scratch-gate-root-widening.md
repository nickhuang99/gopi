<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The scratch gate's ROOTS — a scope that "audits scripts/" while eleven unscanned scripts build /tmp scratch

**Task:** 01a11397-ef8e-7da7-b48e-4df209449601 (2026-10-07) · **Files:**
`internal/testutil/scripts_gate_test.go` (the root list + the coverage proof),
the eleven repaired scripts · **Base:** the parent change
`task/01a1137f90d07f10a4d41f9227190ceb` (the tightened ROUTING predicate, commits
`6b18f8e84` + `7c6e3905a`) merged in, because the scope widening is only
meaningful against the predicate that decides what is an offender.

## What was wrong

The parent change tightened *what counts as an offender* and recorded the residue
it could not fix: ELEVEN tracked scripts that build their own scratch and never
route to `scripts/lib/tmp-ban.sh`, every one of them **outside**
`auditedScriptRoots` (`scripts/`, `devdoc/`, `testdata/`) and therefore never
scanned. That is not a cosmetic gap — the gate reported a green tree while the
files it did not read carried exactly the shape it exists to ban, including two
`.evid/` scripts whose construction defaults to `"${TMPDIR:-/tmp}/…"`: the very
landing spot Rule 22 forbids.

## The census, re-derived (Rule 38: generator + commit)

**Generator** — the parent doc's own command, with the language-specific
vocabulary the gate itself uses (`mktemp` for shell, `mkdtemp`/`mkstemp`/
`os.tempdir(` for Python), run over the git **INDEX** (so an untracked copy is
not a member), one `</dev/null` on every read inside the loop so a helper cannot
swallow the loop's own stdin — the bug that made the parent's first count print
ten instead of eleven):

```bash
REV=c30f666ea            # the base commit this change started from
git ls-tree -r --name-only "$REV" | grep -E '\.(sh|py)$' > /tmp/revfiles.txt
while IFS= read -r f <&3; do
  body="$(git show "$REV:$f" 2>/dev/null </dev/null)"
  case "$f" in
    *.sh) printf '%s\n' "$body" | grep -qE '^[^#]*\bmktemp\b' || continue ;;
    *.py) printf '%s\n' "$body" | grep -qE '^[^#]*(mkdtemp|mkstemp|os\.tempdir\()' || continue ;;
  esac
  printf '%s\n' "$body" | grep -qE 'tmp-ban\.sh|tmp_ban' && continue
  echo "$f"
done 3< /tmp/revfiles.txt
```

**Measured at `c30f666ea`: 363 tracked `.sh`/`.py`, TWELVE members of the shape.**
(Not eleven: the parent's count was taken before its own branch landed the third
gate fixture, `internal/testutil/testdata/scratch-gate/chat_replay_xtest-unfixed.sh`
— input data, deliberately red.) After this change the same generator prints ONE
(that same fixture), i.e. eleven repaired, one kept on purpose.

### The per-file verdicts

| file | verdict | reason |
|---|---|---|
| `.evid/demo-elsewhere.sh` | **offender → repaired** | `mktemp -d "${TMPDIR:-/tmp}/msl-demo.XXXXXX"`; now `. tmp-ban.sh` + `gopi_scratch_mkdir` |
| `.evid/demo-logpath.sh` | **offender → repaired** | same construction (device id of `/tmp` vs `/` measured: 2051 both — the ban's exact target) |
| `.evid/demo-serve-lock-refusal.sh` | **offender → repaired** | `mktemp -d "$ROOT/slref.XXXXXX"`, `$2` defaulting to a spelled path, never measured |
| `.evid/serve-lock-reland-rule31-20260923.sh` | **offender → repaired** | same; its child env also passed `TMPDIR="$ROOT"` and a Python fallback `/home/user/workspace/_tmp` |
| `.evid/redproof.sh` | **offender → repaired** | `export GOPI_MEMORY_DB=/tmp/redproof2/mem.db` (a bare `/tmp` LITERAL, not `mktemp`) — caught by the gate's `tmpPathPrefix` rule |
| `docs/archive/demo/close-loop/build.sh` | **offender → repaired** | `OUT="${1:-$(mktemp -d "${TMPDIR:-/tmp}/…")}"`; now routes, and the `$1` branch is preserved |
| `docs/archive/demo/final/build.sh` | **offender → repaired** | a hand-rolled device-id check then `mktemp -d "$L1_PIXEL_TMPROOT/…"`; now `gopi_scratch_mkdir`, which does that check in ONE place |
| `docs/archive/demo/replay/appendix/pipeline-text2img/gen-replay.sh` | **offender → repaired** | an unbounded `while True` render staircase (the gate's other rule) — bounded by `RENDER_ROUNDS_MAX = MAX_SIZE / STEP + 1` |
| `docs/archive/demo/replay/test-verify-frames.py` | **offender → repaired** | bare `tempfile.mkdtemp(prefix="verify-frames-neg-")` |
| `tasks/shot-reclaim-trigger-evidence/trigger-e2e.sh` | **offender → repaired** | `mktemp -d /home/user/workspace/_tmp/…` — a *pinned* path, but still unmeasured: a spelled path is not evidence it is off `/` |
| `text2img-eval/text2img-ab.py` | **offender → repaired** | `os.path.join(tempfile.mkdtemp(), 'page.png')` |
| `tools/docclass/classify_test.py` | **offender → repaired** | two `tempfile.mkdtemp` fixture roots |
| `tools/gif-verify.py` | **offender → repaired** | `tempfile.mkdtemp(prefix="gifverify-")` |
| `testsuite/resume_acceptance.sh` | **offender → repaired** | `worker --resume --worktree /tmp/definitely-absent-dir` |
| `internal/testutil/testdata/scratch-gate/chat_replay_xtest-unfixed.sh` | **member, KEPT** | the gate's own regression fixture; it exists to be red |
| `plugins/**` (16 files) | **NOT a member of the widened scope** | measured, not assumed — see below |

`plugins/websearch/remotebox/{ab-alternate.sh, ab-intermittency.sh,
google-cdp-watchdog.sh, google_cdp_server.py, restart_stack.sh, start_chrome.sh}`
are the reds widening to `plugins/` WOULD add (6 files, 19 sites). They are left
out ON THE EVIDENCE: they are the deployed search-bot service's own
restart/dashboard scripts and its CDP server, they run on the REMOTE host against
that host's temp root, `google-cdp-watchdog.sh`'s `while true` is the watchdog's
job rather than an unbounded loop, and the plugin's four `*_search.py` files
build a shell `mktemp` inside a string that is executed on that remote box. The
repo's Rule 22 policy is about gopi's own process tree on this machine; a service
we do not run here is not audited by a gate that runs in our suite. Recorded
rather than silently dropped — if the plugin ever runs locally, that is a change
with its own review.

## What the change does

### 1. The roots are named constants and the list is one construction

`auditedScriptRoots` is now assembled from two exported-to-the-file groups so a
root cannot be added to one and forgotten by the walk (Rule 14 — no second copy):

- `auditedScratchRoots` — `scripts/`, `devdoc/`, `testdata/` (as before) plus
  `.evid/`, `.evid2/`, `.evid4/`: the directories whose files exist FOR their
  scratch behaviour;
- `auditedDriverRoots` — `docs/`, `tasks/`, `tools/`, `testsuite/`,
  `text2img-eval/`: the remaining directories that ship a committed driver.

Each directory name is a named constant (Rule 1b/13): the value decides where an
audit looks. `archive/` stays out (a former implementation that is not built and
not run), and `plugins/` is excluded with the reason above.

### 2. The non-vacuous-green proof — a count cannot tell "clean" from "did not look"

`assertScopeCoversTheTrackedTree` runs INSIDE the gate and compares the walk
against an independent enumeration, `git ls-files` (the INDEX, Rule 38), then:

- prints `scope: N of M tracked .sh/.py collected (P.P%), K outside the audited
  roots` and lists the residue — so an excluded tree is reported, not hidden;
- prints the **per-root** file count for every audited root;
- **fails** if any audited root contributed ZERO files (a root that scans nothing
  is a typo or a scope that no longer exists);
- **fails** on an empty index (`total == 0`), because a zero denominator would
  make the ratio a statement about nothing (Rule 15);
- **fails** below `gateMinTrackedCoveragePermille = 850` (85 %).

Measured on the resulting tree:

```
scope: 341 of 363 tracked .sh/.py collected (93.9%), 22 outside the audited roots
per-root files collected: scripts=195 devdoc=96 testdata=1 .evid=13 .evid2=1
                          .evid4=1 docs=14 tasks=2 tools=15 testsuite=2 text2img-eval=1
```

The residue is exactly the 3 gate fixtures under `internal/testutil/testdata`
(input data), the 2 `internal/tools/testdata` probes, the top-level
`blast-radius.sh` (which ROUTES the helper — excluded by location, not by defect)
and the 16 `plugins/` files. `gateMinScriptFiles` moved 12 → 300 on the same
measurement, its comment naming the figure and the generator.

## The traps found while repairing (each cost a run)

1. **A `trap` REPLACES the previous one.** Five scripts already installed an EXIT
   trap; the scratch cleanup was dropped and the directory survived its own run
   (measured: `queue-close-loop-3846837`, `queue-trigger-e2e-3847577` after the
   script's own cleanup line). Fixed by chaining — `trigger-e2e.sh` now installs
   ONE `cleanup()` function twice.
2. **`$WORK` inside the trap body is expanded at trap TIME.** `trigger-e2e.sh`
   re-exports `GOPI_TEST_SCRATCH_HOME="$WORK/scratch"` for its child, so the
   registry path resolved against the CHILD's home and the parent's record was
   never read — the cleanup silently found nothing. Both paths are captured the
   moment they are still the same.
3. **The archive move left a stale repo-root depth.** `docs/archive/demo/…` sits
   FOUR levels below the root; `close-loop/build.sh` and `final/build.sh` both
   still computed `../../..` and had been resolving to `<root>/docs` since the
   2026-10-03 archive move. Found by RUNNING the script (Rule 31), not by reading
   it: the routed helper reported the missing path by name.
4. **`plugins/websearch/scripts/{google,github,wiki}_search.py` carry a shell
   `mktemp` inside a Python string**, so a shell-only vocabulary misses them and
   a Python-only one misses `.evid`'s bash — which is why the census above is
   per-language rather than "grep mktemp".

## Verification (what was actually RUN)

- `go test -count=1 ./internal/testutil/ -run TestScriptsAreBoundedAndOffTmp` on
  the widened tree → **ok** (and, run with `plugins/` temporarily added, it named
  the 6 files / 19 sites above; reverted).
- `go test -count=1 ./internal/testutil/` (whole package) → **ok**, 99.6 s.
- `go test -count=1 ./internal/testutil/ ./internal/standards/ -run
  'TestScriptsAreBoundedAndOffTmp|TestTestDBScaffold…|TestRule27|TestCensusGate|TestRepoHygiene'`
  → **ok** (21 PASS), `internal/standards` ok.
- Red proof unweakened: `-run
  'TestScratchRoutingRegressionFixturesFail|TestOldPredicateWouldPassTheOffenders|TestCompliantScratchRoutingStaysGreen|TestCompoundLineRoutingIsSeen'`
  → all **PASS**, the three offender fixtures still red by name and line.
- `go build ./...` → clean; `bash -n` on every touched `.sh` → clean; `ast.parse`
  on every touched `.py` → clean; `python3 tools/docclass/classify_test.py` → 14
  tests OK; `python3 text2img-eval/text2img-ab.py --help` → usage printed.
- Executed surfaces (Rule 31), each with the routing this change installs:
  `.evid/serve-lock-reland-rule31-20260923.sh bin/gopi` (refused double-serve as
  designed, exit 1); `docs/archive/demo/close-loop/build.sh` (ran to the retired
  `gopi capture` step, exit 1 — and left NO residue after the trap chain);
  `tasks/shot-reclaim-trigger-evidence/trigger-e2e.sh none bin/gopi` (exit 0, the
  leak/live probes printed, no stray `gopi-shot` unit, NO residue);
  `gopi_scratch_mkdir` under `GOPI_TEST_SCRATCH_HOME` → device id 2052 vs `/` 2051
  with `gopi_assert_off_root_fs` PASS.
- `docs/archive/demo/final/build.sh` now fails on a MISSING screenshot rather
  than on `$TMPDIR` — i.e. it got past the scratch setup the change rewrote.
