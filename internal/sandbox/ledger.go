package sandbox

// ledger.go — THE SANDBOX LEDGER IS A REAL, WRITABLE, PRIVATE FILE.
//
// WHY THIS FILE EXISTS (the defect it closes, measured 2026-09-26). The
// sandbox's ledger used to be a SYMLINK to the shared, read-only test SAMPLE
// (`testutil.SampleDBPath`, sealed mode 0444). That shape answers a question
// nobody asked: a sandbox is a place to RUN a gopi process, and its ledger is
// the one file a run WRITES FIRST — every session, every task row, every
// distilled lesson goes there. A read-only target produces one of two failures
// the moment a real run opens it:
//
//   - the open is refused ("attempt to write a readonly database") and the run
//     is dead on arrival, or
//   - the writer does what every SQLite writer does with a blocked path — it
//     writes a SIBLING (a temp file next to the target) and renames it into
//     place, and that rename REPLACES THE SYMLINK NODE: the sandbox's
//     `memory.db` silently becomes a regular file, and the sample it pointed at
//     is no longer what the sandbox reads. Nothing announces the swap.
//
// So the ONE truth, decided here and nowhere else: a sandbox's ledger is a REAL
// FILE, at the path the ledger resolver ACTUALLY computes inside a sandbox —
// `<sandbox>/.gopi/memory.db` (config.GopiDir() joins config.ProfileDirName
// onto $GOPI_HOME; the sandbox IS a GOPI_HOME, so `.gopi/` is where the ledger
// lands). It is private (mode 0600), writable (a run must be able to open it
// read-write), and it starts EMPTY — or, when the caller names a seed, as a
// byte-for-byte COPY of that seed.
//
// THE SAMPLE IS STILL USED — AS A SEED, NEVER AS THE LIVE LEDGER. The scaffold's
// sample remains the ONE definition of "a populated gopi database", so a
// sandbox that wants starter rows asks for them through the seed channel
// (Options.LedgerSeedPath / GopiSandboxLedgerSeedEnvName) and gets a COPY. A
// copy is the whole point: the sample keeps its 0444 seal, the sandbox keeps a
// file it may write, and no run can reach the shared artifact through its own
// ledger path.
//
// THE SYMLINK IS REFUSED, LOUDLY, ON REPAIR. A sandbox created by the previous
// shape still holds a `memory.db` symlink. Silently replacing it would be the
// same class of defect this file closes (a path that changes meaning without
// saying so), and writing THROUGH it would write into the shared sample. So
// Ensure refuses it by name and tells the operator the ONE thing to do: remove
// the link (or use a fresh sandbox). Refusals belong to the caller; nothing
// here guesses.
//
// WHAT THIS FILE DELIBERATELY DOES NOT DO: it does not import internal/testutil
// (testutil imports THIS package — a cycle), it does not create a second
// database generator, and it does not run any DDL. The empty file is a state the
// ledger's own FIRST-OPEN path already handles: internal/tools adopts a
// zero-byte database and initializes its schema (the same contract its
// first-open tests pin), and that is the ONE schema authority — mirrored here it
// would be a second one.

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // the ONE driver the tree uses; named by sandboxLedgerDriverName below

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// The ledger's own shape constants (Rule 1a: no bare literals in a mode, a
// width or a permission — the reclaimer's age logic and the tests reason about
// the same numbers this file creates).
const (
	// sandboxLedgerDirPerm is the profile directory inside the sandbox
	// (`.gopi/`): private, traversable by its owner only. It is the DIRECTORY
	// the ledger lives in, so its mode is half of "nobody else's business".
	sandboxLedgerDirPerm = 0o700
	// sandboxLedgerFilePerm is the ledger itself: writable by the owner, and
	// NOT group/world readable — a sandbox ledger carries the run's rows, and
	// the sandbox's 0700 directory is the only other guard.
	sandboxLedgerFilePerm = 0o600
	// sandboxLedgerSeedBufBytes bounds one read while copying a seed. 64 KiB:
	// large enough that a multi-MB seed is a handful of syscalls, small enough
	// that the copy never holds a whole database in memory (a seed may be the
	// operator's live ledger, whose size is not ours to assume).
	sandboxLedgerSeedBufBytes = 64 << 10
)

