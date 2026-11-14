<!-- doc-class: historical -->
# Task Report: the feedback description's numbers now come from the engine (and a gate keeps them there)

> Task `01a0e0402cf17523a890f709f6e61d03` · branch `feat/memory-feedback-numbers-drift` (renamed from the task id)
> Date: 2026-09-27 · Base: `58ef566a` (merge-base with master) · Predecessor: `01a0dffb` (the parameter-contract half of this same description)

## Completed

- **The stale text is gone; the numbers it states are the engine's numbers.** The
  `memory(action=feedback)` description in `internal/agent/loop.go` now reads:

  > One vote per session per hit moves the hit's stored confidence 0.05 (a useless vote
  > -0.10) and re-ranks search; NO tier is ever auto-demoted — the machine only SUGGESTS
  > (3 decayed useless votes and zero useful ⇒ --suggest-cold lists a candidate), and
  > setting tier=cold is yours via action=update.

  It replaced: `One vote per session per hit; each moves the score ≤0.2; ≥2 useless votes
  auto-demote to cold (reversible).`
- **One more source-of-truth constant, so the engine's suggestion text and the gate read the
  same spelling**: `tools.ColdDemotionHint = "--tier cold"`, now used by both the vote
  result's COOLDOWN line and `--suggest-cold`'s output (Rule 13; no new file, key, script or
  schedule — Rule 35).
- **The contract gate is extended** (`internal/agent/memory_feedback_contract_test.go`, the
  file `01a0dffb` added) with two tests and their support helpers:
  - `TestFeedbackDescriptionStatesTheEnginesNumbers` — the text must carry no stale claim and
    must state the numbers the ENGINE produces: the per-vote stored-confidence step, measured
    by voting and reading the engine's own `confidence_step=` field for **both** ratings; the
    cool-down threshold, measured by voting until the engine itself prints `COOLDOWN`; and the
    suggestion-only verb. Nothing is re-typed from a constant — each expected token is paired
    with a live observation.
  - `TestFeedbackVoteNeverMovesTheTier` — the engine half: useless votes repeated past the
    threshold leave the `tier=` column unchanged while the engine's result calls the demotion
    a suggestion. That observation is what makes "auto-demote" a claim about nothing.

## Not Done (honest)

- **The two repo NOTES that repeated the claim were left in place, deliberately.** They are
  run records, not live teaching surfaces: `devdoc/review-cli-memory.md` (class *historical*)
  and `devdoc/memory-doc-truth-audit.md` (class *historical*). They are the provenance of this
  task and Rule 36's practice is to classify, never to re-key or rewrite a landed record;
  §"Which source was wrong" below is where their errors are corrected. Neither is read by the
  model at runtime (`grep --include=*.go` finds the claim in no other production file).
- **`TestDocAnchorsResolveInNormativeDocs` remains red** — pre-existing and drift-neutral
  (same 269-entry drift set on base and tip, path-normalised, `comm -3` = 0 lines).
- **No tool-description budget gate exists**, so the description's length is not machine-guarded
  (only the *system* prompt has the 4500-char gate; the memory description grew by ~230 chars
  and is not in that budget). Flagged, not fixed — adding a budget is a separate decision.
- No LLM-adoption A/B measurement of the *new* wording's behavioural effect (Rule 11's habit for
  a prompt change): the run below proves the model now reads the corrected numbers, not that
  vote rates changed.

## Which source was wrong — measured

| claim | verdict | evidence |
|---|---|---|
| "≥2 useless votes auto-demote to cold (reversible)" | **the TEXT was wrong** | no automatic tier transition exists; the merged P0 design (commit `682fadc`) replaced it with suggestion-only behaviour. The single writer of `tier` is the explicit update path — `internal/tools/memory.go` `sets = append(sets, "tier=?")` — and a vote writes only `useful_count/useless_count/fb_last_at/confidence`. Measured: 3 useless votes left `tier=warm`, and the engine's own result says **`(suggestion only)`**. Threshold is ≥3 *decayed* useless votes **and** zero useful. |
| "each moves the score ≤0.2" | **neither was wrong in the way the review note claimed** | `fbScore = (β-mean − 0.5)·min(N_eff/fbGateScale,1)·fbWeight` with `fbBetaPrior=1`, `fbGateScale=4`, `fbWeight=1`. Measured over the (useful, useless) grid: max **\|fbScore\| = 0.4756** (approaching `fbWeight/2 = 0.5`), and max **single-vote change = 0.1190**. So one vote does move the term by ≤0.2 — the sentence was not false. But the review note's justification ("actual max ±0.5, typically ≤0.39") is **wrong as a statement about one vote** and wrong as arithmetic (measured max 0.4756, not 0.39). The sentence was replaced anyway because it named neither the term it bounded nor the effect the engine reports: the **stored confidence step, +0.05 useful / −0.10 useless** (larger per vote than the term it hid), which is what a model can actually observe in the vote result. |

