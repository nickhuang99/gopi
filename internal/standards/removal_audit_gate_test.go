// removal_audit_gate_test.go — the STRUCTURAL gate for the removal audit record
// (epic: two live-worktree-removal incidents, 2026-09-17 / 2026-09-18, where no
// component log named the removing arm).
//
// THE DEFECT THIS GATE FREEZES: a removal path that runs `git worktree remove`
// or `git branch -d/-D` without first emitting the audit record makes the
// destruction unattributable again — and the next author to add a removal site
// has no reason to remember an emitter exists. The gate is exhaustive by
// CONSTRUCTION: it does not read a hand-written list of arms (a list is the hole
// — measured 2026-09-16 in the cwd-release census, three arms were being called
// while listed nowhere). It walks every production file's AST, resolves the
// string arguments of EVERY call expression through the tree's own const
// declarations, and treats any call carrying the verb pair
//
//	worktree + remove          → a WORKTREE-REMOVAL site
//	branch + (-d|-D|-r)        → a BRANCH-DELETE site
//
// as a site that owes a record. The site is SATISFIED only when its enclosing
// function contains a matching removalaudit emit BEFORE it (the record must
// precede the mutation — a record written after it explains nothing about a
// crash inside it).
//
// THE VOCABULARY IS DERIVED, NOT WRITTEN HERE. "A matching emit" is not one
// hard-coded function name: the gate reads `internal/removalaudit`'s own
// implementation and classifies every exported entry point by the Record.Kind it
// writes (following the package-internal delegation hop to a fixpoint), so a site
// is satisfied by ANY entry point that writes that kind. The reason is measured:
// the gate used to name `BranchDelete` alone, and when commit 9b88c4b5b moved two
// live sites onto its same-kind sibling `BranchDeleteAt` the records kept being
// written while the gate reported both sites UNRECORDED — a pre-existing red that
// said "no record" where there was one. A rename or a new same-kind sibling must
// not be able to do that again; TestRemovalAuditGateDerivesTheEmitterVocabulary
// pins both directions (a renamed sibling stays green; a name that only READS
// records back is not an emitter).
//
// TWO RELEASE VALVES, both reviewed and both VISIBLE:
//
//   - an inline escape comment on the site (or in the comment block above it)
//     saying WHY this call carries no record of its own — the shared low-level
//     runner whose arms each emit, and nothing else today;
//   - `removalAuditSitesNoRecord`, an explicit list of `file:line` sites with a
//     reason, for a site the AST predicate cannot see the emit of.
//
// Neither valve is a suppression: the escape keeps the site in the data report
// (`TestRemovalAuditGateDataReport`), and the list is asserted to be empty by
// construction unless a reason is written down, so freeing a site is a
// deliberate, reviewable edit — never a silent permission.

package standardgate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/removalaudit"
)

const (
	// The verb values the predicate matches (git's own vocabulary — Rule 1b's
	// "bare string keywords" concern applies to OUR constants; these are the
	// external tool's spellings, matched as data).
	removalAuditVerbWorktree = "worktree"
	removalAuditVerbRemove   = "remove"
	removalAuditVerbBranch   = "branch"
	removalAuditFlagDelete   = "-d"
	removalAuditFlagForceDel = "-D"
	removalAuditFlagRemote   = "-r"

	// removalAuditConfigDirFlag is git's configuration-directory flag, the one
	// argument a `-C`-configured call carries before its subcommand. The census
	// reads it to tell a caller's `-C <dir>` prefix (a real invocation through a
	// runner) from a call whose argument LIST is the whole git vector (a
	// forwarding hook) — see removalAuditIsForwardingCall.
	removalAuditConfigDirFlag = "-C"

	// removalAuditEmitGapBudget is how many lines an emit may sit away from the
	// removal it covers when the emit is NOT in the site's own block — the
	// adjacency half of the pairing rule (see removalAuditPerSitePairing).
	// MEASURED, not chosen (2026-10-12): every emitting site on the real tree
	// has its record within a handful of lines (the epic's contract is "emit
	// BEFORE the mutation", and a record written far above explains little). A
	// site whose record drifted further is a site whose coverage should be
	// re-read by a human, which is exactly what going RED asks for.
	removalAuditEmitGapBudget = 12

	// The package the record comes from. It is the ONE spelling the walk looks
	// for, and it is the only emitter name this file states as a literal: WHICH
	// of that package's exported entry points writes which kind is DERIVED from
	// the package's own implementation by removalAuditDeriveVocabulary, so a
	// renamed or ADDED sibling entry point that writes the same Record.Kind
	// cannot drift the gate red (measured: commit 9b88c4b5b switched two sites
	// from BranchDelete to its same-kind sibling BranchDeleteAt, and a gate that
	// had hard-coded the name reported them UNRECORDED while the records were
	// being written).
	removalAuditPkg = "removalaudit"

	// removalAuditPkgSep and removalAuditCallOpen are the two fixed characters
	// every declared CALL SHAPE carries, and they are the whole reason a shape
	// is a shape: the pair is built by removalAuditCallShape, so a shape reads
	// `removalaudit.<EntryPoint>(` — package, selector and the opening paren.
	// The TRAILING removalAuditCallOpen is load-bearing: a bare identifier is
	// what a COMMENT, a doc note or an error message spells, and none of them
	// is a call, so a site whose only mention of the emitter is prose stays
	// unrecorded (pinned by the `recorded-by-a-comment` probe below).
	removalAuditPkgSep   = "."
	removalAuditCallOpen = "("

	// removalAuditEmitWorktree / removalAuditEmitBranch are the PRE-FIX
	// matcher's one spelling per kind — the gate used to hold exactly
	// `removalaudit.WorktreeRemove(` and `removalaudit.BranchDelete(` and
	// nothing else, which is how a same-kind sibling (BranchDeleteAt,
	// 9b88c4b5b) read two live, emitting sites as UNRECORDED.
	//
	// They are MEASUREMENT INSTRUMENTS, never consulted by the gate: the gate's
	// own vocabulary is DERIVED from the emitter's implementation
	// (removalAuditDeriveVocabulary), and these two literals exist so
	// removalAuditLegacyEmitShapes can reconstruct the pre-fix surface and
	// TestRemovalAuditGateShapeListIsLoadBearing can REPRODUCE the resident red
	// instead of asserting it. They are the only emitter-name literals outside
	// the sibling probes, and they are named (Rule 1) rather than typed.
	removalAuditEmitWorktree = "WorktreeRemove"
	removalAuditEmitBranch   = "BranchDelete"

	// removalAuditEmitBranchAt is the SUFFIX that makes the emitter's superset
	// entry point: removalAuditEmitBranch+removalAuditEmitBranchAt spells
	// BranchDeleteAt, the shape that carries the deleted tip and the delivery
	// point and DELEGATES to the base one. It was added by 9b88c4b5b (the
	// close-out chain captured its tip before the delete), and it is the exact
	// name the pre-fix matcher did not know — the resident red this union fixes.
	// It is built as a suffix rather than spelled whole (Rule 14: one definition
	// for the shared part, and the pair is asserted together below).
	removalAuditEmitBranchAt = "At"

	// The Record field the classification reads, and the one place a kind is
	// DECLARED — the Record's TypeSpec in the emitter's own package file. Both
	// are Go spellings of a single fixed schema (Rule 1b): the field name is
	// part of the wire format, and the kind only counts as declared where the
	// compiler reads it.
	removalAuditKindField     = "Kind"
	removalAuditRecordType    = "Record"
	removalAuditKindTypeIdent = "Kind"

	// removalAuditArmPrefix is the arm constants' name prefix (Rule 13).
	removalAuditArmPrefix = "Arm"

	// removalAuditSitesFloor is the NON-VACUITY floor: the scan must find at
	// least this many production removal sites, or the gate is reading a tree
	// it does not understand (a changed verb spelling, a broken glob) and would
	// pass green-blind. It is a MEASURED floor — see
	// TestRemovalAuditGateDataReport for the current count — and it ratchets up,
	// never down, as sites are added.
	removalAuditSitesFloor = 15

	// removalAuditArmsFloor is the companion floor for the reverse direction
	// (every declared arm is referenced by a production emit).
	removalAuditArmsFloor = 15

	// removalAuditPkgFile is the emitter's declaration file — the ONE file the
	// walk skips (it declares the record surface; it never removes anything).
	removalAuditPkgFile = "internal/removalaudit/audit.go"
)

// removalAuditSitesNoRecord lists `file:line` removal sites that intentionally
// carry no record of their own, with the reason. EMPTY today on purpose: every
// production removal site emits. An entry here is a reviewed exemption that the
// data report prints, never a suppression.
var removalAuditSitesNoRecord = map[string]string{}

// removalAuditSequelVerbs / removalAuditSequelDeleteFlag are the SECOND deletion
// vocabulary — the one the census could not see until 2026-10-12.
//
// THE DEFECT THEY CLOSE: the classification below knew exactly one way to delete
// a branch, git's porcelain pair `branch` + `-d|-D|-r`. A removal written as
// `update-ref -d <full-ref>` is the same deletion through the plumbing, and the
// census classified NOTHING — the site did not read as unrecorded, it read as
// nonexistent, so the gate that "makes every removal attributable" was silent
// about the one arm that ran it. Measured on this host: 11 refs/heads (16 → 5)
// disappeared at 09:45–09:46 CST 2026-10-12 with no `kind=branch-delete` record
// for any of them, and `gopi task archive --reclaim` — the arm that runs that
// verb (internal/tools/archive_ref_reclaim.go) — removed its ref in a fixture
// and wrote nothing to the sink.
//
// The values are the emitter's OWN constants (Rule 14: one definition), so the
// spelling the arm runs and the spelling the census matches cannot drift apart.
var (
	removalAuditSequelVerbs      = []string{removalaudit.RefDeleteVerb}
	removalAuditSequelDeleteFlag = removalaudit.RefDeleteFlag
)

// removalAuditSite is one call site that removes a worktree or deletes a branch.
type removalAuditSite struct {
	rel  string
	line int
	kind string // removalAuditKindWorktree | removalAuditKindBranch
	desc string
}

// The two kinds of site the predicate classifies.
const (
	removalAuditKindWorktree = "worktree-removal"
	removalAuditKindBranch   = "branch-delete"
)

// removalAuditVocabulary is the DERIVED emitter vocabulary: which exported
// entry point of the emitter writes which Record.Kind, plus the entry points
// that write no kind at all.
//
// It is not read from a list anybody maintains — a second list beside the
// implementation is the defect (Rule 13/14: a name that must be written in two
// places drifts in one of them). It is read from the implementation, by
// removalAuditDeriveVocabulary, so the implementation is the single source of
// truth the gate follows.
type removalAuditVocabulary struct {
	// byKind maps a Record.Kind to every entry point that writes it. A site of
	// that kind is satisfied by ANY of them: the sibling functions of one kind
	// are the same record, and the caller picks the shape it can fill.
	byKind map[string][]string
	// opaque is the PERMITTED-BUT-NOT-AN-EMITTER class: an exported entry point
	// of the emitter that writes no Record.Kind this package can read — today
	// the sink-path resolver, the record READER and the test hook. It is tracked
	// so the derivation can SAY what it could not classify, and so a probe can
	// assert that reading records back is not accepted as emitting one (a
	// vocabulary widened to "any exported name" would let a site whose only
	// removalaudit call is a reader pass).
	opaque []string
}

// emitterFor returns the entry points that satisfy the kind, or nil when the
// kind has no derived emitter (which the caller must treat as a hard failure —
// a kind the vocabulary does not cover would otherwise be "satisfied" by an
// empty name set only if the site check were written sloppily).
func (v removalAuditVocabulary) emitterFor(kind string) []string {
	return v.byKind[kind]
}

// removalAuditCallShape renders ONE declared call shape from an entry point's
// name. Every shape in this file comes through here, so "a shape is a call" is
// a property of the spelling rather than a convention each matcher re-states:
// the package, the selector and the opening paren are built from their own
// constants, and no caller can construct a shape that is a bare identifier.
func removalAuditCallShape(name string) string {
	return removalAuditPkg + removalAuditPkgSep + name + removalAuditCallOpen
}

// removalAuditDeclaredEmitShapes returns the declared CALL SHAPES a site of the
// kind's class may satisfy — the UNION the two changes to this file each held
// half of:
//
//   - WHICH entry points write the class's Record.Kind is DERIVED from the
//     emitter's own implementation (removalAuditDeriveVocabulary), so a renamed
//     or ADDED same-kind sibling — BranchDelete/BranchDeleteAt, 9b88c4b5b —
//     cannot drift the gate red; it is never a second hand-kept name list
//     (Rule 13/14);
//   - the SHAPE each is matched as is DECLARED here, one spelling per entry
//     point, ending in removalAuditCallOpen, so only a CALL satisfies it.
//
// The kind class is mapped through removalAuditKindOfClass, which Fatals when
// the emitter's derived constants do not cover the gate's class — an empty or
// mismapped shape list was the "green while reading nothing" failure this gate
// must never have again (Rule 15).
func removalAuditDeclaredEmitShapes(t *testing.T, vocab removalAuditVocabulary, kind string) []string {
	t.Helper()
	names := vocab.emitterFor(removalAuditKindOfClass(t, vocab, kind))
	shapes := make([]string, 0, len(names))
	for _, n := range names {
		shapes = append(shapes, removalAuditCallShape(n))
	}
	sort.Strings(shapes)
	return shapes
}

// removalAuditLegacyEmitShapes reconstructs the PRE-FIX matcher's surface: for
// each kind, the ONE spelling this gate held before the fix — the base entry
// point and nothing beside it. It is a MEASUREMENT INSTRUMENT and the gate never
// consults it.
//
// Its purpose is that the fix be LOAD-BEARING AND MEASURED rather than
// asserted-by-rename: TestRemovalAuditGateShapeListIsLoadBearing runs the
// production predicate over a real close-out file with this list, and the list
// must RED on the very tree the declared list passes on. If the emitter's
// close-out sites stop calling the superset entry point, that half stops
// reproducing the resident red and the test SAYS SO instead of passing quietly
// (a proof that cannot go red has stopped proving anything).
func removalAuditLegacyEmitShapes(_ *testing.T, _ removalAuditVocabulary, kind string) []string {
	name := removalAuditEmitWorktree
	if kind == removalAuditKindBranch {
		name = removalAuditEmitBranch
	}
	return []string{removalAuditCallShape(name)}
}

