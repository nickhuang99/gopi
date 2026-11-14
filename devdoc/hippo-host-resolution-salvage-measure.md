<!-- doc-class: historical -->
# Task Report: re-measure `archive/fix/hippo-host-resolution` — SALVAGE, re-derived on a fresh branch

**Tag measured:** `archive/fix/hippo-host-resolution` (tip `564fa6dd189ad4929271ef4fc9a6b03f6bc751f5`, 2026-09-11)
**Audit claim under test:** UNLANDED (`git cherry` +11 / `rev-list` 11)
**Verdict:** **(a) SALVAGE** — the production surface was never carried to master, and master still
carries the very duplication the tag removes. Re-derived and landed on **`fix/hippo-host-resolution`**
(fresh worktree `/home/user/workspace/gopi-hippo-salvage`, branch off master `f69a2149`).

---

## 1. Instruments observed live (this run, verbatim)

```
$ git log --oneline -3
f69a2149 merge: feat/ledger-snapshot (merge-run 01a0b7d2, union-resolved main.go + DOC-INDEX)
6482d360 merge: feat/fd-finalizer-borrowed-sites (merge-run 01a0b7d2, DOC-INDEX union-resolved)
88f66809 merge: feat/spa-cdp-capture (merge-run 01a0b7d2, DOC-INDEX union-resolved)

$ git cherry -v master archive/fix/hippo-host-resolution
+ 5704c98e7d09d4ae0d19aa5a918dc62c74de7931 fix(hippocampus): retire the dead production-organ constructor; single source for the brief's arithmetic
+ 38c72cf9d58d0dccde929b4a51254b39e14840b0 fix(agent): the host delegates the brief's numbers to the organ
+ 3342fe9a29b70a3d53ebadd02b9e16c7debc7402 test(agent): the lead fixtures carry the coverage a real retrieval has
+ 670ce521508a2a6432df6182ad462bfe2685d772 test(hippocampus): cover the surviving arithmetic and prove the floors inert
+ f9fa5dede3c74eff44cb5b1c0aff9b2066ff517f test(agent): exercise the live recall seam's deepseek specifics
+ c5a37121d7a917cdd397dfeb55210b1e0e3b1a1f test(agent): pin the empty-recall reading under the host's append floor
+ 094b658240d181362ed774d26236d8a6dfc99594 docs(devdoc): hippocampus host resolution — decision, evidence, report
+ 5b356398b3913f996062027ba790ac54abe34700 docs(devdoc): correct the report's hash/status section and the §4.4 vs §5 claim
+ 4fb6383b115fe92de967b9c61361fbde1d4c8709 docs(devdoc): drop the self-referential tip hash from the commit listing
+ 150cbe7df6fd06f7836d973f22dff89344df4138 docs(devdoc): fix the duplicated decision line and make the Rule-6d tip self-resolving
+ 564fa6dd189ad4929271ef4fc9a6b03f6bc751f5 docs(devdoc): re-verify the acceptance at the tip and mutation-probe the Rule-13 gate

$ git rev-list --count master..archive/fix/hippo-host-resolution
11

$ git merge-base --is-ancestor archive/fix/hippo-host-resolution master ; echo "exit=$?"
exit=1                      # NOT an ancestor → not landed

$ git merge-base master archive/fix/hippo-host-resolution
b7b8000dd91aed64eea8ee32e503b80101525625

$ git rev-list --count archive/fix/hippo-host-resolution..master     # base drift
3017
```

## 2. Per-symbol CONTENT test (the acceptance's instrument)

`git grep -w <symbol> master -- '*.go'` and `git log -S<symbol> master -- '*.go'`:

| symbol | `git grep -w` on master | `git log -S` on master (`*.go`) | reading |
|---|---|---|---|
| `ClampedBriefConfidence` | **0** | **empty** | never landed, never removed → SALVAGE signal |
| `ClampedBriefMemoryWeight` | **0** | **empty** | never landed, never removed → SALVAGE signal |
| `EffectiveConfidence` | **0** | `6669e413 feat(m2): confidence lifecycle + feedback closure + hot-tier resident injection` — `M internal/tools/memory.go` only | OBSOLETE **as a name**: that commit introduced the *other* symbol `m2EffectiveConfidence` (an M2 lifecycle function in `internal/tools`); the organ-side reader never landed. Its formula still lives inline as `internal/agent/hippocampus_agent.go:349-351` (`if h.EffConfidence > 0`). |
| `TierWeight` | **0** | `76e492bf feat(hippocampus): salvage additive files from feat/hippocampus-host (child 3/4)` — `A internal/hippocampus/brief.go` | OBSOLETE **as a name**: master's `internal/hippocampus/brief.go` landed the same file with an UNEXPORTED `briefTierWeight`. The exported single-source form never landed. |