// sandboxLedgerDriverName is the database/sql driver name the seed copy uses.
// Named (Rule 1b) because a bare "sqlite" at a call site is the string-literal
// defect the rule exists for; it is also the value the whole tree registers.
const sandboxLedgerDriverName = "sqlite"

// GopiSandboxLedgerSeedEnvName names the environment channel that hands a seed
// database's path to a `gopi tool sandbox create` invocation — the shape a CLI call
// needs, because the shared sample is built by the test scaffold (a
// `*testing.T` helper) and cannot be built from production code.
//
// The seed is COPIED, never linked: see the file comment. The name's ONE
// reader is config.SandboxLedgerSeedValue (called by ledgerSeedPath below) —
// Rule 21's "a declared env constant has a REAL reader" is satisfied there,
// in the package that also declares it.
//
// It is an ALIAS of config.SandboxLedgerSeedEnvName (Rule 14): the catalogue
// row in internal/config/settings.go renders this knob in `gopi --help-env`,
// this package is its reader, and internal/sandbox already imports
// internal/config — so config is the one package both sides can reach, and the
// definition lives there. A second literal here would be the mirror the alias
// exists to prevent.
const GopiSandboxLedgerSeedEnvName = config.SandboxLedgerSeedEnvName

// LedgerDirName is the profile directory inside a sandbox (`<sandbox>/.gopi`).
// It is an ALIAS of config.ProfileDirName (Rule 14): the sandbox IS a GOPI_HOME,
// config.GopiDir() joins exactly this leaf onto it, and a mirrored copy is the
// drift that would put the ledger somewhere the resolver never looks.
const LedgerDirName = config.ProfileDirName

// LedgerPathIn is the path of a sandbox's ledger — the path the ledger resolver
// computes for a process whose GOPI_HOME is dir. ONE function (Rule 13/14), so
// the initializer, the tests and the report cannot disagree about where the file
// is.
func LedgerPathIn(dir string) string {
	return filepath.Join(dir, LedgerDirName, config.MemoryDBFileName)
}

// ledgerSeedPath resolves the seed database the caller asked for, if any. Empty
// means "no seed": the sandbox's ledger then starts EMPTY, which is a valid
// sandbox (a run's ledger is supposed to fill with that run's rows).
//
// The environment read goes through config.SandboxLedgerSeedValue — the ONE
// reader of the name config.SandboxLedgerSeedEnvName (Rule 2/13), which is why
// this function holds no os.Getenv of its own.
func ledgerSeedPath(opts Options) string {
	if p := strings.TrimSpace(opts.LedgerSeedPath); p != "" {
		return p
	}
	return config.SandboxLedgerSeedValue()
}

// refuseSeedInsideSandbox returns an error when the caller's seed database path
// resolves INSIDE the sandbox the ledger is being materialised in.
//
// WHY (the self-referential shape — carried over from the refuted symlink-shape
// branch, which guarded the same property for its own link target). A seed is
// the ONE thing a sandbox is handed from outside: the starter rows the caller
// decided the sandbox should begin from. A seed that lives inside the sandbox is
// not that — it is the sandbox being configured by its own contents, which
// silently tests nothing (the same self-referential refusal settingsDocSource
// makes for the settings document, in this package's own words) and, worse,
// produces a ledger
// that is a COPY of a file inside the same directory: delete the source and the
// sandbox's history has no origin anywhere.
//
// It is a REFUSAL, not a silent skip: the caller named a seed (Rule 15 — a
// dropped input is a silent failure) and gets the reason plus the ONE thing to
// do. Both sides are canonicalised first, because a path reached through a
// symlink is the same directory (this is how the old shape was reached too: the
// pre-fix `memory.db` was a link at `<sandbox>/.gopi/…`, i.e. a path INSIDE the
// sandbox).
func refuseSeedInsideSandbox(dir, seed string) error {
	absSeed, err := filepath.Abs(seed)
	if err != nil {
		return fmt.Errorf("sandbox: resolve the ledger seed path %s: %w", seed, err)
	}
	if !scratch.PathWithin(scratch.CanonicalPath(absSeed), scratch.CanonicalPath(dir)) {
		return nil
	}
	return fmt.Errorf("sandbox: refusing the ledger seed %s — it is INSIDE the sandbox %s. A seed is the "+
		"starter database a sandbox is handed from OUTSIDE; one that lives in the sandbox itself makes the "+
		"sandbox read its own contents (the ledger would be a copy of a file in the very directory it "+
		"configures). Pass the shared sample (%s names one) or any path outside the sandbox, or unset it "+
		"for a ledger that starts empty", absSeed, dir, GopiSandboxLedgerSeedEnvName)
}

