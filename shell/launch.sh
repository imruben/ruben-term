#!/bin/sh
# ruben-term · starts the shell of a tab with ruben-term's own config.
# Managed by the app (rewritten when it starts): your changes go in zsh/local.zsh.
#
# zsh gets ZDOTDIR pointed here, so it never reads ~/.zshrc on its own: that is what
# keeps this terminal apart from whatever ~/.zshrc sets up for the other terminals.

RT="$HOME/.config/ruben-term"

export RUBEN_TERM=1 TERM_PROGRAM=ruben-term COLORTERM=truecolor
case ${TERM:-dumb} in dumb) export TERM=xterm-256color ;; esac
unset TMUX TMUX_PANE

# Started from a shell that terminal-setup configured (not the usual case): drop what
# it exported, so nothing here points back into it.
if [ -n "$TERMINAL_SETUP" ]; then
  case ${BROWSER:-} in "$TERMINAL_SETUP"/*) unset BROWSER ;; esac
  case ${STARSHIP_CONFIG:-} in "$TERMINAL_SETUP"/*) unset STARSHIP_CONFIG ;; esac
  unset TERMINAL_SETUP TERMINAL_AUTOSTARTED
  unset FZF_DEFAULT_OPTS FZF_CTRL_T_OPTS FZF_ALT_C_OPTS FZF_DEFAULT_COMMAND FZF_CTRL_T_COMMAND FZF_ALT_C_COMMAND
fi

# the folder the tab opens in (falls back to home when it is gone)
if [ -n "$RUBEN_TERM_CWD" ] && [ -d "$RUBEN_TERM_CWD" ]; then
  cd "$RUBEN_TERM_CWD" || cd "$HOME" || :
fi
unset RUBEN_TERM_CWD

sh=${RUBEN_TERM_SHELL:-$SHELL}
if [ -z "$sh" ] || ! command -v "$sh" >/dev/null 2>&1; then
  sh=$(command -v zsh || command -v bash || echo /bin/sh)
fi
export SHELL="$sh"

case ${sh##*/} in
  zsh)
    RUBEN_TERM_USER_ZDOTDIR="${ZDOTDIR:-$HOME}"     # where your own .zshrc lives
    ZDOTDIR="$RT/zsh"
    export RUBEN_TERM_USER_ZDOTDIR ZDOTDIR
    exec "$sh" -l
    ;;
  bash)
    exec "$sh" --rcfile "$RT/bash/bashrc" -i
    ;;
  *)
    exec "$sh" -l
    ;;
esac
