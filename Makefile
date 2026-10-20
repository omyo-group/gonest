GO ?= go

.PHONY: all build vet lint test race cover bench tidy

all: vet lint test

build:
	$(GO) build ./...

vet:
	$(GO) vet ./...

lint:
	golangci-lint run ./...

test:
	$(GO) test -race -count=1 ./...

cover:
	$(GO) test -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

bench:
	$(GO) test -run=^$$ -bench=. -benchmem ./...

tidy:
	$(GO) mod tidy
