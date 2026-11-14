<!-- doc-class: normative -->
# The front door must name the knob — `gopi -h` and the reply-language preference

Task `01a0dee036bb73a2a01742dc5e861728` (Epic: *"gopi help mention no language
preference — gopi claimed user preferred output language is ready, but help
mentions no such thing. how can user know this without help text?"*).

## The defect, measured

The reply-language preference was **complete** before this change: three
channels (settings key `output_language`, `GOPI_OUTPUT_LANGUAGE`,
`--output-language`), a resolver with precedence and an invalid-value report, a
prompt directive prepended to both system prompts, a CONFIG.md row, and its own
tests. What it did not have was a sentence on the page a user reads first.

```
$ grep -i language <the rendered front door>   # before
(no output)
$ gopi --help-env | grep -i language           # before
  GOPI_OUTPUT_LANGUAGE    output_language    --output-language
    Language of gopi's REPLIES (BCP-47 tag, e.g. zh-CN) …
```

So the knob was real and undiscoverable at the same time: `--help-env` is the
page you read *after* you already suspect a knob exists. Nobody looks for a
setting they have never heard of. **"Documented somewhere" is not
"discoverable."**

## The change

1. `internal/config/output_language.go` — two exported constants
   (`OutputLanguageScopeClaim`, `OutputLanguageRowSummary`) own the words the
   help surfaces render. They exist because the row and the gate that guards it
   must quote the SAME text (Rules 13/14), and because every spelling inside the
   summary is built from the knob's own constants — the page cannot advertise a
   flag the parser refuses or a settings key the resolver ignores.
2. `internal/config/help.go` — an Options row
   `--output-language TAG  language gopi ANSWERS in (GOPI_OUTPUT_LANGUAGE,
   settings key output_language); reply language only …`.
3. `internal/config/output_language.go` — the `--help-env` catalog row now uses
   the same scope sentence, so the two pages cannot drift apart.

### The trap: the front door has a line budget

`cmd/agent`'s `TestTopLevelHelpFitsItsBudget` (limit 160 lines) measures the
rendered page — and the page was **already 176 lines on master, i.e. red before
this task touched it** (measured: `FormatUsage()` at `HEAD~` = 176 lines,
current tree = 159). Adding a row without paying for it would have deepened an
inherited overflow, so the wording was tightened page-wide (`gopi help.go`;
`git diff internal/config/help.go`), with the *capability* sentences kept —
which knob a verb needs, what to read next — and the machinery detail that
already has its own `-h` page dropped.

The page was also **missing two verbs the dispatch table sends** — `gopi
doccheck` and `gopi memory backup` — which
`TestTopLevelHelpCoversEveryDispatchedSubcommand` flags. Both are now rows.

## The guard (`internal/config/help_knob_discoverability_test.go`)

Two halves, deliberately:

- **The instance** — `TestTopLevelHelpShowsOutputLanguage` pins the required
  spellings on `gopi -h` (flag, env var, settings key, scope claim, the
  capability sentence), reading the expected strings from the knob's constants,
  and asserts the advertised flag actually parses.
- **The class** — `TestTopLevelHelpExposesEveryUserFacingKnob` walks the WHOLE
  catalog with ONE discovery rule (`userFacingKnobHelpWord = "language"`): any
  row whose help prose says what the agent does *for the user* must be named on
  the front door, with the flag and settings key it attaches, unless it is
  listed BY NAME with a reason in `userFacingKnobHelpExempt`. Non-vacuity
  floors: the rule must match ≥1 row and must still match the output-language
  row.

The neighbouring `TestTopLevelUsageShowsSettingsKeysAndFlags` spot-checks a
hand-picked flag list; a spot-check can only prove the rows somebody remembered
to add — which is exactly how this defect happened. Discovery, not enumeration,
is what keeps the next user-facing knob visible.

## Red proof (Rule 10.4 — the smallest sample)

Reverting only the `--output-language` row in `internal/config/help.go` (the constants and the
test file left in place) turns:

- `TestTopLevelHelpShowsOutputLanguage` red — 8 missing-spelling failures
  (`--output-language`, `GOPI_OUTPUT_LANGUAGE`, `output_language`, the scope
  claim, `ANSWERS`);
- `TestTopLevelHelpExposesEveryUserFacingKnob` red — names
  `GOPI_OUTPUT_LANGUAGE` as a catalog row with no front-door exposure.

Restoring the row returns both green. A guard that passes on the un-fixed tree
would prove nothing.

## Not done (honest)

- The discovery rule's vocabulary is one word (`language`). A user-facing knob
  whose help never says the word is not covered — the rule is a ratchet, not a
  proof of completeness.
- `gopi help.go`'s page is still one line under its ceiling; the next added row
  needs its own subtraction. Nothing enforces a *margin*, only the cap.
- CONFIG.md's file:line anchors were re-pointed (`internal/config/help.go:286/394` →
  `settings.go:133` / `internal/config/help.go:502`) because the gate resolves them; the table
  row that defined the help surface pointed at the wrong file entirely (the
  helper's `envVars`, never the renderers).
