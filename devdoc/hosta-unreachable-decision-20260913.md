<!-- doc-class: historical -->
# Task Report: hosta (10.0.0.13) is not keyless-reachable — deliberate decision

**Task**: `bug(infra): hosta (10.0.0.13) is keyless-unreachable from peerbox`
**Branch**: `docs/hosta-unreachable-decision` (worktree `~/workspace/gopi-hosta-access`)
**Date**: 2026-09-13 · **Host of record**: peerbox (`nick`) · **Second vantage**: HP (`nick@10.0.0.5`, keyless)

---

## Decision (acceptance branch 2)

> **hosta (10.0.0.13) is NOT a managed node.** It is deliberately excluded from
> keyless cross-node automation, and no further agent effort is spent on it.
> Re-entry requires one operator action (§ Unblock), which is outside a
> non-interactive task's reach: it needs either (a) a credential peerbox does not
> hold, or (b) a change to a host the agent cannot log into.

The epic's acceptance criterion 1 offered exactly this exit ("OR the task records
the deliberate decision that hosta is not a managed node"). This document is that
record, with the evidence that makes the decision **provable rather than lazy**.

---

## Root cause (mechanism, not symptom)

**Symptom**: `ssh -o BatchMode=yes nick@10.0.0.13 hostname` → `Permission denied (publickey,password)` (exit 255).

**Mechanism**: hosta's sshd offers `publickey,password`, i.e. key auth is *enabled*
but the offered key is not in `authorized_keys` for that account, and BatchMode
forbids the password fallback. The agent is key-only by construction.

**Why no key is offered by hosta's side of the story — the decisive finding**:
peerbox's `~/.ssh/authorized_keys` contains a **hosta** key
(`ssh-rsa AAAA…`, comment `nick@hosta`). Its public key matches **no private
key on peerbox and none on HP** (`ssh-keygen -y` comparison, §evidence 5) — and the
corresponding `ssh-ed25519` entry in peerbox's `authorized_keys` (`user@example.com`)
does match peerbox's `id_ed25519`.

So the trust is **one-way, hosta → peerbox** (hosta is authorized *into* peerbox), and
the private half of hosta's own key lives **on hosta**, a host the agent cannot
log into. Fixing the reverse direction therefore needs a write **on hosta** — the
one thing that is impossible from here. There is no path that keeps the fix on
peerbox's side. **That asymmetry is the root cause of this being an operator task,
not an agent task.**

**5-why chain**
1. automation on .13 fails → non-interactive ssh refuses.
2. ssh refuses → offered key ∉ hosta's `authorized_keys`, password forbidden.
3. no other peerbox key/account works → hosta's `authorized_keys` holds none of peerbox's keys.
4. the fix must add peerbox's `id_ed25519.pub` to hosta's `~/.ssh/authorized_keys`.
5. that write requires an interactive/credentialed login to a host the agent cannot
   reach by design → **stopping rule: this is an operator action, not a task action.**

---

## Evidence (reproducible; raw transcript in `devdoc/hosta-unreachable-evidence-20260913.txt`)

| # | Check | Result |
|---|---|---|
| 1 | `ping -c4 10.0.0.13` | 4/4 received, **0% loss, rtt 0.875 ms** — host is UP |
| 2 | `ssh -o BatchMode=yes nick@10.0.0.13 hostname` | `Permission denied (publickey,password)`, **exit 255** |
| 3 | 15 combos: `{id_ed25519, hp_to_peerbox, do_id_ed25519}` × `{nick,root,pi,admin,ubuntu}` | **15/15 refused** |
| 4 | HP's **own** keys, from HP (`ssh hp '… ssh nick@10.0.0.13'`) | 6/6 refused — not a peerbox-only quirk |
| 5 | `ssh -o BatchMode=yes -J hp nick@10.0.0.13` (jump host) | refused — no forwarding/agent path |
| 6 | `ssh-add -l` | no agent → a forwarded identity cannot exist |
| 7 | ports `22,80,111,11434` open; `443,139,445,3389,631,5900,2222,8080,9000` closed | no other shell surface |
| 8 | `rpcinfo -p` → `nfs 2049`, `mountd`, `portmapper` | NFS **v3** present |
| 9 | `showmount -e 10.0.0.13` | **empty export list — no NFS shares**, so no write path |
| 10 | `curl -X OPTIONS http://10.0.0.13/` → `405`; `PUT /gopi-probe.txt` → `404` | nginx/1.18.0 **static only**, no upload |
| 11 | `curl http://10.0.0.13:11434/api/version` → `{"version":"0.17.7"}` | unauthenticated **Ollama** API (see note) |
| 12 | `~/.ssh/config` | **no `Host 10.0.0.13` stanza** → falls through `Host *`; no `IdentityFile` trick applies |
| 13 | stored credentials: `~/.ssh/*`, `settings.json`, `memory.db` (`distilled_qa`), shell rc | **no hosta password anywhere** (`sshpass` exists, has nothing to feed) |

**Note on #11 (honest scope)**: the unauthenticated Ollama API on `:11434` is a
real *service* surface (`/api/tags` lists `glm-4.7-flash:latest`), but it grants
**no shell, no filesystem write, no keyless login** — it cannot make hosta a
keyless node. Recorded as an inventory fact, not as a bypass. Likewise `:80` serves
a static LibreOffice-exported page ("My Jouney Is Oceans Of Stars…"), not an
admin surface.

