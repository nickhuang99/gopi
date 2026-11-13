package testutil

// THE SCRIPTS HALF of the retired-`gopi task`-verb gap.
//
// WHAT WENT WRONG (measured 2026-10-31, task 01a18f07): the verb-retirement epic
// (01a18799) removed ten `gopi task` spellings and made each one answer a
// removal sentence, but the MEASUREMENT SCRIPTS kept driving them:
//
//	$ gopi task finish-cleanup --dry-run ; echo $?     # scripts/demo-coverage.sh probed this
//	… the terminal-row close-out chain was retired …     # a sentence, not the summary line
//	1
//
// Two of those scripts (scripts/materials/gen-taskA.sh 013,
// scripts/demo-coverage.sh) carry EXPECTED VALUES for what their verb prints —
// so the row could not merely "fail": it asserted the summary line
// `finish-cleanup (dry-run): candidates=… would-run=…` of a command that will
// never print it again. A dead expected value is worse than a missing test: it
// reads as coverage while measuring the removal message.
//
// WHY A GATE AND NOT A NOTE: the retired spellings keep dispatching on purpose
// (internal/taskverbs: a removal must explain itself), so `grep` finds them in
// documentation and the notice text — a human cannot tell "explained" from
// "driven" by looking. The distinction is COMMAND POSITION, and that is what
// this gate measures: a token sequence `… gopi … task <verb>` (or `$BIN task
// <verb>`) at a position that is NOT inside a quoted string.
//
// THE EXEMPTION IS ONE COMMENT, on the same line, naming its reason
// (`retired-verb-ok: …`), and it is only for a script that must TALK about the
// retired spelling outside prose quotes. It prints every site it saw, so a
// reader can tell an empty scan from a clean one.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskverbs"
)

const (
	// retiredVerbScriptsDir is the tree this gate audits (Rule 31's "run the
	// surface you changed" lives here: these are the repo's measurement scripts).
	retiredVerbScriptsDir = "scripts"
	// retiredVerbScriptFloor / retiredVerbLineFloor are the non-vacuity floors:
	// a scan that read almost nothing must fail rather than pass.
	retiredVerbScriptFloor = 40
	retiredVerbLineFloor   = 2000
	// retiredVerbEscape is the inline permit's first half.
	retiredVerbEscape = "retired-verb-ok:"
	// retiredVerbCommandWords are the leading tokens that make `task <verb>` a
	// COMMAND: the repo's own binary spellings. `sub`, `probe`, `skip` and a
	// quoted label are deliberately absent — that is what keeps
	// `probe sub task "task finish-cleanup"` (a row LABEL) out of the scan while
	// catching the `"$BIN" task finish-cleanup` that follows on the same line.
	retiredVerbPrefix = "gopi"
	// taskVerbWord is the family word this gate looks for (the `task` of
	// `gopi task <verb>`).
	taskVerbWord = "task"
)

// heredocOpenRe recognizes the shell heredoc OPENING (`<<'EOF'`, `<<EOF`,
// `<<-EOF`): everything up to its delimiter line is DATA the script writes.
var heredocOpenRe = regexp.MustCompile(`<<-?\s*'?"?([A-Za-z_][A-Za-z0-9_]*)'?"?`)

// retiredVerb drives one line: is `task <retired>` in COMMAND position?
// It returns the verb and true, or "" and false.
func retiredVerb(t *testing.T, line string, removed map[string]bool) (string, bool) {
	t.Helper()
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	// PROSE SHAPES, by their own first character: a markdown table row, a
	// transcript prompt, a fenced/quoted line, a bullet. These carry commands as
	// SUBJECT MATTER (a generated document, a quoted removal notice), never as a
	// command — the shell would not execute `| \`gopi task x\` | …` as one.
	switch trimmed[0] {
	case '|', '$', '`', '>', '*', '-':
		return "", false
	}
	if strings.Contains(line, retiredVerbEscape) {
		return "", false
	}
	fields := strings.Fields(line)
	// byte offsets of each field, so quote parity can be computed up to it.
	offsets := make([]int, len(fields))
	pos := 0
	for i, f := range fields {
		idx := strings.Index(line[pos:], f)
		offsets[i] = pos + idx
		pos = offsets[i] + len(f)
	}
	// A match inside a quoted string is PROSE (an echo, a usage line), never a
	// command: count unescaped `"` before the field; an odd count means "inside".
	inQuote := func(off int) bool {
		n := 0
		for i := 0; i < off && i < len(line); i++ {
			if line[i] == '\\' {
				i++
				continue
			}
			if line[i] == '"' {
				n++
			}
		}
		return n%2 == 1
	}
	for i := 1; i+1 < len(fields); i++ {
		if fields[i] != taskVerbWord {
			continue
		}
		verb := strings.Trim(fields[i+1], `"'`+"`;,")
		if !removed[verb] {
			continue
		}
		// The token BEFORE `task` must be the repo's own binary: `gopi`,
		// `./bin/gopi`, `"$BIN"`, `$GOPI`. A variable's value is unknowable from
		// the text, so `"$BIN"`/`$GOPI` are accepted as names of it — that is the
		// spelling every script in this tree uses, and the alternative (only a
		// literal `gopi` word) would miss every one of them.
		prev := strings.Trim(fields[i-1], `"'`+"`;,")
		prev = strings.TrimPrefix(prev, "$")
		if base := prev[strings.LastIndex(prev, "/")+1:]; base != retiredVerbPrefix && base != "BIN" && base != "GOPI" && prev != retiredVerbPrefix {
			continue
		}
		if inQuote(offsets[i]) {
			continue
		}
		return verb, true
	}
	return "", false
}

