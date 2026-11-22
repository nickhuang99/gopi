package scratch

// worktree_stub.go — the WORKTREE-STUB arm: the ONE reclaim path for the
// degenerate directories a TEST-PROCESS worktree leak leaves in a REAL worktree
// root.
//
// THE RESIDUE, MEASURED (2026-10-01, epic test-worktree-root-leak): 174
// directories named `gopi-<32 hex>` sat directly in the operator's real
// ~/workspace, each holding EXACTLY ONE file — a `.git` POINTER whose target was
//
//	/dev/shm/suite-agent-run-<N>/TestReframe…/001/reframe-main/.git/worktrees/gopi-<hex>
//
// i.e. a per-run suite directory that died with the /dev/shm test run when the
// run ended. The producer was PRODUCTION code run from a test:
// `tools.TaskRunGroundFor` placed a row's worktree under the repo's resolved
// worktree_root, and for a fixture repo that declares none that root was the
// operator's default workspace root. The ROOT CAUSE half is fixed in
// internal/worktreeroots/scratch_pin.go (a test process no longer resolves the
// operator's real root at all); THIS file is the cleanup half, for the residue
// already on disk.
//
// WHY A NEW PREDICATE AND NOT AN EXISTING SWEEP: every sweep in this package is
// namespace-limited by construction, which is exactly why none of them sees this
// family —
//
//   - SweepHome walks the scratch HOME (<workspace>/gopi-test-scratch), a
//     different tree; these stubs sit in the workspace root itself;
//   - the base-residue arm walks a memory base;
//   - the sandbox arm requires the `sandbox-` name marker;
//   - the DECLARED-TREES arm walks the build-cache/go-tmp/raw-log trees.
//
// The stubs are not scratch: they live in a PERSISTENT root the operator owns.
// They are not worktrees either — `git worktree list` of the repository that
// would have owned them says nothing, because the repository they were registered
// on lived in the deleted run directory. So they are invisible to both families,
// which is how they accumulated for weeks while every existing sweep reported
// success.
//
// WHY THIS IS NOT A NEW DEPENDENCY (Rule 35): it is a PREDICATE inside the sweep
// package that already rides the existing `gopi tool scratch-gc` face and the existing
// `gopi tool scratch-gc` one-shot. No daemon, no cron line, no script, no state file.
// The operator-facing door is an ARM of that verb, exactly like
// base/trees/sandboxes/deps.
//
// WHY THE PREDICATE IS THIS NARROW: the root it sweeps is a REAL directory the
// operator shares with their own worktrees, so "delete by location" is one
// mistake away from destroying real work. Every condition below is a POSITIVE
// proof of degeneracy, and every unknown is a KEEP:
//
//	name            `gopi-` + a GENERATED stem (the ground-naming family)
//	kind            a real directory (never a symlink, never a file)
//	content         a FLAT set of small regular files, one of which is the `.git`
//	                pointer FILE — the measured leftover is the pointer plus the
//	                fixture's single 8-byte README.md; a subdirectory, a symlink,
//	                or a byte total past the fixture bound is a working tree
//	                (a real worktree has a `.git` DIRECTORY, so this condition and
//	                the one below overlap deliberately)
//	pointer         its content is a single `gitdir: <absolute path>` line
//	target          that path DOES NOT EXIST (a live registration always does)
//	provenance      the dead target lies under a suite RUN directory
//	                (`suite-<name>-run-<generated tail>` — the shape this
//	                repository's own suite entry point mints), which is what
//	                separates a test leak from an operator's own broken worktree
//	                and from an `rm -rf` of an unrelated repository
//	liveness        NO live process stands in it, holds it open, or maps a file
//	                from it (the shared /proc probe, batched)
//
// A directory matching ALL EIGHT is a pointer to nothing: removing it destroys no
// content that exists anywhere. A directory failing ANY one is kept and the
// reason is REPORTED (Rule 15 — an unknown is a KEEP, and the keep is explained).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// WorktreeStubNamePrefix is the leading half of a leaked stub's name: the
	// task-ground naming family (`gopi-<row stem>`; internal/tools'
	// TaskGroundDirPrefix). It is a SECOND definition of that value by necessity —
	// internal/tools imports this package (the sweep is driven from the tools
	// layer), so this leaf cannot import it back — and the two are tied together
	// by a test in the owning package, which is the rule for a value that cannot
	// be hoisted (Rule 14).
	WorktreeStubNamePrefix = "gopi-"
	// WorktreeStubGitPointerName is the single entry a stub holds: git's
	// linked-worktree pointer file.
	WorktreeStubGitPointerName = ".git"
	// WorktreeStubGitDirKey is that pointer's line key (git's own `gitdir: `
	// prefix), spelled once (Rule 1b).
	WorktreeStubGitDirKey = "gitdir: "
)

