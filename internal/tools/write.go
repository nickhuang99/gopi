// Package tools implements the file operation tools for the GoPi agent loop.
package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/workerownership"
)

// Write overwrites or creates a file at path with the given content.
// It creates parent directories if needed, and rejects empty paths
// or paths that point to existing directories.
// Error messages include hints to help the LLM fix the issue without extra calls.
//
// Symlink safety: a path that is itself a symlink is REFUSED by default.
// The atomic write's final os.Rename replaces the symlink node with a
// regular file — the link is corrupted and the target keeps its old
// content, so the write silently "succeeds" without reaching the target
// (the 2026-08-28 settings.json incident). Use WriteThroughSymlink (or the
// follow_symlink parameter) to resolve the link and write the target file
// in place, leaving the symlink itself untouched.
//
// Write is protected by a timeout (default 30s, GOPI_TOOL_TIMEOUT_SEC).
// On timeout the operation returns an error; the underlying goroutine may
// continue but the caller is unblocked.
func Write(path string, content string) error {
	return writeWithTimeout(path, content, false, FileToolTimeout())
}

// WriteThroughSymlink is Write with followSymlink=true: a path that is a
// symlink is resolved (filepath.EvalSymlinks) to its target and the write is
// performed on the target file — the symlink node itself is never replaced.
func WriteThroughSymlink(path string, content string) error {
	return writeWithTimeout(path, content, true, FileToolTimeout())
}

// writeWithTimeout wraps the write in a goroutine+channel with the given timeout.
// The timeout bounds the byte-landing leg, and that is now the WHOLE operation:
// nothing runs after the rename.
//
// THE POST-WRITE CLASSIFICATION HOOK IS GONE, and its absence is the change
// (see header_hook.go). It used to follow the rename because it shelled out to
// `python3 tools/docclass/classify.py` — a generator that WROTE a row into the
// shared DOC-INDEX.tsv — and a write that expensive could not share the byte
// deadline (it needed its own bound, its own sentinel, and a seam a test could
// substitute). The class now lives in the file's own first line, so the only
// thing left to do at write time is make sure that line exists, and that is a
// string operation on content the caller already handed us, performed BEFORE
// the bytes land (writeImpl → amendDocHeader). A deadline cannot observe
// whether the bytes landed, so its error still says so (writeTimeoutNote).
func writeWithTimeout(path string, content string, followSymlink bool, timeout time.Duration) error {
	return withTimeoutErr(timeout,
		fmt.Errorf("write: %s: timed out after %s%s", path, timeout, writeTimeoutNote),
		func(context.Context) error { return writeImpl(path, content, followSymlink) })
}

// writeTimeoutNote is appended to the write/edit timeout error so the message
// states what the deadline could NOT observe. A file tool's deadline wraps an
// operation that stages the content in a temp file and renames it into place,
// so by the time the deadline fires the bytes may already be at the target —
// and the operation is left to finish (withTimeout has no cancellation over
// os.File I/O). Without this tail, a caller reads a bare "timed out after 30s"
// and retries a write that already landed; the recipe asked for here is the
// one that is always safe: look at what is on disk before re-sending it.
const writeTimeoutNote = " — the content may already have landed on disk (the operation is left to finish); read the file before retrying"

