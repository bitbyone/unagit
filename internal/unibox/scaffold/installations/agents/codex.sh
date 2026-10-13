#!/bin/bash
# npm comes from the image or from tools/nvm; the Node version is the user's
# choice - when none was picked, the current LTS serves. The symlinks put the
# agent and its node where non-login shells and the build check find them.
set -eu
if ! command -v npm >/dev/null 2>&1 && [ -s /etc/profile.d/nvm.sh ]; then
    . /etc/profile.d/nvm.sh
    command -v npm >/dev/null 2>&1 || nvm install --lts
fi
command -v npm >/dev/null 2>&1 || {
    echo 'npm is missing; list tools/nvm before the agents or put node into the image' >&2
    exit 1
}
npm install -g @openai/codex
ln -sf "$(command -v node)" /usr/local/bin/node
ln -sf "$(command -v codex)" /usr/local/bin/codex
