package scratch

// worktree_root_residue.go — the CENSUS of a worktree root: every direct child
// of a directory this system shares with real worktrees is CLASSIFIED, so the
// residue families that no sweep can reach are NAMED instead of invisible.
//
// THE MEASURED DEFECT (2026-10-07, /home/nick/workspace): the worktree root held
// 95 `gopi-*` directories plus 98 children in other naming schemes. Exactly
// SEVEN of them were `git worktree list` registrations. The rest fell into
// families that NO sweep covers —
//
//	44  dead test-leak stubs (`gopi-<32 hex>`, gitdir under a /dev/shm suite run)
//	    → covered by the STUB arm (worktree_stub.go); it simply had never been run
//	47  hand-made evidence dirs (`gopi-attrib-evidence`, `gopi-chat-ui-debug-
//	    evidence`, `gopi-scan-tip-evid`, …), Rule-34 sandbox probes (`sbxcheck-*`,
//	    `sbxref-*`, `sbxfinal-*`), mktemp work dirs (`tmp-m2-6Zgx`, `vocab.tNMt`)
//	 ~100 loose files
//
// WHY A CENSUS AND NOT A SECOND DELETING PREDICATE: the root is a directory the
// OPERATOR owns and shares with real worktrees, and most of that residue is
// EVIDENCE a report cites by name (the devdocs above cite them). "Delete every
// directory that is not a worktree" is one mistake away from destroying work, and
// the evidence is not this sweep's to weigh. So the arm's DESTRUCTIVE half is the
// EXISTING stub predicate, unchanged, and everything else is REPORTED with its
// family — the operator's decision, made with the list in front of them.
//
// WHY THIS IS NOT A NEW DEPENDENCY (Rule 35): a function in the package whose
// sweep already rides the existing `gopi tool scratch-gc` face and the existing
// `gopi tool scratch-gc` one-shot. No daemon, no cron line, no state file.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// RootChildClass is the verdict about ONE direct child of a worktree root. The
// three classes are the whole decision surface, and the CLASS — never the path —
// is what the arm's removal half keys on.
type RootChildClass int

const (
	// RootChildLiveWorktree: the child is a WORKING TREE — its `.git` names a
	// gitdir that EXISTS (a linked worktree's registration is live, a main
	// checkout's `.git` is a directory). NEVER a candidate at any age.
	RootChildLiveWorktree RootChildClass = iota
	// RootChildStub: the child is collectable by the EXISTING stub predicate —
	// a dead pointer to a gitdir under a finished suite run, holding nothing but
	// a fixture checkout. This is the ONLY class the arm can remove.
	RootChildStub
	// RootChildResidue: the child carries an ad-hoc name this system's own
	// tooling mints (a generated suffix) and is NOT a working tree — reported,
	// never removed (the evidence classes above).
	RootChildResidue
	// RootChildForeign: out of scope at any age — a name this sweep did not
	// create (a home directory a person made, a project checkout, `homm3-m1`).
	RootChildForeign
)

// RootChildPlan is one child's verdict, in the reporting form the CLI prints and
// the tests assert on.
type RootChildPlan struct {
	// Path is the child's full path.
	Path string
	// Name is its base name.
	Name string
	// Class is the verdict.
	Class RootChildClass
	// Reason says WHY — in every class, including the live and foreign ones, so
	// an operator reading a census sees evidence rather than a taxonomy.
	Reason string
	// Removable is true only for RootChildStub: the class the arm's `--yes`
	// half may take. It is a SEPARATE field from Class so the removal half
	// cannot be widened by adding a class.
	Removable bool
}

// RootChildClassLabel is the stable word the census prints for a class. It is a
// constant per class (Rule 1b) because the CLI line, the tests and an operator's
// grep all match on it.
func RootChildClassLabel(c RootChildClass) string {
	switch c {
	case RootChildLiveWorktree:
		return "worktree"
	case RootChildStub:
		return "stub"
	case RootChildResidue:
		return "residue"
	default:
		return "foreign"
	}
}

