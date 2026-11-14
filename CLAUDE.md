# CLAUDE.md

Personal dotfiles (ghostty, herdr, vim, zsh/oh-my-zsh/pure/oh-my-posh) plus `dot`, a Go TUI that syncs them
between this repo and the current Mac. Public repo: github.com/gabalese/dotfiles.

## Layout

- `tools.toml` — registry of supported tools: `paths` (relative to `$HOME`),
  `ignore` globs, `fonts` regexes, `brew`/`cask` packages. Adding a tool should
  never need Go changes.
- `packages/<tool>/<path>` — repo copy of `$HOME/<path>`.
- `fonts/fonts.toml` — fonts the configs use (`cask`, `files` → `fonts/files/<dir>/`, or `builtin`).
- Packages: `dot install` brew-installs each tool's `brew`/`cask` entries that
  are missing. Present = in `brew list`, a same-named binary on `PATH`, or the
  cask's `.app` (from `brew info --json=v2`) in `/Applications`. There is no
  Brewfile; fonts are in `fonts.toml`, not in the package lists.
- `dot install [tool…] [--skip a,b]` scopes packages, fonts (by the tools
  whose configs use them) and configs to a `Selection` (nil = all). The TUI's
  install screen (`i`, `dot install --pick`) re-execs `dot install <tools>` via
  `tea.ExecProcess` so brew output and sudo prompts are visible.
- `bootstrap.sh` — new machine: CLT, Homebrew, Go, clone, build, `dot install`.
- Go (single `package main`): `config.go` (loading, ignore matching), `packages.go` (brew
  detection/install), `sync.go`
  (status, apply/import/merge), `fonts.go`, `tui.go` (Bubble Tea v1), `main.go` (CLI).

## How sync works (keep these invariants)

- **Copies, never symlinks.** iglass, SpookiUI and Herdr's settings UI rewrite
  their files atomically, which breaks symlinks. An earlier symlink-based bash
  version was dropped for this reason.
- **Per-machine base snapshot** in `~/.local/state/dotfiles/base/<tool>/<path>`
  (outside the repo, never committed) records content at last sync. Status is
  computed repo vs machine vs base → `Synced / RepoOnly / RepoAhead / LocalOnly / LocalAhead / Diverged`.
- **Merge** = `git merge-file machine base repo`. With no base, the base is the
  lines both sides share (from `git diff -U999999`), so one-sided additions merge
  cleanly and only real disagreements conflict. Conflicts open `$EDITOR` via
  `tea.ExecProcess`; result is rejected if markers remain.
- Anything overwritten on the machine is first copied to
  `~/.local/state/dotfiles/backup/<timestamp>/`.
- `dot install` never imports machine-only files; it only brings the repo down
  and clean-merges. Conflicts are left for the TUI.

## Build & test

```sh
go vet ./... && go build -o bin/dot .     # bin/ is gitignored; ~/.local/bin/dot links to it
```

There are no unit tests yet. Test against a sandbox — **never against the real
`$HOME`** — by overriding all three locations:

```sh
HOME=$SANDBOX/home DOTFILES_DIR=$SANDBOX/repo XDG_STATE_HOME=$SANDBOX/state ./bin/dot status
```

Copy real configs into `$SANDBOX/home` and `tools.toml`/`fonts/` into
`$SANDBOX/repo`. The TUI can be driven headlessly with `/usr/bin/expect`, using a
fake `EDITOR` script to resolve conflicts. Keys sent quickly arrive merged ("jjj");
the TUI splits them, but pause ~0.2s between sends anyway.

## Ghostty specifics

- Only `~/.config/ghostty/config` is used. The macOS
  `~/Library/Application Support/com.mitchellh.ghostty/config.ghostty` was merged
  into it and removed (it loaded *after* the XDG file and silently overrode
  font-size and theme). Don't re-add that path to `tools.toml`.
- Later lines win for scalars; `font-family` and `custom-shader` accumulate.
  The `config-file` include (iglass) is applied after the file, so iglass's theme wins.
- Keep the `added by SpookiUI` block and the iglass `config-file` comment intact —
  those tools look for them.
- Verify any config edit preserves behaviour by diffing `ghostty +show-config`
  before and after.
- Paths use `~/` for portability; SpookiUI/iglass may rewrite them to absolute
  `/Users/<name>/…` — fix in the repo if they do.

## Herdr

`herdr` tracks `~/.config/herdr/config.toml` and Herdr.app's
`~/.config/herdr-app/{settings,keys}.json`. Everything else in `herdr-app/` is
shipped by the app (see `installed.json`) or runtime state (sockets, logs,
sessions) — don't track it. The user-made `control-panel` theme/layout/mod there
is untracked for now.

## Corgi

`corgi` tracks `~/.local/state/corgi/{settings,viewer}.json` (shell, model, theme,
keys). It keeps them under the state dir, next to per-machine data that must stay
untracked: `accounts.json`, `workspaces.json` (absolute paths), `memory/`,
`recovery/`, `crash/`, `limit-history/`. Corgi installs itself into
`~/.local/share/corgi`; there is no brew package.

## Sublime Text

`sublime-text` tracks `Packages/User` (settings, the `Adaptive.sublime-theme`
override) and the hand-made `Installed Packages/Quiet Writer.sublime-package`
colour scheme, both under `~/Library/Application Support/Sublime Text`. The
package is a zip: it syncs as a whole file and cannot be merged. Package Control
and anything it installs are not tracked. Sublime does not load `.sublime-package`
files placed directly in `Packages/`, so don't track copies there.

## Zsh

`zsh` tracks the startup files; `oh-my-zsh` tracks only `~/.oh-my-zsh/custom`
(the framework is installed by `bootstrap.sh`, not tracked). The prompt is
`pure`, which has no config file: its zstyles and `prompt pure` live in
`~/.config/zsh/pure.zsh` (tool `pure`), sourced from `.zshrc`. `oh-my-posh` is
installed but its init line in `.zshrc` is commented out, and
`~/.config/oh-my-posh` is registered for when a theme exists. Keep
the `added by SpookiUI` ssh alias and Docker Desktop block in `.zshrc`/`.zprofile`.

## Open items

- Third-party files in `packages/ghostty` (SpookiUI shaders, `cursor_warp.glsl`,
  iglass and ayu themes) have no license headers; consider excluding them and
  regenerating via `iglass install` / SpookiUI in `bootstrap.sh`.

## Conventions

- This is a public repo: scan for secrets/emails/absolute home paths before committing.
- Commit with the TUI's `c` (message "Sync settings" — no machine name, the repo is public) for config
  changes; write normal messages for code changes.
