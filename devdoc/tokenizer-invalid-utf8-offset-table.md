<!-- doc-class: normative -->
# The invalid-UTF-8 offset-table crash: what the regression list missed, and why

**Scope.** `internal/tokenizer`'s pre-tokenizer offset table
(`runeBoundaries`, `bytes.go`), the crash it caused on 2026-11-07, and the
SHAPE the regression samples had to gain so the crash cannot return unnoticed.
It is normative for anyone editing `splitGrouping`, `runeBoundaries` or the
sample list in `pre_panic_test.go`.

## The crash, as measured

Two worker runs died at the SEND-TIME token counter, exit status 2:

```
panic: runtime error: slice bounds out of range [:183] with length 182 [recovered]
panic: runtime error: slice bounds out of range [:141] with length 140 [recovered]
```

Sources (the rows that carried them):

```
/home/user/.gopi/sessions/01a1b2e7-b357-7779-87cf-8577ca292052/output.log   2026-11-07 06:19:30
/home/user/.gopi/sessions/01a1b2c0-c146-765a-b515-11c5d1ce8320/output.log   2026-11-07 06:21:35
```

The cause is single and small. `splitGrouping` built its rune-index →
byte-offset table by summing `runeLen(r)` over `[]rune(s)`:

```go
rs := []rune(s)
offs := make([]int, len(rs)+1)
at := 0
for i, r := range rs {
    offs[i] = at
    at += runeLen(r)
}
offs[len(rs)] = at
```

`runeLen` is the width of a rune **VALUE**; the width a rune **OCCUPIES** in a
string is a property of its **ENCODING**. They agree for every well-formed
sequence and disagree by two bytes for every INVALID one: `[]rune` turns an
invalid byte into `U+FFFD` (value width 3) that occupies exactly ONE byte in
`s`. Each invalid byte pushes the rest of the table two bytes right, and the
first slice whose upper bound sits out past the drift raises the panic. The
trigger is not input size and not CJK — it is a byte that is not valid UTF-8,
which a request reaches the ordinary way whenever a tool result holds a file's
raw bytes.

The fix (commit `7924d191`) reads the width off the encoding instead, where
`utf8.DecodeRuneInString` answers both halves at once and reports
`(RuneError, size 1)` for an invalid byte, so the table ends at `len(s)` by
construction.

## Question B: why were the regression tests green while production crashed?

**The premise is false, and it is worth stating the measurement rather than the
correction.** The three `...SurvivesInvalidUTF8` tests were never green while
production crashed: they were **born with the fix**.

```
$ git log --all --oneline --diff-filter=A -- internal/tokenizer/pre_panic_test.go
7924d191 fix(tokenizer): read rune widths off the encoding, not the rune value

$ git ls-tree 7924d191^ internal/tokenizer/pre_panic_test.go
                                     ← empty: the file did not exist before the fix

$ git show -s --format='%H %ad %s' --date=iso 7924d191
7924d191a30596afb1af083e6d943786c0a66523 2026-11-07 06:30:00 +0800 fix(tokenizer): ...
```

