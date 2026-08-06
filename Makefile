GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
# -buildmode=pie: Android (Termux) refuses to exec non-PIE binaries
GOFLAGS = -trimpath -buildmode=pie -ldflags '-s -w'

.PHONY: all server client dist test clean

all: server client

# make server / make client — host arch by default, override with e.g.:
#   make server GOOS=linux GOARCH=arm64
server:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o bin/rf-clipd_$(GOOS)_$(GOARCH) ./cmd/rf-clipd

client:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -o bin/rf-clip_$(GOOS)_$(GOARCH) ./cmd/rf-clip

# full release matrix, same artifacts as CI
dist:
	for os in linux darwin; do for arch in amd64 arm64; do \
		$(MAKE) server client GOOS=$$os GOARCH=$$arch; \
	done; done
	$(MAKE) server client GOOS=android GOARCH=arm64 # Termux

test:
	go test ./...

clean:
	rm -rf bin