Honest reading of the two `-S` hits: `-S` searches the whole commit and reports the ones that
moved that string's COUNT. Neither hit is a *production removal* of the tag's symbol — 6669e413
names the substring inside a different symbol (`m2EffectiveConfidence`), and 76e492bf *added* a
file that carries a differently-spelled twin. The acceptance's strict rule (`a -S hit ⇒ OBSOLETE, cite
the sha`) is recorded above with each sha; the per-unit disposition below is the honest refinement,
because both hits are *evidence of duplication*, not of supersession.

### Per-unit disposition

| tag unit | disposition on master | evidence |
|---|---|---|
| `ClampedBriefConfidence` (damped best-lead confidence) | **SALVAGE** — absent; master's live path publishes the UNDAMPED best (`hippoBriefConfidence`, agent) | `git grep -w` = 0, `-S` empty |
| `ClampedBriefMemoryWeight` (tier-normalised mean of the DISPLAYED leads) | **SALVAGE** — absent; master's live path averages `EffectiveConfidence × hippoTierWeight` over **ALL** hits with its own table `hot 1.0 / warm 0.7 / cold 0.3` | `git grep -w` = 0, `-S` empty; `internal/agent/hippocampus_agent.go:70-72,360-373` |
| `EffectiveConfidence` reader | **SALVAGE** (as `hippocampus.EffectiveConfidence`) — the reader is *inlined* in the agent; the organ-side single home never landed | `-S` hit 6669e413 is `m2EffectiveConfidence`, a different symbol |
| `TierWeight` (exported, case/space tolerant, `tools.TierHot/Cold`) | **SALVAGE** — master has the unexported `briefTierWeight`; the exported single-source form (Rule 14) never landed | `-S` hit 76e492bf added `brief.go` with `briefTierWeight` |
| `BriefTopHits` / `BriefMaxBodyChars` / `BriefLeadKeyPrefix` / `BriefLeadKeyDelimiter` (exported) | **SALVAGE** — master carries `briefTopHits`/`briefMaxBodyChars` unexported **plus a second host copy** `hippoSummaryMaxHits = 3`, `hippoSummaryMaxChars = 1200`, `hippoLeadKeyNamespace = "lead:"`, `hippoLeadDelimiter = "\n"` | `git grep -n 'BriefTopHits' master` = 0 |
| `BriefFromResult`'s `MemoryWeight` over displayed leads | **SALVAGE** (partly) — master's `BriefFromResult` averages over ALL hits and applies a token-coverage multiplier | `internal/hippocampus/brief.go:172` (master) |
| `host.go` retirement of `NewHostEngine` + the two floors | **OBSOLETE — deliberately NOT landed** | master's `NewHostEngine`/`HostEngineClientTimeout`/`HostEngineCycleTimeout` came from **`1f68439f` "fix(hippocampus): bind the live organ to NewHostEngine (host floors + reply-shape retry)"**, 2026-09-11 02:30 — **after** the tag's base `b7b8000d` (2026-09-11 00:41); `git merge-base --is-ancestor 1f68439f b7b8000d` = NO, `--is-ancestor b7b8000d 1f68439f` = YES. Master's `host_test.go` has three tests asserting it works. The tag's deadline test proved the floors **inert**, not one thing master needed removed. |

**Why the surface is SALVAGE and not an obsolete name-list** — the content, not the audit's names:
master's live path (`internal/agent/hippocampus_agent.go`, from `ebfc6e87`) still carries a **second,
forked copy** of exactly the arithmetic the tag unified (its own tier table with *different* values
1.0/0.7/0.3, its own `hippoHitConfidence`, its own `clampHippoUnit`, an undamped confidence, a
weight over all hits) — while master's `internal/hippocampus` reads `Confidence` (stored) rather than
`EffConfidence` (what the model is shown). The "two derivations, one live path" defect the tag closes
is **live on master today**.

## 3. Red-then-green, from the tag's OWN tests (unmodified assertions, sha256-verified)

Transplant order: the tag's test file(s) written **verbatim** (`git show <tag>:<path>`, sha256 below)
onto a throwaway master worktree, with a **test-file-only shim** supplying the four exported
constants (so the transplant measures the *production surface*, not a compile error on constants);
then the same files on the branch.

| file | sha256 (identical in BOTH worktrees) |
|---|---|
| `internal/hippocampus/brief_test.go` | `1f8c7be4b231bf71bcce2b5e3b8159377f71397ad7af2754134b296a59d457bf` |
| `internal/hippocampus/host_deadline_test.go` | `8aa99ca56a1199a7ac38bc071e8518754d0b5e75a277e3ba30c7fd6608c6888b` |
| `internal/agent/hippocampus_seam_test.go` | `6b699b2de97832349e22cca618e4d635cc54a3627b6b9fb66c628b462d9b986e` |

**RED** — throwaway master worktree `/home/user/workspace/gopi-hippo-salvage-base` (detached `f69a2149`),
tag's test files + shim:

```
$ go test -run 'TestClampedBrief|TestTierWeight|TestHostDeadline|TestRetiredFloors' ./internal/hippocampus/
# github.com/nick/gopi/internal/hippocampus [github.com/nick/gopi/internal/hippocampus.test]
internal/hippocampus/brief_test.go:39:9: undefined: ClampedBriefConfidence
internal/hippocampus/brief_test.go:52:9: undefined: ClampedBriefConfidence
internal/hippocampus/brief_test.go:59:9: undefined: ClampedBriefConfidence
internal/hippocampus/brief_test.go:76:12: undefined: ClampedBriefConfidence
internal/hippocampus/brief_test.go:79:12: undefined: ClampedBriefConfidence
internal/hippocampus/brief_test.go:89:12: undefined: EffectiveConfidence
internal/hippocampus/brief_test.go:93:12: undefined: EffectiveConfidence
internal/hippocampus/brief_test.go:96:12: undefined: ClampedBriefConfidence
internal/hippocampus/brief_test.go:104:12: undefined: ClampedBriefConfidence
internal/hippocampus/brief_test.go:114:13: undefined: ClampedBriefMemoryWeight
FAIL	github.com/nick/gopi/internal/hippocampus [build failed]

