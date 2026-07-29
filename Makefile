SHELL := /bin/sh

GO ?= go
GOFMT ?= gofmt
GO_BUILD_FLAGS ?= -buildvcs=false
BINARY ?= bin/amadeus
PACKAGES := ./...
GO_FILES := $(shell find cmd internal -type f -name '*.go' -print)

.PHONY: all fmt fmt-check vet test build check ci clean

all: check

fmt:
	$(GOFMT) -w $(GO_FILES)

fmt-check:
	@unformatted="$$($(GOFMT) -l $(GO_FILES))"; \
	if [ -n "$$unformatted" ]; then \
		echo "Go files need formatting:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet:
	$(GO) vet $(PACKAGES)

test:
	$(GO) test $(PACKAGES)

build:
	mkdir -p $(dir $(BINARY))
	$(GO) build $(GO_BUILD_FLAGS) -o $(BINARY) ./cmd/amadeus

check: fmt-check vet test build

ci: check

clean:
	rm -rf bin
