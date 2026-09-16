GO ?= go
ARGS ?=
MONGO_TEST_URI ?= mongodb://localhost:27017

.PHONY: run test build test-integration test-load

run:
	docker compose up -d --wait mongo
	$(GO) run . $(ARGS)

test:
	$(GO) test ./...

test-integration:
	docker compose up -d --wait mongo
	MONGO_TEST_URI="$(MONGO_TEST_URI)" $(GO) test ./presence -run Mongo -count=1

test-load:
	docker compose up -d --wait mongo
	MONGO_TEST_URI="$(MONGO_TEST_URI)" $(GO) test ./presence -run '^$$' -bench '^BenchmarkEvents$$' -benchtime=5s -cpu=8

build:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -o bin/efg-linux-amd64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -o bin/efg-windows-amd64.exe .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -o bin/efg-darwin-amd64 .

help:
	$(GO) run . --help
