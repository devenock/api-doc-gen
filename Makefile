# Specyl - Makefile
BINARY_NAME := specyl
BIN_DIR     := bin
MAIN_PATH   := .
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -X github.com/devenock/specyl/cmd.version=$(VERSION)

.PHONY: build test run install clean

build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) $(MAIN_PATH)

test:
	go test ./...

run: build
	./$(BIN_DIR)/$(BINARY_NAME) generate

install: build
	go install -ldflags "$(LDFLAGS)" $(MAIN_PATH)

clean:
	rm -rf $(BIN_DIR)
