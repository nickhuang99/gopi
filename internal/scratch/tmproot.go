// Temp-root probe — the ONE place that answers "where will this process's
// temporary files land, and is that on the `/` partition?".
//
// ROOT CAUSE THIS FILE EXISTS FOR: on this host `/tmp` is a directory on the
// 147G `/` partition (which the 2026-09-11 exhaustion took from 19G to 1.5G
// free, devdoc/zbook-resource-exhaustion-20260911.md) and TMPDIR is UNSET, so
// every child gopi spawns — `gopi task run` children, the bash tool, git —
// resolves its temp workspace to /tmp by IMPLICIT fallback (os.TempDir()'s
// implementation default). 23.5G of residue accumulated there because not one
// process ever asked the question this file answers mechanically.
//
// WHY HERE (internal/scratch) AND NOT internal/testutil: two consumers with
// opposite halves of the contract must share ONE definition —
//
//  1. the SUITE entry point (internal/testutil.PinSuiteForTestMain) needs the
//     measurement to refuse an ambient temp root on `/`, and
//  2. the RUNTIME scratch fallback (RuntimeScratchDir / MkdirTemp, this
//     package) needs it to report when it is about to fall back onto `/`.
//
// internal/testutil imports "testing", so a production import of it would drag
// the test framework into the shipped binary — the same argument that moved the
// GC sweep here (Rule 14: a shared value lives in a small leaf package every
// consumer can import; the device-id measurement and the TMPDIR spelling were
// previously duplicated/private, which is exactly the drift Rule 13 bans).
package scratch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// TmpDirEnvName is the standard environment variable that decides a
	// process's temporary workspace — the ONE definition (Rule 14).
	// internal/testutil.TempDirEnvName is an ALIAS of this constant, so the
	// suite's pin, this probe and every message that names the variable agree
	// on one spelling (a rename is a one-line change, never a grep-and-hope).
	//
	// It is BOTH the variable a CALLER may export (the ambient override, which
	// always wins over a settings file) and the variable GOPI PUBLISHES for its
	// children — that is what makes gopi's injection work at all: each child
	// starts from os.Environ() and this is the name execve hands it.
	TmpDirEnvName = "TMPDIR"

	// GopiTmpDirEnvName is gopi's OWN spelling of the temp-workspace knob — the
	// env channel of the ordinary parameter (catalogued in internal/config as
	// GOPI_TMPDIR / --tmpdir / the settings "tmpdir" key). It is deliberately
	// NOT TmpDirEnvName: gopi WRITES TMPDIR for its children, so a knob named
	// TMPDIR would be both the input and the output of the same mechanism, and a
	// caller's value would read back as gopi's own (Rule 13/14: one definition,
	// referenced — never re-spelled at the config boundary).
	GopiTmpDirEnvName = "GOPI_TMPDIR"

	// TmpDirSourceEnvName carries the LABEL of the channel that produced the
	// published temp workspace — the pair (TmpDirEnvName, TmpDirSourceEnvName)
	// is the whole provenance of what is in force, written together by the ONE
	// publisher (cmd/agent's publishTmpDir) and read together by the ONE
	// reader (`gopi config show`).
	//
	// WHY A SECOND VARIABLE AND NOT A RE-DERIVATION: the temp workspace is
	// resolved ONCE, before the subcommand chain, and the publisher then writes
	// the resolved directory into TmpDirEnvName — so a later reader that ranked
	// the channels again would be asking a question the environment has already
	// answered for it: the ambient arm tests TMPDIR first, and TMPDIR now holds
	// gopi's OWN publication, so every channel would report itself as "the
	// caller's TMPDIR". The label travels with the value instead of being
	// recomputed from a world the publication itself changed (Rule 2: the
	// resolution has one caller; the report is a READER of its outcome).
	TmpDirSourceEnvName = "GOPI_TMPDIR_SOURCE"

	// ProcRootPath is an ALIAS of ProcRootDir (holder.go), where the /proc
	// spelling already lives. It exists because two importers of this package
	// read a child's environ through it and the value must have exactly ONE
	// definition (Rule 13/14): a second literal would drift on a rename.
	ProcRootPath = ProcRootDir
	// procEnvironFileName names the per-process environment block under
	// ProcRootPath.
	procEnvironFileName = "environ"

	// rootDeviceProbePath is the partition the temp-root ban protects: `/`
	// itself. Named so no call site spells the bare path (Rule 1b).
	rootDeviceProbePath = "/"

	// RootDeviceProbePath is the EXPORTED spelling of rootDeviceProbePath
	// (Rule 14: one value, one definition — an alias, never a mirror). A
	// consumer outside this package that must NAME the partition in a refusal
	// (cmd/agent's sandbox precondition on the hermetic test escapes) references
	// it instead of typing "/".
	RootDeviceProbePath = rootDeviceProbePath

	// tmpEnvPairSeparator splits one "NAME=value" entry of the raw environ
	// block. The entries themselves are separated by NUL bytes.
	tmpEnvPairSeparator = "="
	// tmpEnvEntrySeparator is the NUL byte the kernel uses between environ
	// entries.
	tmpEnvEntrySeparator = "\x00"

	// ProbeReportPrefix labels everything this file prints on stderr, so an
	// operator can tell a scratch report from the work a process is doing. It is
	// EXPORTED because cmd/agent's temp-workspace publisher prints the same
	// class of diagnostic and must not re-spell the prefix (Rule 13/14).
	ProbeReportPrefix = "scratch: "

	// BaseEnvName is the base knob a SUITE fixture may point at a private
	// directory — an ALIAS of the ONE definition, ResolveBaseEnvName
	// (base_residue.go), which ResolveBase reads (Rule 2: one reader). The
	// name is kept because the CLI help and the worktree sweep both surface the
	// knob to an operator under it.
	BaseEnvName = ResolveBaseEnvName

	// MemoryBaseEnvName is the memory-backed base the scratch home is LINKED
	// ONTO — HOST configuration, the variable scripts/setup-scratch-home.sh
	// mounts and reads. This constant is the ONE definition of that name
	// (Rule 14): internal/testutil.MemoryBaseEnvName is a TRUE ALIAS of it
	// (`const MemoryBaseEnvName = scratch.MemoryBaseEnvName`), which is what
	// makes the two layers agree by construction rather than by two copies
	// happening to match. The worktree sweep consults it so a base an OPERATOR
	// declared is the base its finding names. It is deliberately NOT
	// ResolveBaseEnvName: that knob is overridden in fixtures, and a production
	// sweep must never report a fixture's base as the operator's declaration.
	MemoryBaseEnvName = "GOPI_SCRATCH_MEMORY_BASE"

	// tmpfsMagic / ramfsMagic (the Linux statfs magic numbers of the two MEMORY
	// filesystems a scratch tree may legitimately live on) are declared in
	// rootgocache.go — this package's ONE definition site per value (Rule 14) —
	// so this file's classifier and rootgocache.go's placement assertion branch
	// on the same pair instead of two copies drifting apart (Rule 13).
)