const (
	// StubLeftoverMaxFiles and StubLeftoverMaxBytes bound what a LEFTOVER may
	// hold, and the measurement is why they are needed at all: the brief described
	// the residue as "a directory holding only a `.git` pointer", but the operator's
	// real tree (measured 2026-10-01, 183 candidates) holds the pointer PLUS the
	// fixture repo's one checked-out file — an 8-byte README.md, i.e. the worktree
	// checkout of a fixture with a single committed file. So the content condition
	// cannot be "nothing but the pointer": it is "nothing but a FLAT set of small
	// regular files", which is what a fixture checkout is and what a real working
	// tree is not (a real tree has subdirectories, or is far larger).
	//
	// The two bounds are deliberately far above the measured shape (1 extra file,
	// 8 bytes) and far below a real checkout, and both the file count and the byte
	// total are PRINTED per candidate, so an operator judging a dry run sees the
	// evidence rather than a verdict.
	StubLeftoverMaxFiles = 8
	StubLeftoverMaxBytes = 64 * 1024
)

// WorktreeStubKeep* name WHY a candidate was kept. They are constants (Rule 1b)
// because the operator-facing dry run, the sweep's own log and the tests all
// match on them (the SandboxKeep* precedent): a reason typed twice drifts into a
// reason nobody can grep for.
const (
	WorktreeStubKeepNotOurs      = "name is not this system's worktree-naming family"
	WorktreeStubKeepNotDir       = "not a real directory (a symlink or a file is never a candidate)"
	WorktreeStubKeepHasContent   = "holds a subdirectory, a symlink or a non-regular entry — that is a working tree, not a fixture leftover"
	WorktreeStubKeepTooBig       = "the leftover is larger than a fixture checkout — refusing to judge it by size"
	WorktreeStubKeepPointersDir  = ".git is a DIRECTORY — this is a repository, not a linked worktree"
	WorktreeStubKeepPointerShape = "the .git pointer is unreadable, is not a `gitdir: <path>` line, or names a relative path"
	WorktreeStubKeepTargetLive   = "the gitdir target still EXISTS — the registration is live"
	WorktreeStubKeepNotFromSuite = "the gitdir target does not lie under a suite RUN directory — this is not a test leak, and the operator's own layout is not this sweep's business"
	WorktreeStubKeepHeld         = "a live process holds it (working directory, open file, or mapped file)"
)

// StubNoAgeWindow is what this arm's log line puts in the shared `stale=` field,
// and it is the word "none" rather than a zero duration ON PURPOSE: the stub
// predicate has NO age axis (a stub is definitively degenerate — its only content
// points at a path that no longer exists, inside a finished suite run — so no
// window is applied and nothing is waited out). A `stale=0s` would read as "the
// window was zero", i.e. as a threshold an operator could reason about; the word
// says what is true. It is a constant (Rule 1b) because the CLI line and any test
// that reads the line must agree on the spelling.
const StubNoAgeWindow = "none"

// worktreeStubNameRe matches the leaked-stub name family: WorktreeStubNamePrefix
// followed by a GENERATED stem — the alphanumeric body a row-derived ground
// carries (all 174 measured stubs were `gopi-` + 32 lowercase hex characters,
// i.e. a row id with its dashes removed). A word ("gopi-scratch"), a short
// counter ("gopi-1") and a hand-chosen name all fall outside it, so the
// neighbourhood's own directories are out of reach by construction.
var worktreeStubNameRe = regexp.MustCompile(`^` + WorktreeStubNamePrefix + `[0-9a-zA-Z]{8,64}$`)

