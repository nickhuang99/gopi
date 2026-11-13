# chat-policy-sample.md — the TRACKED, deterministic sample rule table

This file is a FIXTURE, not an operator's rule file, and the difference is the
whole reason it exists. The chat write boundary is driven by a ```chat-policy
fence, and until now the only fence any test could assert against was the one in
the repository root's `GOPI.md` — a file that is *untracked and gitignored*, so
the same commit produced SKIP (no file), FAIL (a host copy carrying a narrower
fence) or PASS (a workspace copy with the fence appended), depending on whose
machine ran the suite. A test whose verdict depends on an untracked file is not
a test of this code.

So this sample carries the shape the boundary is specified against, checked in
with the code that reads it:

- `allow write devdoc *` — the document corpus is written with no refusal and no
  approval request (the `*` is "any remaining words", i.e. any depth below the
  corpus root);
- `prompt write *` — every other target is a QUESTION, not a refusal;
- `forbidden` is deliberately ABSENT: the boundary's safety comes from the
  posture plus `prompt write *`, and a sample that compiled a list of banned
  trees would make the document corpus a compile-time decision rather than a
  rule the operator writes.

The prose above the fence is deliberate too: a rule table is read by a human as
much as by the machine, and the parser must never read prose as a rule.

```chat-policy
# the document corpus: written without asking
allow write devdoc *

# every other target: a question for the operator
prompt write *
```
