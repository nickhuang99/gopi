#!/bin/sh
# P2 — the absent user bus: is the PATH missing, or is the socket gone? Read-only.
sec() { echo; echo "=== $1 ==="; }
echo "### probe script: p2-userbus.sh"
UID_N=$(id -u)
sec "stat \$XDG_RUNTIME_DIR/bus"; stat "/run/user/$UID_N/bus"
sec "stat \$XDG_RUNTIME_DIR/systemd/private"; stat "/run/user/$UID_N/systemd/private"
sec "ls -la \$XDG_RUNTIME_DIR/systemd/"; ls -la "/run/user/$UID_N/systemd/"
sec "ls -la \$XDG_RUNTIME_DIR/ (full, alphabetical)"; ls -la "/run/user/$UID_N/"
sec "grep for bus/systemd entries in that listing"; ls -1a "/run/user/$UID_N/" | grep -E 'bus|systemd'
sec "ss -xlp  (HOST-WIDE unix listeners, filtered to this runtime dir)"; ss -xlp 2>/dev/null | grep -F "/run/user/$UID_N"
sec "/proc/net/unix (bound path strings, filtered)"; grep -F "/run/user/$UID_N" /proc/net/unix
sec "which pids own the runtime-dir sockets"; for p in $(ls -d /proc/[0-9]* 2>/dev/null); do
  for f in "$p"/fd/*; do
    t=$(readlink "$f" 2>/dev/null) || continue
    case "$t" in socket*) ;; *) continue;; esac
    # only report processes whose /proc/net/unix inode is one of the two sockets
    :
  done
done
echo "(inode resolution below)"
sec "manager pid + its socket fds"; MANAGER=$(systemctl show -p MainPID --value user@1000.service 2>/dev/null); echo "user@1000 MainPID=$MANAGER"; ls -l "/proc/$MANAGER/fd" 2>&1 | head -40
sec "manager mount namespace view of the paths"; ls -la "/proc/$MANAGER/root/run/user/$UID_N/systemd/" 2>&1; stat "/proc/$MANAGER/root/run/user/$UID_N/bus" 2>&1
sec "namespace identity (self vs manager)"; for ns in mnt net user ipc pid; do printf '%-5s self=%s manager=%s\n' "$ns" "$(readlink /proc/self/ns/$ns)" "$(readlink /proc/$MANAGER/ns/$ns 2>&1)"; done
sec "mountinfo rows for /run/user (self)"; grep -F "/run/user" /proc/self/mountinfo
sec "mountinfo rows for /run/user (manager)"; grep -F "/run/user" "/proc/$MANAGER/mountinfo"
sec "mtimes"; stat -c '%n mtime=%y ctime=%z' "/run/user/$UID_N" "/run/user/$UID_N/systemd" "/run/user/$UID_N/systemd/inaccessible" 2>&1
sec "tmpfiles rules touching /run/user"; grep -rF "/run/user" /usr/lib/tmpfiles.d/ /etc/tmpfiles.d/ 2>/dev/null
sec "systemd user units shipped (dbus.socket / systemd/private)"; ls -la /usr/lib/systemd/user/dbus.socket /usr/lib/systemd/user/systemd-exit.service 2>&1
sec "dbus.socket (systemd user unit) content"; cat /usr/lib/systemd/user/dbus.socket 2>&1
sec "connect() attempts"; command -v python3 >/dev/null 2>&1 && echo "python3: $(command -v python3)" || echo "python3: ABSENT"
python3 - <<'PY' 2>&1
import socket, os, errno
paths = [f"/run/user/{os.getuid()}/bus", f"/run/user/{os.getuid()}/systemd/private"]
for p in paths:
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        s.connect(p)
        print(f"connect({p}) -> OK (manager answered)")
    except OSError as e:
        print(f"connect({p}) -> {errno.errorcode.get(e.errno,'?')}: {e.strerror}")
    finally:
        s.close()
PY
sec "dbus clients"; echo "DBUS_SESSION_BUS_ADDRESS=[${DBUS_SESSION_BUS_ADDRESS-<unset>}]"; dbus-send --session --dest=org.freedesktop.DBus --type=method_call --print-reply / org.freedesktop.DBus.ListNames 2>&1 | head -3; echo "dbus-send exit=$?"
