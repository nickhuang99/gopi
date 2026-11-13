package main

// session_replay_abolition_test.go — THE REGRESSION GATE for the one thing this
// change must not do: bring the top-level `gopi replay` verb back.
//
// WHY A GATE AND NOT A COMMENT. The playback was re-applied here from a
// hand-over branch that had spelled it as its own top-level verb, and the
// 2026-10-03 consolidation had abolished exactly that spelling on purpose
// (devdoc/replay-abolished-verb-single-route.md): replay is reached as
// `gopi chat --replay <session>` for the RE-RUN, and now as
// `gopi session replay <session>` for the PLAYBACK — a SUB-VERB of the existing
// `session` family, which is a door cmd/agent/cli_session.go had already
// reserved for it. A verb list that grows for every reader of a session id is
// the defect the abolition fixed, and a comment has never stopped a later
// ablation from re-adding the word (the census and the positional guard are the
// two places it was caught last time). So the four surfaces that would have to
// change together are checked HERE, together:
//
//	(a) the guard vocabulary   — internal/config.PositionalVerbs
//	(b) the census             — devdoc/gopi-verb-census.md
//	(c) the built binary       — `gopi replay -h` is NOT the player's page
//	(d) the source             — no CliVerbReplay identifier anywhere
//
// and the POSITIVE half of the same claim in the same file: `gopi session
// replay -h` IS the player's page, so a reader can tell "the value came back in
// the right door" from "the value was dropped".

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// replayBannedVerbWord is the top-level word the abolition removed. It is a
// LITERAL here on purpose: this gate asserts the word is dispatched NOWHERE, so
// it must not be able to follow a constant to a new home — the moment
// `replay` is a constant some production file references, the check below would
// keep passing while the verb came back.
const replayBannedVerbWord = "replay"

// replayBannedIdentifier is the identifier the abolished verb used to carry in
// this package (`CliVerbReplay = "replay"`, for the top-level dispatch and its
// completion/contract tables). The delivered sub-verb spells its printed command
// in internal/replayplay (`PlaybackCommand`) and its dispatch word in the
// session family's own table, so this identifier must have no definition and no
// reference anywhere in the tree.
const replayBannedIdentifier = "CliVerbReplay"

// TestReplayVerbStaysAbolished is the ban: the top-level word is in no
// vocabulary that could dispatch it.
func TestReplayVerbStaysAbolished(t *testing.T) {
	// (a) the positional guard. This list is what refuses `gopi --timeout 3
	// replay …` from being spent as a PROMPT; a word present here must be a
	// command, and this word is not one.
	guard := false
	for _, w := range config.PositionalVerbs {
		if w == replayBannedVerbWord {
			guard = true
		}
	}
	// Non-vacuity: an empty or collapsed list would make the loop above pass
	// without checking anything (the failure mode this repo names in other
	// gates: "found nothing" is not "never ran").
	if len(config.PositionalVerbs) < 20 {
		t.Fatalf("config.PositionalVerbs holds only %d words — the guard vocabulary looks truncated, "+
			"so the check below proves nothing", len(config.PositionalVerbs))
	}
	if guard {
		t.Errorf("config.PositionalVerbs guards %q again — the top-level `gopi %s` verb was abolished "+
			"(devdoc/replay-abolished-verb-single-route.md): the playback is `gopi %s %s <id>`",
			replayBannedVerbWord, replayBannedVerbWord, CliVerbSession, sessionSubcmdReplay)
	}

	// (b) the census. A row for the word would mean either the table invented a
	// verb or somebody re-added one; both directions of TestVerbCensusMatchesTheDispatch
	// would then be a lie about the dispatch chain, so refuse the row HERE too.
	doc, err := os.ReadFile(censusDocPathAbs(t))
	if err != nil {
		t.Fatalf("read %s: %v — the census is the surface this gate must read", censusDocPath, err)
	}
	rows := censusParseTable(t, string(doc))
	if len(rows) < censusMinRows {
		t.Fatalf("the census parsed %d rows, floor %d — the table looks truncated, so "+
			"its silence about %q proves nothing", len(rows), censusMinRows, replayBannedVerbWord)
	}
	for _, r := range rows {
		if r.verb == replayBannedVerbWord {
			t.Errorf("%s carries a row for %q again — no row may be added for the abolished verb "+
				"(the playback is `gopi %s %s <id>`, a SUB-VERB of the censused %s group)",
				censusDocPath, replayBannedVerbWord, CliVerbSession, sessionSubcmdReplay, CliVerbSession)
		}
	}

	// (d) the source: the identifier the verb used for its dispatch word.
	for _, hit := range replayIdentifierHits(t) {
		t.Errorf("%s references %s — the abolished top-level verb's constant is back "+
			"(the playback prints internal/replayplay.PlaybackCommand and dispatches through "+
			"the session family's own table)", hit, replayBannedIdentifier)
	}
}

