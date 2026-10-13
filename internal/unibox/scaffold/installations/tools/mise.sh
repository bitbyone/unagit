#!/bin/sh
# mise - the polyglot tool version manager, installed where every shell finds
# it. No versions are chosen here; pick them with mise use or mise install.
set -eu
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl
rm -rf /var/lib/apt/lists/*
curl -fsSL https://mise.run | MISE_INSTALL_PATH=/usr/local/bin/mise sh