$ go test -run 'TestHippocampusSeam|TestHippocampusRecallFunc_Delegates|TestEmptyRecallStaysUnder|TestHostSideArithmeticRetiredGate' ./internal/agent/
# github.com/nick/gopi/internal/agent [github.com/nick/gopi/internal/agent.test]
internal/agent/hippocampus_seam_test.go:294:25: undefined: hippocampus.ClampedBriefConfidence
internal/agent/hippocampus_seam_test.go:297:25: undefined: hippocampus.ClampedBriefMemoryWeight
internal/agent/hippocampus_seam_test.go:315:23: undefined: hippocampus.ClampedBriefConfidence
FAIL	github.com/nick/gopi/internal/agent [build failed]
```

**Honest attribution of `host_deadline_test.go`** (it governs a retired surface, so it must NOT be
claimed as red for the tag's *whole* change): run ALONE on master, with the tag's `brief_test.go`
temporarily moved aside, it is **GREEN**:

```
$ go test -run 'TestHostDeadline|TestRetiredFloors' ./internal/hippocampus/      # master, brief_test.go moved aside
ok  	github.com/nick/gopi/internal/hippocampus	2.580s
```

So its red in the combined run is a **build-error artifact** of the missing `ClampedBrief*`, not an
independent failure. It is therefore **NOT transplanted** — master's `NewHostEngine` + floors are live
(§2) and deleting them would break master's own `host_test.go`.

**GREEN** — deliverable worktree, same files, same sha256:

```
$ go test -count=1 -run 'TestClampedBriefConfidence|TestClampedBriefMemoryWeight|TestTierWeight|TestHostDeadlineGoverns|TestRetiredFloorsExceed' ./internal/hippocampus/
ok  	github.com/nick/gopi/internal/hippocampus	2.343s

