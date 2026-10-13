#!/bin/sh
# Node 22 - the agents' CLIs run on it.
set -eu
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl
curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
apt-get install -y --no-install-recommends nodejs
rm -rf /var/lib/apt/lists/*
