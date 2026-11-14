<!-- doc-class: historical -->
# Task Report: review-fix-medium — 3 Medium bugs from code review

Fixes the 3 Medium findings cross-confirmed by the DS/GLM dual-model review
(`devdoc/code-review-2026-08-28.md` / `-buddy.md`). Each bug is a real defect,
fixed with a regression test.

Branch: `feat/review-fix-medium` (worktree `~/workspace/gopi-fix-medium`).

---

## 1. [安全] parseTTF 缺边界检查 → panic

**Bug** — `internal/text2img/render.go::parseTTF` read head/maxp/hhea/hmtx/cmap/loca
directly via the table directory's declared offsets (`data[h+18:h+20]`, `data[h+50:h+52]`,
…). A truncated/corrupt/forged font blob whose directory lies about a table's
`[offset, offset+length)` window → `slice bounds out of range` → panic → process
crash. Uninitialized/partial font input (e.g. a download cut mid-write) could reach
this from `RenderFile`.

Per the GLM re-verify, the format-4 cmap `gidArr` access (render.go:311-312) was
**already guarded** (`idx >= 0 && idx < len(gidArr)/2`) — that was left untouched.
The real vector is the table-parsing layer.

**Fix** — added layered boundary validation inside `parseTTF`, all returning
graceful errors (matching the existing "CFF unsupported" path, never panic):
- TTC header: `len(data) < 12` and index-table offset `12+4*index` bounds-checked
  before `be32(data[p:])`.
- Table directory: `off+12+16*nt ≤ len(data)` before any record read.
- A `tableOK(t)` guard validates each of the 7 required tables' `to ≥ 0`,
  `tl ≥ 0`, `to ≤ len(data)`, `tl ≤ len(data)-to`, before any deref.
- Per-table minimum-length checks for the exact windows read: `head ≥ 52`,
  `maxp ≥ 6`, `hhea ≥ 36`, `hmtx ≥ 4*nh` (numberOfHMetrics), `loca ≥ 2n|4n`.

**Test** — `TestParseTTFRejectsCorruptFonts` (truncated-to-empty, cut into glyf,
cut half, lying TTC offset) + `TestParseTTFRejectsBadTableWindows` (patched
table length 0xFFFFFFFF): all assert parseTTF **errors gracefully and never
panics** (a `recover` guard fails the test on any panic). The pristine font
still parses (sanity check).

## 2. [数据安全] edit.go 非原子写 + 0644 权限重置

**Bug** — `internal/tools/edit.go::editImpl` ended with
`os.WriteFile(expanded, []byte(newContent), 0644)`:
- **non-atomic**: `kill -9` mid-write → truncated/corrupt file at the target.
- **permission reset**: existing 0600/0700/0755 files were clobbered to 0644 —
  editing a private key / restricted file made it world-readable (security
  regression).

**Fix** — edit now writes through the same atomic path as write:
`atomicWriteFile` (`internal/tools/write.go:42`) — stages bytes in a
same-directory temp file, fsyncs, chmods to the **existing file's mode**, then
atomically renames over the target. New files still default to 0644; overwriting
an existing file preserves its permissions. The edit error prefix is kept
(`"write failed: %w"`) so edit's long-standing error contract is unchanged.

**Test** — `TestEdit_PreservesFileMode`: creates a 0600 file, edits it, asserts
the mode stays `0600` (not 0644) and the content is correct.

## 3. [功能] --keep-thinking CLI flag 失效

**Bug** — `internal/config`: settings.go declared `GOPI_KEEP_THINKING` with
`CLIFlag:"keep-thinking"`, but:
- `isBoolKey("keep_thinking")` returned **false** → flag misclassified as a
  scalar (demanded a value).
- `applyFlag` (flags.go) had **no** `"keep-thinking"` case → fell through to the
  default → `"unhandled flag --keep-thinking"`.
- `FormatUsage` didn't advertise it → users never knew the flag existed.

The env (`GOPI_KEEP_THINKING`) and settings.json (`keep_thinking`) channels were
already fine; only the CLI flag channel was broken.

**Fix** — three consistent edits (mirrors the working `hide-thinking` reference):
- `isBoolKey` adds `keep_thinking`.
- `applyFlag` adds `case "keep-thinking": cfg.KeepThinking = b` (via `atob`);
  the flag being bool also sets `SetEnvVars["GOPI_KEEP_THINKING"]` so it still
  propagates to the process env.
- `FormatUsage` (help.go) lists `--keep-thinking ON|OFF` next to `--thinking`.

**Test** — `TestFlagOverridesKeepThinking`: bare `--keep-thinking` parses as a
bool switch (true, no value required), `=true`/`=false`/`off` forms work, the
bool token isn't leaked as a positional, and `SetEnvVars[GOPI_KEEP_THINKING]`
is marked.

---

## Not Done
- The loca format-0 read loop is now bounds-safe (guarded by `lo[1] < 2*n`);
  format-1 already had its guard. No other table paths needed changes.
- Left the already-correct format-4 cmap `gidArr` guard untouched (per GLM re-verify).

## Verification
- Build: `go build ./...` → **OK** (exit 0)
- Tests: `go test ./internal/text2img/ ./internal/config/` → **OK**
- Full suite: `go test ./...` → **all green** (exit 0; cmd/agent, internal/agent incl. keep-thinking behavior, config, text2img, tools passed)
  real 120s bash-timeout regressions; edit/write/config/text2img tests pass)
- New tests:
  1. `TestParseTTFRejectsCorruptFonts` / `TestParseTTFRejectsBadTableWindows` —
     corrupt font → graceful error, no panic (recover-guarded)
  2. `TestEdit_PreservesFileMode` — 0600 mode preserved after edit
  3. `TestFlagOverridesKeepThinking` — `--keep-thinking` parses as bool switch

## Commits (one per bug)
- `2dc5620` fix(text2img): bound-check parseTTF table windows — no panic on corrupt font
- `3a8ff70` fix(tools): edit writes atomically via atomicWriteFile, preserves file mode
- `4b08400` fix(config): --keep-thinking CLI flag now works (was unhandled flag)
