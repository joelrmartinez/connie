BINARY  := connie
# Version comes from the latest git tag (v1.2.3 -> 1.2.3), else "dev".
VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || echo dev)
ifeq ($(VERSION),)
VERSION := dev
endif
LDFLAGS := -s -w -X github.com/joelrmartinez/connie/internal/version.Version=$(VERSION)

.PHONY: build check test dist clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test -race ./...

test: check

# Local cross-compile. Tagged releases are built by .github/workflows/release.yml.
dist: clean
	mkdir -p dist
	for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do \
		os=$${target%/*}; arch=$${target#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/$(BINARY)-$(VERSION)-$$os-$$arch$$ext . || exit 1; \
	done
	cd dist && (sha256sum * 2>/dev/null || shasum -a 256 *) > checksums.txt

clean:
	rm -rf dist $(BINARY)
