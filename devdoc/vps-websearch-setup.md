<!-- doc-class: historical -->
# New Websearch/Jumper VPS Setup Report

**Date**: 2026-08-21
**Server**: `search-bot` — DigitalOcean droplet **593881651**, Ubuntu 22.04.5 LTS, region **sfo3**
**Main IP**: `137.184.190.229` · **Reserved IP**: `137.184.244.35` (see §4 — currently unusable from China)
**SSH key**: `~/.ssh/do_id_ed25519` (ed25519, same key as old VPS)
**Local alias**: `ssh remotebox` (added to `~/.ssh/config`, replaces `digital` for all future agent use — old VPS 203.0.113.10 is now **read-only reference, do NOT touch**)

---

## 1. SSH hardening — DONE

| Setting | Value |
|---|---|
| **Port** | **4222** (moved off 22; port 22 removed from sshd and dropped by ufw) |
| `PasswordAuthentication` | **no** (was implicitly enabled by default!) |
| `PermitRootLogin` | `prohibit-password` (key-only root; equivalent to old VPS's `yes` for keys) |
| `LoginGraceTime` | 30s |
| `MaxAuthTries` | 3 |
| `KbdInteractiveAuthentication` | no (was already set) |
| `X11Forwarding` | no |

Backup of the original config: `/etc/ssh/sshd_config.bak.<ts>` on the VPS.

**Procedure used (lockout-safe)**: added `Port 4222` alongside `Port 22` → `sshd -t` → restart → verified 4222 from local → only then removed `Port 22` → restart → re-verified. Session never dropped (established connections survive sshd restart and ufw enable).

Verified from local: `ssh -p 4222` OK with ed25519 key; port 22 connection-refused on box, timeout (ufw drop) from outside.

## 2. Websearch tooling — DONE (lean)

**Installed**: `jq` 1.6 (plugin scripts return JSON). Nothing else.

**Already present**: curl 7.81, wget, git, python3 3.10.12.

**Deliberately NOT installed**: go, node, playwright, headless chromium.
Rationale (per `~/work/hermes-agent/devdoc/gopi-websearch-plugin-design.md`): the plugin layer is 0-token curl/python3 scripts — bing/google/github/wikipedia via curl, arxiv/deepseek via API. The VPS is the google/wikipedia **jumper**, which needs nothing beyond curl. The only future browser need is the doubao CDP experiment (W6) — install node+playwright **only if/when** that script is actually built and tested here (design doc also notes HP's headless firefox works better for doubao anyway).

**Outbound verification** (all from the VPS, all **200**):
```
https://en.wikipedia.org -> 200
https://github.com        -> 200
https://www.google.com    -> 200
https://api.github.com    -> 200   (github_search.sh API path works)
```

## 3. Firewall — DONE (ufw on the box)

```
Status: active (enabled at boot)
Default: deny (incoming), allow (outgoing)
Rules:   4222/tcp ALLOW IN (v4+v6)
```

- Chose ufw over DO cloud firewall: no API token available on this machine (see §5), ufw is persistent and sufficient.
- ICMP inbound is dropped (ufw `proto icmp` syntax unsupported on this build — deliberate skip, ping probes are not needed; old VPS behavior of relying on DO cloud firewall does not apply).
- Outbound is unrestricted → websearch scripts work; everything inbound except SSH is dropped.

## 4. Reserved IP 137.184.244.35 — INVESTIGATED, root cause found

**Status: the IP is correctly assigned and works globally — it is blocked by the GFW from China only.**

Evidence:
- Droplet metadata (no auth needed, from inside the box): `floating_ip/ipv4/active = true`, IP `137.184.244.35` — DO considers it **assigned & active** on droplet 593881651 (same region sfo3; anchor `10.48.0.6` present on eth0).
- Global vantage points (check-host.net, 5 nodes across US/EU/Asia): ping OK (60–190ms), **TCP 22 connect OK (150–220ms)**.
- Old VPS (same DC, sfo3): ping + port 22 both OK.
- **Local machine (China): 100% packet loss, all ports dropped.**
- Traceroute from China: packets to the reserved IP die at Chinese carrier border hop `64.86.161.1`; the main IP passes the same hop into DO's network (`ae6.sdr2.us-sjc1.as14061.net`). → China-side blackholing of this specific IP, not a DO problem.

Likely cause: DO reserved IPs are recycled; a previous tenant of `137.184.244.35` almost certainly ran a VPN/proxy that got GFW-blocked, and the block persists on the recycled IP. The fresh main IP `137.184.190.229` is unaffected.

**What would fix it**:
1. **Release `137.184.244.35` and reserve a NEW reserved IP** from the DO panel/API, assign it to droplet 593881651. (Recommended — a reserved IP that the GFW hasn't seen yet.) Needs either DO API token or manual panel action — no token was found on this machine (see §5).
2. Meanwhile, use the **main IP `137.184.190.229:4222` as the jumper endpoint** — it works from China today. If the GFW block on the reserved IP ever lifts (possible but no timeline), it can be adopted back.

## 5. Gotchas / notes for future agents

- **No DO API token exists on this machine**: checked `~/.hermes/.env` (only ANTHROPIC/DEEPSEEK keys), `~/.config` (incl. `doctl` — absent), `~/.ssh`, old VPS bash history, hermes config/state. If you need DO API later (e.g., new reserved IP), get a token from the DO control panel — never hardcode it.
- **Never touch the old VPS** (203.0.113.10, alias `digital`, port 422) — read-only reference for this setup; its sshd/firewall config was NOT copied over. The old VPS has reserved IP 203.0.113.10 assigned to it (its own anchor 10.48.0.5).
- **sshd config on Ubuntu 22.04**: `PasswordAuthentication` defaults to **yes** when commented — set it explicitly (done).
- **ufw quirks**: `ufw allow proto icmp` and `ufw allow icmp` both fail on this build (`Unsupported protocol 'icmp'` / `Need 'to' or 'from' clause`) — omit the rule or edit `/etc/ufw/before.rules` if ping-in is ever needed. `ufw --force enable` required to avoid interactive prompt.
- **Connection-refused vs timeout**: on port 22, box-local = refused (nothing listening), from outside = timeout (ufw drops). Don't mistake the timeout for a hang.
- **Reserved IPs and the GFW**: a DO reserved IP that "doesn't work" from China while working globally is almost certainly GFW-blacklisted from a previous tenant, not a DO issue. Test with check-host.net before blaming DO.
- WireGuard note from memory: old VPS runs WG on 51820 (RPi5 VPN) — the new VPS does NOT have WG set up; if the google_search.sh script's VPN path is needed here, that's future work (design doc's google path currently points at RPi5 VPN; the DO-VPS jumper path is the plain-curl one).
