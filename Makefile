# ad-cli

BINARY_NAME = dvo
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

# 'completion regen' writes both bin/dvo-completion.bash and bin/dvo-completion.ps1,
# each stamped with '# dvo-version: <ver>'. It leaves a file untouched when the
# contents already match, so a rebuild that changed no command rewrites nothing.
# The stamp is what the shell-init snippets installed by 'dvo init' compare
# against, so writing it here also saves the next shell a regeneration.
#
# Redirecting with '>' as before could do neither: it clobbered the stamp and
# rewrote both files on every build.

build-local:
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME).exe .
	$(BUILD_DIR)/$(BINARY_NAME).exe completion regen --dir $(BUILD_DIR)

build:
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) .
	$(BUILD_DIR)/$(BINARY_NAME) completion regen --dir $(BUILD_DIR)

clean:
	rm -rf $(BUILD_DIR)

fmt:
	$(GOFMT) ./...
