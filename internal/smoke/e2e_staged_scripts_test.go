package smoke

// The FIXTURE-CLOSURE guard.
//
// TestBranchScopedSmokeRunIsScopedAndSkipsHeavy executes the repo's OWN entry
// point inside a throwaway fixture tree, so the fixture must carry whatever
// that script can reach. It used to carry a hand-listed pair of files, and when
// scripts/test-budget.sh grew `. "$root/scripts/lib/heavy-threshold.sh"`
// (Rule 14: the heavy inventory path stays single-sourced there) the list did
// not follow: line 93 failed to source, `set -u` made heavy_inventory_rel
// fatal, the entry point never printed its `raw json: ` line, and this suite
// reported the wrong thing — "its artifact contract changed" — for a fixture
// defect. A hand-listed closure is a mirror that goes stale silently.
//
// stageScriptTree now mirrors the whole scripts/ tree, and the guard below is
// what makes the mirror's staleness a RED TEST instead of a mystery:
//
//	(a) every scripts/lib file the repo carries is present in the fixture — the
//	    shared-constants directory travels with the scripts that source it;
//	(b) EVERY path the staged scripts source (derived from their TEXT, never
//	    from a list) resolves inside the staged tree — so a source line added
//	    tomorrow is covered without editing this file. "Source" means a `.` /
//	    `source` DIRECTIVE; a quoted path in any other position (an assignment's
//	    value, a usage line, a parameter-expansion default) is a path MENTION and
//	    is not a closure edge — see sourceDirectivesIn;
//	(c) the red-proof: the same predicate, run over a scratch tree whose
//	    scripts/lib has been removed, reports the unresolvable sources — a guard
//	    that quietly stopped detecting anything cannot read green.

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// scriptsSourceDir is the shared-constants directory (Rule 14) every heavy
	// consumer sources from — the directory whose absence produced this gate's
	// red run (scripts/test-budget.sh line 93).
	scriptsSourceDir = "scripts/lib"
	// scriptSuffix identifies a shell script: the files that carry source
	// directives (and whose mode decides whether the entry point is runnable).
	scriptSuffix = ".sh"
	// sourceVerb is the `source` spelling of the POSIX `.` directive — the line
	// head that makes a path an EDGE of the fixture's source closure rather than
	// a mention of one. Both spellings occur in this repo's scripts.
	sourceVerb = "source"
	// paramDefaultPath is the parameter-expansion DEFAULT that made this gate red
	// on master: scripts/materials/gen-topB.sh names its entries file as
	// `${GOPI_MATERIALS_ENTRIES:-$ROOT/scripts/materials/entries-topB.sh}` — the
	// variable carries the real path, so the literal after `:-` is a fallback
	// VALUE and never a source edge. It is the minimal sample of the class.
	paramDefaultPath = "scripts/materials/entries-topB.sh"
	// sourceVarSigil introduces a `$NAME` expansion, and sourceExpansionOpen /
	// sourceExpansionClose bound the `${...}` spelling of the same thing — the two
	// sigils the predicate resolves a directive's argument through.
	sourceVarSigil       = "$"
	sourceExpansionOpen  = "${"
	sourceExpansionClose = "}"
	// sourceQuoteChars are the quoting characters a directive's argument may be
	// wrapped in; both spellings occur in this repo's scripts.
	sourceQuoteChars = `"'`
	// sourceShellBlanks is the whitespace a script may put between the verb and
	// its argument.
	sourceShellBlanks = " \t"
	// sourceAssignOp is the assignment operator whose RIGHT-HAND side
	// literalAssignmentsIn records for expansion.
	sourceAssignOp = "="
	// sourceStreamScopeLibAssign / sourceStreamScopeLibUse are the entry point's
	// OWN spelling of the class master's substring rule cannot see — the library
	// path held in a literal assignment and sourced through the variable. Verbatim
	// from scripts/test-budget.sh (Rule 10.4: the minimal sample is the real one).
	sourceStreamScopeLibAssign = `stream_scope_lib="` + sourceStreamScopeLibRel + `"`
	sourceStreamScopeLibUse    = `. "$root/$` + sourceStreamScopeLibName + `"`
	// sourceStreamScopeLibName is the variable, and sourceStreamScopeLibRel the
	// path it holds — the two halves the predicate joins back together.
	sourceStreamScopeLibName = "stream_scope_lib"
	sourceStreamScopeLibRel  = scriptsSourceDir + "/stream-scope.sh"
)