// Filesystem kinds a caller may branch on. They are returned by
// ClassifyFilesystemType so a policy can say WHICH filesystem decided it
// (Rule 15: name the cause, never leave the reader guessing).
const (
	FilesystemTmpfs  = "tmpfs"
	FilesystemRamfs  = "ramfs"
	FilesystemOthers = "other"
)

// ClassifyFilesystemType maps a statfs f_type to one of the kind constants.
// It is the ONE classifier: an unknown type is FilesystemOthers, never a
// guessed memory filesystem (a wrong "tmpfs" would authorise a removal on a
// disk-backed mount).

// ErrTempRootOnRootFS is the sentinel a caller can match with errors.Is when
// the effective temp root shares a filesystem with `/` (Rule 1b: the message
// is built from constants, never re-spelled at each site).
var ErrTempRootOnRootFS = errors.New("temp root is on the same filesystem as /")

// TmpDirSettingsKey is the settings.json key carrying the temp-workspace root
// gopi and its children must use ("tmpdir": "<dir off />"). It is defined HERE
// — the leaf package both consumers can import (Rule 14) — because two of them
// name it in text a user reads and must not drift: cmd/agent's injection
// message and this package's refusal. internal/config.TmpDirSettingsKey is the
// alias the config-side code uses.
const TmpDirSettingsKey = "tmpdir"

// DeviceID returns the kernel's filesystem identifier of path — the key
// statfs reports for `/`. Two paths on the same identifier share one
// filesystem, so their bytes compete for one partition's space; that is the
// property the temp-root ban is about. An unreadable path is an error, never a
// guess (Rule 15).
//
// The path is RESOLVED first (EvalSymlinks): the unprivileged scratch setup
// reaches the memory-backed tree through a symlink
// (scripts/setup-scratch-home.sh), and syscall.Statfs reports the device of the
// FINAL target either way — the explicit resolution is what keeps that fact
// from being an accident of the libc call. A resolution failure falls back to
// the literal path (a not-yet-created path has nothing to resolve).
// statfsFacts is what one statfs call tells the temp-root probes, already
// NORMALIZED to this tree's scalar types.
//
// WHY THE SHAPE LIVES HERE AND THE CALL DOES NOT: four probes in this file need
// the same five numbers, but the platforms report them in DIFFERENT structs —
// linux's Statfs_t.Type is int64 while darwin's is uint32, darwin's Fsid names
// its field Val and linux's names it X__val, and darwin's Bsize is uint32
// against linux's int64 (checked with `go doc syscall.Statfs_t` for each GOOS,
// not from memory). Folding once here is what lets DeviceID, FreeBytes,
// FilesystemTypeAt and statfsTmpDirHeadroom stay platform-free, so a platform
// that cannot measure is ONE named refusal (statfsAt, in the per-platform file)
// rather than four copies of the same split.
type statfsFacts struct {
	deviceID uint64 // the Fsid pair folded into one number
	fsType   int64  // f_type, widened where the platform reports it unsigned
	bsize    int64  // block size, kept SIGNED so a nonsense value is detectable
	blocks   uint64 // total blocks
	bavail   uint64 // blocks available to a writer that is not root
}

func DeviceID(path string) (uint64, error) {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		target = resolved
	}
	f, err := statfsAt(target)
	if err != nil {
		return 0, err
	}
	return f.deviceID, nil
}

// RootDeviceID is the filesystem identifier of `/` — the partition this probe
// protects.
func RootDeviceID() (uint64, error) { return DeviceID(rootDeviceProbePath) }

// FreeBytes reports the bytes an unprivileged writer may still lay down at path
// (statfs Bavail × Bsize — the margin for root's reserved blocks is deliberately
// excluded, because the caller asking is not root).
//
// WHY A MEASUREMENT AND NOT A SPELLING: the placement decision in
// internal/testutil must never pick a scratch root that cannot hold a run, and
// the only thing that answers "can it" is the kernel's own number. On this host
// that mattered immediately: /dev/shm sat at 98% (160 MiB free of 7.8 GiB) with
// 146 suite roots in it, and a run placed there failed as a MISLEADING git error
// ("is not a git work tree" for a directory the suite had just created) rather
// than as "there is no room". A path spelling cannot be wrong about that; a
// number can be, so it is measured and printed (Rule 15).
//
// An unreadable path is an error, never a guess.
func FreeBytes(path string) (uint64, error) {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		target = resolved
	}
	f, err := statfsAt(target)
	if err != nil {
		return 0, err
	}
	if f.bsize <= 0 {
		// A nonsense geometry is reported as an error rather than multiplied
		// into a negative-turned-huge number (Rule 15: no silent failure).
		return 0, fmt.Errorf("statfs(%s): implausible block size %d", target, f.bsize)
	}
	return f.bavail * uint64(f.bsize), nil
}

// The free-space floors the suite's scratch placement and its guard measure
// against. They are VALUES with meaning (decided on, printed, refused on), so
// each has exactly one definition — here, the leaf package both the placement
// (internal/testutil) and any production caller can import (Rule 14).
const (
	// MinMemoryPlacementFreeBytes is the free space a MEMORY filesystem must
	// still have before the suite may place its run root there.
	//
	// WHY A MEMORY BASE NEEDS ITS OWN FLOOR (measured on this host,
	// 2026-09-28): /dev/shm is 7.8 GiB and was at 98% — 160-192 MiB free —
	// with 146 "suite-<pkg>-run-<N>" roots (5.9 GiB) that no sweep collected.
	// A run placed there does not fail as "no room": the suite's first
	// directory creation succeeds (tmpfs keeps the small trees) and the FIRST
	// LARGER ONE fails, so the observable symptom is a git/database error about
	// a path the suite just created ("… is not a git work tree", "database or
	// disk is full") — a false code regression across 7-8 cmd/agent E2E tests.
	// The placement therefore REFUSES a base with less room than a run needs
	// and takes the reported device arm instead, printing the measured bytes
	// (Rule 15: the decision is never silent).
	//
	// 1 GiB is chosen against the measured size of one suite run's root on this
	// host (cmd/agent: 57 MiB, 88 MiB, 297 MiB observed; internal/tools
	// larger), so the floor leaves room for a run plus the growth of the roots
	// a concurrent suite and the age GC are still working on. It is a FLOOR,
	// not a quota: a base with more room than this is still swept by
	// gopi tool scratch-gc base.
	MinMemoryPlacementFreeBytes uint64 = 1 << 30

	// MinSuiteRootFreeBytes is the free space a suite's own scratch root must
	// have before the run starts. It is deliberately SMALLER than the
	// placement floor: this one guards the root the run already chose (a
	// caller's pin, a memory base that passed the floor, a device arm), and its
	// job is to turn "the disk is full" into a named, bounded failure rather
	// than a misleading one — a suite that runs out mid-flight pays with
	// unrelated-looking test failures (the /tmp 13,044-directory incident and
	// the /dev/shm ENOSPC one).
	MinSuiteRootFreeBytes uint64 = 512 << 20

	// bytesPerGiB is the divisor FormatBytes reports at or above; the smaller
	// units (bytesPerKiB / bytesPerMiB) are the ONE definitions in limits.go,
	// reused here rather than redeclared (Rule 13: constants exist to be
	// referenced, and a second copy is drift waiting to happen).
	bytesPerGiB = 1 << 30
)

