<!-- doc-class: historical -->
# Symlink-Aware read/write/edit Tools

Date: 2026-08-28 (post-incident) · Branch: `feat/symlink-aware` · Worktree: `~/workspace/gopi-symlink-aware`

## Incident recap

User's `~/.gopi/settings.json` was a symlink → `.flash_settings.json`. An
agent used write/edit on the symlink path:

- **write**: `atomicWriteFile` stages a same-directory temp file, then
  `os.Rename(tmp, path)`. POSIX rename **replaces the node at path** — the
  symlink node itself — not the file the link points to. Result: the link
  became a regular file, the target kept its old content. The write reported
  success but the real config was never touched.
- **edit** (worse to debug): old flow was `os.ReadFile` (FOLLOWS the link, so
  the edit was computed from the target's content) → transform →
  `atomicWriteFile` (rename onto the link path). Read the target, write the
  link — the edit was silently lost and the link corrupted.

Root cause: rename-vs-follow asymmetry. read follows symlinks; the atomic
write path renames onto the path and never resolves them.

## Design

Shared policy helper in `internal/tools/write.go`:

```go
func resolveSymlink(op, path string, followSymlink bool) (string, error)
```

- expands `~` (reuses the shared `expandHome`),
- `os.Lstat` (never follows) to detect a symlink node,
- `os.Readlink` to name the target in messages,
- **follow=false (default)**: refuse with a hint that names the target and
  both remedies (write the target directly, or pass `follow_symlink=true`),
- **follow=true**: `filepath.EvalSymlinks` → the physical target path, so the
  final rename lands on the target file **in place** and the link survives.
  EvalSymlinks also resolves symlinked intermediate directories (a link
  inside a symlinked dir).
- broken (dangling) link + follow=true → explicit "cannot resolve symlink"
  error; the dead node is never silently replaced by a regular file.
- non-symlink paths: returned unchanged — zero behavior change.

Resolution happens **before** the read/rename in every tool, so a rename onto
a link node can no longer occur anywhere in the file-tool code.

### read (read.go)

Reads still follow the link (reading a symlink = reading its target is the
correct behavior). The result is annotated with a hint line, in the existing
footer style, inserted after the content and **before any truncation footer**
(`readFull`/`readPage` take a `symlinkHint` parameter so the ordering holds
for full, budget-truncated, and huge-line-fragment pages alike):

```
<file content>
[symlink: <path> -> <target>]
```

### write (write.go)

- `Write` / `WriteThroughSymlink` (exported signature unchanged; the
  follow variant is a separate exported function).
- `atomicWriteFile(path, content, followSymlink)` calls `resolveSymlink`
  first (the expansion it previously did itself moved into the helper).
- Refusal error (exact contract, asserted in tests):
  `write: <path> is a symlink -> <target>. Refusing to replace the symlink
  itself (would corrupt the link and leave the target unchanged). Write to
  <target> directly, or pass follow_symlink=true to write through the
  symlink.`

### edit (edit.go)

- `Edit` / `EditThroughSymlink`.
- `editImpl` calls `resolveSymlink` **before** `os.ReadFile` — this also
  fixes the read/write asymmetry (old code read the target but wrote the
  link). With follow=true both the read and the rename are pinned to the
  resolved target path; the inner `atomicWriteFile(expanded, newContent,
  false)` receives an already-physical path, so the inner check is a no-op.
- Error prefix stays `"write failed:"` for the underlying write errors
  (edit's long-standing error contract).

### agent loop (internal/agent/loop.go)

- `claudeToolDefs`: `write` and `edit` gain a `follow_symlink` boolean
  property (default false) with a description explaining the refuse/resolve
  semantics; all three descriptions gain one sentence (read: "If the path is
  a symlink, the read result notes the link target."; write/edit: "Paths
  that are symlinks are refused by default to protect the link; pass
  follow_symlink=true to write through to the target.").
- Dispatch: `follow_symlink` decoded exactly like `replace_all` (JSON bool,
  false when omitted) and routed to the exported or ThroughSymlink variant
  per call. Exported `Write`/`Edit` signatures are unchanged, so no other
  callers break.

## Tests (all use t.TempDir(), never the real ~/.gopi/)

`read_symlink_test.go`
- `TestRead_SymlinkNotesTarget` — content comes from the target + hint line
  contains link → target; link node untouched.
- `TestRead_SymlinkHintBeforeTruncationFooter` — hint precedes the
  `[read truncated: …]` footer on a byte-budget-truncated page (600×~100B
  crosses maxPageReadBytes; a mere line-limit page ends without a footer —
  that expectation bug was caught by the first test run and fixed).
- `TestRead_RegularFileNoSymlinkHint` — regression: plain-file read is
  byte-identical, no hint injected.

`write_symlink_test.go`
- `TestWrite_SymlinkRefusedByDefault` — error names symlink + target +
  `follow_symlink=true`; link still a symlink; target content unchanged.
- `TestWrite_ThroughSymlinkWritesTarget` — target updated, content visible
  through the link, link node + target unchanged, no staging temp left.
- `TestWrite_RegularPathUnaffected` — regression for both modes.
- `TestWrite_BrokenSymlinkFollowErrors` — dangling link refuses in both
  modes and the node survives.

`edit_symlink_test.go`
- `TestEdit_SymlinkRefusedByDefault` / `TestEdit_ThroughSymlinkEditsTarget` /
  `TestEdit_RegularPathUnaffected` — mirror the write matrix.
- `TestEdit_ThroughSymlinkPreservesMode` — 0600 target keeps its mode after
  the follow-mode rename.

Plus mechanical signature fixes in `timeout_test.go` and `write_test.go`
(the `false` flag added to internal helper calls).

## Verification

- `CGO_ENABLED=0 go build ./...`: OK
- `go vet ./internal/tools/ ./internal/agent/`: OK
- `go test ./internal/tools/ -run Symlink -count=1`: 10/10 PASS
- `go test ./internal/tools/ -count=1`: ok 129.1s (full suite incl. slow
  bash-timeout regressions)
- `go test ./internal/agent/ -count=1`: ok 36.5s

## Not done / deferred

- `bash` tool remains symlink-unaware (out of scope; shell semantics are the
  model's responsibility there).
- read does not offer a "no-follow" mode (Lstat-only peek) — not needed by
  the incident's contract.
- No LLM-adoption probe (Rule 11) for this change: it is a safety guard on
  existing tools, not a new tool; adoption = the schema default refusing the
  corrupting path, verified by unit tests at the tool layer.