// removalAuditEmitted reports whether the site is covered by a record that is
// ITS OWN — see removalAuditPerSitePairing for the contract, the measurement
// that produced it and the rejected alternatives.
//
// priorSites is the number of same-kind sites that precede this one IN THE SAME
// INNERMOST BLOCK (removalAuditPriorSameKindSites); the site needs priorSites+1
// qualifying emits ahead of it, which is exactly "every site of this kind in
// this block, in order, has its own record before it".
func removalAuditEmitted(t *testing.T, p *parsed, call *ast.CallExpr, kind string, priorSites int, vocab removalAuditVocabulary, shapesFor removalAuditShapeListFor) bool {
	t.Helper()
	if p == nil || call == nil {
		return false
	}
	shapes := shapesFor(t, vocab, kind)
	if len(shapes) == 0 {
		t.Fatalf("the matcher declares no call shape for kind %q — every site of that kind would read "+
			"as unrecorded", kind)
	}
	want := map[string]bool{}
	for _, s := range shapes {
		want[s] = true
	}
	return removalAuditEmitsBefore(t, p, call, want) > priorSites
}

// removalAuditEmitsBefore counts the matching emits that may cover a site,
// applying BOTH halves of the pairing rule:
//
//	(1) POSITION — the emit precedes the site (the epic's "before the mutation"
//	    contract: a record written after the mutation explains nothing about a
//	    crash inside it);
//	(2) ATTRIBUTION — the emit lies in the site's INNERMOST BLOCK, or in a block
//	    enclosing it WITHIN removalAuditEmitGapBudget lines above the site. An
//	    emit in a SIBLING block is never counted (a record written in a branch
//	    that may not be taken is not a record this mutation carries), and the
//	    adjacency budget is what keeps a site from claiming a record that sits
//	    in the same function but belongs to a different lane of it.
func removalAuditEmitsBefore(t *testing.T, p *parsed, call *ast.CallExpr, want map[string]bool) int {
	t.Helper()
	target, targetLine := removalAuditOffsetAndLine(p, call.Pos())
	if target < 0 {
		// Rule 15: a site the file cannot locate is NOT silently "recorded".
		// It cannot be, and saying so is the difference between a refusal and a
		// green from a predicate that read nothing.
		return 0
	}
	blocks := removalAuditBlocks(p)
	inner, ok := removalAuditInnermostBlock(blocks, call.Pos())
	if !ok {
		return 0
	}
	count := 0
	ast.Inspect(p.file, func(n ast.Node) bool {
		em, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		sel, isSel := em.Fun.(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name == "" {
			return true
		}
		pkg, isIdent := sel.X.(*ast.Ident)
		if !isIdent || pkg.Name != removalAuditPkg {
			return true
		}
		// The SHAPE, not the bare selector name: the want-set holds declared call
		// shapes (`removalaudit.X(`), so the comparison is what makes the trailing
		// paren load-bearing in the MATCHER as well as in the list's spelling. A
		// selector name compared against a shape never matches — the failure mode
		// that turns every site red while the records are being written.
		if !want[removalAuditCallShape(sel.Sel.Name)] {
			return true
		}
		off, line := removalAuditOffsetAndLine(p, em.Pos())
		if off < 0 || off >= target {
			return true
		}
		eb, isBlock := removalAuditInnermostBlock(blocks, em.Pos())
		if !isBlock {
			return true
		}
		// (2a) the site's OWN block — the record sits in the same block, so it
		// belongs to this site outright; or (2b) an ENCLOSING block, which only
		// counts while the record is still adjacent to the site
		// (removalAuditEmitGapBudget). Without the budget an emit anywhere in
		// the enclosing function would cover a site in a deeply nested branch —
		// the "one record, several sites" collapse this pairing removes.
		if eb == inner || (removalAuditBlockContains(eb, inner) && targetLine-line <= removalAuditEmitGapBudget) {
			count++
		}
		return true
	})
	return count
}

// removalAuditBlockContains reports whether outer contains inner; blocks nest, so
// identity counts (a block contains itself).
func removalAuditBlockContains(outer, inner *ast.BlockStmt) bool {
	return outer != nil && inner != nil && outer.Lbrace <= inner.Lbrace && inner.Rbrace <= outer.Rbrace
}

// removalAuditBlocks returns every block statement of a file.
func removalAuditBlocks(p *parsed) []*ast.BlockStmt {
	var out []*ast.BlockStmt
	ast.Inspect(p.file, func(n ast.Node) bool {
		if b, ok := n.(*ast.BlockStmt); ok {
			out = append(out, b)
		}
		return true
	})
	return out
}

// removalAuditInnermostBlock returns the smallest block containing pos.
func removalAuditInnermostBlock(blocks []*ast.BlockStmt, pos token.Pos) (*ast.BlockStmt, bool) {
	var best *ast.BlockStmt
	for _, b := range blocks {
		if b.Lbrace <= pos && pos <= b.Rbrace {
			if best == nil || b.Lbrace > best.Lbrace {
				best = b
			}
		}
	}
	return best, best != nil
}

// removalAuditOffsetAndLine converts a token.Pos into an offset inside the FILE's
// own source text plus its 1-based line, or (-1, 0) when pos lies outside the
// file. The file's byte range IS the position domain every node of that file
// belongs to, so two positions taken from nodes of the SAME file compare
// correctly here — including across the FileSets the vocabulary derivation
// builds, where raw token.Pos values are NOT comparable.
func removalAuditOffsetAndLine(p *parsed, pos token.Pos) (int, int) {
	if p == nil || p.file == nil {
		return -1, 0
	}
	if pos < p.file.FileStart || pos > p.file.FileEnd {
		return -1, 0
	}
	return int(pos - p.file.FileStart), p.fset.Position(pos).Line
}

// removalAuditDeriveVocabulary reads the emitter's OWN implementation and
// derives, for every exported entry point, the Record.Kind it writes — the
// gate's vocabulary, derived at run time instead of written here.
//
// THE THREE STEPS, each forced by a shape the emitter actually has:
//
//  1. consts — the Record's `Kind` field is filled with a CONSTANT
//     (`Kind: KindBranchDelete`), never a literal, so the kind value has to be
//     resolved through the same const resolver the site walk uses.
//  2. calls — `BranchDelete` writes no record of its own: it delegates to its
//     same-kind sibling `BranchDeleteAt`, and only that one reaches emit().
//     Without the delegation hop the sibling would derive as "writes nothing"
//     and this gate would report exactly the two sites it was already wrongly
//     reporting. The fixpoint follows the hop transitively.
//  3. fixed point — the delegation graph is traversed until it stops growing,
//     so a chain of any depth classifies the same as a direct call.
//
// The vocabulary is DERIVED, so a rename drifts nothing and a new same-kind
// sibling is covered the moment it is written. What it deliberately does NOT do
// is fall back to the emitter's exported surface wholesale: a name counts only
// when a Record this package constructs is filled with a kind through it, so
// `removalaudit.Records` (which READS records back) cannot satisfy a site —
// TestRemovalAuditGateDerivesTheEmitterVocabulary pins that half.
func removalAuditDeriveVocabulary(t *testing.T, root string) removalAuditVocabulary {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(removalAuditPkgFile)))
	if err != nil {
		t.Fatalf("read the emitter %s: %v — the gate derives its vocabulary from the emitter's own "+
			"implementation, so an unreadable emitter is a failure, never an empty vocabulary (Rule 15)",
			removalAuditPkgFile, err)
	}
	vocab, err := removalAuditDeriveVocabularyFromSource(removalAuditPkgFile, src)
	if err != nil {
		t.Fatalf("derive the emitter vocabulary from %s: %v", removalAuditPkgFile, err)
	}
	return vocab
}

// removalAuditDeriveVocabularyFromSource is the emitter-source half of the
// derivation, separated from the file read so the probe tests can derive from a
// MUTATED copy of the real source (the "renamed sibling does not drift the
// gate" probe runs on exactly that).
func removalAuditDeriveVocabularyFromSource(name string, src []byte) (removalAuditVocabulary, error) {
	fset, f, err := parseSourceErr(name, src)
	if err != nil {
		return removalAuditVocabulary{}, err
	}
	kindValues := removalAuditKindConstants(f)
	// bodyOf: function name → its body, for the delegation hop.
	bodyOf := map[string]*ast.BlockStmt{}
	order := []string{}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		bodyOf[fd.Name.Name] = fd.Body
		order = append(order, fd.Name.Name)
	}
	funcs := map[string]*ast.FuncDecl{}
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
			funcs[fd.Name.Name] = fd
		}
	}

	// kindsWrittenBy returns the kinds the named function writes DIRECTLY or
	// through the delegation hop — computed per function, memoised, cycle-safe.
	writing := map[string]map[string]bool{}
	var written func(name string) map[string]bool
	var visit func(name string, seen map[string]bool) map[string]bool
	visit = func(name string, seen map[string]bool) map[string]bool {
		if got, ok := writing[name]; ok {
			return got
		}
		if seen[name] {
			return nil // a recursion within the emitter writes no kind on this path
		}
		seen[name] = true
		out := map[string]bool{}
		body := bodyOf[name]
		if body != nil {
			ast.Inspect(body, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.CompositeLit:
					if !removalAuditIsRecordLit(v, kindValues) {
						return true
					}
					for _, elt := range v.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						key, ok := kv.Key.(*ast.Ident)
						if !ok || key.Name != removalAuditKindField {
							continue
						}
						if id, ok := kv.Value.(*ast.Ident); ok {
							if kind, ok := kindValues[id.Name]; ok {
								out[kind] = true
							}
						}
					}
				case *ast.CallExpr:
					// the delegation hop: an unqualified call to another
					// function of the same package.
					if id, ok := v.Fun.(*ast.Ident); ok && funcs[id.Name] != nil && id.Name != name {
						for k := range visit(id.Name, seen) {
							out[k] = true
						}
					}
				}
				return true
			})
		}
		writing[name] = out
		return out
	}
	written = func(name string) map[string]bool { return visit(name, map[string]bool{}) }

	vocab := removalAuditVocabulary{byKind: map[string][]string{}}
	for _, name := range order {
		if !ast.IsExported(name) {
			continue
		}
		kinds := written(name)
		if len(kinds) == 0 {
			vocab.opaque = append(vocab.opaque, name)
			continue
		}
		for kind := range kinds {
			vocab.byKind[kind] = append(vocab.byKind[kind], name)
		}
	}
	for kind := range vocab.byKind {
		sort.Strings(vocab.byKind[kind])
	}
	sort.Strings(vocab.opaque)
	_ = fset
	// Rule 15: an EMPTY vocabulary is a hard failure, never a silent skip. Every
	// site would read as unrecorded against it, so a gate that "passed" while
	// understanding nothing is the failure mode this refusal removes. It is an
	// error (not a t.Fatal here) so a probe can assert the refusal.
	if len(vocab.byKind) == 0 {
		return removalAuditVocabulary{}, fmt.Errorf("the emitter source declares no Record.%s filled by an "+
			"exported entry point — the vocabulary would be empty and every removal site would read as "+
			"unrecorded", removalAuditKindField)
	}
	return vocab, nil
}

// removalAuditKindConstants resolves the Kind-typed constants the emitter
// declares, one alias hop at a time to a fixpoint (Rule 13's "spell it once":
// a kind is declared once and referenced everywhere else).
func removalAuditKindConstants(f *ast.File) map[string]string {
	values := map[string]string{}
	aliases := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i := range vs.Names {
				switch v := vs.Values[i].(type) {
				case *ast.BasicLit:
					if v.Kind == token.STRING {
						values[vs.Names[i].Name] = strings.Trim(v.Value, `"`)
					}
				case *ast.Ident:
					aliases[vs.Names[i].Name] = v.Name
				}
			}
		}
	}
	for i := 0; i < len(aliases)+1; i++ {
		changed := false
		for name, target := range aliases {
			if _, done := values[name]; done {
				continue
			}
			if v, ok := values[target]; ok {
				values[name] = v
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return values
}

// removalAuditIsRecordLit reports whether a composite literal is the emitter
// constructing its OWN Record — the shape whose Kind field is the record this
// site would write. The type is matched by AST shape, not by a package
// qualifier, because inside the emitter's own package it is written bare and a
// probe parses the file under a synthetic filename (so no import resolves).
func removalAuditIsRecordLit(lit *ast.CompositeLit, kindValues map[string]string) bool {
	if lit == nil {
		return false
	}
	id, ok := lit.Type.(*ast.Ident)
	if !ok || id.Name != removalAuditRecordType {
		return false
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == removalAuditKindField {
			return true
		}
	}
	// A Record built with a FieldKind variable key is still a Record: accept it
	// only when a kind value is resolvable, which the caller checks.
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if val, ok := kv.Value.(*ast.Ident); ok {
				if _, known := kindValues[val.Name]; known {
					return true
				}
			}
		}
	}
	return false
}

// removalAuditKindOfClass maps the Record.Kind a site classifies into onto that
// kind's value. The KIND classes are the gate's own two site classes; the VALUES
// are the emitter's, taken from its constants, so the two vocabularies meet in
// exactly one place instead of two hand-kept lists.
func removalAuditKindOfClass(t *testing.T, vocab removalAuditVocabulary, kindClass string) string {
	t.Helper()
	var want string
	if kindClass == removalAuditKindBranch {
		want = removalAuditKindValueBranch
	} else {
		want = removalAuditKindValueWorktree
	}
	if len(vocab.emitterFor(want)) == 0 {
		t.Fatalf("the emitter declares no entry point writing Record.Kind %q — the class %q cannot be "+
			"satisfied, so the gate's own classes have drifted from the emitter's constants", want, kindClass)
	}
	return want
}

// removalAuditKindValueWorktree / Branch are the VALUES of the emitter's two
// record kinds. They are the gate's half of a contract whose other half is the
// emitter's: TESTED (not assumed) by
// TestRemovalAuditGateDerivesTheEmitterVocabulary, which requires the emitter to
// declare a Kind constant with each value, so a value renamed in the emitter
// fails loudly HERE rather than silently reporting every site unrecorded.
const (
	removalAuditKindValueWorktree = "worktree-removal"
	removalAuditKindValueBranch   = "branch-delete"
)