// ErrInsufficientSpace is the sentinel a caller can match with errors.Is when a
// scratch root has less free space than a suite run requires.
var ErrInsufficientSpace = errors.New("insufficient free space for a suite scratch root")

// FormatBytes renders a byte count for a human-readable message ("160.0 MiB"),
// so every refusal prints its numbers the same way (Rule 13: one spelling).
func FormatBytes(n uint64) string {
	switch {
	case n >= bytesPerGiB:
		return fmt.Sprintf("%.1f GiB", float64(n)/float64(bytesPerGiB))
	case n >= bytesPerMiB:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(bytesPerMiB))
	case n >= bytesPerKiB:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(bytesPerKiB))
	}
	return fmt.Sprintf("%d bytes", n)
}

// SignedBytes renders a SIGNED byte delta for a message that reports a CHANGE
// ("free 2.4 GiB → 3.6 GiB"), on the same unit scale as FormatBytes so the two
// readings in one sentence cannot be mistaken for different quantities a reader
// would have to convert. A delta IS signed — a reclaim can fail to move the
// filesystem's free space when other writers on it fill the gap — and rendering
// it with FormatBytes' uint64 signature would either lose the sign or wrap it to
// an absurd magnitude, which is precisely the kind of confident wrong number
// Rule 15 forbids.
func SignedBytes(delta int64) string {
	if delta < 0 {
		return "-" + FormatBytes(uint64(-delta))
	}
	return "+" + FormatBytes(uint64(delta))
}

// AssertFreeSpace is the BOUNDED PREFLIGHT a suite runs before it creates its
// scratch root: it measures the free space of the directory the root will live
// in and refuses — naming the path, the filesystem and BOTH byte counts — when
// there is less than want.
//
// WHY IT EXISTS (the misleading-failure defect, measured 2026-09-28): with the
// memory base at 98%, a suite's root was created successfully and its first
// larger artifact failed, so the run reported `repo "/dev/shm/suite-cmd-agent-
// run-<N>/gopi-repo-ctx-<M>" is not a git work tree (git rev-parse failed)` — a
// message about a directory the suite itself had just made, which reads as a
// code regression and cost a diagnosis session. A guard that names the measured
// cause is the fix; the alternative (let it fail, and read the misleading error)
// is what Rule 15 forbids.
//
// An unmeasurable path is a refusal, never a pass: a root whose space cannot be
// measured is a root this run cannot promise to finish in.
func AssertFreeSpace(path string, want uint64) error {
	free, err := FreeBytes(path)
	if err != nil {
		return fmt.Errorf("scratch: cannot measure the free space of %s — refusing to place a suite root there: %w", path, err)
	}
	if free >= want {
		return nil
	}
	_, description, memErr := MemoryFilesystem(path)
	if memErr != nil {
		description = "an unreadable filesystem" // reported, never guessed (Rule 15)
	}
	hint := "free space and re-run; the sweep that bounds this tree is `gopi tool scratch-gc`"
	if strings.Contains(description, "memory") {
		// A memory base is a SHARED, small filesystem whose residue has its own
		// arm: name the exact command that reclaims it, so the reader is not
		// left to work out which of the sweeps owns this tree.
		hint = "reclaim it with `gopi tool scratch-gc base` (the sweep that collects this system's own residue in the memory base), or point " +
			ScratchHomeEnvNameForHint + " at a root with room"
	}
	return fmt.Errorf("%w: %s has %s free (needs at least %s) — the filesystem is %s. "+
		"A suite that runs here fails later as an unrelated-looking error about a path it just created "+
		"(the measured /dev/shm shape: \"is not a git work tree\", \"database or disk is full\"), so it refuses now instead: %s",
		ErrInsufficientSpace, path, FormatBytes(free), FormatBytes(want), description, hint)
}

// ScratchHomeEnvNameForHint is the suite-side variable that relocates the
// scratch home, spelled here (the leaf package) so this message and
// internal/testutil's own constant cannot drift apart (Rule 14).
const ScratchHomeEnvNameForHint = "GOPI_TEST_SCRATCH_HOME"

// SharesRootFS reports whether path lives on the same filesystem as `/` (true
// for /tmp on a default host). An error means the question could not be
// answered; callers must treat that as "not proven safe", never as "safe".
func SharesRootFS(path string) (bool, error) {
	root, err := RootDeviceID()
	if err != nil {
		return false, err
	}
	got, err := DeviceID(path)
	if err != nil {
		return false, err
	}
	return got == root, nil
}

// ProcEnviron reports whether NAME is present in a raw /proc/<pid>/environ
// block — the kernel's record of what a process was HANDED at exec. It returns
// the value, whether the name was present at all, and an error when the block
// could not be parsed. An empty present value is NOT an error: it is a caller
// who deliberately blanked the variable, which os.TempDir() also treats as
// unset for its own fallback — the two facts are kept distinct rather than
// rendered as one "" (which is what os.Getenv does).
func ProcEnviron(raw []byte, name string) (string, bool, error) {
	if name == "" {
		return "", false, errors.New("scratch: ProcEnviron: empty variable name")
	}
	for _, entry := range splitNulEntries(string(raw)) {
		key, value, found := cutEnvPair(entry)
		if !found || key != name {
			continue
		}
		return value, true, nil
	}
	return "", false, nil
}

// splitNulEntries splits a raw environ block into its "NAME=value" entries.
func splitNulEntries(block string) []string {
	var out []string
	start := 0
	for i := 0; i < len(block); i++ {
		if block[i] != tmpEnvEntrySeparator[0] {
			continue
		}
		if entry := block[start:i]; entry != "" {
			out = append(out, entry)
		}
		start = i + 1
	}
	if tail := block[start:]; tail != "" {
		out = append(out, tail)
	}
	return out
}

// cutEnvPair splits one environ entry at its FIRST "=" (a value may contain
// "="; the name may not).
func cutEnvPair(entry string) (name, value string, found bool) {
	for i := 0; i < len(entry); i++ {
		if entry[i] != tmpEnvPairSeparator[0] {
			continue
		}
		return entry[:i], entry[i+1:], true
	}
	return "", "", false
}

// procEnvironReadLimit bounds the raw-environment read. The block comes from
// /proc, so the kernel reports no size up front and the read must be capped
// rather than allocated whole — 1 MiB is far above any real environ block (the
// read is a single call, and a truncated tail can only ever hide a variable
// this probe does not depend on).
const procEnvironReadLimit = 1 << 20

// ProcEnvironPathFor returns the raw-environment path of a live process id —
// the child-side evidence a test reads (see EffectiveTempRoot for why a runtime
// os.Setenv makes the SELF path useless for presence).
func ProcEnvironPathFor(pid int) string {
	return filepath.Join(ProcRootPath, strconv.Itoa(pid), procEnvironFileName)
}

