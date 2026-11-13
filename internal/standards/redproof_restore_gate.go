package standardgate

import (
	"regexp"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// red-proof RESTORE criterion gate — "restored" must be measured against a
// state the restore cannot have written.
//
// THE GATE'S SUBJECT. The repository's red-proof recipe ("mutate the production
// line → run the test → observe FAIL → restore → verify the restore") is
// written, and copied, into devdoc reports and into the briefing text gopi
// renders. Nine of the ten copies end with a restore that is checked by an
// INDEX-RELATIVE diff — `git diff`, `git diff --stat`, `git diff --numstat`,
// `git diff --name-only` with no revision argument — which compares the WORKTREE
// against the INDEX. A restore performed with the other index-seeded mechanism,
// `git checkout -- <path>` / `git restore <path>` / `git checkout .`, writes the
// INDEX into the worktree. So for an ADDED file the pair is self-consistent but
// blind:
//
//	git add <path>            # the index now holds the MUTATION
//	<mutate the file>
//	git checkout -- <path>    # overwrites the worktree with the index = the
//	                          # mutation — a no-op that exits 0
//	git diff --stat <path>    # worktree vs index = EMPTY
//
// The block above is the fragile recipe QUOTED — it is the gate's subject, not a
// prescription, and it carries the gate's own inline escape so the gate does not
// report its own documentation: red-proof-restore-ok: this comment block quotes
// the fragile pair in order to refuse it; the prescriptive spelling is the
// HEAD-anchored one below.
// and the report says "restored". Measured in this run (see the devdoc report):
// the mutation survives, `git diff --stat` prints nothing, exit 0, and the file
// is byte-identical to the index while differing from HEAD. The mutated file
// then rides the branch — `git commit --amend` folds it into "the fix".
//
// WHY A GATE AND NOT A HABIT. The repo already hosts `internal/standards`, a
// `go test` suite that runs on every change, so the criterion can be checked by
// the machinery that already ticks: no new script, no scheduler, no resident
// process, no configuration key (PROJECT.md Rule 35 — the reason there is no
// helper script beside this file).
//
// THE CORRECTED CRITERION is what the gate's own report tells the reader, and
// every form of it names an EXPLICIT revision (Rule 31's "execute the surface"):
//
//	git diff HEAD -- <file>          # worktree vs the committed blob
//	git diff --stat <rev> -- <file>  # worktree vs any named rev
//	test "$(git hash-object <file>)" = "$(git rev-parse HEAD:<file>)"
//
// The third form is index-free: it compares the WORKTREE BLOB against the
// committed blob and never consults the index at all.
//
// MEASURED CORRECTION TO THE EPIC'S OWN TEXT (2026-10-12, in this run). The
// briefing that commissioned this gate proposed `git diff <file>` as the
// index-independent spelling, calling it "the short form of `git diff HEAD --
// <file>`". It is not: `git diff <path>` with no revision is worktree vs INDEX —
// exactly the comparison the gate is about — and the live proof below caught it.
// A criterion that LOOKS different and measures the same thing is worse than the
// original, so the corrected spelling is the `HEAD`-anchored one, and
// `redproofDiffIsIndexRelative` treats a bare `git diff <path>` as a hit rather
// than exempting it.
//
// WHAT IS DELIBERATELY OUT OF SCOPE (decisions, not oversights):
//
//   - devdoc/ and tasks/ are HISTORY — a record must be able to quote a
//     criterion later ruled fragile (the conflict-marker gate's rationale, same
//     reasoning). This gate scans the trees that are INSTRUCTIONS: GOPI.md,
//     devdoc/rules-reference.md, internal/ and cmd/ — the Go string literals
//     and shipped markdown gopi renders into a child's briefing.
//   - `git checkout <rev> -- <path>` and `git restore --source=<rev> <path>` are
//     EXEMPT: they seed the worktree from an explicit revision, so no index state
//     can make them a no-op. The gate's predicate has to tell that spelling apart
//     from the bare one, which is why it parses the command instead of grepping
//     for the word "checkout".
//   - `git diff --cached` / `--staged` / `--no-index` are EXEMPT: they ask a
//     different question on purpose.
//
// THE SCAN IS `git ls-files`-FREE on purpose: unlike the blob/scratch gates, the
// defect here is a line of TEXT, so the tree walk is the right instrument — but
// that makes the READ FLOOR load-bearing, and the corpus test asserts a floor on
// the scanned file count so a broken walk cannot read as a green tree.
// ---------------------------------------------------------------------------

// redproofRestoreEscape is the inline marker that suppresses a hit on its unit.
// Same shape as every other gate's escape (Rule 1: one spelling, declared once).
const redproofRestoreEscape = "red-proof-restore-ok:"

// Read floors (Rule 1: named, and asserted by the corpus test). A scan that
// walks nothing, or parses nothing, must be distinguishable from a clean tree —
// "a scan that found nothing" is otherwise indistinguishable from "a scan that
// never ran" (PROJECT.md Rule 15).
const (
	// redproofRestoreMinFiles is the floor on the number of files the walk must
	// return. The instructions trees hold ~1.5k files; a floor an order of
	// magnitude below that only fires on a broken walk, never on a legitimate
	// shrink.
	redproofRestoreMinFiles = 300
	// redproofRestoreMinUnits is the floor on parsed units. It exists because
	// file count alone cannot catch a unit-splitter that stopped splitting: a
	// splitter returning one unit per FILE would leave the file count intact.
	redproofRestoreMinUnits = 500
)

// The instruction trees. Named here (Rule 1) and kept SEPARATE from the
// devdoc/tasks corpora on purpose — see the scope note above.
var (
	// redproofRestoreGoRoots are walked for every text file (production Go, the
	// shipped briefing markdown gopi renders, and each file's comments).
	redproofRestoreGoRoots = []string{"internal", "cmd"}
	// redproofRestoreNamedFiles are the single-file instruction surfaces outside
	// those trees: the project rules and their Tier-B volume.
	redproofRestoreNamedFiles = []string{ProjectDocName, "devdoc/rules-reference.md"}
)

// redproofRestoreTextSuffixes are the file shapes the walk reads: markdown and
// Go. Anything else in those trees (a fixture, an image, a TSV) is not an
// instruction surface and reading it would only widen the gate's subjects.
var redproofRestoreTextSuffixes = []string{".go", ".md"}

// ---------------------------------------------------------------------------
// the predicate
// ---------------------------------------------------------------------------

// redproofRestoreIndexReSpellings are the two command verbs that can write the
// worktree from the index. Named as ONE table so the carve-outs below read as a
// list of decisions rather than as regex archaeology.
var redproofRestoreVerbs = []string{"checkout", "restore"}

// redproofRevRangeRe matches a revision RANGE (`a..b`, `a...b`), which is always
// an explicit base and therefore exempt.
var redproofRevRangeRe = regexp.MustCompile(`^[^\s.]+\.\.\.?[^\s]*$`)

// redproofShaRe matches an abbreviated or full commit id.
var redproofShaRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// redproofHeadRe matches HEAD and its relative spellings.
var redproofHeadRe = regexp.MustCompile(`^HEAD([~^]\d*)*$`)

// redproofKnownRevRe matches the branch spellings a human writes into a doc
// (`master`, `main`, `origin/x`). A bare word is ambiguous with a directory name
// and is treated as a REVISION here — the false-negative direction, taken on
// purpose: it can only make the gate silent, never make it fire on a correct
// usage.
var redproofKnownRevRe = regexp.MustCompile(`^(master|main|dev|HEAD|origin/[^\s]+)$`)

// redproofFileNameRe matches a token that is plainly a path: an extension we
// recognise, an angle-bracket placeholder, a glob, or a directory separator.
var redproofFileNameRe = regexp.MustCompile(`(?i)(^(?:\.|\.\.)$|\.(?:go|md|tsv|json|ya?ml|sh|py|txt|sql|sum|mod|cfg|ini|toml|lock|out|log|diff|patch)$|^<.*>$|\*|/)`)

// redproofDiffRevFlags are the diff flags that ask a question other than "what
// is the worktree diff": the index against HEAD, or two paths with no repo at
// all. Declared as data so the exemption list is reviewable (Rule 1).
var redproofDiffRevFlags = []string{"--cached", "--staged", "--no-index"}

// redproofRestoreRevFlags are the restore flags that name a source revision or
// target the index rather than the worktree. `--staged` writes HEAD into the
// index — the mirror of the defect, never a worktree restore — and `--source`
// names the revision a restore reseeds from, so either one makes the command
// index-independent.
var redproofRestoreRevFlags = []string{"--source", "-s", "--staged"}

// redproofUnit is one claim: the maximal run of lines that a reader reads as a
// single statement. A fenced block is one unit (the recipe lives in fences); a
// blank-line-separated paragraph is one unit otherwise.
type redproofUnit struct {
	// startLine is the 1-based line the unit opens on.
	startLine int
	// text is the unit's body, newline-joined.
	text string
}

// redproofUnits splits a document into the claims a reader reads as one. It is
// deliberately the SAME unit notion the other text gate uses for fences (a
// fence opens on a run of >= 3 backticks and closes on a run at least as long,
// alone on its line) so the two gates cannot disagree about what a fenced block
// is.
func redproofUnits(lines []string) []redproofUnit {
	var out []redproofUnit
	var para []string
	paraStart := 0
	flush := func() {
		if len(para) == 0 {
			return
		}
		out = append(out, redproofUnit{startLine: paraStart, text: strings.Join(para, "\n")})
		para = nil
	}
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		run := fenceOpeningRun(raw)
		if run != "" {
			flush()
			body := []string{raw}
			j := i + 1
			for j < len(lines) {
				body = append(body, lines[j])
				if closeRun := fenceOpeningRun(lines[j]); closeRun != "" &&
					len(closeRun) >= len(run) &&
					strings.TrimSpace(lines[j]) == closeRun {
					break
				}
				j++
			}
			out = append(out, redproofUnit{startLine: i + 1, text: strings.Join(body, "\n")})
			i = j
			continue
		}
		if strings.TrimSpace(raw) == "" {
			flush()
			continue
		}
		if len(para) == 0 {
			paraStart = i + 1
		}
		para = append(para, raw)
	}
	flush()
	return out
}

