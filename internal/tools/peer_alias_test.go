package tools

// peer_alias_test.go — regression guard for the option-injection hole
// (option-injection epic): every ssh leg used to validate its peer alias
// with `TrimSpace(peer) == "" || ContainsAny(peer, " \t\n;")` only, so an
// OPTION-SHAPED alias ("-oProxyCommand=/tmp/payload", "-oLocalCommand=…")
// passed the guard and reached ssh, where a leading "-" is parsed as an
// OPTION, not a host (ssh reads options + host from the SAME argv vector).
// Observed pre-fix: TaskDiffPeer("-oProxyCommand=x") →
// "diff: ssh -oProxyCommand=x: exit status 255" instead of "invalid peer".
//
// The tests below lock BOTH halves of the fix:
//
//	① the single validator (ValidateSSHPeerAlias / ValidateSSHGitTarget) —
//	   refusal table + the error identity the CLI/tests match on;
//	② the FACE greps: the same option-shaped alias is refused AT EVERY LEG
//	   (diff, harvest, session, assets, claim, git push-back, client pass,
//	   git server target, finish-cleanup target) — because a fix in one leg
//	   that misses the others is exactly the bug this epic closed;
//	③ the LITERAL GATE: no production file in internal/tools may spell the
//	   old per-leg guard (`ContainsAny(x, " \t\n;")`) again — the rule lives
//	   in peer_alias.go or nowhere (Rule 13/14).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// optionShapedAliases are the injection shapes the guard MUST refuse: each
// is a SINGLE argv element (the old guard already refused whitespace, so a
// multi-token payload could not survive it) that ssh parses as an OPTION.
var optionShapedAliases = []string{
	"-oProxyCommand=sh",
	"-oProxyCommand=/tmp/payload",
	"-oLocalCommand=id",
	"-F/tmp/cfg",
	"--",
	"-",
	"-p2222",
}

// TestValidateSSHPeerAliasRefusalTable — the validator's contract: accept
// real aliases (padded ones trimmed, the long-standing behavior), refuse
// everything that is not one host name as ONE argv element.
func TestValidateSSHPeerAliasRefusalTable(t *testing.T) {
	valid := map[string]string{
		"hp":              "hp",
		"zbook":           "zbook",
		"user@host":       "user@host",
		"hp-1":            "hp-1",
		"hp.example.com":  "hp.example.com",
		"  hp  ":          "hp", // padding is tolerated (pre-existing behavior)
		"hp_node":         "hp_node",
		"node.with-dots1": "node.with-dots1",
	}
	for in, want := range valid {
		got, err := ValidateSSHPeerAlias(in)
		if err != nil {
			t.Errorf("ValidateSSHPeerAlias(%q) refused a valid alias: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ValidateSSHPeerAlias(%q) = %q, want %q", in, got, want)
		}
	}

	invalid := append([]string{
		"",
		"   ",
		"peer with space",
		"peer\ttail",
		"peer\nnext",
		"peer;maybe",
		"a;b",
		"hp:~/work/GoPi", // a composed TARGET, not an alias
	}, optionShapedAliases...)
	for _, in := range invalid {
		got, err := ValidateSSHPeerAlias(in)
		if err == nil {
			t.Errorf("ValidateSSHPeerAlias(%q) must be refused, got %q", in, got)
			continue
		}
		if got != "" {
			t.Errorf("refused alias %q returned a value %q — must be empty", in, got)
		}
		if !errors.Is(err, errInvalidPeerAlias) {
			t.Errorf("refusal of %q must wrap errInvalidPeerAlias, got %v", in, err)
		}
		// The phrase survives for the pre-existing CLI/tests contract
		// (memory_sync_reconcile_cov_test.go, ..._gitpushback_cov_test.go).
		if !strings.Contains(err.Error(), "invalid peer") {
			t.Errorf("refusal of %q must stay self-describing, got %v", in, err)
		}
		// The message names the offender with %q, so the comparison must use
		// the QUOTED form (a raw tab/newline never appears verbatim in the
		// message — %q escapes it as \t / \n). For the blank case the raw
		// input is what the message shows (there is no trimmed alias to name).
		if quoted := fmt.Sprintf("%q", in); !strings.Contains(err.Error(), quoted) {
			t.Errorf("refusal of %q must name the offending alias (%s), got %v", in, quoted, err)
		}
	}
}

