package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Node identity (bug041): settings.json declares WHICH node this is
// ("role": "server"|"client") and, for clients, where its sync deltas go
// ("sync_to"). Regression guard: both fields must PARSE — the bug041 root
// cause was Config not carrying them, so a briefing saying "check role=
// client" had no data to read.
func TestRoleSyncToParsing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	// Server form: role only, no sync_to (the server is a destination).
	serverJSON := `{"role": "server"}`
	if err := os.WriteFile(path, []byte(serverJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := FromSettingsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Role != "server" {
		t.Fatalf("Role = %q, want server", cfg.Role)
	}
	if cfg.SyncTo != "" {
		t.Fatalf("SyncTo = %q, want empty on server", cfg.SyncTo)
	}

	// Client form: role + sync_to (hp → zbook).
	clientJSON := `{"role": "client", "sync_to": "zbook"}`
	if err := os.WriteFile(path, []byte(clientJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = FromSettingsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Role != "client" {
		t.Fatalf("Role = %q, want client", cfg.Role)
	}
	if cfg.SyncTo != "zbook" {
		t.Fatalf("SyncTo = %q, want zbook", cfg.SyncTo)
	}

	// Absent fields → zero values (unset role = undeclared topology).
	if err := os.WriteFile(path, []byte(`{"model":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = FromSettingsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Role != "" || cfg.SyncTo != "" {
		t.Fatalf("absent role/sync_to = %q/%q, want empty", cfg.Role, cfg.SyncTo)
	}
}

// The startup banner one-liner: server and client (→) forms render, and an
// empty role renders NOTHING (never guess identity from hostname).
func TestBannerNodeRole(t *testing.T) {
	cases := []struct {
		role, syncTo, wantSubstr string
		wantEmpty                bool
	}{
		{"server", "", "(server)", false},
		{"client", "zbook", "(client→zbook)", false},
		{"", "", "", true}, // undeclared → no banner line at all
	}
	for _, tc := range cases {
		got := NodeBanner(tc.role, tc.syncTo)
		if tc.wantEmpty {
			if got != "" {
				t.Fatalf("bannerNodeRole(%q,%q) = %q, want empty", tc.role, tc.syncTo, got)
			}
			continue
		}
		if !strings.Contains(got, tc.wantSubstr) {
			t.Fatalf("bannerNodeRole(%q,%q) = %q, want substring %q", tc.role, tc.syncTo, got, tc.wantSubstr)
		}
		if !strings.HasPrefix(got, "[gopi] node: ") {
			t.Fatalf("banner = %q, want [gopi] node: prefix", got)
		}
	}
}
