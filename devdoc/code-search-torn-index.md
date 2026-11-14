<!-- doc-class: historical -->
# A torn `.gopi-tags` used to degrade code_search silently — the writer is not atomic

Task `01a11827` (worktree `gopi-01a11827a7667a549104fc49a10566d1`).
Symptom it started from: `TestCodeSearchBackendFindsDefinition` fails —
`did not find CodeSearch in internal/tools/search.go` — over a 5.9MB
`.gopi-tags` whose `sort -c` reports disorder at line 2, while the healthy
artifact is ~14.7–17.7MB.

## 1. Mechanism — who writes it, and is the write atomic? **No.**

`ctags` writes the index **in place**.

| measurement | value |
|---|---|
| writer | `runCtags` (`internal/tools/symbol.go`) → `ctags -R --fields=+ln -f <root>/.gopi-tags .` |
| open mode | `fdinfo` during a run: `flags: 0100001` = `O_WRONLY\|O_APPEND` |
| rewrite over an existing index | **same inode** across the whole run; size `17,756,967 → 0 → 17,756,967` |
| window in which a reader sees a partial file | ~10.2s of a 12.5s full-tree run (sampled every 0.1s, 101 of 102 samples at size < final) |
| atomicity | **none** — no temp file, no `rename(2)`. ctags truncates its target to 0 and appends entries as it walks the tree |

Consequences, both measured on this repo:

1. **A reader that opens the file mid-write** sees the header block
   (`!_TAG_FILE_FORMAT` …) plus a *byte-ordered prefix* of the final content.
   `tagsIndexValid` — header prefix + no NUL in the first 256 bytes — **passes
   it**. The binary search then misses every symbol sorting after the writer's
   current position; `searchTagIndex` reports a clean miss; `codeSearchImpl`
   falls back to ripgrep and answers with `SymbolKind: "reference"` hits. No
   error, no log, no counter (Rule 15 violation).
2. **A ctags killed mid-write persists that partial file.** SIGKILL at 1.69MB:
   header intact, last line `1\tdevdoc/poc-reframe-verdict/case-B-impos` (no
   trailing newline), `sort -c` DISORDER. This is reachable in production —
   `code_search` runs ctags under `FileToolTimeout()`, and a run that outlives
   the timeout is killed with its process group.
3. **The tear survives the kill for as long as staleness allows**: a source tree
   whose 5.9MB index is *newer* than every source file is served, not rebuilt.
   Once a subsequent ctags run keeps dying the same way (index mtime advancing
   each time), every reader is served a partial body — the observed 5.9MB state.

### The torn shapes that beat the old probe

| shape | old header probe | detected now by |
|---|---|---|
| writer stopped **mid-entry** (no trailing newline) | passes | tail is not newline-terminated |
| writer stopped **exactly on a line boundary** | passes | the per-root **size witness** |
| NUL fill / foreign file / truncated header | **rejected** | unchanged (`tagsIndexValid`) |

The second row is the hard one: those bytes are *shape-identical* to a small
complete index (valid header, byte-ordered entries, trailing newline), so no
property of the bytes alone distinguishes them. What distinguishes them is
**observation**: the reader previously served a *finished* index of a different
size, so the file was replaced after that observation.

A prefix of a sorted file is itself sorted — this is why a "scan the tail and
demand byte order" check does **not** solve the prefix case (it only catches a
rewritten/duplicated window). Recorded so a later reader does not simplify the
check back to an ordering test.

## 2. The completion signal actually available on disk

`ctags` prints **no end-of-file trailer**. The string `sorted intact` appears in
neither the installed binary (`strings $(which ctags) | grep -c`) nor any
generated index (`grep -c` over the artifact): **0** in both. What a complete
index has is a trailing newline (ctags appends whole lines):

```
$ tail -c 40 expA.tags | xxd | tail -1
… e7 89 88 ef bc 89 0a                        # …（最终版）\n
```

A complete 17.7MB index is byte-stable across five post-exit samples and passes
`LC_ALL=C sort -c` (exit 0) — so the ordering assertion has no false-positive
risk on real output.

## 3. The fix (`internal/tools/symbol.go`)

`tagsIndexComplete(tagsPath)` — two cheap probes, never a full-body scan (the
index is 17MB; a full ctags run is 12.5s):

