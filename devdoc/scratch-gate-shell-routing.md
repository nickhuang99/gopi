<!-- doc-class: historical -->
# The scratch gate's ROUTING predicate — a mention is not a contract

**Task:** 01a1137f90d07f10a4d41f9227190ceb (2026-10-07) · **Files:**
`internal/testutil/scripts_gate_test.go`,
`internal/testutil/scripts_gate_routing_test.go`,
`internal/testutil/testdata/scratch-gate/`, `scripts/rehome-flat-raw-logs.sh` ·
**Predecessor:** `devdoc/replayplay-scratch-pin-20261007.md` (task 01a11346,
which fixed the offender at its site and was forbidden from touching the gate).

## What was wrong

`assertScratchHelpersAreUsed` decided compliance with

```go
strings.Contains(strings.Join(f.lines, "\n"), filepath.Base(helper)) // ALL lines, comments included
```

so the helper's NAME anywhere in a file was read as the file entering the
contract. Two measured consequences:

1. a script naming `tmp-ban.sh` only in prose (a comment, an `echo`, a
   `[ -r … ]` probe) was compliant while building its own scratch with `mktemp`;
2. a script that ADDED `. lib/tmp-ban.sh` while KEEPING its own `mktemp -d -p`
   was compliant — the brief's step-1..4 repro, whose exit was 0.

Both are false NEGATIVES (an offender passes), which is the opposite of every
other escape in that file: those are reviewed, explicitly reasoned markers.

## What the gate requires now

- **Routing** = a real statement: shell `. ` / `source` whose argument names the
  helper (directly, or through a variable the script assigned the helper's path
  to), Python `from …tmp_ban import …` / `import tmp_ban`. Comments are stripped
  first; a line is split on `;` so a routing statement inside a COMPOUND line
  (`HERE="…"; . "$HERE/lib/tmp-ban.sh"`) routes like one on a line of its own.
- **Own construction** = shell `mktemp`, Python `mkdtemp`/`mkstemp`/`os.tempdir()`,
  or `mkdir` under the AMBIENT temp root. A construction is red on its own when
  the script does not route; and a routed script that still carries one is red
  too, because the helper is the ONE construction (`gopi_scratch_mkdir`).

The command vocabulary is **per-language**: matching `os.tempdir()` inside a
shell script was a false red on `devdoc/daemon-survives-logout-probes/p8-gc-candidate.sh`,
a read-only probe that merely greps for the string. The exemption a routed script
can earn is likewise behavioural, not lexical: `helperDerivedVars` follows the
assignments to a fixed point, so

```bash
SCRATCH="$(gopi_scratch_mkdir smoke)"; export TMPDIR="$SCRATCH/tmp"; mkdir -p "$TMPDIR"
```

(scripts/e2e_smoke.sh, the live compliant form) is not flagged — $TMPDIR was
pointed INTO the helper's own scratch tree. A script that never names the helper
has no derived variable, so `TMPDIR="$(mktemp -d)"` stays red on its own line.

## The offenders this uncovered (tree-wide scan, 2026-10-07)

Scan: every `*.sh`/`*.py` under `scripts/`, `devdoc/`, `testdata/` that routes the
helper while carrying an uncommented `mktemp` → **the fixed offender shape is
gone from the tree**; the two fixtures of it now live in
`internal/testutil/testdata/scratch-gate/` (plus the verbatim UNFIXED
`chat_replay_xtest-unfixed.sh`, taken from the commit before its own fix, so the
record stays in the tree as input data — `internal/` is not an audited root).

Real offenders the tightened gate found and that were repaired in this change:

- `scripts/rehome-flat-raw-logs.sh:92-95` — `INDEX/RAWIDX/LOC/BARE=$(mktemp)`
  four lines above `WORK=$(gopi_scratch_mkdir …)`, which then OVERWROTE all four
  variables (`INDEX="$WORK/refs.txt"` …). Dead assignments whose side effect — a
  temp entry on the ambient temp root, unregistered with the helper — was real
  and leaked. Removed; the script has sourced the helper all along.
- `devdoc/daemon-survives-logout-probes/p8-gc-candidate.sh:14` — a false red
  (a grep pattern, not a construction); fixed by making the vocabulary
  per-language.

Deliberately NOT changed, and why:

- `scripts/e2e_smoke.sh:44` — `TMPDIR="$SCRATCH/tmp"` then `mkdir -p "$TMPDIR"`:
  compliant, see `helperDerivedVars` above.
- `scripts/lib/safe-merge-contract.sh` — states in prose that it is not a
  temp-root channel and creates no file; it carries no construction, so it owes
  the helper nothing.
- `scripts/audit-env-catalog.sh:139` — routes through a variable
  (`TMPBAN=…lib/tmp-ban.sh; . "$TMPBAN"`): compliant.
- `scripts/s3egress_rank.py` — names the helper in a comment and creates nothing.

