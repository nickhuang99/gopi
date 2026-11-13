// Package gitcommit is the ONE place gopi creates a git commit.
//
// ── WHY THIS PACKAGE EXISTS ──────────────────────────────────────────────────
//
// Before it, gopi had THREE internal commit sites, each with its own git
// wrapper and each building the commit message by hand:
//
//	internal/tools/task_adoption.go      adoptionGit(repo, "commit", "--no-verify", …)
//	internal/tools/task_merge_executor.go mergeExecGit(mainRepo, "commit", "--no-verify", …)
//	cmd/agent/merge_union.go             mergeGit(wt, "-c", "user.name=…", "commit", …)
//
// None of them wrote the facts a commit must carry (which ledger row produced
// it, which machine made it), because the mechanism that writes those facts —
// the bash tool's command-text rewrite (internal/tools/git_commit_task_trailer.go)
// — only sees COMMAND TEXT. A Go caller that builds argv directly has no text
// to rewrite, so every internal commit was unsigned. That is a property of the
// placement, not of anyone's diligence: a seam on text cannot reach a seam on
// argv.
//
// So the facts move to where the argv is built. Commit() composes the message,
// appends the trailer lines, runs git, and — the part that makes it a claim
// rather than a hope — READS THE TRAILER BACK OUT OF THE COMMIT OBJECT and
// fails when it is not there (Rule 15: a signature nobody verified is a
// signature nobody has).
//
// ── WHAT IT DELIBERATELY DOES NOT DO ────────────────────────────────────────
//
//   - It is NOT a `gopi commit` verb. People use git itself; a wrapper command
//     would be a second interface to the same thing. This is an internal seam.
//   - It does not rewrite history and does not amend anything the caller did
//     not just create (Options.Amend amends HEAD, which is what the caller
//     asked for).
//   - It does not replace the bash tool's command-text rewrite: that path
//     covers `git commit` an agent types, this one covers `git commit` gopi
//     itself runs. They share the trailer DEFINITIONS (imported from here), so
//     there is one spelling of the trailer and two doors that need it.
//   - It does not import internal/tools. This is a leaf: tools imports it, not
//     the other way round, or the dependency would be a cycle. The consequence
//     is that the caller passes the ledger id in (Options.TaskID) instead of
//     this package reading the environment seam — which is better anyway: the
//     commit records what the CALLER knows is true about itself.
package gitcommit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ── the trailer's spellings: ONE definition each (Rules 1/13/14) ─────────────

const (
	// TaskTrailerKey is the trailer's key word. A commit body line is
	// `<TaskTrailerKey>: <task-id>` — the exact shape `git log --grep="task: <id>"`
	// matches.
	TaskTrailerKey = "task"

	// TaskTrailerLineFmt renders the ledger row line.
	TaskTrailerLineFmt = TaskTrailerKey + ": %s"

	// HostTrailerKey is the second trailer's key word: the machine that made
	// the commit, so `git log --grep="host: zbook"` isolates one machine's
	// commits across the fleet.
	HostTrailerKey = "host"

	// HostTrailerLineFmt renders the host line.
	HostTrailerLineFmt = HostTrailerKey + ": %s"
)

// ── git vocabulary this package spells (Rule 1: no bare literals) ────────────

