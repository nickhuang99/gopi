// profile_root.go — the gopi PROFILE ROOT's DIRECT CHILDREN: the registry of
// every child this repository's own writers create, the OBSERVATION face that
// names a child nobody declared, and the ONE narrow residue family the profile
// root may ever have collected.
//
// WHY THIS FILE EXISTS (the defect, measured 2026-09-27 on this host): four
// directories sat directly under ~/.gopi — `evid-01a0e562` (418M),
// `sigrec-red-1882254` (266M), `evid-01a0c630`, `evid-01a0cba8` — holding
// per-run binaries, whole checkout copies and log dumps an LLM session had
// hand-made while it worked. FOUR collectors exist, and every one of them is
// namespace-limited BY CONSTRUCTION, so all four missed them:
//
//   - the home sweep (gc.go) walks only HomeDir() and collects suite-/legacy/
//     queue names (IsScratchEntry);
//   - the base sweep (base_residue.go) walks the memory base and matches
//     `gopi-*` / `gocache-*` / `probe-*` / `suite-*-run-*` (IsBaseResidueName);
//   - the declared-tree sweep (declared_trees.go) walks GOTMPDIR, raw/,
//     sessions/**/*.raw.log and the build cache — four TREES, not the profile
//     root;
//   - the sandbox reclaimer (sandbox_gc.go) walks the configured sandbox root.
//
// The profile root was in NOBODY's scope, which means it had no vocabulary at
// all: not "owned", not "foreign", simply unmodelled. A directory could appear
// there and nothing in the system could say whether that was expected.
//
// WHAT THE PROFILE ROOT IS (the ruling this file encodes): a STATE directory,
// not a scratch directory. `<GOPI_HOME>/.gopi` holds the live ledger, the
// settings, the session history, the plugin tree, the component logs and the
// service locks — the state a gopi process READS to be itself. It is not a
// place to put work; a run's OWN evidence belongs in its session directory
// (`<profile>/sessions/<task-id>/`, the directory the run's report path
// already names, Rule 37), and a task's deliverable belongs on a branch.
//
// SO THIS FILE DOES THREE THINGS AND REFUSES A FOURTH:
//
//  1. DECLARES every direct child the repository's own writers create, with the
//     writer file, the evidence string that must still exist in it, and the
//     sentence naming how that child is bounded (ProfileRootChildren, D1).
//
//  2. OBSERVES: a child that is on disk and declared by nobody is REPORTED by
//     name — one command (`gopi tool scratch-gc profile`), one field in the resident
//     tick's output. An undeclared child is not automatically wrong (an operator
//     may park a directory here); it is automatically UNEXPLAINED, and the
//     absence of a vocabulary is what made 684 MB invisible for ten days.
//
//  3. COLLECTS exactly ONE narrow family — the hand-made per-run evidence
//     directory, named `evid-<token>` / `sigrec-<token>` — under the SAME hard
//     rule every other arm obeys: in-namespace AND stale AND held by no live
//     process (/proc cwd+exe+fd+maps, the ONE liveness ladder). The name rule is
//     SHAPE, not a prefix test, because this is the operator's profile.
//
//  4. REFUSES to become a general reaper of the profile root. There is
//     deliberately NO `--yes` on the CLI face and NO delete on the tick: the
//     roots are never candidates, the declared children are never candidates,
//     and the automatic arm has no deleting face at all — the lesson of the
//     resident tick that deleted a SHARED family while a comment claimed it did
//     not (base_suite_runs.go, memory #7096) is that an automatic deleter on a
//     shared resource is a defect waiting for the right night.
package scratch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProfileChildClass is what a direct child of the profile root IS — the three
// answers this registry can give, and nothing in between.
type ProfileChildClass string

const (
	// ProfileChildState is LIVE STATE: the child holds the state a gopi process
	// reads to be itself (the ledger, the settings, the session tree, the plugin
	// tree, the service locks). It is never a candidate at any age, and the
	// class exists so that fact is WRITTEN DOWN once instead of being implied by
	// every sweep's namespace rule.
	ProfileChildState ProfileChildClass = "state"
	// ProfileChildDeclared is a child a repository writer CREATES and that the
	// writer itself bounds (or that nothing bounds yet, stated in Bound). It is
	// not a candidate here either — a tree with its own collector keeps it
	// (Rule: one collector per tree).
	ProfileChildDeclared ProfileChildClass = "declared"
	// ProfileChildResidue is the ONE family this file may collect: a hand-made
	// per-run evidence directory. Its name rule is ProfileResidueName.
	ProfileChildResidue ProfileChildClass = "residue"
)

