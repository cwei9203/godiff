#!/bin/sh
# Write locales/catalog_gen.go from the catalogs beside it.
#
# `go tool mygo generate` works on the module it finds, and its own paths break
# when the repository sits under a directory with a space in it, as it does
# under ~/Library/Application Support. So the work is done in a copy of the
# repository under a directory with no space in its path, and the one file the
# generator writes is copied back: the file names no path, so the copy's is the
# same file.
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
out=locales/catalog_gen.go

if [ ! -f "$root/go.mod" ]; then
	echo "gen-locales: $root holds no go.mod, so there is no module to generate" >&2
	exit 1
fi
if [ ! -d "$root/locales" ]; then
	echo "gen-locales: $root holds no locales directory" >&2
	exit 1
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/godiff-locales.XXXXXX") || {
	echo "gen-locales: no room for a directory to work in" >&2
	exit 1
}
trap 'rm -rf "$work"' EXIT INT TERM

case "$work" in
*' '*)
	echo "gen-locales: $work has a space in its path, which the generator cannot work in" >&2
	exit 1
	;;
esac

# The module as it stands, without what the generator never reads: the
# repository's history and the app a build wrote.
if command -v rsync >/dev/null 2>&1; then
	rsync -a --exclude .git --exclude build "$root/" "$work/" || {
		echo "gen-locales: could not copy $root to $work" >&2
		exit 1
	}
else
	(cd "$root" && tar -cf - --exclude .git --exclude build .) | (cd "$work" && tar -xf -) || {
		echo "gen-locales: could not copy $root to $work" >&2
		exit 1
	}
fi

if ! (cd "$work" && go tool mygo generate); then
	echo "gen-locales: the generator failed in a copy of the repository at $work" >&2
	exit 1
fi
if [ ! -f "$work/$out" ]; then
	echo "gen-locales: the generator wrote no $out in $work" >&2
	exit 1
fi

cp "$work/$out" "$root/$out" || {
	echo "gen-locales: could not write $root/$out" >&2
	exit 1
}
echo "gen-locales: wrote $out"