The two crash rows are `06:19:30` and `06:21:35`; the fix (which is also the
test file's first commit, `+219` lines) is `06:30:00`. **No window exists in
which those tests ran against the broken code.** The tests were not green while
production crashed — they simply had not been written yet, which is the normal
state of a regression test before the regression.

What the false premise was reaching for is real, though, and it is the useful
question: **the fix's own regression list was written by the person who had
just read the mechanism, so it spelled the mechanism instead of the incident.**
The section below measures what that cost.

## What the seven shipped samples cover, and the shape they miss

All seven samples carry invalid UTF-8, so under the OLD table every one of them
does drift the table past `len(s)`. Measured against the old table:

| sample | bytes | old table's bound | panics? |
|---|---|---|---|
| `"a\x80"` | 2 | `[:4]` | yes |
| `"\x89PNG\r\n\x1a\n"` | 8 | `[:9]` | yes |
| `"a\xe4\xb8"` | 3 | `[:7]` | yes |
| `"\xff\xff\xff\xff"` | 4 | `[:12]` | yes |
| `139 × 'a' + "\x80"` (named "the incident's shape") | 140 | `[:142]` | yes |
| `"hello \x80 world, 这是一个中文测试句子。"` | 48 | `[:50]` | yes |
| `"x\x80 12345 \x81\ty"` | 12 | `[:14]` | yes |
| **the real incident** | 140 | `[:141]` | yes |
| **the real incident** | 182 | `[:183]` | yes |

So the list does reach the crash — it is not a vacuous list. **What it does not
do is discriminate.** Every sample is invalid UTF-8 and nothing else; none of
them is the incident's SHAPE, and the entry named after the incident is not it:
`139 × 'a' + "\x80"` has no match and no gap ending past the drift, so its
bound is `[:142]` — an over-run of **+2** — while the incident's bounds are
`[:141]` and `[:183]`, over-runs of **+1**. A reader of the list would conclude
the drift alone is the defect.

**The measurement that settles it** — a HALF-FIX mutant: the runeLen table with
only its ENDPOINT clamped to `len(s)` (the intermediate entries keep the drift),
which is exactly the "just stop it panicking" patch someone will reach for.
Under it:

| sample | half-fix mutant |
|---|---|
| `"a\x80"` | PASS |
| `"\x89PNG\r\n\x1a\n"` | FAIL (panics) |
| `"a\xe4\xb8"` | PASS |
| `"\xff\xff\xff\xff"` | PASS |
| `139 × 'a' + "\x80"` **("the incident's shape")** | **PASS** |
| `"hello \x80 world, …"` | PASS |
| `"x\x80 12345 \x81\ty"` | FAIL (panics) |
| **incident `[:141]`, 140 bytes** | **FAIL (panics)** |
| **incident `[:183]`, 182 bytes** | **FAIL (panics)** |

Five of seven shipped samples pass a table that is still wrong everywhere
between its endpoints. The two incident-shaped samples do not.

### The shape, named

> An invalid byte **early** in the piece, a **long run of letters after it**
> that the matcher emits as a single unit, and a **trailing matched piece that
> reaches the end of the string** — so that the gap between the last match and
> the end (or the match itself) has an upper bound that sits out past the
> drift. The over-run is then `drift × 1` (`[:141]` for 140 bytes), not
> `drift × 2`.

The seven shipped samples have the drift. None has the reach.

## The reconstruction the tests now carry

`incidentSample(n)` builds a string of exactly `n` bytes in that shape — ASCII
prefix, one `\x80`, then `"bbbbb中\n"` — which reproduces both real panic
bounds with the REAL `groupingMatch` (not a re-implementation):

```
len=140  →  panic: slice bounds out of range [:141] with length 140
len=182  →  panic: slice bounds out of range [:183] with length 182
```

`pre_panic_test.go` now carries:

- the two incident samples in `invalidUTF8Cases`, so every arm that loops over
  the list (splitGrouping, splitPre, Count) meets the incident's shape;
- `TestIncidentSamplesAreTheIncident`, which re-derives, on every run: the
  sample's byte length, the runeLen table's over-run (exactly 2 bytes per
  invalid byte), the OLD algorithm panicking with the incident's own bound
  string, and the CURRENT algorithm not panicking and concatenating back.
  A future author who "simplifies" the sample loses the red half immediately.

`runeLenSummedBoundaries` / `splitGroupingRuneLenSummed` are the pre-fix
algorithm, kept in the TEST file as a shadow. They are not called by
production code; they exist so the defect stays executable rather than becoming
a story in a comment.

## The rule this leaves behind

1. **A regression list assembled from the mechanism is a list of the mechanism.**
   Add at least one sample reconstructed from the INCIDENT — its real length,
   its real bound — not only from the diagnosis.
2. **A "no panic" test and a "wrong table" test are different tests.** Keep both
   (this file has `TestRuneBoundariesMatchEncoding` for the invariant and the
   `...SurvivesInvalidUTF8` arms for the entry points), because a clamp satisfies
   the first and not the second.
3. **Never clamp an offset table.** `Rule 15` in this repo says a defensive clamp
   must be loud; in an INDEX the honest answer is that the table is wrong, so the
   width must come from the encoding (`utf8.DecodeRuneInString`), which ends at
   `len(s)` by construction and needs no bound at all.

## Verified on

`go test ./internal/tokenizer/ -count=1` green; the red evidence is the same
tree with `bytes.go`'s table reverted to the runeLen form (restore verified by
`git diff HEAD -- internal/tokenizer/bytes.go` being empty and by blob identity
against `HEAD:internal/tokenizer/bytes.go`). The session that measured it is
`01a1b349-d1a5-731c-812d-25a2c17fd9c1`; the raw captures are under its
`evidence/` directory.
