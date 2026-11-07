# CLAUDE.md

Personal dotfiles (ghostty, herdr, vim) plus `dot`, a Go TUI that syncs them
between this repo and the current Mac. Public repo: github.com/gabalese/dotfiles.

## Layout

- `tools.toml` — registry of supported tools: `paths` (relative to `$HOME`),
  `ignore` globs, `fonts` regexes. Adding a tool should never need Go changes.
- `packages/<tool>/<path>` — repo copy of `$HOME/<path>`.
- `fonts/fonts.toml` — fonts the configs use (`cask`, `files` → `fonts/files/<dir>/`, or `builtin`).
- `Brewfile` — apps/tools only; fonts are deliberately *not* here.
- `bootstrap.sh` — new machine: CLT, Homebrew, Go, clone, build, `dot install`.
- Go (single `package main`): `config.go` (loading, ignore matching), `sync.go`
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
fake `EDITOR` script to resolve conflicts.

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

## Open items

- Third-party files in `packages/ghostty` (SpookiUI shaders, `cursor_warp.glsl`,
  iglass and ayu themes) have no license headers; consider excluding them and
  regenerating via `iglass install` / SpookiUI in `bootstrap.sh`.
- No vim config exists yet on the main machine.
- On this Mac, Ghostty and Herdr.app weren't installed via Homebrew, so use
  `dot install --no-brew` here.

## Conventions

- This is a public repo: scan for secrets/emails/absolute home paths before committing.
- Commit with the TUI's `c` (message "Sync settings from <host>") for config
  changes; write normal messages for code changes.
