<!-- doc-class: historical -->
# code_search: the index publish is ATOMIC (concurrent rebuilders in one worktree)

Task `01a12be1-ec32-7e90-bc74-561a9b38d2ff`. Symptom it started from:
`TestCodeSearchBackendNoSymbol` (cmd/agent) went red with
`tags index incomplete (rebuilt 0s ago …)` — an unrelated assertion failing in
whatever test happened to call code_search, whenever two processes (or two
concurrent rebuilders) shared ONE worktree.

## 1. Two independent defects, one shared artifact

The earlier torn-index work (`devdoc/code-search-torn-index.md`) made a
half-written index **visible** and refused it loudly. It could not make the
artifact **safe to share**, because the writer was still this:

| measurement | value |
|---|---|
| writer | `runCtags` → `ctags -R --fields=+ln -f <root>/.gopi-tags .` |
| open mode (fdinfo during a run) | `flags: 0100001` = `O_WRONLY\|O_APPEND` |
| rewrite over an existing index | **same inode**; size `17,756,967 → 0 → 17,756,967` |
| window a reader sees a partial file | ~10.2s of a 12.5s full-tree run |
| atomicity | **none** — no temp file, no `rename(2)` |

Two consequences followed from that one fact:

1. **A second process could never be sure the index was finished.** With an
   in-place writer, "the bytes are a valid prefix" and "the bytes are a small
   complete index" are the same bytes; the only instrument left was a per-root
   **size witness**. In-process that works — the witness is our own prior
   observation. Across processes it does not: process B never observed A's
   publish, and the first thing B did with a peer's fresh index was compare it
   against B's own stale size and call it **unfinished**.
2. **The refusal then fired at zero distance.** `queryDefinition` /
   `ensureTags` rebuild on an unfinished index and take the loud
   `errTagsIndexIncomplete` branch when one is unfinished inside
   `tagsRegenSettleWindow` — so a peer's healthy index produced
   `rebuilt 0s ago` and a red test, and the rebuild it triggered replaced the
   peer's index with the same shape, which is how the loop sustained itself.

## 2. The fix

### (a) Atomic publish — `internal/tools/symbol.go` `runCtags`

ctags now writes a **unique temp file in the index's own directory**
(`tagsIndexTempPattern = tagsIndexFileName + "-tmp-*"`, same directory so the
rename is one filesystem) and the finished artifact is `os.Rename`d onto
`tagsPath`. `rename(2)` is atomic inside one directory, so **no reader ever
observes a partial index**, whoever the writer is.

Two details that are load-bearing, both measured here:

- **`CreateTemp`'s placeholder is removed before the ctags run.** ctags is meant
  to CREATE its target; handing it a freshly created empty file re-introduces
  the refusal class ("doesn't look like a tag file") on a path that now exists
  on every run. `TestEnsureTagsHealsCtagsRefusal` pins this with the shim's own
  existence arm as the instrument (a shim that refuses whenever its target
  exists can only succeed if the path was fresh).
- **The mode is set explicitly** (`tagsIndexFileMode`, 0644) before the rename:
  `CreateTemp` makes the temp 0600 and `rename(2)` replaces the target NODE, so
  publishing without the chmod would silently narrow every index — the same
  node-replacement trap the sandbox profile symlinks document.

### (b) The completion probe stops confusing "replaced" with "unfinished"

The size witness was answering two questions with one number. It is now split by
**file identity** (`os.SameFile` on the recorded `os.FileInfo`):

| what happened to the artifact we served | probe verdict |
|---|---|
| **different node**, different length | a PUBLISH — ours or a peer's. Drop the stale size and judge the new artifact on its own bytes (tail + byte order) |
| **same node**, different length | an IN-PLACE rewrite of the very file served — a writer that does not publish atomically. **Unfinished** |
| **same node**, same length | judge on contents; an unchanged `(size, mtime)` is answered from the cached verdict |

