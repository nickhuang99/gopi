<!-- doc-class: historical -->
# Task Report: X11-input actuation vs CDP-driven Chrome against Google's anti-crawler wall

**Branch:** `feat/google-headed-x11-ab` (worktree `~/workspace/gopi-google-x11-ab`)
**Host:** remotebox VPS (Ubuntu 22.04, 957MB RAM / 1 vCPU) + residential-side host `rpi5` (192.168.7.15)
**Date:** 2026-09-16
**Evidence dir:** `devdoc/evidence/google-x11-ab/` (41 PNGs + 5 amendment PNGs, 15 logs + 3 amendment logs, harness scripts, raw curl HTML)

> ⚠️ **THIS REPORT CARRIES AN AMENDMENT.** Sections §0, §2 and §5–§6 below are
> **superseded in part**: the "datacenter IP is the binding constraint" framing is
> **retracted**, the arm B/C "soft wall" label is a **tab-title artefact**, and a
> **2×2 matrix with the residential cells honestly not run** replaces the 1-D verdict.
> Read the ***Amendment*** immediately below before quoting any conclusion from §5/§6.

---

## AMENDMENT — 2026-09-16, task `01a0a981` (retraction + 2×2 + wall type)

> **Scope:** amends §0 (framing), §2 (arm B "soft wall" label), §5 (verdict wording),
> §6.1 (the "blocked at IP reputation first" sentence). Measured in this amendment's own
> worktree (`~/workspace/gopi-google-x11-amend`, branch `amend/google-x11-actuation-retraction`,
> derived from this branch's tip) on the same remotebox VPS. Evidence: `devdoc/evidence/google-x11-ab/amend-*`.
> **No Go source was touched — this is a documentation correction. Build: not applicable.**
> The parent branch `feat/google-headed-x11-ab` is a live deliverable, so this correction is
> committed **on a branch derived from its tip** (Rule 23: never on a failed branch — this one did not fail).

### A1. RETRACTION — "datacenter IP ⇒ blocked" is withdrawn

**Retracted, explicitly.** This report's framing overstated the IP claim:

| Where | What it said | Status |
|---|---|---|
| §0 | "the **datacenter IP** is the binding constraint" | **RETRACTED** — false as stated |
| §5 | "The walled variable is the **egress IP reputation** — and the evidence for that is now stronger than before" | **DOWNGRADED** to "consistent with an address-level reputation cause; not established by this data" |
| §6.1 | "Google never got down to fingerprinting: it blocked at **IP reputation** first" | **REPLACED** — at this address the wall pre-empted the fingerprint question; that is a statement about *this address*, not about the mechanism |

**The corrected statement.** Google does **not** blanket-block datacenter IPs. Datacenter
addresses reach Google constantly and legitimately (Gmail, Cloud APIs, `google.com` from
DO/AWS/GCP hosts); the block is **not a property of the address class**. What the evidence
supports is the weaker, still-untested claim: *risk control appears to attach to specific
addresses (and their `/64`), accumulating reputation per address* — and this VPS's address
is treated as suspect. "Datacenter ⇒ blocked" must not be written again.

**Downweight the "the challenge page cited our IP" evidence.** The `/sorry` page prints the
source address **by design** — it is a "why did this happen / About this page" page whose
purpose is to report the request's address. Its citation is therefore **not evidence of an
IP-level trigger**, and this amendment adds a first-hand demonstration of that: at the *same*
address, 131 s apart, with *different* actuation methods (X11-only vs CDP), the two pages cite
the *same* IP with the same wording — i.e. the citation is page boilerplate, invariant to the
variable under test. Prior notes (memory #845/#847) leaned on it; they should not.

### A2. The 2×2 — (actuation) × (egress class), every cell filled or honestly not run

**Arm B1 = X11-instruction actuation, no CDP at all, datacenter IPv4** — the critical cell.
Fresh run this amendment, 2026-09-16T13:08:58Z → 13:09Z; harness `amend-arm-b1.sh`
(`cmdline_has_remote_debugging_port=0`, `our_listening_cdp_ports=[0]` — no debugger present).

| | **Datacenter egress** (AS14061 DigitalOcean, Santa Clara) | **Residential egress** |
|---|---|---|
| **X11/XTEST actuation, no CDP** | **WALL — HARD `/sorry`**, `137.184.190.229` (B1, 13:09:25Z, pixel-verified: `google.com/sorry/index?continue=…`, "About this page", "Our systems have detected unusual traffic…", reCAPTCHA "I'm not a robot", `IP address: 137.184.190.229`). IPv6 twin: parent arm B → same HARD page citing `2604:a880:4:1d0:0:3:565a:b000`. | **NOT RUN** — no residential egress exists to run it on: (a) the home ISP **blocks Google at all** (HP laptop `120.36.178.221`: `curl https://www.google.com/` → `http=000 err=Connection timed out after 4002 ms`; TCP 443 → timeout; control `bing` → `http=302`); (b) rpi5 on the same home network: `google_v4 http=000 (12002 ms)`, `google_v6 http=000`, `tcp443 BLOCKED`, `bing 302`; (c) the only tunnel available (wg3) **is not residential** — exit `134.199.237.56` resolves to `AS14061 DigitalOcean, Santa Clara`, the same ASN/city as the VPS. A residential arm must be brought up **on** a residential host, not through this one. **Re-measured first-hand this amendment** (2026-09-16T13:19Z, `amend-residential-cell-blocker.log`): peerbox's own egress is `120.36.178.221` / `AS4134 CHINANET BACKBONE` (consumer broadband pool) with `google.com` never completing while the control `bing.com` → `http=302`; rpi5 shares the *same* NAT address, `google_v4 http=000`, `google_v6 http=000`, `tcp443 BLOCKED`, `bing 302`. The control succeeding is what makes this a **measured blocker, not a broken link**. |
| **CDP actuation** | **WALL — HARD `/sorry`**, same address `137.184.190.229` (B2, 13:11:32Z, 131 s after B1, same window: probe JSON `"sorry": 1, "enablejs": 0`, `body_head` "unusual traffic… IP address: 137.184.190.229"; PNG pixel-verified independently). IPv6 twin: production gateway arm A → `/sorry`. | **NOT RUN** — same three measured reasons as above. |

**What the 2×2 does and does not say.** Both datacenter rows wall, both with the *hard* wall and
both citing the same address ⇒ **at this egress, actuation method produced no observable
difference** (parent §3 already showed the one-off X11 SERP was a transient-window artefact; the
12/12-reload control stands). But the datacenter column is **saturated**: an address already
walled gives both actuation methods the same ceiling, so this matrix **cannot measure the
actuation effect** — it can only show that actuation does not lift a wall that is already up.
The discriminating cells are the residential row, and they were **not run**. Therefore:

> **Nick's challenge is not refuted by this data.** "Actuation signature mattered" is neither
> confirmed nor falsified here — it is **untested where it could show**. What the data *does*
> refute is the parent's stronger claim that actuation was tested and failed as the binding
> constraint: at a walled address, that test has no power.

### A3. Wall TYPE — `enablejs` soft vs `/sorry` hard, by arm

Two distinct walls exist, and they are distinguished by instrument class, **not** by actuation arm:

| Wall type | Signature (measured) | Which arms reach it |
|---|---|---|
| **`enablejs` SOFT** | HTTP 200, ~92 KB single-line JS-redirect shell, `<title>Google Search`, body "Please click here if you are not redirected", `enablejs` present, **0 `/sorry` markers, no reCAPTCHA, no results** | **cookie-free `curl` only** — bare *and* full-header-suite, IPv4 *and* IPv6 (four rows, A4 below) |
| **`/sorry` HARD** | `google.com/sorry/index?continue=…`, "About this page", "Our systems have detected unusual traffic from your computer network…", reCAPTCHA "I'm not a robot", cited `IP address:` + `Time:` | **every browser arm** — X11-only (B1 v4, parent B v6) and CDP (B2 v4, parent A v6) |

**Answer to "does the wall type differ between arms?" — NO.** Every browser arm, both actuation
methods, both IP families, renders the **same hard `/sorry`** page. There is **no `enablejs`-vs-`/sorry`
difference between the X11 and CDP arms**; the difference runs between *instruments* (curl can only
ever reach the soft shell — it has no JS engine and no browser state), which is why the browser is
the only honest instrument and why a curl result must never be quoted as an arm's verdict.

**The parent's "arm B/C = enablejs soft wall" label was a tab-title artefact — corrected here.**
On the real `/sorry` page the tab title *is* the long `/search?…` URL (the page keeps the requested
URL as its title). The parent read that "bare URL title" as the soft-wall signature, and read the
momentary `Google Search` title as the soft wall itself. The B1 title log shows what actually
happens: `Google` → **`Google Search` for exactly one sample (t+1 s, the loading transient)** →
stuck `https://www.google.com/search?q=…` for 17 s and unchanged after F5 — while the *pixels* of that
same page are the HARD `/sorry` (B1 verdict PNG). Counts in the parent's own logs: `titles-ARMB-IPV6.log`
has **0** `Google Search - Google Chrome` lines and 21 raw-URL lines; `titles-ARMC-IPV4.log` 21 / 0.

⇒ **Rule for future reading:** a tab title can tell you "results vs not-results"; it **cannot**
tell you *which wall*. Discriminate the wall by pixels (address bar + `sorry/index` + reCAPTCHA),
never by title.

### A4. The curl header-suite experiment — a WEAK data point, labelled with its limits

Re-measured first-hand this amendment (family-pinned `curl -4` / `curl -6`, raw bytes counted from
the saved files, not eyeballed):

| row | bytes | `/sorry` | `enablejs` | `<h3` | title |
|---|---|---|---|---|---|
| bare v4 | 92077 | 0 | 2 | 0 | `Google Search` |
| bare v6 | 92260 | 0 | 2 | 0 | `Google Search` |
| **full header suite v4** | 92799 | 0 | 2 | 0 | `Google Search` |
| **full header suite v6** | 93016 | 0 | 2 | 0 | `Google Search` |

header suite = Chrome 151 UA, `Accept`, `Accept-Language`, `Sec-Fetch-Dest/Mode/Site/User`,
`Upgrade-Insecure-Requests`, `sec-ch-ua`, `sec-ch-ua-mobile`, `sec-ch-ua-platform`.
Parent-run twin rows for comparison: `curl4-r1-IPV4.html` 92039 bytes, `curl4-r1-IPV6.html` 92190 bytes,
both `sorry=0 enablejs=1 h3=0` (marker counted by *line*, hence 1 vs 2 — see the reading artefacts below).

**Result: headers change nothing** — +0.8 % bytes, the same `enablejs` soft shell, zero results.
**And that result is WEAK — here is exactly what it cannot vary:** it varies **HTTP headers only**.
It cannot vary the **TLS/JA3 fingerprint**, cannot **execute JavaScript**, has no **input dynamics**
(keystroke timing, mouse path), and carries **no cookies or browser state**. A negative header-only
result therefore does **not** refute "automation-signature detection"; it only refutes "the header
set alone is the trigger". **The wall this test observed was the `enablejs` SOFT wall — not a hard
`/sorry`** — i.e. the softest surface Google offers, which is a different layer from what the browser
hits; quoting it as evidence about the browser path is a category error.

**Reading artefacts recorded so this weak test is not mis-read as strong evidence** (both were
artefacts of *reading*, not of the probe):
1. a first pass grepped `enablejs` **without family pinning** and labelled rows "v4" that had in fact
   been fetched over IPv6 (the measured `remote_ip` was IPv6 while the label said v4) — a comparison of
   two IPv6 fetches. Fixed by `curl -4` / `curl -6`.
2. the 92 KB shell is **one line**, so dumping the body made every marker line unreadable and a 0-match
   grep looked like evidence. Count markers with `grep -o … | wc -l` / `grep -c` against the *saved
   file*; never eyeball a dumped body.

### A5. What would falsify / confirm the IP-reputation hypothesis

The honest state: **open** — the hypothesis is *consistent* with the data but not established by it,
because the discriminating cells were not run. Stated falsifiably:

**Would CONFIRM** (address/`/64`-level reputation is the cause):
1. **Same instrument, clean egress:** the *identical* harness (same Chrome build, same profile, same
   actuation code, same query) run from a genuinely different egress whose address has no history —
   a residential ISP or a different provider's ASN — returns a **SERP**. Same code, different address,
   different outcome ⇒ the address, not the client, is the difference.
2. **Same ASN, fresh `/64`:** a *new* address in AS14061 with no prior traffic to Google returns a SERP
   while this `137.184.190.229` keeps walling ⇒ risk is per-address//64, not per-provider — this is the
   test that separates "DigitalOcean is blocked" (already retracted) from "this address is burned".
3. **Neutral datacenter control:** an unrelated long-lived datacenter address that never crawls Google
   returns a SERP without a challenge ⇒ "datacenter" is not the class that triggers the wall.

**Would FALSIFY** (reputation is *not* the explanation, or not the whole one):
1. **Clean egress still walls with the same instrument** ⇒ something the *client* carries (profile/state,
   TLS/HTTP2 fingerprint, headless-ish signals, query pattern) or an upstream network path is doing the
   work, and the address is incidental.
2. **The address recovers with no change of address** (same IP, no client change, wall lifts after a
   quiet period) ⇒ the trigger is *rate/behavioural over time on this address*, not a stored reputation
   verdict about the address as such.
3. **Residential × X11 walls while residential × CDP passes (or vice versa)** ⇒ **actuation signature is
   a real factor** and Nick's challenge was right; the datacenter 2×2 could not have seen it because both
   its rows were saturated.

**Therefore the question is left genuinely OPEN, in one specific place:** the residential row, where a
clean address makes the actuation effect measurable. Until a residential egress exists, no report should
claim either "IP reputation is the binding constraint" (the original overclaim) or "actuation demonstrably
does not matter" (this amendment's ceiling effect).

### A6. Amendment evidence index

| file (`devdoc/evidence/google-x11-ab/`) | what it is |
|---|---|
| `amend-arm-b1.sh` | B1 harness — X11-only, `--remote-debugging-port` absent, prod paused + `trap` restore |
| `amend-b1-run.log` / `amend-b1-titles.log` | B1 preflight (xdotool verified present), curl rows, title series |
| `amend-b1-ipv4-0{1..4}-*.png` | B1 landing / typed / verdict / after-F5 pixels (verdict = HARD `/sorry`, cited IP) |
| `amend-b2-run.log`, `amend-b2-cdp-ipv4-probe.json`, `amend-b2-cdp-ipv4-verdict.png` | B2 CDP at the same IP, 131 s later: `"sorry":1, "enablejs":0` + pixels |
| `amend-curl-{bare,headers}-v{4,6}-pinned.html` | the four family-pinned raw curl responses |
| `amend-curl-weak-test.log` | the weak-test record: byte counts, markers, limits, reading artefacts |
| `amend-residential-cell-blocker.log` | first-hand re-measurement of WHY the residential row is "not run" (peerbox + rpi5: same AS4134 consumer address, Google `http=000`/`TCP443_BLOCKED`, Bing control `302`) |

Production was paused for both arms and restored by the harness `trap`
(`restore_service=active` in both run logs); no CDP artifact in B1 (`cmdline_has_remote_debugging_port=0`,
no listener); one Chrome at a time (957 MB host).

---

## 0. The question

> **SUPERSEDED — read A1/A2/A5 first.** This section restates the parent's framing; the
> "datacenter IP is the binding constraint" attribution it opens with is **RETRACTED** (A1),
> and the question it defines is **open**, not answered, because the discriminating cell is
> not run (A2/A5).

Prior evidence (memory #845/#847, task `google-search-ipv6`) said the **datacenter IP**
is the binding constraint: `/sorry` on IPv4, `enablejs` shell on IPv6, `/sorry` citing
the VPS's own IP. The **untested variable was actuation method** — does driving a
*genuinely headed* Chrome through **X11 input only** (XTEST / xdotool, no CDP
`Runtime.evaluate` / no `Page.navigate`) change Google's outcome, **at constant IP**?

## 1. Arms

| Arm | Actuation | Egress pinned to | Own display / profile | CDP port |
|---|---|---|---|---|
| **A** | production CDP gateway (`:9232` → Chrome `:9222`) | IPv6 (prod `--host-resolver-rules`) | `:99` / `/tmp/chromeprofile` | yes |
| **A4** (extra cell) | CDP, standalone Chrome | **IPv4** via `--host-resolver-rules` | own `:97` | yes (`:9223`) |
| **B** | **X11/XTEST only** (xdotool type/keys/click) | IPv6 (same egress as A) | own `:98`, own user-data-dir | **none** |
| **C** | **X11/XTEST only** | **IPv4** | own `:98`, own user-data-dir | **none** |
| **R** | CDP (rpi5's proven driver) | wg3 tunnel exit | rpi5 `:99` | yes |

**Constant-IP discipline:** the family is pinned by `--host-resolver-rules`
(`MAP www.google.com <literal google IP>`), so arm B and arm C differ **only in egress
family**, and B differs from A **only in actuation method**.

## 2. Result table (all executed on the real hosts, not a literature summary)

| Arm | Actuation | IP family | **Page title / URL signal** | Cited IP on the page | Verdict |
|---|---|---|---|---|---|
| **A** (prod gateway) | CDP | IPv6 | gateway JSON `{"status":"empty","message":"google served reCAPTCHA /sorry challenge for this IP"}`; `/health` → `{"status":"ok","challenged":true}` | **2604:a880:4:1d0:0:3:565a:b000** (VPS IPv6) | **WALL** |
| **A4** (14:51:45Z → 2/2) | CDP | IPv4 | `landed_href=…/sorry/index?continue=…`, title = the URL | **137.184.190.229** (VPS IPv4) | **WALL** |
| **B** | **X11 only** | IPv6 | title progression `Google` → `Google Search` → **raw URL** ⇒ ~~enablejs soft-wall, then `/sorry`~~ **see Amendment A3: the raw-URL title is the HARD `/sorry` page's own title, not a soft wall** | **2604:a880:4:1d0:0:3:565a:b000** | **WALL (hard `/sorry`)** |
| **C** | **X11 only** | IPv4 | r1: `golang context deadline - Google Search` (SERP, one-off); r2: raw URL; **3/3 wall in the paired alternation** | 137.184.190.229 (paired CDP cell) | **WALL (3/3)** |
| **A4↔B/C alt.** | CDP vs X11, order-alternated, same IP | IPv4 | `abalt-result.tsv`: X11 3/3 `WALL-title-is-url`, CDP 3/3 `WALL-sorry` (11:54–11:56Z) | same IP both sides | **both WALL** |
| **Intermittency control** | none (one Chrome, 12× F5) | IPv4 | `SUMMARY IPV4: serp=0 wall=12 of 12 reloads` | — | wall is **stable**, r1 was a transient window |
| **R** (wg3) | CDP | tunnel exit | `exit_v4=134.199.237.56`, `curl http=200`, `enablejs=1`; browser reading empty (see §4) | — | **INCONCLUSIVE / not a residential IP** |

### The 2×2 (actuation × IP family) — measured INSIDE the datacenter only

> Superseded for framing by **Amendment A2**, whose 2×2 is (actuation × *egress class*) and carries
> the residential row as "not run". This table is kept as the raw datacenter-internal result: all four
> cells wall — which, per A2, is a **ceiling effect** and cannot measure the actuation variable.

```
             IPv6                        IPv4
X11-only     WALL (B: enablejs→/sorry)   WALL 3/3 (C, alternation)
CDP          WALL (A: gateway "empty")   WALL 2/2 (A4: /sorry cites 137.184.190.229)
```

**All four cells wall.** Actuation method changes nothing; IP family changes nothing
*within the datacenter*.

### Raw evidence (verbatim, this task)

* **Arm B / IPv6 challenge body** (`ARMB-IPV6-03-verdict.png`, vision transcript):
  > heading: `About this page`
  > `Our systems have detected unusual traffic from your computer network. This page checks to see if it's really you sending the requests, and not a robot. Why did this happen?`
  > **`IP address: 2604:a880:4:1d0:0:3:565a:b000`**
  > reCAPTCHA: `I'm not a robot` / `reCAPTCHA`
* **Arm A / IPv4 CDP** (`armav4-r1.log`, `armav4-r2w.log`):
  `"body_head": "…unusual traffic… IP address: 137.184.190.229  Time: 2026-09-16T11:51:45Z"`, `"sorry": 1`
* **Arm C / IPv4 X11 SERP (the one-off)** (`ARMC-IPV4-03-verdict.png`): title
  `golang context deadline`, results `Context Deadlines in Go` (Medium), `context package` (Go Packages).
* **XTEST genuinely actuating** — Ctrl+L + typed query changed pixels
  (`*-02-typed.png` vs `*-01-landing.png`), F5 re-rendered, and a click on the reCAPTCHA
  checkbox **escalated to the image grid**: vision transcript of `armc-04-after-click.png`:
  > `Select all images with a fire hydrant` / `Click verify once there are none left.`

  i.e. the challenge was *received* and *interacted with* through X11 — it was not bypassed.
* **Curl control** (`curl4-r1-*.html` etc.): cookie-free curl gets the `enablejs` shell with
  **no** `/sorry` marker (`sorry=0 enablejs=1 h3=0`) — a cookie-free curl never reaches the
  `/sorry` layer the real browser hits. This is why the browser is the only honest instrument.

## 3. Root cause of the earlier "X11 wins on IPv4" reading

`r1` (11:41Z) produced a real SERP in arm C. The follow-ups killed that explanation:

1. **Paired alternation** (`ab-alternate.sh` → `abalt-result.tsv`), 3 rounds, order flipped
   each round, constant IPv4: **X11 3/3 wall, CDP 3/3 wall**. No actuation advantage.
2. **Intermittency control** (`ab-intermittency.sh` → `abint-reload-IPV4.log`): one Chrome,
   constant IP, **12 sequential reloads with no actuation change** → **12/12 wall**.

⇒ The wall is stable per IP at the minute scale; `r1` fell inside a **transient window**.
That is a sampling artefact, not a technique. (5-why: symptom "X11 got a SERP" → mechanism
"Google's challenge is time-varying per IP" → norm "control for time by alternating arms
and by reloading one browser" → the control showed the artefact.)

## 4. Residential-egress arm — honest "not reached as designed"

> **Amendment A2 pointer:** this section is also the reason the **residential row of the 2×2 is
> "not run"** rather than "failed" — the arm was attempted on real hosts and the *reason* it could
> not run (no residential egress exists here; the home ISP blocks Google; wg3 is DigitalOcean) is
> recorded below and carried into the amendment's table.

**Reached:** the wg3 tunnel came up on rpi5 with a **real handshake**
(`latest handshake: 5 seconds ago`) and a **different exit IP: `134.199.237.56`**.
Curl through that exit: `http=200 remote_ip=142.251.151.119 bytes=91567`, `enablejs=1`.

**Not reached as designed — and the reason is itself the finding.** `134.199.237.56`
resolves to:

```
"org": "AS14061 DigitalOcean, LLC", "city": "Santa Clara", "region": "California"
```

…which is **the same ASN and city as the remotebox VPS itself**
(`137.184.190.229` → AS14061 DigitalOcean, Santa Clara; the wg3 endpoint `203.0.113.10`
too). **wg3 is not a residential egress — it is a second datacenter IP from the same
provider.** So "residential" was never actually tested, and this arm cannot settle the
IP-reputation question.

The genuinely residential path was probed and **cannot reach Google at all**:

* **HP laptop's own home egress** (`120.36.178.221`): `curl https://www.google.com/`
  → `http=000 err=Connection timed out after 4002 ms`; TCP 443 to `142.251.156.119`
  → **BLOCKED/timeout**, while `https://www.bing.com/` → `http=302`. The home ISP blocks
  Google.
* **rpi5 native egress (tunnel down)**, same home network: `google_v4 http=000
  (Connection timed out after 12002 ms)`, `google_v6 http=000`, `tcp443 BLOCKED`,
  control `bing http=302`. Same block.

Browser reading on the wg3 exit was empty in all three attempts (`title`/`landed_href`/
`body_head` all `""`). Two real causes were found and fixed along the way — Xvfb not
accepting connections before chromium started (`ui/gfx/x/connection.cc: X connection
error received`) and a hand-rolled WS frame decoder returning `null` — after which the
probe ran (`xsock_ok:true, cdp_ok:true`) but the CDP `Runtime.evaluate` round-trip still
returned empty. **I am not claiming a browser verdict for this arm.** The curl-level
signal (`enablejs`, no `/sorry`) is recorded; the browser-level verdict is not.

## 5. Verdict — **NO, X11-input actuation does NOT defeat the block at constant IP**

> **Scope of this verdict (Amendment A2):** it holds **at a walled datacenter address**, where both
> arms hit the same ceiling; it does **not** quantify how much actuation matters on an unwalled
> address, because that row (residential egress) was never run. Read it together with A2/A5.

Falsifiable statement: *X11-only actuation, at the same egress IP, yields a SERP where the
CDP-driven path is walled.*

**Refuted.** At constant IPv4, X11 was walled **3/3** in the order-alternated paired run
while CDP was walled 3/3 in the same window; the intermittency control showed **12/12**
walls with no actuation change; at constant IPv6 both arms cited the same VPS address.
The single SERP seen under X11 was reproduced by *time*, not by *technique*.

⇒ **The actuation method is not the binding constraint.** ~~The walled variable is the
**egress IP reputation** — and the evidence for that is now stronger than before, because
*every* datacenter cell measured (VPS IPv4, VPS IPv6, and the second DigitalOcean exit
`134.199.237.56`) walls regardless of actuation.~~ **AMENDED (see Amendment A1/A2/A5):**
what the cells show is that *at this (suspect) address* actuation made no difference. That is a
**ceiling effect**, not a measurement of actuation — the address was already walled, so neither arm
had anything to lift. "The walled variable is the egress IP reputation" is **downgraded** from
"established" to "consistent with the data at this address and NOT established by it"; the
residential row that could measure it was **not run** (A2). "Datacenter ⇒ blocked" is **retracted**:
Google does not blanket-block datacenter addresses, and the `/sorry` page citing our IP is page
boilerplate, not proof of an IP-level trigger (A1).

**Is X11-input actuation worth pursuing? NO** — not as a block-defeat mechanism. The
technique works perfectly and is worth keeping as *capability*, not as *evasion*:
confirmed by `Select all images with a fire hydrant` escalating after a real X11 click.

## 6. Recommendation

1. **Residual value of the headed-X11 path: low for evasion, high for fidelity.** The
   production CDP gateway is not losing anything to X11 acting. X11's value is
   *undetectability of the driver surface* (no `--remote-debugging-port`, no
   `Runtime.evaluate` in the page's world) — which ~~**did not matter** here, because
   Google never got down to fingerprinting: it blocked at IP reputation first~~ **did not
   manifest at this address, because the address was already walled before either actuation
   method ran (Amendment A2, ceiling effect). Whether it matters on an unwalled address is
   exactly the untested residential cell — see A5.**
2. **The wg3 route as-is buys nothing.** Its exit is DigitalOcean AS14061 Santa Clara —
   the same provider/city as the blocked VPS. Attaching it to the remotebox path would
   replace one datacenter IP with another from the same ASN; the prior evidence predicts
   the same wall, and the measured `enablejs` shell is consistent with that.
3. **What attaching wg3 would take (and what it would *not* fix):** on the remotebox,
   bring up the tunnel, verify the handshake, point DNS at it and *clear the eth0 search
   domain* (memory #733), then pin the browser to it. Mechanically simple — and
   **pointless until the exit is an actual residential ISP** (the current endpoint is
   DigitalOcean). The real requirement is a **residential exit**; wg3 today is not one.
4. **A residential egress is currently unreachable from home at all:** both the HP laptop
   and rpi5 on the home network get TCP-443 timeouts to Google (Bing fine). So even a
   residential exit *reached through the home link* faces an ISP-level block before the
   VPN matters. Any residential arm must come up **on** the residential host, not through
   it.
5. **Do not spend more agent time on actuation *at this already-walled address*.** That is the
   only sense in which the recommendation stands: the datacenter column is saturated, so no
   actuation experiment there has power. It is **not** a statement that the input method is
   irrelevant — that question is open (A5), and this recommendation is itself **superseded** the
   moment a residential/mobile egress exists: the 2×2's discriminating row is then runnable, and
   the single most informative cheap test is *residential × X11* vs *residential × CDP*. Nothing in
   this amendment licenses "actuation demonstrably does not matter".

## 7. Reusable recipe notes

* **The briefing's "xdotool absent" was stale — xdotool and ImageMagick `import` ARE
  installed on the remotebox.** Verify tool presence rather than trusting the briefing.
* **No WM is needed.** `xdotool windowactivate` fails (no `_NET_ACTIVE_WINDOW` without a
  WM). Use **`xdotool windowraise` + `xdotool windowfocus`** (XSetInputFocus) — that is
  the WM-free focus technique that worked.
* **`xdotool key ctrl+l` then type is the reliable actuation path.** Clicking the page
  body at guessed coordinates is not: the homepage search box is not focused on load.
* **The tab title is a machine-readable verdict signal.** Results ⇒ title contains the
  query (`golang context deadline - Google Search`); walled ⇒ the title collapses to the
  bare URL. Reading the title in a loop is a cheap, CDP-free verdict probe.
* **Never use CDP for the X11 arm.** CDP allows one ws debug connection per target — a
  second debugger starves the production gateway. Arm B ran with **no**
  `--remote-debugging-port` at all.
* **One Chrome at a time on the 957MB remotebox.** Two concurrent Chromes triggered a
  global OOM that killed production `google-cdp.service` (dmesg 11:30:56,
  `task_memcg=/system.slice/google-cdp.service`). The harness therefore **pauses**
  production and the `trap` always restarts it — racing is *less* safe than pausing.
* **Remote `pkill -f <pat>` self-kills** over ssh (the pattern matches the ssh command
  line). Put `pkill` in a script file and run the file.
* **A bare `sleep` is not a wait.** Wait for `/tmp/.X11-unix/X99` **and** for
  `/json/version` to answer before driving chromium, or you get
  `ui/gfx/x/connection.cc: X connection error received`.
* **Cookie-free curl is not a substitute for the browser.** It returns the `enablejs`
  shell with no `/sorry` markers — a different (earlier) layer than the real browser hits.
* **`--host-resolver-rules` pins the egress family** without touching DNS — the cheapest
  way to hold IP constant while varying one other thing.
* **Classify the exit before trusting it**: `curl https://ipinfo.io/<ip>/json` showed the
  "residential" wg3 exit was really AS14061 DigitalOcean. *Measure the egress identity;
  do not infer it from the name of the tunnel.*

## 8. Production health (post-interference)

```
systemctl is-active google-cdp.service        → active
curl :9232/health                             → {"status":"ok","challenged":true}
ss -ltnp                                      → 127.0.0.1:9222 (chrome), 127.0.0.1:9232 (python3)
gateway /search (live)                        → {"status":"empty", …, "message":"…reCAPTCHA /sorry challenge for this IP (cached)", "elapsed_ms":0}
```

The temporary gateway patch used for the Arm-A forced probe was **reverted**
(`git status` shows the file clean) and preserved as evidence at
`devdoc/evidence/google-x11-ab/arm-a-force-probe.patch`.

---

## Completed
- All A/B arms executed on the real hosts; 2×2 (actuation × IP family) fully measured.
- Paired order-alternated run + 12-reload intermittency control refuting the one-off SERP.
- Arm A live re-probe with the production headed Chrome showing the cited-IP `/sorry` page.
- Residential/wg3 arm attempted on the real residential-side host; exit identity measured
  and classified; home-network Google reachability measured (blocked).
- Verdict + recommendation + reusable recipe (this document, §5–§7).

## Not Done (honest)
- **Amendment A2: the residential row of the 2×2 is NOT RUN** — no residential egress exists to run it
  on (home ISP blocks Google outright: `http=000` TCP-443 timeout on both HP and rpi5; wg3's exit is
  AS14061 DigitalOcean, same ASN/city as the VPS). So the actuation question is **open**, not settled.
  This amendment **re-measured the blocker first-hand** (`amend-residential-cell-blocker.log`, 13:19Z):
  peerbox's egress `120.36.178.221` / `AS4134 CHINANET` cannot complete a Google request while the Bing
  control answers `302`; rpi5 behind the same NAT shows `google_v4 http=000`, `google_v6 http=000`,
  `tcp443 BLOCKED`, `bing 302` — a measured blocker, not an assumption inherited from the parent.
- **No browser-level verdict on the wg3 exit** (CDP round-trip returned empty in 3 attempts).
  Curl-level signal recorded only.
- **No true residential-ISP arm**: wg3's exit is DigitalOcean AS14061, and the home ISP
  cannot reach Google at all. ~~The IP-reputation hypothesis is therefore *strengthened but
  not settled*~~ **Amendment A1/A5: not even "strengthened" — the residential cells that could
  test it were not run, and the "/sorry page cited our IP" evidence is page boilerplate (A1).**
- **Amendment A1: the parent's "datacenter IP = blocked" framing is retracted** and the
  "arm B/C reached the enablejs SOFT wall" label is corrected to the HARD `/sorry` (A3) — both were
  overclaims/artefacts that survived into this document's earlier revision.
- Arm A's forced-probe patch required to bypass the 15-min `/sorry` cache; upstream
  production behaviour was restored (patch preserved, not committed to the production file).

## Verification
- Build: **not applicable** — no Go source changed in this task. The parent's one code edit
  (production gateway probe patch) was reverted; the amendment is a documentation correction only
  (`git show --stat` lists `.md` + evidence files, zero `.go`).
- Evidence: `devdoc/evidence/google-x11-ab/` — 46 PNGs, 18 logs, `abalt-result.tsv`,
  `abint-reload-IPV4.log` (`wall=12 of 12`), `armav4-r{1,2w}.log` (`sorry:1`,
  `IP address: 137.184.190.229`), `titles-*.log`, `rpi-wg3-arm{3,5,6}.log`, raw curl HTML,
  plus the amendment set `amend-*` (A6 index).
- **Amendment cell B1** (X11-only, no CDP, datacenter IPv4, 2026-09-16T13:08:58Z): verdict PNG
  pixel-verified → `google.com/sorry/index?continue=…`, `About this page`, "unusual traffic",
  reCAPTCHA "I'm not a robot", `IP address: 137.184.190.229`; `cmdline_has_remote_debugging_port=0`,
  `our_listening_cdp_ports=[0]` (no CDP artefact). **Cell B2** (CDP, same IP, 13:11:32Z):
  `"sorry": 1, "enablejs": 0`, same cited address, PNG verified independently.
- **Amendment curl rows** re-counted first-hand from the saved files (bytes / `sorry` / `enablejs` /
  `<h3`): 92077/0/2/0, 92260/0/2/0, 92799/0/2/0, 93016/0/2/0 — labelled a WEAK test (A4).
- Vision readings (verbatim, §2 + A2/A3): Arm B cited `2604:a880:4:1d0:0:3:565a:b000`; Arm C SERP
  titles; reCAPTCHA grid `Select all images with a fire hydrant`; B1/B2 `/sorry` transcripts above.
- Production: `google-cdp.service` active, `/health` ok (above); paused + `trap`-restored for both
  amendment arms (`restore_service=active` in `amend-b1-run.log`, `amend-b2-run.log`).
- Commit: see final message of the task (branch + recovery command named there).