const (
	// gitBinary is the executable a commit is made with.
	gitBinary = "git"

	// gitVerbCommit is the commit-creating verb. It is typed HERE and nowhere
	// else in a production argument list — the seam gate
	// (internal/tools/git_commit_seam_gate_test.go) fails on a fresh `"commit"`
	// argv literal anywhere under internal/ or cmd/.
	gitVerbCommit = "commit"

	// gitVerbLog and gitVerbRevParse are the two READ-BACK verbs: the trailer
	// self-check and the hash of what was just created.
	gitVerbLog      = "log"
	gitVerbRevParse = "rev-parse"

	// flagMessage passes the composed message.
	flagMessage = "-m"

	// flagNoVerify skips the hooks (`--no-verify`) — the sites this replaced
	// passed it and must keep passing it: a commit created by the merge /
	// adoption lanes is not a developer commit and must not run a hook that can
	// block the lane.
	flagNoVerify = "--no-verify"

	// flagNoEdit reuses the prepared message (`--no-edit`). merge_union passed
	// it beside its own `-m`; keeping it preserves that argv exactly.
	flagNoEdit = "--no-edit"

	// flagAmend rewrites HEAD instead of creating a commit.
	flagAmend = "--amend"

	// flagAllowEmpty permits a commit with no change staged.
	flagAllowEmpty = "--allow-empty"

	// flagConfig passes one git configuration pair (`-c key=value`).
	flagConfig = "-c"

	// flagSingleRev is `git log -1`.
	flagSingleRev = "-1"

	// flagFormatPrefix prefixes a `--format=` pattern.
	flagFormatPrefix = "--format="

	// trailerValueOnlyFmt is git's own trailer reader: the VALUES of one key,
	// one per line. It is how a written trailer is VERIFIED — the commit object
	// is asked, not the string we just built (a message that never reached the
	// commit is exactly the failure a string comparison cannot see).
	trailerValueOnlyFmt = "%%(trailers:key=%s,valueonly)"

	// headRev names the commit just created, for the hash and the read-back.
	headRev = "HEAD"

	// repoFlag makes git run in Options.Repo's directory without the caller
	// having to chdir (`-C`).
	// (unused: the backend sets cmd.Dir instead — see defaultBackend)
)

// ── the message and the two argument records ────────────────────────────────

// Message is a commit's prose, WITHOUT the trailer block: Commit appends that.
// A caller cannot forget the trailers precisely because it cannot supply them.
type Message struct {
	// Subject is the first line. A blank subject is an error — a commit with
	// no first line is unreviewable in `git log --oneline`.
	Subject string

	// Body is the optional rest, verbatim (bullets, paragraphs, links).
	Body string
}

// Options is what a caller may decide about its own commit.
type Options struct {
	// TaskID is the ledger row this commit is the output of. Empty = the
	// commit belongs to no row (a merge lane's own commit, an adoption sweep),
	// and then no `task:` line is written rather than a made-up one.
	TaskID string

	// Host overrides this machine's name. Empty = ask the OS (the normal
	// case). It exists so a test can pin the value; production never sets it.
	Host string

	// NoVerify passes `--no-verify`.
	NoVerify bool

	// NoEdit passes `--no-edit` (merge_union's argv).
	NoEdit bool

	// Amend rewrites HEAD instead of creating a commit.
	Amend bool

	// AllowEmpty passes `--allow-empty`.
	AllowEmpty bool

	// Config is a list of `key=value` pairs passed as `-c key=value`, in
	// order. merge_union uses it for its identity override
	// (`user.name` / `user.email`), which must stay exactly as it was: the
	// mechanical lane commits under its own name, never as the operator.
	Config []string

	// Env replaces the child's environment. nil = inherit this process's
	// (the ordinary case). merge_union pins the locale onto its git children
	// and passes that here, so the pin survives the move.
	Env []string

	// Timeout bounds each git invocation; 0 = no deadline beyond ctx.
	Timeout time.Duration
}

// Result is what actually happened — measured, not claimed.
type Result struct {
	// Hash is the commit's full hash (`git rev-parse HEAD` after the commit).
	Hash string

	// Trailers are the trailer LINES this commit carries, read BACK OUT of the
	// commit object. If a line is not here, it is not in the commit.
	Trailers []string
}

// ── the replaceable backend ─────────────────────────────────────────────────

// Invocation is one git call the seam needs made.
type Invocation struct {
	// Args are the arguments after the git binary name.
	Args []string
	// Dir is the working directory git runs in.
	Dir string
	// Env replaces the child's environment (nil = inherit).
	Env []string
}

// Backend runs git. It exists so the seam can be PROVEN to be a seam: a fake
// backend answers with recorded output and the three call sites compile and
// pass unchanged, which is what distinguishes an abstraction from three
// wrappers renamed to one.
type Backend interface {
	// Run executes one git invocation and returns its combined output.
	Run(ctx context.Context, inv Invocation) ([]byte, error)
}

// defaultBackend is git itself.
type defaultBackend struct{}

// Run implements Backend with os/exec, inheriting (or replacing) the
// environment exactly as the three wrappers did.
func (defaultBackend) Run(ctx context.Context, inv Invocation) ([]byte, error) {
	cmd := exec.CommandContext(ctx, gitBinary, inv.Args...)
	cmd.Dir = inv.Dir
	if inv.Env != nil {
		cmd.Env = inv.Env
	}
	return cmd.CombinedOutput()
}

