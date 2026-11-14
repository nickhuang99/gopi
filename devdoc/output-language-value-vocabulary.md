<!-- doc-class: historical -->
# The reply-language knob's VALUE VOCABULARY on `gopi -h`

Epic: `01a0e049` — *"gopi help should give enum value"*. Branch:
`task/01a0e04940897cb8b968c38820e6cacc`.

## The question, verbatim

> `gopi --output-language chinese` is this correct tag name for chinese? gopi help
> gives no example or theme of how to define language. is it like locale naming? or
> enum? such vital information is missing in help and not giving example is a big gap

Answer, established by executing the CLI (not by reading source):

* `chinese` is **NOT** accepted — it is a language *name*, and the value is a
  BCP-47-ish language **tag** (`zh`, `zh-CN`, `en-GB`). It is neither an enum nor a
  locale: `zh_CN` (underscore) is refused too.
* `--output-language zh-CN` **is** accepted, and the run's replies come back in Chinese.

## What was already fixed, and what was still missing

Memory `#6194` recorded an earlier task (`01a0dee0`) that had already closed half
this gap: the knob shipped documented in `gopi --help-env` alone, and `gopi -h` did
not mention language anywhere. That task put a row on the front door.

Measured on this branch's base (`abcd75dd`), the row read:

```
  --output-language TAG     language gopi ANSWERS in (GOPI_OUTPUT_LANGUAGE, settings key
                            output_language); reply language only — the model's
                            reasoning is unaffected
```

So the knob was discoverable and the **value was not**: `TAG` was an unexplained
placeholder, with no shape, no examples and no statement of what is *not* a value.
That is precisely the operator's question, and no help page answered it.

**The asymmetry that made this a defect rather than a nicety.** The refusal message
already taught the vocabulary — `is not a language tag (want e.g. zh, zh-CN, en-GB)`
— while the page taught nothing. A user therefore learned the grammar *by being
refused first*, and a question asked **before** the first attempt (the operator's
exact situation) got no answer at all. The error path is not a substitute for the
page: it can only speak after the mistake.

## The change

1. `internal/config/output_language.go` gains the vocabulary as **constants**, each
   built from the others so every surface quotes the same words (Rules 1b/13/14):
   `OutputLanguageValueShape` ("language tag"), `OutputLanguageValueQualifier`
   ("BCP-47-ish"), `OutputLanguageExampleTags` ("zh, zh-CN, en-GB") and
   `OutputLanguageNotAValueClaim` (``NOT a language name (`chinese`) or a locale
   (`zh_CN`)``). `outputLanguageRefusalFmt` is now built from them, so the error and
   the page **cannot** teach different value sets.
2. `OutputLanguageRowSummary` renders the vocabulary on the `gopi -h` Options row.
3. `exampleRows` gains a copy-pasteable line:
   `gopi --output-language zh-CN "reply in Chinese"`.
4. The catalog row (`internal/config/settings.go`, the `gopi --help-env` table) uses
   the same constants.
5. `CONFIG.md` states the TAG-not-name-not-locale distinction and re-points its
   read-at line anchors.

### The budget was paid, not overrun

`cmd/agent`'s `topLevelUsageMaxLines = 160` counts
`len(strings.Split(FormatUsage(), "\n"))`, and it was **already 160** on the base —
the added example line would have made it 161. So the single-dash paragraph was
tightened (2 lines → 1, information kept: *"-z" is refused (pass it via --prompt
-z)*). Measured with a temporary probe test, removed afterwards: 160 == 160.

## Verification

* `gopi -h` render (before/after captures in the session evidence dir).
* A real run pair with a **live** model: `chinese` refused + English reply;
  `zh-CN` accepted + Chinese reply (`你好 —— GoPi 在此，随时待命。`).
* `internal/config` + `internal/standards` green (the latter includes the
  `CONFIG.md` line-reference gate).
* The new guard has a RED PROOF: reverting the row to its pre-epic form turns
  `TestHelpPageStatesTheValueVocabulary` and
  `TestOperatorQuestionIsAnswerableFromThePage` red.

## The class is guarded, not just the row

`output_language_value_vocabulary_test.go` checks the page against the **parser**,
not against a copy of itself: every example the page teaches must pass the real
predicate, and every spelling the page denies must really be refused — the denied
spellings are *extracted from the claim constant* (`backquotedTokenRe`), never
re-typed, so the test cannot drift into checking a list it invented. That is the
property that makes a help page evidence: it is a claim about behaviour, and this
guard is what keeps the claim true.

## Pitfall for the next run (tooling, not config)

In this session the `edit` tool **silently dropped the tail** of four multi-line
replacement payloads — a const, two `fmt.Fprintf` refusal lines, two help rows and a
`CONFIG.md` table row were each left truncated to a blank line. The only symptom was
a downstream build error (`undefined: OutputLanguageRowSummary`); three of the four
were invisible until then. Repairs used assertion-checked line surgery in python
(each anchor asserted to occur exactly once), then `gofmt -l` + a real build.
**Read back every edited region**; a tool reporting success is not a tool reporting
the whole payload landed.