// TestSourceDirectivesResolveTheScriptsOwnLiteralAssignment is the non-blindness
// sample for the ONE class master's substring rule cannot see (Rule 10.4): the
// repo spells a shared library's path as `stream_scope_lib="scripts/lib/x.sh"`
// and then sources it through the variable (scripts/test-budget.sh line 154,
// sourced at line 212 as `. "$root/$stream_scope_lib"`). Master's predicate
// matches a literal `<tree>/scripts/` substring, and the variable holds no such
// substring — so that edge is INVISIBLE to it and only becomes visible when the
// predicate reads the script's own literal assignments. Pinned here as a
// predicate-level sample (the fixture guard cannot see it: the staged tree
// carries the file, so it would read green either way), which is exactly why
// the blindness was worth a case of its own.
func TestSourceDirectivesResolveTheScriptsOwnLiteralAssignment(t *testing.T) {
	// The real spelling, verbatim from scripts/test-budget.sh (Rule 10.4).
	script := strings.Join([]string{
		`#!/usr/bin/env bash`,
		`stream_scope_lib="scripts/lib/stream-scope.sh"`,
		`. "$root/$stream_scope_lib"`,
		`DEL_REPLAY_SH='scripts/replay/replay.sh' # a value, never a source`,
	}, "\n")
	got := sourceDirectivesIn(script)
	if len(got) != 1 || got[0] != "scripts/lib/stream-scope.sh" {
		t.Fatalf("sourceDirectivesIn(a script assigning then sourcing its lib) = %v, want exactly "+
			"[scripts/lib/stream-scope.sh] — the argument is read from the script's TEXT, one level of its "+
			"own literal assignment included, so the spelling the repo actually uses is not a blind spot", got)
	}

	// The repo really carries that pair, so the sample above is not a fixture of
	// my imagination: if either line is reworded, this test must fail LOUDLY
	// rather than keep asserting a shape nothing uses (Rule 15).
	raw, err := os.ReadFile(filepath.Join(e2eRepoRoot(t), filepath.FromSlash(Command)))
	if err != nil {
		t.Fatalf("read the entry point %s: %v", Command, err)
	}
	text := string(raw)
	if !strings.Contains(text, sourceStreamScopeLibAssign) || !strings.Contains(text, sourceStreamScopeLibUse) {
		t.Fatalf("%s no longer carries the literal-assignment spelling this test pins\n\twant: %s\n\tand:  %s\n"+
			"the sample has gone stale — re-derive it from the entry point rather than trusting this test",
			Command, sourceStreamScopeLibAssign, sourceStreamScopeLibUse)
	}
}

// scriptSource is one source directive a staged tree cannot resolve.
type scriptSource struct {
	script string // repo-relative script carrying the directive
	rel    string // the sourced path, repo-relative (as spelled in the script)
}

func (s scriptSource) String() string { return s.script + " sources " + s.rel }

