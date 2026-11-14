<!-- doc-class: historical -->
# Task Report: @prefix payload-from-file CLI input

## Completed
- `gopi @file` / `gopi --prompt @file`: a positional or `--prompt` value
  starting with `@` reads the task payload from that file verbatim
  (leading whitespace preserved, trailing newline stripped) — solves
  submitting text with shell special characters, quotes, or structured
  formats (JSON/YAML/code) that are impractical to type on the command line.
- `@-` reads the payload from piped stdin (`cat payload.json | gopi @-`),
  even when positionals exist.
- `\@...` escape hatch: literal prompt starting with `@`, backslash stripped.
- Missing/unreadable `@` file is a HARD error naming the path — never silent
  fallback to prompt text (a wrong task reaching the LLM is worse than a
  fast failure).
- Documented in `gopi -h` usage syntax block (internal/config/help.go).

## Not Done
- Live stdin-pipe (`@-`) E2E run through the real API was skipped (bash
  timeout; the path is unit-tested via an os.Pipe test instead).
- Byte-exact BOM handling / binary-file rejection not implemented (text
  payload assumed).

## Verification
- Build: `go build ./cmd/agent/ ./internal/config/` OK
- Tests: `go test ./cmd/agent/ -run 'TestReadPayload|TestReadInput'` — all
  PASS (incl. existing image/text classification regressions);
  `go test ./internal/config/` PASS
- E2E: `./bin/gopi @/tmp/prompt.txt` with a special-char JSON payload →
  LLM read it verbatim, replied OK (exit=0, 11.4s, 98.4% cache hit)
- Commit: `0bea9c0 feat(cli): @prefix payload-from-file input ...`

## Notes / pitfalls for future agents
- help.go's usage text is a Go RAW string literal: a backtick inside it is a
  syntax error — never use backticks in help text.
- readInput processes positionals in order; the `@` check must run BEFORE
  the os.Stat classification (an `@path` file exists, so it would otherwise
  hit the text-file branch with the `@` still attached).