// ClassifyFilesystemType classifies one statfs f_type value; the kinds are the
// Filesystem* constants. Both the suite's scratch-root gate and the worktree
// sweep's memory-base measurement go through here, so "is this a memory
// filesystem?" has exactly one answer in the tree.
func ClassifyFilesystemType(fType int64) (string, bool) {
	switch uint64(uint32(fType)) {
	case tmpfsMagic:
		return FilesystemTmpfs, true
	case ramfsMagic:
		return FilesystemRamfs, true
	}
	return FilesystemOthers, false
}

// FilesystemTypeAt reports the statfs kind of path (resolved first, so a tree
// reached through a symlink is classified by its target — the same rule the
// device probe follows).
func FilesystemTypeAt(path string) (string, bool) {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		target = resolved
	}
	var st statfsFacts
	var err error
	if st, err = statfsAt(target); err != nil {
		return FilesystemOthers, false
	}
	return ClassifyFilesystemType(st.fsType)
}

// SettingsEnvName is the variable that relocates the settings file (the same
// one internal/config reads). Named here so the reader below and the config
// layer cannot drift (Rule 13).
const SettingsEnvName = "GOPI_SETTINGS"

// GopiDirEnvName relocates the gopi data root (/home/nick/.gopi). Named for the same
// reason.
const GopiDirEnvName = "GOPI_HOME"

// gopiDirName is the data directory under the home that holds settings.json.
const gopiDirName = ".gopi"

// SettingsFileName is the settings file inside the gopi data root.
const SettingsFileName = "settings.json"

