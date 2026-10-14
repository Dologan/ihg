# Common tasks. `make` alone runs the checks every phase must pass.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: check test vet fmt build legacy-test

check: fmt vet test

test:
	go test ./...

vet:
	go vet ./...

# Fails if any file is not gofmt-formatted (gofmt -l lists them).
fmt:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/ihg ./cmd/ihg

legacy-test:
	cd legacy && python3 -m pytest -q
