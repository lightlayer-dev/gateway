BINARY_NAME := lightlayer-gateway
BUILD_DIR := .

.PHONY: build test lint clean run

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/gateway

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -f $(BUILD_DIR)/$(BINARY_NAME)

run: build
	./$(BINARY_NAME) start
