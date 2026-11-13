package standardgate

// Worktree-root literal gate (PROJECT.md Rule 13, complementing Rules 1a/1b, 14
// and 22) — the ratchet that stops the /dev/shm exposure from coming back.
//
// WHY THIS GATE EXISTS. The 2026-09-13 assessment measured 19 linked worktrees
// (1 015 MB) inside a tmpfs whose 50%-of-RAM cap "nobody chose", while NO code
// in the repository could answer where an agent is allowed to create a worktree:
// the answer was a sentence in GOPI.md and a per-site string literal. Fixing
// that with a declared root (worktreeroots) and a path policy (internal/tools)
// is only half the work — the other half is making a re-introduced hardcoded
// root impossible to land. A constant that is defined and then bypassed by a
// bare "/dev/shm/somewhere" literal is exactly Rule 13's defect: the fix lands
// at the definition site and the bypassed sites keep the old value.
//
// WHAT IT CHECKS (deterministic, AST-based, no judgement call — the bar Rules
// 16–21 set). TWO TOPOLOGIES, TWO RULES — this is the rule's real boundary,
// stated once here so any site can be classified without reading the predicate:
//
//  1. PRODUCTION (every non-test .go under internal/ + cmd/) — the rule is
//     REFERENCE, NOT LITERAL. A path-valued string literal that hardcodes a
//     worktree root is reported: the standard shared-memory filesystem
//     "/dev/shm" (the BARE directory as well as anything below it), the ambient
//     temp root "/tmp" (likewise), and the home-rooted worktree paths
//     ("~/workspace/…", "/home/<user>/workspace/…", "$HOME/workspace/…"). The
//     literal is allowed in EXACTLY ONE place: internal/worktreeroots, the
//     package that DEFINES the constants. Every other production file must
//     reference worktreeroots.ShmDir / .AmbientTempRoot / .FixtureBannedRoot /
//     .WorkspaceDirName, or carry the documented inline escape with its reason.
//
//     THE ONE REVIEWED EXEMPTION: the literal may stand where it IS THE
//     SUBJECT — where a site's whole point is the standard location itself (the
//     X11 socket directory the protocol mandates: a headless capture cannot
//     move it, and the path a reviewer must see at a glance is that literal).
//     Such a site is not "a worktree root somebody typed"; it is the fact under
//     audit. It is marked `// worktree-root-ok: <reason>`, which keeps it in
//     the data report instead of hiding it in a baseline.
//
//  2. FIXTURES (every _test.go under internal/ + cmd/ + the top-level e2e
//     suite) — the rule is NOT "reference the constant". A fixture's point is
//     very often that a path lies OUTSIDE the declared roots: the refusal under
//     test is exactly that case, and a path derived from worktreeroots.ShmDir
//     would be a path the policy UNDER the test may authorise — the fixture's
//     meaning would INVERT. Nor can a test that must WRITE a spelling into a
//     child process's environment hold a private copy of the value. So a
//     fixture is held to the provable hazard only: it may not reach for the
//     BARE HOST ROOT — "/dev/shm" or "/tmp", the root itself, the spelling that
//     means "wherever temp is" — unless
//
//       (a) it is the DECLARED VALUE of a constant or variable in that same
//           file — a NAMED FIXTURE (Rule 1b: the point is the name, not the
//           spelling). The classification is BY POSITION — by whether some
//           const/var in the file declares the literal — never by trying to
//           recognise the identifier a reader sees: a reference to
//           worktreeroots.ShmDir is a different token and is allowed for a
//           different reason (there is no literal to report at all); or
//       (b) it carries `// fixture-path-ok: <reason>` — a fixture that must
//           keep the host literal because the fixture's POINT is the real
//           filesystem (a probe that must land on the ban's own target even
//           while the suite pins TMPDIR elsewhere, a host measurement), where
//           deriving the path would make the probe follow the very
//           configuration it exists to test.
//
//     WHY THE FIXTURE ANCHOR IS STRICTER THAN THE PRODUCTION ONE. Production
//     wants the whole path, because a production literal is a LOCATION a
//     program acts on and the chosen name below the root is what drifts. In a
//     fixture the string below the root is the FINGERPRINT — the
//     distinguishing leaf a test reads back ("/tmp/x/wt" is a worktree,
//     "/tmp/serve.lock" is a lock file) — and that spelling is the fixture's
//     own data, not a root choice. A path BELOW a root is therefore a
//     fingerprint in a fixture and a drift risk in production; the same string
//     is held to the two rules for that reason.
//
// WHY THE BARE "/dev/shm" COUNTS — the shape this gate used to miss. The
// original pattern was anchored to "/dev/shm/<name>", on the theory that the
// bare directory is a system fact while a path below it is a chosen location.
// That held for the definition site and failed everywhere else: the 22
// /dev/shm code sites in this tree included fixture paths naming the directory
// ITSELF (the candidate a memory-filesystem measurement sweeps, the root label
// a sweep finding renders) and a production value that IS the definition — the
// anchor left every one of them unreported. The bare literal is now reported in
// production and in fixtures alike; the definition package is exempt because it
// IS the definition (an exemption by DIRECTORY, not by shape), not because a
// bare literal is harmless.
//
// The /dev/shm pattern is now the RETIRED-CREATE guard: the operator's
// 2026-09-18 ruling ("don't use tmpfs. change settings to workspace. now")
// retired the memory-backed scratch home, so paths below the standard
// shared-memory filesystem may be created only by the RECOGNISE-AND-CLEAN
// reader that names them through worktreeroots.ShmDir. See
// worktreeRootRetiredClause for the wording the red-proof pins.
//
// WHAT IT DELIBERATELY DOES NOT CHECK (so a future agent does not "close the
// gap" by widening it): the gate is scoped to HOME-ROOTED and STANDARD-ROOT
// literals, the shapes that were measured to place worktrees in the wrong
// filesystem. It does not police:
//
//   - a path that merely MENTIONS a root in prose (a doc comment, an error
//     message, a report path). The walk is over string literals — what a
//     program can ACT on — never over source text; a comment is not a worktree
//     root, and a text scan would punish the explanation. (The 91 /dev/shm
//     comment lines in this tree are out of scope by that rule, and are
//     deliberately not rewritten.)
//   - a fixture-local RELATIVE leaf ("gopi-adhoc", "wt", "probe-"), and a
//     fingerprint path below a root in a fixture: see the boundary above.
//   - a path DERIVED from an ambient root at run time (filepath.Join(os.TempDir(),
//     "/x"), os.MkdirTemp(os.TempDir(), "probe-")). A first draft of this
//     extension reported them and measured 29 hits, every one a false positive
//     on a fixture that legitimately wants the CONFIGURED temp root — the
//     distinction between a name for a path and a second way of writing one is
//     not decidable here, and a noisy gate gets switched off.
//   - a literal in an EVENT or a message that must spell a spelling out for a
//     child process or an operator.
//   - the C library's tmpfile namespace or a fixture's own t.TempDir() — those
//     are not roots an agent chooses.
//
// The alias rule from Rule 14 is encoded too: a SECOND definition of the
// workspace directory name (internal/tools' taskWorkspaceDirName used to be a
// bare "workspace") is reported unless it is written as an alias of the one
// constant.