// TestStagedScriptTreeCarriesTheSourceClosure is the guard: the fixture's
// staged script tree must be a COMPLETE mirror of the closure the entry point
// executes, so the e2e suite keeps exercising the product instead of a fixture
// artefact.
func TestStagedScriptTreeCarriesTheSourceClosure(t *testing.T) {
	repoRoot := e2eRepoRoot(t)
	tree := e2eFixtureTree(t)
	staged := stagedScriptRelPaths(t, tree)
	stagedSet := map[string]bool{}
	for _, rel := range staged {
		stagedSet[rel] = true
	}

	// (a) the repo's shared-constants files all travel into the fixture.
	repoLibs := 0
	for _, rel := range repoScriptRelPaths(t, repoRoot) {
		if !strings.HasPrefix(rel, scriptsSourceDir+"/") {
			continue
		}
		repoLibs++
		if !stagedSet[rel] {
			t.Errorf("the repo carries %s but the fixture's staged %s tree does not — the entry point "+
				"would source a file that is not there (that is the 2026-09-18 red run: line 93's source "+
				"failed, `set -u` made heavy_inventory_rel fatal, and the run printed no raw-json line)",
				rel, e2eEntryPointDir)
		}
	}
	if repoLibs == 0 {
		t.Fatalf("the repo carries no files under %s — this guard compared against nothing, so it would "+
			"pass on any fixture", scriptsSourceDir)
	}

	// (b) every source directive resolves inside the staged tree.
	shadow, measured := unresolvableScriptSources(t, tree, staged)
	if len(shadow) > 0 {
		for _, s := range shadow {
			t.Errorf("the fixture cannot satisfy a source directive: %s — stage the repo's whole %s tree "+
				"so the entry point's own closure travels with it (Rule 14 keeps the constant "+
				"single-sourced in the repo; the MIRROR is what must be complete)", s, e2eEntryPointDir)
		}
	}
	t.Logf("the closure predicate measured %d staged %s file(s)", measured, scriptSuffix)
}

// TestStagedScriptTreeCheckFailsWhenTheSourceClosureIsLost is the red-proof of
// the guard above: over a scratch copy of the repo's script tree with
// scripts/lib REMOVED (the mutation), the same predicate must report the
// unresolvable sources — including the entry point's own. Without this, a
// predicate that silently matched nothing would read green forever.
func TestStagedScriptTreeCheckFailsWhenTheSourceClosureIsLost(t *testing.T) {
	tree := repoScriptTree(t, e2eRepoRoot(t))
	if err := os.RemoveAll(filepath.Join(tree, filepath.FromSlash(scriptsSourceDir))); err != nil {
		t.Fatalf("remove %s from the scratch tree: %v", scriptsSourceDir, err)
	}
	shadow, _ := unresolvableScriptSources(t, tree, stagedScriptRelPaths(t, tree))
	if len(shadow) == 0 {
		t.Fatalf("removing %s left NO unresolvable source directive in the scratch tree — the closure "+
			"predicate is blind, so a fixture that lost %s would still read green", scriptsSourceDir, scriptsSourceDir)
	}
	entryPointFlagged := false
	for _, s := range shadow {
		if s.script == Command {
			entryPointFlagged = true
		}
		if !strings.HasPrefix(s.rel, scriptsSourceDir+"/") {
			t.Errorf("%v — the mutation removed %s, so the flagged directive should name a file under it",
				s, scriptsSourceDir)
		}
	}
	if !entryPointFlagged {
		t.Errorf("the predicate did not flag %s after %s was removed (flagged: %v) — the entry point's own "+
			"source line is exactly what went unresolved in the red run", Command, scriptsSourceDir, shadow)
	}
	t.Logf("red-proof: removing %s yields %d unresolvable source directive(s): %v",
		scriptsSourceDir, len(shadow), shadow)
}

