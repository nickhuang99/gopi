package config

import (
	"os"
	"strings"
	"testing"
)

// The filmed-terminal knob has THREE names (flag, env, settings key) and the
// claim they make must be true of the REAL parser, not of a table read twice.
// These tests drive FlagOverrides and BootstrapEnvFromArgs — the surfaces
// `gopi --film-terminal NAME chat …` actually goes through — so the row cannot
// drift from the parser without turning a gate red.

// TestFilmTerminalFlagIsAcceptedAndHoisted covers the PRE-verb spelling: the flag
// is parsed into the config AND hoisted into the knob's env name before the
// subcommand chain, which is the mechanism that makes
// `gopi --film-terminal xfce4-terminal chat --film …` reach the chat verb (that
// verb returns from main() at the subcommand chain and never sees FlagOverrides).
func TestFilmTerminalFlagIsAcceptedAndHoisted(t *testing.T) {
	// The parse itself.
	cfg, err := FlagOverrides([]string{"--" + FilmTerminalFlagName, "xfce4-terminal", "chat"})
	if err != nil {
		t.Fatalf("--%s was refused by the parser: %v", FilmTerminalFlagName, err)
	}
	if cfg.FilmTerminal != "xfce4-terminal" {
		t.Errorf("parsed value = %q, want xfce4-terminal", cfg.FilmTerminal)
	}
	// The env-name spelling of the same row routes to the SAME case (the escape
	// hatch the help page advertises), so the two spellings cannot diverge.
	cfg2, err := FlagOverrides([]string{"--gopi-film-terminal", "xfce4-terminal", "chat"})
	if err != nil {
		t.Fatalf("the env-name spelling was refused: %v", err)
	}
	if cfg2.FilmTerminal != cfg.FilmTerminal {
		t.Errorf("--gopi-film-terminal parsed %q, --%s parsed %q", cfg2.FilmTerminal, FilmTerminalFlagName, cfg.FilmTerminal)
	}
	// The missing-value refusal names the flag (a bare flag must not be read as
	// "unset", which would silently film with the default).
	if _, err := FlagOverrides([]string{"--" + FilmTerminalFlagName}); err == nil {
		t.Error("a bare --film-terminal was accepted without a value")
	}

	// The hoist.
	t.Setenv(FilmTerminalEnv, "")
	rest := BootstrapEnvFromArgs([]string{"--" + FilmTerminalFlagName, "xfce4-terminal", "chat", "--film", "/tmp/x"})
	if got := os.Getenv(FilmTerminalEnv); got != "xfce4-terminal" {
		t.Errorf("the flag did not reach %s (got %q) — `gopi --%s NAME chat` would silently film with the default",
			FilmTerminalEnv, got, FilmTerminalFlagName)
	}
	want := []string{"chat", "--film", "/tmp/x"}
	if strings.Join(rest, " ") != strings.Join(want, " ") {
		t.Errorf("remaining args = %v, want %v (the hoist must consume the flag AND its value)", rest, want)
	}
}

// TestFilmTerminalCatalogRowIsWired ties the three names to the one catalog row
// that declares them: a row whose env/flag spellings disagree with the constants
// the resolver reads is a knob that works on one channel and is unreachable on
// another (Rules 1b/13/14).
func TestFilmTerminalCatalogRowIsWired(t *testing.T) {
	var found *envKey
	for i := range EnvVars {
		if EnvVars[i].Env == FilmTerminalEnv {
			found = &EnvVars[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no catalog row for %s — the knob is real but undocumented, which is the state the catalog exists to prevent", FilmTerminalEnv)
	}
	if found.CLIFlag != FilmTerminalFlagName {
		t.Errorf("catalog CLIFlag = %q, want %q", found.CLIFlag, FilmTerminalFlagName)
	}
	if found.Key != FilmTerminalSettingsKey {
		t.Errorf("catalog settings key = %q, want %q", found.Key, FilmTerminalSettingsKey)
	}
	if strings.TrimSpace(found.Help) == "" {
		t.Error("the catalog row carries no help text")
	}
	// The flag is a VALUE-carrying scalar: reading it as a boolean would swallow
	// the terminal name as a positional.
	scalar, _, _ := flagArity()
	if !scalar[FilmTerminalFlagName] {
		t.Errorf("--%s is not registered as a value-carrying flag", FilmTerminalFlagName)
	}
	env, takesValue, ok := AgentFlagToken(FilmTerminalFlag)
	if !ok {
		t.Errorf("AgentFlagToken does not know %s — a subcommand that does not parse it could not point the caller at the working spelling", FilmTerminalFlag)
	} else {
		if env != FilmTerminalEnv {
			t.Errorf("AgentFlagToken(%s) = %q, want %q", FilmTerminalFlag, env, FilmTerminalEnv)
		}
		// The THIRD return is the shape of the working spelling the hint prints:
		// this row is a scalar, so the hint must name `--film-terminal VALUE`.
		// Measured defect it prevents (2026-10-27): the hint printed `VALUE` for
		// every row, including the boolean ones, where it becomes the value.
		if !takesValue {
			t.Errorf("AgentFlagToken(%s) says it takes no value — a subcommand's hint would then print `%s` with no value slot and the operator's terminal name would land as a positional", FilmTerminalFlag, FilmTerminalFlag)
		}
	}
}
