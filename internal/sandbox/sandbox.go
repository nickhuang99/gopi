// Package sandbox is the ONE way an agent gets an isolated gopi profile.
//
// WHY THIS PACKAGE EXISTS (the anti-pattern it replaces): an agent that needed
// to run something under a private profile invented the sandbox on the spot —
// `SB=$(mktemp -d)`, `SB=~/workspace/sbx-$$`, a per-script `$SB` variable, an
// `os.MkdirTemp` inside the test body — then exported GOPI_HOME into it and
// built the contents ad hoc (census: devdoc/agent-sandbox-unify.md). Each
// invention is a different shape, none of them is ever reclaimed, and each one
// is a second, unreviewable answer to "what does an isolated profile contain?".
//
// The contract this package makes instead:
//
//	gopi tool sandbox create [--label NAME] [--task-id ID]   → prints the path
//
//	<root>/sandbox-<label>-<rand>/
//	  .gopi/                        ← the gopi PROFILE root (GOPI_HOME=this dir
//	  │                                ⇒ config.GopiDir resolves HERE)
//	  │   settings.json      a read-only COPY (0444) of the operator's real
//	  │                     settings document — a COPY, never a link: a link made
//	  │                     the live file writable from inside the sandbox and a
//	  │                     fixture `>` redirect destroyed it (2026-09-27). See
//	  │                     settings_doc.go.
//	  │   plugins      -> <the REAL plugin tree>     (SYMLINK, read-only by contract;
//	  │                     the web-search scripts resolve under it, so a sandbox
//	  │                     that lacked it could not run `gopi web search` at all)
//	  │   memory.db         the sandbox’s OWN ledger: a REAL, private, WRITABLE
//	  │                     file (mode 0600) at the path the ledger resolver
//	  │                     computes for a sandbox GOPI_HOME — EMPTY unless a seed
//	  │                     was named, in which case it is a byte-for-byte COPY of
//	  │                     that seed. NEVER a symlink: the ledger is the file a
//	  │                     run WRITES FIRST (see ledger.go).
//	  │   sessions/     (private, empty)
//	  │   raw/          (private, empty)
//	  │   logs/         (private, empty)
//	  .sandbox-owner.json  (task id + pid + start ticks + created-at)
//
// THE .gopi LEVEL IS THE WHOLE POINT OF THAT TREE. GOPI_HOME is a HOME, not a
// data root: every reader (gopilog's session listing, the raw-log writer, the
// ledger, the stats surface) resolves <GOPI_HOME>/.gopi/... through
// config.GopiDir. A sandbox that materialised sessions/ directly beside this
// directory would therefore be a shape NOTHING resolves — the help text would
// name a directory the user can see while `GOPI_HOME=$SB gopi session log --last`
// answered "no such file or directory". The initializer builds the profile path
// through config.ProfileDirIn (the READER's own join, Rule 14), so creator and
// readers cannot diverge again.
//
// TWO PROPERTIES ARE THE POINT:
//
//  1. IDEMPOTENCE. Create is safe to call again and again — a repeated call
//     from the SAME process reuses the sandbox it made for the same label (the
//     marker's (pid, start ticks) pair is what proves "mine"), and Ensure
//     repairs a
//     sandbox that lost an entry (a GC that got it wrong, a hand edit). A
//     sandbox is therefore a VALUE that can be re-derived, not a one-shot
//     artifact someone must remember to look after.
//
//  2. RECLAIMABILITY. The ownership marker travels INSIDE the directory, so
//     the reclaimer (scratch.SweepSandboxes, riding the EXISTING scratch-gc
//     one-shot and the scratch tick) can decide an abandoned
//     sandbox's fate without any registry, any daemon and any memory. The
//     module cannot import internal/testutil (testutil imports THIS package —
//     a cycle), so MemoryDBPath takes the path rather than resolving it.
//
// WHAT IT DELIBERATELY IS NOT: not a `t.Cleanup` (a killed run never runs one),
// not a shared/multi-tenant directory (one shape, always private — there is no
// read-only-here/writable-there split, which is exactly why the LEDGER is a
// real private file rather than a link to the shared sample: see ledger.go),
// and not a copy of the operator's profile TREE — with ONE deliberate
// exception: the settings DOCUMENT is a copy, because a link to a single
// mutable file is what a fixture destroyed on 2026-09-27. The plugin TREE keeps
// its link (a tree is never write-opened; a copy would drift).
package sandbox

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// The permissions the initializer creates things with. Named (Rule 1a): the
// reclaimer reasons about the same shape, and a mode inlined at two sites is
// how a "read-only by contract" claim stops being true.
const (
	// sandboxRootPerm is for the root the operator configured: 0755, because
	// the root is a container other tools (and the operator) look inside.
	sandboxRootPerm = 0o755
	// sandboxDirPerm is for ONE sandbox: 0700 — a private run's profile is
	// nobody else's business, and the mode is the first half of that promise
	// (the reclaimer's ownership check is the second).
	sandboxDirPerm = 0o700
	// sandboxEntryPerm is for the private, empty directories inside a sandbox
	// (sessions/, raw/, logs/). They are WRITE targets, so they must not be
	// read-only — unlike the two symlinks, whose read-only-ness is a contract
	// about the TARGET, not about these modes.
	sandboxEntryPerm = 0o700
	// sandboxMarkerPerm is for the ownership marker: readable by the reclaimer,
	// which may run as the same user but must never need write access.
	sandboxMarkerPerm = 0o644
)

