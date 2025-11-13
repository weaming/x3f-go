APP_NAME := x3f-go
BUILD_DIR := build
LDFLAGS := -s -w -X main.Version=debug
BUILD_FLAGS := -ldflags="$(LDFLAGS)" -trimpath

build: fmt
	@mkdir -p $(BUILD_DIR)
	go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(APP_NAME) ./cmd/x3f-go/

build-all:
	fish build-all.fish

fmt:
	go fmt ./...