So: **the teaching TEXT was wrong on the auto-demotion claim; the review NOTE was wrong on the
score claim** — it refuted a sentence that was true by citing a maximum that is also wrong.
Both defects are now fixed where they can do harm (the text), and recorded here for the notes.

## Teaching sites — the enumeration the fix had to cover

| # | location | class | before | after |
|---|---|---|---|---|
| 1 | `internal/agent/loop.go:1354` — the `memory` tool def, `action=feedback` description | **LIVE (the model reads it every round)** | `≤0.2` + `auto-demote to cold` | engine-measured numbers (above) |
| 2 | `internal/agent/loop.go:1426/1431` — the `id`/`rating` property descriptions | live | already correct (fixed by `01a0dffb`); asserted by the new arm 1 | unchanged |
| 3 | `internal/agent/loop.go:192` — `taskStartSearchMessage`, and `:679` — `toolRoutingRules` MEMORY block d | live | no numbers — no numeric claim to correct (grep evidence below) | unchanged |
| 4 | `devdoc/review-cli-memory.md` §8 (P1 item 2) | historical run record | carries the claim as the *source* of this finding | left as the record (file untouched) |
| 5 | `devdoc/memory-doc-truth-audit.md` F2 | historical run record | ditto | left as the record (file untouched) |
| 6 | `tests/fixtures/log-stats-raw/**` (3 files) | fixture / golden bytes | frozen snapshots of the OLD prompt, by design (`--memory` read-compat) | untouched |

Grep evidence (working tree, tracked sources, no `_test` filter):
`grep -rn "auto-demote" --include=*.go internal/ cmd/` → only `internal/agent/loop.go` (now
rewritten); `grep -rn "≤0.2" --include=*.go internal/ cmd/` → same single site. `internal/tools/memory_test.go`
matched only inside a frozen fixture string, not a claim. **One live teaching site, covered by
the gate; the other two hits are records, not teaching surfaces.**

## Verification

- **Build:** `make build` → `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`, exit 0.
- **Vet:** `go vet ./...` → exit 0, no output.
- **Tests (branch-scoped):**
  - `go test ./internal/agent/ -run TestFeedback -count=1` → `ok` (7/7: the five from `01a0dffb` unchanged + the two extended ones).
  - `go test ./internal/agent/ -count=1` (tip) → only `TestDocAnchorsResolveInNormativeDocs` FAIL.
  - `go test ./internal/agent/ -count=1` (base, `58ef566a`) → the same test FAIL **plus** `TestFeedbackDescriptionStatesTheEnginesNumbers`.
  - `comm -3` on the **path-normalised** drift sets: base 269 lines, tip 269 lines, `comm -3` = **0 lines** ⇒ identical drift set, no new failure.
