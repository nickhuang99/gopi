---
title: GOPI-TOOL
section: 1
header: GoPi Manual
---

# NAME

gopi-tool - gopi's local tool commands, collected under one noun

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
  gopi tool -h                     this page
  gopi tool <command> [args…]      run a DIRECT command (first block)
  gopi tool <group> <action> [args…]   run a GROUPED command (second block)
```

<!-- END SYNOPSIS -->

# DESCRIPTION

**gopi tool** is the one door to the local tools. Every command below it is
reached ONLY as `gopi tool <command>`; the command's own page is one hop further
(`gopi tool <command> -h`). The bare spelling (`gopi tool capture`, `gopi tool
shot`, …) is retired: it refuses and names the spelling to use instead, so a
typo there never becomes a model run.

The page carries two blocks, and the shape of the block says what the second
word is:

- **direct commands** — `gopi tool <command>` IS the command. `code-search
  <symbol>` is where a symbol is defined; `keyword_search <query>` is full-text
  search; `screen_capture`, `capture` and `act` are the capture→act→verify loop
  on the LIVE X11 desktop; `shot` screenshots a URL or the local chat UI;
  `vision_analyze`, `text2img-verify`, `gif`, `annotate` and `proxy` round it
  out.
- **grouped commands** — the command keeps its own second level: `sandbox
  <create|run|show>`, `text2img <input.txt> …`, and `web [search|fetch]`.

A third block is marked **internal**: `ledger-window` and `scratch-gc`. They
work and they are exposed on purpose — a capability with no door can only be
tested by unit tests. Their output is a contract for this repository's own
gates, so a script may depend on it; a human should not, since the surface is
not promised to be stable. (The block used to carry a third entry, a
document-corpus checker; that verb was deleted with the document pipeline it
read, and a page that still promised it would be describing a command this
binary answers with "unknown command".)

One declared exception to the one-door rule: `gopi web` is the browser chat UI's
own carrier AND this group's door to the web TOOLS (`search` / `fetch`). Both
spellings reach the SAME predicate, so neither can shadow the other and no
spelling of these tools is ever spent as a prompt. The grouping is what puts the
two tool faces on this page; the top-level word is what starts the server.

# SEE ALSO

`gopi tool <command> -h` (each entry's flags and examples are on its own page),
`gopi -h`, `man gopi`, `man gopi-web` (the same word's other meaning).
