# API Documentation Generator - Makefile
BINARY_NAME := apidoc-gen
BIN_DIR     := bin
MAIN_PATH   := .

.PHONY: build test run install clean

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) $(MAIN_PATH)

test:
	go test ./...

run: build
	./$(BIN_DIR)/$(BINARY_NAME) generate

install: build
	go install $(MAIN_PATH)

clean:
	rm -rf $(BIN_DIR)
