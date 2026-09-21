BIN := unagit
PREFIX ?= $(HOME)/.local

.PHONY: build test install clean fmt

build:
	go build -o $(BIN) ./cmd/unagit

test:
	go test -race ./...

fmt:
	gofmt -w .

install: build
	install -d $(PREFIX)/bin
	install -m 0755 $(BIN) $(PREFIX)/bin/$(BIN)

clean:
	rm -f $(BIN)
