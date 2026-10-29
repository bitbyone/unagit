#!/usr/bin/env python3
"""Record the landing page's walk through unagit as an asciicast: a shell
in the mock world, unagit typed at its prompt and driven as a person
would, at a person's pace, then unagit -s on one repository. Each scene
starts with a marker - its caption - which the page shows over the
terminal.

    python3 website/shots/record.py <world> <out.cast>
"""
import codecs
import json
import os
import pty
import random
import select
import struct
import fcntl
import termios
import time
import sys

COLS, ROWS = 140, 38
# Ctrl-A is Alt-Enter for terminals that keep Alt-Enter to themselves.
ACTIONS, ESC, ENTER = "\x01", "\x1b", "\r"

# The walk: ("caption", text) starts a scene, ("type", text) types at a
# person's pace, ("key", text) sends at once, ("wait", seconds) watches.
SCENES = [
    ("type", "unagit"), ("key", ENTER), ("wait", 1.4),
    ("type", "demo"), ("wait", 0.3), ("key", ENTER), ("wait", 1.2),
    ("caption", "Every repository on every server, in one list"),
    ("wait", 2.0), ("key", "/"), ("type", "pay"), ("wait", 1.6), ("key", ESC), ("key", ESC), ("wait", 0.8),
    ("caption", "Merge requests: pipeline, approvals, open threads"),
    ("key", "2"), ("wait", 1.6), ("key", "j"), ("wait", 0.5), ("key", "j"), ("wait", 0.5), ("key", "k"), ("wait", 0.4), ("key", "k"),
    ("wait", 0.6), ("key", ENTER), ("wait", 3.2), ("key", ESC), ("wait", 0.6),
    ("caption", "Every action has a name - Alt-Enter lists them"),
    ("key", ACTIONS), ("wait", 1.0), ("type", "review"), ("wait", 2.2), ("key", ESC), ("key", ESC), ("wait", 0.5),
    ("key", "q"), ("wait", 0.8),
    ("caption", "Any repository, no setup: unagit -s"),
    ("type", "cd platform/api-gateway"), ("key", ENTER), ("wait", 0.4),
    ("type", "unagit -s"), ("key", ENTER), ("wait", 2.4), ("key", "j"), ("wait", 1.6), ("key", "k"), ("wait", 0.8),
    ("key", "c"), ("wait", 1.0), ("type", "Allow 20 sign-ins per client"), ("wait", 0.6), ("key", ESC), ("wait", 0.6),
    ("key", "o"), ("wait", 2.6), ("key", "2"), ("wait", 2.6), ("key", "q"), ("wait", 1.5),
]


def main():
    world, out = os.path.abspath(sys.argv[1]), sys.argv[2]
    # The walk is unagit's own: unibox's integration is off for it.
    cfg = os.path.join(world, "config", "config.yaml")
    with open(cfg) as f:
        text = f.read()
    with open(cfg, "w") as f:
        f.write(text.replace("unibox: true", "unibox: false"))
    env = {
        "HOME": world,
        "PATH": os.path.join(world, "bin") + ":/usr/bin:/bin:/usr/local/bin",
        "UNAGIT_CONFIG_DIR": os.path.join(world, "config"),
        "TERM": "xterm-256color",
        "COLORTERM": "truecolor",
        "LANG": "C.UTF-8",
        # A prompt in the theme's gold and teal.
        "PS1": "\\[\\e[38;2;165;208;192m\\]\\w\\[\\e[0m\\] \\[\\e[38;2;194;152;93m\\]$\\[\\e[0m\\] ",
    }
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(os.path.join(world, "src"))
        os.execve("/bin/bash", ["bash", "--noprofile", "--norc", "-i"], env)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    rnd = random.Random(7)
    # A read can end inside a character; the decoder keeps its first bytes.
    decode = codecs.getincrementaldecoder("utf-8")("replace").decode
    events = []
    start = None

    def now():
        return 0.0 if start is None else time.time() - start

    def pump(seconds):
        end = time.time() + seconds
        while time.time() < end:
            r, _, _ = select.select([fd], [], [], 0.02)
            if r:
                try:
                    data = os.read(fd, 65536)
                except OSError:
                    return
                text = decode(data)
                if text:
                    events.append([round(now(), 4), "o", text])

    pump(1.0)
    # What the shell wrote as it started is the first frame, at once.
    first = "".join(e[2] for e in events)
    events.clear()
    events.append([0.0, "o", "\x1b[2J\x1b[H" + first.split("\x1b[?2004h")[-1]])
    start = time.time()
    pump(0.6)
    for kind, arg in SCENES:
        if kind == "wait":
            pump(arg)
        elif kind == "caption":
            events.append([round(now(), 4), "m", arg])
        elif kind == "key":
            os.write(fd, arg.encode())
            pump(0.12)
        else:
            for ch in arg:
                os.write(fd, ch.encode())
                pump(rnd.uniform(0.06, 0.13))
    os.kill(pid, 9)
    header = {"version": 2, "width": COLS, "height": ROWS, "timestamp": int(time.time()),
              "title": "unagit", "env": {"TERM": "xterm-256color"}}
    with open(out, "w") as f:
        f.write(json.dumps(header) + "\n")
        for e in sorted(events, key=lambda e: e[0]):
            f.write(json.dumps(e, ensure_ascii=False) + "\n")
    print("recorded", out, round(now(), 1), "s")


if __name__ == "__main__":
    main()
