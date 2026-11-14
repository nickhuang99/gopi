<!-- doc-class: historical -->
# Tool Timeout & Deadlock Fix

> Branch: feat/tool-timeout-deadlock-fix
> Date: 2025-08-04
> Scope: All 4 tools (read, write, edit, bash)

## Problem

All four GoPi tools can hang indefinitely, deadlocking the agent:

| Tool | Timeout Before | Hang Scenarios |
|------|---------------|----------------|
| `read` | **NONE** | FIFO/named pipe, NFS stale mount, device files, `/proc` on hung process |
| `write` | **NONE** | NFS full disk/stale mount, FIFO with no reader, device files, full pipe buffer |
| `edit` | **NONE** | Both read and write unprotected — same vulnerabilities |
| `bash` | 60s absolute only | Interactive prompts (`ssh`, `sudo`, `passwd`), infinite-output commands (`ping`, `yes`, `find /`, `cat /dev/zero`) |

The agent loop has no defense against a hanging tool call — the entire ReAct
loop blocks forever.

## Solution

### File Tools: Goroutine + Channel + Context Timeout

For `read`, `write`, and `edit`, Go's `os.File` operations do not accept a
`context.Context`. The solution wraps each operation in a goroutine and uses a
buffered channel + `context.WithTimeout` `select`:

```go
func readWithTimeout(path string, offset, limit int, timeout time.Duration) (string, error) {
    ch := make(chan result, 1)
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    go func() {
        data, err := readImpl(path, offset, limit)
        select { case ch <- result{data, err}: default: }
    }()
    select {
    case r := <-ch: return r.data, r.err
    case <-ctx.Done(): return "", fmt.Errorf("read: %s: timed out after %s", path, timeout)
    }
}
```

The goroutine may continue running (or hang) after the timeout, but the
caller is unblocked. The channel has a buffer of 1, so the goroutine never
blocks on send. Memory is reclaimed when the goroutine eventually returns.

### Bash: Idle-Output Timeout

The existing absolute timeout (60s) already prevents permanent hangs, but an
interactive prompt like `ssh` or an infinite-output command like `ping` wastes
the full 60s. The new **idle-output timeout** kills the process when no
stdout/stderr output is seen for a configurable duration.

Implementation:
1. Use `cmd.StdoutPipe()` / `cmd.StderrPipe()` instead of `bytes.Buffer`
2. Goroutine reads from pipes, updates `atomic.Int64` timestamp on every byte
3. Idle monitor goroutine polls timestamp every 1s; kills process if idle > limit
4. `atomic.Int32` flag distinguishes idle-kill from normal exit / signal kill
5. Absolute timeout still enforced via `context.WithTimeout` on the command

### Timeout Constants (Centralized)

```go
// tools/timeout.go
const (
    DefaultFileToolTimeout = 30 * time.Second  // read, write, edit
    DefaultBashTimeout     = 60 * time.Second  // absolute max
    DefaultBashIdleTimeout = 30 * time.Second  // idle output
)
```

> **Note (bug025):** the values above are as of the deadlock fix. Since then
> `DefaultBashTimeout` became **120s** and `DefaultBashIdleTimeout` became
> **60s**, and the bash tool gained an LLM-driven per-call `timeout_sec`
> parameter (see `devdoc/bug025-bash-timeout.md`). `timeout_sec=0` = unlimited
> absolute time, still subject to the idle safety net.

Env vars (read once in `init()`, per Rule 2):
- `GOPI_TOOL_TIMEOUT_SEC` — overrides file tool timeout
- `GOPI_BASH_TIMEOUT_SEC` — overrides bash absolute timeout
- `GOPI_BASH_IDLE_SEC` — overrides bash idle timeout

## Design Decisions

### Why not context cancellation for file tools?

Go's `os.File.Read()` and `os.WriteFile()` do not accept a context. While we
could use `os.File.SetReadDeadline()` for some cases, it doesn't work for all
blocking scenarios (e.g., stat on NFS). The goroutine+channel pattern is the
most general approach and works for all blocking I/O.

### Why atomic flags for bash idle detection?

When `cmd.Process.Kill()` is called, `cmd.Wait()` returns `*exec.ExitError`.
This is indistinguishable from a process that exited with a signal for other
reasons. The `idleKilled` atomic flag provides a clean attribution path.

### Why not a single global timeout for all tools?

Different tools have different risk profiles:
- File tools: short operations, 30s is generous even for NFS
- Bash: commands can legitimately take 60s (compilation, large grep)
- Bash idle: `ping` produces output every 1s, so 30s idle is safe

### The LLM trust question

The Epic asks: "can we rely on LLM to always give correct bash cmd without deadlock?"

Answer: **No.** The LLM generates `cat /dev/zero`, `tail -f`, `ping`, `ssh`,
`find /`, and many other commands that can hang or loop forever. Defense must
be at the framework level. The idle-output timeout is the framework-level
defense: any command that stops producing output for N seconds is killed.

## Error Format (LLM-Friendly)

Timeout errors carry clear messages the LLM can act on:

