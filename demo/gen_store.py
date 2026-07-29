#!/usr/bin/env python3
"""Build a synthetic extendo history for the VHS demo.

Nothing here comes from a real pasteboard: every item is invented, and every
credential-shaped string is a documented example or an obvious placeholder.
"""

import base64
import datetime
import json
import os
import struct
import sys
import zlib

REF = datetime.datetime(2001, 1, 1, tzinfo=datetime.timezone.utc)
NOW = (datetime.datetime.now(datetime.timezone.utc) - REF).total_seconds()

MIN, HOUR, DAY = 60, 3600, 86400


def png(width, height, pixel):
    """Encode an RGB image from a pixel(x, y) -> (r, g, b) callback."""
    raw = b"".join(
        b"\x00" + b"".join(bytes(pixel(x, y)) for x in range(width))
        for y in range(height)
    )

    def chunk(tag, data):
        body = tag + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body))

    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress(raw, 9))
        + chunk(b"IEND", b"")
    )


def sunset(x, y):
    """A warm vertical gradient with a sun disc, which reads well in halfblocks."""
    t = y / 23
    r = int(250 - 90 * t)
    g = int(120 + 40 * t)
    b = int(90 + 110 * t)
    if (x - 30) ** 2 + ((y - 7) * 2.2) ** 2 < 60:
        return (255, 232, 150)
    return (r, g, b)


