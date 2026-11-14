<!-- doc-class: historical -->
# Task Report: hippocampus convergence identity aligns with the displayed briefing

Branch: `fix/hippocampus-key-aligns-display` (worktree `~/workspace/gopi-keyalign`, base master `ca27da4`; master has since moved one docs-only commit to `3b69162` and `git diff ca27da4 3b69162 -- internal/agent/` is EMPTY, so the branch merges clean)

## Completed

**The defect (reproduced on master before the fix).** The convergence gate compared
`hippoBriefKeys(res)` — one key per executed action plus one key **per hit over ALL
`res.Hits`** — while the briefing BODY renders only the first `hippoSummaryMaxHits`
(= 3) leads. The identity was therefore FINER than what the model is shown: a brief
whose displayed top-3 leads repeated but whose tail hits moved looked like a new
memory state and was re-injected with a **byte-identical body**.

Deterministic probe on master (throwaway `zz_probe_tail_test.go` in
`~/workspace/gopi-master-check`, deleted afterwards — `git status` clean):

```
body byte-identical = true
keys b1 = [#871 #1330 #42 #104]      // before: displayed top-3 + tail #104
keys b2 = [#871 #1330 #42 #200]      // after:  displayed top-3 + tail #200
round1 first brief          -> lv-1-append
round5 same display, moved tail -> lv-1-append   <-- the leak
```

**The fix (surgical, 3 edits + 2 new helpers, all in `internal/agent/hippocampus_agent.go`).**

1. `hippoLeadLines(hits []tools.MemoryHit) []string` — the **shared render helper**:
   it renders exactly the displayed lead lines (`"- #%d [%s] %s"`, capped at
   `hippoSummaryMaxHits`, retrieval order). It is now the only place lead lines are
   produced; `hippoBriefSummary` joins it with `hippoLeadDelimiter` (`"\n"`), and
   `hippoBriefKeys` keys on its elements. Display and identity can no longer drift.
2. `hippoBriefKeys(res, body string)` — identity = executed-action keys + the
   **displayed** lead keys (namespace `hippoLeadKeyNamespace` = `"lead:"`, which also
   removes the old collision between a hit key `#5` and an action key) + the rendered
   **body** text as the second dimension (`hippoBriefBodyKeyNamespace` = `"body:"`).
   The body is passed **in**, not recomputed: `hippoBriefFromResult` renders the
   summary ONCE and hands the same string to `Summary` and to the identity.
3. An **empty body emits no body key** — found by my own new guard test, which failed
   on the first cut (`got [body:]`): a hitless cycle would have carried a constant
   `body:` key, so two empty briefs would have looked "converged". Now a brief with no
   hits and no body yields **zero** keys ⇒ the host's keyless-brief rule applies.

Why the two dimensions (and their deliberate asymmetry):

- **leads** — the semantic content the model reads. A moved tail (past the display
  cap) leaves this unchanged ⇒ same identity ⇒ Lv-0.
- **body** — the only identity a lead-less brief can have, and what catches a body
  that moved for any other reason (a re-phrased query over the same leads). Here the
  model would read text it has never read, so the brief must stay deliverable.

Boundaries preserved (unchanged code paths, asserted by the pre-existing tests):
first-brief-never-suppressed (`h.sawBrief` latch), keyless brief never converged
(`len(b.Keys) == 0` ⇒ false), min-inject spacing, default-off (nil host ⇒
byte-identical behaviour; no new env var, no new knob, no new file in the agent
package beyond the test).

Where the fix landed and why NOT in `internal/hippocampus`: the organ does not import
`internal/agent`, and the identity must be derived from the same rendering that
produces the *delivered* text. Rendering the leads in the organ would either duplicate
the helper (the exact drift this bug is made of) or force the host to import the organ's
renderer. The minimal-cohesion site is the one file that already owns BOTH renderings
(`hippoBriefSummary` and `hippoBriefKeys` live there today) — the shared helper makes
the drift structurally impossible instead of merely tested.

## Not Done

- No change to the min-inject interval, the level ladder, the budget, or the organ's
  retrieval: the epic scoped this to the gate's identity.