// TestValidateSSHGitTargetSplitsAliasHalf — the git legs compose
// "<alias>:<path>" themselves; the alias HALF is what git hands to ssh, so
// the validator splints exactly that half (the path is git's business).
func TestValidateSSHGitTargetSplitsAliasHalf(t *testing.T) {
	for _, ok := range []string{"hp:~/work/GoPi", "zbook:/srv/repo", "  hp:/x  "} {
		got, err := ValidateSSHGitTarget(ok)
		if err != nil {
			t.Errorf("ValidateSSHGitTarget(%q) refused a valid target: %v", ok, err)
			continue
		}
		if got != strings.TrimSpace(ok) {
			t.Errorf("ValidateSSHGitTarget(%q) = %q, want the trimmed target", ok, got)
		}
	}
	for _, bad := range []string{
		"",             // no separator at all
		"hp",           // no separator
		":~/work/GoPi", // empty alias half
		"-oProxyCommand=sh:~/work/GoPi",
		"-oLocalCommand=id:/tmp/x",
		"-:~/x",
		"hp rm:~/x",
	} {
		if got, err := ValidateSSHGitTarget(bad); err == nil {
			t.Errorf("ValidateSSHGitTarget(%q) must be refused, got %q", bad, got)
		} else if !errors.Is(err, errInvalidPeerAlias) {
			t.Errorf("refusal of %q must wrap errInvalidPeerAlias, got %v", bad, err)
		}
	}
}

// TestSSHPeerAliasGuardRefusesOptionShapedAliasAtEveryLeg is the FACE test:
// the same option-shaped alias must be refused before any transport call —
// at every leg that hands an alias to ssh (or to git, which hands it to ssh
// in turn). Each refusal is asserted with no ssh binary needed, because the
// guard runs BEFORE the exec.
func TestSSHPeerAliasGuardRefusesOptionShapedAliasAtEveryLeg(t *testing.T) {
	const bad = "-oProxyCommand=/tmp/payload"

	// ① recon diff (TaskDiffPeer) — the leg the epic's repro named.
	if stats, err := TaskDiffPeer(bad); err == nil {
		t.Errorf("TaskDiffPeer must refuse an option-shaped alias, got %+v", stats)
	} else if !strings.Contains(err.Error(), "invalid peer") {
		t.Errorf("TaskDiffPeer refusal must say invalid peer, got: %v", err)
	}

	// ② recon read helpers (the unguarded pre-fix legs).
	if rows, err := reconRemoteRowSets(bad, reconRemoteDB); err == nil {
		t.Errorf("reconRemoteRowSets must refuse, got %d rows", len(rows))
	}
	if n, err := reconRemotePendingSet(bad); err == nil {
		t.Errorf("reconRemotePendingSet must refuse, got %d", n)
	}

	// ③ harvest + its git push-back.
	if rows, err := harvestRemoteTasks(bad, "", 0); err == nil {
		t.Errorf("harvestRemoteTasks must refuse, got %d rows", len(rows))
	}
	if rep, err := gitPushbackDefault(bad); err == nil {
		t.Errorf("gitPushbackDefault must refuse, got %+v", rep)
	}
	if rep, err := DefaultGitPushback(bad); err == nil {
		t.Errorf("DefaultGitPushback must refuse, got %+v", rep)
	}

	// ④ session + assets legs — refused BEFORE the test seam, so a hooked
	// in-process transport cannot sneak past the guard either.
	oldHook := syncSSHExchangeHook
	hookCalled := false
	syncSSHExchangeHook = func(string, []byte) ([]byte, error) { hookCalled = true; return nil, nil }
	t.Cleanup(func() { syncSSHExchangeHook = oldHook })

	for _, bad := range optionShapedAliases {
		if _, err := syncSSHExchange(bad, nil, syncPhaseDelta, syncSSHTimeout, 0); err == nil {
			t.Errorf("syncSSHExchange(%q) must refuse", bad)
		}
		if _, err := syncAssetsSSHExchange(bad, nil, memoryAssetSyncSSHTimeout); err == nil {
			t.Errorf("syncAssetsSSHExchange(%q) must refuse", bad)
		}
		if _, err := claimSSHExchange(bad, []byte("{}")); err == nil {
			t.Errorf("claimSSHExchange(%q) must refuse", bad)
		}
	}
	if hookCalled {
		t.Errorf("the transport seam ran despite the refusal — the guard must precede the hook")
	}

	// ⑤ git target legs (the alias half reaches ssh through git).
	repo := t.TempDir()
	if pushed, failed := GitSyncPushLocalBranches(repo, bad+":~/work/GoPi"); len(pushed) != 0 || len(failed) == 0 ||
		!strings.Contains(strings.Join(failed, " "), "invalid peer") {
		t.Errorf("GitSyncPushLocalBranches must refuse an option-shaped target, pushed=%v failed=%v", pushed, failed)
	}
	if rep := GitSyncClientPass(repo, bad+":~/work/GoPi"); !rep.Skipped ||
		!strings.Contains(rep.SkipReason, "invalid peer") {
		t.Errorf("GitSyncClientPass must skip an option-shaped target, got %+v", rep)
	}

	// ⑥ the declared-topology renderers: an option-shaped declared peer
	// renders NO target (fail-soft), never an injectable one.
	t.Setenv(config.SyncPeersEnv, bad)
	if got := GitSyncServerTarget(); got != "" {
		t.Errorf("GitSyncServerTarget rendered %q from an option-shaped peer", got)
	}
	if got := finishCleanupServerTarget(); got != "" {
		t.Errorf("finishCleanupServerTarget rendered %q from an option-shaped peer", got)
	}
	// ...and a well-formed declared peer still renders (the guard is not a
	// blanket refusal).
	t.Setenv("GOPI_SYNC_NODE", "hp")
	t.Setenv(config.SyncPeersEnv, "zbook")
	if got := GitSyncServerTarget(); got != "zbook:"+peerRepoPath() {
		t.Errorf("GitSyncServerTarget = %q, want zbook:%s", got, peerRepoPath())
	}
}

