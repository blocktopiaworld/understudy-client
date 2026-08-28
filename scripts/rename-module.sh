#!/usr/bin/env bash
# Change the module path everywhere it appears.
#
#   scripts/rename-module.sh github.com/blocktopiaworld/understudy-client
#
# The module path has to match the URL the repository is actually published at,
# or `go install` and `go get` both 404. It appears in go.mod, in every import,
# in the README's install line and in the golangci import rules, so changing it
# by hand misses one.
set -euo pipefail
new=${1:-}
[ -n "$new" ] || { echo "usage: $0 <new/module/path>"; exit 1; }
old=$(awk '/^module /{print $2}' go.mod)
[ "$old" != "$new" ] || { echo "already $new"; exit 0; }

echo "  $old -> $new"
files=$(grep -rl --exclude-dir=.git -F "$old" . || true)
for f in $files; do
  perl -pi -e "s{\Q$old\E}{$new}g" "$f"
done
gofmt -w .
go build ./... && echo "  builds"
echo "  changed: $(echo "$files" | wc -w) file(s) — run 'make check' before committing"