// retiredVerbLineKinds are the four verdicts a line can get. They are named so
// the walk and the red proof speak the same vocabulary — a test that planted a
// line and a gate that judged it must agree on what it WAS.
const (
	lineDriven    = "driven"    // a real command driving a removed spelling
	linePermitted = "permitted" // the same, carrying `retired-verb-ok: <reason>`
	lineOrphan    = "orphan"    // a permit whose line no longer drives anything
	lineIgnored   = "ignored"   // prose, a comment, a heredoc body, a non-command
)

// classifyRetiredVerbLine is the ONE judgment: given a line, say which of the
// four it is. It exists so the permit's semantics are testable WITHOUT walking
// the tree — a gate whose escape hatch cannot be proven to fire is the shape
// that lets a permit silently swallow a live site forever.
//
// A permit is MEASURED, never swallowed: the comment is stripped and the SAME
// predicate asks what the line would otherwise be. A permit that permits
// nothing is an ORPHAN — the site stopped driving the verb and the permit
// outlived its reason (Rule 39: an allow-list only ratchets down).
func classifyRetiredVerbLine(t *testing.T, line string, removed map[string]bool) (kind, verb string) {
	t.Helper()
	if !strings.Contains(line, retiredVerbEscape) {
		if v, ok := retiredVerb(t, line, removed); ok {
			return lineDriven, v
		}
		return lineIgnored, ""
	}
	code := strings.SplitN(line, retiredVerbEscape, 2)[0]
	if v, ok := retiredVerb(t, code, removed); ok {
		return linePermitted, v
	}
	return lineOrphan, ""
}

func TestScriptsDoNotDriveRetiredTaskVerbs(t *testing.T) {
	root := repoRootOrFatal(t)

	removed := make(map[string]bool, len(taskverbs.RemovedVerbs))
	for _, v := range taskverbs.RemovedVerbs {
		removed[v] = true
	}
	if len(removed) == 0 {
		t.Fatalf("internal/taskverbs.RemovedVerbs is EMPTY — the gate would pass by having nothing to look for")
	}

	var (
		scripts int
		lines   int
		sites   []string
		permits []string
		orphans []string
	)
	dir := filepath.Join(root, retiredVerbScriptsDir)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".sh") {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read %s: %v — an unreadable script is not a clean script (Rule 15)", path, rerr)
		}
		scripts++
		// filepath.Rel cannot fail for a descendant of root, and this walk only
		// yields descendants — but a discarded error here would turn a scanner
		// that walked the WRONG tree into a green run reporting zero sites
		// (Rule 15). Failing loudly keeps "nothing was found" distinguishable
		// from "nothing was looked at".
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			t.Fatalf("relativise %s against root %s: %v — a scan of an unreachable tree is not a clean scan (Rule 15)",
				path, root, relErr)
		}
		heredoc := ""
		for n, line := range strings.Split(string(body), "\n") {
			lines++
			// A HEREDOC BODY IS DATA the script writes, not shell it executes
			// (`cat > file <<'MD' … \`gopi task run <id>\` … MD`). Tracking the
			// delimiter is what keeps generated documentation out of the scan
			// while a real command on the same file is still caught.
			trimmedLine := strings.TrimSpace(line)
			if heredoc != "" {
				if trimmedLine == heredoc {
					heredoc = ""
				}
				continue
			}
			if m := heredocOpenRe.FindStringSubmatch(line); m != nil {
				heredoc = m[1]
				continue
			}
			switch kind, verb := classifyRetiredVerbLine(t, line, removed); kind {
			case lineDriven:
				sites = append(sites, rel+":"+strconv.Itoa(n+1)+": `task "+verb+"` — "+strings.TrimSpace(line))
			case linePermitted:
				permits = append(permits, rel+":"+strconv.Itoa(n+1)+": `task "+verb+"`")
			case lineOrphan:
				orphans = append(orphans, rel+":"+strconv.Itoa(n+1)+": "+strings.TrimSpace(line))
			case lineIgnored:
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", retiredVerbScriptsDir, err)
	}
	if scripts < retiredVerbScriptFloor {
		t.Fatalf("scanned only %d script(s) under %s/ (floor %d) — the gate read a fragment of the tree and would pass vacuously",
			scripts, retiredVerbScriptsDir, retiredVerbScriptFloor)
	}
	if lines < retiredVerbLineFloor {
		t.Fatalf("scanned only %d line(s) (floor %d) — an empty read must not look like a clean tree",
			lines, retiredVerbLineFloor)
	}
	for _, s := range sites {
		t.Errorf("a script DRIVES a retired `gopi task` spelling (it answers a removal sentence, so this line measures the notice, not the verb): %s\n"+
			"  Replace the expectation with a LIVE door (the removal sentence names it), or say on the same line why the spelling must be typed: `%s <reason>`.",
			s, retiredVerbEscape)
	}
	for _, o := range orphans {
		t.Errorf("an ORPHAN `%s` permit: this line no longer drives a retired spelling, so the permit has outlived its reason — delete the comment (a permit only ratchets down, Rule 39): %s",
			retiredVerbEscape, o)
	}
	t.Logf("retired-verb scan: %d script(s), %d line(s), %d site(s) driving a REMOVED verb, %d permit(s) in force, %d removed spelling(s) checked",
		scripts, lines, len(sites), len(permits), len(removed))
	for _, p := range permits {
		t.Logf("  permit in force: %s", p)
	}
}

