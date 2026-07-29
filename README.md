# extendo-cli

`ext` — a terminal picker for your clipboard history. Press a number, the item
is back on your pasteboard, and you are back at the prompt.

## install

```sh
brew install tjq/tap/ext
```

Or build it:

```sh
go build -o ext ./cmd/ext
```

macOS only.

## ctrl-G

```sh
ext install
```

Binds ctrl-G to the picker, so it opens over whatever you are typing: pick
something and the prompt is redrawn with the item on the pasteboard. Open a
new terminal afterwards, or `source` the file it names.

For a different chord:

```sh
ext install --key ctrl-t
```

`ctrl-t`, `^T` and `t` all name the same one. It binds ctrl plus a letter,
and refuses the few the terminal needs for itself — `ctrl-c`, `ctrl-d`,
`ctrl-z` and the rest — rather than leaving you without an interrupt.

It appends a managed block to `~/.zshrc` (zsh) or `~/.bash_profile` (bash),
between `# >>> extendo-cli >>>` markers. Re-running replaces the block rather
than adding a second, so it is safe after every upgrade, and

```sh
ext install --uninstall
```

takes it back out and leaves the rest of the file alone. `--profile <file>`
names a different file. Everything except ctrl-G works without the block.

## keys

The picker shows ten items a page, labelled `1..9` then `0`.

| key | action |
|---|---|
| `1`–`9`, `0` | copy that row and exit — `0` is the tenth |
| `c` | copy the highlighted row and exit |
| `↑` `↓` / `k` `j` | move the cursor |
| `←` `→` / `h` `l` | flip a page |
| `p` | pin or unpin the highlighted item |
| `d` | delete it, permanently |
| `s` | reveal a masked credential; again to hide it |
| `⏎` / `tab` | preview the highlighted item — any key returns |
| `/` | search; `⏎` keeps the filter and hands the number row back, `esc` clears it |
| `q` / `esc` | quit — `esc` clears an active filter first |
| `ctrl+c` | quit from anywhere, including the preview and the query field |

The number row acts on the row it names wherever the cursor is, so the common
case is one keystroke.

## commands

| command | |
|---|---|
| `ext` | the picker on a terminal; the `list` table when the output is piped |
| `ext list` | table of the history; `--json` for an array meant for scripts |
| `ext get <n\|id>` | copy an item; `--print` writes its text to stdout instead |
| `ext pin <n\|id>` | toggle the pin |
| `ext delete <n\|id>` | remove it (aliased `rm`) |
| `ext version` | print the version |

`<n>` is the number the list prints — pinned first, then newest — and `<id>` is
any unambiguous prefix of an item's id.

```sh
ext list
ext get 3          # back on the pasteboard
ext get 3 --print  # to stdout, byte for byte
ext list --json | jq -r '.[] | select(.pinned) | .label'
```

Confirmations go to stderr and the payload to stdout, so `ext get 3 --print`
pipes cleanly.

## flags

```sh
ext --ascii    # txt / img / fil stand-ins for the glyph columns
ext --nerd     # Nerd Font icons; needs a patched font
```

`EXTENDO_ASCII=1` and `EXTENDO_NERD=1` do the same. `NO_COLOR` turns colour
off, and `EXTENDO_STORE_DIR` points `ext` at a different history directory.