// redproofCommand is one parsed git invocation inside a unit.
type redproofCommand struct {
	// at is the byte offset of the `git` token in the unit text, so the
	// predicate can order the restore against the criterion.
	at int
	// restore is true when this command writes the WORKTREE from the INDEX —
	// the form that is a no-op on an added file.
	restore bool
	// indexDiff is true when this command is an index-relative diff read: a
	// comparison whose right-hand side is the index, blind to an index-only
	// change.
	indexDiff bool
}

// redproofCommands parses every `git <verb>` invocation in one unit and reports
// what each one does. Pure over the text (no filesystem): the predicate must be
// unit-testable without a repo, which is what the corpus test relies on.
func redproofCommands(text string) []redproofCommand {
	var out []redproofCommand
	for _, m := range redproofGitCmdRe.FindAllStringSubmatchIndex(text, -1) {
		verb := text[m[2]:m[3]]
		span := redproofPathTokens(text[m[1]:])
		toks := span
		switch verb {
		case "checkout":
			if redproofCheckoutFromIndex(toks) {
				out = append(out, redproofCommand{at: m[0], restore: true})
			}
		case "restore":
			if redproofRestoreFromIndex(toks) {
				out = append(out, redproofCommand{at: m[0], restore: true})
			}
		case "diff":
			if redproofDiffIsIndexRelative(toks) {
				out = append(out, redproofCommand{at: m[0], indexDiff: true})
			}
		}
	}
	return out
}