// value, one alias hop at a time to a fixpoint — the gate must read
// `finishCleanupWorktreeRemoveVerb` as "remove" or it would see ONLY the few
// sites that still spell the verbs inline (and would then be green-blind, the
// exact failure the floor exists to catch).
func removalAuditConsts(t *testing.T, root string, files []string) map[string]string {
	t.Helper()
	values := map[string]string{}
	idents := map[string]string{} // const name → another const name (alias)
	for _, rel := range files {
		p := parseRepoFile(t, root, rel)
		for _, decl := range p.file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, name := range vs.Names {
					switch v := vs.Values[i].(type) {
					case *ast.BasicLit:
						if v.Kind == token.STRING {
							values[name.Name] = strings.Trim(v.Value, `"`)
						}
					case *ast.Ident:
						idents[name.Name] = v.Name
					}
				}
			}
		}
	}
	for i := 0; i < len(idents)+1; i++ {
		changed := false
		for name, target := range idents {
			if _, done := values[name]; done {
				continue
			}
			if v, ok := values[target]; ok {
				values[name] = v
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return values
}

// removalAuditArgValues renders one call's arguments as resolved string values
// (an unresolvable argument contributes nothing, which is why the gate errs
// toward NOT reporting rather than inventing a site).
func removalAuditArgValues(call *ast.CallExpr, consts map[string]string) []string {
	var out []string
	for _, arg := range call.Args {
		if v := removalAuditResolvedArg(arg, consts); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// removalAuditIsForwardingCall reports whether a classified call merely CARRIES a
// git argument vector instead of performing a removal of its own. Two shapes,
// both measured:
//
//   - its FIRST resolved argument is a git subcommand word (`worktree` /
//     `branch`), i.e. the call's own argument list IS `git <verb> <flag>
//     <target>` with the program name and every directory left off. A real
//     invocation names WHAT to run in (the helper's context/repo argument,
//     `exec.Command`'s program) before the subcommand.
//   - its FIRST argument is a git CONFIGURATION `-C <dir>` pair — the shapes
//     `git <runner>(ctx, "-C", dir, <verb>, …)` and
//     `exec.CommandContext(ctx, "git", "-C", dir, <verb>, …)` — which is the
//     same argument vector with a `-C` in front. Without this arm an ordinary
//     `-C`-configuring call would be read as forwarding and would silently stop
//     counting for a record; it is the LOWEST-PRECEDENCE arm (a real helper's
//     first argument is never `-C`, because it also needs the repo as an
//     argument) and the ambiguity it could introduce is stated below rather than
//     hidden.
//
// MEASURED (2026-10-12), with the call shapes on the real tree side by side:
//
//	internal/tools/worktree_sweep.go:2596  worktreeSweepObserve(branchVerb, deleteFlag, branch)
//	                                        — args ARE `branch -d <name>`; the
//	                                        observer hook (production leaves it
//	                                        nil) whose job is to REPORT the
//	                                        invocation, added 2026-09-14, before
//	                                        the site's emit (2026-09-29).
//	internal/tools/worktree_sweep.go:2602  exec.CommandContext(ctx, "git", branchVerb, deleteFlag, branch)
//	                                        — the deletion itself.
//	internal/tools/task_finish_cleanup.go:4310 finishCleanupGit(ctx, dir, branchVerb, deleteFlag, branch)
//	                                        — a real invocation through a helper.
//
// WHY IT MATTERS TO THE PAIRING, and why it is not an exemption: such a call is
// still REPORTED as a site (the census keeps it; nothing is hidden), but it
// performs no removal, so it must not CONSUME a record — the record belongs to
// the removal it reports and to the invocation that performs it. Without this
// split, one record would have to cover both spellings of one deletion, which is
// the very "one record, several sites" collapse the per-site pairing removes.
//
// The direction of the error is stated honestly: a future removal written as
// `runGit("branch", "-D", br)` would be read as forwarding and would therefore
// be satisfied by any same-kind record above it rather than by one of its own.
// It stays VISIBLE in the census (never silently dropped), and the shape does
// not occur on the tree today — the alternative (treating such a call as an
// invocation) is what silently re-opens the reported defect.
func removalAuditIsForwardingCall(call *ast.CallExpr, consts map[string]string) bool {
	if call == nil || len(call.Args) == 0 {
		return false
	}
	first := removalAuditResolvedArg(call.Args[0], consts)
	if first == removalAuditVerbWorktree || first == removalAuditVerbBranch {
		return true
	}
	return removalAuditFirstArgIsConfigDir(call, consts)
}

// removalAuditFirstArgIsConfigDir reports the `-C <dir>` prefix shape: the first
// argument resolves to git's configuration-directory flag and the second one is
// present (the directory it carries). Both halves are required — a lone `-C` is
// not the shape, and requiring the directory keeps the match as tight as the
// spellings the arms actually run.
func removalAuditFirstArgIsConfigDir(call *ast.CallExpr, consts map[string]string) bool {
	if len(call.Args) < 2 {
		return false
	}
	return removalAuditResolvedArg(call.Args[0], consts) == removalAuditConfigDirFlag
}

// removalAuditResolvedArg resolves one call argument to its string value, or ""
// when it does not resolve (Rule 15: an unresolvable argument contributes
// nothing, which keeps the gate erring toward NOT reporting rather than inventing
// a site).
//
// A SELECTOR argument resolves through the SAME const map by its selector name
// (`removalaudit.RefDeleteVerb` → the const `RefDeleteVerb`), because the
// production spelling of a git verb is a constant in its owning package (Rule
// 14) and a census that could only read bare identifiers would be blind to
// exactly the shared spelling it exists to encourage. The direction of the
// error stays the one this comment describes: a selector whose name is not a
// package const contributes nothing; a name that DOES resolve contributes the
// value the compiler would, so a mismatched package alias cannot manufacture a
// removal site out of prose — it takes a real const name and a real flag.
func removalAuditResolvedArg(arg ast.Expr, consts map[string]string) string {
	switch v := arg.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			return strings.Trim(v.Value, `"`)
		}
	case *ast.Ident:
		return consts[v.Name]
	case *ast.SelectorExpr:
		return consts[v.Sel.Name]
	}
	return ""
}

// removalAuditClassify returns the site's kind ("" when the call is not a
// removal) from its resolved argument values.
func removalAuditClassify(values []string) string {
	has := func(want string) bool {
		for _, v := range values {
			if v == want {
				return true
			}
		}
		return false
	}
	if has(removalAuditVerbWorktree) && has(removalAuditVerbRemove) {
		return removalAuditKindWorktree
	}
	if has(removalAuditVerbBranch) &&
		(has(removalAuditFlagDelete) || has(removalAuditFlagForceDel) || has(removalAuditFlagRemote)) {
		return removalAuditKindBranch
	}
	// The PLUMBING spelling of the same deletion (see removalAuditSequelVerbs):
	// `update-ref -d <ref>` removes a branch ref without ever naming `branch`.
	return removalAuditClassifySequel(has)
}

// removalAuditClassifySequel is the second vocabulary's half of the
// classification, kept as its own function so the porcelain pair above reads as
// one decision and the plumbing pair below as another — a site is a removal if
// EITHER vocabulary says it is.
func removalAuditClassifySequel(has func(string) bool) string {
	if !removalAuditSequelVocabOn {
		return "" // the RED proof's switch: exactly the pre-2026-10-12 census
	}
	for _, verb := range removalAuditSequelVerbs {
		if has(verb) && has(removalAuditSequelDeleteFlag) {
			return removalAuditKindBranch
		}
	}
	return ""
}

// removalAuditSequelVocabOn gates the plumbing vocabulary. It is ON in every
// real run and is flipped OFF only by TestRemovalAuditGateSequelVocabularyIsLoadBearing,
// which measures the pre-fix census (a tree where the plumbing deletion reads as
// no site at all) on the REAL tree rather than asserting that it was blind. It
// exists for the same reason the emitter's SetEmitDisabledForTest does: the RED
// half of a regression proof must be driven, not described.
var removalAuditSequelVocabOn = true

// Site states — the four ways a classified removal site can stand.
const (
	removalAuditStateEmitting   = "emitting"
	removalAuditStateEscaped    = "escaped"
	removalAuditStateExempt     = "exempt"
	removalAuditStateUnrecorded = "unrecorded"
)

// removalAuditClassifiedSite is one removal site plus how it stands.
type removalAuditClassifiedSite struct {
	removalAuditSite
	state string
}

// removalAuditWalk is the gate's ONE pass over the production tree. Both the
// gate and the data report read it, so the census an operator sees and the
// verdict CI enforces can never disagree (a second walk is a second truth).
func removalAuditWalk(t *testing.T) []removalAuditClassifiedSite {
	t.Helper()
	root := repoRoot(t)
	return removalAuditWalkWith(t, root, removalAuditDeriveVocabulary(t, root))
}

// removalAuditWalkWith is the walk with the vocabulary INJECTED — the entry
// point the "a renamed sibling does not drift the gate" probe uses, so the
// probe exercises the real walk rather than a fragment of its predicate.
func removalAuditWalkWith(t *testing.T, root string, vocab removalAuditVocabulary) []removalAuditClassifiedSite {
	t.Helper()
	files := goFilesUnder(t, root, prodRoots, false)
	consts := removalAuditConsts(t, root, files)
	var out []removalAuditClassifiedSite
	for _, rel := range files {
		if rel == removalAuditPkgFile {
			continue // the emitter's own package declares the surface, never a site
		}
		p := parseRepoFile(t, root, rel)
		ast.Inspect(p.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			values := removalAuditArgValues(call, consts)
			kind := removalAuditClassify(values)
			if kind == "" {
				return true
			}
			line := p.fset.Position(call.Pos()).Line
			site := removalAuditSite{rel: rel, line: line, kind: kind, desc: strings.Join(values, " ")}
			state := removalAuditStateUnrecorded
			switch {
			case removalAuditEnclosingFuncEmits(t, p, call, kind, consts, vocab):
				state = removalAuditStateEmitting
			case lineHasMarker(p.src, line, removalAuditGateEscape) ||
				removalAuditEscapeAbove(p, line):
				state = removalAuditStateEscaped
			case removalAuditSitesNoRecord[site.key()] != "":
				state = removalAuditStateExempt
			}
			out = append(out, removalAuditClassifiedSite{removalAuditSite: site, state: state})
			return true
		})
	}
	return out
}

// removalAuditEnclosingFuncEmits reports whether the site is covered by a record
// that is ITS OWN (removalAuditEmitted). The "its own" half is the fix this file
// carries: the check used to be "the enclosing function holds ANY matching emit
// before the site", which let the first site's record satisfy every later site
// of the same function — deleting the SECOND site's emit left the gate green
// with the record gone (bug row 01a12be8, 2026-10-12).
// removalAuditEnclosingFuncEmits runs the production predicate with the gate's
// own declared shape list.
func removalAuditEnclosingFuncEmits(t *testing.T, p *parsed, call *ast.CallExpr, kind string, consts map[string]string, vocab removalAuditVocabulary) bool {
	t.Helper()
	return removalAuditEnclosingFuncEmitsWith(t, p, call, kind, consts, vocab, removalAuditDeclaredEmitShapes)
}

// removalAuditShapeListFor maps a kind class onto the call SHAPES that satisfy
// it. The gate passes removalAuditDeclaredEmitShapes (derived names, declared
// shapes); a probe passes removalAuditLegacyEmitShapes to reconstruct the
// pre-fix surface. Injecting it — instead of mutating the emitter's source —
// is what lets the red proof run over the REAL tree the declared list passes
// on: the emitter's implementation is untouched, only the matcher narrows, so
// the two verdicts differ in exactly the thing under test.
type removalAuditShapeListFor func(t *testing.T, vocab removalAuditVocabulary, kind string) []string

// removalAuditEnclosingFuncEmitsWith is removalAuditEnclosingFuncEmits with the
// SHAPE LIST passed IN, so the red proof can MEASURE what a NARROWER matcher saw
// on the very tree the declared one passes on. Every other half of the predicate
// — the verb classification, the kind mapping, the position and attribution
// rules — is the production one.
func removalAuditEnclosingFuncEmitsWith(t *testing.T, p *parsed, call *ast.CallExpr, kind string, consts map[string]string, vocab removalAuditVocabulary, shapesFor removalAuditShapeListFor) bool {
	t.Helper()
	return removalAuditEmitted(t, p, call, kind,
		removalAuditPriorSameKindSites(p, call, kind, consts), vocab, shapesFor)
}

// removalAuditPriorSameKindSites counts the same-kind sites that precede call in
// source order AND sit in the same INNERMOST BLOCK. Counting them is what turns
// "an emit exists above me" into "an emit exists above me that no earlier site
// has spent": the site at position N of a block needs the (N+1)-th qualifying
// emit above it, not the first.
//
// WHY THE INNERMOST BLOCK AND NOT THE ENCLOSING FUNCTION: the sites this rule
// must separate are the ones whose own emits sit in the block they share — the
// measured pair is the `-d` delete and the `-D` fallback in
// task_finish_cleanup.go, whose two emits sit in ONE function body with the
// second emit between them. Two sites in the same FUNCTION but in different
// BRANCHES are not competing for one record (each branch's lane emits its own),
// so they must not consume each other's.
func removalAuditPriorSameKindSites(p *parsed, call *ast.CallExpr, kind string, consts map[string]string) int {
	target, _ := removalAuditOffsetAndLine(p, call.Pos())
	if target < 0 {
		return 0
	}
	blocks := removalAuditBlocks(p)
	inner, ok := removalAuditInnermostBlock(blocks, call.Pos())
	if !ok {
		return 0
	}
	prior := 0
	ast.Inspect(p.file, func(n ast.Node) bool {
		other, isCall := n.(*ast.CallExpr)
		if !isCall || other == call {
			return true
		}
		if removalAuditClassify(removalAuditArgValues(other, consts)) != kind {
			return true
		}
		// A FORWARDING call is reported as a site but performs no removal, so it
		// must not SPEND a record: the record above it belongs to the removal it
		// reports (removalAuditIsForwardingCall).
		if removalAuditIsForwardingCall(other, consts) {
			return true
		}
		ob, isBlock := removalAuditInnermostBlock(blocks, other.Pos())
		if !isBlock || ob != inner {
			return true
		}
		if off, _ := removalAuditOffsetAndLine(p, other.Pos()); off >= 0 && off < target {
			prior++
		}
		return true
	})
	return prior
}

// removalAuditEscapeAbove reports whether the escape marker sits in the
// CONTIGUOUS comment block directly above the site's line. It uses the parsed
// comment groups, so the block that counts is the one the AUTHOR attached to
// this statement: a doc comment two statements up (the function's own header,
// the arm's own note) does not release a site, and a marker that floats at the
// top of a file releases nothing.
func removalAuditEscapeAbove(p *parsed, line int) bool {
	var (
		best     *ast.CommentGroup
		bestLine int
	)
	for _, cg := range p.file.Comments {
		end := p.fset.Position(cg.End()).Line
		if end > line || end < bestLine {
			continue
		}
		if best == nil || end >= bestLine {
			// Keep the LAST group that ends above the site, but never one whose
			// end is itself a violation of the contiguity test below.
			best, bestLine = cg, end
		}
	}
	if best == nil {
		return false
	}
	for l := bestLine + 1; l < line; l++ {
		if !strings.HasPrefix(strings.TrimSpace(lineOf(p.src, l)), "//") {
			return false
		}
	}
	return strings.Contains(best.Text(), removalAuditGateEscape)
}

// key is the site's `file:line` identity — the form the exemption list uses.
func (s removalAuditSite) key() string { return s.rel + ":" + itoaStandard(s.line) }

// removalAuditDeclaredArms returns the emitter's Arm constants that NO
// production emit passes. An arm nobody passes is a dead constant: the record
// silently stops covering its path while the name still reads like coverage.
func removalAuditDeclaredArms(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(removalAuditPkgFile)))
	if err != nil {
		t.Fatalf("read the emitter: %v", err)
	}
	declared := map[string]bool{}
	for _, line := range strings.Split(string(src), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		// The Arm block's shape: `ArmName Arm = "…"`.
		if len(fields) >= 3 && strings.HasPrefix(fields[0], removalAuditArmPrefix) &&
			fields[0] != "Arm" && fields[1] == "Arm" {
			declared[fields[0]] = true
		}
	}
	for _, rel := range goFilesUnder(t, root, prodRoots, false) {
		body, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		for name := range declared {
			if strings.Contains(string(body), removalAuditPkg+"."+name) {
				delete(declared, name)
			}
		}
	}
	var unused []string
	for name := range declared {
		unused = append(unused, name)
	}
	sort.Strings(unused)
	return unused
}

