package boolvocab

// gate_test.go — the Rule 13 anti-regression guard for the boolean vocabulary.
//
// THE DEFECT. Before this package the on/off spelling lists existed as SIX
// private copies (config/env.go, config/config.go ×2, config/settings.go,
// agent/loop.go, agent/taskregister.go) whose docs contradicted each other, and
// each copy was patched separately for the SAME regression twice (2026-08-29:
// config.ApplyEnv force-writes "off" and a `!= ""` reader read it as ON — the
// raw request dump flooded stderr; 2026-08-30: deepseek's retry log went dark).
// The mirroring kept SPREADING after the first consolidation pass: the scan
// below found config/agentguard.go, internal/heavytest (×2),
// internal/signalrec (×2), internal/cgroupscope and four internal/tools
// kill-switches that no inventory had named.
//
// WHY THE DEFINITION IS A LEAF PACKAGE (and not internal/config). A mirror is
// only written when the definition is UNREACHABLE: internal/heavytest may not
// import internal/config (config → internal/scratch, and a scratch in-package
// test imports heavytest), so it "could not" import and re-spelled instead.
// Zero project imports is what makes one definition importable from every one
// of those positions (PROJECT.md Rule 14 — the internal/toolnames precedent).
//
// WHAT THE SCAN COVERS, and what it deliberately does not: three spelling
// shapes — a `switch`/`case` clause list, an `==`/`!=` comparison chain on one
// line, and the literal comma-separated list string itself. A SINGLE
// comparison against one spelling (`GOPI_ALLOW_DANGEROUS` is compared to the
// exact value "1" as a permission token; a ledger row's stored "true" is
// compared verbatim) is NOT a vocabulary: it does not claim to accept a
// spelling SET, and flagging it would teach the next agent to silence the gate.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	// vocabularyDefinitionFile is the ONE production file allowed to spell the
	// vocabulary out (Rule 13's "the literal may live only at the definition").
	vocabularyDefinitionFile = "internal/boolvocab/boolvocab.go"
	// repoRootFromLeafPkg walks from internal/boolvocab up to the repo root.
	repoRootFromLeafPkg = "../.."
	// minProductionFiles is the vacuity floor: a walk that finds fewer files
	// than this has the wrong root and the gate would pass by finding nothing.
	minProductionFiles = 50
	// minSpellingTokens is the smallest case-list size (after dropping
	// non-vocabulary tokens) that counts as a re-inlined vocabulary. A two-token
	// list is an ordinary binary branch, not a spelling set.
	minSpellingTokens = 3
	// minComparisonTokens is the same floor for the `==`/`!=` chain family.
	minComparisonTokens = 3
	// skippedDirGit is never descended into; also every dot-directory.
	skippedDirGit = ".git"
	// skippedDirBin holds build output, never Go source under version control.
	skippedDirBin = "bin"
)

// vocabularyTokens are the spellings the gate recognises — a case list or a
// comparison chain built only from these, outside the definition file, is a
// re-inlined vocabulary.
var vocabularyTokens = map[string]bool{
	"1": true, "true": true, "on": true, "yes": true, "enabled": true,
	"0": true, "false": true, "off": true, "no": true, "disabled": true,
}

var (
	// caseClauseRe matches a `case "a", "b", ... :` clause start.
	caseClauseRe = regexp.MustCompile(`\bcase\s+(?:"[^"]*"\s*,\s*)*"[^"]*"\s*:`)
	// quotedTokenRe extracts the individual quoted tokens of a case clause.
	quotedTokenRe = regexp.MustCompile(`"([^"]*)"`)
	// comparisonRe extracts `== "tok"` / `!= "tok"` comparisons.
	comparisonRe = regexp.MustCompile(`(?:==|!=)\s*"([^"]*)"`)
	// commentRe strips a Go line comment (the gate reads CODE, not prose: docs
	// and comments legitimately quote the vocabulary to explain it).
	commentRe = regexp.MustCompile(`//.*$`)
)

// spelledListLiterals are the comma-separated list strings a copy would carry.
var spelledListLiterals = []string{TrueValues, FalseValues}

