<!-- doc-class: historical -->
# Hardcode audit — the tree is not bound to one operator

**Date:** 2026-10-19 · **Branch:** master · **Cut point measured against:** `68d2605`
**Detector:** `scripts/hardcode-audit.sh` (+ `scripts/hardcode-audit-baseline.tsv`)

## What was asked

Find the literals that bind GoPi to ONE operator or ONE machine instead of deriving
the value from configuration: absolute personal paths (`/home/<user>`), personal and
machine identifiers (`hostb`, `peerbox`, `nick@`), private LAN addresses, embedded
secrets, and machine-specific fallback defaults in shipped code. Fix the real
defects, ship the detector so the review is repeatable, and report.

## The method, and why the classification is the whole report

A grep for `/home/user` returns 382 hits and tells you nothing: most are a comment
citing an evidence path, or a test fixture that is *allowed* to name a machine. The
defect is a value the **shipped code actually consumes**. So the detector answers
two orthogonal questions per hit:

| dimension | values | meaning |
|---|---|---|
| file class | `PROD` / `TEST` / `DOC` | who runs this file |
| line kind | `CODE` / `COMMENT` / `PROSE` | can the software consume this value |

`PROD` + `CODE` is the gate. `PROD` + `COMMENT` is prose that happens to live in a
source file — reported, counted, never a defect by itself. `TEST` may name a machine.
`DOC` is prose by definition.

The `CODE`/`COMMENT` split is decided **at the hit's own column**, not from the
line's first two characters: a trailing `x = 1 // see /home/u` is a COMMENT hit. The
scanner tracks string literals (so `"http://…"` is not prose) and block-comment state
across lines. `--self-check` proves all six verdicts on a throwaway fixture in under a
second — 12 assertions, including that **`127.0.0.1` and a neutral CI home
(`/home/runner`) are NOT reported**.

Two false-positive classes the self-check and the first census forced out, both worth
stating because each would have gotten the tool switched off:

- `$WORK/home/x` — `/home` is only absolute at a **token boundary**; a scratch tree's
  own subtree spelled relative to a variable is not an operator's home.
- `rework-backup/task-01a0ff…` — an `sk-` **prefix must be token-initial**; the tail
  of a task id is not an OpenAI key. Likewise the repo's own CLI verbs
  (`sk-ledger-archive`, `sk-self-reclaim`) are hyphen-separated short words, so the
  secret patterns demand a long contiguous payload on purpose.

## Census (baseline `68d2605`, 6245 tracked files, 3063 candidates, ~15 s)

| file class | line kind | hits |
|---|---|---:|
| PROD | CODE | **89** |
| PROD | COMMENT | 356 |
| PROD | PRIVATE_IP | CODE 1 / COMMENT 10 |
| TEST | CODE | 650 |
| TEST | COMMENT | 272 |
| DOC | PROSE | 8564 |

`TEST`+`CODE` is 650, not 645: 645 path/address hits **plus 5 `SECRET_INLINE`**
(`secret = "`/`token = "` in fixture literals — recorded as a token class of its own so
a credential-shaped literal is never confused with a path). Counted separately and
summed here because `--table` prints one row per token class.

All 89 `PROD`+`CODE` hits are adjudicated in `scripts/hardcode-audit-baseline.tsv`
with a reason per row; the gate is green and **byte-identical across two runs**
(verified by `diff`). Exit 0 = every PROD+CODE hardcode is a reviewed one; exit 1 = a
NEW one shipped. Exit 2 is an environment error, never a silent pass.

The gate's own red-proof, run against this tree: writing one new shipped literal
(`internal/scratch/…:const zzProbe = "/home/newoperator/secret"`) turned the run from
exit 0 to **exit 1**, naming exactly that hit; removing the file returned it to 0. A
gate that has never been seen to fail is not known to be a gate.

The detector excludes ITSELF (and its baseline) from the scan: the pattern table is
the tool's vocabulary, and `words a linter searches for` is not a hardcode — reporting
it would make the tool permanently red on its own source.

## What was fixed — each a real behavior change

**1. `internal/sessionlayout/sibling_families.go` — the registry spelled a hostname.**
The `backups/` family table carried the literal prefix `memory.db.peerbox-backup`. That
classifies the snapshot of exactly ONE machine: the same hand-copy made from any other
host reached the gate as `UNCLASSIFIED` — the state the gate exists to report,
produced by the gate's own table. The `-backup` **shape** is what the family is about,
so the host left the code and the shape stayed: `memory.db.<anything>-backup…`.
Regression: `internal/sessionlayout/sibling_host_snapshot_test.go` — fails before
(`memory.db.otherhost-backup` unclassified), passes after.

**2. `internal/tools/task_hermes_host_warning.go` — Rule 40's owner was welded.**
The owning host was a bare constant, so the "file it on HP" warning was unconditional
for every other fleet: on their own owner box the submit still warned. The measured
name is now a **default behind a knob** (`GOPI_HERMES_OWNER_HOST`, read through one
accessor `taskHermesOwnerHost()`), and the decision is reachable as
`taskHermesHostWarningForOwner(repo, host, owner)` so a test can prove the knob
changes the answer. Regression: `TestHermesOwnerHostIsDeployableNotWelded`. The
existing one-definition-site gate was updated to name the new constant, so the literal
still lives at exactly one site and cannot be deleted to satisfy the gate.