// writeImpl contains the actual write logic. Symlink handling happens BEFORE
// atomicWriteFile: resolving here (not inside the atomic path) keeps
// atomicWriteFile a plain physical-path primitive, so the rename can never
// land on a symlink node.
func writeImpl(path string, content string, followSymlink bool) error {
	// Write-time worker-ownership annotation: a doc/script landing in an
	// audited tree that teaches a worker-launch or task-run shape gets the
	// canonical pointer LINE inside its own bytes, so the repo gate
	// (internal/testutil TestWorkerOwnershipPointerPresent) can never redden on
	// a document produced through this writer. Idempotent, and a no-op
	// off-scope — the predicate and the emitted line both live in
	// internal/workerownership, so the writer and the gate cannot drift. The
	// returned bool is the "did we change it" signal; the write path does not
	// need it (the annotation is written, not reported).
	content, _ = workerownership.AnnotateOwnerDocs(path, content)
	// Corpus classification, IN THE SAME PASS AS THE BYTES: a markdown file
	// under devdoc/ whose first line is not a legal `<!-- doc-class: … -->`
	// declaration is given one before it lands — no subprocess, no second
	// artifact, no post-write leg (header_hook.go carries the contract,
	// including why a class the author already declared is never re-decided).
	// Off-corpus paths pass through untouched.
	content, _ = amendDocHeader(path, content)
	if err := atomicWriteFile(path, content, followSymlink); err != nil {
		return err
	}
	return nil
}

// symlinkNote is the shared error tail for a refused symlink write: it names
// the target and offers both remedies (write the target directly, or follow).
func symlinkNote(op string, path, target string) string {
	return fmt.Sprintf(
		"%s: %s is a symlink -> %s. Refusing to replace the symlink itself "+
			"(would corrupt the link and leave the target unchanged). "+
			"Write to %s directly, or pass follow_symlink=true to write through the symlink.",
		op, path, target, target)
}

// resolveSymlink implements the shared symlink policy for write/edit: it
// expands ~, detects a symlink node via os.Lstat (never following), and
// returns the path to write.
//
//   - path is a symlink and followSymlink is false (the default):
//     refuse with a hint naming the target — the caller's rename would
//     replace the link node itself and the target would keep its old
//     content (the 2026-08-28 settings.json corruption).
//   - path is a symlink and followSymlink is true: resolve the link to the
//     target's physical path so the rename updates the target IN PLACE and
//     the symlink node survives. Intermediate symlinked directories are
//     resolved too (EvalSymlinks resolves the whole chain).
//   - anything else (regular file, not yet existing, directory…): returned
//     unchanged for the caller's own handling.
func resolveSymlink(op, path string, followSymlink bool) (string, error) {
	expanded, err := expandHome(path)
	if err != nil {
		return "", fmt.Errorf("%s: cannot expand ~/: %w", op, err)
	}
	info, err := os.Lstat(expanded)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		// Not a symlink (or does not exist) — nothing to resolve.
		return expanded, nil
	}
	target, err := os.Readlink(expanded)
	if err != nil {
		// Raced between Lstat and Readlink (or unreadable link) — the caller
		// proceeds with the plain path; the write will fail there if needed.
		return expanded, nil
	}
	if !followSymlink {
		return "", fmt.Errorf("%s", symlinkNote(op, expanded, target))
	}
	resolved, err := filepath.EvalSymlinks(expanded)
	if err != nil {
		// Broken or unreadable link — surface the error instead of silently
		// replacing the dead node with a regular file.
		return "", fmt.Errorf("%s: cannot resolve symlink %s -> %s: %w", op, expanded, target, err)
	}
	return resolved, nil
}

