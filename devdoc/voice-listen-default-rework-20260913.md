<!-- doc-class: historical -->
# Rework: `fix/voice-listen-default` — recreated, master merged in, delta-ZERO proven

**Branch:** `fix/voice-listen-default` (tip `5f482df1`, worktree `~/workspace/gopi-voice-rework`)
**Session/task:** `01a09659` · **Date:** 2026-09-13
**Recovery:** `git show fix/voice-listen-default:devdoc/voice-listen-default-rework-20260913.md`
**Original merge:** `08bdcf0b` (tip of the branch as landed) · **Revert:** `37ed0a04` · **Original fix branch tip:** `49bc78a9`

---

## 1. Why the rework existed (the honest version)

The branch was merged (`08bdcf0b`) and then reverted (`37ed0a04`) with reason
`verify child exceeded the merger's wait cap` — **not** a failing test. The
mechanism is now documented in the tree itself (`internal/tools/verify_gate.go`,
"BASELINE SWEEP" block): the verify child was asked to make the merged state
pass **absolutely**, on a master that was already red
(`internal/testutil`, `internal/standards`, `internal/stylegate`) and against a
15-minute cap. Six branches were reverted that way, this one among them. The
tree today measures the **delta** against a pre-merge baseline
(`MergeVerifyBaselineRunTimeout = 20m`) instead of demanding absolute green.

## 2. Two structural facts that re-shaped the rework

Both were discovered by inspection, not assumed:

1. **`49bc78a9` is already an ancestor of master.** Master descends from the
   original branch tip *through the merge `08bdcf0b` and the revert `37ed0a04`*.
   So "branch at `49bc78a9`, merge master into it" does **not** reproduce the
   reverted state — it **fast-forwards to master**, silently yielding a tree
   with the fix **absent** (`voiceDefaultListen = ":7788"`, dead
   `voiceLooksLikeAudioPath` back). The rework therefore had to **re-apply the
   fix content as a commit on the current-master line**, exactly as the sibling
   reworks (`fix/task-list-state-honesty`) learned.
2. **The merger had already landed an equivalent rework.** While this task ran,
   master moved `7247d00d → 0cfb618c "Merge branch 'rework/voice-listen-default'"`,
   landing branch `784a60cf` (the earlier round of this same task). The landed
   content is **byte-identical** to what this rework produced for the six fix
   files (`git diff master HEAD` for them is empty), so the branch now reduces
   to **master + one real fix** (below). This is recorded rather than hidden:
   the live defect it proves is that master's landed copy **reds
   `internal/stylegate`**, which the landed merge did not catch.

## 3. What was done

| Step | Action | Evidence |
|---|---|---|
| Recreate ref | `git branch fix/voice-listen-default 784a60cf` (no force-push, no history rewrite) | ref → `784a60cf`; `49bc78a9` untouched |
| Re-apply fix | checkout `08bdcf0b`'s 6 files onto the current-master line, then re-apply master's later Rule-17 `// env-leak-ok:` annotation to `cli_voice_review_test.go` | restored files vs `08bdcf0b` differ by **exactly one annotation line** |
| Merge master | `git merge master` (no conflicts) at two points: `59455092`, then `7247d00d`/`0cfb618c` | tips `2746b92c`, `f43aee83`; no conflict markers |
| **Root-cause fix** | `5f482df1` — annotate the two probe-teardown `Close()` sites the branch's **new** bind test introduced | see §5 |

### The fix intent is preserved (re-derived, not quoted)

| Intent | Evidence on the final tree |
|---|---|
| loopback default | `cmd/agent/cli_voice.go:58` → `voiceDefaultListen = "127.0.0.1:7788"` (was `":7788"`) |
| upload body cap | `cmd/agent/cli_voice_server.go:193` → `r.Body = http.MaxBytesReader(w, r.Body, limit)`; budget `voiceMaxUploadBytes = voiceMaxAudioBytes + voiceMultipartFramingSlack` |
| dead code gone | `grep -rn voiceLooksLikeAudioPath cmd/ internal/` → **0 hits** |
| behavioural pins | `TestVoiceDefaultListenBindsLoopbackOnly`, `TestVoiceHTTPListenDefaultShape` (`fixVoiceListenDefault = true`), `TestVoiceListenIsWildcard`, `TestVoiceTranscribeRejectsOverLimitUpload`, `TestVoiceMaxUploadBytesIsTheDocumentedBudget` — all PASS |

**Red-proof (the pins actually detect the bug):** reverting the constant to
`":7788"` in the tree makes **both** pins fail —
`the default ":7788" bound :: — NOT loopback; a wildcard default is the P0
finding the review filed` and `voiceDefaultListen = ":7788" binds EVERY
interface again — the P0 regression is back`. Constant restored; worktree clean.