- The tail beyond the display cap still influences `Confidence`/`MemoryWeight` (the
  organ's raw signals) — deliberately untouched: those are reported signals, not the
  gate's identity, and `hippoBriefConfidence` taking the best lead is the organ's
  contract. A tail change can therefore still raise/lower the *level* of a genuinely
  new brief, but it can no longer resurrect a body the model already read.

## Verification

### Independent re-verification (2nd pass, this session — owner re-checked the prior commit)

- `go build ./...` in the worktree → exit 0.
- `env -u GOPI_HIPPOCAMPUS go test ./internal/agent/ -run TestHippocampus -count=1` → `ok` (8.9s); the 4 new guards + every pre-existing hippocampus test run here.
- Full `env -u GOPI_HIPPOCAMPUS go test ./internal/agent/ -count=1` (run detached as a background job; the package takes ~145s, past the shell's no-output limit) →
  `ok  github.com/nick/gopi/internal/agent  143.980s` — the whole package green, not just the targeted run.
- Focused re-run of the acceptance set, each PASS individually:
  `_ConvergenceGateSuppressesTailOnlyChange`, `_ConvergenceGateSuppressesRepeatedState`,
  `_MinInjectIntervalSpacing`, `_Lv0_ArchiveOnly`, `_Lv1_AppendMemoAsMessage`,
  `_Lv2_MetaEventIsAMessageNotPromptSection`, `_Lv3_QuotaDegradesWithoutSpawningActivations`,
  `_IdentityCoversTheDisplayedLeadsOnly`, `_LeadsRepeatWithNewBodyIsDeliverable`,
  `_NoHitsNoBodyYieldsNoKeys`, `_DefaultOffLeavesBehaviourUnchanged`, plus
  `_LiteralGate`, `_GateAndKnobs`, `_ChatPathClampsToLv1`, `TestHippocampusDocAnchorsResolve`.
- `gofmt -l` on both changed files → no output; `go vet ./internal/agent/` → clean for the touched files.
- Before/after re-run against **real master symbols** (`git worktree add --detach <tmp> ca27da4`, throwaway test, worktree removed afterwards):

```
=== RUN   TestZZMasterTailOnlyRepro
    zz_master_repro_test.go:29: BUG REPRODUCED ON MASTER: moved tail (identical display) = 1, want Lv-0
--- FAIL: TestZZMasterTailOnlyRepro (0.00s)
```

  The probe re-expresses master's own key scheme (action keys + one key per hit over ALL `res.Hits`) and drives master's real `adjudicate` at round `1+hippocampusMinInjectIntervalRounds+1` ⇒ Lv-1 append on master, the bug. The same expectation is asserted by the committed guard `TestHippocampus_ConvergenceGateSuppressesTailOnlyChange`, which **passes** on the fix — the two sides of the before/after.
- Merge safety: `git diff ca27da4 3b69162 -- internal/agent/` is EMPTY (master's one new commit is docs-only), so the branch merges clean; the main repo working tree is untouched (`git status --short` shows only pre-existing untracked briefings/tasks files).

### First-pass evidence (as recorded when the commit landed)

- Build: `go build ./internal/...` → OK (`BUILD_OK`).
- Repro on master (pre-fix, probe in the master worktree): round 5, identical
  displayed leads + moved tail → `lv-1-append` (**bug present**); probe file deleted,
  master `git status --short` clean.
- Post-fix tests (worktree, `GOPI_HIPPOCAMPUS=` cleared for the default-off assertions):
  - `go test ./internal/agent/ ./internal/hippocampus/ -count=1` → _see "Test run" below_
  - New guards:
    - `TestHippocampus_ConvergenceGateSuppressesTailOnlyChange` — the epic's repro
      (round 1 → Lv-1, round 5 after the interval with identical displayed top-3 and a
      moved tail → Lv-0).
    - `TestHippocampus_IdentityCoversTheDisplayedLeadsOnly` — every displayed lead has
      a key and is in the body; no over-cap hit reaches either.
    - `TestHippocampus_LeadsRepeatWithNewBodyIsDeliverable` — different prose over the
      same leads is not converged (second dimension).
    - `TestHippocampus_NoHitsNoBodyYieldsNoKeys` — keyless stays keyless.
  - Pre-existing, still green: `TestHippocampus_ConvergenceGateSuppressesRepeatedState`,
    `_MinInjectIntervalSpacing`, `_Lv0_ArchiveOnly`, `_Lv1_…`, `_Lv2_…`, `_Lv3_…`,
    `_ConfidenceVetoDegradesToArchive`, `_DefaultOffLeavesBehaviourUnchanged`,
    `_DefaultGateIsOffAtLv1`, `_LiteralGate`, `_GateAndKnobs`.

### Test run

```
$ GOPI_HIPPOCAMPUS= go test ./internal/agent/ ./internal/hippocampus/ -count=1
ok  	github.com/nick/gopi/internal/agent	162.922s
ok  	github.com/nick/gopi/internal/hippocampus	21.737s
```

Focused run (`-run TestHippocampus`, 11.6s) green as well — every pre-existing
hippocampus test plus the four new guards.

### Before/after proof (the same expectation, both sides)

Post-fix expectation asserted against **unfixed master** (throwaway probe in a
detached worktree at `ca27da4`, post-fix expectation with master symbols; probe
deleted, `git status` clean):

```
$ GOPI_HIPPOCAMPUS= go test ./internal/agent/ -run TestZZPrefixTailOnlyChange -count=1 -v
    zz_prefix_probe_test.go:31: BUG: identical displayed leads + moved tail re-delivered at 1
        (want lv-0-archive)
--- FAIL: TestZZPrefixTailOnlyChange (0.00s)
```

The new guard `TestHippocampus_ConvergenceGateSuppressesTailOnlyChange` asserts
exactly that expectation and **passes** on the fix. Copying the new test file to
the unfixed-master worktree fails to COMPILE (`undefined: hippoLeadLines` /
`hippoLeadKeyNamespace`) — i.e. it genuinely pins the new rendering contract;
the host-level before/after is the probe above.

### Incident (recorded on purpose)

Mid-task a concurrent **merger killed this worktree** (`git worktree remove` on
the path; both processes reported the same pid) and my uncommitted edits were
lost — only the already-written devdoc file survived. Recovered because
`internal/agent/hippocampus_agent.go` was untouched on master
(`git diff ca27da4 master -- <that file>` empty), the edits were re-applied, and
the commit landed **immediately**. Lesson for the next agent: in this repo, an
uncommitted worktree is not safe — commit as soon as the build is green, then
keep testing.

## Files

- `internal/agent/hippocampus_agent.go` — shared `hippoLeadLines`, two-dimension
  `hippoBriefKeys`, single-render `hippoBriefFromResult`, 4 new named constants.
- `internal/agent/hippocampus_display_identity_test.go` — 4 regression guards.
- `devdoc/hippocampus-key-aligns-display.md` — this report.