// DeclaredTmpDir reports the settings.json "tmpdir" declaration — the ONE
// reader (Rule 2) of the operator's statement of where temp work belongs
// (Rule 22's explicit layer). Empty when nothing is declared or the file cannot
// be read: a declaration is a NAME an operator chose, never the authority the
// measurement above carries.
//
// It exists in this leaf package because two consumers need it and neither may
// import the other: internal/config owns the full settings model, while
// internal/tools must stay free of it (the sweep already breaks that import in
// only one direction). A missing/unparsable file is not an error here — the
// caller's measurement decides, and a declared name is only ever used to LABEL
// the measured base.
func DeclaredTmpDir() string {
	raw, err := os.ReadFile(settingsFilePath())
	if err != nil {
		return ""
	}
	var doc struct {
		TmpDir string `json:"tmpdir"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	return strings.TrimSpace(doc.TmpDir)
}

// settingsFilePath resolves which settings.json to read: the GOPI_SETTINGS
// override, else <GOPI_HOME|$HOME>/.gopi/settings.json. "" when no home can be
// resolved (the caller then has no declaration).
func settingsFilePath() string {
	if p := strings.TrimSpace(os.Getenv(SettingsEnvName)); p != "" {
		return p
	}
	root := strings.TrimSpace(os.Getenv(GopiDirEnvName))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		root = home
	}
	return filepath.Join(root, gopiDirName, SettingsFileName)
}

// ReadProcEnviron reads a live process's RAW environment block (one read, no
// interpretation) — the kernel's record of what that process was HANDED at
// exec, which is what makes it inheritance evidence rather than a claim (a
// child printing its own variable would be reporting on itself; the block
// cannot lie about what it received). A read failure is reported, never
// swallowed (Rule 15b) — the caller then cannot answer the question and must
// say so rather than guess.
func ReadProcEnviron(pid int) ([]byte, error) {
	path := ProcEnvironPathFor(pid)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("scratch: open %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			// Rule 15b: a close failure on /proc is surfaced, not buried —
			// this is a read-only descriptor, so the only cost is a line.
			fmt.Fprintf(os.Stderr, "%sclose %s: %v\n", ProbeReportPrefix, path, cerr)
		}
	}()
	buf := make([]byte, procEnvironReadLimit)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, fmt.Errorf("scratch: read %s: %w", path, err)
	}
	return buf[:n], nil
}

// EffectiveTempRoot answers the two questions the ban needs at once: WHICH path
// this process's temporary workspace resolves to, and whether that came from an
// EXPLICIT TMPDIR or from the implementation default (the implicit fallback).
//
// The path is read from os.TempDir() — the ONE function that owns the
// "TMPDIR set and non-empty, else the implementation default" rule — so this
// can never disagree with what a caller of t.TempDir()/os.MkdirTemp(""…) gets.
// Presence is read from os.LookupEnv, which distinguishes a set-but-empty
// variable from an absent one; os.Getenv cannot (it renders both ""), and an
// empty TMPDIR is a fallback whatever the caller meant by it.
//
// MEASURED CONSTRAINT (2026-09-14): /proc/self/environ is the EXEC-TIME block,
// so it still reports the pre-injection environment after an os.Setenv — using
// it here would make gopi's own injection read back as "unset". It is the right
// evidence one level down, for a CHILD's inheritance (ProcEnvironPathFor +
// ProcEnviron), which is what the acceptance test reads.
func EffectiveTempRoot() (path string, explicit bool, err error) {
	if v, present := os.LookupEnv(TmpDirEnvName); present && v != "" {
		return os.TempDir(), true, nil
	}
	return os.TempDir(), false, nil
}

// IsTempRootOffRootFS reports whether path is a temp root OFF the `/`
// partition — the MEASURED sandbox predicate, not a spelling test (Rule 22: a
// path is never a sandbox "because it is not /tmp"; a different directory on
// the same partition is the same defect).
//
// It answers two questions in the documented order, because they are not the
// same question:
//
//  1. THE IMPLEMENTATION'S OWN TEMP ROOT. When path IS the effective temp root
//     (an explicit TMPDIR, or whatever os.TempDir() reports when TMPDIR is
//     absent), the answer is AssertTempRootOffRootFS's — the one definition of
//     "the process's temp workspace is off `/`", together with its refusal
//     text. A caller that would otherwise proceed on that root has already been
//     told, in that refusal's own words, what to fix.
//  2. ANY OTHER PATH. Measured against the device id of `/`. A path that does
//     not exist yet is judged on its nearest EXISTING ancestor (existingAncestor
//     — the same rule the temp-root ban uses, so a not-yet-created sandbox is
//     judged on the filesystem that would hold it).
//
// "Cannot tell" is never "safe" (Rule 15): an unmeasurable path returns false
// with the measurement error, so a caller refuses.
//
// WHY THIS LIVES HERE (Rule 14): the consumers are the production binary
// (cmd/agent binds the hermetic test escapes to a sandbox) and internal/testutil
// (which may only alias — it imports "testing"), and the device-id measurement
// already has exactly one definition in this package. A second spell of "is it
// off `/`" is the drift Rules 13/14 ban.
func IsTempRootOffRootFS(path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("no path given — cannot measure whether a sandbox root is off %s", rootDeviceProbePath)
	}
	if effective, _, err := EffectiveTempRoot(); err == nil && CanonicalPath(path) == CanonicalPath(effective) {
		return AssertTempRootOffRootFS() == nil, nil
	}
	rootDev, err := RootDeviceID()
	if err != nil {
		return false, fmt.Errorf("cannot measure the filesystem of %s — refusing to assume %q is safe: %w",
			rootDeviceProbePath, path, err)
	}
	dev, err := DeviceID(existingAncestor(path))
	if err != nil {
		return false, fmt.Errorf("cannot measure the filesystem of %q — refusing to assume it is safe: %w", path, err)
	}
	return dev != rootDev, nil
}

// AssertTempRootOffRootFS is the mechanical temp-root ban: it measures the
// device of the EFFECTIVE temp root (explicit TMPDIR or the implicit fallback)
// against the device of `/` and returns ErrTempRootOnRootFS (wrapped, with the
// measured ids, the explicit/implicit classification and the fix) when they are
// the same filesystem. An unmeasurable root is an error too — "cannot tell" is
// never "safe" (Rule 15).
//
// Both branches are checked with the SAME measurement: an EXPLICIT override is
// allowed exactly as long as it is off `/` (an operator who pins TMPDIR at
// /dev/shm or the workspace passes), while the IMPLICIT fallback fails when the
// implementation default is `/tmp` on `/` — which is this host's whole problem,
// and the case a warning never fixed (2026-09-11: 13,044 abandoned scratch
// directories on the boot device).
func AssertTempRootOffRootFS() error {
	path, explicit, err := EffectiveTempRoot()
	if err != nil {
		return fmt.Errorf("%w: cannot determine the temp root: %w", ErrTempRootOnRootFS, err)
	}
	rootDev, err := RootDeviceID()
	if err != nil {
		return fmt.Errorf("temp root %q: cannot measure the filesystem of %s — refusing to assume it is safe: %w",
			path, rootDeviceProbePath, err)
	}
	dev, err := DeviceID(path)
	if err != nil {
		return fmt.Errorf("temp root %q: cannot measure its filesystem — refusing to assume it is safe: %w", path, err)
	}
	if dev != rootDev {
		return nil
	}
	source := implicitTempRootClause
	if explicit {
		source = explicitTempRootClause
	}
	return fmt.Errorf(
		"temp root %q is on the SAME filesystem as %s (device 0x%x): %s. "+
			"%s is on that partition on this host, and it is the partition the 2026-09-11 exhaustion filled "+
			"(unbounded /tmp writers + 13,044 abandoned scratch dirs; / went from 19G to 1.5G free, then 23.5G of "+
			"residue accumulated there again). FIX: point the temp workspace off `/` — put a %q key in settings.json "+
			"(gopi injects it into its own environment so every child inherits it), export %s=<dir under %s>, or make "+
			"the scratch home exist: %s. %w",
		path, rootDeviceProbePath, dev, source, ambientTempRootPath, TmpDirSettingsKey, TmpDirEnvName, WorkspaceDirName,
		scratchHomeMakeHint(), ErrTempRootOnRootFS)
}

// existingAncestor returns the longest existing prefix of path (path itself
// when it exists) — the thing to MEASURE when the path a policy is about has
// not been created yet.
//
// WHY THE MEASUREMENT MUST COME FIRST: the policy here is "never write temp
// data onto `/`". A design that creates the directory and then measures whether
// it is on `/` has already committed the defect it was checking for. The
// nearest existing ancestor is on the same filesystem as the path that would
// be created under it (a mount point is the only exception, and a mount point
// is an ancestor that exists — so a path whose nearest existing ancestor is a
// mount point measures that mount, which is the correct reading). An empty
// path or `/` is returned as-is: `/` always exists.
func existingAncestor(path string) string {
	if path == "" {
		return rootDeviceProbePath
	}
	cur := filepath.Clean(path)
	for {
		if _, err := os.Stat(cur); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return cur // the filesystem root, which always exists
		}
		cur = parent
	}
}

// AssertPathOffRootFS is AssertTempRootOffRootFS's path-scoped sibling: it
// answers "may this directory hold temp data?" for a specific directory that
// need not exist yet (the answer is measured on its nearest existing ancestor
// — see existingAncestor for why the question must be answered BEFORE anything
// is created). It returns ErrTempRootOnRootFS (wrapped, with the path, the
// measured device ids and the fix) when the path shares a filesystem with `/`,
// and a plain error when the path could not be measured at all — "cannot tell"
// is never "safe" (Rule 15).
func AssertPathOffRootFS(path string) error {
	if path == "" {
		return fmt.Errorf("scratch: refusing to answer for an empty path — an unmeasurable temp root is never safe")
	}
	rootDev, err := RootDeviceID()
	if err != nil {
		return fmt.Errorf("scratch: cannot measure the filesystem of %s — refusing to assume %q is safe: %w",
			rootDeviceProbePath, path, err)
	}
	probe := existingAncestor(path)
	dev, err := DeviceID(probe)
	if err != nil {
		return fmt.Errorf("scratch: cannot measure the filesystem of %q (nearest existing ancestor %q) — refusing to assume it is safe: %w",
			path, probe, err)
	}
	if dev != rootDev {
		return nil
	}
	return fmt.Errorf(
		"%q is on the SAME filesystem as %s (device 0x%x, measured on %q): temp data written there competes with the boot "+
			"partition — the 2026-09-11 exhaustion site (/ went from 19G to 1.5G free, then 23.5G of /tmp residue "+
			"accumulated again, and 4.0G more that nothing ever reaped). FIX: point the temp workspace off `/` — put a %q key "+
			"in settings.json (gopi injects it into its own environment so every child inherits it) or export %s=<dir off />: %w",
		path, rootDeviceProbePath, dev, probe, TmpDirSettingsKey, TmpDirEnvName, ErrTempRootOnRootFS)
}

// DefaultTmpDir is the temp workspace gopi publishes when NEITHER a CLI flag,
// NOR the environment (GOPI_TMPDIR / a caller's TMPDIR) NOR settings.json's
// `tmpdir` key names one — an ORDINARY directory under the workspace
// (<home>/workspace/_tmp), created on demand.
//
// WHY AN ORDINARY, VISIBLE DIRECTORY (the operator's decision, 2026-09-16):
// the workspace is the operator's stated working space, so a fresh checkout with
// no settings must behave the way they expect. The previous default was the
// scratch home's `tmp` child, which on this host is reached through a symlink
// into /dev/shm — a tuned, test-oriented location. Tuning is what the
// configuration channels are for (--tmpdir / GOPI_TMPDIR / the settings key);
// the default is the obvious user-facing one. This does NOT re-open the "which
// filesystem is faster" question (closed): it is a decision about which
// directory a DEFAULT names, and it is the operator's.
//
// WHAT IS NOT RELAXED: the root is created through this package, so every
// consumer resolves it with the same code, and it is created with DirPerm. A
// default that cannot be created or written is a LOUD error naming the path, the
// reason, and the fact that no channel configured it (TmpDirUnusableError) —
// gopi never quietly falls back to the ambient /tmp, which is the fallback that
// produced the residue the 2026-09-16 epic closed.
func DefaultTmpDir() (string, error) {
	dir := filepath.Join(WorkspaceUnderHome(), DefaultTmpDirLeaf)
	if err := ensureUsableTempRoot(dir); err != nil {
		var unusable *TmpDirUnusableError
		if errors.As(err, &unusable) {
			unusable.Source = TmpDirSourceNothingConfigured
		}
		return "", err
	}
	// Canonical spelling: a workspace reached THROUGH a symlink (or a home that
	// is one) would otherwise make a chdir'd child report a path differing from
	// the one this function returned.
	return CanonicalPath(dir), nil
}

// TmpDirName remains the SUITE fixture root's leaf: internal/testutil pins
// TMPDIR to a child of <scratch home>/tmp. It is documented here because the
// value lives in scratch.go — one definition (Rule 14), referenced by every
// consumer.

// The two facts a caller may need to state when a temp root is unusable.
// ErrTmpDirUnusable is the sentinel (errors.Is-matchable, Rule 1b: built from
// constants, never re-spelled).
var ErrTmpDirUnusable = errors.New("tmpdir is not usable as a temp workspace")

// tmpDirProbeFilePattern names the file the writability probe creates inside the
// candidate root. It is a dotfile with a unique tail (os.CreateTemp fills it),
// removed immediately: the probe must observe WRITE, not leave residue.
const tmpDirProbeFilePattern = ".gopi-tmpdir-probe-*"

// TmpDirSourceNothingConfigured labels a refusal whose value came from the
// built-in default (no channel named one) — one spelling for every caller.
const TmpDirSourceNothingConfigured = "the built-in default (no --tmpdir, no " + TmpDirEnvName + ", no " + TmpDirSettingsKey + " key)"

// TmpDirUnusableError is the LOUD, specific failure a temp root that cannot be
// provided or written raises (Rule 15: a silent fallback to the ambient root is
// exactly how 23.5G of /tmp residue accumulated unnoticed). It names the PATH
// the operator asked for, the REASON (permission / not-a-directory / create
// error), and WHICH channel supplied the value — the three facts needed to fix
// it, in one line.
type TmpDirUnusableError struct {
	// Path is the resolved temp root that could not be made usable.
	Path string
	// Source names the channel the value came from (a CLI flag, the environment
	// or the settings key) — "" means the value was not configured at all.
	Source string
	// Reason is the underlying cause (the failed syscall's error text).
	Reason string
}

// Error renders the refusal: path, reason, source and the fix, one line.
func (e *TmpDirUnusableError) Error() string {
	source := e.Source
	if source == "" {
		source = TmpDirSourceNothingConfigured
	}
	return fmt.Sprintf("%v: %q: %s (value %s); point --tmpdir / %s / the %q settings key at a writable directory",
		ErrTmpDirUnusable, e.Path, e.Reason, source, TmpDirEnvName, TmpDirSettingsKey)
}

// Unwrap exposes the sentinel so a caller can match the cause with errors.Is
// (the same contract ErrTempRootOnRootFS carries).
func (e *TmpDirUnusableError) Unwrap() error { return ErrTmpDirUnusable }

// ensureUsableTempRoot makes dir usable as the temp workspace: it must be (or
// become) a directory and it must accept a write. Both halves are the D21
// contract ("a temp root that cannot be provided or written fails LOUDLY naming
// path + reason"), and both are checked here — the ONE place — so the default
// and a configured value cannot diverge in what they accept.
//
// The probe creates and removes one file rather than calling access(2): the
// question is "can this process WRITE here", and only a write answers it (a
// mode-bit check would report the intent, not the effect — an ACL, a read-only
// mount or a full filesystem all pass a mode-bit reading).
func ensureUsableTempRoot(dir string) error {
	if err := MkdirAllWithSuiteRunOwnerPerm(dir, DirPerm); err != nil {
		return &TmpDirUnusableError{Path: dir, Reason: fmt.Sprintf("cannot create it: %v", err)}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return &TmpDirUnusableError{Path: dir, Reason: fmt.Sprintf("cannot stat it: %v", err)}
	}
	if !info.IsDir() {
		return &TmpDirUnusableError{Path: dir, Reason: "it exists but is not a directory"}
	}
	probe, err := os.CreateTemp(dir, tmpDirProbeFilePattern)
	if err != nil {
		return &TmpDirUnusableError{Path: dir, Reason: fmt.Sprintf("it is not writable: %v", err)}
	}
	name := probe.Name()
	if cerr := probe.Close(); cerr != nil {
		// Rule 15b: a close failure is surfaced, never swallowed — its only
		// effect is a line, and a swallowed one would hide a real I/O error on
		// the very filesystem under test.
		fmt.Fprintf(os.Stderr, "%sclose %s: %v\n", ProbeReportPrefix, name, cerr)
	}
	if rerr := os.Remove(name); rerr != nil {
		fmt.Fprintf(os.Stderr, "%sremove %s: %v\n", ProbeReportPrefix, name, rerr)
	}
	return nil
}

// ResolvePublishedTmpDir answers the ONE question the temp-workspace knob has:
// WHICH directory will gopi publish as TMPDIR for itself and every child it
// spawns? It takes the value a configuration channel produced ("" when no
// channel named one) plus a SOURCE label for the refusal text, and returns the
// canonical, PROVEN-USABLE path.
//
// configured == "" is the DEFAULT case: the built-in default under the
// workspace (DefaultTmpDir). A configured value is used exactly as given (it is
// the operator's decision) but must pass the same usability probe — a path that
// cannot be provided or written is a LOUD failure naming path + reason + the
// channel that supplied it (D21), never a silent fallback to the ambient root.
//
// The returned SOURCE-TO-USE is the second result: the configured channel's
// label, or TmpDirSourceBuiltInDefault when no channel supplied one — a report
// that says where the directory came from must not print an empty parenthesis
// precisely in the case an operator is most likely to be asking about (nothing
// configured).
func ResolvePublishedTmpDir(configured, source string) (dir, resolvedSource string, err error) {
	if strings.TrimSpace(configured) == "" {
		d, derr := DefaultTmpDir()
		return d, TmpDirSourceBuiltInDefault, derr
	}
	dir = filepath.Clean(strings.TrimSpace(configured))
	if uerr := ensureUsableTempRoot(dir); uerr != nil {
		var unusable *TmpDirUnusableError
		if errors.As(uerr, &unusable) {
			unusable.Source = source
		}
		return "", source, uerr
	}
	return CanonicalPath(dir), source, nil
}

// TmpDirSourceBuiltInDefault labels a report whose value came from the built-in
// default (no channel named one). It is the report's spelling of the same fact
// the refusal renders as TmpDirSourceNothingConfigured — the report is a
// one-line hint, the refusal has to name the fix.
const TmpDirSourceBuiltInDefault = "from the built-in default"

// ReportPublishedTmpDirIfVerbose is the ONE deliberate surface where the
// effective temp workspace is readable after the unconditional banner was
// removed. It prints on stderr ONLY when the operator asked for it, through the
// EXISTING debug knob (GOPI_DEBUG, set by --debug / the settings `debug` key —
// no second mechanism was invented for this):
//
//	GOPI_DEBUG=1 gopi … → "[gopi] tmpdir: TMPDIR=… (from …)"
//
// The inference is that an operator debugging a run wants the resolved paths
// the run is using, and the temp root is one of them. An ordinary run stays
// silent: the value is a parameter with a default, not an announcement.
//
// dir is the resolved root, source labels the channel ("from --tmpdir", "from
// the caller's TMPDIR", …). A quiet run does nothing at all — not a write.
func ReportPublishedTmpDirIfVerbose(dir, source string) {
	if !EnvDebugEnabled() {
		return
	}
	fmt.Fprintf(os.Stderr, "%s %s=%q (%s)\n", TmpDirReportPrefix, TmpDirEnvName, dir, source)
}

// TmpDirReportPrefix labels the opt-in report so an operator can tell it from
// the run's own output (Rule 1b: the prefix is a constant, never re-spelled).
const TmpDirReportPrefix = "[gopi] tmpdir:"

// EnvDebugEnabled reports whether the operator asked for verbose/debug output.
// It is the ONE reader (Rule 2) of the debug knob's environment variable:
// cmd/agent's temp-workspace report consults it, and internal/agent's own
// debug logging keeps its own established reader (deepseek.DebugLogging) — the
// value is the same variable, so the two surfaces agree by construction.
//
// The name is spelled here once (Rule 14) because two packages must not drift
// on it.
func EnvDebugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(DebugEnvName))) {
	case EnvDebugOn, "true", "yes", "on", "enabled":
		return true
	}
	return false
}

// DebugEnvName / EnvDebugOn are the debug knob's two spellings: the variable
// name and its ON value. They mirror the config catalog entry (GOPI_DEBUG is
// catalogued there as the "debug" settings key / --debug flag) so a rename is a
// one-line change in one place.
const (
	DebugEnvName = "GOPI_DEBUG"
	EnvDebugOn   = "1"
)

// scratchHomeMakeHint is the actionable half of the refusal: the exact command
// that makes the scratch home EXIST, off the `/` device.
//
// It used to print a tmpfs mount (`mkdir -p … && mount -t tmpfs …`), and
// internal/testutil printed the same command. That advice is RETIRED (operator
// ruling, 2026-09-18: "just for a few ms performance? no, no no! don't use
// tmpfs. change settings to workspace. now"): the scratch home of record is a
// real directory on the block device, so the refusal prescribes the directory
// and nothing else. A refusal that still offered the tmpfs route would be a
// refusal pointing at a retired location — the advice goes, the incident
// measurements in the same message stay.
func scratchHomeMakeHint() string {
	return fmt.Sprintf("mkdir -p %s", HomeDir())
}

// ambientTempRootPath is the path this ban is really about. It is an ALIAS of
// worktreeroots.AmbientTempRoot, never a second definition (Rule 14): the
// literal lives only at that leaf constant, so the refusal text here and the
// worktree-root literal gate cannot drift apart (Rule 13).
const ambientTempRootPath = worktreeroots.AmbientTempRoot

// The two classifications the refusal prints. They exist so a reader can tell a
// DELIBERATE bad pin from an IMPLICIT fallback without decoding os.Getenv
// (Rule 15: name the cause, never leave the reader guessing).
const (
	explicitTempRootClause = TmpDirEnvName + " is EXPLICITLY set to this path, so the operator pinned a temp root on the boot device"
	implicitTempRootClause = TmpDirEnvName + " is UNSET, so this is the IMPLICIT implementation fallback (os.TempDir()) — nobody chose it"
)

// reportTempRootOnce guards the stderr report so a long-running process that
// falls back to `/` prints the warning once, not once per temp file (a report
// nobody can read is a report nobody acts on).
var reportTempRootOnce sync.Once

// ReportTempRootIfOnRootFS is the LOUD, NON-FATAL half of the ban for code that
// cannot refuse: the runtime scratch fallback (RuntimeScratchDir) may not abort
// a task over a temp path, but falling back onto `/` must not be SILENT either
// (Rule 15: silent failure is the one unacceptable outcome). It reports once per
// process and returns the error for a caller that wants to act on it.
func ReportTempRootIfOnRootFS() error {
	err := AssertTempRootOffRootFS()
	if err == nil {
		return nil
	}
	reportTempRootOnce.Do(func() {
		fmt.Fprintf(os.Stderr, "%s%s\n", ProbeReportPrefix, err)
	})
	return err
}

// --- TEMP-ROOT HEADROOM: does the root that was just resolved still have room? -
//
// THE FAILURE THIS BLOCK EXISTS FOR (measured on this host, 2026-10-31). The
// temp workspace every gopi child inherits is `<home>/workspace/_tmp`, and on
// this host GOCACHE is `<home>/workspace/.gocache` (546G measured) — the SAME
// filesystem (device 66305, nvme1n1p1), which `df` reported at 98%: 91 GiB free
// of 3.6T while the epic was briefed with 2.3 GiB free. When that partition runs
// dry a `go build`/`go test` dies with `no space left on device` printed from an
// UNRELATED package's link step, and the reader chases a code regression that
// does not exist.
//
// WHY A MEASUREMENT AND NOT A REFUSAL. The temp root is USABLE (it exists, it
// takes a write) — the usability contract above (ensureUsableTempRoot) is
// satisfied and says nothing about the partition having room for a build. A run
// on a nearly-full partition is the operator's call to make, so the criterion
// below is LOUD, never fatal: it prints the path, the DEVICE (Rule 22: measured
// with statfs, never judged by path spelling), the free bytes, the floor it
// missed, and the reclaim candidates — listed, never deleted (Rule 42).
const (
	// TmpDirHeadroomFloorBytes is the free-space FLOOR the resolved temp root is
	// compared against.
	//
	// WHY 2 GiB: it is the size of the SMALLEST build this host actually runs.
	// (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` plus one package's test
	// binary measured in the hundreds of MiB on this tree, and the toolchain
	// writes its object files into GOCACHE on the SAME partition before the link
	// step.) Below this the next build is a coin flip, which is exactly the state
	// the epic was briefed in — the floor sits at the point where the failure
	// becomes probable rather than theoretical. A FLOOR, not a quota: it is not a
	// share of the disk and it refuses nothing.
	TmpDirHeadroomFloorBytes uint64 = 2 << 30

	// TmpDirHeadroomFloorEnvName overrides the floor, in BYTES. It exists so the
	// criterion can be exercised against a REAL run without filling a real disk
	// (Rule 27 forbids generating device load to produce a number) and so an
	// operator on a smaller host can state their own floor. A value that is not a
	// byte count is reported and ignored — never silently treated as zero, which
	// would disable the criterion (Rule 15).
	TmpDirHeadroomFloorEnvName = "GOPI_TMPDIR_HEADROOM_BYTES"

	// tmpDirHeadroomReclaimHint is the remedy clause both the warning and the
	// report print. Every verb it names already exists and each one REPORTS
	// before it acts; none deletes anything until its owner says so (Rule 42 —
	// reclaim is the operator's decision).
	tmpDirHeadroomReclaimHint = "reclaimable candidates (listed, NOT deleted): `gopi tool scratch-gc gocache` for the Go build cache, `gopi tool scratch-gc` for abandoned scratch roots, `gopi tool scratch-gc deps` for the resident dependencies this host carries"

	// tmpDirHeadroomOKMarker / tmpDirHeadroomLowMarker label the report row. Two
	// spellings, so a reader scanning `gopi config show` sees the verdict without
	// parsing numbers.
	tmpDirHeadroomOKMarker  = "OK"
	tmpDirHeadroomLowMarker = "LOW"
)

// ErrTmpDirLowHeadroom is the sentinel a caller can match with errors.Is when
// the resolved temp root has less free space than its floor.
var ErrTmpDirLowHeadroom = errors.New("temp root is below its free-space floor")

// TmpDirHeadroom is the temp-root headroom CRITERION as a reportable value: the
// measurement (device + free + total), the floor it was judged against, and the
// verdict. It is a struct rather than a bool so the SAME reading can be printed
// by the warning (stderr, once per process) and by `gopi config show` (the
// on-demand report) without either re-measuring and disagreeing.
type TmpDirHeadroom struct {
	// Path is the resolved temp root the reading is about.
	Path string
	// Source names the channel that produced Path ("" when unknown).
	Source string
	// Device is the statfs filesystem identifier of Path (Rule 22: the partition
	// question is answered by the device, never by a path spelling).
	Device uint64
	// Free and Total are the filesystem's unprivileged-free and total bytes.
	Free, Total uint64
	// Floor is the free-space floor the reading was judged against.
	Floor uint64
	// Low is the verdict; MeasureErr is non-nil when the reading could not be
	// taken at all (an unmeasurable root is reported, never assumed safe).
	Low        bool
	MeasureErr error
}

// MeasureTmpDirHeadroom takes the reading for dir and judges it against
// TmpDirHeadroomFloor() — the production form, with the floor taken from the ONE
// place that resolves it.
func MeasureTmpDirHeadroom(dir, source string) TmpDirHeadroom {
	return judgeTmpDirHeadroom(dir, source, TmpDirHeadroomFloor(), statfsTmpDirHeadroom)
}

// judgeTmpDirHeadroom is the testable core: the floor AND the measuring function
// are parameters, so the criterion can be exercised with an INJECTED reading (no
// real disk has to be filled — Rule 27). Production passes the resolved floor and
// statfsTmpDirHeadroom; nothing else in this package may.
func judgeTmpDirHeadroom(dir, source string, floor uint64, measure func(string) (free, total uint64, err error)) TmpDirHeadroom {
	h := TmpDirHeadroom{Path: dir, Source: source, Floor: floor}
	dev, err := DeviceID(dir)
	if err != nil {
		h.MeasureErr = fmt.Errorf("cannot measure the filesystem device of the temp root: %w", err)
		return h
	}
	h.Device = dev
	free, total, err := measure(dir)
	if err != nil {
		h.MeasureErr = fmt.Errorf("cannot measure the free space of the temp root: %w", err)
		return h
	}
	h.Free, h.Total = free, total
	h.Low = floor > 0 && free < floor
	return h
}

// statfsTmpDirHeadroom reports (free, total) bytes of the filesystem holding
// path, resolved first for the same reason DeviceID resolves it.
func statfsTmpDirHeadroom(path string) (free, total uint64, err error) {
	target := path
	if resolved, rerr := filepath.EvalSymlinks(path); rerr == nil && resolved != "" {
		target = resolved
	}
	f, err := statfsAt(target)
	if err != nil {
		return 0, 0, err
	}
	if f.bsize <= 0 {
		return 0, 0, fmt.Errorf("statfs(%s): implausible block size %d", target, f.bsize)
	}
	return f.bavail * uint64(f.bsize), f.blocks * uint64(f.bsize), nil
}

// TmpDirHeadroomFloor resolves the floor: TmpDirHeadroomFloorEnvName's value when
// it parses, else TmpDirHeadroomFloorBytes. A malformed override is PRINTED and
// ignored (Rule 15: a typo must not silently disable the criterion by reading as
// zero).
func TmpDirHeadroomFloor() uint64 {
	raw := strings.TrimSpace(os.Getenv(TmpDirHeadroomFloorEnvName))
	if raw == "" {
		return TmpDirHeadroomFloorBytes
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s%s=%q is not a byte count (%v) — using the %s floor\n",
			ProbeReportPrefix, TmpDirHeadroomFloorEnvName, raw, err, FormatBytes(TmpDirHeadroomFloorBytes))
		return TmpDirHeadroomFloorBytes
	}
	return n
}

