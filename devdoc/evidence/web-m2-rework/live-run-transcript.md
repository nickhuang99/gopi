<!-- doc-class: historical -->
# Live `gopi web` run — raw evidence (feat/web-m2-rework)

Server: `GOPI_WEB_TURN_TIMEOUT_SEC=60 ./bin/gopi web --addr 127.0.0.1:8093 --tools`
(binary built from this branch; banner: `gopi web: listening` / `open: http://127.0.0.1:8093`).

## B9 — /api/menu (candidates resolved from the SHARED command table / the file tree)

    $ curl -s "http://127.0.0.1:8093/api/menu?trigger=/&q="
    {"trigger":"/","query":"","source":"slash","candidates":["code-search","log","memory","new","task","text2img"],"total":6}

    $ curl -s "http://127.0.0.1:8093/api/menu?trigger=%2F&q=ta"
    {"trigger":"/","query":"ta","source":"slash","candidates":["task"],"total":1}

    $ curl -s "http://127.0.0.1:8093/api/menu?trigger=@&q="
    {"trigger":"@","query":"","source":"file","candidates":[".baseline/gates.txt", … 8 entries …],"total":2000}

    $ curl -s "http://127.0.0.1:8093/api/menu?trigger=X&q=a"
    {"trigger":"X","query":"","source":"","candidates":[],"total":0}

## B1 — reset (POST /clear)

    $ curl -s .../api/transcript | python3 -c '…len(messages)…'
    messages: 14
    $ curl -s -X POST .../api/chat -d '{"message":"/clear"}'
    event: command
    data: {"cleared":14,"command":"/clear","output":"conversation reset — the transcript and per-turn state were cleared"}
    $ curl -s .../api/transcript
    {"messages":[]}

The page's `RESET_NOTICE` is that same string (asserted verbatim by
`TestWebM2B_ResetAckAndQueuedNoticeAreSingleSourced`).

## B2 — a second concurrent turn is told it is waiting

    $ (curl … -d '{"message":"…ALPHA…"}' &) ; sleep 1 ; curl … -d '{"message":"Reply with exactly: BETA"}'
    event: queued
    data: {"notice":"waiting for the in-flight turn to finish…","placement":"queued"}
    event: delta
    data: {"text":"B"}
    …

## B5 — tool rows replay with their name + digests

    $ curl -s -X POST .../api/chat -d '{"message":"Use the read tool on /etc/hostname, then reply with just its content."}'
    (wire) 2 × `event: tool` + deltas + `event: done`

    $ curl -s .../api/transcript
    assistant | content: "" | tools: [{'id':'call_00_ET_…','name':'read','call':'/etc/hostname'}]
    tool      | content: "[t+17s] [tool_use id: call_00_ET_…]…" | tools: [{'id':'call_00_ET_…','name':'read','result':'peerbox'}]

## Roadmap §5 invariant 1 — ONE session across every turn (incl. concurrent ones)

    $ python3 … /home/user/.gopi/.gopi/sessions/bad3868c-…/session.jsonl
    tally: {'session_start': 1, 'user': 4, 'assistant': 9, 'request': 7, 'tool_result': 1}
    raw log bytes: 323613    (one session dir, one JSONL, one raw log)

## Render evidence

`gopi shot` could not be used: the only chromium on this host is a snap build
without a DevTools port (`/snap/bin/chromium --headless --dump-dom` dumps core;
`gopi shot --self --addr 127.0.0.1:8093` reports "chrome did not report a
DevTools port within 15s"). The page was therefore exercised through
`cli_web_page_harness.js`, which evaluates the REAL `<script>` extracted from
`cmd/agent/cli_web.go` against a DOM shim and observes the rendered nodes
(95/95 checks) — the DOM-level truth, without a browser.