// TestRetiredVerbGateRefusesAVacuumAndItsPlantedSite keeps the gate honest in
// both directions: a tree with no scripts is a FAILURE, and a line that drives
// a removed spelling IS caught — while the same line quoted, commented, or
// permitted is not. Without this, a gate that silently stopped matching (a
// renamed helper, a broken walk) would report green forever.
func TestRetiredVerbGateRefusesAVacuumAndItsPlantedSite(t *testing.T) {
	removed := map[string]bool{}
	for _, v := range taskverbs.RemovedVerbs {
		removed[v] = true
	}
	if len(removed) == 0 {
		t.Fatalf("RemovedVerbs is empty")
	}
	driven := []string{
		`"$BIN" task finish-cleanup --dry-run`,
		`probe sub task "task verify" "0 4"   "$BIN" task verify "$ID" --timeout 3`,
		`GOPI_HOME=$H ./bin/gopi task run next --as probe`,
		`  gopi task sweep-zombies --dry-run`,
	}
	for _, line := range driven {
		if _, ok := retiredVerb(t, line, removed); !ok {
			t.Errorf("gate MISSED a command-position retired verb: %q", line)
		}
	}
	notDriven := []string{
		`# gopi task finish-cleanup was retired in 58b5618b`,
		`echo "gopi task finish-cleanup: removed — the close-out chain was retired"`,
		`skip sub task "task sweep-zombies (DELETED)" "the zombie sweeper was removed"`,
		`probe sub task "task clone (dry run)" "0 4" "$BIN" task clone "$ID" --dry-run`,
	}
	for _, line := range notDriven {
		if verb, ok := retiredVerb(t, line, removed); ok {
			t.Errorf("gate FLAGGED a legitimate line (verb %q): %q", verb, line)
		}
	}

	// THE PERMIT'S OWN SEMANTICS, both directions. A permit covers a real site
	// (linePermitted) and a permit on a line that no longer drives anything is
	// an ORPHAN — reported, never silently swallowed. Without this arm a permit
	// could swallow a live site forever and the gate would still print green.
	permitted := `"$BIN" task verify "$ID"   # retired-verb-ok: the notice text is the deliverable`
	if kind, verb := classifyRetiredVerbLine(t, permitted, removed); kind != linePermitted {
		t.Errorf("a PERMIT on a driving line must classify as %q, got %q (verb %q)", linePermitted, kind, verb)
	}
	orphan := `echo hello   # retired-verb-ok: this line stopped driving the verb years ago`
	if kind, _ := classifyRetiredVerbLine(t, orphan, removed); kind != lineOrphan {
		t.Errorf("a PERMIT whose line drives nothing must classify as %q (an orphan is a failure), got %q", lineOrphan, kind)
	}
	plain := `GOPI_HOME=$H ./bin/gopi task run next   `
	if kind, verb := classifyRetiredVerbLine(t, plain, removed); kind != lineDriven {
		t.Errorf("an unpermitted driving line must classify as %q, got %q (verb %q)", lineDriven, kind, verb)
	}
}