The third row is what keeps `TestTagsIndexProbeOnRealCtagsBytes`'s restored
index accepted (an in-place writer that has reached that length has written
every byte), and the second row is what keeps the line-boundary tear caught:
that tear is shape-identical to a small complete index, and the node identity is
the only remaining signal.

The "do not accept the artifact we just published" bound survives as an
**artifact** fact rather than a number: `rejectedSize` is armed only by a size a
probe actually REJECTED, and it is cleared by any publish and by any acceptance,
so a complete index of that size is still served (no rebuild storm, no refusal
of a writer that did its job).

The loud refusal is unchanged and still carries the concurrency: when an index
genuinely never finishes, the caller still gets
`tags index incomplete (rebuilt and still unfinished — a concurrent writer is
indexing this root)` / `… (rebuilt Ns ago and the index on disk is still
unfinished) — refusing to re-index the whole tree on every call` (Rule 15).

## 3. Regression coverage (Rule 10.5)

New file `internal/tools/concurrent_rebuild_test.go`:

- `TestTagsIndexCompleteAcceptsPeerPublishedIndex` — the peer-publish contract:
  a witness recorded from one artifact must not condemn a different, COMPLETE
  artifact. **Red pre-fix** —
  `a peer's freshly published COMPLETE index was judged unfinished`.
- `TestCodeSearchServesACompleteIndexWhileAPeerRebuildsTheSameRoot` — the epic's
  regression, carrying a **concurrent rebuilder in the same worktree**: a second
  `runCtags` over the same root is started and its write is HELD open mid-flight
  by a ctags stand-in (a two-marker file handshake, so the read happens exactly
  while the writer is in flight, not in a hoped-for race window). It asserts the
  published artifact is byte-identical while that writer runs, and that
  code_search still answers with a real definition. **Red pre-fix** —
  `the published index changed while a concurrent rebuilder was still writing
  (127 bytes on disk, 554 bytes served before) — the write is not atomic`.

## 4. Honest alignment of two pre-existing tests

Neither is deleted or weakened; both encode a premise the atomic publish
removes, so they were re-pointed at what now protects the same property:

- `TestEnsureTagsHealsCtagsRefusal` — was: a planted index makes ctags refuse,
  and self-heal costs one extra attempt. Now: **the writer never hands ctags an
  existing target**, so the refusal arm cannot fire — 1 attempt, self-heal 0,
  planted index replaced. The refusal machinery is still covered by
  `TestEnsureTagsCtagsRefusalRetryIsBounded` (a ctags that refuses an ABSENT
  target must fail once, loudly, never loop).
- `TestTagsIndexProbeOnRealCtagsBytes` — dropped its manual witness seeding
  (`noteCompleteTagsIndex(dir, int64(len(full)))`), which is no longer the
  signature and was wrong besides: it recorded the TORN file's identity. The
  probe records the witness itself when it accepts the complete index.

## 5. Not Done (honest)

- **The node identity is per-process.** A teardown that lands on a line boundary
  in a process whose FIRST interaction with the root is the torn file still has
  no witness (no prior observation) — the same window
  `devdoc/code-search-torn-index.md` records as open. The atomic publish shrinks
  it to foreign writers only: every artifact *we* publish is a whole file.
- **A foreign in-place writer is still possible** (`ctags` invoked by hand,
  an older gopi binary, a different tool writing the same path). It is handled —
  SameFile + different length is refused — but it is refused LOUDLY rather than
  made safe, which is the deliberate boundary: we cannot make another process's
  write atomic, we can only refuse to serve a torn artifact.
- The tail window is still the last 2 MiB (unchanged from the earlier work).

## 6. Rule 31 — the changed surface, run for real (two concurrent processes, one worktree)

The epic's scenario, executed against TWO binaries built from THIS worktree's tree
(one at the commit before the fix, one at the fix) over the SAME worktree root,
with both processes forcing an index rebuild at once. Raw transcripts:
`/home/user/.gopi/sessions/01a12be1-ec32-7e90-bc74-561a9b38d2ff/artifacts/{base,cs}-evidence.txt`.