// redproofGitCmdRe finds a `git <verb>` head. The verb set is closed (only the
// three verbs carry a meaning here), so a `git log`/`git show` mention is not a
// command candidate at all.
var redproofGitCmdRe = regexp.MustCompile("\\bgit\\s+(checkout|restore|diff)\\b`?")

// redproofPathTokens cuts one command's argument list out of the surrounding
// prose — three cuts, each one measured against a real document shape: a closing
// backtick (a document wraps a command in backticks far more often than it fences
// it), the end of the line, and the first shell separator. Without them the words
// AFTER a command inside a sentence (`git checkout HEAD -- a.go; git add a.go`)
// would be parsed as its own arguments.
//
// The verb token is consumed and NOT returned: the caller dispatched on it, and
// a token loop that re-scans for its own verb is how a parser silently reads no
// arguments at all.
func redproofPathTokens(rest string) []string {
	// A document wraps a command in backticks far more often than it fences it:
	// `git diff` — the command ENDS at the closing backtick, and reading past it
	// swallows the following prose as arguments (measured: the sentence "`git
	// diff` printed nothing" otherwise parses the word "printed" as a revision
	// and exempts a genuine hit).
	if i := strings.Index(rest, "`"); i >= 0 && strings.TrimSpace(rest[:i]) == "" {
		if j := strings.Index(rest[i+1:], "`"); j >= 0 {
			rest = rest[:i+1+j]
		}
	}
	if i := strings.IndexAny(rest, "\n"); i >= 0 {
		rest = rest[:i]
	}
	for _, sep := range []string{"#", ";", "&&", "||", "|"} {
		if i := strings.Index(rest, sep); i >= 0 {
			rest = rest[:i]
		}
	}
	fields := strings.Fields(rest)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.Trim(f, "`'\"()[]")
		f = strings.Trim(f, ".,:;")
		if f == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// redproofCheckoutFromIndex decides whether one `git checkout …` writes the
// worktree from the index. The decision is parse-shaped on purpose:
//
//   - a flag (`-b`, `-q`, `--detach`, `--ours`, `--theirs`, `--force`) is not a
//     path and not a revision; only `--ours`/`--theirs` resolve from the index,
//     and they are the MERGE spelling rather than the restore spelling, so they
//     are left out (`redproofRestoreVerbFlags` — the default here — refuses a
//     command that carries no positional path).
//   - `git checkout .` / `git checkout -- <path>` are the two UNAMBIGUOUS
//     index-seeded forms: the whole-tree spelling, and the `--`-separated path.
//   - `git checkout <path>` WITHOUT a `--` is deliberately NOT reported: it is
//     indistinguishable from `git checkout <branch>` (a branch switch) inside a
//     pure text predicate, and the false-negative direction is the safe one — a
//     gate that fired on every branch switch would be routed around within a
//     day, and the fragile recipe in the tree always spells the path form with
//     its separator.
func redproofCheckoutFromIndex(toks []string) bool {
	sawRev := false
	sawSeparator := false
	wholeTree := false
	for _, t := range toks {
		if t == "--" {
			sawSeparator = true
			continue
		}
		if strings.HasPrefix(t, "-") {
			if t == "--ours" || t == "--theirs" {
				return false // the merge spelling, not a restore by path
			}
			continue
		}
		if t == "." || t == ".." {
			wholeTree = true
			continue
		}
		if redproofIsRev(t) && !sawRev {
			sawRev = true
			continue
		}
	}
	if sawRev {
		return false // seeded from an explicit revision: index state cannot matter
	}
	if wholeTree {
		return true // `git checkout .` — the index, written over the worktree
	}
	return sawSeparator && redproofAnyPath(toks)
}

// redproofAnyPath reports whether any positional token in the command names a
// path, so `git checkout --` with no path (a no-op of a different kind) is not
// reported as a restore.
func redproofAnyPath(toks []string) bool {
	for _, t := range toks {
		if strings.HasPrefix(t, "-") || redproofIsRev(t) {
			continue
		}
		if redproofFileNameRe.MatchString(t) {
			return true
		}
	}
	return false
}

// redproofRestoreFromIndex decides whether one `git restore …` writes the
// worktree from the index. `--source`/`-s` name the revision it reseeds from
// (exempt), `--staged` targets the index (a different operation), and a
// positional path is otherwise the index-seeded form.
func redproofRestoreFromIndex(toks []string) bool {
	for _, t := range toks {
		if strings.HasPrefix(t, "-") {
			for _, f := range redproofRestoreRevFlags {
				if t == f || strings.HasPrefix(t, f+"=") {
					return false
				}
			}
			continue
		}
		if redproofFileNameRe.MatchString(t) {
			return true
		}
		return redproofIsRev(t) // a bare revision (or an unclassifiable word): not index-seeded
	}
	return false
}

// redproofDiffIsIndexRelative decides whether one `git diff …` compares the
// WORKTREE against the INDEX. With no revision argument that is exactly what it
// does, so the question is whether a revision was named:
//
//   - `--cached`/`--staged`/`--no-index` ask something else by construction;
//   - anything before a `--` separator that is not a path is a revision;
//   - a bare path argument does not change the comparison's sides: the INDEX is
//     still the left-hand side, and that is the blindness the gate is about.
func redproofDiffIsIndexRelative(toks []string) bool {
	sawSeparator := false
	for _, t := range toks {
		if t == "--" {
			sawSeparator = true
			continue
		}
		if strings.HasPrefix(t, "-") {
			for _, f := range redproofDiffRevFlags {
				if t == f {
					return false
				}
			}
			continue
		}
		if sawSeparator {
			continue // a pathspec after `--`: sides unchanged
		}
		if redproofFileNameRe.MatchString(t) {
			continue // a path argument does not change the comparison's sides
		}
		if redproofIsRev(t) {
			return false // an explicit revision: worktree vs THAT revision
		}
		// Neither a path nor a revision: a PROSE word (`git diff` printed
		// nothing…) or a ref spelling this parse cannot classify. It does not
		// exempt the comparison — an exemption has to be earned by naming a
		// revision, never by failing to recognise a word.
	}
	return true
}

// redproofIsRev reports whether a positional token names a revision rather than
// a path.
func redproofIsRev(t string) bool {
	switch {
	case redproofShaRe.MatchString(t):
		return true
	case redproofHeadRe.MatchString(t):
		return true
	case redproofRevRangeRe.MatchString(t):
		return true
	case redproofKnownRevRe.MatchString(t):
		return true
	}
	return false
}

// redproofUnitHit reports the line of the first fragile pair in one unit: a
// restore-by-index command that textually PRECEDES an index-relative diff read.
// The order is what makes the pair a CRITERION rather than two independent
// facts ("we restored X … (a `git diff` elsewhere in the paragraph showed
// something else)").
func redproofUnitHit(u redproofUnit) (int, bool) {
	cmds := redproofCommands(u.text)
	for i, c := range cmds {
		if !c.restore {
			continue
		}
		for _, d := range cmds[i+1:] {
			if !d.indexDiff {
				continue
			}
			return u.startLine + strings.Count(u.text[:d.at], "\n"), true
		}
	}
	return 0, false
}

// redproofRestoreFind is the gate's predicate: it reports one hit per unit that
// pairs an index-seeded restore with an index-relative restore criterion, naming
// the line the criterion sits on.
func redproofRestoreFind(g gate, p *parsed) []hit {
	var out []hit
	lines := strings.Split(string(p.src), "\n")
	units := redproofUnits(lines)
	for i, u := range units {
		// The escape is read from the unit AND from the unit immediately after
		// it: a fenced block has no way to carry a trailing comment on its last
		// line, so the marker a document writes after a quoted recipe would
		// otherwise be unreachable (the Go gates' "comment block above" rule,
		// mirrored for a fenced or paragraph shape).
		if strings.Contains(u.text, g.escape) {
			continue
		}
		if i+1 < len(units) && strings.Contains(units[i+1].text, g.escape) {
			continue
		}
		if line, ok := redproofUnitHit(u); ok {
			out = append(out, hit{rel: p.rel, line: line, desc: redproofRestoreDesc})
		}
	}
	return out
}

// redproofRestoreDesc is the one-line description every hit carries, so the
// report is a sentence a reader can act on rather than a rule number.
const redproofRestoreDesc = "restore criterion is index-relative (`git diff` with no revision — a bare `git diff <path>` IS worktree-vs-index) paired with an index-seeded restore (`git checkout -- <path>` / `git restore <path>` / `git checkout .`) — on an ADDED file the restore is a no-op and the criterion cannot see it; measure with `git diff HEAD -- <path>` or compare `git hash-object <path>` against `git rev-parse HEAD:<path>`"

// redproofRestoreGate is the gate's registration. Its baseline is EMPTY by
// construction: the scan is scoped to the instruction surfaces (the rules, the
// shipped briefings and the Go trees), and a devdoc RECORD that quotes a fragile
// criterion is outside them on purpose — a record must be able to quote what it
// measured.
func redproofRestoreGate() gate {
	return gate{
		id: "Rule 5/31 (red-proof restore criterion must not be index-relative; a " +
			"content-identity reading must be anchored to a revision)",
		checklistRow: "5/red-proof-restore",
		probe:        "TestGateRedProofRestoreRedProof",
		scanTextRoot: textScanRoot,
		textCorpus:   redproofRestoreCorpusFilter,
		escape:       redproofRestoreEscape,
		baselineReason: "EMPTY by construction. Neither a fragile restore criterion nor an " +
			"unanchored content-identity reading is an acceptable state, and each hit carries its " +
			"own correction in the message. devdoc/ and tasks/ are NOT scanned (a record must be " +
			"able to quote the criterion it used, even a fragile one); the inline escapes — " +
			"'red-proof-restore-ok:' for the index-relative pair, 'red-proof-identity-ok:' for the " +
			"unanchored identity comparison — exist for an instruction surface that must quote the " +
			"fragile form verbatim in order to correct it.",
		baseline: redproofRestoreBaseline,
		find:     redproofIdentityHit,
	}
}

// ---------------------------------------------------------------------------
// ARM (c) — CONTENT-IDENTITY ANCHOR.
//
// THE SECOND HALF OF THE SAME RULE (Rule 5), added 2026-10-14. Arm (a)/(b)
// above police WHERE the criterion reads from (the index, which the restore
// itself writes). This arm polices WHICH TWO THINGS the criterion compares. The
// measured defect (task 01a138317d847ac3806c8a701eb652f3, 2026-10-14):
//
//	internal/standards/generated_doc_deps_test.go hash=732aeebc… saved=732aeebc… same=YES
//
// Both sides of that comparison are artifacts the SAME LOOP produced — a
// snapshot file this run wrote, and the digest of that snapshot recorded beside
// it. NEITHER side is a revision. The reading is therefore true only about the
// moment it was taken: the file was written AGAIN at 11:13:12 and the delivered
// blob is 30a26e580…, so the verdict is indexed to an intermediate state and
// says so nowhere. A revision-anchored comparison cannot have this failure
// mode, because the thing it names cannot be rewritten — a later write moves it
// and the reading turns RED by itself.
//
// THE PREDICATE. A unit that performs a CONTENT-IDENTITY comparison while
// naming NO revision anywhere is the fragile shape: the comparison has to be
// getting at least one side from a local artifact or a literal, and that side
// cannot notice a write made after the reading. A revision anchor anywhere in
// the same unit (rev-parse HEAD:path, show/cat-file/ls-tree <rev>:path, a bare
// HEAD, an abbreviated or full commit id, a named branch) is what makes the
// reading self-invalidating, so it is the exemption.
//
// DIRECTION OF ERROR, stated because it is deliberate: an unanchored identity
// comparison is a WARNING SHAPE, not a proven defect — the two sides may be a
// genuine pair of revisions spelled in a way this predicate does not classify.
// The gate therefore errs toward silence (the `redproofKnownRevRe` note above:
// a bare word is read as a revision) and its message names the correction
// rather than asserting the defect.
// ---------------------------------------------------------------------------

// redproofIdentityEscape is the inline marker that suppresses this arm on its
// unit. It is a SECOND marker rather than a reuse of redproofRestoreEscape: the
// two arms' subjects are different claims (an index-relative criterion versus an
// unanchored identity comparison), and a document that must quote one of them
// should not thereby silence the other.
const redproofIdentityEscape = "red-proof-identity-ok:"

// redproofIdentityDigestCmds are the content-digest commands whose output is a
// value compared for identity. Named as a table (Rule 1) rather than inlined in
// a regex so the vocabulary is reviewable in one place.
var redproofIdentityDigestCmds = []string{
	"hash-object", "sha256sum", "sha1sum", "sha512sum", "sha224sum", "sha384sum",
	"md5sum", "b2sum", "cksum", "shasum",
}

// redproofIdentityCmdRe matches any one of those commands. It deliberately does
// NOT include `cmp` — see redproofIdentityCmpRe for why the two are separate
// predicates.
var redproofIdentityCmdRe = regexp.MustCompile(
	`(?:^|[\s(` + redproofShellSeparators + `])(?:` +
		strings.Join(redproofIdentityDigestCmds, "|") + `)\b`)

// redproofIdentityCmpTok is the byte comparator's program name, in ONE place
// (Rule 13): the regexes below splice it, and the regression test builds its
// fixtures from it, so the subject is renameable in a single edit.
const redproofIdentityCmpTok = "cmp"

// `cmp` IS A SECOND PREDICATE, AND A STRICTER ONE (2026-10-26).
//
// WHAT WAS WRONG. `cmp` shipped inside redproofIdentityCmdRe, i.e. behind
// exactly the boundary the digest commands need and nothing more: "this word
// follows a space/paren/quote/separator". That boundary is a fact about SHELL
// TEXT; it is not a fact about Go. `cmp` is also (a) an ordinary Go identifier —
// the standard library package `cmp`, a local `cmp := func(a, b int) int`, the
// common `cmp := p` — and (b) an ordinary English word. Measured on this tree
// (task 01a1750d, probe at internal/tools/zzprobe_cmp_identity_test.go): a
// 17-line Go file that compares NO content at all —
//
//	cmpv := cmp.Compare(a, b)
//	if cmpv == 0 { return 0 }
//
// — turned TestGateRedProofRestoreIdentityRealTreeIsGreen RED at the line
// carrying `cmp`, with the arm's full "no revision anchor" message. A gate whose
// false positive is "you imported the standard library" is routed around, and a
// routed-around gate guards nothing.
//
// THE DISCRIMINATOR is the FACT the false positive gets wrong, not a longer list
// of spellings: a `cmp` comparison is a COMMAND LINE reading two path arguments,
// and an invocation NAMES ITS ARGUMENTS ON ITS OWN LINE. So the token must
// satisfy THREE conditions, all measured against the predicate below:
//
//  1. COMMAND-LINE POSITION — it follows only shell lead-in: the line start, or
//     one of [ \t(/'"|;&`$@{] (a quote, an opening brace/paren/subshell, a
//     separator, or the end of a `$( )`/`xf() {` lead-in). It must NOT be
//     preceded by an identifier, a digit, `.`, `-` or `_`: that single
//     requirement kills every Go spelling in one stroke (`cmp.Compare`,
//     `xcmp`, `TestX-cmp`, `p.cmp`, `cmp1`) because Go puts one of those
//     characters between the name and its callee.
//  2. IT IS SPACE-TERMINATED — a space or the end of the line follows it. Not
//     merely "not a dot": `cmp.Compare`, `cmp(`, `cmp;` and `cmp,` are all names
//     or prose punctuation, and each of them is refused by this one requirement.
//  3. IT NAMES ITS FILE ARGUMENTS ON THE SAME LINE — its FIRST non-flag operand
//     is file-shaped, and at least redproofIdentityCmpMinPaths of its operands
//     are arguments a file comparison can take (redproofIdentityCmpArg: a file
//     name, a token carrying `.` or `/`, a process substitution, or a short bare
//     placeholder such as the single letters of a two-file recipe). This is the
//     condition that keeps `the comparator named cmp decides …` — the arm's own
//     prose-regression negative — out, while a line that IS the byte comparison
//     (two files, with or without a flag) has it.
//
// DIRECTION OF ERROR (deliberate, and the reason condition 3 exists): a Go line
// that survived conditions 1–2 with something path-shaped beside it is still
// read as a comparison — RED. Compare-operators in Go always put a non-shell
// character between the name and the operator, and a command that does not name
// its files on its own line is not the byte comparator either, so the residue is
// small; the defaults stay on the gated side.
//
// WHY NOT GO-AST ANYTHING: this scanner is a pure TEXT walk by design (see the
// file header) and it reads markdown as well as Go, so a parser would cover only
// half its corpus. The three conditions above are syntax facts a text scan can
// decide, which is what makes the fix surgical rather than a second gate.
var redproofIdentityCmpRe = regexp.MustCompile(
	`(?:^|[` + regexp.QuoteMeta(redproofIdentityCmpLeadIn) + `])` + redproofIdentityCmpTok + `(?:[ \t]|$)`)

// redproofIdentityCmpLeadIn is the single home (Rule 14) for what may precede
// the byte comparator's token: shell lead-in only — whitespace, a quote, an
// opening paren/brace/bracket, a command separator, and the tail characters of
// a `$( )` substitution or a function-definition lead-in. It is deliberately NOT
// a superset of "anything non-alphanumeric": `.`, `-`, `_` and digits are absent
// on purpose, and their absence is what stops Go's `cmp.Compare`, `xcmp`,
// `TestX-cmp`, `p.cmp` and `cmp1` from being read as a command.
const redproofIdentityCmpLeadIn = " \t(/[{|\"'&;$@`"

// redproofIdentityCmpMinPaths is how many FILE-ARGUMENT tokens (a file-shaped
// token or a short placeholder — redproofIdentityCmpArg) a `cmp` LINE must carry
// — condition 3 above. Two, because the byte comparator takes two files; one
// would accept a line that merely names the program beside a single path.
const redproofIdentityCmpMinPaths = 2

// redproofIdentityCmpTailRe turns a `cmp` line into its argument tokens: it
// consumes the token itself and then every whitespace-separated field, so the
// caller counts what the command was given rather than what surrounds it.
var redproofIdentityCmpTailRe = regexp.MustCompile(
	`(?:^|[` + regexp.QuoteMeta(redproofIdentityCmpLeadIn) + `])` + redproofIdentityCmpTok +
		`(?:[ \t](.*))?$`)

// redproofIdentityCmpArgs is a `cmp` line's operand tokens, in order: everything
// after the token up to a `#` (a trailing comment is not an argument). ok is
// false when the token is not a command line at all.
func redproofIdentityCmpArgs(line string) ([]string, bool) {
	m := redproofIdentityCmpTailRe.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}
	return strings.Fields(strings.SplitN(m[1], "#", 2)[0]), true
}

