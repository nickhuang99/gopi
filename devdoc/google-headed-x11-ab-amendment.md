<!-- doc-class: historical -->
# Task Report: Google search-wall — 2×2 amendment (actuation × egress) + retraction

**Branch:** `feat/google-x11-ab-amendment` (worktree `~/workspace/gopi-google-ab2`)
**Amends:** task `01a0a4dd` / its devdoc `devdoc/google-headed-x11-actuation-ab.md` (parent experiment)
**Hosts:** remotebox VPS (`137.184.190.229`, AS14061 DigitalOcean Santa Clara — *datacenter*),
HP laptop (`120.36.178.221`, AS4134 CHINANET Fuzhou CN — *residential*)
**Date:** 2026-09-16 (all times UTC)
**Evidence dir:** `devdoc/evidence/google-x11-ab/` (+ raw files fetched from `remotebox:/tmp/amend-ab1`, `/tmp/amend2`)

**Provenance declaration.** The screenshots and logs in `amend-*` are **verbatim copies** of the
artifacts produced on the remotebox by this task's measurement scripts (`amend-arm-b1.sh`,
`amend-curl-weak-test.log` run); the `amend2-*` set is a verbatim copy of the paired-alternation
run (`amend2-paired-ab.sh`) harvested from `remotebox:/tmp/amend2`. Copies are byte-identical to
the originals (verified by `sha256sum` on both sides). No artifact is a re-creation, a mock, or a
rendering of an expected result. Raw telemetry is never overwritten by interpretation: where my
reading differs from a harness's automated label, **both are reported** (§4).

---

## 0. What is being amended

The parent devdoc concluded (§5):

> "The walled variable is the **egress IP reputation** — and the evidence for that is now stronger
> than before, because *every* datacenter cell measured … walls regardless of actuation."

and recommended (§6.1) "Google never got down to fingerprinting: it blocked at IP reputation
first."

Two things are wrong with taking that as settled, and both are corrected here:

1. **A blanket reading ("the datacenter address is the binding constraint") is overclaimed.** It
   generalizes from a small set of addresses on one provider to a class of addresses, and it
   discards the counter-measurement that the same address serves ordinary Google pages on a
   different endpoint (§2).
2. **The actuation hypothesis was not actually tested to the standard claimed.** The curl
   "full-browser-header" run varies HTTP headers only, so its null result cannot refute an
   automation-signature hypothesis that lives in TLS/JS/input dynamics (§3). The 2×2 below adds
   the arm that *does* test the actuation variable — a real Chrome driven purely by X11 input
   with no CDP surface at all (§5).

Both hypotheses — **IP reputation** and **actuation signature** — remain **OPEN** after this work.
Nothing here forecloses either.

---

## 1. The 2×2 matrix (actuation × egress)

| Cell | Actuation | Egress | Status | Wall TYPE (pixel-verified) | Raw evidence |
|---|---|---|---|---|---|
| **A1** | CDP (`--remote-debugging-port`, `Runtime.evaluate`) | datacenter IPv4 (137.184.190.229) | **RUN** | **`/sorry` HARD** (reCAPTCHA "I'm not a robot") | `amend-b2-cdp-ipv4-probe.json` (`sorry=1 enablejs=0 body_chars=351`, `IP address: 137.184.190.229`, `13:11:32Z`); `amend-b2-cdp-ipv4-verdict.png`; `amend2-result.tsv` rows `A1-CDP-r1/r2`; `amend2-cdp-r1-verdict.png` |
| **A2** | CDP | residential (HP, AS4134 CHINANET) | **NOT RUN** | — | **Reason (measured, not assumed): no residential egress in this fleet can reach Google at all.** Raw TCP 443 to the real Google front-ends never completes — see §6 |
| **B1** | **X11/XTEST only, NO CDP** | datacenter IPv4 (137.184.190.229) | **RUN** | **`/sorry` HARD** (reCAPTCHA "I'm not a robot") | `amend-b1-run.log` (`cmdline_has_remote_debugging_port=0`, `our_listening_cdp_ports=[0]`, `cdp_ports_all=[]`); `amend-b1-ipv4-03-verdict.png`, `amend-b1-ipv4-04-after-f5.png`; `amend2-result.tsv` rows `B1-X11-r1/r2`; `amend2-x11-r1-03-verdict.png`, `amend2-x11-r2-03-verdict.png` |
| **B2** | **X11/XTEST only, NO CDP** | residential (HP, AS4134 CHINANET) | **NOT RUN** | — | **Same measured reason as A2** — the residential egress cannot open a TCP connection to Google (§6) |

