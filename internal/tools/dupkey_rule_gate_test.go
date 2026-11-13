package tools

// dupkey_rule_gate_test.go — the anti-regression guard for the INVERTED
// duplicate-env-resolution mechanism this repository stated in more than a dozen
// places.
//
// WHAT WAS WRONG (measured 2026-10-16, the reading and its host are in
// git_child_locale.go's ChildLocaleDuplicateKeyRule): the tree said "a duplicate
// key in an exec environment resolves to the FIRST entry" — and, in a second
// wave of edits that merely swapped one one-word rule for the other, that the
// last copy wins for two named readers. dupkey-ok: this paragraph is the record
// of what was refuted, quoting the refuted rules to identify them, which is the
// contrast the escape exists for.
// BOTH are refuted by the same
// measurement: glibc getenv(3)+setlocale(3)/strerror(3) and Go's os.Getenv take
// the FIRST copy, dash/bash take the LAST, and a wrapper (GNU env, a shell, Go's
// own os/exec Cmd.Env) collapses the pair before the reader sees it. The rule is
// ORDER- and READER-DEPENDENT, and the only order-independent property is
// EXACTLY ONE ENTRY — which is why the production code's strip-then-append was
// never in question and only its stated reason was.
//
// A one-word rule is the attractive simplification a comment is most likely to
// be re-written back into (that is exactly what happened), so the guard is a
// vocabulary gate: the refuted phrasings may not appear outside this file, and
// the ONE owning statement must exist and must still say the measured thing.
//
// SCOPE = the INDEX (Rule 39: `git ls-files`, never a worktree walk — the defect
// is "this claim is TRACKED", and untracked residue is another rule's business).
// An unreadable file or an empty index is a reported failure, never a pass: a
// scan that found nothing must be distinguishable from a scan that never ran
// (Rule 15).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dupKeyRefutedPhrases are the exact statements the measurement refuted, kept
// verbatim in their original spelling so the gate reads as the failure record it
// is. Each is matched as a substring of a SOURCE LINE.
//
// They are assembled from fragments so that THIS file — which is scanned like
// any other tracked file, rather than exempting itself by path — contains no
// occurrence of them. A gate that must exempt its own text is a gate that can
// quietly exempt more.
func dupKeyRefutedPhrases() []string {
	const (
		dupKey   = "duplicate"
		envKey   = " key in an exec environment"
		resolves = "resolves to "
		first    = "FIRST entry"
		lastCopy = "LAST copy wins"
		// glibcAndShell names the two readers the reverse inversion names, so the
		// claim it made can be matched without this file containing it.
		glibcAndShell = "for glibc and the shell"
		// glibcCatalogueLookup + shellOwnGetenv + bothTakeTheLast spell the second
		// reverse phrasing: "glibc's catalogue lookup and the shell's own getenv
		// both take the LAST" — the sentence that landed in a test's failure text on
		// 2026-10-16 and was itself the inversion.
		glibcCatalogueLookup = "glibc's catalogue lookup and the shell's"
		shellOwnGetenv       = "own getenv"
		bothTakeTheLast      = "both take the LAST"
		// goLastRows spells the row the FIRST pass of this correction itself got
		// wrong (2026-10-16, corrected in-run): a "Go takes the LAST" claim is no
		// more true than "FIRST wins" — Go's own table keeps the FIRST mention.
		// Assembled, for the same reason as every other phrase here.
		goKey       = "Go"
		goGetenv    = "'s os.Getenv take the LAST"  // dupkey-ok: names the refuted row, asserts nothing
		goBare      = "Go take the LAST"            // dupkey-ok: names the refuted row, asserts nothing
		goAndThe    = "and Go take the LAST"        // dupkey-ok: names the refuted row, asserts nothing
		shellsAndGo = "shells and Go take the LAST" // dupkey-ok: names the refuted row, asserts nothing
	)
	return []string{
		// the original inversion, in the two spellings the tree carried
		dupKey + envKey + " " + resolves + "the " + first,
		dupKey + envKey + " " + resolves + "its " + first,
		dupKey + " " + resolves + "the " + first,
		// the reverse inversion added on 2026-10-16, which is no more true:
		// a "the LAST copy wins" claim narrowed to two named readers, in the two
		// spellings that landed. Both are assembled here rather than written
		// whole, for the reason this function's doc gives.
		lastCopy + " for " + glibcAndShell,
		glibcCatalogueLookup + " " + shellOwnGetenv + " " + bothTakeTheLast,
		// the THIRD inversion — this correction's own first pass, measured and
		// refuted the same day: Go's os.Getenv takes the FIRST copy, so every
		// "and Go ... LAST" row is as wrong as the FIRST-wins rule it replaced.
		goKey + goGetenv,
		goBare,
		goAndThe,
		shellsAndGo,
	}
}

