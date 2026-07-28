BINARY  := connie
VERSION := 0.1.0

.PHONY: build check test dist clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BINARY) .

check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test -race ./...

test: check

dist: clean
	mkdir -p dist
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/$(BINARY)-$(VERSION)-darwin-arm64 .
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/$(BINARY)-$(VERSION)-darwin-amd64 .
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/$(BINARY)-$(VERSION)-linux-amd64 .
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/$(BINARY)-$(VERSION)-linux-arm64 .

clean:
	rm -rf dist $(BINARY)
