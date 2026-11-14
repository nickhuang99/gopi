<!-- doc-class: historical -->
# `chat_surface action=key` — a key SPELLING is refused, never typed into the line

Task `01a10e8f-ef3b-78ee-a853-ea684abd63b4`. Two defects in the chat console, one
of them found while fixing the other.

## The defect (A), and why it was silent

`chat_surface(action=key)` is **name-first**: a value that IS a table entry
(`enter`, `down`, `ctrl-c`, …) becomes that key's wire bytes, and *anything else*
is injected as its own bytes. That rule is right for a rune — `/` opens the
completion menu and no name produces it — and wrong for a value that is the
vocabulary's **own spelling** of a key:

```
chat_surface {"action":"key","text":"ctrl-u"}
```

was **accepted**. The seam queued the six letters `c-t-r-l---u` as one read unit,
the editor's decoder read them as printable runes, and they landed **in the
operator's editing line** — while the tool answered `injected=ok`. The caller sees
success, the human's text is edited, and nothing says the key never arrived.

The class is wider than the one example. The editor's own decoder (`decodeControl`)
maps **sixteen** control bytes to real keys (`0x01`→Home, `0x15`→KillToStart,
`0x17`→KillWordBack, `0x19`→Yank, …) and the injectable table names exactly
**one** of them (`ctrl-c`). So `ctrl-a/b/e/f/k/w/y/p/n/l/d/y`, the upper-cased
`CTRL-U`, the mis-cased `Down`, and every escape spelling of a byte
(`\u0015`, `\x15`, `\027`, `\n`, `\t`) all took the literal arm.

### The fix is a SHAPE test, not a longer table

Extending the table would fix the sixteen spellings somebody enumerated and leave
the seventeenth — and would *claim the vocabulary owns keys it deliberately does
not*. The defect is that a value plainly **shaped like a key** took the
literal-bytes arm. So `internal/lineedit/inject.go` now asks:

* `keySpellingShape(v)` → the shape (`wrong case` / `control-key name` /
  `escape spelling of a byte`) or "not a spelling";
* a match is **refused**, and the sentence names the whole legal set (read from
  `InjectableKeyNames`, so it cannot drift from the table) **and** the second
  route — pass the byte itself;
* a refusal charges **nothing** from the key ceiling (a refusal that spent the
  budget would be a second silent cost on the same call).

It is **conservative on purpose**: `/`, `q`, `知`, `\Users`, `./x` and a lone
backslash stay on the literal arm. Those are in the test as the positive control.

## The second defect, uncovered by the shape work

Trying to build the geometry case through `InjectKey`, one run answered:

```
InjectKey(" ")  -> no key: pass a key name or the raw bytes of one key
InjectKey("\n") -> no key: pass a key name or the raw bytes of one key
```

Both are **one byte of ordinary input** — the two most common bytes in a line —
and `InjectKeyBounded` ran `strings.TrimSpace` **before** the lookup, so both
became the empty string and the seam denied the very thing its own sentence asks
for. Fixed by trying the **raw** value first and applying the trim to the NAME arm
only (a model that sends `" up "` still gets Up). Regression case:
`TestInject_SpaceAndNewlineAreBytesNotPadding`.

## Defect (B) — the cursor "one row above the input": **NOT REPRODUCED**

Four paths, deliberately spanning the ways a line grows (measured in
`internal/lineedit/input_cursor_row_test.go`, on the editor's own grid model, and
the judge is the grid's `CurRow` after replaying every painted byte — never the
renderer's belief about where it put the cursor):

| path | input | premise asserted |
|---|---|---|
| near-full-row | 36 `a` | the line **wrapped** (≥2 drawn rows) |
| explicit-newline | `first\nsecond` | the value carries the newline |
| cjk-wide | 20 × `知` | wide runes force the wrap |
| long-scrolling | 120 `b` | the window scrolls |

Result: **green on all four.** The cursor's row is inside `[EditorTop,
EditorTop+DrawnRows)` on every path, the cursor's own row is never blank while
text sits above it, and it is never below the last text row.

**Honest scope of that "not reproduced"**: it is the editing line's own layout on a
90×24/40×12 pty under the chat layout. It does **not** cover a terminal resize
mid-edit, a real bracketed paste from a terminal emulator, or a scroll region the
terminal refuses. Those are named, not excluded silently.

### Repro recipe (for the operator, if it recurs)

```bash
cd <a gopi worktree> && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
./bin/gopi chat -h                              # the documented dump/observe sequence
# then, in that chat (or from another session):
gopi ui_state --target chat                     # read `cursor row=<r> col=<c>`
# and compare against the editing line's rows in the same dump
```

The recipe is executable and independent of my session: `gopi ui_state --target
chat` prints `cursor row=… col=…` plus the cell grid, and the editing line's rows
are the rows between the band and the status row.

## Bonus: `gopi ui_state --target chat` does **not** hang

I reported a hang in a first pass; it was **my own timeout being too short**.

```
$ /usr/bin/time ./bin/gopi ui_state --target chat        # exit 0, ELAPSED=2.92 s
$ /usr/bin/time ./bin/gopi ui_state --target chat --list # exit 0, ELAPSED=11.05 s
#gopi-ui-state-list scanned=251 roots=2
```

`--list` walks the profile's session tree (`findDumpFiles`, bounded by
`dumpScanDepth=3` / `dumpMaxCandidates=256`) and reads every candidate's header —
here **83,247** session directories, 498 chat dumps. A runtime `SIGQUIT` dump put
the process inside `filepath.WalkDir` at `dumpread.go:963`, i.e. **working, not
blocked**. So: not a hang, but a real cost worth knowing before putting it in a
tight loop.

## Files

* `internal/lineedit/inject.go` — `keySpellingShape` + `keySpellingRefusal` +
  the refusal branch; the raw-before-trim ordering fix.
* `internal/agent/chat_surface.go` — the tool's own description of `action=key`
  now states the refusal, and its "needs text" sentence reads the legal set from
  `lineedit.InjectableKeyNames` instead of a hand-typed list (Rule 13/14).
* `internal/lineedit/inject_keyshape_test.go` (new) — the shape contract + the
  positive control + the `decodeControl` property + the whitespace regression.
* `internal/lineedit/input_cursor_row_test.go` (new) — the four cursor paths.
* `cmd/agent/chat_surface_key_refusal_e2e_test.go` (new) — the refusal at the real
  stack: a `gopi chat` REPL on a real pty, its own line editor, its own
  `chatSurface`, one round asking for `ctrl-u` **and** `q`.

## Overlap with `01a10e22` (the unified input path) — named, not touched

The sibling task reworks how a line reaches the model. Everything here is **above**
that seam: the tool's `action=key` path, the injector's own value→bytes decision,
and the editing line's layout. No file in the `01a10e22` scope was edited.
