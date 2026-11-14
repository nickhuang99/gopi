package config

// cov_config_reset_identity_test.go — function-level coverage slice for the
// test seam config.ResetNodeIdentityForTest (branch feat/cov-config-hp).
// Tests only.
//
// The seam exists because ResolveNodeRole memoises the node identity
// PROCESS-WIDE (nodeIdentity.role/syncTo): a test that flips the role must be
// able to clear the memo. This slice asserts the seam actually clears BOTH
// halves of the cache — the test below fails if ResetNodeIdentityForTest
// ever becomes a no-op or clears only one field.

import (
	"os"
	"path/filepath"
	"testing"
)

// covCfgNodeSandbox pins every identity source under t.TempDir() and clears
// the memo before AND after the test.
func covCfgNodeSandbox(t *testing.T, settingsBody string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(settingsBody), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(GopiDirEnv, t.TempDir())
	t.Setenv(SettingsEnv, path)
	t.Setenv(RoleEnv, "")
	t.Setenv(SyncToEnv, "")
	ResetNodeIdentityForTest()
	t.Cleanup(ResetNodeIdentityForTest)
	return path
}

// TestCovCfgResetNodeIdentityForTest_ClearsRoleCache is the seam's reason to
// exist: without the reset the first resolution sticks, and after the reset
// the NEW env value is honoured.
func TestCovCfgResetNodeIdentityForTest_ClearsRoleCache(t *testing.T) {
	covCfgNodeSandbox(t, `{}`)

	t.Setenv(RoleEnv, string(NodeRoleServer))
	if role, syncTo := ResolveNodeRole(); role != NodeRoleServer || syncTo != "" {
		t.Fatalf("first resolve = (%q, %q), want (%q, \"\")", role, syncTo, NodeRoleServer)
	}

	// Same process, new env value — the memo still answers (cache proven).
	t.Setenv(RoleEnv, string(NodeRoleClient))
	if role, _ := ResolveNodeRole(); role != NodeRoleServer {
		t.Fatalf("second resolve = %q, want the CACHED %q (the memo must not re-read env)", role, NodeRoleServer)
	}

	// The seam must clear it.
	ResetNodeIdentityForTest()
	if role, syncTo := ResolveNodeRole(); role != NodeRoleClient || syncTo != "" {
		t.Errorf("resolve after ResetNodeIdentityForTest == (%q, %q), want (%q, \"\")", role, syncTo, NodeRoleClient)
	}
}

// TestCovCfgResetNodeIdentityForTest_ClearsSyncToCache covers the OTHER half
// of the memo: ResolveNodeRole caches when EITHER field is non-empty, so a
// reset that cleared only the role would leave a stale sync_to behind (the
// role here is empty the whole time).
func TestCovCfgResetNodeIdentityForTest_ClearsSyncToCache(t *testing.T) {
	covCfgNodeSandbox(t, `{}`)

	t.Setenv(SyncToEnv, "peerbox")
	if role, syncTo := ResolveNodeRole(); role != "" || syncTo != "peerbox" {
		t.Fatalf("first resolve = (%q, %q), want (\"\", \"peerbox\")", role, syncTo)
	}

	t.Setenv(SyncToEnv, "other-node")
	if _, syncTo := ResolveNodeRole(); syncTo != "peerbox" {
		t.Fatalf("second resolve sync_to = %q, want the CACHED peerbox", syncTo)
	}

	ResetNodeIdentityForTest()
	if role, syncTo := ResolveNodeRole(); role != "" || syncTo != "other-node" {
		t.Errorf("resolve after reset = (%q, %q), want (\"\", \"other-node\") — the sync_to half of the memo was not cleared", role, syncTo)
	}
}

// TestCovCfgResetNodeIdentityForTest_ReReadsSettingsFile: the memo also
// freezes the settings.json contribution; after the reset a REWRITTEN
// settings file is read again.
func TestCovCfgResetNodeIdentityForTest_ReReadsSettingsFile(t *testing.T) {
	path := covCfgNodeSandbox(t, `{"role":"client","sync_to":"peerbox"}`)

	if role, syncTo := ResolveNodeRole(); role != NodeRoleClient || syncTo != "peerbox" {
		t.Fatalf("first resolve = (%q, %q), want (client, peerbox)", role, syncTo)
	}

	if err := os.WriteFile(path, []byte(`{"role":"server"}`), 0o600); err != nil {
		t.Fatalf("rewrite settings: %v", err)
	}
	if role, _ := ResolveNodeRole(); role != NodeRoleClient {
		t.Fatalf("second resolve = %q, want the CACHED client", role)
	}

	ResetNodeIdentityForTest()
	if role, syncTo := ResolveNodeRole(); role != NodeRoleServer || syncTo != "" {
		t.Errorf("resolve after reset = (%q, %q), want (server, \"\") — settings were not re-read", role, syncTo)
	}
}

