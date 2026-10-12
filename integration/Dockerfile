# The image of the integration suite (make integration): Go and git, and the
# programs unagit drives, at the versions the suite was last run with. The
# source is not in it; make integration mounts the checkout read-only.
FROM golang:1.26-bookworm

ARG TARGETARCH
ARG NVIM_VERSION=v0.12.6
ARG ZELLIJ_VERSION=v0.45.1
ARG HERDR_VERSION=v0.9.3
# incomm has no releases: a commit of its main branch, cloned and built.
ARG INCOMM_REPOSITORY=https://github.com/bitbyone/incomm.git
ARG INCOMM_REF=8a1f48a2bdcf8460e5a6f60247298f040d301fd9

RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates curl procps \
 && rm -rf /var/lib/apt/lists/*

RUN set -eu; \
    case "$TARGETARCH" in \
      arm64) nvim=arm64; rust=aarch64 ;; \
      amd64) nvim=x86_64; rust=x86_64 ;; \
      *) echo "unsupported architecture $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://github.com/neovim/neovim/releases/download/$NVIM_VERSION/nvim-linux-$nvim.tar.gz" | tar -xz -C /opt; \
    ln -s "/opt/nvim-linux-$nvim/bin/nvim" /usr/local/bin/nvim; \
    curl -fsSL "https://github.com/zellij-org/zellij/releases/download/$ZELLIJ_VERSION/zellij-no-web-$rust-unknown-linux-musl.tar.gz" | tar -xz -C /usr/local/bin zellij; \
    curl -fsSL -o /usr/local/bin/herdr "https://github.com/herdrdev/herdr/releases/download/$HERDR_VERSION/herdr-linux-$rust"; \
    chmod 0755 /usr/local/bin/herdr; \
    nvim --version | head -1; zellij --version; herdr --version

RUN set -eu; \
    git clone --quiet "$INCOMM_REPOSITORY" /tmp/incomm; \
    git -C /tmp/incomm checkout --quiet "$INCOMM_REF"; \
    cd /tmp/incomm/cli && go build -o /usr/local/bin/incomm .; \
    rm -rf /tmp/incomm "$(go env GOCACHE)" "$(go env GOMODCACHE)"; \
    incomm version --json

# Tests run as a user of their own, never as root: a test that a file is
# unreadable or a directory unwritable would pass as root.
RUN useradd --create-home --uid 1000 tester \
 && mkdir -p /cache && chown tester /cache
USER tester
ENV GOCACHE=/cache/build GOMODCACHE=/cache/mod GOFLAGS=-buildvcs=false
WORKDIR /src
