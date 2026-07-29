# extendo-cli demo
#
# Records against a synthetic history: every item is invented, and every
# credential-shaped string is a documented example or an obvious placeholder.

Output demo.gif

Set Shell zsh
Set FontSize 16
Set Width 1180
# Just past the frame's own height: the list is 10 rows plus header, footer and
# rules, and a terminal shorter than that would make the picker scroll.
Set Height 430
Set Padding 24
Set Framerate 24
Set TypingSpeed 60ms

# Setup, off camera: point ext at the demo history and put it on PATH.
Hide
Type "export EXTENDO_STORE_DIR=$PWD/store"
Enter
Type "export PATH=$PWD/bin:$PATH"
Enter
Type "export NO_COLOR="
Enter
Type "clear"
Enter
Sleep 500ms
Show

# ── the picker opens ────────────────────────────────────────────────
Type "ext"
Sleep 600ms
Enter
Sleep 2.5s

# ── moving the cursor ───────────────────────────────────────────────
Down
Sleep 500ms
Down
Sleep 500ms
Down
Sleep 1.2s

# ── an image preview ────────────────────────────────────────────────
Enter
Sleep 3s
Escape
Sleep 800ms

# ── a secret that declines to be one ────────────────────────────────
Down
Sleep 1.2s
Type "s"
Sleep 3s
Down
Sleep 1.5s

# ── the JWT: revealed, then previewed so the whole token is on screen
Down
Down
Sleep 800ms
Type "s"
Sleep 1.8s
Enter
Sleep 4s
Escape
Sleep 800ms

# ── paging ──────────────────────────────────────────────────────────
Right
Sleep 2s

# a long note, to show the preview wrapping it
Down
Down
Down
Down
Sleep 1s
Enter
Sleep 3s
Escape
Sleep 600ms

# the second image
Down
Down
Down
Sleep 800ms
Enter
Sleep 2.5s
Escape
Sleep 600ms

Right
Sleep 2s
Left
Sleep 1s
Left
Sleep 1.5s

# ── search ──────────────────────────────────────────────────────────
Type "/"
Sleep 800ms
Type "key"
Sleep 2.5s
Escape
Sleep 1.2s

# ── out ─────────────────────────────────────────────────────────────
Type "q"
Sleep 1.5s
