<!-- doc-class: historical -->
# ctags index layout: the pseudo-tag block is not always at byte 0

Task `01a12b498921768489284c17e259417f` · branch `task/01a12b498921768489284c17e259417f`
· host: Exuberant Ctags 5.9~svn20110310 · measured 2026-10-11.

## The defect

`code_search`'s ctags fast path was effectively dead on this host:
`TestTagsIndexProbeOnRealCtagsBytes` failed with *"the header probe rejected a
COMPLETE index"*. `tagsIndexValid` (`internal/tools/symbol.go`) tested
`bytes.HasPrefix(head, []byte("!_TAG_FILE_FORMAT"))` — it required the pseudo-tag
block at **byte 0**. Every real Exuberant index on this repo failed that test, so
`ensureTags` classified the artifact as foreign/corrupt, **deleted the 12MB
index** and paid a full-tree ctags run on the next call — repeatedly, silently.

## The root cause — CORRECTED against the briefing

The briefing's stated mechanism (*"Exuberant does not write the six `!_TAG_*`
lines"*) is **wrong**, and so is its explanation of *why* the byte-0 test worked
elsewhere. Both were measured and refuted:

```
$ grep -c '^!_TAG' .gopi-tags          # the block IS there
6
$ grep -n '^!_TAG' .gopi-tags | head -1
21:!_TAG_FILE_FORMAT	2	/extended format; --format=1 will not append ;" to lines/
$ grep -b -o '^!_TAG_FILE_FORMAT	' .gopi-tags | head -1
4294:!_TAG_FILE_FORMAT
$ LC_ALL=C sort -c .gopi-tags && echo SORTED
SORTED
$ stat -c %s .gopi-tags
12291326
```

**Exuberant writes all six pseudo-tags, but writes a byte-sorted file and
therefore SORTS the block among the entries.** On this repo it lands at line 21 /
byte offset **4294**, because exactly 20 entries sort before `!`:

```
$ LC_ALL=C awk -F'\t' 'substr($1,1,1) < "!" {n++} END{print n+0}' index.tags
20
$ head -1 index.tags | cut -c1-60
      GOPI_HUMAN_TERMINAL=1 go test -run <Test> -v ./<pkg>/
```

Those entries are the Go parser's output for the multi-line string literals in
`internal/heavytest/boundary.go` (`heavytest/boundary.go:112`): the parser formats
the literal into a search pattern whose first line begins with six spaces, so the
entry's *name* is `"      GOPI_HUMAN_TERMINAL=1…"` and `' '` (0x20) < `'!'` (0x21).

**Why the byte-0 test shipped working** (the briefing's claim that "on a small
directory only does the header reach byte 0" is right in outcome, wrong in
mechanism): a small directory has no such multi-line literal, so nothing sorts
before `!` and the block lands at byte 0 — verified directly:

```
$ printf 'package a\n\n// Hello does x.\nfunc Hello() {}\n' > a.go && ctags -R --languages=Go --extra=+q --fields=+ln -f m.tags .
$ head -c 40 m.tags
!_TAG_FILE_FORMAT	2	/extended format; --format=
```

The index is the file's **bytes**, so *which bytes land in the window* depends on
the repo's content, not on ctags or its version. Two smaller plausible stories
were tested and eliminated: `--format=1` and a `~/.ctags` config file (neither
exists here), and the repo scale itself (a hand-built minimal fixture reproducing
the literal — `const Multi = "      GOPI_HUMAN_TERMINAL=1 …\n" + …` — emitted the
block at byte 0, 780 bytes).

## The fix

`internal/tools/symbol.go`, two edits, no new tool and no new dependency:

1. **`tagFileHeaderPrefix`'s doc comment** — the false claim *"universal-ctags and
   exuberant both open with `!_TAG_FILE_FORMAT`"* corrected, with the measurement
   (line 21 / offset 4294 / the 20 space-leading entries / `boundary.go`) inlined.
2. **`tagsIndexValid`** — replaced the byte-0 prefix requirement with a bounded
   head scan for a pseudo-tag **LINE**, factored into `tagPseudoTagLineOffset`
   so the probe and the tests share ONE definition of "this window holds the
   block". Two conditions keep it a real test rather than a substring search:
   the prefix must open a line **and** be followed by a tab
   (`tagPseudoTagSeparator`). That is what excludes an ordinary entry whose
   search *pattern* quotes the prefix text — this repo's index has two such Go
   entries (offsets 9376658 and 11504509, byte before `"`, byte after `$`).
   A NUL byte anywhere in the window still rejects (the 2026-09-10 incident), and
   `errTagsIndexInvalid` still means "cache miss, remove and regenerate".

`tagHeaderProbeBytes` grew **256 → 64KiB**: it is now the head *budget* of a scan
rather than a one-shot first-line read, and it must cover the deepest block a real
producer writes (measured 4294 — 15x headroom). Still one cheap read, 192x
smaller than the index, versus a **0.94s** full-tree ctags run on this host.

**Nothing else changed, and that is deliberate.** `tagsIndexComplete`'s two
torn-write defences (trailing newline; per-root size witness incl. the
unverified-written witness) and `tagsRegenSettleWindow` are untouched. The
binsearch path needed no change either: inline pseudo-tag lines sort among the
entries as ordinary lines, they simply never match a queried name, so they cannot
truncate a cluster.

## Why no gate was added

`v1`…`v4` "reject the old byte-0-only parser" gates were designed and rejected.
A test asserting `tagsIndexValid` returns **false** for an off-byte-0 block is
exactly the forbidden "green suite that encodes the bug" shape and would have to
be deleted. The ratchet that matters is positive — the layout fixtures below.

## Verification

Build / Tests / Ran / Result: see the task report (Rule 5). Headline: the
previously failing test is green, the corrupt-index and torn-index suites are
unchanged and green, and the real CLI reuses the 12MB index (cold **0.881s** →
warm **0.066s**, identical size/mtime/inode across two calls).
