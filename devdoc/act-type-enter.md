<!-- doc-class: historical -->
# devdoc: `gopi act type --text` — escape decoding + `--enter`

Task: `01a0c527-5086-799d-b520-32c4718c348e` · branch `feat/act-type-enter` ·
commits `bfebc066` (feature) + `26f727e0` (help/comment truth about the typable
set) + `4ffb201e` (help truth about the injection path).

## The reported defect (attachment `gopi_act_type_enter.txt`)

```
$ gopi act type --text "say hello"
say hello{"action":"type",...,"detail":"typed 9 character(s)"}
$ gopi act type --text "say hello\n"
say hello\n{"action":"type",...,"detail":"typed 11 character(s)"}   # ← wrong
$ say hello\n                                                       # ← never submitted
```

The user expected Enter. What happened: bash double quotes keep `\n` as the
two characters `\` `n`; the CLI passed those two characters straight through;
`internal/shot.TypeText` typed backslash (1 keysym) + `n` (1 keysym) = **11**
keypresses for `say hello\n`, and nothing was ever submitted.

## Root cause

Not in the X11 layer — `internal/shot/xactuate.go` already maps `'\n'` to the
Return keysym (`asciiPlainKeysyms['\n'] = keysymReturn = 0xff0d`), and
`gopi act key --key return` works. The defect was that **nobody decoded the
escape**, so the string that reached `TypeText` contained no newline at all.
The fix therefore belongs at the CLI boundary (`cmd/agent/cli_act.go`), where
the argv string is turned into the text to type.

## What was implemented

### 1. An escape grammar, decoded before typing (`decodeActText`)

| escape | result | escape | result |
|---|---|---|---|
| `\n` | LF (pressed as Return) | `\v` | 0x0B |
| `\r` | CR | `\a` | 0x07 |
| `\t` | Tab | `\0` | NUL |
| `\b` | 0x08 | `\\` `\"` | literal backslash / quote |
| `\f` | 0x0C | `\xHH` | byte ≤ 0x7F |
| `\uHHHH` / `\UHHHHHHHH` | code point ≤ 0x7F | | |

**An unknown escape is a loud error, never a silently-typed literal**
(GOPI.md Rule 15): `--text 'x\q'` exits 1 with
`unknown escape \q in text value — supported: …` and injects **nothing**. A
dangling trailing backslash is refused the same way. `\x80` and above are
refused with `\x80 is above the printable ASCII ceiling 7F` rather than
injecting a byte the X11 layer cannot map.

The decode step is single-pass and boundary-aware (`\xHH` consumes exactly two
hex digits), so `'\\n'` = backslash + `n` and `'a\nb'` = `a`, Return, `b`.

### 2. `--enter`: the Return appended to the text

`--enter` appends `'\n'` to the decoded value **unless it already ends in a
decoded newline** (no double Return: `--text 'x\n' --enter` presses Return
once). Because it appends into `req.text`, the Return goes through the exact
same `XActuator.TypeText` path as the rest of the value — the shortcut and an
explicit `gopi act key --key return` cannot drift apart. The JSON result says
which path was taken: `"detail":"typed 10 character(s) + Return (--enter)"`.

The JSON also echoes the **decoded** text (`"text":"say hello\n"`), so a caller
can see what was sent rather than infer it.

### 3. Backward compatibility

Text without a backslash is returned unchanged and typed byte-identically to
the old binary — measured below.

## The typable set is SMALLER than the decode set (measured, documented)

Decoding and typing are different questions, and the CLI does not silently drop
a character it cannot press. Of the decoded set:

- **reach the screen**: printable ASCII (incl. Tab) and `\n` (pressed as Return);
- **refused loudly by `internal/shot`, nothing injected**: `\r`, `\a`, `\b`,
  `\f`, `\v`, `\0` (no keysym in `keysymForRune`) and every non-ASCII code point.

```
$ ./bin/gopi act type --text 'a\rb'
gopi act: rune '\r' (U+000D) has no X11 keysym mapping in this client — ASCII
printable and the named keys only; arbitrary Unicode would need an input method
```

This is a pre-existing limit of the keysym layer, **not** a regression of this
change (`'a\rb'` was refused before it too) — and it is *safe*: the refusal
happens before any I/O, so a half-typed string is never left on screen. What
was wrong was that the help text advertised `\r` as typable. Commit `26f727e0`
corrects the help and the `decodeActText` doc comment to state the typable set.
A CR that genuinely needs to reach a terminal should be spelled
`gopi act key --key return` (which the docs already call the "documented verb"),
not `\r`.

## Live verification recipe (reusable — Xvfb + xev as the oracle)

Never trust "it looks right": the only proof that Enter was pressed is the X11
**key event stream**. Recipe (`/home/user/workspace/_act-enter-verify/`):

```bash
Xvfb :241 -screen 0 1280x800x24 -ac -noreset &
DISPLAY=:241 xev > xev.log 2>&1 &                 # the oracle: prints every key event
wid=$(xdotool search --sync --name "Event Tester" | head -1)
xdotool windowfocus "$wid"
DISPLAY=:241 ./gopi-new act type --text "say hello\n"
awk '/^KeyPress/{p=1} /^KeyRelease/{p=0} index($0,"keysym 0xff0d"){if(p)c++} END{print c+0}' xev.log
```

- `Old` binary = the worktree built at `master` (`3710dc8e`); `New` = this branch.
- Count Return **presses** (0xff0d) before/after each invocation — a return count
  of 1 with 10 keypresses for `say hello\n` is the whole proof, and the old
  binary's 0/11 is the bug.

`live-verify2.sh` (this branch's exact command + other shell spellings +
refusals), `live-verify3.sh` (plain-text compatibility against the old build).
Both exit 0; output is quoted in the task report.

## Files

- `cmd/agent/cli_act.go` — `decodeActText`, `--enter` parsing, help text.
- `cmd/agent/cli_act_test.go` — `TestDecodeActText` (20 cases), `TestParseActArgsTypeEnter` (7 argv cases incl. no-double-Return), `TestActTextEvidenceIsReadable`.
- No change to `internal/shot` — the Return mapping already existed; the X11
  layer is used exactly as it was.
