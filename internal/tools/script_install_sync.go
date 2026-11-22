package tools

// Installed-script refresh — the post-merge close loop's SECOND half
// (rework of fix/launcher-install-refresh, epic 2026-09-12).
//
// WHY THIS EXISTS (the reproduced failure): the original fix added a `--check`
// audit seam to scripts/gopi-worker-up.sh and byte-identity regression gates in
// internal/testutil. Both are correct and both passed — yet the merged state
// FAILED its post-merge verification on the real host: the operator's shell
// runs the INSTALLED copy (`~/.local/bin/gopi-worker-up.sh`, reached through
// `~/bin/gopi-worker-up.sh`), that copy was still the pre-fix blob
// (sha256 4b2ad5a2…, 16457 B, installed 20:22:56), and the repo's copy was the
// merged one (sha256 98e3ead4…, 19138 B, merged 20:31:52). `--check` on the
// live host exited 1 STALE.
//
// ROOT CAUSE (5-why): a merge changes the repo's script, and NOTHING re-runs
// the script's `--install`. The original fix made the drift AUDIBLE (`--check`)
// but left the repair as a manual operator step, so the audit seam could only
// ever report the drift it was written to prevent — and every test of it
// stayed green, because those tests drive the REPO script or an isolated HOME
// and never look at the copy that actually starts workers.
//
// THE FIX (intent preserved, hole closed): the same merge-round-end hook that
// already rebuilds the binary (Rule 12's close loop) now also refreshes every
// installed script copy that has drifted. The audit remains the seam's alarm;
// the refresh is the seam's hand. Both live in the script itself — this file
// decides only WHEN, never HOW (the script owns its install semantics: the
// already-installed guard, the <HOME>/bin symlink, glm-night-watch's peer
// note). No new tool, no new seam: one more step in a loop that demonstrably
// runs after every merge round.
//
// SCOPE — deliberately narrow and non-destructive:
//   - only a script that ALREADY has an installed copy is refreshed. This code
//     never installs something the host did not choose to install (installing
//     new things is an operator decision, not a merge's side effect).
//   - only a script that DECLARES the install seam is invoked (Rule 21's
//     spirit: never call a knob a script does not read).
//   - a repo with no scripts directory is a quiet no-op (the docs-only /
//     foreign-repo case), and no scripts directory is created anywhere.
//   - failure is LOGGED, never fatal and never a rollback: the merge is done
//     and good (the same contract the rebuild step holds). A script that
//     cannot refresh is a visible wart in the merger log, not a crash.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ScriptSyncDirRel is the stable installed-copy directory relative to HOME
// ("~/.local/bin/<name>") — the one shape every `--install` seam writes. The
// shell side spells the same value (internal/testutil installDirRel pins the
// contract from the test side), and this comment is the cross-reference for
// the two spellings — neither can import the other.
const ScriptSyncDirRel = ".local/bin"

const (
	// scriptSyncRepoDirName is the repo's operator-script directory: the
	// scripts a host installs onto its PATH (gopi-worker-up.sh,
	// gopi-home-gc.sh, glm-night-watch.sh).
	scriptSyncRepoDirName = "scripts"
	// scriptSyncHomeBinDirName is the PATH entry `--install` re-points at the
	// stable copy (a symlink, so a refresh needs no PATH edit).
	scriptSyncHomeBinDirName = "bin"
	// scriptSyncShellSuffix is the only file kind scanned: the operator's
	// entry points are shell scripts.
	scriptSyncShellSuffix = ".sh"
	// scriptSyncInstallFlag is the seam's own verb. A script carrying it can
	// refresh its installed copy from wherever it runs; the Go side only
	// decides WHEN. Rule 21's shape: the flag is READ by the script pointer
	// these bytes are, never a knob nothing consumes.
	scriptSyncInstallFlag = "--install"
	// scriptSyncInstallTimeout caps ONE `--install` child. It is a per-child
	// cap, not a budget, and it must exceed the slowest install a script can
	// legitimately run: glm-night-watch.sh's install attempts a peer ssh with
	// a 15s connect timeout (sshConnectTimeoutSec in that script), so a cap at
	// or below that would report a healthy install as a failure.
	scriptSyncInstallTimeout = 60 * time.Second
	// scriptSyncDigestChars is how many leading hex chars of a digest are
	// printed. Enough to compare by eye in a log line (the incident was
	// diagnosed with the same prefix length), short enough to keep the line
	// readable.
	scriptSyncDigestChars = 16
	// scriptSyncOutputMax caps the captured install output kept in the result
	// (a chatty script must not bloat the merger log).
	scriptSyncOutputMax = 400
)

// ScriptSyncResult is one refresh pass's outcome across the repo's scripts.
type ScriptSyncResult struct {
	// Repo is the repo whose installed copies were audited.
	Repo string
	// Refreshed names the scripts whose installed copy was re-installed from
	// the repo (drift repaired).
	Refreshed []string
	// Current names the scripts whose installed copy already carried the
	// repo's bytes (nothing to do).
	Current []string
	// NotInstalled names the scripts with no installed copy at all — never
	// installed by this code (an operator decision), so nothing to refresh.
	NotInstalled []string
	// Failed maps a script name to why its refresh did not happen (child
	// non-zero exit / spawn error), with the output tail for the audit trail.
	Failed map[string]string
}

// RefreshedAny reports whether at least one installed copy was re-installed.
func (r *ScriptSyncResult) RefreshedAny() bool {
	return r != nil && len(r.Refreshed) > 0
}

