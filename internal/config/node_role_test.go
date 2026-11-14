package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeNodeSettings writes a settings.json fixture and points GOPI_SETTINGS at
// it, returning a restore func (the identity cache is reset per test since
// ResolveNodeRole memoises process-wide state).
func writeNodeSettings(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", p)
	t.Setenv(RoleEnv, "")
	t.Setenv(SyncToEnv, "")
	nodeIdentity.role, nodeIdentity.syncTo = "", ""
	t.Cleanup(func() { nodeIdentity.role, nodeIdentity.syncTo = "", "" })
}

func TestConfigRoleSyncToParsing(t *testing.T) {
	writeNodeSettings(t, `{"role":"client","sync_to":"zpeer"}`)
	cfg, err := FromSettingsFile(os.Getenv("GOPI_SETTINGS"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Role != NodeRoleClient {
		t.Errorf("role = %q, want %q", cfg.Role, NodeRoleClient)
	}
	if cfg.SyncTo != "zpeer" {
		t.Errorf("sync_to = %q, want zpeer", cfg.SyncTo)
	}
}

func TestResolveNodeRoleSettings(t *testing.T) {
	writeNodeSettings(t, `{"role":"client","sync_to":"zpeer"}`)
	role, syncTo := ResolveNodeRole()
	if role != NodeRoleClient || syncTo != "zpeer" {
		t.Errorf("got (%q, %q), want (client, zpeer)", role, syncTo)
	}
}

func TestResolveNodeRoleEnvOverridesSettings(t *testing.T) {
	writeNodeSettings(t, `{"role":"client","sync_to":"zpeer"}`)
	t.Setenv(RoleEnv, "server")
	t.Setenv(SyncToEnv, "hp")
	role, syncTo := ResolveNodeRole()
	if role != NodeRoleServer || syncTo != "hp" {
		t.Errorf("env override ignored: got (%q, %q)", role, syncTo)
	}
}

func TestResolveNodeRoleUnknownSpellingUndeclared(t *testing.T) {
	writeNodeSettings(t, `{"role":"master"}`) // typo must NOT become a role
	role, _ := ResolveNodeRole()
	if role != "" {
		t.Errorf("unknown role %q accepted — must stay undeclared", role)
	}
}

func TestNodeIdentityLineServer(t *testing.T) {
	writeNodeSettings(t, `{"role":"server"}`)
	want := "zpeer (server)" // hostname varies; check shape + role
	line := NodeIdentityLine()
	if !strings.Contains(line, "(server)") || !strings.HasPrefix(line, "node: ") {
		t.Errorf("line = %q, want prefix 'node: ' + '(server)' e.g. %q", line, want)
	}
	if strings.Contains(line, "→") {
		t.Errorf("server line = %q: must not carry a sync_to arrow", line)
	}
}

func TestNodeIdentityLineClientArrow(t *testing.T) {
	writeNodeSettings(t, `{"role":"client","sync_to":"zpeer"}`)
	line := NodeIdentityLine()
	if !strings.Contains(line, "(client→zpeer)") {
		t.Errorf("line = %q, want '(client→zpeer)'", line)
	}
}

func TestNodeIdentityUndeclaredEmpty(t *testing.T) {
	writeNodeSettings(t, `{}`)
	if line := NodeIdentityLine(); line != "" {
		t.Errorf("undeclared node must print no banner, got %q", line)
	}
	if b := NodeIdentityBriefing(); b != "" {
		t.Errorf("undeclared briefing must be empty, got %q", b)
	}
}

func TestNodeIdentityBriefingFormat(t *testing.T) {
	writeNodeSettings(t, `{"role":"client","sync_to":"zpeer"}`)
	b := NodeIdentityBriefing()
	if !strings.HasPrefix(b, "[node] node: ") || !strings.Contains(b, "(client→zpeer)") {
		t.Errorf("briefing = %q, want '[node] node: <host> (client→zpeer)'", b)
	}
}