import (
	"fmt"
	"go/ast"
	"strconv"
	"strings"

	"go/token"
	"regexp"
)

const (
	// worktreeRootEscape marks a PRODUCTION site that deliberately keeps a root
	// literal because the literal IS the subject (the X11 socket directory the
	// protocol mandates). It is the Rules 16–21 inline-escape pattern: a
	// reviewed, visible exception rather than a baseline entry.
	worktreeRootEscape = "// worktree-root-ok:"

	// worktreeFixtureEscape marks a FIXTURE site that must keep a host-root
	// literal because deriving it would invert the test (the probe that must
	// land on the ban's own target, the host filesystem under measurement).
	// It is the fixture-topology twin of worktreeRootEscape, and it exists so
	// the two reasons cannot be confused: one says "this literal is the
	// subject", the other says "this path must stay where the host put it".
	worktreeFixtureEscape = "// fixture-path-ok:"

	// worktreeRootDefinitionDir is the ONE directory allowed to hold the
	// literal — in production AND in its own tests, because
	// internal/worktreeroots/roots_test.go pins ShmDir's value by spelling it.
	// Every other production file must reference it (Rule 14's single source).
	worktreeRootDefinitionDir = "internal/worktreeroots/"

	// worktreeRootRetiredClause is why the /dev/shm half of this gate is now
	// guarding a RETIRED location — and why it is not a no-op merely because
	// nothing creates there any more.
	//
	// The operator's 2026-09-18 ruling ("just for a few ms performance? no, no
	// no! don't use tmpfs. change settings to workspace. now") retired the
	// memory-backed scratch home: the scratch home of record is a real
	// directory on the block device. So the gate's contract changed meaning
	// without changing a line of its predicate, and the meaning is the part a
	// reader must not have to reconstruct:
	//
	//   - CREATE under the standard shared-memory filesystem is now permanently
	//     out of policy (it was already forbidden as a WORKTREE root; it is now
	//     forbidden as a scratch location too), and
	//   - RECOGNISE AND CLEAN a legacy artifact there is still required — the
	//     measured 2.6 GB / 702 entries must not be orphaned — so the value and
	//     the constant that names it SURVIVE, and a path below the base is legal
	//     only when it is BUILT from worktreeroots.ShmDir (a legacy/recognition
	//     spelling) rather than typed.
	//
	// The red-proof pins this clause in the reported reason, so the gate cannot
	// be silently re-pointed at something else while keeping its id.
	worktreeRootRetiredClause = "RETIRED 2026-09-18 as a CREATE location (the scratch home is a real directory on the block device); " +
		"the value survives only to RECOGNISE and CLEAN legacy artifacts"

	// testFileSuffix selects the fixture topology. It is named here because the
	// gate dispatches on it (Rule 1b: a suffix that decides a rule is not a
	// bare literal), and its value is the toolchain's own convention.
	testFileSuffix = "_test.go"
)

