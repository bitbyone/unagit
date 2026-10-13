#!/bin/bash
# npm comes from the image or from tools/nvm; the Node version is the user's
# choice - when none was picked, the current LTS serves. link puts the agent
# and its node where non-login shells and the build check find them, and
# leaves a tool already there alone - a link onto itself would loop.
set -eu
if ! command -v npm >/dev/null 2>&1 && [ -s /etc/profile.d/nvm.sh ]; then
    . /etc/profile.d/nvm.sh
    command -v npm >/dev/null 2>&1 || nvm install --lts
fi
command -v npm >/dev/null 2>&1 || {
    echo 'npm is missing; list tools/nvm before the agents or put node into the image' >&2
    exit 1
}
npm install -g @anthropic-ai/claude-code
link() {
    src="$(command -v "$1" 2>/dev/null || true)"
    if [ -z "$src" ] && [ -x "$(npm prefix -g)/bin/$1" ]; then
        src="$(npm prefix -g)/bin/$1"
    fi
    if [ -n "$src" ] && [ "$src" != "/usr/local/bin/$1" ]; then
        ln -sf "$src" "/usr/local/bin/$1"
    fi
}
link node
link claude
