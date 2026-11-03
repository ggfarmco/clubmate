#!/bin/sh
set -u

tmpdir=$(getconf DARWIN_USER_TEMP_DIR 2>/dev/null || echo "${TMPDIR:-/tmp}")
main=$(git -C "$(dirname "$0")" worktree list --porcelain | sed -n '1s/^worktree //p')
lib="$HOME/Library"

prefixes() {
	find "$2" -mindepth 1 -maxdepth 1 -print0 2>/dev/null | xargs -0 du -sk 2>/dev/null | awk -F'\t' -v kind="$1" '
		{ n = $2; sub(/.*\//, "", n); sub(/\.[A-Za-z0-9]{6}$/, "", n); sub(/[-_.]?[0-9]+$/, "", n); c[n]++; s[n] += $1 }
		END { for (n in c) printf "%s\t%s\t%d\t%d\n", kind, n, c[n], s[n] }'
}

dir() {
	[ -e "$2" ] || return 0
	printf '%s\t%s\t%d\t%d\n' "$1" "$2" "$(find "$2" -mindepth 1 -maxdepth 1 | wc -l)" "$(du -sk "$2" | cut -f1)"
}

snapshot() {
	prefixes tmpdir "$tmpdir"
	prefixes tmp /tmp/
	if [ "${1:-}" = tmp ]; then return; fi
	for s in "$lib/Application Support/clubmate" "$lib/Application Support/clubmate-dev"; do
		dir state "$s"
		for d in "$s"/*/ "$s"/plugin-data/*/ "$s"/plugin-data/*/*/; do dir state "${d%/}"; done
	done
	dir cache "$lib/Caches/clubmate-test-bin"
	dir cache "$lib/Caches/go-build"
	dir cache "$lib/Developer/CoreSimulator/Devices"
	dir cache "$lib/Developer/Xcode/DerivedData"
	dir repo "$main/.worktrees"
	df -k "$HOME" | awk 'NR == 2 { printf "disk\tfree\t0\t%d\n", $4 }'
}

compare() {
	awk -F'\t' -v kb="$3" -v entries="$4" '
		NR == FNR { c[$1 FS $2] = $3; s[$1 FS $2] = $4; next }
		{ k = $1 FS $2; dc = $3 - c[k]; ds = $4 - s[k] }
		$1 == "disk" { if (-ds > kb) printf "disk free\t%+d MB\n", ds / 1024; next }
		ds > kb || dc > entries { printf "%s\t%s\t%+d entries\t%+d MB\t(now %d entries, %d MB)\n", $1, $2, dc, ds / 1024, $3, $4 / 1024 }
	' "$1" "$2"
}

case "${1:-}" in
snapshot) snapshot "${2:-}" ;;
compare) compare "$2" "$3" "${4:-1048576}" "${5:-200}" ;;
*) echo "usage: $0 snapshot [tmp] | compare old new [kb entries]" >&2; exit 2 ;;
esac