// PlanWorktreeRootCensus classifies every direct child of one worktree root.
// It deletes nothing, creates nothing, and a missing root is reported EMPTY
// (a read-only pass must not be able to bring a tree into existence).
//
// ORDER IS CHEAPEST FIRST and the worktree test comes FIRST, because it is the
// one verdict that must be reached even for a candidate the later conditions
// would have collected: "this is a live worktree" is the answer an operator can
// act on, and it is the answer that must never be conditional on anything else.
func PlanWorktreeRootCensus(root string) ([]RootChildPlan, error) {
	if err := WorktreeStubRootRefusal(root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("worktree-root census: read %s: %w", root, err)
	}
	if len(entries) > ScanLimit {
		return nil, fmt.Errorf("worktree-root census: %s holds more than %d entries — refusing to classify an unbounded tree", root, ScanLimit)
	}
	// ONE /proc pass for the whole census (the LiveHolders rationale).
	holders := ScanHolders()
	out := make([]RootChildPlan, 0, len(entries))
	for _, e := range entries {
		out = append(out, classifyRootChild(filepath.Join(root, e.Name()), e.Name(), holders))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// classifyRootChild applies the classification to ONE entry.
func classifyRootChild(path, name string, holders HolderScan) RootChildPlan {
	plan := RootChildPlan{Path: path, Name: name}

	fi, err := os.Lstat(path)
	if err != nil {
		plan.Class = RootChildForeign
		plan.Reason = fmt.Sprintf("unmeasurable (%v)", err)
		return plan
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		// A FILE or a SYMLINK: never a candidate for ANY arm here. The loose
		// files of the measured root (logs, transcripts, one-off scripts) land
		// in this bucket, and deleting them is not this arm's business.
		plan.Class = RootChildForeign
		plan.Reason = RootChildFileReason
		return plan
	}

	// ① THE WORKING-TREE QUESTION FIRST — a `.git` DIRECTORY, or a `.git` FILE
	//    whose gitdir target still EXISTS, is a live registration.
	if why := rootChildWorktreeEvidence(path); why != "" {
		plan.Class = RootChildLiveWorktree
		plan.Reason = why
		return plan
	}

	// ② THE STUB PREDICATE — the arm's ONLY removable class, reused whole: the
	//    name family, the dead pointer, its suite-run provenance, the fixture
	//    content bound and the liveness veto are all judgeWorktreeStub's own
	//    conditions, so a widening here cannot happen by accident.
	if WorktreeStubNameMatches(name) {
		stub := judgeWorktreeStub(path, holders)
		if !stub.Keep {
			plan.Class = RootChildStub
			plan.Removable = true
			plan.Reason = stub.Reason
			return plan
		}
		// A name in the stub family that the predicate KEEPS: report it as
		// residue with the predicate's own reason, so the census never loses the
		// sentence that decided it.
		plan.Class = RootChildResidue
		plan.Reason = stub.Reason
		return plan
	}

	// ③ AD-HOC RESIDUE — a name this system's own tooling mints, and NOT a
	//    working tree. Reported, never removed.
	if rootChildAdHocName(name) {
		plan.Class = RootChildResidue
		plan.Reason = RootChildResidueReason
		return plan
	}
	plan.Class = RootChildForeign
	plan.Reason = RootChildForeignReason
	return plan
}

// The census's three evidence sentences, spelled once (Rule 1b): the CLI line,
// the tests and an operator's grep all read them.
const (
	// RootChildFileReason: a flat file or a symlink directly in a worktree root.
	RootChildFileReason = "a flat file or a symlink, not a directory — directories are the only shape this census judges"
	// RootChildResidueReason: an ad-hoc name of this system's own making that is
	// not a working tree. REPORTED, never removed: these are the evidence
	// directories reports cite by name.
	RootChildResidueReason = "an ad-hoc name this system's own tooling mints and it is not a working tree — REPORTED, never removed (evidence a report may cite by name)"
	// RootChildForeignReason: a name this system did not create.
	RootChildForeignReason = "a name this system's tooling does not mint — out of scope at any age"
	// RootChildLiveWorktreeReasonDir: a `.git` DIRECTORY means a repository or a
	// main checkout.
	RootChildLiveWorktreeReasonDir = "a .git DIRECTORY — a repository or a main checkout; never a candidate at any age"
	// RootChildLiveWorktreeReasonPointer: a `.git` pointer whose gitdir EXISTS —
	// a LIVE linked-worktree registration.
	RootChildLiveWorktreeReasonPointer = "a .git pointer whose gitdir target still EXISTS — a live worktree registration; never a candidate at any age"
	// RootChildLiveWorktreeReasonRepo: `git rev-parse --is-inside-work-tree`
	// answered yes without a readable `.git` at this level.
	RootChildLiveWorktreeReasonRepo = "git reports a work tree here (the checkout's .git is not a direct child) — never a candidate at any age"
)

// rootChildWorktreeEvidence answers the working-tree question for one child, and
// returns the SENTENCE that decided it ("" = no positive worktree evidence, so
// the caller judges by the classes below).
//
// The two positive answers are separate because the evidence is: a `.git`
// DIRECTORY (a repository or a main checkout) and a `.git` FILE whose target
// EXISTS (a live linked registration). An unreadable or malformed pointer answers
// NOTHING — "I could not tell" is not "it is not a worktree", so the caller falls
// through to the classes below, which never delete (Rule 15b: an unknown is never
// a licence to remove).
func rootChildWorktreeEvidence(path string) string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return ""
	}
	kind, found := entryKind(entries, WorktreeStubGitPointerName)
	if !found {
		return ""
	}
	switch kind {
	case entryKindDir:
		return RootChildLiveWorktreeReasonDir
	case entryKindFile:
		target, terr := worktreeStubGitDirTarget(filepath.Join(path, WorktreeStubGitPointerName))
		if terr != nil {
			return ""
		}
		if _, serr := os.Lstat(target); serr == nil {
			return RootChildLiveWorktreeReasonPointer
		}
		return ""
	default:
		return ""
	}
}

