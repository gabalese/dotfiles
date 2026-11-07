# Pure prompt (https://github.com/sindresorhus/pure), sourced from ~/.zshrc.
# Settings are zstyles and PURE_* variables; they must be set before `prompt pure`.

# PURE_PROMPT_SYMBOL='❯'
# PURE_CMD_MAX_EXEC_TIME=5                 # seconds before showing elapsed time
# PURE_GIT_PULL=1                          # 0 = don't check for upstream changes
# zstyle :prompt:pure:git:stash show yes   # show ≡ when there are stashes
# zstyle :prompt:pure:path color blue
# zstyle :prompt:pure:prompt:success color magenta

fpath+=("$(brew --prefix)/share/zsh/site-functions")
autoload -U promptinit; promptinit
prompt pure