// WorktreeStubPlan is ONE candidate's verdict: the REPORTING form of the very
// predicate the sweep applies, so a dry run cannot promise a different set than
// the deletion performs (the ScratchPlanEntry/SandboxPlan invariant).
type WorktreeStubPlan struct {
	// Path is the candidate directory.
	Path string
	// Keep is true when it must survive this sweep.
	Keep bool
	// Reason says why it is kept — or, when Keep is false, the evidence that
	// authorised collecting it (the missing gitdir target).
	Reason string
	// Bytes is the candidate's MEASURED size, filled for EVERY verdict (kept
	// ones included) by the same walk the live sweep uses (DirBytes).
	//
	// WHY IT LIVES ON THE PLAN (measured 2026-11-17, clean-room re-verification
	// of the sandbox arm's fix f2d570a3): without it the arm's DRY RUN had no
	// figure to publish and printed `bytes=0` for a plan that held real bytes —
	// ONE collectable stub of 3,124 bytes printed
	//
	//	scratch-gc role=cli-stubs home=… plan=1 stale=none disabled=false bytes=0 dry-run=true
	//
	// while the identical fixture's live sweep measured `removed=1 … bytes=3124`.
	// The -h page defines the field for the whole verb — "bytes=: what the plan
	// WOULD reclaim (or, without --dry-run, did)" — so a 0 there was an
	// UNMEASURED value wearing a measurement's name (Rule 15), the exact defect
	// f2d570a3 closed on the sandbox arm. This field is that arm's
	// SandboxPlan.Bytes, in this arm's vocabulary (Rule 14: same discipline, not
	// a second one).
	Bytes int64
}

// WorktreeStubNameMatches reports whether a directory name is in the leaked-stub
// family the sweep collects. It is exported for the PRODUCER's side — the tools
// layer, which names the grounds and cannot be imported here — so the tie between
// "the name we mint" and "the name we reclaim" is asserted in ONE place instead of
// being re-derived from a regex in a test (Rule 14).
func WorktreeStubNameMatches(name string) bool { return worktreeStubNameRe.MatchString(name) }