## 4. Builds and the differential sweep

**Build (declared + whole tree), on the merged tip `5f482df1`:**

```
go build ./...                                  → BUILD_OK        (exit 0)
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ → DECLARED_BUILD_OK (exit 0)
```

**The differential, scoped by proof, not by convenience.** The merged tree was
proven to differ from pristine master (`0cfb618c`) **only** by:

```
git diff --name-only master HEAD
  cmd/agent/cli_voice.go
  cmd/agent/cli_voice_review_test.go
  cmd/agent/cli_voice_server.go
  cmd/agent/cli_voice_test.go
  devdoc/voice-input-review.md
  devdoc/voice-listen-fix.md

git diff --quiet master HEAD -- 'internal' 'cmd' ':!cmd/agent'  →  IDENTICAL outside cmd/agent
```

so the differential reduces to `cmd/agent` — **but the whole `go test -count=1
./...` sweep was run on both trees anyway**, as the acceptance demands. Both
sweeps ran under the same `-timeout 30m`, concurrently, on the same host/load.

### Evidence table — merged vs control (full sweep)

| Metric | MERGED (`5f482df1`) | CONTROL (`0cfb618c`, pristine master) |
|---|---|---|
| exit | `EXIT=1` | `EXIT=1` |
| failing **packages** | `cmd/agent`, `internal/standards`, `internal/testutil` | `cmd/agent`, `internal/standards`, **`internal/stylegate`**, `internal/testutil` |
| **merged-only failing packages** | — | **∅ EMPTY** |
| control-only failing package | `internal/stylegate` (fixed by `5f482df1`) | — |
| `cmd/agent` new failing tests | `TestFrontRunConsoleOutputLandsInItsSessionDir`, `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` | run2: `TestSweepZombiesCLI_DryRunTouchesNoNotifySink`, `TestSweepZombiesCLI_ReportNotifiesOperator` · run1: `TestFrontRunConsoleOutputLandsInItsSessionDir`, `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild`, `TestTaskRunChildSIGKILLClosesWrapperRowE2E` |

**The two `cmd/agent` "merged-only" tests are control-side flakes, proven by the
control's own repeat runs.** All four volatile tests live in `cmd/agent` and are
load-sensitive:

| Test (all in `cmd/agent`) | CONTROL run1 (`7247d00d`) | CONTROL run2 (`0cfb618c`) | MERGED (`5f482df1`) |
|---|---|---|---|
| `TestFrontRunConsoleOutputLandsInItsSessionDir` | FAIL | PASS | FAIL |
| `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` | FAIL | PASS | FAIL |
| `TestTaskRunChildSIGKILLClosesWrapperRowE2E` | FAIL | PASS | PASS |
| `TestSweepZombiesCLI_DryRunTouchesNoNotifySink` | PASS | FAIL | PASS |
| `TestSweepZombiesCLI_ReportNotifiesOperator` | PASS | FAIL | PASS |

Master itself flips these tests between two runs on the *same commit family*, so
they cannot be branch-caused; the branch's four changed files are the voice
files, all of which are green, and the package's pre-existing reds
(`TestScriptsAreBoundedAndOffTmp`, `TestWorkerOwnershipPointerPresent`,
`TestStandardGate*`) fail on both trees identically.