// TestRemovalAuditGateEveryRemovalSiteEmits is the gate: a production removal
// site carrying no record of its own FAILS, naming the file, the line, the verb
// pair it ran and the emitter it owes.
func TestRemovalAuditGateEveryRemovalSiteEmits(t *testing.T) {
	sites := removalAuditWalk(t)
	vocab := removalAuditDeriveVocabulary(t, repoRoot(t))

	// Non-vacuity FIRST: a scan that classified almost nothing is a broken
	// predicate (a changed verb spelling, a broken glob), and a green verdict
	// from it would be meaningless.
	if len(sites) < removalAuditSitesFloor {
		t.Fatalf("the walk classified %d removal sites, below the floor of %d — the predicate is reading "+
			"a tree it does not understand, so a green result would be meaningless", len(sites), removalAuditSitesFloor)
	}
	if unused := removalAuditDeclaredArms(t); len(unused) > 0 {
		t.Errorf("arm constants declared but passed by no production emit — a dead arm silently stops "+
			"covering its path (floor: %d arms referenced): %v", removalAuditArmsFloor, unused)
	}

	for _, s := range sites {
		switch s.state {
		case removalAuditStateUnrecorded:
			t.Errorf("%s is a %s site (%s) whose enclosing function holds no removalaudit.%s BEFORE it — "+
				"an unattributable removal is exactly what this gate exists to refuse. Emit the record first, "+
				"or release the site with `%s <reason>`", s.key(), s.kind, s.desc,
				removalAuditEmitNamesFor(t, vocab, s.kind)[len(removalAuditPkg)+1:], removalAuditGateEscape)
		case removalAuditStateEscaped:
			t.Logf("released by %s: %s %s (%s) — visible, reviewed release, not a suppression",
				removalAuditGateEscape, s.key(), s.kind, s.desc)
		case removalAuditStateExempt:
			t.Logf("exempt %s %s — %s", s.key(), s.kind, removalAuditSitesNoRecord[s.key()])
		}
	}
}

// TestRemovalAuditGateDataReport prints the measured census — every classified
// site with its state and every arm the emitter declares — so the floor above is
// re-measured from data rather than lowered to make a tree pass.
func TestRemovalAuditGateDataReport(t *testing.T) {
	sites := removalAuditWalk(t)
	perState := map[string]int{}
	perKind := map[string]int{}
	for _, s := range sites {
		perState[s.state]++
		perKind[s.kind]++
	}
	t.Logf("removal sites: total=%d %v (floor %d) kinds=%v", len(sites), perState, removalAuditSitesFloor, perKind)
	for _, s := range sites {
		if s.state == removalAuditStateUnrecorded {
			t.Logf("UNRECORDED %s %s (%s)", s.key(), s.kind, s.desc)
		}
	}
	unused := removalAuditDeclaredArms(t)
	t.Logf("declared arms with no production emit: %v (floor %d arms referenced)", unused, removalAuditArmsFloor)
}

// removalAuditEmitNamesFor renders the entry points the derived vocabulary says
// a kind owes — the message's half of the DERIVED contract: a site is told which
// of the emitter's own names it could have called, and a same-kind sibling is
// named alongside its twin instead of being invisible to the gate.
func removalAuditEmitNamesFor(t *testing.T, vocab removalAuditVocabulary, kind string) string {
	t.Helper()
	return removalAuditPkg + "." + strings.Join(vocab.emitterFor(removalAuditKindOfClass(t, vocab, kind)), "/"+removalAuditPkg+".")
}

// itoaStandard is the tiny integer renderer this file's messages need; the
// package's other gates use strconv, and so does this one where it matters —
// this helper exists only to keep the report lines allocation-free and readable.
func itoaStandard(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// parseSourceErr is parseSource's non-fatal twin: the emitter derivation runs on
// the package's real source, and (unlike a probe) it must report a parse failure
// as an error rather than a test failure, because it is also run on a MUTATED
// copy of that source inside a probe.
func parseSourceErr(name string, src []byte) (*token.FileSet, *ast.File, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	return fset, f, nil
}

// removalAuditProbeClassify classifies a SYNTHETIC source the way the walk
// classifies a production file, returning the unrecorded site keys. It runs on
// the pure verb pair (the probe's source spells its own verbs), so the probe
// pins the PREDICATE rather than the tree's constant table.
func removalAuditProbeClassify(t *testing.T, name, src string, vocab removalAuditVocabulary) []string {
	t.Helper()
	fset, f := parseSource(t, name, src)
	p := &parsed{rel: name, fset: fset, file: f, src: []byte(src)}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		values := removalAuditArgValues(call, nil)
		kind := removalAuditClassify(values)
		if kind == "" {
			return true
		}
		line := fset.Position(call.Pos()).Line
		site := removalAuditSite{rel: name, line: line, kind: kind, desc: strings.Join(values, " ")}
		switch {
		case removalAuditEnclosingFuncEmits(t, p, call, kind, nil, vocab):
		case lineHasMarker(p.src, line, removalAuditGateEscape) || removalAuditEscapeAbove(p, line):
		default:
			out = append(out, site.key())
		}
		return true
	})
	return out
}

// TestRemovalAuditGateRedProof is the gate's own RED/GREEN proof: the predicate
// must flag a removal site with no record (the defect), must NOT flag a site
// whose record precedes it, must flag a record that comes AFTER the removal, and
// must not accept a werkTree record for a branch site. Each probe pins one half.
func TestRemovalAuditGateRedProof(t *testing.T) {
	vocab := removalAuditDeriveVocabulary(t, repoRoot(t))
	const head = "package probe\n\nimport (\n\t\"github.com/nickhuang99/gopi/internal/removalaudit\"\n\t\"os/exec\"\n)\n\n"

	probes := []struct {
		name     string
		src      string
		wantHits int
	}{
		{
			name: "RED: a worktree removal with no record",
			src: head + `func f(repo, wt string) {
	_ = exec.Command("git", "-C", repo, "worktree", "remove", wt).Run()
}`,
			wantHits: 1,
		},
		{
			name: "RED: a branch deletion with no record",
			src: head + `func f(repo, br string) {
	_ = exec.Command("git", "-C", repo, "branch", "-D", br).Run()
}`,
			wantHits: 1,
		},
		{
			name: "RED: a force -d spelling is the same class",
			src: head + `func f(repo, br string) {
	_ = exec.Command("git", "-C", repo, "branch", "-d", br).Run()
}`,
			wantHits: 1,
		},
		{
			name: "GREEN: the record precedes the removal",
			src: head + `func f(repo, wt, br, id string) {
	removalaudit.WorktreeRemove(removalaudit.ArmSweepReclaim, repo, wt, br, id, "evidence")
	_ = exec.Command("git", "-C", repo, "worktree", "remove", wt).Run()
}`,
			wantHits: 0,
		},
		{
			name: "RED: the record comes AFTER the removal (an explosion explains nothing)",
			src: head + `func f(repo, wt, br, id string) {
	_ = exec.Command("git", "-C", repo, "worktree", "remove", wt).Run()
	removalaudit.WorktreeRemove(removalaudit.ArmSweepReclaim, repo, wt, br, id, "evidence")
}`,
			wantHits: 1,
		},
		{
			name: "RED: a worktree record does not cover a branch deletion",
			src: head + `func f(repo, br string) {
	removalaudit.WorktreeRemove(removalaudit.ArmSweepReclaim, repo, "/some/wt", br, "", "evidence")
	_ = exec.Command("git", "-C", repo, "branch", "-d", br).Run()
}`,
			wantHits: 1,
		},
		{
			name: "GREEN: the reviewed escape releases one site",
			src: head + `func f(repo, wt string) {
	// removal-audit-ok: the shared runner; every caller records its own removal.
	_ = exec.Command("git", "-C", repo, "worktree", "remove", wt).Run()
}`,
			wantHits: 0,
		},
		{
			name: "RED: an escape two comment blocks away releases nothing",
			src: head + `// removal-audit-ok: this is a file header, not attached to any statement.

func f(repo, wt string) {
	_ = exec.Command("git", "-C", repo, "worktree", "remove", wt).Run()
}`,
			wantHits: 1,
		},
		{
			name: "GREEN: a closure carrying both the record and the removal",
			src: head + `func g(repo, wt, br, id string) {
	for {
		func() {
			removalaudit.WorktreeRemove(removalaudit.ArmRowReclaim, repo, wt, br, id, "evidence")
			_ = exec.Command("git", "-C", repo, "worktree", "remove", wt).Run()
		}()
		break
	}
}`,
			wantHits: 0,
		},
		{
			name: "GREEN: a list-only worktree call is not a removal site",
			src: head + `func h(repo string) {
	_ = exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Run()
}`,
			wantHits: 0,
		},
		{
			name: "RED: a comment naming a DECLARED call shape records nothing (a shape ends in '(')",
			src: head + `func j(repo, br string) {
	// removalaudit.BranchDelete( — prose, not a call
	_ = exec.Command("git", "-C", repo, "branch", "-D", br).Run()
}`,
			wantHits: 1,
		},
		{
			name: "GREEN: the BASE entry point of the kind records the deletion (the union covers both siblings)",
			src: head + `func k(repo, br, id string) {
	removalaudit.BranchDelete(removalaudit.ArmFinishCleanupBranch, repo, br, id, "evidence")
	_ = exec.Command("git", "-C", repo, "branch", "-d", br).Run()
}`,
			wantHits: 0,
		},
		{
			name: "GREEN: the SUPERSET entry point BranchDeleteAt records the deletion",
			src: head + `func l(repo, br, id, tip string) {
	removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranch, repo, br, id, tip, "/host/path", "evidence")
	_ = exec.Command("git", "-C", repo, "branch", "-d", br).Run()
}`,
			wantHits: 0,
		},
		{
			name: "RED: a branch record does not cover a WORKTREE removal (the kinds stay apart)",
			src: head + `func m(repo, wt, br, id, tip string) {
	removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranch, repo, br, id, tip, "/host/path", "evidence")
	_ = exec.Command("git", "-C", repo, "worktree", "remove", wt).Run()
}`,
			wantHits: 1,
		},
		{
			name: "GREEN: deleting a remote-tracking ref is not a local branch deletion",
			src: head + `func i(repo, br string) {
	_ = exec.Command("git", "-C", repo, "checkout", "-d", br).Run()
}`,
			wantHits: 0,
		},
	}

	for _, p := range probes {
		got := removalAuditProbeClassify(t, p.name, p.src, vocab)
		if len(got) != p.wantHits {
			t.Errorf("%s: unrecorded sites = %v (%d), want %d — the predicate mis-classifies this shape",
				p.name, got, len(got), p.wantHits)
		}
	}
}

// TestRemovalAuditGateSeesTheRealTree is the DELETE-A-PRODUCER probe: it takes a
// REAL production file's own source, leaves it untouched, and re-runs the
// predicate on the emit line COMMENTED OUT — the real site must then be
// reported. Without this, "the tree is clean" could be a predicate that reports
// nothing at all.
func TestRemovalAuditGateSeesTheRealTree(t *testing.T) {
	root := repoRoot(t)
	const rel = "internal/tools/task_merged_cleanup.go"
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	src := string(raw)
	const realEmit = "removalaudit.WorktreeRemove(removalaudit.ArmMergedCleanupWorktree"
	mutilated, removed := removalAuditDropStatement(src, realEmit)
	if removed == 0 {
		t.Fatalf("%s no longer carries the expected emit — update this probe with the site's current shape", rel)
	}
	t.Logf("mutilated %d line(s) of %s for the probe", removed, rel)

	vocab := removalAuditDeriveVocabulary(t, root)
	clean := removalAuditProbeKeys(t, rel, src, vocab)
	if len(clean) != 0 {
		t.Fatalf("the untouched %s reports %v — the probe's baseline must be clean", rel, clean)
	}
	dirty := removalAuditProbeKeys(t, rel, mutilated, vocab)
	if len(dirty) == 0 {
		t.Fatalf("with the real emit commented out, %s reports NOTHING — the predicate cannot see the "+
			"real tree, so its green verdict is meaningless", rel)
	}
	t.Logf("RED on the real tree (emit removed): %v", dirty)
}

