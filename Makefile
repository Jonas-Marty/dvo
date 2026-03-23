# ad-cli

BINARY_NAME = adg
BUILD_DIR   = bin

GOCMD  = go
GOBUILD = $(GOCMD) build
GOFMT  = $(GOCMD) fmt

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
PKG     := github.com/Jonas-Marty/ad-cli/cmd
LDFLAGS := -ldflags "-s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT)"

.PHONY: all build build-local clean fmt

all: build

build-local:
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME).exe .

build:
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) .

clean:
	rm -rf $(BUILD_DIR)

fmt:
	$(GOFMT) ./...
