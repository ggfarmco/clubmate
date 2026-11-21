#!/usr/bin/env bash
set -uo pipefail

usage() {
  cat >&2 <<USAGE
usage: $0 build                                   build the CompanionPerf scheme (-O, DEBUG fixtures)
       $0 run <tag> <latency ms> <jitter ms> <test>...   XCTest metrics + request log for the named NavigationPerfUITests tests;
                                                  testSwipeBackSlow/testSwipeBackFast/testBackButton/testScrollStreaming/testListScrollRefreshing also print dropped frames per move
                                                  (app launched with -frameMonitor YES; RECORD=1 also records the sim screen to <tag>.mov)
       $0 profile <tag> <test> [seconds]          Time Profiler attached to the app while one test runs (PERF_LATENCY ms, default 120), then the per-move table
       $0 fresh                                   delete and recreate the simulator (stale app state after many runs)
env:   SIM (default cm-navperf), PERF_ITERATIONS (moves per test, default 6), PERF_STREAM (ms between streamed replies in the scroll tests, default 400), PERF_ARGS (extra app launch args, e.g. "-perfTurns 2", "-perfSessions 5", "-perfBare YES"; with -perfBare, "-perfBareBar YES" adds a glass Menu bar item, "-perfBareNative YES" a plain bar button and "-perfBareSearch YES" a search field)
output: build/navperf/<tag>.*  (a move window ends 0.3 s after its end mark or at the next step's begin)
USAGE
  exit 2
}

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/build/navperf"
DD="$ROOT/build/NavPerfO"
HERE="$ROOT/scripts/companion-perf"
SIM="${SIM:-cm-navperf}"
mkdir -p "$OUT"
if [[ "${1:-}" == fresh ]]; then
  "$ROOT/scripts/sim.sh" down "$SIM"
  "$ROOT/scripts/sim.sh" up "$SIM" >/dev/null
  exit
fi
udid="$("$ROOT/scripts/sim.sh" up "$SIM")" || exit 1
cd "$ROOT/client"

logstream() {
  xcrun simctl spawn "$udid" log stream --level info --style compact --predicate 'subsystem == "com.clubmate.companion.perf"' > "$OUT/$1.reqlog" 2>&1 &
  echo $!
}

xctest() {
  local tag=$1 lat=$2 jit=$3; shift 3
  local only=()
  for t in "$@"; do only+=(-only-testing:"CompanionUITests/NavigationPerfUITests/$t"); done
  rm -rf "$OUT/$tag.xcresult"
  TEST_RUNNER_PERF_ITERATIONS="${PERF_ITERATIONS:-6}" TEST_RUNNER_PERF_LATENCY=$lat TEST_RUNNER_PERF_JITTER=$jit TEST_RUNNER_PERF_ARGS="${PERF_ARGS:-}" TEST_RUNNER_PERF_STREAM="${PERF_STREAM:-400}" \
    xcodebuild test-without-building -project Companion.xcodeproj -scheme CompanionPerf -destination id="$udid" \
    -derivedDataPath "$DD" "${only[@]}" -resultBundlePath "$OUT/$tag.xcresult" > "$OUT/$tag.log" 2>&1
}

metrics() {
  grep -E "measured|error:|failed \(|passed \(" "$OUT/$1.log" | grep -v "Instructions\|Cycles" |
    sed -E 's/.*NavigationPerfUITests (test[A-Za-z]+)\]. measured \[([^]]+)\] average: ([0-9.]+), relative standard deviation: ([0-9.]+)%.*/\1 | \2 | \3 | rsd \4/'
}

case "${1:-}" in
  build)
    xcodegen generate --spec companion.yml >/dev/null &&
      xcodebuild build-for-testing -project Companion.xcodeproj -scheme CompanionPerf -destination id="$udid" -configuration Debug \
        -derivedDataPath "$DD" SWIFT_OPTIMIZATION_LEVEL=-O SWIFT_COMPILATION_MODE=wholemodule CODE_SIGN_IDENTITY=- CODE_SIGN_STYLE=Manual 2>&1 |
      grep -E "error:|BUILD"
    ;;
  run)
    [[ $# -ge 5 ]] || usage
    tag=$2 lat=$3 jit=$4; shift 4
    lp=$(logstream "$tag")
    rp=""
    if [[ -n "${RECORD:-}" ]]; then
      xcrun simctl io "$udid" recordVideo --force "$OUT/$tag.mov" > /dev/null 2>&1 &
      rp=$!
    fi
    xctest "$tag" "$lat" "$jit" "$@"
    [[ -n "$rp" ]] && kill -INT "$rp" && wait "$rp"
    kill "$lp"
    metrics "$tag"
    python3 "$HERE/steps.py" "$OUT/$tag.reqlog" 2
    python3 "$HERE/frames.py" "$OUT/$tag.reqlog"
    ;;
  profile)
    [[ $# -ge 3 ]] || usage
    tag=$2 t=$3 secs=${4:-60}
    rm -rf "$OUT/$tag.trace"
    lp=$(logstream "$tag")
    xctest "$tag" "${PERF_LATENCY:-120}" 0 "$t" &
    tp=$!
    pid=""
    for _ in $(seq 1 600); do
      pid=$(pgrep -f "$udid/data/Containers/Bundle/Application/.*/Companion.app/Companion" | head -1)
      [[ -n "$pid" ]] && break
      sleep 0.1
    done
    xcrun xctrace record --template "Time Profiler" --device "$udid" --attach "$pid" --time-limit "${secs}s" --output "$OUT/$tag.trace" > "$OUT/$tag.xctrace.log" 2>&1 &
    xp=$!
    (sleep $((secs + 60)); kill -INT "$xp" 2>/dev/null; sleep 30; kill -9 "$xp" 2>/dev/null) &
    wd=$!
    wait "$xp"
    kill "$wd" 2>/dev/null
    wait "$tp"
    kill "$lp"
    metrics "$tag"
    xcrun xctrace export --input "$OUT/$tag.trace" --toc > "$OUT/$tag.toc.xml"
    xcrun xctrace export --input "$OUT/$tag.trace" --xpath '/trace-toc/run[@number="1"]/data/table[@schema="time-profile"]' > "$OUT/$tag.tp.xml"
    kinds=$(grep -oE 'step [a-z]+ begin' "$OUT/$tag.reqlog" | awk '{print $2}' | sort -u | paste -sd, -)
    (cd "$OUT" && python3 "$HERE/table.py" "$tag:$kinds")
    ;;
  *)
    usage
    ;;
esac
