# API Documentation Generator - Makefile
BINARY_NAME := apidoc-gen
MAIN_PATH := .

.PHONY: build test run install clean

build:
	go build -o $(BINARY_NAME) $(MAIN_PATH)

test:
	go test ./...

run: build
	./$(BINARY_NAME) generate

install: build
	go install $(MAIN_PATH)

clean:
	rm -f $(BINARY_NAME)
