# dotty

My configs for **ghostty**, **herdr** (CLI + Herdr app), **vim** and **zsh** (oh-my-zsh, pure, oh-my-posh),
plus the apps and fonts they use,
managed by `dotty` — a small TUI that syncs this repo with the current machine.

## Supported tools

| Tool | Configuration tracked (under `~`) | Installs |
|---|---|---|
| `ghostty` — Ghostty terminal | `.config/ghostty/` (config, themes, shaders) | cask `ghostty`; brew `iglass`, `spookiui` |
| `herdr` — herdr + Herdr app | `.config/herdr/config.toml`, `.config/herdr-app/{settings,keys}.json` | brew `herdr`; cask `herdr-app` |
| `corgi` — Corgi agent terminal | `.local/state/corgi/{settings,viewer}.json` | — (Corgi installs itself) |
| `sublime-text` — Sublime Text | `Library/Application Support/Sublime Text/Packages/User/`, `…/Installed Packages/Quiet Writer.sublime-package` | cask `sublime-text` |
| `vim` — Vim / Neovim | `.vimrc`, `.gvimrc`, `.vim/`, `.config/nvim/` (not plugins, undo or swap files) | — |
| `zsh` — zsh startup files | `.zshrc`, `.zprofile`, `.zshenv`, `.zlogin` | brew `zsh-syntax-highlighting`, `autojump` |
| `oh-my-zsh` — custom plugins, themes, snippets | `.oh-my-zsh/custom/` (the framework itself comes from `bootstrap.sh`) | — |
| `oh-my-posh` — Oh My Posh prompt themes | `.config/oh-my-posh/` | brew `oh-my-posh` |
| `pure` — Pure zsh prompt | `.config/zsh/pure.zsh` (brings `zsh` along) | brew `pure` |

The list lives in [`tools.toml`](tools.toml); the fonts the configs use are in
[`fonts/fonts.toml`](fonts/fonts.toml).

## Use

```sh
dotty            # TUI
dotty status     # what differs
dotty sync       # apply / import / merge everything without conflicts
dotty merge      # sync conflicts too: word-level merge, then one side wins (--prefer machine|repo)
dotty apply      # overwrite this machine's files with the repo's (dotty apply ghostty for one tool)
dotty restore    # list backups of replaced machine files; dotty restore <timestamp>|latest puts one back
dotty install    # new machine: missing packages + fonts + apply repo
dotty install ghostty zsh    # …only for these tools  (--skip herdr, --no-brew, --no-fonts)
dotty install --pick         # tick/untick tools in the TUI first
dotty packages   # Homebrew packages the tools need, and which are missing
dotty fonts      # fonts referenced by the configs
```

In the TUI every file shows one of:

| | status | `s` (sync) does |
|---|---|---|
| ✓ | in sync | nothing |
| ↓ | repo only / repo newer | copy repo → machine |
| ↑ | machine only / machine newer | copy machine → repo |
| ⇅ | both changed | three-way merge; conflicts open `$EDITOR` |

Keys, on a file: `enter` diff · `s` sync · `p` force repo→machine · `i` force machine→repo ·
`m` merge in `$EDITOR` · `w` magic merge.
On the tool list: `s`/`S` sync the tool / everything · `w`/`W` magic merge · `p`/`P` overwrite
the machine with the repo (asks first) · `o` switch who wins a magic-merge tie · `b` backups ·
`i` install · `f` fonts · `c` commit · `q` quit.

**Magic merge** (`w`, or `dotty merge`) never asks you to review anything. Changes
from both sides are merged; lines both sides touched are merged again word by
word, so `font-size = 16 # small` and `font-size = 14 # readable` become
`font-size = 16 # readable`. Only where both sides changed the *same words* does
one side win — the machine by default (`o`, or `--prefer repo`). Binary files,
and JSON/TOML files a merge would leave unparseable, are taken whole from that side.

**Backups** (`b`, or `dotty restore`) lists every timestamped set of files dotty
replaced on this machine; `enter` (or `dotty restore <timestamp>`) puts one back.
A restore backs up what it replaces, so it can be undone the same way.

The install screen (`i`, or `dotty install --pick`) lists every tool with what it
still needs (missing packages, config files to apply). `space` ticks/unticks a
tool, `a`/`n` all/none, `b`/`f` toggle packages/fonts, `enter` runs
`dotty install` for the ticked tools in the foreground.

"Newer" is judged against a snapshot of the last sync kept per machine in
`~/.local/state/dotfiles/base/`, so dotty knows which side changed and merges
three-way. Anything it overwrites on the machine is first copied to
`~/.local/state/dotfiles/backup/<timestamp>/`.

Files are **copied**, not symlinked, so apps and generators that rewrite their
config (iglass, SpookiUI, Herdr's settings UI) never break anything — the
change just shows up as "machine newer" next time.

## New machine

```sh
curl -fsSL https://raw.githubusercontent.com/gabalese/dotty/main/bootstrap.sh | bash -s -- https://github.com/gabalese/dotty.git
```

Installs Command Line Tools, Homebrew and Go, clones to `~/Projects/dotty`,
builds `bin/dotty`, links it into `~/.local/bin`, installs Oh My Zsh, and runs `dotty install`.

## Adding things

- **A tool**: add a `[tools.<name>]` section to `tools.toml` with its `paths`
  (and the `brew`/`cask` packages it needs), then open `dotty` and sync — the
  machine's files are imported.
- **A path for an existing tool**: append to its `paths`.
- **A font**: the fonts screen (`f`) lists fonts a config references but
  `fonts/fonts.toml` doesn't; press `a` to add it (uses a Homebrew cask when
  one exists, otherwise copies the installed files into `fonts/files/`).
- **An app to install**: add it to the owning tool's `brew` or `cask` list.
  `dotty install` installs only what's missing — an app already in
  `/Applications` or a binary on `PATH` counts as installed, even if it didn't
  come from Homebrew.

## Build

```sh
go build -o bin/dotty .
```
