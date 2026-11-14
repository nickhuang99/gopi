<!-- doc-class: historical -->
# Task Report: fix(shot) — remove unreachable code in shot_test.go

## Completed
- internal/shot/shot_test.go:157 `_ = idSeq` sat after the infinite `for` loop in serveFakeWS → unreachable (go vet error, introduced by 032e9f1). Deleted the single line; `idSeq` stays live via `idSeq++` inside the loop, so test semantics unchanged.

## Not Done
- Nothing deferred. internal/tools/ untouched per constraint.

## Verification
- Build/vet: `go vet ./internal/shot/` → exit 0, zero output
- Tests: `go test ./internal/shot/ -count=1` → ok, 1.012s
- Commit: fix/shot-vet-unreachable (worktree ~/workspace/gopi-shot-vet, shared refs — branch visible in main repo ~/work/GoPi)