- **fidelity check (engine, not prose):** measured `fbScore` max `0.4756`, max one-vote delta `0.1190` (grid `useful,useless ∈ [0,39]`, formula `fbScore` with the file's own constants).
- **Ran (Rule 31 — the changed surface, real arguments, real state):**
  1. CLI against a scratch ledger, post-change binary:
     `$G memory feedback 1 --useless` ×3 →
     `... confidence_step=-0.10 — COOLDOWN: ≥3 decayed useless votes and zero useful; consider \`memory_update 1 --tier cold\` (suggestion only)`,
     then `memory get 1` → `tier=warm` (**unchanged** — the only-up behaviour, live), and
     `$G memory feedback --suggest-cold` → `#1 post-change probe useless=3.0 useful=0.0 … → gopi memory_update 1 --tier cold`
     (the same `ColdDemotionHint` spelling from the second surface).
  2. The model surface in a private sandbox (`gopi sandbox create --label fbnum --task-id …`;
     `GOPI_HOME="$SB"`, binary = the built post-change `./cmd/agent/`), asked to read its own
     description back — `tool_calls=0`, **reads the corrected numbers verbatim**:
     > "One vote per session per hit moves the hit's stored confidence 0.05 (a useless vote -0.10)
     > and re-ranks search" … "NO tier is ever auto-demoted — the machine only SUGGESTS (3 decayed
     > useless votes and zero useful ⇒ --suggest-cold lists a candidate), and setting tier=cold is
     > yours via action=update."
     (full transcript: `rule31-model-run.txt`; sandbox path: `sandbox-path.txt`)
- **gofmt:** `gofmt -l .` → 19 files on base, 19 on tip, `comm -13` prints nothing (no NEW unformatted file).
- **Worktree:** `git status --porcelain` empty at close-out.
- **Commit:** the branch tip on `feat/memory-feedback-numbers-drift` (base `58ef566a`); `git log --oneline -1` names the hash — it is not restated here, because the report lands INSIDE the commit it would name.
- **GREEN proof:** RED log pasted below; the same test passes at the tip (`ok github.com/nick/gopi/internal/agent`).

### RED proof (base `58ef566a`, gate file copied in, source untouched)

```
$ cd /home/user/workspace/gopi-fbnum-red   # detached worktree at 58ef566a
$ grep -c "≤0.2" internal/agent/loop.go    # 1 → the BASE text is what is under test
$ cp <tip>/internal/agent/memory_feedback_contract_test.go internal/agent/memory_feedback_contract_test.go
$ go test ./internal/agent/ -run TestFeedback -count=1 -v
--- PASS: TestFeedbackSchemaTeachesWhatTheValidatorDemands
--- PASS: TestFeedbackSchemaScopedParamsAreAcceptedWhenOptional
--- PASS: TestFeedbackSchemaDropsTheRetiredParam
--- PASS: TestFeedbackVoteLandsWithoutTheRetiredParam
--- PASS: TestFeedbackDescriptionsTeachTheWhitelistRule
=== RUN   TestFeedbackDescriptionStatesTheEnginesNumbers
    memory_feedback_contract_test.go:396: feedback description still teaches the retired claim "≤0.2": the engine does not implement it (see the section comment)
    memory_feedback_contract_test.go:396: feedback description still teaches the retired claim "auto-demote to cold": the engine does not implement it (see the section comment)
    memory_feedback_contract_test.go:413: rating=useful: the engine moves the stored confidence by +0.05, but the feedback description never states 0.05 — the text and the engine disagree
    memory_feedback_contract_test.go:413: rating=useless: the engine moves the stored confidence by -0.10, but the feedback description never states 0.10 — the text and the engine disagree
    memory_feedback_contract_test.go:420: the engine suggests cold after 3 useless votes, but the feedback description does not state that number: the text and the engine disagree
    memory_feedback_contract_test.go:425: feedback description never names "--suggest-cold": the engine's only vote-driven cold path is a suggestion, and a model told nothing about it cannot act on it
    memory_feedback_contract_test.go:428: feedback description does not mark the cold path as a suggestion ("suggest"): the machine never writes the tier column on a vote
--- FAIL: TestFeedbackDescriptionStatesTheEnginesNumbers (0.04s)
=== RUN   TestFeedbackVoteNeverMovesTheTier
    memory_feedback_contract_test.go:454: engine suggested cold after 3 useless votes
--- PASS: TestFeedbackVoteNeverMovesTheTier (0.03s)
FAIL	github.com/nick/gopi/internal/agent	0.834s
```

Full logs: `red-proof-internal-agent.txt`, `tip-agent-package.txt`, `base-agent-package.txt`,
`base-docanchor-v.txt` / `tip-docanchor-v.txt` (+ the normalised `*-docanchor-norm.txt`) in the
run's session dir.

### GREEN proof (tip)

```
$ go test ./internal/agent/ -run TestFeedback -count=1
ok  	github.com/nick/gopi/internal/agent	0.870s
```

## Evidence (self-filed)

- Arm 1 RED: `go test ./internal/agent/ -run TestFeedbackDescriptionStatesTheEnginesNumbers` on base → `FAIL` with the two stale-claim lines quoted above (exit 1).
- Arm 2/3 measurement: the same failing run printed `the engine moves the stored confidence by +0.05` / `-0.10` and `the engine suggests cold after 3 useless votes` — both read from live engine output, not from constants.
- GREEN: same command at the tip → `ok`, exit 0.
- `comm -3 base-docanchor-norm.txt tip-docanchor-norm.txt` → 0 lines (269 = 269).
- `comm -13 base-gofmt.txt tip-gofmt.txt` → no output (19 = 19).
- `make build` exit 0; `go vet ./...` exit 0.
- CLI: the COOLDOWN line quoted above was produced by the post-change binary with `--tier cold` rendered through `ColdDemotionHint`, and `memory get 1` printed `tier=warm` after 3 useless votes.
- `git status --porcelain` at close-out: empty.

## Paste-ready command for the operator

```bash
git -C /home/user/work/GoPi log --oneline -1 feat/memory-feedback-numbers-drift \
  && git -C /home/user/work/GoPi show feat/memory-feedback-numbers-drift:devdoc/memory-feedback-numbers-drift-20260927.md | head -40
```
