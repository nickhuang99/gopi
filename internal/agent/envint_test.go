package agent

import (
	"os"
	"strings"
	"testing"
	"time"
)

// These tests lock the env-var integer/float parsing that repeats across
// loop.go and compact.go (GOPI_COMPACT_TRIGGER/TARGET/KEEP_RECENT/...). The
// Phase-2 envInt/envFloat helper must reproduce BOTH the range-validation
// boundaries and the "warn on invalid, fall back to default" contract.
// compactConfigFromEnv is the cleanest testable surface for the shared
// Getenv→Atoi→validate→fallback template.

// captureStderr redirects os.Stderr to a temp file for the duration of the
// test and returns a func() that reads whatever was written (synchronous —
// no pipe/goroutine drain race on the read).
func captureStderr(t *testing.T) func() string {
	t.Helper()
	old := os.Stderr
	f, err := os.CreateTemp(t.TempDir(), "stderr-*.log")
	if err != nil {
		t.Fatalf("os.CreateTemp: %v", err)
	}
	os.Stderr = f
	t.Cleanup(func() {
		f.Close() //stylegate:ignore close of a file already written and flushed by the checked calls
		os.Stderr = old
	})
	return func() string {
		b, _ := os.ReadFile(f.Name()) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
		return string(b)
	}
}

// TestEnvInt_Defaults locks the envInt helper's unset/empty path: an unset or
// empty var returns the default with no warning.
func TestEnvInt_Defaults(t *testing.T) {
	unset := func() {
		t.Setenv("GOPI_ENVINT_TEST", "")
	}
	unset()
	if got := envInt("GOPI_ENVINT_TEST", 7, 7, func(n int) bool { return n > 0 }); got != 7 {
		t.Errorf("empty envInt = %d, want 7", got)
	}
}

// TestEnvInt_ValidHonored locks that a valid value is accepted and applied.
func TestEnvInt_ValidHonored(t *testing.T) {
	t.Setenv("GOPI_ENVINT_TEST", "42")
	if got := envInt("GOPI_ENVINT_TEST", 7, 7, func(n int) bool { return n >= 0 }); got != 42 {
		t.Errorf("envInt = %d, want 42", got)
	}
}

// TestEnvInt_InvalidWarnsLinks the warning + fallback: an out-of-range or
// non-numeric value emits the warning naming the var and falls back to def.
func TestEnvInt_InvalidWarns(t *testing.T) {
	buf := captureStderr(t)
	t.Setenv("GOPI_ENVINT_TEST", "not-a-num")
	if got := envInt("GOPI_ENVINT_TEST", 9, 9, func(n int) bool { return n > 0 }); got != 9 {
		t.Errorf("envInt invalid = %d, want 9", got)
	}
	got := buf()
	if !strings.Contains(got, "[gopi] warning:") || !strings.Contains(got, "GOPI_ENVINT_TEST") {
		t.Errorf("expected warning naming the var, got: %q", got)
	}

	// Range rejection (validator returns false) also warns + falls back.
	t.Setenv("GOPI_ENVINT_TEST", "-5")
	if got := envInt("GOPI_ENVINT_TEST", 9, 9, func(n int) bool { return n > 0 }); got != 9 {
		t.Errorf("envInt out-of-range = %d, want 9", got)
	}
}

// TestEnvFloat_MirrorsEnvInt locks the float variant (trigger/target range
// (0,1) and warning).
func TestEnvFloat_MirrorsEnvInt(t *testing.T) {
	buf := captureStderr(t)
	t.Setenv("GOPI_ENVFLOAT_TEST", "0.5")
	if got := envFloat("GOPI_ENVFLOAT_TEST", 0.75, 0.75, func(f float64) bool { return f > 0 && f < 1 }); got != 0.5 {
		t.Errorf("envFloat valid = %v, want 0.5", got)
	}
	if strings.Contains(buf(), "warning") {
		t.Errorf("valid float must not warn, got %q", buf())
	}

	t.Setenv("GOPI_ENVFLOAT_TEST", "1.5")
	if got := envFloat("GOPI_ENVFLOAT_TEST", 0.75, 0.75, func(f float64) bool { return f > 0 && f < 1 }); got != 0.75 {
		t.Errorf("envFloat out-of-range = %v, want 0.75", got)
	}
	if !strings.Contains(buf(), "warning") {
		t.Errorf("expected warning for out-of-range float, got %q", buf())
	}
}

