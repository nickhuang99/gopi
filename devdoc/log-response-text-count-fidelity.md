<!-- doc-class: historical -->
# `gopi log --response --text` does NOT lose text — the "4727 vs 3831" delta is a bytes-vs-runes unit mix-up

Task: `01a0ddcd-2b52-771f-8fea-2c3dc42b5b56` (sibling: `01a0ddac-61f6-7970-8fba-c2352bcf6ca8`, `01a0dd26-a578-7e81-a81e-f751dcf993eb`)
Branch: `task/01a0ddcd2b52771f8fea2c3dc42b5b56`
Session measured: `a5e1c3e9-fcfb-450c-a0a8-abcea5688f25`

## 0. Verdict in one line

The fourth "loss channel" is **not a loss channel**. Channel D — `gopi log <id> --response --text` — is a
**byte-exact, rune-exact reproduction of the authoritative JSONL**. The claimed delta is the report
comparing the output's **BYTES** (`| wc -c` → 4727) against the transcript's **RUNES** (python `len()` →
3831) on **CJK text**, where one rune is 3 UTF-8 bytes. The arithmetic closes to the byte.

**Sibling 01a0ddac's `A = B ≠ C` verdict STANDS.** It is not partially falsified: the fourth channel was
never lossy, so the "only the operator's mouse copy is lossy" story is complete for the channels measured.

## 1. Reproduced — the exact command block

The sibling's own reproduction, from its session log (`call_00_aQTBc9hdbJoA2bwKDgwd1168`):

```bash
gopi log a5e1c3e9-fcfb-450c-a0a8-abcea5688f25 --response --text | wc -c
# → 4727
gopi log a5e1c3e9-fcfb-450c-a0a8-abcea5688f25 --response --text | wc -l
# → 44
python3 -c "…sum len of assistant text blocks in ~/.gopi/sessions/<id>/session.jsonl…"
# → assistant text blocks: 3   total chars: 3831
```

Re-run today (`gopi` built from this branch; session has grown to 5 blocks):

```bash
gopi log a5e1c3e9-fcfb-450c-a0a8-abcea5688f25 --response --text           # → 9139 bytes / 6067 runes
gopi log a5e1c3e9-fcfb-450c-a0a8-abcea5688f25 --response --text | wc -l   # → 86
gopi log a5e1c3e9-fcfb-450c-a0a8-abcea5688f25 --json                      # → 5 blocks / 6062 chars (faithful)
```

## 2. Byte accounting — the delta is EXACTLY the unit conversion

The session's assistant text blocks, measured independently from `session.jsonl`:

