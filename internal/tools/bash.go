package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/scratch"
)

const (
	// maxOutputSize is the hard cap on combined stdout+stderr returned to
	// the LLM (100KB) — beyond it the output is truncated with a warning.
	maxOutputSize = 100 * 1024
	// softOutputSize is the SOFT cap (8KB): outputs above it are truncated
	// early WITH a hint telling the model to narrow its command. A full
	// 100KB result is ~25K tokens shoved into the next request — the
	// measured context-growth driver — so large outputs should never be
	// returned whole. 8KB (was 16KB — R1, devdoc/perf-vs-claude-fix-plan.md):
	// halves the per-call context growth at the source; the hint already
	// teaches the model to narrow commands.
	softOutputSize = 8 * 1024
)

// bashDefaultTimeoutSec is the per-call absolute timeout (in seconds) the LLM
// gets when it omits timeout_sec on a bash tool call. The runtime default is
// BashTimeout() (DefaultBashTimeout = 120s, overridable via
// GOPI_BASH_TIMEOUT_SEC in timeout.go — Rule 2: exactly one env read site).
// Kept in sync with DefaultBashTimeout in timeout.go and the "default": 120
// in the bash tool schema (internal/agent/loop.go).
const bashDefaultTimeoutSec = 120

// bashNoOutputTimeoutSec is the idle-output safety net (in seconds): a bash
// command that produces no output for this long is killed even when the LLM
// requested timeout_sec=0 (unlimited absolute time). A silent process is
// almost always a stuck one (interactive prompt, deadlock) — 120s of silence
// means the process is hung regardless of the requested budget. Kept in sync
// with DefaultBashIdleTimeout in timeout.go.
const bashNoOutputTimeoutSec = 120

// bashPipeGraceAfterKill is how long the tool waits for the output pipes to
// reach EOF AFTER it has delivered a kill to the command's process tree.
//
// WHY IT MUST EXIST (measured 2026-10-31/11-01, task 01a1911e): step 9 waited on
// `<-readDone; <-readDone` with NO bound, and EOF only arrives when every holder
// of the pipe's WRITE end closes it. A descendant that leaves the process tree —
// `setsid … &` reparents it to init, so bashDescendantPIDs can no longer find it
// — keeps that write end open forever. At that moment the absolute budget and
// the idle net have BOTH already fired (the budget is what killed the child, and
// the idle monitor returns immediately after its own kill), so nothing else is
// left to fire: the call sits there with no deadline at all. That is the
// "no timeout mechanism" the epic describes, and the mechanism is here — the nets
// DID fire; the pipe wait after them is what has no bound.
//
// The window is deliberately short: by the time a kill has landed the tree is
// dead, so any bytes still in flight are already in the kernel buffer and are
// drained well inside it. It is NOT a poll interval and NOT a keep-alive — the
// tool closes the pipe READ ENDS it owns and returns.
const bashPipeGraceAfterKill = 2 * time.Second

// bashPipeRetainedNoticeFragment is the stable fragment the result carries when
// the tool released a pipe out from under a surviving descendant. It is a named
// constant because the regression test asserts on it (Rule 1/13: one definition
// site, and a test that reads a bare literal cannot tell a wording change from a
// behaviour change).
const bashPipeRetainedNoticeFragment = "output pipe still held"

// bashPipeRetainedNoticeFmt is the full sentence appended to the result (Rule
// 15: a pipe released out from under a survivor is a FACT about the output — the
// bytes after it are missing — so it is stated, never swallowed).
const bashPipeRetainedNoticeFmt = "\n[%s after the command was killed: a descendant left the process tree and kept the output pipe open; the tool released the pipe after %s]"

// The three ways a bash command DIES under our hand, each a NAMED reason so the
// attribution cannot be inferred from a symptom (Rule 1/13/14: one definition,
// referenced everywhere, never a bare literal). The reason slot holds exactly
// one of them, because an operator's cancellation and an expired budget are
// DIFFERENT FACTS about the run: one says the human stopped it, the other says
// the budget was too small. A result that cannot tell them apart explains
// nothing to the model that has to decide what to do next.
const (
	// KillReasonOperatorCancel is the operator pressing Escape/Ctrl+C: the RUN's
	// context was cancelled (agent.InterruptRun), which is UNCONDITIONAL — the
	// command dies now, with no grace period and no second question.
	KillReasonOperatorCancel = "operator-cancel"
	// KillReasonAbsoluteTimeout is the per-call budget expiring (timeout_sec, or
	// BashTimeout() when the LLM omitted it).
	KillReasonAbsoluteTimeout = "absolute-timeout"
	// KillReasonIdleTimeout is the no-output safety net expiring: the process
	// produced nothing for idleTimeout.
	KillReasonIdleTimeout = "idle-timeout"
)

// BashCancelledMarker is the LAST line of a result whose command the operator
// cancelled. It is exported because the reader that must not mistake a
// cancellation for a timeout lives in another package (internal/agent), and two
// spellings of the same sentence would drift (Rule 14).
const BashCancelledMarker = "[command cancelled by the operator]"

// killReasonSlot is one command's kill attribution: the reason, or empty when
// nobody killed it (the command exited on its own), written by whichever watcher
// fired.
//
// It replaces the idleKilled atomic.Int32 the idle monitor used to set, and it is
// deliberately a REASON and not a flag: the flag could only answer "was this the
// idle net?", which cannot express "the operator pressed Escape" — the whole
// point of routing the caller's context here. FIRST writer wins (set ignores a
// second reason), because two watchers can fire in the same instant and "which
// one did it" must not depend on which goroutine happened to win the race.
type killReasonSlot struct {
	mu     sync.Mutex
	reason string
}

// set records the reason. FIRST writer wins: the watchers race, and the first
// kill delivered is the one that actually ended the command — a later watcher
// that fires in the same instant observes the process already dying and must
// not relabel it.
func (k *killReasonSlot) set(reason string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.reason == "" {
		k.reason = reason
	}
}

// get returns the recorded reason ("" = none).
func (k *killReasonSlot) get() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.reason
}

// RejectDangerous controls whether dangerous command patterns are rejected.
// It defaults to TRUE (the safe default — the Go zero value false would leave
// rm -rf /, mkfs, sudo, etc. unguarded). The escape hatches are:
//
//   - GOPI_ALLOW_DANGEROUS=1 disables ALL dangerous-command rejection (legit
//     user-initiated cleanup / disk operations). Read once in init() — Rule 2:
//     exactly one env read site.
//   - BashInDir / BashWithTimeoutInDir workdir: a dangerous command is still
//     rejected unless every path target stays inside the working directory
//     (the cwd whitelist) — see rejectDangerous below.
//
// Tests that need deterministic control set this var directly.
var RejectDangerous = true

// escapeDangerous is set once from the GOPI_ALLOW_DANGEROUS env var in init()
// (Rule 2: single source of truth for the escape hatch). When true, dangerous
// commands are allowed regardless of the cwd whitelist. The scheduler that
// drives the agent sets it explicitly for legitimate cleanup scenarios.
var escapeDangerous bool

// bashDefaultLocale is the locale pinned onto every bash child process. "C"
// keeps coreutils output byte-stable across hosts (English diagnostics,
// predictable sorting) — the wire contract the agent's own tests and parsers
// rely on.
const bashDefaultLocale = "C"

