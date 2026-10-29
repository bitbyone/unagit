#!/bin/sh
# Runs the real unagit over a made-up world: two servers answered on the
# loopback, a dozen repositories, merge requests, clones with changes and a
# few unibox sandboxes. Nothing of yours is read or written: HOME and
# unagit's configuration are the world's. The passphrase is "demo".
#
#   website/shots/demo.sh [dir]           try unagit out (make demo)
#   website/shots/demo.sh [dir] --shots   photograph it for the website (make shots)
set -eu
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
dir=${1:-${TMPDIR:-/tmp}/unagit-demo}
mode=${2:-}

cd "$repo"
go build -tags shots -o "$dir.mock" ./website/shots
"$dir.mock" "$dir" >/dev/null
mkdir -p "$dir/bin"
mv "$dir.mock" "$dir/bin/mock"
go build -o "$dir/bin/unagit" ./cmd/unagit

"$dir/bin/mock" serve "$dir" &
server=$!
trap 'kill $server 2>/dev/null' EXIT INT TERM

if [ "$mode" = "--shots" ]; then
	python3 "$here/shoot.py" "$dir" "$dir/bin/unagit" "$dir/raw"
	(cd "$repo/website" && node shots/render.mjs "$dir/raw" public/shots)
	exit 0
fi

echo "unagit over the demo world in $dir - the passphrase is: demo"
HOME=$dir UNAGIT_CONFIG_DIR=$dir/config PATH=$dir/bin:$PATH "$dir/bin/unagit"
