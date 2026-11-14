#!/bin/sh
name=${0##*/}
self=$(cd "${0%/*}" 2>/dev/null && pwd -P)

own_dir() {
	dir=$(GIT_CONFIG_COUNT=${CLUBMATE_GIT_CONFIG_COUNT:-0} git config --type=path --get core.hooksPath 2>/dev/null)
	if [ -z "$dir" ]; then
		dir=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)/hooks
	fi
	real=$(cd "$dir" 2>/dev/null && pwd -P) || return 1
	[ "$real" != "$self" ] || return 1
	printf '%s\n' "$dir"
}

refuse() {
	echo "Clubmate: $1 is gated: an agent lands it with the git_land tool, which starts the review; plain git can't move it (on the branch itself: stage the change and call git_land with mode index). Syncing with the branch's own upstream is allowed: git fetch, then git merge --ff-only with it." >&2
	echo "This command may already have changed the index or files: \`git reset --merge\` puts them back." >&2
}

gated() {
	[ -n "$CLUBMATE_LAND_PROTECT" ] && [ -n "$CLUBMATE_LAND_COMMON" ] || return 1
	common=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || return 1
	common=$(cd "$common" 2>/dev/null && pwd -P) || return 1
	[ "$common" = "$CLUBMATE_LAND_COMMON" ]
}

approved() {
	tree=$(git rev-parse -q --verify "$2^{tree}" 2>/dev/null)
	[ -n "$tree" ] && grep -q '"outcome": *"approved"' "$CLUBMATE_LAND_VERDICTS/$1-$tree.json" 2>/dev/null
}

upstream() {
	branch=${1#refs/heads/}
	remote=$(git config --get "branch.$branch.remote" 2>/dev/null)
	merge=$(git config --get "branch.$branch.merge" 2>/dev/null)
	if [ -n "$remote" ] && [ -n "$merge" ]; then
		[ "$remote" != . ] || return 1
		printf 'refs/remotes/%s/%s\n' "$remote" "${merge#refs/heads/}"
	else
		printf 'refs/remotes/origin/%s\n' "$branch"
	fi
}

synced() {
	up=$(upstream "$3") || return 1
	git merge-base --is-ancestor "$1" "$2" 2>/dev/null && git merge-base --is-ancestor "$2" "$up" 2>/dev/null
}

gate() {
	[ "$1" = prepared ] && gated || return 0
	status=0
	while read -r old new ref; do
		[ -n "$ref" ] || continue
		case " $CLUBMATE_LAND_PROTECT " in *" $ref "*) ;; *) continue ;; esac
		case $new in *[!0]*) ;; *) refuse "$ref"; status=1; continue ;; esac
		case $new in ref:*) refuse "$ref"; status=1; continue ;; esac
		case $old in *[!0]*) ;; *) old=$(git rev-parse -q --verify "$ref" 2>/dev/null || printf '%s' "$old") ;; esac
		[ "$new" = "$old" ] && continue
		case $old in *[!0]*) synced "$old" "$new" "$ref" && continue ;; esac
		approved "$old" "$new" && continue
		refuse "$ref"
		status=1
	done <<EOF
$input
EOF
	return $status
}

push_gate() {
	gated || return 0
	status=0
	while read -r lref lsha rref rsha; do
		[ -n "$rref" ] || continue
		case " $CLUBMATE_LAND_PROTECT " in *" $rref "*) ;; *) continue ;; esac
		case $lsha in *[!0]*) ;; *) refuse "$rref on $1"; status=1; continue ;; esac
		git merge-base --is-ancestor "$lsha" "$rref" 2>/dev/null && continue
		approved "$rsha" "$lsha" && continue
		refuse "$rref on $1"
		status=1
	done <<EOF
$input
EOF
	return $status
}

if [ "$name" = reference-transaction ] || [ "$name" = pre-push ]; then
	input=$(cat)
	if [ "$name" = pre-push ]; then
		push_gate "$1" || exit 1
	else
		gate "$1" || exit 1
	fi
	dir=$(own_dir) || exit 0
	[ -x "$dir/$name" ] || exit 0
	printf '%s\n' "$input" | "$dir/$name" "$@"
	exit $?
fi
dir=$(own_dir) || exit 0
[ -x "$dir/$name" ] || exit 0
exec "$dir/$name" "$@"