// removalAuditDropStatement removes the whole STATEMENT that starts on the line
// naming needle — the statement's first line and every continuation line up to
// (and including) the line that closes it — and returns the mutilated source and
// the number of lines dropped. A single-line replacement would leave a dangling
// paren and prove nothing but the probe's own typo.
func removalAuditDropStatement(src, needle string) (string, int) {
	lines := strings.Split(src, "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, needle) {
			start = i
			break
		}
	}
	if start < 0 {
		return src, 0
	}
	end := start
	depth := 0
	for i := start; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			end = i
			continue
		}
		depth += strings.Count(lines[i], "(") - strings.Count(lines[i], ")")
		end = i
		if depth <= 0 {
			break
		}
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, "\t\t// mutilated for the probe")
	out = append(out, lines[end+1:]...)
	return strings.Join(out, "\n"), end - start + 1
}

// removalAuditProbeKeys classifies a real file's SOURCE (not the on-disk file),
// resolving the tree's own constants so the probe speaks the same vocabulary the
// walk does.
func removalAuditProbeKeys(t *testing.T, rel, src string, vocab removalAuditVocabulary) []string {
	t.Helper()
	return removalAuditProbeKeysWith(t, rel, src, vocab, removalAuditDeclaredEmitShapes)
}

// removalAuditProbeKeysWith is removalAuditProbeKeys with the SHAPE LIST passed
// in: the same production pass, the same escapes, the same constants — only the
// matcher's declared shapes change. That is what makes the red proof a
// MEASUREMENT rather than a mutation: the tree, the constants and the predicate
// are held fixed, and the two verdicts differ in exactly the list under test.
func removalAuditProbeKeysWith(t *testing.T, rel, src string, vocab removalAuditVocabulary, shapesFor removalAuditShapeListFor) []string {
	t.Helper()
	root := repoRoot(t)
	consts := removalAuditConsts(t, root, goFilesUnder(t, root, prodRoots, false))
	fset, f := parseSource(t, rel, src)
	p := &parsed{rel: rel, fset: fset, file: f, src: []byte(src)}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		kind := removalAuditClassify(removalAuditArgValues(call, consts))
		if kind == "" {
			return true
		}
		line := fset.Position(call.Pos()).Line
		if removalAuditEnclosingFuncEmitsWith(t, p, call, kind, consts, vocab, shapesFor) ||
			lineHasMarker(p.src, line, removalAuditGateEscape) || removalAuditEscapeAbove(p, line) {
			return true
		}
		out = append(out, rel+":"+itoaStandard(line))
		return true
	})
	sort.Strings(out)
	return out
}

// removalAuditProbeTreeKeys is the walk's predicate run over the WHOLE
// production tree read as TEXT, so a probe can rename the emitter's entry points
// on BOTH sides of the call (the emitter's definition and every production call
// site) the way a real rename lands — and see whether the gate still recognises
// the sites. `rename` maps a real entry-point name to its replacement in the
// caller's sources; the emitter source is renamed by the caller before deriving
// the vocabulary it passes in.
func removalAuditProbeTreeKeys(t *testing.T, root string, vocab removalAuditVocabulary, rename map[string]string) []string {
	t.Helper()
	consts := removalAuditConsts(t, root, goFilesUnder(t, root, prodRoots, false))
	var out []string
	for _, rel := range goFilesUnder(t, root, prodRoots, false) {
		if rel == removalAuditPkgFile {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		src := string(raw)
		for from, to := range rename {
			src = strings.ReplaceAll(src, removalAuditPkg+"."+from+"(", removalAuditPkg+"."+to+"(")
		}
		fset, f, perr := parseSourceErr(rel, []byte(src))
		if perr != nil {
			t.Fatalf("parse %s: %v", rel, perr)
		}
		p := &parsed{rel: rel, fset: fset, file: f, src: []byte(src)}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			kind := removalAuditClassify(removalAuditArgValues(call, consts))
			if kind == "" {
				return true
			}
			line := fset.Position(call.Pos()).Line
			if removalAuditEnclosingFuncEmits(t, p, call, kind, consts, vocab) ||
				lineHasMarker(p.src, line, removalAuditGateEscape) || removalAuditEscapeAbove(p, line) {
				return true
			}
			out = append(out, rel+":"+itoaStandard(line))
			return true
		})
	}
	sort.Strings(out)
	return out
}

// removalAuditRenameEmitter returns the emitter's source with one exported entry
// point renamed on both sides of its own definition: `func Old(` and the
// intra-package calls to it. It is a TEXT mutation of the real source on purpose
// — the probe must exercise the derivation on the file as it ships, not on a
// hand-written stand-in that could drift from it.
func removalAuditRenameEmitter(src, old, new string) string {
	src = strings.ReplaceAll(src, "func "+old+"(", "func "+new+"(")
	src = strings.ReplaceAll(src, "\t"+old+"(", "\t"+new+"(")
	return src
}

// TestRemovalAuditGateDerivesTheEmitterVocabulary pins the DERIVATION — the
// property the gate's own defect was the absence of. The gate used to name the
// branch-delete emitter as one string literal, so adding a same-kind sibling
// (BranchDeleteAt, 9b88c4b5b) made two live, emitting sites read UNRECORDED and
// the gate red for as long as nobody noticed. Four halves, in order:
//
//  1. the vocabulary is derived and covers both kinds, with the same-kind
//     SIBLINGS listed side by side (the record class, not one function name);
//  2. a name that only READS records back (`removalaudit.Records`) is not an
//     emitter — the derivation is not "any exported name";
//  3. renaming the sibling in the emitter's source AND at every production call
//     site leaves the gate GREEN (the drift is gone), while the same renamed
//     vocabulary against UN-renamed callers goes RED (the probe is not
//     vacuously green);
//  4. a site whose only "emit" is a whole-record reader is still UNRECORDED.
func TestRemovalAuditGateDerivesTheEmitterVocabulary(t *testing.T) {
	root := repoRoot(t)
	vocab := removalAuditDeriveVocabulary(t, root)

	// (1) both kinds are covered, siblings together.
	worktree := vocab.emitterFor(removalAuditKindValueWorktree)
	branch := vocab.emitterFor(removalAuditKindValueBranch)
	t.Logf("derived vocabulary: worktree-removal %v | branch-delete %v | no kind written by %v",
		worktree, branch, vocab.opaque)
	for _, want := range []string{"WorktreeRemove", "BranchDelete", "BranchDeleteAt"} {
		if !strings.Contains(strings.Join(append(append([]string{}, worktree...), branch...), " "), want) {
			t.Errorf("the derived vocabulary does not name %s — the derivation is reading something other "+
				"than the emitter's own implementation", want)
		}
	}
	if !removalAuditContains(branch, "BranchDeleteAt") || !removalAuditContains(branch, "BranchDelete") {
		t.Errorf("branch-delete emitters = %v: a same-kind sibling must satisfy the kind alongside its "+
			"twin — this is the exact coupling that made two live sites read UNRECORDED", branch)
	}

	// (2) a reader of records is not an emitter of one.
	for _, kind := range []string{removalAuditKindValueWorktree, removalAuditKindValueBranch} {
		if removalAuditContains(vocab.emitterFor(kind), "Records") {
			t.Errorf("the derivation accepts %s.Records as a %s emitter — Records READS the sink back; "+
				"the gate's vocabulary must be the entry points that WRITE a record", removalAuditPkg, kind)
		}
	}

	// (3) the rename probes. The emitter source is the real file, mutated in
	// memory on both sides of the rename.
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(removalAuditPkgFile)))
	if err != nil {
		t.Fatalf("read %s: %v", removalAuditPkgFile, err)
	}
	const oldName, newName = removalAuditSiblingEntryPoint, removalAuditSiblingRenamed
	mutated, err := removalAuditDeriveVocabularyFromSource(removalAuditPkgFile,
		[]byte(removalAuditRenameEmitter(string(raw), oldName, newName)))
	if err != nil {
		t.Fatalf("derive from the renamed emitter source: %v", err)
	}
	if !removalAuditContains(mutated.emitterFor(removalAuditKindValueBranch), newName) {
		t.Errorf("after renaming %s in the emitter's source, the derived branch-delete emitters are %v — "+
			"the derivation is still reading a NAME instead of the implementation",
			oldName, mutated.emitterFor(removalAuditKindValueBranch))
	}
	if removalAuditContains(mutated.emitterFor(removalAuditKindValueBranch), oldName) {
		t.Errorf("the renamed emitter source still yields %s as an emitter — the derivation read the "+
			"wrong thing", oldName)
	}

	// the baseline: the shipped tree with the shipped vocabulary is clean, and
	// the text-walk agrees with the AST walk the gate runs.
	clean := removalAuditProbeTreeKeys(t, root, vocab, nil)
	if len(clean) != 0 {
		t.Fatalf("the untouched production tree reports unrecorded sites %v — the derivation or the text "+
			"walk is wrong (the AST gate's own verdict is the authority; this probe mirrors it)", clean)
	}
	// RED CONTROL: the renamed vocabulary against callers that were NOT renamed
	// sees the two sites — proof the probe can go red, and the exact shape the
	// pre-fix gate produced by accident.
	dirtyControl := removalAuditProbeTreeKeys(t, root, mutated, nil)
	if len(dirtyControl) == 0 {
		t.Errorf("with the emitter renamed and the CALLERS left alone the probe reports nothing — a probe " +
			"that cannot reproduce the unattributable site proves nothing")
	} else {
		t.Logf("RED control (emitter renamed, callers not): %v", dirtyControl)
	}
	// GREEN: the rename landed on BOTH sides — the real shape of a rename, and
	// the shape that used to red this gate.
	renamed := removalAuditProbeTreeKeys(t, root, mutated, map[string]string{oldName: newName})
	if len(renamed) != 0 {
		t.Errorf("the emitter renamed on both sides still reports %v — a rename must not make a live, "+
			"emitting site read as unattributable; that coupling is the defect this derivation removes", renamed)
	}

	// (4) an EMPTY vocabulary is refused, not skipped (Rule 15): a source
	// declaring an exported entry point that writes no Record yields an error.
	const noRecord = "package removalaudit\n\ntype Record struct{ Kind string }\n\n" +
		"func WorktreeRemove(arm string) { _ = arm }\n"
	if _, derr := removalAuditDeriveVocabularyFromSource("empty-emitter.go", []byte(noRecord)); derr == nil {
		t.Errorf("an emitter source with no Record literal derived a vocabulary with no error — an empty " +
			"vocabulary silently makes every removal site read as unrecorded (Rule 15)")
	} else {
		t.Logf("empty vocabulary refused: %v", derr)
	}

	// (5) reading the sink back is not emitting a record.
	const head = "package probe\n\nimport (\n\t\"github.com/nickhuang99/gopi/internal/removalaudit\"\n\t\"os/exec\"\n)\n\n"
	readerSite := head + `func f(repo, wt string) {
	if _, err := removalaudit.Records("/some/sink"); err != nil {
		return
	}
	_ = exec.Command("git", "-C", repo, "worktree", "remove", wt).Run()
}`
	if got := removalAuditProbeClassify(t, "RED: a record READER does not satisfy a removal site", readerSite, vocab); len(got) != 1 {
		t.Errorf("a site whose only %s call is %s.Records reports %v (want 1 unrecorded site) — the "+
			"vocabulary has been widened to the whole exported surface", removalAuditPkg, removalAuditPkg, got)
	}
}

// The SAME-KIND SIBLING this gate was read as ignoring, and the production file
// its two callers live in. They are named once (Rule 1): the probes below build
// their needles and their expectations from these, and
// TestRemovalAuditGateDerivesTheEmitterVocabulary performs the rename with the
// same pair — a probe that spelled its own copy could drift from the one it
// proves.
const (
	removalAuditSiblingEntryPoint = "BranchDeleteAt"
	removalAuditSiblingRenamed    = "BranchDeleteAtRenamedForProbe"
	// removalAuditSiblingCallerFile is the file the two callers below were
	// RE-POINTED AT on 2026-10-30 (task 01a18eb2). It used to be
	// internal/tools/task_finish_cleanup.go, which commit 58b5618b
	// ("refactor(task): retire the finish-cleanup mechanism (slice 3/4)")
	// DELETED — so both probes in this file were reading a path absent from
	// HEAD and died with `open ...: no such file or directory` before they
	// measured anything.
	//
	// WHY THIS FILE AND NOT A DELETION (the Rule-43 judgement, made from a
	// measurement and not from a filename): goFilesUnder(prodRoots) was walked
	// for every caller of removalaudit.BranchDeleteAt with a branch-delete site
	// in reach, and task_merged_cleanup.go is a file of exactly the retired
	// file's topology — 2 sibling calls, 2 emit statements, 2 branch-delete
	// sites (every other caller holds 1 call / 1 emit, and worktree_sweep.go
	// emits from a VARIABLE arm, so no needle can pick one of its records out).
	// The pairing this probe asserts — one emit statement per branch-delete
	// site, read in source order — therefore still EXISTS in the tree; it moved,
	// it was not retired. The arms below are that file's own.
	removalAuditSiblingCallerFile = "internal/tools/task_merged_cleanup.go"
	// The Arms the two record sites in that file emit with — the needles that
	// pick ONE site's emit statement out of the file, IN SOURCE ORDER. The two
	// spellings are DISTINCT prefixes of one another (neither is a prefix of the
	// other beyond the shared `ArmMergedCleanup` stem, because the next character
	// is `B` for the branch lane and `O` for the orphan lane), which is what lets
	// each needle find its own site.
	removalAuditSiblingArm      = "ArmMergedCleanupBranch,"
	removalAuditSiblingArmForce = "ArmMergedCleanupOrphanBranch,"
)

