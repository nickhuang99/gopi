<!-- doc-class: historical -->
# Night Test Report: E2E Smoke (m418-yield + m420-rank) — 2026-09-05

**Node:** hostb-Laptop (HP, GLM free window 23:00–09:00)
**Role:** nightly CI on current master. Verify no regressions from today's landings
(memory-aware POC devdoc, merger config-sweep, server worker recycle, TaskSubmit
repo default, worktree backfill, websearch fix).

**Verdict: m420-rank ALL GREEN. m418-yield script is STALE — 3 chained FAILs
caused by a deleted bridge test, not by regressions. No product regressions found.**

## Environment

- Master synced: `git fetch peerbox && git merge --ff-only peerbox/master` → HEAD **2ec33fc**
  ("docs: memory-aware POC verification devdoc" + merger config-sweep, worker
  recycle, TaskSubmit repo default, worktree backfill, websearch fix all present).
- Worktree `~/workspace/night-e2e-20260905` on branch `feat/night-testsuite-e2e`
  (GOPI.md Rule 6; pre-existing WIP stashed, not touched).
- `go build ./...` → **PASS** (no output). Binary `./bin/gopi` rebuilt for the E2E
  harness (`m418YieldGopi=./bin/gopi m418RankGopi=./bin/gopi`).

## Results Summary

| Script | Result | Detail |
|---|---|---|
| `scripts/e2e-m420-rank.sh` | **6/6 PASS** | pure-Go row kind, ai row kind, mixed budget ratio, rank ignored, header echo, header injected |
| `scripts/e2e-m418-yield.sh` | **8/11 PASS, 3 FAIL** | steps 5–7 fail — see root cause |

## m418-yield: what failed and why

- **step 5 "probe cancels stale runner A row (sync-yielded)" — FAIL**: expected
  stale row for A, got `kind=memory still in_progress`, no `sync-yielded` tag.
- **step 6 "runner B row unaffected" — FAIL**: log has no `2.5. m418-yield: B` marker.
- **step 7 "single runner normal behavior" — FAIL**: log has no `3. m418-yield: solo` marker.

**Root cause (evidence-based, not environment):** steps 5–7 run
`go test -run TestM418YieldProbe ./internal/task/…`, but that bridge test was
**deleted** in commit **1060a3b** (2026-09-01, "cancel semantics vetoed by epic")
while `scripts/e2e-m418-yield.sh` (last touched d5c8057) was never updated. On
current master the probe test does not exist:

```
$ git show 1060a3b --stat   # test file removed, "cancel semantics vetoed by epic"
$ grep -rn "TestM418YieldProbe" internal/   # → no matches
```

So `go test -run TestM418YieldProbe` matches zero tests ("no tests to run"), the
probe harness never executes, A's row is never cancelled, and steps 6–7 (which
await the probe's log markers `2.5. …` / `3. …`) time out in a chain. HP
environment is **not** the cause — the script can never pass on current master.

**Classification: stale-script / test-removal drift — NOT a regression.**
Fix (out of scope here, report-only per briefing): either restore the probe
bridge test or update the script's steps 5–7 to the post-veto cancel semantics.

## Env-dependent checks

None needed: m420-rank steps are pure-Go/AI-local; m418-yield failure is
structural (missing test), not token/egress related.

## Verification

- Build: `go build ./...` OK
- m420-rank: 6/6 PASS (`e2e-m420-rank.sh: all 6 checks passed`)
- m418-yield: 8/11, FAIL steps 5–7 (root cause above)
- Report committed on `feat/night-testsuite-e2e`, pushed to remote `peerbox`.

## Deliverable

- Branch: `feat/night-testsuite-e2e` (worktree `~/workspace/night-e2e-20260905`)
- Recovery: `git show feat/night-testsuite-e2e:devdoc/night-testsuite-e2e-20260905.md`
- Pushed: `git push peerbox feat/night-testsuite-e2e` → nick@10.0.0.6:/home/user/work/GoPi