// worktreeRootLiteralPatterns are the PRODUCTION hardcoded-root shapes, each
// with the reference that must be used instead. They are anchored REGEXES over
// the literal's VALUE (the unquoted string), never over source text, so a
// comment that mentions /dev/shm is not a hit — only a string a program can act
// on.
var worktreeRootLiteralPatterns = []struct {
	why string
	re  *regexp.Regexp
}{
	{
		why: "hardcodes the standard shared-memory filesystem — " + worktreeRootRetiredClause +
			" — reference worktreeroots.ShmDir (the ONLY definition) instead",
		// The bare directory AND every path below it. The original pattern
		// required an entry BELOW it, which is exactly how a bare "/dev/shm"
		// went unreported (see the boundary note above).
		re: regexp.MustCompile(`^/dev/shm(/|$)`),
	},
	{
		why: "hardcodes a worktree/fixture path under the ambient temp root — reference worktreeroots.AmbientTempRoot / worktreeroots.FixtureBannedRoot (or build the path from the scratch tree) instead: a temp workspace must never be chosen by a bare literal (Rule 22)",
		// Same anchoring as /dev/shm: the bare directory and everything below it.
		re: regexp.MustCompile(`^/tmp(/|$)`),
	},
	{
		why: "hardcodes a home-rooted workspace WORKTREE path — build it from worktreeroots.DefaultRoot()/<home>, never as a literal (a literal ignores the declared per-repo root and the sandbox override)",
		re:  homeRootedWorkspaceRe,
	},
	{
		why: "hardcodes an absolute home workspace WORKTREE path — build it from worktreeroots.DefaultRoot()/<home> instead",
		re:  absoluteHomeWorkspaceRe,
	},
}

// worktreeFixtureSeedRe is the FIXTURE topology's seed: the BARE host root a
// fixture may name only as a declared fixture value or behind the fixture
// escape. The values are the same two the production patterns use — a second
// definition of "the roots" would be the drift Rule 14 bans — and the anchor is
// deliberately stricter (no entry below the root): see the boundary note at the
// top of this file for why a path below a root is a fingerprint in a fixture.
var worktreeFixtureSeedRe = regexp.MustCompile(`^(/dev/shm|/tmp)$`)

// workspaceRootLeaf is the workspace directory name the home-rooted patterns
// look for. It is a LOCAL definition rather than an import of
// internal/worktreeroots on purpose: the gate must be able to report a defect
// in that package's consumers without depending on the package compiling (and
// its value is pinned by the gate's own red-proof).
const workspaceRootLeaf = "workspace"

// homeRootedWorkspaceRe / absoluteHomeWorkspaceRe are the two home-rooted
// shapes: the tilde/$HOME spelling the protocol text uses, and the expanded
// absolute spelling a shell or a resolver produces.
var (
	homeRootedWorkspaceRe   = regexp.MustCompile(`^(~|\$HOME|\$\{HOME\})/` + workspaceRootLeaf + `(/|$)`)
	absoluteHomeWorkspaceRe = regexp.MustCompile(`^/(home|Users)/[^/]+/` + workspaceRootLeaf + `(/|$)`)
)

// worktreeRootFind reports every hardcoded root literal in one PRODUCTION file.
//
// The walk is over BASIC LITERALS only: a raw string in a doc comment or a
// message is still a literal, so the escape marker (or a reference) is what
// distinguishes an actioned path from prose — the failure text says exactly
// that.
func worktreeRootFind(g gate, p *parsed) []hit {
	if strings.HasPrefix(p.rel, worktreeRootDefinitionDir) {
		return nil // the definition site owns the values (Rule 14)
	}
	return worktreeLiteralHits(p, false)
}