// redproofIdentityCmpShape reports whether ONE LINE invokes the byte comparator
// with two files — conditions 1–3 above, in the order they are cheapest to
// decide. It is the ONLY way `cmp` reaches the arm.
func redproofIdentityCmpShape(line string) bool {
	if !redproofIdentityCmpRe.MatchString(line) {
		return false
	}
	args, ok := redproofIdentityCmpArgs(line)
	if !ok {
		return false
	}
	files, first := 0, true
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue // a flag (`-s`, `-l`, `--help`) is not an argument
		}
		if !redproofIdentityCmpArg(a) {
			if first {
				// The FIRST operand of a file comparison is a file. When it is
				// not, the token is a word in a sentence about the command, not
				// the command — the arm's own prose negative, in general form.
				return false
			}
			continue
		}
		first = false
		files++
	}
	return files >= redproofIdentityCmpMinPaths
}

// redproofIdentitySubstPrefixes are the bash PROCESS-SUBSTITUTION openers. A
// byte comparison may take one of its sides from a command's output that way
// (`<(...)`), which is a file as far as the comparator is concerned, so these are
// accepted as file-shaped arguments. Declared as data (Rule 1) rather than typed
// at each use.
var redproofIdentitySubstPrefixes = []string{"<(", ">("}

// redproofIdentityCmpPath reports whether one argument token is FILE-SHAPED: the
// shapes redproofFileNameRe enumerates, a file substitution, and — the reason
// this is its own predicate rather than a reuse of that regex — any token
// carrying a `.` or a `/`. A proof compares artifacts whose extensions nobody
// enumerated (`prod.go.saved`, `dump.bak`), and reading such a token as "not a
// path" would let every real byte comparison with an unusual suffix through.
func redproofIdentityCmpPath(tok string) bool {
	for _, prefix := range redproofIdentitySubstPrefixes {
		if strings.HasPrefix(tok, prefix) {
			return true
		}
	}
	if redproofFileNameRe.MatchString(tok) {
		return true
	}
	return strings.ContainsAny(tok, "./")
}