```
$ ( gopi tool code-search EnsureTags & gopi tool code-search RunCtags & )   # same worktree, both rebuild
```

| measurement | pre-fix (base `2509842b5`) | post-fix (`dc38ae016`) |
|---|---|---|
| `incomplete` refusals (run A / run B) | **1 / 1** | **0 / 0** |
| distinct index sizes sampled during the run | 7 (2.8MB → 12.4MB across the run) | **1** (12,441,210 B) |
| exit status | 1 / 1 | 0 / 1 (1 = the honest "no symbol found" for `RunCtags`) |
| temp files left in the worktree | 0 | 0 |

Both processes visibly walked the index climbing byte by byte pre-fix (7 distinct
sizes, from 2.8MB) and both refused with `tags index incomplete`; post-fix the
size never changed under either reader and neither refused. `git status
--porcelain` in the worktree names no `.gopi-tags*` path in either arm.

## 7. Rule 5/31 verification run (scoped to the diff)

```
$ go build ./...                       # OK
$ go test -count=1 -run 'TestCodeSearchBackendNoSymbol' -v ./cmd/agent/
--- PASS: TestCodeSearchBackendNoSymbol (0.90s)
ok  	github.com/nick/gopi/cmd/agent	4.718s
$ go test -count=1 -run 'TestTags|TestQueryDefinition|TestEnsureTags|TestCodeSearchBackend|TestCodeSearchServes' ./internal/tools/
ok  	github.com/nick/gopi/internal/tools	5.275s
$ make test-budget PKGS='./internal/tools ./cmd/agent'
<see the report's ## Verification block>
```

## 8. Acceptance run with a LIVE peer rebuilder in the same worktree

The epic's own scenario, executed as a real overlap rather than in-test only:
a peer process (a gopi binary built from this tree) rebuilds `.gopi-tags` in the
SAME worktree — each round touches a source file so the index is stale and the
peer's lookup must regenerate it — while the cmd/agent code_search test runs.
Transcript:
`/home/user/.gopi/sessions/01a12be1-ec32-7e90-bc74-561a9b38d2ff/artifacts/acceptance-peer-rebuild.txt`.

```
=== binary: /home/user/workspace/_tmp/gopi-cs (built from 76ee1c45b) ===
--- PASS: TestCodeSearchBackendFindsDefinition (1.98s)
--- PASS: TestCodeSearchBackendNoSymbol (0.10s)
ok  	github.com/nick/gopi/cmd/agent	3.204s
=== peer rounds run / 'incomplete' refusals === 12 / 0
=== worktree residue === (empty)
```

## 9. Not Done — a HARDER variant this fix does not close (measured, not inferred)

While validating, one variant was exercised that is NOT the epic's scenario and is
still red: a peer that **deletes** the shared artifact instead of rebuilding it
(12 rounds of `rm -f .gopi-tags` + a lookup). Transcript:
`artifacts/acceptance-live-overlap.txt`.

```
--- PASS: TestCodeSearchBackendFindsDefinition (1.70s)
    cli_code_search_test.go:169: CodeSearch(no-symbol) error: code_search: …/.gopi-tags:
        tags index incomplete (rebuilt and still unfinished — a concurrent writer is indexing this root)
--- FAIL: TestCodeSearchBackendNoSymbol (1.84s)
=== peer rounds run / 'incomplete' refusals === 12 / 0   ← the PEER never refused
```

**This is the deliberate refusal working, not this fix failing**, and the
distinction is worth stating precisely: with an arm that removes the artifact, a
reader can rebuild and lose the artifact *between* its own publish and its own
probe, so "I published and the index is still unfinished" is literally true and
the arm reports it. It is the same branch that makes a genuinely
never-finishing index loud (Rule 15) rather than silently served. The measured
shape is recorded here rather than smoothed over because the operator scenario it
belongs to (a peer deleting the shared index in a loop) is a different defect
from the one this task was dispatched for — **the epic's scenario, a concurrent
REBUILDER, is green under the same live overlap** (§8).
