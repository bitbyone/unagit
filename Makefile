BIN := unagit
PREFIX ?= $(HOME)/.local

.PHONY: build test test-fast integration integration-image install clean fmt

build:
	go build -o $(BIN) ./cmd/unagit

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
	install -d $(PREFIX)/bin
	install -m 0755 $(BIN) $(PREFIX)/bin/$(BIN)

clean:
	rm -f $(BIN)