---

## Node credential inventory (the durable part of this deliverable)

| Node | IP | keyless from peerbox? | Evidence |
|---|---|---|---|
| **hp** | 10.0.0.5 | ✅ **YES** — `ssh hp hostname` → `hostb-Laptop` | used as second vantage in this task |
| **hosta** | 10.0.0.13 | ❌ **NO** (this document) | 15/15 + 6/6 refused, exit 255 |
| rpi5 | 10.0.0.4 / .14 | ⛔ **down** — `No route to host` | `ssh` exit ≠ 0 |
| rpi5-lite | 10.0.0.17 | ⛔ **down** — `No route to host` | `ssh` exit ≠ 0 |
| ollama | 10.0.0.8 | ⛔ **ssh connection refused** (service host, no sshd) | `ssh` exit ≠ 0 |
| nas | 10.0.0.5 | = hp (same IP, alias in `~/.ssh/config`) | config |

**Managed keyless nodes today: `peerbox` (local) + `hp` (10.0.0.5). Only these
two may be assumed by cross-node automation.** This supersedes the note in
`devdoc/night-report-20260910.md` ("hosta … measured blocker") and closes the
open question recorded in `devdoc/voice-input-design-space.md` §F4 (T1 SSH
transport "Blocked today") as a **standing decision, not a pending unknown**.

---

## Unblock (operator action — one command on hosta, then one on peerbox)

Run **on hosta** (interactive or with a credential the agent does not hold):

```bash
mkdir -p ~/.ssh && chmod 700 ~/.ssh
# paste peerbox's public key (printed below), preserving any existing lines:
printf '%s\n' "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAdPR/lK01VUqUFqyowd1AtXuquCmdEZkjwhskN2ZXVU nick@peerbox" >> ~/.ssh/authorized_keys
chmod 600 ~/.ssh/authorized_keys
```

Get the exact line on peerbox with `cat ~/.ssh/id_ed25519.pub` (the key whose
fingerprint appears in the epic's `-vvv` output: `SHA256:WB/Gtn+llw7t4MVUHVFLts9kGSFKo6+LkOD6/2JGBU`).

Then verify **from peerbox**:

```bash
ssh -o BatchMode=yes nick@10.0.0.13 hostname    # expect a hostname, exit 0
```

Alternative (b) — hosta's own key: install a private key that hosta already
trusts into peerbox *and* add peerbox's public key on hosta. This also needs a write
on hosta, so it does not avoid the operator step.

---

## Acceptance criterion 3 (post-unblock follow-up) — prepared, not executed

The follow-up is blocked by the same login, so it is **declared ready, not faked**:

- `scripts/voice-mic-probe.sh` **exists** (19 387 B, mode 755) in this worktree.
- Criterion 3's probe would be wrong as written anyway: `voice-mic-probe.sh` has
  **no `--out` flag** (it prints a `VOICE_MIC_TABLE` TSV + a `DEVICE_VERDICT`
  section to *stdout*). The correct post-unblock command is therefore:

```bash
scp ~/work/GoPi/scripts/voice-mic-probe.sh 10.0.0.13:/tmp/ && \
  ssh 10.0.0.13 'bash /tmp/voice-mic-probe.sh' | tee /tmp/probe-hosta.txt
```

---

## Completed

- Exhaustive non-interactive access attempt against hosta: 3 keys × 5 users from
  peerbox, 3 HP keys × 2 users from HP, a jump-host attempt, an agent check, a port
  scan, an NFS/mountd probe, and HTTP-method probing.
- Root cause established (one-way trust + fix requiring a write on hosta), with the
  5-why chain.
- Deliberate decision recorded (acceptance branch 2) with reproducible evidence.
- Operator unblock step written as one copy-paste command + the verification command.
- Node credential inventory for all 6 LAN hosts (which are managed, which are dead).
- Corrected the epic's criterion-3 command (`--out` flag does not exist).

## Not Done

- **No key was installed on hosta and the login still fails** — deliberately: doing
  so requires a credential/interactive login this non-interactive task does not
  have. Acceptance criterion 1 is satisfied via its explicit "OR … deliberate
  decision" clause.
- **Criterion 3's `DEVICE_VERDICT` block was not produced** — unreachable without
  the login; the command is prepared above.
- No code/build change (this task changes no buildable artifact) → the repo build
  was not run; the worktree contains **docs only**.

---

## Verification

- Build: n/a — docs-only change (no `.go` file touched; `git status --porcelain`
  lists only files under `devdoc/`).
- Tests: n/a for the same reason; the acceptance test is *behavioural* and is the
  refusal reproduction below.
- **Acceptance re-run (branch 2 check, executed from peerbox via this worktree's
  evidence script)**: `ssh -o BatchMode=yes nick@10.0.0.13 hostname` →
  `Permission denied (publickey,password)`, exit 255; raw transcript
  `devdoc/hosta-unreachable-evidence-20260913.txt` (74 lines) contains every
  command above with its real output.
- Commit: the tip of branch `docs/hosta-unreachable-decision` in `~/work/GoPi` —
  this document and the raw transcript are added by that commit (docs-only, no
  buildable artifact touched). Read it with
  `git -C ~/work/GoPi show docs/hosta-unreachable-decision:devdoc/hosta-unreachable-decision-20260913.md`.
