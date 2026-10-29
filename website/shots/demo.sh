#!/bin/sh
# Runs the real unagit over a made-up world: two GitLab servers answered on
# the loopback over TLS of the world's own, fifteen repositories, pipelines,
# merge requests, clones with changes and a few unibox sandboxes. Nothing of
# yours is read or written: HOME and unagit's configuration are the world's.
# The passphrase is "demo".
#
#   website/shots/demo.sh [dir]                  try unagit out (make demo)
#   website/shots/demo.sh [dir] --fresh          the same, unagit not set up yet
#   website/shots/demo.sh [dir] --shots          the website's screenshots and walk (make shots)
#   website/shots/demo.sh [dir] --video <mp4>    the walk as a video (make video)
set -eu
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
dir=${1:-${TMPDIR:-/tmp}/unagit-demo}
mode=${2:-}

cd "$repo"
mkdir -p "$(dirname "$dir")"
go build -tags shots -o "$dir.mock" ./website/shots
go build -o "$dir.unagit" ./cmd/unagit

server=
stop() { [ -n "$server" ] && kill "$server" 2>/dev/null; server=; }
trap stop EXIT INT TERM

# world lays the world out - set up, or -fresh - and has its servers answer.
world() {
	stop
	"$dir.mock" "$@" "$dir" >/dev/null
	mkdir -p "$dir/bin"
	cp "$dir.mock" "$dir/bin/mock"
	cp "$dir.unagit" "$dir/bin/unagit"
	"$dir/bin/mock" serve "$dir" &
	server=$!
	sleep 0.5
}

case "$mode" in
--shots)
	world
	python3 "$here/shoot.py" "$dir" "$dir/bin/unagit" "$dir/raw"
	node "$here/render.mjs" "$dir/raw" "$repo/website/public/shots"
	world -fresh
	python3 "$here/record.py" "$dir" "$dir.cast"
	stop
	node "$here/squeeze.mjs" "$dir.cast" "$repo/website/public/shots/intro.cast.json"
	exit 0
	;;
--video)
	world -fresh
	python3 "$here/record.py" "$dir" "$dir.cast"
	stop
	node "$here/squeeze.mjs" "$dir.cast" "$dir.squeezed.cast"
	node "$here/video.mjs" "$dir.squeezed.cast" "${3:?the video's path}"
	exit 0
	;;
--fresh)
	world -fresh
	;;
*)
	world
	;;
esac

echo "unagit over the demo world in $dir - the passphrase is: demo"
HOME=$dir UNAGIT_CONFIG_DIR=$dir/config PATH=$dir/bin:$PATH \
	HTTPS_PROXY=http://127.0.0.1:8929 SSL_CERT_FILE=$dir/tls/ca.pem "$dir/bin/unagit"