```
Error reading /mnt/nfs/dead/host/file: timed out after 30s
Error writing /tmp/fifo: timed out after 30s
Bash error: command killed: no output for 30s
exit=-1
[command killed: no output for 30s]
Bash error: command timed out after 60s
exit=-1
[command timed out after 60s]
```

The LLM can retry with different parameters, use a different approach, or
report the failure.

## Test Coverage (13 new tests)

| Test | What It Verifies |
|------|-----------------|
| `TestReadTimeout_FIFOBlocks` | Read from named pipe with no writer → times out |
| `TestReadTimeout_ShortTimeout` | Normal file read with generous timeout → succeeds |
| `TestWriteTimeout_FIFOBlocks` | Write to named pipe with no reader → times out |
| `TestWriteTimeout_NormalFile` | Normal file write with generous timeout → succeeds |
| `TestEditTimeout_FIFOBlocks` | Edit on named pipe → times out |
| `TestEditTimeout_NormalFile` | Edit on normal file → succeeds |
| `TestBashIdleTimeout_KillsStalledCommand` | `sleep 60` with 1s idle → killed |
| `TestBashIdleTimeout_DoesNotFireWhenOutputFlows` | Output every 500ms < 2s idle → not killed |
| `TestBashIdleTimeout_InfiniteOutputCommand` | `yes` with 2s absolute → absolute timeout fires |
| `TestBashIdleTimeout_InteractivePrompt` | `sleep 3600` with 1s idle → killed |
| `TestTimeoutConstants_Defaults` | Constants have expected values |
| `TestTimeoutConstants_Resolution` | Runtime resolvers return non-zero |
| `TestBashCommandTimeout` (existing, updated) | Updated for new 3-arg bash() |

## Verification

- Build: `go build ./...` — OK
- Vet: `go vet ./...` — clean
- Tests: `go test ./...` — all pass (49 tests: 27 agent + 26 tools + deepseek cached)
- Race: `go test -race ./...` — all pass, no data races

## Files Changed

| File | Change |
|------|--------|
| `internal/tools/timeout.go` | NEW — centralized timeout constants + env-var resolution |
| `internal/tools/timeout_test.go` | NEW — 13 timeout tests |
| `internal/tools/read.go` | EDITED — goroutine+channel timeout wrapper |
| `internal/tools/write.go` | EDITED — goroutine+channel timeout wrapper |
| `internal/tools/edit.go` | EDITED — goroutine+channel timeout wrapper |
| `internal/tools/bash.go` | EDITED — idle-output timeout with atomic flags |
| `internal/tools/bash_test.go` | EDITED — 3-arg bash() signature update |

## Post-Merge Verification (2025-08-04)

Re-verified after merge to master:

- **Build**: `go build ./...` — OK
- **Vet**: `go vet ./...` — clean
- **Tests**: `go test ./...` — **175/175 PASS** (38 tools + 88 agent + 49 deepseek)
- **Race**: `go test -race ./...` — all pass, no data races

| Package | Tests | Race Clean |
|---------|-------|------------|
| `internal/tools` | 38 | ✅ |
| `internal/agent` | 88 | ✅ |
| `internal/deepseek` | 49 | ✅ |

### Defense-in-Depth: Three Layers of Timeout Protection

The fix adds per-tool timeouts, but the agent already had two other
protection layers. Together they form a defense-in-depth against hangs:

| Layer | Mechanism | Timeout | Scope |
|-------|-----------|---------|-------|
| 1. Per-tool | goroutine+channel (file) / idle+absolute (bash) | 30s file / 60s+30s bash | Single tool call |
| 2. Heartbeat | Stalled-progress detector | 30s × 3 stale rounds | Across LLM iterations |
| 3. Agent global | `context.WithTimeout` on Run() | GOPI_TIMEOUT_SEC (default 0=unlimited) | Entire session |

How they interact:
- A hanging `read` on a FIFO is killed at layer 1 (30s), returning an error
  to the LLM. The LLM can retry with a different path or use `bash(cat)`.
- If the LLM retries the same hanging operation repeatedly, layer 2
  (heartbeat) fires after ~90s of no task progress, injecting a reminder.
- Layer 3 provides a hard deadline for the entire session.

### Idle Timeout Polling Granularity

The bash idle monitor uses a 1-second ticker to check the last-output
timestamp. This means the effective kill time is `idleTimeout + up to 1s`.
For the default 30s idle timeout, worst case is ~31s. This is an intentional
trade-off: a tighter polling interval would increase CPU usage; 1s is
negligible overhead and 1s of extra wait is irrelevant at 30s scale.

### Bash Timeout Priority

When both absolute and idle timeouts could fire near-simultaneously, the
absolute timeout check (`ctx.Err() == DeadlineExceeded`) takes priority.
In practice, the idle timeout (30s) always fires well before the absolute
timeout (60s) for stalled commands, so this race is irrelevant.

## Not Done / Future Considerations

- The goroutine from a timed-out file operation leaks until it completes
  (or hangs forever). This is harmless for short-lived agents but could
  accumulate in a long-running server mode.
- No per-tool timeout override in the agent loop's `executeTool` — all
  file tools share the same timeout. This could be refined if needed.
- No cumulative timeout tracking across multiple tool calls in a single
  iteration.