// removalAuditSiblingEmits names those two arms in ORDER — one entry per
// branch-delete site the caller file is expected to hold, so the pairing below
// is read from a list rather than from two hand-kept singletons. It is a
// package-level var (not a const) because Go has no constant slices.
var removalAuditSiblingEmits = []string{removalAuditSiblingArm, removalAuditSiblingArmForce}

// removalAuditProbeSites classifies a real file's SOURCE (not the on-disk file)
// and returns EVERY removal site it holds, in source order, with its kind — the
// emitting ones included. removalAuditProbeKeys is the same pass filtered to the
// unrecorded ones; this one exists so a probe can DERIVE its expectations from
// the file it measures (which sites exist, in which order) instead of spelling
// their line numbers, and then assert which of them a mutation turned red.
func removalAuditProbeSites(t *testing.T, rel, src string) []removalAuditSite {
	t.Helper()
	root := repoRoot(t)
	consts := removalAuditConsts(t, root, goFilesUnder(t, root, prodRoots, false))
	fset, f := parseSource(t, rel, src)
	var out []removalAuditSite
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		values := removalAuditArgValues(call, consts)
		kind := removalAuditClassify(values)
		if kind == "" {
			return true
		}
		out = append(out, removalAuditSite{rel: rel, line: fset.Position(call.Pos()).Line, kind: kind,
			desc: strings.Join(values, " ")})
		return true
	})
	return out
}

// removalAuditSiteKeyOrdinal returns the `file:line` key of the ordinal-th
// (0-based) site of a kind in src, AS THAT SOURCE SPELLS IT. A probe that
// mutilates a file shifts the line numbers below the mutation, so an expectation
// spelled from the ORIGINAL source would be wrong by exactly the lines the probe
// deleted — and "the key moved" is not "the site is covered". Deriving the
// expected key from the mutated source's own enumeration makes the expectation
// follow the file, while the ORDINAL is what the probe actually asserts (the
// first site, the second site), which no shift can change.
func removalAuditSiteKeyOrdinal(t *testing.T, rel, src, kind string, ordinal int) string {
	t.Helper()
	var keys []string
	for _, s := range removalAuditProbeSites(t, rel, src) {
		if s.kind == kind {
			keys = append(keys, s.key())
		}
	}
	if ordinal >= len(keys) {
		t.Fatalf("%s holds %d %s site(s) %v — this probe needs ordinal %d; re-point it at the file's "+
			"current shape rather than letting it assert a stale topology (Rule 31)", rel, len(keys), kind, keys, ordinal)
	}
	return keys[ordinal]
}

// removalAuditSiteKeysOrdinal returns the `file:line` keys of EVERY site of a
// kind in src, AS THAT SOURCE SPELLS IT — the plural form of
// removalAuditSiteKeyOrdinal, and derived the same way (from the source being
// measured, never from an original copy a mutation shifted).
//
// It exists because a probe that pairs "N records removed" with "the ONE site
// they covered" hard-codes a topology: the file that carries the pairing today
// may hold two sites tomorrow, and the probe would then red on a CORRECT tree.
// A caller that must assert a SET asks for the set the mutated source holds.
func removalAuditSiteKeysOrdinal(t *testing.T, rel, src, kind string) []string {
	t.Helper()
	var keys []string
	for _, s := range removalAuditProbeSites(t, rel, src) {
		if s.kind == kind {
			keys = append(keys, s.key())
		}
	}
	return keys
}

// removalAuditUnrecordedKeys returns the `file:line` keys of the sites the walk
// left UNRECORDED — the verdict the gate fails on, as data, so a probe can
// assert on exactly the sites an operator would be shown.
func removalAuditUnrecordedKeys(sites []removalAuditClassifiedSite) []string {
	var out []string
	for _, s := range sites {
		if s.state == removalAuditStateUnrecorded {
			out = append(out, s.key())
		}
	}
	sort.Strings(out)
	return out
}

// TestRemovalAuditGateSameKindSiblingIsLoadBearing is the PERMANENT both-
// direction red proof for the coupling this gate got wrong: a same-kind sibling
// entry point of the emitter must be recognised, and when it STOPS being
// recognised the gate must go RED on the real sites that call it.
//
// WHY IT IS PERMANENT AND NOT A ONE-SHOT EDIT: the pre-fix gate's false red was
// produced by a real commit (9b88c4b5b) whose effect nobody re-measured; the
// shape that produced it — a gate keyed on one spelling — must be unable to come
// back unnoticed. This test re-runs the REAL walk (removalAuditWalkWith, the
// gate's own pass) over the REAL tree three times in one process, and each half
// is anchored on the source it measures, so it cannot rot into a no-op:
//
//  1. BASELINE — the real vocabulary over the real tree: clean, and the walk
//     classified at least the gate's floor of sites (a green verdict from a walk
//     that sees nothing is worth nothing).
//  2. RED, rename — the emitter's DEFINITION renamed and the call sites left
//     alone: every production caller of the sibling reads UNRECORDED, in the
//     file the row named, as branch-delete sites. This is the reported false red
//     reproduced through the gate's own pass.
//  3. RED, deleted emit — the sibling call DELETED before a real site (each of
//     the two, one at a time): the real predicate reports that site.
//
// The GREEN direction of the same rename (definition AND call sites renamed
// together stays clean) lives in TestRemovalAuditGateDerivesTheEmitterVocabulary,
// which is the one place a rename is applied to caller sources.
func TestRemovalAuditGateSameKindSiblingIsLoadBearing(t *testing.T) {
	root := repoRoot(t)
	vocab := removalAuditDeriveVocabulary(t, root)
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(removalAuditSiblingCallerFile)))
	if err != nil {
		t.Fatalf("read %s: %v", removalAuditSiblingCallerFile, err)
	}
	src := string(raw)

	// The expectation is DERIVED from the source, never restated: a further
	// caller of the sibling — in this file or in ANY production file — is covered
	// by this probe the moment it is written, and a caller that disappears cannot
	// leave it asserting a stale count. (Measured 2026-10-12: the probe's original
	// single-file derivation broke the moment the archive-reclaim arm in
	// internal/tools/archive_ref_reclaim.go became the sibling's third caller —
	// the arm's own record is exactly what that same change added, so the
	// derivation had to follow the tree rather than one file.)
	siblingCall := removalAuditPkg + "." + removalAuditSiblingEntryPoint + "("
	siblingFiles, callers := removalAuditSiblingCallers(t, siblingCall)
	if callers == 0 {
		t.Fatalf("no production file carries a %s call — the same-kind sibling the gate was read as ignoring "+
			"has moved or been replaced; re-point this probe at its current shape rather than letting it "+
			"pass vacuously (Rule 31)", siblingCall)
	}
	// The per-file count the later directions need: this probe mutilates ONE
	// file's source and pairs each of ITS sites with each of ITS emits, so the
	// tree-wide total is the wrong expectation for the topology assertion below.
	fileCallers := strings.Count(src, siblingCall)

	// (1) baseline: the real vocabulary, through the real walk.
	real := removalAuditWalk(t)
	if len(real) < removalAuditSitesFloor {
		t.Fatalf("the walk classified %d removal sites, below the floor of %d — the predicate is reading a "+
			"tree it does not understand, so this probe's baseline proves nothing", len(real), removalAuditSitesFloor)
	}
	if unrecorded := removalAuditUnrecordedKeys(real); len(unrecorded) != 0 {
		t.Fatalf("the untouched tree reports unrecorded sites %v with the derived vocabulary — the baseline "+
			"of this probe must be clean (the gate's verdict is the authority; this is its data form)", unrecorded)
	}

	// (2) RED: the emitter renamed in its DEFINITION only, call sites untouched.
	emitterSrc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(removalAuditPkgFile)))
	if err != nil {
		t.Fatalf("read %s: %v", removalAuditPkgFile, err)
	}
	renamedVocab, err := removalAuditDeriveVocabularyFromSource(removalAuditPkgFile,
		[]byte(removalAuditRenameEmitter(string(emitterSrc), removalAuditSiblingEntryPoint, removalAuditSiblingRenamed)))
	if err != nil {
		t.Fatalf("derive the vocabulary from the renamed emitter: %v", err)
	}
	if removalAuditContains(renamedVocab.emitterFor(removalAuditKindValueBranch), removalAuditSiblingEntryPoint) {
		t.Fatalf("the renamed emitter source still yields %s as a branch-delete emitter — the rename probe "+
			"is not measuring what it claims", removalAuditSiblingEntryPoint)
	}
	// The EXPECTED red is DERIVED from the files that call the renamed entry
	// point: with the sibling renamed, every branch-delete site those files hold
	// which the renamed emitter was covering reads unrecorded. It is NOT the
	// count of emit CALLS any more (that equality held only while one site had
	// exactly one record — since task 01a131bb one delete site carries two
	// records, so counting calls would demand a site that does not exist).
	wantRed := 0
	for _, rel := range siblingFiles {
		body, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		for _, s := range removalAuditProbeSites(t, rel, string(body)) {
			if s.kind == removalAuditKindBranch {
				wantRed++
			}
		}
	}
	if wantRed == 0 {
		t.Fatalf("the caller files %v hold no branch-delete site at all — this direction would pass "+
			"vacuously (Rule 31: re-point the probe at the tree's current shape)", siblingFiles)
	}
	red := removalAuditUnrecordedKeys(removalAuditWalkWith(t, root, renamedVocab))
	if len(red) != wantRed {
		t.Errorf("with the sibling renamed in the emitter's definition alone, the walk reports %d unrecorded "+
			"sites %v, want the %d branch-delete site(s) in %v (the caller files of %s) — the gate is not "+
			"seeing the sites the same-kind sibling emits for (this is exactly the false red of 2026-10-12: "+
			"the records WERE written)", len(red), red, wantRed, siblingFiles, siblingCall)
	}
	for _, key := range red {
		rel, _, ok := strings.Cut(key, ":")
		if !ok || !removalAuditContains(siblingFiles, rel) {
			t.Errorf("unrecorded site %s is not in %v — the renamed sibling should only uncover its own "+
				"callers", key, siblingFiles)
		}
	}
	for _, s := range removalAuditWalkWith(t, root, renamedVocab) {
		if s.state == removalAuditStateUnrecorded && s.kind != removalAuditKindBranch {
			t.Errorf("unrecorded site %s is classified %s, want %s — the renamed sibling's callers are "+
				"branch deletions", s.key(), s.kind, removalAuditKindBranch)
		}
	}
	if len(red) > 0 {
		t.Logf("RED (sibling renamed in the definition only): %v", red)
	}

	// The file must still hold AT LEAST one call per arm this probe mutates —
	// otherwise a needle would be reported missing by the loop below instead of
	// naming the real drift (a record the file no longer writes).
	if fileCallers < len(removalAuditSiblingEmits) {
		t.Fatalf("%s holds %d %s call(s), fewer than the %d record(s) this probe mutates (%v) — re-point "+
			"the pairing at the file's current shape", removalAuditSiblingCallerFile, fileCallers, siblingCall,
			len(removalAuditSiblingEmits), removalAuditSiblingEmits)
	}

	// (3) THE COVERAGE RED PROOF — the delete must be covered BY THE RECORDS, not
	// by its position: with the records this file writes before its branch delete
	// removed from the source, the real predicate must report THAT site.
	//
	// WHAT CHANGED 2026-10-14 (task 01a131bb), measured while re-pointing this
	// probe, and stated here rather than left for the next reader: the chain now
	// deletes with ONE verb, so the file holds ONE branch-delete site while the
	// path still writes TWO records before it (the plain liveness record, then the
	// force standing record). The gate's pairing rule is "one record per SITE",
	// and with ONE site a SINGLE record satisfies it — so deleting EITHER record
	// alone leaves the site green, and this direction can only be measured on the
	// pair. The per-record load-bearing property the 2026-10-12 probe pinned
	// (bug row 01a12be8) was a property of TWO delete sites; it is NOT asserted
	// here, because asserting it would be asserting something the tree does not
	// do. The remaining value of the second record is on the AUDIT stream (a
	// reader can tell which standing the delete had), not on this gate's coverage.
	var records []string
	for _, arm := range removalAuditSiblingEmits {
		records = append(records, removalAuditPkg+"."+removalAuditSiblingEntryPoint+"("+removalAuditPkg+"."+arm)
	}
	bothRemoved := src
	for _, needle := range records {
		var dropped int
		bothRemoved, dropped = removalAuditDropStatement(bothRemoved, needle)
		if dropped == 0 {
			t.Fatalf("%s no longer carries %s — re-point this probe at the file's current shape",
				removalAuditSiblingCallerFile, needle)
		}
	}
	// The site must SURVIVE the mutation: a direction that also removed the
	// delete would report nothing and prove nothing.
	//
	// WHAT CHANGED AGAIN 2026-10-30 (task 01a18eb2), and WHY the expectation is
	// now derived rather than "ordinal 0": this probe was re-pointed at a new
	// file (see removalAuditSiblingCallerFile), and the successor file holds TWO
	// branch-delete sites — one per emit — not the one the retired file held. A
	// single-site expectation here would have asked the predicate to report one
	// site while the source handed it two, i.e. it would have failed on a
	// CORRECT tree and invited the next author to fix it by flooring the count or
	// skipping the direction. The expectation is therefore the set of branch
	// sites the MUTATED source actually holds: the records are what covers each
	// of them, so removing the records must uncover all of them and nothing else.
	wantAll := removalAuditSiteKeysOrdinal(t, removalAuditSiblingCallerFile, bothRemoved, removalAuditKindBranch)
	if len(wantAll) == 0 {
		t.Fatalf("%s holds no branch-delete site after the records were removed — this direction would pass "+
			"vacuously (Rule 31: re-point the probe at the tree's current shape)", removalAuditSiblingCallerFile)
	}
	got := removalAuditProbeKeys(t, removalAuditSiblingCallerFile, bothRemoved, vocab)
	if !removalAuditSameKeys(got, wantAll) {
		t.Errorf("with the %d record(s) %v of %s deleted, the real predicate reports %v, want %v — the "+
			"records are what covers the delete, so removing them must uncover it (a green here means the "+
			"coverage came from somewhere else, or the probe measured a file with no site at all)",
			len(records), removalAuditSiblingEmits, removalAuditSiblingCallerFile, got, wantAll)
	} else {
		t.Logf("RED (all records deleted): %v", got)
	}
}

