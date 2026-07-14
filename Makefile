MODULE  := github.com/reloadlife/openvpnd
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE)

.PHONY: all build test lint cover run-daemon run-ctl cross clean deps

all: build

deps:
	go mod tidy
	go mod download

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/openvpnd ./cmd/openvpnd
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/openvpnctl ./cmd/openvpnctl

test:
	go test -race -count=1 ./...

cover:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

lint:
	golangci-lint run ./...

run-daemon: build
	./bin/openvpnd run --config configs/openvpnd.example.yaml

run-ctl: build
	./bin/openvpnctl

cross:
	mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/openvpnd-linux-amd64 ./cmd/openvpnd
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/openvpnctl-linux-amd64 ./cmd/openvpnctl
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/openvpnd-linux-arm64 ./cmd/openvpnd
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/openvpnctl-linux-arm64 ./cmd/openvpnctl

clean:
	rm -rf bin dist coverage.out coverage.html