// materialiseLedger brings a sandbox's ledger to the ONE truth and returns its
// path. It is the WRITE-side half of the shape: the file it leaves behind is
// real, private, writable, and — when a seed was named — populated from that
// seed by VALUE.
//
// Three pre-existing states are distinguished, and each gets its own answer,
// because they are not the same thing:
//
//	regular file, writable  → adopt it; seed it only when it is still empty
//	regular file, not writable → FAIL LOUDLY, naming the mode (a read-only
//	                             ledger is the defect, and fixing it here
//	                             would silently rewrite the operator's file)
//	symlink                 → REFUSE, by name (the pre-fix shape; see the file
//	                          comment — writing through it targets the shared
//	                          sample, replacing it swallows the link silently)
func materialiseLedger(dir string, opts Options) (string, error) {
	// The seed must come from OUTSIDE the sandbox. This is the ONE enforcement
	// site: every path below (create, adopt, re-seed) is reached from here, so
	// guarding here guards all of them (Rule 13: one definition, referenced).
	if seed := ledgerSeedPath(opts); seed != "" {
		if err := refuseSeedInsideSandbox(dir, seed); err != nil {
			return "", err
		}
	}

	ledgerDir := filepath.Join(dir, LedgerDirName)
	if err := os.MkdirAll(ledgerDir, sandboxLedgerDirPerm); err != nil {
		return "", fmt.Errorf("sandbox: create the profile directory %s for the ledger: %w", ledgerDir, err)
	}
	path := filepath.Join(ledgerDir, config.MemoryDBFileName)

	info, err := os.Lstat(path)
	switch {
	case err == nil:
		return adoptLedger(path, info, opts)
	case !os.IsNotExist(err):
		return "", fmt.Errorf("sandbox: inspect the ledger %s: %w", path, err)
	}

	if err := writeLedger(path, opts); err != nil {
		return "", err
	}
	return path, nil
}

// adoptLedger handles a ledger that already exists: it validates the SHAPE and
// repairs only what is provably empty. An existing ledger with content is the
// sandbox's own history — it is left alone.
func adoptLedger(path string, info os.FileInfo, opts Options) (string, error) {
	if info.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(path) //stylegate:ignore a non-symlink ledger is the non-sandbox case, which is answered as 'not a sandbox ledger' instead of an error
		return "", fmt.Errorf("sandbox: %s is a symlink -> %s. A sandbox ledger must be a REAL writable file: "+
			"a symlink either hands the run a target it may not write (the shared read-only sample — "+
			"\"attempt to write a readonly database\") or is silently REPLACED by a writer's rename, "+
			"after which the sandbox reads a different file than it pointed at. Remove it "+
			"(rm %s) or use a fresh sandbox, then re-run", path, target, path)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("sandbox: %s exists and is not a regular file (%s) — refusing to replace it",
			path, info.Mode().Type())
	}
	if err := checkLedgerWritable(path); err != nil {
		return "", err
	}
	if info.Size() > 0 {
		return path, nil // the sandbox's own history: never rewritten
	}
	if err := seedLedgerFile(path, opts); err != nil {
		return "", err
	}
	return path, nil
}

// checkLedgerWritable proves the ledger can be opened for WRITING — the
// property the whole file is about, checked rather than assumed. It appends
// nothing (O_WRONLY|O_APPEND opens without truncating and writes no byte).
func checkLedgerWritable(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("sandbox: the ledger %s is not writable (%w) — a sandbox run writes its ledger "+
			"first, so a read-only ledger fails every run inside it. Fix the mode (chmod u+w %s) or use a "+
			"fresh sandbox", path, err, path)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("sandbox: close the writability probe on %s: %w", path, err)
	}
	return nil
}

