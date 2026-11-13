package hippocampus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDefaultsAreTheDocumentedConstants: the fallback config IS the constant
// set (Rule 1/2: constants first, env as override only).
func TestDefaultsAreTheDocumentedConstants(t *testing.T) {
	c := DefaultConfig()
	if c.MaxRounds != DefaultMaxRounds || c.RetrievalLimit != DefaultRetrievalLimit ||
		c.MaxAct != DefaultMaxAct || c.Timeout != DefaultTimeout || c.ClientTimeout != DefaultClientTimeout {
		t.Errorf("DefaultConfig() = %+v, want the package constants", c)
	}
	if c.MaxRounds <= 0 || c.RetrievalLimit <= 0 || c.MaxAct <= 0 || c.Timeout <= 0 || c.ClientTimeout <= 0 {
		t.Fatalf("defaults must be positive (a zero budget would make the cycle a no-op): %+v", c)
	}
}

// TestLoadConfigEnvOverridesAndFallsBack: every knob is overridable via
// GOPI_HIPPOCAMPUS_*, and a junk/zero value keeps the constant.
func TestLoadConfigEnvOverridesAndFallsBack(t *testing.T) {
	t.Setenv(envMaxRounds, "7")
	t.Setenv(envRetrievalLimit, "9")
	t.Setenv(envMaxAct, "4")
	t.Setenv(envTimeoutSec, "3")
	t.Setenv(envClientTimeout, "2")

	c := LoadConfig()
	if c.MaxRounds != 7 || c.RetrievalLimit != 9 || c.MaxAct != 4 {
		t.Errorf("int knobs not overridden: %+v", c)
	}
	if c.Timeout != 3*time.Second || c.ClientTimeout != 2*time.Second {
		t.Errorf("duration knobs not overridden: %+v", c)
	}

	for _, bad := range []string{"0", "-1", "abc", " "} {
		t.Run("bad="+bad, func(t *testing.T) {
			t.Setenv(envMaxRounds, bad)
			if got := LoadConfig().MaxRounds; got != DefaultMaxRounds {
				t.Errorf("MaxRounds with %q = %d, want the default %d", bad, got, DefaultMaxRounds)
			}
		})
	}
}

// TestDecisionVerbsAreTheFiveConstants: the legal set is exactly the five
// verb constants, in one place (Rule 1b).
func TestDecisionVerbsAreTheFiveConstants(t *testing.T) {
	want := []string{VerbNone, VerbStore, VerbFetch, VerbUpdate, VerbFeedback}
	if len(DecisionVerbs) != len(want) {
		t.Fatalf("DecisionVerbs = %v, want %v", DecisionVerbs, want)
	}
	for i, v := range want {
		if DecisionVerbs[i] != v {
			t.Errorf("DecisionVerbs[%d] = %q, want %q", i, DecisionVerbs[i], v)
		}
		if !legalVerb(v) {
			t.Errorf("legalVerb(%q) = false", v)
		}
	}
	if legalVerb("delete") {
		t.Error("legalVerb accepted a verb outside the five")
	}
}

// TestHippocampusEnvHasOneReader (Rule 2/13): the GOPI_HIPPOCAMPUS_* env
// names may appear ONLY in config.go — a second reader would be a second
// source of truth for the organ's configuration.
func TestHippocampusEnvHasOneReader(t *testing.T) {
	const definitionFile = "config.go"
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == definitionFile {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), hippocampusEnvPrefix) {
			t.Errorf("%s references %s — read the knob in config.go's LoadConfig instead (Rule 2)",
				f, hippocampusEnvPrefix)
		}
	}
}

// TestExtractJSONObject: the decision contract tolerates a model that wraps
// its JSON in prose (the brew lesson) and never scans past the size cap.
func TestExtractJSONObject(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"bare", `{"verb":"none"}`, `{"verb":"none"}`},
		{"prose wrapped", "Sure!\n```json\n{\"verb\":\"fetch\",\"id\":3}\n```", `{"verb":"fetch","id":3}`},
		{"nested braces", `{"a":{"b":1},"c":2}`, `{"a":{"b":1},"c":2}`},
		{"brace inside string", `{"reason":"use {\"x\":1}"}`, `{"reason":"use {\"x\":1}"}`},
		{"escaped quote", `{"reason":"say \"hi\"","verb":"none"}`, `{"reason":"say \"hi\"","verb":"none"}`},
		{"no object", "no json here", ""},
		{"unbalanced", `{"verb":"none"`, ""},
	}
	for _, c := range cases {
		if got := extractJSONObject(c.in); got != c.want {
			t.Errorf("%s: extractJSONObject(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
	long := strings.Repeat("x", decideJSONMaxChars+50) + `{"verb":"none"}`
	if got := extractJSONObject(long); got != "" {
		t.Errorf("oversized reply was scanned past the cap: %q", got)
	}
}

// TestLastWordsDigest: the divergent step's seed is the context tail.
func TestLastWordsDigest(t *testing.T) {
	in := "one two three four five six seven eight nine ten eleven twelve thirteen"
	got := lastWords(in, retrievalWords)
	if len(got) != retrievalWords {
		t.Fatalf("lastWords returned %d words, want %d: %v", len(got), retrievalWords, got)
	}
	if got[len(got)-1] != "thirteen" {
		t.Errorf("seed does not end at the context tail: %v", got)
	}
	if short := lastWords("a b", retrievalWords); len(short) != 2 {
		t.Errorf("short input truncated: %v", short)
	}
}
