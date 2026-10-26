BINARY := bin/xg
PKG    := ./cmd/xg
DB     ?= $(HOME)/.local/share/x-games/smoke.db

.PHONY: all build run test fmt vet lint clean smoke install tidy

all: fmt vet test build

build:
	go build -o $(BINARY) $(PKG)

install:
	go install $(PKG)

run: build
	$(BINARY)

test:
	go test ./...

test-race:
	go test -race ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin

# Live end-to-end check: fetch from the real sources and print the results.
smoke: build
	@rm -f $(DB)
	XG_DB=$(DB) $(BINARY) sync --limit 20
	@echo
	XG_DB=$(DB) $(BINARY) search "" --limit 10
	@echo
	XG_DB=$(DB) $(BINARY) search "buildathon OR hackathon" --limit 10