**Reading the matrix.** Every cell that could be run is a **wall**, and every runnable browser cell
lands on the **same wall TYPE** (`/sorry` hard) regardless of actuation. The actuation variable —
the one the parent could not isolate — is therefore **null at this egress**, which is *consistent
with* IP reputation being upstream of any fingerprinting, and is **not** a refutation of the
actuation hypothesis (see §7, the confounding).

---

## 2. Retraction: the datacenter-address overclaim

**Retracted.** The earlier framing — that the wall follows from *being* on a datacenter address,
i.e. that datacenter egress is categorically excluded — is **withdrawn**. What the evidence
supports is narrower and different in kind: **risk control targets specific addresses / /64 blocks
carrying accumulated bad reputation, per endpoint.** Three independent grounds:

### 2.1 The same address serves ordinary Google pages (the direct counter-measurement)

Measured on the remotebox at `137.184.190.229`, same minute, same address, as the walled runs:

```
egress_is=137.184.190.229
homepage   http=200 bytes=84110 sorry=0 enablejs=0 title=[Google]
search     http=200 bytes=92134 sorry=0 enablejs=1 title=[Google Search]
```

A categorically excluded address cannot return `HTTP 200`, 84 KB, title `Google`.
The decision is **endpoint-specific and reputation-shaped**, not address-class-shaped.

### 2.2 The trigger is per-endpoint, not address-wide

The automation-shaped surface (`/search`) is challenged while the ordinary surface (`/`) is served,
from one address, in one minute. That is the signature of a risk *score* consulted on the
automation-relevant path — not of an address-level deny rule.

### 2.3 The challenge page's own IP line is WEAK evidence — downweighted

`/sorry` prints `IP address: 137.184.190.229`. This is **boilerplate**: the page always renders the
requester's address, whatever triggered it. It is *not* evidence that the trigger was IP-level, and
it must not be read as one. Any argument of the form "the page names our IP, therefore the IP is
the trigger" is circular and is rejected here.

### 2.4 What survives of the parent's observation

The parent's *observations* stand (its datacenter cells did wall, repeatedly and stably). What is
retracted is the **generalization to a class**. The correct statement is the one the parent's own
data supports: **these particular addresses, on this provider, accumulated enough reputation that
Google challenges the automation path from them** — and a different address, or the same address
later, is not excluded by any rule we have measured.

---

## 3. The curl header test is a WEAK test — a data point, not a refutation

The "full browser header suite" curl run is a **weak instrument** for the automation-signature
hypothesis, and is recorded as such.

**Raw data point (two independent runs, all rows family-pinned with `curl -4` / `curl -6`):**

| row | bytes | sorry | enablejs | title |
|---|---|---|---|---|
| `curl-bare-v4` | 92077 / 92195 | 0 | 1 | `Google Search` |
| `curl-bare-v6` | 92260 / 92263 | 0 | 1 | `Google Search` |
| `curl-headers-v4` | 92799 / 93101 | 0 | 1 | `Google Search` |
| `curl-headers-v6` | 93016 / 93052 | 0 | 1 | `Google Search` |

(the two byte figures per row are the two runs; the archived run is `amend-curl-weak-test.log`,
the re-measured run is `amend-b1-run.log` §phase 1. The briefing's "~91.8–92.2 KB" matches the
`bare` rows; the `headers` rows are ~92.8–93.1 KB — reported as measured.)

**Outcome unchanged.** Adding a Chrome 151 user-agent, `Accept`, `Accept-Language`,
`Sec-Fetch-*`, `Upgrade-Insecure-Requests` and the `sec-ch-ua*` client hints changed **nothing**:
every row is the same soft `enablejs` shell, no `/sorry`, no results. The header suite is a
machine-readable no-op.

**Why it is weak — it varies exactly one surface.** curl with headers set can vary **HTTP headers
only**. It cannot vary:

* the **TLS/JA3 fingerprint** (curl's ClientHello ≠ Chrome's; a detector reading JA3 sees a script),
* **JavaScript execution** (no JS engine at all — the `enablejs` shell is Google explicitly asking
  for a JS-capable client),
* **input dynamics** (no keystroke timing, no pointer path, no scroll/cadence),
* **browser state** (no cookies, no profile, no storage).

⇒ Its null result is evidence about *one* surface (headers), not about the hypothesis. The
automation-signature hypothesis predicts a difference on surfaces this instrument cannot touch;
a test that cannot touch them cannot refute it. **Recorded as a data point and nothing more.**

