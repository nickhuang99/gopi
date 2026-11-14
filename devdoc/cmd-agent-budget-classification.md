<!-- doc-class: normative -->
# Making the `./internal/docclass/ ./cmd/agent/` scope fit its 25m cap — the classification recipe (2026-10-15)

**Task** `01a13cd2-8db9-7966-9056-899a515261e8` · **branch** `task/01a13cd28db979669056899a515261e8`
· **worktree** `/home/user/workspace/gopi-01a13cd28db979669056899a515261e8` (the harness-created one).
**Host** `peerbox`, 4 cores. **No load of any kind was generated** (Rule 27): every number here is
`go test -json` wall clock on an ambient host.

The standing condition this closes: the scope's four most recent runs were all non-green, the latest
ending `{"Action":"fail","Package":"github.com/nick/gopi/cmd/agent","Elapsed":1506.26}` against the
script's own `-timeout 25m` = 1500 s, with 155 of 2812 top-level arms never terminated.

---

## 1. The recipe (this is the reusable part)

```bash
# 1. FIND THE STREAM of a real run of THIS scope (the script writes one per run).
ls -t /home/user/workspace/gopi-test-scratch/*/queue/test-budget/run-*.json | head
R=$(ls -t … | head -1); head -3 "$R"-start.txt          # host + load + budget, stamped before go test

# 2. READ THE COST out of the stream — never out of the test names, never out of a feeling.
#    a pass record's Elapsed is the test's own wall clock; '/' in Test = a subtest.
python3 -c …   # the 30-line derivation: pass → {Test: Elapsed}, filter '/' out, sort desc
#    (the generator used here is quoted in §3's head; it is 12 lines and re-runs anywhere)

# 3. LET THE REPO NAME THE GAP — the drift report compares the same stream with the inventory.
scripts/heavy-drift-report.sh "$R"
#   before: "57 test(s) at or above it, 57 without a heavy-tests.tsv row"
#   after : "57 test(s) at or above it, 20 without a heavy-tests.tsv row"   ← the diff is the work

# 4. CLASSIFY at the call site, as the FIRST statement of the body, criterion from the constants.
heavytest.Require(t, heavytest.CriterionDeviceExec)   # real child process end to end
heavytest.Require(t, heavytest.CriterionDiskFixture)  # minutes of real per-fixture device work
# 5. ADD ONE ROW per call site to testdata/heavy-tests.tsv: test<TAB>criterion<TAB>source
# 6. RUN THE GATES (see §4). The inventory gate is bidirectional and will name a miss.
```

**Floor**: 5 s per test — `scripts/heavy-drift-report.sh`'s own floor. Below it, classifying inflates
a reviewed list and contradicts the derivation; above it, the report names the test until someone
either classifies it or records why it must stay (`devdoc/cmd-agent-package-budget.md` §3(d) is the
precedent for the "recorded, not classified" half).

## 2. What the numbers looked like here

From `run-1792019037-1777144.json` (the run that hit the cap): 2736 top-level tests terminated PASS,
Σ 1550.9 s; **57 tests ≥ 5 s holding 994.4 s** (64 %); 37 of those (Σ **854.9 s**, 57 % of the
package wall) classified `DeviceExec` ×36 / `DiskFixture` ×1; 20 (Σ 139.5 s) left on the smoke path.

The four largest are one family — the version-stamp fixtures (`git init`+`worktree add`, then **two
`go build`** runs of the real binary each): 127.20 + 104.93 + 76.31 + 69.53 = **377.97 s**. The
DocCheck family (41.02 / 35.18 / 26.05 s) is a fixture tree whose cost is dominated by host-condition
device work. So the finding Rule 27 asks for is not "one hot function": the suite is *many medium
fixtures plus four large externals*, which is exactly the shape `heavytest` exists to move.

## 3. The trap: a registered FLAKE may not be classified