## Remaining members of the shape — tracked scripts outside the audited roots

The tightened predicate's construction rule (`mktemp` present, no routing
statement anywhere) was re-run over the WHOLE tracked tree, not only the audited
roots. Generator, at commit `e3289e2bf` (`git ls-files '*.sh' '*.py'` — the
INDEX, so an untracked copy is not a member):

```bash
for f in $(git ls-files '*.sh' '*.py'); do
  grep -qE '^[^#]*\bmktemp\b' "$f" || continue
  grep -qE 'tmp-ban\.sh|tmp_ban' "$f" && continue
  echo "C $f"; done
```

It names ELEVEN TRACKED files on master (checked out at this branch: the same
eleven plus the fixture below), all outside `auditedScriptRoots` (`scripts/`,
`devdoc/`, `testdata/`) and therefore never scanned by the gate:

| file:line | construction |
|---|---|
| `.evid/demo-logpath.sh:8` | `mktemp -d "${TMPDIR:-/tmp}/…"` — a `/tmp` default, the very landing spot the ban exists for |
| `.evid/demo-elsewhere.sh:6` | same form |
| `.evid/demo-serve-lock-refusal.sh:15` | `mktemp -d "$ROOT/slref.XXXXXX"` |
| `.evid/serve-lock-reland-rule31-20260923.sh:12` | same form |
| `docs/archive/demo/close-loop/build.sh:20` | `mktemp -d "${TMPDIR:-/tmp}/gopi-close-loop-XXXXXX"` |
| `docs/archive/demo/final/build.sh:394` | `mktemp -d "$L1_PIXEL_TMPROOT/…"` |
| `tasks/shot-reclaim-trigger-evidence/trigger-e2e.sh:26` | `mktemp -d /home/user/workspace/_tmp/…` (pinned, off `/`) |
| `plugins/websearch/scripts/{github_search,web_fetch,wiki_search}.py:59-102` | `a=$(mktemp)` — shell embedded in a `.py` |
| `plugins/websearch/scripts/google_search.py:64` | `t=$(mktemp -d)` |

…plus ONE member that exists only on this branch — the UNFIXED
`internal/testutil/testdata/scratch-gate/chat_replay_xtest-unfixed.sh` — which is
**input data**, not a script anything runs (its own directory is what makes the
regression tests able to keep the red in the tree).

**A count needs the same care as a predicate: the first cut of this scan printed
ten and silently dropped `docs/archive/demo/final/build.sh`, the SIXTH row above.**
The generator ran `git show "master:$f"` INSIDE a `while read` loop fed by
`< <(git ls-tree …)`: `git show` reads the loop's own stdin and swallowed the
next path, so the count depended on buffering. With `</dev/null` on both `git
show` calls it is stable at **eleven on master, twelve in this branch**, each
path in the table re-listed with its construction (Rule 38 — a census figure
names its generator, and a generator whose number moves is a bug in the
generator). The corrected figure is what the dispatched row names.

They are NOT regressions of this change and NOT silenced: the directory list is
the gate's own scope decision, and widening it is a separate change with its own
blast radius (it would redden the tree on the day it lands). Recorded in the task
report, and DISPATCHED as a row of its own rather than left as prose:
`01a11397-ef8e-7da7-b48e-4df209449601`.

## The regression tests (scripts_gate_routing_test.go)

They drive the REAL gate through a `recordingT` (the gate takes a `gateFailer`
interface for exactly this) — never a copy of its predicate, because a test that
re-implements the predicate agrees with the bug it is supposed to catch:

- `TestScratchRoutingRegressionFixturesFail` — three offender fixtures, each
  asserting the gate names that file AND LINE;
- `TestOldPredicateWouldPassTheOffenders` — the replaced substring predicate,
  spelled verbatim, proved to call those offenders compliant;
- `TestCompliantScratchRoutingStaysGreen` — the helper's own generator, the
  $TMPDIR-into-the-tree form, the Python import: all silent;
- `TestCompoundLineRoutingIsSeen` — the `;`-split, found by RUNNING the repro
  rather than reading the code: the brief's own step-3 line is compound, and
  reading only a line's first verb classified the marginal offender as
  "does not route" (the wrong reason) and would have reported a genuinely routed
  one-liner as unrouted.

## Reproduction of the red (observed, transcript in the task report)

1. `git show f34981a6e^:scripts/replay/chat_replay_xtest.sh` → the unfixed script
   (`SB=$(mktemp -d -p "$SCRATCH_PARENT" …)` at its line 66).
2. `bash -n` OK; the gate fails naming `…REDPROBE.sh:66` (no routing).
3. Insert ONE line — `HERE="…"; . "$HERE/../lib/tmp-ban.sh"` — and change nothing
   else; `bash -n` OK.
4. The gate now fails naming `…REDPROBE.sh:67` ("routes … and STILL runs
   `mktemp`") where the pre-change gate exited 0. Step 5: probe removed → green.
