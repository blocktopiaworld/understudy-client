# understudy-client
#
# `make check` is what CI runs and what should pass before anything is pushed.

# GO resolves to the first go on PATH that actually runs.
#
# Not paranoia: on at least one dev box /usr/local/bin/go and /usr/bin/go are
# symlinks to a *directory*, and they sort before the real one. Executing a
# directory fails with "Permission denied", which reads as a permissions problem
# rather than a broken symlink and cost an afternoon to spot. Override with
# `make GO=/path/to/go` if this picks wrongly.
# Deliberately an absolute path rather than the bare name: make execs simple
# recipes directly instead of through a shell, and its own PATH search does not
# skip an entry it cannot execute the way sh does.
# `command -v -a` is a bashism and make runs $(shell) under /bin/sh, which on
# Debian and on the CI runners is dash — where it is an error, not a fallback.
# So the plain form is in the list too, and it is the one that answers there.
GO      ?= $(shell for g in $$(command -v -a go 2>/dev/null) $$(command -v go 2>/dev/null) \
	              /snap/bin/go /usr/local/go/bin/go; do \
	           "$$g" version >/dev/null 2>&1 && echo "$$g" && break; done)

# An empty GO turns every recipe into an attempt to run "vet" or "test" as a
# program, and make reports that as "No such file or directory" against a name
# nobody wrote. Say what is actually wrong instead.
ifeq ($(strip $(GO)),)
$(error no working go toolchain found; install Go or run `make GO=/path/to/go`)
endif

# Likewise for golangci-lint, which `go install` puts in GOPATH/bin — a
# directory that is frequently not on PATH.
GOLANGCI ?= $(shell for l in $$(command -v -a golangci-lint 2>/dev/null) \
	              $$(command -v golangci-lint 2>/dev/null) \
	              "$$HOME/go/bin/golangci-lint"; do \
	           "$$l" --version >/dev/null 2>&1 && echo "$$l" && break; done)

BINARY  ?= understudy-client
PKGS    := ./...

# The generated protocol tables are excluded from formatting checks: they are
# produced by internal/gen and reformatting them by hand would be undone on the
# next generate.
HAND_WRITTEN := $(shell find . -name '*.go' -not -name 'version_*.go' -not -path './.recovered/*')

.PHONY: all
all: check build

## build: compile the bot binary
.PHONY: build
build:
	$(GO) build -o $(BINARY) ./cmd/understudy-client

## test: run the unit tests
.PHONY: test
test:
	$(GO) test $(PKGS)

## race: run the tests under the race detector
#
# The client drives a read loop, a control API and background goroutines over
# shared state, so this is not optional — it is where the world-model race was
# found.
.PHONY: race
race:
	$(GO) test -race -count=1 $(PKGS)

## cover: report per-package coverage
.PHONY: cover
cover:
	$(GO) test -coverprofile=coverage.out $(PKGS)
	$(GO) tool cover -func=coverage.out | tail -1

## cover-html: open the coverage report
.PHONY: cover-html
cover-html: cover
	$(GO) tool cover -html=coverage.out

## fmt: format everything hand-written
.PHONY: fmt
fmt:
	gofmt -w $(HAND_WRITTEN)

## fmt-check: fail if anything is unformatted
.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l $(HAND_WRITTEN)); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt'd:"; echo "$$unformatted"; exit 1; \
	fi

## vet: run go vet
.PHONY: vet
vet:
	$(GO) vet $(PKGS)

## lint: run golangci-lint if it is installed
.PHONY: lint
lint:
	@if [ -n "$(GOLANGCI)" ]; then \
		$(GOLANGCI) run; \
	else \
		echo "golangci-lint not installed; skipping"; \
		echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; \
	fi

## modernize: rewrite to current Go idioms (range-over-int, slices, maps)
.PHONY: modernize
modernize:
	$(GO) run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix $(PKGS)

## check: everything CI runs
.PHONY: check
check: fmt-check vet lint race

## generate: rebuild a protocol table from minecraft-data
#
#   make generate MC_DATA=/path/to/minecraft-data/data MC_VERSION=26.1
#
# minecraft-data: https://github.com/PrismarineJS/minecraft-data
.PHONY: generate
generate:
	@test -n "$(MC_DATA)" || (echo "set MC_DATA=/path/to/minecraft-data/data"; exit 1)
	@test -n "$(MC_VERSION)" || (echo "set MC_VERSION=26.1"; exit 1)
	node internal/gen/genversion.mjs $(MC_DATA) $(MC_VERSION) \
		protocol/version_$(subst .,_,$(MC_VERSION)).go
	gofmt -w protocol/version_$(subst .,_,$(MC_VERSION)).go

## clean: remove build output
.PHONY: clean
clean:
	rm -f $(BINARY) coverage.out

## help: list the targets
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