// ProfileChildKind says whether a registry row names one exact child or a
// PREFIX family (a writer that appends a stamp to a fixed stem).
type ProfileChildKind int

const (
	// ProfileChildExact matches the child name exactly.
	ProfileChildExact ProfileChildKind = iota
	// ProfileChildPrefix matches every name beginning with the row's stem —
	// used for the two families whose writers append their own suffix
	// (`settings.json.bak-<stamp>`, `memory.db-wal` / `memory.db-shm`).
	ProfileChildPrefix
)

// The residue family: the shape rule's own vocabulary. The prefixes are the
// names a session measured on this host actually used (`evid-<task-id>`,
// `sigrec-red-<pid>`); the rule below is written so that ONLY a name carrying a
// machine-generated token can ever match, and so that a near miss a person
// typed (`evidence`, `evid-notes`) is refused BY NAME and not by accident.
const (
	// ProfileResiduePrefixEvidence names the hand-made evidence directory
	// family: `<stem>-<task-id>` (`evid-01a0e562`). The stem is a token, not a
	// word, so a human's `evidence/` directory — which does NOT carry the dash —
	// can never match it.
	ProfileResiduePrefixEvidence = "evid-"
	// ProfileResiduePrefixSignal names the second measured family: a session
	// reproducing a signal-handling defect kept its per-run binaries and its
	// master checkout in `sigrec-red-<pid>`.
	ProfileResiduePrefixSignal = "sigrec-"
)

// profileResiduePrefixes lists the families the residue arm may ever consider.
// A package-level slice (like baseResiduePrefixes) so the tests can walk it in
// a stable order; the exported surface is the PREDICATE, never this list.
var profileResiduePrefixes = []string{
	ProfileResiduePrefixEvidence,
	ProfileResiduePrefixSignal,
}

// profileDirNameForRoot is the name a profile root carries, and it is the sweep's
// STRONGEST guard: `<GOPI_HOME>/.gopi` always ends in this component
// (config.GopiDir → config.ProfileDirIn), so a path whose base is anything else
// is not a profile root and the arm refuses it before looking at one entry. The
// value is this package's existing constant (tmproot.go), never a second
// spelling of it (Rules 13/14).
const profileRootBaseName = gopiDirName

// ProfileRootChild is ONE direct child of the profile root that this
// repository's own writers create (or one that a writer's own backup/lock
// channel appends to). Every field is read by the observation face or by the
// gate, and the gate fails when a field the registry relies on goes unread.
type ProfileRootChild struct {
	// Name is the child's name (the exact name, or the prefix family's stem —
	// see Kind).
	Name string
	// Kind: exact child or prefix family.
	Kind ProfileChildKind
	// Class is what the child IS (state / declared / residue).
	Class ProfileChildClass
	// Writer is the repo-relative production file that creates the child — the
	// gate reads it. Empty means "no repository writer creates this": the
	// operator's own sink (a configured notify.cmd), their editor, or a session.
	// An empty Writer is a STATEMENT, not a gap: the gate cannot pin evidence in
	// a file that does not exist, and inventing a writer row would be the lie
	// this field exists to prevent.
	Writer string
	// Evidence is a string that must still exist in Writer, so a writer that
	// moves or renames its child fails the gate instead of silently orphaning
	// the declaration.
	Evidence string
	// Bound is the sentence naming how the child is kept from growing without
	// limit — or that nothing bounds it yet. It is required (the gate refuses an
	// empty one) because "we know who writes it" and "something stops it" are
	// two different claims, and this registry is worthless if it conflates them.
	Bound string
}

// matches reports whether a direct child NAME belongs to this row.
func (c ProfileRootChild) matches(name string) bool {
	if c.Kind == ProfileChildPrefix {
		return strings.HasPrefix(name, c.Name)
	}
	return name == c.Name
}