// bashChildEnv assembles the child environment: the parent's environment, with
// the locale variables overridden to bashDefaultLocale and the wrapper-child
// identity STRIPPED (childenv.go — a spawned shell must never inherit the right
// to write ledger telemetry under the wrapper row). GOPI_BASH_LOCALE overrides
// the default (Rule 2: exactly one env read site — here).
func bashChildEnv() []string {
	locale := bashDefaultLocale
	if v := os.Getenv("GOPI_BASH_LOCALE"); v != "" {
		locale = v
	}
	base, removed := stripWrapperIdentity(os.Environ())
	// THE STRIP IS ANNOUNCED (Rule 15: never silent). A shell that quietly lost
	// GOPI_HOME would resolve the operator's own profile and file its state
	// where nobody looks for it; the record names the VALUE and the reason, and
	// it is emitted only when the checkout marker was actually found, so an
	// ordinary run prints nothing.
	fmt.Fprint(os.Stderr, checkoutHomeStripRecord(removed, os.Environ()))
	env := make([]string, 0, len(base)+2)
	for _, kv := range base {
		if strings.HasPrefix(kv, "LANG=") || strings.HasPrefix(kv, "LC_ALL=") ||
			strings.HasPrefix(kv, "LC_MESSAGES=") || strings.HasPrefix(kv, "LANGUAGE=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "LANG="+locale, "LC_ALL="+locale)
}

func init() {
	// GOPI_ALLOW_DANGEROUS=1 escapes the dangerous-command guard entirely. The
	// value only ever switches the guard ON→OFF (any non-"1" value keeps it on).
	if v := os.Getenv("GOPI_ALLOW_DANGEROUS"); v == "1" {
		escapeDangerous = true
	}
}

// dangerousPattern is one rejected pattern plus whether the cwd whitelist
// can approve it. A whitelistable pattern has a path target that can
// legitimately live under the working directory (rm -rf ./build/); a
// non-whitelistable one is dangerous regardless of target (sudo escalates
// privileges; "> /dev/sd*" writes a raw disk, which never lives under a
// workdir) — always rejected unless the escape hatch is open.
type dangerousPattern struct {
	pattern       string
	whitelistable bool
}

// dangerousPatterns is the ordered list of dangerous patterns. The whitelist
// decision itself is made by rejectDangerous: when a whitelistable pattern
// matches, every path-like token in the command must resolve inside the
// working directory.
//
//   - "rm -rf" with a / target touches the filesystem root; with "./..." it
//     cleans within the workdir and is whitelistable.
//   - mkfs/chmod -R/chown -R targets are path-based: a file image or tree
//     inside the workdir is a legitimate target, a device or /etc is not.
//   - "if=/dev/" and "of=/dev/" dd operands touch block devices — matched
//     WITHOUT the "dd" prefix so option order cannot hide them
//     ("dd if=./x of=/dev/sda" still matches); whitelistable ONLY through
//     the pseudo-device exemption (reading /dev/zero or writing /dev/null
//     is a harmless byte-stream idiom, never a raw disk).
//   - "sudo" escalates to root — never whitelisted by cwd.
//   - "> /dev/sd" redirects to a raw disk — never whitelisted.
var dangerousPatterns = []dangerousPattern{
	{pattern: "rm -rf", whitelistable: true},     // recursive removal
	{pattern: "mkfs.", whitelistable: true},      // filesystem creation
	{pattern: "chmod -R ", whitelistable: true},  // recursive permission changes
	{pattern: "chown -R ", whitelistable: true},  // recursive ownership changes
	{pattern: "if=/dev/", whitelistable: true},   // dd raw device read
	{pattern: "of=/dev/", whitelistable: true},   // dd raw device write
	{pattern: "sudo ", whitelistable: false},     // privilege escalation
	{pattern: "> /dev/sd", whitelistable: false}, // raw disk redirection
}

// Bash executes a shell command via bash -c and returns a formatted result
// string. The result always begins with "exit=N\n" followed by the command's
// combined stdout and stderr.  Stderr lines are prefixed with "[stderr] ".
//
// The command is subject to two timeouts:
//  1. Absolute timeout (default 120s, GOPI_BASH_TIMEOUT_SEC) — kills the
//     process after this duration regardless of output. Use BashWithTimeout
//     to set a per-call absolute timeout (the LLM's timeout_sec parameter).
//  2. Idle-output timeout (default 120s, GOPI_BASH_IDLE_SEC) — kills the
//     process if no stdout/stderr output is received for this duration.
//     This catches interactive prompts (ssh, sudo, passwd, read) and
//     infinite-output commands (ping, yes, find /) without waiting for
//     the full absolute timeout.
//
// Output exceeding 100KB is truncated with a warning.  Empty commands are
// rejected.  When RejectDangerous is true, dangerous command patterns are
// also rejected.
//
// On timeout or validation failure, a non-nil error is returned alongside any
// partial output.  A non-zero exit code from the command itself is reported in
// the result string and does NOT produce a Go error — callers should inspect
// the exit=N prefix.
func Bash(command string) (string, error) {
	return bashInDir(command, "", BashTimeout(), BashIdleTimeout())
}

// BashInDir is Bash with an explicit working directory: the command runs in
// workdir instead of the agent's cwd. workdir must be an existing directory
// (validated before execution); empty workdir means the current directory,
// identical to Bash.
func BashInDir(command string, workdir string) (string, error) {
	return bashInDir(command, workdir, BashTimeout(), BashIdleTimeout())
}

// BashWithTimeout executes a shell command via bash -c with an LLM-specified
// absolute timeout in seconds. timeoutSec semantics:
//
//   - timeoutSec > 0: absolute kill after timeoutSec seconds. The idle-output
//     timeout is capped at min(DefaultBashIdleTimeout, timeoutSec) so the
//     requested budget remains a strict upper bound on wall-clock time.
//   - timeoutSec == 0: unlimited absolute time (no deadline). The idle-output
//     safety net (DefaultBashIdleTimeout, 120s of silence) still applies.
//   - timeoutSec < 0: invalid — falls back to the default BashTimeout().
//
// Env-var precedence (Rule 2): timeout_sec arg (LLM) > GOPI_BASH_TIMEOUT_SEC
// (env, read only in timeout.go init) > DefaultBashTimeout (120s).
func BashWithTimeout(command string, timeoutSec int) (string, error) {
	return bashWithTimeoutInDir(command, timeoutSec, "")
}

// BashWithTimeoutInDir combines the timeout_sec semantics of BashWithTimeout
// with an explicit working directory (see BashInDir for workdir rules).
func BashWithTimeoutInDir(command string, timeoutSec int, workdir string) (string, error) {
	return bashWithTimeoutInDir(command, timeoutSec, workdir)
}

// BashInDirCtx is BashInDir bound to a CALLER-SCOPED context: the child is
// killed the moment ctx is cancelled, exactly as it is killed when a timeout
// fires. It is the entry point a run-scoped context reaches bash through, and
// the reason it exists is measured: until it arrived, bashInDir built its own
// root with context.Background(), so the operator's Escape/Ctrl+C — which
// cancels the RUN's context (agent.InterruptRun / runScopedRoot) — was inert
// against a command already executing. The turn unwound; the command ran on.
//
// A cancellation is UNCONDITIONAL (never resumed, never asked twice): the
// operator's key means STOP, not "stop when convenient". The result's last line
// is BashCancelledMarker and the returned error names the same thing, so a
// cancelled command is never read as a timeout or as a command's own failure.
//
// ctx == nil means "no caller scope" and behaves exactly like BashInDir (the
// thin wrappers below rely on that, which is what keeps the old signatures
// honest rather than a second implementation).
func BashInDirCtx(ctx context.Context, command string, workdir string) (string, error) {
	return bashInDirCtx(ctx, command, workdir, BashTimeout(), BashIdleTimeout())
}

// BashWithTimeoutInDirCtx is BashWithTimeoutInDir with a caller-scoped context:
// the timeout_sec budget and the caller's cancellation are BOTH honored, and
// whichever arrives first kills the command. The two deaths stay distinguishable
// (KillReasonAbsoluteTimeout vs KillReasonOperatorCancel) because they are
// different facts about the run: one means the budget was too small, the other
// means a human stopped it.
func BashWithTimeoutInDirCtx(ctx context.Context, command string, timeoutSec int, workdir string) (string, error) {
	abs, idle := bashBudget(timeoutSec)
	return bashInDirCtx(ctx, command, workdir, abs, idle)
}

// bashWithTimeoutInDir is the shared implementation of the timeout variants.
func bashWithTimeoutInDir(command string, timeoutSec int, workdir string) (string, error) {
	abs, idle := bashBudget(timeoutSec)
	return bashInDir(command, workdir, abs, idle)
}

// bashBudget maps the LLM's timeout_sec to the (absolute, idle) pair. It is the
// ONE place that translation happens, so the ctx and non-ctx entries cannot
// drift apart (Rule 14): >0 is a real budget with the idle net capped by it, 0
// is unlimited absolute time (idle net only), <0 falls back to the default.
func bashBudget(timeoutSec int) (abs, idle time.Duration) {
	idle = BashIdleTimeout()
	if timeoutSec < 0 {
		return BashTimeout(), idle
	}
	if timeoutSec == 0 {
		return 0, idle
	}
	abs = time.Duration(timeoutSec) * time.Second
	if idle > abs {
		idle = abs
	}
	return abs, idle
}

// killReasonIdleOrBudget is the attribution rule for a kill that came due on the
// idle net, and it answers from the BUDGET SHAPE — never from which watcher
// reached the kill slot first.
//
// The race it removes: bashBudget clamps the idle net down to the absolute
// budget (idle = min(idle, abs)), so with timeout_sec=1 the net and the budget
// come due in the SAME second and the two watchers (step 6b and step 8) arrive
// in whatever order the scheduler picks. Deciding the wording at the kill site
// from cmdCtx.Err() made that order load-bearing and the message flaky: the
// same call reported "no output for 1s" or "timed out after 1s" run to run, and
// the idle wording tells the model the command was merely silent rather than
// out of budget (measured: TestBashWithTimeout_PositiveKills, -count=20 red).
//
// Both branches below state a fact about the budgets that does not depend on
// arrival order, and both watchers evaluate the same one from the same moment:
//
//   - idleTimeout == absTimeout: the clamp IS the proof. bashBudget is the ONE
//     place raising a timeout lowers the net (Rule 14), so an equal pair means
//     the net was clamped (or the operator configured the two to be equal) and
//     the budget expired exactly when the net did. True even before the
//     deadline is observable through cmdCtx.
//   - an UNCAPPED net (idle < abs) that fires with its own deadline already
//     passed: same death, same honest answer.
//
// Anything else is the idle net doing its own job — silence well before the
// budget — and keeps the "no output for" wording. An unlimited command
// (absTimeout == 0) can only ever reach the second branch's false side.
func killReasonIdleOrBudget(cmdCtx context.Context, absTimeout, idleTimeout time.Duration) string {
	if idleTimeout == absTimeout {
		return KillReasonAbsoluteTimeout
	}
	if absTimeout > 0 && cmdCtx.Err() == context.DeadlineExceeded {
		return KillReasonAbsoluteTimeout
	}
	return KillReasonIdleTimeout
}

// bash executes a shell command with the given absolute and idle timeouts in
// the current working directory.
func bash(command string, absTimeout, idleTimeout time.Duration) (string, error) {
	return bashInDir(command, "", absTimeout, idleTimeout)
}

// bashInDir executes a shell command in the given working directory with the
// given absolute and idle timeouts, WITHOUT a caller scope (context.Background
// root — the historical behavior, kept as the thin wrapper of bashInDirCtx).
func bashInDir(command string, workdir string, absTimeout, idleTimeout time.Duration) (string, error) {
	return bashInDirCtx(context.Background(), command, workdir, absTimeout, idleTimeout)
}

// bashInDirCtx executes a shell command in the given working directory with the
// given absolute and idle timeouts, killing it when ANY of the three deaths
// arrives: the caller's ctx (KillReasonOperatorCancel), the absolute budget
// (KillReasonAbsoluteTimeout), or idle silence (KillReasonIdleTimeout).
func bashInDirCtx(ctx context.Context, command string, workdir string, absTimeout, idleTimeout time.Duration) (string, error) {
	// 1. Validate: reject empty commands.
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("empty command")
	}
	// 1a. Keep the command AS THE CALLER WROTE IT. Step 2f REWRITES the text
	//     (it injects `--trailer`), and the post-run notice in step 14 asks
	//     whether this command created a commit through the SAME scanner the
	//     rewrite uses — which, by design, answers "no insertion point" for a
	//     clause that already carries a trailer. Scanning the rewritten text
	//     therefore asked a question about a string the caller never wrote, and
	//     answered it wrong (measured: the notice never fired on the one
	//     command shape it exists for).
	callerCommand := command

	// 1b. Validate the optional workdir: must be an existing directory when
	//     set. Empty means the current working directory (default behavior).
	if workdir != "" {
		workdir = expandTilde(workdir)
		info, err := os.Stat(workdir)
		if err != nil {
			return "", fmt.Errorf("workdir %q does not exist: %w", workdir, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("workdir %q is not a directory", workdir)
		}
	}

	// 2. Reject dangerous patterns unless the escape hatch is open.
	if RejectDangerous && !escapeDangerous {
		if reason := rejectDangerous(command, workdir); reason != "" {
			return "", fmt.Errorf("dangerous command rejected: %s", reason)
		}
	}

	// 2b. Shape policy: a tree-level create/destroy must target a DECLARED
	//     root. This is the second, property-based half of the guard — the
	//     pattern list above can only refuse what it lists, while this refuses
	//     by PATH SHAPE (a worktree created outside its repo's declared root, a
	//     recursive delete/chown/mount/dd of a path outside every declared
	//     root). It shares the escape hatch, because an operator who opens
	//     GOPI_ALLOW_DANGEROUS is deliberately taking the blast radius.
	if RejectDangerous && !escapeDangerous {
		if reason := rejectOutsideDeclaredRoots(command, workdir); reason != "" {
			return "", fmt.Errorf("dangerous command rejected: %s", reason)
		}
	}

	// 2c. Shared-stash guard: a stash MUTATION in a repo with more than one
	//     linked worktree writes this checkout's index/worktree from a stack
	//     every worktree shares (`refs/stash` is in the common git dir), so
	//     another session's WIP lands here — an interrupted apply/pop leaving
	//     `UU` over files this branch owns. It shares the escape hatch, and a
	//     single-worktree repo (the ordinary local stash) is never refused.
	//     See shared_stash_guard.go for the measured incident family.
	if RejectDangerous && !escapeDangerous {
		if reason := guardSharedStash(command, workdir); reason != "" {
			return "", fmt.Errorf("dangerous command rejected: %s", reason)
		}
	}

	// 2d. Shared-integration-ref guard: `refs/heads/*` lives in the repository's
	//     COMMON git dir, so every linked worktree resolves the SAME branch refs.
	//     A git invocation that moves a branch ANOTHER worktree has checked out
	//     rewrites that checkout's branch while its index keeps the old tree —
	//     `git status` then renders the difference as a large staged diff and
	//     PARKS the merge queue (Rule 30). The scope is the whole refs/heads
	//     family, MEASURED on git 2.34.1: `update-ref` (incl. `-d`),
	//     `symbolic-ref`, `branch -f`/`--force`/`-M`/`-C`, `checkout -B`,
	//     `switch -C`/`--force-create`, and a `fetch` whose refspec destination
	//     (or configured `remote.<r>.fetch`) is a branch ref. Git's OWN refusal
	//     covers only part of it and keys on two different conditions
	//     (`branch -D` / `worktree add -B` refuse a branch held by ANOTHER
	//     worktree; `branch -f <own>` / `fetch` into it refuse the CALLER'S OWN
	//     branch), so every verb above still lands silently. It shares the
	//     escape hatch, and a single-worktree repo is never refused. See
	//     shared_integration_ref_guard.go for the measured incident and the
	//     full census table.
	if RejectDangerous && !escapeDangerous {
		if reason := guardSharedIntegrationRef(command, workdir); reason != "" {
			return "", fmt.Errorf("dangerous command rejected: %s", reason)
		}
	}

	// 2e. Clone-channel publish-target guard: a ground created by the clone
	//     channel is a CLONE standing on the INTEGRATION branch, and its
	//     delivery loop already publishes that branch. A hand-rolled
	//     `git push <remote> HEAD:<other name>` then writes a ref the channel
	//     never creates (or, from a checkout that is not the integration
	//     branch, MOVES the server's integration branch under
	//     receive.denyCurrentBranch=updateInstead). Measured: four such refs on
	//     the server, each one `push` in its reflog, each matching the run's own
	//     logged command — see git_publish_target_guard.go. It shares the escape
	//     hatch, and a worktree ground / a command with no announced ground is
	//     never refused.
	if RejectDangerous && !escapeDangerous {
		if reason := guardCloneGroundPublishTarget(command, workdir); reason != "" {
			return "", fmt.Errorf("dangerous command rejected: %s", reason)
		}
	}

	// 2e-bis. Clone-channel BRANCH-CREATE guard (measured 2026-11-01, row
	//     01a1957a-0e54): the run that owned a clone ground ran
	//     `cd <shared checkout> && git checkout -b task/<id>` in the repo's MAIN
	//     checkout, and the tree it parked there was real work (its commit is
	//     byte-identical to the fix that landed on master that evening). The
	//     briefing channels that forbid it are all TEXT — announcement, publish
	//     clause, authority clause, Rule 6a — and every one of them was in that
	//     run's briefing. This is the half a machine owns: a branch CREATION in a
	//     declared shared checkout, by a process that has a ground of its own, is
	//     refused with the ground named. It shares the escape hatch, and a
	//     process with no announced ground / a creation inside its own ground /
	//     an undeclared directory is never refused. See
	//     clone_ground_branch_create_guard.go.
	if RejectDangerous && !escapeDangerous {
		if reason := guardCloneGroundBranchCreate(command, workdir); reason != "" {
			return "", fmt.Errorf("dangerous command rejected: %s", reason)
		}
	}

	// 2f. Two references on every commit this command creates: a `git commit`
	//     becomes `git commit --trailer 'task: <row>' --trailer 'host: <machine>'`,
	//     so `git log --grep` resolves commit → row AND commit → host with no
	//     table and no index file (Rule 35/5b). ONE FLAG PER LINE is load-bearing:
	//     the space-merged `--trailer 'task: <row> host: <machine>'` writes ONE
	//     body line and both greps miss (measured; see
	//     git_commit_task_trailer.go). The host line is added even
	//     when no ledger row was published — a chat's commits still come from
	//     some machine. It runs AFTER the guards, so every guard judges the
	//     command the caller wrote, and the injection is announced on stderr
	//     with EVERY line it added (Rule 15: the tool never silently runs
	//     something other than what was asked). See
	//     git_commit_task_trailer.go for the measurements behind the spelling,
	//     for what it deliberately does not cover, and for why this seam is
	//     the enforced placement rather than a prose rule.
	if rewritten, lines, injected := gitCommitTaskTrailerRewrite(command); injected {
		fmt.Fprintf(os.Stderr, gitCommitTrailerNoticeFmt, strings.Join(lines, " + "))
		command = rewritten
	}

	// 3. Expand ~/ in paths.
	command = expandTilde(command)

	// 4. Derive the COMMAND's context from the CALLER's scope and layer the
	//    absolute budget on it. This is the whole point of the ctx entry point
	//    (BashInDirCtx): before it existed this line read
	//    context.Background() — a root of bash's own making — so the operator's
	//    Escape/Ctrl+C (which cancels the RUN's context) was inert against a
	//    command already executing: the turn unwound, the command ran on.
	//
	//    absTimeout == 0 means unlimited (no deadline): the LLM asked for
	//    timeout_sec=0. context.WithTimeout(ctx, 0) would fire IMMEDIATELY
	//    (ctx.Err() == DeadlineExceeded at t≈0) and the watcher would SIGKILL the
	//    group instantly — so 0 is special-cased to a plain cancellable context
	//    that STILL CARRIES the caller's cancel. The idle monitor (step 8)
	//    remains the only safety net for the unlimited case.
	//
	//    Attribution is then read off cmdCtx.Err() and is exact rather than
	//    inferred: DeadlineExceeded is OUR budget expiring, Canceled is the
	//    caller's scope (the operator's key) reaching us.
	callerCtx := ctx
	if callerCtx == nil {
		callerCtx = context.Background()
	}
	var cmdCtx context.Context
	var cancel context.CancelFunc
	if absTimeout > 0 {
		cmdCtx, cancel = context.WithTimeout(callerCtx, absTimeout)
	} else {
		cmdCtx, cancel = context.WithCancel(callerCtx)
	}
	defer cancel()

	// kills records WHY this command was killed, written by whichever watcher
	// fires first and read once, after Wait(), to choose the result's wording —
	// so the three deaths can never be reported as one another.
	var kills killReasonSlot
	// killMu orders "the child has been REAPED" (step 10) against "a signal is
	// about to be sent" (steps 6b/8). Without it the deferred cancel() — which
	// closes cmdCtx on every return, the normal one included — could reach the
	// watcher between Wait() and the return, and the signal would address a pid
	// the kernel may already have handed to another process.
	var killMu sync.Mutex
	// commandExited is set as soon as Wait() has reaped the child; see killMu.
	var commandExited atomic.Bool

	// 5. Create command with pipe-based I/O for idle monitoring.
	//
	// The resource clamp rides in the command string: `ulimit` is a bash
	// builtin, so it cannot be a separate command and the limit must be set by
	// the same shell that then runs the command (which is what makes it reach
	// every descendant). The command text itself is preserved verbatim after
	// the preamble, and the preamble ends in ";" so a limit the child's own
	// shell declines to lower cannot stop the command.
	cmd := exec.CommandContext(cmdCtx, "bash", "-c", scratch.ChildLimitPreamble()+command)
	if workdir != "" {
		cmd.Dir = workdir
	}
	// Pin the C locale for the child process (Rule 2: default here, the
	// single override point is GOPI_BASH_LOCALE). Without this the child
	// inherits the host LANG (e.g. zh_CN.UTF-8) and coreutils localize their
	// output — `ls /nonexistent` returns 无法访问 instead of "cannot access"
	// — which broke every agent-side assertion on real command output
	// (TestAgent_Run_ContextTrimReplaceSlimming / ToolResultPayloadOnce,
	// pre-existing red on clean master 56fd84a). The agent's own parsing
	// (isNonZeroExit's "exit=" anchor) is locale-independent, but ANY
	// locale-dependent wire text is a cross-machine behavioral hazard.
	cmd.Env = bashChildEnv()
	// Isolate the command in its own process group so a timeout can kill
	// the WHOLE group, not just bash. Without this, a background daemon
	// spawned via `nohup ... &` is a grandchild that inherits our
	// stdout/stderr pipe write-ends: killing only bash leaves the pipes
	// open forever, so the read goroutines below never see EOF and
	// bash() deadlocks (bug011 — production hang of 2.5+ hours).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// 6. Start the command.
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("command start failed: %w", err)
	}

	// 6b-2. THE TWO FACTS A KILL REPORT NEEDS (bug row 01a1d278).
	//
	// bashEscapedPipeInodes is the SET OF PIPE INODES this call opened the read
	// ends of. A process holding a WRITE-side descriptor on one of them is, by
	// the kernel's own accounting, still attached to THIS call — and that is the
	// only link that survives `setsid`: setsid(2) reparents the survivor, so the
	// ancestry walk in killChildTree can never find it (which is exactly why a
	// foreground `make test-budget` reported "killed and over" while its suite
	// kept running and holding the per-package lock).
	bashEscapedPipeInodes := map[uint64]bool{}
	for _, ino := range []uint64{
		bashPipeInodeOf(stdoutPipe.(interface{ Fd() uintptr })),
		bashPipeInodeOf(stderrPipe.(interface{ Fd() uintptr })),
	} {
		if ino != 0 {
			bashEscapedPipeInodes[ino] = true
		}
	}
	// The child's OWN session, read before any kill: it is what turns "this pid
	// holds my pipe" into "this pid LEFT the child's tree" in the report. With
	// Setpgid the child leads its own GROUP, but its SESSION is still the
	// caller's — so a survivor in a different session is one that called
	// setsid(2). An unreadable value leaves the field unmeasured (Rule 24: the
	// identity is /proc evidence, never a guess).
	bashChildSid, bashChildSidKnown := 0, false
	if sid, ok := procname.ProcSessionKey(procRootName, strconv.Itoa(cmd.Process.Pid)); ok {
		bashChildSid, bashChildSidKnown = sid, true
	}

	// 6b. Watch for the command's context. exec.CommandContext's internal
	//     kill-on-ctx-done goroutine kills ONLY the bash process; a daemon
	//     grandchild that inherited our pipes would survive it and keep the
	//     pipe write-ends open, deadlocking the reads below (bug011). So when
	//     the context fires we take the whole PROCESS TREE down ourselves.
	//
	//     ONE watcher for all three deaths, because the ctx is the single fact
	//     that carries them: the caller's cancel (operator) and OUR budget
	//     (absolute timeout) both arrive here, and cmdCtx.Err() names which.
	//     The idle net has its own watcher (step 8) since it is a wall-clock
	//     observation, not a context event — it records its reason through the
	//     same slot.
	//
	//     The reason is recorded BEFORE the signal, so a reader that sees the
	//     command die always has the attribution that killed it.
	execDone := make(chan struct{})
	defer close(execDone)
	go func() {
		select {
		case <-cmdCtx.Done():
			// commandExited is consulted under killMu: the deferred cancel()
			// above closes cmdCtx on EVERY return, so this branch fires on the
			// normal path too, and a kill aimed at a REAPED pid could land on
			// an unrelated process that reused the number. The mutex orders
			// "the child has been reaped" against "the signal is sent".
			killMu.Lock()
			exited := commandExited.Load()
			reason := cmdCtx.Err()
			killMu.Unlock()
			if exited || cmd.Process == nil {
				return
			}
			if reason == context.DeadlineExceeded {
				// OUR budget expired — nowhere else could this deadline come
				// from (cmdCtx is the only context carrying one).
				kills.set(KillReasonAbsoluteTimeout)
			} else {
				// The operator's scope reached us: the operator pressed
				// Escape/Ctrl+C. Unconditional kill, no grace period.
				kills.set(KillReasonOperatorCancel)
			}
			killChildTree(cmd.Process.Pid)
		case <-execDone:
			// The command finished normally; nothing to kill.
		}
	}()

	// 7. Read stdout and stderr in goroutines, tracking last output time.
	//
	// lastOutputNano is an atomic nanosecond timestamp of the last byte read
	// from either pipe. The idle monitor goroutine polls this and kills the
	// process when it exceeds idleTimeout, recording KillReasonIdleTimeout so
	// the three deaths stay distinguishable.
	var lastOutputNano atomic.Int64
	lastOutputNano.Store(time.Now().UnixNano())

	// The two stream accumulators are BOUNDED (bounded_output.go + the
	// pipeRetainedTail* constants): an unbounded buffer here is what aborted
	// the whole test binary on an unbounded-output command — see
	// pipeRetainedTailSize for the measured incident. The window is the TAIL,
	// i.e. exactly the bytes buildOutput keeps, so the rendered result is
	// unchanged in every case (what is bounded is memory, not the result).
	stdoutBuf := newBoundedTailWriter(pipeRetainedTailSize)
	stderrBuf := newBoundedTailWriter(pipeRetainedTailSize)
	readDone := make(chan struct{}, 2)

	// Goroutine: read stdout.
	go func() {
		buf := make([]byte, pipeReadBufferSize)
		for {
			n, readErr := stdoutPipe.Read(buf)
			if n > 0 {
				stdoutBuf.Write(buf[:n])
				lastOutputNano.Store(time.Now().UnixNano())
			}
			if readErr != nil {
				break
			}
		}
		readDone <- struct{}{}
	}()

	// Goroutine: read stderr.
	go func() {
		buf := make([]byte, pipeReadBufferSize)
		for {
			n, readErr := stderrPipe.Read(buf)
			if n > 0 {
				stderrBuf.Write(buf[:n])
				lastOutputNano.Store(time.Now().UnixNano())
			}
			if readErr != nil {
				break
			}
		}
		readDone <- struct{}{}
	}()

	// 8. Idle monitor goroutine: periodically check last output time.
	//    If idleTimeout has elapsed with no output, kill the process.
	idleStop := make(chan struct{})
	defer close(idleStop)

	if idleTimeout > 0 {
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-idleStop:
					return
				case <-ticker.C:
					last := time.Unix(0, lastOutputNano.Load())
					if time.Since(last) > idleTimeout {
						// Idle timeout fired — kill the whole process TREE.
						// Record the reason BEFORE killing so step 12 attributes
						// the death correctly; killing only bash would leave a
						// background daemon (grandchild inheriting our pipes)
						// alive and holding the pipe write-ends, deadlocking the
						// reads (bug011).
						killMu.Lock()
						exited := commandExited.Load()
						killMu.Unlock()
						if exited || cmd.Process == nil {
							return
						}
						// WHEN THE BUDGET HAS ALSO EXPIRED, THE BUDGET IS THE
						// HONEST ATTRIBUTION — and the decision is taken from
						// the BUDGET SHAPE, not from which watcher got here
						// first (killReasonIdleOrBudget): the clamp in
						// bashBudget makes the net and the budget come due
						// together, so an attribution read off arrival order
						// would make this message flaky. The idle net's own
						// case — silence well BEFORE the budget — is untouched.
						kills.set(killReasonIdleOrBudget(cmdCtx, absTimeout, idleTimeout))
						killChildTree(cmd.Process.Pid)
						return
					}
				}
			}
		}()
	}

	// 9. Wait for both readers to finish — but NEVER unbounded after a kill.
	//
	// THE BOUND IS THE WHOLE FIX (measured 2026-10-31/11-01, task 01a1911e).
	// This was `<-readDone; <-readDone` with no deadline, and EOF only arrives
	// when EVERY holder of the pipe's write end has closed it. A descendant that
	// LEAVES the process tree — `setsid cmd &` reparents it to init, so
	// bashDescendantPIDs(pid) can no longer find it, and killChildTree has
	// nothing to signal — inherits that write end and holds it open forever. By
	// then the absolute budget and the idle net have BOTH already fired (a kill
	// is what killed the child), so nothing else is left to fire and the call
	// hangs with NO deadline at all. The epic calls that "no timeout
	// mechanism"; the measurement is narrower and more useful: the nets fired,
	// and the wait AFTER them was the unbounded rung.
	//
	// The bound applies ONLY once a kill has been DELIVERED, and the predicate
	// for that is cmdCtx.Done() — NOT `kills.get() != ""`, which is empty by the
	// time step 9 runs (the red test caught exactly that on the first attempt).
	// A command that exits on its own closes its own pipes, so the ordinary path
	// keeps its old unlimited semantics — a slow but live reader is never cut
	// off. When the grace expires the tool closes the pipe READ ends IT opened
	// (it never signals a process it does not hold — Rule 42); the surviving
	// descendant is left for the operator to kill by name, and the result says
	// so (Rule 15).
	pipeReleased := false
	// killC fires when a kill has been DELIVERED (cmdCtx.Done covers both the
	// expired budget and the operator's cancel — the same two facts step 6b
	// acts on). It is consumed once and then set to nil, so a closed channel
	// cannot spin the loop. Before it fires the wait is UNBOUNDED, which is the
	// old, correct behaviour for a command that is still alive and producing.
	killC := cmdCtx.Done()
	var graceC <-chan time.Time
	var graceTimer *time.Timer
	for readers := 0; readers < 2; {
		select {
		case <-readDone:
			readers++
		case <-killC:
			killC = nil
			graceTimer = time.NewTimer(bashPipeGraceAfterKill)
			graceC = graceTimer.C
		case <-graceC:
			// The kill landed, the tree is dead, and the pipe is STILL open: a
			// descendant escaped the tree and holds the write end. Close the
			// READ ends this tool owns — the only handle we have — and let the
			// reader goroutines fall out of their Read loop.
			readers = 2
			pipeReleased = true
			// Close the READ ends THIS tool opened. "already closed" is the
			// expected error (a reader that reached EOF first closes it), and
			// the release is reported in the result below — so a failed Close
			// loses nothing a caller could act on.
			_ = stdoutPipe.Close() //stylegate:ignore the read end this call owns; EOF-closed is the expected error
			_ = stderrPipe.Close() //stylegate:ignore the read end this call owns; EOF-closed is the expected error
		}
	}
	if graceTimer != nil {
		graceTimer.Stop()
	}

	// pipeReleaseNotice is "" on the ordinary path, and a stated FACT when the
	// tool had to close a pipe out from under a descendant that escaped the
	// tree: the bytes after that point are missing, so the caller is TOLD (Rule
	// 15) instead of being handed a stream that silently ends early.
	pipeReleaseNotice := ""
	if pipeReleased {
		pipeReleaseNotice = fmt.Sprintf(bashPipeRetainedNoticeFmt,
			bashPipeRetainedNoticeFragment, bashPipeGraceAfterKill)
	}

	// 10. Wait for the command to fully exit. commandExited is published under
	//     killMu BEFORE the deferred cancel() can be observed by the watchers
	//     (they take the same lock), so no signal can be aimed at a reaped pid.
	runErr := cmd.Wait()
	killMu.Lock()
	commandExited.Store(true)
	killMu.Unlock()

	// Drain any remaining pipe data (rare edge case where Wait returns
	// before pipe readers finish).
	// (already drained by the goroutines above)

	// 11-12. Attribute the death from the ONE record the watchers wrote, in the
	//        order the three causes are possible. The operator's cancellation is
	//        checked FIRST and is not shadowed by anything: it is the strongest
	//        statement a run can make about a command ("a human stopped this"),
	//        and a budget that expired in the same instant must not be allowed to
	//        relabel it. Each branch returns the wording its own cause deserves.
	//
	//        WHAT THE KILL LEFT BEHIND (bug row 01a1d278) rides all three: a kill
	//        reports what it STOPPED, and until now it could not report what it
	//        did NOT stop. The measurement is taken HERE, once, after the tree
	//        kill and cmd.Wait(): a process still holding a WRITE end of this
	//        call's own pipes was measured outside the killed tree — which is the
	//        whole difference between "your command is over" and "your command's
	//        work is still running and holding a lock you will meet next run".
	killReason := kills.get()
	survivorNotice := ""
	if killReason != "" {
		survivorNotice = bashEscapedSurvivorNotice(
			bashEscapedPipeHolders(bashEscapedPipeInodes, os.Getpid()),
			bashChildSid, bashChildSidKnown, pipeReleased)
	}
	switch killReason {
	case KillReasonOperatorCancel:
		out := buildOutput(stdoutBuf.String(), stderrBuf.String(), -1)
		out += "\n" + BashCancelledMarker
		return out + pipeReleaseNotice + survivorNotice, fmt.Errorf("command cancelled by the operator")
	case KillReasonAbsoluteTimeout:
		out := buildOutput(stdoutBuf.String(), stderrBuf.String(), -1)
		out += fmt.Sprintf("\n[command timed out after %s]", absTimeout)
		return out + pipeReleaseNotice + survivorNotice, fmt.Errorf("command timed out after %s", absTimeout)
	case KillReasonIdleTimeout:
		out := buildOutput(stdoutBuf.String(), stderrBuf.String(), -1)
		out += fmt.Sprintf("\n[command killed: no output for %s]", idleTimeout)
		return out + pipeReleaseNotice + survivorNotice, fmt.Errorf("command killed: no output for %s", idleTimeout)
	}

	// 13. Determine exit code.
	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Unexpected execution error (e.g. bash not found).
			return buildOutput(stdoutBuf.String(), stderrBuf.String(), -1),
				fmt.Errorf("command execution failed: %w", runErr)
		}
	}

	// 14. Build and return the formatted result.
	//
	// THE ONE PLACE A COMMIT LEAVES A TRACE OF ITSELF (bash_unpublished_commit.go):
	// a command that created a commit, run from a chat's child, is measured for
	// "does any remote-tracking ref reach HEAD" — and when none does, the result
	// NAMES that commit and its two ways out. It rides the RESULT (not stderr)
	// because the result is the one channel that reaches both the model that ran
	// the command and the session record a human reads afterwards. The notice is
	// appended AFTER buildOutput's size guard, so a long build log can never
	// truncate the sentence away — it is the tail, which is where the guard keeps
	// the bytes it keeps.
	out := buildOutput(stdoutBuf.String(), stderrBuf.String(), exitCode)
	if notice := unpublishedCommitNotice(callerCommand, workdir, exitCode); notice != "" {
		out += notice
	}
	return out, nil
}

