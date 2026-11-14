<!-- doc-class: historical -->
# devdoc: the doubaoasr test-sleep red (stylegate ceiling 0)

## Symptom (master, pre-existing)

```
go test ./internal/stylegate/ -run TestStyleGate -count=1
  --- FAIL: TestStyleGate
      stylegate test-sleep: 1 test hit(s), ceiling 0
        internal/doubaoasr/doubaoasr_test.go:458  (time.Sleep in a test)
```

Master was red in `internal/stylegate` since the doubao ASR merge (`900b7837` /
`e7b9d739`); no branch under test touched `internal/doubaoasr` or
`internal/stylegate`.

A/B (each a clean checkout, no branch changes): `/dev/shm/gopi-vf-master`
(16f871c3), `/home/user/workspace/gopi-master-pre` (1335daed), main repo
(72d6f6b/c1117fb9) → same single FAIL, same one site.

The scanner enumerates every rule in one pass
(`go run ./internal/stylegate/cmd/stylegate -list`); the only red class on
master was **test-sleep 1 > 0**. (ignored-error is 164 sites with ceiling 182,
errorf-verb 9 ≤ 9, string ≤ 8, undecidable 119 is informational.)

## Root cause (5-why)

1. `TestTranscribeHonoursContextCancellation` must prove the poll loop stops
   when the context is cancelled — i.e. the cancel has to land **while the
   client sits in the poll wait**.
2. To arrange that, the test spawned `go func(){ time.Sleep(200ms); cancel() }()`.
3. That is (a) a wall-clock race the test only *usually* wins (slow CI can let
   the poll query first) and (b) a `time.Sleep` inside a test — the exact shape
   the 2026-09-12 sweep drove to ceiling 0.
4. The sweep's rule was not applied to this site because the ASR work landed
   after it.
5. → The honest fix is to trigger the cancellation **deterministically**,
   not to annotate a race as acceptable.

## Fix

The fake HTTP service now carries an `onQuery func()` hook, run on the
**server's own handler goroutine** when a `/query` request lands — precisely
while the client is blocked in the poll wait:

```go
mux.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
    if fs.onQuery != nil { fs.onQuery() }
    ...
})

ctx, cancel := context.WithCancel(context.Background())
fs.onQuery = cancel          // fires from the handler goroutine, mid-wait
```

The test's assertion is unchanged (`errors.Is(err, context.Canceled)` plus a
5s wall-clock *ceiling*). No marker, no annotation, no ceiling change: the
ratchet is byte-identical (`git diff master -- internal/stylegate/` empty).

A first draft also asserted `fs.queryCalls > 0` in the test — the race
detector flagged it (`-race`: read of `queryCalls` in the test vs the write in
the handler, `doubaoasr_test.go:477`). It was redundant anyway: `cancel` is
only ever called by the hook, so observing `context.Canceled` already proves
the handler ran.

## Red-proof (the rule still bites)

Planted a fresh `time.Sleep` in the same package → the gate FAILed again
("test-sleep: 1 test hit(s), ceiling 0"); removed it → green. The gate was not
weakened, and the fix is not a suppression.

## Verification

- `go test ./internal/stylegate/ -run TestStyleGate -count=1` → **ok** (16.0s)
- `go test ./internal/doubaoasr/ -race -count=1` → ok (13.6s)
- `go test ./internal/doubaoasr/ -run TestTranscribeHonoursContextCancellation -count=10` → ok (no flake)
- `go vet ./internal/doubaoasr/` → clean; `gofmt -l internal/doubaoasr/` → empty
- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- `git diff master -- internal/stylegate/` → **empty** (ceiling/ratchet untouched)
- Timing: the test now costs `pollIntervalSec` (1s, the poll wait it is about)
  instead of 200ms fire-and-forget + race.

## Not Done (honest)

`go test ./internal/standards/ -count=1` is **also red on master**, unrelated
to this task: `TestGate3ElseAfterReturnRedProof` reports a NEW Rule-18 violation
at `cmd/agent/cli_voice_device_policy.go:226` (else after a terminating if).
A/B: a clean detached worktree at master c1117fb9 shows the identical failure.
Not fixed here (out of scope — one red class per task, and it is a source-line
refactor of the voice CLI).