// ProfileRootChildren is the ONE registry of the profile root's declared direct
// children. It is deliberately SHORT: a row exists because a reader established
// who writes the child, and a row nobody can justify is worse than a reported
// undeclared child (that is what the observation face is for).
//
// MEASURED (2026-10-01, this host, 89 direct children of ~/.gopi): the rows
// below account for the state every gopi process reads, plus the two families
// whose writers append a suffix. Everything else on disk was, at measurement
// time: a session's hand-made directory (`evid-*`, `sigrec-*`, `tabs`, `tabs2`,
// `evidence`, `verify-reports`, `pricing-snapshots`, `night-watch-scratch`,
// `dumps-1214`, `tmp`), a session's loose file (`*.png`, `*.log`, `acc.txt`,
// `req-umbrella-sample.txt`, `chat_history`) or a lock/stamp file. Those are
// reported, never collected — see the census in devdoc/profile-root-children.md.
func ProfileRootChildren() []ProfileRootChild {
	return append(staticProfileRootChildren(), registeredProfileRootChildren...)
}

// registeredProfileRootChildren holds the rows a package that owns a child name
// SUPPLIES at init (RegisterProfileRootChildren). It exists because a name can
// live in a package this one cannot import: `config.SessionsDirName` is defined
// in internal/config, which imports THIS package, so spelling the sessions tree
// here would be both a cycle and a second spelling (Rules 13/14 — the same
// reason the static rows below reach settings.json and memory.db through their
// written form and the declared-tree resolvers reach their roots through a seam).
var registeredProfileRootChildren []ProfileRootChild

// RegisterProfileRootChildren appends the rows a caller owns. It is called once,
// from internal/config's own init, with rows whose Name comes from that
// package's exported constants — so the name of a child is declared exactly
// where it is defined and nowhere else.
//
// It RETURNS the error a caller cannot ignore rather than panicking (Rule 19:
// a panic belongs in init/main/Must*, and this helper is called from an init —
// the panic was one frame away from the exemption it looked like it had). The
// failure is the same one the panic named: a row with no name would silently
// never match anything, which is the shape of a declaration that only looks
// like one (Rule 15). On refusal NOTHING is appended, so a refused batch leaves
// the registry exactly as it was.
func RegisterProfileRootChildren(rows ...ProfileRootChild) error {
	for _, row := range rows {
		if strings.TrimSpace(row.Name) == "" {
			return errProfileRootChildUnnamed
		}
	}
	registeredProfileRootChildren = append(registeredProfileRootChildren, rows...)
	return nil
}

// errProfileRootChildUnnamed reports the one way RegisterProfileRootChildren
// refuses a registration. It is a named error, not a formatted string, for the
// same reason the sibling seam below uses one: the caller reads it by name
// (Rule 15 — the failure is stated, not swallowed).
var errProfileRootChildUnnamed = fmt.Errorf("scratch: RegisterProfileRootChildren called with an empty Name")

