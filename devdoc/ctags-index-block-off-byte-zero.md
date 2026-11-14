<!-- doc-class: historical -->
# The ctags pseudo-tag block is not always at byte 0 — a byte-0 probe rejected every Exuberant index

Task `01a12b6388b57199892195533ff2aaa8` · branch `task/01a12b6388b57199892195533ff2aaa8`
· host **hostb-Laptop**, ctags `Exuberant Ctags 5.9~svn20110310` · measured 2026-10-11.
Host is a client (Rule 28): the fix touches only `internal/tools/symbol.go` — the
product's own code path, not the system layer.

## The defect

`code_search`'s ctags fast path was dead on this host, permanently and silently.
`TestTagsIndexProbeOnRealCtagsBytes` failed with *"the header probe rejected a
COMPLETE index"*; `tagsIndexValid` (`internal/tools/symbol.go`) required the
pseudo-tag block at **byte 0**:

```go
return bytes.HasPrefix(head, []byte(tagFileHeaderPrefix)) && !bytes.ContainsRune(head, 0)
```

Every real Exuberant index of this repo failed it, so `ensureTags` classified the
artifact as foreign/corrupt **on every call** and paid a full-tree ctags run
instead — measured below: 0.85–0.89 s per `code-search` call, forever, with no
error surfaced to the caller. The symbol lookup itself still answered (via the
ripgrep fallback), which is what kept it quiet.

## The root cause, measured — and the corrected mechanism

**Exuberant writes a BYTE-SORTED file and therefore sorts its six pseudo-tags
among the entries.** `' '` (0x20) < `'!'` (0x21), so any entry whose search
pattern's *first line* begins with a space sorts **before** the block. This repo
carries 20 such entries — the Go parser's rendering of multi-line string literals
(`internal/heavytest/boundary.go`): the entry's *name* becomes the literal's
first line, six spaces and all.

That is why the byte-0 probe shipped looking correct: a **small** directory has no
such literal, nothing sorts before `!`, and the block really does land at byte 0
there. The index is the repo's *bytes*, so which bytes precede the block is a
property of the repo's content, not of ctags or its version.

```
$ ctags -R --languages=Go --extra=+q --fields=+ln -f idx.tags .   # cwd = repo root
$ ls -l idx.tags
-rw-rw-r-- 1 nick nick 12008061 ... idx.tags
$ python3 -c "d=open('idx.tags','rb').read(); p=b'!_TAG_FILE_FORMAT'; ..."
occurrences of '!_TAG_FILE_FORMAT': [4294, 9111758, 11233762]
  off=4294     line=21     line_start=True   next_byte=b'\t'   ← the pseudo-tag LINE
  off=9111758  line=63516  line_start=False  next_byte=b'"'    ← an ordinary entry
  off=11233762 line=77435  line_start=False  next_byte=b'"'    ← an ordinary entry
bytes 0..40: b'      GOPI_HUMAN_TERMINAL=1 go test -run'
line  1: b'      GOPI_HUMAN_TERMINAL=1 go '
...
line 21: b'!_TAG_FILE_FORMAT\t2\t/extended '
line 22: b'!_TAG_FILE_SORTED\t1\t/0=unsorte'
```

Two consequences the fix has to respect, both visible in that transcript:

1. **The block's position is data, not a constant.** A byte-0 test is only
   accidentally right, and it is wrong exactly where it matters (a real repo).
2. **The prefix text is not unique.** An entry whose search pattern quotes it —
   this file's own `const tagFileHeaderPrefix = "!_TAG_FILE_FORMAT"`, indexed by
   the host's ctags — must never be mistaken for the block. That is why the
   matcher demands a **line start AND a following tab**, not a bare substring.

## The fix (`internal/tools/symbol.go`)

1. **`tagsIndexValid` is now position-independent inside a bounded head window**:
   the NUL refusal stays (a NUL-filled artifact is never a ctags index), and the
   window is then scanned for a pseudo-tag *line*.
