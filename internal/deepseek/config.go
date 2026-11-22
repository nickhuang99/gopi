package deepseek

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/configreport"
)

// Compile-time proof that this struct tag's key spelling matches the ONE
// declaration in configreport. If either side drifts, the build fails — so a
// reader of `gopi config show` can never call this live knob IGNORED because
// two spellings quietly diverged (epic 01a1eaec, Rules 1b/13).
const _ = "messages_path" == configreport.SettingsKeyMessagesPath

// Config holds settings loaded from the GoPi settings file.
type Config struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`

	// MessagesPath overrides the full Claude Messages API request path.
	// When set (e.g. "/v1/messages"), the client requests {base_url}{path}
	// verbatim — enabling providers whose Anthropic-compatible endpoint does
	// not follow the "/anthropic" prefix conventions, such as Tencent hy4
	// (https://tokenhub.tencentmaas.com/v1/messages). When empty, the legacy
	// {base}/anthropic/messages vs {base}/v1/messages suffix logic applies.
	//
	MessagesPath string `json:"messages_path"`

	// Vision endpoint (multimodal image understanding): separate model + base
	// URL so a future switch to another multimodal provider is zero-code
	// (settings.json only). Falls back to the DeepSeek Anthropic-compatible
	// endpoint and the same api_key by default.
	VisionModel   string `json:"vision_model"`
	VisionBaseURL string `json:"vision_base_url"`
}

// SettingsPath resolves the path to the settings file.
// Resolution order:
//  1. --settings flag in os.Args
//  2. GOPI_SETTINGS environment variable
//  3. ~/.gopi/settings.json (default)
func SettingsPath() string {
	// Check for --settings flag in os.Args.
	for i, arg := range os.Args {
		if arg == "--settings" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}

	// Check GOPI_SETTINGS environment variable.
	if p := os.Getenv("GOPI_SETTINGS"); p != "" {
		return p
	}

	// Default: $GOPI_HOME/.gopi/settings.json (GOPI_HOME > HOME), keeping
	// every gopi-data path on the same resolution.
	gd, err := config.GopiDir()
	if err != nil {
		return ""
	}
	return filepath.Join(gd, "settings.json")
}

// LoadConfig reads and parses the settings file at the given path.
// Returns nil Config and nil error when the file does not exist
// (the caller handles fallback to defaults / env vars).
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("deepseek: failed to parse %s: %w", path, err)
	}
	return &cfg, nil
}

// ResolveModel returns the model name.
// Resolution: ANTHROPIC_MODEL env > DEEPSEEK_MODEL env > config.model > default.
func ResolveModel(cfg *Config) string {
	if m := os.Getenv("ANTHROPIC_MODEL"); m != "" {
		return m
	}
	if m := os.Getenv("DEEPSEEK_MODEL"); m != "" {
		return m
	}
	if cfg != nil && cfg.Model != "" {
		return cfg.Model
	}
	return "deepseek-v4-flash"
}
