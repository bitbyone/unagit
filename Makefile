BINS := unagit
PREFIX ?= $(HOME)/.local

.PHONY: build build-unagit test test-fast integration integration-image install clean fmt website website-dev demo shots

build: build-unagit

# VERSION is the release tag on HEAD without its v, empty anywhere else,
# which leaves the binary calling itself dev with its commit. The release
# workflow and the Homebrew formula set it the same way.
VERSION ?= $(shell git describe --tags --exact-match --match 'v[0-9]*.[0-9]*' 2>/dev/null | sed 's/^v//')
LDFLAGS := $(if $(VERSION),-X github.com/tobola/unagit/internal/version.Version=$(VERSION))

build-unagit:
	go build -ldflags "$(LDFLAGS)" -o unagit ./cmd/unagit

test:
	go test -race ./...

test-fast:
	go test ./...

# The integration suite runs the real programs unagit drives - Neovim,
# incomm, Zellij, herdr - in a Docker image that has them (integration/),
# never on this machine. The image is named after its Dockerfile, so it is
# built once and again only when that changes; the checkout is mounted
# read-only and Go's caches live in a volume of their own. It runs the
# whole suite on Linux first, then the tests of real programs
# (*_integration_test.go) one package at a time: a real Zellij starting
# beside twenty compiling packages missed its own timeouts.
INTEGRATION_IMAGE := unagit-integration:$(shell git hash-object integration/Dockerfile | cut -c1-12)
INTEGRATION_TESTS := $(shell find . -name '*_integration_test.go' -not -path './.*' -exec grep -ho '^func Test[A-Za-z0-9_]*' {} + | sed 's/^func //' | sort | paste -sd'|' -)

integration-image:
	@docker image inspect $(INTEGRATION_IMAGE) >/dev/null 2>&1 || docker build -t $(INTEGRATION_IMAGE) integration

integration: integration-image
	docker run --rm --init -v "$(CURDIR)":/src:ro -v unagit-integration-cache:/cache $(INTEGRATION_IMAGE) \
		sh -c 'go test -count=1 ./... && go test -tags integration -count=1 -p 1 -run "^($(INTEGRATION_TESTS))$$" ./...'

fmt:
	gofmt -w .

install: build
	install -d "$(PREFIX)/bin"
	@set -e; for bin in $(BINS); do \
		if ! cmp -s "$$bin" "$(PREFIX)/bin/$$bin" || [ ! -x "$(PREFIX)/bin/$$bin" ]; then \
			install -m 0755 "$$bin" "$(PREFIX)/bin/$$bin"; \
			echo "installed $$bin"; \
		fi; \
	done

clean:
	rm -f $(BINS)

# The website is VitePress in website/, published to GitHub Pages by
# .github/workflows/website.yml; these build it or serve it while writing.
website:
	cd website && npm ci && npm run build

# unagit over a made-up world - servers, repositories, merge requests,
# sandboxes - with nothing of yours read or written; the passphrase is demo.
demo:
	website/shots/demo.sh

# The website's screenshots, taken of that world in a terminal.
shots:
	cd website && npm ci
	website/shots/demo.sh "$${TMPDIR:-/tmp}/unagit-shots" --shots

website-dev:
	cd website && npm install && npm run dev
