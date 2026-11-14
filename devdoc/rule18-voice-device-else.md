<!-- doc-class: historical -->
# Task Report: master red — Rule 18 else-after-return at cmd/agent/cli_voice_device_policy.go:226

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

## Completed
- Diagnosed the pre-existing master red: `internal/standards` `TestGate3ElseAfterReturnRedProof`
  reported a NEW Rule-18 (no `else` after a terminating `if` body) violation at
  `cmd/agent/cli_voice_device_policy.go:226`, with `hits=4 baselined=3 escaped=0 NEW=1`.
- Root cause (`git_blame`): the site was introduced by commit `91e9f7ac`
  ("fix(voice): resolve an explicitly named pulse monitor; report a considered tier-3 failure",
  2026-09-14) — tier 3 of the voice device-selection policy was changed from a bare
  `if err == nil { return }` into `if err == nil { return } else { notes = append(...) }`.
  The new `else` is dead weight: the `if` body ends in `return`, so the note-taking branch
  can be de-indented into a guard clause. No baseline entry was ever added for it
  (correctly: the allow-list may only ratchet down).
- Fixed by de-indenting the branch (guard clause). To keep the `err` value available after
  the `if` statement, the call is hoisted per the file's own convention
  (`verr := voiceVerifyDevice(dev, capture, &notes)` — the same `rerr`/`verr` style used by
  tier 2 in this file, which does NOT create an `else` after a terminating body):

```go
dev := voiceSystemDefaultDevice()
verr := voiceVerifyDevice(dev, capture, &notes)
if verr == nil {
    return voiceSelection{Device: dev, Tier: voiceTierSystem}, notes, nil
}
// A tier that was CONSIDERED and failed must say so: falling
// through silently would hide the reason (a busy or absent
// `default`) behind whatever tier 4 reports afterwards, and the
// reader would blame the wrong device (Rule 15b).
notes = append(notes, fmt.Sprintf("system default capture %q is not reachable — "+
    "falling through to the next rule: %v", dev.ID, verr))
```

- Comment, message text and control flow are byte-identical in behaviour: on a verify failure
  the note is still appended and selection still falls through to tier 4.
- `internal/standards/baseline.go` was **NOT** touched — the gate's own baseline may only
  ratchet down, and going green by deleting/adding a permit would be silencing it.

## Not Done
- The other `else`-shaped site in the same file (`:215`, tier 2's `} else {` inside an
  `if/else if/else` chain) is **not** a Rule-18 hit: the gate is purely syntactic, and an
  `else` whose sibling body does not terminate is out of scope. Left as is (deliberately).
- No change to the gate, the baseline, or the gate's predicate.

## Verification
- Build: `go build ./...`: OK
- gofmt: `gofmt -l cmd/agent/cli_voice_device_policy.go`: clean (no output)
- Tests: `go test ./internal/standards/ ./cmd/agent/ -count=1`: PASS
  (baseline `internal/standards` was FAIL / `TestGate3ElseAfterReturnRedProof` NEW=1 before the fix)
- Commit: the single commit on branch `fix/rule18-voice-device-else`
  (`git log -1 --oneline`; it de-indents the Rule-18 else and adds this devdoc): de-indent the Rule-18 else in the voice device policy

## Notes for the next agent
- The gate keys baselines on exact `file:line`; this fix **removes a hit** (no permit touched),
  so the baseline is unchanged and no re-key is needed.
- Pre-existing-red briefings should be measured on the worktree before fixing: here the red
  reproduced on `master dcce3a86` in a clean detached worktree with the exact gate line
  `NEW violation (checklist 4.3) at cmd/agent/cli_voice_device_policy.go:226`.
- **Unrelated pre-existing red, NOT caused by this branch**: with several `gopi worker`
  processes live on the host (5 in the process table during this session),
  `TestWorkerDaemon_StatusReportsTheLiveDaemon` (cmd/agent) fails — it asserts that
  `gopi worker --status` with no daemon must not name this home's worker state dir, and the
  status text prints it for the process-table half of the scan. Verified pre-existing: the
  same failure reproduces in a clean detached worktree at `dcce3a86`
  (`/home/user/workspace/gopi-rule18-base`). Out of scope here; it is a host-contention
  artifact of the e2e suite, not a source defect in this change.