var (
	backendMu sync.RWMutex
	backend   Backend = defaultBackend{}
)

// UseBackend replaces the git backend and returns a function that restores the
// previous one. It is the seam's seam: with a fake installed, the callers run
// their own code paths unchanged (no injection point, no build tag, no
// Options field they would have to know about).
func UseBackend(b Backend) (restore func()) {
	backendMu.Lock()
	prev := backend
	if b == nil {
		b = defaultBackend{}
	}
	backend = b
	backendMu.Unlock()
	return func() {
		backendMu.Lock()
		backend = prev
		backendMu.Unlock()
	}
}

// currentBackend reads the installed backend.
func currentBackend() Backend {
	backendMu.RLock()
	defer backendMu.RUnlock()
	return backend
}

// ── the trailer lines ───────────────────────────────────────────────────────

// TaskTrailerLine renders the ledger reference for a task id: `<key>: <id>`.
// It returns "" for a blank id — a reference to nothing is not a reference.
func TaskTrailerLine(taskID string) string {
	id := strings.TrimSpace(taskID)
	if id == "" || !Spellable(id) {
		return ""
	}
	return fmt.Sprintf(TaskTrailerLineFmt, id)
}

// HostTrailerLine renders the machine reference for a host name:
// `<HostTrailerKey>: <name>`. It returns "" when the host cannot be named — a
// guessed machine is worse than a missing one.
func HostTrailerLine(host string) string {
	// The RAW value is validated, never a trimmed one: padding is exactly the
	// shape a shell word-split would silently normalise, and "it looked fine
	// after I trimmed it" is how a value that never appeared in a commit gets
	// reported as one that did.
	if !Spellable(host) {
		return ""
	}
	return fmt.Sprintf(HostTrailerLineFmt, host)
}

// LocalHostTrailerLine is the host line for THIS machine, and the ONE reader of
// the OS hostname for this mechanism (Rule 2's spirit — one place). The bash
// tool's injection asks this function too, so the two doors cannot disagree on
// the failure shape.
func LocalHostTrailerLine() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return HostTrailerLine(h)
}

// Trailers returns the trailer LINES a commit made by this process should
// carry, in order: the ledger row first (when the caller has one), the host
// name second (when the machine can be named).
//
// The two are INDEPENDENT on purpose. A ledger row is published only to a
// dispatched run, so a chat, a hand-run gopi or a merge lane has none — but it
// is still a machine making commits, and "which host wrote this?" must have an
// answer for those commits too.
func Trailers(taskID string) []string {
	var lines []string
	if line := TaskTrailerLine(taskID); line != "" {
		lines = append(lines, line)
	}
	if line := LocalHostTrailerLine(); line != "" {
		lines = append(lines, line)
	}
	return lines
}