// staticProfileRootChildren is the registry of every profile-root child whose
// name this package can spell itself.
func staticProfileRootChildren() []ProfileRootChild {
	return []ProfileRootChild{
		{
			Name: "settings.json", Kind: ProfileChildPrefix, Class: ProfileChildState,
			Writer:   "internal/config/config.go",
			Evidence: `filepath.Join(gd, "settings.json")`,
			Bound:    "the operator's settings file (and its .bak-<stamp> siblings, which its writer creates before a write); never a candidate",
		},
		{
			Name: "memory.db", Kind: ProfileChildPrefix, Class: ProfileChildState,
			Writer:   "internal/config/memory_env.go",
			Evidence: `filepath.Join(gd, MemoryDBFileName)`,
			Bound:    "the append-only ledger (with its -wal/-shm siblings while it is open); never a candidate at any age",
		},
		{
			Name: "plugins", Kind: ProfileChildExact, Class: ProfileChildState,
			Writer:   "internal/config/plugins.go",
			Evidence: `filepath.Join(gopiDir, PluginsDirName)`,
			Bound:    "the plugin tree the operator installs into; never a candidate",
		},
		{
			Name: "raw", Kind: ProfileChildExact, Class: ProfileChildDeclared,
			Writer:   "internal/scratch/declared_trees.go",
			Evidence: `rawLogDirName = "raw"`,
			Bound:    "declared tree `raw` — RawLogStaleAge (72h) over our own `gopi-raw-*.log` entries",
		},
		{
			Name: "logs", Kind: ProfileChildExact, Class: ProfileChildDeclared,
			Writer:   "internal/removalaudit/audit.go",
			Evidence: `LogDirName = "logs"`,
			Bound:    "the removal-audit sink and the component logs; append-only operator material, not bounded here",
		},
		{
			Name: "dumps", Kind: ProfileChildPrefix, Class: ProfileChildDeclared,
			Writer:   "internal/scratch/scratch.go",
			Evidence: `DumpsDirName = "dumps"`,
			Bound:    "the diagnostic-dump sink (the leaf `dumps` plus the knob-suffixed siblings a host configures, e.g. GOPI_1214_DUMP_DIR=<profile>/dumps-1214 — both measured on this host); nothing bounds it here, so it is reported and never collected",
		},
		{
			Name: "backups", Kind: ProfileChildExact, Class: ProfileChildDeclared,
			Writer:   "internal/tools/ledger_backup.go",
			Evidence: `LedgerBackupDirName = "backups"`,
			Bound:    "the ledger backup sink (<profile>/backups); each snapshot is an operator-kept artifact, not bounded here",
		},
		{
			Name: "merger", Kind: ProfileChildExact, Class: ProfileChildState,
			Writer:   "internal/tools/merger_graceful.go",
			Evidence: `MergerStateDirName = "merger"`,
			Bound:    "the merger's control/state directory (one TERM_<pid> file per shutdown handshake); live state, never a candidate",
		},
		{
			Name: "worker", Kind: ProfileChildExact, Class: ProfileChildState,
			Writer:   "cmd/agent/cli_worker_control.go",
			Evidence: `workerStateDirName = "worker"`,
			Bound:    "the worker's control directory; live state, never a candidate",
		},
		{
			Name: "notify", Kind: ProfileChildExact, Class: ProfileChildState,
			Writer:   "",
			Evidence: "",
			Bound:    "the operator's notify sink (settings.json notify.cmd appends to notify/task-events.jsonl — an append-only event log, and the file a zombie audit reads); no repository writer creates the directory, so it is operator-owned and never a candidate",
		},
		{
			Name: "task-sessions", Kind: ProfileChildExact, Class: ProfileChildState,
			Writer:   "internal/tools/task_session.go",
			Evidence: `taskLegacySessionsRootName = "task-sessions"`,
			Bound:    "the LEGACY task-session root, read as a fallback after the sessions tree (taskLegacySessionsRootName); historical state, never a candidate",
		},
		{
			Name: ".gopi", Kind: ProfileChildExact, Class: ProfileChildState,
			Writer:   "",
			Evidence: "",
			Bound:    "a host-level nesting of the profile root itself (measured on this host: a suite that pins GOPI_HOME=$HOME left its own ~/.gopi here); it holds state and is never a candidate — an operator removes it by hand if it is not wanted",
		},
		{
			Name: ".gopi-home-gc.lock", Kind: ProfileChildExact, Class: ProfileChildState,
			Writer:   "",
			Evidence: "",
			Bound:    "the host-level home-GC gate lock installed beside the crontab that ticks this sweep; a lock is live state and is never a candidate (it is the operator's own gate, not a repository writer's)",
		},
		{
			Name: "serve.lock", Kind: ProfileChildExact, Class: ProfileChildState,
			Writer:   "cmd/agent/cli_tasks.go",
			Evidence: `serveLockFilename = "serve.lock"`,
			Bound:    "the serve singleton lock; live state, never a candidate",
		},
		{
			Name: "worker.lock", Kind: ProfileChildExact, Class: ProfileChildState,
			Writer:   "cmd/agent/cli_worker_control.go",
			Evidence: `workerLockFilename = "worker.lock"`,
			Bound:    "the worker spawn guard's lock (internal/config/agentguard.go reads the same name); live state, never a candidate",
		},
	}
}

