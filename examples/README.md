# gopi examples

Each directory is a self-contained, runnable demonstration of one gopi capability.
Every command output is real — captured from an actual run, never a mock-up.
Animated GIFs are built with gopi's own pure-Go verbs (`text2img` + `gif`), no ffmpeg.

## Collaboration

- `examples/session-collaboration/` — two sessions, one repository, a mailbox keyed
  on a session id. 38 messages, 38 receipts, and the corrections that went both
  ways. (deepseek-flash + hy4-flash)
- `examples/agent-collaboration/` — why two agents beat one cleverer agent: a
  `delivered` receipt is written by the READER, and the three times they overturned
  each other (twice it was this author who was wrong). (deepseek-flash)

## Workflow engine

- `examples/workflow-dispatch/` — the work is a ledger row, not a conversation:
  `task submit` → worker claims/runs → PA supervises via events. (deepseek-flash)
- `examples/parallel-projects/` — different rows declare different repositories, and
  every row gets its own CLONE. No merge daemon: each worker merges itself.
  (deepseek-flash)
- `examples/self-development/` — gopi's own defects are filed on gopi's own ledger and
  fixed by a worker in a clone of gopi's source tree. Bootstrap, closed.
  (deepseek-flash)

## Supervision

- `examples/worker-false-completion/` — `completed` is a claim a run makes about
  itself, not a measurement. Three shapes of "done" that are not done (false
  positive, false negative, zero output), the four commands a supervisor runs
  instead of reading the report, and the part that is still not a gate.
  (deepseek-flash)

## Memory

- `examples/memory-provenance/` — memory is an auditable ledger, not a hand-written
  file. Every hit carries its origin (`sess=… round=…`), its confidence, and the votes
  it received; two contradictory lessons are kept side by side instead of one
  overwriting the other, and an injected memory that nobody votes for is pruned.
  Measured: 14 of 162 injections ever received a vote. (deepseek-flash)

## Observability

- `examples/token-and-cache/` — our own BPE tokenizer (ours == server, 61/61 diff +0),
  server-side prompt-cache hit rate read from the API's usage fields, real cost.
- `examples/session-replay/` — every run is a recording: turn/round structure,
  byte-exact `screen-replay`, `--verify` reports the first differing frame.
- `examples/resume-after-failure/` — the resume ladder: status exit codes,
  LEVEL 1 `worker --resume` vs LEVEL 2 `task clone`, and why `resumable` != resume now.
- `examples/voice-input/` — speech to text, four engines with different trade-offs:
  `recording` (any container, batch), `streaming`/`sauc` (WAV or bare PCM, low latency),
  `local` (FunASR, nothing leaves the machine). The container trap is documented:
  handing a FLAC to the streaming channel is refused, and the refusal names the fix.
  (hy4-flash)

## Reproducing the GIFs

All three constraints below are measured, not stylistic:

- **Use a monospace `--font`.** `text2img`'s default face is proportional, so equal
  character count does NOT mean equal pixel width — measured at `--size 13`, ten
  characters: `iiiiiiiiii` → 56 px vs `MMMMMMMMMM` → 131 px. With a monospace face
  both are 103 px.
- **Verify every frame's pixel size before composing.** A GIF has one logical screen
  and `gopi tool gif` refuses mismatched frames. Measure with
  `python3 -c "import struct;d=open(p,'rb').read(33);print(struct.unpack('>II',d[16:24]))"`.
- **Keep the width ≤ 500 px** for a phone messaging view. `gopi tool gif` *warns* above
  the ceiling but still writes the file, so a too-wide GIF looks like success —
  check with `od -j6 -N4 out.gif`.

See `docs/quickstart.md` for the full recipe.