// Spellable reports whether a value is safe to write as a trailer value: an
// apostrophe or a newline would let the value break out of its line, and a
// value that is not spellable is REFUSED (its line is not written) rather than
// rewritten. It is applied to both trailer values — the ledger id
// (uuid-shaped: hex and '-') and the host name (DNS-label shaped).
func Spellable(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

// ── Commit ─────────────────────────────────────────────────────────────────

// Commit creates one commit in repo, carrying this machine's name and — when
// Options.TaskID names a ledger row — that row, and then VERIFIES the result by
// reading the trailers back out of the commit object.
//
// The error is honest about which half failed: a git failure is returned with
// git's own diagnostic, and a MISSING trailer is an error of its own (Rule 15 —
// a commit that silently lacks its signature is the defect this package is for,
// so it may never be reported as success).
func Commit(ctx context.Context, repo string, msg Message, opts Options) (Result, error) {
	subject := strings.TrimSpace(msg.Subject)
	if subject == "" {
		return Result{}, fmt.Errorf("gitcommit: empty commit subject")
	}
	lines, err := trailerLines(opts)
	if err != nil {
		return Result{}, err
	}

	text := subject
	if body := strings.TrimSpace(msg.Body); body != "" {
		text += "\n\n" + body
	}
	// The trailer block is the LAST paragraph, separated by a blank line —
	// which is what makes git read these lines AS trailers rather than as more
	// body prose (`%(trailers:key=…)` only sees a recognised block).
	if len(lines) > 0 {
		text += "\n\n" + strings.Join(lines, "\n")
	}

	args := commitArgs(opts, text)
	runCtx, cancel := withTimeout(ctx, opts.Timeout)
	defer cancel()
	if _, err := run(runCtx, repo, opts.Env, args...); err != nil {
		return Result{}, err
	}

	// ── the self-check: ask the COMMIT OBJECT, not the string we sent ───────
	verified := make([]string, 0, len(lines))
	for _, line := range lines {
		key, value := splitTrailerLine(line)
		got, err := readTrailer(runCtx, repo, opts.Env, key)
		if err != nil {
			return Result{}, err
		}
		if got == "" {
			return Result{}, fmt.Errorf("gitcommit: the commit was created but carries no %q trailer (read back %q) — refused rather than reported as signed",
				key, got)
		}
		if got != value {
			return Result{}, fmt.Errorf("gitcommit: the %q trailer reads back as %q, not the %q that was written", key, got, value)
		}
		verified = append(verified, line)
	}

	hash, err := readHash(runCtx, repo, opts.Env)
	if err != nil {
		return Result{}, err
	}
	return Result{Hash: hash, Trailers: verified}, nil
}

// trailerLines is Trailers() plus the one refusal Commit must make on its own:
// a host line that could not be produced means this commit cannot be
// attributed, and the whole point of the seam is that it forces that question.
func trailerLines(opts Options) ([]string, error) {
	var lines []string
	if line := TaskTrailerLine(opts.TaskID); line != "" {
		lines = append(lines, line)
	}
	host := opts.Host
	if host == "" {
		h, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("gitcommit: this machine cannot be named (%w) — a commit that does not say which host made it is not created: %w", err, err)
		}
		host = h
	}
	line := HostTrailerLine(host)
	if line == "" {
		return nil, fmt.Errorf("gitcommit: host name %q is not spellable as a trailer value", host)
	}
	return append(lines, line), nil
}

// commitArgs builds the argv: config pairs, the verb, the flags, the message.
func commitArgs(opts Options, text string) []string {
	args := make([]string, 0, 8+2*len(opts.Config))
	for _, kv := range opts.Config {
		args = append(args, flagConfig, kv)
	}
	args = append(args, gitVerbCommit)
	if opts.NoVerify {
		args = append(args, flagNoVerify)
	}
	if opts.NoEdit {
		args = append(args, flagNoEdit)
	}
	if opts.Amend {
		args = append(args, flagAmend)
	}
	if opts.AllowEmpty {
		args = append(args, flagAllowEmpty)
	}
	return append(args, flagMessage, text)
}

// readTrailer reads one trailer key's value out of HEAD.
func readTrailer(ctx context.Context, repo string, env []string, key string) (string, error) {
	out, err := run(ctx, repo, env, gitVerbLog, flagSingleRev,
		flagFormatPrefix+fmt.Sprintf(trailerValueOnlyFmt, key))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// readHash reads the hash of the commit just created.
func readHash(ctx context.Context, repo string, env []string) (string, error) {
	out, err := run(ctx, repo, env, gitVerbRevParse, headRev)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// splitTrailerLine splits a rendered line back into its key and value.
func splitTrailerLine(line string) (key, value string) {
	k, v, found := strings.Cut(line, ":")
	if !found {
		return "", ""
	}
	return strings.TrimSpace(k), strings.TrimSpace(v)
}

// run executes one git invocation and renders a failure with git's own cause:
// the first meaningful output line, or the error's text when git wrote nothing
// (a chdir failure says nothing on stdout — Rule 15b).
func run(ctx context.Context, repo string, env []string, args ...string) ([]byte, error) {
	out, err := currentBackend().Run(ctx, Invocation{Args: args, Dir: repo, Env: env})
	if err != nil {
		detail := diagnosticLine(out)
		if detail == "" {
			detail = strings.TrimSpace(err.Error())
		}
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, detail)
	}
	return out, nil
}

// diagnosticLine is the first non-blank output line — the line that carries the
// cause in every git failure this seam sees.
func diagnosticLine(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// withTimeout applies Options.Timeout when one was asked for; the caller's own
// ctx deadline (if any) otherwise governs, because a caller that already
// bounded its work must not be given a longer bound here.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}