// WorktreeStubRootRefusal — the ONE refusal for a root this arm must not sweep.
//
// The arm deletes inside a directory the OPERATOR owns, so this refusal is what
// keeps a mistyped configuration from turning a cleanup into a catastrophe: the
// filesystem root (every suite stub is under it), the home directory and the gopi
// PROFILE (ledger + plugin tree) are refused by name; so is any path that is
// itself a git work tree (a checkout's root is somebody's working tree, and a
// stale registration INSIDE it belongs to that repository's own `git worktree
// prune`).
func WorktreeStubRootRefusal(root string) error {
	clean := filepath.Clean(strings.TrimSpace(root))
	switch clean {
	case "", ".", string(os.PathSeparator):
		return fmt.Errorf("worktree-stub sweep: refusing to sweep %q — that is not a worktree root", root)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && filepath.Clean(home) == clean {
		return fmt.Errorf("worktree-stub sweep: refusing to sweep %q — that is the home directory itself, not a worktree root", clean)
	}
	if profile := gopiProfileDir(); profile != "" && filepath.Clean(profile) == clean {
		return fmt.Errorf("worktree-stub sweep: refusing to sweep %q — that is the gopi profile", clean)
	}
	if _, err := os.Stat(filepath.Join(clean, WorktreeStubGitPointerName)); err == nil {
		return fmt.Errorf("worktree-stub sweep: refusing to sweep %q — it is a git work tree, whose own `git worktree prune` owns that repository's stale registrations", clean)
	}
	return nil
}

// PlanWorktreeStubs reports, deleting NOTHING, every direct child of root the
// sweep would collect, each with the evidence for its verdict. A missing root is
// reported EMPTY, never created (a read-only pass must not be able to bring a
// tree into existence).
func PlanWorktreeStubs(root string) ([]WorktreeStubPlan, error) {
	if err := WorktreeStubRootRefusal(root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("worktree-stub sweep: read %s: %w", root, err)
	}
	if len(entries) > ScanLimit {
		return nil, fmt.Errorf("worktree-stub sweep: %s holds more than %d entries — refusing to sweep an unbounded tree", root, ScanLimit)
	}
	// ONE /proc pass for the whole arm (the LiveHolders rationale: a per-entry
	// probe costs a process-table walk per candidate).
	holders := ScanHolders()
	out := make([]WorktreeStubPlan, 0, 8)
	for _, e := range entries {
		if !worktreeStubNameRe.MatchString(e.Name()) {
			continue // not this system's worktree-naming family: out of scope at any age
		}
		plan := judgeWorktreeStub(filepath.Join(root, e.Name()), holders)
		// The size is measured for EVERY verdict, kept ones included: the plan
		// publishes two figures (what it would reclaim, what it would leave), and
		// a kept entry's bytes are half of that account. Measured through the SAME
		// walk the live sweep uses (DirBytes), so `bytes=` on a dry run and on a
		// real run are the same number for the same tree.
		plan.Bytes = DirBytes(plan.Path)
		out = append(out, plan)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// WorktreeStubTally is what a plan adds up to: the removable and kept COUNTS and
// their measured BYTES. It is a pure function of the verdicts, so the CLI's plan
// line, the CLI's live line and the roots arm cannot add the same plan up
// differently (Rule 14 — the SandboxTally precedent, in this arm's vocabulary).
type WorktreeStubTally struct {
	// Removable / RemovableBytes are the candidates this pass WOULD collect and
	// their measured size — the figures `plan=`/`bytes=` publish on a dry run.
	Removable      int
	RemovableBytes int64
	// Kept / KeptBytes are the candidates the predicate protects. They are a
	// real disk cost that no pass of this arm will ever recover, so a line that
	// published only the removable half would understate what the root holds.
	Kept      int
	KeptBytes int64
}

// TallyWorktreeStubs adds up a plan. Pure, so every face quotes the same arithmetic.
func TallyWorktreeStubs(plans []WorktreeStubPlan) WorktreeStubTally {
	var t WorktreeStubTally
	for _, p := range plans {
		if p.Keep {
			t.Kept++
			t.KeptBytes += p.Bytes
			continue
		}
		t.Removable++
		t.RemovableBytes += p.Bytes
	}
	return t
}

// SweepWorktreeStubs removes the candidates PlanWorktreeStubs names and returns
// how many went, plus the bytes they held. It applies the SAME predicate, so the
// set an operator vetted in a dry run is the set that is deleted.
//
// A removal that fails is an ERROR, never a silent skip (Rule 15b): the operator
// asked for these directories to go, and a partial sweep must not report success.
func SweepWorktreeStubs(root string) (removed int, bytes int64, err error) {
	plans, err := PlanWorktreeStubs(root)
	if err != nil {
		return 0, 0, err
	}
	for _, p := range plans {
		if p.Keep {
			continue
		}
		bytes += DirBytes(p.Path)
		if rerr := os.RemoveAll(p.Path); rerr != nil {
			return removed, bytes, fmt.Errorf("worktree-stub sweep: remove %s: %w", p.Path, rerr)
		}
		removed++
	}
	return removed, bytes, nil
}

// judgeWorktreeStub applies the eight conditions to ONE candidate, cheapest
// first, and returns the verdict WITH its reason.
func judgeWorktreeStub(path string, holders HolderScan) WorktreeStubPlan {
	keep := WorktreeStubPlan{Path: path, Keep: true}

	fi, err := os.Lstat(path)
	if err != nil {
		keep.Reason = fmt.Sprintf("unmeasurable (%v)", err)
		return keep
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		keep.Reason = WorktreeStubKeepNotDir
		return keep
	}

	// THE POINTER — its presence, its type, and the target it names — is decided
	// BEFORE the content heuristic, and the order is not cosmetic: a LIVE
	// registration (the gitdir target exists) must be reported as exactly that,
	// because "this is a live worktree" is the answer an operator can act on,
	// while "holds a subdirectory" is the same verdict for a weaker reason. The
	// operator's own three live worktrees are the concrete case: they are kept
	// either way, and they must be kept for the reason that is true of them.
	entries, err := os.ReadDir(path)
	if err != nil {
		keep.Reason = fmt.Sprintf("unreadable (%v)", err)
		return keep
	}
	pointer := filepath.Join(path, WorktreeStubGitPointerName)
	pointerKind, found := entryKind(entries, WorktreeStubGitPointerName)
	if !found {
		keep.Reason = WorktreeStubKeepPointerShape
		return keep
	}
	if pointerKind == entryKindDir {
		keep.Reason = WorktreeStubKeepPointersDir
		return keep
	}
	if pointerKind != entryKindFile {
		keep.Reason = WorktreeStubKeepPointerShape
		return keep
	}
	target, err := worktreeStubGitDirTarget(pointer)
	if err != nil {
		keep.Reason = WorktreeStubKeepPointerShape
		return keep
	}
	if _, terr := os.Lstat(target); terr == nil || !os.IsNotExist(terr) {
		// A target that EXISTS is a live registration: keep, whatever else is
		// true. A target that cannot be classified for any other reason (a
		// permission error) is an unknown — and an unknown is a KEEP.
		keep.Reason = WorktreeStubKeepTargetLive
		return keep
	}

	// PROVENANCE: the dead target must sit under a suite RUN directory, i.e. be a
	// leak this repository's own test entry point produced.
	if !worktreeStubTargetUnderSuiteRun(target) {
		keep.Reason = WorktreeStubKeepNotFromSuite
		return keep
	}

	// CONTENT: what the directory holds BESIDE the pointer — a FLAT set of small
	// regular files, i.e. a fixture's checked-out files. The measured residue is
	// the pointer plus one 8-byte README.md; a subdirectory, a symlink or a byte
	// total past the bound is a working tree and is kept (see the bounds above).
	files, extraBytes, contentErr := leftoverShape(entries)
	if contentErr != nil {
		keep.Reason = contentErr.Error()
		return keep
	}
	if files > StubLeftoverMaxFiles || extraBytes > StubLeftoverMaxBytes {
		keep.Reason = fmt.Sprintf("%s (%d file(s), %d bytes beside the pointer)", WorktreeStubKeepTooBig, files, extraBytes)
		return keep
	}

	// LIVENESS: a process standing in the directory, holding it open, or mapping
	// a file from it is a veto.
	if held, who := worktreeStubHeld(path, holders); held {
		keep.Reason = fmt.Sprintf("%s (%s)", WorktreeStubKeepHeld, who)
		return keep
	}

	keep.Keep = false
	keep.Reason = fmt.Sprintf("a .git pointer to %s, which no longer exists (the suite run that created it is gone), beside %d leftover file(s)/%d bytes",
		target, files, extraBytes)
	return keep
}

// entryKind classifies a directory entry for the pointer's sake, so the pointer
// checks read the DIRECTORY ENTRY first (the dtype the kernel already reported)
// and only touch the filesystem for what the entry cannot answer (Rule 34's
// cheapest-first discipline; and a `.git` that cannot be typed is a KEEP).
const (
	entryKindAbsent = iota
	entryKindFile
	entryKindDir
	entryKindOther
)

// entryKind classifies the named entry in a directory listing.
func entryKind(entries []os.DirEntry, name string) (int, bool) {
	for _, e := range entries {
		if e.Name() != name {
			continue
		}
		switch {
		case e.Type()&os.ModeSymlink != 0, e.Type()&os.ModeNamedPipe != 0, e.Type()&os.ModeSocket != 0, e.Type()&os.ModeDevice != 0:
			return entryKindOther, true
		case e.IsDir():
			return entryKindDir, true
		case e.Type().IsRegular():
			return entryKindFile, true
		default:
			// An untyped entry (a filesystem that reports DT_UNKNOWN) is resolved
			// by one stat; if even that cannot say, the caller keeps the
			// candidate.
			info, err := e.Info()
			if err != nil || info == nil {
				return entryKindOther, true
			}
			if info.IsDir() {
				return entryKindDir, true
			}
			if info.Mode().IsRegular() {
				return entryKindFile, true
			}
			return entryKindOther, true
		}
	}
	return entryKindAbsent, false
}

// leftoverShape measures what a candidate holds BESIDE its `.git` pointer: how
// many regular files, and their total bytes. Any non-regular entry is an error
// carrying WorktreeStubKeepHasContent — a working tree, not a fixture leftover.
func leftoverShape(entries []os.DirEntry) (files int, bytes int64, err error) {
	for _, e := range entries {
		if e.Name() == WorktreeStubGitPointerName {
			continue
		}
		if !e.Type().IsRegular() {
			return 0, 0, errKeepHasContent
		}
		info, ierr := e.Info()
		if ierr != nil {
			return 0, 0, fmt.Errorf("unmeasurable (%w)", ierr)
		}
		files++
		bytes += info.Size()
	}
	return files, bytes, nil
}

// errKeepHasContent carries WorktreeStubKeepHasContent as an error value, so the
// content check's refusal travels through the ordinary error return instead of
// through a second sentinel the caller would have to compare against (Rule 14:
// one spelling of the reason).
var errKeepHasContent = errors.New(WorktreeStubKeepHasContent)

// worktreeStubGitDirTarget reads a linked-worktree pointer file and returns the
// absolute path its single `gitdir: ` line names. Anything else (a binary
// `index`, an empty file, two lines, a relative value) is an error, never a guess.
func worktreeStubGitDirTarget(pointer string) (string, error) {
	data, err := os.ReadFile(pointer)
	if err != nil {
		return "", err
	}
	text := strings.TrimRight(string(data), "\n")
	if strings.Contains(text, "\n") {
		return "", fmt.Errorf("%s holds more than one line", pointer)
	}
	value, ok := strings.CutPrefix(text, WorktreeStubGitDirKey)
	if !ok {
		return "", fmt.Errorf("%s does not begin with %q", pointer, WorktreeStubGitDirKey)
	}
	value = strings.TrimSpace(value)
	if value == "" || !filepath.IsAbs(value) {
		return "", fmt.Errorf("%s names %q, which is not an absolute path", pointer, value)
	}
	return filepath.Clean(value), nil
}

// worktreeStubTargetUnderSuiteRun reports whether a gitdir path contains a
// component that is a suite RUN directory (`suite-<name>-run-<generated tail>`).
//
// It reuses suiteRunTokens — the SAME predicate that decides whether a directory
// in a memory base is this system's suite root — so the two halves of "a name our
// suite entry point produced" cannot drift into two shapes (Rule 14: one
// definition of the shape).
func worktreeStubTargetUnderSuiteRun(target string) bool {
	for _, part := range strings.Split(target, string(os.PathSeparator)) {
		if !strings.HasPrefix(part, SuiteRunMarker) {
			continue
		}
		if len(suiteRunTokens(part)) > 0 {
			return true
		}
	}
	return false
}

// worktreeStubHeld reports whether a live process holds the directory, and names
// it. The batched /proc snapshot answers the ordinary case; the per-directory
// probe is the fallback for what the snapshot cannot express (a holder that
// appeared after it, or one whose descriptors were not in the collected set).
//
// The second probe runs ONLY when the batch says "free", so the arm's cost stays
// one /proc pass in the common case (the LiveHolders rationale — 587 candidates
// on this host).
func worktreeStubHeld(dir string, holders HolderScan) (bool, string) {
	// TRUNCATED: the batch did not finish, so it proved nothing, and the
	// per-directory fallback below would pay a SECOND full process-table scan —
	// the very cost this bound exists to cap (one probe per candidate, 24
	// callers). Kept, with the reason that is true.
	if holders.Truncated {
		return true, HolderScanWithheldReason()
	}
	if !unheldBatch(dir, holders) {
		return true, "in the batch snapshot"
	}
	if pid, err := dirHolder(dir); err == nil && pid != "" {
		return true, "pid " + pid
	}
	return false, ""
}
