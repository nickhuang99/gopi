package shot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The four channels of the filmed-terminal knob, each exercised through the ONE
// resolver (ResolveFilmTerminal) rather than through a copy of the ranking. A
// test that re-implemented "flag, then env, then settings" would agree with
// itself while the resolver disagreed with both.

// writeSettings drops a settings.json carrying the filmed terminal (and nothing
// else) into the test's own directory and points GOPI_SETTINGS at it — the
// channel the resolver reads last before the default.
func writeSettings(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", path)
}

func TestResolveFilmTerminalChannels(t *testing.T) {
	t.Run("flag wins over env and settings", func(t *testing.T) {
		writeSettings(t, `{"film_terminal":"xterm"}`)
		t.Setenv(FilmTerminalEnv, FilmTerminalNameXterm)
		spec, source, err := ResolveFilmTerminal(FilmTerminalNameXfce4)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if spec.Name != FilmTerminalNameXfce4 || source != FilmTerminalSourceFlag {
			t.Errorf("got %q via %q, want %q via the flag", spec.Name, source, FilmTerminalNameXfce4)
		}
	})
	t.Run("env wins over settings", func(t *testing.T) {
		writeSettings(t, `{"film_terminal":"xterm"}`)
		t.Setenv(FilmTerminalEnv, FilmTerminalNameXfce4)
		spec, source, err := ResolveFilmTerminal("")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if spec.Name != FilmTerminalNameXfce4 || source != FilmTerminalSourceEnv {
			t.Errorf("got %q via %q, want %q via %s", spec.Name, source, FilmTerminalNameXfce4, FilmTerminalEnv)
		}
	})
	t.Run("settings decides when no flag and no env", func(t *testing.T) {
		writeSettings(t, `{"film_terminal":"xfce4-terminal"}`)
		spec, source, err := ResolveFilmTerminal("")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if spec.Name != FilmTerminalNameXfce4 || source != FilmTerminalSourceSettings {
			t.Errorf("got %q via %q, want %q via the settings key", spec.Name, source, FilmTerminalNameXfce4)
		}
	})
	t.Run("nobody asked resolves to the built-in default", func(t *testing.T) {
		writeSettings(t, `{}`)
		spec, source, err := ResolveFilmTerminal("")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if spec.Name != FilmTerminalDefault || source != FilmTerminalSourceDefault {
			t.Errorf("got %q via %q, want %q via the default", spec.Name, source, FilmTerminalDefault)
		}
	})
}

// TestResolveFilmTerminalNeverFallsBackOnATypo is the Rule-15 heart of the knob:
// a name that is not a terminal ENDS the resolution on EVERY channel — the run
// that asked for it must not silently be made in xterm instead, because the
// operator would then read a film of a terminal they did not choose.
func TestResolveFilmTerminalNeverFallsBackOnATypo(t *testing.T) {
	writeSettings(t, `{}`)
	for _, ch := range []struct {
		name string
		flag string
		env  string
		want string
	}{
		{name: "flag", flag: "xterm-terminal", want: FilmTerminalSourceFlag},
		{name: "env", env: "konsole", want: FilmTerminalSourceEnv},
	} {
		t.Run(ch.name, func(t *testing.T) {
			t.Setenv(FilmTerminalEnv, ch.env)
			spec, source, err := ResolveFilmTerminal(ch.flag)
			if err == nil {
				t.Fatalf("a typo resolved to %q via %q — it must be refused", spec.Name, source)
			}
			if source != ch.want {
				t.Errorf("the refusal must name the channel that carried the typo (%q), got %q", ch.want, source)
			}
			if !strings.Contains(err.Error(), FilmTerminalUsableList()) {
				t.Errorf("the refusal must print the usable list, got: %v", err)
			}
		})
	}
	// The settings channel refuses too, and says it came from the settings key.
	t.Run("settings", func(t *testing.T) {
		t.Setenv(FilmTerminalEnv, "")
		writeSettings(t, `{"film_terminal":"xterminal"}`)
		_, source, err := ResolveFilmTerminal("")
		if err == nil {
			t.Fatal("a typo in the settings file was accepted")
		}
		if source != FilmTerminalSourceSettings {
			t.Errorf("want the settings channel named, got %q", source)
		}
	})
}

// TestResolveFilmTerminalRefusesAnUnusableKnownTerminal keeps the two refusals
// distinct on every channel: a terminal the driver KNOWS and cannot use is not a
// typo, and the difference is what tells the operator to change the knobs rather
// than the spelling.
func TestResolveFilmTerminalRefusesAnUnusableKnownTerminal(t *testing.T) {
	writeSettings(t, `{}`)
	t.Setenv(FilmTerminalEnv, "")
	_, source, err := ResolveFilmTerminal(FilmTerminalNameGnome)
	if err == nil {
		t.Fatal("gnome-terminal resolved as usable")
	}
	if source != FilmTerminalSourceFlag {
		t.Errorf("want the flag channel named, got %q", source)
	}
	for _, want := range []string{"known but NOT usable", "org.gnome.Terminal", FilmTerminalUsableList()} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// TestResolveFilmTerminalUnreadableSettingsIsAnError — a settings file that
// cannot be READ is not "nobody asked": reading a terminal out of a file that
// failed to load would pick one nobody chose (Rule 15). A file that is simply
// ABSENT is the ordinary "nothing configured" case (config.FromSettingsFile's own
// documented contract) and correctly falls through to the default, so the two are
// exercised apart: a tree where they were conflated is a knob nobody can debug.
func TestResolveFilmTerminalUnreadableSettingsIsAnError(t *testing.T) {
	t.Setenv(FilmTerminalEnv, "")
	dir := t.TempDir()

	// (a) an absent file is "nothing configured" — the default decides.
	t.Setenv("GOPI_SETTINGS", filepath.Join(dir, "absent.json"))
	spec, source, err := ResolveFilmTerminal("")
	if err != nil {
		t.Fatalf("an absent settings file must not fail the resolution: %v", err)
	}
	if spec.Name != FilmTerminalDefault || source != FilmTerminalSourceDefault {
		t.Errorf("absent settings: got %q via %q, want the default", spec.Name, source)
	}

	// (b) a file that cannot be PARSED is an error — never a silent default.
	bad := filepath.Join(dir, "broken.json")
	if werr := os.WriteFile(bad, []byte("{ this is not json"), 0o600); werr != nil {
		t.Fatalf("write broken settings: %v", werr)
	}
	t.Setenv("GOPI_SETTINGS", bad)
	if _, _, rerr := ResolveFilmTerminal(""); rerr == nil {
		t.Fatal("an unparseable settings file resolved silently to the default")
	} else if !strings.Contains(rerr.Error(), bad) {
		t.Errorf("the refusal must name the file it could not read, got: %v", rerr)
	}
}