1. **whole-line tail** — the last byte must be a newline;
2. **size witness** — a per-root record of the size at which this process last
   saw that root's index *finished* (clean tail **and** byte-ordered tail
   window, `tagsIndexTailScanBytes` = 2MiB). Getting past step 1 with a
   **different** size means the artifact was replaced since that observation.

A witness recorded from an index we wrote but did **not** see complete is stored
`unverified` and can never serve as the "finished" witness — otherwise the
artifact we just created would validate itself.

Wiring, so that neither silence nor a rebuild storm is possible:

* `ensureTags` — an unfinished index is a **cache miss**: remove + regenerate.
  Bounded by `tagsRegenSettleWindow` (5min, well above one ctags run): if the
  root was rebuilt for this reason inside that window and the index is
  unfinished *again*, the cause is a concurrent writer, and the call returns a
  **loud** `errTagsIndexIncomplete` error instead of re-indexing the tree on
  every call (Rule 27 — never manufacture load to satisfy a signal).
* `queryDefinition` — a **clean miss** over an unfinished index is not a miss:
  a symbol sorting after the writer's position is legitimately absent from the
  bytes on disk right now. It rebuilds once and re-checks; if that still yields
  an unfinished index it reports `errTagsIndexIncomplete` rather than letting
  the fallback answer for it. A miss over a **finished** index still falls back
  to ripgrep exactly as before.

### The cost of the check, and why it is cached

The 2MiB tail scan measured **8.24 ms** per call on this repo's 15.9MB index
(`go test -bench` over `tagsIndexComplete`), which is not a price the
highest-frequency tool should pay on every lookup. The verdict is therefore
cached per root, keyed on **(size, mtime)** with the same TTL the staleness
verdict already uses (`stalenessCacheTTL`, 2s) — the same shape as the existing
`cachedStalenessVerdict`. The artifact cannot change without changing one of
those two facts: ctags *begins* a rewrite by truncating, so even its size-0
moment invalidates the entry. Measured at the CLI against the real repo:
first call 1.85s (it regenerated — the sources were newer than the index),
then 0.13s and 0.10s with the artifact untouched.

## 4. Not Done (honest)

* The check cannot see a tear that lands on a line boundary in a process whose
  first interaction with that root is the torn file (no prior observation, no
  witness). Windows: process start (the spawner relaying `GOPI_CHAT_SURFACE_ID`
  is the precedent for that gap) and a *concurrent* ctags inside one process.
  Both leave the **end** of the file (the tear zone), not the middle, and both
  are self-corrected on the next miss — a miss is re-checked against a rebuilt
  index rather than served. Stated rather than papered over.
* The tail window is the last 2MiB, not the whole body: a tear older than that
  window (a file damaged, then grown past it) is not seen by the ordering test —
  the tail check and the witness carry that case.
* ctags itself is not made atomic (it is an external binary). The rule-side fix
  would be a temp+rename in `runCtags`; not done here, because the reader-side
  detection is what the epic asked for and a rename changes the artifact's
  inode contract for concurrent readers.

## 5. Verification

```
Build:   go build ./...                    → OK (no output)
Vet:     go vet ./internal/tools/          → clean
Tests:   go test ./internal/tools/ -run 'Torn|Incomplete|TagsIndex|QueryDefinition|CodeSearch' -count=1
         → ok  github.com/nick/gopi/internal/tools   1.300s
         (new: TestTagsIndexProbeOnRealCtagsBytes, TestQueryDefinitionHealsMidEntryTornIndex,
          TestQueryDefinitionDoesNotReadAMissOffATornIndex,
          TestEnsureTagsRefusesRepeatedRebuildOfUnfinishedIndex)
Runner:  make test-budget PKGS='./internal/tools ./cmd/agent'   → see the Rule 5 report
Ran:     gopi tool code-search CsCliTornProbe --root <torn repo>
Result:  pre-fix binary (pristine HEAD): exit=0,
           CsCliTornProbe  …/badrepo/pkg.go:3  reference  func CsCliTornProbe() {}
         post-fix binary: exit=0,
           CsCliTornProbe  pkg.go:3  func  func CsCliTornProbe() {}
         and the artifact healed on disk: 22158 bytes (torn, no trailing byte
         newline) → 860 bytes, newline-terminated, containing the symbol.
```

The torn fixture in the CLI run is planted, not simulated: a header-valid,
byte-ordered body ending mid-entry with **no** trailing newline and an mtime
newer than the sources, so it is served rather than regenerated.
