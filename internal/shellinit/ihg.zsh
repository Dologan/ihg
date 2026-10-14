# ihg shell integration for zsh. Load it from ~/.zshrc with:
#
#   eval "$(ihg init zsh)"
#
# `ihg [query]` opens the picker. Enter runs the chosen command in *this*
# shell (so aliases, functions, cd, source and export behave as if typed) and
# records it in history; Tab places it on the command line for editing.
# Set IHG_BIND_CTRL_R=1 before the eval to also bind Ctrl-R to the picker.

# Run the ihg binary and leave its answer in __ihg_action / __ihg_cmd.
# emulate -L keeps this helper's own code predictable, but the user's command
# is eval'd by the callers, outside it, under the user's own options.
__ihg_pick() {
  emulate -L zsh
  local out content rc
  typeset -g __ihg_action=none __ihg_cmd=
  out=$(command mktemp "${TMPDIR:-/tmp}/ihg.XXXXXX") || return 1
  # Flush this session's new entries so the picker sees them.
  builtin fc -AI 2>/dev/null
  IHG_HISTFILE=${IHG_HISTFILE:-$HISTFILE} IHG_SHELL=zsh IHG_OUT=$out command ihg "$@"
  rc=$?
  content=$(<$out)
  command rm -f -- $out
  if [[ $content == *$'\n'* ]]; then
    __ihg_action=${content%%$'\n'*}
    __ihg_cmd=${content#*$'\n'}
  elif [[ -n $content ]]; then
    __ihg_action=$content
  fi
  return rc
}

ihg() {
  typeset -g __ihg_stdin=
  if [[ ! -t 0 ]]; then
    # Piped input (`cat file | ihg`): keep it for the chosen command.
    __ihg_stdin=$(command mktemp "${TMPDIR:-/tmp}/ihg-in.XXXXXX") || return 1
    command cat >| $__ihg_stdin
  fi
  __ihg_pick "$@"
  typeset -g __ihg_rc=$?
  case $__ihg_action in
    run)
      print -rs -- "$__ihg_cmd"
      if [[ -n $__ihg_stdin ]]; then
        eval "$__ihg_cmd" < $__ihg_stdin
      else
        eval "$__ihg_cmd"
      fi
      __ihg_rc=$?
      ;;
    edit)
      print -rz -- "$__ihg_cmd"
      ;;
  esac
  [[ -n $__ihg_stdin ]] && command rm -f -- $__ihg_stdin
  return $__ihg_rc
}

# ZLE widget: the current line seeds the query; the shell itself runs the
# result via accept-line, exactly as if it had been typed.
ihg-widget() {
  __ihg_pick -- "$LBUFFER"
  case $__ihg_action in
    run|edit)
      BUFFER=$__ihg_cmd
      CURSOR=$#BUFFER
      ;;
  esac
  zle reset-prompt
  [[ $__ihg_action == run ]] && zle accept-line
  return 0
}
zle -N ihg-widget

if [[ -n ${IHG_BIND_CTRL_R:-} ]]; then
  bindkey -M emacs '^R' ihg-widget
  bindkey -M viins '^R' ihg-widget
  bindkey -M vicmd '^R' ihg-widget
fi