// atomicWriteFile writes content to path atomically: the bytes are staged in
// a same-directory temp file, fsynced, then renamed over the target. A crash
// or interruption mid-write can never leave a half-written file at path —
// readers see either the old content or the new (dsh reference: fsio.ts
// staging file + rename). The temp file is removed on any failure.
//
// Symlink policy (2026-08-28 settings.json corruption): os.Rename's POSIX
// semantics replace the NODE at path, not the file it links to. When path is
// a symlink, the default (followSymlink=false) refuses the write; followSymlink
// =true resolves the link to its physical target first, so the rename lands on
// the target file and the symlink node survives. Non-symlink paths are
// unaffected in both modes (regular-file writes are identical to before).
func atomicWriteFile(path string, content string, followSymlink bool) error {
	if path == "" {
		return fmt.Errorf("write: path must not be empty — provide a file path like /tmp/out.txt")
	}

	path, err := resolveSymlink(toolnames.ToolWrite, path, followSymlink)
	if err != nil {
		return err
	}

	// Clone-channel SHARED-CHECKOUT guard (measured 2026-11-01, row
	// 01a1957a-0e54, the incident's SECOND half): the run that owned a clone
	// ground edited cmd/agent/cli_scratch_gc.go in the repo's MAIN checkout, and
	// nothing on the write path objected. The judgement is the branch-create
	// guard's OWN predicate (cloneGroundSharedCheckoutVerdict) consulted here, so
	// the two faces cannot drift; a run with no announced ground, a path inside
	// the run's own ground and a path in an undeclared directory are all
	// untouched. The check runs BEFORE the stat/mkdir legs: a refused write must
	// not create the parent directories either.
	if reason := cloneGroundFileWriteRefusal(path, cloneGroundFileWriteOpWrite); reason != "" {
		return fmt.Errorf("%s", reason)
	}

	// Stat the path to check whether it is an existing directory.
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			// Suggest a filename inside the directory.
			return fmt.Errorf("write: %s is a directory. Did you mean %s?",
				path, filepath.Join(path, "output.txt"))
		}
	} else if !os.IsNotExist(err) {
		// Permission or other stat error.
		return fmt.Errorf("write: cannot access %s: %w. Check permissions with ls -la %s.",
			path, err, filepath.Dir(path))
	}

	// Ensure parent directories exist, with hints on failure.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// Try to find similar directory names.
		parent := filepath.Dir(dir)
		if entries, readErr := os.ReadDir(parent); readErr == nil {
			var suggestions []string
			for _, e := range entries {
				if e.IsDir() && strings.HasPrefix(e.Name(), filepath.Base(dir)[:suggestionPrefixLen]) {
					suggestions = append(suggestions, filepath.Join(parent, e.Name()))
					if len(suggestions) >= maxPathSuggestions {
						break
					}
				}
			}
			if len(suggestions) > 0 {
				return fmt.Errorf("write: cannot create directory %s: %w. Similar directories: %s",
					dir, err, strings.Join(suggestions, ", "))
			}
		}
		return fmt.Errorf("write: cannot create directory %s: %w. Check that parent %s is writable.",
			dir, err, parent)
	}

	// 0o644 is the default mode for new files; overwriting an existing file
	// keeps its current permissions (os.WriteFile semantics — the rename would
	// otherwise reset them to the temp file's mode). info comes from the stat
	// above (nil when the file does not exist yet).
	mode := os.FileMode(0o644)
	if info != nil && info.Mode().IsRegular() {
		mode = info.Mode().Perm()
	}

	// Stage the content in a same-directory temp file so the final rename is
	// atomic. os.CreateTemp creates with 0600; the chmod below applies the
	// target mode before the rename.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".gopi-tmp-*")
	if err != nil {
		// Permission errors get a helpful hint.
		if os.IsPermission(err) {
			return fmt.Errorf("write: permission denied for %s. Check with ls -la %s.",
				path, dir)
		}
		return fmt.Errorf("write: %w. Check that the path is writable and not read-only.", err)
	}
	tmpName := tmp.Name()
	// Cleanup: on any failure below the staging file is removed; after a
	// successful rename the name no longer exists, so the remove is a no-op.
	defer os.Remove(tmpName)
	defer tmp.Close()

	if _, err := tmp.WriteString(content); err != nil {
		return fmt.Errorf("write: %w. Check that the path is writable and not read-only.", err)
	}
	// fsync before rename: without it a crash could leave an empty/partial
	// file at the target even though the rename happened first.
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("write: %w. Check that the path is writable and not read-only.", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("write: %w. Check that the path is writable and not read-only.", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write: %w. Check that the path is writable and not read-only.", err)
	}

	// Atomic replace: readers see either the old or the new content, never a
	// half-written mix.
	if err := os.Rename(tmpName, path); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("write: permission denied for %s. Check with ls -la %s.",
				path, dir)
		}
		return fmt.Errorf("write: %w. Check that the path is writable and not read-only.", err)
	}

	return nil
}