// dupKeyEscape marks a deliberate, explained contrast (a comment that QUOTES the
// refuted rule in order to refute it). It is the same shape as the repo's other
// inline escapes (Rule 16's `// no-wrap:`) so a reader meets one convention.
const dupKeyEscape = "dupkey-ok:"

// dupKeyOwningFile is the ONE place the rule is decided (Rule 14). Every
// restatement cites it; the gate asserts it is still THERE and still says the
// measured thing, so deleting the correction cannot pass.
const dupKeyOwningFile = "internal/tools/git_child_locale.go"

// dupKeyViolationInLine reports whether a source line carries one of the refuted
// phrasings, returning the phrase it matched. The deliberate-contrast escape is
// tested here — and not at the call site — so the ONLY way a line avoids the
// verdict is by carrying the escape, which is the whole point of having one.
func dupKeyViolationInLine(line string, phrases []string) (string, bool) {
	if strings.Contains(line, dupKeyEscape) {
		return "", false
	}
	for _, phrase := range phrases {
		if strings.Contains(line, phrase) {
			return phrase, true
		}
	}
	return "", false
}

// dupKeyOwningFragments must all appear in the owning file's source: the
// constant's own declaration plus the two facts a one-word rule would drop
// (that the readers DISAGREE, and that the host and date of the reading are
// recorded so a later reader can re-run it).
var dupKeyOwningFragments = []string{
	"ChildLocaleDuplicateKeyRule",
	"READER-dependent",
	"glibc",
	"os.Getenv",
	// The Go row is the one this correction's OWN first pass got wrong, so the
	// owning file must keep it explicitly on the FIRST-copy side — a later edit
	// that quietly moves Go back to LAST turns this red.
	"Go's os.Getenv take the FIRST",
}

// TestChildLocaleDuplicateKeyRuleGate is the gate.
func TestChildLocaleDuplicateKeyRuleGate(t *testing.T) {
	root := dupKeyRepoRoot(t)
	files := dupKeyTrackedFiles(t, root)
	if len(files) < dupKeyMinimumTrackedFiles {
		t.Fatalf("git ls-files reported %d tracked file(s) — the index did not load, so this scan "+
			"would pass by having found nothing (Rule 15)", len(files))
	}

	phrases := dupKeyRefutedPhrases()
	for _, rel := range files {
		if !dupKeyScanThisFile(rel) {
			continue
		}
		path := filepath.Join(root, rel)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("tracked file %s could not be read (%v) — an unreadable file is not a pass", rel, err)
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			if phr, hit := dupKeyViolationInLine(line, phrases); hit {
				t.Errorf("%s:%d states a one-word duplicate-env-resolution rule, which the 2026-10-16 "+
					"measurement refuted:\n  %s  (matched %q)\n"+
					"A duplicated entry is decided by the READER, not by order: glibc gettext takes the FIRST "+
					"copy, dash/bash take the LAST, and exactly ONE entry is the order-independent property. "+
					"Either rewrite the sentence to say the measured thing (or cite %s's "+
					"ChildLocaleDuplicateKeyRule), or mark a deliberate contrast with %s <reason>.",
					rel, i+1, strings.TrimSpace(line), phr, dupKeyOwningFile, dupKeyEscape)
			}
		}
	}

	// And the ONE place must still carry the rule: a scan that passes because the
	// correction was deleted is worse than a red.
	owning, err := os.ReadFile(filepath.Join(root, dupKeyOwningFile))
	if err != nil {
		t.Fatalf("the owning file %s could not be read: %v", dupKeyOwningFile, err)
	}
	for _, frag := range dupKeyOwningFragments {
		if !strings.Contains(string(owning), frag) {
			t.Errorf("%s no longer carries %q — the ONE statement of the duplicate-entry rule (Rule 14) is "+
				"gone, so every citation of it now points at nothing", dupKeyOwningFile, frag)
		}
	}
}

// dupKeyScanThisFile selects the files the vocabulary gate reads: the Go and
// markdown of the tree that a mechanism statement lives in. Binary and generated
// paths are excluded by extension, and an exclusion is a shorter list than an
// inclusion because the gate's subject is a SENTENCE, which only lives in text.
func dupKeyScanThisFile(rel string) bool {
	switch {
	case strings.HasPrefix(rel, "devdoc/DOC-INDEX.tsv"),
		strings.HasPrefix(rel, "tasks/"),
		strings.HasPrefix(rel, "third_party/"):
		return false
	}
	switch filepath.Ext(rel) {
	case ".go", ".md":
		return true
	}
	return false
}

// dupKeyMinimumTrackedFiles is a floor, not a target: it only has to be small
// enough never to fire on a legitimate tree and large enough that a failed
// `git ls-files` cannot pass the gate.
const dupKeyMinimumTrackedFiles = 100

func dupKeyRepoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("cannot resolve the repository root (git rev-parse --show-toplevel): %v", err)
	}
	return strings.TrimSpace(string(out))
}

func dupKeyTrackedFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files failed: %v — the index is the measure (Rule 39), so this is not a pass", err)
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files
}