// randSuffixBytes is the width of the random component in a sandbox name. Four
// bytes (8 hex chars) is enough to make `sandbox-build-3f9a1c02` unique among
// the sandboxes a host creates in a day without making the name unreadable.
const randSuffixBytes = 4

// Options are the caller's inputs. Every field is optional; the zero value is
// "an anonymous sandbox with the operator's real settings and the shared
// sample database".
type Options struct {
	// Label is the human-readable middle of the directory name and the key
	// Create's idempotence matches on: a second Create from the SAME process
	// with the same label reuses the sandbox the first one made. A sandbox is
	// PRIVATE to its caller, so a directory another process made is never
	// adopted (its marker's (pid, start ticks) pair is the proof of ownership).
	// Keep it to [a-z0-9-].
	Label string
	// TaskID is recorded in the ownership marker so a task's sandbox is
	// attributable from the ledger side. Optional (an ad-hoc run has no row).
	TaskID string
	// ProfileSettingsPath is the REAL settings file the sandbox's
	// `settings.json` is COPIED from. Default: config.ResolveSettingsPath() —
	// the same resolution production uses, so the sandbox reads the same
	// configuration production reads. A path INSIDE the sandbox root is refused:
	// a profile configured by its own contents is the self-referential shape
	// that silently tests nothing.
	//
	// The profile's document is a COPY (settings_doc.go), never a link: a link
	// made the operator's live file writable from inside the sandbox through a
	// `>` redirect (measured 2026-09-27, the live settings.json lost).
	ProfileSettingsPath string
	// LedgerSeedPath is a database COPIED to become the sandbox's ledger — a
	// seed, by VALUE. Default: the environment's GopiSandboxLedgerSeedEnvName;
	// empty means the ledger starts EMPTY (a valid sandbox: a run's ledger is
	// supposed to fill with that run's rows). The caller that can import
	// testutil (a test) passes testutil.SampleDBPath(t) — the SAME sample the
	// repository defines once. The copy is why the sample's 0444 seal and the
	// sandbox's need to WRITE are not in conflict (see ledger.go).
	LedgerSeedPath string
	// Now overrides the marker's timestamp (tests). Zero means time.Now().
	Now time.Time
	// SkipSettingsDoc suppresses the profile's settings COPY — NOT a link, see
	// settings_doc.go. It exists for exactly one caller: a test whose
	// environment has no readable settings file (this package's own suite).
	// Production callers never set it.
	SkipSettingsDoc bool
	// ReplaceLegacySettingsLink lets the initializer RETIRE a pre-fix settings
	// SYMLINK at the profile path. That shape is what made the operator's live
	// document writable from inside a sandbox (a `>` redirect follows a link —
	// the 2026-09-27 clobber), so the default is a REFUSAL naming the path and
	// the repair: an initializer that silently unlinked it would still leave a
	// window in which the next writer reaches the target. The one caller is the
	// migration test; a human migrating an old sandbox passes it deliberately.
	ReplaceLegacySettingsLink bool
	// PluginsPath is the plugin tree the sandbox's `plugins` entry links to.
	// Default: derived from the ONE resolver (config.WebSearchScriptsDir,
	// climbed to its tree root — see pluginTreeSource), so the sandbox links
	// exactly what a run inside it resolves. A caller that has already decided
	// (a test pinning a fixture tree) passes it here rather than exporting an
	// env var.
	PluginsPath string
}

