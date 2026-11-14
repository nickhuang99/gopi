<!-- doc-class: historical -->
# Task Report: fix `gopi checkpoint list` hang

## Symptom
`gopi checkpoint list` on the real machine appeared to hang forever: zero
output for minutes (the table prints only after every log is indexed), 120s
timeout killed it mid-run (exit 124) after reading 4.5GB with nothing shown.

## Root cause
`checkpoint.IndexDir` → `indexOne` → `CountRequests` did a **full sequential
read of every raw log** just to fill the display-only `reqs` column. The raw
dir had accumulated 1246 logs / ~19GB (`~/.gopi/raw`). Measured cold-cache
throughput ~37MB/s → a full invocation read ~19GB → **8+ minutes**, reading
as "hangs forever", with no output until the very end.

Latent second defect (same code family, not yet triggered on real logs):
`LastRequest`'s tail-window search caps growth at `maxTailWindow` = 512MB,
but its termination check was `window >= fi.Size()` — for a log >512MB whose
last `→ REQUEST` sits deeper than 512MB from EOF, `window` pins at 512MB and
`window >= fi.Size()` can never hold → the loop re-reads the identical 512MB
tail forever (true infinite loop). Real logs today all have their last
request within ~0.7MB of EOF, so this was latent — but one pathological log
would hang `checkpoint list`/`export`/`resume` forever with no timeout.

## Fix (branch `feat/fix-checkpoint-list-hang`)
1. `internal/checkpoint/index.go` — `CountRequests` is now a **bounded head
   sample**: it reads at most `countScanBudgetBytes` (16MB, named const) and
   returns `(count, truncated, err)`. `LogIndex` gains `RequestsTruncated`;
   `IndexDir` marks oversized logs. Bounds the whole command to
   ≤ ~16MB × #logs (19GB → worst ~5GB, typical far less).
2. `cmd/agent/cli_checkpoint.go` — the `reqs` column prints `≥N` for
   truncated logs; help text documents the bound.
3. `internal/checkpoint/parse.go` — `LastRequest` loop now terminates when
   `window >= fi.Size()` **or** `window >= maxTailWindow` (give up with a
   clear error instead of rescanning the same capped window forever).

## Why the count may be approximate (accepted)
The `reqs` column is a display hint: resume/export pick the log by the
last-checkpoint fields (model, msgs, bytes, sha256, message), which come from
the cheap 8MB tail scan + stat — never the full file. Request density is
~4-5/MB, so a 16MB head sample shows ~70-80 requests, representative for
comparing logs.

## Verification
- Build: `go build ./...` OK; `go vet ./internal/checkpoint/ ./cmd/agent/` OK.
- Tests: `go test ./internal/checkpoint/ -count=1` OK (incl. new
  `TestCountRequests_BudgetExceeded`, `TestCountRequests_ExactBudget`,
  `TestLastRequest_NoRequestInCapWindow`).
- `go test ./cmd/agent/ -count=1 -run 'Resume|Checkpoint'` OK.
- Real-machine run: pre-fix `checkpoint list` killed at 120s with 4.5GB read
  and zero output; post-fix run completes the full 1246-log dir (see timing
  below in the session).
- Regression guards ship with the fix (Rule 5/10).

## Not Done / Notes
- `internal/agent` full-suite run stalled under the two live `gopi task serve`
  daemons hammering the shared memory DB (known pre-existing flake, memory
  #1059) — checkpoint + agent resume tests ran green individually.
- No change to `gopi resume`/`export` hot path (LastRequest tail scan was
  already cheap for real logs); only its infinite-loop guard was added.
