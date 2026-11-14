import fcntl, os, pty, select, struct, subprocess, sys, termios, time

DUMP = "/home/user/workspace/gopi-01a0d5ca-dump/liveA/chat-screen.dump"
os.makedirs(os.path.dirname(DUMP), exist_ok=True)
GOPI = "/home/user/workspace/gopi-01a0d5ca22507cb9ba577dc6000739cd/bin/gopi"
env = dict(os.environ, ANTHROPIC_BASE_URL="http://127.0.0.1:8791", ANTHROPIC_AUTH_TOKEN="stub-key",
           GOPI_HOME="/home/user/workspace/gopi-01a0d5ca-dump/sb",
           GOPI_SETTINGS="/home/user/workspace/gopi-01a0d5ca-dump/sb/settings.json",
           TERM="xterm-256color")

master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 100, 0, 0))
p = subprocess.Popen([GOPI, "chat", "--dump-screen=" + DUMP], stdin=slave, stdout=slave,
                     stderr=slave, env=env, cwd="/home/user/workspace/gopi-01a0d5ca-dump", close_fds=True)
os.close(slave)
out = bytearray()
def pump(seconds):
    end = time.time() + seconds
    while time.time() < end:
        r, _, _ = select.select([master], [], [], 0.2)
        if r:
            try:
                b = os.read(master, 65536)
            except OSError:
                return False
            if not b:
                return False
            out.extend(b)
    return True

pump(4)
os.write(master, b"say the thing")
time.sleep(0.4); pump(0.4)
os.write(master, b"\r")
pump(20)
os.write(master, b"/quit")
time.sleep(0.4); pump(0.4)
os.write(master, b"\r")
pump(6)
try:
    p.terminate()
except ProcessLookupError:
    pass
p.wait(timeout=15)
open("/home/user/workspace/gopi-01a0d5ca-dump/pty.raw", "wb").write(bytes(out))
print("EXIT", p.returncode, "bytes", len(out))