// TestEntryPointSourceDirectivesAreSeen is the anti-blindness sample for the
// narrowed predicate (Rule 15): over the entry point's OWN text — no staging, no
// mutation — the directives it carries must be visible and must name the shared
// constants directory. That is exactly what went unresolved in the 2026-09-18 red
// run, so a predicate narrowed past it would read green over a broken fixture.
func TestEntryPointSourceDirectivesAreSeen(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(e2eRepoRoot(t), filepath.FromSlash(Command)))
	if err != nil {
		t.Fatalf("read the entry point %s: %v", Command, err)
	}
	got := sourceDirectivesIn(string(raw))
	if len(got) == 0 {
		t.Fatalf("the predicate sees no source directive in %s at all — it has been narrowed into "+
			"blindness, so a fixture missing the entry point's closure would still read green", Command)
	}
	for _, s := range got {
		if !strings.HasPrefix(s, scriptsSourceDir+"/") {
			t.Errorf("%s sources %s, which is not under %s — the entry point's closure is the shared "+
				"constants directory", Command, s, scriptsSourceDir)
		}
	}
	t.Logf("the entry point %s carries %d source directive(s): %v", Command, len(got), got)
}

// TestParameterDefaultIsNotASource is the minimal sample for the class that
// actually turned this gate red (Rule 10.4): gen-topB.sh names its entries file
// through a parameter expansion, so REMOVING that file from the scratch tree
// must change the predicate's verdict by exactly nothing. Before the narrowing
// the predicate read the fallback literal (brace included) as a source and the
// scratch tree reported an unresolvable `scripts/materials/entries-topB.sh}`.
func TestParameterDefaultIsNotASource(t *testing.T) {
	tree := repoScriptTree(t, e2eRepoRoot(t))
	rel := filepath.FromSlash(paramDefaultPath)
	before, _ := unresolvableScriptSources(t, tree, stagedScriptRelPaths(t, tree))
	for _, s := range before {
		if s.rel == paramDefaultPath {
			t.Fatalf("the scratch tree ALREADY reports %v as an unresolvable source — the parameter default "+
				"is being counted as a source directive, which is precisely the false positive that made "+
				"this gate red on master", s)
		}
	}
	if err := os.Remove(filepath.Join(tree, rel)); err != nil {
		t.Fatalf("remove %s from the scratch tree: %v — the file the assertion is about must exist in the "+
			"repo's %s tree for this minimal sample to mean anything", paramDefaultPath, err, e2eEntryPointDir)
	}
	after, _ := unresolvableScriptSources(t, tree, stagedScriptRelPaths(t, tree))
	if len(after) != len(before) {
		t.Fatalf("removing %s changed the closure verdict (%d directive(s) before, %d after: %v) — a "+
			"parameter-expansion DEFAULT is a fallback VALUE, never a source edge, so no verdict may move",
			paramDefaultPath, len(before), len(after), after)
	}
	for _, s := range after {
		if s.rel == paramDefaultPath {
			t.Errorf("%v names the REMOVED file — the predicate still reads the parameter default as a "+
				"source", s)
		}
	}
	t.Logf("parameter default %s: %d unresolvable directive(s) before and after its removal — the file is "+
		"not a source edge", paramDefaultPath, len(after))
}

// TestSourceDirectiveRedProofIsNotBlind keeps the narrowed predicate honest on
// the smallest sample there is (Rule 10.4): over a one-file scratch tree, a
// genuine `.` directive must be REPORTED whether or not it is indented, and a
// path MENTION in the same script must not be. A predicate narrowed past the
// real edges would pass the fixture guard by never matching anything.
func TestSourceDirectiveRedProofIsNotBlind(t *testing.T) {
	for _, tc := range []struct{ name, line string }{
		{"dot directive at column 0", `. "$root/scripts/lib/tmp-ban.sh"`},
		{"indented dot directive inside a guard", `    . "$root/scripts/lib/tmp-ban.sh" || exit 2`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree := t.TempDir()
			rel := e2eEntryPointDir + "/probe.sh"
			body := "#!/bin/bash\n" + tc.line + "\nMENTION=\"$root/scripts/materials/entries-topB.sh\"\n"
			if err := os.MkdirAll(filepath.Dir(filepath.Join(tree, filepath.FromSlash(rel))), 0o755); err != nil {
				t.Fatalf("create the scratch script directory: %v", err)
			}
			if err := os.WriteFile(filepath.Join(tree, filepath.FromSlash(rel)), []byte(body), 0o755); err != nil {
				t.Fatalf("write the scratch probe script: %v", err)
			}
			got, measured := unresolvableScriptSources(t, tree, []string{rel})
			if measured != 1 {
				t.Fatalf("the scratch tree exposed %d %s file(s), want exactly 1 — the sample must actually "+
					"be read", measured, scriptSuffix)
			}
			if len(got) != 1 || got[0].rel != "scripts/lib/tmp-ban.sh" {
				t.Errorf("a scratch tree holding %q reported %v — want exactly one unresolvable directive "+
					"naming scripts/lib/tmp-ban.sh (the mention beside it must stay unmatchable)", tc.line, got)
			}
		})
	}
}

