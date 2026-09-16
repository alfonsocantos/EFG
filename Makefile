GO ?= go
ARGS ?=

.PHONY: run test build

run:
	docker compose up -d --wait mongo
	$(GO) run . $(ARGS)

test:
	$(GO) test ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -o bin/efg-linux-amd64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -o bin/efg-windows-amd64.exe .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -o bin/efg-darwin-amd64 .

help:
	$(GO) run . --help
