#
# Makefile for pebble.
#
PROJECT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
PROJECT := github.com/canonical/pebble

GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)
GOHOSTOS=$(shell go env GOHOSTOS)
GOHOSTARCH=$(shell go env GOHOSTARCH)
export CGO_ENABLED=0

BUILD_DIR ?= $(PROJECT_DIR)/_build
BIN_DIR = ${BUILD_DIR}/${GOOS}_${GOARCH}/bin

define MAIN_PACKAGES
	github.com/canonical/pebble/cmd/pebble
endef

GIT_COMMIT ?= $(shell git -C $(PROJECT_DIR) rev-parse HEAD 2>/dev/null)

# Build tags passed to go install/build.
# Example: BUILD_TAGS="minimal provider_kubernetes"
BUILD_TAGS ?=

# Build number passed in must be a monotonic int representing
# the build.
PEBBLE_BUILD_NUMBER ?=

# Build flag passed to go -mod
# CI should set this to vendor
PEBBLE_GOMOD_MODE ?= mod

COMPILE_FLAGS = -gcflags "all=-N -l"
LINK_FLAGS = -ldflags "-X $(PROJECT)/version.GitCommit=$(GIT_COMMIT) -X $(PROJECT)/version.GitTreeState=$(GIT_TREE_STATE) -X $(PROJECT)/version.build=$(PEBBLE_BUILD_NUMBER)"

TEST_TIMEOUT ?= 2700s

default: build

.PHONY: test

build: go-build
## build: Create Pebble binaries

test: run-tests
## test: Verify PEBBLE code using unit tests

run-tests:
## run-tests: Run the unit tests
	$(eval TMP := $(shell mktemp -d $${TMPDIR:-/tmp}/pbbl-XXX))
	$(eval TEST_PACKAGES := $(shell go list $(PROJECT)/... | grep -v $(PROJECT)$$ | grep -v mocks))
	@echo 'go test -mod=$(PEBBLE_GOMOD_MODE) -tags "$(BUILD_TAGS)" $(CHECK_ARGS) -test.timeout=$(TEST_TIMEOUT) $$TEST_PACKAGES'
	@TMPDIR=$(TMP) go test -mod=$(PEBBLE_GOMOD_MODE) -tags "$(BUILD_TAGS)" $(CHECK_ARGS) -test.timeout=$(TEST_TIMEOUT) $(TEST_PACKAGES)
	@rm -r $(TMP)


clean:
## clean: Clean the cache and test caches
	go clean -n -r --cache --testcache $(PROJECT)/...


go-build:
## build: Build PEBBLE binaries without updating dependencies
	@mkdir -p ${BIN_DIR}
	@echo 'go build -mod=$(PEBBLE_GOMOD_MODE) -o ${BIN_DIR} -tags "$(BUILD_TAGS)" $(COMPILE_FLAGS) $(LINK_FLAGS) -v $$MAIN_PACKAGES'
	@go build -mod=$(PEBBLE_GOMOD_MODE) -o ${BIN_DIR} -tags "$(BUILD_TAGS)" $(COMPILE_FLAGS) $(LINK_FLAGS) -v $(strip $(MAIN_PACKAGES))