// TestSourceDirectivesIgnorePathMentions is the minimal sample (Rule 10.4): the
// three false-positive classes that made this gate red on master are pinned in
// one short block. Each of them is a quoted token containing a `scripts/` path,
// and none of them is a source EDGE — a closure guard that counts them demands a
// file the script never reads (measured: master reported
// `scripts/materials/entries-topB.sh}` — brace included — as an unresolvable
// source, and the red-proof then failed because its path is not under
// scripts/lib).
func TestSourceDirectivesIgnorePathMentions(t *testing.T) {
	for _, line := range []string{
		// (1) usage/help text: a path SPLICE, and a usage line holding the path.
		`    out_arg="${root}/scripts/${VENDOR_DIR_NAME}/${arch}"`,
		`  bash scripts/build-links-static.sh --source <tarball|source-dir> [--out DIR] [--arch ARCH]`,
		`#   bash scripts/build-links-static.sh --source <links.tar.bz2|dir> [--out DIR]`,
		// (2) a parameter-expansion DEFAULT — the variable names the real,
		//     overrideable path; the literal after `:-` is a fallback value.
		`ENTRIES="${GOPI_MATERIALS_ENTRIES:-$ROOT/scripts/materials/entries-topB.sh}"`,
		// (3) a plain constant assignment used to LOOK a file up later.
		`DEL_REPLAY_SH='scripts/replay/replay.sh'`,
		`DEV_SCRATCH="$GOPI_TEST_SCRATCH_HOME/scripts/"`,
	} {
		if got := sourceDirectivesIn(line); len(got) != 0 {
			t.Errorf("sourceDirectivesIn(%q) = %v — a path mention is not a source directive, so flagging it "+
				"demands a file the script never sources", line, got)
		}
	}
}

// TestSourceDirectivesCatchEveryRealForm pins the other direction on the same
// minimal sample: the `.` / `source` spellings the repo actually uses are all
// caught, including the ones whose path is only visible through `$root`
// expansion — a predicate narrowed into blindness would read green forever.
func TestSourceDirectivesCatchEveryRealForm(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{`. "$root/scripts/lib/tmp-ban.sh"`, "scripts/lib/tmp-ban.sh"},
		{`source "$ROOT/scripts/lib/heavy-threshold.sh"`, "scripts/lib/heavy-threshold.sh"},
		{`  . "$repo_root/scripts/lib/worktree-fresh.sh" || { echo x; exit 2; }`, "scripts/lib/worktree-fresh.sh"},
		{`. "$root/scripts/lib/tmp-ban.sh"`, "scripts/lib/tmp-ban.sh"}, // unindented at column 0
		{`[[ -f "$x" ]] && . "$ROOT/scripts/lib/pick.sh"`, "scripts/lib/pick.sh"},
	} {
		got := sourceDirectivesIn(tc.line)
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("sourceDirectivesIn(%q) = %v — want exactly [%s]; a source directive the predicate "+
				"misses is a closure edge this guard stops checking", tc.line, got, tc.want)
		}
	}
}