// writeLedger creates the ledger that does not exist yet: an empty file, or a
// byte-for-byte copy of the named seed. The copy lands through a temp file +
// rename INSIDE the ledger directory, so a half-copied ledger is never visible
// at the destination path.
func writeLedger(path string, opts Options) error {
	seed := ledgerSeedPath(opts)
	if seed == "" {
		if err := os.WriteFile(path, nil, sandboxLedgerFilePerm); err != nil {
			return fmt.Errorf("sandbox: create the ledger %s: %w", path, err)
		}
		return nil
	}
	if _, err := os.Stat(seed); err != nil {
		return fmt.Errorf("sandbox: the seed database %s is not readable (%w) — pass a real path, or unset %s "+
			"for an empty ledger", seed, err, GopiSandboxLedgerSeedEnvName)
	}
	if err := copyFileAtomic(seed, path); err != nil {
		return err
	}
	return nil
}

// seedLedgerFile fills an EMPTY adopted ledger from the seed. An empty file is
// the only state it repairs: anything with content belongs to the sandbox.
func seedLedgerFile(path string, opts Options) error {
	if ledgerSeedPath(opts) == "" {
		return nil
	}
	return writeLedger(path, opts)
}

// copyFileAtomic copies src to dst via a temp file in dst's directory and a
// rename, so a reader of dst sees either the old file or the complete copy.
func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("sandbox: open the seed database %s: %w", src, err)
	}
	defer in.Close() //stylegate:ignore the source handle is closed by contract after the copy

	tmp, err := os.CreateTemp(filepath.Dir(dst), config.MemoryDBFileName+".seed-*")
	if err != nil {
		return fmt.Errorf("sandbox: create the ledger's temp file next to %s: %w", dst, err)
	}
	tmpName := tmp.Name()
	buf := make([]byte, sandboxLedgerSeedBufBytes)
	_, copyErr := io.CopyBuffer(tmp, in, buf)
	closeErr := tmp.Close()
	if copyErr != nil {
		removeQuietly(tmpName)
		return fmt.Errorf("sandbox: copy the seed %s into the ledger: %w", src, copyErr)
	}
	if closeErr != nil {
		removeQuietly(tmpName)
		return fmt.Errorf("sandbox: close the copied ledger %s: %w", tmpName, closeErr)
	}
	if err := os.Chmod(tmpName, sandboxLedgerFilePerm); err != nil {
		removeQuietly(tmpName)
		return fmt.Errorf("sandbox: set the ledger's mode %o on %s: %w", sandboxLedgerFilePerm, tmpName, err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		removeQuietly(tmpName)
		return fmt.Errorf("sandbox: publish the copied ledger as %s: %w", dst, err)
	}
	// The copy is a DATABASE, so its integrity is checked with the database's
	// own answer rather than with a byte count: a seed that is not a SQLite file
	// (a truncated artifact, a wrong path) would otherwise become a sandbox
	// ledger that fails at the first query with an obscure page error.
	return verifyLedgerIsADatabase(dst)
}

// verifyLedgerIsADatabase opens the freshly copied ledger and reads its schema.
// It is the LOUD half of "never hand a run a broken ledger": an unusable file
// is removed and reported, so the sandbox never exists in a state where its
// ledger is a mystery.
func verifyLedgerIsADatabase(path string) error {
	db, err := sql.Open(sandboxLedgerDriverName, path)
	if err != nil {
		removeQuietly(path)
		return fmt.Errorf("sandbox: the copied ledger %s could not be opened (%w) — the seed was not a readable database", path, err)
	}
	db.SetMaxOpenConns(1)
	_, queryErr := db.Query("SELECT count(*) FROM sqlite_master")
	closeErr := db.Close()
	if queryErr != nil {
		removeQuietly(path)
		return fmt.Errorf("sandbox: the copied ledger %s is not a usable SQLite database (%w) — check the seed path", path, queryErr)
	}
	if closeErr != nil {
		return fmt.Errorf("sandbox: close the verified ledger %s: %w", path, closeErr)
	}
	return nil
}

// removeQuietly drops a file this package just created and is abandoning. The
// removal is the cleanup of a FAILURE path, so its own error is recorded on
// stderr rather than replacing the failure the caller is about to be told about
// (Rule 15b: an error is never silently dropped — it is reported where it can
// still be read).
func removeQuietly(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "sandbox: could not remove the abandoned ledger file %s: %v\n", path, err)
	}
}