`internal/standards TestFlakeRegistryMatchesTheTestsItNames` refuses a test that is both in
`testdata/flakes.tsv` and `heavytest.Require`d, by name and with the reason:

```
flake_registry_gate_test.go:215: testdata/flakes.tsv:47 registers TestWorkerBusyRoundResetsIdleStreak,
which also classifies itself heavytest.Require — a flake is a DEFECT, not a heaviness class: a skip
reads as a passing green and hides exactly the defect this registry documents
(fix the flake or remove the classification; never both)
```

Observed RED first, reverted in its own commit, GREEN after. A drift report keeps naming such a test —
that is the registry's visibility working, not a missed classification. **Check the flake registry
before classifying** (`grep -F '<Test>' testdata/flakes.tsv`).

## 4. The gates, and the two halves of the proof

```bash
go test ./internal/standards/ -run 'TestHeavyTestInventoryMatchesItsCallSites|TestFlakeRegistryMatchesTheTestsItNames'
go test ./internal/testutil/  -run TestHeavyTestInventoryMatchesItsCallSites   # VACUOUS here: the
#   name lives in internal/standards → "[no tests to run]" is not evidence (the briefing's own
#   command is wrong on that point; use internal/standards)

# HALF 1 — default path (what a merge/verify run executes): the classified tests SKIP, saying so
go test ./cmd/agent/ -run '^(TestSearchWrongGroundExitCodeOnTheRealBinary|TestAliasRefusalLeavesOrdinaryUnknownWordsAlone)$' -count=1 -v
#   --- SKIP: … (0.00s)  ×2      ok  ...cmd/agent  1.088s
# HALF 2 — declared path: the SAME two tests RUN and PASS
GOPI_HUMAN_TERMINAL=1 GOPI_HEAVY_TESTS=1 go test ./cmd/agent/ -run '<same two>' -count=1 -v
#   HEAVY TESTS REFUSED: GOPI_HUMAN_TERMINAL is declared, so the inherited work signature is stepped
#   over and the heavy set RUNS.
#   --- PASS: … (9.72s)  --- PASS: … (6.43s)      ok  ...cmd/agent  20.820s
```

`GOPI_HUMAN_TERMINAL=1` is required on the declared path when the run has an agent/work signature
above it (`internal/heavytest/boundary.go`): the opt-in alone does not entitle the process, and the
refusal is LOUD rather than silent. Two more counter-intuitive notes:

- **`go test ./cmd/agent/ -run 'TestHeavy'` matches nothing** — there is no test named `TestHeavy*`
  in this repo; the heavy surface is the inventory + the gate above, not a test-name prefix.
- **Skipping is not deleting**: the classified tests stay COMPILED and type-checked (that is why the
  mechanism is an in-body `Require`, not a build tag) and every one of them still runs under the
  opt-in. A skip prints a greppable line and appends to `GOPI_HEAVY_SKIP_LEDGER`.

## 5. What this branch did NOT do

No budget constant touched (`scripts/test-budget.sh test_budget="25m"` is the single source; this
branch's diff does not contain that file). No test deleted. The 20 remaining ≥5 s tests are
**accepted with reasons** (gates that must survive on the smoke path, or fixtures under the family
already moved) — full list in the task report, §7. The second wave is deliberately deferred to a run
of the post-classification stream, because classifying from the pre-classification stream would be
classifying against a stale list.

## References

- `devdoc/cmd-agent-package-budget.md` — the 2026-09-21 measurement on `hostb-Laptop` (447 s /
  382 s whole-package, 20 classified), and the M8 hazard (`§7b`: this suite's in-process
  own-worktree-release can free the DEVELOPER's live worktree mid-run — keep deliverable work
  committed).
- `internal/standards/heavy_inventory_gate_test.go` — the gate that reads both directions
  (call site → row, row → call site) and refuses a gate package classified heavy.
- `scripts/heavy-drift-report.sh` / `scripts/heavy-derive.sh` — the readers; the latter refuses a
  narrowed (single-scope) stream on purpose.
