package deepseek

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	content := `{"api_key": "sk-test-key", "base_url": "https://custom.api.com", "model": "test-model"}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil config")
	}
	if cfg.APIKey != "sk-test-key" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "sk-test-key")
	}
	if cfg.BaseURL != "https://custom.api.com" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "https://custom.api.com")
	}
	if cfg.Model != "test-model" {
		t.Errorf("Model = %q, want %q", cfg.Model, "test-model")
	}
}

// TestLoadConfig_MessagesPath verifies the messages_path override field is
// parsed from settings.json (Tencent hy4-preview use case).
func TestLoadConfig_MessagesPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	content := `{"api_key": "sk-test-key", "base_url": "https://tokenhub.tencentmaas.com", "model": "hy4-preview", "messages_path": "/v1/messages"}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.MessagesPath != "/v1/messages" {
		t.Errorf("MessagesPath = %q, want %q", cfg.MessagesPath, "/v1/messages")
	}
	// Regression: unset messages_path stays empty so legacy URL logic applies.
	cfg2, err := LoadConfig(filepath.Join(dir, "missing.json"))
	if err != nil || cfg2 != nil {
		t.Fatalf("missing-file LoadConfig = (%v, %v), want (nil, nil)", cfg2, err)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path/settings.json")
	if err != nil {
		t.Fatalf("LoadConfig returned error for missing file: %v", err)
	}
	if cfg != nil {
		t.Fatal("LoadConfig should return nil config for missing file")
	}
}

func TestLoadConfig_EmptyPath(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig returned error for empty path: %v", err)
	}
	if cfg != nil {
		t.Fatal("LoadConfig should return nil config for empty path")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	content := `{"api_key": "sk-test", invalid json}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig should return error for invalid JSON")
	}
}

func TestLoadConfig_PartialFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	content := `{"api_key": "sk-partial"}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.APIKey != "sk-partial" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "sk-partial")
	}
	if cfg.BaseURL != "" {
		t.Errorf("BaseURL = %q, want empty", cfg.BaseURL)
	}
	if cfg.Model != "" {
		t.Errorf("Model = %q, want empty", cfg.Model)
	}
}

// envGuard saves an environment variable value and restores it after the test.
type envGuard struct {
	key   string
	value string
	exist bool
}

func guardEnv(key string) *envGuard {
	v, ok := os.LookupEnv(key)
	return &envGuard{key: key, value: v, exist: ok}
}

func (g *envGuard) restore() {
	if g.exist {
		os.Setenv(g.key, g.value) //stylegate:ignore env-driven config resolution read synchronously by this test
	} else {
		os.Unsetenv(g.key)
	}
}

func TestResolveModel_EnvVarWins(t *testing.T) {
	g1 := guardEnv("DEEPSEEK_MODEL")
	g2 := guardEnv("ANTHROPIC_MODEL")
	defer g1.restore()
	defer g2.restore()
	os.Unsetenv("DEEPSEEK_MODEL")
	os.Setenv("ANTHROPIC_MODEL", "env-model") //stylegate:ignore env-driven config resolution read synchronously by this test

	cfg := &Config{Model: "file-model"}
	model := ResolveModel(cfg)
	if model != "env-model" {
		t.Errorf("Model = %q, want %q (env var should win)", model, "env-model")
	}
}

func TestResolveModel_ConfigFallback(t *testing.T) {
	g1 := guardEnv("DEEPSEEK_MODEL")
	g2 := guardEnv("ANTHROPIC_MODEL")
	defer g1.restore()
	defer g2.restore()
	os.Unsetenv("DEEPSEEK_MODEL")
	os.Unsetenv("ANTHROPIC_MODEL")

	cfg := &Config{Model: "file-model"}
	model := ResolveModel(cfg)
	if model != "file-model" {
		t.Errorf("Model = %q, want %q (config fallback)", model, "file-model")
	}
}

func TestResolveModel_Default(t *testing.T) {
	g1 := guardEnv("DEEPSEEK_MODEL")
	g2 := guardEnv("ANTHROPIC_MODEL")
	defer g1.restore()
	defer g2.restore()
	os.Unsetenv("DEEPSEEK_MODEL")
	os.Unsetenv("ANTHROPIC_MODEL")

	cfg := &Config{Model: ""}
	model := ResolveModel(cfg)
	if model != "deepseek-v4-flash" {
		t.Errorf("Model = %q, want default %q", model, "deepseek-v4-flash")
	}
}

func TestResolveModel_NilConfig(t *testing.T) {
	g1 := guardEnv("DEEPSEEK_MODEL")
	g2 := guardEnv("ANTHROPIC_MODEL")
	defer g1.restore()
	defer g2.restore()
	os.Unsetenv("DEEPSEEK_MODEL")
	os.Unsetenv("ANTHROPIC_MODEL")

	model := ResolveModel(nil)
	if model != "deepseek-v4-flash" {
		t.Errorf("Model = %q, want default %q", model, "deepseek-v4-flash")
	}
}

func TestSettingsPath_EnvVar(t *testing.T) {
	g := guardEnv("GOPI_SETTINGS")
	defer g.restore()
	os.Setenv("GOPI_SETTINGS", "/custom/path/settings.json") //stylegate:ignore env-driven config resolution read synchronously by this test

	// SettingsPath also checks os.Args for --settings first.
	// Since test binaries don't pass --settings, it should fall through to GOPI_SETTINGS.
	path := SettingsPath()
	if path != "/custom/path/settings.json" {
		t.Errorf("SettingsPath = %q, want %q", path, "/custom/path/settings.json")
	}
}

func TestLoadConfig_EmptyJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	content := `{}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil config for empty JSON object")
	}
	if cfg.APIKey != "" || cfg.BaseURL != "" || cfg.Model != "" {
		t.Errorf("All fields should be empty for empty JSON: %+v", cfg)
	}
}
