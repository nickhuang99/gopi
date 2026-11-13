package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/workerownership"
)

// Edit replaces oldStr with newStr in a file. By default oldStr must be
// unique (count == 1) — a non-unique match is refused as unsafe. replaceAll
// adds an explicit escape hatch: all occurrences are replaced (count == 0
// still errors). Returns error if oldStr not found or non-unique.
//
// Symlink safety: a path that is itself a symlink is REFUSED by default.
// Edit reads through the link (os.ReadFile follows it) but its atomic write
// renames onto the link path, which would replace the symlink node with a
// regular file — the edit would "succeed" while the target kept its old
// content (the 2026-08-28 settings.json incident). Use EditThroughSymlink
// (or the follow_symlink parameter) to resolve the link and read+edit the
// target file in place, leaving the symlink itself untouched.
//
// Edit is protected by a timeout (default 30s, GOPI_TOOL_TIMEOUT_SEC).
// On timeout the operation returns an error; the underlying goroutine may
// continue but the caller is unblocked.
func Edit(path string, oldStr string, newStr string, replaceAll bool) error {
	return editWithTimeout(path, oldStr, newStr, replaceAll, false, FileToolTimeout())
}

// EditThroughSymlink is Edit with followSymlink=true: a path that is a
// symlink is resolved to its target and the read+edit are performed on the
// target file — the symlink node itself is never replaced.
func EditThroughSymlink(path string, oldStr string, newStr string, replaceAll bool) error {
	return editWithTimeout(path, oldStr, newStr, replaceAll, true, FileToolTimeout())
}

// editWithTimeout wraps the edit in a goroutine+channel with the given timeout.
// The deadline bounds the byte-landing leg, and that is the whole operation:
// the post-write classification hook that used to follow it is gone (see
// header_hook.go). Nothing runs after the rename, because a document's class
// is its own first line — a header an edit REWRITES through the same content
// the caller handed us (editImpl → preserveDocHeader).
func editWithTimeout(path string, oldStr string, newStr string, replaceAll bool, followSymlink bool, timeout time.Duration) error {
	return withTimeoutErr(timeout,
		fmt.Errorf("edit: %s: timed out after %s%s", path, timeout, writeTimeoutNote),
		func(context.Context) error { return editImpl(path, oldStr, newStr, replaceAll, followSymlink) })
}

// editImpl contains the actual edit logic. The symlink check happens BEFORE
// the read: the old flow read through the symlink (os.ReadFile follows it)
// and then renamed onto the link path — reading the target's content while
// replacing the link node, i.e. the edit was silently lost. With the check
// first, the default refuses and followSymlink=true pins both the read and
// the write to the resolved target path.
func editImpl(path string, oldStr string, newStr string, replaceAll bool, followSymlink bool) error {
	if path == "" || oldStr == "" {
		return fmt.Errorf("path and oldStr must not be empty")
	}
	// No-op rejection (dsh edit.ts:50 semantics): oldStr == newStr means the
	// edit changes nothing — refuse it so the model re-issues a real edit.
	if oldStr == newStr {
		return fmt.Errorf("oldStr and newStr must differ")
	}

	// Expand ~ and apply the symlink policy (refuse by default; resolve to
	// the physical target when followSymlink is set).
	expanded, err := resolveSymlink(toolnames.ToolEdit, path, followSymlink)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return fmt.Errorf("read failed: %w", err)
	}

	content := string(data)
	count := strings.Count(content, oldStr)
	if count == 0 {
		return fmt.Errorf("oldStr not found in %s", path)
	}
	if !replaceAll && count > 1 {
		return fmt.Errorf("oldStr appears %d times in %s — must be unique for safe replacement (or pass replace_all=true)", count, path)
	}

	n := 1
	if replaceAll {
		n = -1 // replace every occurrence
	}
	// An empty newStr that MATCHED is a DELETION wearing an edit's success
	// message: strings.Replace(content, oldStr, "", n) erases the matched text
	// and the caller is told "Successfully edited". Refuse it here as well as
	// in the agent dispatcher, because this function is reachable from every
	// caller (the CLI mirrors included) and the invariant belongs to the
	// operation, not to one of its doors. The check sits AFTER the match is
	// known to be real so the two answers stay different facts: "oldStr did not
	// match" and "oldStr matched and would be erased" are different bugs.
	// Measured root cause 2026-10-04 (task 01a103c2-36e2-7808-bc42-d652762c4691):
	// the dispatcher read a misspelled argument key as "" and reached this line.
	if newStr == "" {
		return fmt.Errorf("edit %s: newStr is empty — an empty replacement would DELETE the %d matched character(s) instead of replacing them; pass the replacement text (to remove text deliberately, rewrite the surrounding content with write)", path, len(oldStr))
	}
	newContent := strings.Replace(content, oldStr, newStr, n)
	// Write-time worker-ownership annotation: edit is the OTHER door into the
	// same trees as write, so a new section that teaches a launch shape is
	// annotated here too (the edit path reaches atomicWriteFile directly, so
	// writeImpl's hook does not cover it). Idempotent; no-op off-scope.
	newContent, _ = workerownership.AnnotateOwnerDocs(expanded, newContent)
	// The corpus header survives an edit that rewrites the body. An edit lands
	// only the REPLACED bytes, so a document whose first line was outside the
	// matched window can reach this point without its `<!-- doc-class: … -->`
	// line; putting it back here is what keeps "the file declares its class" a
	// property of the corpus rather than a habit of each caller. Off-corpus
	// paths are untouched, and a header already present is left alone.
	newContent, _ = preserveDocHeader(expanded, content, newContent)
	// Clone-channel SHARED-CHECKOUT guard, the edit face: the SAME predicate as
	// atomicWriteFile's, rendered with the edit tool's name so the sentence says
	// which call was refused. It sits immediately before the atomic leg, so a
	// refused edit never reaches the staging file or the rename — the target's
	// bytes stay exactly as they were read above.
	if reason := cloneGroundFileWriteRefusal(expanded, cloneGroundFileWriteOpEdit); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	// Write via the shared atomic path (write.go's atomicWriteFile): the bytes
	// are staged in a same-directory temp file, fsynced, and renamed over the
	// target. Two bugs this fixes vs the old os.WriteFile(path, …, 0644):
	//   (1) atomicity — an interrupted write (kill -9) can no longer leave a
	//       truncated/corrupt file at the target; readers see old or new.
	//   (2) permissions — the hardcoded 0644 clobbered 0600/0700/0755 files to
	//       world-readable (a security regression on private keys). atomicWriteFile
	//       preserves the existing file's mode. New files still default to 0644.
	// expanded is the symlink-resolved physical path (resolveSymlink above):
	// followSymlink=true → the rename lands on the target file in place;
	// followSymlink=false → a symlink path was already refused, so a rename
	// onto a link node can no longer happen. The inner resolveSymlink call in
	// atomicWriteFile is a no-op here (target is a regular file).
	// Error prefix is kept as "write failed:" to match edit's long-standing
	// error contract (the underlying atomicWriteFile errors are wrapped).
	if err := atomicWriteFile(expanded, newContent, false); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	return nil
}