// productionGoFiles returns every non-test .go file under the repo root,
// repo-root-relative and sorted, with its source.
func productionGoFiles(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	root := repoRootFromLeafPkg
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if info.IsDir() {
			if name == skippedDirGit || name == skippedDirBin ||
				(strings.HasPrefix(name, ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(filepath.Clean(path), filepath.Clean(root)+string(os.PathSeparator)))
		out[rel] = strings.Split(string(b), "\n")
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(out) < minProductionFiles {
		t.Fatalf("only %d production .go files found under %s — the walk root is wrong and this gate would pass vacuously",
			len(out), root)
	}
	return out
}

// TestBoolVocab_SingleDefinitionSite is the Rule 13 guard: no production file
// outside the definition may spell the vocabulary as a case list, a comparison
// chain, or the list literal itself.
func TestBoolVocab_SingleDefinitionSite(t *testing.T) {
	files := productionGoFiles(t)
	definitionSeen := false
	var findings []string
	rel := make([]string, 0, len(files))
	for p := range files {
		rel = append(rel, p)
	}
	sort.Strings(rel)

	for _, p := range rel {
		lines := files[p]
		if p == vocabularyDefinitionFile {
			definitionSeen = true
			for _, list := range spelledListLiterals {
				if strings.Contains(strings.Join(lines, "\n"), list) {
					continue
				}
				t.Errorf("%s no longer spells the list %q — the gate's presence half is gone (it would now pass by finding nothing)",
					vocabularyDefinitionFile, list)
			}
			continue
		}
		for i, raw := range lines {
			line := commentRe.ReplaceAllString(raw, "")
			if cl := caseClauseRe.FindString(line); cl != "" {
				toks := vocabularyTokensIn(cl)
				if len(toks) >= minSpellingTokens {
					findings = append(findings, diagnostic(p, i+1, "case clause", toks))
				}
			}
			if cmp := comparisonRe.FindAllStringSubmatch(line, -1); len(cmp) >= minComparisonTokens {
				var toks []string
				for _, m := range cmp {
					if vocabularyTokens[strings.ToLower(strings.TrimSpace(m[1]))] {
						toks = append(toks, m[1])
					}
				}
				if len(toks) >= minComparisonTokens {
					findings = append(findings, diagnostic(p, i+1, "comparison chain", toks))
				}
			}
			for _, list := range spelledListLiterals {
				if strings.Contains(line, list) {
					findings = append(findings, diagnostic(p, i+1, "list literal", []string{list}))
				}
			}
		}
	}
	if !definitionSeen {
		t.Fatalf("%s was not walked — the definition file is the gate's anchor", vocabularyDefinitionFile)
	}
	if len(findings) > 0 {
		sort.Strings(findings)
		t.Errorf("boolean vocabulary re-spelled outside %s (%d site(s)) — reference internal/boolvocab instead "+
			"(Rule 13: a constant exists to be referenced, never re-inlined; Rule 14: one definition, no mirror):\n  %s",
			vocabularyDefinitionFile, len(findings), strings.Join(findings, "\n  "))
	}
}

// vocabularyTokensIn returns the case-clause tokens that ARE vocabulary
// spellings, in order.
func vocabularyTokensIn(clause string) []string {
	var out []string
	for _, m := range quotedTokenRe.FindAllStringSubmatch(clause, -1) {
		if vocabularyTokens[strings.ToLower(strings.TrimSpace(m[1]))] {
			out = append(out, m[1])
		}
	}
	return out
}

func diagnostic(path string, line int, shape string, tokens []string) string {
	return path + ":" + itoa(line) + " — " + shape + " spells " + strings.Join(tokens, "/")
}

// itoa avoids importing strconv for one call site (the gate is a leaf too).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestBoolVocab_Semantics pins the vocabulary itself: the two lists, the
// normalization every former mirror applied, and the asymmetry that makes a
// default-ON switch safe (an unknown value is neither true nor false, so
// `!IsFalse` leaves it ON exactly as every former falsey list did).
func TestBoolVocab_Semantics(t *testing.T) {
	for _, raw := range []string{"1", "true", "on", "yes", "enabled", "TRUE", "  Yes  ", "On", "1 "} {
		if !IsTrue(raw) {
			t.Errorf("IsTrue(%q) = false, want true (the truthy set is %s)", raw, TrueValues)
		}
		if IsFalse(raw) {
			t.Errorf("IsFalse(%q) = true — a spelling may never be in both sets", raw)
		}
	}
	// The regression this vocabulary exists for: the canonical force-written
	// "off" is NOT an enabling spelling.
	for _, raw := range []string{"0", "false", "off", "no", "disabled", "FALSE", " off ", "", "  ", "nope", "enabled!", "2"} {
		if IsTrue(raw) {
			t.Errorf("IsTrue(%q) = true — a canonical/unknown value must never enable a switch "+
				"(2026-08-29: `os.Getenv != \"\"` read the force-written \"off\" as ON and dumped the raw request log)", raw)
		}
	}
	for _, raw := range []string{"0", "false", "off", "no", "disabled", "OFF", " Off "} {
		if !IsFalse(raw) {
			t.Errorf("IsFalse(%q) = false, want true (the falsey set is %s)", raw, FalseValues)
		}
	}
	for _, raw := range []string{"", "  ", "maybe", "2", "enabled!", "disabled?"} {
		if IsFalse(raw) {
			t.Errorf("IsFalse(%q) = true — an unknown value must leave a default-ON switch ON", raw)
		}
	}
	if Normalize("  TrUe\n") != "true" {
		t.Errorf("Normalize = %q, want %q (TrimSpace+ToLower is the normalization every former mirror applied)",
			Normalize("  TrUe\n"), "true")
	}
}

// TestBoolVocab_ImportsNothingFromTheProject pins the LEAF property this design
// rests on: zero project imports is why the one definition is reachable from
// internal/heavytest (which must not import internal/config) and from every
// other former mirror host. The day a project import lands here, the mirrors
// come back — with a compile error this test explains instead.
//
// It scans this package's PRODUCTION files only (the leaf claim is about the
// package a consumer imports, not about its test scaffolding), and it spells
// the import prefix in two pieces so the probe cannot match itself.
func TestBoolVocab_ImportsNothingFromTheProject(t *testing.T) {
	probe := "github.com/" + "the operator/"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		scanned++
		if strings.Contains(string(b), probe) {
			t.Errorf("%s imports the project (%s…) — boolvocab must stay a LEAF, or a consumer that cannot "+
				"reach the project (internal/heavytest) is forced back to a mirror (Rule 14)", name, probe)
		}
	}
	if scanned == 0 {
		t.Fatalf("no production .go file found in this package — the leaf claim was not checked at all")
	}
}