**Pre-existing red (not this branch's, unchanged by it):** `internal/standards`
(`TestStandardGates`, `TestStandardGateDataReport`), `internal/testutil`
(`TestScriptsAreBoundedAndOffTmp`, `TestWorkerOwnershipPointerPresent`),
`cmd/agent` flakes. The acceptance is delta-zero, never absolute green.

## 5. The one branch-caused red, and its root cause

`internal/stylegate` / `TestStyleGate` **(control green, merged red)**:

```
stylegate unchecked-close: 2 test hit(s), ceiling 0
  cmd/agent/cli_voice_review_test.go:1023 (error from wildcard.Close discarded)
  cmd/agent/cli_voice_review_test.go:1050 (error from conn.Close discarded)
```

**Root cause (5-why, not a param tweak):** the fix branch **adds**
`TestVoiceDefaultListenBindsLoopbackOnly`, which probes the default's ownership
of the wildcard namespace (bind `:<port>`, expect `EADDRINUSE`) and the LAN view
(dial every non-loopback IPv4, expect refusal). Both probes **succeed exactly on
the arm that is the failure being reported** — so each arm tears the probe down
and immediately `t.Fatalf`/`t.Errorf`s. The bare `Close()` there is a genuine new
violation of the swept `unchecked-close` rule (ceiling **0**, ratcheted by
`b47abcad`): master never had these `net.Listen`/`net.DialTimeout` closes because
master had no such test. The red therefore *is* branch-caused and lands on
master with the branch.

**Fix (`5f482df1`):** annotate both sites with the escape + its reason (the arm's
own failure is the report; there is no error left to handle), the same treatment
master already applies to the other 17 `Close` sites in that file. Not a
suppression by baseline — an inline reasoned exception, which stays visible in
the gate's data report (Rule 16 mechanics). Verified: `TestStyleGate` →
`ok github.com/nick/gopi/internal/stylegate 12.417s`.

## 6. Not Done / honest gaps

- **Absolute green is not achieved and was never the bar.** `internal/standards`
  and `internal/testutil` are red on **both** trees (master's own pre-existing
  reds); `cmd/agent` carries load-dependent flakes. Delta-zero is the acceptance
  and it is met.
- **The sweeps ran concurrently on a loaded host** (load 15–32 on 4 cores). That
  is why the differential is reported per-test with a control *repeat* run —
  a single control run would have been a weaker instrument, not a stronger one.
- **The two `cmd/agent` tests above are unresolved as a class.** They are
  control-side flakes, but a flaky e2e test is a defect in its own right; it is
  not filed here because it is master's, pre-existing, and out of this branch's
  contract.
- **Master's landed copy (`0cfb618c`) still carries the stylegate red** — the
  landed merge did not catch it. This branch (`5f482df1`) is the fix; it is
  strictly an improvement over master (master + the annotation, nothing else).
- **No force-push, no history rewrite.** The ref was recreated by `git branch`;
  the original tip `49bc78a9` is untouched and still an ancestor of master.

## 7. Round 2 — re-measured on the FINAL tip pair (master advanced twice during the task)

Master moved again while this task ran (`7247d00d → 0cfb618c → 41fa9c56`). Two
consequences, both handled rather than assumed away:

1. **`0cfb618c` landed the earlier round's copy of this very branch**, i.e.
   master itself acquired the loopback fix — and with it the `unchecked-close`
   red. Master then annotated the same two sites **independently** (`41fa9c56`,
   `fix/rule17-env-escape-annotations`), so master and this branch converged on
   the same fix and the merge **conflicted** on
   `cmd/agent/cli_voice_review_test.go` — a pure wording conflict between two
   annotations of the identical statements. Resolved by **taking master's
   canonical wording** (`git checkout --theirs`), not by re-litigating the
   reason. `git diff master HEAD -- cmd/agent/cli_voice_review_test.go` is
   **empty** after the resolution.
2. **The final measurement is the strongest form of the acceptance**: the branch
   now differs from pristine master by **zero `.go` files** — only this devdoc
   and the task report. The sweep was re-run on the final pair
   (`cf9024ca` vs `0cfb618c`→`41fa9c56`) and the differential is **exactly zero**.

| Metric | MERGED (`cf9024ca`) | CONTROL (`41fa9c56`, pristine master) |
|---|---|---|
| exit | `EXIT=1` | `EXIT=1` |
| failing **packages** | `cmd/agent`, `internal/testutil` | `cmd/agent`, `internal/testutil` |
| **merged-only failing packages** | — | **∅ EMPTY** |
| control-only failing packages | — | **∅ EMPTY** |
| **merged-only failing tests** | — | **∅ EMPTY** |
| `.go` delta vs master | **∅ (zero files)** | — |
| declared build + `go build ./...` | `BUILD_OK` + `DECLARED_BUILD_OK` (exit 0) | — |

Rounding out the flake attribution (§4) with a third data point: in round 2 the
volatile pair `TestFrontRunConsoleOutputLandsInItsSessionDir` /
`TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` failed **on the
control** instead. Three sweeps, three disjoint flake sets, all inside
`cmd/agent` — the set never follows the branch.

The branch is therefore not merely delta-free: the branch-caused red is **fixed
in master too**, so landing this branch is a no-op on code and carries the
report. `internal/standards` is green on this master/load (it was red in round
1) — recorded as observed, and irrelevant either way since both trees agree.

## 8. Deliverable location

- Branch **`fix/voice-listen-default`**, tip **`5f482df1`**, in the shared repo
  (worktree `~/workspace/gopi-voice-rework`).
- Recovery: `git show fix/voice-listen-default:devdoc/voice-listen-default-rework-20260913.md`
  and `git show fix/voice-listen-default:tasks/01a09659-report.md`.
- The worktree is removed before hand-off (safe-merge refuses a
  worktree-held branch); the commit is already on the branch in the main repo.
