#!/bin/sh
# Runs the real unagit over a made-up world: two servers answered on the
# loopback, a dozen repositories, merge requests, clones with changes and a
# few unibox sandboxes. Nothing of yours is read or written: HOME and
# unagit's configuration are the world's. The passphrase is "demo".
#
#   website/shots/demo.sh [dir]                  try unagit out (make demo)
#   website/shots/demo.sh [dir] --shots          the website's screenshots and walk (make shots)
#   website/shots/demo.sh [dir] --video <mp4>    the walk as a video (make video)
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

case "$mode" in
--shots)
	python3 "$here/shoot.py" "$dir" "$dir/bin/unagit" "$dir/raw"
	node "$here/render.mjs" "$dir/raw" "$repo/website/public/shots"
	# The walk last: it turns unibox's integration off and commits.
	python3 "$here/record.py" "$dir" "$repo/website/public/shots/intro.cast.json"
	exit 0
	;;
--video)
	python3 "$here/record.py" "$dir" "$dir/intro.cast"
	node "$here/video.mjs" "$dir/intro.cast" "${3:?the video's path}"
	exit 0
	;;
esac

echo "unagit over the demo world in $dir - the passphrase is: demo"
HOME=$dir UNAGIT_CONFIG_DIR=$dir/config PATH=$dir/bin:$PATH "$dir/bin/unagit"