// worktreeFixtureFind reports the FIXTURE-topology hits in one _test.go file: a
// BARE host root inlined without being a declared value of that file and
// without the fixture escape. See the boundary note at the top of this file for
// why the fixture rule is not "reference the constant".
func worktreeFixtureFind(g gate, p *parsed) []hit {
	// The definition package's own tests are the half of the definition that
	// MUST spell the values (roots_test.go pins ShmDir == "/dev/shm" by value).
	// Exempting the whole directory — production and tests — keeps the
	// exemption at ONE rule instead of two.
	if strings.HasPrefix(p.rel, worktreeRootDefinitionDir) {
		return nil
	}
	return worktreeLiteralHits(p, true)
}

// worktreeLiteralHits is the shared AST walk. fixture selects the topology:
// false = the production rule (any hardcoded-root shape), true = the fixture
// rule (the bare root, with the declared-value exemption).
func worktreeLiteralHits(p *parsed, fixture bool) []hit {
	var hits []hit
	ast.Inspect(p.file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind.String() != "STRING" {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true // a malformed literal is the compiler's business
		}
		pos := p.fset.Position(lit.Pos())
		if fixture {
			if !worktreeFixtureSeedRe.MatchString(value) {
				return true // a fingerprint leaf or an unrelated path: not the root itself
			}
			if worktreeFixtureDeclaredValue(p.file, lit) {
				return true // a NAMED FIXTURE: the value has one declared home in this file
			}
			hits = append(hits, hit{rel: p.rel, line: pos.Line,
				desc: fmt.Sprintf("%q: reaches for the BARE HOST ROOT inside a fixture — name it as a fixture constant in this file, or reference worktreeroots.ShmDir / .AmbientTempRoot / .FixtureBannedRoot where deriving it would not invert the test (a fixture that must keep the host literal marks itself %s <why>)", value, worktreeFixtureEscape)})
			return true
		}
		for _, pattern := range worktreeRootLiteralPatterns {
			if !pattern.re.MatchString(value) {
				continue
			}
			hits = append(hits, hit{rel: p.rel, line: pos.Line, desc: fmt.Sprintf("%q: %s", value, pattern.why)})
			break
		}
		return true
	})
	return hits
}

// worktreeFixtureDeclaredValue reports whether lit is the declared VALUE of a
// constant or variable in the same file. That is the named-fixture shape: the
// spelling has ONE home and a NAME, which is the property Rule 1b is about. The
// classification is BY POSITION — never by trying to recognise the identifier a
// reader sees, because a reference and an alias are different tokens and an
// inlined literal is what the rule is about.
//
// Only const/var DECLARATIONS count, not assignments: a value written back into
// a var at run time (`d = "/tmp/probe-" + pid`) is a path a program chose
// inline, and it stays reported.
func worktreeFixtureDeclaredValue(file *ast.File, lit *ast.BasicLit) bool {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, v := range vs.Values {
				if v.Pos() <= lit.Pos() && lit.End() <= v.End() {
					return true
				}
			}
		}
	}
	return false
}

// worktreeRootGate describes the gate for the registry. It is the ONE gate for
// this rule (a second one would be the duplicate definition Rule 14 bans): the
// topography is an OPTION on the gate, not a separate rule set.
func worktreeRootGate() gate {
	return gate{
		id:             "Rule 13 (no hardcoded worktree root literal)",
		checklistRow:   "1b/worktree-root",
		probe:          "TestGate7WorktreeRootLiteralRedProof",
		escape:         worktreeRootEscape,
		escapeTest:     worktreeFixtureEscape,
		scanTests:      true,
		baselineReason: "EMPTY by construction, in both topologies: the 22 /dev/shm code sites were classified and each surviving literal is either the reviewed subject-literal (carrying its escape inline) or a declared fixture value (which is not a hit at all). A re-introduced host path fails on the commit that introduces it — in production and in a fixture alike.",
		baseline:       worktreeRootBaseline,
		find: func(g gate, p *parsed) []hit {
			if strings.HasSuffix(p.rel, testFileSuffix) {
				return worktreeFixtureFind(g, p)
			}
			return worktreeRootFind(g, p)
		},
	}
}

// worktreeRootBaseline is empty BY CONSTRUCTION: the conversion of the measured
// files (internal/testutil ×2, internal/tools ×1, plus the new definition
// package) is what made the gate green the day it landed, and a new violation
// must be FIXED, not baselined. It stays empty after the extension to the
// fixture topology: every host-path literal that survives does so as a DECLARED
// fixture value or behind a marker whose reason is in the code.
var worktreeRootBaseline []string