Note the layer asymmetry this exposes: cookie-free curl hits the **soft `enablejs` shell** while a
real browser hits the **hard `/sorry`** from the *same address*. The two instruments are not
measuring the same layer — which the parent had already flagged and which §4 confirms by pixels.

### 3.1 Raw-verify hazards recorded (so the next reader does not repeat them)

* A first pass grepped `enablejs` **without family pinning** and labelled rows `v4` that curl had in
  fact fetched over IPv6 (the measured `remote_ip` was IPv6 while the label said v4) — a comparison
  of two IPv6 fetches. Fixed with explicit `curl -4` / `curl -6`.
* Feeding the ~92 KB single-line shell to a terminal makes every marker unreadable (one giant
  line), so a 0-match `grep` looks like evidence. Read markers as **counts out of the saved file**
  (`grep -c`), never by eyeballing a dumped body.

Both were artifacts of *reading*, not of the probe.

---

## 4. Wall TYPE per cell — read from PIXELS, not from a title heuristic

Every wall-type determination below was made by rendering the screenshot and reading it, not by
trusting an automated classifier.

| cell | arm | screenshot | pixel verdict | address bar |
|---|---|---|---|---|
| A1 | CDP × datacenter | `amend-b2-cdp-ipv4-verdict.png` | **`/sorry` HARD** | `google.com/sorry/index?continue=…` |
| A1 | CDP × datacenter | `amend2-cdp-r1-verdict.png` | **`/sorry` HARD** | `https://www.google.com/sorry/index?continue=…` |
| B1 | X11-no-CDP × datacenter | `amend-b1-ipv4-03-verdict.png` | **`/sorry` HARD** | `google.com/sorry/index?continue=…` |
| B1 | X11-no-CDP × datacenter | `amend-b1-ipv4-04-after-f5.png` (post-F5) | **`/sorry` HARD** | `google.com/sorry/index?continue=…` |
| B1 | X11-no-CDP × datacenter | `amend2-x11-r1-03-verdict.png` | **`/sorry` HARD** | `google.com/sorry/index?continue=…` |
| B1 | X11-no-CDP × datacenter | `amend2-x11-r2-03-verdict.png` | **`/sorry` HARD** | `google.com/sorry/index?continue=…` |
| — | curl (no JS) | `amend-curl-*.html` (markers) | **`enablejs` SOFT** | `HTTP 200`, title `Google Search`, no results |

### 4.1 A harness label corrected by pixels (and why it matters)

`amend2-result.tsv` records the X11 rows as `WALL-title-is-url(soft-enablejs-look)`. **The pixels
say otherwise: both X11 verdict screenshots are the hard `/sorry` reCAPTCHA page.** The harness
classified from the *tab title*, which collapses to the URL while the `/sorry` navigation is in
flight — a timing artifact of the classifier, not a property of the wall.

The screenshots are nevertheless genuine X11-arm navigations, and a URL detail proves it: the X11
rows carry `&oq=golang+context+deadline` (the X11 path types into the box, which populates `oq`),
while the CDP rows carry `&hl=en&gl=us` and no `oq` (CDP navigated a constructed URL). Two
different navigation paths, two different URL shapes, both landing on `/sorry`.

**Lesson (reusable):** a page's *type* must be read from rendered pixels. A title/URL heuristic
cannot distinguish "the wall is soft" from "the hard wall has not painted its title yet", and here
it produced exactly that wrong label.

### 4.2 What the soft wall was, and where it lives

The `enablejs` soft wall and the `/sorry` hard wall are **different layers**, not two strengths of
one verdict:
* `enablejs` is what a **JS-incapable client** (cookie-free curl) gets at `/search` — Google
  declining to serve a JS-dependent app to a client that cannot run it.
* `/sorry` is what a **full browser** gets from this address on the same endpoint — an
  intervention decision, not a capability mismatch.

At the datacenter egress, **every browser cell measured got the hard wall, under both actuations.**

---

## 5. Arm B purity: no CDP surface, and `navigator.webdriver` false by non-CDP means

The constraint "Arm B must carry NO CDP artifacts" is met and evidenced, not asserted:

* Launch line has **no** `--remote-debugging-port` (`amend-b1-run.log`, §phase 3):
  `cmdline_has_remote_debugging_port=0`, `our_listening_cdp_ports=[0]`, `cdp_ports_all=[]`.
