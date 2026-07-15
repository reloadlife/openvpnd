MODULE  := github.com/reloadlife/openvpnd
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE)

.PHONY: all build test test-unit test-api test-feature test-integration test-race lint cover cover-html run-daemon run-ctl cross clean deps

all: build

deps:
	go mod tidy
	go mod download

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/openvpnd ./cmd/openvpnd
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/openvpnctl ./cmd/openvpnctl

# Full suite (CI default)
test:
	go test -race -count=1 ./...

# Fast pure-logic packages
test-unit:
	go test -count=1 ./internal/netutil/ ./internal/instance/ ./internal/confgen/ ./internal/features/ ./internal/stats/ ./internal/ovpnbackend/ ./internal/bandwidth/

# HTTP + PKI + profile contracts
test-api:
	go test -count=1 ./internal/api/ ./internal/pki/ ./internal/db/

# OpenVPN conf emission + feature presets (tier A matrix)
test-feature:
	go test -count=1 ./internal/confgen/ ./internal/features/ -run 'TestTierA|TestRender|TestEveryBuiltin|TestExpand|TestCCD'

# Host OpenVPN + reconciler integration (skips if openvpn missing / no CAP)
test-integration:
	go test -tags=integration -count=1 ./internal/ovpnbackend/ ./internal/reconcile/ -timeout 120s

test-race:
	go test -race -count=1 ./...

cover:
	go test -count=1 -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -n 1
	@echo "--- lowest packages ---"
	@go tool cover -func=coverage.out | awk '$$3 ~ /%$$/ {print}' | sort -k3 -n | head -n 15

cover-html: cover
	go tool cover -html=coverage.out -o coverage.html
	@echo "wrote coverage.html"

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
