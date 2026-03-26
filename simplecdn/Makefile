# Binary names
CDN_BIN=cdn
SERVER_BIN=server

# Source files
CDN_SRC=cdn/cdn.go
SERVER_SRC=server/server.go

# Output directory
BIN_DIR=bin

# Go command
GO=go

.PHONY: all build clean

# Default target
all: build

# Build both binaries
build: $(BIN_DIR)/$(CDN_BIN) $(BIN_DIR)/$(SERVER_BIN)

# Build client
$(BIN_DIR)/$(CDN_BIN): $(CDN_SRC)
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $@ $<

# Build server
$(BIN_DIR)/$(SERVER_BIN): $(SERVER_SRC)
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $@ $<

# Clean binaries
clean:
	rm -rf $(BIN_DIR)