// unresolvableScriptSources returns every source directive the tree cannot
// satisfy — the predicate the guard and its red-proof both run — together with
// the number of shell scripts it actually MEASURED. The count is returned rather
// than hidden so a caller can tell "the closure is satisfied" from "there was
// nothing to read" (Rule 15: an empty result must stay distinguishable from a
// blind one), and so a one-file scratch tree can be asserted directly.
func unresolvableScriptSources(t *testing.T, tree string, staged []string) ([]scriptSource, int) {
	t.Helper()
	var out []scriptSource
	measured := 0
	for _, rel := range staged {
		if !strings.HasSuffix(rel, scriptSuffix) {
			continue
		}
		measured++
		raw, err := os.ReadFile(filepath.Join(tree, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read the staged %s: %v", rel, err)
		}
		for _, src := range sourceDirectivesIn(string(raw)) {
			if _, err := os.Stat(filepath.Join(tree, filepath.FromSlash(src))); err != nil {
				out = append(out, scriptSource{script: rel, rel: src})
			}
		}
	}
	if measured == 0 {
		t.Fatalf("the tree under %s carries no %s file — the closure predicate measured nothing, so an "+
			"empty result here would be a blind green rather than a satisfied closure", tree, scriptSuffix)
	}
	return out, measured
}

// sourceDirectivesIn returns the repo-relative paths `content` SOURCES.
//
// A source EDGE is a line whose command is the POSIX `.` builtin or its `source`
// spelling, taking a quoted argument that names a file inside this script tree.
// The scripts spell it `"$root/scripts/lib/x.sh"`; the prefix is matched anywhere
// in the token so the `$root` spelling is not hardcoded, and the path after the
// prefix is what the closure is checked against.
//
// The argument is resolved through the file's OWN literal assignments first
// (`stream_scope_lib="scripts/lib/stream-scope.sh"` then `. "$root/$stream_scope_lib"`,
// the spelling the entry point uses), so a path held in a variable is an edge the
// predicate can still check. An argument it cannot resolve to a literal
// script-tree path (`$HERE`, `$(dirname "$0")`, an unset variable) is skipped —
// reported as unprovable, never guessed at.
//
// A quoted path that appears in any OTHER position is a path MENTION, not an
// edge, and is deliberately not returned — measured on master, three such
// classes were read as directives, were demanded of the fixture, and turned this
// guard red for paths the scripts never source:
//
//	1. usage/help text and path splices — `scripts/${VENDOR_DIR_NAME}/${arch}`
//	   (an OUTPUT directory), `<repo>/scripts/${VENDOR_DIR_NAME}/ARCH/` in a
//	   usage line;
//	2. a parameter-expansion default — `${GOPI_MATERIALS_ENTRIES:-$ROOT/scripts/
//	   materials/entries-topB.sh}`: the variable names the real path, the literal
//	   after `:-` is a fallback VALUE. It resolves to a path for a DIFFERENT
//	   variable, so the token is left carrying a `$` and dropped rather than
//	   demanded of the fixture;
//	3. a plain constant assignment — `DEL_REPLAY_SH='scripts/replay/replay.sh'`,
//	   used to LOOK the file up later, possibly in another package. It is a
//	   recorded assignment, so it is an expansion SOURCE and never a directive.
//
// Text, not a file list — that is what keeps the guard correct when a source
// line is added. The predicate is intentionally narrow; the red-proof
// (TestStagedScriptTreeCheckFailsWhenTheSourceClosureIsLost) and the
// non-blindness samples (TestSourceDirectiveRedProofIsNotBlind,
// TestSourceDirectivesResolveTheScriptsOwnLiteralAssignment,
// TestEntryPointSourceDirectivesAreSeen) are what keep the narrowing from
// becoming blindness (Rule 15: an over-narrow predicate must fail LOUDLY, never
// read green).
func sourceDirectivesIn(content string) []string {
	vars := literalAssignmentsIn(content)
	var out []string
	for _, raw := range strings.Split(content, "\n") {
		arg, ok := sourceArgument(strings.TrimSpace(scriptCode(raw)))
		if !ok {
			continue
		}
		token, ok := literalSourcePath(expandLiteralVars(arg, vars))
		if !ok {
			continue
		}
		out = append(out, token)
	}
	return out
}

// literalSourcePath returns the repo-relative script-tree path a directive's
// argument names, and whether the argument names one at all. A path the script
// computes at run time (`"$(dirname "$0")/lib/x.sh"`) is not returned: the
// predicate reports only what it can PROVE, so an argument it cannot resolve to
// a literal script-tree path is skipped — exactly as before the narrowing —
// rather than guessed at (Rule 15).
func literalSourcePath(arg string) (string, bool) {
	at := strings.Index(arg, e2eEntryPointDir+"/")
	if at < 0 {
		return "", false
	}
	rel := arg[at:]
	// The script-tree-relative part must itself be literal: a variable still
	// standing in it (`scripts/${VENDOR_DIR_NAME}/${arch}`) names no file this
	// predicate may demand of the fixture.
	if rel == "" || strings.Contains(rel, sourceVarSigil) {
		return "", false
	}
	return rel, true
}

// literalAssignmentsIn records the file's LITERAL variable assignments
// (`stream_scope_lib="scripts/lib/stream-scope.sh"`). A value that itself names
// a variable, a command substitution or a default expansion is skipped — the
// predicate substitutes literals only, never guesses, so an argument it cannot
// resolve keeps its `$…` and literalSourcePath drops it. Only the FIRST
// assignment of a name wins, which is what makes the result independent of the
// order the lines are visited in.
func literalAssignmentsIn(content string) map[string]string {
	vars := map[string]string{}
	for _, raw := range strings.Split(content, "\n") {
		code := strings.TrimLeft(scriptCode(raw), sourceShellBlanks)
		name, rest, ok := strings.Cut(code, sourceAssignOp)
		if !ok || !isShellIdentifier(name) {
			continue
		}
		val := stripOuterQuotes(strings.TrimSpace(rest))
		if val == "" || strings.Contains(val, sourceVarSigil) {
			continue
		}
		if len(strings.Fields(val)) != 1 {
			continue
		}
		if _, seen := vars[name]; !seen {
			vars[name] = val
		}
	}
	return vars
}

// expandLiteralVars substitutes the recorded literal assignments (longest name
// first, so `${stream_scope_lib}` unfolds while `${stream}` cannot eat a prefix)
// and resolves the non-empty literal half of a default expansion — the form the
// repo spells its optional library paths with, so the path a directive names
// resolves without knowing whether the caller set the variable.
func expandLiteralVars(token string, vars map[string]string) string {
	for _, name := range sortedLongestFirst(vars) {
		token = strings.ReplaceAll(token, sourceExpansionOpen+name+sourceExpansionClose, vars[name])
		token = strings.ReplaceAll(token, sourceVarSigil+name, vars[name])
	}
	return token
}

// sortedLongestFirst orders variable names longest-first, so a textual
// substitution can never be defeated by a shorter name that is its prefix.
func sortedLongestFirst(vars map[string]string) []string {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) > len(names[j])
		}
		return names[i] < names[j]
	})
	return names
}