// removalAuditResidentRedFile is the production file the resident red was
// REPORTED against (bug row 01a12be8's sibling defect: the close-out chain
// switched to the superset entry point while the gate held one spelling), and
// the file this file's red proof runs over. It is the same file the same-kind
// sibling probes use, named once (Rule 13).
const removalAuditResidentRedFile = removalAuditSiblingCallerFile

// TestRemovalAuditGateShapeListIsLoadBearing is the SECOND red proof, and it is
// the one that reproduces the RESIDENT red as a MEASUREMENT rather than an
// assertion. It runs the production predicate over the REAL close-out file
// twice, with the matcher held fixed in every respect but the shape list:
//
//	declared list (derived names, declared call shapes) → 0 unrecorded
//	legacy list  (the ONE spelling the gate used to hold) → the resident red
//
// WHY THIS PROOF IS A DIFFERENT SHAPE FROM ITS NEIGHBOURS. The same-kind-sibling
// probe above mutates the EMITTER's source and re-derives a vocabulary; this one
// leaves the emitter alone and narrows the MATCHER, which is exactly the defect:
// the records were being written on every run while the gate, holding
// `removalaudit.BranchDelete(` alone, read those two sites as UNRECORDED. So this
// is a rename-free, mutation-free measurement of the fix's load-bearing half: if
// a later author narrows the declared list back to one spelling, the FIRST
// assertion reds; if the emitter's close-out arms stop calling a second shape,
// the legacy half stops reproducing the red and the test SAYS SO instead of
// passing vacuously (Rule 15 — a proof that cannot go red proves nothing).
func TestRemovalAuditGateShapeListIsLoadBearing(t *testing.T) {
	root := repoRoot(t)
	vocab := removalAuditDeriveVocabulary(t, root)

	// (0) the declared lists are CALL SHAPES, and the union really is a union:
	// each kind is satisfied by at least one shape, every shape is built from
	// the shape constructor (so it ends in "(" and carries package+selector),
	// and the branch class names MORE THAN ONE entry point — the same-kind
	// siblings the two changes to this file were each half-aware of.
	for _, kind := range []string{removalAuditKindWorktree, removalAuditKindBranch} {
		shapes := removalAuditDeclaredEmitShapes(t, vocab, kind)
		if len(shapes) == 0 {
			t.Fatalf("the declared shape list for %q is empty — a site of that kind can never be "+
				"satisfied, so the gate would red on every one of them", kind)
		}
		for _, s := range shapes {
			if !strings.HasSuffix(s, removalAuditCallOpen) {
				t.Errorf("declared shape %q for %q does not end in %q — a shape that is not a CALL is "+
					"satisfied by a comment, a doc note or an error message naming the emitter, which is "+
					"the whole failure the trailing paren exists to stop", s, kind, removalAuditCallOpen)
			}
			if !strings.HasPrefix(s, removalAuditPkg+removalAuditPkgSep) {
				t.Errorf("declared shape %q for %q is not a qualified call on %s", s, kind, removalAuditPkg)
			}
		}
	}
	branchShapes := removalAuditDeclaredEmitShapes(t, vocab, removalAuditKindBranch)
	if len(branchShapes) < 2 {
		t.Errorf("the branch-delete class declares %d call shape(s) %v — the emitter's close-out sites "+
			"call the superset entry point beside the base one, so a single spelling is the resident red "+
			"returning", len(branchShapes), branchShapes)
	}
	// The union names BOTH siblings, by their own constants.
	for _, want := range []string{removalAuditEmitBranch, removalAuditEmitBranch + removalAuditEmitBranchAt} {
		if !removalAuditContainsShape(branchShapes, removalAuditCallShape(want)) {
			t.Errorf("the declared branch-delete shapes %v do not cover %s — the two changes to this file "+
				"each covered half of the emitter's surface and the union must cover both",
				branchShapes, removalAuditCallShape(want))
		}
	}

	// The file the resident red was reported against, read from disk.
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(removalAuditResidentRedFile)))
	if err != nil {
		t.Fatalf("read %s: %v", removalAuditResidentRedFile, err)
	}
	src := string(raw)

	// (1) DECLARED list → 0 unrecorded. A non-empty result means the sites the
	// close-out chain emits for are not covered by the derived names, i.e. the
	// derivation or the emitter drifted.
	declared := removalAuditProbeKeys(t, removalAuditResidentRedFile, src, vocab)
	if len(declared) != 0 {
		t.Errorf("the DECLARED shape list reports %v unrecorded in %s — the close-out chain records its "+
			"own removals, so a non-empty list here means the derived vocabulary or the emitter drifted",
			declared, removalAuditResidentRedFile)
	}

	// (2) LEGACY list → the resident red, on the SAME tree, measured.
	legacy := removalAuditProbeKeysWith(t, removalAuditResidentRedFile, src, vocab, removalAuditLegacyEmitShapes)
	if len(legacy) == 0 {
		t.Fatalf("the single-spelling (pre-fix) matcher reports EVERYTHING recorded in %s — it can no "+
			"longer reproduce the resident red, so this proof has stopped proving anything (did the "+
			"close-out arms stop calling a second call shape?)", removalAuditResidentRedFile)
	}
	t.Logf("declared list → %v unrecorded | legacy single-spelling → %d unrecorded %v",
		declared, len(legacy), legacy)

	// Every legacy-red key is a branch-delete site of THIS file: the red the
	// narrow matcher produces must be the sites the sibling entry point emits
	// for, never a stray site somewhere else in the tree.
	for _, key := range legacy {
		if !strings.HasPrefix(key, removalAuditResidentRedFile+":") {
			t.Errorf("the legacy matcher's red %s is not in %s — the pre-fix matcher must uncover the "+
				"sites the superset entry point records, and nothing else", key, removalAuditResidentRedFile)
		}
	}
	legacySites := map[string]string{}
	for _, s := range removalAuditProbeSites(t, removalAuditResidentRedFile, src) {
		legacySites[s.key()] = s.kind
	}
	for _, key := range legacy {
		if kind, ok := legacySites[key]; ok && kind != removalAuditKindBranch {
			t.Errorf("the legacy matcher's red %s is classified %s, want %s — the sibling entry point "+
				"records branch deletions", key, kind, removalAuditKindBranch)
		}
	}

	// (3) THE LOAD-BEARING DIRECTION: the declared list is strictly wider than
	// the legacy one on this file, and it is the DERIVED names that close the
	// gap. If the two ever coincide, the union has collapsed back onto one
	// spelling and this proof has lost its subject.
	if len(legacy) <= len(declared) {
		t.Errorf("the legacy single-spelling matcher reports %d unrecorded and the declared union reports "+
			"%d in %s — the declared list must be strictly wider than the pre-fix one, or the union is not "+
			"carrying the coverage this fix exists for", len(legacy), len(declared), removalAuditResidentRedFile)
	}
}

// removalAuditContainsShape reports whether a shape list holds one shape.
func removalAuditContainsShape(shapes []string, want string) bool {
	for _, s := range shapes {
		if s == want {
			return true
		}
	}
	return false
}

// removalAuditContainsKey reports whether a key list holds one `file:line` key.
func removalAuditContainsKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// removalAuditSameKeys reports whether two key lists hold the same `file:line`
// keys, order-insensitive (the walk and the probe sort differently, and the SET
// of uncovered sites is what a verdict is about).
func removalAuditSameKeys(got, want []string) bool {
	g, w := append([]string{}, got...), append([]string{}, want...)
	sort.Strings(g)
	sort.Strings(w)
	if len(g) != len(w) {
		return false
	}
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

// removalAuditContains reports whether a derived emitter list names one entry
// point.
func removalAuditContains(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// The DECIDED-EXCLUSION reclaim arm this census could not see, and the emit
// statement that record lives in. Named once (Rule 1): the probe below derives
// both its needle and its expectation from them.
const (
	// removalAuditArchiveReclaimFile is the file holding the one production
	// deletion spelled with `update-ref -d` instead of `branch -d/-D`.
	removalAuditArchiveReclaimFile = "internal/tools/archive_ref_reclaim.go"
	// removalAuditArchiveReclaimEmit is the needle that picks out the arm's emit
	// STATEMENT (its own `// why:` comment above claims it is what no other site
	// spells, and that was true only of the bare constant). The file now also
	// renders the SAME constant inside the reason sentence it prints
	// (archiveReclaimReasonDetail), which the old needle matched FIRST — the
	// mutation then deleted a sentence half and left the real emit standing, so
	// the probe reported "the census sees nothing". The full call prefix is what
	// is unique to the deletion, so the needle is that prefix (Rule 31).
	removalAuditArchiveReclaimEmit = "removalaudit.BranchDeleteAt(removalaudit.ArmArchiveReclaimBranch"
	// removalAuditArchiveReclaimConst is the emitter const the ARM's evidence
	// sentence and the census both name for the verb pair. It is asserted here
	// because the value the arm RUNS and the value the census MATCHES must be
	// one definition (Rule 14) — a probe that spelled either copy could drift.
	removalAuditArchiveReclaimConst = "RefDeleteVerb"
)

// TestRemovalAuditGateSequelVocabularyIsLoadBearing pins the SECOND deletion
// vocabulary — the half the census was missing, and the reason 11 refs/heads on
// this host were removed on 2026-10-12 without a single `branch-delete` record.
//
// Measured on the real tree, in three parts:
//
//  1. the arm's `update-ref -d` call IS classified as a branch-delete site, so
//     the census counts it and the record it now emits satisfies it;
//  2. removing the ACTUAL emit statement from the file turns that site RED —
//     the fix is load-bearing on the real path, not merely present;
//  3. with the plumbing vocabulary switched OFF (the pre-fix census, driven
//     rather than described), the very tree that reports the site reports
//     NOTHING — the deletion did not read as "unrecorded", it read as absent,
//     which is precisely why the gate stayed green for a year of record-keeping.
func TestRemovalAuditGateSequelVocabularyIsLoadBearing(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(removalAuditArchiveReclaimFile)))
	if err != nil {
		t.Fatalf("read %s: %v", removalAuditArchiveReclaimFile, err)
	}
	src := string(raw)

	// (0) the two spellings are the SAME value, taken from the emitter — an
	// arm that started running a verb the census does not match would make this
	// gate silently blind again, so the coupling is read, not assumed.
	if got := string(removalaudit.RefDeleteVerb); !removalAuditContains(removalAuditSequelVerbs, got) {
		t.Fatalf("the census matches %v but the emitter declares %q — the vocabulary and the spelling "+
			"have drifted apart", removalAuditSequelVerbs, got)
	}
	if string(removalaudit.RefDeleteFlag) != removalAuditSequelDeleteFlag {
		t.Fatalf("the census matches flag %q but the emitter declares %q",
			removalAuditSequelDeleteFlag, string(removalaudit.RefDeleteFlag))
	}

	// (1) the site is IN the census, as a branch-delete site.
	sites := removalAuditProbeSites(t, removalAuditArchiveReclaimFile, src)
	if len(sites) == 0 {
		t.Fatalf("the census classifies NO removal site in %s — the plumbing deletion this probe exists "+
			"for is invisible to it (this is the pre-2026-10-12 state returning)",
			removalAuditArchiveReclaimFile)
	}
	for _, s := range sites {
		if s.kind != removalAuditKindBranch {
			t.Errorf("site %s is classified %q, want %q", s.key(), s.kind, removalAuditKindBranch)
		}
	}
	if n := strings.Count(src, removalAuditArchiveReclaimEmit); n == 0 {
		t.Fatalf("%s carries no %s call — the arm's record has moved or been removed; re-point this probe "+
			"at its current shape rather than letting it pass (Rule 31)",
			removalAuditArchiveReclaimFile, removalAuditArchiveReclaimEmit)
	}

	// (2) RED with the real emit removed.
	vocab := removalAuditDeriveVocabulary(t, root)
	if clean := removalAuditProbeKeys(t, removalAuditArchiveReclaimFile, src, vocab); len(clean) != 0 {
		t.Fatalf("the untouched %s reports unrecorded sites %v — the probe's baseline must be clean",
			removalAuditArchiveReclaimFile, clean)
	}
	mutilated, removed := removalAuditDropStatement(src, removalAuditArchiveReclaimEmit)
	if removed == 0 {
		t.Fatalf("could not mutilate the emit statement for %s", removalAuditArchiveReclaimEmit)
	}
	dirty := removalAuditProbeKeys(t, removalAuditArchiveReclaimFile, mutilated, vocab)
	if len(dirty) == 0 {
		t.Fatalf("with the arm's own emit removed, %s reports NOTHING — the census does not see this "+
			"deletion, so its green verdict on this file is meaningless", removalAuditArchiveReclaimFile)
	}
	t.Logf("RED on the real tree (arm emit removed): %v", dirty)

	// (3) the PRE-FIX census, measured on the same real tree.
	removalAuditSequelVocabOn = false
	blind := removalAuditProbeSites(t, removalAuditArchiveReclaimFile, src)
	removalAuditSequelVocabOn = true
	if len(blind) != 0 {
		t.Errorf("with the plumbing vocabulary OFF the census still classifies %d site(s) in %s — the "+
			"pre-fix blindness this probe claims to reproduce is not reproducible, so the fix has not "+
			"been measured to be load-bearing", len(blind), removalAuditArchiveReclaimFile)
	}
}

