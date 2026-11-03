BINS := unagit
PREFIX ?= $(HOME)/.local

.PHONY: build build-unagit cross dist test test-fast integration integration-image install clean fmt website website-dev demo demo-fresh shots video

build: build-unagit

# VERSION is the release tag on HEAD without its v, empty anywhere else,
# which leaves the binary calling itself dev with its commit. The release
# workflow and the Homebrew formula set it the same way.
VERSION ?= $(shell git describe --tags --exact-match --match 'v[0-9]*.[0-9]*' 2>/dev/null | sed 's/^v//')
LDFLAGS := $(if $(VERSION),-X github.com/tobola/unagit/internal/version.Version=$(VERSION))

build-unagit:
	go build -ldflags "$(LDFLAGS)" -o unagit ./cmd/unagit

# cross builds unagit for every system it runs on, without cgo - the
# keychain, the one part that needs it, is macOS's and left out - to see
# that each still compiles; nothing it makes is published.
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

cross:
	set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; exe=unagit; [ $$os = windows ] && exe=unagit.exe; \
		echo "$$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$$os-$$arch/$$exe ./cmd/unagit; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go vet ./...; \
	done

# dist builds what a release publishes: an archive for Windows on each
# architecture, unagit.exe and its licence at its top, and their SHA-256
# sums, for the Scoop manifest. macOS and Linux are not among them -
# Homebrew builds from source there, and only that build has the keychain.
DIST_PLATFORMS := windows/amd64 windows/arm64

dist:
	@test -n "$(VERSION)" || { echo "dist needs VERSION, the release's version without its v"; exit 1; }
	rm -rf dist
	set -e; for p in $(DIST_PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; name=unagit_$(VERSION)_$${os}_$${arch}; \
		mkdir -p dist/$$name; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$$name/unagit.exe ./cmd/unagit; \
		cp LICENSE README.md dist/$$name/; \
		(cd dist/$$name && zip -q ../$$name.zip *); \
		rm -rf dist/$$name; \
	done
	cd dist && if command -v sha256sum >/dev/null; then sha256sum *.zip; else shasum -a 256 *.zip; fi > checksums.txt

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
	rm -rf $(BINS) dist

# The website is VitePress in website/, published to GitHub Pages by
# .github/workflows/website.yml; these build it or serve it while writing.
website:
	cd website && npm ci && npm run build

# unagit over a made-up world - servers, repositories, merge requests,
# sandboxes - with nothing of yours read or written; the passphrase is demo.
demo:
	website/shots/demo.sh

# The same world with unagit not set up yet: the servers and groups to add.
demo-fresh:
	website/shots/demo.sh "$${TMPDIR:-/tmp}/unagit-demo" --fresh

# The website's screenshots and the walk its home page plays, taken of
# that world in a terminal.
shots:
	cd website/shots && npm ci
	website/shots/demo.sh "$${TMPDIR:-/tmp}/unagit-shots" --shots

# The same walk as an MP4, for where the page's player cannot go.
video:
	cd website/shots && npm ci
	website/shots/demo.sh "$${TMPDIR:-/tmp}/unagit-video" --video "$${VIDEO:-$$PWD/unagit.mp4}"

website-dev:
	cd website && npm install && npm run dev
