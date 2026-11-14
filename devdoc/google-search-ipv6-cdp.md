<!-- doc-class: historical -->
# Task Report: Google search via search-bot — Xvfb + IPv6-forced Chrome CDP (Epic)

## Goal (Epic)
Replace the low-quality google results (all `news.google.com` RSS wrappers) with
real Google SERP, using search-bot (DO VPS, outside GFW) as jumper + virtual
display to mimic a human browser, on the premise that **IPv6 bypasses
google.com anti-robot**.

## What was implemented
1. **CDP SERP gateway restored & hardened on search-bot (137.184.190.229)** —
   the designed primary path was dead (`Connection refused`; nothing had
   restarted it since Aug-24). Now:
   - `Xvfb :99` (virtual display) + headed `google-chrome 151` (CDP `:9222`),
     **IPv6-forced**: `--host-resolver-rules="MAP www.google.com
     [2001:4860:482a:7700::],MAP google.com [2001:4860:482a:7700::]"` (IPv6
     literal MUST be bracketed — without brackets Chrome logs
     `host_mapping_rules.cc:146 Failed parsing rule` and silently drops it,
     then connects over IPv4 and hits /sorry). Plus `--disable-quic` (verifiable
     via `ss`).
   - `google_cdp_server.py` gateway `:9232` (`GET /search?q=&n=` →
     `{status,results:[{title,url,snippet}],count}`).
   - **Durability**: systemd `google-cdp.service` (`Restart=always`) runs
     `google-cdp-watchdog.sh` — health-checks the gateway every 15s, restarts
     the stack after 3 failures. Verified `active` + `enabled`. Root cause of
     the original outage (gateway died, nobody restarted) is fixed.
2. **google_search.py updated** (repo + deployed `~/.gopi/plugins/websearch/scripts/`):
   - CDP ssh timeout 24s→40s (page load 3.5s + 10×1.5s polling + handshake).
   - Header docstring now documents the 2026-08 empirical wall evidence.
   - News-RSS snippet extraction improved (text after the `<a>` anchor,
     "- Source - 摘要"), drop the useless source-name-only snippet when a
     summary exists.
   - Gateway-side **/sorry challenge cache** (15 min): once the wall is detected
     the CDP attempt short-circuits in ~16ms instead of a full ~10s navigation —
     google method total latency 25-28s → **~6s**.
   - Gateway `/health` endpoint for the watchdog (no Chrome navigation on the
     15s health poll).
3. **Repo**: `plugins/websearch/scripts/google_search.py` (was deployed-only —
   audit B5 gap) + `plugins/websearch/remotebox/` (gateway + 4 scripts/units).

## Critical empirical findings (2026-08-25, all verified on search-bot)
| attempt | result |
|---|---|
| curl google.com/search over IPv4 (any UA/params) | 302 → hard **/sorry** reCAPTCHA or enablejs |
| curl over IPv6 (udm=14 / gbv=1 / cookies / enablejs retry flow) | HTTP 200 but always the **enablejs soft wall** — title "Google Search", 92KB shell, NO results/JSON anywhere (verified by tag-stripping the body) |
| headed Chrome over IPv4 | **/sorry** reCAPTCHA (Runtime.evaluate hangs) |
| headed Chrome over IPv6 (fresh profile, warm-up: homepage→pause→search) | **/sorry** reCAPTCHA — page explicitly cites the VPS IPv6 `2604:a880:4:1d0:0:3:565a:b000` |
| reCAPTCHA checkbox click (CDP, same-origin iframe) | no pass (aria-checked stays false) |
| news.google.com wrapper unwrap via curl -L | redirect chain ends at news.google.com **viewer page** (JS-rendered), no real URL in static HTML |
| news.google.com CBMi payload static decode | payload is now **high-entropy (encrypted/signed)** — the old base64/protobuf decode trick no longer yields a URL |
| SearXNG public instances (4 tried) | 403 / 429 / DNS-timeout / non-JSON — not a reliable tier |

**Conclusion: the Epic's "IPv6 bypasses google.com anti-robot" premise does not
hold on this DO VPS.** IPv6 only changes *which* wall appears (curl: enablejs
instead of the IPv4 hard block; Chrome: still /sorry, IP-based). Real Google
SERP is unobtainable from this VPS by any static or browser method. The CDP+IPv6
architecture is correctly implemented, live, and durable — it auto-activates the
moment Google's risk engine changes or a residential exit is added to search-bot
(per past RPi5/wg3 pattern, memory #189). Until then, the google method honestly
returns News-RSS (title + real-ish snippet + working wrapper link).

## Not Done
- Real-SERP retrieval from search-bot (blocked by Google IP reputation — out of
  our control without a residential exit/VPN).
- RPi5 residential-exit gateway (proven in memory #189 but against the Epic's
  "search-bot" mandate; documented as the recommended next step).
- bing/deepseek/merge_results.py still not mirrored into the repo (audit B5, out
  of scope).

## Verification
- Gateway smoke: `GET :9232/search?q=…` → responds (status empty + reCAPTCHA
  message) — **alive** (was Connection refused before).
- `python3 google_search.py "golang web framework 2026" --count 5` →
  `status ok`, 5 news-rss results, message chain shows CDP attempt then fallback.
- Deployed copy == repo copy (same file, `cp` after `py_compile`).
- Build/tests: Python scripts only; Go side untouched (plugin contract intact).
  `go test ./internal/tools/` PASS (fake-script based, unaffected).
- Commit: see git log on branch `feat/google-ipv6-cdp`.

## Search-bot ops cheat-sheet
- Restart stack manually: `ssh remotebox 'bash /tmp/restart_stack.sh'`
- Watchdog/systemd: `systemctl status google-cdp` (active+enabled)
- Verify Chrome uses IPv6: `ss -tn | grep 2001:4860:482a:7700` during a query
- Chrome flag parse check: `grep "Failed parsing" /tmp/chromium.log`