// ProfileResidueName reports whether a direct child NAME is in the hand-made
// per-run evidence family this arm may ever collect.
//
// THE RULE IS SHAPE, NOT A PREFIX (the profile root is the operator's state
// directory; a bare prefix test would delete `evid-notes` the day somebody
// names a directory that way):
//
//		name = <family> "-" <token> [ "-" <token> ]*
//
//	  - the first dash-separated token must be EQUAL to a family, so the bare
//	    word `evidence` — a directory this system's own documentation names — is
//	    refused by the same rule that accepts `evid-01a0e562`;
//	  - every token must be non-empty and in the residue alphabet (letters,
//	    digits, underscore — no dot, no slash);
//	  - at least one token must look GENERATED rather than chosen: it carries a
//	    digit or an underscore, or is a UUID, or is a base36 key. The folding is
//	    the SAME predicate the base sweep uses (baseResidueSuffixGenerated,
//	    Rule 14: one shape rule for one system), so `evid-notes` and
//	    `sigrec-notes` are refused while `sigrec-red-1882254` is accepted.
//
// It deliberately consults neither age nor liveness: those are the caller's
// half (ProfileResidueCollectable), and keeping them apart is what lets a table
// test pin the namespace rule on names alone.
func ProfileResidueName(name string) bool {
	rest, ok := profileResidueRest(name)
	if !ok {
		return false
	}
	for _, token := range strings.Split(rest, "-") {
		if token == "" || !isBaseResidueTokenAlphabet(token) {
			return false // a dot, a plus, a doubled dash: nothing this system writes
		}
		if profileResidueTailGenerated(token) {
			return true
		}
	}
	return false
}

// profileResiduePIDMinDigits is the shortest DECIMAL run the profile arm accepts
// as a generated tail — a pid. Measured on this host: the pids a session spelled
// into these directory names are 7 digits (`sigrec-red-1882254`), and this
// host's live pids sit in the same range. The floor is 5 rather than 2 (the base
// family's floor) because a 4-digit run is exactly the shape of a YEAR a person
// appends to a word ("evid-notes-2026"), and this arm may not draw that
// distinction any other way — a token-level rule cannot tell 2026 from a pid
// except by length.
const profileResiduePIDMinDigits = 5

// profileResidueTailGenerated reports whether ONE tail token of a residue name is
// a shape a GENERATOR produced rather than a word a person chose.
//
// IT IS DELIBERATELY STRICTER THAN THE FAMILY-WIDE FOLD (baseResidueSuffixGenerated,
// which the memory base uses) and the difference is intentional, not drift: that
// fold accepts any token carrying a digit, which is right for a shared 1777
// directory whose job is to recognise a family by its own naming scheme, while
// THIS arm governs the operator's own state directory (the ledger lives here), so
// it takes the narrow shape and accepts only what the two measured families
// actually emitted:
//
//   - a LOWERCASE HEX run of at least baseResidueHexTokenLen characters — a task
//     id (`01a0e562`), which os.MkdirTemp alone cannot produce (its alphabet
//     includes uppercase, so the shape stays unambiguous);
//   - a DECIMAL run of at least profileResiduePIDMinDigits — a pid;
//   - a canonical UUID (`550e8400-e29b-41d4-a716-446655440000`);
//   - a token carrying an UNDERSCORE — os.MkdirTemp's alphabet, never a word a
//     human types.
//
// Every accepted shape is one a session measured in this profile; every refused
// one (`evid-notes`, `sigrec-notes`, `evid-notes-2026`) is a name a person picked,
// and a name this arm did not create is not a name it may delete.
func profileResidueTailGenerated(token string) bool {
	switch {
	case strings.ContainsRune(token, '_'):
		return true
	case isLowerHex(token) && len(token) >= baseResidueHexTokenLen:
		return true
	case IsUUIDName(token):
		return true
	case isDecimalToken(token) && len(token) >= profileResiduePIDMinDigits:
		return true
	}
	return false
}