// Sandbox is a materialised sandbox — the return value of Create/Ensure.
type Sandbox struct {
	// Path is the sandbox directory (the value a caller exports as GOPI_HOME's
	// sibling or hands to a child).
	Path string
	// Root is the configured root it lives under, and RootSource labels the
	// channel that decided the root (config.SandboxRootSource*), so a caller
	// reports where the sandbox came from instead of presenting a default as a
	// discovery.
	Root       string
	RootSource string
	// Created is true when this call made the directory, false when it reused
	// an existing one — the initializer's idempotence, made observable.
	Created bool
	// Reused reports that a PREVIOUS sandbox for the same label was adopted.
	Reused bool
	// SettingsDocSource is the real settings document the profile's
	// `settings.json` COPY was taken from (empty when the copy was suppressed);
	// SettingsDocPath is the copy itself — the path an EXPLICIT GOPI_SETTINGS
	// naming it would read, and EMPTY when the source document does not exist
	// (a legal host: the profile then has no settings entry, and the creation
	// line says so — `settings=<source>(absent-not-linked)`). LedgerPath is the
	// sandbox's real ledger file — the path a process whose GOPI_HOME is Path
	// resolves through config.LedgerPath().
	SettingsDocSource string
	SettingsDocPath   string
	LedgerPath        string
	// PluginsSource is the plugin tree the initializer considered linking and
	// PluginsLink the target it actually linked (empty when that source does not
	// exist). The pair is why an incomplete sandbox is audible at creation time:
	// the caller reports the source it looked for, so "no plugins in this
	// sandbox" is a NAMED fact instead of a search that fails later (Rule 15).
	PluginsSource string
	PluginsLink   string
	// Owner is the marker written for this sandbox.
	Owner Owner
}

// Create materialises a sandbox under root. root == "" resolves the configured
// root through config.ResolveSandboxRoot — the ONE resolver (flag > env >
// settings > built-in default). A caller that has already resolved a root (the
// CLI, which owns the settings-file read) passes it together with the source
// label from that resolution, so the report names the real channel.
//
// It is IDEMPOTENT: with a non-empty Options.Label, a sandbox previously made
// for that label is reused (and repaired by Ensure) rather than duplicated.
// Without a label every call makes a fresh, anonymous sandbox — which is the
// honest behaviour for a caller that cannot say what the sandbox is for.
func Create(root, rootSource string, opts Options) (*Sandbox, error) {
	if strings.TrimSpace(root) == "" {
		root, rootSource = config.ResolveSandboxRoot(nil, nil)
	}
	if strings.TrimSpace(rootSource) == "" {
		rootSource = config.SandboxRootSourceFlag
	}

	if err := os.MkdirAll(root, sandboxRootPerm); err != nil {
		return nil, fmt.Errorf("sandbox: create the root %s: %w", root, err)
	}
	root = scratch.CanonicalPath(root)

	if prior, ok := reusableSandbox(root, opts); ok {
		sb, err := Ensure(prior, root, rootSource, opts)
		if err != nil {
			return nil, err
		}
		sb.Reused = true
		return sb, nil
	}

	dir, err := uniqueSandboxDir(root, opts.Label)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, sandboxDirPerm); err != nil {
		return nil, fmt.Errorf("sandbox: create %s: %w", dir, err)
	}
	sb, err := Ensure(dir, root, rootSource, opts)
	if err != nil {
		return nil, err
	}
	sb.Created = true
	return sb, nil
}

