#!/bin/bash
# nvm - the Node version manager. No Node version is chosen here: pick one in
# base.sh or in the Dockerfile (nvm install <version>); the agents' scripts
# fall back to the current LTS when none was picked.
set -eu
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl git
rm -rf /var/lib/apt/lists/*
export NVM_DIR=/opt/nvm
mkdir -p "$NVM_DIR"
curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/master/install.sh | PROFILE=/dev/null bash
cat > /etc/profile.d/nvm.sh <<'RC'
export NVM_DIR=/opt/nvm
[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"
RC
chmod -R a+rwX "$NVM_DIR"
