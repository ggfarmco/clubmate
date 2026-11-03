PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

MAC_BOARDS="Agents-1280 Agents-640 Mac-AlwaysAllow x-Failed-Session-1280 Code-1280 Code-640 Mac-ChatGraphStep Backlog-1280 Backlog-640 Cron-1280 x-Triggers-1280 Triggers-640 Graphs-1280 Graphs-640 Processes-1280 Processes-640 x-Palette x-Switcher x-Worktree-Picker x-Inbox-popover x-Approval-Sheet Mac-Settings-Remote x-Title-Dot-1280"
PHONE_BOARDS="C-Macs C-Macs-Light C-Workspace C-Workspace-Light C-Permission C-Permission-Light C-Question C-Question-Light C-Inbox C-Inbox-Light C-MacInfo C-MacInfo-Light C-Backlog C-Backlog-Light C-Offline C-iPad"

fail() {
  echo "$(basename "$0"): $*" >&2
  exit 1
}

main_checkout() {
  local common
  common="$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)" || fail "$PWD is not in a git repo"
  dirname "$common"
}

run_key() {
  local key="${CLUBMATE_GRAPH_RUN:-${DESIGN_CHECK_RUN:-manual-$(today)}}"
  key="$(printf '%s' "$key" | tr 'A-Z_' 'a-z-' | tr -cd 'a-z0-9.-')"
  [[ -n "$key" ]] || fail "empty run key"
  printf '%s' "$key"
}

today() {
  printf '%s' "${DESIGN_CHECK_TODAY:-$(date +%F)}"
}

check_root() {
  printf '%s' "$(main_checkout)/.worktrees/design-check"
}

run_dir() {
  printf '%s/%s' "$(check_root)" "$(run_key)"
}

instance_name() {
  printf 'dc-%s' "$(run_key)" | cut -c1-64
}

sim_name() {
  printf 'cm-design-%s' "$(run_key)"
}

known_board() {
  case " $MAC_BOARDS $PHONE_BOARDS " in
    *" $1 "*) return 0 ;;
  esac
  return 1
}

mac_board() {
  case " $MAC_BOARDS " in
    *" $1 "*) return 0 ;;
  esac
  return 1
}

strip_clubmate_env() {
  local v
  for v in $(env | grep -o '^CLUBMATE_[A-Z0-9_]*' || true); do
    unset "$v"
  done
}