* Actuation is **XTEST only** — `xdotool key ctrl+l`, typed text, `Return`, `F5`, and pointer
  clicks; focus via `windowraise` + `windowfocus` (no WM present). Results are read by screenshot
  + vision.
* The paired harness additionally renders a `file://` probe page whose **pixels** carry the value,
  so the proof instrument is not the variable under test:

  | artifact | rendered value |
  |---|---|
  | `amend-b-wd-noCDP.png` (no debug port) | `navigator.webdriver = false` |
  | `amend-a-wd-cdp.png` (debug port, control) | `navigator.webdriver = false` |

  Both are `sha256 51360fbe…205d`, i.e. the same rendering from both arms — so `webdriver=false` is
  established for Arm B **without CDP**, and the CDP arm was not observably distinguishable by this
  probe anyway (consistent with `--disable-blink-features=AutomationControlled` being applied to
  both).

---

## 6. Cells A2 and B2 (residential) — NOT RUN, with the measured reason

The residential row could not be executed because **no residential egress available to this fleet
can reach Google at all** — the failure is upstream of any Google decision, so a browser arm there
would measure the ISP's block, not Google's risk control. Measured on the HP laptop (the
residential host), `amend3-residential-transport.sh` → `amend3-residential-transport.log`:

**The egress is genuinely residential** (measured, not inferred):

```
"ip": "120.36.178.221", "city": "Fuzhou", "region": "Fujian", "country": "CN",
"org": "AS4134 CHINANET BACKBONE", "hostname": "…broad.xm.fj.dynamic.163data.com.cn"
```

**Three distinct failure modes were separated** (so that DNS poisoning is not mistaken for a
network block, and neither for a Google verdict):

1. **DNS poisoning: present.** `www.google.com` resolves to `157.240.7.20` — that is a Facebook/
   Meta address, not a Google front-end (while `google.com` → `142.251.33.206`). A name-based test
   alone would have measured a wrong host.
2. **Network-level block: present, and independent of DNS.** With the poisoned resolver bypassed
   (`curl --resolve www.google.com:443:<real google IP>`), and again with raw TCP and with
   `openssl s_client -servername www.google.com`:

   ```
   resolve 142.251.156.119: code=000 bytes=0 connect=0.000000      (exit 28)
   resolve 142.251.153.119: code=000 bytes=0 connect=0.000000      (exit 28)
   resolve 142.251.151.119: code=000 bytes=0 connect=0.000000      (exit 28)
   tcp443 142.251.156.119: BLOCKED/TIMEOUT(exit=124)
   tcp443 142.251.153.119: BLOCKED/TIMEOUT(exit=124)
   openssl s_client -connect 142.251.156.119:443 -servername www.google.com: handshake_failed
   tcp443 202.89.233.100 (bing, CONTROL): OPEN
   bing-v4 typed (CONTROL): code=302 ip=202.89.233.100
   ```

   The connection never opens; the control (Bing) opens and returns `302` in the same run. The
   block sits on the path, not in Google's decisioning.
3. **Application-level verdict: unreachable** — cannot be measured at all from here.

**Also excluded: the wg3 "residential" exit.** The parent measured its identity
(`134.199.237.56` → AS14061 DigitalOcean Santa Clara — the same provider, city and ASN as the VPS).
It is a second datacenter address, so **no residential cell exists behind it** either.

⇒ A2 and B2 are marked **not run**, reason: **no residential egress in this fleet is capable of
opening a TCP connection to Google** (AS4134 CHINANET blocks it at the network layer, DNS poisoned
on top), and the only alternative path (wg3) is a datacenter address. Running either cell would
require a **real residential or mobile egress that is not behind this ISP's block** — a resource
this environment does not have.

---

## 7. What would falsify / confirm the IP-reputation hypothesis

**The hypothesis (stated so it can fail):** *Google's decision at the `/search` endpoint is driven
primarily by a per-address (or per-`/64`) reputation score accumulated from prior traffic, and is
mostly insensitive to how the browser is actuated.*

**It is CONFIRMED by:**

* **C1 — same address, different endpoints.** `/` served (`200`, no wall markers) while `/search`
  is challenged, same address, same minute (§2.1). Reputation is consulted per endpoint.
* **C2 — actuation invariance at constant address.** X11-no-CDP and CDP both hard-wall at
  `137.184.190.229`, alternating in time, 2 rounds each (§1, §4).
* **C3 — stability per address.** 12/12 reloads of one browser, no actuation change, all walled
  (parent intermittency control) — the wall tracks the address, not the request.

