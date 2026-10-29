#!/usr/bin/env python3
"""Drive the real unagit in a terminal over the mock world, and keep what it
wrote to the terminal at each shot, for render.mjs to draw.

    python3 website/shots/shoot.py <world> <unagit> <out-dir>

Each shot is a list of steps: ("wait", seconds), ("keys", text). Keys are
sent as typed; \r is Enter, \x1b Esc.
"""
import os
import pty
import select
import signal
import struct
import fcntl
import termios
import time
import sys

COLS, ROWS = 160, 44

# What each shot does after unagit started and was unlocked. The name is
# the file the shot is written to.
SHOTS = [
    ("merge-requests", [("keys", "2"), ("wait", 1.5), ("keys", "\r"), ("wait", 2.5)]),
    ("sandboxes", [("keys", "5"), ("wait", 2.5), ("keys", "j"), ("wait", 1.5)]),
]
SOLO = ("standalone", [("wait", 2.5), ("keys", "j"), ("wait", 1.5)])


def run(world, binary, args, steps_before, shots, out):
    env = {
        "HOME": world,
        "PATH": os.path.join(world, "bin") + ":/usr/bin:/bin:/usr/local/bin",
        "UNAGIT_CONFIG_DIR": os.path.join(world, "config"),
        "TERM": "xterm-256color",
        "COLORTERM": "truecolor",
        "LANG": "C.UTF-8",
    }
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(world)
        os.execve(binary, [binary] + args, env)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    buf = bytearray()

    def pump(seconds):
        end = time.time() + seconds
        while time.time() < end:
            r, _, _ = select.select([fd], [], [], 0.05)
            if r:
                try:
                    data = os.read(fd, 65536)
                except OSError:
                    return
                buf.extend(data)

    def do(steps):
        for kind, arg in steps:
            if kind == "wait":
                pump(arg)
            else:
                for ch in arg:
                    os.write(fd, ch.encode())
                    pump(0.08)

    do(steps_before)
    for name, steps in shots:
        do(steps)
        with open(os.path.join(out, name + ".raw"), "wb") as f:
            f.write(buf)
        print("shot", name, len(buf), "bytes")
    os.kill(pid, signal.SIGTERM)
    pump(0.3)


def main():
    world, binary, out = (os.path.abspath(a) for a in sys.argv[1:4])
    os.makedirs(out, exist_ok=True)
    # The passphrase the mock's vault is sealed with.
    unlock = [("wait", 2.0), ("keys", "demo\r"), ("wait", 2.5)]
    run(world, binary, [], unlock, SHOTS, out)
    name, steps = SOLO
    run(world, binary, ["-s", os.path.join(world, "src/platform/api-gateway")], [], [(name, steps)], out)


if __name__ == "__main__":
    main()