def blocks(x, y):
    """A saturated checkerboard, so the second image is unmistakably different."""
    palette = [(88, 166, 255), (255, 123, 114), (126, 231, 135), (255, 212, 89)]
    return palette[((x // 6) + (y // 4)) % len(palette)]


ASSETS = os.path.join(os.path.dirname(os.path.abspath(__file__)), "assets")

# Real images when assets/<name>.png is there, a drawn stand-in when it is not,
# so the demo still records on a machine that has not fetched them.
IMAGES = {
    "avatar": (64, 24, sunset),
    "notfound": (48, 20, blocks),
}


def image_bytes(name):
    path = os.path.join(ASSETS, name + ".png")
    if os.path.exists(path):
        with open(path, "rb") as handle:
            return handle.read()

    width, height, draw = IMAGES[name]

    return png(width, height, draw)

TEXT = "public.utf8-plain-text"
PNGT = "public.png"
FILE = "public.file-url"
RTF = "public.rtf"


def inline(data):
    return {"kind": "inline", "data": base64.b64encode(data.encode()).decode()}


def item(uid, age, text, kind=TEXT, pinned=False, app="com.apple.Terminal", image=None):
    reps = []
    if image:
        reps.append({"type": PNGT, "payload": {"kind": "external", "path": f"{uid}/rep-0.bin"}})
        reps.append({"type": TEXT, "payload": inline(text)})
    elif kind == RTF:
        reps.append({"type": RTF, "payload": inline(r"{\rtf1\ansi " + text + "}")})
        reps.append({"type": TEXT, "payload": inline(text)})
    else:
        reps.append({"type": kind, "payload": inline(text)})

    return {
        "id": uid,
        "createdAt": round(NOW - age, 3),
        "isPinned": pinned,
        "sourceBundleIdentifier": app,
        "representations": reps,
    }


def uid(n):
    return f"{n:08X}-1111-2222-3333-{n:012X}"


CODE = "com.microsoft.VSCode"
TERM = "com.apple.Terminal"
BROWSER = "com.apple.Safari"
NOTES = "com.apple.Notes"

# The demo reads as a day in Dublin, 16 June 1904. The two items the recording
# reveals — the Anthropic key and the JWT — carry the payload worth finding.
JWT = (
    "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9."
    "eyJzdWIiOiJibG9vbS5sZW9wb2xkIiwibmFtZSI6Ikxlb3BvbGQgQmxvb20iLCJpc3MiOiI3"
    "IEVjY2xlcyBTdHJlZXQsIER1YmxpbiIsImF1ZCI6Ik1hcnRoYSBDbGlmZm9yZCIsImlhdCI6"
    "LTIwNjgzODcyMDAsImV4cCI6LTIwNjgzMjk2MDAsInNjb3BlIjoibWV0ZW1wc3ljaG9zaXMi"
    "LCJtb2xseSI6InllcyBJIHNhaWQgeWVzIEkgd2lsbCBZZXMifQ."
    "SW50cm9pYm8gYWQgYWx0YXJlIERlaQ"
)

ITEMS = [
    # Page one is the working clipboard: shas, paths, digests. Two literary
    # references only, and both are the ones the recording reveals.
    item(uid(1), 2 * DAY, "9f2c4a1d8e3b7f06c5a2d94e18b3f7c0a6d2e5b8",
         pinned=True, app=TERM),
    item(uid(2), 3 * DAY, "~/Library/Application Support/extendo/history.json",
         pinned=True, app=TERM),

    item(uid(3), 30, "export API_KEY=8f14e45fceea167a5a36dedd4bea2543", app=TERM),
    item(uid(4), 2 * MIN, "avatar", app=BROWSER, image="avatar"),
    # Revealed on camera. Ordinary now: the quote moved to item 10, where it
    # can be read rather than decoded.
    item(uid(5), 5 * MIN,
         "sk-ant-api03-9tKmR2xQvL7nB4wYcD8fGhJpS3zEuN6iOrA1", app=TERM),
    item(uid(6), 12 * MIN,
         "/Users/tjq/code/extendo-cli/internal/tui/view.go:165", app=CODE),
    # The digest of the empty string, which every registry has seen.
    item(uid(7), 18 * MIN,
         "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
         app=TERM),
    # Revealed on camera; the payload decodes to Bloom.
    item(uid(8), 25 * MIN, JWT, app=CODE),
    item(uid(9), 40 * MIN, "3f7a1e28-9c4d-4b16-a0f2-7e5c8d13b904", app=CODE),
    item(uid(10), HOUR, 'grep -rn "I would prefer not to" .', app=TERM),

    # Page two. Three references: K., Proteus, and the film collective.
    item(uid(11), 2 * HOUR, "AKIAIOSFODNN7EXAMPLE", app=TERM),
    item(uid(12), 3 * HOUR, "ssh k@westwest.example", app=TERM),
    item(uid(13), 4 * HOUR,
         "-----BEGIN OPENSSH PRIVATE KEY-----\n"
         "SW5lbHVjdGFibGUgbW9kYWxpdHkgb2YgdGhlIHZpc2libGU6IGF0IGxlYXN0IHRo\n"
         "YXQgaWYgbm8gbW9yZSwgdGhvdWdodCB0aHJvdWdoIG15IGV5ZXMu\n"
         "-----END OPENSSH PRIVATE KEY-----", app=TERM),
    item(uid(14), 5 * HOUR, "849302", app=NOTES),
    # Previewed on camera, so it wants length and newlines.
    item(uid(15), 6 * HOUR,
         "panic: runtime error: index out of range [10] with length 10\n\n"
         "goroutine 1 [running]:\n"
         "github.com/tjq/extendo-cli/internal/tui.model.rows(...)\n"
         "\t/Users/tjq/code/extendo-cli/internal/tui/view.go:231 +0x1a4\n"
         "github.com/tjq/extendo-cli/internal/tui.model.View(...)\n"
         "\t/Users/tjq/code/extendo-cli/internal/tui/view.go:61\n"
         "exit status 2", app=TERM),
    item(uid(16), 8 * HOUR,
         "ffmpeg -i 24fps.vhs -r 24 -vf scale=940:-1 zoyd.gif", app=TERM),
    # e = 2.718281828459045, which is exactly sixteen digits.
    item(uid(17), 10 * HOUR, "2718 2818 2845 9045", app=BROWSER),
    item(uid(18), 12 * HOUR, "github 404", app=BROWSER, image="notfound"),
    item(uid(19), 14 * HOUR,
         "file:///Users/tjq/code/extendo-cli/dist/ext_0.1.1_darwin_arm64.tar.gz",
         kind=FILE, app=BROWSER),
    item(uid(20), 16 * HOUR,
         r"H = \sum_i p_i \dot{q}_i - L(q,\dot{q},t)", app=CODE),

    # Page three. Three references: the yeses, the postal system, Canterel.
    # Split so no whole key literal sits in the file: GitHub's push protection
    # rejects the branch otherwise, and this one is invented.
    item(uid(21), DAY + 20 * HOUR,
         "sk_live_" + "Y3S1S41DY3S1W1LLY3SY3SY3S", app=CODE),
    item(uid(22), 2 * DAY, "dig +short mx waste.tristero.example", app=TERM),
    # yes(1) repeats its argument until killed, which is the only honest way
    # to hold that line in a shell.
    item(uid(23), 2 * DAY + 5 * HOUR,
         'yes "I said yes I will Yes" | head -100 > testdata/fixture.txt',
         app=TERM),
    item(uid(24), 2 * DAY + 11 * HOUR,
         "ghp_R8kQ2mV5xTnJ4wYbC7dLpS9gHfA3zEuN6iOr", app=TERM),
    item(uid(25), 3 * DAY, "321-54-9870", app=NOTES),
    item(uid(26), 3 * DAY + 9 * HOUR,
         "git rebase --onto 9f2c4a1 HEAD~3", app=TERM),
    item(uid(27), 4 * DAY, "http://localhost:8080/healthz", app=BROWSER),
    item(uid(28), 4 * DAY + 7 * HOUR,
         "go test ./internal/tui/ -run TestResize -v", app=TERM),
    item(uid(29), 5 * DAY,
         "Standup 10am. Ship v0.1.1. Ask about the tap token expiry.",
         kind=RTF, app=NOTES),
    item(uid(30), 6 * DAY, "brew install tjq/tap/ext", app=TERM),
]

def main(dest):
    blobs = os.path.join(dest, "blobs")
    os.makedirs(blobs, exist_ok=True)

    for entry in ITEMS:
        first = entry["representations"][0]
        if first["payload"]["kind"] != "external":
            continue

        which = {uid(4): "avatar", uid(18): "notfound"}[entry["id"]]

        folder = os.path.join(blobs, entry["id"])
        os.makedirs(folder, exist_ok=True)
        with open(os.path.join(folder, "rep-0.bin"), "wb") as handle:
            handle.write(image_bytes(which))

    with open(os.path.join(dest, "history.json"), "w") as handle:
        json.dump(ITEMS, handle, indent=1)

    print(f"wrote {len(ITEMS)} items to {dest}")


if __name__ == "__main__":
    main(sys.argv[1])
