<!-- doc-class: historical -->
# Task Report: the worker-ownership gate no longer under-reports its blast radius

**Branch**: `fix/worker-ownership-gate-full-report` (worktree `~/workspace/gopi-wo-gate-report`, base `30450fc6`).
**Date**: 2026-09-16.
**Pre-existing defect**, observed while fixing task `01a0a58c` on branch
`fix/worker-ownership-pointer-missing` @ `b545c055`; independent of that work.

## The defect

`TestWorkerOwnershipPointerPresent` (`internal/testutil/worker_ownership_gate_test.go`)
capped its failure message's uncovered-file list at
`workerOwnershipMaxReportedHits = 20` and printed a bare `  … (truncated)`.

The cap counted **printed hits**, and nothing stated the real total. So the
message read as a complete list when it was not:

```
files that teach a worker-launch / task-run command without the operator-only pointer:
  briefings/brief-worker-child-stdout-to-session-dir.md
  … (truncated)
```

That is what a briefing author saw. The real uncovered set was **six** files,
so the follow-up fix (task `01a0a58c`, `devdoc/worker-ownership-pointer-regrowth-20260915.md`)
repaired **one sixth** of the defect. The failure mode is structural, not a
miscount: *any* red report from this gate under-stated its blast radius
whenever more than 20 files were uncovered.

## What was implemented

### 1. The message is now built by `workerOwnershipUncoveredReport` (disclosure first)

`internal/testutil/worker_ownership_gate_test.go`:

- `TestWorkerOwnershipPointerPresent` keeps scanning; the rendering moved into
  `workerOwnershipUncoveredReport(missing []string) string` so the message
  contract is directly testable (no tree fixture needed — Rule 10.5's smallest
  sample).
- The message **always** opens with the total:
  `N uncovered file(s) teach a worker-launch / task-run command without the operator-only pointer`.
  The count survives every truncation; the listing below it is a courtesy.
- With more than `workerOwnershipMaxReportedHits` files it adds
  `; listing first 20, N-20 withheld (set GOPI_WORKER_OWNERSHIP_FULL_LIST=1 to list every file)`.
  A complete listing says `; listing all N` and never claims a truncation it
  did not perform (a false "withheld" would be its own lie — pinned by
  `TestWorkerOwnershipReportAtTheCapWithholdsNothing`).
- The literal `… (truncated)` is **gone**: an ellipsis with no count is exactly
  what made the under-report invisible.

### 2. Env escape hatch `GOPI_WORKER_OWNERSHIP_FULL_LIST`

Read via `config.EnvBool` — the repo's ONE boolean env funnel (GOPI.md
Rule 2), not a bare `os.Getenv` compare (Rule 1b). `config` imports nothing
from `testutil`, so no cycle; `internal/testutil` already imports it
(`ledgerpin.go`, `run.go`, `suiteenv.go`). The value is never compared in this
file, so `""` / `"0"` / `"off"` / `"false"` all correctly mean disabled (pinned
by the same test). Rule 21 (env constant needs a real reader) holds by
construction — the constant's only use is as the argument of `config.EnvBool`.

### 3. Minimal repro / regression test

`internal/testutil/worker_ownership_report_test.go` (new):

- `TestWorkerOwnershipReportDisclosesTheTotalWhenTruncated` — 26 uncovered
  files, cap 20: the message must state `26`, the withheld count, the escape
  hatch name, and still list exactly 20.
- `TestWorkerOwnershipReportFullListEscapeHatchPrintsEveryFile` — all 26 with
  the knob on; still capped for `"" / 0 / off / false`.
- `TestWorkerOwnershipReportAtTheCapWithholdsNothing` — the boundary.

**Red-proof (the test really guards the defect).** With the OLD rendering
restored verbatim, the new tests fail for the right reason:

```
--- FAIL: TestWorkerOwnershipReportDisclosesTheTotalWhenTruncated
    the report does not state the TOTAL (26) — a reader can only see the listing:
    the report does not disclose what was withheld (want "listing first 20, 6 withheld"):
--- FAIL: TestWorkerOwnershipReportFullListEscapeHatchPrintsEveryFile
    GOPI_WORKER_OWNERSHIP_FULL_LIST=1 listed 20 files, want every one of 26:
```