// SyncInstalledScripts refreshes the installed copy of every repo script whose
// bytes have drifted from the repo — the merge-round-end hand of the audit
// seam. It never installs a script that has no installed copy, and never
// fails: every problem lands in the result for the caller to log.
//
// A repo with no scripts directory (docs-only repo, or a foreign task field)
// is the deliberate quiet no-op.
func SyncInstalledScripts(repo string) *ScriptSyncResult {
	res := &ScriptSyncResult{Repo: repo}
	if strings.TrimSpace(repo) == "" {
		return res
	}
	scriptsDir := filepath.Join(repo, scriptSyncRepoDirName)
	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		// No scripts dir (docs-only repo) — nothing to audit. Any other read
		// failure is also non-fatal here: the merge is done and good, and the
		// caller's log line is the operator's signal.
		return res
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		res.Failed = map[string]string{"": "HOME is not resolvable: " + errText(err)}
		return res
	}
	installedDir := filepath.Join(home, filepath.FromSlash(ScriptSyncDirRel))

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, scriptSyncShellSuffix) {
			continue
		}
		src := filepath.Join(scriptsDir, name)
		raw, err := os.ReadFile(src)
		if err != nil {
			res.addFailure(name, "read "+src+": "+errText(err))
			continue
		}
		// Rule 21's shape: only invoke a seam the script actually DECLARES.
		// A script with no install flag is not this pass's business (and
		// calling it with --install would be an unknown-flag error, i.e. a
		// fabricated failure).
		if !bytes.Contains(raw, []byte(scriptSyncInstallFlag)) {
			continue
		}
		installed := filepath.Join(installedDir, name)
		installedBytes, err := os.ReadFile(installed)
		if err != nil {
			// Not installed → not our business (never install on a merge's
			// side effect: which scripts a host runs is the operator's call).
			res.NotInstalled = append(res.NotInstalled, name)
			continue
		}
		if bytes.Equal(installedBytes, raw) {
			res.Current = append(res.Current, name)
			continue
		}
		if err := runScriptInstall(src, repo); err != nil {
			res.addFailure(name, err.Error())
			continue
		}
		// TRUST NOTHING: the child's exit 0 is not proof the bytes now match
		// (an install that silently no-ops is exactly the class of failure
		// this whole epic is about). Re-read the installed copy and assert
		// byte identity — the same measure the incident was diagnosed with.
		after, aerr := os.ReadFile(installed)
		if aerr != nil {
			res.addFailure(name, "re-read "+installed+" after install: "+errText(aerr))
			continue
		}
		if !bytes.Equal(after, raw) {
			res.addFailure(name, fmt.Sprintf(
				"install reported success but the bytes still differ (installed %s want %s)",
				digestPrefix(after), digestPrefix(raw)))
			continue
		}
		res.Refreshed = append(res.Refreshed, name)
	}
	return res
}

// addFailure records a per-script problem (nil-map safe).
func (r *ScriptSyncResult) addFailure(name, detail string) {
	if r.Failed == nil {
		r.Failed = map[string]string{}
	}
	r.Failed[name] = detail
}

// runScriptInstall runs ONE script's own `--install` seam with the repo as its
// working directory (the script resolves the repo from its own path). Returns
// a non-nil error carrying the exit status and the output tail — the caller
// logs it and moves on.
//
// The child launches NOTHING: every script's install block exits before its
// launch path (gopi-worker-up.sh's is the first block after flag parsing), so
// this is the seam's hand, never a worker start. Process-group isolation on
// the timeout mirrors runBuild: a hung install is killed with its children.
func runScriptInstall(scriptPath, repo string) error {
	ctx, cancel := context.WithTimeout(context.Background(), scriptSyncInstallTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", scriptPath, scriptSyncInstallFlag)
	cmd.Dir = repo
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			if cmd.Process != nil {
				// signal to a process group we are already abandoning (the
				// runBuild pattern); ESRCH is the expected error.
				// The group id is RE-VERIFIED before delivery: it is this child's pid, and the
		// kernel may have recycled that number to an unrelated group (see killGroupGuard.go).
		_, _ = signalProcessGroup(cmd.Process.Pid, cmd.Process.Pid, syscall.SIGKILL) //stylegate:ignore kill of an abandoned group: ESRCH is the expected outcome
			}
			return fmt.Errorf("--install timed out after %s (output: %s)",
				scriptSyncInstallTimeout, outputTailTrim(string(out), scriptSyncOutputMax))
		}
		return fmt.Errorf("--install exited non-zero: %s (output: %s)",
			errText(err), outputTailTrim(string(out), scriptSyncOutputMax))
	}
	return nil
}

// digestPrefix returns the first scriptSyncDigestChars hex chars of a file's
// sha256 — the comparison the operator reads in the log line.
func digestPrefix(raw []byte) string {
	sum := sha256.Sum256(raw)
	full := hex.EncodeToString(sum[:])
	if len(full) <= scriptSyncDigestChars {
		return full
	}
	return full[:scriptSyncDigestChars]
}

// outputTailTrim returns the last max bytes of a child's combined output,
// trimmed of surrounding whitespace (install errors live at the tail).
func outputTailTrim(out string, max int) string {
	out = strings.TrimSpace(out)
	if len(out) <= max {
		return out
	}
	return "…" + out[len(out)-max:]
}

// errText renders an error for a log line without inventing a nil string.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