// isShellIdentifier reports whether name is a POSIX shell identifier — the shape
// a `NAME=value` line must have for the left of its `=` to be a variable rather
// than part of a command's own text.
func isShellIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// stripOuterQuotes removes one layer of matching outer quotes.
func stripOuterQuotes(s string) string {
	if len(s) >= 2 && strings.ContainsRune(sourceQuoteChars, rune(s[0])) && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// sourceArgument returns the quoted argument of a `.` / `source` directive, with
// an unquoted trailing `|| { … }` guard dropped first, and a leading test
// (`[[ -f "$x" ]] && . "$y"`) stepped over. ok is false for any other line —
// including a command that merely STARTS with the word `source` (the repo's
// `--source PATH` option), which is not the builtin.
func sourceArgument(line string) (string, bool) {
	cmd := line
	if i := strings.Index(cmd, "||"); i >= 0 {
		cmd = cmd[:i]
	}
	if arg, ok := directiveArgument(cmd); ok {
		return arg, true
	}
	if i := strings.LastIndex(cmd, "&&"); i >= 0 {
		return directiveArgument(strings.TrimSpace(cmd[i+2:]))
	}
	return "", false
}

// directiveArgument parses a string that must BEGIN with the `.` / `source`
// builtin followed by one quoted argument.
func directiveArgument(cmd string) (string, bool) {
	arg := ""
	switch {
	case strings.HasPrefix(cmd, "."):
		arg = strings.TrimSpace(cmd[1:])
	case strings.HasPrefix(cmd, sourceVerb+string(' ')):
		arg = strings.TrimSpace(cmd[len(sourceVerb)+1:])
	default:
		return "", false
	}
	if len(arg) < 2 {
		return "", false
	}
	quote := arg[0]
	if quote != '\'' && quote != '"' {
		return "", false
	}
	end := strings.IndexByte(arg[1:], quote)
	if end < 0 {
		return "", false
	}
	return arg[1 : 1+end], true
}

// scriptCode returns a script line with any trailing UNQUOTED comment removed,
// so a path mentioned in prose (several lib files document their consumers)
// is never mistaken for a directive.
func scriptCode(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '#':
			return line[:i]
		}
	}
	return line
}

