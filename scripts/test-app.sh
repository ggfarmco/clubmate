#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEFAULT_APP="$ROOT/build/DerivedData/Build/Products/Debug/Clubmate Dev.app"

usage() {
  cat >&2 <<EOF
usage: $0 start [name] [app] [-- app args]   start an invisible test instance
       $0 cm [name] -- <cm args>             run the CLI against it
       $0 stop [name]                        quit it and remove its dirs
       $0 list                               running instances
name defaults to the current branch; app to the Debug build of this checkout
EOF
  exit 2
}

default_name() {
  git -C "$ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null | tr '/A-Z' '-a-z' | tr -cd 'a-z0-9._-' | cut -c1-64
}

check_name() {
  [[ "$1" =~ ^[a-z0-9][a-z0-9._-]{0,63}$ && "$1" != *..* ]] || { echo "invalid instance name: $1" >&2; exit 2; }
}

dir_of() { echo "/tmp/clubmate-test-$1"; }

private_dir() {
  local dir="$1"
  [[ -d "$dir" && ! -L "$dir" && "$(stat -f %u "$dir")" == "$(id -u)" && "$(stat -f %Lp "$dir")" == 700 ]]
}

cli() {
  local dir="$1"; shift
  CLUBMATE_STATE_DIR="$dir/state" CLUBMATE_RUNTIME_DIR="$dir/run" "$(cat "$dir/cli")" "$@"
}

alive() {
  local dir="$1"
  private_dir "$dir" && [[ -S "$dir/run/core.sock" && -f "$dir/cli" ]] && cli "$dir" core status >/dev/null 2>&1
}

app_pid() {
  local dir="$1"
  ps -E -axo pid=,command= | grep "/Contents/MacOS/Clubmate Dev .*CLUBMATE_RUNTIME_DIR=$dir/run" | grep -v grep | awk '{print $1}' || true
}

start() {
  local name="" app="$DEFAULT_APP"
  [[ $# -gt 0 && "$1" != "--" ]] && { name="$1"; shift; }
  [[ $# -gt 0 && "$1" != "--" ]] && { app="$1"; shift; }
  [[ $# -gt 0 && "$1" == "--" ]] && shift
  name="${name:-$(default_name)}"
  check_name "$name"
  [[ -d "$app" ]] || { echo "app not found: $app (run make app)" >&2; exit 1; }
  local dir; dir="$(dir_of "$name")"
  if alive "$dir"; then echo "instance $name is already running" >&2; exit 1; fi
  if [[ -e "$dir" || -L "$dir" ]]; then
    private_dir "$dir" || { echo "$dir exists and is not a private directory of $(id -un)" >&2; exit 1; }
    rm -rf "$dir"
  fi
  mkdir -m 700 "$dir"
  mkdir -p "$dir"/{state,run,xdg/config,xdg/data,xdg/state,xdg/cache}
  chmod 700 "$dir/run"
  echo "$app/Contents/Resources/bin/clubmate" > "$dir/cli"
  local envs=(
    --env CLUBMATE_TEST_MODE=1
    --env "CLUBMATE_STATE_DIR=$dir/state"
    --env "CLUBMATE_RUNTIME_DIR=$dir/run"
    --env "XDG_CONFIG_HOME=$dir/xdg/config"
    --env "XDG_DATA_HOME=$dir/xdg/data"
    --env "XDG_STATE_HOME=$dir/xdg/state"
    --env "XDG_CACHE_HOME=$dir/xdg/cache"
  )
  local passthrough=()
  for v in $(env | grep -o '^CLUBMATE_AGENTS_COMMAND\|^FAKEAGENT_[A-Z_]*' || true); do passthrough+=(--env "$v=${!v}"); done
  env -i HOME="$HOME" USER="$USER" LOGNAME="${LOGNAME:-$USER}" PATH="/usr/bin:/bin:/usr/sbin:/sbin" TMPDIR="${TMPDIR:-/tmp}" \
    open -n -g -j "${envs[@]}" ${passthrough[@]+"${passthrough[@]}"} "$app" --args -ApplePersistenceIgnoreState YES "$@"
  for _ in $(seq 1 100); do
    alive "$dir" && break
    sleep 0.2
  done
  alive "$dir" || { echo "instance $name: core did not start" >&2; exit 1; }
  for _ in $(seq 1 50); do
    cli "$dir" ui sections >/dev/null 2>&1 && break
    sleep 0.2
  done
  cli "$dir" workspace open "$ROOT" >/dev/null
  echo "instance $name"
  echo "export CLUBMATE_STATE_DIR=$dir/state CLUBMATE_RUNTIME_DIR=$dir/run"
  echo "snapshots: $0 cm $name -- ui snapshot --out $dir/snapshots"
}

stop() {
  local name="${1:-$(default_name)}"
  check_name "$name"
  local dir; dir="$(dir_of "$name")"
  [[ -d "$dir" ]] || { echo "no instance $name" >&2; exit 1; }
  private_dir "$dir" || { echo "$dir is not a private directory of $(id -un); not touching it" >&2; exit 1; }
  local pid; pid="$(app_pid "$dir" || true)"
  [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
  alive "$dir" && cli "$dir" core stop >/dev/null 2>&1 || true
  local sessions
  sessions="$(ps -E -axo pid=,command= | grep "clubmate-session .*CLUBMATE_RUNTIME_DIR=$dir/run" | grep -v grep | awk '{print $1}' || true)"
  [[ -n "$sessions" ]] && kill $sessions 2>/dev/null || true
  for _ in $(seq 1 25); do
    [[ -S "$dir/run/core.sock" ]] || break
    sleep 0.2
  done
  defaults delete "com.clubmate.app.dev.test.clubmate-test-$name" >/dev/null 2>&1 || true
  rm -rf "$dir"
  echo "stopped $name"
}

list() {
  for dir in /tmp/clubmate-test-*; do
    [[ -d "$dir" ]] || continue
    alive "$dir" && echo "${dir#/tmp/clubmate-test-}"
  done
}

cm() {
  local name=""
  [[ $# -gt 0 && "$1" != "--" ]] && { name="$1"; shift; }
  [[ $# -gt 0 && "$1" == "--" ]] && shift
  name="${name:-$(default_name)}"
  check_name "$name"
  local dir; dir="$(dir_of "$name")"
  alive "$dir" || { echo "instance $name is not running" >&2; exit 1; }
  cli "$dir" "$@"
}

[[ $# -ge 1 ]] || usage
cmd="$1"; shift
case "$cmd" in
  start) start "$@" ;;
  stop) stop "$@" ;;
  list) list ;;
  cm) cm "$@" ;;
  *) usage ;;
esac