// oldPeerGuardLiteral is the per-leg guard shape the epic replaced. If it
// reappears in any production file, one leg has re-implemented the rule
// instead of referencing peer_alias.go — the exact drift that left four
// legs unguarded while two had a (still incomplete) check.
var oldPeerGuardLiteral = regexp.MustCompile(`ContainsAny\(\s*[A-Za-z_][A-Za-z0-9_]*\s*,\s*" \\t\\n;"\s*\)`)

// TestPeerAliasLiteralGate_OldGuardShapeMustNotReappear is the anti-
// regression gate (Rule 13): the forbidden-character set may live only at
// its definition in peer_alias.go — a second inline copy (or a copied guard)
// fails CI.
func TestPeerAliasLiteralGate_OldGuardShapeMustNotReappear(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	const definitionFile = "peer_alias.go"
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == definitionFile {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		scanned++
		if m := oldPeerGuardLiteral.Find(body); m != nil {
			t.Errorf("%s carries the per-leg peer guard (%s) — reference ValidateSSHPeerAlias in %s instead",
				name, m, definitionFile)
		}
	}
	if scanned == 0 {
		t.Fatalf("literal gate scanned no production files — the gate is inert")
	}
}

// TestPeerAliasGuardDocumentedAtEverySSHLeg is the second half of the face
// proof: every production file in internal/tools that spawns an ssh process
// must route its peer alias through the shared guard — either directly
// (the file references ValidateSSHPeerAlias/ValidateSSHGitTarget) or
// indirectly, where the map below names the validating call site. A NEW ssh
// leg cannot slip in unvalidated: it fails this test until it is added here
// with its validation site.
func TestPeerAliasGuardDocumentedAtEverySSHLeg(t *testing.T) {
	// theValidatingSite: file that execs ssh → where its alias is checked.
	// "direct" = the file itself calls the helper; anything else must name
	// the validating function (a helper that builds the *exec.Cmd for a
	// caller which validates).
	theValidatingSite := map[string]string{
		// the push transport: claimSSHExchange (the claim leg's ssh spawn)
		// moved here verbatim when the cross-node claim lock was deleted.
		"memory_sync_push_transport.go": "direct", // claimSSHExchange
		// the argv assembler: it spawns ssh and validates NOTHING itself —
		// every caller checks the alias first (the row legs, the push legs,
		// the notify/transfer/submit legs). It moved here from
		// memory_sync_claim_ssh.go, unchanged.
		"ssh_command.go":           "claimSSHExchange and the other sync legs validate the alias before newSSHCommand is called — this file only assembles argv",
		"memory_sync_session.go":   "direct", // syncSSHExchange
		"memory_sync_assets.go":    "direct", // syncAssetsSSHExchange
		"memory_sync_reconcile.go": "direct", // TaskDiffPeer + the recon readers
		"memory_sync_task.go":      "direct", // harvestRemoteTasks
		// the row legs: SyncRowRemote (the send half) and SyncRowReceive both
		// call ValidateSSHPeerAlias before they build an ssh argv.
		"memory_sync_row.go": "direct",
		// ledgerBackupSSHCommand validates the alias before it execs ssh —
		// "direct" so the gate ENFORCES the call (a free-text row would be an
		// unverified claim: the validator could be dropped and still pass).
		"ledger_backup.go": "direct",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	const definitionFile = "peer_alias.go" // the guard itself; its package doc quotes the ssh argv shape but it never execs
	seen := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == definitionFile {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(body), `"ssh"`) {
			continue // no ssh spawn in this file
		}
		site, known := theValidatingSite[name]
		if !known {
			t.Errorf("%s spawns ssh but names no validating site — route its peer alias through ValidateSSHPeerAlias and record it in theValidatingSite", name)
			continue
		}
		seen++
		if site == "direct" && !strings.Contains(string(body), "ValidateSSHPeerAlias") &&
			!strings.Contains(string(body), "ValidateSSHGitTarget") {
			t.Errorf("%s is recorded as validating directly but never calls the peer-alias helper", name)
		}
		if site != "direct" && strings.TrimSpace(site) == "" {
			t.Errorf("%s records an empty validating site — an unjustified exemption", name)
		}
	}
	if seen == 0 {
		t.Fatalf("no ssh-spawning production file was checked — the sweep is inert")
	}
}