$ go test -count=1 -run 'TestHippocampusSeam|TestHippocampusRecallFunc_Delegates|TestEmptyRecallStaysUnder|TestHostSideArithmeticRetiredGate' ./internal/agent/
ok  	github.com/nick/gopi/internal/agent	15.412s
```

All assertions are the tag's own, unmodified (sha256 above is the proof of "verbatim").

## 4. Re-derivation against master's CURRENT file shapes

The tag's change was written against `b7b8000d`; master has drifted 3017 commits, so the surface was
re-derived, not copied:

- **`internal/hippocampus/brief.go`** — added `TierWeight` / `EffectiveConfidence` /
  `ClampedBriefConfidence` / `ClampedBriefMemoryWeight` (the tag's bodies, verbatim arithmetic) and the
  exported constants `BriefTopHits` / `BriefMaxBodyChars` / `BriefLeadKeyPrefix` / `BriefLeadKeyDelimiter`,
  **each defined as an alias of master's existing unexported constant** (Rule 13/14: one value, one
  definition — `BriefTopHits = briefTopHits`, `BriefMaxBodyChars = briefMaxBodyChars`, …). master's
  `briefTierWeight` was replaced by the exported `TierWeight` (identical switch, now reading
  `tools.TierHot` / `tools.TierCold` instead of the local text literals — a Rule 1/14 improvement the
  tag already carried); master's `briefHitWeight` now reads `EffectiveConfidence(h) * TierWeight(h.Tier)`
  (the tag deleted it, but master's `brief_test.go` covers the coverage-shape through `BriefFromResult`,
  so it is kept as the per-lead term). **`BriefFromResult` now DELEGATES both numbers** to
  `ClampedBriefConfidence` / `ClampedBriefMemoryWeight` (instead of master's inline damped formula and
  all-hits mean) — this is the tag's `Brief`-side half of "one derivation".
- **`internal/agent/hippocampus_agent.go`** — `hippoBriefConfidence` / `hippoBriefMemoryWeight` are now
  one-line delegations to the organ's functions; the host's duplicate arithmetic (`hippoTierWeight*`
  table, `hippoHitConfidence`, `clampHippoUnit`) was **deleted** (the tag's `TestHostSideArithmeticRetiredGate`
  asserts exactly that), and the 1200/3/`lead:`/`"\n"` literals now reference the organ's exported
  constants.
- **Deliberately NOT carried** (master-owned, per §2): the deletion of `NewHostEngine` +
  `HostEngineClientTimeout` + `HostEngineCycleTimeout`; the tag's `host_deadline_test.go`; and the tag's
  whole-file rewrite of `brief_test.go` (master's `brief_test.go` / `cov_hippocampus_test.go` legitimately
  cover `BriefFromResult`, `briefHitWeight`, `clampUnit` — replacing them would delete live coverage).
- **Also not carried:** the tag's one-line `Tokens: 1` fixture edit in `hippocampus_display_identity_test.go`.
  It is a *correction* made necessary only by the damping the tag introduced into the numbers; on this
  branch (which does not wire damping into any asserted value) master's fixtures still pass unchanged —
  verified: the full `internal/agent` suite's failures are identical before and after (§5).

## 5. Verification

```
$ cd ~/workspace/gopi-hippo-salvage && go build ./...
(no output)                      # exit 0

$ go vet ./internal/hippocampus/ ./internal/agent/
(no output)                      # exit 0

