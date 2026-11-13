---
title: GOPI-CONFIG
section: 1
header: GoPi Manual
---

# NAME

gopi-config - inspect the configuration actually in force, and provision a host

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
  gopi config show [--settings PATH]
      Print the effective configuration and, for each value, the CHANNEL that
      decided it (flag / env var / settings.json / built-in default).
  gopi config env [--write] [--path P]
      Measure THIS host and render its environment description: hostname, OS,
      interface addresses, default gateway, and the ssh alias table as written.
  gopi config setup [flags]
      Provision a FRESH host to a run-ready state (idempotent).
  gopi config completion <shell>
      Emit a shell tab-completion script. <shell> is one of bash, zsh or fish
      — a bare word the command word accepts, never a bash|zsh|fish
      shorthand (that spelling is a syntax summary, not an invocation: the
      shell is a single POSITIONAL, so exactly one of the three is typed).
  gopi config -h
      This text.
```

<!-- END SYNOPSIS -->

# DESCRIPTION

**gopi config** is the configuration dimension's door, and it has two
directions. `gopi config show` **inspects**: it prints the effective
configuration and, for every value, the CHANNEL that decided it — flag,
environment variable, `settings.json`, or built-in default. `gopi config setup`
**provisions**: it takes a fresh host to a run-ready state, idempotently, so
running it twice is not a second install.

The two point at the same dimension, which is why they share one verb: read what
is in force first, then establish what is missing.

# SUBCOMMANDS

`show`
: the effective configuration, each value with the channel that decided it.

`env`
: measure THIS host and render its environment description — hostname, OS,
interface addresses, default gateway, and the ssh alias table as written.

`setup`
: provision a fresh host to a run-ready state. Idempotent.

`completion <shell>`
: emit a shell tab-completion script. `<shell>` is one of `bash`, `zsh` or
`fish`. The shell is a single POSITIONAL, so exactly one of the three is typed:
the `bash|zsh|fish` shorthand is a syntax summary, not a spelling the parser
accepts. This sub-verb is also the authoritative list of the CLI's published
verbs — the block it emits is what the man pages' verb index is checked against.

# SEE ALSO

`gopi config show -h`, `gopi config setup -h` (each sub-verb answers its own
page), `gopi -h`, `man gopi`, and this repository's `CONFIG.md` — the catalog of
every knob with its env var, `settings.json` key, CLI flag and source line.
