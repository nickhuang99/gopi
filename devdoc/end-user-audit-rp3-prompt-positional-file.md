<!-- doc-class: historical -->
# RP-3 — `--prompt TEXT` silently dropped a non-image file positional

**Slice 8 of the end-user-audit epic (run-path sweep), bug row RP-3 / F2.**
Fixed 2026-09-27.

## The defect

`gopi --prompt "instruction" notes.txt` sent the instruction and **nothing else**.
`readInput()` read `notes.txt`, appended its contents to a local `texts` slice,
and then the prompt-precedence branch returned only `--prompt`:

```go
// Prompt precedence: --prompt/GOPI_PROMPT > positional text.
if p := strings.TrimSpace(overrides.Prompt); p != "" {
    in.prompt = p          // <- texts is never read again
}
```

Nothing was attached, nothing was read into the payload, nothing was refused,
and stderr said nothing — exit 0. The file was consumed and discarded. The
`.png` half of the same row worked (an image positional is attached before the
precedence branch), so the defect was specific to **text** positionals.

The help page made it worse than a silent bug, because it *promised* the right
behaviour and the code did not follow its own promise:

```
--prompt TEXT   task instruction; positionals become files/images
gopi [options] --prompt TEXT a.png   Run one task with files/images attached
```

## The rule, stated once

With `--prompt TEXT` present:

- **images** among the positionals are attached natively (unchanged);
- **text** positionals still carry task input: the instruction stays the prompt
  and each text positional's contents are appended to it, as their own bulleted
  block;
- nothing is ever accepted-and-dropped in silence — with no `--prompt`, the
  positionals remain the prompt exactly as before.

`joinPromptAndTexts` implements it in `cmd/agent/main.go`; `gopi -h` states it
(`promptPositionalRuleClaim` in `internal/config/help.go`, rendered by the
`--prompt` row and asserted by `TestUsageStatesThePromptPositionalRule`).

What reaches the wire, before → after:

```
before:  [EPIC GOAL]\nRP3-PROMPT-TOKEN instruction\n …      (file token: 0 occurrences)
after:   [EPIC GOAL]\nRP3-PROMPT-TOKEN instruction\n\n- RP3-FILE-TOKEN-7f3a: …
                                                        \n- second line of the text file
```

## Why append, and why bullets

Two fixes remove the silence: *use the text* or *refuse*. Use was chosen because
it is what the code's docstring already claimed, and because
`--prompt TEXT a.png` is the documented idiom — refusing would break it for text
while keeping it for images.

The append is `instruction\n\n- <line per positional line>`, not a bare
concatenation: the blank line keeps a multi-line document from merging into the
instruction's paragraph, the bullet marks the block as appended input, and the
multi-line case keeps one bullet per line (`strings.ReplaceAll` on the join
separator) so a document still reads as one. Both separators are named
constants (`promptTextJoiner`, `positionalTextBullet`, `textJoiner`) — the join
is part of the documented contract now, so a bare literal could drift from the
help text (Rules 1b/14).

An empty file positional contributes nothing, and an image-only run gains no
tail; both are pinned by tests, because "append always" would have added a stray
`- ` to every `--prompt TEXT a.png` run.

## How it was proved (0 real tokens)

The request boundary is the only oracle for "what reached the model" — rc and
stdout are blind. A local Anthropic-shaped echo server (fixtured in the run's
session dir, never the repo — Rule 35) recorded every request body, driven by a
probe script that records argv/rc/transcript per probe, in the project's own
sandbox (`gopi sandbox create` + `GOPI_HOME`) with `env -i`:

```
before/prompt-txtfile/req-000.json : grep -c RP3-FILE-TOKEN-7f3a -> 0
after/prompt-txtfile/req-000.json  : grep -c RP3-FILE-TOKEN-7f3a -> 1
after/prompt-png/req-000.json      : image_blocks=1   (the image half still holds)
```

Both binaries were built from this worktree (`gopi-before` from `HEAD`, meaning
the pre-fix source). Red → green of the regression test is in the same evidence
file: with the pre-fix branch restored by hand, the new tests fail with
`prompt = "instruction", want "instruction\n\n- RP3-FILE-TOKEN file body"`.

## Traps a future reader should know

- **The `-h` page is the contract, and it is the thing that drifted.** A wording
  fix is part of a behaviour fix here, not documentation garnish: the old row is
  what made the drop invisible (the operator believed the file was sent).
  `TestUsageStatesThePromptPositionalRule` now fails if the drift wording
  returns.
- **The help wraps**, so asserting a sentence in `FormatUsage()` must compare
  whitespace-collapsed text (`strings.Join(strings.Fields(usage), " ")`) — a raw
  `strings.Contains` fails on a sentence that is present.
- **A devdoc written by the `write` tool can time out after the bytes land**
  (exactly what happened here: `write: timed out after 30s`, file present, hook
  not run). Rule 36: classify it in the same commit — `gopi doccheck --fix`,
  which wrote the `historical` row and the banner.
