#!/usr/bin/env python3
"""Record the landing page's walk through unagit as an asciicast: unagit
started in a world not set up yet (mock -fresh), driven as a person would,
briskly - from adding the servers to a pipeline gone green. Each scene
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
ACTIONS, ESC, ENTER, TAB, SPACE = "\x01", "\x1b", "\r", "\t", " "
CTRL_F, CTRL_K, CTRL_L, CTRL_S, CTRL_U = "\x06", "\x0b", "\x0c", "\x13", "\x15"


def server(name, host, token):
    """Add a GitLab server in Settings: its name, address, where its
    clones go, and a token."""
    return [
        ("key", "a"), ("wait", 0.5),
        ("type", name), ("key", TAB), ("type", host), ("key", TAB), ("type", "~/src"), ("wait", 0.3),
        ("key", ESC), ("key", "j"), ("key", "j"), ("key", "i"), ("type", token, 0.02), ("wait", 0.3),
        ("key", ESC), ("key", "s"), ("wait", 0.9),
    ]


# The walk: ("caption", text) starts a scene, ("type", text[, pace])
# types at a person's pace, ("key", text) sends at once, ("wait",
# seconds) watches.
SCENES = [
    ("wait", 0.8), ("type", "demo"), ("key", ENTER), ("wait", 1.6),
    ("caption", "Add your GitLab servers, and where their clones go"),
    ("key", ESC), ("key", "l"), ("wait", 0.3),
    *server("Northwind", "gitlab.northwind.dev", "glpat-x8Kq2vR7mN4tY9wZ"),
    *server("GitLab.com", "gitlab.com", "glpat-Lm3pQ8sT1vX6zB2d"),
    ("caption", "Pick the groups you work in"),
    ("key", ESC), ("key", "j"), ("wait", 0.2), ("key", "j"), ("wait", 0.2), ("key", "l"), ("wait", 0.5),
    ("key", "r"), ("wait", 1.2),
    ("key", "j"), ("key", SPACE), ("wait", 0.3), ("key", "j"), ("key", SPACE), ("wait", 0.2), ("key", SPACE), ("wait", 0.3),
    ("key", "j"), ("key", "j"), ("key", "j"), ("key", SPACE), ("wait", 0.6),
    ("key", "p"), ("wait", 1.4), ("key", "m"), ("wait", 1.6),
    ("caption", "Every repository of every server, in one list"),
    ("key", "1"), ("wait", 1.8),
    ("caption", "Find one and star it - or mark a few and star them all"),
    ("key", "/"), ("type", "gate"), ("wait", 0.6), ("key", ESC), ("wait", 0.3), ("key", CTRL_F), ("wait", 0.9),
    ("key", ESC), ("wait", 0.5), ("key", "j"), ("key", SPACE), ("wait", 0.3), ("key", SPACE), ("wait", 0.5),
    ("key", ACTIONS), ("wait", 0.6), ("type", "star"), ("wait", 0.6), ("key", ENTER), ("wait", 1.2),
    ("caption", "History, pipelines and job logs, right from the list"),
    ("key", "g"), ("key", "g"), ("wait", 0.3), ("key", CTRL_L), ("wait", 1.4),
    ("key", "J"), ("wait", 1.6), ("key", ENTER), ("wait", 2.6),
    ("caption", "A flaky test? Run the job again"),
    ("key", ESC), ("wait", 0.4), ("key", "R"), ("wait", 2.2), ("key", ESC), ("key", ESC), ("wait", 0.5),
    ("caption", "Watch the branch: unagit follows its pipeline for you"),
    ("key", ACTIONS), ("wait", 0.5), ("type", "watch"), ("wait", 0.5), ("key", ENTER), ("wait", 1.4),
    ("caption", "Review: read the threads, answer, approve"),
    ("key", "2"), ("wait", 1.2), ("key", "c"), ("wait", 2.2),
    ("key", "i"), ("wait", 0.4), ("type", "Twenty works for me - the app backs off after three tries anyway.", 0.03),
    ("wait", 0.3), ("key", CTRL_S), ("wait", 1.6), ("key", ESC), ("wait", 0.4),
    ("key", ACTIONS), ("wait", 0.5), ("type", "approve"), ("wait", 0.5), ("key", ENTER), ("wait", 0.9), ("key", "a"), ("wait", 1.0),
    ("key", "r"), ("wait", 1.4),
    ("caption", "Your changes: read the diffs, commit and push"),
    ("key", "1"), ("wait", 0.5), ("key", "/"), ("type", "identity"), ("wait", 0.4), ("key", ESC), ("wait", 0.4),
    ("key", CTRL_K), ("wait", 1.6), ("key", "j"), ("wait", 0.4), ("key", "j"), ("wait", 1.4),
    ("key", "j"), ("wait", 0.3), ("key", "j"), ("wait", 1.4), ("key", "x"), ("wait", 0.6),
    ("key", "c"), ("wait", 0.6), ("type", "Finish the OAuth callback"), ("wait", 0.4), ("key", ESC), ("key", "p"), ("wait", 0.9),
    ("key", "p"), ("wait", 2.4),
    ("caption", "...and open the merge request"),
    ("key", ESC), ("wait", 0.4), ("key", ACTIONS), ("wait", 0.5), ("type", "new merge"), ("wait", 0.5), ("key", ENTER), ("wait", 1.4),
    ("key", CTRL_U), ("type", "Sign in with GitHub and Google"), ("wait", 0.6), ("key", ESC), ("key", "r"), ("wait", 2.4),
    ("key", "c"), ("wait", 0.8),
    ("caption", "Meanwhile: what happened, as it happened"),
    ("wait", 1.0), ("key", "4"), ("wait", 3.6),
    ("caption", "The watched branch is green again"),
    ("key", "1"), ("wait", 0.4), ("key", ESC), ("wait", 0.4), ("key", "g"), ("key", "g"), ("wait", 1.2),
    ("key", "J"), ("wait", 4.0),
]


def main():
    world, out = os.path.abspath(sys.argv[1]), sys.argv[2]
    env = {
        "HOME": world,
        "PATH": os.path.join(world, "bin") + ":/usr/bin:/bin:/usr/local/bin",
        "UNAGIT_CONFIG_DIR": os.path.join(world, "config"),
        "TERM": "xterm-256color",
        "COLORTERM": "truecolor",
        "LANG": "C.UTF-8",
        # The world's servers, answered by mock serve over TLS of its own.
        "HTTPS_PROXY": "http://127.0.0.1:8929",
        "SSL_CERT_FILE": os.path.join(world, "tls", "ca.pem"),
    }
    binary = os.path.join(world, "bin", "unagit")
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(os.path.join(world, "src"))
        os.execve(binary, [binary], env)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    rnd = random.Random(7)
    # A read can end inside a character; the decoder keeps its first bytes.
    decode = codecs.getincrementaldecoder("utf-8")("replace").decode
    events = []
    start = time.time()

    def now():
        return time.time() - start

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

    for step in SCENES:
        kind, arg = step[0], step[1]
        if kind == "wait":
            pump(arg)
        elif kind == "caption":
            events.append([round(now(), 4), "m", arg])
        elif kind == "key":
            os.write(fd, arg.encode())
            pump(0.08)
        else:
            pace = step[2] if len(step) > 2 else 0.055
            for ch in arg:
                os.write(fd, ch.encode())
                pump(rnd.uniform(pace * 0.6, pace * 1.4))
    # The last screen holds until the walk loops.
    events.append([round(now(), 4), "o", ""])
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