### 4. Sibling-cap scan (the briefing asked explicitly)

| Gate | Cap | Was it silently truncating? | Disposition |
|---|---|---|---|
| `worker_ownership_gate_test.go` | `workerOwnershipMaxReportedHits = 20` | **YES** — bare `… (truncated)` after the 20th hit, no total | FIXED here |
| `scripts_gate_test.go` (`gateMaxReportedHits = 20`) | 4 sites: 573/639/709/728 | **WEAKER FORM, same class** — it does not print an ellipsis, but it *suppresses* hits above the cap with no total either, so the printed report cannot be distinguished from a complete one that happens to have ≤20 hits | FIXED here: each site counts `withheld`, and `assertReportedAllHits` states `N offending site(s), M withheld from the listing above (cap 20) — the count above is the full extent`. Contract pinned by `TestGateWithheldHitsMessageDisclosesTheTotal` |
| `internal/standards/` (gates 1–6 + data report) | no per-hit printing cap found | no | checked: rule reporting prints NEW hits (there are 0–2), the data report prints totals, and `baseline.go` lists are per-site — no truncation to disclose |

So the same defect class existed in **one sibling** (the `/tmp` + unbounded-loop
gate). It was not the headline (`scripts/probe-daemon-term.sh` is legitimately
red on master), but it is the identical under-report shape and is fixed in the
same change.

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` — exit 0.
- **Targeted**: `go test ./internal/testutil/ -run 'TestWorkerOwnership|TestGateWithheld' -count=1` — PASS.
- **Repro tests green on the fixed tree**, and red (for the right reason) on the
  restored old rendering — both runs quoted above.
- **End-to-end red-proof against the real tree**: 25 synthetic uncovered docs
  added under `devdoc/` →
  `30 uncovered file(s) …; listing first 20, 10 withheld (set GOPI_WORKER_OWNERSHIP_FULL_LIST=1 to list every file)`,
  and with the knob set `listing all 30` including paths past the cap. Probes
  removed (`git status` shows only the three intended files).
- **Gate still fails on a genuinely uncovered file** — the property the fix
  must not weaken: master (`30450fc6`) reports 5 truly uncovered files and the
  branch reports the same 5, just with the count disclosed:
  `devdoc/daemon-scope-per-profile.md`, `devdoc/nested-report-path-clobber.md`,
  `devdoc/why-worktree-residue.md`, `devdoc/worktree-refusal-arms.md`,
  `scripts/accept-daemon-scope.sh`.
- **`gofmt -l internal/testutil/`** — clean.

### Pre-existing reds (master control, delta zero)

Both were reproduced in a pristine `30450fc6` control worktree — **not caused by
this branch**, and NOT repaired here (they are their own tasks, Rule 26):

1. `go test ./internal/testutil/ -run TestScriptsAreBoundedAndOffTmp` FAIL —
   `scripts/probe-daemon-term.sh creates scratch data but does not source/import
   scripts/lib/tmp-ban.sh` (byte-identical on master; the file arrived with
   `5bc975b4`).
2. `go test ./internal/standards/` FAIL — Rule 17 reports 2 NEW unannotated
   `os.Setenv` sites (`internal/agent/journalreport_test.go:101,104`);
   byte-identical on master.

The remaining uncovered-file set (5 master + 5 that arrived after `b545c055`)
is likewise for the pointer-backfill task that owns it; this change discloses it
instead of hiding it, which is the point.

## Not Done (honest)

- The 5 (now 10) genuinely uncovered files were **not** annotated — out of scope
  for "make the report honest", and the sibling pointer-backfill work owns the
  content fix. After this change the gate reddens with the FULL list, so that
  work can no longer under-scope itself.

## Deliverable declaration (Rule 6d)

- **Branch**: `fix/worker-ownership-gate-full-report` (committed in the shared repo at `~/work/GoPi`; worktree = the branch, no push needed).
- **Recovery**: `git show fix/worker-ownership-gate-full-report:devdoc/worker-ownership-gate-full-report-20260916.md`
  (and `…:internal/testutil/worker_ownership_gate_test.go`).
- Not merged (Rule 6e).