**It is FALSIFIED by:**

* **F1 — a FRESH address behaves differently.** Rent or move to a new IP, query `/search`
  **immediately, before any accumulated history**, and get SERPs. If a virgin address is walled on
  first contact while a warm one is also walled, reputation accumulation is not the mechanism.
  *(This is the single most decisive and cheapest test, and it is currently untested.)*
* **F2 — per-address granularity inside one block.** Two addresses in the **same /64** that
  disagree (one serves SERPs, the other walls, same minute) falsifies the /64 framing and pins the
  granularity to the single address. Conversely, the *whole* block walling while a *neighbouring*
  block serves would pin it to /64.
* **F3 — the decisive residential cross.** **X11-no-CDP from a residential egress.** If
  residential + X11 → SERP while residential + CDP → `/sorry`, then actuation is a real
  independent variable and this hypothesis is too narrow. If both SERP, reputation dominates. If
  both wall, either something else binds or that ISP's whole range is warm. **Currently
  unrunnable here (§6)** — this is the experiment to run when a real residential/mobile exit
  exists.
* **F4 — a reputed address recovering under an honest client.** Keep a warm datacenter address idle,
  then query with a fully honest browser and observe **gradual** recovery (SERP → soft → hard) or
  its absence. Recovery curves are reputation's signature; a constant wall is not.

**Confound that must be carried honestly (why C2 does not close the actuation question):** both
browser arms ran from **one** warm datacenter address. If reputation gates the request before any
fingerprint is consulted, actuation *cannot* express itself there — so C2 is a **confounded null**,
not a refutation. The actuation hypothesis survives C2 untouched. Distinguishing the two requires
**F1 or F3**, i.e. a change of address, not a change of technique.

---

## 8. Where the evidence points (both hypotheses stay OPEN)

* **IP reputation:** supported (C1–C3), **not settled** — never tested against a fresh address (F1)
  and never tested off-datacenter (F3). Its strongest single piece of support is C1, which is a
  *within-minute, within-address* contrast and therefore free of the "all datacenter cells walled"
  generalization that is being retracted.
* **Actuation signature:** **not refuted**, and not tested to its own prediction either. The curl
  header run cannot reach the relevant surfaces (§3); the two browser arms that could were both
  confounded by a shared warm address (§7). Its proper test is the residential cross (F3) or a
  fresh address with X11 actuation (F1/F3 combined).
* The observed **asymmetry (hard `/sorry` for browsers, soft `enablejs` for a JS-incapable client at
  the *same* address)** is itself a hint of **nuanced, non-flat decisioning**: Google is not
  applying one static verdict per address, it is scoring the request *and* the client together.

**No operational conclusion is drawn here about "which variable to change"** — the parent's
recommendation to abandon actuation is **not** endorsed as settled, precisely because its deciding
test was confounded.

---

## 9. Mechanics learned (reusable)

* **A WM is required for `windowactivate`/`_NET_ACTIVE_WINDOW`, but a WM-free path exists:**
  `xdotool windowraise` + `xdotool windowfocus` (XSetInputFocus). That is what drove Arm B.
* **`xdotool` IS installed on the remotebox** (`/usr/bin/xdotool`) — the briefing that claimed
  otherwise was stale. Verify tool presence at run time.
* **a11y-first over blind Tab:** address the target window by name (`xdotool search --name`), raise
  + focus it, then act (`ctrl+l` + type). Blind Tab order depends on browser chrome state and is
  not reproducible.
* **Never inline `pkill -f <pattern>` over ssh** — the pattern matches the ssh command line and
  kills the session. Put it in a script file on the host.
* **One Chrome at a time** on this 957 MB / 1 vCPU host (a second concurrent Chrome caused a global
  OOM that killed the production unit). Pause `google-cdp.service` and restore it on **every** exit
  path via `trap`. The production service was left `active` after every run in this task.