// killChildTree kills a bash child and everything it spawned, addressing the
// process GROUP only when the child is measurably its own group leader.
//
// THE GROUP IS MEASURED, NEVER ASSUMED. bash.go sets Setpgid, which makes the
// child its own group leader — but the recipe's value is that it does not TRUST
// that: syscall.Getpgid(pid) reads the kernel's own answer, and a group signal
// (kill(-pgid)) is sent only when pgid == pid, i.e. the child really owns a
// group of its own. Otherwise a negative-pid kill would address the group the
// CALLER belongs to. This is the same defect the worker-stop path measured
// (cmd/agent/cli_tasks_worker_stop.go, signalRunChild): there the registry
// recorded Group == pid as an ASSUMPTION, the real group was the worker's, and
// `kill(-childPID)` addressed a group that did not exist — ESRCH was swallowed
// as benign, so a stop reported success while delivering nothing.
//
// HERE THE STAKES ARE HIGHER THAN A HUNG WORKER: when bash runs under `gopi
// chat`, the caller IS the chat process (cmd/agent/cli_chat.go) — the parent of
// the child, in its own session. An unmeasured group signal from here would
// take the operator's terminal out. So the property is checked before every
// group signal, and the negative branch (pgid != pid → the child alone) is a
// regression case with its own test rather than a comment.
//
// AND THE TARGET GROUP MUST NOT BE THE CALLER'S (added 2026-10-22, task
// 01a163df). "The child owns this group" is only half the question a group
// signal has to answer; the other half is "am I inside it". Measured live: three
// `parecord` recorders spawned by `gopi chat` WITHOUT Setpgid were members of
// the chat's own group — one 10m42s old, all `<defunct>` — so the group they
// lived in was the operator's terminal. The guard is therefore stated as a
// REFUSAL (killChildTreeGroupIsTheCallers: pgid == os.Getpgrp()) rather than as
// an extra equality on the child, and the refusal is not a degraded mode: the
// targeted path below still takes the child and every enumerated descendant
// down. The producer half of the same defect is cmd/agent's
// voiceRecorderSysProcAttr — a recorder now leads its own group, so this group
// cannot contain a chat's children in the first place.
//
// The descendants are enumerated BEFORE the child is signalled: killing the
// child first REPARENTS its orphaned grandchildren to init, and a walk taken
// afterwards no longer finds them (measured in the same incident). The walk is
// what takes `nohup ... &`-style daemons down even when the group branch does
// not apply — without it their inherited pipe write-ends keep the reads in step
// 9 blocked forever (bug011).
//
// Best-effort by contract: a signal that fails is not an error to report (the
// process may have exited between the poll and the signal) — ESRCH is the
// EXPECTED outcome, not a failure.
func killChildTree(pid int) {
	if pid <= 0 {
		return
	}
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid && !killChildTreeGroupIsTheCallers(pgid) {
		// THE INCARNATION IS READ ON THE SAME SIDE OF THE DECISION AS THE GROUP
		// ID, so the two readings describe ONE process rather than two moments
		// of a number. A pgid names its leader's PID, so "the group led by 4242"
		// is a claim about a number and not about a process; the start-ticks
		// value taken HERE is what the delivery below is made conditional on
		// (killChildTreeGroupStillTheChilds). An unreadable incarnation is not a
		// pass: it degrades to the per-pid path rather than addressing a group
		// nothing has shown this caller owns (Rule 42).
		if before, ok := killChildTreeGroupStartTicks(pid); ok && killChildTreeGroupStillTheChilds(pid, pgid, before) {
			killChildTreeGroupSignal(pgid, syscall.SIGKILL)
			return
		}
	}
	// Not its own group leader — OR its group is ours (see
	// killChildTreeGroupIsTheCallers) — OR the number's incarnation could not be
	// read or has changed (see killChildTreeGroupStillTheChilds): signal the
	// child directly, and its descendants one by one from the live process
	// table.
	descendants := bashDescendantPIDs(pid)
	_ = syscall.Kill(pid, syscall.SIGKILL) //stylegate:ignore killing the child we started; ESRCH is the expected error
	for _, d := range descendants {
		_ = syscall.Kill(d, syscall.SIGKILL) //stylegate:ignore best-effort descendant; ESRCH is the expected error
	}
}