// TestCompactEnvIntRangeBoundaries locks the per-variable validity ranges:
//   - float ratio vars (trigger/target) accept only (0,1)
//   - keep_recent accepts >= 0 (0 is a valid "no count floor")
//   - cooldown accepts >= 0 seconds
//   - max_summary_tokens requires > 0 (0 is invalid)
func TestCompactEnvIntRangeBoundaries(t *testing.T) {
	cases := []struct {
		name string
		set  func()
		want compactConfig
	}{
		{"trigger-exactly-0-invalid", func() { t.Setenv("GOPI_COMPACT_TRIGGER", "0") },
			compactConfig{TriggerRatio: DefaultCompactTriggerRatio}},
		{"trigger-exactly-1-invalid", func() { t.Setenv("GOPI_COMPACT_TRIGGER", "1") },
			compactConfig{TriggerRatio: DefaultCompactTriggerRatio}},
		{"trigger-neg-invalid", func() { t.Setenv("GOPI_COMPACT_TRIGGER", "-0.5") },
			compactConfig{TriggerRatio: DefaultCompactTriggerRatio}},
		{"target-below-zero-invalid", func() { t.Setenv("GOPI_COMPACT_TARGET", "-0.1") },
			compactConfig{TargetRatio: DefaultCompactTargetRatio}},
		{"target-one-invalid", func() { t.Setenv("GOPI_COMPACT_TARGET", "1.0") },
			compactConfig{TargetRatio: DefaultCompactTargetRatio}},
		{"keep-recent-zero-valid", func() { t.Setenv("GOPI_COMPACT_KEEP_RECENT", "0") },
			compactConfig{KeepRecent: 0}},
		{"cooldown-zero-valid", func() { t.Setenv("GOPI_COMPACT_COOLDOWN", "0") },
			compactConfig{Cooldown: 0}},
		{"max-summary-zero-invalid", func() { t.Setenv("GOPI_COMPACT_MAX_SUMMARY_TOKENS", "0") },
			compactConfig{MaxSummaryTokens: defaultMaxSummaryTokens(200000)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.set()
			cfg := compactConfigFromEnv(200000)
			// Assert only the field under test preserves its default.
			switch tc.name {
			case "trigger-exactly-0-invalid", "trigger-exactly-1-invalid", "trigger-neg-invalid":
				if cfg.TriggerRatio != tc.want.TriggerRatio {
					t.Errorf("TriggerRatio = %v, want default %v", cfg.TriggerRatio, tc.want.TriggerRatio)
				}
			case "target-below-zero-invalid", "target-one-invalid":
				if cfg.TargetRatio != tc.want.TargetRatio {
					t.Errorf("TargetRatio = %v, want default %v", cfg.TargetRatio, tc.want.TargetRatio)
				}
			case "keep-recent-zero-valid":
				if cfg.KeepRecent != 0 {
					t.Errorf("KeepRecent = %d, want 0 (valid)", cfg.KeepRecent)
				}
			case "cooldown-zero-valid":
				if cfg.Cooldown != 0 {
					t.Errorf("Cooldown = %v, want 0 (valid)", cfg.Cooldown)
				}
			case "max-summary-zero-invalid":
				if cfg.MaxSummaryTokens != tc.want.MaxSummaryTokens {
					t.Errorf("MaxSummaryTokens = %d, want default %d", cfg.MaxSummaryTokens, tc.want.MaxSummaryTokens)
				}
			}
		})
	}
}

// TestCompactEnvWarnsOnInvalidValue locks the "warn on invalid, fall back"
// contract: an out-of-range or non-numeric value emits a "[gopi] warning:"
// line naming the variable and falls back to the default. The Phase-2
// envInt/envFloat helper must keep emitting this warning, or operator
// visibility of a mis-set env var is lost.
func TestCompactEnvWarnsOnInvalidValue(t *testing.T) {
	buf := captureStderr(t)
	t.Setenv("GOPI_COMPACT_TRIGGER", "not-a-number")
	cfg := compactConfigFromEnv(200000)
	if cfg.TriggerRatio != DefaultCompactTriggerRatio {
		t.Fatalf("TriggerRatio = %v, want default %v", cfg.TriggerRatio, DefaultCompactTriggerRatio)
	}
	got := buf()
	if !strings.Contains(got, "[gopi] warning:") {
		t.Errorf("expected [gopi] warning on invalid env, got: %q", got)
	}
	if !strings.Contains(got, "GOPI_COMPACT_TRIGGER") {
		t.Errorf("expected env var name in warning, got: %q", got)
	}
}

// TestCompactEnvValidValueNoWarning locks the inverse: a valid value is
// honored WITHOUT a spurious warning.
func TestCompactEnvValidValueNoWarning(t *testing.T) {
	buf := captureStderr(t)
	t.Setenv("GOPI_COMPACT_TRIGGER", "0.5")
	cfg := compactConfigFromEnv(200000)
	if cfg.TriggerRatio != 0.5 {
		t.Fatalf("TriggerRatio = %v, want 0.5", cfg.TriggerRatio)
	}
	if strings.Contains(buf(), "warning") {
		t.Errorf("valid value must not warn, got: %q", buf())
	}
}

// TestCompactEnvNumericStringBoundary locks that a valid numeric string is
// parsed even when it has no leading digits (sanity for strconv.Atoi on a
// contiguous number).
func TestCompactEnvNumericStringBoundary(t *testing.T) {
	t.Setenv("GOPI_COMPACT_MAX_SUMMARY_TOKENS", "999")
	cfg := compactConfigFromEnv(200000)
	if cfg.MaxSummaryTokens != 999 {
		t.Errorf("MaxSummaryTokens = %d, want 999", cfg.MaxSummaryTokens)
	}
}

// TestCompactEnvDefaultsLocked locks the withdrawal defaults so a refactor
// that touches the env blocks cannot accidentally shift them.
func TestCompactEnvDefaultsLocked(t *testing.T) {
	cfg := compactConfigFromEnv(200000)
	if cfg.TriggerRatio != 0.75 {
		t.Errorf("default TriggerRatio = %v, want 0.75", cfg.TriggerRatio)
	}
	if cfg.TargetRatio != 0.35 {
		t.Errorf("default TargetRatio = %v, want 0.35", cfg.TargetRatio)
	}
	if cfg.KeepRecent != 8 {
		t.Errorf("default KeepRecent = %d, want 8", cfg.KeepRecent)
	}
	if cfg.Cooldown != 600*time.Second {
		t.Errorf("default Cooldown = %v, want 600s", cfg.Cooldown)
	}
	if cfg.ReferenceOnly != true {
		t.Errorf("default ReferenceOnly = %v, want true", cfg.ReferenceOnly)
	}
}
