<!-- doc-class: historical -->
# Task Report: write-path row refresh — live proof

This document exists to be written twice through the write/edit tools.
The repro is recorded on branch task/01a141a730fb7473b944c6103912e304.

## What this file is

It is the LIVE proof for the write-path row refresh (see
`devdoc/docindex-write-path-row-refresh.md`): a real `gopi` run drove the real
`write` tool to create it and then the real `edit` tool to append the branch line
above, inside the worktree of task `01a141a7`. The manifest row for this path was
re-rendered by the write path each time, and the counts MOVED:

- after the first write (the branch line not yet present):
  `historical (0 standing / 0 record)`
- after ONE edit of the same file (the line present):
  `historical (0 standing / 2 record)`

The CLASS did not move — it stayed `historical` throughout, which is the other half
of the rule. Before the fix, the second number stayed at 0 and the author's own
`gopi tool doccheck` reported `stored-cell drift` against this very file.

