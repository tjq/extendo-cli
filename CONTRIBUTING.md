# contributing

```sh
go build -o ext ./cmd/ext
go test ./...
go vet ./...
```

Go 1.24, macOS. The TUI tests drive the picker through `teatest` and compare
against golden frames in `internal/tui/testdata`; regenerate them with
`go test ./internal/tui/ -update` after a deliberate layout change, and read
the diff before committing it.

## layout

```
cmd/ext/            cobra commands, one file each
internal/store/     history.json: read, sort, atomic mutate
internal/secrets/   credential classification and masking
internal/clip/      pasteboard write-back via pbcopy/osascript
internal/shell/     the managed profile block
internal/tui/       the Bubble Tea picker
```

## releasing

Pushing a `v*` tag is the whole ritual. `.github/workflows/release.yml` hands
the tag to GoReleaser, which runs the tests, builds darwin `amd64` and
`arm64`, attaches the archives to a GitHub release, and commits the cask to
[tjq/homebrew-tap](https://github.com/tjq/homebrew-tap).

```sh
git tag -a v0.1.0 -m v0.1.0
git push origin v0.1.0
```

Version numbers come from the tag: the build stamps `main.version` with
`-ldflags`, and an unstamped binary reports `dev`.

### the tap token

Two secrets are in play, and only one is free. `GITHUB_TOKEN` is supplied by
Actions and publishes the release. `HOMEBREW_TAP_GITHUB_TOKEN` is not — the
default token is scoped to this repository alone and cannot write to the tap,
so the workflow needs a fine-grained personal access token with **Contents:
read and write** on `tjq/homebrew-tap`, stored as a repository secret of that
name.

```sh
gh secret set HOMEBREW_TAP_GITHUB_TOKEN --repo tjq/extendo-cli
```

Fine-grained tokens expire — a year at most. When one lapses the release still
*succeeds*: the GitHub release publishes and only the tap commit fails, so
`brew install` quietly keeps serving the previous version. Worth a calendar
reminder at the expiry date.

Deleting and recreating the repository also drops the secret, since Actions
secrets belong to the repository rather than the account.

### rehearsing

To exercise the whole pipeline without tagging or publishing anything:

```sh
goreleaser release --snapshot --clean
```

Binaries land in `dist/`, and the cask that would have been pushed is written
to `dist/homebrew/Casks/ext.rb` for reading.

### notes on the cask

It is a cask rather than a formula on two counts. The release binaries are
unsigned, and a cask can strip `com.apple.quarantine` in a `postflight` so the
first run does not die with "developer cannot be verified"; and the tap keeps
a `Casks` directory already. Drop that hook once the builds are signed.

Builds are darwin-only on purpose. The picker reads the history under
`~/Library`, so a linux build would compile cleanly and then never find a
store to open.

### push protection

GitHub's secret scanning rejects pushes containing credential-shaped strings,
and `internal/secrets` is a package full of them by nature. Test vectors are
concatenated rather than written whole (`"sk_live_" + strings.Repeat(...)`) so
no complete key literal sits in the source. Keep new ones in that style — the
alternative is permanently allow-listing a "secret" on the repository.
