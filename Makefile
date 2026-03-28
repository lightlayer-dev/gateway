BINARY_NAME := lightlayer-gateway
BUILD_DIR := .

.PHONY: build test lint clean run ui

ui:
	cd ui && npm ci && npm run build

build: ui
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/gateway

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -f $(BUILD_DIR)/$(BINARY_NAME)
	rm -rf ui/dist

run: build
	./$(BINARY_NAME) start