// rootChildAdHocName reports whether a directory NAME is one this system's own
// naming minted for throwaway work.
//
// THREE POSITIVE FAMILIES, and nothing else:
//
//   - the WORKTREE NAMING PREFIX (`gopi-`, the ground family): measured, every
//     one of the root's ad-hoc directories carried it (`gopi-attrib-evidence`,
//     `gopi-bin-01a0d9ab`, `gopi-scan-tip-evid`, `gopi-session-56507166`). This
//     is also why the class is honest about what it says: the prefix marks a
//     directory this system NAMED, not one it should delete — this host's own
//     declared roots (`gopi-sandbox`, `gopi-test-scratch`) carry it too and are
//     reported here for exactly that reason;
//   - the base-residue families this repository's own commands produce
//     (`gocache-<generated>`, `probe-<generated>`) — read through
//     IsBaseResidueName, the ONE predicate that already encodes "generated
//     token, not a word a person chose", so this census cannot drift from the
//     memory-base sweep's own idea of "ours";
//   - a RANDOM SUFFIX (os.MkdirTemp's own shape: `tmp-m2-6Zgx`, `vocab.tNMt`,
//     `sbxcheck-cfvY`) — see hasRandomSuffix, which is the one judgement call
//     here and is pinned in both directions by its own test.
//
// A name failing all three is FOREIGN. That is deliberate: the measured root's
// own `homm3-m1`, `20210804`, `backup`, `annex-fixture`, `fog` are exactly the
// names a person chose, and calling them this system's residue would be the first
// step toward treating them as ours to remove.
func rootChildAdHocName(name string) bool {
	if strings.HasPrefix(name, WorktreeStubNamePrefix) {
		return true
	}
	if IsBaseResidueName(name) {
		return true
	}
	return hasRandomSuffix(name)
}

// randomSuffixTokenMinLen / MaxLen bound the tail os.MkdirTemp can mint that this
// predicate will treat as generated. Measured tails on this host run from 4
// characters (`-t <prefix>-XXXX` spellings: `aDgf`, `cfvY`, `CUaj`, `MU5p`) to 6
// (the tool's own default). Below 4 the collision risk with real words is what
// makes the test worthless; above 12 a tail is a word, not a nonce — both bounds
// are what keep the rule from swallowing names a person typed.
const (
	randomSuffixTokenMinLen = 4
	randomSuffixTokenMaxLen = 12
)

// hasRandomSuffix reports whether a name ends in a `-`/`.`-delimited token that
// looks like os.MkdirTemp's random tail.
//
// THE SIGNAL IS CASE AND DIGITS, and it was chosen against the real names on this
// host rather than from first principles: the tool draws from base62
// (A-Za-z0-9), so a tail of ≥4 characters is MIXED-CASE almost surely, while a
// word a person types is all-lowercase or all-uppercase — `tNMt`, `cfvY`, `BMzA`
// against `red`, `flag`, `backup`, `fixture`. A tail that is single-case but
// carries a DIGIT counts too (`qY54`'s sibling shapes, `20210804` does not, as it
// has no letter at all).
//
// The conservative direction is the one that matters: a MISS classifies the
// directory as FOREIGN, which reports it and never removes it. This predicate
// only decides which of two never-removable classes a child lands in, so its
// failure mode is a missing label, never a deletion.
func hasRandomSuffix(name string) bool {
	cut := strings.LastIndexAny(name, "-.")
	if cut <= 0 || cut == len(name)-1 {
		return false
	}
	token := name[cut+1:]
	if len(token) < randomSuffixTokenMinLen || len(token) > randomSuffixTokenMaxLen {
		return false
	}
	var lower, upper, digits int
	for _, r := range token {
		switch {
		case unicode.IsLower(r):
			lower++
		case unicode.IsUpper(r):
			upper++
		case unicode.IsDigit(r):
			digits++
		default:
			return false // a dot, a plus, an underscore: not this alphabet
		}
	}
	if lower+upper == 0 {
		return false // all digits: a counter (`20210804`), not a nonce
	}
	return (lower > 0 && upper > 0) || digits > 0
}
