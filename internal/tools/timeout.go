// Package tools implements the tool functions (read, write, edit, bash) used by
// the GoPi coding agent's ReAct loop.
package tools

import (
	"os"
	"strconv"
	"time"
)

// Timeout constants for all four tools.
// These are defaults; env vars provide overrides in exactly one place (Rule 2).
//
// Design rationale:
//   - File tools (read/write/edit) use a goroutine+channel pattern because Go's
//     os.File Read/Write do not accept a context. The operation runs in a
//     goroutine; the caller returns on timeout, leaving the goroutine to
//     eventually complete (or hang harmlessly).
//   - Bash uses an absolute timeout (DefaultBashTimeout) plus an idle-output
//     timeout (DefaultBashIdleTimeout). The process is killed when either fires.
//     The idle timeout catches interactive prompts (ssh, sudo, passwd, read)
//     and infinite-output commands (ping, yes, find /) faster than the absolute
//     timeout alone.
const (
	DefaultFileToolTimeout = 30 * time.Second // read, write, edit
	// DefaultBashTimeout is the absolute max for bash when the LLM omits
	// timeout_sec. Mirrored by bashDefaultTimeoutSec in bash.go (seconds) and
	// by the "default": 120 in the bash tool schema in internal/agent/loop.go.
	// Env override: GOPI_BASH_TIMEOUT_SEC (read once in init below).
	DefaultBashTimeout = 120 * time.Second
	// DefaultBashIdleTimeout is the no-output safety net: a command that
	// produces no output for this long is killed even under timeout_sec=0
	// (unlimited absolute time). Mirrored by bashNoOutputTimeoutSec in
	// bash.go (seconds). Env override: GOPI_BASH_IDLE_SEC (read once in
	// init below). 120s per bug026: 60s of silence still killed long silent
	// ops like a 2-minute offline go build mid-flight.
	DefaultBashIdleTimeout = 120 * time.Second
)

// resolvedTimeout holds the runtime timeout values after env-var overrides.
// Populated once via init() so env vars are read exactly once (Rule 2).
var resolvedTimeout struct {
	fileToolTimeout time.Duration
	bashTimeout     time.Duration
	bashIdleTimeout time.Duration
}

func init() {
	resolvedTimeout.fileToolTimeout = DefaultFileToolTimeout
	resolvedTimeout.bashTimeout = DefaultBashTimeout
	resolvedTimeout.bashIdleTimeout = DefaultBashIdleTimeout

	if v := os.Getenv("GOPI_TOOL_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			resolvedTimeout.fileToolTimeout = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("GOPI_BASH_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			resolvedTimeout.bashTimeout = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("GOPI_BASH_IDLE_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			resolvedTimeout.bashIdleTimeout = time.Duration(n) * time.Second
		}
	}
}

// FileToolTimeout returns the configured timeout for read/write/edit operations.
func FileToolTimeout() time.Duration {
	return resolvedTimeout.fileToolTimeout
}

// BashTimeout returns the configured absolute timeout for bash commands.
func BashTimeout() time.Duration {
	return resolvedTimeout.bashTimeout
}

// BashIdleTimeout returns the configured idle-output timeout for bash commands.
func BashIdleTimeout() time.Duration {
	return resolvedTimeout.bashIdleTimeout
}