// Ensure brings an existing sandbox directory up to the standard shape and
// returns the resulting Sandbox. It is the repair half of Create, and it is
// what makes "run the initializer twice" a no-op rather than an error: an entry
// that is missing (a GC that was too eager, a hand edit, a run that died
// mid-create) is created; an entry that is already correct is left untouched.
//
// A path that exists and is NOT a directory is refused — silently replacing it
// would destroy whatever the operator put there.
func Ensure(dir, root, rootSource string, opts Options) (*Sandbox, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("sandbox: %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("sandbox: %s exists and is not a directory — refusing to replace it", dir)
	}

	// The private, empty, writable directories. Created (never emptied: an
	// existing session record is the run's, not ours to delete) — the "empty"
	// in the shape's description is about a FRESH sandbox.
	//
	// THEY LIVE INSIDE THE PROFILE ROOT (<dir>/.gopi/...), and that placement is
	// load-bearing: GOPI_HOME is a HOME whose profile root is <GOPI_HOME>/.gopi
	// (config.GopiDir, reached through config.ProfileDirIn), so a sandbox that
	// materialised sessions/ beside this directory would be a shape NO reader
	// resolves — `gopi session log --last` under GOPI_HOME=<sandbox> would print "no
	// such file or directory" while the help text claimed the directory was
	// right there (the divergence this materialisation closes). The join is the
	// readers' OWN function (config.ProfilePathIn, Rule 2/14), so the creator
	// cannot drift from config.GopiDir again.
	profileRoot := config.ProfileDirIn(dir)
	for _, name := range []string{
		config.SandboxSessionsDirName, config.SandboxRawDirName, config.SandboxLogsDirName,
	} {
		p := filepath.Join(profileRoot, name)
		if err := os.MkdirAll(p, sandboxEntryPerm); err != nil {
			return nil, fmt.Errorf("sandbox: create %s: %w", p, err)
		}
	}

	sb := &Sandbox{Path: dir, Root: root, RootSource: rootSource}

	if !opts.SkipSettingsDoc {
		source, path, err := materialiseSettingsDoc(dir, opts)
		if err != nil {
			return nil, err
		}
		sb.SettingsDocSource = source
		sb.SettingsDocPath = path
	}

	// The PLUGIN TREE: <profile>/plugins -> the real tree, the same read-only
	// link contract as settings.json and for the same reason (a copy drifts; a
	// write through the link would mutate the operator's plugins). Without it a
	// sandbox is a profile in which the web-search plugin cannot exist — the
	// measured "dead inside a sandbox" defect this closes.
	pluginsSource, pluginsLink, err := ensurePluginTreeLink(dir, opts)
	if err != nil {
		return nil, err
	}
	sb.PluginsSource, sb.PluginsLink = pluginsSource, pluginsLink

	// The LEDGER: a real, private, writable file at the path the ledger
	// resolver computes inside this sandbox. It replaces the old
	// `memory.db` → shared read-only sample symlink, whose two failure modes
	// (write refused; symlink silently replaced by a writer's rename) are
	// documented in ledger.go.
	ledgerPath, err := materialiseLedger(dir, opts)
	if err != nil {
		return nil, err
	}
	sb.LedgerPath = ledgerPath

	owner := newOwner(dir, opts)
	if err := writeOwner(dir, owner); err != nil {
		return nil, err
	}
	sb.Owner = owner
	return sb, nil
}

// ensureSymlink creates link → target when link is missing, and REPLACES a
// wrong link. It never writes THROUGH an existing link (see the read-only
// contract in the package doc): the only mutation it performs is unlink+link,
// which rewrites the link NODE and leaves the target byte-for-byte untouched.
//
// It serves the PLUGIN TREE link now, and nothing else. The ledger is not a
// link (a write target — ledger.go), and the settings DOCUMENT is not a link
// either any more: settings_doc.go copies it, because a fixture's `>` redirect
// followed the old link and destroyed the operator's live file (2026-09-27).
func ensureSymlink(link, target, what string) error {
	if cur, err := os.Readlink(link); err == nil {
		if cur == target {
			return nil
		}
		if err := os.Remove(link); err != nil {
			return fmt.Errorf("sandbox: replace the stale %s symlink %s: %w", what, link, err)
		}
	} else if !os.IsNotExist(err) {
		// Something that is not a symlink sits there. Refusing is the only safe
		// answer: a regular file at this path may be the operator's own edit.
		return fmt.Errorf("sandbox: %s exists and is not a symlink — refusing to replace it: %w", link, err)
	}
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("sandbox: symlink %s -> %s: %w", link, target, err)
	}
	return nil
}

