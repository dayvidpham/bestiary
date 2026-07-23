# bestiary build entry points.
#
# Every Go invocation in this project runs with CGO_ENABLED=0 (pure-Go builds,
# the project standard) and GOWORK=off (a stray go.work anywhere up the tree
# must never change what these targets build). Encoding that here once is the
# point of this Makefile: `make gates` is the full pre-commit suite.

GO      := CGO_ENABLED=0 GOWORK=off go
BINARY  := bestiary

.PHONY: build test vet fmt generate gates install clean

## build: compile the CLI to ./bestiary
build:
	$(GO) build -o $(BINARY) ./cmd/bestiary

## test: run the full test suite
test:
	$(GO) test ./...

## vet: run go vet across the module
vet:
	$(GO) vet ./...

## fmt: fail if any file needs gofmt
fmt:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## generate: offline, deterministic codegen from the committed snapshot
generate:
	$(GO) generate ./...

## gates: the full pre-commit suite (format, vet, tests, regen is byte-clean)
gates: fmt vet test generate
	@if [ -n "$$(git status --porcelain -- '*_gen.go')" ]; then \
		echo "regen produced a diff — generated files are stale:"; \
		git status --porcelain -- '*_gen.go'; exit 1; \
	fi
	@echo "all gates green"

## install: install the CLI into GOBIN/GOPATH/bin
install:
	$(GO) install ./cmd/bestiary

## clean: remove the locally built binary
clean:
	rm -f $(BINARY)
