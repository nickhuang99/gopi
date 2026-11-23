# Example: token counting & cache observability

> Every command below was run on a real session. Outputs are verbatim, not mock-ups.
> Source session: `01a20a33-b6db-7cd2-8231-b2787f919587` (a real worker run, 61 requests).

Most agent harnesses cannot tell you what a run cost, or prove their token count is
right. gopi counts tokens with its **own** tokenizer and shows the cache hit rate as
a measured column — in every session, with real money.

## 1. Count tokens with gopi's own tokenizer

Not an estimate — the DeepSeek-V4.1 BPE vocabulary the bill is computed on:

```console
$ gopi tool tokens --file README.md
README.md: 5457 tokens (19912 bytes, 459 lines, deepseek-v4.1-bpe)

$ gopi tool tokens --prompt 'gopi is built around DeepSeek'
"gopi is built around DeepSeek": 8 tokens (29 bytes, 1 lines, deepseek-v4.1-bpe)
```

## 2. Prove our count equals the server's

`--session` compares every request's count against the number the API actually
billed. Every `diff` is `+0`:

```console
$ gopi tool tokens --session 01a20a33-b6db-7cd2-8231-b2787f919587
line     msgs   tools   ours     server    diff
2        1      17      66376    66376     +0
116      3      17      73487    73487     +0
344      5      17      74458    74458     +0
...
requests: 61, with a server number: 61, exactly equal: 61
```

## 3. Server-side prompt cache, with real money

The 98.43% below is **DeepSeek's** server-side prompt-cache hit rate, read from the
API's own usage fields — it is NOT a gopi-side optimization. gopi's contribution is to
surface that number honestly, per session, priced against the official rate page.

```console
$ gopi session stats 01a20a33-b6db-7cd2-8231-b2787f919587 --cost
│ requests       61
│ prompt tokens  5,367,112   (avg 87,985/request)
│ cache          98.43% hit (5.28M) / 84K miss
│ total cost      0.3601 元 @ CNY
│   cache hit     0.1057 元  (5.28M tokens)
│   input miss    0.0842 元  (84K tokens)
│   output        0.1703 元  (43K tokens)
│ money split      29.3% /  23.4% /  47.3%
```

Only 84K of 5.28M prompt tokens were misses. The number is the server's own
usage data — gopi just makes it visible and prices it.

## 4. Tool-call ranking

```console
$ gopi session stats 01a20a33-b6db-7cd2-8231-b2787f919587 --tools
    47   73.4%  bash
     4    6.2%  memory
     4    6.2%  read
     3    4.7%  edit
     3    4.7%  task
     1    1.6%  context_trim
     1    1.6%  write
```

## Animated GIF

`token-and-cache.gif` — 4 frames, 412x492 px (inside the 500 px ceiling), 48 KB,
built entirely with `gopi tool text2img` + `gopi tool gif` (pure Go, no ffmpeg).

**Reproduce the GIF:** see `frames/` (four ASCII frame files) and the `make.sh`
recipe. Use a **monospace** font for `--font` — see the note in `make.sh`.