// TestCovCfgResetNodeIdentityForTest_EnvBeatsRewrittenSettings: after a reset
// the precedence rule still holds (env > settings), so clearing the memo must
// not change which source wins.
func TestCovCfgResetNodeIdentityForTest_EnvBeatsRewrittenSettings(t *testing.T) {
	path := covCfgNodeSandbox(t, `{"role":"client"}`)
	t.Setenv(RoleEnv, string(NodeRoleServer))

	if role, _ := ResolveNodeRole(); role != NodeRoleServer {
		t.Fatalf("resolve = %q, want the env %q", role, NodeRoleServer)
	}
	if err := os.WriteFile(path, []byte(`{"role":"client","sync_to":"peerbox"}`), 0o600); err != nil {
		t.Fatalf("rewrite settings: %v", err)
	}

	ResetNodeIdentityForTest()
	role, syncTo := ResolveNodeRole()
	if role != NodeRoleServer {
		t.Errorf("role after reset = %q, want the env %q to still win", role, NodeRoleServer)
	}
	if syncTo != "peerbox" {
		t.Errorf("sync_to after reset = %q, want peerbox (env sets no sync_to → settings fill it)", syncTo)
	}
}

// TestCovCfgResetNodeIdentityForTest_IdempotentAndSafeWithoutCache: calling
// the seam twice (or before anything resolved) is a no-op, and the following
// resolution still works.
func TestCovCfgResetNodeIdentityForTest_IdempotentAndSafeWithoutCache(t *testing.T) {
	covCfgNodeSandbox(t, `{}`)

	ResetNodeIdentityForTest()
	ResetNodeIdentityForTest() // no cache to clear — must not panic

	t.Setenv(RoleEnv, string(NodeRoleClient))
	role, _ := ResolveNodeRole()
	if role != NodeRoleClient {
		t.Fatalf("resolve after repeated resets = %q, want %q", role, NodeRoleClient)
	}

	ResetNodeIdentityForTest()
	if role, syncTo := ResolveNodeRole(); role != NodeRoleClient || syncTo != "" {
		t.Errorf("resolve after a second reset = (%q, %q), want (client, \"\")", role, syncTo)
	}
}

// TestCovCfgResetNodeIdentityForTest_UndeclaredIdentityIsNotMemoised: an
// UNKNOWN role spelling resolves to "undeclared" and, because both memo
// fields stay empty, the next call re-reads the env WITHOUT a reset. That is
// the documented "an empty role means undeclared, never guessed" contract
// seen from the cache's side — and it is why the reset is only needed after a
// SUCCESSFUL resolution.
func TestCovCfgResetNodeIdentityForTest_UndeclaredIdentityIsNotMemoised(t *testing.T) {
	covCfgNodeSandbox(t, `{}`)

	t.Setenv(RoleEnv, "serverr") // typo → undeclared
	if role, syncTo := ResolveNodeRole(); role != "" || syncTo != "" {
		t.Fatalf("typo role resolved to (%q, %q), want (\"\", \"\")", role, syncTo)
	}

	// No reset in between: an undeclared identity is not memoised, so the
	// corrected value is picked up immediately.
	t.Setenv(RoleEnv, string(NodeRoleServer))
	if role, _ := ResolveNodeRole(); role != NodeRoleServer {
		t.Errorf("resolve after fixing the typo = %q, want %q (undeclared must not be cached)", role, NodeRoleServer)
	}

	// And now that a real identity IS cached, a reset is required again.
	t.Setenv(RoleEnv, string(NodeRoleClient))
	if role, _ := ResolveNodeRole(); role != NodeRoleServer {
		t.Fatalf("resolve = %q, want the cached %q", role, NodeRoleServer)
	}
	ResetNodeIdentityForTest()
	if role, _ := ResolveNodeRole(); role != NodeRoleClient {
		t.Errorf("resolve after reset = %q, want %q", role, NodeRoleClient)
	}
}
