# Binary names
CLIENT_BIN=client
SERVER_BIN=server

# Source files
CLIENT_SRC=client/client.go
SERVER_SRC=server/server.go

# Output directory
BIN_DIR=bin

# Go command
GO=go

.PHONY: all build clean

# Default target
all: build

# Build both binaries
build: $(BIN_DIR)/$(CLIENT_BIN) $(BIN_DIR)/$(SERVER_BIN)

# Build client
$(BIN_DIR)/$(CLIENT_BIN): $(CLIENT_SRC)
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $@ $<

# Build server
$(BIN_DIR)/$(SERVER_BIN): $(SERVER_SRC)
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $@ $<

# Clean binaries
clean:
	rm -rf $(BIN_DIR)