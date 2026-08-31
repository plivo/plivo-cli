# Plivo Shell — build targets
#
# Sizes on darwin/arm64 (mileage varies):
#
#   make            -> plivo (debug, ~9.6 MB) — local dev
#   make build      -> plivo (stripped, ~6.6 MB) — what release artifacts use
#   make tiny       -> plivo (stripped + UPX, ~2.5 MB) — minimum download size
#   make build-all  -> dist/plivo_<os>_<arch>[.exe] for darwin/linux/windows × amd64/arm64
#   make clean
#   make install    -> $GOPATH/bin/plivo (stripped)
#   make run        -> ./plivo + show help

BINARY  := plivo
PKG     := github.com/plivo/plivo-cli
VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)

# -s removes the symbol table, -w drops DWARF debug info — typical for release.
LDFLAGS := -s -w -X $(PKG)/internal/version.Value=$(VERSION)

.PHONY: default build tiny build-all install run clean fmt vet test docs help

default: ## Debug build with symbols (best for local dev)
	go build -o $(BINARY) .
	@ls -lh $(BINARY) | awk '{print "  built:", $$NF, "(", $$5, ")"}'

build: ## Stripped release build (~6.6 MB)
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) .
	@ls -lh $(BINARY) | awk '{print "  built:", $$NF, "(", $$5, ")"}'

tiny: build ## Stripped + UPX (~2.5 MB) — slower startup
	@command -v upx >/dev/null || (echo "upx not installed: brew install upx"; exit 1)
	upx --best --lzma $(BINARY)
	@ls -lh $(BINARY) | awk '{print "  built:", $$NF, "(", $$5, ")"}'

build-all: ## Cross-compile release binaries for darwin/linux/windows × amd64/arm64
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)_darwin_arm64 .
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)_darwin_amd64 .
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)_linux_arm64 .
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)_linux_amd64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)_windows_amd64.exe .
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/$(BINARY)_windows_arm64.exe .
	@ls -lh dist/*

install: build ## Install stripped binary to $GOPATH/bin
	cp $(BINARY) $(shell go env GOPATH)/bin/

run: default ## Build + show help
	./$(BINARY) --help

fmt: ## gofmt + goimports
	gofmt -w .

vet: ## go vet
	go vet ./...

test: ## go test
	go test ./...

# VERSION defaults to `git describe` (see top of file), so right after tagging
# this needs no arguments. Override with VERSION=v0.3.0 to re-render an old tag.
release-tap: ## Render the Homebrew formula + Scoop manifest into $(TAP) (run after the release exists)
	@test -f dist/SHA256SUMS || { echo "✗ dist/SHA256SUMS missing — run 'make build-all' and regenerate it first"; exit 2; }
	scripts/gen-tap.sh "$(VERSION)" dist/SHA256SUMS "$(or $(TAP),../homebrew-tap)"

test-tap: ## Check gen-tap.sh still renders the golden output
	scripts/gen-tap-test.sh

check-tap-fresh: ## Warn if the Homebrew tap is behind the latest release
	scripts/check-tap-fresh.sh

docs: ## Regenerate the command reference (docs/COMMANDS.md)
	go run ./tools/gendocs -o docs/COMMANDS.md

clean: ## Remove build artefacts
	rm -f $(BINARY)
	rm -rf dist

help: ## Print this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
