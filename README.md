# dotfiles

My configs for **ghostty**, **herdr** (CLI + Herdr app) and **vim**, plus the fonts they use,
managed by `dot` — a small TUI that syncs this repo with the current machine.

## Use

```sh
dot            # TUI
dot status     # what differs
dot sync       # apply / import / merge everything without conflicts
dot install    # new machine: brew bundle + fonts + apply repo
dot fonts      # fonts referenced by the configs
```

In the TUI every file shows one of:

| | status | `s` (sync) does |
|---|---|---|
| ✓ | in sync | nothing |
| ↓ | repo only / repo newer | copy repo → machine |
| ↑ | machine only / machine newer | copy machine → repo |
| ⇅ | both changed | three-way merge; conflicts open `$EDITOR` |

Keys: `enter` diff · `s` sync · `p` force repo→machine · `i` force machine→repo · `m` merge ·
`S` sync all tools · `f` fonts · `c` commit · `q` quit.

"Newer" is judged against a snapshot of the last sync kept per machine in
`~/.local/state/dotfiles/base/`, so dot knows which side changed and merges
three-way. Anything it overwrites on the machine is first copied to
`~/.local/state/dotfiles/backup/<timestamp>/`.

Files are **copied**, not symlinked, so apps and generators that rewrite their
config (iglass, SpookiUI, Herdr's settings UI) never break anything — the
change just shows up as "machine newer" next time.

## New machine

```sh
curl -fsSL https://raw.githubusercontent.com/gabalese/dotfiles/main/bootstrap.sh | bash -s -- https://github.com/gabalese/dotfiles.git
```

Installs Command Line Tools, Homebrew and Go, clones to `~/Projects/dotfiles`,
builds `bin/dot`, links it into `~/.local/bin`, and runs `dot install`.

## Adding things

- **A tool**: add a `[tools.<name>]` section to `tools.toml` with its `paths`,
  then open `dot` and sync — the machine's files are imported.
- **A path for an existing tool**: append to its `paths`.
- **A font**: the fonts screen (`f`) lists fonts a config references but
  `fonts/fonts.toml` doesn't; press `a` to add it (uses a Homebrew cask when
  one exists, otherwise copies the installed files into `fonts/files/`).
- **An app to install**: `Brewfile`.

## Build

```sh
go build -o bin/dot .
```