* **`--host-resolver-rules=MAP` pins the egress family** without touching DNS — the cheapest way to
  hold the address constant while varying one other thing. (Chrome renders an "unsupported
  command-line flag" infobar for it; that is UI, after the response.)
* **Read wall type from pixels.** A title/URL heuristic mislabelled the soft/hard wall in this very
  task (§4.1).
* **Do not trust a briefing's environment claims** — measure: `command -v`, `curl --max-time`,
  `ipinfo.io/<ip>/json` for egress identity, and family-pinned probes.

---

## Completed
- **The 2×2 is complete**: A1 and B1 measured from raw evidence with pixel-verified wall types;
  A2 and B2 explicitly marked *not run* with the measured reason (residential transport block).
- **Retraction** of the datacenter-address overclaim, grounded in C1 (same address serves `/`
  while `/search` is challenged) and the weak-evidence rule for the `/sorry` IP line.
- **Weak-test statement** for the curl header run, with raw data (outcome unchanged across the
  header suite; `enablejs` soft shell; title `Google Search`) and an explicit list of the surfaces
  it cannot vary.
- **Arm B purity proven**: no debug port on the cmdline, no CDP listener, and
  `navigator.webdriver = false` read from rendered pixels of a `file://` probe — by a non-CDP means.
- **Paired same-address alternation** (X11 vs CDP, 2 rounds, order-alternated) harvested, with a
  **harness label corrected by pixel reading**.
- **Falsification paragraph** for the IP-reputation hypothesis (C1–C3 / F1–F4) plus the confound
  that keeps the actuation hypothesis open.
- Mechanics/recipe notes for the X11-driven, WM-free, CDP-free arm.

## Not Done (honest)
- **Cells A2 and B2 are NOT MEASURED.** No residential egress in this fleet can open a TCP
  connection to Google (AS4134 CHINANET, network-layer block; DNS also poisoned), and the wg3 exit
  is a DigitalOcean address, not residential. The residential row therefore reports a *feasibility
  finding*, not a Google verdict.
- **The IP-reputation hypothesis is NOT settled.** F1 (fresh address) and F3 (residential cross) —
  the two tests that could settle it — were not runnable here.
- **The soft `enablejs` wall was not observed from a browser arm at the datacenter egress** — every
  browser cell got the hard `/sorry`. The soft wall is reported as an HTTP-layer (JS-incapable
  client) observation only; no claim is made that a browser ever saw it in these runs.
- No production code changed; the build check below is a liveness check, not a feature build.
- The parent devdoc itself was **not** edited: this document amends it by reference (the parent is
  raw evidence for its own run and is left intact). If the project wants a single canonical
  document, the parent's §5–§6 should be superseded by a pointer to this file — deliberately left
  as a reviewer decision rather than silently rewriting another task's report.

## Verification
- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — see the run recorded with the
  commit (no Go source touched by this task).
- **Evidence (raw, verbatim, on this branch):**
  * `amend-b1-run.log` — Arm B1 launch, egress, and the no-CDP assertions
    (`cmdline_has_remote_debugging_port=0`, `our_listening_cdp_ports=[0]`).
  * `amend-b1-ipv4-03-verdict.png`, `amend-b1-ipv4-04-after-f5.png` — Arm B1 landing on `/sorry`.
  * `amend-b2-cdp-ipv4-probe.json` + `amend-b2-cdp-ipv4-verdict.png` — Arm A1 (`sorry=1`,
    `IP address: 137.184.190.229`).
  * `amend-curl-weak-test.log`, `amend-curl-{bare,headers}-{v4,v6}-pinned.html` — the weak test.
  * `amend2-result.tsv`, `amend2-run.log`, `amend2-{cdp,x11}-r{1,2}-*.png` — the paired
    same-address alternation (provenance: `remotebox:/tmp/amend2`).
  * `amend-b-wd-noCDP.png`, `amend-a-wd-cdp.png` (`sha256 51360fbe…205d` both) — `webdriver=false`.
  * `amend3-residential-transport.sh` + `amend3-residential-transport.log` — the residential
    transport block (raw TCP 443 timeout; Bing control `OPEN`/`302`; DNS poisoning shown).
- **Pixel verification:** every wall-type claim in §4 was produced by rendering the screenshot and
  reading it (`vision_analyze`), not from the harness's own label — see §4.1 for the label that was
  contradicted and corrected.
- **Production health after interference:** `google-cdp.service` = `active`; `:9222` listening;
  production was restored by the harness `trap` on every run in this task.
- **Commit:** see the final message of this task.

## Rule 6d — deliverable declaration
- **Branch:** `feat/google-x11-ab-amendment` (worktree `~/workspace/gopi-google-ab2`).
- **Recovery:** `git show feat/google-x11-ab-amendment:devdoc/google-headed-x11-ab-amendment.md`;
  evidence via `git show feat/google-x11-ab-amendment:devdoc/evidence/google-x11-ab/<file>`.
- **Location:** the commit lives on the branch inside the main repo (`~/work/GoPi`) — a linked
  worktree shares the object store, so no push is required; the branch is not checked out anywhere
  else.
