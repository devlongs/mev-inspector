.PHONY: build run clean test lint fmt vet tidy

BINARY_NAME=mev-inspector
BUILD_DIR=bin
GO=go

build:
	$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/inspector

run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

clean:
	rm -rf $(BUILD_DIR)
	$(GO) clean

test:
	$(GO) test -v ./...

lint:
	golangci-lint run

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

install:
	$(GO) install ./cmd/inspector

all: tidy fmt vet build