// Warning is the LOUD, self-explaining sentence: path, device, free of total, the
// floor it missed, and the reclaim candidates. One line, because it is printed on
// stderr next to a run's own output, and it never names a command that deletes
// anything (Rule 42).
func (h TmpDirHeadroom) Warning() string {
	return fmt.Sprintf("%v: the temp root %q (device 0x%x, %s) has %s free of %s — below the %s floor; every `go build`/`go test` started from this root dies as `no space left on device`, and the toolchain prints some unrelated package's name when it does. %s",
		ErrTmpDirLowHeadroom, h.Path, h.Device, reportSource(h.Source), FormatBytes(h.Free), FormatBytes(h.Total), FormatBytes(h.Floor), tmpDirHeadroomReclaimHint)
}

// Report renders the row `gopi config show` prints beneath the temp workspace:
// the verdict marker first (so a skim finds it), then the same facts the warning
// carries. An unmeasurable root is reported as a FAULT with its reason.
func (h TmpDirHeadroom) Report() string {
	if h.MeasureErr != nil {
		return "(unavailable: " + h.MeasureErr.Error() + ")"
	}
	marker := tmpDirHeadroomOKMarker
	if h.Low {
		marker = tmpDirHeadroomLowMarker
	}
	return fmt.Sprintf("%s: %s free of %s, device 0x%x, floor %s (%s) — %s",
		marker, FormatBytes(h.Free), FormatBytes(h.Total), h.Device, FormatBytes(h.Floor),
		reportSource(h.Source), tmpDirHeadroomReclaimHint)
}