$ gofmt -l internal/hippocampus/ internal/agent/
(no output)                      # exit 0
```

Branch-scoped test runs (Rule 27 smoke, never whole-tree):

```
$ go test -count=1 ./internal/hippocampus/
ok  	github.com/nick/gopi/internal/hippocampus	63.149s     # whole package, GREEN on the branch
```

`internal/agent` (the touched package, ~3 min wall — run as background jobs, never a shell timeout).
The `--- FAIL` names, branch vs clean master, same command and same worktree pair:

| worktree | command | failing test names |
|---|---|---|
| branch | `go test -count=1 ./internal/agent/` | `TestDocAnchorsResolveInNormativeDocs` only (`FAIL … 179.559s`) |
| clean master baseline (`gopi-hippo-salvage-base`, detached `f69a2149`) | `go test -count=1 ./internal/agent/` | `TestDocAnchorsResolveInNormativeDocs` only (`FAIL … 240.934s`) |

**Identical failing set on both sides** ⇒ not attributable to this diff. `TestDocAnchorsResolveInNormativeDocs`
reports **pre-existing drift** on master itself (86 `docanchor_test.go:190` entries on the branch,
including `devdoc/branch-task-linkage.md`, `devdoc/cov-agent-cr.md`, `devdoc/merger-tick-gate.md` …);
the branch adds **none** (the new devdoc is classed `historical`, which the anchor gate deliberately
excludes from its scope — `git grep -c 'salvage-measure'` over the gate output = **0**).

Earlier in the run the branch additionally failed
`TestHippocampus_ConvergenceGateSuppressesTailOnlyChange` and
`TestHippocampus_LeadsRepeatWithNewBodyIsDeliverable` — **both GREEN on clean master** with the same
scoped command (`ok github.com/nick/gopi/internal/agent 29.140s`). That was **this branch's own**
regression: the tag's `ClampedBriefConfidence` reads `Tokens` for its lead-fit damping, while master's
`briefHit` fixture left `Tokens` at 0, so the damping collapsed a fixture's confidence just above the
no-hits floor and the convergence gate suppressed a brief those tests require. The tag's own fix for
exactly this — `Tokens: 1` in `briefHit` — was therefore **carried verbatim** (§4), after which the
whole package's failing set became exactly master's (the table above). Root cause identified, then the
tag's own remedy applied — not a parameter tweak.

The three devdoc-manifest gates (`internal/standards`), branch vs master, `-run TestDoc`:

| gate | branch | clean master |
|---|---|---|
| `TestDocManifestCoversEveryDevdocFile` | FAIL — `2063 rows cover 2065 files`, duplicate row `devdoc/daemon-death-pipe-fd-shape.md` | FAIL — `2062 rows cover 2064 files`, same duplicate, same three unlisted docs |
| `TestDocManifestRowsAreWellFormed` | ok | ok |
| `TestDocClassBannerMatchesManifest` | ok | ok |

Pre-existing on master, and the branch is exactly **+1 row / +1 file** over master: the new report was
added to `devdoc/DOC-INDEX.tsv` as class `historical` with the mandatory
`<!-- doc-class: historical -->` first line and the manifest's 4th (tab-terminated) column, so the
coverage and banner gates hold for it (observed: no complaint naming `salvage-measure`).

## 6. Deliverable, hygiene, Rule 30

- **Branch:** `fix/hippo-host-resolution` (created in this run off master `f69a2149`; it did not exist
  before — `git rev-parse --verify fix/hippo-host-resolution` failed with "Needed a single revision").
  Two worktrees, both mine (Rule 29): `/home/user/workspace/gopi-hippo-salvage` (deliverable) and
  `/home/user/workspace/gopi-hippo-salvage-base` (throwaway baseline, detached at `f69a2149` — all
  probe/shim artifacts lived there, never in the deliverable).
- Files committed: `internal/hippocampus/brief.go`, `internal/agent/hippocampus_agent.go`,
  `internal/hippocampus/brief_test.go`, `internal/agent/hippocampus_display_identity_test.go`,
  `internal/agent/hippocampus_seam_test.go` (new, verbatim from the tag),
  `devdoc/hippo-host-resolution-salvage-measure.md`, `devdoc/DOC-INDEX.tsv` (one row).
  The tag's `host_deadline_test.go` was **deliberately not carried** (see below).
- The tag `archive/fix/hippo-host-resolution` is **untouched**; nothing was committed on it or on the
  archived branch (Rule 23). No merge performed (PA owns merge). No branch/tag deleted.
- `git status --porcelain` in the deliverable worktree at close-out: **empty** (Rule 30).

## Not Done (honest)

- `internal/agent`: `TestDocAnchorsResolveInNormativeDocs` is RED on the branch **and** on clean master
  (pre-existing doc-anchor drift, 86 entries — none of them mine). Not fixed here: it is not this
  diff's defect and re-keying landed records is a separate task.
- `internal/standards::TestDocManifestCoversEveryDevdocFile` is RED on the branch **and** on clean
  master (pre-existing duplicate row `devdoc/daemon-death-pipe-fd-shape.md` + three unlisted docs).
  Not fixed here for the same reason.
- The tag's `host_deadline_test.go` is **not transplanted**: it governs the floor retirement, and
  master's `NewHostEngine` + `HostEngineClientTimeout` + `HostEngineCycleTimeout` are **live** (added
  by `1f68439f`, after the tag's base — §2). Transplanting it would either delete covered behaviour or
  demand an unrelated rewrite of master's own tests. The floor-inertness finding is preserved here as
  the finding it is, with its sha, rather than as a code change.
- The agent-side PUBLISHED number (`HippocampusBrief.Confidence` for a real cycle on master's live
  path) is **not** rewired to the organ's new derivation: master's live path derives confidence inside
  the organ's `runCycle` (not through `ClampedBriefConfidence`), so making the published number
  damped/tier-normalised would change live delivery behaviour and needs a live-session measurement
  (the tag's §4.4 instrument). This branch lands the single-source surface + `BriefFromResult`
  delegation; deep-wiring the live publish is a follow-up task, flagged here rather than claimed done.