// TestSessionReplayIsTheDoorTheValueCameBackThrough is the positive half: the
// word is not a verb, and it IS a sub-verb whose own page is the player's.
//
// Both arms drive the BUILT BINARY, because the claim is about what the operator
// can type, not about what a table says.
func TestSessionReplayIsTheDoorTheValueCameBackThrough(t *testing.T) {
	// The sub-verb is in the family's one vocabulary (the slice the dispatcher,
	// the unknown-subcommand error, the family page and TAB completion all read).
	found := false
	for _, v := range sessionSubcommandNames {
		if v == sessionSubcmdReplay {
			found = true
		}
	}
	if !found {
		t.Fatalf("%q is missing from sessionSubcommandNames — the family's vocabulary IS its dispatch",
			sessionSubcmdReplay)
	}

	bin := gopiTestBinPath(t)

	// The PLAYER's page, through the sub-verb spelling.
	got := replayRunBinary(t, bin, CliVerbSession, sessionSubcmdReplay, "-h")
	if !strings.Contains(got, sessionReplayUsageHead) {
		t.Fatalf("`gopi %s %s -h` did not print the player's page (want %q):\n%s",
			CliVerbSession, sessionSubcmdReplay, sessionReplayUsageHead, got)
	}

	// The TOP-LEVEL word: `gopi replay -h` is not a dispatched verb, so the argv
	// falls through to the ordinary path and the TOP-LEVEL help is printed — the
	// observable difference between "a verb exists here" and "this word is a
	// prompt". (No agent run is bought by an -h.)
	top := replayRunBinary(t, bin, replayBannedVerbWord, "-h")
	if strings.Contains(top, sessionReplayUsageHead) {
		t.Fatalf("`gopi %s -h` printed the PLAYER's page — the abolished top-level verb is dispatched again:\n%s",
			replayBannedVerbWord, top)
	}
	if !strings.Contains(top, "GoPi — a tool-using AI coding agent") {
		t.Fatalf("`gopi %s -h` printed neither the player's page nor the top-level help — the word is "+
			"neither dispatched nor falling through, which no state of this CLI should produce:\n%s",
			replayBannedVerbWord, top)
	}
	if re := regexp.MustCompile(`(?m)^\s*gopi\s+` + replayBannedVerbWord + `\b`); re.MatchString(top) {
		t.Fatalf("the top-level help advertises `gopi %s` again:\n%s", replayBannedVerbWord, top)
	}
}

// replayRunBinary runs the built binary with -h-style arguments and returns its
// COMBINED output. A failed invocation is fatal: the page assertions above are
// about output, and an error would make them vacuous.
func replayRunBinary(t *testing.T, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), config.GopiDirEnv+"="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("`%s %s` failed: %v\n%s", filepath.Base(bin), strings.Join(args, " "), err, out)
	}
	return string(out)
}

// replayIdentifierHits scans this repository's Go sources for the abolished
// verb's identifier and returns `path:line` for each hit.
//
// IT SCANS THE TREE, not a list of files: the identifier came back twice in this
// file's history through a table nobody thought of as "the verb" (the completion
// mirror and the contract inventory), and a hand-listed file set is how a site
// gets forgotten. The scan root is the repository the test runs in, resolved the
// same way every other source-reading gate here resolves it.
//
// ONE exclusion, and it is structural: THIS file spells the identifier twice on
// purpose (the ban's own value and its message), so it is skipped by NAME — the
// shape a value-scanning gate must use, because excluding by substring would
// also exclude any file that merely quotes it.
func replayIdentifierHits(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t) // the repository root, resolved the way every source-reading gate here does
	self := "session_replay_abolition_test.go"
	var hits []string
	for _, dir := range []string{"cmd", "internal"} {
		walk := filepath.Join(root, dir)
		err := filepath.Walk(walk, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || filepath.Base(path) == self {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(src), replayBannedIdentifier) {
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				hits = append(hits, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s for %s: %v", walk, replayBannedIdentifier, err)
		}
	}
	// A scan that found nothing and a scan that never ran must be
	// distinguishable (Rule 15): the tree it read is stated, and an empty
	// package tree is a failure of THIS check, not a pass.
	if hits == nil && !censusTreeHasGoFiles(t, root) {
		t.Fatalf("the scan of %s found no Go file at all — it proved nothing about %s",
			root, replayBannedIdentifier)
	}
	return hits
}

// censusTreeHasGoFiles reports whether the repository root holds any Go source
// under cmd/ (the floor that keeps the scan above from passing vacuously).
func censusTreeHasGoFiles(t *testing.T, root string) bool {
	t.Helper()
	found := false
	_ = filepath.Walk(filepath.Join(root, "cmd"), func(path string, info os.FileInfo, err error) error { //stylegate:ignore the tree walk's error argument is the walk's own report and is handled inside the closure; the collected paths are the assertion
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			found = true
		}
		return nil
	})
	return found
}
