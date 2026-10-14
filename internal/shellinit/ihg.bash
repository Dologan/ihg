# ihg shell integration for bash. Load it from ~/.bashrc with:
#
#   eval "$(ihg init bash)"
#
# `ihg [query]` opens the picker. Enter runs the chosen command in *this*
# shell (so aliases, functions, cd, source and export behave as if typed) and
# records it in history; Tab lets you edit it first. Set IHG_BIND_CTRL_R=1
# before the eval to also bind Ctrl-R to the picker (needs bash 4 or newer).

# Run the ihg binary and leave its answer in __ihg_action / __ihg_cmd.
__ihg_pick() {
  local out content rc
  __ihg_action=none
  __ihg_cmd=
  out=$(command mktemp "${TMPDIR:-/tmp}/ihg.XXXXXX") || return 1
  # Flush this session's new entries so the picker sees them.
  builtin history -a 2>/dev/null
  IHG_HISTFILE="${IHG_HISTFILE:-${HISTFILE:-$HOME/.bash_history}}" IHG_SHELL=bash \
    IHG_OUT="$out" command ihg "$@"
  rc=$?
  content=$(<"$out")
  command rm -f -- "$out"
  if [[ $content == *$'\n'* ]]; then
    __ihg_action=${content%%$'\n'*}
    __ihg_cmd=${content#*$'\n'}
  elif [[ -n $content ]]; then
    __ihg_action=$content
  fi
  return "$rc"
}

ihg() {
  __ihg_stdin=
  if [[ ! -t 0 ]]; then
    # Piped input (`cat file | ihg`): keep it for the chosen command.
    __ihg_stdin=$(command mktemp "${TMPDIR:-/tmp}/ihg-in.XXXXXX") || return 1
    command cat >|"$__ihg_stdin"
  fi
  __ihg_pick "$@"
  __ihg_rc=$?
  if [[ $__ihg_action == edit ]]; then
    if ((BASH_VERSINFO[0] >= 4)); then
      # Readline-edit the command, then run it as if typed.
      IFS= read -r -e -i "$__ihg_cmd" -p "${PS2:-> }" __ihg_cmd || __ihg_action=none
      [[ -n $__ihg_cmd && $__ihg_action != none ]] && __ihg_action=run
    else
      # bash 3.2 cannot pre-fill read -e: queue it in history for Up-arrow.
      builtin history -s -- "$__ihg_cmd"
      printf 'ihg: press Up to edit the command\n' >&2
      __ihg_action=none
    fi
  fi
  if [[ $__ihg_action == run ]]; then
    builtin history -s -- "$__ihg_cmd"
    if [[ -n $__ihg_stdin ]]; then
      eval "$__ihg_cmd" <"$__ihg_stdin"
    else
      eval "$__ihg_cmd"
    fi
    __ihg_rc=$?
  fi
  [[ -n $__ihg_stdin ]] && command rm -f -- "$__ihg_stdin"
  return "$__ihg_rc"
}

# Readline widget (bash >= 4). bind -x can set the line but cannot run it, so
# Ctrl-R expands to two hidden keys: the first runs the picker, the second is
# rebound by the picker to accept-line (run) or a harmless redraw (edit/none).
__ihg_widget() {
  __ihg_pick -- "$READLINE_LINE"
  local accept=redraw-current-line
  case $__ihg_action in
    run|edit)
      READLINE_LINE=$__ihg_cmd
      READLINE_POINT=${#READLINE_LINE}
      [[ $__ihg_action == run ]] && accept=accept-line
      ;;
  esac
  bind -m emacs-standard "\"\\C-x\\C-]a\": $accept"
  bind -m vi-insert "\"\\C-x\\C-]a\": $accept"
}

if [[ -n ${IHG_BIND_CTRL_R:-} ]]; then
  if ((BASH_VERSINFO[0] >= 4)); then
    for __ihg_map in emacs-standard vi-insert; do
      bind -m "$__ihg_map" -x '"\C-x\C-]p": __ihg_widget'
      bind -m "$__ihg_map" '"\C-x\C-]a": redraw-current-line'
      bind -m "$__ihg_map" '"\C-r": "\C-x\C-]p\C-x\C-]a"'
    done
    unset __ihg_map
  else
    printf 'ihg: Ctrl-R binding needs bash 4+ (this is %s); use the ihg command\n' \
      "$BASH_VERSION" >&2
  fi
fi