**3. Harness scripts that could only run on this machine.** The default checkout path
was `/home/user/work/GoPi` in `scripts/git-hooks/post-receive` and `blast-radius.sh`;
`scripts/human-ui/web-only.sh` hardcoded a worktree UUID
(`gopi-01a0efbd…`); `scripts/pre-merge-safety-check.sh` read
`os.path.expanduser("/home/user/.gopi")` — which **returns the literal path**, so the
"fallback" never fell back to the caller's home; `scripts/parent-gate-ab.sh` pinned
`TMPDIR=/home/user/workspace/_tmp`. All now derive from the checkout
(`git rev-parse --show-toplevel`, `$(dirname "$0")`, `$HOME`) and stay overridable.
The `pre-merge-safety-check.sh` fix is the sharpest of these: `expanduser` on an
already-absolute path is a no-op, so the old line was not a default at all.

**4. Documentation defaults a reader would copy.** `internal/config/settings.go` named
`/home/user/.doubao/apikey.txt` as the credential default in two shipped descriptions
and `/home/user/.gopi/.do_settings.json` in a third. These are help text, not program
flow — fixed because a reader copying the default gets another operator's home — and
recorded here as *documentation* fixes, not behavior changes. The rest of the
`PROD`+`COMMENT` population (356 hits) is prose and is left standing: it is history,
and rewriting it would be churn without a defect.

## What is deliberately NOT fixed

`scripts/hp/*`, `scripts/notify/*`, `scripts/voice-e2e-hp.sh`, `scripts/e2e-task-merge.sh`,
`scripts/human-ui/compare-terminals.sh`, `scripts/lib/tmp_ban.py`, `scripts/materials/*`,
`scripts/mine_out.json`, `scripts/demo-memory-loop.sh`, `scripts/replay-check.sh` and
the remaining operator harness scripts carry `PROD`+`CODE` hits that are **the subject
being measured**, not a binding: `hp` and `peerbox` are the two boxes the fleet tooling
drives; `mine_out.json` is a recorded CLI corpus; the demo replays a recorded session
file. Each is listed with its reason in the baseline, so the adjudication is data a
reviewer can argue with — not a decision buried in this prose.

**Not in scope, by design, and stated so the gap is visible:** this is a
**class-based** audit. Magic numbers, timeout literals and per-package numeric
constants are a different class with a different (much larger) population and would
not fit one session; they are not covered here.

## Verification

```
bash scripts/hardcode-audit.sh --self-check   # 12 checks, OK
bash scripts/hardcode-audit.sh                # exit 0, 89/89 reviewed, byte-identical across runs
make test-budget PKGS='./internal/sessionlayout ./internal/config ./internal/tools'
                                              # EXIT=2 — 4 failures, NONE attributable to this change
```

**The gate is NOT green, and this report says so rather than burying it.** Four tests
fail in `internal/tools`, none of them in a file this branch touches
(`git diff --name-only 68d2605..HEAD` contains neither
`task_reclaim_self_delivery_test.go` nor `docindex_banner_gap_test.go`):

| failing test | reproduced alone | cause |
|---|---|---|
| `TestSelfDeliveryPushesRemovesAndForceDeletes` | **yes** | `push: ssh: Could not resolve hostname peerfixture` |
| `TestSelfDeliveryFailureKeepsTheRefAndNamesTheTarget` | **yes** | same — the fixture hostname does not resolve |
| `TestSelfDeliveryIsIdempotentForTheWorkerPath` | **yes** | same |
| `TestAssertionTotalityAuditGate` | not re-run (budget) | two assertions in OTHER packages are `UNCLASSIFIED` — a documentation-of-intent debt in its allow-list, not a behaviour break |

The first three reproduce in **4.5 s alone** with no peer run alive, so they are
environment-bound (a `peerfixture` host entry this host does not have), not overlap
noise and not my change. The gate itself flags this hazard: *"a red inside the overlap
window does NOT belong to any one run's change"* — so each red was re-run alone before
being attributed. They are filed as a separate work item; fixing a DNS-dependent
fixture is not this audit's class of defect.

Each fix above ships a minimal repro that fails before and passes after, and the
touched packages run through the repo's declared gate (`make test-budget PKGS=…`), not
a bare `go test`.

### The detector's own report had a defect — found by reading its output

`--table` printed its header on **line 5**: the header was emitted into the same sort
as the data, and `head -1` of a reverse-sorted stream returns a data row. Every verdict
was correct, so the gate stayed green while its table was malformed — the same failure
shape this audit hunts (a check that cannot see its own presentation). Fixed: rows sort
alone, the header is prepended (`93557ab`). This is why the census numbers in the table
above were re-derived from `--all-hits` rather than trusted from `--table`.


## Delta

`git diff --stat 68d2605..HEAD` — **25 files, +845/−42 excluding this report** (26 files / +1024 with it; the report cannot report its own final size without moving it): the detector and its baseline, this
report, 4 behaviour fixes with 2 regression tests, and the config/script unbinding.