// reportSource labels the channel that produced the root, or says it is unknown
// rather than printing an empty parenthesis precisely where a reader is asking.
func reportSource(source string) string {
	if strings.TrimSpace(source) == "" {
		return "unknown channel"
	}
	return source
}

// tmpDirHeadroomOnce guards the stderr warning so a long-running process prints
// it once, not once per temp file (the same reason reportTempRootOnce exists).
var tmpDirHeadroomOnce sync.Once

// ReportTmpDirHeadroomIfLow is the loud half of the criterion for code that
// cannot refuse a nearly-full root: it measures dir and, when the free space is
// below the floor, prints Warning() to stderr ONCE per process and returns
// ErrTmpDirLowHeadroom (naming the reading) for a caller that wants to act. A
// root that measures fine prints nothing — the criterion is not a heartbeat.
func ReportTmpDirHeadroomIfLow(dir, source string) error {
	h := MeasureTmpDirHeadroom(dir, source)
	if h.MeasureErr != nil {
		return h.MeasureErr
	}
	if !h.Low {
		return nil
	}
	tmpDirHeadroomOnce.Do(func() {
		fmt.Fprintf(os.Stderr, "%s%s\n", ProbeReportPrefix, h.Warning())
	})
	return fmt.Errorf("%w: %s free at %s", ErrTmpDirLowHeadroom, FormatBytes(h.Free), h.Path)
}