// profileResidueRest strips one of the family prefixes and returns the
// remainder, or reports that no family matched. A bare prefix with an empty
// remainder ("evid-") is refused: this system never creates one.
func profileResidueRest(name string) (string, bool) {
	for _, prefix := range profileResiduePrefixes {
		if rest, matched := strings.CutPrefix(name, prefix); matched {
			if rest == "" {
				return "", false
			}
			return rest, true
		}
	}
	return "", false
}

// ProfileRootRefusal is the guard that runs BEFORE the arm looks at any entry,
// and it is what keeps this sweep off a tree it must never touch. It refuses,
// by name and with the reason:
//
//   - the empty path, "." and the filesystem root (there is no profile there);
//   - a path that is not a directory;
//   - a path whose base is not the profile root's own name (`gopiDirName`) —
//     the strongest check available, and the one that makes a mistyped
//     `scratch-gc profile --root ~/workspace` impossible to satisfy;
//   - a git work tree (a `.git` entry): that is somebody's checkout, whose own
//     tooling owns it.
//
// It is a REFUSAL, not a warning: the caller reports it and does nothing else.
func ProfileRootRefusal(root string) error {
	clean := filepath.Clean(strings.TrimSpace(root))
	switch clean {
	case "", ".", string(os.PathSeparator):
		return fmt.Errorf("profile-root arm: refusing %q — that is not a profile root", root)
	}
	fi, err := os.Lstat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("profile-root arm: refusing %q — %w", clean, ErrProfileRootMissing)
		}
		return fmt.Errorf("profile-root arm: cannot measure %q: %w", clean, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("profile-root arm: refusing %q — not a directory (a symlinked profile is not this arm's to walk)", clean)
	}
	if filepath.Base(clean) != profileRootBaseName {
		return fmt.Errorf("profile-root arm: refusing %q — a profile root ends in %q (the value a gopi profile always carries), so this path is not one",
			clean, profileRootBaseName)
	}
	if _, err := os.Lstat(filepath.Join(clean, WorktreeStubGitPointerName)); err == nil {
		return fmt.Errorf("profile-root arm: refusing %q — it is a git work tree", clean)
	}
	return nil
}

// ErrProfileRootMissing reports a profile root that is not there. It is a NAMED
// error because "this host has no profile yet" is a reported no-op (exit 0 — a
// watchdog must not alarm over a tree that was never created), while a root that
// exists and cannot be read IS an alarm (Rule 15).
var ErrProfileRootMissing = errors.New("profile root does not exist on this host")

// ProfileChildVerdict is ONE direct child's judgement: what it is, who declared
// it, and — for the residue family only — whether it may be collected.
type ProfileChildVerdict struct {
	// Path is the child.
	Path string
	// Name is the child's name (what a report prints).
	Name string
	// Declared is true when a registry row matches the name — including the
	// residue family, which is declared BY SHAPE rather than by row.
	Declared bool
	// Class is the declaring row's class ("" when nothing declared it).
	Class ProfileChildClass
	// Writer is the declaring row's writer ("" when nothing declared it).
	Writer string
	// Reason is the sentence that decided the verdict — the registry row's own
	// `bound` sentence for a declared child, or the arm's own words.
	Reason string
	// Collectable is true only for an aged, unheld member of the residue family.
	Collectable bool
	// Age is the child's mtime age, measured (0 when unmeasurable).
	Age time.Duration
}

