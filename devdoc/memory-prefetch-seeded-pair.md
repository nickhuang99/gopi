<!-- doc-class: normative -->
# The prefetched retrieval enters as the model's OWN `memory(action=search)` pair

Task `01a11fc08bb47373aeff98f317ab40af`. Supersedes the injection **shape**
described in `devdoc/memory-prefetch-executor.md`; it does not change what is
retrieved, when, or by whom.

## Symptom

`gopi chat`'s typing-time prefetch appended its rendered hit section to the
operator's own message:

```go
out.Message = out.Message + prefetchSectionSeparator + section   // the old shape
```

The hits arrived, and reading them worked. What could not happen was anything
**after** reading them.

## Root cause (5-why)

1. *Why was a useless injected hit re-sent every round?*
   Nothing ever removed it.
2. *Why did nothing remove it?*
   `whitelistContextSlim` removes an assistant `tool_use` block whose `#N`
   records were not voted usable. The injected section was not a `tool_use`
   block; it was text inside a **user** message.
3. *Why was it text in a user message?*
   The adoption seam's whole contract was "append the section to the message"
   (`chatPrefetchAdopt`), a shape inherited from when the only goal was making
   the hits *visible*.
4. *Why did that shape outlive its goal?*
   Visibility was never the hard half. The two mechanisms that give a hit
   consequences — the feedback vote and the whitelist slim — are both keyed on
   a `tool_use` **and its result**, and a message prefix is neither.
5. *Why does the model need a `tool_use` to vote at all?*
   `memory(action=feedback)` names a record by the `#N` printed on a hit line,
   and the discipline that the model should vote exists only for hits it
   received **as a search result**. The injection had invented a third channel —
   hits that arrived from nowhere the model could name — and no mechanism had
   been told about it.

So the defect is not "the injection was mis-rendered". It is that the injection
**created a channel**: a fourth way for memory text to enter a request, outside
the reach of the three that already existed. The fix is to stop creating one.

## The change

The retrieval is handed to the model in the shape the model's own search
produces:

```
assistant : tool_use   memory {"action":"search","query":<the lookup's own key>}
user      : tool_result <the rendered section, stamped [tool_use id: <that id>]>
```

Built by `Agent.seedPrefetchedRetrieval` (`internal/agent/chat_prefetch_seed.go`),
published into `Agent.PrefetchSeed` by `cli_chat.go`, consumed once on entry by
`ChatOps`.

Nothing new was built for the consequences, because nothing new needed to be:
the `#N` association the slim already parses, the stamp convention
`withTick`/`[tool_use id: …]` the executor's results already carry, and the
`AddAssistantMessageWithThinking` / `AddToolResult` helpers already exist.

### The design point that is counter-intuitive

**The seeded pair gets NO grace round.** Grace exists because a model composes
its tool calls *before* seeing their results — a search it just issued needs one
more round before it is votable. The seeded pair is the opposite: its result
landed **before** the request that produced the current response, so the model
was already reading it when it decided what to call. That response *is* its vote
opportunity — exactly the one round a self-issued search gets.

The first implementation added the seed to round 0's grace set ("give it the
same deadline"), reasoning that a pair dropped at the end of round 0 would be
retracted in the round it arrived. The test caught the reasoning as wrong:
a model that votes `useless` in round 0 saw its vote have no effect, because the
grace set had postponed the slim by a round. Grace would have handed the
prefetch a *longer* deadline than a self-issued search, which is the opposite of
the goal.

### The stated limit: the no-tools path

`gopi chat` without `--tools` advertises **no tools**. A `memory(action=search)`
pair there would be a `tool_use` the request never declared, and there is no tool
round in which to vote. That path keeps the text suffix, through a named helper
(`chatPrefetchTurnText`) that says why: on this path the retrieval is
**readable, not adjudicable**. Rule 15 — the limit is stated where it lives, not
hidden behind a shared code path that silently behaves differently.

## Verification (the evidence this change exists for)

A real `gopi chat --tools` on an allocated pty (a responder answered `ESC[6n`
and OSC 10/11 — without it the editor blocks and nothing is echoed), the line
typed with a pause so the typing-time lookup lands while the line is unsealed:

```
{"query":"rgb 0000 gopi memory prefetch 我要 注入 形状","hits":2,"ms":247,
 "outcome":"hits","adopted":"yes"}
```

and the same run's raw request log:

```
role: assistant
tool_use id: call_prefetch_5a56c291-… | name: memory
  | input: {"action":"search","query":"rgb 0000 gopi memory prefetch 我要 注入 形状"}
next role: user | tool_use_id: call_prefetch_5a56c291-…
result: "[tool_use id: call_prefetch_5a56c291-…]\n\n\nmemory-prefetch (…; 2 hits; …)
         \n[memory:#6425|verified|warm] …"
```

and what the model did with it — **votes on injected hits**, with no prompt
telling it their ids:

```
req1: seeded_blocks=1 feedback_calls=[]
req2: seeded_blocks=1 feedback_calls=[{'action':'feedback','id':6425,'rating':'useful',…}]
req3: seeded_blocks=1 feedback_calls=[…#6425 useful, {…'id':6580,'rating':'useful',…}]
```

`#6425` and `#6580` came **only** from the seeded section. Under the old shape
those ids never entered a request, and no `action=feedback` call could have named
them.

**Red proof** that the assertion is load-bearing: with the slim call replaced by
`_ = currentMemorySearchIDs(toolCalls)`,
`TestSeededPrefetchPairIsDroppedWhenItsRecordsWereNotVotedUseful` fails; restored,
it passes.

## The regression guard

`internal/agent/chat_prefetch_seed_test.go` drives the shipped `ChatOps` entry
point against a recording server and pins four things a shape-only test would
miss:

- the pair is on the wire, in order, with the query, the id prefix, the
  `[tool_use id: …]` stamp and the section verbatim;
- a `useless` vote in the tool round **drops** the pair from the next request;
- a `useful` vote **keeps** it (without this control the drop test would pass for
  a slim that dropped everything);
- the seed is consumed **once** — a turn whose caller published nothing carries
  no pair (a stored field would re-inject the same hits on every later turn).

## Not done (honest)

Automated coverage stops at `ChatOps`: no test drives the REPL/adoption seam
(`chatPrefetchAdoptTurn` → `PrefetchSeed` → the wire) through a real pty. The
repo already has that fixture (`chatOpenPtyPair`,
`cmd/agent/chat_surface_key_e2e_test.go`) and the manual run above is exactly the
case it would automate; the OFF path's byte-identity against `master` is
likewise asserted only at unit level.