// removalAuditSiblingCallers counts the production callers of one emitter
// entry point and reports WHICH files hold them, both read off the tree rather
// than restated (Rule 38: a figure names its generator).
//
// It replaced a single-file `strings.Count` on 2026-10-12, when the
// archive-reclaim arm — whose own removal-audit record is the subject of
// TestRemovalAuditGateSequelVocabularyIsLoadBearing — became the sibling's third
// caller. A probe hard-wired to one file would have failed on a change that made
// the gate STRONGER, and its failure would have read as a real defect.
func removalAuditSiblingCallers(t *testing.T, call string) ([]string, int) {
	t.Helper()
	root := repoRoot(t)
	files := goFilesUnder(t, root, prodRoots, false)
	var holders []string
	total := 0
	for _, rel := range files {
		if rel == removalAuditPkgFile {
			continue // the emitter's own package defines it, never calls it
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if n := strings.Count(string(raw), call); n > 0 {
			holders = append(holders, rel)
			total += n
		}
	}
	sort.Strings(holders)
	return holders, total
}

// ---------------------------------------------------------------------------
// THE SHELL HALF OF THE CENSUS (added 2026-10-15).
//
// THE DEFECT THIS CLOSES — MEASURED, not inferred. `removalAuditWalk` reads
// Go files only (`goFilesUnder(t, root, prodRoots, false)`), so a ref deletion
// written in a tracked SHELL SCRIPT was not "unrecorded" — it was NONEXISTENT to
// the census. scripts/post-merge-cleanup.sh ran
//
//	git branch $DEL_FLAG "$BRANCH"      # DEL_FLAG = -d | -D
//
// and the census said nothing about it, while the gate's own header claims to
// make every removal attributable. Measured on the real tree before the fix:
// the script deleted refs/heads/feat/scriptprobe in a fixture and the private
// sink GOPI_REMOVAL_AUDIT_LOG was never created (0 records). The same shape the
// 2026-10-12 sequel fixed inside Go — a deletion the predicate could not SEE —
// which is why it is fixed by WIDENING THE SCAN, not by a note.
//
// The scan is `git ls-files` (the index), the same reader scratch_gate.go and
// repo_hygiene_gate.go use, for the same reason: the defect is "this line is
// TRACKED and it can delete a ref". A worktree walk would report the operator's
// scratch scripts as if they shipped.
//
// WHAT COUNTS AS A SITE. A tracked file that is EXECUTABLE SHELL (a `#!`
// shebang naming sh/bash, or a `.sh` name) and whose body carries a git ref
// deletion outside a comment:
//
//	git [ -C <path> ] branch  -d | -D | --delete
//	git [ -C <path> ] update-ref -d | --delete
//	git [ -C <path> ] tag     -d
//
// The remedy is not a marker: the site is ROUTED through a lane that already
// emits (measured: `gopi task archive <branch> --reason …` removes the head AND
// writes one `branch:delete` record, arm archive-reclaim-branch, carrying the
// deleted sha). `removal-audit-ok:` is the reviewed escape for a site whose lane
// genuinely cannot emit, printed with its reason like every other release here.
// ---------------------------------------------------------------------------

const (
	// removalAuditShellShebangPrefix opens an interpreter line; the shells this
	// scan recognises are named in removalAuditShellNames (Rule 1b).
	removalAuditShellShebangPrefix = "#!"
	// removalAuditShellSuffix is the second way a shell file announces itself.
	removalAuditShellSuffix = ".sh"
	// removalAuditShellMinFiles is the non-vacuity floor: the scan must see at
	// least this many tracked shell files, or it is reading a tree it does not
	// understand and a green result would be blind (Rule 15 — a scan that found
	// nothing must be distinguishable from one that never ran). MEASURED 2026-10-15:
	// 233 tracked shell files; the floor is set below that and ratchets up.
	removalAuditShellMinFiles = 200
	// removalAuditShellCommentMark is what makes a line a comment, so a
	// documented spelling (a briefing, a usage head) is not a site.
	removalAuditShellCommentMark = "#"
)

// removalAuditShellNames are the interpreters this scan treats as shell: a
// deletion written in one of them runs with the same reach as a Go call.
var removalAuditShellNames = []string{"sh", "bash", "dash", "ksh", "zsh"}

// removalAuditShellDeclarators are the shell keywords that can precede an
// assignment (`readonly DEL_FLAG="-d"`), which the flag-variable resolver strips
// before reading the name.
var removalAuditShellDeclarators = []string{"readonly", "declare", "export", "local", "typeset"}

// removalAuditShellScanOn gates the shell half of the census. It is ON in every
// real run and is flipped OFF only by TestRemovalAuditGateShellScanIsLoadBearing,
// which MEASURES the pre-fix blindness (a fixture holding the script's own
// pre-fix line reads clean with the switch off) on the same fixture the switch-on
// arm flags. It exists for the same reason removalAuditSequelVocabOn does: the
// RED half of a regression proof must be driven, not described.
var removalAuditShellScanOn = true

// removalAuditShellDeletionPatterns are the git argv shapes that delete a ref.
// Each is a prefix of the argv, matched after the optional `-C <path>`.
var removalAuditShellDeletionPatterns = [][]string{
	{"branch", "-d"},
	{"branch", "-D"},
	{"branch", "--delete"},
	{"update-ref", "-d"},
	{"update-ref", "--delete"},
	{"tag", "-d"},
}

// removalAuditShellIsShell reports whether a tracked path is executable shell.
func removalAuditShellIsShell(rel string, body []byte) bool {
	if strings.HasSuffix(rel, removalAuditShellSuffix) {
		return true
	}
	if !strings.HasPrefix(string(body), removalAuditShellShebangPrefix) {
		return false
	}
	first := string(body)
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	for _, name := range removalAuditShellNames {
		if strings.Contains(first, name) {
			return true
		}
	}
	return false
}

// removalAuditShellFlagVars returns the shell variables a file assigns a
// DELETION flag to (`DEL_FLAG="-D"`, `readonly F=-d`). It exists because the
// measured site passed the flag through a variable — `git branch $DEL_FLAG
// "$BRANCH"` — so a scanner that matched only a literal `-d` would have read the
// very line this half exists to catch as clean. Resolution is per-file and
// one-level: an assignment the file itself makes, never a guess at what a
// sourced file might hold.
func removalAuditShellFlagVars(body []byte) map[string]bool {
	out := map[string]bool{}
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, removalAuditShellCommentMark) {
			continue
		}
		// Strip the declarator prefixes that can precede an assignment.
		for _, p := range removalAuditShellDeclarators {
			line = strings.TrimSpace(strings.TrimPrefix(line, p+" "))
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		name := line[:eq]
		if !removalAuditShellIsIdentifier(name) {
			continue
		}
		val := strings.TrimSpace(line[eq+1:])
		if i := strings.IndexAny(val, " \t;"); i >= 0 {
			val = val[:i]
		}
		val = strings.Trim(val, `"'`)
		for _, pat := range removalAuditShellDeletionPatterns {
			if len(pat) == 2 && val == pat[1] {
				out[name] = true
			}
		}
	}
	return out
}

// removalAuditShellIsIdentifier reports whether a token can be a shell variable
// name.
func removalAuditShellIsIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// removalAuditShellRawSites returns `rel:line` for every tracked shell line that
// deletes a ref without the reviewed escape. The line is tokenised on whitespace
// and the git argv is matched as a CONSECUTIVE run, so a quoted prose line in a
// comment does not read as a site and `git -C <path> branch -d <b>` does.
func removalAuditShellRawSites(files map[string][]byte) []string {
	var out []string
	for rel, body := range files {
		if !removalAuditShellIsShell(rel, body) {
			continue
		}
		flagVars := removalAuditShellFlagVars(body)
		for i, raw := range strings.Split(string(body), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, removalAuditShellCommentMark) {
				continue
			}
			if !removalAuditShellLineDeletes(line, flagVars) {
				continue
			}
			key := rel + ":" + itoaStandard(i+1)
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// removalAuditShellLineDeletes reports whether one shell line runs a ref
// deletion: the tokens `git`, an optional `-C <path>` pair, then a verb followed
// by a deletion flag — literal, or a variable the file assigns one to. Quoted
// spans are stripped FIRST, so a spelling inside a string is prose (an echo, a
// usage line) and never a site, while `git branch -D "$BRANCH"` still is one.
func removalAuditShellLineDeletes(line string, flagVars map[string]bool) bool {
	fields := strings.Fields(removalAuditShellStripQuotes(line))
	start := -1
	for i, f := range fields {
		// `git` alone, or the first word being a path to it (`/usr/bin/git`).
		if f == "git" || strings.HasSuffix(f, "/git") {
			start = i + 1
			break
		}
	}
	if start < 0 || start >= len(fields) {
		return false
	}
	if fields[start] == "-C" && start+2 < len(fields) {
		start += 2
	}
	for _, pat := range removalAuditShellDeletionPatterns {
		if start+len(pat) > len(fields) {
			continue
		}
		if fields[start] != pat[0] {
			continue
		}
		if removalAuditShellFlagTokenMatches(fields[start+1], pat[1], flagVars) {
			return true
		}
	}
	return false
}

// removalAuditShellStripQuotes removes every single- and double-quoted span from
// a shell line, so a verb pair inside a string is not mistaken for a command.
func removalAuditShellStripQuotes(line string) string {
	var b strings.Builder
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// removalAuditShellFlagTokenMatches reports whether one token carries the flag a
// pattern wants: the literal, or `$NAME` / `${NAME}` for a name the file assigns
// that flag to.
func removalAuditShellFlagTokenMatches(token, want string, flagVars map[string]bool) bool {
	if token == want {
		return true
	}
	name := strings.TrimPrefix(token, "$")
	name = strings.TrimSuffix(strings.TrimPrefix(name, "{"), "}")
	if name == token {
		return false // not a variable reference at all
	}
	return flagVars[name]
}

// removalAuditShellFilesIn reads every tracked shell file below the repository
// root, through the index (never a worktree walk). A path that cannot be read is
// an error, never a silent skip (Rule 15).
func removalAuditShellFilesIn(t *testing.T, root string) map[string][]byte {
	t.Helper()
	paths, err := trackedPathsIn(root)
	if err != nil {
		t.Fatalf("the shell census cannot read the index: %v", err)
	}
	out := map[string][]byte{}
	for _, rel := range paths {
		body, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			continue // a tracked path deleted in the worktree is not this scan's business
		}
		if removalAuditShellIsShell(rel, body) {
			out[rel] = body
		}
	}
	return out
}

// TestRemovalAuditGateShellRemovalsAreRouted is the shell half of the gate: a
// TRACKED SHELL LINE that deletes a ref and is not routed through an emitting
// lane FAILS, naming the file and the line.
func TestRemovalAuditGateShellRemovalsAreRouted(t *testing.T) {
	if !removalAuditShellScanOn {
		return // the RED proof's switch: exactly the pre-fix census
	}
	files := removalAuditShellFilesIn(t, repoRoot(t))
	if len(files) < removalAuditShellMinFiles {
		t.Fatalf("the shell census read %d tracked shell files, below the floor of %d — it is reading a "+
			"tree it does not understand, so a green result would be meaningless", len(files), removalAuditShellMinFiles)
	}
	raw := removalAuditShellRawSites(files)
	for _, key := range raw {
		t.Errorf("%s runs a git ref deletion in a TRACKED SHELL SCRIPT, outside every emitting lane — the "+
			"census that reads Go files only cannot see this line at all, which is how "+
			"scripts/post-merge-cleanup.sh deleted refs/heads/<b> with no record while this gate stayed green. "+
			"Route it through a lane that emits (`gopi task archive <branch> --reason …` removes the head AND "+
			"writes one branch:delete record), or release the line with `%s <reason>`",
			key, removalAuditGateEscape)
	}
	t.Logf("tracked shell files scanned=%d raw ref deletions=%d (floor %d files)", len(files), len(raw), removalAuditShellMinFiles)
}

// TestRemovalAuditGateShellScanIsLoadBearing is the shell arm's RED/GREEN proof.
// It drives the SAME scanner over the script's own PRE-FIX line and measures both
// verdicts: switch ON flags it, switch OFF reads the identical fixture CLEAN —
// the pre-fix blindness reproduced against the real predicate, rather than
// described in a comment.
func TestRemovalAuditGateShellScanIsLoadBearing(t *testing.T) {
	const fixtureRel = "scripts/fixture-pre-fix.sh"
	preFix := []byte("#!/usr/bin/env bash\n" +
		"# a comment naming git branch -d and git update-ref -d is prose, never a site\n" +
		"if [ \"$FORCE\" -eq 1 ]; then\n" +
		"    DEL_FLAG=\"-D\"\n" +
		"else\n" +
		"    DEL_FLAG=\"-d\"\n" +
		"fi\n" +
		"if git branch $DEL_FLAG \"$BRANCH\" >/dev/null 2>&1; then\n" +
		"    echo deleted\n" +
		"fi\n")
	files := map[string][]byte{fixtureRel: preFix}

	on := removalAuditShellRawSites(files)
	if len(on) != 1 {
		t.Fatalf("with the scan ON the pre-fix line must read as ONE site, got %v", on)
	}
	off := preFixCensusVerdict(files)
	if len(off) != 0 {
		t.Fatalf("with the scan OFF (the pre-fix census) the identical fixture must read CLEAN — that is the "+
			"measured blindness this arm exists to prevent; got %v", off)
	}

	// The scanner is a PREDICATE, not a regex: a routed line and a commented
	// spelling are not sites, and `git -C <path> update-ref -d` is.
	must := func(src string, want int) {
		t.Helper()
		got := removalAuditShellRawSites(map[string][]byte{"s.sh": []byte("#!/bin/bash\n" + src + "\n")})
		if len(got) != want {
			t.Errorf("shell line %q: want %d site(s), got %v", src, want, got)
		}
	}
	must(`"${GOPI_BIN}" task archive "$BRANCH" --reason superseded --detail d --override-completed`, 0)
	must(`echo "run git branch -d to clean up"`, 0)
	must(`git -C /repo update-ref -d refs/heads/x`, 1)
	must(`git -C /repo branch -D "$BRANCH"`, 1)
}

// preFixCensusVerdict is the pre-fix census's verdict on a file set: it holds the
// scan switch OFF, which is exactly what the tree looked like before the shell
// half existed. It restores the switch, so a failure cannot leave the gate blind.
func preFixCensusVerdict(files map[string][]byte) []string {
	prev := removalAuditShellScanOn
	removalAuditShellScanOn = false
	defer func() { removalAuditShellScanOn = prev }()
	if !removalAuditShellScanOn {
		return nil // the pre-fix census read Go files and nothing else
	}
	return removalAuditShellRawSites(files)
}
