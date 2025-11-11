# X3F Extract Go Build Configuration

APP_NAME := x3f-go
VERSION := 0.1.0
BUILD_DIR := build

# Go build flags for static linking
LDFLAGS := -s -w -X main.Version=$(VERSION)
BUILD_FLAGS := -ldflags="$(LDFLAGS)" -trimpath

# Targets
.PHONY: all build clean test install

all: build

build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(APP_NAME) ./cmd/x3f-go

# Cross-platform builds
.PHONY: build-all build-darwin build-linux build-windows

build-all: build-darwin build-linux build-windows

build-darwin:
	@echo "Building for macOS..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(BUILD_FLAGS) \
		-o $(BUILD_DIR)/$(APP_NAME)-darwin-arm64 ./cmd/x3f-go
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build $(BUILD_FLAGS) \
		-o $(BUILD_DIR)/$(APP_NAME)-darwin-amd64 ./cmd/x3f-go

build-linux:
	@echo "Building for Linux..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(BUILD_FLAGS) \
		-o $(BUILD_DIR)/$(APP_NAME)-linux-amd64 ./cmd/x3f-go
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(BUILD_FLAGS) \
		-o $(BUILD_DIR)/$(APP_NAME)-linux-arm64 ./cmd/x3f-go

build-windows:
	@echo "Building for Windows..."
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(BUILD_FLAGS) \
		-o $(BUILD_DIR)/$(APP_NAME)-windows-amd64.exe ./cmd/x3f-go

test:
	@echo "Running tests..."
	go test -v ./...

clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	go clean

install: build
	@echo "Installing $(APP_NAME)..."
	go install ./cmd/x3f-go

# Development helpers
.PHONY: fmt vet lint

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...