| block | runes | UTF-8 bytes |
|---|---|---|
| 0 | 1080 | 1089 |
| 1 | 1887 | 1913 |
| 2 | 864 | 1722 |
| **first 3 (the sibling's measurement)** | **3831** | **4724** |
| 3 | 985 | 2018 |
| 4 | 1246 | 2392 |
| **all 5** | **6062** | **9134** |

- `Σ runes (first 3) = 3831` ← **the sibling's python number, exactly**
- `Σ bytes (first 3) + 3 wrapper newlines = 4724 + 3 = 4727` ← **the sibling's `wc -c` number, exactly**

The report's two figures are the **same three text blocks in two different units**. Its "3831 chars"
*is* its "4727 bytes"; the 896-unit gap is 3-byte CJK runes, not missing characters.

Verified today on the full 5 blocks, where `FormatResponse`'s output was byte-compared against the
transcript-derived expectation:

```
sum chars blocks : 6062
sum utf8 bytes   : 9134
expected + \n    : 9139 bytes, 6067 chars
actual           : 9139 bytes, 6067 chars
EXACT MATCH      : True
```

## 3. The layer that adds: ONE newline per block, and nothing else

`FormatResponse(s, textMode=true)` — `internal/gopilog/formatter.go:332` — is the whole renderer:

```go
for _, iter := range s.Iterations {
    for _, text := range iter.TextBlocks {
        if textMode {
            sb.WriteString(text)   // the block, verbatim
            sb.WriteString("\n")   // the ONLY wrapper
        }
```

There is **no width, no wrap, no band machinery, no cap and no column logic** on this path.
`N_render = Σ runes(block_i) + N_blocks`. That is the complete, documented wrapper.

**The reader adds nothing either**: `reader.go:167` stores each block as `strings.TrimSpace(block.Text)` +
`"\n"` on render — identity for any block that does not carry leading/trailing whitespace (all five here).

### The "cut mid-clause" observation, explained

The sibling reported a Chinese paragraph cut mid-clause ("…里面的明确" → jumps to the next block). The real
block sequences are intact; the *instrument* cut them. Its own diagnostic was:

```bash
gopi log <id> --response --text | tail -5 | cut -c1-200
```

under this shell's locale, `LC_CTYPE="C"` — so **`cut -c` counts BYTES, not characters**. Measured on
the line the sibling quoted:

- the full line is **168 characters / 274 bytes**;
- `cut -c1-200` therefore emitted the first **200 bytes** = **142 characters**, ending at
  `…"no project GOPI.md in chat mod` — an unterminated word, mid-sentence, exactly the "cut mid-clause"
  shape reported.

Every quoted fragment of the sibling's reproduction was output through that same byte-truncating filter,
which is why "truncation" looked visible. It is the measuring stick, not the replay.

## 4. Width dependence — measured, not assumed (two runs)

```bash
COLUMNS=40  gopi log <id> --response --text > w40.txt    # → 9139 bytes / 6067 runes
COLUMNS=200 gopi log <id> --response --text > w200.txt   # → 9139 bytes / 6067 runes
cmp w40.txt w200.txt                                     # → IDENTICAL
```

**Width-independent.** This confirms the source reading: the `--response --text` path shares no wrap/band
machinery with the display path, so the `chat_flow_wrap` hypothesis is dead for this channel.

## 5. The four-channel verdict, against 01a0ddac's `A = B`

| channel | what it is | verdict | evidence |
|---|---|---|---|
| **A** transcript | `session.jsonl` assistant text blocks | **FAITHFUL** (authoritative) | 5 blocks / 6062 runes / 9134 bytes |
| **B** painted dump | the 117x26 rendered screen, `frames=687` | **FAITHFUL** | sibling `01a0dd26` measurement; not re-measured here (see §6) |
| **C** mouse copy | the operator's terminal selection | **LOSSY** (unchanged) | sibling `01a0dd26`; not re-measured here |
| **D** `--response --text` | gopi's own non-interactive replay | **FAITHFUL** — byte-exact | measured here: output == JSONL concatenation to the byte |

**D does not falsify A = B ≠ C.** The union verdict is *unchanged*: the transcript is faithful, the
painted screen is faithful, and the operator's mouse copy loses text. The reported fourth channel is a
measurement error, not a loss layer.

**The one real lesson the sibling was right about**, though not where it aimed: *an instrument is a
channel too, and an unexamined instrument lies.* Its falsification instinct was correct and it did the
honest thing by filing the claim with its own reproduction; the claim simply does not survive
re-measurement. The mirror `--response --text` is a **sound** self-check: it is byte-exact and
width-independent.

## 6. The regression test

`internal/gopilog/response_text_fidelity_test.go` — pins the count relationship in **both** units, each
summed independently from the JSONL, allowing only the one documented wrapper:

```
runes(FormatResponse(s, true)) == Σ runes(block_i)     + N_blocks
bytes(FormatResponse(s, true)) == Σ utf8bytes(block_i) + N_blocks
```

- `TestResponseTextCountRelationshipMatchesTranscript` — the pin, plus a parsed-block-count check (a
  reader that drops a block fails here).
- `TestResponseTextBytesExceedRunesForCJK` — the falsification as an executable fact: on CJK input
  `bytes > runes` **always**, so that comparison is not evidence of anything.
- `TestResponseTextPreservesEveryBlockWhole` — the red-proof (§above): tail cut, rune cap, dropped
  block must each be **detected**, intact output must **not** be flagged, and the report's mixed-unit
  comparison must be shown to be broken on intact output.

### RED → GREEN

`formatTextMaxLen` is `300`; a cap anywhere on this path is exactly the defect class. The pre-fix
channel was reproduced by injecting that cap into the reader's `case "text"` (`reader.go:167`):

```
--- FAIL: TestResponseTextCountRelationshipMatchesTranscript
    --response --text runes = 432, want 457 (= 453 transcript runes + 4 wrapper newlines)
    --response --text bytes = 1116, want 1197 (= 1193 transcript bytes + 4 wrapper newlines)
--- FAIL: TestResponseTextPreservesEveryBlockWhole
    intact --response --text output was flagged as lossy
FAIL
```

Reader restored (`git diff` clean on `reader.go`), same tests:

```
--- PASS: TestResponseTextCountRelationshipMatchesTranscript (0.00s)
--- PASS: TestResponseTextBytesExceedRunesForCJK (0.00s)
--- PASS: TestResponseTextPreservesEveryBlockWhole (0.00s)
ok  	github.com/nick/gopi/internal/gopilog
```

The RED is an **injected-defect** proof, not a reverted-commit proof: the false alarm needed no
production fix, so there is no pre-fix commit to check out — the named layer (`reader.go:167` text-block
path, the only place a `--response --text` block can be altered) was made to carry the defect and the
pin caught it by both units. The red-proof inside the third test additionally proves the predicate
rejects the two truncation *shapes* the report described.

## 7. What was added or dropped

**Nothing was added to or dropped from the text.** The only transform on the path is
`N_blocks` wrapper newlines (`formatter.go:345-347`), and the regression test allows exactly that and
nothing else.

## 8. Real normalizations found nearby (named, NOT defects, NOT fixed here)

Two places on adjacent paths really do normalize, and neither is on `--response --text`:

1. **`reader.go:167-171`** — `strings.TrimSpace` on every text block, and a **whitespace-only block is
   silently dropped** (`if text != ""`). No counter records the drop (Rule 15 territory). Effect on this
   measurement: **zero** (no block here is whitespace-only or padded). Worth a follow-up if a view ever
   needs to report block-level provenance.
2. **`formatter.go:108`** — `FormatSession` (the **default** `gopi log <id>` view) caps each text block
   at `formatTextMaxLen = 300` runes via `truncateRunes`. This is a **deliberate, pre-existing display
   cap**, and `FormatResponse` deliberately has **no cap** — confirmed by source, and by the fact that
   the 5-block run above came back complete. It is a real truncation channel only if someone reads the
   default view as an authoritative transcript; `--response --text` is the faithful view.

## 9. Not Done (honest)

- **Channel B was not re-measured here.** The session `a5e1c3e9` was started without `--dump-screen`, so
  it has no dump to compare (the reachability gap the sibling itself reported). The `A = B` half of the
  verdict therefore rests on sibling `01a0dd26`'s `frames=687` measurement, which this task does not
  contradict and does not independently reproduce.
- **Channel C was not re-measured** (no mouse involved); its lossy verdict is inherited unchanged.
- The two normalizations in §8 are **named, not fixed** — the `TrimSpace` drop has no observable
  effect on any measured channel, and the `FormatSession` cap is intentional product behaviour.
- The §3 `cut -c` finding is a **diagnostic** result: this repo's tooling is not changed by it. If the
  byte-slicing trap is worth guarding, that is a separate task (it lives in the shell/`LC_CTYPE=C`
  environment, not in gopi's Go).