// stagedScriptRelPaths lists every file the fixture staging put under scripts/.
func stagedScriptRelPaths(t *testing.T, tree string) []string {
	t.Helper()
	root := filepath.Join(tree, filepath.FromSlash(e2eEntryPointDir))
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("the fixture carries no %s directory: %v — the e2e run would execute nothing", e2eEntryPointDir, err)
	}
	return relFilesUnder(t, tree, root, e2eEntryPointDir)
}

// repoScriptRelPaths lists every file the REPO carries under scripts/.
func repoScriptRelPaths(t *testing.T, repoRoot string) []string {
	t.Helper()
	root := filepath.Join(repoRoot, filepath.FromSlash(e2eEntryPointDir))
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("the repo carries no %s directory: %v (Command = %s cannot exist)", e2eEntryPointDir, err, Command)
	}
	return relFilesUnder(t, repoRoot, root, e2eEntryPointDir)
}

// relFilesUnder returns the slash-separated paths under root, relative to base,
// sorted — with a non-vacuity floor so a broken walk can never compare nothing.
func relFilesUnder(t *testing.T, base, root, what string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(base, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk the %s tree under %s: %v", what, root, err)
	}
	if len(out) == 0 {
		t.Fatalf("the %s tree under %s carries no files — this comparison would pass vacuously", what, root)
	}
	sort.Strings(out)
	return out
}

// repoScriptTree copies the repo's script tree into a scratch directory (a
// clone-like copy, never the repo's own tree — Rule 6: a test never mutates the
// tree it reads), so a mutation can be introduced without touching the repo.
func repoScriptTree(t *testing.T, repoRoot string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), filepath.FromSlash(e2eEntryPointDir))
	err := filepath.WalkDir(filepath.Join(repoRoot, filepath.FromSlash(e2eEntryPointDir)),
		func(srcPath string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, rerr := filepath.Rel(repoRoot, srcPath)
			if rerr != nil {
				return rerr
			}
			target := filepath.Join(filepath.Dir(dst), rel)
			if d.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			info, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			raw, rerr := os.ReadFile(srcPath)
			if rerr != nil {
				return rerr
			}
			if werr := os.WriteFile(target, raw, info.Mode().Perm()); werr != nil {
				return werr
			}
			return nil
		})
	if err != nil {
		t.Fatalf("copy the repo's %s tree: %v", e2eEntryPointDir, err)
	}
	return filepath.Dir(dst)
}
