#!/bin/bash
# Set up a new Mac:  bash bootstrap.sh <git-url-of-this-repo>
set -euo pipefail
REPO_URL="${1:-${DOTFILES_REPO:-}}"
DEST="${DOTFILES_DIR:-$HOME/Projects/dotty}"

if ! xcode-select -p >/dev/null 2>&1; then
  xcode-select --install || true
  echo "Install the Command Line Tools (dialog opened), then re-run this script."; exit 1
fi
if ! command -v brew >/dev/null 2>&1; then
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
fi
eval "$(/opt/homebrew/bin/brew shellenv 2>/dev/null || /usr/local/bin/brew shellenv)"

if [ ! -d "$DEST/.git" ]; then
  [ -n "$REPO_URL" ] || { echo "usage: bootstrap.sh <repo-url>"; exit 1; }
  mkdir -p "$(dirname "$DEST")"; git clone "$REPO_URL" "$DEST"
fi
command -v go >/dev/null 2>&1 || brew install go
cd "$DEST" && go build -o bin/dotty .
mkdir -p "$HOME/.local/bin" && ln -sf "$DEST/bin/dotty" "$HOME/.local/bin/dotty"

if [ ! -d "$HOME/.oh-my-zsh" ]; then
  # KEEP_ZSHRC: dotty install brings the real .zshrc down from the repo
  RUNZSH=no CHSH=no KEEP_ZSHRC=yes sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)"
fi

"$DEST/bin/dotty" install
echo; echo "Done. Run 'dotty' (add ~/.local/bin to PATH) to review and merge anything left."
