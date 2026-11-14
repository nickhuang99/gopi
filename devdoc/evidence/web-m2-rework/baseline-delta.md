<!-- doc-class: historical -->
# Baseline vs delta — measured, not asserted

Baseline = pristine master `ba53da3d` (this branch's base; the main repo
checkout was at the same tip when the baseline was taken, `.baseline/gates.txt`).

## Build / vet / gofmt

| check | baseline (pristine base) | this branch |
|---|---|---|
| `make build` | OK | OK |
| `go vet ./...` | OK (no output) | OK (no output) |
| `gofmt -l .` | 7 files already unformatted: `cmd/agent/cli_tasks_advert_gate_test.go`, `cmd/agent/daemon_scope_unit_test.go`, `cmd/agent/usage_test.go`, `devdoc/voice-zerodep-poc/model_embed.go`, `devdoc/voice-zerodep-poc/model_embedq5.go`, `internal/testutil/worker_ownership_gate_test.go`, `internal/tools/task_deliverable_worktree.go` | same 7 files — **no new one**; every file this branch touched is `gofmt`-clean |

## `internal/stylegate` (TestStyleGate)

Identical hit-set on both sides (the ceiling failures are a documented
pre-existing master condition; 201 test hits vs a ceiling of 182):

```
baseline: errorf-verb 9/8 · ignored-error 1/0 (prod) · ignored-error 201/182 (test)
          unchecked-close 1/0 · test-sleep 2/0 · test-setenv 1/0 · test-skip 1/0
branch  : (byte-identical list)
```

**No file this branch touched appears in the violation list** —
`grep -E "cli_web_m2|cli_web_m2b|chat_ops.go|cli_web.go|page_harness|page_scenarios"`
over the gate output is empty, so the branch adds zero violations of the swept
rules (`//stylegate:ignore` / `//stylegate:allow-sleep` comments are used where
a deliberate discard/explicit poll is correct, each with its reason).

## `internal/testutil` (5 failures) — pre-existing on master, unrelated

`TestPinCoverageTable`, `TestScriptsAreBoundedAndOffTmp`,
`TestEveryTempUsingSuiteIsPinned`, `TestWorkerOwnershipPointerPresent`,
`TestWorkerOwnershipGateStaysGreenAcrossSuccessiveMergeRounds`.

`TestEveryTempUsingSuiteIsPinned` is measured pre-existing on untouched master
in `devdoc/worker-ownership-slash-pair-20260917.md` (master's own report):
"fails because `internal/lineedit` creates scratch dirs with no `TestMain` pin.
MEASURED on untouched master … with the identical failure text". None of these
tests reads a file this branch touches.

## `internal/cgroupscope`, `cmd/agent`, `internal/agent`

- `internal/cgroupscope`: red on both sides (known set), untouched by this branch.
- `cmd/agent` (full package, ≈10 min): pre-existing failures on the base
  (a goroutine dump from `TestLifecyclePassInvokesWorktreeReap` in
  `internal/tools`' worktree sweep, plus the voice-server tests that keep
  listening). **Every test this branch added or touched passes: 16/16**
  (`go-test-web-verbose.txt`).
- `internal/agent`: pre-existing failures unchanged; the specific tests around
  the changed file (`TestChatOps|TestToolEvent`) pass.