2. **`tagPseudoTagLineOffset(head) int`** is the ONE definition of "this window
   carries the block" — the prefix at a line start (or the window's first byte)
   **and** followed by `tagPseudoTagSeparator` (`'\t'`) — so the probe and the
   tests pinning the producer layouts cannot disagree about what counts. The
   scan is bounded (`bytes.Index` over the window, resuming past each non-match),
   so a window of foreign bytes terminates. (On the re-land below this merged
   with the *identical* predicate master had already landed as `ffa1cf4f8`; the
   merge note at the function says which spelling was kept and why.)
3. **`tagFileHeaderPrefix`'s comment corrected** — it claimed "universal-ctags and
   exuberant both open with" the block, which is what made the byte-0 assumption
   look safe. It now carries the measurement.
4. **`tagHeaderProbeBytes` 256 → 64 KiB.** It is no longer a one-shot first-line
   read but the head *budget* of a scan, and it must cover the deepest block a
   real producer writes: **measured 4294 bytes / 20 preceding entries**, i.e. 15×
   headroom. One cheap read (64 KiB vs this repo's 12 MB index) — and still a
   *bounded* read, which is load-bearing: `code_search` runs on every symbol
   lookup, so an unbounded scan would cost the hot path what a full ctags run
   costs.

Nothing else moved. `tagsIndexComplete`'s two torn-write defences (trailing
newline; per-root size witness) and `tagsRegenSettleWindow` are untouched, and the
binary-search path needed no change: inline pseudo-tag lines sort among the
entries as ordinary lines and simply never match a queried name.

## Regression guard

`internal/tools/tags_index_layout_test.go` (new):

- `TestTagPseudoTagLineOffset` — the matcher itself: byte-0 shape, the exuberant
  shape (block behind space-leading entries), prefix mid-line, prefix without a
  tab, prefix truncated by the window's end, an entry quoting the prefix text,
  empty window.
- `TestTagsIndexProbeAcceptsBlockNotAtByteZero` — **the minimal repro** (Rule
  10.5): one entry before the block is enough to reproduce the defect. It also
  keeps the *rejections* honest — quoting-only, NUL-filled, NUL-after-block, empty.
- `TestTagsIndexProbeOnRealCtagsLayout` — drives the probe over bytes this host's
  ctags actually produced and pins the layout as a measured fact (`t.Logf`), so a
  universal-ctags host (block at 0) still passes.
- `TestEnsureTagsUsesTheIndexWhenTheBlockIsNotAtByteZero` — the wiring, not the
  predicate: a valid block-off-zero index newer than the sources must be
  **served**, with `0` ctags generation runs recorded by the shim.

The existing `TestTagsIndexProbeOnRealCtagsBytes` is untouched and now green — it
is the byte-0-shaped (universal-ctags) half of the same contract, and it is what
made this defect visible in the first place.

## Relation to the orphaned earlier attempt

Task `01a12b49-8921-7684-8928-4c17e259417f` reached `completed` (exit=0) with
commit `ffa1cf4f8` (`fix(code_search): accept a ctags index whose pseudo-tag block
is not at byte 0`, 4 files, 407 insertions) — and that commit is reachable from
**no ref at all** (`git for-each-ref --contains ffa1cf4f8` empty, its
branch/worktree gone). This run read it as information (Rule 23 — never continue
on another attempt's branch), re-derived the fix on this run's own branch, and
independently reproduced its central measurement (offset 4294, line 21, 20
preceding space-leading entries). The orphan's existence as an unreachable object
is filed as a Rule 41 bug row; the object is not the deliverable, the branch is.

## Verification

Build / Tests / Ran / Result: see `task-report.md` in this run's session
directory. Headline: the previously failing test is green, the layout suite is
green, the CLI reuses the 12 MB index (pre-fix binary: 1 ctags regeneration run
**per query**, 0.85–0.89 s; fixed binary: **0** generation runs, 0.05–0.08 s).