// reusableSandbox reports the sandbox Create may ADOPT for these options: the
// newest direct child of root whose name carries the label AND whose ownership
// marker proves this very process made it — the idempotence contract, and
// nothing more.
//
// WHY SO NARROW: the initializer's contract is that a caller's sandbox is
// PRIVATE, and the match key used to be the LABEL alone. Measured 2026-10-13,
// two shapes of the same defect:
//
//  1. TWO LIVE RUNS were handed the SAME directory. The second caller's
//     "private profile" was the first one's, and the first one's
//     `t.Cleanup(os.RemoveAll(sb))` then deleted the second's ground
//     mid-flight (refused writes, a vanished ref, a worktree that disappeared).
//  2. Worse, because the initializer runs as a SHORT-LIVED CLI process
//     (`gopi tool sandbox create`): the surviving marker names a pid that has
//     already exited, so the NEXT caller adopted that directory as "the
//     leftover of a gone run" — and deleted the live holder's ground, whose
//     holder knew nothing about it.
//
// So "the previous incarnation of this label is gone" is NOT evidence that the
// directory is free: it is the ordinary state of a sandbox a caller made with a
// one-shot CLI and is still using. Only the caller itself can prove the
// directory is its own, and it can: the marker's (pid, start ticks) pair. A
// caller that wants the same directory again calls with the same label from the
// SAME process — the CLI shape, deliberately: `gopi tool sandbox run` creates
// and executes the child from ONE process, so the label/idempotence key and the
// ownership identity are the same run.
//
// A caller that legitimately needs to re-adopt a sandbox across processes NAMES
// it (an explicit path), which is the one route where the caller, not a
// heuristic, states the identity.
//
// A candidate whose marker cannot be READ, or names this pid with ticks that do
// not match, is skipped for the same one-way reason: "I could not prove this is
// mine" is a keep-away, never a hand-out. The abandoned directory is left to the
// reclaimer that owns that decision (scratch.SweepSandboxes: aged + unowned).
func reusableSandbox(root string, opts Options) (string, bool) {
	label := sanitizeLabel(opts.Label)
	if label == "" {
		return "", false
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false // unreadable root: Create's MkdirAll already failed, or will
	}
	best := ""
	var bestMod time.Time
	for _, e := range entries {
		if !e.IsDir() || !isSandboxForLabel(e.Name(), label) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		dir := filepath.Join(root, e.Name())
		owner, err := ReadOwner(dir)
		if err != nil {
			continue // unreadable marker: claim it for nobody
		}
		if !owner.ThisProcess() {
			continue // not provably ours: another run's ground (live or not)
		}
		if best == "" || info.ModTime().After(bestMod) {
			best, bestMod = dir, info.ModTime()
		}
	}
	return best, best != ""
}

// isSandboxForLabel reports whether a directory name is a sandbox of label: the
// prefix, the label, a separator, and the random suffix — and nothing else, so
// one label can never match another ("build" must not match "builder-…").
func isSandboxForLabel(name, label string) bool {
	rest, ok := strings.CutPrefix(name, sandboxDirPrefix+label+"-")
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// sandboxDirPrefix is the per-sandbox name marker — config.SandboxMarkerPrefix,
// which is the SAME value the reclaimer scans for (Rule 13/14: one definition,
// referenced by both halves, so a rename cannot leave a directory the reclaimer
// no longer recognises).
const sandboxDirPrefix = config.SandboxMarkerPrefix

// SandboxNamePrefix is the name marker of ONE sandbox, exported so a caller
// (the CLI's usage text, a script, a test) can name the pattern without
// re-spelling it (Rule 13: the constant exists to be REFERENCED).
const SandboxNamePrefix = sandboxDirPrefix

// uniqueSandboxDir builds a free name: sandbox-<label>-<rand>.
func uniqueSandboxDir(root, label string) (string, error) {
	if label = sanitizeLabel(label); label == "" {
		label = anonymousLabel
	}
	for attempt := 0; attempt < sandboxNameAttempts; attempt++ {
		buf := make([]byte, randSuffixBytes)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("sandbox: read randomness for the directory name: %w", err)
		}
		dir := filepath.Join(root, sandboxDirPrefix+label+"-"+hex.EncodeToString(buf))
		if _, err := os.Lstat(dir); os.IsNotExist(err) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("sandbox: could not find a free name under %s after %d attempts", root, sandboxNameAttempts)
}

// sandboxNameAttempts bounds the collision retry. With 4 random bytes the
// collision probability is ~2^-32 per attempt, so this bound is unreachable in
// practice — it exists so the loop is bounded by a named constant rather than
// being an unbounded `for {}` (Rule 1a).
const sandboxNameAttempts = 8

// anonymousLabel is the label of a sandbox created without one.
const anonymousLabel = "anon"

// sanitizeLabel keeps the label to [a-z0-9-] so a directory name can never
// carry a path separator, a space or a dot segment — the input is
// operator-supplied, and a name that can escape its parent is a defect.
func sanitizeLabel(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(label)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-_")
}
