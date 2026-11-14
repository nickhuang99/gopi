<!-- doc-class: normative -->
# A gate whose subject is not in the checkout: state the precondition, never skip

**Task**: `01a161a7-1f53-7652-a3df-2a91c5036fcc-r1` (re-issue of `01a161a7`)
**Files**: `internal/standards/capdemo_external_gate_test.go`,
`internal/standards/capdemo_external_precondition_test.go`

## What was red

`TestCapdemoExternalBundleAnchorsResolve` ran the publish checkpoint
(`scripts/capdemo-external-check.sh`) over `docs/archive/demo/external/` and
**hard-failed** when that directory was absent. It is absent in every checkout cut
from master: the bundle is run output of tracked generators, untracked by design
(GOPI.md Rule 39; `devdoc/demo-artifacts-untracked.md`), and the snapshot repository
tracks **zero** paths under `docs/`. So the package could not be green without a
`-skip` flag, in any worktree.

## Why "absent" was never the same fact as "deleted"

The old message conflated two states that need opposite verdicts:

| state | correct verdict | why |
|---|---|---|
| this checkout **never carried** the bundle | no subject — green | nothing is missing; the bundle is derivable output of tracked recipes |
| this checkout **carried it and no longer has it** | red | somebody deleted a deliverable; emptying a directory must not turn a red gate green |
| the checkout's history **cannot be read** | red | Rule 15: found-nothing and never-scanned must differ |

The discriminating instrument is the checkout's OWN history, which is one call:

```bash
git log --oneline -1 -- docs/archive/demo/external
```

Empty ⇒ never committed here. Non-empty ⇒ committed here, so its absence now is a
deletion. Nothing is guessed from the directory listing, and nothing is skipped.

## The shape that was rejected

`t.Skip("bundle not present")` — the cheap fix — passes a **deleted** bundle, which
is the exact regression the gate was built to catch (it exists because the bundle's
anchors once went stale silently and nothing failed a build). A skip also cannot be
distinguished from a typo'd path at a glance. Rule 15 forbids both.

## What the change does

* `runCapdemoCheck(t capdemoFataler, root string)` is the gate as a function of a
  root and a reporter, so the regression arms drive the **real** logic.
* Absent bundle → `reportCapdemoPrecondition` decides from history and, in the
  never-carried case, prints on **stdout** (measured: `go test` does not print a
  passing test's `t.Logf`, so a log line would have been invisible):

  ```
  capdemo-external: no subject in this checkout — docs/archive/demo/external is absent
  and no commit in this checkout ever carried it (docs/ is not part of this snapshot;
  the bundle is untracked run output by design). The publish checkpoint has nothing to
  audit here.
  ```
* Present bundle → unchanged: the script runs, its non-zero exit is red with the
  script's own per-claim lines, and an exit 0 without the script's verdict marker is
  still red (anti-vacuous-pass).
* Named constants (Rule 1/13/14): `capdemoAbsentMarker`, `capdemoHistoryTimeout`,
  `capdemoCheckoutHistoryArgs` → `capdemoHistoryArgv`, `capdemoArmStaleStub`,
  `capdemoArmScriptMode`.

## The regression arms (`capdemo_external_precondition_test.go`)

Each builds a three-commit throwaway repository under `t.TempDir()` (Rule 22) and
drives `runCapdemoCheck` with a recording reporter:

| arm | fixture | verdict pinned |
|---|---|---|
| P1 `...HasNoSubjectInACheckoutWithoutTheBundle` | repo, bundle never committed | green **and** the marker printed |
| P2 `...StillFailsWhenTheBundleWasDeleted` | repo, bundle committed then removed from the worktree | red, saying `DELETED` |
| P3 `...RefusesAnUnexplainableAbsence` | not a repository | red, saying the history could not be read |
| P4 `...StillFailsOnStaleAnchors` | bundle **present**, checkpoint reports a stale anchor | red, and the no-subject marker is NOT printed |

P2 is the arm a `t.Skip` would have broken; P4 is the arm that catches someone
later widening the absence rule into "any failure is a precondition".

## Evidence

* before (fresh checkout of the parent commit, zero `docs/`):
  `FAIL capdemo_external_gate_test.go:64: ... which is NOT a pass`
* after (fresh checkout of the branch, zero `docs/`):
  `ok github.com/the operator/gopi/internal/standards` (exit 0), marker printed under `-v`
* real bundle copied in (796 KB, `/home/user/work/GoPi/docs/archive/demo/external`)
  → the real script runs and the gate goes **red** on its real stale anchors
  (`A6`/`A7`: the pinned commit `30f655b1…` does not resolve in that checkout) —
  the original verdict is intact.
