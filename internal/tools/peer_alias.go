package tools

// peer_alias.go — the ONE peer-alias validator every ssh leg references
// (Rule 14: a shared rule lives in exactly one place; Rule 13: no leg
// re-implements it with bare literals).
//
// WHY this exists: each sync leg used to spell out
//
//	if strings.TrimSpace(peer) == "" || strings.ContainsAny(peer, " \t\n;") {
//
// and then pass `peer` straight into `ssh` as an argv ELEMENT:
//
//	exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", peer, query)
//
// That guard misses the option-shaped alias: an alias beginning with "-"
// is parsed by ssh as an OPTION, not a host (ssh reads options and the host
// from the SAME argv vector), so
//
//	TaskDiffPeer("-oProxyCommand=/tmp/payload")
//
// passed the guard and reached ssh — observed pre-fix: "diff: ssh
// -oProxyCommand=x: exit status 255" instead of "invalid peer", i.e.
// arbitrary (argument-less) local command execution whenever a peer alias
// arrives from a compromised or shared settings.json / --peer / --report-to.
// The injectable shape is a SINGLE token (-oProxyCommand=sh,
// -oLocalCommand=…, -oProxyCommand=…) because the old guard already refused
// whitespace.
//
// The validator is deliberately ONE function used by every leg (harvest,
// claim lock, recon diff, anchor guard, sync session, memory assets, git
// push-back + git target) — a fix in one leg is a fix in all of them, and
// TestSSHPeerAliasGuardRefusesOptionShapedAliasAtEveryLeg asserts the
// refusal at each face.

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// sshCommandWord is the command word itself, named once so a reader that
	// must RECOGNISE an ssh invocation (internal/tools/chat_write_guard.go
	// treats a clause opening with it as an unreadable payload — the string
	// after the host is not expanded by any reader here) can reference the
	// word instead of typing it. It is a STRING COMPARISON, not a spawn: the
	// file that recognises the word execs nothing.
	sshCommandWord = "ssh"

	// sshPeerAliasOptionPrefix is the leading character that turns an
	// alias into an ssh OPTION (-o…, -F…, -l…). ssh parses options and the
	// host from the SAME argv vector, so a leading dash is option
	// injection, not a host name.
	sshPeerAliasOptionPrefix = "-"

	// sshPeerAliasForbiddenChars are the whitespace/shell metacharacters
	// the original per-leg guards rejected (kept byte-identical so the
	// shared validator's rejection set is a strict superset of theirs: no
	// alias accepted before is refused now except option-shaped ones).
	sshPeerAliasForbiddenChars = " \t\n;"

	// sshPeerAliasTargetSeparator is the alias:path separator the git legs
	// compose themselves (`fmt.Sprintf("%s:%s", peer, repo)`). A separator
	// inside the alias means the caller handed the leg a whole TARGET
	// instead of a host name — refuse it at the alias boundary (an ssh
	// alias can never contain ':'; silently splitting would let a caller
	// smuggle a second element past the check).
	sshPeerAliasTargetSeparator = ":"
)

// errInvalidPeerAlias is the sentinel every refusal wraps. Its text keeps
// the phrase "invalid peer" that the pre-existing legs printed and that the
// existing coverage tests match on (memory_sync_reconcile_cov_test.go,
// memory_sync_task_gitpushback_cov_test.go).
var errInvalidPeerAlias = errors.New("invalid peer alias")

// ValidateSSHPeerAlias normalizes and validates ONE peer alias before it is
// used as an ssh argv element (or composed into a git remote target). It
// returns the trimmed alias (whitespace padding around a valid alias is
// tolerated — the long-standing behavior) or an error wrapping
// errInvalidPeerAlias:
//
//   - empty / whitespace-only → refused (nothing to connect to);
//   - a leading "-" → refused (ssh would parse it as an OPTION — the
//     option-injection hole this validator exists to close);
//   - any whitespace or ';' → refused (the original metacharacter set);
//   - ':' → refused (the alias names a HOST; the git legs compose
//     `<alias>:<path>` themselves).
//
// `user@host` stays valid ("@" is not the first character), which keeps
// ssh_config aliases and user-qualified hosts working.
func ValidateSSHPeerAlias(peer string) (string, error) {
	alias := strings.TrimSpace(peer)
	if alias == "" {
		return "", fmt.Errorf("%w: %q is blank (want an ssh alias, e.g. %q)", errInvalidPeerAlias, peer, "hp")
	}
	if strings.HasPrefix(alias, sshPeerAliasOptionPrefix) {
		return "", fmt.Errorf("%w %q: a leading %q makes ssh parse it as an OPTION (e.g. -oProxyCommand=… runs a local command), not a host",
			errInvalidPeerAlias, alias, sshPeerAliasOptionPrefix)
	}
	if strings.ContainsAny(alias, sshPeerAliasForbiddenChars) {
		return "", fmt.Errorf("%w %q: it carries a whitespace/shell metacharacter (%q) — the alias travels as ONE ssh argv element",
			errInvalidPeerAlias, alias, sshPeerAliasForbiddenChars)
	}
	if strings.Contains(alias, sshPeerAliasTargetSeparator) {
		return "", fmt.Errorf("%w %q: it carries %q — the alias names a HOST; the git legs compose <alias>:<path> themselves",
			errInvalidPeerAlias, alias, sshPeerAliasTargetSeparator)
	}
	return alias, nil
}

// ValidateSSHGitTarget validates the ALIAS HALF of a caller-composed
// "<alias>:<path>" ssh git target (the shape GitSyncServerTarget /
// finishCleanupServerTargetForRepo render, and the shape git then splits before
// handing the host to ssh as an argv element — git pushback's composed
// target carries the same option-injection shape through git). Returns the
// trimmed target or an error wrapping errInvalidPeerAlias; the path half is
// git's business (it quotes it into the remote command, never into ssh's
// option vector).
func ValidateSSHGitTarget(target string) (string, error) {
	trimmed := strings.TrimSpace(target)
	alias, _, ok := strings.Cut(trimmed, sshPeerAliasTargetSeparator)
	if !ok {
		return "", fmt.Errorf("%w %q: a git target must be <ssh-alias>:%s<path>",
			errInvalidPeerAlias, trimmed, sshPeerAliasTargetSeparator)
	}
	if _, err := ValidateSSHPeerAlias(alias); err != nil {
		return "", fmt.Errorf("git target %q: %w", trimmed, err)
	}
	return trimmed, nil
}
