#!/bin/bash
# SDKMAN - the JVM toolchain manager. No versions are chosen here: pick them
# in base.sh or in the Dockerfile, e.g.
#   . /etc/profile.d/sdkman.sh
#   sdk install java <version>
#   ln -sf "$SDKMAN_DIR"/candidates/*/current/bin/* /usr/local/bin/
# The link puts the chosen tools where non-login shells find them.
set -eu
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl git unzip zip
rm -rf /var/lib/apt/lists/*
export SDKMAN_DIR=/opt/sdkman
curl -fsSL "https://get.sdkman.io?rcupdate=false" | bash
cat > /etc/profile.d/sdkman.sh <<'RC'
export SDKMAN_DIR=/opt/sdkman
[ -s "$SDKMAN_DIR/bin/sdkman-init.sh" ] && . "$SDKMAN_DIR/bin/sdkman-init.sh"
RC
chmod -R a+rwX "$SDKMAN_DIR"