// ProfileRootReport judges EVERY direct child of the profile root and deletes
// nothing — the observation face the epic asks for ("a directory nobody declared
// must be visible to one command").
//
// It reads the root's direct children only, never recurses, never resolves a
// symlink, and takes no lock. It returns the verdicts in name order so two runs
// over the same tree print the same report.
func ProfileRootReport(root string, olderThan time.Duration) ([]ProfileChildVerdict, error) {
	if root == "" {
		resolved, err := ResolveProfileRoot()
		if err != nil {
			return nil, err
		}
		root = resolved
	}
	if err := ProfileRootRefusal(root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrProfileRootMissing, root)
		}
		return nil, fmt.Errorf("profile-root arm: read %s: %w", root, err)
	}
	if len(entries) > ScanLimit {
		return nil, fmt.Errorf("profile-root arm: %s holds more than %d entries — report stopped early", root, ScanLimit)
	}
	now := time.Now()
	// ONE /proc pass for the whole report: the per-entry form would cost a
	// process-table walk per child (the rationale LiveHolders documents).
	holders := ScanHolders()
	children := ProfileRootChildren()
	out := make([]ProfileChildVerdict, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		v := ProfileChildVerdict{Path: filepath.Join(root, name), Name: name}
		if row, ok := profileRowFor(name, children); ok {
			v.Declared = true
			v.Class = row.Class
			v.Writer = row.Writer
			v.Reason = row.Bound
			out = append(out, v)
			continue
		}
		if !ProfileResidueName(name) {
			// The observation this whole file exists for: a child nobody
			// declared and whose name is in no family of ours. It is NOT wrong —
			// it is UNEXPLAINED, and it is reported as exactly that.
			v.Reason = ProfileUndeclaredReason
			out = append(out, v)
			continue
		}
		v.Declared = true
		v.Class = ProfileChildResidue
		v.Collectable = profileResidueCollectable(now, v.Path, name, olderThan, holders)
		v.Reason = profileResidueReason(v.Path, now, olderThan, holders, v.Collectable)
		// The age is measured AFTER the verdict so a vanished child cannot make
		// the report claim a number it did not see (Rule 15).
		if fi, statErr := os.Lstat(v.Path); statErr == nil {
			v.Age = now.Sub(fi.ModTime())
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// profileRowFor finds the registry row declaring a child name.
func profileRowFor(name string, rows []ProfileRootChild) (ProfileRootChild, bool) {
	for _, row := range rows {
		if row.matches(name) {
			return row, true
		}
	}
	return ProfileRootChild{}, false
}

// ProfileUndeclaredReason is the ONE spelling of the undeclared-child verdict.
// It is exported because the CLI's report, the tick's line and the tests all
// quote it, and a third spelling of it would be a third policy.
const ProfileUndeclaredReason = "undeclared: no writer in this repository creates this child, and its name is in no family of ours — harmless to look at, and never removed"

// profileResidueCollectable is the residue arm's boolean face: the answer is the
// verdict's own Collectable field (one predicate, GC-able and reportable from
// the same judgement).
func profileResidueCollectable(now time.Time, path, name string, olderThan time.Duration, holders HolderScan) bool {
	if !ProfileResidueName(name) {
		return false
	}
	// The SHARED age+holder half of every arm (staleUnheldDirectory): the profile
	// arm does not get a second, weaker liveness rule (memory #2632).
	return staleUnheldDirectory(path, now, olderThan, holders, "")
}

// profileResidueReason names WHICH half kept an in-namespace child, in the same
// order the shared predicate applies them — so the sentence a report prints is
// the reason the sweep acted on, never a second opinion about it.
func profileResidueReason(path string, now time.Time, olderThan time.Duration, holders HolderScan, collectable bool) string {
	if collectable {
		return EntryCollectablePrefix + "hand-made per-run evidence directory, stale and held by no live process"
	}
	stale, err := ScratchEntryStale(path, now, olderThan)
	if err != nil {
		return fmt.Sprintf("%s: %v", EntryKeepUnmeasurable, err)
	}
	if !stale {
		return fmt.Sprintf("%s (%s)", EntryKeepFresh, olderThan)
	}
	if holders.Truncated {
		// Kept because the scan did not finish — stated as that, never as a
		// holder that was seen (Rule 15). No second probe is run to try to
		// name a holder: that would be a fresh full process-table scan per
		// candidate, which is the cost this epic is bounding.
		return HolderScanWithheldReason()
	}
	if names := HolderNames(path, 0); len(names) > 0 {
		return fmt.Sprintf("%s (%s)", EntryKeepLiveHolder, strings.Join(names, ", "))
	}
	return EntryKeepLiveHolder
}

// SweepProfileRootResidue removes the residue-family children ProfileRootReport
// marked collectable, and NOTHING else. It re-applies the same predicate at the
// delete (removeCollectableOfProfileChild) so a child that became live between
// the report and the removal survives — the invariant every arm in this package
// shares.
//
// It exists for the EXPLICIT face only. The resident tick calls the report and
// never this function (see cmd/agent/scratch_gc_tick.go), which is the whole
// reason the two faces are separate functions rather than a flag.
func SweepProfileRootResidue(root string, olderThan time.Duration) (removed int, bytes int64, err error) {
	if root == "" {
		resolved, rerr := ResolveProfileRoot()
		if rerr != nil {
			return 0, 0, rerr
		}
		root = resolved
	}
	verdicts, err := ProfileRootReport(root, olderThan)
	if err != nil {
		return 0, 0, err
	}
	for _, v := range verdicts {
		if !v.Collectable {
			continue
		}
		// The bytes are measured BEFORE the removal: after it the directory is
		// gone and every count would read 0, which is the shape of a report that
		// silently stops measuring what it reports (Rule 15).
		held := DirBytes(v.Path)
		if rerr := removeCollectableOfProfileChild(v.Path); rerr != nil {
			return removed, bytes, fmt.Errorf("profile-root arm: remove %s: %w", v.Path, rerr)
		}
		bytes += held
		removed++
	}
	return removed, bytes, nil
}

// removeCollectableOfProfileChild is the LAST check before one delete, and it is
// deliberately a second call to the same namespace predicate rather than a
// trusted argument: the root and the family are the two things this arm must
// never get wrong, and re-deciding them costs one Lstat.
func removeCollectableOfProfileChild(path string) error {
	name := filepath.Base(path)
	if !ProfileResidueName(name) {
		return fmt.Errorf("refusing to remove %s: %q is not in the profile-root residue family", path, name)
	}
	if filepath.Base(filepath.Dir(filepath.Clean(path))) != profileRootBaseName {
		return fmt.Errorf("refusing to remove %s: its parent is not a profile root", path)
	}
	return removeCollectable(path)
}

// ProfileRootResolver is the function seam that supplies the profile root from
// the package that owns it (internal/config, via GopiDir). It exists for the
// same reason the declared-tree resolvers do: this is a LEAF package and
// internal/config imports it, so the reverse import is a cycle. The assignment
// is checked by TestProfileRootResolverIsWired.
var profileRootResolver = func() (string, error) { return "", errProfileRootResolverUnwired }

// errProfileRootResolverUnwired reports the one failure mode of the seam above.
var errProfileRootResolverUnwired = fmt.Errorf("scratch: profile-root resolver is not wired (import internal/config)")

// RegisterProfileRootResolver installs the profile-root resolver.
// internal/config calls it once from its own init, passing its GopiDir — so the
// path has one spelling in the tree and this package stays a leaf.
func RegisterProfileRootResolver(fn func() (string, error)) {
	if fn != nil {
		profileRootResolver = fn
	}
}

// ResolveProfileRoot returns the gopi profile root through the wired resolver,
// or the unwired error — never a guessed path (Rule 15: an unmeasured fact must
// not render as a measured one).
func ResolveProfileRoot() (string, error) {
	root, err := profileRootResolver()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("%w", errProfileRootResolverUnwired)
	}
	return CanonicalPath(root), nil
}

// ProfileRootCounts is the report's own tally, so every face (the CLI, the tick,
// the tests) prints the same three numbers from the same walk.
type ProfileRootCounts struct {
	// Children is every direct child seen.
	Children int
	// Declared is how many of them a registry row (or the residue rule) explains.
	Declared int
	// Undeclared is how many nobody declared — the number the tick reports.
	Undeclared int
	// Collectable is how many residue-family children the explicit arm could
	// remove right now.
	Collectable int
	// UndeclaredNames is the undeclared children's names, in report order (the
	// evidence a tick line carries so an operator can act without a second
	// command).
	UndeclaredNames []string
}

// TallyProfileRoot counts a report. It is a pure function of the verdicts, so
// the CLI, the tick and the tests cannot disagree about what the walk found.
func TallyProfileRoot(verdicts []ProfileChildVerdict) ProfileRootCounts {
	var c ProfileRootCounts
	c.Children = len(verdicts)
	for _, v := range verdicts {
		if v.Declared {
			c.Declared++
			continue
		}
		c.Undeclared++
		c.UndeclaredNames = append(c.UndeclaredNames, v.Name)
	}
	for _, v := range verdicts {
		if v.Collectable {
			c.Collectable++
		}
	}
	return c
}