// killChildTreeGroupOf is the reading of "which process group am I in", behind a
// var so the refusal below can be exercised on a group that really IS the
// caller's.
//
// WHY IT MUST BE A SEAM: the two conditions that make a group signal
// self-inflicted cannot be produced together in one live test process. A group
// signal is unsafe when the target group is the CALLER's; and a child can only
// be a group leader of a group that is NOT ours (a group id names its leader's
// pid, so "the child leads my group" is contradictory). The test therefore
// lowers the measurement to the child's own group and asks whether the guard
// refuses — the geometry a real chat hits is the same one, measured earlier.
var killChildTreeGroupOf = syscall.Getpgrp

// killChildTreeGroupSignal addresses a whole process group. A var for the same
// reason as killChildTreeGroupOf: the REFUSAL is what has to be observable, and
// a refusal is only visible to a test that can see whether ANY group was
// addressed (both branches end with the child dead, so the outcome alone cannot
// tell them apart).
var killChildTreeGroupSignal = func(pgid int, sig syscall.Signal) {
	_ = syscall.Kill(-pgid, sig) //stylegate:ignore the group was measured as the child's own AND not the caller's; ESRCH is the expected error
}

// killChildTreeGroupStillTheChilds RE-VERIFIES the group id immediately before
// the group signal is delivered, closing the recycle window between the
// measurement and the delivery.
//
// THE DEFECT THIS CLOSES (measured 2026-10-24). killChildTree measures "the
// child leads a group of its own" with syscall.Getpgid(pid) at T1, and delivers
// syscall.Kill(-pgid, SIGKILL) at T4. A process-group id IS its leader's pid, so
// between T1 and T4 the kernel may reap the child, recycle that number, and hand
// it to an unrelated group — at which point the signal reaches strangers. The
// measured incident: the operator's 8 firefox processes all exited in the same
// second ("Exiting due to channel error", snap.firefox…scope consumed), with no
// OOM and no SIGKILL record anywhere — the shape of a GROUP SIGKILL, which
// cannot be caught, so no process wrote a death record. At that moment a worker
// was running `make test-budget` over internal/tools and internal/agent, and the
// pid ranges overlap: firefox 1047211..1219592, the suite's children
// 1084347..1223658.
//
// WHY THE EXISTING GUARD DID NOT COVER IT. killChildTreeGroupIsTheCallers asks
// "would this group signal reach ME" — a question about the caller, answered
// once, and it is blind to the number changing owner. Its own "honest scope"
// note reasons from kill(0,0)/kill(-1,0) existence probes, which is the CONTRACT
// of the address space, not the recycle race.
//
// WHY A SECOND READING IS THE RIGHT REMEDY RATHER THAN A LOCK. The window cannot
// be closed by holding anything: the kernel recycles the id without asking us.
// What CAN be done is to make the delivery conditional on a reading taken AFTER
// the decision — the child must still be alive, still be its own group leader,
// and the group id must still equal the pid. If any of that has moved, the
// group no longer provably belongs to the child, and the caller degrades to the
// per-pid path, which reads /proc live and therefore cannot reach a stranger
// (bashDescendantPIDs returns nothing for a pid that is gone). The degradation
// costs the reparented-grandchild coverage the group branch exists for — a child
// that died mid-kill has no reachable grandchildren left to take down, so the
// loss is real only in the case that motivated the branch (nohup daemons) and
// only when the child is already gone.
//
// This is a PROBE, not a kill: sig 0 delivers nothing. A seam (var) for the same
// reason as its neighbours — the refused path is what has to be observable.
//
// THE GROUP ID IS NOT THE PROCESS (added 2026-10-27). The two probes
// above are BOTH blind to the number changing owner: a recycled stranger that
// leads a group of its own satisfies `Getpgid(pid) == pgid` and answers
// `Kill(pid, 0) == nil` just as the child did — the predicate is deaf to
// "somebody else lives at this number now", which is precisely the recycle it
// was written to catch. A group id names a number; only the leader's INCARNATION
// (stat field 22, start ticks) names a process. So the third parameter is the
// incarnation read on the same side of the decision as the group id
// (killChildTree), and the delivery is conditional on it still reading the same
// value. Unreadable is a REFUSAL, never a pass: a process whose incarnation
// cannot be measured is not one this caller has shown it owns (Rule 42), and the
// per-pid path still takes the child and its enumerated descendants down.
var killChildTreeGroupStillTheChilds = func(pid, pgid int, incarnation string) bool {
	if pid <= 0 || pgid <= 0 || incarnation == "" {
		return false
	}
	// The child must still lead the group the caller is about to address.
	now, err := syscall.Getpgid(pid)
	if err != nil || now != pgid {
		return false
	}
	// ...and the pid the number names must still be the child itself, not a
	// recycled stranger that happens to lead a group. The group probe above is
	// satisfied by that stranger; this one is not.
	if live, ok := killChildTreeGroupStartTicks(pid); !ok || live != incarnation {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// killChildTreeGroupStartTicks reads a process's incarnation (stat field 22) —
// the value that distinguishes the child from a recycled holder of its pid.
//
// A seam, like its neighbours, because the two readings a test must disagree
// about are taken by PRODUCTION CODE at two moments it does not control: the
// test injects the pair (recorded-before, now-recycled) and asserts that no
// group signal is delivered. It reads through the ONE /proc reader (procRootName,
// Rule 13/14) rather than spelling a path.
var killChildTreeGroupStartTicks = func(pid int) (string, bool) {
	return procname.StartTicks(procRootName, strconv.Itoa(pid))
}

// The two ADDRESSES kill(2) reserves for something other than "the group whose
// id is this number". They are named because the guard below is written in the
// address space, not in group ids (Rule 1/13: these are kernel constants of the
// same standing as a limit, and a bare 0/-1 in a safety comparison is unreadable).
const (
	// killAddressOwnGroup is what the kernel reads as "the caller's own process
	// group": kill(0, sig) delivers to every process in it.
	killAddressOwnGroup = 0
	// killAddressBroadcast is what the kernel reads as "every process this caller
	// may signal, except init": kill(-1, sig).
	killAddressBroadcast = -1
)

// killChildTreeGroupIsTheCallers reports whether a group signal addressed at
// pgid would reach the process that is doing the killing.
//
// THE GUARD THIS ADDS, AND THE DEFECT IT CLOSES (measured 2026-10-22, task
// 01a163df). The original test was `pgid == pid` — "the CHILD owns this group" —
// and that question is blind to the real hazard: when the child sits in the
// CALLER's group, `pgid == pid` is simply false and the code takes the direct
// path, which is right by accident; but nothing stopped a group signal when the
// caller's own group happened to be addressed by other means. Live evidence:
// three `parecord` processes (voice recorders spawned by `gopi chat` without
// Setpgid) were members of the chat's OWN group, one 10m42s old and all
// `<defunct>`. Under `gopi chat` the caller IS the operator's terminal, so a
// group signal here is a self-kill — and SIGKILL cannot be caught, so no signal
// handler can soften it. The only durable answer is to refuse to address a group
// that contains us; the child and its descendants still die from the targeted
// path below.
//
// THE CONTRACT IS ABOUT THE ADDRESSED VALUE, NOT THE GROUP ID (measured
// 2026-10-23, this task). The delivery above spells `kill(-pgid, sig)`, so the
// kernel is handed `-pgid` — and kill(2) reserves three other values in that
// address space that reach the caller just as surely as `-Getpgrp()` does:
//
//	pgid =  0          → kill(0)         every process in MY OWN group
//	pgid =  1          → kill(-1)        every process I may signal (init excepted)
//	pgid = -Getpid()   → kill(+Getpid()) ME, by pid
//
// Measured with sig 0 (which delivers nothing — a pure existence/permission
// probe): kill(0,0) and kill(-1,0) both return nil, while a group nobody is in
// returns ESRCH. The pre-fix predicate compared against Getpgrp() and Getpid()
// only, so it answered false for all three — a stated contract that was wrong
// for three of the five forms reaching the caller. A guard wrong in the safe
// direction is still wrong: the next caller that trusts it spells `kill(-pgid, …)`
// with a pgid that came from somewhere else, and SIGKILL cannot be caught.
//
// Honest scope (Rule 41): both call sites pass a POSITIVE pgid today —
// killChildTree's from Getpgid() gated on `pgid == pid` with pid > 0, and the
// close-out's from RunCloseoutGroupFor, which mints only for pid > 0 — so this
// closes the CONTRACT, not a reachable call. It is the same "defence in depth"
// standing this guard itself was added with, and it can never suppress a real
// kill: a refusal degrades to the per-pid path.
func killChildTreeGroupIsTheCallers(pgid int) bool {
	// The address the kernel is actually handed.
	addr := -pgid
	switch {
	case addr == killAddressOwnGroup, addr == killAddressBroadcast:
		// kill(0) = my own group; kill(-1) = everything I may signal.
		return true
	case pgid == killChildTreeGroupOf(), pgid == syscall.Getpid():
		return true
	}
	// kill(Getpid()) — reachable as pgid = -Getpid(), i.e. a negated id.
	return addr == syscall.Getpid()
}

// bashDescendantPIDs returns the live descendants of pid (children,
// grandchildren, …) read from /proc. It is the bash path's own walk — it does
// NOT reach into cmd/agent's copy, which lives in package main — and it is
// deliberately small: the parent map is built from /proc/<pid>/stat's ppid
// field, so the walk is bounded by the process table (a process has one
// parent, so the traversal terminates). An unreadable entry is skipped rather
// than failing the kill.
func bashDescendantPIDs(pid int) []int {
	children := map[int][]int{}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		child, perr := strconv.Atoi(e.Name())
		if perr != nil {
			continue
		}
		parent, ok := bashParentPID(child)
		if !ok {
			continue
		}
		children[parent] = append(children[parent], child)
	}
	var out []int
	queue := []int{pid}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range children[cur] {
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out
}

// bashParentPID reads a process's parent id out of /proc/<pid>/stat, DELEGATING
// the parse to internal/procname — the repo's one implementation of it. That is
// not tidiness: the comm field is parenthesized and may itself contain spaces and
// parentheses, so counting fields by hand is a trap with three historical copies
// already, and internal/tools/procstat_parse_single_source_test.go fails the
// build on a fourth (Rules 13/14).
func bashParentPID(pid int) (int, bool) {
	raw, ok := procname.StatFieldString(procname.StatStatPath(procRootName, strconv.Itoa(pid)), procname.StatFieldParent)
	if !ok {
		return 0, false
	}
	parent, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return parent, true
}

// buildOutput formats stdout and stderr into the standard result string.
// It prepends "exit=N\n", prefixes stderr lines with "[stderr] ", and
// truncates the combined output if it exceeds maxOutputSize.
// rejectDangerous inspects a command against the dangerous patterns and
// returns a non-empty reason string when it must be rejected, or "" to allow
// it. workdir is the resolved working directory when BashInDir set one; empty
// means the process cwd.
//
// cwd whitelist: a matched whitelistable pattern is tolerated ONLY when every
// path-like token in the command resolves to inside the working directory.
// This lets legitimate in-workdir commands like "rm -rf ./build/" pass while
// "rm -rf /", "mkfs.ext4 /dev/sda" and any out-of-workdir target are
// rejected. Scanning ALL path tokens (not just the pattern's direct argument)
// closes the flag-trick ("mkfs.ext4 -F /dev/sda") and the cd-escape
// ("cd /tmp && rm -rf build") — both would otherwise hide an out-of-cwd
// target behind a relative path. Patterns without a whitelistable target
// (sudo escalation, raw disk redirection) are always rejected.
func rejectDangerous(command, workdir string) string {
	// Determine the effective working directory for the whitelist.
	base := workdir
	if base == "" {
		var err error
		base, err = os.Getwd()
		if err != nil {
			return "cannot determine working directory"
		}
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return fmt.Sprintf("cannot resolve working directory %q", base)
	}

	// Pattern match is case-insensitive (bash flags accept both cases:
	// "chmod -R" and "chmod -r" are the same recursive permission change);
	// path tokens below keep their original case (Linux paths are
	// case-sensitive). Quotes are stripped from the command for the MATCH
	// (dsh fail-closed hardening): "dd of=\"/dev/sda\"" would otherwise dodge
	// the of=/dev/ pattern with the '/' hidden behind a quote — quoting must
	// not hide a dangerous pattern any more than a path token.
	scan := strings.ReplaceAll(command, `"`, "")
	scan = strings.ReplaceAll(scan, "'", "")
	lower := strings.ToLower(scan)
	matched := false
	for _, dp := range dangerousPatterns {
		if !strings.Contains(lower, strings.ToLower(dp.pattern)) {
			continue
		}
		matched = true
		if !dp.whitelistable {
			return fmt.Sprintf("pattern %q (not whitelistable by cwd)", dp.pattern)
		}
	}
	if !matched {
		return ""
	}

	// cwd whitelist: every path-like token must stay inside the working
	// directory.
	for _, tok := range strings.Fields(command) {
		// Fail-closed on shell expansion (dsh hardening): a token carrying
		// $VAR, $(...) or backticks names a target that cannot be statically
		// verified — "rm -rf $HOME/evil" expands to wherever HOME points,
		// which is unknowable here. Because a dangerous pattern already
		// matched, an unverifiable target must not run. Trade-off: a
		// harmless "$PWD" echo beside a dangerous command is rejected too —
		// a rejection costs a retry, a fail-open costs data.
		if unverifiableToken(tok) {
			return fmt.Sprintf("token %q contains shell variable/command substitution — cannot verify its target", tok)
		}
		target, ok := pathTarget(tok)
		if !ok {
			continue
		}
		if !pathTargetInside(target, baseAbs) {
			return fmt.Sprintf("target %q is outside the working directory %q", target, baseAbs)
		}
	}
	return ""
}

// unverifiableToken reports whether a shell token carries expansion ($VAR,
// $(...), backticks) that hides its filesystem target from static analysis.
// Quotes are stripped first so a quoted variable is still caught.
func unverifiableToken(tok string) bool {
	return strings.ContainsAny(strings.Trim(tok, `"'`), "$`")
}

// pathTarget extracts a filesystem target from a shell token, if the token
// references one. Surrounding quotes are stripped FIRST (dsh fail-closed
// hardening): bash removes quotes before a command runs, so "rm -rf
// \"/tmp/evil\"" is the same filesystem operation as rm -rf /tmp/evil. The
// old prefix checks on the raw token let a leading quote classify the token
// as not-a-path, and the quoted absolute target sailed past the whitelist
// (codex also flagged the unparsed quoted path). dd operands (if=..., of=...)
// carry their path after '='; any other key=value token (bs=4k, count=100) is
// not a path. Harmless kernel pseudo-devices (/dev/null, /dev/zero,
// /dev/urandom, /dev/random) are exempt — writing /dev/null or reading
// /dev/zero is a common byte-stream idiom, not a disk operation.
func pathTarget(tok string) (string, bool) {
	tok = strings.Trim(tok, `"'`)
	if i := strings.IndexByte(tok, '='); i >= 0 {
		if tok[:i] != "if" && tok[:i] != "of" {
			return "", false
		}
		// The value may itself carry quotes inside the token: of="/dev/sda".
		tok = strings.Trim(tok[i+1:], `"'`)
	}
	switch tok {
	case "/dev/null", "/dev/zero", "/dev/urandom", "/dev/random":
		return "", false
	}
	if strings.HasPrefix(tok, "/") || strings.HasPrefix(tok, "./") ||
		strings.HasPrefix(tok, "../") || tok == "." || tok == ".." ||
		strings.HasPrefix(tok, "~") {
		return tok, true
	}
	return "", false
}

// pathTargetInside reports whether the path target stays inside baseAbs, the
// working-directory root. Relative targets are resolved relative to baseAbs
// (NOT the process cwd — the command runs with baseAbs as its cwd); absolute
// targets must themselves live under baseAbs. Symlinks are not resolved
// (targets need not exist yet), so a symlink inside baseAbs pointing outside
// is not followed — the whitelist checks the literal path.
func pathTargetInside(target, baseAbs string) bool {
	t := strings.Trim(target, `"'`+" \t\n")
	if t == "" {
		return false
	}
	if t == "." {
		return true // current directory is inside any root
	}
	// ~/ paths resolve against the user's home, which is never the workdir.
	if t == "~" || strings.HasPrefix(t, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		if t == "~" {
			t = home
		} else {
			t = filepath.Join(home, t[2:])
		}
	}
	// Relative target: resolve against baseAbs, the command's effective cwd.
	// An absolute target keeps its own resolution.
	if filepath.IsAbs(t) {
		abs := filepath.Clean(t)
		if abs == "/" {
			// The filesystem root is never a valid target — even when the
			// working directory IS the root, where Rel("/", "/") == "." and
			// the escape check below would pass (dsh fail-closed hardening:
			// "rm -rf /" must not run just because the agent's cwd is /).
			return false
		}
		rel, err := filepath.Rel(baseAbs, abs)
		if err != nil {
			return false
		}
		return !isDotDotEscape(rel)
	}
	cleaned := filepath.Clean(t)
	// Reject traversal that would escape above baseAbs.
	return !isDotDotEscape(strings.ReplaceAll(cleaned, "\\", string(filepath.Separator)))
}

// isDotDotEscape reports whether a cleaned relative path escapes above its
// root via ".." components.
func isDotDotEscape(rel string) bool {
	for _, p := range strings.Split(rel, string(filepath.Separator)) {
		if p == ".." {
			return true
		}
	}
	return false
}

func buildOutput(stdout, stderr string, exitCode int) string {
	var b strings.Builder

	if exitCode < 0 {
		b.WriteString("exit=-1\n")
	} else {
		fmt.Fprintf(&b, "exit=%d\n", exitCode)
	}

	// Append stdout as-is.
	b.WriteString(stdout)

	// Append stderr with "[stderr] " prefix on each line.
	if stderr != "" {
		if stdout != "" && !strings.HasSuffix(stdout, "\n") {
			b.WriteByte('\n')
		}
		for _, line := range strings.Split(strings.TrimSuffix(stderr, "\n"), "\n") {
			b.WriteString("[stderr] ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}

	result := b.String()

	// Size guards: soft cap first (with a hint to narrow), then the hard cap.
	// The tail is kept, not the head (dsh bash render, measured in the R5-R6
	// race): CLI errors — build failures, test failures, git conflicts — land
	// at the END of the output, and the exit=N prefix line already leads the
	// result. Head-truncating a failed build's log hid the very lines the
	// model needed to debug it.
	if len(result) > softOutputSize {
		keep := softOutputSize
		if len(result) > maxOutputSize {
			keep = maxOutputSize
		}
		// "exit=N\n" prefix stays; only the body is trimmed to its tail.
		prefixEnd := strings.IndexByte(result, '\n') + 1
		body := result[prefixEnd:]
		if len(body) > keep {
			marker := fmt.Sprintf("[output truncated at %d bytes — showing the tail; narrow the command with grep/head/sed]\n", keep)
			body = marker + body[len(body)-keep:]
			result = result[:prefixEnd] + body
		}
	}

	return result
}

// expandTilde replaces ~/ at the start of paths with the user's home directory.
func expandTilde(command string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return command
	}
	return strings.ReplaceAll(command, "~/", homeDir+"/")
}