// redproofIdentityCmpPlaceholderMaxLen bounds a BARE placeholder argument — the
// one-letter operands of a real two-file byte comparison as a recipe spells it.
// Two characters, because a placeholder stands in for a path in a recipe while a
// prose word (`decides`, `compare`, `exit`) does not; measured against the arm's
// own prose negative, this is the width that separates "a command line" from "a
// sentence that mentions the command". The residue is stated rather than hidden:
// a sentence whose two short words follow the token (a two-letter preposition and
// a two-letter verb) would still read as a command line, which is the gated
// direction — one line of prose costs a reader an inline escape, one missed
// proof costs the second half of Rule 5.
const redproofIdentityCmpPlaceholderMaxLen = 2

// redproofIdentityCmpArg reports whether one token can be an argument of a FILE
// comparison: file-shaped, or a short bare placeholder.
//
// The second half is what keeps a REAL byte comparison of two placeholder paths
// gated while a sentence about the command is not, and its narrowness is
// deliberate: a placeholder is letters/digits only and at most two of them, so
// an operator (`:=`, `==`), a punctuated word (`p;`) or an English word of any
// length never qualifies. That is exactly the set of tokens Go puts after the
// identifier in `cmp := p` / `cmp == subject`, and the qualified-call spelling
// (`cmp.Compare`) is refused a step earlier by condition 2 — so conditions 1–3
// together reject the Go spellings without weakening the shell one.
func redproofIdentityCmpArg(tok string) bool {
	if redproofIdentityCmpPath(tok) {
		return true
	}
	if len(tok) == 0 || len(tok) > redproofIdentityCmpPlaceholderMaxLen {
		return false
	}
	for _, r := range tok {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// redproofIdentityCmdRePreSplit is the predicate as it stood BEFORE `cmp` was
// split out of it (2026-10-26), kept as a NAMED value rather than described in a
// comment so the regression test can run the pre-fix and post-fix predicates
// over the same fixtures, in the same process, at the same revision — the
// red-before/green-after pair is then MEASURED instead of asserted, and there is
// no mutated file to restore (Rule 5's timeliness trap is designed out).
//
// It is reached ONLY through redproofIdentityScanStrict == false, which no
// shipped path does: `go vet` cannot tell you that, the const can (Rule 15 —
// the old predicate stays visible rather than half-remembered).
var redproofIdentityCmdRePreSplit = regexp.MustCompile(
	`(?:^|[\s(` + redproofShellSeparators + `])(?:cmp|` +
		strings.Join(redproofIdentityDigestCmds, "|") + `)\b`)

// redproofIdentityScanStrict selects the SHIPPED predicate: strict = `cmp` must
// be a command line naming its files (redproofIdentityCmpShape), loose = the
// pre-split regex above. Its only false value is the regression test's red
// column; flipping it here turns
// TestGateRedProofRestoreIdentityCmpSplitIsTwoSided red, which is how the fix's
// necessity is measured rather than claimed.
const redproofIdentityScanStrict = true

// redproofShellSeparators is the character class of what may precede a command
// in shell text (a space, an opening paren/quote, a command separator). It is a
// CLASS, not a separator list, so it can only make the predicate match more —
// never suppress a command that follows an unseen separator.
const redproofShellSeparators = `()'"|;&`

// redproofIdentityEqRe matches an equality/inequality TEST operator: shell's
// `test`/`[` forms, `==` in both shell and Go, and the arithmetic `-eq`/`-ne`.
// `:=` is deliberately absent (a Go assignment is not a comparison) and the
// left boundary excludes `:`/`+`/`<`/`>` so `:=`, `==`, `+=` and a heredoc are
// classified by what they are.
var redproofIdentityEqRe = regexp.MustCompile(
	`(?:^|[\s(;"'\x60])(?:=|==|!=|-eq|-ne)(?:[\s)"'\x60;]|$)`)

// redproofIdentityValuedRe matches the recorded-verdict shape: a named key whose
// value IS the verdict (`same` `=` `YES`, `same` `=` `0f516f98…`). It exists
// because the measured defect was written in exactly that form — a prose report
// printed a hash, a saved copy's hash and a verdict, with no command and no
// operator — and a predicate that only recognised shell comparisons would have
// read it as silent.
//
// The operator is a SINGLE `=` on purpose: `==` is the equality operator of Go
// and of most prose code, where `match == ""` is a string test, not a verdict.
var redproofIdentityValuedRe = regexp.MustCompile(
	`\b(?:same|match|matches|identical|equal|equals|differ|differs)[ \t]*=[ \t]*[A-Za-z0-9_.\-]+`)

// redproofIdentityValued reports whether one line records a verdict as a
// key/value pair. A Go assignment whose value is a CALL — `matches =
// append(matches, n)`, `match = ftsMatchQuery(query)` — is not a verdict: a
// verdict's value is a bare word or a digest, so a `(` immediately after the
// value disqualifies it. (Go's RE2 has no lookahead, so the check is code.)
func redproofIdentityValued(line string) bool {
	m := redproofIdentityValuedRe.FindStringIndex(line)
	if m == nil {
		return false
	}
	return m[1] >= len(line) || line[m[1]] != '('
}

// redproofIdentityRevRe matches a REVISION ANCHOR that is spelled in words: a
// revision-reading git verb, a bare HEAD, a named branch, or a colon-qualified
// revision path whose right-hand side is a file-shaped token (`HEAD:file.go`,
// `0123456:internal/x.go`). The right-hand side must carry a `.` or `/` so that
// prose punctuation ("note: see") is not read as a revision.
var redproofIdentityRevRe = regexp.MustCompile(
	`\b(?:rev-parse|cat-file|ls-tree)\b|\bHEAD\b|\b(?:master|main|origin/[^\s]+)\b|` +
		`\b[0-9a-z][0-9a-z_/.\-]*:[^\s]*[./][^\s]*`)

// redproofIdentityHexRe matches a bare content digest: an abbreviated or full
// commit/blob id written on its own.
var redproofIdentityHexRe = regexp.MustCompile(`[0-9a-f]{7,40}`)

// redproofIdentityValueLeaders are the characters that make a following hex
// token a VALUE rather than a revision: `hash=<blob>`, `saved=<blob>`, and the
// `<rev>:<path>` qualifier. Without this carve-out the measured defect's own
// line would read as anchored — it carries two blobs, both on the right of an
// `=` — and the arm would be silent on the one shape it was built for.
const redproofIdentityValueLeaders = "=:"

// redproofIdentityHasRevAnchor reports whether a unit names a revision anywhere:
// a revision word, a colon-qualified revision path, or a bare digest that is not
// the value of a `key=` pair.
func redproofIdentityHasRevAnchor(text string) bool {
	if redproofIdentityRevRe.MatchString(text) {
		return true
	}
	for _, m := range redproofIdentityHexRe.FindAllStringIndex(text, -1) {
		if m[0] == 0 || !strings.ContainsRune(redproofIdentityValueLeaders, rune(text[m[0]-1])) {
			return true
		}
	}
	return false
}

// redproofIdentityMinDigests is how many content digests a unit must carry for
// the RECORDED-PAIR shape to be a reading at all: the measured defect compared a
// digest with a saved copy's digest, and one digest on its own proves nothing
// about identity. It is what keeps a program's own field (`match=false` in the
// ui_state compare envelope, `identical = 0` in a Go test) out of this arm.
const redproofIdentityMinDigests = 2

// redproofIdentityFind is arm (c): one hit per unit that compares content
// identity without naming a revision anywhere in that unit.
func redproofIdentityFind(g gate, p *parsed) []hit {
	return redproofIdentityFindWith(g, p, redproofIdentityScanStrict)
}

// redproofIdentityFindWith is redproofIdentityFind under an explicit predicate —
// the shipped one, or the pre-split one the regression test's red column runs.
func redproofIdentityFindWith(g gate, p *parsed, strict bool) []hit {
	var out []hit
	lines := strings.Split(string(p.src), "\n")
	units := redproofUnits(lines)
	for i, u := range units {
		// Same escape reach as arm (a): the unit itself, or the unit right after
		// it — a fenced block cannot carry a trailing comment on its last line.
		if strings.Contains(u.text, g.escape) || strings.Contains(u.text, redproofIdentityEscape) {
			continue
		}
		if i+1 < len(units) &&
			(strings.Contains(units[i+1].text, g.escape) ||
				strings.Contains(units[i+1].text, redproofIdentityEscape)) {
			continue
		}
		if !redproofIdentityUnanchored(u.text, strict) {
			continue
		}
		line := u.startLine + redproofIdentityLine(u.text)
		out = append(out, hit{rel: p.rel, line: line, desc: redproofIdentityDesc})
	}
	return out
}

// redproofIdentityLine returns the 0-based offset, inside a unit's text, of the
// line the identity reading sits on — the line a reader has to change.
func redproofIdentityLine(text string) int {
	for i, line := range strings.Split(text, "\n") {
		if redproofIdentityValued(line) {
			return i
		}
		if redproofIdentityShape(line) {
			return i
		}
	}
	return 0
}

// redproofIdentityShape reports whether one LINE carries a content-identity
// comparison: a digest command or `cmp` together with an equality operator, or a
// recorded verdict spelled as a key/value pair.
func redproofIdentityShape(line string) bool {
	if redproofIdentityValued(line) {
		return true
	}
	return redproofIdentityLineCompares(line, redproofIdentityScanStrict)
}

// redproofIdentityLineCompares reports whether ONE line carries the command half
// of a content-identity comparison, under the selected predicate (strict = the
// shipped one). It is the single place the strict/loose choice is made, so the
// two halves of the regression test cannot drift from production.
func redproofIdentityLineCompares(line string, strict bool) bool {
	if !strict {
		// The pre-split predicate ALSO required an equality operator on the same
		// line — a second defect this change closes, measured 2026-10-26: a bare
		// byte comparison of two files was NOT reported, because the byte
		// comparator's verdict IS its exit status and there is no operator to
		// find. Only the digest commands need one (they PRINT a value).
		return redproofIdentityCmdRePreSplit.MatchString(line) && redproofIdentityEqRe.MatchString(line)
	}
	if redproofIdentityCmpShape(line) {
		return true
	}
	return redproofIdentityCmdRe.MatchString(line) && redproofIdentityEqRe.MatchString(line)
}

// redproofIdentityVerdicts is arm (c)'s positive half, as explicit predicates
// rather than one boolean: the two shapes carry different evidence requirements
// (see redproofIdentityMinDigests), so the reader can see both.
//
//   - compared: ONE LINE carries a DIGEST command together with an
//     equality/inequality operator — a digest command PRINTS a value, so a
//     comparison of that value is what makes the reading. The byte comparator is
//     the exception, and the exception IS the fact: its exit STATUS is the
//     verdict, so a byte comparison naming its two files already is a
//     content-identity reading and needs no operator. The scope is the LINE, not
//     the unit: a unit that runs a digest command and separately asks `if err !=
//     nil` is not comparing digests, and reading the two facts out of different
//     lines is how a gate starts reporting every test that hashes anything
//     (measured while writing this arm).
//   - recorded: a line that spells a verdict as a key/value pair (`same=YES`)
//     in a unit that carries at least two content digests — a report comparing a
//     digest against a saved copy's digest, which is the measured defect.
func redproofIdentityVerdicts(text string, strict bool) (compared, recorded bool) {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if redproofIdentityLineCompares(line, strict) {
			compared = true
			break
		}
	}
	if len(redproofIdentityHexRe.FindAllStringIndex(text, -1)) < redproofIdentityMinDigests {
		return compared, false
	}
	for _, line := range lines {
		if redproofIdentityValued(line) {
			recorded = true
			break
		}
	}
	return compared, recorded
}

// redproofIdentityUnanchored reports whether a unit performs a content-identity
// reading while naming no revision: the fragile shape this arm exists for.
func redproofIdentityUnanchored(text string, strict bool) bool {
	if redproofIdentityHasRevAnchor(text) {
		return false
	}
	compared, recorded := redproofIdentityVerdicts(text, strict)
	return compared || recorded
}

// redproofIdentityDesc is arm (c)'s one-line message. It names the defect's
// mechanism (what a copy-anchored reading cannot see), the correction (anchor a
// side to a revision), and the timeliness requirement Rule 5 now states: a
// reading taken before a later write must be RE-TAKEN, not quoted.
const redproofIdentityDesc = "content-identity comparison is anchored to NO revision — neither side is a revision, so a write made after the reading leaves it saying the same thing (the reading cannot invalidate itself; measured 2026-10-14: `hash=<blob> saved=<blob> same=YES`, both sides artifacts of the same loop, while the delivered blob differed). Anchor one side to a revision — `git rev-parse HEAD:<path>`, `git show <rev>:<path>`, `git cat-file -p <rev>:<path>` — or compare `git hash-object <path>` against `git rev-parse HEAD:<path>`; and if the file was written after the reading, RE-TAKE it"

// redproofIdentityHit is arm (c)'s predicate with the two arms merged: a unit
// that trips EITHER arm is reported, so one scan covers both halves of Rule 5.
func redproofIdentityHit(g gate, p *parsed) []hit {
	out := redproofRestoreFind(g, p)
	out = append(out, redproofIdentityFind(g, p)...)
	return out
}

// redproofRestoreBaseline is empty BY CONSTRUCTION (see the gate's
// baselineReason). It is declared as a value rather than inlined so a future
// permit lands in one place with its reason.
var redproofRestoreBaseline []string

// redproofRestoreCorpusFilter narrows the whole-tree walk to the instruction
// surfaces this gate owns. It is a filter over the SAME walker the other text
// gate uses (`textFilesUnder`), so the two gates share one traversal and one
// binary-file rule; what differs is only which files each one reads.
//
// The devdoc/tasks exclusion is expressed by NOT naming those trees among the
// roots, and by the named-file list, rather than by a suffix/substring rule: a
// path-based exclusion is the shape that silently swallows a new tree.
func redproofRestoreCorpusFilter(rel string) bool {
	if !strings.HasPrefix(rel, "internal/") && !strings.HasPrefix(rel, "cmd/") {
		for _, named := range redproofRestoreNamedFiles {
			if rel == named {
				return true
			}
		}
		return false
	}
	for _, suf := range redproofRestoreTextSuffixes {
		if strings.HasSuffix(rel, suf) {
			return true
		}
	}
	return false
}
