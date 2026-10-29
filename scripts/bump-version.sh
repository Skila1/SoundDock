#!/usr/bin/env sh
# Bumps VERSION and internal/version/version.go to the next release.
# Each part counts 0-9 and rolls over into the one before it:
# 0.1.8 -> 0.1.9 -> 0.2.0, and 0.9.9 -> 1.0.0.
# Pass a version to set it outright: scripts/bump-version.sh 0.2.0
set -eu
cd "$(dirname "$0")/.."

cur=$(tr -d '[:space:]' < VERSION)
if [ $# -gt 0 ]; then
	next=$1
else
	next=$(echo "$cur" | awk -F. '{
		maj = $1 + 0; min = $2 + 0; pat = $3 + 1
		if (pat > 9) { pat = 0; min++ }
		if (min > 9) { min = 0; maj++ }
		printf "%d.%d.%d", maj, min, pat
	}')
fi
case $next in
*[!0-9.]* | *..* | .* | *.) echo "bad version: $next" >&2; exit 1 ;;
esac

printf '%s\n' "$next" > VERSION
sed -i "s/Version    = \"$cur\"/Version    = \"$next\"/" internal/version/version.go
grep -q "Version    = \"$next\"" internal/version/version.go || { echo "version.go not updated" >&2; exit 1; }
echo "$cur -> $next"
